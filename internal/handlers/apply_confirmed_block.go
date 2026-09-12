package handlers

import (
	"fmt"

	"platarium-gateway-go/internal/blockchain"
	"platarium-gateway-go/internal/logger"
)

// ConfirmBoundarySteps documents the unified confirm durability boundary (issue #56).
// Shared L2 / legacy finalize path follows these steps in order:
//
//	prepare → apply explorer → persist → Rocks → commit marker → cleanup backup
//
// Backup (.l2bak) must NOT be removed before durable Rocks commit success.
// This is a bounded prototype: peer AddConfirmedBlock may still use a subset of steps.
const ConfirmBoundarySteps = "prepare → apply explorer → persist → Rocks → commit marker → cleanup backup"

// ConfirmedBlockFinalizeResult is the outcome of FinalizeConfirmedBlock after explorer persist.
type ConfirmedBlockFinalizeResult struct {
	Block          blockchain.BlockRecord
	RocksCommitted bool
	BackupKept     bool // true if backup retained because Rocks/header failed
}

// FinalizeConfirmedBlock runs the post-explorer durability half of the confirm boundary (#56):
// assemble header → ApplyBlockHeader (persist) → Rocks commit (commit marker) → cleanup backup.
//
// stateBackup must remain on disk until Rocks succeeds; on failure UndoLastConfirmedBlock consumes it.
// When Rocks is disabled, header success still discards the backup after explorer persist.
func (h *Handler) FinalizeConfirmedBlock(
	block blockchain.BlockRecord,
	moved []*blockchain.Transaction,
	stateBackup string,
	producerID string,
) (ConfirmedBlockFinalizeResult, error) {
	out := ConfirmedBlockFinalizeResult{Block: block}

	header, hdrErr := h.assembleBlockHeader(block.BlockNumber, moved, producerID, block.Timestamp)
	if hdrErr != nil {
		if h.blockchain.RocksEnabled() {
			logger.Error("assemble-block failed with Rocks enabled: %v", hdrErr)
			if undoErr := h.blockchain.UndoLastConfirmedBlock(block, moved, stateBackup); undoErr != nil {
				logger.Error("assemble-fail rollback also failed: %v", undoErr)
			}
			out.BackupKept = false // undo consumed backup
			return out, fmt.Errorf("assemble-block required when Rocks is authoritative: %w", hdrErr)
		}
		blockchain.DiscardStateBackup(stateBackup)
		logger.Warn("assemble-block failed (Rocks disabled): %v", hdrErr)
		if err := h.blockchain.PersistChainSnapshot(); err != nil {
			logger.Warn("Persist chain after confirm (no header) failed: %v", err)
		}
		return out, nil
	}

	h.blockchain.ApplyBlockHeader(block.BlockNumber, *header, producerID)
	out.Block.BlockHash = header.BlockHash
	out.Block.MerkleRoot = header.MerkleRoot
	out.Block.StateRoot = header.StateRoot
	out.Block.PreviousHash = header.PreviousHash
	out.Block.ProducerNodeID = producerID

	if err := h.commitBlockToRocks(out.Block, moved, header.StateRoot); err != nil {
		logger.Error("RocksDB commit after confirm FAILED: %v", err)
		if undoErr := h.blockchain.UndoLastConfirmedBlock(out.Block, moved, stateBackup); undoErr != nil {
			logger.Error("rocks-fail rollback also failed: %v", undoErr)
		}
		out.BackupKept = false
		return out, fmt.Errorf("rocks commit failed after confirm: %w", err)
	}
	out.RocksCommitted = h.blockchain.RocksEnabled()
	// Commit marker: Rocks head advanced (or Rocks disabled and explorer persisted with header).
	// Only now is it safe to remove the Core state backup.
	blockchain.DiscardStateBackup(stateBackup)
	return out, nil
}
