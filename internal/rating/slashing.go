package rating

import (
	"fmt"
)

// SuspensionThreshold matches PlatariumCore slashing.rs (10% of ScoreScale).
const SuspensionThreshold = 100_000

// SlashingReason mirrors Core SlashingReason penalty tiers.
type SlashingReason int

const (
	SlashNoVote SlashingReason = iota
	SlashAgainstMajority
	SlashEquivocation
	SlashInvalidTx
)

func reputationPenaltyFor(reason SlashingReason) int64 {
	switch reason {
	case SlashNoVote:
		return ScoreScale * 2 / 100
	case SlashAgainstMajority:
		return ScoreScale * 3 / 100
	case SlashEquivocation:
		return ScoreScale * 15 / 100
	case SlashInvalidTx:
		return ScoreScale * 10 / 100
	default:
		return 0
	}
}

func stakeSlashFor(reason SlashingReason) int64 {
	switch reason {
	case SlashNoVote:
		return 1
	case SlashAgainstMajority:
		return 2
	case SlashEquivocation:
		return 100
	case SlashInvalidTx:
		return 50
	default:
		return 0
	}
}

// ReasonName returns a stable label for logs and API.
func ReasonName(reason SlashingReason) string {
	switch reason {
	case SlashNoVote:
		return "NoVote"
	case SlashAgainstMajority:
		return "AgainstMajority"
	case SlashEquivocation:
		return "Equivocation"
	case SlashInvalidTx:
		return "InvalidTx"
	default:
		return "Unknown"
	}
}

// ApplySlash reduces reputation and stake; suspends node when reputation falls below threshold.
func (r *Registry) ApplySlash(nodeID string, reason SlashingReason) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.applySlashLocked(nodeID, reason)
}

func (r *Registry) applySlashLocked(nodeID string, reason SlashingReason) error {
	n := r.nodes[nodeID]
	if n == nil {
		return fmt.Errorf("node not found: %s", nodeID)
	}
	repPenalty := reputationPenaltyFor(reason)
	stakeSlash := stakeSlashFor(reason)
	n.ReputationScore -= repPenalty
	if n.ReputationScore < 0 {
		n.ReputationScore = 0
	}
	n.Stake -= stakeSlash
	if n.Stake < 0 {
		n.Stake = 0
	}
	if n.ReputationScore < SuspensionThreshold {
		n.Status = NodeStatusSuspended
	}
	return nil
}

// ApplySlashBatch applies the same reason to multiple node IDs (Core to_penalize list).
func (r *Registry) ApplySlashBatch(nodeIDs []string, reason SlashingReason) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, id := range nodeIDs {
		if id == "" {
			continue
		}
		if _, ok := r.nodes[id]; !ok {
			n := &NodeScores{
				NodeID:       id,
				Status:       NodeStatusActive,
				UptimeScore:  ScoreScale,
				LatencyScore: ScoreScale,
				MaxCapacity:  1,
			}
			n.ComputeReputation(0)
			r.nodes[id] = n
		}
		_ = r.applySlashLocked(id, reason)
	}
}
