package chain

import "math"

func CalculateDistance(peerCount int, trophyHeight uint64) float64 {
	var base float64
	switch {
	case peerCount <= 1:
		base = 8.0
	case peerCount <= 10:
		base = 12.0
	case peerCount <= 50:
		base = 20.0
	case peerCount <= 200:
		base = 35.0
	case peerCount <= 1000:
		base = 60.0
	case peerCount <= 5000:
		base = 100.0
	case peerCount <= 20000:
		base = 150.0
	case peerCount <= 40000:
		base = 200.0
	default:
		// 40000+ miners = VERY HARD starts here
		base = math.Log2(float64(peerCount)+1) * 40.0
	}
	if base < 8.0 { base = 8.0 }

	// Halving every 12500 trophies
	// VERY HARD only kicks in after 100000 SLK mined (12500 trophies)
	halvings := trophyHeight / 12500
	for i := uint64(0); i < halvings; i++ {
		base *= 2.0
	}
	if base > 5000.0 { base = 5000.0 }
	return base
}

func CalculateTargetTime(distance float64) (gold, silver, bronze float64) {
	gold   = distance / 1.2
	silver = gold * 1.40
	bronze = gold * 1.80
	return
}

func EstimateEnergyKWh(powerWatts float64) float64 {
	return powerWatts / 1000.0
}

func DifficultyLabel(peerCount int, height uint64) string {
	dist := CalculateDistance(peerCount, height)
	switch {
	case dist < 15:
		return "EASY"
	case dist < 25:
		return "NORMAL"
	case dist < 50:
		return "MEDIUM"
	case dist < 100:
		return "HARD"
	case dist < 200:
		return "VERY HARD"
	case dist < 500:
		return "EXTREME"
	default:
		return "INSANE"
	}
}
