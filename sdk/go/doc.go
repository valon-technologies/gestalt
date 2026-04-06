// Package gestalt provides a Go SDK for building Gestalt provider plugins.
//
// Gestalt plugins extend the platform with new integrations and automations.
// A [Provider] exposes a set of named operations (e.g. "list_issues",
// "send_message") that callers can invoke. Providers are short-lived: they
// start, handle requests, and stop when the host is done.
//
// # Implementing a Provider
//
// Implement the [Provider] interface and call [ServeProvider]. Static metadata
// and the static catalog belong in the plugin manifest, not in the Go type:
//
//	type MyProvider struct{}
//
//	func (p *MyProvider) Configure(ctx context.Context, name string, config map[string]any) error {
//		return nil
//	}
//
//	func (p *MyProvider) Execute(ctx context.Context, op string, params map[string]any, token string) (*gestalt.OperationResult, error) {
//		return &gestalt.OperationResult{Status: 200, Body: `{"message":"hello"}`}, nil
//	}
//
//	func main() {
//		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
//		defer cancel()
//		if err := gestalt.ServeProvider(ctx, &MyProvider{}); err != nil {
//			log.Fatal(err)
//		}
//	}
//
// The corresponding manifest would define fields like display_name,
// description, auth, and provider.static_catalog_path.
package gestalt
