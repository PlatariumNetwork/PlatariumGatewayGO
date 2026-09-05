package nodes

import (
	"crypto/ed25519"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateIdentityPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "node_identity.json")
	id1, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	if id1.NodeID == "" || id1.PubKey == "" {
		t.Fatal("empty identity")
	}
	id2, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	if id1.NodeID != id2.NodeID || id1.PrivKey != id2.PrivKey {
		t.Fatal("identity not stable across reload")
	}
}

func TestSignAndVerifyVote(t *testing.T) {
	dir := t.TempDir()
	id, err := LoadOrCreateIdentity(filepath.Join(dir, "id.json"))
	if err != nil {
		t.Fatal(err)
	}
	sig, err := id.SignVote("block-abc", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyVoteSignature(id.NodeID, "block-abc", true, id.PubKey, sig); err != nil {
		t.Fatal(err)
	}
	if err := VerifyVoteSignature(id.NodeID, "block-abc", false, id.PubKey, sig); err == nil {
		t.Fatal("expected fail on yes mismatch")
	}
	// spoofed node id
	fake := hex.EncodeToString(make([]byte, ed25519.PublicKeySize))
	if err := VerifyVoteSignature(fake, "block-abc", true, id.PubKey, sig); err == nil {
		t.Fatal("expected nodeId mismatch")
	}
}

func TestRequireSignedVotesDefault(t *testing.T) {
	os.Unsetenv("PLATARIUM_ALLOW_UNSIGNED_VOTES")
	if !RequireSignedVotes() {
		t.Fatal("expected signed votes required by default")
	}
	t.Setenv("PLATARIUM_ALLOW_UNSIGNED_VOTES", "1")
	if RequireSignedVotes() {
		t.Fatal("expected unsigned allowed")
	}
}
