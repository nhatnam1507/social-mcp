// Package ids generates short random identifiers with a type prefix.
package ids

import (
	"crypto/rand"
	"encoding/hex"
)

// New returns prefix + "_" + 12 random hex characters, e.g. "acc_3f9a0c1b2d4e".
func New(prefix string) string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return prefix + "_" + hex.EncodeToString(b)
}
