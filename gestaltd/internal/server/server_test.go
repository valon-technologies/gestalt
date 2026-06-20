package server_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	s3sdk "github.com/valon-technologies/gestalt/sdk/go/s3"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/indexeddbcodec"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	appservice "github.com/valon-technologies/gestalt/server/services/apps"
	"github.com/valon-technologies/gestalt/server/services/apps/apiexec"
	"github.com/valon-technologies/gestalt/server/services/apps/composite"
	"github.com/valon-technologies/gestalt/server/services/apps/declarative"
	gestaltmcp "github.com/valon-technologies/gestalt/server/services/apps/mcp"
	"github.com/valon-technologies/gestalt/server/services/apps/mcpupstream"
	"github.com/valon-technologies/gestalt/server/services/apps/oauth"
	"github.com/valon-technologies/gestalt/server/services/apps/paraminterp"
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
	"github.com/valon-technologies/gestalt/server/services/egressproxy"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	indexeddbservice "github.com/valon-technologies/gestalt/server/services/indexeddb"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/runtimelogs"
	"github.com/valon-technologies/gestalt/server/services/s3"
	"github.com/valon-technologies/gestalt/server/services/ui"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	grpcstatus "google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

func mustStruct(t *testing.T, fields map[string]any) *structpb.Struct {
	t.Helper()
	out, err := structpb.NewStruct(fields)
	if err != nil {
		t.Fatalf("structpb.NewStruct: %v", err)
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
	if cfg.MCPConnection != nil {
		brokerOpts = append(brokerOpts,
			invocation.WithMCPConnectionMapper(invocation.ConnectionMap(cfg.MCPConnection)),
		)
	}
	if cfg.TracerProvider != nil {
		brokerOpts = append(brokerOpts, invocation.WithTracerProvider(cfg.TracerProvider))
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
			grant := credential.Grant
			if grant != nil && grant.RefreshToken != "" && grant.ExpiresAt != nil && time.Until(*grant.ExpiresAt) <= 5*time.Minute {
				if resp, ok, refreshErr := refreshTestCredential(ctx, cfg, req, credential); refreshErr != nil {
					fresh, fetchErr := stub.GetCredential(ctx, credential.Subject, credential.Audience, credential.Qualifier)
					if fetchErr == nil && fresh != nil && fresh.Grant != nil && fresh.Grant.AccessToken != grant.AccessToken {
						return &core.ResolveExternalCredentialResponse{Token: fresh.Grant.AccessToken, ExpiresAt: fresh.Grant.ExpiresAt, MetadataJSON: fresh.MetadataJSON, Credential: fresh}, nil
					}
					if time.Now().Before(*grant.ExpiresAt) {
						return &core.ResolveExternalCredentialResponse{Token: grant.AccessToken, ExpiresAt: grant.ExpiresAt, MetadataJSON: credential.MetadataJSON, Credential: credential}, nil
					}
					return nil, fmt.Errorf("%w: token expired and refresh failed: %v", core.ErrReconnectRequired, refreshErr)
				} else if ok {
					now := time.Now().UTC()
					grant.AccessToken = resp.AccessToken
					if resp.RefreshToken != "" {
						grant.RefreshToken = resp.RefreshToken
					}
					if resp.ExpiresIn > 0 {
						expiresAt := now.Add(time.Duration(resp.ExpiresIn) * time.Second)
						grant.ExpiresAt = &expiresAt
					} else {
						grant.ExpiresAt = nil
					}
					grant.LastRefreshedAt = &now
					grant.RefreshErrorCount = 0
					credential.UpdatedAt = now
					if err := stub.UpsertCredential(ctx, credential); err != nil {
						return nil, err
					}
				}
			}
			resp := &core.ResolveExternalCredentialResponse{
				MetadataJSON: credential.MetadataJSON,
				Credential:   credential,
			}
			switch {
			case credential.Grant != nil:
				resp.Token = credential.Grant.AccessToken
				resp.ExpiresAt = credential.Grant.ExpiresAt
			case credential.Opaque != nil:
				if data, err := json.Marshal(credential.Opaque.Fields); err == nil {
					resp.Token = string(data)
				}
				resp.Params = credential.Opaque.Fields
			}
			return resp, nil
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
	if req.Instance != "" {
		return stub.GetCredential(ctx, req.CredentialSubjectID, req.ConnectionID, req.Instance)
	}
	credentials, err := stub.ListCredentials(ctx, req.CredentialSubjectID, req.ConnectionID)
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

func refreshTestCredential(ctx context.Context, cfg *server.Config, req *core.ResolveExternalCredentialRequest, credential *core.ExternalCredential) (*core.OAuthTokenResponse, bool, error) {
	if cfg == nil || req == nil || credential == nil || credential.Grant == nil {
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
					resp, err := refresher.RefreshTokenWithURL(ctx, credential.Grant.RefreshToken, tokenURL)
					metricutil.RecordConnectionAuthMetrics(ctx, startedAt, req.Provider, "oauth", "refresh", connectionMode, err != nil)
					return resp, true, err
				}
				resp, err := refresher.RefreshToken(ctx, credential.Grant.RefreshToken)
				metricutil.RecordConnectionAuthMetrics(ctx, startedAt, req.Provider, "oauth", "refresh", connectionMode, err != nil)
				return resp, true, err
			}
		}
	}
	if cfg.ManualConnectionAuth != nil {
		if connMap := cfg.ManualConnectionAuth()[req.Provider]; connMap != nil {
			if refresher := connMap[req.Connection]; refresher != nil {
				startedAt := time.Now()
				resp, err := refresher.RefreshToken(ctx, credential.Grant.RefreshToken)
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
	createCredentialCalls   atomic.Int64
	upsertCredentialCalls   atomic.Int64
	deleteCredentialCalls   atomic.Int64
	validateConfigCalls     atomic.Int64
	resolveCredentialCalls  atomic.Int64
	exchangeCredentialCalls atomic.Int64
}

func TestS3ObjectAccessURLUploadsAndDownloadsAppScopedObject(t *testing.T) {
	t.Parallel()

	store := &coretesting.StubS3{}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.PublicBaseURL = "https://gestalt.example.test"
		cfg.S3 = map[string]s3sdk.S3{"brainStorage": store}
	})
	defer ts.Close()

	manager, err := s3.NewObjectAccessURLManager(
		[]byte("0123456789abcdef0123456789abcdef"),
		ts.URL,
	)
	if err != nil {
		t.Fatalf("NewObjectAccessURLManager: %v", err)
	}
	targetRef := s3sdk.ObjectRef{
		Key: " workspaces/acme/tokens/token-1/content.bin ",
	}
	putURL, err := manager.MintURL(s3.ObjectAccessURLRequest{
		AppName:     "brain",
		BindingName: "brainStorage",
		Ref:         targetRef,
		Method:      s3sdk.PresignMethodPut,
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

	prefixed := s3sdk.ObjectRef{
		Key: s3.AppObjectKey("brain", targetRef.Key),
	}
	if _, err := store.HeadObject(context.Background(), prefixed); err != nil {
		t.Fatalf("HeadObject(prefixed): %v", err)
	}
	if _, err := store.HeadObject(context.Background(), targetRef); !errors.Is(err, s3sdk.ErrNotFound) {
		t.Fatalf("HeadObject(unprefixed) error = %v, want ErrNotFound", err)
	}

	getURL, err := manager.MintURL(s3.ObjectAccessURLRequest{
		AppName:     "brain",
		BindingName: "brainStorage",
		Ref:         targetRef,
		Method:      s3sdk.PresignMethodGet,
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
		AppName:     "brain",
		BindingName: "brainStorage",
		Ref:         targetRef,
		Method:      s3sdk.PresignMethodGet,
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

func (r *recordingExternalCredentialProvider) CreateCredential(ctx context.Context, credential *core.ExternalCredential) error {
	r.createCredentialCalls.Add(1)
	return r.inner.CreateCredential(ctx, credential)
}

func (r *recordingExternalCredentialProvider) UpsertCredential(ctx context.Context, credential *core.ExternalCredential) error {
	r.upsertCredentialCalls.Add(1)
	return r.inner.UpsertCredential(ctx, credential)
}

func (r *recordingExternalCredentialProvider) GetCredential(ctx context.Context, subject, audience, qualifier string) (*core.ExternalCredential, error) {
	r.getCredentialCalls.Add(1)
	return r.inner.GetCredential(ctx, subject, audience, qualifier)
}

func (r *recordingExternalCredentialProvider) ListCredentials(ctx context.Context, subject, audience string) ([]*core.ExternalCredential, error) {
	r.listCredentialsCalls.Add(1)
	return r.inner.ListCredentials(ctx, subject, audience)
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
	tokens, err := provider.ListCredentials(ctx, subjectID, "")
	if err != nil {
		return nil, err
	}
	out := make([]*core.ExternalCredential, 0, len(tokens))
	for _, token := range tokens {
		if token != nil && strings.HasPrefix(token.Audience, integration+":") {
			out = append(out, token)
		}
	}
	return out, nil
}

func (r *recordingExternalCredentialProvider) lookupCalls() int64 {
	return r.getCredentialCalls.Load() + r.listCredentialsCalls.Load() + r.resolveCredentialCalls.Load()
}

type staticRuntimeInspector struct {
	mu              sync.Mutex
	snapshots       []bootstrap.RuntimeProviderSnapshot
	sessions        map[string]*proto.ListRuntimeSessionsResponse
	sessionRequests map[string]*proto.ListRuntimeSessionsRequest
	logs            []runtimelogs.Record
	err             error
}

func (s *staticRuntimeInspector) SnapshotRuntimes(context.Context) ([]bootstrap.RuntimeProviderSnapshot, error) {
	if s == nil {
		return nil, nil
	}
	if s.err != nil {
		return nil, s.err
	}
	out := make([]bootstrap.RuntimeProviderSnapshot, 0, len(s.snapshots))
	for _, snapshot := range s.snapshots {
		cloned := snapshot
		out = append(out, cloned)
	}
	return out, nil
}

func (s *staticRuntimeInspector) ListRuntimeSessions(_ context.Context, providerName string, req *proto.ListRuntimeSessionsRequest) (*proto.ListRuntimeSessionsResponse, error) {
	if s == nil {
		return nil, bootstrap.ErrRuntimeProviderNotFound
	}
	if s.err != nil {
		return nil, s.err
	}
	s.mu.Lock()
	if s.sessionRequests == nil {
		s.sessionRequests = map[string]*proto.ListRuntimeSessionsRequest{}
	}
	s.sessionRequests[providerName] = gproto.Clone(req).(*proto.ListRuntimeSessionsRequest)
	s.mu.Unlock()
	resp := s.sessions[providerName]
	if resp == nil {
		for _, snapshot := range s.snapshots {
			if snapshot.Name == providerName {
				return nil, bootstrap.ErrRuntimeProviderUnavailable
			}
		}
		return nil, bootstrap.ErrRuntimeProviderNotFound
	}
	return gproto.Clone(resp).(*proto.ListRuntimeSessionsResponse), nil
}

func (s *staticRuntimeInspector) ListRuntimeSessionLogs(_ context.Context, _ string, _ string, afterSeq int64, limit int) ([]runtimelogs.Record, error) {
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
	return &core.OperationResult{Status: 202, Body: []byte("relayed")}, nil
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

type callerTokenRecordingInvoker struct {
	mu          sync.Mutex
	callerToken string
}

func (i *callerTokenRecordingInvoker) Invoke(ctx context.Context, _ *principal.Principal, _, _, _ string, _ map[string]any) (*core.OperationResult, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.callerToken = providergateway.CallerTokenFromContext(ctx)
	return &core.OperationResult{Status: http.StatusOK, Body: []byte(`{"ok":true}`)}, nil
}

func (i *callerTokenRecordingInvoker) token() string {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.callerToken
}

func relayAppRequestContext() *proto.RequestContext {
	return &proto.RequestContext{
		Caller: &proto.ProviderContext{
			Kind: string(invocation.ProviderKindApp),
			Name: "support",
		},
		Subject: &proto.SubjectContext{
			Id:                  "user:test-user",
			CredentialSubjectId: "user:test-user",
		},
	}
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

type relayTestWorkflowProviderServer struct {
	proto.UnimplementedWorkflowServer
	calls *atomic.Int64
}

func (s relayTestWorkflowProviderServer) GetDefinition(_ context.Context, req *proto.GetWorkflowProviderDefinitionRequest) (*proto.WorkflowDefinition, error) {
	if s.calls != nil {
		s.calls.Add(1)
	}
	return &proto.WorkflowDefinition{
		ProviderName: "registered",
		Id:           req.GetDefinitionId(),
	}, nil
}

type relayTestAgentProviderServer struct {
	proto.UnimplementedAgentServer
	calls *atomic.Int64
}

func (s relayTestAgentProviderServer) GetSession(_ context.Context, req *proto.GetAgentProviderSessionRequest) (*proto.AgentSession, error) {
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
	proto.UnimplementedRuntimeLogHostServer
	calls *atomic.Int64
}

func (s relayTestRuntimeLogHostServer) AppendLogs(_ context.Context, req *proto.AppendRuntimeLogsRequest) (*proto.AppendRuntimeLogsResponse, error) {
	if s.calls != nil {
		s.calls.Add(1)
	}
	return &proto.AppendRuntimeLogsResponse{LastSeq: int64(len(req.GetLogs()))}, nil
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
		Name:           "cache",
		MethodPrefixes: []string{"/gestalt.provider.v1.Cache/"},
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
		AppName:      "support",
		SessionID:    "session-1",
		Service:      "cache",
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
		Name:           "cache",
		MethodPrefixes: []string{"/gestalt.provider.v1.Cache/"},
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
		AppName:      "support",
		SessionID:    "session-1",
		Service:      "cache",
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
		Name:           "cache",
		MethodPrefixes: []string{"/gestalt.provider.v1.Cache/"},
		Register: func(srv *grpc.Server) {
			proto.RegisterCacheServer(srv, cacheSrv1)
		},
	}
	hostService2 := runtimehost.HostService{
		Name:           "cache",
		MethodPrefixes: []string{"/gestalt.provider.v1.Cache/"},
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
		AppName:      "support",
		SessionID:    "session-2",
		Service:      "cache",
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
		AppName:      "support",
		SessionID:    "session-1",
		Service:      "cache",
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
		Name:           "cache",
		MethodPrefixes: []string{"/gestalt.provider.v1.Cache/"},
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
		AppName:      "support",
		SessionID:    "session-1",
		Service:      "cache",
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

func TestHostServiceRelayRoutesRegisteredAppService(t *testing.T) {
	t.Parallel()

	secret := []byte("relay-test-secret-0123456789abcd")
	invoker := &relayTestInvoker{}
	publicHostServices := runtimehost.NewPublicHostServiceRegistry()
	sessionVerifier := newRelayTestSessionVerifier("relay-session")
	publicHostServices.RegisterVerified("support", sessionVerifier, runtimehost.HostService{
		Name:           "app",
		MethodPrefixes: []string{"/" + proto.App_ServiceDesc.ServiceName + "/"},
		Register: func(srv *grpc.Server) {
			proto.RegisterAppServer(srv, appaccessservice.NewServer(
				invoker,
				appaccessservice.WithCallerApp("support"),
			))
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
		AppName:      "support",
		SessionID:    "relay-session",
		Service:      "app",
		MethodPrefix: "/" + proto.App_ServiceDesc.ServiceName + "/",
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
	_, err = proto.NewAppClient(conn).Invoke(ctx, &proto.AppInvokeRequest{
		Context:        relayAppRequestContext(),
		App:            "slack",
		Operation:      "events.reply",
		Instance:       "prod",
		IdempotencyKey: "relay-call",
	})
	if err != nil {
		t.Fatalf("AppInvocation.Invoke via registered relay: %v", err)
	}
	if call := invoker.snapshot(); call.calls != 1 || call.providerName != "slack" || call.operation != "events.reply" || call.instance != "prod" {
		t.Fatalf("plugin invoker call = %+v, want slack events.reply/prod", call)
	}

	sessionVerifier.setActive("relay-session", false)
	staleCtx, staleCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer staleCancel()
	staleCtx = metadata.NewOutgoingContext(staleCtx, metadata.Pairs(runtimehost.HostServiceRelayTokenHeader, relayToken))
	_, err = proto.NewAppClient(conn).Invoke(staleCtx, &proto.AppInvokeRequest{
		Context:   relayAppRequestContext(),
		App:       "slack",
		Operation: "events.reply",
	})
	if grpcstatus.Code(err) != codes.Unauthenticated {
		t.Fatalf("AppInvocation.Invoke stale session code = %v, want %v (err=%v)", grpcstatus.Code(err), codes.Unauthenticated, err)
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
		methodPrefix string
		register     func(*grpc.Server, *atomic.Int64)
		call         func(*testing.T, context.Context, *grpc.ClientConn)
	}{
		{
			name:         "workflow provider",
			service:      "workflow_provider",
			methodPrefix: "/" + proto.Workflow_ServiceDesc.ServiceName + "/",
			register: func(srv *grpc.Server, calls *atomic.Int64) {
				proto.RegisterWorkflowServer(srv, relayTestWorkflowProviderServer{calls: calls})
			},
			call: func(t *testing.T, ctx context.Context, conn *grpc.ClientConn) {
				t.Helper()
				resp, err := proto.NewWorkflowClient(conn).GetDefinition(ctx, &proto.GetWorkflowProviderDefinitionRequest{DefinitionId: "definition-1"})
				if err != nil {
					t.Fatalf("WorkflowProvider.GetDefinition via relay: %v", err)
				}
				if resp.GetProviderName() != "registered" || resp.GetId() != "definition-1" {
					t.Fatalf("WorkflowProvider.GetDefinition response = %+v, want registered definition-1", resp)
				}
			},
		},
		{
			name:         "agent provider",
			service:      "agent_provider",
			methodPrefix: "/" + proto.Agent_ServiceDesc.ServiceName + "/",
			register: func(srv *grpc.Server, calls *atomic.Int64) {
				proto.RegisterAgentServer(srv, relayTestAgentProviderServer{calls: calls})
			},
			call: func(t *testing.T, ctx context.Context, conn *grpc.ClientConn) {
				t.Helper()
				resp, err := proto.NewAgentClient(conn).GetSession(ctx, &proto.GetAgentProviderSessionRequest{SessionId: "agent-session-1"})
				if err != nil {
					t.Fatalf("AgentProvider.GetSession via relay: %v", err)
				}
				if resp.GetProviderName() != "registered" || resp.GetId() != "agent-session-1" {
					t.Fatalf("AgentProvider.GetSession response = %+v, want registered agent-session-1", resp)
				}
			},
		},
		{
			name:         "runtime log host",
			service:      "runtime_log_host",
			methodPrefix: "/" + proto.RuntimeLogHost_ServiceDesc.ServiceName + "/",
			register: func(srv *grpc.Server, calls *atomic.Int64) {
				proto.RegisterRuntimeLogHostServer(srv, relayTestRuntimeLogHostServer{calls: calls})
			},
			call: func(t *testing.T, ctx context.Context, conn *grpc.ClientConn) {
				t.Helper()
				resp, err := proto.NewRuntimeLogHostClient(conn).AppendLogs(ctx, &proto.AppendRuntimeLogsRequest{
					SessionId: "runtime-session-1",
					Logs: []*proto.RuntimeLogEntry{{
						Stream:    proto.RuntimeLogStream_RUNTIME_LOG_STREAM_STDOUT,
						Message:   "hello",
						SourceSeq: 1,
					}},
				})
				if err != nil {
					t.Fatalf("RuntimeLogHost.AppendLogs via relay: %v", err)
				}
				if resp.GetLastSeq() != 1 {
					t.Fatalf("RuntimeLogHost.AppendLogs last_seq = %d, want 1", resp.GetLastSeq())
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
				Name:           tc.service,
				MethodPrefixes: []string{tc.methodPrefix},
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
				AppName:      "support",
				SessionID:    "session-1",
				Service:      tc.service,
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
			case "workflow_provider":
				_, err = proto.NewWorkflowClient(conn).GetDefinition(staleCtx, &proto.GetWorkflowProviderDefinitionRequest{DefinitionId: "definition-1"})
			case "agent_provider":
				_, err = proto.NewAgentClient(conn).GetSession(staleCtx, &proto.GetAgentProviderSessionRequest{SessionId: "agent-session-1"})
			case "runtime_log_host":
				_, err = proto.NewRuntimeLogHostClient(conn).AppendLogs(staleCtx, &proto.AppendRuntimeLogsRequest{
					SessionId: "runtime-session-1",
					Logs:      []*proto.RuntimeLogEntry{{Message: "stale"}},
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
	ts := httptest.NewUnstartedServer(newTestHandler(t, func(cfg *server.Config) {
		cfg.RouteProfile = server.RouteProfilePublic
		cfg.StateSecret = secret
		cfg.Invoker = invoker
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"support": {},
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
		AppName:      "support",
		SessionID:    "relay-session",
		Service:      "app",
		MethodPrefix: "/" + proto.App_ServiceDesc.ServiceName + "/",
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
	_, err = proto.NewAppClient(conn).Invoke(ctx, &proto.AppInvokeRequest{})
	if grpcstatus.Code(err) != codes.Unavailable {
		t.Fatalf("AppInvocation.Invoke without registered service code = %v, want %v (err=%v)", grpcstatus.Code(err), codes.Unavailable, err)
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
		Name:           "cache",
		MethodPrefixes: []string{"/gestalt.provider.v1.Cache/"},
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
		AppName:      "support",
		SessionID:    "session-1",
		Service:      "cache",
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
		Name:           "indexeddb",
		MethodPrefixes: []string{"/" + proto.IndexedDB_ServiceDesc.ServiceName + "/"},
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
		AppName:      "relay-plugin",
		SessionID:    "session-1",
		Service:      "indexeddb",
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
		AppName:      "support",
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
		AppName:      "support",
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
		AppName:      "support",
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
		AppName:      "support",
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
		AppName:      "support",
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
	exchangeCodeFn           func(ctx context.Context, code string) (*core.OAuthTokenResponse, error)
	exchangeCodeWithVerFn    func(ctx context.Context, code, verifier string, opts ...oauth.ExchangeOption) (*core.OAuthTokenResponse, error)
	refreshTokenFn           func(ctx context.Context, refreshToken string) (*core.OAuthTokenResponse, error)
	refreshTokenWithURLFn    func(ctx context.Context, refreshToken, tokenURL string) (*core.OAuthTokenResponse, error)
	authorizationBaseURLVal  string
	tokenURLVal              string
}

func (h *testOAuthHandler) AuthorizationURL(state string, scopes []string) string {
	if h.authorizationURLFn != nil {
		return h.authorizationURLFn(state, scopes)
	}
	return h.authorizationBaseURLVal + "?state=" + state
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

func (h *testOAuthHandler) ExchangeCode(ctx context.Context, code string) (*core.OAuthTokenResponse, error) {
	if h.exchangeCodeFn != nil {
		return h.exchangeCodeFn(ctx, code)
	}
	return nil, fmt.Errorf("ExchangeCode not implemented")
}

func (h *testOAuthHandler) ExchangeCodeWithVerifier(ctx context.Context, code, verifier string, opts ...oauth.ExchangeOption) (*core.OAuthTokenResponse, error) {
	if h.exchangeCodeWithVerFn != nil {
		return h.exchangeCodeWithVerFn(ctx, code, verifier, opts...)
	}
	return h.ExchangeCode(ctx, code)
}

func (h *testOAuthHandler) RefreshToken(ctx context.Context, refreshToken string) (*core.OAuthTokenResponse, error) {
	if h.refreshTokenFn != nil {
		return h.refreshTokenFn(ctx, refreshToken)
	}
	return nil, fmt.Errorf("RefreshToken not implemented")
}

func (h *testOAuthHandler) RefreshTokenWithURL(ctx context.Context, refreshToken, tokenURL string) (*core.OAuthTokenResponse, error) {
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

func oauthConnectionDef(params map[string]config.ConnectionParamDef) *config.ConnectionDef {
	return &config.ConnectionDef{
		Mode: providermanifestv1.ConnectionModeSubject,
		Auth: config.ConnectionAuthDef{
			Type:             providermanifestv1.AuthTypeOAuth2,
			AuthorizationURL: "https://provider.example/oauth/authorize",
			TokenURL:         "https://provider.example/oauth/token",
		},
		ConnectionParams: params,
	}
}

func oauthRefreshConnectionAuth(integration string, refreshFn func(context.Context, string) (*core.OAuthTokenResponse, error)) func() map[string]map[string]bootstrap.OAuthHandler {
	return testConnectionAuth(integration, &testOAuthHandler{refreshTokenFn: refreshFn})
}

func cloneAccessPermissionsForTest(src []core.AccessPermission) []core.AccessPermission {
	if len(src) == 0 {
		return nil
	}
	out := append([]core.AccessPermission(nil), src...)
	for i := range out {
		out[i].Operations = append([]string(nil), out[i].Operations...)
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

func seedUserRecord(t *testing.T, svc *coredata.Services, id, email string, createdAt time.Time) *core.User {
	t.Helper()
	ctx := context.Background()
	rec := idb.Record{
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
		resolvedConnection = config.AppConnectionName
	}
	seedToken(t, svc, &core.ExternalCredential{
		ID:        integration + "-" + connection + "-" + instance,
		Subject:   subjectID,
		Audience:  integration + ":" + resolvedConnection,
		Qualifier: instance,
		Grant:     &core.ExternalCredentialGrant{AccessToken: accessToken},
	})
}

func seedToken(t *testing.T, svc *coredata.Services, tok *core.ExternalCredential) {
	t.Helper()
	ctx := context.Background()
	if err := svc.ExternalCredentials.UpsertCredential(ctx, tok); err != nil {
		t.Fatalf("seedToken: %v", err)
	}
}

func testPluginDefsForConnections(plugin string, connections ...string) map[string]*config.ProviderEntry {
	entry := &config.ProviderEntry{}
	for _, connection := range connections {
		connection = config.ResolveConnectionAlias(connection)
		if connection == "" || connection == config.AppConnectionName {
			continue
		}
		if entry.Connections == nil {
			entry.Connections = map[string]*config.ConnectionDef{}
		}
		entry.Connections[connection] = &config.ConnectionDef{
			ConnectionID: plugin + ":" + connection,
			Mode:         providermanifestv1.ConnectionModeSubject,
			Auth: config.ConnectionAuthDef{
				Type: providermanifestv1.AuthTypeManual,
			},
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
			Routes: []server.MountedUIRoute{{
				Path:         "/*",
				AllowedRoles: []string{"viewer"},
			}},
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

func TestMountedUIThemeRoutes(t *testing.T) {
	t.Parallel()

	uiDir := t.TempDir()
	writeTestUIAsset(t, filepath.Join(uiDir, "index.html"), "<html>portal-shell</html>")

	themeDir := t.TempDir()
	const stylesheetBody = ":root{--brand:#123456;}"
	writeTestUIAsset(t, filepath.Join(themeDir, "tenant.css"), stylesheetBody)
	writeTestUIAsset(t, filepath.Join(themeDir, "secret.css"), "outside-theme-assets")
	writeTestUIAsset(t, filepath.Join(themeDir, "assets", "fonts", "brand.woff2"), "woff2-bytes")

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.ProviderUIs = map[string]*config.UIEntry{
			"portal": {
				Path: "/portal",
				ProviderEntry: config.ProviderEntry{
					ResolvedAssetRoot: uiDir,
				},
				ResolvedThemeStylesheet: filepath.Join(themeDir, "tenant.css"),
				ResolvedThemeAssetsDir:  filepath.Join(themeDir, "assets"),
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/portal/theme.css")
	if err != nil {
		t.Fatalf("GET theme.css: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll theme.css: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("theme.css status = %d, want 200", resp.StatusCode)
	}
	if got := string(body); got != stylesheetBody {
		t.Fatalf("theme.css body = %q, want %q", got, stylesheetBody)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/css; charset=utf-8" {
		t.Fatalf("theme.css Content-Type = %q, want text/css; charset=utf-8", got)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("theme.css Cache-Control = %q, want no-cache", got)
	}
	sum := sha256.Sum256([]byte(stylesheetBody))
	wantETag := `"` + hex.EncodeToString(sum[:]) + `"`
	if got := resp.Header.Get("ETag"); got != wantETag {
		t.Fatalf("theme.css ETag = %q, want %q", got, wantETag)
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/portal/theme.css", nil)
	req.Header.Set("If-None-Match", wantETag)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET theme.css revalidation: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotModified {
		t.Fatalf("theme.css revalidation status = %d, want 304", resp.StatusCode)
	}
	if got := resp.Header.Get("ETag"); got != wantETag {
		t.Fatalf("theme.css revalidation ETag = %q, want %q", got, wantETag)
	}

	resp, err = http.Get(ts.URL + "/portal/theme/fonts/brand.woff2")
	if err != nil {
		t.Fatalf("GET theme asset: %v", err)
	}
	body, err = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll theme asset: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("theme asset status = %d, want 200", resp.StatusCode)
	}
	if got := string(body); got != "woff2-bytes" {
		t.Fatalf("theme asset body = %q, want woff2-bytes", got)
	}
	if got := resp.Header.Get("Content-Type"); got != "font/woff2" {
		t.Fatalf("theme asset Content-Type = %q, want font/woff2", got)
	}

	for _, traversal := range []string{
		"/portal/theme/../secret.css",
		"/portal/theme/%2e%2e/secret.css",
		"/portal/theme/fonts/../../secret.css",
	} {
		req, err := http.NewRequest(http.MethodGet, ts.URL+traversal, nil)
		if err != nil {
			t.Fatalf("NewRequest %q: %v", traversal, err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET %q: %v", traversal, err)
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			t.Fatalf("ReadAll %q: %v", traversal, err)
		}
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("traversal %q status = %d, want 404", traversal, resp.StatusCode)
		}
		if strings.Contains(string(body), "outside-theme-assets") {
			t.Fatalf("traversal %q leaked file outside the theme assets dir", traversal)
		}
	}

	resp, err = http.Get(ts.URL + "/portal/theme/missing.css")
	if err != nil {
		t.Fatalf("GET missing theme asset: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing theme asset status = %d, want 404", resp.StatusCode)
	}
}

func TestMountedUIThemeStylesheetUnconfigured(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	writeTestUIAsset(t, filepath.Join(rootDir, "index.html"), "<html>root-shell</html>")

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.ProviderUIs = map[string]*config.UIEntry{
			"root": {
				Path: "/",
				ProviderEntry: config.ProviderEntry{
					ResolvedAssetRoot: rootDir,
				},
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/theme.css")
	if err != nil {
		t.Fatalf("GET theme.css: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll theme.css: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("theme.css status = %d, want 200", resp.StatusCode)
	}
	if len(body) != 0 {
		t.Fatalf("theme.css body = %q, want empty (must not fall through to index.html)", body)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/css; charset=utf-8" {
		t.Fatalf("theme.css Content-Type = %q, want text/css; charset=utf-8", got)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("theme.css Cache-Control = %q, want no-cache", got)
	}
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Fatal("theme.css ETag missing")
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/theme.css", nil)
	req.Header.Set("If-None-Match", etag)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET theme.css revalidation: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotModified {
		t.Fatalf("theme.css revalidation status = %d, want 304", resp.StatusCode)
	}

	// The SPA fallback still answers navigations; only /theme.css is intercepted.
	resp, err = http.Get(ts.URL + "/dashboard")
	if err != nil {
		t.Fatalf("GET SPA navigation: %v", err)
	}
	body, err = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll SPA navigation: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("SPA navigation status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(string(body), "root-shell") {
		t.Fatalf("SPA navigation body = %q, want root shell", body)
	}
}

func TestPolicyBoundMountedUIThemeKeepsAuthSemantics(t *testing.T) {
	t.Parallel()

	uiDir := t.TempDir()
	writeTestUIAsset(t, filepath.Join(uiDir, "index.html"), "<html>brand-shell</html>")
	themeDir := t.TempDir()
	const stylesheetBody = ":root{--brand:#654321;}"
	writeTestUIAsset(t, filepath.Join(themeDir, "tenant.css"), stylesheetBody)

	handler, err := testutilUIHandler(uiDir)
	if err != nil {
		t.Fatalf("ui handler: %v", err)
	}

	svc := testutil.NewStubServices(t)
	user := seedUserRecord(t, svc, "theme-user", "theme-user@example.test", time.Now())
	authz := &serverTestAuthorizationProvider{
		resourceTypes: []*proto.AuthorizationModelResourceType{{
			Name:                "brandPolicy",
			DefaultAccessPolicy: proto.DefaultAccessPolicy_DEFAULT_ACCESS_POLICY_DENY,
		}},
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(
				principal.UserSubjectID(user.ID),
				"viewer",
				"brandPolicy",
				"brandPolicy",
			),
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "session-token" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: "theme-user@example.test"}, nil
			},
		}
		cfg.Services = svc
		cfg.Authorization = authz
		cfg.MountedUIs = []server.MountedUI{{
			Name:                "brand-ui",
			Path:                "/brand",
			AppName:             "brand",
			AuthorizationPolicy: "brandPolicy",
			Routes: []server.MountedUIRoute{{
				Path:         "/*",
				AllowedRoles: []string{"viewer"},
			}},
			Handler:         handler,
			ThemeStylesheet: filepath.Join(themeDir, "tenant.css"),
		}}
	})
	testutil.CloseOnCleanup(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := noRedirect.Get(ts.URL + "/brand/theme.css")
	if err != nil {
		t.Fatalf("GET theme.css unauthenticated: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("unauthenticated theme.css status = %d, want 302", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); !strings.HasPrefix(got, "/api/v1/auth/login") {
		t.Fatalf("unauthenticated theme.css Location = %q, want login redirect", got)
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/brand/theme.css", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "session-token"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET theme.css authorized: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll theme.css authorized: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authorized theme.css status = %d, want 200: %s", resp.StatusCode, body)
	}
	if got := string(body); got != stylesheetBody {
		t.Fatalf("authorized theme.css body = %q, want %q", got, stylesheetBody)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/css; charset=utf-8" {
		t.Fatalf("authorized theme.css Content-Type = %q, want text/css; charset=utf-8", got)
	}
}

func TestPolicyBoundMountedUIUsesAuthorizationRelationships(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	user := seedUserRecord(t, svc, "ui-user", "ui-user@example.test", time.Now())
	authz := &serverTestAuthorizationProvider{
		resourceTypes: []*proto.AuthorizationModelResourceType{
			{
				Name:                "dealHub",
				DefaultAccessPolicy: proto.DefaultAccessPolicy_DEFAULT_ACCESS_POLICY_DENY,
			},
			{
				Name:                "brainPolicy",
				DefaultAccessPolicy: proto.DefaultAccessPolicy_DEFAULT_ACCESS_POLICY_ALLOW,
			},
			{
				Name:                "gestaltAdmin",
				DefaultAccessPolicy: proto.DefaultAccessPolicy_DEFAULT_ACCESS_POLICY_DENY,
			},
		},
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(
				principal.UserSubjectID(user.ID),
				"admin",
				"dealHub",
				"dealHub",
			),
			testAuthorizationRelationship(
				principal.UserSubjectID(user.ID),
				"admin",
				"gestaltAdmin",
				"gestaltAdmin",
			),
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "session-token" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: "ui-user@example.test"}, nil
			},
		}
		cfg.Services = svc
		cfg.Authorization = authz
		cfg.MountedUIs = []server.MountedUI{
			{
				Name:    "deal-hub-ui",
				Path:    "/deal-hub",
				AppName: "dealHub",
				Routes: []server.MountedUIRoute{{
					Path:         "/*",
					AllowedRoles: []string{"admin"},
				}},
				Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					_, _ = w.Write([]byte("deal-hub-shell"))
				}),
			},
			{
				Name:                "brain-ui",
				Path:                "/brain",
				AppName:             "brain",
				AuthorizationPolicy: "brainPolicy",
				Routes: []server.MountedUIRoute{
					{Path: "/admin/*", AllowedRoles: []string{"admin"}},
					{Path: "/*", AllowedRoles: []string{"viewer"}},
				},
				Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					_, _ = w.Write([]byte("brain-shell"))
				}),
			},
		}
		cfg.AdminUI = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("admin-shell"))
		})
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/deal-hub/", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "session-token"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET protected mounted UI: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("protected mounted UI status = %d, want 200: %s", resp.StatusCode, body)
	}
	if !bytes.Contains(body, []byte("deal-hub-shell")) {
		t.Fatalf("protected mounted UI body = %q, want shell", body)
	}
	if len(authz.listRelationshipRequests) == 0 {
		t.Fatal("authorization ListRelationships was not called")
	}
	if got := authz.listRelationshipRequests[0].GetFilter().GetResource().GetType(); got != "dealHub" {
		t.Fatalf("relationship resource type = %q, want app resource type dealHub", got)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/brain/", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "session-token"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET viewer mounted UI: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("viewer mounted UI status = %d, want 200: %s", resp.StatusCode, body)
	}
	if !bytes.Contains(body, []byte("brain-shell")) {
		t.Fatalf("viewer mounted UI body = %q, want shell", body)
	}
	if got := authz.listRelationshipRequests[len(authz.listRelationshipRequests)-1].GetFilter().GetResource().GetType(); got != "brainPolicy" {
		t.Fatalf("relationship resource type = %q, want explicit AuthorizationPolicy brainPolicy", got)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/brain/admin/settings", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "session-token"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET admin mounted UI: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("admin mounted UI status = %d, want 403: %s", resp.StatusCode, body)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/admin/", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "session-token"})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET admin UI: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin UI status = %d, want 200: %s", resp.StatusCode, body)
	}
	if !bytes.Contains(body, []byte("admin-shell")) {
		t.Fatalf("admin UI body = %q, want shell", body)
	}
	if len(authz.listRelationshipRequests) < 2 {
		t.Fatalf("authorization ListRelationships calls = %d, want at least 2", len(authz.listRelationshipRequests))
	}
	if got := authz.listRelationshipRequests[len(authz.listRelationshipRequests)-1].GetFilter().GetResource().GetType(); got != "gestaltAdmin" {
		t.Fatalf("relationship resource type = %q, want gestaltAdmin", got)
	}
}

type serverTestAuthorizationProvider struct {
	core.AuthorizationProvider

	resourceTypes            []*proto.AuthorizationModelResourceType
	relationships            []*proto.Relationship
	listRelationshipRequests []*proto.ListRelationshipsRequest
}

func (p *serverTestAuthorizationProvider) ListRelationships(_ context.Context, req *proto.ListRelationshipsRequest) (*proto.ListRelationshipsResponse, error) {
	p.listRelationshipRequests = append(p.listRelationshipRequests, req)
	filter := req.GetFilter()
	out := []*proto.Relationship{}
	for _, relationship := range p.relationships {
		if !relationshipMatchesFilter(relationship, filter) {
			continue
		}
		out = append(out, relationship)
	}
	return &proto.ListRelationshipsResponse{Relationships: out}, nil
}

type serviceAccountCredentialAuthorizationProvider struct {
	core.AuthorizationProvider

	allowed  bool
	requests []*proto.CheckAccessRequest
}

func (p *serviceAccountCredentialAuthorizationProvider) CheckAccess(_ context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	p.requests = append(p.requests, req)
	return &proto.CheckAccessResponse{Allowed: p.allowed}, nil
}

func (p *serverTestAuthorizationProvider) ListActiveModelResourceTypes(_ context.Context, req *proto.ListActiveModelResourceTypesRequest) (*proto.ListActiveModelResourceTypesResponse, error) {
	name := strings.TrimSpace(req.GetFilter().GetName())
	out := []*proto.AuthorizationModelResourceType{}
	for _, resourceType := range p.resourceTypes {
		if name != "" && strings.TrimSpace(resourceType.GetName()) != name {
			continue
		}
		out = append(out, resourceType)
	}
	return &proto.ListActiveModelResourceTypesResponse{ResourceTypes: out}, nil
}

func relationshipMatchesFilter(relationship *proto.Relationship, filter *proto.RelationshipFilter) bool {
	if relationship == nil || filter == nil {
		return false
	}
	tuple := relationship.GetTuple()
	if resource := filter.GetResource(); resource != nil {
		if tuple.GetResource().GetType() != resource.GetType() || tuple.GetResource().GetId() != resource.GetId() {
			return false
		}
	}
	if relation := strings.TrimSpace(filter.GetRelation()); relation != "" && tuple.GetRelation() != relation {
		return false
	}
	if target := filter.GetTarget().GetSubject(); target != nil {
		subject := tuple.GetTarget().GetSubject()
		if subject.GetType() != target.GetType() || subject.GetId() != target.GetId() {
			return false
		}
	}
	return true
}

func testAuthorizationRelationship(subjectID, relation, resourceType, resourceID string) *proto.Relationship {
	return &proto.Relationship{
		Tuple: &proto.RelationshipTuple{
			Target: &proto.RelationshipTarget{
				Kind: &proto.RelationshipTarget_Subject{Subject: &proto.Subject{
					Type: "subject",
					Id:   subjectID,
				}},
			},
			Relation: relation,
			Resource: &proto.Resource{
				Type: resourceType,
				Id:   resourceID,
			},
		},
		SourceLayer: proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG,
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

func TestAdminAPI_RuntimeProviders(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Runtimes = &staticRuntimeInspector{
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
					Profile: bootstrap.RuntimePlacementPlan{
						CanHostApps: true,
						EgressMode:  bootstrap.RuntimeEgressModeCIDR,
					},
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
	if _, ok := providers[1]["sessionCount"]; ok {
		t.Fatalf("runtime providers[1].sessionCount unexpectedly present: %#v", providers[1]["sessionCount"])
	}
	profile, ok := providers[1]["profile"].(map[string]any)
	if !ok {
		t.Fatalf("runtime providers[1].profile = %#v, want object", providers[1]["profile"])
	}
	if profile["canHostApps"] != true || profile["egressMode"] != "cidr" {
		t.Fatalf("runtime providers[1].profile = %#v", profile)
	}
}

func TestAdminAPI_RuntimeProviderSessions(t *testing.T) {
	t.Parallel()

	inspector := &staticRuntimeInspector{
		snapshots: []bootstrap.RuntimeProviderSnapshot{{
			Name:   "modal",
			Driver: config.RuntimeProviderDriver("modal"),
			Loaded: true,
		}},
		sessions: map[string]*proto.ListRuntimeSessionsResponse{
			"modal": {
				NextPageToken: "next-page",
				Sessions: []*proto.RuntimeSession{{
					Id:    "session-1",
					State: "running",
					Metadata: map[string]string{
						"provider_name": "support",
						"owner":         "support-platform",
					},
				}},
			},
		},
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Runtimes = inspector
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/admin/api/v1/runtime/providers/modal/sessions?pageSize=500&pageToken=next-in")
	if err != nil {
		t.Fatalf("GET runtime provider sessions: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("runtime provider sessions status = %d, want 200: %s", resp.StatusCode, body)
	}

	var listed map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		t.Fatalf("decoding runtime provider sessions: %v", err)
	}
	sessions, ok := listed["sessions"].([]any)
	if !ok {
		t.Fatalf("runtime provider sessions response = %#v, want sessions array", listed)
	}
	if len(sessions) != 1 {
		t.Fatalf("runtime provider sessions len = %d, want 1", len(sessions))
	}
	session, ok := sessions[0].(map[string]any)
	if !ok {
		t.Fatalf("runtime provider sessions[0] = %#v, want object", sessions[0])
	}
	if got := session["id"]; got != "session-1" {
		t.Fatalf("runtime provider sessions[0].id = %v, want session-1", got)
	}
	if got := session["state"]; got != "running" {
		t.Fatalf("runtime provider sessions[0].state = %v, want %q", got, "running")
	}
	if got := session["app"]; got != "support" {
		t.Fatalf("runtime provider sessions[0].app = %v, want support", got)
	}
	if _, ok := session["metadata"]; ok {
		t.Fatalf("runtime provider sessions[0].metadata unexpectedly present: %#v", session["metadata"])
	}
	if got := listed["nextPageToken"]; got != "next-page" {
		t.Fatalf("runtime provider sessions nextPageToken = %v, want next-page", got)
	}
	inspector.mu.Lock()
	listReq := inspector.sessionRequests["modal"]
	inspector.mu.Unlock()
	if got, want := listReq.GetPageSize(), int32(200); got != want {
		t.Fatalf("runtime provider sessions pageSize = %d, want %d", got, want)
	}
	if got, want := listReq.GetPageToken(), "next-in"; got != want {
		t.Fatalf("runtime provider sessions pageToken = %q, want %q", got, want)
	}
}

func TestAdminAPI_RuntimeProviderSessionsRejectsInvalidPagination(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		inspector *staticRuntimeInspector
		path      string
		want      int
	}{
		{
			name:      "negative page size",
			inspector: &staticRuntimeInspector{},
			path:      "/admin/api/v1/runtime/providers/modal/sessions?pageSize=-1",
			want:      http.StatusBadRequest,
		},
		{
			name:      "provider invalid argument",
			inspector: &staticRuntimeInspector{err: grpcstatus.Error(codes.InvalidArgument, "bad token")},
			path:      "/admin/api/v1/runtime/providers/modal/sessions?pageToken=bad",
			want:      http.StatusBadRequest,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ts := newTestServer(t, func(cfg *server.Config) {
				cfg.Runtimes = tc.inspector
			})
			testutil.CloseOnCleanup(t, ts)

			resp, err := http.Get(ts.URL + tc.path)
			if err != nil {
				t.Fatalf("GET runtime provider sessions: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.want {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("runtime provider sessions status = %d, want %d: %s", resp.StatusCode, tc.want, body)
			}
		})
	}
}

func TestAdminAPI_RuntimeProviderSessionsForwardsTokenWithoutDefaultPageSize(t *testing.T) {
	t.Parallel()

	inspector := &staticRuntimeInspector{
		snapshots: []bootstrap.RuntimeProviderSnapshot{{
			Name:   "modal",
			Driver: config.RuntimeProviderDriver("modal"),
			Loaded: true,
		}},
		sessions: map[string]*proto.ListRuntimeSessionsResponse{
			"modal": {},
		},
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Runtimes = inspector
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/admin/api/v1/runtime/providers/modal/sessions?pageToken=next-in")
	if err != nil {
		t.Fatalf("GET runtime provider sessions: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("runtime provider sessions status = %d, want 200: %s", resp.StatusCode, body)
	}

	inspector.mu.Lock()
	listReq := inspector.sessionRequests["modal"]
	inspector.mu.Unlock()
	if got := listReq.PageSize; got != 0 {
		t.Fatalf("runtime provider sessions pageSize = %d, want 0 for token-only request", got)
	}
	if got, want := listReq.PageToken, "next-in"; got != want {
		t.Fatalf("runtime provider sessions pageToken = %q, want %q", got, want)
	}
}

func TestAdminAPI_RuntimeProviderInspectionError(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Runtimes = &staticRuntimeInspector{
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
		cfg.Runtimes = &staticRuntimeInspector{
			snapshots: []bootstrap.RuntimeProviderSnapshot{{
				Name:          "modal",
				Driver:        config.RuntimeProviderDriver("modal"),
				Loaded:        true,
				SupportLoaded: true,
				Profile: bootstrap.RuntimePlacementPlan{
					CanHostApps: true,
					EgressMode:  bootstrap.RuntimeEgressModeCIDR,
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
	if profile["egressMode"] != "cidr" {
		t.Fatalf("runtime providers[0].profile = %#v, want cidr egress mode", profile)
	}
}

func TestAdminAPI_RuntimeProviderSessionLogs(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.April, 23, 12, 0, 0, 0, time.UTC)
	appendedAt := observedAt.Add(2 * time.Second)
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Runtimes = &staticRuntimeInspector{
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
			cfg.Runtimes = &staticRuntimeInspector{}
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
			cfg.Runtimes = &staticRuntimeInspector{err: idb.ErrNotFound}
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

	resp, err = http.Get(ts.URL + "/apps")
	if err != nil {
		t.Fatalf("GET /apps: %v", err)
	}
	body, err = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll /apps: %v", err)
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

	resp, err := http.Get(ts.URL + "/apps")
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

func TestReadinessCheck_IndexedDBDown(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Readiness = func() string {
			return "indexeddb unavailable"
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
	if body["status"] != "indexeddb unavailable" {
		t.Fatalf("expected status 'indexeddb unavailable', got %q", body["status"])
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

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
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

	plaintext := scopedTestBearerToken("api-user", "")

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = testAuthStubForScopedBearer()
		cfg.Services = testutil.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	for _, tc := range []struct {
		name   string
		scheme string
	}{
		{name: "Bearer", scheme: "Bearer"},
		{name: "bearer", scheme: "bearer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
			req.Header.Set("Authorization", tc.scheme+" "+plaintext)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected 200, got %d", resp.StatusCode)
			}
		})
	}
}

func TestAuthMiddleware_InactiveBearerRejected(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = testAuthStubWithIntrospect(func(_ context.Context, _ string) (*core.IntrospectResponse, error) {
			return &core.IntrospectResponse{Active: false}, nil
		})
		cfg.Services = testutil.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
	req.Header.Set("Authorization", "Bearer inactive-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 401 for inactive bearer, got %d: %s", resp.StatusCode, body)
	}
}

func TestAuthMiddleware_NoAuth(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
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

func TestPluginRouteAuth_HTTPRoutesUseNamedAuthProvider(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	openProvider := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "open",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(_ context.Context, op string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: []byte("open:" + op)}, nil
			},
		},
		ops: []core.Operation{{Name: "ping", Method: http.MethodGet}},
	}
	lockedProvider := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "locked",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(_ context.Context, op string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: []byte("locked:" + op)}, nil
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
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"locked": {
				RouteAuth: &config.RouteAuthDef{Provider: "alt"},
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	t.Run("server-level routes and apps without overrides remain anonymous", func(t *testing.T) {
		t.Parallel()

		resp, err := http.Get(ts.URL + "/api/v1/apps")
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

		resp, err = http.Get(ts.URL + "/api/v1/apps/open/operations")
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

		resp, err = http.Get(ts.URL + "/api/v1/apps/locked/operations")
		if err != nil {
			t.Fatalf("GET locked operations: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusUnauthorized {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("locked operations status = %d, want 401: %s", resp.StatusCode, body)
		}
	})

	t.Run("named auth provider bearer passes through route auth", func(t *testing.T) {
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

		req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/locked/operations", nil)
		req.Header.Set("Authorization", "Bearer alt-session")
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

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
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

	plaintext := scopedTestBearerToken("api-user", "")

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = testAuthStubForScopedBearer()
		cfg.Services = testutil.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
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

	plaintext := scopedTestBearerToken("api-user", "")

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = testAuthStubForScopedBearer()
		cfg.Services = testutil.NewStubServices(t)
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
		cfg.AppDefs = testPluginDefsForConnections("slack", "default")
		cfg.Services = testutil.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
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
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"github": {
				MountPath: "/github",
			},
		}
		cfg.MountedUIs = []server.MountedUI{{
			Name:    "github",
			AppName: "github",
			Path:    "/github",
			Handler: handler,
		}}
		cfg.Services = testutil.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
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

func TestListIntegrationsShowsConnected(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.ExternalCredential{
		ID:        "tok1",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "slack:default",
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "test-token"},
	})

	stub := &coretesting.StubIntegration{N: "slack", DN: "Slack", Desc: "Team messaging"}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.AppDefs = testPluginDefsForConnections("slack", "default")
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
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
	)
	pluginDefs := map[string]*config.ProviderEntry{
		"manual-disconnected": {
			Connections: map[string]*config.ConnectionDef{
				testDefaultConnection: {
					ConnectionID: "manual-disconnected:" + testDefaultConnection,
					Mode:         providermanifestv1.ConnectionModeSubject,
					Auth: config.ConnectionAuthDef{
						Type: providermanifestv1.AuthTypeManual,
					},
				},
			},
		},
		"manual-connected": {
			Connections: map[string]*config.ConnectionDef{
				testDefaultConnection: {
					ConnectionID: "manual-connected:" + testDefaultConnection,
					Mode:         providermanifestv1.ConnectionModeSubject,
					Auth: config.ConnectionAuthDef{
						Type: providermanifestv1.AuthTypeManual,
					},
				},
			},
		},
		"manual-multi": {
			Connections: map[string]*config.ConnectionDef{
				testDefaultConnection: {
					ConnectionID: "manual-multi:" + testDefaultConnection,
					Mode:         providermanifestv1.ConnectionModeSubject,
					Auth: config.ConnectionAuthDef{
						Type: providermanifestv1.AuthTypeManual,
					},
				},
			},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = providers
		cfg.AppDefs = pluginDefs
		cfg.Services = svc
		cfg.DefaultConnection = map[string]string{
			"manual-connected": testDefaultConnection,
			"manual-multi":     testDefaultConnection,
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
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
}

func TestListIntegrations_StaleRefreshFailuresRequireReconnect(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	subjectID := principal.UserSubjectID(u.ID)
	past := time.Now().Add(-1 * time.Hour)
	future := time.Now().Add(1 * time.Hour)

	seedStatusToken := func(integration, connection, instance string, expiresAt *time.Time, refreshErrors int) {
		t.Helper()
		seedToken(t, svc, &core.ExternalCredential{
			ID:        integration + "-" + connection + "-" + instance,
			Subject:   subjectID,
			Audience:  integration + ":" + config.ResolveConnectionAlias(connection),
			Qualifier: instance,
			Grant:     &core.ExternalCredentialGrant{AccessToken: integration + "-" + instance + "-access-token", RefreshToken: integration + "-" + instance + "-refresh-token", ExpiresAt: expiresAt, RefreshErrorCount: refreshErrors},
		})
	}

	seedStatusToken("expired-failed", testDefaultConnection, "default", &past, 2)
	seedStatusToken("expired-untried", testDefaultConnection, "default", &past, 0)
	seedStatusToken("unexpired-error", testDefaultConnection, "default", &future, 3)
	seedStatusToken("mixed", testDefaultConnection, "valid", &future, 0)
	seedStatusToken("mixed", testDefaultConnection, "stale", &past, 1)
	seedStatusToken("all-invalid-multi", testDefaultConnection, "stale-a", &past, 1)
	seedStatusToken("all-invalid-multi", testDefaultConnection, "stale-b", &past, 3)
	seedStatusToken("named-stale", testDefaultConnection, "default", &future, 0)
	seedStatusToken("named-stale", "archive", "archive", &past, 1)

	providers := testutil.NewProviderRegistry(t,
		&stubManualProvider{StubIntegration: coretesting.StubIntegration{N: "expired-failed", DN: "Expired Failed"}},
		&stubManualProvider{StubIntegration: coretesting.StubIntegration{N: "expired-untried", DN: "Expired Untried"}},
		&stubManualProvider{StubIntegration: coretesting.StubIntegration{N: "unexpired-error", DN: "Unexpired Error"}},
		&stubManualProvider{StubIntegration: coretesting.StubIntegration{N: "mixed", DN: "Mixed"}},
		&stubManualProvider{StubIntegration: coretesting.StubIntegration{N: "all-invalid-multi", DN: "All Invalid Multi"}},
		&stubManualProvider{StubIntegration: coretesting.StubIntegration{N: "named-stale", DN: "Named Stale"}},
	)
	pluginDefs := map[string]*config.ProviderEntry{}
	for _, name := range []string{"expired-failed", "expired-untried", "unexpired-error", "mixed", "all-invalid-multi"} {
		pluginDefs[name] = testPluginDefsForConnections(name, testDefaultConnection)[name]
	}
	pluginDefs["named-stale"] = &config.ProviderEntry{
		Connections: map[string]*config.ConnectionDef{
			testDefaultConnection: {
				ConnectionID: "named-stale:" + testDefaultConnection,
				Mode:         providermanifestv1.ConnectionModeSubject,
				Auth: config.ConnectionAuthDef{
					Type: providermanifestv1.AuthTypeManual,
				},
			},
			"archive": {
				ConnectionID: "named-stale:archive",
				Mode:         providermanifestv1.ConnectionModeSubject,
				Auth: config.ConnectionAuthDef{
					Type: providermanifestv1.AuthTypeManual,
				},
			},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = providers
		cfg.AppDefs = pluginDefs
		cfg.Services = svc
		cfg.DefaultConnection = map[string]string{"named-stale": testDefaultConnection}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
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
		Name            string           `json:"name"`
		Status          string           `json:"status"`
		CredentialState string           `json:"credentialState"`
		HealthState     string           `json:"healthState"`
		Actions         []string         `json:"actions"`
		Instances       []map[string]any `json:"instances"`
		StatusCode      string           `json:"statusCode"`
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
	}

	assertStatus := func(name, status, credentialState, healthState string, actions []string) statusIntegration {
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
	assertConnection := func(integration statusIntegration, connection, status, credentialState, healthState, statusCode string, actions []string) statusConnection {
		t.Helper()
		for _, conn := range integration.Connections {
			if conn.Name != connection {
				continue
			}
			if conn.Status != status || conn.CredentialState != credentialState || conn.HealthState != healthState || conn.StatusCode != statusCode || !reflect.DeepEqual(conn.Actions, actions) {
				t.Fatalf("%s/%s connection = {status:%q credential:%q health:%q code:%q actions:%v}, want {%q %q %q %q %v}",
					integration.Name, connection, conn.Status, conn.CredentialState, conn.HealthState, conn.StatusCode, conn.Actions,
					status, credentialState, healthState, statusCode, actions)
			}
			return conn
		}
		t.Fatalf("%s connection %q missing: %+v", integration.Name, connection, integration.Connections)
		return statusConnection{}
	}

	expiredFailed := assertStatus("expired-failed", "needs_user_connection", "invalid", "unhealthy", []string{"reconnect", "disconnect"})
	assertConnection(expiredFailed, testDefaultConnection, "needs_user_connection", "invalid", "unhealthy", "reconnect_required", []string{"reconnect", "disconnect"})
	assertStatus("expired-untried", "ready", "connected", "not_checked", []string{"disconnect", "add_instance"})
	assertStatus("unexpired-error", "ready", "connected", "not_checked", []string{"disconnect", "add_instance"})
	mixed := assertStatus("mixed", "degraded", "invalid", "unhealthy", []string{"select_instance", "disconnect", "add_instance"})
	assertConnection(mixed, testDefaultConnection, "degraded", "invalid", "unhealthy", "reconnect_required", []string{"select_instance", "disconnect", "add_instance"})
	allInvalid := assertStatus("all-invalid-multi", "needs_user_connection", "invalid", "unhealthy", []string{"select_instance", "disconnect"})
	assertConnection(allInvalid, testDefaultConnection, "needs_user_connection", "invalid", "unhealthy", "reconnect_required", []string{"select_instance", "disconnect"})
	namedStale := assertStatus("named-stale", "degraded", "invalid", "unhealthy", []string{})
	assertConnection(namedStale, testDefaultConnection, "ready", "connected", "not_checked", "", []string{"disconnect", "add_instance"})
	assertConnection(namedStale, "archive", "needs_user_connection", "invalid", "unhealthy", "reconnect_required", []string{"disconnect"})
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
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"oauth-svc": {
				Connections: map[string]*config.ConnectionDef{
					"default": {
						ConnectionID: "oauth-svc:default",
						Mode:         providermanifestv1.ConnectionModeSubject,
						Auth: config.ConnectionAuthDef{
							Type: providermanifestv1.AuthTypeOAuth2,
						},
					},
				},
			},
			"manual-svc": {
				Connections: map[string]*config.ConnectionDef{
					"default": {
						ConnectionID: "manual-svc:default",
						Mode:         providermanifestv1.ConnectionModeSubject,
						Auth: config.ConnectionAuthDef{
							Type: providermanifestv1.AuthTypeManual,
						},
					},
				},
			},
			"clickhouse": {},
		}
		cfg.Services = testutil.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
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
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"example": plugin,
		}
		cfg.Services = testutil.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
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
						Connection: config.AppConnectionName,
					},
				},
				Connections: map[string]*providermanifestv1.ManifestConnectionDef{
					"default": {
						Mode: providermanifestv1.ConnectionModeSubject,
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
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"launchdarkly": plugin,
		}
		cfg.Services = testutil.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
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
		t.Fatalf("connections = %+v, want one declared default connection", integrations[0].Connections)
	}
	if integrations[0].Connections[0].Name != "default" {
		t.Fatalf("connection name = %q, want %q", integrations[0].Connections[0].Name, "default")
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
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"linear": {
				Auth: &config.ConnectionAuthDef{
					Type: providermanifestv1.AuthTypeManual,
				},
			},
		}
		cfg.Services = testutil.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
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
	if integrations[0].Connections[0].Name != config.AppConnectionAlias {
		t.Fatalf("connection name = %q, want %q", integrations[0].Connections[0].Name, config.AppConnectionAlias)
	}
	if !reflect.DeepEqual(integrations[0].Connections[0].AuthTypes, []string{"manual"}) {
		t.Fatalf("connection auth types = %v, want [manual]", integrations[0].Connections[0].AuthTypes)
	}
	if !reflect.DeepEqual(integrations[0].Connections[0].CredentialFields, wantFields) {
		t.Fatalf("connection credential fields = %+v, want %+v", integrations[0].Connections[0].CredentialFields, wantFields)
	}
}

func TestListIntegrations_ConnectionInfosUseResolvedConnectionDefs(t *testing.T) {
	t.Parallel()

	t.Run("non manifest-backed connections still expose app and named auth", func(t *testing.T) {
		t.Parallel()

		stub := &coretesting.StubIntegration{N: "example", DN: "Example"}
		plugin := &config.ProviderEntry{
			Source: config.NewMetadataSource("https://example.invalid/github-com-acme-plugins-example/v1.0.0/provider-release.yaml"),
			Auth: &config.ConnectionAuthDef{
				Type: providermanifestv1.AuthTypeManual,
				Credentials: []config.CredentialFieldDef{
					{Name: "plugin_token", Description: "App Config Description"},
					{Name: "plugin_local_only", Label: "App Local Only", Description: "App Local Only Description"},
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
									{Name: "plugin_token", Label: "App Manifest Token", Description: "App Manifest Description"},
									{Name: "plugin_manifest_only", Label: "App Manifest Only", Description: "App Manifest Only Description"},
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
			cfg.AppDefs = map[string]*config.ProviderEntry{
				"example": plugin,
			}
			cfg.Services = testutil.NewStubServices(t)
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
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

		if !reflect.DeepEqual(got[config.AppConnectionAlias].AuthTypes, []string{"manual"}) || !reflect.DeepEqual(got[config.AppConnectionAlias].CredentialFields, []credentialField{
			{Name: "plugin_token", Label: "App Manifest Token", Description: "App Config Description"},
			{Name: "plugin_manifest_only", Label: "App Manifest Only", Description: "App Manifest Only Description"},
			{Name: "plugin_local_only", Label: "App Local Only", Description: "App Local Only Description"},
		}) {
			t.Fatalf("plugin connection info = %+v", got[config.AppConnectionAlias])
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
		t.Parallel()

		stub := &coretesting.StubIntegration{N: "example", DN: "Example"}
		plugin := &config.ProviderEntry{
			Source: config.NewMetadataSource("https://example.invalid/github-com-acme-plugins-example/v1.0.0/provider-release.yaml"),
			Auth: &config.ConnectionAuthDef{
				Type: providermanifestv1.AuthTypeManual,
				Credentials: []config.CredentialFieldDef{
					{Name: "plugin_token", Label: "App Token"},
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
			cfg.AppDefs = map[string]*config.ProviderEntry{
				"example": plugin,
			}
			cfg.Services = testutil.NewStubServices(t)
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
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
			cfg.AppDefs = map[string]*config.ProviderEntry{
				"clickhouse": plugin,
			}
			cfg.Services = testutil.NewStubServices(t)
			cfg.MountedUIs = []server.MountedUI{{
				Name:    "clickhouse",
				AppName: "clickhouse",
				Path:    "/clickhouse",
				Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusOK)
				}),
			}}
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
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
							Mode:        providermanifestv1.ConnectionModeSubject,
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
			cfg.AppDefs = map[string]*config.ProviderEntry{
				"clickhouse": plugin,
			}
			cfg.Services = testutil.NewStubServices(t)
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
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
			cfg.AppDefs = map[string]*config.ProviderEntry{
				"httpbin": plugin,
			}
			cfg.Services = testutil.NewStubServices(t)
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
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
			"default": {
				Auth: config.ConnectionAuthDef{
					Type: providermanifestv1.AuthTypeOAuth2,
				},
			},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.AppDefs = map[string]*config.ProviderEntry{
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

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
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
				cfg.AppDefs = map[string]*config.ProviderEntry{
					"example": tc.plugin,
				}
				cfg.Services = testutil.NewStubServices(t)
			})
			testutil.CloseOnCleanup(t, ts)

			req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
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
			if conn.Name != config.AppConnectionAlias {
				t.Fatalf("expected app connection, got %+v", conn)
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

		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
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
		ID:        "tok1",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "slack:default",
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "test-token"},
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
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"slack": testPluginDefsForConnections("slack", "default")["slack"],
			"github": {
				Connections: map[string]*config.ConnectionDef{
					testDefaultConnection: {
						ConnectionID: "github:" + testDefaultConnection,
						Mode:         providermanifestv1.ConnectionModeSubject,
					},
				},
			},
		}
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
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

func TestListIntegrations_ShowsConnectedStatus_AmbiguousMixedCaseDuplicates(t *testing.T) {
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

			req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
			req.AddCookie(&http.Cookie{Name: "session_token", Value: "session-token"})
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

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
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

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
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
		seedToken(t, svc, &core.ExternalCredential{
			ID:        "tok-1",
			Subject:   principal.UserSubjectID(u.ID),
			Audience:  "app-svc:" + config.AppConnectionName,
			Qualifier: "default",
			Grant:     &core.ExternalCredentialGrant{AccessToken: "test-token"},
		})

		stub := &coretesting.StubIntegration{N: "app-svc", DN: "App Service"}
		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, stub)
			cfg.AppDefs = testPluginDefsForConnections("app-svc")
			cfg.Services = svc
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/apps/app-svc", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}
		tokens, err := listTestCredentialsForProvider(context.Background(), svc.ExternalCredentials, principal.UserSubjectID(u.ID), "app-svc")
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
	})

	t.Run("shared connection remains while another token still exists", func(t *testing.T) {
		t.Parallel()

		svc := testutil.NewStubServices(t)
		u := seedUser(t, svc, "anonymous@gestalt")
		seedToken(t, svc, &core.ExternalCredential{
			ID:        "tok-1",
			Subject:   principal.UserSubjectID(u.ID),
			Audience:  "app-svc:workspace",
			Qualifier: "instance-a",
			Grant:     &core.ExternalCredentialGrant{AccessToken: "test-token-a"},
		})
		seedToken(t, svc, &core.ExternalCredential{
			ID:        "tok-2",
			Subject:   principal.UserSubjectID(u.ID),
			Audience:  "app-svc:workspace",
			Qualifier: "instance-b",
			Grant:     &core.ExternalCredentialGrant{AccessToken: "test-token-b"},
		})

		stub := &coretesting.StubIntegration{N: "app-svc", DN: "App Service"}
		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, stub)
			cfg.AppDefs = testPluginDefsForConnections("app-svc", "workspace")
			cfg.Services = svc
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/apps/app-svc?_connection=workspace&_instance=instance-a", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}
		tokens, err := listTestCredentialsForProvider(context.Background(), svc.ExternalCredentials, principal.UserSubjectID(u.ID), "app-svc")
		if err != nil {
			t.Fatalf("ListCredentialsForProvider: %v", err)
		}
		if len(tokens) != 1 {
			t.Fatalf("expected 1 token after disconnect, got %d", len(tokens))
		}
		if tokens[0].Audience != "app-svc:workspace" || tokens[0].Qualifier != "instance-b" {
			t.Fatalf("unexpected remaining token %+v", tokens[0])
		}
	})

	t.Run("bare disconnect remains ambiguous when multiple connections exist", func(t *testing.T) {
		t.Parallel()

		svc := testutil.NewStubServices(t)
		u := seedUser(t, svc, "anonymous@gestalt")
		seedToken(t, svc, &core.ExternalCredential{
			ID:        "tok-a",
			Subject:   principal.UserSubjectID(u.ID),
			Audience:  "app-svc:mcp",
			Qualifier: "MCP OAuth",
			Grant:     &core.ExternalCredentialGrant{AccessToken: "test-token"},
		})
		seedToken(t, svc, &core.ExternalCredential{
			ID:        "tok-b",
			Subject:   principal.UserSubjectID(u.ID),
			Audience:  "app-svc:default",
			Qualifier: "default",
			Grant:     &core.ExternalCredentialGrant{AccessToken: "test-token-2"},
		})

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{N: "app-svc", DN: "App Service"})
			cfg.AppDefs = testPluginDefsForConnections("app-svc", "mcp", "default")
			cfg.Services = svc
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/apps/app-svc", nil)
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
			ID:        "tok-b",
			Subject:   principal.UserSubjectID(u.ID),
			Audience:  "app-svc:workspace",
			Qualifier: "instance-b",
			Grant:     &core.ExternalCredentialGrant{AccessToken: "test-token"},
		})

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{N: "app-svc", DN: "App Service"})
			cfg.AppDefs = testPluginDefsForConnections("app-svc", "workspace")
			cfg.Services = svc
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/apps/app-svc?_connection=workspace&_instance=instance-b", nil)
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
			ID:        "tok-b",
			Subject:   principal.UserSubjectID(u.ID),
			Audience:  "app-svc:mcp",
			Qualifier: "MCP OAuth",
			Grant:     &core.ExternalCredentialGrant{AccessToken: "test-token"},
		})

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{N: "app-svc", DN: "App Service"})
			cfg.AppDefs = testPluginDefsForConnections("app-svc", "mcp")
			cfg.Services = svc
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/apps/app-svc?connection=mcp", nil)
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
			ID:        "tok-a",
			Subject:   principal.UserSubjectID(u.ID),
			Audience:  "app-svc:workspace",
			Qualifier: "instance-a",
			Grant:     &core.ExternalCredentialGrant{AccessToken: "test-token"},
		})
		seedToken(t, svc, &core.ExternalCredential{
			ID:        "tok-b",
			Subject:   principal.UserSubjectID(u.ID),
			Audience:  "app-svc:workspace",
			Qualifier: "instance-b",
			Grant:     &core.ExternalCredentialGrant{AccessToken: "test-token-2"},
		})

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{N: "app-svc", DN: "App Service"})
			cfg.AppDefs = testPluginDefsForConnections("app-svc", "workspace")
			cfg.AuditSink = invocation.NewSlogAuditSink(&auditBuf)
			cfg.Services = svc
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/apps/app-svc?_connection=workspace", nil)
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

	stub := &coretesting.StubIntegration{N: "app-svc", DN: "App Service"}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Services = testutil.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/apps/app-svc", nil)
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

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/test-int/operations", nil)
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
			StubIntegration: coretesting.StubIntegration{N: "test-int", ConnMode: core.ConnectionModeSubject},
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
		ID:        "tok-cat",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "test-int:" + testCatalogConnection,
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: testCatalogToken},
	})
	seedToken(t, svc, &core.ExternalCredential{
		ID:        "tok-cat-alt",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "test-int:" + altCatalogConnection,
		Qualifier: altInstance,
		Grant:     &core.ExternalCredentialGrant{AccessToken: altCatalogToken},
	})

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"test-int": testDefaultConnection}
		cfg.CatalogConnection = map[string]string{"test-int": testCatalogConnection}
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/test-int/operations", nil)
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
	if len(ops) != 3 {
		t.Fatalf("expected 3 operations, got %d", len(ops))
	}
	if ops[0]["id"] != "alpha_mcp" {
		t.Fatalf("expected first id 'alpha_mcp', got %v", ops[0]["id"])
	}
	if ops[1]["id"] != "alpha_rest" {
		t.Fatalf("expected second id 'alpha_rest', got %v", ops[1]["id"])
	}
	if ops[2]["id"] != "zeta_rest" {
		t.Fatalf("expected third id 'zeta_rest', got %v", ops[2]["id"])
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/test-int/operations?_connection="+altCatalogConnection+"&_instance="+altInstance, nil)
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
	if len(ops) != 3 {
		t.Fatalf("expected 3 override operations, got %d", len(ops))
	}
	if ops[0]["id"] != "beta_mcp_alt" {
		t.Fatalf("expected first id 'beta_mcp_alt', got %v", ops[0]["id"])
	}
	if ops[1]["id"] != "beta_rest_alt" {
		t.Fatalf("expected second id 'beta_rest_alt', got %v", ops[1]["id"])
	}
	if ops[2]["id"] != "zeta_rest" {
		t.Fatalf("expected third id 'zeta_rest', got %v", ops[2]["id"])
	}
	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/test-int/operations?connection="+altCatalogConnection+"&instance="+altInstance, nil)
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
	if len(ops) != 3 {
		t.Fatalf("expected 3 query override operations, got %d", len(ops))
	}
	if ops[0]["id"] != "alpha_mcp" {
		t.Fatalf("expected first id 'alpha_mcp' for query override, got %v", ops[0]["id"])
	}
	if ops[1]["id"] != "alpha_rest" {
		t.Fatalf("expected second id 'alpha_rest' for query override, got %v", ops[1]["id"])
	}
	if ops[2]["id"] != "zeta_rest" {
		t.Fatalf("expected third id 'zeta_rest' for query override, got %v", ops[2]["id"])
	}
}

func TestListOperations_FallsBackToStaticCatalogWhenSessionCatalogErrors(t *testing.T) {
	t.Parallel()

	stub := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "notion", ConnMode: core.ConnectionModeSubject},
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
		ID:        "tok-mcp",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "notion:MCP",
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "mcp-token"},
	})
	seedToken(t, svc, &core.ExternalCredential{
		ID:        "tok-oauth",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "notion:OAuth",
		Qualifier: "OAuth",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "oauth-token"},
	})

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.CatalogConnection = map[string]string{"notion": "OAuth"}
		cfg.MCPConnection = map[string]string{"notion": "MCP"}
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

	assertListOperations("/api/v1/apps/notion/operations")
	assertListOperations("/api/v1/apps/notion/operations?_connection=OAuth&_instance=OAuth")
}

func TestListOperations_SessionCatalogAuthFailureReturnsReconnectRequired(t *testing.T) {
	t.Parallel()

	stub := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "notion", ConnMode: core.ConnectionModeSubject},
		},
		catalog: &catalog.Catalog{
			Name: "notion",
			Operations: []catalog.CatalogOperation{
				{ID: "search", Description: "Search pages", Method: http.MethodPost, Transport: catalog.TransportREST},
			},
		},
		catalogForRequestFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			return nil, fmt.Errorf("mcpupstream notion: initialize: transport error: unauthorized (401) for %q", token)
		},
	}

	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.ExternalCredential{
		ID:        "tok-oauth",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "notion:OAuth",
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "oauth-token"},
	})

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.CatalogConnection = map[string]string{"notion": "OAuth"}
		cfg.MCPConnection = map[string]string{"notion": "MCP"}
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/notion/operations", nil)
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
		Code string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errResp.Code != "reconnect_required" {
		t.Fatalf("error code = %q, want reconnect_required", errResp.Code)
	}
}

func TestListOperations_RetriesDefaultConnectionAfterBrokerCatalogError(t *testing.T) {
	t.Parallel()

	stub := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "sample-int", ConnMode: core.ConnectionModeSubject},
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
		ID:        "tok-rest",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "sample-int:rest-conn",
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "rest-token"},
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

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/sample-int/operations", nil)
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

func TestListOperations_NotFound(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, func(cfg *server.Config) {
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/nonexistent/operations", nil)
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
				StubIntegration: coretesting.StubIntegration{N: "test-int", ConnMode: core.ConnectionModeSubject},
			},
		}

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, stub)
			cfg.CatalogConnection = map[string]string{"test-int": testCatalogConnection}
			cfg.Services = testutil.NewStubServices(t)
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/test-int/operations", nil)
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
			ID:        "tok-a",
			Subject:   principal.UserSubjectID(u.ID),
			Audience:  "test-int:" + testCatalogConnection,
			Qualifier: "inst-a",
			Grant:     &core.ExternalCredentialGrant{AccessToken: "tok-a"},
		})
		seedToken(t, svc, &core.ExternalCredential{
			ID:        "tok-b",
			Subject:   principal.UserSubjectID(u.ID),
			Audience:  "test-int:" + testCatalogConnection,
			Qualifier: "inst-b",
			Grant:     &core.ExternalCredentialGrant{AccessToken: "tok-b"},
		})

		stub := &stubIntegrationWithSessionCatalog{
			stubIntegrationWithOps: stubIntegrationWithOps{
				StubIntegration: coretesting.StubIntegration{N: "test-int", ConnMode: core.ConnectionModeSubject},
			},
		}

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, stub)
			cfg.CatalogConnection = map[string]string{"test-int": testCatalogConnection}
			cfg.Services = svc
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/test-int/operations", nil)
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
				StubIntegration: coretesting.StubIntegration{N: "sample-int", ConnMode: core.ConnectionModeSubject},
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

		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/sample-int/operations", nil)
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
		ID:        "tok1",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "test-int:" + config.AppConnectionName,
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "test-token"},
	})

	fullStub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N: "test-int",
			ExecuteFn: func(_ context.Context, op string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return &core.OperationResult{
					Status: http.StatusOK,
					Body:   []byte(fmt.Sprintf(`{"operation":%q}`, op)),
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
				return &core.OperationResult{Status: http.StatusOK, Body: []byte(`{"ok":true}`)}, nil
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
		ID:        "svc-workspace-default",
		Subject:   principal.UserSubjectID(user.ID),
		Audience:  "svc:workspace",
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "workspace-token"},
	})

	backend := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N: "search-backend",
			ExecuteFn: func(_ context.Context, op string, _ map[string]any, token string) (*core.OperationResult, error) {
				return &core.OperationResult{
					Status: http.StatusOK,
					Body:   []byte(fmt.Sprintf(`{"operation":%q,"token":%q}`, op, token)),
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
			ConnMode: core.ConnectionModeSubject,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				called = true
				return &core.OperationResult{Status: http.StatusOK, Body: []byte(`{"ok":true}`)}, nil
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
			StubIntegration: coretesting.StubIntegration{N: "sample-mcp", ConnMode: core.ConnectionModeSubject},
		},
	})

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, prov)
		cfg.Services = testutil.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/sample-svc/api_get_resource?_connection="+config.AppConnectionAlias, nil)
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
	if !strings.Contains(body["error"], `"`+config.AppConnectionAlias+`"`) {
		t.Fatalf("expected requested connection in error, got %q", body["error"])
	}
	if strings.Contains(body["error"], `"`+config.AppConnectionName+`"`) {
		t.Fatalf("expected error to preserve caller input, got %q", body["error"])
	}
	if called {
		t.Fatal("expected provider execution to be skipped")
	}
}

