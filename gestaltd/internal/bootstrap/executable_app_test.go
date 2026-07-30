package bootstrap

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	s3sdk "github.com/valon-technologies/gestalt/sdk/go/s3"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	corecache "github.com/valon-technologies/gestalt/server/core/cache"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/indexeddbcodec"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	appservice "github.com/valon-technologies/gestalt/server/services/apps"
	graphqlschema "github.com/valon-technologies/gestalt/server/services/apps/graphql"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/egressproxy"
	"github.com/valon-technologies/gestalt/server/services/hostserviceingress"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	telemetrynoop "github.com/valon-technologies/gestalt/server/services/observability/drivers/noop"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/runtimeprovider"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gopkg.in/yaml.v3"
)

type invokePluginEnvelope struct {
	OK                     bool               `json:"ok"`
	TargetApp              string             `json:"target_app"`
	TargetOperation        string             `json:"target_operation"`
	UsedConnectionOverride bool               `json:"used_connection_override"`
	Status                 int                `json:"status"`
	Body                   requestContextBody `json:"body"`
	Error                  string             `json:"error"`
}

type requestContextBody struct {
	Subject struct {
		ID          string `json:"id"`
		Kind        string `json:"kind"`
		DisplayName string `json:"display_name"`
		AuthSource  string `json:"auth_source"`
	} `json:"subject"`
	Credential struct {
		Mode       string `json:"mode"`
		SubjectID  string `json:"subject_id"`
		Connection string `json:"connection"`
		Instance   string `json:"instance"`
	} `json:"credential"`
	Access struct {
		Policy string `json:"policy"`
		Role   string `json:"role"`
	} `json:"access"`
}

type nestedInvokeHarness struct {
	invoker  invocation.Invoker
	services *coredata.Services
}

type allowAllAuthorizationProvider struct{}

func newAllowAllAuthorizationProvider() core.AuthorizationProvider {
	return &allowAllAuthorizationProvider{}
}

func (p *allowAllAuthorizationProvider) CheckAccess(context.Context, *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	return &proto.CheckAccessResponse{Allowed: true}, nil
}

func (p *allowAllAuthorizationProvider) CheckAccessMany(ctx context.Context, req *proto.CheckAccessManyRequest) (*proto.CheckAccessManyResponse, error) {
	resp := &proto.CheckAccessManyResponse{Decisions: make([]*proto.CheckAccessResponse, 0, len(req.GetRequests()))}
	for range req.GetRequests() {
		decision, err := p.CheckAccess(ctx, nil)
		if err != nil {
			return nil, err
		}
		resp.Decisions = append(resp.Decisions, decision)
	}
	return resp, nil
}

func (p *allowAllAuthorizationProvider) ListRelationships(context.Context, *proto.ListRelationshipsRequest) (*proto.ListRelationshipsResponse, error) {
	return &proto.ListRelationshipsResponse{}, nil
}
func (p *allowAllAuthorizationProvider) AddRelationship(context.Context, *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error) {
	return &proto.AddRelationshipResponse{}, nil
}
func (p *allowAllAuthorizationProvider) DeleteRelationship(context.Context, *proto.DeleteRelationshipRequest) (*proto.DeleteRelationshipResponse, error) {
	return &proto.DeleteRelationshipResponse{}, nil
}
func (p *allowAllAuthorizationProvider) SetAuthorizationState(context.Context, *proto.SetAuthorizationStateRequest) (*proto.SetAuthorizationStateResponse, error) {
	return &proto.SetAuthorizationStateResponse{}, nil
}
func (p *allowAllAuthorizationProvider) GetActiveModelRef(context.Context) (*proto.GetActiveModelRefResponse, error) {
	return &proto.GetActiveModelRefResponse{}, nil
}
func (p *allowAllAuthorizationProvider) SetActiveModel(context.Context, *proto.SetActiveModelRequest) (*proto.SetActiveModelResponse, error) {
	return &proto.SetActiveModelResponse{}, nil
}
func (p *allowAllAuthorizationProvider) ListActiveModelResourceTypes(context.Context, *proto.ListActiveModelResourceTypesRequest) (*proto.ListActiveModelResourceTypesResponse, error) {
	return &proto.ListActiveModelResourceTypesResponse{}, nil
}
func (p *allowAllAuthorizationProvider) Ping(context.Context) error { return nil }
func (p *allowAllAuthorizationProvider) Close() error               { return nil }

type capturingRuntime struct {
	provider *runtimeprovider.LocalProvider

	mu                  sync.Mutex
	cond                *sync.Cond
	startRequests       []*proto.StartRuntimeSessionRequest
	startAppRequests    []*proto.StartHostedAppRequest
	startTimes          []time.Time
	getSessionRequests  chan *proto.GetRuntimeSessionRequest
	getSessionCalls     int
	sessionLifecycles   map[string]*proto.RuntimeSessionLifecycle
	lifecycleForSession func(index int) *proto.RuntimeSessionLifecycle
	startErrForSession  func(index int) error
	now                 func() time.Time
	stopCount           atomic.Int32
}

func newCapturingRuntime() *capturingRuntime {
	r := &capturingRuntime{provider: runtimeprovider.NewLocalProvider()}
	r.cond = sync.NewCond(&r.mu)
	return r
}

func (r *capturingRuntime) Support(ctx context.Context) (*proto.RuntimeSupport, error) {
	return r.provider.Support(ctx)
}

func (r *capturingRuntime) StartSession(ctx context.Context, req *proto.StartRuntimeSessionRequest) (*proto.RuntimeSession, error) {
	r.mu.Lock()
	r.startRequests = append(r.startRequests, cloneStartRuntimeSessionRequest(req))
	now := time.Now
	if r.now != nil {
		now = r.now
	}
	r.startTimes = append(r.startTimes, now().UTC())
	index := len(r.startRequests)
	lifecycleForSession := r.lifecycleForSession
	startErrForSession := r.startErrForSession
	r.cond.Broadcast()
	r.mu.Unlock()
	if startErrForSession != nil {
		if err := startErrForSession(index); err != nil {
			return nil, err
		}
	}
	session, err := r.provider.StartSession(ctx, req)
	if err != nil {
		return nil, err
	}
	if lifecycleForSession != nil {
		session.Lifecycle = cloneRuntimeSessionLifecycle(lifecycleForSession(index))
		r.mu.Lock()
		if r.sessionLifecycles == nil {
			r.sessionLifecycles = map[string]*proto.RuntimeSessionLifecycle{}
		}
		r.sessionLifecycles[session.GetId()] = cloneRuntimeSessionLifecycle(session.GetLifecycle())
		r.mu.Unlock()
	}
	return session, nil
}

func (r *capturingRuntime) ListSessions(ctx context.Context, req *proto.ListRuntimeSessionsRequest) (*proto.ListRuntimeSessionsResponse, error) {
	sessions, err := r.provider.ListSessions(ctx, req)
	if err != nil {
		return nil, err
	}
	for _, session := range sessions.GetSessions() {
		r.attachSessionLifecycle(session)
	}
	return sessions, nil
}

func (r *capturingRuntime) GetSession(ctx context.Context, req *proto.GetRuntimeSessionRequest) (*proto.RuntimeSession, error) {
	r.mu.Lock()
	r.getSessionCalls++
	r.cond.Broadcast()
	r.mu.Unlock()

	session, err := r.provider.GetSession(ctx, req)
	if err != nil {
		return nil, err
	}
	r.attachSessionLifecycle(session)
	if r.getSessionRequests != nil {
		select {
		case r.getSessionRequests <- req:
		default:
		}
	}
	return session, nil
}

func (r *capturingRuntime) StopSession(ctx context.Context, req *proto.StopRuntimeSessionRequest) error {
	r.stopCount.Add(1)
	return r.provider.StopSession(ctx, req)
}

func (r *capturingRuntime) StartApp(ctx context.Context, req *proto.StartHostedAppRequest) (*proto.HostedApp, error) {
	r.mu.Lock()
	r.startAppRequests = append(r.startAppRequests, cloneStartHostedAppRequest(req))
	r.mu.Unlock()
	return r.provider.StartApp(ctx, req)
}

func (r *capturingRuntime) Close() error {
	return r.provider.Close()
}

func (r *capturingRuntime) startSessionRequests() []*proto.StartRuntimeSessionRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*proto.StartRuntimeSessionRequest, len(r.startRequests))
	for i, req := range r.startRequests {
		out[i] = cloneStartRuntimeSessionRequest(req)
	}
	return out
}

func (r *capturingRuntime) waitStartSessionRequests(t *testing.T, count int) {
	t.Helper()
	waitOnTestCond(t, r.cond, fmt.Sprintf("%d start session requests", count), func() bool {
		return len(r.startRequests) >= count
	})
}

func waitOnTestCond(t *testing.T, cond *sync.Cond, description string, ready func() bool) {
	t.Helper()
	if cond == nil {
		t.Fatalf("condition variable unavailable while waiting for %s", description)
	}
	timeout := 30 * time.Second
	if deadline, ok := t.Deadline(); ok {
		if untilDeadline := time.Until(deadline) - 100*time.Millisecond; untilDeadline < timeout {
			timeout = untilDeadline
		}
	}
	if timeout <= 0 {
		t.Fatalf("timed out waiting for %s", description)
	}
	timedOut := false
	timer := time.AfterFunc(timeout, func() {
		cond.L.Lock()
		timedOut = true
		cond.Broadcast()
		cond.L.Unlock()
	})
	defer timer.Stop()

	cond.L.Lock()
	defer cond.L.Unlock()
	for !ready() {
		if timedOut {
			t.Fatalf("timed out waiting for %s", description)
		}
		cond.Wait()
	}
}

func (r *capturingRuntime) getSessionCallCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.getSessionCalls
}

func (r *capturingRuntime) waitGetSessionCalls(t *testing.T, count int) {
	t.Helper()
	waitOnTestCond(t, r.cond, fmt.Sprintf("%d get session calls", count), func() bool {
		return r.getSessionCalls >= count
	})
}

func (r *capturingRuntime) startSessionTimes() []time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Time(nil), r.startTimes...)
}

func (r *capturingRuntime) startAppRequestsCopy() []*proto.StartHostedAppRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*proto.StartHostedAppRequest, len(r.startAppRequests))
	for i, req := range r.startAppRequests {
		out[i] = cloneStartHostedAppRequest(req)
	}
	return out
}

func (r *capturingRuntime) attachSessionLifecycle(session *proto.RuntimeSession) {
	if session == nil || session.GetId() == "" {
		return
	}
	r.mu.Lock()
	lifecycle := cloneRuntimeSessionLifecycle(r.sessionLifecycles[session.GetId()])
	r.mu.Unlock()
	session.Lifecycle = lifecycle
}

func cloneRuntimeSessionLifecycle(lifecycle *proto.RuntimeSessionLifecycle) *proto.RuntimeSessionLifecycle {
	if lifecycle == nil {
		return nil
	}
	return gproto.Clone(lifecycle).(*proto.RuntimeSessionLifecycle)
}

type capturingBundleRuntime struct {
	provider   *runtimeprovider.LocalProvider
	support    *proto.RuntimeSupport
	fakeHosted bool

	mu                sync.Mutex
	startAppRequests  []*proto.StartHostedAppRequest
	sessionLifecycles map[string]*proto.RuntimeSessionLifecycle
	fakeApps          map[string]*fakeHostedAppServer
}

type fakeHostedAppServer struct {
	dir      string
	listener net.Listener
	server   *grpc.Server
}

func newCapturingBundleRuntime() *capturingBundleRuntime {
	return &capturingBundleRuntime{
		provider: runtimeprovider.NewLocalProvider(),
		support: &proto.RuntimeSupport{
			CanHostApps: true,
		},
		fakeApps: make(map[string]*fakeHostedAppServer),
	}
}

func (r *capturingBundleRuntime) Support(context.Context) (*proto.RuntimeSupport, error) {
	return r.support, nil
}

func (r *capturingBundleRuntime) StartSession(ctx context.Context, req *proto.StartRuntimeSessionRequest) (*proto.RuntimeSession, error) {
	session, err := r.provider.StartSession(ctx, req)
	if err != nil {
		return nil, err
	}
	r.attachSessionLifecycle(session)
	return session, nil
}

func (r *capturingBundleRuntime) ListSessions(ctx context.Context, req *proto.ListRuntimeSessionsRequest) (*proto.ListRuntimeSessionsResponse, error) {
	sessions, err := r.provider.ListSessions(ctx, req)
	if err != nil {
		return nil, err
	}
	for _, session := range sessions.GetSessions() {
		r.attachSessionLifecycle(session)
	}
	return sessions, nil
}

func (r *capturingBundleRuntime) GetSession(ctx context.Context, req *proto.GetRuntimeSessionRequest) (*proto.RuntimeSession, error) {
	session, err := r.provider.GetSession(ctx, req)
	if err != nil {
		return nil, err
	}
	r.attachSessionLifecycle(session)
	return session, nil
}

func (r *capturingBundleRuntime) StopSession(ctx context.Context, req *proto.StopRuntimeSessionRequest) error {
	r.cleanupFakeHostedApp(req.GetSessionId())
	return r.provider.StopSession(ctx, req)
}

func (r *capturingBundleRuntime) StartApp(ctx context.Context, req *proto.StartHostedAppRequest) (*proto.HostedApp, error) {
	r.mu.Lock()
	r.startAppRequests = append(r.startAppRequests, cloneStartHostedAppRequest(req))
	r.mu.Unlock()

	if r.fakeHosted {
		return r.startFakeHostedApp(req)
	}
	return r.provider.StartApp(ctx, req)
}

func (r *capturingBundleRuntime) Close() error {
	r.mu.Lock()
	sessionIDs := make([]string, 0, len(r.fakeApps))
	for sessionID := range r.fakeApps {
		sessionIDs = append(sessionIDs, sessionID)
	}
	r.mu.Unlock()
	for _, sessionID := range sessionIDs {
		r.cleanupFakeHostedApp(sessionID)
	}
	return r.provider.Close()
}

func (r *capturingBundleRuntime) startFakeHostedApp(req *proto.StartHostedAppRequest) (*proto.HostedApp, error) {
	env := cloneRuntimeMetadata(req.GetEnv())
	dir, err := appservice.NewPluginTempDir("gstp-fake-")
	if err != nil {
		return nil, fmt.Errorf("create fake hosted app dir: %w", err)
	}
	socketPath := filepath.Join(dir, "app.sock")
	lis, err := net.Listen("unix", socketPath)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("listen for fake hosted app: %w", err)
	}

	srv := grpc.NewServer()
	proto.RegisterAppProviderServer(srv, appservice.NewServer(&coretesting.StubIntegration{
		N:        req.GetAppName(),
		DN:       "Fake Hosted Plugin",
		Desc:     "test-only fake hosted app",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{
			Name: req.GetAppName(),
			Operations: []catalog.CatalogOperation{
				{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
				{
					ID:     "indexeddb_roundtrip",
					Method: http.MethodPost,
					Parameters: []catalog.CatalogParameter{
						{Name: "binding", Type: "string"},
						{Name: "store", Type: "string", Required: true},
						{Name: "id", Type: "string", Required: true},
						{Name: "value", Type: "string", Required: true},
					},
				},
				{
					ID:     "cache_roundtrip",
					Method: http.MethodPost,
					Parameters: []catalog.CatalogParameter{
						{Name: "binding", Type: "string"},
						{Name: "key", Type: "string", Required: true},
						{Name: "value", Type: "string", Required: true},
					},
				},
				{
					ID:     "s3_roundtrip",
					Method: http.MethodPost,
					Parameters: []catalog.CatalogParameter{
						{Name: "binding", Type: "string"},
						{Name: "key", Type: "string", Required: true},
						{Name: "value", Type: "string", Required: true},
					},
				},
				{
					ID:     "invoke_plugin",
					Method: http.MethodPost,
					Parameters: []catalog.CatalogParameter{
						{Name: "app", Type: "string", Required: true},
						{Name: "operation", Type: "string", Required: true},
					},
				},
				{
					ID:     "workflow_manager_roundtrip",
					Method: http.MethodPost,
				},
				{
					ID:     "agent_manager_roundtrip",
					Method: http.MethodPost,
				},
				{
					ID:     "make_http_request",
					Method: http.MethodGet,
					Parameters: []catalog.CatalogParameter{
						{Name: "url", Type: "string", Required: true},
					},
				},
			},
		},
		ExecuteFn: func(ctx context.Context, operation string, params map[string]any, _ string) (*core.OperationResult, error) {
			switch operation {
			case "read_env":
				name, _ := params["name"].(string)
				value, found := env[name]
				body, err := json.Marshal(map[string]any{
					"value": value,
					"found": found,
				})
				if err != nil {
					return nil, err
				}
				return &core.OperationResult{Status: http.StatusOK, Body: body}, nil
			case "indexeddb_roundtrip":
				store, _ := params["store"].(string)
				id, _ := params["id"].(string)
				value, _ := params["value"].(string)
				binding, _ := params["binding"].(string)
				record, err := fakeHostedIndexedDBRoundTrip(store, id, value, binding, env)
				if err != nil {
					return nil, err
				}
				body, err := json.Marshal(record)
				if err != nil {
					return nil, err
				}
				return &core.OperationResult{Status: http.StatusOK, Body: body}, nil
			case "cache_roundtrip":
				key, _ := params["key"].(string)
				value, _ := params["value"].(string)
				binding, _ := params["binding"].(string)
				record, err := fakeHostedCacheRoundTrip(key, value, binding, env)
				if err != nil {
					return nil, err
				}
				body, err := json.Marshal(record)
				if err != nil {
					return nil, err
				}
				return &core.OperationResult{Status: http.StatusOK, Body: body}, nil
			case "s3_roundtrip":
				key, _ := params["key"].(string)
				value, _ := params["value"].(string)
				binding, _ := params["binding"].(string)
				record, err := fakeHostedS3RoundTrip(key, value, binding, env)
				if err != nil {
					return nil, err
				}
				body, err := json.Marshal(record)
				if err != nil {
					return nil, err
				}
				return &core.OperationResult{Status: http.StatusOK, Body: body}, nil
			case "invoke_plugin":
				targetApp, _ := params["app"].(string)
				targetOperation, _ := params["operation"].(string)
				envelope, err := fakeHostedInvokePlugin(ctx, targetApp, targetOperation, env)
				if err != nil {
					envelope = invokePluginEnvelope{
						OK:              false,
						TargetApp:       targetApp,
						TargetOperation: targetOperation,
						Error:           err.Error(),
					}
				}
				body, err := json.Marshal(envelope)
				if err != nil {
					return nil, err
				}
				return &core.OperationResult{Status: http.StatusOK, Body: body}, nil
			case "workflow_manager_roundtrip":
				record, err := fakeHostedWorkflowManagerRoundTrip(fakeHostedAppRequestContext(ctx), env)
				if err != nil {
					return nil, err
				}
				body, err := json.Marshal(record)
				if err != nil {
					return nil, err
				}
				return &core.OperationResult{Status: http.StatusOK, Body: body}, nil
			case "agent_manager_roundtrip":
				record, err := fakeHostedAgentManagerRoundTrip(fakeHostedAppRequestContext(ctx), env)
				if err != nil {
					return nil, err
				}
				body, err := json.Marshal(record)
				if err != nil {
					return nil, err
				}
				return &core.OperationResult{Status: http.StatusOK, Body: body}, nil
			case "make_http_request":
				targetURL, _ := params["url"].(string)
				record, err := fakeHostedMakeHTTPRequest(targetURL, env)
				if err != nil {
					return nil, err
				}
				body, err := json.Marshal(record)
				if err != nil {
					return nil, err
				}
				return &core.OperationResult{Status: http.StatusOK, Body: body}, nil
			default:
				return nil, fmt.Errorf("unknown operation %q", operation)
			}
		},
	}))
	go func() {
		_ = srv.Serve(lis)
	}()

	r.mu.Lock()
	r.fakeApps[req.GetSessionId()] = &fakeHostedAppServer{
		dir:      dir,
		listener: lis,
		server:   srv,
	}
	r.mu.Unlock()
	return &proto.HostedApp{
		Id:         "fake-" + req.GetSessionId(),
		SessionId:  req.GetSessionId(),
		AppName:    req.GetAppName(),
		DialTarget: "unix://" + socketPath,
	}, nil
}

func (r *capturingBundleRuntime) cleanupFakeHostedApp(sessionID string) {
	r.mu.Lock()
	fake := r.fakeApps[sessionID]
	delete(r.fakeApps, sessionID)
	r.mu.Unlock()
	if fake == nil {
		return
	}
	fake.server.Stop()
	_ = fake.listener.Close()
	_ = os.RemoveAll(fake.dir)
}

func fakeHostedHostServiceRelay(serviceName string, env map[string]string) (string, string, error) {
	target := strings.TrimSpace(env[runtimehost.HostServiceSocketEnv])
	if target == "" {
		return "", "", fmt.Errorf("missing %s relay target in %s", serviceName, runtimehost.HostServiceSocketEnv)
	}
	token := strings.TrimSpace(env[runtimehost.HostServiceTokenEnv])
	if token == "" {
		return "", "", fmt.Errorf("missing %s relay token in %s", serviceName, runtimehost.HostServiceTokenEnv)
	}
	address := strings.TrimSpace(strings.TrimPrefix(target, "tls://"))
	if address == "" || address == target {
		return "", "", fmt.Errorf("unsupported %s relay target %q", serviceName, target)
	}
	return address, token, nil
}

func fakeHostedHostServiceContext(token, binding string) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	pairs := []string{runtimehost.HostServiceRelayTokenHeader, token}
	if binding = strings.TrimSpace(binding); binding != "" {
		pairs = append(pairs, runtimehost.HostServiceBindingHeader, binding)
	}
	return metadata.NewOutgoingContext(ctx, metadata.Pairs(pairs...)), cancel
}

func fakeHostedIndexedDBRoundTrip(store, id, value, binding string, env map[string]string) (map[string]any, error) {
	address, token, err := fakeHostedHostServiceRelay("indexeddb", env)
	if err != nil {
		return nil, err
	}

	conn, err := grpc.NewClient(
		address,
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			InsecureSkipVerify: true,
			MinVersion:         tls.VersionTLS12,
			NextProtos:         []string{"h2"},
		})),
	)
	if err != nil {
		return nil, fmt.Errorf("connect indexeddb relay: %w", err)
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := fakeHostedHostServiceContext(token, binding)
	defer cancel()

	client := proto.NewIndexedDBClient(conn)
	if _, err := client.CreateObjectStore(ctx, &proto.CreateObjectStoreRequest{Name: store}); err != nil {
		return nil, fmt.Errorf("create object store: %w", err)
	}
	recordValue, err := indexeddbcodec.RecordToProto(indexeddbcodec.Record{"id": id, "value": value})
	if err != nil {
		return nil, fmt.Errorf("encode record: %w", err)
	}
	if _, err := client.Put(ctx, &proto.RecordRequest{Store: store, Record: recordValue}); err != nil {
		return nil, fmt.Errorf("put record: %w", err)
	}
	resp, err := client.Get(ctx, &proto.ObjectStoreRequest{Store: store, Id: id})
	if err != nil {
		return nil, fmt.Errorf("get record: %w", err)
	}
	record, err := indexeddbcodec.RecordFromProto(resp.GetRecord())
	if err != nil {
		return nil, fmt.Errorf("decode record: %w", err)
	}
	return record, nil
}

func fakeHostedCacheRoundTrip(key, value, binding string, env map[string]string) (map[string]any, error) {
	address, token, err := fakeHostedHostServiceRelay("cache", env)
	if err != nil {
		return nil, err
	}

	conn, err := grpc.NewClient(
		address,
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			InsecureSkipVerify: true,
			MinVersion:         tls.VersionTLS12,
			NextProtos:         []string{"h2"},
		})),
	)
	if err != nil {
		return nil, fmt.Errorf("connect cache relay: %w", err)
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := fakeHostedHostServiceContext(token, binding)
	defer cancel()

	client := proto.NewCacheClient(conn)
	if _, err := client.Set(ctx, &proto.CacheSetRequest{
		Key:   key,
		Value: []byte(value),
	}); err != nil {
		return nil, fmt.Errorf("set cache key: %w", err)
	}
	resp, err := client.Get(ctx, &proto.CacheGetRequest{Key: key})
	if err != nil {
		return nil, fmt.Errorf("get cache key: %w", err)
	}
	return map[string]any{
		"found": resp.GetFound(),
		"value": string(resp.GetValue()),
	}, nil
}

func fakeHostedMakeHTTPRequest(targetURL string, env map[string]string) (map[string]any, error) {
	client := &http.Client{}
	if proxyURL := strings.TrimSpace(env["HTTP_PROXY"]); proxyURL != "" {
		parsed, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("parse HTTP_PROXY: %w", err)
		}
		client.Transport = &http.Transport{
			Proxy:           http.ProxyURL(parsed),
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12},
		}
	}
	resp, err := client.Get(targetURL)
	if err != nil {
		return nil, fmt.Errorf("get via proxy: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	return map[string]any{
		"status": resp.StatusCode,
		"body":   string(body),
	}, nil
}

func fakeHostedS3RoundTrip(key, value, binding string, env map[string]string) (map[string]any, error) {
	address, token, err := fakeHostedHostServiceRelay("s3", env)
	if err != nil {
		return nil, err
	}

	conn, err := grpc.NewClient(
		address,
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			InsecureSkipVerify: true,
			MinVersion:         tls.VersionTLS12,
			NextProtos:         []string{"h2"},
		})),
	)
	if err != nil {
		return nil, fmt.Errorf("connect s3 relay: %w", err)
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := fakeHostedHostServiceContext(token, binding)
	defer cancel()

	client := proto.NewS3Client(conn)
	writeStream, err := client.WriteObject(ctx)
	if err != nil {
		return nil, fmt.Errorf("open s3 write stream: %w", err)
	}
	if err := writeStream.Send(&proto.WriteObjectRequest{
		Msg: &proto.WriteObjectRequest_Open{
			Open: &proto.WriteObjectOpen{
				Ref:         &proto.S3ObjectRef{Key: key},
				ContentType: "text/plain",
			},
		},
	}); err != nil {
		return nil, fmt.Errorf("send s3 open frame: %w", err)
	}
	if err := writeStream.Send(&proto.WriteObjectRequest{
		Msg: &proto.WriteObjectRequest_Data{Data: []byte(value)},
	}); err != nil {
		return nil, fmt.Errorf("send s3 data frame: %w", err)
	}
	writeResp, err := writeStream.CloseAndRecv()
	if err != nil {
		return nil, fmt.Errorf("close s3 write stream: %w", err)
	}

	headResp, err := client.HeadObject(ctx, &proto.HeadObjectRequest{
		Ref: &proto.S3ObjectRef{Key: key},
	})
	if err != nil {
		return nil, fmt.Errorf("head s3 object: %w", err)
	}

	readStream, err := client.ReadObject(ctx, &proto.ReadObjectRequest{
		Ref: &proto.S3ObjectRef{Key: key},
	})
	if err != nil {
		return nil, fmt.Errorf("open s3 read stream: %w", err)
	}
	first, err := readStream.Recv()
	if err != nil {
		return nil, fmt.Errorf("read s3 metadata frame: %w", err)
	}
	if first.GetMeta() == nil {
		return nil, fmt.Errorf("s3 read stream did not start with metadata")
	}
	var body bytes.Buffer
	for {
		chunk, err := readStream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read s3 data frame: %w", err)
		}
		if data := chunk.GetData(); len(data) > 0 {
			body.Write(data)
		}
	}

	listResp, err := client.ListObjects(ctx, &proto.ListObjectsRequest{
		Prefix: key,
	})
	if err != nil {
		return nil, fmt.Errorf("list s3 objects: %w", err)
	}
	keys := make([]string, 0, len(listResp.GetObjects()))
	for _, obj := range listResp.GetObjects() {
		keys = append(keys, obj.GetRef().GetKey())
	}

	return map[string]any{
		"body":  body.String(),
		"key":   key,
		"keys":  keys,
		"type":  headResp.GetMeta().GetContentType(),
		"size":  writeResp.GetMeta().GetSize(),
		"found": len(keys) > 0,
	}, nil
}

