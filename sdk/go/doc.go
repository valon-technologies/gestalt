// Package gestalt provides a Go SDK for building Gestalt provider plugins.
//
// Gestalt plugins extend the platform with new integrations and automations.
// A [Provider] exposes a set of named operations (e.g. "list_issues",
// "send_message") that callers can invoke. Providers are short-lived: they
// start, handle requests, and stop when the host is done.
//
// # Implementing a Provider
//
// Implement the [Provider] interface and call [ServeProvider]:
//
//	type MyProvider struct{}
//
//	func (p *MyProvider) Name() string            { return "my_provider" }
//	func (p *MyProvider) DisplayName() string     { return "My Provider" }
//	func (p *MyProvider) Description() string     { return "Does useful things." }
//	func (p *MyProvider) ConnectionMode() gestalt.ConnectionMode {
//		return gestalt.ConnectionModeNone
//	}
//
//	func (p *MyProvider) Catalog() *gestalt.Catalog {
//		return &gestalt.Catalog{
//			Name: "my_provider",
//			Operations: []gestalt.CatalogOperation{{
//				ID:          "hello",
//				Description: "Says hello",
//				Method:      "GET",
//			}},
//		}
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
package gestalt
