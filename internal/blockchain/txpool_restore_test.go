package blockchain

import (
	"path/filepath"
	"testing"

	"platarium-gateway-go/internal/txpool"
)

func TestRestoreSkipsTxsOlderThanCanonicalHead(t *testing.T) {
	dir := t.TempDir()
	pool, err := txpool.Open(filepath.Join(dir, "txpool"))
	if err != nil {
		t.Fatal(err)
	}
	bc := NewBlockchain()
	bc.SetTxPool(pool)
	bc.SetRocksHeadProbe(func() (int64, bool) { return 19302, true })
	bc.mu.Lock()
	bc.blockHistory = []BlockRecord{{
		BlockNumber: 19302,
		Timestamp:   1787346877,
		TxHashes:    []string{"old"},
		TxCount:     1,
	}}
	bc.transactions["ancient"] = &Transaction{
		Hash: "ancient", Type: "transfer", Timestamp: 1784584201, SigMain: "sig", From: "PxA",
	}
	bc.transactions["fresh"] = &Transaction{
		Hash: "fresh", Type: "transfer", Timestamp: 1791679578, SigMain: "sig", From: "PxA",
	}
	bc.mu.Unlock()

	n, err := bc.RestoreDurableTxs()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("restored %d", n)
	}
	mem := bc.GetMempool()
	if len(mem) != 1 || mem[0].Hash != "fresh" {
		t.Fatalf("mempool %+v", mem)
	}
}

func TestRestorePutsUncommittedTxBackInMempool(t *testing.T) {
	dir := t.TempDir()
	pool, err := txpool.Open(filepath.Join(dir, "txpool"))
	if err != nil {
		t.Fatal(err)
	}
	bc := NewBlockchain()
	bc.SetTxPool(pool)
	bc.mu.Lock()
	bc.blockHistory = []BlockRecord{{
		BlockNumber: 19302,
		TxHashes:    []string{"old"},
		TxCount:     1,
	}}
	bc.blockCounter = 19305
	bc.durableHead = 19304
	bc.transactions["old"] = &Transaction{Hash: "old", BlockNumber: 19302, SigMain: "sig"}
	bc.transactions["send"] = &Transaction{
		Hash: "send", Type: "transfer", BlockNumber: 19304, From: "PxA", To: "PxB", SigMain: "sig",
	}
	bc.confirmedHashes = map[string]bool{"old": true, "send": true}
	bc.blockHistory = append(bc.blockHistory, BlockRecord{
		BlockNumber: 19304,
		TxHashes:    []string{"send"},
		TxCount:     1,
	})
	bc.mu.Unlock()
	bc.SetRocksHeadProbe(func() (int64, bool) { return 19302, true })

	n, err := bc.RestoreDurableTxs()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("restored %d", n)
	}
	if bc.GetBlockByNumber(19304) != nil {
		t.Fatal("phantom block still served")
	}
	mem := bc.GetMempool()
	if len(mem) != 1 || mem[0].Hash != "send" || mem[0].BlockNumber != 0 {
		t.Fatalf("mempool %+v", mem)
	}
	if bc.HeadBlockNumber() != 19302 {
		t.Fatalf("head %d", bc.HeadBlockNumber())
	}

	restarted := NewBlockchain()
	restarted.SetTxPool(pool)
	restarted.SetRocksHeadProbe(func() (int64, bool) { return 19302, true })
	n, err = restarted.RestoreDurableTxs()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(restarted.GetMempool()) != 1 {
		t.Fatalf("second boot restored %d mempool %d", n, len(restarted.GetMempool()))
	}

	bc.ForgetTxPool([]string{"send"})
	empty := NewBlockchain()
	empty.SetTxPool(pool)
	n, err = empty.RestoreDurableTxs()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("after forget restored %d", n)
	}
}
