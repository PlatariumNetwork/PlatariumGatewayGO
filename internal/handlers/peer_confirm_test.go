package handlers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"platarium-gateway-go/internal/blockchain"
	"platarium-gateway-go/internal/metrics"
)

// Issue #61: peer confirm explorer write failure → full rollback, no half tip.
func TestPeerConfirmExplorerWriteFailureRollsBack(t *testing.T) {
	dir := t.TempDir()
	bc := blockchain.NewBlockchain()
	bc.SetExplorerOnlyConfirm(true)
	bc.SetChainFile(filepath.Join(dir, "chain.json"))
	bc.SetPeerConfirmInject(blockchain.PeerInjectExplorerWrite)

	tx := &blockchain.Transaction{Hash: "peer-exw", From: "PxA", To: "PxB", Fee: "1"}
	block := blockchain.BlockRecord{
		BlockNumber: 0,
		BlockHash:   "hash-exw",
		TxHashes:    []string{tx.Hash},
		TxCount:     1,
		Timestamp:   1,
	}
	h := &Handler{blockchain: bc}
	added, err := h.ApplyPeerConfirmedBlock(block, []*blockchain.Transaction{tx})
	if err == nil || !strings.Contains(err.Error(), "explorer write failed") {
		t.Fatalf("want explorer write failure, got added=%v err=%v", added, err)
	}
	if added {
		t.Fatal("must not report added on explorer write failure")
	}
	if len(bc.GetBlockHistory()) != 0 {
		t.Fatalf("half tip left after explorer write fail: %+v", bc.GetBlockHistory())
	}
	if bc.HeadBlockNumber() != -1 {
		t.Fatalf("head=%d want -1 (no half tip)", bc.HeadBlockNumber())
	}
	if _, err := os.Stat(filepath.Join(dir, "chain.json")); !os.IsNotExist(err) {
		t.Fatal("chain.json must not remain after explorer write rollback")
	}
}

// Issue #62: peer confirm chain.json persist failure → explorer side effects rolled back.
func TestPeerConfirmChainJSONWriteFailureRollsBackExplorer(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	bc := blockchain.NewBlockchain()
	bc.SetExplorerOnlyConfirm(true)
	bc.SetChainFile(filepath.Join(blocker, "chain.json"))

	tx := &blockchain.Transaction{Hash: "peer-cj", From: "PxA", To: "PxB", Fee: "1"}
	block := blockchain.BlockRecord{
		BlockNumber: 0,
		BlockHash:   "hash-cj",
		TxHashes:    []string{tx.Hash},
		TxCount:     1,
		Timestamp:   1,
	}
	h := &Handler{blockchain: bc}
	added, err := h.ApplyPeerConfirmedBlock(block, []*blockchain.Transaction{tx})
	if !blockchain.IsExplorerPersistAfterApply(err) {
		t.Fatalf("want ErrExplorerPersistAfterApply, got added=%v err=%v", added, err)
	}
	if added {
		t.Fatal("must not report added on chain.json persist failure")
	}
	if len(bc.GetBlockHistory()) != 0 {
		t.Fatal("explorer tip still applied after persist fail")
	}
	if bc.GetTransaction(tx.Hash) != nil {
		t.Fatal("explorer confirmed tx index dirty after persist fail")
	}
	// Compensating requeue to mempool is expected; confirmed tip must stay clear.
	if tip := bc.HeadBlock(); tip != nil {
		t.Fatalf("half tip after chain.json fail: %+v", tip)
	}
	mp := bc.GetMempool()
	if len(mp) != 1 || mp[0].Hash != tx.Hash {
		t.Fatalf("want tx requeued to mempool after persist fail, got %d", len(mp))
	}
}

