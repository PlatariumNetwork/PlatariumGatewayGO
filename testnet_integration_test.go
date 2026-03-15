//go:build integration

// Integration test for test network: requires PlatariumCore (platarium-cli) and optionally a running gateway with -testnet.
// Run: go test -tags=integration -run TestTestnet ./...

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func trimHexOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

const testnetURL = "http://127.0.0.1:2812"

// TestTestnetTransactionRejectNoSignature checks that in testnet mode a TX without signature is rejected.
// Start the gateway with: go run . -testnet -port 2812 -ws 2813
// Then run: go test -tags=integration -run TestTestnet ./...
func TestTestnetTransactionRejectNoSignature(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	body := map[string]interface{}{
		"from": "PxAlice", "to": "PxBob", "amount": "100", "nonce": 0, "timestamp": time.Now().Unix(), "type": "transfer",
	}
	jsonBody, _ := json.Marshal(body)
	resp, err := http.Post(testnetURL+"/pg-sendtx", "application/json", bytes.NewReader(jsonBody))
	if err != nil {
		t.Skipf("gateway not running at %s (start with -testnet -port 2812): %v", testnetURL, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Errorf("testnet should reject TX without signature; got 200")
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Logf("expected 400 Bad Request, got %d", resp.StatusCode)
	}
}

// TestTestnetTransactionValidSignature signs a TX with platarium-cli and sends to testnet; expects 200 if gateway is running.
func TestTestnetTransactionValidSignature(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	rc, err := exec.LookPath("platarium-cli")
	if err != nil {
		// Try relative path from repo root
		rc = "../PlatariumCore/target/release/platarium-cli"
	}

	// Generate mnemonic
	cmd := exec.Command(rc, "generate-mnemonic")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Skipf("platarium-cli not available: %v", err)
		return
	}
	var mnemonic, alphanumeric string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Mnemonic: ") {
			mnemonic = strings.TrimPrefix(line, "Mnemonic: ")
		} else if strings.HasPrefix(line, "Alphanumeric: ") {
			alphanumeric = strings.TrimPrefix(line, "Alphanumeric: ")
		}
	}
	if mnemonic == "" || alphanumeric == "" {
		t.Fatal("could not parse mnemonic")
	}

	// Same key order as gateway's CoreVerifyMessage so JSON hash matches
	type signMsg struct {
		From      string `json:"from"`
		To        string `json:"to"`
		Value     string `json:"value"`
		Nonce     int    `json:"nonce"`
		Timestamp int64  `json:"timestamp"`
		Type      string `json:"type"`
	}
	msg := signMsg{From: "PxFrom", To: "PxTo", Value: "50", Nonce: 0, Timestamp: 1700000000, Type: "transfer"}
	msgJSON, _ := json.Marshal(msg)

	cmd = exec.Command(rc, "sign-message", "--message", string(msgJSON), "--mnemonic", mnemonic, "--alphanumeric", alphanumeric)
	signOut, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sign-message failed: %v", err)
	}

	var signatureHex, pubKeyHex string
	for _, line := range strings.Split(string(signOut), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Public Key:") && pubKeyHex == "" {
			pubKeyHex = strings.TrimSpace(strings.TrimPrefix(trimmed, "Public Key:"))
		}
		if strings.HasPrefix(trimmed, "Compact:") && signatureHex == "" {
			signatureHex = strings.TrimSpace(strings.TrimPrefix(trimmed, "Compact:"))
		}
	}
	if signatureHex == "" || pubKeyHex == "" {
		t.Fatalf("could not parse signature from CLI output")
	}
	// Core verify expects 128 hex (compact). CLI outputs 130 (compact+"01"). Send only first 128.
	signatureHex = trimHexOnly(signatureHex)
	if len(signatureHex) > 128 {
		signatureHex = signatureHex[:128]
	}

	// Build request body (same shape as gateway expects)
	reqBody := map[string]interface{}{
		"from": "PxFrom", "to": "PxTo", "amount": "50", "value": "50", "nonce": 0, "timestamp": 1700000000, "type": "transfer",
		"signature": signatureHex, "pubkey": pubKeyHex,
	}
	jsonBody, _ := json.Marshal(reqBody)
	resp, err := http.Post(testnetURL+"/pg-sendtx", "application/json", bytes.NewReader(jsonBody))
	if err != nil {
		t.Skipf("gateway not running at %s: %v", testnetURL, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var errBody map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		t.Errorf("expected 200 for valid signed TX; got %d: %v", resp.StatusCode, errBody)
	}
}
