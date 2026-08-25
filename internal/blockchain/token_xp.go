package blockchain

import "strings"

const TokenXP = "Token:XP"

// CanonicalAsset normalizes PLP / Token:XP / XP to Core canonical form.
func CanonicalAsset(asset string) string {
	a := strings.TrimSpace(asset)
	if a == "" || strings.EqualFold(a, "PLP") {
		return "PLP"
	}
	if strings.HasPrefix(strings.ToLower(a), "token:") {
		sym := strings.TrimSpace(a[len("Token:"):])
		if strings.EqualFold(sym, "XP") {
			return TokenXP
		}
		return "Token:" + sym
	}
	if strings.EqualFold(a, "XP") {
		return TokenXP
	}
	return "Token:" + a
}

// IsNonTransferableAsset is true for Token:XP — accumulate-only, no send/escrow.
func IsNonTransferableAsset(asset string) bool {
	return CanonicalAsset(asset) == TokenXP
}

// TokenXPFromMap reads Token:XP from a Core tokens map.
func TokenXPFromMap(tokens map[string]string) string {
	if tokens == nil {
		return "0"
	}
	if v, ok := tokens[TokenXP]; ok && v != "" {
		return v
	}
	if v, ok := tokens["XP"]; ok && v != "" {
		return v
	}
	return "0"
}
