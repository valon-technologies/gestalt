package bootstrap

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	pluginservice "github.com/valon-technologies/gestalt/server/services/plugins"
)

type startupWaitTracker struct {
	mu            sync.Mutex
	pluginWaits   map[[2]string]int
	workflowWaits map[[2]string]int
}

func newStartupWaitTracker() *startupWaitTracker {
	return &startupWaitTracker{
		pluginWaits:   make(map[[2]string]int),
		workflowWaits: make(map[[2]string]int),
	}
}

func (t *startupWaitTracker) beginPluginWait(pluginName, providerName string) (func(), error) {
	if t == nil || pluginName == "" || providerName == "" {
		return func() {}, nil
	}
	pluginKey := [2]string{pluginName, providerName}
	workflowKey := [2]string{providerName, pluginName}

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.workflowWaits[workflowKey] > 0 {
		return nil, fmt.Errorf("workflow startup dependency cycle between plugin %q and workflow provider %q", pluginName, providerName)
	}
	t.pluginWaits[pluginKey]++
	return func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		if remaining := t.pluginWaits[pluginKey] - 1; remaining > 0 {
			t.pluginWaits[pluginKey] = remaining
		} else {
			delete(t.pluginWaits, pluginKey)
		}
	}, nil
}

func (t *startupWaitTracker) beginWorkflowWait(providerName, pluginName string) (func(), error) {
	if t == nil || pluginName == "" || providerName == "" {
		return func() {}, nil
	}
	pluginKey := [2]string{pluginName, providerName}
	workflowKey := [2]string{providerName, pluginName}

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pluginWaits[pluginKey] > 0 {
		return nil, fmt.Errorf("workflow startup dependency cycle between plugin %q and workflow provider %q", pluginName, providerName)
	}
	t.workflowWaits[workflowKey]++
	return func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		if remaining := t.workflowWaits[workflowKey] - 1; remaining > 0 {
			t.workflowWaits[workflowKey] = remaining
		} else {
			delete(t.workflowWaits, workflowKey)
		}
	}, nil
}

type startupProviderProxy struct {
	spec             pluginservice.StaticProviderSpec
	operationRouting startupOperationRouting
	tracker          *startupWaitTracker

	ready chan struct{}
	once  sync.Once

	mu       sync.RWMutex
	provider core.Provider
	err      error
}

func newStartupProviderProxy(spec pluginservice.StaticProviderSpec, operationRouting startupOperationRouting, tracker *startupWaitTracker) *startupProviderProxy {
	operationRouting.connections = maps.Clone(operationRouting.connections)
	return &startupProviderProxy{
		spec:             spec,
		operationRouting: operationRouting,
		tracker:          tracker,
		ready:            make(chan struct{}),
	}
}

func (p *startupProviderProxy) publish(provider core.Provider) {
	p.finish(provider, nil)
}

func (p *startupProviderProxy) fail(err error) {
	p.finish(nil, err)
}

func (p *startupProviderProxy) finish(provider core.Provider, err error) {
	if err == nil && provider == nil {
		err = fmt.Errorf("provider %q is not available", p.spec.Name)
	}
	p.once.Do(func() {
		p.mu.Lock()
		p.provider = provider
		p.err = err
		p.mu.Unlock()
		close(p.ready)
	})
}