func fakeHostedWorkflowManagerRoundTrip(reqCtx *proto.RequestContext, env map[string]string) (map[string]any, error) {
	address, token, err := fakeHostedHostServiceRelay("workflow manager", env)
	if err != nil {
		return nil, err
	}

	conn, err := grpc.NewClient(
		address,
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			InsecureSkipVerify: true,
			MinVersion:         tls.VersionTLS12,
			NextProtos:         []string{"h2"},
		})),
	)
	if err != nil {
		return nil, fmt.Errorf("connect workflow manager relay: %w", err)
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := fakeHostedHostServiceContext(token, "")
	defer cancel()

	client := proto.NewWorkflowClient(conn)
	applied, err := client.ApplyDefinition(ctx, &proto.ApplyWorkflowProviderDefinitionRequest{
		Context:        reqCtx,
		Provider:       "basic",
		IdempotencyKey: "workflow-manager-roundtrip",
		Spec: &proto.WorkflowDefinitionSpec{
			Id:    "workflow-manager-roundtrip",
			RunAs: "service_account:echoext-workflow",
			Target: &proto.BoundWorkflowTarget{
				Steps: []*proto.WorkflowStep{{
					Id: "sync",
					Action: &proto.WorkflowStep_App{App: &proto.WorkflowStepAppCall{
						Name:      "roadmap",
						Operation: "sync",
					}},
				}},
			},
			Activations: []*proto.WorkflowActivation{{
				Id: "nightly",
				Trigger: &proto.WorkflowActivation_Schedule{Schedule: &proto.WorkflowScheduleActivation{
					Cron:     "*/5 * * * *",
					Timezone: "UTC",
				}},
			}},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("apply workflow definition: %w", err)
	}
	definitionID := strings.TrimSpace(applied.GetId())
	if definitionID == "" {
		return nil, fmt.Errorf("workflow manager apply did not return a definition id")
	}
	fetched, err := client.GetDefinition(ctx, &proto.GetWorkflowProviderDefinitionRequest{
		Context:      reqCtx,
		Provider:     "basic",
		DefinitionId: definitionID,
	})
	if err != nil {
		return nil, fmt.Errorf("get workflow definition: %w", err)
	}

	return map[string]any{
		"provider":      applied.GetProvider(),
		"definition_id": definitionID,
		"activation_id": fetched.GetActivations()[0].GetId(),
		"generation":    fetched.GetGeneration(),
		"operation":     fetched.GetTarget().GetSteps()[0].GetApp().GetOperation(),
	}, nil
}

func fakeHostedAgentManagerRoundTrip(reqCtx *proto.RequestContext, env map[string]string) (map[string]any, error) {
	address, token, err := fakeHostedHostServiceRelay("agent manager", env)
	if err != nil {
		return nil, err
	}

	conn, err := grpc.NewClient(
		address,
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			InsecureSkipVerify: true,
			MinVersion:         tls.VersionTLS12,
			NextProtos:         []string{"h2"},
		})),
	)
	if err != nil {
		return nil, fmt.Errorf("connect agent manager relay: %w", err)
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := fakeHostedHostServiceContext(token, "")
	defer cancel()

	client := proto.NewAgentClient(conn)
	session, err := client.CreateSession(ctx, &proto.CreateAgentProviderSessionRequest{
		Context:        reqCtx,
		ProviderName:   "managed",
		Model:          "gpt-test",
		ClientRef:      "plugin-session",
		IdempotencyKey: "plugin-agent-session",
		Tools: &proto.AgentToolConfig{Source: &proto.AgentToolConfig_Catalog{
			Catalog: &proto.AgentCatalogToolConfig{Refs: []*proto.AgentToolRef{{
				App:       "roadmap",
				Operation: "sync",
			}}},
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("create agent session: %w", err)
	}
	sessionID := strings.TrimSpace(session.GetId())
	if sessionID == "" {
		return nil, fmt.Errorf("agent manager create session did not return a session id")
	}

	turnMetadata, err := structpb.NewStruct(map[string]any{
		"requireInteraction": true,
	})
	if err != nil {
		return nil, fmt.Errorf("build agent turn metadata: %w", err)
	}

	turn, err := client.CreateTurn(ctx, &proto.CreateAgentProviderTurnRequest{
		ProviderName:   "managed",
		Context:        reqCtx,
		TimeoutSeconds: 1,
		SessionId:      sessionID,
		Model:          "gpt-test",
		IdempotencyKey: "plugin-agent-turn",
		Output: &proto.AgentOutput{
			Kind: &proto.AgentOutput_Text{
				Text: &proto.AgentTextOutput{},
			},
		},
		Metadata: turnMetadata,
		Messages: []*proto.AgentMessage{{
			Role: "user",
			Text: "sync it",
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("create agent turn: %w", err)
	}
	turnID := strings.TrimSpace(turn.GetId())
	if turnID == "" {
		return nil, fmt.Errorf("agent manager create turn did not return a turn id")
	}

	interactions, err := client.ListInteractions(ctx, &proto.ListAgentProviderInteractionsRequest{
		ProviderName: "managed",
		Context:      reqCtx,
		TurnId:       turnID,
	})
	if err != nil {
		return nil, fmt.Errorf("list agent interactions: %w", err)
	}
	if len(interactions.GetInteractions()) != 1 {
		return nil, fmt.Errorf("agent manager listed %d interactions, want 1", len(interactions.GetInteractions()))
	}
	interactionID := strings.TrimSpace(interactions.GetInteractions()[0].GetId())
	if interactionID == "" {
		return nil, fmt.Errorf("agent interaction did not return an interaction id")
	}

	resolution, err := structpb.NewStruct(map[string]any{
		"approved": true,
	})
	if err != nil {
		return nil, fmt.Errorf("build interaction resolution: %w", err)
	}
	resolved, err := client.ResolveInteraction(ctx, &proto.ResolveAgentProviderInteractionRequest{
		ProviderName:  "managed",
		Context:       reqCtx,
		TurnId:        turnID,
		InteractionId: interactionID,
		Resolution:    resolution,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve agent interaction: %w", err)
	}

	fetched, err := client.GetTurn(ctx, &proto.GetAgentProviderTurnRequest{
		ProviderName: "managed",
		Context:      reqCtx,
		TurnId:       turnID,
	})
	if err != nil {
		return nil, fmt.Errorf("get agent turn: %w", err)
	}

	events, err := client.ListTurnEvents(ctx, &proto.ListAgentProviderTurnEventsRequest{
		ProviderName: "managed",
		Context:      reqCtx,
		TurnId:       turnID,
		AfterSeq:     0,
		Limit:        10,
	})
	if err != nil {
		return nil, fmt.Errorf("list agent turn events: %w", err)
	}
	eventTypes := make([]string, 0, len(events.GetEvents()))
	for _, event := range events.GetEvents() {
		eventTypes = append(eventTypes, event.GetType())
	}

	return map[string]any{
		"provider_name":  session.GetProviderName(),
		"session_id":     sessionID,
		"turn_id":        turnID,
		"interaction_id": strings.TrimSpace(resolved.GetId()),
		"status":         fetched.GetStatus().String(),
		"event_types":    eventTypes,
	}, nil
}

func fakeHostedInvokePlugin(providerCtx context.Context, targetApp, targetOperation string, env map[string]string) (invokePluginEnvelope, error) {
	envelope := invokePluginEnvelope{
		OK:              false,
		TargetApp:       targetApp,
		TargetOperation: targetOperation,
	}
	address, token, err := fakeHostedHostServiceRelay("plugin invoker", env)
	if err != nil {
		return envelope, err
	}

	conn, err := grpc.NewClient(
		address,
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			InsecureSkipVerify: true,
			MinVersion:         tls.VersionTLS12,
			NextProtos:         []string{"h2"},
		})),
	)
	if err != nil {
		return envelope, fmt.Errorf("connect app invoker relay: %w", err)
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := fakeHostedHostServiceContext(token, "")
	defer cancel()

	resp, err := proto.NewAppClient(conn).Invoke(ctx, &proto.AppInvokeRequest{
		Context:   fakeHostedAppRequestContext(providerCtx),
		App:       targetApp,
		Operation: targetOperation,
	})
	if err != nil {
		return envelope, err
	}

	envelope.OK = true
	envelope.Status = int(resp.GetStatus())
	if err := json.Unmarshal(resp.GetBody(), &envelope.Body); err != nil {
		return envelope, fmt.Errorf("decode nested invoke body: %w", err)
	}
	return envelope, nil
}

func fakeHostedAppRequestContext(ctx context.Context) *proto.RequestContext {
	caller := invocation.CallerProviderFromContext(ctx)
	out := &proto.RequestContext{}
	if caller.Kind != "" && caller.Name != "" {
		out.Caller = &proto.ProviderContext{
			Kind: string(caller.Kind),
			Name: caller.Name,
		}
	}
	if p := principal.FromContext(ctx); p != nil {
		p = principal.Canonicalized(p)
		out.Subject = &proto.SubjectContext{
			Id: p.SubjectID,
		}
	}
	if cred := invocation.CredentialContextFromContext(ctx); cred != (invocation.CredentialContext{}) {
		out.Credential = &proto.CredentialContext{
			Mode:       string(cred.Mode),
			SubjectId:  cred.SubjectID,
			Connection: cred.Connection,
			Instance:   cred.Instance,
		}
	}
	if meta := invocation.MetaFromContext(ctx); meta != nil {
		out.Invocation = &proto.InvocationContext{
			RequestId: meta.RequestID,
			Depth:     int32(meta.Depth),
			CallChain: append([]string(nil), meta.CallChain...),
		}
	}
	if out.Invocation == nil {
		out.Invocation = &proto.InvocationContext{}
	}
	out.Invocation.Connection = invocation.ConnectionFromContext(ctx)
	if access := invocation.AccessContextFromContext(ctx); access != (invocation.AccessContext{}) {
		out.Access = &proto.AccessContext{
			Policy: access.Policy,
			Role:   access.Role,
		}
	}
	if out.Caller == nil && out.Subject == nil && out.Credential == nil && out.Invocation.GetConnection() == "" && out.Access == nil {
		return nil
	}
	return out
}

func (r *capturingBundleRuntime) startAppRequestsCopy() []*proto.StartHostedAppRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*proto.StartHostedAppRequest, len(r.startAppRequests))
	for i, req := range r.startAppRequests {
		out[i] = cloneStartHostedAppRequest(req)
	}
	return out
}

func (r *capturingBundleRuntime) setSessionLifecycle(sessionID string, lifecycle *proto.RuntimeSessionLifecycle) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sessionLifecycles == nil {
		r.sessionLifecycles = make(map[string]*proto.RuntimeSessionLifecycle)
	}
	r.sessionLifecycles[sessionID] = cloneRuntimeSessionLifecycle(lifecycle)
}

func (r *capturingBundleRuntime) attachSessionLifecycle(session *proto.RuntimeSession) {
	if session == nil || session.GetId() == "" {
		return
	}
	r.mu.Lock()
	lifecycle := cloneRuntimeSessionLifecycle(r.sessionLifecycles[session.GetId()])
	r.mu.Unlock()
	session.Lifecycle = lifecycle
}

func cloneRuntimeMetadata(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func cloneStartHostedAppRequest(req *proto.StartHostedAppRequest) *proto.StartHostedAppRequest {
	if req == nil {
		return nil
	}
	return gproto.Clone(req).(*proto.StartHostedAppRequest)
}

func cloneStartRuntimeSessionRequest(req *proto.StartRuntimeSessionRequest) *proto.StartRuntimeSessionRequest {
	if req == nil {
		return nil
	}
	return gproto.Clone(req).(*proto.StartRuntimeSessionRequest)
}

func assertStartAppEgressPolicy(t *testing.T, req *proto.StartHostedAppRequest, allowedHosts []string, action egress.PolicyAction) {
	t.Helper()
	if got := req.GetAllowedHosts(); !slices.Equal(got, allowedHosts) {
		t.Fatalf("StartApp egress allowed hosts = %#v, want %#v", got, allowedHosts)
	}
	if got := req.GetDefaultAction(); got != string(action) {
		t.Fatalf("StartApp egress default action = %q, want %q", got, action)
	}
}

func assertStartAppRelayEnv(t *testing.T, req *proto.StartHostedAppRequest, relayContext string) {
	t.Helper()
	if got := req.GetEnv()[runtimehost.HostServiceSocketEnv]; !strings.HasPrefix(got, "tls://") {
		t.Fatalf("StartApp env %s = %q, want tls:// public relay target for %s", runtimehost.HostServiceSocketEnv, got, relayContext)
	}
	if got := req.GetEnv()[runtimehost.HostServiceTokenEnv]; strings.TrimSpace(got) == "" {
		t.Fatalf("StartApp env missing non-empty %s for %s", runtimehost.HostServiceTokenEnv, relayContext)
	}
}

type blockingStopRuntime struct {
	runtimeprovider.Provider
	stopCount atomic.Int32
}

func (r *blockingStopRuntime) StopSession(ctx context.Context, req *proto.StopRuntimeSessionRequest) error {
	r.stopCount.Add(1)
	<-ctx.Done()
	return ctx.Err()
}

type staticCapabilityRuntime struct {
	inner   runtimeprovider.Provider
	support *proto.RuntimeSupport
}

func (r *staticCapabilityRuntime) Support(context.Context) (*proto.RuntimeSupport, error) {
	return r.support, nil
}

func (r *staticCapabilityRuntime) StartSession(ctx context.Context, req *proto.StartRuntimeSessionRequest) (*proto.RuntimeSession, error) {
	return r.inner.StartSession(ctx, req)
}

func (r *staticCapabilityRuntime) ListSessions(ctx context.Context, req *proto.ListRuntimeSessionsRequest) (*proto.ListRuntimeSessionsResponse, error) {
	return r.inner.ListSessions(ctx, req)
}

func (r *staticCapabilityRuntime) GetSession(ctx context.Context, req *proto.GetRuntimeSessionRequest) (*proto.RuntimeSession, error) {
	return r.inner.GetSession(ctx, req)
}

func (r *staticCapabilityRuntime) StopSession(ctx context.Context, req *proto.StopRuntimeSessionRequest) error {
	return r.inner.StopSession(ctx, req)
}

func (r *staticCapabilityRuntime) StartApp(ctx context.Context, req *proto.StartHostedAppRequest) (*proto.HostedApp, error) {
	return r.inner.StartApp(ctx, req)
}

func (r *staticCapabilityRuntime) Close() error {
	return r.inner.Close()
}

type stubWorkflowManager struct {
	mu                     sync.Mutex
	subjects               []string
	nextDefinitionID       int
	nextRunID              int
	definitions            map[string]*workflowmanager.ManagedDefinition
	runs                   map[string]*workflowmanager.ManagedRun
	publishedEvents        []coreworkflow.Event
	publishedProviderNames []string
	definitionKeys         []string
}

func newStubWorkflowManager() *stubWorkflowManager {
	return &stubWorkflowManager{
		definitions: make(map[string]*workflowmanager.ManagedDefinition),
		runs:        make(map[string]*workflowmanager.ManagedRun),
	}
}

func (m *stubWorkflowManager) ApplyDefinition(_ context.Context, p *principal.Principal, req workflowmanager.DefinitionApply) (*workflowmanager.ManagedDefinition, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := strings.TrimSpace(req.Spec.ID)
	if id == "" {
		m.nextDefinitionID++
		id = fmt.Sprintf("def-%d", m.nextDefinitionID)
	}
	now := time.Now().UTC().Truncate(time.Second)
	m.definitionKeys = append(m.definitionKeys, strings.TrimSpace(req.IdempotencyKey))
	value := &workflowmanager.ManagedDefinition{
		ProviderName: defaultWorkflowProviderName(req.ProviderName),
		Definition: &coreworkflow.Definition{
			ID:          id,
			Generation:  1,
			Target:      cloneWorkflowTarget(req.Spec.Target),
			Activations: append([]coreworkflow.Activation(nil), req.Spec.Activations...),
			Paused:      req.Spec.Paused,
			CreatedBy:   subjectIDOf(p),
			CreatedAt:   &now,
			UpdatedAt:   &now,
		},
	}
	m.definitions[id] = value
	m.subjects = append(m.subjects, subjectIDOf(p))
	return cloneManagedDefinition(value), nil
}

func (m *stubWorkflowManager) GetDefinition(_ context.Context, p *principal.Principal, _ string, definitionID string) (*workflowmanager.ManagedDefinition, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subjects = append(m.subjects, subjectIDOf(p))
	value, ok := m.definitions[definitionID]
	if !ok {
		return nil, core.ErrNotFound
	}
	return cloneManagedDefinition(value), nil
}

func (m *stubWorkflowManager) ListDefinitions(context.Context, *principal.Principal, string) (*workflowmanager.ListDefinitionsResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*workflowmanager.ManagedDefinition, 0, len(m.definitions))
	for _, item := range m.definitions {
		out = append(out, cloneManagedDefinition(item))
	}
	return &workflowmanager.ListDefinitionsResponse{Definitions: out}, nil
}

func (m *stubWorkflowManager) SetDefinitionPaused(_ context.Context, p *principal.Principal, _ string, definitionID string, paused bool) (*workflowmanager.ManagedDefinition, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subjects = append(m.subjects, subjectIDOf(p))
	value, ok := m.definitions[definitionID]
	if !ok {
		return nil, core.ErrNotFound
	}
	value.Definition.Paused = paused
	return cloneManagedDefinition(value), nil
}

func (m *stubWorkflowManager) SetActivationPaused(_ context.Context, p *principal.Principal, _ string, definitionID, activationID string, paused bool) (*workflowmanager.ManagedDefinition, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subjects = append(m.subjects, subjectIDOf(p))
	value, ok := m.definitions[definitionID]
	if !ok {
		return nil, core.ErrNotFound
	}
	for i := range value.Definition.Activations {
		if value.Definition.Activations[i].ID == activationID {
			value.Definition.Activations[i].Paused = paused
			return cloneManagedDefinition(value), nil
		}
	}
	return nil, core.ErrNotFound
}

func (m *stubWorkflowManager) DeleteDefinition(_ context.Context, p *principal.Principal, _ string, definitionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subjects = append(m.subjects, subjectIDOf(p))
	if _, ok := m.definitions[definitionID]; !ok {
		return core.ErrNotFound
	}
	delete(m.definitions, definitionID)
	return nil
}

func (m *stubWorkflowManager) ListRuns(context.Context, *principal.Principal, string, coreworkflow.ListRunsRequest) (*workflowmanager.ListRunsResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*workflowmanager.ManagedRun, 0, len(m.runs))
	for _, item := range m.runs {
		out = append(out, cloneManagedRun(item))
	}
	return &workflowmanager.ListRunsResponse{Runs: out}, nil
}

func (m *stubWorkflowManager) StartRun(_ context.Context, p *principal.Principal, req workflowmanager.RunStart) (*workflowmanager.ManagedRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextRunID++
	id := fmt.Sprintf("run-%d", m.nextRunID)
	now := time.Now().UTC().Truncate(time.Second)
	value := &workflowmanager.ManagedRun{
		ProviderName: defaultWorkflowProviderName(req.ProviderName),
		Run: &coreworkflow.Run{
			ID:           id,
			Target:       cloneWorkflowTarget(m.workflowTargetForDefinition(req.DefinitionID)),
			DefinitionID: strings.TrimSpace(req.DefinitionID),
			WorkflowKey:  req.WorkflowKey,
			CreatedAt:    &now,
			CreatedBy:    subjectIDOf(p),
		},
	}
	m.runs[id] = value
	m.subjects = append(m.subjects, subjectIDOf(p))
	return cloneManagedRun(value), nil
}

func (m *stubWorkflowManager) workflowTargetForDefinition(definitionID string) coreworkflow.Target {
	if definition := m.definitions[strings.TrimSpace(definitionID)]; definition != nil && definition.Definition != nil {
		return definition.Definition.Target
	}
	return coreworkflow.Target{}
}

func (m *stubWorkflowManager) GetRun(context.Context, *principal.Principal, string, string) (*workflowmanager.ManagedRun, error) {
	return nil, core.ErrNotFound
}

func (m *stubWorkflowManager) GetRunEvents(context.Context, *principal.Principal, string, string) (*proto.GetWorkflowProviderRunEventsResponse, error) {
	return &proto.GetWorkflowProviderRunEventsResponse{}, nil
}

func (m *stubWorkflowManager) GetRunOutput(context.Context, *principal.Principal, string, string) (*proto.GetWorkflowProviderRunOutputResponse, error) {
	return &proto.GetWorkflowProviderRunOutputResponse{}, nil
}

func (m *stubWorkflowManager) CancelRun(context.Context, *principal.Principal, string, string, string) (*workflowmanager.ManagedRun, error) {
	return nil, core.ErrNotFound
}

func (m *stubWorkflowManager) SignalRun(context.Context, *principal.Principal, workflowmanager.RunSignal) (*workflowmanager.ManagedRunSignal, error) {
	return nil, core.ErrNotFound
}

func (m *stubWorkflowManager) SignalOrStartRun(context.Context, *principal.Principal, workflowmanager.RunSignalOrStart) (*workflowmanager.ManagedRunSignal, error) {
	return nil, core.ErrNotFound
}

func (m *stubWorkflowManager) DeliverEvent(_ context.Context, p *principal.Principal, req workflowmanager.EventDeliver) (coreworkflow.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subjects = append(m.subjects, subjectIDOf(p))
	event := req.Event
	if strings.TrimSpace(event.ID) == "" {
		event.ID = fmt.Sprintf("evt-%d", len(m.publishedEvents)+1)
	}
	m.publishedEvents = append(m.publishedEvents, cloneWorkflowEvent(event))
	m.publishedProviderNames = append(m.publishedProviderNames, req.ProviderName)
	return cloneWorkflowEvent(event), nil
}

func (m *stubWorkflowManager) Subjects() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.subjects...)
}

func (m *stubWorkflowManager) DefinitionIdempotencyKeys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.definitionKeys)
}

type stubAgentTurnManagerProvider struct {
	coreagent.UnimplementedProvider
	mu                    sync.Mutex
	createSessionRequests []*proto.CreateAgentProviderSessionRequest
	createTurnRequests    []*proto.CreateAgentProviderTurnRequest
	sessions              map[string]*coreagent.Session
	turns                 map[string]*coreagent.Turn
	turnEvents            map[string][]*coreagent.TurnEvent
	interactions          map[string]*coreagent.Interaction
}

func stubAgentProtoStructToMap(src *structpb.Struct) map[string]any {
	if src == nil {
		return nil
	}
	return src.AsMap()
}

func stubAgentMessagesFromProto(src []*proto.AgentMessage) []coreagent.Message {
	out := make([]coreagent.Message, 0, len(src))
	for _, message := range src {
		if message == nil {
			continue
		}
		out = append(out, coreagent.Message{
			Role:     message.GetRole(),
			Text:     message.GetText(),
			Metadata: stubAgentProtoStructToMap(message.GetMetadata()),
		})
	}
	return out
}

func stubAgentSessionStateFromProto(src proto.AgentSessionState) coreagent.SessionState {
	switch src {
	case proto.AgentSessionState_AGENT_SESSION_STATE_ACTIVE:
		return coreagent.SessionStateActive
	case proto.AgentSessionState_AGENT_SESSION_STATE_ARCHIVED:
		return coreagent.SessionStateArchived
	default:
		return ""
	}
}

func newStubAgentTurnManagerProvider() *stubAgentTurnManagerProvider {
	return &stubAgentTurnManagerProvider{
		sessions:     map[string]*coreagent.Session{},
		turns:        map[string]*coreagent.Turn{},
		turnEvents:   map[string][]*coreagent.TurnEvent{},
		interactions: map[string]*coreagent.Interaction{},
	}
}

func (p *stubAgentTurnManagerProvider) CreateSession(_ context.Context, req *proto.CreateAgentProviderSessionRequest) (*coreagent.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now().UTC().Truncate(time.Second)
	p.createSessionRequests = append(p.createSessionRequests, req)
	session := &coreagent.Session{
		ID:                 fmt.Sprintf("managed-session-%d", len(p.sessions)+1),
		ProviderName:       "managed",
		Model:              req.GetModel(),
		ClientRef:          req.GetClientRef(),
		State:              coreagent.SessionStateActive,
		Metadata:           stubAgentProtoStructToMap(req.GetMetadata()),
		CreatedBySubjectID: appaccessservice.SubjectIDFromRequestContext(req.GetContext()),
		CreatedAt:          &now,
		UpdatedAt:          &now,
	}
	p.sessions[session.ID] = session
	return cloneAgentSession(session), nil
}

func (p *stubAgentTurnManagerProvider) GetSession(_ context.Context, req *proto.GetAgentProviderSessionRequest) (*coreagent.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	session, ok := p.sessions[req.GetSessionId()]
	if !ok {
		return nil, core.ErrNotFound
	}
	return cloneAgentSession(session), nil
}

func (p *stubAgentTurnManagerProvider) ListSessions(context.Context, *proto.ListAgentProviderSessionsRequest) ([]*coreagent.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*coreagent.Session, 0, len(p.sessions))
	for _, session := range p.sessions {
		out = append(out, cloneAgentSession(session))
	}
	return out, nil
}

func (p *stubAgentTurnManagerProvider) UpdateSession(_ context.Context, req *proto.UpdateAgentProviderSessionRequest) (*coreagent.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	session, ok := p.sessions[req.GetSessionId()]
	if !ok {
		return nil, core.ErrNotFound
	}
	now := time.Now().UTC().Truncate(time.Second)
	if req.GetClientRef() != "" {
		session.ClientRef = req.GetClientRef()
	}
	if state := stubAgentSessionStateFromProto(req.GetState()); state != "" {
		session.State = state
	}
	if req.GetMetadata() != nil {
		session.Metadata = stubAgentProtoStructToMap(req.GetMetadata())
	}
	session.UpdatedAt = &now
	return cloneAgentSession(session), nil
}

func (p *stubAgentTurnManagerProvider) CreateTurn(_ context.Context, req *proto.CreateAgentProviderTurnRequest) (*coreagent.Turn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now().UTC().Truncate(time.Second)
	p.createTurnRequests = append(p.createTurnRequests, req)

	turn := &coreagent.Turn{
		ID:                 req.GetTurnId(),
		SessionID:          req.GetSessionId(),
		ProviderName:       "managed",
		Model:              req.GetModel(),
		Status:             coreagent.ExecutionStatusSucceeded,
		Messages:           stubAgentMessagesFromProto(req.GetMessages()),
		Output:             coreagent.TurnOutput{Text: &coreagent.TurnTextOutput{Text: "turn completed"}},
		CreatedBySubjectID: appaccessservice.SubjectIDFromRequestContext(req.GetContext()),
		CreatedAt:          &now,
		StartedAt:          &now,
		CompletedAt:        &now,
		ExecutionRef:       req.GetExecutionRef(),
	}
	p.turns[turn.ID] = turn
	p.appendTurnEventLocked(turn.ID, "turn.started", map[string]any{"session_id": req.GetSessionId()})

	if requireInteraction, _ := stubAgentProtoStructToMap(req.GetMetadata())["requireInteraction"].(bool); requireInteraction {
		turn.Status = coreagent.ExecutionStatusWaitingForInput
		turn.StatusMessage = "waiting for input"
		turn.CompletedAt = nil
		interactionID := "interaction-" + turn.ID
		p.interactions[interactionID] = &coreagent.Interaction{
			ID:        interactionID,
			TurnID:    turn.ID,
			SessionID: turn.SessionID,
			Type:      coreagent.InteractionTypeApproval,
			State:     coreagent.InteractionStatePending,
			Title:     "Approve action",
			Prompt:    "Continue the turn?",
			Request:   map[string]any{"provider_name": "managed"},
			CreatedAt: &now,
		}
		p.appendTurnEventLocked(turn.ID, "interaction.requested", map[string]any{"interaction_id": interactionID})
	} else {
		p.appendTurnEventLocked(turn.ID, "assistant.completed", map[string]any{"text": "turn completed"})
		p.appendTurnEventLocked(turn.ID, "turn.completed", map[string]any{"status": "succeeded"})
	}

	if session := p.sessions[req.GetSessionId()]; session != nil {
		session.LastTurnAt = &now
		session.UpdatedAt = &now
	}
	return cloneAgentTurn(turn), nil
}

func (p *stubAgentTurnManagerProvider) GetTurn(_ context.Context, req *proto.GetAgentProviderTurnRequest) (*coreagent.Turn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	turn, ok := p.turns[req.GetTurnId()]
	if !ok {
		return nil, core.ErrNotFound
	}
	return cloneAgentTurn(turn), nil
}

func (p *stubAgentTurnManagerProvider) ListTurns(_ context.Context, req *proto.ListAgentProviderTurnsRequest) ([]*coreagent.Turn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*coreagent.Turn, 0, len(p.turns))
	for _, turn := range p.turns {
		if req.GetSessionId() == "" || turn.SessionID == req.GetSessionId() {
			out = append(out, cloneAgentTurn(turn))
		}
	}
	return out, nil
}

func (p *stubAgentTurnManagerProvider) CancelTurn(_ context.Context, req *proto.CancelAgentProviderTurnRequest) (*coreagent.Turn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	turn, ok := p.turns[req.GetTurnId()]
	if !ok {
		return nil, core.ErrNotFound
	}
	now := time.Now().UTC().Truncate(time.Second)
	turn.Status = coreagent.ExecutionStatusCanceled
	turn.StatusMessage = req.GetReason()
	turn.CompletedAt = &now
	p.appendTurnEventLocked(turn.ID, "turn.canceled", map[string]any{"reason": req.GetReason()})
	return cloneAgentTurn(turn), nil
}

func (p *stubAgentTurnManagerProvider) ListTurnEvents(_ context.Context, req *proto.ListAgentProviderTurnEventsRequest) ([]*coreagent.TurnEvent, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	events := p.turnEvents[req.GetTurnId()]
	out := make([]*coreagent.TurnEvent, 0, len(events))
	for _, event := range events {
		if event.Seq <= req.AfterSeq {
			continue
		}
		out = append(out, cloneAgentTurnEvent(event))
		if req.GetLimit() > 0 && len(out) >= int(req.GetLimit()) {
			break
		}
	}
	return out, nil
}

func (p *stubAgentTurnManagerProvider) GetInteraction(_ context.Context, req *proto.GetAgentProviderInteractionRequest) (*coreagent.Interaction, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	interaction, ok := p.interactions[req.GetInteractionId()]
	if !ok {
		return nil, core.ErrNotFound
	}
	return cloneAgentInteraction(interaction), nil
}

func (p *stubAgentTurnManagerProvider) ListInteractions(_ context.Context, req *proto.ListAgentProviderInteractionsRequest) ([]*coreagent.Interaction, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*coreagent.Interaction, 0, len(p.interactions))
	for _, interaction := range p.interactions {
		if req.GetTurnId() == "" || interaction.TurnID == req.GetTurnId() {
			out = append(out, cloneAgentInteraction(interaction))
		}
	}
	return out, nil
}

func (p *stubAgentTurnManagerProvider) ResolveInteraction(_ context.Context, req *proto.ResolveAgentProviderInteractionRequest) (*coreagent.Interaction, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	interaction, ok := p.interactions[req.GetInteractionId()]
	if !ok {
		return nil, core.ErrNotFound
	}
	now := time.Now().UTC().Truncate(time.Second)
	interaction.State = coreagent.InteractionStateResolved
	interaction.Resolution = stubAgentProtoStructToMap(req.GetResolution())
	interaction.ResolvedAt = &now
	if turn := p.turns[interaction.TurnID]; turn != nil {
		turn.Status = coreagent.ExecutionStatusSucceeded
		turn.StatusMessage = interaction.ID
		turn.CompletedAt = &now
		p.appendTurnEventLocked(turn.ID, "interaction.resolved", map[string]any{"interaction_id": interaction.ID})
		p.appendTurnEventLocked(turn.ID, "turn.completed", map[string]any{"status": "succeeded"})
	}
	return cloneAgentInteraction(interaction), nil
}

func (p *stubAgentTurnManagerProvider) GetCapabilities(context.Context, *proto.GetAgentProviderCapabilitiesRequest) (*coreagent.ProviderCapabilities, error) {
	return &coreagent.ProviderCapabilities{
		StreamingText:        true,
		ToolCalls:            true,
		Interactions:         true,
		ResumableTurns:       true,
		BoundedListHydration: true,
		SupportedToolSources: []coreagent.ToolSourceMode{coreagent.ToolSourceModeCatalog},
	}, nil
}

func (p *stubAgentTurnManagerProvider) Ping(context.Context) error { return nil }
func (p *stubAgentTurnManagerProvider) Close() error               { return nil }

func (p *stubAgentTurnManagerProvider) appendTurnEventLocked(turnID, eventType string, data map[string]any) {
	events := p.turnEvents[turnID]
	now := time.Now().UTC().Truncate(time.Second)
	p.turnEvents[turnID] = append(events, &coreagent.TurnEvent{
		ID:         fmt.Sprintf("%s-event-%d", turnID, len(events)+1),
		TurnID:     turnID,
		Seq:        int64(len(events) + 1),
		Type:       eventType,
		Source:     "managed",
		Visibility: "private",
		Data:       maps.Clone(data),
		CreatedAt:  &now,
	})
}

func cloneAgentSession(src *coreagent.Session) *coreagent.Session {
	if src == nil {
		return nil
	}
	dst := *src
	dst.Metadata = maps.Clone(src.Metadata)
	return &dst
}

func cloneAgentTurn(src *coreagent.Turn) *coreagent.Turn {
	if src == nil {
		return nil
	}
	dst := *src
	dst.Messages = append([]coreagent.Message(nil), src.Messages...)
	if src.Output.Text != nil {
		dst.Output.Text = &coreagent.TurnTextOutput{Text: src.Output.Text.Text}
	}
	if src.Output.Structured != nil {
		dst.Output.Structured = &coreagent.TurnStructuredOutput{
			Text:  src.Output.Structured.Text,
			Value: maps.Clone(src.Output.Structured.Value),
		}
	}
	return &dst
}

func cloneAgentTurnEvent(src *coreagent.TurnEvent) *coreagent.TurnEvent {
	if src == nil {
		return nil
	}
	dst := *src
	dst.Data = maps.Clone(src.Data)
	return &dst
}

func cloneAgentInteraction(src *coreagent.Interaction) *coreagent.Interaction {
	if src == nil {
		return nil
	}
	dst := *src
	dst.Request = maps.Clone(src.Request)
	dst.Resolution = maps.Clone(src.Resolution)
	return &dst
}

