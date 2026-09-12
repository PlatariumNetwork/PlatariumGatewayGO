package handlers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"platarium-gateway-go/internal/blockchain"
	"platarium-gateway-go/internal/core"
)

func TestRocksAccountFromQueryPreservesTokenXP(t *testing.T) {
	q := &core.AccountQuery{
		Address:     "PxTokenOwner",
		Balance:     "10",
		UplpBalance: "10000000",
		Nonce:       3,
		Tokens:      map[string]string{blockchain.TokenXP: "150", "Token:USDT": "1"},
		Xp:          "150",
	}
	acct := rocksAccountFromQuery(q)
	if acct.Tokens[blockchain.TokenXP] != "150" {
		t.Fatalf("Tokens Token:XP dropped: %#v", acct.Tokens)
	}
	if acct.Xp != "150" {
		t.Fatalf("Xp dropped: %q", acct.Xp)
	}
	if acct.Balance != "10" || acct.Nonce != 3 {
		t.Fatalf("balance/nonce: %+v", acct)
	}
	// Clone: mutating source must not alter commit snapshot.
	q.Tokens[blockchain.TokenXP] = "0"
	if acct.Tokens[blockchain.TokenXP] != "150" {
		t.Fatal("Tokens map not cloned — silent drop risk")
	}
}

func TestRocksAccountFromQueryDerivesXPFromTokens(t *testing.T) {
	q := &core.AccountQuery{
		Address: "PxB",
		Balance: "1",
		Tokens:  map[string]string{blockchain.TokenXP: "99"},
	}
	acct := rocksAccountFromQuery(q)
	if acct.Xp != "99" {
		t.Fatalf("expected derived xp, got %q", acct.Xp)
	}
}

func TestRocksAccountFromQueryEmptyMapsAndStrings(t *testing.T) {
	acct := rocksAccountFromQuery(&core.AccountQuery{Address: "PxEmpty"})
	if acct.Tokens == nil {
		t.Fatal("Tokens must be empty map, not nil")
	}
	if len(acct.Tokens) != 0 {
		t.Fatalf("expected empty Tokens, got %#v", acct.Tokens)
	}
	if acct.Xp != "0" {
		t.Fatalf("empty Xp must normalize to 0, got %q", acct.Xp)
	}

	nilQ := rocksAccountFromQuery(nil)
	if nilQ.Tokens == nil || nilQ.Xp != "0" {
		t.Fatalf("nil query: tokens=%v xp=%q", nilQ.Tokens, nilQ.Xp)
	}

	emptyMap := rocksAccountFromQuery(&core.AccountQuery{
		Address: "PxEmptyMap",
		Tokens:  map[string]string{},
		Xp:      "",
	})
	if emptyMap.Tokens == nil || emptyMap.Xp != "0" {
		t.Fatalf("empty map/string: tokens=%v xp=%q", emptyMap.Tokens, emptyMap.Xp)
	}
}

// Round-trip: commit snapshot fields → AccountQuery shaping used by balance API (#54).
func TestTokenXPSurvivesCommitToBalanceShape(t *testing.T) {
	q := &core.AccountQuery{
		Address: "PxRound",
		Balance: "5",
		Tokens:  map[string]string{blockchain.TokenXP: "42"},
		Xp:      "42",
	}
	committed := rocksAccountFromQuery(q)
	// Mirror getAccountFromRocks → GetBalance mapping.
	restored := &core.AccountQuery{
		Address: committed.Address,
		Asset:   "PLP",
		Balance: committed.Balance,
		Nonce:   committed.Nonce,
		Tokens:  committed.Tokens,
		Xp:      committed.Xp,
	}
	xp := restored.Xp
	if xp == "" {
		xp = blockchain.TokenXPFromMap(restored.Tokens)
	}
	if xp != "42" || restored.Tokens[blockchain.TokenXP] != "42" {
		t.Fatalf("balance API would lose Token:XP: xp=%q tokens=%v", xp, restored.Tokens)
	}
}

