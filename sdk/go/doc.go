// Package gestalt provides a Go SDK for building Gestalt plugins.
//
// Gestalt plugins extend the platform with new integrations and automations.
// There are two plugin types:
//
//   - A [Provider] exposes a set of named operations (e.g. "list_issues",
//     "send_message") that callers can invoke. Providers are short-lived:
//     they start, handle requests, and stop when the host is done.
//
//   - A [Runtime] is a long-lived sidecar that receives a set of available
//     [Capability] values on startup and uses a [RuntimeHost] to invoke
//     operations on other providers in response to external events.
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
//	func (p *MyProvider) ListOperations() []gestalt.Operation {
//		return []gestalt.Operation{{
//			Name:        "hello",
//			Description: "Says hello",
//			Method:      "GET",
//		}}
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
