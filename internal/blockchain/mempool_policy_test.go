package blockchain

import (
	"strings"
	"testing"
)

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

func TestAddToMempoolRejectsNonceAlreadyPending(t *testing.T) {
	bc := NewBlockchain()
	bc.pendingBlock = []*Transaction{{
		Hash: "pending",
		From: "PxSender",
		Nonce: 0,
	}}

	err := bc.AddToMempool(&Transaction{
		Hash: "replacement",
		From: "PxSender",
		Nonce: 0,
	})
	if err == nil {
		t.Fatal("expected duplicate pending nonce to be rejected")
	}
	if len(bc.mempool) != 0 {
		t.Fatalf("mempool len=%d want 0", len(bc.mempool))
	}
}

func TestAdmissionSnapshotIncludesPendingBeforeMempool(t *testing.T) {
	bc := NewBlockchain()
	bc.pendingBlock = []*Transaction{{
		Hash: "pending",
		From: "PxSender",
		Nonce: 0,
	}}
	bc.mempool = []*Transaction{{
		Hash: "queued",
		From: "PxSender",
		Nonce: 1,
	}}

	got, err := bc.AdmissionSnapshotJSON()
	if err != nil {
		t.Fatal(err)
	}
	pendingAt := strings.Index(got, `"hash":"pending"`)
	queuedAt := strings.Index(got, `"hash":"queued"`)
	if pendingAt < 0 || queuedAt < 0 || pendingAt > queuedAt {
		t.Fatalf("unexpected admission snapshot order: %s", got)
	}
}
