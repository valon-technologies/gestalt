package agentmanager

import (
	"context"
	"strconv"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/agents/agentturnscope"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type agentInvocationScopeRequest struct {
	AgentProviderName string
	CallerKind        invocation.ProviderKind
	CallerName        string
	Agent             invocation.AgentInvocationContext
	Principal         *principal.Principal
	RequestContext    *proto.RequestContext
}

func (m *Manager) AuthorizeAgentAppInvocation(ctx context.Context, req appaccessservice.AgentAppInvocationAuthorizationRequest) (appaccessservice.AgentAppInvocationAuthorization, error) {
	scope, err := m.authorizeAgentInvocationScope(ctx, agentInvocationScopeRequest{
		AgentProviderName: req.AgentProviderName,
		CallerKind:        req.CallerKind,
		CallerName:        req.CallerName,
		Agent:             req.Agent,
		Principal:         req.Principal,
		RequestContext:    req.RequestContext,
	})
	if err != nil {
		return appaccessservice.AgentAppInvocationAuthorization{}, err
	}
	tool, ok := matchingAgentAppInvocationTool(scope.ListedTools, req)
	if !ok {
		return appaccessservice.AgentAppInvocationAuthorization{}, status.Errorf(codes.PermissionDenied, "agent turn may not invoke %s.%s", strings.TrimSpace(req.App), strings.TrimSpace(req.Operation))
	}
	p := agentScopePrincipal(scope)
	if p == nil || strings.TrimSpace(p.SubjectID) == "" {
		return appaccessservice.AgentAppInvocationAuthorization{}, status.Error(codes.FailedPrecondition, "agent turn scope subject is required")
	}
	return appaccessservice.AgentAppInvocationAuthorization{
		Principal:      p,
		CredentialMode: core.NormalizeOptionalConnectionMode(tool.Target.CredentialMode),
		Connection:     core.ResolveConnectionAlias(strings.TrimSpace(tool.Target.Connection)),
		Instance:       strings.TrimSpace(tool.Target.Instance),
		RunAs:          core.NormalizeRunAsSubject(tool.Target.RunAs),
		ToolRefs:       append([]coreagent.ToolRef(nil), scope.ToolRefs...),
		ToolRefsSet:    scope.ToolRefsSet,
	}, nil
}

func (m *Manager) AuthorizeAgentWorkflowInvocation(ctx context.Context, req appaccessservice.AgentWorkflowInvocationAuthorizationRequest) (appaccessservice.AgentWorkflowInvocationAuthorization, error) {
	scope, err := m.authorizeAgentInvocationScope(ctx, agentInvocationScopeRequest{
		AgentProviderName: req.AgentProviderName,
		CallerKind:        req.CallerKind,
		CallerName:        req.CallerName,
		Agent:             req.Agent,
		Principal:         req.Principal,
		RequestContext:    req.RequestContext,
	})
	if err != nil {
		return appaccessservice.AgentWorkflowInvocationAuthorization{}, err
	}
	if operation := strings.TrimSpace(req.Operation); operation != "" && !agentWorkflowOperationAllowed(scope, operation) {
		return appaccessservice.AgentWorkflowInvocationAuthorization{}, status.Errorf(codes.PermissionDenied, "agent turn may not invoke workflow.%s", operation)
	}
	var target *coreworkflow.Target
	if req.Target != nil {
		copied := *req.Target
		target = &copied
		if err := validateAgentWorkflowTargetScope(scope, copied); err != nil {
			return appaccessservice.AgentWorkflowInvocationAuthorization{}, err
		}
	}
	p := agentScopePrincipal(scope)
	if p == nil || strings.TrimSpace(p.SubjectID) == "" {
		return appaccessservice.AgentWorkflowInvocationAuthorization{}, status.Error(codes.FailedPrecondition, "agent turn scope subject is required")
	}
	if target != nil {
		p = agentWorkflowPrincipalForTarget(p, scope, *target)
	}
	return appaccessservice.AgentWorkflowInvocationAuthorization{Principal: p}, nil
}

