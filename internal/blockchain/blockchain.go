package blockchain

import (
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"platarium-gateway-go/internal/core"
)

// Transaction represents a blockchain transaction.
// Core-signed TX: set SigMain, SigDerived, Asset, AmountUplp, FeeUplp (Value/Fee kept for display).
type Transaction struct {
	Hash            string   `json:"hash"`
	From            string   `json:"from"`
	To              string   `json:"to"`
	Value           string   `json:"value"`
	Fee             string   `json:"fee"`
	Nonce           int      `json:"nonce"`
	Timestamp       int64    `json:"timestamp"`
	Type            string   `json:"type"`
	AssetType       string   `json:"assetType"`
	ContractAddress string   `json:"contractAddress,omitempty"`
	// Core format (for real validation and signed demo TX)
	SigMain    string   `json:"sig_main,omitempty"`
	SigDerived string   `json:"sig_derived,omitempty"`
	PubMain    string   `json:"pub_main,omitempty"`
	PubDerived string   `json:"pub_derived,omitempty"`
	Asset     string   `json:"asset,omitempty"`      // "PLP" or "Token:XXX"
	AmountUplp uint64  `json:"amount,omitempty"`     // amount in minimal units
	FeeUplp   uint64  `json:"fee_uplp,omitempty"`   // fee in μPLP
	Reads     []string `json:"reads,omitempty"`
	Writes    []string `json:"writes,omitempty"`
}

// BlockRecord is a record for analytics (block number from 0, time, tx count, fees, L1/L2 votes, duration, miners).
type BlockRecord struct {
	BlockNumber           int64          `json:"blockNumber"`
	Timestamp             int64          `json:"timestamp"`
	TxHashes              []string       `json:"txHashes"`
	TxCount               int            `json:"txCount"`
	TotalFees             int64          `json:"totalFees"`
	BlockHash             string         `json:"blockHash,omitempty"`
	MerkleRoot            string         `json:"merkleRoot,omitempty"`
	StateRoot             string         `json:"stateRoot,omitempty"`
	PreviousHash          string         `json:"previousHash,omitempty"`
	ProducerNodeID        string         `json:"producerNodeId,omitempty"`
	L1Yes                 int            `json:"l1Yes,omitempty"`
	L1No                  int            `json:"l1No,omitempty"`
	L2Yes                 int            `json:"l2Yes,omitempty"`
	L2No                  int            `json:"l2No,omitempty"`
	DurationMs            int            `json:"durationMs,omitempty"`
	L1BeneficiaryNodeId   string         `json:"l1BeneficiaryNodeId,omitempty"`
	L2ConfirmerNodeId     string         `json:"l2ConfirmerNodeId,omitempty"`
	L1Votes               map[string]bool `json:"l1Votes,omitempty"` // nodeId -> voted yes (L1 consensus log)
	L2Votes               map[string]bool `json:"l2Votes,omitempty"` // nodeId -> voted yes (L2 consensus log)
}

// Blockchain represents the blockchain interface
type Blockchain struct {
	mu                  sync.RWMutex
	ledger              *core.LedgerService
	transactions        map[string]*Transaction
	mempool             []*Transaction
	pendingBlock        []*Transaction // L1 collected → awaiting L2 confirmation
	addressTxs          map[string][]*Transaction
	lastTx              *Transaction
	blockCounter        int64
	blockHistory        []BlockRecord
	totalFeesCollected  int64 // total fees from all confirmed TX
	chainFile           string
}

// NewBlockchain creates a new blockchain instance
func NewBlockchain() *Blockchain {
	return &Blockchain{
		transactions: make(map[string]*Transaction),
		mempool:      make([]*Transaction, 0),
		pendingBlock: make([]*Transaction, 0),
		addressTxs:   make(map[string][]*Transaction),
		blockHistory: make([]BlockRecord, 0),
	}
}

// SetLedger attaches the Core ledger service (authoritative balances).
func (bc *Blockchain) SetLedger(ledger *core.LedgerService) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	bc.ledger = ledger
}

// Ledger returns the attached Core ledger service.
func (bc *Blockchain) Ledger() *core.LedgerService {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.ledger
}

// Init initializes the blockchain
func (bc *Blockchain) Init() error {
	// Initialize with default values
	// In production, this would connect to the actual platarium-network
	return nil
}

