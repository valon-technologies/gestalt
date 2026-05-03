package server_test

import (
	"bufio"
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
	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"github.com/valon-technologies/gestalt/internal/indexeddbcodec"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	s3store "github.com/valon-technologies/gestalt/server/core/s3"
	"github.com/valon-technologies/gestalt/server/core/session"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	agentservice "github.com/valon-technologies/gestalt/server/services/agents"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/authorization"
	"github.com/valon-technologies/gestalt/server/services/egressproxy"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	indexeddbservice "github.com/valon-technologies/gestalt/server/services/indexeddb"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	plugininvokerservice "github.com/valon-technologies/gestalt/server/services/plugininvoker"
	pluginservice "github.com/valon-technologies/gestalt/server/services/plugins"
	"github.com/valon-technologies/gestalt/server/services/plugins/apiexec"
	"github.com/valon-technologies/gestalt/server/services/plugins/composite"
	"github.com/valon-technologies/gestalt/server/services/plugins/declarative"
	gestaltmcp "github.com/valon-technologies/gestalt/server/services/plugins/mcp"
	"github.com/valon-technologies/gestalt/server/services/plugins/oauth"
	"github.com/valon-technologies/gestalt/server/services/plugins/paraminterp"
	"github.com/valon-technologies/gestalt/server/services/plugins/registry"
	"github.com/valon-technologies/gestalt/server/services/providerdev"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/pluginruntime"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/runtimelogs"
	"github.com/valon-technologies/gestalt/server/services/s3"
	"github.com/valon-technologies/gestalt/server/services/ui"
	"github.com/valon-technologies/gestalt/server/services/ui/adminui"
	workflowservice "github.com/valon-technologies/gestalt/server/services/workflows"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	grpcstatus "google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
)

func configPluginInvocationDependencies(deps []invocation.PluginInvocationDependency) []config.PluginInvocationDependency {
	if len(deps) == 0 {
		return nil
	}
	out := make([]config.PluginInvocationDependency, 0, len(deps))
	for _, dep := range deps {
		out = append(out, config.PluginInvocationDependency{
			Plugin:         dep.Plugin,
			Operation:      dep.Operation,
			Surface:        dep.Surface,
			CredentialMode: providermanifestv1.ConnectionMode(dep.CredentialMode),
		})
	}
	return out
}

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
		Services: testutil.NewStubServices(t),
		Providers: func() *registry.ProviderMap[core.Provider] {
			reg := registry.New()
			return &reg.Providers
		}(),
		StateSecret: []byte("0123456789abcdef0123456789abcdef"),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	installTestExternalCredentialResolver(&cfg)
	brokerOpts := []invocation.BrokerOption{}
	if cfg.DefaultConnection != nil {
		brokerOpts = append(brokerOpts, invocation.WithConnectionMapper(invocation.ConnectionMap(cfg.DefaultConnection)))
	}
	if cfg.CatalogConnection != nil {
		brokerOpts = append(brokerOpts,
			invocation.WithMCPConnectionMapper(invocation.ConnectionMap(cfg.CatalogConnection)),
		)
	}
	if cfg.Authorizer != nil {
		brokerOpts = append(brokerOpts, invocation.WithAuthorizer(cfg.Authorizer))
	}
	if cfg.Invoker == nil {
		externalCredentials := cfg.Services.ExternalCredentials
		cfg.Invoker = invocation.NewBroker(cfg.Providers, cfg.Services.Users, externalCredentials, brokerOpts...)
	}
	srv, err := server.New(cfg)
	if err != nil {
		t.Fatalf("creating server: %v", err)
	}
	return srv
}

func installTestExternalCredentialResolver(cfg *server.Config) {
	if cfg == nil || cfg.Services == nil {
		return
	}
	provider := cfg.Services.ExternalCredentials
	if recording, ok := provider.(*recordingExternalCredentialProvider); ok {
		provider = recording.inner
	}
	stub, ok := provider.(*coretesting.StubExternalCredentialProvider)
	if !ok || stub == nil {
		return
	}
	if stub.ResolveCredentialFunc == nil {
		stub.ResolveCredentialFunc = func(ctx context.Context, req *core.ResolveExternalCredentialRequest) (*core.ResolveExternalCredentialResponse, error) {
			credential, err := resolveStoredTestCredential(ctx, stub, req)
			if err != nil {
				return nil, err
			}
			if credential.RefreshToken != "" && credential.ExpiresAt != nil && time.Until(*credential.ExpiresAt) <= 5*time.Minute {
				if resp, ok, refreshErr := refreshTestCredential(ctx, cfg, req, credential); refreshErr != nil {
					fresh, fetchErr := stub.GetCredential(ctx, credential.SubjectID, credential.ConnectionID, credential.Instance)
					if fetchErr == nil && fresh != nil && fresh.AccessToken != credential.AccessToken {
						return &core.ResolveExternalCredentialResponse{Token: fresh.AccessToken, ExpiresAt: fresh.ExpiresAt, MetadataJSON: fresh.MetadataJSON, Credential: fresh}, nil
					}
					if time.Now().Before(*credential.ExpiresAt) {
						return &core.ResolveExternalCredentialResponse{Token: credential.AccessToken, ExpiresAt: credential.ExpiresAt, MetadataJSON: credential.MetadataJSON, Credential: credential}, nil
					}
					return nil, fmt.Errorf("%w: token expired and refresh failed: %v", core.ErrReconnectRequired, refreshErr)
				} else if ok {
					now := time.Now().UTC()
					credential.AccessToken = resp.AccessToken
					if resp.RefreshToken != "" {
						credential.RefreshToken = resp.RefreshToken
					}
					if resp.ExpiresIn > 0 {
						expiresAt := now.Add(time.Duration(resp.ExpiresIn) * time.Second)
						credential.ExpiresAt = &expiresAt
					} else {
						credential.ExpiresAt = nil
					}
					credential.LastRefreshedAt = &now
					credential.RefreshErrorCount = 0
					credential.UpdatedAt = now
					if err := stub.PutCredential(ctx, credential); err != nil {
						return nil, err
					}
				}
			}
			return &core.ResolveExternalCredentialResponse{
				Token:        credential.AccessToken,
				ExpiresAt:    credential.ExpiresAt,
				MetadataJSON: credential.MetadataJSON,
				Credential:   credential,
			}, nil
		}
	}
	if stub.ExchangeCredentialFunc == nil {
		stub.ExchangeCredentialFunc = func(ctx context.Context, req *core.ExchangeExternalCredentialRequest) (*core.ExchangeExternalCredentialResponse, error) {
			if req == nil || strings.TrimSpace(req.Auth.TokenURL) == "" {
				return &core.ExchangeExternalCredentialResponse{}, nil
			}
			tokenExchange, err := oauth.ParseTokenExchangeFormat(req.Auth.TokenExchange)
			if err != nil {
				return nil, err
			}
			exchanger := oauth.NewCredentialExchanger(oauth.CredentialExchangeConfig{
				TokenURL:        req.Auth.TokenURL,
				TokenParams:     req.Auth.TokenParams,
				TokenExchange:   tokenExchange,
				AcceptHeader:    req.Auth.AcceptHeader,
				AccessTokenPath: req.Auth.AccessTokenPath,
			})
			tokenURL := exchanger.TokenURL()
			if len(req.ConnectionParams) > 0 {
				tokenURL = paraminterp.Interpolate(tokenURL, req.ConnectionParams)
			}
			resp, err := exchanger.ExchangeCredentialsWithURL(ctx, req.CredentialJSON, tokenURL)
			if err != nil {
				return nil, err
			}
			return &core.ExchangeExternalCredentialResponse{TokenResponse: &core.ExternalCredentialTokenResponse{
				AccessToken:   resp.AccessToken,
				RefreshToken:  resp.RefreshToken,
				RefreshSource: req.CredentialJSON,
				ExpiresIn:     resp.ExpiresIn,
				TokenType:     resp.TokenType,
				Extra:         resp.Extra,
			}}, nil
		}
	}
}

func resolveStoredTestCredential(ctx context.Context, stub *coretesting.StubExternalCredentialProvider, req *core.ResolveExternalCredentialRequest) (*core.ExternalCredential, error) {
	if req == nil {
		return nil, core.ErrNotFound
	}
	if req.Mode == core.ConnectionModePlatform {
		return &core.ExternalCredential{AccessToken: req.Auth.Token}, nil
	}
	if req.Instance != "" {
		return stub.GetCredential(ctx, req.CredentialSubjectID, req.ConnectionID, req.Instance)
	}
	credentials, err := stub.ListCredentialsForConnection(ctx, req.CredentialSubjectID, req.ConnectionID)
	if err != nil {
		return nil, err
	}
	switch len(credentials) {
	case 0:
		return nil, core.ErrNotFound
	case 1:
		return credentials[0], nil
	default:
		return nil, core.ErrAmbiguousCredential
	}
}

func refreshTestCredential(ctx context.Context, cfg *server.Config, req *core.ResolveExternalCredentialRequest, credential *core.ExternalCredential) (*core.TokenResponse, bool, error) {
	if cfg == nil || req == nil || credential == nil {
		return nil, false, nil
	}
	if cfg.ConnectionAuth != nil {
		if connMap := cfg.ConnectionAuth()[req.Provider]; connMap != nil {
			if refresher := connMap[req.Connection]; refresher != nil {
				tokenURL := refresher.TokenURL()
				if credential.MetadataJSON != "" {
					var params map[string]string
					if err := json.Unmarshal([]byte(credential.MetadataJSON), &params); err == nil && len(params) > 0 {
						tokenURL = paraminterp.Interpolate(tokenURL, params)
					}
				}
				startedAt := time.Now()
				connectionMode := metricutil.NormalizeConnectionMode(req.Mode)
				if tokenURL != refresher.TokenURL() {
					resp, err := refresher.RefreshTokenWithURL(ctx, credential.RefreshToken, tokenURL)
					metricutil.RecordConnectionAuthMetrics(ctx, startedAt, req.Provider, "oauth", "refresh", connectionMode, err != nil)
					return resp, true, err
				}
				resp, err := refresher.RefreshToken(ctx, credential.RefreshToken)
				metricutil.RecordConnectionAuthMetrics(ctx, startedAt, req.Provider, "oauth", "refresh", connectionMode, err != nil)
				return resp, true, err
			}
		}
	}
	if cfg.ManualConnectionAuth != nil {
		if connMap := cfg.ManualConnectionAuth()[req.Provider]; connMap != nil {
			if refresher := connMap[req.Connection]; refresher != nil {
				startedAt := time.Now()
				resp, err := refresher.RefreshToken(ctx, credential.RefreshToken)
				metricutil.RecordConnectionAuthMetrics(ctx, startedAt, req.Provider, "manual", "refresh", metricutil.NormalizeConnectionMode(req.Mode), err != nil)
				return resp, true, err
			}
		}
	}
	return nil, false, nil
}

type recordingExternalCredentialProvider struct {
	inner                   core.ExternalCredentialProvider
	getCredentialCalls      atomic.Int64
	listCredentialsCalls    atomic.Int64
	listForProviderCalls    atomic.Int64
	listForConnectionCalls  atomic.Int64
	putCredentialCalls      atomic.Int64
	restoreCredentialCalls  atomic.Int64
	deleteCredentialCalls   atomic.Int64
	validateConfigCalls     atomic.Int64
	resolveCredentialCalls  atomic.Int64
	exchangeCredentialCalls atomic.Int64
}

func TestS3ObjectAccessURLUploadsAndDownloadsPluginScopedObject(t *testing.T) {
	t.Parallel()

	store := &coretesting.StubS3{}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.PublicBaseURL = "https://gestalt.example.test"
		cfg.S3 = map[string]s3store.Client{"brainStorage": store}
	})
	defer ts.Close()

	manager, err := s3.NewObjectAccessURLManager(
		[]byte("0123456789abcdef0123456789abcdef"),
		ts.URL,
	)
	if err != nil {
		t.Fatalf("NewObjectAccessURLManager: %v", err)
	}
	targetRef := s3store.ObjectRef{
		Bucket: "brain",
		Key:    " workspaces/acme/tokens/token-1/content.bin ",
	}
	putURL, err := manager.MintURL(s3.ObjectAccessURLRequest{
		PluginName:  "brain",
		BindingName: "brainStorage",
		Ref:         targetRef,
		Method:      s3store.PresignMethodPut,
		Expires:     time.Minute,
		ContentType: "text/plain",
		Headers:     map[string]string{"Content-Length": "11"},
	})
	if err != nil {
		t.Fatalf("MintURL(put): %v", err)
	}
	putReq, err := http.NewRequest(http.MethodPut, putURL.URL, strings.NewReader("hello brain"))
	if err != nil {
		t.Fatalf("NewRequest(put): %v", err)
	}
	putReq.Header.Set("Content-Type", "text/plain")
	putResp, err := ts.Client().Do(putReq)
	if err != nil {
		t.Fatalf("PUT object access URL: %v", err)
	}
	_ = putResp.Body.Close()
	if putResp.StatusCode != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", putResp.StatusCode)
	}
	if putResp.Header.Get("ETag") == "" {
		t.Fatal("PUT response missing ETag")
	}

	prefixed := s3store.ObjectRef{
		Bucket: targetRef.Bucket,
		Key:    s3.PluginObjectKey("brain", targetRef.Key),
	}
	if _, err := store.HeadObject(context.Background(), prefixed); err != nil {
		t.Fatalf("HeadObject(prefixed): %v", err)
	}
	if _, err := store.HeadObject(context.Background(), targetRef); !errors.Is(err, s3store.ErrNotFound) {
		t.Fatalf("HeadObject(unprefixed) error = %v, want ErrNotFound", err)
	}

	getURL, err := manager.MintURL(s3.ObjectAccessURLRequest{
		PluginName:  "brain",
		BindingName: "brainStorage",
		Ref:         targetRef,
		Method:      s3store.PresignMethodGet,
		Expires:     time.Minute,
	})
	if err != nil {
		t.Fatalf("MintURL(get): %v", err)
	}
	getResp, err := ts.Client().Get(getURL.URL)
	if err != nil {
		t.Fatalf("GET object access URL: %v", err)
	}
	body, readErr := io.ReadAll(getResp.Body)
	_ = getResp.Body.Close()
	if readErr != nil {
		t.Fatalf("read GET body: %v", readErr)
	}
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", getResp.StatusCode)
	}
	if string(body) != "hello brain" {
		t.Fatalf("GET body = %q, want hello brain", body)
	}
	if getResp.Header.Get("Content-Type") != "text/plain" {
		t.Fatalf("GET Content-Type = %q, want text/plain", getResp.Header.Get("Content-Type"))
	}

	constrainedGetURL, err := manager.MintURL(s3.ObjectAccessURLRequest{
		PluginName:  "brain",
		BindingName: "brainStorage",
		Ref:         targetRef,
		Method:      s3store.PresignMethodGet,
		Expires:     time.Minute,
		Headers:     map[string]string{"X-Brain-Download": "ok"},
	})
	if err != nil {
		t.Fatalf("MintURL(constrained get): %v", err)
	}
	missingHeaderResp, err := ts.Client().Get(constrainedGetURL.URL)
	if err != nil {
		t.Fatalf("GET constrained object access URL without header: %v", err)
	}
	_ = missingHeaderResp.Body.Close()
	if missingHeaderResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("GET constrained status = %d, want 400", missingHeaderResp.StatusCode)
	}

	rangeReq, err := http.NewRequest(http.MethodGet, constrainedGetURL.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest(range get): %v", err)
	}
	rangeReq.Header.Set("X-Brain-Download", "ok")
	rangeReq.Header.Set("Range", "bytes=0-4")
	rangeResp, err := ts.Client().Do(rangeReq)
	if err != nil {
		t.Fatalf("GET ranged object access URL: %v", err)
	}
	rangeBody, readErr := io.ReadAll(rangeResp.Body)
	_ = rangeResp.Body.Close()
	if readErr != nil {
		t.Fatalf("read ranged GET body: %v", readErr)
	}
	if rangeResp.StatusCode != http.StatusPartialContent {
		t.Fatalf("GET ranged status = %d, want 206", rangeResp.StatusCode)
	}
	if string(rangeBody) != "hello" {
		t.Fatalf("GET ranged body = %q, want hello", rangeBody)
	}
	if rangeResp.Header.Get("Content-Range") != "bytes 0-4/11" {
		t.Fatalf("GET ranged Content-Range = %q, want bytes 0-4/11", rangeResp.Header.Get("Content-Range"))
	}

	fullSuffixReq, err := http.NewRequest(http.MethodGet, constrainedGetURL.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest(full suffix get): %v", err)
	}
	fullSuffixReq.Header.Set("X-Brain-Download", "ok")
	fullSuffixReq.Header.Set("Range", "bytes=-50")
	fullSuffixResp, err := ts.Client().Do(fullSuffixReq)
	if err != nil {
		t.Fatalf("GET full suffix object access URL: %v", err)
	}
	fullSuffixBody, readErr := io.ReadAll(fullSuffixResp.Body)
	_ = fullSuffixResp.Body.Close()
	if readErr != nil {
		t.Fatalf("read full suffix GET body: %v", readErr)
	}
	if fullSuffixResp.StatusCode != http.StatusOK {
		t.Fatalf("GET full suffix status = %d, want 200", fullSuffixResp.StatusCode)
	}
	if string(fullSuffixBody) != "hello brain" {
		t.Fatalf("GET full suffix body = %q, want hello brain", fullSuffixBody)
	}
	if got := fullSuffixResp.Header.Get("Content-Range"); got != "" {
		t.Fatalf("GET full suffix Content-Range = %q, want empty", got)
	}

	invalidConditionalReq, err := http.NewRequest(http.MethodGet, constrainedGetURL.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest(invalid conditional get): %v", err)
	}
	invalidConditionalReq.Header.Set("X-Brain-Download", "ok")
	invalidConditionalReq.Header.Set("If-Modified-Since", "not a valid http date")
	invalidConditionalResp, err := ts.Client().Do(invalidConditionalReq)
	if err != nil {
		t.Fatalf("GET invalid conditional object access URL: %v", err)
	}
	_ = invalidConditionalResp.Body.Close()
	if invalidConditionalResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("GET invalid conditional status = %d, want 400", invalidConditionalResp.StatusCode)
	}
}

func newRecordingExternalCredentialProvider(inner core.ExternalCredentialProvider) *recordingExternalCredentialProvider {
	return &recordingExternalCredentialProvider{inner: inner}
}

func (r *recordingExternalCredentialProvider) PutCredential(ctx context.Context, credential *core.ExternalCredential) error {
	r.putCredentialCalls.Add(1)
	return r.inner.PutCredential(ctx, credential)
}

func (r *recordingExternalCredentialProvider) RestoreCredential(ctx context.Context, credential *core.ExternalCredential) error {
	r.restoreCredentialCalls.Add(1)
	return r.inner.RestoreCredential(ctx, credential)
}

func (r *recordingExternalCredentialProvider) GetCredential(ctx context.Context, subjectID, connectionID, instance string) (*core.ExternalCredential, error) {
	r.getCredentialCalls.Add(1)
	return r.inner.GetCredential(ctx, subjectID, connectionID, instance)
}

func (r *recordingExternalCredentialProvider) ListCredentials(ctx context.Context, subjectID string) ([]*core.ExternalCredential, error) {
	r.listCredentialsCalls.Add(1)
	return r.inner.ListCredentials(ctx, subjectID)
}

func (r *recordingExternalCredentialProvider) ListCredentialsForConnection(ctx context.Context, subjectID, connectionID string) ([]*core.ExternalCredential, error) {
	r.listForConnectionCalls.Add(1)
	return r.inner.ListCredentialsForConnection(ctx, subjectID, connectionID)
}

func (r *recordingExternalCredentialProvider) DeleteCredential(ctx context.Context, id string) error {
	r.deleteCredentialCalls.Add(1)
	return r.inner.DeleteCredential(ctx, id)
}

func (r *recordingExternalCredentialProvider) ValidateCredentialConfig(ctx context.Context, req *core.ValidateExternalCredentialConfigRequest) error {
	r.validateConfigCalls.Add(1)
	return r.inner.ValidateCredentialConfig(ctx, req)
}

func (r *recordingExternalCredentialProvider) ResolveCredential(ctx context.Context, req *core.ResolveExternalCredentialRequest) (*core.ResolveExternalCredentialResponse, error) {
	r.resolveCredentialCalls.Add(1)
	return r.inner.ResolveCredential(ctx, req)
}

func (r *recordingExternalCredentialProvider) ExchangeCredential(ctx context.Context, req *core.ExchangeExternalCredentialRequest) (*core.ExchangeExternalCredentialResponse, error) {
	r.exchangeCredentialCalls.Add(1)
	return r.inner.ExchangeCredential(ctx, req)
}

func listTestCredentialsForProvider(ctx context.Context, provider core.ExternalCredentialProvider, subjectID, integration string) ([]*core.ExternalCredential, error) {
	tokens, err := provider.ListCredentials(ctx, subjectID)
	if err != nil {
		return nil, err
	}
	out := make([]*core.ExternalCredential, 0, len(tokens))
	for _, token := range tokens {
		if token != nil && token.Integration == integration {
			out = append(out, token)
		}
	}
	return out, nil
}

func (r *recordingExternalCredentialProvider) lookupCalls() int64 {
	return r.getCredentialCalls.Load() + r.listCredentialsCalls.Load() + r.listForProviderCalls.Load() + r.listForConnectionCalls.Load() + r.resolveCredentialCalls.Load()
}

type staticRuntimeInspector struct {
	snapshots []bootstrap.RuntimeProviderSnapshot
	logs      []runtimelogs.Record
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

func (s *staticRuntimeInspector) ListPluginRuntimeSessionLogs(_ context.Context, _ string, _ string, afterSeq int64, limit int) ([]runtimelogs.Record, error) {
	if s == nil {
		return nil, nil
	}
	if s.err != nil {
		return nil, s.err
	}
	out := make([]runtimelogs.Record, 0, len(s.logs))
	for _, entry := range s.logs {
		if entry.Seq <= afterSeq {
			continue
		}
		out = append(out, entry)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

type relayTestCacheServer struct {
	proto.UnimplementedCacheServer

	mu             sync.Mutex
	keys           []string
	receivedTokens []string
}

func (s *relayTestCacheServer) Get(ctx context.Context, req *proto.CacheGetRequest) (*proto.CacheGetResponse, error) {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		s.mu.Lock()
		s.keys = append(s.keys, req.GetKey())
		s.receivedTokens = append(s.receivedTokens, md.Get(runtimehost.HostServiceRelayTokenHeader)...)
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

func (s *relayTestCacheServer) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.keys)
}

type relayTestInvoker struct {
	mu             sync.Mutex
	calls          int
	providerName   string
	instance       string
	operation      string
	idempotencyKey string
	params         map[string]any
}

func (i *relayTestInvoker) Invoke(ctx context.Context, _ *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.calls++
	i.providerName = providerName
	i.instance = instance
	i.operation = operation
	i.idempotencyKey = invocation.IdempotencyKeyFromContext(ctx)
	i.params = maps.Clone(params)
	return &core.OperationResult{Status: 202, Body: "relayed"}, nil
}

type relayTestInvokerCall struct {
	calls          int
	providerName   string
	instance       string
	operation      string
	idempotencyKey string
	params         map[string]any
}

func (i *relayTestInvoker) snapshot() relayTestInvokerCall {
	i.mu.Lock()
	defer i.mu.Unlock()
	return relayTestInvokerCall{
		calls:          i.calls,
		providerName:   i.providerName,
		instance:       i.instance,
		operation:      i.operation,
		idempotencyKey: i.idempotencyKey,
		params:         maps.Clone(i.params),
	}
}

type relayTestWorkflowProvider struct {
	*memoryWorkflowProvider

	mu               sync.Mutex
	signalOrStartReq coreworkflow.SignalOrStartRunRequest
}

func newRelayTestWorkflowProvider() *relayTestWorkflowProvider {
	return &relayTestWorkflowProvider{
		memoryWorkflowProvider: newMemoryWorkflowProvider(),
	}
}

func (p *relayTestWorkflowProvider) SignalOrStartRun(_ context.Context, req coreworkflow.SignalOrStartRunRequest) (*coreworkflow.SignalRunResponse, error) {
	p.mu.Lock()
	p.signalOrStartReq = req
	p.mu.Unlock()
	now := time.Now().UTC()
	signal := req.Signal
	if signal.ID == "" {
		signal.ID = "signal-1"
	}
	return &coreworkflow.SignalRunResponse{
		Run: &coreworkflow.Run{
			ID:           "workflow-run-1",
			Status:       coreworkflow.RunStatusRunning,
			WorkflowKey:  req.WorkflowKey,
			Target:       req.Target,
			ExecutionRef: req.ExecutionRef,
			CreatedAt:    &now,
			StartedAt:    &now,
		},
		Signal:      signal,
		StartedRun:  true,
		WorkflowKey: req.WorkflowKey,
	}, nil
}

func (p *relayTestWorkflowProvider) signalOrStartRequest() coreworkflow.SignalOrStartRunRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.signalOrStartReq
}

type relayTestSessionVerifier struct {
	mu     sync.Mutex
	active map[string]bool
}

func newRelayTestSessionVerifier(sessionIDs ...string) *relayTestSessionVerifier {
	verifier := &relayTestSessionVerifier{active: map[string]bool{}}
	for _, sessionID := range sessionIDs {
		verifier.active[sessionID] = true
	}
	return verifier
}

func (v *relayTestSessionVerifier) VerifyHostServiceSession(_ context.Context, sessionID string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.active[strings.TrimSpace(sessionID)] {
		return nil
	}
	return fmt.Errorf("runtime session %q is not active", sessionID)
}

func (v *relayTestSessionVerifier) setActive(sessionID string, active bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.active[sessionID] = active
}

type relayTestWorkflowManagerHostServer struct {
	proto.UnimplementedWorkflowManagerHostServer
	calls *atomic.Int64
}

func (s relayTestWorkflowManagerHostServer) GetSchedule(_ context.Context, req *proto.WorkflowManagerGetScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
	if s.calls != nil {
		s.calls.Add(1)
	}
	return &proto.ManagedWorkflowSchedule{
		ProviderName: "registered",
		Schedule: &proto.BoundWorkflowSchedule{
			Id: req.GetScheduleId(),
		},
	}, nil
}

type relayTestAgentManagerHostServer struct {
	proto.UnimplementedAgentManagerHostServer
	calls *atomic.Int64
}

func (s relayTestAgentManagerHostServer) GetSession(_ context.Context, req *proto.AgentManagerGetSessionRequest) (*proto.AgentSession, error) {
	if s.calls != nil {
		s.calls.Add(1)
	}
	return &proto.AgentSession{
		Id:           req.GetSessionId(),
		ProviderName: "registered",
		Model:        "test-model",
	}, nil
}

type relayTestRuntimeLogHostServer struct {
	proto.UnimplementedPluginRuntimeLogHostServer
	calls *atomic.Int64
}

func (s relayTestRuntimeLogHostServer) AppendLogs(_ context.Context, req *proto.AppendPluginRuntimeLogsRequest) (*proto.AppendPluginRuntimeLogsResponse, error) {
	if s.calls != nil {
		s.calls.Add(1)
	}
	return &proto.AppendPluginRuntimeLogsResponse{LastSeq: int64(len(req.GetLogs()))}, nil
}

func TestHostServiceRelayProxiesGRPCRequests(t *testing.T) {
	t.Parallel()

	secret := []byte("relay-test-secret-0123456789abcd")
	cacheSrv := &relayTestCacheServer{}
	const envVar = "GESTALT_TEST_CACHE_SOCKET"
	publicHostServices := runtimehost.NewPublicHostServiceRegistry()
	sessionVerifier := newRelayTestSessionVerifier("session-1")
	var registerCalls atomic.Int64
	hostService := runtimehost.HostService{
		Name:   "cache",
		EnvVar: envVar,
		Register: func(srv *grpc.Server) {
			registerCalls.Add(1)
			proto.RegisterCacheServer(srv, cacheSrv)
		},
	}

	ts := httptest.NewUnstartedServer(newTestHandler(t, func(cfg *server.Config) {
		cfg.RouteProfile = server.RouteProfilePublic
		cfg.StateSecret = secret
		cfg.PublicHostServices = publicHostServices
	}))
	ts.EnableHTTP2 = true
	ts.StartTLS()
	testutil.CloseOnCleanup(t, ts)
	registration := publicHostServices.RegisterVerified("support", sessionVerifier, hostService)

	tokenManager, err := runtimehost.NewHostServiceRelayTokenManager(secret)
	if err != nil {
		t.Fatalf("NewHostServiceRelayTokenManager: %v", err)
	}
	token, err := tokenManager.MintToken(runtimehost.HostServiceRelayTokenRequest{
		PluginName:   "support",
		SessionID:    "session-1",
		Service:      "cache",
		EnvVar:       envVar,
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
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(runtimehost.HostServiceRelayTokenHeader, token))

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

	secondCtx, secondCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer secondCancel()
	secondCtx = metadata.NewOutgoingContext(secondCtx, metadata.Pairs(runtimehost.HostServiceRelayTokenHeader, token))
	if _, err := proto.NewCacheClient(conn).Get(secondCtx, &proto.CacheGetRequest{Key: "again"}); err != nil {
		t.Fatalf("second Cache.Get via relay: %v", err)
	}
	if got := registerCalls.Load(); got != 1 {
		t.Fatalf("host service registrations = %d, want cached handler registered once", got)
	}

	sessionVerifier.setActive("session-1", false)
	staleCtx, staleCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer staleCancel()
	staleCtx = metadata.NewOutgoingContext(staleCtx, metadata.Pairs(runtimehost.HostServiceRelayTokenHeader, token))
	_, err = proto.NewCacheClient(conn).Get(staleCtx, &proto.CacheGetRequest{Key: "stale"})
	if grpcstatus.Code(err) != codes.Unauthenticated {
		t.Fatalf("Cache.Get stale session code = %v, want %v (err=%v)", grpcstatus.Code(err), codes.Unauthenticated, err)
	}
	if got := cacheSrv.calls(); got != 2 {
		t.Fatalf("backend calls = %d, want only the verified calls", got)
	}

	sessionVerifier.setActive("session-1", true)
	registration.Unregister()
	unregisteredCtx, unregisteredCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer unregisteredCancel()
	unregisteredCtx = metadata.NewOutgoingContext(unregisteredCtx, metadata.Pairs(runtimehost.HostServiceRelayTokenHeader, token))
	_, err = proto.NewCacheClient(conn).Get(unregisteredCtx, &proto.CacheGetRequest{Key: "unregistered"})
	if grpcstatus.Code(err) != codes.Unavailable {
		t.Fatalf("Cache.Get unregistered provider code = %v, want %v (err=%v)", grpcstatus.Code(err), codes.Unavailable, err)
	}
	if got := cacheSrv.calls(); got != 2 {
		t.Fatalf("backend calls = %d, want no calls after unregister", got)
	}
}

func TestHostServiceRelayProxiesGRPCRequestsOnManagementProfile(t *testing.T) {
	t.Parallel()

	secret := []byte("relay-test-secret-0123456789abcd")
	cacheSrv := &relayTestCacheServer{}
	const envVar = "GESTALT_TEST_CACHE_SOCKET"
	publicHostServices := runtimehost.NewPublicHostServiceRegistry()
	publicHostServices.RegisterVerified("support", newRelayTestSessionVerifier("session-1"), runtimehost.HostService{
		Name:   "cache",
		EnvVar: envVar,
		Register: func(srv *grpc.Server) {
			proto.RegisterCacheServer(srv, cacheSrv)
		},
	})

	ts := httptest.NewUnstartedServer(newTestHandler(t, func(cfg *server.Config) {
		cfg.RouteProfile = server.RouteProfileManagement
		cfg.StateSecret = secret
		cfg.PublicHostServices = publicHostServices
	}))
	ts.EnableHTTP2 = true
	ts.StartTLS()
	testutil.CloseOnCleanup(t, ts)

	tokenManager, err := runtimehost.NewHostServiceRelayTokenManager(secret)
	if err != nil {
		t.Fatalf("NewHostServiceRelayTokenManager: %v", err)
	}
	token, err := tokenManager.MintToken(runtimehost.HostServiceRelayTokenRequest{
		PluginName:   "support",
		SessionID:    "session-1",
		Service:      "cache",
		EnvVar:       envVar,
		MethodPrefix: "/" + proto.Cache_ServiceDesc.ServiceName + "/",
		TTL:          time.Minute,
	})
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}

	conn := newRelayGRPCConn(t, ts)
	defer func() { _ = conn.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(runtimehost.HostServiceRelayTokenHeader, token))
	resp, err := proto.NewCacheClient(conn).Get(ctx, &proto.CacheGetRequest{Key: "management"})
	if err != nil {
		t.Fatalf("Cache.Get via management relay: %v", err)
	}
	if got := string(resp.GetValue()); got != "relay:management" {
		t.Fatalf("Cache.Get value = %q, want relay:management", got)
	}
	if got := cacheSrv.calls(); got != 1 {
		t.Fatalf("backend calls = %d, want 1", got)
	}
}

func TestHostServiceRelaySelectsVerifierForDuplicateProviderWideServices(t *testing.T) {
	t.Parallel()

	secret := []byte("relay-test-secret-0123456789abcd")
	const envVar = "GESTALT_TEST_CACHE_SOCKET"
	cacheSrv1 := &relayTestCacheServer{}
	cacheSrv2 := &relayTestCacheServer{}
	publicHostServices := runtimehost.NewPublicHostServiceRegistry()
	hostService1 := runtimehost.HostService{
		Name:   "cache",
		EnvVar: envVar,
		Register: func(srv *grpc.Server) {
			proto.RegisterCacheServer(srv, cacheSrv1)
		},
	}
	hostService2 := runtimehost.HostService{
		Name:   "cache",
		EnvVar: envVar,
		Register: func(srv *grpc.Server) {
			proto.RegisterCacheServer(srv, cacheSrv2)
		},
	}
	publicHostServices.RegisterVerified("support", newRelayTestSessionVerifier("session-1"), hostService1)
	session2Registration := publicHostServices.RegisterVerified("support", newRelayTestSessionVerifier("session-2"), hostService2)

	ts := httptest.NewUnstartedServer(newTestHandler(t, func(cfg *server.Config) {
		cfg.RouteProfile = server.RouteProfilePublic
		cfg.StateSecret = secret
		cfg.PublicHostServices = publicHostServices
	}))
	ts.EnableHTTP2 = true
	ts.StartTLS()
	testutil.CloseOnCleanup(t, ts)

	tokenManager, err := runtimehost.NewHostServiceRelayTokenManager(secret)
	if err != nil {
		t.Fatalf("NewHostServiceRelayTokenManager: %v", err)
	}
	token, err := tokenManager.MintToken(runtimehost.HostServiceRelayTokenRequest{
		PluginName:   "support",
		SessionID:    "session-2",
		Service:      "cache",
		EnvVar:       envVar,
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
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(runtimehost.HostServiceRelayTokenHeader, token))
	if _, err := proto.NewCacheClient(conn).Get(ctx, &proto.CacheGetRequest{Key: "selected"}); err != nil {
		t.Fatalf("Cache.Get via duplicate relay: %v", err)
	}
	if got := cacheSrv1.calls(); got != 0 {
		t.Fatalf("first backend calls = %d, want 0", got)
	}
	if got := cacheSrv2.calls(); got != 1 {
		t.Fatalf("second backend calls = %d, want 1", got)
	}

	session2Registration.Unregister()
	session1Token, err := tokenManager.MintToken(runtimehost.HostServiceRelayTokenRequest{
		PluginName:   "support",
		SessionID:    "session-1",
		Service:      "cache",
		EnvVar:       envVar,
		MethodPrefix: "/gestalt.provider.v1.Cache/",
		TTL:          time.Minute,
	})
	if err != nil {
		t.Fatalf("MintToken(session-1): %v", err)
	}
	session1Ctx, session1Cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer session1Cancel()
	session1Ctx = metadata.NewOutgoingContext(session1Ctx, metadata.Pairs(runtimehost.HostServiceRelayTokenHeader, session1Token))
	if _, err := proto.NewCacheClient(conn).Get(session1Ctx, &proto.CacheGetRequest{Key: "still-active"}); err != nil {
		t.Fatalf("Cache.Get via remaining duplicate relay: %v", err)
	}
	if got := cacheSrv1.calls(); got != 1 {
		t.Fatalf("first backend calls after unregistering second registration = %d, want 1", got)
	}

	removedCtx, removedCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer removedCancel()
	removedCtx = metadata.NewOutgoingContext(removedCtx, metadata.Pairs(runtimehost.HostServiceRelayTokenHeader, token))
	_, err = proto.NewCacheClient(conn).Get(removedCtx, &proto.CacheGetRequest{Key: "removed"})
	if grpcstatus.Code(err) != codes.Unauthenticated {
		t.Fatalf("Cache.Get removed duplicate code = %v, want %v (err=%v)", grpcstatus.Code(err), codes.Unauthenticated, err)
	}
	if got := cacheSrv2.calls(); got != 1 {
		t.Fatalf("second backend calls after unregister = %d, want 1", got)
	}
}

func TestHostServiceRelayStopsServingUnregisteredProviderService(t *testing.T) {
	t.Parallel()

	secret := []byte("relay-test-secret-0123456789abcd")
	cacheSrv := &relayTestCacheServer{}
	const envVar = "GESTALT_TEST_CACHE_SOCKET"
	publicHostServices := runtimehost.NewPublicHostServiceRegistry()
	sessionVerifier := newRelayTestSessionVerifier("session-1")
	hostService := runtimehost.HostService{
		Name:   "cache",
		EnvVar: envVar,
		Register: func(srv *grpc.Server) {
			proto.RegisterCacheServer(srv, cacheSrv)
		},
	}
	registration := publicHostServices.RegisterVerified("support", sessionVerifier, hostService)

	ts := httptest.NewUnstartedServer(newTestHandler(t, func(cfg *server.Config) {
		cfg.RouteProfile = server.RouteProfilePublic
		cfg.StateSecret = secret
		cfg.PublicHostServices = publicHostServices
	}))
	ts.EnableHTTP2 = true
	ts.StartTLS()
	testutil.CloseOnCleanup(t, ts)

	tokenManager, err := runtimehost.NewHostServiceRelayTokenManager(secret)
	if err != nil {
		t.Fatalf("NewHostServiceRelayTokenManager: %v", err)
	}
	token, err := tokenManager.MintToken(runtimehost.HostServiceRelayTokenRequest{
		PluginName:   "support",
		SessionID:    "session-1",
		Service:      "cache",
		EnvVar:       envVar,
		MethodPrefix: "/" + proto.Cache_ServiceDesc.ServiceName + "/",
		TTL:          time.Minute,
	})
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}

	conn := newRelayGRPCConn(t, ts)
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(runtimehost.HostServiceRelayTokenHeader, token))
	if _, err := proto.NewCacheClient(conn).Get(ctx, &proto.CacheGetRequest{Key: "active"}); err != nil {
		t.Fatalf("Cache.Get via relay: %v", err)
	}

	registration.Unregister()
	staleCtx, staleCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer staleCancel()
	staleCtx = metadata.NewOutgoingContext(staleCtx, metadata.Pairs(runtimehost.HostServiceRelayTokenHeader, token))
	_, err = proto.NewCacheClient(conn).Get(staleCtx, &proto.CacheGetRequest{Key: "stale"})
	if grpcstatus.Code(err) != codes.Unavailable {
		t.Fatalf("Cache.Get unregistered service code = %v, want %v (err=%v)", grpcstatus.Code(err), codes.Unavailable, err)
	}
	if got := cacheSrv.calls(); got != 1 {
		t.Fatalf("backend calls = %d, want only the registered call", got)
	}
}