func TestExecuteOperation_UsesResolvedConnectionForSessionCatalogOperation(t *testing.T) {
	t.Parallel()

	const (
		integration = "sample-svc"
		operation   = "session_graphql"
	)

	executed := false
	prov := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N:        integration,
				ConnMode: core.ConnectionModeNone,
				ExecuteFn: func(_ context.Context, gotOperation string, _ map[string]any, _ string) (*core.OperationResult, error) {
					executed = true
					if gotOperation != operation {
						t.Fatalf("operation = %q, want %q", gotOperation, operation)
					}
					return &core.OperationResult{Status: http.StatusOK, Body: []byte(`{"ok":true}`)}, nil
				},
			},
		},
		operationConnection: config.AppConnectionName,
		catalog: &catalog.Catalog{
			Name: integration,
			Operations: []catalog.CatalogOperation{{
				ID:        operation,
				Method:    http.MethodPost,
				Transport: "graphql",
			}},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, prov)
	})
	testutil.CloseOnCleanup(t, ts)

	body := strings.NewReader(`{"_connection":"prod"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/"+integration+"/"+operation, body)
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
	if !executed {
		t.Fatal("expected provider execution")
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
		ID:        "sample-svc-plugin-default",
		Subject:   principal.UserSubjectID(user.ID),
		Audience:  "sample-svc:" + config.AppConnectionName,
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "plugin-token"},
	})

	apiBackend := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "sample-api",
			ConnMode: core.ConnectionModeSubject,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
				return &core.OperationResult{
					Status: http.StatusOK,
					Body:   []byte(fmt.Sprintf(`{"token":%q}`, token)),
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
		composite.BoundProvider{Provider: apiBackend, Connection: config.AppConnectionName},
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

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/sample-svc/api_get_resource?_connection="+config.AppConnectionAlias, nil)
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
		case "/api/messages.send", "/api/messages.schedule", "/api/views.open":
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
		Source:      "messaging",
		DisplayName: "Messaging",
		Spec: &providermanifestv1.Spec{
			DefaultConnection: "default",
			Connections: map[string]*providermanifestv1.ManifestConnectionDef{
				"default": {
					Mode: providermanifestv1.ConnectionModeSubject,
					Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeBearer},
				},
				"bot": {
					Mode: providermanifestv1.ConnectionModeSubject,
					Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeBearer},
				},
			},
			Surfaces: &providermanifestv1.ProviderSurfaces{
				REST: &providermanifestv1.RESTSurface{
					Connection: "default",
					BaseURL:    upstream.URL,
					Operations: []providermanifestv1.ProviderOperation{
						{
							Name:        "messages.send",
							Description: "Send a message",
							Method:      http.MethodPost,
							Path:        "/api/messages.send",
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
							Name:        "messages.schedule",
							Description: "Schedule a message",
							Method:      http.MethodPost,
							Path:        "/api/messages.schedule",
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
							Description: "Open a view",
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
				Mode: providermanifestv1.ConnectionModeSubject,
				Auth: config.ConnectionAuthDef{
					Type: providermanifestv1.AuthTypeBearer,
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
	prov, err := appservice.NewDeclarativeProvider(
		manifest,
		upstream.Client(),
		appservice.WithDeclarativeConnectionMode(plan.ConnectionMode()),
		appservice.WithDeclarativeOperationConnections(restConnections, restSelectors, restLocks),
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
		ID:        "messaging-default",
		Subject:   subjectID,
		Audience:  "messaging:default",
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "user-messaging-token"},
	})
	seedSubjectToken(t, svc, subjectID, "messaging", "bot", "default", "bot-messaging-token")
	connectionRuntime, err := bootstrap.BuildConnectionRuntime(&config.Config{
		Apps: map[string]*config.ProviderEntry{"messaging": entry},
	})
	if err != nil {
		t.Fatalf("BuildConnectionRuntime: %v", err)
	}
	if runtimeInfo, ok := connectionRuntime.Resolve("messaging", "bot"); !ok || runtimeInfo.Mode != core.ConnectionModeSubject {
		t.Fatalf("runtime bot connection = (%+v, %v), want user-owned bot connection", runtimeInfo, ok)
	}
	broker := invocation.NewBroker(
		testutil.NewProviderRegistry(t, prov),
		svc.Users,
		svc.ExternalCredentials,
		invocation.WithConnectionRuntime(connectionRuntime.Resolve),
	)
	if _, token, err := broker.ResolveToken(context.Background(), &principal.Principal{SubjectID: subjectID}, "messaging", "bot", ""); err != nil || token != "bot-messaging-token" {
		t.Fatalf("ResolveToken bot = token %q, err %v; want subject-owned bot token", token, err)
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
		cfg.AppDefs = map[string]*config.ProviderEntry{"messaging": entry}
		cfg.Invoker = broker
	})
	testutil.CloseOnCleanup(t, ts)

	integrationsReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
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
	var botConnection *struct {
		Name            string   `json:"name"`
		Mode            string   `json:"mode"`
		Status          string   `json:"status"`
		CredentialState string   `json:"credentialState"`
		Actions         []string `json:"actions"`
		AuthTypes       []string `json:"authTypes"`
	}
	var messagingStatus, messagingCredentialState string
	for i := range integrations {
		if integrations[i].Name != "messaging" {
			continue
		}
		messagingStatus = integrations[i].Status
		messagingCredentialState = integrations[i].CredentialState
		for j := range integrations[i].Connections {
			if integrations[i].Connections[j].Name == "default" {
				defaultConnection = &integrations[i].Connections[j]
			}
			if integrations[i].Connections[j].Name == "bot" {
				botConnection = &integrations[i].Connections[j]
			}
		}
	}
	if defaultConnection == nil {
		t.Fatal("default connection missing from integrations response")
		return
	}
	if botConnection == nil {
		t.Fatal("bot connection missing from integrations response")
		return
	}
	if messagingStatus != "ready" || messagingCredentialState != "connected" {
		t.Fatalf("messaging status = {%q, %q}, want ready/connected", messagingStatus, messagingCredentialState)
	}
	if defaultConnection.Mode != "subject" || defaultConnection.Status != "ready" || defaultConnection.CredentialState != "connected" || !reflect.DeepEqual(defaultConnection.Actions, []string{"disconnect", "add_instance"}) {
		t.Fatalf("default connection metadata = %+v, want connected subject-scoped connection", *defaultConnection)
	}
	if botConnection.Mode != "subject" || botConnection.Status != "ready" || botConnection.CredentialState != "connected" {
		t.Fatalf("bot connection metadata = %+v, want connected subject-scoped bot connection", *botConnection)
	}

	opsReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/messaging/operations", nil)
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
	if _, ok := seenOps["assistant.threads.setStatus"]; !ok {
		t.Fatal("assistant.threads.setStatus missing from public operations response")
	}
	postMessage, ok := seenOps["messages.send"]
	if !ok {
		t.Fatal("messages.send missing from public operations response")
	}
	for _, param := range postMessage.Parameters {
		if param.Name == "actor" {
			t.Fatal("internal actor parameter leaked in public operations response")
		}
	}
	if strings.Contains(string(postMessage.InputSchema), "actor") {
		t.Fatalf("public messages.send input schema contains internal actor parameter: %s", postMessage.InputSchema)
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
	audienceValues := map[any]bool{}
	for _, value := range audienceEnum {
		audienceValues[value] = true
	}
	if len(audienceEnum) != 2 || !audienceValues["user"] || !audienceValues["bot"] {
		t.Fatalf("views.open audience enum = %#v, want user and bot", audienceEnum)
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
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/messaging/"+operation, strings.NewReader(body))
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

	if status := doInvoke("messages.send", `{"channel":"C1","text":"as user"}`); status != http.StatusOK {
		t.Fatalf("default user status = %d, want %d", status, http.StatusOK)
	}
	if status := doInvoke("messages.send", `{"channel":"C1","text":"bad actor","actor":"user"}`); status != http.StatusBadRequest {
		t.Fatalf("hidden actor status = %d, want %d", status, http.StatusBadRequest)
	}
	if status := doInvoke("messages.schedule?_connection=default", `{"channel":"C1","text":"scheduled","post_at":4102444800}`); status != http.StatusOK {
		t.Fatalf("surface fallback override status = %d, want %d", status, http.StatusOK)
	}
	if status := doInvoke("messages.schedule?_connection=bot", `{"channel":"C1","text":"scheduled","post_at":4102444800}`); status != http.StatusOK {
		t.Fatalf("bot connection override status = %d, want %d", status, http.StatusOK)
	}
	if status := doInvoke("views.open", `{"trigger_id":"T1","audience":"user"}`); status != http.StatusOK {
		t.Fatalf("non-internal selector status = %d, want %d", status, http.StatusOK)
	}
	if status := doInvoke("views.open", `{"trigger_id":"T1","audience":"bot"}`); status != http.StatusOK {
		t.Fatalf("bot selector status = %d, want %d", status, http.StatusOK)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	httpOperationAttrs := map[string]string{
		"http.route":                  "/api/v1/{integration}/{operation}",
		"gestaltd.provider.name":      "messaging",
		"gestaltd.operation.name":     "messages.send",
		"gestaltd.invocation.surface": "http",
	}
	subjectAttrs := maps.Clone(httpOperationAttrs)
	subjectAttrs["gestaltd.connection.mode"] = "subject"
	metrictest.RequireFloat64Histogram(t, rm, "http.server.request.duration", subjectAttrs)

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 5 {
		t.Fatalf("upstream calls = %d, want 5", len(calls))
	}
	if calls[0].path != "/api/messages.send" {
		t.Fatalf("first call path = %q, want messages.send", calls[0].path)
	}
	if calls[0].auth != "Bearer user-messaging-token" {
		t.Fatalf("first call auth = %q, want user token", calls[0].auth)
	}
	if _, ok := calls[0].body["actor"]; ok {
		t.Fatalf("first upstream body included internal actor param: %+v", calls[0].body)
	}
	if calls[1].path != "/api/messages.schedule" {
		t.Fatalf("second call path = %q, want messages.schedule", calls[1].path)
	}
	if calls[1].auth != "Bearer user-messaging-token" {
		t.Fatalf("second call auth = %q, want user token", calls[1].auth)
	}
	if calls[1].body["text"] != "scheduled" {
		t.Fatalf("second upstream body text = %#v, want scheduled", calls[1].body["text"])
	}
	if calls[2].path != "/api/messages.schedule" {
		t.Fatalf("third call path = %q, want messages.schedule", calls[2].path)
	}
	if calls[2].auth != "Bearer bot-messaging-token" {
		t.Fatalf("third call auth = %q, want bot token", calls[2].auth)
	}
	if calls[3].path != "/api/views.open" {
		t.Fatalf("fourth call path = %q, want views.open", calls[3].path)
	}
	if calls[3].auth != "Bearer user-messaging-token" {
		t.Fatalf("fourth call auth = %q, want user token", calls[3].auth)
	}
	if calls[3].body["audience"] != "user" {
		t.Fatalf("fourth upstream body audience = %#v, want user", calls[3].body["audience"])
	}
	if calls[4].path != "/api/views.open" {
		t.Fatalf("fifth call path = %q, want views.open", calls[4].path)
	}
	if calls[4].auth != "Bearer bot-messaging-token" {
		t.Fatalf("fifth call auth = %q, want bot token", calls[4].auth)
	}
	if calls[4].body["audience"] != "bot" {
		t.Fatalf("fifth upstream body audience = %#v, want bot", calls[4].body["audience"])
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
			StubIntegration: coretesting.StubIntegration{N: "sample-int", ConnMode: core.ConnectionModeSubject},
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
		ID:        "tok-team-a",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "sample-int:" + testCatalogConnection,
		Qualifier: "team-a",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "tok-team-a"},
	})

	ts = newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, sessionStub)
		cfg.MCPConnection = map[string]string{"sample-int": testCatalogConnection}
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
}

func TestExecuteOperation_DoesNotFallbackToDefaultWhenBrokerMCPConnectionConfigured(t *testing.T) {
	t.Parallel()

	var gotToken string
	sessionStub := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N:        "sample-int",
				ConnMode: core.ConnectionModeSubject,
				ExecuteFn: func(_ context.Context, op string, _ map[string]any, token string) (*core.OperationResult, error) {
					gotToken = token
					return &core.OperationResult{Status: http.StatusOK, Body: []byte(fmt.Sprintf(`{"operation":%q,"token":%q}`, op, token))}, nil
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
		ID:        "tok-rest",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "sample-int:rest-conn",
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "rest-token"},
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

	if resp.StatusCode != http.StatusPreconditionFailed {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("expected 412 when MCP session catalog credential is missing, got %d: %s", resp.StatusCode, body)
	}
	if gotToken != "" {
		_ = resp.Body.Close()
		t.Fatalf("execute token = %q, want no provider execution", gotToken)
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
				ConnMode: core.ConnectionModeSubject,
				ExecuteFn: func(_ context.Context, op string, _ map[string]any, token string) (*core.OperationResult, error) {
					gotToken = token
					return &core.OperationResult{Status: http.StatusOK, Body: []byte(fmt.Sprintf(`{"operation":%q,"token":%q}`, op, token))}, nil
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
		ID:        "tok-mcp",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "sample-int:mcp-conn",
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "mcp-token"},
	})
	seedToken(t, svc, &core.ExternalCredential{
		ID:        "tok-rest",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "sample-int:rest-conn",
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "rest-token"},
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
				ConnMode: core.ConnectionModeSubject,
				ExecuteFn: func(_ context.Context, op string, _ map[string]any, token string) (*core.OperationResult, error) {
					gotToken = token
					return &core.OperationResult{Status: http.StatusOK, Body: []byte(fmt.Sprintf(`{"operation":%q,"token":%q}`, op, token))}, nil
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
		ID:        "tok-catalog",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "sample-int:catalog-conn",
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "catalog-token"},
	})
	seedToken(t, svc, &core.ExternalCredential{
		ID:        "tok-rest",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "sample-int:rest-conn",
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "rest-token"},
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
		cfg.MCPConnection = map[string]string{"sample-int": "catalog-conn"}
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

func TestExecuteOperation_UsesServerMCPConnectionBeforeBrokerFallback(t *testing.T) {
	t.Parallel()

	var gotToken string
	sessionStub := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N:        "sample-int",
				ConnMode: core.ConnectionModeSubject,
				ExecuteFn: func(_ context.Context, op string, _ map[string]any, token string) (*core.OperationResult, error) {
					gotToken = token
					return &core.OperationResult{Status: http.StatusOK, Body: []byte(fmt.Sprintf(`{"operation":%q,"token":%q}`, op, token))}, nil
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
		ID:        "tok-catalog",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "sample-int:catalog-conn",
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "catalog-token"},
	})
	seedToken(t, svc, &core.ExternalCredential{
		ID:        "tok-rest",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "sample-int:rest-conn",
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "rest-token"},
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
		cfg.MCPConnection = map[string]string{"sample-int": "catalog-conn"}
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

func TestExecuteOperation_DoesNotFallbackPastConfiguredMCPConnection(t *testing.T) {
	t.Parallel()

	var gotToken string
	sessionStub := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N:        "sample-int",
				ConnMode: core.ConnectionModeSubject,
				ExecuteFn: func(_ context.Context, op string, _ map[string]any, token string) (*core.OperationResult, error) {
					gotToken = token
					return &core.OperationResult{Status: http.StatusOK, Body: []byte(fmt.Sprintf(`{"operation":%q,"token":%q}`, op, token))}, nil
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
		ID:        "tok-catalog",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "sample-int:catalog-conn",
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "catalog-token"},
	})
	seedToken(t, svc, &core.ExternalCredential{
		ID:        "tok-rest",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "sample-int:rest-conn",
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "rest-token"},
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
		cfg.MCPConnection = map[string]string{"sample-int": "catalog-conn"}
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
	secret := []byte("0123456789abcdef0123456789abcdef")
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = newHostIssuedSessionAuthStub(secret, hostIssuedSessionAuthOpts{name: "server"})
		cfg.SelectedAuthProvider = "server"
		cfg.AuditSink = auditSink
		cfg.MountedUIs = []server.MountedUI{{
			Name:    "sample_portal",
			Path:    "/sample-portal",
			AppName: "sample_portal",
			Handler: http.NotFoundHandler(),
		}}
		cfg.AppDefs = map[string]*config.ProviderEntry{
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
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			HandleCallbackFn: func(_ context.Context, code string) (*core.UserIdentity, error) {
				if code == "good-code" {
					return &core.UserIdentity{Email: "user@example.com", DisplayName: "User"}, nil
				}
				return nil, fmt.Errorf("bad code")
			},
			UserInfoFn: func(ctx context.Context, _ *core.UserInfoRequest) (*core.UserInfoResponse, error) {
				call := gestalt.AuthCallContextFromContext(ctx)
				if call.CallerBearerToken == "dev-token-user@example.com" {
					return &core.UserInfoResponse{
						SubjectID: "user:user@example.com",
						Email:     "user@example.com",
						Name:      "User",
					}, nil
				}
				return nil, core.ErrNotFound
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
	if result["status"] != "ok" {
		t.Fatalf("unexpected status: %v", result["status"])
	}
	var sessionCookie *http.Cookie
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "session_token" && cookie.Value != "" {
			sessionCookie = cookie
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected session_token cookie after login")
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
	if subjectID, ok := auditRecord["subject_id"].(string); !ok || subjectID != principal.UserSubjectID(existing.ID) {
		t.Fatalf("expected audit subject_id %q, got %v", principal.UserSubjectID(existing.ID), auditRecord["subject_id"])
	}
	if _, ok := auditRecord["user_id"]; ok {
		t.Fatalf("expected emitted audit record to omit user_id, got %v", auditRecord["user_id"])
	}

	sessionResp, err := client.Get(ts.URL + "/api/v1/auth/session")
	if err != nil {
		t.Fatalf("GET /api/v1/auth/session: %v", err)
	}
	defer func() { _ = sessionResp.Body.Close() }()
	if sessionResp.StatusCode != http.StatusOK {
		t.Fatalf("expected session 200, got %d", sessionResp.StatusCode)
	}
	var sessionBody map[string]any
	if err := json.NewDecoder(sessionResp.Body).Decode(&sessionBody); err != nil {
		t.Fatalf("decoding session: %v", err)
	}
	subjectID, _ := sessionBody["subjectId"].(string)
	if strings.TrimSpace(subjectID) == "" {
		t.Fatalf("expected non-empty subjectId, got %v", sessionBody["subjectId"])
	}
	if sessionBody["email"] != "user@example.com" {
		t.Fatalf("expected email user@example.com, got %v", sessionBody["email"])
	}
	if sessionBody["displayName"] != "User" {
		t.Fatalf("expected displayName User, got %v", sessionBody["displayName"])
	}
	if _, ok := sessionBody["credentialSubjectId"]; ok {
		t.Fatalf("expected session response to omit credentialSubjectId, got %v", sessionBody["credentialSubjectId"])
	}
	if _, ok := sessionBody["kind"]; ok {
		t.Fatalf("expected session response to omit kind, got %v", sessionBody["kind"])
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
		cfg.Auth = newHostIssuedSessionAuthStub(secret, hostIssuedSessionAuthOpts{name: "server"})
		cfg.SelectedAuthProvider = "server"
		cfg.StateSecret = secret
		cfg.AuthProviders = map[string]core.AuthenticationProvider{
			"alt": newHostIssuedSessionAuthStub(secret, hostIssuedSessionAuthOpts{name: "alt"}),
		}
		cfg.MountedUIs = []server.MountedUI{{
			Name:    "sample_portal",
			Path:    "/sample-portal",
			AppName: "sample_portal",
			Handler: http.NotFoundHandler(),
		}}
		cfg.AppDefs = map[string]*config.ProviderEntry{
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
		cfg.Auth = newHostIssuedSessionAuthStub(secret, hostIssuedSessionAuthOpts{name: "server"})
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

	for _, cookie := range resp.Cookies() {
		if cookie.Name == "session_token" {
			t.Fatalf("did not expect session cookie for CLI login, got %q", cookie.Value)
		}
	}

	lines := bytes.Split(bytes.TrimSpace(auditBuf.Bytes()), []byte("\n"))
	if len(lines) == 0 {
		t.Fatalf("expected CLI login callback to emit audit records, got %d", len(lines))
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
	_ = seedUser(t, svc, "host@example.com")
	auth := newHostIssuedSessionAuthStub([]byte("host-issued-secret"), hostIssuedSessionAuthOpts{})
	baseTokenFn := auth.TokenFn
	var gotAuthorizationCodeState string
	auth.TokenFn = func(ctx context.Context, req *core.TokenRequest) (*core.TokenResponse, error) {
		if req != nil && req.GrantType == core.GrantTypeAuthorizationCode {
			gotAuthorizationCodeState = req.State
			if req.State != "cli:54305:test-state" {
				return nil, fmt.Errorf("authorization code state = %q, want prefixed CLI state", req.State)
			}
		}
		return baseTokenFn(ctx, req)
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = auth
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
	if gotAuthorizationCodeState != "cli:54305:test-state" {
		t.Fatalf("authorization code state = %q, want prefixed CLI state", gotAuthorizationCodeState)
	}
}

func TestLoginCallbackStateMismatch(t *testing.T) {
	t.Parallel()

	var tokenCalled bool
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			TokenFn: func(context.Context, *core.TokenRequest) (*core.TokenResponse, error) {
				tokenCalled = true
				return nil, fmt.Errorf("Token should not be called on state mismatch")
			},
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
	if tokenCalled {
		t.Fatal("expected provider Token not to be called on state mismatch")
	}
}

func TestLoginCallbackMissingStateCookie(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
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
		cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
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
				return &core.UserIdentity{Email: "pkce@example.com"}, "encrypted-state", nil
			}
			return nil, "", fmt.Errorf("bad code or state")
		},
	}
	stub.TokenFn = func(ctx context.Context, req *core.TokenRequest) (*core.TokenResponse, error) {
		if req == nil || req.Code != "good-code" {
			return nil, fmt.Errorf("invalid code")
		}
		identity, _, err := stub.handleWithState(ctx, req.Code, "encrypted-state")
		if err != nil {
			return nil, err
		}
		token := "dev-token"
		if identity != nil && identity.Email != "" {
			token = "dev-token-" + identity.Email
		}
		return &core.TokenResponse{
			AccessToken: token,
			TokenType:   "Bearer",
			ExpiresIn:   3600,
			GrantID:     "grant-encrypted-state",
		}, nil
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = stub
	})
	testutil.CloseOnCleanup(t, ts)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	body := bytes.NewBufferString(`{"state":"encrypted-state"}`)
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
	if result["status"] != "ok" {
		t.Fatalf("unexpected status: %v", result["status"])
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
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"slack": {
				Connections: map[string]*config.ConnectionDef{
					testDefaultConnection: oauthConnectionDef(nil),
				},
			},
		}
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
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"slack": {
				Connections: map[string]*config.ConnectionDef{
					testDefaultConnection: oauthConnectionDef(nil),
				},
			},
		}
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

func TestStartIntegrationOAuth_ServiceAccountIDStoresCredentialForServiceAccount(t *testing.T) {
	t.Parallel()

	const serviceAccountSubjectID = "service_account:oauth-bot"

	svc := testutil.NewStubServices(t)
	authz := &serviceAccountCredentialAuthorizationProvider{allowed: true}

	handler := &testOAuthHandler{
		authorizationBaseURLVal: "https://auth.example.com/oauth/authorize",
		exchangeCodeFn: func(_ context.Context, code string) (*core.OAuthTokenResponse, error) {
			if code != "good-code" {
				return nil, fmt.Errorf("bad code")
			}
			return &core.OAuthTokenResponse{AccessToken: "service-account-oauth-token"}, nil
		},
	}
	stub := &stubIntegrationWithAuthURL{
		StubIntegration: coretesting.StubIntegration{N: "oauth-service-account"},
		authURL:         "https://auth.example.com/oauth/authorize",
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"oauth-service-account": testDefaultConnection}
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"oauth-service-account": {
				Connections: map[string]*config.ConnectionDef{
					testDefaultConnection: oauthConnectionDef(nil),
				},
			},
		}
		cfg.ConnectionAuth = testConnectionAuth("oauth-service-account", handler)
		cfg.Authorization = authz
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	startReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/start-oauth", bytes.NewBufferString(`{"integration":"oauth-service-account","serviceAccountId":"oauth-bot"}`))
	startReq.Header.Set("Content-Type", "application/json")
	startResp, err := http.DefaultClient.Do(startReq)
	if err != nil {
		t.Fatalf("start request: %v", err)
	}
	defer func() { _ = startResp.Body.Close() }()
	if startResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(startResp.Body)
		t.Fatalf("start status = %d, want 200: %s", startResp.StatusCode, body)
	}
	var startResult map[string]string
	if err := json.NewDecoder(startResp.Body).Decode(&startResult); err != nil {
		t.Fatalf("decode start response: %v", err)
	}

	noRedirect := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	callbackReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/auth/callback?code=good-code&state="+url.QueryEscape(startResult["state"]), nil)
	callbackResp, err := noRedirect.Do(callbackReq)
	if err != nil {
		t.Fatalf("callback request: %v", err)
	}
	defer func() { _ = callbackResp.Body.Close() }()
	if callbackResp.StatusCode != http.StatusSeeOther {
		body, _ := io.ReadAll(callbackResp.Body)
		t.Fatalf("callback status = %d, want 303: %s", callbackResp.StatusCode, body)
	}

	tokens, err := svc.ExternalCredentials.ListCredentials(context.Background(), serviceAccountSubjectID, "")
	if err != nil {
		t.Fatalf("ListCredentials(service account): %v", err)
	}
	if len(tokens) != 1 {
		t.Fatalf("service account tokens len = %d, want 1", len(tokens))
	}
	if tokens[0].Grant == nil || tokens[0].Grant.AccessToken != "service-account-oauth-token" {
		t.Fatalf("stored grant = %+v, want access token service-account-oauth-token", tokens[0].Grant)
	}
	if len(authz.requests) != 1 {
		t.Fatalf("authorization requests = %d, want 1", len(authz.requests))
	}
	authReq := authz.requests[0]
	if authReq.GetSubject().GetType() != "subject" || !strings.HasPrefix(authReq.GetSubject().GetId(), "user:") {
		t.Fatalf("authorization subject = %+v, want user subject", authReq.GetSubject())
	}
	if got := authReq.GetAction().GetName(); got != "manages" {
		t.Fatalf("authorization action = %q, want manages", got)
	}
	if resource := authReq.GetResource(); resource.GetType() != "service_account" || resource.GetId() != serviceAccountSubjectID {
		t.Fatalf("authorization resource = %+v, want service_account/%s", resource, serviceAccountSubjectID)
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

		handler := &testOAuthHandler{
			authorizationBaseURLVal: "https://auth.example.com/oauth/authorize",
			exchangeCodeFn: func(_ context.Context, code string) (*core.OAuthTokenResponse, error) {
				if code == "good-code" {
					return &core.OAuthTokenResponse{
						AccessToken: "oauth-token",
						Extra: map[string]any{
							"tenant":  map[string]any{"id": "tenant-123"},
							"account": map[string]any{"id": "account-456"},
						},
					}, nil
				}
				return nil, fmt.Errorf("bad code")
			},
		}

		stub := &stubIntegrationWithAuthURL{
			StubIntegration: coretesting.StubIntegration{N: "oauth-svc"},
			authURL:         "https://auth.example.com/oauth/authorize",
			connectionParams: map[string]core.ConnectionParamDef{
				"tenant_id": {
					Required: true,
					From:     "token_response",
					Field:    "tenant.id",
				},
				"account_id": {
					Required: true,
					From:     "token_response",
					Field:    "account.id",
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
			cfg.DefaultConnection = map[string]string{"oauth-svc": testDefaultConnection}
			cfg.AppDefs = map[string]*config.ProviderEntry{
				"oauth-svc": {
					Connections: map[string]*config.ConnectionDef{
						testDefaultConnection: oauthConnectionDef(map[string]config.ConnectionParamDef{
							"tenant_id":  {Required: true, From: "token_response", Field: "tenant.id"},
							"account_id": {Required: true, From: "token_response", Field: "account.id"},
						}),
					},
				},
			}
			cfg.ConnectionAuth = testConnectionAuth("oauth-svc", handler)
			cfg.Services = svc
			cfg.AuditSink = invocation.NewSlogAuditSink(&auditBuf)
		})
		testutil.CloseOnCleanup(t, ts)

		startBody := bytes.NewBufferString(`{"integration":"oauth-svc"}`)
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
		if loc != "/apps?connected=oauth-svc" {
			t.Fatalf("expected redirect to /apps?connected=oauth-svc, got %q", loc)
		}
		u, _ := svc.Users.FindOrCreateUser(context.Background(), "user@example.com")
		tokens, _ := svc.ExternalCredentials.ListCredentials(context.Background(), principal.UserSubjectID(u.ID), "")
		if len(tokens) == 0 {
			t.Fatal("expected token to be stored")
		}
		stored := tokens[0]
		if !strings.HasPrefix(stored.Audience, "oauth-svc:") {
			t.Fatalf("stored token audience = %q, want %q prefix", stored.Audience, "oauth-svc:")
		}
		if stored.Grant == nil || stored.Grant.AccessToken != "oauth-token" {
			t.Fatalf("stored grant = %+v, want access token %q", stored.Grant, "oauth-token")
		}
		if recordingCreds.upsertCredentialCalls.Load() == 0 {
			t.Fatal("expected oauth callback to store credentials through ExternalCredentialProvider")
		}
		var metadata map[string]string
		if err := json.Unmarshal([]byte(stored.MetadataJSON), &metadata); err != nil {
			t.Fatalf("unmarshal metadata: %v", err)
		}
		if !reflect.DeepEqual(metadata, map[string]string{
			"tenant_id":  "tenant-123",
			"account_id": "account-456",
		}) {
			t.Fatalf("stored metadata = %+v", metadata)
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
		if subjectID, ok := auditRecord["subject_id"].(string); !ok || subjectID != principal.UserSubjectID(u.ID) {
			t.Fatalf("expected audit subject_id %q, got %v", principal.UserSubjectID(u.ID), auditRecord["subject_id"])
		}
		if _, ok := auditRecord["user_id"]; ok {
			t.Fatalf("expected emitted audit record to omit user_id, got %v", auditRecord["user_id"])
		}
		if auditRecord["target_kind"] != "connection" {
			t.Fatalf("expected audit target_kind connection, got %v", auditRecord["target_kind"])
		}
		if auditRecord["target_id"] != "oauth-svc/default/default" {
			t.Fatalf("expected audit target_id oauth-svc/default/default, got %v", auditRecord["target_id"])
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
		handler := &testOAuthHandler{
			authorizationBaseURLVal: "https://auth.example.com/oauth/authorize",
			exchangeCodeFn: func(_ context.Context, code string) (*core.OAuthTokenResponse, error) {
				if code == "good-code" {
					return &core.OAuthTokenResponse{
						AccessToken: "oauth-token",
						Extra: map[string]any{
							"tenant":  map[string]any{"id": "tenant-123"},
							"account": map[string]any{"id": "account-456"},
						},
					}, nil
				}
				return nil, fmt.Errorf("bad code")
			},
		}

		stub := &stubDiscoveringProvider{
			StubIntegration: coretesting.StubIntegration{N: "oauth-svc"},
			discovery: &core.DiscoveryConfig{
				URL:      discoverySrv.URL,
				IDPath:   "id",
				NamePath: "name",
				Metadata: map[string]string{"workspace": "workspace"},
			},
			connectionParams: map[string]core.ConnectionParamDef{
				"tenant_id": {
					Required: true,
					From:     "token_response",
					Field:    "tenant.id",
				},
				"account_id": {
					Required: true,
					From:     "token_response",
					Field:    "account.id",
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
			cfg.DefaultConnection = map[string]string{"oauth-svc": testDefaultConnection}
			cfg.AppDefs = map[string]*config.ProviderEntry{
				"oauth-svc": {
					Connections: map[string]*config.ConnectionDef{
						testDefaultConnection: oauthConnectionDef(map[string]config.ConnectionParamDef{
							"tenant_id":  {Required: true, From: "token_response", Field: "tenant.id"},
							"account_id": {Required: true, From: "token_response", Field: "account.id"},
						}),
					},
				},
			}
			cfg.ConnectionAuth = testConnectionAuth("oauth-svc", handler)
			cfg.Services = svc
		})
		testutil.CloseOnCleanup(t, ts)

		startBody := bytes.NewBufferString(`{"integration":"oauth-svc"}`)
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
		if !strings.Contains(text, "Select a oauth-svc connection") {
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
		tokens, _ := svc.ExternalCredentials.ListCredentials(context.Background(), principal.UserSubjectID(u.ID), "")
		if len(tokens) == 0 {
			t.Fatal("expected token to be stored after selection")
		}
		stored := tokens[0]
		if !strings.HasPrefix(stored.Audience, "oauth-svc:") {
			t.Fatalf("stored token audience = %q, want %q prefix", stored.Audience, "oauth-svc:")
		}
		var metadata map[string]string
		if err := json.Unmarshal([]byte(stored.MetadataJSON), &metadata); err != nil {
			t.Fatalf("unmarshal metadata: %v", err)
		}
		if !reflect.DeepEqual(metadata, map[string]string{
			"tenant_id":  "tenant-123",
			"account_id": "account-456",
			"workspace":  "beta",
		}) {
			t.Fatalf("stored metadata = %+v", metadata)
		}
	})
}

func TestIntegrationOAuthCallback_InvalidState(t *testing.T) {
	t.Parallel()

	stub := &coretesting.StubIntegration{N: "oauth-svc"}

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
		if !strings.Contains(html, `href="/apps"`) {
			t.Fatalf("expected HTML response to link back to integrations, got %q", html)
		}
	})
}

func TestCreateAndListAPITokens(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		configureGrantTestAuth(cfg)
		cfg.Providers = grantTestProviders(t)
		cfg.Services = testutil.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"name":"my-token","scopes":"testapp"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/tokens", body)
	req.Header.Set("Content-Type", "application/json")
	addGrantTestSessionCookie(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var result struct {
		ID     string   `json:"id"`
		Token  string   `json:"token"`
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if result.Token == "" {
		t.Fatal("expected non-empty token in response")
	}
	if result.ID == "" {
		t.Fatal("expected non-empty grant id in response")
	}
	if result.Name != result.ID {
		t.Fatalf("expected name to equal grant id %q, got %q", result.ID, result.Name)
	}
	if len(result.Scopes) != 1 || result.Scopes[0] != "testapp" {
		t.Fatalf("expected scopes [testapp], got %v", result.Scopes)
	}

	listReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/tokens", nil)
	addGrantTestSessionCookie(listReq)
	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatalf("list request: %v", err)
	}
	defer func() { _ = listResp.Body.Close() }()
	if listResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(listResp.Body)
		t.Fatalf("list status = %d, want 200: %s", listResp.StatusCode, respBody)
	}

	var listed []struct {
		ID        string   `json:"id"`
		Name      string   `json:"name"`
		Scopes    []string `json:"scopes"`
		CreatedAt string   `json:"createdAt"`
		ExpiresAt string   `json:"expiresAt"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected 1 listed token, got %d", len(listed))
	}
	if listed[0].ID != result.ID {
		t.Fatalf("listed id = %q, want %q", listed[0].ID, result.ID)
	}
	if listed[0].Name != result.ID {
		t.Fatalf("listed name = %q, want grant id %q", listed[0].Name, result.ID)
	}
	if len(listed[0].Scopes) != 1 || listed[0].Scopes[0] != "testapp" {
		t.Fatalf("listed scopes = %v, want [testapp]", listed[0].Scopes)
	}
	if _, err := time.Parse(time.RFC3339, listed[0].CreatedAt); err != nil {
		t.Fatalf("listed createdAt = %q, want RFC3339: %v", listed[0].CreatedAt, err)
	}
	if _, err := time.Parse(time.RFC3339, listed[0].ExpiresAt); err != nil {
		t.Fatalf("listed expiresAt = %q, want RFC3339: %v", listed[0].ExpiresAt, err)
	}
}

