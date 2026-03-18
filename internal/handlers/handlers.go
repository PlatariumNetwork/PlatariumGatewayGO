package handlers

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"platarium-gateway-go/internal/blockchain"
	"platarium-gateway-go/internal/core"
	"platarium-gateway-go/internal/logger"
	"platarium-gateway-go/internal/nodes"
	"platarium-gateway-go/internal/rating"
	"platarium-gateway-go/internal/rewards"
	"platarium-gateway-go/internal/websocket"

	"github.com/gorilla/mux"
)

const (
	L1VoteThresholdPct  = 67 // L1 need 67% yes (Core: L1_CONFIRM_THRESHOLD_PCT)
	L2VoteThresholdPct  = 70 // L2 need 70% yes (Core)
	VoteRoundTimeoutMin = 5 * time.Second   // minimum wait for votes
	VoteRoundTimeoutPerNode = 150 * time.Millisecond // extra time per expected node (for 100 nodes: 5s + 15s = 20s)
	VoteRoundTimeoutMax = 45 * time.Second  // cap so we don't wait forever
	ForwardRequestTimeout = 60 * time.Second // timeout when forwarding L1/L2 (vote round + processing)
)

type l1VoteRound struct {
	blockId       string
	votes         map[string]bool // nodeId -> yes
	totalExpected int
	committee     map[string]bool // якщо не nil - рахуються лише голоси з committee (Core: менше валідаторів при навантаженні)
	done          chan bool
	closed        bool
	mu            sync.Mutex
}

type Handler struct {
	blockchain           *blockchain.Blockchain
	nodesManager         *nodes.NodesManager
	wsServer             *websocket.Server
	rustCore             *core.RustCore
	testnet              bool
	nodeEarnedFees       int64
	nodeEarnedL1         int64
	nodeEarnedMu         sync.Mutex
	distributor          *rewards.Distributor
	nodeRegistry         *rating.Registry
	pendingL1Beneficiary string
	pendingL1Mu          sync.Mutex

	// Per-node cumulative earnings (populated from fee_distribution broadcasts)
	allNodeEarnings   map[string][2]int64 // nodeID -> [l1earned, l2earned]
	allNodeEarningsMu sync.RWMutex

	// Real L1/L2 voting
	l1VoteRoundMu sync.Mutex
	l1VoteRound   *l1VoteRound
	l1VotedIds    map[string]bool // blockId -> true (we already voted for this L1 proposal)
	l2VoteRoundMu sync.Mutex
	l2VoteRound   *l2VoteRound
	l2VotedIds    map[string]bool
	// Last round result for UI (real votes)
	lastL1VotesMu   sync.RWMutex
	lastL1Votes     map[string]bool // nodeId -> yes
	lastL1Accepted  bool
	lastL2Votes     map[string]bool
	lastL2Accepted  bool
}

type l2VoteRound struct {
	blockId       string
	votes         map[string]bool
	totalExpected int
	committee     map[string]bool // лише голоси з committee рахуються (Core: менше валідаторів при навантаженні)
	done          chan bool
	closed        bool
	mu            sync.Mutex
}

// NewHandler creates the HTTP handler. If testnet is true, Rust Core is required (returns error if not found).
func NewHandler(bc *blockchain.Blockchain, nm *nodes.NodesManager, ws *websocket.Server, testnet bool) (*Handler, error) {
	rustCore, err := core.NewRustCore()
	if err != nil {
		if testnet {
			return nil, err
		}
		log.Printf("[WARN] Rust Core not available: %v. Some features may be limited.", err)
		rustCore = nil
	} else {
		log.Println("[INFO] Rust Core initialized successfully")
	}

	h := &Handler{
		blockchain:      bc,
		nodesManager:    nm,
		wsServer:        ws,
		rustCore:        rustCore,
		testnet:         testnet,
		distributor:     rewards.NewDistributor(),
		nodeRegistry:    rating.NewRegistry(),
		allNodeEarnings: make(map[string][2]int64),
		l1VotedIds:      make(map[string]bool),
		l2VotedIds:      make(map[string]bool),
	}
	h.RegisterVoteCallbacks()
	return h, nil
}

// RegisterVoteCallbacks registers L1/L2 proposal and vote callbacks with the nodes manager.
func (h *Handler) RegisterVoteCallbacks() {
	h.nodesManager.SetL1ProposalCallback(h.onL1Proposal)
	h.nodesManager.SetL1VoteCallback(h.onL1Vote)
	h.nodesManager.SetL2ProposalCallback(h.onL2Proposal)
	h.nodesManager.SetL2VoteCallback(h.onL2Vote)
	h.nodesManager.SetL1BlockCollectedCallback(h.onL1BlockCollected)
	h.nodesManager.SetPendingBlockSyncCallback(h.onPendingBlockSync)
	h.nodesManager.SetMempoolAddCallback(h.onMempoolAdd)
	h.nodesManager.SetL1VoteResultCallback(h.onL1VoteResult)
	h.nodesManager.SetL2VoteResultCallback(h.onL2VoteResult)
	h.nodesManager.SetFeeDistributionCallback(h.onFeeDistribution)
	h.nodesManager.SetBlockConfirmedCallback(h.onBlockConfirmed)
	h.nodesManager.SetNodeLoadCallback(h.onNodeLoad)
}

func (h *Handler) onNodeLoad(nodeId string, currentTasks, maxCapacity int64) {
	h.nodeRegistry.EnsureNode(nodeId, 0, 1)
	h.nodeRegistry.SetLoad(nodeId, currentTasks, maxCapacity)
}

func (h *Handler) onMempoolAdd(txMap map[string]interface{}) {
	tx := mapToTx(txMap)
	if tx == nil {
		return
	}
	if tx.SigMain != "" && h.rustCore != nil {
		coreJSON, ok := txToCoreJSON(tx)
		if ok {
			if valid, err := h.rustCore.ValidateTransaction(coreJSON); err != nil || !valid {
				logger.Warn("mempool:add rejected invalid signed tx %s: %v", tx.Hash, err)
				return
			}
		}
	}
	_ = h.blockchain.AddToMempool(tx)
}

func (h *Handler) onPendingBlockSync(pendingMaps []map[string]interface{}) {
	txs := make([]*blockchain.Transaction, 0, len(pendingMaps))
	for _, m := range pendingMaps {
		tx := mapToTx(m)
		if tx != nil {
			txs = append(txs, tx)
		}
	}
	if len(txs) > 0 {
		h.blockchain.SetPendingBlock(txs)
	}
}

