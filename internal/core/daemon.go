package core

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// EnsureCoreRPCAuthEnv sets a shared RPC token if neither token nor insecure mode is configured.
// Must run before EnsureCoreDaemon so the child Core inherits the same secret.
func EnsureCoreRPCAuthEnv() {
	if strings.TrimSpace(os.Getenv("PLATARIUM_CORE_RPC_INSECURE")) != "" {
		return
	}
	if strings.TrimSpace(os.Getenv("PLATARIUM_CORE_RPC_TOKEN")) != "" {
		return
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		_ = os.Setenv("PLATARIUM_CORE_RPC_INSECURE", "1")
		return
	}
	_ = os.Setenv("PLATARIUM_CORE_RPC_TOKEN", hex.EncodeToString(b))
}

var (
	daemonMu       sync.Mutex
	daemonCmd      *exec.Cmd
	daemonOwned    bool
	daemonListen   string
	daemonLockFile *os.File
	daemonLogFile  *os.File
)

// EnsureCoreDaemon starts platarium-cli serve if not already reachable.
// Safe to call multiple times; only one owned child process is tracked.
//
// L2: uses flock + pid file so two Gateways cannot race on the same unix socket;
// does not remove a live foreign socket; Core stdout/stderr go to a dedicated log file.
func EnsureCoreDaemon(cliPath, listenAddr string) error {
	EnsureCoreRPCAuthEnv()
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

	lockPath, pidPath, logPath := daemonSidecarPaths(network, dialAddr)
	if err := acquireDaemonLock(lockPath); err != nil {
		// Another Gateway holds the lock — if Core came up meanwhile, succeed.
		if c, e := NewRPCClient(listenAddr); e == nil {
			if pingErr := c.Ping(); pingErr == nil {
				_ = c.Close()
				daemonListen = listenAddr
				return nil
			}
			_ = c.Close()
		}
		return err
	}

	if network == "unix" {
		serveArg = "unix:" + dialAddr
		if dir := filepath.Dir(dialAddr); dir != "" && dir != "." {
			_ = os.MkdirAll(dir, 0o755)
		}
		if err := removeUnixSocketIfSafe(dialAddr, pidPath); err != nil {
			releaseDaemonLock()
			return err
		}
	}

	logFile, err := openDaemonLog(logPath)
	if err != nil {
		releaseDaemonLock()
		return err
	}

	cmd := exec.Command(cliPath, "serve", "--listen", serveArg)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		daemonLogFile = nil
		releaseDaemonLock()
		return fmt.Errorf("start core rpc daemon: %w", err)
	}
	daemonCmd = cmd
	daemonOwned = true
	daemonListen = listenAddr
	daemonLogFile = logFile
	_ = writeDaemonPID(pidPath, cmd.Process.Pid)

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
	_ = os.Remove(pidPath)
	if daemonLogFile != nil {
		_ = daemonLogFile.Close()
		daemonLogFile = nil
	}
	releaseDaemonLock()
	return fmt.Errorf("core rpc daemon did not become ready: %v", lastErr)
}

// StopOwnedCoreDaemon stops a daemon started by EnsureCoreDaemon (not external ones).
func StopOwnedCoreDaemon() {
	daemonMu.Lock()
	defer daemonMu.Unlock()
	if !daemonOwned || daemonCmd == nil || daemonCmd.Process == nil {
		return
	}
	pid := daemonCmd.Process.Pid
	_ = daemonCmd.Process.Kill()
	_, _ = daemonCmd.Process.Wait()
	daemonCmd = nil
	daemonOwned = false
	if daemonListen != "" {
		network, dialAddr := ParseRPCAddr(daemonListen)
		_, pidPath, _ := daemonSidecarPaths(network, dialAddr)
		if cur, err := readDaemonPID(pidPath); err == nil && cur == pid {
			_ = os.Remove(pidPath)
		}
	}
	if daemonLogFile != nil {
		_ = daemonLogFile.Close()
		daemonLogFile = nil
	}
	releaseDaemonLock()
}

func daemonSidecarPaths(network, dialAddr string) (lockPath, pidPath, logPath string) {
	base := dialAddr
	if network != "unix" {
		base = filepath.Join("data", "platarium-core-"+sanitizeAddr(dialAddr))
		_ = os.MkdirAll("data", 0o755)
	} else if dir := filepath.Dir(dialAddr); dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0o755)
	}
	return base + ".lock", base + ".pid", base + ".log"
}

func sanitizeAddr(addr string) string {
	r := strings.NewReplacer(":", "_", "/", "_", ".", "_")
	return r.Replace(addr)
}

func acquireDaemonLock(lockPath string) error {
	if daemonLockFile != nil {
		return nil
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("core daemon lock open: %w", err)
	}
	if err := flockExclusiveNB(f); err != nil {
		_ = f.Close()
		return fmt.Errorf("core daemon lock held by another process (%s): %w", lockPath, err)
	}
	daemonLockFile = f
	return nil
}

func flockExclusiveNB(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

func releaseDaemonLock() {
	if daemonLockFile == nil {
		return
	}
	_ = syscall.Flock(int(daemonLockFile.Fd()), syscall.LOCK_UN)
	_ = daemonLockFile.Close()
	daemonLockFile = nil
}

func openDaemonLog(logPath string) (*os.File, error) {
	if dir := filepath.Dir(logPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("core log dir: %w", err)
		}
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("core log open %s: %w", logPath, err)
	}
	return f, nil
}

func writeDaemonPID(pidPath string, pid int) error {
	return os.WriteFile(pidPath, []byte(strconv.Itoa(pid)+"\n"), 0o600)
}

func readDaemonPID(pidPath string) (int, error) {
	b, err := os.ReadFile(pidPath)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(b)))
}

// processAlive reports whether pid appears to exist (signal 0).
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

// removeUnixSocketIfSafe removes dialAddr only when it is absent, or stale
// (no live pid from sidecar, or pid file points at a dead process). Never
// unlinks a socket owned by a still-running Core (L2).
func removeUnixSocketIfSafe(dialAddr, pidPath string) error {
	if _, err := os.Stat(dialAddr); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}

	if pid, err := readDaemonPID(pidPath); err == nil && processAlive(pid) {
		return fmt.Errorf(
			"unix socket %s exists and pid %d from %s is alive (refusing to remove foreign Core socket)",
			dialAddr, pid, pidPath,
		)
	}
	// Stale socket / dead pid — safe to replace.
	if err := os.Remove(dialAddr); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale unix socket %s: %w", dialAddr, err)
	}
	_ = os.Remove(pidPath)
	return nil
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
