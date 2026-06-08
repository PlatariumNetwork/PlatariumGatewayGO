package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"platarium-gateway-go/internal/blockchain"
	"platarium-gateway-go/internal/core"
	"platarium-gateway-go/internal/logger"
	"platarium-gateway-go/internal/rating"
)

// computeVoteBlockID returns a deterministic proposal ID from chain inputs (no time-only IDs).
func computeVoteBlockID(phase string, blockNumber int64, txHashes []string, stateRoot string) string {
	sorted := append([]string(nil), txHashes...)
	sort.Strings(sorted)
	payload := fmt.Sprintf("%s:%d:%s:%s", phase, blockNumber, strings.Join(sorted, ","), stateRoot)
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func txHashesFromTransactions(txs []*blockchain.Transaction) []string {
	out := make([]string, 0, len(txs))
	for _, tx := range txs {
		if tx != nil && tx.Hash != "" {
			out = append(out, tx.Hash)
		}
	}
	sort.Strings(out)
	return out
}

func txsToCoreJSONArray(txs []*blockchain.Transaction) (string, bool) {
	items := make([]string, 0, len(txs))
	for _, tx := range txs {
		if tx == nil {
			continue
		}
		if tx.From == blockchain.FaucetAddress {
			continue
		}
		coreJSON, ok := txToCoreJSON(tx)
		if !ok {
			return "", false
		}
		items = append(items, coreJSON)
	}
	if len(items) == 0 {
		return "[]", true
	}
	b, err := json.Marshal(items)
	if err != nil {
		return "", false
	}
	return string(b), true
}

// validateTxsForL1 verifies transactions against Core state (L1 confirmation layer).
func (h *Handler) validateTxsForL1(txs []*blockchain.Transaction) (bool, error) {
	if len(txs) == 0 {
		return false, fmt.Errorf("empty transaction set")
	}
	ledger := h.blockchain.Ledger()
	if ledger == nil {
		if h.testnet {
			return false, fmt.Errorf("core ledger unavailable")
		}
		return true, nil
	}
	txsJSON, ok := txsToCoreJSONArray(txs)
	if !ok {
		return false, fmt.Errorf("transactions not Core-compatible")
	}
	if txsJSON == "[]" {
		return true, nil
	}
	if h.rustCore != nil {
		res, err := h.rustCore.L1VerifyTxs(ledger.StateFilePath(), txsJSON)
		if err != nil {
			return false, err
		}
		for _, tr := range res.TxResults {
			if !tr.Valid {
				return false, fmt.Errorf("L1 verification failed for tx %s", tr.Hash)
			}
		}
		return true, nil
	}
	for _, tx := range txs {
		if tx == nil || tx.From == blockchain.FaucetAddress {
			continue
		}
		coreJSON, ok := txToCoreJSON(tx)
		if !ok {
			return false, fmt.Errorf("transaction %s not Core-compatible", tx.Hash)
		}
		if _, err := ledger.ValidateTx(coreJSON); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (h *Handler) assembleBlockHeader(blockNumber int64, txs []*blockchain.Transaction, producerNodeID string, timestamp int64) (*core.BlockHeader, error) {
	ledger := h.blockchain.Ledger()
	if ledger == nil || h.rustCore == nil {
		return nil, fmt.Errorf("core ledger unavailable")
	}
	txHashes := txHashesFromTransactions(txs)
	hashesJSON, err := json.Marshal(txHashes)
	if err != nil {
		return nil, err
	}
	prevHash := h.blockchain.GetPreviousBlockHash()
	return h.rustCore.AssembleBlock(
		ledger.StateFilePath(),
		uint64(blockNumber),
		prevHash,
		timestamp,
		string(hashesJSON),
		producerNodeID,
	)
}

func stringSliceToInterface(items []string) []interface{} {
	out := make([]interface{}, len(items))
	for i, s := range items {
		out[i] = s
	}
	return out
}

func parseStringSlice(raw interface{}) []string {
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func (h *Handler) resolveMempoolTxsForProposal(txCount int, txHashes []string) ([]*blockchain.Transaction, bool) {
	if len(txHashes) > 0 {
		txs, ok := h.blockchain.GetMempoolTxsByHashes(txHashes)
		return txs, ok
	}
	mempool := h.blockchain.GetMempool()
	if len(mempool) != txCount {
		return nil, false
	}
	return mempool, true
}

func (h *Handler) submitL1Vote(blockId, proposerNodeId string, txCount int, txHashes []string) {
	myId := h.nodesManager.GetNodeID()
	const maxAttempts = 8
	const retryDelay = 250 * time.Millisecond
	var yes bool
	var resolved bool
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(retryDelay)
		}
		txs, ok := h.resolveMempoolTxsForProposal(txCount, txHashes)
		if !ok || len(txs) == 0 {
			continue
		}
		valid, err := h.validateTxsForL1(txs)
		if err != nil || !valid {
			logger.Info("L1 proposal validation failed from %s: %v", shortId(proposerNodeId), err)
			yes = false
			resolved = true
			break
		}
		yes = true
		resolved = true
		break
	}
	if !resolved {
		logger.Info("L1 proposal from %s: mempool not ready (txCount=%d hashes=%d)", shortId(proposerNodeId), txCount, len(txHashes))
	}
	logger.Info("L1 received proposal from %s (txCount=%d), sending vote yes=%v", shortId(proposerNodeId), txCount, yes)
	go h.nodesManager.BroadcastBlockchainEvent("l1_vote", map[string]interface{}{
		"blockId": blockId, "nodeId": myId, "yes": yes,
	}, myId)
}

func (h *Handler) submitL2Vote(blockId, proposerNodeId string, txHashes []string) {
	myId := h.nodesManager.GetNodeID()
	const maxAttempts = 8
	const retryDelay = 250 * time.Millisecond
	var yes bool
	var resolved bool
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(retryDelay)
		}
		pending := h.blockchain.GetPendingBlock()
		if len(txHashes) > 0 && !h.blockchain.PendingBlockMatchesHashes(txHashes) {
			continue
		}
		if len(pending) == 0 {
			continue
		}
		valid, err := h.validateTxsForL1(pending)
		if err != nil || !valid {
			logger.Info("L2 block validation failed from %s: %v", shortId(proposerNodeId), err)
			yes = false
			resolved = true
			break
		}
		yes = true
		resolved = true
		break
	}
	if !resolved {
		logger.Info("L2 proposal from %s: pending block not ready (hashes=%d)", shortId(proposerNodeId), len(txHashes))
	}
	logger.Info("L2 received proposal from %s blockId=%s..., sending vote yes=%v", shortId(proposerNodeId), shortId(blockId), yes)
	go h.nodesManager.BroadcastBlockchainEvent("l2_vote", map[string]interface{}{
		"blockId": blockId, "nodeId": myId, "yes": yes,
	}, myId)
}

