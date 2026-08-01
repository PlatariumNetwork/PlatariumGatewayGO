package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

var (
	daemonMu     sync.Mutex
	daemonCmd    *exec.Cmd
	daemonOwned  bool
	daemonListen string
)

// EnsureCoreDaemon starts platarium-cli serve if not already reachable.
// Safe to call multiple times; only one owned child process is tracked.
func EnsureCoreDaemon(cliPath, listenAddr string) error {
	daemonMu.Lock()
	defer daemonMu.Unlock()

	if listenAddr == "" {
		listenAddr = DefaultCoreRPCAddr()
	}
	client, err := NewRPCClient(listenAddr)
	if err == nil {
		if pingErr := client.Ping(); pingErr == nil {
			_ = client.Close()
			daemonListen = listenAddr
			return nil
		}
		_ = client.Close()
	}

	if cliPath == "" {
		var findErr error
		cliPath, findErr = resolveCLIPath()
		if findErr != nil {
			return findErr
		}
	}

	network, dialAddr := ParseRPCAddr(listenAddr)
	serveArg := listenAddr
	if network == "unix" {
		serveArg = "unix:" + dialAddr
		_ = os.Remove(dialAddr)
		if dir := filepath.Dir(dialAddr); dir != "" && dir != "." {
			_ = os.MkdirAll(dir, 0o755)
		}
	}

	cmd := exec.Command(cliPath, "serve", "--listen", serveArg)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start core rpc daemon: %w", err)
	}
	daemonCmd = cmd
	daemonOwned = true
	daemonListen = listenAddr

	deadline := time.Now().Add(15 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		c, err := NewRPCClient(listenAddr)
		if err == nil {
			if pingErr := c.Ping(); pingErr == nil {
				_ = c.Close()
				return nil
			} else {
				lastErr = pingErr
				_ = c.Close()
			}
		} else {
			lastErr = err
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	_, _ = cmd.Process.Wait()
	daemonCmd = nil
	daemonOwned = false
	return fmt.Errorf("core rpc daemon did not become ready: %v", lastErr)
}

// StopOwnedCoreDaemon stops a daemon started by EnsureCoreDaemon (not external ones).
func StopOwnedCoreDaemon() {
	daemonMu.Lock()
	defer daemonMu.Unlock()
	if !daemonOwned || daemonCmd == nil || daemonCmd.Process == nil {
		return
	}
	_ = daemonCmd.Process.Kill()
	_, _ = daemonCmd.Process.Wait()
	daemonCmd = nil
	daemonOwned = false
}

func resolveCLIPath() (string, error) {
	if path := os.Getenv("PLATARIUM_CLI_PATH"); path != "" {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	binaryPath := filepath.Join("..", "PlatariumCore", "target", "release", "platarium-cli")
	if abs, err := filepath.Abs(binaryPath); err == nil {
		if _, err := os.Stat(abs); err == nil {
			return abs, nil
		}
	}
	if _, err := os.Stat(binaryPath); err == nil {
		return binaryPath, nil
	}
	if path, err := exec.LookPath("platarium-cli"); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("platarium-cli binary not found. Set PLATARIUM_CLI_PATH or build: cd PlatariumCore && cargo build --release")
}
