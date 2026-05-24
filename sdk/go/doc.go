// Package gestalt provides a Go SDK for building executable Gestalt providers.
//
// Use this package when you want a provider implemented as normal Go code with
// typed operation inputs, typed operation outputs, and a small runtime surface.
// The provider manifest still owns static identity, connections, hosted HTTP
// routes, passthrough surfaces, and release metadata. Go code owns executable
// handlers and provider runtime hooks.
//
// # Quick start
//
// Implement Provider.Configure, define typed operations, and export New plus
// Router from the provider package:
//
//	type SearchProvider struct{}
//
//	func New() *SearchProvider { return &SearchProvider{} }
//
//	func (p *SearchProvider) Configure(ctx context.Context, name string, config map[string]any) error {
//		return nil
//	}
//
//	type SearchInput struct {
//		Query string `json:"query" doc:"Search query" required:"true"`
//	}
//
//	type SearchOutput struct {
//		Results []string `json:"results"`
//	}
//
//	var Router = gestalt.MustRouter(
//		gestalt.Register(
//			gestalt.Operation[SearchInput, SearchOutput]{
//				ID:     "search",
//				Method: http.MethodGet,
//				Title:  "Search",
//			},
//			func(_ *SearchProvider, _ context.Context, input SearchInput, _ gestalt.Request) (gestalt.Response[SearchOutput], error) {
//				return gestalt.OK(SearchOutput{Results: []string{input.Query}}), nil
//			},
//		),
//	)
//
// Source-provider flows derive the executable catalog name from manifest.yaml.
// Use Router.WithName only when you need an explicit catalog name outside that
// manifest-backed flow.
//
// # API sections
//
// Provider authoring starts with [Provider], [Operation], [Register], [Router],
// [Request], [Response], and [OK]. These types model executable plugin
// providers, typed operation handlers, and operation results.
//
// Catalog metadata is built from [Catalog], [CatalogOperation], struct tags,
// and typed [Operation] definitions.
//
// Provider runtimes are modeled by [AuthenticationProvider],
// [AuthorizationProvider], [CacheProvider], [IndexedDBProvider], [S3Provider],
// [SecretsProvider], [WorkflowProvider], [AgentProvider], and
// [AppRuntimeProvider].
//
// Workflow and agent helpers include [NewAgentMessage] and [NewAgentToolRef].
// Workflow structs use native Go values at provider boundaries; SDK adapters
// own transport conversion.
//
// Host-service clients include [CacheClient], [WorkflowHostClient],
// [WorkflowManagerClient], [AgentHostClient], [AgentManagerClient],
// [AuthorizationClient], and [InvokerClient]. Apps reach IndexedDB and S3 through
// [IndexedDB] and [S3], which return the capability interfaces rather than
// transport-specific client types.
//
// Runtime and telemetry helpers include [ServeProvider], [ProviderMetadata],
// [TelemetryInstrumentationName], and the provider telemetry helpers.
//
// # Catalog metadata
//
// The router derives catalog parameters from Go struct tags. The json tag sets
// the parameter name. json:",omitempty" makes the parameter optional.
// doc:"..." sets the description, required:"true|false" overrides requiredness,
// and default:"..." sets a scalar default.
//
// # Provider surfaces
//
// Provider, Operation, Register, and Router model integration providers. The
// package also exposes provider interfaces for host-service backends, including
// AuthenticationProvider, AuthorizationProvider, CacheProvider,
// IndexedDBProvider, S3Provider, SecretsProvider, WorkflowProvider,
// AgentProvider, and AppRuntimeProvider.
//
// Use the host-service clients when provider code needs to call sibling
// services exposed by gestaltd. These include CacheClient, WorkflowHostClient,
// WorkflowManagerClient, AgentHostClient, AgentManagerClient, AuthorizationClient,
// and InvokerClient. Apps use [IndexedDB] and [S3] for datastore and object
// storage bindings.
//
// AgentHostClient includes plain Go helper methods such as ExecuteToolForTurn,
// ListToolsForTurn, and ResolveConnectionForTurn. Native SDK structs accept
// time.Time values and JSON-compatible maps or structs, so provider code can
// keep transport serialization at the SDK boundary.
//
// See https://gestaltd.ai/reference/sdk for the SDK overview.
// See https://gestaltd.ai/providers/apps for the typed app authoring flow.
// See https://gestaltd.ai/providers for the provider model.
package gestalt
