package consensus

import "github.com/slkproject/slk/core/trophy"

const (
	BaseReward  = 0.00800000
	SilverCut   = 0.30
	BronzeCut   = 0.60
)

// CalculateReward returns the actual SLK reward after tier cut
func CalculateReward(tier trophy.Tier) float64 {
	switch tier {
	case trophy.Gold:
		return BaseReward
	case trophy.Silver:
		return BaseReward * (1.0 - SilverCut)
	case trophy.Bronze:
		return BaseReward * (1.0 - BronzeCut)
	default:
		return BaseReward
	}
}

// CalculateBurn returns how much SLK is burned (not given to winner)
func CalculateBurn(tier trophy.Tier) float64 {
	return BaseReward - CalculateReward(tier)
}

// DetermineTier returns the tier based on finish time vs target
func DetermineTier(finishTime, goldTarget float64) trophy.Tier {
	silverTarget := goldTarget * 1.30
	if finishTime <= goldTarget {
		return trophy.Gold
	}
	if finishTime <= silverTarget {
		return trophy.Silver
	}
	return trophy.Bronze
}
