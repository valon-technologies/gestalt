package bootstrap

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/workflowwire"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

func workflowSystemToolFromRef(ref coreagent.ToolRef) (coreagent.Tool, error) {
	systemName := strings.TrimSpace(ref.System)
	if systemName != coreagent.SystemToolWorkflow {
		return coreagent.Tool{}, fmt.Errorf("%w: unsupported agent system tool %q", invocation.ErrInvalidInvocation, systemName)
	}
	operation := strings.TrimSpace(ref.Operation)
	desc, ok := workflowSystemToolDescriptors[operation]
	if !ok {
		return coreagent.Tool{}, fmt.Errorf("%w: workflow system operation %q is not supported", invocation.ErrOperationNotFound, operation)
	}
	name := strings.TrimSpace(ref.Title)
	if name == "" {
		name = desc.Name
	}
	description := strings.TrimSpace(ref.Description)
	if description == "" {
		description = desc.Description
	}
	return coreagent.Tool{
		ID:               "system.workflow." + operation,
		Name:             name,
		Description:      description,
		ParametersSchema: workflowSystemToolMapDeepClone(desc.ParametersSchema),
		Target: coreagent.ToolTarget{
			System:    coreagent.SystemToolWorkflow,
			Operation: operation,
		},
	}, nil
}

func workflowSystemToolJSONResponse(status int, value any) (*coreagent.ExecuteToolResponse, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal workflow tool response: %v", invocation.ErrInternal, err)
	}
	return &coreagent.ExecuteToolResponse{Status: status, Body: string(body)}, nil
}

func workflowSystemToolWireError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, workflowwire.ErrInvalid) {
		return fmt.Errorf("%w: %v", invocation.ErrInvalidInvocation, err)
	}
	return err
}

func workflowSystemToolTargetFromValue(value any) (coreworkflow.Target, error) {
	target, err := workflowwire.ParseTargetMap(value, "target")
	if err != nil {
		return coreworkflow.Target{}, workflowSystemToolWireError(err)
	}
	return target, nil
}

func workflowSystemToolInheritAgentToolRefs(req agentSystemToolExecutionRequest, target *coreworkflow.Target) {
	if target == nil {
		return
	}
	for i := range target.Steps {
		if target.Steps[i].Agent != nil && target.Steps[i].Agent.ToolRefs == nil {
			target.Steps[i].Agent.ToolRefs = workflowSystemToolInheritedAgentToolRefs(req)
		}
	}
}

func workflowSystemToolInheritedAgentToolRefs(req agentSystemToolExecutionRequest) []coreagent.ToolRef {
	out := []coreagent.ToolRef{}
	seen := map[string]struct{}{}
	add := func(ref coreagent.ToolRef) {
		ref.System = strings.TrimSpace(ref.System)
		ref.App = strings.TrimSpace(ref.App)
		ref.Operation = strings.TrimSpace(ref.Operation)
		ref.Connection = strings.TrimSpace(ref.Connection)
		ref.Instance = strings.TrimSpace(ref.Instance)
		ref.CredentialMode = core.NormalizeOptionalConnectionMode(ref.CredentialMode)
		if ref.System != "" {
			if ref.System != coreagent.SystemToolWorkflow || ref.Operation == "" {
				return
			}
			if ref.App != "" || ref.Connection != "" || ref.Instance != "" || ref.CredentialMode != "" || ref.RunAs != nil {
				return
			}
		} else if ref.App == "" || ref.App == "*" || ref.Operation == "" {
			return
		}
		key := strings.Join([]string{ref.System, ref.App, ref.Operation, ref.Connection, ref.Instance, string(ref.CredentialMode)}, "\x00")
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		inherited := coreagent.ToolRef{
			System:         ref.System,
			App:            ref.App,
			Operation:      ref.Operation,
			Connection:     ref.Connection,
			Instance:       ref.Instance,
			CredentialMode: ref.CredentialMode,
		}
		out = append(out, inherited)
	}
	for i := range req.ToolRefs {
		add(req.ToolRefs[i])
	}
	for i := range req.Tools {
		target := req.Tools[i].Target
		if strings.TrimSpace(target.System) != "" {
			add(coreagent.ToolRef{
				System:    target.System,
				Operation: target.Operation,
			})
			continue
		}
		add(coreagent.ToolRef{
			App:            target.App,
			Operation:      target.Operation,
			Connection:     target.Connection,
			Instance:       target.Instance,
			CredentialMode: target.CredentialMode,
		})
	}
	return out
}

