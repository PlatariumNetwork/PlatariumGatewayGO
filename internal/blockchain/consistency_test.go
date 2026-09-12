package blockchain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestADRConsistencyDiagnosticOnly(t *testing.T) {
	if ADRConsistencyDiagnosticOnly == "" {
		t.Fatal("ADR missing")
	}
	lower := strings.ToLower(ADRConsistencyDiagnosticOnly)
	for _, needle := range []string{"diagnostic", "repair", "never"} {
		if !strings.Contains(lower, needle) {
			t.Fatalf("ADR should mention %q", needle)
		}
	}
}

func TestBuildConsistencyDiagnosticMatchingEmpty(t *testing.T) {
	bc := NewBlockchain()
	rep := bc.BuildConsistencyDiagnostic()
	if !rep.DiagnosticOnly {
		t.Fatal("must be diagnostic_only")
	}
	if rep.Status != ConsistencyStatusConsistent {
		t.Fatalf("empty layers should be CONSISTENT, got %s reasons=%v", rep.Status, rep.Reasons)
	}
}

func TestBuildConsistencyDiagnosticDivergedFixture(t *testing.T) {
	dir := t.TempDir()
	chainPath := filepath.Join(dir, "chain.json")
	bc := NewBlockchain()
	bc.SetChainFile(chainPath)

	bc.mu.Lock()
	bc.blockHistory = []BlockRecord{{
		BlockNumber: 0,
		BlockHash:   "hash-chain-a",
		StateRoot:   "root-a",
	}}
	bc.blockCounter = 1
	bc.mu.Unlock()
	if err := bc.PersistChainSnapshot(); err != nil {
		t.Fatal(err)
	}

	rep := bc.BuildConsistencyDiagnostic()
	if rep.ChainJSON.Height != 1 || rep.ChainJSON.BlockHash != "hash-chain-a" {
		t.Fatalf("chain tip: %+v", rep.ChainJSON)
	}
	if !rep.DiagnosticOnly {
		t.Fatal("diagnostic_only")
	}

	// Forced mismatch fixture → DIVERGED (#68).
	mismatchReasons := EvaluateLayerConsistency(
		LayerTip{Present: true, Height: 2, BlockHash: "bh-rocks", StateRoot: "sr1"},
		LayerTip{Present: true, Height: 1, BlockHash: "bh-chain", StateRoot: "sr2"},
		LayerTip{},
		nil,
	)
	if StatusFromReasons(mismatchReasons) != ConsistencyStatusDiverged {
		t.Fatalf("expected DIVERGED fixture, reasons=%v", mismatchReasons)
	}
	if len(mismatchReasons) == 0 {
		t.Fatal("forced mismatch must produce reasons")
	}

	// Matching fixture → CONSISTENT (#68).
	matchReasons := EvaluateLayerConsistency(
		LayerTip{Present: true, Height: 1, BlockHash: "same", StateRoot: "sr"},
		LayerTip{Present: true, Height: 1, BlockHash: "same", StateRoot: "sr"},
		LayerTip{},
		nil,
	)
	if StatusFromReasons(matchReasons) != ConsistencyStatusConsistent {
		t.Fatalf("expected CONSISTENT fixture, reasons=%v", matchReasons)
	}

	raw, err := os.ReadFile(chainPath)
	if err != nil {
		t.Fatal(err)
	}
	var file ChainFileData
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Blocks) != 1 {
		t.Fatalf("blocks=%d", len(file.Blocks))
	}
}

func TestConsistencyMismatchAndMatchFixtures(t *testing.T) {
	// Standalone fixture test for issue #68 acceptance criteria.
	diverged := EvaluateLayerConsistency(
		LayerTip{Present: true, Height: 3, BlockHash: "r", StateRoot: "a"},
		LayerTip{Present: true, Height: 2, BlockHash: "c", StateRoot: "b"},
		LayerTip{Present: false},
		nil,
	)
	if StatusFromReasons(diverged) != ConsistencyStatusDiverged {
		t.Fatal("forced mismatch fixture must report DIVERGED")
	}
	consistent := EvaluateLayerConsistency(
		LayerTip{Present: true, Height: 2, BlockHash: "x", StateRoot: "y"},
		LayerTip{Present: true, Height: 2, BlockHash: "x", StateRoot: "y"},
		LayerTip{Present: false},
		nil,
	)
	if StatusFromReasons(consistent) != ConsistencyStatusConsistent {
		t.Fatal("matching fixture must report CONSISTENT")
	}
}

func TestBuildConsistencyDiagnosticDoesNotMutate(t *testing.T) {
	bc := NewBlockchain()
	dir := t.TempDir()
	path := filepath.Join(dir, "chain.json")
	bc.SetChainFile(path)
	bc.mu.Lock()
	bc.blockHistory = []BlockRecord{{BlockNumber: 0, BlockHash: "h", StateRoot: "r"}}
	bc.blockCounter = 1
	bc.mu.Unlock()
	_ = bc.PersistChainSnapshot()
	before, _ := os.ReadFile(path)
	_ = bc.BuildConsistencyDiagnostic()
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("diagnostic mutated chain.json")
	}
}
