package websocket

import (
	"path/filepath"
	"strings"
	"testing"

	"platarium-gateway-go/internal/contacteconomy"
	"platarium-gateway-go/internal/protocol"
)

func newContactEconomyFixture(t *testing.T, requestID, sender, receiver string) (*Server, *contacteconomy.Store) {
	t.Helper()
	dir := t.TempDir()
	store, err := contacteconomy.NewStore(filepath.Join(dir, "ce.json"), contacteconomy.Config{
		Enabled: true, MinFeeUplp: 1, MaxFeeUplp: 1e12, TimeoutSecs: 3600, BasePendingLimit: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.CreateRequest(contacteconomy.ContactRequest{
		RequestID: requestID, Sender: sender, Receiver: receiver,
		SenderPubKey: "a", ReceiverPubKey: "b", EncryptedPayload: "c",
		LockTxHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		AmountUplp: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &Server{contactEconomy: store}, store
}

// Issue #34: forged client.Address + owned:forged must fail closed; store never accepts owned:forged.
func TestForgedClientAddressWSRespondRejected(t *testing.T) {
	sender, receiver := "PxSender", "PxReceiver"
	s, store := newContactEconomyFixture(t, "req-forge", sender, receiver)
	xpBefore := store.GetXP(receiver)

	// Attacker forges Address on the socket without session authentication.
	attacker := &Client{ID: "attacker", Address: receiver, Authenticated: false}
	_, err := s.applyContactRespondWS(attacker, map[string]interface{}{
		"requestId": "req-forge",
		"outcome":   contacteconomy.OutcomeAccepted,
		"signature": "owned:forged",
	})
	if err == nil {
		t.Fatal("forged Address + owned:forged must be rejected")
	}

	// Defense in depth: store itself rejects owned:forged even if called directly.
	if _, err := store.Respond("req-forge", receiver, contacteconomy.OutcomeAccepted, "owned:forged"); err == nil {
		t.Fatal("store must not accept owned:forged")
	}

	req, ok := store.GetRequest("req-forge")
	if !ok || req.Status != contacteconomy.StatusPending {
		t.Fatalf("request must stay pending: %+v", req)
	}
	if store.GetXP(receiver) != xpBefore {
		t.Fatalf("XP mutated after forged respond: %d", store.GetXP(receiver))
	}
}

// Issue #35: session authenticated as A cannot respond/settle for address B.
func TestRegisteredACannotActForB(t *testing.T) {
	sender, receiverB := "PxSender", "PxB"
	s, store := newContactEconomyFixture(t, "req-ab", sender, receiverB)
	xpBBefore := store.GetXP(receiverB)
	xpABefore := store.GetXP("PxA")

	clientA := &Client{ID: "sess-a", Address: "PxA", Authenticated: true}
	_, err := s.applyContactRespondWS(clientA, map[string]interface{}{
		"requestId": "req-ab",
		"actor":     receiverB,
		"outcome":   contacteconomy.OutcomeAccepted,
		"signature": "",
	})
	if err == nil {
		t.Fatal("authenticated A must not act for B")
	}

	verified, sig, err := prepareContactRespondOwnership(clientA, map[string]interface{}{
		"actor":     receiverB,
		"signature": "",
	})
	if err == nil {
		t.Fatal("prepare must reject A acting for B")
	}
	if verified != "" || sig != "" {
		t.Fatalf("must not mint for B: verified=%q sig=%q", verified, sig)
	}
	if strings.HasPrefix(sig, "owned:"+receiverB) || strings.EqualFold(sig, "owned:PxB") {
		t.Fatal("must not mint owned: for B")
	}

	req, ok := store.GetRequest("req-ab")
	if !ok || req.Status != contacteconomy.StatusPending {
		t.Fatalf("request must stay pending: %+v", req)
	}
	if store.GetXP(receiverB) != xpBBefore {
		t.Fatalf("XP credited to B wrongly: %d", store.GetXP(receiverB))
	}
	if store.GetXP("PxA") != xpABefore {
		t.Fatalf("XP credited to wrong party A: %d", store.GetXP("PxA"))
	}
}

// Issue #37: valid session material bound to wrong address, and missing auth — both no mint.
func TestValidSigWrongAddressAndMissingAuthNoMint(t *testing.T) {
	// Valid cryptographic/session material for A, claimed address B.
	clientA := &Client{ID: "sess-a", Address: "PxA", Authenticated: true}
	verified, sig, err := prepareContactRespondOwnership(clientA, map[string]interface{}{
		"actor":     "PxB",
		"signature": "",
	})
	if err == nil {
		t.Fatal("valid session A + actor B must be rejected")
	}
	if verified != "" || sig != "" {
		t.Fatalf("no owned: mint on wrong address: verified=%q sig=%q", verified, sig)
	}
	if _, err := protocol.MintOwnedProofAfterResolve("PxB", "PxA"); err == nil {
		t.Fatal("MintOwnedProofAfterResolve must reject wrong address binding")
	}

	// Missing authentication entirely.
	unauth := &Client{ID: "sess-u", Address: "PxB", Authenticated: false}
	verified, sig, err = prepareContactRespondOwnership(unauth, map[string]interface{}{
		"actor":     "PxB",
		"signature": "",
	})
	if err == nil {
		t.Fatal("missing auth must be rejected")
	}
	if verified != "" || sig != "" {
		t.Fatalf("no owned: mint without auth: verified=%q sig=%q", verified, sig)
	}
	if _, err := protocol.MintOwnedProofAfterResolve("PxB", ""); err == nil {
		t.Fatal("missing authenticated owner must not mint")
	}
}
