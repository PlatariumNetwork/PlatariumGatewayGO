package core

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

// RPCClient talks to platarium-cli serve over newline-delimited JSON-RPC 2.0.
// Keeps a persistent connection (TCP or Unix) and reconnects on failure.
type RPCClient struct {
	addr    string // dial target: host:port or unix path
	network string // "tcp" or "unix"
	mu      sync.Mutex
	id      int64
	conn    net.Conn
	reader  *bufio.Reader
}

// ParseRPCAddr returns network ("tcp"|"unix") and dial address.
// Accepts: "127.0.0.1:19500", "tcp://127.0.0.1:19500", "unix:/tmp/x.sock", "/tmp/x.sock".
func ParseRPCAddr(addr string) (network, dialAddr string) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "tcp", "127.0.0.1:19500"
	}
	if strings.HasPrefix(addr, "unix:") {
		return "unix", strings.TrimPrefix(addr, "unix:")
	}
	if strings.HasPrefix(addr, "tcp://") {
		return "tcp", strings.TrimPrefix(addr, "tcp://")
	}
	if strings.HasPrefix(addr, "/") || strings.HasPrefix(addr, "./") {
		return "unix", addr
	}
	return "tcp", addr
}

// NewRPCClient connects to Core RPC daemon at addr (TCP host:port or unix:/path).
func NewRPCClient(addr string) (*RPCClient, error) {
	network, dialAddr := ParseRPCAddr(addr)
	c := &RPCClient{addr: dialAddr, network: network}
	if err := c.connectLocked(); err != nil {
		// Allow construction even if daemon is not up yet (auto-start will retry).
		// First Call will reconnect.
		_ = err
	}
	return c, nil
}

func (c *RPCClient) connectLocked() error {
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
		c.reader = nil
	}
	conn, err := net.DialTimeout(c.network, c.addr, 3*time.Second)
	if err != nil {
		return fmt.Errorf("core rpc dial %s %s: %w", c.network, c.addr, err)
	}
	c.conn = conn
	c.reader = bufio.NewReaderSize(conn, 1024*1024)
	return nil
}

func (c *RPCClient) ensureConnLocked() error {
	if c.conn != nil {
		return nil
	}
	return c.connectLocked()
}

// Ping checks the daemon is reachable.
func (c *RPCClient) Ping() error {
	_, err := c.Call("ping", map[string]interface{}{})
	return err
}

// Close releases the persistent connection.
func (c *RPCClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		c.reader = nil
		return err
	}
	return nil
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

	out, err := c.roundTripLocked(reqBytes)
	if err != nil {
		// One reconnect retry.
		if reconnErr := c.connectLocked(); reconnErr != nil {
			return "", err
		}
		out, err = c.roundTripLocked(reqBytes)
		if err != nil {
			return "", err
		}
	}
	return out, nil
}

func (c *RPCClient) roundTripLocked(reqBytes []byte) (string, error) {
	if err := c.ensureConnLocked(); err != nil {
		return "", err
	}
	_ = c.conn.SetDeadline(time.Now().Add(60 * time.Second))
	if _, err := fmt.Fprintf(c.conn, "%s\n", reqBytes); err != nil {
		_ = c.conn.Close()
		c.conn = nil
		c.reader = nil
		return "", err
	}

	line, err := c.reader.ReadString('\n')
	if err != nil {
		_ = c.conn.Close()
		c.conn = nil
		c.reader = nil
		return "", fmt.Errorf("core rpc read: %w", err)
	}
	line = strings.TrimRight(line, "\r\n")

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

// DefaultCoreRPCAddr returns listen/dial address for the Core daemon.
func DefaultCoreRPCAddr() string {
	if v := strings.TrimSpace(os.Getenv("PLATARIUM_CORE_RPC_ADDR")); v != "" {
		return v
	}
	// Prefer Unix socket on local nodes (lower latency than TCP loopback).
	if v := strings.TrimSpace(os.Getenv("PLATARIUM_CORE_RPC_SOCK")); v != "" {
		return "unix:" + v
	}
	return "unix:/tmp/platarium-core.sock"
}