func cloneManagedDefinition(value *workflowmanager.ManagedDefinition) *workflowmanager.ManagedDefinition {
	if value == nil {
		return nil
	}
	out := *value
	if value.Definition != nil {
		definition := *value.Definition
		definition.Target = cloneWorkflowTarget(value.Definition.Target)
		out.Definition = &definition
	}
	return &out
}

func cloneManagedRun(value *workflowmanager.ManagedRun) *workflowmanager.ManagedRun {
	if value == nil {
		return nil
	}
	out := *value
	if value.Run != nil {
		run := *value.Run
		run.Target = cloneWorkflowTarget(value.Run.Target)
		out.Run = &run
	}
	return &out
}

func cloneWorkflowTarget(value coreworkflow.Target) coreworkflow.Target {
	out := coreworkflow.Target{Steps: make([]coreworkflow.Step, len(value.Steps))}
	for i := range value.Steps {
		step := value.Steps[i]
		if step.Inputs != nil {
			step.Inputs = make(map[string]coreworkflow.Value, len(step.Inputs))
			for key, item := range value.Steps[i].Inputs {
				step.Inputs[key] = coreworkflow.CloneValue(item)
			}
		}
		if step.App != nil {
			appStep := *step.App
			appStep.Input = coreworkflow.CloneValue(appStep.Input)
			step.App = &appStep
		}
		if step.Agent != nil {
			agent := *step.Agent
			agent.Messages = slices.Clone(agent.Messages)
			for j := range agent.Messages {
				agent.Messages[j].Metadata = maps.Clone(agent.Messages[j].Metadata)
			}
			agent.ToolRefs = slices.Clone(agent.ToolRefs)
			if agent.Output.Structured != nil {
				structured := *agent.Output.Structured
				structured.Schema = maps.Clone(structured.Schema)
				agent.Output.Structured = &structured
			}
			agent.ModelOptions = maps.Clone(agent.ModelOptions)
			step.Agent = &agent
		}
		if step.When != nil {
			when := *step.When
			when.Value = coreworkflow.CloneValue(when.Value)
			step.When = &when
		}
		step.Metadata = maps.Clone(step.Metadata)
		out.Steps[i] = step
	}
	return out
}

func cloneWorkflowEvent(value coreworkflow.Event) coreworkflow.Event {
	return coreworkflow.Event{
		ID:              value.ID,
		Source:          value.Source,
		SpecVersion:     value.SpecVersion,
		Type:            value.Type,
		Subject:         value.Subject,
		Time:            value.Time,
		DataContentType: value.DataContentType,
		Data:            maps.Clone(value.Data),
		Extensions:      maps.Clone(value.Extensions),
	}
}

func subjectIDOf(p *principal.Principal) string {
	if p == nil {
		return ""
	}
	return p.SubjectID
}

func defaultWorkflowProviderName(name string) string {
	if strings.TrimSpace(name) == "" {
		return "basic"
	}
	return strings.TrimSpace(name)
}

func TestExecutableSDKExampleProviderReceivesStartConfig(t *testing.T) {
	t.Parallel()

	bin := buildExampleProviderBinary(t)
	manifestRoot := exampleProviderRoot(t)
	manifest := newExecutableManifest("Example Provider", "A minimal example provider built with the public SDK")
	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"example": {
				Command:              bin,
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Config: mustNode(t, map[string]any{
					"greeting": "Hello from config",
				}),
			},
		},
	}

	factories := NewFactoryRegistry()
	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, testRuntimePublicEndpointDeps(t, Deps{}))
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	prov, err := providers.Get("example")
	if err != nil {
		t.Fatalf("providers.Get(example): %v", err)
	}
	if prov.DisplayName() != "Example Provider" {
		t.Fatalf("DisplayName = %q", prov.DisplayName())
	}
	if prov.Description() != "A minimal example provider built with the public SDK" {
		t.Fatalf("Description = %q", prov.Description())
	}
	cat := prov.Catalog()
	if cat == nil || len(cat.Operations) != 5 {
		t.Fatalf("unexpected catalog: %+v", cat)
	}
	if cat.DisplayName != "Example Provider" || cat.Description != "A minimal example provider built with the public SDK" {
		t.Fatalf("unexpected catalog metadata: %+v", cat)
	}
	if cat.Operations[0].Transport != catalog.TransportApp {
		t.Fatalf("unexpected catalog transport: %+v", cat.Operations[0])
	}

	result, err := prov.Execute(context.Background(), "greet", map[string]any{"name": "Gestalt"}, "")
	if err != nil {
		t.Fatalf("Execute(greet): %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("greet status = %d", result.Status)
	}
	if string(result.Body) != `{"message":"Hello from config, Gestalt!"}` {
		t.Fatalf("greet body = %q", result.Body)
	}

	result, err = prov.Execute(context.Background(), "status", nil, "")
	if err != nil {
		t.Fatalf("Execute(status): %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("status status = %d", result.Status)
	}

	var got map[string]string
	if err := json.Unmarshal(result.Body, &got); err != nil {
		t.Fatalf("json.Unmarshal(status): %v", err)
	}
	if got["name"] != "example" {
		t.Fatalf("status.name = %q", got["name"])
	}
	if got["greeting"] != "Hello from config" {
		t.Fatalf("status.greeting = %q", got["greeting"])
	}
}

func TestPythonSourcePluginFallsBackWithoutGoOnPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("source-plugin fallback fixture is POSIX-only")
	}

	bin := buildExampleProviderBinary(t)
	root := t.TempDir()
	manifest := &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      "github.com/testowner/apps/python-source",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Python Source",
		Description: "Python source provider fixture",
		Spec: &providermanifestv1.Spec{
			Connections: map[string]*providermanifestv1.ManifestConnectionDef{
				"default": {
					Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeNone},
				},
			},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: ".gestaltd/bin/python-source"},
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"/bin/sh", "-c", "/bin/mkdir -p .gestaltd/bin && /bin/cp .venv/bin/python .gestaltd/bin/python-source && /bin/chmod +x .gestaltd/bin/python-source"},
			Inputs:  []string{".venv/bin/python"},
		},
	}
	manifestPath := filepath.Join(root, "manifest.yaml")
	if err := os.WriteFile(manifestPath, []byte(`kind: app
source: github.com/testowner/apps/python-source
version: 0.0.1-alpha.1
displayName: Python Source
description: Python source provider fixture
build:
  command: [/bin/sh, -c, "/bin/mkdir -p .gestaltd/bin && /bin/cp .venv/bin/python .gestaltd/bin/python-source && /bin/chmod +x .gestaltd/bin/python-source"]
  inputs: [.venv/bin/python]
run:
  command: [/bin/sh, -c, "/bin/mkdir -p .gestaltd/bin && /bin/cp .venv/bin/python .gestaltd/bin/python-source && /bin/chmod +x .gestaltd/bin/python-source && ./.gestaltd/bin/python-source"]
spec:
  connections:
    default:
      auth:
        type: none
`), 0o644); err != nil {
		t.Fatalf("WriteFile(manifest.yaml): %v", err)
	}
	catalogData, err := yaml.Marshal(&catalog.Catalog{
		Name: "python-source",
		Operations: []catalog.CatalogOperation{
			{ID: "greet", Method: http.MethodPost},
			{ID: "status", Method: http.MethodGet},
		},
	})
	if err != nil {
		t.Fatalf("yaml.Marshal(catalog): %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, providerpkg.StaticCatalogFile), catalogData, 0o644); err != nil {
		t.Fatalf("WriteFile(catalog.yaml): %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".venv", "bin"), 0o755); err != nil {
		t.Fatalf("MkdirAll(.venv/bin): %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".venv", "bin", "python"), []byte("#!/bin/sh\nset -eu\nexec "+strconv.Quote(bin)+"\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(.venv/bin/python): %v", err)
	}

	t.Setenv("PATH", t.TempDir())

	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"python-source": {
				ResolvedManifest:     manifest,
				ResolvedManifestPath: manifestPath,
				Config: mustNode(t, map[string]any{
					"greeting": "Hi",
				}),
			},
		},
	}

	factories := NewFactoryRegistry()
	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, testRuntimePublicEndpointDeps(t, Deps{}))
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	prov, err := providers.Get("python-source")
	if err != nil {
		t.Fatalf("providers.Get(python-source): %v", err)
	}

	result, err := prov.Execute(context.Background(), "greet", map[string]any{"name": "Ada"}, "")
	if err != nil {
		t.Fatalf("Execute(greet): %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("greet status = %d, want %d", result.Status, http.StatusOK)
	}
	if string(result.Body) != `{"message":"Hi, Ada!"}` {
		t.Fatalf("greet body = %q", result.Body)
	}
}

func TestSpecLoadedOpenAPIProviderUsesConfiguredAPIBaseURL(t *testing.T) {
	t.Parallel()

	var docHits atomic.Int32
	docSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		docHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"source":"document"}`))
	}))
	t.Cleanup(docSrv.Close)

	var manifestHits atomic.Int32
	manifestSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		manifestHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"source":"manifest"}`))
	}))
	t.Cleanup(manifestSrv.Close)

	var configHits atomic.Int32
	var configPath atomic.Value
	configSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		configHits.Add(1)
		configPath.Store(r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"source":"config"}`))
	}))
	t.Cleanup(configSrv.Close)

	root := t.TempDir()
	manifestPath := filepath.Join(root, "manifest.yaml")
	if err := os.WriteFile(manifestPath, []byte("kind: app\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(manifest.yaml): %v", err)
	}
	openapiPath := filepath.Join(root, "openapi.yaml")
	openapiDoc := fmt.Sprintf(`openapi: "3.1.0"
info:
  title: Example
  version: "1.0.0"
servers:
  - url: %s
paths:
  /items:
    get:
      operationId: list_items
      responses:
        "200":
          description: OK
`, docSrv.URL)
	if err := os.WriteFile(openapiPath, []byte(openapiDoc), 0o644); err != nil {
		t.Fatalf("WriteFile(openapi.yaml): %v", err)
	}

	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"example": {
				ResolvedManifestPath: manifestPath,
				ResolvedManifest: &providermanifestv1.Manifest{
					Kind:        providermanifestv1.KindApp,
					DisplayName: "Example",
					Description: "OpenAPI example",
					Spec: &providermanifestv1.Spec{
						Surfaces: &providermanifestv1.ProviderSurfaces{
							OpenAPI: &providermanifestv1.OpenAPISurface{
								Document: "openapi.yaml",
								BaseURL:  manifestSrv.URL,
							},
						},
					},
				},
				Surfaces: &config.ProviderSurfaceOverrides{
					OpenAPI: &config.ProviderOpenAPISurfaceOverride{
						BaseURL: configSrv.URL,
					},
				},
			},
		},
	}

	providers, _, err := buildProvidersStrict(context.Background(), cfg, NewFactoryRegistry(), Deps{})
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })

	prov, err := providers.Get("example")
	if err != nil {
		t.Fatalf("providers.Get(example): %v", err)
	}

	result, err := prov.Execute(context.Background(), "list_items", nil, "")
	if err != nil {
		t.Fatalf("Execute(list_items): %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", result.Status, http.StatusOK)
	}
	if got := string(result.Body); got != `{"source":"config"}` {
		t.Fatalf("body = %q, want %q", got, `{"source":"config"}`)
	}
	if got, _ := configPath.Load().(string); got != "/items" {
		t.Fatalf("request path = %q, want %q", got, "/items")
	}
	if got := configHits.Load(); got != 1 {
		t.Fatalf("configured base URL hits = %d, want 1", got)
	}
	if got := manifestHits.Load(); got != 0 {
		t.Fatalf("manifest base URL hits = %d, want 0", got)
	}
	if got := docHits.Load(); got != 0 {
		t.Fatalf("document server hits = %d, want 0", got)
	}
}

func TestHybridExecutableProviderAppliesAllowedOperationsToStaticAndOpenAPICatalogs(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "hybrid",
		Operations: []catalog.CatalogOperation{
			{ID: "echo", Method: http.MethodPost, Tags: []string{"static-source"}, Parameters: []catalog.CatalogParameter{{Name: "message", Type: "string", Required: true}}},
		},
	})
	openapiDoc := `openapi: "3.1.0"
info:
  title: Hybrid
  version: "1.0.0"
paths:
  /status:
    get:
      operationId: status
      tags:
        - openapi-source
      responses:
        "200":
          description: OK
`
	if err := os.WriteFile(filepath.Join(manifestRoot, "openapi.yaml"), []byte(openapiDoc), 0o644); err != nil {
		t.Fatalf("WriteFile(openapi.yaml): %v", err)
	}

	manifest := newExecutableManifest("Hybrid", "Hybrid provider")
	manifest.Entrypoint = &providermanifestv1.Entrypoint{ArtifactPath: "ignored-for-command-mode"}
	manifest.Spec.Surfaces = &providermanifestv1.ProviderSurfaces{
		OpenAPI: &providermanifestv1.OpenAPISurface{Document: "openapi.yaml"},
	}
	manifestPath := filepath.Join(manifestRoot, "manifest.yaml")

	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"hybrid": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: manifestPath,
				AllowedOperations: map[string]*config.OperationOverride{
					"echo":   {Alias: "renamed_echo", Tags: []string{"static-override"}},
					"status": {Alias: "renamed_status", Tags: []string{"status-override"}},
				},
			},
		},
	}

	factories := NewFactoryRegistry()
	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, testRuntimePublicEndpointDeps(t, Deps{}))
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	prov, err := providers.Get("hybrid")
	if err != nil {
		t.Fatalf("providers.Get(hybrid): %v", err)
	}
	cat := prov.Catalog()
	if cat == nil {
		t.Fatal("Catalog() = nil")
	}

	hasOperation := func(id string) bool {
		return slices.ContainsFunc(cat.Operations, func(op catalog.CatalogOperation) bool {
			return op.ID == id
		})
	}
	if !hasOperation("renamed_echo") || !hasOperation("renamed_status") {
		t.Fatalf("catalog operations = %+v, want renamed static and OpenAPI operations", cat.Operations)
	}
	if hasOperation("echo") || hasOperation("status") {
		t.Fatalf("catalog operations = %+v, want original operation ids hidden", cat.Operations)
	}
	operationTags := func(id string) []string {
		for _, op := range cat.Operations {
			if op.ID == id {
				return op.Tags
			}
		}
		t.Fatalf("operation %q not found in catalog: %+v", id, cat.Operations)
		return nil
	}
	if got, want := operationTags("renamed_echo"), []string{"static-source", "static-override"}; !slices.Equal(got, want) {
		t.Fatalf("renamed_echo tags = %#v, want %#v", got, want)
	}
	if got, want := operationTags("renamed_status"), []string{"openapi-source", "status-override"}; !slices.Equal(got, want) {
		t.Fatalf("renamed_status tags = %#v, want %#v", got, want)
	}
}

func TestHybridExecutableProviderRoutesPluginOperationsThroughNamedSpecConnection(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "hybrid",
		Operations: []catalog.CatalogOperation{
			{ID: "echo", Method: http.MethodPost, Parameters: []catalog.CatalogParameter{{Name: "message", Type: "string", Required: true}}},
		},
	})
	openapiDoc := `openapi: "3.1.0"
info:
  title: Hybrid
  version: "1.0.0"
paths:
  /status:
    get:
      operationId: status
      responses:
        "200":
          description: OK
`
	if err := os.WriteFile(filepath.Join(manifestRoot, "openapi.yaml"), []byte(openapiDoc), 0o644); err != nil {
		t.Fatalf("WriteFile(openapi.yaml): %v", err)
	}

	manifest := newExecutableManifest("Hybrid", "Hybrid provider")
	manifest.Entrypoint = &providermanifestv1.Entrypoint{ArtifactPath: "ignored-for-command-mode"}
	manifest.Spec.Surfaces = &providermanifestv1.ProviderSurfaces{
		OpenAPI: &providermanifestv1.OpenAPISurface{Document: "openapi.yaml"},
	}
	manifest.Spec.Connections = map[string]*providermanifestv1.ManifestConnectionDef{
		"default": {
			Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeBearer},
		},
	}
	manifestPath := filepath.Join(manifestRoot, "manifest.yaml")

	entry := &config.ProviderEntry{
		Command:              bin,
		Args:                 []string{"provider"},
		ResolvedManifest:     manifest,
		ResolvedManifestPath: manifestPath,
	}
	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"hybrid": entry,
		},
	}

	factories := NewFactoryRegistry()
	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, testRuntimePublicEndpointDeps(t, Deps{}))
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	prov, err := providers.Get("hybrid")
	if err != nil {
		t.Fatalf("providers.Get(hybrid): %v", err)
	}
	if got := prov.ConnectionForOperation("echo"); got != "default" {
		t.Fatalf("echo connection = %q, want %q", got, "default")
	}
	if got := prov.ConnectionForOperation("status"); got != "default" {
		t.Fatalf("status connection = %q, want %q", got, "default")
	}

	_, operationRouting, err := buildStartupProviderSpec("hybrid", entry)
	if err != nil {
		t.Fatalf("buildStartupProviderSpec: %v", err)
	}
	if got := operationRouting.connections["echo"]; got != "default" {
		t.Fatalf("startup echo connection = %q, want %q", got, "default")
	}
	if _, ok := operationRouting.connections["status"]; ok {
		t.Fatalf("startup catalog unexpectedly exposed spec-loaded status operation")
	}
}

func TestHybridDeclarativeExecutableProviderUsesNamedDefaultConnectionForPluginOperations(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "hybrid",
		Operations: []catalog.CatalogOperation{
			{ID: "echo", Method: http.MethodPost, Parameters: []catalog.CatalogParameter{{Name: "message", Type: "string", Required: true}}},
		},
	})

	manifest := newExecutableManifest("Hybrid", "Hybrid provider")
	manifest.Entrypoint = &providermanifestv1.Entrypoint{ArtifactPath: "ignored-for-command-mode"}
	manifest.Spec.Surfaces = &providermanifestv1.ProviderSurfaces{
		REST: &providermanifestv1.RESTSurface{
			BaseURL: "https://example.invalid",
			Operations: []providermanifestv1.ProviderOperation{
				{
					Name:       "status",
					Method:     http.MethodGet,
					Path:       "/status",
					Connection: "bot",
				},
			},
		},
	}
	manifest.Spec.Connections = map[string]*providermanifestv1.ManifestConnectionDef{
		"default": {
			Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeBearer},
		},
		"bot": {
			Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeBearer},
		},
	}
	manifestPath := filepath.Join(manifestRoot, "manifest.yaml")

	entry := &config.ProviderEntry{
		Command:              bin,
		Args:                 []string{"provider"},
		ResolvedManifest:     manifest,
		ResolvedManifestPath: manifestPath,
	}
	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"hybrid": entry,
		},
	}

	factories := NewFactoryRegistry()
	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, testRuntimePublicEndpointDeps(t, Deps{}))
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	prov, err := providers.Get("hybrid")
	if err != nil {
		t.Fatalf("providers.Get(hybrid): %v", err)
	}
	if got := prov.ConnectionForOperation("echo"); got != "default" {
		t.Fatalf("echo connection = %q, want %q", got, "default")
	}
	if got := prov.ConnectionForOperation("status"); got != "bot" {
		t.Fatalf("status connection = %q, want %q", got, "bot")
	}

	services := testutil.NewStubServices(t)
	subjectID := principal.UserSubjectID("u-hybrid")
	if err := services.ExternalCredentials.UpsertCredential(context.Background(), &core.ExternalCredential{
		Subject:   subjectID,
		Audience:  "hybrid:default",
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "tok-default"},
	}); err != nil {
		t.Fatalf("UpsertCredential(default): %v", err)
	}

	result, err := invocation.NewBroker(providers, services.Users, services.ExternalCredentials).Invoke(
		context.Background(),
		&principal.Principal{
			UserID: "u-hybrid",
			Kind:   principal.KindUser,
			Scopes: []string{"hybrid"},
		},
		"hybrid",
		"",
		"echo",
		map[string]any{"message": "hello"},
	)
	if err != nil {
		t.Fatalf("Invoke(hybrid.echo): %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", result.Status, http.StatusOK)
	}
	if string(result.Body) != `{"message":"hello"}` {
		t.Fatalf("body = %q, want %q", result.Body, `{"message":"hello"}`)
	}
}

func TestSpecLoadedDualSurfaceProviderBuildsMCPOperations(t *testing.T) {
	t.Parallel()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pages" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"source":"api"}`))
	}))
	t.Cleanup(apiSrv.Close)

	mcpSrv := mcpserver.NewMCPServer("notion-upstream", "1.0.0")
	mcpSrv.AddTool(
		mcpgo.NewTool("search", mcpgo.WithDescription("Search Notion")),
		func(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			return mcpgo.NewToolResultText("from-mcp"), nil
		},
	)
	mcpHTTP := httptest.NewServer(mcpserver.NewStreamableHTTPServer(
		mcpSrv,
		mcpserver.WithStateLess(true),
	))
	t.Cleanup(mcpHTTP.Close)

	root := t.TempDir()
	manifestPath := filepath.Join(root, "manifest.yaml")
	if err := os.WriteFile(manifestPath, []byte("kind: app\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(manifest.yaml): %v", err)
	}
	openapiPath := filepath.Join(root, "openapi.yaml")
	openapiDoc := fmt.Sprintf(`openapi: "3.1.0"
info:
  title: Notion
  version: "1.0.0"
servers:
  - url: %s
paths:
  /pages:
    get:
      operationId: list_pages
      responses:
        "200":
          description: OK
`, apiSrv.URL)
	if err := os.WriteFile(openapiPath, []byte(openapiDoc), 0o644); err != nil {
		t.Fatalf("WriteFile(openapi.yaml): %v", err)
	}

	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"notion": {
				ResolvedManifestPath: manifestPath,
				ResolvedManifest: &providermanifestv1.Manifest{
					Kind:        providermanifestv1.KindApp,
					DisplayName: "Notion",
					Description: "Dual-surface provider",
					Spec: &providermanifestv1.Spec{
						Surfaces: &providermanifestv1.ProviderSurfaces{
							OpenAPI: &providermanifestv1.OpenAPISurface{
								Document: "openapi.yaml",
							},
							MCP: &providermanifestv1.MCPSurface{
								URL: mcpHTTP.URL,
							},
						},
					},
				},
			},
		},
	}

	providers, _, err := buildProvidersStrict(context.Background(), cfg, NewFactoryRegistry(), Deps{})
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })

	prov, err := providers.Get("notion")
	if err != nil {
		t.Fatalf("providers.Get(notion): %v", err)
	}

	apiResult, err := prov.Execute(context.Background(), "list_pages", nil, "")
	if err != nil {
		t.Fatalf("Execute(list_pages): %v", err)
	}
	if apiResult.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", apiResult.Status, http.StatusOK)
	}
	if string(apiResult.Body) != `{"source":"api"}` {
		t.Fatalf("body = %q, want %q", apiResult.Body, `{"source":"api"}`)
	}

	directTool, ok := any(prov).(interface {
		CallTool(context.Context, string, map[string]any) (*mcpgo.CallToolResult, error)
	})
	if !ok {
		t.Fatalf("provider does not expose direct MCP tools: %T", prov)
	}
	mcpResult, err := directTool.CallTool(context.Background(), "search", nil)
	if err != nil {
		t.Fatalf("CallTool(search): %v", err)
	}
	if mcpResult.IsError {
		t.Fatalf("unexpected MCP tool error: %+v", mcpResult.Content)
	}
	text, ok := mcpResult.Content[0].(mcpgo.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", mcpResult.Content[0])
	}
	if text.Text != "from-mcp" {
		t.Fatalf("text = %q, want %q", text.Text, "from-mcp")
	}
}

func TestExecutableSDKExampleProviderAppliesConfigMetadataOverrides(t *testing.T) {
	t.Parallel()

	const iconSVG = `<svg viewBox="0 0 10 10"><rect x="1" y="1" width="8" height="8"/></svg>`

	bin := buildExampleProviderBinary(t)
	iconPath := t.TempDir() + "/override.svg"
	if err := os.WriteFile(iconPath, []byte(iconSVG), 0o644); err != nil {
		t.Fatalf("WriteFile(icon): %v", err)
	}

	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name:        "example",
		DisplayName: "Catalog Display",
		Description: "Catalog Description",
		Operations: []catalog.CatalogOperation{
			{ID: "status", Method: http.MethodGet},
		},
	})
	manifest := newExecutableManifest("Manifest Display", "Manifest Description")

	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"example": {
				DisplayName:          "Config Display",
				Description:          "Config Description",
				IconFile:             iconPath,
				Command:              bin,
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
			},
		},
	}

	factories := NewFactoryRegistry()
	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, testRuntimePublicEndpointDeps(t, Deps{}))
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	prov, err := providers.Get("example")
	if err != nil {
		t.Fatalf("providers.Get(example): %v", err)
	}
	if prov.DisplayName() != "Config Display" {
		t.Fatalf("DisplayName = %q, want %q", prov.DisplayName(), "Config Display")
	}
	if prov.Description() != "Config Description" {
		t.Fatalf("Description = %q, want %q", prov.Description(), "Config Description")
	}

	cat := prov.Catalog()
	if cat == nil {
		t.Fatal("expected non-nil catalog")
		return
	}
	if cat.DisplayName != "Config Display" {
		t.Fatalf("catalog DisplayName = %q, want %q", cat.DisplayName, "Config Display")
	}
	if cat.Description != "Config Description" {
		t.Fatalf("catalog Description = %q, want %q", cat.Description, "Config Description")
	}
	if cat.IconSVG != iconSVG {
		t.Fatalf("catalog IconSVG = %q, want %q", cat.IconSVG, iconSVG)
	}
}

func buildEchoPluginBinary(t *testing.T) string {
	t.Helper()
	if sharedEchoPluginBin == "" {
		t.Fatal("shared echo app binary not initialized")
	}
	return sharedEchoPluginBin
}

func buildExampleProviderBinary(t *testing.T) string {
	t.Helper()
	if sharedExampleProviderBin == "" {
		t.Fatal("shared example provider binary not initialized")
	}
	return sharedExampleProviderBin
}

func exampleProviderRoot(t *testing.T) string {
	t.Helper()
	return testutil.ExampleProviderPluginPath(t)
}

func mustNode(t *testing.T, value any) yaml.Node {
	t.Helper()
	var node yaml.Node
	if err := node.Encode(value); err != nil {
		t.Fatalf("node.Encode: %v", err)
	}
	return node
}

func writeStaticCatalog(t *testing.T, cat *catalog.Catalog) string {
	t.Helper()
	data, err := yaml.Marshal(cat)
	if err != nil {
		t.Fatalf("yaml.Marshal(catalog): %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, providerpkg.StaticCatalogFile)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile(catalog): %v", err)
	}
	return dir
}

func newExecutableManifest(displayName, description string) *providermanifestv1.Manifest {
	return &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      "github.com/acme/apps/test",
		Version:     "1.0.0",
		DisplayName: displayName,
		Description: description,
		Spec:        &providermanifestv1.Spec{},
	}
}

func newNestedInvokeHarness(t *testing.T, brokerOpts ...invocation.BrokerOption) *nestedInvokeHarness {
	t.Helper()

	callerBin := buildEchoPluginBinary(t)
	callerRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "caller",
		Operations: []catalog.CatalogOperation{
			{ID: "invoke_plugin", Method: http.MethodPost},
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	exampleBin := buildExampleProviderBinary(t)
	exampleRoot := exampleProviderRoot(t)
	callerManifest := newExecutableManifest("Caller", "Invokes another plugin")
	callerManifest.Spec.Connections = map[string]*providermanifestv1.ManifestConnectionDef{
		"default": {
			Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeBearer},
		},
	}
	exampleManifest := newExecutableManifest("Example Provider", "Reports request context")
	exampleManifest.Spec.Connections = map[string]*providermanifestv1.ManifestConnectionDef{
		"default": {
			Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeBearer},
		},
	}

	bridge := newLazyInvoker()
	secret := []byte("0123456789abcdef0123456789abcdef")
	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"caller": {
				Command:              callerBin,
				Args:                 []string{"provider"},
				ResolvedManifest:     callerManifest,
				ResolvedManifestPath: filepath.Join(callerRoot, "manifest.yaml"),
			},
			"example": {
				Command:              exampleBin,
				ResolvedManifest:     exampleManifest,
				ResolvedManifestPath: filepath.Join(exampleRoot, "manifest.yaml"),
				Config: mustNode(t, map[string]any{
					"greeting": "Hello from nested invoke",
				}),
			},
		},
	}

	deps := testRuntimePublicEndpointDeps(t, Deps{
		EncryptionKey: secret,
		AppInvocation: bridge,
		Authorization: newAllowAllAuthorizationProvider(),
	})
	providers, _, err := buildProvidersStrict(context.Background(), cfg, NewFactoryRegistry(), deps)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })
	registerGlobalAppInvocationForTest(t, deps)

	services, err := coredata.New(&coretesting.StubIndexedDB{})
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	testutil.AttachStubExternalCredentials(services)
	t.Cleanup(func() { _ = services.Close() })

	broker := invocation.NewBroker(providers, services.Users, services.ExternalCredentials, brokerOpts...)
	bridge.SetTarget(invocation.NewGuarded(broker, nil, "app", nil, invocation.WithoutRateLimit()))

	return &nestedInvokeHarness{
		invoker:  invocation.NewGuarded(broker, nil, "test", nil, invocation.WithoutRateLimit()),
		services: services,
	}
}

func graphqlStringPtr(value string) *string {
	return &value
}

func pluginInvokeGraphQLSchema() graphqlschema.Schema {
	return graphqlschema.Schema{
		QueryType: &graphqlschema.TypeName{Name: "Query"},
		Types: []graphqlschema.FullType{
			{
				Kind: "OBJECT",
				Name: "Query",
				Fields: []graphqlschema.Field{
					{
						Name: "viewer",
						Args: []graphqlschema.InputValue{
							{Name: "team", Type: graphqlschema.TypeRef{Kind: "NON_NULL", OfType: &graphqlschema.TypeRef{Kind: "SCALAR", Name: graphqlStringPtr("String")}}},
						},
						Type: graphqlschema.TypeRef{Kind: "OBJECT", Name: graphqlStringPtr("Viewer")},
					},
				},
			},
			{
				Kind: "OBJECT",
				Name: "Viewer",
				Fields: []graphqlschema.Field{
					{Name: "id", Type: graphqlschema.TypeRef{Kind: "SCALAR", Name: graphqlStringPtr("ID")}},
					{Name: "name", Type: graphqlschema.TypeRef{Kind: "SCALAR", Name: graphqlStringPtr("String")}},
				},
			},
		},
	}
}

