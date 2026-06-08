package agentmanager

import (
	"context"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/agents/agentturnscope"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (m *Manager) AuthorizeAgentAppInvocation(ctx context.Context, providerName, turnID string, requestPrincipal *principal.Principal, target coreagent.ToolTarget, reqCtx *proto.RequestContext) (*principal.Principal, agentturnscope.Scope, coreagent.ListedTool, error) {
	if m == nil || m.turnScopes == nil {
		return nil, agentturnscope.Scope{}, coreagent.ListedTool{}, status.Error(codes.FailedPrecondition, "agent turn scopes are not configured")
	}
	providerName = strings.TrimSpace(providerName)
	turnID = strings.TrimSpace(turnID)
	if providerName == "" || turnID == "" {
		return nil, agentturnscope.Scope{}, coreagent.ListedTool{}, status.Error(codes.FailedPrecondition, "agent turn request context is required")
	}
	_, provider, err := m.resolveProvider(ctx, providerName)
	if err != nil {
		return nil, agentturnscope.Scope{}, coreagent.ListedTool{}, status.Errorf(codes.PermissionDenied, "agent provider %q is not available for turn scope", providerName)
	}
	scope, scopeFound := m.scopeForAgentInvocationRequest(providerName, turnID)
	if scopeFound {
		if err := validateAgentInvocationScope(scope, providerName, requestPrincipal); err != nil {
			return nil, agentturnscope.Scope{}, coreagent.ListedTool{}, err
		}
	}
	providerTurnID := turnID
	if scopeFound {
		if scopedProviderTurnID := strings.TrimSpace(scope.ProviderTurnID); scopedProviderTurnID != "" {
			providerTurnID = scopedProviderTurnID
		}
	}
	turn, err := getAgentInvocationTurn(ctx, provider, providerTurnID, turnID, reqCtx, requestPrincipal)
	if err != nil {
		return nil, agentturnscope.Scope{}, coreagent.ListedTool{}, err
	}
	if !scopeFound {
		var ok bool
		scope, ok = m.scopeForAgentInvocationTurn(providerName, turnID, turn)
		if !ok {
			return nil, agentturnscope.Scope{}, coreagent.ListedTool{}, status.Error(codes.PermissionDenied, "agent turn scope was not found")
		}
		if err := validateAgentInvocationScope(scope, providerName, requestPrincipal); err != nil {
			return nil, agentturnscope.Scope{}, coreagent.ListedTool{}, err
		}
	}
	if err := validateAgentInvocationReturnedTurn(scope, turnID, turn); err != nil {
		return nil, agentturnscope.Scope{}, coreagent.ListedTool{}, err
	}
	tool, ok := matchingAgentAppInvocationTool(scope.ListedTools, target)
	if !ok {
		return nil, agentturnscope.Scope{}, coreagent.ListedTool{}, status.Errorf(codes.PermissionDenied, "agent turn may not invoke %s.%s", strings.TrimSpace(target.App), strings.TrimSpace(target.Operation))
	}
	p := agentScopePrincipal(scope)
	if p == nil || strings.TrimSpace(p.SubjectID) == "" {
		return nil, agentturnscope.Scope{}, coreagent.ListedTool{}, status.Error(codes.FailedPrecondition, "agent turn scope subject is required")
	}
	return p, scope, tool, nil
}

func (m *Manager) scopeForAgentInvocationRequest(providerName, requestedTurnID string) (agentturnscope.Scope, bool) {
	if m == nil || m.turnScopes == nil {
		return agentturnscope.Scope{}, false
	}
	return m.turnScopes.GetByTurnID(providerName, requestedTurnID)
}

func (m *Manager) scopeForAgentInvocationTurn(providerName, requestedTurnID string, turn *coreagent.Turn) (agentturnscope.Scope, bool) {
	if m == nil || m.turnScopes == nil || turn == nil {
		return agentturnscope.Scope{}, false
	}
	sessionID := strings.TrimSpace(turn.SessionID)
	if sessionID == "" {
		return agentturnscope.Scope{}, false
	}
	for _, candidate := range []string{requestedTurnID, turn.ID, turn.ExecutionRef} {
		scope, ok := m.turnScopes.Get(providerName, sessionID, candidate)
		if ok {
			return scope, true
		}
	}
	return agentturnscope.Scope{}, false
}

func validateAgentInvocationScope(scope agentturnscope.Scope, providerName string, p *principal.Principal) error {
	if scope.Revoked {
		return status.Error(codes.PermissionDenied, "agent turn scope is revoked")
	}
	if strings.TrimSpace(providerName) != strings.TrimSpace(scope.ProviderName) {
		return status.Error(codes.PermissionDenied, "agent invocation provider does not match turn scope")
	}
	return validateAgentInvocationSubject(scope, p)
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

func getAgentInvocationTurn(ctx context.Context, provider coreagent.Provider, providerTurnID, requestedTurnID string, reqCtx *proto.RequestContext, p *principal.Principal) (*coreagent.Turn, error) {
	turn, err := provider.GetTurn(ctx, &proto.GetAgentProviderTurnRequest{
		TurnId:  strings.TrimSpace(providerTurnID),
		Context: reqCtx,
		Subject: &proto.SubjectContext{
			Id:                  strings.TrimSpace(principalSubjectID(p)),
			CredentialSubjectId: strings.TrimSpace(principal.EffectiveCredentialSubjectID(p)),
		},
	})
	if err != nil {
		if agentProviderReturnedNotFound(err) {
			return nil, status.Errorf(codes.PermissionDenied, "agent turn %q was not found", strings.TrimSpace(requestedTurnID))
		}
		return nil, err
	}
	return turn, nil
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

func matchingAgentAppInvocationTool(tools []coreagent.ListedTool, target coreagent.ToolTarget) (coreagent.ListedTool, bool) {
	targetApp := strings.TrimSpace(target.App)
	targetOperation := strings.TrimSpace(target.Operation)
	targetConnection := core.ResolveConnectionAlias(strings.TrimSpace(target.Connection))
	targetInstance := strings.TrimSpace(target.Instance)
	targetCredentialMode := core.NormalizeOptionalConnectionMode(target.CredentialMode)
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