func mapToTx(m map[string]interface{}) *blockchain.Transaction {
	if m == nil {
		return nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	var tx blockchain.Transaction
	if json.Unmarshal(b, &tx) != nil {
		return nil
	}
	if tx.Hash == "" {
		return nil
	}
	return &tx
}

func txToMap(tx *blockchain.Transaction) map[string]interface{} {
	if tx == nil {
		return nil
	}
	b, _ := json.Marshal(tx)
	var m map[string]interface{}
	_ = json.Unmarshal(b, &m)
	return m
}

// txToCoreJSON returns JSON string for Core validate-tx. ok is false if tx is not Core-signed (no sig_main).
func txToCoreJSON(tx *blockchain.Transaction) (jsonStr string, ok bool) {
	if tx == nil || tx.SigMain == "" {
		return "", false
	}
	asset := tx.Asset
	if asset == "" {
		asset = "PLP"
	}
	amount := tx.AmountUplp
	if amount == 0 && tx.Value != "" {
		if v, err := strconv.ParseUint(tx.Value, 10, 64); err == nil {
			amount = v
		}
	}
	feeUplp := tx.FeeUplp
	if feeUplp == 0 && tx.Fee != "" {
		if v, err := strconv.ParseUint(tx.Fee, 10, 64); err == nil {
			feeUplp = v
		}
	}
	if feeUplp == 0 {
		feeUplp = 1
	}
	reads := tx.Reads
	if reads == nil {
		reads = []string{}
	}
	writes := tx.Writes
	if writes == nil {
		writes = []string{}
	}
	m := map[string]interface{}{
		"hash":       tx.Hash,
		"from":       tx.From,
		"to":         tx.To,
		"asset":      asset,
		"amount":     amount,
		"fee_uplp":   feeUplp,
		"nonce":      tx.Nonce,
		"reads":      reads,
		"writes":     writes,
		"sig_main":   tx.SigMain,
		"sig_derived": tx.SigDerived,
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", false
	}
	return string(b), true
}

func (h *Handler) onL1BlockCollected(l1BeneficiaryNodeId string) {
	if l1BeneficiaryNodeId == "" {
		return
	}
	h.pendingL1Mu.Lock()
	h.pendingL1Beneficiary = l1BeneficiaryNodeId
	h.pendingL1Mu.Unlock()
}

func (h *Handler) onL1VoteResult(votes map[string]bool, accepted bool) {
	h.lastL1VotesMu.Lock()
	h.lastL1Votes = make(map[string]bool)
	for k, v := range votes {
		h.lastL1Votes[k] = v
	}
	h.lastL1Accepted = accepted
	h.lastL1VotesMu.Unlock()
}

func (h *Handler) onL2VoteResult(votes map[string]bool, accepted bool) {
	h.lastL1VotesMu.Lock()
	h.lastL2Votes = make(map[string]bool)
	for k, v := range votes {
		h.lastL2Votes[k] = v
	}
	h.lastL2Accepted = accepted
	h.lastL1VotesMu.Unlock()
}

func (h *Handler) onL1Proposal(blockId, proposerNodeId string, txCount int) {
	myId := h.nodesManager.GetNodeID()
	if proposerNodeId == myId {
		return
	}
	h.l1VoteRoundMu.Lock()
	if h.l1VotedIds[blockId] {
		h.l1VoteRoundMu.Unlock()
		return
	}
	h.l1VotedIds[blockId] = true
	h.l1VoteRoundMu.Unlock()
	yes := rand.Float32() < 0.9
	logger.Info("L1 received proposal from %s (txCount=%d), sending vote yes=%v", shortId(proposerNodeId), txCount, yes)
	go h.nodesManager.BroadcastBlockchainEvent("l1_vote", map[string]interface{}{
		"blockId": blockId, "nodeId": myId, "yes": yes,
	}, myId)
}

func (h *Handler) onL1Vote(blockId, nodeId string, yes bool) {
	h.l1VoteRoundMu.Lock()
	r := h.l1VoteRound
	h.l1VoteRoundMu.Unlock()
	if r == nil || r.blockId != blockId {
		return
	}
	if r.committee != nil && !r.committee[nodeId] {
		return // Core: рахуємо лише голоси обраного комітету (менше валідаторів при навантаженні)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	if _, exists := r.votes[nodeId]; exists {
		return
	}
	r.votes[nodeId] = yes
	yesCount := 0
	for _, v := range r.votes {
		if v {
			yesCount++
		}
	}
	logger.Info("L1 vote received from %s yes=%v (total=%d/%d)", shortId(nodeId), yes, len(r.votes), r.totalExpected)
	need := (r.totalExpected*L1VoteThresholdPct + 99) / 100
	if yesCount >= need && len(r.votes) >= need {
		r.closed = true
		logger.Info("L1 threshold reached: yes=%d need=%d - closing round", yesCount, need)
		select {
		case r.done <- true:
		default:
		}
	}
}

func (h *Handler) onL2Proposal(blockId, proposerNodeId string) {
	myId := h.nodesManager.GetNodeID()
	if proposerNodeId == myId {
		return
	}
	h.l2VoteRoundMu.Lock()
	if h.l2VotedIds[blockId] {
		h.l2VoteRoundMu.Unlock()
		return
	}
	h.l2VotedIds[blockId] = true
	h.l2VoteRoundMu.Unlock()
	yes := true
	if h.rustCore != nil {
		pending := h.blockchain.GetPendingBlock()
		for _, tx := range pending {
			coreJSON, ok := txToCoreJSON(tx)
			if !ok {
				continue
			}
			valid, err := h.rustCore.ValidateTransaction(coreJSON)
			if err != nil || !valid {
				logger.Info("L2 block validation failed for tx %s: %v", tx.Hash, err)
				yes = false
				break
			}
		}
	} else {
		yes = rand.Float32() < 0.9
	}
	logger.Info("L2 received proposal from %s, sending vote yes=%v", shortId(proposerNodeId), yes)
	go h.nodesManager.BroadcastBlockchainEvent("l2_vote", map[string]interface{}{
		"blockId": blockId, "nodeId": myId, "yes": yes,
	}, myId)
}

// voteRoundTimeout returns timeout for the vote round; scales with node count so many nodes have time to respond.
func voteRoundTimeout(totalExpected int) time.Duration {
	d := VoteRoundTimeoutMin + time.Duration(totalExpected)*VoteRoundTimeoutPerNode
	if d > VoteRoundTimeoutMax {
		d = VoteRoundTimeoutMax
	}
	return d
}

// updateVoteStatsFromRound updates node registry vote stats (for reputation). Correct = (votedYes == accepted).
// If expectedVoters != nil (комітет L1/L2), штрафуємо лише тих із комітету, хто не проголосував.
func (h *Handler) updateVoteStatsFromRound(votes map[string]bool, accepted bool, expectedVoters map[string]bool) {
	for nodeId, votedYes := range votes {
		h.nodeRegistry.EnsureNode(nodeId, 0, 1)
		n := h.nodeRegistry.Get(nodeId)
		if n == nil {
			continue
		}
		total := n.TotalVotes + 1
		missed := n.MissedVotes
		if votedYes != accepted {
			missed++
		}
		h.nodeRegistry.SetVoteStats(nodeId, missed, total)
		logger.Info("reputation vote node=%s votedYes=%v accepted=%v missed=%d total=%d", shortId(nodeId), votedYes, accepted, missed, total)
	}
	var toPenalise []string
	if expectedVoters != nil {
		for id := range expectedVoters {
			if _, voted := votes[id]; !voted {
				toPenalise = append(toPenalise, id)
			}
		}
	} else {
		connected := h.nodesManager.GetConnectedNodes()
		for _, cn := range connected {
			if _, voted := votes[cn.NodeID]; !voted {
				toPenalise = append(toPenalise, cn.NodeID)
			}
		}
	}
	for _, nodeId := range toPenalise {
		h.nodeRegistry.EnsureNode(nodeId, 0, 1)
		n := h.nodeRegistry.Get(nodeId)
		if n == nil {
			continue
		}
		missed := n.MissedVotes + 1
		total := n.TotalVotes + 1
		h.nodeRegistry.SetVoteStats(nodeId, missed, total)
		logger.Info("reputation penalty node=%s (no vote) missed=%d total=%d", shortId(nodeId), missed, total)
	}
}

// distributePool splits pool among YES-voters proportional to SelectionWeight (Core: винагорода всім учасникам голосування).
// When pool >= number of voters, each voter gets at least 1; remainder is distributed by weight.
func (h *Handler) distributePool(pool int64, voters map[string]bool) map[string]int64 {
	shares := make(map[string]int64)
	if pool <= 0 {
		return shares
	}
	var yesVoters []string
	for id, yes := range voters {
		if yes {
			yesVoters = append(yesVoters, id)
		}
	}
	n := len(yesVoters)
	if n == 0 {
		return shares
	}
	// Weights for proportional split (рейтинг враховується).
	var totalWeight int64
	weights := make(map[string]int64)
	for _, id := range yesVoters {
		w := h.nodeRegistry.SelectionWeightFor(id)
		if w <= 0 {
			w = 1
		}
		weights[id] = w
		totalWeight += w
	}
	if totalWeight <= 0 {
		totalWeight = int64(n)
		for _, id := range yesVoters {
			weights[id] = 1
		}
	}
	// Core: кожен учасник голосування отримує частку; при достатньому пулі - мінімум 1.
	remainder := pool
	if pool >= int64(n) {
		remainder = pool - int64(n)
		for _, id := range yesVoters {
			shares[id] = 1
		}
	}
	if remainder <= 0 {
		return shares
	}
	// Розподіл залишку пропорційно вазі (репутація × (1−load)).
	var distributed int64
	for _, id := range yesVoters {
		s := remainder * weights[id] / totalWeight
		shares[id] += s
		distributed += s
	}
	if distributed < remainder && n > 0 {
		shares[yesVoters[0]] += remainder - distributed
	}
	return shares
}

// recordEarnings stores per-node earnings locally and updates own counters.
func (h *Handler) recordEarnings(l1Shares, l2Shares map[string]int64) {
	myId := h.nodesManager.GetNodeID()
	h.allNodeEarningsMu.Lock()
	for id, amount := range l1Shares {
		e := h.allNodeEarnings[id]
		e[0] += amount
		h.allNodeEarnings[id] = e
	}
	for id, amount := range l2Shares {
		e := h.allNodeEarnings[id]
		e[1] += amount
		h.allNodeEarnings[id] = e
	}
	h.allNodeEarningsMu.Unlock()

	h.nodeEarnedMu.Lock()
	if l1, ok := l1Shares[myId]; ok {
		h.nodeEarnedL1 += l1
	}
	if l2, ok := l2Shares[myId]; ok {
		h.nodeEarnedFees += l2
	}
	h.nodeEarnedMu.Unlock()
}

// onFeeDistribution handles fee_distribution broadcast from another node.
func (h *Handler) onFeeDistribution(data map[string]interface{}) {
	l1Raw, _ := data["l1Shares"].(map[string]interface{})
	l2Raw, _ := data["l2Shares"].(map[string]interface{})
	l1Shares := make(map[string]int64)
	l2Shares := make(map[string]int64)
	for k, v := range l1Raw {
		if f, ok := v.(float64); ok {
			l1Shares[k] = int64(f)
		}
	}
	for k, v := range l2Raw {
		if f, ok := v.(float64); ok {
			l2Shares[k] = int64(f)
		}
	}
	h.recordEarnings(l1Shares, l2Shares)
	// Sync burn/treasury so all nodes show same Спалено всього | Скарбниця всього.
	var burn, treasury int64
	if b, ok := data["burn"].(float64); ok {
		burn = int64(b)
	}
	if t, ok := data["treasury"].(float64); ok {
		treasury = int64(t)
	}
	if burn > 0 || treasury > 0 {
		h.distributor.AddBurnTreasury(burn, treasury)
	}
}

// onBlockConfirmed handles block_confirmed broadcast: syncs the confirmed block into local chain.
func (h *Handler) onBlockConfirmed(data map[string]interface{}) {
	blockNum, _ := data["blockNumber"].(float64)
	timestamp, _ := data["timestamp"].(float64)
	totalFees, _ := data["totalFees"].(float64)
	txHashesRaw, _ := data["txHashes"].([]interface{})
	txsRaw, _ := data["transactions"].([]interface{})

	block := blockchain.BlockRecord{
		BlockNumber: int64(blockNum),
		Timestamp:   int64(timestamp),
		TotalFees:   int64(totalFees),
		TxHashes:    make([]string, 0, len(txHashesRaw)),
	}
	if v, ok := data["l1Yes"].(float64); ok {
		block.L1Yes = int(v)
	}
	if v, ok := data["l1No"].(float64); ok {
		block.L1No = int(v)
	}
	if v, ok := data["l2Yes"].(float64); ok {
		block.L2Yes = int(v)
	}
	if v, ok := data["l2No"].(float64); ok {
		block.L2No = int(v)
	}
	if v, ok := data["durationMs"].(float64); ok {
		block.DurationMs = int(v)
	}
	if s, ok := data["l1BeneficiaryNodeId"].(string); ok {
		block.L1BeneficiaryNodeId = s
	}
	if s, ok := data["l2ConfirmerNodeId"].(string); ok {
		block.L2ConfirmerNodeId = s
	}
	if m, ok := data["l1Votes"].(map[string]interface{}); ok && len(m) > 0 {
		block.L1Votes = make(map[string]bool, len(m))
		for k, val := range m {
			if b, ok := val.(bool); ok {
				block.L1Votes[k] = b
			}
		}
	}
	if m, ok := data["l2Votes"].(map[string]interface{}); ok && len(m) > 0 {
		block.L2Votes = make(map[string]bool, len(m))
		for k, val := range m {
			if b, ok := val.(bool); ok {
				block.L2Votes[k] = b
			}
		}
	}
	for _, v := range txHashesRaw {
		if s, ok := v.(string); ok {
			block.TxHashes = append(block.TxHashes, s)
		}
	}
	block.TxCount = len(block.TxHashes)

	var txs []*blockchain.Transaction
	for _, raw := range txsRaw {
		if m, ok := raw.(map[string]interface{}); ok {
			tx := mapToTx(m)
			if tx != nil {
				txs = append(txs, tx)
			}
		}
	}

	if h.blockchain.AddConfirmedBlock(block, txs) {
		logger.Info("Block #%d synced from peer (txCount=%d fees=%d)", block.BlockNumber, block.TxCount, block.TotalFees)
		h.wsServer.BroadcastEvent("blockConfirmed", map[string]interface{}{
			"blockNumber": block.BlockNumber,
			"txCount":     block.TxCount,
			"totalFees":   block.TotalFees,
		})
	}
}

func (h *Handler) onL2Vote(blockId, nodeId string, yes bool) {
	h.l2VoteRoundMu.Lock()
	r := h.l2VoteRound
	h.l2VoteRoundMu.Unlock()
	if r == nil || r.blockId != blockId {
		return
	}
	if r.committee != nil && !r.committee[nodeId] {
		return // Core: рахуємо лише голоси обраного комітету
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	if _, exists := r.votes[nodeId]; exists {
		return
	}
	r.votes[nodeId] = yes
	yesCount := 0
	for _, v := range r.votes {
		if v {
			yesCount++
		}
	}
	logger.Info("L2 vote received from %s yes=%v (total=%d/%d)", shortId(nodeId), yes, len(r.votes), r.totalExpected)
	need := (r.totalExpected*L2VoteThresholdPct + 99) / 100
	if yesCount >= need && len(r.votes) >= need {
		r.closed = true
		logger.Info("L2 threshold reached: yes=%d need=%d - closing round", yesCount, need)
		select {
		case r.done <- true:
		default:
		}
	}
}

func (h *Handler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"message":        "PlatariumGateway v1.0.0 is running (Go)",
		"nodeId":         h.nodesManager.GetNodeID(),
		"nodeAddress":    h.nodesManager.GetNodeAddress(),
		"connectedPeers": len(h.nodesManager.GetConnectedNodes()),
	}
	jsonResponse(w, http.StatusOK, response)
}

func (h *Handler) NetworkStatus(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"nodeId":        h.nodesManager.GetNodeID(),
		"nodeAddress":   h.nodesManager.GetNodeAddress(),
		"connectedNodes": h.nodesManager.GetConnectedNodes(),
	}
	jsonResponse(w, http.StatusOK, response)
}

