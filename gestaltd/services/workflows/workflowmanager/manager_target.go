package workflowmanager

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (m *Manager) resolveProviderSelection(providerName string) (string, coreworkflow.Provider, error) {
	if m == nil || m.workflow == nil {
		return "", nil, ErrWorkflowNotConfigured
	}
	return m.workflow.ResolveProviderSelection(strings.TrimSpace(providerName))
}

func (m *Manager) resolveProviderByName(providerName string) (coreworkflow.Provider, error) {
	if m == nil || m.workflow == nil {
		return nil, ErrWorkflowNotConfigured
	}
	return m.workflow.ResolveProvider(strings.TrimSpace(providerName))
}

func (m *Manager) resolveRequestProviderTarget(ctx context.Context, p *principal.Principal, providerSelection string, target coreworkflow.Target, definitionID, callerAppName string) (string, coreworkflow.Provider, coreworkflow.Target, error) {
	definitionID = strings.TrimSpace(definitionID)
	if definitionID == "" {
		providerName, provider, err := m.resolveProviderSelection(strings.TrimSpace(providerSelection))
		if err != nil {
			return "", nil, coreworkflow.Target{}, err
		}
		resolvedTarget, err := m.resolveTarget(ctx, p, target, callerAppName)
		if err != nil {
			return "", nil, coreworkflow.Target{}, err
		}
		return providerName, provider, resolvedTarget, nil
	}
	if workflowTargetIsSet(target) {
		return "", nil, coreworkflow.Target{}, fmt.Errorf("%w: workflow request must set either target or definition_id, not both", invocation.ErrInvalidInvocation)
	}
	definition, err := m.requireOwnedDefinition(ctx, definitionID, p)
	if err != nil {
		return "", nil, coreworkflow.Target{}, err
	}
	if strings.TrimSpace(providerSelection) != "" {
		selectedProviderName, _, err := m.resolveProviderSelection(strings.TrimSpace(providerSelection))
		if err != nil {
			return "", nil, coreworkflow.Target{}, err
		}
		if selectedProviderName != definition.ProviderName {
			return "", nil, coreworkflow.Target{}, fmt.Errorf("%w: workflow definition %s belongs to provider %q, not %q", invocation.ErrInvalidInvocation, definitionID, definition.ProviderName, selectedProviderName)
		}
	}
	resolvedTarget, err := m.resolveTarget(ctx, p, definition.Definition.Target, callerAppName)
	if err != nil {
		return "", nil, coreworkflow.Target{}, err
	}
	return definition.ProviderName, definition.provider, resolvedTarget, nil
}

func (m *Manager) validateExistingProviderSelection(providerSelection, existingProviderName string) error {
	providerSelection = strings.TrimSpace(providerSelection)
	if providerSelection == "" {
		return nil
	}
	selectedProviderName, _, err := m.resolveProviderSelection(providerSelection)
	if err != nil {
		return err
	}
	if selectedProviderName != strings.TrimSpace(existingProviderName) {
		return fmt.Errorf("%w: workflow idempotency key reused with different provider", invocation.ErrInvalidInvocation)
	}
	return nil
}

func workflowTargetIsSet(target coreworkflow.Target) bool {
	return len(target.Steps) > 0
}

