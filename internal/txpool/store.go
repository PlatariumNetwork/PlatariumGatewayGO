package txpool

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Store is the durable transaction pool. It lives in its own directory beside
// the chain RocksDB and is not cleared when the gateway process restarts.
type Store struct {
	dir string
}

// Open creates the pool directory. dir is the folder, not a single file.
func Open(dir string) (*Store, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, fmt.Errorf("tx pool directory required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create tx pool dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Dir returns the pool directory.
func (s *Store) Dir() string {
	if s == nil {
		return ""
	}
	return s.dir
}

// Put writes one transaction atomically. A crash keeps the previous file.
func (s *Store) Put(hash string, raw []byte) error {
	if s == nil {
		return nil
	}
	name, ok := fileName(hash)
	if !ok {
		return fmt.Errorf("unsafe tx hash")
	}
	if !json.Valid(raw) {
		return fmt.Errorf("tx pool value is not json")
	}
	final := filepath.Join(s.dir, name)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return fmt.Errorf("write tx pool temp: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename tx pool file: %w", err)
	}
	return nil
}

// Delete removes one pooled transaction. Missing files are not an error.
func (s *Store) Delete(hash string) error {
	if s == nil {
		return nil
	}
	name, ok := fileName(hash)
	if !ok {
		return nil
	}
	err := os.Remove(filepath.Join(s.dir, name))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// List reads every pooled transaction. Keys are hashes.
func (s *Store) List() (map[string][]byte, error) {
	out := make(map[string][]byte)
	if s == nil {
		return out, nil
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		hash := strings.TrimSuffix(entry.Name(), ".json")
		raw, err := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		out[hash] = raw
	}
	return out, nil
}

func fileName(hash string) (string, bool) {
	if hash == "" || len(hash) > 128 {
		return "", false
	}
	for _, c := range hash {
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		default:
			return "", false
		}
	}
	return hash + ".json", true
}