func workflowSystemToolValidateCreateScope(req agentSystemToolExecutionRequest, target coreworkflow.Target) error {
	if len(target.Steps) == 0 {
		return fmt.Errorf("%w: workflow target is required", invocation.ErrInvalidInvocation)
	}
	for stepIndex := range target.Steps {
		step := target.Steps[stepIndex]
		stepPath := fmt.Sprintf("target.steps[%d]", stepIndex)
		if step.App != nil && !workflowSystemToolAppStepAllowed(*step.App, req.ToolRefs, req.Tools) {
			return fmt.Errorf("%w: %s.app %s.%s is outside the current agent tool scope", invocation.ErrScopeDenied, stepPath, step.App.Name, step.App.Operation)
		}
		if step.Agent == nil {
			continue
		}
		for i := range step.Agent.ToolRefs {
			ref := step.Agent.ToolRefs[i]
			if strings.TrimSpace(ref.System) != "" {
				refPath := fmt.Sprintf("%s.agent.tools[%d]", stepPath, i)
				if err := workflowSystemToolValidateFutureSystemRef(refPath, ref, req); err != nil {
					return err
				}
				continue
			}
			if strings.TrimSpace(ref.App) == "" || strings.TrimSpace(ref.App) == "*" || strings.TrimSpace(ref.Operation) == "" {
				return fmt.Errorf("%w: %s.agent.tools[%d] must be an exact app operation", invocation.ErrInvalidInvocation, stepPath, i)
			}
			if ref.RunAs != nil {
				return fmt.Errorf("%w: %s.agent.tools[%d] runAs is not supported for workflow agent tools", invocation.ErrInvalidInvocation, stepPath, i)
			}
			if !workflowSystemToolAgentAppRefAllowed(ref, req.ToolRefs, req.Tools) {
				return fmt.Errorf("%w: %s.agent.tools[%d] %s.%s is outside the current agent tool scope", invocation.ErrScopeDenied, stepPath, i, ref.App, ref.Operation)
			}
		}
	}
	return nil
}

func workflowSystemToolValidateFutureSystemRef(path string, ref coreagent.ToolRef, req agentSystemToolExecutionRequest) error {
	if strings.TrimSpace(ref.System) != coreagent.SystemToolWorkflow || strings.TrimSpace(ref.Operation) == "" {
		return fmt.Errorf("%w: %s workflow system refs require an exact operation", invocation.ErrInvalidInvocation, path)
	}
	if strings.TrimSpace(ref.App) != "" || strings.TrimSpace(ref.Connection) != "" || strings.TrimSpace(ref.Instance) != "" || ref.CredentialMode != "" || ref.RunAs != nil {
		return fmt.Errorf("%w: %s system refs cannot include app, connection, instance, credentialMode, or runAs", invocation.ErrInvalidInvocation, path)
	}
	for i := range req.ToolRefs {
		if strings.TrimSpace(req.ToolRefs[i].System) == coreagent.SystemToolWorkflow && strings.TrimSpace(req.ToolRefs[i].Operation) == strings.TrimSpace(ref.Operation) {
			return nil
		}
	}
	for i := range req.Tools {
		if strings.TrimSpace(req.Tools[i].Target.System) == coreagent.SystemToolWorkflow && strings.TrimSpace(req.Tools[i].Target.Operation) == strings.TrimSpace(ref.Operation) {
			return nil
		}
	}
	return fmt.Errorf("%w: %s workflow.%s is outside the current agent tool scope", invocation.ErrScopeDenied, path, ref.Operation)
}

func workflowSystemToolAgentAppRefAllowed(target coreagent.ToolRef, refs []coreagent.ToolRef, tools []coreagent.Tool) bool {
	for i := range refs {
		if workflowSystemToolAppRefMatchesAgentRef(refs[i], target) {
			return true
		}
	}
	for i := range tools {
		if workflowSystemToolResolvedToolMatchesAgentRef(tools[i], target) {
			return true
		}
	}
	return false
}

func workflowSystemToolAppStepAllowed(target coreworkflow.AppCall, refs []coreagent.ToolRef, tools []coreagent.Tool) bool {
	for i := range refs {
		if workflowSystemToolAppRefMatchesTarget(refs[i], target) {
			return true
		}
	}
	for i := range tools {
		if workflowSystemToolResolvedToolMatchesTarget(tools[i], target) {
			return true
		}
	}
	return false
}

