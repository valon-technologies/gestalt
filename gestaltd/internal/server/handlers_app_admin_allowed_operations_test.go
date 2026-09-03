package server

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/apps/operationexposure"
)

type appAllowedOperationsRestartRecorder struct {
	app   string
	calls int
}

func (r *appAllowedOperationsRestartRecorder) RestartApp(_ context.Context, app string) error {
	r.app = app
	r.calls++
	return nil
}

func TestAllowedOperationSource(t *testing.T) {
	t.Parallel()

	static := map[string]*operationexposure.OperationOverride{
		"get_item": {AllowedRoles: []string{"viewer"}},
	}
	runtime := map[string]*operationexposure.OperationOverride{
		"create": {AllowedRoles: []string{"admin"}},
	}

	if got := allowedOperationSource("get_item", static, runtime); got != "config" {
		t.Fatalf("get_item source = %q, want config", got)
	}
	if got := allowedOperationSource("create", static, runtime); got != "runtime" {
		t.Fatalf("create source = %q, want runtime", got)
	}
}

func TestValidateAppAdminAllowedOperationsUpdateRejectsUnknownOperation(t *testing.T) {
	t.Parallel()

	server := &Server{}
	entry := &config.ProviderEntry{
		AllowedOperations: map[string]*config.OperationOverride{
			"get_item": {AllowedRoles: []string{"viewer"}},
		},
	}
	err := server.validateAppAdminAllowedOperationsUpdate(entry, appAdminAllowedOperationsUpdateRequest{
		Operations: map[string]*operationexposure.OperationOverride{
			"unknown": {AllowedRoles: []string{"admin"}},
		},
	})
	if err == nil {
		t.Fatal("expected validation error for unknown operation")
	}
}

func TestNormalizeRemovedOperationIDs(t *testing.T) {
	t.Parallel()

	got := normalizeRemovedOperationIDs([]string{" delete ", "delete", ""})
	if len(got) != 1 || got[0] != "delete" {
		t.Fatalf("normalizeRemovedOperationIDs = %#v", got)
	}
}

func TestRestartAppProviderForAllowedOperationsUsesAppRestarter(t *testing.T) {
	t.Parallel()

	restarter := &appAllowedOperationsRestartRecorder{}
	server := &Server{appProviderRestarter: restarter}
	if err := server.restartAppProviderForAllowedOperations(context.Background(), "toolshed"); err != nil {
		t.Fatalf("restartAppProviderForAllowedOperations: %v", err)
	}
	if restarter.calls != 1 || restarter.app != "toolshed" {
		t.Fatalf("restart calls = %d app = %q, want one toolshed restart", restarter.calls, restarter.app)
	}
}
