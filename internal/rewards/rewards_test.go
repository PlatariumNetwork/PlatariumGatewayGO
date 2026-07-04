package rewards

import "testing"

func TestDefaultConfigSumsTo100(t *testing.T) {
	c := DefaultConfig()
	sum := c.BurnPct + c.TreasuryPct + c.L1Pct + c.L2Pct
	if sum != 100 {
		t.Fatalf("default config sum = %d, want 100", sum)
	}
	if c.BurnPct <= 0 {
		t.Fatalf("default burn = %d, want > 0", c.BurnPct)
	}
}

func TestSplitBlockFeesBurn(t *testing.T) {
	cfg := DefaultConfig()
	sr := cfg.SplitBlockFees(10_000)
	if sr.Burn != 1000 {
		t.Fatalf("burn = %d, want 1000", sr.Burn)
	}
	if sr.Treasury != 1800 {
		t.Fatalf("treasury = %d, want 1800", sr.Treasury)
	}
	if sr.L1Pool != 3600 || sr.L2Pool != 3600 {
		t.Fatalf("l1=%d l2=%d, want 3600 each", sr.L1Pool, sr.L2Pool)
	}
	sum := sr.Burn + sr.Treasury + sr.L1Pool + sr.L2Pool
	if sum != 10_000 {
		t.Fatalf("split sum = %d, want 10000", sum)
	}
}
