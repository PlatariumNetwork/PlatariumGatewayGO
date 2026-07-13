package publicchannel

import (
	"path/filepath"
	"testing"
)

func TestPostStoreAppendAndList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "posts.json")
	store, err := NewPostStore(path)
	if err != nil {
		t.Fatalf("NewPostStore: %v", err)
	}

	p1, err := store.Append("PxChannel1", "PxOwner1", "Hello", 100)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if p1.ID == "" || p1.From != "PxOwner1" || p1.Text != "Hello" {
		t.Fatalf("unexpected post: %+v", p1)
	}

	p2, err := store.Append("PxChannel1", "PxOwner1", "World", 200)
	if err != nil {
		t.Fatalf("Append 2: %v", err)
	}

	all := store.List("PxChannel1", 0, 100)
	if len(all) != 2 || all[0].ID != p1.ID || all[1].ID != p2.ID {
		t.Fatalf("List all: %+v", all)
	}

	since := store.List("PxChannel1", 150, 100)
	if len(since) != 1 || since[0].ID != p2.ID {
		t.Fatalf("List since: %+v", since)
	}

	store2, err := NewPostStore(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	reloaded := store2.List("PxChannel1", 0, 100)
	if len(reloaded) != 2 {
		t.Fatalf("persisted list: %+v", reloaded)
	}
}

func TestPostStoreValidation(t *testing.T) {
	store, err := NewPostStore(t.TempDir() + "/posts.json")
	if err != nil {
		t.Fatalf("NewPostStore: %v", err)
	}
	if _, err := store.Append("", "PxA", "x", 1); err == nil {
		t.Fatal("expected channel required error")
	}
	if _, err := store.Append("PxCh", "", "x", 1); err == nil {
		t.Fatal("expected from required error")
	}
	if _, err := store.Append("PxCh", "PxA", "", 1); err == nil {
		t.Fatal("expected text required error")
	}
}
