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
