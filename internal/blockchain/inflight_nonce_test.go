package blockchain

import "testing"

func TestHasInFlightNonce(t *testing.T) {
	bc := NewBlockchain()
	bc.mempool = []*Transaction{{Hash: "h1", From: "PxA", To: "PxB", Nonce: 3}}
	if !bc.HasInFlightNonce("PxA", 3) {
		t.Fatal("expected nonce 3 in flight")
	}
	if bc.HasInFlightNonce("PxA", 2) {
		t.Fatal("nonce 2 should not be in flight")
	}
	if bc.HasInFlightNonce("PxB", 3) {
		t.Fatal("other sender should not match")
	}
}
