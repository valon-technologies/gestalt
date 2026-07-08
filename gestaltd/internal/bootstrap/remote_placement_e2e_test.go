package bootstrap_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const remotePlacementTestToken = "gst_api_remote_placement_test"

type fakeRemoteGestaltd struct {
	proto.UnimplementedAppServer
	proto.UnimplementedAgentServer

	mu        sync.Mutex
	authToken string
	appCalls  []fakeRemoteAppCall
	agentCalls []string
}

type fakeRemoteAppCall struct {
	app       string
	operation string
}

func (f *fakeRemoteGestaltd) recordAuth(ctx context.Context) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return
	}
	values := md.Get("authorization")
	if len(values) == 0 {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.authToken = values[0]
}

func (f *fakeRemoteGestaltd) Invoke(ctx context.Context, req *proto.AppInvokeRequest) (*proto.OperationResult, error) {
	f.recordAuth(ctx)
	f.mu.Lock()
	f.appCalls = append(f.appCalls, fakeRemoteAppCall{app: req.GetApp(), operation: req.GetOperation()})
	f.mu.Unlock()
	return &proto.OperationResult{Status: 200, Body: []byte(`{"remote":true}`)}, nil
}

func (f *fakeRemoteGestaltd) CreateSession(ctx context.Context, req *proto.CreateAgentProviderSessionRequest) (*proto.AgentSession, error) {
	f.recordAuth(ctx)
	f.mu.Lock()
	f.agentCalls = append(f.agentCalls, req.GetProviderName())
	f.mu.Unlock()
	return &proto.AgentSession{Id: "remote-session"}, nil
}

func (f *fakeRemoteGestaltd) snapshot() (auth string, apps []fakeRemoteAppCall, agents []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.authToken, append([]fakeRemoteAppCall(nil), f.appCalls...), append([]string(nil), f.agentCalls...)
}

type fakeRemoteServer struct {
	URL    string
	remote *fakeRemoteGestaltd
	lis    net.Listener
	srv    *grpc.Server
}

func startFakeRemoteGestaltd(t *testing.T) *fakeRemoteServer {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	remote := &fakeRemoteGestaltd{}
	srv := grpc.NewServer()
	proto.RegisterAppServer(srv, remote)
	proto.RegisterAgentServer(srv, remote)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
	})
	return &fakeRemoteServer{
		URL:    "http://" + lis.Addr().String(),
		remote: remote,
		lis:    lis,
		srv:    srv,
	}
}

func remotePlacementConfig(remoteURL string, apps map[string]*config.ProviderEntry, agent map[string]*config.ProviderEntry) *config.Config {
	cfg := validConfig()
	cfg.Server.Remote = remoteURL
	cfg.Server.RemoteToken = remotePlacementTestToken
	cfg.Apps = apps
	if agent != nil {
		cfg.Providers.Agent = agent
	}
	return cfg
}

func localRESTAppEntry(devActive bool, baseURL string) *config.ProviderEntry {
	return &config.ProviderEntry{
		DevActive: devActive,
		ResolvedManifest: &providermanifestv1.Manifest{
			Spec: &providermanifestv1.Spec{
				Surfaces: &providermanifestv1.ProviderSurfaces{
					REST: &providermanifestv1.RESTSurface{
						BaseURL: baseURL,
						Operations: []providermanifestv1.ProviderOperation{
							{Name: "ping", Method: http.MethodGet, Path: "/ping"},
						},
					},
				},
			},
		},
	}
}

