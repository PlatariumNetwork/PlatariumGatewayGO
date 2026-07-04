package blockchain

import "testing"

func TestPruneMempoolRemovesConfirmedAndDuplicateNonce(t *testing.T) {
	bc := NewBlockchain()
	bc.blockHistory = []BlockRecord{{
		BlockNumber: 1,
		TxHashes:    []string{"confirmed-hash"},
	}}
	bc.mempool = []*Transaction{
		{Hash: "confirmed-hash", From: "Pxabc", Nonce: 0, Timestamp: 1},
		{Hash: "first-nonce1", From: "Pxabc", Nonce: 1, Timestamp: 10},
		{Hash: "dup-nonce1", From: "Pxabc", Nonce: 1, Timestamp: 20},
		{Hash: "other", From: "Pxdef", Nonce: 0, Timestamp: 5},
	}

	removed := bc.PruneMempool()
	if removed != 2 {
		t.Fatalf("removed=%d want 2", removed)
	}
	if len(bc.mempool) != 2 {
		t.Fatalf("mempool len=%d want 2", len(bc.mempool))
	}
	if bc.mempool[0].Hash != "first-nonce1" || bc.mempool[1].Hash != "other" {
		t.Fatalf("unexpected mempool: %+v", bc.mempool)
	}
}

func TestL1CollectBlockLimit(t *testing.T) {
	bc := NewBlockchain()
	bc.mempool = []*Transaction{
		{Hash: "a"},
		{Hash: "b"},
		{Hash: "c"},
	}
	moved := bc.L1CollectBlockLimit(1)
	if len(moved) != 1 || moved[0].Hash != "a" {
		t.Fatalf("moved=%v", moved)
	}
	if len(bc.mempool) != 2 || len(bc.pendingBlock) != 1 {
		t.Fatalf("mempool=%d pending=%d", len(bc.mempool), len(bc.pendingBlock))
	}
}
