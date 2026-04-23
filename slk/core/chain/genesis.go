package chain

import (
	"github.com/slkproject/slk/core/trophy"
)

const GenesisSeed = "The marathon of a thousand miles begins with a single step."

func CreateGenesisTrophy() *trophy.Trophy {
	prevHash := make([]byte, 32)
	genesis := trophy.NewTrophy(
		prevHash,
		"GENESIS",
		0.0,
		0.0,
		trophy.Gold,
		0,
	)
	// Genesis reward does NOT reduce supply
	genesis.Reward = 0
	return genesis
}
