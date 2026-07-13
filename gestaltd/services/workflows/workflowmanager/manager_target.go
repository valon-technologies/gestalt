package workflowmanager

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (m *Manager) resolveProvider(ctx context.Context, providerName string) (string, coreworkflow.Provider, error) {
	if m == nil || m.workflow == nil {
		return "", nil, ErrWorkflowNotConfigured
	}
	return m.workflow.ResolveProvider(ctx, strings.TrimSpace(providerName))
}

func (m *Manager) resolveRequestProviderTarget(ctx context.Context, p *principal.Principal, providerSelection string, definitionID string, caller invocation.CallerProvider, operation string) (string, coreworkflow.Provider, coreworkflow.Target, int64, error) {
	definitionID = strings.TrimSpace(definitionID)
	if definitionID == "" {
		return "", nil, coreworkflow.Target{}, 0, fmt.Errorf("%w: workflow definition_id is required", invocation.ErrInvalidInvocation)
	}
	ctx = withWorkflowCaller(ctx, caller)
	definition, err := m.requireOwnedDefinition(ctx, p, definitionID, providerSelection)
	if err != nil {
		return "", nil, coreworkflow.Target{}, 0, err
	}
	if definition == nil || definition.Definition == nil {
		return "", nil, coreworkflow.Target{}, 0, core.ErrNotFound
	}
	// The caller must still be authorized for every referenced app operation and
	// agent tool. This is an independent prerequisite; the provider worker then
	// executes the step as the definition's stored run_as subject.
	if !m.allowStoredTarget(ctx, p, definition.Definition.Target) {
		return "", nil, coreworkflow.Target{}, 0, invocation.ErrAuthorizationDenied
	}
	return definition.ProviderName, definition.provider, definition.Definition.Target, definition.Definition.Generation, nil
}

func (m *Manager) resolveTarget(ctx context.Context, p *principal.Principal, target coreworkflow.Target) (coreworkflow.Target, error) {
	if len(target.Steps) == 0 {
		return coreworkflow.Target{}, fmt.Errorf("workflow target.steps is required")
	}
	out := coreworkflow.Target{Steps: make([]coreworkflow.Step, 0, len(target.Steps))}
	seen := map[string]struct{}{}
	for i := range target.Steps {
		step := target.Steps[i]
		step.ID = strings.TrimSpace(step.ID)
		if step.ID == "" {
			return coreworkflow.Target{}, fmt.Errorf("workflow target.steps[%d].id is required", i)
		}
		if _, exists := seen[step.ID]; exists {
			return coreworkflow.Target{}, fmt.Errorf("workflow target.steps[%d].id duplicates %q", i, step.ID)
		}
		if step.TimeoutSeconds < 0 {
			return coreworkflow.Target{}, fmt.Errorf("workflow target.steps[%d].timeout_seconds must not be negative", i)
		}
		if err := coreworkflow.ValidateValueMapRefs(fmt.Sprintf("workflow target.steps[%d].inputs", i), step.Inputs, seen); err != nil {
			return coreworkflow.Target{}, err
		}
		switch {
		case step.App != nil && step.Agent != nil:
			return coreworkflow.Target{}, fmt.Errorf("workflow target.steps[%d] must set exactly one of app or agent", i)
		case step.App != nil:
			app, err := m.resolveWorkflowStepApp(ctx, p, *step.App)
			if err != nil {
				return coreworkflow.Target{}, fmt.Errorf("workflow target.steps[%d].app: %w", i, err)
			}
			if err := coreworkflow.ValidateValueRefs(fmt.Sprintf("workflow target.steps[%d].app.input", i), app.Input, seen); err != nil {
				return coreworkflow.Target{}, err
			}
			step.App = &app
		case step.Agent != nil:
			agent, err := m.resolveWorkflowStepAgent(ctx, p, *step.Agent)
			if err != nil {
				return coreworkflow.Target{}, fmt.Errorf("workflow target.steps[%d].agent: %w", i, err)
			}
			step.Agent = &agent
		default:
			return coreworkflow.Target{}, fmt.Errorf("workflow target.steps[%d] must set app or agent", i)
		}
		if step.When != nil {
			if err := coreworkflow.ValidateStepWhen(fmt.Sprintf("workflow target.steps[%d].when", i), step.When, seen); err != nil {
				return coreworkflow.Target{}, err
			}
		}
		seen[step.ID] = struct{}{}
		out.Steps = append(out.Steps, step)
	}
	return out, nil
}