func (p *startupProviderProxy) await(ctx context.Context) (core.Provider, error) {
	select {
	case <-p.ready:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.err != nil {
		return nil, p.err
	}
	if p.provider == nil {
		return nil, fmt.Errorf("provider %q is not available", p.spec.Name)
	}
	return p.provider, nil
}

func (p *startupProviderProxy) resolved() core.Provider {
	select {
	case <-p.ready:
		p.mu.RLock()
		defer p.mu.RUnlock()
		return p.provider
	default:
		return nil
	}
}

func (p *startupProviderProxy) Name() string        { return p.spec.Name }
func (p *startupProviderProxy) DisplayName() string { return p.spec.DisplayName }
func (p *startupProviderProxy) Description() string { return p.spec.Description }
func (p *startupProviderProxy) ConnectionMode() core.ConnectionMode {
	return p.spec.ConnectionMode
}
func (p *startupProviderProxy) SupportsSessionCatalog() bool {
	provider := p.resolved()
	if provider == nil {
		return true
	}
	return core.SupportsSessionCatalog(provider)
}
func (p *startupProviderProxy) SupportsHTTPSubject() bool {
	provider := p.resolved()
	if provider == nil {
		return true
	}
	return core.SupportsHTTPSubject(provider)
}
func (p *startupProviderProxy) Catalog() *catalog.Catalog {
	if p.spec.Catalog == nil {
		return nil
	}
	return p.spec.Catalog.Clone()
}

func (p *startupProviderProxy) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	done, err := p.beginWorkflowWait(ctx)
	if err != nil {
		return nil, err
	}
	defer done()

	provider, err := p.await(ctx)
	if err != nil {
		return nil, err
	}
	return provider.Execute(ctx, operation, params, token)
}

func (p *startupProviderProxy) ResolveHTTPSubject(ctx context.Context, req *core.HTTPSubjectResolveRequest) (*core.HTTPResolvedSubject, error) {
	done, err := p.beginWorkflowWait(ctx)
	if err != nil {
		return nil, err
	}
	defer done()

	provider, err := p.await(ctx)
	if err != nil {
		return nil, err
	}
	subject, _, err := core.ResolveHTTPSubject(ctx, provider, req)
	return subject, err
}

func (p *startupProviderProxy) CallTool(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error) {
	done, err := p.beginWorkflowWait(ctx)
	if err != nil {
		return nil, err
	}
	defer done()

	provider, err := p.await(ctx)
	if err != nil {
		return nil, err
	}
	caller, ok := provider.(interface {
		CallTool(context.Context, string, map[string]any) (*mcpgo.CallToolResult, error)
	})
	if !ok {
		return nil, core.ErrMCPOnly
	}
	return caller.CallTool(ctx, name, args)
}

func (p *startupProviderProxy) CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error) {
	done, err := p.beginWorkflowWait(ctx)
	if err != nil {
		return nil, err
	}
	defer done()

	provider, err := p.await(ctx)
	if err != nil {
		return nil, err
	}
	cat, scoped, err := core.CatalogForRequest(ctx, provider, token)
	if !scoped {
		return nil, core.WrapSessionCatalogUnsupported(fmt.Errorf("provider %q does not support session catalogs", p.spec.Name))
	}
	return cat, err
}

func (p *startupProviderProxy) ConnectionForOperation(operation string) string {
	provider := p.resolved()
	if provider != nil {
		return provider.ConnectionForOperation(operation)
	}
	return p.operationRouting.connections[operation]
}

func (p *startupProviderProxy) ResolveConnectionForOperation(operation string, params map[string]any) (string, error) {
	provider := p.resolved()
	if provider != nil {
		if resolver, ok := provider.(core.OperationConnectionResolver); ok {
			return resolver.ResolveConnectionForOperation(operation, params)
		}
		return provider.ConnectionForOperation(operation), nil
	}
	if p.operationRouting.resolver != nil {
		return p.operationRouting.resolver.ResolveConnectionForOperation(operation, params)
	}
	return p.operationRouting.connections[operation], nil
}

func (p *startupProviderProxy) OperationConnectionOverrideAllowed(operation string, params map[string]any) bool {
	provider := p.resolved()
	if provider != nil {
		if policy, ok := provider.(core.OperationConnectionOverridePolicy); ok {
			return policy.OperationConnectionOverrideAllowed(operation, params)
		}
		return false
	}
	if p.operationRouting.overridePolicy != nil {
		return p.operationRouting.overridePolicy.OperationConnectionOverrideAllowed(operation, params)
	}
	return false
}

func (p *startupProviderProxy) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return maps.Clone(p.spec.ConnectionParams)
}

func (p *startupProviderProxy) CredentialFields() []core.CredentialFieldDef {
	return slices.Clone(p.spec.CredentialFields)
}

