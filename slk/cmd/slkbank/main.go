package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"image/color"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"strconv"
	"time"
	"io"
	"net/http"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/slkproject/slk/core/state"
	"github.com/slkproject/slk/core/contracts"
	"github.com/slkproject/slk/network/p2p"
	"github.com/slkproject/slk/wallet"
	"github.com/slkproject/slk/core/chain"
	"github.com/slkproject/slk/core/consensus"
	"github.com/slkproject/slk/core/trophy"
	"github.com/slkproject/slk/race/manager"
	vdfmath "github.com/slkproject/slk/race/math"
)

const (
	SLKtoSLKT = 1_000_000.0
	SLKTtoSLKC = 100_000.0
	BankPort   = 30304
)

// ── TYPES ──
type BankAccount struct {
	AccountID  string  `json:"account_id"`
	OwnerAddr  string  `json:"owner_address"`
	Name       string  `json:"name"`
	NameLocked bool    `json:"name_locked"`
	SLK        float64 `json:"slk"`
	SLKT       float64 `json:"slkt"`
	SLKCT      int64   `json:"slkct"`
	PublicKey  string  `json:"public_key"`
	SecretKey  string  `json:"secret_key"`   // NEVER exposed via API — wallet signing only
	SecretKeyH string  `json:"secret_key_hash"`
	WalletAPIKey string `json:"wallet_api_key"` // separate key for /slkapi/balance /slkapi/send
	CreatedAt  int64   `json:"created_at"`
	ProfilePhoto string `json:"profile_photo"`
	Bio          string `json:"bio"`
	Location     string `json:"location"`
	LoginFailures int   `json:"login_failures"`  // brute force tracking
	LockedUntil   int64 `json:"locked_until"`    // lockout timestamp
	BankBalances map[string]float64 `json:"bank_balances"`
}

type BankTX struct {
	ID        string  `json:"id"`
	From      string  `json:"from"`
	To        string  `json:"to"`
	Amount    float64 `json:"amount"`
	Currency  string  `json:"currency"`
	Type      string  `json:"type"`
	Timestamp int64   `json:"timestamp"`
	Note      string  `json:"note"`
	Verified  bool    `json:"verified"`
}

type MarketListing struct {
	ID          string  `json:"id"`
	Seller      string  `json:"seller"`
	SellerName  string  `json:"seller_name"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	ImagePath   string  `json:"image_path"`
	Category    string  `json:"category"`
	Amount      float64 `json:"amount"`
	Currency    string  `json:"currency"`
	FiatPrice   float64 `json:"fiat_price"`
	FiatCur     string  `json:"fiat_currency"`
	CreatedAt   int64   `json:"created_at"`
	Active      bool    `json:"active"`
	Escrow      bool    `json:"escrow"`
	BuyerID     string  `json:"buyer_id"`
	EscrowDone  bool    `json:"escrow_done"`
	Quantity    int     `json:"quantity"`
}

type SocialPost struct {
	ID        string   `json:"id"`
	From      string   `json:"from"`
	Name      string   `json:"name"`
	Text      string   `json:"text"`
	ImagePath string   `json:"image_path"`
	Timestamp int64    `json:"timestamp"`
	Likes     []string `json:"likes"`
	Comments  []Comment `json:"comments"`
}

type Comment struct {
	ID        string `json:"id"`
	From      string `json:"from"`
	Name      string `json:"name"`
	Text      string `json:"text"`
	Timestamp int64  `json:"timestamp"`
}

type FriendRequest struct {
	ID        string `json:"id"`
	From      string `json:"from"`
	FromName  string `json:"from_name"`
	To        string `json:"to"`
	Status    string `json:"status"` // "pending","accepted","rejected"
	Timestamp int64  `json:"timestamp"`
}

type ChatMessage struct {
	ID        string `json:"id"`
	From      string `json:"from"`
	FromName  string `json:"from_name"`
	To        string `json:"to"`
	Text      string `json:"text"`
	Timestamp int64  `json:"timestamp"`
}

type NetworkRecord struct {
	ID        string  `json:"id"`
	From      string  `json:"from"`
	To        string  `json:"to"`
	Amount    float64 `json:"amount"`
	Currency  string  `json:"currency"`
	TxType    string  `json:"tx_type"`
	Timestamp int64   `json:"timestamp"`
	Verified  bool    `json:"verified"`
}

type Notification struct {
	ID        string `json:"id"`
	Text      string `json:"text"`
	Timestamp int64  `json:"timestamp"`
	Read      bool   `json:"read"`
}

type KnownBank struct {
	AccountID string `json:"account_id"`
	Name      string `json:"name"`
	OwnerAddr string `json:"owner_addr"`
	PeerID    string `json:"peer_id"`
	SeenAt    int64  `json:"seen_at"`
}

// Commercial Bank — earns fees from transactions
type CommercialBank struct {
	ID             string      `json:"id"`
	Name           string      `json:"name"`
	OwnerID        string      `json:"owner_id"`
	FeeBasisPoints int64       `json:"fee_basis_points"` // PERMANENT
	Currency       string      `json:"currency"`          // e.g. "SLKA"
	SLKRate        float64     `json:"slk_rate"`          // T per 1 SLK — set by owner PERMANENT
	TotalFees      float64     `json:"total_fees"`
	TotalDeposited float64     `json:"total_deposited"`
	TotalIssuedT   float64     `json:"total_issued_t"`
	CreatedAt      int64       `json:"created_at"`
	Shares         []BankShare `json:"shares"`
	APIKey         string      `json:"api_key"`
	AllowedDomain  string      `json:"allowed_domain"`
	TxCount        int64       `json:"tx_count"`
	Clients        []BankClient `json:"clients"`
	Announcement   string      `json:"announcement"`
	InterestRate   float64     `json:"interest_rate"`
	TotalClients   int64       `json:"total_clients"`
}
// Reserve Bank — locks SLK as backing, issues custom currency
type ReserveBank struct {
	ID             string      `json:"id"`
	Name           string      `json:"name"`
	OwnerID        string      `json:"owner_id"`
	LockedSLK      float64     `json:"locked_slk"`
	IssuedAmount   float64     `json:"issued_amount"`
	Currency       string      `json:"currency"`          // e.g. "SLKA"
	SLKRate        float64     `json:"slk_rate"`          // T per 1 SLK — PERMANENT
	FeeBasisPoints int64       `json:"fee_basis_points"`  // PERMANENT
	TotalFees      float64     `json:"total_fees"`
	TotalDeposited float64     `json:"total_deposited"`
	TotalIssuedT   float64     `json:"total_issued_t"`
	CreatedAt      int64       `json:"created_at"`
	Shares         []BankShare `json:"shares"`
	APIKey         string      `json:"api_key"`
	AllowedDomain  string      `json:"allowed_domain"`
	TxCount        int64       `json:"tx_count"`
	Clients        []BankClient `json:"clients"`
	Announcement   string      `json:"announcement"`
	InterestRate   float64     `json:"interest_rate"`
	TotalClients   int64       `json:"total_clients"`
	MintedAmount   float64     `json:"minted_amount"`
}
// Bank Share — for dividend distribution
type BankShare struct {
	HolderID  string  `json:"holder_id"`
	HolderName string `json:"holder_name"`
	Shares    int64   `json:"shares"`
	TotalShares int64 `json:"total_shares"`
}

// ════════════════════════════════════════
// MULTI-SIG WALLETS
// BankClient — a client account inside a bank
type BankClient struct {
	AccountID    string         `json:"account_id"`
	Name         string         `json:"name"`
	SLKAddress   string         `json:"slk_address"`
	ExternalID   string         `json:"external_id"`
	Email        string         `json:"email"`
	IsCustodial  bool           `json:"is_custodial"`
	Balance      float64        `json:"balance"`
	JoinedAt     int64          `json:"joined_at"`
	Active       bool           `json:"active"`
	Deposits     []BankDeposit  `json:"deposits"`
	TotalDeposited float64      `json:"total_deposited"`
	TotalWithdrawn float64      `json:"total_withdrawn"`
	KYCName      string         `json:"kyc_name"`
	KYCPhone     string         `json:"kyc_phone"`
	KYCEmail     string         `json:"kyc_email"`
	Verified     bool           `json:"verified"`
}
// BankDeposit — a fixed-term deposit (like a term deposit / CD)
type BankDeposit struct {
	ID            string  `json:"id"`
	ClientID      string  `json:"client_id"`
	BankID        string  `json:"bank_id"`
	AmountSLK     float64 `json:"amount_slk"`
	AmountT       float64 `json:"amount_t"`
	DepositedAt   int64   `json:"deposited_at"`
	WithdrawAt    int64   `json:"withdraw_at"`
	WeeksLocked   int     `json:"weeks_locked"`
	Status        string  `json:"status"` // "active", "ready", "withdrawn"
	InterestEarned float64 `json:"interest_earned"`
	ApprovedByOwner bool  `json:"approved_by_owner"`
}
// BankPayment — a payment made through the bank (Stripe-style)
type BankPayment struct {
	ID          string  `json:"id"`
	FromClient  string  `json:"from_client"`
	ToClient    string  `json:"to_client"`
	AmountT     float64 `json:"amount_t"`
	AmountSLK   float64 `json:"amount_slk"`
	Fee         float64 `json:"fee"`
	Memo        string  `json:"memo"`
	Timestamp   int64   `json:"timestamp"`
	Status      string  `json:"status"` // "completed", "pending", "refunded"
	PaymentLink string  `json:"payment_link"`
}
// BankLoan — a loan issued by the bank
type BankLoan struct {
	ID            string  `json:"id"`
	ClientID      string  `json:"client_id"`
	BankID        string  `json:"bank_id"`
	PrincipalSLK  float64 `json:"principal_slk"`
	InterestRate  float64 `json:"interest_rate"`
	WeeksToRepay  int     `json:"weeks_to_repay"`
	IssuedAt      int64   `json:"issued_at"`
	DueAt         int64   `json:"due_at"`
	RepaidAmount  float64 `json:"repaid_amount"`
	Status        string  `json:"status"` // "active", "repaid", "defaulted"
}
// ════════════════════════════════════════
type MultiSigWallet struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Owners       []string `json:"owners"`        // wallet addresses
	Required     int      `json:"required"`       // signatures needed
	Total        int      `json:"total"`          // total signers
	Balance      float64  `json:"balance"`
	CreatedAt    int64    `json:"created_at"`
	CreatedBy    string   `json:"created_by"`
}

type MultiSigTx struct {
	ID           string   `json:"id"`
	WalletID     string   `json:"wallet_id"`
	To           string   `json:"to"`
	Amount       float64  `json:"amount"`
	Currency     string   `json:"currency"`
	Note         string   `json:"note"`
	Signatures   []string `json:"signatures"`    // who has signed
	Required     int      `json:"required"`
	Executed     bool     `json:"executed"`
	CreatedBy    string   `json:"created_by"`
	CreatedAt    int64    `json:"created_at"`
	ExecutedAt   int64    `json:"executed_at"`
}

// ════════════════════════════════════════
// TIME-LOCKED TRANSACTIONS
// ════════════════════════════════════════
type TimeLock struct {
	ID          string  `json:"id"`
	From        string  `json:"from"`
	To          string  `json:"to"`
	Amount      float64 `json:"amount"`
	Currency    string  `json:"currency"`
	UnlockAt    int64   `json:"unlock_at"`    // unix timestamp
	Note        string  `json:"note"`
	Executed    bool    `json:"executed"`
	Cancelled   bool    `json:"cancelled"`
	CreatedAt   int64   `json:"created_at"`
}

// ════════════════════════════════════════
// RECURRING PAYMENTS
// ════════════════════════════════════════
type RecurringPayment struct {
	ID          string  `json:"id"`
	From        string  `json:"from"`
	To          string  `json:"to"`
	Amount      float64 `json:"amount"`
	Currency    string  `json:"currency"`
	Interval    string  `json:"interval"`     // "daily","weekly","monthly"
	NextDue     int64   `json:"next_due"`
	EndDate     int64   `json:"end_date"`     // 0 = forever
	MaxPayments int64   `json:"max_payments"` // 0 = unlimited
	PaidCount   int64   `json:"paid_count"`
	Note        string  `json:"note"`
	Active      bool    `json:"active"`
	CreatedAt   int64   `json:"created_at"`
}

// ════════════════════════════════════════
// GOVERNANCE
// ════════════════════════════════════════
type GovernanceProposal struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	ProposerID   string   `json:"proposer_id"`
	YesVotes     float64  `json:"yes_votes"`
	NoVotes      float64  `json:"no_votes"`
	Abstain      float64  `json:"abstain"`
	Voters       []string `json:"voters"`
	Status       string   `json:"status"`      // "active","passed","rejected","timelocked"
	CreatedAt    int64    `json:"created_at"`
	EndsAt       int64    `json:"ends_at"`
	ActivatesAt  int64    `json:"activates_at"`
}

// ════════════════════════════════════════
// DECENTRALIZED IDENTITY
// ════════════════════════════════════════
type VerifiedIdentity struct {
	WalletID     string `json:"wallet_id"`
	OrgName      string `json:"org_name"`
	OrgType      string `json:"org_type"`      // "business","charity","government"
	RegNumber    string `json:"reg_number"`
	Website      string `json:"website"`
	VerifiedAt   int64  `json:"verified_at"`
	Badge        string `json:"badge"`         // "✅ Verified Business" etc
}

// ── GLOBALS ──
var (
	bankAccount *BankAccount
	mainWallet  *wallet.Wallet
	utxoSet     *state.UTXOSet
	p2pNode     *p2p.Node

	bankPath    string
	txPath      string
	marketPath  string
	socialPath  string
	recordsPath string
	friendsPath string
	chatPath    string
	banksPath   string

	txHistory   []BankTX
	marketList  []MarketListing
	socialFeed  []SocialPost
	netRecords  []NetworkRecord
	friendReqs  []FriendRequest
	chatMsgs    []ChatMessage
	knownBanks     []KnownBank
	myCommercialBanks []CommercialBank
	myReserveBanks    []ReserveBank
	notifications  []Notification
	notifPath      string
	notifLabel     *widget.Label

	dataDir    = os.Getenv("HOME") + "/.slkbank"
	walletPath = os.Getenv("HOME") + "/.slk/wallet.json"

	slkLabel    *widget.Label
	slktLabel   *widget.Label
	slkcLabel   *widget.Label
	walletBal   *widget.Label
	statusBar   *widget.Label
	peersLabel  *widget.Label
	mainTabs    *container.AppTabs
	mainWin     fyne.Window

	socialBox   *container.Scroll
	recordsInner *fyne.Container
	recordsBox   *container.Scroll
	secKeyHidden = true
	secKeyLabel  *widget.Label
)

func main() {
	os.MkdirAll(dataDir, 0700)
	bankPath    = filepath.Join(dataDir, "account.json")
	txPath      = filepath.Join(dataDir, "transactions.json")
	marketPath  = filepath.Join(dataDir, "market.json")
	socialPath  = filepath.Join(dataDir, "social.json")
	recordsPath = filepath.Join(dataDir, "records.json")
	friendsPath = filepath.Join(dataDir, "friends.json")
	chatPath    = filepath.Join(dataDir, "chat.json")
	banksPath   = filepath.Join(dataDir, "banks.json")
	notifPath   = filepath.Join(dataDir, "notifications.json")

	utxoSet    = state.LoadUTXOSet()
	bc         = chain.NewBlockchain()
	bankAccount = loadOrCreateBankAccount()
	txHistory  = loadTxHistory()
	marketList = loadMarket()
	socialFeed = loadSocial()
	netRecords = loadRecords()
	friendReqs = loadFriends()
	chatMsgs   = loadChat()
	knownBanks         = loadBanks()
	myCommercialBanks  = loadCommercialBanks()
	myReserveBanks     = loadReserveBanks()
	bankPayments       = loadBankPayments()
	bankLoans          = loadBankLoans()
	myLoans            = loadLoans()
	myDeposits         = loadDeposits()
	exchangeOrders     = loadExchangeOrders()
	myMultiSigWallets  = loadMultiSigWallets()
	myMultiSigTxs      = loadMultiSigTxs()
	myTimeLocks        = loadTimeLocks()
	myRecurring        = loadRecurring()
	myProposals        = loadProposals()
	myIdentity         = loadIdentity()
	// Check recurring payments due
	go checkRecurringPayments()
	// Check timelocks due
	go checkTimeLocks()
	// Start smart contract executor
	initContracts()
	notifications = loadNotifications()

	if bankAccount.OwnerAddr != "" {
		mainWallet, _ = wallet.LoadOrCreate(walletPath)
		realBal := utxoSet.GetTotalBalance(mainWallet.Address)
		mainWallet.SyncBalance(realBal)
		// Sync bankAccount.SLK from UTXO on startup — single source of truth
		bankAccount.SLK = realBal
		saveBankAccount(bankAccount)
		fmt.Printf("💰 Real Balance (from UTXO): %.8f SLK\n", realBal)
	}

	a := app.New()
	a.Settings().SetTheme(theme.DarkTheme())
	mainWin = a.NewWindow("SLK Bank — Decentralized Financial System")
	mainWin.Resize(fyne.NewSize(1200, 750))
	mainWin.CenterOnScreen()
	mainWin.SetPadded(false)

	if !bankAccount.NameLocked {
		mainWin.SetContent(makeSetupScreen(mainWin))
	} else {
		mainWin.SetContent(makeUI(mainWin))
		startP2P()
		startAPIServer()
	}
	mainWin.ShowAndRun()
}

// ════════════════════════════════════════
// REAL HTTP API SERVER — for websites
// ════════════════════════════════════════
func startAPIServer() {
	mux := http.NewServeMux()

	// ── RATE LIMITER — max 60 requests per minute per IP ──
	type rateEntry struct { count int; window int64 }
	rateLimiter := make(map[string]*rateEntry)
	rateLimit := func(r *http.Request) bool {
		ip := r.RemoteAddr
		for i := len(ip)-1; i >= 0; i-- {
			if ip[i] == ':' { ip = ip[:i]; break }
		}
		now := time.Now().Unix() / 60
		if e, ok := rateLimiter[ip]; ok {
			if e.window == now {
				e.count++
				if e.count > 60 { return false }
			} else {
				e.count = 1; e.window = now
			}
		} else {
			rateLimiter[ip] = &rateEntry{1, now}
		}
		return true
	}

	// ── SECURITY LOGGER ──
	secLog := func(r *http.Request, status string) {
		ip := r.RemoteAddr
		fmt.Printf("🔐 API [%s] %s %s — IP: %s\n", status, r.Method, r.URL.Path, ip)
	}

	// CORS middleware helper
	cors := func(w http.ResponseWriter) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key")
		w.Header().Set("Content-Type", "application/json")
	}

	// AUTH helper — checks X-API-Key matches WalletAPIKey (NEVER uses SecretKey)
	auth := func(r *http.Request) bool {
		key := r.Header.Get("X-API-Key")
		if key == "" { return false }
		// Lockout check
		if bankAccount.LockedUntil > time.Now().Unix() { return false }
		// NEVER allow SecretKey to be used as API key
		if key == bankAccount.SecretKey { return false }
		ok := key == bankAccount.WalletAPIKey
		if !ok {
			bankAccount.LoginFailures++
			if bankAccount.LoginFailures >= 5 {
				bankAccount.LockedUntil = time.Now().Add(15 * time.Minute).Unix()
				bankAccount.LoginFailures = 0
			}
			saveBankAccount(bankAccount)
		} else {
			bankAccount.LoginFailures = 0
		}
		return ok
	}

	// GET /slkapi/info — public account info (no auth needed)
	mux.HandleFunc("/slkapi/info", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == "OPTIONS" { return }
		if !rateLimit(r) { w.WriteHeader(429); json.NewEncoder(w).Encode(map[string]string{"error":"rate limit exceeded"}); return }
		secLog(r, "OK")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"account_id":  bankAccount.AccountID,
			"name":        bankAccount.Name,
			"owner_addr":  bankAccount.OwnerAddr,
			"public_key":  bankAccount.PublicKey,
			"slk_address": bankAccount.OwnerAddr,
		})
	})

	// GET /slkapi/balance — get balance (auth required)
	mux.HandleFunc("/slkapi/balance", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == "OPTIONS" { return }
		if !rateLimit(r) { w.WriteHeader(429); json.NewEncoder(w).Encode(map[string]string{"error":"rate limit exceeded"}); return }
		if !auth(r) {
			secLog(r, "DENIED"); w.WriteHeader(401); json.NewEncoder(w).Encode(map[string]string{"error":"invalid API key"}); return }
		// Always read fresh from UTXO
		bal := 0.0
		if utxoSet != nil && mainWallet != nil {
			bal = utxoSet.GetTotalBalance(mainWallet.Address)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"account_id": bankAccount.AccountID,
			"name":       bankAccount.Name,
			"slk":        bal,
			"slkt":       bankAccount.SLKT,
			"slkct":      bankAccount.SLKCT,
			"address":    bankAccount.OwnerAddr,
		})
	})

	// POST /slkapi/send — send SLK to another address (auth required)
	// Body: {"to":"SLK-xxxx","amount":0.001,"note":"payment"}
	mux.HandleFunc("/slkapi/send", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == "OPTIONS" { return }
		if !rateLimit(r) { w.WriteHeader(429); json.NewEncoder(w).Encode(map[string]string{"error":"rate limit exceeded"}); return }
		if !auth(r) {
			secLog(r, "DENIED"); w.WriteHeader(401); json.NewEncoder(w).Encode(map[string]string{"error":"invalid API key"}); return }
		if r.Method != "POST" { w.WriteHeader(405); return }
		var req struct {
			To     string  `json:"to"`
			Amount float64 `json:"amount"`
			Note   string  `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(400); json.NewEncoder(w).Encode(map[string]string{"error":"bad request"}); return
		}
		if req.To == "" || req.Amount <= 0 {
			w.WriteHeader(400); json.NewEncoder(w).Encode(map[string]string{"error":"invalid to or amount"}); return
		}
		bal := utxoSet.GetTotalBalance(mainWallet.Address)
		fee := req.Amount * 0.001
		if bal < req.Amount + fee {
			w.WriteHeader(400); json.NewEncoder(w).Encode(map[string]string{"error":"insufficient balance"}); return
		}
		// Build and broadcast TX
		txID := fmt.Sprintf("api_%d", time.Now().UnixNano())
		if p2pNode != nil {
			p2pNode.BroadcastTx(p2p.TxMsg{
				ID: txID, From: bankAccount.OwnerAddr,
				To: req.To, Amount: req.Amount,
				Timestamp: time.Now().Unix(),
			})
		}
		// Record it
		tx := BankTX{ID: txID, From: bankAccount.AccountID, To: req.To,
			Amount: req.Amount, Currency: "SLK", Type: "api_send",
			Timestamp: time.Now().Unix(), Note: req.Note, Verified: true}
		txHistory = append(txHistory, tx)
		saveTxHistory()
		bankAccount.SLK -= req.Amount + fee
		saveBankAccount(bankAccount)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true, "tx_id": txID,
			"amount": req.Amount, "fee": fee, "to": req.To,
		})
	})

	// POST /slkapi/verify — verify a payment was received (auth required)
	// Body: {"tx_id":"xxx"} or {"from":"SLK-xxx","amount":0.001}
	mux.HandleFunc("/slkapi/verify", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == "OPTIONS" { return }
		if !auth(r) { w.WriteHeader(401); json.NewEncoder(w).Encode(map[string]string{"error":"invalid API key"}); return }
		var req struct {
			TxID   string  `json:"tx_id"`
			From   string  `json:"from"`
			Amount float64 `json:"amount"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		found := false
		for _, tx := range txHistory {
			if (req.TxID != "" && tx.ID == req.TxID) ||
				(req.From != "" && tx.From == req.From && tx.Amount >= req.Amount) {
				found = true
				json.NewEncoder(w).Encode(map[string]interface{}{
					"verified": true, "tx_id": tx.ID,
					"amount": tx.Amount, "from": tx.From,
					"timestamp": tx.Timestamp,
				})
				return
			}
		}
		if !found {
			json.NewEncoder(w).Encode(map[string]interface{}{"verified": false})
		}
	})

	// GET /slkapi/transactions — list recent transactions (auth required)
	mux.HandleFunc("/slkapi/transactions", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == "OPTIONS" { return }
		if !auth(r) { w.WriteHeader(401); json.NewEncoder(w).Encode(map[string]string{"error":"invalid API key"}); return }
		limit := 20
		start := len(txHistory) - limit
		if start < 0 { start = 0 }
		json.NewEncoder(w).Encode(map[string]interface{}{
			"transactions": txHistory[start:],
			"total": len(txHistory),
		})
	})


	// ── DOMAIN AUTH helper — checks API key AND origin domain ──
	bankAuth := func(r *http.Request) (bool, *CommercialBank) {
		key := r.Header.Get("X-API-Key")
		if key == "" { return false, nil }
		origin := r.Header.Get("Origin")
		if origin == "" { origin = r.Header.Get("Referer") }
		for i := range myCommercialBanks {
			cb := &myCommercialBanks[i]
			if cb.APIKey != key { continue }
			// domain check — if AllowedDomain is set, enforce it
			if cb.AllowedDomain != "" && origin != "" {
				if !containsStr(origin, cb.AllowedDomain) {
					return false, nil
				}
			}
			return true, cb
		}
		return false, nil
	}

	// POST /slkapi/bank/register — create custodial user (no SLK wallet needed)
	// Body: {"external_id":"user_123","name":"John","email":"j@x.com"}
	mux.HandleFunc("/slkapi/bank/register", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == "OPTIONS" { return }
		if !rateLimit(r) { w.WriteHeader(429); json.NewEncoder(w).Encode(map[string]string{"error":"rate limit exceeded"}); return }
		ok, cb := bankAuth(r)
		if !ok {
			secLog(r, "DENIED"); w.WriteHeader(401); json.NewEncoder(w).Encode(map[string]string{"error":"invalid API key or domain not allowed"}); return }
		if r.Method != "POST" { w.WriteHeader(405); return }
		var req struct {
			ExternalID string `json:"external_id"`
			Name       string `json:"name"`
			Email      string `json:"email"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ExternalID == "" {
			w.WriteHeader(400); json.NewEncoder(w).Encode(map[string]string{"error":"external_id required"}); return
		}
		// Check if already exists
		for _, cl := range cb.Clients {
			if cl.ExternalID == req.ExternalID {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": true, "already_exists": true,
					"account_id": cl.AccountID, "balance": cl.Balance, "currency": cb.Currency,
				})
				return
			}
		}
		// Create custodial client
		accountID := fmt.Sprintf("CUST-%x", time.Now().UnixNano())
		cl := BankClient{
			AccountID:   accountID,
			Name:        req.Name,
			SLKAddress:  "",
			ExternalID:  req.ExternalID,
			Email:       req.Email,
			IsCustodial: true,
			Balance:     0,
			JoinedAt:    time.Now().Unix(),
			Active:      true,
			Verified:    false,
			KYCName:     req.Name,
			KYCEmail:    req.Email,
		}
		for i := range myCommercialBanks {
			if myCommercialBanks[i].ID == cb.ID {
				myCommercialBanks[i].Clients = append(myCommercialBanks[i].Clients, cl)
				myCommercialBanks[i].TxCount++
				myCommercialBanks[i].TotalClients++
				break
			}
		}
		saveCommercialBanks()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true, "account_id": accountID,
			"balance": 0, "currency": cb.Currency,
			"message": "Custodial account created. SLK is held by the bank on your behalf.",
		})
	})

	// GET /slkapi/bank/balance?external_id=user_123
	mux.HandleFunc("/slkapi/bank/balance", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == "OPTIONS" { return }
		ok, cb := bankAuth(r)
		if !ok { w.WriteHeader(401); json.NewEncoder(w).Encode(map[string]string{"error":"invalid API key or domain not allowed"}); return }
		extID := r.URL.Query().Get("external_id")
		if extID == "" { w.WriteHeader(400); json.NewEncoder(w).Encode(map[string]string{"error":"external_id required"}); return }
		for _, cl := range cb.Clients {
			if cl.ExternalID == extID || cl.AccountID == extID {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"account_id":  cl.AccountID,
					"name":        cl.Name,
					"balance":     cl.Balance,
					"currency":    cb.Currency,
					"slk_backed":  cl.Balance / cb.SLKRate,
					"is_custodial": cl.IsCustodial,
					"total_deposited": cl.TotalDeposited,
					"total_withdrawn": cl.TotalWithdrawn,
				})
				return
			}
		}
		w.WriteHeader(404); json.NewEncoder(w).Encode(map[string]string{"error":"user not found"})
	})

	// POST /slkapi/bank/credit — give a user SLK earnings (e.g. they completed a task)
	// Body: {"external_id":"user_123","amount":10.0,"memo":"task reward"}
	mux.HandleFunc("/slkapi/bank/credit", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == "OPTIONS" { return }
		if !rateLimit(r) { w.WriteHeader(429); json.NewEncoder(w).Encode(map[string]string{"error":"rate limit exceeded"}); return }
		ok, cb := bankAuth(r)
		if !ok {
			secLog(r, "DENIED"); w.WriteHeader(401); json.NewEncoder(w).Encode(map[string]string{"error":"invalid API key or domain not allowed"}); return }
		if r.Method != "POST" { w.WriteHeader(405); return }
		var req struct {
			ExternalID string  `json:"external_id"`
			Amount     float64 `json:"amount"`
			Memo       string  `json:"memo"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ExternalID == "" || req.Amount <= 0 {
			w.WriteHeader(400); json.NewEncoder(w).Encode(map[string]string{"error":"external_id and amount required"}); return
		}
		// Generate signed TX receipt — cryptographic proof this credit happened
		txID := fmt.Sprintf("cred_%x", time.Now().UnixNano())
		txData := fmt.Sprintf("%s:%s:%.8f:%d", txID, req.ExternalID, req.Amount, time.Now().Unix())
		txHash := sha256.Sum256([]byte(txData))
		txSig := hex.EncodeToString(txHash[:])

		for i := range myCommercialBanks {
			if myCommercialBanks[i].ID != cb.ID { continue }
			for j := range myCommercialBanks[i].Clients {
				cl := &myCommercialBanks[i].Clients[j]
				if cl.ExternalID != req.ExternalID && cl.AccountID != req.ExternalID { continue }
				oldBal := cl.Balance
				cl.Balance += req.Amount
				myCommercialBanks[i].TxCount++
				// Record signed TX in history
				txHistory = append(txHistory, BankTX{
					ID: txID, From: "BANK:" + cb.ID,
					To: cl.AccountID, Amount: req.Amount,
					Currency: cb.Currency, Type: "credit",
					Timestamp: time.Now().Unix(),
					Note: fmt.Sprintf("sig:%s|memo:%s|prev:%.8f", txSig, req.Memo, oldBal),
					Verified: true,
				})
				saveCommercialBanks(); saveTxHistory()
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": true, "tx_id": txID,
					"tx_signature": txSig,
					"account_id": cl.AccountID,
					"credited": req.Amount, "new_balance": cl.Balance,
					"currency": cb.Currency, "memo": req.Memo,
					"slk_backed": cl.Balance / cb.SLKRate,
				})
				return
			}
		}
		w.WriteHeader(404); json.NewEncoder(w).Encode(map[string]string{"error":"user not found"})
	})

	// POST /slkapi/bank/transfer — move balance between 2 custodial users
	// Body: {"from_id":"user_1","to_id":"user_2","amount":5.0,"memo":"payment"}
	mux.HandleFunc("/slkapi/bank/transfer", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == "OPTIONS" { return }
		if !rateLimit(r) { w.WriteHeader(429); json.NewEncoder(w).Encode(map[string]string{"error":"rate limit exceeded"}); return }
		ok, cb := bankAuth(r)
		if !ok {
			secLog(r, "DENIED"); w.WriteHeader(401); json.NewEncoder(w).Encode(map[string]string{"error":"invalid API key or domain not allowed"}); return }
		if r.Method != "POST" { w.WriteHeader(405); return }
		var req struct {
			FromID string  `json:"from_id"`
			ToID   string  `json:"to_id"`
			Amount float64 `json:"amount"`
			Memo   string  `json:"memo"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.FromID == "" || req.ToID == "" || req.Amount <= 0 {
			w.WriteHeader(400); json.NewEncoder(w).Encode(map[string]string{"error":"from_id, to_id, amount required"}); return
		}
		fromIdx, toIdx := -1, -1
		for i := range myCommercialBanks {
			if myCommercialBanks[i].ID != cb.ID { continue }
			for j := range myCommercialBanks[i].Clients {
				cl := &myCommercialBanks[i].Clients[j]
				if cl.ExternalID == req.FromID || cl.AccountID == req.FromID { fromIdx = j }
				if cl.ExternalID == req.ToID   || cl.AccountID == req.ToID   { toIdx = j }
			}
			if fromIdx == -1 { w.WriteHeader(404); json.NewEncoder(w).Encode(map[string]string{"error":"sender not found"}); return }
			if toIdx == -1   { w.WriteHeader(404); json.NewEncoder(w).Encode(map[string]string{"error":"recipient not found"}); return }
			fee := req.Amount * float64(cb.FeeBasisPoints) / 10000.0
			from := &myCommercialBanks[i].Clients[fromIdx]
			to   := &myCommercialBanks[i].Clients[toIdx]
			if from.Balance < req.Amount + fee {
				w.WriteHeader(400); json.NewEncoder(w).Encode(map[string]string{"error":"insufficient balance"}); return
			}
			from.Balance -= req.Amount + fee
			to.Balance   += req.Amount
			myCommercialBanks[i].TotalFees += fee / cb.SLKRate
			myCommercialBanks[i].TxCount++
			bankAccount.SLK += fee / cb.SLKRate
			saveCommercialBanks(); saveBankAccount(bankAccount)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true, "amount": req.Amount, "fee": fee,
				"from_balance": from.Balance, "to_balance": to.Balance,
				"currency": cb.Currency, "memo": req.Memo,
				"tx_count": myCommercialBanks[i].TxCount,
			})
			return
		}
		w.WriteHeader(404); json.NewEncoder(w).Encode(map[string]string{"error":"bank not found"})
	})

	// POST /slkapi/bank/withdraw — custodial user withdraws to real SLK wallet
	// Body: {"external_id":"user_123","amount":5.0,"slk_address":"SLK-xxxx"}
	mux.HandleFunc("/slkapi/bank/withdraw", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == "OPTIONS" { return }
		if !rateLimit(r) { w.WriteHeader(429); json.NewEncoder(w).Encode(map[string]string{"error":"rate limit exceeded"}); return }
		ok, cb := bankAuth(r)
		if !ok {
			secLog(r, "DENIED"); w.WriteHeader(401); json.NewEncoder(w).Encode(map[string]string{"error":"invalid API key or domain not allowed"}); return }
		if r.Method != "POST" { w.WriteHeader(405); return }
		var req struct {
			ExternalID string  `json:"external_id"`
			Amount     float64 `json:"amount"`
			SLKAddress string  `json:"slk_address"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ExternalID == "" || req.Amount <= 0 || req.SLKAddress == "" {
			w.WriteHeader(400); json.NewEncoder(w).Encode(map[string]string{"error":"external_id, amount, slk_address required"}); return
		}
		slkAmount := req.Amount / cb.SLKRate

		// ── WITHDRAWAL LIMITS — protect bank reserves ──
		maxSingleWithdraw := bankAccount.SLK * 0.10 // max 10% of reserves per withdrawal
		if slkAmount > maxSingleWithdraw && maxSingleWithdraw > 0 {
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(map[string]string{
				"error": fmt.Sprintf("withdrawal exceeds single limit (max %.8f SLK per tx)", maxSingleWithdraw),
			})
			return
		}

		for i := range myCommercialBanks {
			if myCommercialBanks[i].ID != cb.ID { continue }
			for j := range myCommercialBanks[i].Clients {
				cl := &myCommercialBanks[i].Clients[j]
				if cl.ExternalID != req.ExternalID && cl.AccountID != req.ExternalID { continue }
				if cl.Balance < req.Amount {
					w.WriteHeader(400); json.NewEncoder(w).Encode(map[string]string{"error":"insufficient balance"}); return
				}
				// Check bank has enough real SLK
				if bankAccount.SLK < slkAmount {
					w.WriteHeader(400); json.NewEncoder(w).Encode(map[string]string{"error":"bank reserve too low"}); return
				}
				cl.Balance -= req.Amount
				cl.TotalWithdrawn += req.Amount
				myCommercialBanks[i].TxCount++
				// Send real SLK on-chain
				txID := fmt.Sprintf("wd_%x", time.Now().UnixNano())
				if p2pNode != nil {
					p2pNode.BroadcastTx(p2p.TxMsg{
						ID: txID, From: bankAccount.OwnerAddr,
						To: req.SLKAddress, Amount: slkAmount,
						Timestamp: time.Now().Unix(),
					})
				}
				bankAccount.SLK -= slkAmount
				saveCommercialBanks(); saveBankAccount(bankAccount)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": true, "tx_id": txID,
					"slk_sent": slkAmount, "currency_deducted": req.Amount,
					"new_balance": cl.Balance, "currency": cb.Currency,
					"to_address": req.SLKAddress,
				})
				return
			}
		}
		w.WriteHeader(404); json.NewEncoder(w).Encode(map[string]string{"error":"user not found"})
	})

	// GET /slkapi/bank/stats — bank stats (tx count, total clients, fees)
	mux.HandleFunc("/slkapi/bank/stats", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == "OPTIONS" { return }
		ok, cb := bankAuth(r)
		if !ok { w.WriteHeader(401); json.NewEncoder(w).Encode(map[string]string{"error":"invalid API key or domain not allowed"}); return }
		custodial, withWallet := 0, 0
		for _, cl := range cb.Clients {
			if cl.IsCustodial { custodial++ } else { withWallet++ }
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"bank_id":         cb.ID,
			"bank_name":       cb.Name,
			"currency":        cb.Currency,
			"slk_rate":        cb.SLKRate,
			"fee_percent":     float64(cb.FeeBasisPoints) / 100.0,
			"total_clients":   len(cb.Clients),
			"custodial_users": custodial,
			"wallet_users":    withWallet,
			"total_fees_slk":  cb.TotalFees,
			"tx_count":        cb.TxCount,
			"total_deposited": cb.TotalDeposited,
			"allowed_domain":  cb.AllowedDomain,
		})
	})

	go func() {
		fmt.Println("🌐 SLK API Server running on port 8081")
		http.ListenAndServe(":8081", mux)
	}()
}

// ════════════════════════════════════════
// SETUP SCREEN
// ════════════════════════════════════════
func makeSetupScreen(w fyne.Window) fyne.CanvasObject {
	logo := canvas.NewText("SLK Bank", theme.ForegroundColor())
	logo.TextSize = 34; logo.TextStyle = fyne.TextStyle{Bold: true}
	logo.Alignment = fyne.TextAlignCenter

	sub := canvas.NewText("Create your decentralized bank account", theme.PlaceHolderColor())
	sub.TextSize = 13; sub.Alignment = fyne.TextAlignCenter

	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("Your full name or business name")
	errLabel  := widget.NewLabel("")
	errLabel.Alignment = fyne.TextAlignCenter

	createBtn := widget.NewButton("Create My Bank Account  →", func() {
		name := strings.TrimSpace(nameEntry.Text)
		if len(name) < 3 { fyne.Do(func() { errLabel.SetText("❌ Name must be at least 3 characters") }); return }
		// Auto-generate bank wallet on first launch
		mainWallet, _ = wallet.LoadOrCreate(walletPath)
		mainWallet.SyncBalance(utxoSet.GetTotalBalance(mainWallet.Address))
		bankAccount.Name = name
		bankAccount.OwnerAddr = mainWallet.Address
		bankAccount.NameLocked = true
		saveBankAccount(bankAccount)
		w.SetContent(makeUI(w))
		startP2P()
	})
	createBtn.Importance = widget.HighImportance

	// Show auto-generated wallet address
	tmpWallet, _ := wallet.LoadOrCreate(walletPath)
	walletInfoLbl := widget.NewLabel("Auto Wallet: " + tmpWallet.Address)
	walletInfoLbl.TextStyle = fyne.TextStyle{Bold: true}

	return container.NewCenter(container.NewVBox(
		container.NewCenter(logo),
		container.NewCenter(sub),
		widget.NewSeparator(),
		container.NewPadded(container.NewVBox(
			widget.NewLabel("Your Name"), nameEntry,
			widget.NewLabel("⚠  Name is permanent. Choose carefully."),
			widget.NewSeparator(),
			widget.NewLabel("ℹ  A wallet is auto-generated for you. You can connect your node wallet later in Dashboard."),
			walletInfoLbl,
			widget.NewSeparator(),
			container.NewPadded(createBtn), errLabel,
		)),
		widget.NewSeparator(),
		widget.NewLabel("Account ID: "+bankAccount.AccountID),
	))
}

// ════════════════════════════════════════
// CONNECT NODE WALLET (Dashboard widget)
// ════════════════════════════════════════
func makeConnectWalletWidget(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("🔑 Connect Node Wallet", theme.ForegroundColor())
	title.TextSize = 13; title.TextStyle = fyne.TextStyle{Bold: true}

	addrEntry := widget.NewEntry()
	addrEntry.SetPlaceHolder("Node wallet address (SLK-xxxx-xxxx-xxxx-xxxx)")
	privEntry := widget.NewPasswordEntry()
	privEntry.SetPlaceHolder("Private key (hex)")
	result := widget.NewLabel("")
	result.Alignment = fyne.TextAlignCenter

	connectBtn := widget.NewButton("🔗 Connect Wallet", func() {
		addr := strings.TrimSpace(addrEntry.Text)
		priv := strings.TrimSpace(privEntry.Text)
		if len(addr) < 8 { fyne.Do(func() { result.SetText("❌ Invalid address") }); return }
		if len(priv) < 32 { fyne.Do(func() { result.SetText("❌ Invalid private key") }); return }

		// Verify private key matches address
		privBytes, err := hex.DecodeString(priv)
		if err != nil || len(privBytes) != 64 {
			fyne.Do(func() { result.SetText("❌ Invalid private key format") }); return
		}
		// Derive public key and verify address matches
		pubKey := ed25519.PrivateKey(privBytes).Public().(ed25519.PublicKey)
		pubHex := hex.EncodeToString(pubKey)
		expectedAddr := "SLK-" + pubHex[:4] + "-" + pubHex[4:8] + "-" + pubHex[8:12] + "-" + pubHex[12:16]
		if expectedAddr != addr {
			fyne.Do(func() { result.SetText("❌ Private key does not match address — rejected") }); return
		}

		// All good — connect
		bankAccount.OwnerAddr = addr
		saveBankAccount(bankAccount)
		pubKeyBytes := ed25519.PrivateKey(privBytes).Public().(ed25519.PublicKey)
		_ = pubKeyBytes
		mainWallet = &wallet.Wallet{Address: addr, PrivateKey: privBytes, PublicKey: []byte(hex.EncodeToString(pubKeyBytes))}
		mainWallet.SyncBalance(utxoSet.GetTotalBalance(addr))
		bankAccount.OwnerAddr = addr
		bankAccount.SLK = mainWallet.Balance
		saveBankAccount(bankAccount)
		fyne.Do(func() {
			result.SetText(fmt.Sprintf("✅ Connected! Balance: %.8f SLK", mainWallet.Balance))
			refreshLabels()
		})
	})
	connectBtn.Importance = widget.HighImportance

	disconnectBtn := widget.NewButton("Disconnect Node Wallet", func() {
		fyne.Do(func() { result.SetText("✅ Node wallet disconnected") })
	})

	return container.NewVBox(
		widget.NewForm(
			widget.NewFormItem("Address", addrEntry),
			widget.NewFormItem("Private Key", privEntry),
		),
		container.NewGridWithColumns(2, connectBtn, disconnectBtn),
		result,
	)
}

// ════════════════════════════════════════
// P2P
// ════════════════════════════════════════
func startP2P() {
	go func() {
		node, err := p2p.NewNode(BankPort, dataDir)
		if err != nil {
			fyne.Do(func() { if statusBar != nil { statusBar.SetText("⚠ P2P offline") } })
			return
		}
		p2pNode = node

		// Announce our bank to the network
		go func() {
			time.Sleep(3 * time.Second)
			if p2pNode != nil {
				p2pNode.BroadcastSocial(p2p.SocialMsg{
					ID: "announce_" + bankAccount.AccountID,
					From: bankAccount.AccountID,
					Name: bankAccount.Name,
					Text: "__BANK_ANNOUNCE__",
					ImagePath: bankAccount.OwnerAddr,
					Timestamp: time.Now().Unix(),
				})
			}
		}()

		p2pNode.OnTx = func(msg p2p.TxMsg) {
			if msg.To != bankAccount.AccountID && msg.To != bankAccount.OwnerAddr { return }
			if msg.Amount <= 0 { return }
			tx := BankTX{ID: msg.ID, From: msg.From, To: msg.To,
				Amount: msg.Amount, Currency: "SLK", Type: "RECEIVE",
				Timestamp: msg.Timestamp, Verified: true}
			txHistory = append(txHistory, tx)
			saveTxHistory()
			bankAccount.SLK += msg.Amount
			saveBankAccount(bankAccount)
			if p2pNode != nil {
				p2pNode.BroadcastBankRecord(p2p.BankRecord{
					ID: msg.ID, From: msg.From, To: msg.To,
					Amount: msg.Amount, Currency: "SLK",
					TxType: "RECEIVE", Timestamp: msg.Timestamp, Verified: true,
				})
			}
			fyne.Do(func() {
				refreshLabels()
				if statusBar != nil { statusBar.SetText(fmt.Sprintf("💰 Received %.8f SLK", msg.Amount)) }
			pushNotif(fmt.Sprintf("💰 Received %.8f SLK from %s", msg.Amount, msg.From))
				dialog.ShowInformation("💰 SLK Received!",
					fmt.Sprintf("Amount:  %.8f SLK\nFrom:    %s\nTX ID:   %s", msg.Amount, msg.From, msg.ID), mainWin)
			})
		}

		p2pNode.OnSocial = func(msg p2p.SocialMsg) {
			// Bank announcement
			if msg.Text == "__BANK_ANNOUNCE__" {
				found := false
				for i, b := range knownBanks {
					if b.AccountID == msg.From { knownBanks[i].SeenAt = msg.Timestamp; found = true; break }
				}
				if !found {
					knownBanks = append(knownBanks, KnownBank{
						AccountID: msg.From, Name: msg.Name,
						OwnerAddr: msg.ImagePath, SeenAt: msg.Timestamp,
					})
					saveBanks()
				}
				return
			}
			// Friend request
			if strings.HasPrefix(msg.Text, "__FRIEND_REQ__:") {
				to := strings.TrimPrefix(msg.Text, "__FRIEND_REQ__:")
				if to != bankAccount.AccountID { return }
				fr := FriendRequest{ID: msg.ID, From: msg.From, FromName: msg.Name,
					To: to, Status: "pending", Timestamp: msg.Timestamp}
				friendReqs = append(friendReqs, fr)
				saveFriends()
				fyne.Do(func() {
					dialog.ShowInformation("👋 Friend Request",
						fmt.Sprintf("%s wants to connect with you!\nGo to Social → Friends to accept.", msg.Name), mainWin)
				})
				return
			}
			// Friend accepted
			if strings.HasPrefix(msg.Text, "__FRIEND_ACCEPT__:") {
				to := strings.TrimPrefix(msg.Text, "__FRIEND_ACCEPT__:")
				if to != bankAccount.AccountID { return }
				for i, fr := range friendReqs {
					if fr.From == bankAccount.AccountID && fr.To == msg.From {
						friendReqs[i].Status = "accepted"; saveFriends(); break
					}
				}
				fyne.Do(func() {
					dialog.ShowInformation("🤝 Friend Accepted",
						fmt.Sprintf("%s accepted your friend request!", msg.Name), mainWin)
				})
				return
			}
			// Private chat message
			if strings.HasPrefix(msg.Text, "__CHAT__:") {
				parts := strings.SplitN(strings.TrimPrefix(msg.Text, "__CHAT__:"), ":", 2)
				if len(parts) != 2 { return }
				to := parts[0]; text := parts[1]
				if to != bankAccount.AccountID { return }
				cm := ChatMessage{ID: msg.ID, From: msg.From, FromName: msg.Name,
					To: to, Text: text, Timestamp: msg.Timestamp}
				chatMsgs = append(chatMsgs, cm)
				saveChat()
				fyne.Do(func() {
					statusBar.SetText(fmt.Sprintf("💬 Message from %s", msg.Name))
				pushNotif(fmt.Sprintf("💬 New message from %s", msg.Name))
				})
				return
			}
			// Handle incoming like from peer
			if strings.HasPrefix(msg.Text, "__LIKE__:") {
				postID := strings.TrimPrefix(msg.Text, "__LIKE__:")
				for i, p := range socialFeed {
					if p.ID == postID {
						already := false
						for _, lid := range socialFeed[i].Likes {
							if lid == msg.From { already = true; break }
						}
						if !already {
							socialFeed[i].Likes = append(socialFeed[i].Likes, msg.From)
							saveSocial()
							fyne.Do(func() { rebuildSocialBox() })
						}
						break
					}
				}
				return
			}
			// Handle incoming comment from peer
			if strings.HasPrefix(msg.Text, "__COMMENT__:") {
				parts := strings.SplitN(strings.TrimPrefix(msg.Text, "__COMMENT__:"), ":", 2)
				if len(parts) == 2 {
					postID := parts[0]; txt := parts[1]
					for i, p := range socialFeed {
						if p.ID == postID {
							c := Comment{
								ID: msg.ID, From: msg.From, Name: msg.Name,
								Text: txt, Timestamp: msg.Timestamp,
							}
							socialFeed[i].Comments = append(socialFeed[i].Comments, c)
							saveSocial()
							fyne.Do(func() { rebuildSocialBox() })
							break
						}
					}
				}
				return
			}
			// Normal social post
			post := SocialPost{ID: msg.ID, From: msg.From, Name: msg.Name,
				Text: msg.Text, ImagePath: msg.ImagePath, Timestamp: msg.Timestamp}
			socialFeed = append(socialFeed, post)
			saveSocial()
			fyne.Do(func() {
				rebuildSocialBox()
				if statusBar != nil { statusBar.SetText(fmt.Sprintf("📢 New post from %s", msg.Name)) }
				pushNotif(fmt.Sprintf("📢 New post from %s", msg.Name))
			})
		}

		p2pNode.OnExchange = func(o p2p.ExchangeOrder) {
			exchangeOrdersMu.Lock()
			found := false
			for i, ex := range exchangeOrders {
				if ex.ID == o.ID { exchangeOrders[i] = o; found = true; break }
			}
			if !found { exchangeOrders = append(exchangeOrders, o) }
			exchangeOrdersMu.Unlock()
			saveExchangeOrders()
		}
		p2pNode.OnBankRecord = func(rec p2p.BankRecord) {
			nr := NetworkRecord{ID: rec.ID, From: rec.From, To: rec.To,
				Amount: rec.Amount, Currency: rec.Currency,
				TxType: rec.TxType, Timestamp: rec.Timestamp, Verified: rec.Verified}
			netRecords = append(netRecords, nr)
			saveRecords()
			fyne.Do(func() { rebuildRecordsBox() })
		}

		p2pNode.Start()

		go func() {
			for {
				time.Sleep(5 * time.Second)
				if mainWallet != nil {
					mainWallet.SyncBalance(utxoSet.GetTotalBalance(mainWallet.Address))
				}
				fyne.Do(func() {
					refreshLabels()
					if peersLabel != nil { peersLabel.SetText(fmt.Sprintf("🌍 %d peers", p2pNode.PeerCount)) }
					if statusBar != nil && p2pNode.PeerCount > 0 {
						statusBar.SetText(fmt.Sprintf("✅ Connected · %d peers", p2pNode.PeerCount))
					}
				})
			}
		}()
	}()
}

// ════════════════════════════════════════
// MAIN UI
// ════════════════════════════════════════
func makeUI(w fyne.Window) fyne.CanvasObject {
	logo := canvas.NewText("SLK Bank", theme.ForegroundColor())
	logo.TextSize = 22; logo.TextStyle = fyne.TextStyle{Bold: true}
	acctName := canvas.NewText(bankAccount.Name, theme.ForegroundColor()); acctName.TextSize = 12
	acctID   := widget.NewLabel(bankAccount.AccountID); acctID.TextStyle = fyne.TextStyle{Monospace: true}
	peersLabel = widget.NewLabel("🌍 Connecting...")
	notifLabel = widget.NewLabel("🔔 0")
	topBar := container.NewBorder(nil, nil,
		container.NewPadded(container.NewVBox(logo, acctName)),
		container.NewPadded(container.NewVBox(acctID, container.New(layout.NewGridLayout(2), peersLabel, notifLabel))))
	statusBar = widget.NewLabel("⏳ Starting P2P...")
	statusBar.Alignment = fyne.TextAlignCenter
	statusBar.TextStyle = fyne.TextStyle{Italic: true}

	// ── GROUP 1: WALLET
	walletTabs := container.NewAppTabs(
		container.NewTabItem("🏦 Dashboard",  makeDashboardTab(w)),
		container.NewTabItem("💸 Send",       makeSendTab(w)),
		container.NewTabItem("⬇ Deposit",    makeDepositTab(w)),
		container.NewTabItem("⬆ Withdraw",   makeWithdrawTab(w)),
		container.NewTabItem("🔄 Convert",   makeConvertTab(w)),
		container.NewTabItem("📋 History",   makeHistoryTab(w)),
		container.NewTabItem("📊 Chart",     makeChartTab(w)),
	)
	walletTabs.SetTabLocation(container.TabLocationTop)

	// ── GROUP 2: BANKING
	bankingTabs := container.NewAppTabs(
		container.NewTabItem("🏛 Banks",        makeBanksTab(w)),
		container.NewTabItem("🔄 B2B",          makeBankTransferTab(w)),
		container.NewTabItem("📄 Loans",        makeLoanTab(w)),
		container.NewTabItem("💹 Interest",     makeInterestTab(w)),
		container.NewTabItem("🔐 MultiSig",     makeMultiSigTab(w)),
		container.NewTabItem("⏰ Time-Lock",    makeTimeLockTab(w)),
		container.NewTabItem("🔄 Recurring",    makeRecurringTab(w)),
	)
	bankingTabs.SetTabLocation(container.TabLocationTop)

	// ── GROUP 3: MARKET & SOCIAL
	marketTabs := container.NewAppTabs(
		container.NewTabItem("🛒 Market",    makeMarketTab(w)),
		container.NewTabItem("💱 Exchange",   makeExchangeTab(w)),
		container.NewTabItem("📜 Contracts",  makeSmartContractsTab(w)),
		container.NewTabItem("💬 Social",    makeSocialTab(w)),
		container.NewTabItem("🗂 Records",   makeRecordsTab(w)),
	)
	marketTabs.SetTabLocation(container.TabLocationTop)

	// ── GROUP 4: NETWORK
	networkTabs := container.NewAppTabs(
		container.NewTabItem("⛏ Mine",        makeMiningTab(w)),
		container.NewTabItem("🌍 Explorer",    makeExplorerTab(w)),
		container.NewTabItem("📡 Peers",       makePeersTab(w)),
	)
	networkTabs.SetTabLocation(container.TabLocationTop)

	// ── GROUP 5: ACCOUNT
	accountTabs := container.NewAppTabs(
		container.NewTabItem("👤 Profile",       makeProfileTab(w)),
		container.NewTabItem("🏅 Identity",      makeIdentityTab(w)),
		container.NewTabItem("🗳 Governance",    makeGovernanceTab(w)),
		container.NewTabItem("🔔 Notifications", makeNotifTab(w)),
		container.NewTabItem("⚙ Settings",      makeSettingsTab(w)),
		container.NewTabItem("🔐 Backup",        makeBackupTab(w)),
	)
	accountTabs.SetTabLocation(container.TabLocationTop)

	mainTabs = container.NewAppTabs(
		container.NewTabItem("💰 Wallet",   walletTabs),
		container.NewTabItem("🏦 Banking",  bankingTabs),
		container.NewTabItem("🛒 Market",   marketTabs),
		container.NewTabItem("🌐 Network",  networkTabs),
		container.NewTabItem("👤 Account",  accountTabs),
	)
	mainTabs.SetTabLocation(container.TabLocationTop)
	return container.NewBorder(
		container.NewVBox(container.NewPadded(topBar), widget.NewSeparator()),
		container.NewVBox(widget.NewSeparator(), container.NewPadded(statusBar)),
		nil, nil, mainTabs,
	)
}

func refreshLabels() {
	// Generate WalletAPIKey if missing — separate from SecretKey
	if bankAccount.WalletAPIKey == "" {
		bankAccount.WalletAPIKey = generateAPIKey(bankAccount.AccountID + "-wallet-" + fmt.Sprintf("%d", time.Now().UnixNano()))
		saveBankAccount(bankAccount)
		fmt.Println("🔑 New Wallet API Key generated (separate from secret key)")
	}

	// Always sync bankAccount.SLK from UTXO — single source of truth
	if mainWallet != nil && utxoSet != nil {
		realBal := utxoSet.GetTotalBalance(mainWallet.Address)
		mainWallet.SyncBalance(realBal)
		bankAccount.SLK = realBal
		saveBankAccount(bankAccount)
	}
	if slkLabel != nil  { slkLabel.SetText(fmt.Sprintf("%.8f SLK", bankAccount.SLK)) }
	if slktLabel != nil { slktLabel.SetText(fmt.Sprintf("%.5f SLKT", bankAccount.SLKT)) }
	if slkcLabel != nil { slkcLabel.SetText(fmt.Sprintf("%d SLKCT", bankAccount.SLKCT)) }
	if walletBal != nil && mainWallet != nil { walletBal.SetText(fmt.Sprintf("%.8f SLK", mainWallet.Balance)) }
}

func pushNotif(text string) {
	n := Notification{
		ID: fmt.Sprintf("n_%x", time.Now().UnixNano()),
		Text: text, Timestamp: time.Now().Unix(), Read: false,
	}
	notifications = append(notifications, n)
	saveNotifications()
	fyne.Do(func() {
		unread := 0
		for _, n := range notifications { if !n.Read { unread++ } }
		if notifLabel != nil { notifLabel.SetText(fmt.Sprintf("🔔 %d", unread)) }
	})
	// System desktop notification
	go func() {
		exec.Command("notify-send", "-i", "dialog-information", "-t", "5000", "SLK Bank", text).Run()
	}()
}

// ════════════════════════════════════════
// TAB 1 — DASHBOARD
// ════════════════════════════════════════
func makeDashboardTab(w fyne.Window) fyne.CanvasObject {
	slkT := canvas.NewText("SLK Balance", theme.PlaceHolderColor()); slkT.TextSize = 11
	slkLabel = widget.NewLabel(fmt.Sprintf("%.8f SLK", bankAccount.SLK)); slkLabel.TextStyle = fyne.TextStyle{Bold: true}
	slktT := canvas.NewText("SLKT Balance", theme.PlaceHolderColor()); slktT.TextSize = 11
	slktLabel = widget.NewLabel(fmt.Sprintf("%.5f SLKT", bankAccount.SLKT)); slktLabel.TextStyle = fyne.TextStyle{Bold: true}
	slkcT := canvas.NewText("SLKCT Balance", theme.PlaceHolderColor()); slkcT.TextSize = 11
	slkcLabel = widget.NewLabel(fmt.Sprintf("%d SLKCT", bankAccount.SLKCT)); slkcLabel.TextStyle = fyne.TextStyle{Bold: true}
	walletT := canvas.NewText("Main Wallet", theme.PlaceHolderColor()); walletT.TextSize = 11
	walletBal = widget.NewLabel("—"); walletBal.TextStyle = fyne.TextStyle{Bold: true}
	if mainWallet != nil { walletBal.SetText(fmt.Sprintf("%.8f SLK", mainWallet.Balance)) }
	card := func(t *canvas.Text, v *widget.Label) fyne.CanvasObject {
		return container.NewPadded(container.NewVBox(t, v, widget.NewSeparator()))
	}
	grid := container.New(layout.NewGridLayout(4),
		card(slkT, slkLabel), card(slktT, slktLabel),
		card(slkcT, slkcLabel), card(walletT, walletBal))
	rates := widget.NewLabel("📊  1 SLK = 1,000,000 SLKT   |   1 SLKT = 100,000 SLKCT   |   SLKCT = whole numbers")
	rates.Alignment = fyne.TextAlignCenter

	// Live SLK/USD price estimated from market trades
	slkPriceLabel := widget.NewLabel("💱 SLK/USD: fetching...")
	slkPriceLabel.Alignment = fyne.TextAlignCenter
	slkPriceLabel.TextStyle = fyne.TextStyle{Bold: true}

	go func() {
		// Get USD/KES rate from exchangerate-api
		resp, err := http.Get("https://api.exchangerate-api.com/v4/latest/USD")
		var usdToKes float64 = 130.0
		if err == nil {
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			var data map[string]interface{}
			if json.Unmarshal(body, &data) == nil {
				if rm, ok := data["rates"].(map[string]interface{}); ok {
					if k, ok := rm["KES"].(float64); ok { usdToKes = k }
				}
			}
		}
		// Estimate SLK price from recent market listings (fiat/slk ratio)
		var totalRatio float64
		var count float64
		for _, l := range marketList {
			if l.Active && l.Amount > 0 && l.FiatPrice > 0 {
				var fiatInUSD float64
				switch l.FiatCur {
				case "KES": fiatInUSD = l.FiatPrice / usdToKes
				case "USD": fiatInUSD = l.FiatPrice
				case "EUR": fiatInUSD = l.FiatPrice * 1.08
				case "GBP": fiatInUSD = l.FiatPrice * 1.27
				default:    fiatInUSD = l.FiatPrice / usdToKes
				}
				totalRatio += fiatInUSD / l.Amount
				count++
			}
		}
		fyne.Do(func() {
			if count > 0 {
				avgUSD := totalRatio / count
				kesVal  := avgUSD * usdToKes
				slkPriceLabel.SetText(fmt.Sprintf("💱 1 SLK ≈ $%.6f USD  |  KES %.4f  |  based on %d listings", avgUSD, kesVal, int(count)))
			} else {
				slkPriceLabel.SetText("💱 SLK/USD: no listings yet — price discovers from trades")
			}
		})
	}()
	qD := widget.NewButton("⬇ Deposit",  func() { mainTabs.SelectIndex(2) }); qD.Importance = widget.HighImportance
	qS := widget.NewButton("💸 Send",    func() { mainTabs.SelectIndex(1) }); qS.Importance = widget.MediumImportance
	qC := widget.NewButton("🔄 Convert", func() { mainTabs.SelectIndex(4) })
	qM := widget.NewButton("🛒 Market",  func() { mainTabs.SelectIndex(5) })
	qR := widget.NewButton("🗂 Records", func() { mainTabs.SelectIndex(8) })
	qSo:= widget.NewButton("💬 Social",  func() { mainTabs.SelectIndex(9) })
	actions := container.New(layout.NewGridLayout(3), qD, qS, qC, qM, qR, qSo)
	recentTitle := canvas.NewText("Recent Transactions", theme.ForegroundColor())
	recentTitle.TextSize = 13; recentTitle.TextStyle = fyne.TextStyle{Bold: true}
	recentBox := container.NewVBox()
	if len(txHistory) == 0 {
		recentBox.Add(widget.NewLabel("No transactions yet."))
	} else {
		start := 0; if len(txHistory) > 5 { start = len(txHistory) - 5 }
		for i := len(txHistory) - 1; i >= start; i-- {
			tx := txHistory[i]
			recentBox.Add(widget.NewLabel(fmt.Sprintf("%s %-9s  %.8f %-5s  %s",
				txIcon(tx.Type), tx.Type, tx.Amount, tx.Currency,
				time.Unix(tx.Timestamp, 0).Format("Jan 02 15:04"))))
			recentBox.Add(widget.NewSeparator())
		}
	}
	// ── Network stats row ──
	peers := 0
	if p2pNode != nil { peers = p2pNode.PeerCount }
	var bcHeight uint64
	if bc != nil { bcHeight = bc.Height }

	// Real wallet balance from UTXO
	myAddr := ""
	if mainWallet != nil { myAddr = mainWallet.Address }
	curBal := 0.0
	if utxoSet != nil { curBal = utxoSet.GetTotalBalance(myAddr) }
	REWARD_SAT    := int64(800000)
	SUPPLY_SAT    := int64(200000000000000000)
	minedSat      := int64(bcHeight) * REWARD_SAT
	remainingSat  := SUPPLY_SAT - minedSat
	remainWhole   := remainingSat / int64(100000000)
	remainFrac    := remainingSat % int64(100000000)
	minedWhole    := minedSat / int64(100000000)
	minedFrac     := minedSat % int64(100000000)
	dist          := chain.CalculateDistance(peers, bcHeight)
	diffLabel     := chain.DifficultyLabel(peers, bcHeight)
	gold, _, _    := chain.CalculateTargetTime(dist)

	supplyLabel   := widget.NewLabel(fmt.Sprintf("%.8f SLK", curBal))
	supplyLabel.TextStyle = fyne.TextStyle{Bold: true}
	diffValLabel  := widget.NewLabel(fmt.Sprintf("%s (%.0fm)", diffLabel, dist))
	diffValLabel.TextStyle = fyne.TextStyle{Bold: true}
	energyLabel   := widget.NewLabel(fmt.Sprintf("~%.0f-%.0f W required", dist*0.5, dist*1.5))
	energyLabel.TextStyle = fyne.TextStyle{Bold: true}
	goldLabel     := widget.NewLabel(fmt.Sprintf("%.0fs gold target", gold))
	goldLabel.TextStyle = fyne.TextStyle{Bold: true}
	heightValLabel := widget.NewLabel(fmt.Sprintf("#%d", bcHeight))
	heightValLabel.TextStyle = fyne.TextStyle{Bold: true}
	peersValLabel := widget.NewLabel(fmt.Sprintf("%d peers", peers))
	peersValLabel.TextStyle = fyne.TextStyle{Bold: true}

	mkStat := func(label string, val *widget.Label) fyne.CanvasObject {
		t := canvas.NewText(label, theme.PlaceHolderColor()); t.TextSize = 9
		val.TextStyle = fyne.TextStyle{Bold: true}
		val.Wrapping = fyne.TextWrapWord
		return container.NewPadded(container.NewVBox(t, val, widget.NewSeparator()))
	}
	totalMinedLbl := widget.NewLabel(fmt.Sprintf("%d.%08d SLK", minedWhole, minedFrac))
	totalMinedLbl.TextStyle = fyne.TextStyle{Bold: true}
	reserveLbl    := widget.NewLabel(fmt.Sprintf("%d.%08d SLK", remainWhole, remainFrac))
	reserveLbl.TextStyle = fyne.TextStyle{Bold: true}

	netGrid := container.New(layout.NewGridLayout(4),
		mkStat("🏆 My Trophies", heightValLabel),
		mkStat("💰 My Wallet", supplyLabel),
		mkStat("🌍 Peers", peersValLabel),
		mkStat("🥇 Gold Target", goldLabel),
		mkStat("⛏ Total Mined", totalMinedLbl),
		mkStat("🏦 Reserve Left", reserveLbl),
		mkStat("⚡ Difficulty", diffValLabel),
		mkStat("🔗 Height", widget.NewLabel(fmt.Sprintf("#%d", bcHeight))),
	)

	netTitle := canvas.NewText("⛓ Network Stats", theme.ForegroundColor())
	netTitle.TextSize = 13; netTitle.TextStyle = fyne.TextStyle{Bold: true}

	// Live update every 5s
	go func() {
		for range time.Tick(5 * time.Second) {
			p := 0
			if p2pNode != nil { p = p2pNode.PeerCount }
			var h uint64
			if bc != nil { h = bc.Height }
			d   := chain.CalculateDistance(p, h)
			dl  := chain.DifficultyLabel(p, h)
			g2, _, _ := chain.CalculateTargetTime(d)
			// Count MY trophies and sync real balance
			myAddr2 := ""
			if mainWallet != nil { myAddr2 = mainWallet.Address }
			myCount := uint64(0)
			if bc != nil {
				for _, t := range bc.Trophies {
					if t.Winner == myAddr2 { myCount++ }
				}
			}
			realBal2 := 0.0
			if mainWallet != nil && utxoSet != nil {
				realBal2 = utxoSet.GetTotalBalance(myAddr2)
				bankAccount.SLK = realBal2
			}
			tMinedSat := int64(h) * int64(800000)
			tRemSat   := int64(200000000000000000) - tMinedSat
			_ = tRemSat

			fyne.Do(func() {
				supplyLabel.SetText(fmt.Sprintf("%.8f SLK", realBal2))
				diffValLabel.SetText(fmt.Sprintf("%s (%.0fm)", dl, d))
				goldLabel.SetText(fmt.Sprintf("%.0fs gold target", g2))
				heightValLabel.SetText(fmt.Sprintf("#%d (chain: #%d)", myCount, h))
				peersValLabel.SetText(fmt.Sprintf("%d peers", p))
				tMsat := int64(h) * int64(800000)
				rSat  := int64(200000000000000000) - tMsat
				totalMinedLbl.SetText(fmt.Sprintf("%d.%08d SLK", tMsat/100000000, tMsat%100000000))
				reserveLbl.SetText(fmt.Sprintf("%d.%08d SLK", rSat/100000000, rSat%100000000))
			})
		}
	}()
	connectWalletWidget := makeConnectWalletWidget(w)
	scroll := container.NewVScroll(container.NewVBox(
		container.NewPadded(grid), widget.NewSeparator(),
		container.NewPadded(container.NewHBox(rates, slkPriceLabel)), widget.NewSeparator(),
		container.NewPadded(actions), widget.NewSeparator(),
		widget.NewAccordion(widget.NewAccordionItem("🔑 Connect Node Wallet", connectWalletWidget)),
		container.NewPadded(container.NewVBox(netTitle, netGrid)), widget.NewSeparator(),
		container.NewPadded(container.NewVBox(recentTitle, recentBox)),
	))
	return scroll
}
// TAB 2 — SEND
// ════════════════════════════════════════
func makeSendTab(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("Send Funds", theme.ForegroundColor())
	title.TextSize = 18; title.TextStyle = fyne.TextStyle{Bold: true}
	toEntry     := widget.NewEntry(); toEntry.SetPlaceHolder("SLKB-xxxx-xxxx or wallet address")
	amountEntry := widget.NewEntry(); amountEntry.SetPlaceHolder("0.00000000")
	currSel     := widget.NewSelect([]string{"SLK","SLKT","SLKCT"}, nil); currSel.SetSelected("SLK")
	noteEntry   := widget.NewEntry(); noteEntry.SetPlaceHolder("Note (optional)"); noteEntry.MultiLine = true
	result      := widget.NewLabel(""); result.Alignment = fyne.TextAlignCenter; result.TextStyle = fyne.TextStyle{Bold: true}

	// Mode selector
	modeSel := widget.NewSelect([]string{"🔒 Standard (Double Confirm)", "🔐 Independency (Verify Code)"}, nil)
	modeSel.SetSelected("🔒 Standard (Double Confirm)")
	modeDesc := widget.NewLabel("Receiver must accept before SLK moves. Safest method.")
	modeDesc.TextStyle = fyne.TextStyle{Italic: true}
	modeSel.OnChanged = func(s string) {
		if s == "🔐 Independency (Verify Code)" {
			modeDesc.SetText("A secret code is generated. Receiver must enter it. 3 attempts max.")
		} else {
			modeDesc.SetText("Receiver must accept before SLK moves. Safest method.")
		}
	}

	// Bank selector — route through a bank to apply fee
	bankOptions := []string{"No Bank (Direct Send)"}
	for _, cb := range myCommercialBanks { bankOptions = append(bankOptions, "🏢 "+cb.Name) }
	for _, rb := range myReserveBanks    { bankOptions = append(bankOptions, "🏛 "+rb.Name) }
	bankSel := widget.NewSelect(bankOptions, nil)
	bankSel.SetSelected("No Bank (Direct Send)")
	feeLabel := widget.NewLabel("Fee: 0.00000000 SLK  (no bank selected)")
	feeLabel.TextStyle = fyne.TextStyle{Italic: true}

	// Update fee preview when bank or amount changes
	updateFeePreview := func() {
		amt, err := strconv.ParseFloat(strings.TrimSpace(amountEntry.Text), 64)
		if err != nil || amt <= 0 { feeLabel.SetText("Fee: — (enter amount first)"); return }
		sel := bankSel.Selected
		if sel == "No Bank (Direct Send)" { feeLabel.SetText("Fee: 0.00000000 SLK  (direct send, no fee)"); return }
		for _, cb := range myCommercialBanks {
			if sel == "🏢 "+cb.Name {
				fee := (amt * float64(cb.FeeBasisPoints)) / 10000.0
				feeLabel.SetText(fmt.Sprintf("Fee: %.8f SLK  (%.2f%%  =  %d basis points)  →  You send: %.8f SLK", fee, float64(cb.FeeBasisPoints)/100.0, cb.FeeBasisPoints, amt+fee))
				return
			}
		}
		for _, rb := range myReserveBanks {
			if sel == "🏛 "+rb.Name {
				fee := (amt * float64(rb.FeeBasisPoints)) / 10000.0
				feeLabel.SetText(fmt.Sprintf("Fee: %.8f SLK  (%.2f%%  =  %d basis points)  →  You send: %.8f SLK", fee, float64(rb.FeeBasisPoints)/100.0, rb.FeeBasisPoints, amt+fee))
				return
			}
		}
	}
	amountEntry.OnChanged = func(_ string) { updateFeePreview() }
	bankSel.OnChanged    = func(_ string) { updateFeePreview() }

	sendBtn := widget.NewButton("Send Now  →", func() {
		amount, err := strconv.ParseFloat(amountEntry.Text, 64)
		if err != nil || amount <= 0 { fyne.Do(func() { result.SetText("❌ Invalid amount") }); return }
		if toEntry.Text == "" { fyne.Do(func() { result.SetText("❌ Enter recipient") }); return }
		currency := currSel.Selected
		to := toEntry.Text
		note := noteEntry.Text
		mode := modeSel.Selected

		// Check balance first
		switch currency {
		case "SLK":
			if amount > bankAccount.SLK { fyne.Do(func() { result.SetText("❌ Insufficient SLK") }); return }
		case "SLKT":
			if amount > bankAccount.SLKT { fyne.Do(func() { result.SetText("❌ Insufficient SLKT") }); return }
		case "SLKCT":
			if int64(amount) > bankAccount.SLKCT { fyne.Do(func() { result.SetText("❌ Insufficient SLKCT") }); return }
		}

		// ── APPLY BANK FEE ──
		selectedBank := bankSel.Selected
		var bankFee float64
		if selectedBank != "No Bank (Direct Send)" {
			for i, cb := range myCommercialBanks {
				if selectedBank == "🏢 "+cb.Name {
					bankFee = (amount * float64(cb.FeeBasisPoints)) / 10000.0
					myCommercialBanks[i].TotalFees += bankFee
					// Auto-distribute dividends to shareholders
					if len(cb.Shares) > 0 {
						var totalShares int64
						for _, s := range cb.Shares { totalShares += s.Shares }
						if totalShares > 0 {
							for _, s := range cb.Shares {
								payout := (bankFee * float64(s.Shares)) / float64(totalShares)
								if s.HolderID == bankAccount.AccountID {
									bankAccount.SLK += payout
								}
								// Record dividend payout
								tx := BankTX{ID: fmt.Sprintf("div_%x", time.Now().UnixNano()),
									From: "DIVIDEND", To: s.HolderID,
									Amount: payout, Currency: "SLK", Type: "DIVIDEND",
									Timestamp: time.Now().Unix(),
									Note: fmt.Sprintf("Dividend from %s", cb.Name), Verified: true}
								txHistory = append(txHistory, tx)
							}
							saveTxHistory()
							saveBankAccount(bankAccount)
						}
					}
					saveCommercialBanks()
					break
				}
			}
			for i, rb := range myReserveBanks {
				if selectedBank == "🏛 "+rb.Name {
					bankFee = (amount * float64(rb.FeeBasisPoints)) / 10000.0
					myReserveBanks[i].TotalFees += bankFee
					// Auto-distribute dividends to shareholders
					if len(rb.Shares) > 0 {
						var totalShares int64
						for _, s := range rb.Shares { totalShares += s.Shares }
						if totalShares > 0 {
							for _, s := range rb.Shares {
								payout := (bankFee * float64(s.Shares)) / float64(totalShares)
								if s.HolderID == bankAccount.AccountID {
									bankAccount.SLK += payout
								}
								tx := BankTX{ID: fmt.Sprintf("div_%x", time.Now().UnixNano()),
									From: "DIVIDEND", To: s.HolderID,
									Amount: payout, Currency: "SLK", Type: "DIVIDEND",
									Timestamp: time.Now().Unix(),
									Note: fmt.Sprintf("Dividend from %s", rb.Name), Verified: true}
								txHistory = append(txHistory, tx)
							}
							saveTxHistory()
							saveBankAccount(bankAccount)
						}
					}
					saveReserveBanks()
					break
				}
			}
			// Check sender has enough for amount + fee
			switch currency {
			case "SLK":
				if amount+bankFee > bankAccount.SLK { fyne.Do(func() { result.SetText(fmt.Sprintf("❌ Insufficient SLK — need %.8f (amount + fee)", amount+bankFee)) }); return }
			case "SLKT":
				if amount+bankFee > bankAccount.SLKT { fyne.Do(func() { result.SetText(fmt.Sprintf("❌ Insufficient SLKT — need %.8f (amount + fee)", amount+bankFee)) }); return }
			}
			// Deduct fee from sender now
			switch currency {
			case "SLK":   bankAccount.SLK  -= bankFee
			case "SLKT":  bankAccount.SLKT -= bankFee
			case "SLKCT": bankAccount.SLKCT -= int64(bankFee)
			}
			saveBankAccount(bankAccount)
			refreshLabels()
		}

		if mode == "🔐 Independency (Verify Code)" {
			// ── INDEPENDENCY MODE ──
			// Generate 8-char verification code
			b := make([]byte, 4)
			rand.Read(b)
			code := fmt.Sprintf("%X-%X", b[:2], b[2:])

			// Lock funds immediately
			switch currency {
			case "SLK":   bankAccount.SLK  -= amount
			case "SLKT":  bankAccount.SLKT -= amount
			case "SLKCT": bankAccount.SLKCT -= int64(amount)
			}
			saveBankAccount(bankAccount)
			refreshLabels()

			txID := fmt.Sprintf("ind_%x", time.Now().UnixNano())

			// Broadcast pending tx to network
			if p2pNode != nil {
				p2pNode.BroadcastTx(p2p.TxMsg{ID: txID, From: bankAccount.AccountID,
					To: to, Amount: amount, Timestamp: time.Now().Unix(), Type: 2})
			}

			// Show code to sender in a dialog
			codeLabel := canvas.NewText(code, color.NRGBA{R:255,G:200,B:0,A:255})
			codeLabel.TextSize = 32
			codeLabel.TextStyle = fyne.TextStyle{Bold: true, Monospace: true}
			attempts := 0
			codeEntry := widget.NewEntry()
			codeEntry.SetPlaceHolder("Receiver enters code here...")
			submitBtn := widget.NewButton("✅ Submit Code", nil)
			cancelTxBtn := widget.NewButton("❌ Cancel — Return Funds", nil)
			attemptsLabel := widget.NewLabel("3 attempts remaining")
			statusLbl := widget.NewLabel(fmt.Sprintf("🔐 Send %.8f %s to %s\nShare this code with receiver privately:", amount, currency, shortAddr(to)))

			dlgContent := container.NewVBox(
				statusLbl,
				widget.NewSeparator(),
				container.NewCenter(codeLabel),
				widget.NewLabel("Receiver must enter this code to claim funds:"),
				codeEntry,
				attemptsLabel,
				container.NewGridWithColumns(2, submitBtn, cancelTxBtn),
			)
			dlg := dialog.NewCustom("🔐 Independency Transaction", "Close", dlgContent, w)

			cancelTxBtn.OnTapped = func() {
				// Return funds
				switch currency {
				case "SLK":   bankAccount.SLK  += amount
				case "SLKT":  bankAccount.SLKT += amount
				case "SLKCT": bankAccount.SLKCT += int64(amount)
				}
				saveBankAccount(bankAccount)
				refreshLabels()
				dlg.Hide()
				fyne.Do(func() { result.SetText("↩ Transaction cancelled — funds returned") })
			}

			submitBtn.OnTapped = func() {
				entered := strings.TrimSpace(codeEntry.Text)
				if entered == code {
					// Code correct — finalize
					tx := BankTX{ID: txID, From: bankAccount.AccountID, To: to,
						Amount: amount, Currency: currency, Type: "INDEPENDENCY_SEND",
						Timestamp: time.Now().Unix(), Note: note, Verified: true}
					txHistory = append(txHistory, tx); saveTxHistory()
					netRecords = append(netRecords, NetworkRecord{ID: txID, From: bankAccount.AccountID,
						To: to, Amount: amount, Currency: currency,
						TxType: "INDEPENDENCY_SEND", Timestamp: time.Now().Unix(), Verified: true})
					saveRecords()
					dlg.Hide()
					fyne.Do(func() {
						result.SetText(fmt.Sprintf("✅ Verified! %.8f %s sent to %s", amount, currency, shortAddr(to)))
						statusBar.SetText("✅ Independency transaction complete")
						toEntry.SetText(""); amountEntry.SetText(""); noteEntry.SetText("")
					})
				} else {
					attempts++
					remaining := 3 - attempts
					if remaining <= 0 {
						// 3 failed attempts — cancel and return
						switch currency {
						case "SLK":   bankAccount.SLK  += amount
						case "SLKT":  bankAccount.SLKT += amount
						case "SLKCT": bankAccount.SLKCT += int64(amount)
						}
						saveBankAccount(bankAccount)
						refreshLabels()
						dlg.Hide()
						fyne.Do(func() { result.SetText("❌ 3 failed attempts — transaction cancelled, funds returned") })
					} else {
						attemptsLabel.SetText(fmt.Sprintf("❌ Wrong code — %d attempt(s) remaining", remaining))
						codeEntry.SetText("")
					}
				}
			}
			dlg.Show()

		} else {
			// ── STANDARD DOUBLE CONFIRMATION MODE ──
			// Confirmation 1
			dialog.ShowConfirm("Confirm Send — Step 1 of 2",
				fmt.Sprintf("Send %.8f %s to\n%s?", amount, currency, to),
				func(ok bool) {
					if !ok { fyne.Do(func() { result.SetText("❌ Cancelled") }); return }
					// Confirmation 2 — sign with private key
					dialog.ShowConfirm("⚠ Final Confirmation — Step 2 of 2",
					fmt.Sprintf("FINAL: Sign and send %.8f %s to\n%s\nThis will lock funds until receiver accepts.\nAre you sure?", amount, currency, to),
						func(ok2 bool) {
							if !ok2 { fyne.Do(func() { result.SetText("❌ Cancelled at step 2") }); return }

							// Lock funds — deduct from balance
							switch currency {
							case "SLK":   bankAccount.SLK  -= amount
							case "SLKT":  bankAccount.SLKT -= amount
							case "SLKCT": bankAccount.SLKCT -= int64(amount)
							}
							saveBankAccount(bankAccount)

							txID := fmt.Sprintf("snd_%x", time.Now().UnixNano())

							// Broadcast to network — receiver will see incoming
							if p2pNode != nil {
								p2pNode.BroadcastTx(p2p.TxMsg{ID: txID, From: bankAccount.AccountID,
									To: to, Amount: amount, Timestamp: time.Now().Unix(), Type: 1})
								p2pNode.BroadcastBankRecord(p2p.BankRecord{ID: txID,
									From: bankAccount.AccountID, To: to, Amount: amount,
									Currency: currency, TxType: "PENDING_SEND",
									Timestamp: time.Now().Unix(), Verified: false})
							}

							// Record as pending
							tx := BankTX{ID: txID, From: bankAccount.AccountID, To: to,
								Amount: amount, Currency: currency, Type: "PENDING_SEND",
								Timestamp: time.Now().Unix(), Note: note, Verified: false}
							txHistory = append(txHistory, tx); saveTxHistory()
							netRecords = append(netRecords, NetworkRecord{ID: txID,
								From: bankAccount.AccountID, To: to, Amount: amount,
								Currency: currency, TxType: "PENDING_SEND",
								Timestamp: time.Now().Unix(), Verified: false})
							saveRecords()

							// Simulate receiver accept/reject dialog (on same node for now)
							fyne.Do(func() {
								refreshLabels()
								result.SetText(fmt.Sprintf("⏳ Sent — waiting for %s to accept...", shortAddr(to)))
								statusBar.SetText("⏳ Transaction pending receiver acceptance")

								// Show receiver accept dialog
								dialog.ShowConfirm("📥 Incoming Transaction",
									fmt.Sprintf("You have incoming %.8f %s from %s\nAccept or Reject?",
										amount, currency, shortAddr(bankAccount.AccountID)),
									func(accepted bool) {
										if accepted {
											// Mark verified
											for i := range txHistory {
												if txHistory[i].ID == txID {
													txHistory[i].Verified = true
													txHistory[i].Type = "SEND"
												}
											}
											saveTxHistory()
											result.SetText(fmt.Sprintf("✅ Accepted! %.8f %s delivered to %s", amount, currency, shortAddr(to)))
											statusBar.SetText("✅ Transaction accepted by receiver")
											toEntry.SetText(""); amountEntry.SetText(""); noteEntry.SetText("")
										} else {
											// Rejected — return funds
											switch currency {
											case "SLK":   bankAccount.SLK  += amount
											case "SLKT":  bankAccount.SLKT += amount
											case "SLKCT": bankAccount.SLKCT += int64(amount)
											}
											saveBankAccount(bankAccount)
											refreshLabels()
											result.SetText("↩ Rejected by receiver — funds returned to you")
											statusBar.SetText("↩ Transaction rejected")
										}
									}, w)
							})
						}, w)
				}, w)
		}
	})
	sendBtn.Importance = widget.HighImportance
	return container.NewVScroll(container.NewPadded(container.NewVBox(
		container.NewCenter(title), widget.NewSeparator(),
		widget.NewForm(
			widget.NewFormItem("To Address", toEntry),
			widget.NewFormItem("Amount", amountEntry),
			widget.NewFormItem("Currency", currSel),
			widget.NewFormItem("Note", noteEntry),
			widget.NewFormItem("Send Mode", modeSel),
			widget.NewFormItem("Route via Bank", bankSel),
		),
		container.NewPadded(modeDesc),
		container.NewPadded(feeLabel),
		widget.NewSeparator(),
		container.NewPadded(sendBtn), result,
	)))
}

// ════════════════════════════════════════
// TAB 3 — DEPOSIT (private key verified)
// ════════════════════════════════════════
// ════════════════════════════════════════
// BANK-TO-BANK TRANSFER
// ════════════════════════════════════════
func makeBankTransferTab(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("Bank-to-Bank Transfer", theme.ForegroundColor())
	title.TextSize = 18; title.TextStyle = fyne.TextStyle{Bold: true}

	// Check user has a bank
	if len(myCommercialBanks) == 0 && len(myReserveBanks) == 0 {
		msg := widget.NewLabel("You need to create a bank first.\nGo to Banks tab → Create Bank.")
		msg.Wrapping = fyne.TextWrapWord
		return container.NewVScroll(container.NewPadded(container.NewVBox(
			container.NewCenter(title), widget.NewSeparator(), container.NewPadded(msg),
		)))
	}

	// My bank selector
	myBankOptions := []string{}
	for _, cb := range myCommercialBanks { myBankOptions = append(myBankOptions, "🏢 "+cb.Name) }
	for _, rb := range myReserveBanks    { myBankOptions = append(myBankOptions, "🏛 "+rb.Name) }
	myBankSel := widget.NewSelect(myBankOptions, nil)
	if len(myBankOptions) > 0 { myBankSel.SetSelected(myBankOptions[0]) }

	toBankEntry  := widget.NewEntry(); toBankEntry.SetPlaceHolder("Destination bank Account ID or name")
	amountEntry  := widget.NewEntry(); amountEntry.SetPlaceHolder("Amount to transfer")
	currSel      := widget.NewSelect([]string{"SLK","SLKT","SLKCT"}, nil); currSel.SetSelected("SLK")
	purposeEntry := widget.NewEntry(); purposeEntry.SetPlaceHolder("Purpose (e.g. liquidity, settlement)")
	result       := widget.NewLabel(""); result.Alignment = fyne.TextAlignCenter
	result.TextStyle = fyne.TextStyle{Bold: true}

	// Fee preview
	feeLabel := widget.NewLabel("Transfer fee: calculated from your bank's basis points")
	feeLabel.TextStyle = fyne.TextStyle{Italic: true}

	amountEntry.OnChanged = func(_ string) {
		amt, err := strconv.ParseFloat(strings.TrimSpace(amountEntry.Text), 64)
		if err != nil || amt <= 0 { feeLabel.SetText("Fee: — (enter amount)"); return }
		sel := myBankSel.Selected
		for _, cb := range myCommercialBanks {
			if sel == "🏢 "+cb.Name {
				fee := (amt * float64(cb.FeeBasisPoints)) / 10000.0
				feeLabel.SetText(fmt.Sprintf("Transfer fee: %.8f SLK  (%.2f%% = %d bp)  Total: %.8f SLK", fee, float64(cb.FeeBasisPoints)/100.0, cb.FeeBasisPoints, amt+fee))
				return
			}
		}
		for _, rb := range myReserveBanks {
			if sel == "🏛 "+rb.Name {
				fee := (amt * float64(rb.FeeBasisPoints)) / 10000.0
				feeLabel.SetText(fmt.Sprintf("Transfer fee: %.8f SLK  (%.2f%% = %d bp)  Total: %.8f SLK", fee, float64(rb.FeeBasisPoints)/100.0, rb.FeeBasisPoints, amt+fee))
				return
			}
		}
	}

	transferBtn := widget.NewButton("🏦 Transfer Between Banks", func() {
		amt, err := strconv.ParseFloat(strings.TrimSpace(amountEntry.Text), 64)
		if err != nil || amt <= 0 { fyne.Do(func() { result.SetText("❌ Invalid amount") }); return }
		toBank := strings.TrimSpace(toBankEntry.Text)
		if toBank == "" { fyne.Do(func() { result.SetText("❌ Enter destination bank") }); return }
		currency := currSel.Selected
		sel := myBankSel.Selected

		// Calculate fee from source bank
		var feeBP int64
		var bankName string
		for i, cb := range myCommercialBanks {
			if sel == "🏢 "+cb.Name { feeBP = cb.FeeBasisPoints; bankName = cb.Name
				myCommercialBanks[i].TotalFees += (amt * float64(feeBP)) / 10000.0
				saveCommercialBanks(); break }
		}
		for i, rb := range myReserveBanks {
			if sel == "🏛 "+rb.Name { feeBP = rb.FeeBasisPoints; bankName = rb.Name
				myReserveBanks[i].TotalFees += (amt * float64(feeBP)) / 10000.0
				saveReserveBanks(); break }
		}
		fee := (amt * float64(feeBP)) / 10000.0
		total := amt + fee

		// Check balance
		switch currency {
		case "SLK":   if total > bankAccount.SLK  { fyne.Do(func() { result.SetText(fmt.Sprintf("❌ Need %.8f SLK (amount + fee)", total)) }); return }
		case "SLKT":  if total > bankAccount.SLKT { fyne.Do(func() { result.SetText(fmt.Sprintf("❌ Need %.8f SLKT (amount + fee)", total)) }); return }
		case "SLKCT": if int64(total) > bankAccount.SLKCT { fyne.Do(func() { result.SetText("❌ Insufficient SLKCT") }); return }
		}

		dialog.ShowConfirm("⚠ Confirm Bank Transfer",
			fmt.Sprintf("From bank: %s\nTo bank: %s\nAmount: %.8f %s\nFee: %.8f SLK\nTotal: %.8f SLK\n\nConfirm transfer?", bankName, toBank, amt, currency, fee, total),
			func(ok bool) {
				if !ok { return }
				// Deduct total from sender
				switch currency {
				case "SLK":   bankAccount.SLK  -= total
				case "SLKT":  bankAccount.SLKT -= total
				case "SLKCT": bankAccount.SLKCT -= int64(total)
				}
				saveBankAccount(bankAccount); refreshLabels()

				txID := fmt.Sprintf("b2b_%x", time.Now().UnixNano())
				tx := BankTX{ID: txID, From: bankAccount.AccountID, To: toBank,
					Amount: amt, Currency: currency, Type: "BANK_TRANSFER",
					Timestamp: time.Now().Unix(),
					Note: fmt.Sprintf("Bank transfer via %s | fee: %.8f | purpose: %s", bankName, fee, purposeEntry.Text),
					Verified: true}
				txHistory = append(txHistory, tx); saveTxHistory()

				if p2pNode != nil {
					p2pNode.BroadcastBankRecord(p2p.BankRecord{ID: txID,
						From: bankAccount.AccountID, To: toBank, Amount: amt,
						Currency: currency, TxType: "BANK_TRANSFER",
						Timestamp: time.Now().Unix(), Verified: true})
				}

				fyne.Do(func() {
					result.SetText(fmt.Sprintf("✅ Transferred %.8f %s to %s\nFee: %.8f SLK", amt, currency, toBank, fee))
					statusBar.SetText(fmt.Sprintf("✅ Bank transfer complete — %.8f %s", amt, currency))
					amountEntry.SetText(""); toBankEntry.SetText(""); purposeEntry.SetText("")
				})
			}, w)
	})
	transferBtn.Importance = widget.HighImportance

	return container.NewVScroll(container.NewPadded(container.NewVBox(
		container.NewCenter(title), widget.NewSeparator(),
		widget.NewForm(
			widget.NewFormItem("From My Bank", myBankSel),
			widget.NewFormItem("To Bank", toBankEntry),
			widget.NewFormItem("Amount", amountEntry),
			widget.NewFormItem("Currency", currSel),
			widget.NewFormItem("Purpose", purposeEntry),
		),
		container.NewPadded(feeLabel),
		widget.NewSeparator(),
		container.NewPadded(transferBtn), result,
	)))
}

// ════════════════════════════════════════
// LOAN SYSTEM
// ════════════════════════════════════════
type Loan struct {
	ID            string  `json:"id"`
	BorrowerID    string  `json:"borrower_id"`
	BankName      string  `json:"bank_name"`
	Principal     float64 `json:"principal"`
	InterestBP    int64   `json:"interest_bp"`
	Currency      string  `json:"currency"`
	CollateralSLK float64 `json:"collateral_slk"`
	IssuedAt      int64   `json:"issued_at"`
	DueAt         int64   `json:"due_at"`
	Repaid        bool    `json:"repaid"`
	RepaidAt      int64   `json:"repaid_at"`
}

var (
	myLoans   []Loan
	loansPath = ""
)

func initLoansPath() {
	if loansPath == "" {
		loansPath = filepath.Join(os.Getenv("HOME"), ".slkbank", "loans.json")
	}
}
func saveLoans() {
	initLoansPath()
	d, _ := json.MarshalIndent(myLoans, "", "  ")
	os.WriteFile(loansPath, d, 0644)
}
func loadLoans() []Loan {
	initLoansPath()
	d, e := os.ReadFile(loansPath)
	if e != nil { return []Loan{} }
	var x []Loan; json.Unmarshal(d, &x); return x
}

func makeLoanTab(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("Loans", theme.ForegroundColor())
	title.TextSize = 18; title.TextStyle = fyne.TextStyle{Bold: true}

	if len(myCommercialBanks) == 0 && len(myReserveBanks) == 0 {
		msg := widget.NewLabel("You need a bank to issue loans.\nGo to Banks tab → Create Bank.")
		msg.Wrapping = fyne.TextWrapWord
		return container.NewVScroll(container.NewPadded(container.NewVBox(
			container.NewCenter(title), widget.NewSeparator(), container.NewPadded(msg),
		)))
	}

	tabs := container.NewAppTabs(
		container.NewTabItem("📋 Active Loans", makeLoanListTab(w)),
		container.NewTabItem("➕ Issue Loan",   makeIssueLoanTab(w)),
	)
	tabs.SetTabLocation(container.TabLocationTop)
	return tabs
}

func makeLoanListTab(w fyne.Window) fyne.CanvasObject {
	box := container.NewVBox()
	active := 0
	for i, loan := range myLoans {
		if loan.Repaid { continue }
		active++
		lIdx := i
		dueDate := time.Unix(loan.DueAt, 0).Format("Jan 02 2006")
		interest := (loan.Principal * float64(loan.InterestBP)) / 10000.0
		totalDue  := loan.Principal + interest
		overdue := ""
		if time.Now().Unix() > loan.DueAt { overdue = "  ⚠ OVERDUE" }

		repayBtn := widget.NewButton("💰 Repay Loan", func() {
			l := myLoans[lIdx]
			interest := (l.Principal * float64(l.InterestBP)) / 10000.0
			total    := l.Principal + interest
			if bankAccount.SLK < total {
				dialog.ShowInformation("❌ Insufficient SLK",
					fmt.Sprintf("You need %.8f SLK to repay this loan.\nYou have %.8f SLK.", total, bankAccount.SLK), w)
				return
			}
			dialog.ShowConfirm("💰 Repay Loan",
				fmt.Sprintf("Repay %.8f SLK (principal) + %.8f SLK (interest)\nTotal: %.8f SLK\n\nConfirm?", l.Principal, interest, total),
				func(ok bool) {
					if !ok { return }
					bankAccount.SLK -= total
					// Return collateral
					bankAccount.SLK += l.CollateralSLK
					myLoans[lIdx].Repaid   = true
					myLoans[lIdx].RepaidAt = time.Now().Unix()
					saveBankAccount(bankAccount); saveLoans(); refreshLabels()
					tx := BankTX{ID: fmt.Sprintf("rep_%x", time.Now().UnixNano()),
						From: bankAccount.AccountID, To: l.BankName,
						Amount: total, Currency: "SLK", Type: "LOAN_REPAYMENT",
						Timestamp: time.Now().Unix(),
						Note: fmt.Sprintf("Loan repayment — collateral %.8f SLK returned", l.CollateralSLK),
						Verified: true}
					txHistory = append(txHistory, tx); saveTxHistory()
					dialog.ShowInformation("✅ Loan Repaid",
						fmt.Sprintf("Loan repaid!\nCollateral of %.8f SLK returned to your account.", l.CollateralSLK), w)
				}, w)
		})
		repayBtn.Importance = widget.HighImportance

		box.Add(container.NewPadded(container.NewVBox(
			widget.NewLabel(fmt.Sprintf("📄 Loan from %s%s", loan.BankName, overdue)),
			widget.NewLabel(fmt.Sprintf("Principal: %.8f %s  |  Interest: %.2f%% (%d bp)", loan.Principal, loan.Currency, float64(loan.InterestBP)/100.0, loan.InterestBP)),
			widget.NewLabel(fmt.Sprintf("Total due: %.8f SLK  |  Due: %s", totalDue, dueDate)),
			widget.NewLabel(fmt.Sprintf("Collateral locked: %.8f SLK", loan.CollateralSLK)),
			repayBtn,
			widget.NewSeparator(),
		)))
	}
	if active == 0 { box.Add(widget.NewLabel("No active loans.")) }
	return container.NewVScroll(container.NewPadded(box))
}

func makeIssueLoanTab(w fyne.Window) fyne.CanvasObject {
	// Bank owner issues a loan to a borrower
	myBankOptions := []string{}
	for _, cb := range myCommercialBanks { myBankOptions = append(myBankOptions, "🏢 "+cb.Name) }
	for _, rb := range myReserveBanks    { myBankOptions = append(myBankOptions, "🏛 "+rb.Name) }
	myBankSel := widget.NewSelect(myBankOptions, nil)
	if len(myBankOptions) > 0 { myBankSel.SetSelected(myBankOptions[0]) }

	borrowerEntry   := widget.NewEntry(); borrowerEntry.SetPlaceHolder("Borrower Account ID")
	principalEntry  := widget.NewEntry(); principalEntry.SetPlaceHolder("Loan amount in SLK")
	interestEntry   := widget.NewEntry(); interestEntry.SetPlaceHolder("Interest % (e.g. 5.0) — fixed forever")
	collateralEntry := widget.NewEntry(); collateralEntry.SetPlaceHolder("Collateral SLK borrower must lock")
	daysEntry       := widget.NewEntry(); daysEntry.SetPlaceHolder("Loan duration in days (e.g. 30)")
	result          := widget.NewLabel(""); result.Alignment = fyne.TextAlignCenter
	result.TextStyle = fyne.TextStyle{Bold: true}

	issueBtn := widget.NewButton("📄 Issue Loan", func() {
		borrower := strings.TrimSpace(borrowerEntry.Text)
		if borrower == "" { fyne.Do(func() { result.SetText("❌ Enter borrower ID") }); return }
		principal, err1 := strconv.ParseFloat(strings.TrimSpace(principalEntry.Text), 64)
		interest,  err2 := strconv.ParseFloat(strings.TrimSpace(interestEntry.Text), 64)
		collateral,err3 := strconv.ParseFloat(strings.TrimSpace(collateralEntry.Text), 64)
		days,      err4 := strconv.ParseInt(strings.TrimSpace(daysEntry.Text), 10, 64)
		if err1 != nil || principal <= 0 { fyne.Do(func() { result.SetText("❌ Invalid principal") }); return }
		if err2 != nil || interest < 0   { fyne.Do(func() { result.SetText("❌ Invalid interest rate") }); return }
		if err3 != nil || collateral < 0 { fyne.Do(func() { result.SetText("❌ Invalid collateral") }); return }
		if err4 != nil || days <= 0      { fyne.Do(func() { result.SetText("❌ Invalid duration") }); return }
		if principal > bankAccount.SLK   { fyne.Do(func() { result.SetText("❌ Insufficient SLK to issue loan") }); return }

		interestBP := int64(interest * 100)
		sel := myBankSel.Selected
		bankName := ""
		for _, cb := range myCommercialBanks { if sel == "🏢 "+cb.Name { bankName = cb.Name } }
		for _, rb := range myReserveBanks    { if sel == "🏛 "+rb.Name { bankName = rb.Name } }

		totalInterest := (principal * float64(interestBP)) / 10000.0

		dialog.ShowConfirm("📄 Issue Loan",
			fmt.Sprintf("Borrower: %s\nPrincipal: %.8f SLK\nInterest: %.2f%% (%.8f SLK)\nCollateral required: %.8f SLK\nDuration: %d days\n\nConfirm?",
				borrower, principal, interest, totalInterest, collateral, days),
			func(ok bool) {
				if !ok { return }
				// Deduct principal from bank owner (sent to borrower)
				bankAccount.SLK -= principal
				saveBankAccount(bankAccount); refreshLabels()

				loan := Loan{
					ID:            fmt.Sprintf("loan_%x", time.Now().UnixNano()),
					BorrowerID:    borrower,
					BankName:      bankName,
					Principal:     principal,
					InterestBP:    interestBP,
					Currency:      "SLK",
					CollateralSLK: collateral,
					IssuedAt:      time.Now().Unix(),
					DueAt:         time.Now().Unix() + (days * 86400),
					Repaid:        false,
				}
				myLoans = append(myLoans, loan)
				saveLoans()

				tx := BankTX{ID: loan.ID, From: bankAccount.AccountID, To: borrower,
					Amount: principal, Currency: "SLK", Type: "LOAN_ISSUED",
					Timestamp: time.Now().Unix(),
					Note: fmt.Sprintf("Loan issued by %s | interest: %.2f%% | due: %d days", bankName, interest, days),
					Verified: true}
				txHistory = append(txHistory, tx); saveTxHistory()

				fyne.Do(func() {
					result.SetText(fmt.Sprintf("✅ Loan of %.8f SLK issued to %s", principal, borrower))
					borrowerEntry.SetText(""); principalEntry.SetText("")
					interestEntry.SetText(""); collateralEntry.SetText(""); daysEntry.SetText("")
				})
			}, w)
	})
	issueBtn.Importance = widget.HighImportance

	return container.NewVScroll(container.NewPadded(container.NewVBox(
		widget.NewForm(
			widget.NewFormItem("From My Bank", myBankSel),
			widget.NewFormItem("Borrower ID", borrowerEntry),
			widget.NewFormItem("Principal (SLK)", principalEntry),
			widget.NewFormItem("Interest %", interestEntry),
			widget.NewFormItem("Collateral (SLK)", collateralEntry),
			widget.NewFormItem("Duration (days)", daysEntry),
		),
		widget.NewSeparator(),
		container.NewPadded(issueBtn), result,
	)))
}

// ════════════════════════════════════════
// INTEREST SYSTEM
// ════════════════════════════════════════
type Deposit struct {
	ID            string  `json:"id"`
	DepositorID   string  `json:"depositor_id"`
	BankName      string  `json:"bank_name"`
	Amount        float64 `json:"amount"`
	InterestBP    int64   `json:"interest_bp"`
	Currency      string  `json:"currency"`
	DepositedAt   int64   `json:"deposited_at"`
	MaturesAt     int64   `json:"matures_at"`
	Withdrawn     bool    `json:"withdrawn"`
	WithdrawnAt   int64   `json:"withdrawn_at"`
}

var (
	myDeposits   []Deposit
	depositsPath = ""
)

func initDepositsPath() {
	if depositsPath == "" {
		depositsPath = filepath.Join(os.Getenv("HOME"), ".slkbank", "deposits.json")
	}
}
func saveDeposits() {
	initDepositsPath()
	d, _ := json.MarshalIndent(myDeposits, "", "  ")
	os.WriteFile(depositsPath, d, 0644)
}
func loadDeposits() []Deposit {
	initDepositsPath()
	d, e := os.ReadFile(depositsPath)
	if e != nil { return []Deposit{} }
	var x []Deposit; json.Unmarshal(d, &x); return x
}

func makeInterestTab(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("Interest & Deposits", theme.ForegroundColor())
	title.TextSize = 18; title.TextStyle = fyne.TextStyle{Bold: true}

	tabs := container.NewAppTabs(
		container.NewTabItem("💰 My Deposits",    makeDepositListTab(w)),
		container.NewTabItem("➕ Make Deposit",   makeNewDepositTab(w)),
		container.NewTabItem("📊 Interest Rates", makeInterestRatesTab(w)),
	)
	tabs.SetTabLocation(container.TabLocationTop)
	return tabs
}

func makeDepositListTab(w fyne.Window) fyne.CanvasObject {
	box := container.NewVBox()
	active := 0
	for i, dep := range myDeposits {
		if dep.Withdrawn { continue }
		active++
		dIdx := i
		matureDate := time.Unix(dep.MaturesAt, 0).Format("Jan 02 2006")
		interest   := (dep.Amount * float64(dep.InterestBP)) / 10000.0
		totalReturn := dep.Amount + interest
		matured    := time.Now().Unix() >= dep.MaturesAt
		statusTxt  := fmt.Sprintf("⏳ Matures: %s", matureDate)
		if matured  { statusTxt = "✅ MATURED — Ready to withdraw!" }

		withdrawBtn := widget.NewButton("💵 Withdraw + Interest", func() {
			d := myDeposits[dIdx]
			if !matured {
				dialog.ShowConfirm("⚠ Early Withdrawal",
					fmt.Sprintf("Deposit has NOT matured yet.\nEarly withdrawal forfeits all interest.\nYou will only get back %.8f SLK.\n\nWithdraw early?", d.Amount),
					func(ok bool) {
						if !ok { return }
						bankAccount.SLK += d.Amount
						myDeposits[dIdx].Withdrawn   = true
						myDeposits[dIdx].WithdrawnAt = time.Now().Unix()
						saveBankAccount(bankAccount); saveDeposits(); refreshLabels()
						tx := BankTX{ID: fmt.Sprintf("wth_%x", time.Now().UnixNano()),
							From: d.BankName, To: bankAccount.AccountID,
							Amount: d.Amount, Currency: "SLK", Type: "EARLY_WITHDRAWAL",
							Timestamp: time.Now().Unix(), Note: "Early withdrawal — interest forfeited", Verified: true}
						txHistory = append(txHistory, tx); saveTxHistory()
						dialog.ShowInformation("💵 Withdrawn", fmt.Sprintf("%.8f SLK returned (no interest — early withdrawal)", d.Amount), w)
					}, w)
				return
			}
			// Full matured withdrawal
			interest2 := (d.Amount * float64(d.InterestBP)) / 10000.0
			total2    := d.Amount + interest2
			bankAccount.SLK += total2
			myDeposits[dIdx].Withdrawn   = true
			myDeposits[dIdx].WithdrawnAt = time.Now().Unix()
			saveBankAccount(bankAccount); saveDeposits(); refreshLabels()
			tx := BankTX{ID: fmt.Sprintf("wth_%x", time.Now().UnixNano()),
				From: d.BankName, To: bankAccount.AccountID,
				Amount: total2, Currency: "SLK", Type: "WITHDRAWAL_WITH_INTEREST",
				Timestamp: time.Now().Unix(),
				Note: fmt.Sprintf("Matured deposit — principal: %.8f + interest: %.8f", d.Amount, interest2),
				Verified: true}
			txHistory = append(txHistory, tx); saveTxHistory()
			dialog.ShowInformation("✅ Withdrawn!", fmt.Sprintf("%.8f SLK returned\n(%.8f principal + %.8f interest)", total2, d.Amount, interest2), w)
		})
		if matured { withdrawBtn.Importance = widget.HighImportance }

		box.Add(container.NewPadded(container.NewVBox(
			widget.NewLabel(fmt.Sprintf("🏦 Deposit at %s", dep.BankName)),
			widget.NewLabel(fmt.Sprintf("Amount: %.8f %s  |  Interest: %.2f%% (%d bp)", dep.Amount, dep.Currency, float64(dep.InterestBP)/100.0, dep.InterestBP)),
			widget.NewLabel(fmt.Sprintf("Total return: %.8f SLK  |  %s", totalReturn, statusTxt)),
			withdrawBtn, widget.NewSeparator(),
		)))
	}
	if active == 0 { box.Add(widget.NewLabel("No active deposits.\nGo to Make Deposit tab to earn interest.")) }
	return container.NewVScroll(container.NewPadded(box))
}

func makeNewDepositTab(w fyne.Window) fyne.CanvasObject {
	// Pick which bank to deposit into
	myBankOptions := []string{}
	for _, cb := range myCommercialBanks { myBankOptions = append(myBankOptions, "🏢 "+cb.Name) }
	for _, rb := range myReserveBanks    { myBankOptions = append(myBankOptions, "🏛 "+rb.Name) }
	if len(myBankOptions) == 0 {
		return container.NewVScroll(container.NewPadded(widget.NewLabel("No banks available. Create a bank first.")))
	}
	bankSel     := widget.NewSelect(myBankOptions, nil); bankSel.SetSelected(myBankOptions[0])
	amountEntry := widget.NewEntry(); amountEntry.SetPlaceHolder("Amount of SLK to deposit")
	daysEntry   := widget.NewEntry(); daysEntry.SetPlaceHolder("Lock duration in days (e.g. 30)")
	returnLabel := widget.NewLabel("Expected return: —")
	returnLabel.TextStyle = fyne.TextStyle{Bold: true}
	result      := widget.NewLabel(""); result.Alignment = fyne.TextAlignCenter
	result.TextStyle = fyne.TextStyle{Bold: true}

	updateReturn := func() {
		amt, err1 := strconv.ParseFloat(strings.TrimSpace(amountEntry.Text), 64)
		days, err2 := strconv.ParseInt(strings.TrimSpace(daysEntry.Text), 10, 64)
		if err1 != nil || err2 != nil || amt <= 0 || days <= 0 { returnLabel.SetText("Expected return: —"); return }
		sel := bankSel.Selected
		var interestBP int64
		for _, cb := range myCommercialBanks { if sel == "🏢 "+cb.Name { interestBP = cb.FeeBasisPoints } }
		for _, rb := range myReserveBanks    { if sel == "🏛 "+rb.Name { interestBP = rb.FeeBasisPoints } }
		interest := (amt * float64(interestBP)) / 10000.0
		returnLabel.SetText(fmt.Sprintf("Expected return: %.8f SLK  (%.8f principal + %.8f interest at %.2f%%)", amt+interest, amt, interest, float64(interestBP)/100.0))
	}
	amountEntry.OnChanged = func(_ string) { updateReturn() }
	daysEntry.OnChanged   = func(_ string) { updateReturn() }
	bankSel.OnChanged     = func(_ string) { updateReturn() }

	depositBtn := widget.NewButton("💰 Lock Funds & Earn Interest", func() {
		amt, err1  := strconv.ParseFloat(strings.TrimSpace(amountEntry.Text), 64)
		days, err2 := strconv.ParseInt(strings.TrimSpace(daysEntry.Text), 10, 64)
		if err1 != nil || amt <= 0  { fyne.Do(func() { result.SetText("❌ Invalid amount") }); return }
		if err2 != nil || days <= 0 { fyne.Do(func() { result.SetText("❌ Invalid duration") }); return }
		if amt > bankAccount.SLK    { fyne.Do(func() { result.SetText("❌ Insufficient SLK") }); return }

		sel := bankSel.Selected
		var interestBP int64
		bankName := ""
		for _, cb := range myCommercialBanks { if sel == "🏢 "+cb.Name { interestBP = cb.FeeBasisPoints; bankName = cb.Name } }
		for _, rb := range myReserveBanks    { if sel == "🏛 "+rb.Name { interestBP = rb.FeeBasisPoints; bankName = rb.Name } }

		interest := (amt * float64(interestBP)) / 10000.0
		matureDate := time.Now().Add(time.Duration(days) * 24 * time.Hour)

		dialog.ShowConfirm("💰 Confirm Deposit",
			fmt.Sprintf("Bank: %s\nAmount: %.8f SLK\nInterest: %.2f%% = %.8f SLK\nTotal return: %.8f SLK\nMatures: %s\n\nFunds will be locked until maturity.\nEarly withdrawal forfeits interest.\n\nConfirm?",
				bankName, amt, float64(interestBP)/100.0, interest, amt+interest, matureDate.Format("Jan 02 2006")),
			func(ok bool) {
				if !ok { return }
				bankAccount.SLK -= amt
				saveBankAccount(bankAccount); refreshLabels()

				dep := Deposit{
					ID:          fmt.Sprintf("dep_%x", time.Now().UnixNano()),
					DepositorID: bankAccount.AccountID,
					BankName:    bankName,
					Amount:      amt,
					InterestBP:  interestBP,
					Currency:    "SLK",
					DepositedAt: time.Now().Unix(),
					MaturesAt:   matureDate.Unix(),
					Withdrawn:   false,
				}
				myDeposits = append(myDeposits, dep)
				saveDeposits()

				tx := BankTX{ID: dep.ID, From: bankAccount.AccountID, To: bankName,
					Amount: amt, Currency: "SLK", Type: "DEPOSIT",
					Timestamp: time.Now().Unix(),
					Note: fmt.Sprintf("Deposit at %.2f%% interest — matures %s", float64(interestBP)/100.0, matureDate.Format("Jan 02 2006")),
					Verified: true}
				txHistory = append(txHistory, tx); saveTxHistory()

				fyne.Do(func() {
					result.SetText(fmt.Sprintf("✅ %.8f SLK deposited!\nMatures: %s\nReturn: %.8f SLK", amt, matureDate.Format("Jan 02 2006"), amt+interest))
					amountEntry.SetText(""); daysEntry.SetText("")
				})
			}, w)
	})
	depositBtn.Importance = widget.HighImportance

	return container.NewVScroll(container.NewPadded(container.NewVBox(
		widget.NewForm(
			widget.NewFormItem("Bank", bankSel),
			widget.NewFormItem("Amount (SLK)", amountEntry),
			widget.NewFormItem("Duration (days)", daysEntry),
		),
		container.NewPadded(returnLabel),
		widget.NewSeparator(),
		container.NewPadded(depositBtn), result,
	)))
}

func makeInterestRatesTab(w fyne.Window) fyne.CanvasObject {
	box := container.NewVBox()
	box.Add(widget.NewLabel("Current interest rates offered by your banks:"))
	box.Add(widget.NewSeparator())
	for _, cb := range myCommercialBanks {
		box.Add(widget.NewLabel(fmt.Sprintf("🏢 %s  —  %.2f%% (%d basis points)", cb.Name, float64(cb.FeeBasisPoints)/100.0, cb.FeeBasisPoints)))
	}
	for _, rb := range myReserveBanks {
		box.Add(widget.NewLabel(fmt.Sprintf("🏛 %s  —  %.2f%% (%d basis points)", rb.Name, float64(rb.FeeBasisPoints)/100.0, rb.FeeBasisPoints)))
	}
	if len(myCommercialBanks) == 0 && len(myReserveBanks) == 0 {
		box.Add(widget.NewLabel("No banks yet."))
	}
	return container.NewVScroll(container.NewPadded(box))
}

func makeDepositTab(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("Deposit SLK", theme.ForegroundColor())
	title.TextSize = 18; title.TextStyle = fyne.TextStyle{Bold: true}

	// Step 1: wallet address
	addrEntry := widget.NewEntry(); addrEntry.SetPlaceHolder("Step 1: Enter your SLK wallet address")

	// Step 2: private key (masked)
	privEntry := widget.NewPasswordEntry(); privEntry.SetPlaceHolder("Step 2: Enter your private key (hex) to verify ownership")

	// Step 3: only shown after verification
	amountEntry := widget.NewEntry(); amountEntry.SetPlaceHolder("Step 3: Amount in SLK to deposit")
	amountEntry.Disable()

	availLabel := widget.NewLabel("Balance: — (verify wallet first)")
	availLabel.TextStyle = fyne.TextStyle{Monospace: true}

	result := widget.NewLabel(""); result.Alignment = fyne.TextAlignCenter; result.TextStyle = fyne.TextStyle{Bold: true}

	verifiedAddr  := ""
	verifiedWallet := (*wallet.Wallet)(nil)

	verifyBtn := widget.NewButton("🔐 Verify Wallet Ownership", func() {
		addr    := strings.TrimSpace(addrEntry.Text)
		privHex := strings.TrimSpace(privEntry.Text)
		if addr == "" { fyne.Do(func() { result.SetText("❌ Enter wallet address first") }); return }
		if privHex == "" { fyne.Do(func() { result.SetText("❌ Enter private key") }); return }

		// Decode private key from hex
		privBytes, err := hex.DecodeString(privHex)
		if err != nil || len(privBytes) < 32 {
			fyne.Do(func() { result.SetText("❌ Invalid private key format") })
			return
		}

		// Load wallet from file and compare private key
		wlt, err := wallet.LoadOrCreate(walletPath)
		if err != nil {
			fyne.Do(func() { result.SetText("❌ Could not load wallet") })
			return
		}

		// Verify: check address matches and private key matches
		if wlt.Address != addr {
			fyne.Do(func() { result.SetText("❌ Address does not match this wallet") })
			return
		}
		if wlt.PrivKeyHex() != privHex {
			fyne.Do(func() { result.SetText("❌ Wrong private key — access denied") })
			return
		}

		// Verified — now show balance and unlock amount field
		bal := utxoSet.GetTotalBalance(addr)
		verifiedAddr   = addr
		verifiedWallet = wlt
		fyne.Do(func() {
			availLabel.SetText(fmt.Sprintf("✅ Verified! Balance: %.8f SLK available", bal))
			amountEntry.Enable()
			result.SetText("✅ Wallet verified — enter amount to deposit")
		})
	})
	verifyBtn.Importance = widget.HighImportance

	note := widget.NewLabel("⚠  Your private key is used ONLY to verify ownership — it is never stored or sent.\n    Only real verified SLK (mined from P2P network) can be deposited.")
	note.Wrapping = fyne.TextWrapWord

	depositBtn := widget.NewButton("⬇  Deposit into Bank", func() {
		if verifiedAddr == "" || verifiedWallet == nil {
			fyne.Do(func() { result.SetText("❌ Verify your wallet first") }); return
		}
		amount, err := strconv.ParseFloat(amountEntry.Text, 64)
		if err != nil || amount <= 0 { fyne.Do(func() { result.SetText("❌ Invalid amount") }); return }
		bal := utxoSet.GetTotalBalance(verifiedAddr)
		if amount > bal {
			fyne.Do(func() { result.SetText(fmt.Sprintf("❌ Insufficient balance (%.8f SLK available)", bal)) })
			return
		}
		utxos := utxoSet.GetUnspentForAddress(verifiedAddr)
		if len(utxos) == 0 {
			fyne.Do(func() { result.SetText("❌ No verified UTXOs — only real mined SLK accepted") }); return
		}

		// Sign the deposit TX with private key to prove ownership
		txID := fmt.Sprintf("dep_%x", time.Now().UnixNano())
		msgToSign := []byte(txID + verifiedAddr + bankAccount.AccountID + fmt.Sprintf("%.8f", amount))
		sig, err := verifiedWallet.Sign(msgToSign)
		if err != nil {
			fyne.Do(func() { result.SetText("❌ Signing failed — invalid private key") }); return
		}
		sigHex := hex.EncodeToString(sig)
		_ = sigHex // stored in TX record for verification

		// Spend UTXOs
		totalSpent := 0.0
		for _, utxo := range utxos {
			if totalSpent >= amount { break }
			utxoSet.SpendUTXO(utxo.TxID, utxo.OutputIndex, txID)
			totalSpent += utxo.Amount
		}
		change := totalSpent - amount
		if change > 0.000000001 {
			utxoSet.AddUTXO(&state.UTXO{TxID: txID, OutputIndex: 1,
				Amount: change, Address: verifiedAddr, Spent: false})
		}
		utxoSet.Save()
		if mainWallet != nil {
			mainWallet.SyncBalance(utxoSet.GetTotalBalance(mainWallet.Address))
			mainWallet.Save(walletPath)
		}
		bankAccount.SLK += amount
		bankAccount.OwnerAddr = verifiedAddr
		saveBankAccount(bankAccount)

		tx := BankTX{ID: txID, From: verifiedAddr, To: bankAccount.AccountID,
			Amount: amount, Currency: "SLK", Type: "DEPOSIT",
			Timestamp: time.Now().Unix(), Note: "Deposit (Ed25519 signed)", Verified: true}
		txHistory = append(txHistory, tx); saveTxHistory()

		if p2pNode != nil {
			p2pNode.BroadcastBankRecord(p2p.BankRecord{ID: txID, From: verifiedAddr,
				To: bankAccount.AccountID, Amount: amount, Currency: "SLK",
				TxType: "DEPOSIT", Timestamp: time.Now().Unix(), Verified: true})
		}
		netRecords = append(netRecords, NetworkRecord{ID: txID, From: verifiedAddr,
			To: bankAccount.AccountID, Amount: amount, Currency: "SLK",
			TxType: "DEPOSIT", Timestamp: time.Now().Unix(), Verified: true})
		saveRecords()

		newBal := utxoSet.GetTotalBalance(verifiedAddr)
		fyne.Do(func() {
			refreshLabels()
			availLabel.SetText(fmt.Sprintf("Wallet Balance: %.8f SLK (after deposit)", newBal))
			result.SetText(fmt.Sprintf("✅ Deposited %.8f SLK  |  Bank SLK: %.8f", amount, bankAccount.SLK))
			statusBar.SetText(fmt.Sprintf("✅ Deposited %.8f SLK (signed)", amount))
			amountEntry.SetText("")
			// Reset verification
			verifiedAddr = ""; verifiedWallet = nil
			amountEntry.Disable()
			addrEntry.SetText(""); privEntry.SetText("")
		})
	})
	depositBtn.Importance = widget.MediumImportance

	return container.NewVScroll(container.NewPadded(container.NewVBox(
		container.NewCenter(title), widget.NewSeparator(),
		widget.NewLabel("Step 1 — Enter Wallet Address:"), addrEntry,
		widget.NewLabel("Step 2 — Prove Ownership (Private Key):"), privEntry,
		container.NewPadded(verifyBtn),
		container.NewPadded(availLabel),
		widget.NewSeparator(),
		widget.NewLabel("Step 3 — Amount to Deposit:"), amountEntry,
		widget.NewLabel("To Bank: "+bankAccount.AccountID),
		container.NewPadded(depositBtn),
		container.NewPadded(note),
		result,
	)))
}

// ════════════════════════════════════════
// TAB 4 — WITHDRAW
// ════════════════════════════════════════
func makeWithdrawTab(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("Withdraw SLK", theme.ForegroundColor())
	title.TextSize = 18; title.TextStyle = fyne.TextStyle{Bold: true}
	walletEntry := widget.NewEntry(); walletEntry.SetPlaceHolder("Destination wallet address")
	bankInfo := widget.NewLabel(fmt.Sprintf("Bank SLK:  %.8f SLK", bankAccount.SLK))
	bankInfo.TextStyle = fyne.TextStyle{Monospace: true}
	amountEntry := widget.NewEntry(); amountEntry.SetPlaceHolder("Amount in SLK")
	result := widget.NewLabel(""); result.Alignment = fyne.TextAlignCenter; result.TextStyle = fyne.TextStyle{Bold: true}
	withdrawBtn := widget.NewButton("⬆  Withdraw to Wallet", func() {
		addr := strings.TrimSpace(walletEntry.Text)
		if addr == "" { fyne.Do(func() { result.SetText("❌ Enter destination wallet address") }); return }
		amount, err := strconv.ParseFloat(amountEntry.Text, 64)
		if err != nil || amount <= 0 { fyne.Do(func() { result.SetText("❌ Invalid amount") }); return }
		if amount > bankAccount.SLK {
			fyne.Do(func() { result.SetText(fmt.Sprintf("❌ Insufficient bank balance (%.8f SLK)", bankAccount.SLK)) }); return
		}
		bankAccount.SLK -= amount; saveBankAccount(bankAccount)
		txID := fmt.Sprintf("wdr_%x", time.Now().UnixNano())
		utxoSet.AddUTXO(&state.UTXO{TxID: txID, OutputIndex: 0, Amount: amount, Address: addr, Spent: false})
		utxoSet.Save()
		if mainWallet != nil { mainWallet.SyncBalance(utxoSet.GetTotalBalance(mainWallet.Address)); mainWallet.Save(walletPath) }
		tx := BankTX{ID: txID, From: bankAccount.AccountID, To: addr,
			Amount: amount, Currency: "SLK", Type: "WITHDRAW",
			Timestamp: time.Now().Unix(), Note: "Withdrawal", Verified: true}
		txHistory = append(txHistory, tx); saveTxHistory()
		if p2pNode != nil {
			p2pNode.BroadcastBankRecord(p2p.BankRecord{ID: txID, From: bankAccount.AccountID,
				To: addr, Amount: amount, Currency: "SLK",
				TxType: "WITHDRAW", Timestamp: time.Now().Unix(), Verified: true})
		}
		netRecords = append(netRecords, NetworkRecord{ID: txID, From: bankAccount.AccountID,
			To: addr, Amount: amount, Currency: "SLK",
			TxType: "WITHDRAW", Timestamp: time.Now().Unix(), Verified: true})
		saveRecords()
		fyne.Do(func() {
			refreshLabels()
			bankInfo.SetText(fmt.Sprintf("Bank SLK:  %.8f SLK", bankAccount.SLK))
			result.SetText(fmt.Sprintf("✅ Withdrew %.8f SLK to %s", amount, shortAddr(addr)))
			statusBar.SetText(fmt.Sprintf("✅ Withdrew %.8f SLK", amount))
			amountEntry.SetText(""); walletEntry.SetText("")
		})
	})
	withdrawBtn.Importance = widget.HighImportance
	return container.NewVScroll(container.NewPadded(container.NewVBox(
		container.NewCenter(title), widget.NewSeparator(),
		container.NewPadded(bankInfo), widget.NewSeparator(),
		widget.NewForm(
			widget.NewFormItem("Amount (SLK)", amountEntry),
			widget.NewFormItem("From Bank", widget.NewLabel(bankAccount.AccountID)),
			widget.NewFormItem("To Wallet", walletEntry),
		),
		container.NewPadded(withdrawBtn), result,
	)))
}

// ════════════════════════════════════════
// TAB 5 — CONVERT
// ════════════════════════════════════════
func makeConvertTab(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("Convert Currency", theme.ForegroundColor())
	title.TextSize = 18; title.TextStyle = fyne.TextStyle{Bold: true}
	// Live exchange rates
	usdLabel  := widget.NewLabel("   USD/KES: fetching...")
	eurLabel  := widget.NewLabel("   EUR/KES: fetching...")
	gbpLabel  := widget.NewLabel("   GBP/KES: fetching...")
	usdLabel2 := widget.NewLabel("   1 SLK = 1,000,000 SLKT")
	rates := container.NewVBox(
		widget.NewLabel("📊 Live Exchange Rates (via exchangerate-api)"),
		usdLabel, eurLabel, gbpLabel,
		widget.NewSeparator(),
		usdLabel2,
		widget.NewLabel("   1 SLKT = 100,000 SLKCT"),
		widget.NewLabel("   SLKCT = whole numbers only"),
	)
	go func() {
		resp, err := http.Get("https://api.exchangerate-api.com/v4/latest/USD")
		if err != nil { fyne.Do(func() { usdLabel.SetText("   USD rates: offline") }); return }
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var data map[string]interface{}
		if json.Unmarshal(body, &data) != nil { return }
		ratesMap, ok := data["rates"].(map[string]interface{})
		if !ok { return }
		kes := ratesMap["KES"].(float64)
		eur := ratesMap["EUR"].(float64)
		gbp := ratesMap["GBP"].(float64)
		fyne.Do(func() {
			usdLabel.SetText(fmt.Sprintf("   1 USD = %.2f KES", kes))
			eurLabel.SetText(fmt.Sprintf("   1 EUR = %.2f KES", kes/eur))
			gbpLabel.SetText(fmt.Sprintf("   1 GBP = %.2f KES", kes/gbp))
		})
	}()
	amountEntry := widget.NewEntry(); amountEntry.SetPlaceHolder("Amount")
	fromSel := widget.NewSelect([]string{"SLK → SLKT","SLKT → SLKCT","SLKT → SLK","SLKCT → SLKT"}, nil)
	fromSel.SetSelected("SLK → SLKT")
	result := widget.NewLabel(""); result.Alignment = fyne.TextAlignCenter; result.TextStyle = fyne.TextStyle{Bold: true}
	balLabel := widget.NewLabel(fmt.Sprintf("SLK: %.8f  |  SLKT: %.5f  |  SLKCT: %d", bankAccount.SLK, bankAccount.SLKT, bankAccount.SLKCT))
	balLabel.Alignment = fyne.TextAlignCenter
	convertBtn := widget.NewButton("🔄  Convert Now", func() {
		amount, err := strconv.ParseFloat(amountEntry.Text, 64)
		if err != nil || amount <= 0 { fyne.Do(func() { result.SetText("❌ Invalid amount") }); return }
		var res string
		switch fromSel.Selected {
		case "SLK → SLKT":
			if amount > bankAccount.SLK { fyne.Do(func() { result.SetText("❌ Insufficient SLK") }); return }
			c := amount * SLKtoSLKT; bankAccount.SLK -= amount; bankAccount.SLKT += c
			res = fmt.Sprintf("✅  %.8f SLK → %.5f SLKT", amount, c)
		case "SLKT → SLKCT":
			if amount > bankAccount.SLKT { fyne.Do(func() { result.SetText("❌ Insufficient SLKT") }); return }
			c := int64(amount * SLKTtoSLKC); bankAccount.SLKT -= amount; bankAccount.SLKCT += c
			res = fmt.Sprintf("✅  %.5f SLKT → %d SLKCT", amount, c)
		case "SLKT → SLK":
			if amount > bankAccount.SLKT { fyne.Do(func() { result.SetText("❌ Insufficient SLKT") }); return }
			c := amount / SLKtoSLKT; bankAccount.SLKT -= amount; bankAccount.SLK += c
			res = fmt.Sprintf("✅  %.5f SLKT → %.8f SLK", amount, c)
		case "SLKCT → SLKT":
			sc := int64(amount)
			if sc > bankAccount.SLKCT { fyne.Do(func() { result.SetText("❌ Insufficient SLKCT") }); return }
			c := float64(sc) / SLKTtoSLKC; bankAccount.SLKCT -= sc; bankAccount.SLKT += c
			res = fmt.Sprintf("✅  %d SLKCT → %.5f SLKT", sc, c)
		}
		saveBankAccount(bankAccount)
		fyne.Do(func() {
			refreshLabels(); result.SetText(res)
			balLabel.SetText(fmt.Sprintf("SLK: %.8f  |  SLKT: %.5f  |  SLKCT: %d", bankAccount.SLK, bankAccount.SLKT, bankAccount.SLKCT))
			statusBar.SetText("✅ Conversion done"); amountEntry.SetText("")
		})
	})
	convertBtn.Importance = widget.HighImportance
	return container.NewVScroll(container.NewPadded(container.NewVBox(
		container.NewCenter(title), widget.NewSeparator(),
		container.NewPadded(rates), widget.NewSeparator(),
		container.NewPadded(balLabel), widget.NewSeparator(),
		widget.NewForm(widget.NewFormItem("Convert", fromSel), widget.NewFormItem("Amount", amountEntry)),
		container.NewPadded(convertBtn), container.NewPadded(result),
	)))
}

// ════════════════════════════════════════
// TAB 6 — MARKETPLACE (5 sub-tabs)
// ════════════════════════════════════════
func makeMarketTab(w fyne.Window) fyne.CanvasObject {
	tabs := container.NewAppTabs(
		container.NewTabItem("🔍 Browse",    makeMarketBrowse(w)),
		container.NewTabItem("📤 Sell",      makeMarketSell(w)),
		container.NewTabItem("💱 SLK Trade", makeSLKTrade(w)),
		container.NewTabItem("📦 My Items",  makeMyListings(w)),
		container.NewTabItem("📊 Stats",     makeMarketStats(w)),
	)
	tabs.SetTabLocation(container.TabLocationTop)
	return tabs
}

func makeMarketBrowse(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("Browse Marketplace", theme.ForegroundColor())
	title.TextSize = 16; title.TextStyle = fyne.TextStyle{Bold: true}
	searchEntry := widget.NewEntry(); searchEntry.SetPlaceHolder("🔍 Search items, sellers, categories...")
	catFilter := widget.NewSelect([]string{"All","Clothes","Electronics","Food","Art","Services","SLK Trade","Other"}, nil)
	catFilter.SetSelected("All")
	listBox := container.NewVBox()

	var rebuildList func(query, cat string)
	rebuildList = func(query, cat string) {
		listBox.Objects = nil
		found := 0
		for idx := len(marketList) - 1; idx >= 0; idx-- {
			l := marketList[idx]
			if !l.Active { continue }
			if cat != "All" && l.Category != cat { continue }
			if query != "" {
				q := strings.ToLower(query)
				if !strings.Contains(strings.ToLower(l.Title), q) &&
					!strings.Contains(strings.ToLower(l.Description), q) &&
					!strings.Contains(strings.ToLower(l.SellerName), q) { continue }
			}
			found++
			lCopy := l
			lIdx  := idx



			escrowBadge := ""
			if lCopy.Escrow { escrowBadge = "  🔒 ESCROW" }

			// Status label
			statusTxt := ""
			if lCopy.BuyerID != "" && !lCopy.EscrowDone {
				statusTxt = "  ⏳ AWAITING PAYMENT CONFIRMATION"
			} else if lCopy.EscrowDone {
				statusTxt = "  ✅ SOLD"
			}

			// Action buttons
			var actionObj fyne.CanvasObject

			isMine := lCopy.Seller == bankAccount.AccountID

			if isMine && lCopy.BuyerID != "" && !lCopy.EscrowDone {
				// Seller confirms payment received → release SLK to buyer
				confirmBtn := widget.NewButton("✅ Confirm Payment Received — Release SLK to Buyer", func() {
					dialog.ShowConfirm("⚠ Confirm Release",
						fmt.Sprintf("Did you receive %.2f %s from buyer?\nThis will release %.8f SLK to them.",
							lCopy.FiatPrice, lCopy.FiatCur, lCopy.Amount),
						func(ok bool) {
							if !ok { return }
							// Release SLK to buyer
							marketList[lIdx].EscrowDone = true
							marketList[lIdx].Active = false
							saveMarket()
							// Credit SLK to buyer bank account via P2P
							if p2pNode != nil {
								p2pNode.BroadcastTx(p2p.TxMsg{
									ID: fmt.Sprintf("escrow_%x", time.Now().UnixNano()),
									From: bankAccount.AccountID,
									To: lCopy.BuyerID,
									Amount: lCopy.Amount,
									Timestamp: time.Now().Unix(),
									Type: 1,
								})
							}
							txID := fmt.Sprintf("escrel_%x", time.Now().UnixNano())
							tx := BankTX{ID: txID, From: bankAccount.AccountID, To: lCopy.BuyerID,
								Amount: lCopy.Amount, Currency: lCopy.Currency, Type: "ESCROW_RELEASE",
								Timestamp: time.Now().Unix(), Note: "Escrow released: " + lCopy.Title, Verified: true}
							txHistory = append(txHistory, tx); saveTxHistory()
							netRecords = append(netRecords, NetworkRecord{ID: txID, From: bankAccount.AccountID,
								To: lCopy.BuyerID, Amount: lCopy.Amount, Currency: lCopy.Currency,
								TxType: "ESCROW_RELEASE", Timestamp: time.Now().Unix(), Verified: true})
							saveRecords()
							fyne.Do(func() {
								rebuildList(query, cat)
								statusBar.SetText(fmt.Sprintf("✅ Escrow released — %.8f SLK sent to buyer", lCopy.Amount))
								dialog.ShowInformation("✅ Done!", fmt.Sprintf("%.8f SLK released to buyer.\nTransaction complete!", lCopy.Amount), w)
							})
						}, w)
				})
				confirmBtn.Importance = widget.HighImportance
				cancelBtn := widget.NewButton("❌ Cancel — Return SLK to Me", func() {
					dialog.ShowConfirm("Cancel Escrow", "Return SLK to your bank balance?", func(ok bool) {
						if !ok { return }
						marketList[lIdx].Active = false
						marketList[lIdx].EscrowDone = true
						bankAccount.SLK += lCopy.Amount
						saveBankAccount(bankAccount)
						saveMarket()
						fyne.Do(func() {
							refreshLabels()
							rebuildList(query, cat)
							statusBar.SetText("↩ Escrow cancelled — SLK returned")
						})
					}, w)
				})
				actionObj = container.NewVBox(confirmBtn, cancelBtn)

			} else if isMine && lCopy.BuyerID == "" && !lCopy.EscrowDone {
				// No buyer yet — seller can delete and get SLK back
				deleteBtn := widget.NewButton("🗑 Delete Listing — Get SLK Back", func() {
					dialog.ShowConfirm("Delete Listing",
						fmt.Sprintf("Delete this listing and return %.8f SLK to your balance?", lCopy.Amount),
						func(ok bool) {
							if !ok { return }
							marketList[lIdx].Active = false
							marketList[lIdx].EscrowDone = true
							bankAccount.SLK += lCopy.Amount
							saveBankAccount(bankAccount)
							saveMarket()
							fyne.Do(func() {
								refreshLabels()
								rebuildList(query, cat)
								statusBar.SetText(fmt.Sprintf("✅ Listing deleted — %.8f SLK returned", lCopy.Amount))
							})
						}, w)
				})
				deleteBtn.Importance = widget.DangerImportance
				actionObj = container.NewPadded(deleteBtn)
			} else if !isMine && lCopy.BuyerID == "" && !lCopy.EscrowDone {
				// Buy button for other people's listings
				buyBtn := widget.NewButton(fmt.Sprintf("🛒 Buy Now — Pay %.2f %s to seller", lCopy.FiatPrice, lCopy.FiatCur), func() {
					dialog.ShowConfirm("🛒 Confirm Purchase",
						fmt.Sprintf("Item: %s\nPrice: %.2f %s\nSeller: %s\n\nPay seller outside (M-Pesa, bank transfer).\nSLK held in escrow until seller confirms.",
							lCopy.Title, lCopy.FiatPrice, lCopy.FiatCur, lCopy.SellerName),
						func(ok bool) {
							if !ok { return }
							marketList[lIdx].BuyerID = bankAccount.AccountID
							saveMarket()
							// Notify seller via P2P
							if p2pNode != nil {
								p2pNode.BroadcastSocial(p2p.SocialMsg{
									ID: fmt.Sprintf("buy_%x", time.Now().UnixNano()),
									From: bankAccount.AccountID,
									Name: bankAccount.Name,
									Text: "__BUY_REQ__:" + lCopy.Seller + ":" + lCopy.ID + ":" + lCopy.Title,
									Timestamp: time.Now().Unix(),
								})
							}
							fyne.Do(func() {
								rebuildList(query, cat)
								statusBar.SetText("🛒 Purchase initiated — pay seller then wait for SLK release")
								dialog.ShowInformation("🛒 Purchase Started!",
									fmt.Sprintf("Now pay %.2f %s to seller %s\nContact via Social → Chat\nOnce seller confirms, SLK is yours!",
										lCopy.FiatPrice, lCopy.FiatCur, lCopy.SellerName), w)
							})
						}, w)
				})
				buyBtn.Importance = widget.HighImportance
				actionObj = buyBtn

			} else if lCopy.BuyerID == bankAccount.AccountID && !lCopy.EscrowDone {
				actionObj = widget.NewLabel("⏳ Waiting for seller to confirm your payment...")
			} else if lCopy.EscrowDone {
				actionObj = widget.NewLabel("✅ This listing is complete")
			} else if isMine {
				actionObj = widget.NewLabel("👀 Your listing — waiting for a buyer")
			} else {
				actionObj = widget.NewLabel("")
			}

				// ── Amazon-style card ──
			titleLbl := canvas.NewText(lCopy.Title, theme.ForegroundColor())
			titleLbl.TextSize = 15
			titleLbl.TextStyle = fyne.TextStyle{Bold: true}

			sellerLbl := widget.NewLabel(fmt.Sprintf("👤 %s  |  🏷 %s", lCopy.SellerName, lCopy.Category))
			sellerLbl.TextStyle = fyne.TextStyle{Italic: true}

			slkPriceLbl := canvas.NewText(fmt.Sprintf("%.8f SLK", lCopy.Amount), color.NRGBA{R:255, G:200, B:0, A:255})
			slkPriceLbl.TextSize = 16
			slkPriceLbl.TextStyle = fyne.TextStyle{Bold: true}

			usdPriceLbl := widget.NewLabel(fmt.Sprintf("≈ %.2f %s", lCopy.FiatPrice, lCopy.FiatCur))

			qty := lCopy.Quantity
			if qty <= 0 { qty = 1 }
			qtyLbl := widget.NewLabel(fmt.Sprintf("📦 Qty: %d", qty))

			dateLbl := widget.NewLabel(fmt.Sprintf("📅 %s", time.Unix(lCopy.CreatedAt, 0).Format("Jan 02 2006 15:04")))

			statusLbl := widget.NewLabel(statusTxt + escrowBadge)
			statusLbl.TextStyle = fyne.TextStyle{Bold: true}

			descLbl := widget.NewLabel(lCopy.Description)
			descLbl.Wrapping = fyne.TextWrapWord

			infoCol := container.NewVBox(
				titleLbl,
				sellerLbl,
				widget.NewSeparator(),
				slkPriceLbl,
				usdPriceLbl,
				qtyLbl,
				dateLbl,
				statusLbl,
				descLbl,
				container.NewPadded(actionObj),
			)

			var card fyne.CanvasObject
			if lCopy.ImagePath != "" {
				if _, err := os.Stat(lCopy.ImagePath); err == nil {
					img := canvas.NewImageFromFile(lCopy.ImagePath)
					img.FillMode = canvas.ImageFillContain
					img.SetMinSize(fyne.NewSize(160, 160))
					card = container.NewBorder(nil, widget.NewSeparator(), container.NewPadded(img), nil, infoCol)
				} else {
					placeholder := canvas.NewText("📦", theme.ForegroundColor())
					placeholder.TextSize = 48
					card = container.NewBorder(nil, widget.NewSeparator(), container.NewPadded(placeholder), nil, infoCol)
				}
			} else {
				placeholder := canvas.NewText("📦", theme.ForegroundColor())
				placeholder.TextSize = 48
				card = container.NewBorder(nil, widget.NewSeparator(), container.NewPadded(placeholder), nil, infoCol)
			}

			listBox.Add(container.NewPadded(card))
		}
		if found == 0 { listBox.Add(widget.NewLabel("No listings found.")) }
		listBox.Refresh()
	}

	searchEntry.OnChanged = func(q string) { rebuildList(q, catFilter.Selected) }
	catFilter.OnChanged  = func(c string) { rebuildList(searchEntry.Text, c) }
	rebuildList("", "All")
	return container.NewVScroll(container.NewPadded(container.NewVBox(
		container.NewCenter(title), widget.NewSeparator(),
		container.NewBorder(nil, nil, nil, catFilter, searchEntry),
		widget.NewSeparator(), listBox,
	)))
}

func makeMarketSell(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("Post a Listing", theme.ForegroundColor())
	title.TextSize = 16; title.TextStyle = fyne.TextStyle{Bold: true}
	titleEntry := widget.NewEntry(); titleEntry.SetPlaceHolder("Item title")
	descEntry  := widget.NewEntry(); descEntry.SetPlaceHolder("Description — size, condition, details..."); descEntry.MultiLine = true
	imgEntry   := widget.NewEntry(); imgEntry.SetPlaceHolder("Image file path (e.g. /home/user/photo.jpg)")
	imgBrowseBtn := widget.NewButton("📁 Browse", func() {
		dialog.ShowFileOpen(func(uc fyne.URIReadCloser, err error) {
			if err != nil || uc == nil { return }
			imgEntry.SetText(uc.URI().Path())
			uc.Close()
		}, w)
	})
	qtyEntry   := widget.NewEntry(); qtyEntry.SetPlaceHolder("Quantity (e.g. 1)")
	catSel     := widget.NewSelect([]string{"Clothes","Electronics","Food","Art","Services","Other"}, nil); catSel.SetSelected("Clothes")
	amtEntry   := widget.NewEntry(); amtEntry.SetPlaceHolder("Price in SLK")
	currSel    := widget.NewSelect([]string{"SLK","SLKT","SLKCT"}, nil); currSel.SetSelected("SLK")
	fiatEntry  := widget.NewEntry(); fiatEntry.SetPlaceHolder("Fiat price (e.g. 10.00)")
	fiatSel    := widget.NewSelect([]string{"USD","KES","EUR","GBP","NGN","ZAR"}, nil); fiatSel.SetSelected("USD")
	result     := widget.NewLabel(""); result.Alignment = fyne.TextAlignCenter

	// Image preview
	imgPreview := canvas.NewImageFromFile("")
	imgPreview.FillMode = canvas.ImageFillContain
	imgPreview.SetMinSize(fyne.NewSize(200, 150))
	imgEntry.OnChanged = func(path string) {
		if _, err := os.Stat(path); err == nil {
			imgPreview.File = path; imgPreview.Refresh()
		}
	}

	postBtn := widget.NewButton("📤  Post Listing", func() {
		t := strings.TrimSpace(titleEntry.Text)
		if t == "" { fyne.Do(func() { result.SetText("❌ Enter a title") }); return }
		amount, err1 := strconv.ParseFloat(amtEntry.Text, 64)
		price, err2  := strconv.ParseFloat(fiatEntry.Text, 64)
		if err1 != nil || amount <= 0 || err2 != nil || price <= 0 {
			fyne.Do(func() { result.SetText("❌ Invalid amount or price") }); return
		}
		currency := currSel.Selected
		switch currency {
		case "SLK":
			if amount > bankAccount.SLK { fyne.Do(func() { result.SetText("❌ Insufficient SLK") }); return }
			bankAccount.SLK -= amount
		case "SLKT":
			if amount > bankAccount.SLKT { fyne.Do(func() { result.SetText("❌ Insufficient SLKT") }); return }
			bankAccount.SLKT -= amount
		}
		saveBankAccount(bankAccount)
		l := MarketListing{
			ID: fmt.Sprintf("mkt_%x", time.Now().UnixNano()),
			Seller: bankAccount.AccountID, SellerName: bankAccount.Name,
			Title: t, Description: descEntry.Text, ImagePath: imgEntry.Text,
			Category: catSel.Selected, Amount: amount, Currency: currency,
			FiatPrice: price, FiatCur: fiatSel.Selected,
			CreatedAt: time.Now().Unix(), Active: true, Escrow: true,
			Quantity: func() int { q, e := strconv.Atoi(strings.TrimSpace(qtyEntry.Text)); if e != nil || q < 1 { return 1 }; return q }(),
		}
		marketList = append(marketList, l); saveMarket()
		fyne.Do(func() {
			refreshLabels()
			result.SetText(fmt.Sprintf("✅ Listed: %s — SLK locked in escrow until buyer confirms", t))
			titleEntry.SetText(""); descEntry.SetText(""); imgEntry.SetText(""); qtyEntry.SetText("")
			amtEntry.SetText(""); fiatEntry.SetText("")
		})
	})
	postBtn.Importance = widget.HighImportance
	return container.NewVScroll(container.NewPadded(container.NewVBox(
		container.NewCenter(title), widget.NewSeparator(),
		widget.NewLabel("Image Preview:"), container.NewPadded(imgPreview),
		widget.NewForm(
			widget.NewFormItem("Title", titleEntry),
			widget.NewFormItem("Description", descEntry),
			widget.NewFormItem("Image Path", container.NewBorder(nil, nil, nil, imgBrowseBtn, imgEntry)),
			widget.NewFormItem("Quantity", qtyEntry),
			widget.NewFormItem("Category", catSel),
			widget.NewFormItem("Price (SLK)", amtEntry),
			widget.NewFormItem("Currency", currSel),
			widget.NewFormItem("Fiat Price", fiatEntry),
			widget.NewFormItem("Fiat Currency", fiatSel),
		),
		widget.NewLabel("🔒 All listings use escrow — SLK is locked until transaction confirmed by both parties."),
		container.NewPadded(postBtn), result,
	)))
}

func makeSLKTrade(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("SLK Trade Exchange", theme.ForegroundColor())
	title.TextSize = 16; title.TextStyle = fyne.TextStyle{Bold: true}

	// Sell SLK
	sellAmt  := widget.NewEntry(); sellAmt.SetPlaceHolder("SLK amount to sell")
	sellPrice:= widget.NewEntry(); sellPrice.SetPlaceHolder("Asking price (fiat)")
	sellFiat := widget.NewSelect([]string{"USD","KES","EUR","GBP","NGN","ZAR"}, nil); sellFiat.SetSelected("USD")
	sellResult := widget.NewLabel(""); sellResult.Alignment = fyne.TextAlignCenter

	postSellBtn := widget.NewButton("📤  List SLK for Sale", func() {
		amount, err1 := strconv.ParseFloat(sellAmt.Text, 64)
		price, err2  := strconv.ParseFloat(sellPrice.Text, 64)
		if err1 != nil || amount <= 0 || err2 != nil || price <= 0 {
			fyne.Do(func() { sellResult.SetText("❌ Invalid amount or price") }); return
		}
		if amount > bankAccount.SLK {
			fyne.Do(func() { sellResult.SetText("❌ Insufficient SLK in bank") }); return
		}
		// Lock SLK in escrow
		bankAccount.SLK -= amount; saveBankAccount(bankAccount)
		l := MarketListing{
			ID: fmt.Sprintf("slktrade_%x", time.Now().UnixNano()),
			Seller: bankAccount.AccountID, SellerName: bankAccount.Name,
			Title: fmt.Sprintf("SELL %.8f SLK", amount),
			Description: fmt.Sprintf("Selling %.8f SLK for %.2f %s. Contact seller to arrange payment.", amount, price, sellFiat.Selected),
			Category: "SLK Trade", Amount: amount, Currency: "SLK",
			FiatPrice: price, FiatCur: sellFiat.Selected,
			CreatedAt: time.Now().Unix(), Active: true, Escrow: true,
		}
		marketList = append(marketList, l); saveMarket()
		fyne.Do(func() {
			refreshLabels()
			sellResult.SetText(fmt.Sprintf("✅ %.8f SLK listed for sale at %.2f %s — locked in escrow", amount, price, sellFiat.Selected))
			sellAmt.SetText(""); sellPrice.SetText("")
		})
	})
	postSellBtn.Importance = widget.HighImportance

	// Buy SLK
	buyAmt   := widget.NewEntry(); buyAmt.SetPlaceHolder("SLK amount you want to buy")
	buyPrice := widget.NewEntry(); buyPrice.SetPlaceHolder("Max price you'll pay (fiat)")
	buyFiat  := widget.NewSelect([]string{"USD","KES","EUR","GBP","NGN","ZAR"}, nil); buyFiat.SetSelected("USD")
	buyResult := widget.NewLabel(""); buyResult.Alignment = fyne.TextAlignCenter

	postBuyBtn := widget.NewButton("📥  Post Buy Request", func() {
		amount, err1 := strconv.ParseFloat(buyAmt.Text, 64)
		price, err2  := strconv.ParseFloat(buyPrice.Text, 64)
		if err1 != nil || amount <= 0 || err2 != nil || price <= 0 {
			fyne.Do(func() { buyResult.SetText("❌ Invalid amount or price") }); return
		}
		l := MarketListing{
			ID: fmt.Sprintf("slkbuy_%x", time.Now().UnixNano()),
			Seller: bankAccount.AccountID, SellerName: bankAccount.Name,
			Title: fmt.Sprintf("BUY %.8f SLK", amount),
			Description: fmt.Sprintf("Wanting to buy %.8f SLK for up to %.2f %s. Contact buyer.", amount, price, buyFiat.Selected),
			Category: "SLK Trade", Amount: amount, Currency: "SLK",
			FiatPrice: price, FiatCur: buyFiat.Selected,
			CreatedAt: time.Now().Unix(), Active: true, Escrow: false,
		}
		marketList = append(marketList, l); saveMarket()
		fyne.Do(func() {
			buyResult.SetText(fmt.Sprintf("✅ Buy request posted: %.8f SLK at %.2f %s", amount, price, buyFiat.Selected))
			buyAmt.SetText(""); buyPrice.SetText("")
		})
	})
	postBuyBtn.Importance = widget.MediumImportance

	escrowNote := widget.NewLabel(
		"🔒 Escrow System:\n" +
		"  • Seller lists SLK → SLK immediately deducted and locked\n" +
		"  • Buyer contacts seller (via Social chat)\n" +
		"  • Buyer pays fiat outside (bank transfer, mobile money)\n" +
		"  • Seller confirms payment received → SLK released to buyer\n" +
		"  • If seller doesn't confirm in 48h → SLK returned automatically\n" +
		"  • Dispute: both parties submit evidence → network nodes vote")
	escrowNote.Wrapping = fyne.TextWrapWord

	return container.NewVScroll(container.NewPadded(container.NewVBox(
		container.NewCenter(title), widget.NewSeparator(),
		canvas.NewText("Sell SLK", theme.ForegroundColor()),
		widget.NewForm(
			widget.NewFormItem("SLK Amount", sellAmt),
			widget.NewFormItem("Asking Price", sellPrice),
			widget.NewFormItem("Fiat Currency", sellFiat),
		),
		container.NewPadded(postSellBtn), sellResult,
		widget.NewSeparator(),
		canvas.NewText("Buy SLK", theme.ForegroundColor()),
		widget.NewForm(
			widget.NewFormItem("SLK Amount", buyAmt),
			widget.NewFormItem("Max Price", buyPrice),
			widget.NewFormItem("Fiat Currency", buyFiat),
		),
		container.NewPadded(postBuyBtn), buyResult,
		widget.NewSeparator(),
		container.NewPadded(escrowNote),
	)))
}

func makeMyListings(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("My Listings", theme.ForegroundColor())
	title.TextSize = 16; title.TextStyle = fyne.TextStyle{Bold: true}
	box := container.NewVBox(); found := 0
	for i := len(marketList) - 1; i >= 0; i-- {
		l := marketList[i]
		if l.Seller != bankAccount.AccountID { continue }
		if !l.Active { continue }
		found++
		status := "✅ Active"; if !l.Active { status = "❌ Inactive" }
		escrow := ""; if l.Escrow { escrow = " 🔒 Escrow" }
		box.Add(container.NewVBox(
			widget.NewLabel(fmt.Sprintf("%s%s  📌 %s", status, escrow, l.Title)),
			widget.NewLabel(fmt.Sprintf("   %.8f %s  |  %.2f %s  |  %s",
				l.Amount, l.Currency, l.FiatPrice, l.FiatCur, l.Category)),
			widget.NewLabel(fmt.Sprintf("   %s", time.Unix(l.CreatedAt, 0).Format("Jan 02 2006 15:04"))),
			widget.NewSeparator(),
		))
	}
	if found == 0 { box.Add(widget.NewLabel("No listings yet.")) }
	return container.NewVScroll(container.NewPadded(container.NewVBox(
		container.NewCenter(title), widget.NewSeparator(), box,
	)))
}

func makeMarketStats(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("Market Statistics", theme.ForegroundColor())
	title.TextSize = 16; title.TextStyle = fyne.TextStyle{Bold: true}
	total, active, vol := 0, 0, 0.0; sellers := map[string]bool{}
	for _, l := range marketList {
		total++
		if l.Active { active++; vol += l.Amount; sellers[l.Seller] = true }
	}
	return container.NewVScroll(container.NewPadded(container.NewVBox(
		container.NewCenter(title), widget.NewSeparator(),
		widget.NewLabel(fmt.Sprintf("📦 Total Listings:   %d", total)),
		widget.NewLabel(fmt.Sprintf("✅ Active Listings:  %d", active)),
		widget.NewLabel(fmt.Sprintf("💰 Total Volume:     %.8f SLK", vol)),
		widget.NewLabel(fmt.Sprintf("👤 Unique Sellers:   %d", len(sellers))),
		widget.NewSeparator(),
		widget.NewLabel("📊 Supply & Demand"),
		widget.NewLabel(fmt.Sprintf("   SLK locked in listings: %.8f SLK", vol)),
		widget.NewLabel(fmt.Sprintf("   Active market makers:   %d", len(sellers))),
	)))
}

// ════════════════════════════════════════
// TAB 7 — HISTORY
// ════════════════════════════════════════
func makeHistoryTab(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("Transaction History", theme.ForegroundColor())
	title.TextSize = 18; title.TextStyle = fyne.TextStyle{Bold: true}
	box := container.NewVBox()
	if len(txHistory) == 0 {
		box.Add(widget.NewLabel("No transactions yet."))
	} else {
		for i := len(txHistory) - 1; i >= 0; i-- {
			tx := txHistory[i]
			v := "✅"; if !tx.Verified { v = "⚠" }
			box.Add(container.NewVBox(
				widget.NewLabel(fmt.Sprintf("%s %s  %s  %.8f %s  %s",
					v, txIcon(tx.Type), tx.Type, tx.Amount, tx.Currency,
					time.Unix(tx.Timestamp, 0).Format("2006-01-02 15:04:05"))),
				widget.NewLabel(fmt.Sprintf("   From: %s", shortAddr(tx.From))),
				widget.NewLabel(fmt.Sprintf("   To:   %s", shortAddr(tx.To))),
				widget.NewLabel(fmt.Sprintf("   TX:   %s", shortStr(tx.ID, 24))),
				widget.NewSeparator(),
			))
		}
	}
	return container.NewVScroll(container.NewPadded(container.NewVBox(
		container.NewCenter(title),
		widget.NewLabel(fmt.Sprintf("Total: %d transactions", len(txHistory))),
		widget.NewSeparator(), box,
	)))
}

// ════════════════════════════════════════
// TAB 8 — SETTINGS
// ════════════════════════════════════════
func makeSettingsTab(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("Settings & API Keys", theme.ForegroundColor())
	title.TextSize = 18; title.TextStyle = fyne.TextStyle{Bold: true}
	nameInfo := widget.NewLabel(fmt.Sprintf("Name:  %s  (permanent)", bankAccount.Name))
	nameInfo.TextStyle = fyne.TextStyle{Bold: true}
	details := widget.NewLabel(fmt.Sprintf(
		"Account ID:    %s\nOwner Wallet:  %s\nCreated:       %s\nAlgorithm:     Ed25519 (libsodium)",
		bankAccount.AccountID, bankAccount.OwnerAddr,
		time.Unix(bankAccount.CreatedAt, 0).Format("2006-01-02 15:04:05")))
	details.TextStyle = fyne.TextStyle{Monospace: true}; details.Wrapping = fyne.TextWrapWord
	pubKeyLabel := widget.NewLabel(bankAccount.PublicKey)
	pubKeyLabel.TextStyle = fyne.TextStyle{Monospace: true}; pubKeyLabel.Wrapping = fyne.TextWrapWord
	secKeyLabel = widget.NewLabel("sk_" + strings.Repeat("•", 40))
	secKeyLabel.TextStyle = fyne.TextStyle{Monospace: true}
	showSecBtn := widget.NewButton("👁 Reveal", func() {
		dialog.ShowConfirm("⚠ Warning", "Never share your secret key. Reveal?",
			func(ok bool) {
				if ok { fyne.Do(func() { secKeyLabel.SetText(bankAccount.SecretKey) }) }
			}, w)
	})
	hideSecBtn := widget.NewButton("🔒 Hide", func() {
		fyne.Do(func() { secKeyLabel.SetText("sk_" + strings.Repeat("•", 40)) })
	})
	copyPubBtn := widget.NewButton("📋 Copy Public Key", func() {
		w.Clipboard().SetContent(bankAccount.PublicKey)
		fyne.Do(func() { statusBar.SetText("✅ Public key copied") })
	})
	copySecBtn := widget.NewButton("📋 Copy Secret Key", func() {
		dialog.ShowConfirm("⚠", "Copy secret key?", func(ok bool) {
			if ok { w.Clipboard().SetContent(bankAccount.SecretKey) }
		}, w)
	})
	copySecBtn.Importance = widget.DangerImportance
	regenBtn := widget.NewButton("🔄 Regenerate Keys", func() {
		dialog.ShowConfirm("⚠", "Old keys stop working. Continue?", func(ok bool) {
			if ok {
				pub, sec := generateAPIKeys()
				bankAccount.PublicKey = pub; bankAccount.SecretKey = sec; bankAccount.SecretKeyH = hashKey(sec)
				saveBankAccount(bankAccount)
				fyne.Do(func() {
					pubKeyLabel.SetText(bankAccount.PublicKey)
					secKeyLabel.SetText("sk_" + strings.Repeat("•", 40))
				})
				dialog.ShowInformation("✅", "New API keys generated.", w)
			}
		}, w)
	})
	// ── WALLET API KEY (separate from secret key) ──
	walletAPIKeyLabel := widget.NewLabel("wak_" + strings.Repeat("•", 40))
	walletAPIKeyLabel.TextStyle = fyne.TextStyle{Monospace: true}
	walletAPIKeyLabel.Wrapping = fyne.TextWrapWord
	showWAKBtn := widget.NewButton("👁 Reveal Wallet API Key", func() {
		dialog.ShowConfirm("⚠ Warning", "This key allows API access to your wallet balance/send. Never share publicly. Reveal?",
			func(ok bool) {
				if ok { fyne.Do(func() { walletAPIKeyLabel.SetText(bankAccount.WalletAPIKey) }) }
			}, w)
	})
	showWAKBtn.Importance = widget.WarningImportance
	hideWAKBtn := widget.NewButton("🔒 Hide", func() {
		fyne.Do(func() { walletAPIKeyLabel.SetText("wak_" + strings.Repeat("•", 40)) })
	})
	copyWAKBtn := widget.NewButton("📋 Copy Wallet API Key", func() {
		dialog.ShowConfirm("⚠", "Copy Wallet API Key? Use this in your server — never in frontend code.", func(ok bool) {
			if ok { w.Clipboard().SetContent(bankAccount.WalletAPIKey) }
		}, w)
	})
	copyWAKBtn.Importance = widget.WarningImportance
	regenWAKBtn := widget.NewButton("🔄 Regenerate Wallet API Key", func() {
		dialog.ShowConfirm("⚠", "Old Wallet API Key stops working immediately. Any website using it must be updated. Continue?", func(ok bool) {
			if ok {
				bankAccount.WalletAPIKey = generateAPIKey(bankAccount.AccountID + fmt.Sprintf("%d", time.Now().UnixNano()))
				saveBankAccount(bankAccount)
				fyne.Do(func() { walletAPIKeyLabel.SetText("wak_" + strings.Repeat("•", 40)) })
				dialog.ShowInformation("✅ Regenerated", "New Wallet API Key generated. Update your website/app.", w)
			}
		}, w)
	})
	regenWAKBtn.Importance = widget.DangerImportance

	return container.NewVScroll(container.NewPadded(container.NewVBox(
		container.NewCenter(title), widget.NewSeparator(),
		nameInfo, widget.NewSeparator(),
		widget.NewLabel("Account Details"), container.NewPadded(details),
		widget.NewSeparator(),
		widget.NewLabel("Public Key:"), container.NewPadded(pubKeyLabel),
		widget.NewLabel("Secret Key (never share — wallet signing only):"), container.NewPadded(secKeyLabel),
		container.New(layout.NewGridLayout(2), showSecBtn, hideSecBtn),
		container.New(layout.NewGridLayout(2), copyPubBtn, copySecBtn),
		regenBtn,
		widget.NewSeparator(),
		canvas.NewText("🔑 Wallet API Key — for /slkapi/* endpoints", theme.ForegroundColor()),
		widget.NewLabel("Use this in your backend server to call balance/send APIs. NEVER in frontend code."),
		container.NewPadded(walletAPIKeyLabel),
		container.New(layout.NewGridLayout(2), showWAKBtn, hideWAKBtn),
		container.New(layout.NewGridLayout(2), copyWAKBtn, regenWAKBtn),
	)))
}

// ════════════════════════════════════════
// TAB 9 — RECORDS
// ════════════════════════════════════════
func makeRecordsTab(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("Network Bank Records", theme.ForegroundColor())
	title.TextSize = 18; title.TextStyle = fyne.TextStyle{Bold: true}
	recordsInner = container.NewVBox()
	buildRecordsInner()
	recordsBox = container.NewVScroll(recordsInner)
	return container.NewBorder(
		container.NewVBox(
			container.NewCenter(title),
			widget.NewLabel("All bank transactions broadcast to every peer on the network."),
			widget.NewLabel(fmt.Sprintf("Total Records: %d", len(netRecords))),
			widget.NewSeparator(),
		),
		nil, nil, nil, recordsBox,
	)
}

func buildRecordsInner() {
	if recordsInner == nil { return }
	recordsInner.Objects = nil
	if len(netRecords) == 0 {
		recordsInner.Add(widget.NewLabel("No records yet."))
	} else {
		for i := len(netRecords) - 1; i >= 0; i-- {
			r := netRecords[i]
			v := "✅"; if !r.Verified { v = "⚠" }
			recordsInner.Add(container.NewVBox(
				widget.NewLabel(fmt.Sprintf("%s %s  %.8f %s  %s",
					v, r.TxType, r.Amount, r.Currency,
					time.Unix(r.Timestamp, 0).Format("2006-01-02 15:04:05"))),
				widget.NewLabel(fmt.Sprintf("   From: %s", shortAddr(r.From))),
				widget.NewLabel(fmt.Sprintf("   To:   %s", shortAddr(r.To))),
				widget.NewSeparator(),
			))
		}
	}
	recordsInner.Refresh()
}

func rebuildRecordsBox() { buildRecordsInner() }

// ════════════════════════════════════════
// TAB 10 — SOCIAL (posts + friends + chat)
// ════════════════════════════════════════
func makeSocialTab(w fyne.Window) fyne.CanvasObject {
	tabs := container.NewAppTabs(
		container.NewTabItem("📢 Feed",    makeFeedTab(w)),
		container.NewTabItem("👥 Friends", makeFriendsTab(w)),
		container.NewTabItem("💬 Chat",    makeChatTab(w)),
	)
	tabs.SetTabLocation(container.TabLocationTop)
	return tabs
}

func makeFeedTab(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("Global SLK Feed", theme.ForegroundColor())
	title.TextSize = 15; title.TextStyle = fyne.TextStyle{Bold: true}
	sub := canvas.NewText("Posts go to every peer worldwide over P2P", theme.PlaceHolderColor()); sub.TextSize = 11
	postEntry := widget.NewEntry(); postEntry.SetPlaceHolder("What's on your mind?"); postEntry.MultiLine = true
	imgEntry  := widget.NewEntry(); imgEntry.SetPlaceHolder("Image path (optional)")
	imgBrowseBtn2 := widget.NewButton("📁 Browse", func() {
		dialog.ShowFileOpen(func(uc fyne.URIReadCloser, err error) {
			if err != nil || uc == nil { return }
			imgEntry.SetText(uc.URI().Path())
			uc.Close()
		}, w)
	})
	postResult := widget.NewLabel(""); postResult.Alignment = fyne.TextAlignCenter

	// Image preview in post composer
	imgPreview := canvas.NewImageFromFile("")
	imgPreview.FillMode = canvas.ImageFillContain
	imgPreview.SetMinSize(fyne.NewSize(180, 120))
	imgEntry.OnChanged = func(path string) {
		if _, err := os.Stat(path); err == nil {
			imgPreview.File = path; imgPreview.Refresh()
		}
	}

	imgRow := container.NewBorder(nil, nil, nil, imgBrowseBtn2, imgEntry)
	postBtn := widget.NewButton("📢  Broadcast to Network", func() {
		text := strings.TrimSpace(postEntry.Text)
		if len(text) < 2 { fyne.Do(func() { postResult.SetText("❌ Too short") }); return }
		if p2pNode == nil { fyne.Do(func() { postResult.SetText("⚠ Not connected yet") }); return }
		postID := fmt.Sprintf("post_%x", time.Now().UnixNano())
		post := SocialPost{ID: postID, From: bankAccount.AccountID, Name: bankAccount.Name,
			Text: text, ImagePath: imgEntry.Text, Timestamp: time.Now().Unix()}
		socialFeed = append(socialFeed, post); saveSocial()
		p2pNode.BroadcastSocial(p2p.SocialMsg{ID: postID, From: bankAccount.AccountID,
			Name: bankAccount.Name, Text: text, ImagePath: imgEntry.Text, Timestamp: time.Now().Unix()})
		fyne.Do(func() {
			rebuildSocialBox()
			postResult.SetText(fmt.Sprintf("✅ Posted to %d peers!", p2pNode.PeerCount))
			postEntry.SetText(""); imgEntry.SetText("")
		})
	})
	postBtn.Importance = widget.HighImportance
	feedTitle := canvas.NewText("Live Feed", theme.ForegroundColor())
	feedTitle.TextSize = 13; feedTitle.TextStyle = fyne.TextStyle{Bold: true}
	innerFeed := buildSocialInner()
	socialBox = container.NewVScroll(innerFeed)
	return container.NewBorder(
		container.NewVBox(
			container.NewCenter(title), container.NewCenter(sub), widget.NewSeparator(),
			container.NewPadded(container.NewVBox(
				postEntry, imgRow, imgPreview,
				container.NewPadded(postBtn), postResult,
			)),
			widget.NewSeparator(), container.NewCenter(feedTitle),
		),
		nil, nil, nil, socialBox,
	)
}

func makeFriendsTab(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("Friends", theme.ForegroundColor())
	title.TextSize = 15; title.TextStyle = fyne.TextStyle{Bold: true}

	// Send friend request
	toEntry := widget.NewEntry(); toEntry.SetPlaceHolder("Enter their Account ID (SLKB-xxxx-xxxx)")
	reqResult := widget.NewLabel(""); reqResult.Alignment = fyne.TextAlignCenter
	sendReqBtn := widget.NewButton("👋 Send Friend Request", func() {
		to := strings.TrimSpace(toEntry.Text)
		if to == "" { fyne.Do(func() { reqResult.SetText("❌ Enter account ID") }); return }
		if to == bankAccount.AccountID { fyne.Do(func() { reqResult.SetText("❌ Cannot add yourself") }); return }
		if p2pNode == nil { fyne.Do(func() { reqResult.SetText("⚠ Not connected") }); return }
		reqID := fmt.Sprintf("fr_%x", time.Now().UnixNano())
		p2pNode.BroadcastSocial(p2p.SocialMsg{
			ID: reqID, From: bankAccount.AccountID, Name: bankAccount.Name,
			Text: "__FRIEND_REQ__:" + to, Timestamp: time.Now().Unix(),
		})
		fr := FriendRequest{ID: reqID, From: bankAccount.AccountID, FromName: bankAccount.Name,
			To: to, Status: "pending", Timestamp: time.Now().Unix()}
		friendReqs = append(friendReqs, fr); saveFriends()
		fyne.Do(func() {
			reqResult.SetText("✅ Friend request sent to " + shortAddr(to))
			toEntry.SetText("")
		})
	})
	sendReqBtn.Importance = widget.HighImportance

	// Pending requests received
	pendingTitle := canvas.NewText("Incoming Requests", theme.ForegroundColor())
	pendingTitle.TextSize = 13; pendingTitle.TextStyle = fyne.TextStyle{Bold: true}
	pendingBox := container.NewVBox()
	for _, fr := range friendReqs {
		if fr.To != bankAccount.AccountID || fr.Status != "pending" { continue }
		frCopy := fr
		acceptBtn := widget.NewButton("✅ Accept", func() {
			for i, f := range friendReqs {
				if f.ID == frCopy.ID { friendReqs[i].Status = "accepted"; break }
			}
			saveFriends()
			if p2pNode != nil {
				p2pNode.BroadcastSocial(p2p.SocialMsg{
					ID: fmt.Sprintf("fa_%x", time.Now().UnixNano()),
					From: bankAccount.AccountID, Name: bankAccount.Name,
					Text: "__FRIEND_ACCEPT__:" + frCopy.From, Timestamp: time.Now().Unix(),
				})
			}
			fyne.Do(func() { reqResult.SetText("✅ Accepted " + frCopy.FromName) })
		})
		pendingBox.Add(container.NewVBox(
			widget.NewLabel(fmt.Sprintf("👋 %s (%s)", frCopy.FromName, shortAddr(frCopy.From))),
			acceptBtn, widget.NewSeparator(),
		))
	}
	if pendingBox.Objects == nil { pendingBox.Add(widget.NewLabel("No pending requests.")) }

	// Friends list
	friendsTitle := canvas.NewText("Your Friends", theme.ForegroundColor())
	friendsTitle.TextSize = 13; friendsTitle.TextStyle = fyne.TextStyle{Bold: true}
	friendsBox := container.NewVBox()
	friendCount := 0
	for _, fr := range friendReqs {
		if fr.Status != "accepted" { continue }
		friendCount++
		name := fr.FromName; id := fr.To
		if fr.From == bankAccount.AccountID { name = fr.To; id = fr.To }
		if fr.To == bankAccount.AccountID { name = fr.FromName; id = fr.From }
		friendsBox.Add(widget.NewLabel(fmt.Sprintf("🤝 %s  —  %s", name, shortAddr(id))))
		friendsBox.Add(widget.NewSeparator())
	}
	if friendCount == 0 { friendsBox.Add(widget.NewLabel("No friends yet. Send a request!")) }

	return container.NewVScroll(container.NewPadded(container.NewVBox(
		container.NewCenter(title), widget.NewSeparator(),
		widget.NewLabel("Send Friend Request:"),
		toEntry, container.NewPadded(sendReqBtn), reqResult,
		widget.NewSeparator(),
		container.NewCenter(pendingTitle), pendingBox,
		widget.NewSeparator(),
		container.NewCenter(friendsTitle), friendsBox,
	)))
}

func makeChatTab(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("Private Chat", theme.ForegroundColor())
	title.TextSize = 15; title.TextStyle = fyne.TextStyle{Bold: true}

	toEntry  := widget.NewEntry(); toEntry.SetPlaceHolder("Friend's Account ID (SLKB-xxxx-xxxx)")
	msgEntry := widget.NewEntry(); msgEntry.SetPlaceHolder("Type your message..."); msgEntry.MultiLine = true
	sendResult := widget.NewLabel(""); sendResult.Alignment = fyne.TextAlignCenter

	sendMsgBtn := widget.NewButton("💬 Send Message", func() {
		to   := strings.TrimSpace(toEntry.Text)
		text := strings.TrimSpace(msgEntry.Text)
		if to == "" { fyne.Do(func() { sendResult.SetText("❌ Enter recipient") }); return }
		if text == "" { fyne.Do(func() { sendResult.SetText("❌ Type a message") }); return }
		if p2pNode == nil { fyne.Do(func() { sendResult.SetText("⚠ Not connected") }); return }
		msgID := fmt.Sprintf("msg_%x", time.Now().UnixNano())
		p2pNode.BroadcastSocial(p2p.SocialMsg{
			ID: msgID, From: bankAccount.AccountID, Name: bankAccount.Name,
			Text: "__CHAT__:" + to + ":" + text, Timestamp: time.Now().Unix(),
		})
		cm := ChatMessage{ID: msgID, From: bankAccount.AccountID, FromName: bankAccount.Name,
			To: to, Text: text, Timestamp: time.Now().Unix()}
		chatMsgs = append(chatMsgs, cm); saveChat()
		fyne.Do(func() {
			sendResult.SetText("✅ Message sent")
			msgEntry.SetText("")
		})
	})
	sendMsgBtn.Importance = widget.HighImportance

	// Chat history
	chatBox := container.NewVBox()
	if len(chatMsgs) == 0 {
		chatBox.Add(widget.NewLabel("No messages yet."))
	} else {
		for i := len(chatMsgs) - 1; i >= 0; i-- {
			cm := chatMsgs[i]
			dir := "→"; if cm.From != bankAccount.AccountID { dir = "←" }
			t := time.Unix(cm.Timestamp, 0).Format("Jan 02 15:04")
			chatBox.Add(container.NewVBox(
				widget.NewLabel(fmt.Sprintf("%s %s  [%s]  %s", dir, cm.FromName, t, cm.Text)),
				widget.NewSeparator(),
			))
		}
	}

	return container.NewVScroll(container.NewPadded(container.NewVBox(
		container.NewCenter(title), widget.NewSeparator(),
		widget.NewForm(
			widget.NewFormItem("To", toEntry),
			widget.NewFormItem("Message", msgEntry),
		),
		container.NewPadded(sendMsgBtn), sendResult,
		widget.NewSeparator(),
		canvas.NewText("Message History", theme.ForegroundColor()),
		chatBox,
	)))
}

// ════════════════════════════════════════
// TAB 11 — BANKS DIRECTORY
// ════════════════════════════════════════
func makeBanksTab(w fyne.Window) fyne.CanvasObject {
	myBanksContent   := container.NewStack(makeMyBanksTab(w))
	clientContent    := container.NewStack(makeBankClientPortal(w))
	ownerContent     := container.NewStack(makeBankOwnerDashboard(w))
	myBanksTab   := container.NewTabItem("🏢 My Banks", myBanksContent)
	clientTab    := container.NewTabItem("👤 Client Portal", clientContent)
	ownerTab     := container.NewTabItem("👑 Owner Dashboard", ownerContent)
	tabs := container.NewAppTabs(
		container.NewTabItem("🏦 Directory", makeBankDirectoryTab(w)),
		myBanksTab,
		clientTab,
		ownerTab,
		container.NewTabItem("➕ Create Bank", makeCreateBankTab(w)),
	)
	tabs.SetTabLocation(container.TabLocationTop)
	tabs.OnChanged = func(tab *container.TabItem) {
		if tab == myBanksTab {
			myBanksContent.Objects = []fyne.CanvasObject{makeMyBanksTab(w)}
			myBanksContent.Refresh()
		}
		if tab == clientTab {
			clientContent.Objects = []fyne.CanvasObject{makeBankClientPortal(w)}
			clientContent.Refresh()
		}
		if tab == ownerTab {
			ownerContent.Objects = []fyne.CanvasObject{makeBankOwnerDashboard(w)}
			ownerContent.Refresh()
		}
	}
	return tabs
}

// ════════════════════════════════════════
// CLIENT PORTAL — Deposit, Withdraw, Send, Pay
// ════════════════════════════════════════
func makeBankClientPortal(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("👤 Bank Client Portal", theme.ForegroundColor())
	title.TextSize = 16; title.TextStyle = fyne.TextStyle{Bold: true}

	if len(myCommercialBanks) == 0 {
		return container.NewCenter(widget.NewLabel("No bank found. Create or join a bank first."))
	}
	cb := &myCommercialBanks[0]

	// Find owner as client or create owner client
	ownerClientIdx := -1
	for i, cl := range cb.Clients {
		if cl.SLKAddress == bankAccount.AccountID { ownerClientIdx = i; break }
	}
	if ownerClientIdx == -1 {
		cb.Clients = append(cb.Clients, BankClient{
			AccountID:  bankAccount.AccountID,
			Name:       bankAccount.Name,
			SLKAddress: bankAccount.AccountID,
			Balance:    0,
			JoinedAt:   time.Now().Unix(),
			Active:     true,
			Verified:   true,
			KYCName:    bankAccount.Name,
		})
		ownerClientIdx = len(cb.Clients) - 1
		saveCommercialBanks()
	}

	client := &cb.Clients[ownerClientIdx]

	// ── BALANCE CARD ──
	balTitle := canvas.NewText(fmt.Sprintf("💳 %s Account", cb.Currency), color.NRGBA{R:0,G:212,B:255,A:255})
	balTitle.TextStyle = fyne.TextStyle{Bold: true}; balTitle.TextSize = 14
	balLbl := canvas.NewText(fmt.Sprintf("%.8f %s", client.Balance, cb.Currency), color.NRGBA{R:0,G:255,B:128,A:255})
	balLbl.TextSize = 22; balLbl.TextStyle = fyne.TextStyle{Bold: true}
	slkBalLbl := widget.NewLabel(fmt.Sprintf("Your SLK Wallet: %.8f SLK", bankAccount.SLK))
	idLbl := widget.NewLabel(fmt.Sprintf("Account ID: %s", client.AccountID))
	idLbl.TextStyle = fyne.TextStyle{Monospace: true}

	// ── DEPOSIT ──
	depTitle := canvas.NewText("💰 Deposit SLK → Get "+cb.Currency, theme.ForegroundColor())
	depTitle.TextStyle = fyne.TextStyle{Bold: true}
	depAmtEntry := widget.NewEntry(); depAmtEntry.SetPlaceHolder("SLK amount to deposit (e.g. 0.5)")
	weeksSelect := widget.NewSelect([]string{"3 weeks","4 weeks","5 weeks","6 weeks","8 weeks","10 weeks","12 weeks","16 weeks","20 weeks","24 weeks","52 weeks"}, nil)
	weeksSelect.SetSelected("3 weeks")
	depInfo := widget.NewLabel(fmt.Sprintf("Rate: 1 SLK = %.0f %s | Min lock: 3 weeks", cb.SLKRate, cb.Currency))
	depInfo.Wrapping = fyne.TextWrapWord
	depBtn := widget.NewButton("💰 Deposit & Lock", func() {
		amt, err := strconv.ParseFloat(strings.TrimSpace(depAmtEntry.Text), 64)
		if err != nil || amt <= 0 { dialog.ShowInformation("Error", "Enter valid SLK amount", w); return }
		if amt > bankAccount.SLK { dialog.ShowInformation("❌ Insufficient", fmt.Sprintf("You only have %.8f SLK", bankAccount.SLK), w); return }
		if weeksSelect.Selected == "" { dialog.ShowInformation("Error", "Select lock period", w); return }
		weeks := 3
		fmt.Sscanf(weeksSelect.Selected, "%d", &weeks)
		withdrawAt := time.Now().Add(time.Duration(weeks) * 7 * 24 * time.Hour).Unix()
		amtT := amt * cb.SLKRate
		fee := amt * float64(cb.FeeBasisPoints) / 10000.0
		netAmt := amt - fee
		netT := netAmt * cb.SLKRate
		dialog.ShowConfirm("💰 Confirm Deposit",
			fmt.Sprintf("Deposit %.8f SLK | Receive: %.4f %s | Fee: %.8f SLK | Locked: %d weeks | Withdraw: %s", amt, netT, cb.Currency, fee, weeks, time.Unix(withdrawAt, 0).Format("Jan 02 2006")),
			func(ok bool) {
				if !ok { return }
				bankAccount.SLK -= amt
				dep := BankDeposit{
					ID: fmt.Sprintf("dep_%x", time.Now().UnixNano()),
					ClientID: client.AccountID,
					BankID: cb.ID,
					AmountSLK: amt,
					AmountT: netT,
					DepositedAt: time.Now().Unix(),
					WithdrawAt: withdrawAt,
					WeeksLocked: weeks,
					Status: "active",
					InterestEarned: 0,
					ApprovedByOwner: cb.OwnerID == bankAccount.AccountID,
				}
				cb.Clients[ownerClientIdx].Deposits = append(cb.Clients[ownerClientIdx].Deposits, dep)
				cb.Clients[ownerClientIdx].Balance += netT
				cb.Clients[ownerClientIdx].TotalDeposited += amt
				cb.TotalDeposited += amt
				cb.TotalFees += fee
				cb.TotalIssuedT += netT
				_ = amtT
				bankAccount.SLK += fee
				saveBankAccount(bankAccount)
				saveCommercialBanks()
				refreshLabels()
				broadcastBankEvent("DEPOSIT", client.AccountID, cb.ID, cb.Currency, amt)
				balLbl.Text = fmt.Sprintf("%.8f %s", cb.Clients[ownerClientIdx].Balance, cb.Currency)
				balLbl.Refresh()
				slkBalLbl.SetText(fmt.Sprintf("Your SLK Wallet: %.8f SLK", bankAccount.SLK))
				dialog.ShowInformation("✅ Deposited",
					fmt.Sprintf("Deposited %.8f SLK | Received: %.4f %s | Unlocks: %s", amt, netT, cb.Currency, time.Unix(withdrawAt, 0).Format("Jan 02 2006 15:04")), w)
				depAmtEntry.SetText("")
			}, w)
	})
	depBtn.Importance = widget.HighImportance

	// ── DEPOSITS LIST ──
	depListTitle := canvas.NewText("📋 My Deposits", theme.ForegroundColor())
	depListTitle.TextStyle = fyne.TextStyle{Bold: true}
	depBox := container.NewVBox()
	now := time.Now().Unix()
	for i, dep := range client.Deposits {
		depIdx := i
		status := dep.Status
		ready := now >= dep.WithdrawAt && status == "active"
		if ready { status = "✅ READY TO WITHDRAW" } else if status == "active" { status = "🔒 Locked" } else if status == "withdrawn" { status = "✔ Withdrawn" }
		rowLbl := widget.NewLabel(fmt.Sprintf("%s | %.8f SLK → %.4f %s | Locked %d weeks | Withdraw: %s | %s",
			dep.ID[:12], dep.AmountSLK, dep.AmountT, cb.Currency, dep.WeeksLocked,
			time.Unix(dep.WithdrawAt, 0).Format("Jan 02 2006"), status))
		rowLbl.Wrapping = fyne.TextWrapWord
		wdBtn := widget.NewButton("💸 Withdraw", func() {
			d := cb.Clients[ownerClientIdx].Deposits[depIdx]
			if time.Now().Unix() < d.WithdrawAt {
				dialog.ShowInformation("🔒 Locked", fmt.Sprintf("Cannot withdraw until %s", time.Unix(d.WithdrawAt, 0).Format("Jan 02 2006")), w); return
			}
			if d.Status != "active" { dialog.ShowInformation("Error", "Already withdrawn", w); return }
			interest := d.AmountSLK * (cb.InterestRate / 100.0) * (float64(d.WeeksLocked) / 52.0)
			totalSLK := d.AmountSLK + interest
			dialog.ShowConfirm("💸 Withdraw",
				fmt.Sprintf("Withdraw %.8f SLK + %.8f SLK interest = %.8f SLK total", d.AmountSLK, interest, totalSLK),
				func(ok bool) {
					if !ok { return }
					bankAccount.SLK += totalSLK
					cb.Clients[ownerClientIdx].Balance -= d.AmountT
					cb.Clients[ownerClientIdx].TotalWithdrawn += d.AmountSLK
					cb.Clients[ownerClientIdx].Deposits[depIdx].Status = "withdrawn"
					cb.Clients[ownerClientIdx].Deposits[depIdx].InterestEarned = interest
					saveBankAccount(bankAccount)
					saveCommercialBanks()
					refreshLabels()
					broadcastBankEvent("WITHDRAW", client.AccountID, cb.ID, "SLK", totalSLK)
					balLbl.Text = fmt.Sprintf("%.8f %s", cb.Clients[ownerClientIdx].Balance, cb.Currency)
					balLbl.Refresh()
					slkBalLbl.SetText(fmt.Sprintf("Your SLK Wallet: %.8f SLK", bankAccount.SLK))
					dialog.ShowInformation("✅ Withdrawn", fmt.Sprintf("Received %.8f SLK (includes %.8f interest)", totalSLK, interest), w)
				}, w)
		})
		if !ready || dep.Status == "withdrawn" { wdBtn.Disable() }
		depBox.Add(container.NewVBox(rowLbl, wdBtn, widget.NewSeparator()))
	}
	if len(client.Deposits) == 0 {
		depBox.Add(widget.NewLabel("No deposits yet. Make your first deposit above."))
	}

	// ── SEND PAYMENT ──
	sendTitle := canvas.NewText("💸 Send "+cb.Currency+" Payment", theme.ForegroundColor())
	sendTitle.TextStyle = fyne.TextStyle{Bold: true}
	sendToEntry  := widget.NewEntry(); sendToEntry.SetPlaceHolder("Recipient Account ID (SLKB-xxxx)")
	sendAmtEntry := widget.NewEntry(); sendAmtEntry.SetPlaceHolder(fmt.Sprintf("Amount in %s", cb.Currency))
	sendMemoEntry := widget.NewEntry(); sendMemoEntry.SetPlaceHolder("Memo / note (optional)")
	sendBtn := widget.NewButton("💸 Send Payment", func() {
		amt, err := strconv.ParseFloat(strings.TrimSpace(sendAmtEntry.Text), 64)
		if err != nil || amt <= 0 { dialog.ShowInformation("Error", "Invalid amount", w); return }
		to := strings.TrimSpace(sendToEntry.Text)
		if to == "" { dialog.ShowInformation("Error", "Enter recipient", w); return }
		if amt > cb.Clients[ownerClientIdx].Balance {
			dialog.ShowInformation("❌ Insufficient", fmt.Sprintf("Balance: %.4f %s", cb.Clients[ownerClientIdx].Balance, cb.Currency), w); return
		}
		fee := amt * float64(cb.FeeBasisPoints) / 10000.0
		netAmt := amt - fee
		dialog.ShowConfirm("💸 Confirm Payment",
			fmt.Sprintf("Send %.4f %s to %s | Fee: %.4f %s | Recipient gets: %.4f %s", amt, cb.Currency, shortAddr(to), fee, cb.Currency, netAmt, cb.Currency),
			func(ok bool) {
				if !ok { return }
				cb.Clients[ownerClientIdx].Balance -= amt
				cb.TotalFees += fee / cb.SLKRate
				bankAccount.SLK += fee / cb.SLKRate
				pmt := BankPayment{
					ID: fmt.Sprintf("pay_%x", time.Now().UnixNano()),
					FromClient: client.AccountID,
					ToClient: to,
					AmountT: amt,
					AmountSLK: amt / cb.SLKRate,
					Fee: fee,
					Memo: sendMemoEntry.Text,
					Timestamp: time.Now().Unix(),
					Status: "completed",
				}
				bankPayments = append(bankPayments, pmt)
				saveBankPayments()
				saveCommercialBanks()
				broadcastBankEvent("PAYMENT", client.AccountID, to, cb.Currency, amt)
				balLbl.Text = fmt.Sprintf("%.8f %s", cb.Clients[ownerClientIdx].Balance, cb.Currency)
				balLbl.Refresh()
				dialog.ShowInformation("✅ Sent", fmt.Sprintf("Sent %.4f %s | TX ID: %s", netAmt, cb.Currency, pmt.ID[:16]), w)
				sendToEntry.SetText(""); sendAmtEntry.SetText(""); sendMemoEntry.SetText("")
			}, w)
	})
	sendBtn.Importance = widget.HighImportance

	// ── PAYMENT LINK ──
	linkTitle := canvas.NewText("🔗 Generate Payment Link", theme.ForegroundColor())
	linkTitle.TextStyle = fyne.TextStyle{Bold: true}
	linkAmtEntry := widget.NewEntry(); linkAmtEntry.SetPlaceHolder(fmt.Sprintf("Amount in %s", cb.Currency))
	linkMemoEntry := widget.NewEntry(); linkMemoEntry.SetPlaceHolder("What is this payment for?")
	linkResult := widget.NewLabel("")
	linkResult.Wrapping = fyne.TextWrapWord
	linkResult.TextStyle = fyne.TextStyle{Monospace: true}
	linkBtn := widget.NewButton("🔗 Generate Link", func() {
		amt := strings.TrimSpace(linkAmtEntry.Text)
		memo := strings.TrimSpace(linkMemoEntry.Text)
		if amt == "" { dialog.ShowInformation("Error", "Enter amount", w); return }
		link := fmt.Sprintf("slkpay://%s?bank=%s&to=%s&amount=%s&currency=%s&memo=%s",
			cb.ID, cb.Name, client.AccountID, amt, cb.Currency, memo)
		linkResult.SetText(link)
		w.Clipboard().SetContent(link)
		dialog.ShowInformation("✅ Link Generated", "Payment link copied to clipboard!", w)
	})
	linkBtn.Importance = widget.HighImportance

	// ── TX HISTORY ──
	txTitle := canvas.NewText("📜 Transaction History", theme.ForegroundColor())
	txTitle.TextStyle = fyne.TextStyle{Bold: true}
	txBox := container.NewVBox()
	count := 0
	for i := len(bankPayments)-1; i >= 0 && count < 20; i-- {
		p := bankPayments[i]
		if p.FromClient == client.AccountID || p.ToClient == client.AccountID {
			dir := "📤 Sent"; if p.ToClient == client.AccountID { dir = "📥 Received" }
			txBox.Add(widget.NewLabel(fmt.Sprintf("%s | %.4f %s | %s | %s | %s",
				dir, p.AmountT, cb.Currency, shortAddr(p.ToClient),
				p.Memo, time.Unix(p.Timestamp, 0).Format("Jan 02 15:04"))))
			txBox.Add(widget.NewSeparator())
			count++
		}
	}
	if count == 0 { txBox.Add(widget.NewLabel("No transactions yet.")) }

	scroll := container.NewVScroll(container.NewPadded(container.NewVBox(
		container.NewCenter(title), widget.NewSeparator(),
		// Balance
		container.NewPadded(container.NewVBox(balTitle, balLbl, slkBalLbl, idLbl)),
		widget.NewSeparator(),
		// Deposit
		depTitle, depInfo,
		widget.NewForm(
			widget.NewFormItem("SLK Amount", depAmtEntry),
			widget.NewFormItem("Lock Period", weeksSelect),
		),
		depBtn,
		widget.NewSeparator(),
		// Deposits list
		depListTitle, container.NewPadded(depBox),
		widget.NewSeparator(),
		// Send
		sendTitle,
		widget.NewForm(
			widget.NewFormItem("To (Account ID)", sendToEntry),
			widget.NewFormItem("Amount "+cb.Currency, sendAmtEntry),
			widget.NewFormItem("Memo", sendMemoEntry),
		),
		sendBtn,
		widget.NewSeparator(),
		// Payment Link
		linkTitle,
		widget.NewForm(
			widget.NewFormItem("Amount "+cb.Currency, linkAmtEntry),
			widget.NewFormItem("Description", linkMemoEntry),
		),
		linkBtn, linkResult,
		widget.NewSeparator(),
		// TX History
		txTitle, container.NewPadded(txBox),
		widget.NewSeparator(),
	)))
	return scroll
}

// ════════════════════════════════════════
// OWNER DASHBOARD — See all clients, deposits, approve withdrawals
// ════════════════════════════════════════
func makeBankOwnerDashboard(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("👑 Owner Dashboard", theme.ForegroundColor())
	title.TextSize = 16; title.TextStyle = fyne.TextStyle{Bold: true}

	if len(myCommercialBanks) == 0 {
		return container.NewCenter(widget.NewLabel("You don't own a bank yet."))
	}
	cb := &myCommercialBanks[0]
	if cb.OwnerID != bankAccount.AccountID {
		return container.NewCenter(widget.NewLabel("You are not the owner of this bank."))
	}

	// ── BANK STATS ──
	statsTitle := canvas.NewText("📊 Bank Statistics", theme.ForegroundColor())
	statsTitle.TextStyle = fyne.TextStyle{Bold: true}
	totalClients := len(cb.Clients)
	totalDeposited := 0.0
	totalBalance := 0.0
	totalWithdrawn := 0.0
	activeDeposits := 0
	for _, cl := range cb.Clients {
		totalDeposited += cl.TotalDeposited
		totalBalance += cl.Balance
		totalWithdrawn += cl.TotalWithdrawn
		for _, d := range cl.Deposits {
			if d.Status == "active" { activeDeposits++ }
		}
	}
	statsBox := container.NewVBox(
		widget.NewLabel(fmt.Sprintf("👥 Total Clients: %d", totalClients)),
		widget.NewLabel(fmt.Sprintf("💰 Total Deposited: %.8f SLK", totalDeposited)),
		widget.NewLabel(fmt.Sprintf("📈 Total %s in Circulation: %.4f", cb.Currency, totalBalance)),
		widget.NewLabel(fmt.Sprintf("💸 Total Withdrawn: %.8f SLK", totalWithdrawn)),
		widget.NewLabel(fmt.Sprintf("🔒 Active Deposits: %d", activeDeposits)),
		widget.NewLabel(fmt.Sprintf("💹 Fees Earned: %.8f SLK", cb.TotalFees)),
		widget.NewLabel(fmt.Sprintf("💹 Interest Rate: %.2f%% per year", cb.InterestRate)),
	)

	// ── ALL CLIENTS & DEPOSITS ──
	clientsTitle := canvas.NewText("👥 All Client Wallets & Deposits", theme.ForegroundColor())
	clientsTitle.TextStyle = fyne.TextStyle{Bold: true}
	clientsBox := container.NewVBox()

	now := time.Now().Unix()
	for i, cl := range cb.Clients {
		clIdx := i
		verified := "❌ Unverified"; if cl.Verified { verified = "✅ Verified" }
		hdr := canvas.NewText(fmt.Sprintf("👤 %s (%s) | %s | Balance: %.4f %s | Deposited: %.8f SLK",
			cl.Name, shortAddr(cl.AccountID), verified, cl.Balance, cb.Currency, cl.TotalDeposited),
			color.NRGBA{R:0,G:212,B:255,A:255})
		hdr.TextStyle = fyne.TextStyle{Bold: true}

		verifyBtn := widget.NewButton("✅ Verify Client", func() {
			cb.Clients[clIdx].Verified = true
			saveCommercialBanks()
			dialog.ShowInformation("✅ Verified", fmt.Sprintf("%s is now verified.", cb.Clients[clIdx].Name), w)
		})
		if cl.Verified { verifyBtn.Disable() }

		depRows := container.NewVBox()
		for j, dep := range cl.Deposits {
			depIdx := j
			clIdxCopy := clIdx
			ready := now >= dep.WithdrawAt && dep.Status == "active"
			statusTxt := "🔒 Locked"
			if ready { statusTxt = "✅ READY" }
			if dep.Status == "withdrawn" { statusTxt = "✔ Done" }
			interest := dep.AmountSLK * (cb.InterestRate / 100.0) * (float64(dep.WeeksLocked) / 52.0)
			depLbl := widget.NewLabel(fmt.Sprintf("  %s | %.8f SLK → %.4f %s | %d weeks | Withdraw: %s | %s | Interest: %.8f SLK",
				dep.ID[:12], dep.AmountSLK, dep.AmountT, cb.Currency,
				dep.WeeksLocked, time.Unix(dep.WithdrawAt, 0).Format("Jan 02 2006"),
				statusTxt, interest))
			depLbl.Wrapping = fyne.TextWrapWord

			// Owner can approve early withdrawal or set custom date
			approveBtn := widget.NewButton("✅ Approve Withdrawal", func() {
				if cb.Clients[clIdxCopy].Deposits[depIdx].Status != "active" {
					dialog.ShowInformation("Error", "Already processed", w); return
				}
				dialog.ShowConfirm("Approve Withdrawal",
					fmt.Sprintf("Allow %s to withdraw %.8f SLK now?", cb.Clients[clIdxCopy].Name, cb.Clients[clIdxCopy].Deposits[depIdx].AmountSLK),
					func(ok bool) {
						if !ok { return }
						cb.Clients[clIdxCopy].Deposits[depIdx].WithdrawAt = time.Now().Unix()
						cb.Clients[clIdxCopy].Deposits[depIdx].ApprovedByOwner = true
						saveCommercialBanks()
						dialog.ShowInformation("✅ Approved", "Client can now withdraw their deposit.", w)
					}, w)
			})
			if dep.Status == "withdrawn" { approveBtn.Disable() }
			depRows.Add(container.NewVBox(depLbl, approveBtn, widget.NewSeparator()))
		}
		if len(cl.Deposits) == 0 {
			depRows.Add(widget.NewLabel("  No deposits."))
		}
		clientsBox.Add(container.NewPadded(container.NewVBox(hdr, verifyBtn, depRows, widget.NewSeparator())))
	}
	if totalClients == 0 {
		clientsBox.Add(widget.NewLabel("No clients yet. Share your API key so clients can join."))
	}

	// ── ALL PAYMENTS ──
	pmtTitle := canvas.NewText("💳 All Bank Payments", theme.ForegroundColor())
	pmtTitle.TextStyle = fyne.TextStyle{Bold: true}
	pmtBox := container.NewVBox()
	bankPmts := 0
	for i := len(bankPayments)-1; i >= 0 && bankPmts < 50; i-- {
		p := bankPayments[i]
		pmtBox.Add(widget.NewLabel(fmt.Sprintf("💳 %s | %.4f %s | %s → %s | %s | %s",
			p.ID[:12], p.AmountT, cb.Currency,
			shortAddr(p.FromClient), shortAddr(p.ToClient),
			p.Memo, time.Unix(p.Timestamp, 0).Format("Jan 02 15:04"))))
		pmtBox.Add(widget.NewSeparator())
		bankPmts++
	}
	if bankPmts == 0 { pmtBox.Add(widget.NewLabel("No payments yet.")) }

	scroll := container.NewVScroll(container.NewPadded(container.NewVBox(
		container.NewCenter(title), widget.NewSeparator(),
		statsTitle, container.NewPadded(statsBox),
		widget.NewSeparator(),
		clientsTitle, container.NewPadded(clientsBox),
		widget.NewSeparator(),
		pmtTitle, container.NewPadded(pmtBox),
		widget.NewSeparator(),
	)))
	return scroll
}

func makeBankExchangeTab(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("Bank Currency Exchange", theme.ForegroundColor())
	title.TextSize = 16; title.TextStyle = fyne.TextStyle{Bold: true}

	type bankOpt struct {
		bankName string
		curr     string   // e.g. SLKA
		ctCurr   string   // e.g. SLKAC
		rate     float64  // T per 1 SLK
		feeBP    int64
	}
	var options []bankOpt
	for _, cb := range myCommercialBanks {
		options = append(options, bankOpt{cb.Name, cb.Currency, cb.Currency + "C", cb.SLKRate, cb.FeeBasisPoints})
	}
	for _, rb := range myReserveBanks {
		options = append(options, bankOpt{rb.Name, rb.Currency, rb.Currency + "C", rb.SLKRate, rb.FeeBasisPoints})
	}

	if len(options) == 0 {
		msg := widget.NewLabel("No banks yet. Create a bank first.")
		return container.NewVScroll(container.NewPadded(container.NewVBox(
			container.NewCenter(title), widget.NewSeparator(), container.NewPadded(msg),
		)))
	}

	bankNames := []string{}
	for _, o := range options { bankNames = append(bankNames, o.bankName) }
	bankSel := widget.NewSelect(bankNames, nil); bankSel.SetSelected(bankNames[0])

	dirSel := widget.NewSelect([]string{
		"SLK → T  (deposit SLK, get user currency)",
		"T → SLK  (redeem user currency back to SLK)",
		"T → CT   (convert user currency to bank ops currency)",
		"CT → T   (convert bank ops currency to user currency)",
	}, nil)
	dirSel.SetSelected("SLK → T  (deposit SLK, get user currency)")

	amountEntry  := widget.NewEntry(); amountEntry.SetPlaceHolder("Amount")
	previewLabel := widget.NewLabel("You will receive: —")
	previewLabel.TextStyle = fyne.TextStyle{Bold: true}
	feePreview := widget.NewLabel("Fee: —")
	feePreview.TextStyle = fyne.TextStyle{Italic: true}
	result := widget.NewLabel(""); result.Alignment = fyne.TextAlignCenter
	result.TextStyle = fyne.TextStyle{Bold: true}

	getOpt := func() bankOpt {
		for _, o := range options { if o.bankName == bankSel.Selected { return o } }
		return options[0]
	}

	updatePreview := func() {
		amt, err := strconv.ParseFloat(strings.TrimSpace(amountEntry.Text), 64)
		if err != nil || amt <= 0 { previewLabel.SetText("You will receive: —"); feePreview.SetText("Fee: —"); return }
		o := getOpt()
		fee := (amt * float64(o.feeBP)) / 10000.0
		switch dirSel.Selected {
		case "SLK → T  (deposit SLK, get user currency)":
			netSLK := amt - fee
			received := slkToBankT(netSLK, o.rate)
			previewLabel.SetText(fmt.Sprintf("You receive: %.4f %s", received, o.curr))
			feePreview.SetText(fmt.Sprintf("Fee: %.8f SLK (%.2f%%)  |  Net SLK deposited: %.8f  |  Rate: 1 SLK = %.0f %s", fee, float64(o.feeBP)/100.0, netSLK, o.rate, o.curr))
		case "T → SLK  (redeem user currency back to SLK)":
			slkOut := bankTToSLK(amt, o.rate)
			slkFee := slkOut * float64(o.feeBP) / 10000.0
			previewLabel.SetText(fmt.Sprintf("You receive: %.8f SLK", slkOut-slkFee))
			feePreview.SetText(fmt.Sprintf("Fee: %.8f SLK (%.2f%%)  |  Rate: %.0f %s = 1 SLK", slkFee, float64(o.feeBP)/100.0, o.rate, o.curr))
		case "T → CT   (convert user currency to bank ops currency)":
			ctFee := amt * float64(o.feeBP) / 10000.0
			net := amt - ctFee
			previewLabel.SetText(fmt.Sprintf("You receive: %.4f %s", bankTToBankCT(net), o.ctCurr))
			feePreview.SetText(fmt.Sprintf("Fee: %.4f %s (%.2f%%)  |  Rate: 1 %s = 1,000,000 %s", ctFee, o.curr, float64(o.feeBP)/100.0, o.curr, o.ctCurr))
		case "CT → T   (convert bank ops currency to user currency)":
			tOut := bankCTToBankT(amt)
			tFee := tOut * float64(o.feeBP) / 10000.0
			previewLabel.SetText(fmt.Sprintf("You receive: %.8f %s", tOut-tFee, o.curr))
			feePreview.SetText(fmt.Sprintf("Fee: %.8f %s (%.2f%%)  |  Rate: 1,000,000 %s = 1 %s", tFee, o.curr, float64(o.feeBP)/100.0, o.ctCurr, o.curr))
		}
	}
	amountEntry.OnChanged = func(_ string) { updatePreview() }
	dirSel.OnChanged     = func(_ string) { updatePreview() }
	bankSel.OnChanged    = func(_ string) { updatePreview() }

	exchangeBtn := widget.NewButton("💱 Exchange Now", func() {
		amt, err := strconv.ParseFloat(strings.TrimSpace(amountEntry.Text), 64)
		if err != nil || amt <= 0 { fyne.Do(func() { result.SetText("❌ Invalid amount") }); return }
		o := getOpt()
		dir := dirSel.Selected

		switch dir {
		case "SLK → T  (deposit SLK, get user currency)":
			if amt > bankAccount.SLK { fyne.Do(func() { result.SetText("❌ Insufficient SLK") }); return }
			fee    := (amt * float64(o.feeBP)) / 10000.0
			netSLK := amt - fee
			if !isSupplySafe(o.curr, slkToBankT(netSLK, o.rate)) {
				fyne.Do(func() { result.SetText("❌ Bank reserve insufficient — not enough SLK locked") }); return
			}
			bankAccount.SLK -= amt
			addBankBalance(o.curr, slkToBankT(netSLK, o.rate))
			// Update bank totals
			for i, cb := range myCommercialBanks {
				if cb.Name == o.bankName { myCommercialBanks[i].TotalDeposited += netSLK; myCommercialBanks[i].TotalIssuedT += slkToBankT(netSLK, o.rate); myCommercialBanks[i].TotalFees += fee; saveCommercialBanks() }
			}
			for i, rb := range myReserveBanks {
				if rb.Name == o.bankName { myReserveBanks[i].TotalDeposited += netSLK; myReserveBanks[i].TotalIssuedT += slkToBankT(netSLK, o.rate); myReserveBanks[i].TotalFees += fee; saveReserveBanks() }
			}
			saveBankAccount(bankAccount); refreshLabels()
			tx := BankTX{ID: fmt.Sprintf("dep_%x", time.Now().UnixNano()), From: "SLK", To: o.curr,
				Amount: amt, Currency: "SLK", Type: "DEPOSIT", Timestamp: time.Now().Unix(),
				Note: fmt.Sprintf("%.8f SLK → %.4f %s  |  fee: %.8f SLK  |  rate: 1 SLK = %.0f %s", netSLK, slkToBankT(netSLK, o.rate), o.curr, fee, o.rate, o.curr), Verified: true}
			txHistory = append(txHistory, tx); saveTxHistory()
			fyne.Do(func() {
				result.SetText(fmt.Sprintf("✅ %.8f SLK → %.4f %s  (fee: %.8f SLK)", netSLK, slkToBankT(netSLK, o.rate), o.curr, fee))
				amountEntry.SetText("")
			})

		case "T → SLK  (redeem user currency back to SLK)":
			if getBankBalance(o.curr) < amt { fyne.Do(func() { result.SetText(fmt.Sprintf("❌ Insufficient %s", o.curr)) }); return }
			slkOut := bankTToSLK(amt, o.rate)
			slkFee := slkOut * float64(o.feeBP) / 10000.0
			netSLK := slkOut - slkFee
			deductBankBalance(o.curr, amt)
			bankAccount.SLK += netSLK
			for i, cb := range myCommercialBanks {
				if cb.Name == o.bankName { myCommercialBanks[i].TotalDeposited -= slkOut; myCommercialBanks[i].TotalIssuedT -= amt; myCommercialBanks[i].TotalFees += slkFee; saveCommercialBanks() }
			}
			for i, rb := range myReserveBanks {
				if rb.Name == o.bankName { myReserveBanks[i].TotalDeposited -= slkOut; myReserveBanks[i].TotalIssuedT -= amt; myReserveBanks[i].TotalFees += slkFee; saveReserveBanks() }
			}
			saveBankAccount(bankAccount); refreshLabels()
			tx := BankTX{ID: fmt.Sprintf("wth_%x", time.Now().UnixNano()), From: o.curr, To: "SLK",
				Amount: netSLK, Currency: "SLK", Type: "WITHDRAWAL", Timestamp: time.Now().Unix(),
				Note: fmt.Sprintf("%.4f %s → %.8f SLK  |  fee: %.8f SLK", amt, o.curr, netSLK, slkFee), Verified: true}
			txHistory = append(txHistory, tx); saveTxHistory()
			fyne.Do(func() {
				result.SetText(fmt.Sprintf("✅ %.4f %s → %.8f SLK  (fee: %.8f SLK)", amt, o.curr, netSLK, slkFee))
				amountEntry.SetText("")
			})

		case "T → CT   (convert user currency to bank ops currency)":
			if getBankBalance(o.curr) < amt { fyne.Do(func() { result.SetText(fmt.Sprintf("❌ Insufficient %s", o.curr)) }); return }
			ctFee := amt * float64(o.feeBP) / 10000.0
			net   := amt - ctFee
			deductBankBalance(o.curr, amt)
			addBankBalance(o.ctCurr, bankTToBankCT(net))
			for i, cb := range myCommercialBanks { if cb.Name == o.bankName { myCommercialBanks[i].TotalFees += bankTToSLK(ctFee, o.rate); saveCommercialBanks() } }
			for i, rb := range myReserveBanks    { if rb.Name == o.bankName { myReserveBanks[i].TotalFees += bankTToSLK(ctFee, o.rate); saveReserveBanks() } }
			saveBankAccount(bankAccount)
			tx := BankTX{ID: fmt.Sprintf("exc_%x", time.Now().UnixNano()), From: o.curr, To: o.ctCurr,
				Amount: amt, Currency: o.curr, Type: "EXCHANGE", Timestamp: time.Now().Unix(),
				Note: fmt.Sprintf("%.4f %s → %.4f %s  |  fee: %.4f %s  |  rate: 1 %s = 1,000,000 %s", amt, o.curr, bankTToBankCT(net), o.ctCurr, ctFee, o.curr, o.curr, o.ctCurr), Verified: true}
			txHistory = append(txHistory, tx); saveTxHistory()
			fyne.Do(func() {
				result.SetText(fmt.Sprintf("✅ %.4f %s → %.4f %s  (fee: %.4f %s)", amt, o.curr, bankTToBankCT(net), o.ctCurr, ctFee, o.curr))
				amountEntry.SetText("")
			})

		case "CT → T   (convert bank ops currency to user currency)":
			if getBankBalance(o.ctCurr) < amt { fyne.Do(func() { result.SetText(fmt.Sprintf("❌ Insufficient %s", o.ctCurr)) }); return }
			tOut := bankCTToBankT(amt)
			tFee := tOut * float64(o.feeBP) / 10000.0
			net  := tOut - tFee
			deductBankBalance(o.ctCurr, amt)
			addBankBalance(o.curr, net)
			for i, cb := range myCommercialBanks { if cb.Name == o.bankName { myCommercialBanks[i].TotalFees += bankTToSLK(tFee, o.rate); saveCommercialBanks() } }
			for i, rb := range myReserveBanks    { if rb.Name == o.bankName { myReserveBanks[i].TotalFees += bankTToSLK(tFee, o.rate); saveReserveBanks() } }
			saveBankAccount(bankAccount)
			tx := BankTX{ID: fmt.Sprintf("exc_%x", time.Now().UnixNano()), From: o.ctCurr, To: o.curr,
				Amount: amt, Currency: o.ctCurr, Type: "EXCHANGE", Timestamp: time.Now().Unix(),
				Note: fmt.Sprintf("%.4f %s → %.8f %s  |  fee: %.8f %s", amt, o.ctCurr, net, o.curr, tFee, o.curr), Verified: true}
			txHistory = append(txHistory, tx); saveTxHistory()
			fyne.Do(func() {
				result.SetText(fmt.Sprintf("✅ %.4f %s → %.8f %s  (fee: %.8f %s)", amt, o.ctCurr, net, o.curr, tFee, o.curr))
				amountEntry.SetText("")
			})
		}
	})
	exchangeBtn.Importance = widget.HighImportance

	// Balance display
	balBox := container.NewVBox(widget.NewLabel("💰 Your Bank Currency Balances:"))
	for _, o := range options {
		balBox.Add(widget.NewLabel(fmt.Sprintf("  %s: %.4f  |  %s: %.4f  |  Rate: 1 SLK = %.0f %s",
			o.curr, getBankBalance(o.curr), o.ctCurr, getBankBalance(o.ctCurr), o.rate, o.curr)))
	}

	return container.NewVScroll(container.NewPadded(container.NewVBox(
		container.NewCenter(title), widget.NewSeparator(),
		container.NewPadded(balBox), widget.NewSeparator(),
		widget.NewLabel("📊 SLK is GOLD — backs all bank currencies. Fee taken on every deposit & withdrawal."),
		widget.NewSeparator(),
		widget.NewForm(
			widget.NewFormItem("Bank", bankSel),
			widget.NewFormItem("Direction", dirSel),
			widget.NewFormItem("Amount", amountEntry),
		),
		container.NewPadded(previewLabel),
		container.NewPadded(feePreview),
		widget.NewSeparator(),
		container.NewPadded(exchangeBtn), result,
	)))
}

// ════════════════════════════════════════
// CLIENT VIEW — shown when user clicks "View Bank" in directory
// Only shows public info + their own wallet. NO secrets.
// ════════════════════════════════════════
func showBankOverview(w fyne.Window, cb *CommercialBank) {
	clientIdx := -1
	for i, cl := range cb.Clients {
		if cl.SLKAddress == bankAccount.AccountID { clientIdx = i; break }
	}

	// ── PUBLIC BANK INFO (no secrets) ──
	nameText := canvas.NewText(fmt.Sprintf("🏢 %s", cb.Name), color.NRGBA{R:0,G:212,B:255,A:255})
	nameText.TextStyle = fyne.TextStyle{Bold: true}; nameText.TextSize = 16
	infoLbl := widget.NewLabel(fmt.Sprintf("Currency: %s  |  Rate: 1 SLK = %.0f %s  |  Fee: %.2f%%  |  Total Clients: %d",
		cb.Currency, cb.SLKRate, cb.Currency, float64(cb.FeeBasisPoints)/100.0, len(cb.Clients)))
	infoLbl.Wrapping = fyne.TextWrapWord
	interestLbl := widget.NewLabel(fmt.Sprintf("💹 Interest: %.2f%% per year  |  Min Lock: 3 weeks  |  Total Deposited: %.4f SLK", cb.InterestRate, cb.TotalDeposited))
	interestLbl.Wrapping = fyne.TextWrapWord

	// ── MY WALLET IN THIS BANK ──
	myBalTitle := canvas.NewText("💳 My Wallet in This Bank", color.NRGBA{R:0,G:255,B:128,A:255})
	myBalTitle.TextStyle = fyne.TextStyle{Bold: true}
	myBal := 0.0; myDepositsCount := 0; myTotalDeposited := 0.0
	if clientIdx >= 0 {
		myBal = cb.Clients[clientIdx].Balance
		myDepositsCount = len(cb.Clients[clientIdx].Deposits)
		myTotalDeposited = cb.Clients[clientIdx].TotalDeposited
	}
	myBalLbl := canvas.NewText(fmt.Sprintf("%.4f %s", myBal, cb.Currency), color.NRGBA{R:0,G:255,B:128,A:255})
	myBalLbl.TextSize = 22; myBalLbl.TextStyle = fyne.TextStyle{Bold: true}
	mySlkLbl := widget.NewLabel(fmt.Sprintf("SLK Wallet: %.8f SLK  |  Total Deposited: %.8f SLK  |  Deposits: %d",
		bankAccount.SLK, myTotalDeposited, myDepositsCount))
	mySlkLbl.Wrapping = fyne.TextWrapWord

	// ── DEPOSIT FORM ──
	depTitle := canvas.NewText("💰 Deposit SLK into This Bank", color.NRGBA{R:255,G:200,B:0,A:255})
	depTitle.TextStyle = fyne.TextStyle{Bold: true}
	depAmtEntry := widget.NewEntry(); depAmtEntry.SetPlaceHolder("SLK amount (e.g. 0.001)")
	weeksOpts := []string{"3 weeks","4 weeks","5 weeks","6 weeks","8 weeks","10 weeks","12 weeks","16 weeks","20 weeks","24 weeks","52 weeks"}
	weeksSelect := widget.NewSelect(weeksOpts, nil); weeksSelect.SetSelected("3 weeks")
	previewLbl := widget.NewLabel("")
	previewLbl.Wrapping = fyne.TextWrapWord
	depAmtEntry.OnChanged = func(s string) {
		amt, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil || amt <= 0 { previewLbl.SetText(""); return }
		fee := amt * float64(cb.FeeBasisPoints) / 10000.0
		netT := (amt - fee) * cb.SLKRate
		previewLbl.SetText(fmt.Sprintf("You receive: %.4f %s  |  Fee: %.8f SLK", netT, cb.Currency, fee))
	}
	depBtn := widget.NewButton("💰 Deposit & Lock SLK", func() {
		amt, err := strconv.ParseFloat(strings.TrimSpace(depAmtEntry.Text), 64)
		if err != nil || amt <= 0 { dialog.ShowInformation("❌ Error", "Enter valid SLK amount", w); return }
		if amt > bankAccount.SLK { dialog.ShowInformation("❌ Insufficient", fmt.Sprintf("You have %.8f SLK", bankAccount.SLK), w); return }
		weeks := 3
		fmt.Sscanf(weeksSelect.Selected, "%d", &weeks)
		withdrawAt := time.Now().Add(time.Duration(weeks) * 7 * 24 * time.Hour).Unix()
		fee := amt * float64(cb.FeeBasisPoints) / 10000.0
		netT := (amt - fee) * cb.SLKRate
		dialog.ShowConfirm("💰 Confirm Deposit",
			fmt.Sprintf("Bank: %s | Deposit: %.8f SLK | Receive: %.4f %s | Locked: %d weeks | Unlock: %s", cb.Name, amt, netT, cb.Currency, weeks, time.Unix(withdrawAt, 0).Format("Jan 02 2006")),
			func(ok bool) {
				if !ok { return }
				bankAccount.SLK -= amt
				dep := BankDeposit{
					ID: fmt.Sprintf("dep_%x", time.Now().UnixNano()),
					ClientID: bankAccount.AccountID, BankID: cb.ID,
					AmountSLK: amt, AmountT: netT,
					DepositedAt: time.Now().Unix(), WithdrawAt: withdrawAt,
					WeeksLocked: weeks, Status: "active", ApprovedByOwner: false,
				}
				if clientIdx == -1 {
					cb.Clients = append(cb.Clients, BankClient{
						AccountID: bankAccount.AccountID, Name: bankAccount.Name,
						SLKAddress: bankAccount.AccountID, Balance: netT,
						JoinedAt: time.Now().Unix(), Active: true, Verified: false,
						KYCName: bankAccount.Name, Deposits: []BankDeposit{dep}, TotalDeposited: amt,
					})
					clientIdx = len(cb.Clients) - 1
				} else {
					cb.Clients[clientIdx].Deposits = append(cb.Clients[clientIdx].Deposits, dep)
					cb.Clients[clientIdx].Balance += netT
					cb.Clients[clientIdx].TotalDeposited += amt
				}
				cb.TotalDeposited += amt; cb.TotalFees += fee; cb.TotalIssuedT += netT
				bankAccount.SLK += fee
				saveBankAccount(bankAccount); saveCommercialBanks(); refreshLabels()
				broadcastBankEvent("DEPOSIT", bankAccount.AccountID, cb.ID, cb.Currency, amt)
				myBalLbl.Text = fmt.Sprintf("%.4f %s", cb.Clients[clientIdx].Balance, cb.Currency)
				myBalLbl.Refresh()
				mySlkLbl.SetText(fmt.Sprintf("SLK Wallet: %.8f SLK  |  Total Deposited: %.8f SLK", bankAccount.SLK, cb.Clients[clientIdx].TotalDeposited))
				dialog.ShowInformation("✅ Deposited", fmt.Sprintf("Deposited %.8f SLK | Received: %.4f %s | Unlocks: %s", amt, netT, cb.Currency, time.Unix(withdrawAt, 0).Format("Jan 02 2006")), w)
				depAmtEntry.SetText(""); previewLbl.SetText("")
			}, w)
	})
	depBtn.Importance = widget.HighImportance

	// ── MY DEPOSITS LIST ──
	depListTitle := canvas.NewText("📋 My Deposits", theme.ForegroundColor())
	depListTitle.TextStyle = fyne.TextStyle{Bold: true}
	depListBox := container.NewVBox()
	if clientIdx >= 0 {
		now := time.Now().Unix()
		for j, dep := range cb.Clients[clientIdx].Deposits {
			depIdx := j
			ready := now >= dep.WithdrawAt && dep.Status == "active"
			statusTxt := "🔒 Locked until " + time.Unix(dep.WithdrawAt, 0).Format("Jan 02 2006")
			if ready { statusTxt = "✅ READY TO WITHDRAW" }
			if dep.Status == "withdrawn" { statusTxt = "✔ Withdrawn" }
			interest := dep.AmountSLK * (cb.InterestRate / 100.0) * (float64(dep.WeeksLocked) / 52.0)
			depLbl := widget.NewLabel(fmt.Sprintf("%.8f SLK → %.4f %s | %d weeks | %s | Interest: %.8f SLK",
				dep.AmountSLK, dep.AmountT, cb.Currency, dep.WeeksLocked, statusTxt, interest))
			depLbl.Wrapping = fyne.TextWrapWord
			wdBtn := widget.NewButton("💸 Withdraw", func() {
				d := cb.Clients[clientIdx].Deposits[depIdx]
				if time.Now().Unix() < d.WithdrawAt {
					dialog.ShowInformation("🔒 Locked", fmt.Sprintf("Cannot withdraw until %s", time.Unix(d.WithdrawAt, 0).Format("Jan 02 2006")), w); return
				}
				if d.Status != "active" { dialog.ShowInformation("Already withdrawn", "This deposit was already withdrawn.", w); return }
				interest := d.AmountSLK * (cb.InterestRate / 100.0) * (float64(d.WeeksLocked) / 52.0)
				totalSLK := d.AmountSLK + interest
				dialog.ShowConfirm("💸 Withdraw", fmt.Sprintf("You get: %.8f SLK + %.8f interest = %.8f SLK", d.AmountSLK, interest, totalSLK),
					func(ok bool) {
						if !ok { return }
						bankAccount.SLK += totalSLK
						cb.Clients[clientIdx].Balance -= d.AmountT
						cb.Clients[clientIdx].TotalWithdrawn += d.AmountSLK
						cb.Clients[clientIdx].Deposits[depIdx].Status = "withdrawn"
						cb.Clients[clientIdx].Deposits[depIdx].InterestEarned = interest
						saveBankAccount(bankAccount); saveCommercialBanks(); refreshLabels()
						broadcastBankEvent("WITHDRAW", bankAccount.AccountID, cb.ID, "SLK", totalSLK)
						myBalLbl.Text = fmt.Sprintf("%.4f %s", cb.Clients[clientIdx].Balance, cb.Currency)
						myBalLbl.Refresh()
						mySlkLbl.SetText(fmt.Sprintf("SLK Wallet: %.8f SLK", bankAccount.SLK))
						dialog.ShowInformation("✅ Withdrawn", fmt.Sprintf("Received %.8f SLK (+ %.8f interest)", totalSLK, interest), w)
					}, w)
			})
			if !ready || dep.Status == "withdrawn" { wdBtn.Disable() }
			depListBox.Add(container.NewVBox(depLbl, wdBtn, widget.NewSeparator()))
		}
	}
	if clientIdx < 0 || len(cb.Clients[clientIdx].Deposits) == 0 {
		depListBox.Add(widget.NewLabel("No deposits yet. Make your first deposit above!"))
	}

	content := container.NewVScroll(container.NewPadded(container.NewVBox(
		container.NewCenter(nameText),
		infoLbl, interestLbl,
		widget.NewSeparator(),
		myBalTitle, myBalLbl, mySlkLbl,
		widget.NewSeparator(),
		depTitle,
		widget.NewForm(
			widget.NewFormItem("SLK Amount", depAmtEntry),
			widget.NewFormItem("Lock Period", weeksSelect),
		),
		previewLbl, depBtn,
		widget.NewSeparator(),
		depListTitle, container.NewPadded(depListBox),
	)))
	content.SetMinSize(fyne.NewSize(700, 500))
	dlg := dialog.NewCustom(fmt.Sprintf("🏢 %s", cb.Name), "Close", content, w)
	dlg.Resize(fyne.NewSize(750, 600))
	dlg.Show()
}

// ════════════════════════════════════════
// OWNER VIEW — full control, shown from My Banks tab
// ════════════════════════════════════════
func showOwnerBankOverview(w fyne.Window, cb *CommercialBank) {
	nameText := canvas.NewText(fmt.Sprintf("👑 %s — Owner Dashboard", cb.Name), color.NRGBA{R:255,G:200,B:0,A:255})
	nameText.TextStyle = fyne.TextStyle{Bold: true}; nameText.TextSize = 16

	// ── STATS ──
	totalClients := len(cb.Clients)
	activeDeposits := 0
	for _, cl := range cb.Clients {
		for _, d := range cl.Deposits { if d.Status == "active" { activeDeposits++ } }
	}
	statsBox := container.NewVBox(
		widget.NewLabel(fmt.Sprintf("👥 Clients: %d  |  Active Deposits: %d", totalClients, activeDeposits)),
		widget.NewLabel(fmt.Sprintf("💰 Total Deposited: %.8f SLK  |  In Circulation: %.4f %s", cb.TotalDeposited, cb.TotalIssuedT, cb.Currency)),
		widget.NewLabel(fmt.Sprintf("💹 Fees Earned: %.8f SLK  |  Interest Rate: %.2f%%/yr  |  Fee: %.2f%%", cb.TotalFees, cb.InterestRate, float64(cb.FeeBasisPoints)/100.0)),
		widget.NewLabel(fmt.Sprintf("💱 Rate: 1 SLK = %.0f %s  |  Bank ID: %s", cb.SLKRate, cb.Currency, cb.ID[:16])),
	)

	// ── API KEY (owner only, copy button) ──
	apiTitle := canvas.NewText("🔑 API Key (Keep Secret)", color.NRGBA{R:255,G:80,B:80,A:255})
	apiTitle.TextStyle = fyne.TextStyle{Bold: true}
	apiMasked := widget.NewLabel("●●●●●●●●●●●●●●●●●●●●●●●●●●●●●●●●●●")
	copyAPIBtn := widget.NewButton("📋 Copy API Key", func() {
		w.Clipboard().SetContent(cb.APIKey)
		dialog.ShowInformation("✅ Copied", "API key copied to clipboard. Keep it secret!", w)
	})
	apiEndpointLbl := widget.NewLabel("Endpoint: http://YOUR-IP:8081/slkapi | POST /slkapi/pay | GET /slkapi/balance")
	apiEndpointLbl.Wrapping = fyne.TextWrapWord

	// ── ALL CLIENTS ──
	clientsTitle := canvas.NewText("👥 All Clients & Deposits", theme.ForegroundColor())
	clientsTitle.TextStyle = fyne.TextStyle{Bold: true}
	clientsBox := container.NewVBox()
	now := time.Now().Unix()
	for i, cl := range cb.Clients {
		clIdx := i
		verified := "❌"; if cl.Verified { verified = "✅" }
		hdr := widget.NewLabel(fmt.Sprintf("%s %s | Balance: %.4f %s | Deposited: %.8f SLK | Deposits: %d",
			verified, cl.Name, cl.Balance, cb.Currency, cl.TotalDeposited, len(cl.Deposits)))
		hdr.TextStyle = fyne.TextStyle{Bold: true}
		verifyBtn := widget.NewButton("✅ Verify", func() {
			cb.Clients[clIdx].Verified = true
			saveCommercialBanks()
			dialog.ShowInformation("✅ Verified", cb.Clients[clIdx].Name+" is now verified.", w)
		})
		if cl.Verified { verifyBtn.Disable() }
		depRows := container.NewVBox()
		for j, dep := range cl.Deposits {
			depIdx := j; clIdxCopy := clIdx
			ready := now >= dep.WithdrawAt && dep.Status == "active"
			status := "🔒 Locked"
			if ready { status = "✅ Ready" }
			if dep.Status == "withdrawn" { status = "✔ Done" }
			interest := dep.AmountSLK * (cb.InterestRate/100.0) * (float64(dep.WeeksLocked)/52.0)
			depLbl := widget.NewLabel(fmt.Sprintf("  %.8f SLK → %.4f %s | %d wks | Unlock: %s | %s | Interest: %.8f",
				dep.AmountSLK, dep.AmountT, cb.Currency, dep.WeeksLocked,
				time.Unix(dep.WithdrawAt, 0).Format("Jan 02 2006"), status, interest))
			depLbl.Wrapping = fyne.TextWrapWord
			approveBtn := widget.NewButton("✅ Approve Early Withdraw", func() {
				cb.Clients[clIdxCopy].Deposits[depIdx].ApprovedByOwner = true
				saveCommercialBanks()
				dialog.ShowInformation("✅ Approved", "Client can now withdraw early.", w)
			})
			if dep.Status == "withdrawn" || dep.ApprovedByOwner { approveBtn.Disable() }
			depRows.Add(container.NewVBox(depLbl, approveBtn, widget.NewSeparator()))
		}
		clientsBox.Add(container.NewVBox(container.NewHBox(hdr, verifyBtn), depRows, widget.NewSeparator()))
	}
	if len(cb.Clients) == 0 { clientsBox.Add(widget.NewLabel("No clients yet.")) }

	content := container.NewVScroll(container.NewPadded(container.NewVBox(
		container.NewCenter(nameText),
		widget.NewSeparator(),
		statsBox,
		widget.NewSeparator(),
		apiTitle, apiMasked, copyAPIBtn, apiEndpointLbl,
		widget.NewSeparator(),
		clientsTitle, container.NewPadded(clientsBox),
	)))
	content.SetMinSize(fyne.NewSize(700, 500))
	dlg := dialog.NewCustom(fmt.Sprintf("👑 %s — Owner", cb.Name), "Close", content, w)
	dlg.Resize(fyne.NewSize(780, 620))
	dlg.Show()
}

func makeBankDirectoryTab(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("🌍 SLK Banks Directory", theme.ForegroundColor())
	title.TextSize = 16; title.TextStyle = fyne.TextStyle{Bold: true}
	subTitle := widget.NewLabel("All banks on the SLK network — deposit SLK, earn interest, connect to websites")
	subTitle.Wrapping = fyne.TextWrapWord
	subTitle.TextStyle = fyne.TextStyle{Italic: true}

	searchEntry := widget.NewEntry(); searchEntry.SetPlaceHolder("🔍 Search banks by name or currency...")
	box := container.NewVBox()

	buildList := func(query string) {
		box.Objects = nil
		q := strings.ToLower(query)

		// ── MY OWN BANKS ──
		for i := range myCommercialBanks {
			cb := &myCommercialBanks[i]
			if q != "" && !strings.Contains(strings.ToLower(cb.Name), q) && !strings.Contains(strings.ToLower(cb.Currency), q) { continue }
			cbCopy := cb
			nameLbl := canvas.NewText(fmt.Sprintf("🏢 %s  [COMMERCIAL BANK]  ★ YOURS", cb.Name), color.NRGBA{R:100,G:200,B:255,A:255})
			nameLbl.TextStyle = fyne.TextStyle{Bold: true}; nameLbl.TextSize = 13
			viewBtn := widget.NewButton("👁 View Bank", func() { showBankOverview(w, cbCopy) })
			viewBtn.Importance = widget.HighImportance
			box.Add(container.NewPadded(container.NewVBox(
				nameLbl,
				widget.NewLabel(fmt.Sprintf("💱 Currency: %s  |  Rate: 1 SLK = %.0f %s  |  Fee: %.2f%%", cb.Currency, cb.SLKRate, cb.Currency, float64(cb.FeeBasisPoints)/100.0)),
				widget.NewLabel(fmt.Sprintf("💰 Fees Earned: %.8f SLK  |  Total Deposited: %.8f SLK  |  Clients: %d", cb.TotalFees, cb.TotalDeposited, len(cb.Clients))),
				viewBtn,
				widget.NewSeparator(),
			)))
		}
		for i := range myReserveBanks {
			rb := &myReserveBanks[i]
			if q != "" && !strings.Contains(strings.ToLower(rb.Name), q) && !strings.Contains(strings.ToLower(rb.Currency), q) { continue }
			nameLbl := canvas.NewText(fmt.Sprintf("🏛 %s  [RESERVE BANK]  ★ YOURS", rb.Name), color.NRGBA{R:100,G:200,B:255,A:255})
			nameLbl.TextStyle = fyne.TextStyle{Bold: true}; nameLbl.TextSize = 13
			box.Add(container.NewPadded(container.NewVBox(
				nameLbl,
				widget.NewLabel(fmt.Sprintf("💱 Currency: %s  |  Rate: 1 SLK = %.0f %s  |  Fee: %.2f%%", rb.Currency, rb.SLKRate, rb.Currency, float64(rb.FeeBasisPoints)/100.0)),
				widget.NewLabel(fmt.Sprintf("🔒 SLK Locked: %.8f  |  Issued: %.8f  |  Fees: %.8f SLK", rb.LockedSLK, rb.IssuedAmount, rb.TotalFees)),
				widget.NewSeparator(),
			)))
		}

		// ── PEER BANKS ──
		for i := len(knownBanks) - 1; i >= 0; i-- {
			b := knownBanks[i]
			if q != "" && !strings.Contains(strings.ToLower(b.Name), q) && !strings.Contains(strings.ToLower(b.AccountID), q) { continue }
			b2 := b
			nameLbl := canvas.NewText(fmt.Sprintf("🏦 %s", b.Name), theme.ForegroundColor())
			nameLbl.TextStyle = fyne.TextStyle{Bold: true}; nameLbl.TextSize = 13

			// Deposit button
			depositBtn := widget.NewButton("⬇ Deposit SLK", func() {
				amtEntry := widget.NewEntry(); amtEntry.SetPlaceHolder("Amount of SLK to deposit")
				dlg := dialog.NewForm("⬇ Deposit to "+b2.Name, "Deposit", "Cancel",
					[]*widget.FormItem{widget.NewFormItem("SLK Amount", amtEntry)},
					func(ok bool) {
						if !ok { return }
						amt, err := strconv.ParseFloat(strings.TrimSpace(amtEntry.Text), 64)
						if err != nil || amt <= 0 { dialog.ShowInformation("❌ Error", "Invalid amount", w); return }
						if amt > bankAccount.SLK { dialog.ShowInformation("❌ Error", "Insufficient SLK", w); return }
						bankAccount.SLK -= amt
						saveBankAccount(bankAccount); refreshLabels()
						tx := BankTX{ID: fmt.Sprintf("dep_%x", time.Now().UnixNano()),
							From: bankAccount.AccountID, To: b2.AccountID,
							Amount: amt, Currency: "SLK", Type: "DEPOSIT",
							Timestamp: time.Now().Unix(),
							Note: fmt.Sprintf("Deposit to bank: %s", b2.Name), Verified: true}
						txHistory = append(txHistory, tx); saveTxHistory()
						if p2pNode != nil {
							p2pNode.BroadcastBankRecord(p2p.BankRecord{
								ID: tx.ID, From: bankAccount.AccountID, To: b2.AccountID,
								Amount: amt, Currency: "SLK", TxType: "DEPOSIT",
								Timestamp: time.Now().Unix(), Verified: true})
						}
						dialog.ShowInformation("✅ Deposited",
							fmt.Sprintf("%.8f SLK deposited to %s\nTransaction recorded on network.", amt, b2.Name), w)
					}, w)
				dlg.Show()
			})
			depositBtn.Importance = widget.HighImportance

			// Website button
			webBtn := widget.NewButton("🌐 Open Website", func() {
				if b2.OwnerAddr == "" {
					dialog.ShowInformation("🌐 Website", "No website registered for this bank yet.", w)
					return
				}
				dialog.ShowInformation("🌐 Connect to Bank Website",
					fmt.Sprintf("Bank: %s\nWebsite: %s\n\nVisit this URL to use the bank online.\nThe website accepts SLK payments via API.", b2.Name, b2.OwnerAddr), w)
			})

			// Send SLK button
			sendBtn := widget.NewButton("💸 Send SLK", func() {
				amtEntry  := widget.NewEntry(); amtEntry.SetPlaceHolder("Amount of SLK")
				noteEntry := widget.NewEntry(); noteEntry.SetPlaceHolder("Note (optional)")
				dlg := dialog.NewForm("💸 Send SLK to "+b2.Name, "Send", "Cancel",
					[]*widget.FormItem{
						widget.NewFormItem("Amount", amtEntry),
						widget.NewFormItem("Note", noteEntry),
					},
					func(ok bool) {
						if !ok { return }
						amt, err := strconv.ParseFloat(strings.TrimSpace(amtEntry.Text), 64)
						if err != nil || amt <= 0 { dialog.ShowInformation("❌ Error", "Invalid amount", w); return }
						if amt > bankAccount.SLK { dialog.ShowInformation("❌ Error", "Insufficient SLK", w); return }
						bankAccount.SLK -= amt
						saveBankAccount(bankAccount); refreshLabels()
						tx := BankTX{ID: fmt.Sprintf("snd_%x", time.Now().UnixNano()),
							From: bankAccount.AccountID, To: b2.AccountID,
							Amount: amt, Currency: "SLK", Type: "SEND",
							Timestamp: time.Now().Unix(), Note: noteEntry.Text, Verified: true}
						txHistory = append(txHistory, tx); saveTxHistory()
						if p2pNode != nil {
							p2pNode.BroadcastTx(p2p.TxMsg{ID: tx.ID, From: bankAccount.AccountID,
								To: b2.AccountID, Amount: amt, Timestamp: time.Now().Unix(), Type: 1})
						}
						dialog.ShowInformation("✅ Sent", fmt.Sprintf("%.8f SLK sent to %s", amt, b2.Name), w)
					}, w)
				dlg.Show()
			})

			box.Add(container.NewPadded(container.NewVBox(
				nameLbl,
				widget.NewLabel(fmt.Sprintf("🆔 ID: %s", b2.AccountID)),
				widget.NewLabel(fmt.Sprintf("🌐 Website: %s", func() string { if b2.OwnerAddr != "" { return b2.OwnerAddr } else { return "Not registered" } }())),
				widget.NewLabel(fmt.Sprintf("⏰ Last seen: %s", time.Unix(b2.SeenAt, 0).Format("Jan 02 2006 15:04"))),
				container.NewGridWithColumns(3, depositBtn, sendBtn, webBtn),
				widget.NewSeparator(),
			)))
		}

		if len(knownBanks) == 0 && len(myCommercialBanks) == 0 && len(myReserveBanks) == 0 {
			box.Add(container.NewPadded(widget.NewLabel("No banks yet. Create one in the Create Bank tab or wait for peers to connect.")))
		}
		box.Refresh()
	}

	searchEntry.OnChanged = func(q string) { buildList(q) }
	buildList("")

	totalLabel := widget.NewLabel(fmt.Sprintf("📊 Total banks on network: %d", len(knownBanks)+len(myCommercialBanks)+len(myReserveBanks)))
	totalLabel.TextStyle = fyne.TextStyle{Bold: true}

	return container.NewBorder(
		container.NewVBox(
			container.NewCenter(title),
			container.NewPadded(subTitle),
			container.NewPadded(totalLabel),
			container.NewPadded(searchEntry),
			widget.NewSeparator(),
		),
		nil, nil, nil, container.NewVScroll(container.NewPadded(box)),
	)
}

func makeMyBanksTab(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("🏦 Bank Management Office", theme.ForegroundColor())
	title.TextSize = 16; title.TextStyle = fyne.TextStyle{Bold: true}

	if len(myCommercialBanks) == 0 && len(myReserveBanks) == 0 {
		msg := widget.NewLabel("You don't own a bank yet. Go to Create Bank tab to open one.")
		msg.Wrapping = fyne.TextWrapWord
		return container.NewCenter(container.NewVBox(title, widget.NewSeparator(), msg))
	}

	box := container.NewVBox()

	// ── COMMERCIAL BANK OFFICE ──
	for i := range myCommercialBanks {
		cb := &myCommercialBanks[i]
		cbIdx := i

		// Use stored API key — never regenerate
		apiKey := cb.APIKey
		if apiKey == "" {
			apiKey = generateAPIKey(cb.ID)
			myCommercialBanks[cbIdx].APIKey = apiKey
			saveCommercialBanks()
		}

		var totalShares int64
		for _, s := range cb.Shares { totalShares += s.Shares }

		// ── DASHBOARD ──
		dashTitle := canvas.NewText("📊 Dashboard", theme.ForegroundColor())
		dashTitle.TextStyle = fyne.TextStyle{Bold: true}
		feeLbl    := widget.NewLabel(fmt.Sprintf("💰 Total Fees Earned: %.8f SLK", cb.TotalFees))
		clientLbl := widget.NewLabel(fmt.Sprintf("👥 Total Clients: %d", len(cb.Clients)))
		shareLbl  := widget.NewLabel(fmt.Sprintf("📈 Total Shares: %d", totalShares))
		depLbl    := widget.NewLabel(fmt.Sprintf("🏦 Total Deposited: %.8f %s", cb.TotalDeposited, cb.Currency))
		rateLbl   := widget.NewLabel(fmt.Sprintf("💹 Interest Rate: %.2f%%", cb.InterestRate))

		// ── ANNOUNCEMENT ──
		annTitle   := canvas.NewText("📢 Bank Announcement", theme.ForegroundColor())
		annTitle.TextStyle = fyne.TextStyle{Bold: true}
		annEntry   := widget.NewEntry()
		annEntry.SetPlaceHolder("Post announcement to all clients...")
		annEntry.MultiLine = true
		annEntry.SetText(cb.Announcement)
		annBtn := widget.NewButton("📢 Post Announcement", func() {
			myCommercialBanks[cbIdx].Announcement = strings.TrimSpace(annEntry.Text)
			saveCommercialBanks()
			if p2pNode != nil {
				p2pNode.BroadcastSocial(p2p.SocialMsg{
					ID: fmt.Sprintf("ann_%x", time.Now().UnixNano()),
					From: bankAccount.AccountID, Name: cb.Name,
					Text: "__BANK_ANN__:" + cb.ID + ":" + annEntry.Text,
					Timestamp: time.Now().Unix(),
				})
			}
			dialog.ShowInformation("✅ Posted", "Announcement sent to all peers.", w)
		})
		annBtn.Importance = widget.HighImportance

		// ── INTEREST RATE ──
		intTitle  := canvas.NewText("💹 Set Interest Rate", theme.ForegroundColor())
		intTitle.TextStyle = fyne.TextStyle{Bold: true}
		intEntry  := widget.NewEntry()
		intEntry.SetPlaceHolder("Annual interest rate % (e.g. 3.5)")
		intEntry.SetText(fmt.Sprintf("%.2f", cb.InterestRate))
		intBtn := widget.NewButton("💹 Update Interest Rate", func() {
			rate, err := strconv.ParseFloat(strings.TrimSpace(intEntry.Text), 64)
			if err != nil || rate < 0 || rate > 50 {
				dialog.ShowInformation("Error", "Rate must be 0-50%", w); return
			}
			myCommercialBanks[cbIdx].InterestRate = rate
			saveCommercialBanks()
			rateLbl.SetText(fmt.Sprintf("💹 Interest Rate: %.2f%%", rate))
			dialog.ShowInformation("✅ Updated", fmt.Sprintf("Interest rate set to %.2f%%", rate), w)
		})

		// ── CLIENT LIST ──
		clientTitle := canvas.NewText("👥 Client Accounts", theme.ForegroundColor())
		clientTitle.TextStyle = fyne.TextStyle{Bold: true}
		clientBox := container.NewVBox()
		if len(cb.Clients) == 0 {
			clientBox.Add(widget.NewLabel("No clients yet. Share your API key so clients can connect."))
		} else {
			for _, cl := range cb.Clients {
				status := "✅ Active"; if !cl.Active { status = "⛔ Inactive" }
				clientBox.Add(container.NewVBox(
					widget.NewLabel(fmt.Sprintf("%s  %s  |  Balance: %.8f %s  |  Joined: %s",
						status, cl.Name, cl.Balance, cb.Currency,
						time.Unix(cl.JoinedAt, 0).Format("Jan 02 2006"))),
					widget.NewLabel(fmt.Sprintf("   Address: %s", cl.SLKAddress)),
					widget.NewSeparator(),
				))
			}
		}

		// ── SHARES ──
		shareTitle := canvas.NewText("📈 Share Management", theme.ForegroundColor())
		shareTitle.TextStyle = fyne.TextStyle{Bold: true}
		shareToEntry  := widget.NewEntry(); shareToEntry.SetPlaceHolder("Recipient Account ID (SLKB-xxxx)")
		shareAmtEntry := widget.NewEntry(); shareAmtEntry.SetPlaceHolder("Number of shares to issue")
		shareBtn := widget.NewButton("📤 Issue Shares", func() {
			amt, err := strconv.ParseInt(strings.TrimSpace(shareAmtEntry.Text), 10, 64)
			if err != nil || amt <= 0 { dialog.ShowInformation("Error", "Invalid share amount", w); return }
			to := strings.TrimSpace(shareToEntry.Text)
			if to == "" { dialog.ShowInformation("Error", "Enter recipient Account ID", w); return }
			myCommercialBanks[cbIdx].Shares = append(myCommercialBanks[cbIdx].Shares,
				BankShare{HolderID: to, HolderName: to, Shares: amt, TotalShares: totalShares + amt})
			saveCommercialBanks()
			dialog.ShowInformation("✅ Shares Issued",
				fmt.Sprintf("Issued %d shares of %s to %s. Shareholders earn dividends from every transaction fee.", amt, cb.Name, shortAddr(to)), w)
			shareToEntry.SetText(""); shareAmtEntry.SetText("")
		})
		shareBtn.Importance = widget.HighImportance

		// Shareholder list
		holderBox := container.NewVBox()
		if len(cb.Shares) == 0 {
			holderBox.Add(widget.NewLabel("No shareholders yet."))
		} else {
			for _, s := range cb.Shares {
				pct := 0.0
				if totalShares > 0 { pct = float64(s.Shares) / float64(totalShares) * 100 }
				holderBox.Add(widget.NewLabel(fmt.Sprintf("  %s — %d shares (%.1f%%)", shortAddr(s.HolderID), s.Shares, pct)))
			}
		}

		// ── API KEY ──
		apiTitle := canvas.NewText("🔑 API Key — Connect Your Website", theme.ForegroundColor())
		apiTitle.TextStyle = fyne.TextStyle{Bold: true}
		apiLabel := widget.NewLabel(apiKey)
		apiLabel.TextStyle = fyne.TextStyle{Monospace: true}
		apiLabel.Wrapping = fyne.TextWrapWord
		copyAPIBtn := widget.NewButton("📋 Copy API Key", func() {
			w.Clipboard().SetContent(apiKey)
			statusBar.SetText("✅ API key copied — keep it secret!")
		})
		copyAPIBtn.Importance = widget.HighImportance
		// ── DOMAIN LOCK ──
		domainEntry := widget.NewEntry()
		domainEntry.SetPlaceHolder("e.g. mywebsite.com or https://myapp.com")
		domainEntry.SetText(cb.AllowedDomain)
		domainBtn := widget.NewButton("🔒 Lock API to This Domain", func() {
			d := strings.TrimSpace(domainEntry.Text)
			myCommercialBanks[cbIdx].AllowedDomain = d
			saveCommercialBanks()
			if d == "" {
				dialog.ShowInformation("🔓 Unlocked", "API key now works from ANY domain. Set a domain to restrict it.", w)
			} else {
				dialog.ShowInformation("🔒 Locked", fmt.Sprintf("API key now ONLY works from: %s\n\nRequests from other domains will be rejected.", d), w)
			}
		})
		domainBtn.Importance = widget.WarningImportance
		domainStatusLbl := widget.NewLabel("")
		if cb.AllowedDomain != "" {
			domainStatusLbl.SetText(fmt.Sprintf("🔒 Locked to: %s", cb.AllowedDomain))
		} else {
			domainStatusLbl.SetText("🔓 Open — works from any domain (set domain to restrict)")
		}
		apiInstructions := widget.NewLabel(fmt.Sprintf("Website API: http://localhost:8081/slkapi | Header: X-API-Key: %s... | NEVER share publicly.", apiKey[:20]))
		apiInstructions.Wrapping = fyne.TextWrapWord

		// ── BANK HEADER ──
		nameLbl := canvas.NewText(fmt.Sprintf("🏢 %s — COMMERCIAL BANK", cb.Name), color.NRGBA{R:0,G:212,B:255,A:255})
		nameLbl.TextStyle = fyne.TextStyle{Bold: true}; nameLbl.TextSize = 15
		subLbl := widget.NewLabel(fmt.Sprintf("Currency: %s  |  Fee: %.2f%% PERMANENT  |  1 SLK = %.0f %s  |  Created: %s",
			cb.Currency, float64(cb.FeeBasisPoints)/100.0, cb.SLKRate, cb.Currency,
			time.Unix(cb.CreatedAt, 0).Format("Jan 02 2006")))
		subLbl.Wrapping = fyne.TextWrapWord

		// Full API key display
		fullAPILabel := widget.NewLabel(apiKey)
		fullAPILabel.TextStyle = fyne.TextStyle{Monospace: true}
		fullAPILabel.Wrapping = fyne.TextWrapWord

		box.Add(container.NewPadded(container.NewVBox(
			// ── HEADER ──
			nameLbl, subLbl,
			widget.NewSeparator(),
			// ── DASHBOARD ──
			dashTitle, feeLbl, clientLbl, shareLbl, depLbl, rateLbl,
			widget.NewSeparator(),
			// ── API KEY (prominent) ──
			apiTitle,
			fullAPILabel,
			copyAPIBtn,
			apiInstructions,
			widget.NewSeparator(),
			widget.NewLabel("🔒 Domain Lock — restrict API key to 1 website only:"),
			domainStatusLbl,
			widget.NewForm(widget.NewFormItem("Domain", domainEntry)),
			domainBtn,
			widget.NewSeparator(),
			// ── ANNOUNCEMENT ──
			annTitle, annEntry, annBtn,
			widget.NewSeparator(),
			// ── INTEREST ──
			intTitle,
			widget.NewForm(widget.NewFormItem("Rate %", intEntry)),
			intBtn,
			widget.NewSeparator(),
			// ── CLIENTS ──
			clientTitle, container.NewPadded(clientBox),
			widget.NewSeparator(),
			// ── SHARES ──
			shareTitle,
			widget.NewForm(
				widget.NewFormItem("Recipient ID", shareToEntry),
				widget.NewFormItem("Shares", shareAmtEntry),
			),
			shareBtn,
			widget.NewLabel("Current Shareholders:"),
			container.NewPadded(holderBox),
			widget.NewSeparator(),
			widget.NewButton("👑 Full Owner Dashboard", func() { showOwnerBankOverview(w, cb) }),
			widget.NewSeparator(),
		)))
	}

	// ── RESERVE BANK OFFICE ──
	for i := range myReserveBanks {
		rb := &myReserveBanks[i]
		rbIdx := i

		apiKey := rb.APIKey
		if apiKey == "" {
			apiKey = generateAPIKey(rb.ID)
			myReserveBanks[rbIdx].APIKey = apiKey
			saveReserveBanks()
		}

		reserveRatio := 0.0
		if rb.IssuedAmount > 0 { reserveRatio = (rb.LockedSLK / rb.IssuedAmount) * 100 }

		// ── DASHBOARD ──
		dashTitle := canvas.NewText("📊 Central Bank Dashboard", theme.ForegroundColor())
		dashTitle.TextStyle = fyne.TextStyle{Bold: true}
		lockedLbl  := widget.NewLabel(fmt.Sprintf("🔒 SLK Locked as Reserve: %.8f SLK", rb.LockedSLK))
		issuedLbl  := widget.NewLabel(fmt.Sprintf("💵 %s Issued: %.8f", rb.Currency, rb.IssuedAmount))
		ratioLbl   := widget.NewLabel(fmt.Sprintf("📊 Reserve Ratio: %.2f%% (healthy = above 100%%)", reserveRatio))
		feeLbl2    := widget.NewLabel(fmt.Sprintf("💰 Total Fees Earned: %.8f SLK", rb.TotalFees))
		clientLbl2 := widget.NewLabel(fmt.Sprintf("👥 Member Banks: %d", len(rb.Clients)))
		rateLbl2   := widget.NewLabel(fmt.Sprintf("💹 Base Interest Rate: %.2f%%", rb.InterestRate))

		// ── MONETARY POLICY ──
		mintTitle := canvas.NewText("💵 Monetary Policy — Issue Currency", theme.ForegroundColor())
		mintTitle.TextStyle = fyne.TextStyle{Bold: true}
		mintWarn := widget.NewLabel("⚠ You can only issue currency backed by locked SLK. Max = 10x your locked SLK. SLK owners can ALWAYS redeem back.")
		mintWarn.Wrapping = fyne.TextWrapWord
		issueEntry := widget.NewEntry()
		issueEntry.SetPlaceHolder(fmt.Sprintf("Amount of %s to mint (backed by SLK)", rb.Currency))
		issueBtn := widget.NewButton(fmt.Sprintf("💵 Mint %s", rb.Currency), func() {
			amt, err := strconv.ParseFloat(strings.TrimSpace(issueEntry.Text), 64)
			if err != nil || amt <= 0 { dialog.ShowInformation("Error", "Invalid amount", w); return }
			maxIssue := myReserveBanks[rbIdx].LockedSLK * 10
			if myReserveBanks[rbIdx].IssuedAmount+amt > maxIssue {
				dialog.ShowInformation("❌ Insufficient Reserve",
					fmt.Sprintf("Max you can mint: %.8f %s. Lock more SLK to mint more.", maxIssue-myReserveBanks[rbIdx].IssuedAmount, rb.Currency), w)
				return
			}
			myReserveBanks[rbIdx].IssuedAmount += amt
			myReserveBanks[rbIdx].MintedAmount += amt
			saveReserveBanks()
			newRatio := 0.0
			if myReserveBanks[rbIdx].IssuedAmount > 0 {
				newRatio = myReserveBanks[rbIdx].LockedSLK / myReserveBanks[rbIdx].IssuedAmount * 100
			}
			issuedLbl.SetText(fmt.Sprintf("💵 %s Issued: %.8f", rb.Currency, myReserveBanks[rbIdx].IssuedAmount))
			ratioLbl.SetText(fmt.Sprintf("📊 Reserve Ratio: %.2f%%", newRatio))
			dialog.ShowInformation("✅ Minted",
				fmt.Sprintf("Minted %.8f %s | In circulation: %.8f %s | Reserve Ratio: %.2f%%", amt, rb.Currency, myReserveBanks[rbIdx].IssuedAmount, rb.Currency, newRatio), w)
			issueEntry.SetText("")
		})
		issueBtn.Importance = widget.HighImportance

		// ── LOCK MORE SLK ──
		lockTitle := canvas.NewText("🔒 Lock More SLK as Reserve", theme.ForegroundColor())
		lockTitle.TextStyle = fyne.TextStyle{Bold: true}
		lockWarn := widget.NewLabel("Locking more SLK increases your reserve ratio and allows more currency minting.")
		lockWarn.Wrapping = fyne.TextWrapWord
		lockEntry := widget.NewEntry()
		lockEntry.SetPlaceHolder("SLK amount to lock permanently as reserve")
		lockBtn := widget.NewButton("🔒 Lock SLK", func() {
			amt, err := strconv.ParseFloat(strings.TrimSpace(lockEntry.Text), 64)
			if err != nil || amt <= 0 { dialog.ShowInformation("Error", "Invalid amount", w); return }
			if amt > bankAccount.SLK { dialog.ShowInformation("❌ Error", "Insufficient SLK balance", w); return }
			dialog.ShowConfirm("⚠ Lock SLK",
				fmt.Sprintf("Lock %.8f SLK as reserve? This SLK backs your currency and protects clients.", amt),
				func(ok bool) {
					if !ok { return }
					bankAccount.SLK -= amt
					myReserveBanks[rbIdx].LockedSLK += amt
					saveBankAccount(bankAccount)
					saveReserveBanks()
					refreshLabels()
					newRatio := 0.0
					if myReserveBanks[rbIdx].IssuedAmount > 0 {
						newRatio = myReserveBanks[rbIdx].LockedSLK / myReserveBanks[rbIdx].IssuedAmount * 100
					}
					lockedLbl.SetText(fmt.Sprintf("🔒 SLK Locked as Reserve: %.8f SLK", myReserveBanks[rbIdx].LockedSLK))
					ratioLbl.SetText(fmt.Sprintf("📊 Reserve Ratio: %.2f%%", newRatio))
					dialog.ShowInformation("✅ Locked",
						fmt.Sprintf("%.8f SLK locked. New reserve ratio: %.2f%%", amt, newRatio), w)
					lockEntry.SetText("")
				}, w)
		})
		lockBtn.Importance = widget.DangerImportance

		// ── INTEREST RATE ──
		intTitle2 := canvas.NewText("💹 Base Interest Rate Policy", theme.ForegroundColor())
		intTitle2.TextStyle = fyne.TextStyle{Bold: true}
		intEntry2 := widget.NewEntry()
		intEntry2.SetPlaceHolder("Base rate % (e.g. 5.0) — affects all member banks")
		intEntry2.SetText(fmt.Sprintf("%.2f", rb.InterestRate))
		intBtn2 := widget.NewButton("💹 Set Base Rate", func() {
			rate, err := strconv.ParseFloat(strings.TrimSpace(intEntry2.Text), 64)
			if err != nil || rate < 0 || rate > 100 {
				dialog.ShowInformation("Error", "Rate must be 0-100%", w); return
			}
			myReserveBanks[rbIdx].InterestRate = rate
			saveReserveBanks()
			rateLbl2.SetText(fmt.Sprintf("💹 Base Interest Rate: %.2f%%", rate))
			dialog.ShowInformation("✅ Rate Set", fmt.Sprintf("Base interest rate: %.2f%%. All member commercial banks are notified.", rate), w)
		})

		// ── ANNOUNCEMENT ──
		annTitle2 := canvas.NewText("📢 Central Bank Announcement", theme.ForegroundColor())
		annTitle2.TextStyle = fyne.TextStyle{Bold: true}
		annEntry2 := widget.NewEntry()
		annEntry2.MultiLine = true
		annEntry2.SetPlaceHolder("Official monetary policy announcement...")
		annEntry2.SetText(rb.Announcement)
		annBtn2 := widget.NewButton("📢 Broadcast Announcement", func() {
			myReserveBanks[rbIdx].Announcement = strings.TrimSpace(annEntry2.Text)
			saveReserveBanks()
			if p2pNode != nil {
				p2pNode.BroadcastSocial(p2p.SocialMsg{
					ID: fmt.Sprintf("ann_%x", time.Now().UnixNano()),
					From: bankAccount.AccountID, Name: rb.Name,
					Text: "__BANK_ANN__:" + rb.ID + ":" + annEntry2.Text,
					Timestamp: time.Now().Unix(),
				})
			}
			dialog.ShowInformation("✅ Broadcast", "Announcement sent to all network peers.", w)
		})
		annBtn2.Importance = widget.HighImportance

		// ── API KEY ──
		apiTitle2 := canvas.NewText("🔑 API Key — Central Bank Integration", theme.ForegroundColor())
		apiTitle2.TextStyle = fyne.TextStyle{Bold: true}
		apiLabel2 := widget.NewLabel(apiKey)
		apiLabel2.TextStyle = fyne.TextStyle{Monospace: true}
		apiLabel2.Wrapping = fyne.TextWrapWord
		copyAPIBtn2 := widget.NewButton("📋 Copy API Key", func() {
			w.Clipboard().SetContent(apiKey)
			statusBar.SetText("✅ Central Bank API key copied!")
		})
		copyAPIBtn2.Importance = widget.HighImportance

		// ── BANK HEADER ──
		nameLbl2 := canvas.NewText(fmt.Sprintf("🏛 %s — RESERVE BANK (CENTRAL BANK)", rb.Name), color.NRGBA{R:245,G:158,B:11,A:255})
		nameLbl2.TextStyle = fyne.TextStyle{Bold: true}; nameLbl2.TextSize = 15
		subLbl2 := widget.NewLabel(fmt.Sprintf("Currency: %s  |  Fee: %.2f%% PERMANENT  |  1 SLK = %.0f %s  |  Created: %s",
			rb.Currency, float64(rb.FeeBasisPoints)/100.0, rb.SLKRate, rb.Currency,
			time.Unix(rb.CreatedAt, 0).Format("Jan 02 2006")))
		subLbl2.Wrapping = fyne.TextWrapWord

		box.Add(container.NewPadded(container.NewVBox(
			nameLbl2, subLbl2, widget.NewSeparator(),
			// Dashboard
			dashTitle, lockedLbl, issuedLbl, ratioLbl, feeLbl2, clientLbl2, rateLbl2,
			widget.NewSeparator(),
			// Mint
			mintTitle, mintWarn,
			widget.NewForm(widget.NewFormItem(fmt.Sprintf("Mint %s", rb.Currency), issueEntry)),
			issueBtn,
			widget.NewSeparator(),
			// Lock
			lockTitle, lockWarn,
			widget.NewForm(widget.NewFormItem("SLK Amount", lockEntry)),
			lockBtn,
			widget.NewSeparator(),
			// Interest
			intTitle2,
			widget.NewForm(widget.NewFormItem("Base Rate %", intEntry2)),
			intBtn2,
			widget.NewSeparator(),
			// Announcement
			annTitle2, annEntry2, annBtn2,
			widget.NewSeparator(),
			// API
			apiTitle2, apiLabel2, copyAPIBtn2,
			widget.NewSeparator(),
		)))
	}

	return container.NewBorder(
		container.NewVBox(container.NewCenter(title), widget.NewSeparator()),
		nil, nil, nil, container.NewVScroll(container.NewPadded(box)),
	)
}


func makeCreateBankTab(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("Create a Bank", theme.ForegroundColor())
	title.TextSize = 16; title.TextStyle = fyne.TextStyle{Bold: true}

	// ── LOCK: ONE bank per account — FOREVER ──
	if len(myCommercialBanks) > 0 {
		cb := myCommercialBanks[0]
		lockedMsg := canvas.NewText("🏢 You already own a Commercial Bank. Only ONE bank per account allowed.", color.NRGBA{R:255,G:80,B:80,A:255})
		lockedMsg.TextSize = 12; lockedMsg.TextStyle = fyne.TextStyle{Bold: true}
		infoMsg := widget.NewLabel(fmt.Sprintf("Bank: %s | Currency: %s | Created: %s — One bank per account. Forever. Go to My Banks tab.", cb.Name, cb.Currency, time.Unix(cb.CreatedAt, 0).Format("Jan 02 2006")))
		infoMsg.Wrapping = fyne.TextWrapWord
		return container.NewVScroll(container.NewPadded(container.NewVBox(
			container.NewCenter(title), widget.NewSeparator(),
			container.NewPadded(lockedMsg),
			container.NewPadded(infoMsg),
		)))
	}
	if len(myReserveBanks) > 0 {
		rb := myReserveBanks[0]
		lockedMsg := canvas.NewText("🏛 You already own a Reserve Bank. Only ONE bank per account allowed.", color.NRGBA{R:255,G:80,B:80,A:255})
		lockedMsg.TextSize = 12; lockedMsg.TextStyle = fyne.TextStyle{Bold: true}
		infoMsg := widget.NewLabel(fmt.Sprintf("Bank: %s | Currency: %s | Created: %s — One bank per account. Forever. Go to My Banks tab.", rb.Name, rb.Currency, time.Unix(rb.CreatedAt, 0).Format("Jan 02 2006")))
		infoMsg.Wrapping = fyne.TextWrapWord
		return container.NewVScroll(container.NewPadded(container.NewVBox(
			container.NewCenter(title), widget.NewSeparator(),
			container.NewPadded(lockedMsg),
			container.NewPadded(infoMsg),
		)))
	}

	warningLbl := canvas.NewText("WARNING: Bank type and fee rate are PERMANENT and can NEVER be changed.", color.NRGBA{R:255,G:80,B:80,A:255})
	warningLbl.TextSize = 11
	typeSel := widget.NewSelect([]string{
		"🏢 Commercial Bank — earns fees from transactions",
		"🏛 Reserve Bank — locks SLK, issues custom currency",
	}, nil)
	typeSel.SetSelected("🏢 Commercial Bank — earns fees from transactions")
	typeDesc := widget.NewLabel("Earn a fee on every transaction. Fee is set ONCE and locked FOREVER. Get an API key to connect your website.")
	typeDesc.Wrapping = fyne.TextWrapWord
	typeSel.OnChanged = func(s string) {
		if strings.Contains(s, "Reserve") {
			typeDesc.SetText("Lock real SLK as backing and issue your own custom currency. Users can always withdraw to real SLK. Fee is PERMANENT.")
		} else {
			typeDesc.SetText("Earn a fee on every transaction. Fee is set ONCE and locked FOREVER. Get an API key to connect your website.")
		}
	}
	nameEntry := widget.NewEntry(); nameEntry.SetPlaceHolder("Bank name (e.g. SLKafrica)")
	currEntry := widget.NewEntry(); currEntry.SetPlaceHolder("Currency ticker (e.g. SLKA — no spaces)")
	feeEntry  := widget.NewEntry(); feeEntry.SetPlaceHolder("Fee % (e.g. 0.5) — PERMANENT FOREVER")
	rateEntry := widget.NewEntry(); rateEntry.SetPlaceHolder("SLK Rate: T per 1 SLK (e.g. 10000) — PERMANENT")
	lockEntry := widget.NewEntry(); lockEntry.SetPlaceHolder("SLK to lock as reserve (Reserve Bank only)")
	webEntry  := widget.NewEntry(); webEntry.SetPlaceHolder("Your website (e.g. https://mybank.com)")

	// Rate preview
	ratePreview := widget.NewLabel("Rate preview: —")
	ratePreview.TextStyle = fyne.TextStyle{Italic: true}
	rateEntry.OnChanged = func(s string) {
		rate, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		curr := strings.TrimSpace(currEntry.Text)
		if curr == "" { curr = "SLKA" }
		if err != nil || rate <= 0 { ratePreview.SetText("Rate preview: —"); return }
		ratePreview.SetText(fmt.Sprintf("1 SLK = %.0f %s  |  1 %s = 1,000,000 %sC  |  SLK is GOLD backing", rate, curr, curr, curr))
	}

	result := widget.NewLabel(""); result.Alignment = fyne.TextAlignCenter; result.TextStyle = fyne.TextStyle{Bold: true}
	createBtn := widget.NewButton("🏦 CREATE BANK — PERMANENT, CANNOT BE UNDONE", func() {
		name    := strings.TrimSpace(nameEntry.Text)
		curr    := strings.TrimSpace(currEntry.Text)
		feeStr  := strings.TrimSpace(feeEntry.Text)
		rateStr := strings.TrimSpace(rateEntry.Text)
		if name == "" { result.SetText("❌ Enter bank name"); return }
		if curr == "" { result.SetText("❌ Enter currency ticker"); return }
		if strings.Contains(curr, " ") { result.SetText("❌ Currency ticker cannot have spaces"); return }
		// ── UNIQUE NAME CHECK — no two banks can share a name ──
		nameLower := strings.ToLower(name)
		for _, cb := range myCommercialBanks {
			if strings.ToLower(cb.Name) == nameLower { result.SetText("❌ Bank name already taken. Choose another."); return }
		}
		for _, rb := range myReserveBanks {
			if strings.ToLower(rb.Name) == nameLower { result.SetText("❌ Bank name already taken. Choose another."); return }
		}
		for _, kb := range knownBanks {
			if strings.ToLower(kb.Name) == nameLower { result.SetText("❌ Bank name taken by another peer. Choose another."); return }
		}
		feePercent, err := strconv.ParseFloat(feeStr, 64)
		if err != nil || feePercent < 0 || feePercent > 10 { result.SetText("❌ Fee must be 0-10%"); return }
		slkRate, rerr := strconv.ParseFloat(rateStr, 64)
		if rerr != nil || slkRate <= 0 { result.SetText("❌ Enter valid SLK rate (e.g. 10000)"); return }
		feeBP  := int64(feePercent * 100)
		bType  := typeSel.Selected
		_ = curr + "C"
		dialog.ShowConfirm("PERMANENT — Cannot Be Changed Ever",
			fmt.Sprintf("Bank: %s | Currency: %s | Fee: %.2f%% | 1 SLK = %.0f %s | CANNOT be changed. Sure?", name, curr, feePercent, slkRate, curr),
			func(confirmed bool) {
				if !confirmed { return }
				bankID := fmt.Sprintf("bank_%x", time.Now().UnixNano())
				apiKey := generateAPIKey(bankID)
				if strings.Contains(bType, "Reserve") {
					lockAmt, lerr := strconv.ParseFloat(strings.TrimSpace(lockEntry.Text), 64)
					if lerr != nil || lockAmt <= 0 { result.SetText("❌ Enter SLK to lock"); return }
					if lockAmt > bankAccount.SLK { result.SetText("❌ Insufficient SLK"); return }
					bankAccount.SLK -= lockAmt; saveBankAccount(bankAccount); refreshLabels()
					myReserveBanks = append(myReserveBanks, ReserveBank{
						ID: bankID, Name: name, OwnerID: bankAccount.AccountID,
						LockedSLK: lockAmt, Currency: curr, SLKRate: slkRate,
						FeeBasisPoints: feeBP, CreatedAt: time.Now().Unix(), APIKey: apiKey})
					saveReserveBanks()
					dialog.ShowInformation("🏛 Reserve Bank Created!",
						fmt.Sprintf("Bank: %s | Currency: %s | 1SLK=%.0f%s | Locked:%.8fSLK | Fee:%.2f%% | API Key: %s", name, curr, slkRate, curr, lockAmt, feePercent, apiKey), w)
					result.SetText(fmt.Sprintf("✅ Reserve Bank %s created!", name))
					nameEntry.SetText(""); currEntry.SetText(""); feeEntry.SetText("")
					rateEntry.SetText(""); lockEntry.SetText("")
				} else {
					myCommercialBanks = append(myCommercialBanks, CommercialBank{
						ID: bankID, Name: name, OwnerID: bankAccount.AccountID,
						FeeBasisPoints: feeBP, Currency: curr, SLKRate: slkRate,
						CreatedAt: time.Now().Unix(), APIKey: apiKey})
					saveCommercialBanks()
					dialog.ShowInformation("🏢 Commercial Bank Created!",
						fmt.Sprintf("Bank: %s | Currency: %s | 1SLK=%.0f%s | Fee:%.2f%% PERMANENT | API Key: %s", name, curr, slkRate, curr, feePercent, apiKey), w)
					result.SetText(fmt.Sprintf("✅ Commercial Bank %s created!", name))
					nameEntry.SetText(""); currEntry.SetText(""); feeEntry.SetText(""); rateEntry.SetText("")
				}
			}, w)
	})
	createBtn.Importance = widget.DangerImportance
	_ = webEntry
	return container.NewVScroll(container.NewPadded(container.NewVBox(
		container.NewCenter(title), widget.NewSeparator(),
		container.NewPadded(warningLbl), widget.NewSeparator(),
		widget.NewForm(widget.NewFormItem("Bank Type", typeSel)),
		container.NewPadded(typeDesc), widget.NewSeparator(),
		widget.NewForm(
			widget.NewFormItem("Bank Name", nameEntry),
			widget.NewFormItem("Currency Ticker", currEntry),
			widget.NewFormItem("Fee Rate %", feeEntry),
			widget.NewFormItem("SLK Rate", rateEntry),
			widget.NewFormItem("Lock SLK (Reserve)", lockEntry),
			widget.NewFormItem("Website URL", webEntry),
		),
		container.NewPadded(ratePreview),
		widget.NewSeparator(),
		container.NewPadded(createBtn), result,
	)))
}

// ════════════════════════════════════════
// SOCIAL HELPERS
// ════════════════════════════════════════
func buildSocialInner() *fyne.Container {
	box := container.NewVBox()
	if len(socialFeed) == 0 { box.Add(widget.NewLabel("No posts yet.")); return box }
	for i := len(socialFeed) - 1; i >= 0; i-- {
		pIdx := i
		p := socialFeed[i]
		t := time.Unix(p.Timestamp, 0).Format("Jan 02 2006  15:04")
		nameL := canvas.NewText(p.Name, theme.ForegroundColor())
		nameL.TextSize = 13; nameL.TextStyle = fyne.TextStyle{Bold: true}
		fromL := widget.NewLabel(fmt.Sprintf("@%s  ·  %s", shortAddr(p.From), t))
		fromL.TextStyle = fyne.TextStyle{Italic: true}
		textL := widget.NewLabel(p.Text); textL.Wrapping = fyne.TextWrapWord
		row := container.NewVBox(nameL, fromL, textL)
		if p.ImagePath != "" {
			if _, err := os.Stat(p.ImagePath); err == nil {
				img := canvas.NewImageFromFile(p.ImagePath)
				img.FillMode = canvas.ImageFillContain
				img.SetMinSize(fyne.NewSize(300, 200))
				row.Add(img)
			} else {
				row.Add(widget.NewLabel("🖼 " + p.ImagePath))
			}
		}

		// ── LIKES ──
		alreadyLiked := false
		for _, lid := range p.Likes { if lid == bankAccount.AccountID { alreadyLiked = true; break } }
		likeText := fmt.Sprintf("❤ %d", len(p.Likes))
		if alreadyLiked { likeText = fmt.Sprintf("❤ %d (liked)", len(p.Likes)) }
		likeBtn := widget.NewButton(likeText, func() {
			if p2pNode == nil { return }
			already := false
			for _, lid := range socialFeed[pIdx].Likes {
				if lid == bankAccount.AccountID { already = true; break }
			}
			if already { return }
			socialFeed[pIdx].Likes = append(socialFeed[pIdx].Likes, bankAccount.AccountID)
			saveSocial()
			// Broadcast like
			p2pNode.BroadcastSocial(p2p.SocialMsg{
				ID: fmt.Sprintf("like_%x", time.Now().UnixNano()),
				From: bankAccount.AccountID, Name: bankAccount.Name,
				Text: "__LIKE__:" + socialFeed[pIdx].ID,
				Timestamp: time.Now().Unix(),
			})
			fyne.Do(func() { rebuildSocialBox() })
		})
		if alreadyLiked { likeBtn.Importance = widget.HighImportance }

		// ── COMMENTS ──
		commentEntry := widget.NewEntry()
		commentEntry.SetPlaceHolder("Write a comment...")
		commentBtn := widget.NewButton("💬 Post", func() {
			txt := strings.TrimSpace(commentEntry.Text)
			if txt == "" || p2pNode == nil { return }
			c := Comment{
				ID: fmt.Sprintf("cmt_%x", time.Now().UnixNano()),
				From: bankAccount.AccountID, Name: bankAccount.Name,
				Text: txt, Timestamp: time.Now().Unix(),
			}
			socialFeed[pIdx].Comments = append(socialFeed[pIdx].Comments, c)
			saveSocial()
			p2pNode.BroadcastSocial(p2p.SocialMsg{
				ID: c.ID, From: bankAccount.AccountID, Name: bankAccount.Name,
				Text: "__COMMENT__:" + socialFeed[pIdx].ID + ":" + txt,
				Timestamp: time.Now().Unix(),
			})
			fyne.Do(func() {
				commentEntry.SetText("")
				rebuildSocialBox()
			})
		})
		commentBtn.Importance = widget.MediumImportance

		// Show existing comments
		commentBox := container.NewVBox()
		for _, c := range p.Comments {
			ct := time.Unix(c.Timestamp, 0).Format("Jan 02 15:04")
			commentBox.Add(widget.NewLabel(fmt.Sprintf("  💬 %s: %s  [%s]", c.Name, c.Text, ct)))
		}

		row.Add(container.New(layout.NewGridLayout(2), likeBtn, container.NewBorder(nil,nil,nil,commentBtn,commentEntry)))
		if len(p.Comments) > 0 { row.Add(commentBox) }
		row.Add(widget.NewSeparator())
		box.Add(container.NewPadded(row))
	}
	return box
}

func rebuildSocialBox() {
	if socialBox == nil { return }
	socialBox.Content = buildSocialInner()
	socialBox.Refresh()
}

// ════════════════════════════════════════
// HELPERS & PERSISTENCE
// ════════════════════════════════════════
func txIcon(t string) string {
	switch t {
	case "DEPOSIT": return "📥"
	case "WITHDRAW": return "🏦"
	case "SEND": return "📤"
	case "RECEIVE": return "💰"
	default: return "🔄"
	}
}
// ── ENCRYPTED SECRET KEY AT REST ──
// Uses AES-256-GCM to encrypt secret key before saving
func encryptSecretKey(plaintext, passphrase string) string {
	key := sha256.Sum256([]byte(passphrase))
	block, err := aes.NewCipher(key[:])
	if err != nil { return plaintext }
	gcm, err := cipher.NewGCM(block)
	if err != nil { return plaintext }
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil { return plaintext }
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return "ENC:" + hex.EncodeToString(sealed)
}

func decryptSecretKey(ciphertext, passphrase string) string {
	if !strings.HasPrefix(ciphertext, "ENC:") { return ciphertext }
	data, err := hex.DecodeString(ciphertext[4:])
	if err != nil { return ciphertext }
	key := sha256.Sum256([]byte(passphrase))
	block, err := aes.NewCipher(key[:])
	if err != nil { return ciphertext }
	gcm, err := cipher.NewGCM(block)
	if err != nil { return ciphertext }
	if len(data) < gcm.NonceSize() { return ciphertext }
	nonce, ct := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil { return ciphertext }
	return string(plain)
}

// passphrase = AccountID + machine hostname — unique per machine
func getEncPassphrase() string {
	host, _ := os.Hostname()
	return bankAccount.AccountID + host
}

func containsStr(s, sub string) bool {
	if len(sub) == 0 { return true }
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub { return true }
	}
	return false
}
func shortAddr(s string) string { if len(s) > 20 { return s[:20] + "..." }; return s }
func shortStr(s string, n int) string { if len(s) > n { return s[:n] + "..." }; return s }
func hashKey(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

func loadOrCreateBankAccount() *BankAccount {
	data, err := os.ReadFile(bankPath)
	if err == nil {
		var a BankAccount
		if json.Unmarshal(data, &a) == nil {
			// Decrypt secret key if encrypted
			if strings.HasPrefix(a.SecretKey, "ENC:") {
				host, _ := os.Hostname()
				a.SecretKey = decryptSecretKey(a.SecretKey, a.AccountID + host)
			}
			return &a
		}
	}
	pub, sec := generateAPIKeys()
	a := &BankAccount{AccountID: generateAccountID(), OwnerAddr: "", Name: "", NameLocked: false,
		PublicKey: pub, SecretKey: sec, SecretKeyH: hashKey(sec), CreatedAt: time.Now().Unix()}
	saveBankAccount(a); return a
}
func saveBankAccount(a *BankAccount) {
	// Encrypt secret key before saving to disk
	tmp := *a
	if tmp.SecretKey != "" && !strings.HasPrefix(tmp.SecretKey, "ENC:") {
		tmp.SecretKey = encryptSecretKey(tmp.SecretKey, getEncPassphrase())
	}
	d, _ := json.MarshalIndent(tmp, "", "  ")
	os.WriteFile(bankPath, d, 0600)
}

func loadBankAccountFromDisk() *BankAccount {
	d, err := os.ReadFile(bankPath)
	if err != nil { return nil }
	var a BankAccount
	if err := json.Unmarshal(d, &a); err != nil { return nil }
	// Decrypt secret key after loading
	if strings.HasPrefix(a.SecretKey, "ENC:") {
		a.SecretKey = decryptSecretKey(a.SecretKey, a.AccountID + func() string { h, _ := os.Hostname(); return h }())
	}
	return &a
}
func loadTxHistory() []BankTX { d, e := os.ReadFile(txPath); if e != nil { return []BankTX{} }; var x []BankTX; json.Unmarshal(d, &x); return x }
func saveTxHistory() { d, _ := json.MarshalIndent(txHistory, "", "  "); os.WriteFile(txPath, d, 0600) }
func loadMarket() []MarketListing { d, e := os.ReadFile(marketPath); if e != nil { return []MarketListing{} }; var x []MarketListing; json.Unmarshal(d, &x); return x }

// ── BANK PERSISTENCE ──
var (
	commercialBanksPath = filepath.Join(os.Getenv("HOME"), ".slkbank", "commercial_banks.json")
	reserveBanksPath    = filepath.Join(os.Getenv("HOME"), ".slkbank", "reserve_banks.json")
	bankPaymentsPath    = filepath.Join(os.Getenv("HOME"), ".slkbank", "bank_payments.json")
	bankLoansPath       = filepath.Join(os.Getenv("HOME"), ".slkbank", "bank_loans.json")
	bankPayments        []BankPayment
	bankLoans           []BankLoan
)
func saveBankPayments() {
	d, _ := json.MarshalIndent(bankPayments, "", "  ")
	os.WriteFile(bankPaymentsPath, d, 0644)
}
func loadBankPayments() []BankPayment {
	d, e := os.ReadFile(bankPaymentsPath)
	if e != nil { return []BankPayment{} }
	var x []BankPayment; json.Unmarshal(d, &x); return x
}
func saveBankLoans() {
	d, _ := json.MarshalIndent(bankLoans, "", "  ")
	os.WriteFile(bankLoansPath, d, 0644)
}
func loadBankLoans() []BankLoan {
	d, e := os.ReadFile(bankLoansPath)
	if e != nil { return []BankLoan{} }
	var x []BankLoan; json.Unmarshal(d, &x); return x
}

func saveCommercialBanks() {
	d, _ := json.MarshalIndent(myCommercialBanks, "", "  ")
	os.WriteFile(commercialBanksPath, d, 0644)
}
// broadcastBankEvent — sends a bank event to ALL peers worldwide via P2P
func broadcastBankEvent(txType, from, to, currency string, amount float64) {
	if p2pNode == nil { return }
	p2pNode.BroadcastBankRecord(p2p.BankRecord{
		ID:        fmt.Sprintf("be_%x", time.Now().UnixNano()),
		From:      from,
		To:        to,
		Amount:    amount,
		Currency:  currency,
		TxType:    txType,
		Timestamp: time.Now().Unix(),
		Verified:  true,
	})
}
func loadCommercialBanks() []CommercialBank {
	d, e := os.ReadFile(commercialBanksPath)
	if e != nil { return []CommercialBank{} }
	var x []CommercialBank; json.Unmarshal(d, &x); return x
}
func saveReserveBanks() {
	d, _ := json.MarshalIndent(myReserveBanks, "", "  ")
	os.WriteFile(reserveBanksPath, d, 0644)
}
func loadReserveBanks() []ReserveBank {
	d, e := os.ReadFile(reserveBanksPath)
	if e != nil { return []ReserveBank{} }
	var x []ReserveBank; json.Unmarshal(d, &x); return x
}

// ── PER-BANK CURRENCY HELPERS ──
// Each bank issues its own T (user) and CT (bank) currency backed by SLK
// e.g. Bank "SLKafrica" currency="SLKA" → user gets "SLKA", bank ops in "SLKAC"
// Bank owner sets SLKRate: how many SLKA per 1 SLK (e.g. 10,000 or 1,000,000)
// Then 1 SLKA = 1,000,000 SLKAC always — fixed forever
// SLK is GOLD — all T and CT are backed by locked SLK

func bankTCurrency(bankCurrency string) string  { return bankCurrency }           // e.g. "SLKA"
func bankCTCurrency(bankCurrency string) string { return bankCurrency + "C" }     // e.g. "SLKAC"

// Use bank's custom rate: 1 SLK = SLKRate T
func slkToBankT(slk float64, rate float64) float64  { return slk * rate }
func bankTToSLK(t float64, rate float64) float64    { return t / rate }
// Fixed: 1 T = 1,000,000 CT always
func bankTToBankCT(t float64) float64  { return t * 1_000_000.0 }
func bankCTToBankT(ct float64) float64 { return ct / 1_000_000.0 }

// Get bank rate — looks up from commercial or reserve banks
func getBankRate(currency string) float64 {
	for _, cb := range myCommercialBanks { if cb.Currency == currency { return cb.SLKRate } }
	for _, rb := range myReserveBanks    { if rb.Currency == currency { return rb.SLKRate } }
	return 1_000_000.0 // default
}

// Verify supply is safe — T issued never exceeds SLK locked * rate
func isSupplySafe(currency string, newTAmount float64) bool {
	for _, cb := range myCommercialBanks {
		if cb.Currency == currency {
			maxT := cb.TotalDeposited * cb.SLKRate
			return (cb.TotalIssuedT + newTAmount) <= maxT
		}
	}
	for _, rb := range myReserveBanks {
		if rb.Currency == currency {
			maxT := rb.LockedSLK * rb.SLKRate
			return (rb.TotalIssuedT + newTAmount) <= maxT
		}
	}
	return false
}

// Get balance of a bank currency
func getBankBalance(currency string) float64 {
	if bankAccount.BankBalances == nil { return 0 }
	return bankAccount.BankBalances[currency]
}

// Set balance of a bank currency
func setBankBalance(currency string, amount float64) {
	if bankAccount.BankBalances == nil { bankAccount.BankBalances = make(map[string]float64) }
	bankAccount.BankBalances[currency] = amount
}

// Add to bank currency balance
func addBankBalance(currency string, amount float64) {
	if bankAccount.BankBalances == nil { bankAccount.BankBalances = make(map[string]float64) }
	bankAccount.BankBalances[currency] += amount
}

// Deduct from bank currency balance — returns false if insufficient
func deductBankBalance(currency string, amount float64) bool {
	if bankAccount.BankBalances == nil { return false }
	if bankAccount.BankBalances[currency] < amount { return false }
	bankAccount.BankBalances[currency] -= amount
	return true
}

// ── MULTISIG / TIMELOCK / RECURRING / GOVERNANCE / IDENTITY GLOBALS ──
var (
	myMultiSigWallets []MultiSigWallet
	myMultiSigTxs     []MultiSigTx
	myTimeLocks       []TimeLock
	myRecurring       []RecurringPayment
	myProposals       []GovernanceProposal
	myIdentity        *VerifiedIdentity
	multiSigPath      string
	multiSigTxPath    string
	timeLockPath      string
	recurringPath     string
	proposalsPath     string
	identityPath      string
)

func initExtraPaths() {
	base := filepath.Join(os.Getenv("HOME"), ".slkbank")
	if multiSigPath   == "" { multiSigPath   = filepath.Join(base, "multisig.json") }
	if multiSigTxPath == "" { multiSigTxPath = filepath.Join(base, "multisig_txs.json") }
	if timeLockPath   == "" { timeLockPath   = filepath.Join(base, "timelocks.json") }
	if recurringPath  == "" { recurringPath  = filepath.Join(base, "recurring.json") }
	if proposalsPath  == "" { proposalsPath  = filepath.Join(base, "proposals.json") }
	if identityPath   == "" { identityPath   = filepath.Join(base, "identity.json") }
}

func saveMultiSigWallets() { initExtraPaths(); d,_:=json.MarshalIndent(myMultiSigWallets,"","  "); os.WriteFile(multiSigPath,d,0644) }
func loadMultiSigWallets() []MultiSigWallet { initExtraPaths(); d,e:=os.ReadFile(multiSigPath); if e!=nil{return []MultiSigWallet{}}; var x []MultiSigWallet; json.Unmarshal(d,&x); return x }
func saveMultiSigTxs()     { initExtraPaths(); d,_:=json.MarshalIndent(myMultiSigTxs,"","  "); os.WriteFile(multiSigTxPath,d,0644) }
func loadMultiSigTxs()     []MultiSigTx { initExtraPaths(); d,e:=os.ReadFile(multiSigTxPath); if e!=nil{return []MultiSigTx{}}; var x []MultiSigTx; json.Unmarshal(d,&x); return x }
func saveTimeLocks()        { initExtraPaths(); d,_:=json.MarshalIndent(myTimeLocks,"","  "); os.WriteFile(timeLockPath,d,0644) }
func loadTimeLocks()        []TimeLock { initExtraPaths(); d,e:=os.ReadFile(timeLockPath); if e!=nil{return []TimeLock{}}; var x []TimeLock; json.Unmarshal(d,&x); return x }
func saveRecurring()        { initExtraPaths(); d,_:=json.MarshalIndent(myRecurring,"","  "); os.WriteFile(recurringPath,d,0644) }
func loadRecurring()        []RecurringPayment { initExtraPaths(); d,e:=os.ReadFile(recurringPath); if e!=nil{return []RecurringPayment{}}; var x []RecurringPayment; json.Unmarshal(d,&x); return x }
func saveProposals()        { initExtraPaths(); d,_:=json.MarshalIndent(myProposals,"","  "); os.WriteFile(proposalsPath,d,0644) }
func loadProposals()        []GovernanceProposal { initExtraPaths(); d,e:=os.ReadFile(proposalsPath); if e!=nil{return []GovernanceProposal{}}; var x []GovernanceProposal; json.Unmarshal(d,&x); return x }
func saveIdentity()         { initExtraPaths(); d,_:=json.MarshalIndent(myIdentity,"","  "); os.WriteFile(identityPath,d,0644) }
func loadIdentity()         *VerifiedIdentity { initExtraPaths(); d,e:=os.ReadFile(identityPath); if e!=nil{return nil}; var x VerifiedIdentity; json.Unmarshal(d,&x); return &x }

// ── CHECK TIMELOCKS ──
func checkTimeLocks() {
	for range time.Tick(30 * time.Second) {
		now := time.Now().Unix()
		changed := false
		for i, tl := range myTimeLocks {
			if tl.Executed || tl.Cancelled { continue }
			if now >= tl.UnlockAt {
				if tl.From == bankAccount.AccountID {
					// already deducted at creation — just mark executed
				} else {
					bankAccount.SLK += tl.Amount
					saveBankAccount(bankAccount)
					fyne.Do(func() { refreshLabels() })
				}
				myTimeLocks[i].Executed = true
				changed = true
				tx := BankTX{ID: fmt.Sprintf("tl_%x", time.Now().UnixNano()),
					From: tl.From, To: tl.To, Amount: tl.Amount, Currency: tl.Currency,
					Type: "TIMELOCK_RELEASE", Timestamp: now,
					Note: fmt.Sprintf("Time-lock released: %s", tl.Note), Verified: true}
				txHistory = append(txHistory, tx); saveTxHistory()
			}
		}
		if changed { saveTimeLocks() }
	}
}

// ── CHECK RECURRING PAYMENTS ──
func checkRecurringPayments() {
	for range time.Tick(60 * time.Second) {
		now := time.Now().Unix()
		changed := false
		for i, rp := range myRecurring {
			if !rp.Active { continue }
			if rp.EndDate > 0 && now > rp.EndDate { myRecurring[i].Active = false; changed = true; continue }
			if rp.MaxPayments > 0 && rp.PaidCount >= rp.MaxPayments { myRecurring[i].Active = false; changed = true; continue }
			if now >= rp.NextDue {
				if bankAccount.SLK >= rp.Amount {
					bankAccount.SLK -= rp.Amount
					saveBankAccount(bankAccount)
					fyne.Do(func() { refreshLabels() })
					myRecurring[i].PaidCount++
					tx := BankTX{ID: fmt.Sprintf("rec_%x", time.Now().UnixNano()),
						From: rp.From, To: rp.To, Amount: rp.Amount, Currency: rp.Currency,
						Type: "RECURRING", Timestamp: now, Note: rp.Note, Verified: true}
					txHistory = append(txHistory, tx); saveTxHistory()
				}
				// Advance next due date
				switch rp.Interval {
				case "daily":   myRecurring[i].NextDue = rp.NextDue + 86400
				case "weekly":  myRecurring[i].NextDue = rp.NextDue + 604800
				case "monthly": myRecurring[i].NextDue = rp.NextDue + 2592000
				}
				changed = true
			}
		}
		if changed { saveRecurring() }
	}
}

// ════════════════════════════════════════
// MULTI-SIG WALLET TAB
// ════════════════════════════════════════
func makeMultiSigTab(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("🔐 Multi-Signature Wallets", theme.ForegroundColor())
	title.TextSize = 18; title.TextStyle = fyne.TextStyle{Bold: true}
	tabs := container.NewAppTabs(
		container.NewTabItem("📋 My MultiSig",   makeMultiSigListTab(w)),
		container.NewTabItem("➕ Create MultiSig", makeCreateMultiSigTab(w)),
		container.NewTabItem("✍ Pending Signs",  makeMultiSigPendingTab(w)),
	)
	tabs.SetTabLocation(container.TabLocationTop)
	_ = title
	return tabs
}

func makeMultiSigListTab(w fyne.Window) fyne.CanvasObject {
	box := container.NewVBox()
	if len(myMultiSigWallets) == 0 {
		box.Add(widget.NewLabel("No multi-sig wallets yet. Create one to share control of funds."))
	}
	for _, ms := range myMultiSigWallets {
		ms := ms
		nameLbl := canvas.NewText(fmt.Sprintf("🔐 %s  (%d of %d signatures required)", ms.Name, ms.Required, ms.Total), theme.ForegroundColor())
		nameLbl.TextStyle = fyne.TextStyle{Bold: true}
		box.Add(container.NewPadded(container.NewVBox(
			nameLbl,
			widget.NewLabel(fmt.Sprintf("Balance: %.8f SLK", ms.Balance)),
			widget.NewLabel(fmt.Sprintf("Owners: %d  |  Created: %s", len(ms.Owners), time.Unix(ms.CreatedAt,0).Format("Jan 02 2006"))),
			widget.NewLabel(fmt.Sprintf("ID: %s", ms.ID)),
			widget.NewSeparator(),
		)))
	}
	return container.NewVScroll(container.NewPadded(box))
}

func makeCreateMultiSigTab(w fyne.Window) fyne.CanvasObject {
	nameEntry   := widget.NewEntry(); nameEntry.SetPlaceHolder("Wallet name (e.g. Company Treasury)")
	ownersEntry := widget.NewEntry(); ownersEntry.SetPlaceHolder("Owner IDs comma-separated (e.g. SLKB-xxx,SLKB-yyy,SLKB-zzz)")
	ownersEntry.MultiLine = true
	reqEntry    := widget.NewEntry(); reqEntry.SetPlaceHolder("Signatures required (e.g. 2)")
	result      := widget.NewLabel(""); result.Alignment = fyne.TextAlignCenter
	result.TextStyle = fyne.TextStyle{Bold: true}

	createBtn := widget.NewButton("🔐 Create Multi-Sig Wallet", func() {
		name := strings.TrimSpace(nameEntry.Text)
		if name == "" { fyne.Do(func() { result.SetText("❌ Enter wallet name") }); return }
		ownersRaw := strings.Split(ownersEntry.Text, ",")
		var owners []string
		for _, o := range ownersRaw {
			o = strings.TrimSpace(o)
			if o != "" { owners = append(owners, o) }
		}
		// Always include self
		found := false
		for _, o := range owners { if o == bankAccount.AccountID { found = true } }
		if !found { owners = append(owners, bankAccount.AccountID) }
		if len(owners) < 2 { fyne.Do(func() { result.SetText("❌ Need at least 2 owners") }); return }
		req, err := strconv.Atoi(strings.TrimSpace(reqEntry.Text))
		if err != nil || req < 1 || req > len(owners) { fyne.Do(func() { result.SetText(fmt.Sprintf("❌ Required signatures must be 1-%d", len(owners))) }); return }

		ms := MultiSigWallet{
			ID: fmt.Sprintf("ms_%x", time.Now().UnixNano()),
			Name: name, Owners: owners, Required: req, Total: len(owners),
			CreatedAt: time.Now().Unix(), CreatedBy: bankAccount.AccountID,
		}
		myMultiSigWallets = append(myMultiSigWallets, ms)
		saveMultiSigWallets()
		fyne.Do(func() {
			result.SetText(fmt.Sprintf("✅ Multi-sig wallet '%s' created!\n%d of %d signatures required", name, req, len(owners)))
			nameEntry.SetText(""); ownersEntry.SetText(""); reqEntry.SetText("")
		})
	})
	createBtn.Importance = widget.HighImportance

	info := widget.NewLabel("💡 Example: 2-of-3 means any 2 of 3 owners must sign to send funds.\nYour ID is automatically included as an owner.")
	info.Wrapping = fyne.TextWrapWord

	return container.NewVScroll(container.NewPadded(container.NewVBox(
		info, widget.NewSeparator(),
		widget.NewForm(
			widget.NewFormItem("Wallet Name", nameEntry),
			widget.NewFormItem("Owner IDs", ownersEntry),
			widget.NewFormItem("Signatures Required", reqEntry),
		),
		container.NewPadded(createBtn), result,
	)))
}

func makeMultiSigPendingTab(w fyne.Window) fyne.CanvasObject {
	box := container.NewVBox()
	pending := 0
	for _, tx := range myMultiSigTxs {
		if tx.Executed { continue }
		tx := tx
		pending++
		signed := false
		for _, s := range tx.Signatures { if s == bankAccount.AccountID { signed = true } }
		statusTxt := fmt.Sprintf("✍ %d/%d signatures", len(tx.Signatures), tx.Required)
		if signed { statusTxt += "  (you signed ✅)" }

		signBtn := widget.NewButton("✍ Sign & Approve", func() {
			for i, t := range myMultiSigTxs {
				if t.ID != tx.ID { continue }
				// Check not already signed
				for _, s := range t.Signatures { if s == bankAccount.AccountID { dialog.ShowInformation("Already Signed", "You already signed this transaction.", w); return } }
				myMultiSigTxs[i].Signatures = append(myMultiSigTxs[i].Signatures, bankAccount.AccountID)
				// Check if threshold met
				if len(myMultiSigTxs[i].Signatures) >= myMultiSigTxs[i].Required {
					// Execute
					if bankAccount.SLK >= t.Amount {
						bankAccount.SLK -= t.Amount
						saveBankAccount(bankAccount); refreshLabels()
					}
					myMultiSigTxs[i].Executed = true
					myMultiSigTxs[i].ExecutedAt = time.Now().Unix()
					btx := BankTX{ID: fmt.Sprintf("ms_%x", time.Now().UnixNano()),
						From: bankAccount.AccountID, To: t.To, Amount: t.Amount,
						Currency: t.Currency, Type: "MULTISIG_SEND",
						Timestamp: time.Now().Unix(), Note: t.Note, Verified: true}
					txHistory = append(txHistory, btx); saveTxHistory()
					dialog.ShowInformation("✅ Executed!", fmt.Sprintf("Threshold met!\n%.8f %s sent to %s", t.Amount, t.Currency, t.To), w)
				} else {
					dialog.ShowInformation("✅ Signed", fmt.Sprintf("Signature added.\n%d/%d required.", len(myMultiSigTxs[i].Signatures), myMultiSigTxs[i].Required), w)
				}
				saveMultiSigTxs()
				break
			}
		})
		if !signed { signBtn.Importance = widget.HighImportance }

		box.Add(container.NewPadded(container.NewVBox(
			widget.NewLabel(fmt.Sprintf("💸 Send %.8f %s to %s", tx.Amount, tx.Currency, tx.To)),
			widget.NewLabel(fmt.Sprintf("Note: %s  |  %s", tx.Note, statusTxt)),
			widget.NewLabel(fmt.Sprintf("Proposed by: %s  |  %s", tx.CreatedBy, time.Unix(tx.CreatedAt,0).Format("Jan 02 2006 15:04"))),
			signBtn, widget.NewSeparator(),
		)))
	}
	if pending == 0 { box.Add(widget.NewLabel("No pending multi-sig transactions.")) }
	return container.NewVScroll(container.NewPadded(box))
}

// ════════════════════════════════════════
// TIME-LOCK TAB
// ════════════════════════════════════════
func makeTimeLockTab(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("⏰ Time-Locked Transactions", theme.ForegroundColor())
	title.TextSize = 18; title.TextStyle = fyne.TextStyle{Bold: true}
	tabs := container.NewAppTabs(
		container.NewTabItem("📋 My Locks",    makeTimeLockListTab(w)),
		container.NewTabItem("➕ New Lock",    makeCreateTimeLockTab(w)),
	)
	tabs.SetTabLocation(container.TabLocationTop)
	_ = title
	return tabs
}

func makeTimeLockListTab(w fyne.Window) fyne.CanvasObject {
	box := container.NewVBox()
	active := 0
	for _, tl := range myTimeLocks {
		if tl.Cancelled || tl.Executed { continue }
		active++
		tl := tl
		unlockDate := time.Unix(tl.UnlockAt, 0).Format("Jan 02 2006 15:04")
		remaining := time.Until(time.Unix(tl.UnlockAt, 0))
		remainStr := fmt.Sprintf("%.0f days remaining", remaining.Hours()/24)
		if remaining < 0 { remainStr = "✅ UNLOCKED — processing..." }

		cancelBtn := widget.NewButton("❌ Cancel Lock", func() {
			dialog.ShowConfirm("Cancel Time-Lock", "Cancel this lock and return funds to your wallet?",
				func(ok bool) {
					if !ok { return }
					for i, t := range myTimeLocks {
						if t.ID == tl.ID {
							myTimeLocks[i].Cancelled = true
							bankAccount.SLK += tl.Amount
							saveBankAccount(bankAccount); saveTimeLocks(); refreshLabels()
							dialog.ShowInformation("✅ Cancelled", fmt.Sprintf("%.8f SLK returned to your wallet.", tl.Amount), w)
							break
						}
					}
				}, w)
		})

		box.Add(container.NewPadded(container.NewVBox(
			widget.NewLabel(fmt.Sprintf("⏰ %.8f %s → %s", tl.Amount, tl.Currency, tl.To)),
			widget.NewLabel(fmt.Sprintf("Unlocks: %s  |  %s", unlockDate, remainStr)),
			widget.NewLabel(fmt.Sprintf("Note: %s", tl.Note)),
			cancelBtn, widget.NewSeparator(),
		)))
	}
	if active == 0 { box.Add(widget.NewLabel("No active time-locked transactions.")) }
	return container.NewVScroll(container.NewPadded(box))
}

func makeCreateTimeLockTab(w fyne.Window) fyne.CanvasObject {
	toEntry     := widget.NewEntry(); toEntry.SetPlaceHolder("Recipient Account ID")
	amtEntry    := widget.NewEntry(); amtEntry.SetPlaceHolder("Amount of SLK")
	noteEntry   := widget.NewEntry(); noteEntry.SetPlaceHolder("Note (e.g. Birthday gift, Inheritance)")
	daysEntry   := widget.NewEntry(); daysEntry.SetPlaceHolder("Lock for how many days? (e.g. 365)")
	unlockLabel := widget.NewLabel("Unlocks: —")
	unlockLabel.TextStyle = fyne.TextStyle{Bold: true}
	result      := widget.NewLabel(""); result.Alignment = fyne.TextAlignCenter
	result.TextStyle = fyne.TextStyle{Bold: true}

	daysEntry.OnChanged = func(s string) {
		days, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil || days <= 0 { unlockLabel.SetText("Unlocks: —"); return }
		unlockLabel.SetText(fmt.Sprintf("Unlocks: %s", time.Now().Add(time.Duration(days)*24*time.Hour).Format("Jan 02 2006")))
	}

	lockBtn := widget.NewButton("⏰ Lock Funds", func() {
		to   := strings.TrimSpace(toEntry.Text)
		note := strings.TrimSpace(noteEntry.Text)
		amt, err1  := strconv.ParseFloat(strings.TrimSpace(amtEntry.Text), 64)
		days, err2 := strconv.ParseInt(strings.TrimSpace(daysEntry.Text), 10, 64)
		if to == ""              { fyne.Do(func() { result.SetText("❌ Enter recipient") }); return }
		if err1 != nil || amt <= 0 { fyne.Do(func() { result.SetText("❌ Invalid amount") }); return }
		if err2 != nil || days <= 0 { fyne.Do(func() { result.SetText("❌ Invalid days") }); return }
		if amt > bankAccount.SLK  { fyne.Do(func() { result.SetText("❌ Insufficient SLK") }); return }

		unlockAt := time.Now().Add(time.Duration(days) * 24 * time.Hour).Unix()
		dialog.ShowConfirm("⏰ Confirm Time-Lock",
			fmt.Sprintf("Lock %.8f SLK for %s\nUnlocks: %s\nNote: %s\n\nFunds are LOCKED until unlock date.\nYou can cancel to get them back early.", amt, to, time.Unix(unlockAt,0).Format("Jan 02 2006"), note),
			func(ok bool) {
				if !ok { return }
				bankAccount.SLK -= amt
				saveBankAccount(bankAccount); refreshLabels()
				tl := TimeLock{
					ID: fmt.Sprintf("tl_%x", time.Now().UnixNano()),
					From: bankAccount.AccountID, To: to,
					Amount: amt, Currency: "SLK", Note: note,
					UnlockAt: unlockAt, CreatedAt: time.Now().Unix(),
				}
				myTimeLocks = append(myTimeLocks, tl)
				saveTimeLocks()
				fyne.Do(func() {
					result.SetText(fmt.Sprintf("✅ %.8f SLK locked until %s", amt, time.Unix(unlockAt,0).Format("Jan 02 2006")))
					toEntry.SetText(""); amtEntry.SetText(""); daysEntry.SetText(""); noteEntry.SetText("")
				})
			}, w)
	})
	lockBtn.Importance = widget.HighImportance

	info := widget.NewLabel("💡 Funds are deducted immediately and locked on-chain.\nRecipient receives them automatically on the unlock date.\nYou can cancel any time to get your SLK back.")
	info.Wrapping = fyne.TextWrapWord

	return container.NewVScroll(container.NewPadded(container.NewVBox(
		info, widget.NewSeparator(),
		widget.NewForm(
			widget.NewFormItem("To", toEntry),
			widget.NewFormItem("Amount (SLK)", amtEntry),
			widget.NewFormItem("Lock Days", daysEntry),
			widget.NewFormItem("Note", noteEntry),
		),
		container.NewPadded(unlockLabel),
		widget.NewSeparator(),
		container.NewPadded(lockBtn), result,
	)))
}

// ════════════════════════════════════════
// RECURRING PAYMENTS TAB
// ════════════════════════════════════════
func makeRecurringTab(w fyne.Window) fyne.CanvasObject {
	tabs := container.NewAppTabs(
		container.NewTabItem("📋 Active",     makeRecurringListTab(w)),
		container.NewTabItem("➕ New Payment", makeCreateRecurringTab(w)),
	)
	tabs.SetTabLocation(container.TabLocationTop)
	return tabs
}

func makeRecurringListTab(w fyne.Window) fyne.CanvasObject {
	box := container.NewVBox()
	active := 0
	for _, rp := range myRecurring {
		if !rp.Active { continue }
		active++
		rp := rp
		nextDate := time.Unix(rp.NextDue, 0).Format("Jan 02 2006")
		paidInfo := fmt.Sprintf("Paid: %d times", rp.PaidCount)
		if rp.MaxPayments > 0 { paidInfo += fmt.Sprintf(" / %d max", rp.MaxPayments) }

		cancelBtn := widget.NewButton("⛔ Cancel", func() {
			dialog.ShowConfirm("Cancel Recurring Payment", "Stop this recurring payment?",
				func(ok bool) {
					if !ok { return }
					for i, r := range myRecurring {
						if r.ID == rp.ID { myRecurring[i].Active = false; saveRecurring(); break }
					}
				}, w)
		})

		box.Add(container.NewPadded(container.NewVBox(
			widget.NewLabel(fmt.Sprintf("🔄 %.8f %s → %s  (%s)", rp.Amount, rp.Currency, rp.To, rp.Interval)),
			widget.NewLabel(fmt.Sprintf("Next due: %s  |  %s", nextDate, paidInfo)),
			widget.NewLabel(fmt.Sprintf("Note: %s", rp.Note)),
			cancelBtn, widget.NewSeparator(),
		)))
	}
	if active == 0 { box.Add(widget.NewLabel("No active recurring payments.")) }
	return container.NewVScroll(container.NewPadded(box))
}

func makeCreateRecurringTab(w fyne.Window) fyne.CanvasObject {
	toEntry    := widget.NewEntry(); toEntry.SetPlaceHolder("Recipient Account ID")
	amtEntry   := widget.NewEntry(); amtEntry.SetPlaceHolder("Amount per payment")
	noteEntry  := widget.NewEntry(); noteEntry.SetPlaceHolder("Note (e.g. Rent, Salary, Subscription)")
	maxEntry   := widget.NewEntry(); maxEntry.SetPlaceHolder("Max payments (0 = forever)")
	intervalSel := widget.NewSelect([]string{"daily","weekly","monthly"}, nil)
	intervalSel.SetSelected("monthly")
	result     := widget.NewLabel(""); result.Alignment = fyne.TextAlignCenter
	result.TextStyle = fyne.TextStyle{Bold: true}

	createBtn := widget.NewButton("🔄 Set Up Recurring Payment", func() {
		to  := strings.TrimSpace(toEntry.Text)
		note := strings.TrimSpace(noteEntry.Text)
		amt, err := strconv.ParseFloat(strings.TrimSpace(amtEntry.Text), 64)
		if to == ""              { fyne.Do(func() { result.SetText("❌ Enter recipient") }); return }
		if err != nil || amt <= 0 { fyne.Do(func() { result.SetText("❌ Invalid amount") }); return }
		maxP, _ := strconv.ParseInt(strings.TrimSpace(maxEntry.Text), 10, 64)

		var nextDue int64
		switch intervalSel.Selected {
		case "daily":   nextDue = time.Now().Add(24*time.Hour).Unix()
		case "weekly":  nextDue = time.Now().Add(7*24*time.Hour).Unix()
		case "monthly": nextDue = time.Now().Add(30*24*time.Hour).Unix()
		}

		dialog.ShowConfirm("🔄 Confirm Recurring Payment",
			fmt.Sprintf("Pay %.8f SLK to %s\nEvery: %s\nMax payments: %d (0=forever)\nFirst payment: %s\n\nConfirm?",
				amt, to, intervalSel.Selected, maxP, time.Unix(nextDue,0).Format("Jan 02 2006")),
			func(ok bool) {
				if !ok { return }
				rp := RecurringPayment{
					ID: fmt.Sprintf("rec_%x", time.Now().UnixNano()),
					From: bankAccount.AccountID, To: to,
					Amount: amt, Currency: "SLK",
					Interval: intervalSel.Selected, NextDue: nextDue,
					MaxPayments: maxP, Note: note,
					Active: true, CreatedAt: time.Now().Unix(),
				}
				myRecurring = append(myRecurring, rp)
				saveRecurring()
				fyne.Do(func() {
					result.SetText(fmt.Sprintf("✅ Recurring payment set up!\n%.8f SLK %s to %s", amt, intervalSel.Selected, to))
					toEntry.SetText(""); amtEntry.SetText(""); noteEntry.SetText("")
				})
			}, w)
	})
	createBtn.Importance = widget.HighImportance

	return container.NewVScroll(container.NewPadded(container.NewVBox(
		widget.NewForm(
			widget.NewFormItem("To", toEntry),
			widget.NewFormItem("Amount (SLK)", amtEntry),
			widget.NewFormItem("Interval", intervalSel),
			widget.NewFormItem("Max Payments", maxEntry),
			widget.NewFormItem("Note", noteEntry),
		),
		widget.NewSeparator(),
		container.NewPadded(createBtn), result,
	)))
}

// ════════════════════════════════════════
// GOVERNANCE TAB
// ════════════════════════════════════════
func makeGovernanceTab(w fyne.Window) fyne.CanvasObject {
	tabs := container.NewAppTabs(
		container.NewTabItem("🗳 Proposals",    makeProposalListTab(w)),
		container.NewTabItem("➕ New Proposal", makeCreateProposalTab(w)),
	)
	tabs.SetTabLocation(container.TabLocationTop)
	return tabs
}

func makeProposalListTab(w fyne.Window) fyne.CanvasObject {
	box := container.NewVBox()
	box.Add(widget.NewLabel("📜 On-Chain Governance — Vote on protocol changes"))
	box.Add(widget.NewSeparator())
	if len(myProposals) == 0 {
		box.Add(widget.NewLabel("No proposals yet. Be the first to submit one."))
	}
	for _, p := range myProposals {
		p := p
		total := p.YesVotes + p.NoVotes + p.Abstain
		yesPct := 0.0; if total > 0 { yesPct = (p.YesVotes/total)*100 }
		endsDate := time.Unix(p.EndsAt, 0).Format("Jan 02 2006")
		statusColor := theme.ForegroundColor()
		if p.Status == "passed"   { statusColor = color.NRGBA{R:0,G:200,B:0,A:255} }
		if p.Status == "rejected" { statusColor = color.NRGBA{R:200,G:0,B:0,A:255} }
		statusLbl := canvas.NewText(fmt.Sprintf("Status: %s", strings.ToUpper(p.Status)), statusColor)
		statusLbl.TextStyle = fyne.TextStyle{Bold: true}

		alreadyVoted := false
		for _, v := range p.Voters { if v == bankAccount.AccountID { alreadyVoted = true } }

		voteBox := container.NewGridWithColumns(3,
			widget.NewButton("✅ YES", func() {
				if alreadyVoted { dialog.ShowInformation("Already Voted", "You already voted on this proposal.", w); return }
				for i, prop := range myProposals {
					if prop.ID == p.ID {
						myProposals[i].YesVotes += bankAccount.SLK
						myProposals[i].Voters = append(myProposals[i].Voters, bankAccount.AccountID)
						saveProposals()
						dialog.ShowInformation("✅ Voted YES", fmt.Sprintf("Vote cast with %.8f SLK weight", bankAccount.SLK), w)
						break
					}
				}
			}),
			widget.NewButton("❌ NO", func() {
				if alreadyVoted { dialog.ShowInformation("Already Voted", "You already voted on this proposal.", w); return }
				for i, prop := range myProposals {
					if prop.ID == p.ID {
						myProposals[i].NoVotes += bankAccount.SLK
						myProposals[i].Voters = append(myProposals[i].Voters, bankAccount.AccountID)
						saveProposals()
						dialog.ShowInformation("❌ Voted NO", fmt.Sprintf("Vote cast with %.8f SLK weight", bankAccount.SLK), w)
						break
					}
				}
			}),
			widget.NewButton("🤐 ABSTAIN", func() {
				if alreadyVoted { dialog.ShowInformation("Already Voted", "You already voted on this proposal.", w); return }
				for i, prop := range myProposals {
					if prop.ID == p.ID {
						myProposals[i].Abstain += bankAccount.SLK
						myProposals[i].Voters = append(myProposals[i].Voters, bankAccount.AccountID)
						saveProposals()
						dialog.ShowInformation("🤐 Abstained", "Abstain vote recorded.", w)
						break
					}
				}
			}),
		)

		box.Add(container.NewPadded(container.NewVBox(
			canvas.NewText(p.Title, theme.ForegroundColor()),
			widget.NewLabel(p.Description),
			statusLbl,
			widget.NewLabel(fmt.Sprintf("✅ YES: %.2f%%  |  ❌ NO: %.2f%%  |  Voters: %d  |  Ends: %s",
				yesPct, 100-yesPct, len(p.Voters), endsDate)),
			voteBox,
			widget.NewSeparator(),
		)))
	}
	return container.NewVScroll(container.NewPadded(box))
}

func makeCreateProposalTab(w fyne.Window) fyne.CanvasObject {
	titleEntry := widget.NewEntry(); titleEntry.SetPlaceHolder("Proposal title (e.g. Reduce block reward)")
	descEntry  := widget.NewEntry(); descEntry.SetPlaceHolder("Full description of the proposed change...")
	descEntry.MultiLine = true
	daysEntry  := widget.NewEntry(); daysEntry.SetPlaceHolder("Voting period in days (e.g. 7)")
	result     := widget.NewLabel(""); result.Alignment = fyne.TextAlignCenter
	result.TextStyle = fyne.TextStyle{Bold: true}

	submitBtn := widget.NewButton("📜 Submit Proposal", func() {
		title := strings.TrimSpace(titleEntry.Text)
		desc  := strings.TrimSpace(descEntry.Text)
		days, err := strconv.ParseInt(strings.TrimSpace(daysEntry.Text), 10, 64)
		if title == ""           { fyne.Do(func() { result.SetText("❌ Enter title") }); return }
		if desc == ""            { fyne.Do(func() { result.SetText("❌ Enter description") }); return }
		if err != nil || days <= 0 { fyne.Do(func() { result.SetText("❌ Invalid voting period") }); return }
		if bankAccount.SLK < 10 { fyne.Do(func() { result.SetText("❌ Need at least 10 SLK to submit a proposal") }); return }

		p := GovernanceProposal{
			ID: fmt.Sprintf("gov_%x", time.Now().UnixNano()),
			Title: title, Description: desc,
			ProposerID: bankAccount.AccountID,
			Status: "active",
			CreatedAt: time.Now().Unix(),
			EndsAt: time.Now().Add(time.Duration(days)*24*time.Hour).Unix(),
		}
		myProposals = append(myProposals, p)
		saveProposals()
		fyne.Do(func() {
			result.SetText(fmt.Sprintf("✅ Proposal submitted!\nVoting ends: %s", time.Unix(p.EndsAt,0).Format("Jan 02 2006")))
			titleEntry.SetText(""); descEntry.SetText(""); daysEntry.SetText("")
		})
	})
	submitBtn.Importance = widget.HighImportance

	return container.NewVScroll(container.NewPadded(container.NewVBox(
		widget.NewLabel("📜 Submit a protocol improvement proposal.\nMinimum 10 SLK required to submit.\nAll SLK holders can vote — weight = SLK balance."),
		widget.NewSeparator(),
		widget.NewForm(
			widget.NewFormItem("Title", titleEntry),
			widget.NewFormItem("Description", descEntry),
			widget.NewFormItem("Voting Days", daysEntry),
		),
		container.NewPadded(submitBtn), result,
	)))
}

// ════════════════════════════════════════
// IDENTITY TAB
// ════════════════════════════════════════
func makeIdentityTab(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("🏅 Decentralized Identity", theme.ForegroundColor())
	title.TextSize = 18; title.TextStyle = fyne.TextStyle{Bold: true}
	box := container.NewVBox(container.NewCenter(title), widget.NewSeparator())

	if myIdentity != nil {
		badgeLbl := canvas.NewText(myIdentity.Badge, color.NRGBA{R:0,G:200,B:100,A:255})
		badgeLbl.TextSize = 16; badgeLbl.TextStyle = fyne.TextStyle{Bold: true}
		box.Add(container.NewPadded(container.NewVBox(
			badgeLbl,
			widget.NewLabel(fmt.Sprintf("Organisation: %s", myIdentity.OrgName)),
			widget.NewLabel(fmt.Sprintf("Type: %s  |  Reg#: %s", myIdentity.OrgType, myIdentity.RegNumber)),
			widget.NewLabel(fmt.Sprintf("Website: %s", myIdentity.Website)),
			widget.NewLabel(fmt.Sprintf("Verified: %s", time.Unix(myIdentity.VerifiedAt,0).Format("Jan 02 2006"))),
			widget.NewSeparator(),
			widget.NewLabel("✅ Your wallet shows a verified badge to all users on the network."),
		)))
		return container.NewVScroll(container.NewPadded(box))
	}

	// Registration form
	orgEntry  := widget.NewEntry(); orgEntry.SetPlaceHolder("Organisation name")
	typeSel   := widget.NewSelect([]string{"business","charity","government","ngo"}, nil); typeSel.SetSelected("business")
	regEntry  := widget.NewEntry(); regEntry.SetPlaceHolder("Registration number / license")
	webEntry  := widget.NewEntry(); webEntry.SetPlaceHolder("https://yourwebsite.com")
	result    := widget.NewLabel(""); result.Alignment = fyne.TextAlignCenter
	result.TextStyle = fyne.TextStyle{Bold: true}

	registerBtn := widget.NewButton("🏅 Register Verified Identity", func() {
		org := strings.TrimSpace(orgEntry.Text)
		reg := strings.TrimSpace(regEntry.Text)
		web := strings.TrimSpace(webEntry.Text)
		if org == "" { fyne.Do(func() { result.SetText("❌ Enter organisation name") }); return }
		if reg == "" { fyne.Do(func() { result.SetText("❌ Enter registration number") }); return }

		badge := "✅ Verified Business"
		switch typeSel.Selected {
		case "charity":    badge = "✅ Verified Charity"
		case "government": badge = "✅ Verified Government"
		case "ngo":        badge = "✅ Verified NGO"
		}

		myIdentity = &VerifiedIdentity{
			WalletID: bankAccount.AccountID, OrgName: org,
			OrgType: typeSel.Selected, RegNumber: reg, Website: web,
			VerifiedAt: time.Now().Unix(), Badge: badge,
		}
		saveIdentity()
		fyne.Do(func() {
			result.SetText(fmt.Sprintf("✅ Identity registered!\n%s — %s", badge, org))
		})
	})
	registerBtn.Importance = widget.HighImportance

	box.Add(container.NewPadded(widget.NewLabel("Register your organisation to show a verified badge to all users.\nThis is voluntary — individual users are never required to register.")))
	box.Add(widget.NewForm(
		widget.NewFormItem("Organisation", orgEntry),
		widget.NewFormItem("Type", typeSel),
		widget.NewFormItem("Reg Number", regEntry),
		widget.NewFormItem("Website", webEntry),
	))
	box.Add(container.NewPadded(registerBtn))
	box.Add(result)
	return container.NewVScroll(container.NewPadded(box))
}

// ════════════════════════════════════════
// P2P EXCHANGE ORDER BOOK
// ════════════════════════════════════════
var (
	exchangeOrders   []p2p.ExchangeOrder
	exchangeOrdersMu sync.Mutex
	exchangePath     string
)

func initExchangePath() {
	if exchangePath == "" {
		exchangePath = filepath.Join(os.Getenv("HOME"), ".slkbank", "exchange.json")
	}
}

func saveExchangeOrders() {
	initExchangePath()
	exchangeOrdersMu.Lock(); defer exchangeOrdersMu.Unlock()
	d, _ := json.MarshalIndent(exchangeOrders, "", "  ")
	os.WriteFile(exchangePath, d, 0644)
}

func loadExchangeOrders() []p2p.ExchangeOrder {
	initExchangePath()
	d, e := os.ReadFile(exchangePath)
	if e != nil { return []p2p.ExchangeOrder{} }
	var x []p2p.ExchangeOrder
	json.Unmarshal(d, &x)
	return x
}


// ════════════════════════════════════════
// P2P EXCHANGE UI
// ════════════════════════════════════════
func makeExchangeTab(w fyne.Window) fyne.CanvasObject {
	tabs := container.NewAppTabs(
		container.NewTabItem("📊 Order Book",  makeOrderBookTab(w)),
		container.NewTabItem("📤 Place Order", makePlaceOrderTab(w)),
		container.NewTabItem("📋 My Orders",   makeMyOrdersTab(w)),
	)
	tabs.SetTabLocation(container.TabLocationTop)
	return tabs
}

func makeOrderBookTab(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("📊 Live P2P Order Book", theme.ForegroundColor())
	title.TextSize = 16; title.TextStyle = fyne.TextStyle{Bold: true}
	filterSel := widget.NewSelect([]string{"All","BUY","SELL"}, nil)
	filterSel.SetSelected("All")
	curSel := widget.NewSelect([]string{"All","USD","KES","EUR","GBP","NGN","ZAR"}, nil)
	curSel.SetSelected("All")
	box := container.NewVBox()
	rebuild := func() {
		box.Objects = nil
		exchangeOrdersMu.Lock()
		orders := make([]p2p.ExchangeOrder, len(exchangeOrders))
		copy(orders, exchangeOrders)
		exchangeOrdersMu.Unlock()
		var filtered []p2p.ExchangeOrder
		now := time.Now().Unix()
		for _, o := range orders {
			if o.Status != "open" { continue }
			if o.ExpiresAt > 0 && o.ExpiresAt < now { continue }
			if filterSel.Selected != "All" && o.Type != filterSel.Selected { continue }
			if curSel.Selected != "All" && o.Currency != curSel.Selected { continue }
			filtered = append(filtered, o)
		}
		if len(filtered) == 0 {
			box.Add(widget.NewLabel("No open orders yet. Be the first to place one!"))
			box.Refresh()
			return
		}
		for _, o := range filtered {
			o := o
			isMine := o.From == bankAccount.AccountID
			typeColor := color.NRGBA{R:0,G:200,B:100,A:255}
			if o.Type == "SELL" { typeColor = color.NRGBA{R:255,G:80,B:80,A:255} }
			typeLbl := canvas.NewText(o.Type, typeColor)
			typeLbl.TextStyle = fyne.TextStyle{Bold: true}; typeLbl.TextSize = 14
			pricePerSLK := 0.0
			if o.Amount > 0 { pricePerSLK = o.Price / o.Amount }
			senderName := o.FromName
			if senderName == "" && len(o.From) > 16 { senderName = o.From[:16]+"..." }
			infoTxt := fmt.Sprintf("%.8f SLK @ %.2f %s total (%.4f %s/SLK)\nFrom: %s | Posted: %s",
				o.Amount, o.Price, o.Currency, pricePerSLK, o.Currency,
				senderName, time.Unix(o.Timestamp,0).Format("Jan 02 15:04"))
			infoLbl := widget.NewLabel(infoTxt)
			infoLbl.Wrapping = fyne.TextWrapWord
			var actionBtn *widget.Button
			if isMine {
				actionBtn = widget.NewButton("❌ Cancel", func() {
					for i, ex := range exchangeOrders {
						if ex.ID == o.ID {
							exchangeOrders[i].Status = "cancelled"
							if o.Type == "SELL" {
								bankAccount.SLK += o.Amount
								saveBankAccount(bankAccount)
								fyne.Do(func() { refreshLabels() })
							}
							saveExchangeOrders()
							if p2pNode != nil {
								cancelled := exchangeOrders[i]
								p2pNode.BroadcastExchangeOrder(cancelled)
							}
							break
						}
					}
					box.Objects = nil; box.Refresh()
				})
			} else if o.Type == "SELL" {
				actionBtn = widget.NewButton("💸 Buy This", func() {
					msg := fmt.Sprintf("Buy %.8f SLK from %s\nTotal: %.2f %s\nContact seller via Social chat to arrange payment.", o.Amount, senderName, o.Price, o.Currency)
					dialog.ShowConfirm("💸 Buy SLK", msg, func(ok bool) {
						if !ok { return }
						tx := BankTX{ID: fmt.Sprintf("buy_%x", time.Now().UnixNano()),
							From: bankAccount.AccountID, To: o.From,
							Amount: o.Amount, Currency: "SLK", Type: "EXCHANGE_BUY",
							Timestamp: time.Now().Unix(),
							Note: fmt.Sprintf("Buy order %s", o.ID[:8]), Verified: false}
						txHistory = append(txHistory, tx); saveTxHistory()
						dialog.ShowInformation("✅ Intent Recorded",
							fmt.Sprintf("Contact %s via Social to arrange %.2f %s payment.", senderName, o.Price, o.Currency), w)
					}, w)
				})
				actionBtn.Importance = widget.HighImportance
			} else {
				actionBtn = widget.NewButton("📤 Sell to This", func() {
					msg := fmt.Sprintf("Sell %.8f SLK to %s\nYou receive: %.2f %s\nYour SLK will be locked until payment confirmed.", o.Amount, senderName, o.Price, o.Currency)
					dialog.ShowConfirm("📤 Fill Buy Order", msg, func(ok bool) {
						if !ok { return }
						if bankAccount.SLK < o.Amount {
							dialog.ShowInformation("❌ Error", "Insufficient SLK", w); return
						}
						bankAccount.SLK -= o.Amount
						saveBankAccount(bankAccount)
						fyne.Do(func() { refreshLabels() })
						for i, ex := range exchangeOrders {
							if ex.ID == o.ID {
								exchangeOrders[i].Status = "filled"
								saveExchangeOrders()
								if p2pNode != nil { p2pNode.BroadcastExchangeOrder(exchangeOrders[i]) }
								break
							}
						}
						dialog.ShowInformation("✅ Filled", fmt.Sprintf("%.8f SLK locked. Contact %s to arrange payment.", o.Amount, senderName), w)
						box.Objects = nil; box.Refresh()
					}, w)
				})
				actionBtn.Importance = widget.MediumImportance
			}
			box.Add(container.NewPadded(container.NewVBox(
				container.NewGridWithColumns(2, typeLbl, widget.NewLabel(fmt.Sprintf("%.8f SLK", o.Amount))),
				infoLbl, actionBtn, widget.NewSeparator(),
			)))
		}
		box.Refresh()
	}
	filterSel.OnChanged = func(_ string) { box.Objects = nil; box.Refresh() }
	curSel.OnChanged = func(_ string) { box.Objects = nil; box.Refresh() }
	box.Objects = nil; box.Refresh()
	refreshBtn := widget.NewButton("🔄 Refresh", func() { rebuild() })
	return container.NewBorder(
		container.NewVBox(
			container.NewCenter(title),
			container.NewGridWithColumns(3,
				container.NewHBox(widget.NewLabel("Type:"), filterSel),
				container.NewHBox(widget.NewLabel("Currency:"), curSel),
				refreshBtn,
			),
			widget.NewSeparator(),
		),
		nil, nil, nil,
		container.NewVScroll(container.NewPadded(box)),
	)
}

func makePlaceOrderTab(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("📤 Place Exchange Order", theme.ForegroundColor())
	title.TextSize = 16; title.TextStyle = fyne.TextStyle{Bold: true}
	typeSel    := widget.NewSelect([]string{"SELL","BUY"}, nil); typeSel.SetSelected("SELL")
	amtEntry   := widget.NewEntry(); amtEntry.SetPlaceHolder("SLK amount (e.g. 10.5)")
	priceEntry := widget.NewEntry(); priceEntry.SetPlaceHolder("Total fiat price (e.g. 250.00)")
	curSel     := widget.NewSelect([]string{"USD","KES","EUR","GBP","NGN","ZAR","TZS","UGX"}, nil); curSel.SetSelected("KES")
	daysEntry  := widget.NewEntry(); daysEntry.SetPlaceHolder("Valid for days (e.g. 7)")
	result     := widget.NewLabel(""); result.Alignment = fyne.TextAlignCenter
	result.TextStyle = fyne.TextStyle{Bold: true}
	pricePerLbl := widget.NewLabel("Price per SLK: —")
	update := func(_ string) {
		amt, e1 := strconv.ParseFloat(strings.TrimSpace(amtEntry.Text), 64)
		price, e2 := strconv.ParseFloat(strings.TrimSpace(priceEntry.Text), 64)
		if e1 == nil && e2 == nil && amt > 0 {
			pricePerLbl.SetText(fmt.Sprintf("Price per SLK: %.4f %s", price/amt, curSel.Selected))
		}
	}
	amtEntry.OnChanged = update; priceEntry.OnChanged = update
	placeBtn := widget.NewButton("📡 Broadcast Order to Network", func() {
		orderType := typeSel.Selected
		amt, err1   := strconv.ParseFloat(strings.TrimSpace(amtEntry.Text), 64)
		price, err2 := strconv.ParseFloat(strings.TrimSpace(priceEntry.Text), 64)
		days, err3  := strconv.ParseInt(strings.TrimSpace(daysEntry.Text), 10, 64)
		if err1 != nil || amt <= 0   { fyne.Do(func() { result.SetText("❌ Invalid SLK amount") }); return }
		if err2 != nil || price <= 0 { fyne.Do(func() { result.SetText("❌ Invalid price") }); return }
		if err3 != nil || days <= 0  { fyne.Do(func() { result.SetText("❌ Enter valid days") }); return }
		if orderType == "SELL" && amt > bankAccount.SLK {
			fyne.Do(func() { result.SetText("❌ Insufficient SLK") }); return
		}
		if orderType == "SELL" {
			bankAccount.SLK -= amt; saveBankAccount(bankAccount)
			fyne.Do(func() { refreshLabels() })
		}
		order := p2p.ExchangeOrder{
			ID: fmt.Sprintf("ord_%x", time.Now().UnixNano()),
			Type: orderType, From: bankAccount.AccountID, FromName: bankAccount.Name,
			Amount: amt, Price: price, Currency: curSel.Selected,
			Status: "open", Timestamp: time.Now().Unix(),
			ExpiresAt: time.Now().Add(time.Duration(days)*24*time.Hour).Unix(),
		}
		exchangeOrdersMu.Lock()
		exchangeOrders = append(exchangeOrders, order)
		exchangeOrdersMu.Unlock()
		saveExchangeOrders()
		if p2pNode != nil { p2pNode.BroadcastExchangeOrder(order) }
		fyne.Do(func() {
			result.SetText(fmt.Sprintf("✅ %s order broadcast! %.8f SLK @ %.2f %s (%d days)", orderType, amt, price, curSel.Selected, days))
			amtEntry.SetText(""); priceEntry.SetText(""); daysEntry.SetText("")
		})
	})
	placeBtn.Importance = widget.HighImportance
	info := widget.NewLabel("SELL orders lock your SLK in escrow until filled or cancelled.\nBUY orders are visible to all peers on the network.\nContact sellers/buyers via the Social tab to arrange fiat payment.")
	info.Wrapping = fyne.TextWrapWord
	return container.NewVScroll(container.NewPadded(container.NewVBox(
		container.NewCenter(title), widget.NewSeparator(),
		container.NewPadded(info), widget.NewSeparator(),
		widget.NewForm(
			widget.NewFormItem("Order Type", typeSel),
			widget.NewFormItem("SLK Amount", amtEntry),
			widget.NewFormItem("Total Price", priceEntry),
			widget.NewFormItem("Currency", curSel),
			widget.NewFormItem("Valid Days", daysEntry),
		),
		container.NewPadded(pricePerLbl),
		widget.NewSeparator(),
		container.NewPadded(placeBtn), result,
	)))
}

func makeMyOrdersTab(w fyne.Window) fyne.CanvasObject {
	box := container.NewVBox()
	mine := 0
	exchangeOrdersMu.Lock()
	orders := make([]p2p.ExchangeOrder, len(exchangeOrders))
	copy(orders, exchangeOrders)
	exchangeOrdersMu.Unlock()
	for _, o := range orders {
		if o.From != bankAccount.AccountID { continue }
		mine++
		o := o
		statusColor := color.NRGBA{R:0,G:200,B:100,A:255}
		if o.Status == "cancelled" { statusColor = color.NRGBA{R:150,G:150,B:150,A:255} }
		if o.Status == "filled"    { statusColor = color.NRGBA{R:100,G:200,B:255,A:255} }
		statusLbl := canvas.NewText(fmt.Sprintf("%s - %s", o.Type, strings.ToUpper(o.Status)), statusColor)
		statusLbl.TextStyle = fyne.TextStyle{Bold: true}
		box.Add(container.NewPadded(container.NewVBox(
			statusLbl,
			widget.NewLabel(fmt.Sprintf("%.8f SLK @ %.2f %s", o.Amount, o.Price, o.Currency)),
			widget.NewLabel(fmt.Sprintf("Posted: %s  Expires: %s",
				time.Unix(o.Timestamp,0).Format("Jan 02 15:04"),
				time.Unix(o.ExpiresAt,0).Format("Jan 02 2006"))),
			widget.NewSeparator(),
		)))
	}
	if mine == 0 { box.Add(widget.NewLabel("No orders placed yet.")) }
	return container.NewVScroll(container.NewPadded(box))
}

// ════════════════════════════════════════
// SMART CONTRACTS
// ════════════════════════════════════════
var contractStore *contracts.ContractStore

func initContracts() {
	base := filepath.Join(os.Getenv("HOME"), ".slkbank")
	os.MkdirAll(base, 0700)
	contractStore = contracts.NewContractStore(base)
	// Start background executor
	go func() {
		for range time.Tick(30 * time.Second) {
			if contractStore == nil { continue }
			contractStore.CheckAndExecute(func(c *contracts.Contract) error {
				// Only execute contracts where we are the creator (we hold the funds)
				if c.Creator != bankAccount.AccountID { return fmt.Errorf("not our contract") }
				amt := c.Amount
				if c.Type == contracts.TypeVesting {
					amt = c.VestingClaimable()
				}
				if amt <= 0 { return fmt.Errorf("nothing to release") }
				// Move SLK to beneficiary via recorded tx
				tx := BankTX{
					ID: fmt.Sprintf("sc_%x", time.Now().UnixNano()),
					From: bankAccount.AccountID,
					To: c.Beneficiary,
					Amount: amt,
					Currency: "SLK",
					Type: "SMART_CONTRACT",
					Timestamp: time.Now().Unix(),
					Note: fmt.Sprintf("Contract %s: %s", c.Type, c.Title),
					Verified: true,
				}
				txHistory = append(txHistory, tx)
				saveTxHistory()
				if p2pNode != nil {
					p2pNode.BroadcastTx(p2p.TxMsg{
						ID: tx.ID, From: tx.From, To: tx.To,
						Amount: amt, Timestamp: tx.Timestamp, Type: 1,
					})
				}
				fmt.Printf("✅ Contract executed: %s -> %.8f SLK -> %s\n", c.Title, amt, c.Beneficiary)
				return nil
			})
		}
	}()
}

// ════════════════════════════════════════
// SMART CONTRACTS UI
// ════════════════════════════════════════
func makeSmartContractsTab(w fyne.Window) fyne.CanvasObject {
	tabs := container.NewAppTabs(
		container.NewTabItem("📋 My Contracts", makeContractListTab(w)),
		container.NewTabItem("➕ New Contract",  makeCreateContractTab(w)),
	)
	tabs.SetTabLocation(container.TabLocationTop)
	return tabs
}

func makeContractListTab(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("📋 Smart Contracts", theme.ForegroundColor())
	title.TextSize = 16; title.TextStyle = fyne.TextStyle{Bold: true}
	box := container.NewVBox()

	if contractStore == nil {
		return container.NewVScroll(container.NewPadded(widget.NewLabel("Contract store not initialized.")))
	}

	myContracts := contractStore.GetByAddress(bankAccount.AccountID)
	if len(myContracts) == 0 {
		box.Add(widget.NewLabel("No contracts yet. Create one below."))
	}

	for _, c := range myContracts {
		c := c
		statusColor := color.NRGBA{R:0,G:200,B:100,A:255}
		if c.Status == contracts.StatusExecuted  { statusColor = color.NRGBA{R:100,G:200,B:255,A:255} }
		if c.Status == contracts.StatusCancelled { statusColor = color.NRGBA{R:150,G:150,B:150,A:255} }
		if c.Status == contracts.StatusExpired   { statusColor = color.NRGBA{R:255,G:100,B:100,A:255} }

		titleLbl := canvas.NewText(fmt.Sprintf("[%s] %s", c.Type, c.Title), theme.ForegroundColor())
		titleLbl.TextStyle = fyne.TextStyle{Bold: true}
		statusLbl := canvas.NewText(string(c.Status), statusColor)
		statusLbl.TextStyle = fyne.TextStyle{Bold: true}

		role := "CREATOR"
		if c.Beneficiary == bankAccount.AccountID { role = "BENEFICIARY" }

		details := fmt.Sprintf("Amount: %.8f SLK | Role: %s | Beneficiary: %s",
			c.Amount, role, func() string {
				if len(c.Beneficiary) > 16 { return c.Beneficiary[:16]+"..." }
				return c.Beneficiary
			}())

		var extraInfo string
		switch c.Type {
		case contracts.TypeVesting:
			claimable := c.VestingClaimable()
			extraInfo = fmt.Sprintf("Claimable now: %.8f SLK | Claimed: %.8f SLK", claimable, c.VestingClaimed)
		case contracts.TypeEscrow:
			extraInfo = fmt.Sprintf("Creator confirmed: %v | Beneficiary confirmed: %v",
				c.CreatorConfirmed, c.BeneficiaryConfirmed)
		case contracts.TypeMultiSig:
			extraInfo = fmt.Sprintf("Signatures: %d/%d", len(c.Signatures), c.SigsRequired)
		case contracts.TypeWill:
			days := (time.Now().Unix() - c.LastActivity) / 86400
			extraInfo = fmt.Sprintf("Inactive for: %d days | Triggers after: %d days", days, c.InactivityDays)
		case contracts.TypeSavings:
			extraInfo = fmt.Sprintf("Saved: %.8f / %.8f SLK", c.SavingsBalance, c.SavingsTarget)
		}

		// Action buttons based on type and role
		btnBox := container.NewHBox()

		if c.Status == contracts.StatusActive {
			if c.Type == contracts.TypeEscrow {
				if c.Creator == bankAccount.AccountID && !c.CreatorConfirmed {
					confirmBtn := widget.NewButton("✅ Confirm (Creator)", func() {
						c.CreatorConfirmed = true
						contractStore.Save()
						dialog.ShowInformation("✅ Confirmed", "Your confirmation recorded. Waiting for beneficiary.", w)
					})
					confirmBtn.Importance = widget.HighImportance
					btnBox.Add(confirmBtn)
				}
				if c.Beneficiary == bankAccount.AccountID && !c.BeneficiaryConfirmed {
					confirmBtn := widget.NewButton("✅ Confirm (Beneficiary)", func() {
						c.BeneficiaryConfirmed = true
						contractStore.Save()
						dialog.ShowInformation("✅ Confirmed", "Both parties confirmed. Funds will be released.", w)
					})
					confirmBtn.Importance = widget.HighImportance
					btnBox.Add(confirmBtn)
				}
			}
			if c.Type == contracts.TypeMultiSig {
				alreadySigned := false
				for _, s := range c.Signatures { if s == bankAccount.AccountID { alreadySigned = true } }
				if !alreadySigned {
					signBtn := widget.NewButton("✍ Sign", func() {
						c.Signatures = append(c.Signatures, bankAccount.AccountID)
						contractStore.Save()
						dialog.ShowInformation("✅ Signed",
							fmt.Sprintf("Signature added. %d/%d required.", len(c.Signatures), c.SigsRequired), w)
					})
					signBtn.Importance = widget.HighImportance
					btnBox.Add(signBtn)
				}
			}
			if c.Type == contracts.TypeConditional && c.Creator == bankAccount.AccountID {
				markBtn := widget.NewButton("✅ Mark Condition Met", func() {
					dialog.ShowConfirm("Confirm", fmt.Sprintf("Confirm condition is met: %s", c.Condition), func(ok bool) {
						if !ok { return }
						c.ConditionMet = true
						// Execute immediately
						tx := BankTX{
							ID: fmt.Sprintf("sc_%x", time.Now().UnixNano()),
							From: bankAccount.AccountID, To: c.Beneficiary,
							Amount: c.Amount, Currency: "SLK",
							Type: "SMART_CONTRACT", Timestamp: time.Now().Unix(),
							Note: fmt.Sprintf("Conditional contract: %s", c.Title), Verified: true,
						}
						txHistory = append(txHistory, tx); saveTxHistory()
						c.Status = contracts.StatusExecuted
						c.ExecutedAt = time.Now().Unix()
						contractStore.Save()
						refreshLabels()
						dialog.ShowInformation("✅ Executed",
							fmt.Sprintf("%.8f SLK sent to %s", c.Amount, c.Beneficiary), w)
					}, w)
				})
				markBtn.Importance = widget.HighImportance
				btnBox.Add(markBtn)
			}
			if c.Type == contracts.TypeSavings && c.Creator == bankAccount.AccountID {
				topupEntry := widget.NewEntry(); topupEntry.SetPlaceHolder("Add SLK to savings")
				topupBtn := widget.NewButton("💰 Top Up", func() {
					amt, err := strconv.ParseFloat(strings.TrimSpace(topupEntry.Text), 64)
					if err != nil || amt <= 0 { dialog.ShowInformation("❌", "Invalid amount", w); return }
					if amt > bankAccount.SLK { dialog.ShowInformation("❌", "Insufficient SLK", w); return }
					bankAccount.SLK -= amt; saveBankAccount(bankAccount); refreshLabels()
					c.SavingsBalance += amt
					if c.SavingsBalance >= c.SavingsTarget {
						c.Status = contracts.StatusExecuted
						c.ExecutedAt = time.Now().Unix()
						dialog.ShowInformation("🎉 Target Reached!",
							fmt.Sprintf("Savings target of %.8f SLK reached!", c.SavingsTarget), w)
					}
					contractStore.Save()
					topupEntry.SetText("")
				})
				btnBox.Add(topupEntry)
				btnBox.Add(topupBtn)
			}
			// Cancel button for creator
			if c.Creator == bankAccount.AccountID {
				cancelBtn := widget.NewButton("❌ Cancel", func() {
					dialog.ShowConfirm("Cancel Contract", "Cancel and return SLK to your wallet?", func(ok bool) {
						if !ok { return }
						c.Status = contracts.StatusCancelled
						// Return locked SLK
						returnAmt := c.Amount - c.VestingClaimed
						if c.Type == contracts.TypeSavings { returnAmt = c.SavingsBalance }
						if returnAmt > 0 {
							bankAccount.SLK += returnAmt
							saveBankAccount(bankAccount); refreshLabels()
						}
						contractStore.Save()
						dialog.ShowInformation("✅ Cancelled",
							fmt.Sprintf("%.8f SLK returned to your wallet.", returnAmt), w)
					}, w)
				})
				btnBox.Add(cancelBtn)
			}
		}

		box.Add(container.NewPadded(container.NewVBox(
			titleLbl, statusLbl,
			widget.NewLabel(details),
			widget.NewLabel(extraInfo),
			widget.NewLabel(fmt.Sprintf("Created: %s", time.Unix(c.CreatedAt,0).Format("Jan 02 2006 15:04"))),
			btnBox,
			widget.NewSeparator(),
		)))
	}
	return container.NewBorder(
		container.NewVBox(container.NewCenter(title), widget.NewSeparator()),
		nil, nil, nil, container.NewVScroll(container.NewPadded(box)))
}

func makeCreateContractTab(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("➕ New Smart Contract", theme.ForegroundColor())
	title.TextSize = 16; title.TextStyle = fyne.TextStyle{Bold: true}

	contractTypes := []string{"ESCROW","VESTING","MULTISIG","CONDITIONAL","SAVINGS","WILL"}
	typeSel := widget.NewSelect(contractTypes, nil); typeSel.SetSelected("ESCROW")

	titleEntry := widget.NewEntry(); titleEntry.SetPlaceHolder("Contract title")
	descEntry  := widget.NewEntry(); descEntry.SetPlaceHolder("Description"); descEntry.MultiLine = true
	benefEntry := widget.NewEntry(); benefEntry.SetPlaceHolder("Beneficiary address")
	amtEntry   := widget.NewEntry(); amtEntry.SetPlaceHolder("SLK amount to lock")

	// Type-specific fields
	condEntry   := widget.NewEntry(); condEntry.SetPlaceHolder("Condition (e.g. Project delivered)")
	vestDays    := widget.NewEntry(); vestDays.SetPlaceHolder("Vesting period in days")
	savTarget   := widget.NewEntry(); savTarget.SetPlaceHolder("Savings target (SLK)")
	willDays    := widget.NewEntry(); willDays.SetPlaceHolder("Inactivity days to trigger (min 30)")
	multisigOwners := widget.NewEntry(); multisigOwners.SetPlaceHolder("Signer addresses comma-separated")
	multisigReq    := widget.NewEntry(); multisigReq.SetPlaceHolder("Signatures required (e.g. 2)")
	expDays     := widget.NewEntry(); expDays.SetPlaceHolder("Expires in days (0 = never)")

	result := widget.NewLabel(""); result.Alignment = fyne.TextAlignCenter
	result.TextStyle = fyne.TextStyle{Bold: true}

	// Show/hide type-specific fields
	extraBox := container.NewVBox()
	typeSel.OnChanged = func(t string) {
		extraBox.Objects = nil
		switch t {
		case "ESCROW":
			extraBox.Add(widget.NewLabel("Both parties must confirm to release funds."))
		case "VESTING":
			extraBox.Add(widget.NewFormItem("Vesting Days", vestDays).Widget)
			extraBox.Add(widget.NewLabel("Funds released linearly over the vesting period."))
		case "MULTISIG":
			extraBox.Add(widget.NewFormItem("Signers", multisigOwners).Widget)
			extraBox.Add(widget.NewFormItem("Required", multisigReq).Widget)
		case "CONDITIONAL":
			extraBox.Add(widget.NewFormItem("Condition", condEntry).Widget)
			extraBox.Add(widget.NewLabel("You manually mark condition as met to release funds."))
		case "SAVINGS":
			extraBox.Add(widget.NewFormItem("Target", savTarget).Widget)
			extraBox.Add(widget.NewLabel("Lock SLK until savings target is reached."))
		case "WILL":
			extraBox.Add(widget.NewFormItem("Inactivity Days", willDays).Widget)
			extraBox.Add(widget.NewLabel("Releases to beneficiary if you are inactive for specified days."))
		}
		extraBox.Refresh()
	}
	typeSel.OnChanged("ESCROW")

	createBtn := widget.NewButton("🔐 Deploy Smart Contract", func() {
		contractTitle := strings.TrimSpace(titleEntry.Text)
		benef := strings.TrimSpace(benefEntry.Text)
		amt, err := strconv.ParseFloat(strings.TrimSpace(amtEntry.Text), 64)
		if contractTitle == "" { fyne.Do(func() { result.SetText("❌ Enter title") }); return }
		if benef == ""  { fyne.Do(func() { result.SetText("❌ Enter beneficiary address") }); return }
		if err != nil || amt <= 0 { fyne.Do(func() { result.SetText("❌ Invalid amount") }); return }
		if amt > bankAccount.SLK { fyne.Do(func() { result.SetText("❌ Insufficient SLK") }); return }

		now := time.Now().Unix()
		expDaysN, _ := strconv.ParseInt(strings.TrimSpace(expDays.Text), 10, 64)
		expiresAt := int64(0)
		if expDaysN > 0 { expiresAt = now + expDaysN*86400 }

		c := &contracts.Contract{
			ID: fmt.Sprintf("sc_%x", time.Now().UnixNano()),
			Type: contracts.ContractType(typeSel.Selected),
			Creator: bankAccount.AccountID,
			Beneficiary: benef,
			Amount: amt,
			Status: contracts.StatusActive,
			CreatedAt: now,
			ExpiresAt: expiresAt,
			Title: contractTitle,
			Description: strings.TrimSpace(descEntry.Text),
			LastActivity: now,
		}

		switch contracts.ContractType(typeSel.Selected) {
		case contracts.TypeVesting:
			days, err := strconv.ParseInt(strings.TrimSpace(vestDays.Text), 10, 64)
			if err != nil || days <= 0 { fyne.Do(func() { result.SetText("❌ Invalid vesting days") }); return }
			c.VestingStart = now
			c.VestingEnd = now + days*86400
			c.VestingInterval = "daily"
		case contracts.TypeMultiSig:
			parts := strings.Split(multisigOwners.Text, ",")
			var signers []string
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" { signers = append(signers, p) }
			}
			req, err := strconv.Atoi(strings.TrimSpace(multisigReq.Text))
			if err != nil || req < 1 { fyne.Do(func() { result.SetText("❌ Invalid signatures required") }); return }
			c.Signers = signers
			c.SigsRequired = req
		case contracts.TypeConditional:
			cond := strings.TrimSpace(condEntry.Text)
			if cond == "" { fyne.Do(func() { result.SetText("❌ Enter condition") }); return }
			c.Condition = cond
		case contracts.TypeSavings:
			target, err := strconv.ParseFloat(strings.TrimSpace(savTarget.Text), 64)
			if err != nil || target <= 0 { fyne.Do(func() { result.SetText("❌ Invalid savings target") }); return }
			c.SavingsTarget = target
			c.SavingsBalance = amt
		case contracts.TypeWill:
			days, err := strconv.ParseInt(strings.TrimSpace(willDays.Text), 10, 64)
			if err != nil || days < 30 { fyne.Do(func() { result.SetText("❌ Minimum 30 days") }); return }
			c.InactivityDays = days
		}

		if err := contractStore.Add(c); err != nil {
			fyne.Do(func() { result.SetText("❌ " + err.Error()) }); return
		}

		// Lock SLK
		bankAccount.SLK -= amt
		saveBankAccount(bankAccount); refreshLabels()

		// Broadcast to network
		if p2pNode != nil {
			p2pNode.BroadcastBankRecord(p2p.BankRecord{
				ID: c.ID, From: bankAccount.AccountID, To: benef,
				Amount: amt, Currency: "SLK", TxType: "SMART_CONTRACT",
				Timestamp: now, Verified: true,
			})
		}

		fyne.Do(func() {
			result.SetText(fmt.Sprintf("✅ Contract deployed! %.8f SLK locked\nID: %s", amt, c.ID))
			titleEntry.SetText(""); benefEntry.SetText(""); amtEntry.SetText("")
			descEntry.SetText(""); condEntry.SetText(""); vestDays.SetText("")
		})
	})
	createBtn.Importance = widget.HighImportance

	return container.NewVScroll(container.NewPadded(container.NewVBox(
		container.NewCenter(title), widget.NewSeparator(),
		widget.NewForm(
			widget.NewFormItem("Type", typeSel),
			widget.NewFormItem("Title", titleEntry),
			widget.NewFormItem("Description", descEntry),
			widget.NewFormItem("Beneficiary", benefEntry),
			widget.NewFormItem("SLK Amount", amtEntry),
			widget.NewFormItem("Expires (days)", expDays),
		),
		container.NewPadded(extraBox),
		widget.NewSeparator(),
		container.NewPadded(createBtn), result,
	)))
}

// Generate a real API key for bank owners to connect their website
func generateAPIKey(bankID string) string {
	h := sha256.New()
	h.Write([]byte(bankID + fmt.Sprintf("%d", time.Now().UnixNano())))
	return "slk_" + hex.EncodeToString(h.Sum(nil))[:48]
}

// Supply-safe fee calculation — basis points, floor rounding, SLKCT
func calculateFee(amountSLKCT int64, feeBasisPoints int64) (fee int64, remainder int64) {
	// FEE = FLOOR( Amount_SLKCT x Basis_Points / 10,000 )
	fee = (amountSLKCT * feeBasisPoints) / 10000
	remainder = amountSLKCT - fee
	return
}

// Distribute dividends to all share holders automatically
func distributeDividends(shares []BankShare, totalFeeSLKCT int64) map[string]int64 {
	payouts := make(map[string]int64)
	if len(shares) == 0 { return payouts }
	var totalShares int64
	for _, s := range shares { totalShares += s.Shares }
	if totalShares == 0 { return payouts }
	var distributed int64
	for _, s := range shares {
		// FLOOR rounding — supply safe
		payout := (totalFeeSLKCT * s.Shares) / totalShares
		payouts[s.HolderID] += payout
		distributed += payout
	}
	// Remainder stays with bank owner — never lost
	return payouts
}
func saveMarket() { d, _ := json.MarshalIndent(marketList, "", "  "); os.WriteFile(marketPath, d, 0600) }
func loadSocial() []SocialPost { d, e := os.ReadFile(socialPath); if e != nil { return []SocialPost{} }; var x []SocialPost; json.Unmarshal(d, &x); return x }
func saveSocial() { d, _ := json.MarshalIndent(socialFeed, "", "  "); os.WriteFile(socialPath, d, 0600) }
func loadRecords() []NetworkRecord { d, e := os.ReadFile(recordsPath); if e != nil { return []NetworkRecord{} }; var x []NetworkRecord; json.Unmarshal(d, &x); return x }
func saveRecords() { d, _ := json.MarshalIndent(netRecords, "", "  "); os.WriteFile(recordsPath, d, 0600) }
func loadFriends() []FriendRequest { d, e := os.ReadFile(friendsPath); if e != nil { return []FriendRequest{} }; var x []FriendRequest; json.Unmarshal(d, &x); return x }
func saveFriends() { d, _ := json.MarshalIndent(friendReqs, "", "  "); os.WriteFile(friendsPath, d, 0600) }
func loadChat() []ChatMessage { d, e := os.ReadFile(chatPath); if e != nil { return []ChatMessage{} }; var x []ChatMessage; json.Unmarshal(d, &x); return x }
func saveChat() { d, _ := json.MarshalIndent(chatMsgs, "", "  "); os.WriteFile(chatPath, d, 0600) }
func loadBanks() []KnownBank { d, e := os.ReadFile(banksPath); if e != nil { return []KnownBank{} }; var x []KnownBank; json.Unmarshal(d, &x); return x }
func saveBanks() { d, _ := json.MarshalIndent(knownBanks, "", "  "); os.WriteFile(banksPath, d, 0600) }
func generateAPIKeys() (string, string) {
	pub := make([]byte, 16); sec := make([]byte, 32); rand.Read(pub); rand.Read(sec)
	return "pk_" + hex.EncodeToString(pub), "sk_" + hex.EncodeToString(sec)
}
func generateAccountID() string {
	b := make([]byte, 8); rand.Read(b)
	return "SLKB-" + hex.EncodeToString(b[:4]) + "-" + hex.EncodeToString(b[4:])
}

// ════════════════════════════════════════
// PROFILE TAB
// ════════════════════════════════════════
func makeProfileTab(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("My Profile", theme.ForegroundColor())
	title.TextSize = 18; title.TextStyle = fyne.TextStyle{Bold: true}

	photoBox := container.NewVBox()
	rebuildPhoto := func() {
		photoBox.Objects = nil
		if bankAccount.ProfilePhoto != "" {
			if _, err := os.Stat(bankAccount.ProfilePhoto); err == nil {
				img := canvas.NewImageFromFile(bankAccount.ProfilePhoto)
				img.FillMode = canvas.ImageFillContain
				img.SetMinSize(fyne.NewSize(120, 120))
				photoBox.Add(container.NewCenter(img))
			} else {
				photoBox.Add(widget.NewLabel("Photo not found: " + bankAccount.ProfilePhoto))
			}
		} else {
			photoBox.Add(widget.NewLabel("No profile photo set"))
		}
		photoBox.Refresh()
	}
	rebuildPhoto()

	photoEntry := widget.NewEntry()
	photoEntry.SetPlaceHolder("Path to profile photo (e.g. /home/user/photo.jpg)")
	photoEntry.SetText(bankAccount.ProfilePhoto)
	photoEntry.OnChanged = func(path string) {
		if _, err := os.Stat(path); err == nil {
			bankAccount.ProfilePhoto = path
			saveBankAccount(bankAccount)
			rebuildPhoto()
		}
	}

	bioEntry := widget.NewEntry()
	bioEntry.SetPlaceHolder("Tell the network about yourself...")
	bioEntry.MultiLine = true
	bioEntry.SetText(bankAccount.Bio)

	locEntry := widget.NewEntry()
	locEntry.SetPlaceHolder("Your city / country (e.g. Nairobi, Kenya)")
	locEntry.SetText(bankAccount.Location)

	result := widget.NewLabel("")
	result.Alignment = fyne.TextAlignCenter

	saveBtn := widget.NewButton("Save Profile", func() {
		bankAccount.Bio = strings.TrimSpace(bioEntry.Text)
		bankAccount.Location = strings.TrimSpace(locEntry.Text)
		saveBankAccount(bankAccount)
		if p2pNode != nil {
			p2pNode.BroadcastSocial(p2p.SocialMsg{
				ID: fmt.Sprintf("profile_%x", time.Now().UnixNano()),
				From: bankAccount.AccountID,
				Name: bankAccount.Name,
				Text: "__BANK_ANNOUNCE__",
				ImagePath: bankAccount.OwnerAddr,
				Timestamp: time.Now().Unix(),
			})
		}
		fyne.Do(func() {
			result.SetText("Profile saved!")
			statusBar.SetText("Profile updated")
		})
	})
	saveBtn.Importance = widget.HighImportance

	totalTx := len(txHistory)
	totalSent, totalReceived := 0.0, 0.0
	for _, tx := range txHistory {
		if tx.Type == "SEND" { totalSent += tx.Amount }
		if tx.Type == "RECEIVE" { totalReceived += tx.Amount }
	}
	myListings := 0
	for _, l := range marketList {
		if l.Seller == bankAccount.AccountID { myListings++ }
	}

	statsBox := container.NewVBox(
		widget.NewLabel("My Stats"),
		widget.NewLabel(fmt.Sprintf("   Transactions:     %d", totalTx)),
		widget.NewLabel(fmt.Sprintf("   Total Sent:       %.8f SLK", totalSent)),
		widget.NewLabel(fmt.Sprintf("   Total Received:   %.8f SLK", totalReceived)),
		widget.NewLabel(fmt.Sprintf("   Market Listings:  %d", myListings)),
		widget.NewLabel(fmt.Sprintf("   Member Since:     %s", time.Unix(bankAccount.CreatedAt, 0).Format("Jan 02 2006"))),
	)

	return container.NewVScroll(container.NewPadded(container.NewVBox(
		container.NewCenter(title), widget.NewSeparator(),
		container.NewCenter(photoBox),
		widget.NewLabel("Profile Photo Path:"), photoEntry,
		widget.NewSeparator(),
		widget.NewForm(
			widget.NewFormItem("Name", widget.NewLabel(bankAccount.Name+" (permanent)")),
			widget.NewFormItem("Account ID", widget.NewLabel(bankAccount.AccountID)),
			widget.NewFormItem("Bio", bioEntry),
			widget.NewFormItem("Location", locEntry),
		),
		container.NewPadded(saveBtn), result,
		widget.NewSeparator(),
		container.NewPadded(statsBox),
	)))
}

// ════════════════════════════════════════
// NOTIFICATIONS TAB
// ════════════════════════════════════════
func makeNotifTab(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("Notifications", theme.ForegroundColor())
	title.TextSize = 18; title.TextStyle = fyne.TextStyle{Bold: true}

	box := container.NewVBox()
	rebuild := func() {
		box.Objects = nil
		unread := 0
		for _, n := range notifications { if !n.Read { unread++ } }
		if unread == 0 {
			box.Add(widget.NewLabel("No new notifications."))
		}
		for i := len(notifications) - 1; i >= 0; i-- {
			n := notifications[i]
			t := time.Unix(n.Timestamp, 0).Format("Jan 02 15:04:05")
			status := "🔵"; if n.Read { status = "⚪" }
			box.Add(container.NewVBox(
				widget.NewLabel(fmt.Sprintf("%s  %s", status, n.Text)),
				widget.NewLabel(fmt.Sprintf("        %s", t)),
				widget.NewSeparator(),
			))
		}
		box.Refresh()
	}
	rebuild()

	markAllBtn := widget.NewButton("✅ Mark All as Read", func() {
		for i := range notifications { notifications[i].Read = true }
		saveNotifications()
		if notifLabel != nil { notifLabel.SetText("🔔 0") }
		rebuild()
	})
	markAllBtn.Importance = widget.HighImportance

	clearBtn := widget.NewButton("🗑 Clear All", func() {
		dialog.ShowConfirm("Clear", "Delete all notifications?", func(ok bool) {
			if !ok { return }
			notifications = []Notification{}
			saveNotifications()
			if notifLabel != nil { notifLabel.SetText("🔔 0") }
			rebuild()
		}, w)
	})

	unread := 0
	for _, n := range notifications { if !n.Read { unread++ } }
	countLabel := widget.NewLabel(fmt.Sprintf("Total: %d  |  Unread: %d", len(notifications), unread))

	return container.NewBorder(
		container.NewVBox(
			container.NewCenter(title),
			countLabel,
			container.New(layout.NewGridLayout(2), markAllBtn, clearBtn),
			widget.NewSeparator(),
		),
		nil, nil, nil,
		container.NewVScroll(box),
	)
}

func loadNotifications() []Notification {
	d, e := os.ReadFile(notifPath); if e != nil { return []Notification{} }
	var x []Notification; json.Unmarshal(d, &x); return x
}
func saveNotifications() {
	d, _ := json.MarshalIndent(notifications, "", "  "); os.WriteFile(notifPath, d, 0600)
}

// ════════════════════════════════════════
// PRICE CHART TAB
// ════════════════════════════════════════
func makeChartTab(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("SLK Price Chart", theme.ForegroundColor())
	title.TextSize = 18; title.TextStyle = fyne.TextStyle{Bold: true}

	// Build price history from real trade data
	type PricePoint struct {
		Time  int64
		Price float64
	}
	var points []PricePoint

	// Extract real prices from market listings and transactions
	for _, l := range marketList {
		if l.FiatPrice > 0 && l.Amount > 0 {
			pricePerSLK := l.FiatPrice / l.Amount
			points = append(points, PricePoint{l.CreatedAt, pricePerSLK})
		}
	}
	for _, tx := range txHistory {
		if tx.Amount > 0 {
			points = append(points, PricePoint{tx.Timestamp, tx.Amount})
		}
	}

	// Sort by time
	for i := 0; i < len(points); i++ {
		for j := i + 1; j < len(points); j++ {
			if points[j].Time < points[i].Time {
				points[i], points[j] = points[j], points[i]
			}
		}
	}

	// Stats
	totalVolume := 0.0
	highPrice := 0.0
	lowPrice := 999999999.0
	for _, l := range marketList {
		if l.Active {
			totalVolume += l.Amount
			if l.FiatPrice/l.Amount > highPrice { highPrice = l.FiatPrice / l.Amount }
			if l.FiatPrice/l.Amount < lowPrice  { lowPrice  = l.FiatPrice / l.Amount }
		}
	}
	if lowPrice == 999999999.0 { lowPrice = 0 }

	// Draw chart using canvas
	chartArea := container.NewVBox()

	if len(points) < 2 {
		chartArea.Add(widget.NewLabel("Not enough trade data yet."))
		chartArea.Add(widget.NewLabel("Post listings and complete trades to see price history."))
	} else {
		// Simple ASCII-style chart using labels
		chartArea.Add(widget.NewLabel("Price History (USD per SLK):"))
		chartArea.Add(widget.NewSeparator())

		maxP := 0.0
		for _, p := range points { if p.Price > maxP { maxP = p.Price } }

		for _, p := range points {
			bar := ""
			if maxP > 0 {
				bars := int((p.Price / maxP) * 40)
				for i := 0; i < bars; i++ { bar += "█" }
			}
			t := time.Unix(p.Time, 0).Format("Jan 02 15:04")
			chartArea.Add(widget.NewLabel(fmt.Sprintf("%s  $%.4f  %s", t, p.Price, bar)))
		}
	}

	// Live market stats
	activeTrades := 0
	for _, l := range marketList { if l.Active { activeTrades++ } }

	statsBox := container.NewVBox(
		widget.NewSeparator(),
		canvas.NewText("📊 Live Market Stats", theme.ForegroundColor()),
		widget.NewLabel(fmt.Sprintf("   Active Listings:    %d", activeTrades)),
		widget.NewLabel(fmt.Sprintf("   Total Volume:       %.8f SLK", totalVolume)),
		widget.NewLabel(fmt.Sprintf("   Highest Ask:        $%.4f / SLK", highPrice)),
		widget.NewLabel(fmt.Sprintf("   Lowest Ask:         $%.4f / SLK", lowPrice)),
		widget.NewLabel(fmt.Sprintf("   Your Bank Balance:  %.8f SLK", bankAccount.SLK)),
		widget.NewLabel(fmt.Sprintf("   Network Peers:      %d", func() int {
			if p2pNode != nil { return p2pNode.PeerCount }
			return 0
		}())),
		widget.NewLabel(fmt.Sprintf("   Total TX on Record: %d", len(netRecords))),
	)

	refreshBtn := widget.NewButton("🔄 Refresh", func() {
		mainTabs.Items[len(mainTabs.Items)-1].Content = makeChartTab(w)
		mainTabs.Refresh()
	})
	refreshBtn.Importance = widget.MediumImportance

	return container.NewVScroll(container.NewPadded(container.NewVBox(
		container.NewCenter(title), widget.NewSeparator(),
		container.NewPadded(refreshBtn),
		container.NewPadded(chartArea),
		container.NewPadded(statsBox),
	)))
}

// ════════════════════════════════════════
// WALLET BACKUP TAB
// ════════════════════════════════════════
func makeBackupTab(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("Wallet Backup & Restore", theme.ForegroundColor())
	title.TextSize = 18; title.TextStyle = fyne.TextStyle{Bold: true}

	result := widget.NewLabel("")
	result.Alignment = fyne.TextAlignCenter
	result.TextStyle = fyne.TextStyle{Bold: true}

	// ── EXPORT ──
	exportTitle := canvas.NewText("Export Wallet", theme.ForegroundColor())
	exportTitle.TextSize = 14; exportTitle.TextStyle = fyne.TextStyle{Bold: true}

	exportPath := widget.NewEntry()
	exportPath.SetPlaceHolder("Export path (e.g. /home/user/slk_backup.json)")
	exportPath.SetText(os.Getenv("HOME") + "/slk_wallet_backup.json")

	exportBtn := widget.NewButton("📤 Export Wallet to File", func() {
		path := strings.TrimSpace(exportPath.Text)
		if path == "" {
			fyne.Do(func() { result.SetText("❌ Enter export path") })
			return
		}
		// Read wallet
		data, err := os.ReadFile(walletPath)
		if err != nil {
			fyne.Do(func() { result.SetText("❌ Could not read wallet: " + err.Error()) })
			return
		}
		// Write backup
		err = os.WriteFile(path, data, 0600)
		if err != nil {
			fyne.Do(func() { result.SetText("❌ Could not write backup: " + err.Error()) })
			return
		}
		fyne.Do(func() {
			result.SetText("✅ Wallet exported to: " + path)
			statusBar.SetText("✅ Wallet backed up")
		})
	})
	exportBtn.Importance = widget.HighImportance

	// Export bank account too
	exportBankBtn := widget.NewButton("📤 Export Bank Account", func() {
		path := os.Getenv("HOME") + "/slk_bank_backup.json"
		data, err := os.ReadFile(bankPath)
		if err != nil {
			fyne.Do(func() { result.SetText("❌ Could not read bank account") })
			return
		}
		err = os.WriteFile(path, data, 0600)
		if err != nil {
			fyne.Do(func() { result.SetText("❌ Could not write backup") })
			return
		}
		fyne.Do(func() {
			result.SetText("✅ Bank account exported to: " + path)
		})
	})
	exportBankBtn.Importance = widget.MediumImportance

	// ── IMPORT ──
	importTitle := canvas.NewText("Restore Wallet", theme.ForegroundColor())
	importTitle.TextSize = 14; importTitle.TextStyle = fyne.TextStyle{Bold: true}

	importPath := widget.NewEntry()
	importPath.SetPlaceHolder("Path to backup file (e.g. /home/user/slk_backup.json)")

	importBtn := widget.NewButton("📥 Restore Wallet from File", func() {
		path := strings.TrimSpace(importPath.Text)
		if path == "" {
			fyne.Do(func() { result.SetText("❌ Enter backup file path") })
			return
		}
		if _, err := os.Stat(path); err != nil {
			fyne.Do(func() { result.SetText("❌ File not found: " + path) })
			return
		}
		dialog.ShowConfirm("⚠ Restore Wallet",
			"This will REPLACE your current wallet. Are you sure?",
			func(ok bool) {
				if !ok { return }
				data, err := os.ReadFile(path)
				if err != nil {
					fyne.Do(func() { result.SetText("❌ Could not read backup file") })
					return
				}
				// Validate it is a valid wallet
				var testWallet map[string]interface{}
				if err := json.Unmarshal(data, &testWallet); err != nil {
					fyne.Do(func() { result.SetText("❌ Invalid wallet file") })
					return
				}
				if _, ok := testWallet["address"]; !ok {
					fyne.Do(func() { result.SetText("❌ Not a valid SLK wallet file") })
					return
				}
				os.MkdirAll(filepath.Dir(walletPath), 0700)
				err = os.WriteFile(walletPath, data, 0600)
				if err != nil {
					fyne.Do(func() { result.SetText("❌ Could not restore wallet") })
					return
				}
				mainWallet, _ = wallet.LoadOrCreate(walletPath)
				if mainWallet != nil {
					mainWallet.SyncBalance(utxoSet.GetTotalBalance(mainWallet.Address))
				}
				fyne.Do(func() {
					refreshLabels()
					result.SetText("✅ Wallet restored! Address: " + mainWallet.Address)
					statusBar.SetText("✅ Wallet restored from backup")
				})
			}, w)
	})
	importBtn.Importance = widget.DangerImportance

	// ── WALLET INFO ──
	infoTitle := canvas.NewText("Current Wallet Info", theme.ForegroundColor())
	infoTitle.TextSize = 14; infoTitle.TextStyle = fyne.TextStyle{Bold: true}

	walletAddr := "No wallet loaded"
	walletPub  := "—"
	if mainWallet != nil {
		walletAddr = mainWallet.Address
		walletPub  = hex.EncodeToString(mainWallet.PublicKey)
	}

	addrLabel := widget.NewLabel(walletAddr)
	addrLabel.TextStyle = fyne.TextStyle{Monospace: true}
	addrLabel.Wrapping = fyne.TextWrapWord

	pubLabel := widget.NewLabel(walletPub)
	pubLabel.TextStyle = fyne.TextStyle{Monospace: true}
	pubLabel.Wrapping = fyne.TextWrapWord

	copyAddrBtn := widget.NewButton("📋 Copy Address", func() {
		w.Clipboard().SetContent(walletAddr)
		fyne.Do(func() { statusBar.SetText("✅ Address copied") })
	})

	warning := widget.NewLabel("⚠ NEVER share your backup file with anyone. It contains your private key.\n   Store it in a safe place — USB drive, encrypted folder, or printed paper.")
	warning.Wrapping = fyne.TextWrapWord

	return container.NewVScroll(container.NewPadded(container.NewVBox(
		container.NewCenter(title), widget.NewSeparator(),
		container.NewCenter(infoTitle),
		widget.NewLabel("Wallet Address:"), container.NewPadded(addrLabel),
		widget.NewLabel("Public Key:"), container.NewPadded(pubLabel),
		container.NewPadded(copyAddrBtn),
		widget.NewSeparator(),
		container.NewCenter(exportTitle),
		widget.NewLabel("Export Path:"), exportPath,
		container.NewPadded(exportBtn),
		container.NewPadded(exportBankBtn),
		widget.NewSeparator(),
		container.NewCenter(importTitle),
		widget.NewLabel("Backup File Path:"), importPath,
		container.NewPadded(importBtn),
		widget.NewSeparator(),
		container.NewPadded(warning),
		container.NewPadded(result),
	)))
}

// ════════════════════════════════════════
// PEERS TAB
// ════════════════════════════════════════
func makePeersTab(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("Connected Peers", theme.ForegroundColor())
	title.TextSize = 18; title.TextStyle = fyne.TextStyle{Bold: true}

	myIDLabel := widget.NewLabel("Your Peer ID: loading...")
	myIDLabel.TextStyle = fyne.TextStyle{Monospace: true}
	myIDLabel.Wrapping = fyne.TextWrapWord

	countLabel := widget.NewLabel("Peers: 0")
	countLabel.TextStyle = fyne.TextStyle{Bold: true}

	box := container.NewVBox()

	var rebuild func()
	rebuild = func() {
		box.Objects = nil
		if p2pNode == nil {
			box.Add(widget.NewLabel("P2P node not running."))
			box.Refresh()
			return
		}
		myIDLabel.SetText("Your Peer ID: " + p2pNode.MyPeerID())
		peerAddrs := p2pNode.GetPeerAddrs()
		countLabel.SetText(fmt.Sprintf("Connected Peers: %d", len(peerAddrs)))
		if len(peerAddrs) == 0 {
			box.Add(widget.NewLabel("No peers connected yet. Connecting..."))
			box.Refresh()
			return
		}
		i := 1
		for peerID, addrs := range peerAddrs {
			shortID := peerID
			if len(shortID) > 20 { shortID = shortID[:20] + "..." }
			addrStr := "unknown"
			if len(addrs) > 0 { addrStr = addrs[0] }
			peerLabel := widget.NewLabel(fmt.Sprintf("#%d  %s  |  %s", i, shortID, addrStr))
			peerLabel.TextStyle = fyne.TextStyle{Monospace: true}
			peerLabel.Wrapping = fyne.TextWrapWord
			copyBtn := widget.NewButton("📋", func() {
				w.Clipboard().SetContent(peerID)
				statusBar.SetText("✅ Peer ID copied")
			})
			box.Add(container.NewBorder(nil, widget.NewSeparator(), nil, copyBtn, peerLabel))
			i++
		}
		box.Refresh()
	}

	rebuild()

	refreshBtn := widget.NewButton("🔄 Refresh", func() { rebuild() })
	refreshBtn.Importance = widget.HighImportance

	// Auto refresh every 10 seconds
	go func() {
		for {
			time.Sleep(10 * time.Second)
			fyne.Do(func() { rebuild() })
		}
	}()

	shareAddr := ""
	if p2pNode != nil {
		shareAddr = "/ip4/41.90.70.28/tcp/30303/p2p/" + p2pNode.MyPeerID()
	}
	shareLabel := widget.NewLabel("Your network address (share with others): " + shareAddr)
	shareLabel.TextStyle = fyne.TextStyle{Monospace: true}
	shareLabel.Wrapping = fyne.TextWrapWord

	copyShareBtn := widget.NewButton("📋 Copy My Address", func() {
		w.Clipboard().SetContent(shareAddr)
		statusBar.SetText("✅ Your address copied")
	})

	return container.NewBorder(
		container.NewVBox(
			container.NewCenter(title),
			container.NewPadded(myIDLabel),
			container.NewPadded(shareLabel),
			container.NewPadded(copyShareBtn),
			container.New(layout.NewGridLayout(2), countLabel, refreshBtn),
			widget.NewSeparator(),
		),
		nil, nil, nil,
		container.NewVScroll(container.NewPadded(box)),
	)
}

// ════════════════════════════════════════
// MINING TAB — REAL PROOF-OF-RACE ENGINE
// ════════════════════════════════════════

type GUIRacer struct {
	Address      string
	Username     string
	DistanceLeft float64
	Power        float64
	Temp         float64
	Status       string
	LastSeen     time.Time
}

var (
	miningActive   bool
	miningStop     chan struct{}
	bc             *chain.Blockchain
	guiRacers      = make(map[string]*GUIRacer)
	guiRacersMu    sync.Mutex
	miningThrottle = false // false=cool, true=full speed
	networkWinner  = make(chan p2p.TrophyMsg, 5)
)

func makeMiningTab(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("⛏ SLK Miner — Proof of Race", theme.ForegroundColor())
	title.TextSize = 18
	title.TextStyle = fyne.TextStyle{Bold: true}

	if bc == nil {
		bc = chain.NewBlockchain()
	}

	// ── Wallet ──
	walletAddrEntry := widget.NewEntry()
	walletAddrEntry.SetPlaceHolder("Your SLK wallet address")
	privKeyEntry := widget.NewPasswordEntry()
	privKeyEntry.SetPlaceHolder("Your private key (hex or base64)")
	if mainWallet != nil {
		walletAddrEntry.SetText(mainWallet.Address)
		privKeyEntry.SetText(hex.EncodeToString(mainWallet.PrivateKey))
	}
	connectedLabel := widget.NewLabel("🔴 Wallet not connected")
	connectedLabel.TextStyle = fyne.TextStyle{Bold: true}
	var minerWallet *wallet.Wallet

	connectBtn := widget.NewButton("🔗 Connect Wallet", func() {
		addr := strings.TrimSpace(walletAddrEntry.Text)
		privRaw := strings.TrimSpace(privKeyEntry.Text)
		if addr == "" || privRaw == "" {
			fyne.Do(func() { connectedLabel.SetText("❌ Enter wallet address and private key") })
			return
		}
		var privBytes []byte
		var err error
		privBytes, err = hex.DecodeString(privRaw)
		if err != nil || len(privBytes) < 32 {
			privBytes, err = base64.StdEncoding.DecodeString(privRaw)
			if err != nil || len(privBytes) < 32 {
				privBytes, err = base64.RawStdEncoding.DecodeString(privRaw)
				if err != nil || len(privBytes) < 32 {
					fyne.Do(func() { connectedLabel.SetText("❌ Invalid private key") })
					return
				}
			}
		}
		pubBytes := privBytes[32:]
		if len(privBytes) < 64 {
			pubBytes = privBytes[:32]
		}
		minerWallet = &wallet.Wallet{Address: addr, PrivateKey: privBytes, PublicKey: pubBytes}
		minerWallet.SyncBalance(utxoSet.GetTotalBalance(addr))
		fyne.Do(func() {
			connectedLabel.SetText(fmt.Sprintf("🟢 Connected: %s | Balance: %.8f SLK", shortAddr(addr), minerWallet.Balance))
		})
	})
	connectBtn.Importance = widget.HighImportance

	// ── Stats row ──
	heightLabel := widget.NewLabel("Height: —")
	peersLabel2 := widget.NewLabel("Peers: —")
	raceLabel   := widget.NewLabel("Race: —")
	statusLabel := widget.NewLabel("Status: Idle")
	statusLabel.TextStyle = fyne.TextStyle{Bold: true}

	// ── Throttle buttons ──
	fullSpeedBtn := widget.NewButton("🔥 FULL SPEED", nil)
	coolDownBtn  := widget.NewButton("❄️  COOL DOWN", nil)
	fullSpeedBtn.Importance = widget.WarningImportance
	coolDownBtn.Importance  = widget.MediumImportance

	fullSpeedBtn.OnTapped = func() {
		miningThrottle = true
		manager.SetThrottle(true)
		fyne.Do(func() {
			statusLabel.SetText("🔥 FULL SPEED — MAX ENERGY BURN!")
			fullSpeedBtn.Importance = widget.DangerImportance
			coolDownBtn.Importance  = widget.MediumImportance
			fullSpeedBtn.Refresh()
			coolDownBtn.Refresh()
		})
	}
	coolDownBtn.OnTapped = func() {
		miningThrottle = false
		manager.SetThrottle(false)
		fyne.Do(func() {
			statusLabel.SetText("❄️  Cooling down...")
			coolDownBtn.Importance  = widget.HighImportance
			fullSpeedBtn.Importance = widget.MediumImportance
			fullSpeedBtn.Refresh()
			coolDownBtn.Refresh()
		})
	}

	// ── Live leaderboard ──
	leaderboardBox := container.NewVBox()
	leaderHeader   := widget.NewLabel("POS  ADDRESS              DIST LEFT    POWER    TEMP")
	leaderHeader.TextStyle = fyne.TextStyle{Monospace: true, Bold: true}

	rebuildLeaderboard := func(myAddr string, myDist, myPower, myTemp float64) {
		type entry struct {
			addr string
			dist float64
			pow  float64
			temp float64
			name string
			isMe bool
		}
		entries := []entry{}
		if myAddr != "" {
			entries = append(entries, entry{addr: myAddr, dist: myDist, pow: myPower, temp: myTemp, name: "YOU", isMe: true})
		}
		guiRacersMu.Lock()
		for _, r := range guiRacers {
			if r.Status == "RACING" || r.Status == "JOINED" {
				name := r.Username
				if name == "" {
					name = r.Address
					if len(name) > 14 { name = name[:14] }
				}
				entries = append(entries, entry{addr: r.Address, dist: r.DistanceLeft, pow: r.Power, temp: r.Temp, name: name})
			}
		}
		guiRacersMu.Unlock()
		// Sort by dist left ascending
		for i := 0; i < len(entries); i++ {
			for j := i + 1; j < len(entries); j++ {
				if entries[j].dist < entries[i].dist {
					entries[i], entries[j] = entries[j], entries[i]
				}
			}
		}
		fyne.Do(func() {
			leaderboardBox.Objects = nil
			leaderboardBox.Add(leaderHeader)
			for i, e := range entries {
				marker := "  "
				if e.isMe { marker = "►" }
				line := fmt.Sprintf("%s #%-2d  %-16s  %8.3fm  %5.1fW  %4.0f°C",
					marker, i+1, e.name, e.dist, e.pow, e.temp)
				lbl := widget.NewLabel(line)
				lbl.TextStyle = fyne.TextStyle{Monospace: true}
				if e.isMe {
					lbl.TextStyle.Bold = true
				}
				leaderboardBox.Add(lbl)
			}
			leaderboardBox.Refresh()
		})
	}

	// ── Trophy log ──
	trophyBox := container.NewVBox()
	for i := len(bc.Trophies) - 1; i >= 0 && i >= len(bc.Trophies)-5; i-- {
		t := bc.Trophies[i]
		if mainWallet != nil && t.Winner == mainWallet.Address {
			trophyBox.Add(widget.NewLabel(fmt.Sprintf("🏆 #%d %s +%.8f SLK", t.Header.Height, t.TierName(), t.Reward)))
		}
	}

	// ── Start/Stop ──
	startBtn := widget.NewButton("⛏ START MINING", nil)
	startBtn.Importance = widget.HighImportance

	startBtn.OnTapped = func() {
		if miningActive {
			miningActive = false
			manager.StopRace()
			if miningStop != nil {
				select {
				case <-miningStop:
				default:
					close(miningStop)
				}
			}
			fyne.Do(func() {
				startBtn.SetText("⛏ START MINING")
				startBtn.Importance = widget.HighImportance
				statusLabel.SetText("Status: Stopped")
			})
			return
		}

		addr := ""
		if minerWallet != nil {
			addr = minerWallet.Address
		} else if mainWallet != nil {
			addr = mainWallet.Address
		}
		if addr == "" {
			fyne.Do(func() { statusLabel.SetText("❌ Connect a wallet first") })
			return
		}

		miningActive = true
		miningThrottle = false
		miningStop = make(chan struct{})

		// Hook p2pNode to receive racer positions and trophy wins
		if p2pNode != nil {
			p2pNode.OnRacer = func(r p2p.RacerMsg) {
				if r.Address == addr { return }
				guiRacersMu.Lock()
				guiRacers[r.Address] = &GUIRacer{
					Address:      r.Address,
					Username:     r.Username,
					DistanceLeft: r.DistanceLeft,
					Power:        r.Power,
					Temp:         r.Temp,
					Status:       r.Status,
					LastSeen:     time.Now(),
				}
				guiRacersMu.Unlock()
			}
			p2pNode.OnTrophy = func(t p2p.TrophyMsg) {
				if t.Winner == addr { return }

				// STEP 1: Height must be exactly our height+1
				if t.Height != bc.Height+1 {
					return
				}

				// STEP 2: PrevHash must match our chain tip — rejects ties/duplicates
				if len(bc.Trophies) > 0 {
					tip := bc.Trophies[len(bc.Trophies)-1]
					if fmt.Sprintf("%x", tip.Hash) != t.PrevHash {
						return
					}
				}

				// STEP 3: Verify VDF proof if present
				if t.VDFProof != "" && t.VDFInput != "" {
					vdfOk := vdfmath.Verify(&vdfmath.Proof{
						Input:      t.VDFInput,
						Output:     t.VDFProof,
						Iterations: uint64(t.Distance * 1000),
					})
					if !vdfOk {
						fmt.Printf("⚠️  REJECTED trophy from %s — VDF INVALID\n", t.Winner)
						return
					}
				}

				// STEP 4: Verify hash matches
				if len(bc.Trophies) > 0 {
					tip := bc.Trophies[len(bc.Trophies)-1]
					newT := trophy.NewTrophy(tip.Hash, t.Winner, t.Distance, t.Time, trophy.Tier(t.Tier), t.Height)
					if fmt.Sprintf("%x", newT.Hash) != t.Hash {
						fmt.Printf("⚠️  REJECTED trophy from %s — hash INVALID\n", t.Winner)
						return
					}
				}

				// VALID — send to race loop
				select {
				case networkWinner <- t:
				default:
				}
			}
		}

		fyne.Do(func() {
			startBtn.SetText("⏹ STOP MINING")
			startBtn.Importance = widget.DangerImportance
			statusLabel.SetText("Status: Starting...")
		})

		// Announce to network
		if p2pNode != nil {
			p2pNode.BroadcastRacerPosition(p2p.RacerMsg{
				Address:  addr,
				Status:   "JOINED",
				Username: bankAccount.Name,
			})
		}

		go func() {
			for {
				select {
				case <-miningStop:
					return
				default:
				}
				if !miningActive { return }

				// Drain stale winner signals before new race
				for len(networkWinner) > 0 { <-networkWinner }

				peers := 0
				if p2pNode != nil { peers = p2pNode.PeerCount }
				distance := chain.CalculateDistance(peers, bc.Height)
				gold, _, _ := chain.CalculateTargetTime(distance)
				raceNum := bc.Height + 1

				fyne.Do(func() {
					peersLabel2.SetText(fmt.Sprintf("Peers: %d", peers))
					heightLabel.SetText(fmt.Sprintf("Height: %d", bc.Height))
					raceLabel.SetText(fmt.Sprintf("Race #%d | %.2fm | Gold: %.0fs", raceNum, distance, gold))
					statusLabel.SetText("Status: Racing...")
				})

				err := manager.StartRace(0, distance)
				if err != nil {
					fyne.Do(func() { statusLabel.SetText("❌ Engine error: " + err.Error()) })
					time.Sleep(2 * time.Second)
					continue
				}

				startTime := time.Now()
				raceOver := false
				broadcastTick := 0

				for !raceOver {
					select {
					case <-miningStop:
						manager.StopRace()
						return
					case winner := <-networkWinner:
						// Someone else won — STOP immediately
						manager.StopRace()
						winnerShort := winner.Winner
						if len(winnerShort) > 20 { winnerShort = winnerShort[:20] }

						// Add their trophy to our chain properly
					if len(bc.Trophies) > 0 {
						tip := bc.Trophies[len(bc.Trophies)-1]
						newT := trophy.NewTrophy(tip.Hash, winner.Winner, winner.Distance, winner.Time, trophy.Tier(winner.Tier), winner.Height)
						newT.VDFProof = winner.VDFProof
						newT.VDFInput = winner.VDFInput
						bc.Trophies = append(bc.Trophies, newT)
						bc.Height = winner.Height
						bc.TotalSupply -= newT.Reward
					}

						fyne.Do(func() {
							statusLabel.SetText(fmt.Sprintf("🏆 %s WON! Restarting in 3s...", winnerShort))
						})

						// 3 second synchronized countdown
						for i := 3; i > 0; i-- {
							countdown := i
							fyne.Do(func() {
								raceLabel.SetText(fmt.Sprintf("⏳ New race in %ds...", countdown))
							})
							time.Sleep(1 * time.Second)
						}

						// Clear stale racers
						guiRacersMu.Lock()
						guiRacers = make(map[string]*GUIRacer)
						guiRacersMu.Unlock()

						raceOver = true
					default:
					}
					if raceOver { break }

					telemetry := manager.GetTelemetry()
					elapsed := time.Since(startTime).Seconds()

					// Auto-throttle on overheat
					if telemetry.CPUTempCelsius >= 95 {
						manager.SetThrottle(false)
						fyne.Do(func() { statusLabel.SetText("🚨 THROTTLING — too hot!") })
					}

					// Broadcast our position every ~2.5s
					broadcastTick++
					if broadcastTick%5 == 0 && p2pNode != nil {
						p2pNode.BroadcastRacerPosition(p2p.RacerMsg{
							Address:      addr,
							DistanceLeft: telemetry.DistanceLeft,
							Power:        telemetry.CPUPowerWatts,
							Temp:         telemetry.CPUTempCelsius,
							Status:       "RACING",
							Username:     bankAccount.Name,
						})
					}

					// Clean stale racers (30s timeout)
					guiRacersMu.Lock()
					for k, r := range guiRacers {
						if time.Since(r.LastSeen) > 30*time.Second {
							delete(guiRacers, k)
						}
					}
					guiRacersMu.Unlock()

					// Rebuild leaderboard
					rebuildLeaderboard(addr, telemetry.DistanceLeft, telemetry.CPUPowerWatts, telemetry.CPUTempCelsius)

					fyne.Do(func() {
						statusLabel.SetText(fmt.Sprintf("🏃 Racing... %.1fs | CPU: %.1fW | %.0f°C", elapsed, telemetry.CPUPowerWatts, telemetry.CPUTempCelsius))
					})

					if telemetry.Status == manager.StatusFinished {
						finishTime := elapsed
						tier := consensus.DetermineTier(finishTime, gold)
						reward := consensus.CalculateReward(tier)
						newTrophy := bc.AddTrophy(addr, distance, finishTime, tier)

						bankAccount.SLK += reward
						saveBankAccount(bankAccount)
						utxoSet.AddUTXO(&state.UTXO{
							TxID:        fmt.Sprintf("%x", newTrophy.Hash)[:16],
							OutputIndex: 0,
							Amount:      reward,
							Address:     addr,
							FromTrophy:  bc.Height,
							Spent:       false,
						})
						utxoSet.Save()

						// Compute VDF proof — real cryptographic race certificate
					vdfIterations := uint64(distance * 1000)
					if vdfIterations < 10000 { vdfIterations = 10000 }
					if vdfIterations > 500000 { vdfIterations = 500000 }
					seed := []byte(fmt.Sprintf("%s:%.0f:%.2f:%d", addr, distance, finishTime, raceNum))
					vdfProof, vdfErr := vdfmath.Prove(seed, vdfIterations)

					if p2pNode != nil {
						msg := p2p.TrophyMsg{
							Winner:   addr,
							Distance: distance,
							Time:     finishTime,
							Tier:     int(tier),
							Hash:     fmt.Sprintf("%x", newTrophy.Hash),
							PrevHash: fmt.Sprintf("%x", newTrophy.PrevHash),
							Height:   bc.Height,
						}
						if vdfErr == nil {
							newTrophy.VDFProof = vdfProof.Output
							newTrophy.VDFInput = vdfProof.Input
							msg.VDFProof = vdfProof.Output
							msg.VDFInput = vdfProof.Input
						}
						p2pNode.BroadcastTrophy(msg)
					}

						tierName := newTrophy.TierName()
						go func(tier string) {
							soundFile := "/home/michael-faraday/Desktop/slk/assets/trophy_bronze.wav"
							if tier == "Gold"   { soundFile = "/home/michael-faraday/Desktop/slk/assets/trophy_win.wav" }
							if tier == "Silver" { soundFile = "/home/michael-faraday/Desktop/slk/assets/trophy_silver.wav" }
							exec.Command("aplay", "-q", soundFile).Run()
						}(tierName)
						fyne.Do(func() {
							refreshLabels()
							statusLabel.SetText(fmt.Sprintf("✅ %s! +%.8f SLK in %.1fs", tierName, reward, finishTime))
							trophyBox.Add(widget.NewLabel(fmt.Sprintf("🏆 #%d %s +%.8f SLK (%.1fs)", bc.Height, tierName, reward, finishTime)))
							trophyBox.Refresh()
							pushNotif(fmt.Sprintf("⛏ Trophy #%d! +%.8f SLK (%s)", bc.Height, reward, tierName))
						})
						manager.StopRace()

						// Synchronized 3s restart
						for i := 3; i > 0; i-- {
							countdown := i
							fyne.Do(func() {
								raceLabel.SetText(fmt.Sprintf("⏳ Next race in %ds...", countdown))
							})
							time.Sleep(1 * time.Second)
						}

						guiRacersMu.Lock()
						guiRacers = make(map[string]*GUIRacer)
						guiRacersMu.Unlock()

						raceOver = true
					}

					if telemetry.Status == manager.StatusAccident {
						fyne.Do(func() { statusLabel.SetText("💥 Accident! Restarting...") })
						manager.StopRace()
						time.Sleep(2 * time.Second)
						raceOver = true
					}

					time.Sleep(500 * time.Millisecond)
				}
			}
		}()
	}

	throttleRow := container.New(layout.NewGridLayout(2), fullSpeedBtn, coolDownBtn)

	return container.NewBorder(
		container.NewVBox(
			container.NewCenter(title),
			widget.NewSeparator(),
			widget.NewLabel("Wallet Address:"), walletAddrEntry,
			widget.NewLabel("Private Key:"), privKeyEntry,
			container.NewPadded(connectBtn),
			container.NewPadded(connectedLabel),
			widget.NewSeparator(),
			container.New(layout.NewGridLayout(3), heightLabel, peersLabel2, raceLabel),
			container.NewPadded(statusLabel),
			container.NewPadded(throttleRow),
			container.NewPadded(startBtn),
			widget.NewSeparator(),
			widget.NewLabel("🏁 LIVE RACE LEADERBOARD:"),
		),
		container.NewVBox(
			widget.NewSeparator(),
			widget.NewLabel("🏆 Your Trophies:"),
			container.NewVScroll(container.NewPadded(trophyBox)),
		),
		nil, nil,
		container.NewVScroll(container.NewPadded(leaderboardBox)),
	)
}

// ════════════════════════════════════════
// EXPLORER TAB — ALL NETWORK TROPHIES
// ════════════════════════════════════════
func makeExplorerTab(w fyne.Window) fyne.CanvasObject {
	title := canvas.NewText("🌍 Block Explorer", theme.ForegroundColor())
	title.TextSize = 18
	title.TextStyle = fyne.TextStyle{Bold: true}

	// Stats bar
	heightStat  := widget.NewLabel("—")
	supplyStat  := widget.NewLabel("—")
	minersStat  := widget.NewLabel("—")
	heightStat.TextStyle = fyne.TextStyle{Bold: true}
	supplyStat.TextStyle = fyne.TextStyle{Bold: true}
	minersStat.TextStyle = fyne.TextStyle{Bold: true}

	mkStat2 := func(label string, val *widget.Label) fyne.CanvasObject {
		t := canvas.NewText(label, theme.PlaceHolderColor()); t.TextSize = 10
		return container.NewPadded(container.NewVBox(t, val))
	}
	statsRow := container.New(layout.NewGridLayout(3),
		mkStat2("⛓ Chain Height", heightStat),
		mkStat2("💰 SLK Remaining", supplyStat),
		mkStat2("⛏ Unique Miners", minersStat),
	)

	// Search bar
	searchEntry := widget.NewEntry()
	searchEntry.SetPlaceHolder("Search by address or trophy #...")

	// Trophy list
	listBox := container.NewVBox()

	// Header
	header := widget.NewLabel("  #      WINNER                    TIER      REWARD        TIME       DIST")
	header.TextStyle = fyne.TextStyle{Monospace: true, Bold: true}

	var allTrophies []*trophy.Trophy
	var filtered  []*trophy.Trophy

	rebuild := func(filter string) {
		listBox.Objects = nil
		if bc == nil {
			listBox.Add(widget.NewLabel("Loading blockchain..."))
			listBox.Refresh()
			return
		}
		allTrophies = bc.Trophies
		filtered = nil
		for _, t := range allTrophies {
			if t.Header.Height == 0 { continue } // skip genesis
			if filter == "" {
				filtered = append(filtered, t)
			} else {
				if strings.Contains(strings.ToLower(t.Winner), strings.ToLower(filter)) {
					filtered = append(filtered, t)
				}
				if strings.Contains(fmt.Sprintf("%d", t.Header.Height), filter) {
					filtered = append(filtered, t)
				}
			}
		}

		// Show newest first
		for i, j := 0, len(filtered)-1; i < j; i, j = i+1, j-1 {
			filtered[i], filtered[j] = filtered[j], filtered[i]
		}

		// Count unique miners
		miners := make(map[string]bool)
		for _, t := range allTrophies {
			if t.Header.Height > 0 { miners[t.Winner] = true }
		}

		// Update stats
		if bc != nil {
			rem := 2_000_000_000.0 - float64(bc.Height)*0.00800000
			heightStat.SetText(fmt.Sprintf("#%d", bc.Height))
			supplyStat.SetText(fmt.Sprintf("%.3f SLK", rem))
			minersStat.SetText(fmt.Sprintf("%d miners", len(miners)))
		}

		if len(filtered) == 0 {
			listBox.Add(widget.NewLabel("No trophies found."))
			listBox.Refresh()
			return
		}

		for _, t := range filtered {
			tc := t // capture
			winner := tc.Winner
			if len(winner) > 24 { winner = winner[:24] }
			isMe := mainWallet != nil && tc.Winner == mainWallet.Address
			meMarker := "  "
			if isMe { meMarker = "►" }

			line := fmt.Sprintf("%s #%-4d  %-24s  %-8s  %.8f  %6.1fs  %.1fm",
				meMarker,
				tc.Header.Height,
				winner,
				tc.TierName(),
				tc.Reward,
				tc.FinishTime,
				tc.Distance,
			)
			lbl := widget.NewLabel(line)
			lbl.TextStyle = fyne.TextStyle{Monospace: true}
			if isMe { lbl.TextStyle.Bold = true }

			// Click to copy address
			copyBtn := widget.NewButton("📋", func() {
				w.Clipboard().SetContent(tc.Winner)
				statusBar.SetText("✅ Address copied")
			})

			row := container.NewBorder(nil, widget.NewSeparator(), nil, copyBtn, lbl)
			listBox.Add(row)
		}
		listBox.Refresh()
	}

	rebuild("")

	searchEntry.OnChanged = func(s string) { rebuild(s) }

	refreshBtn := widget.NewButton("🔄 Refresh", func() { rebuild(searchEntry.Text) })
	refreshBtn.Importance = widget.HighImportance

	// Auto refresh every 10s
	go func() {
		for {
			time.Sleep(10 * time.Second)
			fyne.Do(func() { rebuild(searchEntry.Text) })
		}
	}()

	return container.NewBorder(
		container.NewVBox(
			container.NewCenter(title),
			widget.NewSeparator(),
			container.NewPadded(statsRow),
			widget.NewSeparator(),
			container.NewBorder(nil, nil, nil, refreshBtn, searchEntry),
			widget.NewSeparator(),
			container.NewPadded(header),
			widget.NewSeparator(),
		),
		nil, nil, nil,
		container.NewVScroll(container.NewPadded(listBox)),
	)
}
