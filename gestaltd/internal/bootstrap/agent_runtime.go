package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/internal/agentgrant"
	"github.com/valon-technologies/gestalt/server/internal/agentmanager"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type agentRuntime struct {
	mu                  sync.RWMutex
	defaultProviderName string
	configuredProviders map[string]struct{}
	providers           map[string]coreagent.Provider
	invoker             invocation.Invoker
	systemTools         agentSystemToolExecutor
	toolGrants          *agentgrant.Manager
	toolSearcher        agentToolResolver
}

type agentSystemToolExecutionRequest struct {
	Principal      *principal.Principal
	ProviderName   string
	Tool           coreagent.Tool
	Arguments      map[string]any
	IdempotencyKey string
	ToolRefs       []coreagent.ToolRef
	Tools          []coreagent.Tool
}

type agentSystemToolExecutor interface {
	ExecuteSystemTool(ctx context.Context, req agentSystemToolExecutionRequest) (*coreagent.ExecuteToolResponse, error)
}

type agentToolResolver interface {
	SearchTools(ctx context.Context, p *principal.Principal, req coreagent.SearchToolsRequest) (*coreagent.SearchToolsResponse, error)
	ResolveTool(ctx context.Context, p *principal.Principal, ref coreagent.ToolRef) (coreagent.Tool, error)
}

func newAgentRuntime(cfg *config.Config) (*agentRuntime, error) {
	runtime := &agentRuntime{
		configuredProviders: map[string]struct{}{},
		providers:           map[string]coreagent.Provider{},
	}
	if cfg != nil {
		selectedProviderName, _, err := cfg.SelectedAgentProvider()
		if err == nil {
			runtime.defaultProviderName = strings.TrimSpace(selectedProviderName)
		}
		for name, entry := range cfg.Providers.Agent {
			name = strings.TrimSpace(name)
			if name == "" || entry == nil {
				continue
			}
			runtime.configuredProviders[name] = struct{}{}
		}
	}
	return runtime, nil
}

func (r *agentRuntime) PublishProvider(name string, provider coreagent.Provider) {
	if r == nil || provider == nil || strings.TrimSpace(name) == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.providers == nil {
		r.providers = map[string]coreagent.Provider{}
	}
	r.providers[name] = provider
}

func (r *agentRuntime) FailProvider(name string) {
	if r == nil || strings.TrimSpace(name) == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.providers, name)
}

func (r *agentRuntime) HasConfiguredProviders() bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.configuredProviders) > 0 || len(r.providers) > 0
}

func (r *agentRuntime) SetInvoker(invoker invocation.Invoker) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.invoker = invoker
}

func (r *agentRuntime) SetToolGrants(grants *agentgrant.Manager) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.toolGrants = grants
}

func (r *agentRuntime) SetToolSearcher(searcher agentToolResolver) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.toolSearcher = searcher
}

