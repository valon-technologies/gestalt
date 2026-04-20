package bootstrap

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
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

type workflowBinding struct {
	providerName string
	operations   map[string]struct{}
}

type workflowRuntime struct {
	mu                     sync.RWMutex
	bindings               map[string]workflowBinding
	managedScheduleIDs     map[string]map[string]struct{}
	managedEventTriggerIDs map[string]map[string]struct{}
	workloadTokens         map[string]string
	providers              map[string]coreworkflow.Provider
	startupWaits           *startupWaitTracker
	invoker                invocation.Invoker
	executionRefs          *coredata.WorkflowExecutionRefService
}

func newWorkflowRuntime(cfg *config.Config) (*workflowRuntime, error) {
	runtime := &workflowRuntime{
		bindings:               make(map[string]workflowBinding, len(cfg.Plugins)),
		managedScheduleIDs:     make(map[string]map[string]struct{}, len(cfg.Plugins)),
		managedEventTriggerIDs: make(map[string]map[string]struct{}, len(cfg.Plugins)),
		workloadTokens:         make(map[string]string, len(cfg.Plugins)),
		providers:              map[string]coreworkflow.Provider{},
		startupWaits:           newStartupWaitTracker(),
	}
	for pluginName, entry := range cfg.Plugins {
		effective, err := cfg.EffectivePluginWorkflow(pluginName, entry)
		if err != nil {
			return nil, err
		}
		if !effective.Enabled {
			continue
		}
		allowed := make(map[string]struct{}, len(effective.Operations))
		for _, operation := range effective.Operations {
			allowed[operation] = struct{}{}
		}
		runtime.bindings[pluginName] = workflowBinding{
			providerName: effective.ProviderName,
			operations:   allowed,
		}
		managedSchedules := make(map[string]struct{}, len(effective.Schedules))
		for scheduleKey := range effective.Schedules {
			managedSchedules[workflowConfigScheduleID(pluginName, scheduleKey)] = struct{}{}
		}
		runtime.managedScheduleIDs[pluginName] = managedSchedules
		managedEventTriggers := make(map[string]struct{}, len(effective.EventTriggers))
		for triggerKey := range effective.EventTriggers {
			managedEventTriggers[workflowConfigEventTriggerID(pluginName, triggerKey)] = struct{}{}
		}
		runtime.managedEventTriggerIDs[pluginName] = managedEventTriggers
		runtime.workloadTokens[pluginName], err = workflowWorkloadToken()
		if err != nil {
			return nil, err
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

func (r *workflowRuntime) HasBinding(pluginName string) bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.bindings[pluginName]
	return ok
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

func (r *workflowRuntime) ResolvePlugin(pluginName string) (coreworkflow.Provider, map[string]struct{}, error) {
	if r == nil {
		return nil, nil, fmt.Errorf("workflow runtime is not configured")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	binding, ok := r.bindings[pluginName]
	if !ok {
		return nil, nil, fmt.Errorf("plugin %q does not have a workflow binding", pluginName)
	}
	provider, ok := r.providers[binding.providerName]
	if !ok || provider == nil {
		return nil, nil, fmt.Errorf("workflow provider %q is not available", binding.providerName)
	}
	return provider, maps.Clone(binding.operations), nil
}

func (r *workflowRuntime) ResolveBinding(pluginName string) (string, map[string]struct{}, error) {
	if r == nil {
		return "", nil, fmt.Errorf("workflow runtime is not configured")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	binding, ok := r.bindings[pluginName]
	if !ok {
		return "", nil, fmt.Errorf("plugin %q does not have a workflow binding", pluginName)
	}
	return binding.providerName, maps.Clone(binding.operations), nil
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

func (r *workflowRuntime) ManagedScheduleIDs(pluginName string) map[string]struct{} {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return maps.Clone(r.managedScheduleIDs[pluginName])
}

func (r *workflowRuntime) ManagedEventTriggerIDs(pluginName string) map[string]struct{} {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return maps.Clone(r.managedEventTriggerIDs[pluginName])
}

func (r *workflowRuntime) Allow(providerName, pluginName, operation string) bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	binding, ok := r.bindings[pluginName]
	if !ok {
		return false
	}
	if binding.providerName != strings.TrimSpace(providerName) {
		return false
	}
	_, ok = binding.operations[operation]
	return ok
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
	pluginName := strings.TrimSpace(req.PluginName)
	if pluginName == "" {
		return nil, fmt.Errorf("workflow plugin scope is required")
	}
	targetPluginName := strings.TrimSpace(req.Target.PluginName)
	if targetPluginName == "" {
		return nil, fmt.Errorf("workflow target plugin is required")
	}
	if targetPluginName != pluginName {
		return nil, fmt.Errorf("workflow target plugin %q is outside scoped plugin %q", targetPluginName, pluginName)
	}
	if !r.Allow(req.ProviderName, pluginName, req.Target.Operation) {
		return nil, fmt.Errorf("workflow target %q for plugin %q is not enabled", req.Target.Operation, pluginName)
	}
	principalValue := workflowWorkloadPrincipal(pluginName)
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
	subjectID := strings.TrimSpace(ref.SubjectID)
	permissions := principal.CompilePermissions(ref.Permissions)
	value := &principal.Principal{
		SubjectID:        subjectID,
		Scopes:           principal.PermissionPlugins(permissions),
		TokenPermissions: permissions,
	}
	switch {
	case principal.UserIDFromSubjectID(subjectID) != "":
		value.UserID = principal.UserIDFromSubjectID(subjectID)
		value.Kind = principal.KindUser
	case strings.HasPrefix(subjectID, string(principal.KindWorkload)+":"),
		principal.ManagedIdentityIDFromSubjectID(subjectID) != "",
		subjectID == principal.IdentitySubjectID():
		value.Kind = principal.KindWorkload
	}
	return value
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

func (r *workflowRuntime) AugmentAuthorization(cfg config.AuthorizationConfig) (config.AuthorizationConfig, error) {
	if r == nil || len(r.bindings) == 0 {
		return cfg, nil
	}
	cfg.Policies = maps.Clone(cfg.Policies)
	cfg.Workloads = maps.Clone(cfg.Workloads)
	if cfg.Workloads == nil {
		cfg.Workloads = map[string]config.WorkloadDef{}
	}
	for pluginName, binding := range r.bindings {
		workloadID := workflowWorkloadID(pluginName)
		if _, exists := cfg.Workloads[workloadID]; exists {
			return config.AuthorizationConfig{}, fmt.Errorf("authorization validation: managed workflow workload %q conflicts with configured workload", workloadID)
		}
		allow := make([]string, 0, len(binding.operations))
		for operation := range binding.operations {
			allow = append(allow, operation)
		}
		slices.Sort(allow)
		token, ok := r.workloadTokens[pluginName]
		if !ok {
			return config.AuthorizationConfig{}, fmt.Errorf("authorization validation: managed workflow workload %q is missing a token", workloadID)
		}
		cfg.Workloads[workloadID] = config.WorkloadDef{
			DisplayName: workflowWorkloadDisplayName(pluginName),
			Token:       token,
			Providers: map[string]config.WorkloadProviderDef{
				pluginName: {
					Allow: allow,
				},
			},
		}
	}
	return cfg, nil
}

func workflowWorkloadPrincipal(pluginName string) *principal.Principal {
	return &principal.Principal{
		Kind:      principal.KindWorkload,
		SubjectID: principal.WorkloadSubjectID(workflowWorkloadID(pluginName)),
		Source:    principal.SourceWorkloadToken,
	}
}

func workflowWorkloadID(pluginName string) string {
	return "workflow." + strings.TrimSpace(pluginName)
}

func workflowWorkloadDisplayName(pluginName string) string {
	pluginName = strings.TrimSpace(pluginName)
	if pluginName == "" {
		return "workflow"
	}
	return pluginName + " workflow"
}

func workflowWorkloadToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate managed workflow workload token: %w", err)
	}
	return "gst_wld_" + hex.EncodeToString(raw[:]), nil
}