func (p *startupProviderProxy) AuthTypes() []string {
	return slices.Clone(p.spec.AuthTypes)
}

func (p *startupProviderProxy) DiscoveryConfig() *core.DiscoveryConfig {
	if p.spec.DiscoveryConfig == nil {
		return nil
	}
	value := *p.spec.DiscoveryConfig
	if len(value.Metadata) > 0 {
		value.Metadata = maps.Clone(value.Metadata)
	}
	return &value
}

func (p *startupProviderProxy) SupportsManualAuth() bool {
	if provider := p.resolved(); provider != nil {
		return slices.Contains(provider.AuthTypes(), "manual")
	}
	return slices.Contains(p.spec.AuthTypes, "manual")
}

func (p *startupProviderProxy) Close() error {
	provider := p.resolved()
	if provider == nil {
		return nil
	}
	closer, ok := provider.(interface{ Close() error })
	if !ok {
		return nil
	}
	return closer.Close()
}

func (p *startupProviderProxy) beginWorkflowWait(ctx context.Context) (func(), error) {
	if p == nil || p.tracker == nil {
		return func() {}, nil
	}
	workflow := invocation.WorkflowContextFromContext(ctx)
	providerName, _ := workflow["provider"].(string)
	return p.tracker.beginWorkflowWait(providerName, p.spec.Name)
}

type startupWorkflowProviderProxy struct {
	providerName string
	tracker      *startupWaitTracker

	ready chan struct{}
	once  sync.Once

	mu       sync.RWMutex
	provider coreworkflow.Provider
	err      error

	pendingExecutionRefs map[string]*coreworkflow.ExecutionReference
}

func newStartupWorkflowProviderProxy(providerName string, tracker *startupWaitTracker) *startupWorkflowProviderProxy {
	return &startupWorkflowProviderProxy{
		providerName:         providerName,
		tracker:              tracker,
		ready:                make(chan struct{}),
		pendingExecutionRefs: map[string]*coreworkflow.ExecutionReference{},
	}
}

func (p *startupWorkflowProviderProxy) publish(provider coreworkflow.Provider) {
	p.finish(provider, nil)
}

func (p *startupWorkflowProviderProxy) fail(err error) {
	p.finish(nil, err)
}

func (p *startupWorkflowProviderProxy) finish(provider coreworkflow.Provider, err error) {
	if err == nil && provider == nil {
		err = fmt.Errorf("workflow provider is not available")
	}
	p.once.Do(func() {
		p.mu.Lock()
		p.provider = provider
		p.err = err
		p.mu.Unlock()
		close(p.ready)
	})
}

