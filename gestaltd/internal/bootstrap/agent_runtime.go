package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/agents/agentgrant"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/plugins/declarative"
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
	runGrants           *agentgrant.Manager
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
	ListTools(ctx context.Context, p *principal.Principal, req coreagent.ListToolsRequest) (*coreagent.ListToolsResponse, error)
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

func (r *agentRuntime) SetRunGrants(grants *agentgrant.Manager) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runGrants = grants
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
	grants := r.runGrants
	searcher := r.toolSearcher
	r.mu.RUnlock()
	requestedTurnID := strings.TrimSpace(req.TurnID)
	grant, err := resolveAgentRunGrant(grants, strings.TrimSpace(req.RunGrant), strings.TrimSpace(req.ProviderName), strings.TrimSpace(req.SessionID), requestedTurnID)
	if err != nil {
		return nil, err
	}
	if err := r.validateAgentRunGrantTurn(ctx, grant, requestedTurnID); err != nil {
		return nil, err
	}
	toolTarget, err := grants.ResolveToolID(req.ToolID)
	if err != nil {
		return nil, fmt.Errorf("%w: agent tool id is invalid", invocation.ErrAuthorizationDenied)
	}
	principalValue := agentRunGrantPrincipal(grant)
	if principalValue == nil || strings.TrimSpace(principalValue.SubjectID) == "" {
		return nil, fmt.Errorf("%w: agent execution principal is required", invocation.ErrInternal)
	}
	if toolTarget.Unavailable != nil {
		if err := validateUnavailableAgentToolTargetForGrant(grant, principalValue, toolTarget, req.ToolID); err != nil {
			return nil, err
		}
		return executeUnavailableAgentTool(toolTarget)
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
		RunAs:          core.NormalizeRunAsSubject(toolTarget.RunAs),
	})
	if err != nil {
		return nil, err
	}
	if resolvedTool.Hidden && !agentToolHiddenExplicitlyGranted(resolvedTool.Target, resolvedTool.ID, grant.ToolRefs, grant.Tools) {
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
	invokePrincipal := principalValue
	if runAs := core.NormalizeRunAsSubject(resolvedTool.Target.RunAs); runAs != nil {
		invokePrincipal = agentRunAsPrincipal(principalValue, runAs)
		ctx = invocation.WithRunAsAudit(ctx, agentAuditSubjectFromPrincipal(principalValue), runAs)
	}
	if idempotencyKey != "" {
		ctx = invocation.WithIdempotencyKey(ctx, idempotencyKey)
	}
	params := maps.Clone(req.Arguments)
	result, err := invoker.Invoke(ctx, invokePrincipal, resolvedTool.Target.Plugin, strings.TrimSpace(resolvedTool.Target.Instance), resolvedTool.Target.Operation, params)
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

func (r *agentRuntime) ListTools(ctx context.Context, req coreagent.ListToolsRequest) (*coreagent.ListToolsResponse, error) {
	if r == nil {
		return nil, fmt.Errorf("agent runtime is not configured")
	}
	r.mu.RLock()
	grants := r.runGrants
	searcher := r.toolSearcher
	r.mu.RUnlock()
	if searcher == nil {
		return nil, fmt.Errorf("%w: agent tool listing is not configured", invocation.ErrInternal)
	}
	requestedTurnID := strings.TrimSpace(req.TurnID)
	grant, err := resolveAgentRunGrant(grants, strings.TrimSpace(req.RunGrant), strings.TrimSpace(req.ProviderName), strings.TrimSpace(req.SessionID), requestedTurnID)
	if err != nil {
		return nil, err
	}
	if err := r.validateAgentRunGrantTurn(ctx, grant, requestedTurnID); err != nil {
		return nil, err
	}
	principalValue := agentRunGrantPrincipal(grant)
	if principalValue == nil || strings.TrimSpace(principalValue.SubjectID) == "" {
		return nil, fmt.Errorf("%w: agent execution principal is required", invocation.ErrInternal)
	}
	toolSource := normalizeAgentToolSource(grant.ToolSource)
	if toolSource != coreagent.ToolSourceModeMCPCatalog {
		return nil, fmt.Errorf("%w: agent tool listing requires %q tool source", invocation.ErrAuthorizationDenied, coreagent.ToolSourceModeMCPCatalog)
	}
	if err := validateAgentMCPCatalogToolRefs(grant.ToolRefs); err != nil {
		return nil, fmt.Errorf("%w: %v", invocation.ErrAuthorizationDenied, err)
	}
	if len(grant.ToolRefs) == 0 {
		return &coreagent.ListToolsResponse{}, nil
	}
	resp, err := searcher.ListTools(ctx, principalValue, coreagent.ListToolsRequest{
		ProviderName: strings.TrimSpace(grant.ProviderName),
		SessionID:    strings.TrimSpace(grant.SessionID),
		TurnID:       requestedTurnID,
		PageSize:     req.PageSize,
		PageToken:    strings.TrimSpace(req.PageToken),
		ToolRefs:     append([]coreagent.ToolRef(nil), grant.ToolRefs...),
		ToolSource:   toolSource,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return &coreagent.ListToolsResponse{}, nil
	}
	if err := validateAgentListedTools(principalValue, grant.ToolRefs, toolSource, resp.Tools); err != nil {
		return nil, err
	}
	return &coreagent.ListToolsResponse{
		Tools:         append([]coreagent.ListedTool(nil), resp.Tools...),
		NextPageToken: strings.TrimSpace(resp.NextPageToken),
	}, nil
}

func (r *agentRuntime) ResolveConnection(ctx context.Context, req coreagent.ResolveConnectionRequest) (*coreagent.ResolvedConnection, error) {
	if r == nil {
		return nil, fmt.Errorf("agent runtime is not configured")
	}
	r.mu.RLock()
	grants := r.runGrants
	invoker := r.invoker
	r.mu.RUnlock()
	requestedTurnID := strings.TrimSpace(req.TurnID)
	grant, err := resolveAgentRunGrant(grants, strings.TrimSpace(req.RunGrant), strings.TrimSpace(req.ProviderName), strings.TrimSpace(req.SessionID), requestedTurnID)
	if err != nil {
		return nil, err
	}
	if err := r.validateAgentRunGrantTurn(ctx, grant, requestedTurnID); err != nil {
		return nil, err
	}
	connection := config.ResolveConnectionAlias(req.Connection)
	if connection == "" {
		connection = config.PluginConnectionName
	}
	if !agentRunGrantAllowsConnection(grant, connection) {
		return nil, fmt.Errorf("%w: agent connection %q is outside the run scope", invocation.ErrAuthorizationDenied, connection)
	}
	credentialResolver, ok := invoker.(invocation.RuntimeCredentialResolver)
	if !ok || credentialResolver == nil {
		return nil, fmt.Errorf("%w: agent connection credential resolver is not configured", invocation.ErrInternal)
	}
	principalValue := agentRunGrantPrincipal(grant)
	if principalValue == nil || strings.TrimSpace(principalValue.SubjectID) == "" {
		return nil, fmt.Errorf("%w: agent execution principal is required", invocation.ErrInternal)
	}
	providerName := strings.TrimSpace(grant.ProviderName)
	_, credential, info, err := credentialResolver.ResolveRuntimeConnectionCredential(invocation.WithInternalConnectionAccess(ctx), principalValue, providerName, connection, strings.TrimSpace(req.Instance))
	if err != nil {
		return nil, err
	}
	headers, err := materializeAgentConnectionHeaders(credential.Token, info)
	if err != nil {
		return nil, err
	}
	return &coreagent.ResolvedConnection{
		ConnectionID: strings.TrimSpace(info.ConnectionID),
		Connection:   connection,
		Instance:     strings.TrimSpace(req.Instance),
		Mode:         info.Mode,
		Headers:      headers,
		Params:       maps.Clone(info.Params),
		ExpiresAt:    credential.ExpiresAt,
	}, nil
}

func agentRunGrantAllowsConnection(grant agentgrant.Grant, connection string) bool {
	connection = config.ResolveConnectionAlias(connection)
	for _, binding := range grant.Connections {
		if config.ResolveConnectionAlias(binding.Connection) == connection {
			return true
		}
	}
	return false
}

func materializeAgentConnectionHeaders(token string, info invocation.ConnectionRuntimeInfo) (map[string]string, error) {
	token = strings.TrimSpace(token)
	if info.AuthMapping != nil {
		authToken, headers, err := declarative.MappedCredentialParser(info.AuthMapping)(token)
		if err != nil {
			return nil, err
		}
		if headers == nil {
			headers = map[string]string{}
		}
		if strings.TrimSpace(authToken) != "" {
			headers["Authorization"] = authToken
		}
		return headers, nil
	}
	if token == "" || info.Mode == core.ConnectionModeNone {
		return nil, nil
	}
	return map[string]string{"Authorization": core.BearerScheme + token}, nil
}

func resolveAgentRunGrant(grants *agentgrant.Manager, token, providerName, sessionID, turnID string) (agentgrant.Grant, error) {
	if grants == nil {
		return agentgrant.Grant{}, fmt.Errorf("%w: agent run grants are not configured", invocation.ErrInternal)
	}
	grant, err := grants.Resolve(token)
	if err != nil {
		return agentgrant.Grant{}, fmt.Errorf("%w: %v", invocation.ErrAuthorizationDenied, err)
	}
	if strings.TrimSpace(grant.ProviderName) == "" {
		return agentgrant.Grant{}, fmt.Errorf("%w: agent run grant has no provider", invocation.ErrAuthorizationDenied)
	}
	if providerName != "" && strings.TrimSpace(grant.ProviderName) != providerName {
		return agentgrant.Grant{}, fmt.Errorf("%w: agent run grant is not valid for provider %q", invocation.ErrAuthorizationDenied, providerName)
	}
	if strings.TrimSpace(grant.SessionID) == "" || strings.TrimSpace(grant.SessionID) != sessionID {
		return agentgrant.Grant{}, fmt.Errorf("%w: agent run grant is not valid for session %q", invocation.ErrAuthorizationDenied, sessionID)
	}
	if strings.TrimSpace(turnID) == "" {
		return agentgrant.Grant{}, fmt.Errorf("%w: agent turn is required", invocation.ErrAuthorizationDenied)
	}
	if strings.TrimSpace(grant.TurnID) == "" {
		return agentgrant.Grant{}, fmt.Errorf("%w: agent run grant has no turn", invocation.ErrAuthorizationDenied)
	}
	if strings.TrimSpace(grant.SubjectID) == "" {
		return agentgrant.Grant{}, fmt.Errorf("%w: agent run grant has no subject", invocation.ErrAuthorizationDenied)
	}
	return grant, nil
}

func (r *agentRuntime) validateAgentRunGrantTurn(ctx context.Context, grant agentgrant.Grant, turnID string) error {
	r.mu.RLock()
	provider := r.providers[strings.TrimSpace(grant.ProviderName)]
	r.mu.RUnlock()
	if provider == nil {
		return fmt.Errorf("%w: agent provider %q is not available for run grant", invocation.ErrAuthorizationDenied, strings.TrimSpace(grant.ProviderName))
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
		return fmt.Errorf("%w: agent run grant is not valid for session %q", invocation.ErrAuthorizationDenied, strings.TrimSpace(grant.SessionID))
	}
	grantTurnID := strings.TrimSpace(grant.TurnID)
	if grantTurnID != turnID && grantTurnID != strings.TrimSpace(turn.ExecutionRef) {
		return fmt.Errorf("%w: agent run grant is not valid for turn %q", invocation.ErrAuthorizationDenied, turnID)
	}
	if !coreagent.ExecutionStatusIsLive(turn.Status) {
		return fmt.Errorf("%w: agent turn %q is not active", invocation.ErrAuthorizationDenied, turnID)
	}
	return nil
}

func agentRunGrantPrincipal(grant agentgrant.Grant) *principal.Principal {
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

func agentRunAsPrincipal(base *principal.Principal, runAs *core.RunAsSubject) *principal.Principal {
	base = principal.Canonicalized(base)
	runAs = core.NormalizeRunAsSubject(runAs)
	if runAs == nil {
		return base
	}
	if base == nil {
		base = &principal.Principal{}
	}
	value := &principal.Principal{
		SubjectID:           strings.TrimSpace(runAs.SubjectID),
		CredentialSubjectID: strings.TrimSpace(runAs.CredentialSubjectID),
		DisplayName:         strings.TrimSpace(runAs.DisplayName),
		Kind:                principal.Kind(strings.TrimSpace(runAs.SubjectKind)),
		Scopes:              append([]string(nil), base.Scopes...),
		TokenPermissions:    principal.ClonePermissionSet(base.TokenPermissions),
		ActionPermissions:   principal.CloneActionPermissionSet(base.ActionPermissions),
		Identity:            base.Identity,
	}
	principal.SetAuthSource(value, runAs.AuthSource)
	if value.CredentialSubjectID == "" && principal.IsSystemSubjectID(value.SubjectID) {
		value.CredentialSubjectID = value.SubjectID
	}
	return principal.Canonicalize(value)
}

func agentAuditSubjectFromPrincipal(p *principal.Principal) *core.RunAsSubject {
	p = principal.Canonicalized(p)
	if p == nil {
		return nil
	}
	return core.NormalizeRunAsSubject(&core.RunAsSubject{
		SubjectID:           strings.TrimSpace(p.SubjectID),
		SubjectKind:         string(p.Kind),
		CredentialSubjectID: strings.TrimSpace(principal.EffectiveCredentialSubjectID(p)),
		DisplayName:         strings.TrimSpace(p.DisplayName),
		AuthSource:          p.AuthSource(),
	})
}

func validateAgentToolTargetForGrant(grant agentgrant.Grant, principalValue *principal.Principal, target coreagent.ToolTarget, rawToolID string) error {
	if principalValue == nil {
		return fmt.Errorf("%w: agent execution principal is required", invocation.ErrInternal)
	}
	source := normalizeAgentToolSource(grant.ToolSource)
	if source != coreagent.ToolSourceModeMCPCatalog {
		return fmt.Errorf("%w: unsupported agent tool source %q", invocation.ErrInternal, grant.ToolSource)
	}
	if err := validateAgentMCPCatalogToolRefs(grant.ToolRefs); err != nil {
		return fmt.Errorf("%w: %v", invocation.ErrAuthorizationDenied, err)
	}
	if len(grant.ToolRefs) == 0 {
		return fmt.Errorf("%w: agent tool %q is outside the turn tool scope", invocation.ErrAuthorizationDenied, rawToolID)
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
	if target.RunAs != nil && !agentToolRunAsExplicitlyGranted(target, grant.ToolRefs, grant.Tools) {
		return fmt.Errorf("%w: agent tool %q runAs delegation was not granted to this turn", invocation.ErrAuthorizationDenied, rawToolID)
	}
	return nil
}

func validateUnavailableAgentToolTargetForGrant(grant agentgrant.Grant, principalValue *principal.Principal, target coreagent.ToolTarget, rawToolID string) error {
	if err := validateAgentRunGrantForToolTarget(grant, target, rawToolID); err != nil {
		return err
	}
	return validateUnavailableAgentToolTarget(principalValue, grant.ToolRefs, target, rawToolID)
}

func validateAgentRunGrantForToolTarget(grant agentgrant.Grant, target coreagent.ToolTarget, rawToolID string) error {
	source := normalizeAgentToolSource(grant.ToolSource)
	if source != coreagent.ToolSourceModeMCPCatalog {
		return fmt.Errorf("%w: unsupported agent tool source %q", invocation.ErrInternal, grant.ToolSource)
	}
	if err := validateAgentMCPCatalogToolRefs(grant.ToolRefs); err != nil {
		return fmt.Errorf("%w: %v", invocation.ErrAuthorizationDenied, err)
	}
	if len(grant.ToolRefs) == 0 || !agentToolMatchesRefs(target, grant.ToolRefs) {
		return fmt.Errorf("%w: agent tool %q is outside the turn tool scope", invocation.ErrAuthorizationDenied, rawToolID)
	}
	if target.CredentialMode != "" && !agentToolCredentialModeExplicitlyGranted(target, grant.ToolRefs, grant.Tools) {
		return fmt.Errorf("%w: agent tool %q credential mode was not granted to this turn", invocation.ErrAuthorizationDenied, rawToolID)
	}
	return nil
}

func validateListedUnavailableAgentToolTarget(p *principal.Principal, refs []coreagent.ToolRef, target coreagent.ToolTarget, rawToolID string) error {
	if len(refs) == 0 || !agentToolMatchesRefs(target, refs) {
		return fmt.Errorf("%w: listed agent tool %q is outside the turn tool scope", invocation.ErrAuthorizationDenied, rawToolID)
	}
	return validateUnavailableAgentToolTarget(p, refs, target, rawToolID)
}

func validateUnavailableAgentToolTarget(principalValue *principal.Principal, refs []coreagent.ToolRef, target coreagent.ToolTarget, rawToolID string) error {
	if principalValue == nil {
		return fmt.Errorf("%w: agent execution principal is required", invocation.ErrInternal)
	}
	if target.Unavailable == nil || strings.TrimSpace(target.Unavailable.Reason) == "" {
		return fmt.Errorf("%w: unavailable agent tool %q is incomplete", invocation.ErrAuthorizationDenied, rawToolID)
	}
	if strings.TrimSpace(target.System) != "" || strings.TrimSpace(target.Operation) != "" {
		return fmt.Errorf("%w: unavailable agent tool %q cannot target a concrete operation", invocation.ErrAuthorizationDenied, rawToolID)
	}
	pluginName := strings.TrimSpace(target.Plugin)
	if pluginName == "" {
		return fmt.Errorf("%w: unavailable agent tool %q plugin is required", invocation.ErrAuthorizationDenied, rawToolID)
	}
	if !principal.AllowsProviderPermission(principalValue, pluginName) {
		return fmt.Errorf("%w: unavailable agent tool %q is not authorized", invocation.ErrAuthorizationDenied, rawToolID)
	}
	if !agentUnavailableReasonAllowed(strings.TrimSpace(target.Unavailable.Reason)) {
		return fmt.Errorf("%w: unavailable agent tool %q reason is invalid", invocation.ErrAuthorizationDenied, rawToolID)
	}
	if len(refs) > 0 && !agentToolMatchesRefs(target, refs) {
		return fmt.Errorf("%w: unavailable agent tool %q is outside the turn tool scope", invocation.ErrAuthorizationDenied, rawToolID)
	}
	return nil
}

func agentUnavailableReasonAllowed(reason string) bool {
	switch reason {
	case coreagent.ToolUnavailableReasonReconnectRequired,
		coreagent.ToolUnavailableReasonNotAuthenticated,
		coreagent.ToolUnavailableReasonNoCredential,
		coreagent.ToolUnavailableReasonScopeDenied,
		coreagent.ToolUnavailableReasonInstanceRequired:
		return true
	default:
		return false
	}
}

func executeUnavailableAgentTool(target coreagent.ToolTarget) (*coreagent.ExecuteToolResponse, error) {
	reason := coreagent.ToolUnavailableReasonReconnectRequired
	message := ""
	if target.Unavailable != nil {
		if strings.TrimSpace(target.Unavailable.Reason) != "" {
			reason = strings.TrimSpace(target.Unavailable.Reason)
		}
		message = strings.TrimSpace(target.Unavailable.Message)
	}
	if message == "" {
		message = "The requested integration is unavailable for this agent turn."
	}
	status := http.StatusFailedDependency
	switch reason {
	case coreagent.ToolUnavailableReasonScopeDenied:
		status = http.StatusForbidden
	case coreagent.ToolUnavailableReasonInstanceRequired:
		status = http.StatusPreconditionRequired
	case coreagent.ToolUnavailableReasonNotAuthenticated, coreagent.ToolUnavailableReasonNoCredential:
		status = http.StatusUnauthorized
	}
	body, err := json.Marshal(map[string]any{
		"error": map[string]any{
			"code":       reason,
			"message":    message,
			"plugin":     strings.TrimSpace(target.Plugin),
			"connection": strings.TrimSpace(target.Connection),
			"instance":   strings.TrimSpace(target.Instance),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("%w: encode unavailable agent tool response: %v", invocation.ErrInternal, err)
	}
	return &coreagent.ExecuteToolResponse{
		Status: status,
		Body:   string(body),
	}, nil
}

func normalizeAgentToolSource(source coreagent.ToolSourceMode) coreagent.ToolSourceMode {
	if strings.TrimSpace(string(source)) == "" {
		return coreagent.ToolSourceModeMCPCatalog
	}
	return source
}

func validateAgentMCPCatalogToolRefs(refs []coreagent.ToolRef) error {
	return coreagent.ValidateMCPCatalogToolRefs(refs, "toolRefs")
}

func validateAgentListedTools(p *principal.Principal, refs []coreagent.ToolRef, source coreagent.ToolSourceMode, tools []coreagent.ListedTool) error {
	if source != coreagent.ToolSourceModeMCPCatalog {
		return fmt.Errorf("%w: unsupported agent tool source %q", invocation.ErrInternal, source)
	}
	for i := range tools {
		if strings.TrimSpace(tools[i].ToolID) == "" {
			return fmt.Errorf("%w: listed agent tool id is required", invocation.ErrAuthorizationDenied)
		}
		if strings.TrimSpace(tools[i].MCPName) == "" {
			return fmt.Errorf("%w: listed agent tool mcp_name is required", invocation.ErrAuthorizationDenied)
		}
		target := tools[i].Target
		if target.Unavailable != nil {
			if err := validateListedUnavailableAgentToolTarget(p, refs, target, tools[i].ToolID); err != nil {
				return err
			}
			continue
		}
		if systemName := strings.TrimSpace(target.System); systemName != "" {
			if systemName != coreagent.SystemToolWorkflow || strings.TrimSpace(target.Operation) == "" {
				return fmt.Errorf("%w: listed agent system tool target is incomplete", invocation.ErrAuthorizationDenied)
			}
			if !agentToolMatchesRefs(target, refs) {
				return fmt.Errorf("%w: listed agent tool %q is outside the turn tool scope", invocation.ErrAuthorizationDenied, tools[i].ToolID)
			}
			continue
		}
		pluginName := strings.TrimSpace(target.Plugin)
		operation := strings.TrimSpace(target.Operation)
		if pluginName == "" || operation == "" {
			return fmt.Errorf("%w: listed agent tool target is incomplete", invocation.ErrAuthorizationDenied)
		}
		if !principal.AllowsProviderPermission(p, pluginName) || !principal.AllowsOperationPermission(p, pluginName, operation) {
			return fmt.Errorf("%w: listed agent tool %q is not authorized", invocation.ErrAuthorizationDenied, tools[i].ToolID)
		}
		if len(refs) > 0 && !agentToolMatchesRefs(target, refs) {
			return fmt.Errorf("%w: listed agent tool %q is outside the turn tool scope", invocation.ErrAuthorizationDenied, tools[i].ToolID)
		}
		if tools[i].Hidden && !agentToolHiddenExplicitlyGranted(target, tools[i].ToolID, refs, nil) {
			return fmt.Errorf("%w: listed hidden agent tool %q was not explicitly granted", invocation.ErrAuthorizationDenied, tools[i].ToolID)
		}
		if target.RunAs != nil && !agentToolRunAsExplicitlyGranted(target, refs, nil) {
			return fmt.Errorf("%w: listed agent tool %q runAs delegation was not explicitly granted", invocation.ErrAuthorizationDenied, tools[i].ToolID)
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
		if ref.RunAs != nil && !core.RunAsSubjectsEqual(ref.RunAs, target.RunAs) {
			continue
		}
		return true
	}
	return false
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

func agentToolHiddenExplicitlyGranted(target coreagent.ToolTarget, rawToolID string, refs []coreagent.ToolRef, tools []coreagent.Tool) bool {
	if agentToolMatchesResolvedTools(target, rawToolID, tools) {
		return true
	}
	targetOperation := strings.TrimSpace(target.Operation)
	if targetOperation == "" {
		return false
	}
	if systemName := strings.TrimSpace(target.System); systemName != "" {
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
		if strings.TrimSpace(ref.Plugin) != strings.TrimSpace(target.Plugin) {
			continue
		}
		if strings.TrimSpace(ref.Operation) != targetOperation {
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
		if ref.RunAs != nil && !core.RunAsSubjectsEqual(ref.RunAs, target.RunAs) {
			continue
		}
		return true
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

func agentToolRunAsExplicitlyGranted(target coreagent.ToolTarget, refs []coreagent.ToolRef, tools []coreagent.Tool) bool {
	if target.RunAs == nil {
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
		if !core.RunAsSubjectsEqual(ref.RunAs, target.RunAs) {
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
		left.CredentialMode == right.CredentialMode &&
		core.RunAsSubjectsEqual(left.RunAs, right.RunAs)
}
