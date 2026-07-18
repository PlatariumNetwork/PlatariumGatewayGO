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
	if chainFile != "" {
		if err := h.blockchain.LoadChainFile(chainFile); err != nil {
			logger.Warn("Chain file load failed (%s): %v", chainFile, err)
		} else {
			h.blockchain.SetChainFile(chainFile)
			head := h.blockchain.HeadBlockNumber()
			logger.Info("Chain metadata loaded from %s (blocks=%d head=%d)", chainFile, len(h.blockchain.GetBlockHistory()), head)
			h.rebuildDistributorTotalsFromChain()
		}
	}
	if h.blockchain.RocksEnabled() {
		h.initRocksFromChainFile(chainFile)
	}
}

func (h *Handler) initRocksFromChainFile(chainFile string) {
	rocks := h.blockchain.RocksStore()
	if rocks == nil {
		return
	}
	head, err := rocks.RocksGetHead()
	if err != nil {
		logger.Warn("RocksDB head read failed: %v", err)
		return
	}
	if head == 0 && chainFile != "" {
		if _, statErr := os.Stat(chainFile); statErr == nil {
			if err := rocks.MigrateJSONToRocks(chainFile, ""); err != nil {
				logger.Warn("migrate-json-to-rocks failed: %v", err)
			} else if head, err = rocks.RocksGetHead(); err == nil {
				logger.Info("Migrated legacy chain JSON to RocksDB (head=%d)", head)
			}
		}
	}
	if err := h.blockchain.SyncFromRocksHead(); err != nil {
		logger.Warn("Sync block counter from RocksDB failed: %v", err)
	} else if head, err := rocks.RocksGetHead(); err == nil {
		logger.Info("RocksDB canonical head=%d gatewayHead=%d", head, h.blockchain.HeadBlockNumber())
	}

	// chain.json may be stale: while Rocks is canonical, persistChain used to be a
	// no-op, so restarts only kept old explorer txs. Rebuild the in-memory index
	// from Rocks whenever it is incomplete relative to blockHistory.
	expected := h.blockchain.ExpectedConfirmedTxCount()
	indexed := h.blockchain.IndexedTransactionCount()
	if expected > 0 && indexed < expected {
		logger.Info("Explorer index incomplete (%d/%d txs) — hydrating from RocksDB…", indexed, expected)
		n, err := h.blockchain.HydrateExplorerIndexFromRocks()
		if err != nil {
			logger.Warn("Explorer index hydrate from RocksDB failed: %v", err)
			return
		}
		logger.Info("Explorer index hydrated from RocksDB (%d txs)", n)
		if err := h.blockchain.PersistChainSnapshot(); err != nil {
			logger.Warn("Persist explorer chain cache after hydrate failed: %v", err)
		}
	}
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
