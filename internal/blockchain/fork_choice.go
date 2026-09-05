package blockchain

import "fmt"

// ErrForkConflict is returned when a peer block conflicts at an existing height.
var ErrForkConflict = fmt.Errorf("fork conflict")

// countYes returns the number of true votes in a vote map.
func countYes(votes map[string]bool) int {
	n := 0
	for _, y := range votes {
		if y {
			n++
		}
	}
	return n
}

// PreferBlock reports whether incoming should replace existing at the same height.
// Score: higher L2Yes, then higher L1Yes, then lexicographically greater BlockHash.
func PreferBlock(existing, incoming BlockRecord) bool {
	if incoming.L2Yes != existing.L2Yes {
		return incoming.L2Yes > existing.L2Yes
	}
	inL2 := countYes(incoming.L2Votes)
	exL2 := countYes(existing.L2Votes)
	if inL2 != exL2 {
		return inL2 > exL2
	}
	if incoming.L1Yes != existing.L1Yes {
		return incoming.L1Yes > existing.L1Yes
	}
	inL1 := countYes(incoming.L1Votes)
	exL1 := countYes(existing.L1Votes)
	if inL1 != exL1 {
		return inL1 > exL1
	}
	return incoming.BlockHash > existing.BlockHash
}
