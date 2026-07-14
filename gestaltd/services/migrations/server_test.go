package migrations_test

import (
	"context"
	"sync"
	"testing"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	migrationsservice "github.com/valon-technologies/gestalt/server/services/migrations"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type recordingMigrationManager struct {
	mu    sync.Mutex
	calls int
	last  workflowmanager.DefinitionMigrationApply
}

func (m *recordingMigrationManager) ApplyDefinitionMigration(_ context.Context, req workflowmanager.DefinitionMigrationApply) (*workflowmanager.ManagedDefinition, error) {
	m.mu.Lock()
	m.calls++
	m.last = req
	m.mu.Unlock()
	parsedApp, _, err := coreworkflow.ParseAppManagedDefinitionID(req.Spec.ID)
	if err != nil || parsedApp != req.AppName {
		return nil, status.Error(codes.InvalidArgument, "expected namespaced definition id")
	}
	return &workflowmanager.ManagedDefinition{
		ProviderName: req.ProviderName,
		Definition:   &coreworkflow.Definition{ID: req.Spec.ID, Generation: 1},
	}, nil
}

func TestApplyWorkflowMigrationRequiresConfigurePhase(t *testing.T) {
	t.Parallel()
	server := migrationsservice.NewServer(migrationsservice.ServerConfig{
		AppName:             "dealHub",
		Manager:             &recordingMigrationManager{},
		ConfigureSessions:   migrationsservice.NewConfigureSessionRegistry(),
		ConfiguredProviders: map[string]struct{}{"temporal": {}},
	})
	_, err := server.ApplyWorkflowMigration(context.Background(), &proto.ApplyWorkflowMigrationRequest{
		Provider:   "temporal",
		RevisionId: "0001",
		Spec:       &proto.WorkflowDefinitionSpec{Id: "extract_row"},
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("status = %v, want PermissionDenied", err)
	}
}

func TestApplyWorkflowMigrationRejectsDefaultProvider(t *testing.T) {
	t.Parallel()
	sessions := migrationsservice.NewConfigureSessionRegistry()
	sessions.Begin("dealHub")
	defer sessions.End("dealHub")
	server := migrationsservice.NewServer(migrationsservice.ServerConfig{
		AppName:             "dealHub",
		Manager:             &recordingMigrationManager{},
		ConfigureSessions:   sessions,
		ConfiguredProviders: map[string]struct{}{"temporal": {}},
	})
	_, err := server.ApplyWorkflowMigration(context.Background(), &proto.ApplyWorkflowMigrationRequest{
		Provider:   "default",
		RevisionId: "0001",
		Spec:       &proto.WorkflowDefinitionSpec{Id: "extract_row"},
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("status = %v, want InvalidArgument", err)
	}
}

func TestApplyWorkflowMigrationRejectsReservedDefinitionID(t *testing.T) {
	t.Parallel()
	sessions := migrationsservice.NewConfigureSessionRegistry()
	sessions.Begin("dealHub")
	defer sessions.End("dealHub")
	server := migrationsservice.NewServer(migrationsservice.ServerConfig{
		AppName:             "dealHub",
		Manager:             &recordingMigrationManager{},
		ConfigureSessions:   sessions,
		ConfiguredProviders: map[string]struct{}{"temporal": {}},
	})
	_, err := server.ApplyWorkflowMigration(context.Background(), &proto.ApplyWorkflowMigrationRequest{
		Provider:   "temporal",
		RevisionId: "0001",
		Spec:       &proto.WorkflowDefinitionSpec{Id: "app_dealHub_extract_row"},
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("status = %v, want InvalidArgument", err)
	}
}

func TestApplyWorkflowMigrationNamespacesLocalDefinitionID(t *testing.T) {
	t.Parallel()
	sessions := migrationsservice.NewConfigureSessionRegistry()
	sessions.Begin("dealHub")
	defer sessions.End("dealHub")
	manager := &recordingMigrationManager{}
	server := migrationsservice.NewServer(migrationsservice.ServerConfig{
		AppName:             "dealHub",
		Manager:             manager,
		ConfigureSessions:   sessions,
		ConfiguredProviders: map[string]struct{}{"temporal": {}},
	})
	_, err := server.ApplyWorkflowMigration(context.Background(), &proto.ApplyWorkflowMigrationRequest{
		Provider:   "temporal",
		RevisionId: "0001",
		Spec:       &proto.WorkflowDefinitionSpec{Id: "extract_row", RunAs: "service_account:runner"},
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	manager.mu.Lock()
	calls := manager.calls
	lastID := manager.last.Spec.ID
	manager.mu.Unlock()
	if calls != 1 {
		t.Fatalf("manager calls = %d, want 1", calls)
	}
	want := coreworkflow.AppManagedDefinitionID("dealHub", "extract_row")
	if lastID != want {
		t.Fatalf("namespaced id = %q, want %q", lastID, want)
	}
}
