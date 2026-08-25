package blockchain

import "testing"

func TestCanonicalAssetAndXP(t *testing.T) {
	if CanonicalAsset("") != "PLP" {
		t.Fatalf("empty → PLP")
	}
	if CanonicalAsset("xp") != TokenXP {
		t.Fatalf("xp → Token:XP, got %s", CanonicalAsset("xp"))
	}
	if CanonicalAsset("Token:XP") != TokenXP {
		t.Fatalf("Token:XP stays canonical")
	}
	if !IsNonTransferableAsset("XP") || !IsNonTransferableAsset("Token:xp") {
		t.Fatal("XP must be non-transferable")
	}
	if IsNonTransferableAsset("PLP") || IsNonTransferableAsset("Token:USDT") {
		t.Fatal("PLP/USDT must stay transferable")
	}
	if TokenXPFromMap(map[string]string{TokenXP: "150"}) != "150" {
		t.Fatal("xp map")
	}
}
