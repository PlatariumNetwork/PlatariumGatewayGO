package handlers

import (
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

// Round-trip: commit snapshot fields → AccountQuery shaping used by balance API (#29).
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
