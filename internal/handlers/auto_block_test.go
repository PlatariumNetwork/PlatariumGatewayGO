package handlers

import (
	"os"
	"testing"

	"platarium-gateway-go/internal/nodes"
)

func withEnv(t *testing.T, key, val string) {
	t.Helper()
	prev, had := os.LookupEnv(key)
	if val == "" {
		_ = os.Unsetenv(key)
	} else {
		_ = os.Setenv(key, val)
	}
	t.Cleanup(func() {
		if !had {
			_ = os.Unsetenv(key)
			return
		}
		_ = os.Setenv(key, prev)
	})
}

func TestAutoBlockEnabled(t *testing.T) {
	withEnv(t, "PLATARIUM_AUTO_BLOCK", "")
	if !AutoBlockEnabled(true) {
		t.Fatal("empty env on testnet should enable")
	}
	if AutoBlockEnabled(false) {
		t.Fatal("empty env off testnet should disable")
	}
	for _, v := range []string{"0", "false", "no", "off", "FALSE", "Off"} {
		withEnv(t, "PLATARIUM_AUTO_BLOCK", v)
		if AutoBlockEnabled(true) || AutoBlockEnabled(false) {
			t.Fatalf("%q must disable", v)
		}
	}
	for _, v := range []string{"1", "true", "yes", "on", "anything"} {
		withEnv(t, "PLATARIUM_AUTO_BLOCK", v)
		if !AutoBlockEnabled(true) || !AutoBlockEnabled(false) {
			t.Fatalf("%q must enable", v)
		}
	}
}

func TestAutoBlockDrainMaxRounds(t *testing.T) {
	withEnv(t, "PLATARIUM_AUTO_BLOCK_DRAIN_MAX", "")
	if got := autoBlockDrainMaxRounds(); got != 12 {
		t.Fatalf("default=%d", got)
	}
	withEnv(t, "PLATARIUM_AUTO_BLOCK_DRAIN_MAX", "3")
	if got := autoBlockDrainMaxRounds(); got != 3 {
		t.Fatalf("got %d", got)
	}
	for _, v := range []string{"0", "-1", "nope", " "} {
		withEnv(t, "PLATARIUM_AUTO_BLOCK_DRAIN_MAX", v)
		if got := autoBlockDrainMaxRounds(); got != 12 {
			t.Fatalf("%q => %d want 12", v, got)
		}
	}
}

func TestAllowBlockCycleAutoConfirm(t *testing.T) {
	if allowBlockCycleAutoConfirm(nil) {
		t.Fatal("nil handler")
	}

	withEnv(t, "PLATARIUM_BLOCK_CYCLE_AUTO_CONFIRM", "off")
	h := &Handler{testnet: true, nodesManager: nodes.NewTestNodesManager()}
	if allowBlockCycleAutoConfirm(h) {
		t.Fatal("explicit disable")
	}

	withEnv(t, "PLATARIUM_BLOCK_CYCLE_AUTO_CONFIRM", "on")
	h = &Handler{testnet: false, nodesManager: nodes.NewTestNodesManager()}
	if !allowBlockCycleAutoConfirm(h) {
		t.Fatal("explicit enable + zero peers")
	}
	h = &Handler{testnet: false, nodesManager: nodes.NewTestNodesManager("peer-a")}
	if allowBlockCycleAutoConfirm(h) {
		t.Fatal("explicit enable + peers must be false")
	}

	withEnv(t, "PLATARIUM_BLOCK_CYCLE_AUTO_CONFIRM", "")
	h = &Handler{testnet: true, nodesManager: nodes.NewTestNodesManager()}
	if !allowBlockCycleAutoConfirm(h) {
		t.Fatal("default testnet + zero peers")
	}
	h = &Handler{testnet: true, nodesManager: nodes.NewTestNodesManager("p1", "p2")}
	if allowBlockCycleAutoConfirm(h) {
		t.Fatal("default testnet + peers")
	}
	h = &Handler{testnet: false, nodesManager: nodes.NewTestNodesManager()}
	if allowBlockCycleAutoConfirm(h) {
		t.Fatal("default non-testnet")
	}
}
