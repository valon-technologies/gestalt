package bootstrap

import (
	"context"
	"fmt"
	"strings"

	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/services/access"
	"github.com/valon-technologies/gestalt/server/services/agents/agentgrant"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

type agentToolAuthorizer struct {
	ctx       context.Context
	enforcer  *access.Enforcer
	principal *principal.Principal
}

func newAgentToolAuthorizer(ctx context.Context, enforcer *access.Enforcer, p *principal.Principal) agentToolAuthorizer {
	return agentToolAuthorizer{ctx: ctx, enforcer: enforcer, principal: p}
}

func (a agentToolAuthorizer) validateForGrant(grant agentgrant.Grant, target coreagent.ToolTarget, rawToolID string) error {
	if a.principal == nil {
		return fmt.Errorf("%w: agent execution principal is required", invocation.ErrInternal)
	}
	if err := validateAgentMCPCatalogGrant(grant.ToolSource, grant.ToolRefs); err != nil {
		return err
	}
	if len(grant.ToolRefs) == 0 {
		return fmt.Errorf("%w: agent tool %q is outside the turn tool scope", access.ErrDenied, rawToolID)
	}
	appTarget, err := a.validateConcreteToolTarget(target, grant.ToolRefs, rawToolID, "agent")
	if err != nil {
		return err
	}
	if !appTarget {
		return nil
	}
	if target.CredentialMode != "" && !agentToolCredentialModeExplicitlyGranted(target, grant.ToolRefs, grant.Tools) {
		return fmt.Errorf("%w: agent tool %q credential mode was not granted to this turn", access.ErrDenied, rawToolID)
	}
	if target.RunAs != nil && !agentToolRunAsExplicitlyGranted(target, grant.ToolRefs, grant.Tools) {
		return fmt.Errorf("%w: agent tool %q runAs delegation was not granted to this turn", access.ErrDenied, rawToolID)
	}
	return nil
}

func (a agentToolAuthorizer) validateUnavailableForGrant(grant agentgrant.Grant, target coreagent.ToolTarget, rawToolID string) error {
	if err := validateAgentRunGrantForToolTarget(grant, target, rawToolID); err != nil {
		return err
	}
	return a.validateUnavailable(grant.ToolRefs, target, rawToolID)
}

func validateAgentRunGrantForToolTarget(grant agentgrant.Grant, target coreagent.ToolTarget, rawToolID string) error {
	if err := validateAgentMCPCatalogGrant(grant.ToolSource, grant.ToolRefs); err != nil {
		return err
	}
	if len(grant.ToolRefs) == 0 || !agentToolMatchesRefs(target, grant.ToolRefs) {
		return fmt.Errorf("%w: agent tool %q is outside the turn tool scope", access.ErrDenied, rawToolID)
	}
	if target.CredentialMode != "" && !agentToolCredentialModeExplicitlyGranted(target, grant.ToolRefs, grant.Tools) {
		return fmt.Errorf("%w: agent tool %q credential mode was not granted to this turn", access.ErrDenied, rawToolID)
	}
	return nil
}

func validateAgentMCPCatalogGrant(sourceMode coreagent.ToolSourceMode, refs []coreagent.ToolRef) error {
	source := normalizeAgentToolSource(sourceMode)
	if source != coreagent.ToolSourceModeMCPCatalog {
		return fmt.Errorf("%w: unsupported agent tool source %q", invocation.ErrInternal, sourceMode)
	}
	if err := validateAgentMCPCatalogToolRefs(refs); err != nil {
		return fmt.Errorf("%w: %v", access.ErrDenied, err)
	}
	return nil
}

func (a agentToolAuthorizer) validateListedUnavailable(refs []coreagent.ToolRef, target coreagent.ToolTarget, rawToolID string) error {
	if len(refs) == 0 || !agentToolMatchesRefs(target, refs) {
		return fmt.Errorf("%w: listed agent tool %q is outside the turn tool scope", access.ErrDenied, rawToolID)
	}
	return a.validateUnavailable(refs, target, rawToolID)
}

func (a agentToolAuthorizer) validateUnavailable(refs []coreagent.ToolRef, target coreagent.ToolTarget, rawToolID string) error {
	if a.principal == nil {
		return fmt.Errorf("%w: agent execution principal is required", invocation.ErrInternal)
	}
	if target.Unavailable == nil || strings.TrimSpace(target.Unavailable.Reason) == "" {
		return fmt.Errorf("%w: unavailable agent tool %q is incomplete", access.ErrDenied, rawToolID)
	}
	if strings.TrimSpace(target.System) != "" || strings.TrimSpace(target.Operation) != "" {
		return fmt.Errorf("%w: unavailable agent tool %q cannot target a concrete operation", access.ErrDenied, rawToolID)
	}
	appName := strings.TrimSpace(target.App)
	if appName == "" {
		return fmt.Errorf("%w: unavailable agent tool %q app is required", access.ErrDenied, rawToolID)
	}
	if err := a.enforcer.Require(a.ctx, a.principal, access.Provider(appName)); err != nil {
		return fmt.Errorf("%w: unavailable agent tool %q is not authorized", err, rawToolID)
	}
	if !agentUnavailableReasonAllowed(strings.TrimSpace(target.Unavailable.Reason)) {
		return fmt.Errorf("%w: unavailable agent tool %q reason is invalid", access.ErrDenied, rawToolID)
	}
	if len(refs) > 0 && !agentToolMatchesRefs(target, refs) {
		return fmt.Errorf("%w: unavailable agent tool %q is outside the turn tool scope", access.ErrDenied, rawToolID)
	}
	return nil
}

func (a agentToolAuthorizer) validateListed(refs []coreagent.ToolRef, source coreagent.ToolSourceMode, tools []coreagent.ListedTool) error {
	if source != coreagent.ToolSourceModeMCPCatalog {
		return fmt.Errorf("%w: unsupported agent tool source %q", invocation.ErrInternal, source)
	}
	for i := range tools {
		if strings.TrimSpace(tools[i].ToolID) == "" {
			return fmt.Errorf("%w: listed agent tool id is required", access.ErrDenied)
		}
		if strings.TrimSpace(tools[i].MCPName) == "" {
			return fmt.Errorf("%w: listed agent tool mcp_name is required", access.ErrDenied)
		}
		target := tools[i].Target
		if target.Unavailable != nil {
			if err := a.validateListedUnavailable(refs, target, tools[i].ToolID); err != nil {
				return err
			}
			continue
		}
		appTarget, err := a.validateConcreteToolTarget(target, refs, tools[i].ToolID, "listed agent")
		if err != nil {
			return err
		}
		if !appTarget {
			continue
		}
		if tools[i].Hidden && !agentToolHiddenExplicitlyGranted(target, tools[i].ToolID, refs, nil) {
			return fmt.Errorf("%w: listed hidden agent tool %q was not explicitly granted", access.ErrDenied, tools[i].ToolID)
		}
		if target.RunAs != nil && !agentToolRunAsExplicitlyGranted(target, refs, nil) {
			return fmt.Errorf("%w: listed agent tool %q runAs delegation was not explicitly granted", access.ErrDenied, tools[i].ToolID)
		}
	}
	return nil
}

func (a agentToolAuthorizer) validateConcreteToolTarget(target coreagent.ToolTarget, refs []coreagent.ToolRef, rawToolID, label string) (bool, error) {
	operation := strings.TrimSpace(target.Operation)
	if systemName := strings.TrimSpace(target.System); systemName != "" {
		if systemName != coreagent.SystemToolWorkflow || operation == "" {
			return false, fmt.Errorf("%w: %s system tool target is incomplete", access.ErrDenied, label)
		}
		if !agentToolMatchesRefs(target, refs) {
			return false, fmt.Errorf("%w: %s tool %q is outside the turn tool scope", access.ErrDenied, label, rawToolID)
		}
		return false, nil
	}
	appName := strings.TrimSpace(target.App)
	if appName == "" || operation == "" {
		return false, fmt.Errorf("%w: %s tool target is incomplete", access.ErrDenied, label)
	}
	if err := a.enforcer.Require(a.ctx, a.principal, access.AppOperation(appName, operation)); err != nil {
		return true, fmt.Errorf("%w: %s tool %q is not authorized", err, label, rawToolID)
	}
	if len(refs) > 0 && !agentToolMatchesRefs(target, refs) {
		return true, fmt.Errorf("%w: %s tool %q is outside the turn tool scope", access.ErrDenied, label, rawToolID)
	}
	return true, nil
}