func newGraphQLSurfaceInvokeHarness(t *testing.T, graphQLURL string, allowSurface bool, _ config.AuthorizationConfig, brokerOpts ...invocation.BrokerOption) *nestedInvokeHarness {
	t.Helper()

	callerBin := buildEchoPluginBinary(t)
	callerRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "caller",
		Operations: []catalog.CatalogOperation{
			{ID: "invoke_plugin_graphql", Method: http.MethodPost},
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	callerManifest := newExecutableManifest("Caller", "Invokes graphql on another plugin")
	callerManifest.Spec.Connections = map[string]*providermanifestv1.ManifestConnectionDef{
		"default": {
			Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeBearer},
		},
	}

	linearRoot := t.TempDir()
	linearManifestPath := filepath.Join(linearRoot, "manifest.yaml")
	if err := os.WriteFile(linearManifestPath, []byte("kind: app\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(manifest.yaml): %v", err)
	}
	linearManifest := &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      "github.com/acme/apps/linear",
		Version:     "1.0.0",
		DisplayName: "Linear",
		Description: "GraphQL target",
		Spec: &providermanifestv1.Spec{
			Connections: map[string]*providermanifestv1.ManifestConnectionDef{
				"default": {
					Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeBearer},
				},
			},
			Surfaces: &providermanifestv1.ProviderSurfaces{
				GraphQL: &providermanifestv1.GraphQLSurface{
					Connection: "default",
					URL:        graphQLURL,
				},
			},
		},
	}

	bridge := newLazyInvoker()
	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"caller": {
				Command:              callerBin,
				Args:                 []string{"provider"},
				ResolvedManifest:     callerManifest,
				ResolvedManifestPath: filepath.Join(callerRoot, "manifest.yaml"),
			},
			"linear": {
				ResolvedManifest:     linearManifest,
				ResolvedManifestPath: linearManifestPath,
			},
		},
	}

	secret := []byte("0123456789abcdef0123456789abcdef")
	deps := testRuntimePublicEndpointDeps(t, Deps{
		EncryptionKey: secret,
		AppInvocation: bridge,
		Authorization: newAllowAllAuthorizationProvider(),
	})
	providers, _, err := buildProvidersStrict(context.Background(), cfg, NewFactoryRegistry(), deps)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })
	registerGlobalAppInvocationForTest(t, deps)

	services, err := coredata.New(&coretesting.StubIndexedDB{})
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	testutil.AttachStubExternalCredentials(services)
	t.Cleanup(func() { _ = services.Close() })

	broker := invocation.NewBroker(providers, services.Users, services.ExternalCredentials, brokerOpts...)
	bridge.SetTarget(invocation.NewGuarded(broker, nil, "app", nil, invocation.WithoutRateLimit()))

	return &nestedInvokeHarness{
		invoker:  invocation.NewGuarded(broker, nil, "test", nil, invocation.WithoutRateLimit()),
		services: services,
	}
}

func newNestedInvokeUser(t *testing.T, harness *nestedInvokeHarness, ctx context.Context, email string) *core.User {
	t.Helper()

	user, err := harness.services.Users.FindOrCreateUser(ctx, email)
	if err != nil {
		t.Fatalf("FindOrCreateUser(%q): %v", email, err)
	}
	return user
}

func storeNestedInvokeToken(t *testing.T, harness *nestedInvokeHarness, ctx context.Context, userID, plugin, connection, instance string) {
	t.Helper()

	storeNestedInvokeTokenForSubject(t, harness, ctx, principal.UserSubjectID(userID), plugin, connection, instance)
}

func storeNestedInvokeTokenForSubject(t *testing.T, harness *nestedInvokeHarness, ctx context.Context, subjectID, plugin, connection, instance string) {
	t.Helper()

	if err := harness.services.ExternalCredentials.UpsertCredential(ctx, &core.ExternalCredential{
		Subject:   subjectID,
		Audience:  plugin + ":" + connection,
		Qualifier: instance,
		Grant: &core.ExternalCredentialGrant{
			AccessToken:  plugin + "-" + connection + "-token",
			RefreshToken: "refresh-token",
		},
	}); err != nil {
		t.Fatalf("UpsertCredential(%s,%s,%s): %v", plugin, connection, instance, err)
	}
}

func TestBuildStartupProviderSpecPreservesStaticCatalogConnectionRouting(t *testing.T) {
	t.Parallel()

	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "roadmap",
		Operations: []catalog.CatalogOperation{
			{ID: "status", Method: http.MethodGet, Transport: catalog.TransportREST},
			{ID: "search", Method: http.MethodPost, Transport: catalog.TransportMCPPassthrough},
			{ID: "echo", Method: http.MethodPost},
		},
	})
	manifest := newExecutableManifest("Roadmap", "Workflow startup routing")
	manifest.Spec.DefaultConnection = config.AppConnectionAlias
	manifest.Spec.Connections = map[string]*providermanifestv1.ManifestConnectionDef{
		"api": {
			Mode: providermanifestv1.ConnectionModeSubject,
			Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeBearer},
		},
		"openapi": {
			Mode: providermanifestv1.ConnectionModeSubject,
			Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeBearer},
		},
		"mcp": {
			Mode: providermanifestv1.ConnectionModeSubject,
			Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeBearer},
		},
	}
	manifest.Spec.Surfaces = &providermanifestv1.ProviderSurfaces{
		OpenAPI: &providermanifestv1.OpenAPISurface{Document: "openapi.yaml", Connection: "openapi"},
		REST:    &providermanifestv1.RESTSurface{Connection: "api"},
		MCP:     &providermanifestv1.MCPSurface{URL: "https://example.invalid/mcp", Connection: "mcp"},
	}

	spec, operationRouting, err := buildStartupProviderSpec("roadmap", &config.ProviderEntry{
		ResolvedManifest:     manifest,
		ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
	})
	if err != nil {
		t.Fatalf("buildStartupProviderSpec: %v", err)
	}
	if spec.Catalog == nil || len(spec.Catalog.Operations) != 3 {
		t.Fatalf("unexpected startup catalog: %+v", spec.Catalog)
	}
	if got := operationRouting.connections["status"]; got != "api" {
		t.Fatalf("status connection = %q, want %q", got, "api")
	}
	if got := operationRouting.connections["search"]; got != "mcp" {
		t.Fatalf("search connection = %q, want %q", got, "mcp")
	}
	if got := operationRouting.connections["echo"]; got != config.AppConnectionName {
		t.Fatalf("echo connection = %q, want %q", got, config.AppConnectionName)
	}

	manifestRoot = writeStaticCatalog(t, &catalog.Catalog{
		Name: "schema",
		Operations: []catalog.CatalogOperation{
			{ID: "versions.list", Method: http.MethodPost, Transport: "graphql"},
		},
	})
	manifest = newExecutableManifest("Schema", "GraphQL startup routing")
	manifest.Spec.Connections = map[string]*providermanifestv1.ManifestConnectionDef{
		"dev":  {Mode: providermanifestv1.ConnectionModeSubject},
		"prod": {Mode: providermanifestv1.ConnectionModeSubject},
	}
	manifest.Spec.Surfaces = &providermanifestv1.ProviderSurfaces{
		GraphQL: &providermanifestv1.GraphQLSurface{URL: "https://{graphql_host}/graphql"},
	}

	spec, operationRouting, err = buildStartupProviderSpec("schema", &config.ProviderEntry{
		ResolvedManifest:     manifest,
		ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
	})
	if err != nil {
		t.Fatalf("buildStartupProviderSpec ambiguous graphql: %v", err)
	}
	if spec.Catalog == nil || len(spec.Catalog.Operations) != 1 {
		t.Fatalf("unexpected ambiguous graphql startup catalog: %+v", spec.Catalog)
	}
	if got, ok := operationRouting.connections["versions.list"]; ok {
		t.Fatalf("versions.list connection = %q, want no static connection so the selected connection can apply", got)
	}

	manifest = &providermanifestv1.Manifest{
		Source:      "gqlapp",
		DisplayName: "GraphQL App",
		Spec: &providermanifestv1.Spec{
			Connections: map[string]*providermanifestv1.ManifestConnectionDef{
				"dev":  {Mode: providermanifestv1.ConnectionModeNone},
				"prod": {Mode: providermanifestv1.ConnectionModeNone},
			},
			Surfaces: &providermanifestv1.ProviderSurfaces{
				GraphQL: &providermanifestv1.GraphQLSurface{URL: "https://{graphql_host}/graphql"},
			},
			AllowedOperations: map[string]*providermanifestv1.ManifestOperationOverride{
				"items": {
					Alias: "items.list",
					GraphQL: &providermanifestv1.ManifestGraphQLOperation{
						OperationName: "ItemsQuery",
						Document:      "query ItemsQuery { items { id name } }",
					},
				},
				"status": {
					GraphQL: &providermanifestv1.ManifestGraphQLOperation{
						OperationName: "StatusQuery",
						Document:      "query StatusQuery { status }",
					},
				},
			},
		},
	}

	spec, _, err = buildStartupProviderSpec("gqlapp", &config.ProviderEntry{ResolvedManifest: manifest})
	if err != nil {
		t.Fatalf("buildStartupProviderSpec graphql allowedOps: %v", err)
	}
	if spec.Catalog == nil {
		t.Fatal("expected startup catalog for GraphQL allowedOperations manifest, got nil")
	}
	ids := make(map[string]bool, len(spec.Catalog.Operations))
	for i := range spec.Catalog.Operations {
		ids[spec.Catalog.Operations[i].ID] = true
	}
	if !ids["items.list"] {
		t.Errorf("catalog missing aliased operation %q; have %v", "items.list", ids)
	}
	if !ids["status"] {
		t.Errorf("catalog missing operation %q; have %v", "status", ids)
	}
}

func TestBuildStartupProviderSpecMCPOnlyManifestHasNoStartupCatalog(t *testing.T) {
	t.Parallel()

	manifest := &providermanifestv1.Manifest{
		Source:      "mcponly",
		DisplayName: "MCP Only",
		Spec: &providermanifestv1.Spec{
			Connections: map[string]*providermanifestv1.ManifestConnectionDef{
				"mcp": {
					Mode: providermanifestv1.ConnectionModeSubject,
					Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeMCPOAuth},
				},
			},
			Surfaces: &providermanifestv1.ProviderSurfaces{
				MCP: &providermanifestv1.MCPSurface{URL: "https://example.invalid/mcp", Connection: "mcp"},
			},
		},
	}

	spec, operationRouting, err := buildStartupProviderSpec("mcponly", &config.ProviderEntry{ResolvedManifest: manifest})
	if err != nil {
		t.Fatalf("buildStartupProviderSpec: %v", err)
	}
	if spec.Catalog != nil {
		t.Fatalf("unexpected startup catalog for MCP-only manifest: %+v", spec.Catalog)
	}
	if len(operationRouting.connections) != 0 {
		t.Fatalf("unexpected operation connections: %+v", operationRouting.connections)
	}
}

func TestStartupProviderProxyResolvesDeclarativeConnectionSelectorBeforeProviderReady(t *testing.T) {
	t.Parallel()

	manifest := &providermanifestv1.Manifest{
		Source:      "slack",
		DisplayName: "Slack",
		Spec: &providermanifestv1.Spec{
			DefaultConnection: "default",
			Connections: map[string]*providermanifestv1.ManifestConnectionDef{
				"default": {Mode: providermanifestv1.ConnectionModeSubject},
				"bot":     {Mode: providermanifestv1.ConnectionModeSubject},
			},
			Surfaces: &providermanifestv1.ProviderSurfaces{
				REST: &providermanifestv1.RESTSurface{
					Connection: "bot",
					BaseURL:    "https://slack.com",
					Operations: []providermanifestv1.ProviderOperation{
						{
							Name:   "chat.postMessage",
							Method: http.MethodPost,
							Path:   "/api/chat.postMessage",
							ConnectionSelector: &providermanifestv1.OperationConnectionSelector{
								Parameter: "actor",
								Default:   "bot",
								Values: map[string]string{
									"bot":  "bot",
									"user": "default",
								},
							},
							Parameters: []providermanifestv1.ProviderParameter{
								{Name: "actor", Type: "string", In: "body", Internal: true},
								{Name: "channel", Type: "string", In: "body", Required: true},
								{Name: "text", Type: "string", In: "body", Required: true},
							},
						},
						{
							Name:   "chat.scheduleMessage",
							Method: http.MethodPost,
							Path:   "/api/chat.scheduleMessage",
							Parameters: []providermanifestv1.ProviderParameter{
								{Name: "channel", Type: "string", In: "body", Required: true},
								{Name: "text", Type: "string", In: "body", Required: true},
								{Name: "post_at", Type: "int", In: "body", Required: true},
							},
						},
					},
				},
			},
		},
	}
	spec, operationRouting, err := buildStartupProviderSpec("slack", &config.ProviderEntry{ResolvedManifest: manifest})
	if err != nil {
		t.Fatalf("buildStartupProviderSpec: %v", err)
	}
	proxy := newStartupProviderProxy(spec, operationRouting, nil)

	conn, err := proxy.ResolveConnectionForOperation("chat.postMessage", map[string]any{"actor": "user"})
	if err != nil {
		t.Fatalf("ResolveConnectionForOperation(user): %v", err)
	}
	if conn != "default" {
		t.Fatalf("user actor connection = %q, want default", conn)
	}
	conn, err = proxy.ResolveConnectionForOperation("chat.postMessage", nil)
	if err != nil {
		t.Fatalf("ResolveConnectionForOperation(default): %v", err)
	}
	if conn != "bot" {
		t.Fatalf("default actor connection = %q, want bot", conn)
	}
	if proxy.OperationConnectionOverrideAllowed("chat.postMessage", map[string]any{"actor": "user"}) {
		t.Fatal("selector-selected operation allowed explicit override")
	}
	if !proxy.OperationConnectionOverrideAllowed("chat.scheduleMessage", nil) {
		t.Fatal("surface fallback operation rejected explicit override before provider ready")
	}
}

func TestPluginManifestOAuthWiresConnectionAuth(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)

	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoauth",
		Operations: []catalog.CatalogOperation{
			{ID: "echo", Method: http.MethodPost},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	manifest.Entrypoint = &providermanifestv1.Entrypoint{
		ArtifactPath: "bin/echo",
		Args:         []string{"--config", "/etc/gestalt/echo.yaml"},
	}
	manifest.Spec.Connections = map[string]*providermanifestv1.ManifestConnectionDef{
		"default": {
			Auth: &providermanifestv1.ProviderAuth{
				Type:             providermanifestv1.AuthTypeOAuth2,
				AuthorizationURL: "https://example.com/authorize",
				TokenURL:         "https://example.com/token",
				Scopes:           []string{"read", "write"},
			},
		},
	}
	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"echoauth": {
				Command: bin,
				Args:    []string{"provider"},
				Config: mustNode(t, map[string]any{
					"clientId":     "test-client-id",
					"clientSecret": "test-client-secret",
				}),
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
			},
		},
	}

	factories := NewFactoryRegistry()
	providers, connAuth, err := buildProvidersStrict(
		context.Background(), cfg, factories,
		testRuntimePublicEndpointDeps(t, Deps{}),
	)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	prov, err := providers.Get("echoauth")
	if err != nil {
		t.Fatalf("providers.Get(echoauth): %v", err)
	}
	if cat := prov.Catalog(); cat == nil || len(cat.Operations) == 0 {
		t.Fatal("expected at least one operation from the echo provider")
	}

	handlers, ok := connAuth["echoauth"]
	if !ok {
		t.Fatal("expected connection auth entry for echoauth")
	}
	handler, ok := handlers["default"]
	if !ok {
		t.Fatalf("expected handler for connection %q", "default")
	}
	if handler.AuthorizationBaseURL() != "https://example.com/authorize" {
		t.Fatalf("authorization URL = %q, want %q", handler.AuthorizationBaseURL(), "https://example.com/authorize")
	}
	if handler.TokenURL() != "https://example.com/token" {
		t.Fatalf("token URL = %q, want %q", handler.TokenURL(), "https://example.com/token")
	}
}

func TestPluginManifestNoAuthSkipsConnectionAuth(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)

	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echonoauth",
		Operations: []catalog.CatalogOperation{
			{ID: "echo", Method: http.MethodPost},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"echonoauth": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
			},
		},
	}

	factories := NewFactoryRegistry()
	providers, connAuth, err := buildProvidersStrict(context.Background(), cfg, factories, testRuntimePublicEndpointDeps(t, Deps{}))
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	if _, ok := connAuth["echonoauth"]; ok {
		t.Fatal("expected no connection auth for app without oauth2 auth")
	}
}

func TestPluginManifestNamedOAuthKeepsProviderTokenMode(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)

	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoauth",
		Operations: []catalog.CatalogOperation{
			{ID: "echo", Method: http.MethodPost},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"echoauth": {
				Command:           bin,
				Args:              []string{"provider"},
				Source:            config.NewMetadataSource("https://example.invalid/github-com-acme-plugins-test/v1.0.0/provider-release.yaml"),
				DefaultConnection: "workspace",
				Connections: map[string]*config.ConnectionDef{
					"workspace": {
						Auth: config.ConnectionAuthDef{
							Type:             providermanifestv1.AuthTypeOAuth2,
							AuthorizationURL: "https://example.com/authorize",
							TokenURL:         "https://example.com/token",
						},
					},
				},
				Config: mustNode(t, map[string]any{
					"clientId":     "test-client-id",
					"clientSecret": "test-client-secret",
				}),
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
			},
		},
	}

	factories := NewFactoryRegistry()
	providers, _, err := buildProvidersStrict(
		context.Background(), cfg, factories,
		Deps{BaseURL: "https://gestalt.example.com"},
	)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	prov, err := providers.Get("echoauth")
	if err != nil {
		t.Fatalf("providers.Get(echoauth): %v", err)
	}
	if prov.ConnectionMode() != core.ConnectionModeSubject {
		t.Fatalf("ConnectionMode = %q, want %q", prov.ConnectionMode(), core.ConnectionModeSubject)
	}
}

func TestPreparedProviderStub_RejectsMixedConnectionModes(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"echoauth": {
				Source: config.NewMetadataSource("https://example.invalid/github-com-acme-plugins-test/v1.0.0/provider-release.yaml"),
				ResolvedManifest: &providermanifestv1.Manifest{
					DisplayName: "Echo Auth",
					Spec: &providermanifestv1.Spec{
						Connections: map[string]*providermanifestv1.ManifestConnectionDef{
							"default": {
								Mode: providermanifestv1.ConnectionModeSubject,
								Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeNone},
							},
							"workspace": {
								Mode: providermanifestv1.ConnectionModeSubject,
								Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeNone},
							},
						},
					},
				},
			},
		},
	}

	providers, _, err := buildProvidersStrict(context.Background(), cfg, NewFactoryRegistry(), Deps{})
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()
}

func TestAppProcessEnvIsolation(t *testing.T) {
	t.Parallel()
	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "echo", Method: http.MethodPost},
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")

	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
			},
		},
	}

	factories := NewFactoryRegistry()
	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, testRuntimePublicEndpointDeps(t, Deps{}))
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	prov, err := providers.Get("echoext")
	if err != nil {
		t.Fatalf("providers.Get: %v", err)
	}

	result, err := prov.Execute(context.Background(), "read_env", map[string]any{"name": "USER"}, "")
	if err != nil {
		t.Fatalf("Execute read_env: %v", err)
	}

	var env struct {
		Value string `json:"value"`
		Found bool   `json:"found"`
	}
	if err := json.Unmarshal(result.Body, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Found {
		t.Fatalf("plugin process should not see USER, but got %q", env.Value)
	}

	result, err = prov.Execute(context.Background(), "read_env", map[string]any{"name": "PATH"}, "")
	if err != nil {
		t.Fatalf("Execute read_env PATH: %v", err)
	}
	if err := json.Unmarshal(result.Body, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !env.Found || env.Value == "" {
		t.Fatal("plugin process should see PATH")
	}
}

func TestPluginIndexedDBExposeHostSocketEnv(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")

	makeConfig := func(indexedDB *config.IndexedDBBindingConfig) *config.Config {
		return &config.Config{
			Apps: map[string]*config.ProviderEntry{
				"echoext": {
					Command:              bin,
					Args:                 []string{"provider"},
					ResolvedManifest:     manifest,
					ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
					IndexedDB:            indexedDB,
				},
			},
		}
	}

	indexedDBDefs := map[string]*config.ProviderEntry{
		"main": {
			Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.2/provider-release.yaml"),
			Config: mustNode(t, map[string]any{"dsn": "postgres://main.example.test/gestalt"}),
		},
		"archive": {
			Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.2/provider-release.yaml"),
			Config: mustNode(t, map[string]any{"dsn": "sqlite://archive.db"}),
		},
	}

	checkEnv := func(t *testing.T, indexedDB *config.IndexedDBBindingConfig, envName string) bool {
		t.Helper()
		providers, _, err := buildProvidersStrict(context.Background(), makeConfig(indexedDB), NewFactoryRegistry(), testRuntimePublicEndpointDeps(t, Deps{
			SelectedIndexedDBName: "main",
			IndexedDBDefs:         indexedDBDefs,
			IndexedDBFactory: func(yaml.Node) (indexeddb.IndexedDB, error) {
				return &coretesting.StubIndexedDB{}, nil
			},
		}))
		if err != nil {
			t.Fatalf("buildProvidersStrict: %v", err)
		}
		defer func() { _ = CloseProviders(providers) }()

		prov, err := providers.Get("echoext")
		if err != nil {
			t.Fatalf("providers.Get: %v", err)
		}
		result, err := prov.Execute(context.Background(), "read_env", map[string]any{"name": envName}, "")
		if err != nil {
			t.Fatalf("Execute read_env: %v", err)
		}
		var env struct {
			Value string `json:"value"`
			Found bool   `json:"found"`
		}
		if err := json.Unmarshal(result.Body, &env); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return env.Found && env.Value != ""
	}
	if got := checkEnv(t, nil, runtimehost.HostServiceSocketEnv); !got {
		t.Fatal("unified host-service env should be set when app omits indexeddb and inherits the host selection")
	}
	if got := checkEnv(t, &config.IndexedDBBindingConfig{}, runtimehost.HostServiceSocketEnv); !got {
		t.Fatal("unified host-service env should be set when app indexeddb is explicitly empty")
	}
	if got := checkEnv(t, &config.IndexedDBBindingConfig{Provider: "archive"}, runtimehost.HostServiceSocketEnv); !got {
		t.Fatal("unified host-service env should be set when app explicitly selects one indexeddb provider")
	}
}

func TestPluginInvokesExposeHostSocketEnv(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "caller",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
			{ID: "invoke_plugin", Method: http.MethodPost},
		},
	})
	manifest := newExecutableManifest("Caller", "Invokes another plugin")

	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"caller": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
			},
		},
	}

	providers, _, err := buildProvidersStrict(context.Background(), cfg, NewFactoryRegistry(), testRuntimePublicEndpointDeps(t, Deps{}))
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	prov, err := providers.Get("caller")
	if err != nil {
		t.Fatalf("providers.Get(caller): %v", err)
	}

	result, err := prov.Execute(context.Background(), "read_env", map[string]any{"name": runtimehost.HostServiceSocketEnv}, "")
	if err != nil {
		t.Fatalf("Execute read_env: %v", err)
	}

	var env struct {
		Value string `json:"value"`
		Found bool   `json:"found"`
	}
	if err := json.Unmarshal(result.Body, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !env.Found || env.Value == "" {
		t.Fatalf("host-service env %q should be set for executable plugins", runtimehost.HostServiceSocketEnv)
	}
}

func TestAppWorkflowManagerExposeHostSocketEnv(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echo",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Workflow manager host env")

	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"echo": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
			},
		},
	}

	providers, _, err := buildProvidersStrict(context.Background(), cfg, NewFactoryRegistry(), testRuntimePublicEndpointDeps(t, Deps{
		WorkflowManager: newStubWorkflowManager(),
	}))
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	prov, err := providers.Get("echo")
	if err != nil {
		t.Fatalf("providers.Get(echo): %v", err)
	}

	result, err := prov.Execute(context.Background(), "read_env", map[string]any{"name": runtimehost.HostServiceSocketEnv}, "")
	if err != nil {
		t.Fatalf("Execute read_env: %v", err)
	}

	var env struct {
		Value string `json:"value"`
		Found bool   `json:"found"`
	}
	if err := json.Unmarshal(result.Body, &env); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if !env.Found || env.Value == "" {
		t.Fatalf("host-service env %q should be set for executable plugins", runtimehost.HostServiceSocketEnv)
	}
}

func TestPluginAgentManagerExposeHostSocketEnv(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echo",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Agent manager host env")

	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"echo": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
			},
		},
	}

	providers, _, err := buildProvidersStrict(context.Background(), cfg, NewFactoryRegistry(), testRuntimePublicEndpointDeps(t, Deps{
		AgentRuntime: &agentRuntime{providers: map[string]coreagent.Provider{}},
	}))
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	prov, err := providers.Get("echo")
	if err != nil {
		t.Fatalf("providers.Get(echo): %v", err)
	}

	result, err := prov.Execute(context.Background(), "read_env", map[string]any{"name": runtimehost.HostServiceSocketEnv}, "")
	if err != nil {
		t.Fatalf("Execute read_env: %v", err)
	}

	var env struct {
		Value string `json:"value"`
		Found bool   `json:"found"`
	}
	if err := json.Unmarshal(result.Body, &env); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if !env.Found || env.Value == "" {
		t.Fatalf("host-service env %q should be set for executable plugins", runtimehost.HostServiceSocketEnv)
	}
}

func TestPluginAgentManagerTurnUsesInheritedInvokesAndRequestContext(t *testing.T) {
	t.Parallel()

	secret := []byte("0123456789abcdef0123456789abcdef")
	publicHostServices := runtimehost.NewPublicHostServiceRegistry()
	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "agent_manager_roundtrip", Method: http.MethodPost},
		},
	})
	manifest := newExecutableManifest("Echo", "Agent manager turn roundtrip")
	services := testutil.NewStubServices(t)

	pluginProviders := registry.New()
	if err := pluginProviders.Providers.Register("roadmap", &coretesting.StubIntegration{
		N:        "roadmap",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{
			Name: "roadmap",
			Operations: []catalog.CatalogOperation{{
				ID:          "sync",
				Method:      http.MethodPost,
				Title:       "Sync roadmap",
				Description: "Sync the roadmap state",
			}},
		},
		ExecuteFn: func(ctx context.Context, operation string, params map[string]any, _ string) (*core.OperationResult, error) {
			body, err := json.Marshal(map[string]any{
				"operation": operation,
				"subject":   principal.FromContext(ctx).SubjectID,
				"taskId":    params["taskId"],
			})
			if err != nil {
				return nil, err
			}
			return &core.OperationResult{Status: http.StatusAccepted, Body: body}, nil
		},
	}); err != nil {
		t.Fatalf("Register roadmap provider: %v", err)
	}

	agentProvider := newStubAgentTurnManagerProvider()
	agentRuntime := &agentRuntime{defaultProviderName: "managed", providers: map[string]coreagent.Provider{"managed": agentProvider}}
	broker := invocation.NewBroker(&pluginProviders.Providers, services.Users, services.ExternalCredentials)
	turnScopes := newTestAgentTurnScopes()
	manager := agentmanager.New(agentmanager.Config{
		Providers:  &pluginProviders.Providers,
		Agent:      agentRuntime,
		TurnScopes: turnScopes,
		ToolIDs:    newTestAgentToolIDs(t),
		Invoker:    broker,
	})
	relaySrv := httptest.NewUnstartedServer(newRuntimeRelayTestHandler(t, secret, publicHostServices))
	relaySrv.EnableHTTP2 = true
	relaySrv.StartTLS()
	testutil.CloseOnCleanup(t, relaySrv)

	runtimeProvider := newCapturingBundleRuntime()
	runtimeProvider.support.EgressMode = proto.RuntimeEgressMode_RUNTIME_EGRESS_MODE_HOSTNAME
	runtimeProvider.fakeHosted = true
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (runtimeprovider.Provider, error) {
		return runtimeProvider, nil
	}

	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Apps: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Runtime:              &config.RuntimePlacementConfig{},
			},
		},
	}

	deps := Deps{
		BaseURL:            relaySrv.URL,
		EncryptionKey:      secret,
		Services:           services,
		AgentRuntime:       agentRuntime,
		AgentManager:       manager,
		PublicHostServices: publicHostServices,
	}
	deps.RuntimeRegistry = newRuntimeRegistry(cfg, factories.Runtime, deps)

	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()
	assertPublicHostServicesVerified(t, publicHostServices, "agent")

	prov, err := providers.Get("echoext")
	if err != nil {
		t.Fatalf("providers.Get(echoext): %v", err)
	}

	perms := principal.CompilePermissions([]core.AccessPermission{{
		App:        "roadmap",
		Operations: []string{"sync"},
	}, {
		App: "managed",
	}})
	ctx := principal.WithPrincipal(context.Background(), &principal.Principal{
		SubjectID: "user:user-123",
		UserID:    "user-123",
		Kind:      principal.KindUser,
		Source:    principal.SourceBearer,
		Scopes:    append([]string{"echoext"}, principal.ScopeStringsFromPermissionSet(perms)...),
	})

	result, err := prov.Execute(ctx, "agent_manager_roundtrip", nil, "")
	if err != nil {
		t.Fatalf("Execute(agent_manager_roundtrip): %v", err)
	}

	var roundTrip struct {
		ProviderName  string   `json:"provider_name"`
		SessionID     string   `json:"session_id"`
		TurnID        string   `json:"turn_id"`
		InteractionID string   `json:"interaction_id"`
		Status        string   `json:"status"`
		EventTypes    []string `json:"event_types"`
	}
	if err := json.Unmarshal(result.Body, &roundTrip); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if roundTrip.ProviderName != "managed" || roundTrip.SessionID == "" || roundTrip.TurnID == "" || roundTrip.InteractionID == "" {
		t.Fatalf("agent roundtrip result = %+v", roundTrip)
	}
	if roundTrip.Status != proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_SUCCEEDED.String() {
		t.Fatalf("agent roundtrip status = %q, want %q", roundTrip.Status, proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_SUCCEEDED.String())
	}
	if !slices.Contains(roundTrip.EventTypes, "turn.started") ||
		!slices.Contains(roundTrip.EventTypes, "interaction.requested") ||
		!slices.Contains(roundTrip.EventTypes, "interaction.resolved") ||
		!slices.Contains(roundTrip.EventTypes, "turn.completed") {
		t.Fatalf("agent roundtrip event_types = %#v, want canonical turn lifecycle events", roundTrip.EventTypes)
	}

	agentProvider.mu.Lock()
	createSessionCount := len(agentProvider.createSessionRequests)
	createTurnCount := len(agentProvider.createTurnRequests)
	var sessionReq *proto.CreateAgentProviderSessionRequest
	if createSessionCount > 0 {
		sessionReq = agentProvider.createSessionRequests[0]
	}
	var turnReq *proto.CreateAgentProviderTurnRequest
	if createTurnCount > 0 {
		turnReq = agentProvider.createTurnRequests[0]
	}
	agentProvider.mu.Unlock()

	if createSessionCount != 1 {
		t.Fatalf("CreateSession count = %d, want 1", createSessionCount)
	}
	if createTurnCount != 1 {
		t.Fatalf("CreateTurn count = %d, want 1", createTurnCount)
	}
	if sessionReq.IdempotencyKey != "plugin-agent-session" {
		t.Fatalf("CreateSession idempotency_key = %q, want %q", sessionReq.IdempotencyKey, "plugin-agent-session")
	}
	if sessionReq.GetContext().GetSubject().GetId() != "user:user-123" {
		t.Fatalf("CreateSession context.subject.id = %q, want %q", sessionReq.GetContext().GetSubject().GetId(), "user:user-123")
	}
	if turnReq.IdempotencyKey != "plugin-agent-turn" {
		t.Fatalf("CreateTurn idempotency_key = %q, want %q", turnReq.IdempotencyKey, "plugin-agent-turn")
	}
	if turnReq.GetSessionId() != roundTrip.SessionID {
		t.Fatalf("CreateTurn session_id = %q, want %q", turnReq.GetSessionId(), roundTrip.SessionID)
	}
	if turnReq.GetContext().GetSubject().GetId() != "user:user-123" {
		t.Fatalf("CreateTurn context.subject.id = %q, want %q", turnReq.GetContext().GetSubject().GetId(), "user:user-123")
	}
	turnMetadata := stubAgentProtoStructToMap(turnReq.GetMetadata())
	if requireInteraction, _ := turnMetadata["requireInteraction"].(bool); !requireInteraction {
		t.Fatalf("CreateTurn metadata = %#v, want requireInteraction=true", turnMetadata)
	}
}

