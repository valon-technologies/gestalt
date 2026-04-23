package server_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	gestaltsdk "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	corecrypto "github.com/valon-technologies/gestalt/server/core/crypto"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coreintegration "github.com/valon-technologies/gestalt/server/core/integration"
	"github.com/valon-technologies/gestalt/server/core/session"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/adminui"
	"github.com/valon-technologies/gestalt/server/internal/apiexec"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/composite"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/emailutil"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	gestaltmcp "github.com/valon-technologies/gestalt/server/internal/mcp"
	"github.com/valon-technologies/gestalt/server/internal/oauth"
	"github.com/valon-technologies/gestalt/server/internal/pluginruntime"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/provider"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	"github.com/valon-technologies/gestalt/server/internal/registry"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/internal/ui"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	grpcstatus "google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
)

func newTestServer(t *testing.T, opts ...func(*server.Config)) *httptest.Server {
	t.Helper()
	return newTestHTTPServer(t, httptest.NewServer, opts...)
}

func newTestHTTPServer(t *testing.T, start func(http.Handler) *httptest.Server, opts ...func(*server.Config)) *httptest.Server {
	t.Helper()
	return start(newTestHandler(t, opts...))
}

func newTestHandler(t *testing.T, opts ...func(*server.Config)) http.Handler {
	t.Helper()
	cfg := server.Config{
		Auth:     &coretesting.StubAuthProvider{N: "none"},
		Services: coretesting.NewStubServices(t),
		Providers: func() *registry.ProviderMap[core.Provider] {
			reg := registry.New()
			return &reg.Providers
		}(),
		StateSecret: []byte("0123456789abcdef0123456789abcdef"),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	brokerOpts := []invocation.BrokerOption{}
	if cfg.DefaultConnection != nil {
		brokerOpts = append(brokerOpts, invocation.WithConnectionMapper(invocation.ConnectionMap(cfg.DefaultConnection)))
	}
	if cfg.CatalogConnection != nil {
		brokerOpts = append(brokerOpts,
			invocation.WithMCPConnectionMapper(invocation.ConnectionMap(cfg.CatalogConnection)),
		)
	}
	if cfg.ConnectionAuth != nil {
		authFn := cfg.ConnectionAuth
		brokerOpts = append(brokerOpts, invocation.WithConnectionAuth(func() map[string]map[string]invocation.OAuthRefresher {
			m := authFn()
			refreshers := make(map[string]map[string]invocation.OAuthRefresher, len(m))
			for intg, conns := range m {
				inner := make(map[string]invocation.OAuthRefresher, len(conns))
				for conn, h := range conns {
					inner[conn] = h
				}
				refreshers[intg] = inner
			}
			return refreshers
		}))
	}
	if cfg.Authorizer != nil {
		brokerOpts = append(brokerOpts, invocation.WithAuthorizer(cfg.Authorizer))
	}
	if cfg.Invoker == nil {
		cfg.Invoker = invocation.NewBroker(cfg.Providers, cfg.Services.Users, cfg.Services.Tokens, brokerOpts...)
	}
	srv, err := server.New(cfg)
	if err != nil {
		t.Fatalf("creating server: %v", err)
	}
	return srv
}

type staticRuntimeInspector struct {
	snapshots []bootstrap.RuntimeProviderSnapshot
	err       error
}

func (s *staticRuntimeInspector) SnapshotPluginRuntimes(context.Context) ([]bootstrap.RuntimeProviderSnapshot, error) {
	if s == nil {
		return nil, nil
	}
	if s.err != nil {
		return nil, s.err
	}
	out := make([]bootstrap.RuntimeProviderSnapshot, 0, len(s.snapshots))
	for _, snapshot := range s.snapshots {
		cloned := snapshot
		cloned.Sessions = make([]pluginruntime.Session, 0, len(snapshot.Sessions))
		for _, session := range snapshot.Sessions {
			cloned.Sessions = append(cloned.Sessions, pluginruntime.Session{
				ID:       session.ID,
				State:    session.State,
				Metadata: maps.Clone(session.Metadata),
			})
		}
		out = append(out, cloned)
	}
	return out, nil
}

type relayTestCacheServer struct {
	proto.UnimplementedCacheServer

	mu             sync.Mutex
	receivedTokens []string
}

func (s *relayTestCacheServer) Get(ctx context.Context, req *proto.CacheGetRequest) (*proto.CacheGetResponse, error) {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		s.mu.Lock()
		s.receivedTokens = append(s.receivedTokens, md.Get(providerhost.HostServiceRelayTokenHeader)...)
		s.mu.Unlock()
	}
	return &proto.CacheGetResponse{
		Found: true,
		Value: []byte("relay:" + req.GetKey()),
	}, nil
}

func (s *relayTestCacheServer) relayTokens() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.receivedTokens...)
}

func TestHostServiceRelayProxiesGRPCRequests(t *testing.T) {
	t.Parallel()

	secret := []byte("relay-test-secret-0123456789abcd")
	cacheSrv := &relayTestCacheServer{}
	hostServices, err := providerhost.StartHostServices([]providerhost.HostService{{
		EnvVar: "GESTALT_TEST_CACHE_SOCKET",
		Register: func(srv *grpc.Server) {
			proto.RegisterCacheServer(srv, cacheSrv)
		},
	}})
	if err != nil {
		t.Fatalf("StartHostServices: %v", err)
	}
	t.Cleanup(func() {
		_ = hostServices.Close()
	})

	bindings := hostServices.Bindings()
	if len(bindings) != 1 {
		t.Fatalf("host service bindings len = %d, want 1", len(bindings))
	}

	ts := httptest.NewUnstartedServer(newTestHandler(t, func(cfg *server.Config) {
		cfg.RouteProfile = server.RouteProfilePublic
		cfg.StateSecret = secret
	}))
	ts.EnableHTTP2 = true
	ts.StartTLS()
	testutil.CloseOnCleanup(t, ts)

	tokenManager, err := providerhost.NewHostServiceRelayTokenManager(secret)
	if err != nil {
		t.Fatalf("NewHostServiceRelayTokenManager: %v", err)
	}
	token, err := tokenManager.MintToken(providerhost.HostServiceRelayTokenRequest{
		PluginName:   "support",
		SessionID:    "session-1",
		Service:      "cache",
		SocketPath:   bindings[0].SocketPath,
		MethodPrefix: "/gestalt.provider.v1.Cache/",
		TTL:          time.Minute,
	})
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}

	conn := newRelayGRPCConn(t, ts)
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(providerhost.HostServiceRelayTokenHeader, token))

	resp, err := proto.NewCacheClient(conn).Get(ctx, &proto.CacheGetRequest{Key: "hello"})
	if err != nil {
		t.Fatalf("Cache.Get via relay: %v", err)
	}
	if !resp.GetFound() {
		t.Fatalf("Cache.Get found = false, want true")
	}
	if got := string(resp.GetValue()); got != "relay:hello" {
		t.Fatalf("Cache.Get value = %q, want relay:hello", got)
	}
	if got := cacheSrv.relayTokens(); len(got) != 0 {
		t.Fatalf("backend unexpectedly received relay token metadata: %#v", got)
	}
}

func TestHostServiceRelayRejectsInvalidToken(t *testing.T) {
	t.Parallel()

	secret := []byte("relay-test-secret-0123456789abcd")
	ts := httptest.NewUnstartedServer(newTestHandler(t, func(cfg *server.Config) {
		cfg.RouteProfile = server.RouteProfilePublic
		cfg.StateSecret = secret
	}))
	ts.EnableHTTP2 = true
	ts.StartTLS()
	testutil.CloseOnCleanup(t, ts)

	conn := newRelayGRPCConn(t, ts)
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(providerhost.HostServiceRelayTokenHeader, "not-a-valid-token"))

	_, err := proto.NewCacheClient(conn).Get(ctx, &proto.CacheGetRequest{Key: "hello"})
	if grpcstatus.Code(err) != codes.Unauthenticated {
		t.Fatalf("Cache.Get invalid token code = %v, want %v (err=%v)", grpcstatus.Code(err), codes.Unauthenticated, err)
	}
}

func TestHostServiceRelayRejectsMethodOutsideTokenPrefix(t *testing.T) {
	t.Parallel()

	secret := []byte("relay-test-secret-0123456789abcd")
	cacheSrv := &relayTestCacheServer{}
	hostServices, err := providerhost.StartHostServices([]providerhost.HostService{{
		EnvVar: "GESTALT_TEST_CACHE_SOCKET",
		Register: func(srv *grpc.Server) {
			proto.RegisterCacheServer(srv, cacheSrv)
		},
	}})
	if err != nil {
		t.Fatalf("StartHostServices: %v", err)
	}
	t.Cleanup(func() {
		_ = hostServices.Close()
	})

	bindings := hostServices.Bindings()
	if len(bindings) != 1 {
		t.Fatalf("host service bindings len = %d, want 1", len(bindings))
	}

	ts := httptest.NewUnstartedServer(newTestHandler(t, func(cfg *server.Config) {
		cfg.RouteProfile = server.RouteProfilePublic
		cfg.StateSecret = secret
	}))
	ts.EnableHTTP2 = true
	ts.StartTLS()
	testutil.CloseOnCleanup(t, ts)

	tokenManager, err := providerhost.NewHostServiceRelayTokenManager(secret)
	if err != nil {
		t.Fatalf("NewHostServiceRelayTokenManager: %v", err)
	}
	token, err := tokenManager.MintToken(providerhost.HostServiceRelayTokenRequest{
		PluginName:   "support",
		SessionID:    "session-1",
		Service:      "cache",
		SocketPath:   bindings[0].SocketPath,
		MethodPrefix: "/gestalt.provider.v1.IndexedDB/",
		TTL:          time.Minute,
	})
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}

	conn := newRelayGRPCConn(t, ts)
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(providerhost.HostServiceRelayTokenHeader, token))

	_, err = proto.NewCacheClient(conn).Get(ctx, &proto.CacheGetRequest{Key: "hello"})
	if grpcstatus.Code(err) != codes.PermissionDenied {
		t.Fatalf("Cache.Get disallowed method code = %v, want %v (err=%v)", grpcstatus.Code(err), codes.PermissionDenied, err)
	}
}

func TestHostServiceRelaySupportsIndexedDBSDKClient(t *testing.T) {
	t.Parallel()

	secret := []byte("relay-test-secret-0123456789abcd")
	stubDB := &coretesting.StubIndexedDB{}
	hostServices, err := providerhost.StartHostServices([]providerhost.HostService{{
		EnvVar: providerhost.DefaultIndexedDBSocketEnv,
		Register: func(srv *grpc.Server) {
			proto.RegisterIndexedDBServer(srv, providerhost.NewIndexedDBServer(stubDB, "relay-plugin", providerhost.IndexedDBServerOptions{
				AllowedStores: []string{"tasks"},
			}))
		},
	}})
	if err != nil {
		t.Fatalf("StartHostServices: %v", err)
	}
	t.Cleanup(func() {
		_ = hostServices.Close()
	})

	bindings := hostServices.Bindings()
	if len(bindings) != 1 {
		t.Fatalf("host service bindings len = %d, want 1", len(bindings))
	}

	ts := httptest.NewUnstartedServer(newTestHandler(t, func(cfg *server.Config) {
		cfg.RouteProfile = server.RouteProfilePublic
		cfg.StateSecret = secret
	}))
	ts.EnableHTTP2 = true
	ts.StartTLS()
	testutil.CloseOnCleanup(t, ts)

	tokenManager, err := providerhost.NewHostServiceRelayTokenManager(secret)
	if err != nil {
		t.Fatalf("NewHostServiceRelayTokenManager: %v", err)
	}
	token, err := tokenManager.MintToken(providerhost.HostServiceRelayTokenRequest{
		PluginName:   "relay-plugin",
		SessionID:    "session-1",
		Service:      "indexeddb",
		SocketPath:   bindings[0].SocketPath,
		MethodPrefix: "/" + proto.IndexedDB_ServiceDesc.ServiceName + "/",
		TTL:          time.Minute,
	})
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(providerhost.HostServiceRelayTokenHeader, token))

	recordValue, err := gestaltsdk.RecordToProto(gestaltsdk.Record{"id": "task-1", "value": "ship-it"})
	if err != nil {
		t.Fatalf("RecordToProto: %v", err)
	}
	conn := newRelayGRPCConn(t, ts)
	defer func() { _ = conn.Close() }()
	client := proto.NewIndexedDBClient(conn)
	if _, err := client.CreateObjectStore(ctx, &proto.CreateObjectStoreRequest{Name: "tasks"}); err != nil {
		t.Fatalf("CreateObjectStore: %v", err)
	}
	if _, err := client.Put(ctx, &proto.RecordRequest{Store: "tasks", Record: recordValue}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	resp, err := client.Get(ctx, &proto.ObjectStoreRequest{Store: "tasks", Id: "task-1"})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	record, err := gestaltsdk.RecordFromProto(resp.GetRecord())
	if err != nil {
		t.Fatalf("RecordFromProto: %v", err)
	}
	if got := record["value"]; got != "ship-it" {
		t.Fatalf("record value = %#v, want %q", got, "ship-it")
	}
}

func TestEgressProxyProxiesHTTPRequest(t *testing.T) {
	t.Parallel()

	secret := []byte("relay-test-secret-0123456789abcd")
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/hello" {
			t.Fatalf("target path = %q, want /hello", got)
		}
		_, _ = io.WriteString(w, "proxied-ok")
	}))
	testutil.CloseOnCleanup(t, target)

	proxy := httptest.NewUnstartedServer(newTestHandler(t, func(cfg *server.Config) {
		cfg.RouteProfile = server.RouteProfilePublic
		cfg.StateSecret = secret
	}))
	proxy.EnableHTTP2 = true
	proxy.StartTLS()
	testutil.CloseOnCleanup(t, proxy)

	proxyURL := mustEgressProxyURL(t, proxy.URL, secret, providerhost.EgressProxyTokenRequest{
		PluginName:   "support",
		SessionID:    "session-1",
		AllowedHosts: []string{"127.0.0.1", "localhost"},
	})

	client := &http.Client{
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12},
		},
	}
	resp, err := client.Get(target.URL + "/hello")
	if err != nil {
		t.Fatalf("GET via egress proxy: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("proxy status = %d, want %d (body=%s)", resp.StatusCode, http.StatusOK, string(body))
	}
	if got := string(body); got != "proxied-ok" {
		t.Fatalf("proxy body = %q, want %q", got, "proxied-ok")
	}
}

func TestEgressProxyRejectsDisallowedHost(t *testing.T) {
	t.Parallel()

	secret := []byte("relay-test-secret-0123456789abcd")
	proxy := httptest.NewUnstartedServer(newTestHandler(t, func(cfg *server.Config) {
		cfg.RouteProfile = server.RouteProfilePublic
		cfg.StateSecret = secret
	}))
	proxy.EnableHTTP2 = true
	proxy.StartTLS()
	testutil.CloseOnCleanup(t, proxy)

	proxyURL := mustEgressProxyURL(t, proxy.URL, secret, providerhost.EgressProxyTokenRequest{
		PluginName:   "support",
		SessionID:    "session-1",
		AllowedHosts: []string{"api.github.com"},
	})

	client := &http.Client{
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12},
		},
	}
	resp, err := client.Get("http://example.com/blocked")
	if err != nil {
		t.Fatalf("GET blocked host via egress proxy: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("proxy status = %d, want %d (body=%s)", resp.StatusCode, http.StatusForbidden, string(body))
	}
	if !strings.Contains(string(body), "egress denied") {
		t.Fatalf("proxy body = %q, want egress denied", string(body))
	}
}

func TestEgressProxySupportsHTTPSConnect(t *testing.T) {
	t.Parallel()

	secret := []byte("relay-test-secret-0123456789abcd")
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "secure-proxied-ok")
	}))
	testutil.CloseOnCleanup(t, target)

	proxy := httptest.NewUnstartedServer(newTestHandler(t, func(cfg *server.Config) {
		cfg.RouteProfile = server.RouteProfilePublic
		cfg.StateSecret = secret
	}))
	proxy.EnableHTTP2 = true
	proxy.StartTLS()
	testutil.CloseOnCleanup(t, proxy)

	proxyURL := mustEgressProxyURL(t, proxy.URL, secret, providerhost.EgressProxyTokenRequest{
		PluginName:   "support",
		SessionID:    "session-1",
		AllowedHosts: []string{"127.0.0.1", "localhost"},
	})

	client := &http.Client{
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12},
		},
	}
	resp, err := client.Get(target.URL)
	if err != nil {
		t.Fatalf("GET https target via egress proxy: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("proxy status = %d, want %d (body=%s)", resp.StatusCode, http.StatusOK, string(body))
	}
	if got := string(body); got != "secure-proxied-ok" {
		t.Fatalf("proxy body = %q, want %q", got, "secure-proxied-ok")
	}
}

func newRelayGRPCConn(t *testing.T, ts *httptest.Server) *grpc.ClientConn {
	t.Helper()
	targetURL, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("parse relay URL: %v", err)
	}
	conn, err := grpc.NewClient(
		targetURL.Host,
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	return conn
}

func mustEgressProxyURL(t *testing.T, baseURL string, secret []byte, req providerhost.EgressProxyTokenRequest) *url.URL {
	t.Helper()

	tokenManager, err := providerhost.NewEgressProxyTokenManager(secret)
	if err != nil {
		t.Fatalf("NewEgressProxyTokenManager: %v", err)
	}
	token, err := tokenManager.MintToken(req)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("Parse proxy URL: %v", err)
	}
	parsed.User = url.UserPassword("gestalt-egress-proxy", token)
	return parsed
}

type memoryAuthorizationProvider struct {
	name string

	mu            sync.Mutex
	activeModelID string
	models        []*core.AuthorizationModelRef
	relsByModel   map[string]map[string]*core.Relationship
	writeErr      error
}

func newMemoryAuthorizationProvider(name string) *memoryAuthorizationProvider {
	return &memoryAuthorizationProvider{
		name:        name,
		relsByModel: map[string]map[string]*core.Relationship{},
	}
}

func (p *memoryAuthorizationProvider) Name() string { return p.name }

func (p *memoryAuthorizationProvider) Evaluate(ctx context.Context, req *core.AccessEvaluationRequest) (*core.AccessDecision, error) {
	resp, err := p.EvaluateMany(ctx, &core.AccessEvaluationsRequest{Requests: []*core.AccessEvaluationRequest{req}})
	if err != nil {
		return nil, err
	}
	if len(resp.GetDecisions()) == 0 {
		return &core.AccessDecision{}, nil
	}
	return resp.GetDecisions()[0], nil
}

func (p *memoryAuthorizationProvider) EvaluateMany(_ context.Context, req *core.AccessEvaluationsRequest) (*core.AccessEvaluationsResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	resp := &core.AccessEvaluationsResponse{
		Decisions: make([]*core.AccessDecision, 0, len(req.GetRequests())),
	}
	rels := p.relsByModel[p.activeModelID]
	for _, item := range req.GetRequests() {
		allowed := false
		if item != nil && rels != nil {
			_, allowed = rels[memoryAuthorizationRelationshipKey(item.GetSubject(), item.GetAction().GetName(), item.GetResource())]
		}
		resp.Decisions = append(resp.Decisions, &core.AccessDecision{
			Allowed: allowed,
			ModelId: p.activeModelID,
		})
	}
	return resp, nil
}

func (p *memoryAuthorizationProvider) SearchResources(context.Context, *core.ResourceSearchRequest) (*core.ResourceSearchResponse, error) {
	return &core.ResourceSearchResponse{}, nil
}

func (p *memoryAuthorizationProvider) SearchSubjects(context.Context, *core.SubjectSearchRequest) (*core.SubjectSearchResponse, error) {
	return &core.SubjectSearchResponse{}, nil
}

func (p *memoryAuthorizationProvider) SearchActions(context.Context, *core.ActionSearchRequest) (*core.ActionSearchResponse, error) {
	return &core.ActionSearchResponse{}, nil
}

func (p *memoryAuthorizationProvider) GetMetadata(context.Context) (*core.AuthorizationMetadata, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return &core.AuthorizationMetadata{ActiveModelId: p.activeModelID}, nil
}

func (p *memoryAuthorizationProvider) ReadRelationships(_ context.Context, req *core.ReadRelationshipsRequest) (*core.ReadRelationshipsResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	modelID := strings.TrimSpace(req.GetModelId())
	if modelID == "" {
		modelID = p.activeModelID
	}
	rels := p.relsByModel[modelID]
	keys := make([]string, 0, len(rels))
	for key, rel := range rels {
		if !memoryAuthorizationRelationshipMatches(rel, req) {
			continue
		}
		keys = append(keys, key)
	}
	slices.Sort(keys)

	start := 0
	if token := strings.TrimSpace(req.GetPageToken()); token != "" {
		offset, err := strconv.Atoi(token)
		if err != nil || offset < 0 {
			offset = 0
		}
		start = offset
	}
	if start > len(keys) {
		start = len(keys)
	}

	pageSize := int(req.GetPageSize())
	if pageSize <= 0 {
		pageSize = len(keys)
	}
	end := start + pageSize
	if end > len(keys) {
		end = len(keys)
	}

	out := make([]*core.Relationship, 0, end-start)
	for _, key := range keys[start:end] {
		out = append(out, cloneMemoryAuthorizationRelationship(rels[key]))
	}
	nextPageToken := ""
	if end < len(keys) {
		nextPageToken = strconv.Itoa(end)
	}
	return &core.ReadRelationshipsResponse{
		Relationships: out,
		NextPageToken: nextPageToken,
		ModelId:       modelID,
	}, nil
}

func (p *memoryAuthorizationProvider) WriteRelationships(_ context.Context, req *core.WriteRelationshipsRequest) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.writeErr != nil {
		return p.writeErr
	}
	modelID := strings.TrimSpace(req.GetModelId())
	if modelID == "" {
		modelID = p.activeModelID
	}
	rels := p.relsByModel[modelID]
	if rels == nil {
		rels = map[string]*core.Relationship{}
		p.relsByModel[modelID] = rels
	}
	for _, key := range req.GetDeletes() {
		delete(rels, memoryAuthorizationRelationshipKey(key.GetSubject(), key.GetRelation(), key.GetResource()))
	}
	for _, rel := range req.GetWrites() {
		rels[memoryAuthorizationRelationshipKey(rel.GetSubject(), rel.GetRelation(), rel.GetResource())] = cloneMemoryAuthorizationRelationship(rel)
	}
	return nil
}

func (p *memoryAuthorizationProvider) GetActiveModel(context.Context) (*core.GetActiveModelResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, model := range p.models {
		if model.GetId() == p.activeModelID {
			return &core.GetActiveModelResponse{Model: model}, nil
		}
	}
	return &core.GetActiveModelResponse{}, nil
}

func (p *memoryAuthorizationProvider) ListModels(context.Context, *core.ListModelsRequest) (*core.ListModelsResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return &core.ListModelsResponse{Models: append([]*core.AuthorizationModelRef(nil), p.models...)}, nil
}

func (p *memoryAuthorizationProvider) WriteModel(_ context.Context, req *core.WriteModelRequest) (*core.AuthorizationModelRef, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	definition := req.GetModel()
	if definition == nil {
		return nil, fmt.Errorf("model is required")
	}
	modelVersion := definition.GetVersion()
	if modelVersion == 0 {
		modelVersion = 1
	}
	modelBytes, err := gproto.MarshalOptions{Deterministic: true}.Marshal(definition)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(modelBytes)
	modelID := "model-" + hex.EncodeToString(sum[:])
	for _, existing := range p.models {
		if existing.GetId() == modelID {
			p.activeModelID = modelID
			if p.relsByModel[modelID] == nil {
				p.relsByModel[modelID] = map[string]*core.Relationship{}
			}
			return existing, nil
		}
	}
	model := &core.AuthorizationModelRef{
		Id:      modelID,
		Version: fmt.Sprintf("%d", modelVersion),
	}
	p.models = append(p.models, model)
	p.activeModelID = model.GetId()
	if p.relsByModel[model.GetId()] == nil {
		p.relsByModel[model.GetId()] = map[string]*core.Relationship{}
	}
	return model, nil
}

func memoryAuthorizationRelationshipMatches(rel *core.Relationship, req *core.ReadRelationshipsRequest) bool {
	if rel == nil || req == nil {
		return rel != nil
	}
	if subject := req.GetSubject(); subject != nil {
		if got := rel.GetSubject(); got == nil || got.GetType() != subject.GetType() || got.GetId() != subject.GetId() {
			return false
		}
	}
	if relation := strings.TrimSpace(req.GetRelation()); relation != "" && rel.GetRelation() != relation {
		return false
	}
	if resource := req.GetResource(); resource != nil {
		if got := rel.GetResource(); got == nil || got.GetType() != resource.GetType() || got.GetId() != resource.GetId() {
			return false
		}
	}
	return true
}

func memoryAuthorizationRelationshipKey(subject *core.SubjectRef, relation string, resource *core.ResourceRef) string {
	return strings.Join([]string{
		subject.GetType(),
		subject.GetId(),
		relation,
		resource.GetType(),
		resource.GetId(),
	}, "\x00")
}

func cloneMemoryAuthorizationRelationship(rel *core.Relationship) *core.Relationship {
	if rel == nil {
		return nil
	}
	return &core.Relationship{
		Subject: &core.SubjectRef{
			Type: rel.GetSubject().GetType(),
			Id:   rel.GetSubject().GetId(),
		},
		Relation: rel.GetRelation(),
		Resource: &core.ResourceRef{
			Type: rel.GetResource().GetType(),
			Id:   rel.GetResource().GetId(),
		},
	}
}

func (p *memoryAuthorizationProvider) putRelationship(modelID string, rel *core.Relationship) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.relsByModel[modelID] == nil {
		p.relsByModel[modelID] = map[string]*core.Relationship{}
	}
	p.relsByModel[modelID][memoryAuthorizationRelationshipKey(rel.GetSubject(), rel.GetRelation(), rel.GetResource())] = cloneMemoryAuthorizationRelationship(rel)
}

type putFailingIndexedDB struct {
	indexeddb.IndexedDB
	failStorePuts map[string]*atomic.Bool
}

func (d *putFailingIndexedDB) ObjectStore(name string) indexeddb.ObjectStore {
	store := d.IndexedDB.ObjectStore(name)
	failPut := d.failStorePuts[name]
	if failPut == nil {
		return store
	}
	return &putFailingObjectStore{ObjectStore: store, failPut: failPut}
}

type putFailingObjectStore struct {
	indexeddb.ObjectStore
	failPut *atomic.Bool
}

func (s *putFailingObjectStore) Put(ctx context.Context, record indexeddb.Record) error {
	if s.failPut != nil && s.failPut.Load() {
		return fmt.Errorf("forced users put failure")
	}
	return s.ObjectStore.Put(ctx, record)
}

func newTestServicesWithUsersPutFailure(t *testing.T) (*coredata.Services, *atomic.Bool) {
	t.Helper()
	enc, err := corecrypto.NewAESGCM([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("newTestServicesWithUsersPutFailure encryptor: %v", err)
	}
	failPut := &atomic.Bool{}
	svc, err := coredata.New(&putFailingIndexedDB{
		IndexedDB: &coretesting.StubIndexedDB{},
		failStorePuts: map[string]*atomic.Bool{
			coredata.StoreUsers: failPut,
		},
	}, enc)
	if err != nil {
		t.Fatalf("newTestServicesWithUsersPutFailure: %v", err)
	}
	return svc, failPut
}

func newVirtualHostClient(t *testing.T, hostAddrs map[string]string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	dialer := &net.Dialer{}
	return &http.Client{
		Jar: jar,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				if actual, ok := hostAddrs[addr]; ok {
					addr = actual
				}
				return dialer.DialContext(ctx, network, addr)
			},
		},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func extractHiddenInputValue(t *testing.T, html, name string) string {
	t.Helper()
	needle := fmt.Sprintf(`name="%s" value="`, name)
	start := strings.Index(html, needle)
	if start == -1 {
		t.Fatalf("missing hidden input %q in %q", name, html)
	}
	start += len(needle)
	end := strings.Index(html[start:], `"`)
	if end == -1 {
		t.Fatalf("unterminated hidden input %q in %q", name, html)
	}
	return html[start : start+end]
}

// testOAuthHandler adapts a test stub into bootstrap.OAuthHandler for use in
// server tests. Only the methods actually exercised by each test need non-nil
// implementations.
type testOAuthHandler struct {
	authorizationURLFn       func(state string, scopes []string) string
	startOAuthFn             func(state string, scopes []string) (string, string)
	startOAuthWithOverrideFn func(authBaseURL, state string, scopes []string) (string, string)
	exchangeCodeFn           func(ctx context.Context, code string) (*core.TokenResponse, error)
	exchangeCodeWithVerFn    func(ctx context.Context, code, verifier string, opts ...oauth.ExchangeOption) (*core.TokenResponse, error)
	refreshTokenFn           func(ctx context.Context, refreshToken string) (*core.TokenResponse, error)
	refreshTokenWithURLFn    func(ctx context.Context, refreshToken, tokenURL string) (*core.TokenResponse, error)
	authorizationBaseURLVal  string
	tokenURLVal              string
}

func (h *testOAuthHandler) AuthorizationURL(state string, scopes []string) string {
	if h.authorizationURLFn != nil {
		return h.authorizationURLFn(state, scopes)
	}
	url, _ := h.StartOAuth(state, scopes)
	return url
}

func (h *testOAuthHandler) StartOAuth(state string, scopes []string) (string, string) {
	if h.startOAuthFn != nil {
		return h.startOAuthFn(state, scopes)
	}
	return h.authorizationBaseURLVal + "?state=" + state, ""
}

func (h *testOAuthHandler) StartOAuthWithOverride(authBaseURL, state string, scopes []string) (string, string) {
	if h.startOAuthWithOverrideFn != nil {
		return h.startOAuthWithOverrideFn(authBaseURL, state, scopes)
	}
	return authBaseURL + "?state=" + state, ""
}

func (h *testOAuthHandler) ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error) {
	if h.exchangeCodeFn != nil {
		return h.exchangeCodeFn(ctx, code)
	}
	return nil, fmt.Errorf("ExchangeCode not implemented")
}

func (h *testOAuthHandler) ExchangeCodeWithVerifier(ctx context.Context, code, verifier string, opts ...oauth.ExchangeOption) (*core.TokenResponse, error) {
	if h.exchangeCodeWithVerFn != nil {
		return h.exchangeCodeWithVerFn(ctx, code, verifier, opts...)
	}
	return h.ExchangeCode(ctx, code)
}

func (h *testOAuthHandler) RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error) {
	if h.refreshTokenFn != nil {
		return h.refreshTokenFn(ctx, refreshToken)
	}
	return nil, fmt.Errorf("RefreshToken not implemented")
}

func (h *testOAuthHandler) RefreshTokenWithURL(ctx context.Context, refreshToken, tokenURL string) (*core.TokenResponse, error) {
	if h.refreshTokenWithURLFn != nil {
		return h.refreshTokenWithURLFn(ctx, refreshToken, tokenURL)
	}
	return h.RefreshToken(ctx, refreshToken)
}

func (h *testOAuthHandler) AuthorizationBaseURL() string { return h.authorizationBaseURLVal }
func (h *testOAuthHandler) TokenURL() string             { return h.tokenURLVal }

const (
	testDefaultConnection = "default"
	testCatalogConnection = "catalog"
	testCatalogToken      = "catalog-token"
)

func testConnectionAuth(integration string, handler bootstrap.OAuthHandler) func() map[string]map[string]bootstrap.OAuthHandler {
	m := map[string]map[string]bootstrap.OAuthHandler{
		integration: {testDefaultConnection: handler},
	}
	return func() map[string]map[string]bootstrap.OAuthHandler { return m }
}

func oauthRefreshConnectionAuth(integration string, refreshFn func(context.Context, string) (*core.TokenResponse, error)) func() map[string]map[string]bootstrap.OAuthHandler {
	return testConnectionAuth(integration, &testOAuthHandler{refreshTokenFn: refreshFn})
}

func seedAPIToken(t *testing.T, svc *coredata.Services, plaintext, hashed, userID string) {
	t.Helper()
	ctx := context.Background()
	user, err := svc.Users.FindOrCreateUser(ctx, userID+"@test.local")
	if err != nil {
		t.Fatalf("seedAPIToken: FindOrCreateUser: %v", err)
	}
	exp := time.Now().Add(24 * time.Hour)
	if err := svc.APITokens.StoreAPIToken(ctx, &core.APIToken{
		ID:                  "api-tok-" + userID,
		OwnerKind:           core.APITokenOwnerKindUser,
		OwnerID:             user.ID,
		CredentialSubjectID: principal.UserSubjectID(user.ID),
		Name:                "test-token",
		HashedToken:         hashed,
		ExpiresAt:           &exp,
	}); err != nil {
		t.Fatalf("seedAPIToken: StoreAPIToken: %v", err)
	}
}

func seedUser(t *testing.T, svc *coredata.Services, email string) *core.User {
	t.Helper()
	ctx := context.Background()
	u, err := svc.Users.FindOrCreateUser(ctx, email)
	if err != nil {
		t.Fatalf("seedUser: %v", err)
	}
	return u
}

func staticPolicyUserMember(t *testing.T, svc *coredata.Services, email, role string) config.HumanPolicyMemberDef {
	t.Helper()
	return config.HumanPolicyMemberDef{
		SubjectID: principal.UserSubjectID(seedUser(t, svc, email).ID),
		Role:      role,
	}
}

func seedLegacyUserRecord(t *testing.T, svc *coredata.Services, id, email string, createdAt time.Time) *core.User {
	t.Helper()
	ctx := context.Background()
	rec := indexeddb.Record{
		"id":               id,
		"email":            email,
		"normalized_email": emailutil.Normalize(email),
		"display_name":     "",
		"created_at":       createdAt,
		"updated_at":       createdAt,
	}
	if err := svc.DB.ObjectStore(coredata.StoreUsers).Add(ctx, rec); err != nil {
		t.Fatalf("seedLegacyUserRecord: %v", err)
	}
	return &core.User{
		ID:          id,
		Email:       email,
		DisplayName: "",
		CreatedAt:   createdAt,
		UpdatedAt:   createdAt,
	}
}

func seedSubjectToken(t *testing.T, svc *coredata.Services, subjectID, integration, connection, instance, accessToken string) {
	t.Helper()
	seedToken(t, svc, &core.IntegrationToken{
		ID:          integration + "-" + connection + "-" + instance,
		SubjectID:   subjectID,
		Integration: integration,
		Connection:  connection,
		Instance:    instance,
		AccessToken: accessToken,
	})
}

func mustAuthorizer(t *testing.T, cfg config.AuthorizationConfig, providers *registry.ProviderMap[core.Provider], pluginDefs map[string]*config.ProviderEntry, defaultConnections map[string]string) *authorization.Authorizer {
	t.Helper()
	authz, err := authorization.New(cfg, pluginDefs, providers, defaultConnections)
	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}
	return authz
}

func mustProviderBackedAuthorizer(t *testing.T, base *authorization.Authorizer, provider *memoryAuthorizationProvider) *authorization.ProviderBackedAuthorizer {
	t.Helper()
	authz, err := authorization.NewProviderBacked(base, provider)
	if err != nil {
		t.Fatalf("NewProviderBacked: %v", err)
	}
	if err := authz.ReloadAuthorizationState(context.Background()); err != nil {
		t.Fatalf("ReloadAuthorizationState: %v", err)
	}
	return authz
}

func testExternalIdentityResourceID(typ, id string) string {
	typ = strings.TrimSpace(typ)
	id = strings.TrimSpace(id)
	if typ == "" || id == "" {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(typ + "\x00" + id))
}

func seedProviderDynamicAdminMembership(t *testing.T, svc *coredata.Services, authz authorization.RuntimeAuthorizer, provider *memoryAuthorizationProvider, email, role string) *core.User {
	t.Helper()
	user := seedUser(t, svc, email)
	if provider.activeModelID == "" {
		if err := authz.ReloadAuthorizationState(context.Background()); err != nil {
			t.Fatalf("seedProviderDynamicAdminMembership authorization state reload: %v", err)
		}
	}
	if provider.activeModelID == "" {
		t.Fatal("seedProviderDynamicAdminMembership: provider has no active model")
	}
	provider.putRelationship(provider.activeModelID, &core.Relationship{
		Subject:  &core.SubjectRef{Type: authorization.ProviderSubjectTypeSubject, Id: principal.UserSubjectID(user.ID)},
		Relation: role,
		Resource: &core.ResourceRef{Type: authorization.ProviderResourceTypeAdminDynamic, Id: authorization.ProviderResourceIDAdminDynamicGlobal},
	})
	if err := authz.ReloadAuthorizationState(context.Background()); err != nil {
		t.Fatalf("seedProviderDynamicAdminMembership authorization state reload after write: %v", err)
	}
	return user
}

func seedProviderPluginAuthorization(t *testing.T, svc *coredata.Services, authz authorization.RuntimeAuthorizer, provider *memoryAuthorizationProvider, plugin, email, role string) *core.User {
	t.Helper()
	user := seedUser(t, svc, email)
	if provider.activeModelID == "" {
		if err := authz.ReloadAuthorizationState(context.Background()); err != nil {
			t.Fatalf("seedProviderPluginAuthorization authorization state reload: %v", err)
		}
	}
	if provider.activeModelID == "" {
		t.Fatal("seedProviderPluginAuthorization: provider has no active model")
	}
	provider.putRelationship(provider.activeModelID, &core.Relationship{
		Subject:  &core.SubjectRef{Type: authorization.ProviderSubjectTypeSubject, Id: principal.UserSubjectID(user.ID)},
		Relation: role,
		Resource: &core.ResourceRef{Type: authorization.ProviderResourceTypePluginDynamic, Id: plugin},
	})
	if err := authz.ReloadAuthorizationState(context.Background()); err != nil {
		t.Fatalf("seedProviderPluginAuthorization authorization state reload after write: %v", err)
	}
	return user
}

func seedToken(t *testing.T, svc *coredata.Services, tok *core.IntegrationToken) {
	t.Helper()
	ctx := context.Background()
	if err := svc.Tokens.StoreToken(ctx, tok); err != nil {
		t.Fatalf("seedToken: %v", err)
	}
}

func TestNewServerRequiresStateSecretWithAuth(t *testing.T) {
	t.Parallel()
	svc := coretesting.NewStubServices(t)
	providers := func() *registry.ProviderMap[core.Provider] {
		reg := registry.New()
		return &reg.Providers
	}()
	_, err := server.New(server.Config{
		Auth:      &coretesting.StubAuthProvider{N: "google"},
		Services:  svc,
		Providers: providers,
		Invoker:   invocation.NewBroker(providers, svc.Users, svc.Tokens),
	})
	if err == nil {
		t.Fatal("expected error when auth is enabled without state secret")
	}
	if !strings.Contains(err.Error(), "state secret is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewServerAdminAuthorizationRequiresValidSplitBaseURLs(t *testing.T) {
	t.Parallel()

	makeConfig := func() server.Config {
		svc := coretesting.NewStubServices(t)
		reg := registry.New()
		return server.Config{
			Auth:      &coretesting.StubAuthProvider{N: "google"},
			Services:  svc,
			Providers: &reg.Providers,
			Invoker:   invocation.NewBroker(&reg.Providers, svc.Users, svc.Tokens),
			StateSecret: []byte(
				"0123456789abcdef0123456789abcdef",
			),
			Admin: server.AdminRouteConfig{
				AuthorizationPolicy: "admin_policy",
			},
		}
	}

	tests := []struct {
		name              string
		routeProfile      server.RouteProfile
		publicBaseURL     string
		managementBaseURL string
		admin             server.AdminRouteConfig
		want              string
	}{
		{
			name:              "route profile all rejects management base url",
			routeProfile:      server.RouteProfileAll,
			publicBaseURL:     "https://gestalt.example.test",
			managementBaseURL: "https://gestalt.example.test:9090",
			admin:             server.AdminRouteConfig{AuthorizationPolicy: "admin_policy"},
			want:              "ManagementBaseURL requires RouteProfilePublic or RouteProfileManagement",
		},
		{
			name:              "route profile public rejects mismatched management hostname",
			routeProfile:      server.RouteProfilePublic,
			publicBaseURL:     "https://gestalt.example.test",
			managementBaseURL: "https://evil.example.test:9090",
			admin:             server.AdminRouteConfig{AuthorizationPolicy: "admin_policy"},
			want:              "PublicBaseURL and ManagementBaseURL must use the same hostname",
		},
		{
			name:              "route profile public rejects insecure management url",
			routeProfile:      server.RouteProfilePublic,
			publicBaseURL:     "https://gestalt.example.test",
			managementBaseURL: "http://gestalt.example.test:9090",
			admin:             server.AdminRouteConfig{AuthorizationPolicy: "admin_policy"},
			want:              "ManagementBaseURL must use https when PublicBaseURL uses https",
		},
		{
			name:         "blank allowed role is rejected",
			routeProfile: server.RouteProfileAll,
			admin: server.AdminRouteConfig{
				AuthorizationPolicy: "admin_policy",
				AllowedRoles:        []string{""},
			},
			want: "admin allowedRoles[0] is required",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := makeConfig()
			cfg.RouteProfile = tc.routeProfile
			cfg.PublicBaseURL = tc.publicBaseURL
			cfg.ManagementBaseURL = tc.managementBaseURL
			if tc.admin.AuthorizationPolicy != "" || tc.admin.AllowedRoles != nil {
				cfg.Admin = tc.admin
			}

			_, err := server.New(cfg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("server.New error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestHealthCheck(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("expected status ok, got %q", body["status"])
	}
}

func TestMountedUIRoutes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>sample-shell</html>"), 0o644); err != nil {
		t.Fatalf("WriteFile index.html: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatalf("MkdirAll assets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("console.log('sample')"), 0o644); err != nil {
		t.Fatalf("WriteFile app.js: %v", err)
	}
	handler, err := testutilUIHandler(dir)
	if err != nil {
		t.Fatalf("ui handler: %v", err)
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.MountedUIs = []server.MountedUI{{
			Path:    "/sample-portal",
			Handler: handler,
		}}
	})
	testutil.CloseOnCleanup(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := noRedirect.Get(ts.URL + "/sample-portal")
	if err != nil {
		t.Fatalf("GET mounted root: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusMovedPermanently)
	}
	if got := resp.Header.Get("Location"); got != "/sample-portal/" {
		t.Fatalf("Location = %q, want %q", got, "/sample-portal/")
	}

	resp, err = noRedirect.Get(ts.URL + "/sample-portal?code=invite-code&state=abc123")
	if err != nil {
		t.Fatalf("GET mounted root with query: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusMovedPermanently)
	}
	if got := resp.Header.Get("Location"); got != "/sample-portal/?code=invite-code&state=abc123" {
		t.Fatalf("Location = %q, want %q", got, "/sample-portal/?code=invite-code&state=abc123")
	}

	resp, err = http.Get(ts.URL + "/sample-portal/sync")
	if err != nil {
		t.Fatalf("GET mounted sync: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll mounted sync: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(string(body), "sample-shell") {
		t.Fatalf("body = %q, want sample shell", body)
	}

	resp, err = http.Get(ts.URL + "/sample-portal/assets/app.js")
	if err != nil {
		t.Fatalf("GET mounted asset: %v", err)
	}
	body, err = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll mounted asset: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("asset status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(string(body), "sample") {
		t.Fatalf("asset body = %q, want sample asset", body)
	}
}

func TestMountedUIRoutes_PrefersNestedMount(t *testing.T) {
	t.Parallel()

	parentDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(parentDir, "index.html"), []byte("<html>parent-shell</html>"), 0o644); err != nil {
		t.Fatalf("WriteFile parent index.html: %v", err)
	}
	childDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(childDir, "index.html"), []byte("<html>child-shell</html>"), 0o644); err != nil {
		t.Fatalf("WriteFile child index.html: %v", err)
	}

	parentHandler, err := testutilUIHandler(parentDir)
	if err != nil {
		t.Fatalf("parent ui handler: %v", err)
	}
	childHandler, err := testutilUIHandler(childDir)
	if err != nil {
		t.Fatalf("child ui handler: %v", err)
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.MountedUIs = []server.MountedUI{
			{
				Path:    "/workplace-hub",
				Handler: parentHandler,
			},
			{
				Path:    "/workplace-hub/nyc-badges",
				Handler: childHandler,
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/workplace-hub/nyc-badges/new-hire")
	if err != nil {
		t.Fatalf("GET nested mounted UI: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll nested mounted UI: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(string(body), "child-shell") {
		t.Fatalf("body = %q, want child shell", body)
	}

	resp, err = http.Get(ts.URL + "/workplace-hub/admin")
	if err != nil {
		t.Fatalf("GET parent mounted UI: %v", err)
	}
	body, err = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll parent mounted UI: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(string(body), "parent-shell") {
		t.Fatalf("body = %q, want parent shell", body)
	}
}

func TestMountedUIRoutes_HumanAuthorization(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>protected-sample-shell</html>"), 0o644); err != nil {
		t.Fatalf("WriteFile index.html: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatalf("MkdirAll assets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("console.log('protected-sample')"), 0o644); err != nil {
		t.Fatalf("WriteFile app.js: %v", err)
	}
	handler, err := testutilUIHandler(dir)
	if err != nil {
		t.Fatalf("ui handler: %v", err)
	}

	svc := coretesting.NewStubServices(t)
	viewer := seedUser(t, svc, "viewer@example.test")
	legacy := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(viewer.ID), Role: "viewer"},
				},
			},
		},
	}, nil, map[string]*config.ProviderEntry{
		"sample_portal": {AuthorizationPolicy: "sample_policy"},
	}, nil)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				switch token {
				case "viewer-session":
					return &core.UserIdentity{Email: "viewer@example.test"}, nil
				default:
					return nil, fmt.Errorf("invalid token")
				}
			},
		}
		cfg.Services = svc
		cfg.Authorizer = legacy
		cfg.MountedUIs = []server.MountedUI{{
			Name:                "sample_portal",
			Path:                "/sample-portal",
			AuthorizationPolicy: "sample_policy",
			Routes: []server.MountedUIRoute{
				{Path: "/admin/*", AllowedRoles: []string{"admin"}},
				{Path: "/*", AllowedRoles: []string{"viewer", "admin"}},
			},
			Handler: handler,
		}}
	})
	testutil.CloseOnCleanup(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	resp, err := noRedirect.Get(ts.URL + "/sample-portal/sync?code=invite-code&state=abc123")
	if err != nil {
		t.Fatalf("GET protected mounted sync without auth: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	if got, want := resp.Header.Get("Location"), "/api/v1/auth/login?next=%2Fsample-portal%2Fsync%3Fcode%3Dinvite-code%26state%3Dabc123"; got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/sample-portal/sync", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "viewer-session"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET protected mounted sync with viewer session: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll protected mounted sync: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(string(body), "protected-sample-shell") {
		t.Fatalf("body = %q, want protected sample shell", body)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/sample-portal/admin", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "viewer-session"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET protected mounted admin: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("admin status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/sample-portal/admin/index.html", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "viewer-session"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET protected mounted admin/index.html: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("admin/index.html status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/sample-portal/assets/app.js", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "viewer-session"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET protected mounted asset: %v", err)
	}
	body, err = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll protected mounted asset: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("asset status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(string(body), "protected-sample") {
		t.Fatalf("asset body = %q, want protected sample asset", body)
	}
}

func TestMountedUIRoutes_HumanAuthorization_UsesPluginRouteAuthOverride(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>protected-sample-shell</html>"), 0o644); err != nil {
		t.Fatalf("WriteFile index.html: %v", err)
	}
	handler, err := testutilUIHandler(dir)
	if err != nil {
		t.Fatalf("ui handler: %v", err)
	}

	svc := coretesting.NewStubServices(t)
	viewer := seedUser(t, svc, "viewer@example.test")
	legacy := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(viewer.ID), Role: "viewer"},
				},
			},
		},
	}, nil, map[string]*config.ProviderEntry{
		"sample_portal": {AuthorizationPolicy: "sample_policy"},
	}, nil)

	serverSecret := []byte("0123456789abcdef0123456789abcdef")
	altSecret := []byte("abcdef0123456789abcdef0123456789")
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &stubHostIssuedSessionAuth{
			secret:    serverSecret,
			name:      "server",
			loginHost: "server-idp.example.test",
			email:     "server@example.test",
		}
		cfg.StateSecret = altSecret
		cfg.SelectedAuthProvider = "server"
		cfg.AuthProviders = map[string]core.AuthenticationProvider{
			"alt": &stubHostIssuedSessionAuth{
				secret:    altSecret,
				name:      "alt",
				loginHost: "alt-idp.example.test",
				email:     "viewer@example.test",
			},
		}
		cfg.Services = svc
		cfg.Authorizer = legacy
		cfg.PluginDefs = map[string]*config.ProviderEntry{
			"sample_portal": {
				AuthorizationPolicy: "sample_policy",
				RouteAuth:           &config.RouteAuthDef{Provider: "alt"},
			},
		}
		cfg.MountedUIs = []server.MountedUI{{
			Name:                "sample_portal",
			Path:                "/sample-portal",
			PluginName:          "sample_portal",
			AuthorizationPolicy: "sample_policy",
			Routes: []server.MountedUIRoute{
				{Path: "/*", AllowedRoles: []string{"viewer", "admin"}},
			},
			Handler: handler,
		}}
	})
	testutil.CloseOnCleanup(t, ts)

	protectedPath := "/sample-portal/sync?code=invite-code&state=abc123"
	resp, err := client.Get(ts.URL + protectedPath)
	if err != nil {
		t.Fatalf("GET protected mounted ui without auth: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("start status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	if got, want := resp.Header.Get("Location"), "/api/v1/auth/login?next=%2Fsample-portal%2Fsync%3Fcode%3Dinvite-code%26state%3Dabc123"; got != want {
		t.Fatalf("start Location = %q, want %q", got, want)
	}

	loginStartResp, err := client.Get(ts.URL + resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("start browser login: %v", err)
	}
	defer func() { _ = loginStartResp.Body.Close() }()
	if loginStartResp.StatusCode != http.StatusFound {
		t.Fatalf("login start status = %d, want %d", loginStartResp.StatusCode, http.StatusFound)
	}
	loginURL, err := url.Parse(loginStartResp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse login redirect: %v", err)
	}
	if got, want := loginURL.Host, "alt-idp.example.test"; got != want {
		t.Fatalf("login redirect host = %q, want %q", got, want)
	}
	if got, want := loginURL.Query().Get("state"), "/sample-portal/sync"; got != want {
		t.Fatalf("login redirect state = %q, want %q", got, want)
	}

	callbackResp, err := client.Get(ts.URL + "/api/v1/auth/login/callback?code=good-code&state=" + url.QueryEscape(loginURL.Query().Get("state")))
	if err != nil {
		t.Fatalf("browser login callback: %v", err)
	}
	defer func() { _ = callbackResp.Body.Close() }()
	if callbackResp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want %d", callbackResp.StatusCode, http.StatusFound)
	}
	if got, want := callbackResp.Header.Get("Location"), protectedPath; got != want {
		t.Fatalf("callback redirect = %q, want %q", got, want)
	}

	resp, err = client.Get(ts.URL + protectedPath)
	if err != nil {
		t.Fatalf("GET protected mounted ui with route-auth session: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll protected mounted ui: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("protected mounted ui status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if !strings.Contains(string(body), "protected-sample-shell") {
		t.Fatalf("body = %q, want protected sample shell", body)
	}

	resp, err = client.Get(ts.URL + "/api/v1/integrations")
	if err != nil {
		t.Fatalf("GET integrations with route-auth session: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("integrations status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestMountedUIRoutes_HumanAuthorization_DefaultAllowTreatsAuthenticatedUsersAsViewerWithPluginName(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>protected-sample-shell</html>"), 0o644); err != nil {
		t.Fatalf("WriteFile index.html: %v", err)
	}
	handler, err := testutilUIHandler(dir)
	if err != nil {
		t.Fatalf("ui handler: %v", err)
	}

	svc := coretesting.NewStubServices(t)
	legacy := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {
				Default: "allow",
				Members: []config.HumanPolicyMemberDef{
					staticPolicyUserMember(t, svc, "admin@example.test", "admin"),
				},
			},
		},
	}, nil, map[string]*config.ProviderEntry{
		"sample_portal": {AuthorizationPolicy: "sample_policy"},
	}, nil)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				switch token {
				case "viewer-session":
					return &core.UserIdentity{Email: "viewer@example.test"}, nil
				case "admin-session":
					return &core.UserIdentity{Email: "admin@example.test"}, nil
				default:
					return nil, fmt.Errorf("invalid token")
				}
			},
		}
		cfg.Services = svc
		cfg.Authorizer = legacy
		cfg.MountedUIs = []server.MountedUI{{
			Name:                "sample_portal",
			Path:                "/sample-portal",
			PluginName:          "sample_portal",
			AuthorizationPolicy: "sample_policy",
			Routes: []server.MountedUIRoute{
				{Path: "/admin/*", AllowedRoles: []string{"admin"}},
				{Path: "/*", AllowedRoles: []string{"viewer", "admin"}},
			},
			Handler: handler,
		}}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/sample-portal/sync", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "viewer-session"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET default-allow mounted sync with viewer session: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll default-allow mounted sync: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("default-allow viewer status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if !strings.Contains(string(body), "protected-sample-shell") {
		t.Fatalf("body = %q, want protected sample shell", body)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/sample-portal/admin", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "viewer-session"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET default-allow mounted admin with viewer session: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("default-allow viewer admin status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/sample-portal/admin", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "admin-session"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET default-allow mounted admin with admin session: %v", err)
	}
	body, err = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll default-allow mounted admin: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("default-allow admin status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if !strings.Contains(string(body), "protected-sample-shell") {
		t.Fatalf("admin body = %q, want protected sample shell", body)
	}
}

func TestMountedUIRoutes_HumanAuthorization_DynamicGrant(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>protected-sample-shell</html>"), 0o644); err != nil {
		t.Fatalf("WriteFile index.html: %v", err)
	}
	handler, err := testutilUIHandler(dir)
	if err != nil {
		t.Fatalf("ui handler: %v", err)
	}

	svc := coretesting.NewStubServices(t)
	adminUser := seedUser(t, svc, "admin@example.test")
	provider := newMemoryAuthorizationProvider("memory-authorization")
	legacy, err := authorization.New(config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(adminUser.ID), Role: "admin"},
				},
			},
		},
	}, map[string]*config.ProviderEntry{
		"sample_portal": {AuthorizationPolicy: "sample_policy"},
	}, nil, nil)
	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}
	authz := mustProviderBackedAuthorizer(t, legacy, provider)
	seedProviderPluginAuthorization(t, svc, authz, provider, "sample_portal", "viewer@example.test", "viewer")

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				switch token {
				case "viewer-session":
					return &core.UserIdentity{Email: "viewer@example.test"}, nil
				case "admin-session":
					return &core.UserIdentity{Email: "admin@example.test"}, nil
				default:
					return nil, fmt.Errorf("invalid token")
				}
			},
		}
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.MountedUIs = []server.MountedUI{{
			Name:                "sample_portal",
			Path:                "/sample-portal",
			PluginName:          "sample_portal",
			AuthorizationPolicy: "sample_policy",
			Routes: []server.MountedUIRoute{
				{Path: "/admin/*", AllowedRoles: []string{"admin"}},
				{Path: "/*", AllowedRoles: []string{"viewer", "admin"}},
			},
			Handler: handler,
		}}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/sample-portal/sync", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "viewer-session"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET dynamic mounted sync with viewer session: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll dynamic mounted sync: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dynamic viewer status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(string(body), "protected-sample-shell") {
		t.Fatalf("body = %q, want protected sample shell", body)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/sample-portal/admin", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "viewer-session"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET dynamic mounted admin with viewer session: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("dynamic viewer admin status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/sample-portal/admin", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "admin-session"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET dynamic mounted admin with admin session: %v", err)
	}
	body, err = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll dynamic mounted admin: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("static-over-dynamic admin status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(string(body), "protected-sample-shell") {
		t.Fatalf("body = %q, want protected sample shell", body)
	}
}

func TestMountedUIRoutes_HumanAuthorization_DefaultAllowTreatsAuthenticatedUsersAsViewerWithoutPluginName(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>protected-sample-shell</html>"), 0o644); err != nil {
		t.Fatalf("WriteFile index.html: %v", err)
	}
	handler, err := testutilUIHandler(dir)
	if err != nil {
		t.Fatalf("ui handler: %v", err)
	}

	svc := coretesting.NewStubServices(t)
	legacy := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {
				Default: "allow",
				Members: []config.HumanPolicyMemberDef{
					staticPolicyUserMember(t, svc, "admin@example.test", "admin"),
				},
			},
		},
	}, nil, nil, nil)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				switch token {
				case "viewer-session":
					return &core.UserIdentity{Email: "viewer@example.test"}, nil
				case "admin-session":
					return &core.UserIdentity{Email: "admin@example.test"}, nil
				default:
					return nil, fmt.Errorf("invalid token")
				}
			},
		}
		cfg.Services = svc
		cfg.Authorizer = legacy
		cfg.MountedUIs = []server.MountedUI{{
			Name:                "sample_portal",
			Path:                "/sample-portal",
			AuthorizationPolicy: "sample_policy",
			Routes: []server.MountedUIRoute{
				{Path: "/admin/*", AllowedRoles: []string{"admin"}},
				{Path: "/*", AllowedRoles: []string{"viewer", "admin"}},
			},
			Handler: handler,
		}}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/sample-portal/sync", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "viewer-session"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET default-allow mounted sync without plugin name: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll default-allow mounted sync without plugin name: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("default-allow viewer status without plugin name = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if !strings.Contains(string(body), "protected-sample-shell") {
		t.Fatalf("body = %q, want protected sample shell", body)
	}
}

func TestBuiltInAdminRoute_HumanAuthorization(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>protected-admin-shell</html>"), 0o644); err != nil {
		t.Fatalf("WriteFile index.html: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "theme.css"), []byte("body{background:#eee;}"), 0o644); err != nil {
		t.Fatalf("WriteFile theme.css: %v", err)
	}
	handler, err := testutilUIHandler(dir)
	if err != nil {
		t.Fatalf("ui handler: %v", err)
	}

	svc := coretesting.NewStubServices(t)
	viewer := seedUser(t, svc, "viewer@example.test")
	admin := seedUser(t, svc, "admin@example.test")
	legacy := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"admin_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(viewer.ID), Role: "viewer"},
					{SubjectID: principal.UserSubjectID(admin.ID), Role: "admin"},
				},
			},
		},
	}, nil, nil, nil)
	provider := newMemoryAuthorizationProvider("memory-authorization")
	authz := mustProviderBackedAuthorizer(t, legacy, provider)
	seedProviderDynamicAdminMembership(t, svc, authz, provider, "dynamic-admin@example.test", "admin")

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				switch token {
				case "viewer-session":
					return &core.UserIdentity{Email: "viewer@example.test"}, nil
				case "admin-session":
					return &core.UserIdentity{Email: "admin@example.test"}, nil
				case "dynamic-admin-session":
					return &core.UserIdentity{Email: "dynamic-admin@example.test"}, nil
				default:
					return nil, fmt.Errorf("invalid token")
				}
			},
		}
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.AuthorizationProvider = provider
		cfg.Admin = server.AdminRouteConfig{
			AuthorizationPolicy: "admin_policy",
			AllowedRoles:        []string{"admin"},
		}
		cfg.AdminUI = handler
	})
	testutil.CloseOnCleanup(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	resp, err := noRedirect.Get(ts.URL + "/admin/?tab=members")
	if err != nil {
		t.Fatalf("GET protected admin without auth: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	if got, want := resp.Header.Get("Location"), "/api/v1/auth/login?next=%2Fadmin%2F%3Ftab%3Dmembers"; got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/admin/", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "viewer-session"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET protected admin with viewer session: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer admin status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/admin/", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "dynamic-admin-session"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET protected admin with dynamic admin session: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll protected admin dynamic admin: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dynamic admin status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(string(body), "protected-admin-shell") {
		t.Fatalf("dynamic admin body = %q, want protected admin shell", body)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/admin/", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "admin-session"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET protected admin with admin session: %v", err)
	}
	body, err = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll protected admin: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(string(body), "protected-admin-shell") {
		t.Fatalf("body = %q, want protected admin shell", body)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/admin/theme.css", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "admin-session"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET protected admin asset: %v", err)
	}
	body, err = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll protected admin asset: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin asset status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(string(body), "background") {
		t.Fatalf("admin asset body = %q, want stylesheet", body)
	}
}

func TestBuiltInAdminRoute_HumanAuthorizationOnManagementProfile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>protected-admin-shell</html>"), 0o644); err != nil {
		t.Fatalf("WriteFile index.html: %v", err)
	}
	handler, err := testutilUIHandler(dir)
	if err != nil {
		t.Fatalf("ui handler: %v", err)
	}

	svc := coretesting.NewStubServices(t)
	viewer := seedUser(t, svc, "viewer@example.test")
	admin := seedUser(t, svc, "admin@example.test")
	legacy := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"admin_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(viewer.ID), Role: "viewer"},
					{SubjectID: principal.UserSubjectID(admin.ID), Role: "admin"},
				},
			},
		},
	}, nil, nil, nil)
	provider := newMemoryAuthorizationProvider("memory-authorization")
	authz := mustProviderBackedAuthorizer(t, legacy, provider)
	seedProviderDynamicAdminMembership(t, svc, authz, provider, "dynamic-admin@example.test", "admin")

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				switch token {
				case "viewer-session":
					return &core.UserIdentity{Email: "viewer@example.test"}, nil
				case "admin-session":
					return &core.UserIdentity{Email: "admin@example.test"}, nil
				case "dynamic-admin-session":
					return &core.UserIdentity{Email: "dynamic-admin@example.test"}, nil
				default:
					return nil, fmt.Errorf("invalid token")
				}
			},
		}
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.AuthorizationProvider = provider
		cfg.RouteProfile = server.RouteProfileManagement
		cfg.PublicBaseURL = "https://gestalt.example.test"
		cfg.ManagementBaseURL = "https://gestalt.example.test:9090"
		cfg.Admin = server.AdminRouteConfig{
			AuthorizationPolicy: "admin_policy",
			AllowedRoles:        []string{"admin"},
		}
		cfg.AdminUI = handler
	})
	testutil.CloseOnCleanup(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	resp, err := noRedirect.Get(ts.URL + "/admin/?tab=members")
	if err != nil {
		t.Fatalf("GET protected management admin without auth: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	if got, want := resp.Header.Get("Location"), "https://gestalt.example.test/api/v1/auth/login?next=https%3A%2F%2Fgestalt.example.test%3A9090%2Fadmin%2F%3Ftab%3Dmembers"; got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/admin/", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "viewer-session"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET protected management admin with viewer session: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer management admin status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/admin/", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "dynamic-admin-session"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET protected management admin with dynamic admin session: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll protected management admin dynamic admin: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("management dynamic admin status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(string(body), "protected-admin-shell") {
		t.Fatalf("body = %q, want protected admin shell", body)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/admin/", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "admin-session"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET protected management admin with admin session: %v", err)
	}
	body, err = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll protected management admin: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("management admin status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(string(body), "protected-admin-shell") {
		t.Fatalf("body = %q, want protected admin shell", body)
	}
}

func TestBuiltInAdminRoute_HumanAuthorizationSplitManagementLoginFlow(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>protected-admin-shell</html>"), 0o644); err != nil {
		t.Fatalf("WriteFile index.html: %v", err)
	}
	handler, err := testutilUIHandler(dir)
	if err != nil {
		t.Fatalf("ui handler: %v", err)
	}

	secret := []byte("0123456789abcdef0123456789abcdef")
	auth := &stubHostIssuedSessionAuth{secret: secret}
	svc := coretesting.NewStubServices(t)
	legacy := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"admin_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					staticPolicyUserMember(t, svc, "host@example.com", "admin"),
				},
			},
		},
	}, nil, nil, nil)

	publicListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen public: %v", err)
	}
	managementListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = publicListener.Close()
		t.Fatalf("listen management: %v", err)
	}
	publicPort := publicListener.Addr().(*net.TCPAddr).Port
	managementPort := managementListener.Addr().(*net.TCPAddr).Port
	publicURL := fmt.Sprintf("https://gestalt.example.test:%d", publicPort)
	managementURL := fmt.Sprintf("https://gestalt.example.test:%d", managementPort)

	publicTS := httptest.NewUnstartedServer(newTestHandler(t, func(cfg *server.Config) {
		cfg.Auth = auth
		cfg.StateSecret = secret
		cfg.Services = svc
		cfg.Authorizer = legacy
		cfg.PublicBaseURL = publicURL
		cfg.ManagementBaseURL = managementURL
		cfg.RouteProfile = server.RouteProfilePublic
		cfg.Admin = server.AdminRouteConfig{
			AuthorizationPolicy: "admin_policy",
		}
	}))
	publicTS.Listener = publicListener
	publicTS.StartTLS()
	testutil.CloseOnCleanup(t, publicTS)

	managementTS := httptest.NewUnstartedServer(newTestHandler(t, func(cfg *server.Config) {
		cfg.Auth = auth
		cfg.StateSecret = secret
		cfg.Services = svc
		cfg.Authorizer = legacy
		cfg.PublicBaseURL = publicURL
		cfg.ManagementBaseURL = managementURL
		cfg.RouteProfile = server.RouteProfileManagement
		cfg.Admin = server.AdminRouteConfig{
			AuthorizationPolicy: "admin_policy",
		}
		cfg.AdminUI = handler
	}))
	managementTS.Listener = managementListener
	managementTS.StartTLS()
	testutil.CloseOnCleanup(t, managementTS)

	client := newVirtualHostClient(t, map[string]string{
		fmt.Sprintf("gestalt.example.test:%d", publicPort):     publicTS.Listener.Addr().String(),
		fmt.Sprintf("gestalt.example.test:%d", managementPort): managementTS.Listener.Addr().String(),
	})

	adminResp, err := client.Get(managementURL + "/admin/?tab=members")
	if err != nil {
		t.Fatalf("GET protected management admin without auth: %v", err)
	}
	defer func() { _ = adminResp.Body.Close() }()
	if adminResp.StatusCode != http.StatusFound {
		t.Fatalf("initial admin status = %d, want %d", adminResp.StatusCode, http.StatusFound)
	}

	loginStartURL := adminResp.Header.Get("Location")
	if got, want := loginStartURL, publicURL+"/api/v1/auth/login?next="+url.QueryEscape(managementURL+"/admin/?tab=members"); got != want {
		t.Fatalf("initial admin redirect = %q, want %q", got, want)
	}

	loginStartResp, err := client.Get(loginStartURL)
	if err != nil {
		t.Fatalf("GET browser login start: %v", err)
	}
	defer func() { _ = loginStartResp.Body.Close() }()
	if loginStartResp.StatusCode != http.StatusFound {
		t.Fatalf("login start status = %d, want %d", loginStartResp.StatusCode, http.StatusFound)
	}
	loginURL, err := url.Parse(loginStartResp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse start login redirect: %v", err)
	}

	callbackResp, err := client.Get(publicURL + "/api/v1/auth/login/callback?code=good-code&state=" + url.QueryEscape(loginURL.Query().Get("state")))
	if err != nil {
		t.Fatalf("GET browser login callback: %v", err)
	}
	defer func() { _ = callbackResp.Body.Close() }()
	if callbackResp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want %d", callbackResp.StatusCode, http.StatusFound)
	}
	if got, want := callbackResp.Header.Get("Location"), managementURL+"/admin/?tab=members"; got != want {
		t.Fatalf("callback redirect = %q, want %q", got, want)
	}

	finalResp, err := client.Get(callbackResp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("GET management admin after login: %v", err)
	}
	body, err := io.ReadAll(finalResp.Body)
	_ = finalResp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll protected management admin after login: %v", err)
	}
	if finalResp.StatusCode != http.StatusOK {
		t.Fatalf("final admin status = %d, want 200", finalResp.StatusCode)
	}
	if !strings.Contains(string(body), "protected-admin-shell") {
		t.Fatalf("body = %q, want protected admin shell", body)
	}
}

func TestBuiltInAdminRoute_EmbeddedAdminUIIncludesAuthorizationWorkspace(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.AdminUI = adminui.EmbeddedHandler(adminui.Options{BrandHref: "/"})
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/admin/?tab=members")
	if err != nil {
		t.Fatalf("GET embedded admin ui: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll embedded admin ui: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("embedded admin ui status = %d, want 200", resp.StatusCode)
	}

	text := string(body)
	for _, want := range []string{
		"Control surface",
		"Authorization rules",
		`data-tab="authorization"`,
		`data-tab-panel="authorization"`,
		`data-tab="admins"`,
		`data-tab-panel="admins"`,
		"/admin/api/v1/authorization/plugins",
		"/admin/api/v1/authorization/admins/members",
		`window.__gestaltAdminShell.loginBase = "/api/v1/auth/login"`,
		"Save dynamic grant",
		"Save admin grant",
		"Built-in admin members",
		"window.history.replaceState",
		"Prometheus telemetry",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("embedded admin ui body missing %q", want)
		}
	}
}

func TestAdminAPI_HumanAuthorization(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	viewer := seedUser(t, svc, "viewer@example.test")
	admin := seedUser(t, svc, "admin@example.test")
	legacy := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"admin_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(viewer.ID), Role: "viewer"},
					{SubjectID: principal.UserSubjectID(admin.ID), Role: "admin"},
				},
			},
			"sample_policy": {Default: "deny"},
		},
	}, nil, map[string]*config.ProviderEntry{
		"sample_plugin": {AuthorizationPolicy: "sample_policy"},
	}, nil)
	provider := newMemoryAuthorizationProvider("memory-authorization")
	authz := mustProviderBackedAuthorizer(t, legacy, provider)
	seedProviderDynamicAdminMembership(t, svc, authz, provider, "dynamic-admin@example.test", "admin")

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				switch token {
				case "viewer-session":
					return &core.UserIdentity{Email: "viewer@example.test"}, nil
				case "admin-session":
					return &core.UserIdentity{Email: "admin@example.test"}, nil
				case "dynamic-admin-session":
					return &core.UserIdentity{Email: "dynamic-admin@example.test"}, nil
				default:
					return nil, fmt.Errorf("invalid token")
				}
			},
		}
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.AuthorizationProvider = provider
		cfg.PluginDefs = map[string]*config.ProviderEntry{
			"sample_plugin": {AuthorizationPolicy: "sample_policy"},
		}
		cfg.Admin = server.AdminRouteConfig{
			AuthorizationPolicy: "admin_policy",
			AllowedRoles:        []string{"admin"},
		}
		cfg.AdminUI = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("admin"))
		})
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/admin/api/v1/authorization/plugins", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET admin api without auth: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated admin api status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/admin/api/v1/authorization/plugins", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "viewer-session"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET admin api with viewer session: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer admin api status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/admin/api/v1/authorization/admins/members", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "dynamic-admin-session"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET admin members api with dynamic admin session: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("dynamic admin api status = %d, want 200: %s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("X-Gestalt-Can-Write"); got != "true" {
		t.Fatalf("dynamic admin can-write header = %q, want true", got)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/admin/api/v1/authorization/plugins", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "admin-session"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET admin api with admin session: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("admin api status = %d, want 200: %s", resp.StatusCode, body)
	}

	var plugins []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&plugins); err != nil {
		t.Fatalf("decoding plugins: %v", err)
	}
	if len(plugins) != 1 || plugins[0]["name"] != "sample_plugin" {
		t.Fatalf("plugins = %+v, want sample_plugin", plugins)
	}
}

func TestAdminAPI_RoutesMountedWithoutAdminUI(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.PluginDefs = map[string]*config.ProviderEntry{
			"sample_plugin": {AuthorizationPolicy: "sample_policy"},
		}
		cfg.AdminUI = nil
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/admin/api/v1/authorization/plugins")
	if err != nil {
		t.Fatalf("GET admin api without admin ui: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("admin api without admin ui status = %d, want 200: %s", resp.StatusCode, body)
	}

	var plugins []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&plugins); err != nil {
		t.Fatalf("decoding plugins: %v", err)
	}
	if len(plugins) != 1 || plugins[0]["name"] != "sample_plugin" {
		t.Fatalf("plugins = %+v, want sample_plugin", plugins)
	}
}

func TestAdminAPI_RuntimeProviders(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.PluginRuntimes = &staticRuntimeInspector{
			snapshots: []bootstrap.RuntimeProviderSnapshot{
				{
					Name:    "local",
					Driver:  config.RuntimeProviderDriverLocal,
					Default: true,
				},
				{
					Name:          "modal",
					Driver:        config.RuntimeProviderDriver("modal"),
					Loaded:        true,
					SupportLoaded: true,
					Advertised: bootstrap.RuntimeBehavior{
						CanHostPlugins:    true,
						HostServiceAccess: bootstrap.RuntimeHostServiceAccessNone,
						EgressMode:        bootstrap.RuntimeEgressModeCIDR,
						LaunchMode:        bootstrap.RuntimeLaunchModeBundle,
						ExecutionTarget: bootstrap.RuntimeExecutionTarget{
							GOOS:   "linux",
							GOARCH: "amd64",
						},
					},
					Effective: bootstrap.RuntimeBehavior{
						CanHostPlugins:    true,
						HostServiceAccess: bootstrap.RuntimeHostServiceAccessRelay,
						EgressMode:        bootstrap.RuntimeEgressModeCIDR,
						LaunchMode:        bootstrap.RuntimeLaunchModeBundle,
						ExecutionTarget: bootstrap.RuntimeExecutionTarget{
							GOOS:   "linux",
							GOARCH: "amd64",
						},
					},
					Sessions: []pluginruntime.Session{{
						ID:    "session-1",
						State: pluginruntime.SessionStateRunning,
						Metadata: map[string]string{
							"plugin": "support",
							"owner":  "support-platform",
						},
					}},
				},
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/admin/api/v1/runtime/providers")
	if err != nil {
		t.Fatalf("GET runtime providers: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("runtime providers status = %d, want 200: %s", resp.StatusCode, body)
	}

	var providers []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&providers); err != nil {
		t.Fatalf("decoding runtime providers: %v", err)
	}
	if len(providers) != 2 {
		t.Fatalf("runtime providers len = %d, want 2", len(providers))
	}
	if got := providers[0]["name"]; got != "local" {
		t.Fatalf("runtime providers[0].name = %v, want local", got)
	}
	if got := providers[0]["loaded"]; got != false {
		t.Fatalf("runtime providers[0].loaded = %v, want false", got)
	}
	if got := providers[0]["default"]; got != true {
		t.Fatalf("runtime providers[0].default = %v, want true", got)
	}
	if _, ok := providers[0]["sessionCount"]; ok {
		t.Fatalf("runtime providers[0].sessionCount unexpectedly present: %#v", providers[0]["sessionCount"])
	}
	if got := providers[1]["name"]; got != "modal" {
		t.Fatalf("runtime providers[1].name = %v, want modal", got)
	}
	if got := providers[1]["loaded"]; got != true {
		t.Fatalf("runtime providers[1].loaded = %v, want true", got)
	}
	if got := providers[1]["sessionCount"]; got != float64(1) {
		t.Fatalf("runtime providers[1].sessionCount = %v, want 1", got)
	}
	profile, ok := providers[1]["profile"].(map[string]any)
	if !ok {
		t.Fatalf("runtime providers[1].profile = %#v, want object", providers[1]["profile"])
	}
	advertised, ok := profile["advertised"].(map[string]any)
	if !ok {
		t.Fatalf("runtime providers[1].profile.advertised = %#v, want object", profile["advertised"])
	}
	effective, ok := profile["effective"].(map[string]any)
	if !ok {
		t.Fatalf("runtime providers[1].profile.effective = %#v, want object", profile["effective"])
	}
	if advertised["canHostPlugins"] != true || advertised["hostServiceAccess"] != "none" || advertised["egressMode"] != "cidr" || advertised["launchMode"] != "bundle" {
		t.Fatalf("runtime providers[1].profile.advertised = %#v", advertised)
	}
	if effective["canHostPlugins"] != true || effective["hostServiceAccess"] != "relay" || effective["egressMode"] != "cidr" || effective["launchMode"] != "bundle" {
		t.Fatalf("runtime providers[1].profile.effective = %#v", effective)
	}
	executionTarget, ok := effective["executionTarget"].(map[string]any)
	if !ok || executionTarget["goos"] != "linux" || executionTarget["goarch"] != "amd64" {
		t.Fatalf("runtime providers[1].profile.effective.executionTarget = %#v", effective["executionTarget"])
	}
}

func TestAdminAPI_RuntimeProviderSessions(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.PluginRuntimes = &staticRuntimeInspector{
			snapshots: []bootstrap.RuntimeProviderSnapshot{{
				Name:   "modal",
				Driver: config.RuntimeProviderDriver("modal"),
				Loaded: true,
				Sessions: []pluginruntime.Session{{
					ID:    "session-1",
					State: pluginruntime.SessionStateRunning,
					Metadata: map[string]string{
						"plugin": "support",
						"owner":  "support-platform",
					},
				}},
			}},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/admin/api/v1/runtime/providers/modal/sessions")
	if err != nil {
		t.Fatalf("GET runtime provider sessions: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("runtime provider sessions status = %d, want 200: %s", resp.StatusCode, body)
	}

	var sessions []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		t.Fatalf("decoding runtime provider sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("runtime provider sessions len = %d, want 1", len(sessions))
	}
	if got := sessions[0]["id"]; got != "session-1" {
		t.Fatalf("runtime provider sessions[0].id = %v, want session-1", got)
	}
	if got := sessions[0]["state"]; got != string(pluginruntime.SessionStateRunning) {
		t.Fatalf("runtime provider sessions[0].state = %v, want %q", got, pluginruntime.SessionStateRunning)
	}
	if got := sessions[0]["plugin"]; got != "support" {
		t.Fatalf("runtime provider sessions[0].plugin = %v, want support", got)
	}
	if _, ok := sessions[0]["metadata"]; ok {
		t.Fatalf("runtime provider sessions[0].metadata unexpectedly present: %#v", sessions[0]["metadata"])
	}
}

func TestAdminAPI_RuntimeProviderInspectionError(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.PluginRuntimes = &staticRuntimeInspector{
			snapshots: []bootstrap.RuntimeProviderSnapshot{{
				Name:   "modal",
				Driver: config.RuntimeProviderDriver("modal"),
				Loaded: true,
				Error:  "support: boom",
			}},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	providersResp, err := http.Get(ts.URL + "/admin/api/v1/runtime/providers")
	if err != nil {
		t.Fatalf("GET runtime providers with inspection error: %v", err)
	}
	defer func() { _ = providersResp.Body.Close() }()
	if providersResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(providersResp.Body)
		t.Fatalf("runtime providers status = %d, want 200: %s", providersResp.StatusCode, body)
	}

	var providers []map[string]any
	if err := json.NewDecoder(providersResp.Body).Decode(&providers); err != nil {
		t.Fatalf("decoding runtime providers: %v", err)
	}
	if len(providers) != 1 {
		t.Fatalf("runtime providers len = %d, want 1", len(providers))
	}
	if got := providers[0]["error"]; got != "support: boom" {
		t.Fatalf("runtime providers[0].error = %v, want support: boom", got)
	}
	if _, ok := providers[0]["profile"]; ok {
		t.Fatalf("runtime providers[0].profile unexpectedly present: %#v", providers[0]["profile"])
	}
	if _, ok := providers[0]["capabilities"]; ok {
		t.Fatalf("runtime providers[0].capabilities unexpectedly present: %#v", providers[0]["capabilities"])
	}
	if _, ok := providers[0]["sessionCount"]; ok {
		t.Fatalf("runtime providers[0].sessionCount unexpectedly present: %#v", providers[0]["sessionCount"])
	}

	sessionsResp, err := http.Get(ts.URL + "/admin/api/v1/runtime/providers/modal/sessions")
	if err != nil {
		t.Fatalf("GET runtime provider sessions with inspection error: %v", err)
	}
	defer func() { _ = sessionsResp.Body.Close() }()
	if sessionsResp.StatusCode != http.StatusServiceUnavailable {
		body, _ := io.ReadAll(sessionsResp.Body)
		t.Fatalf("runtime provider sessions status = %d, want 503: %s", sessionsResp.StatusCode, body)
	}
}

func TestAdminAPI_RuntimeProviderSessionInspectionErrorKeepsProfile(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.PluginRuntimes = &staticRuntimeInspector{
			snapshots: []bootstrap.RuntimeProviderSnapshot{{
				Name:          "modal",
				Driver:        config.RuntimeProviderDriver("modal"),
				Loaded:        true,
				SupportLoaded: true,
				Advertised: bootstrap.RuntimeBehavior{
					CanHostPlugins:    true,
					HostServiceAccess: bootstrap.RuntimeHostServiceAccessNone,
					EgressMode:        bootstrap.RuntimeEgressModeCIDR,
					LaunchMode:        bootstrap.RuntimeLaunchModeBundle,
				},
				Effective: bootstrap.RuntimeBehavior{
					CanHostPlugins:    true,
					HostServiceAccess: bootstrap.RuntimeHostServiceAccessRelay,
					EgressMode:        bootstrap.RuntimeEgressModeCIDR,
					LaunchMode:        bootstrap.RuntimeLaunchModeBundle,
				},
				Error: "list sessions: boom",
			}},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/admin/api/v1/runtime/providers")
	if err != nil {
		t.Fatalf("GET runtime providers with session inspection error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("runtime providers status = %d, want 200: %s", resp.StatusCode, body)
	}

	var providers []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&providers); err != nil {
		t.Fatalf("decoding runtime providers: %v", err)
	}
	if len(providers) != 1 {
		t.Fatalf("runtime providers len = %d, want 1", len(providers))
	}
	if got := providers[0]["error"]; got != "list sessions: boom" {
		t.Fatalf("runtime providers[0].error = %v, want list sessions: boom", got)
	}
	if _, ok := providers[0]["sessionCount"]; ok {
		t.Fatalf("runtime providers[0].sessionCount unexpectedly present: %#v", providers[0]["sessionCount"])
	}
	profile, ok := providers[0]["profile"].(map[string]any)
	if !ok {
		t.Fatalf("runtime providers[0].profile = %#v, want object", providers[0]["profile"])
	}
	effective, ok := profile["effective"].(map[string]any)
	if !ok {
		t.Fatalf("runtime providers[0].profile.effective = %#v, want object", profile["effective"])
	}
	if effective["hostServiceAccess"] != "relay" {
		t.Fatalf("runtime providers[0].profile.effective = %#v, want relay host-service access", effective)
	}
}

func TestAdminAPI_HumanAuthorizationOnManagementProfile(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	admin := seedUser(t, svc, "admin@example.test")
	legacy := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"admin_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(admin.ID), Role: "admin"},
				},
			},
			"sample_policy": {Default: "deny"},
		},
	}, nil, map[string]*config.ProviderEntry{
		"sample_plugin": {AuthorizationPolicy: "sample_policy"},
	}, nil)
	provider := newMemoryAuthorizationProvider("memory-authorization")
	authz := mustProviderBackedAuthorizer(t, legacy, provider)
	seedProviderDynamicAdminMembership(t, svc, authz, provider, "dynamic-admin@example.test", "admin")

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				switch token {
				case "admin-session":
					return &core.UserIdentity{Email: "admin@example.test"}, nil
				case "dynamic-admin-session":
					return &core.UserIdentity{Email: "dynamic-admin@example.test"}, nil
				default:
					return nil, fmt.Errorf("invalid token")
				}
			},
		}
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.AuthorizationProvider = provider
		cfg.PluginDefs = map[string]*config.ProviderEntry{
			"sample_plugin": {AuthorizationPolicy: "sample_policy"},
		}
		cfg.RouteProfile = server.RouteProfileManagement
		cfg.PublicBaseURL = "https://gestalt.example.test"
		cfg.ManagementBaseURL = "https://gestalt.example.test:9090"
		cfg.Admin = server.AdminRouteConfig{
			AuthorizationPolicy: "admin_policy",
			AllowedRoles:        []string{"admin"},
		}
		cfg.AdminUI = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("admin"))
		})
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/admin/api/v1/authorization/admins/members", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "dynamic-admin-session"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET management admin members api with dynamic admin session: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("management dynamic admin api status = %d, want 200: %s", resp.StatusCode, body)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/admin/api/v1/authorization/plugins", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "admin-session"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET management admin api with admin session: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("management admin api status = %d, want 200: %s", resp.StatusCode, body)
	}
}

func TestAdminAPI_HumanAuthorization_UserResolutionFailure(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	admin := seedUser(t, svc, "admin@example.test")
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"admin_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(admin.ID), Role: "admin"},
				},
			},
			"sample_policy": {Default: "deny"},
		},
	}, nil, map[string]*config.ProviderEntry{
		"sample_plugin": {AuthorizationPolicy: "sample_policy"},
	}, nil)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "admin-session" {
					return nil, fmt.Errorf("invalid token")
				}
				return &core.UserIdentity{Email: "admin@example.test"}, nil
			},
		}
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.PluginDefs = map[string]*config.ProviderEntry{
			"sample_plugin": {AuthorizationPolicy: "sample_policy"},
		}
		cfg.Admin = server.AdminRouteConfig{
			AuthorizationPolicy: "admin_policy",
			AllowedRoles:        []string{"admin"},
		}
		cfg.AdminUI = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("admin"))
		})
	})
	testutil.CloseOnCleanup(t, ts)

	stubDB := svc.DB.(*coretesting.StubIndexedDB)
	stubDB.Err = fmt.Errorf("database unavailable")
	defer func() { stubDB.Err = nil }()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/admin/api/v1/authorization/plugins", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "admin-session"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET admin api with failed user resolution: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("admin api user-resolution failure status = %d, want 500: %s", resp.StatusCode, body)
	}
}

func TestAdminAPIRoutes_HiddenOnPublicProfile(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.RouteProfile = server.RouteProfilePublic
		cfg.PluginDefs = map[string]*config.ProviderEntry{
			"sample_plugin": {AuthorizationPolicy: "sample_policy"},
		}
		cfg.AdminUI = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("admin"))
		})
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/admin/api/v1/authorization/plugins")
	if err != nil {
		t.Fatalf("GET public admin api: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("public admin api status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestAdminAPI_PluginAuthorizationCRUD(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	legacy, err := authorization.New(config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					staticPolicyUserMember(t, svc, "static@example.test", "admin"),
				},
			},
		},
	}, map[string]*config.ProviderEntry{
		"sample_plugin": {AuthorizationPolicy: "sample_policy", MountPath: "/sample"},
	}, nil, nil)
	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}
	provider := newMemoryAuthorizationProvider("memory-authorization")
	authz := mustProviderBackedAuthorizer(t, legacy, provider)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.AuthorizationProvider = provider
		cfg.PluginDefs = map[string]*config.ProviderEntry{
			"sample_plugin": {AuthorizationPolicy: "sample_policy", MountPath: "/sample"},
		}
		cfg.AdminUI = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("admin"))
		})
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/admin/api/v1/authorization/plugins")
	if err != nil {
		t.Fatalf("GET plugins: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("plugins status = %d, want 200", resp.StatusCode)
	}

	dynamicEmail := "dynamic@example.test"
	body := bytes.NewBufferString(fmt.Sprintf(`{"email":%q,"role":"viewer"}`, dynamicEmail))
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/admin/api/v1/authorization/plugins/sample_plugin/members", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT dynamic member: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("put dynamic member status = %d, want 200: %s", resp.StatusCode, respBody)
	}
	var putPluginMembershipResp struct {
		Membership struct {
			SelectorKind  string `json:"selectorKind"`
			SelectorValue string `json:"selectorValue"`
			Email         string `json:"email"`
			Role          string `json:"role"`
		} `json:"membership"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&putPluginMembershipResp); err != nil {
		t.Fatalf("decode plugin membership response: %v", err)
	}
	dynamicUser, err := svc.Users.FindUserByEmail(context.Background(), dynamicEmail)
	if err != nil {
		t.Fatalf("FindUserByEmail dynamic plugin member: %v", err)
	}
	if putPluginMembershipResp.Membership.SelectorKind != "subject_id" {
		t.Fatalf("plugin membership selectorKind = %q, want subject_id", putPluginMembershipResp.Membership.SelectorKind)
	}
	if putPluginMembershipResp.Membership.SelectorValue != principal.UserSubjectID(dynamicUser.ID) {
		t.Fatalf("plugin membership selectorValue = %q, want canonical subject id", putPluginMembershipResp.Membership.SelectorValue)
	}
	if putPluginMembershipResp.Membership.Email != dynamicEmail {
		t.Fatalf("plugin membership email = %q, want %q", putPluginMembershipResp.Membership.Email, dynamicEmail)
	}

	resp, err = http.Get(ts.URL + "/admin/api/v1/authorization/plugins/sample_plugin/members")
	if err != nil {
		t.Fatalf("GET members: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("members status = %d, want 200", resp.StatusCode)
	}

	var members []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&members); err != nil {
		t.Fatalf("decoding members: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("expected 2 merged members, got %d (%+v)", len(members), members)
	}
	foundDynamicPluginMember := false
	for _, member := range members {
		if member["source"] != "dynamic" {
			continue
		}
		foundDynamicPluginMember = true
		if got := member["selectorKind"]; got != "subject_id" {
			t.Fatalf("dynamic plugin member selectorKind = %v, want subject_id", got)
		}
		if member["selectorValue"] != principal.UserSubjectID(dynamicUser.ID) {
			t.Fatalf("dynamic plugin member selector metadata = %+v, want canonical subject_id selectorValue", member)
		}
		if member["email"] != dynamicEmail {
			t.Fatalf("dynamic plugin member email = %v, want %q", member["email"], dynamicEmail)
		}
	}
	if !foundDynamicPluginMember {
		t.Fatalf("expected one dynamic plugin member, got %+v", members)
	}

	body = bytes.NewBufferString(`{"email":"static@example.test","role":"viewer"}`)
	req, _ = http.NewRequest(http.MethodPut, ts.URL+"/admin/api/v1/authorization/plugins/sample_plugin/members", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT static-conflict member: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusConflict {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("put static-conflict status = %d, want 409: %s", resp.StatusCode, respBody)
	}

	req, _ = http.NewRequest(http.MethodDelete, ts.URL+"/admin/api/v1/authorization/plugins/sample_plugin/members/"+url.PathEscape(principal.UserSubjectID(dynamicUser.ID)), nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE dynamic member: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("delete dynamic member status = %d, want 200: %s", resp.StatusCode, respBody)
	}

	resp, err = http.Get(ts.URL + "/admin/api/v1/authorization/plugins/sample_plugin/members")
	if err != nil {
		t.Fatalf("GET members after delete: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("members after delete status = %d, want 200", resp.StatusCode)
	}
	members = nil
	if err := json.NewDecoder(resp.Body).Decode(&members); err != nil {
		t.Fatalf("decoding members after delete: %v", err)
	}
	if len(members) != 1 || members[0]["source"] != "static" {
		t.Fatalf("members after delete = %+v, want only static row", members)
	}
}

func TestAdminAPI_PluginAuthorizationProviderBackedReadsAndDebug(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	provider := newMemoryAuthorizationProvider("memory-authorization")
	legacy, err := authorization.New(config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					staticPolicyUserMember(t, svc, "static@example.test", "admin"),
				},
			},
		},
	}, map[string]*config.ProviderEntry{
		"sample_plugin": {AuthorizationPolicy: "sample_policy", MountPath: "/sample"},
	}, nil, nil)
	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}
	authz := mustProviderBackedAuthorizer(t, legacy, provider)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "admin-session" {
					return nil, fmt.Errorf("invalid token")
				}
				return &core.UserIdentity{Email: "static@example.test"}, nil
			},
		}
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.AuthorizationProvider = provider
		cfg.PluginDefs = map[string]*config.ProviderEntry{
			"sample_plugin": {AuthorizationPolicy: "sample_policy", MountPath: "/sample"},
		}
		cfg.Admin = server.AdminRouteConfig{
			AuthorizationPolicy: "sample_policy",
			AllowedRoles:        []string{"admin"},
		}
		cfg.AdminUI = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("admin"))
		})
	})
	testutil.CloseOnCleanup(t, ts)

	dynamicUser := seedUser(t, svc, "dynamic@example.test")
	body := bytes.NewBufferString(fmt.Sprintf(`{"subjectId":%q,"role":"viewer"}`, principal.UserSubjectID(dynamicUser.ID)))
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/admin/api/v1/authorization/plugins/sample_plugin/members", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "admin-session"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT dynamic member: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("put dynamic member status = %d, want 200: %s", resp.StatusCode, respBody)
	}

	canonicalIdentityID, err := svc.Users.CanonicalIdentityIDForUser(context.Background(), dynamicUser.ID)
	if err != nil {
		t.Fatalf("CanonicalIdentityIDForUser(dynamic): %v", err)
	}
	pluginAccess, err := svc.IdentityPluginAccess.GetAccess(context.Background(), canonicalIdentityID, "sample_plugin")
	if err != nil {
		t.Fatalf("GetAccess(sample_plugin): %v", err)
	}
	if !pluginAccess.InvokeAllOperations {
		t.Fatal("expected provider-backed plugin write to sync canonical invoke-all access")
	}
	if err := authz.ReloadAuthorizationState(context.Background()); err != nil {
		t.Fatalf("ReloadAuthorizationState after provider-backed plugin write: %v", err)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/admin/api/v1/authorization/plugins/sample_plugin/members", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "admin-session"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET members: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("members status = %d, want 200", resp.StatusCode)
	}

	var members []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&members); err != nil {
		t.Fatalf("decoding members: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("expected provider-backed merged members, got %d (%+v)", len(members), members)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/admin/api/v1/authorization/provider", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "admin-session"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET provider summary: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("provider summary status = %d, want 200: %s", resp.StatusCode, respBody)
	}
	var providerSummary struct {
		Name          string `json:"name"`
		ActiveModelID string `json:"activeModelId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&providerSummary); err != nil {
		t.Fatalf("decoding provider summary: %v", err)
	}
	if providerSummary.Name != "memory-authorization" {
		t.Fatalf("provider name = %q, want %q", providerSummary.Name, "memory-authorization")
	}
	if providerSummary.ActiveModelID == "" {
		t.Fatal("expected active model id")
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/admin/api/v1/authorization/models", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "admin-session"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET models: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("models status = %d, want 200: %s", resp.StatusCode, respBody)
	}
	var modelsResp struct {
		Models []struct {
			ID string `json:"id"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&modelsResp); err != nil {
		t.Fatalf("decoding models response: %v", err)
	}
	if len(modelsResp.Models) == 0 {
		t.Fatal("expected at least one authorization model")
	}
	foundActiveModel := false
	for _, model := range modelsResp.Models {
		if model.ID == providerSummary.ActiveModelID {
			foundActiveModel = true
			break
		}
	}
	if !foundActiveModel {
		t.Fatalf("models response = %+v, want active model %q to be listed", modelsResp.Models, providerSummary.ActiveModelID)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/admin/api/v1/authorization/relationships?resourceType=plugin_dynamic&resourceId=sample_plugin", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "admin-session"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET relationships: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("relationships status = %d, want 200: %s", resp.StatusCode, respBody)
	}
	var relationshipsResp struct {
		ModelID       string `json:"modelId"`
		Relationships []struct {
			Managed bool `json:"managed"`
		} `json:"relationships"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&relationshipsResp); err != nil {
		t.Fatalf("decoding relationships response: %v", err)
	}
	if relationshipsResp.ModelID == "" {
		t.Fatal("expected model id on relationships response")
	}
	if len(relationshipsResp.Relationships) != 1 {
		t.Fatalf("expected 1 provider relationship, got %d", len(relationshipsResp.Relationships))
	}
	for _, rel := range relationshipsResp.Relationships {
		if !rel.Managed {
			t.Fatalf("expected managed relationship rows, got %+v", relationshipsResp.Relationships)
		}
	}
}

func TestAdminAPI_AuthorizationProviderDebugRequiresAdminPolicy(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	provider := newMemoryAuthorizationProvider("memory-authorization")
	legacy, err := authorization.New(config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					staticPolicyUserMember(t, svc, "static@example.test", "admin"),
				},
			},
		},
	}, map[string]*config.ProviderEntry{
		"sample_plugin": {AuthorizationPolicy: "sample_policy", MountPath: "/sample"},
	}, nil, nil)
	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}
	authz := mustProviderBackedAuthorizer(t, legacy, provider)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.AuthorizationProvider = provider
		cfg.PluginDefs = map[string]*config.ProviderEntry{
			"sample_plugin": {AuthorizationPolicy: "sample_policy", MountPath: "/sample"},
		}
		cfg.AdminUI = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("admin"))
		})
	})
	testutil.CloseOnCleanup(t, ts)

	paths := []string{
		"/admin/api/v1/authorization/provider",
		"/admin/api/v1/authorization/models",
		"/admin/api/v1/authorization/relationships",
	}
	for _, path := range paths {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		if resp.StatusCode != http.StatusServiceUnavailable {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			t.Fatalf("%s status = %d, want 503: %s", path, resp.StatusCode, body)
		}
		_ = resp.Body.Close()
	}
}

func TestAdminAPI_AdminAuthorizationCRUD(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	ctx := context.Background()
	seedUser(t, svc, "static-admin@example.test")
	const adminRole = "owner"
	legacy := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"admin_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					staticPolicyUserMember(t, svc, "static-admin@example.test", adminRole),
				},
			},
		},
	}, nil, nil, nil)
	provider := newMemoryAuthorizationProvider("memory-authorization")
	authz := mustProviderBackedAuthorizer(t, legacy, provider)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "admin-session" {
					return nil, fmt.Errorf("invalid token")
				}
				return &core.UserIdentity{Email: "static-admin@example.test"}, nil
			},
		}
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.AuthorizationProvider = provider
		cfg.Admin = server.AdminRouteConfig{
			AuthorizationPolicy: "admin_policy",
			AllowedRoles:        []string{adminRole, "operator"},
		}
		cfg.AdminUI = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("admin"))
		})
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/admin/api/v1/authorization/admins/members", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "admin-session"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET admin members: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("admin members status = %d, want 200: %s", resp.StatusCode, body)
	}

	var members []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&members); err != nil {
		t.Fatalf("decoding admin members: %v", err)
	}
	if len(members) != 1 || members[0]["source"] != "static" {
		t.Fatalf("admin members = %+v, want one static row", members)
	}

	dynamicAdminEmail := "dynamic-admin@example.test"
	body := bytes.NewBufferString(fmt.Sprintf(`{"email":%q,"role":"owner"}`, dynamicAdminEmail))
	req, _ = http.NewRequest(http.MethodPut, ts.URL+"/admin/api/v1/authorization/admins/members", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "admin-session"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT admin member: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("put admin member status = %d, want 200: %s", resp.StatusCode, respBody)
	}
	var putAdminMembershipResp struct {
		Membership struct {
			SelectorKind  string `json:"selectorKind"`
			SelectorValue string `json:"selectorValue"`
			Email         string `json:"email"`
			Role          string `json:"role"`
		} `json:"membership"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&putAdminMembershipResp); err != nil {
		t.Fatalf("decode admin membership response: %v", err)
	}
	dynamicAdmin, err := svc.Users.FindUserByEmail(context.Background(), dynamicAdminEmail)
	if err != nil {
		t.Fatalf("FindUserByEmail dynamic admin member: %v", err)
	}
	if putAdminMembershipResp.Membership.SelectorKind != "subject_id" {
		t.Fatalf("admin membership selectorKind = %q, want subject_id", putAdminMembershipResp.Membership.SelectorKind)
	}
	if putAdminMembershipResp.Membership.SelectorValue != principal.UserSubjectID(dynamicAdmin.ID) {
		t.Fatalf("admin membership selectorValue = %q, want canonical subject id", putAdminMembershipResp.Membership.SelectorValue)
	}
	if putAdminMembershipResp.Membership.Email != dynamicAdminEmail {
		t.Fatalf("admin membership email = %q, want %q", putAdminMembershipResp.Membership.Email, dynamicAdminEmail)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/admin/api/v1/authorization/admins/members", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "admin-session"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET admin members after put: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin members after put status = %d, want 200", resp.StatusCode)
	}
	members = nil
	if err := json.NewDecoder(resp.Body).Decode(&members); err != nil {
		t.Fatalf("decoding admin members after put: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("expected 2 merged admin members, got %d (%+v)", len(members), members)
	}
	foundDynamicAdminMember := false
	for _, member := range members {
		if member["source"] != "dynamic" {
			continue
		}
		foundDynamicAdminMember = true
		if got := member["selectorKind"]; got != "subject_id" {
			t.Fatalf("dynamic admin member selectorKind = %v, want subject_id", got)
		}
		if member["selectorValue"] != principal.UserSubjectID(dynamicAdmin.ID) {
			t.Fatalf("dynamic admin member selector metadata = %+v, want canonical subject_id selectorValue", member)
		}
		if member["email"] != dynamicAdminEmail {
			t.Fatalf("dynamic admin member email = %v, want %q", member["email"], dynamicAdminEmail)
		}
	}
	if !foundDynamicAdminMember {
		t.Fatalf("expected one dynamic admin member, got %+v", members)
	}

	body = bytes.NewBufferString(`{"email":"static-admin@example.test","role":"owner"}`)
	req, _ = http.NewRequest(http.MethodPut, ts.URL+"/admin/api/v1/authorization/admins/members", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "admin-session"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT static admin conflict: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusConflict {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("put static admin conflict status = %d, want 409: %s", resp.StatusCode, respBody)
	}

	roles, err := svc.WorkspaceRoles.ListByPrincipal(ctx, dynamicAdmin.ID)
	if err != nil {
		t.Fatalf("ListByPrincipal(dynamic admin): %v", err)
	}
	if len(roles) != 1 || roles[0].Role != "owner" {
		t.Fatalf("workspace roles = %+v, want [owner]", roles)
	}

	body = bytes.NewBufferString(fmt.Sprintf(`{"subjectId":%q,"role":"operator"}`, principal.UserSubjectID(dynamicAdmin.ID)))
	req, _ = http.NewRequest(http.MethodPut, ts.URL+"/admin/api/v1/authorization/admins/members", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "admin-session"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT dynamic admin role change: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("put dynamic admin role change status = %d, want 200: %s", resp.StatusCode, respBody)
	}

	roles, err = svc.WorkspaceRoles.ListByPrincipal(ctx, dynamicAdmin.ID)
	if err != nil {
		t.Fatalf("ListByPrincipal(dynamic admin) after role change: %v", err)
	}
	if len(roles) != 1 || roles[0].Role != "operator" {
		t.Fatalf("workspace roles after role change = %+v, want [operator]", roles)
	}
	req, _ = http.NewRequest(http.MethodDelete, ts.URL+"/admin/api/v1/authorization/admins/members/"+url.PathEscape(principal.UserSubjectID(dynamicAdmin.ID)), nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "admin-session"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE admin member: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("delete admin member status = %d, want 200: %s", resp.StatusCode, respBody)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/admin/api/v1/authorization/admins/members", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "admin-session"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET admin members after delete: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin members after delete status = %d, want 200", resp.StatusCode)
	}
	members = nil
	if err := json.NewDecoder(resp.Body).Decode(&members); err != nil {
		t.Fatalf("decoding admin members after delete: %v", err)
	}
	if len(members) != 1 || members[0]["source"] != "static" {
		t.Fatalf("admin members after delete = %+v, want only static row", members)
	}
	roles, err = svc.WorkspaceRoles.ListByPrincipal(ctx, dynamicAdmin.ID)
	if err != nil {
		t.Fatalf("ListByPrincipal(dynamic admin) after delete: %v", err)
	}
	if len(roles) != 0 {
		t.Fatalf("workspace roles after delete = %+v, want none", roles)
	}
}

func TestAdminAPI_AdminAuthorizationProviderBackedReads(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	seedUser(t, svc, "static-admin@example.test")
	const adminRole = "owner"
	provider := newMemoryAuthorizationProvider("memory-authorization")
	legacy := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"admin_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					staticPolicyUserMember(t, svc, "static-admin@example.test", adminRole),
				},
			},
		},
	}, nil, nil, nil)
	authz := mustProviderBackedAuthorizer(t, legacy, provider)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "admin-session" {
					return nil, fmt.Errorf("invalid token")
				}
				return &core.UserIdentity{Email: "static-admin@example.test"}, nil
			},
		}
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.AuthorizationProvider = provider
		cfg.Admin = server.AdminRouteConfig{
			AuthorizationPolicy: "admin_policy",
			AllowedRoles:        []string{adminRole, "operator"},
		}
		cfg.AdminUI = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("admin"))
		})
	})
	testutil.CloseOnCleanup(t, ts)

	dynamicAdmin := seedUser(t, svc, "dynamic-admin@example.test")
	body := bytes.NewBufferString(fmt.Sprintf(`{"subjectId":%q,"role":"owner"}`, principal.UserSubjectID(dynamicAdmin.ID)))
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/admin/api/v1/authorization/admins/members", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "admin-session"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT admin member: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("put admin member status = %d, want 200: %s", resp.StatusCode, respBody)
	}

	canonicalIdentityID, err := svc.Users.CanonicalIdentityIDForUser(context.Background(), dynamicAdmin.ID)
	if err != nil {
		t.Fatalf("CanonicalIdentityIDForUser(dynamic admin): %v", err)
	}
	roles, err := svc.WorkspaceRoles.ListByPrincipal(context.Background(), canonicalIdentityID)
	if err != nil {
		t.Fatalf("ListByPrincipal(dynamic admin): %v", err)
	}
	if len(roles) != 1 || roles[0].Role != "owner" {
		t.Fatalf("workspace roles after provider-backed write = %+v, want [owner]", roles)
	}
	if err := authz.ReloadAuthorizationState(context.Background()); err != nil {
		t.Fatalf("ReloadAuthorizationState after provider-backed admin write: %v", err)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/admin/api/v1/authorization/admins/members", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "admin-session"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET admin members: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("admin members status = %d, want 200: %s", resp.StatusCode, respBody)
	}

	var members []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&members); err != nil {
		t.Fatalf("decoding admin members: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("expected provider-backed merged admin members, got %d (%+v)", len(members), members)
	}
}

func TestAdminAPI_ProviderBackedWritesDoNotRequireLegacyHumanAuthStores(t *testing.T) {
	t.Parallel()

	t.Run("plugin members", func(t *testing.T) {
		t.Parallel()

		svc := coretesting.NewStubServices(t)
		provider := newMemoryAuthorizationProvider("memory-authorization")
		legacy, err := authorization.New(config.AuthorizationConfig{
			Policies: map[string]config.HumanPolicyDef{
				"sample_policy": {
					Default: "deny",
					Members: []config.HumanPolicyMemberDef{
						staticPolicyUserMember(t, svc, "static@example.test", "admin"),
					},
				},
			},
		}, map[string]*config.ProviderEntry{
			"sample_plugin": {AuthorizationPolicy: "sample_policy", MountPath: "/sample"},
		}, nil, nil)
		if err != nil {
			t.Fatalf("authorization.New: %v", err)
		}
		authz := mustProviderBackedAuthorizer(t, legacy, provider)

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Auth = &coretesting.StubAuthProvider{
				N: "test",
				ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
					if token != "admin-session" {
						return nil, fmt.Errorf("invalid token")
					}
					return &core.UserIdentity{Email: "static@example.test"}, nil
				},
			}
			cfg.Services = svc
			cfg.Authorizer = authz
			cfg.AuthorizationProvider = provider
			cfg.PluginDefs = map[string]*config.ProviderEntry{
				"sample_plugin": {AuthorizationPolicy: "sample_policy", MountPath: "/sample"},
			}
			cfg.Admin = server.AdminRouteConfig{
				AuthorizationPolicy: "sample_policy",
				AllowedRoles:        []string{"admin"},
			}
			cfg.AdminUI = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("admin"))
			})
		})
		testutil.CloseOnCleanup(t, ts)

		dynamicUser := seedUser(t, svc, "dynamic@example.test")
		body := bytes.NewBufferString(fmt.Sprintf(`{"subjectId":%q,"role":"viewer"}`, principal.UserSubjectID(dynamicUser.ID)))
		req, _ := http.NewRequest(http.MethodPut, ts.URL+"/admin/api/v1/authorization/plugins/sample_plugin/members", body)
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session_token", Value: "admin-session"})
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("PUT dynamic member: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(resp.Body)
			t.Fatalf("put dynamic member status = %d, want 200: %s", resp.StatusCode, respBody)
		}
	})

	t.Run("admin members", func(t *testing.T) {
		t.Parallel()

		svc := coretesting.NewStubServices(t)
		provider := newMemoryAuthorizationProvider("memory-authorization")
		seedUser(t, svc, "static-admin@example.test")
		legacy := mustAuthorizer(t, config.AuthorizationConfig{
			Policies: map[string]config.HumanPolicyDef{
				"admin_policy": {
					Default: "deny",
					Members: []config.HumanPolicyMemberDef{
						staticPolicyUserMember(t, svc, "static-admin@example.test", "owner"),
					},
				},
			},
		}, nil, nil, nil)
		authz := mustProviderBackedAuthorizer(t, legacy, provider)

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Auth = &coretesting.StubAuthProvider{
				N: "test",
				ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
					if token != "admin-session" {
						return nil, fmt.Errorf("invalid token")
					}
					return &core.UserIdentity{Email: "static-admin@example.test"}, nil
				},
			}
			cfg.Services = svc
			cfg.Authorizer = authz
			cfg.AuthorizationProvider = provider
			cfg.Admin = server.AdminRouteConfig{
				AuthorizationPolicy: "admin_policy",
				AllowedRoles:        []string{"owner", "operator"},
			}
			cfg.AdminUI = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("admin"))
			})
		})
		testutil.CloseOnCleanup(t, ts)

		dynamicAdmin := seedUser(t, svc, "dynamic-admin@example.test")
		body := bytes.NewBufferString(fmt.Sprintf(`{"subjectId":%q,"role":"operator"}`, principal.UserSubjectID(dynamicAdmin.ID)))
		req, _ := http.NewRequest(http.MethodPut, ts.URL+"/admin/api/v1/authorization/admins/members", body)
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session_token", Value: "admin-session"})
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("PUT admin member: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(resp.Body)
			t.Fatalf("put admin member status = %d, want 200: %s", resp.StatusCode, respBody)
		}
	})
}

func TestAdminAPI_AdminAuthorizationWriteUsesAllowedAdminRoles(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	seedUser(t, svc, "static-admin@example.test")
	seedUser(t, svc, "viewer@example.test")
	const adminRole = "ops-admin"
	provider := newMemoryAuthorizationProvider("memory-authorization")
	legacy := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"admin_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					staticPolicyUserMember(t, svc, "static-admin@example.test", adminRole),
				},
			},
		},
	}, nil, nil, nil)
	authz := mustProviderBackedAuthorizer(t, legacy, provider)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				switch token {
				case "ops-admin-session":
					return &core.UserIdentity{Email: "static-admin@example.test"}, nil
				case "viewer-session":
					return &core.UserIdentity{Email: "viewer@example.test"}, nil
				default:
					return nil, fmt.Errorf("invalid token")
				}
			},
		}
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.AuthorizationProvider = provider
		cfg.Admin = server.AdminRouteConfig{
			AuthorizationPolicy: "admin_policy",
			AllowedRoles:        []string{adminRole},
		}
		cfg.AdminUI = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("admin"))
		})
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/admin/api/v1/authorization/admins/members", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "ops-admin-session"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("ops-admin GET admin members: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("ops-admin get admin members status = %d, want 200: %s", resp.StatusCode, respBody)
	}
	if got := resp.Header.Get("X-Gestalt-Can-Write"); got != "true" {
		t.Fatalf("ops-admin can-write header = %q, want true", got)
	}

	viewer := seedUser(t, svc, "viewer@example.test")
	body := bytes.NewBufferString(fmt.Sprintf(`{"subjectId":%q,"role":"ops-admin"}`, principal.UserSubjectID(viewer.ID)))
	req, _ = http.NewRequest(http.MethodPut, ts.URL+"/admin/api/v1/authorization/admins/members", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "ops-admin-session"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("ops-admin PUT admin member: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("ops-admin put admin member status = %d, want 200: %s", resp.StatusCode, respBody)
	}

	req, _ = http.NewRequest(http.MethodDelete, ts.URL+"/admin/api/v1/authorization/admins/members/"+url.PathEscape(principal.UserSubjectID(viewer.ID)), nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "ops-admin-session"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("ops-admin DELETE admin member: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("ops-admin delete admin member status = %d, want 200: %s", resp.StatusCode, respBody)
	}
}

func TestAdminAPI_PluginAuthorizationUnavailable(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	authz, err := authorization.New(config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					staticPolicyUserMember(t, svc, "admin@example.test", "admin"),
				},
			},
		},
	}, map[string]*config.ProviderEntry{
		"sample_plugin": {AuthorizationPolicy: "sample_policy", MountPath: "/sample"},
	}, nil, nil)
	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}
	dynamicUser := seedUser(t, svc, "dynamic@example.test")

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "admin-session" {
					return nil, fmt.Errorf("invalid token")
				}
				return &core.UserIdentity{Email: "admin@example.test"}, nil
			},
		}
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.PluginDefs = map[string]*config.ProviderEntry{
			"sample_plugin": {AuthorizationPolicy: "sample_policy", MountPath: "/sample"},
		}
		cfg.Admin = server.AdminRouteConfig{
			AuthorizationPolicy: "sample_policy",
			AllowedRoles:        []string{"admin"},
		}
		cfg.AdminUI = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("admin"))
		})
	})
	testutil.CloseOnCleanup(t, ts)

	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "list", method: http.MethodGet, path: "/admin/api/v1/authorization/plugins/sample_plugin/members"},
		{name: "put", method: http.MethodPut, path: "/admin/api/v1/authorization/plugins/sample_plugin/members", body: fmt.Sprintf(`{"subjectId":%q,"role":"viewer"}`, principal.UserSubjectID(dynamicUser.ID))},
		{name: "delete", method: http.MethodDelete, path: "/admin/api/v1/authorization/plugins/sample_plugin/members/" + url.PathEscape(principal.UserSubjectID(dynamicUser.ID))},
	} {
		reqBody := io.Reader(nil)
		if tc.body != "" {
			reqBody = strings.NewReader(tc.body)
		}
		req, _ := http.NewRequest(tc.method, ts.URL+tc.path, reqBody)
		if tc.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		req.AddCookie(&http.Cookie{Name: "session_token", Value: "admin-session"})
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s members: %v", tc.name, err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusServiceUnavailable {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("%s members status = %d, want 503: %s", tc.name, resp.StatusCode, body)
		}
	}
}

func TestAdminAPI_AdminAuthorizationUnavailable(t *testing.T) {
	t.Parallel()

	t.Run("authorization provider missing", func(t *testing.T) {
		t.Parallel()

		svc := coretesting.NewStubServices(t)
		seedUser(t, svc, "admin@example.test")
		authz := mustAuthorizer(t, config.AuthorizationConfig{
			Policies: map[string]config.HumanPolicyDef{
				"admin_policy": {
					Default: "deny",
					Members: []config.HumanPolicyMemberDef{
						staticPolicyUserMember(t, svc, "admin@example.test", "admin"),
					},
				},
			},
		}, nil, nil, nil)
		user, err := svc.Users.FindOrCreateUser(context.Background(), "dynamic-admin@example.test")
		if err != nil {
			t.Fatalf("FindOrCreateUser dynamic admin: %v", err)
		}

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Auth = &coretesting.StubAuthProvider{
				N: "test",
				ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
					if token != "admin-session" {
						return nil, fmt.Errorf("invalid token")
					}
					return &core.UserIdentity{Email: "admin@example.test"}, nil
				},
			}
			cfg.Services = svc
			cfg.Authorizer = authz
			cfg.Admin = server.AdminRouteConfig{
				AuthorizationPolicy: "admin_policy",
				AllowedRoles:        []string{"admin"},
			}
			cfg.AdminUI = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("admin"))
			})
		})
		testutil.CloseOnCleanup(t, ts)

		for _, tc := range []struct {
			name   string
			method string
			path   string
			body   string
		}{
			{name: "list", method: http.MethodGet, path: "/admin/api/v1/authorization/admins/members"},
			{name: "put", method: http.MethodPut, path: "/admin/api/v1/authorization/admins/members", body: fmt.Sprintf(`{"subjectId":%q,"role":"operator"}`, principal.UserSubjectID(user.ID))},
			{name: "delete", method: http.MethodDelete, path: "/admin/api/v1/authorization/admins/members/" + url.PathEscape(principal.UserSubjectID(user.ID))},
		} {
			reqBody := io.Reader(nil)
			if tc.body != "" {
				reqBody = strings.NewReader(tc.body)
			}
			req, _ := http.NewRequest(tc.method, ts.URL+tc.path, reqBody)
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			req.AddCookie(&http.Cookie{Name: "session_token", Value: "admin-session"})
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("%s admin members without authorization provider: %v", tc.name, err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusServiceUnavailable {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("%s status = %d, want 503: %s", tc.name, resp.StatusCode, body)
			}
		}
	})

	t.Run("admin policy unset", func(t *testing.T) {
		t.Parallel()

		svc := coretesting.NewStubServices(t)
		authz := mustAuthorizer(t, config.AuthorizationConfig{}, nil, nil, nil)

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Auth = &coretesting.StubAuthProvider{
				N: "test",
				ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
					if token != "admin-session" {
						return nil, fmt.Errorf("invalid token")
					}
					return &core.UserIdentity{Email: "admin@example.test"}, nil
				},
			}
			cfg.Services = svc
			cfg.Authorizer = authz
			cfg.AdminUI = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("admin"))
			})
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/admin/api/v1/authorization/admins/members", nil)
		req.AddCookie(&http.Cookie{Name: "session_token", Value: "admin-session"})
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET admin members with admin policy unset: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusServiceUnavailable {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d, want 503: %s", resp.StatusCode, body)
		}
	})

}

func TestAdminAPI_PluginAuthorizationPutFailureReturnsServerError(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	provider := newMemoryAuthorizationProvider("memory-authorization")
	legacy, err := authorization.New(config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {Default: "deny"},
		},
	}, map[string]*config.ProviderEntry{
		"sample_plugin": {AuthorizationPolicy: "sample_policy", MountPath: "/sample"},
	}, nil, nil)
	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}
	authz := mustProviderBackedAuthorizer(t, legacy, provider)
	provider.writeErr = fmt.Errorf("provider write failed")

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.AuthorizationProvider = provider
		cfg.PluginDefs = map[string]*config.ProviderEntry{
			"sample_plugin": {AuthorizationPolicy: "sample_policy", MountPath: "/sample"},
		}
		cfg.AdminUI = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("admin"))
		})
	})
	testutil.CloseOnCleanup(t, ts)

	dynamicUser := seedUser(t, svc, "dynamic@example.test")
	body := bytes.NewBufferString(fmt.Sprintf(`{"subjectId":%q,"role":"viewer"}`, principal.UserSubjectID(dynamicUser.ID)))
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/admin/api/v1/authorization/plugins/sample_plugin/members", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT dynamic member: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("put dynamic member status = %d, want 500: %s", resp.StatusCode, respBody)
	}
}

func TestAdminAPI_AdminAuthorizationPutFailureReturnsServerError(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	seedUser(t, svc, "static-admin@example.test")
	provider := newMemoryAuthorizationProvider("memory-authorization")
	legacy := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"admin_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					staticPolicyUserMember(t, svc, "static-admin@example.test", "admin"),
				},
			},
		},
	}, nil, nil, nil)
	authz := mustProviderBackedAuthorizer(t, legacy, provider)
	provider.writeErr = fmt.Errorf("provider write failed")

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "admin-session" {
					return nil, fmt.Errorf("invalid token")
				}
				return &core.UserIdentity{Email: "static-admin@example.test"}, nil
			},
		}
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.AuthorizationProvider = provider
		cfg.Admin = server.AdminRouteConfig{
			AuthorizationPolicy: "admin_policy",
			AllowedRoles:        []string{"admin"},
		}
		cfg.AdminUI = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("admin"))
		})
	})
	testutil.CloseOnCleanup(t, ts)

	dynamicAdmin := seedUser(t, svc, "dynamic-admin@example.test")
	body := bytes.NewBufferString(fmt.Sprintf(`{"subjectId":%q,"role":"admin"}`, principal.UserSubjectID(dynamicAdmin.ID)))
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/admin/api/v1/authorization/admins/members", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "admin-session"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT admin member: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("put admin member status = %d, want 500: %s", resp.StatusCode, respBody)
	}
}

func TestMountedUIRoutesHiddenOnManagementProfile(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.RouteProfile = server.RouteProfileManagement
		cfg.MountedUIs = []server.MountedUI{{
			Path:    "/sample-portal",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("unexpected")) }),
		}}
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/sample-portal/sync")
	if err != nil {
		t.Fatalf("GET management mounted route: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestMountedUIAuthorizationPolicyRequiresExplicitRouteCoverage(t *testing.T) {
	t.Parallel()

	makeConfig := func(mounted server.MountedUI) server.Config {
		svc := coretesting.NewStubServices(t)
		authz := mustAuthorizer(t, config.AuthorizationConfig{
			Policies: map[string]config.HumanPolicyDef{
				"sample_policy": {
					Default: "deny",
					Members: []config.HumanPolicyMemberDef{
						staticPolicyUserMember(t, svc, "viewer@example.test", "viewer"),
					},
				},
			},
		}, nil, map[string]*config.ProviderEntry{
			"sample_portal": {AuthorizationPolicy: "sample_policy"},
		}, nil)
		return server.Config{
			Auth:        &coretesting.StubAuthProvider{N: "test"},
			Services:    svc,
			Invoker:     &testutil.StubInvoker{},
			Authorizer:  authz,
			StateSecret: []byte("0123456789abcdef0123456789abcdef"),
			MountedUIs:  []server.MountedUI{mounted},
			Providers: func() *registry.ProviderMap[core.Provider] {
				reg := registry.New()
				return &reg.Providers
			}(),
		}
	}

	tests := []struct {
		name    string
		mounted server.MountedUI
		want    string
	}{
		{
			name: "missing routes",
			mounted: server.MountedUI{
				Name:                "sample_portal",
				Path:                "/sample-portal",
				AuthorizationPolicy: "sample_policy",
				Handler:             http.NotFoundHandler(),
			},
			want: "must declare at least one route",
		},
		{
			name: "missing root coverage",
			mounted: server.MountedUI{
				Name:                "sample_portal",
				Path:                "/sample-portal",
				AuthorizationPolicy: "sample_policy",
				Routes: []server.MountedUIRoute{
					{Path: "/sync/*", AllowedRoles: []string{"viewer", "admin"}},
				},
				Handler: http.NotFoundHandler(),
			},
			want: "must declare a route covering /",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := server.New(makeConfig(tc.mounted))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("server.New error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestMountedUIAuthorizationPolicyNamedBuiltinAdminDoesNotUseAdminResolver(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>custom-builtin-admin-name</html>"), 0o644); err != nil {
		t.Fatalf("WriteFile index.html: %v", err)
	}
	handler, err := testutilUIHandler(dir)
	if err != nil {
		t.Fatalf("ui handler: %v", err)
	}

	svc := coretesting.NewStubServices(t)
	seedUser(t, svc, "static-admin@example.test")
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"admin_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					staticPolicyUserMember(t, svc, "static-admin@example.test", "admin"),
				},
			},
			"sample_policy": {
				Default: "deny",
			},
		},
	}, nil, nil, nil)
	seedUser(t, svc, "dynamic-admin@example.test")

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "dynamic-admin-session" {
					return nil, fmt.Errorf("invalid token")
				}
				return &core.UserIdentity{Email: "dynamic-admin@example.test"}, nil
			},
		}
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.Admin = server.AdminRouteConfig{
			AuthorizationPolicy: "admin_policy",
			AllowedRoles:        []string{"admin"},
		}
		cfg.MountedUIs = []server.MountedUI{{
			Name:                "builtin_admin",
			Path:                "/sample-portal",
			AuthorizationPolicy: "sample_policy",
			Routes: []server.MountedUIRoute{
				{Path: "/*", AllowedRoles: []string{"admin"}},
			},
			Handler: handler,
		}}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/sample-portal/", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "dynamic-admin-session"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET custom builtin_admin-named mounted ui: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 403: %s", resp.StatusCode, body)
	}
}

func TestMountedUIAuthorizationPolicyDeniesUnmatchedNavigationRoute(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>protected-sample-shell</html>"), 0o644); err != nil {
		t.Fatalf("WriteFile index.html: %v", err)
	}
	handler, err := testutilUIHandler(dir)
	if err != nil {
		t.Fatalf("ui handler: %v", err)
	}

	svc := coretesting.NewStubServices(t)
	viewer := seedUser(t, svc, "viewer@example.test")
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(viewer.ID), Role: "viewer"},
				},
			},
		},
	}, nil, map[string]*config.ProviderEntry{
		"sample_portal": {AuthorizationPolicy: "sample_policy"},
	}, nil)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "viewer-session" {
					return nil, fmt.Errorf("invalid token")
				}
				return &core.UserIdentity{Email: "viewer@example.test"}, nil
			},
		}
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.MountedUIs = []server.MountedUI{{
			Name:                "sample_portal",
			Path:                "/sample-portal",
			AuthorizationPolicy: "sample_policy",
			Routes: []server.MountedUIRoute{
				{Path: "/", AllowedRoles: []string{"viewer", "admin"}},
				{Path: "/sync/*", AllowedRoles: []string{"viewer", "admin"}},
			},
			Handler: handler,
		}}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/sample-portal/admin", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "viewer-session"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET unmatched protected mounted route: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}

func TestMountedUIAuthorizationPolicyUsesCanonicalNavigationPaths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>protected-sample-shell</html>"), 0o644); err != nil {
		t.Fatalf("WriteFile index.html: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatalf("MkdirAll assets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("console.log('protected-sample')"), 0o644); err != nil {
		t.Fatalf("WriteFile app.js: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "reports"), 0o755); err != nil {
		t.Fatalf("MkdirAll reports: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "reports", "index.html"), []byte("<html>reports</html>"), 0o644); err != nil {
		t.Fatalf("WriteFile reports/index.html: %v", err)
	}
	handler, err := testutilUIHandler(dir)
	if err != nil {
		t.Fatalf("ui handler: %v", err)
	}

	svc := coretesting.NewStubServices(t)
	viewer := seedUser(t, svc, "viewer@example.test")
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(viewer.ID), Role: "viewer"},
				},
			},
		},
	}, nil, map[string]*config.ProviderEntry{
		"sample_portal": {AuthorizationPolicy: "sample_policy"},
	}, nil)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "viewer-session" {
					return nil, fmt.Errorf("invalid token")
				}
				return &core.UserIdentity{Email: "viewer@example.test"}, nil
			},
		}
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.MountedUIs = []server.MountedUI{{
			Name:                "sample_portal",
			Path:                "/sample-portal",
			AuthorizationPolicy: "sample_policy",
			Routes: []server.MountedUIRoute{
				{Path: "/", AllowedRoles: []string{"viewer", "admin"}},
				{Path: "/reports", AllowedRoles: []string{"viewer", "admin"}},
			},
			Handler: handler,
		}}
	})
	testutil.CloseOnCleanup(t, ts)

	assetReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/sample-portal/assets/app.js", nil)
	assetReq.AddCookie(&http.Cookie{Name: "session_token", Value: "viewer-session"})
	assetResp, err := http.DefaultClient.Do(assetReq)
	if err != nil {
		t.Fatalf("GET protected mounted asset: %v", err)
	}
	defer func() { _ = assetResp.Body.Close() }()
	if assetResp.StatusCode != http.StatusOK {
		t.Fatalf("asset status = %d, want %d", assetResp.StatusCode, http.StatusOK)
	}

	reportsReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/sample-portal/reports/index.html", nil)
	reportsReq.AddCookie(&http.Cookie{Name: "session_token", Value: "viewer-session"})
	reportsResp, err := http.DefaultClient.Do(reportsReq)
	if err != nil {
		t.Fatalf("GET protected mounted reports html: %v", err)
	}
	defer func() { _ = reportsResp.Body.Close() }()
	if reportsResp.StatusCode != http.StatusOK {
		t.Fatalf("reports html status = %d, want %d", reportsResp.StatusCode, http.StatusOK)
	}
}

func TestMountedUIAuthorizationPolicyUsesNearestAncestorRouteForNestedAssets(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>protected-sample-shell</html>"), 0o644); err != nil {
		t.Fatalf("WriteFile index.html: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "reports", "assets"), 0o755); err != nil {
		t.Fatalf("MkdirAll reports/assets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "reports", "assets", "app.js"), []byte("console.log('reports-only')"), 0o644); err != nil {
		t.Fatalf("WriteFile reports/assets/app.js: %v", err)
	}
	handler, err := testutilUIHandler(dir)
	if err != nil {
		t.Fatalf("ui handler: %v", err)
	}

	svc := coretesting.NewStubServices(t)
	viewer := seedUser(t, svc, "viewer@example.test")
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(viewer.ID), Role: "viewer"},
				},
			},
		},
	}, nil, map[string]*config.ProviderEntry{
		"sample_portal": {AuthorizationPolicy: "sample_policy"},
	}, nil)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "viewer-session" {
					return nil, fmt.Errorf("invalid token")
				}
				return &core.UserIdentity{Email: "viewer@example.test"}, nil
			},
		}
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.MountedUIs = []server.MountedUI{{
			Name:                "sample_portal",
			Path:                "/sample-portal",
			AuthorizationPolicy: "sample_policy",
			Routes: []server.MountedUIRoute{
				{Path: "/", AllowedRoles: []string{"viewer", "admin"}},
				{Path: "/reports", AllowedRoles: []string{"admin"}},
			},
			Handler: handler,
		}}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/sample-portal/reports/assets/app.js", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "viewer-session"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET nested protected mounted asset: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("nested asset status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}

func TestMountedUIAuthorizationPolicyAllowsExplicitCatchAllAndDottedRoutes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>protected-sample-shell</html>"), 0o644); err != nil {
		t.Fatalf("WriteFile index.html: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "admin"), 0o755); err != nil {
		t.Fatalf("MkdirAll admin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "admin", "widget.js"), []byte("console.log('admin-only')"), 0o644); err != nil {
		t.Fatalf("WriteFile admin/widget.js: %v", err)
	}
	handler, err := testutilUIHandler(dir)
	if err != nil {
		t.Fatalf("ui handler: %v", err)
	}

	svc := coretesting.NewStubServices(t)
	viewer := seedUser(t, svc, "viewer@example.test")
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(viewer.ID), Role: "viewer"},
				},
			},
		},
	}, nil, map[string]*config.ProviderEntry{
		"sample_portal": {AuthorizationPolicy: "sample_policy"},
	}, nil)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "viewer-session" {
					return nil, fmt.Errorf("invalid token")
				}
				return &core.UserIdentity{Email: "viewer@example.test"}, nil
			},
		}
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.MountedUIs = []server.MountedUI{{
			Name:                "sample_portal",
			Path:                "/sample-portal",
			AuthorizationPolicy: "sample_policy",
			Routes: []server.MountedUIRoute{
				{Path: "/admin/widget.js", AllowedRoles: []string{"admin"}},
				{Path: "/*", AllowedRoles: []string{"viewer", "admin"}},
			},
			Handler: handler,
		}}
	})
	testutil.CloseOnCleanup(t, ts)

	dottedReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/sample-portal/customers/acme.test", nil)
	dottedReq.AddCookie(&http.Cookie{Name: "session_token", Value: "viewer-session"})
	dottedResp, err := http.DefaultClient.Do(dottedReq)
	if err != nil {
		t.Fatalf("GET dotted protected mounted route: %v", err)
	}
	defer func() { _ = dottedResp.Body.Close() }()
	if dottedResp.StatusCode != http.StatusOK {
		t.Fatalf("dotted route status = %d, want %d", dottedResp.StatusCode, http.StatusOK)
	}

	adminReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/sample-portal/admin/widget.js", nil)
	adminReq.AddCookie(&http.Cookie{Name: "session_token", Value: "viewer-session"})
	adminResp, err := http.DefaultClient.Do(adminReq)
	if err != nil {
		t.Fatalf("GET asset-like protected mounted route: %v", err)
	}
	defer func() { _ = adminResp.Body.Close() }()
	if adminResp.StatusCode != http.StatusForbidden {
		t.Fatalf("asset-like route status = %d, want %d", adminResp.StatusCode, http.StatusForbidden)
	}
}

func TestMountedUIAuthorizationPolicyPrefersExactRoutesOverWildcards(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>protected-sample-shell</html>"), 0o644); err != nil {
		t.Fatalf("WriteFile index.html: %v", err)
	}
	handler, err := testutilUIHandler(dir)
	if err != nil {
		t.Fatalf("ui handler: %v", err)
	}

	svc := coretesting.NewStubServices(t)
	viewer := seedUser(t, svc, "viewer@example.test")
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(viewer.ID), Role: "viewer"},
				},
			},
		},
	}, nil, map[string]*config.ProviderEntry{
		"sample_portal": {AuthorizationPolicy: "sample_policy"},
	}, nil)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "viewer-session" {
					return nil, fmt.Errorf("invalid token")
				}
				return &core.UserIdentity{Email: "viewer@example.test"}, nil
			},
		}
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.MountedUIs = []server.MountedUI{{
			Name:                "sample_portal",
			Path:                "/sample-portal",
			AuthorizationPolicy: "sample_policy",
			Routes: []server.MountedUIRoute{
				{Path: "/admin/settings", AllowedRoles: []string{"admin"}},
				{Path: "/admin/*", AllowedRoles: []string{"viewer", "admin"}},
				{Path: "/*", AllowedRoles: []string{"viewer", "admin"}},
			},
			Handler: handler,
		}}
	})
	testutil.CloseOnCleanup(t, ts)

	exactReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/sample-portal/admin/settings", nil)
	exactReq.AddCookie(&http.Cookie{Name: "session_token", Value: "viewer-session"})
	exactResp, err := http.DefaultClient.Do(exactReq)
	if err != nil {
		t.Fatalf("GET exact protected mounted route: %v", err)
	}
	defer func() { _ = exactResp.Body.Close() }()
	if exactResp.StatusCode != http.StatusForbidden {
		t.Fatalf("exact route status = %d, want %d", exactResp.StatusCode, http.StatusForbidden)
	}

	wildReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/sample-portal/admin/overview", nil)
	wildReq.AddCookie(&http.Cookie{Name: "session_token", Value: "viewer-session"})
	wildResp, err := http.DefaultClient.Do(wildReq)
	if err != nil {
		t.Fatalf("GET wildcard protected mounted route: %v", err)
	}
	defer func() { _ = wildResp.Body.Close() }()
	if wildResp.StatusCode != http.StatusOK {
		t.Fatalf("wildcard route status = %d, want %d", wildResp.StatusCode, http.StatusOK)
	}
}

func TestMountedUIAuthorizationPolicyExactRoutesDoNotMatchDescendants(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>protected-sample-shell</html>"), 0o644); err != nil {
		t.Fatalf("WriteFile index.html: %v", err)
	}
	handler, err := testutilUIHandler(dir)
	if err != nil {
		t.Fatalf("ui handler: %v", err)
	}

	svc := coretesting.NewStubServices(t)
	viewer := seedUser(t, svc, "viewer@example.test")
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(viewer.ID), Role: "viewer"},
				},
			},
		},
	}, nil, map[string]*config.ProviderEntry{
		"sample_portal": {AuthorizationPolicy: "sample_policy"},
	}, nil)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "viewer-session" {
					return nil, fmt.Errorf("invalid token")
				}
				return &core.UserIdentity{Email: "viewer@example.test"}, nil
			},
		}
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.MountedUIs = []server.MountedUI{{
			Name:                "sample_portal",
			Path:                "/sample-portal",
			AuthorizationPolicy: "sample_policy",
			Routes: []server.MountedUIRoute{
				{Path: "/admin", AllowedRoles: []string{"viewer", "admin"}},
				{Path: "/admin/*", AllowedRoles: []string{"admin"}},
				{Path: "/*", AllowedRoles: []string{"viewer", "admin"}},
			},
			Handler: handler,
		}}
	})
	testutil.CloseOnCleanup(t, ts)

	exactReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/sample-portal/admin", nil)
	exactReq.AddCookie(&http.Cookie{Name: "session_token", Value: "viewer-session"})
	exactResp, err := http.DefaultClient.Do(exactReq)
	if err != nil {
		t.Fatalf("GET exact protected mounted route: %v", err)
	}
	defer func() { _ = exactResp.Body.Close() }()
	if exactResp.StatusCode != http.StatusOK {
		t.Fatalf("exact route status = %d, want %d", exactResp.StatusCode, http.StatusOK)
	}

	descendantReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/sample-portal/admin/reports", nil)
	descendantReq.AddCookie(&http.Cookie{Name: "session_token", Value: "viewer-session"})
	descendantResp, err := http.DefaultClient.Do(descendantReq)
	if err != nil {
		t.Fatalf("GET descendant protected mounted route: %v", err)
	}
	defer func() { _ = descendantResp.Body.Close() }()
	if descendantResp.StatusCode != http.StatusForbidden {
		t.Fatalf("descendant route status = %d, want %d", descendantResp.StatusCode, http.StatusForbidden)
	}

	htmlReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/sample-portal/admin/index.html", nil)
	htmlReq.AddCookie(&http.Cookie{Name: "session_token", Value: "viewer-session"})
	htmlResp, err := http.DefaultClient.Do(htmlReq)
	if err != nil {
		t.Fatalf("GET html descendant protected mounted route: %v", err)
	}
	defer func() { _ = htmlResp.Body.Close() }()
	if htmlResp.StatusCode != http.StatusForbidden {
		t.Fatalf("html descendant route status = %d, want %d", htmlResp.StatusCode, http.StatusForbidden)
	}
}

func TestMountedRootUIRoutes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>root-shell</html>"), 0o644); err != nil {
		t.Fatalf("WriteFile index.html: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatalf("MkdirAll assets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("console.log('root-ui')"), 0o644); err != nil {
		t.Fatalf("WriteFile app.js: %v", err)
	}
	handler, err := testutilUIHandler(dir)
	if err != nil {
		t.Fatalf("ui handler: %v", err)
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.MountedUIs = []server.MountedUI{{
			Path:    "/",
			Handler: handler,
		}}
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll /: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(string(body), "root-shell") {
		t.Fatalf("body = %q, want root shell", body)
	}

	resp, err = http.Get(ts.URL + "/integrations")
	if err != nil {
		t.Fatalf("GET /integrations: %v", err)
	}
	body, err = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll /integrations: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("integrations status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(string(body), "root-shell") {
		t.Fatalf("integrations body = %q, want root shell", body)
	}

	resp, err = http.Get(ts.URL + "/assets/app.js")
	if err != nil {
		t.Fatalf("GET /assets/app.js: %v", err)
	}
	body, err = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll /assets/app.js: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("asset status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(string(body), "root-ui") {
		t.Fatalf("asset body = %q, want root-ui asset", body)
	}

	resp, err = http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	body, err = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll /health: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, want 200", resp.StatusCode)
	}
	if strings.Contains(string(body), "root-shell") {
		t.Fatalf("health body unexpectedly served root UI: %q", body)
	}
}

func TestMountedRootUIRoutesHiddenOnManagementProfile(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.RouteProfile = server.RouteProfileManagement
		cfg.MountedUIs = []server.MountedUI{{
			Path:    "/",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("unexpected")) }),
		}}
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/integrations")
	if err != nil {
		t.Fatalf("GET management root-mounted route: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func testutilUIHandler(dir string) (http.Handler, error) {
	return ui.DirHandler(dir)
}

func TestSecurityHeaders(t *testing.T) {
	t.Parallel()

	t.Run("default", func(t *testing.T) {
		t.Parallel()
		ts := newTestServer(t)
		testutil.CloseOnCleanup(t, ts)

		resp, err := http.Get(ts.URL + "/health")
		if err != nil {
			t.Fatalf("GET /health: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("X-Content-Type-Options = %q, want %q", got, "nosniff")
		}
		if got := resp.Header.Get("X-Frame-Options"); got != "DENY" {
			t.Errorf("X-Frame-Options = %q, want %q", got, "DENY")
		}
		if got := resp.Header.Get("Strict-Transport-Security"); got != "" {
			t.Errorf("Strict-Transport-Security = %q, want empty (secureCookies=false)", got)
		}
		csp := resp.Header.Get("Content-Security-Policy")
		for _, directive := range []string{
			"default-src 'self'",
			"script-src 'self' 'unsafe-inline'",
			"style-src 'self' 'unsafe-inline'",
			"object-src 'none'",
			"frame-ancestors 'none'",
		} {
			if !strings.Contains(csp, directive) {
				t.Errorf("Content-Security-Policy missing directive %q; got %q", directive, csp)
			}
		}
	})

	t.Run("secure_cookies", func(t *testing.T) {
		t.Parallel()
		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.SecureCookies = true
		})
		testutil.CloseOnCleanup(t, ts)

		resp, err := http.Get(ts.URL + "/health")
		if err != nil {
			t.Fatalf("GET /health: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("X-Content-Type-Options = %q, want %q", got, "nosniff")
		}
		if got := resp.Header.Get("X-Frame-Options"); got != "DENY" {
			t.Errorf("X-Frame-Options = %q, want %q", got, "DENY")
		}
		const wantHSTS = "max-age=63072000; includeSubDomains"
		if got := resp.Header.Get("Strict-Transport-Security"); got != wantHSTS {
			t.Errorf("Strict-Transport-Security = %q, want %q", got, wantHSTS)
		}
	})
}

func TestReadinessCheck(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/ready")
	if err != nil {
		t.Fatalf("GET /ready: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("expected status ok, got %q", body["status"])
	}
}

func TestReadinessCheck_NotReady(t *testing.T) {
	t.Parallel()
	var ready atomic.Bool
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Readiness = func() string {
			if !ready.Load() {
				return "providers loading"
			}
			return ""
		}
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/ready")
	if err != nil {
		t.Fatalf("GET /ready: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 while not ready, got %d", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body["status"] != "providers loading" {
		t.Fatalf("expected status 'providers loading', got %q", body["status"])
	}

	ready.Store(true)

	resp2, err := http.Get(ts.URL + "/ready")
	if err != nil {
		t.Fatalf("GET /ready after ready: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()

	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after ready, got %d", resp2.StatusCode)
	}
}

func TestReadinessCheck_DatastoreDown(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Readiness = func() string {
			return "datastore unavailable"
		}
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/ready")
	if err != nil {
		t.Fatalf("GET /ready: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body["status"] != "datastore unavailable" {
		t.Fatalf("expected status 'datastore unavailable', got %q", body["status"])
	}
}

func TestAuthMiddleware_ValidSession(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token == "valid-session" {
					return &core.UserIdentity{Email: "user@example.com"}, nil
				}
				return nil, fmt.Errorf("invalid token")
			},
		}
		cfg.Services = coretesting.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	req.Header.Set("Authorization", "Bearer valid-session")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestAuthMiddleware_ValidAPIToken(t *testing.T) {
	t.Parallel()

	plaintext, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	svc := coretesting.NewStubServices(t)
	seedAPIToken(t, svc, plaintext, hashed, "api-user")

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, _ string) (*core.UserIdentity, error) {
				return nil, fmt.Errorf("not a session token")
			},
		}
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestAuthMiddleware_ValidWorkloadToken(t *testing.T) {
	t.Parallel()

	plaintext, _, err := principal.GenerateToken(principal.TokenTypeWorkload)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	stub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{N: "weather", ConnMode: core.ConnectionModeNone},
		ops:             []core.Operation{{Name: "forecast", Method: http.MethodGet}},
	}
	providers := testutil.NewProviderRegistry(t, stub)
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Workloads: map[string]config.WorkloadDef{
			"weather-bot": {
				Token: plaintext,
				Providers: map[string]config.WorkloadProviderDef{
					"weather": {Allow: []string{"forecast"}},
				},
			},
		},
	}, providers, nil, nil)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, _ string) (*core.UserIdentity, error) {
				return nil, fmt.Errorf("not a session token")
			},
		}
		cfg.Providers = providers
		cfg.Services = coretesting.NewStubServices(t)
		cfg.Authorizer = authz
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}
}

func TestAuthMiddleware_NoAuth(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body["error"] == "" {
		t.Fatal("expected error message in response")
	}
}

func TestPluginRouteAuth_HTTPRoutesUseNamedProviderOverride(t *testing.T) {
	t.Parallel()

	plaintext, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	svc := coretesting.NewStubServices(t)
	seedAPIToken(t, svc, plaintext, hashed, "api-user")
	openProvider := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "open",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(_ context.Context, op string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: "open:" + op}, nil
			},
		},
		ops: []core.Operation{{Name: "ping", Method: http.MethodGet}},
	}
	lockedProvider := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "locked",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(_ context.Context, op string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: "locked:" + op}, nil
			},
		},
		ops: []core.Operation{{Name: "ping", Method: http.MethodGet}},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = nil
		cfg.AuthProviders = map[string]core.AuthenticationProvider{
			"alt": &coretesting.StubAuthProvider{
				N: "alt",
				ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
					if token != "alt-session" {
						return nil, fmt.Errorf("invalid token")
					}
					return &core.UserIdentity{Email: "alt-user@example.test"}, nil
				},
			},
		}
		cfg.Services = svc
		cfg.Providers = testutil.NewProviderRegistry(t, openProvider, lockedProvider)
		cfg.PluginDefs = map[string]*config.ProviderEntry{
			"locked": {
				RouteAuth: &config.RouteAuthDef{Provider: "alt"},
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	t.Run("server-level routes and plugins without overrides remain anonymous", func(t *testing.T) {
		t.Parallel()

		resp, err := http.Get(ts.URL + "/api/v1/integrations")
		if err != nil {
			t.Fatalf("GET integrations: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("integrations status = %d, want 200: %s", resp.StatusCode, body)
		}

		resp, err = http.Get(ts.URL + "/api/v1/open/ping")
		if err != nil {
			t.Fatalf("GET open ping: %v", err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("open ping status = %d, want 200: %s", resp.StatusCode, body)
		}
		if string(body) != "open:ping" {
			t.Fatalf("open ping body = %q, want %q", body, "open:ping")
		}

		resp, err = http.Get(ts.URL + "/api/v1/integrations/open/operations")
		if err != nil {
			t.Fatalf("GET open operations: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("open operations status = %d, want 200: %s", resp.StatusCode, body)
		}
	})

	t.Run("plugin override requires its named auth provider", func(t *testing.T) {
		t.Parallel()

		resp, err := http.Get(ts.URL + "/api/v1/locked/ping")
		if err != nil {
			t.Fatalf("GET locked ping: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusUnauthorized {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("locked ping status = %d, want 401: %s", resp.StatusCode, body)
		}

		resp, err = http.Get(ts.URL + "/api/v1/integrations/locked/operations")
		if err != nil {
			t.Fatalf("GET locked operations: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusUnauthorized {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("locked operations status = %d, want 401: %s", resp.StatusCode, body)
		}
	})

	t.Run("named auth provider and api tokens both pass through route auth", func(t *testing.T) {
		t.Parallel()

		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/locked/ping", nil)
		req.Header.Set("Authorization", "Bearer alt-session")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET locked ping with named auth: %v", err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("locked ping with named auth status = %d, want 200: %s", resp.StatusCode, body)
		}
		if string(body) != "locked:ping" {
			t.Fatalf("locked ping body = %q, want %q", body, "locked:ping")
		}

		req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations/locked/operations", nil)
		req.Header.Set("Authorization", "Bearer "+plaintext)
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET locked operations with api token: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("locked operations with api token status = %d, want 200: %s", resp.StatusCode, body)
		}

		var ops []catalog.CatalogOperation
		if err := json.NewDecoder(resp.Body).Decode(&ops); err != nil {
			t.Fatalf("decoding locked operations: %v", err)
		}
		if len(ops) != 1 || ops[0].ID != "ping" {
			t.Fatalf("operations = %#v, want [ping]", ops)
		}
	})
}

func TestAuthMiddleware_UnprefixedTokenRejected(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, _ string) (*core.UserIdentity, error) {
				return nil, fmt.Errorf("not a session token")
			},
		}
		cfg.Services = coretesting.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	req.Header.Set("Authorization", "Bearer unprefixed-legacy-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unprefixed token, got %d", resp.StatusCode)
	}
}

func TestAuthMiddleware_PrefixedAPITokenSkipsOAuth(t *testing.T) {
	t.Parallel()

	plaintext, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	svc := coretesting.NewStubServices(t)
	seedAPIToken(t, svc, plaintext, hashed, "api-user")

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, _ string) (*core.UserIdentity, error) {
				t.Fatal("OAuth ValidateToken must not be called for prefixed API tokens")
				return nil, nil
			},
		}
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestMetricsEndpointsRequireAuth(t *testing.T) {
	t.Parallel()

	plaintext, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	svc := coretesting.NewStubServices(t)
	seedAPIToken(t, svc, plaintext, hashed, "api-user")

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, _ string) (*core.UserIdentity, error) {
				return nil, fmt.Errorf("not a session token")
			},
		}
		cfg.Services = svc
		cfg.PrometheusMetrics = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain; version=0.0.4")
			_, _ = w.Write([]byte("gestaltd_operation_count_total 1\n"))
		})
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated /metrics, got %d", resp.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/metrics", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("authenticated GET /metrics: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for authenticated /metrics, got %d: %s", resp.StatusCode, body)
	}
	if !bytes.Contains(body, []byte("gestaltd_operation_count_total")) {
		t.Fatalf("expected prometheus metric in body, got %s", body)
	}
}

func TestMetricsSessionAuthDoesNotRequireUserLookup(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "session-token" {
					return nil, fmt.Errorf("invalid token")
				}
				return &core.UserIdentity{Email: "metrics@example.test"}, nil
			},
		}
		cfg.Services = coretesting.NewStubServices(t)
		cfg.PrometheusMetrics = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain; version=0.0.4")
			_, _ = w.Write([]byte("gestaltd_operation_count_total 1\n"))
		})
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/metrics", nil)
	req.Header.Set("Authorization", "Bearer session-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("authenticated GET /metrics with session token: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for session-authenticated /metrics, got %d: %s", resp.StatusCode, body)
	}
	if !bytes.Contains(body, []byte("gestaltd_operation_count_total")) {
		t.Fatalf("expected prometheus metric in body, got %s", body)
	}
}

func TestListIntegrations(t *testing.T) {
	t.Parallel()

	stub := &coretesting.StubIntegration{N: "slack", DN: "Slack", Desc: "Team messaging"}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Services = coretesting.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var integrations []struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
		Description string `json:"description"`
		Connected   bool   `json:"connected"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&integrations); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(integrations) != 1 {
		t.Fatalf("expected 1 integration, got %d", len(integrations))
	}
	if integrations[0].Name != "slack" {
		t.Fatalf("expected slack, got %q", integrations[0].Name)
	}
	if integrations[0].DisplayName != "Slack" {
		t.Fatalf("expected display name Slack, got %q", integrations[0].DisplayName)
	}
	if integrations[0].Connected {
		t.Fatal("expected connected=false when no tokens stored")
	}
}

func TestListIntegrations_IncludesMountedPath(t *testing.T) {
	t.Parallel()

	stub := &coretesting.StubIntegration{N: "github", DN: "GitHub"}
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.PluginDefs = map[string]*config.ProviderEntry{
			"github": {
				MountPath: "/github",
			},
		}
		cfg.MountedUIs = []server.MountedUI{{
			Name:       "github",
			PluginName: "github",
			Path:       "/github",
			Handler:    handler,
		}}
		cfg.Services = coretesting.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var integrations []struct {
		Name        string `json:"name"`
		MountedPath string `json:"mountedPath"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&integrations); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(integrations) != 1 {
		t.Fatalf("expected 1 integration, got %d", len(integrations))
	}
	if integrations[0].Name != "github" {
		t.Fatalf("expected github, got %q", integrations[0].Name)
	}
	if integrations[0].MountedPath != "/github" {
		t.Fatalf("expected mounted path /github, got %q", integrations[0].MountedPath)
	}
}

func TestListIntegrations_HumanAuthorizationFiltersByMountedUIAccessAndVisibleOperations(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	viewer := seedUser(t, svc, "viewer@example.test")
	policyMembers := []config.HumanPolicyMemberDef{
		{SubjectID: principal.UserSubjectID(viewer.ID), Role: "viewer"},
	}

	opsVisibleProvider := &stubNonOAuthProvider{
		name: "ops-visible",
		catalog: serverTestCatalog("ops-visible", []catalog.CatalogOperation{{
			ID:           "list",
			Method:       http.MethodGet,
			Path:         "/list",
			Transport:    catalog.TransportREST,
			AllowedRoles: []string{"viewer"},
		}}),
	}
	settingsVisibleProvider := &stubIntegrationWithCatalog{
		StubIntegration: coretesting.StubIntegration{N: "settings-visible", DN: "Settings Visible"},
		catalog: serverTestCatalog("settings-visible", []catalog.CatalogOperation{{
			ID:           "sync",
			Method:       http.MethodPost,
			Transport:    catalog.TransportMCPPassthrough,
			AllowedRoles: []string{"viewer"},
		}}),
	}
	uiVisibleProvider := &stubNonOAuthProvider{
		name: "ui-visible",
		catalog: serverTestCatalog("ui-visible", []catalog.CatalogOperation{{
			ID:           "sync",
			Method:       http.MethodPost,
			Transport:    catalog.TransportMCPPassthrough,
			AllowedRoles: []string{"viewer"},
		}}),
	}
	hiddenProvider := &stubNonOAuthProvider{
		name: "hidden",
		catalog: serverTestCatalog("hidden", []catalog.CatalogOperation{{
			ID:           "sync",
			Method:       http.MethodPost,
			Transport:    catalog.TransportMCPPassthrough,
			AllowedRoles: []string{"viewer"},
		}}),
	}
	providers := testutil.NewProviderRegistry(t, opsVisibleProvider, settingsVisibleProvider, uiVisibleProvider, hiddenProvider)
	pluginDefs := map[string]*config.ProviderEntry{
		"ops-visible":      {MountPath: "/ops-visible", AuthorizationPolicy: "sample_policy"},
		"settings-visible": {MountPath: "/settings-visible", AuthorizationPolicy: "sample_policy"},
		"ui-visible":       {MountPath: "/ui-visible", AuthorizationPolicy: "sample_policy"},
		"hidden":           {MountPath: "/hidden", AuthorizationPolicy: "sample_policy"},
	}
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: policyMembers,
			},
		},
	}, providers, pluginDefs, nil)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "viewer-session" {
					return nil, fmt.Errorf("invalid token")
				}
				return &core.UserIdentity{Email: "viewer@example.test"}, nil
			},
		}
		cfg.Providers = providers
		cfg.PluginDefs = pluginDefs
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.MountedUIs = []server.MountedUI{
			{
				Name:                "ops-visible",
				PluginName:          "ops-visible",
				Path:                "/ops-visible",
				AuthorizationPolicy: "sample_policy",
				Routes: []server.MountedUIRoute{
					{Path: "/*", AllowedRoles: []string{"admin"}},
				},
				Handler: handler,
			},
			{
				Name:                "settings-visible",
				PluginName:          "settings-visible",
				Path:                "/settings-visible",
				AuthorizationPolicy: "sample_policy",
				Routes: []server.MountedUIRoute{
					{Path: "/*", AllowedRoles: []string{"admin"}},
				},
				Handler: handler,
			},
			{
				Name:                "ui-visible",
				PluginName:          "ui-visible",
				Path:                "/ui-visible",
				AuthorizationPolicy: "sample_policy",
				Routes: []server.MountedUIRoute{
					{Path: "/*", AllowedRoles: []string{"viewer"}},
				},
				Handler: handler,
			},
			{
				Name:                "hidden",
				PluginName:          "hidden",
				Path:                "/hidden",
				AuthorizationPolicy: "sample_policy",
				Routes: []server.MountedUIRoute{
					{Path: "/*", AllowedRoles: []string{"admin"}},
				},
				Handler: handler,
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	req.Header.Set("Authorization", "Bearer viewer-session")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var integrations []struct {
		Name        string `json:"name"`
		MountedPath string `json:"mountedPath"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&integrations); err != nil {
		t.Fatalf("decoding: %v", err)
	}

	got := make(map[string]string, len(integrations))
	for _, integration := range integrations {
		got[integration.Name] = integration.MountedPath
	}

	if !reflect.DeepEqual(sortedKeys(got), []string{"ops-visible", "settings-visible", "ui-visible"}) {
		t.Fatalf("integration names = %v, want %v", sortedKeys(got), []string{"ops-visible", "settings-visible", "ui-visible"})
	}
	if got["ops-visible"] != "" {
		t.Fatalf("ops-visible mounted path = %q, want empty", got["ops-visible"])
	}
	if got["settings-visible"] != "" {
		t.Fatalf("settings-visible mounted path = %q, want empty", got["settings-visible"])
	}
	if got["ui-visible"] != "/ui-visible" {
		t.Fatalf("ui-visible mounted path = %q, want /ui-visible", got["ui-visible"])
	}
}

func TestWorkloadAuthorization_ListIntegrationsFiltersAndHidesConnectionAffordances(t *testing.T) {
	t.Parallel()

	workloadToken, _, err := principal.GenerateToken(principal.TokenTypeWorkload)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	svc := coretesting.NewStubServices(t)
	seedSubjectToken(t, svc, principal.WorkloadSubjectID("triage-bot"), "svc", "workspace", "default", "identity-svc-token")

	svcProvider := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{N: "svc", DN: "Service", ConnMode: core.ConnectionModeUser},
		ops:             []core.Operation{{Name: "run", Method: http.MethodGet}},
	}
	weatherProvider := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{N: "weather", DN: "Weather", ConnMode: core.ConnectionModeNone},
		ops:             []core.Operation{{Name: "forecast", Method: http.MethodGet}},
	}
	mcpOnlyProvider := &stubIntegrationWithCatalog{
		StubIntegration: coretesting.StubIntegration{N: "mcp-only", DN: "MCP Only", ConnMode: core.ConnectionModeNone},
		catalog: serverTestCatalog("mcp-only", []catalog.CatalogOperation{{
			ID:        "inspect",
			Method:    http.MethodPost,
			Transport: catalog.TransportMCPPassthrough,
		}}),
	}
	secretProvider := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{N: "secret", DN: "Secret", ConnMode: core.ConnectionModeNone},
		ops:             []core.Operation{{Name: "peek", Method: http.MethodGet}},
	}
	providers := testutil.NewProviderRegistry(t, svcProvider, weatherProvider, mcpOnlyProvider, secretProvider)
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Workloads: map[string]config.WorkloadDef{
			"triage-bot": {
				Token: workloadToken,
				Providers: map[string]config.WorkloadProviderDef{
					"svc":      {Allow: []string{"run"}},
					"weather":  {Allow: []string{"forecast"}},
					"mcp-only": {Allow: []string{"inspect"}},
				},
			},
		},
	}, providers, nil, map[string]string{"svc": "workspace"})

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
		cfg.Providers = providers
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.DefaultConnection = map[string]string{"svc": "workspace"}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	req.Header.Set("Authorization", "Bearer "+workloadToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var integrations []struct {
		Name             string                    `json:"name"`
		Connected        bool                      `json:"connected"`
		Instances        []map[string]any          `json:"instances"`
		AuthTypes        []string                  `json:"authTypes"`
		Connections      []map[string]any          `json:"connections"`
		CredentialFields []map[string]any          `json:"credentialFields"`
		ConnectionParams map[string]map[string]any `json:"connectionParams"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&integrations); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(integrations) != 2 {
		t.Fatalf("expected 2 integrations, got %+v", integrations)
	}

	got := map[string]struct {
		Connected        bool
		Instances        []map[string]any
		AuthTypes        []string
		Connections      []map[string]any
		CredentialFields []map[string]any
		ConnectionParams map[string]map[string]any
	}{}
	for _, integration := range integrations {
		got[integration.Name] = struct {
			Connected        bool
			Instances        []map[string]any
			AuthTypes        []string
			Connections      []map[string]any
			CredentialFields []map[string]any
			ConnectionParams map[string]map[string]any
		}{
			Connected:        integration.Connected,
			Instances:        integration.Instances,
			AuthTypes:        integration.AuthTypes,
			Connections:      integration.Connections,
			CredentialFields: integration.CredentialFields,
			ConnectionParams: integration.ConnectionParams,
		}
	}
	if _, ok := got["secret"]; ok {
		t.Fatalf("unauthorized integration was visible: %+v", integrations)
	}
	if _, ok := got["mcp-only"]; ok {
		t.Fatalf("mcp-only integration should not be visible over HTTP: %+v", integrations)
	}
	if !got["svc"].Connected {
		t.Fatalf("expected bound identity integration to be connected, got %+v", got["svc"])
	}
	if !got["weather"].Connected {
		t.Fatalf("expected connection-mode none integration to be connected, got %+v", got["weather"])
	}
	for name, info := range got {
		if len(info.Instances) != 0 || len(info.AuthTypes) != 0 || len(info.Connections) != 0 || len(info.CredentialFields) != 0 || len(info.ConnectionParams) != 0 {
			t.Fatalf("workload integration %q should not expose connection affordances: %+v", name, info)
		}
	}

	stubDB := svc.DB.(*coretesting.StubIndexedDB)
	stubDB.Err = fmt.Errorf("token store unavailable")
	t.Cleanup(func() { stubDB.Err = nil })

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	req.Header.Set("Authorization", "Bearer "+workloadToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request with token store outage: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusInternalServerError {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 500 when workload binding lookup fails, got %d: %s", resp.StatusCode, body)
	}
}

func TestWorkloadAuthorization_ListOperationsFiltersAndRejectsSelectors(t *testing.T) {
	t.Parallel()

	workloadToken, _, err := principal.GenerateToken(principal.TokenTypeWorkload)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	provider := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{N: "svc", ConnMode: core.ConnectionModeUser},
		ops: []core.Operation{
			{Name: "run", Method: http.MethodGet},
			{Name: "admin", Method: http.MethodGet},
		},
	}
	providers := testutil.NewProviderRegistry(t, provider)
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Workloads: map[string]config.WorkloadDef{
			"triage-bot": {
				Token: workloadToken,
				Providers: map[string]config.WorkloadProviderDef{
					"svc": {
						Connection: "workspace",
						Instance:   "default",
						Allow:      []string{"run"},
					},
				},
			},
		},
	}, providers, nil, nil)

	var auditBuf bytes.Buffer
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
		cfg.Providers = providers
		cfg.Services = coretesting.NewStubServices(t)
		cfg.Authorizer = authz
		cfg.AuditSink = invocation.NewSlogAuditSink(&auditBuf)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations/svc/operations", nil)
	req.Header.Set("Authorization", "Bearer "+workloadToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var ops []struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ops); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(ops) != 1 || ops[0].ID != "run" {
		t.Fatalf("operations = %+v, want only run", ops)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations/svc/operations?_connection=workspace", nil)
	req.Header.Set("Authorization", "Bearer "+workloadToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request with selector: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 403, got %d: %s", resp.StatusCode, body)
	}

	lines := bytes.Split(bytes.TrimSpace(auditBuf.Bytes()), []byte("\n"))
	if len(lines) == 0 {
		t.Fatal("expected list operations denial audit record")
	}
	var auditRecord map[string]any
	if err := json.Unmarshal(lines[len(lines)-1], &auditRecord); err != nil {
		t.Fatalf("parsing list operations audit record: %v\nraw: %s", err, auditBuf.String())
	}
	if auditRecord["provider"] != "svc" {
		t.Fatalf("expected audit provider svc, got %v", auditRecord["provider"])
	}
	if auditRecord["operation"] != "operations.list" {
		t.Fatalf("expected audit operation operations.list, got %v", auditRecord["operation"])
	}
	if auditRecord["allowed"] != false {
		t.Fatalf("expected denied audit record, got %v", auditRecord["allowed"])
	}
	if auditRecord["auth_source"] != "workload_token" {
		t.Fatalf("expected workload auth_source, got %v", auditRecord["auth_source"])
	}
	if auditRecord["subject_id"] != "workload:triage-bot" {
		t.Fatalf("expected workload subject_id, got %v", auditRecord["subject_id"])
	}
	if auditRecord["error"] != "workload callers may not override connection or instance bindings" {
		t.Fatalf("unexpected audit error: %v", auditRecord["error"])
	}

	svc := coretesting.NewStubServices(t)
	seedSubjectToken(t, svc, principal.WorkloadSubjectID("triage-bot"), "svc-session", "workspace", "team-a", "session-bound-token")

	var sessionCatalogToken string
	sessionProvider := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "svc-session", ConnMode: core.ConnectionModeUser},
		},
		catalogForRequestFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			sessionCatalogToken = token
			return &catalog.Catalog{
				Name: "svc-session",
				Operations: []catalog.CatalogOperation{
					{ID: "run", Method: http.MethodGet},
				},
			}, nil
		},
	}
	sessionProviders := testutil.NewProviderRegistry(t, sessionProvider)
	sessionAuthz := mustAuthorizer(t, config.AuthorizationConfig{
		Workloads: map[string]config.WorkloadDef{
			"triage-bot": {
				Token: workloadToken,
				Providers: map[string]config.WorkloadProviderDef{
					"svc-session": {
						Connection: "workspace",
						Instance:   "team-a",
						Allow:      []string{"run"},
					},
				},
			},
		},
	}, sessionProviders, nil, map[string]string{"svc-session": testDefaultConnection})

	sessionTS := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
		cfg.Providers = sessionProviders
		cfg.Services = svc
		cfg.Authorizer = sessionAuthz
		cfg.DefaultConnection = map[string]string{"svc-session": testDefaultConnection}
	})
	testutil.CloseOnCleanup(t, sessionTS)

	req, _ = http.NewRequest(http.MethodGet, sessionTS.URL+"/api/v1/integrations/svc-session/operations", nil)
	req.Header.Set("Authorization", "Bearer "+workloadToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("session catalog request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 for session-catalog workload discovery, got %d: %s", resp.StatusCode, body)
	}

	ops = nil
	if err := json.NewDecoder(resp.Body).Decode(&ops); err != nil {
		t.Fatalf("decoding session ops: %v", err)
	}
	if len(ops) != 1 || ops[0].ID != "run" {
		t.Fatalf("session operations = %+v, want only run", ops)
	}
	if sessionCatalogToken != "session-bound-token" {
		t.Fatalf("expected session catalog to use bound identity token, got %q", sessionCatalogToken)
	}
}

func TestListIntegrationsShowsConnected(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok1", SubjectID: principal.UserSubjectID(u.ID), Integration: "slack",
		Connection: "default", Instance: "default", AccessToken: "test-token",
	})

	stub := &coretesting.StubIntegration{N: "slack", DN: "Slack", Desc: "Team messaging"}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var integrations []struct {
		Name      string `json:"name"`
		Connected bool   `json:"connected"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&integrations); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(integrations) != 1 {
		t.Fatalf("expected 1 integration, got %d", len(integrations))
	}
	if !integrations[0].Connected {
		t.Fatal("expected connected=true when token exists")
	}
}

func TestListIntegrations_AuthTypes(t *testing.T) {
	t.Parallel()

	oauthStub := &coretesting.StubIntegration{N: "oauth-svc", DN: "OAuth Service"}
	manualStub := &stubManualProvider{
		StubIntegration: coretesting.StubIntegration{N: "manual-svc", DN: "Manual Service"},
	}
	mcpStub := &stubNonOAuthProvider{
		name: "clickhouse",
		ops:  []core.Operation{{Name: "query", Method: http.MethodGet}},
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, oauthStub, manualStub, mcpStub)
		cfg.Services = coretesting.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var integrations []struct {
		Name      string   `json:"name"`
		AuthTypes []string `json:"authTypes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&integrations); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(integrations) != 3 {
		t.Fatalf("expected 3 integrations, got %d", len(integrations))
	}

	authTypes := make(map[string][]string)
	for _, i := range integrations {
		authTypes[i.Name] = i.AuthTypes
	}
	if len(authTypes["manual-svc"]) != 1 || authTypes["manual-svc"][0] != "manual" {
		t.Fatalf("expected manual-svc auth_types=[manual], got %v", authTypes["manual-svc"])
	}
	if len(authTypes["oauth-svc"]) != 1 || authTypes["oauth-svc"][0] != "oauth" {
		t.Fatalf("expected oauth-svc auth_types=[oauth], got %v", authTypes["oauth-svc"])
	}
	if len(authTypes["clickhouse"]) != 0 {
		t.Fatalf("expected clickhouse auth_types=[], got %v", authTypes["clickhouse"])
	}
}

func TestListIntegrations_DerivesAuthTypesFromConnectionsWhenProviderOmitsThem(t *testing.T) {
	t.Parallel()

	stub := &stubNilAuthTypesProvider{
		StubIntegration: coretesting.StubIntegration{N: "example", DN: "Example"},
	}
	plugin := &config.ProviderEntry{
		Connections: map[string]*config.ConnectionDef{
			"default": {
				Auth: config.ConnectionAuthDef{
					Type: providermanifestv1.AuthTypeManual,
				},
			},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.PluginDefs = map[string]*config.ProviderEntry{
			"example": plugin,
		}
		cfg.Services = coretesting.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, `"authTypes":["manual"]`) {
		t.Fatalf("expected response to contain authTypes=[manual], got %s", text)
	}

	var integrations []struct {
		Name      string   `json:"name"`
		AuthTypes []string `json:"authTypes"`
	}
	if err := json.Unmarshal(body, &integrations); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(integrations) != 1 {
		t.Fatalf("expected 1 integration, got %d", len(integrations))
	}
	if !reflect.DeepEqual(integrations[0].AuthTypes, []string{"manual"}) {
		t.Fatalf("auth types = %v, want [manual]", integrations[0].AuthTypes)
	}
	if strings.Contains(text, `"authTypes":null`) {
		t.Fatalf("unexpected null authTypes in response: %s", text)
	}
}

func TestListIntegrations_ShowsCredentialedConnectionsInUserFacingMetadata(t *testing.T) {
	t.Parallel()

	stub := &stubManualProvider{
		StubIntegration: coretesting.StubIntegration{N: "launchdarkly", DN: "LaunchDarkly"},
	}
	plugin := &config.ProviderEntry{
		Source: config.NewMetadataSource("https://example.invalid/github-com-acme-plugins-launchdarkly/v1.0.0/provider-release.yaml"),
		ResolvedManifest: &providermanifestv1.Manifest{
			Spec: &providermanifestv1.Spec{
				Surfaces: &providermanifestv1.ProviderSurfaces{
					OpenAPI: &providermanifestv1.OpenAPISurface{
						Document:   "https://example.com/openapi.json",
						Connection: config.PluginConnectionName,
					},
				},
				Connections: map[string]*providermanifestv1.ManifestConnectionDef{
					"default": {
						Mode: providermanifestv1.ConnectionModeUser,
						Auth: &providermanifestv1.ProviderAuth{
							Type: providermanifestv1.AuthTypeManual,
						},
					},
				},
			},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.PluginDefs = map[string]*config.ProviderEntry{
			"launchdarkly": plugin,
		}
		cfg.Services = coretesting.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var integrations []struct {
		Name        string `json:"name"`
		AuthTypes   []string
		Connections []struct {
			Name      string   `json:"name"`
			AuthTypes []string `json:"authTypes"`
		} `json:"connections"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&integrations); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(integrations) != 1 {
		t.Fatalf("expected 1 integration, got %d", len(integrations))
	}
	if !reflect.DeepEqual(integrations[0].AuthTypes, []string{"manual"}) {
		t.Fatalf("auth types = %v, want [manual]", integrations[0].AuthTypes)
	}
	if len(integrations[0].Connections) != 2 {
		t.Fatalf("connections = %+v, want plugin and default user-facing connections", integrations[0].Connections)
	}
	if integrations[0].Connections[0].Name != "plugin" {
		t.Fatalf("first connection name = %q, want %q", integrations[0].Connections[0].Name, "plugin")
	}
	if integrations[0].Connections[1].Name != "default" {
		t.Fatalf("second connection name = %q, want %q", integrations[0].Connections[1].Name, "default")
	}
	for _, conn := range integrations[0].Connections {
		if !reflect.DeepEqual(conn.AuthTypes, []string{"manual"}) {
			t.Fatalf("connection auth types = %v, want [manual]", conn.AuthTypes)
		}
	}
}

func TestListIntegrations_ManualProvidersWithoutDeclaredCredentialsExposeGenericField(t *testing.T) {
	t.Parallel()

	stub := &stubManualProvider{
		StubIntegration: coretesting.StubIntegration{N: "linear", DN: "Linear"},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.PluginDefs = map[string]*config.ProviderEntry{
			"linear": {},
		}
		cfg.Services = coretesting.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	type credentialField struct {
		Name        string `json:"name"`
		Label       string `json:"label"`
		Description string `json:"description"`
	}
	var integrations []struct {
		Name             string            `json:"name"`
		AuthTypes        []string          `json:"authTypes"`
		CredentialFields []credentialField `json:"credentialFields"`
		Connections      []struct {
			Name             string            `json:"name"`
			AuthTypes        []string          `json:"authTypes"`
			CredentialFields []credentialField `json:"credentialFields"`
		} `json:"connections"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&integrations); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(integrations) != 1 {
		t.Fatalf("expected 1 integration, got %d", len(integrations))
	}

	wantFields := []credentialField{{Name: "credential", Label: "Credential"}}
	if !reflect.DeepEqual(integrations[0].AuthTypes, []string{"manual"}) {
		t.Fatalf("auth types = %v, want [manual]", integrations[0].AuthTypes)
	}
	if !reflect.DeepEqual(integrations[0].CredentialFields, wantFields) {
		t.Fatalf("credential fields = %+v, want %+v", integrations[0].CredentialFields, wantFields)
	}
	if len(integrations[0].Connections) != 1 {
		t.Fatalf("connections = %+v, want one default connection", integrations[0].Connections)
	}
	if integrations[0].Connections[0].Name != config.PluginConnectionAlias {
		t.Fatalf("connection name = %q, want %q", integrations[0].Connections[0].Name, config.PluginConnectionAlias)
	}
	if !reflect.DeepEqual(integrations[0].Connections[0].AuthTypes, []string{"manual"}) {
		t.Fatalf("connection auth types = %v, want [manual]", integrations[0].Connections[0].AuthTypes)
	}
	if !reflect.DeepEqual(integrations[0].Connections[0].CredentialFields, wantFields) {
		t.Fatalf("connection credential fields = %+v, want %+v", integrations[0].Connections[0].CredentialFields, wantFields)
	}
}

//nolint:paralleltest // This response-shape integration test flakes under full-package parallelism in CI.
func TestListIntegrations_ConnectionInfosUseResolvedConnectionDefs(t *testing.T) {
	t.Run("non manifest-backed connections still expose plugin and named auth", func(t *testing.T) {
		stub := &coretesting.StubIntegration{N: "example", DN: "Example"}
		plugin := &config.ProviderEntry{
			Source: config.NewMetadataSource("https://example.invalid/github-com-acme-plugins-example/v1.0.0/provider-release.yaml"),
			Auth: &config.ConnectionAuthDef{
				Type: providermanifestv1.AuthTypeManual,
				Credentials: []config.CredentialFieldDef{
					{Name: "plugin_token", Description: "Plugin Config Description"},
					{Name: "plugin_local_only", Label: "Plugin Local Only", Description: "Plugin Local Only Description"},
				},
			},
			Connections: map[string]*config.ConnectionDef{
				"workspace": {
					DisplayName: "Workspace OAuth",
					Auth: config.ConnectionAuthDef{
						Type: providermanifestv1.AuthTypeManual,
						Credentials: []config.CredentialFieldDef{
							{Name: "workspace_token", Label: "Workspace Config Token"},
							{Name: "workspace_local_only", Label: "Workspace Local Only", Description: "Workspace Local Only Description"},
						},
					},
				},
			},
			ResolvedManifest: &providermanifestv1.Manifest{
				Spec: &providermanifestv1.Spec{
					Connections: map[string]*providermanifestv1.ManifestConnectionDef{
						"default": {
							Auth: &providermanifestv1.ProviderAuth{
								Type: providermanifestv1.AuthTypeManual,
								Credentials: []providermanifestv1.CredentialField{
									{Name: "plugin_token", Label: "Plugin Manifest Token", Description: "Plugin Manifest Description"},
									{Name: "plugin_manifest_only", Label: "Plugin Manifest Only", Description: "Plugin Manifest Only Description"},
								},
							},
						},
						"workspace": {
							DisplayName: "Workspace Access",
							Auth: &providermanifestv1.ProviderAuth{
								Type: providermanifestv1.AuthTypeManual,
								Credentials: []providermanifestv1.CredentialField{
									{Name: "workspace_token", Label: "Workspace Manifest Token", Description: "Workspace Manifest Description"},
									{Name: "workspace_manifest_only", Label: "Workspace Manifest Only", Description: "Workspace Manifest Only Description"},
								},
							},
						},
					},
				},
			},
		}

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, stub)
			cfg.PluginDefs = map[string]*config.ProviderEntry{
				"example": plugin,
			}
			cfg.Services = coretesting.NewStubServices(t)
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		text := string(body)
		for _, fragment := range []string{
			`"instances":[]`,
			`"connectionParams":{}`,
			`"connections":[`,
			`"credentialFields":[`,
		} {
			if !strings.Contains(text, fragment) {
				t.Fatalf("expected response to contain %s, got %s", fragment, text)
			}
		}
		for _, fragment := range []string{
			`"instances":null`,
			`"connectionParams":null`,
			`"connections":null`,
			`"credentialFields":null`,
		} {
			if strings.Contains(text, fragment) {
				t.Fatalf("unexpected null collection in response: %s", text)
			}
		}

		type credentialField struct {
			Name        string `json:"name"`
			Label       string `json:"label"`
			Description string `json:"description"`
		}
		type connectionInfo struct {
			DisplayName      string            `json:"displayName"`
			Name             string            `json:"name"`
			AuthTypes        []string          `json:"authTypes"`
			CredentialFields []credentialField `json:"credentialFields"`
		}

		var integrations []struct {
			Name             string           `json:"name"`
			AuthTypes        []string         `json:"authTypes"`
			Instances        []map[string]any `json:"instances"`
			ConnectionParams map[string]any   `json:"connectionParams"`
			CredentialFields []map[string]any `json:"credentialFields"`
			Connections      []connectionInfo `json:"connections"`
		}
		if err := json.Unmarshal(body, &integrations); err != nil {
			t.Fatalf("decoding: %v", err)
		}
		if len(integrations) != 1 {
			t.Fatalf("expected 1 integration, got %d", len(integrations))
		}
		if integrations[0].Instances == nil || integrations[0].ConnectionParams == nil || integrations[0].CredentialFields == nil || integrations[0].Connections == nil {
			t.Fatalf("expected non-nil collections, got %+v", integrations[0])
		}

		got := make(map[string]connectionInfo, len(integrations[0].Connections))
		for _, conn := range integrations[0].Connections {
			got[conn.Name] = conn
		}

		if !reflect.DeepEqual(got[config.PluginConnectionAlias].AuthTypes, []string{"manual"}) || !reflect.DeepEqual(got[config.PluginConnectionAlias].CredentialFields, []credentialField{
			{Name: "plugin_token", Label: "Plugin Manifest Token", Description: "Plugin Config Description"},
			{Name: "plugin_manifest_only", Label: "Plugin Manifest Only", Description: "Plugin Manifest Only Description"},
			{Name: "plugin_local_only", Label: "Plugin Local Only", Description: "Plugin Local Only Description"},
		}) {
			t.Fatalf("plugin connection info = %+v", got[config.PluginConnectionAlias])
		}
		if got["workspace"].DisplayName != "Workspace OAuth" {
			t.Fatalf("workspace connection info = %+v", got["workspace"])
		}
		if !reflect.DeepEqual(got["workspace"].AuthTypes, []string{"manual"}) || !reflect.DeepEqual(got["workspace"].CredentialFields, []credentialField{
			{Name: "workspace_token", Label: "Workspace Config Token", Description: "Workspace Manifest Description"},
			{Name: "workspace_manifest_only", Label: "Workspace Manifest Only", Description: "Workspace Manifest Only Description"},
			{Name: "workspace_local_only", Label: "Workspace Local Only", Description: "Workspace Local Only Description"},
		}) {
			t.Fatalf("workspace connection info = %+v", got["workspace"])
		}
	})

	t.Run("manifest-backed API surfaces only expose the resolved named connection", func(t *testing.T) {
		stub := &coretesting.StubIntegration{N: "example", DN: "Example"}
		plugin := &config.ProviderEntry{
			Source: config.NewMetadataSource("https://example.invalid/github-com-acme-plugins-example/v1.0.0/provider-release.yaml"),
			Auth: &config.ConnectionAuthDef{
				Type: providermanifestv1.AuthTypeManual,
				Credentials: []config.CredentialFieldDef{
					{Name: "plugin_token", Label: "Plugin Token"},
				},
			},
			Connections: map[string]*config.ConnectionDef{
				"default": {
					Auth: config.ConnectionAuthDef{
						Type: providermanifestv1.AuthTypeManual,
						Credentials: []config.CredentialFieldDef{
							{Name: "default_token", Label: "Default Token"},
						},
					},
				},
			},
			ResolvedManifest: &providermanifestv1.Manifest{
				Spec: &providermanifestv1.Spec{
					Surfaces: &providermanifestv1.ProviderSurfaces{
						OpenAPI: &providermanifestv1.OpenAPISurface{
							Document: "https://example.com/openapi.json",
						},
					},
					Connections: map[string]*providermanifestv1.ManifestConnectionDef{
						"default": {
							Auth: &providermanifestv1.ProviderAuth{
								Type: providermanifestv1.AuthTypeManual,
								Credentials: []providermanifestv1.CredentialField{
									{Name: "default_token", Label: "Default Manifest Token"},
								},
							},
						},
					},
				},
			},
		}

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, stub)
			cfg.PluginDefs = map[string]*config.ProviderEntry{
				"example": plugin,
			}
			cfg.Services = coretesting.NewStubServices(t)
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var integrations []struct {
			Name        string `json:"name"`
			Connections []struct {
				Name      string   `json:"name"`
				AuthTypes []string `json:"authTypes"`
			} `json:"connections"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&integrations); err != nil {
			t.Fatalf("decoding: %v", err)
		}
		if len(integrations) != 1 {
			t.Fatalf("expected 1 integration, got %d", len(integrations))
		}
		if len(integrations[0].Connections) != 1 {
			t.Fatalf("expected only resolved named connection, got %+v", integrations[0].Connections)
		}
		if integrations[0].Connections[0].Name != "default" {
			t.Fatalf("expected only default connection, got %+v", integrations[0].Connections)
		}
		if !reflect.DeepEqual(integrations[0].Connections[0].AuthTypes, []string{"manual"}) {
			t.Fatalf("expected default authTypes [manual], got %+v", integrations[0].Connections[0].AuthTypes)
		}
	})

	t.Run("composite wrappers preserve API metadata", func(t *testing.T) {
		t.Parallel()

		apiProv := &stubManualProviderWithCapabilities{
			stubManualProvider: stubManualProvider{
				StubIntegration: coretesting.StubIntegration{N: "docs", DN: "Docs"},
			},
			credentialFields: []core.CredentialFieldDef{
				{Name: "api_key", Label: "API Key", Description: "Docs API key"},
			},
			connectionParams: map[string]core.ConnectionParamDef{
				"tenant": {
					Required:    true,
					Description: "Tenant slug",
					Default:     "acme",
				},
			},
		}
		prov := composite.New("docs", apiProv, &stubIntegrationWithSessionCatalog{
			stubIntegrationWithOps: stubIntegrationWithOps{
				StubIntegration: coretesting.StubIntegration{N: "docs-mcp", ConnMode: core.ConnectionModeNone},
			},
		})

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, prov)
			cfg.PluginDefs = map[string]*config.ProviderEntry{
				"docs": {},
			}
			cfg.Services = coretesting.NewStubServices(t)
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		type credentialField struct {
			Name        string `json:"name"`
			Label       string `json:"label"`
			Description string `json:"description"`
		}
		type connectionParam struct {
			Required    bool   `json:"required"`
			Description string `json:"description"`
			Default     string `json:"default"`
		}
		var integrations []struct {
			Name             string                     `json:"name"`
			AuthTypes        []string                   `json:"authTypes"`
			CredentialFields []credentialField          `json:"credentialFields"`
			ConnectionParams map[string]connectionParam `json:"connectionParams"`
			Connections      []struct {
				Name             string            `json:"name"`
				AuthTypes        []string          `json:"authTypes"`
				CredentialFields []credentialField `json:"credentialFields"`
			} `json:"connections"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&integrations); err != nil {
			t.Fatalf("decoding: %v", err)
		}
		if len(integrations) != 1 {
			t.Fatalf("expected 1 integration, got %d", len(integrations))
		}

		wantFields := []credentialField{{Name: "api_key", Label: "API Key", Description: "Docs API key"}}
		if !reflect.DeepEqual(integrations[0].AuthTypes, []string{"manual"}) {
			t.Fatalf("auth types = %v, want [manual]", integrations[0].AuthTypes)
		}
		if !reflect.DeepEqual(integrations[0].CredentialFields, wantFields) {
			t.Fatalf("credential fields = %+v, want %+v", integrations[0].CredentialFields, wantFields)
		}
		if !reflect.DeepEqual(integrations[0].ConnectionParams, map[string]connectionParam{
			"tenant": {
				Required:    true,
				Description: "Tenant slug",
				Default:     "acme",
			},
		}) {
			t.Fatalf("connection params = %+v", integrations[0].ConnectionParams)
		}
		if len(integrations[0].Connections) != 1 {
			t.Fatalf("connections = %+v, want one default connection", integrations[0].Connections)
		}
		if integrations[0].Connections[0].Name != config.PluginConnectionAlias {
			t.Fatalf("connection name = %q, want %q", integrations[0].Connections[0].Name, config.PluginConnectionAlias)
		}
		if !reflect.DeepEqual(integrations[0].Connections[0].AuthTypes, []string{"manual"}) {
			t.Fatalf("connection auth types = %v, want [manual]", integrations[0].Connections[0].AuthTypes)
		}
		if !reflect.DeepEqual(integrations[0].Connections[0].CredentialFields, wantFields) {
			t.Fatalf("connection credential fields = %+v, want %+v", integrations[0].Connections[0].CredentialFields, wantFields)
		}
	})

	t.Run("manifest-backed MCP passthrough without declared auth exposes no synthetic connection", func(t *testing.T) {
		t.Parallel()

		stub := &stubNonOAuthProvider{name: "clickhouse"}
		plugin := &config.ProviderEntry{
			Source:    config.NewMetadataSource("https://example.invalid/github-com-acme-plugins-clickhouse/v1.0.0/provider-release.yaml"),
			MountPath: "/clickhouse",
			ResolvedManifest: &providermanifestv1.Manifest{
				Spec: &providermanifestv1.Spec{
					Surfaces: &providermanifestv1.ProviderSurfaces{
						MCP: &providermanifestv1.MCPSurface{
							URL: "https://example.com/mcp",
						},
					},
				},
			},
		}

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, stub)
			cfg.PluginDefs = map[string]*config.ProviderEntry{
				"clickhouse": plugin,
			}
			cfg.Services = coretesting.NewStubServices(t)
			cfg.MountedUIs = []server.MountedUI{{
				Name:       "clickhouse",
				PluginName: "clickhouse",
				Path:       "/clickhouse",
				Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusOK)
				}),
			}}
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var integrations []struct {
			Name        string   `json:"name"`
			AuthTypes   []string `json:"authTypes"`
			Connections []struct {
				Name      string   `json:"name"`
				AuthTypes []string `json:"authTypes"`
			} `json:"connections"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&integrations); err != nil {
			t.Fatalf("decoding: %v", err)
		}
		if len(integrations) != 1 {
			t.Fatalf("expected 1 integration, got %d", len(integrations))
		}
		if len(integrations[0].AuthTypes) != 0 {
			t.Fatalf("expected no auth types, got %+v", integrations[0].AuthTypes)
		}
		if len(integrations[0].Connections) != 0 {
			t.Fatalf("expected no connectable connections, got %+v", integrations[0].Connections)
		}
	})

	t.Run("manifest-backed explicit no-auth MCP connection is exposed", func(t *testing.T) {
		t.Parallel()

		stub := &stubNonOAuthProvider{name: "clickhouse"}
		plugin := &config.ProviderEntry{
			Source: config.NewMetadataSource("https://example.invalid/github-com-acme-plugins-clickhouse/v1.0.0/provider-release.yaml"),
			ResolvedManifest: &providermanifestv1.Manifest{
				Spec: &providermanifestv1.Spec{
					Surfaces: &providermanifestv1.ProviderSurfaces{
						MCP: &providermanifestv1.MCPSurface{
							Connection: "MCP",
							URL:        "https://example.com/mcp",
						},
					},
					Connections: map[string]*providermanifestv1.ManifestConnectionDef{
						"MCP": {
							DisplayName: "MCP",
							Mode:        providermanifestv1.ConnectionModeUser,
							Auth: &providermanifestv1.ProviderAuth{
								Type: providermanifestv1.AuthTypeNone,
							},
						},
					},
				},
			},
		}

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, stub)
			cfg.PluginDefs = map[string]*config.ProviderEntry{
				"clickhouse": plugin,
			}
			cfg.Services = coretesting.NewStubServices(t)
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var integrations []struct {
			Name        string   `json:"name"`
			AuthTypes   []string `json:"authTypes"`
			Connections []struct {
				Name        string   `json:"name"`
				DisplayName string   `json:"displayName"`
				AuthTypes   []string `json:"authTypes"`
			} `json:"connections"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&integrations); err != nil {
			t.Fatalf("decoding: %v", err)
		}
		if len(integrations) != 1 {
			t.Fatalf("expected 1 integration, got %d", len(integrations))
		}
		if len(integrations[0].AuthTypes) != 0 {
			t.Fatalf("expected no top-level auth types, got %+v", integrations[0].AuthTypes)
		}
		if len(integrations[0].Connections) != 1 {
			t.Fatalf("expected one explicit no-auth connection, got %+v", integrations[0].Connections)
		}
		if integrations[0].Connections[0].Name != "MCP" || integrations[0].Connections[0].DisplayName != "MCP" {
			t.Fatalf("unexpected connection %+v", integrations[0].Connections[0])
		}
		if len(integrations[0].Connections[0].AuthTypes) != 0 {
			t.Fatalf("expected MCP connection authTypes=[], got %+v", integrations[0].Connections[0].AuthTypes)
		}
	})

	t.Run("manifest-backed passive default no-auth connection stays hidden", func(t *testing.T) {
		t.Parallel()

		stub := &stubNonOAuthProvider{
			name: "httpbin",
			ops:  []core.Operation{{Name: "get", Method: http.MethodGet}},
		}
		plugin := &config.ProviderEntry{
			Source: config.NewMetadataSource("https://example.invalid/github-com-acme-plugins-httpbin/v1.0.0/provider-release.yaml"),
			ResolvedManifest: &providermanifestv1.Manifest{
				Spec: &providermanifestv1.Spec{
					Surfaces: &providermanifestv1.ProviderSurfaces{
						REST: &providermanifestv1.RESTSurface{
							BaseURL: "https://httpbin.org",
						},
					},
					Connections: map[string]*providermanifestv1.ManifestConnectionDef{
						"default": {
							Mode: providermanifestv1.ConnectionModeNone,
						},
					},
				},
			},
		}

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, stub)
			cfg.PluginDefs = map[string]*config.ProviderEntry{
				"httpbin": plugin,
			}
			cfg.Services = coretesting.NewStubServices(t)
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var integrations []struct {
			Name        string   `json:"name"`
			AuthTypes   []string `json:"authTypes"`
			Connections []struct {
				Name string `json:"name"`
			} `json:"connections"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&integrations); err != nil {
			t.Fatalf("decoding: %v", err)
		}
		if len(integrations) != 1 {
			t.Fatalf("expected 1 integration, got %d", len(integrations))
		}
		if len(integrations[0].AuthTypes) != 0 {
			t.Fatalf("expected no top-level auth types, got %+v", integrations[0].AuthTypes)
		}
		if len(integrations[0].Connections) != 0 {
			t.Fatalf("expected passive default connection to stay hidden, got %+v", integrations[0].Connections)
		}
	})
}

func TestListIntegrations_ConnectionInfosHideOAuthConnectionsWithoutHandler(t *testing.T) {
	t.Parallel()

	stub := &coretesting.StubIntegration{N: "slack", DN: "Slack"}
	plugin := &config.ProviderEntry{
		Source: config.NewMetadataSource("https://example.invalid/github-com-acme-plugins-slack/v1.0.0/provider-release.yaml"),
		Connections: map[string]*config.ConnectionDef{
			"default": {},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.PluginDefs = map[string]*config.ProviderEntry{
			"slack": plugin,
		}
		cfg.ConnectionAuth = func() map[string]map[string]bootstrap.OAuthHandler {
			return map[string]map[string]bootstrap.OAuthHandler{
				"slack": {
					"default": &testOAuthHandler{authorizationBaseURLVal: "https://slack.com/oauth/v2/authorize"},
				},
			}
		}
		cfg.Services = coretesting.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var integrations []struct {
		Name        string `json:"name"`
		Connections []struct {
			Name      string   `json:"name"`
			AuthTypes []string `json:"authTypes"`
		} `json:"connections"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&integrations); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(integrations) != 1 {
		t.Fatalf("expected 1 integration, got %d", len(integrations))
	}
	if len(integrations[0].Connections) != 1 {
		t.Fatalf("expected 1 connection, got %+v", integrations[0].Connections)
	}
	if integrations[0].Connections[0].Name != "default" {
		t.Fatalf("expected only default connection, got %+v", integrations[0].Connections)
	}
	if !reflect.DeepEqual(integrations[0].Connections[0].AuthTypes, []string{"oauth"}) {
		t.Fatalf("expected default authTypes [oauth], got %+v", integrations[0].Connections[0].AuthTypes)
	}
}

func TestListIntegrations_ConnectionInfosIncludeProviderManualAuth(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		provider   func(t *testing.T) core.Provider
		plugin     *config.ProviderEntry
		wantAuth   []string
		wantFields []struct {
			Name  string `json:"name"`
			Label string `json:"label"`
		}
	}{
		{
			name: "explicit oauth2 auth",
			provider: func(t *testing.T) core.Provider {
				t.Helper()
				return &stubDualAuthProvider{
					StubIntegration: coretesting.StubIntegration{N: "example", DN: "Example"},
				}
			},
			plugin: &config.ProviderEntry{
				Auth: &config.ConnectionAuthDef{
					Type:             providermanifestv1.AuthTypeOAuth2,
					AuthorizationURL: "https://example.com/oauth/authorize",
					TokenURL:         "https://example.com/oauth/token",
				},
			},
			wantAuth: []string{"oauth"},
			wantFields: []struct {
				Name  string `json:"name"`
				Label string `json:"label"`
			}{},
		},
		{
			name: "empty auth type still exposes oauth",
			provider: func(t *testing.T) core.Provider {
				t.Helper()
				return &stubDualAuthProvider{
					StubIntegration: coretesting.StubIntegration{N: "example", DN: "Example"},
				}
			},
			plugin: &config.ProviderEntry{
				Auth: &config.ConnectionAuthDef{
					Type:             "",
					AuthorizationURL: "https://example.com/oauth/authorize",
					TokenURL:         "https://example.com/oauth/token",
				},
			},
			wantAuth: []string{"oauth", "manual"},
			wantFields: []struct {
				Name  string `json:"name"`
				Label string `json:"label"`
			}{
				{Name: "api_token", Label: "API Token"},
			},
		},
		{
			name: "plugin auth unset uses provider auth types",
			provider: func(t *testing.T) core.Provider {
				t.Helper()
				prov, err := provider.Build(&provider.Definition{
					Provider:    "example",
					DisplayName: "Example",
					Auth:        provider.AuthDef{Type: "manual"},
					CredentialFields: []provider.CredentialFieldDef{
						{Name: "primary_token", Label: "Primary Token"},
						{Name: "secondary_token", Label: "Secondary Token"},
					},
					Operations: map[string]provider.OperationDef{
						"list_items": {Method: http.MethodGet, Path: "/items"},
					},
				}, config.ConnectionDef{})
				if err != nil {
					t.Fatalf("Build: %v", err)
				}
				return prov
			},
			plugin:   &config.ProviderEntry{},
			wantAuth: []string{"manual"},
			wantFields: []struct {
				Name  string `json:"name"`
				Label string `json:"label"`
			}{
				{Name: "primary_token", Label: "Primary Token"},
				{Name: "secondary_token", Label: "Secondary Token"},
			},
		},
		{
			name: "declared manual credential fields are exposed without synthetic auth inputs",
			provider: func(t *testing.T) core.Provider {
				t.Helper()
				return &coretesting.StubIntegration{N: "example", DN: "Example"}
			},
			plugin: &config.ProviderEntry{
				Auth: &config.ConnectionAuthDef{
					Type: providermanifestv1.AuthTypeManual,
					Credentials: []config.CredentialFieldDef{
						{Name: "api_key", Label: "API Key"},
					},
					AuthMapping: &config.AuthMappingDef{
						Basic: &config.BasicAuthMappingDef{
							Username: config.AuthValueDef{
								Value: "org-123",
							},
							Password: config.AuthValueDef{
								ValueFrom: &config.AuthValueFromDef{
									CredentialFieldRef: &config.CredentialFieldRefDef{Name: "api_key"},
								},
							},
						},
					},
				},
			},
			wantAuth: []string{"manual"},
			wantFields: []struct {
				Name  string `json:"name"`
				Label string `json:"label"`
			}{
				{Name: "api_key", Label: "API Key"},
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ts := newTestServer(t, func(cfg *server.Config) {
				cfg.Providers = testutil.NewProviderRegistry(t, tc.provider(t))
				cfg.PluginDefs = map[string]*config.ProviderEntry{
					"example": tc.plugin,
				}
				cfg.Services = coretesting.NewStubServices(t)
			})
			testutil.CloseOnCleanup(t, ts)

			req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected 200, got %d", resp.StatusCode)
			}

			var integrations []struct {
				Name        string `json:"name"`
				Connections []struct {
					Name             string   `json:"name"`
					AuthTypes        []string `json:"authTypes"`
					CredentialFields []struct {
						Name  string `json:"name"`
						Label string `json:"label"`
					} `json:"credentialFields"`
				} `json:"connections"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&integrations); err != nil {
				t.Fatalf("decoding: %v", err)
			}
			if len(integrations) != 1 || len(integrations[0].Connections) != 1 {
				t.Fatalf("unexpected integrations response: %+v", integrations)
			}

			conn := integrations[0].Connections[0]
			if conn.Name != config.PluginConnectionAlias {
				t.Fatalf("expected plugin connection, got %+v", conn)
			}
			if !reflect.DeepEqual(conn.AuthTypes, tc.wantAuth) {
				t.Fatalf("auth types = %+v, want %+v", conn.AuthTypes, tc.wantAuth)
			}
			if !reflect.DeepEqual(conn.CredentialFields, tc.wantFields) {
				t.Fatalf("credential fields = %+v, want %+v", conn.CredentialFields, tc.wantFields)
			}
		})
	}
}

func TestListIntegrationsWithIcon(t *testing.T) {
	t.Parallel()

	const testSVG = `<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/></svg>`

	newIconProvider := func(t *testing.T) core.Provider {
		t.Helper()
		prov, err := provider.Build(&provider.Definition{
			Provider:    "iconprov",
			DisplayName: "Icon Provider",
			Description: "Has an icon",
			IconSVG:     testSVG,
			BaseURL:     "https://api.example.com",
			Auth:        provider.AuthDef{Type: "manual"},
			Operations: map[string]provider.OperationDef{
				"op": {Description: "An op", Method: http.MethodGet, Path: "/op"},
			},
		}, config.ConnectionDef{})
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		return prov
	}

	assertIcon := func(t *testing.T, prov core.Provider) {
		t.Helper()
		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, prov)
			cfg.Services = coretesting.NewStubServices(t)
		})
		defer ts.Close()

		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var integrations []struct {
			IconSVG string `json:"iconSvg"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&integrations); err != nil {
			t.Fatalf("decoding: %v", err)
		}
		if resp.StatusCode != http.StatusOK || len(integrations) != 1 {
			t.Fatalf("unexpected integrations response: status=%d body=%+v", resp.StatusCode, integrations)
		}
		if integrations[0].IconSVG != testSVG {
			t.Fatalf("icon_svg = %q, want %q", integrations[0].IconSVG, testSVG)
		}
	}

	assertIcon(t, newIconProvider(t))

	assertIcon(t, composite.New("iconprov", newIconProvider(t), &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "iconprov"},
		},
		catalog: &catalog.Catalog{
			Name:        "iconprov",
			DisplayName: "Icon Provider",
			Description: "Has an icon",
			Operations: []catalog.CatalogOperation{
				{ID: "search", Description: "Search via MCP", Transport: catalog.TransportMCPPassthrough},
			},
		},
	}))
}

func TestListIntegrations_ShowsConnectedStatus(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	u := seedLegacyUserRecord(t, svc, "user-a", "user@example.com", time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok1", SubjectID: principal.UserSubjectID(u.ID), Integration: "slack",
		Connection: "default", Instance: "default", AccessToken: "test-token",
	})

	stub := &coretesting.StubIntegration{N: "slack", DN: "Slack"}
	stub2 := &coretesting.StubIntegration{N: "github", DN: "GitHub"}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "session-token" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: "USER@example.com"}, nil
			},
		}
		cfg.Providers = testutil.NewProviderRegistry(t, stub, stub2)
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "session-token"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var integrations []struct {
		Name      string `json:"name"`
		Connected bool   `json:"connected"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&integrations); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(integrations) != 2 {
		t.Fatalf("expected 2 integrations, got %d", len(integrations))
	}

	connected := make(map[string]bool)
	for _, i := range integrations {
		connected[i.Name] = i.Connected
	}
	if !connected["slack"] {
		t.Fatal("expected slack to be connected")
	}
	if connected["github"] {
		t.Fatal("expected github to be disconnected")
	}
}

func TestListIntegrations_ShowsConnectedStatus_PrefersCanonicalLowercaseEmailOverExactRawDuplicate(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	seedLegacyUserRecord(t, svc, "user-a", "user@example.com", time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	userB := seedLegacyUserRecord(t, svc, "user-b", "USER@example.com", time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC))
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok1", SubjectID: principal.UserSubjectID(userB.ID), Integration: "slack",
		Connection: "default", Instance: "default", AccessToken: "test-token",
	})

	stub := &coretesting.StubIntegration{N: "slack", DN: "Slack"}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "session-token" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: "USER@example.com"}, nil
			},
		}
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "session-token"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var integrations []struct {
		Name      string `json:"name"`
		Connected bool   `json:"connected"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&integrations); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(integrations) != 1 {
		t.Fatalf("expected 1 integration, got %d", len(integrations))
	}
	if integrations[0].Connected {
		t.Fatal("expected canonical lowercase user to win over the exact raw-email duplicate")
	}
}

func TestListIntegrations_ShowsConnectedStatus_AmbiguousMixedCaseDuplicatesFailClosed(t *testing.T) {
	t.Parallel()

	for _, email := range []string{"user@example.com", "USER@example.com"} {
		email := email
		t.Run(email, func(t *testing.T) {
			t.Parallel()

			svc := coretesting.NewStubServices(t)
			seedLegacyUserRecord(t, svc, "user-a", "User@example.com", time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
			seedLegacyUserRecord(t, svc, "user-b", "USER@example.com", time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC))

			stub := &coretesting.StubIntegration{N: "slack", DN: "Slack"}
			ts := newTestServer(t, func(cfg *server.Config) {
				cfg.Auth = &coretesting.StubAuthProvider{
					N: "stub",
					ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
						if token != "session-token" {
							return nil, core.ErrNotFound
						}
						return &core.UserIdentity{Email: email}, nil
					},
				}
				cfg.Providers = testutil.NewProviderRegistry(t, stub)
				cfg.Services = svc
			})
			testutil.CloseOnCleanup(t, ts)

			req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
			req.AddCookie(&http.Cookie{Name: "session_token", Value: "session-token"})
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusInternalServerError {
				t.Fatalf("expected 500, got %d", resp.StatusCode)
			}
		})
	}
}

func TestLoginCallback_LegacyMixedCaseRepairFailureStillSucceeds(t *testing.T) {
	t.Parallel()

	svc, failPut := newTestServicesWithUsersPutFailure(t)
	existing := seedLegacyUserRecord(t, svc, "legacy-user", "User@Example.com", time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	failPut.Store(true)

	var auditBuf bytes.Buffer
	auditSink := invocation.NewSlogAuditSink(&auditBuf)
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &stubAuthWithToken{
			StubAuthProvider: coretesting.StubAuthProvider{
				N: "test",
				HandleCallbackFn: func(_ context.Context, code string) (*core.UserIdentity, error) {
					if code == "good-code" {
						return &core.UserIdentity{Email: "user@example.com", DisplayName: "User"}, nil
					}
					return nil, fmt.Errorf("bad code")
				},
			},
		}
		cfg.Services = svc
		cfg.AuditSink = auditSink
	})
	testutil.CloseOnCleanup(t, ts)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	body := bytes.NewBufferString(`{"state":"test-state"}`)
	loginResp, err := client.Post(ts.URL+"/api/v1/auth/login", "application/json", body)
	if err != nil {
		t.Fatalf("start login: %v", err)
	}
	_ = loginResp.Body.Close()

	resp, err := client.Get(ts.URL + "/api/v1/auth/login/callback?code=good-code&state=test-state")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	stored, err := svc.Users.GetUser(context.Background(), existing.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if stored.Email != "User@Example.com" {
		t.Fatalf("expected user email to remain unrepaired after forced put failure, got %q", stored.Email)
	}

	lines := bytes.Split(bytes.TrimSpace(auditBuf.Bytes()), []byte("\n"))
	if len(lines) == 0 {
		t.Fatal("expected login audit record")
	}
	var auditRecord map[string]any
	if err := json.Unmarshal(lines[len(lines)-1], &auditRecord); err != nil {
		t.Fatalf("parsing audit record: %v\nraw: %s", err, auditBuf.String())
	}
	if auditRecord["operation"] != "auth.login.complete" {
		t.Fatalf("expected audit operation auth.login.complete, got %v", auditRecord["operation"])
	}
	if subjectID, ok := auditRecord["subject_id"].(string); !ok || subjectID != principal.UserSubjectID(existing.ID) {
		t.Fatalf("expected audit subject_id %q, got %v", principal.UserSubjectID(existing.ID), auditRecord["subject_id"])
	}
	if _, ok := auditRecord["user_id"]; ok {
		t.Fatalf("expected emitted audit record to omit user_id, got %v", auditRecord["user_id"])
	}
}

func TestListIntegrations_FindOrCreateUserError(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	stubDB := svc.DB.(*coretesting.StubIndexedDB)

	stub := &coretesting.StubIntegration{N: "test-integ", DN: "Test"}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	stubDB.Err = fmt.Errorf("database unavailable")
	defer func() { stubDB.Err = nil }()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestListIntegrations_ListTokensError(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	stubDB := svc.DB.(*coretesting.StubIndexedDB)
	seedUser(t, svc, "anonymous@gestalt")

	stub := &coretesting.StubIntegration{N: "test-integ", DN: "Test"}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	stubDB.Err = fmt.Errorf("database unavailable")
	defer func() { stubDB.Err = nil }()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestDisconnectIntegration(t *testing.T) {
	t.Parallel()

	t.Run("default token", func(t *testing.T) {
		t.Parallel()

		svc := coretesting.NewStubServices(t)
		u := seedUser(t, svc, "anonymous@gestalt")
		externalIdentityID := testExternalIdentityResourceID("slack_identity", "team:T123:user:U456")
		authzProvider := newMemoryAuthorizationProvider("memory-authorization")
		legacy, err := authorization.New(config.AuthorizationConfig{}, map[string]*config.ProviderEntry{}, nil, nil)
		if err != nil {
			t.Fatalf("authorization.New: %v", err)
		}
		authz := mustProviderBackedAuthorizer(t, legacy, authzProvider)
		seedToken(t, svc, &core.IntegrationToken{
			ID: "tok-1", SubjectID: principal.UserSubjectID(u.ID), Integration: "slack",
			Connection: "", Instance: "default", AccessToken: "test-token",
			MetadataJSON: `{"team_id":"T123","user_id":"U456","gestalt.external_identity.type":"slack_identity","gestalt.external_identity.id":"team:T123:user:U456"}`,
		})
		modelID, err := authz.ManagedModelID(context.Background())
		if err != nil {
			t.Fatalf("ManagedModelID: %v", err)
		}
		if err := authzProvider.WriteRelationships(context.Background(), &core.WriteRelationshipsRequest{
			Writes: []*core.Relationship{{
				Subject:  &core.SubjectRef{Type: "user", Id: principal.UserSubjectID(u.ID)},
				Relation: authorization.ProviderExternalIdentityRelationAssume,
				Resource: &core.ResourceRef{
					Type: authorization.ProviderResourceTypeExternalIdentity,
					Id:   externalIdentityID,
				},
			}},
			ModelId: modelID,
		}); err != nil {
			t.Fatalf("WriteRelationships seed slack identity: %v", err)
		}

		stub := &coretesting.StubIntegration{N: "slack", DN: "Slack"}
		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, stub)
			cfg.Services = svc
			cfg.Authorizer = authz
			cfg.AuthorizationProvider = authzProvider
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/integrations/slack", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}
		tokens, err := svc.Tokens.ListTokensForIntegration(context.Background(), principal.UserSubjectID(u.ID), "slack")
		if err != nil {
			t.Fatalf("ListTokensForIntegration: %v", err)
		}
		if len(tokens) != 0 {
			t.Fatalf("expected 0 tokens after disconnect, got %d", len(tokens))
		}
		respAuthz, err := authzProvider.ReadRelationships(context.Background(), &core.ReadRelationshipsRequest{
			Relation: authorization.ProviderExternalIdentityRelationAssume,
			Resource: &core.ResourceRef{
				Type: authorization.ProviderResourceTypeExternalIdentity,
				Id:   externalIdentityID,
			},
		})
		if err != nil {
			t.Fatalf("ReadRelationships after disconnect: %v", err)
		}
		if len(respAuthz.GetRelationships()) != 0 {
			t.Fatalf("expected external identity relationship to be removed, got %+v", respAuthz.GetRelationships())
		}
	})

	t.Run("shared external identity remains linked while another token still exists", func(t *testing.T) {
		t.Parallel()

		svc := coretesting.NewStubServices(t)
		u := seedUser(t, svc, "anonymous@gestalt")
		externalIdentityID := testExternalIdentityResourceID("slack_identity", "team:T123:user:U456")
		authzProvider := newMemoryAuthorizationProvider("memory-authorization")
		legacy, err := authorization.New(config.AuthorizationConfig{}, map[string]*config.ProviderEntry{}, nil, nil)
		if err != nil {
			t.Fatalf("authorization.New: %v", err)
		}
		authz := mustProviderBackedAuthorizer(t, legacy, authzProvider)
		seedToken(t, svc, &core.IntegrationToken{
			ID: "tok-1", SubjectID: principal.UserSubjectID(u.ID), Integration: "slack",
			Connection: "workspace", Instance: "team-a", AccessToken: "test-token-a",
			MetadataJSON: `{"team_id":"T123","user_id":"U456","gestalt.external_identity.type":"slack_identity","gestalt.external_identity.id":"team:T123:user:U456"}`,
		})
		seedToken(t, svc, &core.IntegrationToken{
			ID: "tok-2", SubjectID: principal.UserSubjectID(u.ID), Integration: "slack",
			Connection: "workspace", Instance: "team-b", AccessToken: "test-token-b",
			MetadataJSON: `{"team_id":"T123","user_id":"U456","gestalt.external_identity.type":"slack_identity","gestalt.external_identity.id":"team:T123:user:U456"}`,
		})
		modelID, err := authz.ManagedModelID(context.Background())
		if err != nil {
			t.Fatalf("ManagedModelID: %v", err)
		}
		if err := authzProvider.WriteRelationships(context.Background(), &core.WriteRelationshipsRequest{
			Writes: []*core.Relationship{{
				Subject:  &core.SubjectRef{Type: "user", Id: principal.UserSubjectID(u.ID)},
				Relation: authorization.ProviderExternalIdentityRelationAssume,
				Resource: &core.ResourceRef{
					Type: authorization.ProviderResourceTypeExternalIdentity,
					Id:   externalIdentityID,
				},
			}},
			ModelId: modelID,
		}); err != nil {
			t.Fatalf("WriteRelationships seed slack identity: %v", err)
		}

		stub := &coretesting.StubIntegration{N: "slack", DN: "Slack"}
		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, stub)
			cfg.Services = svc
			cfg.Authorizer = authz
			cfg.AuthorizationProvider = authzProvider
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/integrations/slack?_connection=workspace&_instance=team-a", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}
		tokens, err := svc.Tokens.ListTokensForIntegration(context.Background(), principal.UserSubjectID(u.ID), "slack")
		if err != nil {
			t.Fatalf("ListTokensForIntegration: %v", err)
		}
		if len(tokens) != 1 {
			t.Fatalf("expected 1 token after disconnect, got %d", len(tokens))
		}
		if tokens[0].Connection != "workspace" || tokens[0].Instance != "team-b" {
			t.Fatalf("unexpected remaining token %+v", tokens[0])
		}
		respAuthz, err := authzProvider.ReadRelationships(context.Background(), &core.ReadRelationshipsRequest{
			Relation: authorization.ProviderExternalIdentityRelationAssume,
			Resource: &core.ResourceRef{
				Type: authorization.ProviderResourceTypeExternalIdentity,
				Id:   externalIdentityID,
			},
		})
		if err != nil {
			t.Fatalf("ReadRelationships after partial disconnect: %v", err)
		}
		if len(respAuthz.GetRelationships()) != 1 {
			t.Fatalf("expected external identity relationship to remain, got %+v", respAuthz.GetRelationships())
		}
	})

	t.Run("disconnect restores token when authz unlink fails", func(t *testing.T) {
		t.Parallel()

		svc := coretesting.NewStubServices(t)
		u := seedUser(t, svc, "anonymous@gestalt")
		externalIdentityID := testExternalIdentityResourceID("slack_identity", "team:T123:user:U456")
		authzProvider := newMemoryAuthorizationProvider("memory-authorization")
		legacy, err := authorization.New(config.AuthorizationConfig{}, map[string]*config.ProviderEntry{}, nil, nil)
		if err != nil {
			t.Fatalf("authorization.New: %v", err)
		}
		authz := mustProviderBackedAuthorizer(t, legacy, authzProvider)
		seedToken(t, svc, &core.IntegrationToken{
			ID: "tok-1", SubjectID: principal.UserSubjectID(u.ID), Integration: "slack",
			Connection: "", Instance: "default", AccessToken: "test-token",
			MetadataJSON: `{"team_id":"T123","user_id":"U456","gestalt.external_identity.type":"slack_identity","gestalt.external_identity.id":"team:T123:user:U456"}`,
		})
		modelID, err := authz.ManagedModelID(context.Background())
		if err != nil {
			t.Fatalf("ManagedModelID: %v", err)
		}
		if err := authzProvider.WriteRelationships(context.Background(), &core.WriteRelationshipsRequest{
			Writes: []*core.Relationship{{
				Subject:  &core.SubjectRef{Type: "user", Id: principal.UserSubjectID(u.ID)},
				Relation: authorization.ProviderExternalIdentityRelationAssume,
				Resource: &core.ResourceRef{
					Type: authorization.ProviderResourceTypeExternalIdentity,
					Id:   externalIdentityID,
				},
			}},
			ModelId: modelID,
		}); err != nil {
			t.Fatalf("WriteRelationships seed slack identity: %v", err)
		}
		originalTokens, err := svc.Tokens.ListTokensForIntegration(context.Background(), principal.UserSubjectID(u.ID), "slack")
		if err != nil {
			t.Fatalf("ListTokensForIntegration before disconnect: %v", err)
		}
		if len(originalTokens) != 1 {
			t.Fatalf("expected 1 token before disconnect, got %d", len(originalTokens))
		}
		originalCreatedAt := originalTokens[0].CreatedAt
		originalUpdatedAt := originalTokens[0].UpdatedAt
		authzProvider.writeErr = errors.New("unlink failed")

		stub := &coretesting.StubIntegration{N: "slack", DN: "Slack"}
		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, stub)
			cfg.Services = svc
			cfg.Authorizer = authz
			cfg.AuthorizationProvider = authzProvider
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/integrations/slack", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusInternalServerError {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 500, got %d: %s", resp.StatusCode, body)
		}
		tokens, err := svc.Tokens.ListTokensForIntegration(context.Background(), principal.UserSubjectID(u.ID), "slack")
		if err != nil {
			t.Fatalf("ListTokensForIntegration: %v", err)
		}
		if len(tokens) != 1 || tokens[0].ID != "tok-1" {
			t.Fatalf("expected token rollback after unlink failure, got %+v", tokens)
		}
		if !tokens[0].CreatedAt.Equal(originalCreatedAt) {
			t.Fatalf("expected rollback to preserve created_at %v, got %v", originalCreatedAt, tokens[0].CreatedAt)
		}
		if !tokens[0].UpdatedAt.Equal(originalUpdatedAt) {
			t.Fatalf("expected rollback to preserve updated_at %v, got %v", originalUpdatedAt, tokens[0].UpdatedAt)
		}
		respAuthz, err := authzProvider.ReadRelationships(context.Background(), &core.ReadRelationshipsRequest{
			Relation: authorization.ProviderExternalIdentityRelationAssume,
			Resource: &core.ResourceRef{
				Type: authorization.ProviderResourceTypeExternalIdentity,
				Id:   externalIdentityID,
			},
		})
		if err != nil {
			t.Fatalf("ReadRelationships after rollback: %v", err)
		}
		if len(respAuthz.GetRelationships()) != 1 {
			t.Fatalf("expected external identity relationship to remain after rollback, got %+v", respAuthz.GetRelationships())
		}
	})

	t.Run("bare disconnect remains ambiguous when multiple connections exist", func(t *testing.T) {
		t.Parallel()

		svc := coretesting.NewStubServices(t)
		u := seedUser(t, svc, "anonymous@gestalt")
		seedToken(t, svc, &core.IntegrationToken{
			ID: "tok-a", SubjectID: principal.UserSubjectID(u.ID), Integration: "notion",
			Connection: "mcp", Instance: "MCP OAuth", AccessToken: "test-token",
		})
		seedToken(t, svc, &core.IntegrationToken{
			ID: "tok-b", SubjectID: principal.UserSubjectID(u.ID), Integration: "notion",
			Connection: "default", Instance: "default", AccessToken: "test-token-2",
		})

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{N: "notion", DN: "Notion"})
			cfg.Services = svc
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/integrations/notion", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusConflict {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 409, got %d: %s", resp.StatusCode, body)
		}
	})

	t.Run("underscored parameters", func(t *testing.T) {
		t.Parallel()

		svc := coretesting.NewStubServices(t)
		u := seedUser(t, svc, "anonymous@gestalt")
		seedToken(t, svc, &core.IntegrationToken{
			ID: "tok-b", SubjectID: principal.UserSubjectID(u.ID), Integration: "slack",
			Connection: "workspace", Instance: "team-b", AccessToken: "test-token",
		})

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{N: "slack", DN: "Slack"})
			cfg.Services = svc
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/integrations/slack?_connection=workspace&_instance=team-b", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}
	})

	t.Run("plain parameters are accepted for disconnect", func(t *testing.T) {
		t.Parallel()

		svc := coretesting.NewStubServices(t)
		u := seedUser(t, svc, "anonymous@gestalt")
		var auditBuf bytes.Buffer
		seedToken(t, svc, &core.IntegrationToken{
			ID: "tok-b", SubjectID: principal.UserSubjectID(u.ID), Integration: "notion",
			Connection: "mcp", Instance: "MCP OAuth", AccessToken: "test-token",
		})
		seedToken(t, svc, &core.IntegrationToken{
			ID: "tok-c", SubjectID: principal.UserSubjectID(u.ID), Integration: "notion",
			Connection: "default", Instance: "default", AccessToken: "test-token-2",
		})

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{N: "notion", DN: "Notion"})
			cfg.AuditSink = invocation.NewSlogAuditSink(&auditBuf)
			cfg.Services = svc
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/integrations/notion?connection=mcp&instance=MCP%20OAuth", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}
		tokens, err := svc.Tokens.ListTokensForIntegration(context.Background(), principal.UserSubjectID(u.ID), "notion")
		if err != nil {
			t.Fatalf("ListTokensForIntegration: %v", err)
		}
		if len(tokens) != 1 {
			t.Fatalf("expected 1 token after targeted disconnect, got %d", len(tokens))
		}
		if tokens[0].Connection != "default" || tokens[0].Instance != "default" {
			t.Fatalf("unexpected remaining token %+v", tokens[0])
		}

		var auditRecord map[string]any
		if err := json.Unmarshal(auditBuf.Bytes(), &auditRecord); err != nil {
			t.Fatalf("parsing audit record: %v\nraw: %s", err, auditBuf.String())
		}
		if auditRecord["target_kind"] != "connection" {
			t.Fatalf("expected audit target_kind connection, got %v", auditRecord["target_kind"])
		}
		if auditRecord["target_id"] != "notion/mcp/MCP OAuth" {
			t.Fatalf("expected audit target_id notion/mcp/MCP OAuth, got %v", auditRecord["target_id"])
		}
		if auditRecord["target_name"] != "mcp/MCP OAuth" {
			t.Fatalf("expected audit target_name mcp/MCP OAuth, got %v", auditRecord["target_name"])
		}
	})

	t.Run("ambiguous error uses canonical hint", func(t *testing.T) {
		t.Parallel()

		svc := coretesting.NewStubServices(t)
		u := seedUser(t, svc, "anonymous@gestalt")
		var auditBuf bytes.Buffer
		seedToken(t, svc, &core.IntegrationToken{
			ID: "tok-a", SubjectID: principal.UserSubjectID(u.ID), Integration: "slack",
			Connection: "workspace", Instance: "team-a", AccessToken: "test-token",
		})
		seedToken(t, svc, &core.IntegrationToken{
			ID: "tok-b", SubjectID: principal.UserSubjectID(u.ID), Integration: "slack",
			Connection: "workspace", Instance: "team-b", AccessToken: "test-token-2",
		})

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{N: "slack", DN: "Slack"})
			cfg.AuditSink = invocation.NewSlogAuditSink(&auditBuf)
			cfg.Services = svc
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/integrations/slack?_connection=workspace", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusConflict {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 409, got %d: %s", resp.StatusCode, body)
		}
		var result map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decoding: %v", err)
		}
		if !strings.Contains(result["error"], "?_instance=NAME") {
			t.Fatalf("expected canonical parameter hint, got %q", result["error"])
		}

		var auditRecord map[string]any
		if err := json.Unmarshal(auditBuf.Bytes(), &auditRecord); err != nil {
			t.Fatalf("parsing audit record: %v\nraw: %s", err, auditBuf.String())
		}
		if auditRecord["target_kind"] != nil {
			t.Fatalf("expected no audit target_kind for ambiguous disconnect, got %v", auditRecord["target_kind"])
		}
		if auditRecord["target_id"] != nil {
			t.Fatalf("expected no audit target_id for ambiguous disconnect, got %v", auditRecord["target_id"])
		}
		if auditRecord["target_name"] != nil {
			t.Fatalf("expected no audit target_name for ambiguous disconnect, got %v", auditRecord["target_name"])
		}
	})
}

func TestDisconnectIntegration_NotConnected(t *testing.T) {
	t.Parallel()

	stub := &coretesting.StubIntegration{N: "slack", DN: "Slack"}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Services = coretesting.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/integrations/slack", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	if resp.StatusCode != http.StatusNotFound {
		_ = resp.Body.Close()
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

func TestListOperations(t *testing.T) {
	t.Parallel()

	stub := &stubIntegrationWithCatalog{
		StubIntegration: coretesting.StubIntegration{N: "test-int"},
		catalog: &catalog.Catalog{
			Name: "test-int",
			Operations: []catalog.CatalogOperation{
				{
					ID:          "archive_comment",
					Description: "Archive a comment",
					Method:      http.MethodPost,
				},
				{
					ID:          "save_comment",
					Description: "Create or update a comment",
					Method:      http.MethodPost,
					InputSchema: json.RawMessage(`{
						"type":"object",
						"properties":{
							"body":{"type":"string"},
							"displayObject":{"type":"object{title!, teamId!}"},
							"issueId":{"type":"string"}
							,"notActuallyBoolean":{"type":"booleans"}
						},
						"required":["body","displayObject","issueId"]
					}`),
				},
			},
		},
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Services = coretesting.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations/test-int/operations", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var ops []struct {
		ID         string `json:"id"`
		Parameters []struct {
			Name     string `json:"name"`
			Type     string `json:"type"`
			Required bool   `json:"required"`
		} `json:"parameters"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ops); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("decoding response: %v", err)
	}
	_ = resp.Body.Close()
	if len(ops) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(ops))
	}
	if ops[0].ID != "archive_comment" {
		t.Fatalf("expected archive_comment first, got %+v", ops)
	}
	if ops[1].ID != "save_comment" {
		t.Fatalf("expected save_comment second, got %+v", ops)
	}
	if len(ops[1].Parameters) != 4 {
		t.Fatalf("save_comment parameters = %+v, want 4", ops[1].Parameters)
	}
	paramsByName := make(map[string]struct {
		Name     string `json:"name"`
		Type     string `json:"type"`
		Required bool   `json:"required"`
	}, len(ops[1].Parameters))
	for _, param := range ops[1].Parameters {
		paramsByName[param.Name] = param
	}
	if got := paramsByName["body"]; got.Type != "string" || !got.Required {
		t.Fatalf("body param = %+v", got)
	}
	if got := paramsByName["displayObject"]; got.Type != "object" || !got.Required {
		t.Fatalf("displayObject param = %+v", got)
	}
	if got := paramsByName["issueId"]; got.Type != "string" || !got.Required {
		t.Fatalf("issueId param = %+v", got)
	}
	if got := paramsByName["notActuallyBoolean"]; got.Type != "string" || got.Required {
		t.Fatalf("notActuallyBoolean param = %+v", got)
	}
}

func TestListOperations_UsesCatalogConnectionOverride(t *testing.T) {
	t.Parallel()

	const (
		altCatalogConnection = "catalog-alt"
		altInstance          = "team-b"
		altCatalogToken      = "tok-catalog-alt"
	)

	stub := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "test-int", ConnMode: core.ConnectionModeUser},
		},
		catalog: &catalog.Catalog{
			Name: "test-int",
			Operations: []catalog.CatalogOperation{
				{ID: "zeta_rest", Description: "REST op", Method: http.MethodGet, Transport: catalog.TransportREST},
			},
		},
		catalogForRequestFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			switch token {
			case testCatalogToken:
				return &catalog.Catalog{
					Name: "test-int",
					Operations: []catalog.CatalogOperation{
						{ID: "alpha_mcp", Description: "Session-only MCP op", Method: http.MethodPost, Transport: catalog.TransportMCPPassthrough},
						{ID: "alpha_rest", Description: "Session-only REST op", Method: http.MethodPost, Transport: catalog.TransportREST},
					},
				}, nil
			case altCatalogToken:
				return &catalog.Catalog{
					Name: "test-int",
					Operations: []catalog.CatalogOperation{
						{ID: "beta_mcp_alt", Description: "Session-only alt MCP op", Method: http.MethodPost, Transport: catalog.TransportMCPPassthrough},
						{ID: "beta_rest_alt", Description: "Session-only alt REST op", Method: http.MethodPost, Transport: catalog.TransportREST},
					},
				}, nil
			default:
				return nil, fmt.Errorf("unexpected token %q", token)
			}
		},
	}

	svc := coretesting.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok-cat", SubjectID: principal.UserSubjectID(u.ID), Integration: "test-int",
		Connection: testCatalogConnection, Instance: "default", AccessToken: testCatalogToken,
	})
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok-cat-alt", SubjectID: principal.UserSubjectID(u.ID), Integration: "test-int",
		Connection: altCatalogConnection, Instance: altInstance, AccessToken: altCatalogToken,
	})

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"test-int": testDefaultConnection}
		cfg.CatalogConnection = map[string]string{"test-int": testCatalogConnection}
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations/test-int/operations", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var ops []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&ops); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(ops) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(ops))
	}
	if ops[0]["id"] != "alpha_rest" {
		t.Fatalf("expected first id 'alpha_rest', got %v", ops[0]["id"])
	}
	if ops[1]["id"] != "zeta_rest" {
		t.Fatalf("expected second id 'zeta_rest', got %v", ops[1]["id"])
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations/test-int/operations?_connection="+altCatalogConnection+"&_instance="+altInstance, nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("override list request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("override list: expected 200, got %d: %s", resp.StatusCode, respBody)
	}
	ops = nil
	if err := json.NewDecoder(resp.Body).Decode(&ops); err != nil {
		t.Fatalf("decoding override response: %v", err)
	}
	if len(ops) != 2 {
		t.Fatalf("expected 2 override operations, got %d", len(ops))
	}
	if ops[0]["id"] != "beta_rest_alt" {
		t.Fatalf("expected first id 'beta_rest_alt', got %v", ops[0]["id"])
	}
	if ops[1]["id"] != "zeta_rest" {
		t.Fatalf("expected second id 'zeta_rest', got %v", ops[1]["id"])
	}
	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations/test-int/operations?connection="+altCatalogConnection+"&instance="+altInstance, nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("legacy override list request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("legacy override list: expected 200, got %d: %s", resp.StatusCode, respBody)
	}
	ops = nil
	if err := json.NewDecoder(resp.Body).Decode(&ops); err != nil {
		t.Fatalf("decoding legacy override response: %v", err)
	}
	if len(ops) != 2 {
		t.Fatalf("expected 2 legacy override operations, got %d", len(ops))
	}
	if ops[0]["id"] != "alpha_rest" {
		t.Fatalf("expected first id 'alpha_rest' for legacy override, got %v", ops[0]["id"])
	}
	if ops[1]["id"] != "zeta_rest" {
		t.Fatalf("expected second id 'zeta_rest' for legacy override, got %v", ops[1]["id"])
	}
}

func TestListOperations_FallsBackToStaticCatalogWhenSessionCatalogErrors(t *testing.T) {
	t.Parallel()

	stub := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "notion", ConnMode: core.ConnectionModeUser},
		},
		catalog: &catalog.Catalog{
			Name: "notion",
			Operations: []catalog.CatalogOperation{
				{ID: "get_page", Description: "Get page", Method: http.MethodGet, Transport: catalog.TransportREST},
				{ID: "search", Description: "Search pages", Method: http.MethodPost, Transport: catalog.TransportREST},
			},
		},
		catalogForRequestFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			switch token {
			case "mcp-token", "oauth-token":
				return nil, fmt.Errorf("upstream catalog failed for %s", token)
			default:
				return nil, fmt.Errorf("unexpected token %q", token)
			}
		},
	}

	svc := coretesting.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok-mcp", SubjectID: principal.UserSubjectID(u.ID), Integration: "notion",
		Connection: "MCP", Instance: "default", AccessToken: "mcp-token",
	})
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok-oauth", SubjectID: principal.UserSubjectID(u.ID), Integration: "notion",
		Connection: "OAuth", Instance: "OAuth", AccessToken: "oauth-token",
	})

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.CatalogConnection = map[string]string{"notion": "MCP"}
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	assertListOperations := func(path string) {
		t.Helper()

		req, _ := http.NewRequest(http.MethodGet, ts.URL+path, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request %s: %v", path, err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("%s: expected 200, got %d: %s", path, resp.StatusCode, body)
		}

		var ops []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&ops); err != nil {
			t.Fatalf("%s: decoding response: %v", path, err)
		}
		if len(ops) != 2 {
			t.Fatalf("%s: expected 2 operations, got %d", path, len(ops))
		}
		if ops[0]["id"] != "get_page" {
			t.Fatalf("%s: expected first id 'get_page', got %v", path, ops[0]["id"])
		}
		if ops[1]["id"] != "search" {
			t.Fatalf("%s: expected second id 'search', got %v", path, ops[1]["id"])
		}
	}

	assertListOperations("/api/v1/integrations/notion/operations")
	assertListOperations("/api/v1/integrations/notion/operations?_connection=OAuth&_instance=OAuth")
}

func TestListOperations_UsesBrokerCatalogConnectionFallback(t *testing.T) {
	t.Parallel()

	stub := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "sample-int", ConnMode: core.ConnectionModeUser},
		},
		catalogForRequestFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			switch token {
			case "catalog-token":
				return &catalog.Catalog{
					Name: "sample-int",
					Operations: []catalog.CatalogOperation{
						{ID: "run", Description: "Run", Method: http.MethodGet, Transport: catalog.TransportREST},
					},
				}, nil
			case "rest-token":
				return &catalog.Catalog{Name: "sample-int"}, nil
			default:
				return nil, fmt.Errorf("unexpected token %q", token)
			}
		},
	}

	providers := testutil.NewProviderRegistry(t, stub)
	svc := coretesting.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok-catalog", SubjectID: principal.UserSubjectID(u.ID), Integration: "sample-int",
		Connection: "catalog-conn", Instance: "default", AccessToken: "catalog-token",
	})
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok-rest", SubjectID: principal.UserSubjectID(u.ID), Integration: "sample-int",
		Connection: "rest-conn", Instance: "default", AccessToken: "rest-token",
	})

	broker := invocation.NewBroker(
		providers,
		svc.Users,
		svc.Tokens,
		invocation.WithConnectionMapper(invocation.ConnectionMap(map[string]string{"sample-int": "rest-conn"})),
		invocation.WithMCPConnectionMapper(invocation.ConnectionMap(map[string]string{"sample-int": "catalog-conn"})),
	)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = providers
		cfg.Services = svc
		cfg.Invoker = broker
		cfg.DefaultConnection = map[string]string{"sample-int": "rest-conn"}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations/sample-int/operations", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var ops []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&ops); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(ops) != 1 || ops[0]["id"] != "run" {
		t.Fatalf("operations = %+v, want only run", ops)
	}
}

func TestListOperations_RetriesDefaultConnectionAfterBrokerCatalogError(t *testing.T) {
	t.Parallel()

	stub := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "sample-int", ConnMode: core.ConnectionModeUser},
		},
		catalogForRequestFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			switch token {
			case "rest-token":
				return &catalog.Catalog{
					Name: "sample-int",
					Operations: []catalog.CatalogOperation{
						{ID: "run", Description: "Run", Method: http.MethodGet, Transport: catalog.TransportREST},
					},
				}, nil
			default:
				return nil, fmt.Errorf("unexpected token %q", token)
			}
		},
	}

	providers := testutil.NewProviderRegistry(t, stub)
	svc := coretesting.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok-rest", SubjectID: principal.UserSubjectID(u.ID), Integration: "sample-int",
		Connection: "rest-conn", Instance: "default", AccessToken: "rest-token",
	})

	broker := invocation.NewBroker(
		providers,
		svc.Users,
		svc.Tokens,
		invocation.WithConnectionMapper(invocation.ConnectionMap(map[string]string{"sample-int": "rest-conn"})),
		invocation.WithMCPConnectionMapper(invocation.ConnectionMap(map[string]string{"sample-int": "mcp-conn"})),
	)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = providers
		cfg.Services = svc
		cfg.Invoker = broker
		cfg.DefaultConnection = map[string]string{"sample-int": "rest-conn"}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations/sample-int/operations", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var ops []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&ops); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(ops) != 1 || ops[0]["id"] != "run" {
		t.Fatalf("operations = %+v, want only run", ops)
	}
}

func TestListOperations_HumanAuthorizationFiltersMergedCatalog(t *testing.T) {
	t.Parallel()

	plaintext, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	svc := coretesting.NewStubServices(t)
	seedAPIToken(t, svc, plaintext, hashed, "viewer-user")
	viewer := seedUser(t, svc, "viewer-user@test.local")
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok-cat-human", SubjectID: principal.UserSubjectID(viewer.ID), Integration: "test-int",
		Connection: testCatalogConnection, Instance: "default", AccessToken: testCatalogToken,
	})

	var gotAccess invocation.AccessContext
	stub := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "test-int", ConnMode: core.ConnectionModeUser},
		},
		catalog: &catalog.Catalog{
			Name: "test-int",
			Operations: []catalog.CatalogOperation{
				{ID: "public_static", Description: "Visible to anyone with app access", Method: http.MethodGet, Transport: catalog.TransportREST},
				{ID: "admin_static", Description: "Admin only", Method: http.MethodGet, Transport: catalog.TransportREST, AllowedRoles: []string{"admin"}},
			},
		},
		catalogForRequestFn: func(ctx context.Context, token string) (*catalog.Catalog, error) {
			if token != testCatalogToken {
				return nil, fmt.Errorf("unexpected token %q", token)
			}
			gotAccess = invocation.AccessContextFromContext(ctx)
			return &catalog.Catalog{
				Name: "test-int",
				Operations: []catalog.CatalogOperation{
					{ID: "viewer_session", Description: "Viewer session op", Method: http.MethodPost, Transport: catalog.TransportREST, AllowedRoles: []string{"viewer"}},
					{ID: "admin_session", Description: "Admin session op", Method: http.MethodPost, Transport: catalog.TransportREST, AllowedRoles: []string{"admin"}},
				},
			}, nil
		},
	}

	pluginDefs := map[string]*config.ProviderEntry{
		"test-int": {AuthorizationPolicy: "sample_policy"},
	}
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(viewer.ID), Role: "viewer"},
				},
			},
		},
	}, nil, pluginDefs, nil)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.CatalogConnection = map[string]string{"test-int": testCatalogConnection}
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.PluginDefs = pluginDefs
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations/test-int/operations", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var ops []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&ops); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(ops) != 1 {
		t.Fatalf("expected 1 visible operation, got %d", len(ops))
	}
	if ops[0]["id"] != "viewer_session" {
		t.Fatalf("unexpected filtered operations: %+v", ops)
	}
	if gotAccess.Policy != "sample_policy" || gotAccess.Role != "viewer" {
		t.Fatalf("unexpected access context propagated to session catalog: %+v", gotAccess)
	}
}

func TestListOperations_HumanAuthorizationFiltersMergedCatalog_DynamicGrant(t *testing.T) {
	t.Parallel()

	plaintext, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	svc := coretesting.NewStubServices(t)
	seedAPIToken(t, svc, plaintext, hashed, "viewer-user")
	viewer := seedUser(t, svc, "viewer-user@test.local")
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok-cat-human", SubjectID: principal.UserSubjectID(viewer.ID), Integration: "test-int",
		Connection: testCatalogConnection, Instance: "default", AccessToken: testCatalogToken,
	})

	var gotAccess invocation.AccessContext
	stub := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "test-int", ConnMode: core.ConnectionModeUser},
		},
		catalog: &catalog.Catalog{
			Name: "test-int",
			Operations: []catalog.CatalogOperation{
				{ID: "public_static", Description: "Visible to anyone with app access", Method: http.MethodGet, Transport: catalog.TransportREST},
				{ID: "admin_static", Description: "Admin only", Method: http.MethodGet, Transport: catalog.TransportREST, AllowedRoles: []string{"admin"}},
			},
		},
		catalogForRequestFn: func(ctx context.Context, token string) (*catalog.Catalog, error) {
			if token != testCatalogToken {
				return nil, fmt.Errorf("unexpected token %q", token)
			}
			gotAccess = invocation.AccessContextFromContext(ctx)
			return &catalog.Catalog{
				Name: "test-int",
				Operations: []catalog.CatalogOperation{
					{ID: "viewer_session", Description: "Viewer session op", Method: http.MethodPost, Transport: catalog.TransportREST, AllowedRoles: []string{"viewer"}},
					{ID: "admin_session", Description: "Admin session op", Method: http.MethodPost, Transport: catalog.TransportREST, AllowedRoles: []string{"admin"}},
				},
			}, nil
		},
	}

	pluginDefs := map[string]*config.ProviderEntry{
		"test-int": {AuthorizationPolicy: "sample_policy"},
	}
	legacy, err := authorization.New(config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {Default: "deny"},
		},
	}, pluginDefs, nil, nil)
	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}
	provider := newMemoryAuthorizationProvider("memory-authorization")
	authz := mustProviderBackedAuthorizer(t, legacy, provider)
	seedProviderPluginAuthorization(t, svc, authz, provider, "test-int", "viewer-user@test.local", "viewer")

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.CatalogConnection = map[string]string{"test-int": testCatalogConnection}
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.PluginDefs = pluginDefs
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations/test-int/operations", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var ops []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&ops); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(ops) != 1 || ops[0]["id"] != "viewer_session" {
		t.Fatalf("unexpected filtered operations: %+v", ops)
	}
	if gotAccess.Policy != "sample_policy" || gotAccess.Role != "viewer" {
		t.Fatalf("unexpected access context propagated to session catalog: %+v", gotAccess)
	}
}

func TestExecuteOperation_HumanAuthorizationUsesCatalogRoles(t *testing.T) {
	t.Parallel()

	plaintext, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	svc := coretesting.NewStubServices(t)
	seedAPIToken(t, svc, plaintext, hashed, "viewer-user")
	viewer := seedUser(t, svc, "viewer-user@test.local")
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok-exec-human", SubjectID: principal.UserSubjectID(viewer.ID), Integration: "test-int",
		Connection: testDefaultConnection, Instance: "default", AccessToken: "exec-token",
	})

	var called bool
	stub := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N:        "test-int",
				ConnMode: core.ConnectionModeUser,
				ExecuteFn: func(_ context.Context, operation string, _ map[string]any, _ string) (*core.OperationResult, error) {
					called = true
					return &core.OperationResult{Status: http.StatusOK, Body: operation}, nil
				},
			},
		},
		catalog: &catalog.Catalog{
			Name: "test-int",
			Operations: []catalog.CatalogOperation{
				{ID: "public_static", Description: "Visible only when explicitly allowed", Method: http.MethodGet, Transport: catalog.TransportREST},
				{ID: "viewer_static", Description: "Viewer only", Method: http.MethodGet, Transport: catalog.TransportREST, AllowedRoles: []string{"viewer"}},
			},
		},
	}

	pluginDefs := map[string]*config.ProviderEntry{
		"test-int": {AuthorizationPolicy: "sample_policy"},
	}
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(viewer.ID), Role: "viewer"},
				},
			},
		},
	}, nil, pluginDefs, nil)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"test-int": testDefaultConnection}
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.PluginDefs = pluginDefs
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/test-int/public_static", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request denied op: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("expected 403 for unannotated operation, got %d: %s", resp.StatusCode, body)
	}
	_ = resp.Body.Close()
	if called {
		t.Fatal("expected denied operation to stop before provider execution")
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/v1/test-int/viewer_static", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request allowed op: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("expected 200 for viewer operation, got %d: %s", resp.StatusCode, body)
	}
	_ = resp.Body.Close()
	if !called {
		t.Fatal("expected allowed operation to reach provider execution")
	}
}

func TestExecuteOperation_HumanAuthorizationUsesSessionMetadataOnCollision(t *testing.T) {
	t.Parallel()

	plaintext, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	svc := coretesting.NewStubServices(t)
	seedAPIToken(t, svc, plaintext, hashed, "viewer-user")
	viewer := seedUser(t, svc, "viewer-user@test.local")
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok-session-collision", SubjectID: principal.UserSubjectID(viewer.ID), Integration: "sample-int",
		Connection: testDefaultConnection, Instance: "default", AccessToken: "session-token",
	})

	var called bool
	stub := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N:        "sample-int",
				ConnMode: core.ConnectionModeUser,
				ExecuteFn: func(_ context.Context, operation string, _ map[string]any, _ string) (*core.OperationResult, error) {
					called = true
					return &core.OperationResult{Status: http.StatusOK, Body: operation}, nil
				},
			},
		},
		catalog: &catalog.Catalog{
			Name: "sample-int",
			Operations: []catalog.CatalogOperation{
				{ID: "run", Description: "Static viewer op", Method: http.MethodGet, Transport: catalog.TransportREST, AllowedRoles: []string{"viewer"}},
			},
		},
		catalogForRequestFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			if token != "session-token" {
				t.Fatalf("token = %q, want %q", token, "session-token")
			}
			return &catalog.Catalog{
				Name: "sample-int",
				Operations: []catalog.CatalogOperation{
					{ID: "run", Description: "Session admin op", Method: http.MethodGet, Transport: catalog.TransportREST, AllowedRoles: []string{"admin"}},
				},
			}, nil
		},
	}

	pluginDefs := map[string]*config.ProviderEntry{
		"sample-int": {AuthorizationPolicy: "sample_policy"},
	}
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(viewer.ID), Role: "viewer"},
				},
			},
		},
	}, nil, pluginDefs, nil)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"sample-int": testDefaultConnection}
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.PluginDefs = pluginDefs
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/sample-int/run?_connection="+testDefaultConnection, nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 403, got %d: %s", resp.StatusCode, body)
	}
	if called {
		t.Fatal("expected session-side collision metadata to stop provider execution")
	}
}

func TestListOperations_NotFound(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, func(cfg *server.Config) {
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations/nonexistent/operations", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestListOperations_TokenSelectionErrors(t *testing.T) {
	t.Parallel()

	t.Run("no_token", func(t *testing.T) {
		t.Parallel()

		stub := &stubIntegrationWithSessionCatalog{
			stubIntegrationWithOps: stubIntegrationWithOps{
				StubIntegration: coretesting.StubIntegration{N: "test-int", ConnMode: core.ConnectionModeUser},
			},
		}

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, stub)
			cfg.CatalogConnection = map[string]string{"test-int": testCatalogConnection}
			cfg.Services = coretesting.NewStubServices(t)
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations/test-int/operations", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusPreconditionFailed {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 412, got %d: %s", resp.StatusCode, body)
		}

		var errResp struct {
			Error       string `json:"error"`
			Code        string `json:"code"`
			Integration string `json:"integration"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
			t.Fatalf("decoding error response: %v", err)
		}
		if errResp.Error != `no token stored for integration "test-int"; connect via OAuth first` {
			t.Fatalf("expected no-token message, got %q", errResp.Error)
		}
		if errResp.Code != "not_connected" {
			t.Fatalf("expected not_connected code, got %q", errResp.Code)
		}
		if errResp.Integration != "test-int" {
			t.Fatalf("expected integration test-int, got %q", errResp.Integration)
		}
	})

	t.Run("ambiguous_instance", func(t *testing.T) {
		t.Parallel()

		svc := coretesting.NewStubServices(t)
		u := seedUser(t, svc, "anonymous@gestalt")
		seedToken(t, svc, &core.IntegrationToken{
			ID: "tok-a", SubjectID: principal.UserSubjectID(u.ID), Integration: "test-int",
			Connection: testCatalogConnection, Instance: "inst-a", AccessToken: "tok-a",
		})
		seedToken(t, svc, &core.IntegrationToken{
			ID: "tok-b", SubjectID: principal.UserSubjectID(u.ID), Integration: "test-int",
			Connection: testCatalogConnection, Instance: "inst-b", AccessToken: "tok-b",
		})

		stub := &stubIntegrationWithSessionCatalog{
			stubIntegrationWithOps: stubIntegrationWithOps{
				StubIntegration: coretesting.StubIntegration{N: "test-int", ConnMode: core.ConnectionModeUser},
			},
		}

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, stub)
			cfg.CatalogConnection = map[string]string{"test-int": testCatalogConnection}
			cfg.Services = svc
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations/test-int/operations", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusConflict {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 409, got %d: %s", resp.StatusCode, body)
		}
		var result map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decoding error response: %v", err)
		}
		if !strings.Contains(result["error"], `"_instance"`) {
			t.Fatalf("expected error to mention _instance, got %q", result["error"])
		}
	})

	t.Run("static_catalog_does_not_fail_open", func(t *testing.T) {
		t.Parallel()

		stub := &stubIntegrationWithSessionCatalog{
			stubIntegrationWithOps: stubIntegrationWithOps{
				StubIntegration: coretesting.StubIntegration{N: "sample-int", ConnMode: core.ConnectionModeUser},
			},
			catalog: &catalog.Catalog{
				Name: "sample-int",
				Operations: []catalog.CatalogOperation{
					{ID: "run", Description: "Static REST op", Method: http.MethodGet, Transport: catalog.TransportREST},
				},
			},
		}

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, stub)
			cfg.CatalogConnection = map[string]string{"sample-int": "catalog-conn"}
			cfg.Services = coretesting.NewStubServices(t)
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations/sample-int/operations", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusPreconditionFailed {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 412, got %d: %s", resp.StatusCode, body)
		}
	})
}

func TestExecuteOperation(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok1", SubjectID: principal.UserSubjectID(u.ID), Integration: "test-int",
		Connection: "", Instance: "default", AccessToken: "test-token",
	})

	fullStub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N: "test-int",
			ExecuteFn: func(_ context.Context, op string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return &core.OperationResult{
					Status: http.StatusOK,
					Body:   fmt.Sprintf(`{"operation":%q}`, op),
				}, nil
			},
		},
		ops: []core.Operation{
			{Name: "do_thing", Description: "Do a thing", Method: http.MethodGet},
			{Name: "create_thing", Description: "Create a thing", Method: http.MethodPost},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, fullStub)
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/test-int/do_thing?foo=bar", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if body["operation"] != "do_thing" {
		t.Fatalf("expected operation do_thing, got %q", body["operation"])
	}
}

func TestExecuteOperation_UsesInjectedInvoker(t *testing.T) {
	t.Parallel()

	var called bool
	var gotProvider string
	var gotInstance string
	var gotOperation string
	var gotParams map[string]any
	var gotConnection string

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, &stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "custom-provider"},
			ops: []core.Operation{
				{Name: "custom-operation", Description: "Custom operation", Method: http.MethodPost},
			},
		})
		cfg.Invoker = &testutil.StubInvoker{
			InvokeFn: func(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error) {
				called = true
				gotProvider = providerName
				gotInstance = instance
				gotOperation = operation
				gotParams = params
				gotConnection = invocation.ConnectionFromContext(ctx)
				if p == nil || p.Identity == nil || p.Identity.Email == "" {
					t.Fatal("expected authenticated principal")
				}
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		}
	})
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/custom-provider/custom-operation?_connection=workspace&_instance=tenant-a", bytes.NewBufferString(`{"foo":"bar"}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if !called {
		t.Fatal("expected injected invoker to be called")
	}
	if gotProvider != "custom-provider" {
		t.Fatalf("expected provider custom-provider, got %q", gotProvider)
	}
	if gotInstance != "tenant-a" {
		t.Fatalf("expected instance tenant-a, got %q", gotInstance)
	}
	if gotOperation != "custom-operation" {
		t.Fatalf("expected operation custom-operation, got %q", gotOperation)
	}
	if gotConnection != "workspace" {
		t.Fatalf("expected connection workspace, got %q", gotConnection)
	}
	if gotParams["foo"] != "bar" {
		t.Fatalf("expected params to include foo=bar, got %v", gotParams)
	}
	if _, ok := gotParams["_instance"]; ok {
		t.Fatalf("expected _instance to be stripped from params, got %v", gotParams)
	}
	if _, ok := gotParams["_connection"]; ok {
		t.Fatalf("expected _connection to be stripped from params, got %v", gotParams)
	}
}

func TestExecuteOperation_WrappedProvidersPreserveOperationConnectionRouting(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	user, err := svc.Users.FindOrCreateUser(context.Background(), "wrapped@test.local")
	if err != nil {
		t.Fatalf("FindOrCreateUser: %v", err)
	}
	seedToken(t, svc, &core.IntegrationToken{
		ID:          "svc-workspace-default",
		SubjectID:   principal.UserSubjectID(user.ID),
		Integration: "svc",
		Connection:  "workspace",
		Instance:    "default",
		AccessToken: "workspace-token",
	})

	backend := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N: "search-backend",
			ExecuteFn: func(_ context.Context, op string, _ map[string]any, token string) (*core.OperationResult, error) {
				return &core.OperationResult{
					Status: http.StatusOK,
					Body:   fmt.Sprintf(`{"operation":%q,"token":%q}`, op, token),
				}, nil
			},
		},
		ops: []core.Operation{
			{Name: "search", Description: "Search", Method: http.MethodGet},
		},
	}
	merged, err := composite.NewMergedWithConnections("svc-api", "Svc API", "", "",
		composite.BoundProvider{Provider: backend, Connection: "workspace"},
	)
	if err != nil {
		t.Fatalf("NewMergedWithConnections: %v", err)
	}
	apiProv := coreintegration.NewRestricted(merged, map[string]string{"find": "search"})
	prov := composite.New("svc", apiProv, &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "svc-mcp", ConnMode: core.ConnectionModeNone},
		},
	})
	if got := apiProv.ConnectionForOperation("find"); got != "workspace" {
		t.Fatalf("restricted op connection = %q, want workspace", got)
	}
	if got := prov.ConnectionForOperation("find"); got != "workspace" {
		t.Fatalf("composite op connection = %q, want workspace", got)
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "user-token" {
					return nil, fmt.Errorf("bad token")
				}
				return &core.UserIdentity{Email: "wrapped@test.local"}, nil
			},
		}
		cfg.Providers = testutil.NewProviderRegistry(t, prov)
		cfg.CatalogConnection = map[string]string{"svc": "workspace"}
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/svc/find", nil)
	req.Header.Set("Authorization", "Bearer user-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if body["operation"] != "search" {
		t.Fatalf("operation = %q, want search", body["operation"])
	}
	if body["token"] != "workspace-token" {
		t.Fatalf("token = %q, want workspace-token", body["token"])
	}
}

func TestExecuteOperation_RejectsExplicitConnectionForStaticOperation(t *testing.T) {
	t.Parallel()

	var called bool
	apiBackend := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "sample-api",
			ConnMode: core.ConnectionModeUser,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				called = true
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		},
		ops: []core.Operation{
			{Name: "api_get_resource", Description: "Get resource", Method: http.MethodGet},
		},
	}
	apiProv, err := composite.NewMergedWithConnections(
		"sample-api",
		"Sample API",
		"",
		"",
		composite.BoundProvider{Provider: apiBackend, Connection: "api-conn"},
	)
	if err != nil {
		t.Fatalf("NewMergedWithConnections: %v", err)
	}
	prov := composite.New("sample-svc", apiProv, &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "sample-mcp", ConnMode: core.ConnectionModeUser},
		},
	})

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, prov)
		cfg.Services = coretesting.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/sample-svc/api_get_resource?_connection="+config.PluginConnectionAlias, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, body)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if !strings.Contains(body["error"], `uses connection "api-conn"`) {
		t.Fatalf("expected connection mismatch message, got %q", body["error"])
	}
	if !strings.Contains(body["error"], `"`+config.PluginConnectionAlias+`"`) {
		t.Fatalf("expected requested connection in error, got %q", body["error"])
	}
	if strings.Contains(body["error"], `"`+config.PluginConnectionName+`"`) {
		t.Fatalf("expected error to preserve caller input, got %q", body["error"])
	}
	if called {
		t.Fatal("expected provider execution to be skipped")
	}
}

func TestExecuteOperation_AllowsExplicitConnectionAliasForStaticOperation(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	user, err := svc.Users.FindOrCreateUser(context.Background(), "alias@test.local")
	if err != nil {
		t.Fatalf("FindOrCreateUser: %v", err)
	}
	seedToken(t, svc, &core.IntegrationToken{
		ID:          "sample-svc-plugin-default",
		SubjectID:   principal.UserSubjectID(user.ID),
		Integration: "sample-svc",
		Connection:  config.PluginConnectionName,
		Instance:    "default",
		AccessToken: "plugin-token",
	})

	apiBackend := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "sample-api",
			ConnMode: core.ConnectionModeUser,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
				return &core.OperationResult{
					Status: http.StatusOK,
					Body:   fmt.Sprintf(`{"token":%q}`, token),
				}, nil
			},
		},
		ops: []core.Operation{
			{Name: "api_get_resource", Description: "Get resource", Method: http.MethodGet},
		},
	}
	apiProv, err := composite.NewMergedWithConnections(
		"sample-api",
		"Sample API",
		"",
		"",
		composite.BoundProvider{Provider: apiBackend, Connection: config.PluginConnectionName},
	)
	if err != nil {
		t.Fatalf("NewMergedWithConnections: %v", err)
	}
	prov := composite.New("sample-svc", apiProv, &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "sample-mcp", ConnMode: core.ConnectionModeNone},
		},
	})

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "user-token" {
					return nil, fmt.Errorf("bad token")
				}
				return &core.UserIdentity{Email: "alias@test.local"}, nil
			},
		}
		cfg.Providers = testutil.NewProviderRegistry(t, prov)
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/sample-svc/api_get_resource?_connection="+config.PluginConnectionAlias, nil)
	req.Header.Set("Authorization", "Bearer user-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if body["token"] != "plugin-token" {
		t.Fatalf("token = %q, want plugin-token", body["token"])
	}
}

func TestExecuteOperation_UnknownIntegration(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Services = coretesting.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/nonexistent/some_op", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestExecuteOperation_UnknownOperation(t *testing.T) {
	t.Parallel()

	fullStub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{N: "test-int"},
		ops: []core.Operation{
			{Name: "do_thing", Description: "Do a thing", Method: http.MethodGet},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, fullStub)
		cfg.Services = coretesting.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/test-int/nonexistent", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}

	sessionStub := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "sample-int", ConnMode: core.ConnectionModeUser},
		},
		catalog: serverTestCatalog("sample-int", []catalog.CatalogOperation{
			{ID: "run", Description: "Run", Method: http.MethodGet, Transport: catalog.TransportREST},
		}),
		catalogForRequestFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			if token != "tok-team-a" {
				return nil, fmt.Errorf("unexpected token %q", token)
			}
			return &catalog.Catalog{Name: "sample-int"}, nil
		},
	}

	svc := coretesting.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok-team-a", SubjectID: principal.UserSubjectID(u.ID), Integration: "sample-int",
		Connection: testCatalogConnection, Instance: "team-a", AccessToken: "tok-team-a",
	})

	ts = newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, sessionStub)
		cfg.CatalogConnection = map[string]string{"sample-int": testCatalogConnection}
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/v1/sample-int/run?_instance=team-a", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("session request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 404 for missing session operation, got %d: %s", resp.StatusCode, body)
	}
}

func TestExecuteOperation_NoStoredToken(t *testing.T) {
	t.Parallel()

	fullStub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{N: "test-int"},
		ops: []core.Operation{
			{Name: "do_thing", Description: "Do a thing", Method: http.MethodGet},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, fullStub)
		cfg.Services = coretesting.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/test-int/do_thing", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusPreconditionFailed {
		t.Fatalf("expected 412, got %d", resp.StatusCode)
	}

	sessionStub := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "test-int", ConnMode: core.ConnectionModeUser},
		},
	}

	ts = newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, sessionStub)
		cfg.CatalogConnection = map[string]string{"test-int": testCatalogConnection}
		cfg.Services = coretesting.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

}

func TestWorkloadAuthorization_ExecuteOperation_UsesBoundIdentityAndRejectsSelectors(t *testing.T) {
	t.Parallel()

	workloadToken, _, err := principal.GenerateToken(principal.TokenTypeWorkload)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	svc := coretesting.NewStubServices(t)
	seedSubjectToken(t, svc, principal.WorkloadSubjectID("triage-bot"), "svc", "workspace", "team-a", "identity-bound-token")

	stub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "svc",
			ConnMode: core.ConnectionModeUser,
			ExecuteFn: func(_ context.Context, op string, _ map[string]any, token string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: fmt.Sprintf(`{"operation":%q,"token":%q}`, op, token)}, nil
			},
		},
		ops: []core.Operation{
			{Name: "run", Method: http.MethodGet},
			{Name: "admin", Method: http.MethodGet},
		},
	}
	providers := testutil.NewProviderRegistry(t, stub)
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Workloads: map[string]config.WorkloadDef{
			"triage-bot": {
				Token: workloadToken,
				Providers: map[string]config.WorkloadProviderDef{
					"svc": {
						Connection: "workspace",
						Instance:   "team-a",
						Allow:      []string{"run"},
					},
				},
			},
		},
	}, providers, nil, nil)

	var auditBuf bytes.Buffer
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
		cfg.Providers = providers
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.AuditSink = invocation.NewSlogAuditSink(&auditBuf)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/svc/run", nil)
	req.Header.Set("Authorization", "Bearer "+workloadToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if result["token"] != "identity-bound-token" {
		t.Fatalf("expected bound identity token, got %v", result["token"])
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/v1/svc/admin", nil)
	req.Header.Set("Authorization", "Bearer "+workloadToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unauthorized request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 403, got %d: %s", resp.StatusCode, body)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/v1/svc/run?_instance=team-a", nil)
	req.Header.Set("Authorization", "Bearer "+workloadToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("selector request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 403 for selector override, got %d: %s", resp.StatusCode, body)
	}

	lines := bytes.Split(bytes.TrimSpace(auditBuf.Bytes()), []byte("\n"))
	if len(lines) < 2 {
		t.Fatalf("expected denial audit records, got %d", len(lines))
	}

	var accessDeniedAudit map[string]any
	if err := json.Unmarshal(lines[len(lines)-2], &accessDeniedAudit); err != nil {
		t.Fatalf("parsing denied operation audit record: %v\nraw: %s", err, auditBuf.String())
	}
	if accessDeniedAudit["provider"] != "svc" {
		t.Fatalf("expected denied operation provider svc, got %v", accessDeniedAudit["provider"])
	}
	if accessDeniedAudit["operation"] != "admin" {
		t.Fatalf("expected denied operation admin, got %v", accessDeniedAudit["operation"])
	}
	if accessDeniedAudit["allowed"] != false {
		t.Fatalf("expected denied operation audit allowed=false, got %v", accessDeniedAudit["allowed"])
	}
	if accessDeniedAudit["error"] != "operation access denied" {
		t.Fatalf("unexpected denied operation error: %v", accessDeniedAudit["error"])
	}
	if accessDeniedAudit["authorization_decision"] != "operation_binding_denied" {
		t.Fatalf("expected denied operation authorization_decision operation_binding_denied, got %v", accessDeniedAudit["authorization_decision"])
	}

	var selectorAudit map[string]any
	if err := json.Unmarshal(lines[len(lines)-1], &selectorAudit); err != nil {
		t.Fatalf("parsing selector denial audit record: %v\nraw: %s", err, auditBuf.String())
	}
	if selectorAudit["provider"] != "svc" {
		t.Fatalf("expected selector audit provider svc, got %v", selectorAudit["provider"])
	}
	if selectorAudit["operation"] != "run" {
		t.Fatalf("expected selector audit operation run, got %v", selectorAudit["operation"])
	}
	if selectorAudit["allowed"] != false {
		t.Fatalf("expected selector audit allowed=false, got %v", selectorAudit["allowed"])
	}
	if selectorAudit["auth_source"] != "workload_token" {
		t.Fatalf("expected selector audit auth_source workload_token, got %v", selectorAudit["auth_source"])
	}
	if selectorAudit["subject_id"] != "workload:triage-bot" {
		t.Fatalf("expected selector audit subject_id workload:triage-bot, got %v", selectorAudit["subject_id"])
	}
	if selectorAudit["error"] != "workload callers may not override connection or instance bindings" {
		t.Fatalf("unexpected selector audit error: %v", selectorAudit["error"])
	}
}

func TestHumanAuthorization_ExecuteOperation_UsesResolvedRoleAndRejectsDisallowedOperations(t *testing.T) {
	t.Parallel()

	plaintext, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	svc := coretesting.NewStubServices(t)
	seedAPIToken(t, svc, plaintext, hashed, "viewer-user")
	viewer := seedUser(t, svc, "viewer-user@test.local")

	stub := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N:        "svc",
				ConnMode: core.ConnectionModeNone,
				ExecuteFn: func(ctx context.Context, op string, _ map[string]any, _ string) (*core.OperationResult, error) {
					access := invocation.AccessContextFromContext(ctx)
					body, err := json.Marshal(map[string]string{
						"operation": op,
						"policy":    access.Policy,
						"role":      access.Role,
					})
					if err != nil {
						return nil, err
					}
					return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil
				},
			},
		},
		catalog: &catalog.Catalog{
			Name: "svc",
			Operations: []catalog.CatalogOperation{
				{ID: "run", Method: http.MethodGet, Transport: catalog.TransportREST, AllowedRoles: []string{"viewer"}},
				{ID: "admin", Method: http.MethodGet, Transport: catalog.TransportREST, AllowedRoles: []string{"admin"}},
			},
		},
	}

	pluginDefs := map[string]*config.ProviderEntry{
		"svc": {AuthorizationPolicy: "sample_policy"},
	}
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					staticPolicyUserMember(t, svc, "viewer-user@test.local", "viewer"),
					{SubjectID: principal.UserSubjectID(viewer.ID), Role: "viewer"},
				},
			},
		},
	}, nil, pluginDefs, nil)

	var auditBuf bytes.Buffer
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.PluginDefs = pluginDefs
		cfg.AuditSink = invocation.NewSlogAuditSink(&auditBuf)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/svc/run", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if result["policy"] != "sample_policy" || result["role"] != "viewer" {
		t.Fatalf("unexpected access context in execute response: %+v", result)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/v1/svc/admin", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("admin request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 403, got %d: %s", resp.StatusCode, body)
	}

	lines := bytes.Split(bytes.TrimSpace(auditBuf.Bytes()), []byte("\n"))
	if len(lines) == 0 {
		t.Fatal("expected denial audit record")
	}

	var deniedAudit map[string]any
	if err := json.Unmarshal(lines[len(lines)-1], &deniedAudit); err != nil {
		t.Fatalf("parsing denied audit record: %v\nraw: %s", err, auditBuf.String())
	}
	if deniedAudit["provider"] != "svc" {
		t.Fatalf("expected denied audit provider svc, got %v", deniedAudit["provider"])
	}
	if deniedAudit["operation"] != "admin" {
		t.Fatalf("expected denied audit operation admin, got %v", deniedAudit["operation"])
	}
	if deniedAudit["allowed"] != false {
		t.Fatalf("expected denied audit allowed=false, got %v", deniedAudit["allowed"])
	}
	if deniedAudit["auth_source"] != "api_token" {
		t.Fatalf("expected denied audit auth_source api_token, got %v", deniedAudit["auth_source"])
	}
	if deniedAudit["subject_id"] != principal.UserSubjectID(viewer.ID) {
		t.Fatalf("expected denied audit subject_id %q, got %v", principal.UserSubjectID(viewer.ID), deniedAudit["subject_id"])
	}
	if deniedAudit["access_policy"] != "sample_policy" {
		t.Fatalf("expected denied audit access_policy sample_policy, got %v", deniedAudit["access_policy"])
	}
	if deniedAudit["access_role"] != "viewer" {
		t.Fatalf("expected denied audit access_role viewer, got %v", deniedAudit["access_role"])
	}
	if deniedAudit["authorization_decision"] != "catalog_role_denied" {
		t.Fatalf("expected denied audit authorization_decision catalog_role_denied, got %v", deniedAudit["authorization_decision"])
	}
	if deniedAudit["error"] != "operation access denied" {
		t.Fatalf("expected denied audit error operation access denied, got %v", deniedAudit["error"])
	}
}

func TestHumanAuthorization_ExecuteOperation_DefaultAllowTreatsAuthenticatedUsersAsViewer(t *testing.T) {
	t.Parallel()

	plaintext, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	svc := coretesting.NewStubServices(t)
	seedAPIToken(t, svc, plaintext, hashed, "viewer-user")
	viewer := seedUser(t, svc, "viewer-user@test.local")

	stub := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N:        "svc",
				ConnMode: core.ConnectionModeNone,
				ExecuteFn: func(ctx context.Context, op string, _ map[string]any, _ string) (*core.OperationResult, error) {
					access := invocation.AccessContextFromContext(ctx)
					body, err := json.Marshal(map[string]string{
						"operation": op,
						"policy":    access.Policy,
						"role":      access.Role,
					})
					if err != nil {
						return nil, err
					}
					return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil
				},
			},
		},
		catalog: &catalog.Catalog{
			Name: "svc",
			Operations: []catalog.CatalogOperation{
				{ID: "run", Method: http.MethodGet, Transport: catalog.TransportREST, AllowedRoles: []string{"viewer"}},
				{ID: "admin", Method: http.MethodGet, Transport: catalog.TransportREST, AllowedRoles: []string{"admin"}},
			},
		},
	}

	pluginDefs := map[string]*config.ProviderEntry{
		"svc": {AuthorizationPolicy: "sample_policy"},
	}
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {
				Default: "allow",
				Members: []config.HumanPolicyMemberDef{
					staticPolicyUserMember(t, svc, "admin-user@test.local", "admin"),
				},
			},
		},
	}, nil, pluginDefs, nil)

	var auditBuf bytes.Buffer
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.PluginDefs = pluginDefs
		cfg.AuditSink = invocation.NewSlogAuditSink(&auditBuf)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/svc/run", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if result["policy"] != "sample_policy" || result["role"] != "viewer" {
		t.Fatalf("unexpected access context in execute response: %+v", result)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/v1/svc/admin", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("admin request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 403, got %d: %s", resp.StatusCode, body)
	}

	lines := bytes.Split(bytes.TrimSpace(auditBuf.Bytes()), []byte("\n"))
	if len(lines) == 0 {
		t.Fatal("expected denial audit record")
	}

	var deniedAudit map[string]any
	if err := json.Unmarshal(lines[len(lines)-1], &deniedAudit); err != nil {
		t.Fatalf("parsing denied audit record: %v\nraw: %s", err, auditBuf.String())
	}
	if deniedAudit["provider"] != "svc" {
		t.Fatalf("expected denied audit provider svc, got %v", deniedAudit["provider"])
	}
	if deniedAudit["operation"] != "admin" {
		t.Fatalf("expected denied audit operation admin, got %v", deniedAudit["operation"])
	}
	if deniedAudit["allowed"] != false {
		t.Fatalf("expected denied audit allowed=false, got %v", deniedAudit["allowed"])
	}
	if deniedAudit["auth_source"] != "api_token" {
		t.Fatalf("expected denied audit auth_source api_token, got %v", deniedAudit["auth_source"])
	}
	if deniedAudit["subject_id"] != principal.UserSubjectID(viewer.ID) {
		t.Fatalf("expected denied audit subject_id %q, got %v", principal.UserSubjectID(viewer.ID), deniedAudit["subject_id"])
	}
	if deniedAudit["access_policy"] != "sample_policy" {
		t.Fatalf("expected denied audit access_policy sample_policy, got %v", deniedAudit["access_policy"])
	}
	if deniedAudit["access_role"] != "viewer" {
		t.Fatalf("expected denied audit access_role viewer, got %v", deniedAudit["access_role"])
	}
	if deniedAudit["authorization_decision"] != "catalog_role_denied" {
		t.Fatalf("expected denied audit authorization_decision catalog_role_denied, got %v", deniedAudit["authorization_decision"])
	}
	if deniedAudit["error"] != "operation access denied" {
		t.Fatalf("expected denied audit error operation access denied, got %v", deniedAudit["error"])
	}
}

func TestHumanAuthorization_ExecuteOperation_UsesResolvedRoleAndRejectsDisallowedOperations_DynamicGrant(t *testing.T) {
	t.Parallel()

	plaintext, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	svc := coretesting.NewStubServices(t)
	seedAPIToken(t, svc, plaintext, hashed, "viewer-user")
	viewer := seedUser(t, svc, "viewer-user@test.local")

	stub := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N:        "svc",
				ConnMode: core.ConnectionModeNone,
				ExecuteFn: func(ctx context.Context, op string, _ map[string]any, _ string) (*core.OperationResult, error) {
					access := invocation.AccessContextFromContext(ctx)
					body, err := json.Marshal(map[string]string{
						"operation": op,
						"policy":    access.Policy,
						"role":      access.Role,
					})
					if err != nil {
						return nil, err
					}
					return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil
				},
			},
		},
		catalog: &catalog.Catalog{
			Name: "svc",
			Operations: []catalog.CatalogOperation{
				{ID: "run", Method: http.MethodGet, Transport: catalog.TransportREST, AllowedRoles: []string{"viewer"}},
				{ID: "admin", Method: http.MethodGet, Transport: catalog.TransportREST, AllowedRoles: []string{"admin"}},
			},
		},
	}

	pluginDefs := map[string]*config.ProviderEntry{
		"svc": {AuthorizationPolicy: "sample_policy"},
	}
	legacy, err := authorization.New(config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {Default: "deny"},
		},
	}, pluginDefs, nil, nil)
	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}
	provider := newMemoryAuthorizationProvider("memory-authorization")
	authz := mustProviderBackedAuthorizer(t, legacy, provider)
	seedProviderPluginAuthorization(t, svc, authz, provider, "svc", "viewer-user@test.local", "viewer")

	var auditBuf bytes.Buffer
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.PluginDefs = pluginDefs
		cfg.AuditSink = invocation.NewSlogAuditSink(&auditBuf)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/svc/run", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if result["policy"] != "sample_policy" || result["role"] != "viewer" {
		t.Fatalf("unexpected access context in execute response: %+v", result)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/v1/svc/admin", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("admin request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 403, got %d: %s", resp.StatusCode, body)
	}

	lines := bytes.Split(bytes.TrimSpace(auditBuf.Bytes()), []byte("\n"))
	if len(lines) == 0 {
		t.Fatal("expected denial audit record")
	}

	var deniedAudit map[string]any
	if err := json.Unmarshal(lines[len(lines)-1], &deniedAudit); err != nil {
		t.Fatalf("parsing denied audit record: %v\nraw: %s", err, auditBuf.String())
	}
	if deniedAudit["provider"] != "svc" {
		t.Fatalf("expected denied audit provider svc, got %v", deniedAudit["provider"])
	}
	if deniedAudit["operation"] != "admin" {
		t.Fatalf("expected denied audit operation admin, got %v", deniedAudit["operation"])
	}
	if deniedAudit["allowed"] != false {
		t.Fatalf("expected denied audit allowed=false, got %v", deniedAudit["allowed"])
	}
	if deniedAudit["auth_source"] != "api_token" {
		t.Fatalf("expected denied audit auth_source api_token, got %v", deniedAudit["auth_source"])
	}
	if deniedAudit["subject_id"] != principal.UserSubjectID(viewer.ID) {
		t.Fatalf("expected denied audit subject_id %q, got %v", principal.UserSubjectID(viewer.ID), deniedAudit["subject_id"])
	}
	if deniedAudit["access_policy"] != "sample_policy" {
		t.Fatalf("expected denied audit access_policy sample_policy, got %v", deniedAudit["access_policy"])
	}
	if deniedAudit["access_role"] != "viewer" {
		t.Fatalf("expected denied audit access_role viewer, got %v", deniedAudit["access_role"])
	}
	if deniedAudit["authorization_decision"] != "catalog_role_denied" {
		t.Fatalf("expected denied audit authorization_decision catalog_role_denied, got %v", deniedAudit["authorization_decision"])
	}
	if deniedAudit["error"] != "operation access denied" {
		t.Fatalf("expected denied audit error operation access denied, got %v", deniedAudit["error"])
	}
}

func TestWorkloadAuthorization_ExecuteOperation_MissingBoundIdentityTokenReturns412(t *testing.T) {
	t.Parallel()

	workloadToken, _, err := principal.GenerateToken(principal.TokenTypeWorkload)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	var auditBuf bytes.Buffer
	auditSink := invocation.NewSlogAuditSink(&auditBuf)

	stub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "svc",
			ConnMode: core.ConnectionModeUser,
		},
		ops: []core.Operation{{Name: "run", Method: http.MethodGet}},
	}
	providers := testutil.NewProviderRegistry(t, stub)
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Workloads: map[string]config.WorkloadDef{
			"triage-bot": {
				Token: workloadToken,
				Providers: map[string]config.WorkloadProviderDef{
					"svc": {
						Connection: "workspace",
						Instance:   "team-a",
						Allow:      []string{"run"},
					},
				},
			},
		},
	}, providers, nil, nil)

	svc := coretesting.NewStubServices(t)
	broker := invocation.NewBroker(providers, svc.Users, svc.Tokens, invocation.WithAuthorizer(authz))
	guarded := invocation.NewGuarded(broker, broker, "http", auditSink, invocation.WithoutRateLimit())

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
		cfg.Providers = providers
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.AuditSink = auditSink
		cfg.Invoker = guarded
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/svc/run", nil)
	req.Header.Set("Authorization", "Bearer "+workloadToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusPreconditionFailed {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 412, got %d: %s", resp.StatusCode, body)
	}

	var record map[string]any
	if err := json.Unmarshal(auditBuf.Bytes(), &record); err != nil {
		t.Fatalf("failed to parse audit JSON: %v\nraw: %s", err, auditBuf.String())
	}
	if record["subject_id"] != "workload:triage-bot" {
		t.Fatalf("expected workload subject_id, got %v", record["subject_id"])
	}
	if record["credential_subject_id"] != "workload:triage-bot" {
		t.Fatalf("expected credential_subject_id workload principal, got %v", record["credential_subject_id"])
	}
	if record["credential_connection"] != "workspace" {
		t.Fatalf("expected credential_connection=workspace, got %v", record["credential_connection"])
	}
	if record["credential_instance"] != "team-a" {
		t.Fatalf("expected credential_instance=team-a, got %v", record["credential_instance"])
	}
}

func TestWorkloadAuthorization_HumanRoutesReturn403(t *testing.T) {
	t.Parallel()

	workloadToken, _, err := principal.GenerateToken(principal.TokenTypeWorkload)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	stub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{N: "weather", ConnMode: core.ConnectionModeNone},
		ops:             []core.Operation{{Name: "forecast", Method: http.MethodGet}},
	}
	providers := testutil.NewProviderRegistry(t, stub)
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Workloads: map[string]config.WorkloadDef{
			"triage-bot": {
				Token: workloadToken,
				Providers: map[string]config.WorkloadProviderDef{
					"weather": {Allow: []string{"forecast"}},
				},
			},
		},
	}, providers, nil, nil)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
		cfg.Providers = providers
		cfg.Services = coretesting.NewStubServices(t)
		cfg.Authorizer = authz
	})
	testutil.CloseOnCleanup(t, ts)

	for _, request := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodPost, path: "/api/v1/tokens", body: `{"name":"bot-token"}`},
		{method: http.MethodPost, path: "/api/v1/auth/connect-manual", body: `{"integration":"weather","credential":"abc"}`},
		{method: http.MethodDelete, path: "/api/v1/integrations/weather"},
		{method: http.MethodPost, path: "/api/v1/auth/logout"},
	} {
		req, _ := http.NewRequest(request.method, ts.URL+request.path, bytes.NewBufferString(request.body))
		req.Header.Set("Authorization", "Bearer "+workloadToken)
		if request.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", request.method, request.path, err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusForbidden {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("%s %s expected 403, got %d: %s", request.method, request.path, resp.StatusCode, body)
		}
	}
}

func TestExecuteOperation_RejectsSessionPassthrough(t *testing.T) {
	t.Parallel()

	assertMCPOnly := func(t *testing.T, ts *httptest.Server) {
		t.Helper()

		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/test-int/session_only", bytes.NewBufferString(`{}`))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusBadRequest {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 400, got %d: %s", resp.StatusCode, body)
		}

		var errResp map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
			t.Fatalf("decode error response: %v", err)
		}
		if errResp["error"] != "this integration is accessible only via MCP" {
			t.Fatalf("expected MCP-only error, got %q", errResp["error"])
		}
	}

	t.Run("default session catalog connection", func(t *testing.T) {
		t.Parallel()

		svc := coretesting.NewStubServices(t)
		u := seedUser(t, svc, "anonymous@gestalt")
		seedToken(t, svc, &core.IntegrationToken{
			ID: "tok-cat", SubjectID: principal.UserSubjectID(u.ID), Integration: "test-int",
			Connection: testCatalogConnection, Instance: "default", AccessToken: "tok-a",
		})

		var sessionCatalogCalls atomic.Int32
		var resolvedToken atomic.Value
		sessionStub := &stubIntegrationWithSessionCatalog{
			stubIntegrationWithOps: stubIntegrationWithOps{
				StubIntegration: coretesting.StubIntegration{N: "test-int", ConnMode: core.ConnectionModeUser},
			},
			catalogForRequestFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
				sessionCatalogCalls.Add(1)
				resolvedToken.Store(token)
				if token != "tok-a" {
					return nil, fmt.Errorf("unexpected token %q", token)
				}
				return &catalog.Catalog{
					Name: "test-int",
					Operations: []catalog.CatalogOperation{
						{ID: "session_only", Description: "Session-only op", Method: http.MethodPost, Transport: catalog.TransportMCPPassthrough},
					},
				}, nil
			},
		}

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, sessionStub)
			cfg.CatalogConnection = map[string]string{"test-int": testCatalogConnection}
			cfg.Services = svc
		})
		testutil.CloseOnCleanup(t, ts)

		assertMCPOnly(t, ts)
		if got := sessionCatalogCalls.Load(); got != 1 {
			t.Fatalf("session catalog calls = %d, want 1", got)
		}
		if got, _ := resolvedToken.Load().(string); got != "tok-a" {
			t.Fatalf("resolved token = %q, want %q", got, "tok-a")
		}
	})

	t.Run("server catalog connection takes precedence over broker MCP connection", func(t *testing.T) {
		t.Parallel()

		var sessionCatalogCalls atomic.Int32
		var resolvedToken atomic.Value

		sessionStub := &stubIntegrationWithSessionCatalog{
			stubIntegrationWithOps: stubIntegrationWithOps{
				StubIntegration: coretesting.StubIntegration{N: "test-int", ConnMode: core.ConnectionModeUser},
			},
			catalogForRequestFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
				sessionCatalogCalls.Add(1)
				resolvedToken.Store(token)
				switch token {
				case "mcp-token":
					return &catalog.Catalog{
						Name: "test-int",
						Operations: []catalog.CatalogOperation{
							{ID: "session_only", Description: "Session-only op", Method: http.MethodPost, Transport: catalog.TransportMCPPassthrough},
						},
					}, nil
				case "catalog-token":
					return &catalog.Catalog{
						Name:       "test-int",
						Operations: nil,
					}, nil
				default:
					return nil, fmt.Errorf("unexpected token %q", token)
				}
			},
		}

		providers := testutil.NewProviderRegistry(t, sessionStub)
		svc := coretesting.NewStubServices(t)
		u := seedUser(t, svc, "anonymous@gestalt")
		seedToken(t, svc, &core.IntegrationToken{
			ID: "tok-mcp", SubjectID: principal.UserSubjectID(u.ID), Integration: "test-int",
			Connection: "mcp-conn", Instance: "default", AccessToken: "mcp-token",
		})
		seedToken(t, svc, &core.IntegrationToken{
			ID: "tok-cat", SubjectID: principal.UserSubjectID(u.ID), Integration: "test-int",
			Connection: "catalog-conn", Instance: "default", AccessToken: "catalog-token",
		})

		broker := invocation.NewBroker(
			providers,
			svc.Users,
			svc.Tokens,
			invocation.WithMCPConnectionMapper(invocation.ConnectionMap(map[string]string{"test-int": "mcp-conn"})),
		)

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = providers
			cfg.Services = svc
			cfg.Invoker = broker
			cfg.CatalogConnection = map[string]string{"test-int": "catalog-conn"}
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/test-int/session_only", bytes.NewBufferString(`{}`))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusNotFound {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 404, got %d: %s", resp.StatusCode, body)
		}

		if got := sessionCatalogCalls.Load(); got != 1 {
			t.Fatalf("session catalog calls = %d, want 1", got)
		}
		if got, _ := resolvedToken.Load().(string); got != "catalog-token" {
			t.Fatalf("resolved token = %q, want %q", got, "catalog-token")
		}
	})
}

func TestExecuteOperation_UsesFallbackSessionCatalogConnectionAfterEarlierError(t *testing.T) {
	t.Parallel()

	var gotToken string
	sessionStub := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N:        "sample-int",
				ConnMode: core.ConnectionModeUser,
				ExecuteFn: func(_ context.Context, op string, _ map[string]any, token string) (*core.OperationResult, error) {
					gotToken = token
					return &core.OperationResult{Status: http.StatusOK, Body: fmt.Sprintf(`{"operation":%q,"token":%q}`, op, token)}, nil
				},
			},
		},
		catalogForRequestFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			if token != "rest-token" {
				return nil, fmt.Errorf("unexpected token %q", token)
			}
			return &catalog.Catalog{
				Name: "sample-int",
				Operations: []catalog.CatalogOperation{
					{ID: "run", Description: "Run", Method: http.MethodGet, Transport: catalog.TransportREST},
				},
			}, nil
		},
	}

	providers := testutil.NewProviderRegistry(t, sessionStub)
	svc := coretesting.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok-rest", SubjectID: principal.UserSubjectID(u.ID), Integration: "sample-int",
		Connection: "rest-conn", Instance: "default", AccessToken: "rest-token",
	})

	broker := invocation.NewBroker(
		providers,
		svc.Users,
		svc.Tokens,
		invocation.WithMCPConnectionMapper(invocation.ConnectionMap(map[string]string{"sample-int": "mcp-conn"})),
	)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = providers
		cfg.Services = svc
		cfg.Invoker = broker
		cfg.DefaultConnection = map[string]string{"sample-int": "rest-conn"}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/sample-int/run", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}
	if gotToken != "rest-token" {
		_ = resp.Body.Close()
		t.Fatalf("execute token = %q, want %q", gotToken, "rest-token")
	}
	_ = resp.Body.Close()

	gotToken = ""
	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/v1/sample-int/run?_connection=mcp-conn", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("explicit request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusPreconditionFailed {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 412 for explicit session catalog failure, got %d: %s", resp.StatusCode, body)
	}
	if gotToken != "" {
		t.Fatalf("execute token = %q, want no provider execution", gotToken)
	}
}

func TestExecuteOperation_PinsSessionCatalogConnectionIntoExecution(t *testing.T) {
	t.Parallel()

	var gotToken string
	sessionStub := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N:        "sample-int",
				ConnMode: core.ConnectionModeUser,
				ExecuteFn: func(_ context.Context, op string, _ map[string]any, token string) (*core.OperationResult, error) {
					gotToken = token
					return &core.OperationResult{Status: http.StatusOK, Body: fmt.Sprintf(`{"operation":%q,"token":%q}`, op, token)}, nil
				},
			},
		},
		catalogForRequestFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			switch token {
			case "mcp-token":
				return &catalog.Catalog{
					Name: "sample-int",
					Operations: []catalog.CatalogOperation{
						{ID: "run", Description: "Run", Method: http.MethodGet, Transport: catalog.TransportREST},
					},
				}, nil
			case "rest-token":
				return &catalog.Catalog{Name: "sample-int"}, nil
			default:
				return nil, fmt.Errorf("unexpected token %q", token)
			}
		},
	}

	providers := testutil.NewProviderRegistry(t, sessionStub)
	svc := coretesting.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok-mcp", SubjectID: principal.UserSubjectID(u.ID), Integration: "sample-int",
		Connection: "mcp-conn", Instance: "default", AccessToken: "mcp-token",
	})
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok-rest", SubjectID: principal.UserSubjectID(u.ID), Integration: "sample-int",
		Connection: "rest-conn", Instance: "default", AccessToken: "rest-token",
	})

	broker := invocation.NewBroker(
		providers,
		svc.Users,
		svc.Tokens,
		invocation.WithMCPConnectionMapper(invocation.ConnectionMap(map[string]string{"sample-int": "mcp-conn"})),
	)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = providers
		cfg.Services = svc
		cfg.Invoker = broker
		cfg.DefaultConnection = map[string]string{"sample-int": "rest-conn"}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/sample-int/run", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}
	if gotToken != "mcp-token" {
		t.Fatalf("execute token = %q, want %q", gotToken, "mcp-token")
	}
}

func TestExecuteOperation_UsesConfiguredCatalogConnectionWhenInvokerIsWrapped(t *testing.T) {
	t.Parallel()

	var gotToken string
	sessionStub := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N:        "sample-int",
				ConnMode: core.ConnectionModeUser,
				ExecuteFn: func(_ context.Context, op string, _ map[string]any, token string) (*core.OperationResult, error) {
					gotToken = token
					return &core.OperationResult{Status: http.StatusOK, Body: fmt.Sprintf(`{"operation":%q,"token":%q}`, op, token)}, nil
				},
			},
		},
		catalogForRequestFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			switch token {
			case "catalog-token":
				return &catalog.Catalog{
					Name: "sample-int",
					Operations: []catalog.CatalogOperation{
						{ID: "run", Description: "Run", Method: http.MethodGet, Transport: catalog.TransportREST},
					},
				}, nil
			case "rest-token":
				return &catalog.Catalog{Name: "sample-int"}, nil
			default:
				return nil, fmt.Errorf("unexpected token %q", token)
			}
		},
	}

	providers := testutil.NewProviderRegistry(t, sessionStub)
	svc := coretesting.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok-catalog", SubjectID: principal.UserSubjectID(u.ID), Integration: "sample-int",
		Connection: "catalog-conn", Instance: "default", AccessToken: "catalog-token",
	})
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok-rest", SubjectID: principal.UserSubjectID(u.ID), Integration: "sample-int",
		Connection: "rest-conn", Instance: "default", AccessToken: "rest-token",
	})

	broker := invocation.NewBroker(
		providers,
		svc.Users,
		svc.Tokens,
		invocation.WithConnectionMapper(invocation.ConnectionMap(map[string]string{"sample-int": "rest-conn"})),
	)
	wrappedInvoker := struct {
		invocation.Invoker
		invocation.TokenResolver
	}{
		Invoker:       broker,
		TokenResolver: broker,
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = providers
		cfg.Services = svc
		cfg.Invoker = wrappedInvoker
		cfg.DefaultConnection = map[string]string{"sample-int": "rest-conn"}
		cfg.CatalogConnection = map[string]string{"sample-int": "catalog-conn"}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/sample-int/run", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}
	if gotToken != "catalog-token" {
		t.Fatalf("execute token = %q, want %q", gotToken, "catalog-token")
	}
}

func TestExecuteOperation_UsesServerCatalogConnectionBeforeBrokerFallback(t *testing.T) {
	t.Parallel()

	var gotToken string
	sessionStub := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N:        "sample-int",
				ConnMode: core.ConnectionModeUser,
				ExecuteFn: func(_ context.Context, op string, _ map[string]any, token string) (*core.OperationResult, error) {
					gotToken = token
					return &core.OperationResult{Status: http.StatusOK, Body: fmt.Sprintf(`{"operation":%q,"token":%q}`, op, token)}, nil
				},
			},
		},
		catalogForRequestFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			switch token {
			case "catalog-token":
				return &catalog.Catalog{
					Name: "sample-int",
					Operations: []catalog.CatalogOperation{
						{ID: "run", Description: "Run", Method: http.MethodGet, Transport: catalog.TransportREST},
					},
				}, nil
			case "rest-token":
				return &catalog.Catalog{Name: "sample-int"}, nil
			default:
				return nil, fmt.Errorf("unexpected token %q", token)
			}
		},
	}

	providers := testutil.NewProviderRegistry(t, sessionStub)
	svc := coretesting.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok-catalog", SubjectID: principal.UserSubjectID(u.ID), Integration: "sample-int",
		Connection: "catalog-conn", Instance: "default", AccessToken: "catalog-token",
	})
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok-rest", SubjectID: principal.UserSubjectID(u.ID), Integration: "sample-int",
		Connection: "rest-conn", Instance: "default", AccessToken: "rest-token",
	})

	broker := invocation.NewBroker(
		providers,
		svc.Users,
		svc.Tokens,
		invocation.WithConnectionMapper(invocation.ConnectionMap(map[string]string{"sample-int": "rest-conn"})),
	)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = providers
		cfg.Services = svc
		cfg.Invoker = broker
		cfg.DefaultConnection = map[string]string{"sample-int": "rest-conn"}
		cfg.CatalogConnection = map[string]string{"sample-int": "catalog-conn"}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/sample-int/run", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}
	if gotToken != "catalog-token" {
		t.Fatalf("execute token = %q, want %q", gotToken, "catalog-token")
	}
}

func TestExecuteOperation_DoesNotFallbackPastConfiguredCatalogConnection(t *testing.T) {
	t.Parallel()

	var gotToken string
	sessionStub := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N:        "sample-int",
				ConnMode: core.ConnectionModeUser,
				ExecuteFn: func(_ context.Context, op string, _ map[string]any, token string) (*core.OperationResult, error) {
					gotToken = token
					return &core.OperationResult{Status: http.StatusOK, Body: fmt.Sprintf(`{"operation":%q,"token":%q}`, op, token)}, nil
				},
			},
		},
		catalogForRequestFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			switch token {
			case "catalog-token":
				return &catalog.Catalog{Name: "sample-int"}, nil
			case "rest-token":
				return &catalog.Catalog{
					Name: "sample-int",
					Operations: []catalog.CatalogOperation{
						{ID: "run", Description: "Run", Method: http.MethodGet, Transport: catalog.TransportREST},
					},
				}, nil
			default:
				return nil, fmt.Errorf("unexpected token %q", token)
			}
		},
	}

	providers := testutil.NewProviderRegistry(t, sessionStub)
	svc := coretesting.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok-catalog", SubjectID: principal.UserSubjectID(u.ID), Integration: "sample-int",
		Connection: "catalog-conn", Instance: "default", AccessToken: "catalog-token",
	})
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok-rest", SubjectID: principal.UserSubjectID(u.ID), Integration: "sample-int",
		Connection: "rest-conn", Instance: "default", AccessToken: "rest-token",
	})

	broker := invocation.NewBroker(
		providers,
		svc.Users,
		svc.Tokens,
		invocation.WithConnectionMapper(invocation.ConnectionMap(map[string]string{"sample-int": "rest-conn"})),
	)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = providers
		cfg.Services = svc
		cfg.Invoker = broker
		cfg.DefaultConnection = map[string]string{"sample-int": "rest-conn"}
		cfg.CatalogConnection = map[string]string{"sample-int": "catalog-conn"}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/sample-int/run", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 404, got %d: %s", resp.StatusCode, body)
	}
	if gotToken != "" {
		t.Fatalf("execute token = %q, want no provider execution", gotToken)
	}
}

func TestStartLogin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		loginURL      string
		publicBaseURL string
		wantURL       func(serverURL string) string
	}{
		{
			name:     "preserves absolute login URL",
			loginURL: "https://auth.example.com/login?state=abc",
			wantURL: func(_ string) string {
				return "https://auth.example.com/login?state=abc"
			},
		},
		{
			name:     "resolves relative login URL against request host",
			loginURL: "/login/callback?state=abc",
			wantURL: func(serverURL string) string {
				return serverURL + "/login/callback?state=abc"
			},
		},
		{
			name:          "resolves relative login URL against configured public base URL",
			loginURL:      "/login/callback?state=abc",
			publicBaseURL: "https://gestalt.example.test",
			wantURL: func(_ string) string {
				return "https://gestalt.example.test/login/callback?state=abc"
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ts := newTestServer(t, func(cfg *server.Config) {
				cfg.Auth = &stubAuthWithLoginURL{
					StubAuthProvider: coretesting.StubAuthProvider{N: "test"},
					loginURL:         tt.loginURL,
				}
				cfg.PublicBaseURL = tt.publicBaseURL
			})
			testutil.CloseOnCleanup(t, ts)

			body := bytes.NewBufferString(`{"state":"abc"}`)
			resp, err := http.Post(ts.URL+"/api/v1/auth/login", "application/json", body)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected 200, got %d", resp.StatusCode)
			}

			var result map[string]string
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				t.Fatalf("decoding: %v", err)
			}
			if result["url"] != tt.wantURL(ts.URL) {
				t.Fatalf("unexpected url: %q", result["url"])
			}
		})
	}
}

func TestStartLoginWithCallbackPort(t *testing.T) {
	t.Parallel()

	stub := &stubAuthWithLoginURL{
		StubAuthProvider: coretesting.StubAuthProvider{N: "test"},
		loginURL:         "https://auth.example.com/login",
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = stub
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"state":"abc","callbackPort":12345}`)
	resp, err := http.Post(ts.URL+"/api/v1/auth/login", "application/json", body)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	if stub.capturedState != "cli:12345:abc" {
		t.Fatalf("expected state 'cli:12345:abc', got %q", stub.capturedState)
	}
}

func TestStartLoginWithInvalidCallbackPort(t *testing.T) {
	t.Parallel()

	stub := &stubAuthWithLoginURL{
		StubAuthProvider: coretesting.StubAuthProvider{N: "test"},
		loginURL:         "https://auth.example.com/login",
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = stub
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"state":"abc","callbackPort":99999}`)
	resp, err := http.Post(ts.URL+"/api/v1/auth/login", "application/json", body)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	if stub.capturedState != "abc" {
		t.Fatalf("expected state 'abc', got %q", stub.capturedState)
	}
}

func TestStartLogin_NoAuthInvalidJSON(t *testing.T) {
	t.Parallel()

	var auditBuf bytes.Buffer
	auditSink := invocation.NewSlogAuditSink(&auditBuf)
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = nil
		cfg.AuditSink = auditSink
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Post(ts.URL+"/api/v1/auth/login", "application/json", strings.NewReader("{"))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	var auditRecord map[string]any
	if err := json.Unmarshal(auditBuf.Bytes(), &auditRecord); err != nil {
		t.Fatalf("parsing audit record: %v\nraw: %s", err, auditBuf.String())
	}
	if auditRecord["operation"] != "auth.login.start" {
		t.Fatalf("expected audit operation auth.login.start, got %v", auditRecord["operation"])
	}
	if auditRecord["provider"] != "none" {
		t.Fatalf("expected audit provider none, got %v", auditRecord["provider"])
	}
	if auditRecord["allowed"] != false {
		t.Fatalf("expected audit allowed=false, got %v", auditRecord["allowed"])
	}
}

func TestStartBrowserLogin_MissingPluginRouteAuthProviderAuditsAttemptedProvider(t *testing.T) {
	t.Parallel()

	var auditBuf bytes.Buffer
	auditSink := invocation.NewSlogAuditSink(&auditBuf)
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &stubHostIssuedSessionAuth{name: "server"}
		cfg.SelectedAuthProvider = "server"
		cfg.AuditSink = auditSink
		cfg.MountedUIs = []server.MountedUI{{
			Name:       "sample_portal",
			Path:       "/sample-portal",
			PluginName: "sample_portal",
			Handler:    http.NotFoundHandler(),
		}}
		cfg.PluginDefs = map[string]*config.ProviderEntry{
			"sample_portal": {RouteAuth: &config.RouteAuthDef{Provider: "alt"}},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := noRedirect.Get(ts.URL + "/api/v1/auth/login?next=" + url.QueryEscape("/sample-portal"))
	if err != nil {
		t.Fatalf("GET browser login start: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}

	var auditRecord map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(auditBuf.Bytes()), &auditRecord); err != nil {
		t.Fatalf("parsing audit record: %v\nraw: %s", err, auditBuf.String())
	}
	if auditRecord["operation"] != "auth.login.start" {
		t.Fatalf("expected audit operation auth.login.start, got %v", auditRecord["operation"])
	}
	if auditRecord["provider"] != "alt" {
		t.Fatalf("expected audit provider alt, got %v", auditRecord["provider"])
	}
	if auditRecord["allowed"] != false {
		t.Fatalf("expected audit allowed=false, got %v", auditRecord["allowed"])
	}
}

func TestLoginCallback(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	existing := seedLegacyUserRecord(t, svc, "legacy-user", "User@Example.com", time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	var auditBuf bytes.Buffer
	auditSink := invocation.NewSlogAuditSink(&auditBuf)
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &stubAuthWithToken{
			StubAuthProvider: coretesting.StubAuthProvider{
				N: "test",
				HandleCallbackFn: func(_ context.Context, code string) (*core.UserIdentity, error) {
					if code == "good-code" {
						return &core.UserIdentity{Email: "user@example.com", DisplayName: "User"}, nil
					}
					return nil, fmt.Errorf("bad code")
				},
			},
		}
		cfg.Services = svc
		cfg.AuditSink = auditSink
	})
	testutil.CloseOnCleanup(t, ts)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	body := bytes.NewBufferString(`{"state":"test-state"}`)
	loginResp, err := client.Post(ts.URL+"/api/v1/auth/login", "application/json", body)
	if err != nil {
		t.Fatalf("start login: %v", err)
	}
	_ = loginResp.Body.Close()

	resp, err := client.Get(ts.URL + "/api/v1/auth/login/callback?code=good-code&state=test-state")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if result["email"] != "user@example.com" {
		t.Fatalf("unexpected email: %v", result["email"])
	}
	stored, err := svc.Users.GetUser(context.Background(), existing.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if stored.Email != "user@example.com" {
		t.Fatalf("expected repaired user email %q, got %q", "user@example.com", stored.Email)
	}

	lines := bytes.Split(bytes.TrimSpace(auditBuf.Bytes()), []byte("\n"))
	if len(lines) == 0 {
		t.Fatal("expected login audit record")
	}
	var auditRecord map[string]any
	if err := json.Unmarshal(lines[len(lines)-1], &auditRecord); err != nil {
		t.Fatalf("parsing audit record: %v\nraw: %s", err, auditBuf.String())
	}
	if auditRecord["operation"] != "auth.login.complete" {
		t.Fatalf("expected audit operation auth.login.complete, got %v", auditRecord["operation"])
	}
	if auditRecord["auth_source"] != "session" {
		t.Fatalf("expected audit auth_source session, got %v", auditRecord["auth_source"])
	}
	if subjectID, ok := auditRecord["subject_id"].(string); !ok || subjectID != principal.UserSubjectID(existing.ID) {
		t.Fatalf("expected audit subject_id %q, got %v", principal.UserSubjectID(existing.ID), auditRecord["subject_id"])
	}
	if _, ok := auditRecord["user_id"]; ok {
		t.Fatalf("expected emitted audit record to omit user_id, got %v", auditRecord["user_id"])
	}
}

func TestLoginCallback_MissingPluginRouteAuthProviderAuditsAttemptedProvider(t *testing.T) {
	t.Parallel()

	secret := []byte("0123456789abcdef0123456789abcdef")
	startJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	startClient := &http.Client{
		Jar: startJar,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	startServer := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &stubHostIssuedSessionAuth{name: "server"}
		cfg.SelectedAuthProvider = "server"
		cfg.StateSecret = secret
		cfg.AuthProviders = map[string]core.AuthenticationProvider{
			"alt": &stubHostIssuedSessionAuth{name: "alt"},
		}
		cfg.MountedUIs = []server.MountedUI{{
			Name:       "sample_portal",
			Path:       "/sample-portal",
			PluginName: "sample_portal",
			Handler:    http.NotFoundHandler(),
		}}
		cfg.PluginDefs = map[string]*config.ProviderEntry{
			"sample_portal": {RouteAuth: &config.RouteAuthDef{Provider: "alt"}},
		}
	})
	testutil.CloseOnCleanup(t, startServer)

	startResp, err := startClient.Get(startServer.URL + "/api/v1/auth/login?next=" + url.QueryEscape("/sample-portal"))
	if err != nil {
		t.Fatalf("start browser login: %v", err)
	}
	defer func() { _ = startResp.Body.Close() }()
	if startResp.StatusCode != http.StatusFound {
		t.Fatalf("start status = %d, want %d", startResp.StatusCode, http.StatusFound)
	}

	var loginStateCookie *http.Cookie
	for _, cookie := range startJar.Cookies(startResp.Request.URL) {
		if cookie.Name == "login_state" {
			c := *cookie
			loginStateCookie = &c
			break
		}
	}
	if loginStateCookie == nil {
		t.Fatal("expected login_state cookie")
	}

	var auditBuf bytes.Buffer
	callbackServer := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &stubHostIssuedSessionAuth{name: "server"}
		cfg.SelectedAuthProvider = "server"
		cfg.StateSecret = secret
		cfg.AuditSink = invocation.NewSlogAuditSink(&auditBuf)
	})
	testutil.CloseOnCleanup(t, callbackServer)

	req, _ := http.NewRequest(http.MethodGet, callbackServer.URL+"/api/v1/auth/login/callback?code=good-code&state="+url.QueryEscape("/sample-portal"), nil)
	req.AddCookie(loginStateCookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("callback request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("callback status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}

	var auditRecord map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(auditBuf.Bytes()), &auditRecord); err != nil {
		t.Fatalf("parsing audit record: %v\nraw: %s", err, auditBuf.String())
	}
	if auditRecord["operation"] != "auth.login.complete" {
		t.Fatalf("expected audit operation auth.login.complete, got %v", auditRecord["operation"])
	}
	if auditRecord["provider"] != "alt" {
		t.Fatalf("expected audit provider alt, got %v", auditRecord["provider"])
	}
	if auditRecord["allowed"] != false {
		t.Fatalf("expected audit allowed=false, got %v", auditRecord["allowed"])
	}
}

func TestLoginCallbackForCLI(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	u := seedUser(t, svc, "user@example.com")
	var auditBuf bytes.Buffer
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			HandleCallbackFn: func(_ context.Context, code string) (*core.UserIdentity, error) {
				if code == "good-code" {
					return &core.UserIdentity{Email: "User@Example.com", DisplayName: "User"}, nil
				}
				return nil, fmt.Errorf("bad code")
			},
		}
		cfg.Services = svc
		cfg.AuditSink = invocation.NewSlogAuditSink(&auditBuf)
	})
	testutil.CloseOnCleanup(t, ts)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	body := bytes.NewBufferString(`{"state":"test-state"}`)
	loginResp, err := client.Post(ts.URL+"/api/v1/auth/login", "application/json", body)
	if err != nil {
		t.Fatalf("start login: %v", err)
	}
	_ = loginResp.Body.Close()

	resp, err := client.Get(ts.URL + "/api/v1/auth/login/callback?code=good-code&state=test-state&cli=1")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if result["id"] == "" {
		t.Fatal("expected id in CLI login response")
	}
	if result["token"] == "" {
		t.Fatal("expected token in CLI login response")
	}
	if result["name"] != "cli-token" {
		t.Fatalf("expected cli-token name in CLI login response, got %v", result["name"])
	}

	tokens, err := svc.APITokens.ListAPITokens(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("list api tokens: %v", err)
	}
	if len(tokens) == 0 {
		t.Fatal("expected API token to be stored")
	}
	if tokens[0].Name != "cli-token" {
		t.Fatalf("expected cli token name, got %q", tokens[0].Name)
	}

	for _, cookie := range resp.Cookies() {
		if cookie.Name == "session_token" {
			t.Fatalf("did not expect session cookie for CLI login, got %q", cookie.Value)
		}
	}

	lines := bytes.Split(bytes.TrimSpace(auditBuf.Bytes()), []byte("\n"))
	if len(lines) < 2 {
		t.Fatalf("expected CLI login callback to emit token and login audit records, got %d", len(lines))
	}

	var tokenAudit map[string]any
	if err := json.Unmarshal(lines[len(lines)-2], &tokenAudit); err != nil {
		t.Fatalf("parsing token audit record: %v\nraw: %s", err, auditBuf.String())
	}
	if tokenAudit["operation"] != "api_token.create" {
		t.Fatalf("expected api_token.create audit operation, got %v", tokenAudit["operation"])
	}
	if tokenAudit["source"] != "http" {
		t.Fatalf("expected token audit source http, got %v", tokenAudit["source"])
	}
	if tokenAudit["auth_source"] != "session" {
		t.Fatalf("expected token audit auth_source session, got %v", tokenAudit["auth_source"])
	}
	if subjectID, ok := tokenAudit["subject_id"].(string); !ok || subjectID != principal.UserSubjectID(u.ID) {
		t.Fatalf("expected token audit subject_id %q, got %v", principal.UserSubjectID(u.ID), tokenAudit["subject_id"])
	}
	if _, ok := tokenAudit["user_id"]; ok {
		t.Fatalf("expected emitted token audit record to omit user_id, got %v", tokenAudit["user_id"])
	}
	if tokenAudit["allowed"] != true {
		t.Fatalf("expected token audit allowed=true, got %v", tokenAudit["allowed"])
	}

	var loginAudit map[string]any
	if err := json.Unmarshal(lines[len(lines)-1], &loginAudit); err != nil {
		t.Fatalf("parsing login audit record: %v\nraw: %s", err, auditBuf.String())
	}
	if loginAudit["operation"] != "auth.login.complete" {
		t.Fatalf("expected auth.login.complete audit operation, got %v", loginAudit["operation"])
	}
	if subjectID, ok := loginAudit["subject_id"].(string); !ok || subjectID != principal.UserSubjectID(u.ID) {
		t.Fatalf("expected login audit subject_id %q, got %v", principal.UserSubjectID(u.ID), loginAudit["subject_id"])
	}
	if _, ok := loginAudit["user_id"]; ok {
		t.Fatalf("expected emitted login audit record to omit user_id, got %v", loginAudit["user_id"])
	}
}

func TestLoginCallbackForCLIWithCallbackPortStrippedState(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	u := seedUser(t, svc, "host@example.com")
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &stubHostIssuedSessionAuth{secret: []byte("host-issued-secret")}
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	body := bytes.NewBufferString(`{"state":"test-state","callbackPort":54305}`)
	loginResp, err := client.Post(ts.URL+"/api/v1/auth/login", "application/json", body)
	if err != nil {
		t.Fatalf("start login: %v", err)
	}
	_ = loginResp.Body.Close()

	resp, err := client.Get(ts.URL + "/api/v1/auth/login/callback?code=good-code&state=test-state&cli=1")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if result["id"] == "" {
		t.Fatal("expected id in CLI login response")
	}
	if result["token"] == "" {
		t.Fatal("expected token in CLI login response")
	}

	tokens, err := svc.APITokens.ListAPITokens(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("list api tokens: %v", err)
	}
	if len(tokens) == 0 {
		t.Fatal("expected API token to be stored")
	}
}

func TestLoginCallbackStateMismatch(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &stubAuthWithToken{
			StubAuthProvider: coretesting.StubAuthProvider{N: "test"},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	body := bytes.NewBufferString(`{"state":"correct-state"}`)
	loginResp, err := client.Post(ts.URL+"/api/v1/auth/login", "application/json", body)
	if err != nil {
		t.Fatalf("start login: %v", err)
	}
	_ = loginResp.Body.Close()

	resp, err := client.Get(ts.URL + "/api/v1/auth/login/callback?code=good-code&state=wrong-state")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestLoginCallbackMissingStateCookie(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &stubAuthWithToken{
			StubAuthProvider: coretesting.StubAuthProvider{N: "test"},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/api/v1/auth/login/callback?code=good-code&state=anything")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestLoginCallback_NoAuthMissingCode(t *testing.T) {
	t.Parallel()

	var auditBuf bytes.Buffer
	auditSink := invocation.NewSlogAuditSink(&auditBuf)
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = nil
		cfg.AuditSink = auditSink
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/api/v1/auth/login/callback?state=anything")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	var auditRecord map[string]any
	if err := json.Unmarshal(auditBuf.Bytes(), &auditRecord); err != nil {
		t.Fatalf("parsing audit record: %v\nraw: %s", err, auditBuf.String())
	}
	if auditRecord["operation"] != "auth.login.complete" {
		t.Fatalf("expected audit operation auth.login.complete, got %v", auditRecord["operation"])
	}
	if auditRecord["provider"] != "none" {
		t.Fatalf("expected audit provider none, got %v", auditRecord["provider"])
	}
	if auditRecord["allowed"] != false {
		t.Fatalf("expected audit allowed=false, got %v", auditRecord["allowed"])
	}
}

func TestLoginCallbackExpiredState(t *testing.T) {
	t.Parallel()

	nowVal := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Now = func() time.Time { return nowVal }
		cfg.Auth = &stubAuthWithToken{
			StubAuthProvider: coretesting.StubAuthProvider{N: "test"},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	body := bytes.NewBufferString(`{"state":"test-state"}`)
	loginResp, err := client.Post(ts.URL+"/api/v1/auth/login", "application/json", body)
	if err != nil {
		t.Fatalf("start login: %v", err)
	}
	_ = loginResp.Body.Close()

	nowVal = nowVal.Add(11 * time.Minute)

	resp, err := client.Get(ts.URL + "/api/v1/auth/login/callback?code=good-code&state=test-state")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestLoginCallbackWithStatefulHandler(t *testing.T) {
	t.Parallel()

	stub := &stubStatefulAuth{
		StubAuthProvider: coretesting.StubAuthProvider{N: "test"},
		handleWithState: func(_ context.Context, code, state string) (*core.UserIdentity, string, error) {
			if code == "good-code" && state == "encrypted-state" {
				return &core.UserIdentity{Email: "pkce@example.com"}, "original-state", nil
			}
			return nil, "", fmt.Errorf("bad code or state")
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = stub
	})
	testutil.CloseOnCleanup(t, ts)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	body := bytes.NewBufferString(`{"state":"original-state"}`)
	loginResp, err := client.Post(ts.URL+"/api/v1/auth/login", "application/json", body)
	if err != nil {
		t.Fatalf("start login: %v", err)
	}
	_ = loginResp.Body.Close()

	resp, err := client.Get(ts.URL + "/api/v1/auth/login/callback?code=good-code&state=encrypted-state")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if result["email"] != "pkce@example.com" {
		t.Fatalf("unexpected email: %v", result["email"])
	}
}

func TestStartIntegrationOAuth(t *testing.T) {
	t.Parallel()

	var auditBuf bytes.Buffer
	stub := &stubIntegrationWithAuthURL{
		StubIntegration: coretesting.StubIntegration{N: "slack"},
		authURL:         "https://slack.com/oauth/v2/authorize",
	}

	handler := &testOAuthHandler{
		authorizationBaseURLVal: "https://slack.com/oauth/v2/authorize",
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"slack": testDefaultConnection}
		cfg.ConnectionAuth = testConnectionAuth("slack", handler)
		cfg.AuditSink = invocation.NewSlogAuditSink(&auditBuf)
		cfg.Services = coretesting.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"integration":"slack","scopes":["channels:read"]}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/start-oauth", body)
	req.Header.Set("Authorization", "Bearer ignored")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if result["url"] == "" {
		t.Fatal("expected non-empty url")
	}
	if result["state"] == "" {
		t.Fatal("expected non-empty state")
	}
	parsedURL, err := url.Parse(result["url"])
	if err != nil {
		t.Fatalf("parse auth URL: %v", err)
	}
	if parsedURL.Query().Get("state") != result["state"] {
		t.Fatal("expected auth URL state to match returned state")
	}

	var auditRecord map[string]any
	if err := json.Unmarshal(auditBuf.Bytes(), &auditRecord); err != nil {
		t.Fatalf("parsing audit record: %v\nraw: %s", err, auditBuf.String())
	}
	if auditRecord["target_kind"] != "connection" {
		t.Fatalf("expected audit target_kind connection, got %v", auditRecord["target_kind"])
	}
	if auditRecord["target_id"] != "slack/default/default" {
		t.Fatalf("expected audit target_id slack/default/default, got %v", auditRecord["target_id"])
	}
	if auditRecord["target_name"] != "default/default" {
		t.Fatalf("expected audit target_name default/default, got %v", auditRecord["target_name"])
	}

	var invalidAuditBuf bytes.Buffer
	invalidTS := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"slack": testDefaultConnection}
		cfg.ConnectionAuth = testConnectionAuth("slack", handler)
		cfg.AuditSink = invocation.NewSlogAuditSink(&invalidAuditBuf)
		cfg.Services = coretesting.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, invalidTS)

	invalidBody := bytes.NewBufferString(`{"integration":"slack","connectionParams":{"unknown":"nope"}}`)
	invalidReq, _ := http.NewRequest(http.MethodPost, invalidTS.URL+"/api/v1/auth/start-oauth", invalidBody)
	invalidReq.Header.Set("Content-Type", "application/json")
	invalidResp, err := http.DefaultClient.Do(invalidReq)
	if err != nil {
		t.Fatalf("invalid request: %v", err)
	}
	defer func() { _ = invalidResp.Body.Close() }()

	if invalidResp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(invalidResp.Body)
		t.Fatalf("expected 400, got %d: %s", invalidResp.StatusCode, body)
	}

	var invalidAuditRecord map[string]any
	if err := json.Unmarshal(invalidAuditBuf.Bytes(), &invalidAuditRecord); err != nil {
		t.Fatalf("parsing invalid audit record: %v\nraw: %s", err, invalidAuditBuf.String())
	}
	if invalidAuditRecord["target_id"] != "slack/default/default" {
		t.Fatalf("expected invalid audit target_id slack/default/default, got %v", invalidAuditRecord["target_id"])
	}
}

func TestIntegrationOAuthCallback(t *testing.T) {
	t.Parallel()

	const pendingSelectionPath = "/api/v1/auth/pending-connection"

	t.Run("connected", func(t *testing.T) {
		t.Parallel()

		var auditBuf bytes.Buffer
		svc := coretesting.NewStubServices(t)
		externalIdentityID := testExternalIdentityResourceID("slack_identity", "team:T123:user:U456")
		authzProvider := newMemoryAuthorizationProvider("memory-authorization")
		legacy, err := authorization.New(config.AuthorizationConfig{}, map[string]*config.ProviderEntry{}, nil, nil)
		if err != nil {
			t.Fatalf("authorization.New: %v", err)
		}
		authz := mustProviderBackedAuthorizer(t, legacy, authzProvider)

		handler := &testOAuthHandler{
			authorizationBaseURLVal: "https://slack.com/oauth/v2/authorize",
			exchangeCodeFn: func(_ context.Context, code string) (*core.TokenResponse, error) {
				if code == "good-code" {
					return &core.TokenResponse{
						AccessToken: "slack-token",
						Extra: map[string]any{
							"team":        map[string]any{"id": "T123"},
							"authed_user": map[string]any{"id": "U456"},
						},
					}, nil
				}
				return nil, fmt.Errorf("bad code")
			},
		}

		stub := &stubIntegrationWithAuthURL{
			StubIntegration: coretesting.StubIntegration{N: "slack"},
			authURL:         "https://slack.com/oauth/v2/authorize",
			postConnect:     testSlackPostConnect,
			connectionParams: map[string]core.ConnectionParamDef{
				"team_id": {
					Required: true,
					From:     "token_response",
					Field:    "team.id",
				},
				"user_id": {
					Required: true,
					From:     "token_response",
					Field:    "authed_user.id",
				},
			},
		}

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Auth = &coretesting.StubAuthProvider{
				N: "test",
				ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
					if token != "session-token" {
						return nil, fmt.Errorf("bad token")
					}
					return &core.UserIdentity{Email: "user@example.com"}, nil
				},
			}
			cfg.Providers = testutil.NewProviderRegistry(t, stub)
			cfg.DefaultConnection = map[string]string{"slack": testDefaultConnection}
			cfg.ConnectionAuth = testConnectionAuth("slack", handler)
			cfg.Services = svc
			cfg.Authorizer = authz
			cfg.AuthorizationProvider = authzProvider
			cfg.AuditSink = invocation.NewSlogAuditSink(&auditBuf)
		})
		testutil.CloseOnCleanup(t, ts)

		startBody := bytes.NewBufferString(`{"integration":"slack"}`)
		startReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/start-oauth", startBody)
		startReq.Header.Set("Content-Type", "application/json")
		startReq.Header.Set("Authorization", "Bearer session-token")
		startResp, err := http.DefaultClient.Do(startReq)
		if err != nil {
			t.Fatalf("start request: %v", err)
		}
		defer func() { _ = startResp.Body.Close() }()

		if startResp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 from start-oauth, got %d", startResp.StatusCode)
		}

		var startResult map[string]string
		if err := json.NewDecoder(startResp.Body).Decode(&startResult); err != nil {
			t.Fatalf("decoding start response: %v", err)
		}

		noRedirect := &http.Client{
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/auth/callback?code=good-code&state="+url.QueryEscape(startResult["state"]), nil)
		resp, err := noRedirect.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("expected 303, got %d", resp.StatusCode)
		}
		loc := resp.Header.Get("Location")
		if loc != "/integrations?connected=slack" {
			t.Fatalf("expected redirect to /integrations?connected=slack, got %q", loc)
		}
		u, _ := svc.Users.FindOrCreateUser(context.Background(), "user@example.com")
		tokens, _ := svc.Tokens.ListTokens(context.Background(), principal.UserSubjectID(u.ID))
		if len(tokens) == 0 {
			t.Fatal("expected token to be stored")
		}
		stored := tokens[0]
		if stored.Integration != "slack" {
			t.Fatalf("stored token integration = %q, want %q", stored.Integration, "slack")
		}
		if stored.AccessToken != "slack-token" {
			t.Fatalf("stored access token = %q, want %q", stored.AccessToken, "slack-token")
		}
		var metadata map[string]string
		if err := json.Unmarshal([]byte(stored.MetadataJSON), &metadata); err != nil {
			t.Fatalf("unmarshal metadata: %v", err)
		}
		if !reflect.DeepEqual(metadata, map[string]string{
			"team_id":                        "T123",
			"user_id":                        "U456",
			"gestalt.external_identity.type": "slack_identity",
			"gestalt.external_identity.id":   "team:T123:user:U456",
		}) {
			t.Fatalf("stored metadata = %+v", metadata)
		}
		respAuthz, err := authzProvider.ReadRelationships(context.Background(), &core.ReadRelationshipsRequest{
			Relation: authorization.ProviderExternalIdentityRelationAssume,
			Resource: &core.ResourceRef{
				Type: authorization.ProviderResourceTypeExternalIdentity,
				Id:   externalIdentityID,
			},
		})
		if err != nil {
			t.Fatalf("ReadRelationships after connect: %v", err)
		}
		if len(respAuthz.GetRelationships()) != 1 {
			t.Fatalf("relationships after connect = %+v, want one", respAuthz.GetRelationships())
		}
		if got := respAuthz.GetRelationships()[0].GetSubject().GetId(); got != principal.UserSubjectID(u.ID) {
			t.Fatalf("linked subject = %q, want %q", got, principal.UserSubjectID(u.ID))
		}
		if err := authz.ReloadAuthorizationState(context.Background()); err != nil {
			t.Fatalf("ReloadAuthorizationState after connect: %v", err)
		}
		respAuthz, err = authzProvider.ReadRelationships(context.Background(), &core.ReadRelationshipsRequest{
			Relation: authorization.ProviderExternalIdentityRelationAssume,
			Resource: &core.ResourceRef{
				Type: authorization.ProviderResourceTypeExternalIdentity,
				Id:   externalIdentityID,
			},
		})
		if err != nil {
			t.Fatalf("ReadRelationships after reload: %v", err)
		}
		if len(respAuthz.GetRelationships()) != 1 {
			t.Fatalf("relationships after reload = %+v, want one", respAuthz.GetRelationships())
		}

		lines := bytes.Split(bytes.TrimSpace(auditBuf.Bytes()), []byte("\n"))
		if len(lines) == 0 {
			t.Fatal("expected oauth callback audit record")
		}
		var auditRecord map[string]any
		if err := json.Unmarshal(lines[len(lines)-1], &auditRecord); err != nil {
			t.Fatalf("parsing audit record: %v\nraw: %s", err, auditBuf.String())
		}
		if auditRecord["operation"] != "connection.oauth.complete" {
			t.Fatalf("expected audit operation connection.oauth.complete, got %v", auditRecord["operation"])
		}
		if auditRecord["auth_source"] != "session" {
			t.Fatalf("expected audit auth_source session, got %v", auditRecord["auth_source"])
		}
		if subjectID, ok := auditRecord["subject_id"].(string); !ok || subjectID != principal.UserSubjectID(u.ID) {
			t.Fatalf("expected audit subject_id %q, got %v", principal.UserSubjectID(u.ID), auditRecord["subject_id"])
		}
		if _, ok := auditRecord["user_id"]; ok {
			t.Fatalf("expected emitted audit record to omit user_id, got %v", auditRecord["user_id"])
		}
		if auditRecord["target_kind"] != "connection" {
			t.Fatalf("expected audit target_kind connection, got %v", auditRecord["target_kind"])
		}
		if auditRecord["target_id"] != "slack/default/default" {
			t.Fatalf("expected audit target_id slack/default/default, got %v", auditRecord["target_id"])
		}
		if auditRecord["target_name"] != "default/default" {
			t.Fatalf("expected audit target_name default/default, got %v", auditRecord["target_name"])
		}
	})

	t.Run("selection_required", func(t *testing.T) {
		t.Parallel()

		discoverySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `[{"id":"site-a","name":"Site A","workspace":"alpha"},{"id":"site-b","name":"Site B","workspace":"beta"}]`)
		}))
		testutil.CloseOnCleanup(t, discoverySrv)

		svc := coretesting.NewStubServices(t)
		externalIdentityID := testExternalIdentityResourceID("slack_identity", "team:T123:user:U456")
		authzProvider := newMemoryAuthorizationProvider("memory-authorization")
		legacy, err := authorization.New(config.AuthorizationConfig{}, map[string]*config.ProviderEntry{}, nil, nil)
		if err != nil {
			t.Fatalf("authorization.New: %v", err)
		}
		authz := mustProviderBackedAuthorizer(t, legacy, authzProvider)
		handler := &testOAuthHandler{
			authorizationBaseURLVal: "https://slack.com/oauth/v2/authorize",
			exchangeCodeFn: func(_ context.Context, code string) (*core.TokenResponse, error) {
				if code == "good-code" {
					return &core.TokenResponse{
						AccessToken: "slack-token",
						Extra: map[string]any{
							"team":        map[string]any{"id": "T123"},
							"authed_user": map[string]any{"id": "U456"},
						},
					}, nil
				}
				return nil, fmt.Errorf("bad code")
			},
		}

		stub := &stubDiscoveringProvider{
			StubIntegration: coretesting.StubIntegration{N: "slack"},
			discovery: &core.DiscoveryConfig{
				URL:      discoverySrv.URL,
				IDPath:   "id",
				NamePath: "name",
				Metadata: map[string]string{"workspace": "workspace"},
			},
			postConnect: testSlackPostConnect,
			connectionParams: map[string]core.ConnectionParamDef{
				"team_id": {
					Required: true,
					From:     "token_response",
					Field:    "team.id",
				},
				"user_id": {
					Required: true,
					From:     "token_response",
					Field:    "authed_user.id",
				},
			},
		}

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Auth = &coretesting.StubAuthProvider{
				N: "test",
				ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
					if token != "cli-api-token" {
						return nil, fmt.Errorf("bad token")
					}
					return &core.UserIdentity{Email: "cli@test.local"}, nil
				},
			}
			cfg.Providers = testutil.NewProviderRegistry(t, stub)
			cfg.DefaultConnection = map[string]string{"slack": testDefaultConnection}
			cfg.ConnectionAuth = testConnectionAuth("slack", handler)
			cfg.Services = svc
			cfg.Authorizer = authz
			cfg.AuthorizationProvider = authzProvider
		})
		testutil.CloseOnCleanup(t, ts)

		startBody := bytes.NewBufferString(`{"integration":"slack"}`)
		startReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/start-oauth", startBody)
		startReq.Header.Set("Content-Type", "application/json")
		startReq.Header.Set("Authorization", "Bearer cli-api-token")
		startResp, err := http.DefaultClient.Do(startReq)
		if err != nil {
			t.Fatalf("start request: %v", err)
		}
		defer func() { _ = startResp.Body.Close() }()

		var startResult map[string]string
		if err := json.NewDecoder(startResp.Body).Decode(&startResult); err != nil {
			t.Fatalf("decoding start response: %v", err)
		}

		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatalf("cookie jar: %v", err)
		}
		noRedirect := &http.Client{
			Jar: jar,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/auth/callback?code=good-code&state="+url.QueryEscape(startResult["state"]), nil)
		resp, err := noRedirect.Do(req)
		if err != nil {
			t.Fatalf("callback request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		text := string(body)
		if !strings.Contains(text, "Select a slack connection") {
			t.Fatalf("expected selection page, got %q", text)
		}
		if !strings.Contains(text, "Site A") || !strings.Contains(text, "Site B") {
			t.Fatalf("expected both candidates in page, got %q", text)
		}
		if !strings.Contains(text, pendingSelectionPath) {
			t.Fatalf("expected selection form action in page, got %q", text)
		}
		if !strings.Contains(text, "name=\"pending_token\"") {
			t.Fatalf("expected pending token hidden input in page, got %q", text)
		}
		if !strings.Contains(text, "name=\"candidate_index\"") {
			t.Fatalf("expected candidate index hidden input in page, got %q", text)
		}
		selectionURL, err := url.Parse(ts.URL + pendingSelectionPath)
		if err != nil {
			t.Fatalf("parse selection url: %v", err)
		}
		cookies := jar.Cookies(selectionURL)
		foundPendingCookie := false
		for _, cookie := range cookies {
			if cookie.Name == "pending_connection_state" {
				foundPendingCookie = true
				break
			}
		}
		if !foundPendingCookie {
			t.Fatal("expected pending connection cookie to be set on callback response")
		}

		form := url.Values{
			"pending_token":   {extractHiddenInputValue(t, text, "pending_token")},
			"candidate_index": {"1"},
		}
		selectReq, _ := http.NewRequest(http.MethodPost, ts.URL+pendingSelectionPath, strings.NewReader(form.Encode()))
		selectReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		selectResp, err := noRedirect.Do(selectReq)
		if err != nil {
			t.Fatalf("select request: %v", err)
		}
		defer func() { _ = selectResp.Body.Close() }()

		if selectResp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", selectResp.StatusCode)
		}
		u, _ := svc.Users.FindOrCreateUser(context.Background(), "cli@test.local")
		tokens, _ := svc.Tokens.ListTokens(context.Background(), principal.UserSubjectID(u.ID))
		if len(tokens) == 0 {
			t.Fatal("expected token to be stored after selection")
		}
		stored := tokens[0]
		if stored.Integration != "slack" {
			t.Fatalf("stored token integration = %q, want %q", stored.Integration, "slack")
		}
		var metadata map[string]string
		if err := json.Unmarshal([]byte(stored.MetadataJSON), &metadata); err != nil {
			t.Fatalf("unmarshal metadata: %v", err)
		}
		if !reflect.DeepEqual(metadata, map[string]string{
			"team_id":                        "T123",
			"user_id":                        "U456",
			"workspace":                      "beta",
			"gestalt.external_identity.type": "slack_identity",
			"gestalt.external_identity.id":   "team:T123:user:U456",
		}) {
			t.Fatalf("stored metadata = %+v", metadata)
		}
		respAuthz, err := authzProvider.ReadRelationships(context.Background(), &core.ReadRelationshipsRequest{
			Relation: authorization.ProviderExternalIdentityRelationAssume,
			Resource: &core.ResourceRef{
				Type: authorization.ProviderResourceTypeExternalIdentity,
				Id:   externalIdentityID,
			},
		})
		if err != nil {
			t.Fatalf("ReadRelationships after pending selection: %v", err)
		}
		if len(respAuthz.GetRelationships()) != 1 {
			t.Fatalf("relationships after pending selection = %+v, want one", respAuthz.GetRelationships())
		}
		if err := authz.ReloadAuthorizationState(context.Background()); err != nil {
			t.Fatalf("ReloadAuthorizationState after pending selection: %v", err)
		}
		respAuthz, err = authzProvider.ReadRelationships(context.Background(), &core.ReadRelationshipsRequest{
			Relation: authorization.ProviderExternalIdentityRelationAssume,
			Resource: &core.ResourceRef{
				Type: authorization.ProviderResourceTypeExternalIdentity,
				Id:   externalIdentityID,
			},
		})
		if err != nil {
			t.Fatalf("ReadRelationships after pending selection reload: %v", err)
		}
		if len(respAuthz.GetRelationships()) != 1 {
			t.Fatalf("relationships after pending selection reload = %+v, want one", respAuthz.GetRelationships())
		}
	})

	t.Run("slack identity already linked", func(t *testing.T) {
		t.Parallel()

		svc := coretesting.NewStubServices(t)
		externalIdentityID := testExternalIdentityResourceID("slack_identity", "team:T123:user:U456")
		authzProvider := newMemoryAuthorizationProvider("memory-authorization")
		legacy, err := authorization.New(config.AuthorizationConfig{}, map[string]*config.ProviderEntry{}, nil, nil)
		if err != nil {
			t.Fatalf("authorization.New: %v", err)
		}
		authz := mustProviderBackedAuthorizer(t, legacy, authzProvider)

		handler := &testOAuthHandler{
			authorizationBaseURLVal: "https://slack.com/oauth/v2/authorize",
			exchangeCodeFn: func(_ context.Context, code string) (*core.TokenResponse, error) {
				if code == "good-code" {
					return &core.TokenResponse{
						AccessToken: "slack-token",
						Extra: map[string]any{
							"team":        map[string]any{"id": "T123"},
							"authed_user": map[string]any{"id": "U456"},
						},
					}, nil
				}
				return nil, fmt.Errorf("bad code")
			},
		}

		stub := &stubIntegrationWithAuthURL{
			StubIntegration: coretesting.StubIntegration{N: "slack"},
			authURL:         "https://slack.com/oauth/v2/authorize",
			postConnect:     testSlackPostConnect,
			connectionParams: map[string]core.ConnectionParamDef{
				"team_id": {
					Required: true,
					From:     "token_response",
					Field:    "team.id",
				},
				"user_id": {
					Required: true,
					From:     "token_response",
					Field:    "authed_user.id",
				},
			},
		}

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Auth = &coretesting.StubAuthProvider{
				N: "test",
				ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
					switch token {
					case "admin-session":
						return &core.UserIdentity{Email: "admin@example.test"}, nil
					case "viewer-session":
						return &core.UserIdentity{Email: "viewer@example.test"}, nil
					default:
						return nil, fmt.Errorf("bad token")
					}
				},
			}
			cfg.Providers = testutil.NewProviderRegistry(t, stub)
			cfg.DefaultConnection = map[string]string{"slack": testDefaultConnection}
			cfg.ConnectionAuth = testConnectionAuth("slack", handler)
			cfg.Services = svc
			cfg.Authorizer = authz
			cfg.AuthorizationProvider = authzProvider
		})
		testutil.CloseOnCleanup(t, ts)

		runConnect := func(session string) *http.Response {
			t.Helper()
			startBody := bytes.NewBufferString(`{"integration":"slack"}`)
			startReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/start-oauth", startBody)
			startReq.Header.Set("Content-Type", "application/json")
			startReq.Header.Set("Authorization", "Bearer "+session)
			startResp, err := http.DefaultClient.Do(startReq)
			if err != nil {
				t.Fatalf("start request: %v", err)
			}
			defer func() { _ = startResp.Body.Close() }()
			if startResp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(startResp.Body)
				t.Fatalf("expected 200 from start-oauth, got %d: %s", startResp.StatusCode, strings.TrimSpace(string(body)))
			}
			var startResult map[string]string
			if err := json.NewDecoder(startResp.Body).Decode(&startResult); err != nil {
				t.Fatalf("decoding start response: %v", err)
			}
			noRedirect := &http.Client{
				CheckRedirect: func(*http.Request, []*http.Request) error {
					return http.ErrUseLastResponse
				},
			}
			req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/auth/callback?code=good-code&state="+url.QueryEscape(startResult["state"]), nil)
			resp, err := noRedirect.Do(req)
			if err != nil {
				t.Fatalf("callback request: %v", err)
			}
			return resp
		}

		adminResp := runConnect("admin-session")
		defer func() { _ = adminResp.Body.Close() }()
		if adminResp.StatusCode != http.StatusSeeOther {
			body, _ := io.ReadAll(adminResp.Body)
			t.Fatalf("admin callback status = %d, want %d: %s", adminResp.StatusCode, http.StatusSeeOther, strings.TrimSpace(string(body)))
		}

		viewerResp := runConnect("viewer-session")
		defer func() { _ = viewerResp.Body.Close() }()
		if viewerResp.StatusCode != http.StatusBadGateway {
			body, _ := io.ReadAll(viewerResp.Body)
			t.Fatalf("viewer callback status = %d, want %d: %s", viewerResp.StatusCode, http.StatusBadGateway, strings.TrimSpace(string(body)))
		}

		admin, _ := svc.Users.FindOrCreateUser(context.Background(), "admin@example.test")
		viewer, _ := svc.Users.FindOrCreateUser(context.Background(), "viewer@example.test")
		adminTokens, _ := svc.Tokens.ListTokens(context.Background(), principal.UserSubjectID(admin.ID))
		if len(adminTokens) != 1 {
			t.Fatalf("expected one admin token, got %d", len(adminTokens))
		}
		viewerTokens, _ := svc.Tokens.ListTokens(context.Background(), principal.UserSubjectID(viewer.ID))
		if len(viewerTokens) != 0 {
			t.Fatalf("expected viewer token rollback, got %d", len(viewerTokens))
		}

		respAuthz, err := authzProvider.ReadRelationships(context.Background(), &core.ReadRelationshipsRequest{
			Relation: authorization.ProviderExternalIdentityRelationAssume,
			Resource: &core.ResourceRef{
				Type: authorization.ProviderResourceTypeExternalIdentity,
				Id:   externalIdentityID,
			},
		})
		if err != nil {
			t.Fatalf("ReadRelationships after conflict: %v", err)
		}
		if len(respAuthz.GetRelationships()) != 1 {
			t.Fatalf("relationships after conflict = %+v, want one", respAuthz.GetRelationships())
		}
		if got := respAuthz.GetRelationships()[0].GetSubject().GetId(); got != principal.UserSubjectID(admin.ID) {
			t.Fatalf("linked subject after conflict = %q, want %q", got, principal.UserSubjectID(admin.ID))
		}
	})
}

func TestIntegrationOAuthCallback_InvalidState(t *testing.T) {
	t.Parallel()

	stub := &coretesting.StubIntegration{N: "slack"}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
	})
	testutil.CloseOnCleanup(t, ts)

	t.Run("api response stays json", func(t *testing.T) {
		t.Parallel()

		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/auth/callback?code=good-code&state=not-valid", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}

		var result map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decoding: %v", err)
		}
		if result["error"] == "" {
			t.Fatal("expected error response")
		}
	})

	t.Run("browser response uses html page", func(t *testing.T) {
		t.Parallel()

		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/auth/callback?code=good-code&state=not-valid", nil)
		req.Header.Set("Accept", "text/html,application/xhtml+xml")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
		if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "text/html") {
			t.Fatalf("content-type = %q, want HTML", got)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("reading body: %v", err)
		}
		html := string(body)
		if !strings.Contains(html, "Connection expired") {
			t.Fatalf("expected HTML response to include title, got %q", html)
		}
		if !strings.Contains(html, "Start a new connection from Integrations.") {
			t.Fatalf("expected HTML response to include recovery guidance, got %q", html)
		}
		if !strings.Contains(html, `href="/integrations"`) {
			t.Fatalf("expected HTML response to link back to integrations, got %q", html)
		}
	})
}

func TestCreateAndListAPITokens(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Services = coretesting.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"name":"my-token"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/tokens", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if result["token"] == "" {
		t.Fatal("expected non-empty token in response")
	}
	if result["name"] != "my-token" {
		t.Fatalf("expected name my-token, got %q", result["name"])
	}
}

func TestListAPITokensListsOwnedUserRecords(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"name":"owned-token"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/tokens", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("create status = %d, want %d: %s", resp.StatusCode, http.StatusCreated, respBody)
	}

	var createResp struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&createResp); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/v1/tokens", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("list status = %d, want %d: %s", resp.StatusCode, http.StatusOK, respBody)
	}

	var tokens []struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(tokens) != 1 || tokens[0].ID != createResp.ID {
		t.Fatalf("tokens = %+v, want only %q", tokens, createResp.ID)
	}
}

func TestRevokeAPIToken(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	var auditBuf bytes.Buffer
	ctx := context.Background()
	exp := time.Now().Add(24 * time.Hour)
	_ = svc.APITokens.StoreAPIToken(ctx, &core.APIToken{
		ID: "tok-123", OwnerKind: core.APITokenOwnerKindUser, OwnerID: u.ID, CredentialSubjectID: principal.UserSubjectID(u.ID), Name: "test", HashedToken: "h1", ExpiresAt: &exp,
	})

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.AuditSink = invocation.NewSlogAuditSink(&auditBuf)
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/tokens/tok-123", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if result["status"] != "revoked" {
		t.Fatalf("expected revoked, got %q", result["status"])
	}

	var auditRecord map[string]any
	if err := json.Unmarshal(auditBuf.Bytes(), &auditRecord); err != nil {
		t.Fatalf("parsing audit record: %v\nraw: %s", err, auditBuf.String())
	}
	if auditRecord["target_kind"] != "api_token" {
		t.Fatalf("expected audit target_kind api_token, got %v", auditRecord["target_kind"])
	}
	if auditRecord["target_id"] != "tok-123" {
		t.Fatalf("expected audit target_id tok-123, got %v", auditRecord["target_id"])
	}
}

func TestRevokeAPIToken_WrongUser(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Services = coretesting.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/tokens/tok-owned-by-a", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 404, got %d: %s", resp.StatusCode, body)
	}
}

func TestCreateAPIToken_DefaultExpiry(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	svc := coretesting.NewStubServices(t)
	existing := seedUser(t, svc, "user@example.com")
	var auditBuf bytes.Buffer
	auditSink := invocation.NewSlogAuditSink(&auditBuf)
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "session-token" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: "User@Example.com"}, nil
			},
		}
		cfg.Now = func() time.Time { return fixedNow }
		cfg.AuditSink = auditSink
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"name":"expiry-test"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/tokens", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "session-token"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, respBody)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	tokenID, ok := result["id"].(string)
	if !ok || tokenID == "" {
		t.Fatalf("expected non-empty id in response, got %v", result["id"])
	}
	expiresAtRaw, ok := result["expiresAt"]
	if !ok || expiresAtRaw == nil {
		t.Fatal("expected expiresAt in response, got nil")
	}
	expiresAtStr, ok := expiresAtRaw.(string)
	if !ok {
		t.Fatalf("expected expiresAt to be a string, got %T", expiresAtRaw)
	}
	expiresAt, err := time.Parse(time.RFC3339, expiresAtStr)
	if err != nil {
		t.Fatalf("parsing expiresAt: %v", err)
	}
	expected := fixedNow.Add(30 * 24 * time.Hour).UTC().Truncate(time.Second)
	if !expiresAt.Equal(expected) {
		t.Fatalf("expected expiresAt %v, got %v", expected, expiresAt)
	}

	tokens, err := svc.APITokens.ListAPITokens(context.Background(), existing.ID)
	if err != nil {
		t.Fatalf("list api tokens: %v", err)
	}
	if len(tokens) != 1 {
		t.Fatalf("expected 1 API token for canonical user, got %d", len(tokens))
	}
	if tokens[0].ID != tokenID {
		t.Fatalf("expected stored token ID %q, got %q", tokenID, tokens[0].ID)
	}

	var auditRecord map[string]any
	if err := json.Unmarshal(auditBuf.Bytes(), &auditRecord); err != nil {
		t.Fatalf("parsing audit record: %v\nraw: %s", err, auditBuf.String())
	}
	if auditRecord["operation"] != "api_token.create" {
		t.Fatalf("expected audit operation api_token.create, got %v", auditRecord["operation"])
	}
	if auditRecord["source"] != "http" {
		t.Fatalf("expected audit source http, got %v", auditRecord["source"])
	}
	if auditRecord["auth_source"] != "session" {
		t.Fatalf("expected audit auth_source session, got %v", auditRecord["auth_source"])
	}
	if subjectID, ok := auditRecord["subject_id"].(string); !ok || subjectID != principal.UserSubjectID(existing.ID) {
		t.Fatalf("expected audit subject_id %q, got %v", principal.UserSubjectID(existing.ID), auditRecord["subject_id"])
	}
	if _, ok := auditRecord["user_id"]; ok {
		t.Fatalf("expected emitted audit record to omit user_id, got %v", auditRecord["user_id"])
	}
	if auditRecord["allowed"] != true {
		t.Fatalf("expected audit allowed=true, got %v", auditRecord["allowed"])
	}
	if auditRecord["target_kind"] != "api_token" {
		t.Fatalf("expected audit target_kind api_token, got %v", auditRecord["target_kind"])
	}
	if auditRecord["target_id"] != tokenID {
		t.Fatalf("expected audit target_id %q, got %v", tokenID, auditRecord["target_id"])
	}
	if auditRecord["target_name"] != "expiry-test" {
		t.Fatalf("expected audit target_name expiry-test, got %v", auditRecord["target_name"])
	}
}

func TestCreateAPIToken_AuditResolveUserFailure(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	stubDB := svc.DB.(*coretesting.StubIndexedDB)
	var auditBuf bytes.Buffer
	auditSink := invocation.NewSlogAuditSink(&auditBuf)
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "session-token" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: "user@example.com"}, nil
			},
		}
		cfg.AuditSink = auditSink
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	stubDB.Err = fmt.Errorf("database unavailable")

	body := bytes.NewBufferString(`{"name":"failure-test"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/tokens", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "session-token"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	stubDB.Err = nil

	if resp.StatusCode != http.StatusInternalServerError {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 500, got %d: %s", resp.StatusCode, respBody)
	}

	var auditRecord map[string]any
	if err := json.Unmarshal(auditBuf.Bytes(), &auditRecord); err != nil {
		t.Fatalf("parsing audit record: %v\nraw: %s", err, auditBuf.String())
	}
	if auditRecord["operation"] != "api_token.create" {
		t.Fatalf("expected audit operation api_token.create, got %v", auditRecord["operation"])
	}
	if auditRecord["auth_source"] != "session" {
		t.Fatalf("expected audit auth_source session, got %v", auditRecord["auth_source"])
	}
	if auditRecord["allowed"] != false {
		t.Fatalf("expected audit allowed=false, got %v", auditRecord["allowed"])
	}
	if auditRecord["error"] != "failed to resolve user" {
		t.Fatalf("expected audit error failed to resolve user, got %v", auditRecord["error"])
	}
}

func TestCreateAPIToken_ConfigurableTTL(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	customTTL := 7 * 24 * time.Hour

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Now = func() time.Time { return fixedNow }
		cfg.APITokenTTL = customTTL
		cfg.Services = coretesting.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"name":"ttl-test"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/tokens", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, respBody)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	expiresAtStr, ok := result["expiresAt"].(string)
	if !ok {
		t.Fatal("expected expiresAt string in response")
	}
	expiresAt, err := time.Parse(time.RFC3339, expiresAtStr)
	if err != nil {
		t.Fatalf("parsing expiresAt: %v", err)
	}
	expected := fixedNow.Add(customTTL).UTC().Truncate(time.Second)
	if !expiresAt.Equal(expected) {
		t.Fatalf("expected expiresAt %v, got %v", expected, expiresAt)
	}
}

func TestRevokeAllAPITokens(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	var auditBuf bytes.Buffer
	ctx := context.Background()
	exp := time.Now().Add(24 * time.Hour)
	for i, name := range []string{"tok-a", "tok-b", "tok-c"} {
		_ = svc.APITokens.StoreAPIToken(ctx, &core.APIToken{
			ID: name, OwnerKind: core.APITokenOwnerKindUser, OwnerID: u.ID, CredentialSubjectID: principal.UserSubjectID(u.ID), Name: fmt.Sprintf("token-%d", i),
			HashedToken: fmt.Sprintf("h%d", i), ExpiresAt: &exp,
		})
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.AuditSink = invocation.NewSlogAuditSink(&auditBuf)
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/tokens", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if result["status"] != "revoked" {
		t.Fatalf("expected status revoked, got %q", result["status"])
	}
	if count, ok := result["count"].(float64); !ok || count != 3 {
		t.Fatalf("expected count 3, got %v", result["count"])
	}

	var auditRecord map[string]any
	if err := json.Unmarshal(auditBuf.Bytes(), &auditRecord); err != nil {
		t.Fatalf("parsing audit record: %v\nraw: %s", err, auditBuf.String())
	}
	if auditRecord["target_kind"] != "api_token_collection" {
		t.Fatalf("expected audit target_kind api_token_collection, got %v", auditRecord["target_kind"])
	}
}

func TestRevokeAllAPITokens_NoneExist(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Services = coretesting.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/tokens", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if count, ok := result["count"].(float64); !ok || count != 0 {
		t.Fatalf("expected count 0, got %v", result["count"])
	}
}

func TestExecuteOperation_POST(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok1", SubjectID: principal.UserSubjectID(u.ID), Integration: "test-int",
		Connection: "", Instance: "default", AccessToken: "test-token",
	})

	fullStub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N: "test-int",
			ExecuteFn: func(_ context.Context, op string, params map[string]any, _ string) (*core.OperationResult, error) {
				text, _ := params["text"].(string)
				return &core.OperationResult{
					Status: http.StatusOK,
					Body:   fmt.Sprintf(`{"text":%q}`, text),
				}, nil
			},
		},
		ops: []core.Operation{
			{Name: "send", Description: "Send", Method: http.MethodPost},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, fullStub)
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"text":"hello"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/test-int/send", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if result["text"] != "hello" {
		t.Fatalf("expected hello, got %q", result["text"])
	}
}

func TestHostedHTTPBinding_HMACAckDispatchesOperationAndRejectsReplay(t *testing.T) {
	t.Setenv("REQUEST_SIGNING_SECRET", "super-secret")

	type bindingInvocation struct {
		Params   map[string]any
		Workflow map[string]any
	}

	invocations := make(chan bindingInvocation, 1)
	provider := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "signed",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(ctx context.Context, op string, params map[string]any, _ string) (*core.OperationResult, error) {
				if op != "handle_command" {
					t.Fatalf("operation = %q, want %q", op, "handle_command")
				}
				invocations <- bindingInvocation{
					Params:   cloneAnyMapForTest(params),
					Workflow: invocation.WorkflowContextFromContext(ctx),
				}
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		},
		ops: []core.Operation{{Name: "handle_command", Method: http.MethodPost}},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, provider)
		cfg.PluginDefs = map[string]*config.ProviderEntry{
			"signed": {
				SecuritySchemes: map[string]*config.HTTPSecurityScheme{
					"signed": {
						Type: providermanifestv1.HTTPSecuritySchemeTypeHMAC,
						Secret: &providermanifestv1.HTTPSecretRef{
							Env: "REQUEST_SIGNING_SECRET",
						},
						SignatureHeader: "X-Request-Signature",
						SignaturePrefix: "v0=",
						PayloadTemplate: "v0:{header:X-Request-Timestamp}:{raw_body}",
						TimestampHeader: "X-Request-Timestamp",
						MaxAgeSeconds:   300,
					},
				},
				HTTP: map[string]*config.HTTPBinding{
					"command": {
						Path:   "/command",
						Method: http.MethodPost,
						RequestBody: &providermanifestv1.HTTPRequestBody{
							Required: true,
							Content: map[string]*providermanifestv1.HTTPMediaType{
								"application/x-www-form-urlencoded": {},
							},
						},
						Security: "signed",
						Target:   "handle_command",
						Ack: &providermanifestv1.HTTPAck{
							Body: map[string]any{
								"status": "accepted",
							},
						},
					},
				},
			},
		}
		cfg.Authorizer = mustAuthorizer(t, config.AuthorizationConfig{}, cfg.Providers, cfg.PluginDefs, nil)
	})
	testutil.CloseOnCleanup(t, ts)

	body := "text=hello&callback_url=https%3A%2F%2Fhooks.example.test%2Fresponse"
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	signature := httpBindingTestSignature("super-secret", "v0:"+timestamp+":"+body)

	makeRequest := func() *http.Request {
		req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/signed/command?source=query", strings.NewReader(body))
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Request-Timestamp", timestamp)
		req.Header.Set("X-Request-Signature", signature)
		return req
	}

	resp, err := http.DefaultClient.Do(makeRequest())
	if err != nil {
		t.Fatalf("http binding request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var ack map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&ack); err != nil {
		t.Fatalf("decode ack: %v", err)
	}
	if got, want := ack["status"], "accepted"; got != want {
		t.Fatalf("status = %#v, want %q", got, want)
	}

	select {
	case invocation := <-invocations:
		if got, want := invocation.Params["text"], "hello"; got != want {
			t.Fatalf("params[text] = %#v, want %q", got, want)
		}
		if got, want := invocation.Params["callback_url"], "https://hooks.example.test/response"; got != want {
			t.Fatalf("params[callback_url] = %#v, want %q", got, want)
		}
		if got, want := invocation.Params["source"], "query"; got != want {
			t.Fatalf("params[source] = %#v, want %q", got, want)
		}
		httpCtx, ok := invocation.Workflow["http"].(map[string]any)
		if !ok {
			t.Fatalf("workflow http context = %#v", invocation.Workflow)
		}
		if got, want := httpCtx["name"], "command"; got != want {
			t.Fatalf("http context name = %#v, want %q", got, want)
		}
		if got, want := httpCtx["path"], "/api/v1/signed/command"; got != want {
			t.Fatalf("http context path = %#v, want %q", got, want)
		}
		if got, want := httpCtx["security"], "signed"; got != want {
			t.Fatalf("http context security = %#v, want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for async http binding dispatch")
	}

	resp, err = http.DefaultClient.Do(makeRequest())
	if err != nil {
		t.Fatalf("duplicate http binding request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("duplicate status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var duplicateAck map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&duplicateAck); err != nil {
		t.Fatalf("decode duplicate ack: %v", err)
	}
	if got, want := duplicateAck["status"], "accepted"; got != want {
		t.Fatalf("duplicate status = %#v, want %q", got, want)
	}
	select {
	case invocation := <-invocations:
		t.Fatalf("unexpected duplicate async dispatch: %#v", invocation)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestHostedHTTPBinding_MergesManifestAndConfigOverrides(t *testing.T) {
	t.Setenv("REQUEST_SIGNING_SECRET", "super-secret")

	invocations := make(chan map[string]any, 1)
	provider := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "signed",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(_ context.Context, op string, params map[string]any, _ string) (*core.OperationResult, error) {
				if op != "handle_command" {
					t.Fatalf("operation = %q, want %q", op, "handle_command")
				}
				invocations <- cloneAnyMapForTest(params)
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		},
		ops: []core.Operation{{Name: "handle_command", Method: http.MethodPost}},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, provider)
		cfg.PluginDefs = map[string]*config.ProviderEntry{
			"signed": {
				ResolvedManifest: &providermanifestv1.Manifest{
					Spec: &providermanifestv1.Spec{
						SecuritySchemes: map[string]*providermanifestv1.HTTPSecurityScheme{
							"signed": {
								Type:            providermanifestv1.HTTPSecuritySchemeTypeHMAC,
								SignatureHeader: "X-Request-Signature",
								SignaturePrefix: "v0=",
								PayloadTemplate: "v0:{header:X-Request-Timestamp}:{raw_body}",
								TimestampHeader: "X-Request-Timestamp",
								MaxAgeSeconds:   300,
							},
						},
						HTTP: map[string]*providermanifestv1.HTTPBinding{
							"command": {
								Path:   "/command",
								Method: http.MethodPost,
								RequestBody: &providermanifestv1.HTTPRequestBody{
									Required: true,
									Content: map[string]*providermanifestv1.HTTPMediaType{
										"application/x-www-form-urlencoded": {},
									},
								},
								Security: "signed",
								Target:   "handle_command",
							},
						},
					},
				},
				SecuritySchemes: map[string]*config.HTTPSecurityScheme{
					"signed": {
						Secret: &providermanifestv1.HTTPSecretRef{Env: "REQUEST_SIGNING_SECRET"},
					},
				},
				HTTP: map[string]*config.HTTPBinding{
					"command": {
						Ack: &providermanifestv1.HTTPAck{
							Body: map[string]any{
								"status": "queued",
							},
						},
					},
				},
			},
		}
		cfg.Authorizer = mustAuthorizer(t, config.AuthorizationConfig{}, cfg.Providers, cfg.PluginDefs, nil)
	})
	testutil.CloseOnCleanup(t, ts)

	body := "text=hello"
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	signature := httpBindingTestSignature("super-secret", "v0:"+timestamp+":"+body)

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/signed/command", strings.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Request-Timestamp", timestamp)
	req.Header.Set("X-Request-Signature", signature)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http binding request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var ack map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&ack); err != nil {
		t.Fatalf("decode ack: %v", err)
	}
	if got, want := ack["status"], "queued"; got != want {
		t.Fatalf("ack status = %#v, want %q", got, want)
	}

	select {
	case params := <-invocations:
		if got, want := params["text"], "hello"; got != want {
			t.Fatalf("params[text] = %#v, want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for merged hosted http binding invocation")
	}
}

func TestHostedHTTPBinding_HMACSyncRetriesReinvokeOperation(t *testing.T) {
	t.Setenv("REQUEST_SIGNING_SECRET", "super-secret")

	invocations := make(chan map[string]any, 2)
	provider := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "signed",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(_ context.Context, op string, params map[string]any, _ string) (*core.OperationResult, error) {
				if op != "handle_command" {
					t.Fatalf("operation = %q, want %q", op, "handle_command")
				}
				invocations <- cloneAnyMapForTest(params)
				return &core.OperationResult{Status: http.StatusOK, Body: `{"text":"pong"}`}, nil
			},
		},
		ops: []core.Operation{{Name: "handle_command", Method: http.MethodPost}},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, provider)
		cfg.PluginDefs = map[string]*config.ProviderEntry{
			"signed": {
				SecuritySchemes: map[string]*config.HTTPSecurityScheme{
					"signed": {
						Type:            providermanifestv1.HTTPSecuritySchemeTypeHMAC,
						Secret:          &providermanifestv1.HTTPSecretRef{Env: "REQUEST_SIGNING_SECRET"},
						SignatureHeader: "X-Request-Signature",
						SignaturePrefix: "v0=",
						PayloadTemplate: "v0:{header:X-Request-Timestamp}:{raw_body}",
						TimestampHeader: "X-Request-Timestamp",
						MaxAgeSeconds:   300,
					},
				},
				HTTP: map[string]*config.HTTPBinding{
					"command": {
						Path:   "/command",
						Method: http.MethodPost,
						RequestBody: &providermanifestv1.HTTPRequestBody{
							Required: true,
							Content: map[string]*providermanifestv1.HTTPMediaType{
								"application/x-www-form-urlencoded": {},
							},
						},
						Security: "signed",
						Target:   "handle_command",
					},
				},
			},
		}
		cfg.Authorizer = mustAuthorizer(t, config.AuthorizationConfig{}, cfg.Providers, cfg.PluginDefs, nil)
	})
	testutil.CloseOnCleanup(t, ts)

	body := "text=ping"
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	signature := httpBindingTestSignature("super-secret", "v0:"+timestamp+":"+body)

	makeRequest := func() *http.Request {
		req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/signed/command", strings.NewReader(body))
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Request-Timestamp", timestamp)
		req.Header.Set("X-Request-Signature", signature)
		return req
	}

	for i := 0; i < 2; i++ {
		resp, err := http.DefaultClient.Do(makeRequest())
		if err != nil {
			t.Fatalf("sync hmac request %d: %v", i, err)
		}
		var result map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			_ = resp.Body.Close()
			t.Fatalf("decode sync result %d: %v", i, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("sync status %d = %d, want %d", i, resp.StatusCode, http.StatusOK)
		}
		if got, want := result["text"], "pong"; got != want {
			t.Fatalf("sync result %d text = %#v, want %q", i, got, want)
		}
	}

	for i := 0; i < 2; i++ {
		select {
		case params := <-invocations:
			if got, want := params["text"], "ping"; got != want {
				t.Fatalf("invocation %d text = %#v, want %q", i, got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for sync invocation %d", i)
		}
	}
}

func TestHostedHTTPBinding_APIKeySyncInvokesOperation(t *testing.T) {
	t.Parallel()

	type bindingInvocation struct {
		Params   map[string]any
		Workflow map[string]any
	}

	invocations := make(chan bindingInvocation, 1)
	provider := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "events",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(ctx context.Context, op string, params map[string]any, _ string) (*core.OperationResult, error) {
				if op != "handle_event" {
					t.Fatalf("operation = %q, want %q", op, "handle_event")
				}
				invocations <- bindingInvocation{
					Params:   cloneAnyMapForTest(params),
					Workflow: invocation.WorkflowContextFromContext(ctx),
				}
				return &core.OperationResult{Status: http.StatusCreated, Body: `{"accepted":true}`}, nil
			},
		},
		ops: []core.Operation{{Name: "handle_event", Method: http.MethodPost}},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, provider)
		cfg.PluginDefs = map[string]*config.ProviderEntry{
			"events": {
				SecuritySchemes: map[string]*config.HTTPSecurityScheme{
					"eventKey": {
						Type:   providermanifestv1.HTTPSecuritySchemeTypeAPIKey,
						Name:   "X-Webhook-Key",
						In:     providermanifestv1.HTTPInHeader,
						Secret: &providermanifestv1.HTTPSecretRef{Secret: "shared-key"},
					},
				},
				HTTP: map[string]*config.HTTPBinding{
					"event": {
						Path:   "/event",
						Method: http.MethodPost,
						RequestBody: &providermanifestv1.HTTPRequestBody{
							Required: true,
							Content: map[string]*providermanifestv1.HTTPMediaType{
								"application/json": {},
							},
						},
						Security: "eventKey",
						Target:   "handle_event",
					},
				},
			},
		}
		cfg.Authorizer = mustAuthorizer(t, config.AuthorizationConfig{}, cfg.Providers, cfg.PluginDefs, nil)
	})
	testutil.CloseOnCleanup(t, ts)

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/events/event?source=query", strings.NewReader(`{"event":"opened"}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Key", "shared-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http binding request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if got, want := result["accepted"], true; got != want {
		t.Fatalf("accepted = %#v, want %#v", got, want)
	}

	select {
	case invocation := <-invocations:
		if got, want := invocation.Params["event"], "opened"; got != want {
			t.Fatalf("params[event] = %#v, want %q", got, want)
		}
		if got, want := invocation.Params["source"], "query"; got != want {
			t.Fatalf("params[source] = %#v, want %q", got, want)
		}
		httpCtx, ok := invocation.Workflow["http"].(map[string]any)
		if !ok {
			t.Fatalf("workflow http context = %#v", invocation.Workflow)
		}
		if got, want := httpCtx["name"], "event"; got != want {
			t.Fatalf("http context name = %#v, want %q", got, want)
		}
		if got, want := httpCtx["contentType"], "application/json"; got != want {
			t.Fatalf("http context contentType = %#v, want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for sync http binding invocation")
	}
}

func TestHostedHTTPBinding_APIKeyQueryDoesNotLeakCredentialParam(t *testing.T) {
	t.Parallel()

	invocations := make(chan map[string]any, 1)
	provider := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "events",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(_ context.Context, op string, params map[string]any, _ string) (*core.OperationResult, error) {
				if op != "handle_event" {
					t.Fatalf("operation = %q, want %q", op, "handle_event")
				}
				invocations <- cloneAnyMapForTest(params)
				return &core.OperationResult{Status: http.StatusOK, Body: `{"accepted":true}`}, nil
			},
		},
		ops: []core.Operation{{Name: "handle_event", Method: http.MethodPost}},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, provider)
		cfg.PluginDefs = map[string]*config.ProviderEntry{
			"events": {
				SecuritySchemes: map[string]*config.HTTPSecurityScheme{
					"eventKey": {
						Type:   providermanifestv1.HTTPSecuritySchemeTypeAPIKey,
						Name:   "token",
						In:     providermanifestv1.HTTPInQuery,
						Secret: &providermanifestv1.HTTPSecretRef{Secret: "shared-key"},
					},
				},
				HTTP: map[string]*config.HTTPBinding{
					"event": {
						Path:     "/event",
						Method:   http.MethodPost,
						Security: "eventKey",
						Target:   "handle_event",
					},
				},
			},
		}
		cfg.Authorizer = mustAuthorizer(t, config.AuthorizationConfig{}, cfg.Providers, cfg.PluginDefs, nil)
	})
	testutil.CloseOnCleanup(t, ts)

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/events/event?source=query&token=shared-key", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http binding request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	select {
	case params := <-invocations:
		if got, want := params["source"], "query"; got != want {
			t.Fatalf("params[source] = %#v, want %q", got, want)
		}
		if _, exists := params["token"]; exists {
			t.Fatalf("params[token] should be stripped from query apiKey binding: %#v", params)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for query apiKey http binding invocation")
	}
}

func TestHostedHTTPBinding_RejectsReservedSystemResolvedSubject(t *testing.T) {
	t.Parallel()

	invoked := make(chan struct{}, 1)
	provider := &stubIntegrationWithResolvedSubject{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N:        "events",
				ConnMode: core.ConnectionModeNone,
				ExecuteFn: func(_ context.Context, op string, params map[string]any, _ string) (*core.OperationResult, error) {
					invoked <- struct{}{}
					return &core.OperationResult{Status: http.StatusOK, Body: `{"accepted":true}`}, nil
				},
			},
			ops: []core.Operation{{Name: "handle_event", Method: http.MethodPost}},
		},
		resolveFn: func(context.Context, *core.HTTPSubjectResolveRequest) (*core.HTTPResolvedSubject, error) {
			return &core.HTTPResolvedSubject{ID: "system:admin"}, nil
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, provider)
		cfg.PluginDefs = map[string]*config.ProviderEntry{
			"events": {
				SecuritySchemes: map[string]*config.HTTPSecurityScheme{
					"public": {Type: providermanifestv1.HTTPSecuritySchemeTypeNone},
				},
				HTTP: map[string]*config.HTTPBinding{
					"event": {
						Path:     "/event",
						Method:   http.MethodPost,
						Security: "public",
						Target:   "handle_event",
					},
				},
			},
		}
		cfg.Authorizer = mustAuthorizer(t, config.AuthorizationConfig{}, cfg.Providers, cfg.PluginDefs, nil)
	})
	testutil.CloseOnCleanup(t, ts)

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/events/event", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http binding request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}

	select {
	case <-invoked:
		t.Fatal("operation should not execute when resolver returns a reserved system subject")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestHostedHTTPBinding_RejectsGenericOperationRouteConflicts(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	providers := testutil.NewProviderRegistry(t, &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "reports",
			ConnMode: core.ConnectionModeNone,
		},
		ops: []core.Operation{
			{Name: "status", Method: http.MethodGet},
			{Name: "handle_status", Method: http.MethodGet},
		},
	})
	cfg := server.Config{
		Auth:        &coretesting.StubAuthProvider{N: "none"},
		Services:    svc,
		Providers:   providers,
		Invoker:     invocation.NewBroker(providers, svc.Users, svc.Tokens),
		StateSecret: []byte("0123456789abcdef0123456789abcdef"),
		PluginDefs: map[string]*config.ProviderEntry{
			"reports": {
				SecuritySchemes: map[string]*config.HTTPSecurityScheme{
					"none": {Type: providermanifestv1.HTTPSecuritySchemeTypeNone},
				},
				HTTP: map[string]*config.HTTPBinding{
					"status_binding": {
						Path:     "/status",
						Method:   http.MethodGet,
						Security: "none",
						Target:   "handle_status",
					},
				},
			},
		},
	}

	_, err := server.New(cfg)
	if err == nil {
		t.Fatal("expected generic operation route conflict")
	}
	if !strings.Contains(err.Error(), "generic operation route") {
		t.Fatalf("error = %v, want generic operation route conflict", err)
	}
}

func TestHostedHTTPBinding_RejectsInvalidConfigBindings(t *testing.T) {
	t.Parallel()

	baseConfig := func(t *testing.T) server.Config {
		svc := coretesting.NewStubServices(t)
		providers := testutil.NewProviderRegistry(t, &stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N:        "events",
				ConnMode: core.ConnectionModeNone,
			},
			ops: []core.Operation{{Name: "handle_event", Method: http.MethodPost}},
		})
		return server.Config{
			Auth:        &coretesting.StubAuthProvider{N: "none"},
			Services:    svc,
			Providers:   providers,
			Invoker:     invocation.NewBroker(providers, svc.Users, svc.Tokens),
			StateSecret: []byte("0123456789abcdef0123456789abcdef"),
		}
	}

	tests := []struct {
		name    string
		entry   *config.ProviderEntry
		wantErr string
	}{
		{
			name: "invalid ack status",
			entry: &config.ProviderEntry{
				SecuritySchemes: map[string]*config.HTTPSecurityScheme{
					"none": {Type: providermanifestv1.HTTPSecuritySchemeTypeNone},
				},
				HTTP: map[string]*config.HTTPBinding{
					"event": {
						Path:     "/event",
						Method:   http.MethodPost,
						Security: "none",
						Target:   "handle_event",
						Ack:      &providermanifestv1.HTTPAck{Status: http.StatusInternalServerError},
					},
				},
			},
			wantErr: "ack.status must be a 2xx status",
		},
		{
			name: "missing api key secret",
			entry: &config.ProviderEntry{
				SecuritySchemes: map[string]*config.HTTPSecurityScheme{
					"eventKey": {
						Type: providermanifestv1.HTTPSecuritySchemeTypeAPIKey,
						Name: "X-Webhook-Key",
						In:   providermanifestv1.HTTPInHeader,
					},
				},
				HTTP: map[string]*config.HTTPBinding{
					"event": {
						Path:     "/event",
						Method:   http.MethodPost,
						Security: "eventKey",
						Target:   "handle_event",
					},
				},
			},
			wantErr: "secret is required",
		},
		{
			name: "unsupported security scheme type",
			entry: &config.ProviderEntry{
				SecuritySchemes: map[string]*config.HTTPSecurityScheme{
					"eventKey": {
						Type: "bogus",
					},
				},
				HTTP: map[string]*config.HTTPBinding{
					"event": {
						Path:     "/event",
						Method:   http.MethodPost,
						Security: "eventKey",
						Target:   "handle_event",
					},
				},
			},
			wantErr: `type "bogus" is not supported`,
		},
		{
			name: "invalid api key location",
			entry: &config.ProviderEntry{
				SecuritySchemes: map[string]*config.HTTPSecurityScheme{
					"eventKey": {
						Type:   providermanifestv1.HTTPSecuritySchemeTypeAPIKey,
						Name:   "X-Webhook-Key",
						In:     "cookie",
						Secret: &providermanifestv1.HTTPSecretRef{Secret: "shared-key"},
					},
				},
				HTTP: map[string]*config.HTTPBinding{
					"event": {
						Path:     "/event",
						Method:   http.MethodPost,
						Security: "eventKey",
						Target:   "handle_event",
					},
				},
			},
			wantErr: `in "cookie" is not supported`,
		},
		{
			name: "invalid http auth scheme",
			entry: &config.ProviderEntry{
				SecuritySchemes: map[string]*config.HTTPSecurityScheme{
					"eventKey": {
						Type:   providermanifestv1.HTTPSecuritySchemeTypeHTTP,
						Scheme: "digest",
						Secret: &providermanifestv1.HTTPSecretRef{Secret: "shared-key"},
					},
				},
				HTTP: map[string]*config.HTTPBinding{
					"event": {
						Path:     "/event",
						Method:   http.MethodPost,
						Security: "eventKey",
						Target:   "handle_event",
					},
				},
			},
			wantErr: `scheme "digest" is not supported`,
		},
		{
			name: "blank secret reference",
			entry: &config.ProviderEntry{
				SecuritySchemes: map[string]*config.HTTPSecurityScheme{
					"eventKey": {
						Type:   providermanifestv1.HTTPSecuritySchemeTypeAPIKey,
						Name:   "X-Webhook-Key",
						In:     providermanifestv1.HTTPInHeader,
						Secret: &providermanifestv1.HTTPSecretRef{},
					},
				},
				HTTP: map[string]*config.HTTPBinding{
					"event": {
						Path:     "/event",
						Method:   http.MethodPost,
						Security: "eventKey",
						Target:   "handle_event",
					},
				},
			},
			wantErr: "secret must set env or secret",
		},
		{
			name: "missing hmac signature header",
			entry: &config.ProviderEntry{
				SecuritySchemes: map[string]*config.HTTPSecurityScheme{
					"eventKey": {
						Type:            providermanifestv1.HTTPSecuritySchemeTypeHMAC,
						Secret:          &providermanifestv1.HTTPSecretRef{Secret: "shared-key"},
						PayloadTemplate: "{raw_body}",
					},
				},
				HTTP: map[string]*config.HTTPBinding{
					"event": {
						Path:     "/event",
						Method:   http.MethodPost,
						Security: "eventKey",
						Target:   "handle_event",
					},
				},
			},
			wantErr: "signatureHeader is required",
		},
		{
			name: "duplicate normalized content types",
			entry: &config.ProviderEntry{
				SecuritySchemes: map[string]*config.HTTPSecurityScheme{
					"none": {Type: providermanifestv1.HTTPSecuritySchemeTypeNone},
				},
				HTTP: map[string]*config.HTTPBinding{
					"event": {
						Path:   "/event",
						Method: http.MethodPost,
						RequestBody: &providermanifestv1.HTTPRequestBody{
							Content: map[string]*providermanifestv1.HTTPMediaType{
								"application/json":                {},
								"application/json; charset=utf-8": {},
							},
						},
						Security: "none",
						Target:   "handle_event",
					},
				},
			},
			wantErr: `requestBody.content "application/json" is duplicated after normalization`,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := baseConfig(t)
			cfg.PluginDefs = map[string]*config.ProviderEntry{"events": tt.entry}

			_, err := server.New(cfg)
			if err == nil {
				t.Fatal("expected invalid hosted http config")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestAuthInfo(t *testing.T) {
	t.Parallel()

	stub := &stubAuthWithDisplayName{
		StubAuthProvider: coretesting.StubAuthProvider{N: "google"},
		displayName:      "Google",
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = stub
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/api/v1/auth/info")
	if err != nil {
		t.Fatalf("GET /api/v1/auth/info: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if body["provider"] != "google" {
		t.Fatalf("expected provider google, got %q", body["provider"])
	}
	if body["displayName"] != "Google" {
		t.Fatalf("expected displayName Google, got %q", body["displayName"])
	}
	if body["loginSupported"] != true {
		t.Fatalf("expected loginSupported true, got %#v", body["loginSupported"])
	}
}

func TestAuthInfoFallback(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "custom"}
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/api/v1/auth/info")
	if err != nil {
		t.Fatalf("GET /api/v1/auth/info: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if body["provider"] != "custom" {
		t.Fatalf("expected provider custom, got %q", body["provider"])
	}
	if body["displayName"] != "custom" {
		t.Fatalf("expected displayName to fall back to name custom, got %q", body["displayName"])
	}
	if body["loginSupported"] != true {
		t.Fatalf("expected loginSupported true, got %#v", body["loginSupported"])
	}
}

func TestAuthInfoNoAuth(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = nil
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/api/v1/auth/info")
	if err != nil {
		t.Fatalf("GET /api/v1/auth/info: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if body["provider"] != "none" {
		t.Fatalf("expected provider none, got %q", body["provider"])
	}
	if body["displayName"] != "none" {
		t.Fatalf("expected displayName none, got %q", body["displayName"])
	}
	if body["loginSupported"] != false {
		t.Fatalf("expected loginSupported false, got %#v", body["loginSupported"])
	}
}

type stubAuthWithDisplayName struct {
	coretesting.StubAuthProvider
	displayName string
}

func (s *stubAuthWithDisplayName) DisplayName() string {
	return s.displayName
}

type stubIntegrationWithOps struct {
	coretesting.StubIntegration
	ops []core.Operation
}

func (s *stubIntegrationWithOps) Catalog() *catalog.Catalog {
	return serverTestCatalogFromOperations(s.N, s.ops)
}

type stubIntegrationWithResolvedSubject struct {
	stubIntegrationWithOps
	resolveFn func(context.Context, *core.HTTPSubjectResolveRequest) (*core.HTTPResolvedSubject, error)
}

func (s *stubIntegrationWithResolvedSubject) ResolveHTTPSubject(ctx context.Context, req *core.HTTPSubjectResolveRequest) (*core.HTTPResolvedSubject, error) {
	if s.resolveFn != nil {
		return s.resolveFn(ctx, req)
	}
	return nil, nil
}

type stubIntegrationWithCatalog struct {
	coretesting.StubIntegration
	catalog *catalog.Catalog
}

func (s *stubIntegrationWithCatalog) Catalog() *catalog.Catalog {
	return s.catalog
}

type stubIntegrationWithSessionCatalog struct {
	stubIntegrationWithOps
	catalog             *catalog.Catalog
	catalogForRequestFn func(context.Context, string) (*catalog.Catalog, error)
	callFn              func(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error)
}

func (s *stubIntegrationWithSessionCatalog) Catalog() *catalog.Catalog {
	return s.catalog
}

func (s *stubIntegrationWithSessionCatalog) CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error) {
	if s.catalogForRequestFn != nil {
		return s.catalogForRequestFn(ctx, token)
	}
	return s.catalog, nil
}

func (s *stubIntegrationWithSessionCatalog) AuthTypes() []string { return []string{"manual"} }
func (s *stubIntegrationWithSessionCatalog) Close() error        { return nil }
func (s *stubIntegrationWithSessionCatalog) CallTool(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error) {
	if s.callFn != nil {
		return s.callFn(ctx, name, args)
	}
	return mcpgo.NewToolResultText("passthrough:" + name), nil
}

type stubAuthWithLoginURL struct {
	coretesting.StubAuthProvider
	loginURL      string
	capturedState string
	loginURLCtxFn func(context.Context, string) (string, error)
}

func (s *stubAuthWithLoginURL) LoginURL(state string) (string, error) {
	s.capturedState = state
	return s.loginURL, nil
}

func (s *stubAuthWithLoginURL) LoginURLContext(ctx context.Context, state string) (string, error) {
	if s.loginURLCtxFn != nil {
		return s.loginURLCtxFn(ctx, state)
	}
	return s.LoginURL(state)
}

type stubIntegrationWithAuthURL struct {
	coretesting.StubIntegration
	authURL          string
	connectionParams map[string]core.ConnectionParamDef
	postConnect      func(context.Context, *core.IntegrationToken) (map[string]string, error)
}

func (s *stubIntegrationWithAuthURL) AuthorizationURL(_ string, _ []string) string {
	return s.authURL
}

func (s *stubIntegrationWithAuthURL) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return maps.Clone(s.connectionParams)
}

func (s *stubIntegrationWithAuthURL) PostConnect(ctx context.Context, token *core.IntegrationToken) (map[string]string, error) {
	if s.postConnect != nil {
		return s.postConnect(ctx, token)
	}
	return nil, nil
}

type stubPKCEIntegration struct {
	coretesting.StubIntegration
	authURL      string
	wantVerifier string
	gotVerifier  string
}

func (s *stubPKCEIntegration) AuthorizationURL(state string, _ []string) string {
	return s.authURL + "?state=" + url.QueryEscape(state)
}

func (s *stubPKCEIntegration) StartOAuth(state string, _ []string) (string, string) {
	return s.AuthorizationURL(state, nil), s.wantVerifier
}

func (s *stubPKCEIntegration) ExchangeCodeWithVerifier(_ context.Context, code, verifier string, _ ...oauth.ExchangeOption) (*core.TokenResponse, error) {
	s.gotVerifier = verifier
	if code != "good-code" {
		return nil, fmt.Errorf("bad code")
	}
	return &core.TokenResponse{AccessToken: "pkce-token"}, nil
}

func TestIntegrationOAuthCallback_PKCEUsesVerifier(t *testing.T) {
	t.Parallel()

	stub := &stubPKCEIntegration{
		StubIntegration: coretesting.StubIntegration{N: "gitlab"},
		authURL:         "https://gitlab.com/oauth/authorize",
		wantVerifier:    "verifier-123",
	}

	handler := &testOAuthHandler{
		authorizationBaseURLVal: "https://gitlab.com/oauth/authorize",
		startOAuthFn: func(state string, _ []string) (string, string) {
			return "https://gitlab.com/oauth/authorize?state=" + state, "verifier-123"
		},
		exchangeCodeWithVerFn: func(_ context.Context, code, verifier string, _ ...oauth.ExchangeOption) (*core.TokenResponse, error) {
			stub.gotVerifier = verifier
			if code != "good-code" {
				return nil, fmt.Errorf("bad code")
			}
			return &core.TokenResponse{AccessToken: "pkce-token"}, nil
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"gitlab": testDefaultConnection}
		cfg.ConnectionAuth = testConnectionAuth("gitlab", handler)
		cfg.Services = coretesting.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	startBody := bytes.NewBufferString(`{"integration":"gitlab"}`)
	startReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/start-oauth", startBody)
	startReq.Header.Set("Content-Type", "application/json")
	startResp, err := http.DefaultClient.Do(startReq)
	if err != nil {
		t.Fatalf("start request: %v", err)
	}
	defer func() { _ = startResp.Body.Close() }()

	if startResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from start-oauth, got %d", startResp.StatusCode)
	}

	var startResult map[string]string
	if err := json.NewDecoder(startResp.Body).Decode(&startResult); err != nil {
		t.Fatalf("decoding start response: %v", err)
	}

	noRedirect := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/auth/callback?code=good-code&state="+url.QueryEscape(startResult["state"]), nil)
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatalf("callback request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}
	if stub.gotVerifier != stub.wantVerifier {
		t.Fatalf("got verifier %q, want %q", stub.gotVerifier, stub.wantVerifier)
	}
}

func TestCallbackPathConstants(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
	})
	testutil.CloseOnCleanup(t, ts)

	// Auth login callback: should not 404 (it will return 400 for missing code,
	// which proves the route exists).
	resp, err := http.Get(ts.URL + config.AuthCallbackPath)
	if err != nil {
		t.Fatalf("GET %s: %v", config.AuthCallbackPath, err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		t.Errorf("config.AuthCallbackPath %q is not a registered route (got 404)", config.AuthCallbackPath)
	}

	// Integration callback: should be public and return 400 for missing params,
	// which proves the route exists without auth middleware.
	resp, err = http.Get(ts.URL + config.IntegrationCallbackPath)
	if err != nil {
		t.Fatalf("GET %s: %v", config.IntegrationCallbackPath, err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		t.Errorf("config.IntegrationCallbackPath %q is not a registered route (got 404)", config.IntegrationCallbackPath)
	}
}

type stubOAuthIntegration struct {
	stubIntegrationWithOps
	refreshTokenFn func(context.Context, string) (*core.TokenResponse, error)
}

func (s *stubOAuthIntegration) RefreshToken(ctx context.Context, token string) (*core.TokenResponse, error) {
	if s.refreshTokenFn != nil {
		return s.refreshTokenFn(ctx, token)
	}
	return nil, nil
}

// stubNonOAuthProvider implements core.Provider but NOT core.OAuthProvider.
type stubNonOAuthProvider struct {
	name    string
	ops     []core.Operation
	catalog *catalog.Catalog
	execFn  func(context.Context, string, map[string]any, string) (*core.OperationResult, error)
}

func (s *stubNonOAuthProvider) Name() string                        { return s.name }
func (s *stubNonOAuthProvider) DisplayName() string                 { return s.name }
func (s *stubNonOAuthProvider) Description() string                 { return "" }
func (s *stubNonOAuthProvider) ConnectionMode() core.ConnectionMode { return core.ConnectionModeUser }
func (s *stubNonOAuthProvider) AuthTypes() []string                 { return nil }
func (s *stubNonOAuthProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return nil
}
func (s *stubNonOAuthProvider) CredentialFields() []core.CredentialFieldDef { return nil }
func (s *stubNonOAuthProvider) DiscoveryConfig() *core.DiscoveryConfig      { return nil }
func (s *stubNonOAuthProvider) ConnectionForOperation(string) string        { return "" }
func (s *stubNonOAuthProvider) Catalog() *catalog.Catalog {
	if s.catalog != nil {
		return s.catalog
	}
	return serverTestCatalogFromOperations(s.name, s.ops)
}
func (s *stubNonOAuthProvider) Execute(ctx context.Context, op string, params map[string]any, token string) (*core.OperationResult, error) {
	if s.execFn != nil {
		return s.execFn(ctx, op, params, token)
	}
	return &core.OperationResult{Status: http.StatusOK, Body: `{}`}, nil
}

func serverTestCatalogFromOperations(name string, ops []core.Operation) *catalog.Catalog {
	cat := &catalog.Catalog{
		Name:       name,
		Operations: make([]catalog.CatalogOperation, 0, len(ops)),
	}
	for _, op := range ops {
		params := make([]catalog.CatalogParameter, 0, len(op.Parameters))
		for _, param := range op.Parameters {
			params = append(params, catalog.CatalogParameter{
				Name:        param.Name,
				Type:        param.Type,
				Description: param.Description,
				Required:    param.Required,
				Default:     param.Default,
			})
		}
		cat.Operations = append(cat.Operations, catalog.CatalogOperation{
			ID:          op.Name,
			Method:      op.Method,
			Path:        "/" + op.Name,
			Description: op.Description,
			Parameters:  params,
			Transport:   catalog.TransportREST,
		})
	}
	coreintegration.CompileSchemas(cat)
	return cat
}

func serverTestCatalog(name string, ops []catalog.CatalogOperation) *catalog.Catalog {
	cat := &catalog.Catalog{
		Name:       name,
		Operations: append([]catalog.CatalogOperation(nil), ops...),
	}
	coreintegration.CompileSchemas(cat)
	return cat
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func cloneAnyMapForTest(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func httpBindingTestSignature(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}

func TestExecuteOperation_RefreshesExpiredToken(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	expired := time.Now().Add(-1 * time.Hour)
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok1", SubjectID: principal.UserSubjectID(u.ID), Integration: "fake",
		Connection: "default", Instance: "default",
		AccessToken: "expired-access", RefreshToken: "old-refresh-token", ExpiresAt: &expired,
	})

	var refreshedToken string
	stub := &stubOAuthIntegration{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N: "fake",
				ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
					refreshedToken = token
					return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
				},
			},
			ops: []core.Operation{{Name: "list", Description: "List", Method: http.MethodGet}},
		},
		refreshTokenFn: func(_ context.Context, rt string) (*core.TokenResponse, error) {
			if rt == "old-refresh-token" {
				return &core.TokenResponse{AccessToken: "fresh-access-token", ExpiresIn: 3600}, nil
			}
			return nil, fmt.Errorf("unexpected refresh token")
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"fake": testDefaultConnection}
		cfg.ConnectionAuth = oauthRefreshConnectionAuth("fake", stub.refreshTokenFn)
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/fake/list", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if refreshedToken != "fresh-access-token" {
		t.Fatalf("expected operation to use refreshed token, got %q", refreshedToken)
	}
}

func TestExecuteOperation_RefreshFailsButTokenStillValid(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	almostExpired := time.Now().Add(2 * time.Minute)
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok1", SubjectID: principal.UserSubjectID(u.ID), Integration: "fake",
		Connection: "default", Instance: "default",
		AccessToken: "still-valid-token", RefreshToken: "some-refresh", ExpiresAt: &almostExpired,
	})

	var usedToken string
	stub := &stubOAuthIntegration{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N: "fake",
				ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
					usedToken = token
					return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
				},
			},
			ops: []core.Operation{{Name: "list", Description: "List", Method: http.MethodGet}},
		},
		refreshTokenFn: func(context.Context, string) (*core.TokenResponse, error) {
			return nil, fmt.Errorf("upstream error")
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"fake": testDefaultConnection}
		cfg.ConnectionAuth = oauthRefreshConnectionAuth("fake", stub.refreshTokenFn)
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/fake/list", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 (graceful degradation), got %d", resp.StatusCode)
	}
	if usedToken != "still-valid-token" {
		t.Fatalf("expected operation to use old token, got %q", usedToken)
	}
}

func TestExecuteOperation_RefreshPassesThroughStoredTokenWhenRefreshDoesNotApply(t *testing.T) {
	t.Parallel()

	expired := time.Now().Add(-1 * time.Hour)
	cases := []struct {
		name                    string
		token                   core.IntegrationToken
		configureConnectionAuth bool
		wantStatus              int
		wantUsedToken           string
	}{
		{
			name: "missing refresh token",
			token: core.IntegrationToken{
				ID: "tok1", Integration: "fake",
				Connection: "default", Instance: "default",
				AccessToken: "no-refresh-token",
			},
			configureConnectionAuth: true,
			wantStatus:              http.StatusOK,
			wantUsedToken:           "no-refresh-token",
		},
		{
			name: "missing expiry",
			token: core.IntegrationToken{
				ID: "tok1", Integration: "fake",
				Connection: "default", Instance: "default",
				AccessToken: "no-expiry-token", RefreshToken: "some-refresh",
			},
			configureConnectionAuth: true,
			wantStatus:              http.StatusOK,
			wantUsedToken:           "no-expiry-token",
		},
		{
			name: "missing refresher",
			token: core.IntegrationToken{
				ID: "tok1", Integration: "fake",
				Connection: "default", Instance: "default",
				AccessToken: "no-refresher-token", RefreshToken: "some-refresh", ExpiresAt: &expired,
			},
			configureConnectionAuth: false,
			wantStatus:              http.StatusOK,
			wantUsedToken:           "no-refresher-token",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := coretesting.NewStubServices(t)
			u := seedUser(t, svc, "anonymous@gestalt")
			token := tc.token
			token.SubjectID = principal.UserSubjectID(u.ID)
			seedToken(t, svc, &token)

			refreshCalled := false
			var usedToken string
			stub := &stubOAuthIntegration{
				stubIntegrationWithOps: stubIntegrationWithOps{
					StubIntegration: coretesting.StubIntegration{
						N: "fake",
						ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
							usedToken = token
							return &core.OperationResult{Status: http.StatusOK, Body: `{}`}, nil
						},
					},
					ops: []core.Operation{{Name: "list", Description: "List", Method: http.MethodGet}},
				},
			}

			var connectionAuth func() map[string]map[string]bootstrap.OAuthHandler
			if tc.configureConnectionAuth {
				connectionAuth = testConnectionAuth("fake", &testOAuthHandler{
					refreshTokenFn: func(context.Context, string) (*core.TokenResponse, error) {
						refreshCalled = true
						return nil, fmt.Errorf("unexpected refresh")
					},
				})
			}

			ts := newTestServer(t, func(cfg *server.Config) {
				cfg.Providers = testutil.NewProviderRegistry(t, stub)
				cfg.DefaultConnection = map[string]string{"fake": testDefaultConnection}
				cfg.ConnectionAuth = connectionAuth
				cfg.Services = svc
			})
			testutil.CloseOnCleanup(t, ts)

			req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/fake/list", nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			if usedToken != tc.wantUsedToken {
				t.Fatalf("used token = %q, want %q", usedToken, tc.wantUsedToken)
			}
			if refreshCalled {
				t.Fatalf("refresh handler should not have been called")
			}
		})
	}
}

func TestExecuteOperation_RefreshPersistsReturnedTokenFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name              string
		response          *core.TokenResponse
		wantAccessToken   string
		wantRefreshToken  string
		wantHasExpiration bool
	}{
		{
			name: "rotates refresh token and expiry",
			response: &core.TokenResponse{
				AccessToken:  "new-access",
				RefreshToken: "rotated-refresh",
				ExpiresIn:    7200,
			},
			wantAccessToken:   "new-access",
			wantRefreshToken:  "rotated-refresh",
			wantHasExpiration: true,
		},
		{
			name: "clears expiry when omitted",
			response: &core.TokenResponse{
				AccessToken: "new-access",
				ExpiresIn:   0,
			},
			wantAccessToken:   "new-access",
			wantRefreshToken:  "old-refresh",
			wantHasExpiration: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := coretesting.NewStubServices(t)
			u := seedUser(t, svc, "anonymous@gestalt")
			expired := time.Now().Add(-1 * time.Hour)
			seedToken(t, svc, &core.IntegrationToken{
				ID: "tok1", SubjectID: principal.UserSubjectID(u.ID), Integration: "fake",
				Connection: "default", Instance: "default",
				AccessToken: "old-access", RefreshToken: "old-refresh", ExpiresAt: &expired,
			})

			stub := &stubOAuthIntegration{
				stubIntegrationWithOps: stubIntegrationWithOps{
					StubIntegration: coretesting.StubIntegration{
						N: "fake",
						ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
							return &core.OperationResult{Status: http.StatusOK, Body: `{}`}, nil
						},
					},
					ops: []core.Operation{{Name: "list", Description: "List", Method: http.MethodGet}},
				},
				refreshTokenFn: func(_ context.Context, _ string) (*core.TokenResponse, error) {
					return tc.response, nil
				},
			}

			ts := newTestServer(t, func(cfg *server.Config) {
				cfg.Providers = testutil.NewProviderRegistry(t, stub)
				cfg.DefaultConnection = map[string]string{"fake": testDefaultConnection}
				cfg.ConnectionAuth = oauthRefreshConnectionAuth("fake", stub.refreshTokenFn)
				cfg.Services = svc
			})
			testutil.CloseOnCleanup(t, ts)

			req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/fake/list", nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected 200, got %d", resp.StatusCode)
			}

			stored, err := svc.Tokens.Token(context.Background(), principal.UserSubjectID(u.ID), "fake", "default", "default")
			if err != nil {
				t.Fatalf("Token: %v", err)
			}
			if stored.AccessToken != tc.wantAccessToken {
				t.Fatalf("stored access token = %q, want %q", stored.AccessToken, tc.wantAccessToken)
			}
			if stored.RefreshToken != tc.wantRefreshToken {
				t.Fatalf("stored refresh token = %q, want %q", stored.RefreshToken, tc.wantRefreshToken)
			}
			if (stored.ExpiresAt != nil) != tc.wantHasExpiration {
				t.Fatalf("stored expiry present = %v, want %v", stored.ExpiresAt != nil, tc.wantHasExpiration)
			}
		})
	}
}

func TestExecuteOperation_RefreshFailureEdgeCases(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		expiresAt     time.Time
		beforeRefresh func(*coredata.Services)
		wantStatus    int
		wantUsedToken string
	}{
		{
			name:          "expired token requires reconnect",
			expiresAt:     time.Now().Add(-1 * time.Hour),
			wantStatus:    http.StatusPreconditionFailed,
			wantUsedToken: "",
		},
		{
			name:      "deleted token falls back to in-memory token when still valid",
			expiresAt: time.Now().Add(2 * time.Minute),
			beforeRefresh: func(svc *coredata.Services) {
				_ = svc.Tokens.DeleteToken(context.Background(), "tok1")
			},
			wantStatus:    http.StatusOK,
			wantUsedToken: "still-valid-token",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := coretesting.NewStubServices(t)
			u := seedUser(t, svc, "anonymous@gestalt")
			seedToken(t, svc, &core.IntegrationToken{
				ID: "tok1", SubjectID: principal.UserSubjectID(u.ID), Integration: "fake",
				Connection: "default", Instance: "default",
				AccessToken: "still-valid-token", RefreshToken: "some-refresh", ExpiresAt: &tc.expiresAt,
			})

			var usedToken string
			stub := &stubOAuthIntegration{
				stubIntegrationWithOps: stubIntegrationWithOps{
					StubIntegration: coretesting.StubIntegration{
						N: "fake",
						ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
							usedToken = token
							return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
						},
					},
					ops: []core.Operation{{Name: "list", Description: "List", Method: http.MethodGet}},
				},
				refreshTokenFn: func(context.Context, string) (*core.TokenResponse, error) {
					if tc.beforeRefresh != nil {
						tc.beforeRefresh(svc)
					}
					return nil, fmt.Errorf("upstream error")
				},
			}

			ts := newTestServer(t, func(cfg *server.Config) {
				cfg.Providers = testutil.NewProviderRegistry(t, stub)
				cfg.DefaultConnection = map[string]string{"fake": testDefaultConnection}
				cfg.ConnectionAuth = oauthRefreshConnectionAuth("fake", stub.refreshTokenFn)
				cfg.Services = svc
			})
			testutil.CloseOnCleanup(t, ts)

			req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/fake/list", nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			if usedToken != tc.wantUsedToken {
				t.Fatalf("used token = %q, want %q", usedToken, tc.wantUsedToken)
			}
		})
	}
}

func TestExecuteOperation_RefreshErrorSkipsStoreOnConcurrentRefresh(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	expired := time.Now().Add(-1 * time.Hour)
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok1", SubjectID: principal.UserSubjectID(u.ID), Integration: "fake",
		Connection: "default", Instance: "default",
		AccessToken: "original-token", RefreshToken: "some-refresh", ExpiresAt: &expired,
	})

	var usedToken string
	stub := &stubOAuthIntegration{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N: "fake",
				ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
					usedToken = token
					return &core.OperationResult{Status: http.StatusOK, Body: `{}`}, nil
				},
			},
			ops: []core.Operation{{Name: "list", Description: "List", Method: http.MethodGet}},
		},
		refreshTokenFn: func(_ context.Context, _ string) (*core.TokenResponse, error) {
			ctx := context.Background()
			_ = svc.Tokens.StoreToken(ctx, &core.IntegrationToken{
				ID: "tok1", SubjectID: principal.UserSubjectID(u.ID), Integration: "fake",
				Connection: "default", Instance: "default",
				AccessToken: "concurrently-refreshed-token", RefreshToken: "new-refresh",
			})
			return nil, fmt.Errorf("upstream error")
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"fake": testDefaultConnection}
		cfg.ConnectionAuth = oauthRefreshConnectionAuth("fake", stub.refreshTokenFn)
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/fake/list", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if usedToken != "concurrently-refreshed-token" {
		t.Fatalf("expected concurrently refreshed token, got %q", usedToken)
	}
}

func TestExecuteOperation_StoreTokenFailureReturnsError(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	stubDB := svc.DB.(*coretesting.StubIndexedDB)
	u := seedUser(t, svc, "anonymous@gestalt")
	expired := time.Now().Add(-1 * time.Hour)
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok1", SubjectID: principal.UserSubjectID(u.ID), Integration: "fake",
		Connection: "default", Instance: "default",
		AccessToken: "old-access", RefreshToken: "old-refresh", ExpiresAt: &expired,
	})

	stub := &stubOAuthIntegration{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "fake"},
			ops:             []core.Operation{{Name: "list", Description: "List", Method: http.MethodGet}},
		},
		refreshTokenFn: func(_ context.Context, _ string) (*core.TokenResponse, error) {
			stubDB.Err = fmt.Errorf("store unavailable")
			return &core.TokenResponse{
				AccessToken:  "new-access",
				RefreshToken: "rotated-refresh",
				ExpiresIn:    3600,
			}, nil
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"fake": testDefaultConnection}
		cfg.ConnectionAuth = oauthRefreshConnectionAuth("fake", stub.refreshTokenFn)
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/fake/list", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	stubDB.Err = nil

	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502 when StoreToken fails after refresh, got %d", resp.StatusCode)
	}
}

type stubStatefulAuth struct {
	coretesting.StubAuthProvider
	handleWithState func(context.Context, string, string) (*core.UserIdentity, string, error)
}

func (s *stubStatefulAuth) HandleCallbackWithState(ctx context.Context, code, state string) (*core.UserIdentity, string, error) {
	return s.handleWithState(ctx, code, state)
}

func (s *stubStatefulAuth) IssueSessionToken(identity *core.UserIdentity) (string, error) {
	return "session-token-" + identity.Email, nil
}

func TestExecuteOperation_ConnectionModeNone(t *testing.T) {
	t.Parallel()

	tokenCalled := false
	stub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "noop",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
				if token != "" {
					t.Errorf("expected empty token for ConnectionModeNone, got %q", token)
				}
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		},
		ops: []core.Operation{
			{Name: "ping", Method: http.MethodGet},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Services = coretesting.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/noop/ping", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if tokenCalled {
		t.Fatal("datastore.Token should not be called for ConnectionModeNone")
	}
}

func TestExecuteOperation_EchoProvider(t *testing.T) {
	t.Parallel()

	echoProvider := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "echo",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(_ context.Context, _ string, params map[string]any, _ string) (*core.OperationResult, error) {
				body, _ := json.Marshal(params)
				return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil
			},
		},
		ops: []core.Operation{
			{Name: "echo", Method: http.MethodPost},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, echoProvider)
		cfg.Services = coretesting.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"message":"hello"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/echo/echo", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if result["message"] != "hello" {
		t.Fatalf("expected message hello, got %v", result["message"])
	}
}

func TestExecuteOperation_HTTPAndMCPEquivalent(t *testing.T) {
	t.Parallel()

	echoProvider := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "echo",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(_ context.Context, op string, params map[string]any, token string) (*core.OperationResult, error) {
				body, _ := json.Marshal(map[string]any{
					"op":    op,
					"query": params["q"],
					"token": token,
				})
				return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil
			},
		},
		ops: []core.Operation{{Name: "search", Method: http.MethodGet}},
	}

	providers := testutil.NewProviderRegistry(t, echoProvider)
	svc := coretesting.NewStubServices(t)

	httpSrv := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = providers
		cfg.Services = svc
	})
	defer httpSrv.Close()

	httpReq, _ := http.NewRequest(http.MethodGet, httpSrv.URL+"/api/v1/echo/search?q=hello", nil)
	httpResp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("HTTP request: %v", err)
	}
	defer func() { _ = httpResp.Body.Close() }()
	if httpResp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", httpResp.StatusCode)
	}
	var httpBody map[string]any
	if err := json.NewDecoder(httpResp.Body).Decode(&httpBody); err != nil {
		t.Fatalf("decode HTTP body: %v", err)
	}

	invoker := invocation.NewBroker(providers, svc.Users, svc.Tokens)
	mcpSrv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:   invoker,
		Providers: providers,
	})
	tool := mcpSrv.GetTool("echo_search")
	if tool == nil {
		t.Fatal("expected echo_search tool")
	}

	ctx := principal.WithPrincipal(context.Background(), &principal.Principal{
		Identity: &core.UserIdentity{Email: "dev@example.com"},
		UserID:   "u1",
		Source:   principal.SourceSession,
	})
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "echo_search"
	req.Params.Arguments = map[string]any{"q": "hello"}

	mcpResult, err := tool.Handler(ctx, req)
	if err != nil {
		t.Fatalf("MCP tool call: %v", err)
	}
	if mcpResult.IsError {
		t.Fatalf("unexpected MCP error result: %v", mcpResult.Content)
	}
	if len(mcpResult.Content) != 1 {
		t.Fatalf("expected one MCP content item, got %d", len(mcpResult.Content))
	}
	text, ok := mcpgo.AsTextContent(mcpResult.Content[0])
	if !ok {
		t.Fatalf("expected MCP text content, got %T", mcpResult.Content[0])
	}

	httpJSON, _ := json.Marshal(httpBody)
	if text.Text != string(httpJSON) {
		t.Fatalf("expected MCP body %s to match HTTP body %s", text.Text, string(httpJSON))
	}
}

type stubManualProvider struct {
	coretesting.StubIntegration
}

func (s *stubManualProvider) AuthTypes() []string { return []string{"manual"} }

type stubNilAuthTypesProvider struct {
	coretesting.StubIntegration
}

func (s *stubNilAuthTypesProvider) AuthTypes() []string { return nil }

type stubDiscoveringManualProvider struct {
	stubManualProvider
	discovery *core.DiscoveryConfig
}

func (s *stubDiscoveringManualProvider) DiscoveryConfig() *core.DiscoveryConfig {
	return s.discovery
}

type stubDiscoveringProvider struct {
	coretesting.StubIntegration
	discovery        *core.DiscoveryConfig
	connectionParams map[string]core.ConnectionParamDef
	postConnect      func(context.Context, *core.IntegrationToken) (map[string]string, error)
}

func (s *stubDiscoveringProvider) DiscoveryConfig() *core.DiscoveryConfig {
	return s.discovery
}

func (s *stubDiscoveringProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return maps.Clone(s.connectionParams)
}

func (s *stubDiscoveringProvider) PostConnect(ctx context.Context, token *core.IntegrationToken) (map[string]string, error) {
	if s.postConnect != nil {
		return s.postConnect(ctx, token)
	}
	return nil, nil
}

func testSlackPostConnect(_ context.Context, token *core.IntegrationToken) (map[string]string, error) {
	var metadata map[string]string
	if strings.TrimSpace(token.MetadataJSON) != "" {
		if err := json.Unmarshal([]byte(token.MetadataJSON), &metadata); err != nil {
			return nil, err
		}
	}
	teamID := strings.TrimSpace(metadata["team_id"])
	userID := strings.TrimSpace(metadata["user_id"])
	if teamID == "" || userID == "" {
		return nil, fmt.Errorf("missing slack token metadata")
	}
	return map[string]string{
		"gestalt.external_identity.type": "slack_identity",
		"gestalt.external_identity.id":   "team:" + teamID + ":user:" + userID,
	}, nil
}

type stubManualProviderWithCapabilities struct {
	stubManualProvider
	credentialFields []core.CredentialFieldDef
	connectionParams map[string]core.ConnectionParamDef
	discovery        *core.DiscoveryConfig
}

func (s *stubManualProviderWithCapabilities) CredentialFields() []core.CredentialFieldDef {
	return s.credentialFields
}

func (s *stubManualProviderWithCapabilities) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return s.connectionParams
}

func (s *stubManualProviderWithCapabilities) DiscoveryConfig() *core.DiscoveryConfig {
	return s.discovery
}

type stubPostConnectManualProvider struct {
	stubManualProvider
	connectionParams map[string]core.ConnectionParamDef
	postConnect      func(context.Context, *core.IntegrationToken) (map[string]string, error)
}

func (s *stubPostConnectManualProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return maps.Clone(s.connectionParams)
}

func (s *stubPostConnectManualProvider) PostConnect(ctx context.Context, token *core.IntegrationToken) (map[string]string, error) {
	if s.postConnect != nil {
		return s.postConnect(ctx, token)
	}
	return nil, nil
}

type stubDualAuthProvider struct {
	coretesting.StubIntegration
}

func (s *stubDualAuthProvider) AuthTypes() []string { return []string{"oauth", "manual"} }
func (s *stubDualAuthProvider) CredentialFields() []core.CredentialFieldDef {
	return []core.CredentialFieldDef{{Name: "api_token", Label: "API Token"}}
}

func TestConnectManual(t *testing.T) {
	t.Parallel()

	const pendingSelectionPath = "/api/v1/auth/pending-connection"

	t.Run("connected", func(t *testing.T) {
		t.Parallel()

		var auditBuf bytes.Buffer
		svc := coretesting.NewStubServices(t)
		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, &stubManualProvider{
				StubIntegration: coretesting.StubIntegration{N: "manual-svc"},
			})
			cfg.DefaultConnection = map[string]string{"manual-svc": config.PluginConnectionName}
			cfg.AuditSink = invocation.NewSlogAuditSink(&auditBuf)
			cfg.Services = svc
		})
		testutil.CloseOnCleanup(t, ts)

		body := bytes.NewBufferString(`{"integration":"manual-svc","credential":"my-api-key"}`)
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/connect-manual", body)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		var result map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decoding: %v", err)
		}
		if result["status"] != "connected" {
			t.Fatalf("expected connected, got %q", result["status"])
		}

		u, _ := svc.Users.FindOrCreateUser(context.Background(), "anonymous@gestalt")
		tokens, _ := svc.Tokens.ListTokens(context.Background(), principal.UserSubjectID(u.ID))
		if len(tokens) == 0 {
			t.Fatal("expected StoreToken to be called")
		}
		stored := tokens[0]
		if stored.Integration != "manual-svc" {
			t.Fatalf("expected integration manual-svc, got %q", stored.Integration)
		}
		if stored.AccessToken != "my-api-key" {
			t.Fatalf("expected credential my-api-key, got %q", stored.AccessToken)
		}

		var auditRecord map[string]any
		if err := json.Unmarshal(auditBuf.Bytes(), &auditRecord); err != nil {
			t.Fatalf("parsing audit record: %v\nraw: %s", err, auditBuf.String())
		}
		if auditRecord["target_kind"] != "connection" {
			t.Fatalf("expected audit target_kind connection, got %v", auditRecord["target_kind"])
		}
		if auditRecord["target_id"] != "manual-svc/plugin/default" {
			t.Fatalf("expected audit target_id manual-svc/plugin/default, got %v", auditRecord["target_id"])
		}
		if auditRecord["target_name"] != "plugin/default" {
			t.Fatalf("expected audit target_name plugin/default, got %v", auditRecord["target_name"])
		}
	})

	t.Run("reconnect replaces prior external identity authorization", func(t *testing.T) {
		t.Parallel()

		svc := coretesting.NewStubServices(t)
		authzProvider := newMemoryAuthorizationProvider("memory-authorization")
		legacy, err := authorization.New(config.AuthorizationConfig{}, map[string]*config.ProviderEntry{}, nil, nil)
		if err != nil {
			t.Fatalf("authorization.New: %v", err)
		}
		authz := mustProviderBackedAuthorizer(t, legacy, authzProvider)
		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, &stubPostConnectManualProvider{
				stubManualProvider: stubManualProvider{
					StubIntegration: coretesting.StubIntegration{N: "manual-svc"},
				},
				connectionParams: map[string]core.ConnectionParamDef{
					"team_id": {Required: true},
					"user_id": {Required: true},
				},
				postConnect: testSlackPostConnect,
			})
			cfg.DefaultConnection = map[string]string{"manual-svc": config.PluginConnectionName}
			cfg.Services = svc
			cfg.Authorizer = authz
			cfg.AuthorizationProvider = authzProvider
		})
		testutil.CloseOnCleanup(t, ts)

		connect := func(credential, teamID, userID string) *http.Response {
			t.Helper()
			body := bytes.NewBufferString(fmt.Sprintf(`{"integration":"manual-svc","credential":%q,"connectionParams":{"team_id":%q,"user_id":%q}}`, credential, teamID, userID))
			req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/connect-manual", body)
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			return resp
		}

		firstResp := connect("first-key", "T123", "U456")
		defer func() { _ = firstResp.Body.Close() }()
		if firstResp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(firstResp.Body)
			t.Fatalf("expected first connect 200, got %d: %s", firstResp.StatusCode, body)
		}

		secondResp := connect("second-key", "T999", "U999")
		defer func() { _ = secondResp.Body.Close() }()
		if secondResp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(secondResp.Body)
			t.Fatalf("expected reconnect 200, got %d: %s", secondResp.StatusCode, body)
		}

		u, _ := svc.Users.FindOrCreateUser(context.Background(), "anonymous@gestalt")
		tokens, err := svc.Tokens.ListTokens(context.Background(), principal.UserSubjectID(u.ID))
		if err != nil {
			t.Fatalf("ListTokens: %v", err)
		}
		if len(tokens) != 1 {
			t.Fatalf("expected exactly one stored token after reconnect, got %d", len(tokens))
		}
		if tokens[0].AccessToken != "second-key" {
			t.Fatalf("expected updated access token second-key, got %q", tokens[0].AccessToken)
		}
		oldExternalIdentityID := testExternalIdentityResourceID("slack_identity", "team:T123:user:U456")
		newExternalIdentityID := testExternalIdentityResourceID("slack_identity", "team:T999:user:U999")
		oldResp, err := authzProvider.ReadRelationships(context.Background(), &core.ReadRelationshipsRequest{
			Relation: authorization.ProviderExternalIdentityRelationAssume,
			Resource: &core.ResourceRef{
				Type: authorization.ProviderResourceTypeExternalIdentity,
				Id:   oldExternalIdentityID,
			},
		})
		if err != nil {
			t.Fatalf("ReadRelationships old identity: %v", err)
		}
		if len(oldResp.GetRelationships()) != 0 {
			t.Fatalf("expected old external identity relationship to be removed, got %+v", oldResp.GetRelationships())
		}
		newResp, err := authzProvider.ReadRelationships(context.Background(), &core.ReadRelationshipsRequest{
			Relation: authorization.ProviderExternalIdentityRelationAssume,
			Resource: &core.ResourceRef{
				Type: authorization.ProviderResourceTypeExternalIdentity,
				Id:   newExternalIdentityID,
			},
		})
		if err != nil {
			t.Fatalf("ReadRelationships new identity: %v", err)
		}
		if len(newResp.GetRelationships()) != 1 {
			t.Fatalf("expected new external identity relationship to exist, got %+v", newResp.GetRelationships())
		}
	})

	t.Run("reconnect rolls back existing token when authz sync fails", func(t *testing.T) {
		t.Parallel()

		svc := coretesting.NewStubServices(t)
		authzProvider := newMemoryAuthorizationProvider("memory-authorization")
		legacy, err := authorization.New(config.AuthorizationConfig{}, map[string]*config.ProviderEntry{}, nil, nil)
		if err != nil {
			t.Fatalf("authorization.New: %v", err)
		}
		authz := mustProviderBackedAuthorizer(t, legacy, authzProvider)
		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, &stubPostConnectManualProvider{
				stubManualProvider: stubManualProvider{
					StubIntegration: coretesting.StubIntegration{N: "manual-svc"},
				},
				connectionParams: map[string]core.ConnectionParamDef{
					"team_id": {Required: true},
					"user_id": {Required: true},
				},
				postConnect: testSlackPostConnect,
			})
			cfg.DefaultConnection = map[string]string{"manual-svc": config.PluginConnectionName}
			cfg.Services = svc
			cfg.Authorizer = authz
			cfg.AuthorizationProvider = authzProvider
		})
		testutil.CloseOnCleanup(t, ts)

		connect := func(credential, teamID, userID string) *http.Response {
			t.Helper()
			body := bytes.NewBufferString(fmt.Sprintf(`{"integration":"manual-svc","credential":%q,"connectionParams":{"team_id":%q,"user_id":%q}}`, credential, teamID, userID))
			req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/connect-manual", body)
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			return resp
		}

		firstResp := connect("first-key", "T123", "U456")
		defer func() { _ = firstResp.Body.Close() }()
		if firstResp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(firstResp.Body)
			t.Fatalf("expected first connect 200, got %d: %s", firstResp.StatusCode, body)
		}

		authzProvider.writeErr = errors.New("authorization provider unavailable")
		secondResp := connect("second-key", "T999", "U999")
		defer func() { _ = secondResp.Body.Close() }()
		if secondResp.StatusCode != http.StatusBadGateway {
			body, _ := io.ReadAll(secondResp.Body)
			t.Fatalf("expected reconnect failure 502, got %d: %s", secondResp.StatusCode, body)
		}

		u, _ := svc.Users.FindOrCreateUser(context.Background(), "anonymous@gestalt")
		tokens, err := svc.Tokens.ListTokens(context.Background(), principal.UserSubjectID(u.ID))
		if err != nil {
			t.Fatalf("ListTokens: %v", err)
		}
		if len(tokens) != 1 {
			t.Fatalf("expected original token to be restored, got %d tokens", len(tokens))
		}
		if tokens[0].AccessToken != "first-key" {
			t.Fatalf("expected original access token first-key after rollback, got %q", tokens[0].AccessToken)
		}
		oldExternalIdentityID := testExternalIdentityResourceID("slack_identity", "team:T123:user:U456")
		newExternalIdentityID := testExternalIdentityResourceID("slack_identity", "team:T999:user:U999")
		oldResp, err := authzProvider.ReadRelationships(context.Background(), &core.ReadRelationshipsRequest{
			Relation: authorization.ProviderExternalIdentityRelationAssume,
			Resource: &core.ResourceRef{
				Type: authorization.ProviderResourceTypeExternalIdentity,
				Id:   oldExternalIdentityID,
			},
		})
		if err != nil {
			t.Fatalf("ReadRelationships old identity: %v", err)
		}
		if len(oldResp.GetRelationships()) != 1 {
			t.Fatalf("expected original external identity relationship to remain, got %+v", oldResp.GetRelationships())
		}
		newResp, err := authzProvider.ReadRelationships(context.Background(), &core.ReadRelationshipsRequest{
			Relation: authorization.ProviderExternalIdentityRelationAssume,
			Resource: &core.ResourceRef{
				Type: authorization.ProviderResourceTypeExternalIdentity,
				Id:   newExternalIdentityID,
			},
		})
		if err != nil {
			t.Fatalf("ReadRelationships new identity: %v", err)
		}
		if len(newResp.GetRelationships()) != 0 {
			t.Fatalf("expected new external identity relationship to be absent after rollback, got %+v", newResp.GetRelationships())
		}
	})

	t.Run("selection_required", func(t *testing.T) {
		t.Parallel()

		discoverySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `[{"id":"site-a","name":"Site A","workspace":"alpha"},{"id":"site-b","name":"Site B","workspace":"beta"}]`)
		}))
		testutil.CloseOnCleanup(t, discoverySrv)

		var auditBuf bytes.Buffer
		auditSink := invocation.NewSlogAuditSink(&auditBuf)
		svc := coretesting.NewStubServices(t)
		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Auth = &coretesting.StubAuthProvider{
				N: "stub",
				ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
					switch token {
					case "same-user-token":
						return &core.UserIdentity{Email: "same@test.local"}, nil
					case "other-user-token":
						return &core.UserIdentity{Email: "other@test.local"}, nil
					default:
						return nil, fmt.Errorf("bad token")
					}
				},
			}
			cfg.Providers = testutil.NewProviderRegistry(t, &stubDiscoveringManualProvider{
				stubManualProvider: stubManualProvider{
					StubIntegration: coretesting.StubIntegration{N: "manual-svc"},
				},
				discovery: &core.DiscoveryConfig{
					URL:      discoverySrv.URL,
					IDPath:   "id",
					NamePath: "name",
					Metadata: map[string]string{"workspace": "workspace"},
				},
			})
			cfg.DefaultConnection = map[string]string{"manual-svc": config.PluginConnectionName}
			cfg.Services = svc
			cfg.AuditSink = auditSink
		})
		testutil.CloseOnCleanup(t, ts)

		noRedirect := &http.Client{
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}

		connectBody := bytes.NewBufferString(`{"integration":"manual-svc","credential":"my-api-key"}`)
		connectReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/connect-manual", connectBody)
		connectReq.Header.Set("Content-Type", "application/json")
		connectReq.Header.Set("Authorization", "Bearer same-user-token")
		connectResp, err := noRedirect.Do(connectReq)
		if err != nil {
			t.Fatalf("connect request: %v", err)
		}
		defer func() { _ = connectResp.Body.Close() }()

		if connectResp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", connectResp.StatusCode)
		}

		var connectResult struct {
			Status       string `json:"status"`
			Integration  string `json:"integration"`
			SelectionURL string `json:"selectionUrl"`
			PendingToken string `json:"pendingToken"`
			Candidates   []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"candidates"`
		}
		if err := json.NewDecoder(connectResp.Body).Decode(&connectResult); err != nil {
			t.Fatalf("decode connect result: %v", err)
		}
		if connectResult.Status != "selection_required" {
			t.Fatalf("expected selection_required, got %q", connectResult.Status)
		}
		if connectResult.Integration != "manual-svc" {
			t.Fatalf("expected integration %q, got %q", "manual-svc", connectResult.Integration)
		}
		if connectResult.SelectionURL != pendingSelectionPath {
			t.Fatalf("expected selection URL %q, got %q", pendingSelectionPath, connectResult.SelectionURL)
		}
		if connectResult.PendingToken == "" {
			t.Fatal("expected pending token")
		}
		if len(connectResult.Candidates) != 2 {
			t.Fatalf("expected 2 candidates, got %d", len(connectResult.Candidates))
		}
		if connectResult.Candidates[0].Name != "Site A" || connectResult.Candidates[1].ID != "site-b" {
			t.Fatalf("unexpected candidates: %+v", connectResult.Candidates)
		}
		renderForm := url.Values{"pending_token": {connectResult.PendingToken}}
		renderReq, _ := http.NewRequest(http.MethodPost, ts.URL+connectResult.SelectionURL, strings.NewReader(renderForm.Encode()))
		renderReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		renderReq.AddCookie(&http.Cookie{Name: "session_token", Value: "same-user-token"})
		pageResp, err := noRedirect.Do(renderReq)
		if err != nil {
			t.Fatalf("selection page request: %v", err)
		}
		defer func() { _ = pageResp.Body.Close() }()

		if pageResp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", pageResp.StatusCode)
		}
		pageBody, err := io.ReadAll(pageResp.Body)
		if err != nil {
			t.Fatalf("read page body: %v", err)
		}
		pageText := string(pageBody)
		if !strings.Contains(pageText, "Select a manual-svc connection") {
			t.Fatalf("expected selection page body, got %q", pageText)
		}
		if !strings.Contains(pageText, "Site A") || !strings.Contains(pageText, "Site B") {
			t.Fatalf("expected both candidates in selection page, got %q", pageText)
		}
		if !strings.Contains(pageText, "name=\"pending_token\"") {
			t.Fatalf("expected pending token in selection page, got %q", pageText)
		}
		if !strings.Contains(pageText, "name=\"candidate_index\"") {
			t.Fatalf("expected candidate index in selection page, got %q", pageText)
		}

		noAuthForm := url.Values{
			"pending_token":   {connectResult.PendingToken},
			"candidate_index": {"1"},
		}
		noAuthReq, _ := http.NewRequest(http.MethodPost, ts.URL+connectResult.SelectionURL, strings.NewReader(noAuthForm.Encode()))
		noAuthReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		noAuthResp, err := noRedirect.Do(noAuthReq)
		if err != nil {
			t.Fatalf("unauthenticated request: %v", err)
		}
		defer func() { _ = noAuthResp.Body.Close() }()
		if noAuthResp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401 without auth, got %d", noAuthResp.StatusCode)
		}
		lines := bytes.Split(bytes.TrimSpace(auditBuf.Bytes()), []byte("\n"))
		if len(lines) == 0 {
			t.Fatal("expected pending connection audit record")
		}
		var noAuthAudit map[string]any
		if err := json.Unmarshal(lines[len(lines)-1], &noAuthAudit); err != nil {
			t.Fatalf("parsing pending connection audit record: %v\nraw: %s", err, auditBuf.String())
		}
		if noAuthAudit["operation"] != "connection.pending.select" {
			t.Fatalf("expected pending connection audit operation, got %v", noAuthAudit["operation"])
		}
		if noAuthAudit["allowed"] != false {
			t.Fatalf("expected denied pending connection audit, got %v", noAuthAudit["allowed"])
		}
		if subjectID, ok := noAuthAudit["subject_id"]; ok && subjectID != "" {
			t.Fatalf("expected unauthenticated denied selection to omit subject_id, got %v", subjectID)
		}
		if noAuthAudit["target_kind"] != "connection" {
			t.Fatalf("expected pending connection target_kind connection, got %v", noAuthAudit["target_kind"])
		}
		if noAuthAudit["target_id"] != "manual-svc/plugin/default" {
			t.Fatalf("expected pending connection target_id manual-svc/plugin/default, got %v", noAuthAudit["target_id"])
		}
		if noAuthAudit["target_name"] != "plugin/default" {
			t.Fatalf("expected pending connection target_name plugin/default, got %v", noAuthAudit["target_name"])
		}

		mismatchForm := url.Values{
			"pending_token":   {connectResult.PendingToken},
			"candidate_index": {"1"},
		}
		mismatchReq, _ := http.NewRequest(http.MethodPost, ts.URL+connectResult.SelectionURL, strings.NewReader(mismatchForm.Encode()))
		mismatchReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		mismatchReq.AddCookie(&http.Cookie{Name: "session_token", Value: "other-user-token"})
		mismatchResp, err := noRedirect.Do(mismatchReq)
		if err != nil {
			t.Fatalf("mismatch request: %v", err)
		}
		defer func() { _ = mismatchResp.Body.Close() }()

		if mismatchResp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404 for mismatched user, got %d", mismatchResp.StatusCode)
		}
		lines = bytes.Split(bytes.TrimSpace(auditBuf.Bytes()), []byte("\n"))
		if len(lines) == 0 {
			t.Fatal("expected pending connection audit record")
		}
		var mismatchAudit map[string]any
		if err := json.Unmarshal(lines[len(lines)-1], &mismatchAudit); err != nil {
			t.Fatalf("parsing mismatched pending connection audit record: %v\nraw: %s", err, auditBuf.String())
		}
		if mismatchAudit["operation"] != "connection.pending.select" {
			t.Fatalf("expected pending connection audit operation, got %v", mismatchAudit["operation"])
		}
		if mismatchAudit["allowed"] != false {
			t.Fatalf("expected denied pending connection audit, got %v", mismatchAudit["allowed"])
		}
		if mismatchAudit["subject_id"] == principal.UserSubjectID("u1") {
			t.Fatalf("expected denied selection not to be attributed to token owner, got %v", mismatchAudit["subject_id"])
		}
		form := url.Values{
			"pending_token":   {connectResult.PendingToken},
			"candidate_index": {"1"},
		}
		selectReq, _ := http.NewRequest(http.MethodPost, ts.URL+connectResult.SelectionURL, strings.NewReader(form.Encode()))
		selectReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		selectReq.AddCookie(&http.Cookie{Name: "session_token", Value: "same-user-token"})
		selectResp, err := noRedirect.Do(selectReq)
		if err != nil {
			t.Fatalf("select request: %v", err)
		}
		defer func() { _ = selectResp.Body.Close() }()

		if selectResp.StatusCode != http.StatusSeeOther {
			t.Fatalf("expected 303, got %d", selectResp.StatusCode)
		}
		if loc := selectResp.Header.Get("Location"); loc != "/integrations?connected=manual-svc" {
			t.Fatalf("expected redirect to /integrations?connected=manual-svc, got %q", loc)
		}
		u, _ := svc.Users.FindOrCreateUser(context.Background(), "same@test.local")
		tokens, _ := svc.Tokens.ListTokens(context.Background(), principal.UserSubjectID(u.ID))
		if len(tokens) == 0 {
			t.Fatal("expected token to be stored")
		}
		stored := tokens[0]
		if stored.AccessToken != "my-api-key" {
			t.Fatalf("expected access token my-api-key, got %q", stored.AccessToken)
		}
		var metadata map[string]string
		if err := json.Unmarshal([]byte(stored.MetadataJSON), &metadata); err != nil {
			t.Fatalf("unmarshal metadata: %v", err)
		}
		if metadata["workspace"] != "beta" {
			t.Fatalf("expected workspace metadata beta, got %q", metadata["workspace"])
		}
	})

	t.Run("rejects unknown connection params when provider exposes none", func(t *testing.T) {
		t.Parallel()

		var auditBuf bytes.Buffer
		prov := coreintegration.NewRestricted(&stubManualProviderWithCapabilities{
			stubManualProvider: stubManualProvider{
				StubIntegration: coretesting.StubIntegration{N: "manual-svc"},
			},
			connectionParams: map[string]core.ConnectionParamDef{},
		}, map[string]string{})

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, prov)
			cfg.DefaultConnection = map[string]string{"manual-svc": config.PluginConnectionName}
			cfg.PluginDefs = map[string]*config.ProviderEntry{
				"manual-svc": {},
			}
			cfg.AuditSink = invocation.NewSlogAuditSink(&auditBuf)
			cfg.Services = coretesting.NewStubServices(t)
		})
		testutil.CloseOnCleanup(t, ts)

		body := bytes.NewBufferString(`{"integration":"manual-svc","credential":"my-api-key","connectionParams":{"unknown":"nope"}}`)
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/connect-manual", body)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusBadRequest {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 400, got %d: %s", resp.StatusCode, body)
		}

		var auditRecord map[string]any
		if err := json.Unmarshal(auditBuf.Bytes(), &auditRecord); err != nil {
			t.Fatalf("parsing audit record: %v\nraw: %s", err, auditBuf.String())
		}
		if auditRecord["target_id"] != "manual-svc/plugin/default" {
			t.Fatalf("expected audit target_id manual-svc/plugin/default, got %v", auditRecord["target_id"])
		}
	})

	t.Run("credential validation still wins before connection params", func(t *testing.T) {
		t.Parallel()

		prov := coreintegration.NewRestricted(&stubManualProviderWithCapabilities{
			stubManualProvider: stubManualProvider{
				StubIntegration: coretesting.StubIntegration{N: "manual-svc"},
			},
			connectionParams: map[string]core.ConnectionParamDef{},
		}, map[string]string{})

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, prov)
			cfg.DefaultConnection = map[string]string{"manual-svc": config.PluginConnectionName}
			cfg.PluginDefs = map[string]*config.ProviderEntry{
				"manual-svc": {},
			}
			cfg.Services = coretesting.NewStubServices(t)
		})
		testutil.CloseOnCleanup(t, ts)

		body := bytes.NewBufferString(`{"integration":"manual-svc","connectionParams":{"unknown":"nope"}}`)
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/connect-manual", body)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusBadRequest {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 400, got %d: %s", resp.StatusCode, body)
		}

		var result map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decoding: %v", err)
		}
		if result["error"] != "credential is required" {
			t.Fatalf("expected credential validation error, got %q", result["error"])
		}
	})

	t.Run("composite wrappers preserve discovery and connection metadata", func(t *testing.T) {
		t.Parallel()

		discoverySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `[{"id":"site-a","name":"Site A","workspace":"alpha"}]`)
		}))
		testutil.CloseOnCleanup(t, discoverySrv)

		svc := coretesting.NewStubServices(t)
		apiProv := &stubManualProviderWithCapabilities{
			stubManualProvider: stubManualProvider{
				StubIntegration: coretesting.StubIntegration{N: "manual-svc"},
			},
			connectionParams: map[string]core.ConnectionParamDef{
				"tenant": {
					Required:    true,
					Description: "Tenant slug",
				},
			},
			discovery: &core.DiscoveryConfig{
				URL:      discoverySrv.URL,
				IDPath:   "id",
				NamePath: "name",
				Metadata: map[string]string{"workspace": "workspace"},
			},
		}
		prov := composite.New("manual-svc", apiProv, &stubIntegrationWithSessionCatalog{
			stubIntegrationWithOps: stubIntegrationWithOps{
				StubIntegration: coretesting.StubIntegration{N: "manual-svc-mcp", ConnMode: core.ConnectionModeNone},
			},
		})

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, prov)
			cfg.DefaultConnection = map[string]string{"manual-svc": config.PluginConnectionName}
			cfg.PluginDefs = map[string]*config.ProviderEntry{
				"manual-svc": {},
			}
			cfg.Services = svc
		})
		testutil.CloseOnCleanup(t, ts)

		body := bytes.NewBufferString(`{"integration":"manual-svc","credential":"my-api-key","connectionParams":{"tenant":"acme"}}`)
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/connect-manual", body)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		var result map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decoding: %v", err)
		}
		if result["status"] != "connected" {
			t.Fatalf("expected connected, got %q", result["status"])
		}

		u, _ := svc.Users.FindOrCreateUser(context.Background(), "anonymous@gestalt")
		tokens, _ := svc.Tokens.ListTokens(context.Background(), principal.UserSubjectID(u.ID))
		if len(tokens) != 1 {
			t.Fatalf("expected 1 stored token, got %d", len(tokens))
		}

		var metadata map[string]string
		if err := json.Unmarshal([]byte(tokens[0].MetadataJSON), &metadata); err != nil {
			t.Fatalf("unmarshal metadata: %v", err)
		}
		if !reflect.DeepEqual(metadata, map[string]string{
			"tenant":    "acme",
			"workspace": "alpha",
		}) {
			t.Fatalf("metadata = %+v", metadata)
		}
	})

	t.Run("restricted wrappers preserve discovery and connection metadata", func(t *testing.T) {
		t.Parallel()

		discoverySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `[{"id":"site-a","name":"Site A","workspace":"alpha"}]`)
		}))
		testutil.CloseOnCleanup(t, discoverySrv)

		svc := coretesting.NewStubServices(t)
		prov := coreintegration.NewRestricted(&stubManualProviderWithCapabilities{
			stubManualProvider: stubManualProvider{
				StubIntegration: coretesting.StubIntegration{N: "manual-svc"},
			},
			connectionParams: map[string]core.ConnectionParamDef{
				"tenant": {
					Required:    true,
					Description: "Tenant slug",
				},
			},
			discovery: &core.DiscoveryConfig{
				URL:      discoverySrv.URL,
				IDPath:   "id",
				NamePath: "name",
				Metadata: map[string]string{"workspace": "workspace"},
			},
		}, map[string]string{})

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, prov)
			cfg.DefaultConnection = map[string]string{"manual-svc": config.PluginConnectionName}
			cfg.PluginDefs = map[string]*config.ProviderEntry{
				"manual-svc": {},
			}
			cfg.Services = svc
		})
		testutil.CloseOnCleanup(t, ts)

		body := bytes.NewBufferString(`{"integration":"manual-svc","credential":"my-api-key","connectionParams":{"tenant":"acme"}}`)
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/connect-manual", body)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		var result map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decoding: %v", err)
		}
		if result["status"] != "connected" {
			t.Fatalf("expected connected, got %q", result["status"])
		}

		u, _ := svc.Users.FindOrCreateUser(context.Background(), "anonymous@gestalt")
		tokens, _ := svc.Tokens.ListTokens(context.Background(), principal.UserSubjectID(u.ID))
		if len(tokens) != 1 {
			t.Fatalf("expected 1 stored token, got %d", len(tokens))
		}

		var metadata map[string]string
		if err := json.Unmarshal([]byte(tokens[0].MetadataJSON), &metadata); err != nil {
			t.Fatalf("unmarshal metadata: %v", err)
		}
		if !reflect.DeepEqual(metadata, map[string]string{
			"tenant":    "acme",
			"workspace": "alpha",
		}) {
			t.Fatalf("metadata = %+v", metadata)
		}
	})
}

func TestConnectManual_OAuthProviderRejected(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{N: "slack"})
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"integration":"slack","credential":"some-key"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/connect-manual", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestConnectManual_MissingFields(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/connect-manual", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestConnectManual_UnknownIntegration(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"integration":"nonexistent","credential":"key"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/connect-manual", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestStartOAuth_ManualProviderRejected(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, &stubManualProvider{
			StubIntegration: coretesting.StubIntegration{N: "manual-svc"},
		})
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"integration":"manual-svc","scopes":[]}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/start-oauth", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if result["error"] == "" {
		t.Fatal("expected error message in response")
	}
}

func TestStartOAuth_MultiConnection_SelectsByConnectionName(t *testing.T) {
	t.Parallel()

	connAHandler := &testOAuthHandler{
		authorizationBaseURLVal: "https://provider.example/oauth/a",
	}
	connBHandler := &testOAuthHandler{
		authorizationBaseURLVal: "https://provider.example/oauth/b",
	}

	stub := &stubIntegrationWithAuthURL{
		StubIntegration: coretesting.StubIntegration{N: "multi"},
		authURL:         "https://provider.example/oauth/a",
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"multi": "conn-a"}
		cfg.ConnectionAuth = func() map[string]map[string]bootstrap.OAuthHandler {
			return map[string]map[string]bootstrap.OAuthHandler{
				"multi": {
					"conn-a": connAHandler,
					"conn-b": connBHandler,
				},
			}
		}
		cfg.Services = coretesting.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"integration":"multi","connection":"conn-b"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/start-oauth", body)
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, bodyBytes)
	}
	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if !strings.Contains(result["url"], "provider.example/oauth/b") {
		t.Fatalf("expected conn-b auth URL, got %q", result["url"])
	}
}

func TestStartOAuth_MultiConnectionWithoutDefaultRequiresExplicitConnection(t *testing.T) {
	t.Parallel()

	stub := &stubIntegrationWithAuthURL{
		StubIntegration: coretesting.StubIntegration{N: "multi"},
		authURL:         "https://provider.example/oauth/a",
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.ConnectionAuth = func() map[string]map[string]bootstrap.OAuthHandler {
			return map[string]map[string]bootstrap.OAuthHandler{
				"multi": {
					"conn-a": &testOAuthHandler{authorizationBaseURLVal: "https://provider.example/oauth/a"},
					"conn-b": &testOAuthHandler{authorizationBaseURLVal: "https://provider.example/oauth/b"},
				},
			}
		}
		cfg.Services = coretesting.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"integration":"multi"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/start-oauth", body)
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if !strings.Contains(result["error"], "requires an explicit connection") {
		t.Fatalf("expected explicit-connection error, got %q", result["error"])
	}
}

func TestStartOAuth_MissingConnection_FailsCleanly(t *testing.T) {
	t.Parallel()

	handler := &testOAuthHandler{
		authorizationBaseURLVal: "https://provider.example/oauth",
	}

	stub := &stubIntegrationWithAuthURL{
		StubIntegration: coretesting.StubIntegration{N: "myint"},
		authURL:         "https://provider.example/oauth",
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"myint": "conn-a"}
		cfg.ConnectionAuth = testConnectionAuth("myint", handler)
		cfg.Services = coretesting.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"integration":"myint","connection":"nonexistent"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/start-oauth", body)
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if !strings.Contains(result["error"], "nonexistent") {
		t.Fatalf("expected error to mention missing connection, got %q", result["error"])
	}
}

func TestOAuthCallback_UsesStateConnection(t *testing.T) {
	t.Parallel()

	var exchangedConnection string
	handler := &testOAuthHandler{
		authorizationBaseURLVal: "https://provider.example/oauth",
		exchangeCodeFn: func(_ context.Context, code string) (*core.TokenResponse, error) {
			exchangedConnection = "conn-b"
			return &core.TokenResponse{AccessToken: "token-for-b"}, nil
		},
	}

	stub := &stubIntegrationWithAuthURL{
		StubIntegration: coretesting.StubIntegration{N: "multi"},
		authURL:         "https://provider.example/oauth",
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"multi": "conn-a"}
		cfg.ConnectionAuth = func() map[string]map[string]bootstrap.OAuthHandler {
			return map[string]map[string]bootstrap.OAuthHandler{
				"multi": {
					"conn-a": &testOAuthHandler{authorizationBaseURLVal: "https://provider.example/oauth/a"},
					"conn-b": handler,
				},
			}
		}
		cfg.Services = coretesting.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	startBody := bytes.NewBufferString(`{"integration":"multi","connection":"conn-b"}`)
	startReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/start-oauth", startBody)
	startReq.Header.Set("X-Dev-User-Email", "dev@example.com")
	startReq.Header.Set("Content-Type", "application/json")
	startResp, err := http.DefaultClient.Do(startReq)
	if err != nil {
		t.Fatalf("start request: %v", err)
	}
	defer func() { _ = startResp.Body.Close() }()
	if startResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from start-oauth, got %d", startResp.StatusCode)
	}
	var startResult map[string]string
	if err := json.NewDecoder(startResp.Body).Decode(&startResult); err != nil {
		t.Fatalf("decoding start response: %v", err)
	}

	noRedirect := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/auth/callback?code=ok&state="+url.QueryEscape(startResult["state"]), nil)
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatalf("callback request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d", resp.StatusCode)
	}
	if exchangedConnection != "conn-b" {
		t.Fatalf("expected conn-b handler to be used for exchange, got %q", exchangedConnection)
	}
}

func TestRefresh_UsesConnectionAuthHandlers(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name             string
		metadataJSON     string
		tokenURL         string
		wantRefreshedURL string
	}{
		{
			name: "direct refresh uses connection handler",
		},
		{
			name:             "resolved token URL uses override refresh handler",
			metadataJSON:     `{"tenant":"acme"}`,
			tokenURL:         "https://{tenant}.example.com/oauth/token",
			wantRefreshedURL: "https://acme.example.com/oauth/token",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := coretesting.NewStubServices(t)
			u := seedUser(t, svc, "anonymous@gestalt")
			expired := time.Now().Add(-1 * time.Hour)
			seedToken(t, svc, &core.IntegrationToken{
				ID:           "tok1",
				SubjectID:    principal.UserSubjectID(u.ID),
				Integration:  "fake",
				Connection:   "default",
				Instance:     "default",
				AccessToken:  "old-token",
				RefreshToken: "old-refresh",
				ExpiresAt:    &expired,
				MetadataJSON: tc.metadataJSON,
			})

			var refreshedToken string
			var refreshedURL string
			var usedToken string
			stub := &stubOAuthIntegration{
				stubIntegrationWithOps: stubIntegrationWithOps{
					StubIntegration: coretesting.StubIntegration{
						N: "fake",
						ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
							usedToken = token
							return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
						},
					},
					ops: []core.Operation{{Name: "list", Description: "List", Method: http.MethodGet}},
				},
			}
			handler := &testOAuthHandler{
				tokenURLVal: tc.tokenURL,
				refreshTokenFn: func(_ context.Context, rt string) (*core.TokenResponse, error) {
					if tc.wantRefreshedURL != "" {
						t.Fatalf("expected refresh to use resolved token URL override")
					}
					refreshedToken = rt
					return &core.TokenResponse{AccessToken: "refreshed-token", ExpiresIn: 3600}, nil
				},
				refreshTokenWithURLFn: func(_ context.Context, rt, tokenURL string) (*core.TokenResponse, error) {
					if tc.wantRefreshedURL == "" {
						t.Fatalf("expected direct refresh without token URL override")
					}
					refreshedToken = rt
					refreshedURL = tokenURL
					return &core.TokenResponse{AccessToken: "refreshed-token", ExpiresIn: 3600}, nil
				},
			}

			ts := newTestServer(t, func(cfg *server.Config) {
				cfg.Providers = testutil.NewProviderRegistry(t, stub)
				cfg.DefaultConnection = map[string]string{"fake": testDefaultConnection}
				cfg.ConnectionAuth = testConnectionAuth("fake", handler)
				cfg.Services = svc
			})
			testutil.CloseOnCleanup(t, ts)

			req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/fake/list", nil)
			req.Header.Set("X-Dev-User-Email", "dev@example.com")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
			}
			if refreshedToken != "old-refresh" {
				t.Fatalf("refresh token = %q, want %q", refreshedToken, "old-refresh")
			}
			if refreshedURL != tc.wantRefreshedURL {
				t.Fatalf("resolved token URL = %q, want %q", refreshedURL, tc.wantRefreshedURL)
			}
			if usedToken != "refreshed-token" {
				t.Fatalf("used token = %q, want %q", usedToken, "refreshed-token")
			}
		})
	}
}

func TestRefresh_UsesResolvedConnectionTokenURL(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	expired := time.Now().Add(-1 * time.Hour)
	seedToken(t, svc, &core.IntegrationToken{
		ID:           "tok1",
		SubjectID:    principal.UserSubjectID(u.ID),
		Integration:  "fake",
		Connection:   "default",
		Instance:     "default",
		AccessToken:  "old-token",
		RefreshToken: "old-refresh",
		ExpiresAt:    &expired,
		MetadataJSON: `{"tenant":"acme"}`,
	})

	var refreshedURL string
	var refreshedToken string
	var usedToken string
	stub := &stubOAuthIntegration{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N: "fake",
				ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
					usedToken = token
					return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
				},
			},
			ops: []core.Operation{{Name: "list", Description: "List", Method: http.MethodGet}},
		},
	}
	handler := &testOAuthHandler{
		tokenURLVal: "https://{tenant}.example.com/oauth/token",
		refreshTokenFn: func(context.Context, string) (*core.TokenResponse, error) {
			t.Fatal("expected refresh to use resolved token URL override")
			return nil, nil
		},
		refreshTokenWithURLFn: func(_ context.Context, rt, tokenURL string) (*core.TokenResponse, error) {
			refreshedToken = rt
			refreshedURL = tokenURL
			return &core.TokenResponse{AccessToken: "refreshed-token", ExpiresIn: 3600}, nil
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"fake": testDefaultConnection}
		cfg.ConnectionAuth = testConnectionAuth("fake", handler)
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/fake/list", nil)
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if refreshedToken != "old-refresh" {
		t.Fatalf("expected refresh token old-refresh, got %q", refreshedToken)
	}
	if refreshedURL != "https://acme.example.com/oauth/token" {
		t.Fatalf("expected resolved token URL, got %q", refreshedURL)
	}
	if usedToken != "refreshed-token" {
		t.Fatalf("expected operation to use refreshed token, got %q", usedToken)
	}
}

func newMCPHandler(t *testing.T, providers *registry.ProviderMap[core.Provider], svc *coredata.Services, auditSink core.AuditSink, authorizer *authorization.Authorizer) http.Handler {
	t.Helper()
	brokerOpts := []invocation.BrokerOption{}
	if authorizer != nil {
		brokerOpts = append(brokerOpts, invocation.WithAuthorizer(authorizer))
	}
	broker := invocation.NewBroker(providers, svc.Users, svc.Tokens, brokerOpts...)
	mcpInvoker := invocation.NewGuarded(broker, broker, "mcp", auditSink)
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       mcpInvoker,
		TokenResolver: broker,
		AuditSink:     auditSink,
		Providers:     providers,
		Authorizer:    authorizer,
	})
	return mcpserver.NewStreamableHTTPServer(srv, mcpserver.WithStateLess(true))
}

func mcpJSONRPCWithHeaders(t *testing.T, ts *httptest.Server, headers map[string]string, body map[string]any) (int, map[string]any, http.Header) {
	t.Helper()
	payload, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatalf("decoding MCP response: %v\nbody: %s", err, raw)
		}
	}
	return resp.StatusCode, result, resp.Header.Clone()
}

func mcpJSONRPC(t *testing.T, ts *httptest.Server, headers map[string]string, body map[string]any) (int, map[string]any) {
	t.Helper()
	status, result, _ := mcpJSONRPCWithHeaders(t, ts, headers, body)
	return status, result
}

func TestMCPEndpoint_InitializeAndListTools(t *testing.T) {
	t.Parallel()

	stub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{N: "linear"},
		ops: []core.Operation{
			{Name: "search_issues", Description: "Search issues", Method: http.MethodGet},
		},
	}
	svc := coretesting.NewStubServices(t)
	providers := testutil.NewProviderRegistry(t, stub)
	mcpHandler := newMCPHandler(t, providers, svc, nil, nil)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = providers
		cfg.Services = svc
		cfg.MCPHandler = mcpHandler
	})
	defer ts.Close()

	status, resp := mcpJSONRPC(t, ts, nil, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test", "version": "1.0"},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("initialize: expected 200, got %d", status)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("initialize: expected result object, got %v", resp)
	}
	if result["serverInfo"] == nil {
		t.Fatal("initialize: missing serverInfo")
	}

	status, resp = mcpJSONRPC(t, ts, nil, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
	})
	if status != http.StatusOK {
		t.Fatalf("tools/list: expected 200, got %d", status)
	}
	result, ok = resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("tools/list: expected result object, got %v", resp)
	}
	tools, ok := result["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatalf("tools/list: expected non-empty tools, got %v", result)
	}
	firstTool := tools[0].(map[string]any)
	if firstTool["name"] != "linear_search_issues" {
		t.Fatalf("expected tool linear_search_issues, got %v", firstTool["name"])
	}
}

func TestMCPEndpoint_RequiresAuth(t *testing.T) {
	t.Parallel()

	providers := func() *registry.ProviderMap[core.Provider] {
		reg := registry.New()
		return &reg.Providers
	}()
	svc := coretesting.NewStubServices(t)
	mcpHandler := newMCPHandler(t, providers, svc, nil, nil)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
		cfg.MCPHandler = mcpHandler
	})
	defer ts.Close()

	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test", "version": "1.0"},
		},
	})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth, got %d", resp.StatusCode)
	}
	wantAuth := `Bearer resource_metadata="` + ts.URL + `/.well-known/oauth-protected-resource/mcp"`
	if got := resp.Header.Get("WWW-Authenticate"); got != wantAuth {
		t.Fatalf("WWW-Authenticate = %q, want %q", got, wantAuth)
	}
}

func TestMCPProtectedResourceMetadata(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	mcpHandler := newMCPHandler(t, func() *registry.ProviderMap[core.Provider] {
		reg := registry.New()
		return &reg.Providers
	}(), svc, nil, nil)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &stubAuthWithLoginURL{
			StubAuthProvider: coretesting.StubAuthProvider{N: "oidc"},
			loginURL:         "https://accounts.example.test/authorize?scope=openid+email+profile",
		}
		cfg.MCPHandler = mcpHandler
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/.well-known/oauth-protected-resource/mcp")
	if err != nil {
		t.Fatalf("GET protected resource metadata: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want %q", got, "application/json")
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}

	if got := body["resource"]; got != ts.URL+"/mcp" {
		t.Fatalf("resource = %v, want %q", got, ts.URL+"/mcp")
	}
	authServers, _ := body["authorization_servers"].([]any)
	if len(authServers) != 1 || authServers[0] != ts.URL {
		t.Fatalf("authorization_servers = %v, want [%s]", authServers, ts.URL)
	}
	if got := body["authorization_endpoint"]; got != ts.URL+"/oauth/authorize" {
		t.Fatalf("authorization_endpoint = %v, want %q", got, ts.URL+"/oauth/authorize")
	}
	if got := body["token_endpoint"]; got != ts.URL+"/oauth/token" {
		t.Fatalf("token_endpoint = %v, want %q", got, ts.URL+"/oauth/token")
	}
	if got := body["registration_endpoint"]; got != ts.URL+"/oauth/register" {
		t.Fatalf("registration_endpoint = %v, want %q", got, ts.URL+"/oauth/register")
	}
	scopes, _ := body["scopes_supported"].([]any)
	if !reflect.DeepEqual(scopes, []any{"openid", "email", "profile"}) {
		t.Fatalf("scopes_supported = %v, want [openid email profile]", scopes)
	}
	bearerMethods, _ := body["bearer_methods_supported"].([]any)
	if !reflect.DeepEqual(bearerMethods, []any{"header"}) {
		t.Fatalf("bearer_methods_supported = %v, want [header]", bearerMethods)
	}
	challengeMethods, _ := body["code_challenge_methods_supported"].([]any)
	if !reflect.DeepEqual(challengeMethods, []any{"S256"}) {
		t.Fatalf("code_challenge_methods_supported = %v, want [S256]", challengeMethods)
	}
	authMethods, _ := body["token_endpoint_auth_methods_supported"].([]any)
	if !reflect.DeepEqual(authMethods, []any{"none", "client_secret_post", "client_secret_basic"}) {
		t.Fatalf("token_endpoint_auth_methods_supported = %v, want [none client_secret_post client_secret_basic]", authMethods)
	}
}

func TestMCPProtectedResourceMetadataRoute_PrecedesRootMountedUIFallback(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>root-shell</html>"), 0o644); err != nil {
		t.Fatalf("WriteFile index.html: %v", err)
	}
	handler, err := testutilUIHandler(dir)
	if err != nil {
		t.Fatalf("ui handler: %v", err)
	}

	svc := coretesting.NewStubServices(t)
	mcpHandler := newMCPHandler(t, func() *registry.ProviderMap[core.Provider] {
		reg := registry.New()
		return &reg.Providers
	}(), svc, nil, nil)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &stubAuthWithLoginURL{
			StubAuthProvider: coretesting.StubAuthProvider{N: "oidc"},
			loginURL:         "https://accounts.example.test/authorize",
		}
		cfg.MountedUIs = []server.MountedUI{{
			Path:    "/",
			Handler: handler,
		}}
		cfg.MCPHandler = mcpHandler
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/.well-known/oauth-protected-resource/mcp")
	if err != nil {
		t.Fatalf("GET protected resource metadata with root UI: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll metadata response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want %q", got, "application/json")
	}
	if strings.Contains(string(body), "root-shell") {
		t.Fatalf("body = %q, want JSON metadata instead of root UI shell", body)
	}
}

func TestMCPAuthorizationServerMetadata(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	mcpHandler := newMCPHandler(t, func() *registry.ProviderMap[core.Provider] {
		reg := registry.New()
		return &reg.Providers
	}(), svc, nil, nil)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &stubAuthWithLoginURL{
			StubAuthProvider: coretesting.StubAuthProvider{N: "oidc"},
			loginURL:         "https://accounts.example.test/authorize?scope=openid+email+profile",
		}
		cfg.MCPHandler = mcpHandler
	})
	testutil.CloseOnCleanup(t, ts)

	for _, path := range []string{"/.well-known/oauth-authorization-server", "/.well-known/oauth-authorization-server/mcp"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			t.Fatalf("%s status = %d, want %d", path, resp.StatusCode, http.StatusOK)
		}

		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			_ = resp.Body.Close()
			t.Fatalf("decode %s JSON: %v", path, err)
		}
		_ = resp.Body.Close()

		if got := body["issuer"]; got != ts.URL {
			t.Fatalf("%s issuer = %v, want %q", path, got, ts.URL)
		}
		if got := body["authorization_endpoint"]; got != ts.URL+"/oauth/authorize" {
			t.Fatalf("%s authorization_endpoint = %v, want %q", path, got, ts.URL+"/oauth/authorize")
		}
		if got := body["token_endpoint"]; got != ts.URL+"/oauth/token" {
			t.Fatalf("%s token_endpoint = %v, want %q", path, got, ts.URL+"/oauth/token")
		}
		if got := body["registration_endpoint"]; got != ts.URL+"/oauth/register" {
			t.Fatalf("%s registration_endpoint = %v, want %q", path, got, ts.URL+"/oauth/register")
		}
	}
}

func TestMCPAuthorizationServerMetadataRoute_PrecedesRootMountedUIFallback(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>root-shell</html>"), 0o644); err != nil {
		t.Fatalf("WriteFile index.html: %v", err)
	}
	handler, err := testutilUIHandler(dir)
	if err != nil {
		t.Fatalf("ui handler: %v", err)
	}

	svc := coretesting.NewStubServices(t)
	mcpHandler := newMCPHandler(t, func() *registry.ProviderMap[core.Provider] {
		reg := registry.New()
		return &reg.Providers
	}(), svc, nil, nil)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &stubAuthWithLoginURL{
			StubAuthProvider: coretesting.StubAuthProvider{N: "oidc"},
			loginURL:         "https://accounts.example.test/authorize",
		}
		cfg.MountedUIs = []server.MountedUI{{
			Path:    "/",
			Handler: handler,
		}}
		cfg.MCPHandler = mcpHandler
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/.well-known/oauth-authorization-server")
	if err != nil {
		t.Fatalf("GET auth server metadata with root UI: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll metadata response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want %q", got, "application/json")
	}
	if strings.Contains(string(body), "root-shell") {
		t.Fatalf("body = %q, want JSON metadata instead of root UI shell", body)
	}
}

func TestMCPOAuthAuthorizationCodeFlow(t *testing.T) {
	t.Parallel()

	secret := []byte("0123456789abcdef0123456789abcdef")
	svc := coretesting.NewStubServices(t)
	providers := func() *registry.ProviderMap[core.Provider] {
		reg := registry.New()
		return &reg.Providers
	}()
	mcpHandler := newMCPHandler(t, providers, svc, nil, nil)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &stubHostIssuedSessionAuth{secret: secret, name: "oidc"}
		cfg.StateSecret = secret
		cfg.MCPHandler = mcpHandler
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	registrationResp, err := http.Post(ts.URL+"/oauth/register", "application/json", strings.NewReader(`{
		"client_name":"Claude",
		"redirect_uris":["http://127.0.0.1:4318/callback"],
		"grant_types":["authorization_code","refresh_token"],
		"response_types":["code"],
		"token_endpoint_auth_method":"none"
	}`))
	if err != nil {
		t.Fatalf("POST /oauth/register: %v", err)
	}
	defer func() { _ = registrationResp.Body.Close() }()

	if registrationResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(registrationResp.Body)
		t.Fatalf("register status = %d, want %d: %s", registrationResp.StatusCode, http.StatusCreated, body)
	}

	var registration map[string]any
	if err := json.NewDecoder(registrationResp.Body).Decode(&registration); err != nil {
		t.Fatalf("decode registration response: %v", err)
	}
	clientID, _ := registration["client_id"].(string)
	if clientID == "" {
		t.Fatal("registration response missing client_id")
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	noRedirect := &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	redirectURI := "http://127.0.0.1:4318/callback"
	verifier := oauth.GenerateVerifier()
	challenge := oauth.ComputeS256Challenge(verifier)
	authState := "   "
	authorizeURL := ts.URL + "/oauth/authorize?response_type=code" +
		"&client_id=" + url.QueryEscape(clientID) +
		"&redirect_uri=" + url.QueryEscape(redirectURI) +
		"&scope=" + url.QueryEscape("openid email profile") +
		"&state=" + url.QueryEscape(authState) +
		"&code_challenge=" + url.QueryEscape(challenge) +
		"&code_challenge_method=S256"

	resp, err := noRedirect.Get(authorizeURL)
	if err != nil {
		t.Fatalf("GET /oauth/authorize: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("authorize status = %d, want %d: %s", resp.StatusCode, http.StatusFound, body)
	}
	loginURL := ts.URL + resp.Header.Get("Location")
	if !strings.Contains(loginURL, "/api/v1/auth/login?next=") {
		t.Fatalf("authorize redirect = %q, want /api/v1/auth/login?next=...", loginURL)
	}

	loginResp, err := noRedirect.Get(loginURL)
	if err != nil {
		t.Fatalf("GET login redirect: %v", err)
	}
	defer func() { _ = loginResp.Body.Close() }()

	if loginResp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(loginResp.Body)
		t.Fatalf("login status = %d, want %d: %s", loginResp.StatusCode, http.StatusFound, body)
	}
	idpURL := loginResp.Header.Get("Location")
	idpParsed, err := url.Parse(idpURL)
	if err != nil {
		t.Fatalf("parse IDP redirect: %v", err)
	}
	idpState := idpParsed.Query().Get("state")
	if idpState == "" {
		t.Fatal("IDP redirect missing state")
	}

	callbackResp, err := noRedirect.Get(ts.URL + "/api/v1/auth/login/callback?code=good-code&state=" + url.QueryEscape(idpState))
	if err != nil {
		t.Fatalf("GET login callback: %v", err)
	}
	defer func() { _ = callbackResp.Body.Close() }()

	if callbackResp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(callbackResp.Body)
		t.Fatalf("callback status = %d, want %d: %s", callbackResp.StatusCode, http.StatusFound, body)
	}

	resumeAuthorizeURL, err := url.Parse(callbackResp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse callback redirect: %v", err)
	}
	resumeAuthorize := callbackResp.Request.URL.ResolveReference(resumeAuthorizeURL).String()

	authorizedResp, err := noRedirect.Get(resumeAuthorize)
	if err != nil {
		t.Fatalf("GET resumed authorize: %v", err)
	}
	defer func() { _ = authorizedResp.Body.Close() }()

	if authorizedResp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(authorizedResp.Body)
		t.Fatalf("resumed authorize status = %d, want %d: %s", authorizedResp.StatusCode, http.StatusFound, body)
	}
	finalRedirect, err := url.Parse(authorizedResp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse final redirect: %v", err)
	}
	if got := finalRedirect.Scheme + "://" + finalRedirect.Host + finalRedirect.Path; got != redirectURI {
		t.Fatalf("final redirect target = %q, want %q", got, redirectURI)
	}
	if got := finalRedirect.Query().Get("state"); got != authState {
		t.Fatalf("final state = %q, want %q", got, authState)
	}
	code := finalRedirect.Query().Get("code")
	if code == "" {
		t.Fatal("final redirect missing code")
	}

	tokenResp, err := http.PostForm(ts.URL+"/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	})
	if err != nil {
		t.Fatalf("POST /oauth/token (authorization_code): %v", err)
	}
	defer func() { _ = tokenResp.Body.Close() }()

	if tokenResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(tokenResp.Body)
		t.Fatalf("token status = %d, want %d: %s", tokenResp.StatusCode, http.StatusOK, body)
	}

	var tokenBody map[string]any
	if err := json.NewDecoder(tokenResp.Body).Decode(&tokenBody); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	accessToken, _ := tokenBody["access_token"].(string)
	refreshToken, _ := tokenBody["refresh_token"].(string)
	if accessToken == "" {
		t.Fatal("token response missing access_token")
	}
	if refreshToken == "" {
		t.Fatal("token response missing refresh_token")
	}

	status, _ := mcpJSONRPC(t, ts, map[string]string{"Authorization": "Bearer " + accessToken}, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test", "version": "1.0"},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("initialize with access token: expected 200, got %d", status)
	}

	refreshResp, err := http.PostForm(ts.URL+"/oauth/token", url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientID},
		"refresh_token": {refreshToken},
	})
	if err != nil {
		t.Fatalf("POST /oauth/token (refresh_token): %v", err)
	}
	defer func() { _ = refreshResp.Body.Close() }()

	if refreshResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(refreshResp.Body)
		t.Fatalf("refresh status = %d, want %d: %s", refreshResp.StatusCode, http.StatusOK, body)
	}

	var refreshBody map[string]any
	if err := json.NewDecoder(refreshResp.Body).Decode(&refreshBody); err != nil {
		t.Fatalf("decode refresh response: %v", err)
	}
	refreshedAccessToken, _ := refreshBody["access_token"].(string)
	if refreshedAccessToken == "" {
		t.Fatal("refresh response missing access_token")
	}

	status, _ = mcpJSONRPC(t, ts, map[string]string{"Authorization": "Bearer " + refreshedAccessToken}, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test", "version": "1.0"},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("initialize with refreshed access token: expected 200, got %d", status)
	}
}

func TestMCPEndpoint_DirectPassthrough(t *testing.T) {
	t.Parallel()

	cat := &catalog.Catalog{
		Name: "clickhouse",
		Operations: []catalog.CatalogOperation{
			{
				ID:          "run_query",
				Description: "Execute a SQL query",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"sql":{"type":"string"}}}`),
				Transport:   catalog.TransportMCPPassthrough,
			},
		},
	}

	var calledName string
	var calledRequestID string
	var auditBuf bytes.Buffer
	auditSink := invocation.NewSlogAuditSink(&auditBuf)
	prov := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "clickhouse", ConnMode: core.ConnectionModeNone},
			ops:             []core.Operation{{Name: "run_query", Description: "Execute a SQL query"}},
		},
		catalog: cat,
		callFn: func(ctx context.Context, name string, _ map[string]any) (*mcpgo.CallToolResult, error) {
			calledName = name
			meta := invocation.MetaFromContext(ctx)
			if meta != nil {
				calledRequestID = meta.RequestID
			}
			return mcpgo.NewToolResultText("query executed"), nil
		},
	}

	svc := coretesting.NewStubServices(t)
	providers := testutil.NewProviderRegistry(t, prov)
	mcpHandler := newMCPHandler(t, providers, svc, auditSink, nil)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "session-token" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: "user@example.com"}, nil
			},
		}
		cfg.Providers = providers
		cfg.Services = svc
		cfg.MCPHandler = mcpHandler
	})
	defer ts.Close()

	headers := map[string]string{"Authorization": "Bearer session-token"}

	mcpJSONRPC(t, ts, headers, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test", "version": "1.0"},
		},
	})

	status, resp := mcpJSONRPC(t, ts, headers, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
	})
	if status != http.StatusOK {
		t.Fatalf("tools/list: expected 200, got %d", status)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("tools/list: expected result, got %v", resp)
	}
	tools, ok := result["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatalf("expected tools, got %v", result)
	}
	firstTool := tools[0].(map[string]any)
	if firstTool["name"] != "clickhouse_run_query" {
		t.Fatalf("expected clickhouse_run_query, got %v", firstTool["name"])
	}

	status, resp = mcpJSONRPC(t, ts, headers, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "clickhouse_run_query",
			"arguments": map[string]any{"sql": "SELECT 1"},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("tools/call: expected 200, got %d", status)
	}
	result, ok = resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("tools/call: expected result, got %v", resp)
	}
	if calledName != "run_query" {
		t.Fatalf("expected direct CallTool with run_query, got %q", calledName)
	}
	if calledRequestID == "" {
		t.Fatal("expected direct CallTool context to include request ID")
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("expected content in result, got %v", result)
	}
	textBlock := content[0].(map[string]any)
	if textBlock["text"] != "query executed" {
		t.Fatalf("expected passthrough result, got %v", textBlock)
	}

	lines := bytes.Split(bytes.TrimSpace(auditBuf.Bytes()), []byte("\n"))
	if len(lines) == 0 {
		t.Fatal("expected MCP audit record")
	}
	var auditRecord map[string]any
	if err := json.Unmarshal(lines[len(lines)-1], &auditRecord); err != nil {
		t.Fatalf("parsing MCP audit record: %v\nraw: %s", err, auditBuf.String())
	}
	if auditRecord["source"] != "mcp" {
		t.Fatalf("expected audit source mcp, got %v", auditRecord["source"])
	}
	if auditRecord["provider"] != "clickhouse" {
		t.Fatalf("expected audit provider clickhouse, got %v", auditRecord["provider"])
	}
	if auditRecord["operation"] != "run_query" {
		t.Fatalf("expected audit operation run_query, got %v", auditRecord["operation"])
	}
	if auditRecord["request_id"] != calledRequestID {
		t.Fatalf("expected audit request_id %q, got %v", calledRequestID, auditRecord["request_id"])
	}
	if auditRecord["auth_source"] != "session" {
		t.Fatalf("expected audit auth_source session, got %v", auditRecord["auth_source"])
	}
	if subjectID, ok := auditRecord["subject_id"].(string); !ok || subjectID == "" {
		t.Fatalf("expected non-empty audit subject_id, got %v", auditRecord["subject_id"])
	}
	if _, ok := auditRecord["user_id"]; ok {
		t.Fatalf("expected emitted audit record to omit user_id, got %v", auditRecord["user_id"])
	}
	if auditRecord["allowed"] != true {
		t.Fatalf("expected audit allowed=true, got %v", auditRecord["allowed"])
	}

	prov.callFn = func(_ context.Context, _ string, _ map[string]any) (*mcpgo.CallToolResult, error) {
		return &mcpgo.CallToolResult{
			IsError:           true,
			Content:           []mcpgo.Content{mcpgo.NewTextContent("query failed"), mcpgo.NewTextContent("try again")},
			StructuredContent: map[string]any{"code": "bad_query"},
		}, nil
	}

	status, resp = mcpJSONRPC(t, ts, headers, map[string]any{
		"jsonrpc": "2.0",
		"id":      4,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "clickhouse_run_query",
			"arguments": map[string]any{"sql": "SELECT broken"},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("tools/call error result: expected 200, got %d", status)
	}
	result, ok = resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("tools/call error result: expected result, got %v", resp)
	}
	if result["isError"] != true {
		t.Fatalf("expected MCP error result, got %v", result["isError"])
	}
	content, ok = result["content"].([]any)
	if !ok || len(content) != 2 {
		t.Fatalf("expected 2 content items on MCP error result, got %v", result)
	}
	firstText, ok := content[0].(map[string]any)
	if !ok || firstText["text"] != "query failed" {
		t.Fatalf("expected first MCP error block text query failed, got %v", content[0])
	}
	structured, ok := result["structuredContent"].(map[string]any)
	if !ok || structured["code"] != "bad_query" {
		t.Fatalf("expected structuredContent.code=bad_query, got %v", result["structuredContent"])
	}
}

func TestMCPEndpoint_WorkloadAuthorizationAndAudit(t *testing.T) {
	t.Parallel()

	staticCat := &catalog.Catalog{
		Name: "clickhouse",
		Operations: []catalog.CatalogOperation{
			{
				ID:          "run_query",
				Description: "Execute a SQL query",
				Transport:   catalog.TransportMCPPassthrough,
			},
			{
				ID:          "delete_table",
				Description: "Delete a table",
				Transport:   catalog.TransportMCPPassthrough,
			},
		},
	}

	var auditBuf bytes.Buffer
	var calledName string
	auditSink := invocation.NewSlogAuditSink(&auditBuf)
	prov := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "clickhouse", ConnMode: core.ConnectionModeUser},
			ops: []core.Operation{
				{Name: "run_query", Description: "Execute a SQL query"},
				{Name: "delete_table", Description: "Delete a table"},
			},
		},
		catalog: staticCat,
		callFn: func(_ context.Context, name string, _ map[string]any) (*mcpgo.CallToolResult, error) {
			calledName = name
			return mcpgo.NewToolResultText("unexpected"), nil
		},
	}

	svc := coretesting.NewStubServices(t)
	seedSubjectToken(t, svc, principal.WorkloadSubjectID("triage-bot"), "clickhouse", "default", "default", "identity-token")

	providers := testutil.NewProviderRegistry(t, prov)
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Workloads: map[string]config.WorkloadDef{
			"triage-bot": {
				Token: "gst_wld_triage-bot-token",
				Providers: map[string]config.WorkloadProviderDef{
					"clickhouse": {
						Connection: "default",
						Instance:   "default",
						Allow:      []string{"run_query"},
					},
				},
			},
		},
	}, providers, nil, nil)
	mcpHandler := newMCPHandler(t, providers, svc, auditSink, authz)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "stub"}
		cfg.Providers = providers
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.MCPHandler = mcpHandler
	})
	defer ts.Close()

	headers := map[string]string{
		"Authorization": "Bearer gst_wld_triage-bot-token",
	}

	_, _, initHeaders := mcpJSONRPCWithHeaders(t, ts, headers, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test", "version": "1.0"},
		},
	})
	if sessionID := initHeaders.Get("Mcp-Session-Id"); sessionID != "" {
		headers["Mcp-Session-Id"] = sessionID
	}

	status, resp := mcpJSONRPC(t, ts, headers, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
	})
	if status != http.StatusOK {
		t.Fatalf("tools/list: expected 200, got %d", status)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("tools/list: expected result object, got %v", resp)
	}
	tools, ok := result["tools"].([]any)
	if !ok {
		t.Fatalf("tools/list: expected tools array, got %v", result)
	}
	names := make([]string, 0, len(tools))
	for _, raw := range tools {
		names = append(names, raw.(map[string]any)["name"].(string))
	}
	if !reflect.DeepEqual(names, []string{"clickhouse_run_query"}) {
		t.Fatalf("tools/list names = %v, want %v", names, []string{"clickhouse_run_query"})
	}

	status, resp = mcpJSONRPC(t, ts, headers, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "clickhouse_delete_table",
			"arguments": map[string]any{"table": "users"},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("tools/call: expected 200, got %d", status)
	}
	result, ok = resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("tools/call: expected result object, got %v", resp)
	}
	if result["isError"] != true {
		t.Fatalf("expected MCP error result, got %v", result["isError"])
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("expected MCP error content, got %v", result)
	}
	firstText, ok := content[0].(map[string]any)
	if !ok || firstText["text"] != "operation access denied" {
		t.Fatalf("unexpected MCP error content: %v", content)
	}
	if calledName != "" {
		t.Fatalf("expected denied tool call to stop before provider CallTool, got %q", calledName)
	}

	lines := bytes.Split(bytes.TrimSpace(auditBuf.Bytes()), []byte("\n"))
	if len(lines) == 0 {
		t.Fatal("expected MCP audit record")
	}
	var auditRecord map[string]any
	if err := json.Unmarshal(lines[len(lines)-1], &auditRecord); err != nil {
		t.Fatalf("parsing MCP audit record: %v\nraw: %s", err, auditBuf.String())
	}
	if auditRecord["source"] != "mcp" {
		t.Fatalf("expected audit source mcp, got %v", auditRecord["source"])
	}
	if auditRecord["provider"] != "clickhouse" {
		t.Fatalf("expected audit provider clickhouse, got %v", auditRecord["provider"])
	}
	if auditRecord["operation"] != "delete_table" {
		t.Fatalf("expected audit operation delete_table, got %v", auditRecord["operation"])
	}
	if auditRecord["allowed"] != false {
		t.Fatalf("expected audit allowed=false, got %v", auditRecord["allowed"])
	}
	if auditRecord["auth_source"] != "workload_token" {
		t.Fatalf("expected audit auth_source workload_token, got %v", auditRecord["auth_source"])
	}
	if auditRecord["subject_id"] != "workload:triage-bot" {
		t.Fatalf("expected subject_id workload:triage-bot, got %v", auditRecord["subject_id"])
	}
	if auditRecord["subject_kind"] != "workload" {
		t.Fatalf("expected subject_kind workload, got %v", auditRecord["subject_kind"])
	}
	if auditRecord["credential_mode"] != "user" {
		t.Fatalf("expected credential_mode user, got %v", auditRecord["credential_mode"])
	}
	if auditRecord["credential_subject_id"] != "workload:triage-bot" {
		t.Fatalf("expected credential_subject_id workload:triage-bot, got %v", auditRecord["credential_subject_id"])
	}
	if auditRecord["credential_connection"] != "default" {
		t.Fatalf("expected credential_connection default, got %v", auditRecord["credential_connection"])
	}
	if auditRecord["credential_instance"] != "default" {
		t.Fatalf("expected credential_instance default, got %v", auditRecord["credential_instance"])
	}
}

func TestMCPEndpoint_NotMountedWhenDisabled(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
	})
	defer ts.Close()

	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
	})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 404/405 when MCP not enabled, got %d", resp.StatusCode)
	}
}

func TestMaxBodySize(t *testing.T) {
	t.Parallel()

	fullStub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N: "test-int",
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		},
		ops: []core.Operation{
			{Name: "do_thing", Description: "Do a thing", Method: http.MethodPost},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, fullStub)
		cfg.Services = coretesting.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	largeBody := bytes.NewReader(bytes.Repeat([]byte("A"), (1<<20)+1))
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/test-int/do_thing", largeBody)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, body)
	}
}

func TestErrorSanitization(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok1", SubjectID: principal.UserSubjectID(u.ID), Integration: "test-int",
		Connection: "", Instance: "default", AccessToken: "test-token",
	})

	sensitiveMsg := "secret-internal-db-password-leaked"
	fullStub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N: "test-int",
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return nil, fmt.Errorf("upstream broke: %s", sensitiveMsg)
			},
		},
		ops: []core.Operation{
			{Name: "do_thing", Description: "Do a thing", Method: http.MethodGet},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, fullStub)
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/test-int/do_thing", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), sensitiveMsg) {
		t.Fatalf("response body contains sensitive error details: %s", body)
	}

	var errResp map[string]string
	if err := json.Unmarshal(body, &errResp); err != nil {
		t.Fatalf("decoding error response: %v", err)
	}
	if errResp["error"] != "operation failed" {
		t.Fatalf("expected generic error message, got %q", errResp["error"])
	}

}

func TestUpstreamHTTPErrorPassthrough(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": "invalid parameter: limit",
			},
		})
	}))
	testutil.CloseOnCleanup(t, upstream)

	prov, err := provider.Build(&provider.Definition{
		Provider:         "test-int",
		DisplayName:      "Test Integration",
		BaseURL:          upstream.URL,
		ConnectionMode:   "none",
		Auth:             provider.AuthDef{Type: "manual"},
		ErrorMessagePath: "error.message",
		Operations: map[string]provider.OperationDef{
			"do_thing": {Description: "Do a thing", Method: http.MethodGet, Path: "/do_thing"},
		},
	}, config.ConnectionDef{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, prov)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/test-int/do_thing", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 400: %s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}

	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), `"operation failed"`) {
		t.Fatalf("expected upstream body, got generic error: %s", body)
	}

	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decoding upstream body: %v", err)
	}
	errObj, ok := decoded["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested error object, got %v", decoded)
	}
	if errObj["message"] != "invalid parameter: limit" {
		t.Fatalf("message = %v, want %q", errObj["message"], "invalid parameter: limit")
	}
}

func TestExecuteOperation_UserFacingErrorMessage(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok1", SubjectID: principal.UserSubjectID(u.ID), Integration: "test-int",
		Connection: "", Instance: "default", AccessToken: "test-token",
	})

	sensitiveMsg := "postgres://user:secret@example.internal/db"
	fullStub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N: "test-int",
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return nil, fmt.Errorf("%w: request failed: %s", apiexec.ErrUpstreamTimedOut, sensitiveMsg)
			},
		},
		ops: []core.Operation{
			{Name: "do_thing", Description: "Do a thing", Method: http.MethodGet},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, fullStub)
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/test-int/do_thing", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), sensitiveMsg) {
		t.Fatalf("response body contains sensitive error details: %s", body)
	}

	var errResp map[string]string
	if err := json.Unmarshal(body, &errResp); err != nil {
		t.Fatalf("decoding error response: %v", err)
	}
	if errResp["error"] != "upstream service timed out" {
		t.Fatalf("expected user-facing message, got %q", errResp["error"])
	}
}

func TestExecuteOperation_ReconnectRequiredMessage(t *testing.T) {
	t.Parallel()

	fullStub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{N: "test-int"},
		ops: []core.Operation{
			{Name: "do_thing", Description: "Do a thing", Method: http.MethodGet},
		},
	}

	invoker := &testutil.StubInvoker{
		Err: fmt.Errorf("%w: token endpoint returned 400: {\"error\":\"invalid_grant\"}", invocation.ErrReconnectRequired),
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, fullStub)
		cfg.Invoker = invoker
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/test-int/do_thing", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusPreconditionFailed {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 412: %s", resp.StatusCode, body)
	}

	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), "invalid_grant") {
		t.Fatalf("response body contains upstream refresh details: %s", body)
	}

	var errResp struct {
		Error       string `json:"error"`
		Code        string `json:"code"`
		Integration string `json:"integration"`
	}
	if err := json.Unmarshal(body, &errResp); err != nil {
		t.Fatalf("decoding error response: %v", err)
	}
	if errResp.Error != `OAuth token for integration "test-int" expired or was revoked; reconnect it` {
		t.Fatalf("expected reconnect-required message, got %q", errResp.Error)
	}
	if errResp.Code != "reconnect_required" {
		t.Fatalf("expected reconnect_required code, got %q", errResp.Code)
	}
	if errResp.Integration != "test-int" {
		t.Fatalf("expected integration test-int, got %q", errResp.Integration)
	}
}

func TestExecuteOperation_WrappedOperationErrorMessage(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok1", SubjectID: principal.UserSubjectID(u.ID), Integration: "test-int",
		Connection: "", Instance: "default", AccessToken: "test-token",
	})

	sensitiveContext := "postgres://user:secret@example.internal/db"
	publicMessage := "invalid parameter: limit"
	fullStub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N: "test-int",
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return nil, fmt.Errorf("graphql request failed against %s: %w", sensitiveContext, &apiexec.UpstreamOperationError{
					Message: publicMessage,
				})
			},
		},
		ops: []core.Operation{
			{Name: "do_thing", Description: "Do a thing", Method: http.MethodGet},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, fullStub)
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/test-int/do_thing", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), sensitiveContext) {
		t.Fatalf("response body contains sensitive error details: %s", body)
	}

	var errResp map[string]string
	if err := json.Unmarshal(body, &errResp); err != nil {
		t.Fatalf("decoding error response: %v", err)
	}
	if errResp["error"] != publicMessage {
		t.Fatalf("expected wrapped operation message, got %q", errResp["error"])
	}
}

func TestExecuteOperation_RuntimeUnavailableMessage(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok1", SubjectID: principal.UserSubjectID(u.ID), Integration: "test-int",
		Connection: "", Instance: "default", AccessToken: "test-token",
	})

	fullStub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N: "test-int",
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return nil, grpcstatus.Error(codes.Unavailable, "dial tcp 10.0.0.15: connection refused")
			},
		},
		ops: []core.Operation{
			{Name: "do_thing", Description: "Do a thing", Method: http.MethodGet},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, fullStub)
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/test-int/do_thing", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var errResp map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		t.Fatalf("decoding error response: %v", err)
	}
	if errResp["error"] != "integration unavailable" {
		t.Fatalf("expected integration unavailable message, got %q", errResp["error"])
	}
}

type stubAuthWithToken struct {
	coretesting.StubAuthProvider
}

func (s *stubAuthWithToken) IssueSessionToken(identity *core.UserIdentity) (string, error) {
	return "dev-token-" + identity.Email, nil
}

func (s *stubAuthWithToken) SessionTokenTTL() time.Duration {
	return time.Hour
}

type stubHostIssuedSessionAuth struct {
	secret      []byte
	name        string
	loginHost   string
	email       string
	displayName string
}

func (s *stubHostIssuedSessionAuth) Name() string {
	if s.name != "" {
		return s.name
	}
	return "host-issued"
}

func (s *stubHostIssuedSessionAuth) LoginURL(state string) (string, error) {
	host := s.loginHost
	if host == "" {
		host = "idp.example.test"
	}
	return "https://" + host + "/login?state=" + url.QueryEscape(state), nil
}

func (s *stubHostIssuedSessionAuth) HandleCallback(_ context.Context, _ string) (*core.UserIdentity, error) {
	return nil, fmt.Errorf("use HandleCallbackWithState")
}

func (s *stubHostIssuedSessionAuth) HandleCallbackWithState(_ context.Context, code, state string) (*core.UserIdentity, string, error) {
	if code != "good-code" {
		return nil, "", fmt.Errorf("unexpected code %q", code)
	}
	email := s.email
	if email == "" {
		email = "host@example.com"
	}
	displayName := s.displayName
	if displayName == "" {
		displayName = "Host Issued"
	}
	return &core.UserIdentity{Email: email, DisplayName: displayName}, state, nil
}

func (s *stubHostIssuedSessionAuth) ValidateToken(_ context.Context, token string) (*core.UserIdentity, error) {
	return session.ValidateToken(token, s.secret)
}

func (s *stubHostIssuedSessionAuth) SessionTokenTTL() time.Duration {
	return time.Hour
}

func TestCookieAuth(t *testing.T) {
	t.Parallel()

	stub := &coretesting.StubAuthProvider{
		N: "test",
		ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
			switch token {
			case "valid-cookie-token":
				return &core.UserIdentity{Email: "cookie@test.local"}, nil
			case "valid-header-token":
				return &core.UserIdentity{Email: "header@test.local"}, nil
			default:
				return nil, fmt.Errorf("invalid token")
			}
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = stub
	})
	testutil.CloseOnCleanup(t, ts)

	// Request without cookie should be rejected.
	reqNoCookie, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	noAuthResp, err := http.DefaultClient.Do(reqNoCookie)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = noAuthResp.Body.Close() }()
	if noAuthResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without cookie, got %d", noAuthResp.StatusCode)
	}

	// Request with cookie should pass auth middleware.
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "valid-cookie-token"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		t.Fatal("cookie auth should have passed middleware, got 401")
	}

	// An invalid cookie should still fall back to a valid Authorization header.
	reqWithFallback, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	reqWithFallback.AddCookie(&http.Cookie{Name: "session_token", Value: "invalid-cookie-token"})
	reqWithFallback.Header.Set("Authorization", "Bearer valid-header-token")
	fallbackResp, err := http.DefaultClient.Do(reqWithFallback)
	if err != nil {
		t.Fatalf("request with header fallback: %v", err)
	}
	defer func() { _ = fallbackResp.Body.Close() }()

	if fallbackResp.StatusCode == http.StatusUnauthorized {
		t.Fatal("valid Authorization header should have passed middleware after invalid cookie")
	}
}

func TestLoginCallback_HostIssuesSessionWhenProviderDoesNot(t *testing.T) {
	t.Parallel()

	secret := []byte("0123456789abcdef0123456789abcdef")
	auth := &stubHostIssuedSessionAuth{secret: secret}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{Jar: jar}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = auth
		cfg.StateSecret = secret
		cfg.Services = coretesting.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	startBody := bytes.NewBufferString(`{"state":"test-state"}`)
	startReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/login", startBody)
	startReq.Header.Set("Content-Type", "application/json")
	startResp, err := client.Do(startReq)
	if err != nil {
		t.Fatalf("start request: %v", err)
	}
	_ = startResp.Body.Close()
	if startResp.StatusCode != http.StatusOK {
		t.Fatalf("start status = %d, want %d", startResp.StatusCode, http.StatusOK)
	}

	callbackResp, err := client.Get(ts.URL + "/api/v1/auth/login/callback?code=good-code&state=test-state")
	if err != nil {
		t.Fatalf("callback request: %v", err)
	}
	defer func() { _ = callbackResp.Body.Close() }()
	if callbackResp.StatusCode != http.StatusOK {
		t.Fatalf("callback status = %d, want %d", callbackResp.StatusCode, http.StatusOK)
	}

	foundSession := false
	for _, cookie := range jar.Cookies(callbackResp.Request.URL) {
		if cookie.Name == "session_token" && cookie.Value != "" {
			foundSession = true
		}
	}
	if !foundSession {
		t.Fatal("expected session_token cookie to be issued by host")
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("integrations request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusUnauthorized {
		t.Fatal("host-issued session cookie should authenticate subsequent requests")
	}
}

func TestBrowserLoginRedirect_RedirectsBackToNextPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		next              string
		publicBaseURL     string
		managementBaseURL string
		enableAdminAuth   bool
		routeProfile      server.RouteProfile
		wantStartStatus   int
		wantState         string
		wantRedirect      string
	}{
		{
			name:            "relative next path",
			next:            "/sample-portal/admin?tab=members",
			wantStartStatus: http.StatusFound,
			wantState:       "/sample-portal/admin",
			wantRedirect:    "/sample-portal/admin?tab=members",
		},
		{
			name:              "trusted absolute management next path",
			next:              "https://gestalt.example.test:9090/admin?tab=members",
			publicBaseURL:     "https://gestalt.example.test",
			managementBaseURL: "https://gestalt.example.test:9090",
			enableAdminAuth:   true,
			routeProfile:      server.RouteProfilePublic,
			wantStartStatus:   http.StatusFound,
			wantState:         "/admin",
			wantRedirect:      "https://gestalt.example.test:9090/admin?tab=members",
		},
		{
			name:              "rejects absolute management next path when admin auth is disabled",
			next:              "https://gestalt.example.test:9090/admin?tab=members",
			publicBaseURL:     "https://gestalt.example.test",
			managementBaseURL: "https://gestalt.example.test:9090",
			routeProfile:      server.RouteProfilePublic,
			wantStartStatus:   http.StatusBadRequest,
		},
		{
			name:              "rejects untrusted absolute next path",
			next:              "https://evil.example.test/admin?tab=members",
			publicBaseURL:     "https://gestalt.example.test",
			managementBaseURL: "https://gestalt.example.test:9090",
			enableAdminAuth:   true,
			routeProfile:      server.RouteProfilePublic,
			wantStartStatus:   http.StatusBadRequest,
		},
		{
			name:              "rejects absolute management next path outside admin",
			next:              "https://gestalt.example.test:9090/metrics",
			publicBaseURL:     "https://gestalt.example.test",
			managementBaseURL: "https://gestalt.example.test:9090",
			enableAdminAuth:   true,
			routeProfile:      server.RouteProfilePublic,
			wantStartStatus:   http.StatusBadRequest,
		},
		{
			name:              "rejects absolute management next path with admin traversal",
			next:              "https://gestalt.example.test:9090/admin/%2e%2e/metrics",
			publicBaseURL:     "https://gestalt.example.test",
			managementBaseURL: "https://gestalt.example.test:9090",
			enableAdminAuth:   true,
			routeProfile:      server.RouteProfilePublic,
			wantStartStatus:   http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			secret := []byte("0123456789abcdef0123456789abcdef")
			auth := &stubHostIssuedSessionAuth{secret: secret}
			jar, err := cookiejar.New(nil)
			if err != nil {
				t.Fatalf("cookiejar.New: %v", err)
			}
			client := &http.Client{
				Jar: jar,
				CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
					return http.ErrUseLastResponse
				},
			}

			ts := newTestServer(t, func(cfg *server.Config) {
				cfg.Auth = auth
				cfg.StateSecret = secret
				cfg.Services = coretesting.NewStubServices(t)
				cfg.PublicBaseURL = tc.publicBaseURL
				cfg.ManagementBaseURL = tc.managementBaseURL
				cfg.RouteProfile = tc.routeProfile
				if tc.enableAdminAuth {
					cfg.Admin = server.AdminRouteConfig{AuthorizationPolicy: "admin_policy"}
				}
			})
			testutil.CloseOnCleanup(t, ts)

			startResp, err := client.Get(ts.URL + "/api/v1/auth/login?next=" + url.QueryEscape(tc.next))
			if err != nil {
				t.Fatalf("start browser login: %v", err)
			}
			defer func() { _ = startResp.Body.Close() }()
			if startResp.StatusCode != tc.wantStartStatus {
				t.Fatalf("start status = %d, want %d", startResp.StatusCode, tc.wantStartStatus)
			}
			if tc.wantStartStatus != http.StatusFound {
				body, readErr := io.ReadAll(startResp.Body)
				if readErr != nil {
					t.Fatalf("ReadAll start error body: %v", readErr)
				}
				if !strings.Contains(string(body), "invalid next path") {
					t.Fatalf("start error body = %q, want %q", body, "invalid next path")
				}
				return
			}
			loginURL, err := url.Parse(startResp.Header.Get("Location"))
			if err != nil {
				t.Fatalf("parse start login redirect: %v", err)
			}
			if got := loginURL.Host; got != "idp.example.test" {
				t.Fatalf("login redirect host = %q, want %q", got, "idp.example.test")
			}
			if got := loginURL.Query().Get("state"); got != tc.wantState {
				t.Fatalf("login redirect state = %q, want %q", got, tc.wantState)
			}

			callbackResp, err := client.Get(ts.URL + "/api/v1/auth/login/callback?code=good-code&state=" + url.QueryEscape(loginURL.Query().Get("state")))
			if err != nil {
				t.Fatalf("browser login callback: %v", err)
			}
			defer func() { _ = callbackResp.Body.Close() }()
			if callbackResp.StatusCode != http.StatusFound {
				t.Fatalf("callback status = %d, want %d", callbackResp.StatusCode, http.StatusFound)
			}
			if got := callbackResp.Header.Get("Location"); got != tc.wantRedirect {
				t.Fatalf("callback redirect = %q, want %q", got, tc.wantRedirect)
			}

			foundSession := false
			for _, cookie := range jar.Cookies(callbackResp.Request.URL) {
				if cookie.Name == "session_token" && cookie.Value != "" {
					foundSession = true
				}
			}
			if !foundSession {
				t.Fatal("expected session cookie after browser login callback")
			}
		})
	}
}

func TestLogout(t *testing.T) {
	t.Parallel()

	var auditBuf bytes.Buffer
	auditSink := invocation.NewSlogAuditSink(&auditBuf)
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "session-token" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: "user@example.com"}, nil
			},
		}
		cfg.Services = coretesting.NewStubServices(t)
		cfg.AuditSink = auditSink
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "session-token"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var found bool
	for _, c := range resp.Cookies() {
		if c.Name == "session_token" {
			found = true
			if c.MaxAge != -1 {
				t.Fatalf("expected MaxAge -1, got %d", c.MaxAge)
			}
		}
	}
	if !found {
		t.Fatal("expected session_token cookie to be cleared")
	}

	var auditRecord map[string]any
	if err := json.Unmarshal(auditBuf.Bytes(), &auditRecord); err != nil {
		t.Fatalf("parsing audit record: %v\nraw: %s", err, auditBuf.String())
	}
	if auditRecord["operation"] != "auth.logout" {
		t.Fatalf("expected audit operation auth.logout, got %v", auditRecord["operation"])
	}
	if auditRecord["source"] != "http" {
		t.Fatalf("expected audit source http, got %v", auditRecord["source"])
	}
	if auditRecord["auth_source"] != "session" {
		t.Fatalf("expected audit auth_source session, got %v", auditRecord["auth_source"])
	}
	if subjectID, ok := auditRecord["subject_id"].(string); !ok || subjectID == "" {
		t.Fatalf("expected non-empty audit subject_id, got %v", auditRecord["subject_id"])
	}
	if _, ok := auditRecord["user_id"]; ok {
		t.Fatalf("expected emitted audit record to omit user_id, got %v", auditRecord["user_id"])
	}
	if auditRecord["allowed"] != true {
		t.Fatalf("expected audit allowed=true, got %v", auditRecord["allowed"])
	}
}

func TestLogout_NoAuthNilProvider(t *testing.T) {
	t.Parallel()

	var auditBuf bytes.Buffer
	auditSink := invocation.NewSlogAuditSink(&auditBuf)
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = nil
		cfg.AuditSink = auditSink
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/logout", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var auditRecord map[string]any
	if err := json.Unmarshal(auditBuf.Bytes(), &auditRecord); err != nil {
		t.Fatalf("parsing audit record: %v\nraw: %s", err, auditBuf.String())
	}
	if auditRecord["operation"] != "auth.logout" {
		t.Fatalf("expected audit operation auth.logout, got %v", auditRecord["operation"])
	}
	if auditRecord["provider"] != "none" {
		t.Fatalf("expected audit provider none, got %v", auditRecord["provider"])
	}
	if auditRecord["allowed"] != true {
		t.Fatalf("expected audit allowed=true, got %v", auditRecord["allowed"])
	}
}

func TestExecuteOperation_ConnectionModeUserDoesNotFallbackToIdentity(t *testing.T) {
	t.Parallel()

	stub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "svc",
			ConnMode: core.ConnectionModeUser,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: fmt.Sprintf(`{"token":%q}`, token)}, nil
			},
		},
		ops: []core.Operation{{Name: "do", Method: http.MethodGet}},
	}

	t.Run("prefers user token", func(t *testing.T) {
		t.Parallel()

		svc := coretesting.NewStubServices(t)
		apiToken, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
		if err != nil {
			t.Fatalf("GenerateToken: %v", err)
		}
		seedAPIToken(t, svc, apiToken, hashed, "api-user")
		u, _ := svc.Users.FindOrCreateUser(context.Background(), "api-user@test.local")
		seedToken(t, svc, &core.IntegrationToken{
			ID: "tok-user", SubjectID: principal.UserSubjectID(u.ID), Integration: "svc",
			Connection: "", Instance: "default", AccessToken: "user-tok",
		})

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
			cfg.Providers = testutil.NewProviderRegistry(t, stub)
			cfg.Services = svc
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/svc/do", nil)
		req.Header.Set("Authorization", "Bearer "+apiToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decoding: %v", err)
		}
		if result["token"] != "user-tok" {
			t.Fatalf("expected user-tok (preferred), got %v", result["token"])
		}
	})

	t.Run("no longer falls back to shared identity", func(t *testing.T) {
		t.Parallel()

		svc := coretesting.NewStubServices(t)
		apiToken, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
		if err != nil {
			t.Fatalf("GenerateToken: %v", err)
		}
		seedAPIToken(t, svc, apiToken, hashed, "api-user")
		seedToken(t, svc, &core.IntegrationToken{
			ID: "tok-identity", SubjectID: "identity:__identity__", Integration: "svc",
			Connection: "", Instance: "default", AccessToken: "identity-tok",
		})

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
			cfg.Providers = testutil.NewProviderRegistry(t, stub)
			cfg.Services = svc
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/svc/do", nil)
		req.Header.Set("Authorization", "Bearer "+apiToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusPreconditionFailed {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 412, got %d: %s", resp.StatusCode, body)
		}

		var errResp struct {
			Error       string `json:"error"`
			Code        string `json:"code"`
			Integration string `json:"integration"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
			t.Fatalf("decoding error response: %v", err)
		}
		if errResp.Code != "not_connected" {
			t.Fatalf("expected not_connected code, got %q", errResp.Code)
		}
		if errResp.Error != `no token stored for integration "svc"; connect via OAuth first` {
			t.Fatalf("unexpected error message: %q", errResp.Error)
		}
		if errResp.Integration != "svc" {
			t.Fatalf("expected integration svc, got %q", errResp.Integration)
		}
	})
}

func TestConnectManual_MultiCredential(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		integration   string
		requestBody   string
		provider      func() core.Provider
		pluginDefs    map[string]*config.ProviderEntry
		wantTokenData map[string]string
	}{
		{
			name:        "stores named credentials map",
			integration: "multi-key-svc",
			requestBody: `{"integration":"multi-key-svc","credentials":{"api_key":"k1","app_key":"k2"}}`,
			provider: func() core.Provider {
				return &stubManualProvider{
					StubIntegration: coretesting.StubIntegration{N: "multi-key-svc"},
				}
			},
			wantTokenData: map[string]string{
				"api_key": "k1",
				"app_key": "k2",
			},
		},
		{
			name:        "single credential input wraps structured auth mapping field",
			integration: "modern-treasury",
			requestBody: `{"integration":"modern-treasury","credential":"api-key-abc"}`,
			provider: func() core.Provider {
				return &stubManualProvider{
					StubIntegration: coretesting.StubIntegration{N: "modern-treasury"},
				}
			},
			pluginDefs: map[string]*config.ProviderEntry{
				"modern-treasury": {
					Auth: &config.ConnectionAuthDef{
						Type: providermanifestv1.AuthTypeManual,
						Credentials: []config.CredentialFieldDef{
							{Name: "api_key", Label: "API Key"},
						},
						AuthMapping: &config.AuthMappingDef{
							Basic: &config.BasicAuthMappingDef{
								Username: config.AuthValueDef{
									Value: "org-123",
								},
								Password: config.AuthValueDef{
									ValueFrom: &config.AuthValueFromDef{
										CredentialFieldRef: &config.CredentialFieldRefDef{Name: "api_key"},
									},
								},
							},
						},
					},
				},
			},
			wantTokenData: map[string]string{
				"api_key": "api-key-abc",
			},
		},
		{
			name:        "explicit manual connection auth does not require provider manual interface",
			integration: "clickhouse-manual",
			requestBody: `{"integration":"clickhouse-manual","credentials":{"api_key":"api-key-abc"}}`,
			provider: func() core.Provider {
				return &stubNonOAuthProvider{name: "clickhouse-manual"}
			},
			pluginDefs: map[string]*config.ProviderEntry{
				"clickhouse-manual": {
					Auth: &config.ConnectionAuthDef{
						Type: providermanifestv1.AuthTypeManual,
						Credentials: []config.CredentialFieldDef{
							{Name: "api_key", Label: "API Key"},
						},
					},
				},
			},
			wantTokenData: map[string]string{
				"api_key": "api-key-abc",
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := coretesting.NewStubServices(t)
			ts := newTestServer(t, func(cfg *server.Config) {
				cfg.Providers = testutil.NewProviderRegistry(t, tc.provider())
				cfg.DefaultConnection = map[string]string{tc.integration: config.PluginConnectionName}
				cfg.PluginDefs = tc.pluginDefs
				cfg.Services = svc
			})
			testutil.CloseOnCleanup(t, ts)

			req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/connect-manual", bytes.NewBufferString(tc.requestBody))
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected 200, got %d", resp.StatusCode)
			}

			u, _ := svc.Users.FindOrCreateUser(context.Background(), "anonymous@gestalt")
			tokens, _ := svc.Tokens.ListTokens(context.Background(), principal.UserSubjectID(u.ID))
			if len(tokens) == 0 {
				t.Fatal("expected StoreToken to be called")
			}
			stored := tokens[0]

			var tokenData map[string]string
			if err := json.Unmarshal([]byte(stored.AccessToken), &tokenData); err != nil {
				t.Fatalf("stored token is not valid JSON: %v", err)
			}
			if !reflect.DeepEqual(tokenData, tc.wantTokenData) {
				t.Fatalf("token data = %+v, want %+v", tokenData, tc.wantTokenData)
			}
		})
	}
}

func TestAPITokenScopes_EnforcedDuringInvocation(t *testing.T) {
	t.Parallel()

	alphaStub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "alpha",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		},
		ops: []core.Operation{{Name: "do_thing", Method: http.MethodGet}},
	}
	betaStub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "beta",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		},
		ops: []core.Operation{{Name: "do_thing", Method: http.MethodGet}},
	}

	svc := coretesting.NewStubServices(t)
	plaintext, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	ctx := context.Background()
	u, _ := svc.Users.FindOrCreateUser(ctx, "scoped@test.local")
	exp := time.Now().Add(24 * time.Hour)
	_ = svc.APITokens.StoreAPIToken(ctx, &core.APIToken{
		ID: "api-tok-scoped", OwnerKind: core.APITokenOwnerKindUser, OwnerID: u.ID, CredentialSubjectID: principal.UserSubjectID(u.ID), Name: "scoped-token",
		HashedToken: hashed, Scopes: "alpha", ExpiresAt: &exp,
	})

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, _ string) (*core.UserIdentity, error) {
				return nil, fmt.Errorf("not a session token")
			},
		}
		cfg.Providers = testutil.NewProviderRegistry(t, alphaStub, betaStub)
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	t.Run("allowed provider succeeds", func(t *testing.T) {
		t.Parallel()
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/alpha/do_thing", nil)
		req.Header.Set("Authorization", "Bearer "+plaintext)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("denied provider returns 403", func(t *testing.T) {
		t.Parallel()
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/beta/do_thing", nil)
		req.Header.Set("Authorization", "Bearer "+plaintext)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", resp.StatusCode)
		}
	})
}

func TestAPITokenScopes_EmptyScopesAllowAll(t *testing.T) {
	t.Parallel()

	stub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "any-provider",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		},
		ops: []core.Operation{{Name: "do_thing", Method: http.MethodGet}},
	}

	svc := coretesting.NewStubServices(t)
	plaintext, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	seedAPIToken(t, svc, plaintext, hashed, "unscoped")

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, _ string) (*core.UserIdentity, error) {
				return nil, fmt.Errorf("not a session token")
			},
		}
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/any-provider/do_thing", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestCreateAPIToken_InvalidScope(t *testing.T) {
	t.Parallel()

	stub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{N: "real-provider"},
		ops:             []core.Operation{{Name: "op", Method: http.MethodGet}},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Services = coretesting.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"name":"test-token","scopes":"nonexistent"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/tokens", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestServerAllowsProviderNamedIdentities(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	seedUser(t, svc, "admin@example.test")

	providers := testutil.NewProviderRegistry(t, &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "identities",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(_ context.Context, operation string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: fmt.Sprintf(`{"operation":%q}`, operation)}, nil
			},
		},
		ops: []core.Operation{
			{Name: "read", Method: http.MethodGet},
			{Name: "write", Method: http.MethodPost},
		},
	})

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "admin-session" {
					return nil, fmt.Errorf("unknown session %q", token)
				}
				return &core.UserIdentity{Email: "admin@example.test"}, nil
			},
		}
		cfg.Services = svc
		cfg.Providers = providers
	})
	testutil.CloseOnCleanup(t, ts)

	readReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/identities/read", nil)
	readReq.AddCookie(&http.Cookie{Name: "session_token", Value: "admin-session"})
	readResp, err := http.DefaultClient.Do(readReq)
	if err != nil {
		t.Fatalf("GET identities/read: %v", err)
	}
	defer func() { _ = readResp.Body.Close() }()
	if readResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(readResp.Body)
		t.Fatalf("GET identities/read status = %d, want %d: %s", readResp.StatusCode, http.StatusOK, body)
	}

	writeReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/identities/write", bytes.NewBufferString(`{}`))
	writeReq.Header.Set("Content-Type", "application/json")
	writeReq.AddCookie(&http.Cookie{Name: "session_token", Value: "admin-session"})
	writeResp, err := http.DefaultClient.Do(writeReq)
	if err != nil {
		t.Fatalf("POST identities/write: %v", err)
	}
	defer func() { _ = writeResp.Body.Close() }()
	if writeResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(writeResp.Body)
		t.Fatalf("POST identities/write status = %d, want %d: %s", writeResp.StatusCode, http.StatusOK, body)
	}

	missingReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/identities/missing", nil)
	missingReq.AddCookie(&http.Cookie{Name: "session_token", Value: "admin-session"})
	missingResp, err := http.DefaultClient.Do(missingReq)
	if err != nil {
		t.Fatalf("GET identities/missing: %v", err)
	}
	defer func() { _ = missingResp.Body.Close() }()
	if missingResp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(missingResp.Body)
		t.Fatalf("GET identities/missing status = %d, want %d: %s", missingResp.StatusCode, http.StatusNotFound, body)
	}
}

func TestLegacyManagedIdentityListCompatibility(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	admin := seedUser(t, svc, "admin@example.test")
	ctx := context.Background()
	if _, err := svc.Identities.UpsertIdentity(ctx, &core.Identity{
		ID:           "service-account-identity",
		Status:       "active",
		DisplayName:  "Service Account Identity",
		MetadataJSON: `{"label":"service_account"}`,
	}); err != nil {
		t.Fatalf("UpsertIdentity: %v", err)
	}
	if _, err := svc.IdentityManagementGrants.UpsertGrant(ctx, &core.IdentityManagementGrant{
		ManagerIdentityID: admin.ID,
		TargetIdentityID:  "service-account-identity",
		Role:              core.IdentityManagementRoleEditor,
	}); err != nil {
		t.Fatalf("UpsertGrant: %v", err)
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "admin-session" {
					return nil, fmt.Errorf("unknown session %q", token)
				}
				return &core.UserIdentity{Email: "admin@example.test"}, nil
			},
		}
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	var got []struct {
		ID          string    `json:"id"`
		DisplayName string    `json:"displayName"`
		Role        string    `json:"role"`
		CreatedAt   time.Time `json:"createdAt"`
		UpdatedAt   time.Time `json:"updatedAt"`
	}
	doJSONRequestAndDecode(t, http.MethodGet, ts.URL+"/api/v1/identities", "admin-session", "", http.StatusOK, &got)
	if len(got) != 1 {
		t.Fatalf("len(identities) = %d, want 1: %#v", len(got), got)
	}
	if got[0].ID != "service-account-identity" || got[0].DisplayName != "Service Account Identity" || got[0].Role != core.IdentityManagementRoleEditor {
		t.Fatalf("identity = %#v", got[0])
	}
}

func TestLegacyManagedIdentityMutationsReturnJSONGone(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	seedUser(t, svc, "admin@example.test")

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "admin-session" {
					return nil, fmt.Errorf("unknown session %q", token)
				}
				return &core.UserIdentity{Email: "admin@example.test"}, nil
			},
		}
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodPost, path: "/api/v1/identities", body: `{"displayName":"Legacy"}`},
		{method: http.MethodGet, path: "/api/v1/identities/legacy-id/members"},
		{method: http.MethodPost, path: "/api/v1/identities/legacy-id/tokens", body: `{}`},
		{method: http.MethodPost, path: "/api/v1/identities/legacy-id/auth/start-oauth", body: `{}`},
	} {
		req, _ := http.NewRequest(tc.method, ts.URL+tc.path, bytes.NewBufferString(tc.body))
		if tc.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		req.AddCookie(&http.Cookie{Name: "session_token", Value: "admin-session"})
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", tc.method, tc.path, err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusGone {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("%s %s status = %d, want %d: %s", tc.method, tc.path, resp.StatusCode, http.StatusGone, body)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Fatalf("%s %s Content-Type = %q, want application/json", tc.method, tc.path, ct)
		}
		var payload struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatalf("%s %s decode: %v", tc.method, tc.path, err)
		}
		if payload.Code != "managed_identities_removed" || !strings.Contains(payload.Error, "managed identities have been removed") {
			t.Fatalf("%s %s payload = %#v", tc.method, tc.path, payload)
		}
	}
}

func doJSONRequestAndDecode(t *testing.T, method, url, sessionToken, body string, wantStatus int, dst any) {
	t.Helper()

	var reqBody io.Reader
	if body != "" {
		reqBody = bytes.NewBufferString(body)
	}
	req, _ := http.NewRequest(method, url, reqBody)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if sessionToken != "" {
		req.AddCookie(&http.Cookie{Name: "session_token", Value: sessionToken})
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != wantStatus {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("%s %s status = %d, want %d: %s", method, url, resp.StatusCode, wantStatus, strings.TrimSpace(string(payload)))
	}
	if dst == nil {
		return
	}
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		t.Fatalf("%s %s decode: %v", method, url, err)
	}
}