// GetBalance returns PLP balance from Core state (confirmed only).
func (bc *Blockchain) GetBalance(address string) (string, error) {
	bc.mu.RLock()
	ledger := bc.ledger
	bc.mu.RUnlock()
	if ledger == nil {
		return "", fmt.Errorf("core ledger unavailable")
	}
	q, err := ledger.Query(address)
	if err != nil {
		return "", err
	}
	return q.Balance, nil
}

// GetAccountQuery returns full Core account state for an address.
func (bc *Blockchain) GetAccountQuery(address string) (*core.AccountQuery, error) {
	bc.mu.RLock()
	ledger := bc.ledger
	bc.mu.RUnlock()
	if ledger == nil {
		return nil, fmt.Errorf("core ledger unavailable")
	}
	return ledger.Query(address)
}

// GetTransaction returns a transaction by hash
func (bc *Blockchain) GetTransaction(hash string) *Transaction {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	
	return bc.transactions[hash]
}

// GetTransactionsByAddress returns all transactions for an address
func (bc *Blockchain) GetTransactionsByAddress(address string) []*Transaction {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	
	txs, exists := bc.addressTxs[address]
	if !exists {
		return []*Transaction{}
	}
	
	// Return a copy
	result := make([]*Transaction, len(txs))
	copy(result, txs)
	return result
}

// AddTransaction adds a new transaction to the blockchain
func (bc *Blockchain) AddTransaction(tx *Transaction) error {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	
	if tx.Hash == "" {
		return errors.New("transaction hash is required")
	}
	
	bc.transactions[tx.Hash] = tx
	bc.lastTx = tx
	
	// Add to address transactions
	bc.addressTxs[tx.From] = append(bc.addressTxs[tx.From], tx)
	bc.addressTxs[tx.To] = append(bc.addressTxs[tx.To], tx)
	
	return nil
}

// GetLastTransaction returns the last added transaction
func (bc *Blockchain) GetLastTransaction() *Transaction {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	
	return bc.lastTx
}

// AddToMempool adds a transaction to the mempool (pending, not yet in chain)
func (bc *Blockchain) AddToMempool(tx *Transaction) error {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	
	if tx.Hash == "" {
		return errors.New("transaction hash is required")
	}
	for _, t := range bc.mempool {
		if t.Hash == tx.Hash {
			return nil
		}
	}
	bc.mempool = append(bc.mempool, tx)
	return nil
}

// GetMempool returns a copy of pending transactions
func (bc *Blockchain) GetMempool() []*Transaction {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	
	out := make([]*Transaction, len(bc.mempool))
	copy(out, bc.mempool)
	return out
}

// RemoveFromMempool removes transactions by hash (after L1 collect sync from peer).
func (bc *Blockchain) RemoveFromMempool(hashes []string) {
	if len(hashes) == 0 {
		return
	}
	remove := make(map[string]bool, len(hashes))
	for _, h := range hashes {
		remove[h] = true
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	next := make([]*Transaction, 0, len(bc.mempool))
	for _, tx := range bc.mempool {
		if tx == nil || !remove[tx.Hash] {
			next = append(next, tx)
		}
	}
	bc.mempool = next
}

// SyncPendingBlock sets the pending block and removes those txs from mempool (multi-node L1 sync).
func (bc *Blockchain) SyncPendingBlock(txs []*Transaction) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	bc.pendingBlock = make([]*Transaction, 0, len(txs))
	hashes := make(map[string]bool, len(txs))
	for _, tx := range txs {
		if tx == nil {
			continue
		}
		bc.pendingBlock = append(bc.pendingBlock, tx)
		hashes[tx.Hash] = true
	}
	if len(hashes) == 0 {
		return
	}
	nextMempool := make([]*Transaction, 0, len(bc.mempool))
	for _, tx := range bc.mempool {
		if tx == nil || !hashes[tx.Hash] {
			nextMempool = append(nextMempool, tx)
		}
	}
	bc.mempool = nextMempool
}

