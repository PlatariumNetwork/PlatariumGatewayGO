package websocket

import (
	"strings"
)

// normalizePlatariumAddress canonicalizes Px wallet addresses (px vs Px, hex case).
func normalizePlatariumAddress(addr string) string {
	v := strings.TrimSpace(addr)
	if len(v) < 3 {
		return v
	}
	if (strings.HasPrefix(v, "Px") || strings.HasPrefix(v, "px")) && len(v) > 2 {
		hex := v[2:]
		lower := strings.ToLower(hex)
		if isHexString(lower) {
			return "Px" + lower
		}
	}
	return v
}

func isHexString(s string) bool {
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
