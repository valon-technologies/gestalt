package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

type workflowRuntime struct {
	mu                  sync.RWMutex
	defaultProviderName string
	providers           map[string]coreworkflow.Provider
	startupWaits        *startupWaitTracker
	invoker             invocation.Invoker
	executionRefs       *coredata.WorkflowExecutionRefService
}

func newWorkflowRuntime(cfg *config.Config) (*workflowRuntime, error) {
	runtime := &workflowRuntime{
		providers:    map[string]coreworkflow.Provider{},
		startupWaits: newStartupWaitTracker(),
	}
	if cfg != nil {
		selectedProviderName, _, err := cfg.SelectedWorkflowProvider()
		if err == nil {
			runtime.defaultProviderName = strings.TrimSpace(selectedProviderName)
		}
	}
	return runtime, nil
}

func (r *workflowRuntime) InitProviderPlaceholders(defs map[string]*config.ProviderEntry) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.providers == nil {
		r.providers = map[string]coreworkflow.Provider{}
	}
	for name, entry := range defs {
		if entry == nil {
			continue
		}
		if _, exists := r.providers[name]; exists {
			continue
		}
		r.providers[name] = newStartupWorkflowProviderProxy(name, r.startupWaits)
	}
}

func (r *workflowRuntime) PublishProvider(name string, provider coreworkflow.Provider) {
	if r == nil || strings.TrimSpace(name) == "" || provider == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if proxy, ok := r.providers[name].(*startupWorkflowProviderProxy); ok {
		proxy.publish(provider)
	}
	r.providers[name] = provider
}

func (r *workflowRuntime) FailProvider(name string, err error) {
	if r == nil || strings.TrimSpace(name) == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if proxy, ok := r.providers[name].(*startupWorkflowProviderProxy); ok {
		proxy.fail(err)
	}
	delete(r.providers, name)
}

func (r *workflowRuntime) FailPendingProviders(err error) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for name, provider := range r.providers {
		proxy, ok := provider.(*startupWorkflowProviderProxy)
		if !ok {
			continue
		}
		proxy.fail(err)
		delete(r.providers, name)
	}
}

func (r *workflowRuntime) StartupWaitTracker() *startupWaitTracker {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.startupWaits
}

func (r *workflowRuntime) SetInvoker(invoker invocation.Invoker) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.invoker = invoker
}

func (r *workflowRuntime) SetExecutionRefs(service *coredata.WorkflowExecutionRefService) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.executionRefs = service
}

func (r *workflowRuntime) HasConfiguredProviders() bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.providers) > 0
}