func (m *Manager) resolveTarget(ctx context.Context, p *principal.Principal, target coreworkflow.Target, callerAppName string) (coreworkflow.Target, error) {
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
			app, err := m.resolveWorkflowStepApp(ctx, p, *step.App, callerAppName)
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

func (m *Manager) resolveWorkflowStepApp(ctx context.Context, p *principal.Principal, target coreworkflow.AppCall, callerAppName string) (coreworkflow.AppCall, error) {
	appName := strings.TrimSpace(target.Name)
	if appName == "" {
		return coreworkflow.AppCall{}, fmt.Errorf("%w: workflow target app is required", invocation.ErrInvalidInvocation)
	}
	operation := strings.TrimSpace(target.Operation)
	if operation == "" {
		return coreworkflow.AppCall{}, fmt.Errorf("%w: workflow target operation is required", invocation.ErrInvalidInvocation)
	}
	credentialMode, err := m.normalizeWorkflowAppStepCredentialMode(target.CredentialMode, callerAppName, appName, operation)
	if err != nil {
		return coreworkflow.AppCall{}, err
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
	if !principal.AllowsOperationPermission(p, appName, opMeta.ID) && !m.callerAppDeclaresInvoke(callerAppName, appName, opMeta.ID) {
		return coreworkflow.AppCall{}, fmt.Errorf("%w: %s.%s", invocation.ErrAuthorizationDenied, appName, opMeta.ID)
	}
	if m.authorizer != nil && !m.authorizer.AllowCatalogOperation(ctx, p, appName, opMeta) {
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
	providerName, _, err := m.agent.ResolveProviderSelection(target.ProviderName)
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
	target.ResponseSchema = maps.Clone(target.ResponseSchema)
	target.ModelOptions = maps.Clone(target.ModelOptions)
	return target, nil
}

func (m *Manager) normalizeWorkflowAppStepCredentialMode(mode core.ConnectionMode, callerAppName, appName, operation string) (core.ConnectionMode, error) {
	mode = core.NormalizeOptionalConnectionMode(mode)
	switch mode {
	case "":
		return "", nil
	case core.ConnectionModeNone, core.ConnectionModeUser:
	default:
		return "", fmt.Errorf("%w: workflow target credential_mode %q is not supported", invocation.ErrInvalidInvocation, mode)
	}
	if strings.TrimSpace(callerAppName) == "" {
		return "", fmt.Errorf("%w: workflow target credential_mode requires a caller app declaration", invocation.ErrAuthorizationDenied)
	}
	declared, ok, err := m.callerAppInvokeCredentialMode(callerAppName, appName, operation)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("%w: workflow target credential_mode requires a declared invoke mode", invocation.ErrAuthorizationDenied)
	}
	if mode != declared {
		return "", fmt.Errorf("%w: workflow target credential_mode %q exceeds declared invoke mode %q", invocation.ErrAuthorizationDenied, mode, declared)
	}
	return mode, nil
}

func (m *Manager) requireOwnedDefinition(ctx context.Context, definitionID string, p *principal.Principal) (*ManagedDefinition, error) {
	definitionID = strings.TrimSpace(definitionID)
	if definitionID == "" || !strings.HasPrefix(definitionID, workflowDefinitionExecutionRefBasePrefix) {
		return nil, core.ErrNotFound
	}
	refs, err := m.listOwnedExecutionRefs(ctx, p, true)
	if err != nil {
		return nil, err
	}
	var match *coreworkflow.ExecutionReference
	for _, ref := range refs {
		if ref == nil || strings.TrimSpace(ref.ID) != definitionID {
			continue
		}
		if match != nil {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateExecutionRefs, definitionID)
		}
		match = ref
	}
	if match == nil || !m.allowTarget(ctx, p, match.Target) {
		return nil, core.ErrNotFound
	}
	provider, err := m.resolveProviderByName(strings.TrimSpace(match.ProviderName))
	if err != nil {
		return nil, err
	}
	return &ManagedDefinition{
		ProviderName: strings.TrimSpace(match.ProviderName),
		Definition:   match,
		provider:     provider,
	}, nil
}

func (m *Manager) findOwnedExecutionRef(ctx context.Context, scheduleID string, p *principal.Principal) (*coreworkflow.ExecutionReference, error) {
	refs, err := m.listOwnedExecutionRefs(ctx, p, true)
	if err != nil {
		return nil, err
	}
	prefix := scheduleExecutionRefPrefix(scheduleID)
	var match *coreworkflow.ExecutionReference
	for _, ref := range refs {
		if !strings.HasPrefix(strings.TrimSpace(ref.ID), prefix) {
			continue
		}
		if match != nil {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateExecutionRefs, scheduleID)
		}
		match = ref
	}
	if match == nil {
		return nil, core.ErrNotFound
	}
	return match, nil
}

func (m *Manager) findOwnedEventTriggerExecutionRef(ctx context.Context, triggerID string, p *principal.Principal) (*coreworkflow.ExecutionReference, error) {
	refs, err := m.listOwnedExecutionRefs(ctx, p, true)
	if err != nil {
		return nil, err
	}
	prefix := eventTriggerExecutionRefPrefix(triggerID)
	var match *coreworkflow.ExecutionReference
	for _, ref := range refs {
		if !strings.HasPrefix(strings.TrimSpace(ref.ID), prefix) {
			continue
		}
		if match != nil {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateExecutionRefs, triggerID)
		}
		match = ref
	}
	if match == nil {
		return nil, core.ErrNotFound
	}
	return match, nil
}

