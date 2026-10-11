package blockchain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func TestLoadRebuildsBlocksAndKeepsDealOnConfirmedList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chain.json")
	raw, err := json.Marshal(ChainFileData{
		Version:      chainFileVersion,
		BlockCounter: 2,
		Blocks: []BlockRecord{{
			BlockNumber: 19302,
			Timestamp:   1787346877,
			TxHashes:    []string{"old"},
			TxCount:     1,
		}},
		Transactions: []*Transaction{
			{Hash: "old", BlockNumber: 19302, Timestamp: 1787346877, From: "PxA", To: "PxB"},
			{
				Hash:      "efec8e15cd6646e789391c65b4f9e3c5",
				Type:      "escrow_lock",
				Purpose:   "deal",
				EscrowID:  "efec8e15cd6646e789391c65b4f9e3c5",
				Timestamp: 1800000000,
				From:      "PxA",
				To:        "PxB",
				Value:     "100000000",
			},
			{Hash: "later", BlockNumber: 19310, Timestamp: 1790000000, From: "PxA", To: "PxB"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	bc := NewBlockchain()
	if err := bc.LoadChainFile(path); err != nil {
		t.Fatal(err)
	}
	if got := bc.MemoryHeadBlockNumber(); got != 19310 {
		t.Fatalf("memory head=%d want 19310", got)
	}
	refs := bc.ConfirmedTxRefsNewestFirst()
	if len(refs) < 3 {
		t.Fatalf("refs=%+v", refs)
	}
	if refs[0].Hash != "efec8e15cd6646e789391c65b4f9e3c5" {
		t.Fatalf("newest ref=%+v", refs[0])
	}
	foundLater := false
	for _, ref := range refs {
		if ref.Hash == "later" && ref.BlockNumber == 19310 {
			foundLater = true
		}
	}
	if !foundLater {
		t.Fatalf("rebuilt block tx missing: %+v", refs)
	}

	bc.mu.Lock()
	bc.blockHistory = []BlockRecord{{BlockNumber: 19302, TxHashes: []string{"old"}, TxCount: 1}}
	err = bc.persistChain()
	bc.mu.Unlock()
	if err == nil || !strings.Contains(err.Error(), "refuse to shrink") {
		t.Fatalf("shrink err=%v", err)
	}
}

func TestConfirmedListHidesBlocksAheadOfRocks(t *testing.T) {
	bc := NewBlockchain()
	bc.mu.Lock()
	bc.blockHistory = []BlockRecord{{
		BlockNumber: 19302,
		Timestamp:   1787346877,
		TxHashes:    []string{"old"},
		TxCount:     1,
	}}
	bc.transactions["old"] = &Transaction{Hash: "old", BlockNumber: 19302, Timestamp: 1787346877}
	bc.transactions["deal"] = &Transaction{
		Hash:        "deal",
		Type:        "escrow_lock",
		Purpose:     "deal",
		BlockNumber: 19304,
		Timestamp:   1791679578,
	}
	bc.transactions["send"] = &Transaction{
		Hash:        "send",
		Type:        "transfer",
		BlockNumber: 19304,
		Timestamp:   1791679500,
	}
	bc.mu.Unlock()
	bc.SetRocksHeadProbe(func() (int64, bool) { return 19302, true })

	refs := bc.ConfirmedTxRefsNewestFirst()
	for _, ref := range refs {
		if ref.BlockNumber > 19302 {
			t.Fatalf("served block %d for %s ahead of rocks head", ref.BlockNumber, ref.Hash)
		}
		if ref.Hash == "deal" || ref.Hash == "send" {
			t.Fatalf("phantom tx %s still on confirmed list as block %d", ref.Hash, ref.BlockNumber)
		}
	}
	if bc.GetBlockByNumber(19304) != nil {
		t.Fatal("block 19304 must stay unservable above rocks head")
	}
	if bc.GetBlockByNumber(19302) == nil {
		t.Fatal("block 19302 missing")
	}
}
