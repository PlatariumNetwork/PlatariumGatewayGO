package handlers

import (
	"testing"

	"platarium-gateway-go/internal/core"
)

func TestFinalizeVoteRoundTimeoutPendingNotNamedAccepted(t *testing.T) {
	h := &Handler{} // rustCore nil → returns provisional timeoutPending / timeoutRejected
	accepted, _, failed := h.finalizeVoteRoundWithCore(map[string]bool{"n1": true}, true, true)
	if !accepted || failed {
		t.Fatal("timeoutPending=true with no Core → provisional pass")
	}
	rejected, _, failed := h.finalizeVoteRoundWithCore(map[string]bool{"n1": false}, true, false)
	if rejected || failed {
		t.Fatal("timeoutPending=false (timeoutRejected) must not pass")
	}
	// Issue #51: empty / zero-vote aggregation ⇒ no finality (even if timeoutPending was true).
	pass, _, _ := h.finalizeVoteRoundWithCore(nil, false, true)
	if pass {
		t.Fatal("empty votes must not finalize")
	}
}

func TestFinalizeVoteRoundCoreErrorRejects(t *testing.T) {
	// Non-nil Core with empty binary → process-votes Execute fails.
	h := &Handler{rustCore: &core.RustCore{}}
	accepted, _, failed := h.finalizeVoteRoundWithCore(map[string]bool{"n1": true}, true, true)
	if accepted || !failed {
		t.Fatal("Core process-votes error must not accept (no timeoutAccepted fallback)")
	}
	acceptedL2, _, failedL2 := h.finalizeVoteRoundWithCore(map[string]bool{"n1": true}, false, true)
	if acceptedL2 || !failedL2 {
		t.Fatal("L2 Core process-votes error must not accept")
	}
}
