package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/internal/config"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/agents/agentgrant"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/apps/declarative"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type agentRuntime struct {
	mu                     sync.RWMutex
	defaultProviderName    string
	configuredProviders    map[string]struct{}
	startupFailedProviders map[string]struct{}
	providers              map[string]coreagent.Provider
	pendingProviders       map[string]*startupProviderHandle[coreagent.Provider]
	startupWaits           *startupWaitTracker
	invoker                invocation.Invoker
	systemTools            agentSystemToolExecutor
	runGrants              *agentgrant.Manager
	toolSearcher           agentToolResolver
}

type agentSystemToolExecutionRequest struct {
	Principal      *principal.Principal
	ProviderName   string
	CallerAppName  string
	SessionID      string
	TurnID         string
	ToolCallID     string
	ToolID         string
	Tool           coreagent.Tool
	Arguments      map[string]any
	IdempotencyKey string
	ToolRefs       []coreagent.ToolRef
	Tools          []coreagent.Tool
	Permissions    []core.AccessPermission
}

type agentSystemToolExecutor interface {
	ExecuteSystemTool(ctx context.Context, req agentSystemToolExecutionRequest) (*coreagent.ExecuteToolResponse, error)
}

type agentToolResolver interface {
	ListTools(ctx context.Context, p *principal.Principal, req coreagent.ListToolsRequest) (*coreagent.ListToolsResponse, error)
	ResolveTool(ctx context.Context, p *principal.Principal, ref coreagent.ToolRef) (coreagent.Tool, error)
}

func newAgentRuntime(cfg *config.Config, startupWaits *startupWaitTracker) (*agentRuntime, error) {
	if startupWaits == nil {
		startupWaits = newStartupWaitTracker()
	}
	runtime := &agentRuntime{
		configuredProviders:    map[string]struct{}{},
		startupFailedProviders: map[string]struct{}{},
		providers:              map[string]coreagent.Provider{},
		pendingProviders:       map[string]*startupProviderHandle[coreagent.Provider]{},
		startupWaits:           startupWaits,
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
			runtime.pendingProviders[name] = newAgentProviderHandle(name, startupWaits)
		}
	}
	return runtime, nil
}

func newAgentProviderHandle(name string, tracker *startupWaitTracker) *startupProviderHandle[coreagent.Provider] {
	return newStartupProviderHandle[coreagent.Provider](name, newStartupProviderNode(invocation.ProviderKindAgent, name), tracker)
}