func workflowSystemToolAppRefMatchesTarget(ref coreagent.ToolRef, target coreworkflow.AppCall) bool {
	if strings.TrimSpace(ref.System) != "" || strings.TrimSpace(ref.App) == "" || strings.TrimSpace(ref.App) == "*" || strings.TrimSpace(ref.Operation) == "" {
		return false
	}
	if strings.TrimSpace(ref.App) != strings.TrimSpace(target.Name) || strings.TrimSpace(ref.Operation) != strings.TrimSpace(target.Operation) {
		return false
	}
	if !workflowSystemToolCredentialModeMatches(ref.CredentialMode, target.CredentialMode) {
		return false
	}
	return workflowSystemToolRefBindingMatchesTarget(ref.Connection, ref.Instance, target.Connection, target.Instance)
}

func workflowSystemToolAppRefMatchesAgentRef(ref coreagent.ToolRef, target coreagent.ToolRef) bool {
	if strings.TrimSpace(ref.System) != "" || strings.TrimSpace(ref.App) == "" || strings.TrimSpace(ref.App) == "*" || strings.TrimSpace(ref.Operation) == "" {
		return false
	}
	if strings.TrimSpace(ref.App) != strings.TrimSpace(target.App) || strings.TrimSpace(ref.Operation) != strings.TrimSpace(target.Operation) {
		return false
	}
	if !workflowSystemToolCredentialModeMatches(ref.CredentialMode, target.CredentialMode) {
		return false
	}
	return workflowSystemToolRefBindingMatchesTarget(ref.Connection, ref.Instance, target.Connection, target.Instance)
}

func workflowSystemToolResolvedToolMatchesTarget(tool coreagent.Tool, target coreworkflow.AppCall) bool {
	if strings.TrimSpace(tool.Target.System) != "" || strings.TrimSpace(tool.Target.App) == "" || strings.TrimSpace(tool.Target.Operation) == "" {
		return false
	}
	if strings.TrimSpace(tool.Target.App) != strings.TrimSpace(target.Name) || strings.TrimSpace(tool.Target.Operation) != strings.TrimSpace(target.Operation) {
		return false
	}
	if !workflowSystemToolCredentialModeMatches(tool.Target.CredentialMode, target.CredentialMode) {
		return false
	}
	return workflowSystemToolResolvedBindingMatchesTarget(tool.Target.Connection, tool.Target.Instance, target.Connection, target.Instance)
}

func workflowSystemToolResolvedToolMatchesAgentRef(tool coreagent.Tool, target coreagent.ToolRef) bool {
	if strings.TrimSpace(tool.Target.System) != "" || strings.TrimSpace(tool.Target.App) == "" || strings.TrimSpace(tool.Target.Operation) == "" {
		return false
	}
	if strings.TrimSpace(tool.Target.App) != strings.TrimSpace(target.App) || strings.TrimSpace(tool.Target.Operation) != strings.TrimSpace(target.Operation) {
		return false
	}
	if !workflowSystemToolCredentialModeMatches(tool.Target.CredentialMode, target.CredentialMode) {
		return false
	}
	return workflowSystemToolResolvedBindingMatchesTarget(tool.Target.Connection, tool.Target.Instance, target.Connection, target.Instance)
}

func workflowSystemToolCredentialModeMatches(scope, target core.ConnectionMode) bool {
	scope = core.NormalizeOptionalConnectionMode(scope)
	target = core.NormalizeOptionalConnectionMode(target)
	if scope == "" && target == "" {
		return true
	}
	return scope == target
}

func workflowSystemToolRefBindingMatchesTarget(scopeConnection, scopeInstance, targetConnection, targetInstance string) bool {
	scopeConnection = config.ResolveConnectionAlias(strings.TrimSpace(scopeConnection))
	targetConnection = config.ResolveConnectionAlias(strings.TrimSpace(targetConnection))
	if scopeConnection != "" && scopeConnection != targetConnection {
		return false
	}
	if scopeInstance = strings.TrimSpace(scopeInstance); scopeInstance != "" && scopeInstance != strings.TrimSpace(targetInstance) {
		return false
	}
	return true
}

func workflowSystemToolResolvedBindingMatchesTarget(scopeConnection, scopeInstance, targetConnection, targetInstance string) bool {
	scopeConnection = config.ResolveConnectionAlias(strings.TrimSpace(scopeConnection))
	targetConnection = config.ResolveConnectionAlias(strings.TrimSpace(targetConnection))
	if scopeConnection != targetConnection {
		return false
	}
	if strings.TrimSpace(scopeInstance) != strings.TrimSpace(targetInstance) {
		return false
	}
	return true
}

