package core

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// MempoolAdmitResult matches Core mempool_admit JSON.
type MempoolAdmitResult struct {
	Accepted      bool   `json:"accepted"`
	Error         string `json:"error,omitempty"`
	MinFeeUplp    uint64 `json:"min_fee_uplp"`
	ExpectedNonce uint64 `json:"expected_nonce"`
}

// BlockProposalStatusResult matches Core block_proposal_status JSON.
type BlockProposalStatusResult struct {
	ShouldPropose        bool   `json:"should_propose"`
	MempoolCount         int    `json:"mempool_count"`
	MempoolGasUplp       uint64 `json:"mempool_gas_uplp"`
	BlockGasCapUplp      uint64 `json:"block_gas_cap_uplp"`
	MinFeeUplp           uint64 `json:"min_fee_uplp"`
	OldestMempoolWaitSec int64  `json:"oldest_mempool_wait_sec"`
}

// SelectBlockTxsResult matches Core select_block_txs JSON.
type SelectBlockTxsResult struct {
	Hashes  []string `json:"hashes"`
	GasUsed uint64   `json:"gas_used"`
	GasCap  uint64   `json:"gas_cap"`
	TxCount int      `json:"tx_count"`
}

// MinFeeFromLoad returns load-based minimum fee_uplp from Core.
func (rc *RustCore) MinFeeFromLoad(pendingCount int) (uint64, error) {
	out, err := rc.Execute([]string{
		"min-fee-from-load",
		"--pending-count", strconv.Itoa(pendingCount),
	})
	if err != nil {
		return 0, err
	}
	var parsed struct {
		MinFeeUplp uint64 `json:"min_fee_uplp"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		return 0, fmt.Errorf("parse min-fee-from-load: %w", err)
	}
	return parsed.MinFeeUplp, nil
}

// MempoolAdmit asks Core whether a tx may enter the mempool.
func (rc *RustCore) MempoolAdmit(stateFile, txJSON, mempoolTxsJSON string) (*MempoolAdmitResult, error) {
	out, err := rc.Execute([]string{
		"mempool-admit",
		"--state-file", stateFile,
		"--tx", txJSON,
		"--mempool-txs", mempoolTxsJSON,
	})
	if err != nil {
		return nil, err
	}
	var res MempoolAdmitResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return nil, fmt.Errorf("parse mempool-admit: %w", err)
	}
	return &res, nil
}

// BlockProposalStatus asks Core whether a block should be proposed.
func (rc *RustCore) BlockProposalStatus(mempoolTxsJSON string, nowUnix int64) (*BlockProposalStatusResult, error) {
	out, err := rc.Execute([]string{
		"block-proposal-status",
		"--mempool-txs", mempoolTxsJSON,
		"--now-unix", strconv.FormatInt(nowUnix, 10),
	})
	if err != nil {
		return nil, err
	}
	var res BlockProposalStatusResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return nil, fmt.Errorf("parse block-proposal-status: %w", err)
	}
	return &res, nil
}

// SelectBlockTxs asks Core which mempool hashes to include in the next block.
func (rc *RustCore) SelectBlockTxs(stateFile, mempoolTxsJSON string) (*SelectBlockTxsResult, error) {
	out, err := rc.Execute([]string{
		"select-block-txs",
		"--state-file", stateFile,
		"--mempool-txs", mempoolTxsJSON,
	})
	if err != nil {
		return nil, err
	}
	var res SelectBlockTxsResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return nil, fmt.Errorf("parse select-block-txs: %w", err)
	}
	return &res, nil
}
