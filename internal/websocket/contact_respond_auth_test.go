package websocket

import (
	"path/filepath"
	"testing"

	"platarium-gateway-go/internal/contacteconomy"
	"platarium-gateway-go/internal/protocol"
)

func TestContactRespondEmptySigWithoutAuthErrors(t *testing.T) {
	c := &Client{ID: "c1", Address: "PxReceiver", Authenticated: false}
	_, _, err := prepareContactRespondOwnership(c, map[string]interface{}{
		"requestId": "r1",
		"outcome":   contacteconomy.OutcomeAccepted,
		"signature": "",
	})
	if err == nil {
		t.Fatal("empty signature without auth must error")
	}
}

func TestContactRespondAuthenticatedSelfOnly(t *testing.T) {
	c := &Client{ID: "c1", Address: "PxReceiver", Authenticated: true}
	verified, sig, err := prepareContactRespondOwnership(c, map[string]interface{}{
		"actor":     "PxReceiver",
		"signature": "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if verified != "PxReceiver" {
		t.Fatalf("verified=%q", verified)
	}
	want, _ := protocol.MintOwnedProof("PxReceiver")
	if sig != want {
		t.Fatalf("sig=%q want %q", sig, want)
	}
	_, _, err = prepareContactRespondOwnership(c, map[string]interface{}{
		"actor":     "PxOther",
		"signature": "",
	})
	if err == nil {
		t.Fatal("authenticated session must not respond for another address")
	}
}

func TestContactRespondFailedAuthNoXPOrSettle(t *testing.T) {
	dir := t.TempDir()
	store, err := contacteconomy.NewStore(filepath.Join(dir, "ce.json"), contacteconomy.Config{
		Enabled: true, MinFeeUplp: 1, MaxFeeUplp: 1e12, TimeoutSecs: 3600, BasePendingLimit: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	sender, receiver := "PxSender", "PxReceiver"
	_, err = store.CreateRequest(contacteconomy.ContactRequest{
		RequestID: "req-xp", Sender: sender, Receiver: receiver,
		SenderPubKey: "a", ReceiverPubKey: "b", EncryptedPayload: "c",
		LockTxHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		AmountUplp: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	xpBefore := store.GetXP(receiver)
	s := &Server{contactEconomy: store}
	client := &Client{ID: "c-unauth", Address: receiver, Authenticated: false}
	_, err = s.applyContactRespondWS(client, map[string]interface{}{
		"requestId": "req-xp",
		"outcome":   contacteconomy.OutcomeAccepted,
		"signature": "",
	})
	if err == nil {
		t.Fatal("expected auth failure")
	}
	if store.GetXP(receiver) != xpBefore {
		t.Fatalf("XP mutated on failed auth: %d", store.GetXP(receiver))
	}
	req, ok := store.GetRequest("req-xp")
	if !ok || req.Status != contacteconomy.StatusPending {
		t.Fatalf("request must stay pending on failed auth: %+v", req)
	}
	// Authenticated self can complete; restores cleanly for subsequent tests via temp dir.
	client.Authenticated = true
	ack, err := s.applyContactRespondWS(client, map[string]interface{}{
		"requestId": "req-xp",
		"outcome":   contacteconomy.OutcomeAccepted,
		"signature": "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ack == nil {
		t.Fatal("expected ack")
	}
	if store.GetXP(receiver) != xpBefore+25 {
		t.Fatalf("XP after accept=%d", store.GetXP(receiver))
	}
}

func TestContactRespondClaimedAddressAloneNotProof(t *testing.T) {
	// Claimed Address without Authenticated must not mint owned: (issue #33).
	c := &Client{ID: "c1", Address: "PxForged", Authenticated: false}
	_, sig, err := prepareContactRespondOwnership(c, map[string]interface{}{"signature": ""})
	if err == nil {
		t.Fatal("claimed Address alone must not authorize empty-sig mint")
	}
	if sig != "" {
		t.Fatalf("must not mint on failure: %q", sig)
	}
}
