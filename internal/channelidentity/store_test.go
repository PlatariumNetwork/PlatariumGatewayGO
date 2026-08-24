package channelidentity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPutAndGet(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "ids.json"))
	if err != nil {
		t.Fatal(err)
	}
	rec := store.Put(Record{
		Address:      "PxAABBCCDD",
		OwnerAddress: "Px11223344",
		Name:         "News",
	})
	if rec.Kind != "channel" {
		t.Fatalf("kind=%s", rec.Kind)
	}
	got, ok := store.Get("pxaabbccdd")
	if !ok || got.Name != "News" || got.OwnerAddress != "Px11223344" {
		t.Fatalf("got=%+v ok=%v", got, ok)
	}
	if _, err := os.Stat(filepath.Join(dir, "ids.json")); err != nil {
		t.Fatal(err)
	}
}
