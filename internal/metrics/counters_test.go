package metrics

import (
	"strings"
	"testing"
)

func TestDurabilityCountersVisible(t *testing.T) {
	c := &DurabilityCounters{}
	c.IncCoreRPCErrors()
	c.IncRocksCommitErrors()
	c.IncStateRocksDivergence()
	c.IncStateRocksDivergence()
	s := c.Snapshot()
	if s.CoreRPCErrors != 1 || s.RocksCommitErrors != 1 || s.StateRocksDivergence != 2 {
		t.Fatalf("snapshot=%+v", s)
	}
	c.ResetForTest()
	if c.Snapshot() != (Snapshot{}) {
		t.Fatal("reset failed")
	}
}

func TestHowToReadDocumentsCounterNames(t *testing.T) {
	for _, name := range []string{"core_rpc_errors", "rocks_commit_errors", "state_rocks_divergence", "/internal/counters"} {
		if !strings.Contains(HowToRead, name) {
			t.Fatalf("HowToRead missing %q", name)
		}
	}
}