func TestCreateAPITokenAllowsEmptyScopes(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		configureGrantTestAuth(cfg)
		cfg.Services = testutil.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"name":"empty-scope-token"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/tokens", body)
	req.Header.Set("Content-Type", "application/json")
	addGrantTestSessionCookie(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read create response: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %s", resp.StatusCode, respBody)
	}
	var result struct {
		ID     string   `json:"id"`
		Token  string   `json:"token"`
		Scopes []string `json:"scopes"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if result.Token == "" {
		t.Fatalf("expected token in response, got empty: %s", respBody)
	}
	if result.ID == "" {
		t.Fatalf("expected grant id in response, got empty: %s", respBody)
	}
	if len(result.Scopes) != 0 {
		t.Fatalf("expected empty scopes for full-identity token, got %v", result.Scopes)
	}
}

func TestCreateAPITokenRejectsMissingGrantID(t *testing.T) {
	t.Parallel()

	stub := newGrantTrackingAuthStub()
	stub.TokenFn = func(_ context.Context, req *core.TokenRequest) (*core.TokenResponse, error) {
		if req != nil {
			copied := *req
			stub.lastTokenExchangeReq = &copied
		}
		return &core.TokenResponse{
			AccessToken: "grant-access-no-id",
			TokenType:   "Bearer",
			ExpiresIn:   3600,
		}, nil
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = stub
		cfg.Providers = grantTestProviders(t)
		cfg.Services = testutil.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"name":"missing-grant-id","scopes":"testapp"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/tokens", body)
	req.Header.Set("Content-Type", "application/json")
	addGrantTestSessionCookie(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read create response: %v", err)
	}
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("create status = %d, want 500: %s", resp.StatusCode, respBody)
	}
}

func TestCreateAPITokenDoesNotUseStateForName(t *testing.T) {
	t.Parallel()

	stub := newGrantTrackingAuthStub()
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = stub
		cfg.Providers = grantTestProviders(t)
		cfg.Services = testutil.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"name":"named-token","scopes":"testapp"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/tokens", body)
	req.Header.Set("Content-Type", "application/json")
	addGrantTestSessionCookie(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("create status = %d, want 201: %s", resp.StatusCode, respBody)
	}

	if stub.lastTokenExchangeReq == nil {
		t.Fatal("expected token exchange request to be recorded")
	}
	if stub.lastTokenExchangeReq.State != "" {
		t.Fatalf("token exchange state = %q, want empty", stub.lastTokenExchangeReq.State)
	}
}

func TestListAPITokensSkipsNotFoundGrant(t *testing.T) {
	t.Parallel()

	stub := newGrantTrackingAuthStub()
	now := time.Now().UTC()
	stub.grants["grant-live"] = &core.GetGrantResponse{
		CreatedAt: now.Unix(),
		ExpiresAt: now.Add(time.Hour).Unix(),
		Scopes:    []core.GrantScope{{Scope: "testapp"}},
	}
	stub.ListGrantsFn = func(context.Context, *core.ListGrantsRequest) (*core.ListGrantsResponse, error) {
		return &core.ListGrantsResponse{GrantIDs: []string{"grant-live", "grant-stale"}}, nil
	}
	stub.GetGrantFn = func(_ context.Context, req *core.GetGrantRequest) (*core.GetGrantResponse, error) {
		if req == nil {
			return nil, fmt.Errorf("missing grant request")
		}
		switch req.GrantID {
		case "grant-live":
			return stub.grants["grant-live"], nil
		case "grant-stale":
			return nil, core.ErrNotFound
		default:
			return nil, core.ErrNotFound
		}
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = stub
		cfg.Services = testutil.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/tokens", nil)
	addGrantTestSessionCookie(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("list status = %d, want 200: %s", resp.StatusCode, respBody)
	}

	var tokens []struct {
		ID        string   `json:"id"`
		Name      string   `json:"name"`
		Scopes    []string `json:"scopes"`
		CreatedAt string   `json:"createdAt"`
		ExpiresAt string   `json:"expiresAt"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(tokens) != 1 || tokens[0].ID != "grant-live" {
		t.Fatalf("tokens = %+v, want only grant-live", tokens)
	}
	if tokens[0].Name != "grant-live" {
		t.Fatalf("listed token name = %q, want grant id", tokens[0].Name)
	}
	if len(tokens[0].Scopes) != 1 || tokens[0].Scopes[0] != "testapp" {
		t.Fatalf("listed scopes = %v, want [testapp]", tokens[0].Scopes)
	}
	expectedCreatedAt := time.Unix(now.Unix(), 0).UTC().Format(time.RFC3339)
	if tokens[0].CreatedAt != expectedCreatedAt {
		t.Fatalf("listed createdAt = %q, want %q", tokens[0].CreatedAt, expectedCreatedAt)
	}
	expectedExpiresAt := time.Unix(now.Add(time.Hour).Unix(), 0).UTC().Format(time.RFC3339)
	if tokens[0].ExpiresAt != expectedExpiresAt {
		t.Fatalf("listed expiresAt = %q, want %q", tokens[0].ExpiresAt, expectedExpiresAt)
	}
}

func TestListAPITokensGetGrantFailureReturns500(t *testing.T) {
	t.Parallel()

	stub := newGrantTrackingAuthStub()
	stub.ListGrantsFn = func(context.Context, *core.ListGrantsRequest) (*core.ListGrantsResponse, error) {
		return &core.ListGrantsResponse{GrantIDs: []string{"grant-live"}}, nil
	}
	stub.GetGrantFn = func(context.Context, *core.GetGrantRequest) (*core.GetGrantResponse, error) {
		return nil, fmt.Errorf("storage unavailable")
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = stub
		cfg.Services = testutil.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/tokens", nil)
	addGrantTestSessionCookie(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("list status = %d, want 500: %s", resp.StatusCode, respBody)
	}
}

func TestCreateAPITokenRejectsUnknownPermissionFields(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		configureGrantTestAuth(cfg)
		cfg.Services = testutil.NewStubServices(t)
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "roadmap",
			ConnMode: core.ConnectionModeNone,
		})
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"name":"action-token","permissions":[{"app":"roadmap","actions":["legacy.action"]}]}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/tokens", body)
	req.Header.Set("Content-Type", "application/json")
	addGrantTestSessionCookie(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read create response: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("create status = %d, want 400: %s", resp.StatusCode, respBody)
	}
	if !strings.Contains(string(respBody), "invalid JSON body") {
		t.Fatalf("create response = %s, want invalid JSON body error", respBody)
	}
}

