package core

import (
	"encoding/json"
	"fmt"
)

// BlockCycleResult is the parsed block_cycle RPC response.
type BlockCycleResult struct {
	OK           bool            `json:"ok"`
	Error        string          `json:"error,omitempty"`
	Selected     json.RawMessage `json:"selected"`
	L1           json.RawMessage `json:"l1"`
	ValidHashes  []string        `json:"valid_hashes"`
	Block        *BlockHeader    `json:"block"`
	L1Votes      *VoteResult     `json:"l1_votes"`
	L2Votes      *VoteResult     `json:"l2_votes"`
	Applied      json.RawMessage `json:"applied"`
	StateRoot    string          `json:"state_root"`
	RocksCommit  json.RawMessage `json:"rocks_commit"`
}

// BlockCycleParams drives the Core-owned admit→pack→L1/L2→assemble pipeline.
type BlockCycleParams struct {
	StateFile    string
	MempoolTxs   string
	BlockNumber  uint64
	PreviousHash string
	Timestamp    int64
	ProducerID   string
	AutoConfirm  bool
	ApplyTxs     bool
	DBPath       string
	CommitJSON   string
}

// BlockCycle runs select → L1 verify → assemble → optional votes/apply/commit in one RPC.
func (rc *RustCore) BlockCycle(p BlockCycleParams) (*BlockCycleResult, error) {
	params := map[string]interface{}{
		"state_file":    p.StateFile,
		"mempool_txs":   p.MempoolTxs,
		"block_number":  p.BlockNumber,
		"previous_hash": p.PreviousHash,
		"timestamp":     p.Timestamp,
		"producer_id":   p.ProducerID,
		"auto_confirm":  p.AutoConfirm,
		"apply_txs":     p.ApplyTxs,
	}
	if p.DBPath != "" {
		params["db_path"] = p.DBPath
	}
	if p.CommitJSON != "" {
		params["commit"] = p.CommitJSON
	}

	var out string
	var err error
	if rc.rpcClient != nil {
		out, err = rc.rpcClient.Call("block_cycle", params)
	} else {
		// CLI fallback: not a single argv command — require RPC mode.
		return nil, fmt.Errorf("block_cycle requires PLATARIUM_CORE_MODE=rpc")
	}
	if err != nil {
		return nil, err
	}
	var res BlockCycleResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return nil, fmt.Errorf("parse block_cycle: %w", err)
	}
	return &res, nil
}
