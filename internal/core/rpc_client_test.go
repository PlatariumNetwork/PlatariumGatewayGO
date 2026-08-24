package core

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
)

func TestCLIArgsToParams(t *testing.T) {
	params := cliArgsToParams([]string{
		"state-query",
		"--state-file", "/tmp/state.json",
		"--address", "Pxabc",
		"--asset", "PLP",
	})
	if params["state_file"] != "/tmp/state.json" {
		t.Fatalf("state_file = %v", params["state_file"])
	}
	if params["address"] != "Pxabc" {
		t.Fatalf("address = %v", params["address"])
	}
}

func TestCLICommandToMethod(t *testing.T) {
	if cliCommandToMethod("l1-verify-txs") != "l1_verify_txs" {
		t.Fatal("method mismatch")
	}
}

func TestParseRPCAddr(t *testing.T) {
	n, a := ParseRPCAddr("unix:/tmp/x.sock")
	if n != "unix" || a != "/tmp/x.sock" {
		t.Fatalf("unix: %s %s", n, a)
	}
	n, a = ParseRPCAddr("127.0.0.1:19500")
	if n != "tcp" || a != "127.0.0.1:19500" {
		t.Fatalf("tcp: %s %s", n, a)
	}
	n, a = ParseRPCAddr("/var/run/core.sock")
	if n != "unix" || a != "/var/run/core.sock" {
		t.Fatalf("path: %s %s", n, a)
	}
}

func TestNormalizeRPCOutputVerify(t *testing.T) {
	// M4: security-sensitive methods keep JSON (no prose rewrite).
	out, err := normalizeRPCOutput("verify_signature", `{"verified":true}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != `{"verified":true}` {
		t.Fatalf("unexpected: %q", out)
	}
	out, err = normalizeRPCOutput("generate_keys", `{"publicKey":"a","privateKey":"b"}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != `{"publicKey":"a","privateKey":"b"}` {
		t.Fatalf("unexpected: %q", out)
	}
}

func TestIsMutatingRPCMethod(t *testing.T) {
	if !isMutatingRPCMethod("state_apply_tx") {
		t.Fatal("state_apply_tx should be mutating")
	}
	if isMutatingRPCMethod("ping") {
		t.Fatal("ping should not be mutating")
	}
	if isMutatingRPCMethod("handshake") {
		t.Fatal("handshake should not be mutating")
	}
}

// M3/H8: mismatched response id must fail closed.
func TestRPCClientRejectsIDMismatch(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			br := bufio.NewReader(c)
			_, _ = br.ReadString('\n')
			_, _ = fmt.Fprintf(c, "{\"jsonrpc\":\"2.0\",\"id\":999,\"result\":{\"ok\":true}}\n")
			_ = c.Close()
		}
	}()

	client, err := NewRPCClient(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Call("ping", map[string]interface{}{})
	if err == nil || !strings.Contains(err.Error(), "id mismatch") {
		t.Fatalf("expected id mismatch, got %v", err)
	}
}
