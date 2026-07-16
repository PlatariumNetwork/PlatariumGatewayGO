package publicchannel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Record is a registered public messenger channel (feed address + owner wallet).
type Record struct {
	Address      string `json:"address"`
	OwnerAddress string `json:"ownerAddress"`
	Name         string `json:"name,omitempty"`
	CreatedAt    int64  `json:"createdAt"`
}

type filePayload struct {
	Channels map[string]Record `json:"channels"`
}

// Registry persists public channel metadata on the gateway node.
type Registry struct {
	path     string
	mu       sync.RWMutex
	channels map[string]Record
}

// NewRegistry loads or creates a public-channel registry file.
func NewRegistry(path string) (*Registry, error) {
	if path == "" {
		path = "data/public-channels.json"
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create public channel registry dir: %w", err)
		}
	}
	r := &Registry{
		path:     path,
		channels: make(map[string]Record),
	}
	if err := r.load(); err != nil {
		return nil, err
	}
	return r, nil
}

func normalizeAddress(address string) string {
	return strings.TrimSpace(address)
}

func (r *Registry) load() error {
	data, err := os.ReadFile(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read public channel registry: %w", err)
	}
	var payload filePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return fmt.Errorf("parse public channel registry: %w", err)
	}
	if payload.Channels != nil {
		r.channels = payload.Channels
	}
	return nil
}

func (r *Registry) persistLocked() error {
	payload := filePayload{Channels: r.channels}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}

// Register stores a public channel. Same address may only be owned by the same owner.
func (r *Registry) Register(address, ownerAddress, name string) (Record, error) {
	addr := normalizeAddress(address)
	owner := normalizeAddress(ownerAddress)
	if addr == "" || owner == "" {
		return Record{}, fmt.Errorf("address and ownerAddress are required")
	}
	if !strings.HasPrefix(strings.ToLower(addr), "px") || !strings.HasPrefix(strings.ToLower(owner), "px") {
		return Record{}, fmt.Errorf("address and ownerAddress must be Platarium wallets")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, ok := r.channels[addr]; ok {
		if !strings.EqualFold(existing.OwnerAddress, owner) {
			return Record{}, fmt.Errorf("channel already registered by another owner")
		}
		if name = strings.TrimSpace(name); name != "" && existing.Name != name {
			existing.Name = name
			r.channels[addr] = existing
			if err := r.persistLocked(); err != nil {
				return Record{}, err
			}
		}
		return existing, nil
	}

	rec := Record{
		Address:      addr,
		OwnerAddress: owner,
		Name:         strings.TrimSpace(name),
		CreatedAt:    time.Now().Unix(),
	}
	r.channels[addr] = rec
	if err := r.persistLocked(); err != nil {
		return Record{}, err
	}
	return rec, nil
}

// Get returns a channel by address.
func (r *Registry) Get(address string) (Record, bool) {
	addr := normalizeAddress(address)
	r.mu.RLock()
	defer r.mu.RUnlock()
	rec, ok := r.channels[addr]
	return rec, ok
}

// List returns all registered channels.
func (r *Registry) List() []Record {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Record, 0, len(r.channels))
	for _, rec := range r.channels {
		out = append(out, rec)
	}
	return out
}

// ListByOwner returns channels whose ownerAddress matches (case-insensitive).
func (r *Registry) ListByOwner(ownerAddress string) []Record {
	owner := strings.TrimSpace(ownerAddress)
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Record, 0)
	for _, rec := range r.channels {
		if strings.EqualFold(rec.OwnerAddress, owner) {
			out = append(out, rec)
		}
	}
	return out
}
