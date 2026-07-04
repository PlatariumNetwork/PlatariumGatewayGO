package faucet

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCooldownStoreRecordAndRemaining(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cooldown.json")
	store, err := NewCooldownStore(path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	if rem := store.Remaining("PxAlice", now); rem != 0 {
		t.Fatalf("expected no cooldown, got %v", rem)
	}
	if err := store.RecordClaim("PxAlice", now); err != nil {
		t.Fatal(err)
	}
	if rem := store.Remaining("PxAlice", now.Add(30*time.Minute)); rem != 30*time.Minute {
		t.Fatalf("expected 30m remaining, got %v", rem)
	}
	if rem := store.Remaining("PxAlice", now.Add(time.Hour)); rem != 0 {
		t.Fatalf("expected cooldown elapsed, got %v", rem)
	}

	store2, err := NewCooldownStore(path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if rem := store2.Remaining("PxAlice", now.Add(10*time.Minute)); rem != 50*time.Minute {
		t.Fatalf("persisted cooldown mismatch: got %v", rem)
	}
}

func TestFormatWait(t *testing.T) {
	h, m, s, label := FormatWait(2*time.Hour + 3*time.Minute + 5*time.Second)
	if h != 2 || m != 3 || s != 5 {
		t.Fatalf("unexpected parts: %d %d %d", h, m, s)
	}
	if label != "2 hours 3 minutes 5 seconds" {
		t.Fatalf("unexpected label: %q", label)
	}
}

func TestNewCooldownStoreMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "cooldown.json")
	store, err := NewCooldownStore(path, defaultCooldown)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file should not exist until first claim")
	}
	if err := store.RecordClaim("PxBob", time.Unix(1_700_000_000, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
