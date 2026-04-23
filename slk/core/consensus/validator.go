package consensus

import (
	"fmt"
	"github.com/slkproject/slk/core/trophy"
)

// ValidateTrophy checks a trophy is legitimate before adding to chain
func ValidateTrophy(t *trophy.Trophy, prevTrophy *trophy.Trophy, expectedHeight uint64) error {
	// Check height
	if t.Header.Height != expectedHeight {
		return fmt.Errorf("bad height: got %d expected %d", t.Header.Height, expectedHeight)
	}

	// Check prevHash links correctly
	if fmt.Sprintf("%x", t.PrevHash) != fmt.Sprintf("%x", prevTrophy.Hash) {
		return fmt.Errorf("prevHash mismatch")
	}

	// Recompute hash and verify
	computed := t.ComputeHash()
	if fmt.Sprintf("%x", computed) != fmt.Sprintf("%x", t.Hash) {
		return fmt.Errorf("hash invalid — trophy tampered")
	}

	// Check reward is correct for tier
	expectedReward := CalculateReward(t.Tier)
	if t.Reward != expectedReward {
		return fmt.Errorf("reward mismatch: got %.8f expected %.8f", t.Reward, expectedReward)
	}

	// Check winner address is valid
	if len(t.Winner) < 10 {
		return fmt.Errorf("invalid winner address")
	}

	return nil
}