func (h *Handler) GetSockets(w http.ResponseWriter, r *http.Request) {
	// Query peer nodes for their socket lists
	h.nodesManager.QueryPeerSockets()
	
	allSockets := h.nodesManager.GetConnectedSockets()
	connectedNodes := h.nodesManager.GetConnectedNodes()
	
	response := map[string]interface{}{
		"nodeId":          h.nodesManager.GetNodeID(),
		"nodeAddress":     h.nodesManager.GetNodeAddress(),
		"connectedSockets": allSockets,
		"summary": map[string]interface{}{
			"connectedClients": len(allSockets),
			"connectedPeers":   len(connectedNodes),
		},
	}
	jsonResponse(w, http.StatusOK, response)
}

// GetDetailedStatus returns detailed status of all components
func (h *Handler) GetDetailedStatus(w http.ResponseWriter, r *http.Request) {
	components := make(map[string]string)
	
	// Check REST API status (if we can respond, it's OK)
	components["REST"] = "ok"
	
	// Check WebSocket server status
	if h.wsServer != nil {
		components["WebSocket"] = "ok"
	} else {
		components["WebSocket"] = "not_ok"
	}
	
	// Check P2P connections (must have at least 1 peer)
	connectedNodes := h.nodesManager.GetConnectedNodes()
	if len(connectedNodes) > 0 {
		components["P2P"] = "ok"
	} else {
		components["P2P"] = "not_ok"
	}
	
	// Check Balance system (check if blockchain is initialized)
	if h.blockchain != nil {
		// Try to get a balance to verify it works
		// GetBalance always returns a string (even "0" for non-existent addresses)
		testBalance := h.blockchain.GetBalance("test")
		// If we get a response (even "0"), the system is working
		if testBalance != "" {
			components["Balance"] = "ok"
		} else {
			components["Balance"] = "not_ok"
		}
	} else {
		components["Balance"] = "not_ok"
	}
	
	// Check Transactions module (check if we can get last transaction)
	if h.blockchain != nil {
		lastTx := h.blockchain.GetLastTransaction()
		if lastTx != nil {
			components["Transactions"] = "ok"
		} else {
			// If no transactions yet, it's still OK (module works)
			components["Transactions"] = "ok"
		}
	} else {
		components["Transactions"] = "not_ok"
	}
	
	// Determine overall status
	overallStatus := "ok"
	for _, status := range components {
		if status == "not_ok" {
			overallStatus = "not_ok"
			break
		}
	}
	
	// Get peers with ping information
	peersWithPing := h.nodesManager.GetPeersWithPing()
	
	// Get socket summary
	allSockets := h.nodesManager.GetConnectedSockets()
	
	// Get metrics
	metrics := h.nodesManager.GetMetrics()
	
	response := map[string]interface{}{
		"network":        "Platarium",
		"status":         overallStatus,
		"components":     components,
		"nodeId":         h.nodesManager.GetNodeID(),
		"nodeAddress":    h.nodesManager.GetNodeAddress(),
		"connectedPeers": len(connectedNodes),
		"peers":          peersWithPing,
		"summary": map[string]interface{}{
			"connectedClients": len(allSockets),
			"connectedPeers":   len(connectedNodes),
		},
		"metrics":   metrics,
		"timestamp": time.Now().UnixMilli(),
	}
	
	jsonResponse(w, http.StatusOK, response)
}

func (h *Handler) GetBalance(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	address := vars["address"]
	balance := h.blockchain.GetBalance(address)
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"address": address,
		"balance": balance,
	})
}

// GenerateWallet creates a new wallet via Core (mnemonic + alphanumeric + publicKey). For testnet/real flow.
func (h *Handler) GenerateWallet(w http.ResponseWriter, r *http.Request) {
	if h.rustCore == nil {
		jsonResponse(w, http.StatusServiceUnavailable, map[string]string{"error": "Core (platarium-cli) not available"})
		return
	}
	mnemonic, alphanumeric, err := h.rustCore.GenerateMnemonic()
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	keys, err := h.rustCore.GenerateKeys(mnemonic, alphanumeric, 0)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	publicKey := keys["publicKey"]
	if publicKey == "" {
		jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": "Core did not return publicKey"})
		return
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"mnemonic":    mnemonic,
		"alphanumeric": alphanumeric,
		"publicKey":   publicKey,
		"address":     publicKey,
	})
}

// RestoreWallet derives wallet keys/address from mnemonic + alphanumeric (same as GenerateWallet would).
func (h *Handler) RestoreWallet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
		return
	}
	if h.rustCore == nil {
		jsonResponse(w, http.StatusServiceUnavailable, map[string]string{"error": "Core (platarium-cli) not available"})
		return
	}
	var body struct {
		Mnemonic     string `json:"mnemonic"`
		Alphanumeric string `json:"alphanumeric"`
		SeedIndex    uint32 `json:"seedIndex"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON"})
		return
	}
	body.Mnemonic = strings.TrimSpace(body.Mnemonic)
	body.Alphanumeric = strings.TrimSpace(body.Alphanumeric)
	if body.Mnemonic == "" || body.Alphanumeric == "" {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "body must include mnemonic and alphanumeric"})
		return
	}
	keys, err := h.rustCore.GenerateKeys(body.Mnemonic, body.Alphanumeric, body.SeedIndex)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	publicKey := keys["publicKey"]
	if publicKey == "" {
		jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": "Core did not return publicKey"})
		return
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"publicKey": publicKey,
		"address":   publicKey,
	})
}

// Faucet adds a faucet TX to mempool; balance is applied when block is confirmed (L1→L2). All nodes get consistent state.
const FaucetAmountUplp = 1_000_000

func (h *Handler) Faucet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
		return
	}
	var body struct {
		Address string `json:"address"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Address == "" {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "body must include \"address\""})
		return
	}
	tx := &blockchain.Transaction{
		Hash:       generateHash(),
		From:       blockchain.FaucetAddress,
		To:         body.Address,
		Value:      strconv.FormatUint(FaucetAmountUplp, 10),
		Fee:        "0",
		Nonce:      0,
		Timestamp:  time.Now().Unix(),
		Type:       "faucet",
		AssetType:  "native",
		AmountUplp: FaucetAmountUplp,
		FeeUplp:    0,
	}
	if err := h.blockchain.AddToMempool(tx); err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	myId := h.nodesManager.GetNodeID()
	h.wsServer.BroadcastEvent("mempoolUpdate", map[string]interface{}{"hash": tx.Hash, "from": tx.From, "to": tx.To, "value": tx.Value, "type": "faucet"})
	go h.nodesManager.BroadcastBlockchainEvent("mempoolUpdate", map[string]interface{}{"hash": tx.Hash}, myId)
	go h.nodesManager.BroadcastBlockchainEvent("mempool:add", map[string]interface{}{"tx": txToMap(tx)}, myId)
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"address":  body.Address,
		"credited": FaucetAmountUplp,
		"txHash":   tx.Hash,
		"balance":  "0",
		"message":  "TX крану в mempool. Запустіть L1→L2 (або чекайте авто), щоб підтвердити блок і отримати баланс.",
	})
}

func (h *Handler) GetTransaction(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	hash := vars["hash"]
	
	tx := h.blockchain.GetTransaction(hash)
	if tx == nil {
		jsonResponse(w, http.StatusNotFound, map[string]string{
			"error": "Transaction not found",
		})
		return
	}
	jsonResponse(w, http.StatusOK, tx)
}

func (h *Handler) GetTransactions(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	address := vars["address"]
	
	txs := h.blockchain.GetTransactionsByAddress(address)
	jsonResponse(w, http.StatusOK, map[string]interface{}{"transactions": txs})
}

