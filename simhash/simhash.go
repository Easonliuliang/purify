package simhash

import (
	"hash/fnv"
	"math/bits"
	"strings"
)

// Fingerprint computes a 64-bit SimHash of the given text.
// Uses FNV-64a hash on word-level tokens with bit vector accumulation.
func Fingerprint(text string) uint64 {
	words := strings.Fields(text)
	if len(words) == 0 {
		return 0
	}

	var vector [64]int

	for _, word := range words {
		h := fnv.New64a()
		h.Write([]byte(word))
		hash := h.Sum64()

		for i := 0; i < 64; i++ {
			if hash&(1<<uint(i)) != 0 {
				vector[i]++
			} else {
				vector[i]--
			}
		}
	}

	var fingerprint uint64
	for i := 0; i < 64; i++ {
		if vector[i] > 0 {
			fingerprint |= 1 << uint(i)
		}
	}

	return fingerprint
}

// Distance returns the Hamming distance between two SimHash fingerprints.
func Distance(a, b uint64) int {
	return bits.OnesCount64(a ^ b)
}

// Similar returns true if the Hamming distance between two fingerprints
// is less than or equal to the threshold.
func Similar(a, b uint64, threshold int) bool {
	return Distance(a, b) <= threshold
}
