package providergateway

import (
	"context"
	"testing"
	"time"
)

func TestWithInvocationCallerToken(t *testing.T) {
	t.Parallel()

	privateKeyPEM, publicKeyPEM := testCallerTokenKeyPair(t)
	issuer, err := NewCallerTokenIssuer(privateKeyPEM)
	if err != nil {
		t.Fatalf("NewCallerTokenIssuer: %v", err)
	}
	transport := NewProviderGatewayTransport()
	transport.SetCallerTokenIssuer(issuer)
	transport.SetCallerTokenPublicKey(publicKeyPEM)

	ctx, err := transport.WithInvocationCallerToken(context.Background(), "user:alice")
	if err != nil {
		t.Fatalf("WithInvocationCallerToken: %v", err)
	}
	token := CallerTokenFromContext(ctx)
	if token == "" {
		t.Fatal("expected caller token in context")
	}
	claims, err := Verify(token, publicKeyPEM)
	if err != nil {
		t.Fatalf("Verify caller token: %v", err)
	}
	if claims.SubjectID != "user:alice" {
		t.Fatalf("SubjectID = %q, want user:alice", claims.SubjectID)
	}
	if time.Until(time.Unix(claims.ExpiresAt, 0)) > callerTokenTTL {
		t.Fatalf("caller token lifetime exceeds maximum")
	}
}

func TestWithInvocationCallerToken_NoIssuer(t *testing.T) {
	t.Parallel()

	transport := NewProviderGatewayTransport()
	ctx, err := transport.WithInvocationCallerToken(context.Background(), "user:alice")
	if err != nil {
		t.Fatalf("WithInvocationCallerToken: %v", err)
	}
	if CallerTokenFromContext(ctx) != "" {
		t.Fatal("expected empty caller token without issuer")
	}
}
