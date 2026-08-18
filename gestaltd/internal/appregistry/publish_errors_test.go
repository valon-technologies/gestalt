package appregistry

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestPublishHTTPStatusDeclarationInvalidIs400(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("%w: artifact %q size must be greater than zero", ErrPublishDeclarationInvalid, "linux/amd64")
	if status := PublishHTTPStatus(err); status != http.StatusBadRequest {
		t.Fatalf("PublishHTTPStatus = %d, want 400", status)
	}
}

func TestPublishHTTPStatusRegistryNotEnrolledIs404(t *testing.T) {
	t.Parallel()

	if status := PublishHTTPStatus(ErrPublishRegistryNotEnrolled); status != http.StatusNotFound {
		t.Fatalf("PublishHTTPStatus = %d, want 404", status)
	}
}

func TestPublishHTTPStatus(t *testing.T) {
	t.Parallel()

	backendFailure := errors.New("gcs read failed")
	wrappedBackendFailure := fmt.Errorf("load index: %w", backendFailure)

	tests := []struct {
		name   string
		err    error
		status int
	}{
		{name: "registry entry conflict exact", err: ErrRegistryEntryConflict, status: http.StatusConflict},
		{name: "registry entry conflict wrapped", err: fmt.Errorf("gs://bucket/entry.json: %w; guidance", ErrRegistryEntryConflict), status: http.StatusConflict},
		{name: "index version conflict exact", err: ErrIndexVersionConflict, status: http.StatusConflict},
		{name: "index version conflict wrapped", err: fmt.Errorf("app %q version %q index identity mismatch: %w", "demo", "1.0.0", ErrIndexVersionConflict), status: http.StatusConflict},
		{name: "object precondition exact", err: ErrObjectPreconditionFailed, status: http.StatusConflict},
		{name: "object precondition wrapped", err: fmt.Errorf("update gs://bucket/index.json: %w", ErrObjectPreconditionFailed), status: http.StatusConflict},
		{name: "immutable artifact conflict wrapped", err: fmt.Errorf("gs://bucket/artifact.tgz: %w; guidance", ErrObjectPreconditionFailed), status: http.StatusConflict},
		{name: "exhausted catalog conflict", err: fmt.Errorf("update gs://bucket/index.json: %w", ErrObjectPreconditionFailed), status: http.StatusConflict},
		{name: "identity mismatch", err: ErrPublishIdentityMismatch, status: http.StatusConflict},
		{name: "version conflict", err: ErrPublishVersionConflict, status: http.StatusConflict},
		{name: "backend failure", err: backendFailure, status: http.StatusBadGateway},
		{name: "wrapped backend failure", err: wrappedBackendFailure, status: http.StatusBadGateway},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if status := PublishHTTPStatus(tc.err); status != tc.status {
				t.Fatalf("PublishHTTPStatus(%v) = %d, want %d", tc.err, status, tc.status)
			}
		})
	}
}
