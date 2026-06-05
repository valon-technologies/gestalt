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

func (m *Manager) ApplyDefinition(ctx context.Context, p *principal.Principal, req DefinitionApply) (out *ManagedDefinition, err error) {
	p = principal.Canonicalized(p)
	caller := workflowCaller(ctx, req.Caller)
	ctx = withWorkflowCaller(ctx, caller)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationDefinitionApply)
	audit.setCallerApp(workflowCallerAuditAppName(caller))
	defer func() {
		if out != nil && out.Definition != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetDefinition, out.Definition.ID, "")
			audit.setWorkflowTarget(out.Definition.Target)
		}
		audit.finish(ctx, err)
	}()
	if strings.TrimSpace(principalSubjectID(p)) == "" {
		return nil, ErrWorkflowSubjectRequired
	}
	if err := m.enforcer().RequireWorkflowPlatform(ctx, p, workflowAuditOperationDefinitionApply); err != nil {
		return nil, err
	}
	providerName, provider, err := m.resolveProvider(ctx, strings.TrimSpace(req.ProviderName))
	if err != nil {
		return nil, err
	}
	spec, err := m.resolveDefinitionSpec(ctx, p, req.Spec)
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
		Spec:                 specProto,
		IdempotencyKey:       strings.TrimSpace(req.IdempotencyKey),
		RequestedBySubjectId: workflowSubjectIDFromPrincipal(p),
		Context:              reqContext,
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

func (m *Manager) GetDefinition(ctx context.Context, p *principal.Principal, definitionID string) (*ManagedDefinition, error) {
	return m.requireOwnedDefinition(ctx, p, definitionID, "")
}

func (m *Manager) ListDefinitions(ctx context.Context, p *principal.Principal) (*ListDefinitionsResponse, error) {
	if m == nil || m.workflow == nil {
		return nil, ErrWorkflowNotConfigured
	}
	p = principal.Canonicalized(p)
	subjectID := strings.TrimSpace(principalSubjectID(p))
	if subjectID == "" {
		return nil, ErrWorkflowSubjectRequired
	}
	reqContext, err := workflowProviderRequestContext(ctx, p, invocation.CallerProvider{})
	if err != nil {
		return nil, err
	}
	out := make([]*ManagedDefinition, 0)
	for _, providerName := range m.providerNames() {
		_, provider, err := m.resolveProvider(ctx, providerName)
		if err != nil {
			return nil, err
		}
		resp, err := provider.ListDefinitions(ctx, &proto.ListWorkflowProviderDefinitionsRequest{Context: reqContext})
		if err != nil {
			return nil, err
		}
		for _, definitionProto := range resp.GetDefinitions() {
			if definitionProto == nil {
				continue
			}
			definition, err := workflowwire.DefinitionFromProto(definitionProto)
			if err != nil {
				return nil, err
			}
			managed := &ManagedDefinition{
				ProviderName: strings.TrimSpace(providerName),
				Definition:   definition,
				provider:     provider,
			}
			accessible, err := m.definitionAccessible(ctx, p, managed)
			if err != nil {
				return nil, err
			}
			if accessible {
				out = append(out, managed)
			}
		}
	}
	return &ListDefinitionsResponse{Definitions: out}, nil
}

func (m *Manager) SetDefinitionPaused(ctx context.Context, p *principal.Principal, definitionID string, paused bool) (out *ManagedDefinition, err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationDefinitionPause)
	audit.setObjectTarget(workflowAuditTargetDefinition, definitionID, "")
	defer func() {
		if out != nil && out.Definition != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetDefinition, out.Definition.ID, "")
			audit.setWorkflowTarget(out.Definition.Target)
		}
		audit.finish(ctx, err)
	}()
	existing, err := m.requireOwnedDefinition(ctx, p, definitionID, "")
	if err != nil {
		return nil, err
	}
	reqContext, err := workflowProviderRequestContext(ctx, p, invocation.CallerProvider{})
	if err != nil {
		return nil, err
	}
	definitionProto, err := existing.provider.SetDefinitionPaused(ctx, &proto.SetWorkflowProviderDefinitionPausedRequest{
		DefinitionId:         strings.TrimSpace(definitionID),
		Paused:               paused,
		RequestedBySubjectId: workflowSubjectIDFromPrincipal(p),
		Context:              reqContext,
	})
	if err != nil {
		return nil, err
	}
	definition, err := workflowwire.DefinitionFromProto(definitionProto)
	if err != nil {
		return nil, err
	}
	return &ManagedDefinition{ProviderName: existing.ProviderName, Definition: definition, provider: existing.provider}, nil
}

