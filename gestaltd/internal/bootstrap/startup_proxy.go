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
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	appservice "github.com/valon-technologies/gestalt/server/services/apps"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

type startupWaitTracker struct {
	mu    sync.Mutex
	waits map[startupProviderNode]map[startupProviderNode]int
}

func newStartupWaitTracker() *startupWaitTracker {
	return &startupWaitTracker{
		waits: make(map[startupProviderNode]map[startupProviderNode]int),
	}
}

type startupProviderNode struct {
	kind invocation.ProviderKind
	name string
}

func newStartupProviderNode(kind invocation.ProviderKind, name string) startupProviderNode {
	return startupProviderNode{kind: kind, name: strings.TrimSpace(name)}
}

func (n startupProviderNode) valid() bool {
	return n.kind != "" && strings.TrimSpace(n.name) != ""
}

func (n startupProviderNode) String() string {
	return fmt.Sprintf("%s %q", n.kind, n.name)
}

func startupProviderNodeFromContext(ctx context.Context) (startupProviderNode, bool) {
	caller := invocation.CallerProviderFromContext(ctx)
	if caller.Kind == "" || strings.TrimSpace(caller.Name) == "" {
		return startupProviderNode{}, false
	}
	return newStartupProviderNode(caller.Kind, caller.Name), true
}

func (t *startupWaitTracker) beginWait(waiting, target startupProviderNode) (func(), error) {
	waiting.name = strings.TrimSpace(waiting.name)
	target.name = strings.TrimSpace(target.name)
	if t == nil || !waiting.valid() || !target.valid() {
		return func() {}, nil
	}
	if waiting == target {
		return nil, fmt.Errorf("startup dependency cycle: %s -> %s", waiting, target)
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if path := t.waitPathLocked(target, waiting, nil); len(path) > 0 {
		return nil, fmt.Errorf("startup dependency cycle: %s", formatStartupWaitPath(append([]startupProviderNode{waiting}, path...)))
	}
	if t.waits[waiting] == nil {
		t.waits[waiting] = make(map[startupProviderNode]int)
	}
	t.waits[waiting][target]++
	return func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		if remaining := t.waits[waiting][target] - 1; remaining > 0 {
			t.waits[waiting][target] = remaining
		} else {
			delete(t.waits[waiting], target)
			if len(t.waits[waiting]) == 0 {
				delete(t.waits, waiting)
			}
		}
	}, nil
}

func (t *startupWaitTracker) beginCallerProviderWait(ctx context.Context, target startupProviderNode) (func(), bool, error) {
	if t == nil {
		return func() {}, false, nil
	}
	source, ok := startupProviderNodeFromContext(ctx)
	if !ok {
		return func() {}, false, nil
	}
	done, err := t.beginWait(source, target)
	if err != nil {
		return nil, true, err
	}
	return done, true, nil
}

func (t *startupWaitTracker) waitPathLocked(from, to startupProviderNode, seen map[startupProviderNode]bool) []startupProviderNode {
	if from == to {
		return []startupProviderNode{from}
	}
	if seen == nil {
		seen = make(map[startupProviderNode]bool)
	}
	if seen[from] {
		return nil
	}
	seen[from] = true
	for next, count := range t.waits[from] {
		if count <= 0 {
			continue
		}
		if path := t.waitPathLocked(next, to, seen); len(path) > 0 {
			return append([]startupProviderNode{from}, path...)
		}
	}
	return nil
}

func formatStartupWaitPath(path []startupProviderNode) string {
	if len(path) == 0 {
		return ""
	}
	parts := make([]string, 0, len(path))
	for _, node := range path {
		parts = append(parts, node.String())
	}
	return strings.Join(parts, " -> ")
}

type startupProviderProxy struct {
	spec             appservice.StaticProviderSpec
	operationRouting startupOperationRouting
	tracker          *startupWaitTracker

	ready chan struct{}
	once  sync.Once

	mu       sync.RWMutex
	provider core.Provider
	err      error
}

