package metrics

import (
	"sync/atomic"
)

// DurabilityCounters are minimal in-process counters for Core RPC / Rocks / divergence (#69).
// Not a Prometheus stack — read via Snapshot() or GET /internal/counters.
type DurabilityCounters struct {
	coreRPCErrors         atomic.Int64
	rocksCommitErrors     atomic.Int64
	stateRocksDivergence  atomic.Int64
}

// Global is the process-wide counter set used by Gateway handlers and Core RPC.
var Global = &DurabilityCounters{}

// Snapshot is a plain copy for JSON / diagnostics.
type Snapshot struct {
	CoreRPCErrors        int64 `json:"core_rpc_errors"`
	RocksCommitErrors    int64 `json:"rocks_commit_errors"`
	StateRocksDivergence int64 `json:"state_rocks_divergence"`
}

// HowToRead documents operator access for durability counters (#69).
const HowToRead = `Durability counters (issue #69)

Names:
  - core_rpc_errors: Core JSON-RPC / CLI Execute failures
  - rocks_commit_errors: RocksDB commit failures after explorer apply
  - state_rocks_divergence: consistency diagnostic reported DIVERGED

How to read:
  - GET /internal/counters — JSON Snapshot (in-process, not Prometheus)
  - metrics.Global.Snapshot() from Go tests / tooling
  - ResetForTest() only in unit tests

These counters are diagnostic visibility only; they do not repair ledgers.
`

func (c *DurabilityCounters) IncCoreRPCErrors() {
	if c == nil {
		return
	}
	c.coreRPCErrors.Add(1)
}

func (c *DurabilityCounters) IncRocksCommitErrors() {
	if c == nil {
		return
	}
	c.rocksCommitErrors.Add(1)
}

func (c *DurabilityCounters) IncStateRocksDivergence() {
	if c == nil {
		return
	}
	c.stateRocksDivergence.Add(1)
}

func (c *DurabilityCounters) Snapshot() Snapshot {
	if c == nil {
		return Snapshot{}
	}
	return Snapshot{
		CoreRPCErrors:        c.coreRPCErrors.Load(),
		RocksCommitErrors:    c.rocksCommitErrors.Load(),
		StateRocksDivergence: c.stateRocksDivergence.Load(),
	}
}

// ResetForTest zeroes counters (unit tests only).
func (c *DurabilityCounters) ResetForTest() {
	if c == nil {
		return
	}
	c.coreRPCErrors.Store(0)
	c.rocksCommitErrors.Store(0)
	c.stateRocksDivergence.Store(0)
}