func TestListAPITokensListsOwnedUserRecords(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)

	ts := newTestServer(t, func(cfg *server.Config) {
		configureGrantTestAuth(cfg)
		cfg.Providers = grantTestProviders(t)
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"name":"owned-token","scopes":"testapp"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/tokens", body)
	req.Header.Set("Content-Type", "application/json")
	addGrantTestSessionCookie(req)
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
	addGrantTestSessionCookie(req)
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
		ID        string   `json:"id"`
		Name      string   `json:"name"`
		Scopes    []string `json:"scopes"`
		CreatedAt string   `json:"createdAt"`
		ExpiresAt string   `json:"expiresAt"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(tokens) != 1 || tokens[0].ID != createResp.ID {
		t.Fatalf("tokens = %+v, want only %q", tokens, createResp.ID)
	}
	if len(tokens[0].Scopes) != 1 || tokens[0].Scopes[0] != "testapp" {
		t.Fatalf("listed scopes = %v, want [testapp]", tokens[0].Scopes)
	}
	if _, err := time.Parse(time.RFC3339, tokens[0].CreatedAt); err != nil {
		t.Fatalf("listed createdAt = %q, want RFC3339: %v", tokens[0].CreatedAt, err)
	}
	if _, err := time.Parse(time.RFC3339, tokens[0].ExpiresAt); err != nil {
		t.Fatalf("listed expiresAt = %q, want RFC3339: %v", tokens[0].ExpiresAt, err)
	}
}

func TestRevokeAPIToken(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	var auditBuf bytes.Buffer
	now := time.Now().UTC()
	ts := newTestServer(t, func(cfg *server.Config) {
		stub := configureGrantTestAuthForUser(cfg, u.ID)
		stub.grants["tok-123"] = &core.GetGrantResponse{
			CreatedAt: now.Unix(),
			ExpiresAt: now.Add(24 * time.Hour).Unix(),
		}
		cfg.AuditSink = invocation.NewSlogAuditSink(&auditBuf)
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/tokens/tok-123", nil)
	addGrantTestSessionCookie(req)
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
		configureGrantTestAuthForUser(cfg, existing.ID)
		cfg.Providers = grantTestProviders(t)
		cfg.Now = func() time.Time { return fixedNow }
		cfg.AuditSink = auditSink
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"name":"expiry-test","scopes":"testapp"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/tokens", body)
	req.Header.Set("Content-Type", "application/json")
	addGrantTestSessionCookie(req)
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

	listReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/tokens", nil)
	addGrantTestSessionCookie(listReq)
	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatalf("list grants: %v", err)
	}
	defer func() { _ = listResp.Body.Close() }()
	if listResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(listResp.Body)
		t.Fatalf("list status = %d, want 200: %s", listResp.StatusCode, respBody)
	}
	var grants []struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&grants); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(grants) != 1 {
		t.Fatalf("expected 1 grant for canonical user, got %d", len(grants))
	}
	if grants[0].ID != tokenID {
		t.Fatalf("expected stored grant ID %q, got %q", tokenID, grants[0].ID)
	}

	var auditRecord map[string]any
	auditLines := bytes.Split(bytes.TrimSpace(auditBuf.Bytes()), []byte("\n"))
	if len(auditLines) == 0 {
		t.Fatal("expected grant.create audit record")
	}
	if err := json.Unmarshal(auditLines[0], &auditRecord); err != nil {
		t.Fatalf("parsing audit record: %v\nraw: %s", err, auditBuf.String())
	}
	if auditRecord["operation"] != "grant.create" {
		t.Fatalf("expected audit operation api_token.create, got %v", auditRecord["operation"])
	}
	if auditRecord["source"] != "http" {
		t.Fatalf("expected audit source http, got %v", auditRecord["source"])
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
		cfg.Auth = authStubWithSessionEmail("user@example.com")
		cfg.Providers = grantTestProviders(t)
		cfg.AuditSink = auditSink
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	stubDB.Err = fmt.Errorf("database unavailable")

	body := bytes.NewBufferString(`{"name":"failure-test","scopes":"testapp"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/tokens", body)
	req.Header.Set("Content-Type", "application/json")
	addGrantTestSessionCookie(req)
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
	if auditRecord["operation"] != "grant.create" {
		t.Fatalf("expected audit operation grant.create, got %v", auditRecord["operation"])
	}
	if auditRecord["allowed"] != false {
		t.Fatalf("expected audit allowed=false, got %v", auditRecord["allowed"])
	}
	if auditRecord["error"] != "failed to resolve user" {
		t.Fatalf("expected audit error failed to resolve user, got %v", auditRecord["error"])
	}
}

func TestRevokeAllAPITokens(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	var auditBuf bytes.Buffer
	now := time.Now().UTC()
	ts := newTestServer(t, func(cfg *server.Config) {
		stub := configureGrantTestAuthForUser(cfg, u.ID)
		for _, name := range []string{"tok-a", "tok-b", "tok-c"} {
			stub.grants[name] = &core.GetGrantResponse{
				CreatedAt: now.Unix(),
				ExpiresAt: now.Add(24 * time.Hour).Unix(),
			}
		}
		cfg.AuditSink = invocation.NewSlogAuditSink(&auditBuf)
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/tokens", nil)
	addGrantTestSessionCookie(req)
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

	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	ts := newTestServer(t, func(cfg *server.Config) {
		configureGrantTestAuthForUser(cfg, u.ID)
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/tokens", nil)
	addGrantTestSessionCookie(req)
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

func TestRevokeAllAPITokens_ListGrantsNil(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	ts := newTestServer(t, func(cfg *server.Config) {
		stub := configureGrantTestAuthForUser(cfg, u.ID)
		stub.ListGrantsFn = func(context.Context, *core.ListGrantsRequest) (*core.ListGrantsResponse, error) {
			return nil, nil
		}
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/tokens", nil)
	addGrantTestSessionCookie(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestRevokeAllAPITokens_PartialFailure(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	now := time.Now().UTC()
	ts := newTestServer(t, func(cfg *server.Config) {
		stub := configureGrantTestAuthForUser(cfg, u.ID)
		for _, name := range []string{"tok-a", "tok-b"} {
			stub.grants[name] = &core.GetGrantResponse{
				CreatedAt: now.Unix(),
				ExpiresAt: now.Add(24 * time.Hour).Unix(),
			}
		}
		stub.RevokeGrantFn = func(_ context.Context, req *core.RevokeGrantRequest) (*core.RevokeGrantResponse, error) {
			if req == nil || req.GrantID == "" {
				return nil, core.ErrNotFound
			}
			if req.GrantID == "tok-b" {
				return nil, fmt.Errorf("failed to revoke grant")
			}
			stub.mu.Lock()
			defer stub.mu.Unlock()
			if _, ok := stub.grants[req.GrantID]; !ok {
				return nil, core.ErrNotFound
			}
			stub.revoked[req.GrantID] = struct{}{}
			return &core.RevokeGrantResponse{}, nil
		}
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/tokens", nil)
	addGrantTestSessionCookie(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if result["status"] != "partial" {
		t.Fatalf("expected status partial, got %q", result["status"])
	}
	if count, ok := result["count"].(float64); !ok || count != 1 {
		t.Fatalf("expected count 1, got %v", result["count"])
	}
	failed, ok := result["failed"].([]any)
	if !ok || len(failed) != 1 {
		t.Fatalf("expected one failed entry, got %v", result["failed"])
	}
	entry, ok := failed[0].(map[string]any)
	if !ok {
		t.Fatalf("expected failed entry object, got %T", failed[0])
	}
	if entry["id"] != "tok-b" {
		t.Fatalf("expected failed id tok-b, got %v", entry["id"])
	}
}

func TestRevokeAllAPITokens_ExcludesSessionGrants(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	now := time.Now().UTC()
	var stub *grantTrackingAuthStub
	ts := newTestServer(t, func(cfg *server.Config) {
		stub = configureGrantTestAuthForUser(cfg, u.ID)
		stub.grants["grant-stub"] = &core.GetGrantResponse{
			CreatedAt: now.Unix(),
			ExpiresAt: now.Add(24 * time.Hour).Unix(),
		}
		stub.grants["tok-a"] = &core.GetGrantResponse{
			CreatedAt: now.Unix(),
			ExpiresAt: now.Add(24 * time.Hour).Unix(),
		}
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/tokens", nil)
	addGrantTestSessionCookie(req)
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
	if count, ok := result["count"].(float64); !ok || count != 1 {
		t.Fatalf("expected count 1, got %v", result["count"])
	}
	stub.mu.Lock()
	_, sessionRevoked := stub.revoked["grant-stub"]
	_, apiRevoked := stub.revoked["tok-a"]
	stub.mu.Unlock()
	if sessionRevoked {
		t.Fatal("expected session grant-stub to remain active")
	}
	if !apiRevoked {
		t.Fatal("expected API token tok-a to be revoked")
	}
}

func TestExecuteOperation_POST(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.ExternalCredential{
		ID:        "tok1",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "test-int:" + config.AppConnectionName,
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "test-token"},
	})

	fullStub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N: "test-int",
			ExecuteFn: func(_ context.Context, op string, params map[string]any, _ string) (*core.OperationResult, error) {
				text, _ := params["text"].(string)
				return &core.OperationResult{
					Status: http.StatusOK,
					Body:   []byte(fmt.Sprintf(`{"text":%q}`, text)),
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

func TestVisibleFalseGenericRouteSkipsSessionCatalogCredentialResolution(t *testing.T) {
	t.Parallel()

	plaintext := scopedTestBearerToken("viewer-user", "")
	svc := testutil.NewStubServices(t)
	seedUser(t, svc, "viewer-user@test.local")

	hidden := false
	var sessionCatalogCalls atomic.Int64
	provider := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N:        "events",
				ConnMode: core.ConnectionModeSubject,
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
		cfg.Auth = testAuthStubForScopedBearer()
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
		AppDefs: map[string]*config.ProviderEntry{
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

func TestHostedHTTPBinding_AddsRequestHeadersToWorkflowContext(t *testing.T) {
	t.Parallel()

	const providerName = "webhook-context"
	workflowSeen := make(chan map[string]any, 1)
	provider := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        providerName,
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(ctx context.Context, operation string, _ map[string]any, _ string) (*core.OperationResult, error) {
				if operation == "receive_event" {
					workflowSeen <- invocation.WorkflowContextFromContext(ctx)
					return &core.OperationResult{Status: http.StatusOK, Body: []byte(`{"ok":true}`)}, nil
				}
				return &core.OperationResult{Status: http.StatusNotFound, Body: []byte(`{}`)}, nil
			},
		},
		ops: []core.Operation{{Name: "receive_event", Method: http.MethodPost}},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, provider)
		cfg.AppDefs = map[string]*config.ProviderEntry{
			providerName: {
				SecuritySchemes: map[string]*config.HTTPSecurityScheme{
					"public": {Type: providermanifestv1.HTTPSecuritySchemeTypeNone},
				},
				HTTP: map[string]*config.HTTPBinding{
					"delivery": {
						Path:     "/delivery",
						Method:   http.MethodPost,
						Security: "public",
						Target:   "receive_event",
					},
				},
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/"+providerName+"/delivery", strings.NewReader(`{"event":"opened"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Slack-Request-Timestamp", "123")
	req.Header.Set("X-Slack-Signature", "v0=abc")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http binding request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("http binding status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var workflow map[string]any
	select {
	case workflow = <-workflowSeen:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for http binding invocation")
	}
	httpContext := invocation.WorkflowContextMap(workflow, "http")
	if httpContext == nil {
		t.Fatal("workflow http context is missing")
	}
	headers, ok := httpContext["headers"].(map[string]any)
	if !ok {
		t.Fatalf("workflow http headers = %#v, want map", httpContext["headers"])
	}
	signatures, ok := headers["X-Slack-Signature"].([]string)
	if !ok || len(signatures) != 1 || signatures[0] != "v0=abc" {
		t.Fatalf("X-Slack-Signature header = %#v, want [v0=abc]", headers["X-Slack-Signature"])
	}
	timestamps, ok := headers["X-Slack-Request-Timestamp"].([]string)
	if !ok || len(timestamps) != 1 || timestamps[0] != "123" {
		t.Fatalf("X-Slack-Request-Timestamp header = %#v, want [123]", headers["X-Slack-Request-Timestamp"])
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
			cfg.AppDefs = map[string]*config.ProviderEntry{"events": tt.entry}

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
		cfg.SelectedAuthProvider = "google"
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
		cfg.SelectedAuthProvider = "custom"
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

	sessionResp, err := http.Get(ts.URL + "/api/v1/auth/session")
	if err != nil {
		t.Fatalf("GET /api/v1/auth/session: %v", err)
	}
	defer func() { _ = sessionResp.Body.Close() }()
	if sessionResp.StatusCode != http.StatusOK {
		t.Fatalf("expected session 200, got %d", sessionResp.StatusCode)
	}
	var sessionBody map[string]any
	if err := json.NewDecoder(sessionResp.Body).Decode(&sessionBody); err != nil {
		t.Fatalf("decoding session: %v", err)
	}
	subjectID, _ := sessionBody["subjectId"].(string)
	email, _ := sessionBody["email"].(string)
	if strings.TrimSpace(subjectID) == "" && strings.TrimSpace(email) != "anonymous@gestalt" {
		t.Fatalf("expected non-empty subjectId or anonymous@gestalt email, got %#v", sessionBody)
	}
	if _, ok := sessionBody["credentialSubjectId"]; ok {
		t.Fatalf("expected session response to omit credentialSubjectId, got %v", sessionBody["credentialSubjectId"])
	}
	if _, ok := sessionBody["kind"]; ok {
		t.Fatalf("expected session response to omit kind, got %v", sessionBody["kind"])
	}
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
	operationConnection string
	catalogForRequestFn func(context.Context, string) (*catalog.Catalog, error)
	callFn              func(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error)
}

func (s *stubIntegrationWithSessionCatalog) Catalog() *catalog.Catalog {
	return s.catalog
}

func (s *stubIntegrationWithSessionCatalog) ConnectionForOperation(string) string {
	return s.operationConnection
}

func (s *stubIntegrationWithSessionCatalog) CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error) {
	if s.catalogForRequestFn != nil {
		return s.catalogForRequestFn(ctx, token)
	}
	return s.catalog, nil
}

func (s *stubIntegrationWithSessionCatalog) AuthTypes() []string { return []string{"manual"} }
func (s *stubIntegrationWithSessionCatalog) Close() error        { return nil }
func (s *stubIntegrationWithSessionCatalog) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	if op, ok := invocation.CatalogOperationFromContext(ctx, s.N, operation); ok {
		if invocation.OperationTransport(op) != catalog.TransportMCPPassthrough {
			return s.StubIntegration.Execute(ctx, operation, params, token)
		}
		return mcpupstream.ExecuteTool(ctx, s, operation, params, token)
	}
	if op, ok := catalog.OperationByID(s.catalog, operation); ok && invocation.OperationTransport(op) == catalog.TransportMCPPassthrough {
		return mcpupstream.ExecuteTool(ctx, s, operation, params, token)
	}
	return s.StubIntegration.Execute(ctx, operation, params, token)
}
func (s *stubIntegrationWithSessionCatalog) CallTool(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error) {
	if s.callFn != nil {
		return s.callFn(ctx, name, args)
	}
	return mcpgo.NewToolResultText("passthrough:" + name), nil
}

type stubIntegrationWithAuthURL struct {
	coretesting.StubIntegration
	authURL          string
	connectionParams map[string]core.ConnectionParamDef
}

func (s *stubIntegrationWithAuthURL) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return maps.Clone(s.connectionParams)
}

type stubPKCEIntegration struct {
	coretesting.StubIntegration
	authURL      string
	wantVerifier string
	gotVerifier  string
}

func (s *stubPKCEIntegration) StartOAuth(state string, _ []string) (string, string) {
	return s.AuthorizationURL(state, nil), s.wantVerifier
}

func (s *stubPKCEIntegration) ExchangeCodeWithVerifier(_ context.Context, code, verifier string, _ ...oauth.ExchangeOption) (*core.OAuthTokenResponse, error) {
	s.gotVerifier = verifier
	if code != "good-code" {
		return nil, fmt.Errorf("bad code")
	}
	return &core.OAuthTokenResponse{AccessToken: "pkce-token"}, nil
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
		exchangeCodeWithVerFn: func(_ context.Context, code, verifier string, _ ...oauth.ExchangeOption) (*core.OAuthTokenResponse, error) {
			stub.gotVerifier = verifier
			if code != "good-code" {
				return nil, fmt.Errorf("bad code")
			}
			return &core.OAuthTokenResponse{AccessToken: "pkce-token"}, nil
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"gitlab": testDefaultConnection}
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"gitlab": {
				Connections: map[string]*config.ConnectionDef{
					testDefaultConnection: oauthConnectionDef(nil),
				},
			},
		}
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

type stubOAuthIntegration struct {
	stubIntegrationWithOps
	refreshTokenFn func(context.Context, string) (*core.OAuthTokenResponse, error)
}

func (s *stubOAuthIntegration) RefreshToken(ctx context.Context, token string) (*core.OAuthTokenResponse, error) {
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

func (s *stubNonOAuthProvider) Name() string        { return s.name }
func (s *stubNonOAuthProvider) DisplayName() string { return s.name }
func (s *stubNonOAuthProvider) Description() string { return "" }
func (s *stubNonOAuthProvider) ConnectionMode() core.ConnectionMode {
	return core.ConnectionModeSubject
}
func (s *stubNonOAuthProvider) AuthTypes() []string { return nil }
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
	return &core.OperationResult{Status: http.StatusOK, Body: []byte(`{}`)}, nil
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

type serverTestLogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *serverTestLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *serverTestLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func installServerTestLogger(t *testing.T, logs *serverTestLogBuffer) {
	t.Helper()
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
}

func waitForServerStructuredLogRecord(t *testing.T, logs *serverTestLogBuffer, msg string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var record map[string]any
			if err := json.Unmarshal([]byte(line), &record); err != nil {
				t.Fatalf("log line is not valid JSON: %q: %v", line, err)
			}
			if record["msg"] == msg {
				return record
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("did not find structured log %q; output:\n%s", msg, logs.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func assertServerStructuredLogField(t *testing.T, record map[string]any, field string, want string) {
	t.Helper()
	if got := record[field]; got != want {
		t.Fatalf("%s = %v, want %s", field, got, want)
	}
}

func TestExecuteOperation_RefreshesExpiredToken(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	recordingCreds := newRecordingExternalCredentialProvider(svc.ExternalCredentials)
	svc.ExternalCredentials = recordingCreds
	u := seedUser(t, svc, "anonymous@gestalt")
	expired := time.Now().Add(-1 * time.Hour)
	seedToken(t, svc, &core.ExternalCredential{
		ID:        "tok1",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "fake:default",
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "expired-access", RefreshToken: "old-refresh-token", ExpiresAt: &expired},
	})

	var refreshedToken string
	stub := &stubOAuthIntegration{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N: "fake",
				ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
					refreshedToken = token
					return &core.OperationResult{Status: http.StatusOK, Body: []byte(`{"ok":true}`)}, nil
				},
			},
			ops: []core.Operation{{Name: "list", Description: "List", Method: http.MethodGet}},
		},
		refreshTokenFn: func(_ context.Context, rt string) (*core.OAuthTokenResponse, error) {
			if rt == "old-refresh-token" {
				return &core.OAuthTokenResponse{AccessToken: "fresh-access-token", ExpiresIn: 3600}, nil
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
	if recordingCreds.upsertCredentialCalls.Load() == 0 {
		t.Fatal("expected broker to persist refreshed credentials through ExternalCredentialProvider")
	}
}

func TestExecuteOperation_RefreshFailsButTokenStillValid(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	almostExpired := time.Now().Add(2 * time.Minute)
	seedToken(t, svc, &core.ExternalCredential{
		ID:        "tok1",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "fake:default",
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "still-valid-token", RefreshToken: "some-refresh", ExpiresAt: &almostExpired},
	})

	var usedToken string
	stub := &stubOAuthIntegration{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N: "fake",
				ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
					usedToken = token
					return &core.OperationResult{Status: http.StatusOK, Body: []byte(`{"ok":true}`)}, nil
				},
			},
			ops: []core.Operation{{Name: "list", Description: "List", Method: http.MethodGet}},
		},
		refreshTokenFn: func(context.Context, string) (*core.OAuthTokenResponse, error) {
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
				ID:        "tok1",
				Audience:  "fake:default",
				Qualifier: "default",
				Grant:     &core.ExternalCredentialGrant{AccessToken: "no-refresh-token"},
			},
			configureConnectionAuth: true,
			wantStatus:              http.StatusOK,
			wantUsedToken:           "no-refresh-token",
		},
		{
			name: "missing expiry",
			token: core.ExternalCredential{
				ID:        "tok1",
				Audience:  "fake:default",
				Qualifier: "default",
				Grant:     &core.ExternalCredentialGrant{AccessToken: "no-expiry-token", RefreshToken: "some-refresh"},
			},
			configureConnectionAuth: true,
			wantStatus:              http.StatusOK,
			wantUsedToken:           "no-expiry-token",
		},
		{
			name: "missing refresher",
			token: core.ExternalCredential{
				ID:        "tok1",
				Audience:  "fake:default",
				Qualifier: "default",
				Grant:     &core.ExternalCredentialGrant{AccessToken: "no-refresher-token", RefreshToken: "some-refresh", ExpiresAt: &expired},
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
			token.Subject = principal.UserSubjectID(u.ID)
			seedToken(t, svc, &token)

			refreshCalled := false
			var usedToken string
			stub := &stubOAuthIntegration{
				stubIntegrationWithOps: stubIntegrationWithOps{
					StubIntegration: coretesting.StubIntegration{
						N: "fake",
						ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
							usedToken = token
							return &core.OperationResult{Status: http.StatusOK, Body: []byte(`{}`)}, nil
						},
					},
					ops: []core.Operation{{Name: "list", Description: "List", Method: http.MethodGet}},
				},
			}

			var connectionAuth func() map[string]map[string]bootstrap.OAuthHandler
			if tc.configureConnectionAuth {
				connectionAuth = testConnectionAuth("fake", &testOAuthHandler{
					refreshTokenFn: func(context.Context, string) (*core.OAuthTokenResponse, error) {
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
		response          *core.OAuthTokenResponse
		wantAccessToken   string
		wantRefreshToken  string
		wantHasExpiration bool
	}{
		{
			name: "rotates refresh token and expiry",
			response: &core.OAuthTokenResponse{
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
			response: &core.OAuthTokenResponse{
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
				ID:        "tok1",
				Subject:   principal.UserSubjectID(u.ID),
				Audience:  "fake:default",
				Qualifier: "default",
				Grant:     &core.ExternalCredentialGrant{AccessToken: "old-access", RefreshToken: "old-refresh", ExpiresAt: &expired},
			})

			stub := &stubOAuthIntegration{
				stubIntegrationWithOps: stubIntegrationWithOps{
					StubIntegration: coretesting.StubIntegration{
						N: "fake",
						ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
							return &core.OperationResult{Status: http.StatusOK, Body: []byte(`{}`)}, nil
						},
					},
					ops: []core.Operation{{Name: "list", Description: "List", Method: http.MethodGet}},
				},
				refreshTokenFn: func(_ context.Context, _ string) (*core.OAuthTokenResponse, error) {
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
			if stored.Grant == nil {
				t.Fatal("stored grant = nil, want refreshed grant")
			}
			if stored.Grant.AccessToken != tc.wantAccessToken {
				t.Fatalf("stored access token = %q, want %q", stored.Grant.AccessToken, tc.wantAccessToken)
			}
			if stored.Grant.RefreshToken != tc.wantRefreshToken {
				t.Fatalf("stored refresh token = %q, want %q", stored.Grant.RefreshToken, tc.wantRefreshToken)
			}
			if (stored.Grant.ExpiresAt != nil) != tc.wantHasExpiration {
				t.Fatalf("stored expiry present = %v, want %v", stored.Grant.ExpiresAt != nil, tc.wantHasExpiration)
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
				ID:        "tok1",
				Subject:   principal.UserSubjectID(u.ID),
				Audience:  "fake:default",
				Qualifier: "default",
				Grant:     &core.ExternalCredentialGrant{AccessToken: "still-valid-token", RefreshToken: "some-refresh", ExpiresAt: &tc.expiresAt},
			})

			var usedToken string
			stub := &stubOAuthIntegration{
				stubIntegrationWithOps: stubIntegrationWithOps{
					StubIntegration: coretesting.StubIntegration{
						N: "fake",
						ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
							usedToken = token
							return &core.OperationResult{Status: http.StatusOK, Body: []byte(`{"ok":true}`)}, nil
						},
					},
					ops: []core.Operation{{Name: "list", Description: "List", Method: http.MethodGet}},
				},
				refreshTokenFn: func(context.Context, string) (*core.OAuthTokenResponse, error) {
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
		ID:        "tok1",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "fake:default",
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "original-token", RefreshToken: "some-refresh", ExpiresAt: &expired},
	})

	var usedToken string
	stub := &stubOAuthIntegration{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N: "fake",
				ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
					usedToken = token
					return &core.OperationResult{Status: http.StatusOK, Body: []byte(`{}`)}, nil
				},
			},
			ops: []core.Operation{{Name: "list", Description: "List", Method: http.MethodGet}},
		},
		refreshTokenFn: func(_ context.Context, _ string) (*core.OAuthTokenResponse, error) {
			ctx := context.Background()
			_ = svc.ExternalCredentials.UpsertCredential(ctx, &core.ExternalCredential{
				ID:        "tok1",
				Subject:   principal.UserSubjectID(u.ID),
				Audience:  "fake:default",
				Qualifier: "default",
				Grant:     &core.ExternalCredentialGrant{AccessToken: "concurrently-refreshed-token", RefreshToken: "new-refresh"},
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

func TestExecuteOperation_UpsertCredentialFailureReturnsError(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	provider := svc.ExternalCredentials.(*coretesting.StubExternalCredentialProvider)
	u := seedUser(t, svc, "anonymous@gestalt")
	expired := time.Now().Add(-1 * time.Hour)
	seedToken(t, svc, &core.ExternalCredential{
		ID:        "tok1",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "fake:default",
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "old-access", RefreshToken: "old-refresh", ExpiresAt: &expired},
	})

	stub := &stubOAuthIntegration{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "fake"},
			ops:             []core.Operation{{Name: "list", Description: "List", Method: http.MethodGet}},
		},
		refreshTokenFn: func(_ context.Context, _ string) (*core.OAuthTokenResponse, error) {
			provider.PutErr = fmt.Errorf("store unavailable")
			return &core.OAuthTokenResponse{
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
		t.Fatalf("expected 502 when UpsertCredential fails after refresh, got %d", resp.StatusCode)
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
				return &core.OperationResult{Status: http.StatusOK, Body: []byte(`{"ok":true}`)}, nil
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
		t.Fatal("indexeddb.Token should not be called for ConnectionModeNone")
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
				return &core.OperationResult{Status: http.StatusOK, Body: body}, nil
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
				return &core.OperationResult{Status: http.StatusOK, Body: body}, nil
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
	mcpHandler := gestaltmcp.NewStatelessHTTPHandler(gestaltmcp.Config{
		Invoker:   invoker,
		Providers: providers,
	})

	ctx := principal.WithPrincipal(context.Background(), &principal.Principal{
		Identity: &core.UserIdentity{Email: "dev@example.com"},
		UserID:   "u1",
		Source:   principal.SourceBearer,
	})
	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "echo_search",
			"arguments": map[string]any{"q": "hello"},
		},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(payload)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	mcpHandler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("MCP status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var mcpResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &mcpResp); err != nil {
		t.Fatalf("decode MCP response: %v", err)
	}
	result, ok := mcpResp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected MCP result, got %v", mcpResp)
	}
	if result["isError"] == true {
		t.Fatalf("unexpected MCP error result: %v", result)
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("expected one MCP content item, got %v", result["content"])
	}
	text, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("expected MCP text content, got %T", content[0])
	}

	httpJSON, _ := json.Marshal(httpBody)
	if text["text"] != string(httpJSON) {
		t.Fatalf("expected MCP body %s to match HTTP body %s", text["text"], string(httpJSON))
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
}

func (s *stubDiscoveringProvider) DiscoveryConfig() *core.DiscoveryConfig {
	return s.discovery
}

func (s *stubDiscoveringProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return maps.Clone(s.connectionParams)
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

type stubDualAuthProvider struct {
	coretesting.StubIntegration
}

func (s *stubDualAuthProvider) AuthTypes() []string { return []string{"oauth", "manual"} }
func (s *stubDualAuthProvider) CredentialFields() []core.CredentialFieldDef {
	return []core.CredentialFieldDef{{Name: "api_token", Label: "API Token"}}
}

func TestConnectManual_OAuthProviderRejected(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{N: "oauth-svc"})
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"integration":"oauth-svc","credential":"some-key"}`)
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
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"multi": {
				Connections: map[string]*config.ConnectionDef{
					"conn-a": oauthConnectionDef(nil),
					"conn-b": oauthConnectionDef(nil),
				},
			},
		}
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
		exchangeCodeFn: func(_ context.Context, code string) (*core.OAuthTokenResponse, error) {
			exchangedConnection = "conn-b"
			return &core.OAuthTokenResponse{AccessToken: "token-for-b"}, nil
		},
	}

	stub := &stubIntegrationWithAuthURL{
		StubIntegration: coretesting.StubIntegration{N: "multi"},
		authURL:         "https://provider.example/oauth",
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"multi": "conn-a"}
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"multi": {
				Connections: map[string]*config.ConnectionDef{
					"conn-a": oauthConnectionDef(nil),
					"conn-b": oauthConnectionDef(nil),
				},
			},
		}
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
				Subject:      principal.UserSubjectID(u.ID),
				Audience:     "fake:default",
				Qualifier:    "default",
				Grant:        &core.ExternalCredentialGrant{AccessToken: "old-token", RefreshToken: "old-refresh", ExpiresAt: &expired},
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
							return &core.OperationResult{Status: http.StatusOK, Body: []byte(`{"ok":true}`)}, nil
						},
					},
					ops: []core.Operation{{Name: "list", Description: "List", Method: http.MethodGet}},
				},
			}
			handler := &testOAuthHandler{
				tokenURLVal: tc.tokenURL,
				refreshTokenFn: func(_ context.Context, rt string) (*core.OAuthTokenResponse, error) {
					if tc.wantRefreshedURL != "" {
						t.Fatalf("expected refresh to use resolved token URL override")
					}
					refreshedToken = rt
					return &core.OAuthTokenResponse{AccessToken: "refreshed-token", ExpiresIn: 3600}, nil
				},
				refreshTokenWithURLFn: func(_ context.Context, rt, tokenURL string) (*core.OAuthTokenResponse, error) {
					if tc.wantRefreshedURL == "" {
						t.Fatalf("expected direct refresh without token URL override")
					}
					refreshedToken = rt
					refreshedURL = tokenURL
					return &core.OAuthTokenResponse{AccessToken: "refreshed-token", ExpiresIn: 3600}, nil
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
		Subject:      principal.UserSubjectID(u.ID),
		Audience:     "fake:default",
		Qualifier:    "default",
		Grant:        &core.ExternalCredentialGrant{AccessToken: "old-token", RefreshToken: "old-refresh", ExpiresAt: &expired},
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
					return &core.OperationResult{Status: http.StatusOK, Body: []byte(`{"ok":true}`)}, nil
				},
			},
			ops: []core.Operation{{Name: "list", Description: "List", Method: http.MethodGet}},
		},
	}
	handler := &testOAuthHandler{
		tokenURLVal: "https://{tenant}.example.com/oauth/token",
		refreshTokenFn: func(context.Context, string) (*core.OAuthTokenResponse, error) {
			t.Fatal("expected refresh to use resolved token URL override")
			return nil, nil
		},
		refreshTokenWithURLFn: func(_ context.Context, rt, tokenURL string) (*core.OAuthTokenResponse, error) {
			refreshedToken = rt
			refreshedURL = tokenURL
			return &core.OAuthTokenResponse{AccessToken: "refreshed-token", ExpiresIn: 3600}, nil
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

func newMCPHandler(t *testing.T, providers *registry.ProviderMap[core.Provider], svc *coredata.Services, auditSink core.AuditSink, _ any) http.Handler {
	t.Helper()
	broker := invocation.NewBroker(providers, svc.Users, svc.ExternalCredentials)
	return gestaltmcp.NewStatelessHTTPHandler(gestaltmcp.Config{
		Invoker:       broker,
		TokenResolver: broker,
		AuditSink:     auditSink,
		Providers:     providers,
	})
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

	status, resp, header := mcpJSONRPCWithHeaders(t, ts, nil, map[string]any{
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
	if got := header.Get("Mcp-Session-Id"); got != "" {
		t.Fatalf("initialize returned MCP session id %q, want none", got)
	}
	capabilities, ok := result["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("initialize: expected capabilities object, got %v", result["capabilities"])
	}
	toolsCapability, ok := capabilities["tools"].(map[string]any)
	if !ok {
		t.Fatalf("initialize: expected tools capability, got %v", capabilities["tools"])
	}
	if got := toolsCapability["listChanged"]; got != nil && got != false {
		t.Fatalf("tools.listChanged = %v, want false or omitted", got)
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

func TestMCPEndpoint_EmptyAllowedProvidersExposesNoTools(t *testing.T) {
	t.Parallel()

	stub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{N: "linear"},
		ops: []core.Operation{
			{Name: "search_issues", Description: "Search issues", Method: http.MethodGet},
		},
	}
	svc := testutil.NewStubServices(t)
	providers := testutil.NewProviderRegistry(t, stub)
	broker := invocation.NewBroker(providers, svc.Users, svc.ExternalCredentials)
	mcpHandler := gestaltmcp.NewStatelessHTTPHandler(gestaltmcp.Config{
		Invoker:          broker,
		TokenResolver:    broker,
		Providers:        providers,
		AllowedProviders: []string{},
	})

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = providers
		cfg.Services = svc
		cfg.MCPHandler = mcpHandler
	})
	defer ts.Close()

	status, resp := mcpJSONRPC(t, ts, nil, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
	})
	if status != http.StatusOK {
		t.Fatalf("tools/list status = %d, want 200", status)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object, got %v", resp)
	}
	tools, ok := result["tools"].([]any)
	if !ok {
		t.Fatalf("expected tools list, got %v", result["tools"])
	}
	if len(tools) != 0 {
		t.Fatalf("tools = %v, want empty list", tools)
	}

	status, resp = mcpJSONRPC(t, ts, nil, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "linear_search_issues",
			"arguments": map[string]any{},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("tools/call status = %d, want 200", status)
	}
	if _, ok := resp["error"].(map[string]any); !ok {
		t.Fatalf("expected JSON-RPC error for disallowed tool call, got %v", resp)
	}
}

func TestMCPEndpoint_ListUsesStrictCatalogResolution(t *testing.T) {
	t.Parallel()

	stub := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "sample", ConnMode: core.ConnectionModeSubject},
		},
		catalog: &catalog.Catalog{Name: "sample", Operations: []catalog.CatalogOperation{{
			ID:          "static_fallback",
			Description: "Static fallback",
			Method:      http.MethodGet,
			Transport:   catalog.TransportREST,
		}}},
	}

	svc := testutil.NewStubServices(t)
	seedUser(t, svc, "user@example.com")
	providers := testutil.NewProviderRegistry(t, stub)
	mcpHandler := newMCPHandler(t, providers, svc, nil, nil)

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

	status, resp := mcpJSONRPC(t, ts, map[string]string{
		"Authorization": "Bearer session-token",
	}, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
	})
	if status != http.StatusOK {
		t.Fatalf("tools/list status = %d, want 200", status)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object, got %v", resp)
	}
	tools, ok := result["tools"].([]any)
	if !ok {
		t.Fatalf("expected tools list, got %v", result["tools"])
	}
	if len(tools) != 0 {
		t.Fatalf("tools = %v, want empty list when strict catalog resolution fails", tools)
	}

	status, resp = mcpJSONRPC(t, ts, map[string]string{
		"Authorization": "Bearer session-token",
	}, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "sample_static_fallback",
			"arguments": map[string]any{},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("tools/call status = %d, want 200", status)
	}
	result, ok = resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected tool result object, got %v", resp)
	}
	if result["isError"] != true {
		t.Fatalf("tools/call result = %v, want MCP tool error", result)
	}
	if _, ok := resp["error"]; ok {
		t.Fatalf("tools/call response = %v, want tool error instead of JSON-RPC error", resp)
	}
}

func TestMCPEndpoint_MethodsReturn405BeforeAuth(t *testing.T) {
	t.Parallel()

	providers := func() *registry.ProviderMap[core.Provider] {
		reg := registry.New()
		return &reg.Providers
	}()
	svc := testutil.NewStubServices(t)
	seedUser(t, svc, "user@example.com")
	mcpHandler := newMCPHandler(t, providers, svc, nil, nil)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "valid-token" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: "user@example.com"}, nil
			},
		}
		cfg.PublicBaseURL = "https://valon.tools"
		cfg.Services = svc
		cfg.MCPHandler = mcpHandler
	})
	defer ts.Close()

	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodDelete, http.MethodOptions, http.MethodPatch} {
		req, _ := http.NewRequest(method, ts.URL+"/mcp", nil)
		req.Header.Set("Origin", "https://evil.example")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s /mcp: %v", method, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("%s /mcp status = %d, want 405", method, resp.StatusCode)
		}
		if got := resp.Header.Get("WWW-Authenticate"); got != "" {
			t.Fatalf("%s /mcp WWW-Authenticate = %q, want none", method, got)
		}
	}
}

func TestMCPEndpoint_ValidOriginAndForwardedHostAllowed(t *testing.T) {
	t.Parallel()

	providers := func() *registry.ProviderMap[core.Provider] {
		reg := registry.New()
		return &reg.Providers
	}()
	svc := testutil.NewStubServices(t)
	seedUser(t, svc, "user@example.com")
	mcpHandler := newMCPHandler(t, providers, svc, nil, nil)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "valid-token" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: "user@example.com"}, nil
			},
		}
		cfg.PublicBaseURL = "https://valon.tools"
		cfg.Services = svc
		cfg.MCPHandler = mcpHandler
	})
	defer ts.Close()

	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
	})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Origin", "https://VALON.tools:443")
	req.Header.Set("X-Forwarded-Host", "VALON.tools:443")
	req.Header.Set("X-Original-Host", "VALON.tools:443")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestMCPEndpoint_InvalidOriginRejectedBeforeAuth(t *testing.T) {
	t.Parallel()

	providers := func() *registry.ProviderMap[core.Provider] {
		reg := registry.New()
		return &reg.Providers
	}()
	svc := testutil.NewStubServices(t)
	mcpHandler := newMCPHandler(t, providers, svc, nil, nil)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
		cfg.PublicBaseURL = "https://valon.tools"
		cfg.MCPHandler = mcpHandler
	})
	defer ts.Close()

	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
	})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if got := resp.Header.Get("WWW-Authenticate"); got != "" {
		t.Fatalf("WWW-Authenticate = %q, want none", got)
	}
}

func TestMCPEndpoint_ForwardedHostMismatchRejectedBeforeAuth(t *testing.T) {
	t.Parallel()

	providers := func() *registry.ProviderMap[core.Provider] {
		reg := registry.New()
		return &reg.Providers
	}()
	svc := testutil.NewStubServices(t)
	mcpHandler := newMCPHandler(t, providers, svc, nil, nil)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
		cfg.PublicBaseURL = "https://valon.tools"
		cfg.MCPHandler = mcpHandler
	})
	defer ts.Close()

	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
	})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://valon.tools")
	req.Header.Set("X-Forwarded-Host", "evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if got := resp.Header.Get("WWW-Authenticate"); got != "" {
		t.Fatalf("WWW-Authenticate = %q, want none", got)
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

func TestMCPEndpoint_IgnoresSessionIDForDynamicCatalogIsolation(t *testing.T) {
	t.Parallel()

	stub := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "sample", ConnMode: core.ConnectionModeSubject},
		},
		catalogForRequestFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			switch token {
			case "access-a":
				return &catalog.Catalog{Name: "sample", Operations: []catalog.CatalogOperation{{
					ID:          "only_a",
					Description: "Only A",
					Method:      http.MethodPost,
					Transport:   catalog.TransportREST,
				}}}, nil
			case "access-b":
				return &catalog.Catalog{Name: "sample", Operations: []catalog.CatalogOperation{{
					ID:          "only_b",
					Description: "Only B",
					Method:      http.MethodPost,
					Transport:   catalog.TransportREST,
				}}}, nil
			default:
				return nil, fmt.Errorf("unexpected token %q", token)
			}
		},
	}

	svc := testutil.NewStubServices(t)
	userA := seedUser(t, svc, "a@example.com")
	userB := seedUser(t, svc, "b@example.com")
	seedToken(t, svc, &core.ExternalCredential{
		ID:        "tok-a",
		Subject:   principal.UserSubjectID(userA.ID),
		Audience:  "sample:" + config.AppConnectionName,
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "access-a"},
	})
	seedToken(t, svc, &core.ExternalCredential{
		ID:        "tok-b",
		Subject:   principal.UserSubjectID(userB.ID),
		Audience:  "sample:" + config.AppConnectionName,
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "access-b"},
	})

	providers := testutil.NewProviderRegistry(t, stub)
	mcpHandler := newMCPHandler(t, providers, svc, nil, nil)
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				switch token {
				case "auth-a":
					return &core.UserIdentity{Email: "a@example.com"}, nil
				case "auth-b":
					return &core.UserIdentity{Email: "b@example.com"}, nil
				default:
					return nil, core.ErrNotFound
				}
			},
		}
		cfg.Providers = providers
		cfg.Services = svc
		cfg.MCPHandler = mcpHandler
	})
	defer ts.Close()

	listToolNames := func(authToken string) []string {
		t.Helper()
		status, resp := mcpJSONRPC(t, ts, map[string]string{
			"Authorization":  "Bearer " + authToken,
			"Mcp-Session-Id": "shared-session-id",
		}, map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "tools/list",
		})
		if status != http.StatusOK {
			t.Fatalf("tools/list status = %d, want 200", status)
		}
		result, ok := resp["result"].(map[string]any)
		if !ok {
			t.Fatalf("expected result object, got %v", resp)
		}
		tools, ok := result["tools"].([]any)
		if !ok {
			t.Fatalf("expected tools list, got %v", result["tools"])
		}
		names := make([]string, 0, len(tools))
		for _, rawTool := range tools {
			tool, ok := rawTool.(map[string]any)
			if !ok {
				t.Fatalf("tool = %v, want object", rawTool)
			}
			names = append(names, fmt.Sprint(tool["name"]))
		}
		return names
	}

	if got := listToolNames("auth-a"); !reflect.DeepEqual(got, []string{"sample_only_a"}) {
		t.Fatalf("auth-a tools = %v, want [sample_only_a]", got)
	}
	if got := listToolNames("auth-b"); !reflect.DeepEqual(got, []string{"sample_only_b"}) {
		t.Fatalf("auth-b tools = %v, want [sample_only_b]", got)
	}
}

func TestMCPEndpoint_CallResolvesDynamicCatalogPerRequest(t *testing.T) {
	t.Parallel()

	stub := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N:        "sample",
				ConnMode: core.ConnectionModeSubject,
				ExecuteFn: func(_ context.Context, op string, params map[string]any, token string) (*core.OperationResult, error) {
					body, _ := json.Marshal(map[string]any{
						"op":    op,
						"mode":  params["mode"],
						"token": token,
					})
					return &core.OperationResult{Status: http.StatusOK, Body: body}, nil
				},
			},
		},
		catalogForRequestFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			var mode string
			switch token {
			case "access-a":
				mode = "a"
			case "access-b":
				mode = "b"
			default:
				return nil, fmt.Errorf("unexpected token %q", token)
			}
			return &catalog.Catalog{Name: "sample", Operations: []catalog.CatalogOperation{{
				ID:          "whoami",
				Description: "Who am I",
				Method:      http.MethodPost,
				Transport:   catalog.TransportREST,
				InputSchema: json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{"mode":{"type":"string","enum":[%q]}}}`, mode)),
			}}}, nil
		},
	}

	svc := testutil.NewStubServices(t)
	userA := seedUser(t, svc, "a@example.com")
	userB := seedUser(t, svc, "b@example.com")
	seedToken(t, svc, &core.ExternalCredential{
		ID:        "call-tok-a",
		Subject:   principal.UserSubjectID(userA.ID),
		Audience:  "sample:" + config.AppConnectionName,
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "access-a"},
	})
	seedToken(t, svc, &core.ExternalCredential{
		ID:        "call-tok-b",
		Subject:   principal.UserSubjectID(userB.ID),
		Audience:  "sample:" + config.AppConnectionName,
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "access-b"},
	})

	providers := testutil.NewProviderRegistry(t, stub)
	mcpHandler := newMCPHandler(t, providers, svc, nil, nil)
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				switch token {
				case "auth-a":
					return &core.UserIdentity{Email: "a@example.com"}, nil
				case "auth-b":
					return &core.UserIdentity{Email: "b@example.com"}, nil
				default:
					return nil, core.ErrNotFound
				}
			},
		}
		cfg.Providers = providers
		cfg.Services = svc
		cfg.MCPHandler = mcpHandler
	})
	defer ts.Close()

	call := func(authToken, mode, wantAccessToken string) {
		t.Helper()
		status, resp := mcpJSONRPC(t, ts, map[string]string{
			"Authorization":  "Bearer " + authToken,
			"Mcp-Session-Id": "shared-session-id",
		}, map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "tools/call",
			"params": map[string]any{
				"name":      "sample_whoami",
				"arguments": map[string]any{"mode": mode},
			},
		})
		if status != http.StatusOK {
			t.Fatalf("tools/call status = %d, want 200", status)
		}
		result, ok := resp["result"].(map[string]any)
		if !ok {
			t.Fatalf("expected result object, got %v", resp)
		}
		if result["isError"] == true {
			t.Fatalf("tools/call returned MCP error: %v", result)
		}
		content, ok := result["content"].([]any)
		if !ok || len(content) == 0 {
			t.Fatalf("expected content in result, got %v", result)
		}
		textBlock, ok := content[0].(map[string]any)
		if !ok {
			t.Fatalf("expected text block, got %v", content[0])
		}
		var body map[string]any
		if err := json.Unmarshal([]byte(fmt.Sprint(textBlock["text"])), &body); err != nil {
			t.Fatalf("decode tool body: %v", err)
		}
		if body["mode"] != mode || body["token"] != wantAccessToken {
			t.Fatalf("tool body = %v, want mode %q and token %q", body, mode, wantAccessToken)
		}
	}

	call("auth-a", "a", "access-a")
	call("auth-b", "b", "access-b")

	status, resp := mcpJSONRPC(t, ts, map[string]string{
		"Authorization":  "Bearer auth-b",
		"Mcp-Session-Id": "shared-session-id",
	}, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "sample_whoami",
			"arguments": map[string]any{"mode": "a"},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("tools/call status = %d, want 200", status)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object, got %v", resp)
	}
	if result["isError"] != true {
		t.Fatalf("cross-context enum call result = %v, want MCP error", result)
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
	auditSink := invocation.NewSlogAuditSink(io.Discard)
	prov := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "clickhouse", ConnMode: core.ConnectionModeNone},
			ops:             []core.Operation{{Name: "run_query", Description: "Execute a SQL query"}},
		},
		catalog: cat,
		callFn: func(ctx context.Context, name string, _ map[string]any) (*mcpgo.CallToolResult, error) {
			_ = ctx
			calledName = name
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
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("expected content in result, got %v", result)
	}
	textBlock := content[0].(map[string]any)
	if textBlock["text"] != "query executed" {
		t.Fatalf("expected passthrough result, got %v", textBlock)
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
				return &core.OperationResult{Status: http.StatusOK, Body: []byte(`{"ok":true}`)}, nil
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
		ID:        "tok1",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "test-int:" + config.AppConnectionName,
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "test-token"},
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
			Body:   []byte(""),
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
		ID:        "tok1",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "test-int:" + config.AppConnectionName,
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "test-token"},
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
		ID:        "tok1",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "test-int:" + config.AppConnectionName,
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "test-token"},
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
		ID:        "tok1",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "test-int:" + config.AppConnectionName,
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "test-token"},
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
	reqNoCookie, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
	noAuthResp, err := http.DefaultClient.Do(reqNoCookie)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = noAuthResp.Body.Close() }()
	if noAuthResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without cookie, got %d", noAuthResp.StatusCode)
	}

	// Request with cookie should pass auth middleware.
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
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
	reqWithFallback, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
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
	auth := newHostIssuedSessionAuthStub(secret, hostIssuedSessionAuthOpts{})
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

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("integrations request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusUnauthorized {
		t.Fatal("host-issued session cookie should authenticate subsequent requests")
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

func TestExecuteOperation_ConnectionModeSubjectUsesSubjectCredential(t *testing.T) {
	t.Parallel()

	stub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "svc",
			ConnMode: core.ConnectionModeSubject,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: []byte(fmt.Sprintf(`{"token":%q}`, token))}, nil
			},
		},
		ops: []core.Operation{{Name: "do", Method: http.MethodGet}},
	}

	t.Run("prefers subject token", func(t *testing.T) {
		t.Parallel()

		svc := testutil.NewStubServices(t)
		u, _ := svc.Users.FindOrCreateUser(context.Background(), "api-user@test.local")
		apiToken := scopedTestBearerToken(u.ID, "")
		seedToken(t, svc, &core.ExternalCredential{
			ID:        "tok-user",
			Subject:   principal.UserSubjectID(u.ID),
			Audience:  "svc:" + config.AppConnectionName,
			Qualifier: "default",
			Grant:     &core.ExternalCredentialGrant{AccessToken: "user-tok"},
		})

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Auth = testAuthStubForScopedBearer()
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
			pluginDefs: map[string]*config.ProviderEntry{
				"multi-key-svc": {
					Auth: &config.ConnectionAuthDef{
						Type: providermanifestv1.AuthTypeManual,
						Credentials: []config.CredentialFieldDef{
							{Name: "api_key", Label: "API Key"},
							{Name: "app_key", Label: "App Key"},
						},
					},
				},
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
				cfg.DefaultConnection = map[string]string{tc.integration: config.AppConnectionName}
				cfg.AppDefs = tc.pluginDefs
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
			tokens, _ := svc.ExternalCredentials.ListCredentials(context.Background(), principal.UserSubjectID(u.ID), "")
			if len(tokens) == 0 {
				t.Fatal("expected credential to be stored")
			}
			stored := tokens[0]

			if stored.Opaque == nil {
				t.Fatalf("stored credential = %+v, want opaque fields", stored)
			}
			if !reflect.DeepEqual(stored.Opaque.Fields, tc.wantTokenData) {
				t.Fatalf("token data = %+v, want %+v", stored.Opaque.Fields, tc.wantTokenData)
			}
		})
	}
}