func (m *Manager) resolveWorkflowStepApp(ctx context.Context, p *principal.Principal, target coreworkflow.AppCall) (coreworkflow.AppCall, error) {
	appName := strings.TrimSpace(target.Name)
	if appName == "" {
		return coreworkflow.AppCall{}, fmt.Errorf("%w: workflow target app is required", invocation.ErrInvalidInvocation)
	}
	operation := strings.TrimSpace(target.Operation)
	if operation == "" {
		return coreworkflow.AppCall{}, fmt.Errorf("%w: workflow target operation is required", invocation.ErrInvalidInvocation)
	}
	credentialMode := core.NormalizeOptionalConnectionMode(target.CredentialMode)
	switch credentialMode {
	case "", core.ConnectionModeNone, core.ConnectionModeSubject:
	default:
		return coreworkflow.AppCall{}, fmt.Errorf("%w: workflow target credential_mode %q is not supported", invocation.ErrInvalidInvocation, credentialMode)
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
	if !m.allowProvider(ctx, p, appName) || !m.allowOperation(ctx, p, appName, operation) {
		return coreworkflow.AppCall{}, invocation.ErrAuthorizationDenied
	}
	if credentialMode != "" {
		ctx = invocation.WithCredentialModeOverride(ctx, credentialMode)
	}

	connection := strings.TrimSpace(target.Connection)
	if connection != "" && !core.SafeConnectionValue(connection) {
		return coreworkflow.AppCall{}, fmt.Errorf("connection name contains invalid characters")
	}
	connection = core.ResolveConnectionAlias(connection)
	instance := strings.TrimSpace(target.Instance)
	if instance != "" && !core.SafeInstanceValue(instance) {
		return coreworkflow.AppCall{}, fmt.Errorf("instance name contains invalid characters")
	}

	ctx = invocation.WithAccessContext(ctx, m.providerAccessContext(ctx, p, appName))
	var resolver invocation.TokenResolver
	if tr, ok := m.invoker.(invocation.TokenResolver); ok {
		resolver = tr
	}
	sessionConnections := m.catalogSelectorConfig().SessionCatalogConnections(appName, connection)
	sessionInstance := instance
	opMeta, _, resolvedConnection, err := invocation.ResolveOperation(ctx, prov, appName, resolver, p, operation, sessionConnections, sessionInstance)
	if err != nil {
		return coreworkflow.AppCall{}, err
	}
	if !principal.AllowsOperationPermission(p, appName, opMeta.ID) {
		return coreworkflow.AppCall{}, fmt.Errorf("%w: %s.%s", invocation.ErrAuthorizationDenied, appName, opMeta.ID)
	}
	if connection == "" {
		connection = resolvedConnection
	}
	if resolver != nil && sessionInstance == "" {
		resolvedCtx, _, err := resolver.ResolveToken(ctx, p, appName, connection, sessionInstance)
		if err != nil {
			return coreworkflow.AppCall{}, err
		}
		cred := invocation.CredentialContextFromContext(resolvedCtx)
		if cred.Connection != "" {
			connection = cred.Connection
		}
		if cred.Instance != "" {
			sessionInstance = cred.Instance
		}
	}
	return coreworkflow.AppCall{
		Name:           appName,
		Operation:      opMeta.ID,
		Connection:     connection,
		Instance:       sessionInstance,
		CredentialMode: credentialMode,
		Input:          target.Input,
	}, nil
}

func (m *Manager) resolveWorkflowStepAgent(ctx context.Context, p *principal.Principal, target coreworkflow.AgentTurn) (coreworkflow.AgentTurn, error) {
	if m == nil || m.agent == nil || m.agentManager == nil {
		return coreworkflow.AgentTurn{}, fmt.Errorf("%w: agent workflows are not configured", invocation.ErrInternal)
	}
	providerName, _, err := m.agent.ResolveProvider(ctx, target.ProviderName)
	if err != nil {
		return coreworkflow.AgentTurn{}, err
	}
	target.ProviderName = strings.TrimSpace(providerName)
	target.Prompt.Template = strings.TrimSpace(target.Prompt.Template)
	for i := range target.Messages {
		target.Messages[i].Role = strings.TrimSpace(target.Messages[i].Role)
		target.Messages[i].Text.Template = strings.TrimSpace(target.Messages[i].Text.Template)
	}
	if target.Prompt.Template == "" && len(target.Messages) == 0 {
		return coreworkflow.AgentTurn{}, fmt.Errorf("%w: workflow target agent prompt or messages is required", invocation.ErrInvalidInvocation)
	}
	if !m.allowProvider(ctx, p, target.ProviderName) || !principal.AllowsProviderPermission(p, target.ProviderName) {
		return coreworkflow.AgentTurn{}, fmt.Errorf("%w: %s", invocation.ErrAuthorizationDenied, target.ProviderName)
	}
	target.Model = strings.TrimSpace(target.Model)
	target.SessionKey = strings.TrimSpace(target.SessionKey)
	target.ToolRefs = append([]coreagent.ToolRef(nil), target.ToolRefs...)
	if err := validateWorkflowAgentToolRefs(target.ToolRefs); err != nil {
		return coreworkflow.AgentTurn{}, err
	}
	if err := validateWorkflowAgentOutput(target.Output); err != nil {
		return coreworkflow.AgentTurn{}, err
	}
	if target.Output.Structured != nil {
		target.Output.Structured.Schema = maps.Clone(target.Output.Structured.Schema)
	}
	target.ModelOptions = maps.Clone(target.ModelOptions)
	return target, nil
}

func validateWorkflowAgentOutput(output coreagent.Output) error {
	textSet := output.Text != nil
	structuredSet := output.Structured != nil
	if textSet == structuredSet {
		return fmt.Errorf("%w: exactly one of workflow target agent output.text or output.structured is required", invocation.ErrInvalidInvocation)
	}
	if output.Structured == nil {
		return nil
	}
	schema := output.Structured.Schema
	if len(schema) == 0 {
		return fmt.Errorf("%w: workflow target agent output.structured.schema must be a non-empty JSON schema object with type %q", invocation.ErrInvalidInvocation, "object")
	}
	rawType, ok := schema["type"]
	if !ok {
		return fmt.Errorf("%w: workflow target agent output.structured.schema.type must be %q", invocation.ErrInvalidInvocation, "object")
	}
	typeValue, ok := rawType.(string)
	if !ok || strings.TrimSpace(typeValue) != "object" {
		return fmt.Errorf("%w: workflow target agent output.structured.schema.type must be %q", invocation.ErrInvalidInvocation, "object")
	}
	return nil
}

func isWorkflowProviderNotFound(err error) bool {
	return errors.Is(err, core.ErrNotFound) || status.Code(err) == codes.NotFound
}

func (m *Manager) allowProvider(ctx context.Context, p *principal.Principal, provider string) bool {
	return true
}

func (m *Manager) allowOperation(ctx context.Context, p *principal.Principal, provider, operation string) bool {
	return true
}

func (m *Manager) providerAccessContext(ctx context.Context, p *principal.Principal, provider string) invocation.AccessContext {
	return invocation.AccessContext{}
}

const (
	targetAuthorizationComponentTarget        = "target"
	targetAuthorizationComponentAgentProvider = "agent_provider"
	targetAuthorizationComponentAgentToolRef  = "agent_tool_ref"
	targetAuthorizationComponentAppStep       = "app_step"

	targetAuthorizationReasonMissingAgentProvider               = "missing_agent_provider"
	targetAuthorizationReasonAuthorizerProviderDenied           = "authorizer_provider_denied"
	targetAuthorizationReasonPrincipalProviderPermissionDenied  = "principal_provider_permission_denied"
	targetAuthorizationReasonMissingToolProvider                = "missing_tool_provider"
	targetAuthorizationReasonInvalidSystemToolRef               = "invalid_system_tool_ref"
	targetAuthorizationReasonNonExactToolRefWithSystemTools     = "non_exact_tool_ref_with_system_tools"
	targetAuthorizationReasonAuthorizerOperationDenied          = "authorizer_operation_denied"
	targetAuthorizationReasonPrincipalOperationPermissionDenied = "principal_operation_permission_denied"
	targetAuthorizationReasonMissingAppStep                     = "missing_app_step"
	targetAuthorizationReasonMissingAppProvider                 = "missing_app_provider"
	targetAuthorizationReasonMissingAppOperation                = "missing_app_operation"
)

type targetAuthorizationDecision struct {
	allowed bool
	failure targetAuthorizationFailure
}

type targetAuthorizationFailure struct {
	component    string
	reason       string
	provider     string
	operation    string
	toolRefIndex int
}

type workflowTargetAuthorizationError struct {
	failure targetAuthorizationFailure
}

func (e workflowTargetAuthorizationError) Error() string { return core.ErrNotFound.Error() }
func (e workflowTargetAuthorizationError) Unwrap() error { return core.ErrNotFound }

func workflowTargetAuthorizationFailure(err error) (*targetAuthorizationFailure, bool) {
	var targetErr workflowTargetAuthorizationError
	if !errors.As(err, &targetErr) {
		return nil, false
	}
	failure := targetErr.failure
	return &failure, true
}

func (m *Manager) allowStoredTarget(ctx context.Context, p *principal.Principal, target coreworkflow.Target) bool {
	authorizedPrincipal, err := m.authorizeAgentWorkflowTarget(ctx, p, workflowManagerOperationTargetScopeOnly, target, invocation.CallerProvider{})
	if err != nil {
		return false
	}
	p = authorizedPrincipal
	return m.checkTargetAuthorization(ctx, p, target).allowed
}

func (m *Manager) checkResolvedAgentToolAuthorization(ctx context.Context, p *principal.Principal, target coreworkflow.Target) targetAuthorizationDecision {
	for stepIndex := range target.Steps {
		step := target.Steps[stepIndex]
		if step.Agent == nil {
			continue
		}
		hasSystemTools := workflowAgentToolRefsContainSystem(step.Agent.ToolRefs)
		for i := range step.Agent.ToolRefs {
			if denied := m.checkWorkflowAgentToolAuthorization(ctx, p, step.Agent.ToolRefs[i], hasSystemTools, i); !denied.allowed {
				return denied
			}
		}
	}
	return targetAuthorizationAllowed()
}

func (m *Manager) checkTargetAuthorization(ctx context.Context, p *principal.Principal, target coreworkflow.Target) targetAuthorizationDecision {
	if len(target.Steps) == 0 {
		return targetAuthorizationDenied(targetAuthorizationComponentTarget, targetAuthorizationReasonMissingAppStep, "", "", -1)
	}
	for stepIndex := range target.Steps {
		step := target.Steps[stepIndex]
		if step.App != nil {
			if denied := m.checkWorkflowStepAppAuthorization(ctx, p, step.App, targetAuthorizationComponentAppStep); !denied.allowed {
				return denied
			}
		}
		if step.Agent != nil {
			agentProviderName := strings.TrimSpace(step.Agent.ProviderName)
			if agentProviderName == "" {
				return targetAuthorizationDenied(targetAuthorizationComponentAgentProvider, targetAuthorizationReasonMissingAgentProvider, "", "", -1)
			}
			if !m.allowProvider(ctx, p, agentProviderName) {
				return targetAuthorizationDenied(targetAuthorizationComponentAgentProvider, targetAuthorizationReasonAuthorizerProviderDenied, agentProviderName, "", -1)
			}
			if !principal.AllowsProviderPermission(p, agentProviderName) {
				return targetAuthorizationDenied(targetAuthorizationComponentAgentProvider, targetAuthorizationReasonPrincipalProviderPermissionDenied, agentProviderName, "", -1)
			}
			hasSystemTools := workflowAgentToolRefsContainSystem(step.Agent.ToolRefs)
			for i := range step.Agent.ToolRefs {
				if denied := m.checkWorkflowAgentToolAuthorization(ctx, p, step.Agent.ToolRefs[i], hasSystemTools, i); !denied.allowed {
					return denied
				}
			}
		}
		_ = stepIndex
	}
	return targetAuthorizationAllowed()
}

func (m *Manager) checkWorkflowStepAppAuthorization(ctx context.Context, p *principal.Principal, app *coreworkflow.AppCall, component string) targetAuthorizationDecision {
	if app == nil {
		return targetAuthorizationAllowed()
	}
	appName := strings.TrimSpace(app.Name)
	operation := strings.TrimSpace(app.Operation)
	if appName == "" {
		return targetAuthorizationDenied(component, targetAuthorizationReasonMissingAppProvider, "", operation, -1)
	}
	if operation == "" {
		return targetAuthorizationDenied(component, targetAuthorizationReasonMissingAppOperation, appName, "", -1)
	}
	if !m.allowProvider(ctx, p, appName) {
		return targetAuthorizationDenied(component, targetAuthorizationReasonAuthorizerProviderDenied, appName, operation, -1)
	}
	if !m.allowOperation(ctx, p, appName, operation) {
		return targetAuthorizationDenied(component, targetAuthorizationReasonAuthorizerOperationDenied, appName, operation, -1)
	}
	if !principal.AllowsOperationPermission(p, appName, operation) {
		return targetAuthorizationDenied(component, targetAuthorizationReasonPrincipalOperationPermissionDenied, appName, operation, -1)
	}
	return targetAuthorizationAllowed()
}

func (m *Manager) checkWorkflowAgentToolAuthorization(ctx context.Context, p *principal.Principal, tool coreagent.ToolRef, hasSystemTools bool, index int) targetAuthorizationDecision {
	if systemName := strings.TrimSpace(tool.System); systemName != "" {
		if systemName != coreagent.SystemToolWorkflow || strings.TrimSpace(tool.Operation) == "" {
			return targetAuthorizationDenied(targetAuthorizationComponentAgentToolRef, targetAuthorizationReasonInvalidSystemToolRef, "", "", index)
		}
		if strings.TrimSpace(tool.App) != "" || strings.TrimSpace(tool.Connection) != "" || strings.TrimSpace(tool.Instance) != "" || tool.CredentialMode != "" {
			return targetAuthorizationDenied(targetAuthorizationComponentAgentToolRef, targetAuthorizationReasonInvalidSystemToolRef, "", "", index)
		}
		return targetAuthorizationAllowed()
	}
	appName := strings.TrimSpace(tool.App)
	operation := strings.TrimSpace(tool.Operation)
	if appName == "" {
		return targetAuthorizationDenied(targetAuthorizationComponentAgentToolRef, targetAuthorizationReasonMissingToolProvider, "", operation, index)
	}
	if hasSystemTools && (appName == "*" || operation == "") {
		return targetAuthorizationDenied(targetAuthorizationComponentAgentToolRef, targetAuthorizationReasonNonExactToolRefWithSystemTools, appName, operation, index)
	}
	if operation == "" {
		if !m.allowProvider(ctx, p, appName) {
			return targetAuthorizationDenied(targetAuthorizationComponentAgentToolRef, targetAuthorizationReasonAuthorizerProviderDenied, appName, "", index)
		}
		if !principal.AllowsProviderPermission(p, appName) {
			return targetAuthorizationDenied(targetAuthorizationComponentAgentToolRef, targetAuthorizationReasonPrincipalProviderPermissionDenied, appName, "", index)
		}
		return targetAuthorizationAllowed()
	}
	return m.checkWorkflowStepAppAuthorization(ctx, p, &coreworkflow.AppCall{Name: appName, Operation: operation}, targetAuthorizationComponentAgentToolRef)
}

func targetAuthorizationAllowed() targetAuthorizationDecision {
	return targetAuthorizationDecision{allowed: true}
}

func targetAuthorizationDenied(component, reason, provider, operation string, toolRefIndex int) targetAuthorizationDecision {
	return targetAuthorizationDecision{
		failure: targetAuthorizationFailure{
			component:    component,
			reason:       reason,
			provider:     provider,
			operation:    operation,
			toolRefIndex: toolRefIndex,
		},
	}
}

func workflowAgentToolRefsContainSystem(refs []coreagent.ToolRef) bool {
	for i := range refs {
		if strings.TrimSpace(refs[i].System) != "" {
			return true
		}
	}
	return false
}

func validateWorkflowAgentToolRefs(refs []coreagent.ToolRef) error {
	hasSystemTools := workflowAgentToolRefsContainSystem(refs)
	for i := range refs {
		ref := refs[i]
		systemName := strings.TrimSpace(ref.System)
		appName := strings.TrimSpace(ref.App)
		operation := strings.TrimSpace(ref.Operation)
		connection := strings.TrimSpace(ref.Connection)
		instance := strings.TrimSpace(ref.Instance)
		if ref.RunAs != nil {
			return fmt.Errorf("%w: workflow agent tool_refs[%d] runAs delegation is not supported", invocation.ErrAuthorizationDenied, i)
		}
		if systemName != "" {
			if appName != "" {
				return fmt.Errorf("%w: workflow agent tool_refs[%d] must set exactly one of app or system", invocation.ErrInvalidInvocation, i)
			}
			if systemName != coreagent.SystemToolWorkflow {
				return fmt.Errorf("%w: workflow agent tool_refs[%d].system %q is not supported", invocation.ErrInvalidInvocation, i, systemName)
			}
			if operation == "" {
				return fmt.Errorf("%w: workflow agent tool_refs[%d].operation is required for system tool refs", invocation.ErrOperationNotFound, i)
			}
			if connection != "" || instance != "" || ref.CredentialMode != "" {
				return fmt.Errorf("%w: workflow agent tool_refs[%d] system refs cannot include connection, instance, or credential mode", invocation.ErrInvalidInvocation, i)
			}
			continue
		}
		if !hasSystemTools {
			continue
		}
		if appName == "" || appName == "*" || operation == "" {
			return fmt.Errorf("%w: workflow agent tool_refs[%d] must be an exact app operation when workflow system tools are granted", invocation.ErrInvalidInvocation, i)
		}
	}
	return nil
}