func agentSessionStartConfigs(cfg *config.Config) map[string]*coreagent.SessionStartConfig {
	if cfg == nil || len(cfg.Providers.Agent) == 0 {
		return nil
	}
	out := make(map[string]*coreagent.SessionStartConfig)
	for name, entry := range cfg.Providers.Agent {
		name = strings.TrimSpace(name)
		if name == "" || entry == nil || entry.Lifecycle == nil || len(entry.Lifecycle.SessionStart) == 0 {
			continue
		}
		hooks := make([]coreagent.SessionStartHook, 0, len(entry.Lifecycle.SessionStart))
		for _, hook := range entry.Lifecycle.SessionStart {
			hooks = append(hooks, coreagent.SessionStartHook{
				ID:      hook.ID,
				Type:    hook.Type,
				Command: append([]string(nil), hook.Command...),
				CWD:     hook.CWD,
				Timeout: hook.Timeout,
				Env:     maps.Clone(hook.Env),
				Output: coreagent.SessionStartHookOutput{
					AdditionalContext: hook.Output.AdditionalContext,
					Metadata:          hook.Output.Metadata,
				},
			})
		}
		if len(hooks) > 0 {
			out[name] = &coreagent.SessionStartConfig{Hooks: hooks}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (r *agentRuntime) PublishProvider(name string, provider coreagent.Provider) {
	name = strings.TrimSpace(name)
	if r == nil || provider == nil || name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.providers == nil {
		r.providers = map[string]coreagent.Provider{}
	}
	if r.pendingProviders == nil {
		r.pendingProviders = map[string]*startupProviderHandle[coreagent.Provider]{}
	}
	if r.startupFailedProviders == nil {
		r.startupFailedProviders = map[string]struct{}{}
	}
	if _, failedStartup := r.startupFailedProviders[name]; failedStartup {
		return
	}
	if handle := r.pendingProviders[name]; handle != nil {
		handle.publish(provider)
		delete(r.pendingProviders, name)
	}
	r.providers[name] = provider
}

func (r *agentRuntime) FailStartupProvider(name string, err error) {
	name = strings.TrimSpace(name)
	if r == nil || name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	handle := r.pendingProviders[name]
	if handle == nil {
		return
	}
	if err == nil {
		err = agentmanager.NewAgentProviderNotAvailableError(name)
	}
	handle.fail(err)
	if r.startupFailedProviders == nil {
		r.startupFailedProviders = map[string]struct{}{}
	}
	r.startupFailedProviders[name] = struct{}{}
	delete(r.pendingProviders, name)
}

func (r *agentRuntime) UnpublishProvider(name string) {
	name = strings.TrimSpace(name)
	if r == nil || name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.providers, name)
	delete(r.pendingProviders, name)
}

func (r *agentRuntime) FailPendingProviders(err error) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for name, handle := range r.pendingProviders {
		failErr := err
		if failErr == nil {
			failErr = agentmanager.NewAgentProviderNotAvailableError(name)
		}
		handle.fail(failErr)
		if r.startupFailedProviders == nil {
			r.startupFailedProviders = map[string]struct{}{}
		}
		r.startupFailedProviders[name] = struct{}{}
		delete(r.pendingProviders, name)
	}
}

func (r *agentRuntime) HasConfiguredProviders() bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.configuredProviders) > 0 || len(r.providers) > 0 || len(r.pendingProviders) > 0
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
func (r *agentRuntime) ResolveProvider(ctx context.Context, name string) (string, coreagent.Provider, error) {
	if r == nil {
		return "", nil, fmt.Errorf("agent runtime is not configured")
	}
	r.mu.RLock()
	selectedName := strings.TrimSpace(name)
	if selectedName == "" {
		selectedName = strings.TrimSpace(r.defaultProviderName)
	}
	if selectedName == "" {
		r.mu.RUnlock()
		return "", nil, agentmanager.ErrAgentProviderRequired
	}
	provider, ok := r.providers[selectedName]
	handle := r.pendingProviders[selectedName]
	r.mu.RUnlock()
	if ok && provider != nil {
		return selectedName, provider, nil
	}
	if handle == nil {
		return "", nil, agentmanager.NewAgentProviderNotAvailableError(selectedName)
	}
	provider, err := handle.await(ctx)
	if err != nil {
		return "", nil, err
	}
	if provider == nil {
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
	seen := map[string]struct{}{}
	names := make([]string, 0, len(r.providers)+len(r.pendingProviders))
	for name := range r.providers {
		if strings.TrimSpace(name) == "" {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	for name := range r.pendingProviders {
		if strings.TrimSpace(name) == "" {
			continue
		}
		if _, ok := seen[name]; ok {
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
	pendingProviders := maps.Clone(r.pendingProviders)
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
			if handle := pendingProviders[name]; handle != nil {
				if resolved, ready, err := handle.resolved(); err != nil {
					errs <- fmt.Errorf("agent provider %q unavailable: %w", name, err)
					continue
				} else if ready {
					provider = resolved
				}
			}
			if provider == nil {
				errs <- fmt.Errorf("agent provider %q unavailable: %w", name, agentmanager.NewAgentProviderNotAvailableError(name))
				continue
			}
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
	if source := normalizeAgentToolSource(grant.ToolSource); source != coreagent.ToolSourceModeMCPCatalog {
		return nil, fmt.Errorf("%w: agent tool execution requires %q tool source", invocation.ErrAuthorizationDenied, coreagent.ToolSourceModeMCPCatalog)
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
		System:                toolTarget.System,
		App:                   toolTarget.App,
		Operation:             toolTarget.Operation,
		Connection:            toolTarget.Connection,
		Instance:              toolTarget.Instance,
		CredentialMode:        toolTarget.CredentialMode,
		RunAs:                 core.NormalizeRunAsSubject(toolTarget.RunAs),
		RunAsExternalIdentity: core.NormalizeExternalIdentityRef(toolTarget.RunAsExternalIdentity),
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
			CallerAppName:  strings.TrimSpace(grant.CallerAppName),
			SessionID:      strings.TrimSpace(req.SessionID),
			TurnID:         strings.TrimSpace(req.TurnID),
			ToolCallID:     strings.TrimSpace(req.ToolCallID),
			ToolID:         strings.TrimSpace(req.ToolID),
			Tool:           resolvedTool,
			Arguments:      maps.Clone(req.Arguments),
			IdempotencyKey: idempotencyKey,
			ToolRefs:       append([]coreagent.ToolRef(nil), grant.ToolRefs...),
			Tools:          append([]coreagent.Tool(nil), grant.Tools...),
			Permissions:    append([]core.AccessPermission(nil), grant.Permissions...),
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
	if identity, err := renderAgentToolExternalIdentity(resolvedTool.Target.RunAsExternalIdentity, params); err != nil {
		return nil, err
	} else if identity != nil {
		ctx = invocation.WithExternalIdentityContext(ctx, invocation.ExternalIdentityContext{
			Type: identity.Type,
			ID:   identity.ID,
		})
	}
	if grant.ToolRefsSet {
		ctx = invocation.WithToolRefsContext(ctx, grant.ToolRefs)
	}
	result, err := invoker.Invoke(ctx, invokePrincipal, resolvedTool.Target.App, strings.TrimSpace(resolvedTool.Target.Instance), resolvedTool.Target.Operation, params)
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

func (r *agentRuntime) ListTools(ctx context.Context, req coreagent.ListToolsRequest) (resp *coreagent.ListToolsResponse, err error) {
	requestedTurnID := strings.TrimSpace(req.TurnID)
	var grant agentgrant.Grant
	defer func() {
		logAgentRuntimeListTools(ctx, req, requestedTurnID, grant, resp, err)
	}()
	if r == nil {
		return nil, fmt.Errorf("agent runtime is not configured")
	}
	r.mu.RLock()
	grants := r.runGrants
	searcher := r.toolSearcher
	r.mu.RUnlock()
	grant, err = resolveAgentRunGrant(grants, strings.TrimSpace(req.RunGrant), strings.TrimSpace(req.ProviderName), strings.TrimSpace(req.SessionID), requestedTurnID)
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
	if searcher == nil {
		return nil, fmt.Errorf("%w: agent tool listing is not configured", invocation.ErrInternal)
	}
	if err := validateAgentMCPCatalogToolRefs(grant.ToolRefs); err != nil {
		return nil, fmt.Errorf("%w: %v", invocation.ErrAuthorizationDenied, err)
	}
	if len(grant.ToolRefs) == 0 {
		return &coreagent.ListToolsResponse{}, nil
	}
	listResp, err := searcher.ListTools(ctx, principalValue, coreagent.ListToolsRequest{
		ProviderName: strings.TrimSpace(grant.ProviderName),
		SessionID:    strings.TrimSpace(grant.SessionID),
		TurnID:       requestedTurnID,
		PageSize:     req.PageSize,
		PageToken:    strings.TrimSpace(req.PageToken),
		Query:        strings.TrimSpace(req.Query),
		ToolRefs:     append([]coreagent.ToolRef(nil), grant.ToolRefs...),
		ToolSource:   toolSource,
	})
	if err != nil {
		return nil, err
	}
	if listResp == nil {
		return &coreagent.ListToolsResponse{}, nil
	}
	if err := validateAgentListedTools(principalValue, grant.ToolRefs, toolSource, listResp.Tools); err != nil {
		return nil, err
	}
	return &coreagent.ListToolsResponse{
		Tools:         append([]coreagent.ListedTool(nil), listResp.Tools...),
		NextPageToken: strings.TrimSpace(listResp.NextPageToken),
	}, nil
}

func logAgentRuntimeListTools(ctx context.Context, req coreagent.ListToolsRequest, requestedTurnID string, grant agentgrant.Grant, resp *coreagent.ListToolsResponse, err error) {
	attrs := []any{
		"provider", strings.TrimSpace(req.ProviderName),
		"session_id", strings.TrimSpace(req.SessionID),
		"turn_id", requestedTurnID,
		"page_size", req.PageSize,
		"page_token", strings.TrimSpace(req.PageToken),
		"query_present", strings.TrimSpace(req.Query) != "",
		"grant_provider", strings.TrimSpace(grant.ProviderName),
		"grant_tool_source", strings.TrimSpace(string(grant.ToolSource)),
		"grant_tool_refs", agentToolRefsLogValue(grant.ToolRefs),
	}
	if resp != nil {
		attrs = append(attrs,
			"tool_count", len(resp.Tools),
			"github_mcp_names", githubListedToolMCPNames(resp.Tools),
			"next_page_token", strings.TrimSpace(resp.NextPageToken),
		)
	}
	if err != nil {
		attrs = append(attrs, "error", err)
		slog.WarnContext(ctx, "agent runtime MCP catalog tool listing failed", attrs...)
		return
	}
	slog.InfoContext(ctx, "agent runtime MCP catalog tools listed", attrs...)
}

func agentToolRefsLogValue(refs []coreagent.ToolRef) []map[string]string {
	if len(refs) == 0 {
		return nil
	}
	out := make([]map[string]string, 0, len(refs))
	for i := range refs {
		ref := refs[i]
		value := map[string]string{}
		if systemName := strings.TrimSpace(ref.System); systemName != "" {
			value["system"] = systemName
		}
		if appName := strings.TrimSpace(ref.App); appName != "" {
			value["app"] = appName
		}
		if operation := strings.TrimSpace(ref.Operation); operation != "" {
			value["operation"] = operation
		}
		if connection := strings.TrimSpace(ref.Connection); connection != "" {
			value["connection"] = connection
		}
		if instance := strings.TrimSpace(ref.Instance); instance != "" {
			value["instance"] = instance
		}
		if mode := strings.TrimSpace(string(ref.CredentialMode)); mode != "" {
			value["credential_mode"] = mode
		}
		if runAs := core.NormalizeRunAsSubject(ref.RunAs); runAs != nil {
			if subjectID := strings.TrimSpace(runAs.SubjectID); subjectID != "" {
				value["run_as_subject_id"] = subjectID
			}
			if subjectKind := strings.TrimSpace(runAs.SubjectKind); subjectKind != "" {
				value["run_as_subject_kind"] = subjectKind
			}
		}
		if identity := core.NormalizeExternalIdentityRef(ref.RunAsExternalIdentity); identity != nil {
			if identityType := strings.TrimSpace(identity.Type); identityType != "" {
				value["external_identity_type"] = identityType
			}
			if identityID := strings.TrimSpace(identity.ID); identityID != "" {
				value["external_identity_id"] = identityID
			}
		}
		out = append(out, value)
	}
	return out
}

func githubListedToolMCPNames(tools []coreagent.ListedTool) []string {
	if len(tools) == 0 {
		return nil
	}
	names := make([]string, 0)
	seen := map[string]struct{}{}
	for i := range tools {
		tool := tools[i]
		if !listedToolIsGitHub(tool) {
			continue
		}
		name := strings.TrimSpace(tool.MCPName)
		if name == "" {
			name = strings.TrimSpace(tool.Title)
		}
		if name == "" {
			name = strings.TrimSpace(tool.Target.Operation)
		}
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}

func listedToolIsGitHub(tool coreagent.ListedTool) bool {
	return strings.TrimSpace(tool.Ref.App) == "github" ||
		strings.TrimSpace(tool.Target.App) == "github" ||
		strings.HasPrefix(strings.TrimSpace(tool.MCPName), "github__")
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
	if source := normalizeAgentToolSource(grant.ToolSource); source != coreagent.ToolSourceModeMCPCatalog {
		return nil, fmt.Errorf("%w: agent connection resolution requires %q tool source", invocation.ErrAuthorizationDenied, coreagent.ToolSourceModeMCPCatalog)
	}
	connection := config.ResolveConnectionAlias(req.Connection)
	if connection == "" {
		connection = config.AppConnectionName
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
	handle := r.pendingProviders[strings.TrimSpace(grant.ProviderName)]
	r.mu.RUnlock()
	if provider == nil {
		if handle == nil {
			return fmt.Errorf("%w: agent provider %q is not available for run grant", invocation.ErrAuthorizationDenied, strings.TrimSpace(grant.ProviderName))
		}
		resolved, err := handle.await(ctx)
		if err != nil {
			return err
		}
		provider = resolved
		if provider == nil {
			return fmt.Errorf("%w: agent provider %q is not available for run grant", invocation.ErrAuthorizationDenied, strings.TrimSpace(grant.ProviderName))
		}
	}
	turnID = strings.TrimSpace(turnID)
	turn, err := provider.GetTurn(ctx, &proto.GetAgentProviderTurnRequest{
		TurnId: turnID,
		Subject: &proto.SubjectContext{
			Id:                  strings.TrimSpace(grant.SubjectID),
			Kind:                strings.TrimSpace(grant.SubjectKind),
			CredentialSubjectId: strings.TrimSpace(grant.CredentialSubjectID),
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
		Scopes:              principal.PermissionApps(compiled),
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

func renderAgentToolExternalIdentity(identity *core.ExternalIdentityRef, args map[string]any) (*core.ExternalIdentityRef, error) {
	identity = core.NormalizeExternalIdentityRef(identity)
	if identity == nil {
		return nil, nil
	}
	renderedID, err := renderAgentToolExternalIdentityTemplate(identity.ID, args)
	if err != nil {
		return nil, err
	}
	rendered := core.NormalizeExternalIdentityRef(&core.ExternalIdentityRef{
		Type: identity.Type,
		ID:   renderedID,
	})
	if rendered == nil {
		return nil, fmt.Errorf("%w: runAs external identity is incomplete", invocation.ErrInvalidInvocation)
	}
	return rendered, nil
}

func renderAgentToolExternalIdentityTemplate(tmpl string, args map[string]any) (string, error) {
	if !strings.Contains(tmpl, "{") {
		return strings.TrimSpace(tmpl), nil
	}
	var out strings.Builder
	for i := 0; i < len(tmpl); {
		open := strings.IndexByte(tmpl[i:], '{')
		if open < 0 {
			out.WriteString(tmpl[i:])
			break
		}
		open += i
		out.WriteString(tmpl[i:open])
		close := strings.IndexByte(tmpl[open+1:], '}')
		if close < 0 {
			return "", fmt.Errorf("%w: runAs external identity template has an unterminated placeholder", invocation.ErrInvalidInvocation)
		}
		close += open + 1
		name := strings.TrimSpace(tmpl[open+1 : close])
		if name == "" {
			return "", fmt.Errorf("%w: runAs external identity template has an empty placeholder", invocation.ErrInvalidInvocation)
		}
		value, ok := args[name]
		if !ok || value == nil {
			return "", fmt.Errorf("%w: runAs external identity template argument %q is required", invocation.ErrInvalidInvocation, name)
		}
		rendered, ok := agentToolExternalIdentityTemplateValue(value)
		if !ok || strings.TrimSpace(rendered) == "" {
			return "", fmt.Errorf("%w: runAs external identity template argument %q must be a scalar value", invocation.ErrInvalidInvocation, name)
		}
		out.WriteString(rendered)
		i = close + 1
	}
	return strings.TrimSpace(out.String()), nil
}

func agentToolExternalIdentityTemplateValue(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		return v, true
	case fmt.Stringer:
		return v.String(), true
	case bool:
		return fmt.Sprint(v), true
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return fmt.Sprint(v), true
	default:
		return "", false
	}
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
	appName := strings.TrimSpace(target.App)
	if appName == "" || operation == "" {
		return fmt.Errorf("%w: agent tool target is incomplete", invocation.ErrAuthorizationDenied)
	}
	if !principal.AllowsProviderPermission(principalValue, appName) || !principal.AllowsOperationPermission(principalValue, appName, operation) {
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
	if target.RunAsExternalIdentity != nil && !agentToolExternalIdentityExplicitlyGranted(target, grant.ToolRefs, grant.Tools) {
		return fmt.Errorf("%w: agent tool %q runAs external identity was not granted to this turn", invocation.ErrAuthorizationDenied, rawToolID)
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
	if target.RunAsExternalIdentity != nil && !agentToolExternalIdentityExplicitlyGranted(target, grant.ToolRefs, grant.Tools) {
		return fmt.Errorf("%w: agent tool %q runAs external identity was not granted to this turn", invocation.ErrAuthorizationDenied, rawToolID)
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
	appName := strings.TrimSpace(target.App)
	if appName == "" {
		return fmt.Errorf("%w: unavailable agent tool %q app is required", invocation.ErrAuthorizationDenied, rawToolID)
	}
	if !principal.AllowsProviderPermission(principalValue, appName) {
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
			"app":        strings.TrimSpace(target.App),
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
		appName := strings.TrimSpace(target.App)
		operation := strings.TrimSpace(target.Operation)
		if appName == "" || operation == "" {
			return fmt.Errorf("%w: listed agent tool target is incomplete", invocation.ErrAuthorizationDenied)
		}
		if !principal.AllowsProviderPermission(p, appName) || !principal.AllowsOperationPermission(p, appName, operation) {
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
		if target.RunAsExternalIdentity != nil && !agentToolExternalIdentityExplicitlyGranted(target, refs, nil) {
			return fmt.Errorf("%w: listed agent tool %q runAs external identity was not explicitly granted", invocation.ErrAuthorizationDenied, tools[i].ToolID)
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
		if strings.TrimSpace(ref.App) == "*" && strings.TrimSpace(ref.Operation) == "" {
			return true
		}
		if strings.TrimSpace(ref.App) != strings.TrimSpace(target.App) {
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
		if ref.RunAsExternalIdentity != nil && !core.ExternalIdentityRefsEqual(ref.RunAsExternalIdentity, target.RunAsExternalIdentity) {
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
		if strings.TrimSpace(ref.App) != strings.TrimSpace(target.App) {
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
		if ref.RunAsExternalIdentity != nil && !core.ExternalIdentityRefsEqual(ref.RunAsExternalIdentity, target.RunAsExternalIdentity) {
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
		if strings.TrimSpace(ref.App) == "*" {
			continue
		}
		if strings.TrimSpace(ref.App) != strings.TrimSpace(target.App) {
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
		if strings.TrimSpace(ref.App) == "*" {
			continue
		}
		if strings.TrimSpace(ref.App) != strings.TrimSpace(target.App) {
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

func agentToolExternalIdentityExplicitlyGranted(target coreagent.ToolTarget, refs []coreagent.ToolRef, tools []coreagent.Tool) bool {
	if target.RunAsExternalIdentity == nil {
		return true
	}
	if agentToolMatchesResolvedTools(target, "", tools) {
		return true
	}
	for i := range refs {
		ref := refs[i]
		if strings.TrimSpace(ref.App) == "*" {
			continue
		}
		if strings.TrimSpace(ref.App) != strings.TrimSpace(target.App) {
			continue
		}
		if strings.TrimSpace(ref.Operation) != strings.TrimSpace(target.Operation) {
			continue
		}
		if !core.RunAsSubjectsEqual(ref.RunAs, target.RunAs) {
			continue
		}
		if !core.ExternalIdentityRefsEqual(ref.RunAsExternalIdentity, target.RunAsExternalIdentity) {
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
		strings.TrimSpace(left.App) == strings.TrimSpace(right.App) &&
		strings.TrimSpace(left.Operation) == strings.TrimSpace(right.Operation) &&
		config.ResolveConnectionAlias(strings.TrimSpace(left.Connection)) == config.ResolveConnectionAlias(strings.TrimSpace(right.Connection)) &&
		strings.TrimSpace(left.Instance) == strings.TrimSpace(right.Instance) &&
		left.CredentialMode == right.CredentialMode &&
		core.RunAsSubjectsEqual(left.RunAs, right.RunAs) &&
		core.ExternalIdentityRefsEqual(left.RunAsExternalIdentity, right.RunAsExternalIdentity)
}