func (m *Manager) SetActivationPaused(ctx context.Context, p *principal.Principal, definitionID, activationID string, paused bool) (out *ManagedDefinition, err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationActivationPause)
	audit.setObjectTarget(workflowAuditTargetDefinition, definitionID, activationID)
	defer func() {
		if out != nil && out.Definition != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetDefinition, out.Definition.ID, activationID)
			audit.setWorkflowTarget(out.Definition.Target)
		}
		audit.finish(ctx, err)
	}()
	existing, err := m.requireOwnedDefinition(ctx, p, definitionID, "")
	if err != nil {
		return nil, err
	}
	reqContext, err := workflowProviderRequestContext(ctx, p, invocation.CallerProvider{})
	if err != nil {
		return nil, err
	}
	definitionProto, err := existing.provider.SetActivationPaused(ctx, &proto.SetWorkflowProviderActivationPausedRequest{
		DefinitionId:         strings.TrimSpace(definitionID),
		ActivationId:         strings.TrimSpace(activationID),
		Paused:               paused,
		RequestedBySubjectId: workflowSubjectIDFromPrincipal(p),
		Context:              reqContext,
	})
	if err != nil {
		return nil, err
	}
	definition, err := workflowwire.DefinitionFromProto(definitionProto)
	if err != nil {
		return nil, err
	}
	return &ManagedDefinition{ProviderName: existing.ProviderName, Definition: definition, provider: existing.provider}, nil
}

func (m *Manager) DeleteDefinition(ctx context.Context, p *principal.Principal, definitionID string) (err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationDefinitionDelete)
	audit.setObjectTarget(workflowAuditTargetDefinition, definitionID, "")
	defer func() {
		audit.finish(ctx, err)
	}()
	existing, err := m.requireOwnedDefinition(ctx, p, definitionID, "")
	if err != nil {
		return err
	}
	if err := m.enforcer().RequireWorkflowPlatform(ctx, p, workflowAuditOperationDefinitionDelete); err != nil {
		return err
	}
	audit.setProvider(existing.ProviderName)
	if existing.Definition != nil {
		audit.setWorkflowTarget(existing.Definition.Target)
	}
	reqContext, err := workflowProviderRequestContext(ctx, p, invocation.CallerProvider{})
	if err != nil {
		return err
	}
	return existing.provider.DeleteDefinition(ctx, &proto.DeleteWorkflowProviderDefinitionRequest{
		DefinitionId: strings.TrimSpace(definitionID),
		Context:      reqContext,
	})
}

