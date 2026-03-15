// Package rewards implements fee distribution: burn, treasury, L1, L2 (aligned with Core concepts).
package rewards

import (
	"strconv"
	"sync"
)

// Config holds reward distribution percentages (0-100). Sum should be 100.
type Config struct {
	BurnPct     int `json:"burnPct"`
	TreasuryPct int `json:"treasuryPct"`
	L1Pct       int `json:"l1Pct"`
	L2Pct       int `json:"l2Pct"`
}

// DefaultConfig: 0 burn, 20 treasury, 40 L1, 40 L2.
func DefaultConfig() Config {
	return Config{BurnPct: 0, TreasuryPct: 20, L1Pct: 40, L2Pct: 40}
}

// SplitResult is the result of splitting block fees.
type SplitResult struct {
	Burn     int64 `json:"burn"`
	Treasury int64 `json:"treasury"`
	L1Pool   int64 `json:"l1Pool"`
	L2Pool   int64 `json:"l2Pool"`
}

// SplitBlockFees splits totalFees by config percentages.
func (c Config) SplitBlockFees(totalFees int64) SplitResult {
	if totalFees <= 0 {
		return SplitResult{}
	}
	sum := c.BurnPct + c.TreasuryPct + c.L1Pct + c.L2Pct
	if sum <= 0 {
		sum = 100
	}
	burn := totalFees * int64(c.BurnPct) / int64(sum)
	treasury := totalFees * int64(c.TreasuryPct) / int64(sum)
	l1 := totalFees * int64(c.L1Pct) / int64(sum)
	l2 := totalFees * int64(c.L2Pct) / int64(sum)
	treasury += totalFees - burn - treasury - l1 - l2
	return SplitResult{Burn: burn, Treasury: treasury, L1Pool: l1, L2Pool: l2}
}

// Distributor tracks cumulative burn, treasury, and applies config.
type Distributor struct {
	mu            sync.RWMutex
	config        Config
	TotalBurned   int64 `json:"totalBurned"`
	TotalTreasury int64 `json:"totalTreasury"`
}

func NewDistributor() *Distributor {
	return &Distributor{config: DefaultConfig()}
}

func (d *Distributor) SetConfig(c Config) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.config = c
}

func (d *Distributor) GetConfig() Config {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.config
}

func (d *Distributor) ApplyBlock(totalFees int64) (l1Pool, l2Pool int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	sr := d.config.SplitBlockFees(totalFees)
	d.TotalBurned += sr.Burn
	d.TotalTreasury += sr.Treasury
	return sr.L1Pool, sr.L2Pool
}

func (d *Distributor) Totals() (burned, treasury int64) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.TotalBurned, d.TotalTreasury
}

// AddBurnTreasury adds burn and treasury from a block confirmed by another node (so all nodes show same totals).
func (d *Distributor) AddBurnTreasury(burn, treasury int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.TotalBurned += burn
	d.TotalTreasury += treasury
}

func ParseFee(feeStr string) int64 {
	n, _ := strconv.ParseInt(feeStr, 10, 64)
	if n < 0 {
		return 0
	}
	return n
}
