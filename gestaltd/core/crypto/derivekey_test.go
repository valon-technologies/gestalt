package crypto_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/valon-technologies/gestalt/server/core/crypto"
)

func TestDeriveKeyPassphraseEncryptRoundTrip(t *testing.T) {
	t.Parallel()
	key := crypto.DeriveKey("dummy-passphrase-for-testing")
	enc, err := crypto.NewAESGCM(key)
	if err != nil {
		t.Fatalf("NewAESGCM: %v", err)
	}

	plaintext := "sensitive-token-value"
	ciphertext, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	got, err := enc.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != plaintext {
		t.Fatalf("round-trip failed: got %q, want %q", got, plaintext)
	}
}

func TestDeriveKeyHexKeyEncryptRoundTrip(t *testing.T) {
	t.Parallel()
	hexKey := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	key := crypto.DeriveKey(hexKey)
	want, _ := hex.DecodeString(hexKey)
	if !bytes.Equal(key, want) {
		t.Fatalf("hex key not passed through: got %x, want %x", key, want)
	}

	enc, err := crypto.NewAESGCM(key)
	if err != nil {
		t.Fatalf("NewAESGCM: %v", err)
	}

	plaintext := "sensitive-token-value"
	ciphertext, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	got, err := enc.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != plaintext {
		t.Fatalf("round-trip failed: got %q, want %q", got, plaintext)
	}
}

func TestDeriveKeyExplicitRawHexKey(t *testing.T) {
	t.Parallel()
	hexKey := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	key := crypto.DeriveKey("raw-hex:" + hexKey)
	want, _ := hex.DecodeString(hexKey)
	if !bytes.Equal(key, want) {
		t.Fatalf("explicit raw hex key not passed through: got %x, want %x", key, want)
	}
}

func TestDeriveKeyExplicitArgon2idForHexShapedPassphrase(t *testing.T) {
	t.Parallel()
	passphrase := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	key := crypto.DeriveKey("argon2id:" + passphrase)
	raw, _ := hex.DecodeString(passphrase)
	if bytes.Equal(key, raw) {
		t.Fatal("argon2id-prefixed hex-shaped passphrase bypassed KDF")
	}
	if len(key) != 32 {
		t.Fatalf("key length = %d, want 32", len(key))
	}
}

func TestDeriveKeyMalformedExplicitRawHexReturnsNil(t *testing.T) {
	t.Parallel()
	if key := crypto.DeriveKey("raw-hex:not-a-32-byte-key"); key != nil {
		t.Fatalf("key = %x, want nil", key)
	}
}

func TestDeriveKeyEmptyExplicitArgon2idReturnsNil(t *testing.T) {
	t.Parallel()
	if key := crypto.DeriveKey("argon2id:"); key != nil {
		t.Fatalf("key = %x, want nil", key)
	}
}

func TestDeriveKeySamePassphraseProducesSameEncryption(t *testing.T) {
	t.Parallel()
	passphrase := "deterministic-dummy-phrase"
	key1 := crypto.DeriveKey(passphrase)
	key2 := crypto.DeriveKey(passphrase)

	enc1, err := crypto.NewAESGCM(key1)
	if err != nil {
		t.Fatalf("NewAESGCM key1: %v", err)
	}
	enc2, err := crypto.NewAESGCM(key2)
	if err != nil {
		t.Fatalf("NewAESGCM key2: %v", err)
	}

	plaintext := "cross-instance-token"
	ciphertext, err := enc1.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	got, err := enc2.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt with second key failed: %v", err)
	}
	if got != plaintext {
		t.Fatalf("cross-key decrypt failed: got %q, want %q", got, plaintext)
	}
}

func TestDeriveKeySHA256KeyCannotDecryptArgon2idCiphertext(t *testing.T) {
	t.Parallel()
	passphrase := "dummy-phrase-kdf-mismatch"

	key := crypto.DeriveKey(passphrase)
	enc, err := crypto.NewAESGCM(key)
	if err != nil {
		t.Fatalf("NewAESGCM: %v", err)
	}
	ciphertext, err := enc.Encrypt("secret-data")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	sha := sha256.Sum256([]byte(passphrase))
	shaEnc, err := crypto.NewAESGCM(sha[:])
	if err != nil {
		t.Fatalf("NewAESGCM (sha256): %v", err)
	}
	_, err = shaEnc.Decrypt(ciphertext)
	if err == nil {
		t.Fatal("SHA-256 derived key decrypted Argon2id ciphertext; KDF is not Argon2id")
	}
}
