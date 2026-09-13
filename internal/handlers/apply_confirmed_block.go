package handlers

import (
	"fmt"

	"platarium-gateway-go/internal/blockchain"
	"platarium-gateway-go/internal/logger"
	"platarium-gateway-go/internal/metrics"
)

// ConfirmFailPoint names injectable failure sites on the shared confirm boundary (#59).
type ConfirmFailPoint string

const (
	// FailPointNone disables injection.
	FailPointNone ConfirmFailPoint = ""
	// FailPointAfterExplorerBeforeRocks injects failure after explorer apply/persist
	// and header apply, before Rocks commit — between apply and Rocks (#59).
	FailPointAfterExplorerBeforeRocks ConfirmFailPoint = "after_explorer_before_rocks"
	// FailPointRocksWriteBatch injects Rocks WriteBatch failure after explorer persist (#63).
	FailPointRocksWriteBatch ConfirmFailPoint = "rocks_write_batch"
	// FailPointCrashAfterExplorer leaves explorer tip + backup without undo (crash mid-path #64).
	FailPointCrashAfterExplorer ConfirmFailPoint = "crash_after_explorer"
)

// ConfirmBoundarySteps documents the unified confirm durability boundary (issue #56).
// Shared L2 / legacy / peer finalize path follows these steps in order:
//
//	prepare → apply explorer → persist → Rocks → commit marker → cleanup backup
//
// Backup (.l2bak) must NOT be removed before durable Rocks commit success.
const ConfirmBoundarySteps = "prepare → apply explorer → persist → Rocks → commit marker → cleanup backup"

// ConfirmedBlockFinalizeResult is the outcome of FinalizeConfirmedBlock after explorer persist.
type ConfirmedBlockFinalizeResult struct {
	Block          blockchain.BlockRecord
	RocksCommitted bool
	BackupKept     bool // true if backup retained because Rocks/header failed
}

// SetConfirmFailPoint installs a mid-confirm failure injection point (tests / #59).
func (h *Handler) SetConfirmFailPoint(p ConfirmFailPoint) {
	if h == nil {
		return
	}
	h.confirmFailPoint = p
}

// DurableCommitAfterExplorer is the shared Rocks → commit marker → cleanup/undo half
// used by L2, legacy confirm, and peer AddConfirmedBlock (#56/#60).
// stateBackup must remain on disk until Rocks succeeds; on failure UndoLastConfirmedBlock consumes it.
// When Rocks is disabled, success still discards the backup after explorer persist.
func (h *Handler) DurableCommitAfterExplorer(
	block blockchain.BlockRecord,
	moved []*blockchain.Transaction,
	stateBackup string,
) (ConfirmedBlockFinalizeResult, error) {
	out := ConfirmedBlockFinalizeResult{Block: block}

	// Failure injection point (#59): between explorer apply and Rocks (or persist marker).
	if h != nil && h.confirmFailPoint == FailPointAfterExplorerBeforeRocks {
		logger.Error("confirm fail-point %s: injecting failure before Rocks", FailPointAfterExplorerBeforeRocks)
		if undoErr := h.blockchain.UndoLastConfirmedBlock(block, moved, stateBackup); undoErr != nil {
			logger.Error("fail-point rollback also failed: %v", undoErr)
		}
		out.BackupKept = false
		return out, fmt.Errorf("injected failure at %s: no advanced canonical tip", FailPointAfterExplorerBeforeRocks)
	}

	// Simulated crash mid-path (#64): explorer persisted, backup retained, no Rocks / no undo.
	if h != nil && h.confirmFailPoint == FailPointCrashAfterExplorer {
		logger.Error("confirm fail-point %s: simulating crash after explorer", FailPointCrashAfterExplorer)
		out.BackupKept = true
		return out, fmt.Errorf("injected crash at %s: recovery must rebuild from Rocks", FailPointCrashAfterExplorer)
	}

	// Rocks WriteBatch failure (#63): prior Rocks head unchanged; compensating explorer undo.
	if h != nil && h.confirmFailPoint == FailPointRocksWriteBatch {
		metrics.Global.IncRocksCommitErrors()
		logger.Error("confirm fail-point %s: injecting Rocks WriteBatch failure", FailPointRocksWriteBatch)
		if undoErr := h.blockchain.UndoLastConfirmedBlock(out.Block, moved, stateBackup); undoErr != nil {
			logger.Error("rocks WriteBatch fail-point rollback also failed: %v", undoErr)
		}
		out.BackupKept = false
		return out, fmt.Errorf("rocks WriteBatch failed after confirm: injected at %s", FailPointRocksWriteBatch)
	}

	if err := h.commitBlockToRocks(out.Block, moved, out.Block.StateRoot); err != nil {
		metrics.Global.IncRocksCommitErrors()
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

// FinalizeConfirmedBlock runs the post-explorer durability half of the confirm boundary (#56):
// assemble header → ApplyBlockHeader (persist) → DurableCommitAfterExplorer (Rocks → cleanup).
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

	return h.DurableCommitAfterExplorer(out.Block, moved, stateBackup)
}

// ApplyPeerConfirmedBlock routes peer block_confirmed through the shared confirm primitive (#60):
// AddConfirmedBlock (prepare/apply/persist, backup retained) → DurableCommitAfterExplorer.
func (h *Handler) ApplyPeerConfirmedBlock(block blockchain.BlockRecord, txs []*blockchain.Transaction) (bool, error) {
	added, backupPath, err := h.blockchain.AddConfirmedBlock(block, txs)
	if err != nil {
		return false, err
	}
	if !added {
		return false, nil
	}
	if _, err := h.DurableCommitAfterExplorer(block, txs, backupPath); err != nil {
		return false, err
	}
	return true, nil
}
