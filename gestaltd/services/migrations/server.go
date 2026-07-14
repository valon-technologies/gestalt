package migrations

import (
	"context"
	"errors"
	"fmt"
	"strings"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/workflowwire"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// DefinitionMigrationApplier applies trusted workflow definition migrations.
type DefinitionMigrationApplier interface {
	ApplyDefinitionMigration(ctx context.Context, req workflowmanager.DefinitionMigrationApply) (*workflowmanager.ManagedDefinition, error)
}

type Server struct {
	proto.UnimplementedMigrationServer

	appName             string
	manager             DefinitionMigrationApplier
	configureSessions   *ConfigureSessionRegistry
	configuredProviders map[string]struct{}
}

type ServerConfig struct {
	AppName             string
	Manager             DefinitionMigrationApplier
	ConfigureSessions   *ConfigureSessionRegistry
	ConfiguredProviders map[string]struct{}
}

func NewServer(cfg ServerConfig) *Server {
	return &Server{
		appName:             strings.TrimSpace(cfg.AppName),
		manager:             cfg.Manager,
		configureSessions:   cfg.ConfigureSessions,
		configuredProviders: cfg.ConfiguredProviders,
	}
}

func (s *Server) ApplyWorkflowMigration(ctx context.Context, req *proto.ApplyWorkflowMigrationRequest) (*proto.ApplyWorkflowMigrationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if s == nil || s.manager == nil {
		return nil, status.Error(codes.FailedPrecondition, "workflow manager is not configured")
	}
	if !s.configureSessions.Active(s.appName) {
		return nil, status.Error(codes.PermissionDenied, "workflow migrations are only allowed during provider configure")
	}

	revisionID := strings.TrimSpace(req.GetRevisionId())
	providerName := strings.TrimSpace(req.GetProvider())
	if err := validateWorkflowMigrationProvider(providerName, s.configuredProviders); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	spec, err := workflowwire.DefinitionSpecFromProto(req.GetSpec())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "definition spec: %v", err)
	}
	if err := coreworkflow.ValidateLocalDefinitionID(spec.ID); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	namespacedID := coreworkflow.AppManagedDefinitionID(s.appName, spec.ID)
	spec.ID = namespacedID

	idempotencyKey := strings.TrimSpace(req.GetIdempotencyKey())
	if idempotencyKey == "" {
		idempotencyKey = workflowMigrationIdempotencyKey(s.appName, revisionID, providerName, namespacedID)
	}

	managed, err := s.manager.ApplyDefinitionMigration(ctx, workflowmanager.DefinitionMigrationApply{
		AppName:        s.appName,
		RevisionID:     revisionID,
		ProviderName:   providerName,
		Spec:           *spec,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return nil, workflowMigrationStatusError(err)
	}
	definition, err := managedWorkflowDefinitionToProto(managed)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	return &proto.ApplyWorkflowMigrationResponse{Definition: definition}, nil
}

func workflowMigrationIdempotencyKey(appName, revisionID, providerName, definitionID string) string {
	parts := []string{
		"workflow-migration",
		strings.TrimSpace(appName),
		strings.TrimSpace(revisionID),
		strings.TrimSpace(providerName),
		strings.TrimSpace(definitionID),
	}
	return strings.Join(parts, "/")
}

func validateWorkflowMigrationProvider(providerName string, configured map[string]struct{}) error {
	providerName = strings.TrimSpace(providerName)
	switch {
	case providerName == "":
		return fmt.Errorf("workflow migration provider is required")
	case strings.EqualFold(providerName, "default"):
		return fmt.Errorf("workflow migration provider %q is not allowed; name the workflow provider explicitly", providerName)
	}
	if configured != nil {
		if _, ok := configured[providerName]; !ok {
			return fmt.Errorf("workflow migration provider %q is not configured as a workflow provider", providerName)
		}
	}
	return nil
}

func managedWorkflowDefinitionToProto(managed *workflowmanager.ManagedDefinition) (*proto.WorkflowDefinition, error) {
	if managed == nil || managed.Definition == nil {
		return nil, fmt.Errorf("workflow definition is required")
	}
	return workflowwire.DefinitionToProto(managed.Definition)
}

func workflowMigrationStatusError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, workflowmanager.ErrWorkflowNotConfigured):
		return status.Error(codes.FailedPrecondition, err.Error())
	default:
		msg := err.Error()
		if strings.Contains(msg, "workflow definition id") || strings.Contains(msg, "must use prefix") {
			return status.Error(codes.InvalidArgument, msg)
		}
		return status.Errorf(codes.Internal, "%v", err)
	}
}
