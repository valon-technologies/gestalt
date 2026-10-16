package coretesting

import (
	"crypto/rand"
	"testing"
)

func EncryptionKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	return key
}
