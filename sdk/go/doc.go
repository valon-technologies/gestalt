// Package gestalt provides a Go SDK for building Gestalt executable providers.
//
// Gestalt plugins extend the platform with new integrations and automations.
// [PluginProvider] is the runtime contract for integration plugins: it
// receives startup config and serves typed executable operations when the host
// invokes them.
//
// Static metadata still belongs to the provider manifest. Static executable
// operations are authored in Go and materialized automatically by the SDK and
// host. The recommended authoring flow is:
//
//  1. Implement [PluginProvider.Configure].
//  2. Define typed operations and handlers in Go with [Operation], [Register],
//     and [Router].
//  3. Export `New()` plus `Router` from the provider package.
//
// This keeps the runtime contract single-source and manifest-backed while still
// giving provider authors typed Go definitions for executable helper
// operations.
//
// The package also includes first-pass runtime/auth/datastore protocol types
// for non-integration provider kinds. The current host-side execution flow
// remains integration-focused and is anchored on [ServeProvider].
//
// # Implementing a Provider
//
// The runtime provider interface stays small:
//
//	type MyProvider struct{}
//
//	func (p *MyProvider) Configure(ctx context.Context, name string, config map[string]any) error {
//		return nil
//	}
//
//	func New() *MyProvider { return &MyProvider{} }
//
//	var Router = gestalt.MustRouter(
//		gestalt.Register(myOperation, (*MyProvider).myHandler),
//	)
//
// Source-provider flows derive the executable catalog name from provider.yaml. Use
// [MustNamedRouter] only when you need an explicit catalog name outside that
// manifest-backed flow.
//
// See `/docs/content/providers/plugins.mdx` for the full typed authoring flow.
package gestalt
