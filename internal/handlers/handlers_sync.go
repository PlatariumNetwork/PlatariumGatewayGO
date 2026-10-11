package handlers

import (
	"os"
	"path/filepath"
	"sort"

	"platarium-gateway-go/internal/blockchain"
	"platarium-gateway-go/internal/core"
	"platarium-gateway-go/internal/logger"
	"platarium-gateway-go/internal/txpool"
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
		// Always register the path so L2 confirms keep writing the explorer cache,
		// even when the file is missing on first boot or load fails.
		h.blockchain.SetChainFile(chainFile)
		if err := h.blockchain.LoadChainFile(chainFile); err != nil {
			logger.Warn("Chain file load failed (%s): %v", chainFile, err)
		} else {
			head := h.blockchain.HeadBlockNumber()
			logger.Info(
				"Chain metadata loaded from %s (blocks=%d head=%d txs=%d)",
				chainFile,
				len(h.blockchain.GetBlockHistory()),
				head,
				h.blockchain.IndexedTransactionCount(),
			)
			h.rebuildDistributorTotalsFromChain()
		}
	}
	if h.blockchain.RocksEnabled() {
		h.initRocksFromChainFile(chainFile)
	}
	h.openTxPool(stateFile)
	if n, err := h.blockchain.RestoreDurableTxs(); err != nil {
		logger.Warn("Transaction pool restore failed: %v", err)
	} else if n > 0 {
		logger.Info("Restored %d transaction(s) from the pool database into the mempool", n)
	}
	if h.blockchain.RocksEnabled() {
		// Rebuild explorer tx/block index from Rocks after the pool is restored,
		// so a cache tip ahead of Rocks is not written back over the pool.
		go h.hydrateExplorerIndexFromRocksIfNeeded()
	}
}

func (h *Handler) openTxPool(stateFile string) {
	dir := os.Getenv("PLATARIUM_TXPOOL_DIR")
	if dir == "" {
		rocksDir := core.ResolveRocksDBPath(stateFile)
		dir = filepath.Join(filepath.Dir(rocksDir), "txpool")
	}
	pool, err := txpool.Open(dir)
	if err != nil {
		logger.Warn("Transaction pool database open failed (%s): %v", dir, err)
		return
	}
	h.blockchain.SetTxPool(pool)
	logger.Info("Transaction pool database: %s (separate from chain RocksDB, kept across restarts)", dir)
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
}

func (h *Handler) hydrateExplorerIndexFromRocksIfNeeded() {
	if !h.blockchain.RocksEnabled() {
		return
	}
	rocks := h.blockchain.RocksStore()
	if rocks == nil {
		return
	}
	head, err := rocks.RocksGetHead()
	if err != nil {
		logger.Warn("Explorer hydrate: RocksDB head failed: %v", err)
		return
	}

	expected := h.blockchain.ExpectedConfirmedTxCount()
	indexed := h.blockchain.IndexedTransactionCount()
	historyLen := len(h.blockchain.GetBlockHistory())

	// Nothing canonical yet — keep whatever chain.json already loaded.
	if head == 0 && expected == 0 {
		return
	}
	// chain.json is ahead of Rocks. Keep it and write it back so the next
	// restart still has the deals. Do not hydrate over that cache.
	if mem, rocksHead, leads := h.blockchain.ChainCacheLeadsRocks(); leads {
		logger.Info("Explorer keeps chain cache head=%d ahead of Rocks head=%d", mem, rocksHead)
		if err := h.blockchain.PersistChainSnapshot(); err != nil {
			logger.Warn("Persist leading chain cache failed: %v", err)
		}
		return
	}

	needsHydrate := head > 0 && (indexed < expected || indexed == 0 || historyLen == 0)
	if !needsHydrate && head > 0 && indexed == expected && expected > 0 {
		// Still refresh chain.json cache so restarts are instant.
		if err := h.blockchain.PersistChainSnapshot(); err != nil {
			logger.Warn("Persist explorer chain cache failed: %v", err)
		}
		return
	}
	if !needsHydrate {
		return
	}

	logger.Info(
		"Hydrating explorer from RocksDB (head=%d indexed=%d expected=%d history=%d)…",
		head, indexed, expected, historyLen,
	)
	if err := h.blockchain.SyncFromRocksHead(); err != nil {
		logger.Warn("Explorer hydrate SyncFromRocksHead failed: %v", err)
	}
	n, err := h.blockchain.HydrateExplorerIndexFromRocks()
	if err != nil {
		logger.Warn("Explorer index hydrate from RocksDB failed: %v", err)
		return
	}
	logger.Info("Explorer index hydrated from RocksDB (%d txs, blocks=%d)", n, len(h.blockchain.GetBlockHistory()))
	if err := h.blockchain.PersistChainSnapshot(); err != nil {
		logger.Warn("Persist explorer chain cache after hydrate failed: %v", err)
	}
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
		logger.Info("Restored fee totals from chain: burned=%d treasury=%d", burned, treasury)
	}
}
