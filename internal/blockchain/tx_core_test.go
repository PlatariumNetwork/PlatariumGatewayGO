package blockchain

import (
	"strings"
	"testing"
)

func TestMethodToCoreJSONIncludesEscrow(t *testing.T) {
	tx := &Transaction{
		Hash:       "abc",
		From:       "PxA",
		To:         "PxB",
		SigMain:    "sm",
		SigDerived: "sd",
		Type:       "escrow_lock",
		Asset:      "PLP",
		AmountUplp: 1000,
		FeeUplp:    1,
		EscrowID:   "eid-1",
		Purpose:    "contact",
		ExpiresAt:  1893456000,
		SettlePayee: "PxB",
	}
	js, ok := tx.ToCoreJSON()
	if !ok {
		t.Fatal("method ToCoreJSON failed")
	}
	for _, part := range []string{`"tx_kind":"escrow_lock"`, `"escrow_id":"eid-1"`, `"purpose":"contact"`, `"settle_payee":"PxB"`} {
		if !strings.Contains(js, part) {
			t.Fatalf("method ToCoreJSON missing %s in %s", part, js)
		}
	}
}

func TestToCoreJSONEscrowKinds(t *testing.T) {
	tx := &Transaction{
		Hash:       "h1",
		From:       "PxA",
		To:         "PxB",
		Type:       "escrow_lock",
		Asset:      "PLP",
		AmountUplp: 1000,
		FeeUplp:    1,
		Nonce:      1,
		SigMain:    "sig",
		EscrowID:   "eid-1",
		Purpose:    "contact",
		ExpiresAt:  99,
	}
	js, ok := ToCoreJSON(tx)
	if !ok || js == "" {
		t.Fatal("ToCoreJSON failed")
	}
	for _, part := range []string{`"tx_kind":"escrow_lock"`, `"escrow_id":"eid-1"`, `"purpose":"contact"`} {
		if !strings.Contains(js, part) {
			t.Fatalf("missing %s in %s", part, js)
		}
	}
	tx2 := &Transaction{
		Hash: "h2", From: "PxA", To: "PxB", Type: "escrow_settle",
		Asset: "PLP", AmountUplp: 1000, FeeUplp: 1, Nonce: 2, SigMain: "sig",
		EscrowID: "eid-1", SettleOutcomeKey: "accept", SettlePayee: "PxB", SettleNode: "PxN",
	}
	js2, ok := ToCoreJSON(tx2)
	if !ok {
		t.Fatal("settle ToCoreJSON failed")
	}
	for _, part := range []string{`"settle_outcome_key":"accept"`, `"settle_payee":"PxB"`} {
		if !strings.Contains(js2, part) {
			t.Fatalf("missing %s in %s", part, js2)
		}
	}
}
