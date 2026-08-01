package core

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// KernelExecEnabled is always on unless explicitly disabled with
// PLATARIUM_KERNEL_EXEC=0|false|no|off (emergency kill-switch only).
// When on, L2 confirm applies Core-compatible txs via kernel execute+commit.
func KernelExecEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("PLATARIUM_KERNEL_EXEC")))
	return v != "0" && v != "false" && v != "no" && v != "off"
}

// KernelExecuteResult is the parsed kernel_execute_batch response.
type KernelExecuteResult struct {
	OK    bool            `json:"ok"`
	Diff  json.RawMessage `json:"diff"`
	Waves json.RawMessage `json:"waves"`
}

// KernelCommitResult is the parsed kernel_commit_diff response.
type KernelCommitResult struct {
	OK            bool   `json:"ok"`
	PostStateRoot string `json:"post_state_root"`
	Height        uint64 `json:"height"`
	Error         string `json:"error,omitempty"`
}

// KernelExecuteBatchParams drives read-only execute → StateDiff.
type KernelExecuteBatchParams struct {
	StateFile    string
	BatchID      string
	Height       uint64
	Transactions []json.RawMessage // Core TX JSON objects
	Parallel     bool
}

// KernelExecuteBatch runs OrderedBatch execution without durable writes.
func (rc *RustCore) KernelExecuteBatch(p KernelExecuteBatchParams) (*KernelExecuteResult, error) {
	if rc.rpcClient == nil {
		return nil, fmt.Errorf("kernel_execute_batch requires PLATARIUM_CORE_MODE=rpc")
	}
	batchID := p.BatchID
	if batchID == "" {
		batchID = "batch"
	}
	txs := make([]interface{}, 0, len(p.Transactions))
	for _, raw := range p.Transactions {
		var obj interface{}
		if err := json.Unmarshal(raw, &obj); err != nil {
			return nil, fmt.Errorf("kernel tx json: %w", err)
		}
		txs = append(txs, obj)
	}
	params := map[string]interface{}{
		"state_file": p.StateFile,
		"parallel":   p.Parallel,
		"batch": map[string]interface{}{
			"batch_id":     batchID,
			"height":       p.Height,
			"transactions": txs,
		},
	}
	out, err := rc.rpcClient.Call("kernel_execute_batch", params)
	if err != nil {
		return nil, err
	}
	var res KernelExecuteResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return nil, fmt.Errorf("parse kernel_execute_batch: %w", err)
	}
	return &res, nil
}

// KernelCommitDiff applies a StateDiff via StorageEngine (state file).
func (rc *RustCore) KernelCommitDiff(stateFile string, diff json.RawMessage) (*KernelCommitResult, error) {
	if rc.rpcClient == nil {
		return nil, fmt.Errorf("kernel_commit_diff requires PLATARIUM_CORE_MODE=rpc")
	}
	var diffObj interface{}
	if err := json.Unmarshal(diff, &diffObj); err != nil {
		return nil, fmt.Errorf("kernel diff json: %w", err)
	}
	params := map[string]interface{}{
		"state_file": stateFile,
		"diff":       diffObj,
	}
	out, err := rc.rpcClient.Call("kernel_commit_diff", params)
	if err != nil {
		return nil, err
	}
	var res KernelCommitResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return nil, fmt.Errorf("parse kernel_commit_diff: %w", err)
	}
	return &res, nil
}

// KernelApplyBatch execute+commit for a batch of Core TX JSON strings (RPC only).
func (rc *RustCore) KernelApplyBatch(stateFile, batchID string, height uint64, coreTxJSONs []string, parallel bool) (*KernelCommitResult, error) {
	raws := make([]json.RawMessage, 0, len(coreTxJSONs))
	for _, s := range coreTxJSONs {
		raws = append(raws, json.RawMessage(s))
	}
	execRes, err := rc.KernelExecuteBatch(KernelExecuteBatchParams{
		StateFile:    stateFile,
		BatchID:      batchID,
		Height:       height,
		Transactions: raws,
		Parallel:     parallel,
	})
	if err != nil {
		return nil, err
	}
	if !execRes.OK {
		return nil, fmt.Errorf("kernel_execute_batch not ok")
	}
	return rc.KernelCommitDiff(stateFile, execRes.Diff)
}
