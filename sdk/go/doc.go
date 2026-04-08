// Package gestalt provides a Go SDK for building Gestalt executable plugins.
//
// Gestalt plugins extend the platform with new integrations and automations.
// A [Provider] is the current runtime contract for integration plugins: it
// receives startup config and serves typed executable operations when the host
// invokes them.
//
// Static metadata still belongs to the plugin manifest. Static executable
// operations are authored in Go and materialized automatically by the SDK and
// host. The recommended authoring flow is:
//
//  1. Implement [Provider.Configure].
//  2. Define typed operations and handlers in Go with [Operation], [Register],
//     and [Router].
//  3. Export `New()` plus `Router` from the provider package.
//
// This keeps the runtime contract single-source and manifest-backed while still
// giving plugin authors typed Go definitions for executable helper operations.
//
// The package also includes first-pass runtime/auth/datastore protocol types
// for future non-integration plugin kinds. The current host-side execution flow
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
// Source-plugin flows derive the executable catalog name from plugin.yaml. Use
// [MustNamedRouter] only when you need an explicit catalog name outside that
// manifest-backed flow.
//
// See `/docs/plugins/writing-a-plugin` for the full typed authoring flow.
package gestalt