func bootstrapWithRemote(t *testing.T, cfg *config.Config) *bootstrap.Result {
	t.Helper()
	result, err := bootstrap.Bootstrap(context.Background(), cfg, validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	t.Cleanup(func() { _ = result.Close(context.Background()) })
	<-result.ProvidersReady
	return result
}

func testPrincipal(scopes ...string) *principal.Principal {
	return &principal.Principal{
		SubjectID: "user:remote-placement",
		Kind:      principal.KindUser,
		Scopes:    scopes,
	}
}

func TestRemotePlacementNothingLocal(t *testing.T) {
	t.Parallel()

	remote := startFakeRemoteGestaltd(t)
	cfg := remotePlacementConfig(remote.URL, map[string]*config.ProviderEntry{
		"linear":        {},
		"valon-profile": {},
	}, map[string]*config.ProviderEntry{
		"managed": {Default: true},
	})
	result := bootstrapWithRemote(t, cfg)

	if _, err := result.Invoker.Invoke(context.Background(), testPrincipal("linear"), "linear", "", "issues.list", nil); err != nil {
		t.Fatalf("Invoke(linear): %v", err)
	}
	if _, err := result.Invoker.Invoke(context.Background(), testPrincipal("valon-profile"), "valon-profile", "", "profile.get", nil); err != nil {
		t.Fatalf("Invoke(valon-profile): %v", err)
	}
	_, provider, err := result.AgentControl.ResolveProvider(context.Background(), "managed")
	if err != nil {
		t.Fatalf("ResolveProvider(managed): %v", err)
	}
	if _, err := provider.CreateSession(context.Background(), &proto.CreateAgentProviderSessionRequest{Model: "test"}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	auth, apps, agents := remote.remote.snapshot()
	if auth != "Bearer "+remotePlacementTestToken {
		t.Fatalf("authorization = %q, want bearer test token", auth)
	}
	if len(apps) != 2 {
		t.Fatalf("remote app calls = %d, want 2", len(apps))
	}
	if len(agents) != 1 || agents[0] != "managed" {
		t.Fatalf("remote agent calls = %#v, want [managed]", agents)
	}
}

func TestRemotePlacementCICDLocal(t *testing.T) {
	t.Parallel()

	localSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ping" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"local":"ci-cd"}`))
	}))
	t.Cleanup(localSrv.Close)

	remote := startFakeRemoteGestaltd(t)
	cfg := remotePlacementConfig(remote.URL, map[string]*config.ProviderEntry{
		"ci-cd":         localRESTAppEntry(true, localSrv.URL),
		"linear":        {},
		"valon-profile": {},
	}, map[string]*config.ProviderEntry{
		"managed": {Default: true},
	})
	result := bootstrapWithRemote(t, cfg)

	localResult, err := result.Invoker.Invoke(context.Background(), testPrincipal("ci-cd"), "ci-cd", "", "ping", nil)
	if err != nil {
		t.Fatalf("Invoke(ci-cd): %v", err)
	}
	if localResult == nil || string(localResult.Body) != `{"local":"ci-cd"}` {
		t.Fatalf("local ci-cd result = %#v", localResult)
	}
	if _, err := result.Invoker.Invoke(context.Background(), testPrincipal("linear"), "linear", "", "issues.list", nil); err != nil {
		t.Fatalf("Invoke(linear): %v", err)
	}
	if _, err := result.Invoker.Invoke(context.Background(), testPrincipal("valon-profile"), "valon-profile", "", "profile.get", nil); err != nil {
		t.Fatalf("Invoke(valon-profile): %v", err)
	}
	_, provider, err := result.AgentControl.ResolveProvider(context.Background(), "managed")
	if err != nil {
		t.Fatalf("ResolveProvider(managed): %v", err)
	}
	if _, err := provider.CreateSession(context.Background(), &proto.CreateAgentProviderSessionRequest{Model: "test"}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	_, apps, agents := remote.remote.snapshot()
	if len(apps) != 2 {
		t.Fatalf("remote app calls = %#v, want linear and valon-profile only", apps)
	}
	for _, call := range apps {
		if call.app == "ci-cd" {
			t.Fatalf("ci-cd should stay local, got remote call %#v", call)
		}
	}
	if len(agents) != 1 {
		t.Fatalf("remote agent calls = %d, want 1", len(agents))
	}
}

func TestRemotePlacementCICDAndValonProfileLocal(t *testing.T) {
	t.Parallel()

	ciCDSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"local":"ci-cd"}`))
	}))
	t.Cleanup(ciCDSrv.Close)
	profileSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"local":"valon-profile"}`))
	}))
	t.Cleanup(profileSrv.Close)

	remote := startFakeRemoteGestaltd(t)
	cfg := remotePlacementConfig(remote.URL, map[string]*config.ProviderEntry{
		"ci-cd":         localRESTAppEntry(true, ciCDSrv.URL),
		"valon-profile": localRESTAppEntry(true, profileSrv.URL),
		"linear":        {},
	}, map[string]*config.ProviderEntry{
		"managed": {Default: true},
	})
	result := bootstrapWithRemote(t, cfg)

	if _, err := result.Invoker.Invoke(context.Background(), testPrincipal("valon-profile"), "valon-profile", "", "ping", nil); err != nil {
		t.Fatalf("Invoke(valon-profile): %v", err)
	}
	if _, err := result.Invoker.Invoke(context.Background(), testPrincipal("linear"), "linear", "", "issues.list", nil); err != nil {
		t.Fatalf("Invoke(linear): %v", err)
	}
	_, provider, err := result.AgentControl.ResolveProvider(context.Background(), "managed")
	if err != nil {
		t.Fatalf("ResolveProvider(managed): %v", err)
	}
	if _, err := provider.CreateSession(context.Background(), &proto.CreateAgentProviderSessionRequest{Model: "test"}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	_, apps, _ := remote.remote.snapshot()
	if len(apps) != 1 || apps[0].app != "linear" {
		t.Fatalf("remote app calls = %#v, want linear only", apps)
	}
}

