package chain

func CalculateDistance(peerCount int, trophyHeight uint64) float64 {
	var base float64
	switch {
	case peerCount <= 1:
		base = 10.0
	case peerCount <= 5:
		base = 30.0
	case peerCount <= 20:
		base = 60.0
	case peerCount <= 100:
		base = 100.0
	default:
		base = 200.0
	}
	halvings := trophyHeight / 12500
	for i := uint64(0); i < halvings; i++ {
		base *= 2.0
	}
	return base
}

func CalculateTargetTime(distance float64) (gold, silver, bronze float64) {
	gold = distance * 2.0
	silver = gold * 1.30
	bronze = gold * 1.60
	return
}
