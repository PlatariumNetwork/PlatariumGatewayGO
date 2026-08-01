package core

import "testing"

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
	out, err := normalizeRPCOutput("verify_signature", `{"verified":true}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "Verified: true\nSignature is valid." {
		t.Fatalf("unexpected: %q", out)
	}
}
