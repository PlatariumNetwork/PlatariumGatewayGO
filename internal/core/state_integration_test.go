package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLedgerServiceStateParity verifies LedgerService matches platarium-cli state-* commands.
func TestLedgerServiceStateParity(t *testing.T) {
	rc, err := NewRustCore()
	if err != nil {
		t.Skipf("Platarium Core not available: %v", err)
		return
	}

	stateFile := filepath.Join(t.TempDir(), "state.json")
	ls, err := NewLedgerService(rc, stateFile, true)
	if err != nil {
		t.Fatalf("NewLedgerService: %v", err)
	}

	mnemonic, alphanumeric, err := rc.GenerateMnemonic()
	if err != nil {
		t.Fatalf("GenerateMnemonic: %v", err)
	}
	keys, err := rc.GenerateKeys(mnemonic, alphanumeric, 0)
	if err != nil {
		t.Fatalf("GenerateKeys: %v", err)
	}
	from := keys["publicKey"]
	if from == "" {
		t.Fatal("empty publicKey from GenerateKeys")
	}

	if err := ls.Credit(from, 1_000_000, 10_000); err != nil {
		t.Fatalf("Credit: %v", err)
	}

	q, err := ls.Query(from)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if q.Balance != "1000000" || q.Nonce != 0 {
		t.Fatalf("unexpected query: balance=%s nonce=%d", q.Balance, q.Nonce)
	}

	rootBefore, err := ls.StateRoot()
	if err != nil {
		t.Fatalf("StateRoot before: %v", err)
	}
	if rootBefore == "" {
		t.Fatal("empty state root before apply")
	}

	signedTx, err := rc.SignTransaction(from, "PxBob", "PLP", 100, 1000, 0, nil, nil, mnemonic, alphanumeric)
	if err != nil {
		t.Fatalf("SignTransaction: %v", err)
	}

	ok, err := ls.ValidateTx(signedTx)
	if err != nil || !ok {
		t.Fatalf("ValidateTx valid tx: ok=%v err=%v", ok, err)
	}

	badTx := strings.Replace(signedTx, `"nonce":0`, `"nonce":1`, 1)
	if ok, _ := ls.ValidateTx(badTx); ok {
		t.Fatal("ValidateTx should reject wrong nonce")
	}

	if _, err := ls.ApplyTx(signedTx); err != nil {
		t.Fatalf("ApplyTx: %v", err)
	}

	qAfter, err := ls.Query(from)
	if err != nil {
		t.Fatalf("Query after apply: %v", err)
	}
	if qAfter.Nonce != 1 {
		t.Fatalf("expected nonce 1 after apply, got %d", qAfter.Nonce)
	}

	rootAfter, err := ls.StateRoot()
	if err != nil {
		t.Fatalf("StateRoot after: %v", err)
	}
	if rootAfter == rootBefore {
		t.Fatal("state root should change after apply")
	}

	// Direct CLI query should match LedgerService
	cliOut, err := rc.StateQuery(stateFile, from, "PLP")
	if err != nil {
		t.Fatalf("StateQuery CLI: %v", err)
	}
	var cliQ AccountQuery
	if err := json.Unmarshal([]byte(cliOut), &cliQ); err != nil {
		t.Fatalf("parse CLI query: %v", err)
	}
	if cliQ.Balance != qAfter.Balance || cliQ.Nonce != qAfter.Nonce {
		t.Fatalf("CLI/Gateway mismatch: cli=%+v gateway=%+v", cliQ, qAfter)
	}
}

// TestLedgerServiceInitCreatesFile ensures missing state file is initialized on startup.
func TestLedgerServiceInitCreatesFile(t *testing.T) {
	rc, err := NewRustCore()
	if err != nil {
		t.Skipf("Platarium Core not available: %v", err)
		return
	}
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "nested", "state.json")
	if _, err := NewLedgerService(rc, stateFile, false); err != nil {
		t.Fatalf("NewLedgerService: %v", err)
	}
	if _, err := os.Stat(stateFile); err != nil {
		t.Fatalf("state file not created: %v", err)
	}
}
