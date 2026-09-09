package server

import (
	"testing"
	"time"
)

func TestIntegrationOAuthStateRoundTripsExplicitCredentialID(t *testing.T) {
	t.Parallel()

	codec, err := newIntegrationOAuthStateCodec([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	want := integrationOAuthState{
		SubjectID:    "user:1",
		Integration:  "slack",
		Instance:     "workspace",
		CredentialID: "credential-123",
		ExpiresAt:    time.Now().Add(time.Minute).Unix(),
	}

	encoded, err := codec.Encode(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := codec.Decode(encoded, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got.CredentialID != want.CredentialID {
		t.Fatalf("credential ID = %q, want %q", got.CredentialID, want.CredentialID)
	}
}
