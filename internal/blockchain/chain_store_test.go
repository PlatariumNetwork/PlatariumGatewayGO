package blockchain

import (
	"os"
	"path/filepath"
	"testing"
)

func TestChainFileFromStateFile(t *testing.T) {
	got := ChainFileFromStateFile("/tmp/data/state-node0.json")
	want := filepath.Join("/tmp/data", "chain-node0.json")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestChainFileSaveLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chain-test.json")
	bc := NewBlockchain()
	bc.SetChainFile(path)
	bc.mu.Lock()
	bc.blockCounter = 2
	bc.blockHistory = []BlockRecord{{
		BlockNumber: 0,
		BlockHash:   "abc",
		TxHashes:    []string{"tx1"},
		TxCount:     1,
	}}
	bc.transactions["tx1"] = &Transaction{Hash: "tx1", From: "a", To: "b", Value: "1"}
	bc.mu.Unlock()
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if err := bc.persistChain(); err != nil {
		t.Fatalf("persist: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("chain file missing: %v", err)
	}

	restored := NewBlockchain()
	if err := restored.LoadChainFile(path); err != nil {
		t.Fatalf("load: %v", err)
	}
	if restored.HeadBlockNumber() != 0 {
		t.Fatalf("head=%d", restored.HeadBlockNumber())
	}
	if restored.transactions["tx1"] == nil {
		t.Fatal("tx missing after load")
	}
}
