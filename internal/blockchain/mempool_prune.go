package blockchain

import (
	"sort"
	"time"

	"platarium-gateway-go/internal/logger"
)

// MempoolMaxNonceGap matches PlatariumCore MEMPOOL_MAX_NONCE_GAP — parallel submits
// may arrive out of order; do not prune txs within this window of the expected nonce.
const MempoolMaxNonceGap = 64

// mempoolFutureMaxAgeSec keeps a fresh out-of-order nonce. Older futures are leftovers
// from a previous run and must not sit in the pool forever ahead of the account nonce.
const mempoolFutureMaxAgeSec = 6 * 3600

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

	orig := make([]string, 0, len(bc.mempool))
	for _, tx := range bc.mempool {
		if tx != nil && tx.Hash != "" {
			orig = append(orig, tx.Hash)
		}
	}

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

	// Drop stale nonces and txs too far ahead; keep in-gap futures (parallel HTTP submit).
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
			for i := 1; i < len(list); i++ {
				j := i
				for j > 0 && list[j-1].Nonce > list[j].Nonce {
					list[j-1], list[j] = list[j], list[j-1]
					j--
				}
			}
			now := time.Now().Unix()
			for _, tx := range list {
				if tx.Nonce < want {
					dropHash[tx.Hash] = true
					continue
				}
				if tx.Nonce > want+MempoolMaxNonceGap {
					dropHash[tx.Hash] = true
					continue
				}
				if tx.Nonce != want && tx.Timestamp > 0 && now-tx.Timestamp > mempoolFutureMaxAgeSec {
					dropHash[tx.Hash] = true
				}
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
	if bc.txPool != nil && len(orig) > 0 {
		keptSet := make(map[string]bool, len(kept))
		for _, tx := range kept {
			if tx != nil && tx.Hash != "" {
				keptSet[tx.Hash] = true
			}
		}
		for _, hash := range orig {
			if !keptSet[hash] {
				_ = bc.txPool.Delete(hash)
			}
		}
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

func escrowAwaitingBlock(tx *Transaction) bool {
	if tx == nil || tx.Hash == "" || tx.SigMain == "" {
		return false
	}
	if tx.BlockNumber > 0 {
		return false
	}
	switch tx.Type {
	case "escrow_lock", "escrow_settle", "escrow_cancel", "escrow_refund":
		return true
	default:
		return tx.Purpose == "deal"
	}
}

// RequeueUnconfirmedEscrows puts indexed deal locks (and their settle/cancel)
// back into the mempool when they never received a block number. A restart
// keeps those rows in chain.json but drops the in-memory mempool, so the
// lock would stay visible in the wallet without debiting the payer.
func (bc *Blockchain) RequeueUnconfirmedEscrows() int {
	bc.mu.RLock()
	ledger := bc.ledger
	inFlight := make(map[string]bool, len(bc.mempool)+len(bc.pendingBlock))
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
	cands := make([]*Transaction, 0)
	senders := make(map[string]struct{})
	for _, tx := range bc.transactions {
		if !escrowAwaitingBlock(tx) || inFlight[tx.Hash] || bc.confirmedHashes[tx.Hash] {
			continue
		}
		if tx.Timestamp > 0 && time.Now().Unix()-tx.Timestamp > mempoolFutureMaxAgeSec {
			continue
		}
		cands = append(cands, tx)
		if tx.From != "" {
			senders[tx.From] = struct{}{}
		}
	}
	bc.mu.RUnlock()
	if len(cands) == 0 {
		return 0
	}

	accountNonce := map[string]uint64{}
	if ledger != nil {
		for addr := range senders {
			q, err := ledger.Query(addr)
			if err != nil {
				continue
			}
			accountNonce[addr] = q.Nonce
		}
	}

	sort.Slice(cands, func(i, j int) bool {
		if cands[i].From != cands[j].From {
			return cands[i].From < cands[j].From
		}
		return cands[i].Nonce < cands[j].Nonce
	})

	bc.mu.Lock()
	defer bc.mu.Unlock()
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
	added := 0
	for _, tx := range cands {
		if tx == nil || inFlight[tx.Hash] || bc.confirmedHashes[tx.Hash] || tx.BlockNumber > 0 {
			continue
		}
		if ledger != nil && tx.From != "" {
			if want, ok := accountNonce[tx.From]; ok && uint64(tx.Nonce) < want {
				continue
			}
		}
		bc.mempool = append(bc.mempool, tx)
		inFlight[tx.Hash] = true
		added++
	}
	return added
}
