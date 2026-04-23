package trophy

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"time"
)

// Tier represents the reward tier for a race winner
type Tier int

const (
	Gold   Tier = iota // 0% cut - full reward
	Silver             // 30% cut
	Bronze             // 60% cut
)

// BlockReward is the base reward per race (0.008 SLK)
const BlockReward = 0.00800000

// Trophy represents a single block in the SLK blockchain
type Trophy struct {
	Header    TrophyHeader
	Timestamp int64
	Winner    string  // Winner's wallet address
	Distance  float64 // Race distance in meters
	FinishTime float64 // Time in seconds
	Tier      Tier
	Reward    float64
	PrevHash  []byte
	Hash      []byte
	VDFProof  string  // Cryptographic proof of real work
	VDFInput  string  // Seed used for VDF
}

// TrophyHeader contains the metadata of the trophy
type TrophyHeader struct {
	Version   uint32
	Height    uint64
	PrevHash  []byte
	Seed      []byte
	Timestamp int64
}

// NewTrophy creates a new trophy (block)
func NewTrophy(prevHash []byte, winner string, distance, finishTime float64, tier Tier, height uint64) *Trophy {
	t := &Trophy{
		Header: TrophyHeader{
			Version:   1,
			Height:    height,
			PrevHash:  prevHash,
			Timestamp: time.Now().Unix(),
		},
		Timestamp:  time.Now().Unix(),
		Winner:     winner,
		Distance:   distance,
		FinishTime: finishTime,
		Tier:       tier,
		Reward:     calculateReward(tier),
		PrevHash:   prevHash,
	}
	t.Hash = t.ComputeHash()
	return t
}

// calculateReward returns the reward based on tier
func calculateReward(tier Tier) float64 {
	return BlockReward // Always full 0.00800000 SLK
}

// ComputeHash calculates the SHA-256 hash of the trophy
func (t *Trophy) ComputeHash() []byte {
	var buf bytes.Buffer

	binary.Write(&buf, binary.LittleEndian, t.Header.Version)
	binary.Write(&buf, binary.LittleEndian, t.Header.Height)
	binary.Write(&buf, binary.LittleEndian, t.Timestamp)
	buf.Write(t.PrevHash)
	buf.WriteString(t.Winner)
	binary.Write(&buf, binary.LittleEndian, t.Distance)
	binary.Write(&buf, binary.LittleEndian, t.FinishTime)

	hash := sha256.Sum256(buf.Bytes())
	return hash[:]
}

// TierName returns the string name of the tier
func (t *Trophy) TierName() string {
	switch t.Tier {
	case Gold:
		return "GOLD 🥇"
	case Silver:
		return "SILVER 🥈"
	case Bronze:
		return "BRONZE 🥉"
	default:
		return "UNKNOWN"
	}
}

// String returns a human-readable trophy summary
func (t *Trophy) String() string {
	return fmt.Sprintf(
		"[TROPHY #%d]\n  Winner:   %s\n  Distance: %.9f m\n  Time:     %.2fs\n  Tier:     %s\n  Reward:   %.8f SLK\n  Hash:     %x\n  PrevHash: %x",
		t.Header.Height,
		t.Winner,
		t.Distance,
		t.FinishTime,
		t.TierName(),
		t.Reward,
		t.Hash,
		t.PrevHash,
	)
}