func (m *Manager) authorizeAgentInvocationScope(ctx context.Context, req agentInvocationScopeRequest) (agentturnscope.Scope, error) {
	if m == nil || m.turnScopes == nil {
		return agentturnscope.Scope{}, status.Error(codes.FailedPrecondition, "agent turn scopes are not configured")
	}
	agent := req.Agent
	if strings.TrimSpace(agent.ProviderName) == "" || strings.TrimSpace(agent.SessionID) == "" || strings.TrimSpace(agent.TurnID) == "" {
		return agentturnscope.Scope{}, status.Error(codes.FailedPrecondition, "agent invocation context is required")
	}
	scope, ok := m.turnScopes.Get(agent.ProviderName, agent.SessionID, agent.TurnID)
	if !ok {
		return agentturnscope.Scope{}, status.Error(codes.PermissionDenied, "agent turn scope was not found")
	}
	if scope.Revoked {
		return agentturnscope.Scope{}, status.Error(codes.PermissionDenied, "agent turn scope is revoked")
	}
	requestedTurnID := strings.TrimSpace(agent.TurnID)
	if agentProviderName := strings.TrimSpace(req.AgentProviderName); agentProviderName == "" || agentProviderName != strings.TrimSpace(scope.ProviderName) {
		return agentturnscope.Scope{}, status.Error(codes.PermissionDenied, "agent provider does not match turn scope")
	}
	if err := validateAgentInvocationCaller(scope, req, requestedTurnID); err != nil {
		return agentturnscope.Scope{}, err
	}
	if err := validateAgentInvocationSubject(scope, req.Principal); err != nil {
		return agentturnscope.Scope{}, err
	}
	if err := m.validateAgentInvocationTurn(ctx, scope, requestedTurnID, req.RequestContext); err != nil {
		return agentturnscope.Scope{}, err
	}
	return scope, nil
}

func validateAgentInvocationCaller(scope agentturnscope.Scope, req agentInvocationScopeRequest, requestedTurnID string) error {
	if strings.TrimSpace(scope.ProviderName) != strings.TrimSpace(req.Agent.ProviderName) ||
		strings.TrimSpace(scope.SessionID) != strings.TrimSpace(req.Agent.SessionID) ||
		requestedTurnID == "" {
		return status.Error(codes.PermissionDenied, "agent invocation context does not match turn scope")
	}
	if scope.CallerKind == "" || strings.TrimSpace(scope.CallerName) == "" {
		return status.Error(codes.FailedPrecondition, "agent turn scope caller context is required")
	}
	if scope.CallerKind != req.CallerKind || strings.TrimSpace(scope.CallerName) != strings.TrimSpace(req.CallerName) {
		return status.Error(codes.PermissionDenied, "agent invocation caller does not match turn scope")
	}
	return nil
}

func validateAgentInvocationSubject(scope agentturnscope.Scope, p *principal.Principal) error {
	p = principal.Canonicalized(p)
	if p == nil || strings.TrimSpace(p.SubjectID) == "" {
		return status.Error(codes.PermissionDenied, "agent request context subject is required")
	}
	if strings.TrimSpace(p.SubjectID) != strings.TrimSpace(scope.SubjectID) {
		return status.Error(codes.PermissionDenied, "agent request context subject does not match turn scope")
	}
	if credentialSubjectID := strings.TrimSpace(scope.CredentialSubjectID); credentialSubjectID != "" && strings.TrimSpace(principal.EffectiveCredentialSubjectID(p)) != credentialSubjectID {
		return status.Error(codes.PermissionDenied, "agent request context credential subject does not match turn scope")
	}
	return nil
}

func (m *Manager) validateAgentInvocationTurn(ctx context.Context, scope agentturnscope.Scope, requestedTurnID string, reqCtx *proto.RequestContext) error {
	_, provider, err := m.resolveProvider(ctx, scope.ProviderName)
	if err != nil {
		return status.Errorf(codes.PermissionDenied, "agent provider %q is not available for turn scope", strings.TrimSpace(scope.ProviderName))
	}
	turn, err := m.getAgentInvocationTurn(ctx, provider, scope, requestedTurnID, reqCtx)
	if err != nil {
		return err
	}
	if turn == nil {
		return status.Errorf(codes.PermissionDenied, "agent turn %q was not found", strings.TrimSpace(requestedTurnID))
	}
	return validateAgentInvocationReturnedTurn(scope, requestedTurnID, turn)
}

