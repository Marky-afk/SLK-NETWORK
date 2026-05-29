package chain

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"github.com/slkproject/slk/core/state"
	"github.com/slkproject/slk/core/trophy"
)

type Blockchain struct {
	Trophies    []*trophy.Trophy `json:"trophies"`
	TotalSupply float64          `json:"total_supply"`
	Height      uint64           `json:"height"`
	UTXOSet     *state.UTXOSet   `json:"utxo_set"`
}

var chainPath = os.Getenv("HOME") + "/.slk/chain.json"

func NewBlockchain() *Blockchain {
	// Try to load existing chain first
	if bc, err := loadChain(); err == nil {
		// CRITICAL: always load UTXOSet from utxo.json — it has ALL UTXOs including received TXs
		// Never trust the embedded utxo_set in chain.json — it only has trophy UTXOs
		bc.UTXOSet = state.LoadUTXOSet()
		if bc.UTXOSet == nil {
			bc.UTXOSet = state.NewUTXOSet()
		}
		fmt.Printf("📦 Loaded existing chain — %d trophies\n", len(bc.Trophies))
		bc.RehashChain()
		bc.SaveChain()
		return bc
	}

	// Fresh start — create genesis (NOT a real race win, just an anchor)
	genesis := CreateGenesisTrophy()
	bc := &Blockchain{
		Trophies:    []*trophy.Trophy{genesis},
		TotalSupply: 2_000_000_000.000,
		Height:      0,
		UTXOSet:     state.NewUTXOSet(),
	}
	bc.SaveChain()
	return bc
}

func (bc *Blockchain) AddTrophy(winner string, distance, finishTime float64, tier trophy.Tier) *trophy.Trophy {
	// Safety guard — should never be nil at this point
	if bc.UTXOSet == nil {
		bc.UTXOSet = state.LoadUTXOSet()
	}

	prevTrophy := bc.Trophies[len(bc.Trophies)-1]
	bc.Height++

	newTrophy := trophy.NewTrophy(
		prevTrophy.Hash,
		winner,
		distance,
		finishTime,
		tier,
		bc.Height,
	)

	bc.Trophies = append(bc.Trophies, newTrophy)
	bc.TotalSupply -= newTrophy.Reward

	// Create real UTXO for winner — traceable to this trophy
	b := make([]byte, 8)
	rand.Read(b)
	txID := hex.EncodeToString(newTrophy.Hash)[:16] + hex.EncodeToString(b)[:8]

	utxo := &state.UTXO{
		TxID:        txID,
		OutputIndex: 0,
		Amount:      newTrophy.Reward,
		Address:     winner,
		FromTrophy:  bc.Height,
		Spent:       false,
	}

	bc.UTXOSet.AddUTXO(utxo)
	fmt.Printf("📊 UTXO created: %.8f SLK → %s\n", newTrophy.Reward, winner[:20])
	fmt.Printf("\n✅ Trophy #%d added to chain!\n", bc.Height)
	fmt.Println(newTrophy.String())
	fmt.Printf("💰 Total SLK Remaining: %.3f\n", bc.TotalSupply)

	bc.SaveChain()
	return newTrophy
}

// GetAverageBlockTime returns avg seconds per block over last N trophies
func (bc *Blockchain) GetAverageBlockTime(last int) float64 {
	if len(bc.Trophies) < 2 { return TargetBlockTime }
	n := last
	if n > len(bc.Trophies)-1 { n = len(bc.Trophies)-1 }
	newest := bc.Trophies[len(bc.Trophies)-1]
	oldest := bc.Trophies[len(bc.Trophies)-1-n]
	elapsed := float64(newest.Timestamp - oldest.Timestamp)
	if elapsed <= 0 { return TargetBlockTime }
	return elapsed / float64(n)
}

// AdjustedDistance returns the difficulty-adjusted race distance
// Retargets every 100 blocks based on actual block times
func (bc *Blockchain) AdjustedDistance(peerCount int) float64 {
	base := CalculateDistance(peerCount, bc.Height)
	if peerCount <= 1 { return base } // solo mining — skip adjustment, use base directly
	if bc.Height < 10 { return base } // not enough data yet
	avgTime := bc.GetAverageBlockTime(100)
	return AdjustDistance(base, avgTime)
}

func (bc *Blockchain) IsValid() bool {
	for i := 1; i < len(bc.Trophies); i++ {
		current  := bc.Trophies[i]
		previous := bc.Trophies[i-1]
		if fmt.Sprintf("%x", current.ComputeHash()) != fmt.Sprintf("%x", current.Hash) {
			return false
		}
		if fmt.Sprintf("%x", current.PrevHash) != fmt.Sprintf("%x", previous.Hash) {
			return false
		}
	}
	return true
}

// saveChain uses atomic write — temp file then rename
// This prevents chain corruption if power cuts out mid-write
func (bc *Blockchain) SaveChain() {
	dir := os.Getenv("HOME") + "/.slk"
	os.MkdirAll(dir, 0700)
	data, err := json.Marshal(bc)
	if err != nil {
		fmt.Printf("Chain marshal error: %v\n", err)
		return
	}
	tmp := chainPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		fmt.Printf("Chain write error: %v\n", err)
		return
	}
	// Atomic rename — either fully written or not at all
	if err := os.Rename(tmp, chainPath); err != nil {
		fmt.Printf("Chain rename error: %v\n", err)
	}
}

func loadChain() (*Blockchain, error) {
	data, err := os.ReadFile(chainPath)
	if err != nil {
		return nil, err
	}
	var bc Blockchain
	if err := json.Unmarshal(data, &bc); err != nil {
		return nil, err
	}
	return &bc, nil
}

// RehashChain recomputes all trophy hashes after JSON load.
// This fixes float64 precision loss from JSON round-trip.
func (bc *Blockchain) RehashChain() {
	for i := 1; i < len(bc.Trophies); i++ {
		prev := bc.Trophies[i-1]
		curr := bc.Trophies[i]
		curr.PrevHash = prev.Hash
		curr.Hash = curr.ComputeHash()
	}
}

// ReconcileUTXOs rebuilds the UTXO set from the chain if counts don't match.
// Runs on startup to fix any missing UTXOs without manual intervention.
func (bc *Blockchain) ReconcileUTXOs() {
	if bc.UTXOSet == nil {
		bc.UTXOSet = state.NewUTXOSet()
	}
	realTrophies := 0
	for _, t := range bc.Trophies {
		if t.Winner != "GENESIS" && t.Reward > 0 {
			realTrophies++
		}
	}
	utxoCount := len(bc.UTXOSet.UTXOs)
	if utxoCount >= realTrophies {
		return // already in sync
	}
	fmt.Printf("⚠️  UTXO mismatch: %d UTXOs vs %d trophies — rebuilding...\n", utxoCount, realTrophies)
	// Rebuild from chain
	for _, t := range bc.Trophies {
		if t.Winner == "GENESIS" || t.Reward == 0 {
			continue
		}
		key := fmt.Sprintf("trophy:%d:%x", t.Header.Height, t.Hash[:8])
		if _, exists := bc.UTXOSet.UTXOs[key]; exists {
			continue // already have it
		}
		bc.UTXOSet.AddUTXO(&state.UTXO{
			TxID:        key,
			OutputIndex: 0,
			Amount:      t.Reward,
			Address:     t.Winner,
			FromTrophy:  uint64(t.Header.Height),
			Spent:       false,
		})
	}
	bc.UTXOSet.Save()
	fmt.Printf("✅ UTXO set rebuilt — %d entries\n", len(bc.UTXOSet.UTXOs))
}