func TestHostServiceRelayRoutesRegisteredPluginInvokerService(t *testing.T) {
	t.Parallel()

	secret := []byte("relay-test-secret-0123456789abcd")
	invoker := &relayTestInvoker{}
	invokes := []invocation.PluginInvocationDependency{{
		Plugin:         "slack",
		Operation:      "events.reply",
		CredentialMode: core.ConnectionModeNone,
	}}
	invocationTokens, err := plugininvokerservice.NewInvocationTokenManager(secret)
	if err != nil {
		t.Fatalf("NewInvocationTokenManager: %v", err)
	}
	publicHostServices := runtimehost.NewPublicHostServiceRegistry()
	sessionVerifier := newRelayTestSessionVerifier("provider-dev-session")
	publicHostServices.RegisterVerified("support", sessionVerifier, runtimehost.HostService{
		Name:   "plugin_invoker",
		EnvVar: plugininvokerservice.DefaultSocketEnv,
		Register: func(srv *grpc.Server) {
			proto.RegisterPluginInvokerServer(srv, plugininvokerservice.NewServer("support", invokes, invoker, invocationTokens))
		},
	})
	ts := httptest.NewUnstartedServer(newTestHandler(t, func(cfg *server.Config) {
		cfg.RouteProfile = server.RouteProfilePublic
		cfg.StateSecret = secret
		cfg.PublicHostServices = publicHostServices
	}))
	ts.EnableHTTP2 = true
	ts.StartTLS()
	testutil.CloseOnCleanup(t, ts)

	tokenManager, err := runtimehost.NewHostServiceRelayTokenManager(secret)
	if err != nil {
		t.Fatalf("NewHostServiceRelayTokenManager: %v", err)
	}
	relayToken, err := tokenManager.MintToken(runtimehost.HostServiceRelayTokenRequest{
		PluginName:   "support",
		SessionID:    "provider-dev-session",
		Service:      "plugin_invoker",
		EnvVar:       plugininvokerservice.DefaultSocketEnv,
		MethodPrefix: "/" + proto.PluginInvoker_ServiceDesc.ServiceName + "/",
		TTL:          time.Minute,
	})
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	principalCtx := principal.WithPrincipal(context.Background(), &principal.Principal{
		SubjectID: "user:test-user",
		UserID:    "test-user",
		Kind:      principal.KindUser,
		Source:    principal.SourceSession,
	})
	invocationToken, err := invocationTokens.MintRootToken(principalCtx, "support", plugininvokerservice.InvocationDependencyGrants(invokes))
	if err != nil {
		t.Fatalf("MintRootToken: %v", err)
	}

	conn := newRelayGRPCConn(t, ts)
	defer func() { _ = conn.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(runtimehost.HostServiceRelayTokenHeader, relayToken))
	_, err = proto.NewPluginInvokerClient(conn).Invoke(ctx, &proto.PluginInvokeRequest{
		InvocationToken: invocationToken,
		Plugin:          "slack",
		Operation:       "events.reply",
		Instance:        "prod",
		IdempotencyKey:  "provider-dev-call",
	})
	if err != nil {
		t.Fatalf("PluginInvoker.Invoke via registered relay: %v", err)
	}
	if call := invoker.snapshot(); call.calls != 1 || call.providerName != "slack" || call.operation != "events.reply" || call.instance != "prod" {
		t.Fatalf("plugin invoker call = %+v, want slack events.reply/prod", call)
	}

	sessionVerifier.setActive("provider-dev-session", false)
	staleCtx, staleCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer staleCancel()
	staleCtx = metadata.NewOutgoingContext(staleCtx, metadata.Pairs(runtimehost.HostServiceRelayTokenHeader, relayToken))
	_, err = proto.NewPluginInvokerClient(conn).Invoke(staleCtx, &proto.PluginInvokeRequest{
		InvocationToken: invocationToken,
		Plugin:          "slack",
		Operation:       "events.reply",
	})
	if grpcstatus.Code(err) != codes.Unauthenticated {
		t.Fatalf("PluginInvoker.Invoke stale session code = %v, want %v (err=%v)", grpcstatus.Code(err), codes.Unauthenticated, err)
	}
	if call := invoker.snapshot(); call.calls != 1 {
		t.Fatalf("plugin invoker calls = %d, want only the verified call", call.calls)
	}
}

func TestHostServiceRelayRoutesRegisteredRuntimeCoreServices(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		service      string
		envVar       string
		methodPrefix string
		register     func(*grpc.Server, *atomic.Int64)
		call         func(*testing.T, context.Context, *grpc.ClientConn)
	}{
		{
			name:         "workflow manager",
			service:      "workflow_manager",
			envVar:       workflowservice.DefaultManagerSocketEnv,
			methodPrefix: "/" + proto.WorkflowManagerHost_ServiceDesc.ServiceName + "/",
			register: func(srv *grpc.Server, calls *atomic.Int64) {
				proto.RegisterWorkflowManagerHostServer(srv, relayTestWorkflowManagerHostServer{calls: calls})
			},
			call: func(t *testing.T, ctx context.Context, conn *grpc.ClientConn) {
				t.Helper()
				resp, err := proto.NewWorkflowManagerHostClient(conn).GetSchedule(ctx, &proto.WorkflowManagerGetScheduleRequest{ScheduleId: "schedule-1"})
				if err != nil {
					t.Fatalf("WorkflowManager.GetSchedule via relay: %v", err)
				}
				if resp.GetProviderName() != "registered" || resp.GetSchedule().GetId() != "schedule-1" {
					t.Fatalf("WorkflowManager.GetSchedule response = %+v, want registered schedule-1", resp)
				}
			},
		},
		{
			name:         "agent manager",
			service:      "agent_manager",
			envVar:       agentservice.DefaultManagerSocketEnv,
			methodPrefix: "/" + proto.AgentManagerHost_ServiceDesc.ServiceName + "/",
			register: func(srv *grpc.Server, calls *atomic.Int64) {
				proto.RegisterAgentManagerHostServer(srv, relayTestAgentManagerHostServer{calls: calls})
			},
			call: func(t *testing.T, ctx context.Context, conn *grpc.ClientConn) {
				t.Helper()
				resp, err := proto.NewAgentManagerHostClient(conn).GetSession(ctx, &proto.AgentManagerGetSessionRequest{SessionId: "agent-session-1"})
				if err != nil {
					t.Fatalf("AgentManager.GetSession via relay: %v", err)
				}
				if resp.GetProviderName() != "registered" || resp.GetId() != "agent-session-1" {
					t.Fatalf("AgentManager.GetSession response = %+v, want registered agent-session-1", resp)
				}
			},
		},
		{
			name:         "runtime log host",
			service:      "runtime_log_host",
			envVar:       runtimehost.DefaultRuntimeLogHostSocketEnv,
			methodPrefix: "/" + proto.PluginRuntimeLogHost_ServiceDesc.ServiceName + "/",
			register: func(srv *grpc.Server, calls *atomic.Int64) {
				proto.RegisterPluginRuntimeLogHostServer(srv, relayTestRuntimeLogHostServer{calls: calls})
			},
			call: func(t *testing.T, ctx context.Context, conn *grpc.ClientConn) {
				t.Helper()
				resp, err := proto.NewPluginRuntimeLogHostClient(conn).AppendLogs(ctx, &proto.AppendPluginRuntimeLogsRequest{
					SessionId: "runtime-session-1",
					Logs: []*proto.PluginRuntimeLogEntry{{
						Stream:    proto.PluginRuntimeLogStream_PLUGIN_RUNTIME_LOG_STREAM_STDOUT,
						Message:   "hello",
						SourceSeq: 1,
					}},
				})
				if err != nil {
					t.Fatalf("PluginRuntimeLogHost.AppendLogs via relay: %v", err)
				}
				if resp.GetLastSeq() != 1 {
					t.Fatalf("PluginRuntimeLogHost.AppendLogs last_seq = %d, want 1", resp.GetLastSeq())
				}
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			secret := []byte("relay-test-secret-0123456789abcd")
			var calls atomic.Int64
			publicHostServices := runtimehost.NewPublicHostServiceRegistry()
			sessionVerifier := newRelayTestSessionVerifier("session-1")
			publicHostServices.RegisterVerified("support", sessionVerifier, runtimehost.HostService{
				Name:   tc.service,
				EnvVar: tc.envVar,
				Register: func(srv *grpc.Server) {
					tc.register(srv, &calls)
				},
			})

			ts := httptest.NewUnstartedServer(newTestHandler(t, func(cfg *server.Config) {
				cfg.RouteProfile = server.RouteProfilePublic
				cfg.StateSecret = secret
				cfg.PublicHostServices = publicHostServices
			}))
			ts.EnableHTTP2 = true
			ts.StartTLS()
			testutil.CloseOnCleanup(t, ts)

			tokenManager, err := runtimehost.NewHostServiceRelayTokenManager(secret)
			if err != nil {
				t.Fatalf("NewHostServiceRelayTokenManager: %v", err)
			}
			relayToken, err := tokenManager.MintToken(runtimehost.HostServiceRelayTokenRequest{
				PluginName:   "support",
				SessionID:    "session-1",
				Service:      tc.service,
				EnvVar:       tc.envVar,
				MethodPrefix: tc.methodPrefix,
				TTL:          time.Minute,
			})
			if err != nil {
				t.Fatalf("MintToken: %v", err)
			}

			conn := newRelayGRPCConn(t, ts)
			defer func() { _ = conn.Close() }()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(runtimehost.HostServiceRelayTokenHeader, relayToken))
			tc.call(t, ctx, conn)
			if got := calls.Load(); got != 1 {
				t.Fatalf("registered handler calls = %d, want 1", got)
			}

			sessionVerifier.setActive("session-1", false)
			staleCtx, staleCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer staleCancel()
			staleCtx = metadata.NewOutgoingContext(staleCtx, metadata.Pairs(runtimehost.HostServiceRelayTokenHeader, relayToken))
			switch tc.service {
			case "workflow_manager":
				_, err = proto.NewWorkflowManagerHostClient(conn).GetSchedule(staleCtx, &proto.WorkflowManagerGetScheduleRequest{ScheduleId: "schedule-1"})
			case "agent_manager":
				_, err = proto.NewAgentManagerHostClient(conn).GetSession(staleCtx, &proto.AgentManagerGetSessionRequest{SessionId: "agent-session-1"})
			case "runtime_log_host":
				_, err = proto.NewPluginRuntimeLogHostClient(conn).AppendLogs(staleCtx, &proto.AppendPluginRuntimeLogsRequest{
					SessionId: "runtime-session-1",
					Logs:      []*proto.PluginRuntimeLogEntry{{Message: "stale"}},
				})
			}
			if grpcstatus.Code(err) != codes.Unauthenticated {
				t.Fatalf("stale %s relay code = %v, want %v (err=%v)", tc.service, grpcstatus.Code(err), codes.Unauthenticated, err)
			}
			if got := calls.Load(); got != 1 {
				t.Fatalf("registered handler calls after stale session = %d, want 1", got)
			}
		})
	}
}

func TestHostServiceRelayDoesNotFallbackWithoutRegisteredService(t *testing.T) {
	t.Parallel()

	secret := []byte("relay-test-secret-0123456789abcd")
	invoker := &relayTestInvoker{}
	invokes := []invocation.PluginInvocationDependency{{
		Plugin:         "slack",
		Operation:      "events.reply",
		CredentialMode: core.ConnectionModeNone,
	}}
	ts := httptest.NewUnstartedServer(newTestHandler(t, func(cfg *server.Config) {
		cfg.RouteProfile = server.RouteProfilePublic
		cfg.StateSecret = secret
		cfg.Invoker = invoker
		cfg.PluginDefs = map[string]*config.ProviderEntry{
			"support": {Invokes: configPluginInvocationDependencies(invokes)},
		}
	}))
	ts.EnableHTTP2 = true
	ts.StartTLS()
	testutil.CloseOnCleanup(t, ts)

	tokenManager, err := runtimehost.NewHostServiceRelayTokenManager(secret)
	if err != nil {
		t.Fatalf("NewHostServiceRelayTokenManager: %v", err)
	}
	relayToken, err := tokenManager.MintToken(runtimehost.HostServiceRelayTokenRequest{
		PluginName:   "support",
		SessionID:    "provider-dev-session",
		Service:      "plugin_invoker",
		EnvVar:       plugininvokerservice.DefaultSocketEnv,
		MethodPrefix: "/" + proto.PluginInvoker_ServiceDesc.ServiceName + "/",
		TTL:          time.Minute,
	})
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}

	conn := newRelayGRPCConn(t, ts)
	defer func() { _ = conn.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(runtimehost.HostServiceRelayTokenHeader, relayToken))
	_, err = proto.NewPluginInvokerClient(conn).Invoke(ctx, &proto.PluginInvokeRequest{})
	if grpcstatus.Code(err) != codes.Unavailable {
		t.Fatalf("PluginInvoker.Invoke without registered service code = %v, want %v (err=%v)", grpcstatus.Code(err), codes.Unavailable, err)
	}
	if call := invoker.snapshot(); call.providerName != "" {
		t.Fatalf("invoker was called without registered relay service: %+v", call)
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
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(runtimehost.HostServiceRelayTokenHeader, "not-a-valid-token"))

	_, err := proto.NewCacheClient(conn).Get(ctx, &proto.CacheGetRequest{Key: "hello"})
	if grpcstatus.Code(err) != codes.Unauthenticated {
		t.Fatalf("Cache.Get invalid token code = %v, want %v (err=%v)", grpcstatus.Code(err), codes.Unauthenticated, err)
	}
}

func TestHostServiceRelayRejectsMethodOutsideTokenPrefix(t *testing.T) {
	t.Parallel()

	secret := []byte("relay-test-secret-0123456789abcd")
	cacheSrv := &relayTestCacheServer{}
	const envVar = "GESTALT_TEST_CACHE_SOCKET"
	publicHostServices := runtimehost.NewPublicHostServiceRegistry()
	publicHostServices.RegisterVerified("support", newRelayTestSessionVerifier("session-1"), runtimehost.HostService{
		Name:   "cache",
		EnvVar: envVar,
		Register: func(srv *grpc.Server) {
			proto.RegisterCacheServer(srv, cacheSrv)
		},
	})

	ts := httptest.NewUnstartedServer(newTestHandler(t, func(cfg *server.Config) {
		cfg.RouteProfile = server.RouteProfilePublic
		cfg.StateSecret = secret
		cfg.PublicHostServices = publicHostServices
	}))
	ts.EnableHTTP2 = true
	ts.StartTLS()
	testutil.CloseOnCleanup(t, ts)

	tokenManager, err := runtimehost.NewHostServiceRelayTokenManager(secret)
	if err != nil {
		t.Fatalf("NewHostServiceRelayTokenManager: %v", err)
	}
	token, err := tokenManager.MintToken(runtimehost.HostServiceRelayTokenRequest{
		PluginName:   "support",
		SessionID:    "session-1",
		Service:      "cache",
		EnvVar:       envVar,
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
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(runtimehost.HostServiceRelayTokenHeader, token))

	_, err = proto.NewCacheClient(conn).Get(ctx, &proto.CacheGetRequest{Key: "hello"})
	if grpcstatus.Code(err) != codes.PermissionDenied {
		t.Fatalf("Cache.Get disallowed method code = %v, want %v (err=%v)", grpcstatus.Code(err), codes.PermissionDenied, err)
	}
}

func TestHostServiceRelaySupportsIndexedDBSDKClient(t *testing.T) {
	t.Parallel()

	secret := []byte("relay-test-secret-0123456789abcd")
	stubDB := &coretesting.StubIndexedDB{}
	publicHostServices := runtimehost.NewPublicHostServiceRegistry()
	publicHostServices.RegisterVerified("relay-plugin", newRelayTestSessionVerifier("session-1"), runtimehost.HostService{
		Name:   "indexeddb",
		EnvVar: indexeddbservice.DefaultSocketEnv,
		Register: func(srv *grpc.Server) {
			proto.RegisterIndexedDBServer(srv, indexeddbservice.NewServer(stubDB, "relay-plugin", indexeddbservice.ServerOptions{
				AllowedStores: []string{"tasks"},
			}))
		},
	})

	ts := httptest.NewUnstartedServer(newTestHandler(t, func(cfg *server.Config) {
		cfg.RouteProfile = server.RouteProfilePublic
		cfg.StateSecret = secret
		cfg.PublicHostServices = publicHostServices
	}))
	ts.EnableHTTP2 = true
	ts.StartTLS()
	testutil.CloseOnCleanup(t, ts)

	tokenManager, err := runtimehost.NewHostServiceRelayTokenManager(secret)
	if err != nil {
		t.Fatalf("NewHostServiceRelayTokenManager: %v", err)
	}
	token, err := tokenManager.MintToken(runtimehost.HostServiceRelayTokenRequest{
		PluginName:   "relay-plugin",
		SessionID:    "session-1",
		Service:      "indexeddb",
		EnvVar:       indexeddbservice.DefaultSocketEnv,
		MethodPrefix: "/" + proto.IndexedDB_ServiceDesc.ServiceName + "/",
		TTL:          time.Minute,
	})
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(runtimehost.HostServiceRelayTokenHeader, token))

	recordValue, err := indexeddbcodec.RecordToProto(indexeddbcodec.Record{"id": "task-1", "value": "ship-it"})
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
	record, err := indexeddbcodec.RecordFromProto(resp.GetRecord())
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

	proxyURL := mustEgressProxyURL(t, proxy.URL, secret, egressproxy.TokenRequest{
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

func TestEgressProxyProxiesHTTPRequestOnManagementProfile(t *testing.T) {
	t.Parallel()

	secret := []byte("relay-test-secret-0123456789abcd")
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/management" {
			t.Fatalf("target path = %q, want /management", got)
		}
		_, _ = io.WriteString(w, "management-proxied-ok")
	}))
	testutil.CloseOnCleanup(t, target)

	proxy := httptest.NewUnstartedServer(newTestHandler(t, func(cfg *server.Config) {
		cfg.RouteProfile = server.RouteProfileManagement
		cfg.StateSecret = secret
	}))
	proxy.EnableHTTP2 = true
	proxy.StartTLS()
	testutil.CloseOnCleanup(t, proxy)

	proxyURL := mustEgressProxyURL(t, proxy.URL, secret, egressproxy.TokenRequest{
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
	resp, err := client.Get(target.URL + "/management")
	if err != nil {
		t.Fatalf("GET via management egress proxy: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("proxy status = %d, want %d (body=%s)", resp.StatusCode, http.StatusOK, string(body))
	}
	if got := string(body); got != "management-proxied-ok" {
		t.Fatalf("proxy body = %q, want %q", got, "management-proxied-ok")
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

	proxyURL := mustEgressProxyURL(t, proxy.URL, secret, egressproxy.TokenRequest{
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

	proxyURL := mustEgressProxyURL(t, proxy.URL, secret, egressproxy.TokenRequest{
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

func TestEgressProxyConnectForwardsBufferedClientBytes(t *testing.T) {
	t.Parallel()

	secret := []byte("relay-test-secret-0123456789abcd")
	targetListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen target: %v", err)
	}
	t.Cleanup(func() { _ = targetListener.Close() })

	payload := []byte("prefetched-client-bytes")
	reply := []byte("target-acknowledged")
	targetDone := make(chan error, 1)
	go func() {
		conn, err := targetListener.Accept()
		if err != nil {
			targetDone <- err
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

		got := make([]byte, len(payload))
		if _, err := io.ReadFull(conn, got); err != nil {
			targetDone <- fmt.Errorf("read payload: %w", err)
			return
		}
		if !bytes.Equal(got, payload) {
			targetDone <- fmt.Errorf("payload = %q, want %q", string(got), string(payload))
			return
		}
		if _, err := conn.Write(reply); err != nil {
			targetDone <- fmt.Errorf("write reply: %w", err)
			return
		}
		targetDone <- nil
	}()

	proxy := httptest.NewTLSServer(newTestHandler(t, func(cfg *server.Config) {
		cfg.RouteProfile = server.RouteProfilePublic
		cfg.StateSecret = secret
	}))
	testutil.CloseOnCleanup(t, proxy)

	tokenManager, err := egressproxy.NewTokenManager(secret)
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}
	token, err := tokenManager.MintToken(egressproxy.TokenRequest{
		PluginName:   "support",
		SessionID:    "session-1",
		AllowedHosts: []string{"127.0.0.1", "localhost"},
	})
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}

	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatalf("Parse proxy URL: %v", err)
	}
	conn, err := tls.Dial("tcp", proxyURL.Host, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatalf("Dial proxy: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("gestalt-egress-proxy:"+token))
	targetAddr := targetListener.Addr().String()
	request := fmt.Sprintf(
		"CONNECT %s HTTP/1.1\r\nHost: %s\r\nProxy-Authorization: %s\r\n\r\n%s",
		targetAddr,
		targetAddr,
		authHeader,
		payload,
	)
	if _, err := conn.Write([]byte(request)); err != nil {
		t.Fatalf("Write CONNECT request: %v", err)
	}

	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatalf("ReadResponse: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("proxy status = %d, want %d (body=%s)", resp.StatusCode, http.StatusOK, string(body))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(len(reply))))
	if err != nil {
		t.Fatalf("ReadAll tunneled reply: %v", err)
	}
	if got := string(body); got != string(reply) {
		t.Fatalf("tunneled reply = %q, want %q", got, string(reply))
	}

	if err := <-targetDone; err != nil {
		t.Fatal(err)
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

func mustEgressProxyURL(t *testing.T, baseURL string, secret []byte, req egressproxy.TokenRequest) *url.URL {
	t.Helper()

	tokenManager, err := egressproxy.NewTokenManager(secret)
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
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
	seedAPITokenWithPermissions(t, svc, plaintext, hashed, userID, nil)
}

func seedAPITokenWithPermissions(t *testing.T, svc *coredata.Services, plaintext, hashed, userID string, permissions []core.AccessPermission) *core.User {
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
		Permissions:         cloneAccessPermissionsForTest(permissions),
	}); err != nil {
		t.Fatalf("seedAPIToken: StoreAPIToken: %v", err)
	}
	return user
}

func seedSubjectAPIToken(t *testing.T, svc *coredata.Services, hashed, subjectID, name string) {
	t.Helper()
	seedSubjectAPITokenWithPermissions(t, svc, hashed, subjectID, name, nil)
}

func seedSubjectAPITokenWithPermissions(t *testing.T, svc *coredata.Services, hashed, subjectID, name string, permissions []core.AccessPermission) {
	t.Helper()
	exp := time.Now().Add(24 * time.Hour)
	if err := svc.APITokens.StoreAPIToken(context.Background(), &core.APIToken{
		ID:                  "api-tok-" + strings.ReplaceAll(subjectID, ":", "-"),
		OwnerKind:           core.APITokenOwnerKindSubject,
		OwnerID:             subjectID,
		CredentialSubjectID: subjectID,
		Name:                name,
		HashedToken:         hashed,
		ExpiresAt:           &exp,
		Permissions:         cloneAccessPermissionsForTest(permissions),
	}); err != nil {
		t.Fatalf("seedSubjectAPIToken: StoreAPIToken: %v", err)
	}
}

func cloneAccessPermissionsForTest(src []core.AccessPermission) []core.AccessPermission {
	if len(src) == 0 {
		return nil
	}
	out := append([]core.AccessPermission(nil), src...)
	for i := range out {
		out[i].Operations = append([]string(nil), out[i].Operations...)
		out[i].Actions = append([]string(nil), out[i].Actions...)
	}
	return out
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

func staticPolicyUserMember(t *testing.T, svc *coredata.Services, email, role string) config.SubjectPolicyMemberDef {
	t.Helper()
	return config.SubjectPolicyMemberDef{
		SubjectID: principal.UserSubjectID(seedUser(t, svc, email).ID),
		Role:      role,
	}
}

func seedUserRecord(t *testing.T, svc *coredata.Services, id, email string, createdAt time.Time) *core.User {
	t.Helper()
	ctx := context.Background()
	rec := indexeddb.Record{
		"id":               id,
		"email":            email,
		"normalized_email": strings.ToLower(strings.TrimSpace(email)),
		"display_name":     "",
		"created_at":       createdAt,
		"updated_at":       createdAt,
	}
	if err := svc.DB.ObjectStore(coredata.StoreUsers).Add(ctx, rec); err != nil {
		t.Fatalf("seedUserRecord: %v", err)
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
	resolvedConnection := config.ResolveConnectionAlias(connection)
	if resolvedConnection == "" {
		resolvedConnection = config.PluginConnectionName
	}
	seedToken(t, svc, &core.ExternalCredential{
		ID:           integration + "-" + connection + "-" + instance,
		SubjectID:    subjectID,
		ConnectionID: integration + ":" + resolvedConnection,
		Integration:  integration,
		Connection:   connection,
		Instance:     instance,
		AccessToken:  accessToken,
	})
}

func mustAuthorizer(t *testing.T, cfg config.AuthorizationConfig, pluginDefs map[string]*config.ProviderEntry) *authorization.Authorizer {
	t.Helper()
	authz, err := newTestAuthorizer(cfg, pluginDefs)
	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}
	return authz
}

func newTestAuthorizer(cfg config.AuthorizationConfig, pluginDefs map[string]*config.ProviderEntry) (*authorization.Authorizer, error) {
	return authorization.New(config.AuthorizationStaticConfig(cfg, pluginDefs))
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

func seedToken(t *testing.T, svc *coredata.Services, tok *core.ExternalCredential) {
	t.Helper()
	ctx := context.Background()
	if err := svc.ExternalCredentials.PutCredential(ctx, tok); err != nil {
		t.Fatalf("seedToken: %v", err)
	}
}

func testPluginDefsForConnections(plugin string, connections ...string) map[string]*config.ProviderEntry {
	entry := &config.ProviderEntry{}
	for _, connection := range connections {
		connection = config.ResolveConnectionAlias(connection)
		if connection == "" || connection == config.PluginConnectionName {
			continue
		}
		if entry.Connections == nil {
			entry.Connections = map[string]*config.ConnectionDef{}
		}
		entry.Connections[connection] = &config.ConnectionDef{
			ConnectionID: plugin + ":" + connection,
			Mode:         providermanifestv1.ConnectionModeUser,
		}
	}
	return map[string]*config.ProviderEntry{plugin: entry}
}

func TestNewServerRequiresStateSecretWithAuth(t *testing.T) {
	t.Parallel()
	svc := testutil.NewStubServices(t)
	providers := func() *registry.ProviderMap[core.Provider] {
		reg := registry.New()
		return &reg.Providers
	}()
	_, err := server.New(server.Config{
		Auth:      &coretesting.StubAuthProvider{N: "google"},
		Services:  svc,
		Providers: providers,
		Invoker:   invocation.NewBroker(providers, svc.Users, svc.ExternalCredentials),
	})
	if err == nil {
		t.Fatal("expected error when auth is enabled without state secret")
	}
	if !strings.Contains(err.Error(), "state secret is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewServerRequiresExternalCredentialsProvider(t *testing.T) {
	t.Parallel()

	svc, err := coredata.New(&coretesting.StubIndexedDB{})
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	providers := func() *registry.ProviderMap[core.Provider] {
		reg := registry.New()
		return &reg.Providers
	}()

	_, err = server.New(server.Config{
		Auth:        &coretesting.StubAuthProvider{N: "none"},
		Services:    svc,
		Providers:   providers,
		Invoker:     invocation.NewBroker(providers, svc.Users, nil),
		StateSecret: []byte("0123456789abcdef0123456789abcdef"),
	})
	if err == nil {
		t.Fatal("expected error when external credentials provider is missing")
	}
	if !strings.Contains(err.Error(), "external credentials provider is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewServerAdminAuthorizationRequiresValidSplitBaseURLs(t *testing.T) {
	t.Parallel()

	makeConfig := func() server.Config {
		svc := testutil.NewStubServices(t)
		reg := registry.New()
		return server.Config{
			Auth:      &coretesting.StubAuthProvider{N: "google"},
			Services:  svc,
			Providers: &reg.Providers,
			Invoker:   invocation.NewBroker(&reg.Providers, svc.Users, svc.ExternalCredentials),
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

	svc := testutil.NewStubServices(t)
	viewer := seedUser(t, svc, "viewer@example.test")
	baseAuthz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.SubjectPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(viewer.ID), Role: "viewer"},
				},
			},
		},
	}, map[string]*config.ProviderEntry{
		"sample_portal": {AuthorizationPolicy: "sample_policy"},
	})

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
		cfg.Authorizer = baseAuthz
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

	svc := testutil.NewStubServices(t)
	viewer := seedUser(t, svc, "viewer@example.test")
	baseAuthz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.SubjectPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(viewer.ID), Role: "viewer"},
				},
			},
		},
	}, map[string]*config.ProviderEntry{
		"sample_portal": {AuthorizationPolicy: "sample_policy"},
	})

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
		cfg.Authorizer = baseAuthz
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

	svc := testutil.NewStubServices(t)
	baseAuthz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"sample_policy": {
				Default: "allow",
				Members: []config.SubjectPolicyMemberDef{
					staticPolicyUserMember(t, svc, "admin@example.test", "admin"),
				},
			},
		},
	}, map[string]*config.ProviderEntry{
		"sample_portal": {AuthorizationPolicy: "sample_policy"},
	})

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
		cfg.Authorizer = baseAuthz
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

	svc := testutil.NewStubServices(t)
	adminUser := seedUser(t, svc, "admin@example.test")
	provider := newMemoryAuthorizationProvider("memory-authorization")
	baseAuthz, err := newTestAuthorizer(config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.SubjectPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(adminUser.ID), Role: "admin"},
				},
			},
		},
	}, map[string]*config.ProviderEntry{
		"sample_portal": {AuthorizationPolicy: "sample_policy"},
	})

	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}
	authz := mustProviderBackedAuthorizer(t, baseAuthz, provider)
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

	svc := testutil.NewStubServices(t)
	baseAuthz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"sample_policy": {
				Default: "allow",
				Members: []config.SubjectPolicyMemberDef{
					staticPolicyUserMember(t, svc, "admin@example.test", "admin"),
				},
			},
		},
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
		cfg.Authorizer = baseAuthz
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

	svc := testutil.NewStubServices(t)
	viewer := seedUser(t, svc, "viewer@example.test")
	admin := seedUser(t, svc, "admin@example.test")
	baseAuthz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"admin_policy": {
				Default: "deny",
				Members: []config.SubjectPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(viewer.ID), Role: "viewer"},
					{SubjectID: principal.UserSubjectID(admin.ID), Role: "admin"},
				},
			},
		},
	}, nil)

	provider := newMemoryAuthorizationProvider("memory-authorization")
	authz := mustProviderBackedAuthorizer(t, baseAuthz, provider)
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

	svc := testutil.NewStubServices(t)
	viewer := seedUser(t, svc, "viewer@example.test")
	admin := seedUser(t, svc, "admin@example.test")
	baseAuthz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"admin_policy": {
				Default: "deny",
				Members: []config.SubjectPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(viewer.ID), Role: "viewer"},
					{SubjectID: principal.UserSubjectID(admin.ID), Role: "admin"},
				},
			},
		},
	}, nil)

	provider := newMemoryAuthorizationProvider("memory-authorization")
	authz := mustProviderBackedAuthorizer(t, baseAuthz, provider)
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

	const loginBasePath = "/api/v1/auth/login"

	uiDir := t.TempDir()
	writeTestUIAsset(t, filepath.Join(uiDir, "index.html"), "<html>root-ui-shell</html>")
	writeTestAdminShell(t, uiDir, "protected-admin-shell")

	secret := []byte("0123456789abcdef0123456789abcdef")
	auth := &stubHostIssuedSessionAuth{secret: secret}
	svc := testutil.NewStubServices(t)
	baseAuthz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"admin_policy": {
				Default: "deny",
				Members: []config.SubjectPolicyMemberDef{
					staticPolicyUserMember(t, svc, "host@example.com", "admin"),
				},
			},
		},
	}, nil)

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
		cfg.Authorizer = baseAuthz
		cfg.PublicBaseURL = publicURL
		cfg.ManagementBaseURL = managementURL
		cfg.RouteProfile = server.RouteProfilePublic
		cfg.Admin = server.AdminRouteConfig{
			AuthorizationPolicy: "admin_policy",
		}
		cfg.ProviderUIs = map[string]*config.UIEntry{
			"root": {
				Path: "/",
				ProviderEntry: config.ProviderEntry{
					ResolvedAssetRoot: uiDir,
				},
			},
		}
		cfg.BuiltinAdminUI = &server.BuiltinAdminUIOptions{
			BrandHref: "/",
			LoginBase: loginBasePath,
		}
	}))
	publicTS.Listener = publicListener
	publicTS.StartTLS()
	testutil.CloseOnCleanup(t, publicTS)

	managementTS := httptest.NewUnstartedServer(newTestHandler(t, func(cfg *server.Config) {
		cfg.Auth = auth
		cfg.StateSecret = secret
		cfg.Services = svc
		cfg.Authorizer = baseAuthz
		cfg.PublicBaseURL = publicURL
		cfg.ManagementBaseURL = managementURL
		cfg.RouteProfile = server.RouteProfileManagement
		cfg.Admin = server.AdminRouteConfig{
			AuthorizationPolicy: "admin_policy",
		}
		cfg.ProviderUIs = map[string]*config.UIEntry{
			"root": {
				Path: "/",
				ProviderEntry: config.ProviderEntry{
					ResolvedAssetRoot: uiDir,
				},
			},
		}
		cfg.BuiltinAdminUI = &server.BuiltinAdminUIOptions{
			BrandHref: "/admin/",
			LoginBase: publicURL + loginBasePath,
		}
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
	text := string(body)
	if !strings.Contains(text, "protected-admin-shell") {
		t.Fatalf("body = %q, want protected admin shell", body)
	}
	if !strings.Contains(text, fmt.Sprintf(`window.__gestaltAdminShell.loginBase = %q;`, publicURL+loginBasePath)) {
		t.Fatalf("body = %q, want injected management login base", body)
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

func TestBuiltInAdminRoute_ProviderBackedAdminUIAutoDiscoversRootUI(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	writeTestUIAsset(t, filepath.Join(rootDir, "index.html"), "<html>root-ui-shell</html>")
	writeTestAdminShell(t, rootDir, "provider-admin-shell")
	writeTestUIAsset(t, filepath.Join(rootDir, "admin", "theme.css"), "body{background:#123456;}")

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.ProviderUIs = map[string]*config.UIEntry{
			"root": {
				Path: "/",
				ProviderEntry: config.ProviderEntry{
					ResolvedAssetRoot: rootDir,
				},
			},
		}
		cfg.BuiltinAdminUI = &server.BuiltinAdminUIOptions{
			BrandHref: "/workplace/",
			LoginBase: "https://login.example.test/start",
		}
	})
	testutil.CloseOnCleanup(t, ts)

	rootResp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET root ui: %v", err)
	}
	rootBody, err := io.ReadAll(rootResp.Body)
	_ = rootResp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll root ui: %v", err)
	}
	if rootResp.StatusCode != http.StatusOK {
		t.Fatalf("root ui status = %d, want 200", rootResp.StatusCode)
	}
	if !strings.Contains(string(rootBody), "root-ui-shell") {
		t.Fatalf("root ui body = %q, want root ui shell", rootBody)
	}

	adminResp, err := http.Get(ts.URL + "/admin/?tab=members")
	if err != nil {
		t.Fatalf("GET provider-backed admin ui: %v", err)
	}
	adminBody, err := io.ReadAll(adminResp.Body)
	_ = adminResp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll provider-backed admin ui: %v", err)
	}
	if adminResp.StatusCode != http.StatusOK {
		t.Fatalf("provider-backed admin ui status = %d, want 200", adminResp.StatusCode)
	}

	text := string(adminBody)
	for _, want := range []string{
		"provider-admin-shell",
		`<a class="brand" href="/workplace/">Gestalt</a>`,
		`window.__gestaltAdminShell.loginBase = "https://login.example.test/start";`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("provider-backed admin ui body missing %q", want)
		}
	}
	if strings.Contains(text, `<a href="/">Client UI</a>`) {
		t.Fatalf("provider-backed admin ui body still contains client ui link")
	}

	assetResp, err := http.Get(ts.URL + "/admin/theme.css")
	if err != nil {
		t.Fatalf("GET provider-backed admin asset: %v", err)
	}
	assetBody, err := io.ReadAll(assetResp.Body)
	_ = assetResp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll provider-backed admin asset: %v", err)
	}
	if assetResp.StatusCode != http.StatusOK {
		t.Fatalf("provider-backed admin asset status = %d, want 200", assetResp.StatusCode)
	}
	if !strings.Contains(string(assetBody), "background:#123456") {
		t.Fatalf("provider-backed admin asset body = %q, want provider stylesheet", assetBody)
	}
}

