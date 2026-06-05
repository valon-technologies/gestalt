package bootstrap

import (
	"fmt"
	"strings"

	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/services/access"
	"github.com/valon-technologies/gestalt/server/services/agents/agentgrant"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

func validateAgentToolForGrant(grant agentgrant.Grant, target coreagent.ToolTarget, rawToolID string) error {
	if err := validateGrantScope(grant.ToolSource, grant.ToolRefs); err != nil {
		return err
	}
	if len(grant.ToolRefs) == 0 {
		return fmt.Errorf("%w: agent tool %q is outside the turn tool scope", access.ErrDenied, rawToolID)
	}
	if err := validateConcreteAgentToolTarget(target, grant.ToolRefs, rawToolID); err != nil {
		return err
	}
	if strings.TrimSpace(target.System) != "" {
		return nil
	}
	if err := validateGrantedCredentialMode(target, grant.ToolRefs, grant.Tools, rawToolID); err != nil {
		return err
	}
	if target.RunAs != nil && !agentToolRunAsExplicitlyGranted(target, grant.ToolRefs, grant.Tools) {
		return fmt.Errorf("%w: agent tool %q runAs delegation was not granted to this turn", access.ErrDenied, rawToolID)
	}
	return nil
}

func validateUnavailableAgentToolForGrant(grant agentgrant.Grant, target coreagent.ToolTarget, rawToolID string) error {
	if err := validateGrantScope(grant.ToolSource, grant.ToolRefs); err != nil {
		return err
	}
	if err := validateGrantedCredentialMode(target, grant.ToolRefs, grant.Tools, rawToolID); err != nil {
		return err
	}
	return validateUnavailableAgentToolTarget(grant.ToolRefs, target, rawToolID)
}

func validateGrantScope(sourceMode coreagent.ToolSourceMode, refs []coreagent.ToolRef) error {
	source := normalizeAgentToolSource(sourceMode)
	if source != coreagent.ToolSourceModeMCPCatalog {
		return fmt.Errorf("%w: unsupported agent tool source %q", invocation.ErrInternal, sourceMode)
	}
	if err := validateAgentMCPCatalogToolRefs(refs); err != nil {
		return fmt.Errorf("%w: %v", access.ErrDenied, err)
	}
	return nil
}

func validateUnavailableAgentToolTarget(refs []coreagent.ToolRef, target coreagent.ToolTarget, rawToolID string) error {
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
	if !agentUnavailableReasonAllowed(strings.TrimSpace(target.Unavailable.Reason)) {
		return fmt.Errorf("%w: unavailable agent tool %q reason is invalid", access.ErrDenied, rawToolID)
	}
	if len(refs) == 0 || !agentToolMatchesRefs(target, refs) {
		return fmt.Errorf("%w: unavailable agent tool %q is outside the turn tool scope", access.ErrDenied, rawToolID)
	}
	return nil
}

func validateListedAgentTools(refs []coreagent.ToolRef, source coreagent.ToolSourceMode, tools []coreagent.ListedTool) error {
	if err := validateGrantScope(source, refs); err != nil {
		return err
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
			if err := validateUnavailableAgentToolTarget(refs, target, tools[i].ToolID); err != nil {
				return err
			}
			continue
		}
		if err := validateConcreteAgentToolTarget(target, refs, tools[i].ToolID); err != nil {
			return err
		}
		if strings.TrimSpace(target.System) != "" {
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

func validateConcreteAgentToolTarget(target coreagent.ToolTarget, refs []coreagent.ToolRef, rawToolID string) error {
	operation := strings.TrimSpace(target.Operation)
	if systemName := strings.TrimSpace(target.System); systemName != "" {
		if systemName != coreagent.SystemToolWorkflow || operation == "" {
			return fmt.Errorf("%w: agent system tool target is incomplete", access.ErrDenied)
		}
		if !agentToolMatchesRefs(target, refs) {
			return fmt.Errorf("%w: agent tool %q is outside the turn tool scope", access.ErrDenied, rawToolID)
		}
		return nil
	}
	appName := strings.TrimSpace(target.App)
	if appName == "" || operation == "" {
		return fmt.Errorf("%w: agent tool target is incomplete", access.ErrDenied)
	}
	if len(refs) > 0 && !agentToolMatchesRefs(target, refs) {
		return fmt.Errorf("%w: agent tool %q is outside the turn tool scope", access.ErrDenied, rawToolID)
	}
	return nil
}

func validateGrantedCredentialMode(target coreagent.ToolTarget, refs []coreagent.ToolRef, tools []coreagent.Tool, rawToolID string) error {
	if target.CredentialMode != "" && !agentToolCredentialModeExplicitlyGranted(target, refs, tools) {
		return fmt.Errorf("%w: agent tool %q credential mode was not granted to this turn", access.ErrDenied, rawToolID)
	}
	return nil
}
