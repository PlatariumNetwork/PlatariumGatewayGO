package blockchain

import "platarium-gateway-go/internal/logger"

// PruneMempool removes stale entries: txs already in chain (memory or RocksDB),
// duplicate (from, nonce) pairs, and txs that can never be packed (nonce behind
// account or sitting behind a nonce gap). Without gap/stale pruning, Core
// select_block_txs returns empty forever while should_propose stays true.
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

	accountNonce := bc.accountNoncesForMempool()

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
			if want, ok := accountNonce[tx.From]; ok && tx.Nonce < int(want) {
				removed++
				continue
			}
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

	// Drop anything sitting behind a nonce hole (e.g. have 7,8 but account expects 6).
	if len(accountNonce) > 0 {
		bySender := make(map[string][]*Transaction)
		for _, tx := range kept {
			if tx.From == "" || tx.From == FaucetAddress {
				continue
			}
			bySender[tx.From] = append(bySender[tx.From], tx)
		}
		dropHash := make(map[string]bool)
		for from, list := range bySender {
			want := int(accountNonce[from])
			// sort by nonce ascending (small N — insertion sort)
			for i := 1; i < len(list); i++ {
				j := i
				for j > 0 && list[j-1].Nonce > list[j].Nonce {
					list[j-1], list[j] = list[j], list[j-1]
					j--
				}
			}
			expect := want
			for _, tx := range list {
				if tx.Nonce == expect {
					expect++
					continue
				}
				if tx.Nonce < expect {
					dropHash[tx.Hash] = true
					continue
				}
				// gap: drop this and every later nonce for this sender
				dropHash[tx.Hash] = true
				for _, rest := range list {
					if rest.Nonce > tx.Nonce {
						dropHash[rest.Hash] = true
					}
				}
				break
			}
		}
		if len(dropHash) > 0 {
			filtered := make([]*Transaction, 0, len(kept))
			for _, tx := range kept {
				if dropHash[tx.Hash] {
					removed++
					continue
				}
				filtered = append(filtered, tx)
			}
			kept = filtered
			logger.Info("Mempool pruned nonce-gap/stale hashes=%d", len(dropHash))
		}
	}

	if removed > 0 {
		bc.mempool = kept
	}
	return removed
}

// accountNoncesForMempool reads current account nonces for every mempool sender.
func (bc *Blockchain) accountNoncesForMempool() map[string]uint64 {
	bc.mu.RLock()
	ledger := bc.ledger
	senders := make(map[string]struct{})
	for _, tx := range bc.mempool {
		if tx != nil && tx.From != "" && tx.From != FaucetAddress {
			senders[tx.From] = struct{}{}
		}
	}
	bc.mu.RUnlock()
	if ledger == nil || len(senders) == 0 {
		return nil
	}
	out := make(map[string]uint64, len(senders))
	for addr := range senders {
		q, err := ledger.Query(addr)
		if err != nil {
			continue
		}
		out[addr] = q.Nonce
	}
	return out
}