func TestRemotePlacementUndeclaredAppNotFound(t *testing.T) {
	t.Parallel()

	remote := startFakeRemoteGestaltd(t)
	cfg := remotePlacementConfig(remote.URL, map[string]*config.ProviderEntry{
		"linear": {},
	}, nil)
	result := bootstrapWithRemote(t, cfg)

	_, err := result.Invoker.Invoke(context.Background(), testPrincipal("missing"), "missing", "", "op", nil)
	if !errors.Is(err, invocation.ErrProviderNotFound) {
		t.Fatalf("err = %v, want ErrProviderNotFound", err)
	}
	_, apps, _ := remote.remote.snapshot()
	if len(apps) != 0 {
		t.Fatalf("remote app calls = %#v, want none for undeclared app", apps)
	}
}

func TestRemotePlacementLocalStartupFailureDoesNotFallback(t *testing.T) {
	t.Parallel()

	remote := startFakeRemoteGestaltd(t)
	cfg := remotePlacementConfig(remote.URL, map[string]*config.ProviderEntry{
		"ci-cd": {
			DevActive: true,
			Source:    config.ProviderSource{Path: "/definitely/missing/provider"},
		},
		"linear": {},
	}, nil)

	_, err := bootstrap.Bootstrap(context.Background(), cfg, validFactories())
	if err == nil {
		t.Fatal("Bootstrap: expected startup failure for broken local provider, got nil")
	}
	if !strings.Contains(err.Error(), "ci-cd") && !strings.Contains(err.Error(), "missing") {
		t.Fatalf("Bootstrap error = %v, want local ci-cd startup failure", err)
	}
	_, apps, _ := remote.remote.snapshot()
	if len(apps) != 0 {
		t.Fatalf("remote app calls = %#v, want no remote fallback after local startup failure", apps)
	}
}

func TestRemotePlacementDisabledWhenRemoteUnset(t *testing.T) {
	t.Parallel()

	localSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"local":"linear"}`))
	}))
	t.Cleanup(localSrv.Close)

	cfg := validConfig()
	cfg.Apps = map[string]*config.ProviderEntry{
		"linear": localRESTAppEntry(false, localSrv.URL),
	}
	result, err := bootstrap.Bootstrap(context.Background(), cfg, validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	t.Cleanup(func() { _ = result.Close(context.Background()) })
	<-result.ProvidersReady

	resultBody, err := result.Invoker.Invoke(context.Background(), testPrincipal("linear"), "linear", "", "ping", nil)
	if err != nil {
		t.Fatalf("Invoke(linear): %v", err)
	}
	if resultBody == nil || string(resultBody.Body) != `{"local":"linear"}` {
		t.Fatalf("result = %#v, want local linear response", resultBody)
	}
}

func TestRemotePlacementAgentAuthFailureSurfaces(t *testing.T) {
	t.Parallel()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	srv := grpc.NewServer()
	proto.RegisterAgentServer(srv, &denyAgentServer{code: codes.PermissionDenied, msg: "remote denied"})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
	})

	cfg := remotePlacementConfig("http://"+lis.Addr().String(), nil, map[string]*config.ProviderEntry{
		"managed": {Default: true},
	})
	result := bootstrapWithRemote(t, cfg)

	_, provider, err := result.AgentControl.ResolveProvider(context.Background(), "managed")
	if err != nil {
		t.Fatalf("ResolveProvider(managed): %v", err)
	}
	_, err = provider.CreateSession(context.Background(), &proto.CreateAgentProviderSessionRequest{Model: "test"})
	if !errors.Is(err, invocation.ErrAuthorizationDenied) {
		t.Fatalf("CreateSession err = %v, want ErrAuthorizationDenied", err)
	}
}

type denyAgentServer struct {
	proto.UnimplementedAgentServer
	code codes.Code
	msg  string
}

func (s *denyAgentServer) CreateSession(context.Context, *proto.CreateAgentProviderSessionRequest) (*proto.AgentSession, error) {
	return nil, status.Error(s.code, s.msg)
}
