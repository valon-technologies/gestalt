package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

type AESGCMEncryptor struct {
	gcm cipher.AEAD
}

func NewAESGCM(key []byte) (*AESGCMEncryptor, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes.NewCipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher.NewGCM: %w", err)
	}
	return &AESGCMEncryptor{gcm: gcm}, nil
}

func (e *AESGCMEncryptor) Encrypt(plaintext string) (string, error) {
	return e.encrypt(plaintext, base64.StdEncoding, nil)
}

func (e *AESGCMEncryptor) Decrypt(encoded string) (string, error) {
	return e.decrypt(encoded, base64.StdEncoding, nil)
}

func (e *AESGCMEncryptor) EncryptURLSafe(plaintext string) (string, error) {
	return e.encrypt(plaintext, base64.RawURLEncoding, nil)
}

func (e *AESGCMEncryptor) DecryptURLSafe(encoded string) (string, error) {
	return e.decrypt(encoded, base64.RawURLEncoding, nil)
}

func (e *AESGCMEncryptor) EncryptURLSafeWithAAD(plaintext string, aad []byte) (string, error) {
	return e.encrypt(plaintext, base64.RawURLEncoding, aad)
}

func (e *AESGCMEncryptor) DecryptURLSafeWithAAD(encoded string, aad []byte) (string, error) {
	return e.decrypt(encoded, base64.RawURLEncoding, aad)
}

func (e *AESGCMEncryptor) encrypt(plaintext string, enc *base64.Encoding, aad []byte) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	nonce := make([]byte, e.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generating nonce: %w", err)
	}
	ciphertext := e.gcm.Seal(nonce, nonce, []byte(plaintext), aad)
	return enc.EncodeToString(ciphertext), nil
}

func (e *AESGCMEncryptor) decrypt(encoded string, enc *base64.Encoding, aad []byte) (string, error) {
	if encoded == "" {
		return "", nil
	}
	data, err := enc.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}
	nonceSize := e.gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := e.gcm.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plaintext), nil
}
