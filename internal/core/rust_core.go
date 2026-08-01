package core

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// RustCore wraps Platarium Core — either a long-lived JSON-RPC daemon (default)
// or platarium-cli subprocesses (PLATARIUM_CORE_MODE=cli).
// The Gateway (Go) is the only component that connects to Core; clients and peers never talk to Core directly.
type RustCore struct {
	binaryPath string
	rpcClient  *RPCClient
}

// NewRustCore creates a new RustCore instance (Gateway connects to Core; Core is used only via this package).
// Mode: PLATARIUM_CORE_MODE=rpc (default) or cli (subprocess per call).
// RPC addr: PLATARIUM_CORE_RPC_ADDR (tcp host:port or unix:/path); default unix:/tmp/platarium-core.sock.
// When RPC mode and daemon is down, Gateway auto-starts `platarium-cli serve` unless
// PLATARIUM_CORE_RPC_AUTOSTART=0.
func NewRustCore() (*RustCore, error) {
	rc := &RustCore{}

	mode := strings.ToLower(strings.TrimSpace(os.Getenv("PLATARIUM_CORE_MODE")))
	if mode == "" {
		mode = "rpc"
	}

	if mode == "rpc" {
		addr := DefaultCoreRPCAddr()
		autostart := strings.ToLower(strings.TrimSpace(os.Getenv("PLATARIUM_CORE_RPC_AUTOSTART")))
		if autostart != "0" && autostart != "false" && autostart != "no" {
			cliPath, _ := resolveCLIPath()
			if err := EnsureCoreDaemon(cliPath, addr); err != nil {
				return nil, fmt.Errorf("core rpc daemon: %w (set PLATARIUM_CORE_MODE=cli to use subprocesses)", err)
			}
		}
		client, err := NewRPCClient(addr)
		if err != nil {
			return nil, err
		}
		if err := client.Ping(); err != nil {
			return nil, fmt.Errorf("core rpc ping %s: %w", addr, err)
		}
		rc.rpcClient = client
		rc.binaryPath, _ = resolveCLIPath()
		return rc, nil
	}

	path, err := resolveCLIPath()
	if err != nil {
		return nil, err
	}
	rc.binaryPath = path
	return rc, nil
}

// Mode reports "rpc" or "cli".
func (rc *RustCore) Mode() string {
	if rc.rpcClient != nil {
		return "rpc"
	}
	return "cli"
}