func TestAppWorkflowManagerDefinitionLifecycleUsesRequestContext(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echo",
		Operations: []catalog.CatalogOperation{
			{ID: "apply_workflow_definition", Method: http.MethodPost},
			{ID: "get_workflow_definition", Method: http.MethodGet},
			{ID: "set_workflow_definition_paused", Method: http.MethodPost},
			{ID: "set_workflow_activation_paused", Method: http.MethodPost},
			{ID: "delete_workflow_definition", Method: http.MethodPost},
			{ID: "deliver_workflow_event", Method: http.MethodPost},
		},
	})
	manifest := newExecutableManifest("Echo", "Workflow manager definitions")
	manager := newStubWorkflowManager()

	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"echo": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
			},
		},
	}

	secret := []byte("0123456789abcdef0123456789abcdef")
	providers, _, err := buildProvidersStrict(context.Background(), cfg, NewFactoryRegistry(), testRuntimePublicEndpointDeps(t, Deps{
		EncryptionKey:   secret,
		WorkflowManager: manager,
		Authorization:   newAllowAllAuthorizationProvider(),
	}))
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	prov, err := providers.Get("echo")
	if err != nil {
		t.Fatalf("providers.Get(echo): %v", err)
	}

	ctx := principal.WithPrincipal(context.Background(), &principal.Principal{
		SubjectID: "user:user-123",
		UserID:    "user-123",
		Kind:      principal.KindUser,
		Source:    principal.SourceBearer,
		Scopes:    []string{"echo"},
	})

	applyResult, err := prov.Execute(ctx, "apply_workflow_definition", map[string]any{
		"definition_id": "roadmap_sync",
		"provider":      "basic",
		"run_as":        "service_account:echo-workflow",
		"target": map[string]any{
			"app":        "roadmap",
			"operation":  "sync",
			"connection": "work",
			"instance":   "default",
			"input": map[string]any{
				"mode": "incremental",
			},
		},
		"activations": []any{
			map[string]any{
				"id": "nightly",
				"schedule": map[string]any{
					"cron":     "*/5 * * * *",
					"timezone": "America/New_York",
				},
			},
		},
	}, "")
	if err != nil {
		t.Fatalf("Execute(apply_workflow_definition): %v", err)
	}
	var applied struct {
		Provider   string `json:"provider"`
		Definition struct {
			ID          string `json:"id"`
			Paused      bool   `json:"paused"`
			Activations []struct {
				ID       string `json:"id"`
				Schedule struct {
					Cron     string `json:"cron"`
					Timezone string `json:"timezone"`
				} `json:"schedule"`
			} `json:"activations"`
			Target struct {
				App       string         `json:"app"`
				Operation string         `json:"operation"`
				Input     map[string]any `json:"input"`
			} `json:"target"`
		} `json:"definition"`
	}
	if err := json.Unmarshal(applyResult.Body, &applied); err != nil {
		t.Fatalf("json.Unmarshal(apply): %v", err)
	}
	if applied.Definition.ID != "roadmap_sync" {
		t.Fatalf("unexpected apply result: %+v", applied)
	}
	if applied.Definition.Target.App != "roadmap" || applied.Definition.Target.Operation != "sync" {
		t.Fatalf("unexpected target: %+v", applied.Definition.Target)
	}
	if got := applied.Definition.Target.Input["mode"]; got != "incremental" {
		t.Fatalf("target.input.mode = %v, want incremental", got)
	}
	if len(applied.Definition.Activations) != 1 || applied.Definition.Activations[0].ID != "nightly" || applied.Definition.Activations[0].Schedule.Cron != "*/5 * * * *" {
		t.Fatalf("unexpected activations: %+v", applied.Definition.Activations)
	}

	getResult, err := prov.Execute(ctx, "get_workflow_definition", map[string]any{
		"definition_id": applied.Definition.ID,
		"provider":      "basic",
	}, "")
	if err != nil {
		t.Fatalf("Execute(get_workflow_definition): %v", err)
	}
	var fetched map[string]any
	if err := json.Unmarshal(getResult.Body, &fetched); err != nil {
		t.Fatalf("json.Unmarshal(get): %v", err)
	}
	if fetched["definition"] == nil {
		t.Fatalf("fetched definition missing: %+v", fetched)
	}

	pauseDefinitionResult, err := prov.Execute(ctx, "set_workflow_definition_paused", map[string]any{
		"definition_id": applied.Definition.ID,
		"provider":      "basic",
		"paused":        true,
	}, "")
	if err != nil {
		t.Fatalf("Execute(set_workflow_definition_paused): %v", err)
	}
	var pausedDefinition struct {
		Definition struct {
			Paused bool `json:"paused"`
		} `json:"definition"`
	}
	if err := json.Unmarshal(pauseDefinitionResult.Body, &pausedDefinition); err != nil {
		t.Fatalf("json.Unmarshal(definition pause): %v", err)
	}
	if !pausedDefinition.Definition.Paused {
		t.Fatalf("pause definition result = %+v, want paused definition", pausedDefinition)
	}

	pauseActivationResult, err := prov.Execute(ctx, "set_workflow_activation_paused", map[string]any{
		"definition_id": applied.Definition.ID,
		"activation_id": "nightly",
		"provider":      "basic",
		"paused":        true,
	}, "")
	if err != nil {
		t.Fatalf("Execute(set_workflow_activation_paused): %v", err)
	}
	var pausedActivation struct {
		Definition struct {
			Activations []struct {
				ID     string `json:"id"`
				Paused bool   `json:"paused"`
			} `json:"activations"`
		} `json:"definition"`
	}
	if err := json.Unmarshal(pauseActivationResult.Body, &pausedActivation); err != nil {
		t.Fatalf("json.Unmarshal(activation pause): %v", err)
	}
	if len(pausedActivation.Definition.Activations) != 1 || !pausedActivation.Definition.Activations[0].Paused {
		t.Fatalf("pause activation result = %+v, want paused activation", pausedActivation)
	}

	deleteResult, err := prov.Execute(ctx, "delete_workflow_definition", map[string]any{
		"definition_id": applied.Definition.ID,
		"provider":      "basic",
	}, "")
	if err != nil {
		t.Fatalf("Execute(delete_workflow_definition): %v", err)
	}
	var deleted struct {
		Deleted bool `json:"deleted"`
	}
	if err := json.Unmarshal(deleteResult.Body, &deleted); err != nil {
		t.Fatalf("json.Unmarshal(delete): %v", err)
	}
	if !deleted.Deleted {
		t.Fatalf("delete result = %+v, want deleted", deleted)
	}

	deliverEventResult, err := prov.Execute(ctx, "deliver_workflow_event", map[string]any{
		"provider": "basic",
		"type":     "roadmap.item.updated",
		"source":   "roadmap",
		"subject":  "item-123",
		"data": map[string]any{
			"id":    "item-123",
			"title": "Ship parity",
		},
		"extensions": map[string]any{
			"tenant": "acme",
		},
	}, "")
	if err != nil {
		t.Fatalf("Execute(deliver_workflow_event): %v", err)
	}
	var deliveredEvent struct {
		ID         string         `json:"id"`
		Type       string         `json:"type"`
		Source     string         `json:"source"`
		Subject    string         `json:"subject"`
		Data       map[string]any `json:"data"`
		Extensions map[string]any `json:"extensions"`
	}
	if err := json.Unmarshal(deliverEventResult.Body, &deliveredEvent); err != nil {
		t.Fatalf("json.Unmarshal(deliver event): %v", err)
	}
	if deliveredEvent.ID == "" || deliveredEvent.Type != "roadmap.item.updated" || deliveredEvent.Source != "roadmap" || deliveredEvent.Subject != "item-123" {
		t.Fatalf("unexpected delivered event result: %+v", deliveredEvent)
	}
	if deliveredEvent.Data["title"] != "Ship parity" || deliveredEvent.Extensions["tenant"] != "acme" {
		t.Fatalf("unexpected delivered event data: %+v", deliveredEvent)
	}

	if got := manager.Subjects(); len(got) != 6 || slices.Contains(got, "") || !slices.Equal(got, []string{
		"user:user-123",
		"user:user-123",
		"user:user-123",
		"user:user-123",
		"user:user-123",
		"user:user-123",
	}) {
		t.Fatalf("manager subjects = %v, want all user:user-123", got)
	}
}

func TestPluginInvokesInheritAmbientConnectionAndAllowOverride(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name                string
		email               string
		outerConnection     string
		outerInstance       string
		invokeConnection    string
		wantConnection      string
		wantInstance        string
		wantOverrideApplied bool
	}{
		{
			name:            "inherits ambient connection",
			email:           "nested-ambient-success@test.com",
			outerConnection: "work",
			wantConnection:  "work",
			wantInstance:    "default",
		},
		{
			name:                "uses explicit connection override",
			email:               "nested-override-success@test.com",
			outerConnection:     "work",
			outerInstance:       "primary",
			invokeConnection:    "backup",
			wantConnection:      "backup",
			wantInstance:        "default",
			wantOverrideApplied: true,
		},
		{
			name:                "ignores whitespace-only connection override",
			email:               "nested-whitespace-override-success@test.com",
			outerConnection:     "work",
			outerInstance:       "primary",
			invokeConnection:    "   ",
			wantConnection:      "work",
			wantInstance:        "primary",
			wantOverrideApplied: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			harness := newNestedInvokeHarness(t)
			ctx := context.Background()
			user := newNestedInvokeUser(t, harness, ctx, tc.email)
			storeNestedInvokeToken(t, harness, ctx, user.ID, "caller", "work", "default")
			if tc.outerInstance != "" {
				storeNestedInvokeToken(t, harness, ctx, user.ID, "caller", "work", tc.outerInstance)
			}
			storeNestedInvokeToken(t, harness, ctx, user.ID, "example", "work", "default")
			if tc.outerInstance != "" && strings.TrimSpace(tc.invokeConnection) == "" {
				storeNestedInvokeToken(t, harness, ctx, user.ID, "example", "work", tc.outerInstance)
			}
			storeNestedInvokeToken(t, harness, ctx, user.ID, "example", "backup", "default")

			invokeCtx := invocation.WithConnection(context.Background(), tc.outerConnection)
			callerPrincipal := &principal.Principal{
				UserID:      user.ID,
				Kind:        principal.KindUser,
				Source:      principal.SourceBearer,
				DisplayName: "Nested Success",
				Scopes:      []string{"caller", "example"},
			}

			params := map[string]any{
				"app":       "example",
				"operation": "request_context",
			}
			if tc.invokeConnection != "" {
				params["connection"] = tc.invokeConnection
			}

			result, err := harness.invoker.Invoke(invokeCtx, callerPrincipal, "caller", tc.outerInstance, "invoke_plugin", params)
			if err != nil {
				t.Fatalf("Invoke(caller.invoke_plugin): %v", err)
			}
			if result.Status != http.StatusOK {
				t.Fatalf("status = %d, want %d", result.Status, http.StatusOK)
			}

			var got invokePluginEnvelope
			if err := json.Unmarshal(result.Body, &got); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}
			if !got.OK {
				t.Fatalf("invoke_plugin returned error envelope: %+v", got)
			}
			if got.TargetApp != "example" || got.TargetOperation != "request_context" {
				t.Fatalf("unexpected target: %+v", got)
			}
			if got.UsedConnectionOverride != tc.wantOverrideApplied {
				t.Fatalf("used_connection_override = %v, want %v", got.UsedConnectionOverride, tc.wantOverrideApplied)
			}
			if got.Status != http.StatusOK {
				t.Fatalf("nested status = %d, want %d", got.Status, http.StatusOK)
			}
			if got.Body.Credential.Connection != tc.wantConnection {
				t.Fatalf("nested credential.connection = %q, want %q", got.Body.Credential.Connection, tc.wantConnection)
			}
			if got.Body.Credential.Instance != tc.wantInstance {
				t.Fatalf("nested credential.instance = %q, want %q", got.Body.Credential.Instance, tc.wantInstance)
			}
			if got.Body.Subject.ID != principal.UserSubjectID(user.ID) {
				t.Fatalf("nested subject.id = %q, want %q", got.Body.Subject.ID, principal.UserSubjectID(user.ID))
			}
		})
	}
}

