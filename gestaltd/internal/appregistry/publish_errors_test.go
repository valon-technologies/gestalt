package appregistry

import (
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
