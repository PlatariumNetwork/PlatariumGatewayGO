package blockchain

import (
	"encoding/json"
)

// MempoolSnapshotJSON builds Core-compatible mempool snapshot with arrival_index.
func (bc *Blockchain) MempoolSnapshotJSON() (string, error) {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	entries := make([]map[string]interface{}, 0, len(bc.mempool))
	for i, tx := range bc.mempool {
		if tx == nil {
			continue
		}
		m := map[string]interface{}{
			"hash":          tx.Hash,
			"from":          tx.From,
			"to":            tx.To,
			"asset":         tx.Asset,
			"amount":        tx.AmountUplp,
			"fee_uplp":      tx.FeeUplp,
			"nonce":         tx.Nonce,
			"sig_main":      tx.SigMain,
			"sig_derived":   tx.SigDerived,
			"arrival_index": uint64(i),
			"timestamp":     tx.Timestamp,
		}
		if tx.Asset == "" {
			m["asset"] = "PLP"
		}
		if tx.FeeUplp == 0 && tx.Fee != "" {
			m["fee"] = tx.Fee
		}
		if tx.AmountUplp == 0 && tx.Value != "" {
			m["amount"] = tx.Value
		}
		if len(tx.Reads) > 0 {
			m["reads"] = tx.Reads
		} else {
			m["reads"] = []string{}
		}
		if len(tx.Writes) > 0 {
			m["writes"] = tx.Writes
		} else {
			m["writes"] = []string{}
		}
		if tx.PubMain != "" {
			m["pub_main"] = tx.PubMain
		}
		if tx.PubDerived != "" {
			m["pub_derived"] = tx.PubDerived
		}
		entries = append(entries, m)
	}
	b, err := json.Marshal(entries)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// SelectTxsByHashes returns mempool txs in hash order, preserving first-seen order of hashes.
func (bc *Blockchain) SelectTxsByHashes(hashes []string) []*Transaction {
	if len(hashes) == 0 {
		return nil
	}
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	byHash := make(map[string]*Transaction, len(bc.mempool))
	for _, tx := range bc.mempool {
		if tx != nil && tx.Hash != "" {
			byHash[tx.Hash] = tx
		}
	}
	out := make([]*Transaction, 0, len(hashes))
	for _, h := range hashes {
		if tx, ok := byHash[h]; ok {
			out = append(out, tx)
		}
	}
	return out
}

// L1CollectSelected moves the Core-selected transactions into the pending block.
// This mutates gateway storage only; transaction selection remains authoritative in Core.
func (bc *Blockchain) L1CollectSelected(selected []*Transaction) []*Transaction {
	if len(selected) == 0 {
		return nil
	}
	pick := make(map[string]bool, len(selected))
	for _, tx := range selected {
		if tx != nil && tx.Hash != "" {
			pick[tx.Hash] = true
		}
	}

	bc.mu.Lock()
	defer bc.mu.Unlock()

	moved := make([]*Transaction, 0, len(selected))
	remaining := make([]*Transaction, 0, len(bc.mempool))
	for _, tx := range bc.mempool {
		if tx != nil && pick[tx.Hash] {
			moved = append(moved, tx)
			delete(pick, tx.Hash)
		} else {
			remaining = append(remaining, tx)
		}
	}
	bc.mempool = remaining
	bc.pendingBlock = moved
	return moved
}
