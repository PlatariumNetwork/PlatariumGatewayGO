package blockchain

import "testing"

func TestPageBlocksDefaultIsBounded(t *testing.T) {
	blocks := make([]BlockRecord, 80)
	for i := range blocks {
		blocks[i] = BlockRecord{BlockNumber: int64(i)}
	}
	got := PageBlocks(blocks, BlockPageQuery{})
	if len(got) != DefaultBlockPageLimit {
		t.Fatalf("len %d", len(got))
	}
	if got[0].BlockNumber != 79 {
		t.Fatalf("newest %d", got[0].BlockNumber)
	}
	full := PageBlocks(blocks, BlockPageQuery{Full: true})
	if len(full) != 80 {
		t.Fatalf("full %d", len(full))
	}
}