func (r *agentRuntime) SetSystemToolExecutor(executor agentSystemToolExecutor) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.systemTools = executor
}
func (r *agentRuntime) ResolveProvider(name string) (coreagent.Provider, error) {
	if r == nil {
		return nil, fmt.Errorf("agent runtime is not configured")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	provider, ok := r.providers[strings.TrimSpace(name)]
	if !ok || provider == nil {
		return nil, agentmanager.NewAgentProviderNotAvailableError(name)
	}
	return provider, nil
}

func (r *agentRuntime) ResolveProviderSelection(name string) (string, coreagent.Provider, error) {
	if r == nil {
		return "", nil, fmt.Errorf("agent runtime is not configured")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	selectedName := strings.TrimSpace(name)
	if selectedName == "" {
		selectedName = strings.TrimSpace(r.defaultProviderName)
	}
	if selectedName == "" {
		return "", nil, agentmanager.ErrAgentProviderRequired
	}
	provider, ok := r.providers[selectedName]
	if !ok || provider == nil {
		return "", nil, agentmanager.NewAgentProviderNotAvailableError(selectedName)
	}
	return selectedName, provider, nil
}

func (r *agentRuntime) ProviderNames() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		if strings.TrimSpace(name) == "" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (r *agentRuntime) Ping(ctx context.Context) error {
	if r == nil {
		return fmt.Errorf("agent runtime is not configured")
	}
	r.mu.RLock()
	defaultProviderName := strings.TrimSpace(r.defaultProviderName)
	providers := maps.Clone(r.providers)
	configuredProviders := make(map[string]struct{}, len(r.configuredProviders))
	for name := range r.configuredProviders {
		name = strings.TrimSpace(name)
		if name != "" {
			configuredProviders[name] = struct{}{}
		}
	}
	r.mu.RUnlock()

	if len(configuredProviders) == 0 {
		for name, provider := range providers {
			name = strings.TrimSpace(name)
			if name != "" && provider != nil {
				configuredProviders[name] = struct{}{}
			}
		}
	}
	if defaultProviderName != "" {
		configuredProviders[defaultProviderName] = struct{}{}
	}
	if len(configuredProviders) == 0 {
		return nil
	}

	names := make([]string, 0, len(configuredProviders))
	for name := range configuredProviders {
		names = append(names, name)
	}
	sort.Strings(names)
	errs := make(chan error, len(names))
	var wg sync.WaitGroup
	for _, name := range names {
		provider := providers[name]
		if provider == nil {
			errs <- fmt.Errorf("agent provider %q unavailable: %w", name, agentmanager.NewAgentProviderNotAvailableError(name))
			continue
		}
		wg.Add(1)
		go func(name string, provider coreagent.Provider) {
			defer wg.Done()
			if err := provider.Ping(ctx); err != nil {
				errs <- fmt.Errorf("agent provider %q unavailable: %w", name, err)
			}
		}(name, provider)
	}
	wg.Wait()
	close(errs)
	var joined []error
	for err := range errs {
		joined = append(joined, err)
	}
	return errors.Join(joined...)
}

func (r *agentRuntime) ExecuteTool(ctx context.Context, req coreagent.ExecuteToolRequest) (*coreagent.ExecuteToolResponse, error) {
	if r == nil {
		return nil, fmt.Errorf("agent runtime is not configured")
	}
	r.mu.RLock()
	invoker := r.invoker
	systemTools := r.systemTools
	grants := r.toolGrants
	searcher := r.toolSearcher
	r.mu.RUnlock()
	requestedTurnID := strings.TrimSpace(req.TurnID)
	grant, err := resolveAgentToolGrant(grants, strings.TrimSpace(req.ToolGrant), strings.TrimSpace(req.ProviderName), strings.TrimSpace(req.SessionID), requestedTurnID)
	if err != nil {
		return nil, err
	}
	if err := r.validateAgentToolGrantTurn(ctx, grant, requestedTurnID); err != nil {
		return nil, err
	}
	toolTarget, err := grants.ResolveToolID(req.ToolID)
	if err != nil {
		return nil, fmt.Errorf("%w: agent tool id is invalid", invocation.ErrAuthorizationDenied)
	}
	principalValue := agentToolGrantPrincipal(grant)
	if principalValue == nil || strings.TrimSpace(principalValue.SubjectID) == "" {
		return nil, fmt.Errorf("%w: agent execution principal is required", invocation.ErrInternal)
	}
	if searcher == nil {
		return nil, fmt.Errorf("%w: agent tool resolver is not configured", invocation.ErrInternal)
	}
	resolvedTool, err := searcher.ResolveTool(ctx, principalValue, coreagent.ToolRef{
		System:         toolTarget.System,
		Plugin:         toolTarget.Plugin,
		Operation:      toolTarget.Operation,
		Connection:     toolTarget.Connection,
		Instance:       toolTarget.Instance,
		CredentialMode: toolTarget.CredentialMode,
	})
	if err != nil {
		return nil, err
	}
	if resolvedTool.Hidden && !agentToolMatchesResolvedTools(resolvedTool.Target, resolvedTool.ID, grant.Tools) {
		return nil, fmt.Errorf("%w: hidden agent tool %q was not granted to this turn", invocation.ErrAuthorizationDenied, resolvedTool.ID)
	}
	if err := validateAgentToolTargetForGrant(grant, principalValue, resolvedTool.Target, resolvedTool.ID); err != nil {
		return nil, err
	}
	idempotencyKey := agentToolIdempotencyKey(req)
	if strings.TrimSpace(resolvedTool.Target.System) != "" {
		if systemTools == nil {
			return nil, agentmanager.ErrAgentWorkflowToolsNotConfigured
		}
		return systemTools.ExecuteSystemTool(ctx, agentSystemToolExecutionRequest{
			Principal:      principalValue,
			ProviderName:   strings.TrimSpace(grant.ProviderName),
			Tool:           resolvedTool,
			Arguments:      maps.Clone(req.Arguments),
			IdempotencyKey: idempotencyKey,
			ToolRefs:       append([]coreagent.ToolRef(nil), grant.ToolRefs...),
			Tools:          append([]coreagent.Tool(nil), grant.Tools...),
		})
	}
	if invoker == nil {
		return nil, fmt.Errorf("%w: agent runtime invoker is not configured", invocation.ErrInternal)
	}
	if connection := strings.TrimSpace(resolvedTool.Target.Connection); connection != "" {
		ctx = invocation.WithConnection(ctx, connection)
	}
	if mode := resolvedTool.Target.CredentialMode; mode != "" {
		ctx = invocation.WithCredentialModeOverride(ctx, mode)
	}
	if idempotencyKey != "" {
		ctx = invocation.WithIdempotencyKey(ctx, idempotencyKey)
	}
	params := maps.Clone(req.Arguments)
	result, err := invoker.Invoke(ctx, principalValue, resolvedTool.Target.Plugin, strings.TrimSpace(resolvedTool.Target.Instance), resolvedTool.Target.Operation, params)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return &coreagent.ExecuteToolResponse{Status: http.StatusOK}, nil
	}
	return &coreagent.ExecuteToolResponse{
		Status: result.Status,
		Body:   result.Body,
	}, nil
}

func (r *agentRuntime) SearchTools(ctx context.Context, req coreagent.SearchToolsRequest) (*coreagent.SearchToolsResponse, error) {
	if r == nil {
		return nil, fmt.Errorf("agent runtime is not configured")
	}
	r.mu.RLock()
	grants := r.toolGrants
	searcher := r.toolSearcher
	r.mu.RUnlock()
	if searcher == nil {
		return nil, fmt.Errorf("%w: agent tool search is not configured", invocation.ErrInternal)
	}
	requestedTurnID := strings.TrimSpace(req.TurnID)
	grant, err := resolveAgentToolGrant(grants, strings.TrimSpace(req.ToolGrant), strings.TrimSpace(req.ProviderName), strings.TrimSpace(req.SessionID), requestedTurnID)
	if err != nil {
		return nil, err
	}
	if err := r.validateAgentToolGrantTurn(ctx, grant, requestedTurnID); err != nil {
		return nil, err
	}
	principalValue := agentToolGrantPrincipal(grant)
	if principalValue == nil || strings.TrimSpace(principalValue.SubjectID) == "" {
		return nil, fmt.Errorf("%w: agent execution principal is required", invocation.ErrInternal)
	}
	toolSource := normalizeAgentToolSource(grant.ToolSource)
	resp, err := searcher.SearchTools(ctx, principalValue, coreagent.SearchToolsRequest{
		ProviderName:   strings.TrimSpace(grant.ProviderName),
		SessionID:      strings.TrimSpace(grant.SessionID),
		TurnID:         requestedTurnID,
		Query:          strings.TrimSpace(req.Query),
		MaxResults:     req.MaxResults,
		CandidateLimit: req.CandidateLimit,
		LoadRefs:       append([]coreagent.ToolRef(nil), req.LoadRefs...),
		ToolRefs:       append([]coreagent.ToolRef(nil), grant.ToolRefs...),
		ToolSource:     toolSource,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return &coreagent.SearchToolsResponse{}, nil
	}
	if err := validateAgentToolSearchResults(principalValue, grant.ToolRefs, toolSource, resp.Tools); err != nil {
		return nil, err
	}
	if err := validateAgentToolSearchCandidates(principalValue, grant.ToolRefs, toolSource, resp.Candidates); err != nil {
		return nil, err
	}
	return &coreagent.SearchToolsResponse{
		Tools:      append([]coreagent.Tool(nil), resp.Tools...),
		Candidates: append([]coreagent.ToolCandidate(nil), resp.Candidates...),
		HasMore:    resp.HasMore,
	}, nil
}

func resolveAgentToolGrant(grants *agentgrant.Manager, token, providerName, sessionID, turnID string) (agentgrant.Grant, error) {
	if grants == nil {
		return agentgrant.Grant{}, fmt.Errorf("%w: agent tool grants are not configured", invocation.ErrInternal)
	}
	grant, err := grants.Resolve(token)
	if err != nil {
		return agentgrant.Grant{}, fmt.Errorf("%w: %v", invocation.ErrAuthorizationDenied, err)
	}
	if strings.TrimSpace(grant.ProviderName) == "" {
		return agentgrant.Grant{}, fmt.Errorf("%w: agent tool grant has no provider", invocation.ErrAuthorizationDenied)
	}
	if providerName != "" && strings.TrimSpace(grant.ProviderName) != providerName {
		return agentgrant.Grant{}, fmt.Errorf("%w: agent tool grant is not valid for provider %q", invocation.ErrAuthorizationDenied, providerName)
	}
	if strings.TrimSpace(grant.SessionID) == "" || strings.TrimSpace(grant.SessionID) != sessionID {
		return agentgrant.Grant{}, fmt.Errorf("%w: agent tool grant is not valid for session %q", invocation.ErrAuthorizationDenied, sessionID)
	}
	if strings.TrimSpace(turnID) == "" {
		return agentgrant.Grant{}, fmt.Errorf("%w: agent turn is required", invocation.ErrAuthorizationDenied)
	}
	if strings.TrimSpace(grant.TurnID) == "" {
		return agentgrant.Grant{}, fmt.Errorf("%w: agent tool grant has no turn", invocation.ErrAuthorizationDenied)
	}
	if strings.TrimSpace(grant.SubjectID) == "" {
		return agentgrant.Grant{}, fmt.Errorf("%w: agent tool grant has no subject", invocation.ErrAuthorizationDenied)
	}
	return grant, nil
}

func (r *agentRuntime) validateAgentToolGrantTurn(ctx context.Context, grant agentgrant.Grant, turnID string) error {
	r.mu.RLock()
	provider := r.providers[strings.TrimSpace(grant.ProviderName)]
	r.mu.RUnlock()
	if provider == nil {
		return fmt.Errorf("%w: agent provider %q is not available for tool grant", invocation.ErrAuthorizationDenied, strings.TrimSpace(grant.ProviderName))
	}
	turnID = strings.TrimSpace(turnID)
	turn, err := provider.GetTurn(ctx, coreagent.GetTurnRequest{
		TurnID: turnID,
		Subject: coreagent.SubjectContext{
			SubjectID:           strings.TrimSpace(grant.SubjectID),
			SubjectKind:         strings.TrimSpace(grant.SubjectKind),
			CredentialSubjectID: strings.TrimSpace(grant.CredentialSubjectID),
			DisplayName:         strings.TrimSpace(grant.DisplayName),
			AuthSource:          strings.TrimSpace(grant.AuthSource),
		},
	})
	if err != nil {
		if errors.Is(err, core.ErrNotFound) || status.Code(err) == codes.NotFound {
			return fmt.Errorf("%w: agent turn %q was not found", invocation.ErrAuthorizationDenied, turnID)
		}
		return err
	}
	if turn == nil {
		return fmt.Errorf("%w: agent turn %q was not found", invocation.ErrAuthorizationDenied, turnID)
	}
	if strings.TrimSpace(turn.ID) != turnID {
		return fmt.Errorf("%w: agent provider returned turn %q for requested turn %q", invocation.ErrAuthorizationDenied, strings.TrimSpace(turn.ID), turnID)
	}
	if strings.TrimSpace(turn.SessionID) != strings.TrimSpace(grant.SessionID) {
		return fmt.Errorf("%w: agent tool grant is not valid for session %q", invocation.ErrAuthorizationDenied, strings.TrimSpace(grant.SessionID))
	}
	grantTurnID := strings.TrimSpace(grant.TurnID)
	if grantTurnID != turnID && grantTurnID != strings.TrimSpace(turn.ExecutionRef) {
		return fmt.Errorf("%w: agent tool grant is not valid for turn %q", invocation.ErrAuthorizationDenied, turnID)
	}
	if !coreagent.ExecutionStatusIsLive(turn.Status) {
		return fmt.Errorf("%w: agent turn %q is not active", invocation.ErrAuthorizationDenied, turnID)
	}
	return nil
}

func agentToolGrantPrincipal(grant agentgrant.Grant) *principal.Principal {
	compiled := principal.CompilePermissions(grant.Permissions)
	value := &principal.Principal{
		SubjectID:           strings.TrimSpace(grant.SubjectID),
		CredentialSubjectID: strings.TrimSpace(grant.CredentialSubjectID),
		DisplayName:         strings.TrimSpace(grant.DisplayName),
		Kind:                principal.Kind(strings.TrimSpace(grant.SubjectKind)),
		Scopes:              principal.PermissionPlugins(compiled),
		TokenPermissions:    compiled,
	}
	principal.SetAuthSource(value, grant.AuthSource)
	if value.CredentialSubjectID == "" && principal.IsSystemSubjectID(value.SubjectID) {
		value.CredentialSubjectID = value.SubjectID
	}
	return principal.Canonicalize(value)
}

func validateAgentToolTargetForGrant(grant agentgrant.Grant, principalValue *principal.Principal, target coreagent.ToolTarget, rawToolID string) error {
	if principalValue == nil {
		return fmt.Errorf("%w: agent execution principal is required", invocation.ErrInternal)
	}
	if normalizeAgentToolSource(grant.ToolSource) != coreagent.ToolSourceModeNativeSearch {
		return fmt.Errorf("%w: unsupported agent tool source %q", invocation.ErrInternal, grant.ToolSource)
	}
	operation := strings.TrimSpace(target.Operation)
	if systemName := strings.TrimSpace(target.System); systemName != "" {
		if systemName != coreagent.SystemToolWorkflow || operation == "" {
			return fmt.Errorf("%w: agent system tool target is incomplete", invocation.ErrAuthorizationDenied)
		}
		if !agentToolMatchesRefs(target, grant.ToolRefs) {
			return fmt.Errorf("%w: agent tool %q is outside the turn tool scope", invocation.ErrAuthorizationDenied, rawToolID)
		}
		return nil
	}
	pluginName := strings.TrimSpace(target.Plugin)
	if pluginName == "" || operation == "" {
		return fmt.Errorf("%w: agent tool target is incomplete", invocation.ErrAuthorizationDenied)
	}
	if !principal.AllowsProviderPermission(principalValue, pluginName) || !principal.AllowsOperationPermission(principalValue, pluginName, operation) {
		return fmt.Errorf("%w: agent tool %q is not authorized", invocation.ErrAuthorizationDenied, rawToolID)
	}
	if len(grant.ToolRefs) > 0 && !agentToolMatchesRefs(target, grant.ToolRefs) {
		return fmt.Errorf("%w: agent tool %q is outside the turn tool scope", invocation.ErrAuthorizationDenied, rawToolID)
	}
	if target.CredentialMode != "" && !agentToolCredentialModeExplicitlyGranted(target, grant.ToolRefs, grant.Tools) {
		return fmt.Errorf("%w: agent tool %q credential mode was not granted to this turn", invocation.ErrAuthorizationDenied, rawToolID)
	}
	return nil
}

func normalizeAgentToolSource(source coreagent.ToolSourceMode) coreagent.ToolSourceMode {
	if strings.TrimSpace(string(source)) == "" {
		return coreagent.ToolSourceModeNativeSearch
	}
	return source
}

func validateAgentToolSearchResults(p *principal.Principal, refs []coreagent.ToolRef, source coreagent.ToolSourceMode, tools []coreagent.Tool) error {
	if source != coreagent.ToolSourceModeNativeSearch {
		return fmt.Errorf("%w: unsupported agent tool source %q", invocation.ErrInternal, source)
	}
	for i := range tools {
		if strings.TrimSpace(tools[i].ID) == "" {
			return fmt.Errorf("%w: searched agent tool id is required", invocation.ErrAuthorizationDenied)
		}
		target := tools[i].Target
		if systemName := strings.TrimSpace(target.System); systemName != "" {
			if systemName != coreagent.SystemToolWorkflow || strings.TrimSpace(target.Operation) == "" {
				return fmt.Errorf("%w: searched agent system tool target is incomplete", invocation.ErrAuthorizationDenied)
			}
			if !agentToolMatchesRefs(target, refs) {
				return fmt.Errorf("%w: searched agent tool %q is outside the turn tool scope", invocation.ErrAuthorizationDenied, tools[i].ID)
			}
			continue
		}
		pluginName := strings.TrimSpace(target.Plugin)
		operation := strings.TrimSpace(target.Operation)
		if pluginName == "" || operation == "" {
			return fmt.Errorf("%w: searched agent tool target is incomplete", invocation.ErrAuthorizationDenied)
		}
		if !principal.AllowsProviderPermission(p, pluginName) || !principal.AllowsOperationPermission(p, pluginName, operation) {
			return fmt.Errorf("%w: searched agent tool %q is not authorized", invocation.ErrAuthorizationDenied, tools[i].ID)
		}
		if len(refs) > 0 && !agentToolMatchesRefs(target, refs) {
			return fmt.Errorf("%w: searched agent tool %q is outside the turn tool scope", invocation.ErrAuthorizationDenied, tools[i].ID)
		}
	}
	return nil
}

func agentToolIdempotencyKey(req coreagent.ExecuteToolRequest) string {
	if idempotencyKey := strings.TrimSpace(req.IdempotencyKey); idempotencyKey != "" {
		return idempotencyKey
	}
	turnID := strings.TrimSpace(req.TurnID)
	toolCallID := strings.TrimSpace(req.ToolCallID)
	if turnID == "" || toolCallID == "" {
		return ""
	}
	return "agent-tool:" + turnID + ":" + toolCallID
}

func validateAgentToolSearchCandidates(p *principal.Principal, refs []coreagent.ToolRef, source coreagent.ToolSourceMode, candidates []coreagent.ToolCandidate) error {
	if source != coreagent.ToolSourceModeNativeSearch {
		return fmt.Errorf("%w: unsupported agent tool source %q", invocation.ErrInternal, source)
	}
	for i := range candidates {
		ref := candidates[i].Ref
		if systemName := strings.TrimSpace(ref.System); systemName != "" {
			if systemName != coreagent.SystemToolWorkflow || strings.TrimSpace(ref.Operation) == "" {
				return fmt.Errorf("%w: searched agent system tool candidate target is incomplete", invocation.ErrAuthorizationDenied)
			}
			if !agentToolCandidateMatchesRefs(ref, refs) {
				return fmt.Errorf("%w: searched agent tool candidate %q is outside the turn tool scope", invocation.ErrAuthorizationDenied, candidates[i].ID)
			}
			continue
		}
		pluginName := strings.TrimSpace(ref.Plugin)
		operation := strings.TrimSpace(ref.Operation)
		if pluginName == "" || operation == "" {
			return fmt.Errorf("%w: searched agent tool candidate target is incomplete", invocation.ErrAuthorizationDenied)
		}
		if !principal.AllowsProviderPermission(p, pluginName) || !principal.AllowsOperationPermission(p, pluginName, operation) {
			return fmt.Errorf("%w: searched agent tool candidate %q is not authorized", invocation.ErrAuthorizationDenied, candidates[i].ID)
		}
		if len(refs) > 0 && !agentToolCandidateMatchesRefs(ref, refs) {
			return fmt.Errorf("%w: searched agent tool candidate %q is outside the turn tool scope", invocation.ErrAuthorizationDenied, candidates[i].ID)
		}
	}
	return nil
}

func agentToolMatchesRefs(target coreagent.ToolTarget, refs []coreagent.ToolRef) bool {
	if systemName := strings.TrimSpace(target.System); systemName != "" {
		targetOperation := strings.TrimSpace(target.Operation)
		for i := range refs {
			if strings.TrimSpace(refs[i].System) != systemName {
				continue
			}
			if strings.TrimSpace(refs[i].Operation) != targetOperation {
				continue
			}
			return true
		}
		return false
	}

	targetConnection := config.ResolveConnectionAlias(strings.TrimSpace(target.Connection))
	for i := range refs {
		ref := refs[i]
		if strings.TrimSpace(ref.Plugin) == "*" && strings.TrimSpace(ref.Operation) == "" {
			return true
		}
		if strings.TrimSpace(ref.Plugin) != strings.TrimSpace(target.Plugin) {
			continue
		}
		if operation := strings.TrimSpace(ref.Operation); operation != "" && operation != strings.TrimSpace(target.Operation) {
			continue
		}
		if connection := strings.TrimSpace(ref.Connection); connection != "" && config.ResolveConnectionAlias(connection) != targetConnection {
			continue
		}
		if instance := strings.TrimSpace(ref.Instance); instance != "" && instance != strings.TrimSpace(target.Instance) {
			continue
		}
		if ref.CredentialMode != "" && ref.CredentialMode != target.CredentialMode {
			continue
		}
		return true
	}
	return false
}

func agentToolCandidateMatchesRefs(candidate coreagent.ToolRef, refs []coreagent.ToolRef) bool {
	target := coreagent.ToolTarget{
		System:         candidate.System,
		Plugin:         candidate.Plugin,
		Operation:      candidate.Operation,
		Connection:     candidate.Connection,
		Instance:       candidate.Instance,
		CredentialMode: candidate.CredentialMode,
	}
	return agentToolMatchesRefs(target, refs)
}

func agentToolMatchesResolvedTools(target coreagent.ToolTarget, rawToolID string, tools []coreagent.Tool) bool {
	rawToolID = strings.TrimSpace(rawToolID)
	for i := range tools {
		if rawToolID != "" && strings.TrimSpace(tools[i].ID) == rawToolID {
			return true
		}
		if agentToolTargetsEqual(tools[i].Target, target) {
			return true
		}
	}
	return false
}

func agentToolCredentialModeExplicitlyGranted(target coreagent.ToolTarget, refs []coreagent.ToolRef, tools []coreagent.Tool) bool {
	if target.CredentialMode == "" {
		return true
	}
	if agentToolMatchesResolvedTools(target, "", tools) {
		return true
	}
	for i := range refs {
		ref := refs[i]
		if strings.TrimSpace(ref.Plugin) == "*" {
			continue
		}
		if strings.TrimSpace(ref.Plugin) != strings.TrimSpace(target.Plugin) {
			continue
		}
		if strings.TrimSpace(ref.Operation) != strings.TrimSpace(target.Operation) {
			continue
		}
		if ref.CredentialMode != target.CredentialMode {
			continue
		}
		if connection := strings.TrimSpace(ref.Connection); connection != "" && config.ResolveConnectionAlias(connection) != config.ResolveConnectionAlias(strings.TrimSpace(target.Connection)) {
			continue
		}
		if instance := strings.TrimSpace(ref.Instance); instance != "" && instance != strings.TrimSpace(target.Instance) {
			continue
		}
		return true
	}
	return false
}

func agentToolTargetsEqual(left, right coreagent.ToolTarget) bool {
	return strings.TrimSpace(left.System) == strings.TrimSpace(right.System) &&
		strings.TrimSpace(left.Plugin) == strings.TrimSpace(right.Plugin) &&
		strings.TrimSpace(left.Operation) == strings.TrimSpace(right.Operation) &&
		config.ResolveConnectionAlias(strings.TrimSpace(left.Connection)) == config.ResolveConnectionAlias(strings.TrimSpace(right.Connection)) &&
		strings.TrimSpace(left.Instance) == strings.TrimSpace(right.Instance) &&
		left.CredentialMode == right.CredentialMode
}