func (m *Manager) getAgentInvocationTurn(ctx context.Context, provider coreagent.Provider, scope agentturnscope.Scope, requestedTurnID string, reqCtx *proto.RequestContext) (*coreagent.Turn, error) {
	ids := []string{strings.TrimSpace(scope.ProviderTurnID), strings.TrimSpace(requestedTurnID), strings.TrimSpace(scope.TurnID)}
	var sawNotFound bool
	seen := map[string]struct{}{}
	for _, turnID := range ids {
		turnID = strings.TrimSpace(turnID)
		if turnID == "" {
			continue
		}
		if _, ok := seen[turnID]; ok {
			continue
		}
		seen[turnID] = struct{}{}
		turn, err := provider.GetTurn(ctx, &proto.GetAgentProviderTurnRequest{
			TurnId:       turnID,
			ProviderName: strings.TrimSpace(scope.ProviderName),
			Context:      reqCtx,
			Subject: &proto.SubjectContext{
				Id:                  strings.TrimSpace(scope.SubjectID),
				CredentialSubjectId: strings.TrimSpace(scope.CredentialSubjectID),
			},
		})
		if err != nil {
			if agentProviderReturnedNotFound(err) {
				sawNotFound = true
				continue
			}
			return nil, err
		}
		return turn, nil
	}
	if sawNotFound {
		return nil, status.Errorf(codes.PermissionDenied, "agent turn %q was not found", strings.TrimSpace(requestedTurnID))
	}
	return nil, status.Error(codes.PermissionDenied, "agent turn was not found")
}

func validateAgentInvocationReturnedTurn(scope agentturnscope.Scope, requestedTurnID string, turn *coreagent.Turn) error {
	scopeTurnID := strings.TrimSpace(scope.TurnID)
	requestedTurnID = strings.TrimSpace(requestedTurnID)
	returnedTurnID := strings.TrimSpace(turn.ID)
	executionRef := strings.TrimSpace(turn.ExecutionRef)
	if !agentInvocationTurnIDMatches(returnedTurnID, scopeTurnID, requestedTurnID) && !agentInvocationTurnIDMatches(executionRef, scopeTurnID, requestedTurnID) {
		return status.Errorf(codes.PermissionDenied, "agent provider returned turn %q for requested turn %q", returnedTurnID, requestedTurnID)
	}
	if strings.TrimSpace(turn.SessionID) != strings.TrimSpace(scope.SessionID) {
		return status.Errorf(codes.PermissionDenied, "agent turn scope is not valid for session %q", strings.TrimSpace(scope.SessionID))
	}
	if !coreagent.ExecutionStatusIsLive(turn.Status) {
		return status.Errorf(codes.PermissionDenied, "agent turn %q is not active", scopeTurnID)
	}
	return nil
}

func agentInvocationTurnIDMatches(value, scopeTurnID, requestedTurnID string) bool {
	value = strings.TrimSpace(value)
	return value != "" && (value == strings.TrimSpace(scopeTurnID) || value == strings.TrimSpace(requestedTurnID))
}