func TestPluginInvokesInheritResolvedCredentialConnection(t *testing.T) {
	t.Parallel()

	harness := newNestedInvokeHarness(t, invocation.WithConnectionMapper(invocation.ConnectionMap{
		"caller": "work",
	}))
	ctx := context.Background()
	user := newNestedInvokeUser(t, harness, ctx, "nested-resolved-connection@test.com")
	storeNestedInvokeToken(t, harness, ctx, user.ID, "caller", "work", "default")
	storeNestedInvokeToken(t, harness, ctx, user.ID, "example", "work", "default")

	result, err := harness.invoker.Invoke(
		context.Background(),
		&principal.Principal{
			UserID: user.ID,
			Kind:   principal.KindUser,
			Source: principal.SourceBearer,
			Scopes: []string{"caller", "example"},
		},
		"caller",
		"",
		"invoke_plugin",
		map[string]any{
			"app":       "example",
			"operation": "request_context",
		},
	)
	if err != nil {
		t.Fatalf("Invoke(caller.invoke_plugin): %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", result.Status, http.StatusOK)
	}

	var got invokePluginEnvelope
	if err := json.Unmarshal(result.Body, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if !got.OK {
		t.Fatalf("invoke_plugin returned error envelope: %+v", got)
	}
	if got.Body.Credential.Connection != "work" {
		t.Fatalf("nested credential.connection = %q, want %q", got.Body.Credential.Connection, "work")
	}
}

func TestPluginInvokesPreserveCallerScopes(t *testing.T) {
	t.Parallel()

	harness := newNestedInvokeHarness(t)
	ctx := context.Background()
	user := newNestedInvokeUser(t, harness, ctx, "nested-scope@test.com")
	storeNestedInvokeToken(t, harness, ctx, user.ID, "caller", "work", "default")
	storeNestedInvokeToken(t, harness, ctx, user.ID, "example", "work", "default")

	result, err := harness.invoker.Invoke(
		invocation.WithConnection(context.Background(), "work"),
		&principal.Principal{
			UserID: user.ID,
			Kind:   principal.KindUser,
			Source: principal.SourceBearer,
			Scopes: []string{"caller"},
		},
		"caller",
		"",
		"invoke_plugin",
		map[string]any{
			"app":       "example",
			"operation": "request_context",
		},
	)
	if err != nil {
		t.Fatalf("Invoke(caller.invoke_plugin): %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", result.Status, http.StatusOK)
	}

	var got invokePluginEnvelope
	if err := json.Unmarshal(result.Body, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got.OK {
		t.Fatalf("expected scope denial envelope, got success: %+v", got)
	}
	if !strings.Contains(got.Error, invocation.ErrScopeDenied.Error()) || !strings.Contains(got.Error, "example") {
		t.Fatalf("scope denial error = %q, want token scope denied for example", got.Error)
	}
}

func TestPluginInvokesSupportInvokerFromContext(t *testing.T) {
	t.Parallel()

	harness := newNestedInvokeHarness(t)
	ctx := context.Background()
	user := newNestedInvokeUser(t, harness, ctx, "nested-context-invoker@test.com")
	storeNestedInvokeToken(t, harness, ctx, user.ID, "example", "work", "primary")
	storeNestedInvokeToken(t, harness, ctx, user.ID, "example", "work", "secondary")

	result, err := harness.invoker.Invoke(
		invocation.WithConnection(context.Background(), "work"),
		&principal.Principal{
			UserID: user.ID,
			Kind:   principal.KindUser,
			Source: principal.SourceBearer,
			Scopes: []string{"example"},
		},
		"example",
		"primary",
		"invoke_request_context",
		nil,
	)
	if err != nil {
		t.Fatalf("Invoke(example.invoke_request_context): %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", result.Status, http.StatusOK)
	}

	var got requestContextBody
	if err := json.Unmarshal(result.Body, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got.Credential.Connection != "work" {
		t.Fatalf("nested credential.connection = %q, want %q", got.Credential.Connection, "work")
	}
	if got.Credential.Instance != "primary" {
		t.Fatalf("nested credential.instance = %q, want %q", got.Credential.Instance, "primary")
	}
}

func TestPluginInvokesGraphQLSurface(t *testing.T) {
	t.Parallel()

	type capturedGraphQLRequest struct {
		Query         string
		Variables     map[string]any
		Authorization string
	}

	var (
		mu                 sync.Mutex
		captured           []capturedGraphQLRequest
		introspectionCalls atomic.Int32
	)
	schema := pluginInvokeGraphQLSchema()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(payload.Query, "__schema") {
			introspectionCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"__schema": schema,
				},
			})
			return
		}
		mu.Lock()
		captured = append(captured, capturedGraphQLRequest{
			Query:         payload.Query,
			Variables:     maps.Clone(payload.Variables),
			Authorization: r.Header.Get("Authorization"),
		})
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"echo": map[string]any{
					"authorization": r.Header.Get("Authorization"),
					"query":         payload.Query,
					"variables":     payload.Variables,
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	harness := newGraphQLSurfaceInvokeHarness(t, srv.URL, true, config.AuthorizationConfig{})
	ctx := context.Background()
	user := newNestedInvokeUser(t, harness, ctx, "nested-graphql-surface@test.com")
	storeNestedInvokeToken(t, harness, ctx, user.ID, "caller", "work", "default")
	storeNestedInvokeToken(t, harness, ctx, user.ID, "linear", "work", "default")

	document := "query Viewer($team: String!) { viewer(team: $team) { id } }"
	result, err := harness.invoker.Invoke(
		invocation.WithConnection(context.Background(), "work"),
		&principal.Principal{
			UserID: user.ID,
			Kind:   principal.KindUser,
			Source: principal.SourceBearer,
			Scopes: []string{"caller", "linear"},
		},
		"caller",
		"",
		"invoke_plugin_graphql",
		map[string]any{
			"app":      "linear",
			"document": document,
			"variables": map[string]any{
				"team": "eng",
			},
		},
	)
	if err != nil {
		t.Fatalf("Invoke(caller.invoke_plugin_graphql): %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", result.Status, http.StatusOK)
	}

	var got struct {
		OK                     bool           `json:"ok"`
		TargetApp              string         `json:"target_app"`
		TargetOperation        string         `json:"target_operation"`
		UsedConnectionOverride bool           `json:"used_connection_override"`
		Status                 int            `json:"status"`
		Body                   map[string]any `json:"body"`
		Error                  string         `json:"error"`
	}
	if err := json.Unmarshal(result.Body, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if !got.OK {
		t.Fatalf("invoke_plugin_graphql returned error envelope: %+v", got)
	}
	if got.TargetApp != "linear" || got.TargetOperation != "graphql" {
		t.Fatalf("unexpected target: %+v", got)
	}
	if got.UsedConnectionOverride {
		t.Fatalf("used_connection_override = %v, want false", got.UsedConnectionOverride)
	}
	if got.Status != http.StatusOK {
		t.Fatalf("nested status = %d, want %d", got.Status, http.StatusOK)
	}

	echo, ok := got.Body["echo"].(map[string]any)
	if !ok {
		t.Fatalf("body.echo = %#v, want object", got.Body["echo"])
	}
	if echo["authorization"] != "Bearer linear-work-token" {
		t.Fatalf("body.echo.authorization = %#v, want %q", echo["authorization"], "Bearer linear-work-token")
	}
	if echo["query"] != document {
		t.Fatalf("body.echo.query = %#v, want %q", echo["query"], document)
	}
	variables, ok := echo["variables"].(map[string]any)
	if !ok {
		t.Fatalf("body.echo.variables = %#v, want object", echo["variables"])
	}
	if variables["team"] != "eng" {
		t.Fatalf("body.echo.variables.team = %#v, want %q", variables["team"], "eng")
	}

	missingUser := newNestedInvokeUser(t, harness, ctx, "nested-graphql-surface-missing@test.com")
	storeNestedInvokeToken(t, harness, ctx, missingUser.ID, "caller", "work", "default")
	missingResult, err := harness.invoker.Invoke(
		invocation.WithConnection(context.Background(), "work"),
		&principal.Principal{
			UserID: missingUser.ID,
			Kind:   principal.KindUser,
			Source: principal.SourceBearer,
			Scopes: []string{"caller", "linear"},
		},
		"caller",
		"",
		"invoke_plugin_graphql",
		map[string]any{
			"app":      "linear",
			"document": document,
			"variables": map[string]any{
				"team": "eng",
			},
		},
	)
	if err != nil {
		t.Fatalf("Invoke(caller.invoke_plugin_graphql missing credential): %v", err)
	}
	var missingGot struct {
		OK     bool   `json:"ok"`
		Status int    `json:"status"`
		Body   any    `json:"body"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(missingResult.Body, &missingGot); err != nil {
		t.Fatalf("json.Unmarshal missing credential: %v", err)
	}
	if !missingGot.OK {
		t.Fatalf("expected missing credential operation result, got error: %+v", missingGot)
	}
	if missingGot.Status != http.StatusPreconditionFailed {
		t.Fatalf("missing credential nested status = %d, want %d", missingGot.Status, http.StatusPreconditionFailed)
	}
	body, ok := missingGot.Body.(string)
	if !ok || !strings.Contains(body, `no external credential stored for integration "linear"`) {
		t.Fatalf("missing credential body = %#v, want missing linear credential", missingGot.Body)
	}

	if got := introspectionCalls.Load(); got != 0 {
		t.Fatalf("introspection calls = %d, want 0", got)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(captured) != 1 {
		t.Fatalf("captured graphql requests = %d, want 1", len(captured))
	}
	if captured[0].Query != document {
		t.Fatalf("captured query = %q, want %q", captured[0].Query, document)
	}
	if captured[0].Authorization != "Bearer linear-work-token" {
		t.Fatalf("captured authorization = %q, want %q", captured[0].Authorization, "Bearer linear-work-token")
	}
	if captured[0].Variables["team"] != "eng" {
		t.Fatalf("captured variables.team = %#v, want %q", captured[0].Variables["team"], "eng")
	}
}

func TestPluginInvokesDoNotLeakCallerAccessToPolicylessTargets(t *testing.T) {
	t.Parallel()

	harness := newNestedInvokeHarness(t)
	ctx := context.Background()
	user := newNestedInvokeUser(t, harness, ctx, "nested-access@test.com")
	storeNestedInvokeToken(t, harness, ctx, user.ID, "caller", "work", "default")
	storeNestedInvokeToken(t, harness, ctx, user.ID, "example", "work", "default")

	invokeCtx := invocation.WithConnection(context.Background(), "work")
	invokeCtx = invocation.WithAccessContext(invokeCtx, invocation.AccessContext{
		Policy: "caller-policy",
		Role:   "admin",
	})

	result, err := harness.invoker.Invoke(
		invokeCtx,
		&principal.Principal{
			UserID: user.ID,
			Kind:   principal.KindUser,
			Source: principal.SourceBearer,
			Scopes: []string{"caller", "example"},
		},
		"caller",
		"",
		"invoke_plugin",
		map[string]any{
			"app":       "example",
			"operation": "request_context",
		},
	)
	if err != nil {
		t.Fatalf("Invoke(caller.invoke_plugin): %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", result.Status, http.StatusOK)
	}

	var got invokePluginEnvelope
	if err := json.Unmarshal(result.Body, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if !got.OK {
		t.Fatalf("invoke_plugin returned error envelope: %+v", got)
	}
	if got.Body.Access.Policy != "" || got.Body.Access.Role != "" {
		t.Fatalf("nested access leaked caller context: %+v", got.Body.Access)
	}
}

func TestPluginInvokesRejectInvalidTargetRequests(t *testing.T) {
	t.Parallel()

	type tokenSpec struct {
		plugin     string
		connection string
		instance   string
	}
	tests := []struct {
		name              string
		email             string
		tokens            []tokenSpec
		params            map[string]any
		wantStatus        int
		wantBodySubstring string
		wantError         string
	}{
		{
			name:  "missing target credential returns operation result",
			email: "nested-no-target-token@test.com",
			tokens: []tokenSpec{
				{plugin: "caller", connection: "work", instance: "default"},
			},
			params: map[string]any{
				"app":       "example",
				"operation": "request_context",
			},
			wantStatus:        http.StatusPreconditionFailed,
			wantBodySubstring: `no external credential stored for integration "example"`,
		},
		{
			name:  "ambiguous target instance",
			email: "nested-ambiguous-target@test.com",
			tokens: []tokenSpec{
				{plugin: "caller", connection: "work", instance: "default"},
				{plugin: "example", connection: "work", instance: "primary"},
				{plugin: "example", connection: "work", instance: "secondary"},
			},
			params: map[string]any{
				"app":        "example",
				"operation":  "request_context",
				"connection": "work",
			},
			wantError: "code = Aborted",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			harness := newNestedInvokeHarness(t)
			ctx := context.Background()
			user := newNestedInvokeUser(t, harness, ctx, tc.email)
			for _, token := range tc.tokens {
				storeNestedInvokeToken(t, harness, ctx, user.ID, token.plugin, token.connection, token.instance)
			}

			result, err := harness.invoker.Invoke(
				invocation.WithConnection(context.Background(), "work"),
				&principal.Principal{
					UserID: user.ID,
					Kind:   principal.KindUser,
					Source: principal.SourceBearer,
					Scopes: []string{"caller", "example"},
				},
				"caller",
				"",
				"invoke_plugin",
				tc.params,
			)
			if err != nil {
				t.Fatalf("Invoke(caller.invoke_plugin): %v", err)
			}

			var got struct {
				OK     bool   `json:"ok"`
				Status int    `json:"status"`
				Body   any    `json:"body"`
				Error  string `json:"error"`
			}
			if err := json.Unmarshal(result.Body, &got); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}
			if tc.wantError == "" {
				if !got.OK {
					t.Fatalf("expected success envelope, got error: %+v", got)
				}
				if got.Status != tc.wantStatus {
					t.Fatalf("nested status = %d, want %d", got.Status, tc.wantStatus)
				}
				body, ok := got.Body.(string)
				if !ok || !strings.Contains(body, tc.wantBodySubstring) {
					t.Fatalf("body = %#v, want substring %q", got.Body, tc.wantBodySubstring)
				}
				return
			}
			if got.OK {
				t.Fatalf("expected error envelope, got success: %+v", got)
			}
			if !strings.Contains(got.Error, tc.wantError) {
				t.Fatalf("error = %q, want substring %q", got.Error, tc.wantError)
			}
		})
	}
}

func TestPluginCacheBindingsExposeHostSocketEnv(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")

	makeConfig := func(bindings []string) *config.Config {
		return &config.Config{
			Apps: map[string]*config.ProviderEntry{
				"echoext": {
					Command:              bin,
					Args:                 []string{"provider"},
					ResolvedManifest:     manifest,
					ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
					Cache:                bindings,
				},
			},
		}
	}

	cacheBindings := map[string]*config.ProviderEntry{
		"session": {Config: mustNode(t, map[string]any{"namespace": "session"})},
		"rate_limit": {
			Config: mustNode(t, map[string]any{"namespace": "rate_limit"}),
		},
	}

	checkEnv := func(t *testing.T, bindings []string, envName string) bool {
		t.Helper()
		providers, _, err := buildProvidersStrict(context.Background(), makeConfig(bindings), NewFactoryRegistry(), testRuntimePublicEndpointDeps(t, Deps{
			CacheDefs: cacheBindings,
			CacheFactory: func(yaml.Node) (corecache.Cache, error) {
				return coretesting.NewStubCache(), nil
			},
		}))
		if err != nil {
			t.Fatalf("buildProvidersStrict: %v", err)
		}
		defer func() { _ = CloseProviders(providers) }()

		prov, err := providers.Get("echoext")
		if err != nil {
			t.Fatalf("providers.Get: %v", err)
		}
		result, err := prov.Execute(context.Background(), "read_env", map[string]any{"name": envName}, "")
		if err != nil {
			t.Fatalf("Execute read_env: %v", err)
		}
		var env struct {
			Value string `json:"value"`
			Found bool   `json:"found"`
		}
		if err := json.Unmarshal(result.Body, &env); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return env.Found && env.Value != ""
	}
	if got := checkEnv(t, []string{"session"}, runtimehost.HostServiceSocketEnv); !got {
		t.Fatal("unified host-service env should be set with a single app cache binding")
	}
	if got := checkEnv(t, []string{"session", "rate_limit"}, runtimehost.HostServiceSocketEnv); !got {
		t.Fatal("unified host-service env should be set with multiple app cache bindings")
	}
}

func TestInjectedRuntimeStopsSessionOnProviderClose(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	runtimeProvider := newCapturingRuntime()
	t.Cleanup(func() { _ = runtimeProvider.Close() })
	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
			},
		},
	}

	providers, _, err := buildProvidersStrict(context.Background(), cfg, NewFactoryRegistry(), testRuntimePublicEndpointDeps(t, Deps{
		Runtime: runtimeProvider,
	}))
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}

	prov, err := providers.Get("echoext")
	if err != nil {
		t.Fatalf("providers.Get: %v", err)
	}
	if _, err := prov.Execute(context.Background(), "read_env", map[string]any{"name": "PATH"}, ""); err != nil {
		t.Fatalf("Execute read_env: %v", err)
	}
	if err := CloseProviders(providers); err != nil {
		t.Fatalf("CloseProviders: %v", err)
	}
	if runtimeProvider.stopCount.Load() == 0 {
		t.Fatal("expected CloseProviders to stop the hosted runtime session")
	}
}

func TestRuntimeBackedHostedCloserStopSessionTimeout(t *testing.T) {
	t.Parallel()

	runtimeProvider := &blockingStopRuntime{}
	closer := &runtimeBackedHostedCloser{
		runtime:     runtimeProvider,
		sessionID:   "session-1",
		stopTimeout: 25 * time.Millisecond,
	}

	start := time.Now()
	err := closer.Close()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Close elapsed = %s, want short timeout", elapsed)
	}
	if got := runtimeProvider.stopCount.Load(); got != 1 {
		t.Fatalf("StopSession calls = %d, want 1", got)
	}
}

func TestRuntimeConfigSelectedProviderStartsSessionWithRuntimeFields(t *testing.T) {
	t.Parallel()

	type runtimeFactoryContextKey struct{}

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	imageEntrypointDir, err := os.MkdirTemp(".", "plugin-image-entrypoint-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(imageEntrypointDir) })
	imageEntrypoint := filepath.Join(imageEntrypointDir, "app")
	pluginBytes, err := os.ReadFile(bin)
	if err != nil {
		t.Fatalf("ReadFile(plugin bin): %v", err)
	}
	if err := os.WriteFile(imageEntrypoint, pluginBytes, 0o755); err != nil {
		t.Fatalf("WriteFile(image entrypoint): %v", err)
	}
	manifest.Entrypoint = &providermanifestv1.Entrypoint{
		ArtifactPath: filepath.ToSlash(imageEntrypoint),
		Args:         []string{"provider"},
	}
	runtimeProvider := newCapturingRuntime()
	ctxSentinel := &struct{}{}
	var factoryContextValue any
	factories := NewFactoryRegistry()
	factories.Runtime = func(ctx context.Context, _ string, _ *config.RuntimeProviderEntry, _ Deps) (runtimeprovider.Provider, error) {
		factoryContextValue = ctx.Value(runtimeFactoryContextKey{})
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Apps: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Runtime: &config.RuntimePlacementConfig{
					Template: "python-dev",
					Image:    "ghcr.io/valon/gestalt-python-runtime:latest",
					Metadata: map[string]string{"tenant": "eng"},
				},
			},
		},
	}

	deps := testRuntimePublicEndpointDeps(t, Deps{})
	deps.RuntimeRegistry = newRuntimeRegistry(cfg, factories.Runtime, deps)
	buildCtx := context.WithValue(context.Background(), runtimeFactoryContextKey{}, ctxSentinel)
	providers, _, err := buildProvidersStrict(buildCtx, cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}

	prov, err := providers.Get("echoext")
	if err != nil {
		t.Fatalf("providers.Get: %v", err)
	}
	if _, err := prov.Execute(context.Background(), "read_env", map[string]any{"name": "PATH"}, ""); err != nil {
		t.Fatalf("Execute read_env: %v", err)
	}
	if err := CloseProviders(providers); err != nil {
		t.Fatalf("CloseProviders: %v", err)
	}

	requests := runtimeProvider.startSessionRequests()
	if len(requests) != 1 {
		t.Fatalf("start session requests = %d, want 1", len(requests))
	}
	req := requests[0]
	if req.GetAppName() != "echoext" {
		t.Fatalf("StartSession AppName = %q, want echoext", req.GetAppName())
	}
	if req.GetTemplate() != "python-dev" {
		t.Fatalf("StartSession Template = %q, want python-dev", req.GetTemplate())
	}
	if req.GetImage() != "ghcr.io/valon/gestalt-python-runtime:latest" {
		t.Fatalf("StartSession Image = %q", req.GetImage())
	}
	if req.GetMetadata()["tenant"] != "eng" {
		t.Fatalf("StartSession Metadata[tenant] = %q, want eng", req.GetMetadata()["tenant"])
	}
	if req.GetMetadata()["provider_kind"] != "app" {
		t.Fatalf("StartSession Metadata[provider_kind] = %q, want plugin", req.GetMetadata()["provider_kind"])
	}
	if req.GetMetadata()["provider_name"] != "echoext" {
		t.Fatalf("StartSession Metadata[provider_name] = %q, want echoext", req.GetMetadata()["provider_name"])
	}
	if factoryContextValue != ctxSentinel {
		t.Fatalf("runtime factory context value = %#v, want %#v", factoryContextValue, ctxSentinel)
	}
}

func TestRuntimeStartsHostedCommandWithoutBundleStaging(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	artifactPath := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, filepath.Base(bin)))
	artifactBytes, err := os.ReadFile(bin)
	if err != nil {
		t.Fatalf("ReadFile(binary): %v", err)
	}
	artifactFile := filepath.Join(manifestRoot, filepath.FromSlash(artifactPath))
	if err := os.MkdirAll(filepath.Dir(artifactFile), 0o755); err != nil {
		t.Fatalf("MkdirAll(artifact dir): %v", err)
	}
	if err := os.WriteFile(artifactFile, artifactBytes, 0o755); err != nil {
		t.Fatalf("WriteFile(artifact): %v", err)
	}
	digest, err := providerpkg.FileSHA256(artifactFile)
	if err != nil {
		t.Fatalf("FileSHA256(artifact): %v", err)
	}
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	manifest.Artifacts = []providermanifestv1.Artifact{{
		OS:     runtime.GOOS,
		Arch:   runtime.GOARCH,
		Path:   artifactPath,
		SHA256: digest,
	}}
	manifest.Entrypoint = &providermanifestv1.Entrypoint{ArtifactPath: artifactPath}
	manifestData, err := providerpkg.EncodeManifestFormat(manifest, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeManifestFormat(manifest): %v", err)
	}
	manifestPath := filepath.Join(manifestRoot, "manifest.yaml")
	if err := os.WriteFile(manifestPath, manifestData, 0o644); err != nil {
		t.Fatalf("WriteFile(manifest.yaml): %v", err)
	}

	runtimeProvider := newCapturingBundleRuntime()
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (runtimeprovider.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Apps: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: manifestPath,
				Runtime:              &config.RuntimePlacementConfig{},
			},
		},
	}

	deps := testRuntimePublicEndpointDeps(t, Deps{})
	deps.RuntimeRegistry = newRuntimeRegistry(cfg, factories.Runtime, deps)
	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}

	prov, err := providers.Get("echoext")
	if err != nil {
		t.Fatalf("providers.Get(echoext): %v", err)
	}
	if _, err := prov.Execute(context.Background(), "read_env", map[string]any{"name": "PATH"}, ""); err != nil {
		t.Fatalf("Execute(read_env): %v", err)
	}

	requests := runtimeProvider.startAppRequestsCopy()
	if len(requests) != 1 {
		t.Fatalf("start app requests = %d, want 1", len(requests))
	}
	req := requests[0]
	if req.GetCommand() != bin {
		t.Fatalf("StartApp Command = %q, want configured command", req.GetCommand())
	}
	if !slices.Equal(req.GetArgs(), []string{"provider"}) {
		t.Fatalf("StartApp Args = %#v, want configured args", req.GetArgs())
	}

	if err := CloseProviders(providers); err != nil {
		t.Fatalf("CloseProviders: %v", err)
	}
}

func TestRuntimeImageLaunchUsesManifestEntrypoint(t *testing.T) {
	t.Parallel()

	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	manifest.Entrypoint = &providermanifestv1.Entrypoint{
		ArtifactPath: "bin/echo",
		Args:         []string{"--config", "/etc/gestalt/echo.yaml"},
	}

	runtimeProvider := newCapturingBundleRuntime()
	runtimeProvider.fakeHosted = true
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (runtimeprovider.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Apps: map[string]*config.ProviderEntry{
			"echoext": {
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Runtime: &config.RuntimePlacementConfig{
					Image: "ghcr.io/example/echo-plugin@sha256:abc123",
				},
			},
		},
	}

	deps := testRuntimePublicEndpointDeps(t, Deps{})
	deps.RuntimeRegistry = newRuntimeRegistry(cfg, factories.Runtime, deps)
	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() {
		if err := CloseProviders(providers); err != nil {
			t.Fatalf("CloseProviders: %v", err)
		}
	}()

	requests := runtimeProvider.startAppRequestsCopy()
	if len(requests) != 1 {
		t.Fatalf("start app requests = %d, want 1", len(requests))
	}
	req := requests[0]
	if req.GetCommand() != "./bin/echo" {
		t.Fatalf("StartApp Command = %q, want manifest image entrypoint", req.GetCommand())
	}
	if !slices.Equal(req.GetArgs(), []string{"--config", "/etc/gestalt/echo.yaml"}) {
		t.Fatalf("StartApp Args = %#v, want manifest image args", req.GetArgs())
	}
}

func TestRuntimeTemplateLaunchUsesManifestEntrypoint(t *testing.T) {
	t.Parallel()

	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	manifest.Entrypoint = &providermanifestv1.Entrypoint{
		ArtifactPath: "bin/echo",
		Args:         []string{"--config", "/etc/gestalt/echo.yaml"},
	}

	runtimeProvider := newCapturingBundleRuntime()
	runtimeProvider.fakeHosted = true
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (runtimeprovider.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Apps: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              "/host/only/plugin",
				Args:                 []string{"host-arg"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Runtime: &config.RuntimePlacementConfig{
					Template: "python-runtime",
				},
			},
		},
	}

	deps := testRuntimePublicEndpointDeps(t, Deps{})
	deps.RuntimeRegistry = newRuntimeRegistry(cfg, factories.Runtime, deps)
	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() {
		if err := CloseProviders(providers); err != nil {
			t.Fatalf("CloseProviders: %v", err)
		}
	}()

	requests := runtimeProvider.startAppRequestsCopy()
	if len(requests) != 1 {
		t.Fatalf("start app requests = %d, want 1", len(requests))
	}
	req := requests[0]
	if req.GetCommand() != "./bin/echo" {
		t.Fatalf("StartApp Command = %q, want manifest template entrypoint", req.GetCommand())
	}
	if !slices.Equal(req.GetArgs(), []string{"--config", "/etc/gestalt/echo.yaml"}) {
		t.Fatalf("StartApp Args = %#v, want manifest template args", req.GetArgs())
	}
}

func TestRuntimeLocalFallbackImageLaunchUsesConfiguredCommand(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	manifest.Entrypoint = &providermanifestv1.Entrypoint{
		ArtifactPath: "bin/echo",
		Args:         []string{"--config", "/etc/gestalt/echo.yaml"},
	}

	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Runtime: &config.RuntimePlacementConfig{
					Image: "ghcr.io/example/echo-plugin@sha256:abc123",
				},
			},
		},
	}

	providers, _, err := buildProvidersStrict(context.Background(), cfg, NewFactoryRegistry(), testRuntimePublicEndpointDeps(t, Deps{}))
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() {
		if err := CloseProviders(providers); err != nil {
			t.Fatalf("CloseProviders: %v", err)
		}
	}()

	prov, err := providers.Get("echoext")
	if err != nil {
		t.Fatalf("providers.Get(echoext): %v", err)
	}
	if _, err := prov.Execute(context.Background(), "read_env", map[string]any{"name": "PATH"}, ""); err != nil {
		t.Fatalf("Execute(read_env): %v", err)
	}
}

func TestRuntimeConfigUsesPublicS3RelayWithoutHostServiceTunnelCapability(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	runtimeProvider := newCapturingBundleRuntime()
	runtimeProvider.support.EgressMode = proto.RuntimeEgressMode_RUNTIME_EGRESS_MODE_HOSTNAME
	runtimeProvider.fakeHosted = true
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (runtimeprovider.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultProvider: "hosted"},
			Egress:  config.EgressConfig{DefaultAction: string(egress.PolicyDeny)},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Apps: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Runtime:              &config.RuntimePlacementConfig{},
				S3:                   []string{"main"},
			},
		},
	}

	deps := Deps{
		BaseURL:       "https://gestalt.example.test",
		EncryptionKey: []byte("0123456789abcdef0123456789abcdef"),
		Egress:        newEgressDeps(cfg),
		S3: map[string]s3sdk.S3{
			"main":    &coretesting.StubS3{},
			"archive": &coretesting.StubS3{},
		},
	}
	deps.RuntimeRegistry = newRuntimeRegistry(cfg, factories.Runtime, deps)

	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })

	prov, err := providers.Get("echoext")
	if err != nil {
		t.Fatalf("providers.Get: %v", err)
	}
	checkEnv := func(envName string) (string, bool) {
		t.Helper()
		result, err := prov.Execute(context.Background(), "read_env", map[string]any{"name": envName}, "")
		if err != nil {
			t.Fatalf("Execute read_env(%s): %v", envName, err)
		}
		var env struct {
			Value string `json:"value"`
			Found bool   `json:"found"`
		}
		if err := json.Unmarshal(result.Body, &env); err != nil {
			t.Fatalf("unmarshal env result for %s: %v", envName, err)
		}
		return env.Value, env.Found
	}

	if got, found := checkEnv(runtimehost.HostServiceSocketEnv); !found || got != "tls://gestalt.example.test:443" {
		t.Fatalf("plugin host-service env %s = (%q, %v), want (%q, true)", runtimehost.HostServiceSocketEnv, got, found, "tls://gestalt.example.test:443")
	}
	if got, found := checkEnv(runtimehost.HostServiceTokenEnv); !found || got == "" {
		t.Fatalf("plugin host-service token env %s = (%q, %v), want non-empty token", runtimehost.HostServiceTokenEnv, got, found)
	}

	startRequests := runtimeProvider.startAppRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("StartApp requests = %d, want 1", len(startRequests))
	}
	assertStartAppRelayEnv(t, startRequests[0], runtimehost.HostServiceSocketEnv)
	if allowedHosts := slices.Clone(startRequests[0].GetAllowedHosts()); !slices.Contains(allowedHosts, "gestalt.example.test") {
		t.Fatalf("StartApp allowed hosts = %#v, want relay host gestalt.example.test", allowedHosts)
	}
}

func TestRuntimeConfigUsesPublicIndexedDBRelayWithoutHostServiceTunnelCapability(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	runtimeProvider := newCapturingBundleRuntime()
	runtimeProvider.support.EgressMode = proto.RuntimeEgressMode_RUNTIME_EGRESS_MODE_HOSTNAME
	runtimeProvider.fakeHosted = true
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (runtimeprovider.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultProvider: "hosted"},
			Egress:  config.EgressConfig{DefaultAction: string(egress.PolicyDeny)},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Apps: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Runtime:              &config.RuntimePlacementConfig{},
				IndexedDB:            &config.IndexedDBBindingConfig{ObjectStores: []string{"tasks"}},
			},
		},
	}

	deps := Deps{
		BaseURL:               "https://gestalt.example.test",
		EncryptionKey:         []byte("0123456789abcdef0123456789abcdef"),
		Egress:                newEgressDeps(cfg),
		SelectedIndexedDBName: "memory",
		IndexedDBDefs: map[string]*config.ProviderEntry{
			"memory": {
				Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.2/provider-release.yaml"),
				Config: mustNode(t, map[string]any{"bucket": "plugin-state"}),
			},
		},
		IndexedDBFactory: func(yaml.Node) (indexeddb.IndexedDB, error) {
			return &coretesting.StubIndexedDB{}, nil
		},
	}
	deps.RuntimeRegistry = newRuntimeRegistry(cfg, factories.Runtime, deps)

	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })

	prov, err := providers.Get("echoext")
	if err != nil {
		t.Fatalf("providers.Get: %v", err)
	}
	checkEnv := func(envName string) string {
		t.Helper()
		result, err := prov.Execute(context.Background(), "read_env", map[string]any{"name": envName}, "")
		if err != nil {
			t.Fatalf("Execute read_env(%s): %v", envName, err)
		}
		var env struct {
			Value string `json:"value"`
			Found bool   `json:"found"`
		}
		if err := json.Unmarshal(result.Body, &env); err != nil {
			t.Fatalf("unmarshal env result for %s: %v", envName, err)
		}
		if !env.Found {
			t.Fatalf("env %s not found", envName)
		}
		return env.Value
	}

	if got := checkEnv(runtimehost.HostServiceSocketEnv); got != "tls://gestalt.example.test:443" {
		t.Fatalf("plugin indexeddb socket env = %q, want %q", got, "tls://gestalt.example.test:443")
	}
	if got := checkEnv(runtimehost.HostServiceTokenEnv); got == "" {
		t.Fatal("plugin host-service token env should be set for the public relay")
	}

	startRequests := runtimeProvider.startAppRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("StartApp requests = %d, want 1", len(startRequests))
	}
	assertStartAppRelayEnv(t, startRequests[0], "indexeddb")
	if allowedHosts := slices.Clone(startRequests[0].GetAllowedHosts()); !slices.Contains(allowedHosts, "gestalt.example.test") {
		t.Fatalf("StartApp allowed hosts = %#v, want relay host gestalt.example.test", allowedHosts)
	}
}

func TestRuntimePublicIndexedDBRelayRoundTripsThroughHostedApp(t *testing.T) {
	t.Parallel()

	secret := []byte("0123456789abcdef0123456789abcdef")
	publicHostServices := runtimehost.NewPublicHostServiceRegistry()
	relaySrv := httptest.NewUnstartedServer(newRuntimeRelayTestHandler(t, secret, publicHostServices))
	relaySrv.EnableHTTP2 = true
	relaySrv.StartTLS()
	testutil.CloseOnCleanup(t, relaySrv)

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{
				ID:     "indexeddb_roundtrip",
				Method: http.MethodPost,
				Parameters: []catalog.CatalogParameter{
					{Name: "store", Type: "string", Required: true},
					{Name: "id", Type: "string", Required: true},
					{Name: "value", Type: "string", Required: true},
				},
			},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	runtimeProvider := newCapturingBundleRuntime()
	runtimeProvider.support.EgressMode = proto.RuntimeEgressMode_RUNTIME_EGRESS_MODE_HOSTNAME
	runtimeProvider.fakeHosted = true

	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (runtimeprovider.Provider, error) {
		return runtimeProvider, nil
	}

	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {Driver: config.RuntimeProviderDriver("capture")},
			},
		},
		Apps: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Runtime:              &config.RuntimePlacementConfig{},
				IndexedDB:            &config.IndexedDBBindingConfig{ObjectStores: []string{"tasks"}},
			},
		},
	}

	boundDB := &trackedIndexedDB{StubIndexedDB: coretesting.StubIndexedDB{}}
	deps := Deps{
		BaseURL:               relaySrv.URL,
		EncryptionKey:         secret,
		SelectedIndexedDBName: "memory",
		IndexedDBs: map[string]indexeddb.IndexedDB{
			"memory": boundDB,
		},
		IndexedDBDefs: map[string]*config.ProviderEntry{
			"memory": {
				Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.2/provider-release.yaml"),
				Config: mustNode(t, map[string]any{"bucket": "plugin-state"}),
			},
		},
		IndexedDBFactory: func(yaml.Node) (indexeddb.IndexedDB, error) {
			return boundDB, nil
		},
		PublicHostServices: publicHostServices,
	}
	deps.RuntimeRegistry = newRuntimeRegistry(cfg, factories.Runtime, deps)

	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })

	prov, err := providers.Get("echoext")
	if err != nil {
		t.Fatalf("providers.Get: %v", err)
	}

	result, err := prov.Execute(context.Background(), "indexeddb_roundtrip", map[string]any{
		"store": "tasks",
		"id":    "task-1",
		"value": "ship-it",
	}, "")
	if err != nil {
		t.Fatalf("Execute indexeddb_roundtrip: %v", err)
	}

	var record map[string]any
	if err := json.Unmarshal(result.Body, &record); err != nil {
		t.Fatalf("unmarshal indexeddb_roundtrip: %v", err)
	}
	if got := record["value"]; got != "ship-it" {
		t.Fatalf("indexeddb_roundtrip value = %#v, want %q", got, "ship-it")
	}

	gotRecord, err := boundDB.ObjectStore("tasks").Get(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("bound IndexedDB Get: %v", err)
	}
	if got := gotRecord["value"]; got != "ship-it" {
		t.Fatalf("bound IndexedDB stored value = %#v, want %q", got, "ship-it")
	}

	startRequests := runtimeProvider.startAppRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("StartApp requests = %d, want 1", len(startRequests))
	}
	if got := startRequests[0].GetEnv()[runtimehost.HostServiceTokenEnv]; got == "" {
		t.Fatal("StartApp env should include the host-service relay token")
	}
	if got := startRequests[0].GetEnv()[runtimehost.HostServiceSocketEnv]; !strings.HasPrefix(got, "tls://") {
		t.Fatalf("StartApp env %s = %q, want tls relay target", runtimehost.HostServiceSocketEnv, got)
	}

	expiredAt := time.Now().Add(-time.Minute)
	runtimeProvider.setSessionLifecycle(startRequests[0].GetSessionId(), &proto.RuntimeSessionLifecycle{
		ExpiresAt: timestamppb.New(expiredAt),
	})
	if _, err = prov.Execute(context.Background(), "indexeddb_roundtrip", map[string]any{
		"store": "tasks",
		"id":    "task-2",
		"value": "expired-session",
	}, ""); err != nil {
		t.Fatalf("Execute indexeddb_roundtrip after runtime expiry error = %v, want success (host-service access is authorized by the signed relay token, not the runtime session)", err)
	}
}

func TestRuntimeConfigUsesPublicCacheRelayWithoutHostServiceTunnelCapability(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	runtimeProvider := newCapturingBundleRuntime()
	runtimeProvider.support.EgressMode = proto.RuntimeEgressMode_RUNTIME_EGRESS_MODE_HOSTNAME
	runtimeProvider.fakeHosted = true
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (runtimeprovider.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultProvider: "hosted"},
			Egress:  config.EgressConfig{DefaultAction: string(egress.PolicyDeny)},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Apps: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Runtime:              &config.RuntimePlacementConfig{},
				Cache:                []string{"session", "rate_limit"},
			},
		},
	}

	deps := Deps{
		BaseURL:       "https://gestalt.example.test",
		EncryptionKey: []byte("0123456789abcdef0123456789abcdef"),
		Egress:        newEgressDeps(cfg),
		CacheDefs: map[string]*config.ProviderEntry{
			"session":    {Config: mustNode(t, map[string]any{"namespace": "session"})},
			"rate_limit": {Config: mustNode(t, map[string]any{"namespace": "rate_limit"})},
		},
		CacheFactory: func(yaml.Node) (corecache.Cache, error) {
			return coretesting.NewStubCache(), nil
		},
	}
	deps.RuntimeRegistry = newRuntimeRegistry(cfg, factories.Runtime, deps)

	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })

	prov, err := providers.Get("echoext")
	if err != nil {
		t.Fatalf("providers.Get: %v", err)
	}
	checkEnv := func(envName string) (string, bool) {
		t.Helper()
		result, err := prov.Execute(context.Background(), "read_env", map[string]any{"name": envName}, "")
		if err != nil {
			t.Fatalf("Execute read_env(%s): %v", envName, err)
		}
		var env struct {
			Value string `json:"value"`
			Found bool   `json:"found"`
		}
		if err := json.Unmarshal(result.Body, &env); err != nil {
			t.Fatalf("unmarshal env result for %s: %v", envName, err)
		}
		return env.Value, env.Found
	}

	if got, found := checkEnv(runtimehost.HostServiceSocketEnv); !found || got != "tls://gestalt.example.test:443" {
		t.Fatalf("plugin host-service env %s = (%q, %v), want (%q, true)", runtimehost.HostServiceSocketEnv, got, found, "tls://gestalt.example.test:443")
	}
	if got, found := checkEnv(runtimehost.HostServiceTokenEnv); !found || got == "" {
		t.Fatalf("plugin host-service token env %s = (%q, %v), want non-empty token", runtimehost.HostServiceTokenEnv, got, found)
	}

	startRequests := runtimeProvider.startAppRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("StartApp requests = %d, want 1", len(startRequests))
	}
	assertStartAppRelayEnv(t, startRequests[0], runtimehost.HostServiceSocketEnv)
	if allowedHosts := slices.Clone(startRequests[0].GetAllowedHosts()); !slices.Contains(allowedHosts, "gestalt.example.test") {
		t.Fatalf("StartApp allowed hosts = %#v, want relay host gestalt.example.test", allowedHosts)
	}
}

func TestRuntimePublicCacheRelayRoundTripsThroughHostedApp(t *testing.T) {
	t.Parallel()

	secret := []byte("0123456789abcdef0123456789abcdef")
	publicHostServices := runtimehost.NewPublicHostServiceRegistry()
	relaySrv := httptest.NewUnstartedServer(newRuntimeRelayTestHandler(t, secret, publicHostServices))
	relaySrv.EnableHTTP2 = true
	relaySrv.StartTLS()
	testutil.CloseOnCleanup(t, relaySrv)

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{
				ID:     "cache_roundtrip",
				Method: http.MethodPost,
				Parameters: []catalog.CatalogParameter{
					{Name: "key", Type: "string", Required: true},
					{Name: "value", Type: "string", Required: true},
				},
			},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	runtimeProvider := newCapturingBundleRuntime()
	runtimeProvider.support.EgressMode = proto.RuntimeEgressMode_RUNTIME_EGRESS_MODE_HOSTNAME
	runtimeProvider.fakeHosted = true

	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (runtimeprovider.Provider, error) {
		return runtimeProvider, nil
	}

	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {Driver: config.RuntimeProviderDriver("capture")},
			},
		},
		Apps: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Runtime:              &config.RuntimePlacementConfig{},
				Cache:                []string{"session"},
			},
		},
	}

	boundCache := coretesting.NewStubCache()
	deps := Deps{
		BaseURL:       relaySrv.URL,
		EncryptionKey: secret,
		Caches: map[string]corecache.Cache{
			"session":    boundCache,
			"rate_limit": coretesting.NewStubCache(),
		},
		CacheDefs: map[string]*config.ProviderEntry{
			"session":    {Config: mustNode(t, map[string]any{"namespace": "session"})},
			"rate_limit": {Config: mustNode(t, map[string]any{"namespace": "rate_limit"})},
		},
		CacheFactory: func(yaml.Node) (corecache.Cache, error) {
			return boundCache, nil
		},
		PublicHostServices: publicHostServices,
	}
	deps.RuntimeRegistry = newRuntimeRegistry(cfg, factories.Runtime, deps)

	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })

	prov, err := providers.Get("echoext")
	if err != nil {
		t.Fatalf("providers.Get: %v", err)
	}

	result, err := prov.Execute(context.Background(), "cache_roundtrip", map[string]any{
		"key":   "task-1",
		"value": "ship-it",
	}, "")
	if err != nil {
		t.Fatalf("Execute cache_roundtrip: %v", err)
	}

	var record map[string]any
	if err := json.Unmarshal(result.Body, &record); err != nil {
		t.Fatalf("unmarshal cache_roundtrip: %v", err)
	}
	if got := record["found"]; got != true {
		t.Fatalf("cache_roundtrip found = %#v, want true", got)
	}
	if got := record["value"]; got != "ship-it" {
		t.Fatalf("cache_roundtrip value = %#v, want %q", got, "ship-it")
	}

	gotValue, found, err := boundCache.Get(context.Background(), "echoext:task-1")
	if err != nil {
		t.Fatalf("bound cache Get: %v", err)
	}
	if !found {
		t.Fatal("bound cache missing echoed value")
	}
	if got := string(gotValue); got != "ship-it" {
		t.Fatalf("bound cache stored value = %q, want %q", got, "ship-it")
	}

	startRequests := runtimeProvider.startAppRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("StartApp requests = %d, want 1", len(startRequests))
	}
	assertStartAppRelayEnv(t, startRequests[0], "cache relay round-trip")
}

func TestRuntimePublicS3RelayRoundTripsThroughHostedApp(t *testing.T) {
	t.Parallel()

	secret := []byte("0123456789abcdef0123456789abcdef")
	publicHostServices := runtimehost.NewPublicHostServiceRegistry()
	relaySrv := httptest.NewUnstartedServer(newRuntimeRelayTestHandler(t, secret, publicHostServices))
	relaySrv.EnableHTTP2 = true
	relaySrv.StartTLS()
	testutil.CloseOnCleanup(t, relaySrv)

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{
				ID:     "s3_roundtrip",
				Method: http.MethodPost,
				Parameters: []catalog.CatalogParameter{
					{Name: "key", Type: "string", Required: true},
					{Name: "value", Type: "string", Required: true},
				},
			},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	runtimeProvider := newCapturingBundleRuntime()
	runtimeProvider.support.EgressMode = proto.RuntimeEgressMode_RUNTIME_EGRESS_MODE_HOSTNAME
	runtimeProvider.fakeHosted = true

	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (runtimeprovider.Provider, error) {
		return runtimeProvider, nil
	}

	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {Driver: config.RuntimeProviderDriver("capture")},
			},
		},
		Apps: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Runtime:              &config.RuntimePlacementConfig{},
				S3:                   []string{"main"},
			},
		},
	}

	boundS3 := &coretesting.StubS3{}
	deps := Deps{
		BaseURL:       relaySrv.URL,
		EncryptionKey: secret,
		S3: map[string]s3sdk.S3{
			"main":    boundS3,
			"archive": &coretesting.StubS3{},
		},
		PublicHostServices: publicHostServices,
	}
	deps.RuntimeRegistry = newRuntimeRegistry(cfg, factories.Runtime, deps)

	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })

	prov, err := providers.Get("echoext")
	if err != nil {
		t.Fatalf("providers.Get: %v", err)
	}

	result, err := prov.Execute(context.Background(), "s3_roundtrip", map[string]any{
		"key":   "plans/q3.txt",
		"value": "ship-it",
	}, "")
	if err != nil {
		t.Fatalf("Execute s3_roundtrip: %v", err)
	}

	var body struct {
		Body  string   `json:"body"`
		Key   string   `json:"key"`
		Keys  []string `json:"keys"`
		Type  string   `json:"type"`
		Size  int64    `json:"size"`
		Found bool     `json:"found"`
	}
	if err := json.Unmarshal(result.Body, &body); err != nil {
		t.Fatalf("unmarshal s3_roundtrip: %v", err)
	}
	if body.Body != "ship-it" {
		t.Fatalf("body = %q, want %q", body.Body, "ship-it")
	}
	if body.Key != "plans/q3.txt" {
		t.Fatalf("key = %q, want %q", body.Key, "plans/q3.txt")
	}
	if !slices.Equal(body.Keys, []string{"plans/q3.txt"}) {
		t.Fatalf("keys = %#v, want %#v", body.Keys, []string{"plans/q3.txt"})
	}
	if body.Type != "text/plain" {
		t.Fatalf("content type = %q, want %q", body.Type, "text/plain")
	}
	if body.Size != int64(len("ship-it")) {
		t.Fatalf("size = %d, want %d", body.Size, len("ship-it"))
	}
	if !body.Found {
		t.Fatal("expected s3 list operation to find the written object")
	}

	if _, err := boundS3.HeadObject(context.Background(), s3sdk.ObjectRef{
		Key: "plans/q3.txt",
	}); err != nil {
		t.Fatalf("expected backing key: %v", err)
	}

	startRequests := runtimeProvider.startAppRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("StartApp requests = %d, want 1", len(startRequests))
	}
	assertStartAppRelayEnv(t, startRequests[0], "s3 relay round-trip")
	assertStartAppRelayEnv(t, startRequests[0], "s3 multi-binding relay")
}

func TestRuntimePublicAppInvocationRelayRoundTripsThroughHostedApp(t *testing.T) {
	t.Parallel()

	secret := []byte("0123456789abcdef0123456789abcdef")
	publicHostServices := runtimehost.NewPublicHostServiceRegistry()

	callerBin := buildEchoPluginBinary(t)
	callerRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "caller",
		Operations: []catalog.CatalogOperation{
			{ID: "invoke_plugin", Method: http.MethodPost},
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	exampleBin := buildExampleProviderBinary(t)
	exampleRoot := exampleProviderRoot(t)
	callerManifest := newExecutableManifest("Caller", "Invokes another plugin")
	callerManifest.Spec.Connections = map[string]*providermanifestv1.ManifestConnectionDef{
		"default": {
			Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeBearer},
		},
	}
	exampleManifest := newExecutableManifest("Example Provider", "Reports request context")
	exampleManifest.Spec.Connections = map[string]*providermanifestv1.ManifestConnectionDef{
		"default": {
			Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeBearer},
		},
	}

	runtimeProvider := newCapturingBundleRuntime()
	runtimeProvider.support.EgressMode = proto.RuntimeEgressMode_RUNTIME_EGRESS_MODE_HOSTNAME
	runtimeProvider.fakeHosted = true

	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (runtimeprovider.Provider, error) {
		return runtimeProvider, nil
	}

	bridge := newLazyInvoker()
	relaySrv := httptest.NewUnstartedServer(newRuntimeRelayTestHandler(t, secret, publicHostServices))
	relaySrv.EnableHTTP2 = true
	relaySrv.StartTLS()
	testutil.CloseOnCleanup(t, relaySrv)

	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {Driver: config.RuntimeProviderDriver("capture")},
			},
		},
		Apps: map[string]*config.ProviderEntry{
			"caller": {
				Command:              callerBin,
				Args:                 []string{"provider"},
				ResolvedManifest:     callerManifest,
				ResolvedManifestPath: filepath.Join(callerRoot, "manifest.yaml"),
				Runtime:              &config.RuntimePlacementConfig{},
			},
			"example": {
				Command:              exampleBin,
				ResolvedManifest:     exampleManifest,
				ResolvedManifestPath: filepath.Join(exampleRoot, "manifest.yaml"),
				Config: mustNode(t, map[string]any{
					"greeting": "Hello from relay invoke",
				}),
			},
		},
	}

	deps := Deps{
		BaseURL:            relaySrv.URL,
		EncryptionKey:      secret,
		AppInvocation:      bridge,
		PublicHostServices: publicHostServices,
		Authorization:      newAllowAllAuthorizationProvider(),
	}
	deps.RuntimeRegistry = newRuntimeRegistry(cfg, factories.Runtime, deps)

	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })
	registerGlobalAppInvocationForTest(t, deps)
	assertPublicHostServicesVerified(t, publicHostServices, "app")

	services, err := coredata.New(&coretesting.StubIndexedDB{})
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	testutil.AttachStubExternalCredentials(services)
	t.Cleanup(func() { _ = services.Close() })

	broker := invocation.NewBroker(providers, services.Users, services.ExternalCredentials)
	guarded := invocation.NewGuarded(broker, nil, "app", nil, invocation.WithoutRateLimit())
	bridge.SetTarget(guarded)
	harness := &nestedInvokeHarness{
		invoker:  invocation.NewGuarded(broker, nil, "test", nil, invocation.WithoutRateLimit()),
		services: services,
	}

	ctx := context.Background()
	user := newNestedInvokeUser(t, harness, ctx, "nested-runtime-relay@test.com")
	storeNestedInvokeToken(t, harness, ctx, user.ID, "caller", "work", "default")
	storeNestedInvokeToken(t, harness, ctx, user.ID, "example", "work", "default")

	result, err := harness.invoker.Invoke(
		invocation.WithConnection(context.Background(), "work"),
		&principal.Principal{
			UserID:      user.ID,
			Kind:        principal.KindUser,
			Source:      principal.SourceBearer,
			DisplayName: "Runtime Relay",
			Scopes:      []string{"caller", "example"},
		},
		"caller",
		"default",
		"invoke_plugin",
		map[string]any{
			"app":       "example",
			"operation": "request_context",
		},
	)
	if err != nil {
		t.Fatalf("Invoke(caller.invoke_plugin): %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", result.Status, http.StatusOK)
	}

	var got invokePluginEnvelope
	if err := json.Unmarshal(result.Body, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if !got.OK {
		t.Fatalf("invoke_plugin returned error envelope: %+v", got)
	}
	if got.TargetApp != "example" || got.TargetOperation != "request_context" {
		t.Fatalf("unexpected target: %+v", got)
	}
	if got.Status != http.StatusOK {
		t.Fatalf("nested status = %d, want %d", got.Status, http.StatusOK)
	}
	if got.Body.Credential.Connection != "work" {
		t.Fatalf("nested credential.connection = %q, want %q", got.Body.Credential.Connection, "work")
	}
	if got.Body.Credential.Instance != "default" {
		t.Fatalf("nested credential.instance = %q, want %q", got.Body.Credential.Instance, "default")
	}

	startRequests := runtimeProvider.startAppRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("StartApp requests = %d, want 1", len(startRequests))
	}
	assertStartAppRelayEnv(t, startRequests[0], "app")
	if allowedHosts := slices.Clone(startRequests[0].GetAllowedHosts()); len(allowedHosts) != 0 {
		t.Fatalf("StartApp allowed hosts = %#v, want none when hostname egress enforcement is not required", allowedHosts)
	}
}

func TestRuntimeConfigUsesPublicWorkflowManagerRelayWithoutHostServiceTunnelCapability(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	runtimeProvider := newCapturingBundleRuntime()
	runtimeProvider.support.EgressMode = proto.RuntimeEgressMode_RUNTIME_EGRESS_MODE_HOSTNAME
	runtimeProvider.fakeHosted = true
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (runtimeprovider.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultProvider: "hosted"},
			Egress:  config.EgressConfig{DefaultAction: string(egress.PolicyDeny)},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Apps: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Runtime:              &config.RuntimePlacementConfig{},
			},
		},
	}

	deps := Deps{
		BaseURL:         "https://gestalt.example.test",
		EncryptionKey:   []byte("0123456789abcdef0123456789abcdef"),
		Egress:          newEgressDeps(cfg),
		WorkflowManager: newStubWorkflowManager(),
	}
	deps.RuntimeRegistry = newRuntimeRegistry(cfg, factories.Runtime, deps)

	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })

	prov, err := providers.Get("echoext")
	if err != nil {
		t.Fatalf("providers.Get: %v", err)
	}
	checkEnv := func(envName string) (string, bool) {
		t.Helper()
		result, err := prov.Execute(context.Background(), "read_env", map[string]any{"name": envName}, "")
		if err != nil {
			t.Fatalf("Execute read_env(%s): %v", envName, err)
		}
		var env struct {
			Value string `json:"value"`
			Found bool   `json:"found"`
		}
		if err := json.Unmarshal(result.Body, &env); err != nil {
			t.Fatalf("unmarshal env result for %s: %v", envName, err)
		}
		return env.Value, env.Found
	}

	if got, found := checkEnv(runtimehost.HostServiceSocketEnv); !found || got != "tls://gestalt.example.test:443" {
		t.Fatalf("plugin host-service env %s = (%q, %v), want (%q, true)", runtimehost.HostServiceSocketEnv, got, found, "tls://gestalt.example.test:443")
	}
	if got, found := checkEnv(runtimehost.HostServiceTokenEnv); !found || got == "" {
		t.Fatalf("plugin host-service token env %s = (%q, %v), want non-empty token", runtimehost.HostServiceTokenEnv, got, found)
	}

	startRequests := runtimeProvider.startAppRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("StartApp requests = %d, want 1", len(startRequests))
	}
	assertStartAppRelayEnv(t, startRequests[0], "workflow provider relay")
	if allowedHosts := slices.Clone(startRequests[0].GetAllowedHosts()); !slices.Contains(allowedHosts, "gestalt.example.test") {
		t.Fatalf("StartApp allowed hosts = %#v, want relay host gestalt.example.test", allowedHosts)
	}
}

func TestRuntimeConfigRejectsMissingHostnameEgressCapability(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	runtimeProvider := &staticCapabilityRuntime{
		inner: runtimeprovider.NewLocalProvider(),
		support: &proto.RuntimeSupport{
			CanHostApps: true,
		},
	}
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (runtimeprovider.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Apps: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Runtime:              &config.RuntimePlacementConfig{},
				Egress:               &config.ProviderEgressConfig{AllowedHosts: []string{"api.github.com"}},
			},
		},
	}

	deps := testRuntimePublicEndpointDeps(t, Deps{})
	deps.RuntimeRegistry = newRuntimeRegistry(cfg, factories.Runtime, deps)
	_, _, err := buildProvidersStrict(context.Background(), cfg, factories, deps)
	if err == nil || !strings.Contains(err.Error(), "cannot preserve hostname-based egress required by this provider") {
		t.Fatalf("buildProvidersStrict error = %v, want hostname-based egress requirement failure", err)
	}
}

func TestRuntimeConfigRejectsMissingHostServiceRelay(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	runtimeProvider := &staticCapabilityRuntime{
		inner: runtimeprovider.NewLocalProvider(),
		support: &proto.RuntimeSupport{
			CanHostApps: true,
		},
	}
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (runtimeprovider.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Apps: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Runtime:              &config.RuntimePlacementConfig{},
				Cache:                []string{"session"},
			},
		},
	}

	deps := Deps{
		CacheDefs: map[string]*config.ProviderEntry{
			"session": {Config: mustNode(t, map[string]any{"namespace": "session"})},
		},
		CacheFactory: func(yaml.Node) (corecache.Cache, error) {
			return coretesting.NewStubCache(), nil
		},
	}
	deps.RuntimeRegistry = newRuntimeRegistry(cfg, factories.Runtime, deps)

	_, _, err := buildProvidersStrict(context.Background(), cfg, factories, deps)
	if err == nil || !strings.Contains(err.Error(), "cannot provide host service access required by this provider") {
		t.Fatalf("buildProvidersStrict error = %v, want host service access failure", err)
	}
}

func TestRuntimeConfigInjectsRuntimeLogSessionAndHostService(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	runtimeProvider := newCapturingBundleRuntime()
	runtimeProvider.fakeHosted = true
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (runtimeprovider.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {Driver: config.RuntimeProviderDriver("capture")},
			},
		},
		Apps: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Runtime:              &config.RuntimePlacementConfig{},
			},
		},
	}
	services, err := coredata.New(&coretesting.StubIndexedDB{})
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	t.Cleanup(func() { _ = services.Close() })
	deps := Deps{
		BaseURL:       "https://gestalt.example.test",
		EncryptionKey: []byte("0123456789abcdef0123456789abcdef"),
		Services:      services,
	}
	deps.RuntimeRegistry = newRuntimeRegistry(cfg, factories.Runtime, deps)

	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })

	startRequests := runtimeProvider.startAppRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("StartApp requests = %d, want 1", len(startRequests))
	}
	if got := startRequests[0].GetEnv()[runtimehost.HostServiceSocketEnv]; got != "tls://gestalt.example.test:443" {
		t.Fatalf("host service socket = %q, want public relay target", got)
	}
	if got := startRequests[0].GetEnv()[runtimehost.HostServiceTokenEnv]; got == "" {
		t.Fatalf("StartApp env missing %s", runtimehost.HostServiceTokenEnv)
	}
}

// registerGlobalAppInvocationForTest mirrors gestaltd bootstrap registering
// the app-invocation host service once, globally, against the shared invoker.
func registerGlobalAppInvocationForTest(t *testing.T, deps Deps) {
	t.Helper()
	t.Cleanup(registerGlobalAppInvocationPublicHostService(deps))
}

func assertPublicHostServicesVerified(t *testing.T, registry *runtimehost.PublicHostServiceRegistry, serviceName string) {
	t.Helper()

	if registry == nil {
		t.Fatalf("public host services registry is nil, want %s verifier entry", serviceName)
	}
	found := false
	for _, service := range registry.Snapshot() {
		if strings.TrimSpace(service.Service.Name) != strings.TrimSpace(serviceName) {
			continue
		}
		found = true
		if service.SessionVerifier == nil {
			t.Fatalf("public host services = %#v, want %s verifier entry", registry.Snapshot(), serviceName)
		}
	}
	if !found {
		t.Fatalf("public host services = %#v, want %s verifier entry", registry.Snapshot(), serviceName)
	}
}

func newRuntimeRelayTestHandler(t *testing.T, stateSecret []byte, publicHostServices *runtimehost.PublicHostServiceRegistry) http.Handler {
	t.Helper()

	tokenManager, err := runtimehost.NewHostServiceRelayTokenManager(stateSecret)
	if err != nil {
		t.Fatalf("NewHostServiceRelayTokenManager: %v", err)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimSpace(r.Header.Get(runtimehost.HostServiceRelayTokenHeader))
		target, err := tokenManager.ResolveToken(token)
		if err != nil {
			writeRuntimeRelayGRPCTrailersOnly(w, codes.Unauthenticated, "invalid-host-service-relay-token")
			return
		}
		if !runtimeRelayMethodAllowed(r.URL.Path, target.MethodPrefix) {
			writeRuntimeRelayGRPCTrailersOnly(w, codes.PermissionDenied, "host-service-relay-method-not-allowed")
			return
		}
		if runtimehost.CallerCapabilityRequiredMethod(r.URL.Path) && target.Caller == nil && strings.TrimSpace(target.MethodPrefix) != "/" {
			writeRuntimeRelayGRPCTrailersOnly(w, codes.Unauthenticated, "invalid-host-service-relay-token")
			return
		}
		handler, err := runtimeRelayPublicHostServiceHandler(r.Context(), publicHostServices, target, r.URL.Path)
		if err != nil {
			writeRuntimeRelayGRPCTrailersOnly(w, codes.Unauthenticated, "invalid-host-service-relay-session")
			return
		}
		if handler == nil {
			writeRuntimeRelayGRPCTrailersOnly(w, codes.Unavailable, "host-service-relay-unavailable")
			return
		}
		relayReq := r.Clone(hostserviceingress.ApplyCapability(runtimehost.WithRelayAuthenticated(r.Context()), target))
		relayReq.Header = r.Header.Clone()
		relayReq.Header.Del(runtimehost.HostServiceRelayTokenHeader)
		handler.ServeHTTP(w, relayReq)
	})
}

func newRuntimePublicEndpointTestServer(t *testing.T, stateSecret []byte, publicHostServices *runtimehost.PublicHostServiceRegistry) *httptest.Server {
	t.Helper()

	relay := newRuntimeRelayTestHandler(t, stateSecret, publicHostServices)
	proxy := newRuntimeEgressProxyTestHandler(t, stateSecret)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(r.Header.Get(runtimehost.HostServiceRelayTokenHeader)) != "" {
			relay.ServeHTTP(w, r)
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	testutil.CloseOnCleanup(t, srv)

	cert := srv.Certificate()
	if cert == nil {
		t.Fatal("runtime public endpoint certificate is nil")
	}
	return srv
}

func testRuntimePublicEndpointDeps(t *testing.T, deps Deps) Deps {
	t.Helper()

	if len(deps.EncryptionKey) == 0 {
		deps.EncryptionKey = []byte("0123456789abcdef0123456789abcdef")
	}
	if deps.PublicHostServices == nil {
		deps.PublicHostServices = runtimehost.NewPublicHostServiceRegistry()
	}
	if len(deps.IndexedDBs) == 0 && deps.IndexedDBFactory != nil && len(deps.IndexedDBDefs) > 0 {
		deps.IndexedDBs = make(map[string]indexeddb.IndexedDB, len(deps.IndexedDBDefs))
		for _, name := range slices.Sorted(maps.Keys(deps.IndexedDBDefs)) {
			entry := deps.IndexedDBDefs[name]
			if entry == nil {
				continue
			}
			db, err := buildIndexedDB(nil, name, entry, &FactoryRegistry{IndexedDB: deps.IndexedDBFactory}, Deps{})
			if err != nil {
				t.Fatalf("build test indexeddb %q: %v", name, err)
			}
			deps.IndexedDBs[name] = db
			t.Cleanup(func() { _ = db.Close() })
			if strings.TrimSpace(deps.SelectedIndexedDBName) == "" {
				deps.SelectedIndexedDBName = name
			}
		}
	}
	if len(deps.Caches) == 0 && deps.CacheFactory != nil && len(deps.CacheDefs) > 0 {
		deps.Caches = make(map[string]corecache.Cache, len(deps.CacheDefs))
		for _, name := range slices.Sorted(maps.Keys(deps.CacheDefs)) {
			entry := deps.CacheDefs[name]
			if entry == nil {
				continue
			}
			cache, err := buildCache(entry, &FactoryRegistry{Cache: deps.CacheFactory})
			if err != nil {
				t.Fatalf("build test cache %q: %v", name, err)
			}
			deps.Caches[name] = cache
			t.Cleanup(func() { _ = cache.Close() })
		}
	}
	if strings.TrimSpace(deps.BaseURL) == "" {
		srv := newRuntimePublicEndpointTestServer(t, deps.EncryptionKey, deps.PublicHostServices)
		deps.BaseURL = srv.URL
		if cert := srv.Certificate(); cert != nil && strings.TrimSpace(deps.HostServiceTLSCAFile) == "" && strings.TrimSpace(deps.HostServiceTLSCAPEM) == "" {
			deps.HostServiceTLSCAPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}))
		}
	}
	return deps
}

func runtimeRelayPublicHostServiceHandler(ctx context.Context, registry *runtimehost.PublicHostServiceRegistry, target runtimehost.HostServiceRelayTarget, methodPath string) (http.Handler, error) {
	if registry == nil {
		return nil, nil
	}
	pluginName := strings.TrimSpace(target.AppName)
	if strings.HasPrefix(methodPath, "/"+proto.App_ServiceDesc.ServiceName+"/") {
		pluginName = appInvocationPublicProviderKey
	}
	for _, entry := range registry.Snapshot() {
		if strings.TrimSpace(entry.AppName) != pluginName {
			continue
		}
		if !entry.Service.AllowsMethod(methodPath) {
			continue
		}
		if entry.Service.Register == nil {
			continue
		}
		if entry.SessionVerifier == nil {
			return nil, fmt.Errorf("public host service %s/%s requires a session verifier", strings.TrimSpace(target.AppName), strings.TrimSpace(target.Service))
		}
		if err := entry.SessionVerifier.VerifyHostServiceSession(ctx, target.SessionID); err != nil {
			return nil, err
		}
		srv := grpc.NewServer()
		entry.Service.Register(srv)
		return http.HandlerFunc(srv.ServeHTTP), nil
	}
	return nil, nil
}

func newRuntimeEgressProxyTestHandler(t *testing.T, stateSecret []byte) http.Handler {
	t.Helper()

	tokenManager, err := egressproxy.NewTokenManager(stateSecret)
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractRuntimeProxyAuthorizationToken(r.Header.Get("Proxy-Authorization"))
		target, err := tokenManager.ResolveToken(token)
		if err != nil {
			http.Error(w, "invalid egress proxy token", http.StatusProxyAuthRequired)
			return
		}
		host := runtimeProxyTargetHost(r)
		if host == "" {
			http.Error(w, "proxy target host is required", http.StatusBadRequest)
			return
		}
		if err := egress.CheckHost(target.AllowedHosts, host, target.DefaultAction); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		newRuntimeEgressProxy().ServeHTTP(w, r)
	})
}

func extractRuntimeProxyAuthorizationToken(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}
	if token, ok := strings.CutPrefix(header, "Basic "); ok {
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(token))
		if err != nil {
			return ""
		}
		user, pass, found := strings.Cut(string(decoded), ":")
		if found && strings.TrimSpace(pass) != "" {
			return strings.TrimSpace(pass)
		}
		return strings.TrimSpace(user)
	}
	return ""
}

func runtimeProxyTargetHost(r *http.Request) string {
	if r == nil {
		return ""
	}
	var host string
	switch {
	case r.Method == http.MethodConnect:
		host = strings.TrimSpace(r.Host)
	case r.URL != nil && r.URL.Host != "":
		host = strings.TrimSpace(r.URL.Hostname())
	default:
		host = strings.TrimSpace(r.Host)
	}
	if host == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

func newRuntimeEgressProxy() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			runtimeHandleProxyConnect(w, r)
			return
		}
		runtimeHandleProxyHTTP(w, r)
	})
}

func runtimeHandleProxyHTTP(w http.ResponseWriter, r *http.Request) {
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	out := r.Clone(r.Context())
	out.RequestURI = ""
	out.Header = out.Header.Clone()
	out.Header.Del("Proxy-Authorization")
	if out.URL == nil || !out.URL.IsAbs() {
		http.Error(w, "proxy target URL is required", http.StatusBadRequest)
		return
	}
	out.Host = out.URL.Host

	resp, err := transport.RoundTrip(out)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func runtimeHandleProxyConnect(w http.ResponseWriter, r *http.Request) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}

	targetAddr := strings.TrimSpace(r.Host)
	if targetAddr == "" {
		http.Error(w, "proxy target address is required", http.StatusBadRequest)
		return
	}
	if _, _, err := net.SplitHostPort(targetAddr); err != nil {
		targetAddr = net.JoinHostPort(targetAddr, "443")
	}
	targetConn, err := net.DialTimeout("tcp", targetAddr, 10*time.Second)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	w.WriteHeader(http.StatusOK)
	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		_ = targetConn.Close()
		return
	}
	deadline := time.Now().Add(10 * time.Minute)
	_ = clientConn.SetDeadline(deadline)
	_ = targetConn.SetDeadline(deadline)

	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(targetConn, clientConn)
		closeRuntimeProxyWrite(targetConn)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(clientConn, targetConn)
		closeRuntimeProxyWrite(clientConn)
		done <- struct{}{}
	}()
	<-done
	<-done
	_ = clientConn.Close()
	_ = targetConn.Close()
}

func closeRuntimeProxyWrite(c net.Conn) {
	if closeWriter, ok := c.(interface{ CloseWrite() error }); ok {
		_ = closeWriter.CloseWrite()
	}
}

func runtimeRelayMethodAllowed(path, methodPrefix string) bool {
	methodPrefix = strings.TrimSpace(methodPrefix)
	if methodPrefix == "" {
		return true
	}
	if path == methodPrefix {
		return true
	}
	if strings.HasSuffix(methodPrefix, "/") {
		return strings.HasPrefix(path, methodPrefix)
	}
	return strings.HasPrefix(path, methodPrefix+"/")
}

func writeRuntimeRelayGRPCTrailersOnly(w http.ResponseWriter, code codes.Code, message string) {
	headers := w.Header()
	headers.Set("Content-Type", "application/grpc")
	headers.Set("Trailer", "Grpc-Status, Grpc-Message")
	headers.Set("Grpc-Status", strconv.Itoa(int(code)))
	if message != "" {
		headers.Set("Grpc-Message", message)
	}
	w.WriteHeader(http.StatusOK)
}

func TestRuntimePublicWorkflowManagerRelayRoundTripsThroughHostedApp(t *testing.T) {
	t.Parallel()

	secret := []byte("0123456789abcdef0123456789abcdef")
	publicHostServices := runtimehost.NewPublicHostServiceRegistry()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "workflow_manager_roundtrip", Method: http.MethodPost},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	runtimeProvider := newCapturingBundleRuntime()
	runtimeProvider.support.EgressMode = proto.RuntimeEgressMode_RUNTIME_EGRESS_MODE_HOSTNAME
	runtimeProvider.fakeHosted = true
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (runtimeprovider.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Apps: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Runtime:              &config.RuntimePlacementConfig{},
			},
		},
	}

	manager := newStubWorkflowManager()
	relaySrv := httptest.NewUnstartedServer(newRuntimeRelayTestHandler(t, secret, publicHostServices))
	relaySrv.EnableHTTP2 = true
	relaySrv.StartTLS()
	testutil.CloseOnCleanup(t, relaySrv)

	deps := Deps{
		BaseURL:            relaySrv.URL,
		EncryptionKey:      secret,
		WorkflowManager:    manager,
		PublicHostServices: publicHostServices,
		Authorization:      newAllowAllAuthorizationProvider(),
	}
	deps.RuntimeRegistry = newRuntimeRegistry(cfg, factories.Runtime, deps)

	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })
	assertPublicHostServicesVerified(t, publicHostServices, "workflow")

	prov, err := providers.Get("echoext")
	if err != nil {
		t.Fatalf("providers.Get: %v", err)
	}

	ctx := principal.WithPrincipal(context.Background(), &principal.Principal{
		SubjectID: "user:user-123",
		UserID:    "user-123",
		Kind:      principal.KindUser,
		Source:    principal.SourceBearer,
		Scopes:    []string{"echoext"},
	})

	result, err := prov.Execute(ctx, "workflow_manager_roundtrip", nil, "")
	if err != nil {
		t.Fatalf("Execute workflow_manager_roundtrip: %v", err)
	}

	var body struct {
		Provider     string `json:"provider"`
		DefinitionID string `json:"definition_id"`
		ActivationID string `json:"activation_id"`
		Operation    string `json:"operation"`
	}
	if err := json.Unmarshal(result.Body, &body); err != nil {
		t.Fatalf("unmarshal workflow_manager_roundtrip: %v", err)
	}
	if body.Provider != "basic" {
		t.Fatalf("provider = %q, want %q", body.Provider, "basic")
	}
	if body.DefinitionID == "" {
		t.Fatal("workflow_manager_roundtrip should return a definition id")
	}
	if body.ActivationID != "nightly" {
		t.Fatalf("activation_id = %q, want %q", body.ActivationID, "nightly")
	}
	if body.Operation != "sync" {
		t.Fatalf("operation = %q, want %q", body.Operation, "sync")
	}

	definitions, err := manager.ListDefinitions(context.Background(), nil, "basic")
	if err != nil {
		t.Fatalf("manager.ListDefinitions: %v", err)
	}
	if len(definitions.Definitions) != 1 {
		t.Fatalf("manager definitions len = %d, want 1", len(definitions.Definitions))
	}
	if len(definitions.Definitions[0].Definition.Target.Steps) == 0 || definitions.Definitions[0].Definition.Target.Steps[0].App == nil {
		t.Fatalf("stored target app step is missing: %#v", definitions.Definitions[0].Definition.Target)
		return
	}
	definitionTarget := definitions.Definitions[0].Definition.Target.Steps[0].App
	if got := definitionTarget.Operation; got != "sync" {
		t.Fatalf("stored target operation = %q, want %q", got, "sync")
	}
	if got := manager.Subjects(); !slices.Equal(got, []string{"user:user-123", "user:user-123"}) {
		t.Fatalf("manager subjects = %v, want two user:user-123 entries", got)
	}
	if got := manager.DefinitionIdempotencyKeys(); !slices.Equal(got, []string{"workflow-manager-roundtrip"}) {
		t.Fatalf("manager definition idempotency keys = %v, want [workflow-manager-roundtrip]", got)
	}

	startRequests := runtimeProvider.startAppRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("StartApp requests = %d, want 1", len(startRequests))
	}
	assertStartAppRelayEnv(t, startRequests[0], "workflow provider relay")
}

func TestRuntimeConfigInjectsPublicEgressProxyWithoutHostServiceTunnelCapability(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	runtimeProvider := newCapturingBundleRuntime()
	runtimeProvider.support.EgressMode = proto.RuntimeEgressMode_RUNTIME_EGRESS_MODE_HOSTNAME
	runtimeProvider.fakeHosted = true
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (runtimeprovider.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultProvider: "hosted"},
			Egress:  config.EgressConfig{DefaultAction: string(egress.PolicyDeny)},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Apps: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Runtime:              &config.RuntimePlacementConfig{},
				Egress:               &config.ProviderEgressConfig{AllowedHosts: []string{"api.github.com"}},
			},
		},
	}
	deps := Deps{
		BaseURL:             "https://gestalt.example.test",
		RuntimeRelayBaseURL: "http://gestaltd.gestalt-runtime.svc.cluster.local:8080",
		EncryptionKey:       []byte("0123456789abcdef0123456789abcdef"),
		Egress:              EgressDeps{DefaultAction: egress.PolicyDeny},
	}
	deps.RuntimeRegistry = newRuntimeRegistry(cfg, factories.Runtime, deps)

	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })

	startRequests := runtimeProvider.startAppRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("StartApp requests = %d, want 1", len(startRequests))
	}
	httpProxy := startRequests[0].GetEnv()["HTTP_PROXY"]
	httpsProxy := startRequests[0].GetEnv()["HTTPS_PROXY"]
	if httpProxy == "" {
		t.Fatal("StartApp env should include HTTP_PROXY")
	}
	if httpsProxy == "" {
		t.Fatal("StartApp env should include HTTPS_PROXY")
	}
	if httpProxy != httpsProxy {
		t.Fatalf("HTTP_PROXY = %q, HTTPS_PROXY = %q, want matching values", httpProxy, httpsProxy)
	}
	parsed, err := url.Parse(httpProxy)
	if err != nil {
		t.Fatalf("parse HTTP_PROXY: %v", err)
	}
	if parsed.Scheme != "http" {
		t.Fatalf("HTTP_PROXY scheme = %q, want explicit relay base URL scheme http", parsed.Scheme)
	}
	if parsed.Host != "gestaltd.gestalt-runtime.svc.cluster.local:8080" {
		t.Fatalf("HTTP_PROXY host = %q, want explicit relay base URL host", parsed.Host)
	}
	if parsed.User == nil {
		t.Fatal("HTTP_PROXY should include relay credentials")
	}
	assertStartAppEgressPolicy(t, startRequests[0], []string{"api.github.com", "gestaltd.gestalt-runtime.svc.cluster.local"}, egress.PolicyDeny)
}

func TestRuntimeConfigSkipsPublicEgressProxyWhenHostnameEgressIsNotRequired(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	runtimeProvider := newCapturingBundleRuntime()
	runtimeProvider.support.EgressMode = proto.RuntimeEgressMode_RUNTIME_EGRESS_MODE_HOSTNAME
	runtimeProvider.fakeHosted = true
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (runtimeprovider.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultProvider: "hosted"},
			Egress:  config.EgressConfig{DefaultAction: string(egress.PolicyAllow)},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Apps: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Runtime:              &config.RuntimePlacementConfig{},
			},
		},
	}
	deps := Deps{
		BaseURL:       "https://gestalt.example.test",
		EncryptionKey: []byte("0123456789abcdef0123456789abcdef"),
		Egress:        EgressDeps{DefaultAction: egress.PolicyAllow},
	}
	deps.RuntimeRegistry = newRuntimeRegistry(cfg, factories.Runtime, deps)

	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })

	startRequests := runtimeProvider.startAppRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("StartApp requests = %d, want 1", len(startRequests))
	}
	if got := startRequests[0].GetEnv()["HTTP_PROXY"]; got != "" {
		t.Fatalf("StartApp HTTP_PROXY = %q, want empty when hostname egress is not required", got)
	}
	if got := startRequests[0].GetEnv()["HTTPS_PROXY"]; got != "" {
		t.Fatalf("StartApp HTTPS_PROXY = %q, want empty when hostname egress is not required", got)
	}
}

func TestRuntimeConfigUsesPublicRelayAndEgressProxyWhenHostCanRelay(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	runtimeProvider := newCapturingBundleRuntime()
	runtimeProvider.support.EgressMode = proto.RuntimeEgressMode_RUNTIME_EGRESS_MODE_HOSTNAME
	runtimeProvider.fakeHosted = true
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (runtimeprovider.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultProvider: "hosted"},
			Egress:  config.EgressConfig{DefaultAction: string(egress.PolicyDeny)},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Apps: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Runtime:              &config.RuntimePlacementConfig{},
				Egress:               &config.ProviderEgressConfig{AllowedHosts: []string{"api.github.com"}},
				Cache:                []string{"session"},
			},
		},
	}
	deps := Deps{
		BaseURL:       "https://gestalt.example.test",
		EncryptionKey: []byte("0123456789abcdef0123456789abcdef"),
		Egress:        EgressDeps{DefaultAction: egress.PolicyDeny},
		CacheDefs: map[string]*config.ProviderEntry{
			"session": {Config: mustNode(t, map[string]any{"namespace": "session"})},
		},
		CacheFactory: func(yaml.Node) (corecache.Cache, error) {
			return coretesting.NewStubCache(), nil
		},
	}
	deps.RuntimeRegistry = newRuntimeRegistry(cfg, factories.Runtime, deps)

	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })

	startRequests := runtimeProvider.startAppRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("StartApp requests = %d, want 1", len(startRequests))
	}
	if got := startRequests[0].GetEnv()["HTTP_PROXY"]; !strings.Contains(got, "@gestalt.example.test") {
		t.Fatalf("StartApp HTTP_PROXY = %q, want public egress proxy on gestalt.example.test", got)
	}
	if got := startRequests[0].GetEnv()["HTTPS_PROXY"]; !strings.Contains(got, "@gestalt.example.test") {
		t.Fatalf("StartApp HTTPS_PROXY = %q, want public egress proxy on gestalt.example.test", got)
	}
	assertStartAppRelayEnv(t, startRequests[0], "cache session binding relay")
	assertStartAppEgressPolicy(t, startRequests[0], []string{"api.github.com", "gestalt.example.test"}, egress.PolicyDeny)
}

func TestRuntimePublicEgressProxyRoundTripsThroughHostedApp(t *testing.T) {
	t.Parallel()

	secret := []byte("0123456789abcdef0123456789abcdef")
	proxySrv := httptest.NewUnstartedServer(newRuntimeEgressProxyTestHandler(t, secret))
	proxySrv.EnableHTTP2 = true
	proxySrv.StartTLS()
	testutil.CloseOnCleanup(t, proxySrv)

	targetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/proxy-test" {
			t.Fatalf("target path = %q, want /proxy-test", got)
		}
		_, _ = io.WriteString(w, "egress-proxy-ok")
	}))
	testutil.CloseOnCleanup(t, targetSrv)

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "make_http_request", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "url", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Hosted egress proxy roundtrip")
	runtimeProvider := newCapturingBundleRuntime()
	runtimeProvider.support.EgressMode = proto.RuntimeEgressMode_RUNTIME_EGRESS_MODE_HOSTNAME
	runtimeProvider.fakeHosted = true
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (runtimeprovider.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Apps: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Runtime:              &config.RuntimePlacementConfig{},
				Egress:               &config.ProviderEgressConfig{AllowedHosts: []string{"127.0.0.1", "localhost"}},
			},
		},
	}
	deps := Deps{
		BaseURL:       proxySrv.URL,
		EncryptionKey: secret,
	}
	deps.RuntimeRegistry = newRuntimeRegistry(cfg, factories.Runtime, deps)

	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })

	prov, err := providers.Get("echoext")
	if err != nil {
		t.Fatalf("providers.Get: %v", err)
	}
	result, err := prov.Execute(context.Background(), "make_http_request", map[string]any{"url": targetSrv.URL + "/proxy-test"}, "")
	if err != nil {
		t.Fatalf("Execute make_http_request: %v", err)
	}

	var body struct {
		Status int    `json:"status"`
		Body   string `json:"body"`
	}
	if err := json.Unmarshal(result.Body, &body); err != nil {
		t.Fatalf("unmarshal make_http_request: %v", err)
	}
	if body.Status != http.StatusOK {
		t.Fatalf("result status = %d, want %d (body=%s)", body.Status, http.StatusOK, body.Body)
	}
	if body.Body != "egress-proxy-ok" {
		t.Fatalf("result body = %q, want %q", body.Body, "egress-proxy-ok")
	}

	startRequests := runtimeProvider.startAppRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("StartApp requests = %d, want 1", len(startRequests))
	}
	if got := startRequests[0].GetEnv()["HTTP_PROXY"]; got == "" {
		t.Fatal("StartApp env should include HTTP_PROXY")
	}
	if got := startRequests[0].GetEnv()["HTTPS_PROXY"]; got == "" {
		t.Fatal("StartApp env should include HTTPS_PROXY")
	}
}

func TestRuntimeConfigRejectsDefaultDenyWithoutHostnameEgressCapability(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	runtimeProvider := &staticCapabilityRuntime{
		inner: runtimeprovider.NewLocalProvider(),
		support: &proto.RuntimeSupport{
			CanHostApps: true,
		},
	}
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (runtimeprovider.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultProvider: "hosted"},
			Egress:  config.EgressConfig{DefaultAction: string(egress.PolicyDeny)},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Apps: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Runtime:              &config.RuntimePlacementConfig{},
			},
		},
	}

	deps := testRuntimePublicEndpointDeps(t, Deps{
		Egress: EgressDeps{DefaultAction: egress.PolicyDeny},
	})
	deps.RuntimeRegistry = newRuntimeRegistry(cfg, factories.Runtime, deps)
	_, _, err := buildProvidersStrict(context.Background(), cfg, factories, deps)
	if err == nil || !strings.Contains(err.Error(), "cannot preserve hostname-based egress required by this provider") {
		t.Fatalf("buildProvidersStrict error = %v, want hostname-based egress requirement failure", err)
	}
}

func TestPluginCacheBindingsDoNotGateSharedCacheHostServices(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")

	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Cache:                []string{"missing"},
			},
		},
	}

	providers, _, err := buildProvidersStrict(context.Background(), cfg, NewFactoryRegistry(), testRuntimePublicEndpointDeps(t, Deps{
		CacheDefs: map[string]*config.ProviderEntry{
			"session": {
				Config: mustNode(t, map[string]any{"namespace": "session"}),
			},
		},
		CacheFactory: func(yaml.Node) (corecache.Cache, error) {
			return coretesting.NewStubCache(), nil
		},
	}))
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })
}

func TestPluginIndexedDBInheritsHostSelectionAndDefaultDBName(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{
				ID:     "indexeddb_roundtrip",
				Method: http.MethodPost,
				Parameters: []catalog.CatalogParameter{
					{Name: "store", Type: "string", Required: true},
					{Name: "id", Type: "string", Required: true},
					{Name: "value", Type: "string", Required: true},
				},
			},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")

	cases := []struct {
		name      string
		indexedDB *config.IndexedDBBindingConfig
	}{
		{name: "omitted indexeddb inherits host selection"},
		{name: "empty indexeddb inherits host selection", indexedDB: &config.IndexedDBBindingConfig{}},
		{name: "objectStores-only indexeddb inherits host selection", indexedDB: &config.IndexedDBBindingConfig{ObjectStores: []string{"tasks"}}},
	}
	runtimeModes := []struct {
		name   string
		hosted bool
	}{
		{name: "local executable"},
		{name: "hosted runtime relay", hosted: true},
	}

	for _, tc := range cases {
		tc := tc
		for _, runtimeMode := range runtimeModes {
			runtimeMode := runtimeMode
			t.Run(tc.name+"/"+runtimeMode.name, func(t *testing.T) {
				t.Parallel()

				boundDB := &trackedIndexedDB{StubIndexedDB: coretesting.StubIndexedDB{}}
				var runtimeProvider *capturingRuntime
				deps := Deps{
					SelectedIndexedDBName: "memory",
					IndexedDBDefs: map[string]*config.ProviderEntry{
						"memory": {
							Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.2/provider-release.yaml"),
							Config: mustNode(t, map[string]any{"bucket": "plugin-state"}),
						},
					},
					IndexedDBFactory: func(yaml.Node) (indexeddb.IndexedDB, error) {
						return boundDB, nil
					},
				}
				if runtimeMode.hosted {
					runtimeProvider = newCapturingRuntime()
					deps.Runtime = runtimeProvider
					t.Cleanup(func() { _ = runtimeProvider.Close() })
				}

				providers, _, err := buildProvidersStrict(context.Background(), &config.Config{
					Apps: map[string]*config.ProviderEntry{
						"echoext": {
							Command:              bin,
							Args:                 []string{"provider"},
							ResolvedManifest:     manifest,
							ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
							IndexedDB:            tc.indexedDB,
						},
					},
				}, NewFactoryRegistry(), testRuntimePublicEndpointDeps(t, deps))
				if err != nil {
					t.Fatalf("buildProvidersStrict: %v", err)
				}
				t.Cleanup(func() { _ = CloseProviders(providers) })

				prov, err := providers.Get("echoext")
				if err != nil {
					t.Fatalf("providers.Get: %v", err)
				}
				result, err := prov.Execute(context.Background(), "indexeddb_roundtrip", map[string]any{
					"store": "tasks",
					"id":    "task-1",
					"value": "ship-it",
				}, "")
				if err != nil {
					t.Fatalf("Execute indexeddb_roundtrip: %v", err)
				}
				var record map[string]any
				if err := json.Unmarshal(result.Body, &record); err != nil {
					t.Fatalf("unmarshal record: %v", err)
				}
				if got := record["value"]; got != "ship-it" {
					t.Fatalf("record value = %#v, want %q", got, "ship-it")
				}
				if _, err := boundDB.ObjectStore("tasks").Get(context.Background(), "task-1"); err != nil {
					t.Fatalf("inherited indexeddb provider should expose logical store name directly: %v", err)
				}
				if runtimeProvider != nil {
					startRequests := runtimeProvider.startAppRequestsCopy()
					if len(startRequests) != 1 {
						t.Fatalf("StartApp requests = %d, want 1", len(startRequests))
					}
					assertStartAppRelayEnv(t, startRequests[0], "indexeddb")
				}
			})
		}
	}
}

func TestPluginIndexedDBUsesSharedConfiguredResources(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")

	type capturedIndexedDBConfig struct {
		Config map[string]any `yaml:"config"`
	}

	makeConfig := func(indexedDB *config.IndexedDBBindingConfig) *config.Config {
		return &config.Config{
			Apps: map[string]*config.ProviderEntry{
				"echoext": {
					Command:              bin,
					Args:                 []string{"provider"},
					ResolvedManifest:     manifest,
					ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
					IndexedDB:            indexedDB,
				},
			},
		}
	}

	indexedDBDefs := map[string]*config.ProviderEntry{
		"postgres": {
			Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.2/provider-release.yaml"),
			Config: mustNode(t, map[string]any{
				"dsn":    "postgres://db.example.test/gestalt",
				"schema": "host_schema",
			}),
		},
		"sqlite": {
			Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.2/provider-release.yaml"),
			Config: mustNode(t, map[string]any{
				"dsn":          "sqlite://plugin-state.db",
				"table_prefix": "host_",
				"prefix":       "host_",
				"schema":       "should_be_removed",
			}),
		},
		"local-postgres": {
			Source: config.ProviderSource{Path: "./relationaldb/manifest.yaml"},
			Config: mustNode(t, map[string]any{
				"dsn":    "postgres://local.example.test/gestalt",
				"schema": "host_local",
			}),
		},
		"mysql-secret": {
			Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.2/provider-release.yaml"),
			Config: mustNode(t, map[string]any{
				"dsn": map[string]any{
					"secret": map[string]any{
						"provider": "secrets",
						"name":     "gestalt-mysql-dsn-east4",
					},
				},
				"schema": "host_secret",
			}),
		},
	}

	cases := []struct {
		name       string
		indexedDB  *config.IndexedDBBindingConfig
		wantDSN    string
		wantDB     string
		wantSQLite bool
		wantSecret bool
	}{
		{
			name:      "defaults db to app name for postgres",
			indexedDB: &config.IndexedDBBindingConfig{Provider: "postgres"},
			wantDSN:   "postgres://db.example.test/gestalt",
			wantDB:    "host_schema",
		},
		{
			name:      "uses db override for postgres",
			indexedDB: &config.IndexedDBBindingConfig{Provider: "postgres", DB: "roadmap_state"},
			wantDSN:   "postgres://db.example.test/gestalt",
			wantDB:    "host_schema",
		},
		{
			name:       "uses db override for sqlite table prefixes",
			indexedDB:  &config.IndexedDBBindingConfig{Provider: "sqlite", DB: "roadmap_state"},
			wantDSN:    "sqlite://plugin-state.db",
			wantDB:     "should_be_removed",
			wantSQLite: true,
		},
		{
			name:       "uses schema scope for secret-backed relational DSNs",
			indexedDB:  &config.IndexedDBBindingConfig{Provider: "mysql-secret", DB: "secret_state"},
			wantDB:     "host_secret",
			wantSecret: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var captured []capturedIndexedDBConfig
			providers, _, err := buildProvidersStrict(context.Background(), makeConfig(tc.indexedDB), NewFactoryRegistry(), testRuntimePublicEndpointDeps(t, Deps{
				SelectedIndexedDBName: "postgres",
				IndexedDBDefs:         indexedDBDefs,
				IndexedDBFactory: func(node yaml.Node) (indexeddb.IndexedDB, error) {
					var decoded capturedIndexedDBConfig
					if err := node.Decode(&decoded); err != nil {
						return nil, err
					}
					captured = append(captured, decoded)
					return &trackedIndexedDB{
						StubIndexedDB: coretesting.StubIndexedDB{},
					}, nil
				},
			}))
			if err != nil {
				t.Fatalf("buildProvidersStrict: %v", err)
			}
			t.Cleanup(func() {
				if providers != nil {
					_ = CloseProviders(providers)
				}
			})

			var cfg capturedIndexedDBConfig
			if tc.wantSecret {
				for _, candidate := range captured {
					if _, ok := candidate.Config["dsn"].(map[string]any); ok && candidate.Config["schema"] == tc.wantDB {
						cfg = candidate
						break
					}
				}
			} else {
				for _, candidate := range captured {
					if dsn, _ := candidate.Config["dsn"].(string); dsn == tc.wantDSN {
						cfg = candidate
						break
					}
				}
			}
			if cfg.Config == nil {
				t.Fatalf("missing captured indexeddb config for case %q", tc.name)
			}
			if tc.wantSQLite {
				if got := cfg.Config["table_prefix"]; got != "host_" {
					t.Fatalf("sqlite table_prefix = %#v, want %q", got, "host_")
				}
				if got := cfg.Config["prefix"]; got != "host_" {
					t.Fatalf("sqlite prefix = %#v, want %q", got, "host_")
				}
				if got := cfg.Config["schema"]; got != tc.wantDB {
					t.Fatalf("sqlite schema = %#v, want %q", got, tc.wantDB)
				}
			} else {
				if got := cfg.Config["schema"]; got != tc.wantDB {
					t.Fatalf("schema = %#v, want %q", got, tc.wantDB)
				}
				if _, ok := cfg.Config["table_prefix"]; ok {
					t.Fatalf("table_prefix should be removed, got %#v", cfg.Config["table_prefix"])
				}
				if _, ok := cfg.Config["prefix"]; ok {
					t.Fatalf("prefix should be removed, got %#v", cfg.Config["prefix"])
				}
			}
			_ = CloseProviders(providers)
			providers = nil
		})
	}
}

func TestPluginIndexedDBRouteObjectStores(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{
				ID:     "indexeddb_roundtrip",
				Method: http.MethodPost,
				Parameters: []catalog.CatalogParameter{
					{Name: "store", Type: "string", Required: true},
					{Name: "id", Type: "string", Required: true},
					{Name: "value", Type: "string", Required: true},
				},
			},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")

	runtimeModes := []struct {
		name   string
		hosted bool
	}{
		{name: "local executable"},
		{name: "hosted runtime relay", hosted: true},
	}

	for _, runtimeMode := range runtimeModes {
		runtimeMode := runtimeMode
		t.Run(runtimeMode.name, func(t *testing.T) {
			t.Parallel()

			var (
				boundDB         *trackedIndexedDB
				runtimeProvider *capturingRuntime
			)
			deps := Deps{
				SelectedIndexedDBName: "memory",
				IndexedDBDefs: map[string]*config.ProviderEntry{
					"memory": {
						Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.2/provider-release.yaml"),
						Config: mustNode(t, map[string]any{"bucket": "plugin-state"}),
					},
				},
				IndexedDBFactory: func(yaml.Node) (indexeddb.IndexedDB, error) {
					boundDB = &trackedIndexedDB{
						StubIndexedDB: coretesting.StubIndexedDB{},
					}
					return boundDB, nil
				},
			}
			if runtimeMode.hosted {
				runtimeProvider = newCapturingRuntime()
				deps.Runtime = runtimeProvider
				t.Cleanup(func() { _ = runtimeProvider.Close() })
			}

			providers, _, err := buildProvidersStrict(context.Background(), &config.Config{
				Apps: map[string]*config.ProviderEntry{
					"echoext": {
						Command:              bin,
						Args:                 []string{"provider"},
						ResolvedManifest:     manifest,
						ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
						IndexedDB: &config.IndexedDBBindingConfig{
							Provider:     "memory",
							DB:           "roadmap",
							ObjectStores: []string{"tasks"},
						},
					},
				},
			}, NewFactoryRegistry(), testRuntimePublicEndpointDeps(t, deps))
			if err != nil {
				t.Fatalf("buildProvidersStrict: %v", err)
			}
			t.Cleanup(func() { _ = CloseProviders(providers) })

			prov, err := providers.Get("echoext")
			if err != nil {
				t.Fatalf("providers.Get: %v", err)
			}

			result, err := prov.Execute(context.Background(), "indexeddb_roundtrip", map[string]any{
				"store": "tasks",
				"id":    "task-1",
				"value": "ship-it",
			}, "")
			if err != nil {
				t.Fatalf("Execute indexeddb_roundtrip: %v", err)
			}
			var record map[string]any
			if err := json.Unmarshal(result.Body, &record); err != nil {
				t.Fatalf("unmarshal record: %v", err)
			}
			if got := record["value"]; got != "ship-it" {
				t.Fatalf("record value = %#v, want %q", got, "ship-it")
			}
			if _, err := boundDB.ObjectStore("tasks").Get(context.Background(), "task-1"); err != nil {
				t.Fatalf("logical backing store should contain task: %v", err)
			}

			blockedResult, err := prov.Execute(context.Background(), "indexeddb_roundtrip", map[string]any{
				"store": "events",
				"id":    "evt-1",
				"value": "blocked",
			}, "")
			if err != nil {
				t.Fatalf("Execute indexeddb_roundtrip on additional object store: %v", err)
			}
			if blockedResult == nil || blockedResult.Status != 200 {
				t.Fatalf("indexeddb_roundtrip on additional object store status = %#v, want success", blockedResult)
			}
			if runtimeProvider != nil {
				startRequests := runtimeProvider.startAppRequestsCopy()
				if len(startRequests) != 1 {
					t.Fatalf("StartApp requests = %d, want 1", len(startRequests))
				}
				assertStartAppRelayEnv(t, startRequests[0], "indexeddb")
			}

			_ = CloseProviders(providers)
			providers = nil
		})
	}
}

func TestPluginIndexedDBUsesSharedDefaultIndexedDB(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{
				ID:     "indexeddb_roundtrip",
				Method: http.MethodPost,
				Parameters: []catalog.CatalogParameter{
					{Name: "store", Type: "string", Required: true},
					{Name: "id", Type: "string", Required: true},
					{Name: "value", Type: "string", Required: true},
				},
			},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")

	boundDBs := make(map[string]*trackedIndexedDB)
	providers, _, err := buildProvidersStrict(context.Background(), &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				IndexedDB: &config.IndexedDBBindingConfig{
					Provider: "archive",
					DB:       "roadmap",
				},
			},
		},
	}, NewFactoryRegistry(), testRuntimePublicEndpointDeps(t, Deps{
		SelectedIndexedDBName: "main",
		IndexedDBDefs: map[string]*config.ProviderEntry{
			"main": {
				Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.2/provider-release.yaml"),
				Config: mustNode(t, map[string]any{"bucket": "main"}),
			},
			"archive": {
				Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.2/provider-release.yaml"),
				Config: mustNode(t, map[string]any{"bucket": "archive"}),
			},
		},
		IndexedDBFactory: func(node yaml.Node) (indexeddb.IndexedDB, error) {
			var decoded struct {
				Config map[string]any `yaml:"config"`
			}
			if err := node.Decode(&decoded); err != nil {
				return nil, err
			}
			bucket, _ := decoded.Config["bucket"].(string)
			db := &trackedIndexedDB{StubIndexedDB: coretesting.StubIndexedDB{}}
			boundDBs[bucket] = db
			return db, nil
		},
	}))
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })

	prov, err := providers.Get("echoext")
	if err != nil {
		t.Fatalf("providers.Get: %v", err)
	}

	result, err := prov.Execute(context.Background(), "indexeddb_roundtrip", map[string]any{
		"store": "events",
		"id":    "evt-1",
		"value": "stored",
	}, "")
	if err != nil {
		t.Fatalf("Execute indexeddb_roundtrip: %v", err)
	}
	var record map[string]any
	if err := json.Unmarshal(result.Body, &record); err != nil {
		t.Fatalf("unmarshal record: %v", err)
	}
	if got := record["value"]; got != "stored" {
		t.Fatalf("record value = %#v, want %q", got, "stored")
	}
	if len(boundDBs) != 2 {
		t.Fatalf("boundDBs = %d, want both configured providers built", len(boundDBs))
	}
	if _, err := boundDBs["main"].ObjectStore("events").Get(context.Background(), "evt-1"); err != nil {
		t.Fatalf("main backing store should contain event from default binding: %v", err)
	}
	if _, err := boundDBs["archive"].ObjectStore("events").Get(context.Background(), "evt-1"); err != nil {
		if !strings.Contains(err.Error(), "not found") {
			t.Fatalf("archive backing store error = %v, want not found", err)
		}
	}
}

func TestPluginIndexedDBBindingsDoNotDependOnAppS3Bindings(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")

	providers, _, err := buildProvidersStrict(context.Background(), &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				IndexedDB:            &config.IndexedDBBindingConfig{Provider: "main"},
				S3:                   []string{"missing"},
			},
		},
	}, NewFactoryRegistry(), testRuntimePublicEndpointDeps(t, Deps{
		SelectedIndexedDBName: "main",
		IndexedDBDefs: map[string]*config.ProviderEntry{
			"main": {
				Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.2/provider-release.yaml"),
				Config: mustNode(t, map[string]any{"bucket": "main"}),
			},
		},
		IndexedDBFactory: func(yaml.Node) (indexeddb.IndexedDB, error) {
			return &trackedIndexedDB{
				StubIndexedDB: coretesting.StubIndexedDB{},
			}, nil
		},
		S3: map[string]s3sdk.S3{},
	}))
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })
}

func TestPluginS3BindingsRoundtripKeys(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{
				ID:     "s3_roundtrip",
				Method: http.MethodPost,
				Parameters: []catalog.CatalogParameter{
					{Name: "key", Type: "string", Required: true},
					{Name: "value", Type: "string", Required: true},
				},
			},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")

	stubS3 := &coretesting.StubS3{}
	providers, _, err := buildProvidersStrict(context.Background(), &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				S3:                   []string{"main"},
			},
		},
	}, NewFactoryRegistry(), testRuntimePublicEndpointDeps(t, Deps{
		Services: testutil.NewStubServices(t),
		S3: map[string]s3sdk.S3{
			"main":    stubS3,
			"archive": &coretesting.StubS3{},
		},
	}))
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })

	prov, err := providers.Get("echoext")
	if err != nil {
		t.Fatalf("providers.Get: %v", err)
	}

	result, err := prov.Execute(context.Background(), "s3_roundtrip", map[string]any{
		"key":   "plans/q1.txt",
		"value": "ship-it",
	}, "")
	if err != nil {
		t.Fatalf("Execute s3_roundtrip: %v", err)
	}
	var body struct {
		Body  string   `json:"body"`
		Key   string   `json:"key"`
		Keys  []string `json:"keys"`
		Type  string   `json:"type"`
		Size  int64    `json:"size"`
		Found bool     `json:"found"`
	}
	if err := json.Unmarshal(result.Body, &body); err != nil {
		t.Fatalf("unmarshal roundtrip body: %v", err)
	}
	if body.Body != "ship-it" {
		t.Fatalf("body = %q, want %q", body.Body, "ship-it")
	}
	if body.Key != "plans/q1.txt" {
		t.Fatalf("key = %q, want %q", body.Key, "plans/q1.txt")
	}
	if !slices.Equal(body.Keys, []string{"plans/q1.txt"}) {
		t.Fatalf("keys = %#v, want %#v", body.Keys, []string{"plans/q1.txt"})
	}
	if body.Type != "text/plain" {
		t.Fatalf("content type = %q, want %q", body.Type, "text/plain")
	}
	if body.Size != int64(len("ship-it")) {
		t.Fatalf("size = %d, want %d", body.Size, len("ship-it"))
	}
	if !body.Found {
		t.Fatal("expected list operation to find the written object")
	}

	if _, err := stubS3.HeadObject(context.Background(), s3sdk.ObjectRef{
		Key: "plans/q1.txt",
	}); err != nil {
		t.Fatalf("expected backing key: %v", err)
	}
}

func TestPluginS3BindingsRouteExplicitBinding(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{
				ID:     "s3_roundtrip",
				Method: http.MethodPost,
				Parameters: []catalog.CatalogParameter{
					{Name: "binding", Type: "string"},
					{Name: "key", Type: "string", Required: true},
					{Name: "value", Type: "string", Required: true},
				},
			},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")

	mainS3 := &coretesting.StubS3{}
	archiveS3 := &coretesting.StubS3{}
	providers, _, err := buildProvidersStrict(context.Background(), &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				S3:                   []string{"main", "archive"},
			},
		},
	}, NewFactoryRegistry(), testRuntimePublicEndpointDeps(t, Deps{
		Services: testutil.NewStubServices(t),
		S3: map[string]s3sdk.S3{
			"main":    mainS3,
			"archive": archiveS3,
		},
	}))
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })

	prov, err := providers.Get("echoext")
	if err != nil {
		t.Fatalf("providers.Get: %v", err)
	}
	if _, err := prov.Execute(context.Background(), "s3_roundtrip", map[string]any{
		"binding": "archive",
		"key":     "plans/q2.txt",
		"value":   "ship-archive",
	}, ""); err != nil {
		t.Fatalf("Execute s3_roundtrip: %v", err)
	}

	if _, err := archiveS3.HeadObject(context.Background(), s3sdk.ObjectRef{
		Key: "plans/q2.txt",
	}); err != nil {
		t.Fatalf("archive binding should receive the write: %v", err)
	}
	if _, err := mainS3.HeadObject(context.Background(), s3sdk.ObjectRef{
		Key: "plans/q2.txt",
	}); err == nil {
		t.Fatal("main binding should remain untouched when archive is selected explicitly")
	}
}

func TestPluginS3BindingsExposeHostSocketEnv(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")

	makeConfig := func(bindings []string) *config.Config {
		return &config.Config{
			Apps: map[string]*config.ProviderEntry{
				"echoext": {
					Command:              bin,
					Args:                 []string{"provider"},
					ResolvedManifest:     manifest,
					ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
					S3:                   bindings,
				},
			},
		}
	}

	services := testutil.NewStubServices(t)
	s3Bindings := map[string]s3sdk.S3{
		"main":    &coretesting.StubS3{},
		"archive": &coretesting.StubS3{},
	}

	checkEnv := func(t *testing.T, bindings []string, envName string) bool {
		t.Helper()
		providers, _, err := buildProvidersStrict(context.Background(), makeConfig(bindings), NewFactoryRegistry(), testRuntimePublicEndpointDeps(t, Deps{
			Services: services,
			S3:       s3Bindings,
		}))
		if err != nil {
			t.Fatalf("buildProvidersStrict: %v", err)
		}
		defer func() { _ = CloseProviders(providers) }()

		prov, err := providers.Get("echoext")
		if err != nil {
			t.Fatalf("providers.Get: %v", err)
		}
		result, err := prov.Execute(context.Background(), "read_env", map[string]any{"name": envName}, "")
		if err != nil {
			t.Fatalf("Execute read_env: %v", err)
		}
		var env struct {
			Value string `json:"value"`
			Found bool   `json:"found"`
		}
		if err := json.Unmarshal(result.Body, &env); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return env.Found && env.Value != ""
	}
	if got := checkEnv(t, []string{"main"}, runtimehost.HostServiceSocketEnv); !got {
		t.Fatal("unified host-service env should be set with a single app s3 binding")
	}
	if got := checkEnv(t, []string{"main", "archive"}, runtimehost.HostServiceSocketEnv); !got {
		t.Fatal("unified host-service env should be set with multiple app s3 bindings")
	}
}

type trackedIndexedDB struct {
	coretesting.StubIndexedDB
	onClose func(int32) int32
}

func (t *trackedIndexedDB) Close() error {
	if t.onClose != nil {
		t.onClose(1)
	}
	return t.StubIndexedDB.Close()
}

func TestExecutablePluginRequiresManifest(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"echoext": {
				Command: bin,
				Args:    []string{"provider"},
			},
		},
	}

	factories := NewFactoryRegistry()
	_, _, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{})
	if err == nil {
		t.Fatal("expected buildProvidersStrict to reject executable app without manifest")
	}
	if got := err.Error(); got != `bootstrap: provider validation failed: integration "echoext": integration "echoext" must resolve to a provider manifest` {
		t.Fatalf("unexpected error: %v", err)
	}
}

// stubTelemetryWithEnv is a core.TelemetryProvider that also implements
// runtimehost's ProviderTelemetryEnv, so tests can verify OTLP env vars
// are propagated to hosted provider StartApp requests.
type stubTelemetryWithEnv struct {
	telemetrynoop.Provider
	env map[string]string
}

func (s *stubTelemetryWithEnv) ProviderTelemetryEnv(providerName string) map[string]string {
	out := make(map[string]string, len(s.env))
	maps.Copy(out, s.env)
	return out
}

func TestHostedAppStartAppReceivesTelemetryEnv(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "echo", Method: http.MethodPost},
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	artifactBytes, err := os.ReadFile(bin)
	if err != nil {
		t.Fatalf("ReadFile(bin): %v", err)
	}
	artifactFile := filepath.Join(manifestRoot, filepath.Base(bin))
	if err := os.WriteFile(artifactFile, artifactBytes, 0o755); err != nil {
		t.Fatalf("WriteFile(artifact): %v", err)
	}
	artifactDigest, err := providerpkg.FileSHA256(artifactFile)
	if err != nil {
		t.Fatalf("FileSHA256: %v", err)
	}
	manifest.Artifacts = []providermanifestv1.Artifact{{
		OS:     runtime.GOOS,
		Arch:   runtime.GOARCH,
		Path:   filepath.Base(bin),
		SHA256: artifactDigest,
	}}
	manifest.Entrypoint = &providermanifestv1.Entrypoint{ArtifactPath: filepath.Base(bin)}
	manifestPath := filepath.Join(manifestRoot, "manifest.yaml")
	manifestData, err := providerpkg.EncodeManifestFormat(manifest, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeManifestFormat: %v", err)
	}
	if err := os.WriteFile(manifestPath, manifestData, 0o644); err != nil {
		t.Fatalf("WriteFile(manifest.yaml): %v", err)
	}

	runtimeProvider := newCapturingBundleRuntime()
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (runtimeprovider.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {Driver: config.RuntimeProviderDriver("capture")},
			},
		},
		Apps: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: manifestPath,
				Runtime:              &config.RuntimePlacementConfig{},
			},
		},
	}

	deps := testRuntimePublicEndpointDeps(t, Deps{})
	deps.Telemetry = &stubTelemetryWithEnv{
		env: map[string]string{
			"OTEL_EXPORTER_OTLP_ENDPOINT": "otel-collector:4317",
			"OTEL_SERVICE_NAME":           "test-echoext",
		},
	}
	deps.RuntimeRegistry = newRuntimeRegistry(cfg, factories.Runtime, deps)
	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	prov, err := providers.Get("echoext")
	if err != nil {
		t.Fatalf("providers.Get(echoext): %v", err)
	}
	if _, err := prov.Execute(context.Background(), "echo", map[string]any{"name": "test"}, ""); err != nil {
		t.Fatalf("Execute(echo): %v", err)
	}

	requests := runtimeProvider.startAppRequestsCopy()
	if len(requests) != 1 {
		t.Fatalf("start app requests = %d, want 1", len(requests))
	}
	env := requests[0].GetEnv()
	if got := env["OTEL_EXPORTER_OTLP_ENDPOINT"]; got != "otel-collector:4317" {
		t.Fatalf("StartApp OTEL_EXPORTER_OTLP_ENDPOINT = %q, want otel-collector:4317", got)
	}
	if got := env["OTEL_SERVICE_NAME"]; got != "test-echoext" {
		t.Fatalf("StartApp OTEL_SERVICE_NAME = %q, want test-echoext", got)
	}
}
