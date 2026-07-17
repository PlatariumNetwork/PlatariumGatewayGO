package blockchain

import "testing"

func TestL1CollectSelected(t *testing.T) {
	bc := NewBlockchain()
	bc.mempool = []*Transaction{
		{Hash: "a", FeeUplp: 1},
		{Hash: "b", FeeUplp: 1},
		{Hash: "c", FeeUplp: 1},
	}
	moved := bc.L1CollectSelected([]*Transaction{bc.mempool[0], bc.mempool[2]})
	if len(moved) != 2 || len(bc.mempool) != 1 || bc.mempool[0].Hash != "b" {
		t.Fatalf("moved=%d mempool=%v pending=%d", len(moved), bc.mempool, len(bc.pendingBlock))
	}
}