func newStartupProviderProxy(spec appservice.StaticProviderSpec, operationRouting startupOperationRouting, tracker *startupWaitTracker) *startupProviderProxy {
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
func (p *startupProviderProxy) SupportsPostConnect() bool {
	provider := p.resolved()
	if provider == nil {
		return len(p.spec.PostConnectConfigs) > 0
	}
	return core.SupportsPostConnect(provider)
}
func (p *startupProviderProxy) Catalog() *catalog.Catalog {
	if p.spec.Catalog == nil {
		return nil
	}
	return p.spec.Catalog.Clone()
}

func (p *startupProviderProxy) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	done, err := p.beginCallerWait(ctx)
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
	done, err := p.beginCallerWait(ctx)
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

func (p *startupProviderProxy) PostConnect(ctx context.Context, token *core.ExternalCredential) (map[string]string, error) {
	done, err := p.beginCallerWait(ctx)
	if err != nil {
		return nil, err
	}
	defer done()

	provider, err := p.await(ctx)
	if err != nil {
		return nil, err
	}
	metadata, supported, err := core.PostConnect(ctx, provider, token)
	if !supported {
		return nil, core.ErrPostConnectUnsupported
	}
	return metadata, err
}

func (p *startupProviderProxy) CallTool(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error) {
	done, err := p.beginCallerWait(ctx)
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
	done, err := p.beginCallerWait(ctx)
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

func (p *startupProviderProxy) beginCallerWait(ctx context.Context) (func(), error) {
	if p == nil || p.tracker == nil {
		return func() {}, nil
	}
	target := newStartupProviderNode(invocation.ProviderKindApp, p.spec.Name)
	workflow := invocation.WorkflowContextFromContext(ctx)
	if providerName, _ := workflow["provider"].(string); strings.TrimSpace(providerName) != "" {
		return p.tracker.beginWait(newStartupProviderNode(invocation.ProviderKindWorkflow, providerName), target)
	}
	if done, ok, err := p.tracker.beginCallerProviderWait(ctx, target); ok || err != nil {
		return done, err
	}
	return func() {}, nil
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

func (p *startupWorkflowProviderProxy) CreateDefinition(ctx context.Context, req coreworkflow.CreateDefinitionRequest) (*coreworkflow.Definition, error) {
	provider, err := p.awaitForApp(ctx, startupWorkflowTargetAppName(req.Target))
	if err != nil {
		return nil, err
	}
	return provider.CreateDefinition(ctx, req)
}

func (p *startupWorkflowProviderProxy) GetDefinition(ctx context.Context, req coreworkflow.GetDefinitionRequest) (*coreworkflow.Definition, error) {
	provider, err := p.awaitForContextApp(ctx)
	if err != nil {
		return nil, err
	}
	return provider.GetDefinition(ctx, req)
}

func (p *startupWorkflowProviderProxy) UpdateDefinition(ctx context.Context, req coreworkflow.UpdateDefinitionRequest) (*coreworkflow.Definition, error) {
	provider, err := p.awaitForApp(ctx, startupWorkflowTargetAppName(req.Target))
	if err != nil {
		return nil, err
	}
	return provider.UpdateDefinition(ctx, req)
}

func (p *startupWorkflowProviderProxy) DeleteDefinition(ctx context.Context, req coreworkflow.DeleteDefinitionRequest) error {
	provider, err := p.awaitForContextApp(ctx)
	if err != nil {
		return err
	}
	return provider.DeleteDefinition(ctx, req)
}

func (p *startupWorkflowProviderProxy) StartRun(ctx context.Context, req coreworkflow.StartRunRequest) (*coreworkflow.Run, error) {
	provider, err := p.awaitForApp(ctx, startupWorkflowTargetAppName(req.Target))
	if err != nil {
		return nil, err
	}
	return provider.StartRun(ctx, req)
}

func (p *startupWorkflowProviderProxy) GetRun(ctx context.Context, req coreworkflow.GetRunRequest) (*coreworkflow.Run, error) {
	provider, err := p.awaitForContextApp(ctx)
	if err != nil {
		return nil, err
	}
	return provider.GetRun(ctx, req)
}

func (p *startupWorkflowProviderProxy) ListRuns(ctx context.Context, req coreworkflow.ListRunsRequest) (*coreworkflow.ListRunsResponse, error) {
	provider, err := p.awaitForContextApp(ctx)
	if err != nil {
		return nil, err
	}
	return provider.ListRuns(ctx, req)
}

func (p *startupWorkflowProviderProxy) CancelRun(ctx context.Context, req coreworkflow.CancelRunRequest) (*coreworkflow.Run, error) {
	provider, err := p.awaitForContextApp(ctx)
	if err != nil {
		return nil, err
	}
	return provider.CancelRun(ctx, req)
}

func (p *startupWorkflowProviderProxy) SignalRun(ctx context.Context, req coreworkflow.SignalRunRequest) (*coreworkflow.SignalRunResponse, error) {
	provider, err := p.awaitForContextApp(ctx)
	if err != nil {
		return nil, err
	}
	return provider.SignalRun(ctx, req)
}

func (p *startupWorkflowProviderProxy) SignalOrStartRun(ctx context.Context, req coreworkflow.SignalOrStartRunRequest) (*coreworkflow.SignalRunResponse, error) {
	provider, err := p.awaitForApp(ctx, startupWorkflowTargetAppName(req.Target))
	if err != nil {
		return nil, err
	}
	return provider.SignalOrStartRun(ctx, req)
}

func (p *startupWorkflowProviderProxy) UpsertSchedule(ctx context.Context, req coreworkflow.UpsertScheduleRequest) (*coreworkflow.Schedule, error) {
	provider, err := p.awaitForApp(ctx, startupWorkflowTargetAppName(req.Target))
	if err != nil {
		return nil, err
	}
	return provider.UpsertSchedule(ctx, req)
}

func (p *startupWorkflowProviderProxy) GetSchedule(ctx context.Context, req coreworkflow.GetScheduleRequest) (*coreworkflow.Schedule, error) {
	provider, err := p.awaitForContextApp(ctx)
	if err != nil {
		return nil, err
	}
	return provider.GetSchedule(ctx, req)
}

func (p *startupWorkflowProviderProxy) ListSchedules(ctx context.Context, req coreworkflow.ListSchedulesRequest) ([]*coreworkflow.Schedule, error) {
	provider, err := p.awaitForContextApp(ctx)
	if err != nil {
		return nil, err
	}
	return provider.ListSchedules(ctx, req)
}

func (p *startupWorkflowProviderProxy) DeleteSchedule(ctx context.Context, req coreworkflow.DeleteScheduleRequest) error {
	provider, err := p.awaitForContextApp(ctx)
	if err != nil {
		return err
	}
	return provider.DeleteSchedule(ctx, req)
}

func (p *startupWorkflowProviderProxy) PauseSchedule(ctx context.Context, req coreworkflow.PauseScheduleRequest) (*coreworkflow.Schedule, error) {
	provider, err := p.awaitForContextApp(ctx)
	if err != nil {
		return nil, err
	}
	return provider.PauseSchedule(ctx, req)
}

func (p *startupWorkflowProviderProxy) ResumeSchedule(ctx context.Context, req coreworkflow.ResumeScheduleRequest) (*coreworkflow.Schedule, error) {
	provider, err := p.awaitForContextApp(ctx)
	if err != nil {
		return nil, err
	}
	return provider.ResumeSchedule(ctx, req)
}

func (p *startupWorkflowProviderProxy) UpsertEventTrigger(ctx context.Context, req coreworkflow.UpsertEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	provider, err := p.awaitForApp(ctx, startupWorkflowTargetAppName(req.Target))
	if err != nil {
		return nil, err
	}
	return provider.UpsertEventTrigger(ctx, req)
}

func (p *startupWorkflowProviderProxy) GetEventTrigger(ctx context.Context, req coreworkflow.GetEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	provider, err := p.awaitForContextApp(ctx)
	if err != nil {
		return nil, err
	}
	return provider.GetEventTrigger(ctx, req)
}

func (p *startupWorkflowProviderProxy) ListEventTriggers(ctx context.Context, req coreworkflow.ListEventTriggersRequest) ([]*coreworkflow.EventTrigger, error) {
	provider, err := p.awaitForContextApp(ctx)
	if err != nil {
		return nil, err
	}
	return provider.ListEventTriggers(ctx, req)
}

func (p *startupWorkflowProviderProxy) DeleteEventTrigger(ctx context.Context, req coreworkflow.DeleteEventTriggerRequest) error {
	provider, err := p.awaitForContextApp(ctx)
	if err != nil {
		return err
	}
	return provider.DeleteEventTrigger(ctx, req)
}

func (p *startupWorkflowProviderProxy) PauseEventTrigger(ctx context.Context, req coreworkflow.PauseEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	provider, err := p.awaitForContextApp(ctx)
	if err != nil {
		return nil, err
	}
	return provider.PauseEventTrigger(ctx, req)
}

func (p *startupWorkflowProviderProxy) ResumeEventTrigger(ctx context.Context, req coreworkflow.ResumeEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	provider, err := p.awaitForContextApp(ctx)
	if err != nil {
		return nil, err
	}
	return provider.ResumeEventTrigger(ctx, req)
}

func (p *startupWorkflowProviderProxy) PublishEvent(ctx context.Context, req coreworkflow.PublishEventRequest) (*coreworkflow.Event, error) {
	provider, err := p.awaitForApp(ctx, req.AppName)
	if err != nil {
		return nil, err
	}
	return provider.PublishEvent(ctx, req)
}

func (p *startupWorkflowProviderProxy) Ping(ctx context.Context) error {
	provider, err := p.awaitForApp(ctx, "")
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

func (p *startupWorkflowProviderProxy) awaitForApp(ctx context.Context, appName string) (coreworkflow.Provider, error) {
	done, err := p.beginCallerWait(ctx, appName)
	if err != nil {
		return nil, err
	}
	defer done()
	return p.await(ctx)
}

func (p *startupWorkflowProviderProxy) awaitForContextApp(ctx context.Context) (coreworkflow.Provider, error) {
	appName := strings.TrimSpace(invocation.WorkflowContextString(invocation.WorkflowContextFromContext(ctx), "app"))
	return p.awaitForApp(ctx, appName)
}

func startupWorkflowTargetAppName(target coreworkflow.Target) string {
	for i := range target.Steps {
		if target.Steps[i].App != nil {
			return strings.TrimSpace(target.Steps[i].App.Name)
		}
	}
	return ""
}

func (p *startupWorkflowProviderProxy) beginCallerWait(ctx context.Context, appName string) (func(), error) {
	if p == nil || p.tracker == nil {
		return func() {}, nil
	}
	target := newStartupProviderNode(invocation.ProviderKindWorkflow, p.providerName)
	if done, ok, err := p.tracker.beginCallerProviderWait(ctx, target); ok || err != nil {
		return done, err
	}
	return p.tracker.beginWait(
		newStartupProviderNode(invocation.ProviderKindApp, appName),
		target,
	)
}

type startupAgentProviderProxy struct {
	providerName string
	tracker      *startupWaitTracker

	ready chan struct{}
	once  sync.Once

	mu       sync.RWMutex
	provider coreagent.Provider
	err      error
}

func newStartupAgentProviderProxy(providerName string, tracker *startupWaitTracker) *startupAgentProviderProxy {
	return &startupAgentProviderProxy{
		providerName: providerName,
		tracker:      tracker,
		ready:        make(chan struct{}),
	}
}

func (p *startupAgentProviderProxy) publish(provider coreagent.Provider) {
	p.finish(provider, nil)
}

func (p *startupAgentProviderProxy) fail(err error) {
	p.finish(nil, err)
}

func (p *startupAgentProviderProxy) finish(provider coreagent.Provider, err error) {
	if err == nil && provider == nil {
		err = fmt.Errorf("agent provider %q is not available", p.providerName)
	}
	p.once.Do(func() {
		p.mu.Lock()
		p.provider = provider
		p.err = err
		p.mu.Unlock()
		close(p.ready)
	})
}

func (p *startupAgentProviderProxy) await(ctx context.Context) (coreagent.Provider, error) {
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
		return nil, fmt.Errorf("agent provider %q is not available", p.providerName)
	}
	return p.provider, nil
}

func (p *startupAgentProviderProxy) awaitForCaller(ctx context.Context) (coreagent.Provider, error) {
	done, err := p.beginCallerWait(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	return p.await(ctx)
}

func (p *startupAgentProviderProxy) beginCallerWait(ctx context.Context) (func(), error) {
	if p == nil || p.tracker == nil {
		return func() {}, nil
	}
	done, _, err := p.tracker.beginCallerProviderWait(ctx, newStartupProviderNode(invocation.ProviderKindAgent, p.providerName))
	return done, err
}

func (p *startupAgentProviderProxy) SupportsWorkspaceRequests() bool {
	select {
	case <-p.ready:
	default:
		return true
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	workspaceProvider, ok := p.provider.(coreagent.WorkspaceProvider)
	return ok && workspaceProvider.SupportsWorkspaceRequests()
}

func (p *startupAgentProviderProxy) CreateSession(ctx context.Context, req coreagent.CreateSessionRequest) (*coreagent.Session, error) {
	provider, err := p.awaitForCaller(ctx)
	if err != nil {
		return nil, err
	}
	if req.Workspace != nil {
		workspaceProvider, ok := provider.(coreagent.WorkspaceProvider)
		if !ok || !workspaceProvider.SupportsWorkspaceRequests() {
			return nil, fmt.Errorf("%w: provider %q", agentmanager.ErrAgentWorkspaceUnsupported, p.providerName)
		}
	}
	return provider.CreateSession(ctx, req)
}

func (p *startupAgentProviderProxy) GetSession(ctx context.Context, req coreagent.GetSessionRequest) (*coreagent.Session, error) {
	provider, err := p.awaitForCaller(ctx)
	if err != nil {
		return nil, err
	}
	return provider.GetSession(ctx, req)
}

func (p *startupAgentProviderProxy) ListSessions(ctx context.Context, req coreagent.ListSessionsRequest) ([]*coreagent.Session, error) {
	provider, err := p.awaitForCaller(ctx)
	if err != nil {
		return nil, err
	}
	return provider.ListSessions(ctx, req)
}

func (p *startupAgentProviderProxy) UpdateSession(ctx context.Context, req coreagent.UpdateSessionRequest) (*coreagent.Session, error) {
	provider, err := p.awaitForCaller(ctx)
	if err != nil {
		return nil, err
	}
	return provider.UpdateSession(ctx, req)
}

func (p *startupAgentProviderProxy) CreateTurn(ctx context.Context, req coreagent.CreateTurnRequest) (*coreagent.Turn, error) {
	provider, err := p.awaitForCaller(ctx)
	if err != nil {
		return nil, err
	}
	return provider.CreateTurn(ctx, req)
}

func (p *startupAgentProviderProxy) GetTurn(ctx context.Context, req coreagent.GetTurnRequest) (*coreagent.Turn, error) {
	provider, err := p.awaitForCaller(ctx)
	if err != nil {
		return nil, err
	}
	return provider.GetTurn(ctx, req)
}

func (p *startupAgentProviderProxy) ListTurns(ctx context.Context, req coreagent.ListTurnsRequest) ([]*coreagent.Turn, error) {
	provider, err := p.awaitForCaller(ctx)
	if err != nil {
		return nil, err
	}
	return provider.ListTurns(ctx, req)
}

func (p *startupAgentProviderProxy) CancelTurn(ctx context.Context, req coreagent.CancelTurnRequest) (*coreagent.Turn, error) {
	provider, err := p.awaitForCaller(ctx)
	if err != nil {
		return nil, err
	}
	return provider.CancelTurn(ctx, req)
}

func (p *startupAgentProviderProxy) ListTurnEvents(ctx context.Context, req coreagent.ListTurnEventsRequest) ([]*coreagent.TurnEvent, error) {
	provider, err := p.awaitForCaller(ctx)
	if err != nil {
		return nil, err
	}
	return provider.ListTurnEvents(ctx, req)
}

func (p *startupAgentProviderProxy) GetInteraction(ctx context.Context, req coreagent.GetInteractionRequest) (*coreagent.Interaction, error) {
	provider, err := p.awaitForCaller(ctx)
	if err != nil {
		return nil, err
	}
	return provider.GetInteraction(ctx, req)
}

func (p *startupAgentProviderProxy) ListInteractions(ctx context.Context, req coreagent.ListInteractionsRequest) ([]*coreagent.Interaction, error) {
	provider, err := p.awaitForCaller(ctx)
	if err != nil {
		return nil, err
	}
	return provider.ListInteractions(ctx, req)
}

func (p *startupAgentProviderProxy) ResolveInteraction(ctx context.Context, req coreagent.ResolveInteractionRequest) (*coreagent.Interaction, error) {
	provider, err := p.awaitForCaller(ctx)
	if err != nil {
		return nil, err
	}
	return provider.ResolveInteraction(ctx, req)
}

func (p *startupAgentProviderProxy) GetCapabilities(ctx context.Context, req coreagent.GetCapabilitiesRequest) (*coreagent.ProviderCapabilities, error) {
	provider, err := p.awaitForCaller(ctx)
	if err != nil {
		return nil, err
	}
	return provider.GetCapabilities(ctx, req)
}

func (p *startupAgentProviderProxy) Ping(ctx context.Context) error {
	select {
	case <-p.ready:
	default:
		return agentmanager.NewAgentProviderNotAvailableError(p.providerName)
	}
	p.mu.RLock()
	provider := p.provider
	err := p.err
	p.mu.RUnlock()
	if err != nil {
		return err
	}
	if provider == nil {
		return agentmanager.NewAgentProviderNotAvailableError(p.providerName)
	}
	return provider.Ping(ctx)
}

func (p *startupAgentProviderProxy) Close() error {
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
