package bootstrap

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/remote"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	appservice "github.com/valon-technologies/gestalt/server/services/apps"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRegisterRemoteAppsSkipsLocalProviders(t *testing.T) {
	t.Parallel()

	local := &coretesting.StubIntegration{N: "demo", DN: "Demo"}
	providers := testutil.NewProviderRegistry(t, local)
	cfg := &config.Config{
		Server: config.ServerConfig{Remote: "https://valon.tools", RemoteToken: "token"},
		Apps: map[string]*config.ProviderEntry{
			"demo":   {DevActive: true},
			"linear": {},
		},
	}
	placement := NewPlacementPlan(cfg)
	clients := &remote.ClientSet{App: &remoteAppInvokeStub{}}

	if err := registerRemoteApps(providers, cfg, placement, clients); err != nil {
		t.Fatalf("registerRemoteApps: %v", err)
	}
	if _, err := providers.Get("demo"); err != nil {
		t.Fatalf("local demo provider missing: %v", err)
	}
	remoteProvider, err := providers.Get("linear")
	if err != nil {
		t.Fatalf("remote linear provider missing: %v", err)
	}
	if remoteProvider.Name() != "linear" {
		t.Fatalf("remote provider name = %q", remoteProvider.Name())
	}
}

func remoteLinearProvider(client proto.AppClient) core.Provider {
	return appservice.NewGestaltRemoteProvider(client, appservice.StaticProviderSpec{
		Name:           "linear",
		ConnectionMode: core.ConnectionModeNone,
		Catalog: &catalog.Catalog{
			Operations: []catalog.CatalogOperation{{ID: "issues.get", Transport: catalog.TransportREST}},
		},
	})
}

func TestBrokerInvokesRemoteAppWhenLocalMissing(t *testing.T) {
	t.Parallel()

	providers := testutil.NewProviderRegistry(t)
	services := testutil.NewStubServices(t)
	stub := &remoteAppInvokeStub{
		result: &proto.OperationResult{Status: 200, Body: []byte("ok")},
	}
	if err := providers.Register("linear", remoteLinearProvider(stub)); err != nil {
		t.Fatalf("Register: %v", err)
	}

	broker := invocation.NewBroker(providers, services.Users, services.ExternalCredentials)
	p := &principal.Principal{SubjectID: "user:test", Kind: principal.KindUser, Source: principal.SourceBearer}
	result, err := broker.Invoke(context.Background(), p, "linear", "", "issues.get", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result.Status != 200 {
		t.Fatalf("status = %d, want 200", result.Status)
	}
	if stub.calls != 1 {
		t.Fatalf("remote calls = %d, want 1", stub.calls)
	}
}

func TestBrokerLocalProviderWinsOverRemote(t *testing.T) {
	t.Parallel()

	local := &coretesting.StubIntegration{
		N:        "linear",
		DN:       "Linear",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{
			Operations: []catalog.CatalogOperation{{ID: "issues.get", Transport: catalog.TransportREST}},
		},
		ExecuteFn: func(context.Context, string, map[string]any, string) (*core.OperationResult, error) {
			return &core.OperationResult{Status: 200}, nil
		},
	}
	providers := testutil.NewProviderRegistry(t, local)
	services := testutil.NewStubServices(t)
	stub := &remoteAppInvokeStub{}
	if err := providers.Register("linear-remote", remoteLinearProvider(stub)); err != nil {
		t.Fatalf("Register remote duplicate: %v", err)
	}

	broker := invocation.NewBroker(providers, services.Users, services.ExternalCredentials)
	p := &principal.Principal{SubjectID: "user:test", Kind: principal.KindUser, Source: principal.SourceBearer}
	if _, err := broker.Invoke(context.Background(), p, "linear", "", "issues.get", nil); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if stub.calls != 0 {
		t.Fatal("expected local provider to win over remote")
	}
}

func TestBrokerUnknownAppRemainsNotFound(t *testing.T) {
	t.Parallel()

	providers := testutil.NewProviderRegistry(t)
	broker := invocation.NewBroker(providers, nil, nil)
	p := &principal.Principal{SubjectID: "user:test", Kind: principal.KindUser, Source: principal.SourceBearer}
	_, err := broker.Invoke(context.Background(), p, "missing-app", "", "issues.get", nil)
	if !errors.Is(err, invocation.ErrProviderNotFound) {
		t.Fatalf("Invoke err = %v, want ErrProviderNotFound", err)
	}
}

type remoteAppInvokeStub struct {
	calls  int
	result *proto.OperationResult
	err    error
}

func (s *remoteAppInvokeStub) Invoke(context.Context, *proto.AppInvokeRequest, ...grpc.CallOption) (*proto.OperationResult, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	if s.result != nil {
		return s.result, nil
	}
	return &proto.OperationResult{Status: 200}, nil
}

func (s *remoteAppInvokeStub) InvokeGraphQL(context.Context, *proto.AppInvokeGraphQLRequest, ...grpc.CallOption) (*proto.OperationResult, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return &proto.OperationResult{Status: 200}, nil
}

func TestBrokerRemoteAuthErrorsPreserveStatus(t *testing.T) {
	t.Parallel()

	providers := testutil.NewProviderRegistry(t)
	services := testutil.NewStubServices(t)
	stub := &remoteAppInvokeStub{
		err: status.Error(codes.PermissionDenied, "remote access denied"),
	}
	if err := providers.Register("linear", remoteLinearProvider(stub)); err != nil {
		t.Fatalf("Register: %v", err)
	}

	broker := invocation.NewBroker(providers, services.Users, services.ExternalCredentials)
	p := &principal.Principal{SubjectID: "user:test", Kind: principal.KindUser, Source: principal.SourceBearer}
	_, err := broker.Invoke(context.Background(), p, "linear", "", "issues.get", nil)
	if !errors.Is(err, invocation.ErrAuthorizationDenied) {
		t.Fatalf("Invoke err = %v, want ErrAuthorizationDenied", err)
	}
}

var _ core.Provider = appservice.NewGestaltRemoteProvider(&remoteAppInvokeStub{}, appservice.StaticProviderSpec{Name: "linear"})