func (m *Manager) requireOwnedSchedule(ctx context.Context, scheduleID string, p *principal.Principal) (*ManagedSchedule, error) {
	scheduleID = strings.TrimSpace(scheduleID)
	if scheduleID == "" {
		return nil, core.ErrNotFound
	}
	ref, err := m.findOwnedExecutionRef(ctx, scheduleID, p)
	if err != nil {
		return nil, err
	}
	if !m.allowTarget(ctx, p, ref.Target) {
		return nil, core.ErrNotFound
	}
	provider, err := m.resolveProviderByName(strings.TrimSpace(ref.ProviderName))
	if err != nil {
		return nil, err
	}
	schedule, err := provider.GetSchedule(ctx, coreworkflow.GetScheduleRequest{ScheduleID: scheduleID})
	if err != nil {
		return nil, err
	}
	if !scheduleMatchesExecutionRef(ref.ProviderName, schedule, ref) {
		return nil, core.ErrNotFound
	}
	return &ManagedSchedule{
		ProviderName: strings.TrimSpace(ref.ProviderName),
		Schedule:     schedule,
		ExecutionRef: ref,
		provider:     provider,
	}, nil
}

func (m *Manager) requireOwnedEventTrigger(ctx context.Context, triggerID string, p *principal.Principal) (*ManagedEventTrigger, error) {
	triggerID = strings.TrimSpace(triggerID)
	if triggerID == "" {
		return nil, core.ErrNotFound
	}
	ref, err := m.findOwnedEventTriggerExecutionRef(ctx, triggerID, p)
	if err != nil {
		return nil, err
	}
	if !m.allowTarget(ctx, p, ref.Target) {
		return nil, core.ErrNotFound
	}
	provider, err := m.resolveProviderByName(strings.TrimSpace(ref.ProviderName))
	if err != nil {
		return nil, err
	}
	trigger, err := provider.GetEventTrigger(ctx, coreworkflow.GetEventTriggerRequest{TriggerID: triggerID})
	if err != nil {
		return nil, err
	}
	if !eventTriggerMatchesExecutionRef(ref.ProviderName, trigger, ref) {
		return nil, core.ErrNotFound
	}
	return &ManagedEventTrigger{
		ProviderName: strings.TrimSpace(ref.ProviderName),
		Trigger:      trigger,
		ExecutionRef: ref,
		provider:     provider,
	}, nil
}

func (m *Manager) listOwnedExecutionRefs(ctx context.Context, p *principal.Principal, activeOnly bool) ([]*coreworkflow.ExecutionReference, error) {
	if m == nil || m.workflow == nil {
		return nil, ErrExecutionRefsNotConfigured
	}
	subjectID := strings.TrimSpace(principalSubjectID(principal.Canonicalized(p)))
	if subjectID == "" {
		return nil, ErrWorkflowSubjectRequired
	}
	out := []*coreworkflow.ExecutionReference{}
	for _, providerName := range m.workflow.ProviderNames() {
		provider, err := m.resolveProviderByName(providerName)
		if err != nil {
			return nil, err
		}
		store, err := workflowExecutionReferenceStore(providerName, provider)
		if err != nil {
			return nil, err
		}
		refs, err := store.ListExecutionReferences(ctx, subjectID)
		if err != nil {
			return nil, err
		}
		for _, ref := range refs {
			ref = workflowExecutionRefForProvider(ref, providerName)
			if !executionRefOwnedBy(ref, p) || (activeOnly && !executionRefActive(ref)) {
				continue
			}
			out = append(out, ref)
		}
	}
	return out, nil
}

func (m *Manager) putExecutionRef(ctx context.Context, executionRefID, providerName string, provider coreworkflow.Provider, target coreworkflow.Target, p *principal.Principal, callerAppName, sourceDefinitionID string) (*coreworkflow.ExecutionReference, error) {
	return m.putExecutionRefWithPermissions(ctx, executionRefID, providerName, provider, target, p, callerAppName, sourceDefinitionID, nil)
}

