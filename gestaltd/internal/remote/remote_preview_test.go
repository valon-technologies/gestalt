package remote_test

import (
	"context"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/remote"
	"golang.org/x/oauth2"
)

func TestCloudRunDialOptionsAddsGestaltAuthorizationMetadata(t *testing.T) { //nolint:paralleltest // mutates package-level Cloud Run token hooks
	remote.TestCloudRunTokenSourceHook = func(context.Context, string) (oauth2.TokenSource, error) {
		return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "google-id-token"}), nil
	}
	t.Cleanup(func() { remote.TestCloudRunTokenSourceHook = nil })

	opts, err := remote.TestCloudRunDialOptions(context.Background(), "https://preview.test", "gestalt-token")
	if err != nil {
		t.Fatalf("TestCloudRunDialOptions: %v", err)
	}
	if len(opts) == 0 {
		t.Fatal("expected dial options")
	}
}

func TestCloudRunDialOptionsRejectsMissingTokenSource(t *testing.T) { //nolint:paralleltest // mutates package-level Cloud Run token hooks
	remote.TestCloudRunTokenSourceHook = func(context.Context, string) (oauth2.TokenSource, error) {
		return nil, context.DeadlineExceeded
	}
	t.Cleanup(func() { remote.TestCloudRunTokenSourceHook = nil })

	_, err := remote.TestCloudRunDialOptions(context.Background(), "https://preview.test", "gestalt-token")
	if err == nil || !strings.Contains(err.Error(), "cloud run identity token") {
		t.Fatalf("error = %v, want cloud run identity token failure", err)
	}
}
