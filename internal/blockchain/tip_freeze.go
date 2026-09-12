package blockchain

// CanonicalHeadNumber returns the tip height that may be served as canonical (#57).
// When rocksOK (Rocks is SoT), explorer/chain.json tip must never lead Rocks head.
func CanonicalHeadNumber(memHead, rocksHead int64, rocksOK bool) int64 {
	if !rocksOK {
		return memHead
	}
	return rocksHead
}

// FreezeBlockHistory drops explorer blocks ahead of Rocks gateway head when Rocks is SoT (#57).
// rocksGatewayHead < 0 means Rocks empty → serve no leading tip (empty history).
func FreezeBlockHistory(history []BlockRecord, rocksGatewayHead int64, rocksOK bool) []BlockRecord {
	if !rocksOK || len(history) == 0 {
		return history
	}
	if rocksGatewayHead < 0 {
		return nil
	}
	out := make([]BlockRecord, 0, len(history))
	for _, b := range history {
		if b.BlockNumber <= rocksGatewayHead {
			out = append(out, b)
		}
	}
	return out
}

// TipLeadsRocks reports explorer tip ahead of Rocks canonical head (#57).
func TipLeadsRocks(memHead, rocksHead int64, rocksOK bool) bool {
	if !rocksOK {
		return false
	}
	return memHead > rocksHead
}
