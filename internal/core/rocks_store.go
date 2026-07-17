package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// RocksAccount is an account record from rocks_get_account.
type RocksAccount struct {
	Address     string `json:"address"`
	Balance     string `json:"balance"`
	UplpBalance string `json:"uplp_balance"`
	Nonce       uint64 `json:"nonce"`
}

// RocksBlockStored matches Core BlockRecordStored (rocks_get_block).
type RocksBlockStored struct {
	Height       uint64   `json:"height"`
	PreviousHash string   `json:"previous_hash"`
	Timestamp    int64    `json:"timestamp"`
	TxHashes     []string `json:"tx_hashes"`
	MerkleRoot   string   `json:"merkle_root"`
	StateRoot    string   `json:"state_root"`
	BlockHash    string   `json:"block_hash"`
	ProducerID   string   `json:"producer_id"`
}

// BlockCommitPayload matches Core BlockCommit for rocks_commit_block.
type BlockCommitPayload struct {
	Block      RocksBlockStored `json:"block"`
	TxJSONs    []string         `json:"tx_jsons"`
	Accounts   []RocksAccount   `json:"accounts"`
	Receipts   []BlockReceipt   `json:"receipts"`
	StateRoot  string           `json:"state_root"`
}

// BlockReceipt matches Core ReceiptRecord.
type BlockReceipt struct {
	TxHash      string `json:"tx_hash"`
	Status      string `json:"status"`
	FeeUplp     uint64 `json:"fee_uplp"`
	BlockHeight uint64 `json:"block_height"`
}

// RocksStoreClient queries and commits canonical chain data via platarium-cli / Core RPC.
type RocksStoreClient struct {
	rustCore *RustCore
	dbPath   string
}

// ResolveRocksDBPath returns PLATARIUM_ROCKSDB_PATH or {state_dir}/rocksdb.
func ResolveRocksDBPath(stateFile string) string {
	if p := os.Getenv("PLATARIUM_ROCKSDB_PATH"); p != "" {
		return p
	}
	if stateFile != "" {
		return filepath.Join(filepath.Dir(stateFile), "rocksdb")
	}
	return filepath.Join("data", "rocksdb")
}

// NewRocksStoreClient creates a RocksDB client. dbPath may be empty to use ResolveRocksDBPath later.
func NewRocksStoreClient(rustCore *RustCore, dbPath string) (*RocksStoreClient, error) {
	if rustCore == nil {
		return nil, fmt.Errorf("rust core required for rocks store")
	}
	if dbPath == "" {
		return nil, fmt.Errorf("rocksdb path required")
	}
	return &RocksStoreClient{rustCore: rustCore, dbPath: dbPath}, nil
}

func (c *RocksStoreClient) Enabled() bool {
	return c != nil && c.dbPath != "" && c.rustCore != nil
}

func (c *RocksStoreClient) DBPath() string {
	if c == nil {
		return ""
	}
	return c.dbPath
}

// GatewayBlockToRocksHeight maps gateway block numbers (0-based) to Core heights (1-based).
func GatewayBlockToRocksHeight(blockNumber int64) uint64 {
	return uint64(blockNumber + 1)
}

// RocksHeightToGatewayBlock maps Core head/height to gateway block number.
func RocksHeightToGatewayBlock(height uint64) int64 {
	if height == 0 {
		return -1
	}
	return int64(height) - 1
}

func (c *RocksStoreClient) RocksGetHead() (uint64, error) {
	out, err := c.rustCore.Execute([]string{"rocks-get-head", "--db-path", c.dbPath})
	if err != nil {
		return 0, err
	}
	var res struct {
		Head uint64 `json:"head"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return 0, fmt.Errorf("parse rocks-get-head: %w", err)
	}
	return res.Head, nil
}

func (c *RocksStoreClient) RocksGetTx(txHash string) (found bool, txJSON string, err error) {
	out, err := c.rustCore.Execute([]string{
		"rocks-get-tx", "--db-path", c.dbPath, "--tx-hash", txHash,
	})
	if err != nil {
		return false, "", err
	}
	var res struct {
		Found bool            `json:"found"`
		Tx    json.RawMessage `json:"tx"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return false, "", fmt.Errorf("parse rocks-get-tx: %w", err)
	}
	if !res.Found {
		return false, "", nil
	}
	return true, string(res.Tx), nil
}

func (c *RocksStoreClient) RocksGetBlock(height uint64) (found bool, block *RocksBlockStored, err error) {
	out, err := c.rustCore.Execute([]string{
		"rocks-get-block", "--db-path", c.dbPath, "--height", fmt.Sprintf("%d", height),
	})
	if err != nil {
		return false, nil, err
	}
	var res struct {
		Found bool             `json:"found"`
		Block RocksBlockStored `json:"block"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return false, nil, fmt.Errorf("parse rocks-get-block: %w", err)
	}
	if !res.Found {
		return false, nil, nil
	}
	b := res.Block
	return true, &b, nil
}

func (c *RocksStoreClient) RocksGetAccount(address string) (found bool, account *RocksAccount, err error) {
	out, err := c.rustCore.Execute([]string{
		"rocks-get-account", "--db-path", c.dbPath, "--address", address,
	})
	if err != nil {
		return false, nil, err
	}
	var res struct {
		Found   bool         `json:"found"`
		Account RocksAccount `json:"account"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return false, nil, fmt.Errorf("parse rocks-get-account: %w", err)
	}
	if !res.Found {
		return false, nil, nil
	}
	a := res.Account
	return true, &a, nil
}

func (c *RocksStoreClient) RocksListAddressTxs(address string) ([]string, error) {
	out, err := c.rustCore.Execute([]string{
		"rocks-list-address-txs", "--db-path", c.dbPath, "--address", address,
	})
	if err != nil {
		return nil, err
	}
	var res struct {
		Address  string   `json:"address"`
		TxHashes []string `json:"tx_hashes"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return nil, fmt.Errorf("parse rocks-list-address-txs: %w", err)
	}
	return res.TxHashes, nil
}

func (c *RocksStoreClient) RocksCommitBlock(commit *BlockCommitPayload) (uint64, error) {
	raw, err := json.Marshal(commit)
	if err != nil {
		return 0, fmt.Errorf("marshal BlockCommit: %w", err)
	}
	out, err := c.rustCore.Execute([]string{
		"rocks-commit-block", "--db-path", c.dbPath, "--commit", string(raw),
	})
	if err != nil {
		return 0, err
	}
	var res struct {
		OK     bool   `json:"ok"`
		Height uint64 `json:"height"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return 0, fmt.Errorf("parse rocks-commit-block: %w", err)
	}
	if !res.OK {
		return 0, fmt.Errorf("rocks-commit-block failed")
	}
	return res.Height, nil
}

func (c *RocksStoreClient) MigrateJSONToRocks(chainFile, accountsFile string) error {
	args := []string{
		"migrate-json-to-rocks",
		"--db-path", c.dbPath,
		"--chain-file", chainFile,
	}
	if accountsFile != "" {
		args = append(args, "--accounts-file", accountsFile)
	}
	_, err := c.rustCore.Execute(args)
	return err
}
