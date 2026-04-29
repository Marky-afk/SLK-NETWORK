package chain

import "math"

// TargetBlockTime is the ideal seconds per block — same philosophy as Bitcoin's 10 min
// SLK targets 60 seconds per block at any scale
const TargetBlockTime = 60.0 // seconds

// CalculateDistance computes race distance based on peer count and chain height.
// Distance is calibrated so that a typical CPU finishes in ~TargetBlockTime seconds.
// Formula: distance = speed * time → assuming ~2.0 m/s average CPU race speed:
//   distance = 2.0 * 60 = 120m base at low peer counts, scales with log2 at high counts.
// Halving every 12500 trophies makes it harder over time — like Bitcoin difficulty increase.
func CalculateDistance(peerCount int, trophyHeight uint64) float64 {
	var base float64
	switch {
	case peerCount <= 1:
		base = 2.0
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
		// 40000+ miners — logarithmic scaling prevents runaway difficulty
		base = math.Log2(float64(peerCount)+1) * 40.0
	}
	if base < 2.0 {
		base = 2.0
	}

	// Halving every 12500 trophies — distance doubles = harder over time
	halvings := trophyHeight / 12500
	for i := uint64(0); i < halvings; i++ {
		base *= 2.0
	}

	// Hard cap — prevents blocks becoming impossible even at 1M+ miners
	if base > 5000.0 {
		base = 5000.0
	}
	return base
}

// AdjustDistance dynamically corrects distance based on actual recent block times.
// This is equivalent to Bitcoin's difficulty retarget every 2016 blocks.
// Called every 100 trophies — if blocks are coming too fast, distance increases.
// If blocks are too slow, distance decreases. Target is always TargetBlockTime.
func AdjustDistance(base float64, actualAvgBlockTime float64) float64 {
	if actualAvgBlockTime <= 0 {
		return base
	}
	// ratio > 1 means blocks are too slow → reduce distance
	// ratio < 1 means blocks are too fast → increase distance
	ratio := actualAvgBlockTime / TargetBlockTime

	// Cap adjustment to 4x in either direction per retarget — same as Bitcoin
	if ratio > 4.0 {
		ratio = 4.0
	}
	if ratio < 0.25 {
		ratio = 0.25
	}

	adjusted := base * ratio
	if adjusted < 2.0 {
		adjusted = 2.0
	}
	if adjusted > 5000.0 {
		adjusted = 5000.0
	}
	return adjusted
}

// CalculateTargetTime returns gold/silver/bronze time targets for a given distance.
// Gold = finish in time → full 0.008 SLK reward
// Silver = 30% cut, Bronze = 60% cut
func CalculateTargetTime(distance float64) (gold, silver, bronze float64) {
	gold   = distance / 1.2
	silver = gold * 1.40
	bronze = gold * 1.80
	return
}

// EstimateEnergyKWh converts CPU watts to kilowatt-hours for one block
func EstimateEnergyKWh(powerWatts float64) float64 {
	return (powerWatts / 1000.0) * (TargetBlockTime / 3600.0)
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
