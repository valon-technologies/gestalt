package server

import (
	"context"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"

	"google.golang.org/api/cloudkms/v1"
)

// ManagedSecretKMSKeyPattern matches a Cloud KMS crypto key resource name (not
// a crypto key version). This is the same value the relationaldb secrets
// provider takes as `kmsKey`, because that provider decrypts what we encrypt.
var ManagedSecretKMSKeyPattern = regexp.MustCompile(`^projects/[^/]+/locations/[^/]+/keyRings/[^/]+/cryptoKeys/[^/]+$`)

// kmsEncryptor is the slice of the Cloud KMS API the cipher needs. It exists so
// tests can fake KMS without credentials.
type kmsEncryptor interface {
	Encrypt(ctx context.Context, keyName string, req *cloudkms.EncryptRequest) (*cloudkms.EncryptResponse, error)
}

type cloudKMSEncryptor struct {
	svc *cloudkms.Service
}

func (c cloudKMSEncryptor) Encrypt(ctx context.Context, keyName string, req *cloudkms.EncryptRequest) (*cloudkms.EncryptResponse, error) {
	return c.svc.Projects.Locations.KeyRings.CryptoKeys.Encrypt(keyName, req).Context(ctx).Do()
}

// NewKMSManagedSecretCipher builds a ManagedSecretCipher backed by Cloud KMS
// using application default credentials. kmsKey is the crypto key resource
// name, e.g. projects/p/locations/l/keyRings/r/cryptoKeys/k.
//
// The contract matches the relationaldb secrets provider in gestalt-providers:
// the secret's logical name is bound as additional authenticated data and the
// stored ciphertext is the raw KMS ciphertext bytes.
func NewKMSManagedSecretCipher(ctx context.Context, kmsKey string) (ManagedSecretCipher, error) {
	kmsKey = strings.TrimSpace(kmsKey)
	if err := ValidateManagedSecretKMSKey(kmsKey); err != nil {
		return nil, err
	}
	svc, err := cloudkms.NewService(ctx)
	if err != nil {
		return nil, fmt.Errorf("create Cloud KMS client for managed secrets: %w", err)
	}
	return newKMSManagedSecretCipher(kmsKey, cloudKMSEncryptor{svc: svc}), nil
}

// ValidateManagedSecretKMSKey checks the shape of a crypto key resource name.
func ValidateManagedSecretKMSKey(kmsKey string) error {
	if !ManagedSecretKMSKeyPattern.MatchString(strings.TrimSpace(kmsKey)) {
		return fmt.Errorf("managed secrets kmsKey must be a Cloud KMS crypto key name like projects/PROJECT/locations/LOCATION/keyRings/RING/cryptoKeys/KEY")
	}
	return nil
}

func newKMSManagedSecretCipher(kmsKey string, enc kmsEncryptor) ManagedSecretCipher {
	return NewStaticManagedSecretCipher(kmsKey, "", func(ctx context.Context, name string, plaintext []byte) ([]byte, error) {
		resp, err := enc.Encrypt(ctx, kmsKey, &cloudkms.EncryptRequest{
			Plaintext:                   base64.StdEncoding.EncodeToString(plaintext),
			AdditionalAuthenticatedData: base64.StdEncoding.EncodeToString([]byte(name)),
		})
		if err != nil {
			return nil, fmt.Errorf("encrypt managed secret with Cloud KMS: %w", err)
		}
		ciphertext, err := base64.StdEncoding.DecodeString(resp.Ciphertext)
		if err != nil {
			return nil, fmt.Errorf("decode Cloud KMS ciphertext: %w", err)
		}
		if len(ciphertext) == 0 {
			return nil, fmt.Errorf("Cloud KMS returned empty ciphertext")
		}
		return ciphertext, nil
	})
}
