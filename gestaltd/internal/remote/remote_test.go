package remote_test

import (
	"context"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/remote"
)

func TestDialValidation(t *testing.T) {
	t.Parallel()

	_, err := remote.Dial(context.Background(), remote.Config{})
	if err == nil || !strings.Contains(err.Error(), "URL is required") {
		t.Fatalf("Dial(empty URL) = %v, want URL required", err)
	}

	_, err = remote.Dial(context.Background(), remote.Config{URL: "ftp://valon.tools"})
	if err == nil || !strings.Contains(err.Error(), "unsupported URL scheme") {
		t.Fatalf("Dial(ftp URL) = %v, want unsupported scheme", err)
	}

	_, err = remote.NewClientSet(context.Background(), remote.Config{URL: "https://valon.tools"})
	if err == nil || !strings.Contains(err.Error(), "token is required") {
		t.Fatalf("NewClientSet(empty token) = %v, want token required", err)
	}
}
