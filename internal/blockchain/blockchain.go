package blockchain

import (
	"errors"
	"sync"
)

// Transaction represents a blockchain transaction
type Transaction struct {
	Hash            string `json:"hash"`
	From            string `json:"from"`
	To              string `json:"to"`
	Value           string `json:"value"`
	Fee             string `json:"fee"`
	Nonce           int    `json:"nonce"`
	Timestamp       int64  `json:"timestamp"`
	Type            string `json:"type"`
	AssetType       string `json:"assetType"`
	ContractAddress string `json:"contractAddress,omitempty"`
}

// Blockchain represents the blockchain interface
type Blockchain struct {
	mu           sync.RWMutex
	transactions map[string]*Transaction
	balances     map[string]string
	addressTxs   map[string][]*Transaction
	lastTx       *Transaction
}

// NewBlockchain creates a new blockchain instance
func NewBlockchain() *Blockchain {
	return &Blockchain{
		transactions: make(map[string]*Transaction),
		balances:     make(map[string]string),
		addressTxs:   make(map[string][]*Transaction),
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

