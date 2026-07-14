package workflowmanager

import (
	"context"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/workflowwire"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

// DefinitionMigrationApply carries trusted bootstrap workflow migration input.
type DefinitionMigrationApply struct {
	AppName        string
	RevisionID     string
	ProviderName   string
	Spec           coreworkflow.DefinitionSpec
	IdempotencyKey string
}

// ApplyDefinitionMigration applies an app-owned workflow definition during trusted
// provider configure. Authorization uses system:gestaltd; the app name is audit
// provenance only. Semantic validation matches the public definition path.
func (m *Manager) ApplyDefinitionMigration(ctx context.Context, req DefinitionMigrationApply) (out *ManagedDefinition, err error) {
	appName := strings.TrimSpace(req.AppName)
	revisionID := strings.TrimSpace(req.RevisionID)
	if appName == "" {
		return nil, fmt.Errorf("configuring app name is required")
	}
	if revisionID == "" {
		return nil, fmt.Errorf("migration revision id is required")
	}

	p := principal.Canonicalized(&principal.Principal{SubjectID: core.GestaltdSubjectID})
	caller := invocation.CallerProvider{Kind: invocation.ProviderKindApp, Name: appName}
	ctx = withWorkflowCaller(ctx, caller)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationDefinitionApply)
	audit.setCallerApp(appName)
	defer func() {
		if out != nil && out.Definition != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetDefinition, out.Definition.ID, "")
			audit.setWorkflowTarget(out.Definition.Target)
		}
		audit.finish(ctx, err)
	}()

	providerName, provider, err := m.resolveProvider(ctx, strings.TrimSpace(req.ProviderName))
	if err != nil {
		return nil, err
	}
	spec, err := m.validateAndNormalizeDefinitionSpec(ctx, req.Spec)
	if err != nil {
		return nil, err
	}
	audit.setProvider(providerName)
	audit.setWorkflowTarget(spec.Target)

	specProto, err := workflowwire.DefinitionSpecToProto(&spec)
	if err != nil {
		return nil, err
	}
	reqContext, err := workflowProviderRequestContext(ctx, p, caller)
	if err != nil {
		return nil, err
	}
	definitionProto, err := provider.ApplyDefinition(ctx, &proto.ApplyWorkflowProviderDefinitionRequest{
		Provider:       providerName,
		Spec:           specProto,
		IdempotencyKey: strings.TrimSpace(req.IdempotencyKey),
		Context:        reqContext,
	})
	if err != nil {
		return nil, err
	}
	definition, err := workflowwire.DefinitionFromProto(definitionProto)
	if err != nil {
		return nil, err
	}
	return &ManagedDefinition{ProviderName: providerName, Definition: definition, provider: provider}, nil
}
