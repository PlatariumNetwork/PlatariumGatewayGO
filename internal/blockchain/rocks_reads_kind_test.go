package blockchain

import "testing"

func TestTxFromCoreJSONKeepsEscrowKind(t *testing.T) {
	raw := `{"hash":"abc","from":"PxA","to":"PxB","asset":"PLP","amount":100000000,"fee_uplp":1,"nonce":1,"tx_kind":"escrow_lock","purpose":"deal","escrow_id":"deal-1","writes":["terms:ship it"],"settle_node":"PxN"}`
	tx, err := txFromCoreJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if tx.Type != "escrow_lock" || tx.Purpose != "deal" || tx.EscrowID != "deal-1" {
		t.Fatalf("lost deal fields: %+v", tx)
	}
	if tx.Value != "100000000" {
		t.Fatalf("value %q", tx.Value)
	}
}
