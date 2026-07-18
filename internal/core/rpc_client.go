package core

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// RPCClient talks to platarium-cli serve over newline-delimited JSON-RPC 2.0.
type RPCClient struct {
	addr string
	mu   sync.Mutex
	id   int64
}

// NewRPCClient connects to Core RPC daemon at addr (e.g. 127.0.0.1:19500).
func NewRPCClient(addr string) (*RPCClient, error) {
	if addr == "" {
		addr = "127.0.0.1:19500"
	}
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return nil, fmt.Errorf("core rpc dial %s: %w", addr, err)
	}
	_ = conn.Close()
	return &RPCClient{addr: addr}, nil
}

func cliCommandToMethod(command string) string {
	return strings.ReplaceAll(command, "-", "_")
}

func cliArgsToParams(args []string) map[string]interface{} {
	params := make(map[string]interface{})
	for i := 1; i < len(args); i++ {
		if !strings.HasPrefix(args[i], "--") {
			continue
		}
		key := strings.TrimPrefix(args[i], "--")
		key = strings.ReplaceAll(key, "-", "_")
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
			params[key] = args[i+1]
			i++
		} else {
			params[key] = true
		}
	}
	return params
}

// Call executes a JSON-RPC method and returns the result as JSON string (same shape as CLI stdout).
func (c *RPCClient) Call(method string, params map[string]interface{}) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.id++
	reqID := c.id

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      reqID,
		"method":  method,
		"params":  params,
	}
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return "", err
	}

	conn, err := net.DialTimeout("tcp", c.addr, 5*time.Second)
	if err != nil {
		return "", fmt.Errorf("core rpc dial: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))

	if _, err := fmt.Fprintf(conn, "%s\n", reqBytes); err != nil {
		return "", err
	}

	scanner := bufio.NewScanner(conn)
	// Large mempool / L1 verify JSON can exceed the default 64KiB token limit.
	scanner.Buffer(make([]byte, 0, 1024*1024), 32*1024*1024)
	if !scanner.Scan() {
		return "", fmt.Errorf("core rpc: empty response")
	}
	line := scanner.Text()

	var resp struct {
		ID     json.RawMessage `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return "", fmt.Errorf("core rpc parse response: %w", err)
	}
	if resp.Error != nil {
		return "", fmt.Errorf("core rpc error %d: %s", resp.Error.Code, resp.Error.Message)
	}
	if len(resp.Result) == 0 {
		return "", nil
	}

	// Return compact JSON string (Execute callers expect JSON or CLI text).
	var asString string
	if err := json.Unmarshal(resp.Result, &asString); err == nil {
		return asString, nil
	}
	return string(resp.Result), nil
}

// ExecuteRPC maps platarium-cli argv to JSON-RPC and normalizes output for CLI-compatible callers.
func (c *RPCClient) ExecuteRPC(args []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("empty core rpc args")
	}
	method := cliCommandToMethod(args[0])
	params := cliArgsToParams(args)
	out, err := c.Call(method, params)
	if err != nil {
		return "", err
	}
	return normalizeRPCOutput(method, out)
}

func normalizeRPCOutput(method, out string) (string, error) {
	switch method {
	case "generate_mnemonic":
		var parsed struct {
			Mnemonic     string `json:"mnemonic"`
			Alphanumeric string `json:"alphanumeric"`
		}
		if err := json.Unmarshal([]byte(out), &parsed); err != nil {
			return out, nil
		}
		return fmt.Sprintf("Mnemonic: %s\nAlphanumeric: %s", parsed.Mnemonic, parsed.Alphanumeric), nil
	case "generate_keys":
		var parsed map[string]string
		if err := json.Unmarshal([]byte(out), &parsed); err != nil {
			return out, nil
		}
		return fmt.Sprintf("Public Key: %s\nPrivate Key: %s\nSignature Key: %s",
			parsed["publicKey"], parsed["privateKey"], parsed["signatureKey"]), nil
	case "verify_signature":
		var parsed struct {
			Verified bool `json:"verified"`
		}
		if err := json.Unmarshal([]byte(out), &parsed); err != nil {
			return out, nil
		}
		if parsed.Verified {
			return "Verified: true\nSignature is valid.", nil
		}
		return "Verified: false\nSignature is invalid.", nil
	case "sign_message":
		var parsed struct {
			Hash string `json:"hash"`
		}
		if err := json.Unmarshal([]byte(out), &parsed); err != nil {
			return out, nil
		}
		return fmt.Sprintf("Message Hash: %s\n%s", parsed.Hash, out), nil
	default:
		return out, nil
	}
}
