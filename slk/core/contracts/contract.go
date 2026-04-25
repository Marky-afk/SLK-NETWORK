package contracts

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// SLK Smart Contract System
// Contracts are programmable conditions on SLK transactions
// They execute automatically when conditions are met

type ContractType string

const (
	TypeEscrow      ContractType = "ESCROW"       // Release funds when both parties confirm
	TypeVesting     ContractType = "VESTING"       // Release funds over time schedule
	TypeMultiSig    ContractType = "MULTISIG"      // Require N-of-M signatures
	TypeConditional ContractType = "CONDITIONAL"   // Release if condition string is met
	TypeSavings     ContractType = "SAVINGS"       // Lock until target amount reached
	TypeWill        ContractType = "WILL"          // Release after inactivity period
)

type ContractStatus string

const (
	StatusActive    ContractStatus = "ACTIVE"
	StatusExecuted  ContractStatus = "EXECUTED"
	StatusCancelled ContractStatus = "CANCELLED"
	StatusExpired   ContractStatus = "EXPIRED"
)

// Contract is a programmable SLK transaction condition
type Contract struct {
	ID           string         `json:"id"`
	Type         ContractType   `json:"type"`
	Creator      string         `json:"creator"`
	Beneficiary  string         `json:"beneficiary"`
	Amount       float64        `json:"amount"`        // SLK locked
	Status       ContractStatus `json:"status"`
	CreatedAt    int64          `json:"created_at"`
	ExpiresAt    int64          `json:"expires_at"`    // 0 = never
	ExecutedAt   int64          `json:"executed_at"`
	Title        string         `json:"title"`
	Description  string         `json:"description"`

	// Escrow fields
	CreatorConfirmed     bool `json:"creator_confirmed"`
	BeneficiaryConfirmed bool `json:"beneficiary_confirmed"`

	// Vesting fields
	VestingStart    int64   `json:"vesting_start"`
	VestingEnd      int64   `json:"vesting_end"`
	VestingClaimed  float64 `json:"vesting_claimed"`
	VestingInterval string  `json:"vesting_interval"` // "monthly","weekly","daily"

	// MultiSig fields
	Signers      []string `json:"signers"`
	Signatures   []string `json:"signatures"`
	SigsRequired int      `json:"sigs_required"`

	// Conditional fields
	Condition    string `json:"condition"`     // human-readable condition
	ConditionMet bool   `json:"condition_met"`

	// Savings fields
	SavingsTarget  float64 `json:"savings_target"`
	SavingsBalance float64 `json:"savings_balance"`

	// Will fields
	InactivityDays int64 `json:"inactivity_days"`
	LastActivity   int64 `json:"last_activity"`
}

// VestingClaimable returns how much SLK can be claimed right now
func (c *Contract) VestingClaimable() float64 {
	if c.Type != TypeVesting { return 0 }
	now := time.Now().Unix()
	if now < c.VestingStart { return 0 }
	end := c.VestingEnd
	if end == 0 { end = now }
	elapsed := float64(now - c.VestingStart)
	total := float64(c.VestingEnd - c.VestingStart)
	if total <= 0 { return c.Amount - c.VestingClaimed }
	vested := (elapsed / total) * c.Amount
	if vested > c.Amount { vested = c.Amount }
	return vested - c.VestingClaimed
}

// IsWillTriggered returns true if inactivity period has passed
func (c *Contract) IsWillTriggered() bool {
	if c.Type != TypeWill { return false }
	if c.InactivityDays <= 0 { return false }
	deadline := c.LastActivity + (c.InactivityDays * 86400)
	return time.Now().Unix() > deadline
}

// Validate checks contract fields are valid before creation
func (c *Contract) Validate() error {
	if c.Creator == "" { return fmt.Errorf("missing creator") }
	if c.Beneficiary == "" { return fmt.Errorf("missing beneficiary") }
	if c.Amount <= 0 { return fmt.Errorf("amount must be positive") }
	if c.Title == "" { return fmt.Errorf("missing title") }
	switch c.Type {
	case TypeEscrow:
		// valid
	case TypeVesting:
		if c.VestingEnd <= c.VestingStart { return fmt.Errorf("vesting end must be after start") }
	case TypeMultiSig:
		if len(c.Signers) < 2 { return fmt.Errorf("multisig needs at least 2 signers") }
		if c.SigsRequired < 1 || c.SigsRequired > len(c.Signers) {
			return fmt.Errorf("invalid signatures required")
		}
	case TypeConditional:
		if c.Condition == "" { return fmt.Errorf("missing condition") }
	case TypeSavings:
		if c.SavingsTarget <= 0 { return fmt.Errorf("savings target must be positive") }
	case TypeWill:
		if c.InactivityDays < 30 { return fmt.Errorf("inactivity period must be at least 30 days") }
	default:
		return fmt.Errorf("unknown contract type: %s", c.Type)
	}
	return nil
}

// ContractStore manages all contracts on disk
type ContractStore struct {
	Contracts []*Contract `json:"contracts"`
	path      string
}

func NewContractStore(dataDir string) *ContractStore {
	path := dataDir + "/contracts.json"
	cs := &ContractStore{path: path}
	cs.load()
	return cs
}

func (cs *ContractStore) Add(c *Contract) error {
	if err := c.Validate(); err != nil { return err }
	cs.Contracts = append(cs.Contracts, c)
	return cs.save()
}

func (cs *ContractStore) GetByID(id string) *Contract {
	for _, c := range cs.Contracts { if c.ID == id { return c } }
	return nil
}

func (cs *ContractStore) GetByAddress(addr string) []*Contract {
	var result []*Contract
	for _, c := range cs.Contracts {
		if c.Creator == addr || c.Beneficiary == addr { result = append(result, c) }
	}
	return result
}

func (cs *ContractStore) Save() error { return cs.save() }

func (cs *ContractStore) save() error {
	os.MkdirAll(cs.path[:len(cs.path)-len("/contracts.json")], 0700)
	data, err := json.MarshalIndent(cs, "", "  ")
	if err != nil { return err }
	tmp := cs.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil { return err }
	return os.Rename(tmp, cs.path)
}

func (cs *ContractStore) load() {
	data, err := os.ReadFile(cs.path)
	if err != nil { return }
	json.Unmarshal(data, cs)
}

// CheckAndExecute runs through all active contracts and executes any that are ready
func (cs *ContractStore) CheckAndExecute(onExecute func(c *Contract) error) {
	now := time.Now().Unix()
	for _, c := range cs.Contracts {
		if c.Status != StatusActive { continue }
		// Check expiry
		if c.ExpiresAt > 0 && now > c.ExpiresAt {
			c.Status = StatusExpired
			cs.save()
			continue
		}
		switch c.Type {
		case TypeEscrow:
			if c.CreatorConfirmed && c.BeneficiaryConfirmed {
				if err := onExecute(c); err == nil {
					c.Status = StatusExecuted
					c.ExecutedAt = now
					cs.save()
				}
			}
		case TypeVesting:
			claimable := c.VestingClaimable()
			if claimable > 0 {
				if err := onExecute(c); err == nil {
					c.VestingClaimed += claimable
					if c.VestingClaimed >= c.Amount { c.Status = StatusExecuted }
					c.ExecutedAt = now
					cs.save()
				}
			}
		case TypeWill:
			if c.IsWillTriggered() {
				if err := onExecute(c); err == nil {
					c.Status = StatusExecuted
					c.ExecutedAt = now
					cs.save()
				}
			}
		}
	}
}
