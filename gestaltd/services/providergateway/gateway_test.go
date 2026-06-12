package providergateway

import (
	"testing"
	"time"
)

func TestNewWithCallerTokenIssuer(t *testing.T) {
	t.Parallel()

	privateKeyPEM, _ := testCallerTokenKeyPair(t)
	issuer, err := NewCallerTokenIssuer(privateKeyPEM)
	if err != nil {
		t.Fatalf("NewCallerTokenIssuer: %v", err)
	}
	gateway := New(WithCallerTokenIssuer(issuer))

	if gateway.callerTokenIssuer == nil {
		t.Fatal("callerTokenIssuer = nil")
	}
}

func TestIssueCallerToken(t *testing.T) {
	t.Parallel()

	privateKeyPEM, publicKeyPEM := testCallerTokenKeyPair(t)
	issuer, err := NewCallerTokenIssuer(privateKeyPEM)
	if err != nil {
		t.Fatalf("NewCallerTokenIssuer: %v", err)
	}
	gateway := New(WithCallerTokenIssuer(issuer))

	now := time.Now().UTC()
	token, ok, err := gateway.IssueCallerToken("user:123", now)
	if err != nil {
		t.Fatalf("IssueCallerToken: %v", err)
	}
	if !ok {
		t.Fatal("IssueCallerToken ok = false, want true")
	}
	claims, err := Verify(token, publicKeyPEM)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.SubjectID != "user:123" {
		t.Fatalf("SubjectID = %q, want user:123", claims.SubjectID)
	}
}

func TestNewCallerTokenIssuerEmptyKey(t *testing.T) {
	t.Parallel()

	issuer, err := NewCallerTokenIssuer(" ")
	if err != nil {
		t.Fatalf("NewCallerTokenIssuer: %v", err)
	}
	if issuer != nil {
		t.Fatalf("issuer = %#v, want nil", issuer)
	}
}
