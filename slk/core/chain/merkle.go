package chain

import (
	"crypto/sha256"
	"encoding/hex"
)

// MerkleTree builds a Merkle tree from transaction IDs
// This makes it mathematically impossible to tamper with any single tx
// without changing the Merkle root — and therefore the block hash

// MerkleRoot computes the root hash of a list of transaction IDs
// If txIDs is empty, returns a zero hash
func MerkleRoot(txIDs []string) string {
	if len(txIDs) == 0 {
		return hex.EncodeToString(make([]byte, 32))
	}

	// Convert to byte hashes
	hashes := make([][]byte, len(txIDs))
	for i, id := range txIDs {
		h := sha256.Sum256([]byte(id))
		hashes[i] = h[:]
	}

	// Build tree bottom-up
	for len(hashes) > 1 {
		if len(hashes)%2 != 0 {
			// Duplicate last hash if odd number (same as Bitcoin)
			hashes = append(hashes, hashes[len(hashes)-1])
		}
		var next [][]byte
		for i := 0; i < len(hashes); i += 2 {
			combined := append(hashes[i], hashes[i+1]...)
			h := sha256.Sum256(combined)
			// Double hash the combined pair
			h2 := sha256.Sum256(h[:])
			next = append(next, h2[:])
		}
		hashes = next
	}
	return hex.EncodeToString(hashes[0])
}

// VerifyMerkleProof checks a single tx is included in a block
// without needing the full block — this is how lightweight clients work
func VerifyMerkleProof(txID string, proof []string, root string) bool {
	current := sha256.Sum256([]byte(txID))
	h := current[:]
	for _, sibling := range proof {
		sibBytes, err := hex.DecodeString(sibling)
		if err != nil {
			return false
		}
		combined := append(h, sibBytes...)
		next := sha256.Sum256(combined)
		next2 := sha256.Sum256(next[:])
		h = next2[:]
	}
	return hex.EncodeToString(h) == root
}
