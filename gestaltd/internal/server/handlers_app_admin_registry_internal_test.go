package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/coredata"
)

func TestRegistryInstallPausedReturnsLocked(t *testing.T) {
	t.Parallel()
	for _, err := range []error{coredata.ErrAppDeployPaused, fmt.Errorf("admission: %w", coredata.ErrAppDeployPaused)} {
		response := httptest.NewRecorder()
		writeAppAdminRegistryInstallError(response, err)
		if response.Code != http.StatusLocked {
			t.Fatalf("status = %d, want 423", response.Code)
		}
	}
}
