package core

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RustCore wraps the platarium-cli binary for cryptographic operations.
// The Gateway (Go) is the only component that connects to Core; clients and peers never talk to Core directly.
type RustCore struct {
	binaryPath string
}

// NewRustCore creates a new RustCore instance (Gateway connects to Core; Core is used only via this package).
// Binary resolution order: PLATARIUM_CLI_PATH env, then ../PlatariumCore/target/release/platarium-cli, then PATH.
func NewRustCore() (*RustCore, error) {
	// 1) Explicit path from environment (for testnet and CI)
	if path := os.Getenv("PLATARIUM_CLI_PATH"); path != "" {
		if _, err := os.Stat(path); err == nil {
			return &RustCore{binaryPath: path}, nil
		}
	}

	// 2) Relative to current working directory (e.g. when running from PlatariumGatewayGO)
	binaryPath := filepath.Join("..", "PlatariumCore", "target", "release", "platarium-cli")
	absPath, err := filepath.Abs(binaryPath)
	if err == nil {
		if _, err := os.Stat(absPath); err == nil {
			return &RustCore{binaryPath: absPath}, nil
		}
	}
	if _, err := os.Stat(binaryPath); err == nil {
		return &RustCore{binaryPath: binaryPath}, nil
	}

	// 3) System PATH
	if path, err := exec.LookPath("platarium-cli"); err == nil {
		return &RustCore{binaryPath: path}, nil
	}

	return nil, fmt.Errorf("platarium-cli binary not found. Set PLATARIUM_CLI_PATH or build: cd PlatariumCore && cargo build --release")
}

// normalizeSignatureHex returns the signature in the form Core verify-signature expects.
// Core accepts 64 bytes (128 hex) compact or DER. The CLI sign-message outputs compact + "01" (130 hex).
// We always pass exactly 128 hex (compact) when we have at least 128 hex chars, so Core never sees 65 bytes (DER path).
func normalizeSignatureHex(signatureHex string) string {
	signatureHex = strings.TrimSpace(signatureHex)
	// Keep only hex runes (CLI output may have spaces/newlines, or multiple Compact lines)
	var b strings.Builder
	for _, r := range signatureHex {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
			b.WriteRune(r)
		}
	}
	signatureHex = b.String()
	if len(signatureHex) >= 128 {
		return signatureHex[:128]
	}
	return signatureHex
}

// Execute runs a platarium-cli command
func (rc *RustCore) Execute(args []string) (string, error) {
	cmd := exec.Command(rc.binaryPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("rust core execution failed: %v, output: %s", err, string(output))
	}
	return strings.TrimSpace(string(output)), nil
}

// VerifySignature verifies a message signature using Rust Core.
// Core expects either 64 bytes (128 hex chars) compact or DER. The sign-message CLI outputs
// compact + "01" (130 hex chars); we pass only the first 128 hex chars so verification uses compact.
func (rc *RustCore) VerifySignature(message interface{}, signatureHex, pubKeyHex string) (bool, error) {
	sigForCLI := normalizeSignatureHex(signatureHex)

	// Serialize message to JSON
	messageJSON, err := json.Marshal(message)
	if err != nil {
		return false, fmt.Errorf("failed to serialize message: %v", err)
	}

	args := []string{
		"verify-signature",
		"--message", string(messageJSON),
		"--signature", sigForCLI,
		"--pubkey", pubKeyHex,
	}
	
	output, err := rc.Execute(args)
	if err != nil {
		return false, err
	}
	
	// Check if output contains "Verified: true"
	return strings.Contains(output, "Verified: true"), nil
}

// GenerateMnemonic creates a new mnemonic and alphanumeric via Core. Returns mnemonic, alphanumeric, error.
func (rc *RustCore) GenerateMnemonic() (mnemonic, alphanumeric string, err error) {
	output, err := rc.Execute([]string{"generate-mnemonic"})
	if err != nil {
		return "", "", err
	}
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Mnemonic: ") {
			mnemonic = strings.TrimPrefix(line, "Mnemonic: ")
		} else if strings.HasPrefix(line, "Alphanumeric: ") {
			alphanumeric = strings.TrimPrefix(line, "Alphanumeric: ")
		}
	}
	if mnemonic == "" || alphanumeric == "" {
		return "", "", fmt.Errorf("could not parse generate-mnemonic output")
	}
	return mnemonic, alphanumeric, nil
}

// GenerateKeys generates keys from mnemonic
func (rc *RustCore) GenerateKeys(mnemonic, alphanumeric string, seedIndex uint32) (map[string]string, error) {
	args := []string{
		"generate-keys",
		"--mnemonic", mnemonic,
		"--alphanumeric", alphanumeric,
		"--seed-index", fmt.Sprintf("%d", seedIndex),
	}
	
	output, err := rc.Execute(args)
	if err != nil {
		return nil, err
	}
	
	// Parse output
	result := make(map[string]string)
	lines := strings.Split(output, "\n")
	
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Public Key: ") {
			result["publicKey"] = strings.TrimPrefix(line, "Public Key: ")
		} else if strings.HasPrefix(line, "Private Key: ") {
			result["privateKey"] = strings.TrimPrefix(line, "Private Key: ")
		} else if strings.HasPrefix(line, "Signature Key: ") {
			result["signatureKey"] = strings.TrimPrefix(line, "Signature Key: ")
		}
	}
	
	return result, nil
}

