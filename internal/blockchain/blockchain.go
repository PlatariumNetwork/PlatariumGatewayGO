package blockchain

import (
	"errors"
	"strconv"
	"sync"
	"time"
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
	SigMain   string   `json:"sig_main,omitempty"`
	SigDerived string  `json:"sig_derived,omitempty"`
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
	L1Yes                 int            `json:"l1Yes,omitempty"`
	L1No                  int            `json:"l1No,omitempty"`
	L2Yes                 int            `json:"l2Yes,omitempty"`
	L2No                  int            `json:"l2No,omitempty"`
	DurationMs            int            `json:"durationMs,omitempty"`
	L1BeneficiaryNodeId   string         `json:"l1BeneficiaryNodeId,omitempty"`
	L2ConfirmerNodeId     string         `json:"l2ConfirmerNodeId,omitempty"`
	L1Votes               map[string]bool `json:"l1Votes,omitempty"` // nodeId -> voted yes (лог консенсусу L1)
	L2Votes               map[string]bool `json:"l2Votes,omitempty"` // nodeId -> voted yes (лог консенсусу L2)
}

// Blockchain represents the blockchain interface
type Blockchain struct {
	mu                  sync.RWMutex
	transactions        map[string]*Transaction
	mempool             []*Transaction
	pendingBlock        []*Transaction // L1 зібрав → чекає підтвердження L2
	balances            map[string]string
	addressTxs          map[string][]*Transaction
	lastTx              *Transaction
	blockCounter        int64
	blockHistory        []BlockRecord
	totalFeesCollected  int64 // сума комісій з усіх підтверджених TX
}

// NewBlockchain creates a new blockchain instance
func NewBlockchain() *Blockchain {
	return &Blockchain{
		transactions: make(map[string]*Transaction),
		mempool:      make([]*Transaction, 0),
		pendingBlock: make([]*Transaction, 0),
		balances:     make(map[string]string),
		addressTxs:   make(map[string][]*Transaction),
		blockHistory: make([]BlockRecord, 0),
	}
}

// Init initializes the blockchain
func (bc *Blockchain) Init() error {
	// Initialize with default values
	// In production, this would connect to the actual platarium-network
	return nil
}

// GetBalance returns the balance for an address
func (bc *Blockchain) GetBalance(address string) string {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	
	balance, exists := bc.balances[address]
	if !exists {
		return "0"
	}
	return balance
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
}// L1CollectBlock moves all mempool TX into pending block (L1 зібрав блок, чекає L2)
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

// L2ConfirmBlock moves pending block into chain (L2 підтвердив блок), знімає комісію
func (bc *Blockchain) L2ConfirmBlock() (moved []*Transaction, block BlockRecord) {
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
		amt := parseAmount(tx)
		if amt == 0 {
			amt = uint64(parseFee(tx.Value))
		}
		bc.applyTransferLocked(tx.From, tx.To, amt, uint64(fee))
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
	return moved, block
}

// ConfirmMempoolToChain moves all mempool transactions into the chain (legacy: one step)
func (bc *Blockchain) ConfirmMempoolToChain() (moved []*Transaction, block BlockRecord) {
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
	return moved, block
}

// AddConfirmedBlock adds a block received from a peer. Returns false if the block is already known.
// Updates blockCounter, removes synced TX from mempool/pendingBlock.
func (bc *Blockchain) AddConfirmedBlock(block BlockRecord, txs []*Transaction) bool {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	for _, b := range bc.blockHistory {
		if b.BlockNumber == block.BlockNumber {
			return false
		}
	}

	if block.BlockNumber >= 0 && block.BlockNumber+1 > bc.blockCounter {
		bc.blockCounter = block.BlockNumber + 1
	}

	txHashes := make(map[string]bool)
	for _, tx := range txs {
		if tx == nil {
			continue
		}
		amt := parseAmount(tx)
		if amt == 0 {
			amt = uint64(parseFee(tx.Value))
		}
		fee := parseFee(tx.Fee)
		if fee == 0 && tx.FeeUplp > 0 {
			fee = int64(tx.FeeUplp)
		}
		bc.applyTransferLockedFromSync(tx.From, tx.To, amt, uint64(fee))
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
	return true
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

// CreditBalance adds amount to address balance (faucet / testnet). Caller must not hold bc.mu.
func (bc *Blockchain) CreditBalance(address string, amount uint64) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	bc.creditBalanceLocked(address, amount)
}

func (bc *Blockchain) creditBalanceLocked(address string, amount uint64) {
	current := uint64(0)
	if s, ok := bc.balances[address]; ok && s != "" {
		current, _ = strconv.ParseUint(s, 10, 64)
	}
	bc.balances[address] = strconv.FormatUint(current+amount, 10)
}// FaucetAddress is the sender address for faucet (kran) transactions. No deduction, only credit to To.
const FaucetAddress = "faucet"

// applyTransferLocked applies a confirmed TX: deduct amount+fee from From, add amount to To. Caller must hold bc.mu.
// For FaucetAddress: тільки зарахування на To (без списання).
// If sender has insufficient balance, returns without changing state (used for local L2 confirm).
func (bc *Blockchain) applyTransferLocked(from, to string, amount, fee uint64) {
	if from == FaucetAddress {
		currentTo := uint64(0)
		if s, ok := bc.balances[to]; ok && s != "" {
			currentTo, _ = strconv.ParseUint(s, 10, 64)
		}
		bc.balances[to] = strconv.FormatUint(currentTo+amount, 10)
		return
	}
	currentFrom := uint64(0)
	if s, ok := bc.balances[from]; ok && s != "" {
		currentFrom, _ = strconv.ParseUint(s, 10, 64)
	}
	if currentFrom < amount+fee {
		return
	}
	bc.balances[from] = strconv.FormatUint(currentFrom-amount-fee, 10)
	currentTo := uint64(0)
	if s, ok := bc.balances[to]; ok && s != "" {
		currentTo, _ = strconv.ParseUint(s, 10, 64)
	}
	bc.balances[to] = strconv.FormatUint(currentTo+amount, 10)
}

// applyTransferLockedFromSync applies a TX from a block received from a peer. Block was already
// validated by the network, so we always credit To and debit From so local state matches the chain.
// If From has insufficient balance on this node (e.g. missed earlier blocks), we still credit To
// and set From to 0 so balances stay consistent with confirmed history.
func (bc *Blockchain) applyTransferLockedFromSync(from, to string, amount, fee uint64) {
	if from == FaucetAddress {
		currentTo := uint64(0)
		if s, ok := bc.balances[to]; ok && s != "" {
			currentTo, _ = strconv.ParseUint(s, 10, 64)
		}
		bc.balances[to] = strconv.FormatUint(currentTo+amount, 10)
		return
	}
	currentFrom := uint64(0)
	if s, ok := bc.balances[from]; ok && s != "" {
		currentFrom, _ = strconv.ParseUint(s, 10, 64)
	}
	deduct := amount + fee
	if currentFrom < deduct {
		deduct = currentFrom
	}
	bc.balances[from] = strconv.FormatUint(currentFrom-deduct, 10)
	currentTo := uint64(0)
	if s, ok := bc.balances[to]; ok && s != "" {
		currentTo, _ = strconv.ParseUint(s, 10, 64)
	}
	bc.balances[to] = strconv.FormatUint(currentTo+amount, 10)
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