func TestConnectManual_ServiceAccountIDStoresCredentialForServiceAccount(t *testing.T) {
	t.Parallel()

	const serviceAccountSubjectID = "service_account:manual-bot"

	svc := testutil.NewStubServices(t)
	authz := &serviceAccountCredentialAuthorizationProvider{allowed: true}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, &stubManualProvider{
			StubIntegration: coretesting.StubIntegration{N: "manual-service-account"},
		})
		cfg.DefaultConnection = map[string]string{"manual-service-account": config.AppConnectionName}
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"manual-service-account": {
				Auth: &config.ConnectionAuthDef{Type: providermanifestv1.AuthTypeManual},
			},
		}
		cfg.Authorization = authz
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/connect-manual", bytes.NewBufferString(`{"integration":"manual-service-account","serviceAccountId":"service_account:manual-bot","credential":"manual-service-account-token"}`))
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

	tokens, err := svc.ExternalCredentials.ListCredentials(context.Background(), serviceAccountSubjectID, "")
	if err != nil {
		t.Fatalf("ListCredentials(service account): %v", err)
	}
	if len(tokens) != 1 {
		t.Fatalf("service account tokens len = %d, want 1", len(tokens))
	}
	if tokens[0].Grant == nil || tokens[0].Grant.AccessToken != "manual-service-account-token" {
		t.Fatalf("stored grant = %+v, want access token manual-service-account-token", tokens[0].Grant)
	}
	if len(authz.requests) != 1 {
		t.Fatalf("authorization requests = %d, want 1", len(authz.requests))
	}
	authReq := authz.requests[0]
	if authReq.GetSubject().GetType() != "subject" || !strings.HasPrefix(authReq.GetSubject().GetId(), "user:") {
		t.Fatalf("authorization subject = %+v, want user subject", authReq.GetSubject())
	}
	if got := authReq.GetAction().GetName(); got != "manages" {
		t.Fatalf("authorization action = %q, want manages", got)
	}
	if resource := authReq.GetResource(); resource.GetType() != "service_account" || resource.GetId() != serviceAccountSubjectID {
		t.Fatalf("authorization resource = %+v, want service_account/%s", resource, serviceAccountSubjectID)
	}
}