// GetMempoolTxsByHashes returns mempool transactions matching hashes in order; false if any hash missing.
func (bc *Blockchain) GetMempoolTxsByHashes(hashes []string) ([]*Transaction, bool) {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	byHash := make(map[string]*Transaction, len(bc.mempool))
	for _, tx := range bc.mempool {
		if tx != nil && tx.Hash != "" {
			byHash[tx.Hash] = tx
		}
	}
	out := make([]*Transaction, 0, len(hashes))
	for _, hash := range hashes {
		tx, ok := byHash[hash]
		if !ok {
			return nil, false
		}
		out = append(out, tx)
	}
	return out, true
}

// PendingBlockMatchesHashes reports whether pending block has exactly the given hashes.
func (bc *Blockchain) PendingBlockMatchesHashes(hashes []string) bool {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	if len(hashes) != len(bc.pendingBlock) {
		return false
	}
	remaining := make(map[string]int, len(hashes))
	for _, h := range hashes {
		remaining[h]++
	}
	for _, tx := range bc.pendingBlock {
		if tx == nil {
			return false
		}
		remaining[tx.Hash]--
		if remaining[tx.Hash] < 0 {
			return false
		}
	}
	for _, count := range remaining {
		if count != 0 {
			return false
		}
	}
	return true
}

// L1CollectBlock moves all mempool TX into pending block (L1 collected, awaiting L2)
func (bc *Blockchain) L1CollectBlock() (moved []*Transaction) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	moved = make([]*Transaction, 0, len(bc.mempool))
	bc.pendingBlock = make([]*Transaction, 0, len(bc.mempool))
	for _, tx := range bc.mempool {
		bc.pendingBlock = append(bc.pendingBlock, tx)
		moved = append(moved, tx)
	}
	bc.mempool = bc.mempool[:0]
	return moved
}

// GetPendingBlock returns TX collected by L1 (block waiting for L2 confirm)
func (bc *Blockchain) GetPendingBlock() []*Transaction {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	out := make([]*Transaction, len(bc.pendingBlock))
	copy(out, bc.pendingBlock)
	return out
}

// SetPendingBlock replaces the pending block (used when syncing from L1 proposer so any node can run L2).
// Also removes those TX from mempool so they are not duplicated.
func (bc *Blockchain) SetPendingBlock(txs []*Transaction) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	hashes := make(map[string]bool)
	bc.pendingBlock = make([]*Transaction, 0, len(txs))
	for _, tx := range txs {
		if tx != nil {
			bc.pendingBlock = append(bc.pendingBlock, tx)
			hashes[tx.Hash] = true
		}
	}
	// Remove same TX from mempool so we don't have duplicates
	newMempool := make([]*Transaction, 0, len(bc.mempool))
	for _, tx := range bc.mempool {
		if !hashes[tx.Hash] {
			newMempool = append(newMempool, tx)
		}
	}
	bc.mempool = newMempool
}

func parseFee(fee string) int64 {
	if fee == "" {
		return 0
	}
	n, _ := strconv.ParseInt(fee, 10, 64)
	if n < 0 {
		return 0
	}
	return n
}

// applyConfirmedTransactions applies each transaction through Core ledger.
func (bc *Blockchain) applyConfirmedTransactions(txs []*Transaction) error {
	bc.mu.RLock()
	ledger := bc.ledger
	bc.mu.RUnlock()
	if ledger == nil {
		return fmt.Errorf("core ledger unavailable")
	}
	for _, tx := range txs {
		if tx == nil {
			continue
		}
		if tx.From == FaucetAddress {
			amt := parseAmount(tx)
			uplp := tx.FeeUplp
			if uplp == 0 {
				uplp = 10_000
			}
			if err := ledger.Credit(tx.To, amt, uplp); err != nil {
				return fmt.Errorf("faucet credit %s: %w", tx.Hash, err)
			}
			continue
		}
		coreJSON, ok := ToCoreJSON(tx)
		if !ok {
			return fmt.Errorf("transaction %s is not Core-compatible", tx.Hash)
		}
		if _, err := ledger.ApplyTx(coreJSON); err != nil {
			return fmt.Errorf("apply tx %s: %w", tx.Hash, err)
		}
	}
	return nil
}

