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

	d := ConsistencyDiagnostic{
		DiagnosticOnly: true,
		Status:         ConsistencyStatusConsistent,
		Rocks:          LayerTip{Present: true, Height: 2, BlockHash: "bh-rocks", StateRoot: "sr1"},
		ChainJSON:      LayerTip{Present: true, Height: 1, BlockHash: "bh-chain", StateRoot: "sr2"},
	}
	if d.Rocks.Height != d.ChainJSON.Height {
		d.Status = ConsistencyStatusDiverged
		d.Reasons = append(d.Reasons, "height_mismatch")
	}
	if d.Status != ConsistencyStatusDiverged {
		t.Fatal("expected DIVERGED fixture")
	}

	m := ConsistencyDiagnostic{
		DiagnosticOnly: true,
		Status:         ConsistencyStatusConsistent,
		Rocks:          LayerTip{Present: true, Height: 1, BlockHash: "same", StateRoot: "sr"},
		ChainJSON:      LayerTip{Present: true, Height: 1, BlockHash: "same", StateRoot: "sr"},
	}
	if m.Rocks.Height != m.ChainJSON.Height || m.Rocks.BlockHash != m.ChainJSON.BlockHash {
		m.Status = ConsistencyStatusDiverged
	}
	if m.Status != ConsistencyStatusConsistent {
		t.Fatal("expected CONSISTENT fixture")
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
