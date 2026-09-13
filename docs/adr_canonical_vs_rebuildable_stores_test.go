package docs

import (
	"strings"
	"testing"
)

func TestADRCanonicalVsRebuildableStores(t *testing.T) {
	body := ADRCanonicalVsRebuildableStores
	needles := []string{
		"RocksDB",
		"chain.json",
		"TASK-015",
		"TASK-033",
		"COMMITTED",
		"fail-closed",
		"Crash recovery",
		"tip leading Rocks",
	}
	for _, n := range needles {
		if !strings.Contains(body, n) {
			t.Fatalf("ADR missing required reference %q", n)
		}
	}
	if !strings.Contains(body, "flag matrix") && !strings.Contains(body, "Flag matrix") {
		t.Fatal("ADR must reference TASK-015 flag matrix")
	}
	if !strings.Contains(body, "failure table") && !strings.Contains(body, "finalize contract") {
		t.Fatal("ADR must point at Core finalize failure table")
	}
	if !strings.Contains(body, "rebuild") || !strings.Contains(body, "from Rocks") {
		t.Fatal("ADR crash recovery must require rebuild from Rocks")
	}
}
