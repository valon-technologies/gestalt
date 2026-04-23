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
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

type agentRuntime struct {
	mu                  sync.RWMutex
	defaultProviderName string
	providers           map[string]coreagent.Provider
	invoker             invocation.Invoker
	runMetadata         *coredata.AgentRunMetadataService
	runEvents           *coredata.AgentRunEventService
}

func newAgentRuntime(cfg *config.Config) (*agentRuntime, error) {
	runtime := &agentRuntime{providers: map[string]coreagent.Provider{}}
	if cfg != nil {
		selectedProviderName, _, err := cfg.SelectedAgentProvider()
		if err == nil {
			runtime.defaultProviderName = strings.TrimSpace(selectedProviderName)
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
	return len(r.providers) > 0
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

func (r *agentRuntime) SetRunEvents(service *coredata.AgentRunEventService) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runEvents = service
}

func (r *agentRuntime) TrackRun(ctx context.Context, providerName string, req coreagent.StartRunRequest) error {
	if r == nil {
		return nil
	}
	runID := strings.TrimSpace(req.RunID)
	if runID == "" || len(req.Tools) == 0 {
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
		return fmt.Errorf("%w: agent execution principal is required", invocation.ErrInternal)
	}
	_, err := runMetadata.Put(ctx, &coreagent.ExecutionReference{
		ID:                  runID,
		ProviderName:        strings.TrimSpace(providerName),
		SubjectID:           subjectID,
		CredentialSubjectID: credentialSubjectID,
		IdempotencyKey:      strings.TrimSpace(req.IdempotencyKey),
		Permissions:         permissionsForAgentTools(req.Tools),
		Tools:               append([]coreagent.Tool(nil), req.Tools...),
	})
	return err
}

func (r *agentRuntime) DeleteTrackedRun(ctx context.Context, runID string) error {
	if r == nil || strings.TrimSpace(runID) == "" {
		return nil
	}
	r.mu.RLock()
	runMetadata := r.runMetadata
	r.mu.RUnlock()
	if runMetadata == nil {
		return nil
	}
	var err error
	if deleteErr := runMetadata.Delete(ctx, runID); deleteErr != nil {
		err = errors.Join(err, deleteErr)
	}
	r.mu.RLock()
	runEvents := r.runEvents
	r.mu.RUnlock()
	if runEvents != nil {
		if deleteErr := runEvents.DeleteByRun(ctx, runID); deleteErr != nil {
			err = errors.Join(err, deleteErr)
		}
	}
	return err
}

func (r *agentRuntime) RevokeTrackedRun(ctx context.Context, runID string) error {
	if r == nil || strings.TrimSpace(runID) == "" {
		return nil
	}
	r.mu.RLock()
	runMetadata := r.runMetadata
	r.mu.RUnlock()
	if runMetadata == nil {
		return nil
	}
	_, err := runMetadata.Revoke(ctx, runID, time.Now())
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
	runID := strings.TrimSpace(req.RunID)
	if runID == "" {
		return nil, fmt.Errorf("%w: run id is required", invocation.ErrAuthorizationDenied)
	}
	ref, err := runMetadata.Get(ctx, runID)
	if err != nil {
		if errors.Is(err, indexeddb.ErrNotFound) {
			return nil, fmt.Errorf("%w: agent run %q was not found", invocation.ErrAuthorizationDenied, runID)
		}
		return nil, fmt.Errorf("%w: agent run %q lookup failed: %v", invocation.ErrInternal, runID, err)
	}
	if ref == nil {
		return nil, fmt.Errorf("%w: agent run %q was not found", invocation.ErrAuthorizationDenied, runID)
	}
	if ref.RevokedAt != nil && !ref.RevokedAt.IsZero() {
		return nil, fmt.Errorf("%w: agent run %q is revoked", invocation.ErrAuthorizationDenied, runID)
	}
	if providerName := strings.TrimSpace(req.ProviderName); providerName != "" && strings.TrimSpace(ref.ProviderName) != providerName {
		return nil, fmt.Errorf("%w: agent run %q is not valid for provider %q", invocation.ErrAuthorizationDenied, runID, providerName)
	}
	tool, ok := lookupAgentTool(ref.Tools, req.ToolID)
	if !ok {
		return nil, fmt.Errorf("%w: agent tool %q is not available for run %q", invocation.ErrAuthorizationDenied, strings.TrimSpace(req.ToolID), runID)
	}
	principalValue := executionReferencePrincipal(ref.SubjectID, ref.CredentialSubjectID, ref.Permissions)
	if principalValue == nil || strings.TrimSpace(principalValue.SubjectID) == "" {
		return nil, fmt.Errorf("%w: agent execution principal is required", invocation.ErrInternal)
	}
	if connection := strings.TrimSpace(tool.Target.Connection); connection != "" {
		ctx = invocation.WithConnection(ctx, connection)
	}
	params := maps.Clone(req.Arguments)
	result, err := invoker.Invoke(ctx, principalValue, tool.Target.PluginName, strings.TrimSpace(tool.Target.Instance), tool.Target.Operation, params)
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

func (r *agentRuntime) EmitEvent(ctx context.Context, req coreagent.EmitEventRequest) (*coreagent.RunEvent, error) {
	if r == nil {
		return nil, fmt.Errorf("agent runtime is not configured")
	}
	r.mu.RLock()
	runMetadata := r.runMetadata
	runEvents := r.runEvents
	r.mu.RUnlock()
	if runMetadata == nil {
		return nil, fmt.Errorf("%w: agent run metadata is not configured", invocation.ErrInternal)
	}
	if runEvents == nil {
		return nil, fmt.Errorf("%w: agent run events are not configured", invocation.ErrInternal)
	}
	runID := strings.TrimSpace(req.RunID)
	if runID == "" {
		return nil, fmt.Errorf("%w: run id is required", invocation.ErrAuthorizationDenied)
	}
	eventType := strings.TrimSpace(req.Type)
	if eventType == "" {
		return nil, fmt.Errorf("%w: event type is required", invocation.ErrAuthorizationDenied)
	}
	ref, err := runMetadata.Get(ctx, runID)
	if err != nil {
		if errors.Is(err, indexeddb.ErrNotFound) {
			return nil, fmt.Errorf("%w: agent run %q was not found", invocation.ErrAuthorizationDenied, runID)
		}
		return nil, fmt.Errorf("%w: agent run %q lookup failed: %v", invocation.ErrInternal, runID, err)
	}
	if ref == nil {
		return nil, fmt.Errorf("%w: agent run %q was not found", invocation.ErrAuthorizationDenied, runID)
	}
	if ref.RevokedAt != nil && !ref.RevokedAt.IsZero() {
		return nil, fmt.Errorf("%w: agent run %q is revoked", invocation.ErrAuthorizationDenied, runID)
	}
	providerName := strings.TrimSpace(req.ProviderName)
	if providerName == "" {
		return nil, fmt.Errorf("%w: provider name is required", invocation.ErrAuthorizationDenied)
	}
	if strings.TrimSpace(ref.ProviderName) != providerName {
		return nil, fmt.Errorf("%w: agent run %q is not valid for provider %q", invocation.ErrAuthorizationDenied, runID, providerName)
	}
	event, err := runEvents.Append(ctx, coreagent.RunEvent{
		RunID:      runID,
		Type:       eventType,
		Source:     providerName,
		Visibility: strings.TrimSpace(req.Visibility),
		Data:       maps.Clone(req.Data),
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", invocation.ErrInternal, err)
	}
	return event, nil
}

func lookupAgentTool(tools []coreagent.Tool, toolID string) (coreagent.Tool, bool) {
	toolID = strings.TrimSpace(toolID)
	for _, tool := range tools {
		if strings.TrimSpace(tool.ID) == toolID {
			return tool, true
		}
	}
	return coreagent.Tool{}, false
}

func permissionsForAgentTools(tools []coreagent.Tool) []core.AccessPermission {
	if len(tools) == 0 {
		return nil
	}
	operationsByPlugin := make(map[string]map[string]struct{}, len(tools))
	for _, tool := range tools {
		pluginName := strings.TrimSpace(tool.Target.PluginName)
		operation := strings.TrimSpace(tool.Target.Operation)
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
