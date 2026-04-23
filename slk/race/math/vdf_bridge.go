package math

/*
#cgo LDFLAGS: -L${SRCDIR}/../../build/linux -lslkvdf
#include "vdf.h"
#include <stdlib.h>
*/
import "C"
import (
	"encoding/hex"
	"fmt"
	"unsafe"
)

// Proof holds the VDF proof in Go
type Proof struct {
	Input      string
	Output     string
	Iterations uint64
	TimeTaken  float64
	Valid      bool
}

// Prove runs the real VDF computation
// This consumes real CPU cycles and takes real time
func Prove(seed []byte, iterations uint64) (*Proof, error) {
	if len(seed) < 32 {
		// Pad seed to 32 bytes
		padded := make([]byte, 32)
		copy(padded, seed)
		seed = padded
	}

	var cProof C.VDFProof

	ret := C.vdf_prove(
		(*C.uint8_t)(unsafe.Pointer(&seed[0])),
		C.uint64_t(iterations),
		&cProof,
	)

	if ret != 0 {
		return nil, fmt.Errorf("VDF prove failed")
	}

	output := C.GoBytes(unsafe.Pointer(&cProof.output[0]), 32)

	return &Proof{
		Input:      hex.EncodeToString(seed),
		Output:     hex.EncodeToString(output),
		Iterations: iterations,
		TimeTaken:  float64(cProof.time_taken),
		Valid:      true,
	}, nil
}

// Verify checks the VDF proof cryptographically
func Verify(proof *Proof) bool {
	input, _ := hex.DecodeString(proof.Input)
	output, _ := hex.DecodeString(proof.Output)

	if len(input) < 32 || len(output) < 32 {
		return false
	}

	var cProof C.VDFProof
	copy((*[32]byte)(unsafe.Pointer(&cProof.input[0]))[:], input)
	copy((*[32]byte)(unsafe.Pointer(&cProof.output[0]))[:], output)
	cProof.iterations = C.uint64_t(proof.Iterations)

	result := C.vdf_verify(&cProof)
	return result == 1
}
