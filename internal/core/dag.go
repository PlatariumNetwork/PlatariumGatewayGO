package core

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// DagOrderingEnabled is always on unless explicitly disabled with
// PLATARIUM_DAG_ORDERING=0|false|no (emergency kill-switch only).
func DagOrderingEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("PLATARIUM_DAG_ORDERING")))
	return v != "0" && v != "false" && v != "no" && v != "off"
}

// DagP2PEnabled is always on unless explicitly disabled with
// PLATARIUM_DAG_P2P=0|false|no (emergency kill-switch only).
func DagP2PEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("PLATARIUM_DAG_P2P")))
	return v != "0" && v != "false" && v != "no" && v != "off"
}

// DagVertexWire is the JSON shape for dag:vertex / dag_ingest.
type DagVertexWire struct {
	ID        string   `json:"id"`
	Round     uint64   `json:"round"`
	Author    string   `json:"author"`
	Parents   []string `json:"parents"`
	TxDigests []string `json:"tx_digests"`
}

// DagInsertParams is the body for dag_insert.
type DagInsertParams struct {
	Round     uint64
	Author    string
	Parents   []string
	TxDigests []string
}

// DagInsertResult is the parsed dag_insert response.
type DagInsertResult struct {
	OK bool   `json:"ok"`
	ID string `json:"id"`
}

// DagLinearizeResult is the parsed dag_linearize response.
type DagLinearizeResult struct {
	OK          bool     `json:"ok"`
	Digests     []string `json:"digests"`
	VertexOrder []string `json:"vertex_order"`
}

// DagTryCommitResult is the parsed dag_try_commit response.
type DagTryCommitResult struct {
	OK          bool     `json:"ok"`
	Committed   bool     `json:"committed"`
	Anchor      string   `json:"anchor,omitempty"`
	Digests     []string `json:"digests,omitempty"`
	VertexOrder []string `json:"vertex_order,omitempty"`
	Round       uint64   `json:"round,omitempty"`
}

// DagProposeResult is the parsed dag_propose response.
type DagProposeResult struct {
	OK             bool          `json:"ok"`
	Status         string        `json:"status"`
	Vertex         DagVertexWire `json:"vertex"`
	MissingParents []string      `json:"missing_parents"`
	Flushed        []string      `json:"flushed"`
	Error          string        `json:"error,omitempty"`
}

// DagIngestResult is the parsed dag_ingest response.
type DagIngestResult struct {
	OK             bool     `json:"ok"`
	Status         string   `json:"status"`
	ID             string   `json:"id"`
	MissingParents []string `json:"missing_parents"`
	Flushed        []string `json:"flushed"`
	Error          string   `json:"error,omitempty"`
}

// DagReset clears the Core process-global DAG store (test helper).
func (rc *RustCore) DagReset() error {
	if rc.rpcClient == nil {
		return fmt.Errorf("dag_reset requires PLATARIUM_CORE_MODE=rpc")
	}
	_, err := rc.rpcClient.Call("dag_reset", map[string]interface{}{})
	return err
}

// DagInsert inserts a vertex into the Core DAG store.
func (rc *RustCore) DagInsert(p DagInsertParams) (*DagInsertResult, error) {
	if rc.rpcClient == nil {
		return nil, fmt.Errorf("dag_insert requires PLATARIUM_CORE_MODE=rpc")
	}
	params := map[string]interface{}{
		"vertex": map[string]interface{}{
			"round":      p.Round,
			"author":     p.Author,
			"parents":    p.Parents,
			"tx_digests": p.TxDigests,
		},
	}
	out, err := rc.rpcClient.Call("dag_insert", params)
	if err != nil {
		return nil, err
	}
	var res DagInsertResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return nil, fmt.Errorf("parse dag_insert: %w", err)
	}
	return &res, nil
}

// DagLinearize returns causal digest order for an anchor.
func (rc *RustCore) DagLinearize(anchor string) (*DagLinearizeResult, error) {
	if rc.rpcClient == nil {
		return nil, fmt.Errorf("dag_linearize requires PLATARIUM_CORE_MODE=rpc")
	}
	out, err := rc.rpcClient.Call("dag_linearize", map[string]interface{}{"anchor": anchor})
	if err != nil {
		return nil, err
	}
	var res DagLinearizeResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return nil, fmt.Errorf("parse dag_linearize: %w", err)
	}
	return &res, nil
}

// DagTryCommit attempts Bullshark-lite commit for support round.
func (rc *RustCore) DagTryCommit(round uint64, committee []string, f int) (*DagTryCommitResult, error) {
	if rc.rpcClient == nil {
		return nil, fmt.Errorf("dag_try_commit requires PLATARIUM_CORE_MODE=rpc")
	}
	out, err := rc.rpcClient.Call("dag_try_commit", map[string]interface{}{
		"round":     round,
		"committee": committee,
		"f":         f,
	})
	if err != nil {
		return nil, err
	}
	var res DagTryCommitResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return nil, fmt.Errorf("parse dag_try_commit: %w", err)
	}
	return &res, nil
}

