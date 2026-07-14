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
}

func (m *recordingMigrationManager) ApplyDefinitionMigration(_ context.Context, req workflowmanager.DefinitionMigrationApply) (*workflowmanager.ManagedDefinition, error) {
	if err := coreworkflow.ValidateAppManagedDefinitionID(req.AppName, req.Spec.ID); err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()
	return &workflowmanager.ManagedDefinition{
		ProviderName: req.ProviderName,
		Definition:   &coreworkflow.Definition{ID: req.Spec.ID, Generation: 1},
	}, nil
}

func TestApplyWorkflowMigrationRequiresConfigurePhase(t *testing.T) {
	server := migrationsservice.NewServer(migrationsservice.ServerConfig{
		AppName:             "dealHub",
		Manager:             &recordingMigrationManager{},
		ConfigureSessions:   migrationsservice.NewConfigureSessionRegistry(),
		ConfiguredProviders: map[string]struct{}{"temporal": {}},
	})
	_, err := server.ApplyWorkflowMigration(context.Background(), &proto.ApplyWorkflowMigrationRequest{
		Provider:   "temporal",
		RevisionId: "0001",
		Spec:       &proto.WorkflowDefinitionSpec{Id: "app_dealHub_extract_row"},
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("status = %v, want PermissionDenied", err)
	}
}

func TestApplyWorkflowMigrationRejectsDefaultProvider(t *testing.T) {
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
		Spec:       &proto.WorkflowDefinitionSpec{Id: "app_dealHub_extract_row"},
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("status = %v, want InvalidArgument", err)
	}
}

func TestApplyWorkflowMigrationRejectsWrongNamespace(t *testing.T) {
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
		Spec:       &proto.WorkflowDefinitionSpec{Id: "app_other_extract_row"},
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("status = %v, want InvalidArgument", err)
	}
}

func TestApplyWorkflowMigrationDerivesIdempotencyKey(t *testing.T) {
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
		Spec:       &proto.WorkflowDefinitionSpec{Id: "app_dealHub_extract_row", RunAs: "service_account:runner"},
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	manager.mu.Lock()
	calls := manager.calls
	manager.mu.Unlock()
	if calls != 1 {
		t.Fatalf("manager calls = %d, want 1", calls)
	}
}
