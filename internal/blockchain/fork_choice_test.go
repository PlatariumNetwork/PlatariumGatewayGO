package blockchain

import (
	"fmt"
	"testing"
)

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

func TestIsForkConflict(t *testing.T) {
	if !IsForkConflict(ErrForkConflict) {
		t.Fatal("bare ErrForkConflict")
	}
	wrapped := fmt.Errorf("%w at height 1: keeping local", ErrForkConflict)
	if !IsForkConflict(wrapped) {
		t.Fatal("wrapped ErrForkConflict")
	}
	if IsForkConflict(fmt.Errorf("other")) {
		t.Fatal("non-fork error")
	}
}
