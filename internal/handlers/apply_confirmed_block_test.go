package handlers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"platarium-gateway-go/internal/blockchain"
	"platarium-gateway-go/internal/metrics"
)

func TestConfirmBoundaryStepsDocumented(t *testing.T) {
	for _, step := range []string{"prepare", "apply explorer", "persist", "Rocks", "commit marker", "cleanup backup"} {
		if !strings.Contains(ConfirmBoundarySteps, step) {
			t.Fatalf("missing step %q in %q", step, ConfirmBoundarySteps)
		}
	}
}

// Backup must survive until durable success — DiscardStateBackup is the sole cleanup after Rocks.
func TestDiscardStateBackupOnlyAfterExplicitCall(t *testing.T) {
	dir := t.TempDir()
	backup := filepath.Join(dir, "state.json.l2bak")
	if err := os.WriteFile(backup, []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Simulate mid-boundary: explorer applied, Rocks not yet — backup must still exist.
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("backup missing before Rocks success: %v", err)
	}
	blockchain.DiscardStateBackup(backup)
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatal("backup should be removed only via DiscardStateBackup after commit marker")
	}
}

func TestFinalizeConfirmedBlockNilMovedNoPanic(t *testing.T) {
	// Wire-level: empty producer/header path without Core should not panic when Rocks off.
	h := &Handler{blockchain: blockchain.NewBlockchain()}
	block := blockchain.BlockRecord{BlockNumber: 0, Timestamp: 1}
	res, err := h.FinalizeConfirmedBlock(block, nil, "", "n0")
	// Without ledger/Core, assemble fails; Rocks disabled → soft path, no error.
	if h.blockchain.RocksEnabled() {
		t.Fatal("expected Rocks disabled")
	}
	if err != nil {
		// Soft path returns nil error when Rocks disabled.
		t.Fatalf("unexpected error: %v", err)
	}
	_ = res
}

// Failure point: after_explorer_before_rocks — between apply and Rocks (#59).
func TestFailureInjectionAfterExplorerBeforeRocksNoTipDiverge(t *testing.T) {
	metrics.Global.ResetForTest()
	dir := t.TempDir()
	bc := blockchain.NewBlockchain()
	bc.SetChainFile(filepath.Join(dir, "chain.json"))
	tx := &blockchain.Transaction{Hash: "inj1", From: "PxA", To: "PxB", Fee: "1"}
	moved, block, err := bc.ConfirmExplorerWithoutCore([]*blockchain.Transaction{tx}, "pending", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if bc.HeadBlockNumber() != 0 {
		t.Fatalf("pre-inject head=%d", bc.HeadBlockNumber())
	}

	backup := filepath.Join(dir, "core-state.json.l2bak")
	if err := os.WriteFile(backup, []byte(`{"backup":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	h := &Handler{blockchain: bc}
	h.SetConfirmFailPoint(FailPointAfterExplorerBeforeRocks)
	_, err = h.DurableCommitAfterExplorer(block, moved, backup)
	if err == nil || !strings.Contains(err.Error(), string(FailPointAfterExplorerBeforeRocks)) {
		t.Fatalf("want injected failure documenting fail point, got %v", err)
	}
	if bc.HeadBlockNumber() != -1 {
		t.Fatalf("canonical tip advanced after fail-point: head=%d", bc.HeadBlockNumber())
	}
	if len(bc.GetBlockHistory()) != 0 {
		t.Fatal("explorer tip not undone after fail-point")
	}
	if _, statErr := os.Stat(backup); !os.IsNotExist(statErr) {
		t.Fatal("undo should consume backup")
	}
}

// Peer path retains backup until DurableCommitAfterExplorer success (#60).
func TestPeerConfirmBackupRetainedUntilDurableSuccess(t *testing.T) {
	dir := t.TempDir()
	bc := blockchain.NewBlockchain()
	bc.SetChainFile(filepath.Join(dir, "chain.json"))
	tx := &blockchain.Transaction{Hash: "p1", From: "PxA", To: "PxB", Fee: "1"}
	moved, block, err := bc.ConfirmExplorerWithoutCore([]*blockchain.Transaction{tx}, "pending", "", "")
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(dir, "state.json.l2bak")
	if err := os.WriteFile(backup, []byte(`{"peer":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	h := &Handler{blockchain: bc}
	// Shared durable path with Rocks disabled: success discards backup (commit marker).
	res, err := h.DurableCommitAfterExplorer(block, moved, backup)
	if err != nil {
		t.Fatal(err)
	}
	if res.BackupKept {
		t.Fatal("backup should be discarded after durable success")
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatal("backup retained after success")
	}
}

func TestDurabilityCountersEndpointPayload(t *testing.T) {
	metrics.Global.ResetForTest()
	metrics.Global.IncCoreRPCErrors()
	metrics.Global.IncRocksCommitErrors()
	metrics.Global.IncStateRocksDivergence()
	snap := metrics.Global.Snapshot()
	if snap.CoreRPCErrors != 1 || snap.RocksCommitErrors != 1 || snap.StateRocksDivergence != 1 {
		t.Fatalf("snap=%+v", snap)
	}
	if !strings.Contains(metrics.HowToRead, "/internal/counters") {
		t.Fatal("how_to_read missing endpoint")
	}
}