func (m *Manager) putExecutionRefWithPermissions(ctx context.Context, executionRefID, providerName string, provider coreworkflow.Provider, target coreworkflow.Target, p *principal.Principal, callerAppName, sourceDefinitionID string, permissions []core.AccessPermission) (*coreworkflow.ExecutionReference, error) {
	store, err := workflowExecutionReferenceStore(providerName, provider)
	if err != nil {
		return nil, err
	}
	p = principal.Canonicalized(p)
	subjectID := strings.TrimSpace(principalSubjectID(p))
	if subjectID == "" {
		return nil, ErrWorkflowSubjectRequired
	}
	actor := workflowActorFromPrincipal(p)
	return store.PutExecutionReference(ctx, &coreworkflow.ExecutionReference{
		ID:                  executionRefID,
		ProviderName:        strings.TrimSpace(providerName),
		Target:              target,
		CallerAppName:       strings.TrimSpace(callerAppName),
		SourceDefinitionID:  strings.TrimSpace(sourceDefinitionID),
		SubjectID:           subjectID,
		SubjectKind:         actor.SubjectKind,
		DisplayName:         actor.DisplayName,
		AuthSource:          actor.AuthSource,
		CredentialSubjectID: strings.TrimSpace(principal.EffectiveCredentialSubjectID(p)),
		Permissions:         m.executionRefPermissionsWithOverride(p, target, callerAppName, permissions),
	})
}

func (m *Manager) putRunExecutionRef(ctx context.Context, executionRefID, providerName string, provider coreworkflow.Provider, target coreworkflow.Target, p *principal.Principal, callerAppName, sourceDefinitionID string, permissions []core.AccessPermission) (*coreworkflow.ExecutionReference, bool, error) {
	store, err := workflowExecutionReferenceStore(providerName, provider)
	if err != nil {
		return nil, false, err
	}
	expectedPermissions := m.executionRefPermissionsWithOverride(p, target, callerAppName, permissions)
	existing, err := store.GetExecutionReference(ctx, executionRefID)
	if err == nil {
		existing = workflowExecutionRefForProvider(existing, providerName)
		if !runExecutionRefMatches(existing, executionRefID, providerName, target, p, callerAppName, sourceDefinitionID, expectedPermissions) {
			return nil, false, fmt.Errorf("%w: %s", ErrDuplicateExecutionRefs, executionRefID)
		}
		if executionRefActive(existing) {
			return existing, false, nil
		}
	} else if !isWorkflowProviderNotFound(err) {
		return nil, false, err
	}
	p = principal.Canonicalized(p)
	subjectID := strings.TrimSpace(principalSubjectID(p))
	if subjectID == "" {
		return nil, false, ErrWorkflowSubjectRequired
	}
	actor := workflowActorFromPrincipal(p)
	ref, err := store.PutExecutionReference(ctx, &coreworkflow.ExecutionReference{
		ID:                  executionRefID,
		ProviderName:        strings.TrimSpace(providerName),
		Target:              target,
		CallerAppName:       strings.TrimSpace(callerAppName),
		SourceDefinitionID:  strings.TrimSpace(sourceDefinitionID),
		SubjectID:           subjectID,
		SubjectKind:         actor.SubjectKind,
		DisplayName:         actor.DisplayName,
		AuthSource:          actor.AuthSource,
		CredentialSubjectID: strings.TrimSpace(principal.EffectiveCredentialSubjectID(p)),
		Permissions:         expectedPermissions,
	})
	if err != nil {
		return nil, false, err
	}
	return ref, true, nil
}

func (m *Manager) executionRefPermissionsWithOverride(p *principal.Principal, target coreworkflow.Target, callerAppName string, override []core.AccessPermission) []core.AccessPermission {
	if override == nil {
		return m.executionRefPermissions(p, target, callerAppName)
	}
	out := principal.PermissionsToAccessPermissions(principal.CompilePermissions(override))
	if len(out) == 0 {
		return []core.AccessPermission{{App: workflowNoProviderPermissionsApp}}
	}
	return out
}

