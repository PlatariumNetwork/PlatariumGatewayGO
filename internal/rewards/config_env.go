package rewards

import (
	"os"
	"strconv"
)

// DefaultConfig: 10% burn, 18% treasury, 36% L1, 36% L2 (testnet protocol distribution).
func DefaultConfig() Config {
	return Config{BurnPct: 10, TreasuryPct: 18, L1Pct: 36, L2Pct: 36}
}

// ConfigFromEnv reads PLATARIUM_BURN_PCT, PLATARIUM_TREASURY_PCT, PLATARIUM_L1_PCT, PLATARIUM_L2_PCT.
// Unset variables use DefaultConfig values.
func ConfigFromEnv() Config {
	def := DefaultConfig()
	return Config{
		BurnPct:     envInt("PLATARIUM_BURN_PCT", def.BurnPct),
		TreasuryPct: envInt("PLATARIUM_TREASURY_PCT", def.TreasuryPct),
		L1Pct:       envInt("PLATARIUM_L1_PCT", def.L1Pct),
		L2Pct:       envInt("PLATARIUM_L2_PCT", def.L2Pct),
	}
}

func envInt(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return fallback
	}
	return n
}
