package wallet

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Transaction types
const (
	TxStandard    = 1
	TxIndependent = 2
)

// Transaction represents a real SLK transfer
type Transaction struct {
	ID          string    `json:"id"`
	Type        int       `json:"type"`
	From        string    `json:"from"`
	To          string    `json:"to"`
	Amount      float64   `json:"amount"`
	Timestamp   int64     `json:"timestamp"`
	Signature   string    `json:"signature"`
	SecretCode  string    `json:"secret_code,omitempty"` // Type 2 only
	Status      string    `json:"status"` // pending/confirmed/denied
	Attempts    int       `json:"attempts"` // for type 2 receiver
	FromPubKey  string    `json:"from_pub_key"`
}

// PendingTransaction stored on disk waiting for receiver
type PendingTransaction struct {
	Transaction Transaction `json:"transaction"`
	CreatedAt   int64       `json:"created_at"`
}

var txFile = ""
var pendingFile = ""

func init() {
	home, _ := os.UserHomeDir()
	dir := home + "/.slk/data"
	os.MkdirAll(dir, 0700)
	txFile = dir + "/transactions.json"
	pendingFile = dir + "/pending_tx.json"
}

// GenerateSecretCode generates a cryptographically secure 8-char code
func GenerateSecretCode() string {
	b := make([]byte, 4)
	rand.Read(b)
	return fmt.Sprintf("%08X", b)
}

// GenerateTxID generates a unique transaction ID
func GenerateTxID(from, to string, amount float64, ts int64) string {
	data := fmt.Sprintf("%s%s%.8f%d", from, to, amount, ts)
	h := Hash([]byte(data))
	return hex.EncodeToString(h)[:16]
}

// SignTransaction signs a transaction with private key
func SignTransaction(tx *Transaction, w *Wallet) error {
	// Build message to sign
	msg := fmt.Sprintf("%s|%s|%.8f|%d|%d",
		tx.From, tx.To, tx.Amount, tx.Timestamp, tx.Type)

	sig, err := w.Sign([]byte(msg))
	if err != nil {
		return fmt.Errorf("signing failed: %v", err)
	}

	tx.Signature = hex.EncodeToString(sig)
	tx.FromPubKey = hex.EncodeToString(w.PublicKey)

	// Key rotation happens AFTER verification in main.go
	return nil
}

// VerifyTransactionSignature verifies a transaction is real
func VerifyTransactionSignature(tx *Transaction) bool {
	msg := fmt.Sprintf("%s|%s|%.8f|%d|%d",
		tx.From, tx.To, tx.Amount, tx.Timestamp, tx.Type)

	sig, err := hex.DecodeString(tx.Signature)
	if err != nil {
		return false
	}
	pubKey, err := hex.DecodeString(tx.FromPubKey)
	if err != nil {
		return false
	}

	return Verify([]byte(msg), sig, pubKey)
}

// SavePendingTransaction saves a pending tx to disk
func SavePendingTransaction(tx Transaction) error {
	var pending []PendingTransaction

	// Load existing
	data, err := os.ReadFile(pendingFile)
	if err == nil {
		json.Unmarshal(data, &pending)
	}

	pending = append(pending, PendingTransaction{
		Transaction: tx,
		CreatedAt:   time.Now().Unix(),
	})

	data, err = json.MarshalIndent(pending, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(pendingFile, data, 0600)
}

// GetPendingForAddress gets pending transactions for a wallet address
func GetPendingForAddress(address string) []Transaction {
	data, err := os.ReadFile(pendingFile)
	if err != nil {
		return nil
	}

	var pending []PendingTransaction
	json.Unmarshal(data, &pending)

	var result []Transaction
	for _, p := range pending {
		if p.Transaction.To == address &&
			(p.Transaction.Status == "pending" || p.Transaction.Status == "confirmed") {
			result = append(result, p.Transaction)
		}
	}
	return result
}

// SaveConfirmedTransaction saves a confirmed transaction
func SaveConfirmedTransaction(tx Transaction) error {
	var txs []Transaction

	data, err := os.ReadFile(txFile)
	if err == nil {
		json.Unmarshal(data, &txs)
	}

	txs = append(txs, tx)

	data, err = json.MarshalIndent(txs, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(txFile, data, 0600)
}

// UpdatePendingTransaction updates a pending tx status
func UpdatePendingTransaction(txID string, status string) {
	data, err := os.ReadFile(pendingFile)
	if err != nil {
		return
	}

	var pending []PendingTransaction
	json.Unmarshal(data, &pending)

	for i, p := range pending {
		if p.Transaction.ID == txID {
			pending[i].Transaction.Status = status
		}
	}

	data, _ = json.MarshalIndent(pending, "", "  ")
	os.WriteFile(pendingFile, data, 0600)
}

// IncrementAttempts increments failed attempts for type 2 tx
func IncrementAttempts(txID string) int {
	data, err := os.ReadFile(pendingFile)
	if err != nil {
		return 0
	}

	var pending []PendingTransaction
	json.Unmarshal(data, &pending)

	attempts := 0
	for i, p := range pending {
		if p.Transaction.ID == txID {
			pending[i].Transaction.Attempts++
			attempts = pending[i].Transaction.Attempts
		}
	}

	data, _ = json.MarshalIndent(pending, "", "  ")
	os.WriteFile(pendingFile, data, 0600)
	return attempts
}

// RotatePrivateKey generates new private key after use
func (w *Wallet) RotatePrivateKey() error {
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("key generation failed: %w", err)
	}
	w.PrivateKey = privKey
	w.PublicKey  = pubKey
	fmt.Println("Private key rotated for security!")
	return nil
}


