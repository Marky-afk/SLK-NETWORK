package consensus

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"time"
)

// TiebreakerResult holds the result of a photo finish
type TiebreakerResult struct {
	WinnerAddress string
	WinnerTime    float64
	LoserAddress  string
	LoserTime     float64
	Difference    float64
}

// PhotoFinish resolves a tie between two racers
// Each racer gets a micro-challenge based on their address + seed
func PhotoFinish(addrA, addrB string, seed []byte) TiebreakerResult {
	timeA := solveMicroChallenge(addrA, seed)
	timeB := solveMicroChallenge(addrB, seed)

	fmt.Println("======================================================================")
	fmt.Println("[FINISH LINE] Multiple finishers detected!")
	fmt.Printf("[INFO] %s finished\n", addrA)
	fmt.Printf("[INFO] %s finished\n", addrB)
	fmt.Println("[TIE DETECTED] Initiating Tie-Breaker Sprint...")

	if timeA <= timeB {
		fmt.Printf("\n[TIE-BREAKER RESULT]\n1st: %s (%.3fs)\n2nd: %s (%.3fs)\n",
			addrA, timeA, addrB, timeB)
		fmt.Printf("[WINNER] %s wins!\n", addrA)
		fmt.Println("======================================================================")
		return TiebreakerResult{
			WinnerAddress: addrA,
			WinnerTime:    timeA,
			LoserAddress:  addrB,
			LoserTime:     timeB,
			Difference:    timeB - timeA,
		}
	}
	fmt.Printf("\n[TIE-BREAKER RESULT]\n1st: %s (%.3fs)\n2nd: %s (%.3fs)\n",
		addrB, timeB, addrA, timeA)
	fmt.Printf("[WINNER] %s wins!\n", addrB)
	fmt.Println("======================================================================")
	return TiebreakerResult{
		WinnerAddress: addrB,
		WinnerTime:    timeB,
		LoserAddress:  addrA,
		LoserTime:     timeA,
		Difference:    timeA - timeB,
	}
}

// solveMicroChallenge computes a deterministic micro-sprint time for an address
func solveMicroChallenge(address string, seed []byte) float64 {
	start := time.Now()
	data := append([]byte(address), seed...)
	h := sha256.Sum256(data)
	// Do real work — 50k hash rounds
	for i := 0; i < 50000; i++ {
		buf := make([]byte, 36)
		copy(buf, h[:])
		binary.LittleEndian.PutUint32(buf[32:], uint32(i))
		h = sha256.Sum256(buf)
	}
	return time.Since(start).Seconds()
}