func (r *workflowRuntime) ResolveProvider(name string) (coreworkflow.Provider, error) {
	if r == nil {
		return nil, fmt.Errorf("workflow runtime is not configured")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	provider, ok := r.providers[strings.TrimSpace(name)]
	if !ok || provider == nil {
		return nil, fmt.Errorf("workflow provider %q is not available", name)
	}
	return provider, nil
}

func (r *workflowRuntime) ResolveProviderSelection(name string) (string, coreworkflow.Provider, error) {
	if r == nil {
		return "", nil, fmt.Errorf("workflow runtime is not configured")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	selectedName := strings.TrimSpace(name)
	if selectedName == "" {
		selectedName = strings.TrimSpace(r.defaultProviderName)
	}
	if selectedName == "" {
		return "", nil, fmt.Errorf("workflow provider is required")
	}
	provider, ok := r.providers[selectedName]
	if !ok || provider == nil {
		return "", nil, fmt.Errorf("workflow provider %q is not available", selectedName)
	}
	return selectedName, provider, nil
}

func (r *workflowRuntime) ProviderNames() []string {
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

func (r *workflowRuntime) Invoke(ctx context.Context, req coreworkflow.InvokeOperationRequest) (*coreworkflow.InvokeOperationResponse, error) {
	if r == nil {
		return nil, fmt.Errorf("workflow runtime is not configured")
	}
	r.mu.RLock()
	invoker := r.invoker
	executionRefs := r.executionRefs
	r.mu.RUnlock()
	if invoker == nil {
		return nil, fmt.Errorf("workflow runtime invoker is not configured")
	}
	targetPluginName := strings.TrimSpace(req.Target.PluginName)
	if targetPluginName == "" {
		return nil, fmt.Errorf("workflow target plugin is required")
	}
	principalValue := principal.Canonicalized(principal.FromContext(ctx))
	target := req.Target
	invokeConnection := ""
	invokeInstance := ""
	if strings.TrimSpace(req.ExecutionRef) != "" {
		resolvedRef, err := resolveWorkflowExecutionRef(ctx, executionRefs, req)
		if err != nil {
			return nil, err
		}
		principalValue = workflowExecutionPrincipal(resolvedRef)
		target.PluginName = resolvedRef.Target.PluginName
		target.Operation = resolvedRef.Target.Operation
		target.Connection = resolvedRef.Target.Connection
		target.Instance = resolvedRef.Target.Instance
		invokeConnection = strings.TrimSpace(target.Connection)
		invokeInstance = strings.TrimSpace(target.Instance)
	} else if principalValue == nil || strings.TrimSpace(principalValue.SubjectID) == "" {
		principalValue = workflowRequestPrincipal(req)
		if principalValue == nil || strings.TrimSpace(principalValue.SubjectID) == "" {
			return nil, fmt.Errorf("%w: workflow execution principal is required when execution_ref is omitted", invocation.ErrInternal)
		}
	}
	if contextValue := workflowInvocationContext(req); len(contextValue) > 0 {
		ctx = invocation.WithWorkflowContext(ctx, contextValue)
	}
	if invokeConnection != "" {
		ctx = invocation.WithConnection(ctx, invokeConnection)
	}
	params := workflowInvocationParams(req)
	result, err := invoker.Invoke(ctx, principalValue, target.PluginName, invokeInstance, target.Operation, params)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return &coreworkflow.InvokeOperationResponse{}, nil
	}
	return &coreworkflow.InvokeOperationResponse{
		Status: result.Status,
		Body:   result.Body,
	}, nil
}

func resolveWorkflowExecutionRef(ctx context.Context, service *coredata.WorkflowExecutionRefService, req coreworkflow.InvokeOperationRequest) (*coreworkflow.ExecutionReference, error) {
	if service == nil {
		return nil, fmt.Errorf("%w: workflow execution refs are not configured", invocation.ErrInternal)
	}
	refID := strings.TrimSpace(req.ExecutionRef)
	ref, err := service.Get(ctx, refID)
	if err != nil {
		if errors.Is(err, indexeddb.ErrNotFound) {
			return nil, fmt.Errorf("%w: workflow execution ref %q was not found", invocation.ErrAuthorizationDenied, refID)
		}
		return nil, fmt.Errorf("%w: workflow execution ref %q lookup failed: %v", invocation.ErrInternal, refID, err)
	}
	if ref == nil {
		return nil, fmt.Errorf("%w: workflow execution ref %q was not found", invocation.ErrAuthorizationDenied, refID)
	}
	if ref.RevokedAt != nil && !ref.RevokedAt.IsZero() {
		return nil, fmt.Errorf("%w: workflow execution ref %q is revoked", invocation.ErrAuthorizationDenied, refID)
	}
	if strings.TrimSpace(ref.ProviderName) != strings.TrimSpace(req.ProviderName) {
		return nil, fmt.Errorf("%w: workflow execution ref %q is not valid for provider %q", invocation.ErrAuthorizationDenied, refID, req.ProviderName)
	}
	if strings.TrimSpace(ref.Target.PluginName) != strings.TrimSpace(req.Target.PluginName) ||
		strings.TrimSpace(ref.Target.Operation) != strings.TrimSpace(req.Target.Operation) ||
		strings.TrimSpace(ref.Target.Connection) != strings.TrimSpace(req.Target.Connection) ||
		strings.TrimSpace(ref.Target.Instance) != strings.TrimSpace(req.Target.Instance) {
		return nil, fmt.Errorf("%w: workflow execution ref %q target does not match the scheduled invocation", invocation.ErrAuthorizationDenied, refID)
	}
	return ref, nil
}

func workflowExecutionPrincipal(ref *coreworkflow.ExecutionReference) *principal.Principal {
	if ref == nil {
		return nil
	}
	permissions := principal.CompilePermissions(ref.Permissions)
	return principal.Canonicalize(&principal.Principal{
		SubjectID:        strings.TrimSpace(ref.SubjectID),
		Scopes:           principal.PermissionPlugins(permissions),
		TokenPermissions: permissions,
	})
}

func workflowRequestPrincipal(req coreworkflow.InvokeOperationRequest) *principal.Principal {
	subjectID := strings.TrimSpace(req.CreatedBy.SubjectID)
	if subjectID != "system:workflow-startup" {
		return nil
	}
	permissions := principal.CompilePermissions(workflowExecutionRefPermissionsForTarget(req.Target))
	value := &principal.Principal{
		SubjectID:        subjectID,
		DisplayName:      strings.TrimSpace(req.CreatedBy.DisplayName),
		Source:           principal.ParseSource(req.CreatedBy.AuthSource),
		Scopes:           principal.PermissionPlugins(permissions),
		TokenPermissions: permissions,
	}
	value.CredentialSubjectID = principal.IdentitySubjectID()
	return principal.Canonicalize(value)
}

func workflowInvocationParams(req coreworkflow.InvokeOperationRequest) map[string]any {
	params := maps.Clone(req.Target.Input)
	if req.Input != nil {
		if params == nil {
			params = map[string]any{}
		}
		maps.Copy(params, req.Input)
	}
	return params
}

func workflowInvocationContext(req coreworkflow.InvokeOperationRequest) map[string]any {
	ctxValue := map[string]any{}
	if runID := strings.TrimSpace(req.RunID); runID != "" {
		ctxValue["runId"] = runID
	}
	if providerName := strings.TrimSpace(req.ProviderName); providerName != "" {
		ctxValue["provider"] = providerName
	}
	target := workflowTargetContext(req.Target)
	if len(target) > 0 {
		ctxValue["target"] = target
	}
	trigger := workflowTriggerContext(req.Trigger)
	if len(trigger) > 0 {
		ctxValue["trigger"] = trigger
	}
	if req.Input != nil {
		ctxValue["input"] = maps.Clone(req.Input)
	}
	if req.Metadata != nil {
		ctxValue["metadata"] = maps.Clone(req.Metadata)
	}
	if createdBy := workflowActorContext(req.CreatedBy); len(createdBy) > 0 {
		ctxValue["createdBy"] = createdBy
	}
	if executionRef := strings.TrimSpace(req.ExecutionRef); executionRef != "" {
		ctxValue["executionRef"] = executionRef
	}
	return ctxValue
}

func workflowTargetContext(target coreworkflow.Target) map[string]any {
	value := map[string]any{}
	if pluginName := strings.TrimSpace(target.PluginName); pluginName != "" {
		value["pluginName"] = pluginName
	}
	if operation := strings.TrimSpace(target.Operation); operation != "" {
		value["operation"] = operation
	}
	if connection := strings.TrimSpace(target.Connection); connection != "" {
		value["connection"] = connection
	}
	if instance := strings.TrimSpace(target.Instance); instance != "" {
		value["instance"] = instance
	}
	if target.Input != nil {
		value["input"] = maps.Clone(target.Input)
	}
	return value
}

func workflowTriggerContext(trigger coreworkflow.RunTrigger) map[string]any {
	switch {
	case trigger.Schedule != nil:
		value := map[string]any{
			"kind":       "schedule",
			"scheduleId": trigger.Schedule.ScheduleID,
		}
		if trigger.Schedule.ScheduledFor != nil {
			value["scheduledFor"] = trigger.Schedule.ScheduledFor.UTC().Format(time.RFC3339Nano)
		}
		return value
	case trigger.Event != nil:
		value := map[string]any{
			"kind":      "event",
			"triggerId": trigger.Event.TriggerID,
		}
		if event := workflowEventContext(trigger.Event.Event); len(event) > 0 {
			value["event"] = event
		}
		return value
	case trigger.Manual:
		return map[string]any{"kind": "manual"}
	default:
		return nil
	}
}

func workflowEventContext(event coreworkflow.Event) map[string]any {
	value := map[string]any{}
	if event.ID != "" {
		value["id"] = event.ID
	}
	if event.Source != "" {
		value["source"] = event.Source
	}
	if event.SpecVersion != "" {
		value["specVersion"] = event.SpecVersion
	}
	if event.Type != "" {
		value["type"] = event.Type
	}
	if event.Subject != "" {
		value["subject"] = event.Subject
	}
	if event.Time != nil {
		value["time"] = event.Time.UTC().Format(time.RFC3339Nano)
	}
	if event.DataContentType != "" {
		value["dataContentType"] = event.DataContentType
	}
	if event.Data != nil {
		value["data"] = maps.Clone(event.Data)
	}
	if event.Extensions != nil {
		value["extensions"] = maps.Clone(event.Extensions)
	}
	return value
}

func workflowActorContext(actor coreworkflow.Actor) map[string]any {
	value := map[string]any{}
	if actor.SubjectID != "" {
		value["subjectId"] = actor.SubjectID
	}
	if actor.SubjectKind != "" {
		value["subjectKind"] = actor.SubjectKind
	}
	if actor.DisplayName != "" {
		value["displayName"] = actor.DisplayName
	}
	if actor.AuthSource != "" {
		value["authSource"] = actor.AuthSource
	}
	return value
}
