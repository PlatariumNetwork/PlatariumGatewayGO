package protocol

import (
	"testing"

	"platarium-gateway-go/internal/contacteconomy"
	"platarium-gateway-go/internal/escrow"
)

func TestContactSettleFromRequest(t *testing.T) {
	req := contacteconomy.ContactRequest{
		RequestID:     "req-1",
		RequestIDHash: "abc123",
		Sender:        "PxA",
		Receiver:      "PxB",
		AmountUplp:    1_000_000,
		LockTxHash:    "lock1",
		SettleOutcome: contacteconomy.OutcomeAccepted,
	}
	intent := ContactSettleFromRequest(req, "PxNode")
	if intent.TxKind != escrow.TxKindSettle {
		t.Fatalf("txKind: %s", intent.TxKind)
	}
	if intent.EscrowID != "abc123" || intent.OutcomeKey != escrow.OutcomeAccept {
		t.Fatalf("intent: %+v", intent)
	}
	if intent.Purpose != PurposeContact {
		t.Fatal("purpose")
	}
}

func TestStatusFromContactRequest(t *testing.T) {
	req := contacteconomy.ContactRequest{
		RequestIDHash: "eid",
		Status:        contacteconomy.StatusPending,
		AmountUplp:    10,
		LockTxHash:    "L",
	}
	st := StatusFromContactRequest(req)
	if st.Status != "LOCKED" || st.EscrowID != "eid" {
		t.Fatalf("%+v", st)
	}
}
