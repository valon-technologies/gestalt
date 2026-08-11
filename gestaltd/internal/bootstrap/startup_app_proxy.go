package bootstrap

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	appservice "github.com/valon-technologies/gestalt/server/services/apps"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

type startupProviderProxy struct {
	spec             appservice.StaticProviderSpec
	operationRouting startupOperationRouting
	tracker          *startupWaitTracker
	gate             startupGate[core.Provider]
	activate         func(context.Context)
}

func (p *startupProviderProxy) setActivationTrigger(activate func(context.Context)) {
	p.activate = activate
}

func newStartupProviderProxy(spec appservice.StaticProviderSpec, operationRouting startupOperationRouting, tracker *startupWaitTracker) *startupProviderProxy {
	operationRouting.connections = maps.Clone(operationRouting.connections)
	return &startupProviderProxy{
		spec:             spec,
		operationRouting: operationRouting,
		tracker:          tracker,
		gate:             newStartupGate[core.Provider](),
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
	p.gate.finish(provider, err)
}

func (p *startupProviderProxy) await(ctx context.Context) (core.Provider, error) {
	if p.activate != nil {
		if _, done, _ := p.gate.resolved(); !done {
			p.activate(ctx)
			if _, done, _ := p.gate.resolved(); !done {
				return nil, fmt.Errorf("%w: %q", core.ErrProviderActivating, p.spec.Name)
			}
		}
		// The gate has finished (install succeeded or failed); fall through so
		// callers get the resolved provider or the real install error, not a
		// perpetual ErrProviderActivating.
	}
	provider, err := p.gate.await(ctx)
	if err != nil {
		return nil, err
	}
	if provider == nil {
		return nil, fmt.Errorf("provider %q is not available", p.spec.Name)
	}
	return provider, nil
}

func (p *startupProviderProxy) resolved() core.Provider {
	provider, _, _ := p.gate.resolved()
	return provider
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

func (p *startupProviderProxy) StaticHeaders() map[string]string {
	if provider := p.resolved(); provider != nil {
		if headers, ok := provider.(interface{ StaticHeaders() map[string]string }); ok {
			return maps.Clone(headers.StaticHeaders())
		}
	}
	return maps.Clone(p.spec.StaticHeaders)
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
	if providerName, _ := workflow["providerName"].(string); strings.TrimSpace(providerName) != "" {
		return p.tracker.beginWait(newStartupProviderNode(invocation.ProviderKindWorkflow, providerName), target)
	}
	if done, ok, err := p.tracker.beginCallerProviderWait(ctx, target); ok || err != nil {
		return done, err
	}
	return func() {}, nil
}
