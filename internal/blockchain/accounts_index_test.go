package blockchain

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListTopAccountsSortAndPaginate(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")
	content := `{
  "version": 1,
  "asset_balances": [
    ["PxLow", "PLP", "100"],
    ["PxHigh", "PLP", "9000"],
    ["PxMid", "PLP", "5000"],
    ["PxZero", "PLP", "0"]
  ],
  "uplp_balances": [],
  "nonces": [
    ["PxHigh", 2],
    ["PxMid", 1]
  ]
}`
	if err := os.WriteFile(stateFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	bc := NewBlockchain()
	bc.addressTxs["PxHigh"] = make([]*Transaction, 5)
	bc.addressTxs["PxMid"] = make([]*Transaction, 2)

	result, err := bc.ListTopAccounts(stateFile, 1, 2)
	if err != nil {
		t.Fatalf("ListTopAccounts: %v", err)
	}
	if result.TotalCount != 3 {
		t.Fatalf("totalCount=%d want 3", result.TotalCount)
	}
	if result.TotalSupply != "14100" {
		t.Fatalf("totalSupply=%s want 14100", result.TotalSupply)
	}
	if len(result.Accounts) != 2 {
		t.Fatalf("page len=%d want 2", len(result.Accounts))
	}
	if result.Accounts[0].Address != "PxHigh" || result.Accounts[0].Balance != "9000" {
		t.Fatalf("first row=%+v", result.Accounts[0])
	}
	if result.Accounts[0].TxCount != 5 {
		t.Fatalf("txCount=%d want 5", result.Accounts[0].TxCount)
	}
	if result.LargestAccountBalance != "9000" {
		t.Fatalf("largestAccountBalance=%s want 9000", result.LargestAccountBalance)
	}
	if result.Accounts[1].Address != "PxMid" {
		t.Fatalf("second row=%+v", result.Accounts[1])
	}
}
