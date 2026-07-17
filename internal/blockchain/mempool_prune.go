package blockchain

// PruneMempool removes stale entries: txs already in chain (memory or RocksDB)
// and duplicate (from, nonce) pairs. For duplicate nonces the earliest timestamp (FIFO) is kept.
func (bc *Blockchain) PruneMempool() int {
	// Gather RocksDB confirmed hashes before taking the write lock (rocksClient uses RLock).
	rocksConfirmed := make(map[string]bool)
	if bc.RocksEnabled() {
		if blocks, err := bc.listBlockHistoryFromRocks(); err == nil {
			for _, block := range blocks {
				for _, hash := range block.TxHashes {
					if hash != "" {
						rocksConfirmed[hash] = true
					}
				}
			}
		}
	}

	bc.mu.Lock()
	defer bc.mu.Unlock()

	confirmed := make(map[string]bool)
	for _, block := range bc.blockHistory {
		for _, hash := range block.TxHashes {
			if hash != "" {
				confirmed[hash] = true
			}
		}
	}
	for hash := range rocksConfirmed {
		confirmed[hash] = true
	}

	pendingHashes := make(map[string]bool, len(bc.pendingBlock))
	for _, tx := range bc.pendingBlock {
		if tx != nil && tx.Hash != "" {
			pendingHashes[tx.Hash] = true
		}
	}

	type senderNonce struct {
		from  string
		nonce int
	}
	seenNonce := make(map[senderNonce]int) // index in kept slice

	kept := make([]*Transaction, 0, len(bc.mempool))
	removed := 0

	for _, tx := range bc.mempool {
		if tx == nil || tx.Hash == "" {
			removed++
			continue
		}
		// Instant faucet credits never belong in the consensus mempool.
		if tx.From == FaucetAddress || tx.Type == "faucet" {
			removed++
			continue
		}
		if confirmed[tx.Hash] || pendingHashes[tx.Hash] {
			removed++
			continue
		}
		if tx.From != "" && tx.From != FaucetAddress {
			key := senderNonce{from: tx.From, nonce: tx.Nonce}
			if prevIdx, ok := seenNonce[key]; ok {
				prev := kept[prevIdx]
				if tx.Timestamp > 0 && prev.Timestamp > 0 && tx.Timestamp < prev.Timestamp {
					kept[prevIdx] = tx
				}
				removed++
				continue
			}
			seenNonce[key] = len(kept)
		}
		kept = append(kept, tx)
	}

	if removed > 0 {
		bc.mempool = kept
	}
	return removed
}
