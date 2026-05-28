package chain

import (
	"github.com/slkproject/slk/core/trophy"
)

const GenesisSeed = "The marathon of a thousand miles begins with a single step."

// GenesisMessage is embedded in the SLK genesis block — permanent and immutable
// Just like Bitcoin embedded: "Chancellor on brink of second bailout for banks"
const GenesisMessage = "SLK — 24/Apr/2026 — Banking for the people, not the banks. The marathon of a thousand miles begins with a single step."
const GenesisDate    = "2025-04-24"
const GenesisCreator = "Franklin Mozac"

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
	// Hardcoded genesis timestamp — MUST be identical on ALL nodes
	// This ensures every node produces the same genesis hash
	genesis.Header.Timestamp = 1745452800 // 2025-04-24 00:00:00 UTC (GenesisDate)
	genesis.Timestamp        = 1745452800
	genesis.Hash             = genesis.ComputeHash()
	// Genesis reward does NOT reduce supply
	genesis.Reward = 0
	return genesis
}