func (h *Handler) SendTransaction(w http.ResponseWriter, r *http.Request) {
	var txData map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&txData); err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid request body",
		})
		return
	}

	// Testnet: require Core and valid signature for every transaction
	if h.testnet {
		if h.rustCore == nil {
			jsonResponse(w, http.StatusServiceUnavailable, map[string]string{
				"error": "Testnet requires Platarium Core; core unavailable",
			})
			return
		}
		signature, hasSig := txData["signature"].(string)
		pubKey := getString(txData, "pubkey")
		if pubKey == "" {
			pubKey = getString(txData, "from")
		}
		if !hasSig || signature == "" {
			jsonResponse(w, http.StatusBadRequest, map[string]string{
				"error": "Testnet requires signature",
			})
			return
		}
		if pubKey == "" {
			jsonResponse(w, http.StatusBadRequest, map[string]string{
				"error": "Testnet requires pubkey or from (public key)",
			})
			return
		}
		verifyMsg := coreMessageForVerification(txData)
		verified, err := h.rustCore.VerifySignature(verifyMsg, signature, pubKey)
		if err != nil {
			log.Printf("[TESTNET] Signature verification error: %v", err)
			jsonResponse(w, http.StatusBadRequest, map[string]string{
				"error": "Signature verification failed: " + err.Error(),
			})
			return
		}
		if !verified {
			jsonResponse(w, http.StatusBadRequest, map[string]string{
				"error": "Invalid signature",
			})
			return
		}
		log.Printf("[TESTNET] Transaction signature verified by Core")
	} else if h.rustCore != nil {
		// Non-testnet: optional verification when signature is provided
		signature, hasSig := txData["signature"].(string)
		pubKey := getString(txData, "pubkey")
		if pubKey == "" {
			pubKey = getString(txData, "from")
		}
		if hasSig && signature != "" && pubKey != "" {
			verifyMsg := coreMessageForVerification(txData)
			verified, err := h.rustCore.VerifySignature(verifyMsg, signature, pubKey)
			if err != nil {
				log.Printf("[WARN] Signature verification error: %v", err)
			} else if !verified {
				jsonResponse(w, http.StatusBadRequest, map[string]string{
					"error": "Invalid signature",
				})
				return
			} else {
				log.Printf("[INFO] Transaction signature verified using Rust Core")
			}
		}
	}
	
	// Create transaction
	tx := &blockchain.Transaction{
		Hash:            generateHash(),
		From:            getString(txData, "from"),
		To:              getString(txData, "to"),
		Value:           getString(txData, "amount"),
		Fee:             "1",
		Nonce:           getInt(txData, "nonce"),
		Timestamp:       time.Now().Unix(),
		Type:            getString(txData, "type"),
		AssetType:       getString(txData, "assetType"),
		ContractAddress: getString(txData, "contractAddress"),
	}
	
	if tx.Type == "" {
		tx.Type = "transfer"
	}
	if tx.AssetType == "" {
		tx.AssetType = "native"
	}
	
	// Add transaction
	if err := h.blockchain.AddTransaction(tx); err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{
			"error": err.Error(),
		})
		return
	}
	
	// Broadcast transaction event
	eventData := map[string]interface{}{
		"hash":  tx.Hash,
		"from":  tx.From,
		"to":    tx.To,
		"value": tx.Value,
	}
	h.wsServer.BroadcastEvent("transactionProcessed", eventData)
	h.nodesManager.BroadcastBlockchainEvent("transactionProcessed", eventData, h.nodesManager.GetNodeID())
	
	response := map[string]interface{}{
		"success":     true,
		"transaction": tx,
	}
	jsonResponse(w, http.StatusOK, response)
}

func jsonResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func getString(m map[string]interface{}, key string) string {
	if val, ok := m[key].(string); ok {
		return val
	}
	return ""
}

func getInt(m map[string]interface{}, key string) int {
	if val, ok := m[key].(float64); ok {
		return int(val)
	}
	return 0
}

func getUint64(m map[string]interface{}, key string) uint64 {
	if val, ok := m[key].(float64); ok {
		return uint64(val)
	}
	if s, ok := m[key].(string); ok && s != "" {
		if v, err := strconv.ParseUint(s, 10, 64); err == nil {
			return v
		}
	}
	return 0
}

// CoreVerifyMessage is the exact shape and key order sent to platarium-cli verify-signature.
// Key order must match the message used when signing, or the hash will differ and verification fails.
type CoreVerifyMessage struct {
	From      string      `json:"from"`
	To        string      `json:"to"`
	Value     string      `json:"value"`
	Nonce     int         `json:"nonce"`
	Timestamp interface{} `json:"timestamp"`
	Type      string      `json:"type"`
}

// coreMessageForVerification builds the message in canonical key order so verify-signature hash matches the sign-message hash.
func coreMessageForVerification(txData map[string]interface{}) *CoreVerifyMessage {
	value := getString(txData, "value")
	if v := getString(txData, "amount"); v != "" {
		value = v
	}
	msg := &CoreVerifyMessage{
		From:      getString(txData, "from"),
		To:        getString(txData, "to"),
		Value:     value,
		Nonce:     getInt(txData, "nonce"),
		Timestamp: txData["timestamp"],
		Type:      getString(txData, "type"),
	}
	if msg.Type == "" {
		msg.Type = "transfer"
	}
	return msg
}

func generateHash() string {
	// Simple hash generation - in production use proper hashing
	return time.Now().Format("20060102150405") + "-hash"
}

// GetMempool returns pending transactions (for demo UI)
func (h *Handler) GetMempool(w http.ResponseWriter, r *http.Request) {
	txs := h.blockchain.GetMempool()
	jsonResponse(w, http.StatusOK, map[string]interface{}{"mempool": txs, "count": len(txs)})
}

// GetAllTransactions returns all confirmed transactions
func (h *Handler) GetAllTransactions(w http.ResponseWriter, r *http.Request) {
	txs := h.blockchain.GetAllTransactions()
	jsonResponse(w, http.StatusOK, map[string]interface{}{"transactions": txs, "count": len(txs)})
}

// GetBlockHistory returns block history for analytics
func (h *Handler) GetBlockHistory(w http.ResponseWriter, r *http.Request) {
	blocks := h.blockchain.GetBlockHistory()
	jsonResponse(w, http.StatusOK, map[string]interface{}{"blocks": blocks})
}

// GetBlock returns one block by number with full transactions and consensus log (for block detail page).
func (h *Handler) GetBlock(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	blockNumStr := vars["blockNumber"]
	if blockNumStr == "" {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "blockNumber required"})
		return
	}
	var blockNum int64
	if n, err := strconv.ParseInt(blockNumStr, 10, 64); err != nil || n < 0 {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "invalid blockNumber"})
		return
	} else {
		blockNum = n
	}
	block := h.blockchain.GetBlockByNumber(blockNum)
	if block == nil {
		// Fallback: block list might be from same node; find in full history
		allBlocks := h.blockchain.GetBlockHistory()
		for i := range allBlocks {
			if allBlocks[i].BlockNumber == blockNum {
				block = &allBlocks[i]
				break
			}
		}
	}
	if block == nil {
		jsonResponse(w, http.StatusNotFound, map[string]string{"error": "block not found"})
		return
	}
	txMaps := make([]map[string]interface{}, 0, len(block.TxHashes))
	for _, hash := range block.TxHashes {
		tx := h.blockchain.GetTransaction(hash)
		if tx != nil {
			txMaps = append(txMaps, txToMap(tx))
		}
	}
	l1Yes, l1No := block.L1Yes, block.L1No
	l2Yes, l2No := block.L2Yes, block.L2No
	consensusLog := fmt.Sprintf("L1: %d yes, %d no (threshold >=67%%). L2: %d yes, %d no (threshold >=70%%). Block confirmed. L2 round time: %d ms. L1 miner (collector): %s. L2 miner (confirmer): %s.",
		l1Yes, l1No, l2Yes, l2No, block.DurationMs,
		nonEmpty(block.L1BeneficiaryNodeId, "-"),
		nonEmpty(block.L2ConfirmerNodeId, "-"))
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"block":         block,
		"transactions": txMaps,
		"consensusLog": consensusLog,
	})
}

func nonEmpty(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// GetStats returns chain/mempool/pending stats, fees, reward distribution, and this node's L1/L2 earnings
func (h *Handler) GetStats(w http.ResponseWriter, r *http.Request) {
	st := h.blockchain.GetStats()
	h.nodeEarnedMu.Lock()
	earnedL2 := h.nodeEarnedFees
	earnedL1 := h.nodeEarnedL1
	h.nodeEarnedMu.Unlock()
	burned, treasury := h.distributor.Totals()
	cfg := h.distributor.GetConfig()
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"chainTxCount":    st.ChainTxCount,
		"mempoolCount":    st.MempoolCount,
		"pendingCount":    st.PendingCount,
		"totalFees":       st.TotalFees,
		"lastBlockNumber": st.LastBlockNum,
		"nodeEarnedFees":  earnedL2,
		"nodeEarnedL1":    earnedL1,
		"totalBurned":     burned,
		"totalTreasury":   treasury,
		"rewardConfig": map[string]interface{}{
			"burnPct":     cfg.BurnPct,
			"treasuryPct": cfg.TreasuryPct,
			"l1Pct":       cfg.L1Pct,
			"l2Pct":       cfg.L2Pct,
		},
	})
}

