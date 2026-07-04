package blockchain

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	for _, tx := range file.Transactions {
		if tx == nil || tx.Hash == "" {
			continue
		}
		bc.transactions[tx.Hash] = tx
		bc.lastTx = tx
		bc.addressTxs[tx.From] = append(bc.addressTxs[tx.From], tx)
		bc.addressTxs[tx.To] = append(bc.addressTxs[tx.To], tx)
	}
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
	raw, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal chain file: %w", err)
	}
	return atomicWriteFile(path, raw)
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
