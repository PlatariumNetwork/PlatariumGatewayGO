package core

import (
	"encoding/json"
	"fmt"
)

// CoreConsistencyReport is the Rocks-internal check_consistency result from Core (TASK-055).
type CoreConsistencyReport struct {
	OK     bool     `json:"ok"`
	Head   uint64   `json:"head"`
	Errors []string `json:"errors,omitempty"`
}

// CheckConsistency calls Core check_consistency (read-only; does not mutate ledgers).
func (c *RocksStoreClient) CheckConsistency() (*CoreConsistencyReport, error) {
	if c == nil || !c.Enabled() {
		return nil, fmt.Errorf("rocks store unavailable")
	}
	out, err := c.rustCore.Execute([]string{"check-consistency", "--db-path", c.dbPath})
	if err != nil {
		return nil, err
	}
	var res CoreConsistencyReport
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return nil, fmt.Errorf("parse check-consistency: %w", err)
	}
	return &res, nil
}
