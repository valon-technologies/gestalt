package appregistry

import (
	"context"
	"strings"
	"testing"

	"cloud.google.com/go/storage"
)

func TestGCSUploadSignerCheckSigningReadinessUsesSignCreateUpload(t *testing.T) {
	t.Parallel()

	called := false
	signer := &GCSUploadSigner{
		newClient: func(context.Context) (*storage.Client, error) {
			return &storage.Client{}, nil
		},
		signURL: func(_ *storage.Client, _, object string, _ *storage.SignedURLOptions) (string, error) {
			called = true
			if !strings.Contains(object, ".gestaltd-signing-readiness-probe/") {
				t.Fatalf("probe object = %q", object)
			}
			return "https://storage.googleapis.com/gestalt-app-registry/probe", nil
		},
	}
	if err := signer.CheckSigningReadiness(context.Background(), "gs://gestalt-app-registry"); err != nil {
		t.Fatalf("CheckSigningReadiness: %v", err)
	}
	if !called {
		t.Fatal("expected signing probe")
	}
}