func (p *startupWorkflowProviderProxy) await(ctx context.Context) (coreworkflow.Provider, error) {
	select {
	case <-p.ready:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.err != nil {
		return nil, p.err
	}
	if p.provider == nil {
		return nil, fmt.Errorf("workflow provider is not available")
	}
	return p.provider, nil
}

func (p *startupWorkflowProviderProxy) StartRun(ctx context.Context, req coreworkflow.StartRunRequest) (*coreworkflow.Run, error) {
	provider, err := p.awaitForPlugin(ctx, startupWorkflowTargetPluginName(req.Target))
	if err != nil {
		return nil, err
	}
	return provider.StartRun(ctx, req)
}

func (p *startupWorkflowProviderProxy) GetRun(ctx context.Context, req coreworkflow.GetRunRequest) (*coreworkflow.Run, error) {
	provider, err := p.awaitForContextPlugin(ctx)
	if err != nil {
		return nil, err
	}
	return provider.GetRun(ctx, req)
}

func (p *startupWorkflowProviderProxy) ListRuns(ctx context.Context, req coreworkflow.ListRunsRequest) ([]*coreworkflow.Run, error) {
	provider, err := p.awaitForContextPlugin(ctx)
	if err != nil {
		return nil, err
	}
	return provider.ListRuns(ctx, req)
}

func (p *startupWorkflowProviderProxy) CancelRun(ctx context.Context, req coreworkflow.CancelRunRequest) (*coreworkflow.Run, error) {
	provider, err := p.awaitForContextPlugin(ctx)
	if err != nil {
		return nil, err
	}
	return provider.CancelRun(ctx, req)
}

func (p *startupWorkflowProviderProxy) SignalRun(ctx context.Context, req coreworkflow.SignalRunRequest) (*coreworkflow.SignalRunResponse, error) {
	provider, err := p.awaitForContextPlugin(ctx)
	if err != nil {
		return nil, err
	}
	return provider.SignalRun(ctx, req)
}

func (p *startupWorkflowProviderProxy) SignalOrStartRun(ctx context.Context, req coreworkflow.SignalOrStartRunRequest) (*coreworkflow.SignalRunResponse, error) {
	provider, err := p.awaitForPlugin(ctx, startupWorkflowTargetPluginName(req.Target))
	if err != nil {
		return nil, err
	}
	return provider.SignalOrStartRun(ctx, req)
}

func (p *startupWorkflowProviderProxy) UpsertSchedule(ctx context.Context, req coreworkflow.UpsertScheduleRequest) (*coreworkflow.Schedule, error) {
	provider, err := p.awaitForPlugin(ctx, startupWorkflowTargetPluginName(req.Target))
	if err != nil {
		return nil, err
	}
	return provider.UpsertSchedule(ctx, req)
}

func (p *startupWorkflowProviderProxy) GetSchedule(ctx context.Context, req coreworkflow.GetScheduleRequest) (*coreworkflow.Schedule, error) {
	provider, err := p.awaitForContextPlugin(ctx)
	if err != nil {
		return nil, err
	}
	return provider.GetSchedule(ctx, req)
}

func (p *startupWorkflowProviderProxy) ListSchedules(ctx context.Context, req coreworkflow.ListSchedulesRequest) ([]*coreworkflow.Schedule, error) {
	provider, err := p.awaitForContextPlugin(ctx)
	if err != nil {
		return nil, err
	}
	return provider.ListSchedules(ctx, req)
}

func (p *startupWorkflowProviderProxy) DeleteSchedule(ctx context.Context, req coreworkflow.DeleteScheduleRequest) error {
	provider, err := p.awaitForContextPlugin(ctx)
	if err != nil {
		return err
	}
	return provider.DeleteSchedule(ctx, req)
}

func (p *startupWorkflowProviderProxy) PauseSchedule(ctx context.Context, req coreworkflow.PauseScheduleRequest) (*coreworkflow.Schedule, error) {
	provider, err := p.awaitForContextPlugin(ctx)
	if err != nil {
		return nil, err
	}
	return provider.PauseSchedule(ctx, req)
}

func (p *startupWorkflowProviderProxy) ResumeSchedule(ctx context.Context, req coreworkflow.ResumeScheduleRequest) (*coreworkflow.Schedule, error) {
	provider, err := p.awaitForContextPlugin(ctx)
	if err != nil {
		return nil, err
	}
	return provider.ResumeSchedule(ctx, req)
}

func (p *startupWorkflowProviderProxy) UpsertEventTrigger(ctx context.Context, req coreworkflow.UpsertEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	provider, err := p.awaitForPlugin(ctx, startupWorkflowTargetPluginName(req.Target))
	if err != nil {
		return nil, err
	}
	return provider.UpsertEventTrigger(ctx, req)
}

func (p *startupWorkflowProviderProxy) GetEventTrigger(ctx context.Context, req coreworkflow.GetEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	provider, err := p.awaitForContextPlugin(ctx)
	if err != nil {
		return nil, err
	}
	return provider.GetEventTrigger(ctx, req)
}

func (p *startupWorkflowProviderProxy) ListEventTriggers(ctx context.Context, req coreworkflow.ListEventTriggersRequest) ([]*coreworkflow.EventTrigger, error) {
	provider, err := p.awaitForContextPlugin(ctx)
	if err != nil {
		return nil, err
	}
	return provider.ListEventTriggers(ctx, req)
}

func (p *startupWorkflowProviderProxy) DeleteEventTrigger(ctx context.Context, req coreworkflow.DeleteEventTriggerRequest) error {
	provider, err := p.awaitForContextPlugin(ctx)
	if err != nil {
		return err
	}
	return provider.DeleteEventTrigger(ctx, req)
}

func (p *startupWorkflowProviderProxy) PauseEventTrigger(ctx context.Context, req coreworkflow.PauseEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	provider, err := p.awaitForContextPlugin(ctx)
	if err != nil {
		return nil, err
	}
	return provider.PauseEventTrigger(ctx, req)
}

func (p *startupWorkflowProviderProxy) ResumeEventTrigger(ctx context.Context, req coreworkflow.ResumeEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	provider, err := p.awaitForContextPlugin(ctx)
	if err != nil {
		return nil, err
	}
	return provider.ResumeEventTrigger(ctx, req)
}

func (p *startupWorkflowProviderProxy) PublishEvent(ctx context.Context, req coreworkflow.PublishEventRequest) error {
	provider, err := p.awaitForPlugin(ctx, req.PluginName)
	if err != nil {
		return err
	}
	return provider.PublishEvent(ctx, req)
}

func (p *startupWorkflowProviderProxy) PutExecutionReference(ctx context.Context, ref *coreworkflow.ExecutionReference) (*coreworkflow.ExecutionReference, error) {
	pluginName := ""
	if ref != nil {
		pluginName = startupWorkflowTargetPluginName(ref.Target)
	}
	select {
	case <-p.ready:
	default:
		stored := cloneStartupWorkflowExecutionRef(ref)
		if stored == nil || strings.TrimSpace(stored.ID) == "" {
			return nil, fmt.Errorf("workflow execution reference id is required")
		}
		p.mu.Lock()
		p.pendingExecutionRefs[stored.ID] = stored
		p.mu.Unlock()
		return cloneStartupWorkflowExecutionRef(stored), nil
	}
	provider, err := p.awaitForPlugin(ctx, pluginName)
	if err != nil {
		return nil, err
	}
	store, ok := provider.(coreworkflow.ExecutionReferenceStore)
	if !ok {
		return nil, fmt.Errorf("workflow provider %q does not support execution references", p.providerName)
	}
	return store.PutExecutionReference(ctx, ref)
}

func (p *startupWorkflowProviderProxy) GetExecutionReference(ctx context.Context, id string) (*coreworkflow.ExecutionReference, error) {
	id = strings.TrimSpace(id)
	p.mu.RLock()
	if ref := p.pendingExecutionRefs[id]; ref != nil {
		p.mu.RUnlock()
		return cloneStartupWorkflowExecutionRef(ref), nil
	}
	p.mu.RUnlock()

	provider, err := p.await(ctx)
	if err != nil {
		return nil, err
	}
	store, ok := provider.(coreworkflow.ExecutionReferenceStore)
	if !ok {
		return nil, fmt.Errorf("workflow provider %q does not support execution references", p.providerName)
	}
	return store.GetExecutionReference(ctx, id)
}

func (p *startupWorkflowProviderProxy) ListExecutionReferences(ctx context.Context, subjectID string) ([]*coreworkflow.ExecutionReference, error) {
	pending := p.pendingExecutionRefsForSubject(subjectID)
	select {
	case <-p.ready:
	default:
		return pending, nil
	}
	provider, err := p.await(ctx)
	if err != nil {
		return nil, err
	}
	store, ok := provider.(coreworkflow.ExecutionReferenceStore)
	if !ok {
		if len(pending) > 0 {
			return pending, nil
		}
		return nil, fmt.Errorf("workflow provider %q does not support execution references", p.providerName)
	}
	refs, err := store.ListExecutionReferences(ctx, subjectID)
	if err != nil {
		return nil, err
	}
	if len(pending) == 0 {
		return refs, nil
	}
	merged := make([]*coreworkflow.ExecutionReference, 0, len(pending)+len(refs))
	seen := make(map[string]bool, len(pending)+len(refs))
	for _, ref := range pending {
		if ref == nil || seen[ref.ID] {
			continue
		}
		seen[ref.ID] = true
		merged = append(merged, ref)
	}
	for _, ref := range refs {
		if ref == nil || seen[ref.ID] {
			continue
		}
		seen[ref.ID] = true
		merged = append(merged, ref)
	}
	return merged, nil
}

func (p *startupWorkflowProviderProxy) Ping(ctx context.Context) error {
	provider, err := p.await(ctx)
	if err != nil {
		return err
	}
	return provider.Ping(ctx)
}

func (p *startupWorkflowProviderProxy) Close() error {
	select {
	case <-p.ready:
	default:
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.provider == nil {
		return nil
	}
	return p.provider.Close()
}

func (p *startupWorkflowProviderProxy) awaitForPlugin(ctx context.Context, pluginName string) (coreworkflow.Provider, error) {
	done, err := p.beginPluginWait(pluginName)
	if err != nil {
		return nil, err
	}
	defer done()
	return p.await(ctx)
}

func (p *startupWorkflowProviderProxy) awaitForContextPlugin(ctx context.Context) (coreworkflow.Provider, error) {
	pluginName := strings.TrimSpace(invocation.WorkflowContextString(invocation.WorkflowContextFromContext(ctx), "plugin"))
	if pluginName == "" {
		return p.await(ctx)
	}
	return p.awaitForPlugin(ctx, pluginName)
}

func startupWorkflowTargetPluginName(target coreworkflow.Target) string {
	if target.Plugin == nil {
		return ""
	}
	return strings.TrimSpace(target.Plugin.PluginName)
}

func (p *startupWorkflowProviderProxy) beginPluginWait(pluginName string) (func(), error) {
	if p == nil || p.tracker == nil {
		return func() {}, nil
	}
	return p.tracker.beginPluginWait(pluginName, p.providerName)
}

func (p *startupWorkflowProviderProxy) pendingExecutionRefsForSubject(subjectID string) []*coreworkflow.ExecutionReference {
	subjectID = strings.TrimSpace(subjectID)
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]*coreworkflow.ExecutionReference, 0, len(p.pendingExecutionRefs))
	for _, ref := range p.pendingExecutionRefs {
		if ref == nil {
			continue
		}
		if subjectID != "" && strings.TrimSpace(ref.SubjectID) != subjectID {
			continue
		}
		out = append(out, cloneStartupWorkflowExecutionRef(ref))
	}
	return out
}

