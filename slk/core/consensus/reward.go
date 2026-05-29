package consensus

import "github.com/slkproject/slk/core/trophy"

const (
	BaseReward = 0.00800000
)

// CalculateReward always returns full 0.008 SLK regardless of tier
func CalculateReward(tier trophy.Tier) float64 {
	return BaseReward
}

// CalculateBurn returns 0 — no burn, full reward always paid
func CalculateBurn(tier trophy.Tier) float64 {
	return 0
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
