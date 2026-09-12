package blockchain

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUndoLastConfirmedBlockClearsAddressTxsAndLastTx(t *testing.T) {
	bc := NewBlockchain()
	tx := &Transaction{Hash: "h1", From: "PxA", To: "PxB", BlockNumber: 0}
	block := BlockRecord{BlockNumber: 0, TotalFees: 10, TxHashes: []string{"h1"}, TxCount: 1}

	bc.blockCounter = 1
	bc.totalFeesCollected = 10
	bc.blockHistory = []BlockRecord{block}
	bc.transactions["h1"] = tx
	bc.confirmedHashes["h1"] = true
	bc.lastTx = tx
	bc.addressTxs["PxA"] = []*Transaction{tx}
	bc.addressTxs["PxB"] = []*Transaction{tx}

	if err := bc.UndoLastConfirmedBlock(block, []*Transaction{tx}, ""); err != nil {
		t.Fatal(err)
	}
	if len(bc.blockHistory) != 0 {
		t.Fatalf("blockHistory=%d", len(bc.blockHistory))
	}
	if bc.blockCounter != 0 {
		t.Fatalf("blockCounter=%d", bc.blockCounter)
	}
	if bc.totalFeesCollected != 0 {
		t.Fatalf("fees=%d", bc.totalFeesCollected)
	}
	if bc.transactions["h1"] != nil || bc.confirmedHashes["h1"] {
		t.Fatal("tx still marked confirmed")
	}
	if bc.lastTx != nil {
		t.Fatal("lastTx not cleared")
	}
	if len(bc.addressTxs["PxA"]) != 0 || len(bc.addressTxs["PxB"]) != 0 {
		t.Fatalf("addressTxs dirty: A=%d B=%d", len(bc.addressTxs["PxA"]), len(bc.addressTxs["PxB"]))
	}
	if len(bc.pendingBlock) != 1 || bc.pendingBlock[0].Hash != "h1" {
		t.Fatalf("expected requeue to pending, got %d", len(bc.pendingBlock))
	}
}

// confirm → Undo → indexes match pre-confirm (#27).
func TestConfirmThenUndoIndexesMatchPreConfirm(t *testing.T) {
	bc := NewBlockchain()
	prior := &Transaction{Hash: "old", From: "PxA", To: "PxC"}
	bc.transactions["old"] = prior
	bc.confirmedHashes["old"] = true
	bc.lastTx = prior
	bc.addressTxs["PxA"] = []*Transaction{prior}
	bc.addressTxs["PxC"] = []*Transaction{prior}

	preLast := bc.lastTx
	preA := len(bc.addressTxs["PxA"])
	preC := len(bc.addressTxs["PxC"])
	preB := len(bc.addressTxs["PxB"])

	tx := &Transaction{Hash: "h1", From: "PxA", To: "PxB", Fee: "1"}
	moved, block, err := bc.ConfirmExplorerWithoutCore([]*Transaction{tx}, "pending", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if bc.lastTx == nil || bc.lastTx.Hash != "h1" {
		t.Fatal("expected lastTx after confirm")
	}
	if len(bc.addressTxs["PxA"]) != preA+1 || len(bc.addressTxs["PxB"]) != preB+1 {
		t.Fatalf("confirm did not index: A=%d B=%d", len(bc.addressTxs["PxA"]), len(bc.addressTxs["PxB"]))
	}

	if err := bc.UndoLastConfirmedBlock(block, moved, ""); err != nil {
		t.Fatal(err)
	}
	if len(bc.addressTxs["PxA"]) != preA {
		t.Fatalf("PxA addressTxs want %d got %d", preA, len(bc.addressTxs["PxA"]))
	}
	if len(bc.addressTxs["PxB"]) != preB {
		t.Fatalf("PxB addressTxs want %d got %d", preB, len(bc.addressTxs["PxB"]))
	}
	if len(bc.addressTxs["PxC"]) != preC {
		t.Fatalf("PxC addressTxs want %d got %d", preC, len(bc.addressTxs["PxC"]))
	}
	// lastTx pointed at undone tx → reset (nil), not restored to prior tip.
	if bc.lastTx != nil {
		t.Fatalf("lastTx want nil after undo of tip, got %v (pre was %v)", bc.lastTx, preLast)
	}
	if bc.transactions["h1"] != nil || bc.confirmedHashes["h1"] {
		t.Fatal("undone tx still present")
	}
	if bc.transactions["old"] == nil {
		t.Fatal("prior tx removed")
	}
}

func TestPersistFailureUndoesExplorerL2AndLegacy(t *testing.T) {
	for _, requeue := range []string{"pending", "mempool"} {
		t.Run(requeue, func(t *testing.T) {
			dir := t.TempDir()
			// Parent path is a regular file → persistChain cannot create chain file.
			blocker := filepath.Join(dir, "not-a-dir")
			if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
			statePath := filepath.Join(dir, "core-state.json")
			backupPath := statePath + ".l2bak"
			if err := os.WriteFile(statePath, []byte(`{"ok":true}`), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(backupPath, []byte(`{"backup":true}`), 0o644); err != nil {
				t.Fatal(err)
			}

			bc := NewBlockchain()
			bc.SetChainFile(filepath.Join(blocker, "chain.json"))
			tx := &Transaction{Hash: "h-" + requeue, From: "PxA", To: "PxB", Fee: "2"}

			_, _, err := bc.ConfirmExplorerWithoutCore([]*Transaction{tx}, requeue, backupPath, statePath)
			if !IsExplorerPersistAfterApply(err) {
				t.Fatalf("want ErrExplorerPersistAfterApply, got %v", err)
			}
			if len(bc.blockHistory) != 0 {
				t.Fatalf("explorer tip still applied: %d blocks", len(bc.blockHistory))
			}
			if bc.lastTx != nil {
				t.Fatal("lastTx dirty after persist fail")
			}
			if len(bc.addressTxs["PxA"]) != 0 || len(bc.addressTxs["PxB"]) != 0 {
				t.Fatal("addressTxs dirty after persist fail")
			}
			if _, err := os.Stat(backupPath); !os.IsNotExist(err) {
				t.Fatalf("backup should be removed after explorer rollback, stat=%v", err)
			}
			raw, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != `{"backup":true}` {
				t.Fatalf("core state not restored from backup: %s", raw)
			}
			switch requeue {
			case "mempool":
				if len(bc.mempool) != 1 {
					t.Fatalf("legacy requeue: mempool=%d", len(bc.mempool))
				}
			default:
				if len(bc.pendingBlock) != 1 {
					t.Fatalf("L2 requeue: pending=%d", len(bc.pendingBlock))
				}
			}
		})
	}
}
