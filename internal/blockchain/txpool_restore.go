package blockchain

import (
	"encoding/json"

	"platarium-gateway-go/internal/logger"
	"platarium-gateway-go/internal/txpool"
)

// SetTxPool attaches the durable pool. Nil disables it.
func (bc *Blockchain) SetTxPool(pool *txpool.Store) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	bc.txPool = pool
}

// SaveAdmittedTx writes an accepted transaction into the durable pool.
// Confirmed chain transactions are not stored there.
func (bc *Blockchain) SaveAdmittedTx(tx *Transaction) {
	if tx == nil || tx.Hash == "" {
		return
	}
	bc.mu.RLock()
	pool := bc.txPool
	confirmed := bc.confirmedHashes[tx.Hash]
	bc.mu.RUnlock()
	if pool == nil || confirmed {
		return
	}
	raw, err := json.Marshal(tx)
	if err != nil {
		logger.Warn("tx pool marshal %s: %v", tx.Hash, err)
		return
	}
	if err := pool.Put(tx.Hash, raw); err != nil {
		logger.Warn("tx pool save %s: %v", tx.Hash, err)
	}
}

// ForgetTxPool drops hashes that are now inside a committed block.
func (bc *Blockchain) ForgetTxPool(hashes []string) {
	bc.mu.RLock()
	pool := bc.txPool
	bc.mu.RUnlock()
	if pool == nil {
		return
	}
	for _, hash := range hashes {
		if err := pool.Delete(hash); err != nil {
			logger.Warn("tx pool delete %s: %v", hash, err)
		}
	}
}

// RestoreDurableTxs loads the pool and any indexed transaction that is not
// inside a block at or below the Rocks head. Those rows go back to the mempool
// and are written to the pool so the next restart finds them again.
func (bc *Blockchain) RestoreDurableTxs() (int, error) {
	rocksHead, rocksOK := bc.headBlockNumberFromRocks()

	bc.mu.RLock()
	pool := bc.txPool
	bc.mu.RUnlock()

	pooled := map[string]*Transaction{}
	if pool != nil {
		raws, err := pool.List()
		if err != nil {
			return 0, err
		}
		for hash, raw := range raws {
			var tx Transaction
			if err := json.Unmarshal(raw, &tx); err != nil {
				logger.Warn("tx pool skip %s: %v", hash, err)
				continue
			}
			if tx.Hash == "" {
				tx.Hash = hash
			}
			pooled[tx.Hash] = &tx
		}
	}

	bc.mu.Lock()
	canonical := map[string]int64{}
	kept := make([]BlockRecord, 0, len(bc.blockHistory))
	droppedLead := false
	for _, block := range bc.blockHistory {
		if rocksOK && block.BlockNumber > rocksHead {
			droppedLead = true
			continue
		}
		kept = append(kept, block)
		for _, hash := range block.TxHashes {
			if hash != "" {
				canonical[hash] = block.BlockNumber
			}
		}
	}
	if droppedLead {
		bc.blockHistory = kept
		head := historyHead(kept)
		bc.durableHead = head
		if rocksOK {
			bc.blockCounter = rocksHead + 1
		} else if head >= 0 {
			bc.blockCounter = head + 1
		}
	}

	seen := map[string]*Transaction{}
	for hash, tx := range pooled {
		seen[hash] = tx
	}
	for hash, tx := range bc.transactions {
		if tx == nil || hash == "" {
			continue
		}
		if _, ok := seen[hash]; ok {
			continue
		}
		if _, inBlock := canonical[hash]; inBlock {
			continue
		}
		seen[hash] = tx
	}

	inFlight := map[string]bool{}
	for _, tx := range bc.mempool {
		if tx != nil && tx.Hash != "" {
			inFlight[tx.Hash] = true
		}
	}
	for _, tx := range bc.pendingBlock {
		if tx != nil && tx.Hash != "" {
			inFlight[tx.Hash] = true
		}
	}

	restored := make([]*Transaction, 0)
	forget := make([]string, 0)
	for hash, tx := range seen {
		if tx == nil || hash == "" {
			continue
		}
		if _, inBlock := canonical[hash]; inBlock {
			forget = append(forget, hash)
			continue
		}
		if tx.From == FaucetAddress || tx.Type == "faucet" || tx.SigMain == "" {
			continue
		}
		current := bc.transactions[hash]
		if current == nil {
			current = tx
			bc.transactions[hash] = current
			if current.From != "" {
				bc.addressTxs[current.From] = append(bc.addressTxs[current.From], current)
			}
			if current.To != "" && current.To != current.From {
				bc.addressTxs[current.To] = append(bc.addressTxs[current.To], current)
			}
		}
		current.BlockNumber = 0
		delete(bc.confirmedHashes, hash)
		if !inFlight[hash] {
			bc.mempool = append(bc.mempool, current)
			inFlight[hash] = true
		}
		restored = append(restored, current)
	}
	bc.mu.Unlock()

	if pool != nil {
		for _, hash := range forget {
			_ = pool.Delete(hash)
		}
		for _, tx := range restored {
			raw, err := json.Marshal(tx)
			if err != nil {
				continue
			}
			if err := pool.Put(tx.Hash, raw); err != nil {
				logger.Warn("tx pool rewrite %s: %v", tx.Hash, err)
			}
		}
	}
	if droppedLead {
		if err := bc.PersistChainSnapshot(); err != nil {
			logger.Warn("rewrite chain cache after dropping uncommitted tip: %v", err)
		}
	}
	return len(restored), nil
}
