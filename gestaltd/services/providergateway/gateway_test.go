package providergateway

import (
	"context"
	"testing"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

func TestAuthorizeAllowsRequests(t *testing.T) {
	t.Parallel()

	transport := NewProviderGatewayTransport(nil)
	allowed, err := transport.Authorize(context.Background(), AuthorizationParams{
		ProviderID:  "authz-primary",
		Operation:   "CheckAccess",
		CallerToken: "caller-token",
	})
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if !allowed {
		t.Fatal("Authorize allowed = false, want true")
	}
}

func TestProviderGatewayTransportAuthorizesThenInvokesNext(t *testing.T) {
	t.Parallel()

	inner := &recordingTransport{}
	transport := NewProviderGatewayTransport(inner)
	req := ProviderGatewayRequest{
		ProviderID:   "authz-primary",
		ProviderKind: ProviderKindAuthorization,
		Operation:    "SetAuthorizationState",
	}
	nextCalled := false

	_, err := transport.Invoke(context.Background(), req, func(_ context.Context, got ProviderGatewayRequest) (ProviderGatewayResponse, error) {
		nextCalled = true
		if got.ProviderID != req.ProviderID {
			t.Fatalf("ProviderID = %q, want %q", got.ProviderID, req.ProviderID)
		}
		return ProviderGatewayResponse{}, nil
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !inner.called {
		t.Fatal("inner transport was not called")
	}
	if !nextCalled {
		t.Fatal("next was not called")
	}
}

func TestProviderGatewayTransportStoresAuthorizationProvider(t *testing.T) {
	t.Parallel()

	authorization := &stubAuthorizationProvider{}
	transport := NewProviderGatewayTransport(nil)

	transport.SetAuthorizationProvider(authorization)

	if transport.authorization != authorization {
		t.Fatal("authorization provider was not stored")
	}
}

func TestDirectTransportInvokesNext(t *testing.T) {
	t.Parallel()

	called := false
	req := ProviderGatewayRequest{ProviderID: "authz", Operation: "CheckAccess"}
	_, err := (DirectTransport{}).Invoke(context.Background(), req, func(_ context.Context, got ProviderGatewayRequest) (ProviderGatewayResponse, error) {
		called = true
		if got.ProviderID != req.ProviderID {
			t.Fatalf("ProviderID = %q, want %q", got.ProviderID, req.ProviderID)
		}
		return ProviderGatewayResponse{}, nil
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !called {
		t.Fatal("next was not called")
	}
}

type recordingTransport struct {
	called  bool
	request ProviderGatewayRequest
}

func (t *recordingTransport) Invoke(ctx context.Context, req ProviderGatewayRequest, next Next) (ProviderGatewayResponse, error) {
	t.called = true
	t.request = req
	return next(ctx, req)
}

type stubAuthorizationProvider struct{}

func (p *stubAuthorizationProvider) CheckAccess(context.Context, *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	return &proto.CheckAccessResponse{Allowed: true}, nil
}
