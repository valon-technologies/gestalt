package server

import (
	"context"
	"fmt"
	"strings"
)

// ManagedSecretCipher encrypts application secret values. Deployment wiring
// owns this implementation because KMS credentials and key selection are
// environment-specific. The interface keeps
// the HTTP/admin layer independent of Cloud KMS while preserving the exact
// contract used by the relationaldb runtime provider: ciphertext is bound to
// the logical secret name with additional authenticated data.
type ManagedSecretCipher interface {
	Encrypt(ctx context.Context, name string, plaintext []byte) (ManagedSecretCiphertext, error)
}

// ManagedSecretCiphertext is the encrypted form of one secret value. KMSKey
// and KMSKeyVersion are non-secret metadata; Ciphertext must never be logged.
type ManagedSecretCiphertext struct {
	Ciphertext    []byte
	KMSKey        string
	KMSKeyVersion string
}

type staticManagedSecretCipher struct {
	key        string
	version    string
	encryptRow func(ctx context.Context, name string, plaintext []byte) ([]byte, error)
}

// NewStaticManagedSecretCipher adapts an environment-specific encrypt callback
// into ManagedSecretCipher. The KMS key resource name is non-secret and is
// recorded with the resulting version.
func NewStaticManagedSecretCipher(kmsKey, kmsKeyVersion string, encrypt func(ctx context.Context, name string, plaintext []byte) ([]byte, error)) ManagedSecretCipher {
	return staticManagedSecretCipher{
		key:        strings.TrimSpace(kmsKey),
		version:    strings.TrimSpace(kmsKeyVersion),
		encryptRow: encrypt,
	}
}

func (c staticManagedSecretCipher) Encrypt(ctx context.Context, name string, plaintext []byte) (ManagedSecretCiphertext, error) {
	if c.encryptRow == nil {
		return ManagedSecretCiphertext{}, fmt.Errorf("managed secret encryption is not configured")
	}
	if strings.TrimSpace(c.key) == "" {
		return ManagedSecretCiphertext{}, fmt.Errorf("managed secret KMS key is not configured")
	}
	ciphertext, err := c.encryptRow(ctx, name, plaintext)
	if err != nil {
		return ManagedSecretCiphertext{}, err
	}
	return ManagedSecretCiphertext{
		Ciphertext:    ciphertext,
		KMSKey:        c.key,
		KMSKeyVersion: c.version,
	}, nil
}
