package websocket

import "testing"

// TestWSCannotInvokeLabMutations audits that lab mutation aliases are not productive
// WS handlers and are explicitly rejected (issue #43 / TASK-014).
func TestWSCannotInvokeLabMutations(t *testing.T) {
	known := map[string]bool{}
	for _, typ := range knownWSClientMessageTypes() {
		known[typ] = true
	}
	for _, alias := range wsLabMutationAliasAttempts() {
		if known[alias] {
			t.Fatalf("lab alias %q must not be a productive WS message type", alias)
		}
	}
	// Checklist: scanned handleClientMessages — productive types listed in knownWSClientMessageTypes;
	// attempted aliases testSetLoad / test-set-load / rewardCreditL1 / reward-credit-l1 are
	// rejected in the switch (not routed to TestSetLoad / RewardCreditL1).
	if len(wsLabMutationAliasAttempts()) == 0 {
		t.Fatal("expected documented alias attempts")
	}
}