// L2ConfirmBlock moves pending block into chain (L2 confirmed), applying state via Core.
func (bc *Blockchain) L2ConfirmBlock() (moved []*Transaction, block BlockRecord, err error) {
	bc.mu.Lock()
	pendingCopy := make([]*Transaction, len(bc.pendingBlock))
	copy(pendingCopy, bc.pendingBlock)
	bc.mu.Unlock()

	if err := bc.applyConfirmedTransactions(pendingCopy); err != nil {
		return nil, BlockRecord{}, err
	}

	bc.mu.Lock()
	defer bc.mu.Unlock()
	block = BlockRecord{
		BlockNumber: bc.blockCounter,
		Timestamp:   0,
		TxHashes:    make([]string, 0),
		TxCount:     0,
		TotalFees:   0,
	}
	bc.blockCounter++
	moved = make([]*Transaction, 0, len(bc.pendingBlock))
	for _, tx := range bc.pendingBlock {
		fee := parseFee(tx.Fee)
		if fee == 0 && tx.FeeUplp > 0 {
			fee = int64(tx.FeeUplp)
		}
		block.TotalFees += fee
		bc.totalFeesCollected += fee
		bc.transactions[tx.Hash] = tx
		bc.lastTx = tx
		bc.addressTxs[tx.From] = append(bc.addressTxs[tx.From], tx)
		bc.addressTxs[tx.To] = append(bc.addressTxs[tx.To], tx)
		block.TxHashes = append(block.TxHashes, tx.Hash)
		block.TxCount++
		if block.Timestamp == 0 || tx.Timestamp > 0 {
			block.Timestamp = tx.Timestamp
		}
		moved = append(moved, tx)
	}
	bc.pendingBlock = bc.pendingBlock[:0]
	if block.Timestamp == 0 {
		if bc.lastTx != nil {
			block.Timestamp = bc.lastTx.Timestamp
		} else {
			block.Timestamp = time.Now().Unix()
		}
	}
	bc.blockHistory = append(bc.blockHistory, block)
	if err := bc.persistChain(); err != nil {
		return moved, block, err
	}
	return moved, block, nil
}

// ConfirmMempoolToChain moves all mempool transactions into the chain (legacy: one step)
func (bc *Blockchain) ConfirmMempoolToChain() (moved []*Transaction, block BlockRecord, err error) {
	bc.mu.Lock()
	mempoolCopy := make([]*Transaction, len(bc.mempool))
	copy(mempoolCopy, bc.mempool)
	bc.mu.Unlock()

	if err := bc.applyConfirmedTransactions(mempoolCopy); err != nil {
		return nil, BlockRecord{}, err
	}

	bc.mu.Lock()
	defer bc.mu.Unlock()
	block = BlockRecord{
		BlockNumber: bc.blockCounter,
		Timestamp:   0,
		TxHashes:    make([]string, 0),
		TxCount:     0,
		TotalFees:   0,
	}
	bc.blockCounter++
	moved = make([]*Transaction, 0, len(bc.mempool))
	for _, tx := range bc.mempool {
		fee := parseFee(tx.Fee)
		block.TotalFees += fee
		bc.totalFeesCollected += fee
		bc.transactions[tx.Hash] = tx
		bc.lastTx = tx
		bc.addressTxs[tx.From] = append(bc.addressTxs[tx.From], tx)
		bc.addressTxs[tx.To] = append(bc.addressTxs[tx.To], tx)
		block.TxHashes = append(block.TxHashes, tx.Hash)
		block.TxCount++
		if block.Timestamp == 0 || tx.Timestamp > 0 {
			block.Timestamp = tx.Timestamp
		}
		moved = append(moved, tx)
	}
	bc.mempool = bc.mempool[:0]
	if block.Timestamp == 0 {
		if bc.lastTx != nil {
			block.Timestamp = bc.lastTx.Timestamp
		} else {
			block.Timestamp = time.Now().Unix()
		}
	}
	bc.blockHistory = append(bc.blockHistory, block)
	if err := bc.persistChain(); err != nil {
		return moved, block, err
	}
	return moved, block, nil
}