func (m *Manager) putSignalOrStartExecutionRef(ctx context.Context, executionRefID, providerName string, provider coreworkflow.Provider, target coreworkflow.Target, p *principal.Principal, callerAppName, sourceDefinitionID string, permissions []core.AccessPermission) (*coreworkflow.ExecutionReference, error) {
	store, err := workflowExecutionReferenceStore(providerName, provider)
	if err != nil {
		return nil, err
	}
	existing, err := store.GetExecutionReference(ctx, executionRefID)
	if err == nil {
		existing = workflowExecutionRefForProvider(existing, providerName)
		if !signalOrStartExecutionRefMatches(existing, executionRefID, providerName, target, p, callerAppName, permissions) {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateExecutionRefs, executionRefID)
		}
		if executionRefActive(existing) {
			return existing, nil
		}
	} else if !isWorkflowProviderNotFound(err) {
		return nil, err
	}

	ref, err := m.putExecutionRef(ctx, executionRefID, providerName, provider, target, p, callerAppName, sourceDefinitionID)
	if err != nil {
		return nil, err
	}
	return ref, nil
}

func (m *Manager) executionRefPermissions(p *principal.Principal, target coreworkflow.Target, callerAppName string) []core.AccessPermission {
	p = principal.Canonicalized(p)
	if p == nil || p.TokenPermissions == nil {
		return principal.PermissionsToAccessPermissions(nil)
	}
	permissions := principal.ClonePermissionSet(p.TokenPermissions)
	for i := range target.Steps {
		step := target.Steps[i]
		if step.App != nil && m.callerAppDeclaresInvoke(callerAppName, step.App.Name, step.App.Operation) {
			addWorkflowPermission(permissions, step.App.Name, step.App.Operation)
		}
		if step.Agent == nil {
			continue
		}
		for j := range step.Agent.ToolRefs {
			tool := step.Agent.ToolRefs[j]
			appName := strings.TrimSpace(tool.App)
			operation := strings.TrimSpace(tool.Operation)
			if appName == "" || appName == "*" || operation == "" {
				continue
			}
			if m.callerAppDeclaresInvoke(callerAppName, appName, operation) {
				addWorkflowPermission(permissions, appName, operation)
			}
		}
	}
	out := principal.PermissionsToAccessPermissions(permissions)
	if len(out) == 0 {
		return []core.AccessPermission{{App: workflowNoProviderPermissionsApp}}
	}
	return out
}

func executionRefPrincipal(p *principal.Principal, permissions []core.AccessPermission) *principal.Principal {
	p = principal.Canonicalized(p)
	if p == nil {
		return nil
	}
	compiled := principal.CompilePermissions(permissions)
	if permissions != nil && compiled == nil {
		compiled = principal.PermissionSet{}
	}
	next := *p
	next.TokenPermissions = compiled
	next.ActionPermissions = nil
	next.Scopes = principal.PermissionApps(compiled)
	return principal.Canonicalize(&next)
}

func (m *Manager) callerAppDeclaresInvoke(callerAppName, appName, operation string) bool {
	callerAppName = strings.TrimSpace(callerAppName)
	appName = strings.TrimSpace(appName)
	operation = strings.TrimSpace(operation)
	if callerAppName == "" || appName == "" || operation == "" || m == nil {
		return false
	}
	for _, invoke := range m.appInvokes[callerAppName] {
		if strings.TrimSpace(invoke.Surface) != "" {
			continue
		}
		if strings.TrimSpace(invoke.App) == appName && strings.TrimSpace(invoke.Operation) == operation {
			return true
		}
	}
	return false
}

func addWorkflowPermission(permissions principal.PermissionSet, appName, operation string) {
	appName = strings.TrimSpace(appName)
	operation = strings.TrimSpace(operation)
	if permissions == nil || appName == "" || operation == "" {
		return
	}
	if operations, ok := permissions[appName]; ok && operations == nil {
		return
	}
	operations := permissions[appName]
	if operations == nil {
		operations = map[string]struct{}{}
		permissions[appName] = operations
	}
	operations[operation] = struct{}{}
}

func (m *Manager) revokeExecutionRef(ctx context.Context, ref *coreworkflow.ExecutionReference) {
	_, _ = m.revokeExecutionRefWithError(ctx, ref)
}