func TestBuiltInAdminRoute_ProviderBackedAdminUIUsesExplicitProvider(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	writeTestUIAsset(t, filepath.Join(rootDir, "index.html"), "<html>root-ui-shell</html>")

	adminProviderDir := t.TempDir()
	writeTestUIAsset(t, filepath.Join(adminProviderDir, "index.html"), "<html>admin-provider-root</html>")
	writeTestAdminShell(t, adminProviderDir, "explicit-provider-admin-shell")

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.ProviderUIs = map[string]*config.UIEntry{
			"root": {
				Path: "/",
				ProviderEntry: config.ProviderEntry{
					ResolvedAssetRoot: rootDir,
				},
			},
			"admin": {
				ProviderEntry: config.ProviderEntry{
					ResolvedAssetRoot: adminProviderDir,
				},
			},
		}
		cfg.AdminUIProvider = "admin"
		cfg.BuiltinAdminUI = &server.BuiltinAdminUIOptions{
			BrandHref: "/",
			LoginBase: "/api/v1/auth/login",
		}
	})
	testutil.CloseOnCleanup(t, ts)

	rootResp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET root ui: %v", err)
	}
	rootBody, err := io.ReadAll(rootResp.Body)
	_ = rootResp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll root ui: %v", err)
	}
	if rootResp.StatusCode != http.StatusOK {
		t.Fatalf("root ui status = %d, want 200", rootResp.StatusCode)
	}
	if !strings.Contains(string(rootBody), "root-ui-shell") {
		t.Fatalf("root ui body = %q, want root ui shell", rootBody)
	}

	adminResp, err := http.Get(ts.URL + "/admin/")
	if err != nil {
		t.Fatalf("GET explicit provider admin ui: %v", err)
	}
	adminBody, err := io.ReadAll(adminResp.Body)
	_ = adminResp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll explicit provider admin ui: %v", err)
	}
	if adminResp.StatusCode != http.StatusOK {
		t.Fatalf("explicit provider admin ui status = %d, want 200", adminResp.StatusCode)
	}
	text := string(adminBody)
	if !strings.Contains(text, "explicit-provider-admin-shell") {
		t.Fatalf("explicit provider admin ui body = %q, want explicit provider shell", adminBody)
	}
	if strings.Contains(text, "root-ui-shell") {
		t.Fatalf("explicit provider admin ui body = %q, should not use root ui shell", adminBody)
	}
}

func TestBuiltInAdminRoute_ProviderBackedAdminUIDoesNotAutoDiscoverNonRootUI(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	writeTestUIAsset(t, filepath.Join(rootDir, "index.html"), "<html>root-ui-shell</html>")

	portalDir := t.TempDir()
	writeTestUIAsset(t, filepath.Join(portalDir, "index.html"), "<html>portal-ui-shell</html>")
	writeTestAdminShell(t, portalDir, "portal-admin-shell")

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.ProviderUIs = map[string]*config.UIEntry{
			"root": {
				Path: "/",
				ProviderEntry: config.ProviderEntry{
					ResolvedAssetRoot: rootDir,
				},
			},
			"portal": {
				Path: "/portal",
				ProviderEntry: config.ProviderEntry{
					ResolvedAssetRoot: portalDir,
				},
			},
		}
		cfg.BuiltinAdminUI = &server.BuiltinAdminUIOptions{
			BrandHref: "/",
			LoginBase: "/api/v1/auth/login",
		}
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/admin/?tab=members")
	if err != nil {
		t.Fatalf("GET admin ui fallback: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll admin ui fallback: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin ui fallback status = %d, want 200", resp.StatusCode)
	}
	text := string(body)
	if strings.Contains(text, "portal-admin-shell") {
		t.Fatalf("admin ui fallback body = %q, should not auto-discover non-root ui", body)
	}
	if !strings.Contains(text, "Control surface") {
		t.Fatalf("admin ui fallback body = %q, want built-in admin shell", body)
	}
}

