package core

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// RustCore wraps the platarium-cli binary for cryptographic operations
type RustCore struct {
	binaryPath string
}

// NewRustCore creates a new RustCore instance
func NewRustCore() (*RustCore, error) {
	// Try to find the binary relative to the Go server
	// Assuming PlatariumCore is in the parent directory
	binaryPath := filepath.Join("..", "PlatariumCore", "target", "release", "platarium-cli")
	
	// Check if binary exists using absolute path resolution
	absPath, err := filepath.Abs(binaryPath)
	if err == nil {
		// Check if file exists
		if _, err := exec.LookPath(absPath); err == nil {
			return &RustCore{
				binaryPath: absPath,
			}, nil
		}
	}
	
	// Try relative path from current working directory
	if _, err := exec.LookPath(binaryPath); err == nil {
		return &RustCore{
			binaryPath: binaryPath,
		}, nil
	}
	
	// Try system PATH
	if path, err := exec.LookPath("platarium-cli"); err == nil {
		return &RustCore{
			binaryPath: path,
		}, nil
	}
	
	return nil, fmt.Errorf("platarium-cli binary not found. Please build PlatariumCore first: cd PlatariumCore && cargo build --release")
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

// VerifySignature verifies a message signature using Rust Core
func (rc *RustCore) VerifySignature(message interface{}, signatureHex, pubKeyHex string) (bool, error) {
	// Serialize message to JSON
	messageJSON, err := json.Marshal(message)
	if err != nil {
		return false, fmt.Errorf("failed to serialize message: %v", err)
	}
	
	args := []string{
		"verify-signature",
		"--message", string(messageJSON),
		"--signature", signatureHex,
		"--pubkey", pubKeyHex,
	}
	
	output, err := rc.Execute(args)
	if err != nil {
		return false, err
	}
	
	// Check if output contains "Verified: true"
	return strings.Contains(output, "Verified: true"), nil
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

