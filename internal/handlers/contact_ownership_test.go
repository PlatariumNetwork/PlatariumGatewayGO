package handlers

import "testing"

func TestVerifyContactRespondRejectsForgedOwned(t *testing.T) {
	h := &Handler{}
	_, err := h.verifyContactRespondOwnership("PxActor", "req1", "accepted", "owned:forged", "", "", "")
	if err == nil {
		t.Fatal("expected rejection of client-supplied owned:")
	}
}

func TestVerifyContactPricingRejectsForgedOwned(t *testing.T) {
	h := &Handler{}
	_, err := h.verifyContactPricingOwnership("PxActor", "owned:forged", "", "", "")
	if err == nil {
		t.Fatal("expected rejection of client-supplied owned:")
	}
}

func TestContactOwnershipUsesResolveAuthenticatedOwner(t *testing.T) {
	// Missing auth path: no mnemonic and no sig → error.
	h := &Handler{}
	if _, err := h.verifyContactPricingOwnership("PxA", "", "", "", ""); err == nil {
		t.Fatal("expected missing proof error")
	}
}