func TestAdminAPI_HumanAuthorization(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	viewer := seedUser(t, svc, "viewer@example.test")
	admin := seedUser(t, svc, "admin@example.test")
	baseAuthz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"admin_policy": {
				Default: "deny",
				Members: []config.SubjectPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(viewer.ID), Role: "viewer"},
					{SubjectID: principal.UserSubjectID(admin.ID), Role: "admin"},
				},
			},
			"sample_policy": {Default: "deny"},
		},
	}, map[string]*config.ProviderEntry{
		"sample_plugin": {AuthorizationPolicy: "sample_policy"},
	})

	provider := newMemoryAuthorizationProvider("memory-authorization")
	authz := mustProviderBackedAuthorizer(t, baseAuthz, provider)
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
					},
					Effective: bootstrap.RuntimeBehavior{
						CanHostPlugins:    true,
						HostServiceAccess: bootstrap.RuntimeHostServiceAccessRelay,
						EgressMode:        bootstrap.RuntimeEgressModeCIDR,
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
	if advertised["canHostPlugins"] != true || advertised["hostServiceAccess"] != "none" || advertised["egressMode"] != "cidr" {
		t.Fatalf("runtime providers[1].profile.advertised = %#v", advertised)
	}
	if effective["canHostPlugins"] != true || effective["hostServiceAccess"] != "relay" || effective["egressMode"] != "cidr" {
		t.Fatalf("runtime providers[1].profile.effective = %#v", effective)
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
				},
				Effective: bootstrap.RuntimeBehavior{
					CanHostPlugins:    true,
					HostServiceAccess: bootstrap.RuntimeHostServiceAccessRelay,
					EgressMode:        bootstrap.RuntimeEgressModeCIDR,
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

func TestAdminAPI_RuntimeProviderSessionLogs(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.April, 23, 12, 0, 0, 0, time.UTC)
	appendedAt := observedAt.Add(2 * time.Second)
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.PluginRuntimes = &staticRuntimeInspector{
			logs: []runtimelogs.Record{
				{
					Seq:        1,
					SourceSeq:  10,
					Stream:     runtimelogs.StreamRuntime,
					Message:    "runtime boot",
					ObservedAt: observedAt,
					AppendedAt: appendedAt,
				},
				{
					Seq:        2,
					SourceSeq:  11,
					Stream:     runtimelogs.StreamStdout,
					Message:    "hello\n",
					ObservedAt: observedAt.Add(time.Second),
					AppendedAt: appendedAt.Add(time.Second),
				},
				{
					Seq:        3,
					SourceSeq:  12,
					Stream:     runtimelogs.StreamStderr,
					Message:    "boom\n",
					ObservedAt: observedAt.Add(2 * time.Second),
					AppendedAt: appendedAt.Add(2 * time.Second),
				},
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/admin/api/v1/runtime/providers/modal/sessions/session-1/logs?after=1&limit=2")
	if err != nil {
		t.Fatalf("GET runtime provider session logs: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("runtime provider session logs status = %d, want 200: %s", resp.StatusCode, body)
	}

	var logs []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&logs); err != nil {
		t.Fatalf("decoding runtime provider session logs: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("runtime provider session logs len = %d, want 2", len(logs))
	}
	if got := logs[0]["seq"]; got != float64(2) {
		t.Fatalf("runtime provider session logs[0].seq = %v, want 2", got)
	}
	if got := logs[0]["stream"]; got != string(runtimelogs.StreamStdout) {
		t.Fatalf("runtime provider session logs[0].stream = %v, want stdout", got)
	}
	if got := logs[0]["message"]; got != "hello\n" {
		t.Fatalf("runtime provider session logs[0].message = %v, want %q", got, "hello\n")
	}
	if got := logs[1]["seq"]; got != float64(3) {
		t.Fatalf("runtime provider session logs[1].seq = %v, want 3", got)
	}
	if got := logs[1]["stream"]; got != string(runtimelogs.StreamStderr) {
		t.Fatalf("runtime provider session logs[1].stream = %v, want stderr", got)
	}
	if got := logs[1]["message"]; got != "boom\n" {
		t.Fatalf("runtime provider session logs[1].message = %v, want %q", got, "boom\n")
	}
	if _, ok := logs[0]["observedAt"]; !ok {
		t.Fatalf("runtime provider session logs[0].observedAt missing: %#v", logs[0])
	}
	if _, ok := logs[0]["appendedAt"]; !ok {
		t.Fatalf("runtime provider session logs[0].appendedAt missing: %#v", logs[0])
	}
}

func TestAdminAPI_RuntimeProviderSessionLogsRejectsInvalidCursorAndMapsNotFound(t *testing.T) {
	t.Parallel()

	t.Run("invalid after", func(t *testing.T) {
		t.Parallel()

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.PluginRuntimes = &staticRuntimeInspector{}
		})
		testutil.CloseOnCleanup(t, ts)

		resp, err := http.Get(ts.URL + "/admin/api/v1/runtime/providers/modal/sessions/session-1/logs?after=-1")
		if err != nil {
			t.Fatalf("GET runtime provider session logs: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusBadRequest {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("runtime provider session logs status = %d, want 400: %s", resp.StatusCode, body)
		}
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.PluginRuntimes = &staticRuntimeInspector{err: indexeddb.ErrNotFound}
		})
		testutil.CloseOnCleanup(t, ts)

		resp, err := http.Get(ts.URL + "/admin/api/v1/runtime/providers/modal/sessions/missing/logs")
		if err != nil {
			t.Fatalf("GET missing runtime provider session logs: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusNotFound {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("runtime provider session logs status = %d, want 404: %s", resp.StatusCode, body)
		}
	})
}

func TestAdminAPI_HumanAuthorizationOnManagementProfile(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	admin := seedUser(t, svc, "admin@example.test")
	baseAuthz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"admin_policy": {
				Default: "deny",
				Members: []config.SubjectPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(admin.ID), Role: "admin"},
				},
			},
			"sample_policy": {Default: "deny"},
		},
	}, map[string]*config.ProviderEntry{
		"sample_plugin": {AuthorizationPolicy: "sample_policy"},
	})

	provider := newMemoryAuthorizationProvider("memory-authorization")
	authz := mustProviderBackedAuthorizer(t, baseAuthz, provider)
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

	svc := testutil.NewStubServices(t)
	admin := seedUser(t, svc, "admin@example.test")
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"admin_policy": {
				Default: "deny",
				Members: []config.SubjectPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(admin.ID), Role: "admin"},
				},
			},
			"sample_policy": {Default: "deny"},
		},
	}, map[string]*config.ProviderEntry{
		"sample_plugin": {AuthorizationPolicy: "sample_policy"},
	})

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

	svc := testutil.NewStubServices(t)
	baseAuthz, err := newTestAuthorizer(config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.SubjectPolicyMemberDef{
					staticPolicyUserMember(t, svc, "static@example.test", "admin"),
				},
			},
		},
	}, map[string]*config.ProviderEntry{
		"sample_plugin": {AuthorizationPolicy: "sample_policy", MountPath: "/sample"},
	})

	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}
	provider := newMemoryAuthorizationProvider("memory-authorization")
	authz := mustProviderBackedAuthorizer(t, baseAuthz, provider)

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

	serviceAccountSubjectID := "service_account:reporting-bot"
	body = bytes.NewBufferString(fmt.Sprintf(`{"subjectId":%q,"role":"viewer"}`, serviceAccountSubjectID))
	req, _ = http.NewRequest(http.MethodPut, ts.URL+"/admin/api/v1/authorization/plugins/sample_plugin/members", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT service account member: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("put service account member status = %d, want 200: %s", resp.StatusCode, respBody)
	}
	var putServiceAccountMembershipResp struct {
		Membership struct {
			SelectorKind  string `json:"selectorKind"`
			SelectorValue string `json:"selectorValue"`
			Email         string `json:"email"`
			Role          string `json:"role"`
		} `json:"membership"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&putServiceAccountMembershipResp); err != nil {
		t.Fatalf("decode service account membership response: %v", err)
	}
	if putServiceAccountMembershipResp.Membership.SelectorKind != "subject_id" {
		t.Fatalf("service account membership selectorKind = %q, want subject_id", putServiceAccountMembershipResp.Membership.SelectorKind)
	}
	if putServiceAccountMembershipResp.Membership.SelectorValue != serviceAccountSubjectID {
		t.Fatalf("service account membership selectorValue = %q, want %q", putServiceAccountMembershipResp.Membership.SelectorValue, serviceAccountSubjectID)
	}
	if putServiceAccountMembershipResp.Membership.Email != "" {
		t.Fatalf("service account membership email = %q, want empty", putServiceAccountMembershipResp.Membership.Email)
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
	if len(members) != 3 {
		t.Fatalf("expected 3 merged members, got %d (%+v)", len(members), members)
	}
	foundDynamicPluginMember := false
	foundDynamicServiceAccountMember := false
	for _, member := range members {
		if member["source"] != "dynamic" {
			continue
		}
		switch member["selectorValue"] {
		case principal.UserSubjectID(dynamicUser.ID):
			foundDynamicPluginMember = true
			if got := member["selectorKind"]; got != "subject_id" {
				t.Fatalf("dynamic plugin member selectorKind = %v, want subject_id", got)
			}
			if member["email"] != dynamicEmail {
				t.Fatalf("dynamic plugin member email = %v, want %q", member["email"], dynamicEmail)
			}
		case serviceAccountSubjectID:
			foundDynamicServiceAccountMember = true
			if got := member["selectorKind"]; got != "subject_id" {
				t.Fatalf("dynamic service account member selectorKind = %v, want subject_id", got)
			}
			if member["email"] != nil {
				t.Fatalf("dynamic service account member email = %v, want omitted", member["email"])
			}
		}
	}
	if !foundDynamicPluginMember {
		t.Fatalf("expected one dynamic plugin member, got %+v", members)
	}
	if !foundDynamicServiceAccountMember {
		t.Fatalf("expected one dynamic service account member, got %+v", members)
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

	req, _ = http.NewRequest(http.MethodDelete, ts.URL+"/admin/api/v1/authorization/plugins/sample_plugin/members/"+url.PathEscape(serviceAccountSubjectID), nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE service account member: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("delete service account member status = %d, want 200: %s", resp.StatusCode, respBody)
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

func TestAuthorizationManagedSubjectsAPI(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	scopedToken, scopedHash, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	seedAPITokenWithPermissions(t, svc, scopedToken, scopedHash, "scoped-user", []core.AccessPermission{{
		Plugin:     "svc",
		Operations: []string{"run"},
	}})

	pluginDefs := map[string]*config.ProviderEntry{
		"discover-manual-svc": {AuthorizationPolicy: "discover_manual_policy"},
		"manual-svc":          {AuthorizationPolicy: "manual_policy"},
		"oauth-svc":           {AuthorizationPolicy: "oauth_policy"},
		"open-svc":            {AuthorizationPolicy: "open_policy"},
		"svc":                 {AuthorizationPolicy: "svc_policy"},
	}
	baseAuthz, err := newTestAuthorizer(config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"discover_manual_policy": {Default: "deny"},
			"manual_policy":          {Default: "deny"},
			"oauth_policy":           {Default: "deny"},
			"open_policy":            {Default: "allow"},
			"svc_policy":             {Default: "deny"},
		},
	}, pluginDefs)
	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}
	provider := newMemoryAuthorizationProvider("memory-authorization")
	authz := mustProviderBackedAuthorizer(t, baseAuthz, provider)
	ownerUser := seedProviderPluginAuthorization(t, svc, authz, provider, "svc", "owner@example.test", "editor")
	seedProviderPluginAuthorization(t, svc, authz, provider, "manual-svc", "owner@example.test", "viewer")
	seedProviderPluginAuthorization(t, svc, authz, provider, "discover-manual-svc", "owner@example.test", "viewer")
	seedProviderPluginAuthorization(t, svc, authz, provider, "oauth-svc", "owner@example.test", "viewer")
	ownerSubjectID := principal.UserSubjectID(ownerUser.ID)

	newStub := func(name string) *stubIntegrationWithSessionCatalog {
		return &stubIntegrationWithSessionCatalog{
			stubIntegrationWithOps: stubIntegrationWithOps{
				StubIntegration: coretesting.StubIntegration{
					N:        name,
					ConnMode: core.ConnectionModeNone,
					ExecuteFn: func(ctx context.Context, op string, _ map[string]any, _ string) (*core.OperationResult, error) {
						p := principal.FromContext(ctx)
						access := invocation.AccessContextFromContext(ctx)
						body, err := json.Marshal(map[string]string{
							"operation": op,
							"subject":   p.SubjectID,
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
				Name: name,
				Operations: []catalog.CatalogOperation{
					{ID: "run", Method: http.MethodGet, Transport: catalog.TransportREST, AllowedRoles: []string{"viewer"}},
					{ID: "admin", Method: http.MethodGet, Transport: catalog.TransportREST, AllowedRoles: []string{"admin"}},
				},
			},
		}
	}
	stub := newStub("svc")
	openStub := newStub("open-svc")
	var created struct {
		ID          string `json:"id"`
		SubjectID   string `json:"subjectId"`
		Kind        string `json:"kind"`
		DisplayName string `json:"displayName"`
	}
	assertManagedSubjectCredentialContext := func(label string) func(context.Context, *core.ExternalCredential) (map[string]string, error) {
		return func(ctx context.Context, token *core.ExternalCredential) (map[string]string, error) {
			p := principal.FromContext(ctx)
			if p == nil {
				return nil, fmt.Errorf("%s post-connect principal missing", label)
			}
			if p.SubjectID != ownerSubjectID {
				return nil, fmt.Errorf("%s post-connect subject = %q, want %q", label, p.SubjectID, ownerSubjectID)
			}
			if p.CredentialSubjectID != created.SubjectID {
				return nil, fmt.Errorf("%s post-connect credential subject = %q, want %q", label, p.CredentialSubjectID, created.SubjectID)
			}
			if token.SubjectID != created.SubjectID {
				return nil, fmt.Errorf("%s stored subject = %q, want %q", label, token.SubjectID, created.SubjectID)
			}
			return nil, nil
		}
	}
	managedDiscoverySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"id":"site-a","name":"Site A","workspace":"alpha"},{"id":"site-b","name":"Site B","workspace":"beta"}]`)
	}))
	testutil.CloseOnCleanup(t, managedDiscoverySrv)
	manualStub := &stubPostConnectManualProvider{
		stubManualProvider: stubManualProvider{
			StubIntegration: coretesting.StubIntegration{N: "manual-svc", DN: "Manual Service"},
		},
		postConnect: assertManagedSubjectCredentialContext("manual"),
	}
	discoverManualStub := &stubDiscoveringManualProvider{
		stubManualProvider: stubManualProvider{
			StubIntegration: coretesting.StubIntegration{N: "discover-manual-svc", DN: "Discover Manual Service"},
		},
		discovery: &core.DiscoveryConfig{
			URL:      managedDiscoverySrv.URL,
			IDPath:   "id",
			NamePath: "name",
			Metadata: map[string]string{"workspace": "workspace"},
		},
		postConnect: assertManagedSubjectCredentialContext("discovery"),
	}
	oauthStub := &stubIntegrationWithAuthURL{
		StubIntegration: coretesting.StubIntegration{N: "oauth-svc", DN: "OAuth Service"},
		authURL:         "https://oauth.example/authorize",
		postConnect:     assertManagedSubjectCredentialContext("oauth"),
	}
	oauthHandler := &testOAuthHandler{
		authorizationBaseURLVal: "https://oauth.example/authorize",
		exchangeCodeFn: func(_ context.Context, code string) (*core.TokenResponse, error) {
			if code != "good-code" {
				return nil, fmt.Errorf("bad code")
			}
			return &core.TokenResponse{AccessToken: "oauth-upstream-token"}, nil
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				switch token {
				case "owner-session":
					return &core.UserIdentity{Email: "owner@example.test"}, nil
				case "viewer-session":
					return &core.UserIdentity{Email: "viewer@example.test"}, nil
				case "blocked-session":
					return &core.UserIdentity{Email: "blocked@example.test"}, nil
				default:
					return nil, fmt.Errorf("invalid token")
				}
			},
		}
		cfg.Providers = testutil.NewProviderRegistry(t, stub, openStub, manualStub, discoverManualStub, oauthStub)
		cfg.DefaultConnection = map[string]string{
			"manual-svc":          config.PluginConnectionName,
			"discover-manual-svc": config.PluginConnectionName,
			"oauth-svc":           testDefaultConnection,
		}
		cfg.ConnectionAuth = testConnectionAuth("oauth-svc", oauthHandler)
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.AuthorizationProvider = provider
		cfg.PluginDefs = pluginDefs
	})
	testutil.CloseOnCleanup(t, ts)

	doJSON := func(method, path, session, body string) *http.Response {
		t.Helper()
		var reader io.Reader
		if body != "" {
			reader = bytes.NewBufferString(body)
		}
		req, _ := http.NewRequest(method, ts.URL+path, reader)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if session != "" {
			req.AddCookie(&http.Cookie{Name: "session_token", Value: session})
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		return resp
	}
	expectJSONStatus := func(method, path, session, body string, want int) {
		t.Helper()
		resp := doJSON(method, path, session, body)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != want {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d, want %d: %s", resp.StatusCode, want, body)
		}
	}

	expectJSONStatus(http.MethodPost, "/api/v1/authorization/subjects", "owner-session", `{"subjectId":"user:not-a-service-account","displayName":"bad"}`, http.StatusBadRequest)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/authorization/subjects", bytes.NewBufferString(`{"id":"scoped-token-bot"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+scopedToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create with scoped API token: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("scoped API token create status = %d, want 403: %s", resp.StatusCode, body)
	}
	_ = resp.Body.Close()

	resp = doJSON(http.MethodPost, "/api/v1/authorization/subjects", "owner-session", `{"id":"reporting-bot","displayName":"Reporting Bot","description":"Nightly reports"}`)
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("create subject status = %d, want 201: %s", resp.StatusCode, body)
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("decode created managed subject: %v", err)
	}
	_ = resp.Body.Close()
	if created.ID != "reporting-bot" || created.SubjectID != "service_account:reporting-bot" || created.Kind != "service_account" {
		t.Fatalf("created managed subject = %+v", created)
	}
	if err := svc.ExternalCredentials.PutCredential(context.Background(), &core.ExternalCredential{
		ID:          "managed-subject-credential",
		SubjectID:   created.SubjectID,
		Integration: "svc",
		Connection:  "default",
		Instance:    "default",
		AccessToken: "upstream-token",
	}); err != nil {
		t.Fatalf("seed managed subject credential: %v", err)
	}

	escapedSubjectID := url.PathEscape(created.SubjectID)
	expectJSONStatus(http.MethodGet, "/api/v1/authorization/subjects/"+escapedSubjectID, "owner-session", "", http.StatusOK)

	expectJSONStatus(http.MethodPut, "/api/v1/authorization/subjects/"+escapedSubjectID+"/members", "owner-session", `{"email":"viewer@example.test","role":"viewer"}`, http.StatusOK)

	expectJSONStatus(http.MethodGet, "/api/v1/authorization/subjects/"+escapedSubjectID, "viewer-session", "", http.StatusOK)

	expectJSONStatus(http.MethodPost, "/api/v1/authorization/subjects/"+escapedSubjectID+"/tokens", "viewer-session", `{"name":"viewer-token"}`, http.StatusForbidden)

	expectJSONStatus(http.MethodPost, "/api/v1/authorization/subjects/"+escapedSubjectID+"/auth/connect-manual", "viewer-session", `{"integration":"manual-svc","credential":"viewer-key"}`, http.StatusForbidden)

	resp = doJSON(http.MethodPost, "/api/v1/authorization/subjects/"+escapedSubjectID+"/auth/connect-manual", "owner-session", `{"integration":"manual-svc","credential":"managed-key"}`)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("managed subject manual connect status = %d, want 200: %s", resp.StatusCode, body)
	}
	var connectResp struct {
		Status      string `json:"status"`
		Integration string `json:"integration"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&connectResp); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("decode managed subject manual connect: %v", err)
	}
	_ = resp.Body.Close()
	if connectResp.Status != "connected" || connectResp.Integration != "manual-svc" {
		t.Fatalf("manual connect response = %+v", connectResp)
	}
	managedCredentials, err := listTestCredentialsForProvider(context.Background(), svc.ExternalCredentials, created.SubjectID, "manual-svc")
	if err != nil {
		t.Fatalf("list managed subject manual credentials: %v", err)
	}
	if len(managedCredentials) != 1 || managedCredentials[0].AccessToken != "managed-key" {
		t.Fatalf("managed subject manual credentials = %+v", managedCredentials)
	}

	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp = doJSON(http.MethodPost, "/api/v1/authorization/subjects/"+escapedSubjectID+"/auth/connect-manual", "owner-session", `{"integration":"discover-manual-svc","credential":"managed-discovery-key"}`)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("managed subject discovery connect status = %d, want 200: %s", resp.StatusCode, body)
	}
	var discoveryConnectResp struct {
		Status       string `json:"status"`
		Integration  string `json:"integration"`
		SelectionURL string `json:"selectionUrl"`
		PendingToken string `json:"pendingToken"`
		Candidates   []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"candidates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&discoveryConnectResp); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("decode managed subject discovery connect: %v", err)
	}
	_ = resp.Body.Close()
	if discoveryConnectResp.Status != "selection_required" || discoveryConnectResp.Integration != "discover-manual-svc" || discoveryConnectResp.SelectionURL != "/api/v1/auth/pending-connection" || discoveryConnectResp.PendingToken == "" || len(discoveryConnectResp.Candidates) != 2 {
		t.Fatalf("managed subject discovery connect response = %+v", discoveryConnectResp)
	}

	discoveryForm := url.Values{
		"pending_token":   {discoveryConnectResp.PendingToken},
		"candidate_index": {"1"},
	}
	req, _ = http.NewRequest(http.MethodPost, ts.URL+discoveryConnectResp.SelectionURL, strings.NewReader(discoveryForm.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err = noRedirect.Do(req)
	if err != nil {
		t.Fatalf("managed subject discovery unauthenticated select: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("managed subject discovery unauthenticated select status = %d, want 401: %s", resp.StatusCode, body)
	}
	_ = resp.Body.Close()

	req, _ = http.NewRequest(http.MethodPost, ts.URL+discoveryConnectResp.SelectionURL, strings.NewReader(discoveryForm.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "owner-session"})
	resp, err = noRedirect.Do(req)
	if err != nil {
		t.Fatalf("managed subject discovery select: %v", err)
	}
	if resp.StatusCode != http.StatusSeeOther {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("managed subject discovery select status = %d, want 303: %s", resp.StatusCode, body)
	}
	_ = resp.Body.Close()
	managedCredentials, err = listTestCredentialsForProvider(context.Background(), svc.ExternalCredentials, created.SubjectID, "discover-manual-svc")
	if err != nil {
		t.Fatalf("list managed subject discovery credentials: %v", err)
	}
	if len(managedCredentials) != 1 || managedCredentials[0].AccessToken != "managed-discovery-key" {
		t.Fatalf("managed subject discovery credentials = %+v", managedCredentials)
	}
	var discoveryMetadata map[string]string
	if err := json.Unmarshal([]byte(managedCredentials[0].MetadataJSON), &discoveryMetadata); err != nil {
		t.Fatalf("unmarshal managed subject discovery metadata: %v", err)
	}
	if discoveryMetadata["workspace"] != "beta" {
		t.Fatalf("managed subject discovery workspace = %q, want beta", discoveryMetadata["workspace"])
	}

	resp = doJSON(http.MethodGet, "/api/v1/authorization/subjects/"+escapedSubjectID+"/integrations", "owner-session", "")
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("managed subject integrations status = %d, want 200: %s", resp.StatusCode, body)
	}
	var integrations []struct {
		Name            string `json:"name"`
		Status          string `json:"status"`
		CredentialState string `json:"credentialState"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&integrations); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("decode managed subject integrations: %v", err)
	}
	_ = resp.Body.Close()
	var manualIntegration *struct {
		Name            string `json:"name"`
		Status          string `json:"status"`
		CredentialState string `json:"credentialState"`
	}
	for i := range integrations {
		if integrations[i].Name == "manual-svc" {
			manualIntegration = &integrations[i]
			break
		}
	}
	if manualIntegration == nil {
		t.Fatalf("manual-svc missing from managed subject integrations: %+v", integrations)
	}
	if manualIntegration.Status != "ready" || manualIntegration.CredentialState != "connected" {
		t.Fatalf("manual-svc status = %+v, want connected ready", *manualIntegration)
	}

	resp = doJSON(http.MethodPost, "/api/v1/authorization/subjects/"+escapedSubjectID+"/auth/start-oauth", "owner-session", `{"integration":"oauth-svc","scopes":["read"]}`)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("managed subject oauth start status = %d, want 200: %s", resp.StatusCode, body)
	}
	var oauthStart struct {
		URL   string `json:"url"`
		State string `json:"state"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&oauthStart); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("decode managed subject oauth start: %v", err)
	}
	_ = resp.Body.Close()
	if oauthStart.State == "" || !strings.Contains(oauthStart.URL, url.QueryEscape(oauthStart.State)) {
		t.Fatalf("oauth start response = %+v", oauthStart)
	}
	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/v1/auth/callback?code=good-code&state="+url.QueryEscape(oauthStart.State), nil)
	resp, err = noRedirect.Do(req)
	if err != nil {
		t.Fatalf("managed subject oauth callback: %v", err)
	}
	if resp.StatusCode != http.StatusSeeOther {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("managed subject oauth callback status = %d, want 303: %s", resp.StatusCode, body)
	}
	_ = resp.Body.Close()
	managedCredentials, err = listTestCredentialsForProvider(context.Background(), svc.ExternalCredentials, created.SubjectID, "oauth-svc")
	if err != nil {
		t.Fatalf("list managed subject oauth credentials: %v", err)
	}
	if len(managedCredentials) != 1 || managedCredentials[0].AccessToken != "oauth-upstream-token" {
		t.Fatalf("managed subject oauth credentials = %+v", managedCredentials)
	}

	expectJSONStatus(http.MethodDelete, "/api/v1/authorization/subjects/"+escapedSubjectID+"/integrations/manual-svc", "viewer-session", "", http.StatusForbidden)

	resp = doJSON(http.MethodDelete, "/api/v1/authorization/subjects/"+escapedSubjectID+"/integrations/manual-svc", "owner-session", "")
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("managed subject disconnect status = %d, want 200: %s", resp.StatusCode, body)
	}
	_ = resp.Body.Close()
	managedCredentials, err = listTestCredentialsForProvider(context.Background(), svc.ExternalCredentials, created.SubjectID, "manual-svc")
	if err != nil {
		t.Fatalf("list managed subject manual credentials after disconnect: %v", err)
	}
	if len(managedCredentials) != 0 {
		t.Fatalf("managed subject manual credentials after disconnect = %+v, want none", managedCredentials)
	}

	expectJSONStatus(http.MethodPut, "/api/v1/authorization/subjects/"+escapedSubjectID+"/members", "owner-session", `{"email":"blocked@example.test","role":"admin"}`, http.StatusOK)

	expectJSONStatus(http.MethodPut, "/api/v1/authorization/subjects/"+escapedSubjectID+"/grants/svc", "blocked-session", `{"role":"viewer"}`, http.StatusForbidden)

	resp = doJSON(http.MethodPost, "/api/v1/authorization/subjects/"+escapedSubjectID+"/tokens", "owner-session", `{"name":"open-token","permissions":[{"plugin":"open-svc","operations":["run"]}]}`)
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("create open subject token status = %d, want 201: %s", resp.StatusCode, body)
	}
	var openTokenResp struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&openTokenResp); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("decode open token response: %v", err)
	}
	_ = resp.Body.Close()
	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/v1/open-svc/run", nil)
	req.Header.Set("Authorization", "Bearer "+openTokenResp.Token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("invoke default-allow plugin without managed subject grant: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("default-allow invocation without grant status = %d, want 403: %s", resp.StatusCode, body)
	}
	_ = resp.Body.Close()

	resp = doJSON(http.MethodPut, "/api/v1/authorization/subjects/"+escapedSubjectID+"/grants/svc", "owner-session", `{"role":"viewer"}`)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("grant subject status = %d, want 200: %s", resp.StatusCode, body)
	}
	var grant struct {
		Plugin  string `json:"plugin"`
		Role    string `json:"role"`
		Source  string `json:"source"`
		Mutable bool   `json:"mutable"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&grant); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("decode grant response: %v", err)
	}
	_ = resp.Body.Close()
	if grant.Plugin != "svc" || grant.Role != "viewer" || grant.Source != "dynamic" || !grant.Mutable {
		t.Fatalf("grant response = %+v", grant)
	}

	resp = doJSON(http.MethodPost, "/api/v1/authorization/subjects/"+escapedSubjectID+"/tokens", "owner-session", `{"name":"reporting-token","permissions":[{"plugin":"svc","operations":["run"]}]}`)
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("create subject token status = %d, want 201: %s", resp.StatusCode, body)
	}
	var tokenResp struct {
		ID          string                  `json:"id"`
		Token       string                  `json:"token"`
		Permissions []core.AccessPermission `json:"permissions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("decode token response: %v", err)
	}
	_ = resp.Body.Close()
	if tokenResp.ID == "" || tokenResp.Token == "" || len(tokenResp.Permissions) != 1 || tokenResp.Permissions[0].Plugin != "svc" {
		t.Fatalf("token response = %+v", tokenResp)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/v1/svc/run", nil)
	req.Header.Set("Authorization", "Bearer "+tokenResp.Token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("invoke with subject token: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("invoke status = %d, want 200: %s", resp.StatusCode, body)
	}
	var invokeResp map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&invokeResp); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("decode invoke response: %v", err)
	}
	_ = resp.Body.Close()
	if invokeResp["subject"] != created.SubjectID || invokeResp["role"] != "viewer" || invokeResp["operation"] != "run" {
		t.Fatalf("invoke response = %+v", invokeResp)
	}

	expectJSONStatus(http.MethodDelete, "/api/v1/authorization/subjects/"+escapedSubjectID, "owner-session", "", http.StatusOK)

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/v1/svc/run", nil)
	req.Header.Set("Authorization", "Bearer "+tokenResp.Token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("invoke after delete: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("invoke after delete status = %d, want 401: %s", resp.StatusCode, body)
	}
	_ = resp.Body.Close()

	rels, err := provider.ReadRelationships(context.Background(), &core.ReadRelationshipsRequest{
		Subject: &core.SubjectRef{Type: authorization.ProviderSubjectTypeSubject, Id: created.SubjectID},
	})
	if err != nil {
		t.Fatalf("read subject relationships after delete: %v", err)
	}
	if len(rels.GetRelationships()) != 0 {
		t.Fatalf("subject relationships after delete = %+v, want none", rels.GetRelationships())
	}
	rels, err = provider.ReadRelationships(context.Background(), &core.ReadRelationshipsRequest{
		Resource: &core.ResourceRef{Type: authorization.ProviderResourceTypeManagedSubject, Id: created.SubjectID},
	})
	if err != nil {
		t.Fatalf("read managed subject relationships after delete: %v", err)
	}
	if len(rels.GetRelationships()) != 0 {
		t.Fatalf("managed subject relationships after delete = %+v, want none", rels.GetRelationships())
	}
	credentials, err := svc.ExternalCredentials.ListCredentials(context.Background(), created.SubjectID)
	if err != nil {
		t.Fatalf("list credentials after delete: %v", err)
	}
	if len(credentials) != 0 {
		t.Fatalf("credentials after delete = %+v, want none", credentials)
	}

	expectJSONStatus(http.MethodPost, "/api/v1/authorization/subjects", "owner-session", `{"id":"reporting-bot","displayName":"Reporting Bot"}`, http.StatusConflict)
}

func TestAdminAPI_PluginAuthorizationProviderBackedReadsAndDebug(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	provider := newMemoryAuthorizationProvider("memory-authorization")
	baseAuthz, err := newTestAuthorizer(config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.SubjectPolicyMemberDef{
					staticPolicyUserMember(t, svc, "static@example.test", "admin"),
				},
			},
		},
	}, map[string]*config.ProviderEntry{
		"sample_plugin": {AuthorizationPolicy: "sample_policy", MountPath: "/sample"},
	})

	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}
	authz := mustProviderBackedAuthorizer(t, baseAuthz, provider)

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

	if err := authz.ReloadAuthorizationState(context.Background()); err != nil {
		t.Fatalf("ReloadAuthorizationState after provider-backed plugin write: %v", err)
	}
	provider.putRelationship(provider.activeModelID, &core.Relationship{
		Subject:  &core.SubjectRef{Type: authorization.ProviderSubjectTypeSubject, Id: "raw-subject"},
		Relation: "viewer",
		Resource: &core.ResourceRef{Type: authorization.ProviderResourceTypePluginDynamic, Id: "sample_plugin"},
	})

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
	if len(relationshipsResp.Relationships) != 2 {
		t.Fatalf("expected 2 provider relationships, got %d", len(relationshipsResp.Relationships))
	}
	for _, rel := range relationshipsResp.Relationships {
		if !rel.Managed {
			t.Fatalf("expected managed relationship rows, got %+v", relationshipsResp.Relationships)
		}
	}
}

func TestAdminAPI_AuthorizationProviderDebugRequiresAdminPolicy(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	provider := newMemoryAuthorizationProvider("memory-authorization")
	baseAuthz, err := newTestAuthorizer(config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.SubjectPolicyMemberDef{
					staticPolicyUserMember(t, svc, "static@example.test", "admin"),
				},
			},
		},
	}, map[string]*config.ProviderEntry{
		"sample_plugin": {AuthorizationPolicy: "sample_policy", MountPath: "/sample"},
	})

	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}
	authz := mustProviderBackedAuthorizer(t, baseAuthz, provider)

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

	svc := testutil.NewStubServices(t)
	seedUser(t, svc, "static-admin@example.test")
	const adminRole = "owner"
	baseAuthz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"admin_policy": {
				Default: "deny",
				Members: []config.SubjectPolicyMemberDef{
					staticPolicyUserMember(t, svc, "static-admin@example.test", adminRole),
				},
			},
		},
	}, nil)

	provider := newMemoryAuthorizationProvider("memory-authorization")
	authz := mustProviderBackedAuthorizer(t, baseAuthz, provider)

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
}

func TestAdminAPI_AdminAuthorizationProviderBackedReads(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	seedUser(t, svc, "static-admin@example.test")
	const adminRole = "owner"
	provider := newMemoryAuthorizationProvider("memory-authorization")
	baseAuthz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"admin_policy": {
				Default: "deny",
				Members: []config.SubjectPolicyMemberDef{
					staticPolicyUserMember(t, svc, "static-admin@example.test", adminRole),
				},
			},
		},
	}, nil)

	authz := mustProviderBackedAuthorizer(t, baseAuthz, provider)

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

func TestAdminAPI_ProviderBackedWritesUseAuthorizationProvider(t *testing.T) {
	t.Parallel()

	t.Run("plugin members", func(t *testing.T) {
		t.Parallel()

		svc := testutil.NewStubServices(t)
		provider := newMemoryAuthorizationProvider("memory-authorization")
		baseAuthz, err := newTestAuthorizer(config.AuthorizationConfig{
			Policies: map[string]config.SubjectPolicyDef{
				"sample_policy": {
					Default: "deny",
					Members: []config.SubjectPolicyMemberDef{
						staticPolicyUserMember(t, svc, "static@example.test", "admin"),
					},
				},
			},
		}, map[string]*config.ProviderEntry{
			"sample_plugin": {AuthorizationPolicy: "sample_policy", MountPath: "/sample"},
		})

		if err != nil {
			t.Fatalf("authorization.New: %v", err)
		}
		authz := mustProviderBackedAuthorizer(t, baseAuthz, provider)

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

		svc := testutil.NewStubServices(t)
		provider := newMemoryAuthorizationProvider("memory-authorization")
		seedUser(t, svc, "static-admin@example.test")
		baseAuthz := mustAuthorizer(t, config.AuthorizationConfig{
			Policies: map[string]config.SubjectPolicyDef{
				"admin_policy": {
					Default: "deny",
					Members: []config.SubjectPolicyMemberDef{
						staticPolicyUserMember(t, svc, "static-admin@example.test", "owner"),
					},
				},
			},
		}, nil)

		authz := mustProviderBackedAuthorizer(t, baseAuthz, provider)

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

	svc := testutil.NewStubServices(t)
	seedUser(t, svc, "static-admin@example.test")
	seedUser(t, svc, "viewer@example.test")
	const adminRole = "ops-admin"
	provider := newMemoryAuthorizationProvider("memory-authorization")
	baseAuthz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"admin_policy": {
				Default: "deny",
				Members: []config.SubjectPolicyMemberDef{
					staticPolicyUserMember(t, svc, "static-admin@example.test", adminRole),
				},
			},
		},
	}, nil)

	authz := mustProviderBackedAuthorizer(t, baseAuthz, provider)

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

	svc := testutil.NewStubServices(t)
	authz, err := newTestAuthorizer(config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.SubjectPolicyMemberDef{
					staticPolicyUserMember(t, svc, "admin@example.test", "admin"),
				},
			},
		},
	}, map[string]*config.ProviderEntry{
		"sample_plugin": {AuthorizationPolicy: "sample_policy", MountPath: "/sample"},
	})

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

		svc := testutil.NewStubServices(t)
		seedUser(t, svc, "admin@example.test")
		authz := mustAuthorizer(t, config.AuthorizationConfig{
			Policies: map[string]config.SubjectPolicyDef{
				"admin_policy": {
					Default: "deny",
					Members: []config.SubjectPolicyMemberDef{
						staticPolicyUserMember(t, svc, "admin@example.test", "admin"),
					},
				},
			},
		}, nil)

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

		svc := testutil.NewStubServices(t)
		authz := mustAuthorizer(t, config.AuthorizationConfig{}, nil)

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

	svc := testutil.NewStubServices(t)
	provider := newMemoryAuthorizationProvider("memory-authorization")
	baseAuthz, err := newTestAuthorizer(config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"sample_policy": {Default: "deny"},
		},
	}, map[string]*config.ProviderEntry{
		"sample_plugin": {AuthorizationPolicy: "sample_policy", MountPath: "/sample"},
	})

	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}
	authz := mustProviderBackedAuthorizer(t, baseAuthz, provider)
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

	svc := testutil.NewStubServices(t)
	seedUser(t, svc, "static-admin@example.test")
	provider := newMemoryAuthorizationProvider("memory-authorization")
	baseAuthz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"admin_policy": {
				Default: "deny",
				Members: []config.SubjectPolicyMemberDef{
					staticPolicyUserMember(t, svc, "static-admin@example.test", "admin"),
				},
			},
		},
	}, nil)

	authz := mustProviderBackedAuthorizer(t, baseAuthz, provider)
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
		svc := testutil.NewStubServices(t)
		authz := mustAuthorizer(t, config.AuthorizationConfig{
			Policies: map[string]config.SubjectPolicyDef{
				"sample_policy": {
					Default: "deny",
					Members: []config.SubjectPolicyMemberDef{
						staticPolicyUserMember(t, svc, "viewer@example.test", "viewer"),
					},
				},
			},
		}, map[string]*config.ProviderEntry{
			"sample_portal": {AuthorizationPolicy: "sample_policy"},
		})

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

	svc := testutil.NewStubServices(t)
	seedUser(t, svc, "static-admin@example.test")
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"admin_policy": {
				Default: "deny",
				Members: []config.SubjectPolicyMemberDef{
					staticPolicyUserMember(t, svc, "static-admin@example.test", "admin"),
				},
			},
			"sample_policy": {
				Default: "deny",
			},
		},
	}, nil)

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

	svc := testutil.NewStubServices(t)
	viewer := seedUser(t, svc, "viewer@example.test")
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.SubjectPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(viewer.ID), Role: "viewer"},
				},
			},
		},
	}, map[string]*config.ProviderEntry{
		"sample_portal": {AuthorizationPolicy: "sample_policy"},
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

	svc := testutil.NewStubServices(t)
	viewer := seedUser(t, svc, "viewer@example.test")
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.SubjectPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(viewer.ID), Role: "viewer"},
				},
			},
		},
	}, map[string]*config.ProviderEntry{
		"sample_portal": {AuthorizationPolicy: "sample_policy"},
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

	svc := testutil.NewStubServices(t)
	viewer := seedUser(t, svc, "viewer@example.test")
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.SubjectPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(viewer.ID), Role: "viewer"},
				},
			},
		},
	}, map[string]*config.ProviderEntry{
		"sample_portal": {AuthorizationPolicy: "sample_policy"},
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

	svc := testutil.NewStubServices(t)
	viewer := seedUser(t, svc, "viewer@example.test")
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.SubjectPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(viewer.ID), Role: "viewer"},
				},
			},
		},
	}, map[string]*config.ProviderEntry{
		"sample_portal": {AuthorizationPolicy: "sample_policy"},
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

	svc := testutil.NewStubServices(t)
	viewer := seedUser(t, svc, "viewer@example.test")
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.SubjectPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(viewer.ID), Role: "viewer"},
				},
			},
		},
	}, map[string]*config.ProviderEntry{
		"sample_portal": {AuthorizationPolicy: "sample_policy"},
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

	svc := testutil.NewStubServices(t)
	viewer := seedUser(t, svc, "viewer@example.test")
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.SubjectPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(viewer.ID), Role: "viewer"},
				},
			},
		},
	}, map[string]*config.ProviderEntry{
		"sample_portal": {AuthorizationPolicy: "sample_policy"},
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

	resp, err = http.Get(ts.URL + "/api/v1/not-a-real-provider")
	if err != nil {
		t.Fatalf("GET unknown API route: %v", err)
	}
	body, err = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll unknown API route: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown API route status = %d, want 404", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("unknown API route content-type = %q, want application/json", ct)
	}
	if strings.Contains(string(body), "root-shell") {
		t.Fatalf("unknown API route unexpectedly served root UI: %q", body)
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

func writeTestUIAsset(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

func writeTestAdminShell(t *testing.T, rootDir, marker string) {
	t.Helper()
	writeTestUIAsset(t, filepath.Join(rootDir, "admin", "index.html"), fmt.Sprintf(`<!doctype html>
<html>
  <body>
    <a class="brand" href="/">Gestalt</a>
    <a href="/">Client UI</a>
    <section>%s</section>
    <script>
      window.__gestaltAdminShell = window.__gestaltAdminShell || {};
      (function () {
        try {
          window.__gestaltAdminShell.loginBase = __GESTALT_ADMIN_LOGIN_BASE__;
        } catch (error) {
          window.__gestaltAdminShell.loginBase = "/api/v1/auth/login";
        }
      })();
    </script>
  </body>
</html>`, marker))
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
		cfg.Services = testutil.NewStubServices(t)
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

func TestProviderDevAttachmentRoutesEnforceGateDispatcherSecretAndRedaction(t *testing.T) {
	t.Parallel()

	newManager := func(t *testing.T) *providerdev.Manager {
		t.Helper()
		manager, err := providerdev.NewManager([]providerdev.Target{{
			Name:   "roadmap",
			Source: "github.com/acme/plugins/roadmap",
			UIPath: "/roadmap",
			RuntimeEnv: func(string) (providerdev.RuntimeEnv, error) {
				return providerdev.RuntimeEnv{
					Env: map[string]string{"SECRET": "do-not-return"},
				}, nil
			},
		}})
		if err != nil {
			t.Fatalf("NewManager: %v", err)
		}
		return manager
	}
	auth := &coretesting.StubAuthProvider{
		N: "test",
		ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
			switch token {
			case "owner-session":
				return &core.UserIdentity{Email: "owner@example.test"}, nil
			case "other-session":
				return &core.UserIdentity{Email: "other@example.test"}, nil
			default:
				return nil, principal.ErrInvalidToken
			}
		},
	}
	createBody := []byte(`{"providers":[{"name":"roadmap","ui":true}]}`)

	disabledTS := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = auth
		cfg.ProviderDevSessions = newManager(t)
	})
	testutil.CloseOnCleanup(t, disabledTS)
	req, err := http.NewRequest(http.MethodPost, disabledTS.URL+providerdev.PathAttachments, bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("new disabled request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer owner-session")
	resp, err := disabledTS.Client().Do(req)
	if err != nil {
		t.Fatalf("disabled create request: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("disabled create status = %d, want 403", resp.StatusCode)
	}

	noAuthTS := newTestServer(t, func(cfg *server.Config) {
		cfg.ProviderDevAttach = true
		cfg.ProviderDevSessions = newManager(t)
	})
	testutil.CloseOnCleanup(t, noAuthTS)
	resp, err = noAuthTS.Client().Post(noAuthTS.URL+providerdev.PathAttachments, "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("no-auth create request: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("no-auth create status = %d, want 403", resp.StatusCode)
	}

	enabledTS := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = auth
		cfg.ProviderDevAttach = true
		cfg.ProviderDevSessions = newManager(t)
		cfg.PluginDefs = map[string]*config.ProviderEntry{
			"roadmap": {
				AuthorizationPolicy: "provider_devs",
				Dev: &config.ProviderEntryDevConfig{
					Attach: config.ProviderEntryDevAttachConfig{AllowedRoles: []string{"viewer"}},
				},
			},
		}
		cfg.Authorizer = mustAuthorizer(t, config.AuthorizationConfig{
			Policies: map[string]config.SubjectPolicyDef{
				"provider_devs": {Default: "allow"},
			},
		}, cfg.PluginDefs)
	})
	testutil.CloseOnCleanup(t, enabledTS)
	req, err = http.NewRequest(http.MethodPost, enabledTS.URL+providerdev.PathAttachments, bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("new enabled request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer owner-session")
	req.Header.Set("Content-Type", "application/json")
	resp, err = enabledTS.Client().Do(req)
	if err != nil {
		t.Fatalf("enabled create request: %v", err)
	}
	payload, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("read create response: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("enabled create status = %d, want 201; body = %s", resp.StatusCode, payload)
	}
	var created providerdev.CreateSessionResponse
	if err := json.Unmarshal(payload, &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.AttachID == "" || created.DispatcherSecret == "" {
		t.Fatalf("create response missing attachId or dispatcherSecret: %s", payload)
	}

	completeReq, err := http.NewRequest(http.MethodPost, enabledTS.URL+providerdev.PathAttachments+"/"+created.AttachID+"/calls/call-1", strings.NewReader("not-json"))
	if err != nil {
		t.Fatalf("new complete request: %v", err)
	}
	completeReq.Header.Set("Authorization", "Bearer owner-session")
	completeResp, err := enabledTS.Client().Do(completeReq)
	if err != nil {
		t.Fatalf("complete without dispatcher secret: %v", err)
	}
	_ = completeResp.Body.Close()
	if completeResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("complete without dispatcher secret status = %d, want 401", completeResp.StatusCode)
	}

	pollReq, err := http.NewRequest(http.MethodGet, enabledTS.URL+providerdev.PathAttachments+"/"+created.AttachID+"/poll", nil)
	if err != nil {
		t.Fatalf("new poll request: %v", err)
	}
	pollReq.Header.Set("Authorization", "Bearer owner-session")
	pollResp, err := enabledTS.Client().Do(pollReq)
	if err != nil {
		t.Fatalf("poll without dispatcher secret: %v", err)
	}
	_ = pollResp.Body.Close()
	if pollResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("poll without dispatcher secret status = %d, want 401", pollResp.StatusCode)
	}

	invalidPollReq, err := http.NewRequest(http.MethodGet, enabledTS.URL+providerdev.PathAttachments+"/"+created.AttachID+"/poll", nil)
	if err != nil {
		t.Fatalf("new invalid-secret poll request: %v", err)
	}
	invalidPollReq.Header.Set("Authorization", "Bearer owner-session")
	invalidPollReq.Header.Set(providerdev.HeaderDispatcherSecret, "wrong")
	invalidPollResp, err := enabledTS.Client().Do(invalidPollReq)
	if err != nil {
		t.Fatalf("poll with invalid dispatcher secret: %v", err)
	}
	_ = invalidPollResp.Body.Close()
	if invalidPollResp.StatusCode != http.StatusForbidden {
		t.Fatalf("poll with invalid dispatcher secret status = %d, want 403", invalidPollResp.StatusCode)
	}

	for _, path := range []string{providerdev.PathAttachments, providerdev.PathAttachments + "/" + created.AttachID} {
		req, err = http.NewRequest(http.MethodGet, enabledTS.URL+path, nil)
		if err != nil {
			t.Fatalf("new GET %s request: %v", path, err)
		}
		req.Header.Set("Authorization", "Bearer owner-session")
		resp, err = enabledTS.Client().Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		payload, err = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			t.Fatalf("read GET %s response: %v", path, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200; body = %s", path, resp.StatusCode, payload)
		}
		body := string(payload)
		if !strings.Contains(body, created.AttachID) {
			t.Fatalf("GET %s response missing attachId %q: %s", path, created.AttachID, body)
		}
		for _, forbidden := range []string{created.DispatcherSecret, "do-not-return", "allowedHosts", "SECRET"} {
			if forbidden != "" && strings.Contains(body, forbidden) {
				t.Fatalf("GET %s leaked %q in %s", path, forbidden, body)
			}
		}
	}

	req, err = http.NewRequest(http.MethodGet, enabledTS.URL+providerdev.PathAttachments, nil)
	if err != nil {
		t.Fatalf("new other list request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer other-session")
	resp, err = enabledTS.Client().Do(req)
	if err != nil {
		t.Fatalf("other list request: %v", err)
	}
	payload, err = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("read other list response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("other list status = %d, want 200; body = %s", resp.StatusCode, payload)
	}
	if strings.Contains(string(payload), created.AttachID) {
		t.Fatalf("other principal list leaked attachId %q: %s", created.AttachID, payload)
	}

	req, err = http.NewRequest(http.MethodGet, enabledTS.URL+providerdev.PathAttachments+"/"+created.AttachID, nil)
	if err != nil {
		t.Fatalf("new other get request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer other-session")
	resp, err = enabledTS.Client().Do(req)
	if err != nil {
		t.Fatalf("other get request: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("other get status = %d, want 403", resp.StatusCode)
	}

	req, err = http.NewRequest(http.MethodDelete, enabledTS.URL+providerdev.PathAttachments+"/"+created.AttachID, nil)
	if err != nil {
		t.Fatalf("new delete request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer owner-session")
	resp, err = enabledTS.Client().Do(req)
	if err != nil {
		t.Fatalf("delete request: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d, want 200", resp.StatusCode)
	}
}

func TestProviderDevAttachmentCreateRequiresAttachActionPermission(t *testing.T) {
	t.Parallel()

	newManager := func(t *testing.T) *providerdev.Manager {
		t.Helper()
		manager, err := providerdev.NewManager([]providerdev.Target{{
			Name:   "roadmap",
			Source: "github.com/acme/plugins/roadmap",
		}})
		if err != nil {
			t.Fatalf("NewManager: %v", err)
		}
		return manager
	}
	newToken := func(t *testing.T) (string, string) {
		t.Helper()
		plaintext, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
		if err != nil {
			t.Fatalf("GenerateToken: %v", err)
		}
		return plaintext, hashed
	}
	svc := testutil.NewStubServices(t)
	broadToken, broadHash := newToken(t)
	seedAPIToken(t, svc, broadToken, broadHash, "broad-user")
	invokeToken, invokeHash := newToken(t)
	seedAPITokenWithPermissions(t, svc, invokeToken, invokeHash, "invoke-user", []core.AccessPermission{{Plugin: "roadmap"}})
	attachToken, attachHash := newToken(t)
	seedAPITokenWithPermissions(t, svc, attachToken, attachHash, "attach-user", []core.AccessPermission{{
		Plugin:  "roadmap",
		Actions: []string{core.ProviderActionDevAttach},
	}})
	subjectToken, subjectHash := newToken(t)
	seedSubjectAPITokenWithPermissions(t, svc, subjectHash, "service_account:roadmap-dev", "roadmap-dev", []core.AccessPermission{{
		Plugin:  "roadmap",
		Actions: []string{core.ProviderActionDevAttach},
	}})

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(context.Context, string) (*core.UserIdentity, error) {
				return nil, fmt.Errorf("not a session token")
			},
		}
		cfg.Services = svc
		cfg.ProviderDevAttach = true
		cfg.ProviderDevSessions = newManager(t)
		cfg.PluginDefs = map[string]*config.ProviderEntry{
			"roadmap": {
				AuthorizationPolicy: "provider_devs",
				Dev: &config.ProviderEntryDevConfig{
					Attach: config.ProviderEntryDevAttachConfig{AllowedRoles: []string{"viewer"}},
				},
			},
		}
		cfg.Authorizer = mustAuthorizer(t, config.AuthorizationConfig{
			Policies: map[string]config.SubjectPolicyDef{
				"provider_devs": {Default: "allow"},
			},
		}, cfg.PluginDefs)
	})
	testutil.CloseOnCleanup(t, ts)

	create := func(t *testing.T, token string) (int, string) {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, ts.URL+providerdev.PathAttachments, strings.NewReader(`{"providers":[{"name":"roadmap"}]}`))
		if err != nil {
			t.Fatalf("new create request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("create request: %v", err)
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			t.Fatalf("read create response: %v", err)
		}
		return resp.StatusCode, string(body)
	}

	for name, token := range map[string]string{
		"broad token":       broadToken,
		"invoke permission": invokeToken,
		"subject token":     subjectToken,
	} {
		status, body := create(t, token)
		if status != http.StatusForbidden {
			t.Fatalf("%s create status = %d, want 403; body = %s", name, status, body)
		}
	}

	status, body := create(t, attachToken)
	if status != http.StatusCreated {
		t.Fatalf("attach action create status = %d, want 201; body = %s", status, body)
	}
	if !strings.Contains(body, `"attachId"`) || !strings.Contains(body, `"dispatcherSecret"`) {
		t.Fatalf("attach action create response missing attachId/dispatcherSecret: %s", body)
	}
}

func TestProviderDevAttachAuthorizationBrowserApprovalCreatesDispatcherSession(t *testing.T) {
	t.Parallel()

	manager, err := providerdev.NewManager([]providerdev.Target{{
		Name:   "roadmap",
		Source: "github.com/acme/plugins/roadmap",
		RuntimeEnv: func(string) (providerdev.RuntimeEnv, error) {
			return providerdev.RuntimeEnv{Env: map[string]string{"SESSION_ENV": "ok"}}, nil
		},
	}})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	auth := &coretesting.StubAuthProvider{
		N: "test",
		ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
			if token == "owner-session" {
				return &core.UserIdentity{Email: "owner@example.test"}, nil
			}
			return nil, principal.ErrInvalidToken
		},
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = auth
		cfg.ProviderDevAttach = true
		cfg.ProviderDevSessions = manager
		cfg.PluginDefs = map[string]*config.ProviderEntry{
			"roadmap": {
				AuthorizationPolicy: "provider_devs",
				Dev: &config.ProviderEntryDevConfig{
					Attach: config.ProviderEntryDevAttachConfig{AllowedRoles: []string{"viewer"}},
				},
			},
		}
		cfg.Authorizer = mustAuthorizer(t, config.AuthorizationConfig{
			Policies: map[string]config.SubjectPolicyDef{
				"provider_devs": {Default: "allow"},
			},
		}, cfg.PluginDefs)
	})
	testutil.CloseOnCleanup(t, ts)

	createReq, err := http.NewRequest(http.MethodPost, ts.URL+providerdev.PathAttachAuthorizations, strings.NewReader(`{"providers":[{"name":"roadmap"}]}`))
	if err != nil {
		t.Fatalf("new attach authorization request: %v", err)
	}
	createReq.Header.Set("Content-Type", "application/json")
	createResp, err := ts.Client().Do(createReq)
	if err != nil {
		t.Fatalf("create attach authorization: %v", err)
	}
	createBody, err := io.ReadAll(createResp.Body)
	_ = createResp.Body.Close()
	if err != nil {
		t.Fatalf("read attach authorization response: %v", err)
	}
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create attach authorization status = %d, want 201; body = %s", createResp.StatusCode, createBody)
	}
	var authorization providerdev.CreateAttachAuthorizationResponse
	if err := json.Unmarshal(createBody, &authorization); err != nil {
		t.Fatalf("decode attach authorization response: %v", err)
	}
	if authorization.AuthorizationID == "" || authorization.ClientSecret == "" || authorization.VerificationCode == "" || authorization.ApprovalURL == "" {
		t.Fatalf("attach authorization response missing fields: %s", createBody)
	}

	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	showReq, err := http.NewRequest(http.MethodGet, authorization.ApprovalURL, nil)
	if err != nil {
		t.Fatalf("new show approval request: %v", err)
	}
	showResp, err := noRedirect.Do(showReq)
	if err != nil {
		t.Fatalf("show approval without browser session: %v", err)
	}
	_ = showResp.Body.Close()
	if showResp.StatusCode != http.StatusFound {
		t.Fatalf("show approval without browser session status = %d, want 302", showResp.StatusCode)
	}
	if location := showResp.Header.Get("Location"); !strings.HasPrefix(location, "/api/v1/auth/login?next=") {
		t.Fatalf("show approval redirect = %q, want browser login", location)
	}

	bearerOnlyReq, err := http.NewRequest(http.MethodGet, authorization.ApprovalURL, nil)
	if err != nil {
		t.Fatalf("new bearer-only approval request: %v", err)
	}
	bearerOnlyReq.Header.Set("Authorization", "Bearer owner-session")
	bearerOnlyResp, err := noRedirect.Do(bearerOnlyReq)
	if err != nil {
		t.Fatalf("show approval with bearer only: %v", err)
	}
	_ = bearerOnlyResp.Body.Close()
	if bearerOnlyResp.StatusCode != http.StatusFound {
		t.Fatalf("show approval with bearer only status = %d, want 302", bearerOnlyResp.StatusCode)
	}

	showReq, err = http.NewRequest(http.MethodGet, authorization.ApprovalURL, nil)
	if err != nil {
		t.Fatalf("new authenticated approval request: %v", err)
	}
	showReq.AddCookie(&http.Cookie{Name: "session_token", Value: "owner-session"})
	showResp, err = ts.Client().Do(showReq)
	if err != nil {
		t.Fatalf("show approval with browser session: %v", err)
	}
	showBody, err := io.ReadAll(showResp.Body)
	_ = showResp.Body.Close()
	if err != nil {
		t.Fatalf("read approval page: %v", err)
	}
	if showResp.StatusCode != http.StatusOK {
		t.Fatalf("show approval with browser session status = %d, want 200; body = %s", showResp.StatusCode, showBody)
	}
	if showResp.Header.Get("Cache-Control") != "no-store" || !strings.Contains(string(showBody), "roadmap") {
		t.Fatalf("approval page missing no-store or provider name: headers=%v body=%s", showResp.Header, showBody)
	}
	wantAction := `action="./` + authorization.AuthorizationID + `/approve"`
	if !strings.Contains(string(showBody), wantAction) {
		t.Fatalf("approval page action = %s, want relative action %q", showBody, wantAction)
	}
	if strings.Contains(string(showBody), authorization.VerificationCode) {
		t.Fatalf("approval page leaked verification code: %s", showBody)
	}

	wrongCode := "000-000"
	if authorization.VerificationCode == wrongCode {
		wrongCode = "111-111"
	}
	approveReq, err := http.NewRequest(http.MethodPost, authorization.ApprovalURL+"/approve", strings.NewReader(url.Values{"verificationCode": {wrongCode}}.Encode()))
	if err != nil {
		t.Fatalf("new wrong-code approve request: %v", err)
	}
	approveReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	approveReq.AddCookie(&http.Cookie{Name: "session_token", Value: "owner-session"})
	approveResp, err := ts.Client().Do(approveReq)
	if err != nil {
		t.Fatalf("approve attach authorization with wrong code: %v", err)
	}
	_ = approveResp.Body.Close()
	if approveResp.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong-code approve status = %d, want 403", approveResp.StatusCode)
	}

	approveReq, err = http.NewRequest(http.MethodPost, authorization.ApprovalURL+"/approve", strings.NewReader(url.Values{"verificationCode": {authorization.VerificationCode}}.Encode()))
	if err != nil {
		t.Fatalf("new approve request: %v", err)
	}
	approveReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	approveReq.AddCookie(&http.Cookie{Name: "session_token", Value: "owner-session"})
	approveResp, err = ts.Client().Do(approveReq)
	if err != nil {
		t.Fatalf("approve attach authorization: %v", err)
	}
	approveBody, err := io.ReadAll(approveResp.Body)
	_ = approveResp.Body.Close()
	if err != nil {
		t.Fatalf("read approve response: %v", err)
	}
	if approveResp.StatusCode != http.StatusOK {
		t.Fatalf("approve status = %d, want 200; body = %s", approveResp.StatusCode, approveBody)
	}
	if approveResp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("approve Cache-Control = %q, want no-store", approveResp.Header.Get("Cache-Control"))
	}

	pollReq, err := http.NewRequest(http.MethodGet, ts.URL+providerdev.PathAttachAuthorizations+"/"+authorization.AuthorizationID+"/poll", nil)
	if err != nil {
		t.Fatalf("new poll request: %v", err)
	}
	pollResp, err := ts.Client().Do(pollReq)
	if err != nil {
		t.Fatalf("poll without authorization secret: %v", err)
	}
	_ = pollResp.Body.Close()
	if pollResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("poll without authorization secret status = %d, want 401", pollResp.StatusCode)
	}

	pollReq, err = http.NewRequest(http.MethodGet, ts.URL+providerdev.PathAttachAuthorizations+"/"+authorization.AuthorizationID+"/poll", nil)
	if err != nil {
		t.Fatalf("new authorized poll request: %v", err)
	}
	pollReq.Header.Set(providerdev.HeaderAuthorizationSecret, authorization.ClientSecret)
	pollResp, err = ts.Client().Do(pollReq)
	if err != nil {
		t.Fatalf("poll approval: %v", err)
	}
	pollBody, err := io.ReadAll(pollResp.Body)
	_ = pollResp.Body.Close()
	if err != nil {
		t.Fatalf("read poll response: %v", err)
	}
	if pollResp.StatusCode != http.StatusOK {
		t.Fatalf("poll status = %d, want 200; body = %s", pollResp.StatusCode, pollBody)
	}
	var poll providerdev.PollAttachAuthorizationResponse
	if err := json.Unmarshal(pollBody, &poll); err != nil {
		t.Fatalf("decode poll response: %v", err)
	}
	if !poll.Approved {
		t.Fatalf("poll response missing approval: %s", pollBody)
	}

	approveReq, err = http.NewRequest(http.MethodPost, authorization.ApprovalURL+"/approve", strings.NewReader(url.Values{"verificationCode": {authorization.VerificationCode}}.Encode()))
	if err != nil {
		t.Fatalf("new second approve request: %v", err)
	}
	approveReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	approveReq.AddCookie(&http.Cookie{Name: "session_token", Value: "owner-session"})
	approveResp, err = ts.Client().Do(approveReq)
	if err != nil {
		t.Fatalf("approve attach authorization again: %v", err)
	}
	_ = approveResp.Body.Close()
	if approveResp.StatusCode != http.StatusOK {
		t.Fatalf("second approve status = %d, want 200", approveResp.StatusCode)
	}
	pollReq, err = http.NewRequest(http.MethodGet, ts.URL+providerdev.PathAttachAuthorizations+"/"+authorization.AuthorizationID+"/poll", nil)
	if err != nil {
		t.Fatalf("new second authorized poll request: %v", err)
	}
	pollReq.Header.Set(providerdev.HeaderAuthorizationSecret, authorization.ClientSecret)
	pollResp, err = ts.Client().Do(pollReq)
	if err != nil {
		t.Fatalf("poll approval after second approve: %v", err)
	}
	pollBody, err = io.ReadAll(pollResp.Body)
	_ = pollResp.Body.Close()
	if err != nil {
		t.Fatalf("read second poll response: %v", err)
	}
	var secondPoll providerdev.PollAttachAuthorizationResponse
	if err := json.Unmarshal(pollBody, &secondPoll); err != nil {
		t.Fatalf("decode second poll response: %v", err)
	}
	if !secondPoll.Approved {
		t.Fatalf("second poll response missing approval: %s", pollBody)
	}

	missingSecretReq, err := http.NewRequest(http.MethodPost, ts.URL+providerdev.PathAttachAuthorizations+"/"+authorization.AuthorizationID+"/attachments", strings.NewReader(`{"providers":[{"name":"roadmap"}]}`))
	if err != nil {
		t.Fatalf("new missing-secret authorized session request: %v", err)
	}
	missingSecretReq.Header.Set("Content-Type", "application/json")
	missingSecretResp, err := ts.Client().Do(missingSecretReq)
	if err != nil {
		t.Fatalf("create authorized session without secret: %v", err)
	}
	missingSecretBody, err := io.ReadAll(missingSecretResp.Body)
	_ = missingSecretResp.Body.Close()
	if err != nil {
		t.Fatalf("read missing-secret session response: %v", err)
	}
	if missingSecretResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing-secret session status = %d, want 401; body = %s", missingSecretResp.StatusCode, missingSecretBody)
	}

	changedRequestReq, err := http.NewRequest(http.MethodPost, ts.URL+providerdev.PathAttachAuthorizations+"/"+authorization.AuthorizationID+"/attachments", strings.NewReader(`{"providers":[{"name":"other"}]}`))
	if err != nil {
		t.Fatalf("new changed-request authorized session request: %v", err)
	}
	changedRequestReq.Header.Set("Content-Type", "application/json")
	changedRequestReq.Header.Set(providerdev.HeaderAuthorizationSecret, authorization.ClientSecret)
	changedRequestResp, err := ts.Client().Do(changedRequestReq)
	if err != nil {
		t.Fatalf("create authorized session with changed request: %v", err)
	}
	changedRequestBody, err := io.ReadAll(changedRequestResp.Body)
	_ = changedRequestResp.Body.Close()
	if err != nil {
		t.Fatalf("read changed-request session response: %v", err)
	}
	if changedRequestResp.StatusCode != http.StatusForbidden {
		t.Fatalf("changed-request session status = %d, want 403; body = %s", changedRequestResp.StatusCode, changedRequestBody)
	}

	sessionReq, err := http.NewRequest(http.MethodPost, ts.URL+providerdev.PathAttachAuthorizations+"/"+authorization.AuthorizationID+"/attachments", strings.NewReader(`{"providers":[{"name":"roadmap"}]}`))
	if err != nil {
		t.Fatalf("new authorized session request: %v", err)
	}
	sessionReq.Header.Set("Content-Type", "application/json")
	sessionReq.Header.Set(providerdev.HeaderAuthorizationSecret, authorization.ClientSecret)
	sessionResp, err := ts.Client().Do(sessionReq)
	if err != nil {
		t.Fatalf("create authorized session: %v", err)
	}
	sessionBody, err := io.ReadAll(sessionResp.Body)
	_ = sessionResp.Body.Close()
	if err != nil {
		t.Fatalf("read authorized session response: %v", err)
	}
	if sessionResp.StatusCode != http.StatusCreated {
		t.Fatalf("authorized session status = %d, want 201; body = %s", sessionResp.StatusCode, sessionBody)
	}
	var session providerdev.CreateSessionResponse
	if err := json.Unmarshal(sessionBody, &session); err != nil {
		t.Fatalf("decode authorized session response: %v", err)
	}
	if session.AttachID == "" || session.DispatcherSecret == "" {
		t.Fatalf("authorized session missing attachId/dispatcherSecret: %s", sessionBody)
	}

	deleteReq, err := http.NewRequest(http.MethodDelete, ts.URL+providerdev.PathAttachments+"/"+session.AttachID, nil)
	if err != nil {
		t.Fatalf("new dispatcher delete request: %v", err)
	}
	deleteReq.Header.Set(providerdev.HeaderDispatcherSecret, session.DispatcherSecret)
	deleteResp, err := ts.Client().Do(deleteReq)
	if err != nil {
		t.Fatalf("delete by dispatcher secret: %v", err)
	}
	deleteBody, err := io.ReadAll(deleteResp.Body)
	_ = deleteResp.Body.Close()
	if err != nil {
		t.Fatalf("read delete response: %v", err)
	}
	if deleteResp.StatusCode != http.StatusOK {
		t.Fatalf("delete by dispatcher secret status = %d, want 200; body = %s", deleteResp.StatusCode, deleteBody)
	}
}

func TestProviderDevAttachAuthorizationApprovalURLPreservesPublicBasePath(t *testing.T) {
	t.Parallel()

	manager, err := providerdev.NewManager([]providerdev.Target{{Name: "roadmap"}})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
		cfg.PublicBaseURL = "https://gestalt.example.test/team-a"
		cfg.ProviderDevAttach = true
		cfg.ProviderDevSessions = manager
	})
	testutil.CloseOnCleanup(t, ts)

	createReq, err := http.NewRequest(http.MethodPost, ts.URL+providerdev.PathAttachAuthorizations, strings.NewReader(`{"providers":[{"name":"roadmap"}]}`))
	if err != nil {
		t.Fatalf("new attach authorization request: %v", err)
	}
	createReq.Header.Set("Content-Type", "application/json")
	createResp, err := ts.Client().Do(createReq)
	if err != nil {
		t.Fatalf("create attach authorization: %v", err)
	}
	createBody, err := io.ReadAll(createResp.Body)
	_ = createResp.Body.Close()
	if err != nil {
		t.Fatalf("read attach authorization response: %v", err)
	}
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create attach authorization status = %d, want 201; body = %s", createResp.StatusCode, createBody)
	}
	var authorization providerdev.CreateAttachAuthorizationResponse
	if err := json.Unmarshal(createBody, &authorization); err != nil {
		t.Fatalf("decode attach authorization response: %v", err)
	}
	if !strings.HasPrefix(authorization.ApprovalURL, "https://gestalt.example.test/team-a/api/v1/provider-dev/attach-authorizations/") {
		t.Fatalf("approvalUrl = %q, want public base path preserved", authorization.ApprovalURL)
	}
}

func TestAuthMiddleware_ValidAPIToken(t *testing.T) {
	t.Parallel()

	plaintext, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	svc := testutil.NewStubServices(t)
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

func TestAuthMiddleware_ValidSubjectOwnedAPIToken(t *testing.T) {
	t.Parallel()

	plaintext, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	svc := testutil.NewStubServices(t)
	seedSubjectAPIToken(t, svc, hashed, "service_account:weather-bot", "weather-bot")

	stub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{N: "weather", ConnMode: core.ConnectionModeNone},
		ops:             []core.Operation{{Name: "forecast", Method: http.MethodGet}},
	}
	providers := testutil.NewProviderRegistry(t, stub)
	authz := mustAuthorizer(t, config.AuthorizationConfig{}, nil)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, _ string) (*core.UserIdentity, error) {
				return nil, fmt.Errorf("not a session token")
			},
		}
		cfg.Providers = providers
		cfg.Services = svc
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

func TestAuthMiddleware_SubjectOwnedAPITokenRejectsBorrowedCredentialSubject(t *testing.T) {
	t.Parallel()

	plaintext, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	svc := testutil.NewStubServices(t)
	now := time.Now()
	if err := svc.DB.ObjectStore(coredata.StoreAPITokens).Add(context.Background(), indexeddb.Record{
		"id":                    "api-tok-borrowed-credential",
		"owner_kind":            core.APITokenOwnerKindSubject,
		"owner_id":              "service_account:triage-bot",
		"credential_subject_id": principal.UserSubjectID("other-user"),
		"name":                  "triage-bot",
		"hashed_token":          hashed,
		"created_at":            now,
		"updated_at":            now,
	}); err != nil {
		t.Fatalf("seed malformed api token: %v", err)
	}

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

	if resp.StatusCode != http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 401 for borrowed credential subject, got %d: %s", resp.StatusCode, body)
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

	svc := testutil.NewStubServices(t)
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
		cfg.Services = testutil.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	req.Header.Set("Authorization", "Bearer unprefixed-token")
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

	svc := testutil.NewStubServices(t)
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

	svc := testutil.NewStubServices(t)
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
		cfg.Services = testutil.NewStubServices(t)
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
		cfg.Services = testutil.NewStubServices(t)
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
		Name            string `json:"name"`
		DisplayName     string `json:"displayName"`
		Description     string `json:"description"`
		Status          string `json:"status"`
		CredentialState string `json:"credentialState"`
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
	if integrations[0].Status != "needs_user_connection" || integrations[0].CredentialState != "missing" {
		t.Fatalf("status = {%q, %q}, want needs_user_connection/missing", integrations[0].Status, integrations[0].CredentialState)
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
		cfg.Services = testutil.NewStubServices(t)
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

	svc := testutil.NewStubServices(t)
	viewer := seedUser(t, svc, "viewer@example.test")
	policyMembers := []config.SubjectPolicyMemberDef{
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
		Policies: map[string]config.SubjectPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: policyMembers,
			},
		},
	}, pluginDefs)

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

func TestListIntegrations_HidesProviderWithOnlyInternalHTTPOperations(t *testing.T) {
	t.Parallel()

	manifest := &providermanifestv1.Manifest{
		Source:      "internal-only",
		DisplayName: "Internal Only",
		Spec: &providermanifestv1.Spec{
			DefaultConnection: "bot",
			Connections: map[string]*providermanifestv1.ManifestConnectionDef{
				"bot": {
					Mode:     providermanifestv1.ConnectionModePlatform,
					Exposure: providermanifestv1.ConnectionExposureInternal,
					Auth:     &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeBearer},
				},
			},
			Surfaces: &providermanifestv1.ProviderSurfaces{
				REST: &providermanifestv1.RESTSurface{
					Connection: "bot",
					Operations: []providermanifestv1.ProviderOperation{
						{
							Name:       "bot.only",
							Method:     http.MethodGet,
							Path:       "/bot.only",
							Connection: "bot",
						},
					},
				},
			},
		},
	}
	prov := &stubNonOAuthProvider{
		name: "internal-only",
		catalog: serverTestCatalog("internal-only", []catalog.CatalogOperation{{
			ID:        "bot.only",
			Method:    http.MethodGet,
			Path:      "/bot.only",
			Transport: catalog.TransportREST,
		}}),
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, prov)
		cfg.PluginDefs = map[string]*config.ProviderEntry{
			"internal-only": {ResolvedManifest: manifest},
		}
		cfg.Services = testutil.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/api/v1/integrations")
	if err != nil {
		t.Fatalf("list integrations: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("integrations status = %d, want %d: %s", resp.StatusCode, http.StatusOK, body)
	}
	var integrations []struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&integrations); err != nil {
		t.Fatalf("decode integrations: %v", err)
	}
	for _, integration := range integrations {
		if integration.Name == "internal-only" {
			t.Fatalf("internal-only integration leaked in integrations response: %+v", integration)
		}
	}

	opsResp, err := http.Get(ts.URL + "/api/v1/integrations/internal-only/operations")
	if err != nil {
		t.Fatalf("list operations: %v", err)
	}
	defer func() { _ = opsResp.Body.Close() }()
	if opsResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(opsResp.Body)
		t.Fatalf("operations status = %d, want %d: %s", opsResp.StatusCode, http.StatusOK, body)
	}
	var ops []catalog.CatalogOperation
	if err := json.NewDecoder(opsResp.Body).Decode(&ops); err != nil {
		t.Fatalf("decode operations: %v", err)
	}
	if len(ops) != 0 {
		t.Fatalf("operations = %+v, want none", ops)
	}
}

func TestSubjectAuthorization_ListIntegrationsUsesSubjectPolicyAndCredentials(t *testing.T) {
	t.Parallel()

	subjectToken, subjectTokenHash, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	svc := testutil.NewStubServices(t)
	seedSubjectAPIToken(t, svc, subjectTokenHash, "service_account:triage-bot", "triage-bot")
	seedSubjectToken(t, svc, "service_account:triage-bot", "svc", "workspace", "default", "identity-svc-token")

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
	pluginDefs := map[string]*config.ProviderEntry{
		"svc": {
			AuthorizationPolicy: "subject_policy",
			Connections: map[string]*config.ConnectionDef{
				"workspace": {
					ConnectionID: "svc:workspace",
					Mode:         providermanifestv1.ConnectionModeUser,
				},
			},
		},
		"weather":  {AuthorizationPolicy: "subject_policy"},
		"mcp-only": {AuthorizationPolicy: "subject_policy"},
		"secret":   {AuthorizationPolicy: "secret_policy"},
	}
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"subject_policy": {
				Default: "allow",
				Members: []config.SubjectPolicyMemberDef{{
					SubjectID: "service_account:triage-bot",
					Role:      "viewer",
				}},
			},
			"secret_policy": {},
		},
	}, pluginDefs)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
		cfg.Providers = providers
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.PluginDefs = map[string]*config.ProviderEntry{"svc": pluginDefs["svc"]}
		cfg.DefaultConnection = map[string]string{"svc": "workspace"}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	req.Header.Set("Authorization", "Bearer "+subjectToken)
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
		Name            string `json:"name"`
		Status          string `json:"status"`
		CredentialState string `json:"credentialState"`
		Connections     []struct {
			Name      string           `json:"name"`
			Instances []map[string]any `json:"instances"`
		} `json:"connections"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&integrations); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(integrations) != 2 {
		t.Fatalf("expected 2 integrations, got %+v", integrations)
	}

	got := map[string]struct {
		Status          string
		CredentialState string
		Connections     []struct {
			Name      string           `json:"name"`
			Instances []map[string]any `json:"instances"`
		}
	}{}
	for _, integration := range integrations {
		got[integration.Name] = struct {
			Status          string
			CredentialState string
			Connections     []struct {
				Name      string           `json:"name"`
				Instances []map[string]any `json:"instances"`
			}
		}{
			Status:          integration.Status,
			CredentialState: integration.CredentialState,
			Connections:     integration.Connections,
		}
	}
	if _, ok := got["secret"]; ok {
		t.Fatalf("unauthorized integration was visible: %+v", integrations)
	}
	if _, ok := got["mcp-only"]; ok {
		t.Fatalf("mcp-only integration should not be visible over HTTP: %+v", integrations)
	}
	if got["svc"].Status != "ready" || got["svc"].CredentialState != "connected" {
		t.Fatalf("expected service account integration to be connected, got %+v", got["svc"])
	}
	if got["weather"].Status != "ready" || got["weather"].CredentialState != "not_required" {
		t.Fatalf("expected connection-mode none integration to be connected, got %+v", got["weather"])
	}
	var svcInstances []map[string]any
	for _, conn := range got["svc"].Connections {
		svcInstances = append(svcInstances, conn.Instances...)
	}
	if len(svcInstances) != 1 {
		t.Fatalf("expected service-account-owned svc instance to be listed, got %+v", got["svc"])
	}

	provider := svc.ExternalCredentials.(*coretesting.StubExternalCredentialProvider)
	provider.ListErr = fmt.Errorf("token store unavailable")
	provider.GetErr = provider.ListErr
	t.Cleanup(func() {
		provider.ListErr = nil
		provider.GetErr = nil
	})

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	req.Header.Set("Authorization", "Bearer "+subjectToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request with token store outage: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusInternalServerError {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 500 when service account credential lookup fails, got %d: %s", resp.StatusCode, body)
	}
}

