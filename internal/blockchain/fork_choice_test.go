package blockchain

import "testing"

func TestPreferBlockByL2Yes(t *testing.T) {
	a := BlockRecord{BlockNumber: 1, BlockHash: "aaa", L2Yes: 2}
	b := BlockRecord{BlockNumber: 1, BlockHash: "bbb", L2Yes: 5}
	if !PreferBlock(a, b) {
		t.Fatal("expected b preferred")
	}
	if PreferBlock(b, a) {
		t.Fatal("expected a not preferred over b")
	}
}

func TestPreferBlockByHashTiebreak(t *testing.T) {
	a := BlockRecord{BlockNumber: 1, BlockHash: "aaa", L2Yes: 3}
	b := BlockRecord{BlockNumber: 1, BlockHash: "zzz", L2Yes: 3}
	if !PreferBlock(a, b) {
		t.Fatal("expected higher hash preferred")
	}
}
