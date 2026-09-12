package handlers

import (
	"testing"

	"platarium-gateway-go/internal/core"
)

func TestFinalizeVoteRoundTimeoutPendingNotNamedAccepted(t *testing.T) {
	h := &Handler{} // rustCore nil → returns provisional timeoutPending / timeoutRejected
	accepted, _ := h.finalizeVoteRoundWithCore(map[string]bool{"n1": true}, true, true)
	if !accepted {
		t.Fatal("timeoutPending=true with no Core → provisional pass")
	}
	rejected, _ := h.finalizeVoteRoundWithCore(map[string]bool{"n1": false}, true, false)
	if rejected {
		t.Fatal("timeoutPending=false (timeoutRejected) must not pass")
	}
	// Empty votes: still returns the timeoutPending provisional value.
	pass, _ := h.finalizeVoteRoundWithCore(nil, false, true)
	if !pass {
		t.Fatal("empty votes + timeoutPending")
	}
}

func TestFinalizeVoteRoundCoreErrorRejects(t *testing.T) {
	// Non-nil Core with empty binary → process-votes Execute fails.
	h := &Handler{rustCore: &core.RustCore{}}
	accepted, _ := h.finalizeVoteRoundWithCore(map[string]bool{"n1": true}, true, true)
	if accepted {
		t.Fatal("Core process-votes error must not accept (no timeoutAccepted fallback)")
	}
	acceptedL2, _ := h.finalizeVoteRoundWithCore(map[string]bool{"n1": true}, false, true)
	if acceptedL2 {
		t.Fatal("L2 Core process-votes error must not accept")
	}
}
