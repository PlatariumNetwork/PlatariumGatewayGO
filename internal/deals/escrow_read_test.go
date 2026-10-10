package deals

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDealsIgnoreContactStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	body := `{
	  "escrows_json": [
	    "{\"escrow_id\":\"deal-1\",\"creator\":\"alice\",\"beneficiary\":\"bob\",\"node\":\"node-1\",\"amount\":10,\"peaceful_fee\":1,\"reserved_fee\":2,\"asset\":\"PLP\",\"purpose\":\"deal\",\"status\":\"Locked\",\"expires_at\":99}",
	    "{\"escrow_id\":\"contact-1\",\"creator\":\"alice\",\"beneficiary\":\"bob\",\"purpose\":\"contact\",\"status\":\"Locked\",\"asset\":\"PLP\",\"amount\":5}"
	  ]
	}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := LoadCoreEscrows(path)
	if err != nil {
		t.Fatal(err)
	}
	list := DealsForAddress(rows, "bob")
	if len(list) != 1 || list[0].EscrowID != "deal-1" {
		t.Fatalf("%+v", list)
	}
	creator := DealsForAddress(rows, "alice")
	if len(creator) != 1 || creator[0].EscrowID != "deal-1" {
		t.Fatalf("creator %+v", creator)
	}
	for _, row := range list {
		if row.EscrowID == "contact-1" {
			t.Fatal("contact request leaked into deals")
		}
	}
	got, ok := DealByID(rows, "deal-1")
	if !ok || got.PeacefulFee != 1 || got.ReservedFee != 2 || got.Status != "Locked" {
		t.Fatalf("%+v", got)
	}
	if _, ok := DealByID(rows, "contact-1"); ok {
		t.Fatal("contact id must not read as a deal")
	}
	if _, ok := DealByID(rows, "missing"); ok {
		t.Fatal("unknown id")
	}
}
