package coretesting

import (
	"crypto/rand"
	"testing"
)

func EncryptionKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generating encryption key: %v", err)
	}
	return key
}