func TestConnectManual_ServiceAccountIDAuthorizesInvokingSubjectNotCredentialSubject(t *testing.T) {
	t.Parallel()

	const (
		serviceAccountSubjectID = "service_account:manual-bot"
	)

	svc := testutil.NewStubServices(t)
	user, err := svc.Users.FindOrCreateUser(context.Background(), "api-user@test.local")
	if err != nil {
		t.Fatalf("FindOrCreateUser: %v", err)
	}
	apiToken := scopedTestBearerToken(user.ID, "")
	authz := &serviceAccountCredentialAuthorizationProvider{allowed: true}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = testAuthStubForScopedBearer()
		cfg.Providers = testutil.NewProviderRegistry(t, &stubManualProvider{
			StubIntegration: coretesting.StubIntegration{N: "manual-service-account-invoker"},
		})
		cfg.DefaultConnection = map[string]string{"manual-service-account-invoker": config.AppConnectionName}
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"manual-service-account-invoker": {
				Auth: &config.ConnectionAuthDef{Type: providermanifestv1.AuthTypeManual},
			},
		}
		cfg.Authorization = authz
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/connect-manual", bytes.NewBufferString(`{"integration":"manual-service-account-invoker","serviceAccountId":"manual-bot","credential":"manual-service-account-token"}`))
	req.Header.Set("Authorization", "Bearer "+apiToken)
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
	if len(authz.requests) != 1 {
		t.Fatalf("authorization requests = %d, want 1", len(authz.requests))
	}
	authReq := authz.requests[0]
	if got, want := authReq.GetSubject().GetId(), principal.UserSubjectID(user.ID); got != want {
		t.Fatalf("authorization subject id = %q, want invoking subject %q", got, want)
	}
	if got := authReq.GetSubject().GetId(); strings.HasPrefix(got, "service_account:") {
		t.Fatalf("authorization subject id = %q, must not use service account credential subject", got)
	}
	if resource := authReq.GetResource(); resource.GetType() != "service_account" || resource.GetId() != serviceAccountSubjectID {
		t.Fatalf("authorization resource = %+v, want service_account/%s", resource, serviceAccountSubjectID)
	}
}

