package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/coredata"
)

func TestResolveRevisionActorLabelPreservesSystemActor(t *testing.T) {
	t.Parallel()

	server := &Server{}
	if got := server.resolveSubjectDisplayLabel(context.Background(), "system:auto-deploy"); got != "system:auto-deploy" {
		t.Fatalf("system actor label = %q, want system:auto-deploy", got)
	}
	if got := server.resolveRevisionActorLabel(context.Background(), "system:auto-deploy"); got != "system:auto-deploy" {
		t.Fatalf("revision wrapper label = %q, want system:auto-deploy", got)
	}
}

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