func TestSubjectAuthorization_ListOperationsUsesSubjectPolicyAndSessionSelectors(t *testing.T) {
	t.Parallel()

	subjectToken, subjectTokenHash, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	authSvc := testutil.NewStubServices(t)
	seedSubjectAPIToken(t, authSvc, subjectTokenHash, "service_account:triage-bot", "triage-bot")

	provider := &stubIntegrationWithCatalog{
		StubIntegration: coretesting.StubIntegration{N: "svc", ConnMode: core.ConnectionModeUser},
		catalog: serverTestCatalog("svc", []catalog.CatalogOperation{
			{ID: "run", Method: http.MethodGet, Transport: catalog.TransportREST, AllowedRoles: []string{"viewer"}},
			{ID: "admin", Method: http.MethodGet, Transport: catalog.TransportREST, AllowedRoles: []string{"admin"}},
		}),
	}
	providers := testutil.NewProviderRegistry(t, provider)
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"svc_policy": {
				Default: "allow",
				Members: []config.SubjectPolicyMemberDef{{
					SubjectID: "service_account:triage-bot",
					Role:      "viewer",
				}},
			},
		},
	}, map[string]*config.ProviderEntry{"svc": {AuthorizationPolicy: "svc_policy"}})

	var auditBuf bytes.Buffer
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
		cfg.Providers = providers
		cfg.Services = authSvc
		cfg.Authorizer = authz
		cfg.AuditSink = invocation.NewSlogAuditSink(&auditBuf)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations/svc/operations", nil)
	req.Header.Set("Authorization", "Bearer "+subjectToken)
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

	svc := testutil.NewStubServices(t)
	seedSubjectAPIToken(t, svc, subjectTokenHash, "service_account:triage-bot", "triage-bot")
	seedSubjectToken(t, svc, "service_account:triage-bot", "svc-session", testDefaultConnection, "team-a", "session-bound-token")

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
					{ID: "run", Method: http.MethodGet, AllowedRoles: []string{"viewer"}},
				},
			}, nil
		},
	}
	sessionProviders := testutil.NewProviderRegistry(t, sessionProvider)
	sessionAuthz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"svc_session_policy": {
				Members: []config.SubjectPolicyMemberDef{{
					SubjectID: "service_account:triage-bot",
					Role:      "viewer",
				}},
			},
		},
	}, map[string]*config.ProviderEntry{"svc-session": {AuthorizationPolicy: "svc_session_policy"}})

	sessionTS := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
		cfg.Providers = sessionProviders
		cfg.Services = svc
		cfg.Authorizer = sessionAuthz
		cfg.DefaultConnection = map[string]string{"svc-session": testDefaultConnection}
	})
	testutil.CloseOnCleanup(t, sessionTS)

	req, _ = http.NewRequest(http.MethodGet, sessionTS.URL+"/api/v1/integrations/svc-session/operations", nil)
	req.Header.Set("Authorization", "Bearer "+subjectToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("session catalog request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 for session-catalog service account discovery, got %d: %s", resp.StatusCode, body)
	}

	ops = nil
	if err := json.NewDecoder(resp.Body).Decode(&ops); err != nil {
		t.Fatalf("decoding session ops: %v", err)
	}
	if len(ops) != 1 || ops[0].ID != "run" {
		t.Fatalf("session operations = %+v, want only run", ops)
	}
	if sessionCatalogToken != "session-bound-token" {
		t.Fatalf("expected session catalog to use service account credential token, got %q", sessionCatalogToken)
	}
}

