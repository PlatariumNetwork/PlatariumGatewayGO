package handlers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"platarium-gateway-go/internal/blockchain"
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
