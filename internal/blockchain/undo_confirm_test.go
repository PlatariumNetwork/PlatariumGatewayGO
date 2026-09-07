package blockchain

import "testing"

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
