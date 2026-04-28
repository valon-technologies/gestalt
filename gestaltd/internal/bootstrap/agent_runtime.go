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
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/agentmanager"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/observability"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"go.opentelemetry.io/otel/attribute"
)

type agentRuntime struct {
	mu                  sync.RWMutex
	defaultProviderName string
	configuredProviders map[string]struct{}
	providers           map[string]coreagent.Provider
	invoker             invocation.Invoker
	runMetadata         *coredata.AgentRunMetadataService
	toolSearcher        agentToolSearcher
	toolMergeMu         sync.Mutex
}

type agentToolSearcher interface {
	SearchTools(ctx context.Context, p *principal.Principal, req coreagent.SearchToolsRequest) (*coreagent.SearchToolsResponse, error)
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

func (r *agentRuntime) SetRunMetadata(service *coredata.AgentRunMetadataService) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runMetadata = service
}

func (r *agentRuntime) SetToolSearcher(searcher agentToolSearcher) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.toolSearcher = searcher
}

func (r *agentRuntime) TrackTurn(ctx context.Context, providerName string, req coreagent.CreateTurnRequest) error {
	if r == nil {
		return nil
	}
	turnID := strings.TrimSpace(req.TurnID)
	if turnID == "" {
		return nil
	}
	r.mu.RLock()
	runMetadata := r.runMetadata
	r.mu.RUnlock()
	if runMetadata == nil {
		return fmt.Errorf("%w: agent run metadata is not configured", invocation.ErrInternal)
	}
	subjectID := ""
	credentialSubjectID := ""
	if p := principal.Canonicalized(principal.FromContext(ctx)); p != nil {
		subjectID = strings.TrimSpace(p.SubjectID)
		credentialSubjectID = strings.TrimSpace(principal.EffectiveCredentialSubjectID(p))
	}
	if subjectID == "" {
		subjectID = strings.TrimSpace(req.CreatedBy.SubjectID)
	}
	if credentialSubjectID == "" && principal.IsSystemSubjectID(subjectID) {
		credentialSubjectID = subjectID
	}
	if subjectID == "" {
		if len(req.Tools) == 0 {
			return nil
		}
		return fmt.Errorf("%w: agent execution principal is required", invocation.ErrInternal)
	}
	var existingRef *coreagent.ExecutionReference
	existing, err := runMetadata.Get(ctx, turnID)
	if err == nil {
		existingRef = existing
	} else if !errors.Is(err, indexeddb.ErrNotFound) {
		return fmt.Errorf("%w: agent turn %q metadata lookup failed: %v", invocation.ErrInternal, turnID, err)
	}
	principalValue := principal.Canonicalized(principal.FromContext(ctx))
	var permissions []core.AccessPermission
	if principalValue != nil {
		permissions = principal.PermissionsToAccessPermissions(principalValue.TokenPermissions)
	}
	if permissions == nil && len(req.Tools) > 0 {
		permissions = permissionsForAgentTools(req.Tools)
	}
	if existingRef != nil {
		permissions = append([]core.AccessPermission(nil), existingRef.Permissions...)
	}
	startedAt := time.Now()
	attrs := []attribute.KeyValue{
		observability.AttrAgentProvider.String(strings.TrimSpace(providerName)),
		observability.AttrAgentOperation.String("track_turn"),
	}
	metadataCtx, span := observability.StartSpan(ctx, "agent.run_metadata.write", attrs...)
	_, err = runMetadata.Put(metadataCtx, &coreagent.ExecutionReference{
		ID:                  turnID,
		SessionID:           strings.TrimSpace(req.SessionID),
		ProviderName:        strings.TrimSpace(providerName),
		SubjectID:           subjectID,
		CredentialSubjectID: credentialSubjectID,
		IdempotencyKey:      strings.TrimSpace(req.IdempotencyKey),
		Permissions:         permissions,
		ToolRefs:            append([]coreagent.ToolRef(nil), req.ToolRefs...),
		ToolSource:          normalizeAgentToolSource(req.ToolSource),
		Tools:               append([]coreagent.Tool(nil), req.Tools...),
	})
	observability.EndSpan(span, err)
	observability.RecordAgentRunMetadataWrite(metadataCtx, startedAt, err != nil, attrs...)
	return err
}