// DemoSendTx adds a transaction to mempool. With mnemonic+alphanumeric and Core: generates wallet, signs TX via Core, adds signed TX. Otherwise: legacy unsigned demo TX.
func (h *Handler) DemoSendTx(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
		return
	}
	var txData map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&txData); err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON"})
		return
	}
	mnemonic := getString(txData, "mnemonic")
	alphanumeric := getString(txData, "alphanumeric")
	to := getString(txData, "to")
	if to == "" {
		to = getString(txData, "toAddress")
	}
	amount := getUint64(txData, "amount")
	if amount == 0 {
		if v := getString(txData, "value"); v != "" {
			if n, err := strconv.ParseUint(v, 10, 64); err == nil {
				amount = n
			}
		}
	}
	if amount == 0 {
		amount = 1
	}
	feeUplp := getUint64(txData, "fee_uplp")
	if feeUplp == 0 {
		if v := getString(txData, "fee"); v != "" {
			if n, err := strconv.ParseUint(v, 10, 64); err == nil {
				feeUplp = n
			}
		}
	}
	if feeUplp == 0 {
		feeUplp = 1
	}
	nonce := getInt(txData, "nonce")
	if nonce < 0 {
		nonce = 0
	}
	asset := getString(txData, "asset")
	if asset == "" {
		asset = "PLP"
	}

	var tx *blockchain.Transaction
	if h.rustCore != nil && mnemonic != "" && alphanumeric != "" && to != "" {
		from := getString(txData, "from")
		if from == "" {
			keys, err := h.rustCore.GenerateKeys(mnemonic, alphanumeric, 0)
			if err != nil {
				jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "GenerateKeys: " + err.Error()})
				return
			}
			from = keys["publicKey"]
			if from == "" {
				jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "Core did not return publicKey"})
				return
			}
		}
		signedJSON, err := h.rustCore.SignTransaction(from, to, asset, amount, feeUplp, uint64(nonce), nil, nil, mnemonic, alphanumeric)
		if err != nil {
			jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "SignTransaction: " + err.Error()})
			return
		}
		var coreTx struct {
			Hash       string   `json:"hash"`
			From       string   `json:"from"`
			To         string   `json:"to"`
			Asset      string   `json:"asset"`
			Amount     uint64   `json:"amount"`
			FeeUplp    uint64   `json:"fee_uplp"`
			Nonce      int      `json:"nonce"`
			SigMain    string   `json:"sig_main"`
			SigDerived string   `json:"sig_derived"`
			Reads      []string `json:"reads"`
			Writes     []string `json:"writes"`
		}
		if err := json.Unmarshal([]byte(signedJSON), &coreTx); err != nil {
			jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "parse signed tx: " + err.Error()})
			return
		}
		tx = &blockchain.Transaction{
			Hash:       coreTx.Hash,
			From:       coreTx.From,
			To:         coreTx.To,
			Value:      strconv.FormatUint(coreTx.Amount, 10),
			Fee:        strconv.FormatUint(coreTx.FeeUplp, 10),
			Nonce:      coreTx.Nonce,
			Timestamp:  time.Now().Unix(),
			Type:       "transfer",
			AssetType:  "native",
			SigMain:    coreTx.SigMain,
			SigDerived: coreTx.SigDerived,
			Asset:      coreTx.Asset,
			AmountUplp: coreTx.Amount,
			FeeUplp:    coreTx.FeeUplp,
			Reads:      coreTx.Reads,
			Writes:     coreTx.Writes,
		}
	} else {
		tx = &blockchain.Transaction{
			Hash:      generateHash(),
			From:      getString(txData, "from"),
			To:        to,
			Value:     strconv.FormatUint(amount, 10),
			Fee:       strconv.FormatUint(feeUplp, 10),
			Nonce:     nonce,
			Timestamp: time.Now().Unix(),
			Type:      getString(txData, "type"),
			AssetType: getString(txData, "assetType"),
		}
		if tx.Type == "" {
			tx.Type = "transfer"
		}
		if tx.AssetType == "" {
			tx.AssetType = "native"
		}
	}

	if err := h.blockchain.AddToMempool(tx); err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	h.wsServer.BroadcastEvent("mempoolUpdate", map[string]interface{}{"hash": tx.Hash, "from": tx.From, "to": tx.To, "value": tx.Value})
	myId := h.nodesManager.GetNodeID()
	go h.nodesManager.BroadcastBlockchainEvent("mempoolUpdate", map[string]interface{}{"hash": tx.Hash}, myId)
	go h.nodesManager.BroadcastBlockchainEvent("mempool:add", map[string]interface{}{"tx": txToMap(tx)}, myId)
	jsonResponse(w, http.StatusOK, map[string]interface{}{"success": true, "transaction": tx, "message": "TX додано в mempool"})
}

// shortId for log lines
func shortId(id string) string {
	if len(id) <= 10 {
		return id
	}
	return id[:6] + "…" + id[len(id)-4:]
}

// sendRewardCreditL1 notifies the L1 beneficiary node to add L1 reward (called when we did L2 but L1 was another node).
func (h *Handler) sendRewardCreditL1(restBaseURL string, amount int64) {
	url := restBaseURL + "/api/reward-credit-l1"
	body, _ := json.Marshal(map[string]interface{}{"amount": amount})
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		logger.Error("L2 reward-credit-l1 failed url=%s amount=%d err=%v", url, amount, err)
		return
	}
	resp.Body.Close()
	logger.Info("L2 reward-credit-l1 OK url=%s amount=%d", url, amount)
}

// Header set when forwarding L1/L2 so the target node knows it was selected and must run (no re-forward).
const HeaderSelectedNode = "X-Platarium-Selected-Node"

// forwardPostToNode forwards POST to peer's REST URL and copies response back; returns true if forwarded.
// If selectedNodeID is non-empty, the request includes that header so the target runs without re-selecting (stops forward chain).
func (h *Handler) forwardPostToNode(restBaseURL, path, selectedNodeID string, w http.ResponseWriter) bool {
	if restBaseURL == "" {
		return false
	}
	url := restBaseURL + path
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		logger.Error("Forward POST NewRequest failed url=%s err=%v", url, err)
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	if selectedNodeID != "" {
		req.Header.Set(HeaderSelectedNode, selectedNodeID)
	}
	client := &http.Client{Timeout: ForwardRequestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		logger.Error("Forward POST failed url=%s err=%v", url, err)
		return false
	}
	defer resp.Body.Close()
	for k, v := range resp.Header {
		if k == "Content-Length" {
			continue
		}
		for _, vv := range v {
			w.Header().Add(k, vv)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
	return true
}

// L1CollectBlock runs real L1 vote round: broadcast proposal, collect votes, 67% yes required (Core threshold).
// Only the first receiver selects the proposer; when we forward we send X-Platarium-Selected-Node so the target runs without re-forwarding.
func (h *Handler) L1CollectBlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
		return
	}
	myId := h.nodesManager.GetNodeID()
	selectedHeader := r.Header.Get(HeaderSelectedNode)
	// If we received a forward with ourselves as selected, we are the proposer - run L1 without re-selecting (stops chain).
	if selectedHeader == myId {
		logger.Info("L1 running as selected proposer (forwarded to us)")
		h.l1CollectBlockRun(w, r)
		return
	}
	mempool := h.blockchain.GetMempool()
	txCount := len(mempool)
	if txCount == 0 {
		logger.Warn("L1 collect rejected: mempool empty")
		jsonResponse(w, http.StatusBadRequest, map[string]interface{}{
			"error":   "mempool empty",
			"hint":    "Send transactions first (POST /api/send-tx or /api/demo-sendtx), then call L1 collect",
		})
		return
	}
	connected := h.nodesManager.GetConnectedNodes()
	candidates := make([]string, 0, 1+len(connected))
	candidates = append(candidates, myId)
	for _, n := range connected {
		candidates = append(candidates, n.NodeID)
	}
	selected := h.nodeRegistry.SelectByWeight(candidates)
	logger.Info("L1 candidates=%d selected=%s myId=%s peers=%d", len(candidates), shortId(selected), shortId(myId), len(connected))
	if selected != myId {
		if restURL := h.nodesManager.GetPeerRestURL(selected); restURL != "" {
			logger.Info("L1 forwarding to proposer %s restURL=%s", shortId(selected), restURL)
			if h.forwardPostToNode(restURL, "/api/l1-collect", selected, w) {
				return
			}
			logger.Warn("L1 forward failed, running locally")
		} else {
			logger.Warn("L1 selected %s but no RestURL, running locally", shortId(selected))
		}
	}
	h.l1CollectBlockRun(w, r)
}

