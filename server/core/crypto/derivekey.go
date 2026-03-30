package crypto

import (
	"encoding/hex"

	"golang.org/x/crypto/argon2"
)

const (
	argonTime    = 3
	argonMemory  = 64 * 1024 // 64 MiB
	argonThreads = 4
	argonKeyLen  = 32
)

var argonSalt = []byte("gestalt-derivekey-v1")

// DeriveKey converts an encryption key string to a 32-byte key for AES-256-GCM.
// If the string is 64 hex characters it is hex-decoded directly; otherwise it is
// derived using Argon2id. Returns nil for an empty string.
func DeriveKey(s string) []byte {
	if s == "" {
		return nil
	}
	if b, err := hex.DecodeString(s); err == nil && len(b) == 32 {
		return b
	}
	return argon2.IDKey([]byte(s), argonSalt, argonTime, argonMemory, argonThreads, argonKeyLen)
}
