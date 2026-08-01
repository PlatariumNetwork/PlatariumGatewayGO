package contacteconomy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRequestLifecycleAccept(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ce.json")
	store, err := NewStore(path, Config{
		Enabled:          true,
		MinFeeUplp:       1,
		MaxFeeUplp:       1_000_000_000,
		DefaultUnknownUplp: 1_000_000,
		TimeoutSecs:      3600,
		BasePendingLimit: 5,
		EconomyGateDMs:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	a, b := "PxAAAA", "PxBBBB"
	if store.CanSendFreeDM(a, b) {
		t.Fatal("expected gated DM")
	}
	req, err := store.CreateRequest(ContactRequest{
		RequestID:        "req-1",
		Sender:           a,
		Receiver:         b,
		SenderPubKey:     "pkA",
		ReceiverPubKey:   "pkB",
		EncryptedPayload: "cipher",
		LockTxHash:       "a1b2c3d4e5f6789012345678901234567890abcdef1234567890abcdef123456",
		AmountUplp:       1_000_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if req.RequestIDHash == "" || req.Status != StatusPending {
		t.Fatalf("bad request: %+v", req)
	}
	_, err = store.Respond("req-1", b, OutcomeAccepted, "owned:"+b)
	if err != nil {
		t.Fatal(err)
	}
	if !store.HasProtocolContact(a, b) {
		t.Fatal("expected protocol contact")
	}
	if !store.CanSendFreeDM(a, b) {
		t.Fatal("expected free DM after accept")
	}
	store.AddXP(b, 150)
	if store.PendingLimit(b) != 6 {
		t.Fatalf("xp pending limit: got %d", store.PendingLimit(b))
	}
}

func TestDuplicateLockTxRejected(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "ce.json"), Config{
		Enabled: true, MinFeeUplp: 1, MaxFeeUplp: 1e12, TimeoutSecs: 60, BasePendingLimit: 10, EconomyGateDMs: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	base := ContactRequest{
		Sender: "PxA", Receiver: "PxB", EncryptedPayload: "c", LockTxHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", AmountUplp: 100,
		SenderPubKey: "a", ReceiverPubKey: "b",
	}
	base.RequestID = "r1"
	if _, err := store.CreateRequest(base); err != nil {
		t.Fatal(err)
	}
	base.RequestID = "r2"
	if _, err := store.CreateRequest(base); err == nil {
		t.Fatal("expected duplicate lockTxHash error")
	}
}

func TestPendingLimitAndPairCooldown(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "ce.json"), Config{
		Enabled: true, MinFeeUplp: 1, MaxFeeUplp: 1e12, TimeoutSecs: 3600, BasePendingLimit: 1, EconomyGateDMs: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	base := ContactRequest{
		Sender: "PxA", Receiver: "PxB", EncryptedPayload: "c", AmountUplp: 100,
		SenderPubKey: "a", ReceiverPubKey: "b",
	}
	base.RequestID = "p1"
	base.LockTxHash = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if _, err := store.CreateRequest(base); err != nil {
		t.Fatal(err)
	}
	base.RequestID = "p2"
	base.LockTxHash = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	base.Receiver = "PxC"
	if _, err := store.CreateRequest(base); err == nil {
		t.Fatal("expected pending limit exceeded")
	}
	// Pair cooldown: same pair while pending
	store2, _ := NewStore(filepath.Join(dir, "ce2.json"), Config{
		Enabled: true, MinFeeUplp: 1, MaxFeeUplp: 1e12, TimeoutSecs: 3600, BasePendingLimit: 10, EconomyGateDMs: true,
	})
	r := ContactRequest{
		RequestID: "c1", Sender: "PxX", Receiver: "PxY", EncryptedPayload: "c",
		LockTxHash: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", AmountUplp: 10, SenderPubKey: "a", ReceiverPubKey: "b",
	}
	if _, err := store2.CreateRequest(r); err != nil {
		t.Fatal(err)
	}
	r.RequestID = "c2"
	r.LockTxHash = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	if _, err := store2.CreateRequest(r); err == nil {
		t.Fatal("expected pending pair cooldown")
	}
}

func TestBlockedReceiver(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "ce.json"), Config{
		Enabled: true, MinFeeUplp: 1, MaxFeeUplp: 1e12, TimeoutSecs: 60, BasePendingLimit: 10, EconomyGateDMs: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.SetPricing(PricingAnnounce{
		Address: "PxB", UnknownFeeUplp: 1, VerifiedFeeUplp: 1, Blocked: true, Signature: "s",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.CreateRequest(ContactRequest{
		RequestID: "b1", Sender: "PxA", Receiver: "PxB", EncryptedPayload: "c",
		LockTxHash: "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", AmountUplp: 10, SenderPubKey: "a", ReceiverPubKey: "b",
	})
	if err == nil {
		t.Fatal("expected blocked")
	}
}

func TestMarkSettledAndFindByEscrowID(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "ce.json"), Config{
		Enabled: true, MinFeeUplp: 1, MaxFeeUplp: 1e12, TimeoutSecs: 3600, BasePendingLimit: 5, EconomyGateDMs: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	req, err := store.CreateRequest(ContactRequest{
		RequestID: "ms1", Sender: "PxA", Receiver: "PxB", EncryptedPayload: "c",
		LockTxHash: "1111111111111111111111111111111111111111111111111111111111111111", AmountUplp: 10, SenderPubKey: "a", ReceiverPubKey: "b",
	})
	if err != nil {
		t.Fatal(err)
	}
	found, ok := store.FindByEscrowID(req.RequestIDHash)
	if !ok || found.RequestID != "ms1" {
		t.Fatal("FindByEscrowID")
	}
	if err := store.MarkSettled("ms1", "settle-tx-1"); err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetRequest("ms1")
	if got.SettleTxHash != "settle-tx-1" {
		t.Fatalf("settle hash: %s", got.SettleTxHash)
	}
}

func TestLocalBookDoesNotAffectProtocolGate(t *testing.T) {
	// Documented invariant: only HasProtocolContact unlocks free DM — local address books are client-side.
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "ce.json"), Config{
		Enabled: true, MinFeeUplp: 1, MaxFeeUplp: 1e12, TimeoutSecs: 60, BasePendingLimit: 5, EconomyGateDMs: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if store.CanSendFreeDM("PxA", "PxB") {
		t.Fatal("no protocol contact → gated")
	}
}

func TestPlaceholderLockRejected(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "ce.json"), Config{
		Enabled: true, MinFeeUplp: 1, MaxFeeUplp: 1e12, TimeoutSecs: 60, BasePendingLimit: 10, EconomyGateDMs: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.CreateRequest(ContactRequest{
		RequestID: "ph1", Sender: "PxA", Receiver: "PxB", EncryptedPayload: "c",
		LockTxHash: "escrow_lock:abc:1000", AmountUplp: 10, SenderPubKey: "a", ReceiverPubKey: "b",
	})
	if err == nil {
		t.Fatal("expected placeholder lock rejected")
	}
}

func TestStubSignatureRejected(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "ce.json"), Config{
		Enabled: true, MinFeeUplp: 1, MaxFeeUplp: 1e12, TimeoutSecs: 3600, BasePendingLimit: 5, EconomyGateDMs: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.CreateRequest(ContactRequest{
		RequestID: "s1", Sender: "PxA", Receiver: "PxB", EncryptedPayload: "c",
		LockTxHash: "3333333333333333333333333333333333333333333333333333333333333333", AmountUplp: 10,
		SenderPubKey: "a", ReceiverPubKey: "b",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Respond("s1", "PxB", OutcomeAccepted, "sig-deadbeef-12"); err == nil {
		t.Fatal("expected stub sig rejected")
	}
}

func TestExpireDue(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "ce.json"), Config{
		Enabled: true, MinFeeUplp: 1, MaxFeeUplp: 1e12, TimeoutSecs: 1, BasePendingLimit: 10, EconomyGateDMs: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	req, err := store.CreateRequest(ContactRequest{
		RequestID: "exp1", Sender: "PxA", Receiver: "PxB", EncryptedPayload: "c",
		LockTxHash: "2222222222222222222222222222222222222222222222222222222222222222", AmountUplp: 10, SenderPubKey: "a", ReceiverPubKey: "b", Timestamp: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	expired := store.ExpireDue(req.ExpiresAt + 1)
	if len(expired) != 1 || expired[0].SettleOutcome != OutcomeTimeout {
		t.Fatalf("expire: %+v", expired)
	}
	_ = os.RemoveAll(dir)
}
