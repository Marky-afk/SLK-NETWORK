package wallet

/*
#cgo LDFLAGS: -lsodium
#include <sodium.h>
#include <string.h>
#include <stdlib.h>
*/
import "C"
import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"
)

type Wallet struct {
	Address           string  `json:"address"`
	PublicKey         []byte  `json:"public_key"`
	PrivateKey        []byte  `json:"private_key"`
	EncryptedPrivKey  string  `json:"encrypted_priv_key,omitempty"`
	Balance           float64 `json:"balance"`
}

func LoadOrCreate(path string) (*Wallet, error) {
	if data, err := os.ReadFile(path); err == nil {
		var w Wallet
		if err := json.Unmarshal(data, &w); err == nil {
			return &w, nil
		}
	}
	w, err := NewWallet()
	if err != nil {
		return nil, err
	}
	os.MkdirAll(filepath.Dir(path), 0700)
	// Save with restricted permissions — only owner can read
	data, _ := json.Marshal(w)
	os.WriteFile(path, data, 0600)
	fmt.Println("🆕 New wallet generated and saved to disk")
	return w, nil
}

func NewWallet() (*Wallet, error) {
	if C.sodium_init() < 0 {
		return nil, fmt.Errorf("libsodium init failed")
	}
	pubKey  := make([]byte, C.crypto_sign_PUBLICKEYBYTES)
	privKey := make([]byte, C.crypto_sign_SECRETKEYBYTES)
	ret := C.crypto_sign_keypair(
		(*C.uchar)(unsafe.Pointer(&pubKey[0])),
		(*C.uchar)(unsafe.Pointer(&privKey[0])),
	)
	if ret != 0 {
		return nil, fmt.Errorf("keypair generation failed")
	}
	pubHex  := hex.EncodeToString(pubKey)
	address := "SLK-" + pubHex[:4] + "-" + pubHex[4:8] + "-" + pubHex[8:12] + "-" + pubHex[12:16]
	return &Wallet{
		Address:    address,
		PublicKey:  pubKey,
		PrivateKey: privKey,
		Balance:    0.0,
	}, nil
}

func (w *Wallet) Sign(message []byte) ([]byte, error) {
	sig := make([]byte, C.crypto_sign_BYTES)
	var sigLen C.ulonglong
	ret := C.crypto_sign_detached(
		(*C.uchar)(unsafe.Pointer(&sig[0])),
		&sigLen,
		(*C.uchar)(unsafe.Pointer(&message[0])),
		C.ulonglong(len(message)),
		(*C.uchar)(unsafe.Pointer(&w.PrivateKey[0])),
	)
	if ret != 0 {
		return nil, fmt.Errorf("signing failed")
	}
	return sig[:sigLen], nil
}

func Verify(message, signature, publicKey []byte) bool {
	ret := C.crypto_sign_verify_detached(
		(*C.uchar)(unsafe.Pointer(&signature[0])),
		(*C.uchar)(unsafe.Pointer(&message[0])),
		C.ulonglong(len(message)),
		(*C.uchar)(unsafe.Pointer(&publicKey[0])),
	)
	return ret == 0
}

func Hash(data []byte) []byte {
	out := make([]byte, C.crypto_hash_sha256_BYTES)
	C.crypto_hash_sha256(
		(*C.uchar)(unsafe.Pointer(&out[0])),
		(*C.uchar)(unsafe.Pointer(&data[0])),
		C.ulonglong(len(data)),
	)
	return out
}

// EncryptPrivateKey encrypts private key with password using libsodium secretbox
func (w *Wallet) EncryptPrivateKey(password string) error {
	if C.sodium_init() < 0 {
		return fmt.Errorf("sodium init failed")
	}
	// Derive key from password using crypto_pwhash
	key := make([]byte, C.crypto_secretbox_KEYBYTES)
	salt := make([]byte, C.crypto_pwhash_SALTBYTES)
	// Use address as salt (deterministic)
	copy(salt, []byte(w.Address))

	ret := C.crypto_pwhash(
		(*C.uchar)(unsafe.Pointer(&key[0])),
		C.ulonglong(len(key)),
		C.CString(password),
		C.ulonglong(len(password)),
		(*C.uchar)(unsafe.Pointer(&salt[0])),
		C.crypto_pwhash_OPSLIMIT_INTERACTIVE,
		C.crypto_pwhash_MEMLIMIT_INTERACTIVE,
		C.crypto_pwhash_ALG_DEFAULT,
	)
	if ret != 0 {
		return fmt.Errorf("key derivation failed")
	}

	// Encrypt private key
	nonce := make([]byte, C.crypto_secretbox_NONCEBYTES)
	ciphertext := make([]byte, int(C.crypto_secretbox_MACBYTES)+len(w.PrivateKey))

	C.crypto_secretbox_easy(
		(*C.uchar)(unsafe.Pointer(&ciphertext[0])),
		(*C.uchar)(unsafe.Pointer(&w.PrivateKey[0])),
		C.ulonglong(len(w.PrivateKey)),
		(*C.uchar)(unsafe.Pointer(&nonce[0])),
		(*C.uchar)(unsafe.Pointer(&key[0])),
	)

	w.EncryptedPrivKey = hex.EncodeToString(ciphertext)
	// Clear private key from memory after encryption
	for i := range w.PrivateKey {
		w.PrivateKey[i] = 0
	}
	fmt.Println("🔐 Private key encrypted with your password!")
	return nil
}

func (w *Wallet) Save(path string) {
	os.MkdirAll(filepath.Dir(path), 0700)
	data, _ := json.Marshal(w)
	// 0600 = only owner can read/write — no one else on system can see it
	os.WriteFile(path, data, 0600)
}

func (w *Wallet) SyncBalance(utxoBalance float64) {
	w.Balance = utxoBalance
}

func (w *Wallet) Print() {
	pubHex  := hex.EncodeToString(w.PublicKey)
	privHex := hex.EncodeToString(w.PrivateKey)
	privMasked := privHex[:4] + strings.Repeat("*", 56) + privHex[60:64]
	privMasked2 := strings.Repeat("*", 60) + privHex[124:]

	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                        💰 YOUR SLK WALLET                               ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════════════════╣")
	fmt.Printf( "║  Address:     %s                                    ║\n", w.Address)
	fmt.Printf( "║  Balance:     %.8f SLK                                          ║\n", w.Balance)
	fmt.Println("╠══════════════════════════════════════════════════════════════════════════╣")
	fmt.Println("║  Public Key (32 bytes / Ed25519):                                        ║")
	fmt.Printf( "║  %s  ║\n", pubHex)
	fmt.Println("╠══════════════════════════════════════════════════════════════════════════╣")
	fmt.Println("║  Private Key (64 bytes / Ed25519) — KEEP SECRET:                        ║")
	fmt.Printf( "║  %s  ║\n", privMasked)
	fmt.Printf( "║  %s  ║\n", privMasked2)
	fmt.Println("╠══════════════════════════════════════════════════════════════════════════╣")
	fmt.Println("║  Algorithm:   Ed25519 (libsodium) — Bitcoin-grade cryptography          ║")
	fmt.Println("║  Storage:     ~/.slk/wallet.json (chmod 600 — owner only)               ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════════════════╝")
}

func (w *Wallet) PrivKeyHex() string {
	return hex.EncodeToString(w.PrivateKey)
}
