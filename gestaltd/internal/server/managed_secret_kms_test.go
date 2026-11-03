package server

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"

	"google.golang.org/api/cloudkms/v1"
)

const testKMSKey = "projects/p/locations/us-east1/keyRings/r/cryptoKeys/k"

type fakeKMS struct {
	gotKey string
	gotReq *cloudkms.EncryptRequest
	err    error
}

func (f *fakeKMS) Encrypt(_ context.Context, key string, req *cloudkms.EncryptRequest) (*cloudkms.EncryptResponse, error) {
	f.gotKey, f.gotReq = key, req
	if f.err != nil {
		return nil, f.err
	}
	return &cloudkms.EncryptResponse{Ciphertext: base64.StdEncoding.EncodeToString([]byte("sealed"))}, nil
}

func TestKMSManagedSecretCipherBindsNameAsAAD(t *testing.T) {
	t.Parallel()
	fake := &fakeKMS{}
	out, err := newKMSManagedSecretCipher(testKMSKey, fake).Encrypt(context.Background(), "my-secret", []byte("value"))
	if err != nil {
		t.Fatal(err)
	}
	if string(out.Ciphertext) != "sealed" || out.KMSKey != testKMSKey {
		t.Fatalf("unexpected ciphertext result: %+v", out)
	}
	if fake.gotKey != testKMSKey {
		t.Fatalf("KMS key = %q", fake.gotKey)
	}
	if pt, _ := base64.StdEncoding.DecodeString(fake.gotReq.Plaintext); string(pt) != "value" {
		t.Fatalf("plaintext = %q", pt)
	}
	if aad, _ := base64.StdEncoding.DecodeString(fake.gotReq.AdditionalAuthenticatedData); string(aad) != "my-secret" {
		t.Fatalf("AAD = %q, want secret name", aad)
	}
}

func TestKMSManagedSecretCipherPropagatesErrors(t *testing.T) {
	t.Parallel()
	fake := &fakeKMS{err: errors.New("denied")}
	if _, err := newKMSManagedSecretCipher(testKMSKey, fake).Encrypt(context.Background(), "n", []byte("v")); err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateManagedSecretKMSKey(t *testing.T) {
	t.Parallel()
	if err := ValidateManagedSecretKMSKey(testKMSKey); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "k", testKMSKey + "/cryptoKeyVersions/1"} {
		if err := ValidateManagedSecretKMSKey(bad); err == nil {
			t.Fatalf("expected %q to be rejected", bad)
		}
	}
}