func (r *agentRuntime) DeleteTrackedTurn(ctx context.Context, turnID string) error {
	if r == nil || strings.TrimSpace(turnID) == "" {
		return nil
	}
	r.mu.RLock()
	runMetadata := r.runMetadata
	r.mu.RUnlock()
	if runMetadata == nil {
		return nil
	}
	return runMetadata.Delete(ctx, turnID)
}

func (r *agentRuntime) RevokeTrackedTurn(ctx context.Context, turnID string) error {
	if r == nil || strings.TrimSpace(turnID) == "" {
		return nil
	}
	r.mu.RLock()
	runMetadata := r.runMetadata
	r.mu.RUnlock()
	if runMetadata == nil {
		return nil
	}
	_, err := runMetadata.Revoke(ctx, turnID, time.Now())
	if err != nil {
		if errors.Is(err, indexeddb.ErrNotFound) {
			return nil
		}
		return err
	}
	return nil
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
	runMetadata := r.runMetadata
	r.mu.RUnlock()
	if invoker == nil {
		return nil, fmt.Errorf("%w: agent runtime invoker is not configured", invocation.ErrInternal)
	}
	if runMetadata == nil {
		return nil, fmt.Errorf("%w: agent run metadata is not configured", invocation.ErrInternal)
	}
	turnID := strings.TrimSpace(req.TurnID)
	if turnID == "" {
		return nil, fmt.Errorf("%w: turn id is required", invocation.ErrAuthorizationDenied)
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("%w: session id is required", invocation.ErrAuthorizationDenied)
	}
	ref, err := runMetadata.Get(ctx, turnID)
	if err != nil {
		if errors.Is(err, indexeddb.ErrNotFound) {
			return nil, fmt.Errorf("%w: agent turn %q was not found", invocation.ErrAuthorizationDenied, turnID)
		}
		return nil, fmt.Errorf("%w: agent turn %q lookup failed: %v", invocation.ErrInternal, turnID, err)
	}
	if ref == nil {
		return nil, fmt.Errorf("%w: agent turn %q was not found", invocation.ErrAuthorizationDenied, turnID)
	}
	if ref.RevokedAt != nil && !ref.RevokedAt.IsZero() {
		return nil, fmt.Errorf("%w: agent turn %q is revoked", invocation.ErrAuthorizationDenied, turnID)
	}
	if providerName := strings.TrimSpace(req.ProviderName); providerName != "" && strings.TrimSpace(ref.ProviderName) != providerName {
		return nil, fmt.Errorf("%w: agent turn %q is not valid for provider %q", invocation.ErrAuthorizationDenied, turnID, providerName)
	}
	if strings.TrimSpace(ref.SessionID) != sessionID {
		return nil, fmt.Errorf("%w: agent turn %q is not valid for session %q", invocation.ErrAuthorizationDenied, turnID, sessionID)
	}
	tool, ok := lookupAgentTool(ref.Tools, req.ToolID)
	if !ok {
		return nil, fmt.Errorf("%w: agent tool %q is not available for turn %q", invocation.ErrAuthorizationDenied, strings.TrimSpace(req.ToolID), turnID)
	}
	principalValue := executionReferencePrincipal(ref.SubjectID, ref.CredentialSubjectID, ref.Permissions)
	if principalValue == nil || strings.TrimSpace(principalValue.SubjectID) == "" {
		return nil, fmt.Errorf("%w: agent execution principal is required", invocation.ErrInternal)
	}
	if connection := strings.TrimSpace(tool.Target.Connection); connection != "" {
		ctx = invocation.WithConnection(ctx, connection)
	}
	if mode := tool.Target.CredentialMode; mode != "" {
		ctx = invocation.WithCredentialModeOverride(ctx, mode)
	}
	params := maps.Clone(req.Arguments)
	result, err := invoker.Invoke(ctx, principalValue, tool.Target.Plugin, strings.TrimSpace(tool.Target.Instance), tool.Target.Operation, params)
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
	runMetadata := r.runMetadata
	searcher := r.toolSearcher
	r.mu.RUnlock()
	if runMetadata == nil {
		return nil, fmt.Errorf("%w: agent run metadata is not configured", invocation.ErrInternal)
	}
	if searcher == nil {
		return nil, fmt.Errorf("%w: agent tool search is not configured", invocation.ErrInternal)
	}
	turnID := strings.TrimSpace(req.TurnID)
	if turnID == "" {
		return nil, fmt.Errorf("%w: turn id is required", invocation.ErrAuthorizationDenied)
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("%w: session id is required", invocation.ErrAuthorizationDenied)
	}
	ref, err := runMetadata.Get(ctx, turnID)
	if err != nil {
		if errors.Is(err, indexeddb.ErrNotFound) {
			return nil, fmt.Errorf("%w: agent turn %q was not found", invocation.ErrAuthorizationDenied, turnID)
		}
		return nil, fmt.Errorf("%w: agent turn %q lookup failed: %v", invocation.ErrInternal, turnID, err)
	}
	if ref == nil {
		return nil, fmt.Errorf("%w: agent turn %q was not found", invocation.ErrAuthorizationDenied, turnID)
	}
	if ref.RevokedAt != nil && !ref.RevokedAt.IsZero() {
		return nil, fmt.Errorf("%w: agent turn %q is revoked", invocation.ErrAuthorizationDenied, turnID)
	}
	if providerName := strings.TrimSpace(req.ProviderName); providerName != "" && strings.TrimSpace(ref.ProviderName) != providerName {
		return nil, fmt.Errorf("%w: agent turn %q is not valid for provider %q", invocation.ErrAuthorizationDenied, turnID, providerName)
	}
	if strings.TrimSpace(ref.SessionID) != sessionID {
		return nil, fmt.Errorf("%w: agent turn %q is not valid for session %q", invocation.ErrAuthorizationDenied, turnID, sessionID)
	}
	principalValue := executionReferencePrincipal(ref.SubjectID, ref.CredentialSubjectID, ref.Permissions)
	if principalValue == nil || strings.TrimSpace(principalValue.SubjectID) == "" {
		return nil, fmt.Errorf("%w: agent execution principal is required", invocation.ErrInternal)
	}
	resp, err := searcher.SearchTools(ctx, principalValue, coreagent.SearchToolsRequest{
		ProviderName:   strings.TrimSpace(ref.ProviderName),
		SessionID:      sessionID,
		TurnID:         turnID,
		Query:          strings.TrimSpace(req.Query),
		MaxResults:     req.MaxResults,
		CandidateLimit: req.CandidateLimit,
		LoadRefs:       append([]coreagent.ToolRef(nil), req.LoadRefs...),
		ToolRefs:       append([]coreagent.ToolRef(nil), ref.ToolRefs...),
		ToolSource:     normalizeAgentToolSource(ref.ToolSource),
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return &coreagent.SearchToolsResponse{}, nil
	}
	if err := validateAgentToolSearchResults(principalValue, ref.ToolRefs, normalizeAgentToolSource(ref.ToolSource), resp.Tools); err != nil {
		return nil, err
	}
	if err := validateAgentToolSearchCandidates(principalValue, ref.ToolRefs, normalizeAgentToolSource(ref.ToolSource), resp.Candidates); err != nil {
		return nil, err
	}
	if len(resp.Tools) == 0 {
		return &coreagent.SearchToolsResponse{
			Candidates: append([]coreagent.ToolCandidate(nil), resp.Candidates...),
			HasMore:    resp.HasMore,
		}, nil
	}

	r.toolMergeMu.Lock()
	defer r.toolMergeMu.Unlock()
	latestRef, err := runMetadata.Get(ctx, turnID)
	if err != nil {
		if errors.Is(err, indexeddb.ErrNotFound) {
			return nil, fmt.Errorf("%w: agent turn %q was not found", invocation.ErrAuthorizationDenied, turnID)
		}
		return nil, fmt.Errorf("%w: agent turn %q lookup failed: %v", invocation.ErrInternal, turnID, err)
	}
	if latestRef == nil {
		return nil, fmt.Errorf("%w: agent turn %q was not found", invocation.ErrAuthorizationDenied, turnID)
	}
	if latestRef.RevokedAt != nil && !latestRef.RevokedAt.IsZero() {
		return nil, fmt.Errorf("%w: agent turn %q is revoked", invocation.ErrAuthorizationDenied, turnID)
	}
	if strings.TrimSpace(latestRef.ProviderName) != strings.TrimSpace(ref.ProviderName) {
		return nil, fmt.Errorf("%w: agent turn %q changed provider while searching tools", invocation.ErrAuthorizationDenied, turnID)
	}
	if strings.TrimSpace(latestRef.SessionID) != sessionID {
		return nil, fmt.Errorf("%w: agent turn %q is not valid for session %q", invocation.ErrAuthorizationDenied, turnID, sessionID)
	}
	latestPrincipal := executionReferencePrincipal(latestRef.SubjectID, latestRef.CredentialSubjectID, latestRef.Permissions)
	if latestPrincipal == nil || strings.TrimSpace(latestPrincipal.SubjectID) == "" {
		return nil, fmt.Errorf("%w: agent execution principal is required", invocation.ErrInternal)
	}
	if err := validateAgentToolSearchResults(latestPrincipal, latestRef.ToolRefs, normalizeAgentToolSource(latestRef.ToolSource), resp.Tools); err != nil {
		return nil, err
	}
	if err := validateAgentToolSearchCandidates(latestPrincipal, latestRef.ToolRefs, normalizeAgentToolSource(latestRef.ToolSource), resp.Candidates); err != nil {
		return nil, err
	}
	latestRef.Tools = mergeAgentTools(latestRef.Tools, resp.Tools)
	if _, err := runMetadata.Put(ctx, latestRef); err != nil {
		return nil, err
	}
	return &coreagent.SearchToolsResponse{
		Tools:      append([]coreagent.Tool(nil), resp.Tools...),
		Candidates: append([]coreagent.ToolCandidate(nil), resp.Candidates...),
		HasMore:    resp.HasMore,
	}, nil
}

func lookupAgentTool(tools []coreagent.Tool, toolID string) (coreagent.Tool, bool) {
	toolID = strings.TrimSpace(toolID)
	for i := range tools {
		if strings.TrimSpace(tools[i].ID) == toolID {
			return tools[i], true
		}
	}
	return coreagent.Tool{}, false
}

func mergeAgentTools(existing []coreagent.Tool, loaded []coreagent.Tool) []coreagent.Tool {
	if len(existing) == 0 {
		return append([]coreagent.Tool(nil), loaded...)
	}
	out := append([]coreagent.Tool(nil), existing...)
	seen := make(map[string]struct{}, len(out)+len(loaded))
	for i := range out {
		if id := strings.TrimSpace(out[i].ID); id != "" {
			seen[id] = struct{}{}
		}
	}
	for i := range loaded {
		id := strings.TrimSpace(loaded[i].ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, loaded[i])
	}
	return out
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

func validateAgentToolSearchCandidates(p *principal.Principal, refs []coreagent.ToolRef, source coreagent.ToolSourceMode, candidates []coreagent.ToolCandidate) error {
	if source != coreagent.ToolSourceModeNativeSearch {
		return fmt.Errorf("%w: unsupported agent tool source %q", invocation.ErrInternal, source)
	}
	for i := range candidates {
		ref := candidates[i].Ref
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
	targetConnection := config.ResolveConnectionAlias(strings.TrimSpace(target.Connection))
	for _, ref := range refs {
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
		Plugin:         candidate.Plugin,
		Operation:      candidate.Operation,
		Connection:     candidate.Connection,
		Instance:       candidate.Instance,
		CredentialMode: candidate.CredentialMode,
	}
	return agentToolMatchesRefs(target, refs)
}

func permissionsForAgentTools(tools []coreagent.Tool) []core.AccessPermission {
	if len(tools) == 0 {
		return nil
	}
	operationsByPlugin := make(map[string]map[string]struct{}, len(tools))
	for i := range tools {
		pluginName := strings.TrimSpace(tools[i].Target.Plugin)
		operation := strings.TrimSpace(tools[i].Target.Operation)
		if pluginName == "" || operation == "" {
			continue
		}
		if operationsByPlugin[pluginName] == nil {
			operationsByPlugin[pluginName] = map[string]struct{}{}
		}
		operationsByPlugin[pluginName][operation] = struct{}{}
	}
	if len(operationsByPlugin) == 0 {
		return nil
	}
	plugins := make([]string, 0, len(operationsByPlugin))
	for pluginName := range operationsByPlugin {
		plugins = append(plugins, pluginName)
	}
	sort.Strings(plugins)

	permissions := make([]core.AccessPermission, 0, len(plugins))
	for _, pluginName := range plugins {
		operationSet := operationsByPlugin[pluginName]
		operations := make([]string, 0, len(operationSet))
		for operation := range operationSet {
			operations = append(operations, operation)
		}
		sort.Strings(operations)
		permissions = append(permissions, core.AccessPermission{
			Plugin:     pluginName,
			Operations: operations,
		})
	}
	return permissions
}