// DagOrderDigestsResult is the parsed dag_order_digests response.
type DagOrderDigestsResult struct {
	OK          bool     `json:"ok"`
	Digests     []string `json:"digests"`
	VertexOrder []string `json:"vertex_order"`
	Tip         string   `json:"tip"`
}

// DagOrderDigests runs the ephemeral pack-and-order pipeline (confirm wiring).
func (rc *RustCore) DagOrderDigests(producer string, digests []string) (*DagOrderDigestsResult, error) {
	if rc.rpcClient == nil {
		return nil, fmt.Errorf("dag_order_digests requires PLATARIUM_CORE_MODE=rpc")
	}
	if producer == "" {
		producer = "n0"
	}
	out, err := rc.rpcClient.Call("dag_order_digests", map[string]interface{}{
		"producer": producer,
		"digests":  digests,
	})
	if err != nil {
		return nil, err
	}
	var res DagOrderDigestsResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return nil, fmt.Errorf("parse dag_order_digests: %w", err)
	}
	return &res, nil
}

// DagEnsureGenesisResult is the parsed dag_ensure_genesis response.
type DagEnsureGenesisResult struct {
	OK     bool          `json:"ok"`
	Status string        `json:"status"`
	Vertex DagVertexWire `json:"vertex"`
	Error  string        `json:"error,omitempty"`
}

// DagTryCommitBatchesResult is the parsed dag_try_commit_batches response.
type DagTryCommitBatchesResult struct {
	OK          bool     `json:"ok"`
	Committed   bool     `json:"committed"`
	Anchor      string   `json:"anchor,omitempty"`
	Digests     []string `json:"digests,omitempty"`
	VertexOrder []string `json:"vertex_order,omitempty"`
	Round       uint64   `json:"round,omitempty"`
}

// SharedGenesisAuthor matches Core SHARED_GENESIS_AUTHOR.
const SharedGenesisAuthor = "platarium-genesis"

// DagEnsureGenesis inserts the network-shared genesis into the Core DAG store.
func (rc *RustCore) DagEnsureGenesis() (*DagEnsureGenesisResult, error) {
	if rc.rpcClient == nil {
		return nil, fmt.Errorf("dag_ensure_genesis requires PLATARIUM_CORE_MODE=rpc")
	}
	out, err := rc.rpcClient.Call("dag_ensure_genesis", map[string]interface{}{})
	if err != nil {
		return nil, err
	}
	var res DagEnsureGenesisResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return nil, fmt.Errorf("parse dag_ensure_genesis: %w", err)
	}
	return &res, nil
}

// DagTryCommitBatches attempts quorum commit of round-1 batches under shared genesis.
func (rc *RustCore) DagTryCommitBatches(batchRound uint64, committee []string, f *int) (*DagTryCommitBatchesResult, error) {
	if rc.rpcClient == nil {
		return nil, fmt.Errorf("dag_try_commit_batches requires PLATARIUM_CORE_MODE=rpc")
	}
	params := map[string]interface{}{
		"batch_round": batchRound,
		"committee":   committee,
	}
	if f != nil {
		params["f"] = *f
	}
	out, err := rc.rpcClient.Call("dag_try_commit_batches", params)
	if err != nil {
		return nil, err
	}
	var res DagTryCommitBatchesResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return nil, fmt.Errorf("parse dag_try_commit_batches: %w", err)
	}
	return &res, nil
}

