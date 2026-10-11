package txpool

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPutListSurvivesReopen(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "txpool")
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put("abc123", []byte(`{"hash":"abc123"}`)); err != nil {
		t.Fatal(err)
	}
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := again.List()
	if err != nil {
		t.Fatal(err)
	}
	if string(got["abc123"]) != `{"hash":"abc123"}` {
		t.Fatalf("got %#v", got)
	}
	if err := again.Delete("abc123"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "abc123.json")); !os.IsNotExist(err) {
		t.Fatalf("file still present: %v", err)
	}
}