func (m *Manager) resolveDefinitionSpec(ctx context.Context, p *principal.Principal, spec coreworkflow.DefinitionSpec) (coreworkflow.DefinitionSpec, error) {
	spec.ID = strings.TrimSpace(spec.ID)
	if spec.ID == "" {
		return coreworkflow.DefinitionSpec{}, fmt.Errorf("%w: workflow definition id is required", invocation.ErrInvalidInvocation)
	}
	authorizedPrincipal, err := m.authorizeAgentWorkflowTarget(ctx, p, workflowManagerOperationDefinitionsApply, spec.Target, invocation.CallerProvider{})
	if err != nil {
		return coreworkflow.DefinitionSpec{}, err
	}
	target, err := m.resolveTarget(ctx, authorizedPrincipal, spec.Target)
	if err != nil {
		return coreworkflow.DefinitionSpec{}, err
	}
	if _, err := m.authorizeAgentWorkflowTarget(ctx, authorizedPrincipal, workflowManagerOperationTargetScopeOnly, target, invocation.CallerProvider{}); err != nil {
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

func normalizeDefinitionActivations(values []coreworkflow.Activation) ([]coreworkflow.Activation, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]coreworkflow.Activation, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for i := range values {
		activation := values[i]
		activation.ID = strings.TrimSpace(activation.ID)
		if activation.ID == "" {
			return nil, fmt.Errorf("%w: workflow definition.activations[%d].id is required", invocation.ErrInvalidInvocation, i)
		}
		if _, exists := seen[activation.ID]; exists {
			return nil, fmt.Errorf("%w: workflow definition.activations[%d].id duplicates %q", invocation.ErrInvalidInvocation, i, activation.ID)
		}
		seen[activation.ID] = struct{}{}
		switch {
		case activation.Schedule != nil && activation.Event != nil:
			return nil, fmt.Errorf("%w: workflow definition.activations[%d] must set exactly one of schedule or event", invocation.ErrInvalidInvocation, i)
		case activation.Schedule != nil:
			activation.Schedule.Cron = strings.TrimSpace(activation.Schedule.Cron)
			activation.Schedule.Timezone = strings.TrimSpace(activation.Schedule.Timezone)
			if activation.Schedule.Cron == "" {
				return nil, fmt.Errorf("%w: workflow definition.activations[%d].schedule.cron is required", invocation.ErrInvalidInvocation, i)
			}
		case activation.Event != nil:
			activation.Event.Match.Type = strings.TrimSpace(activation.Event.Match.Type)
			activation.Event.Match.Source = strings.TrimSpace(activation.Event.Match.Source)
			activation.Event.Match.Subject = strings.TrimSpace(activation.Event.Match.Subject)
			if activation.Event.Match.Type == "" {
				return nil, fmt.Errorf("%w: workflow definition.activations[%d].event.match.type is required", invocation.ErrInvalidInvocation, i)
			}
		default:
			return nil, fmt.Errorf("%w: workflow definition.activations[%d] must set schedule or event", invocation.ErrInvalidInvocation, i)
		}
		if err := coreworkflow.ValidateValueRefs(fmt.Sprintf("workflow definition.activations[%d].input", i), activation.Input, map[string]struct{}{}); err != nil {
			return nil, err
		}
		out = append(out, activation)
	}
	return out, nil
}

func (m *Manager) findDefinition(ctx context.Context, p *principal.Principal, definitionID, providerSelection string) (*ManagedDefinition, error) {
	definitionID = strings.TrimSpace(definitionID)
	if definitionID == "" {
		return nil, core.ErrNotFound
	}
	reqContext, err := workflowProviderRequestContext(ctx, principal.Canonicalized(p), invocation.CallerProvider{})
	if err != nil {
		return nil, err
	}
	if providerSelection = strings.TrimSpace(providerSelection); providerSelection != "" {
		providerName, provider, err := m.resolveProvider(ctx, providerSelection)
		if err != nil {
			return nil, err
		}
		definitionProto, err := provider.GetDefinition(ctx, &proto.GetWorkflowProviderDefinitionRequest{
			DefinitionId: definitionID,
			Context:      reqContext,
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

	var match *ManagedDefinition
	var firstErr error
	for _, providerName := range m.providerNames() {
		_, provider, err := m.resolveProvider(ctx, providerName)
		if err != nil {
			return nil, err
		}
		definitionProto, err := provider.GetDefinition(ctx, &proto.GetWorkflowProviderDefinitionRequest{
			DefinitionId: definitionID,
			Context:      reqContext,
		})
		if err != nil {
			if isWorkflowProviderNotFound(err) {
				continue
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		definition, err := workflowwire.DefinitionFromProto(definitionProto)
		if err != nil {
			return nil, err
		}
		if match != nil {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateWorkflowObjects, definitionID)
		}
		match = &ManagedDefinition{ProviderName: strings.TrimSpace(providerName), Definition: definition, provider: provider}
	}
	if match != nil {
		return match, nil
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return nil, core.ErrNotFound
}

func (m *Manager) requireOwnedDefinition(ctx context.Context, p *principal.Principal, definitionID, providerSelection string) (*ManagedDefinition, error) {
	definition, err := m.findDefinition(ctx, p, definitionID, providerSelection)
	if err != nil {
		return nil, err
	}
	accessible, err := m.definitionAccessible(ctx, p, definition)
	if err != nil {
		return nil, err
	}
	if !accessible {
		return nil, core.ErrNotFound
	}
	return definition, nil
}

func (m *Manager) definitionAccessible(ctx context.Context, p *principal.Principal, definition *ManagedDefinition) (bool, error) {
	if definition == nil || definition.Definition == nil {
		return false, nil
	}
	if !workflowSubjectOwnedBy(definition.Definition.CreatedBySubjectID, p) {
		return false, nil
	}
	return m.storedTargetAccessible(ctx, p, definition.Definition.Target)
}

func (m *Manager) providerNames() []string {
	if m == nil || m.workflow == nil {
		return nil
	}
	return m.workflow.ProviderNames()
}
