package rewards

import "platarium-gateway-go/internal/rating"

// MinValidationXP is the floor for a successful L1/L2 participation reward.
const MinValidationXP = 10

// ComputeValidationXP returns reputation XP for a YES-voter that earned a fee share.
// Mirrors Validation Model economics: higher network load → scarcer committee → more XP;
// higher selection weight vs equal share → more XP (distribution). Floor is MinValidationXP.
func ComputeValidationXP(loadPct int, myWeight, totalWeight int64, voterCount int) int {
	xp := MinValidationXP
	xp += loadXPBonus(loadPct)
	xp += distributionXPBonus(myWeight, totalWeight, voterCount)
	if xp < MinValidationXP {
		return MinValidationXP
	}
	return xp
}

// loadXPBonus scales with committee scarcity (SelectionPercentFromLoad).
// Low load / large committee → 0; peak load / 10% committee → +10.
func loadXPBonus(loadPct int) int {
	sel := rating.SelectionPercentFromLoad(loadPct)
	switch {
	case sel >= 50:
		return 0
	case sel >= 30:
		return 2
	case sel >= 25:
		return 4
	case sel >= 20:
		return 6
	case sel >= 15:
		return 8
	default:
		return 10
	}
}

// distributionXPBonus rewards nodes with above-equal selection weight among YES voters.
// Equal weight → 0; up to 2× equal → +10.
func distributionXPBonus(myWeight, totalWeight int64, voterCount int) int {
	if voterCount <= 0 || totalWeight <= 0 || myWeight <= 0 {
		return 0
	}
	equal := totalWeight / int64(voterCount)
	if equal <= 0 {
		equal = 1
	}
	// ratioScaled: 10 = equal share, 20 = 2× equal
	ratioScaled := myWeight * 10 / equal
	if ratioScaled <= 10 {
		return 0
	}
	bonus := int(ratioScaled - 10)
	if bonus > 10 {
		return 10
	}
	return bonus
}