func votesToCoreJSON(votes map[string]bool) (string, error) {
	type voteEntry struct {
		NodeID string `json:"node_id"`
		Yes    bool   `json:"yes"`
	}
	entries := make([]voteEntry, 0, len(votes))
	for id, yes := range votes {
		entries = append(entries, voteEntry{NodeID: id, Yes: yes})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].NodeID < entries[j].NodeID })
	b, err := json.Marshal(entries)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func allowDegradedConsensus() bool {
	v := os.Getenv("PLATARIUM_ALLOW_DEGRADED_CONSENSUS")
	if v == "" {
		return true
	}
	return strings.EqualFold(v, "1") || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

// finalizeVoteRoundWithCore uses Core vote aggregation when available.
func (h *Handler) finalizeVoteRoundWithCore(votes map[string]bool, isL1 bool, timeoutAccepted bool) (accepted bool, toPenalize []string) {
	accepted = timeoutAccepted
	if h.rustCore == nil || len(votes) == 0 {
		return accepted, nil
	}
	votesJSON, err := votesToCoreJSON(votes)
	if err != nil {
		return accepted, nil
	}
	var res *core.VoteResult
	if isL1 {
		res, err = h.rustCore.L1ProcessVotes(votesJSON)
	} else {
		res, err = h.rustCore.L2ProcessVotes(votesJSON)
	}
	if err != nil || res == nil {
		logger.Warn("Core process-votes failed, using timeout result: %v", err)
		return accepted, nil
	}
	return res.Confirmed, res.ToPenalize
}

func (h *Handler) applyVoteSlashing(votes map[string]bool, toPenalize []string, committee map[string]bool) {
	if len(toPenalize) > 0 {
		h.nodeRegistry.ApplySlashBatch(toPenalize, rating.SlashAgainstMajority)
		for _, id := range toPenalize {
			logger.Info("slash node=%s reason=%s", shortId(id), rating.ReasonName(rating.SlashAgainstMajority))
		}
	}
	if committee == nil {
		return
	}
	var noVote []string
	for id := range committee {
		if _, voted := votes[id]; !voted {
			noVote = append(noVote, id)
		}
	}
	if len(noVote) == 0 {
		return
	}
	h.nodeRegistry.ApplySlashBatch(noVote, rating.SlashNoVote)
	for _, id := range noVote {
		logger.Info("slash node=%s reason=%s", shortId(id), rating.ReasonName(rating.SlashNoVote))
	}
}