// AddConfirmedBlock adds a block received from a peer. Returns false if the block is already known.
func (bc *Blockchain) AddConfirmedBlock(block BlockRecord, txs []*Transaction) (bool, error) {
	bc.mu.Lock()
	for _, b := range bc.blockHistory {
		if b.BlockNumber == block.BlockNumber {
			bc.mu.Unlock()
			return false, nil
		}
	}
	bc.mu.Unlock()

	if err := bc.applyConfirmedTransactions(txs); err != nil {
		return false, err
	}

	bc.mu.Lock()
	defer bc.mu.Unlock()

	if block.BlockNumber >= 0 && block.BlockNumber+1 > bc.blockCounter {
		bc.blockCounter = block.BlockNumber + 1
	}

	txHashes := make(map[string]bool)
	for _, tx := range txs {
		if tx == nil {
			continue
		}
		fee := parseFee(tx.Fee)
		if fee == 0 && tx.FeeUplp > 0 {
			fee = int64(tx.FeeUplp)
		}
		bc.transactions[tx.Hash] = tx
		bc.lastTx = tx
		bc.addressTxs[tx.From] = append(bc.addressTxs[tx.From], tx)
		bc.addressTxs[tx.To] = append(bc.addressTxs[tx.To], tx)
		txHashes[tx.Hash] = true
		bc.totalFeesCollected += fee
	}
	newMempool := make([]*Transaction, 0, len(bc.mempool))
	for _, tx := range bc.mempool {
		if !txHashes[tx.Hash] {
			newMempool = append(newMempool, tx)
		}
	}
	bc.mempool = newMempool
	newPending := make([]*Transaction, 0, len(bc.pendingBlock))
	for _, tx := range bc.pendingBlock {
		if !txHashes[tx.Hash] {
			newPending = append(newPending, tx)
		}
	}
	bc.pendingBlock = newPending

	bc.blockHistory = append(bc.blockHistory, block)
	if err := bc.persistChain(); err != nil {
		return false, err
	}
	return true, nil
}

// GetAllTransactions returns all transactions in chain (order: oldest first by timestamp)
func (bc *Blockchain) GetAllTransactions() []*Transaction {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	
	out := make([]*Transaction, 0, len(bc.transactions))
	for _, tx := range bc.transactions {
		out = append(out, tx)
	}
	// sort by timestamp
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Timestamp < out[i].Timestamp {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// SetBlockVoteCounts sets L1/L2 vote counts, duration and miners for a block (for analytics; call after L2 confirm).
func (bc *Blockchain) SetBlockVoteCounts(blockNumber int64, l1Yes, l1No, l2Yes, l2No, durationMs int, l1BeneficiaryNodeId, l2ConfirmerNodeId string) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	for i := range bc.blockHistory {
		if bc.blockHistory[i].BlockNumber == blockNumber {
			bc.blockHistory[i].L1Yes = l1Yes
			bc.blockHistory[i].L1No = l1No
			bc.blockHistory[i].L2Yes = l2Yes
			bc.blockHistory[i].L2No = l2No
			bc.blockHistory[i].DurationMs = durationMs
			bc.blockHistory[i].L1BeneficiaryNodeId = l1BeneficiaryNodeId
			bc.blockHistory[i].L2ConfirmerNodeId = l2ConfirmerNodeId
			return
		}
	}
}

// SetBlockVoteDetails stores per-node L1/L2 votes for a block (consensus log).
func (bc *Blockchain) SetBlockVoteDetails(blockNumber int64, l1Votes, l2Votes map[string]bool) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	for i := range bc.blockHistory {
		if bc.blockHistory[i].BlockNumber == blockNumber {
			if len(l1Votes) > 0 {
				bc.blockHistory[i].L1Votes = make(map[string]bool, len(l1Votes))
				for k, v := range l1Votes {
					bc.blockHistory[i].L1Votes[k] = v
				}
			}
			if len(l2Votes) > 0 {
				bc.blockHistory[i].L2Votes = make(map[string]bool, len(l2Votes))
				for k, v := range l2Votes {
					bc.blockHistory[i].L2Votes[k] = v
				}
			}
			return
		}
	}
}

// parseAmount returns amount in μPLP from tx Value or AmountUplp.
func parseAmount(tx *Transaction) uint64 {
	if tx.AmountUplp > 0 {
		return tx.AmountUplp
	}
	if tx.Value != "" {
		if n, err := strconv.ParseUint(tx.Value, 10, 64); err == nil {
			return n
		}
	}
	return 0
}

// FaucetAddress is the sender address for faucet transactions.
const FaucetAddress = "faucet"

// MelancholyFaucetAmountPLP is the default testnet faucet drip (whole PLP units).
const MelancholyFaucetAmountPLP = 5000

