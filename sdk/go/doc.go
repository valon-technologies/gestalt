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
// AgentProvider, and PluginRuntimeProvider.
//
// Use the host-service clients when provider code needs to call sibling
// services exposed by gestaltd. These include CacheClient, IndexedDBClient,
// S3Client, WorkflowHostClient, WorkflowManagerClient, AgentHostClient,
// AgentManagerClient, AuthorizationClient, and InvokerClient.
//
// See https://gestaltd.ai/reference/go-sdk for the Go SDK guide.
// See https://gestaltd.ai/custom-providers/plugins for the full typed plugin
// authoring flow.
package gestalt