// Commit payload JSON must carry tokens/xp end-to-end (Gateway → Core wire shape).
func TestBlockCommitPayloadPreservesTokensXpJSON(t *testing.T) {
	q := &core.AccountQuery{
		Address: "PxJSON",
		Balance: "7",
		Tokens:  map[string]string{blockchain.TokenXP: "88", "Token:USDT": "2"},
		Xp:      "88",
	}
	acct := rocksAccountFromQuery(q)
	payload := &core.BlockCommitPayload{
		Block: core.RocksBlockStored{
			Height:    1,
			StateRoot: "sr",
			BlockHash: "bh",
		},
		Accounts:  []core.RocksAccount{acct},
		StateRoot: "sr",
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var back core.BlockCommitPayload
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if len(back.Accounts) != 1 {
		t.Fatalf("accounts: %d", len(back.Accounts))
	}
	got := back.Accounts[0]
	if got.Xp != "88" || got.Tokens[blockchain.TokenXP] != "88" || got.Tokens["Token:USDT"] != "2" {
		t.Fatalf("Tokens/Xp dropped in commit JSON: %+v", got)
	}
}

// Integration: Gateway ledger query (Tokens/Xp) → rocksAccountFromQuery → Core rocks commit → read (#54).
func TestTokensXpGatewayCoreReadIntegration(t *testing.T) {
	prevMode := os.Getenv("PLATARIUM_CORE_MODE")
	prevAllow := os.Getenv("PLATARIUM_CORE_ALLOW_EXTERNAL_ROCKS_COMMIT")
	_ = os.Setenv("PLATARIUM_CORE_MODE", "cli")
	_ = os.Setenv("PLATARIUM_CORE_ALLOW_EXTERNAL_ROCKS_COMMIT", "1")
	t.Cleanup(func() {
		_ = os.Setenv("PLATARIUM_CORE_MODE", prevMode)
		_ = os.Setenv("PLATARIUM_CORE_ALLOW_EXTERNAL_ROCKS_COMMIT", prevAllow)
	})

	rc, err := core.NewRustCore()
	if err != nil {
		t.Skipf("Platarium Core not available: %v", err)
	}
	defer rc.Close()

	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")
	dbPath := filepath.Join(dir, "rocksdb")
	ls, err := core.NewLedgerService(rc, stateFile, true)
	if err != nil {
		t.Fatalf("NewLedgerService: %v", err)
	}
	addr := "PxTokensXpOwner"
	if err := ls.Credit(addr, 1000, 0); err != nil {
		t.Fatalf("Credit: %v", err)
	}
	if err := ls.CreditToken(addr, blockchain.TokenXP, 150); err != nil {
		t.Fatalf("CreditToken XP: %v", err)
	}
	q, err := ls.Query(addr)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if q.Tokens[blockchain.TokenXP] != "150" && q.Xp != "150" {
		// Core may expose XP via tokens map and/or xp field after Wave D1.
		t.Fatalf("Core query missing XP: tokens=%v xp=%q", q.Tokens, q.Xp)
	}

	acct := rocksAccountFromQuery(q)
	if acct.Tokens[blockchain.TokenXP] != "150" && acct.Xp != "150" {
		t.Fatalf("Gateway snapshot dropped Tokens/Xp: %+v", acct)
	}
	if acct.Xp == "" {
		t.Fatal("Gateway Xp empty after rocksAccountFromQuery")
	}

	root, err := ls.StateRoot()
	if err != nil {
		t.Fatalf("StateRoot: %v", err)
	}
	rocks, err := core.NewRocksStoreClient(rc, dbPath)
	if err != nil {
		t.Fatalf("NewRocksStoreClient: %v", err)
	}
	commit := &core.BlockCommitPayload{
		Block: core.RocksBlockStored{
			Height:       1,
			PreviousHash: "0",
			Timestamp:    1,
			TxHashes:     []string{},
			MerkleRoot:   "m",
			StateRoot:    root,
			BlockHash:    "bh-tokens-xp",
			ProducerID:   "n0",
		},
		TxJSONs:   []string{},
		Accounts:  []core.RocksAccount{acct},
		Receipts:  []core.BlockReceipt{},
		StateRoot: root,
	}
	if _, err := rocks.RocksCommitBlock(commit); err != nil {
		t.Fatalf("RocksCommitBlock: %v", err)
	}
	found, got, err := rocks.RocksGetAccount(addr)
	if err != nil {
		t.Fatalf("RocksGetAccount: %v", err)
	}
	if !found || got == nil {
		t.Fatal("account missing after rocks commit")
	}
	xp := got.Xp
	if xp == "" {
		xp = blockchain.TokenXPFromMap(got.Tokens)
	}
	if xp != "150" {
		t.Fatalf("post-commit read lost Xp: got=%+v", got)
	}
	if tok := got.Tokens[blockchain.TokenXP]; tok != "" && tok != "150" {
		t.Fatalf("post-commit Tokens Token:XP=%q", tok)
	}
}
