package handlers

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"platarium-gateway-go/internal/core"
)

// coreRPCStubMode controls how the stub reacts after reading a request.
type coreRPCStubMode int

const (
	coreRPCRespond coreRPCStubMode = iota
	coreRPCHang
	coreRPCDisconnect
)

// coreRPCServer starts a newline JSON-RPC stub. handler receives the request id and method.
func coreRPCServer(t *testing.T, handle func(id int64, method string) (line string, mode coreRPCStubMode)) (addr string, closeFn func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				select {
				case <-done:
					return
				default:
					return
				}
			}
			go func(conn net.Conn) {
				defer conn.Close()
				br := bufio.NewReader(conn)
				for {
					raw, err := br.ReadString('\n')
					if err != nil {
						return
					}
					var req struct {
						ID     int64  `json:"id"`
						Method string `json:"method"`
					}
					if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &req); err != nil {
						return
					}
					line, mode := handle(req.ID, req.Method)
					switch mode {
					case coreRPCHang:
						time.Sleep(2 * time.Second)
						return
					case coreRPCDisconnect:
						// Crash / socket disconnect: drop the TCP session with no response body.
						return
					default:
						if _, err := fmt.Fprintf(conn, "%s\n", line); err != nil {
							return
						}
					}
				}
			}(c)
		}
	}()
	return ln.Addr().String(), func() {
		close(done)
		_ = ln.Close()
	}
}

func rustCoreOnRPC(t *testing.T, addr string) *core.RustCore {
	t.Helper()
	client, err := core.NewRPCClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	return core.NewRustCoreFromRPC(client)
}

// Issue #47: simulated Core RPC timeout ⇒ finalized == false.
func TestCoreTimeoutNoFinality(t *testing.T) {
	addr, closeFn := coreRPCServer(t, func(id int64, method string) (string, coreRPCStubMode) {
		return "", coreRPCHang
	})
	defer closeFn()

	rc := rustCoreOnRPC(t, addr)
	rc.SetRPCCallTimeout(80 * time.Millisecond)
	defer rc.Close()

	h := &Handler{rustCore: rc}
	finalized, _, failed := h.finalizeVoteRoundWithCore(map[string]bool{"n1": true}, true, true)
	if finalized || !failed {
		t.Fatal("simulated Core timeout ⇒ finalized == false and coreRPCFailed")
	}
	finalizedL2, _, failedL2 := h.finalizeVoteRoundWithCore(map[string]bool{"n1": true}, false, true)
	if finalizedL2 || !failedL2 {
		t.Fatal("L2 simulated Core timeout ⇒ finalized == false")
	}
}

// Issue #48: simulated Core HTTP 500 / JSON-RPC error ⇒ finalized == false.
func TestCoreHTTP500RPCErrorNoFinality(t *testing.T) {
	addr, closeFn := coreRPCServer(t, func(id int64, method string) (string, coreRPCStubMode) {
		return fmt.Sprintf(
			`{"jsonrpc":"2.0","id":%d,"error":{"code":-32603,"message":"HTTP 500 Internal Server Error"}}`,
			id,
		), coreRPCRespond
	})
	defer closeFn()

	rc := rustCoreOnRPC(t, addr)
	rc.SetRPCCallTimeout(2 * time.Second)
	defer rc.Close()

	h := &Handler{rustCore: rc}
	finalized, _, failed := h.finalizeVoteRoundWithCore(map[string]bool{"n1": true}, true, true)
	if finalized || !failed {
		t.Fatal("simulated Core HTTP 500 / RPC error ⇒ finalized == false")
	}
	finalizedL2, _, failedL2 := h.finalizeVoteRoundWithCore(map[string]bool{"n1": true}, false, true)
	if finalizedL2 || !failedL2 {
		t.Fatal("L2 simulated Core RPC error ⇒ finalized == false")
	}
}

