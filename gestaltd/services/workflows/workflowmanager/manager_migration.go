package workflowmanager

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
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
// provenance only.
func (m *Manager) ApplyDefinitionMigration(ctx context.Context, req DefinitionMigrationApply) (out *ManagedDefinition, err error) {
	appName := strings.TrimSpace(req.AppName)
	revisionID := strings.TrimSpace(req.RevisionID)
	if appName == "" {
		return nil, fmt.Errorf("configuring app name is required")
	}
	if revisionID == "" {
		return nil, fmt.Errorf("migration revision id is required")
	}
	if err := coreworkflow.ValidateAppManagedDefinitionID(appName, req.Spec.ID); err != nil {
		return nil, err
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
	spec, err := m.resolveMigrationDefinitionSpec(ctx, req.Spec)
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

func (m *Manager) resolveMigrationDefinitionSpec(ctx context.Context, spec coreworkflow.DefinitionSpec) (coreworkflow.DefinitionSpec, error) {
	spec.ID = strings.TrimSpace(spec.ID)
	if spec.ID == "" {
		return coreworkflow.DefinitionSpec{}, fmt.Errorf("%w: workflow definition id is required", invocation.ErrInvalidInvocation)
	}
	target, err := m.resolveMigrationTarget(spec.Target)
	if err != nil {
		return coreworkflow.DefinitionSpec{}, err
	}
	activations, err := normalizeDefinitionActivations(spec.Activations)
	if err != nil {
		return coreworkflow.DefinitionSpec{}, err
	}
	spec.Target = target
	spec.Activations = activations
	return spec, nil
}

func (m *Manager) resolveMigrationTarget(target coreworkflow.Target) (coreworkflow.Target, error) {
	return validateTargetSteps(target,
		func(_ int, app coreworkflow.AppCall) (coreworkflow.AppCall, error) {
			return m.validateStaticWorkflowStepApp(app)
		},
		func(_ int, agent coreworkflow.AgentTurn) (coreworkflow.AgentTurn, error) {
			return m.validateStaticWorkflowStepAgent(agent)
		},
	)
}

func (m *Manager) validateStaticWorkflowStepApp(target coreworkflow.AppCall) (coreworkflow.AppCall, error) {
	appName := strings.TrimSpace(target.Name)
	if appName == "" {
		return coreworkflow.AppCall{}, fmt.Errorf("%w: workflow target app is required", invocation.ErrInvalidInvocation)
	}
	operation := strings.TrimSpace(target.Operation)
	if operation == "" {
		return coreworkflow.AppCall{}, fmt.Errorf("%w: workflow target operation is required", invocation.ErrInvalidInvocation)
	}
	if m == nil || m.providers == nil {
		return coreworkflow.AppCall{}, fmt.Errorf("%w: workflow providers are not configured", invocation.ErrInternal)
	}
	prov, err := m.providers.Get(appName)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return coreworkflow.AppCall{}, fmt.Errorf("%w: %q", invocation.ErrProviderNotFound, appName)
		}
		return coreworkflow.AppCall{}, fmt.Errorf("%w: looking up provider: %v", invocation.ErrInternal, err)
	}
	cat := prov.Catalog()
	if cat == nil {
		return coreworkflow.AppCall{}, fmt.Errorf("workflow target app %q has no static catalog", appName)
	}
	if _, ok := catalog.OperationByID(cat, operation); !ok {
		return coreworkflow.AppCall{}, fmt.Errorf("workflow target app %q has no operation %q in static catalog", appName, operation)
	}
	return target, nil
}

func (m *Manager) validateStaticWorkflowStepAgent(target coreworkflow.AgentTurn) (coreworkflow.AgentTurn, error) {
	providerName := strings.TrimSpace(target.ProviderName)
	if providerName == "" {
		return coreworkflow.AgentTurn{}, fmt.Errorf("%w: workflow target agent provider is required", invocation.ErrInvalidInvocation)
	}
	if m == nil || m.agent == nil {
		return coreworkflow.AgentTurn{}, fmt.Errorf("%w: agent control is not configured", invocation.ErrInternal)
	}
	if _, _, err := m.agent.ResolveProvider(context.Background(), providerName); err != nil {
		return coreworkflow.AgentTurn{}, err
	}
	return target, nil
}
