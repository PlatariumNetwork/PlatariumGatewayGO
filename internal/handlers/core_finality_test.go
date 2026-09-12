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

// coreRPCServer starts a newline JSON-RPC stub. handler receives the request id and method.
func coreRPCServer(t *testing.T, handle func(id int64, method string) (line string, hang bool)) (addr string, closeFn func()) {
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
					line, hang := handle(req.ID, req.Method)
					if hang {
						time.Sleep(2 * time.Second)
						return
					}
					if _, err := fmt.Fprintf(conn, "%s\n", line); err != nil {
						return
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
	addr, closeFn := coreRPCServer(t, func(id int64, method string) (string, bool) {
		// Hang without responding — client deadline fires as timeout.
		return "", true
	})
	defer closeFn()

	rc := rustCoreOnRPC(t, addr)
	rc.SetRPCCallTimeout(80 * time.Millisecond)
	defer rc.Close()

	h := &Handler{rustCore: rc}
	finalized, _ := h.finalizeVoteRoundWithCore(map[string]bool{"n1": true}, true, true)
	if finalized {
		t.Fatal("simulated Core timeout ⇒ finalized == false")
	}
	finalizedL2, _ := h.finalizeVoteRoundWithCore(map[string]bool{"n1": true}, false, true)
	if finalizedL2 {
		t.Fatal("L2 simulated Core timeout ⇒ finalized == false")
	}
}

// Issue #48: simulated Core HTTP 500 / JSON-RPC error ⇒ finalized == false.
func TestCoreHTTP500RPCErrorNoFinality(t *testing.T) {
	addr, closeFn := coreRPCServer(t, func(id int64, method string) (string, bool) {
		// JSON-RPC error analogous to upstream HTTP 500 / Internal error.
		return fmt.Sprintf(
			`{"jsonrpc":"2.0","id":%d,"error":{"code":-32603,"message":"HTTP 500 Internal Server Error"}}`,
			id,
		), false
	})
	defer closeFn()

	rc := rustCoreOnRPC(t, addr)
	rc.SetRPCCallTimeout(2 * time.Second)
	defer rc.Close()

	h := &Handler{rustCore: rc}
	finalized, _ := h.finalizeVoteRoundWithCore(map[string]bool{"n1": true}, true, true)
	if finalized {
		t.Fatal("simulated Core HTTP 500 / RPC error ⇒ finalized == false")
	}
	finalizedL2, _ := h.finalizeVoteRoundWithCore(map[string]bool{"n1": true}, false, true)
	if finalizedL2 {
		t.Fatal("L2 simulated Core RPC error ⇒ finalized == false")
	}
}