func matchingAgentAppInvocationTool(tools []coreagent.ListedTool, req appaccessservice.AgentAppInvocationAuthorizationRequest) (coreagent.ListedTool, bool) {
	targetApp := strings.TrimSpace(req.App)
	targetOperation := strings.TrimSpace(req.Operation)
	targetConnection := core.ResolveConnectionAlias(strings.TrimSpace(req.Connection))
	targetInstance := strings.TrimSpace(req.Instance)
	targetCredentialMode := core.NormalizeOptionalConnectionMode(req.CredentialMode)
	var matched coreagent.ListedTool
	var found bool
	for i := range tools {
		tool := tools[i]
		target := tool.Target
		if target.Unavailable != nil || strings.TrimSpace(target.System) != "" {
			continue
		}
		if strings.TrimSpace(target.App) != targetApp || strings.TrimSpace(target.Operation) != targetOperation {
			continue
		}
		if targetConnection != "" && core.ResolveConnectionAlias(strings.TrimSpace(target.Connection)) != targetConnection {
			continue
		}
		if targetInstance != "" && strings.TrimSpace(target.Instance) != targetInstance {
			continue
		}
		if targetCredentialMode != "" && core.NormalizeOptionalConnectionMode(target.CredentialMode) != targetCredentialMode {
			continue
		}
		if found {
			return coreagent.ListedTool{}, false
		}
		matched = tool
		found = true
	}
	if found {
		return matched, true
	}
	return coreagent.ListedTool{}, false
}

func agentWorkflowOperationAllowed(scope agentturnscope.Scope, operation string) bool {
	operation = strings.TrimSpace(operation)
	if operation == "" {
		return false
	}
	for i := range scope.ToolRefs {
		if strings.TrimSpace(scope.ToolRefs[i].System) == coreagent.SystemToolWorkflow && strings.TrimSpace(scope.ToolRefs[i].Operation) == operation {
			return true
		}
	}
	for i := range scope.ListedTools {
		if workflowToolTargetMatches(scope.ListedTools[i].Target, operation) {
			return true
		}
	}
	for i := range scope.Tools {
		if workflowToolTargetMatches(scope.Tools[i].Target, operation) {
			return true
		}
	}
	return false
}

func workflowToolTargetMatches(target coreagent.ToolTarget, operation string) bool {
	return strings.TrimSpace(target.System) == coreagent.SystemToolWorkflow && strings.TrimSpace(target.Operation) == strings.TrimSpace(operation)
}

func validateAgentWorkflowTargetScope(scope agentturnscope.Scope, target coreworkflow.Target) error {
	for stepIndex := range target.Steps {
		step := target.Steps[stepIndex]
		stepPath := "workflow target.steps[" + strconv.Itoa(stepIndex) + "]"
		if step.App != nil && !agentWorkflowAppStepAllowed(scope, *step.App) {
			return status.Errorf(codes.PermissionDenied, "%s.app %s.%s is outside the current agent tool scope", stepPath, step.App.Name, step.App.Operation)
		}
		if step.Agent == nil {
			continue
		}
		for i := range step.Agent.ToolRefs {
			ref := step.Agent.ToolRefs[i]
			refPath := stepPath + ".agent.tool_refs[" + strconv.Itoa(i) + "]"
			if strings.TrimSpace(ref.System) != "" {
				if err := validateAgentWorkflowSystemRef(scope, refPath, ref); err != nil {
					return err
				}
				continue
			}
			if strings.TrimSpace(ref.App) == "" || strings.TrimSpace(ref.App) == agentToolSearchAllApp || strings.TrimSpace(ref.Operation) == "" {
				return status.Errorf(codes.InvalidArgument, "%s must be an exact app operation", refPath)
			}
			if ref.RunAs != nil {
				return status.Errorf(codes.PermissionDenied, "%s runAs is not supported for workflow agent tools", refPath)
			}
			if !agentWorkflowAppRefAllowed(scope, ref) {
				return status.Errorf(codes.PermissionDenied, "%s %s.%s is outside the current agent tool scope", refPath, ref.App, ref.Operation)
			}
		}
	}
	return nil
}

func validateAgentWorkflowSystemRef(scope agentturnscope.Scope, path string, ref coreagent.ToolRef) error {
	if strings.TrimSpace(ref.System) != coreagent.SystemToolWorkflow || strings.TrimSpace(ref.Operation) == "" {
		return status.Errorf(codes.InvalidArgument, "%s workflow system refs require an exact operation", path)
	}
	if strings.TrimSpace(ref.App) != "" || strings.TrimSpace(ref.Connection) != "" || strings.TrimSpace(ref.Instance) != "" || ref.CredentialMode != "" || ref.RunAs != nil {
		return status.Errorf(codes.InvalidArgument, "%s system refs cannot include app, connection, instance, credentialMode, or runAs", path)
	}
	if !agentWorkflowOperationAllowed(scope, ref.Operation) {
		return status.Errorf(codes.PermissionDenied, "%s workflow.%s is outside the current agent tool scope", path, ref.Operation)
	}
	return nil
}

