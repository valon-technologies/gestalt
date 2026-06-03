package workflowmanager

import (
	"context"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/workflowwire"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

func (m *Manager) CreateDefinition(ctx context.Context, p *principal.Principal, req DefinitionUpsert) (out *ManagedDefinition, err error) {
	p = principal.Canonicalized(p)
	ctx = WithCallerAppName(ctx, req.CallerAppName)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationDefinitionCreate)
	audit.setCallerApp(req.CallerAppName)
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
	providerName, provider, err := m.resolveProvider(ctx, strings.TrimSpace(req.ProviderName))
	if err != nil {
		return nil, err
	}
	target, err := m.resolveTarget(ctx, p, req.Target, req.CallerAppName)
	if err != nil {
		return nil, err
	}
	audit.setProvider(providerName)
	audit.setWorkflowTarget(target)

	targetProto, err := workflowwire.TargetToProto(target)
	if err != nil {
		return nil, err
	}
	definitionProto, err := provider.CreateDefinition(ctx, &proto.CreateWorkflowProviderDefinitionRequest{
		Target:         targetProto,
		IdempotencyKey: strings.TrimSpace(req.IdempotencyKey),
		CreatedBySubjectId: workflowSubjectIDFromPrincipal(p),
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

func (m *Manager) UpdateDefinition(ctx context.Context, p *principal.Principal, definitionID string, req DefinitionUpsert) (out *ManagedDefinition, err error) {
	p = principal.Canonicalized(p)
	ctx = WithCallerAppName(ctx, req.CallerAppName)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationDefinitionUpdate)
	audit.setCallerApp(req.CallerAppName)
	audit.setObjectTarget(workflowAuditTargetDefinition, definitionID, "")
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
	existing, err := m.requireOwnedDefinition(ctx, p, definitionID, "")
	if err != nil {
		return nil, err
	}
	providerName := existing.ProviderName
	provider := existing.provider
	if selected := strings.TrimSpace(req.ProviderName); selected != "" {
		selectedProviderName, _, err := m.resolveProvider(ctx, selected)
		if err != nil {
			return nil, err
		}
		if selectedProviderName != providerName {
			return nil, fmt.Errorf("%w: workflow definition %s belongs to provider %q, not %q", invocation.ErrInvalidInvocation, strings.TrimSpace(definitionID), providerName, selectedProviderName)
		}
	}
	target, err := m.resolveTarget(ctx, p, req.Target, req.CallerAppName)
	if err != nil {
		return nil, err
	}
	audit.setProvider(providerName)
	audit.setWorkflowTarget(target)
	targetProto, err := workflowwire.TargetToProto(target)
	if err != nil {
		return nil, err
	}
	definitionProto, err := provider.UpdateDefinition(ctx, &proto.UpdateWorkflowProviderDefinitionRequest{
		DefinitionId: strings.TrimSpace(definitionID),
		Target:       targetProto,
		RequestedBySubjectId: workflowSubjectIDFromPrincipal(p),
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
	audit.setProvider(existing.ProviderName)
	if existing.Definition != nil {
		audit.setWorkflowTarget(existing.Definition.Target)
	}
	return existing.provider.DeleteDefinition(ctx, &proto.DeleteWorkflowProviderDefinitionRequest{DefinitionId: strings.TrimSpace(definitionID)})
}

func (m *Manager) findDefinition(ctx context.Context, definitionID, providerSelection string) (*ManagedDefinition, error) {
	definitionID = strings.TrimSpace(definitionID)
	if definitionID == "" {
		return nil, core.ErrNotFound
	}
	if providerSelection = strings.TrimSpace(providerSelection); providerSelection != "" {
		providerName, provider, err := m.resolveProvider(ctx, providerSelection)
		if err != nil {
			return nil, err
		}
		definitionProto, err := provider.GetDefinition(ctx, &proto.GetWorkflowProviderDefinitionRequest{DefinitionId: definitionID})
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
		definitionProto, err := provider.GetDefinition(ctx, &proto.GetWorkflowProviderDefinitionRequest{DefinitionId: definitionID})
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
	definition, err := m.findDefinition(ctx, definitionID, providerSelection)
	if err != nil {
		return nil, err
	}
	if !m.definitionAccessible(ctx, p, definition) {
		return nil, core.ErrNotFound
	}
	return definition, nil
}

func (m *Manager) definitionAccessible(ctx context.Context, p *principal.Principal, definition *ManagedDefinition) bool {
	if definition == nil || definition.Definition == nil {
		return false
	}
	return workflowSubjectOwnedBy(definition.Definition.CreatedBySubjectID, p) && m.allowStoredTarget(ctx, p, definition.Definition.Target)
}

func (m *Manager) providerNames() []string {
	if m == nil || m.workflow == nil {
		return nil
	}
	return m.workflow.ProviderNames()
}