// InstantFaucetCredit applies testnet PLP directly via Core state-credit (no consensus).
func (bc *Blockchain) InstantFaucetCredit(to string, plp uint64) (*Transaction, error) {
	if plp == 0 {
		return nil, fmt.Errorf("faucet amount must be positive")
	}
	bc.mu.RLock()
	ledger := bc.ledger
	bc.mu.RUnlock()
	if ledger == nil {
		return nil, fmt.Errorf("core ledger unavailable")
	}
	if err := ledger.Credit(to, plp, 0); err != nil {
		return nil, err
	}
	tx := &Transaction{
		Hash:       fmt.Sprintf("faucet-%d-%s", time.Now().UnixNano(), to),
		From:       FaucetAddress,
		To:         to,
		Value:      strconv.FormatUint(plp, 10),
		Fee:        "0",
		Nonce:      0,
		Timestamp:  time.Now().Unix(),
		Type:       "faucet",
		AssetType:  "native",
		AmountUplp: plp,
		FeeUplp:    0,
	}
	if err := bc.AddTransaction(tx); err != nil {
		return tx, err
	}
	return tx, nil
}

const genesisPreviousHash = "0000000000000000000000000000000000000000000000000000000000000000"

// GetPreviousBlockHash returns the hash of the last confirmed block, or genesis hash.
func (bc *Blockchain) GetPreviousBlockHash() string {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	if len(bc.blockHistory) == 0 {
		return genesisPreviousHash
	}
	last := bc.blockHistory[len(bc.blockHistory)-1]
	if last.BlockHash != "" {
		return last.BlockHash
	}
	return genesisPreviousHash
}

// NextBlockNumber returns the block number for the next confirmed block.
func (bc *Blockchain) NextBlockNumber() int64 {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.blockCounter
}

// ApplyBlockHeader sets Core-assembled header fields on the last block in history.
func (bc *Blockchain) ApplyBlockHeader(blockNumber int64, header core.BlockHeader, producerNodeID string) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	for i := range bc.blockHistory {
		if bc.blockHistory[i].BlockNumber == blockNumber {
			bc.blockHistory[i].BlockHash = header.BlockHash
			bc.blockHistory[i].MerkleRoot = header.MerkleRoot
			bc.blockHistory[i].StateRoot = header.StateRoot
			bc.blockHistory[i].PreviousHash = header.PreviousHash
			if producerNodeID != "" {
				bc.blockHistory[i].ProducerNodeID = producerNodeID
			}
			return
		}
	}
}

// GetBlockHistory returns block records for analytics (with L1/L2 votes when available).
func (bc *Blockchain) GetBlockHistory() []BlockRecord {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	
	out := make([]BlockRecord, len(bc.blockHistory))
	copy(out, bc.blockHistory)
	return out
}

// GetBlockByNumber returns a copy of the block record for the given block number, or nil if not found.
func (bc *Blockchain) GetBlockByNumber(blockNumber int64) *BlockRecord {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	for i := range bc.blockHistory {
		if bc.blockHistory[i].BlockNumber == blockNumber {
			b := bc.blockHistory[i]
			// Return pointer to heap copy so caller keeps valid reference
			out := new(BlockRecord)
			*out = b
			if len(b.TxHashes) > 0 {
				out.TxHashes = make([]string, len(b.TxHashes))
				copy(out.TxHashes, b.TxHashes)
			}
			return out
		}
	}
	return nil
}

// ChainStats holds aggregate stats for analytics
type ChainStats struct {
	ChainTxCount   int   `json:"chainTxCount"`
	MempoolCount   int   `json:"mempoolCount"`
	PendingCount   int   `json:"pendingCount"`
	TotalFees      int64 `json:"totalFees"`
	LastBlockNum   int64 `json:"lastBlockNumber"`
}

// GetStats returns current chain/mempool/pending stats and total fees
func (bc *Blockchain) GetStats() ChainStats {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	
	lastNum := int64(0)
	if bc.blockCounter > 0 {
		lastNum = bc.blockCounter - 1
	}
	st := ChainStats{
		ChainTxCount: len(bc.transactions),
		MempoolCount: len(bc.mempool),
		PendingCount: len(bc.pendingBlock),
		TotalFees:    bc.totalFeesCollected,
		LastBlockNum: lastNum,
	}
	return st
}