func (m *Manager) revokeExecutionRefWithError(ctx context.Context, ref *coreworkflow.ExecutionReference) (*coreworkflow.ExecutionReference, error) {
	if m == nil || ref == nil || strings.TrimSpace(ref.ID) == "" {
		return nil, nil
	}
	providerName := strings.TrimSpace(ref.ProviderName)
	provider, err := m.resolveProviderByName(providerName)
	if err != nil {
		return nil, err
	}
	store, err := workflowExecutionReferenceStore(providerName, provider)
	if err != nil {
		return nil, err
	}
	cloned := *ref
	now := m.now().UTC().Truncate(time.Second)
	cloned.RevokedAt = &now
	return store.PutExecutionReference(ctx, &cloned)
}

func (m *Manager) restoreExecutionRef(ctx context.Context, ref *coreworkflow.ExecutionReference) {
	if m == nil || ref == nil || strings.TrimSpace(ref.ID) == "" {
		return
	}
	providerName := strings.TrimSpace(ref.ProviderName)
	provider, err := m.resolveProviderByName(providerName)
	if err != nil {
		return
	}
	store, err := workflowExecutionReferenceStore(providerName, provider)
	if err != nil {
		return
	}
	cloned := *ref
	_, _ = store.PutExecutionReference(ctx, &cloned)
}

func workflowExecutionReferenceStore(providerName string, provider coreworkflow.Provider) (coreworkflow.ExecutionReferenceStore, error) {
	if provider == nil {
		return nil, fmt.Errorf("%w: workflow provider %q is not configured", ErrExecutionRefsNotConfigured, strings.TrimSpace(providerName))
	}
	store, ok := provider.(coreworkflow.ExecutionReferenceStore)
	if !ok {
		return nil, fmt.Errorf("%w: workflow provider %q does not support execution references", ErrExecutionRefsNotConfigured, strings.TrimSpace(providerName))
	}
	return store, nil
}

func workflowExecutionRefForProvider(ref *coreworkflow.ExecutionReference, providerName string) *coreworkflow.ExecutionReference {
	if ref == nil {
		return nil
	}
	providerName = strings.TrimSpace(providerName)
	refProviderName := strings.TrimSpace(ref.ProviderName)
	if providerName == "" || refProviderName == providerName {
		return ref
	}
	if refProviderName != "" {
		return nil
	}
	cloned := *ref
	cloned.ProviderName = providerName
	return &cloned
}

func isWorkflowProviderNotFound(err error) bool {
	return errors.Is(err, core.ErrNotFound) || status.Code(err) == codes.NotFound
}

func (m *Manager) allowProvider(ctx context.Context, p *principal.Principal, provider string) bool {
	if m == nil || m.authorizer == nil {
		return true
	}
	return m.authorizer.AllowProvider(ctx, p, provider)
}

func (m *Manager) allowOperation(ctx context.Context, p *principal.Principal, provider, operation string) bool {
	if m == nil || m.authorizer == nil {
		return true
	}
	return m.authorizer.AllowOperation(ctx, p, provider, operation)
}

func (m *Manager) providerAccessContext(ctx context.Context, p *principal.Principal, provider string) invocation.AccessContext {
	if m == nil || m.authorizer == nil {
		return invocation.AccessContext{}
	}
	access, _ := m.authorizer.ResolveAccess(ctx, p, provider)
	return access
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

func (m *Manager) allowTarget(ctx context.Context, p *principal.Principal, target coreworkflow.Target) bool {
	return m.checkTargetAuthorization(ctx, p, target).allowed
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

func (m *Manager) callerAppInvokeCredentialMode(callerAppName, appName, operation string) (core.ConnectionMode, bool, error) {
	callerAppName = strings.TrimSpace(callerAppName)
	appName = strings.TrimSpace(appName)
	operation = strings.TrimSpace(operation)
	if callerAppName == "" || appName == "" || operation == "" || m == nil {
		return "", false, nil
	}
	for _, invoke := range m.appInvokes[callerAppName] {
		if strings.TrimSpace(invoke.Surface) != "" {
			continue
		}
		if strings.TrimSpace(invoke.App) != appName || strings.TrimSpace(invoke.Operation) != operation {
			continue
		}
		mode := core.NormalizeOptionalConnectionMode(invoke.CredentialMode)
		switch mode {
		case "":
			return "", false, nil
		case core.ConnectionModeNone, core.ConnectionModeUser:
			return mode, true, nil
		default:
			return "", false, fmt.Errorf("%w: caller app invoke credentialMode %q is not supported", invocation.ErrInvalidInvocation, invoke.CredentialMode)
		}
	}
	return "", false, nil
}