func TestConnectManual_ServiceAccountIDRequiresManagesAuthorization(t *testing.T) {
	t.Parallel()

	const serviceAccountSubjectID = "service_account:manual-bot"

	svc := testutil.NewStubServices(t)
	authz := &serviceAccountCredentialAuthorizationProvider{allowed: false}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, &stubManualProvider{
			StubIntegration: coretesting.StubIntegration{N: "manual-service-account-denied"},
		})
		cfg.DefaultConnection = map[string]string{"manual-service-account-denied": config.AppConnectionName}
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"manual-service-account-denied": {
				Auth: &config.ConnectionAuthDef{Type: providermanifestv1.AuthTypeManual},
			},
		}
		cfg.Authorization = authz
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/connect-manual", bytes.NewBufferString(`{"integration":"manual-service-account-denied","serviceAccountId":"manual-bot","credential":"manual-service-account-token"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 403: %s", resp.StatusCode, body)
	}
	if len(authz.requests) != 1 {
		t.Fatalf("authorization requests = %d, want 1", len(authz.requests))
	}
	authReq := authz.requests[0]
	if got := authReq.GetAction().GetName(); got != "manages" {
		t.Fatalf("authorization action = %q, want manages", got)
	}
	if resource := authReq.GetResource(); resource.GetType() != "service_account" || resource.GetId() != serviceAccountSubjectID {
		t.Fatalf("authorization resource = %+v, want service_account/%s", resource, serviceAccountSubjectID)
	}
	tokens, err := svc.ExternalCredentials.ListCredentials(context.Background(), serviceAccountSubjectID, "")
	if err != nil {
		t.Fatalf("ListCredentials(service account): %v", err)
	}
	if len(tokens) != 0 {
		t.Fatalf("service account tokens len = %d, want 0", len(tokens))
	}
}

