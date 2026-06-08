package gestalt

import "context"

// HelloWorldRequest carries no input fields. It exists to exercise the
// SDK-to-proto provider adapter path.
type HelloWorldRequest struct{}

// HelloWorldResponse returns the fixed test-provider message.
type HelloWorldResponse struct {
	Message string
}

// TestProvider is implemented by providers that serve the minimal test
// provider surface over gRPC.
type TestProvider interface {
	Provider
	HelloWorld(ctx context.Context, req *HelloWorldRequest) (*HelloWorldResponse, error)
}
