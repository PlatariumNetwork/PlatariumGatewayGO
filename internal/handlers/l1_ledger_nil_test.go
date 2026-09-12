package handlers

import (
	"testing"

	"platarium-gateway-go/internal/blockchain"
)

func TestValidateTxsForL1LedgerNilFailClosed(t *testing.T) {
	txs := []*blockchain.Transaction{{Hash: "tx1", From: "PxA", To: "PxB"}}
	for _, tc := range []struct {
		name    string
		testnet bool
	}{
		{"non_testnet", false},
		{"testnet", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &Handler{
				blockchain: blockchain.NewBlockchain(), // ledger nil
				testnet:    tc.testnet,
			}
			outcome := h.validateTxsForL1(txs)
			if outcome.OK {
				t.Fatal("ledger nil must not OK")
			}
			if outcome.Err == nil {
				t.Fatal("ledger nil must return error")
			}
		})
	}
}