// l1CollectBlockRun performs the L1 vote round and block collect (assumes mempool check already done or we are the selected node).
func (h *Handler) l1CollectBlockRun(w http.ResponseWriter, r *http.Request) {
	mempool := h.blockchain.GetMempool()
	txCount := len(mempool)
	if txCount == 0 {
		logger.Warn("L1 collect rejected: mempool empty")
		jsonResponse(w, http.StatusBadRequest, map[string]interface{}{
			"error":   "mempool empty",
			"hint":    "Send transactions first (POST /api/send-tx or /api/demo-sendtx), then call L1 collect",
		})
		return
	}
	myId := h.nodesManager.GetNodeID()
	connected := h.nodesManager.GetConnectedNodes()
	candidates := make([]string, 0, 1+len(connected))
	candidates = append(candidates, myId)
	for _, n := range connected {
		candidates = append(candidates, n.NodeID)
	}
	// blockId first so we can use it as deterministic seed for Core committee selection
	blockId := strconv.FormatInt(time.Now().UnixNano(), 10)
	// Вся логіка комітету в Core. Навантаження мережі = max load серед кандидатів (щоб при високому load на будь-якій ноді менше валідаторів).
	loadPct := 0
	for _, id := range candidates {
		if n := h.nodeRegistry.Get(id); n != nil {
			pct := int(n.LoadScore * 100 / rating.ScoreScale)
			if pct > loadPct {
				loadPct = pct
			}
		}
	}
	numCandidates := len(candidates)
	// Розмір комітету з Core: мін. 3 (при N≥3), далі від % від кількості нод. Не константа.
	var committeeSize int
	var selectionPercent int
	var committeeList []string
	if h.rustCore != nil {
		size, err := h.rustCore.CommitteeCount(numCandidates, loadPct)
		if err != nil {
			logger.Warn("L1 Core committee-count failed, using Go fallback: %v", err)
			selectionPercent = rating.SelectionPercentFromLoad(loadPct)
			committeeSize = (numCandidates * selectionPercent) / 100
			if committeeSize < 1 {
				committeeSize = 1
			}
			if numCandidates >= 3 && committeeSize < 3 {
				committeeSize = 3
			}
			if committeeSize > numCandidates {
				committeeSize = numCandidates
			}
			others := make([]string, 0, numCandidates-1)
			for _, id := range candidates {
				if id != myId {
					others = append(others, id)
				}
			}
			toSelect := committeeSize - 1
			if toSelect < 0 {
				toSelect = 0
			}
			if toSelect > len(others) {
				toSelect = len(others)
			}
			if toSelect > 0 {
				committeeList = h.nodeRegistry.SelectCommittee(others, toSelect)
			}
		} else {
			committeeSize = size
			if numCandidates > 0 {
				selectionPercent = committeeSize * 100 / numCandidates
			}
			// Комітет = пропонер + (committeeSize-1) інших, щоб totalExpected = committeeSize завжди.
			others := make([]string, 0, numCandidates-1)
			for _, id := range candidates {
				if id != myId {
					others = append(others, id)
				}
			}
			toSelect := committeeSize - 1
			if toSelect < 0 {
				toSelect = 0
			}
			if toSelect > len(others) {
				toSelect = len(others)
			}
			if toSelect == 0 {
				committeeList = nil
			} else {
				weighted := make([]core.CommitteeCandidate, 0, len(others))
				for _, id := range others {
					w := h.nodeRegistry.SelectionWeightFor(id)
					if w <= 0 {
						w = 1
					}
					weighted = append(weighted, core.CommitteeCandidate{ID: id, Weight: w})
				}
				seed := sha256.Sum256([]byte(blockId))
				seedHex := hex.EncodeToString(seed[:])
				committeeList, err = h.rustCore.SelectCommittee(weighted, seedHex, toSelect)
				if err != nil {
					logger.Warn("L1 Core select-committee failed, using Go fallback: %v", err)
					committeeList = h.nodeRegistry.SelectCommittee(others, toSelect)
				}
			}
		}
	} else {
		selectionPercent = rating.SelectionPercentFromLoad(loadPct)
		committeeSize = (numCandidates * selectionPercent) / 100
		if committeeSize < 1 {
			committeeSize = 1
		}
		if numCandidates >= 3 && committeeSize < 3 {
			committeeSize = 3
		}
		if committeeSize > numCandidates {
			committeeSize = numCandidates
		}
		others := make([]string, 0, numCandidates-1)
		for _, id := range candidates {
			if id != myId {
				others = append(others, id)
			}
		}
		toSelect := committeeSize - 1
		if toSelect < 0 {
			toSelect = 0
		}
		if toSelect > len(others) {
			toSelect = len(others)
		}
		if toSelect > 0 {
			committeeList = h.nodeRegistry.SelectCommittee(others, toSelect)
		}
	}
	committeeSet := make(map[string]bool, committeeSize+1)
	committeeSet[myId] = true
	for _, id := range committeeList {
		committeeSet[id] = true
	}
	totalExpected := len(committeeSet) // завжди = committeeSize (пропонер + (size-1) інших)
	done := make(chan bool, 1)
	round := &l1VoteRound{
		blockId:       blockId,
		votes:         map[string]bool{myId: true},
		totalExpected: totalExpected,
		committee:     committeeSet,
		done:          done,
	}
	h.l1VoteRoundMu.Lock()
	h.l1VoteRound = round
	h.l1VoteRoundMu.Unlock()

	logger.Info("L1 committee: loadPct=%d selection%%=%d committee=%d (of %d)", loadPct, selectionPercent, totalExpected, len(candidates))

	go h.nodesManager.BroadcastBlockchainEvent("l1_proposal", map[string]interface{}{
		"blockId": blockId, "proposerNodeId": myId, "txCount": txCount,
	}, myId)
	go h.nodesManager.BroadcastBlockchainEvent("l1_vote", map[string]interface{}{
		"blockId": blockId, "nodeId": myId, "yes": true,
	}, myId)

	// Single-node: no peers receive events (we don't deliver to self), so signal done immediately.
	if totalExpected == 1 {
		logger.Info("L1 single-node mode: no peers, accepting with own vote")
		round.mu.Lock()
		if !round.closed {
			round.closed = true
			select { case round.done <- true: default: }
		}
		round.mu.Unlock()
	}

	l1Timeout := voteRoundTimeout(totalExpected)
	logger.Info("L1 vote round timeout=%v (peers=%d)", l1Timeout, totalExpected-1)
	go func() {
		time.Sleep(l1Timeout)
		round.mu.Lock()
		if !round.closed {
			round.closed = true
			select { case round.done <- false: default: }
		}
		round.mu.Unlock()
	}()

	var ok bool
	select {
	case ok = <-done:
	case <-time.After(l1Timeout + time.Second):
		ok = false
	}

	need := (totalExpected*L1VoteThresholdPct + 99) / 100
	yesCount := 0
	for _, v := range round.votes {
		if v {
			yesCount++
		}
	}
	voterIds := make([]string, 0, len(round.votes))
	for id := range round.votes {
		voterIds = append(voterIds, shortId(id))
	}
	logger.Info("L1 round result totalExpected=%d need=%d yes=%d total_votes=%d accepted=%v voters=%v", totalExpected, need, yesCount, len(round.votes), ok, voterIds)
	// Fallback: if no peer voted (old gateways or multi-node with no responses), accept with our vote only
	if !ok && totalExpected > 1 && len(round.votes) == 1 && round.votes[myId] {
		logger.Warn("L1 degraded: no peer votes (only proposer), accepting")
		ok = true
	}

	h.l1VoteRoundMu.Lock()
	h.l1VoteRound = nil
	h.l1VoteRoundMu.Unlock()

	h.lastL1VotesMu.Lock()
	h.lastL1Votes = make(map[string]bool)
	for k, v := range round.votes {
		h.lastL1Votes[k] = v
	}
	h.lastL1Accepted = ok
	h.updateVoteStatsFromRound(round.votes, ok, round.committee)
	h.lastL1VotesMu.Unlock()

	// Broadcast vote result so all nodes (and any UI) see the same L1 votes
	go h.nodesManager.BroadcastBlockchainEvent("l1_vote_result", map[string]interface{}{
		"votes":   round.votes,
		"accepted": ok,
	}, myId)

	if !ok {
		logger.Error("L1 threshold not met yes=%d total=%d need=%d", yesCount, len(round.votes), need)
		jsonResponse(w, http.StatusBadRequest, map[string]interface{}{
			"error":   "L1 threshold not met (need 67% yes)",
			"yes":     yesCount,
			"total":   len(round.votes),
			"need":    need,
		})
		return
	}

	h.pendingL1Mu.Lock()
	h.pendingL1Beneficiary = myId
	h.pendingL1Mu.Unlock()
	moved := h.blockchain.L1CollectBlock()
	logger.Info("L1 block collected proposer=%s moved=%d", shortId(myId), len(moved))
	pendingBlockMaps := make([]map[string]interface{}, 0, len(moved))
	for _, tx := range moved {
		pendingBlockMaps = append(pendingBlockMaps, txToMap(tx))
	}
	eventData := map[string]interface{}{"txCount": len(moved), "l1BeneficiaryNodeId": myId, "pendingBlock": pendingBlockMaps}
	h.wsServer.BroadcastEvent("l1BlockCollected", map[string]interface{}{"txCount": len(moved), "l1BeneficiaryNodeId": myId})
	go h.nodesManager.BroadcastBlockchainEvent("l1BlockCollected", eventData, myId)
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"moved":   len(moved),
		"message": "L1 зібрав блок (67% так), TX у черзі на L2",
	})
}

// GetPendingBlock returns block collected by L1 (waiting for L2)
func (h *Handler) GetPendingBlock(w http.ResponseWriter, r *http.Request) {
	txs := h.blockchain.GetPendingBlock()
	jsonResponse(w, http.StatusOK, map[string]interface{}{"pendingBlock": txs, "count": len(txs)})
}

// L2ConfirmBlock runs real L2 vote round: 70% yes required (Core), then moves pending → chain.
// Only the first receiver selects the confirmer; when we forward we send X-Platarium-Selected-Node so the target runs without re-forwarding.
func (h *Handler) L2ConfirmBlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
		return
	}
	myId := h.nodesManager.GetNodeID()
	selectedHeader := r.Header.Get(HeaderSelectedNode)
	if selectedHeader == myId {
		logger.Info("L2 running as selected confirmer (forwarded to us)")
		h.l2ConfirmBlockRun(w, r)
		return
	}
	pending := h.blockchain.GetPendingBlock()
	if len(pending) == 0 {
		logger.Warn("L2 confirm rejected: no pending block (L1 collect first)")
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "no pending block (L1 collect first)"})
		return
	}
	connected := h.nodesManager.GetConnectedNodes()
	candidates := make([]string, 0, 1+len(connected))
	candidates = append(candidates, myId)
	for _, n := range connected {
		candidates = append(candidates, n.NodeID)
	}
	selected := h.nodeRegistry.SelectByWeight(candidates)
	logger.Info("L2 candidates=%d selected=%s myId=%s peers=%d", len(candidates), shortId(selected), shortId(myId), len(connected))
	if selected != myId {
		if restURL := h.nodesManager.GetPeerRestURL(selected); restURL != "" {
			logger.Info("L2 forwarding to confirmer %s restURL=%s", shortId(selected), restURL)
			if h.forwardPostToNode(restURL, "/api/l2-confirm", selected, w) {
				return
			}
			logger.Warn("L2 forward failed, running locally")
		} else {
			logger.Warn("L2 selected %s but no RestURL, running locally", shortId(selected))
		}
	}
	h.l2ConfirmBlockRun(w, r)
}