// BuildDagCommittee returns sorted unique node ids: self + peers. f is inferred by Core when nil.
func BuildDagCommittee(selfID string, peerIDs []string) []string {
	set := map[string]struct{}{}
	if selfID != "" {
		set[selfID] = struct{}{}
	}
	for _, p := range peerIDs {
		if p != "" {
			set[p] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// DagLastCommit returns the Core-cached last successful batch commit (if any).
func (rc *RustCore) DagLastCommit() (*DagTryCommitBatchesResult, error) {
	if rc.rpcClient == nil {
		return nil, fmt.Errorf("dag_last_commit requires PLATARIUM_CORE_MODE=rpc")
	}
	out, err := rc.rpcClient.Call("dag_last_commit", map[string]interface{}{})
	if err != nil {
		return nil, err
	}
	var res DagTryCommitBatchesResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return nil, fmt.Errorf("parse dag_last_commit: %w", err)
	}
	return &res, nil
}

// DagPropose creates and stores a local vertex (Narwhal primary-lite).
func (rc *RustCore) DagPropose(round uint64, author string, parents, txDigests []string) (*DagProposeResult, error) {
	if rc.rpcClient == nil {
		return nil, fmt.Errorf("dag_propose requires PLATARIUM_CORE_MODE=rpc")
	}
	if author == "" {
		author = "n0"
	}
	if parents == nil {
		parents = []string{}
	}
	if txDigests == nil {
		txDigests = []string{}
	}
	out, err := rc.rpcClient.Call("dag_propose", map[string]interface{}{
		"round":      round,
		"author":     author,
		"parents":    parents,
		"tx_digests": txDigests,
	})
	if err != nil {
		return nil, err
	}
	var res DagProposeResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return nil, fmt.Errorf("parse dag_propose: %w", err)
	}
	return &res, nil
}

// DagIngest verifies and stores a peer wire vertex (pending if parents missing).
func (rc *RustCore) DagIngest(v DagVertexWire) (*DagIngestResult, error) {
	if rc.rpcClient == nil {
		return nil, fmt.Errorf("dag_ingest requires PLATARIUM_CORE_MODE=rpc")
	}
	out, err := rc.rpcClient.Call("dag_ingest", map[string]interface{}{
		"vertex": map[string]interface{}{
			"id":         v.ID,
			"round":      v.Round,
			"author":     v.Author,
			"parents":    v.Parents,
			"tx_digests": v.TxDigests,
		},
	})
	if err != nil {
		return nil, err
	}
	var res DagIngestResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return nil, fmt.Errorf("parse dag_ingest: %w", err)
	}
	return &res, nil
}

// VertexToMap converts a wire vertex to a broadcast payload map.
func VertexToMap(v DagVertexWire) map[string]interface{} {
	parents := v.Parents
	if parents == nil {
		parents = []string{}
	}
	digests := v.TxDigests
	if digests == nil {
		digests = []string{}
	}
	return map[string]interface{}{
		"id":         v.ID,
		"round":      v.Round,
		"author":     v.Author,
		"parents":    parents,
		"tx_digests": digests,
	}
}

// VertexFromMap parses a dag:vertex payload.
func VertexFromMap(m map[string]interface{}) (DagVertexWire, bool) {
	var v DagVertexWire
	if m == nil {
		return v, false
	}
	id, _ := m["id"].(string)
	author, _ := m["author"].(string)
	if id == "" || author == "" {
		return v, false
	}
	v.ID = id
	v.Author = author
	switch r := m["round"].(type) {
	case float64:
		v.Round = uint64(r)
	case uint64:
		v.Round = r
	case int:
		v.Round = uint64(r)
	case int64:
		v.Round = uint64(r)
	case json.Number:
		n, _ := r.Int64()
		v.Round = uint64(n)
	}
	v.Parents = stringSliceFromAny(m["parents"])
	v.TxDigests = stringSliceFromAny(m["tx_digests"])
	return v, true
}

func stringSliceFromAny(raw interface{}) []string {
	arr, ok := raw.([]interface{})
	if !ok {
		if ss, ok := raw.([]string); ok {
			return ss
		}
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// StringSliceFromAny exports stringSliceFromAny for handlers.
func StringSliceFromAny(raw interface{}) []string {
	return stringSliceFromAny(raw)
}

// DigestOverlapCount counts how many commitDigests appear in batchDigests.
func DigestOverlapCount(commitDigests, batchDigests []string) int {
	set := make(map[string]struct{}, len(batchDigests))
	for _, d := range batchDigests {
		if d != "" {
			set[d] = struct{}{}
		}
	}
	n := 0
	for _, d := range commitDigests {
		if _, ok := set[d]; ok {
			n++
		}
	}
	return n
}

// PermuteByDigests reorders items so item hashes follow orderedDigests.
// Items whose hash is missing from orderedDigests are appended in original order.
func PermuteByDigests[T any](orderedDigests []string, items []T, hashOf func(T) string) []T {
	if len(items) <= 1 || len(orderedDigests) == 0 {
		return items
	}
	index := make(map[string]int, len(items))
	for i, it := range items {
		h := hashOf(it)
		if h != "" {
			if _, exists := index[h]; !exists {
				index[h] = i
			}
		}
	}
	used := make([]bool, len(items))
	out := make([]T, 0, len(items))
	for _, d := range orderedDigests {
		if i, ok := index[d]; ok && !used[i] {
			out = append(out, items[i])
			used[i] = true
		}
	}
	for i, it := range items {
		if !used[i] {
			out = append(out, it)
		}
	}
	return out
}

// PermuteStrings returns hashes reordered to match orderedDigests (unknowns appended).
func PermuteStrings(orderedDigests, hashes []string) []string {
	return PermuteByDigests(orderedDigests, hashes, func(s string) string { return s })
}
