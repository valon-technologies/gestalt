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
	gcm         cipher.AEAD
	fallbackGCM cipher.AEAD
}

func NewAESGCM(key []byte) (*AESGCMEncryptor, error) {
	return NewAESGCMWithFallback(key)
}

func NewAESGCMWithFallback(key []byte, fallbackKeys ...[]byte) (*AESGCMEncryptor, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	var fallbackGCM cipher.AEAD
	var fallbackKey []byte
	if len(fallbackKeys) > 0 {
		fallbackKey = fallbackKeys[0]
	}
	if len(fallbackKey) > 0 {
		fallbackGCM, err = newGCM(fallbackKey)
		if err != nil {
			return nil, err
		}
	}
	return &AESGCMEncryptor{gcm: gcm, fallbackGCM: fallbackGCM}, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes.NewCipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher.NewGCM: %w", err)
	}
	return gcm, nil
}

func (e *AESGCMEncryptor) Encrypt(plaintext string) (string, error) {
	return e.encrypt(plaintext, base64.StdEncoding)
}

func (e *AESGCMEncryptor) Decrypt(encoded string) (string, error) {
	return e.decrypt(encoded, base64.StdEncoding)
}

func (e *AESGCMEncryptor) EncryptURLSafe(plaintext string) (string, error) {
	return e.encrypt(plaintext, base64.RawURLEncoding)
}

func (e *AESGCMEncryptor) DecryptURLSafe(encoded string) (string, error) {
	return e.decrypt(encoded, base64.RawURLEncoding)
}

func (e *AESGCMEncryptor) encrypt(plaintext string, enc *base64.Encoding) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	nonce := make([]byte, e.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generating nonce: %w", err)
	}
	ciphertext := e.gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return enc.EncodeToString(ciphertext), nil
}

func (e *AESGCMEncryptor) decrypt(encoded string, enc *base64.Encoding) (string, error) {
	if encoded == "" {
		return "", nil
	}
	data, err := enc.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}
	plaintext, err := decryptWithGCM(e.gcm, data)
	if err == nil {
		return plaintext, nil
	}
	if e.fallbackGCM != nil {
		plaintext, fallbackErr := decryptWithGCM(e.fallbackGCM, data)
		if fallbackErr == nil {
			return plaintext, nil
		}
	}
	return "", err
}

func decryptWithGCM(gcm cipher.AEAD, data []byte) (string, error) {
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plaintext), nil
}
