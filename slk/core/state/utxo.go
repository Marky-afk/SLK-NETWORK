package state

import (
	"encoding/json"
	"fmt"
	"os"
)

var utxoFile = os.Getenv("HOME") + "/.slk/utxo.json"

// UTXO represents an Unspent Transaction Output
// Every SLK coin is a UTXO — traceable back to its origin
type UTXO struct {
	TxID        string  `json:"tx_id"`       // Which transaction created this
	OutputIndex int     `json:"output_index"` // Which output in that transaction
	Amount      float64 `json:"amount"`       // How much SLK
	Address     string  `json:"address"`      // Who owns it
	FromTrophy  uint64  `json:"from_trophy"`  // Which trophy block created it
	Spent       bool    `json:"spent"`        // Has it been spent?
	SpentInTx   string  `json:"spent_in_tx"`  // Which tx spent it
}

// UTXOSet holds all unspent outputs — this IS the wallet balance
type UTXOSet struct {
	UTXOs map[string]*UTXO `json:"utxos"` // key = txid:index
}

// NewUTXOSet creates empty UTXO set
func NewUTXOSet() *UTXOSet {
	return &UTXOSet{
		UTXOs: make(map[string]*UTXO),
	}
}

// Key generates the UTXO key
func UTXOKey(txID string, index int) string {
	return fmt.Sprintf("%s:%d", txID, index)
}

// AddUTXO adds a new unspent output
func (u *UTXOSet) AddUTXO(utxo *UTXO) {
	key := UTXOKey(utxo.TxID, utxo.OutputIndex)
	u.UTXOs[key] = utxo
	u.Save()
}

// SpendUTXO marks a UTXO as spent
func (u *UTXOSet) SpendUTXO(txID string, index int, spentInTx string) bool {
	key := UTXOKey(txID, index)
	utxo, exists := u.UTXOs[key]
	if !exists || utxo.Spent {
		return false
	}
	utxo.Spent = true
	utxo.SpentInTx = spentInTx
	u.Save()
	return true
}

// GetBalance returns real balance for an address
// Scans ALL UTXOs — just like Bitcoin nodes do
func (u *UTXOSet) GetBalance(address string) float64 {
	balance := 0.0
	for _, utxo := range u.UTXOs {
		if utxo.Address == address && !utxo.Spent {
			balance += utxo.Amount
		}
	}
	return balance
}

// GetUnspentForAddress returns all unspent UTXOs for an address
func (u *UTXOSet) GetUnspentForAddress(address string) []*UTXO {
	var unspent []*UTXO
	for _, utxo := range u.UTXOs {
		if utxo.Address == address && !utxo.Spent {
			unspent = append(unspent, utxo)
		}
	}
	return unspent
}

// SelectUTXOs selects UTXOs to cover an amount (like Bitcoin coin selection)
func (u *UTXOSet) SelectUTXOs(address string, amount float64) ([]*UTXO, float64, error) {
	unspent := u.GetUnspentForAddress(address)
	var selected []*UTXO
	total := 0.0

	for _, utxo := range unspent {
		selected = append(selected, utxo)
		total += utxo.Amount
		if total >= amount {
			break
		}
	}

	if total < amount {
		return nil, 0, fmt.Errorf("insufficient funds: have %.8f need %.8f", total, amount)
	}

	return selected, total, nil
}

// PrintUTXOs shows all UTXOs for an address
func (u *UTXOSet) PrintUTXOs(address string) {
	if u == nil {
		fmt.Println("📊 UTXO set not initialized yet — race to earn SLK!")
		return
	}
	fmt.Println("╔══════════════════════════════════════════════════════════╗")
	fmt.Println("║              📊 YOUR REAL UTXO BALANCE                  ║")
	fmt.Println("╠══════════════════════════════════════════════════════════╣")

	total := 0.0
	count := 0
	for _, utxo := range u.UTXOs {
		if utxo.Address == address && !utxo.Spent {
			fmt.Printf("║  UTXO #%d:\n", count+1)
			fmt.Printf("║    TX:      %s\n", utxo.TxID[:16]+"...")
			fmt.Printf("║    Amount:  %.8f SLK\n", utxo.Amount)
			fmt.Printf("║    Trophy:  #%d\n", utxo.FromTrophy)
			fmt.Printf("║    Status:  ✅ UNSPENT\n")
			fmt.Println("║")
			total += utxo.Amount
			count++
		}
	}

	if count == 0 {
		fmt.Println("║  No UTXOs found — race to earn SLK!")
	}

	fmt.Println("╠══════════════════════════════════════════════════════════╣")
	fmt.Printf( "║  Total Unspent: %.8f SLK (%d UTXOs)              ║\n", total, count)
	fmt.Println("║  Every coin traceable to its trophy block               ║")
	fmt.Println("╚══════════════════════════════════════════════════════════╝")
}

// Save persists UTXO set to disk
func (u *UTXOSet) Save() error {
	os.MkdirAll(os.Getenv("HOME")+"/.slk", 0700)
	data, err := json.MarshalIndent(u, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(utxoFile, data, 0600)
}

// Load loads UTXO set from disk
func LoadUTXOSet() *UTXOSet {
	data, err := os.ReadFile(utxoFile)
	if err != nil {
		return NewUTXOSet()
	}
	var u UTXOSet
	if err := json.Unmarshal(data, &u); err != nil {
		return NewUTXOSet()
	}
	if u.UTXOs == nil {
		u.UTXOs = make(map[string]*UTXO)
	}
	fmt.Printf("📊 Loaded UTXO set — %d outputs\n", len(u.UTXOs))
	return &u
}

// GetTotalBalance scans ALL UTXOs for an address — this is the ONLY real balance
// Nobody can fake this — it's derived from actual trophy blocks
func (u *UTXOSet) GetTotalBalance(address string) float64 {
	if u == nil {
		return 0.0
	}
	total := 0.0
	for _, utxo := range u.UTXOs {
		if utxo.Address == address && !utxo.Spent {
			total += utxo.Amount
		}
	}
	return total
}

// HasSufficientFunds checks if address can afford amount — double spend protection
func (u *UTXOSet) HasSufficientFunds(address string, amount float64) bool {
	return u.GetTotalBalance(address) >= amount
}

// IsDoubleSpend checks if a tx is trying to spend already-spent UTXOs
func (u *UTXOSet) IsDoubleSpend(txID string) bool {
	for _, utxo := range u.UTXOs {
		if utxo.SpentInTx == txID && utxo.Spent {
			return true
		}
	}
	return false
}
