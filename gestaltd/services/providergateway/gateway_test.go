package providergateway

import (
	"context"
	"testing"
	"time"
)

func TestNewWithCallerTokenIssuer(t *testing.T) {
	t.Parallel()

	privateKeyPEM, _ := testCallerTokenKeyPair(t)
	issuer, err := NewCallerTokenIssuer(privateKeyPEM)
	if err != nil {
		t.Fatalf("NewCallerTokenIssuer: %v", err)
	}
	gateway := New(WithCallerTokenIssuer(issuer))

	if gateway.callerTokenIssuer == nil {
		t.Fatal("callerTokenIssuer = nil")
	}
}

func TestIssueCallerToken(t *testing.T) {
	t.Parallel()

	privateKeyPEM, publicKeyPEM := testCallerTokenKeyPair(t)
	issuer, err := NewCallerTokenIssuer(privateKeyPEM)
	if err != nil {
		t.Fatalf("NewCallerTokenIssuer: %v", err)
	}
	gateway := New(WithCallerTokenIssuer(issuer))

	now := time.Now().UTC()
	token, ok, err := gateway.IssueCallerToken("user:123", now)
	if err != nil {
		t.Fatalf("IssueCallerToken: %v", err)
	}
	if !ok {
		t.Fatal("IssueCallerToken ok = false, want true")
	}
	claims, err := Verify(token, publicKeyPEM)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.SubjectID != "user:123" {
		t.Fatalf("SubjectID = %q, want user:123", claims.SubjectID)
	}
}

func TestNewCallerTokenIssuerEmptyKey(t *testing.T) {
	t.Parallel()

	issuer, err := NewCallerTokenIssuer(" ")
	if err != nil {
		t.Fatalf("NewCallerTokenIssuer: %v", err)
	}
	if issuer != nil {
		t.Fatalf("issuer = %#v, want nil", issuer)
	}
}

func TestAuthorizeAllowsRequests(t *testing.T) {
	t.Parallel()

	gateway := New()
	allowed, err := gateway.Authorize(context.Background(), AuthorizationParams{
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

func TestInvokeAuthorizesThenCallsConfiguredTransport(t *testing.T) {
	t.Parallel()

	transport := &recordingTransport{}
	gateway := New(WithTransport(transport))
	req := ProviderGatewayRequest{
		ProviderID:  "authz-primary",
		Operation:   "CheckAccess",
		CallerToken: "caller-token",
	}
	nextCalled := false

	_, err := gateway.Invoke(context.Background(), req, func(_ context.Context, got ProviderGatewayRequest) (ProviderGatewayResponse, error) {
		nextCalled = true
		if got.ProviderID != req.ProviderID {
			t.Fatalf("ProviderID = %q, want %q", got.ProviderID, req.ProviderID)
		}
		if got.Operation != req.Operation {
			t.Fatalf("Operation = %q, want %q", got.Operation, req.Operation)
		}
		if got.CallerToken != req.CallerToken {
			t.Fatalf("CallerToken = %q, want %q", got.CallerToken, req.CallerToken)
		}
		return ProviderGatewayResponse{}, nil
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !transport.called {
		t.Fatal("transport was not called")
	}
	if transport.request.ProviderID != req.ProviderID {
		t.Fatalf("ProviderID = %q, want %q", transport.request.ProviderID, req.ProviderID)
	}
	if !nextCalled {
		t.Fatal("next was not called")
	}
}

func TestProviderGatewayTransportInvokesNext(t *testing.T) {
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