func TestSelectPendingConnection_ServiceAccountIDRequiresManagesAuthorization(t *testing.T) {
	t.Parallel()

	const serviceAccountSubjectID = "service_account:manual-bot"

	discoverySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"id":"site-a","name":"Site A","workspace":"alpha"},{"id":"site-b","name":"Site B","workspace":"beta"}]`)
	}))
	testutil.CloseOnCleanup(t, discoverySrv)

	svc := testutil.NewStubServices(t)
	authz := &serviceAccountCredentialAuthorizationProvider{allowed: true}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "session-token" {
					return nil, fmt.Errorf("bad token")
				}
				return &core.UserIdentity{Email: "service-account-manager@test.local"}, nil
			},
		}
		cfg.Providers = testutil.NewProviderRegistry(t, &stubManualProviderWithCapabilities{
			stubManualProvider: stubManualProvider{
				StubIntegration: coretesting.StubIntegration{N: "manual-service-account-pending"},
			},
			discovery: &core.DiscoveryConfig{
				URL:      discoverySrv.URL,
				IDPath:   "id",
				NamePath: "name",
				Metadata: map[string]string{"workspace": "workspace"},
			},
		})
		cfg.DefaultConnection = map[string]string{"manual-service-account-pending": config.AppConnectionName}
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"manual-service-account-pending": {
				Auth: &config.ConnectionAuthDef{Type: providermanifestv1.AuthTypeManual},
				ConnectionParams: map[string]config.ConnectionParamDef{
					"workspace": {From: "selection", Field: "workspace"},
				},
			},
		}
		cfg.Authorization = authz
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/connect-manual", bytes.NewBufferString(`{"integration":"manual-service-account-pending","serviceAccountId":"manual-bot","credential":"manual-service-account-token"}`))
	req.Header.Set("Authorization", "Bearer session-token")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("connect status = %d, want 200: %s", resp.StatusCode, body)
	}
	var connectResp struct {
		Status       string `json:"status"`
		PendingToken string `json:"pendingToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&connectResp); err != nil {
		t.Fatalf("decode connect response: %v", err)
	}
	if connectResp.Status != "selection_required" || connectResp.PendingToken == "" {
		t.Fatalf("connect response = %+v, want selection_required with pending token", connectResp)
	}

	authz.allowed = false
	form := url.Values{
		"pending_token":   {connectResp.PendingToken},
		"candidate_index": {"0"},
	}
	selectReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/pending-connection", strings.NewReader(form.Encode()))
	selectReq.Header.Set("Authorization", "Bearer session-token")
	selectReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	selectResp, err := http.DefaultClient.Do(selectReq)
	if err != nil {
		t.Fatalf("select request: %v", err)
	}
	defer func() { _ = selectResp.Body.Close() }()
	if selectResp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(selectResp.Body)
		t.Fatalf("select status = %d, want 403: %s", selectResp.StatusCode, body)
	}
	if len(authz.requests) != 2 {
		t.Fatalf("authorization requests = %d, want 2", len(authz.requests))
	}
	for idx, authReq := range authz.requests {
		if got := authReq.GetAction().GetName(); got != "manages" {
			t.Fatalf("authorization request %d action = %q, want manages", idx, got)
		}
		if resource := authReq.GetResource(); resource.GetType() != "service_account" || resource.GetId() != serviceAccountSubjectID {
			t.Fatalf("authorization request %d resource = %+v, want service_account/%s", idx, resource, serviceAccountSubjectID)
		}
	}
	tokens, err := svc.ExternalCredentials.ListCredentials(context.Background(), serviceAccountSubjectID, "")
	if err != nil {
		t.Fatalf("ListCredentials(service account): %v", err)
	}
	if len(tokens) != 0 {
		t.Fatalf("service account tokens len = %d, want 0", len(tokens))
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
			cfg.DefaultConnection = map[string]string{"looker-like": config.AppConnectionName}
			cfg.AppDefs = map[string]*config.ProviderEntry{
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
					ConnectionParams: map[string]config.ConnectionParamDef{
						"account_id": {From: "token_response", Field: "account.id", Required: true},
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
		tokens, _ := svc.ExternalCredentials.ListCredentials(context.Background(), principal.UserSubjectID(u.ID), "")
		if len(tokens) != 1 {
			t.Fatalf("stored credentials = %d, want 1", len(tokens))
		}
		stored := tokens[0]
		if stored.Grant != nil {
			t.Fatalf("stored grant = %+v, want minted token not persisted", stored.Grant)
		}
		if stored.Opaque == nil || !reflect.DeepEqual(stored.Opaque.Fields, map[string]string{"client_id": "id-123", "client_secret": "secret-456"}) {
			t.Fatalf("stored opaque = %+v, want pasted credential fields", stored.Opaque)
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
			cfg.DefaultConnection = map[string]string{"json-token": config.AppConnectionName}
			cfg.AppDefs = map[string]*config.ProviderEntry{
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
		tokens, _ := svc.ExternalCredentials.ListCredentials(context.Background(), principal.UserSubjectID(u.ID), "")
		if len(tokens) != 1 {
			t.Fatalf("stored credentials = %d, want 1", len(tokens))
		}
		if tokens[0].Grant != nil {
			t.Fatalf("stored grant = %+v, want minted token not persisted", tokens[0].Grant)
		}
		if tokens[0].Opaque == nil || !reflect.DeepEqual(tokens[0].Opaque.Fields, map[string]string{"client_id": "json-id", "client_secret": "json-secret"}) {
			t.Fatalf("stored opaque = %+v, want pasted credential fields", tokens[0].Opaque)
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
			cfg.DefaultConnection = map[string]string{"fallback-token": config.AppConnectionName}
			cfg.AppDefs = map[string]*config.ProviderEntry{
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
		tokens, _ := svc.ExternalCredentials.ListCredentials(context.Background(), principal.UserSubjectID(u.ID), "")
		if len(tokens) != 1 {
			t.Fatalf("stored credentials = %d, want 1", len(tokens))
		}
		if tokens[0].Grant != nil {
			t.Fatalf("stored grant = %+v, want minted token not persisted", tokens[0].Grant)
		}
		if tokens[0].Opaque == nil || !reflect.DeepEqual(tokens[0].Opaque.Fields, map[string]string{"client_id": "fallback-id", "client_secret": "fallback-secret"}) {
			t.Fatalf("stored opaque = %+v, want pasted credential fields", tokens[0].Opaque)
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
					cfg.DefaultConnection = map[string]string{"strict-token": config.AppConnectionName}
					cfg.AppDefs = map[string]*config.ProviderEntry{
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
		ID:        "tok-manual",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "manual-refresh:default",
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "expired-manual", RefreshToken: sourceCredential, ExpiresAt: &expired},
	})

	var usedToken string
	stub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N: "manual-refresh",
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
				usedToken = token
				return &core.OperationResult{Status: http.StatusOK, Body: []byte(`{"ok":true}`)}, nil
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

	tokens, _ := svc.ExternalCredentials.ListCredentials(context.Background(), principal.UserSubjectID(u.ID), "")
	if len(tokens) != 1 {
		t.Fatalf("stored credentials = %d, want 1", len(tokens))
	}
	if tokens[0].Grant == nil || tokens[0].Grant.AccessToken != "refreshed-manual" {
		t.Fatalf("stored grant = %+v, want access token refreshed-manual", tokens[0].Grant)
	}
	if tokens[0].Grant.RefreshToken != sourceCredential {
		t.Fatalf("stored refresh source = %q, want original credential JSON", tokens[0].Grant.RefreshToken)
	}
}

func TestAPITokenScopes_EnforcedDuringInvocation(t *testing.T) {
	t.Parallel()

	alphaStub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "alpha",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: []byte(`{"ok":true}`)}, nil
			},
		},
		ops: []core.Operation{{Name: "do_thing", Method: http.MethodGet}},
	}
	betaStub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "beta",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: []byte(`{"ok":true}`)}, nil
			},
		},
		ops: []core.Operation{{Name: "do_thing", Method: http.MethodGet}},
	}

	svc := testutil.NewStubServices(t)
	plaintext := scopedTestBearerToken("scoped", "alpha")

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = testAuthStubForScopedBearer()
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
				return &core.OperationResult{Status: http.StatusOK, Body: []byte(`{"ok":true}`)}, nil
			},
		},
		ops: []core.Operation{{Name: "do_thing", Method: http.MethodGet}},
	}

	plaintext := scopedTestBearerToken("unscoped", "")

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = testAuthStubForScopedBearer()
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Services = testutil.NewStubServices(t)
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

func TestExecuteOperationIssuesProviderGatewayCallerToken(t *testing.T) {
	t.Parallel()

	stub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{N: "cli-provider", ConnMode: core.ConnectionModeNone},
		ops:             []core.Operation{{Name: "do_thing", Method: http.MethodGet}},
	}

	svc := testutil.NewStubServices(t)
	user := seedUser(t, svc, "cli-user@test.local")
	plaintext := scopedTestBearerToken(user.ID, "")
	invoker := &callerTokenRecordingInvoker{}
	now := time.Now().UTC()
	privateKeyPEM, publicKeyPEM := testProviderGatewayCallerTokenKeyPair(t)
	issuer, err := providergateway.NewCallerTokenIssuer(privateKeyPEM)
	if err != nil {
		t.Fatalf("NewCallerTokenIssuer: %v", err)
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = testAuthStubForScopedBearer()
		cfg.Invoker = invoker
		cfg.Now = func() time.Time { return now }
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.CallerTokenIssuer = issuer
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/cli-provider/do_thing", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	token := invoker.token()
	if token == "" {
		t.Fatalf("expected provider gateway caller token")
	}
	claims, err := providergateway.Verify(token, publicKeyPEM)
	if err != nil {
		t.Fatalf("Verify caller token: %v", err)
	}
	if claims.SubjectID != principal.UserSubjectID(user.ID) {
		t.Fatalf("SubjectID = %q, want %q", claims.SubjectID, principal.UserSubjectID(user.ID))
	}
	if claims.IssuedAt != now.Unix() {
		t.Fatalf("IssuedAt = %d, want %d", claims.IssuedAt, now.Unix())
	}
	if claims.ExpiresAt != now.Add(5*time.Minute).Unix() {
		t.Fatalf("ExpiresAt = %d, want %d", claims.ExpiresAt, now.Add(5*time.Minute).Unix())
	}
}

func testProviderGatewayCallerTokenKeyPair(t testing.TB) (string, string) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	privateKeyBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey: %v", err)
	}
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyBytes})
	publicKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicKeyBytes})
	return string(privateKeyPEM), string(publicKeyPEM)
}

func TestCreateAPIToken_InvalidScope(t *testing.T) {
	t.Parallel()

	stub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{N: "real-provider"},
		ops:             []core.Operation{{Name: "op", Method: http.MethodGet}},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		configureGrantTestAuth(cfg)
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Services = testutil.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"name":"test-token","scopes":"nonexistent"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/tokens", body)
	req.Header.Set("Content-Type", "application/json")
	addGrantTestSessionCookie(req)
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
