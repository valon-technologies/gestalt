package crypto

import (
	"crypto/sha256"
	"encoding/hex"
)

// DeriveKey converts an encryption key string to a 32-byte key for AES-256-GCM.
// If the string is 64 hex characters it is hex-decoded directly; otherwise it is
// SHA-256 hashed. Returns nil for an empty string.
func DeriveKey(s string) []byte {
	if s == "" {
		return nil
	}
	if b, err := hex.DecodeString(s); err == nil && len(b) == 32 {
		return b
	}
	h := sha256.Sum256([]byte(s))
	return h[:]
}
