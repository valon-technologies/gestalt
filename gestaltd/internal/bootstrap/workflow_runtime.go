package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowprincipal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type workflowRuntime struct {
	mu                  sync.RWMutex
	defaultProviderName string
	providers           map[string]coreworkflow.Provider
	startupWaits        *startupWaitTracker
	invoker             invocation.Invoker
	agentManager        agentmanager.Service
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

func (r *workflowRuntime) SetAgentManager(agentManager agentmanager.Service) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agentManager = agentManager
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
	agentManager := r.agentManager
	r.mu.RUnlock()
	principalValue := principal.Canonicalized(principal.FromContext(ctx))
	target := req.Target
	callerAppName := ""
	if strings.TrimSpace(req.ExecutionRef) != "" {
		resolvedRef, err := r.resolveWorkflowExecutionRef(ctx, req)
		if err != nil {
			return nil, err
		}
		principalValue = workflowprincipal.RuntimePrincipalFromExecutionReference(resolvedRef)
		target = resolvedRef.Target
		callerAppName = strings.TrimSpace(resolvedRef.CallerAppName)
		ctx = workflowExecutionRefWithRunAsAudit(ctx, resolvedRef)
		if workflowExecutionRefAllowsInternalConnectionAccess(resolvedRef) {
			ctx = invocation.WithInternalConnectionAccess(ctx)
		}
	} else if principalValue == nil || strings.TrimSpace(principalValue.SubjectID) == "" {
		return nil, fmt.Errorf("%w: workflow execution principal is required when execution_ref is omitted", invocation.ErrInternal)
	}
	if len(target.Steps) == 0 {
		return nil, fmt.Errorf("workflow target steps are required")
	}
	return r.invokeWorkflowStepsWithExecutor(ctx, req, target, agentManager, invoker, principalValue, callerAppName)
}

func (r *workflowRuntime) resolveWorkflowExecutionRef(ctx context.Context, req coreworkflow.InvokeOperationRequest) (*coreworkflow.ExecutionReference, error) {
	refID := strings.TrimSpace(req.ExecutionRef)
	providerName := strings.TrimSpace(req.ProviderName)
	if providerName == "" {
		return nil, fmt.Errorf("%w: workflow execution provider is required", invocation.ErrInternal)
	}
	provider, err := r.ResolveProvider(providerName)
	if err != nil {
		return nil, fmt.Errorf("%w: workflow provider %q is not available: %v", invocation.ErrInternal, providerName, err)
	}
	store, ok := provider.(coreworkflow.ExecutionReferenceStore)
	if !ok {
		return nil, fmt.Errorf("%w: workflow provider %q does not support execution refs", invocation.ErrInternal, providerName)
	}
	ref, err := store.GetExecutionReference(ctx, refID)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) || status.Code(err) == codes.NotFound {
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
	if !coreworkflow.TargetsEqual(req.Target, ref.Target) {
		return nil, fmt.Errorf("%w: workflow execution ref %q target does not match the scheduled invocation", invocation.ErrAuthorizationDenied, refID)
	}
	return ref, nil
}

func workflowExecutionRefAllowsInternalConnectionAccess(ref *coreworkflow.ExecutionReference) bool {
	if ref == nil {
		return false
	}
	return strings.TrimSpace(ref.AuthSource) == "config" &&
		strings.TrimSpace(ref.SubjectKind) == "system" &&
		strings.TrimSpace(ref.SubjectID) == workflowConfigOwnerSubjectID() &&
		strings.TrimSpace(ref.CredentialSubjectID) == workflowConfigOwnerSubjectID()
}

func workflowExecutionRefWithRunAsAudit(ctx context.Context, ref *coreworkflow.ExecutionReference) context.Context {
	if ref == nil {
		return ctx
	}
	runAs := core.NormalizeRunAsSubject(ref.RunAs)
	if runAs == nil {
		return ctx
	}
	owner := core.NormalizeRunAsSubject(&core.RunAsSubject{
		SubjectID:           ref.SubjectID,
		SubjectKind:         ref.SubjectKind,
		CredentialSubjectID: ref.CredentialSubjectID,
		DisplayName:         ref.DisplayName,
		AuthSource:          ref.AuthSource,
	})
	return invocation.WithRunAsAudit(ctx, owner, runAs)
}
