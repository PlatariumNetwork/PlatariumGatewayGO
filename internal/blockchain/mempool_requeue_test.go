package blockchain

import "testing"

func TestRequeueUnconfirmedDealLockOnce(t *testing.T) {
	bc := NewBlockchain()
	deal := &Transaction{
		Hash:       "deal-hash",
		Type:       "escrow_lock",
		Purpose:    "deal",
		EscrowID:   "c021590df1d04a279d88dd2ae7c0f4f4",
		From:       "PxA",
		To:         "PxB",
		SigMain:    "sig",
		Nonce:      2,
		AmountUplp: 500_000_000,
		Writes:     []string{"terms:тест 3"},
	}
	cancel := &Transaction{
		Hash:     "cancel-hash",
		Type:     "escrow_cancel",
		Purpose:  "deal",
		EscrowID: deal.EscrowID,
		From:     "PxA",
		SigMain:  "sig",
		Nonce:    1,
	}
	sealed := &Transaction{
		Hash:        "sealed",
		Type:        "escrow_lock",
		Purpose:     "deal",
		From:        "PxA",
		SigMain:     "sig",
		BlockNumber: 19,
	}
	bc.mu.Lock()
	bc.transactions[deal.Hash] = deal
	bc.transactions[cancel.Hash] = cancel
	bc.transactions[sealed.Hash] = sealed
	bc.confirmedHashes[sealed.Hash] = true
	bc.mu.Unlock()

	if n := bc.RequeueUnconfirmedEscrows(); n != 2 {
		t.Fatalf("requeued %d", n)
	}
	pool := bc.GetMempool()
	if len(pool) != 2 || pool[0].Hash != "cancel-hash" || pool[1].Hash != "deal-hash" {
		t.Fatalf("mempool=%v", hashesOf(pool))
	}
	if n := bc.RequeueUnconfirmedEscrows(); n != 0 {
		t.Fatalf("second requeue %d", n)
	}
}

func hashesOf(txs []*Transaction) []string {
	out := make([]string, len(txs))
	for i, tx := range txs {
		if tx != nil {
			out[i] = tx.Hash
		}
	}
	return out
}