// Close releases the persistent RPC connection (does not stop a shared Core daemon).
func (rc *RustCore) Close() {
	if rc.rpcClient != nil {
		_ = rc.rpcClient.Close()
	}
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

// Execute runs a platarium-cli command (subprocess) or JSON-RPC call when PLATARIUM_CORE_MODE=rpc.
// Oversized JSON argv values are spilled to temp files (@path) so CLI mode does not hit ARG_MAX.
func (rc *RustCore) Execute(args []string) (string, error) {
	if rc.rpcClient != nil {
		return rc.rpcClient.ExecuteRPC(args)
	}
	args, cleanup, err := spillLargeCLIArgs(args)
	if err != nil {
		return "", fmt.Errorf("spill large CLI args: %w", err)
	}
	defer cleanup()

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

// StateInit initializes an empty Core state file.
func (rc *RustCore) StateInit(stateFile string) (string, error) {
	return rc.Execute([]string{"state-init", "--state-file", stateFile})
}

// StateQuery queries balance, uplp, and nonce from Core state file.
func (rc *RustCore) StateQuery(stateFile, address, asset string) (string, error) {
	if asset == "" {
		asset = "PLP"
	}
	return rc.Execute([]string{
		"state-query",
		"--state-file", stateFile,
		"--address", address,
		"--asset", asset,
	})
}

// StateValidateTx validates a transaction against Core state without applying.
func (rc *RustCore) StateValidateTx(stateFile, txJSON string) (bool, error) {
	output, execErr := rc.Execute([]string{"state-validate-tx", "--state-file", stateFile, "--tx", txJSON})
	if execErr != nil {
		return false, execErr
	}
	var out struct {
		Valid bool   `json:"valid"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		return false, fmt.Errorf("parse state-validate-tx output: %w", err)
	}
	if !out.Valid {
		if out.Error != "" {
			return false, fmt.Errorf("%s", out.Error)
		}
		return false, fmt.Errorf("invalid transaction")
	}
	return true, nil
}

// StateApplyTx applies a transaction to Core state file.
func (rc *RustCore) StateApplyTx(stateFile, txJSON string) (string, error) {
	return rc.Execute([]string{"state-apply-tx", "--state-file", stateFile, "--tx", txJSON})
}

// StateCredit credits PLP and μPLP to an address (testnet only).
func (rc *RustCore) StateCredit(stateFile, address string, plp, uplp uint64, testnet bool) (string, error) {
	args := []string{
		"state-credit",
		"--state-file", stateFile,
		"--address", address,
		"--plp", fmt.Sprintf("%d", plp),
		"--uplp", fmt.Sprintf("%d", uplp),
	}
	if testnet {
		args = append(args, "--testnet")
	}
	return rc.Execute(args)
}

// StateRoot returns the deterministic state root from Core state file.
func (rc *RustCore) StateRoot(stateFile string) (string, error) {
	return rc.Execute([]string{"state-root", "--state-file", stateFile})
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
}

// SignTransaction creates a full signed transaction via Core (mnemonic + alphanumeric). Returns the signed tx as JSON string (Core format).
// Optional escrow fields are hashed into the signature (must match Transaction::compute_hash).
func (rc *RustCore) SignTransaction(from, to, asset string, amount, feeUplp, nonce uint64, reads, writes []string, mnemonic, alphanumeric string) (signedTxJSON string, err error) {
	return rc.SignTransactionExt(from, to, asset, amount, feeUplp, nonce, reads, writes, mnemonic, alphanumeric, nil)
}

// EscrowSignOpts optional fields for escrow_lock / escrow_settle.
type EscrowSignOpts struct {
	TxKind           string
	EscrowID         string
	Purpose          string
	ExpiresAt        uint64
	SettleOutcomeKey string
	SettlePayee      string
	SettleNode       string
}

func (rc *RustCore) SignTransactionExt(
	from, to, asset string,
	amount, feeUplp, nonce uint64,
	reads, writes []string,
	mnemonic, alphanumeric string,
	escrow *EscrowSignOpts,
) (signedTxJSON string, err error) {
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
	if escrow != nil {
		if escrow.TxKind != "" {
			args = append(args, "--tx-kind", escrow.TxKind)
		}
		if escrow.EscrowID != "" {
			args = append(args, "--escrow-id", escrow.EscrowID)
		}
		if escrow.Purpose != "" {
			args = append(args, "--purpose", escrow.Purpose)
		}
		if escrow.ExpiresAt > 0 {
			args = append(args, "--expires-at", fmt.Sprintf("%d", escrow.ExpiresAt))
		}
		if escrow.SettleOutcomeKey != "" {
			args = append(args, "--settle-outcome-key", escrow.SettleOutcomeKey)
		}
		if escrow.SettlePayee != "" {
			args = append(args, "--settle-payee", escrow.SettlePayee)
		}
		if escrow.SettleNode != "" {
			args = append(args, "--settle-node", escrow.SettleNode)
		}
	}
	output, err := rc.Execute(args)
	if err != nil {
		return "", err
	}
	signed := strings.TrimSpace(output)
	if escrow != nil && escrow.TxKind != "" {
		var check struct {
			TxKind   string `json:"tx_kind"`
			EscrowID string `json:"escrow_id"`
		}
		if err := json.Unmarshal([]byte(signed), &check); err != nil {
			return "", fmt.Errorf("parse signed tx: %w", err)
		}
		if check.TxKind != escrow.TxKind {
			return "", fmt.Errorf(
				"Core signed tx missing tx_kind=%q (got %q): platarium-cli/RPC daemon is outdated or not restarted — rebuild PlatariumCore (`cargo build --release`) and restart the Core RPC process (kill platarium-cli serve / remove stale unix socket). Signing without escrow fields then admitting with tx_kind causes: Invalid signature: One or both signatures are invalid",
				escrow.TxKind, check.TxKind,
			)
		}
		if escrow.EscrowID != "" && check.EscrowID != escrow.EscrowID {
			return "", fmt.Errorf(
				"Core signed tx missing escrow_id=%q (got %q): restart/update platarium-cli RPC daemon",
				escrow.EscrowID, check.EscrowID,
			)
		}
	}
	return signed, nil
}