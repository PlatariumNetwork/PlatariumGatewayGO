package blockchain

import (
	"encoding/json"
	"fmt"
	"strconv"

	"platarium-gateway-go/internal/core"
)

func (bc *Blockchain) rocksClient() *core.RocksStoreClient {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.rocks
}

func (bc *Blockchain) RocksEnabled() bool {
	return bc.rocksClient() != nil && bc.rocksClient().Enabled()
}

// SetRocksStore attaches the Core RocksDB client for canonical reads/commits.
func (bc *Blockchain) SetRocksStore(rocks *core.RocksStoreClient) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	bc.rocks = rocks
}

// SyncFromRocksHead sets blockCounter and hydrates in-memory blockHistory from Core RocksDB.
func (bc *Blockchain) SyncFromRocksHead() error {
	rocks := bc.rocksClient()
	if rocks == nil || !rocks.Enabled() {
		return nil
	}
	head, err := rocks.RocksGetHead()
	if err != nil {
		return err
	}
	history, err := bc.listBlockHistoryFromRocks()
	if err != nil {
		return err
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	// Next gateway block number equals Rocks head (0-based next after last imported height).
	// Rocks height H ↔ gateway blockNumber H-1; after head H, next BlockNumber is H.
	bc.blockCounter = int64(head)
	if len(history) > 0 {
		bc.blockHistory = history
	}
	return nil
}

func txFromCoreJSON(raw string) (*Transaction, error) {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, err
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	var tx Transaction
	if err := json.Unmarshal(b, &tx); err != nil {
		return nil, err
	}
	if tx.Hash == "" {
		if h, ok := m["hash"].(string); ok {
			tx.Hash = h
		}
	}
	if tx.Value == "" && tx.AmountUplp > 0 {
		tx.Value = strconv.FormatUint(tx.AmountUplp, 10)
	}
	if tx.Fee == "" && tx.FeeUplp > 0 {
		tx.Fee = strconv.FormatUint(tx.FeeUplp, 10)
	}
	if tx.Asset == "" {
		tx.Asset = "PLP"
	}
	return &tx, nil
}

func blockRecordFromRocks(b *core.RocksBlockStored) BlockRecord {
	return BlockRecord{
		BlockNumber:    core.RocksHeightToGatewayBlock(b.Height),
		Timestamp:      b.Timestamp,
		TxHashes:       append([]string(nil), b.TxHashes...),
		TxCount:        len(b.TxHashes),
		BlockHash:      b.BlockHash,
		MerkleRoot:     b.MerkleRoot,
		StateRoot:      b.StateRoot,
		PreviousHash:   b.PreviousHash,
		ProducerNodeID: b.ProducerID,
	}
}

func (bc *Blockchain) getTransactionFromRocks(hash string) *Transaction {
	rocks := bc.rocksClient()
	if rocks == nil || !rocks.Enabled() {
		return nil
	}
	found, raw, err := rocks.RocksGetTx(hash)
	if err != nil || !found {
		return nil
	}
	tx, err := txFromCoreJSON(raw)
	if err != nil {
		return nil
	}
	return tx
}

func (bc *Blockchain) getAccountFromRocks(address string) (*core.AccountQuery, error) {
	rocks := bc.rocksClient()
	if rocks == nil || !rocks.Enabled() {
		return nil, fmt.Errorf("rocks store unavailable")
	}
	found, acct, err := rocks.RocksGetAccount(address)
	if err != nil {
		return nil, err
	}
	if !found {
		return &core.AccountQuery{
			Address: address,
			Asset:   "PLP",
			Balance: "0",
		}, nil
	}
	return &core.AccountQuery{
		Address:     acct.Address,
		Asset:       "PLP",
		Balance:     acct.Balance,
		UplpBalance: acct.UplpBalance,
		Nonce:       acct.Nonce,
	}, nil
}

func (bc *Blockchain) getBlockFromRocks(blockNumber int64) *BlockRecord {
	rocks := bc.rocksClient()
	if rocks == nil || !rocks.Enabled() {
		return nil
	}
	height := core.GatewayBlockToRocksHeight(blockNumber)
	found, stored, err := rocks.RocksGetBlock(height)
	if err != nil || !found || stored == nil {
		return nil
	}
	b := blockRecordFromRocks(stored)
	return &b
}

func (bc *Blockchain) listConfirmedTxsFromRocks() ([]*Transaction, error) {
	rocks := bc.rocksClient()
	if rocks == nil || !rocks.Enabled() {
		return nil, fmt.Errorf("rocks store unavailable")
	}
	head, err := rocks.RocksGetHead()
	if err != nil {
		return nil, err
	}
	out := make([]*Transaction, 0)
	seen := make(map[string]bool)
	for h := uint64(1); h <= head; h++ {
		found, stored, err := rocks.RocksGetBlock(h)
		if err != nil || !found || stored == nil {
			continue
		}
		for _, hash := range stored.TxHashes {
			if seen[hash] {
				continue
			}
			tx := bc.getTransactionFromRocks(hash)
			if tx == nil {
				continue
			}
			tx.BlockNumber = core.RocksHeightToGatewayBlock(h)
			seen[hash] = true
			out = append(out, tx)
		}
	}
	return out, nil
}

func (bc *Blockchain) listBlockHistoryFromRocks() ([]BlockRecord, error) {
	rocks := bc.rocksClient()
	if rocks == nil || !rocks.Enabled() {
		return nil, fmt.Errorf("rocks store unavailable")
	}
	head, err := rocks.RocksGetHead()
	if err != nil {
		return nil, err
	}
	out := make([]BlockRecord, 0, head)
	for h := uint64(1); h <= head; h++ {
		found, stored, err := rocks.RocksGetBlock(h)
		if err != nil || !found || stored == nil {
			continue
		}
		out = append(out, blockRecordFromRocks(stored))
	}
	return out, nil
}

func (bc *Blockchain) headBlockNumberFromRocks() (int64, bool) {
	rocks := bc.rocksClient()
	if rocks == nil || !rocks.Enabled() {
		return -1, false
	}
	head, err := rocks.RocksGetHead()
	if err != nil {
		return -1, false
	}
	return core.RocksHeightToGatewayBlock(head), true
}
