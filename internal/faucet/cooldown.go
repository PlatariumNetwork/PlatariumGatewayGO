package faucet

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const defaultCooldown = 24 * time.Hour

// CooldownStore tracks last faucet claim per address (persisted JSON).
type CooldownStore struct {
	path      string
	cooldown  time.Duration
	mu        sync.Mutex
	claims    map[string]int64 // normalized address -> unix seconds
}

type filePayload struct {
	Claims map[string]int64 `json:"claims"`
}

// NewCooldownStore loads or creates a cooldown file.
func NewCooldownStore(path string, cooldown time.Duration) (*CooldownStore, error) {
	if cooldown <= 0 {
		cooldown = defaultCooldown
	}
	if path == "" {
		path = "data/faucet-cooldown.json"
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create faucet cooldown dir: %w", err)
		}
	}
	s := &CooldownStore{
		path:     path,
		cooldown: cooldown,
		claims:   make(map[string]int64),
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func normalizeAddress(address string) string {
	return strings.TrimSpace(address)
}

func (s *CooldownStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read faucet cooldown file: %w", err)
	}
	var payload filePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return fmt.Errorf("parse faucet cooldown file: %w", err)
	}
	if payload.Claims != nil {
		s.claims = payload.Claims
	}
	return nil
}

func (s *CooldownStore) persistLocked() error {
	payload := filePayload{Claims: s.claims}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Cooldown returns configured claim interval.
func (s *CooldownStore) Cooldown() time.Duration {
	return s.cooldown
}
func (s *CooldownStore) Remaining(address string, now time.Time) time.Duration {
	key := normalizeAddress(address)
	if key == "" {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	last, ok := s.claims[key]
	if !ok || last <= 0 {
		return 0
	}
	next := time.Unix(last, 0).Add(s.cooldown)
	if !now.Before(next) {
		return 0
	}
	return next.Sub(now)
}

// RecordClaim stores claim time for address.
func (s *CooldownStore) RecordClaim(address string, now time.Time) error {
	key := normalizeAddress(address)
	if key == "" {
		return fmt.Errorf("address required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.claims[key] = now.Unix()
	return s.persistLocked()
}

// FormatWait formats duration as "X hours Y minutes Z seconds" (English for API; scan localizes).
func FormatWait(d time.Duration) (hours, minutes, seconds int, label string) {
	if d < 0 {
		d = 0
	}
	total := int(d.Seconds())
	hours = total / 3600
	minutes = (total % 3600) / 60
	seconds = total % 60
	parts := make([]string, 0, 3)
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%d hour%s", hours, plural(hours)))
	}
	if minutes > 0 {
		parts = append(parts, fmt.Sprintf("%d minute%s", minutes, plural(minutes)))
	}
	if seconds > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%d second%s", seconds, plural(seconds)))
	}
	label = strings.Join(parts, " ")
	return hours, minutes, seconds, label
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
