package escrow

import "testing"

func TestMapProtocolOutcome(t *testing.T) {
	if MapProtocolOutcome("accepted") != OutcomeAccept {
		t.Fatal("accept")
	}
	if MapProtocolOutcome("rejected") != OutcomeReject {
		t.Fatal("reject")
	}
	if MapProtocolOutcome("timeout") != OutcomeTimeout {
		t.Fatal("timeout")
	}
	if !IsEscrowTxKind(TxKindLock) || !IsEscrowTxKind("contact_escrow_settle") {
		t.Fatal("kinds")
	}
}

func TestPurposeDealConstant(t *testing.T) {
	if PurposeDeal != "deal" {
		t.Fatalf("purpose %q", PurposeDeal)
	}
	if DefaultPurpose("") != PurposeContact || DefaultPurpose("  ") != PurposeContact {
		t.Fatal("blank purpose must stay contact")
	}
	if DefaultPurpose(PurposeDeal) != PurposeDeal {
		t.Fatal("deal must be explicit")
	}
}

func TestBuildLockPayload(t *testing.T) {
	p := BuildLockPayload("eid", "A", "B", "", "rh", 100, 99, 1)
	if p.TxKind != TxKindLock || p.Purpose != PurposeContact || p.Amount != 100 {
		t.Fatalf("%+v", p)
	}
}
