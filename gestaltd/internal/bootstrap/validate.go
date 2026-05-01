package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/pluginvalidation"
	"github.com/valon-technologies/gestalt/server/services/authorization"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"github.com/valon-technologies/gestalt/server/services/plugins/registry"
)

// Validate loads daemon dependencies and integration factories without
// starting the server or running migrations. Unlike Bootstrap, provider
// validation is strict: any provider construction failure is returned.
func Validate(ctx context.Context, cfg *config.Config, factories *FactoryRegistry) ([]string, error) {
	if err := pluginvalidation.ValidateEffectiveCatalogsAndDependencies(ctx, cfg); err != nil {
		return nil, err
	}

	prepared, err := prepareCore(ctx, cfg, factories, false)
	if err != nil {
		return nil, err
	}
	defer func() { _ = prepared.Close(context.Background()) }()

	var warnings []string
	if w, ok := metricutil.UnwrapIndexedDB(prepared.Services.DB).(interface{ Warnings() []string }); ok {
		warnings = w.Warnings()
	}

	providers, providersReady, connAuthResolver, errResolver, err := buildProvidersAsync(
		ctx,
		cfg,
		factories,
		prepared.Deps,
		buildProviderForValidation,
	)
	if err != nil {
		return warnings, err
	}
	defer func() {
		<-providersReady
		_ = CloseProviders(providers)
	}()
	connMaps, err := BuildConnectionMaps(cfg)
	if err != nil {
		prepared.Deps.WorkflowRuntime.FailPendingProviders(err)
		return warnings, err
	}
	connRuntime, err := BuildConnectionRuntime(cfg)
	if err != nil {
		prepared.Deps.WorkflowRuntime.FailPendingProviders(err)
		return warnings, err
	}
	if _, _, err := cfg.SelectedAuthorizationProvider(); err != nil {
		prepared.Deps.WorkflowRuntime.FailPendingProviders(err)
		return warnings, err
	}
	authz, err := authorization.New(config.AuthorizationStaticConfig(cfg.Authorization, cfg.Plugins))
	if err != nil {
		prepared.Deps.WorkflowRuntime.FailPendingProviders(err)
		return warnings, err
	}
	defer func() { _ = authz.Close() }()
	sharedInvoker := invocation.NewBroker(providers, prepared.Services.Users, coredata.EffectiveExternalCredentialProvider(prepared.Services),
		invocation.WithAuthorizer(authz),
		invocation.WithConnectionMapper(invocation.ConnectionMap(connMaps.APIConnection)),
		invocation.WithMCPConnectionMapper(invocation.ConnectionMap(connMaps.MCPConnection)),
		invocation.WithConnectionAuth(func() map[string]map[string]invocation.OAuthRefresher {
			return connectionAuthToRefreshers(connAuthResolver())
		}),
		invocation.WithConnectionRuntime(connRuntime.Resolve),
	)
	prepared.Deps.WorkflowRuntime.SetInvoker(sharedInvoker)
	prepared.Deps.AgentRuntime.SetInvoker(sharedInvoker)
	extraWorkflows, err := buildWorkflows(ctx, cfg, factories, prepared.Deps)
	if err != nil {
		return warnings, err
	}
	defer func() { _ = closeWorkflows(extraWorkflows...) }()
	extraAgents, err := buildAgents(ctx, cfg, factories, prepared.Deps)
	if err != nil {
		return warnings, err
	}
	defer func() { _ = closeAgents(extraAgents...) }()
	<-providersReady
	if errs := errResolver(); len(errs) > 0 {
		return warnings, fmt.Errorf("bootstrap: provider validation failed: %w", errors.Join(errs...))
	}
	if err := validateMCPCatalogs(providers); err != nil {
		return warnings, err
	}

	return warnings, nil
}

func validateMCPCatalogs(providers *registry.ProviderMap[core.Provider]) error {
	for _, name := range providers.List() {
		prov, err := providers.Get(name)
		if err != nil {
			continue
		}
		cat := prov.Catalog()
		if cat == nil {
			continue
		}
		if err := cat.ValidateMCPCompat(); err != nil {
			return fmt.Errorf("integration %q: %w", name, err)
		}
	}
	return nil
}

