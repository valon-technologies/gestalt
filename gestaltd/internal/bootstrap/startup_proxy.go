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
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
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
	spec                 providerhost.StaticProviderSpec
	operationConnections map[string]string
	tracker              *startupWaitTracker

	ready chan struct{}
	once  sync.Once

	mu       sync.RWMutex
	provider core.Provider
	err      error
}

func newStartupProviderProxy(spec providerhost.StaticProviderSpec, operationConnections map[string]string, tracker *startupWaitTracker) *startupProviderProxy {
	return &startupProviderProxy{
		spec:                 spec,
		operationConnections: maps.Clone(operationConnections),
		tracker:              tracker,
		ready:                make(chan struct{}),
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
	return p.operationConnections[operation]
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
}

func newStartupWorkflowProviderProxy(providerName string, tracker *startupWaitTracker) *startupWorkflowProviderProxy {
	return &startupWorkflowProviderProxy{
		providerName: providerName,
		tracker:      tracker,
		ready:        make(chan struct{}),
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
	provider, err := p.awaitForPlugin(ctx, req.Target.PluginName)
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

func (p *startupWorkflowProviderProxy) UpsertSchedule(ctx context.Context, req coreworkflow.UpsertScheduleRequest) (*coreworkflow.Schedule, error) {
	provider, err := p.awaitForPlugin(ctx, req.Target.PluginName)
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
	provider, err := p.awaitForPlugin(ctx, req.Target.PluginName)
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

func (p *startupWorkflowProviderProxy) beginPluginWait(pluginName string) (func(), error) {
	if p == nil || p.tracker == nil {
		return func() {}, nil
	}
	return p.tracker.beginPluginWait(pluginName, p.providerName)
}