func workflowSystemToolPermissionsForTarget(target coreworkflow.Target, defaultAgentProvider string) []core.AccessPermission {
	operationsByApp := map[string]map[string]struct{}{}
	addOperation := func(appName, operation string) {
		appName = strings.TrimSpace(appName)
		operation = strings.TrimSpace(operation)
		if appName == "" || operation == "" {
			return
		}
		if ops, ok := operationsByApp[appName]; ok && ops == nil {
			return
		}
		if operationsByApp[appName] == nil {
			operationsByApp[appName] = map[string]struct{}{}
		}
		operationsByApp[appName][operation] = struct{}{}
	}
	addProvider := func(providerName string) {
		providerName = strings.TrimSpace(providerName)
		if providerName == "" {
			return
		}
		if _, ok := operationsByApp[providerName]; !ok {
			operationsByApp[providerName] = nil
		}
	}
	for i := range target.Steps {
		step := target.Steps[i]
		if step.App != nil {
			addOperation(step.App.Name, step.App.Operation)
		}
		if step.Agent == nil {
			continue
		}
		agentProvider := strings.TrimSpace(step.Agent.ProviderName)
		if agentProvider == "" {
			agentProvider = strings.TrimSpace(defaultAgentProvider)
		}
		addProvider(agentProvider)
		for j := range step.Agent.ToolRefs {
			ref := step.Agent.ToolRefs[j]
			if strings.TrimSpace(ref.System) == "" {
				addOperation(ref.App, ref.Operation)
			}
		}
	}
	if len(operationsByApp) == 0 {
		return []core.AccessPermission{}
	}
	apps := slices.Sorted(maps.Keys(operationsByApp))
	out := make([]core.AccessPermission, 0, len(apps))
	for _, appName := range apps {
		operations := slices.Sorted(maps.Keys(operationsByApp[appName]))
		out = append(out, core.AccessPermission{App: appName, Operations: operations})
	}
	return out
}

func workflowSystemToolTrustedAgentProvider(req agentSystemToolExecutionRequest, target coreworkflow.Target) string {
	currentProvider := strings.TrimSpace(req.ProviderName)
	if currentProvider == "" {
		return ""
	}
	foundAgent := false
	for i := range target.Steps {
		if target.Steps[i].Agent == nil {
			continue
		}
		foundAgent = true
		targetProvider := strings.TrimSpace(target.Steps[i].Agent.ProviderName)
		if targetProvider != "" && targetProvider != currentProvider {
			return ""
		}
	}
	if !foundAgent {
		return ""
	}
	return currentProvider
}

func workflowSystemToolPrincipalWithTrustedProvider(p *principal.Principal, trustedProvider string) *principal.Principal {
	p = principal.Canonicalized(p)
	trustedProvider = strings.TrimSpace(trustedProvider)
	if p == nil || trustedProvider == "" || p.TokenPermissions == nil {
		return p
	}
	next := *p
	next.TokenPermissions = principal.ClonePermissionSet(p.TokenPermissions)
	next.TokenPermissions[trustedProvider] = nil
	next.Scopes = principal.PermissionApps(next.TokenPermissions)
	return principal.Canonicalize(&next)
}

func workflowSystemToolScopedPrincipal(p *principal.Principal, permissions []core.AccessPermission, trustedProvider string) (*principal.Principal, error) {
	return workflowSystemToolScopedPrincipalWithPermissions(p, permissions, trustedProvider, true)
}

func workflowSystemToolExactPermissionsPrincipal(p *principal.Principal, permissions []core.AccessPermission, trustedProvider string) (*principal.Principal, error) {
	return workflowSystemToolScopedPrincipalWithPermissions(p, permissions, trustedProvider, false)
}

func workflowSystemToolScopedPrincipalWithPermissions(p *principal.Principal, permissions []core.AccessPermission, trustedProvider string, requireWithinCaller bool) (*principal.Principal, error) {
	p = principal.Canonicalized(p)
	if p == nil || strings.TrimSpace(p.SubjectID) == "" {
		return nil, fmt.Errorf("%w: agent execution principal is required", invocation.ErrAuthorizationDenied)
	}
	if permissions == nil {
		return p, nil
	}
	requested := principal.CompilePermissions(permissions)
	if requested == nil {
		requested = principal.PermissionSet{}
	}
	if p.TokenPermissions != nil {
		requested = principal.IntersectPermissions(requested, p.TokenPermissions)
		if trustedProvider != "" {
			if requested == nil {
				requested = principal.PermissionSet{}
			}
			requested[trustedProvider] = nil
		}
		if requireWithinCaller && len(permissions) > 0 && len(requested) == 0 {
			return nil, fmt.Errorf("%w: workflow target is outside the caller permission scope", invocation.ErrScopeDenied)
		}
	}
	next := *p
	next.TokenPermissions = requested
	next.Scopes = principal.PermissionApps(requested)
	return principal.Canonicalize(&next), nil
}