func cloneStartupWorkflowExecutionRef(ref *coreworkflow.ExecutionReference) *coreworkflow.ExecutionReference {
	if ref == nil {
		return nil
	}
	clone := *ref
	clone.Target = cloneStartupWorkflowTarget(ref.Target)
	clone.Permissions = append([]core.AccessPermission(nil), ref.Permissions...)
	for i := range clone.Permissions {
		clone.Permissions[i].Operations = append([]string(nil), clone.Permissions[i].Operations...)
		clone.Permissions[i].Actions = append([]string(nil), clone.Permissions[i].Actions...)
	}
	if ref.CreatedAt != nil {
		createdAt := ref.CreatedAt.UTC()
		clone.CreatedAt = &createdAt
	}
	if ref.RevokedAt != nil {
		revokedAt := ref.RevokedAt.UTC()
		clone.RevokedAt = &revokedAt
	}
	return &clone
}

func cloneStartupWorkflowTarget(target coreworkflow.Target) coreworkflow.Target {
	clone := coreworkflow.Target{}
	if target.Plugin != nil {
		plugin := *target.Plugin
		plugin.Input = maps.Clone(plugin.Input)
		clone.Plugin = &plugin
	}
	if target.Agent != nil {
		agent := *target.Agent
		agent.Messages = slices.Clone(agent.Messages)
		agent.ToolRefs = slices.Clone(agent.ToolRefs)
		agent.ResponseSchema = maps.Clone(agent.ResponseSchema)
		agent.Metadata = maps.Clone(agent.Metadata)
		agent.ProviderOptions = maps.Clone(agent.ProviderOptions)
		clone.Agent = &agent
	}
	return clone
}
