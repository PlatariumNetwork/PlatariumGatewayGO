package blockchain

import (
	"testing"

	"platarium-gateway-go/internal/core"
)

// M6: RocksEnabled gates authoritative fail-closed paths in handlers.
func TestRocksEnabledRequiresClient(t *testing.T) {
	bc := NewBlockchain()
	if bc.RocksEnabled() {
		t.Fatal("expected Rocks disabled without client")
	}
	// Nil client stays disabled.
	bc.SetRocksStore(nil)
	if bc.RocksEnabled() {
		t.Fatal("nil rocks must stay disabled")
	}
	_ = core.RocksStoreClient{}
}
