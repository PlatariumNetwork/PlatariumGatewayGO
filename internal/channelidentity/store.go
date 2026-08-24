package channelidentity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Record struct {
	Address      string `json:"address"`
	OwnerAddress string `json:"ownerAddress"`
	Name         string `json:"name,omitempty"`
	Kind         string `json:"kind"`
	CreatedAt    int64  `json:"createdAt"`
}

type Store struct {
	mu   sync.RWMutex
	path string
	byID map[string]Record
}

func NewStore(path string) (*Store, error) {
	s := &Store{path: path, byID: map[string]Record{}}
	if err := s.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return s, nil
}

func (s *Store) Get(address string) (Record, bool) {
	id := canonPx(address)
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.byID[id]
	return rec, ok
}

func (s *Store) Put(rec Record) Record {
	rec.Address = canonPx(rec.Address)
	rec.OwnerAddress = canonPx(rec.OwnerAddress)
	if rec.Kind == "" {
		rec.Kind = "channel"
	}
	if rec.CreatedAt == 0 {
		rec.CreatedAt = time.Now().Unix()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.byID[rec.Address]
	if ok && existing.CreatedAt > 0 {
		rec.CreatedAt = existing.CreatedAt
	}
	s.byID[rec.Address] = rec
	_ = s.saveLocked()
	return rec
}

func (s *Store) load() error {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	var payload struct {
		Channels []Record `json:"channels"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID = map[string]Record{}
	for _, rec := range payload.Channels {
		id := canonPx(rec.Address)
		if id == "" {
			continue
		}
		rec.Address = id
		rec.OwnerAddress = canonPx(rec.OwnerAddress)
		if rec.Kind == "" {
			rec.Kind = "channel"
		}
		s.byID[id] = rec
	}
	return nil
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil && filepath.Dir(s.path) != "." {
		return err
	}
	list := make([]Record, 0, len(s.byID))
	for _, rec := range s.byID {
		list = append(list, rec)
	}
	raw, err := json.MarshalIndent(struct {
		Channels []Record `json:"channels"`
	}{Channels: list}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, raw, 0o644)
}

func CanonPx(addr string) string {
	v := strings.TrimSpace(addr)
	if len(v) < 4 {
		return ""
	}
	if strings.HasPrefix(v, "Px") || strings.HasPrefix(v, "px") || strings.HasPrefix(v, "PX") {
		hex := strings.ToLower(v[2:])
		for i := 0; i < len(hex); i++ {
			c := hex[i]
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				return ""
			}
		}
		if len(hex) < 8 {
			return ""
		}
		return "Px" + hex
	}
	return ""
}

func canonPx(addr string) string {
	return CanonPx(addr)
}