// SelectionPercentFromLoad returns the validator selection percent (10–30) from load percentage (0–100).
// Gateway uses Core so this logic is not duplicated; loadPct = LoadScore×100/ScoreScale.
func (rc *RustCore) SelectionPercentFromLoad(loadPct int) (int, error) {
	args := []string{"selection-percent-from-load", "--load-pct", fmt.Sprintf("%d", loadPct)}
	output, err := rc.Execute(args)
	if err != nil {
		return 0, err
	}
	var out struct {
		Percent int `json:"percent"`
	}
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		return 0, fmt.Errorf("parse selection-percent output: %w", err)
	}
	return out.Percent, nil
}

// CommitteeCount returns how many nodes to select for the committee (all logic in Core).
// candidateCount = 1 + peer count; loadPct = LoadScore×100/ScoreScale. Different load → different count.
func (rc *RustCore) CommitteeCount(candidateCount int, loadPct int) (int, error) {
	if candidateCount <= 0 {
		return 0, nil
	}
	args := []string{"committee-count", "--candidates", fmt.Sprintf("%d", candidateCount), "--load-pct", fmt.Sprintf("%d", loadPct)}
	output, err := rc.Execute(args)
	if err != nil {
		return 0, err
	}
	var out struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		return 0, fmt.Errorf("parse committee-count output: %w", err)
	}
	return out.Count, nil
}

// CommitteeCandidate is one (id, weight) for select-committee.
type CommitteeCandidate struct {
	ID     string `json:"id"`
	Weight int64  `json:"weight"`
}

// SelectCommittee selects count node IDs from weighted candidates using Core's deterministic selection.
// seedHex must be 64 hex chars (32 bytes); e.g. hex.EncodeToString(sha256(blockId)).
func (rc *RustCore) SelectCommittee(candidates []CommitteeCandidate, seedHex string, count int) ([]string, error) {
	if count <= 0 || len(candidates) == 0 {
		return nil, nil
	}
	candidatesJSON, err := json.Marshal(candidates)
	if err != nil {
		return nil, fmt.Errorf("marshal candidates: %w", err)
	}
	args := []string{
		"select-committee",
		"--candidates", string(candidatesJSON),
		"--seed-hex", seedHex,
		"--count", fmt.Sprintf("%d", count),
	}
	output, err := rc.Execute(args)
	if err != nil {
		return nil, err
	}
	var selected []string
	if err := json.Unmarshal([]byte(output), &selected); err != nil {
		return nil, fmt.Errorf("parse select-committee output: %w", err)
	}
	return selected, nil
}

// SignMessage signs a message with both keys
func (rc *RustCore) SignMessage(message interface{}, mnemonic, alphanumeric string) (map[string]interface{}, error) {
	// Serialize message to JSON
	messageJSON, err := json.Marshal(message)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize message: %v", err)
	}
	
	args := []string{
		"sign-message",
		"--message", string(messageJSON),
		"--mnemonic", mnemonic,
		"--alphanumeric", alphanumeric,
	}
	
	output, err := rc.Execute(args)
	if err != nil {
		return nil, err
	}
	
	// Parse output - this is complex, so we'll return raw output for now
	// In production, you'd want to parse the structured output
	result := make(map[string]interface{})
	result["raw"] = output
	
	// Extract hash
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Message Hash: ") {
			result["hash"] = strings.TrimPrefix(line, "Message Hash: ")
			break
		}
	}
	
	return result, nil
}

// ValidateTransaction runs Core validate-tx on a transaction JSON (Core format: hash, from, to, asset, amount, fee_uplp, nonce, reads, writes, sig_main, sig_derived).
// txJSON must be the full tx as JSON string. Returns true if valid, false and error message if invalid.
func (rc *RustCore) ValidateTransaction(txJSON string) (valid bool, err error) {
	args := []string{"validate-tx", "--tx", txJSON}
	output, execErr := rc.Execute(args)
	if execErr != nil {
		return false, execErr
	}
	var out struct {
		Valid bool   `json:"valid"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		return false, fmt.Errorf("parse validate-tx output: %w", err)
	}
	if !out.Valid {
		return false, fmt.Errorf("%s", out.Error)
	}
	return true, nil
}// SignTransaction creates a full signed transaction via Core (mnemonic + alphanumeric). Returns the signed tx as JSON string (Core format).
func (rc *RustCore) SignTransaction(from, to, asset string, amount, feeUplp, nonce uint64, reads, writes []string, mnemonic, alphanumeric string) (signedTxJSON string, err error) {
	if reads == nil {
		reads = []string{}
	}
	if writes == nil {
		writes = []string{}
	}
	readsJSON, _ := json.Marshal(reads)
	writesJSON, _ := json.Marshal(writes)
	args := []string{
		"sign-transaction",
		"--from", from,
		"--to", to,
		"--asset", asset,
		"--amount", fmt.Sprintf("%d", amount),
		"--fee-uplp", fmt.Sprintf("%d", feeUplp),
		"--nonce", fmt.Sprintf("%d", nonce),
		"--reads", string(readsJSON),
		"--writes", string(writesJSON),
		"--mnemonic", mnemonic,
		"--alphanumeric", alphanumeric,
	}
	output, err := rc.Execute(args)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}