// Issue #63: Rocks WriteBatch failure leaves prior Rocks head; compensating undo applied.
func TestPeerConfirmRocksWriteBatchFailureKeepsPriorHead(t *testing.T) {
	metrics.Global.ResetForTest()
	dir := t.TempDir()
	bc := blockchain.NewBlockchain()
	bc.SetExplorerOnlyConfirm(true)
	bc.SetChainFile(filepath.Join(dir, "chain.json"))
	// Prior Rocks head at genesis (gateway -1); probe treats Rocks as SoT.
	priorRocksHead := int64(-1)
	bc.SetRocksHeadProbe(func() (int64, bool) { return priorRocksHead, true })

	tx := &blockchain.Transaction{Hash: "peer-rwb", From: "PxA", To: "PxB", Fee: "1"}
	block := blockchain.BlockRecord{
		BlockNumber: 0,
		BlockHash:   "hash-rwb",
		TxHashes:    []string{tx.Hash},
		TxCount:     1,
		Timestamp:   1,
		StateRoot:   "sr0",
	}
	backup := filepath.Join(dir, "core-state.json.l2bak")
	if err := os.WriteFile(backup, []byte(`{"prior":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	h := &Handler{blockchain: bc}
	// First stage explorer via AddConfirmedBlock, then fail Rocks WriteBatch in durable half.
	added, backupPath, err := bc.AddConfirmedBlock(block, []*blockchain.Transaction{tx})
	if err != nil || !added {
		t.Fatalf("stage explorer: added=%v err=%v", added, err)
	}
	if backupPath == "" {
		backupPath = backup
	}
	h.SetConfirmFailPoint(FailPointRocksWriteBatch)
	_, err = h.DurableCommitAfterExplorer(block, []*blockchain.Transaction{tx}, backupPath)
	if err == nil || !strings.Contains(err.Error(), string(FailPointRocksWriteBatch)) {
		t.Fatalf("want Rocks WriteBatch fail-point, got %v", err)
	}
	if snap := metrics.Global.Snapshot(); snap.RocksCommitErrors < 1 {
		t.Fatalf("want rocks_commit_errors incremented, got %+v", snap)
	}
	// Compensating undo: explorer tip gone.
	if len(bc.GetBlockHistory()) != 0 {
		t.Fatal("explorer tip not undone after Rocks WriteBatch fail")
	}
	// Prior Rocks head unchanged; served tip must not lead Rocks.
	if got := bc.HeadBlockNumber(); got != priorRocksHead {
		t.Fatalf("served head=%d want prior Rocks head %d", got, priorRocksHead)
	}
}

// Issue #64: crash mid-path → restart leaves recoverable non-divergent tip (ADR tip freeze).
func TestPeerConfirmCrashMidPathRecovery(t *testing.T) {
	dir := t.TempDir()
	chainPath := filepath.Join(dir, "chain.json")
	bc := blockchain.NewBlockchain()
	bc.SetExplorerOnlyConfirm(true)
	bc.SetChainFile(chainPath)

	tx := &blockchain.Transaction{Hash: "peer-crash", From: "PxA", To: "PxB", Fee: "1"}
	block := blockchain.BlockRecord{
		BlockNumber: 0,
		BlockHash:   "hash-crash",
		TxHashes:    []string{tx.Hash},
		TxCount:     1,
		Timestamp:   1,
	}
	backup := filepath.Join(dir, "core-state.json.l2bak")
	if err := os.WriteFile(backup, []byte(`{"crash":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	h := &Handler{blockchain: bc}
	added, backupPath, err := bc.AddConfirmedBlock(block, []*blockchain.Transaction{tx})
	if err != nil || !added {
		t.Fatalf("stage explorer: added=%v err=%v", added, err)
	}
	if backupPath == "" {
		backupPath = backup
	}
	h.SetConfirmFailPoint(FailPointCrashAfterExplorer)
	res, err := h.DurableCommitAfterExplorer(block, []*blockchain.Transaction{tx}, backupPath)
	if err == nil || !strings.Contains(err.Error(), string(FailPointCrashAfterExplorer)) {
		t.Fatalf("want crash fail-point, got %v", err)
	}
	if !res.BackupKept {
		t.Fatal("crash must retain backup for recovery")
	}
	if _, statErr := os.Stat(backupPath); os.IsNotExist(statErr) {
		t.Fatal("backup missing after simulated crash")
	}
	// Explorer tip was written to chain.json (rebuildable cache ahead of Rocks).
	if len(bc.GetBlockHistory()) != 1 {
		t.Fatalf("pre-crash explorer tip missing: %d", len(bc.GetBlockHistory()))
	}

	// Restart: load chain.json, Rocks SoT still at genesis → freeze leading tip (ADR).
	restarted := blockchain.NewBlockchain()
	if err := restarted.LoadChainFile(chainPath); err != nil {
		t.Fatal(err)
	}
	rocksHead := int64(-1) // no committed Rocks tip
	restarted.SetRocksHeadProbe(func() (int64, bool) { return rocksHead, true })
	if got := restarted.HeadBlockNumber(); got != rocksHead {
		t.Fatalf("after restart served head=%d want Rocks %d (non-divergent)", got, rocksHead)
	}
	if hist := restarted.GetBlockHistory(); len(hist) != 0 {
		t.Fatalf("leading explorer tip must not be served after crash recovery: %+v", hist)
	}
	if tip := restarted.HeadBlock(); tip != nil {
		t.Fatalf("canonical tip must be empty after crash before Rocks commit, got %+v", tip)
	}
}

// Issue #65: re-applying the same confirmed peer block is idempotent / safe.
func TestDuplicatePeerBlockIdempotent(t *testing.T) {
	dir := t.TempDir()
	bc := blockchain.NewBlockchain()
	bc.SetExplorerOnlyConfirm(true)
	bc.SetChainFile(filepath.Join(dir, "chain.json"))

	tx := &blockchain.Transaction{Hash: "peer-dup", From: "PxA", To: "PxB", Fee: "1"}
	block := blockchain.BlockRecord{
		BlockNumber: 0,
		BlockHash:   "hash-dup",
		TxHashes:    []string{tx.Hash},
		TxCount:     1,
		Timestamp:   1,
	}
	h := &Handler{blockchain: bc}
	added, err := h.ApplyPeerConfirmedBlock(block, []*blockchain.Transaction{tx})
	if err != nil || !added {
		t.Fatalf("first apply: added=%v err=%v", added, err)
	}
	histLen := len(bc.GetBlockHistory())
	added2, err2 := h.ApplyPeerConfirmedBlock(block, []*blockchain.Transaction{tx})
	if err2 != nil {
		t.Fatalf("duplicate must be safe, got err=%v", err2)
	}
	if added2 {
		t.Fatal("duplicate must not re-add")
	}
	if len(bc.GetBlockHistory()) != histLen {
		t.Fatalf("history grew on duplicate: %d → %d", histLen, len(bc.GetBlockHistory()))
	}
	if tip := bc.HeadBlock(); tip == nil || tip.BlockHash != "hash-dup" {
		t.Fatalf("local tip overwritten: %+v", tip)
	}
}

// Issue #66: conflicting peer block → ErrForkConflict; no silent overwrite.
func TestConflictingPeerBlockErrForkConflict(t *testing.T) {
	dir := t.TempDir()
	bc := blockchain.NewBlockchain()
	bc.SetExplorerOnlyConfirm(true)
	bc.SetChainFile(filepath.Join(dir, "chain.json"))

	tx := &blockchain.Transaction{Hash: "peer-local", From: "PxA", To: "PxB", Fee: "1"}
	local := blockchain.BlockRecord{
		BlockNumber: 0,
		BlockHash:   "hash-local",
		L2Yes:       3,
		TxHashes:    []string{tx.Hash},
		TxCount:     1,
		Timestamp:   1,
	}
	h := &Handler{blockchain: bc}
	added, err := h.ApplyPeerConfirmedBlock(local, []*blockchain.Transaction{tx})
	if err != nil || !added {
		t.Fatalf("local apply: added=%v err=%v", added, err)
	}

	conflictTx := &blockchain.Transaction{Hash: "peer-fork", From: "PxC", To: "PxD", Fee: "1"}
	conflict := blockchain.BlockRecord{
		BlockNumber: 0,
		BlockHash:   "hash-fork",
		L2Yes:       1,
		TxHashes:    []string{conflictTx.Hash},
		TxCount:     1,
		Timestamp:   2,
	}
	added2, err2 := h.ApplyPeerConfirmedBlock(conflict, []*blockchain.Transaction{conflictTx})
	if !blockchain.IsForkConflict(err2) {
		t.Fatalf("want ErrForkConflict, got added=%v err=%v", added2, err2)
	}
	if added2 {
		t.Fatal("conflict must not add")
	}
	tip := bc.HeadBlock()
	if tip == nil || tip.BlockHash != "hash-local" {
		t.Fatalf("silent overwrite: tip=%+v", tip)
	}
	if bc.GetTransaction(conflictTx.Hash) != nil {
		t.Fatal("conflicting txs must not enter explorer index")
	}
}
