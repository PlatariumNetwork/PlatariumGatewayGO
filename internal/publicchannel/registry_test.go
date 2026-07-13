package publicchannel

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegisterAndGet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "channels.json")
	reg, err := NewRegistry(path)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	rec, err := reg.Register("PxChannel1", "PxOwner1", "Network")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if rec.Address != "PxChannel1" || rec.OwnerAddress != "PxOwner1" || rec.Name != "Network" {
		t.Fatalf("unexpected record: %+v", rec)
	}
	if rec.CreatedAt <= 0 {
		t.Fatalf("expected createdAt")
	}

	got, ok := reg.Get("PxChannel1")
	if !ok || got.OwnerAddress != "PxOwner1" {
		t.Fatalf("Get: %+v ok=%v", got, ok)
	}

	_, err = reg.Register("PxChannel1", "PxOther", "")
	if err == nil {
		t.Fatalf("expected conflict for different owner")
	}

	_, err = reg.Register("PxChannel1", "PxOwner1", "Renamed")
	if err != nil {
		t.Fatalf("same owner update: %v", err)
	}
	got, _ = reg.Get("PxChannel1")
	if got.Name != "Renamed" {
		t.Fatalf("name not updated: %+v", got)
	}

	reg2, err := NewRegistry(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got, ok = reg2.Get("PxChannel1")
	if !ok || got.OwnerAddress != "PxOwner1" {
		t.Fatalf("persisted Get: %+v ok=%v", got, ok)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file missing: %v", err)
	}
}
