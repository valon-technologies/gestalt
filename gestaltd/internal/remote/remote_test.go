package remote

import (
	"context"
	"strings"
	"testing"
)

func TestNewClientSetRequiresURLAndToken(t *testing.T) {
	t.Parallel()

	_, err := NewClientSet(context.Background(), Config{Token: "token"})
	if err == nil || !strings.Contains(err.Error(), "URL is required") {
		t.Fatalf("NewClientSet(empty URL) = %v, want URL required", err)
	}

	_, err = NewClientSet(context.Background(), Config{URL: "https://valon.tools"})
	if err == nil || !strings.Contains(err.Error(), "token is required") {
		t.Fatalf("NewClientSet(empty token) = %v, want token required", err)
	}
}

func TestGRPCUnsupportedScheme(t *testing.T) {
	t.Parallel()

	_, _, err := grpcTarget("ftp://valon.tools", nil)
	if err == nil || !strings.Contains(err.Error(), "unsupported URL scheme") {
		t.Fatalf("grpcTarget(ftp) = %v, want unsupported scheme", err)
	}
}
