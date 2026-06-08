package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// AccountQuery is the parsed state-query response from platarium-cli.
type AccountQuery struct {
	Address     string `json:"address"`
	Asset       string `json:"asset"`
	Balance     string `json:"balance"`
	UplpBalance string `json:"uplp_balance"`
	Nonce       uint64 `json:"nonce"`
}

// ApplyTxResult is the parsed state-apply-tx response.
type ApplyTxResult struct {
	OK        bool   `json:"ok"`
	Hash      string `json:"hash"`
	StateRoot string `json:"state_root"`
	Error     string `json:"error"`
}

// LedgerService wraps Core state file operations via platarium-cli.
type LedgerService struct {
	rustCore  *RustCore
	stateFile string
	testnet   bool
}

// NewLedgerService creates a ledger service and initializes the state file if missing.
func NewLedgerService(rustCore *RustCore, stateFile string, testnet bool) (*LedgerService, error) {
	if rustCore == nil {
		return nil, fmt.Errorf("rust core required for ledger service")
	}
	if stateFile == "" {
		stateFile = "data/core-state.json"
	}
	dir := filepath.Dir(stateFile)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create state dir: %w", err)
		}
	}
	ls := &LedgerService{
		rustCore:  rustCore,
		stateFile: stateFile,
		testnet:   testnet,
	}
	if _, err := os.Stat(stateFile); os.IsNotExist(err) {
		if _, err := ls.rustCore.StateInit(stateFile); err != nil {
			return nil, err
		}
	}
	return ls, nil
}

func (ls *LedgerService) StateFilePath() string {
	return ls.stateFile
}

// Query returns authoritative balance/nonce from Core state file.
func (ls *LedgerService) Query(address string) (*AccountQuery, error) {
	out, err := ls.rustCore.StateQuery(ls.stateFile, address, "PLP")
	if err != nil {
		return nil, err
	}
	var q AccountQuery
	if err := json.Unmarshal([]byte(out), &q); err != nil {
		return nil, fmt.Errorf("parse state-query: %w", err)
	}
	return &q, nil
}

// ValidateTx dry-runs transaction against current state (mempool admission).
func (ls *LedgerService) ValidateTx(txJSON string) (bool, error) {
	return ls.rustCore.StateValidateTx(ls.stateFile, txJSON)
}

// ApplyTx commits transaction to Core state file.
func (ls *LedgerService) ApplyTx(txJSON string) (*ApplyTxResult, error) {
	out, err := ls.rustCore.StateApplyTx(ls.stateFile, txJSON)
	if err != nil {
		return nil, err
	}
	var res ApplyTxResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return nil, fmt.Errorf("parse state-apply-tx: %w", err)
	}
	if !res.OK {
		if res.Error == "" {
			res.Error = "state-apply-tx failed"
		}
		return &res, fmt.Errorf("%s", res.Error)
	}
	return &res, nil
}

// Credit funds an address in testnet mode (state-credit).
func (ls *LedgerService) Credit(address string, plp, uplp uint64) error {
	if !ls.testnet {
		return fmt.Errorf("state credit only allowed in testnet mode")
	}
	_, err := ls.rustCore.StateCredit(ls.stateFile, address, plp, uplp, true)
	return err
}

// StateRoot returns deterministic state root hex.
func (ls *LedgerService) StateRoot() (string, error) {
	out, err := ls.rustCore.StateRoot(ls.stateFile)
	if err != nil {
		return "", err
	}
	var res struct {
		StateRoot string `json:"state_root"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return "", fmt.Errorf("parse state-root: %w", err)
	}
	return res.StateRoot, nil
}
