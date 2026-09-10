//go:build !jpqg_packed_cjmodel

package embedded

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestCJModelProvenance(t *testing.T) {
	sum := sha256.Sum256(CJModelGZIP)
	if got, want := hex.EncodeToString(sum[:]), "b0fcb1e82dac11d2e11710012b563f7b19ee3e92ce6a01e7de806bcaadfc012f"; got != want {
		t.Fatalf("CJ model SHA-256 = %s, want %s", got, want)
	}
}
