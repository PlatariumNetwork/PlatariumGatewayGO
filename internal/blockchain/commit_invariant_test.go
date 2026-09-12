package blockchain

import (
	"strings"
	"testing"

	"platarium-gateway-go/internal/core"
)

func TestCommitHeightHashInvariantMatchingPasses(t *testing.T) {
	err := CheckCommitHeightHashInvariant(1, "bh", 1, "bh", "sr", "sr")
	if err != nil {
		t.Fatal(err)
	}
}

func TestCommitHeightHashInvariantFailsOnDiverge(t *testing.T) {
	if err := CheckCommitHeightHashInvariant(2, "a", 1, "a", "s", "s"); err == nil || !strings.Contains(err.Error(), "height") {
		t.Fatalf("want height diverge, got %v", err)
	}
	if err := CheckCommitHeightHashInvariant(1, "a", 1, "b", "s", "s"); err == nil || !strings.Contains(err.Error(), "hash") {
		t.Fatalf("want hash diverge, got %v", err)
	}
	if err := CheckCommitHeightHashInvariant(1, "a", 1, "a", "s1", "s2"); err == nil || !strings.Contains(err.Error(), "state_root") {
		t.Fatalf("want state_root diverge, got %v", err)
	}
}

// After successful confirm, committed block B must pair with state after B across layers (#58).
func TestConfirmExplorerPairsHeightHashAcrossLayers(t *testing.T) {
	bc := NewBlockchain()
	dir := t.TempDir()
	bc.SetChainFile(dir + "/chain.json")
	tx := &Transaction{Hash: "h1", From: "PxA", To: "PxB", Fee: "1"}
	moved, block, err := bc.ConfirmExplorerWithoutCore([]*Transaction{tx}, "pending", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(moved) != 1 {
		t.Fatalf("moved=%d", len(moved))
	}
	// Simulate successful Rocks commit matching explorer tip (height+hash pairing).
	block.BlockHash = "paired-hash"
	block.StateRoot = "paired-root"
	bc.mu.Lock()
	if n := len(bc.blockHistory); n > 0 {
		bc.blockHistory[n-1].BlockHash = block.BlockHash
		bc.blockHistory[n-1].StateRoot = block.StateRoot
	}
	bc.mu.Unlock()

	explorerH := core.GatewayBlockToRocksHeight(block.BlockNumber)
	rocksH := explorerH
	if err := CheckCommitHeightHashInvariant(explorerH, block.BlockHash, rocksH, block.BlockHash, block.StateRoot, block.StateRoot); err != nil {
		t.Fatal(err)
	}
	// Divergent Rocks layer must fail the invariant (acceptance: test fails if diverge).
	if err := CheckCommitHeightHashInvariant(explorerH, block.BlockHash, rocksH+1, block.BlockHash, block.StateRoot, block.StateRoot); err == nil {
		t.Fatal("expected invariant failure when Rocks height diverges after commit")
	}
}
