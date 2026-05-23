package workflowmanager

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

func (m *Manager) CreateDefinition(ctx context.Context, p *principal.Principal, req DefinitionUpsert) (out *ManagedDefinition, err error) {
	p = principal.Canonicalized(p)
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
	providerName, provider, err := m.resolveProviderSelection(strings.TrimSpace(req.ProviderName))
	if err != nil {
		return nil, err
	}
	target, err := m.resolveTarget(ctx, p, req.Target, req.CallerAppName)
	if err != nil {
		return nil, err
	}
	audit.setProvider(providerName)
	audit.setWorkflowTarget(target)

	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	definitionID := newDefinitionID("")
	if idempotencyKey != "" {
		definitionID = newDefinitionID(workflowCreateIdempotencyScope(p, req.CallerAppName, idempotencyKey))
		existing, err := m.requireOwnedDefinition(ctx, definitionID, p)
		if err == nil {
			if !managedDefinitionMatchesUpsert(existing, providerName, target) {
				return nil, fmt.Errorf("%w: workflow definition idempotency key reused with different request", invocation.ErrInvalidInvocation)
			}
			audit.setObjectTarget(workflowAuditTargetDefinition, existing.Definition.ID, "")
			return existing, nil
		}
		if !errors.Is(err, core.ErrNotFound) {
			return nil, err
		}
	}
	audit.setObjectTarget(workflowAuditTargetDefinition, definitionID, "")
	ref, err := m.putExecutionRefWithPermissions(ctx, definitionID, providerName, provider, target, p, req.CallerAppName, "", req.Permissions)
	if err != nil {
		return nil, err
	}
	return &ManagedDefinition{
		ProviderName: providerName,
		Definition:   ref,
		provider:     provider,
	}, nil
}

func (m *Manager) GetDefinition(ctx context.Context, p *principal.Principal, definitionID string) (*ManagedDefinition, error) {
	return m.requireOwnedDefinition(ctx, definitionID, p)
}

func (m *Manager) UpdateDefinition(ctx context.Context, p *principal.Principal, definitionID string, req DefinitionUpsert) (out *ManagedDefinition, err error) {
	p = principal.Canonicalized(p)
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
	existing, err := m.requireOwnedDefinition(ctx, definitionID, p)
	if err != nil {
		return nil, err
	}
	audit.setProvider(existing.ProviderName)
	providerName, provider, err := m.resolveProviderSelection(strings.TrimSpace(req.ProviderName))
	if err != nil {
		return nil, err
	}
	target, err := m.resolveTarget(ctx, p, req.Target, req.CallerAppName)
	if err != nil {
		return nil, err
	}
	audit.setProvider(providerName)
	audit.setWorkflowTarget(target)
	if strings.TrimSpace(existing.ProviderName) != providerName {
		if _, err := m.revokeExecutionRefWithError(ctx, existing.Definition); err != nil {
			return nil, err
		}
		ref, err := m.putExecutionRefWithPermissions(ctx, strings.TrimSpace(definitionID), providerName, provider, target, p, req.CallerAppName, "", req.Permissions)
		if err != nil {
			m.restoreExecutionRef(ctx, existing.Definition)
			return nil, err
		}
		return &ManagedDefinition{
			ProviderName: providerName,
			Definition:   ref,
			provider:     provider,
		}, nil
	}
	ref, err := m.putExecutionRefWithPermissions(ctx, strings.TrimSpace(definitionID), providerName, provider, target, p, req.CallerAppName, "", req.Permissions)
	if err != nil {
		return nil, err
	}
	return &ManagedDefinition{
		ProviderName: providerName,
		Definition:   ref,
		provider:     provider,
	}, nil
}

func (m *Manager) DeleteDefinition(ctx context.Context, p *principal.Principal, definitionID string) (err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationDefinitionDelete)
	audit.setObjectTarget(workflowAuditTargetDefinition, definitionID, "")
	defer func() {
		audit.finish(ctx, err)
	}()
	existing, err := m.requireOwnedDefinition(ctx, definitionID, p)
	if err != nil {
		return err
	}
	audit.setProvider(existing.ProviderName)
	if existing.Definition != nil {
		audit.setWorkflowTarget(existing.Definition.Target)
	}
	m.revokeExecutionRef(ctx, existing.Definition)
	return nil
}

func managedDefinitionMatchesUpsert(existing *ManagedDefinition, providerName string, target coreworkflow.Target) bool {
	if existing == nil || existing.Definition == nil {
		return false
	}
	if strings.TrimSpace(existing.ProviderName) != strings.TrimSpace(providerName) {
		return false
	}
	return coreworkflow.TargetsEqual(existing.Definition.Target, target)
}
