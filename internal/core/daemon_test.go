package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveUnixSocketIfSafe_StaleOK(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "core.sock")
	pidPath := sock + ".pid"
	if err := os.WriteFile(sock, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	// Dead pid
	if err := os.WriteFile(pidPath, []byte("99999999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := removeUnixSocketIfSafe(sock, pidPath); err != nil {
		t.Fatalf("stale socket should be removable: %v", err)
	}
	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Fatal("socket should be gone")
	}
}

func TestRemoveUnixSocketIfSafe_LivePIDRefused(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "core.sock")
	pidPath := sock + ".pid"
	if err := os.WriteFile(sock, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	live := os.Getpid()
	if err := writeDaemonPID(pidPath, live); err != nil {
		t.Fatal(err)
	}
	err := removeUnixSocketIfSafe(sock, pidPath)
	if err == nil {
		t.Fatal("expected refuse to remove foreign live socket")
	}
	if _, statErr := os.Stat(sock); statErr != nil {
		t.Fatal("socket must remain when pid is alive")
	}
}

func TestAcquireDaemonLockExclusive(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "core.lock")

	daemonMu.Lock()
	releaseDaemonLock()
	daemonMu.Unlock()

	if err := acquireDaemonLock(lockPath); err != nil {
		t.Fatal(err)
	}
	defer func() {
		daemonMu.Lock()
		releaseDaemonLock()
		daemonMu.Unlock()
	}()

	// Second exclusive open+flock must fail while first holds it.
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := flockExclusiveNB(f); err == nil {
		t.Fatal("second flock should fail while lock held")
	}
}

func TestDaemonSidecarPathsUnix(t *testing.T) {
	lock, pid, log := daemonSidecarPaths("unix", "/tmp/platarium-core.sock")
	if lock != "/tmp/platarium-core.sock.lock" || pid != "/tmp/platarium-core.sock.pid" || log != "/tmp/platarium-core.sock.log" {
		t.Fatalf("%s %s %s", lock, pid, log)
	}
}
