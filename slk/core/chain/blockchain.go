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
	bc.saveChain()
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

	bc.saveChain()
	return newTrophy
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

func (bc *Blockchain) saveChain() {
	os.MkdirAll(os.Getenv("HOME")+"/.slk", 0700)
	data, _ := json.Marshal(bc)
	os.WriteFile(chainPath, data, 0600)
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