// Issue #49: malformed Core vote JSON/body ⇒ finalized == false.
func TestMalformedCoreVoteResponseNoFinality(t *testing.T) {
	cases := []struct {
		name string
		line func(id int64) string
	}{
		{"not_json", func(id int64) string { return `<<<not-json>>>` }},
		{"truncated_object", func(id int64) string {
			return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"confirmed":tru`, id)
		}},
		{"result_not_object", func(id int64) string {
			return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":"confirmed-true"}`, id)
		}},
		{"confirmed_wrong_type", func(id int64) string {
			return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"confirmed":"yes","to_penalize":[]}}`, id)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			addr, closeFn := coreRPCServer(t, func(id int64, method string) (string, coreRPCStubMode) {
				return tc.line(id), coreRPCRespond
			})
			defer closeFn()

			rc := rustCoreOnRPC(t, addr)
			rc.SetRPCCallTimeout(2 * time.Second)
			defer rc.Close()

			h := &Handler{rustCore: rc}
			finalized, _, _ := h.finalizeVoteRoundWithCore(map[string]bool{"n1": true}, true, true)
			if finalized {
				t.Fatalf("malformed Core vote response (%s) ⇒ finalized == false", tc.name)
			}
			finalizedL2, _, _ := h.finalizeVoteRoundWithCore(map[string]bool{"n1": true}, false, true)
			if finalizedL2 {
				t.Fatalf("L2 malformed Core vote response (%s) ⇒ finalized == false", tc.name)
			}
		})
	}
}

// Issue #50: Core process crash / socket disconnect ⇒ finalized == false.
func TestCoreCrashDisconnectNoFinality(t *testing.T) {
	addr, closeFn := coreRPCServer(t, func(id int64, method string) (string, coreRPCStubMode) {
		return "", coreRPCDisconnect
	})
	defer closeFn()

	rc := rustCoreOnRPC(t, addr)
	rc.SetRPCCallTimeout(500 * time.Millisecond)
	defer rc.Close()

	h := &Handler{rustCore: rc}
	finalized, _, failed := h.finalizeVoteRoundWithCore(map[string]bool{"n1": true}, true, true)
	if finalized || !failed {
		t.Fatal("Core crash/disconnect ⇒ finalized == false")
	}
	finalizedL2, _, failedL2 := h.finalizeVoteRoundWithCore(map[string]bool{"n1": true}, false, true)
	if finalizedL2 || !failedL2 {
		t.Fatal("L2 Core crash/disconnect ⇒ finalized == false")
	}
}

// Issue #50 (crash mid-flight): accept then close the listening socket so the peer sees reset.
func TestCoreListenerCrashNoFinality(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		_ = ln.Close()
		_ = c.Close() // process crash: accept then die without response
	}()

	rc := rustCoreOnRPC(t, addr)
	rc.SetRPCCallTimeout(500 * time.Millisecond)
	defer rc.Close()

	h := &Handler{rustCore: rc}
	finalized, _, failed := h.finalizeVoteRoundWithCore(map[string]bool{"n1": true}, true, true)
	if finalized || !failed {
		t.Fatal("listener crash ⇒ finalized == false")
	}
}

// Issue #51: empty / zero-vote aggregation ⇒ finalized == false.
func TestEmptyVoteResultNoFinality(t *testing.T) {
	hNil := &Handler{}
	finalized, _, _ := hNil.finalizeVoteRoundWithCore(nil, true, true)
	if finalized {
		t.Fatal("empty votes map ⇒ finalized == false")
	}
	finalized, _, _ = hNil.finalizeVoteRoundWithCore(map[string]bool{}, false, true)
	if finalized {
		t.Fatal("zero-length votes ⇒ finalized == false")
	}

	// Core returns empty / unconfirmed aggregation body.
	addr, closeFn := coreRPCServer(t, func(id int64, method string) (string, coreRPCStubMode) {
		return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{}}`, id), coreRPCRespond
	})
	defer closeFn()

	rc := rustCoreOnRPC(t, addr)
	rc.SetRPCCallTimeout(2 * time.Second)
	defer rc.Close()

	h := &Handler{rustCore: rc}
	finalized, _, failed := h.finalizeVoteRoundWithCore(map[string]bool{"n1": false}, true, true)
	if finalized {
		t.Fatal("empty Core vote result (confirmed unset) ⇒ finalized == false")
	}
	if failed {
		t.Fatal("empty object result is parseable; coreRPCFailed should be false")
	}
	finalizedL2, _, _ := h.finalizeVoteRoundWithCore(map[string]bool{"n1": false}, false, true)
	if finalizedL2 {
		t.Fatal("L2 empty Core vote result ⇒ finalized == false")
	}
}

// Issue #53/#77: degraded opt-in still rejects on Core RPC / process-votes error (fail-closed).
// L1 and L2 handlers call finalizeVoteRoundWithCore → maybeDegradedAccept; Core failure must
// not flip accepted=true even when only the local proposer/confirmer voted yes.
func TestDegradedConsensusStillFailClosedOnCoreRPCError(t *testing.T) {
	t.Setenv("PLATARIUM_ALLOW_DEGRADED_CONSENSUS", "true")
	if !allowDegradedConsensus() {
		t.Fatal("expected degraded opt-in")
	}

	addr, closeFn := coreRPCServer(t, func(id int64, method string) (string, coreRPCStubMode) {
		return fmt.Sprintf(
			`{"jsonrpc":"2.0","id":%d,"error":{"code":-32000,"message":"core unavailable"}}`,
			id,
		), coreRPCRespond
	})
	defer closeFn()

	rc := rustCoreOnRPC(t, addr)
	rc.SetRPCCallTimeout(2 * time.Second)
	defer rc.Close()

	h := &Handler{rustCore: rc}
	myID := "proposer-1"
	votes := map[string]bool{myID: true}

	// L1: Core RPC error → reject; degraded must not override (multi-node, sole yes vote).
	finalized, _, coreFailed := h.finalizeVoteRoundWithCore(votes, true, true)
	if finalized || !coreFailed {
		t.Fatal("L1 Core RPC error must reject and mark coreRPCFailed")
	}
	accepted, applied := maybeDegradedAccept(finalized, coreFailed, 3, votes, myID)
	if applied || accepted {
		t.Fatal("L1 degraded opt-in must not accept when Core RPC failed")
	}

	// L2: same fail-closed contract.
	finalizedL2, _, coreFailedL2 := h.finalizeVoteRoundWithCore(votes, false, true)
	if finalizedL2 || !coreFailedL2 {
		t.Fatal("L2 Core RPC error must reject and mark coreRPCFailed")
	}
	acceptedL2, appliedL2 := maybeDegradedAccept(finalizedL2, coreFailedL2, 3, votes, myID)
	if appliedL2 || acceptedL2 {
		t.Fatal("L2 degraded opt-in must not accept when Core RPC failed")
	}
}

func TestMaybeDegradedAcceptStillWorksWithoutCoreFailure(t *testing.T) {
	t.Setenv("PLATARIUM_ALLOW_DEGRADED_CONSENSUS", "true")
	myID := "proposer-1"
	votes := map[string]bool{myID: true}
	accepted, applied := maybeDegradedAccept(false, false, 3, votes, myID)
	if !accepted || !applied {
		t.Fatal("degraded path may accept proposer-only when Core did not fail")
	}
	accepted, applied = maybeDegradedAccept(false, true, 3, votes, myID)
	if accepted || applied {
		t.Fatal("coreRPCFailed blocks degraded accept")
	}
}
