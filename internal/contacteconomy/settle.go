package contacteconomy

// Deprecated display helpers. Settlement authority is PlatariumCore Escrow Engine
// (modules/contacteconomy presets). Gateway must not use these for applying funds.

const (
	MicroPLPPerPLP = 1_000_000
)

// SplitBps is a display-only mirror of Core contact purpose presets.
// Do not use for settlement — submit escrow_settle to Core instead.
func SplitBps(amount uint64, outcome string) (recipient, node, treasury, refund uint64) {
	var rb, nb, tb, fb uint64
	switch outcome {
	case OutcomeAccepted:
		rb, nb, tb, fb = 70, 20, 10, 0
	case OutcomeTimeout:
		rb, nb, tb, fb = 0, 10, 0, 90
	case OutcomeRejected:
		rb, nb, tb, fb = 0, 10, 10, 80
	default:
		rb, nb, tb, fb = 0, 10, 0, 90
	}
	recipient = amount * rb / 100
	node = amount * nb / 100
	treasury = amount * tb / 100
	refund = amount * fb / 100
	allocated := recipient + node + treasury + refund
	if allocated < amount {
		treasury += amount - allocated
	}
	return
}