// l2ConfirmBlockRun performs the L2 vote round and block confirm.
func (h *Handler) l2ConfirmBlockRun(w http.ResponseWriter, r *http.Request) {
	startL2 := time.Now()
	pending := h.blockchain.GetPendingBlock()
	if len(pending) == 0 {
		logger.Warn("L2 confirm rejected: no pending block (L1 collect first)")
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "no pending block (L1 collect first)"})
		return
	}
	myId := h.nodesManager.GetNodeID()
	connected := h.nodesManager.GetConnectedNodes()
	candidates := make([]string, 0, 1+len(connected))
	candidates = append(candidates, myId)
	for _, n := range connected {
		candidates = append(candidates, n.NodeID)
	}
	// blockId first for deterministic Core committee seed
	blockId := "l2-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	// Навантаження мережі = max load серед кандидатів; різний load → різна кількість підтверджень.
	loadPct := 0
	for _, id := range candidates {
		if n := h.nodeRegistry.Get(id); n != nil {
			pct := int(n.LoadScore * 100 / rating.ScoreScale)
			if pct > loadPct {
				loadPct = pct
			}
		}
	}
	numCandidates := len(candidates)
	var committeeSize int
	var selectionPercent int
	var committeeList []string
	if h.rustCore != nil {
		size, err := h.rustCore.CommitteeCount(numCandidates, loadPct)
		if err != nil {
			logger.Warn("L2 Core committee-count failed, using Go fallback: %v", err)
			selectionPercent = rating.SelectionPercentFromLoad(loadPct)
			committeeSize = (numCandidates * selectionPercent) / 100
			if committeeSize < 1 {
				committeeSize = 1
			}
			if numCandidates >= 3 && committeeSize < 3 {
				committeeSize = 3
			}
			if committeeSize > numCandidates {
				committeeSize = numCandidates
			}
			others := make([]string, 0, numCandidates-1)
			for _, id := range candidates {
				if id != myId {
					others = append(others, id)
				}
			}
			toSelect := committeeSize - 1
			if toSelect < 0 {
				toSelect = 0
			}
			if toSelect > len(others) {
				toSelect = len(others)
			}
			if toSelect > 0 {
				committeeList = h.nodeRegistry.SelectCommittee(others, toSelect)
			}
		} else {
			committeeSize = size
			if numCandidates > 0 {
				selectionPercent = committeeSize * 100 / numCandidates
			}
			others := make([]string, 0, numCandidates-1)
			for _, id := range candidates {
				if id != myId {
					others = append(others, id)
				}
			}
			toSelect := committeeSize - 1
			if toSelect < 0 {
				toSelect = 0
			}
			if toSelect > len(others) {
				toSelect = len(others)
			}
			if toSelect == 0 {
				committeeList = nil
			} else {
				weighted := make([]core.CommitteeCandidate, 0, len(others))
				for _, id := range others {
					w := h.nodeRegistry.SelectionWeightFor(id)
					if w <= 0 {
						w = 1
					}
					weighted = append(weighted, core.CommitteeCandidate{ID: id, Weight: w})
				}
				seed := sha256.Sum256([]byte(blockId))
				seedHex := hex.EncodeToString(seed[:])
				committeeList, err = h.rustCore.SelectCommittee(weighted, seedHex, toSelect)
				if err != nil {
					logger.Warn("L2 Core select-committee failed, using Go fallback: %v", err)
					committeeList = h.nodeRegistry.SelectCommittee(others, toSelect)
				}
			}
		}
	} else {
		selectionPercent = rating.SelectionPercentFromLoad(loadPct)
		committeeSize = (numCandidates * selectionPercent) / 100
		if committeeSize < 1 {
			committeeSize = 1
		}
		if numCandidates >= 3 && committeeSize < 3 {
			committeeSize = 3
		}
		if committeeSize > numCandidates {
			committeeSize = numCandidates
		}
		others := make([]string, 0, numCandidates-1)
		for _, id := range candidates {
			if id != myId {
				others = append(others, id)
			}
		}
		toSelect := committeeSize - 1
		if toSelect < 0 {
			toSelect = 0
		}
		if toSelect > len(others) {
			toSelect = len(others)
		}
		if toSelect > 0 {
			committeeList = h.nodeRegistry.SelectCommittee(others, toSelect)
		}
	}
	committeeSet := make(map[string]bool, committeeSize+1)
	committeeSet[myId] = true
	for _, id := range committeeList {
		committeeSet[id] = true
	}
	totalExpected := len(committeeSet)
	done := make(chan bool, 1)
	round := &l2VoteRound{
		blockId:       blockId,
		votes:         map[string]bool{myId: true},
		totalExpected: totalExpected,
		committee:     committeeSet,
		done:          done,
	}
	h.l2VoteRoundMu.Lock()
	h.l2VoteRound = round
	h.l2VoteRoundMu.Unlock()

	logger.Info("L2 committee: loadPct=%d selection%%=%d committee=%d (of %d)", loadPct, selectionPercent, totalExpected, len(candidates))

	go h.nodesManager.BroadcastBlockchainEvent("l2_proposal", map[string]interface{}{
		"blockId": blockId, "proposerNodeId": myId,
	}, myId)
	go h.nodesManager.BroadcastBlockchainEvent("l2_vote", map[string]interface{}{
		"blockId": blockId, "nodeId": myId, "yes": true,
	}, myId)

	// Single-node: no peers receive events (we don't deliver to self), so signal done immediately.
	if totalExpected == 1 {
		logger.Info("L2 single-node mode: no peers, accepting with own vote")
		round.mu.Lock()
		if !round.closed {
			round.closed = true
			select { case round.done <- true: default: }
		}
		round.mu.Unlock()
	}

	l2Timeout := voteRoundTimeout(totalExpected)
	logger.Info("L2 vote round timeout=%v (peers=%d)", l2Timeout, totalExpected-1)
	go func() {
		time.Sleep(l2Timeout)
		round.mu.Lock()
		if !round.closed {
			round.closed = true
			select { case round.done <- false: default: }
		}
		round.mu.Unlock()
	}()

	var ok bool
	select {
	case ok = <-done:
	case <-time.After(l2Timeout + time.Second):
		ok = false
	}

	needL2 := (totalExpected*L2VoteThresholdPct + 99) / 100
	yesCountL2 := 0
	for _, v := range round.votes {
		if v {
			yesCountL2++
		}
	}
	voterIdsL2 := make([]string, 0, len(round.votes))
	for id := range round.votes {
		voterIdsL2 = append(voterIdsL2, shortId(id))
	}
	logger.Info("L2 round result totalExpected=%d need=%d yes=%d total_votes=%d accepted=%v voters=%v", totalExpected, needL2, yesCountL2, len(round.votes), ok, voterIdsL2)
	// Fallback: if no peer voted, accept with proposer vote only (degraded mode)
	if !ok && totalExpected > 1 && len(round.votes) == 1 && round.votes[myId] {
		logger.Warn("L2 degraded: no peer votes (only confirmer), accepting")
		ok = true
	}

	h.l2VoteRoundMu.Lock()
	h.l2VoteRound = nil
	h.l2VoteRoundMu.Unlock()

	h.lastL1VotesMu.Lock()
	h.lastL2Votes = make(map[string]bool)
	for k, v := range round.votes {
		h.lastL2Votes[k] = v
	}
	h.lastL2Accepted = ok
	h.updateVoteStatsFromRound(round.votes, ok, round.committee)
	h.lastL1VotesMu.Unlock()

	// Broadcast vote result so all nodes (and any UI) see the same L2 votes
	go h.nodesManager.BroadcastBlockchainEvent("l2_vote_result", map[string]interface{}{
		"votes":   round.votes,
		"accepted": ok,
	}, myId)

	if !ok {
		logger.Error("L2 threshold not met yes=%d total=%d need=%d", yesCountL2, len(round.votes), needL2)
		jsonResponse(w, http.StatusBadRequest, map[string]interface{}{
			"error":   "L2 threshold not met (need 70% yes)",
			"yes":     yesCountL2,
			"total":   len(round.votes),
			"need":    needL2,
		})
		return
	}

	// Count L1 and L2 votes for analytics response.
	h.lastL1VotesMu.RLock()
	l1YesResp, l1NoResp := 0, 0
	for _, v := range h.lastL1Votes {
		if v {
			l1YesResp++
		} else {
			l1NoResp++
		}
	}
	h.lastL1VotesMu.RUnlock()
	l2YesResp, l2NoResp := 0, 0
	for _, v := range round.votes {
		if v {
			l2YesResp++
		} else {
			l2NoResp++
		}
	}

	moved, block := h.blockchain.L2ConfirmBlock()
	logger.Info("L2 block confirmed confirmer=%s blockNumber=%d moved=%d totalFees=%d", shortId(myId), block.BlockNumber, len(moved), block.TotalFees)
	if block.TotalFees > 0 {
		cfg := h.distributor.GetConfig()
		sr := cfg.SplitBlockFees(block.TotalFees)
		l1Pool, l2Pool := h.distributor.ApplyBlock(block.TotalFees)

		// Distribute L1 pool among L1 YES-voters, L2 pool among L2 YES-voters (weighted by reputation).
		h.lastL1VotesMu.RLock()
		l1VotersCopy := make(map[string]bool, len(h.lastL1Votes))
		for k, v := range h.lastL1Votes {
			l1VotersCopy[k] = v
		}
		h.lastL1VotesMu.RUnlock()

		l1Shares := h.distributePool(l1Pool, l1VotersCopy)
		l2Shares := h.distributePool(l2Pool, round.votes)
		h.recordEarnings(l1Shares, l2Shares)

		logger.Info("L2 fee distribution: l1Pool=%d (among %d L1 voters), l2Pool=%d (among %d L2 voters)",
			l1Pool, len(l1Shares), l2Pool, len(l2Shares))

		// Convert shares to interface maps for broadcasting.
		l1SharesIF := make(map[string]interface{}, len(l1Shares))
		for k, v := range l1Shares {
			l1SharesIF[k] = v
		}
		l2SharesIF := make(map[string]interface{}, len(l2Shares))
		for k, v := range l2Shares {
			l2SharesIF[k] = v
		}
		go h.nodesManager.BroadcastBlockchainEvent("fee_distribution", map[string]interface{}{
			"l1Shares":    l1SharesIF,
			"l2Shares":    l2SharesIF,
			"blockNumber": block.BlockNumber,
			"totalFees":   block.TotalFees,
			"burn":        sr.Burn,
			"treasury":    sr.Treasury,
		}, myId)
	}
	for _, tx := range moved {
		eventData := map[string]interface{}{"hash": tx.Hash, "from": tx.From, "to": tx.To, "value": tx.Value}
		h.wsServer.BroadcastEvent("transactionProcessed", eventData)
	}

	// Broadcast confirmed block to all peers so their chain stays in sync.
	txMaps := make([]interface{}, 0, len(moved))
	txHashStrs := make([]interface{}, 0, len(block.TxHashes))
	for _, tx := range moved {
		txMaps = append(txMaps, txToMap(tx))
	}
	for _, h := range block.TxHashes {
		txHashStrs = append(txHashStrs, h)
	}
	durationMs := int(time.Since(startL2).Milliseconds())
	h.pendingL1Mu.Lock()
	l1Ben := h.pendingL1Beneficiary
	h.pendingL1Mu.Unlock()
	h.blockchain.SetBlockVoteCounts(block.BlockNumber, l1YesResp, l1NoResp, l2YesResp, l2NoResp, durationMs, l1Ben, myId)
	h.lastL1VotesMu.RLock()
	l1VotesCopy := make(map[string]bool, len(h.lastL1Votes))
	for k, v := range h.lastL1Votes {
		l1VotesCopy[k] = v
	}
	h.lastL1VotesMu.RUnlock()
	l2VotesCopy := make(map[string]bool, len(round.votes))
	for k, v := range round.votes {
		l2VotesCopy[k] = v
	}
	h.blockchain.SetBlockVoteDetails(block.BlockNumber, l1VotesCopy, l2VotesCopy)
	l1VotesIF := make(map[string]interface{}, len(l1VotesCopy))
	for k, v := range l1VotesCopy {
		l1VotesIF[k] = v
	}
	l2VotesIF := make(map[string]interface{}, len(l2VotesCopy))
	for k, v := range l2VotesCopy {
		l2VotesIF[k] = v
	}
	go h.nodesManager.BroadcastBlockchainEvent("block_confirmed", map[string]interface{}{
		"blockNumber":           block.BlockNumber,
		"timestamp":             block.Timestamp,
		"txHashes":              txHashStrs,
		"txCount":               block.TxCount,
		"totalFees":             block.TotalFees,
		"transactions":          txMaps,
		"l1Yes":                 l1YesResp,
		"l1No":                  l1NoResp,
		"l2Yes":                 l2YesResp,
		"l2No":                  l2NoResp,
		"durationMs":            durationMs,
		"l1BeneficiaryNodeId":   l1Ben,
		"l2ConfirmerNodeId":     myId,
		"l1Votes":               l1VotesIF,
		"l2Votes":               l2VotesIF,
	}, myId)

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"block":   block,
		"moved":   len(moved),
		"l1Yes":   l1YesResp,
		"l1No":    l1NoResp,
		"l2Yes":   l2YesResp,
		"l2No":    l2NoResp,
		"message": "L2 підтвердив блок (70% так), TX у ланцюгу",
	})
}

