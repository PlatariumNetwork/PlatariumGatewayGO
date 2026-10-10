package blockchain

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const chainFileVersion = 1

// ChainFileData is the on-disk chain snapshot for chain persistence.
type ChainFileData struct {
	Version            int            `json:"version"`
	BlockCounter       int64          `json:"blockCounter"`
	TotalFeesCollected int64          `json:"totalFeesCollected"`
	Blocks             []BlockRecord  `json:"blocks"`
	Transactions       []*Transaction `json:"transactions"`
}

// ChainFileFromStateFile derives a chain file path from the Core state file path.
func ChainFileFromStateFile(statePath string) string {
	dir := filepath.Dir(statePath)
	base := filepath.Base(statePath)
	if strings.Contains(base, "state") {
		base = strings.Replace(base, "state", "chain", 1)
	} else {
		ext := filepath.Ext(base)
		name := strings.TrimSuffix(base, ext)
		base = name + "-chain" + ext
	}
	return filepath.Join(dir, base)
}

// LoadChainFile restores in-memory chain metadata from disk. Does not replay txs into Core state.
func (bc *Blockchain) LoadChainFile(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read chain file: %w", err)
	}
	var file ChainFileData
	if err := json.Unmarshal(raw, &file); err != nil {
		return fmt.Errorf("parse chain file: %w", err)
	}
	if file.Version != chainFileVersion {
		return fmt.Errorf("unsupported chain file version %d", file.Version)
	}

	bc.mu.Lock()
	defer bc.mu.Unlock()

	bc.blockCounter = file.BlockCounter
	bc.totalFeesCollected = file.TotalFeesCollected
	bc.blockHistory = append([]BlockRecord(nil), file.Blocks...)
	bc.transactions = make(map[string]*Transaction, len(file.Transactions))
	bc.addressTxs = make(map[string][]*Transaction)
	bc.confirmedHashes = make(map[string]bool, len(file.Transactions))
	for _, tx := range file.Transactions {
		if tx == nil || tx.Hash == "" {
			continue
		}
		bc.transactions[tx.Hash] = tx
		if tx.BlockNumber >= 0 {
			bc.confirmedHashes[tx.Hash] = true
		}
		bc.lastTx = tx
		bc.addressTxs[tx.From] = append(bc.addressTxs[tx.From], tx)
		bc.addressTxs[tx.To] = append(bc.addressTxs[tx.To], tx)
	}
	bc.attachConfirmedTxsToHistoryLocked()
	bc.noteDurableHeadLocked()
	bc.chainFile = path
	return nil
}

// SetChainFile sets the path used for automatic persistence after confirmed blocks.
func (bc *Blockchain) SetChainFile(path string) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	bc.chainFile = path
}

// ChainFilePath returns the configured chain file path (may be empty).
func (bc *Blockchain) ChainFilePath() string {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.chainFile
}

// PersistChainSnapshot writes indexed transactions and block history to disk.
// When RocksDB is enabled this is still written as a fast explorer restart cache;
// RocksDB remains the canonical chain source of truth.
func (bc *Blockchain) PersistChainSnapshot() error {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	return bc.persistChain()
}

// persistChain writes the chain snapshot when a chain file is configured.
// Caller must hold bc.mu (write lock).
func (bc *Blockchain) persistChain() error {
	path := bc.chainFile
	if path == "" {
		return nil
	}
	file := ChainFileData{
		Version:            chainFileVersion,
		BlockCounter:       bc.blockCounter,
		TotalFeesCollected: bc.totalFeesCollected,
		Blocks:             append([]BlockRecord(nil), bc.blockHistory...),
		Transactions:       make([]*Transaction, 0, len(bc.transactions)),
	}
	for _, tx := range bc.transactions {
		file.Transactions = append(file.Transactions, tx)
	}
	nextHead := historyHead(file.Blocks)
	if bc.durableHead >= 0 && nextHead < bc.durableHead {
		return fmt.Errorf("refuse to shrink chain head %d to %d", bc.durableHead, nextHead)
	}
	raw, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal chain file: %w", err)
	}
	if err := atomicWriteFile(path, raw); err != nil {
		return err
	}
	if nextHead > bc.durableHead {
		bc.durableHead = nextHead
	}
	return nil
}

func atomicWriteFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create chain dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write chain temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename chain file: %w", err)
	}
	return nil
}

func historyHead(blocks []BlockRecord) int64 {
	max := int64(-1)
	for _, block := range blocks {
		if block.BlockNumber > max {
			max = block.BlockNumber
		}
	}
	return max
}

func (bc *Blockchain) noteDurableHeadLocked() {
	if h := historyHead(bc.blockHistory); h > bc.durableHead {
		bc.durableHead = h
	}
}

