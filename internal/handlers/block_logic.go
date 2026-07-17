package handlers

import (
	"fmt"
	"time"

	"platarium-gateway-go/internal/blockchain"
	"platarium-gateway-go/internal/core"
	"platarium-gateway-go/internal/logger"
)

func (h *Handler) selectTxsForBlockCollect() []*blockchain.Transaction {
	if h.rustCore == nil || h.blockchain.Ledger() == nil {
		logger.Warn("Core unavailable: block transaction selection stopped")
		return nil
	}

	snap, err := h.blockchain.MempoolSnapshotJSON()
	if err != nil {
		logger.Warn("core select_block_txs snapshot failed: %v", err)
		return nil
	}
	res, err := h.rustCore.SelectBlockTxs(h.blockchain.Ledger().StateFilePath(), snap)
	if err != nil {
		logger.Warn("core select_block_txs failed: %v", err)
		return nil
	}
	return h.blockchain.SelectTxsByHashes(res.Hashes)
}

func (h *Handler) coreBlockProposalStatus() (*core.BlockProposalStatusResult, error) {
	if h.rustCore == nil {
		return nil, fmt.Errorf("rust core unavailable")
	}
	snap, err := h.blockchain.MempoolSnapshotJSON()
	if err != nil {
		return nil, err
	}
	status, err := h.rustCore.BlockProposalStatus(snap, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	return status, nil
}