// ConfirmBlock moves mempool to chain in one step (legacy)
func (h *Handler) ConfirmBlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
		return
	}
	moved, block := h.blockchain.ConfirmMempoolToChain()
	if block.TotalFees > 0 {
		h.distributor.ApplyBlock(block.TotalFees)
	}
	for _, tx := range moved {
		eventData := map[string]interface{}{"hash": tx.Hash, "from": tx.From, "to": tx.To, "value": tx.Value}
		h.wsServer.BroadcastEvent("transactionProcessed", eventData)
	}
	go func() {
		for _, tx := range moved {
			h.nodesManager.BroadcastBlockchainEvent("transactionProcessed", map[string]interface{}{
				"hash": tx.Hash, "from": tx.From, "to": tx.To, "value": tx.Value,
			}, "")
		}
	}()
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"block":   block,
		"moved":   len(moved),
		"message": "Блок підтверджено, TX у ланцюгу",
	})
}

// GetRewardConfig returns reward distribution config and cumulative burn/treasury (from Core-aligned distribution).
func (h *Handler) GetRewardConfig(w http.ResponseWriter, r *http.Request) {
	cfg := h.distributor.GetConfig()
	burned, treasury := h.distributor.Totals()
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"burnPct":     cfg.BurnPct,
		"treasuryPct": cfg.TreasuryPct,
		"l1Pct":       cfg.L1Pct,
		"l2Pct":       cfg.L2Pct,
		"totalBurned": burned,
		"totalTreasury": treasury,
	})
}

// RewardCreditL1 is called by another node that did L2 when we were the L1 proposer; adds amount to our L1 earnings.
func (h *Handler) RewardCreditL1(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
		return
	}
	var body struct {
		Amount int64 `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Amount <= 0 {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "invalid amount"})
		return
	}
	h.nodeEarnedMu.Lock()
	h.nodeEarnedL1 += body.Amount
	h.nodeEarnedMu.Unlock()
	jsonResponse(w, http.StatusOK, map[string]interface{}{"success": true})
}

// GetLastVotes returns the last L1 and L2 vote round result (real votes per node for UI).
func (h *Handler) GetLastVotes(w http.ResponseWriter, r *http.Request) {
	h.lastL1VotesMu.RLock()
	l1 := make(map[string]bool)
	for k, v := range h.lastL1Votes {
		l1[k] = v
	}
	l1Accepted := h.lastL1Accepted
	l2 := make(map[string]bool)
	for k, v := range h.lastL2Votes {
		l2[k] = v
	}
	l2Accepted := h.lastL2Accepted
	h.lastL1VotesMu.RUnlock()
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"l1": map[string]interface{}{"votes": l1, "accepted": l1Accepted},
		"l2": map[string]interface{}{"votes": l2, "accepted": l2Accepted},
	})
}

// GetFeeDistribution returns per-node cumulative L1/L2 earnings.
func (h *Handler) GetFeeDistribution(w http.ResponseWriter, r *http.Request) {
	thisID := h.nodesManager.GetNodeID()
	connected := h.nodesManager.GetConnectedNodes()
	h.allNodeEarningsMu.RLock()
	list := make([]map[string]interface{}, 0, len(h.allNodeEarnings)+len(connected)+1)
	seen := make(map[string]bool)
	for id, e := range h.allNodeEarnings {
		list = append(list, map[string]interface{}{"nodeId": id, "l1Earned": e[0], "l2Earned": e[1]})
		seen[id] = true
	}
	h.allNodeEarningsMu.RUnlock()
	if !seen[thisID] {
		list = append(list, map[string]interface{}{"nodeId": thisID, "l1Earned": int64(0), "l2Earned": int64(0)})
		seen[thisID] = true
	}
	for _, cn := range connected {
		if !seen[cn.NodeID] {
			list = append(list, map[string]interface{}{"nodeId": cn.NodeID, "l1Earned": int64(0), "l2Earned": int64(0)})
			seen[cn.NodeID] = true
		}
	}
	burned, treasury := h.distributor.Totals()
	cfg := h.distributor.GetConfig()
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"nodes":         list,
		"totalBurned":   burned,
		"totalTreasury": treasury,
		"burnPct":       cfg.BurnPct,
		"treasuryPct":   cfg.TreasuryPct,
		"l1Pct":         cfg.L1Pct,
		"l2Pct":         cfg.L2Pct,
	})
}

// TestSetLoad sets load for a node (тест навантаження мережі). POST body: { "nodeId": "optional", "currentTasks": 2, "maxCapacity": 10 }.
// Якщо nodeId не вказано - встановлюється для поточної ноди. Впливає на SelectionWeight = репутація×(1−load) (Core).
func (h *Handler) TestSetLoad(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
		return
	}
	var body struct {
		NodeID       string `json:"nodeId"`
		CurrentTasks int64  `json:"currentTasks"`
		MaxCapacity  int64  `json:"maxCapacity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	nodeID := body.NodeID
	if nodeID == "" {
		nodeID = h.nodesManager.GetNodeID()
	}
	h.nodeRegistry.EnsureNode(nodeID, 0, 1)
	h.nodeRegistry.SetLoad(nodeID, body.CurrentTasks, body.MaxCapacity)
	// Розсилаємо load пірам - щоб розмір комітету залежав від навантаження мережі (max load серед кандидатів).
	go h.nodesManager.BroadcastBlockchainEvent("node_load", map[string]interface{}{
		"nodeId": nodeID, "currentTasks": body.CurrentTasks, "maxCapacity": body.MaxCapacity,
	}, h.nodesManager.GetNodeID())
	n := h.nodeRegistry.Get(nodeID)
	if n == nil {
		jsonResponse(w, http.StatusOK, map[string]interface{}{"success": true, "message": "load set"})
		return
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"success":       true,
		"nodeId":        nodeID,
		"currentTasks":  n.CurrentTasks,
		"maxCapacity":   n.MaxCapacity,
		"loadScore":     n.LoadScore,
		"selectionWeight": n.SelectionWeight(),
		"message":       "Навантаження встановлено (вага вибору = репутація×(1−load))",
	})
}

// GetNodeRatings returns reputation/rating for all known nodes (Core formula: uptime 30%, latency 20%, vote 30%, stake 20%).
func (h *Handler) GetNodeRatings(w http.ResponseWriter, r *http.Request) {
	thisID := h.nodesManager.GetNodeID()
	connected := h.nodesManager.GetConnectedNodes()
	h.nodeRegistry.EnsureNode(thisID, 0, 1)
	for _, n := range connected {
		nodeID := n.NodeID
		if nodeID == "" {
			continue
		}
		h.nodeRegistry.EnsureNode(nodeID, 0, 1)
	}
	all := h.nodeRegistry.All()
	list := make([]map[string]interface{}, 0, len(all))
	for _, s := range all {
		list = append(list, map[string]interface{}{
			"nodeId":          s.NodeID,
			"uptimeScore":     s.UptimeScore,
			"latencyScore":    s.LatencyScore,
			"missedVotes":     s.MissedVotes,
			"totalVotes":      s.TotalVotes,
			"stake":           s.Stake,
			"reputationScore": s.ReputationScore,
			"loadScore":       s.LoadScore,
			"selectionWeight": s.SelectionWeight(),
			"voteAccuracy":   s.VoteAccuracy(),
		})
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{"ratings": list, "scale": rating.ScoreScale})
}

// PingPeer handles ping requests to peer nodes
func (h *Handler) PingPeer(w http.ResponseWriter, r *http.Request) {
	address := r.URL.Query().Get("address")
	if address == "" {
		jsonResponse(w, http.StatusBadRequest, map[string]string{
			"error": "address parameter is required",
		})
		return
	}

	// Simple ping implementation - measure connection time
	start := time.Now()
	
	// Try to connect to the peer's HTTP endpoint
	client := &http.Client{
		Timeout: 5 * time.Second,
	}
	
	// Convert WebSocket address to HTTP
	httpAddr := address
	if len(httpAddr) > 2 && httpAddr[:2] == "ws" {
		httpAddr = "http" + httpAddr[2:]
	}
	
	resp, err := client.Get(httpAddr + "/")
	duration := time.Since(start)
	
	var ping *int64
	if err == nil && resp != nil {
		resp.Body.Close()
		pingMs := duration.Milliseconds()
		ping = &pingMs
	}
	
	response := map[string]interface{}{
		"address": address,
		"ping":    ping,
	}
	
	jsonResponse(w, http.StatusOK, response)
}