func TestListIntegrationsShowsConnected(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.ExternalCredential{
		ID: "tok1", SubjectID: principal.UserSubjectID(u.ID), ConnectionID: "slack:default",
		Integration: "slack", Connection: "default", Instance: "default", AccessToken: "test-token",
	})

	stub := &coretesting.StubIntegration{N: "slack", DN: "Slack", Desc: "Team messaging"}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.PluginDefs = testPluginDefsForConnections("slack", "default")
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
		Name            string `json:"name"`
		Status          string `json:"status"`
		CredentialState string `json:"credentialState"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&integrations); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(integrations) != 1 {
		t.Fatalf("expected 1 integration, got %d", len(integrations))
	}
	if integrations[0].Status != "ready" || integrations[0].CredentialState != "connected" {
		t.Fatalf("status = {%q, %q}, want ready/connected", integrations[0].Status, integrations[0].CredentialState)
	}
}

func TestListIntegrations_ConnectionStatusContract(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	subjectID := principal.UserSubjectID(u.ID)
	seedSubjectToken(t, svc, subjectID, "manual-connected", testDefaultConnection, "default", "connected-token")
	seedSubjectToken(t, svc, subjectID, "manual-multi", testDefaultConnection, "team-a", "team-a-token")
	seedSubjectToken(t, svc, subjectID, "manual-multi", testDefaultConnection, "team-b", "team-b-token")

	providers := testutil.NewProviderRegistry(t,
		&coretesting.StubIntegration{N: "no-auth", DN: "No Auth", ConnMode: core.ConnectionModeNone},
		&stubManualProvider{StubIntegration: coretesting.StubIntegration{N: "manual-disconnected", DN: "Manual Disconnected"}},
		&stubManualProvider{StubIntegration: coretesting.StubIntegration{N: "manual-connected", DN: "Manual Connected"}},
		&stubManualProvider{StubIntegration: coretesting.StubIntegration{N: "manual-multi", DN: "Manual Multi"}},
		&coretesting.StubIntegration{N: "platform-bearer", DN: "Platform Bearer", ConnMode: core.ConnectionModePlatform},
		&coretesting.StubIntegration{N: "platform-manual", DN: "Platform Manual", ConnMode: core.ConnectionModePlatform},
		&coretesting.StubIntegration{N: "platform-missing", DN: "Platform Missing", ConnMode: core.ConnectionModePlatform},
	)
	pluginDefs := map[string]*config.ProviderEntry{
		"manual-connected": {
			Connections: map[string]*config.ConnectionDef{
				testDefaultConnection: {
					ConnectionID: "manual-connected:" + testDefaultConnection,
					Mode:         providermanifestv1.ConnectionModeUser,
				},
			},
		},
		"manual-multi": {
			Connections: map[string]*config.ConnectionDef{
				testDefaultConnection: {
					ConnectionID: "manual-multi:" + testDefaultConnection,
					Mode:         providermanifestv1.ConnectionModeUser,
				},
			},
		},
		"platform-bearer": {
			ConnectionMode: providermanifestv1.ConnectionModePlatform,
			Auth: &config.ConnectionAuthDef{
				Type:  providermanifestv1.AuthTypeBearer,
				Token: "deployment-token",
			},
		},
		"platform-manual": {
			ConnectionMode: providermanifestv1.ConnectionModePlatform,
			Auth: &config.ConnectionAuthDef{
				Type: providermanifestv1.AuthTypeManual,
				AuthMapping: &config.AuthMappingDef{
					Headers: map[string]providermanifestv1.AuthValue{
						"X-API-Key": {Value: "deployment-api-key"},
					},
				},
			},
		},
		"platform-missing": {
			ConnectionMode: providermanifestv1.ConnectionModePlatform,
			Auth: &config.ConnectionAuthDef{
				Type: providermanifestv1.AuthTypeBearer,
			},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = providers
		cfg.PluginDefs = pluginDefs
		cfg.Services = svc
		cfg.DefaultConnection = map[string]string{
			"manual-connected": testDefaultConnection,
			"manual-multi":     testDefaultConnection,
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	type statusConnection struct {
		Name             string           `json:"name"`
		Mode             string           `json:"mode"`
		Status           string           `json:"status"`
		CredentialState  string           `json:"credentialState"`
		HealthState      string           `json:"healthState"`
		Actions          []string         `json:"actions"`
		CredentialMode   string           `json:"credentialMode"`
		OwnerKind        string           `json:"ownerKind"`
		Instances        []map[string]any `json:"instances"`
		StatusCode       string           `json:"statusCode"`
		StatusReason     string           `json:"statusReason"`
		AuthTypes        []string         `json:"authTypes"`
		CredentialFields []map[string]any `json:"credentialFields"`
	}
	type statusIntegration struct {
		Name            string             `json:"name"`
		Connections     []statusConnection `json:"connections"`
		Status          string             `json:"status"`
		CredentialState string             `json:"credentialState"`
		HealthState     string             `json:"healthState"`
		Actions         []string           `json:"actions"`
	}
	var integrations []statusIntegration
	if err := json.Unmarshal(body, &integrations); err != nil {
		t.Fatalf("decode integrations: %v (body: %s)", err, body)
	}
	got := make(map[string]statusIntegration, len(integrations))
	for _, integration := range integrations {
		got[integration.Name] = integration
		if integration.Connections == nil {
			t.Fatalf("%s connections must stay non-nil: %+v", integration.Name, integration)
		}
	}

	assertIntegrationStatus := func(name, status, credentialState, healthState string, actions []string) statusIntegration {
		t.Helper()
		integration, ok := got[name]
		if !ok {
			t.Fatalf("integration %q missing from response: %s", name, body)
		}
		if integration.Status != status || integration.CredentialState != credentialState || integration.HealthState != healthState || !reflect.DeepEqual(integration.Actions, actions) {
			t.Fatalf("%s status = {status:%q credential:%q health:%q actions:%v}, want {%q %q %q %v}",
				name, integration.Status, integration.CredentialState, integration.HealthState, integration.Actions,
				status, credentialState, healthState, actions)
		}
		return integration
	}

	assertIntegrationStatus("no-auth", "ready", "not_required", "not_applicable", []string{})
	assertIntegrationStatus("manual-disconnected", "needs_user_connection", "missing", "not_applicable", []string{"connect"})
	assertIntegrationStatus("manual-connected", "ready", "connected", "not_checked", []string{"disconnect", "add_instance"})
	assertIntegrationStatus("manual-multi", "needs_instance_selection", "connected", "not_checked", []string{"select_instance", "disconnect", "add_instance"})

	platformBearer := assertIntegrationStatus("platform-bearer", "ready", "configured", "not_checked", []string{})
	platformManual := assertIntegrationStatus("platform-manual", "ready", "configured", "not_checked", []string{})
	platformMissing := assertIntegrationStatus("platform-missing", "needs_admin_configuration", "missing", "unknown", []string{"admin_configure"})

	for _, tc := range []struct {
		name        string
		integration statusIntegration
		wantStatus  string
		wantCode    string
	}{
		{name: "platform-bearer", integration: platformBearer, wantStatus: "ready"},
		{name: "platform-manual", integration: platformManual, wantStatus: "ready"},
		{name: "platform-missing", integration: platformMissing, wantStatus: "needs_admin_configuration", wantCode: "admin_configuration_required"},
	} {
		tc := tc
		t.Run(tc.name+" connection", func(t *testing.T) {
			t.Parallel()
			if len(tc.integration.Connections) != 1 {
				t.Fatalf("connections = %+v, want one platform connection", tc.integration.Connections)
			}
			conn := tc.integration.Connections[0]
			if conn.Mode != string(core.ConnectionModePlatform) || conn.CredentialMode != "platform" || conn.OwnerKind != "platform" {
				t.Fatalf("platform connection fields = %+v", conn)
			}
			if conn.Status != tc.wantStatus {
				t.Fatalf("connection status = %q, want %q", conn.Status, tc.wantStatus)
			}
			if conn.StatusCode != tc.wantCode {
				t.Fatalf("connection statusCode = %q, want %q", conn.StatusCode, tc.wantCode)
			}
			if len(conn.Instances) != 0 {
				t.Fatalf("platform instances = %+v, want empty", conn.Instances)
			}
		})
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
		cfg.Services = testutil.NewStubServices(t)
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
			AuthTypes []string `json:"authTypes"`
		} `json:"connections"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&integrations); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(integrations) != 3 {
		t.Fatalf("expected 3 integrations, got %d", len(integrations))
	}

	authTypes := make(map[string][]string)
	for _, i := range integrations {
		if len(i.Connections) > 0 {
			authTypes[i.Name] = i.Connections[0].AuthTypes
		}
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
		cfg.Services = testutil.NewStubServices(t)
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
		Name        string `json:"name"`
		Connections []struct {
			AuthTypes []string `json:"authTypes"`
		} `json:"connections"`
	}
	if err := json.Unmarshal(body, &integrations); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(integrations) != 1 {
		t.Fatalf("expected 1 integration, got %d", len(integrations))
	}
	if len(integrations[0].Connections) == 0 {
		t.Fatalf("connections = %+v, want manual connection metadata", integrations[0].Connections)
	}
	for _, conn := range integrations[0].Connections {
		if !reflect.DeepEqual(conn.AuthTypes, []string{"manual"}) {
			t.Fatalf("connection auth types = %+v, want [manual]", conn)
		}
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
		cfg.Services = testutil.NewStubServices(t)
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
		cfg.Services = testutil.NewStubServices(t)
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
		Name        string `json:"name"`
		Connections []struct {
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
					ConnectionParams: map[string]config.ConnectionParamDef{
						"region": {Required: true, Description: "Workspace region", Default: "us-east"},
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
			cfg.Services = testutil.NewStubServices(t)
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
		type connectionParam struct {
			Required    bool   `json:"required"`
			Description string `json:"description"`
			Default     string `json:"default"`
		}
		type connectionInfo struct {
			DisplayName      string                     `json:"displayName"`
			Name             string                     `json:"name"`
			AuthTypes        []string                   `json:"authTypes"`
			CredentialFields []credentialField          `json:"credentialFields"`
			ConnectionParams map[string]connectionParam `json:"connectionParams"`
		}

		var integrations []struct {
			Name        string           `json:"name"`
			Connections []connectionInfo `json:"connections"`
		}
		if err := json.Unmarshal(body, &integrations); err != nil {
			t.Fatalf("decoding: %v", err)
		}
		if len(integrations) != 1 {
			t.Fatalf("expected 1 integration, got %d", len(integrations))
		}
		if integrations[0].Connections == nil {
			t.Fatalf("expected non-nil connections, got %+v", integrations[0])
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
		if !reflect.DeepEqual(got["workspace"].ConnectionParams, map[string]connectionParam{
			"region": {Required: true, Description: "Workspace region", Default: "us-east"},
		}) {
			t.Fatalf("workspace connection params = %+v", got["workspace"].ConnectionParams)
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
			cfg.Services = testutil.NewStubServices(t)
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
			cfg.Services = testutil.NewStubServices(t)
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
			Name        string `json:"name"`
			Connections []struct {
				Name             string                     `json:"name"`
				AuthTypes        []string                   `json:"authTypes"`
				CredentialFields []credentialField          `json:"credentialFields"`
				ConnectionParams map[string]connectionParam `json:"connectionParams"`
			} `json:"connections"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&integrations); err != nil {
			t.Fatalf("decoding: %v", err)
		}
		if len(integrations) != 1 {
			t.Fatalf("expected 1 integration, got %d", len(integrations))
		}

		wantFields := []credentialField{{Name: "api_key", Label: "API Key", Description: "Docs API key"}}
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
		if !reflect.DeepEqual(integrations[0].Connections[0].ConnectionParams, map[string]connectionParam{
			"tenant": {
				Required:    true,
				Description: "Tenant slug",
				Default:     "acme",
			},
		}) {
			t.Fatalf("connection params = %+v", integrations[0].Connections[0].ConnectionParams)
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
			cfg.Services = testutil.NewStubServices(t)
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
			cfg.Services = testutil.NewStubServices(t)
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
			cfg.Services = testutil.NewStubServices(t)
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
				Name string `json:"name"`
			} `json:"connections"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&integrations); err != nil {
			t.Fatalf("decoding: %v", err)
		}
		if len(integrations) != 1 {
			t.Fatalf("expected 1 integration, got %d", len(integrations))
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
		cfg.Services = testutil.NewStubServices(t)
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
				prov, err := declarative.Build(&declarative.Definition{
					Provider:    "example",
					DisplayName: "Example",
					Auth:        declarative.AuthDef{Type: "manual"},
					CredentialFields: []declarative.CredentialFieldDef{
						{Name: "primary_token", Label: "Primary Token"},
						{Name: "secondary_token", Label: "Secondary Token"},
					},
					Operations: map[string]declarative.OperationDef{
						"list_items": {Method: http.MethodGet, Path: "/items"},
					},
				}, declarative.ConnectionDef{})
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
				cfg.Services = testutil.NewStubServices(t)
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
		prov, err := declarative.Build(&declarative.Definition{
			Provider:    "iconprov",
			DisplayName: "Icon Provider",
			Description: "Has an icon",
			IconSVG:     testSVG,
			BaseURL:     "https://api.example.com",
			Auth:        declarative.AuthDef{Type: "manual"},
			Operations: map[string]declarative.OperationDef{
				"op": {Description: "An op", Method: http.MethodGet, Path: "/op"},
			},
		}, declarative.ConnectionDef{})
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		return prov
	}

	assertIcon := func(t *testing.T, prov core.Provider) {
		t.Helper()
		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, prov)
			cfg.Services = testutil.NewStubServices(t)
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

	svc := testutil.NewStubServices(t)
	u := seedUserRecord(t, svc, "user-a", "user@example.com", time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	seedToken(t, svc, &core.ExternalCredential{
		ID: "tok1", SubjectID: principal.UserSubjectID(u.ID), ConnectionID: "slack:default",
		Integration: "slack", Connection: "default", Instance: "default", AccessToken: "test-token",
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
		cfg.PluginDefs = testPluginDefsForConnections("slack", "default")
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
		Name            string `json:"name"`
		Status          string `json:"status"`
		CredentialState string `json:"credentialState"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&integrations); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(integrations) != 2 {
		t.Fatalf("expected 2 integrations, got %d", len(integrations))
	}

	states := make(map[string]struct {
		Status          string
		CredentialState string
	})
	for _, i := range integrations {
		states[i.Name] = struct {
			Status          string
			CredentialState string
		}{Status: i.Status, CredentialState: i.CredentialState}
	}
	if states["slack"].Status != "ready" || states["slack"].CredentialState != "connected" {
		t.Fatalf("expected slack to be connected, got %+v", states["slack"])
	}
	if states["github"].Status != "needs_user_connection" || states["github"].CredentialState != "missing" {
		t.Fatalf("expected github to be disconnected, got %+v", states["github"])
	}
}

func TestListIntegrations_ShowsConnectedStatus_AmbiguousMixedCaseDuplicatesFailClosed(t *testing.T) {
	t.Parallel()

	for _, email := range []string{"user@example.com", "USER@example.com"} {
		email := email
		t.Run(email, func(t *testing.T) {
			t.Parallel()

			svc := testutil.NewStubServices(t)
			seedUserRecord(t, svc, "user-a", "User@example.com", time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
			seedUserRecord(t, svc, "user-b", "USER@example.com", time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC))

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

func TestListIntegrations_FindOrCreateUserError(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
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

func TestListIntegrations_ListCredentialsError(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
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

		svc := testutil.NewStubServices(t)
		recordingCreds := newRecordingExternalCredentialProvider(svc.ExternalCredentials)
		svc.ExternalCredentials = recordingCreds
		u := seedUser(t, svc, "anonymous@gestalt")
		externalIdentityID := testExternalIdentityResourceID("slack_identity", "team:T123:user:U456")
		authzProvider := newMemoryAuthorizationProvider("memory-authorization")
		baseAuthz, err := newTestAuthorizer(config.AuthorizationConfig{}, map[string]*config.ProviderEntry{})
		if err != nil {
			t.Fatalf("authorization.New: %v", err)
		}
		authz := mustProviderBackedAuthorizer(t, baseAuthz, authzProvider)
		seedToken(t, svc, &core.ExternalCredential{
			ID: "tok-1", SubjectID: principal.UserSubjectID(u.ID), ConnectionID: "slack:" + config.PluginConnectionName,
			Integration: "slack", Connection: "", Instance: "default", AccessToken: "test-token",
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
			cfg.PluginDefs = testPluginDefsForConnections("slack")
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
		tokens, err := listTestCredentialsForProvider(context.Background(), svc.ExternalCredentials, principal.UserSubjectID(u.ID), "slack")
		if err != nil {
			t.Fatalf("ListCredentialsForProvider: %v", err)
		}
		if len(tokens) != 0 {
			t.Fatalf("expected 0 tokens after disconnect, got %d", len(tokens))
		}
		if recordingCreds.listCredentialsCalls.Load() == 0 {
			t.Fatal("expected disconnect to list credentials through ExternalCredentialProvider")
		}
		if recordingCreds.deleteCredentialCalls.Load() == 0 {
			t.Fatal("expected disconnect to delete credentials through ExternalCredentialProvider")
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

		svc := testutil.NewStubServices(t)
		u := seedUser(t, svc, "anonymous@gestalt")
		externalIdentityID := testExternalIdentityResourceID("slack_identity", "team:T123:user:U456")
		authzProvider := newMemoryAuthorizationProvider("memory-authorization")
		baseAuthz, err := newTestAuthorizer(config.AuthorizationConfig{}, map[string]*config.ProviderEntry{})
		if err != nil {
			t.Fatalf("authorization.New: %v", err)
		}
		authz := mustProviderBackedAuthorizer(t, baseAuthz, authzProvider)
		seedToken(t, svc, &core.ExternalCredential{
			ID: "tok-1", SubjectID: principal.UserSubjectID(u.ID), ConnectionID: "slack:workspace",
			Integration: "slack", Connection: "workspace", Instance: "team-a", AccessToken: "test-token-a",
			MetadataJSON: `{"team_id":"T123","user_id":"U456","gestalt.external_identity.type":"slack_identity","gestalt.external_identity.id":"team:T123:user:U456"}`,
		})
		seedToken(t, svc, &core.ExternalCredential{
			ID: "tok-2", SubjectID: principal.UserSubjectID(u.ID), ConnectionID: "slack:workspace",
			Integration: "slack", Connection: "workspace", Instance: "team-b", AccessToken: "test-token-b",
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
			cfg.PluginDefs = testPluginDefsForConnections("slack", "workspace")
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
		tokens, err := listTestCredentialsForProvider(context.Background(), svc.ExternalCredentials, principal.UserSubjectID(u.ID), "slack")
		if err != nil {
			t.Fatalf("ListCredentialsForProvider: %v", err)
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

		svc := testutil.NewStubServices(t)
		u := seedUser(t, svc, "anonymous@gestalt")
		externalIdentityID := testExternalIdentityResourceID("slack_identity", "team:T123:user:U456")
		authzProvider := newMemoryAuthorizationProvider("memory-authorization")
		baseAuthz, err := newTestAuthorizer(config.AuthorizationConfig{}, map[string]*config.ProviderEntry{})
		if err != nil {
			t.Fatalf("authorization.New: %v", err)
		}
		authz := mustProviderBackedAuthorizer(t, baseAuthz, authzProvider)
		seedToken(t, svc, &core.ExternalCredential{
			ID: "tok-1", SubjectID: principal.UserSubjectID(u.ID), ConnectionID: "slack:" + config.PluginConnectionName,
			Integration: "slack", Connection: "", Instance: "default", AccessToken: "test-token",
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
		originalTokens, err := listTestCredentialsForProvider(context.Background(), svc.ExternalCredentials, principal.UserSubjectID(u.ID), "slack")
		if err != nil {
			t.Fatalf("ListCredentialsForProvider before disconnect: %v", err)
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
			cfg.PluginDefs = testPluginDefsForConnections("slack")
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
		tokens, err := listTestCredentialsForProvider(context.Background(), svc.ExternalCredentials, principal.UserSubjectID(u.ID), "slack")
		if err != nil {
			t.Fatalf("ListCredentialsForProvider: %v", err)
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

		svc := testutil.NewStubServices(t)
		u := seedUser(t, svc, "anonymous@gestalt")
		seedToken(t, svc, &core.ExternalCredential{
			ID: "tok-a", SubjectID: principal.UserSubjectID(u.ID), ConnectionID: "notion:mcp",
			Integration: "notion", Connection: "mcp", Instance: "MCP OAuth", AccessToken: "test-token",
		})
		seedToken(t, svc, &core.ExternalCredential{
			ID: "tok-b", SubjectID: principal.UserSubjectID(u.ID), ConnectionID: "notion:default",
			Integration: "notion", Connection: "default", Instance: "default", AccessToken: "test-token-2",
		})

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{N: "notion", DN: "Notion"})
			cfg.PluginDefs = testPluginDefsForConnections("notion", "mcp", "default")
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

		svc := testutil.NewStubServices(t)
		u := seedUser(t, svc, "anonymous@gestalt")
		seedToken(t, svc, &core.ExternalCredential{
			ID: "tok-b", SubjectID: principal.UserSubjectID(u.ID), ConnectionID: "slack:workspace",
			Integration: "slack", Connection: "workspace", Instance: "team-b", AccessToken: "test-token",
		})

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{N: "slack", DN: "Slack"})
			cfg.PluginDefs = testPluginDefsForConnections("slack", "workspace")
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

	t.Run("plain selectors are rejected for disconnect", func(t *testing.T) {
		t.Parallel()

		svc := testutil.NewStubServices(t)
		u := seedUser(t, svc, "anonymous@gestalt")
		seedToken(t, svc, &core.ExternalCredential{
			ID: "tok-b", SubjectID: principal.UserSubjectID(u.ID), ConnectionID: "notion:mcp",
			Integration: "notion", Connection: "mcp", Instance: "MCP OAuth", AccessToken: "test-token",
		})

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{N: "notion", DN: "Notion"})
			cfg.PluginDefs = testPluginDefsForConnections("notion", "mcp")
			cfg.Services = svc
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/integrations/notion?connection=mcp", nil)
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
		if !strings.Contains(result["error"], "unsupported query parameter") {
			t.Fatalf("expected unsupported query parameter error, got %q", result["error"])
		}
	})

	t.Run("ambiguous error uses canonical hint", func(t *testing.T) {
		t.Parallel()

		svc := testutil.NewStubServices(t)
		u := seedUser(t, svc, "anonymous@gestalt")
		var auditBuf bytes.Buffer
		seedToken(t, svc, &core.ExternalCredential{
			ID: "tok-a", SubjectID: principal.UserSubjectID(u.ID), ConnectionID: "slack:workspace",
			Integration: "slack", Connection: "workspace", Instance: "team-a", AccessToken: "test-token",
		})
		seedToken(t, svc, &core.ExternalCredential{
			ID: "tok-b", SubjectID: principal.UserSubjectID(u.ID), ConnectionID: "slack:workspace",
			Integration: "slack", Connection: "workspace", Instance: "team-b", AccessToken: "test-token-2",
		})

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{N: "slack", DN: "Slack"})
			cfg.PluginDefs = testPluginDefsForConnections("slack", "workspace")
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
		cfg.Services = testutil.NewStubServices(t)
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
		cfg.Services = testutil.NewStubServices(t)
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

	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.ExternalCredential{
		ID: "tok-cat", SubjectID: principal.UserSubjectID(u.ID), Integration: "test-int",
		Connection: testCatalogConnection, Instance: "default", AccessToken: testCatalogToken,
	})
	seedToken(t, svc, &core.ExternalCredential{
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
		t.Fatalf("query override list request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("query override list: expected 200, got %d: %s", resp.StatusCode, respBody)
	}
	ops = nil
	if err := json.NewDecoder(resp.Body).Decode(&ops); err != nil {
		t.Fatalf("decoding query override response: %v", err)
	}
	if len(ops) != 2 {
		t.Fatalf("expected 2 query override operations, got %d", len(ops))
	}
	if ops[0]["id"] != "alpha_rest" {
		t.Fatalf("expected first id 'alpha_rest' for query override, got %v", ops[0]["id"])
	}
	if ops[1]["id"] != "zeta_rest" {
		t.Fatalf("expected second id 'zeta_rest' for query override, got %v", ops[1]["id"])
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

	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.ExternalCredential{
		ID: "tok-mcp", SubjectID: principal.UserSubjectID(u.ID), Integration: "notion",
		Connection: "MCP", Instance: "default", AccessToken: "mcp-token",
	})
	seedToken(t, svc, &core.ExternalCredential{
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
	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.ExternalCredential{
		ID: "tok-catalog", SubjectID: principal.UserSubjectID(u.ID), Integration: "sample-int",
		Connection: "catalog-conn", Instance: "default", AccessToken: "catalog-token",
	})
	seedToken(t, svc, &core.ExternalCredential{
		ID: "tok-rest", SubjectID: principal.UserSubjectID(u.ID), Integration: "sample-int",
		Connection: "rest-conn", Instance: "default", AccessToken: "rest-token",
	})

	broker := invocation.NewBroker(
		providers,
		svc.Users,
		svc.ExternalCredentials,
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
	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.ExternalCredential{
		ID: "tok-rest", SubjectID: principal.UserSubjectID(u.ID), Integration: "sample-int",
		Connection: "rest-conn", Instance: "default", AccessToken: "rest-token",
	})

	broker := invocation.NewBroker(
		providers,
		svc.Users,
		svc.ExternalCredentials,
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

	svc := testutil.NewStubServices(t)
	seedAPIToken(t, svc, plaintext, hashed, "viewer-user")
	viewer := seedUser(t, svc, "viewer-user@test.local")
	seedToken(t, svc, &core.ExternalCredential{
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
		Policies: map[string]config.SubjectPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.SubjectPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(viewer.ID), Role: "viewer"},
				},
			},
		},
	}, pluginDefs)

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

	svc := testutil.NewStubServices(t)
	seedAPIToken(t, svc, plaintext, hashed, "viewer-user")
	viewer := seedUser(t, svc, "viewer-user@test.local")
	seedToken(t, svc, &core.ExternalCredential{
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
	baseAuthz, err := newTestAuthorizer(config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"sample_policy": {Default: "deny"},
		},
	}, pluginDefs)

	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}
	provider := newMemoryAuthorizationProvider("memory-authorization")
	authz := mustProviderBackedAuthorizer(t, baseAuthz, provider)
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

	svc := testutil.NewStubServices(t)
	seedAPIToken(t, svc, plaintext, hashed, "viewer-user")
	viewer := seedUser(t, svc, "viewer-user@test.local")
	seedToken(t, svc, &core.ExternalCredential{
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
		Policies: map[string]config.SubjectPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.SubjectPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(viewer.ID), Role: "viewer"},
				},
			},
		},
	}, pluginDefs)

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

func TestExecuteOperation_HumanAuthorizationUsesCanonicalSubjectOnCollision(t *testing.T) {
	t.Parallel()

	plaintext, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	svc := testutil.NewStubServices(t)
	seedAPIToken(t, svc, plaintext, hashed, "viewer-user")
	viewer := seedUser(t, svc, "viewer-user@test.local")
	seedToken(t, svc, &core.ExternalCredential{
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
		Policies: map[string]config.SubjectPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.SubjectPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(viewer.ID), Role: "viewer"},
				},
			},
		},
	}, pluginDefs)

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
			cfg.Services = testutil.NewStubServices(t)
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
		if errResp.Error != `no external credential stored for integration "test-int"; connect via OAuth first` {
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

		svc := testutil.NewStubServices(t)
		u := seedUser(t, svc, "anonymous@gestalt")
		seedToken(t, svc, &core.ExternalCredential{
			ID: "tok-a", SubjectID: principal.UserSubjectID(u.ID), Integration: "test-int",
			Connection: testCatalogConnection, Instance: "inst-a", AccessToken: "tok-a",
		})
		seedToken(t, svc, &core.ExternalCredential{
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
		if result["code"] != "instance_selection_required" {
			t.Fatalf("expected instance_selection_required code, got %q", result["code"])
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
			cfg.Services = testutil.NewStubServices(t)
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

	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.ExternalCredential{
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

	svc := testutil.NewStubServices(t)
	user, err := svc.Users.FindOrCreateUser(context.Background(), "wrapped@test.local")
	if err != nil {
		t.Fatalf("FindOrCreateUser: %v", err)
	}
	seedToken(t, svc, &core.ExternalCredential{
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
	apiProv := declarative.NewRestricted(merged, map[string]string{"find": "search"})
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
		cfg.Services = testutil.NewStubServices(t)
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

	svc := testutil.NewStubServices(t)
	user, err := svc.Users.FindOrCreateUser(context.Background(), "alias@test.local")
	if err != nil {
		t.Fatalf("FindOrCreateUser: %v", err)
	}
	seedToken(t, svc, &core.ExternalCredential{
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

func TestExecuteOperation_PlatformMissingRuntimeMaterialUsesAdminConfigurationError(t *testing.T) {
	t.Parallel()

	stub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "platform-svc",
			ConnMode: core.ConnectionModePlatform,
		},
		ops: []core.Operation{{Name: "do", Method: http.MethodGet}},
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Services = testutil.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/platform-svc/do", nil)
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
		t.Fatalf("decode error: %v", err)
	}
	if errResp.Code != "admin_configuration_required" {
		t.Fatalf("code = %q, want admin_configuration_required", errResp.Code)
	}
	if !strings.Contains(errResp.Error, "deployment/admin configuration") {
		t.Fatalf("error = %q, want deployment/admin copy", errResp.Error)
	}
	if errResp.Integration != "platform-svc" {
		t.Fatalf("integration = %q, want platform-svc", errResp.Integration)
	}
}

func TestExecuteOperation_DeclarativeRESTConnectionSelectorRoutesCredentialAndOmitsInternalParam(t *testing.T) {
	t.Parallel()

	var (
		mu    sync.Mutex
		calls []struct {
			path string
			auth string
			body map[string]any
		}
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/chat.postMessage", "/api/chat.scheduleMessage", "/api/views.open":
		default:
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		calls = append(calls, struct {
			path string
			auth string
			body map[string]any
		}{
			path: r.URL.Path,
			auth: r.Header.Get("Authorization"),
			body: body,
		})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(upstream.Close)

	manifest := &providermanifestv1.Manifest{
		Source:      "slack",
		DisplayName: "Slack",
		Spec: &providermanifestv1.Spec{
			DefaultConnection: "default",
			Connections: map[string]*providermanifestv1.ManifestConnectionDef{
				"default": {Mode: providermanifestv1.ConnectionModeUser},
				"bot": {
					Mode:     providermanifestv1.ConnectionModePlatform,
					Exposure: providermanifestv1.ConnectionExposureInternal,
					Auth:     &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeBearer},
				},
			},
			Surfaces: &providermanifestv1.ProviderSurfaces{
				REST: &providermanifestv1.RESTSurface{
					Connection: "default",
					BaseURL:    upstream.URL,
					Operations: []providermanifestv1.ProviderOperation{
						{
							Name:        "chat.postMessage",
							Description: "Send a Slack message",
							Method:      http.MethodPost,
							Path:        "/api/chat.postMessage",
							ConnectionSelector: &providermanifestv1.OperationConnectionSelector{
								Parameter: "actor",
								Default:   "user",
								Values: map[string]string{
									"bot":  "bot",
									"user": "default",
								},
							},
							Parameters: []providermanifestv1.ProviderParameter{
								{Name: "channel", Type: "string", In: "body", Required: true},
								{Name: "text", Type: "string", In: "body", Required: true},
								{Name: "actor", Type: "string", In: "body", Internal: true},
							},
						},
						{
							Name:        "chat.scheduleMessage",
							Description: "Schedule a Slack message",
							Method:      http.MethodPost,
							Path:        "/api/chat.scheduleMessage",
							Parameters: []providermanifestv1.ProviderParameter{
								{Name: "channel", Type: "string", In: "body", Required: true},
								{Name: "text", Type: "string", In: "body", Required: true},
								{Name: "post_at", Type: "int", In: "body", Required: true},
							},
						},
						{
							Name:        "assistant.threads.setStatus",
							Description: "Set assistant status",
							Method:      http.MethodPost,
							Path:        "/api/assistant.threads.setStatus",
							Connection:  "bot",
							Parameters: []providermanifestv1.ProviderParameter{
								{Name: "channel_id", Type: "string", In: "body", Required: true},
								{Name: "thread_ts", Type: "string", In: "body", Required: true},
								{Name: "status", Type: "string", In: "body", Required: true},
							},
						},
						{
							Name:        "views.open",
							Description: "Open a Slack view",
							Method:      http.MethodPost,
							Path:        "/api/views.open",
							ConnectionSelector: &providermanifestv1.OperationConnectionSelector{
								Parameter: "audience",
								Default:   "user",
								Values: map[string]string{
									"bot":  "bot",
									"user": "default",
								},
							},
							Parameters: []providermanifestv1.ProviderParameter{
								{Name: "trigger_id", Type: "string", In: "body", Required: true},
								{Name: "audience", Type: "string", In: "body"},
							},
						},
					},
				},
			},
		},
	}
	entry := &config.ProviderEntry{
		ResolvedManifest: manifest,
		Connections: map[string]*config.ConnectionDef{
			"bot": {
				Mode: providermanifestv1.ConnectionModePlatform,
				Auth: config.ConnectionAuthDef{
					Type:  providermanifestv1.AuthTypeBearer,
					Token: "bot-slack-token",
				},
			},
		},
	}
	plan, err := config.BuildStaticConnectionPlan(entry, manifest.Spec)
	if err != nil {
		t.Fatalf("BuildStaticConnectionPlan: %v", err)
	}
	restConnections, restSelectors, restLocks, err := plan.RESTOperationConnectionBindings(manifest.Spec)
	if err != nil {
		t.Fatalf("RESTOperationConnectionBindings: %v", err)
	}
	prov, err := pluginservice.NewDeclarativeProvider(
		manifest,
		upstream.Client(),
		pluginservice.WithDeclarativeConnectionMode(plan.ConnectionMode()),
		pluginservice.WithDeclarativeOperationConnections(restConnections, restSelectors, restLocks),
	)
	if err != nil {
		t.Fatalf("NewDeclarativeProvider: %v", err)
	}

	svc := testutil.NewStubServices(t)
	user, err := svc.Users.FindOrCreateUser(context.Background(), "selector@test.local")
	if err != nil {
		t.Fatalf("FindOrCreateUser: %v", err)
	}
	subjectID := principal.UserSubjectID(user.ID)
	seedToken(t, svc, &core.ExternalCredential{
		ID:          "slack-default",
		SubjectID:   subjectID,
		Integration: "slack",
		Connection:  "default",
		Instance:    "default",
		AccessToken: "user-slack-token",
	})
	connectionRuntime, err := bootstrap.BuildConnectionRuntime(&config.Config{
		Plugins: map[string]*config.ProviderEntry{"slack": entry},
	})
	if err != nil {
		t.Fatalf("BuildConnectionRuntime: %v", err)
	}
	if runtimeInfo, ok := connectionRuntime.Resolve("slack", "bot"); !ok || runtimeInfo.Mode != core.ConnectionModePlatform || runtimeInfo.Token != "bot-slack-token" {
		t.Fatalf("runtime bot connection = (%+v, %v), want configured platform token", runtimeInfo, ok)
	}
	broker := invocation.NewBroker(
		testutil.NewProviderRegistry(t, prov),
		svc.Users,
		svc.ExternalCredentials,
		invocation.WithConnectionRuntime(connectionRuntime.Resolve),
	)
	if _, token, err := broker.ResolveToken(context.Background(), &principal.Principal{SubjectID: subjectID}, "slack", "bot", ""); !errors.Is(err, invocation.ErrAuthorizationDenied) || token != "" {
		t.Fatalf("ResolveToken public bot = token %q, err %v; want authorization denied", token, err)
	}
	trustedCtx := invocation.WithHTTPBinding(invocation.WithInvocationSurface(context.Background(), invocation.InvocationSurfaceHTTPBinding), "event")
	if _, token, err := broker.ResolveToken(trustedCtx, &principal.Principal{SubjectID: subjectID}, "slack", "bot", ""); err != nil || token != "bot-slack-token" {
		t.Fatalf("ResolveToken trusted bot = token %q, err %v; want platform token", token, err)
	}
	metrics := metrictest.NewManualMeterProvider(t)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.MeterProvider = metrics.Provider
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "api-token" {
					return nil, fmt.Errorf("bad token")
				}
				return &core.UserIdentity{Email: "selector@test.local"}, nil
			},
		}
		cfg.Providers = testutil.NewProviderRegistry(t, prov)
		cfg.Services = svc
		cfg.PluginDefs = map[string]*config.ProviderEntry{"slack": entry}
		cfg.Invoker = broker
	})
	testutil.CloseOnCleanup(t, ts)

	integrationsReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	integrationsReq.Header.Set("Authorization", "Bearer api-token")
	integrationsResp, err := http.DefaultClient.Do(integrationsReq)
	if err != nil {
		t.Fatalf("integrations request: %v", err)
	}
	defer func() { _ = integrationsResp.Body.Close() }()
	if integrationsResp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(integrationsResp.Body)
		t.Fatalf("integrations status = %d: %s", integrationsResp.StatusCode, payload)
	}
	var integrations []struct {
		Name            string `json:"name"`
		Status          string `json:"status"`
		CredentialState string `json:"credentialState"`
		Connections     []struct {
			Name            string   `json:"name"`
			Mode            string   `json:"mode"`
			Status          string   `json:"status"`
			CredentialState string   `json:"credentialState"`
			Actions         []string `json:"actions"`
			AuthTypes       []string `json:"authTypes"`
		} `json:"connections"`
	}
	if err := json.NewDecoder(integrationsResp.Body).Decode(&integrations); err != nil {
		t.Fatalf("decode integrations: %v", err)
	}
	var defaultConnection *struct {
		Name            string   `json:"name"`
		Mode            string   `json:"mode"`
		Status          string   `json:"status"`
		CredentialState string   `json:"credentialState"`
		Actions         []string `json:"actions"`
		AuthTypes       []string `json:"authTypes"`
	}
	var slackStatus, slackCredentialState string
	for i := range integrations {
		if integrations[i].Name != "slack" {
			continue
		}
		slackStatus = integrations[i].Status
		slackCredentialState = integrations[i].CredentialState
		for j := range integrations[i].Connections {
			if integrations[i].Connections[j].Name == "default" {
				defaultConnection = &integrations[i].Connections[j]
			}
			if integrations[i].Connections[j].Name == "bot" {
				t.Fatalf("internal bot connection leaked in integrations response: %+v", integrations[i].Connections[j])
			}
		}
	}
	if defaultConnection == nil {
		t.Fatal("default connection missing from integrations response")
	}
	if slackStatus != "ready" || slackCredentialState != "connected" {
		t.Fatalf("slack status = {%q, %q}, want ready/connected", slackStatus, slackCredentialState)
	}
	if defaultConnection.Mode != "user" || defaultConnection.Status != "ready" || defaultConnection.CredentialState != "connected" || !reflect.DeepEqual(defaultConnection.Actions, []string{"disconnect", "add_instance"}) {
		t.Fatalf("default connection metadata = %+v, want connected user connection", *defaultConnection)
	}
	for _, tc := range []struct {
		name string
		path string
		body string
	}{
		{
			name: "oauth",
			path: "/api/v1/auth/start-oauth",
			body: `{"integration":"slack","connection":"bot"}`,
		},
		{
			name: "manual",
			path: "/api/v1/auth/connect-manual",
			body: `{"integration":"slack","connection":"bot","credential":"user-supplied"}`,
		},
	} {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+tc.path, strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer api-token")
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s connect request: %v", tc.name, err)
		}
		payload, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(payload), "deployment-managed") {
			t.Fatalf("%s connect response = %d %s, want 400 deployment-managed", tc.name, resp.StatusCode, payload)
		}
	}

	opsReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations/slack/operations", nil)
	opsReq.Header.Set("Authorization", "Bearer api-token")
	opsResp, err := http.DefaultClient.Do(opsReq)
	if err != nil {
		t.Fatalf("operations request: %v", err)
	}
	if opsResp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(opsResp.Body)
		_ = opsResp.Body.Close()
		t.Fatalf("operations status = %d: %s", opsResp.StatusCode, payload)
	}
	var ops []catalog.CatalogOperation
	if err := json.NewDecoder(opsResp.Body).Decode(&ops); err != nil {
		_ = opsResp.Body.Close()
		t.Fatalf("decode operations: %v", err)
	}
	_ = opsResp.Body.Close()
	seenOps := map[string]catalog.CatalogOperation{}
	for _, op := range ops {
		seenOps[op.ID] = op
	}
	if _, ok := seenOps["assistant.threads.setStatus"]; ok {
		t.Fatal("internal bot-only operation leaked in public operations response")
	}
	postMessage, ok := seenOps["chat.postMessage"]
	if !ok {
		t.Fatal("chat.postMessage missing from public operations response")
	}
	for _, param := range postMessage.Parameters {
		if param.Name == "actor" {
			t.Fatal("internal actor parameter leaked in public operations response")
		}
	}
	if strings.Contains(string(postMessage.InputSchema), "actor") {
		t.Fatalf("public chat.postMessage input schema contains internal actor parameter: %s", postMessage.InputSchema)
	}
	viewsOpen, ok := seenOps["views.open"]
	if !ok {
		t.Fatal("views.open missing from public operations response")
	}
	var viewsSchema map[string]any
	if err := json.Unmarshal(viewsOpen.InputSchema, &viewsSchema); err != nil {
		t.Fatalf("unmarshal views.open schema: %v", err)
	}
	viewsProps, _ := viewsSchema["properties"].(map[string]any)
	audienceSchema, _ := viewsProps["audience"].(map[string]any)
	audienceEnum, _ := audienceSchema["enum"].([]any)
	if len(audienceEnum) != 1 || audienceEnum[0] != "user" {
		t.Fatalf("views.open audience enum = %#v, want [user]", audienceEnum)
	}
	if strings.Contains(string(viewsOpen.InputSchema), "bot") {
		t.Fatalf("public views.open input schema contains internal selector value: %s", viewsOpen.InputSchema)
	}
	cachedViewsOpen, ok := invocation.CatalogOperation(prov.Catalog(), "views.open")
	if !ok {
		t.Fatal("views.open missing from provider catalog")
	}
	for _, param := range cachedViewsOpen.Parameters {
		if param.Name == "audience" && param.Default != nil {
			t.Fatalf("cached views.open audience default mutated = %#v, want nil", param.Default)
		}
	}

	doInvoke := func(operation, body string) int {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/slack/"+operation, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer api-token")
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		payload, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode >= 500 {
			t.Fatalf("status = %d: %s", resp.StatusCode, payload)
		}
		if resp.StatusCode >= 400 {
			t.Logf("%s response = %d: %s", operation, resp.StatusCode, payload)
		}
		return resp.StatusCode
	}

	if status := doInvoke("chat.postMessage", `{"channel":"C1","text":"as user"}`); status != http.StatusOK {
		t.Fatalf("default user status = %d, want %d", status, http.StatusOK)
	}
	if status := doInvoke("chat.postMessage", `{"channel":"C1","text":"bad actor","actor":"user"}`); status != http.StatusBadRequest {
		t.Fatalf("hidden actor status = %d, want %d", status, http.StatusBadRequest)
	}
	if status := doInvoke("chat.scheduleMessage?_connection=default", `{"channel":"C1","text":"scheduled","post_at":4102444800}`); status != http.StatusOK {
		t.Fatalf("surface fallback override status = %d, want %d", status, http.StatusOK)
	}
	if status := doInvoke("chat.scheduleMessage?_connection=bot", `{"channel":"C1","text":"scheduled","post_at":4102444800}`); status != http.StatusForbidden {
		t.Fatalf("internal connection override status = %d, want %d", status, http.StatusForbidden)
	}
	if status := doInvoke("assistant.threads.setStatus", `{"channel_id":"C1","thread_ts":"1.0","status":"thinking"}`); status != http.StatusForbidden {
		t.Fatalf("internal operation status = %d, want %d", status, http.StatusForbidden)
	}
	if status := doInvoke("views.open", `{"trigger_id":"T1","audience":"user"}`); status != http.StatusOK {
		t.Fatalf("non-internal selector status = %d, want %d", status, http.StatusOK)
	}
	if status := doInvoke("views.open", `{"trigger_id":"T1","audience":"bot"}`); status != http.StatusForbidden {
		t.Fatalf("internal selector status = %d, want %d", status, http.StatusForbidden)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	httpOperationAttrs := map[string]string{
		"http.route":                  "/api/v1/{integration}/{operation}",
		"gestaltd.provider.name":      "slack",
		"gestaltd.operation.name":     "chat.postMessage",
		"gestaltd.invocation.surface": "http",
	}
	userAttrs := maps.Clone(httpOperationAttrs)
	userAttrs["gestaltd.connection.mode"] = "user"
	metrictest.RequireFloat64Histogram(t, rm, "http.server.request.duration", userAttrs)

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 3 {
		t.Fatalf("upstream calls = %d, want 3", len(calls))
	}
	if calls[0].path != "/api/chat.postMessage" {
		t.Fatalf("first call path = %q, want chat.postMessage", calls[0].path)
	}
	if calls[0].auth != "Bearer user-slack-token" {
		t.Fatalf("first call auth = %q, want user token", calls[0].auth)
	}
	if _, ok := calls[0].body["actor"]; ok {
		t.Fatalf("first upstream body included internal actor param: %+v", calls[0].body)
	}
	if calls[1].path != "/api/chat.scheduleMessage" {
		t.Fatalf("second call path = %q, want chat.scheduleMessage", calls[1].path)
	}
	if calls[1].auth != "Bearer user-slack-token" {
		t.Fatalf("second call auth = %q, want user token", calls[1].auth)
	}
	if calls[1].body["text"] != "scheduled" {
		t.Fatalf("second upstream body text = %#v, want scheduled", calls[1].body["text"])
	}
	if calls[2].path != "/api/views.open" {
		t.Fatalf("third call path = %q, want views.open", calls[2].path)
	}
	if calls[2].auth != "Bearer user-slack-token" {
		t.Fatalf("third call auth = %q, want user token", calls[2].auth)
	}
	if calls[2].body["audience"] != "user" {
		t.Fatalf("third upstream body audience = %#v, want user", calls[2].body["audience"])
	}
}

func TestExecuteOperation_UnknownIntegration(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Services = testutil.NewStubServices(t)
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
		cfg.Services = testutil.NewStubServices(t)
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

	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.ExternalCredential{
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
		cfg.Services = testutil.NewStubServices(t)
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
		cfg.Services = testutil.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

}

func TestSubjectAuthorization_ExecuteOperation_UsesSubjectCredentialAndSessionSelectors(t *testing.T) {
	t.Parallel()

	subjectToken, subjectTokenHash, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	svc := testutil.NewStubServices(t)
	seedSubjectAPIToken(t, svc, subjectTokenHash, "service_account:triage-bot", "triage-bot")
	seedSubjectToken(t, svc, "service_account:triage-bot", "svc", "workspace", "team-a", "identity-bound-token")

	stub := &stubIntegrationWithCatalog{
		StubIntegration: coretesting.StubIntegration{
			N:        "svc",
			ConnMode: core.ConnectionModeUser,
			ExecuteFn: func(_ context.Context, op string, _ map[string]any, token string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: fmt.Sprintf(`{"operation":%q,"token":%q}`, op, token)}, nil
			},
		},
		catalog: serverTestCatalog("svc", []catalog.CatalogOperation{
			{ID: "run", Method: http.MethodGet, Transport: catalog.TransportREST, AllowedRoles: []string{"viewer"}},
			{ID: "admin", Method: http.MethodGet, Transport: catalog.TransportREST, AllowedRoles: []string{"admin"}},
		}),
	}
	providers := testutil.NewProviderRegistry(t, stub)
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"svc_policy": {
				Default: "allow",
				Members: []config.SubjectPolicyMemberDef{{
					SubjectID: "service_account:triage-bot",
					Role:      "viewer",
				}},
			},
		},
	}, map[string]*config.ProviderEntry{"svc": {AuthorizationPolicy: "svc_policy"}})

	var auditBuf bytes.Buffer
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
		cfg.Providers = providers
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.AuditSink = invocation.NewSlogAuditSink(&auditBuf)
		cfg.DefaultConnection = map[string]string{"svc": "workspace"}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/svc/run", nil)
	req.Header.Set("Authorization", "Bearer "+subjectToken)
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
		t.Fatalf("expected service account credential token, got %v", result["token"])
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/v1/svc/admin", nil)
	req.Header.Set("Authorization", "Bearer "+subjectToken)
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
	req.Header.Set("Authorization", "Bearer "+subjectToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("selector request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 for requested instance, got %d: %s", resp.StatusCode, body)
	}

}

func TestSubjectAuthorization_ExecuteOperation_AllowsPolicylessPlugin(t *testing.T) {
	t.Parallel()

	subjectToken, subjectTokenHash, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	svc := testutil.NewStubServices(t)
	seedSubjectAPIToken(t, svc, subjectTokenHash, "service_account:triage-bot", "triage-bot")
	stub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "svc",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(_ context.Context, op string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: fmt.Sprintf(`{"operation":%q}`, op)}, nil
			},
		},
		ops: []core.Operation{{Name: "run", Method: http.MethodGet}},
	}
	authz := mustAuthorizer(t, config.AuthorizationConfig{}, nil)
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Services = svc
		cfg.Authorizer = authz
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/svc/run", nil)
	req.Header.Set("Authorization", "Bearer "+subjectToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 for policyless service account plugin, got %d: %s", resp.StatusCode, body)
	}
}

func TestHumanAuthorization_ExecuteOperation_UsesResolvedRoleAndRejectsDisallowedOperations(t *testing.T) {
	t.Parallel()

	plaintext, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	svc := testutil.NewStubServices(t)
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
		Policies: map[string]config.SubjectPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.SubjectPolicyMemberDef{
					staticPolicyUserMember(t, svc, "viewer-user@test.local", "viewer"),
					{SubjectID: principal.UserSubjectID(viewer.ID), Role: "viewer"},
				},
			},
		},
	}, pluginDefs)

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

	svc := testutil.NewStubServices(t)
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
		Policies: map[string]config.SubjectPolicyDef{
			"sample_policy": {
				Default: "allow",
				Members: []config.SubjectPolicyMemberDef{
					staticPolicyUserMember(t, svc, "admin-user@test.local", "admin"),
				},
			},
		},
	}, pluginDefs)

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

	svc := testutil.NewStubServices(t)
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
	baseAuthz, err := newTestAuthorizer(config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"sample_policy": {Default: "deny"},
		},
	}, pluginDefs)

	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}
	provider := newMemoryAuthorizationProvider("memory-authorization")
	authz := mustProviderBackedAuthorizer(t, baseAuthz, provider)
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

func TestSubjectAuthorization_ExecuteOperation_MissingSubjectCredentialReturns412(t *testing.T) {
	t.Parallel()

	subjectToken, subjectTokenHash, err := principal.GenerateToken(principal.TokenTypeAPI)
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
		Policies: map[string]config.SubjectPolicyDef{
			"svc_policy": {
				Default: "allow",
				Members: []config.SubjectPolicyMemberDef{{
					SubjectID: "service_account:triage-bot",
					Role:      "viewer",
				}},
			},
		},
	}, map[string]*config.ProviderEntry{"svc": {AuthorizationPolicy: "svc_policy"}})

	svc := testutil.NewStubServices(t)
	seedSubjectAPIToken(t, svc, subjectTokenHash, "service_account:triage-bot", "triage-bot")
	broker := invocation.NewBroker(providers, svc.Users, svc.ExternalCredentials, invocation.WithAuthorizer(authz))
	guarded := invocation.NewGuarded(broker, broker, "http", auditSink, invocation.WithoutRateLimit())

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
		cfg.Providers = providers
		cfg.Services = svc
		cfg.Authorizer = authz
		cfg.AuditSink = auditSink
		cfg.Invoker = guarded
		cfg.DefaultConnection = map[string]string{"svc": "workspace"}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/svc/run", nil)
	req.Header.Set("Authorization", "Bearer "+subjectToken)
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
	if record["subject_id"] != "service_account:triage-bot" {
		t.Fatalf("expected service account subject_id, got %v", record["subject_id"])
	}
}

func TestSubjectAuthorization_UserOnlyRoutesReturn403ForNonUserSubjects(t *testing.T) {
	t.Parallel()

	subjectToken, subjectTokenHash, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	svc := testutil.NewStubServices(t)
	seedSubjectAPIToken(t, svc, subjectTokenHash, "service_account:triage-bot", "triage-bot")

	stub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{N: "weather", ConnMode: core.ConnectionModeNone},
		ops:             []core.Operation{{Name: "forecast", Method: http.MethodGet}},
	}
	providers := testutil.NewProviderRegistry(t, stub)
	authz := mustAuthorizer(t, config.AuthorizationConfig{}, nil)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
		cfg.Providers = providers
		cfg.Services = svc
		cfg.Authorizer = authz
	})
	testutil.CloseOnCleanup(t, ts)

	for _, request := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodPost, path: "/api/v1/tokens", body: `{"name":"bot-token"}`},
		{method: http.MethodPost, path: "/api/v1/auth/logout"},
	} {
		req, _ := http.NewRequest(request.method, ts.URL+request.path, bytes.NewBufferString(request.body))
		req.Header.Set("Authorization", "Bearer "+subjectToken)
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

		svc := testutil.NewStubServices(t)
		u := seedUser(t, svc, "anonymous@gestalt")
		seedToken(t, svc, &core.ExternalCredential{
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
		svc := testutil.NewStubServices(t)
		u := seedUser(t, svc, "anonymous@gestalt")
		seedToken(t, svc, &core.ExternalCredential{
			ID: "tok-mcp", SubjectID: principal.UserSubjectID(u.ID), Integration: "test-int",
			Connection: "mcp-conn", Instance: "default", AccessToken: "mcp-token",
		})
		seedToken(t, svc, &core.ExternalCredential{
			ID: "tok-cat", SubjectID: principal.UserSubjectID(u.ID), Integration: "test-int",
			Connection: "catalog-conn", Instance: "default", AccessToken: "catalog-token",
		})

		broker := invocation.NewBroker(
			providers,
			svc.Users,
			svc.ExternalCredentials,
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
	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.ExternalCredential{
		ID: "tok-rest", SubjectID: principal.UserSubjectID(u.ID), Integration: "sample-int",
		Connection: "rest-conn", Instance: "default", AccessToken: "rest-token",
	})

	broker := invocation.NewBroker(
		providers,
		svc.Users,
		svc.ExternalCredentials,
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
	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.ExternalCredential{
		ID: "tok-mcp", SubjectID: principal.UserSubjectID(u.ID), Integration: "sample-int",
		Connection: "mcp-conn", Instance: "default", AccessToken: "mcp-token",
	})
	seedToken(t, svc, &core.ExternalCredential{
		ID: "tok-rest", SubjectID: principal.UserSubjectID(u.ID), Integration: "sample-int",
		Connection: "rest-conn", Instance: "default", AccessToken: "rest-token",
	})

	broker := invocation.NewBroker(
		providers,
		svc.Users,
		svc.ExternalCredentials,
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
	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.ExternalCredential{
		ID: "tok-catalog", SubjectID: principal.UserSubjectID(u.ID), Integration: "sample-int",
		Connection: "catalog-conn", Instance: "default", AccessToken: "catalog-token",
	})
	seedToken(t, svc, &core.ExternalCredential{
		ID: "tok-rest", SubjectID: principal.UserSubjectID(u.ID), Integration: "sample-int",
		Connection: "rest-conn", Instance: "default", AccessToken: "rest-token",
	})

	broker := invocation.NewBroker(
		providers,
		svc.Users,
		svc.ExternalCredentials,
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
	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.ExternalCredential{
		ID: "tok-catalog", SubjectID: principal.UserSubjectID(u.ID), Integration: "sample-int",
		Connection: "catalog-conn", Instance: "default", AccessToken: "catalog-token",
	})
	seedToken(t, svc, &core.ExternalCredential{
		ID: "tok-rest", SubjectID: principal.UserSubjectID(u.ID), Integration: "sample-int",
		Connection: "rest-conn", Instance: "default", AccessToken: "rest-token",
	})

	broker := invocation.NewBroker(
		providers,
		svc.Users,
		svc.ExternalCredentials,
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
	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.ExternalCredential{
		ID: "tok-catalog", SubjectID: principal.UserSubjectID(u.ID), Integration: "sample-int",
		Connection: "catalog-conn", Instance: "default", AccessToken: "catalog-token",
	})
	seedToken(t, svc, &core.ExternalCredential{
		ID: "tok-rest", SubjectID: principal.UserSubjectID(u.ID), Integration: "sample-int",
		Connection: "rest-conn", Instance: "default", AccessToken: "rest-token",
	})

	broker := invocation.NewBroker(
		providers,
		svc.Users,
		svc.ExternalCredentials,
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

	svc := testutil.NewStubServices(t)
	existing := seedUserRecord(t, svc, "user-existing", "user@example.com", time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
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
		t.Fatalf("expected user email %q, got %q", "user@example.com", stored.Email)
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

	svc := testutil.NewStubServices(t)
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

	svc := testutil.NewStubServices(t)
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
		cfg.Services = testutil.NewStubServices(t)
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
		cfg.Services = testutil.NewStubServices(t)
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

func TestStartIntegrationOAuth_ServiceAccountDeniedByPolicy(t *testing.T) {
	t.Parallel()

	subjectToken, subjectTokenHash, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	svc := testutil.NewStubServices(t)
	seedSubjectAPIToken(t, svc, subjectTokenHash, "service_account:triage-bot", "triage-bot")
	stub := &stubIntegrationWithAuthURL{
		StubIntegration: coretesting.StubIntegration{N: "slack"},
		authURL:         "https://slack.com/oauth/v2/authorize",
	}
	handler := &testOAuthHandler{
		authorizationBaseURLVal: "https://slack.com/oauth/v2/authorize",
	}
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"slack_policy": {
				Default: "deny",
			},
		},
	}, map[string]*config.ProviderEntry{"slack": {AuthorizationPolicy: "slack_policy"}})
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"slack": testDefaultConnection}
		cfg.ConnectionAuth = testConnectionAuth("slack", handler)
		cfg.Authorizer = authz
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"integration":"slack","scopes":["channels:read"]}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/start-oauth", body)
	req.Header.Set("Authorization", "Bearer "+subjectToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 403, got %d: %s", resp.StatusCode, body)
	}
}

func TestIntegrationOAuthCallback(t *testing.T) {
	t.Parallel()

	const pendingSelectionPath = "/api/v1/auth/pending-connection"

	t.Run("connected", func(t *testing.T) {
		t.Parallel()

		var auditBuf bytes.Buffer
		svc := testutil.NewStubServices(t)
		recordingCreds := newRecordingExternalCredentialProvider(svc.ExternalCredentials)
		svc.ExternalCredentials = recordingCreds
		externalIdentityID := testExternalIdentityResourceID("slack_identity", "team:T123:user:U456")
		authzProvider := newMemoryAuthorizationProvider("memory-authorization")
		baseAuthz, err := newTestAuthorizer(config.AuthorizationConfig{}, map[string]*config.ProviderEntry{})
		if err != nil {
			t.Fatalf("authorization.New: %v", err)
		}
		authz := mustProviderBackedAuthorizer(t, baseAuthz, authzProvider)

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
		tokens, _ := svc.ExternalCredentials.ListCredentials(context.Background(), principal.UserSubjectID(u.ID))
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
		if recordingCreds.getCredentialCalls.Load() == 0 {
			t.Fatal("expected oauth callback to load credentials through ExternalCredentialProvider")
		}
		if recordingCreds.putCredentialCalls.Load() == 0 {
			t.Fatal("expected oauth callback to store credentials through ExternalCredentialProvider")
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

		svc := testutil.NewStubServices(t)
		externalIdentityID := testExternalIdentityResourceID("slack_identity", "team:T123:user:U456")
		authzProvider := newMemoryAuthorizationProvider("memory-authorization")
		baseAuthz, err := newTestAuthorizer(config.AuthorizationConfig{}, map[string]*config.ProviderEntry{})
		if err != nil {
			t.Fatalf("authorization.New: %v", err)
		}
		authz := mustProviderBackedAuthorizer(t, baseAuthz, authzProvider)
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
		tokens, _ := svc.ExternalCredentials.ListCredentials(context.Background(), principal.UserSubjectID(u.ID))
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

		svc := testutil.NewStubServices(t)
		externalIdentityID := testExternalIdentityResourceID("slack_identity", "team:T123:user:U456")
		authzProvider := newMemoryAuthorizationProvider("memory-authorization")
		baseAuthz, err := newTestAuthorizer(config.AuthorizationConfig{}, map[string]*config.ProviderEntry{})
		if err != nil {
			t.Fatalf("authorization.New: %v", err)
		}
		authz := mustProviderBackedAuthorizer(t, baseAuthz, authzProvider)

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
		adminTokens, _ := svc.ExternalCredentials.ListCredentials(context.Background(), principal.UserSubjectID(admin.ID))
		if len(adminTokens) != 1 {
			t.Fatalf("expected one admin token, got %d", len(adminTokens))
		}
		viewerTokens, _ := svc.ExternalCredentials.ListCredentials(context.Background(), principal.UserSubjectID(viewer.ID))
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
		cfg.Services = testutil.NewStubServices(t)
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

func TestCreateAPIToken_WithActionPermissions(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Services = testutil.NewStubServices(t)
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "roadmap",
			ConnMode: core.ConnectionModeNone,
		})
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"name":"attach-token","permissions":[{"plugin":"roadmap","actions":["provider_dev.attach"]}]}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/tokens", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("create status = %d, want 201: %s", resp.StatusCode, respBody)
	}
	var created struct {
		ID          string                  `json:"id"`
		Token       string                  `json:"token"`
		Permissions []core.AccessPermission `json:"permissions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.ID == "" || created.Token == "" {
		t.Fatalf("create response missing id/token: %+v", created)
	}
	if len(created.Permissions) != 1 || created.Permissions[0].Plugin != "roadmap" || len(created.Permissions[0].Actions) != 1 || created.Permissions[0].Actions[0] != core.ProviderActionDevAttach || len(created.Permissions[0].Operations) != 0 {
		t.Fatalf("created permissions = %#v", created.Permissions)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/v1/tokens", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("list status = %d, want 200: %s", resp.StatusCode, respBody)
	}
	var listed []struct {
		ID          string                  `json:"id"`
		Permissions []core.AccessPermission `json:"permissions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID || len(listed[0].Permissions) != 1 || len(listed[0].Permissions[0].Actions) != 1 || listed[0].Permissions[0].Actions[0] != core.ProviderActionDevAttach {
		t.Fatalf("listed tokens = %#v", listed)
	}
}

func TestListAPITokensListsOwnedUserRecords(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)

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

	svc := testutil.NewStubServices(t)
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
		cfg.Services = testutil.NewStubServices(t)
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
	svc := testutil.NewStubServices(t)
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

	svc := testutil.NewStubServices(t)
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
		cfg.Services = testutil.NewStubServices(t)
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

	svc := testutil.NewStubServices(t)
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
		cfg.Services = testutil.NewStubServices(t)
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

	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.ExternalCredential{
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
		cfg.Authorizer = mustAuthorizer(t, config.AuthorizationConfig{}, cfg.PluginDefs)
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
		cfg.Authorizer = mustAuthorizer(t, config.AuthorizationConfig{}, cfg.PluginDefs)
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
		cfg.Authorizer = mustAuthorizer(t, config.AuthorizationConfig{}, cfg.PluginDefs)
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
		cfg.Authorizer = mustAuthorizer(t, config.AuthorizationConfig{}, cfg.PluginDefs)
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

func TestVisibleFalseHidesOperationFromHTTPSurfacesButAllowsHostedBinding(t *testing.T) {
	t.Parallel()

	hidden := false
	invocations := make(chan string, 1)
	provider := &stubIntegrationWithCatalog{
		StubIntegration: coretesting.StubIntegration{
			N:        "events",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(_ context.Context, op string, _ map[string]any, _ string) (*core.OperationResult, error) {
				invocations <- op
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		},
		catalog: serverTestCatalog("events", []catalog.CatalogOperation{
			{ID: "visible_status", Method: http.MethodGet, Path: "/visible_status", Transport: catalog.TransportREST},
			{ID: "handle_event", Method: http.MethodPost, Path: "/handle_event", Transport: catalog.TransportREST, Visible: &hidden},
		}),
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
						Path:     "/event",
						Method:   http.MethodPost,
						Security: "eventKey",
						Target:   "handle_event",
					},
				},
			},
		}
		cfg.Authorizer = mustAuthorizer(t, config.AuthorizationConfig{}, cfg.PluginDefs)
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/api/v1/integrations/events/operations")
	if err != nil {
		t.Fatalf("list operations: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("list operations status = %d, want %d: %s", resp.StatusCode, http.StatusOK, body)
	}
	var ops []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&ops); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("decode operations: %v", err)
	}
	_ = resp.Body.Close()
	if len(ops) != 1 || ops[0]["id"] != "visible_status" {
		t.Fatalf("operations = %#v, want only visible_status", ops)
	}

	resp, err = http.Post(ts.URL+"/api/v1/events/handle_event", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("generic hidden operation request: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("generic hidden operation status = %d, want %d: %s", resp.StatusCode, http.StatusNotFound, body)
	}
	select {
	case op := <-invocations:
		t.Fatalf("generic hidden operation unexpectedly invoked %q", op)
	default:
	}

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/events/event", strings.NewReader(`{"event":"opened"}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Key", "shared-key")

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("hosted binding request: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("hosted binding status = %d, want %d: %s", resp.StatusCode, http.StatusOK, body)
	}
	select {
	case op := <-invocations:
		if op != "handle_event" {
			t.Fatalf("hosted binding invoked %q, want handle_event", op)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for hosted binding invocation")
	}
}

func TestVisibleFalseGenericRouteSkipsSessionCatalogCredentialResolution(t *testing.T) {
	t.Parallel()

	plaintext, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	svc := testutil.NewStubServices(t)
	seedAPIToken(t, svc, plaintext, hashed, "viewer-user")
	seedUser(t, svc, "viewer-user@test.local")

	hidden := false
	var sessionCatalogCalls atomic.Int64
	provider := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N:        "events",
				ConnMode: core.ConnectionModeUser,
			},
		},
		catalog: serverTestCatalog("events", []catalog.CatalogOperation{
			{ID: "handle_event", Method: http.MethodPost, Path: "/handle_event", Transport: catalog.TransportREST, Visible: &hidden},
		}),
		catalogForRequestFn: func(context.Context, string) (*catalog.Catalog, error) {
			sessionCatalogCalls.Add(1)
			return nil, fmt.Errorf("session catalog should not be resolved for static hidden operations")
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
		cfg.Providers = testutil.NewProviderRegistry(t, provider)
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/events/handle_event", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+plaintext)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("generic hidden operation request: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("generic hidden operation status = %d, want %d: %s", resp.StatusCode, http.StatusNotFound, body)
	}
	if calls := sessionCatalogCalls.Load(); calls != 0 {
		t.Fatalf("session catalog calls = %d, want 0", calls)
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
		cfg.Authorizer = mustAuthorizer(t, config.AuthorizationConfig{}, cfg.PluginDefs)
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
		cfg.Authorizer = mustAuthorizer(t, config.AuthorizationConfig{}, cfg.PluginDefs)
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

func TestHostedHTTPBinding_ComposedProviderPreservesSubjectResolver(t *testing.T) {
	t.Parallel()

	type resolvedInvocationSubject struct {
		ID         string
		AuthSource string
	}
	subjects := make(chan resolvedInvocationSubject, 1)
	resolved := atomic.Bool{}
	baseProvider := &stubIntegrationWithResolvedSubject{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N:        "events",
				ConnMode: core.ConnectionModeNone,
				ExecuteFn: func(ctx context.Context, op string, params map[string]any, _ string) (*core.OperationResult, error) {
					if op != "handle_event" {
						t.Fatalf("operation = %q, want %q", op, "handle_event")
					}
					if got, want := params["event"], "opened"; got != want {
						t.Fatalf("params[event] = %#v, want %q", got, want)
					}
					p := principal.FromContext(ctx)
					subjects <- resolvedInvocationSubject{
						ID:         p.SubjectID,
						AuthSource: p.AuthSource(),
					}
					return &core.OperationResult{Status: http.StatusOK, Body: `{"accepted":true}`}, nil
				},
			},
			ops: []core.Operation{{Name: "handle_event", Method: http.MethodPost}},
		},
		resolveFn: func(_ context.Context, req *core.HTTPSubjectResolveRequest) (*core.HTTPResolvedSubject, error) {
			resolved.Store(true)
			if got, want := req.Params["event"], "opened"; got != want {
				t.Fatalf("resolver params[event] = %#v, want %q", got, want)
			}
			return &core.HTTPResolvedSubject{
				ID:          "user:slack-linked",
				Kind:        string(principal.KindUser),
				DisplayName: "Slack User",
				AuthSource:  "authorization",
			}, nil
		},
	}
	restricted := declarative.NewRestricted(baseProvider, map[string]string{"handle_event": ""})
	otherProvider := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "events-mcp",
			ConnMode: core.ConnectionModeNone,
		},
		ops: []core.Operation{{Name: "other_event", Method: http.MethodPost}},
	}
	provider, err := composite.NewMergedWithConnections(
		"events",
		"Events",
		"Event receiver",
		"",
		composite.BoundProvider{Provider: restricted},
		composite.BoundProvider{Provider: otherProvider},
	)
	if err != nil {
		t.Fatalf("NewMergedWithConnections: %v", err)
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
		cfg.Authorizer = mustAuthorizer(t, config.AuthorizationConfig{}, cfg.PluginDefs)
	})
	testutil.CloseOnCleanup(t, ts)

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/events/event", strings.NewReader(`{"event":"opened"}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http binding request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want %d: %s", resp.StatusCode, http.StatusOK, strings.TrimSpace(string(body)))
	}
	if !resolved.Load() {
		t.Fatal("expected composed provider to call HTTP subject resolver")
	}

	select {
	case subject := <-subjects:
		if subject.ID != "user:slack-linked" {
			t.Fatalf("operation subject = %q, want %q", subject.ID, "user:slack-linked")
		}
		if subject.AuthSource != "authorization" {
			t.Fatalf("operation auth_source = %q, want %q", subject.AuthSource, "authorization")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for http binding invocation")
	}
}

func TestHostedHTTPBinding_CredentialModeNoneBypassesProviderTokenLookup(t *testing.T) {
	t.Parallel()

	type bindingInvocation struct {
		Token      string
		Credential invocation.CredentialContext
	}

	invocations := make(chan bindingInvocation, 1)
	provider := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "events",
			ConnMode: core.ConnectionModeUser,
			ExecuteFn: func(ctx context.Context, op string, _ map[string]any, token string) (*core.OperationResult, error) {
				if op != "handle_event" {
					t.Fatalf("operation = %q, want %q", op, "handle_event")
				}
				invocations <- bindingInvocation{
					Token:      token,
					Credential: invocation.CredentialContextFromContext(ctx),
				}
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
					"public": {Type: providermanifestv1.HTTPSecuritySchemeTypeNone},
				},
				HTTP: map[string]*config.HTTPBinding{
					"event": {
						Path:           "/event",
						Method:         http.MethodPost,
						CredentialMode: providermanifestv1.ConnectionModeNone,
						Security:       "public",
						Target:         "handle_event",
					},
				},
			},
		}
		cfg.Authorizer = mustAuthorizer(t, config.AuthorizationConfig{}, cfg.PluginDefs)
	})
	testutil.CloseOnCleanup(t, ts)

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/events/event", strings.NewReader(`{"event":"opened"}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http binding request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	select {
	case invocation := <-invocations:
		if invocation.Token != "" {
			t.Fatalf("token = %q, want empty", invocation.Token)
		}
		if got, want := invocation.Credential.Mode, core.ConnectionModeNone; got != want {
			t.Fatalf("credential mode = %q, want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for http binding invocation")
	}
}

