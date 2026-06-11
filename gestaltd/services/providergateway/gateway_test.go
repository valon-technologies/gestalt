package providergateway

import (
	"context"
	"testing"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
)

func TestAuthorizeAllowsRequests(t *testing.T) {
	t.Parallel()

	transport := NewProviderGatewayTransport()
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

func TestAuthorizePassesThroughWithAuthorizationProvider(t *testing.T) {
	t.Parallel()

	authorization := &stubAuthorizationProvider{}
	transport := NewProviderGatewayTransport()
	transport.SetAuthorizationProvider(authorization)

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
	if authorization.called {
		t.Fatal("authorization provider was called")
	}
}

func TestProviderGatewayTransportAuthorizesThenInvokesNext(t *testing.T) {
	t.Parallel()

	transport := NewProviderGatewayTransport()
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
	if !nextCalled {
		t.Fatal("next was not called")
	}
}

func TestProviderGatewayTransportStoresAuthorizationProvider(t *testing.T) {
	t.Parallel()

	authorization := &stubAuthorizationProvider{}
	transport := NewProviderGatewayTransport()

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

type stubAuthorizationProvider struct {
	called  bool
	ctx     context.Context
	request *proto.CheckAccessRequest
}

func (p *stubAuthorizationProvider) CheckAccess(ctx context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	p.called = true
	p.ctx = ctx
	p.request = req
	return &proto.CheckAccessResponse{Allowed: true}, nil
}

func (p *stubAuthorizationProvider) CheckAccessMany(context.Context, *proto.CheckAccessManyRequest) (*proto.CheckAccessManyResponse, error) {
	return &proto.CheckAccessManyResponse{}, nil
}

func (p *stubAuthorizationProvider) ListRelationships(context.Context, *proto.ListRelationshipsRequest) (*proto.ListRelationshipsResponse, error) {
	return &proto.ListRelationshipsResponse{}, nil
}

func (p *stubAuthorizationProvider) AddRelationship(context.Context, *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error) {
	return &proto.AddRelationshipResponse{}, nil
}

func (p *stubAuthorizationProvider) DeleteRelationship(context.Context, *proto.DeleteRelationshipRequest) (*proto.DeleteRelationshipResponse, error) {
	return &proto.DeleteRelationshipResponse{}, nil
}

func (p *stubAuthorizationProvider) SetAuthorizationState(context.Context, *proto.SetAuthorizationStateRequest) (*proto.SetAuthorizationStateResponse, error) {
	return &proto.SetAuthorizationStateResponse{}, nil
}

func (p *stubAuthorizationProvider) GetActiveModelRef(context.Context) (*proto.GetActiveModelRefResponse, error) {
	return &proto.GetActiveModelRefResponse{}, nil
}

func (p *stubAuthorizationProvider) SetActiveModel(context.Context, *proto.SetActiveModelRequest) (*proto.SetActiveModelResponse, error) {
	return &proto.SetActiveModelResponse{}, nil
}

func (p *stubAuthorizationProvider) ListActiveModelResourceTypes(context.Context, *proto.ListActiveModelResourceTypesRequest) (*proto.ListActiveModelResourceTypesResponse, error) {
	return &proto.ListActiveModelResourceTypesResponse{}, nil
}

func (p *stubAuthorizationProvider) Ping(context.Context) error { return nil }

func (p *stubAuthorizationProvider) Close() error { return nil }

func TestGatewayInvokeRecordsMetrics(t *testing.T) {
	metrics := metrictest.NewManualMeterProvider(t)
	ctx := metricutil.WithMeterProvider(context.Background(), metrics.Provider)
	gateway := New()
	req := ProviderGatewayRequest{
		ProviderID:   "authz",
		ProviderKind: ProviderKindAuthorization,
		ServiceName:  "gestalt.v1.Authorization",
		Operation:    "CheckAccess",
		Source:       GatewaySourceSDKGRPC,
	}

	_, err := gateway.Invoke(ctx, req, func(ctx context.Context, req ProviderGatewayRequest) (ProviderGatewayResponse, error) {
		return ProviderGatewayResponse{Payload: []byte("ok")}, nil
	})
	if err != nil {
		t.Fatalf("Invoke error = %v", err)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	attrs := providerGatewayMetricAttrs(req)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.provider_gateway.operation.count", 1, attrs)
	metrictest.RequireFloat64Histogram(t, rm, "gestaltd.provider_gateway.operation.duration", attrs)
	metrictest.RequireNoInt64Sum(t, rm, "gestaltd.provider_gateway.operation.error_count", attrs)
}

func TestGatewayInvokeRecordsErrorMetrics(t *testing.T) {
	metrics := metrictest.NewManualMeterProvider(t)
	ctx := metricutil.WithMeterProvider(context.Background(), metrics.Provider)
	gateway := New()
	req := ProviderGatewayRequest{
		ProviderID:   "authz",
		ProviderKind: ProviderKindAuthorization,
		ServiceName:  "gestalt.v1.Authorization",
		Operation:    "CheckAccess",
		Source:       GatewaySourceInternal,
	}
	wantErr := errors.New("provider failed")

	_, err := gateway.Invoke(ctx, req, func(ctx context.Context, req ProviderGatewayRequest) (ProviderGatewayResponse, error) {
		return ProviderGatewayResponse{}, wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Invoke error = %v, want %v", err, wantErr)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	attrs := providerGatewayMetricAttrs(req)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.provider_gateway.operation.count", 1, attrs)
	metrictest.RequireFloat64Histogram(t, rm, "gestaltd.provider_gateway.operation.duration", attrs)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.provider_gateway.operation.error_count", 1, attrs)
}

func providerGatewayMetricAttrs(req ProviderGatewayRequest) map[string]string {
	return map[string]string{
		"gestaltd.provider_gateway.provider.id":    req.ProviderID,
		"gestaltd.provider_gateway.provider.kind":  string(req.ProviderKind),
		"gestaltd.provider_gateway.service.name":   req.ServiceName,
		"gestaltd.provider_gateway.operation.name": req.Operation,
		"gestaltd.provider_gateway.source":         string(req.Source),
	}
}