func agentWorkflowAppStepAllowed(scope agentturnscope.Scope, target coreworkflow.AppCall) bool {
	for i := range scope.ToolRefs {
		if agentWorkflowToolRefMatchesAppCall(scope.ToolRefs[i], target) {
			return true
		}
	}
	for i := range scope.ListedTools {
		if scope.ListedTools[i].Target.Unavailable == nil && agentWorkflowToolTargetMatchesAppCall(scope.ListedTools[i].Target, target) {
			return true
		}
	}
	for i := range scope.Tools {
		if scope.Tools[i].Target.Unavailable == nil && agentWorkflowToolTargetMatchesAppCall(scope.Tools[i].Target, target) {
			return true
		}
	}
	return false
}

func agentWorkflowAppRefAllowed(scope agentturnscope.Scope, target coreagent.ToolRef) bool {
	for i := range scope.ToolRefs {
		if agentWorkflowToolRefMatchesToolRef(scope.ToolRefs[i], target) {
			return true
		}
	}
	for i := range scope.ListedTools {
		if scope.ListedTools[i].Target.Unavailable == nil && agentWorkflowToolTargetMatchesToolRef(scope.ListedTools[i].Target, target) {
			return true
		}
	}
	for i := range scope.Tools {
		if scope.Tools[i].Target.Unavailable == nil && agentWorkflowToolTargetMatchesToolRef(scope.Tools[i].Target, target) {
			return true
		}
	}
	return false
}

func agentWorkflowToolRefMatchesAppCall(ref coreagent.ToolRef, target coreworkflow.AppCall) bool {
	if strings.TrimSpace(ref.System) != "" || strings.TrimSpace(ref.App) == "" || strings.TrimSpace(ref.App) == agentToolSearchAllApp || strings.TrimSpace(ref.Operation) == "" {
		return false
	}
	if strings.TrimSpace(ref.App) != strings.TrimSpace(target.Name) || strings.TrimSpace(ref.Operation) != strings.TrimSpace(target.Operation) {
		return false
	}
	if !agentWorkflowCredentialModeMatches(ref.CredentialMode, target.CredentialMode) {
		return false
	}
	return agentWorkflowFlexibleBindingMatches(ref.Connection, ref.Instance, target.Connection, target.Instance)
}

func agentWorkflowToolRefMatchesToolRef(ref coreagent.ToolRef, target coreagent.ToolRef) bool {
	if strings.TrimSpace(ref.System) != "" || strings.TrimSpace(ref.App) == "" || strings.TrimSpace(ref.App) == agentToolSearchAllApp || strings.TrimSpace(ref.Operation) == "" {
		return false
	}
	if strings.TrimSpace(ref.App) != strings.TrimSpace(target.App) || strings.TrimSpace(ref.Operation) != strings.TrimSpace(target.Operation) {
		return false
	}
	if !agentWorkflowCredentialModeMatches(ref.CredentialMode, target.CredentialMode) {
		return false
	}
	return agentWorkflowFlexibleBindingMatches(ref.Connection, ref.Instance, target.Connection, target.Instance)
}

func agentWorkflowToolTargetMatchesAppCall(tool coreagent.ToolTarget, target coreworkflow.AppCall) bool {
	if strings.TrimSpace(tool.System) != "" || strings.TrimSpace(tool.App) == "" || strings.TrimSpace(tool.Operation) == "" {
		return false
	}
	if strings.TrimSpace(tool.App) != strings.TrimSpace(target.Name) || strings.TrimSpace(tool.Operation) != strings.TrimSpace(target.Operation) {
		return false
	}
	if !agentWorkflowCredentialModeMatches(tool.CredentialMode, target.CredentialMode) {
		return false
	}
	return agentWorkflowExactBindingMatches(tool.Connection, tool.Instance, target.Connection, target.Instance)
}