// MemoryHeadBlockNumber is the highest block in the explorer cache, ignoring Rocks tip-freeze.
func (bc *Blockchain) MemoryHeadBlockNumber() int64 {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return historyHead(bc.blockHistory)
}

// ChainCacheLeadsRocks reports that the explorer cache head is newer than Rocks.
func (bc *Blockchain) ChainCacheLeadsRocks() (mem int64, rocks int64, leads bool) {
	mem = bc.MemoryHeadBlockNumber()
	rocksHead, ok := bc.headBlockNumberFromRocks()
	if !ok {
		return mem, -1, false
	}
	return mem, rocksHead, mem > rocksHead
}

// attachConfirmedTxsToHistoryLocked rebuilds block records for confirmed txs whose
// block was dropped when an older Rocks snapshot replaced the explorer cache.
// Caller must hold bc.mu.
func (bc *Blockchain) attachConfirmedTxsToHistoryLocked() {
	index := make(map[int64]int, len(bc.blockHistory))
	for i := range bc.blockHistory {
		index[bc.blockHistory[i].BlockNumber] = i
	}
	type built struct {
		hashes []string
		ts     int64
		fees   int64
	}
	missing := map[int64]*built{}
	for _, tx := range bc.transactions {
		if tx == nil || tx.Hash == "" || tx.BlockNumber <= 0 {
			continue
		}
		if i, ok := index[tx.BlockNumber]; ok {
			found := false
			for _, hash := range bc.blockHistory[i].TxHashes {
				if hash == tx.Hash {
					found = true
					break
				}
			}
			if !found {
				bc.blockHistory[i].TxHashes = append(bc.blockHistory[i].TxHashes, tx.Hash)
				bc.blockHistory[i].TxCount = len(bc.blockHistory[i].TxHashes)
			}
			continue
		}
		row := missing[tx.BlockNumber]
		if row == nil {
			row = &built{}
			missing[tx.BlockNumber] = row
		}
		row.hashes = append(row.hashes, tx.Hash)
		if tx.Timestamp > row.ts {
			row.ts = tx.Timestamp
		}
		row.fees += int64(tx.FeeUplp)
	}
	if len(missing) == 0 {
		return
	}
	nums := make([]int64, 0, len(missing))
	for n := range missing {
		nums = append(nums, n)
	}
	sort.Slice(nums, func(i, j int) bool { return nums[i] < nums[j] })
	for _, n := range nums {
		row := missing[n]
		bc.blockHistory = append(bc.blockHistory, BlockRecord{
			BlockNumber: n,
			Timestamp:   row.ts,
			TxHashes:    row.hashes,
			TxCount:     len(row.hashes),
			TotalFees:   row.fees,
		})
	}
	sort.Slice(bc.blockHistory, func(i, j int) bool {
		return bc.blockHistory[i].BlockNumber < bc.blockHistory[j].BlockNumber
	})
	if h := historyHead(bc.blockHistory); h >= 0 && bc.blockCounter <= h {
		bc.blockCounter = h + 1
	}
	bc.noteDurableHeadLocked()
}

func isIndexedDeal(tx *Transaction) bool {
	if tx == nil {
		return false
	}
	return tx.Purpose == "deal" || tx.Type == "escrow_lock"
}

// indexTxsMissingFromHistoryLocked returns deal locks (and any confirmed tx that
// still has no block row) so the explorer list matches the address index.
// Caller must hold bc.mu.
func (bc *Blockchain) indexTxsMissingFromHistoryLocked(blocks []BlockRecord) []ConfirmedTxRef {
	inBlock := make(map[string]bool)
	head := historyHead(blocks)
	for _, block := range blocks {
		for _, hash := range block.TxHashes {
			if hash != "" {
				inBlock[hash] = true
			}
		}
	}
	skip := make(map[string]bool)
	for _, tx := range bc.mempool {
		if tx != nil && tx.Hash != "" {
			skip[tx.Hash] = true
		}
	}
	for _, tx := range bc.pendingBlock {
		if tx != nil && tx.Hash != "" {
			skip[tx.Hash] = true
		}
	}
	out := make([]ConfirmedTxRef, 0)
	for _, tx := range bc.transactions {
		if tx == nil || tx.Hash == "" || skip[tx.Hash] || inBlock[tx.Hash] {
			continue
		}
		if tx.BlockNumber <= 0 && !isIndexedDeal(tx) {
			continue
		}
		blockNumber := tx.BlockNumber
		if blockNumber <= 0 {
			blockNumber = head
		}
		if blockNumber < 0 {
			blockNumber = 0
		}
		out = append(out, ConfirmedTxRef{
			Hash:        tx.Hash,
			BlockNumber: blockNumber,
			Timestamp:   tx.Timestamp,
		})
	}
	return out
}