func buildProvidersStrict(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, deps Deps) (*registry.ProviderMap[core.Provider], map[string]map[string]OAuthHandler, error) {
	reg := registry.New()
	connAuth := make(map[string]map[string]OAuthHandler)

	for _, builtin := range factories.Builtins {
		if err := reg.Providers.Register(builtin.Name(), builtin); errors.Is(err, core.ErrAlreadyRegistered) {
			continue
		} else if err != nil {
			_ = CloseProviders(&reg.Providers)
			return nil, nil, fmt.Errorf("bootstrap: registering builtin %q: %w", builtin.Name(), err)
		}
	}

	names := slices.Sorted(maps.Keys(cfg.Plugins))

	var errs []error
	for _, name := range names {
		entry := cfg.Plugins[name]
		result, err := buildProviderForValidation(ctx, name, entry, deps)
		if err != nil {
			errs = append(errs, fmt.Errorf("integration %q: %w", name, err))
			continue
		}
		if err := reg.Providers.Register(name, result.Provider); err != nil {
			closeIfPossible(result.Provider)
			errs = append(errs, fmt.Errorf("bootstrap: registering provider %q: %w", name, err))
		}
		if len(result.ConnectionAuth) > 0 {
			connAuth[name] = result.ConnectionAuth
		}
	}

	if len(errs) > 0 {
		_ = CloseProviders(&reg.Providers)
		return nil, nil, fmt.Errorf("bootstrap: provider validation failed: %w", errors.Join(errs...))
	}

	return &reg.Providers, connAuth, nil
}

func buildProviderForValidation(ctx context.Context, name string, entry *config.ProviderEntry, deps Deps) (*ProviderBuildResult, error) {
	if entry == nil || !entry.HasReleaseMetadataSource() || !entry.HasResolvedManifest() {
		return buildProvider(ctx, name, entry, deps)
	}
	prov, err := newPreparedProviderStub(name, entry)
	if err != nil {
		return nil, err
	}
	return &ProviderBuildResult{Provider: prov}, nil
}

type preparedProviderStub struct {
	name           string
	displayName    string
	description    string
	connectionMode core.ConnectionMode
	catalog        *catalog.Catalog
	connections    map[string]string
}

func newPreparedProviderStub(name string, entry *config.ProviderEntry) (core.Provider, error) {
	if entry == nil || entry.ResolvedManifest == nil {
		return nil, fmt.Errorf("prepared manifest is not resolved")
	}
	spec, operationRouting, err := buildStartupProviderSpec(name, entry)
	if err != nil {
		return nil, err
	}
	displayName := spec.DisplayName
	if displayName == "" {
		displayName = name
	}
	description := spec.Description
	if description == "" {
		description = fmt.Sprintf("prepared plugin stub for %s", name)
	}
	cat := spec.Catalog
	if cat == nil {
		cat = &catalog.Catalog{
			Name:        name,
			DisplayName: displayName,
			Description: description,
		}
	}
	return &preparedProviderStub{
		name:           name,
		displayName:    displayName,
		description:    description,
		connectionMode: spec.ConnectionMode,
		catalog:        cat,
		connections:    operationRouting.connections,
	}, nil
}

func (p *preparedProviderStub) Name() string                        { return p.name }
func (p *preparedProviderStub) DisplayName() string                 { return p.displayName }
func (p *preparedProviderStub) Description() string                 { return p.description }
func (p *preparedProviderStub) ConnectionMode() core.ConnectionMode { return p.connectionMode }
func (p *preparedProviderStub) AuthTypes() []string                 { return nil }
func (p *preparedProviderStub) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return nil
}
func (p *preparedProviderStub) CredentialFields() []core.CredentialFieldDef { return nil }
func (p *preparedProviderStub) DiscoveryConfig() *core.DiscoveryConfig      { return nil }
func (p *preparedProviderStub) Catalog() *catalog.Catalog {
	if p.catalog == nil {
		return &catalog.Catalog{
			Name:        p.name,
			DisplayName: p.displayName,
			Description: p.description,
		}
	}
	return p.catalog.Clone()
}
func (p *preparedProviderStub) Execute(context.Context, string, map[string]any, string) (*core.OperationResult, error) {
	return &core.OperationResult{Status: 202, Body: `{}`}, nil
}

func (p *preparedProviderStub) CallTool(context.Context, string, map[string]any) (*mcpgo.CallToolResult, error) {
	return mcpgo.NewToolResultText(`{}`), nil
}

func (p *preparedProviderStub) ConnectionForOperation(operation string) string {
	if p == nil || p.connections == nil {
		return ""
	}
	return p.connections[operation]
}
