package handlers

import (
	"os"
	"sort"

	"platarium-gateway-go/internal/blockchain"
	"platarium-gateway-go/internal/logger"
)

func (h *Handler) RegisterSyncCallbacks() {
	h.nodesManager.SetChainHeadCallback(func() int64 {
		return h.blockchain.HeadBlockNumber()
	})
	h.nodesManager.SetSyncRespondCallback(h.onSyncRespond)
	h.nodesManager.SetSyncApplyCallback(h.onSyncApply)
}

func (h *Handler) onSyncRespond(fromBlock int64) map[string]interface{} {
	blocks := h.blockchain.ExportBlocksForSync(fromBlock)
	head := h.blockchain.HeadBlock()
	resp := map[string]interface{}{
		"nodeId":          h.nodesManager.GetNodeID(),
		"fromBlockNumber": fromBlock,
		"blocks":          blocks,
	}
	if head != nil {
		resp["headBlockNumber"] = head.BlockNumber
		resp["headBlockHash"] = head.BlockHash
		resp["stateRoot"] = head.StateRoot
	} else {
		resp["headBlockNumber"] = int64(-1)
	}
	return resp
}

func (h *Handler) onSyncApply(data map[string]interface{}) {
	blocksRaw, ok := data["blocks"].([]interface{})
	if !ok || len(blocksRaw) == 0 {
		return
	}
	entries := make([]map[string]interface{}, 0, len(blocksRaw))
	for _, raw := range blocksRaw {
		if m, ok := raw.(map[string]interface{}); ok {
			entries = append(entries, m)
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		a, _ := entries[i]["blockNumber"].(float64)
		b, _ := entries[j]["blockNumber"].(float64)
		return a < b
	})
	applied := 0
	for _, entry := range entries {
		blockNum, _ := entry["blockNumber"].(float64)
		if int64(blockNum) <= h.blockchain.HeadBlockNumber() {
			continue
		}
		h.onBlockConfirmed(entry)
		applied++
	}
	if applied > 0 {
		logger.Info("Chain sync applied %d block(s), head=%d", applied, h.blockchain.HeadBlockNumber())
	}
}

func (h *Handler) initChainPersistence(stateFile string) {
	chainFile := os.Getenv("PLATARIUM_CHAIN_FILE")
	if chainFile == "" && stateFile != "" {
		chainFile = blockchain.ChainFileFromStateFile(stateFile)
	}
	if chainFile == "" {
		return
	}
	if err := h.blockchain.LoadChainFile(chainFile); err != nil {
		logger.Warn("Chain file load failed (%s): %v", chainFile, err)
		return
	}
	h.blockchain.SetChainFile(chainFile)
	head := h.blockchain.HeadBlockNumber()
	logger.Info("Chain loaded from %s (blocks=%d head=%d)", chainFile, len(h.blockchain.GetBlockHistory()), head)
	h.rebuildDistributorTotalsFromChain()
}

func (h *Handler) rebuildDistributorTotalsFromChain() {
	cfg := h.distributor.GetConfig()
	var burned, treasury int64
	for _, block := range h.blockchain.GetBlockHistory() {
		if block.TotalFees <= 0 {
			continue
		}
		sr := cfg.SplitBlockFees(block.TotalFees)
		burned += sr.Burn
		treasury += sr.Treasury
	}
	if burned > 0 || treasury > 0 {
		h.distributor.SetTotals(burned, treasury)
		logger.Info("Fee distribution totals rebuilt from chain: burned=%d treasury=%d (burn=%d%%)", burned, treasury, cfg.BurnPct)
	}
}
