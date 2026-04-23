package state

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

var mempoolFile = os.Getenv("HOME") + "/.slk/mempool.json"

// MempoolTx is a transaction waiting to be confirmed in the next trophy block
type MempoolTx struct {
	ID        string  `json:"id"`
	From      string  `json:"from"`
	To        string  `json:"to"`
	Amount    float64 `json:"amount"`
	Fee       float64 `json:"fee"`
	Timestamp int64   `json:"timestamp"`
	Signature string  `json:"signature"`
	PubKey    string  `json:"pub_key"`
	Type      int     `json:"type"`
}

// Mempool holds unconfirmed transactions — exactly like Bitcoin mempool
type Mempool struct {
	mu  sync.Mutex
	Txs map[string]*MempoolTx `json:"txs"`
}

// NewMempool creates an empty mempool
func NewMempool() *Mempool {
	m := &Mempool{
		Txs: make(map[string]*MempoolTx),
	}
	m.load()
	return m
}

// Add adds a transaction to the mempool
func (m *Mempool) Add(tx *MempoolTx) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.Txs[tx.ID]; exists {
		return fmt.Errorf("tx %s already in mempool", tx.ID)
	}
	if tx.Amount <= 0 {
		return fmt.Errorf("invalid amount")
	}

	// Double spend check — reject if duplicate TX ID
	if _, exists := m.Txs[tx.ID]; exists {
		return fmt.Errorf("tx %s already in mempool", tx.ID)
	}
	if tx.From == tx.To {
		return fmt.Errorf("cannot send to yourself")
	}

	tx.Timestamp = time.Now().Unix()
	m.Txs[tx.ID] = tx
	fmt.Printf("📥 Mempool: tx %s added (%.8f SLK from %s)\n",
		tx.ID[:8], tx.Amount, tx.From[:min(16, len(tx.From))])
	m.save()
	return nil
}

// Remove confirms a tx — called when trophy is mined
func (m *Mempool) Remove(txID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.Txs, txID)
	m.save()
}

// GetAll returns all pending transactions
func (m *Mempool) GetAll() []*MempoolTx {
	m.mu.Lock()
	defer m.mu.Unlock()
	txs := make([]*MempoolTx, 0, len(m.Txs))
	for _, tx := range m.Txs {
		txs = append(txs, tx)
	}
	return txs
}

// Size returns how many txs are waiting
func (m *Mempool) Size() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Txs)
}

// ConfirmBlock clears all txs included in a trophy block
func (m *Mempool) ConfirmBlock(txIDs []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range txIDs {
		delete(m.Txs, id)
	}
	fmt.Printf("✅ Mempool: %d txs confirmed in trophy block\n", len(txIDs))
	m.save()
}

// Print shows mempool contents
func (m *Mempool) Print() {
	m.mu.Lock()
	defer m.mu.Unlock()
	fmt.Println("╔══════════════════════════════════════════════════════════╗")
	fmt.Println("║                  📬 MEMPOOL STATUS                      ║")
	fmt.Println("╠══════════════════════════════════════════════════════════╣")
	if len(m.Txs) == 0 {
		fmt.Println("║  No pending transactions                                ║")
	} else {
		i := 1
		for _, tx := range m.Txs {
			fmt.Printf("║  [%d] %s → %s\n", i, tx.From[:min(16, len(tx.From))], tx.To[:min(16, len(tx.To))])
			fmt.Printf("║      Amount: %.8f SLK  Fee: %.8f SLK\n", tx.Amount, tx.Fee)
			i++
		}
	}
	fmt.Printf("║  Total pending: %d txs                                  ║\n", len(m.Txs))
	fmt.Println("╚══════════════════════════════════════════════════════════╝")
}

func (m *Mempool) save() {
	os.MkdirAll(os.Getenv("HOME")+"/.slk", 0700)
	data, _ := json.MarshalIndent(m, "", "  ")
	os.WriteFile(mempoolFile, data, 0600)
}

func (m *Mempool) load() {
	data, err := os.ReadFile(mempoolFile)
	if err != nil {
		return
	}
	json.Unmarshal(data, m)
	if m.Txs == nil {
		m.Txs = make(map[string]*MempoolTx)
	}
	if len(m.Txs) > 0 {
		fmt.Printf("📬 Mempool loaded — %d pending txs\n", len(m.Txs))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
