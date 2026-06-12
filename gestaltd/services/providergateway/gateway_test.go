package providergateway

import "testing"

func TestNewWithCallerTokenIssuer(t *testing.T) {
	t.Parallel()

	issuer := NewCallerTokenIssuer(" private-key-pem ")
	gateway := New(WithCallerTokenIssuer(issuer))

	if gateway.callerTokenIssuer == nil {
		t.Fatal("callerTokenIssuer = nil")
	}
	if got := gateway.callerTokenIssuer.privateKeyForTesting(); got != "private-key-pem" {
		t.Fatalf("private key = %q, want private-key-pem", got)
	}
}

func TestWithCallerTokenPrivateKeyBuildsIssuer(t *testing.T) {
	t.Parallel()

	gateway := New(WithCallerTokenPrivateKey(" private-key-pem "))

	if gateway.callerTokenIssuer == nil {
		t.Fatal("callerTokenIssuer = nil")
	}
	if got := gateway.callerTokenIssuer.privateKeyForTesting(); got != "private-key-pem" {
		t.Fatalf("private key = %q, want private-key-pem", got)
	}
}

func TestNewCallerTokenIssuerEmptyKey(t *testing.T) {
	t.Parallel()

	if issuer := NewCallerTokenIssuer(" "); issuer != nil {
		t.Fatalf("issuer = %#v, want nil", issuer)
	}
}