func TestHostedHTTPBinding_MergedStaticOperationSkipsSessionCatalogResolution(t *testing.T) {
	t.Parallel()

	hidden := false
	invocations := make(chan string, 1)
	sourceProvider := &stubIntegrationWithCatalog{
		StubIntegration: coretesting.StubIntegration{
			N:        "github",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(_ context.Context, op string, _ map[string]any, token string) (*core.OperationResult, error) {
				if token != "" {
					t.Fatalf("token = %q, want empty", token)
				}
				invocations <- op
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true,"ignored":"ping"}`}, nil
			},
		},
		catalog: serverTestCatalog("github", []catalog.CatalogOperation{{
			ID:        "events.handle",
			Method:    http.MethodPost,
			Transport: catalog.TransportPlugin,
			Visible:   &hidden,
		}}),
	}

	var sessionCatalogCalls atomic.Int64
	sessionProvider := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N:        "github",
				ConnMode: core.ConnectionModeUser,
			},
		},
		catalogForRequestFn: func(context.Context, string) (*catalog.Catalog, error) {
			sessionCatalogCalls.Add(1)
			return nil, fmt.Errorf("session catalog should not be resolved for static http binding operation")
		},
	}
	merged, err := composite.NewMergedWithConnections("github", "GitHub", "", "",
		composite.BoundProvider{Provider: sourceProvider},
		composite.BoundProvider{Provider: sessionProvider},
	)
	if err != nil {
		t.Fatalf("NewMergedWithConnections: %v", err)
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, merged)
		cfg.PluginDefs = map[string]*config.ProviderEntry{
			"github": {
				SecuritySchemes: map[string]*config.HTTPSecurityScheme{
					"github_app": {Type: providermanifestv1.HTTPSecuritySchemeTypeNone},
				},
				HTTP: map[string]*config.HTTPBinding{
					"event": {
						Path:           "/event",
						Method:         http.MethodPost,
						CredentialMode: providermanifestv1.ConnectionModeNone,
						Security:       "github_app",
						Target:         "events.handle",
					},
				},
			},
		}
		cfg.Authorizer = mustAuthorizer(t, config.AuthorizationConfig{}, cfg.PluginDefs)
	})
	testutil.CloseOnCleanup(t, ts)

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/github/event", strings.NewReader(`{"zen":"Keep it logically awesome.","hook":{}}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http binding request: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", resp.StatusCode, http.StatusOK, body)
	}

	select {
	case op := <-invocations:
		if op != "events.handle" {
			t.Fatalf("operation = %q, want events.handle", op)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for http binding invocation")
	}
	if got := sessionCatalogCalls.Load(); got != 0 {
		t.Fatalf("session catalog calls = %d, want 0", got)
	}
}

func TestHostedHTTPBinding_RejectsGenericOperationRouteConflicts(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
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
		Invoker:     invocation.NewBroker(providers, svc.Users, svc.ExternalCredentials),
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
		svc := testutil.NewStubServices(t)
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
			Invoker:     invocation.NewBroker(providers, svc.Users, svc.ExternalCredentials),
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
	requireAuthInfoAgentFeature(t, body, false)
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
	requireAuthInfoAgentFeature(t, body, false)
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
	requireAuthInfoAgentFeature(t, body, false)
}

func TestAuthInfoAgentFeature(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.AgentManager = agentmanager.New(agentmanager.Config{
			Agent: &stubAgentControl{
				defaultProviderName: "managed",
				provider:            newMemoryAgentProvider(),
			},
		})
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
	requireAuthInfoAgentFeature(t, body, true)
}

func requireAuthInfoAgentFeature(t *testing.T, body map[string]any, want bool) {
	t.Helper()

	features, ok := body["features"].(map[string]any)
	if !ok {
		t.Fatalf("expected features object, got %#v", body["features"])
	}
	if features["agent"] != want {
		t.Fatalf("expected features.agent %v, got %#v", want, features["agent"])
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
	postConnect      func(context.Context, *core.ExternalCredential) (map[string]string, error)
}

func (s *stubIntegrationWithAuthURL) AuthorizationURL(_ string, _ []string) string {
	return s.authURL
}

func (s *stubIntegrationWithAuthURL) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return maps.Clone(s.connectionParams)
}

func (s *stubIntegrationWithAuthURL) PostConnect(ctx context.Context, token *core.ExternalCredential) (map[string]string, error) {
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
		cfg.Services = testutil.NewStubServices(t)
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
	declarative.CompileSchemas(cat)
	return cat
}

func serverTestCatalog(name string, ops []catalog.CatalogOperation) *catalog.Catalog {
	cat := &catalog.Catalog{
		Name:       name,
		Operations: append([]catalog.CatalogOperation(nil), ops...),
	}
	declarative.CompileSchemas(cat)
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

	svc := testutil.NewStubServices(t)
	recordingCreds := newRecordingExternalCredentialProvider(svc.ExternalCredentials)
	svc.ExternalCredentials = recordingCreds
	u := seedUser(t, svc, "anonymous@gestalt")
	expired := time.Now().Add(-1 * time.Hour)
	seedToken(t, svc, &core.ExternalCredential{
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
	if recordingCreds.lookupCalls() == 0 {
		t.Fatal("expected broker to resolve credentials through ExternalCredentialProvider")
	}
	if recordingCreds.putCredentialCalls.Load() == 0 {
		t.Fatal("expected broker to persist refreshed credentials through ExternalCredentialProvider")
	}
}

func TestExecuteOperation_RefreshFailsButTokenStillValid(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	almostExpired := time.Now().Add(2 * time.Minute)
	seedToken(t, svc, &core.ExternalCredential{
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
		token                   core.ExternalCredential
		configureConnectionAuth bool
		wantStatus              int
		wantUsedToken           string
	}{
		{
			name: "missing refresh token",
			token: core.ExternalCredential{
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
			token: core.ExternalCredential{
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
			token: core.ExternalCredential{
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

			svc := testutil.NewStubServices(t)
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

			svc := testutil.NewStubServices(t)
			u := seedUser(t, svc, "anonymous@gestalt")
			expired := time.Now().Add(-1 * time.Hour)
			seedToken(t, svc, &core.ExternalCredential{
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

			stored, err := svc.ExternalCredentials.GetCredential(context.Background(), principal.UserSubjectID(u.ID), "fake:default", "default")
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
				_ = svc.ExternalCredentials.DeleteCredential(context.Background(), "tok1")
			},
			wantStatus:    http.StatusOK,
			wantUsedToken: "still-valid-token",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := testutil.NewStubServices(t)
			u := seedUser(t, svc, "anonymous@gestalt")
			seedToken(t, svc, &core.ExternalCredential{
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

	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	expired := time.Now().Add(-1 * time.Hour)
	seedToken(t, svc, &core.ExternalCredential{
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
			_ = svc.ExternalCredentials.PutCredential(ctx, &core.ExternalCredential{
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

func TestExecuteOperation_PutCredentialFailureReturnsError(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	provider := svc.ExternalCredentials.(*coretesting.StubExternalCredentialProvider)
	u := seedUser(t, svc, "anonymous@gestalt")
	expired := time.Now().Add(-1 * time.Hour)
	seedToken(t, svc, &core.ExternalCredential{
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
			provider.PutErr = fmt.Errorf("store unavailable")
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
	provider.PutErr = nil

	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502 when PutCredential fails after refresh, got %d", resp.StatusCode)
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
		cfg.Services = testutil.NewStubServices(t)
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
		cfg.Services = testutil.NewStubServices(t)
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
	svc := testutil.NewStubServices(t)

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

	invoker := invocation.NewBroker(providers, svc.Users, svc.ExternalCredentials)
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
	discovery   *core.DiscoveryConfig
	postConnect func(context.Context, *core.ExternalCredential) (map[string]string, error)
}

func (s *stubDiscoveringManualProvider) DiscoveryConfig() *core.DiscoveryConfig {
	return s.discovery
}

func (s *stubDiscoveringManualProvider) PostConnect(ctx context.Context, token *core.ExternalCredential) (map[string]string, error) {
	if s.postConnect != nil {
		return s.postConnect(ctx, token)
	}
	return nil, nil
}

type stubDiscoveringProvider struct {
	coretesting.StubIntegration
	discovery        *core.DiscoveryConfig
	connectionParams map[string]core.ConnectionParamDef
	postConnect      func(context.Context, *core.ExternalCredential) (map[string]string, error)
}

func (s *stubDiscoveringProvider) DiscoveryConfig() *core.DiscoveryConfig {
	return s.discovery
}

func (s *stubDiscoveringProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return maps.Clone(s.connectionParams)
}

func (s *stubDiscoveringProvider) PostConnect(ctx context.Context, token *core.ExternalCredential) (map[string]string, error) {
	if s.postConnect != nil {
		return s.postConnect(ctx, token)
	}
	return nil, nil
}

func testSlackPostConnect(_ context.Context, token *core.ExternalCredential) (map[string]string, error) {
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
	postConnect      func(context.Context, *core.ExternalCredential) (map[string]string, error)
}

func (s *stubPostConnectManualProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return maps.Clone(s.connectionParams)
}

func (s *stubPostConnectManualProvider) PostConnect(ctx context.Context, token *core.ExternalCredential) (map[string]string, error) {
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
		svc := testutil.NewStubServices(t)
		recordingCreds := newRecordingExternalCredentialProvider(svc.ExternalCredentials)
		svc.ExternalCredentials = recordingCreds
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
		tokens, _ := svc.ExternalCredentials.ListCredentials(context.Background(), principal.UserSubjectID(u.ID))
		if len(tokens) == 0 {
			t.Fatal("expected PutCredential to be called")
		}
		stored := tokens[0]
		if stored.Integration != "manual-svc" {
			t.Fatalf("expected integration manual-svc, got %q", stored.Integration)
		}
		if stored.AccessToken != "my-api-key" {
			t.Fatalf("expected credential my-api-key, got %q", stored.AccessToken)
		}
		if recordingCreds.getCredentialCalls.Load() == 0 {
			t.Fatal("expected manual connect to load credentials through ExternalCredentialProvider")
		}
		if recordingCreds.putCredentialCalls.Load() == 0 {
			t.Fatal("expected manual connect to store credentials through ExternalCredentialProvider")
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

	t.Run("service account denied by policy cannot connect", func(t *testing.T) {
		t.Parallel()

		subjectToken, subjectTokenHash, err := principal.GenerateToken(principal.TokenTypeAPI)
		if err != nil {
			t.Fatalf("GenerateToken: %v", err)
		}
		svc := testutil.NewStubServices(t)
		seedSubjectAPIToken(t, svc, subjectTokenHash, "service_account:triage-bot", "triage-bot")
		authz := mustAuthorizer(t, config.AuthorizationConfig{
			Policies: map[string]config.SubjectPolicyDef{
				"manual_policy": {
					Default: "deny",
				},
			},
		}, map[string]*config.ProviderEntry{"manual-svc": {AuthorizationPolicy: "manual_policy"}})
		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
			cfg.Providers = testutil.NewProviderRegistry(t, &stubManualProvider{
				StubIntegration: coretesting.StubIntegration{N: "manual-svc"},
			})
			cfg.DefaultConnection = map[string]string{"manual-svc": config.PluginConnectionName}
			cfg.Services = svc
			cfg.Authorizer = authz
		})
		testutil.CloseOnCleanup(t, ts)

		body := bytes.NewBufferString(`{"integration":"manual-svc","credential":"my-api-key"}`)
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/connect-manual", body)
		req.Header.Set("Authorization", "Bearer "+subjectToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusForbidden {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 403, got %d: %s", resp.StatusCode, body)
		}
		tokens, err := svc.ExternalCredentials.ListCredentials(context.Background(), "service_account:triage-bot")
		if err != nil {
			t.Fatalf("ListCredentials: %v", err)
		}
		if len(tokens) != 0 {
			t.Fatalf("expected denied service account connect not to store credentials, got %d", len(tokens))
		}
	})

	t.Run("service account external identity writes subject relationship", func(t *testing.T) {
		t.Parallel()

		subjectToken, subjectTokenHash, err := principal.GenerateToken(principal.TokenTypeAPI)
		if err != nil {
			t.Fatalf("GenerateToken: %v", err)
		}
		svc := testutil.NewStubServices(t)
		seedSubjectAPIToken(t, svc, subjectTokenHash, "service_account:triage-bot", "triage-bot")
		authzProvider := newMemoryAuthorizationProvider("memory-authorization")
		base, err := newTestAuthorizer(config.AuthorizationConfig{
			Policies: map[string]config.SubjectPolicyDef{
				"manual_policy": {
					Members: []config.SubjectPolicyMemberDef{{
						SubjectID: "service_account:triage-bot",
						Role:      "viewer",
					}},
				},
			},
		}, map[string]*config.ProviderEntry{"manual-svc": {AuthorizationPolicy: "manual_policy"}})
		if err != nil {
			t.Fatalf("authorization.New: %v", err)
		}
		authz := mustProviderBackedAuthorizer(t, base, authzProvider)
		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
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

		body := bytes.NewBufferString(`{"integration":"manual-svc","credential":"my-api-key","connectionParams":{"team_id":"T123","user_id":"U456"}}`)
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/connect-manual", body)
		req.Header.Set("Authorization", "Bearer "+subjectToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		relResp, err := authzProvider.ReadRelationships(context.Background(), &core.ReadRelationshipsRequest{
			Relation: authorization.ProviderExternalIdentityRelationAssume,
			Resource: &core.ResourceRef{
				Type: authorization.ProviderResourceTypeExternalIdentity,
				Id:   testExternalIdentityResourceID("slack_identity", "team:T123:user:U456"),
			},
		})
		if err != nil {
			t.Fatalf("ReadRelationships: %v", err)
		}
		relationships := relResp.GetRelationships()
		if len(relationships) != 1 {
			t.Fatalf("expected one external identity relationship, got %+v", relationships)
		}
		subject := relationships[0].GetSubject()
		if subject.GetType() != authorization.ProviderSubjectTypeSubject || subject.GetId() != "service_account:triage-bot" {
			t.Fatalf("expected subject service account relationship, got %+v", subject)
		}
	})

	t.Run("reconnect replaces prior external identity authorization", func(t *testing.T) {
		t.Parallel()

		svc := testutil.NewStubServices(t)
		authzProvider := newMemoryAuthorizationProvider("memory-authorization")
		baseAuthz, err := newTestAuthorizer(config.AuthorizationConfig{}, map[string]*config.ProviderEntry{})
		if err != nil {
			t.Fatalf("authorization.New: %v", err)
		}
		authz := mustProviderBackedAuthorizer(t, baseAuthz, authzProvider)
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
		tokens, err := svc.ExternalCredentials.ListCredentials(context.Background(), principal.UserSubjectID(u.ID))
		if err != nil {
			t.Fatalf("ListCredentials: %v", err)
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

		svc := testutil.NewStubServices(t)
		authzProvider := newMemoryAuthorizationProvider("memory-authorization")
		baseAuthz, err := newTestAuthorizer(config.AuthorizationConfig{}, map[string]*config.ProviderEntry{})
		if err != nil {
			t.Fatalf("authorization.New: %v", err)
		}
		authz := mustProviderBackedAuthorizer(t, baseAuthz, authzProvider)
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
		tokens, err := svc.ExternalCredentials.ListCredentials(context.Background(), principal.UserSubjectID(u.ID))
		if err != nil {
			t.Fatalf("ListCredentials: %v", err)
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
		svc := testutil.NewStubServices(t)
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
		tokens, _ := svc.ExternalCredentials.ListCredentials(context.Background(), principal.UserSubjectID(u.ID))
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
		prov := declarative.NewRestricted(&stubManualProviderWithCapabilities{
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
			cfg.Services = testutil.NewStubServices(t)
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

		prov := declarative.NewRestricted(&stubManualProviderWithCapabilities{
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
			cfg.Services = testutil.NewStubServices(t)
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

		svc := testutil.NewStubServices(t)
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
		tokens, _ := svc.ExternalCredentials.ListCredentials(context.Background(), principal.UserSubjectID(u.ID))
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

		svc := testutil.NewStubServices(t)
		prov := declarative.NewRestricted(&stubManualProviderWithCapabilities{
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
		tokens, _ := svc.ExternalCredentials.ListCredentials(context.Background(), principal.UserSubjectID(u.ID))
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
		cfg.Services = testutil.NewStubServices(t)
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
		cfg.Services = testutil.NewStubServices(t)
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
		cfg.Services = testutil.NewStubServices(t)
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
		cfg.Services = testutil.NewStubServices(t)
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

			svc := testutil.NewStubServices(t)
			u := seedUser(t, svc, "anonymous@gestalt")
			expired := time.Now().Add(-1 * time.Hour)
			seedToken(t, svc, &core.ExternalCredential{
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

	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	expired := time.Now().Add(-1 * time.Hour)
	seedToken(t, svc, &core.ExternalCredential{
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
	broker := invocation.NewBroker(providers, svc.Users, svc.ExternalCredentials, brokerOpts...)
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
	svc := testutil.NewStubServices(t)
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
	svc := testutil.NewStubServices(t)
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

	svc := testutil.NewStubServices(t)
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

	svc := testutil.NewStubServices(t)
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

	svc := testutil.NewStubServices(t)
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

	svc := testutil.NewStubServices(t)
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
	svc := testutil.NewStubServices(t)
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

	svc := testutil.NewStubServices(t)
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

func TestMCPEndpoint_ServiceAccountAuthorizationAndAudit(t *testing.T) {
	t.Parallel()

	staticCat := &catalog.Catalog{
		Name: "clickhouse",
		Operations: []catalog.CatalogOperation{
			{
				ID:           "run_query",
				Description:  "Execute a SQL query",
				Transport:    catalog.TransportMCPPassthrough,
				AllowedRoles: []string{"viewer"},
			},
			{
				ID:           "delete_table",
				Description:  "Delete a table",
				Transport:    catalog.TransportMCPPassthrough,
				AllowedRoles: []string{"admin"},
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

	svc := testutil.NewStubServices(t)
	subjectToken, subjectTokenHash, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	seedSubjectAPIToken(t, svc, subjectTokenHash, "service_account:triage-bot", "triage-bot")
	seedSubjectToken(t, svc, "service_account:triage-bot", "clickhouse", "", "default", "identity-token")

	providers := testutil.NewProviderRegistry(t, prov)
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"clickhouse_policy": {
				Members: []config.SubjectPolicyMemberDef{{
					SubjectID: "service_account:triage-bot",
					Role:      "viewer",
				}},
			},
		},
	}, map[string]*config.ProviderEntry{"clickhouse": {AuthorizationPolicy: "clickhouse_policy"}})

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
		"Authorization": "Bearer " + subjectToken,
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
	if auditRecord["auth_source"] != "api_token" {
		t.Fatalf("expected audit auth_source api_token, got %v", auditRecord["auth_source"])
	}
	if auditRecord["subject_id"] != "service_account:triage-bot" {
		t.Fatalf("expected subject_id service_account:triage-bot, got %v", auditRecord["subject_id"])
	}
	if auditRecord["subject_kind"] != "service_account" {
		t.Fatalf("expected subject_kind service_account, got %v", auditRecord["subject_kind"])
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
		cfg.Services = testutil.NewStubServices(t)
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

	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.ExternalCredential{
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

	prov, err := declarative.Build(&declarative.Definition{
		Provider:         "test-int",
		DisplayName:      "Test Integration",
		BaseURL:          upstream.URL,
		ConnectionMode:   "none",
		Auth:             declarative.AuthDef{Type: "manual"},
		ErrorMessagePath: "error.message",
		Operations: map[string]declarative.OperationDef{
			"do_thing": {Description: "Do a thing", Method: http.MethodGet, Path: "/do_thing"},
		},
	}, declarative.ConnectionDef{})
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

func TestExecuteOperation_UpstreamUnauthorizedRequiresReconnect(t *testing.T) {
	t.Parallel()

	fullStub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{N: "test-int"},
		ops: []core.Operation{
			{Name: "do_thing", Description: "Do a thing", Method: http.MethodGet},
		},
	}
	invoker := &testutil.StubInvoker{
		Err: &apiexec.UpstreamHTTPError{
			Status: http.StatusUnauthorized,
			Body:   "",
		},
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

	var errResp struct {
		Error       string `json:"error"`
		Code        string `json:"code"`
		Integration string `json:"integration"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		t.Fatalf("decoding error response: %v", err)
	}
	if errResp.Code != "reconnect_required" {
		t.Fatalf("expected reconnect_required code, got %q", errResp.Code)
	}
	if errResp.Integration != "test-int" {
		t.Fatalf("expected integration test-int, got %q", errResp.Integration)
	}
	if !strings.Contains(errResp.Error, "reconnect it") {
		t.Fatalf("expected reconnect hint, got %q", errResp.Error)
	}
}

func TestExecuteOperation_UserFacingErrorMessage(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.ExternalCredential{
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

	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.ExternalCredential{
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

	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.ExternalCredential{
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
		cfg.Services = testutil.NewStubServices(t)
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
				cfg.Services = testutil.NewStubServices(t)
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
		cfg.Services = testutil.NewStubServices(t)
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

func TestExecuteOperation_ConnectionModeUserUsesSubjectCredential(t *testing.T) {
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

	t.Run("prefers subject token", func(t *testing.T) {
		t.Parallel()

		svc := testutil.NewStubServices(t)
		apiToken, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
		if err != nil {
			t.Fatalf("GenerateToken: %v", err)
		}
		seedAPIToken(t, svc, apiToken, hashed, "api-user")
		u, _ := svc.Users.FindOrCreateUser(context.Background(), "api-user@test.local")
		seedToken(t, svc, &core.ExternalCredential{
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

			svc := testutil.NewStubServices(t)
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
			tokens, _ := svc.ExternalCredentials.ListCredentials(context.Background(), principal.UserSubjectID(u.ID))
			if len(tokens) == 0 {
				t.Fatal("expected PutCredential to be called")
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

func TestConnectManual_TokenExchange(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)

	t.Run("exchanges declared credentials and stores refresh source", func(t *testing.T) {
		t.Parallel()

		var seenAccept string
		var seenContentType string
		var seenForm url.Values
		tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/login" {
				t.Fatalf("token path = %q, want /login", r.URL.Path)
			}
			seenAccept = r.Header.Get("Accept")
			seenContentType = r.Header.Get("Content-Type")
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			seenForm = maps.Clone(r.PostForm)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"manual-access","refresh_token":"ignored-refresh","expires_in":3600,"account":{"id":"acct_123"}}`))
		}))
		testutil.CloseOnCleanup(t, tokenSrv)

		svc := testutil.NewStubServices(t)
		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, &stubManualProviderWithCapabilities{
				stubManualProvider: stubManualProvider{
					StubIntegration: coretesting.StubIntegration{N: "looker-like"},
				},
				connectionParams: map[string]core.ConnectionParamDef{
					"account_id": {From: "token_response", Field: "account.id", Required: true},
				},
			})
			cfg.DefaultConnection = map[string]string{"looker-like": config.PluginConnectionName}
			cfg.PluginDefs = map[string]*config.ProviderEntry{
				"looker-like": {
					Auth: &config.ConnectionAuthDef{
						Type:          providermanifestv1.AuthTypeManual,
						TokenURL:      tokenSrv.URL + "/login",
						TokenExchange: "form",
						TokenParams:   map[string]string{"audience": "api"},
						AcceptHeader:  "application/json",
						Credentials: []config.CredentialFieldDef{
							{Name: "client_id", Label: "Client ID"},
							{Name: "client_secret", Label: "Client Secret"},
						},
					},
				},
			}
			cfg.Now = func() time.Time { return fixedNow }
			cfg.Services = svc
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/connect-manual", bytes.NewBufferString(`{"integration":"looker-like","credentials":{"client_id":"id-123","client_secret":"secret-456"}}`))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
		}

		if seenAccept != "application/json" {
			t.Fatalf("Accept = %q, want application/json", seenAccept)
		}
		if !strings.HasPrefix(seenContentType, "application/x-www-form-urlencoded") {
			t.Fatalf("Content-Type = %q, want form", seenContentType)
		}
		if got := seenForm.Get("client_id"); got != "id-123" {
			t.Fatalf("client_id = %q", got)
		}
		if got := seenForm.Get("client_secret"); got != "secret-456" {
			t.Fatalf("client_secret = %q", got)
		}
		if got := seenForm.Get("audience"); got != "api" {
			t.Fatalf("audience = %q", got)
		}

		u, _ := svc.Users.FindOrCreateUser(context.Background(), "anonymous@gestalt")
		tokens, _ := svc.ExternalCredentials.ListCredentials(context.Background(), principal.UserSubjectID(u.ID))
		if len(tokens) != 1 {
			t.Fatalf("stored credentials = %d, want 1", len(tokens))
		}
		stored := tokens[0]
		if stored.AccessToken != "manual-access" {
			t.Fatalf("access token = %q, want manual-access", stored.AccessToken)
		}
		var refreshSource map[string]string
		if err := json.Unmarshal([]byte(stored.RefreshToken), &refreshSource); err != nil {
			t.Fatalf("refresh source is not credential JSON: %v", err)
		}
		if !reflect.DeepEqual(refreshSource, map[string]string{"client_id": "id-123", "client_secret": "secret-456"}) {
			t.Fatalf("refresh source = %+v", refreshSource)
		}
		if stored.ExpiresAt == nil || !stored.ExpiresAt.Equal(fixedNow.Add(time.Hour)) {
			t.Fatalf("expires_at = %v, want %v", stored.ExpiresAt, fixedNow.Add(time.Hour))
		}
		var metadata map[string]string
		if err := json.Unmarshal([]byte(stored.MetadataJSON), &metadata); err != nil {
			t.Fatalf("metadata JSON: %v", err)
		}
		if metadata["account_id"] != "acct_123" {
			t.Fatalf("account_id metadata = %q, want acct_123", metadata["account_id"])
		}
	})

	t.Run("supports json exchange and accessTokenPath", func(t *testing.T) {
		t.Parallel()

		var seen map[string]string
		tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
				t.Fatalf("Content-Type = %q, want JSON", got)
			}
			if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
				t.Fatalf("decode token request: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"token":"nested-access"},"expires_in":"120"}`))
		}))
		testutil.CloseOnCleanup(t, tokenSrv)

		svc := testutil.NewStubServices(t)
		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, &stubManualProvider{
				StubIntegration: coretesting.StubIntegration{N: "json-token"},
			})
			cfg.DefaultConnection = map[string]string{"json-token": config.PluginConnectionName}
			cfg.PluginDefs = map[string]*config.ProviderEntry{
				"json-token": {
					Auth: &config.ConnectionAuthDef{
						Type:            providermanifestv1.AuthTypeManual,
						TokenURL:        tokenSrv.URL,
						TokenExchange:   "json",
						AccessTokenPath: "data.token",
						Credentials: []config.CredentialFieldDef{
							{Name: "client_id"},
							{Name: "client_secret"},
						},
					},
				},
			}
			cfg.Now = func() time.Time { return fixedNow }
			cfg.Services = svc
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/connect-manual", bytes.NewBufferString(`{"integration":"json-token","credentials":{"client_id":"json-id","client_secret":"json-secret"}}`))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
		}
		if !reflect.DeepEqual(seen, map[string]string{"client_id": "json-id", "client_secret": "json-secret"}) {
			t.Fatalf("token request body = %+v", seen)
		}

		u, _ := svc.Users.FindOrCreateUser(context.Background(), "anonymous@gestalt")
		tokens, _ := svc.ExternalCredentials.ListCredentials(context.Background(), principal.UserSubjectID(u.ID))
		if len(tokens) != 1 {
			t.Fatalf("stored credentials = %d, want 1", len(tokens))
		}
		if tokens[0].AccessToken != "nested-access" {
			t.Fatalf("access token = %q, want nested-access", tokens[0].AccessToken)
		}
		if tokens[0].ExpiresAt == nil || !tokens[0].ExpiresAt.Equal(fixedNow.Add(120*time.Second)) {
			t.Fatalf("expires_at = %v, want %v", tokens[0].ExpiresAt, fixedNow.Add(120*time.Second))
		}
	})

	t.Run("uses registered exchanger when connection auth has no token URL", func(t *testing.T) {
		t.Parallel()

		var calls atomic.Int64
		tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if got := r.Form.Get("client_id"); got != "fallback-id" {
				t.Fatalf("client_id = %q, want fallback-id", got)
			}
			if got := r.Form.Get("client_secret"); got != "fallback-secret" {
				t.Fatalf("client_secret = %q, want fallback-secret", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"fallback-access","expires_in":300}`))
		}))
		testutil.CloseOnCleanup(t, tokenSrv)

		svc := testutil.NewStubServices(t)
		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, &stubManualProviderWithCapabilities{
				stubManualProvider: stubManualProvider{
					StubIntegration: coretesting.StubIntegration{N: "fallback-token"},
				},
				credentialFields: []core.CredentialFieldDef{
					{Name: "client_id"},
					{Name: "client_secret"},
				},
			})
			cfg.DefaultConnection = map[string]string{"fallback-token": config.PluginConnectionName}
			cfg.PluginDefs = map[string]*config.ProviderEntry{
				"fallback-token": {
					Auth: &config.ConnectionAuthDef{
						Type:     providermanifestv1.AuthTypeManual,
						TokenURL: tokenSrv.URL,
						Credentials: []config.CredentialFieldDef{
							{Name: "client_id"},
							{Name: "client_secret"},
						},
					},
				},
			}
			cfg.Now = func() time.Time { return fixedNow }
			cfg.Services = svc
		})
		testutil.CloseOnCleanup(t, ts)

		rawReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/connect-manual", bytes.NewBufferString(`{"integration":"fallback-token","credential":"raw-token"}`))
		rawReq.Header.Set("Content-Type", "application/json")
		rawResp, err := http.DefaultClient.Do(rawReq)
		if err != nil {
			t.Fatalf("raw request: %v", err)
		}
		defer func() { _ = rawResp.Body.Close() }()
		if rawResp.StatusCode != http.StatusBadRequest {
			body, _ := io.ReadAll(rawResp.Body)
			t.Fatalf("raw status = %d, want 400: %s", rawResp.StatusCode, body)
		}
		if calls.Load() != 0 {
			t.Fatalf("token endpoint calls after raw credential = %d, want 0", calls.Load())
		}

		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/connect-manual", bytes.NewBufferString(`{"integration":"fallback-token","credentials":{"client_id":"fallback-id","client_secret":"fallback-secret"}}`))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
		}
		if calls.Load() != 1 {
			t.Fatalf("token endpoint calls = %d, want 1", calls.Load())
		}

		u, _ := svc.Users.FindOrCreateUser(context.Background(), "anonymous@gestalt")
		tokens, _ := svc.ExternalCredentials.ListCredentials(context.Background(), principal.UserSubjectID(u.ID))
		if len(tokens) != 1 {
			t.Fatalf("stored credentials = %d, want 1", len(tokens))
		}
		if tokens[0].AccessToken != "fallback-access" {
			t.Fatalf("access token = %q, want fallback-access", tokens[0].AccessToken)
		}
	})

	t.Run("rejects raw missing and unknown credentials before exchange", func(t *testing.T) {
		t.Parallel()

		var calls atomic.Int64
		tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			http.Error(w, "should not be called", http.StatusInternalServerError)
		}))
		testutil.CloseOnCleanup(t, tokenSrv)

		cases := []string{
			`{"integration":"strict-token","credential":"raw-token"}`,
			`{"integration":"strict-token","credentials":{"client_id":"id-only"}}`,
			`{"integration":"strict-token","credentials":{"client_id":"id","client_secret":"secret","extra":"nope"}}`,
		}
		t.Cleanup(func() {
			if calls.Load() != 0 {
				t.Fatalf("token endpoint calls = %d, want 0", calls.Load())
			}
		})
		for _, body := range cases {
			body := body
			t.Run(body, func(t *testing.T) {
				t.Parallel()

				svc := testutil.NewStubServices(t)
				ts := newTestServer(t, func(cfg *server.Config) {
					cfg.Providers = testutil.NewProviderRegistry(t, &stubManualProvider{
						StubIntegration: coretesting.StubIntegration{N: "strict-token"},
					})
					cfg.DefaultConnection = map[string]string{"strict-token": config.PluginConnectionName}
					cfg.PluginDefs = map[string]*config.ProviderEntry{
						"strict-token": {
							Auth: &config.ConnectionAuthDef{
								Type:     providermanifestv1.AuthTypeManual,
								TokenURL: tokenSrv.URL,
								Credentials: []config.CredentialFieldDef{
									{Name: "client_id"},
									{Name: "client_secret"},
								},
							},
						},
					}
					cfg.Services = svc
				})
				testutil.CloseOnCleanup(t, ts)

				req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/connect-manual", bytes.NewBufferString(body))
				req.Header.Set("Content-Type", "application/json")
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatalf("request: %v", err)
				}
				defer func() { _ = resp.Body.Close() }()
				if resp.StatusCode != http.StatusBadRequest {
					responseBody, _ := io.ReadAll(resp.Body)
					t.Fatalf("status = %d, want 400: %s", resp.StatusCode, responseBody)
				}
			})
		}
	})
}

func TestRefresh_UsesManualTokenExchangeHandlers(t *testing.T) {
	t.Parallel()

	sourceCredential := `{"client_id":"id-123","client_secret":"secret-456"}`
	var seenForm url.Values
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		seenForm = maps.Clone(r.PostForm)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"refreshed-manual","refresh_token":"ignored-by-gestalt","expires_in":3600}`))
	}))
	testutil.CloseOnCleanup(t, tokenSrv)

	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	expired := time.Now().Add(-1 * time.Hour)
	seedToken(t, svc, &core.ExternalCredential{
		ID:           "tok-manual",
		SubjectID:    principal.UserSubjectID(u.ID),
		Integration:  "manual-refresh",
		Connection:   "default",
		Instance:     "default",
		AccessToken:  "expired-manual",
		RefreshToken: sourceCredential,
		ExpiresAt:    &expired,
	})

	var usedToken string
	stub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N: "manual-refresh",
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
				usedToken = token
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		},
		ops: []core.Operation{{Name: "list", Description: "List", Method: http.MethodGet}},
	}

	exchanger := oauth.NewCredentialExchanger(oauth.CredentialExchangeConfig{
		TokenURL: tokenSrv.URL,
	})
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"manual-refresh": testDefaultConnection}
		cfg.ManualConnectionAuth = func() map[string]map[string]bootstrap.ManualTokenExchanger {
			return map[string]map[string]bootstrap.ManualTokenExchanger{
				"manual-refresh": {
					testDefaultConnection: exchanger,
				},
			}
		}
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/manual-refresh/list", nil)
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}
	if usedToken != "refreshed-manual" {
		t.Fatalf("used token = %q, want refreshed-manual", usedToken)
	}
	if seenForm.Get("client_id") != "id-123" || seenForm.Get("client_secret") != "secret-456" {
		t.Fatalf("token request form = %+v", seenForm)
	}

	tokens, _ := svc.ExternalCredentials.ListCredentials(context.Background(), principal.UserSubjectID(u.ID))
	if len(tokens) != 1 {
		t.Fatalf("stored credentials = %d, want 1", len(tokens))
	}
	if tokens[0].AccessToken != "refreshed-manual" {
		t.Fatalf("stored access token = %q, want refreshed-manual", tokens[0].AccessToken)
	}
	if tokens[0].RefreshToken != sourceCredential {
		t.Fatalf("stored refresh source = %q, want original credential JSON", tokens[0].RefreshToken)
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

	svc := testutil.NewStubServices(t)
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

	svc := testutil.NewStubServices(t)
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
		cfg.Services = testutil.NewStubServices(t)
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
