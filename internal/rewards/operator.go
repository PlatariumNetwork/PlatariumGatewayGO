package rewards

import (
	"os"
	"strings"
)

// OperatorWalletFromEnv reads PLATARIUM_OPERATOR_WALLET (Px… address credited for node fees + XP).
func OperatorWalletFromEnv() string {
	return normalizeOperatorWallet(os.Getenv("PLATARIUM_OPERATOR_WALLET"))
}

// ContributorsAPIURLFromEnv is the Scan base URL for node reward reporting
// (e.g. https://platarium.network). Empty disables XP reporting.
func ContributorsAPIURLFromEnv() string {
	return strings.TrimRight(strings.TrimSpace(os.Getenv("PLATARIUM_CONTRIBUTORS_API_URL")), "/")
}

// NodeRewardsSecretFromEnv authenticates POST /api/contributors/node-rewards on Scan.
func NodeRewardsSecretFromEnv() string {
	return strings.TrimSpace(os.Getenv("PLATARIUM_NODE_REWARDS_SECRET"))
}

func normalizeOperatorWallet(addr string) string {
	v := strings.TrimSpace(addr)
	if len(v) < 3 {
		return ""
	}
	if (strings.HasPrefix(v, "Px") || strings.HasPrefix(v, "px")) && len(v) > 2 {
		hex := strings.ToLower(v[2:])
		if isHex(hex) {
			return "Px" + hex
		}
	}
	return ""
}

func isHex(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
