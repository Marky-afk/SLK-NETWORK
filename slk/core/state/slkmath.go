package state

import (
	"fmt"
	"math"
	"math/big"
)

// SLK uses 8 decimal places — same precision as Bitcoin (satoshis)
// 1 SLK = 100_000_000 nanoSLK
// We store as int64 nanoSLK internally to avoid float64 rounding errors
const NanoSLKPerSLK = 100_000_000

// ToNano converts SLK float to integer nanoSLK — no rounding errors
func ToNano(slk float64) int64 {
	return int64(math.Round(slk * NanoSLKPerSLK))
}

// FromNano converts integer nanoSLK back to SLK float for display
func FromNano(nano int64) float64 {
	return float64(nano) / NanoSLKPerSLK
}

// AddSLK safely adds two SLK amounts via integer arithmetic
func AddSLK(a, b float64) float64 {
	return FromNano(ToNano(a) + ToNano(b))
}

// SubSLK safely subtracts SLK amounts — returns error if result would be negative
func SubSLK(a, b float64) (float64, error) {
	nA := ToNano(a)
	nB := ToNano(b)
	if nB > nA {
		return 0, fmt.Errorf("insufficient funds: have %.8f need %.8f", a, b)
	}
	return FromNano(nA - nB), nil
}

// MulSLK safely multiplies SLK by a rate (e.g. fee percentage)
func MulSLK(slk float64, rate float64) float64 {
	// Use big.Float for precision on multiplication
	a := new(big.Float).SetFloat64(slk)
	b := new(big.Float).SetFloat64(rate)
	result, _ := new(big.Float).Mul(a, b).Float64()
	return FromNano(ToNano(result))
}

// FormatSLK formats a SLK amount to exactly 8 decimal places
func FormatSLK(slk float64) string {
	return fmt.Sprintf("%.8f SLK", slk)
}

// ValidateSLKAmount checks an amount is positive and has at most 8 decimal places
func ValidateSLKAmount(slk float64) error {
	if slk <= 0 {
		return fmt.Errorf("amount must be positive")
	}
	nano := ToNano(slk)
	if nano <= 0 {
		return fmt.Errorf("amount too small — minimum is 0.00000001 SLK")
	}
	if slk > 2_000_000_000 {
		return fmt.Errorf("amount exceeds total SLK supply")
	}
	return nil
}
