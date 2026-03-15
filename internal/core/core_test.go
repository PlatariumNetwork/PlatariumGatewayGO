package core

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// TestRustCoreAvailable checks that platarium-cli is available (used by testnet).
func TestRustCoreAvailable(t *testing.T) {
	rc, err := NewRustCore()
	if err != nil {
		t.Skipf("Platarium Core not available (build PlatariumCore first): %v", err)
		return
	}
	_ = rc
}

// TestVerifySignatureWithRealCore runs a full sign-then-verify cycle using platarium-cli when available.
// This ensures transactions are verified correctly by the Core in the test network.
func TestVerifySignatureWithRealCore(t *testing.T) {
	rc, err := NewRustCore()
	if err != nil {
		t.Skipf("Platarium Core not available: %v", err)
		return
	}

	// Generate mnemonic via CLI
	out, err := rc.Execute([]string{"generate-mnemonic"})
	if err != nil {
		t.Fatalf("generate-mnemonic failed: %v", err)
	}
	var mnemonic, alphanumeric string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Mnemonic: ") {
			mnemonic = strings.TrimPrefix(line, "Mnemonic: ")
		} else if strings.HasPrefix(line, "Alphanumeric: ") {
			alphanumeric = strings.TrimPrefix(line, "Alphanumeric: ")
		}
	}
	if mnemonic == "" || alphanumeric == "" {
		t.Fatal("could not parse mnemonic output")
	}

	// Message that looks like a TX (same format as gateway uses for verification)
	msg := map[string]interface{}{
		"from": "PxAlice", "to": "PxBob", "value": "100", "nonce": 0, "timestamp": 1700000000, "type": "transfer",
	}
	msgJSON, _ := json.Marshal(msg)

	// Sign via CLI
	signOut, err := rc.Execute([]string{
		"sign-message",
		"--message", string(msgJSON),
		"--mnemonic", mnemonic,
		"--alphanumeric", alphanumeric,
	})
	if err != nil {
		t.Fatalf("sign-message failed: %v", err)
	}

	// Parse first Main Signature block: "  Public Key: ..." and "  Compact: ..."
	var signatureHex, pubKeyHex string
	for _, line := range strings.Split(signOut, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Public Key:") && pubKeyHex == "" {
			pubKeyHex = strings.TrimSpace(strings.TrimPrefix(trimmed, "Public Key:"))
		}
		if strings.HasPrefix(trimmed, "Compact:") && signatureHex == "" {
			signatureHex = strings.TrimSpace(strings.TrimPrefix(trimmed, "Compact:"))
		}
	}
	if signatureHex == "" || pubKeyHex == "" {
		t.Logf("sign output: %s", signOut)
		t.Fatal("could not parse signature or pubkey from sign output")
	}

	// Verify using RustCore
	verified, err := rc.VerifySignature(msg, signatureHex, pubKeyHex)
	if err != nil {
		t.Fatalf("VerifySignature failed: %v", err)
	}
	if !verified {
		t.Fatal("expected signature to verify")
	}

	// Invalid signature must not verify
	verifiedBad, _ := rc.VerifySignature(msg, signatureHex+"00", pubKeyHex)
	if verifiedBad {
		t.Fatal("invalid signature should not verify")
	}
}

// TestExecuteGenerateMnemonic ensures CLI execute works (smoke test for testnet).
func TestExecuteGenerateMnemonic(t *testing.T) {
	rc, err := NewRustCore()
	if err != nil {
		t.Skipf("Platarium Core not available: %v", err)
		return
	}
	out, err := rc.Execute([]string{"generate-mnemonic"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Mnemonic:") {
		t.Errorf("unexpected output: %s", out)
	}
}
