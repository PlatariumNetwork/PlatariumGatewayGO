package blockchain

import "testing"

func TestCanonicalHeadFreezesExplorerLeadingRocks(t *testing.T) {
	// Explorer tip at gateway block 2, Rocks head at gateway block 0 → serve Rocks.
	got := CanonicalHeadNumber(2, 0, true)
	if got != 0 {
		t.Fatalf("canonical head=%d want 0 (frozen to Rocks)", got)
	}
	if !TipLeadsRocks(2, 0, true) {
		t.Fatal("expected tip leading Rocks")
	}
	if TipLeadsRocks(0, 0, true) {
		t.Fatal("equal tips should not lead")
	}
	if CanonicalHeadNumber(5, -1, false) != 5 {
		t.Fatal("Rocks off should serve mem tip")
	}
	if CanonicalHeadNumber(5, -1, true) != -1 {
		t.Fatal("empty Rocks SoT should freeze to -1")
	}
}

func TestFreezeBlockHistoryDropsLeadingTip(t *testing.T) {
	history := []BlockRecord{
		{BlockNumber: 0, BlockHash: "h0"},
		{BlockNumber: 1, BlockHash: "h1"},
		{BlockNumber: 2, BlockHash: "h2"},
	}
	frozen := FreezeBlockHistory(history, 0, true)
	if len(frozen) != 1 || frozen[0].BlockHash != "h0" {
		t.Fatalf("frozen=%+v", frozen)
	}
	if len(FreezeBlockHistory(history, -1, true)) != 0 {
		t.Fatal("empty Rocks should drop all explorer tips")
	}
	if len(FreezeBlockHistory(history, 2, false)) != 3 {
		t.Fatal("Rocks off should not freeze")
	}
}

func TestHeadBlockNumberUsesTipFreezeProbe(t *testing.T) {
	bc := NewBlockchain()
	bc.mu.Lock()
	bc.blockHistory = []BlockRecord{
		{BlockNumber: 0, BlockHash: "a"},
		{BlockNumber: 1, BlockHash: "b"},
	}
	bc.blockCounter = 2
	bc.mu.Unlock()

	bc.SetRocksHeadProbe(func() (int64, bool) { return 0, true })
	if got := bc.HeadBlockNumber(); got != 0 {
		t.Fatalf("HeadBlockNumber=%d want 0", got)
	}
	hist := bc.GetBlockHistory()
	if len(hist) != 1 || hist[0].BlockNumber != 0 {
		t.Fatalf("GetBlockHistory=%+v", hist)
	}
	if bc.GetBlockByNumber(1) != nil {
		t.Fatal("leading block must not be served")
	}
	if tip := bc.HeadBlock(); tip == nil || tip.BlockNumber != 0 || tip.BlockHash != "a" {
		t.Fatalf("HeadBlock=%+v", tip)
	}
}
