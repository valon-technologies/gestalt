package crypto

import (
	"encoding/hex"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonTime    = 3
	argonMemory  = 64 * 1024 // 64 MiB
	argonThreads = 4
	argonKeyLen  = 32

	rawHexKeyPrefix = "raw-hex:"
	argon2idPrefix  = "argon2id:"
)

var argonSalt = []byte("gestalt-derivekey-v1")

// DeriveKey converts an encryption key string to a 32-byte key for AES-256-GCM.
// Use raw-hex:<64 hex chars> for a raw AES-256 key and argon2id:<passphrase>
// to force Argon2id derivation. For compatibility, an unprefixed 64-character
// hex string is still decoded as a raw key; all other strings use Argon2id.
// Returns nil for an empty string or malformed explicit raw-hex key.
func DeriveKey(s string) []byte {
	if s == "" {
		return nil
	}
	if raw, ok := strings.CutPrefix(s, rawHexKeyPrefix); ok {
		return decodeRawHexKey(raw)
	}
	if passphrase, ok := strings.CutPrefix(s, argon2idPrefix); ok {
		if passphrase == "" {
			return nil
		}
		return deriveArgon2idKey(passphrase)
	}
	if b := decodeRawHexKey(s); b != nil {
		return b
	}
	return deriveArgon2idKey(s)
}

func decodeRawHexKey(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != argonKeyLen {
		return nil
	}
	return b
}

func deriveArgon2idKey(s string) []byte {
	return argon2.IDKey([]byte(s), argonSalt, argonTime, argonMemory, argonThreads, argonKeyLen)
}