func agentWorkflowToolTargetMatchesToolRef(tool coreagent.ToolTarget, target coreagent.ToolRef) bool {
	if strings.TrimSpace(tool.System) != "" || strings.TrimSpace(tool.App) == "" || strings.TrimSpace(tool.Operation) == "" {
		return false
	}
	if strings.TrimSpace(tool.App) != strings.TrimSpace(target.App) || strings.TrimSpace(tool.Operation) != strings.TrimSpace(target.Operation) {
		return false
	}
	if !agentWorkflowCredentialModeMatches(tool.CredentialMode, target.CredentialMode) {
		return false
	}
	return agentWorkflowExactBindingMatches(tool.Connection, tool.Instance, target.Connection, target.Instance)
}

func agentWorkflowCredentialModeMatches(scope, target core.ConnectionMode) bool {
	scope = core.NormalizeOptionalConnectionMode(scope)
	target = core.NormalizeOptionalConnectionMode(target)
	return (scope == "" && target == "") || scope == target
}

func agentWorkflowFlexibleBindingMatches(scopeConnection, scopeInstance, targetConnection, targetInstance string) bool {
	scopeConnection = core.ResolveConnectionAlias(strings.TrimSpace(scopeConnection))
	targetConnection = core.ResolveConnectionAlias(strings.TrimSpace(targetConnection))
	if scopeConnection != "" && scopeConnection != targetConnection {
		return false
	}
	if scopeInstance = strings.TrimSpace(scopeInstance); scopeInstance != "" && scopeInstance != strings.TrimSpace(targetInstance) {
		return false
	}
	return true
}

func agentWorkflowExactBindingMatches(scopeConnection, scopeInstance, targetConnection, targetInstance string) bool {
	return core.ResolveConnectionAlias(strings.TrimSpace(scopeConnection)) == core.ResolveConnectionAlias(strings.TrimSpace(targetConnection)) &&
		strings.TrimSpace(scopeInstance) == strings.TrimSpace(targetInstance)
}

func agentWorkflowPrincipalForTarget(p *principal.Principal, scope agentturnscope.Scope, target coreworkflow.Target) *principal.Principal {
	p = principal.Canonicalized(p)
	if p == nil || strings.TrimSpace(scope.ProviderName) == "" || !agentWorkflowTargetOnlyUsesCurrentAgentProvider(scope.ProviderName, target) {
		return p
	}
	next := *p
	next.TokenPermissions = principal.ClonePermissionSet(p.TokenPermissions)
	if next.TokenPermissions == nil {
		next.TokenPermissions = principal.PermissionSet{}
	}
	next.TokenPermissions[strings.TrimSpace(scope.ProviderName)] = nil
	next.Scopes = principal.PermissionApps(next.TokenPermissions)
	return principal.Canonicalize(&next)
}

func agentWorkflowTargetOnlyUsesCurrentAgentProvider(providerName string, target coreworkflow.Target) bool {
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		return false
	}
	foundAgent := false
	for i := range target.Steps {
		if target.Steps[i].Agent == nil {
			continue
		}
		foundAgent = true
		targetProvider := strings.TrimSpace(target.Steps[i].Agent.ProviderName)
		if targetProvider != "" && targetProvider != providerName {
			return false
		}
	}
	return foundAgent
}

func agentScopePrincipal(scope agentturnscope.Scope) *principal.Principal {
	compiled := principal.CompilePermissions(scope.Permissions)
	value := &principal.Principal{
		SubjectID:           strings.TrimSpace(scope.SubjectID),
		CredentialSubjectID: strings.TrimSpace(scope.CredentialSubjectID),
		Scopes:              principal.PermissionApps(compiled),
		TokenPermissions:    compiled,
	}
	if kind, _, ok := core.ParseSubjectID(value.SubjectID); ok {
		value.Kind = principal.Kind(kind)
	}
	if value.CredentialSubjectID == "" && principal.IsSystemSubjectID(value.SubjectID) {
		value.CredentialSubjectID = value.SubjectID
	}
	return principal.Canonicalize(value)
}
