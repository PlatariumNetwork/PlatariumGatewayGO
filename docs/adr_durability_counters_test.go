package docs

import (
	"strings"
	"testing"
)

func TestADRDurabilityCounters(t *testing.T) {
	if ADRDurabilityCounters == "" {
		t.Fatal("ADR missing")
	}
	for _, needle := range []string{"core_rpc_errors", "rocks_commit_errors", "state_rocks_divergence", "/internal/counters"} {
		if !strings.Contains(ADRDurabilityCounters, needle) {
			t.Fatalf("ADR missing %q", needle)
		}
	}
}
