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
	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	corecache "github.com/valon-technologies/gestalt/server/core/cache"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	s3store "github.com/valon-technologies/gestalt/server/core/s3"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	agentservice "github.com/valon-technologies/gestalt/server/services/agents"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	authorizationservice "github.com/valon-technologies/gestalt/server/services/authorization"
	cacheservice "github.com/valon-technologies/gestalt/server/services/cache"
	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/egressproxy"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	indexeddbservice "github.com/valon-technologies/gestalt/server/services/indexeddb"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	plugininvokerservice "github.com/valon-technologies/gestalt/server/services/plugininvoker"
	pluginservice "github.com/valon-technologies/gestalt/server/services/plugins"
	graphqlschema "github.com/valon-technologies/gestalt/server/services/plugins/graphql"
	"github.com/valon-technologies/gestalt/server/services/plugins/providerpkg"
	"github.com/valon-technologies/gestalt/server/services/plugins/registry"
	"github.com/valon-technologies/gestalt/server/services/providerdev"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/pluginruntime"
	s3service "github.com/valon-technologies/gestalt/server/services/s3"
	workflowservice "github.com/valon-technologies/gestalt/server/services/workflows"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
	"gopkg.in/yaml.v3"
)

type invokePluginEnvelope struct {
	OK                     bool               `json:"ok"`
	TargetPlugin           string             `json:"target_plugin"`
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
	InvocationToken string `json:"invocation_token"`
}

type nestedInvokeHarness struct {
	invoker  invocation.Invoker
	services *coredata.Services
}

type hostedHTTPAuthorizationProvider struct{}

func (p *hostedHTTPAuthorizationProvider) Name() string { return "authz" }

func (p *hostedHTTPAuthorizationProvider) Evaluate(context.Context, *core.AccessEvaluationRequest) (*core.AccessDecision, error) {
	return &core.AccessDecision{}, nil
}

func (p *hostedHTTPAuthorizationProvider) EvaluateMany(context.Context, *core.AccessEvaluationsRequest) (*core.AccessEvaluationsResponse, error) {
	return &core.AccessEvaluationsResponse{}, nil
}

func (p *hostedHTTPAuthorizationProvider) SearchResources(context.Context, *core.ResourceSearchRequest) (*core.ResourceSearchResponse, error) {
	return &core.ResourceSearchResponse{}, nil
}

func (p *hostedHTTPAuthorizationProvider) SearchSubjects(context.Context, *core.SubjectSearchRequest) (*core.SubjectSearchResponse, error) {
	return &core.SubjectSearchResponse{}, nil
}

func (p *hostedHTTPAuthorizationProvider) SearchActions(context.Context, *core.ActionSearchRequest) (*core.ActionSearchResponse, error) {
	return &core.ActionSearchResponse{}, nil
}

func (p *hostedHTTPAuthorizationProvider) GetMetadata(context.Context) (*core.AuthorizationMetadata, error) {
	return &core.AuthorizationMetadata{}, nil
}

func (p *hostedHTTPAuthorizationProvider) ReadRelationships(context.Context, *core.ReadRelationshipsRequest) (*core.ReadRelationshipsResponse, error) {
	return &core.ReadRelationshipsResponse{}, nil
}

func (p *hostedHTTPAuthorizationProvider) WriteRelationships(context.Context, *core.WriteRelationshipsRequest) error {
	return nil
}

func (p *hostedHTTPAuthorizationProvider) GetActiveModel(context.Context) (*core.GetActiveModelResponse, error) {
	return &core.GetActiveModelResponse{}, nil
}

func (p *hostedHTTPAuthorizationProvider) ListModels(context.Context, *core.ListModelsRequest) (*core.ListModelsResponse, error) {
	return &core.ListModelsResponse{}, nil
}

func (p *hostedHTTPAuthorizationProvider) WriteModel(context.Context, *core.WriteModelRequest) (*core.AuthorizationModelRef, error) {
	return &core.AuthorizationModelRef{}, nil
}

type authorizationSearchCall struct {
	SubjectType  string
	ResourceType string
	ResourceID   string
	ActionName   string
	PageSize     int32
}

type recordingHostedAuthorizationProvider struct {
	hostedHTTPAuthorizationProvider

	mu          sync.Mutex
	searchCalls []authorizationSearchCall
}

func (p *recordingHostedAuthorizationProvider) SearchSubjects(_ context.Context, req *core.SubjectSearchRequest) (*core.SubjectSearchResponse, error) {
	call := authorizationSearchCall{
		SubjectType: req.GetSubjectType(),
		PageSize:    req.GetPageSize(),
	}
	if resource := req.GetResource(); resource != nil {
		call.ResourceType = resource.GetType()
		call.ResourceID = resource.GetId()
	}
	if action := req.GetAction(); action != nil {
		call.ActionName = action.GetName()
	}

	p.mu.Lock()
	p.searchCalls = append(p.searchCalls, call)
	p.mu.Unlock()

	return &core.SubjectSearchResponse{
		Subjects: []*core.SubjectRef{{
			Type: "user",
			Id:   "user:user-123",
		}},
		ModelId: "authz-model-1",
	}, nil
}

func (p *recordingHostedAuthorizationProvider) Calls() []authorizationSearchCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.searchCalls)
}

type capturingPluginRuntime struct {
	provider *pluginruntime.LocalProvider

	mu                  sync.Mutex
	startRequests       []pluginruntime.StartSessionRequest
	startPluginRequests []pluginruntime.StartPluginRequest
	startTimes          []time.Time
	sessionLifecycles   map[string]*pluginruntime.SessionLifecycle
	lifecycleForSession func(index int) *pluginruntime.SessionLifecycle
	startErrForSession  func(index int) error
	stopCount           atomic.Int32
}

func newCapturingPluginRuntime() *capturingPluginRuntime {
	return &capturingPluginRuntime{provider: pluginruntime.NewLocalProvider()}
}

func (r *capturingPluginRuntime) Support(ctx context.Context) (pluginruntime.Support, error) {
	return r.provider.Support(ctx)
}

func (r *capturingPluginRuntime) StartSession(ctx context.Context, req pluginruntime.StartSessionRequest) (*pluginruntime.Session, error) {
	r.mu.Lock()
	r.startRequests = append(r.startRequests, pluginruntime.StartSessionRequest{
		PluginName:    req.PluginName,
		Template:      req.Template,
		Image:         req.Image,
		ImagePullAuth: cloneImagePullAuth(req.ImagePullAuth),
		Metadata:      cloneRuntimeMetadata(req.Metadata),
	})
	r.startTimes = append(r.startTimes, time.Now().UTC())
	index := len(r.startRequests)
	lifecycleForSession := r.lifecycleForSession
	startErrForSession := r.startErrForSession
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
		session.Lifecycle = clonePluginRuntimeSessionLifecycle(lifecycleForSession(index))
		r.mu.Lock()
		if r.sessionLifecycles == nil {
			r.sessionLifecycles = map[string]*pluginruntime.SessionLifecycle{}
		}
		r.sessionLifecycles[session.ID] = clonePluginRuntimeSessionLifecycle(session.Lifecycle)
		r.mu.Unlock()
	}
	return session, nil
}

func (r *capturingPluginRuntime) ListSessions(ctx context.Context) ([]pluginruntime.Session, error) {
	sessions, err := r.provider.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	for i := range sessions {
		r.attachSessionLifecycle(&sessions[i])
	}
	return sessions, nil
}

func (r *capturingPluginRuntime) GetSession(ctx context.Context, req pluginruntime.GetSessionRequest) (*pluginruntime.Session, error) {
	session, err := r.provider.GetSession(ctx, req)
	if err != nil {
		return nil, err
	}
	r.attachSessionLifecycle(session)
	return session, nil
}

func (r *capturingPluginRuntime) StopSession(ctx context.Context, req pluginruntime.StopSessionRequest) error {
	r.stopCount.Add(1)
	return r.provider.StopSession(ctx, req)
}

func (r *capturingPluginRuntime) StartPlugin(ctx context.Context, req pluginruntime.StartPluginRequest) (*pluginruntime.HostedPlugin, error) {
	r.mu.Lock()
	r.startPluginRequests = append(r.startPluginRequests, pluginruntime.StartPluginRequest{
		SessionID:  req.SessionID,
		PluginName: req.PluginName,
		Command:    req.Command,
		Args:       slices.Clone(req.Args),
		Env:        cloneRuntimeMetadata(req.Env),
		Egress:     cloneRuntimeEgressPolicy(req.Egress),
		HostBinary: req.HostBinary,
	})
	r.mu.Unlock()
	return r.provider.StartPlugin(ctx, req)
}

func (r *capturingPluginRuntime) Close() error {
	return r.provider.Close()
}

func (r *capturingPluginRuntime) startSessionRequests() []pluginruntime.StartSessionRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]pluginruntime.StartSessionRequest, len(r.startRequests))
	for i, req := range r.startRequests {
		out[i] = pluginruntime.StartSessionRequest{
			PluginName:    req.PluginName,
			Template:      req.Template,
			Image:         req.Image,
			ImagePullAuth: cloneImagePullAuth(req.ImagePullAuth),
			Metadata:      cloneRuntimeMetadata(req.Metadata),
		}
	}
	return out
}

func cloneImagePullAuth(src *pluginruntime.ImagePullAuth) *pluginruntime.ImagePullAuth {
	if src == nil {
		return nil
	}
	return &pluginruntime.ImagePullAuth{
		DockerConfigJSON: src.DockerConfigJSON,
	}
}

func (r *capturingPluginRuntime) startSessionTimes() []time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Time(nil), r.startTimes...)
}

func (r *capturingPluginRuntime) startPluginRequestsCopy() []pluginruntime.StartPluginRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]pluginruntime.StartPluginRequest, len(r.startPluginRequests))
	for i, req := range r.startPluginRequests {
		out[i] = pluginruntime.StartPluginRequest{
			SessionID:  req.SessionID,
			PluginName: req.PluginName,
			Command:    req.Command,
			Args:       slices.Clone(req.Args),
			Env:        cloneRuntimeMetadata(req.Env),
			Egress:     cloneRuntimeEgressPolicy(req.Egress),
			HostBinary: req.HostBinary,
		}
	}
	return out
}

func (r *capturingPluginRuntime) attachSessionLifecycle(session *pluginruntime.Session) {
	if session == nil || session.ID == "" {
		return
	}
	r.mu.Lock()
	lifecycle := clonePluginRuntimeSessionLifecycle(r.sessionLifecycles[session.ID])
	r.mu.Unlock()
	session.Lifecycle = lifecycle
}

func clonePluginRuntimeSessionLifecycle(lifecycle *pluginruntime.SessionLifecycle) *pluginruntime.SessionLifecycle {
	if lifecycle == nil {
		return nil
	}
	return &pluginruntime.SessionLifecycle{
		StartedAt:          cloneTestTime(lifecycle.StartedAt),
		RecommendedDrainAt: cloneTestTime(lifecycle.RecommendedDrainAt),
		ExpiresAt:          cloneTestTime(lifecycle.ExpiresAt),
	}
}

func cloneTestTime(src *time.Time) *time.Time {
	if src == nil {
		return nil
	}
	out := src.UTC()
	return &out
}

type capturingBundlePluginRuntime struct {
	provider   *pluginruntime.LocalProvider
	support    pluginruntime.Support
	fakeHosted bool

	mu                  sync.Mutex
	startPluginRequests []pluginruntime.StartPluginRequest
	sessionLifecycles   map[string]*pluginruntime.SessionLifecycle
	fakePlugins         map[string]*fakeHostedPluginServer
}

type fakeHostedPluginServer struct {
	dir      string
	listener net.Listener
	server   *grpc.Server
}

func newCapturingBundlePluginRuntime() *capturingBundlePluginRuntime {
	return &capturingBundlePluginRuntime{
		provider: pluginruntime.NewLocalProvider(),
		support: pluginruntime.Support{
			CanHostPlugins: true,
		},
		fakePlugins: make(map[string]*fakeHostedPluginServer),
	}
}

func (r *capturingBundlePluginRuntime) Support(context.Context) (pluginruntime.Support, error) {
	return r.support, nil
}

func (r *capturingBundlePluginRuntime) StartSession(ctx context.Context, req pluginruntime.StartSessionRequest) (*pluginruntime.Session, error) {
	session, err := r.provider.StartSession(ctx, req)
	if err != nil {
		return nil, err
	}
	r.attachSessionLifecycle(session)
	return session, nil
}

func (r *capturingBundlePluginRuntime) ListSessions(ctx context.Context) ([]pluginruntime.Session, error) {
	sessions, err := r.provider.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	for i := range sessions {
		r.attachSessionLifecycle(&sessions[i])
	}
	return sessions, nil
}

func (r *capturingBundlePluginRuntime) GetSession(ctx context.Context, req pluginruntime.GetSessionRequest) (*pluginruntime.Session, error) {
	session, err := r.provider.GetSession(ctx, req)
	if err != nil {
		return nil, err
	}
	r.attachSessionLifecycle(session)
	return session, nil
}

func (r *capturingBundlePluginRuntime) StopSession(ctx context.Context, req pluginruntime.StopSessionRequest) error {
	r.cleanupFakeHostedPlugin(req.SessionID)
	return r.provider.StopSession(ctx, req)
}

func (r *capturingBundlePluginRuntime) StartPlugin(ctx context.Context, req pluginruntime.StartPluginRequest) (*pluginruntime.HostedPlugin, error) {
	r.mu.Lock()
	r.startPluginRequests = append(r.startPluginRequests, pluginruntime.StartPluginRequest{
		SessionID:  req.SessionID,
		PluginName: req.PluginName,
		Command:    req.Command,
		Args:       slices.Clone(req.Args),
		Env:        cloneRuntimeMetadata(req.Env),
		Egress:     cloneRuntimeEgressPolicy(req.Egress),
		HostBinary: req.HostBinary,
	})
	r.mu.Unlock()

	if r.fakeHosted {
		return r.startFakeHostedPlugin(req)
	}
	return r.provider.StartPlugin(ctx, req)
}

func (r *capturingBundlePluginRuntime) Close() error {
	r.mu.Lock()
	sessionIDs := make([]string, 0, len(r.fakePlugins))
	for sessionID := range r.fakePlugins {
		sessionIDs = append(sessionIDs, sessionID)
	}
	r.mu.Unlock()
	for _, sessionID := range sessionIDs {
		r.cleanupFakeHostedPlugin(sessionID)
	}
	return r.provider.Close()
}

func (r *capturingBundlePluginRuntime) startFakeHostedPlugin(req pluginruntime.StartPluginRequest) (*pluginruntime.HostedPlugin, error) {
	env := cloneRuntimeMetadata(req.Env)
	dir, err := pluginservice.NewPluginTempDir("gstp-fake-")
	if err != nil {
		return nil, fmt.Errorf("create fake hosted plugin dir: %w", err)
	}
	socketPath := filepath.Join(dir, "plugin.sock")
	lis, err := net.Listen("unix", socketPath)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("listen for fake hosted plugin: %w", err)
	}

	srv := grpc.NewServer()
	proto.RegisterIntegrationProviderServer(srv, pluginservice.NewServer(&coretesting.StubIntegration{
		N:        req.PluginName,
		DN:       "Fake Hosted Plugin",
		Desc:     "test-only fake hosted plugin",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{
			Name: req.PluginName,
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
						{Name: "bucket", Type: "string", Required: true},
						{Name: "key", Type: "string", Required: true},
						{Name: "value", Type: "string", Required: true},
					},
				},
				{
					ID:     "invoke_plugin",
					Method: http.MethodPost,
					Parameters: []catalog.CatalogParameter{
						{Name: "plugin", Type: "string", Required: true},
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
					ID:     "authorization_roundtrip",
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
				return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil
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
				return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil
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
				return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil
			case "s3_roundtrip":
				bucket, _ := params["bucket"].(string)
				key, _ := params["key"].(string)
				value, _ := params["value"].(string)
				binding, _ := params["binding"].(string)
				record, err := fakeHostedS3RoundTrip(bucket, key, value, binding, env)
				if err != nil {
					return nil, err
				}
				body, err := json.Marshal(record)
				if err != nil {
					return nil, err
				}
				return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil
			case "invoke_plugin":
				targetPlugin, _ := params["plugin"].(string)
				targetOperation, _ := params["operation"].(string)
				envelope, err := fakeHostedInvokePlugin(targetPlugin, targetOperation, plugininvokerservice.InvocationTokenFromContext(ctx), env)
				if err != nil {
					envelope = invokePluginEnvelope{
						OK:              false,
						TargetPlugin:    targetPlugin,
						TargetOperation: targetOperation,
						Error:           err.Error(),
					}
				}
				body, err := json.Marshal(envelope)
				if err != nil {
					return nil, err
				}
				return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil
			case "workflow_manager_roundtrip":
				record, err := fakeHostedWorkflowManagerRoundTrip(plugininvokerservice.InvocationTokenFromContext(ctx), env)
				if err != nil {
					return nil, err
				}
				body, err := json.Marshal(record)
				if err != nil {
					return nil, err
				}
				return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil
			case "agent_manager_roundtrip":
				record, err := fakeHostedAgentManagerRoundTrip(plugininvokerservice.InvocationTokenFromContext(ctx), env)
				if err != nil {
					return nil, err
				}
				body, err := json.Marshal(record)
				if err != nil {
					return nil, err
				}
				return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil
			case "authorization_roundtrip":
				record, err := fakeHostedAuthorizationRoundTrip(env)
				if err != nil {
					return nil, err
				}
				body, err := json.Marshal(record)
				if err != nil {
					return nil, err
				}
				return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil
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
				return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil
			default:
				return nil, fmt.Errorf("unknown operation %q", operation)
			}
		},
	}))
	go func() {
		_ = srv.Serve(lis)
	}()

	r.mu.Lock()
	r.fakePlugins[req.SessionID] = &fakeHostedPluginServer{
		dir:      dir,
		listener: lis,
		server:   srv,
	}
	r.mu.Unlock()
	return &pluginruntime.HostedPlugin{
		ID:         "fake-" + req.SessionID,
		SessionID:  req.SessionID,
		PluginName: req.PluginName,
		DialTarget: "unix://" + socketPath,
	}, nil
}

func (r *capturingBundlePluginRuntime) cleanupFakeHostedPlugin(sessionID string) {
	r.mu.Lock()
	fake := r.fakePlugins[sessionID]
	delete(r.fakePlugins, sessionID)
	r.mu.Unlock()
	if fake == nil {
		return
	}
	fake.server.Stop()
	_ = fake.listener.Close()
	_ = os.RemoveAll(fake.dir)
}

func fakeHostedIndexedDBRoundTrip(store, id, value, binding string, env map[string]string) (map[string]any, error) {
	envName := indexeddbservice.DefaultSocketEnv
	tokenEnvName := indexeddbservice.SocketTokenEnv("")
	if strings.TrimSpace(binding) != "" {
		envName = indexeddbservice.SocketEnv(binding)
		tokenEnvName = indexeddbservice.SocketTokenEnv(binding)
	}
	target := strings.TrimSpace(env[envName])
	if target == "" {
		return nil, fmt.Errorf("missing indexeddb relay target in %s", envName)
	}
	token := strings.TrimSpace(env[tokenEnvName])
	if token == "" {
		return nil, fmt.Errorf("missing indexeddb relay token in %s", tokenEnvName)
	}
	address := strings.TrimSpace(strings.TrimPrefix(target, "tls://"))
	if address == "" || address == target {
		return nil, fmt.Errorf("unsupported indexeddb relay target %q", target)
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(runtimehost.HostServiceRelayTokenHeader, token))

	client := proto.NewIndexedDBClient(conn)
	if _, err := client.CreateObjectStore(ctx, &proto.CreateObjectStoreRequest{Name: store}); err != nil {
		return nil, fmt.Errorf("create object store: %w", err)
	}
	recordValue, err := gestalt.RecordToProto(gestalt.Record{"id": id, "value": value})
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
	record, err := gestalt.RecordFromProto(resp.GetRecord())
	if err != nil {
		return nil, fmt.Errorf("decode record: %w", err)
	}
	return record, nil
}

func fakeHostedCacheRoundTrip(key, value, binding string, env map[string]string) (map[string]any, error) {
	envName := cacheservice.DefaultSocketEnv
	tokenEnvName := cacheservice.SocketTokenEnv("")
	if strings.TrimSpace(binding) != "" {
		envName = cacheservice.SocketEnv(binding)
		tokenEnvName = cacheservice.SocketTokenEnv(binding)
	}
	target := strings.TrimSpace(env[envName])
	if target == "" {
		return nil, fmt.Errorf("missing cache relay target in %s", envName)
	}
	token := strings.TrimSpace(env[tokenEnvName])
	if token == "" {
		return nil, fmt.Errorf("missing cache relay token in %s", tokenEnvName)
	}
	address := strings.TrimSpace(strings.TrimPrefix(target, "tls://"))
	if address == "" || address == target {
		return nil, fmt.Errorf("unsupported cache relay target %q", target)
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(runtimehost.HostServiceRelayTokenHeader, token))

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

func fakeHostedS3RoundTrip(bucket, key, value, binding string, env map[string]string) (map[string]any, error) {
	envName := s3service.DefaultSocketEnv
	tokenEnvName := s3service.SocketTokenEnv("")
	if strings.TrimSpace(binding) != "" {
		envName = s3service.SocketEnv(binding)
		tokenEnvName = s3service.SocketTokenEnv(binding)
	}
	target := strings.TrimSpace(env[envName])
	if target == "" {
		return nil, fmt.Errorf("missing s3 relay target in %s", envName)
	}
	token := strings.TrimSpace(env[tokenEnvName])
	if token == "" {
		return nil, fmt.Errorf("missing s3 relay token in %s", tokenEnvName)
	}
	address := strings.TrimSpace(strings.TrimPrefix(target, "tls://"))
	if address == "" || address == target {
		return nil, fmt.Errorf("unsupported s3 relay target %q", target)
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(runtimehost.HostServiceRelayTokenHeader, token))

	client := proto.NewS3Client(conn)
	writeStream, err := client.WriteObject(ctx)
	if err != nil {
		return nil, fmt.Errorf("open s3 write stream: %w", err)
	}
	if err := writeStream.Send(&proto.WriteObjectRequest{
		Msg: &proto.WriteObjectRequest_Open{
			Open: &proto.WriteObjectOpen{
				Ref:         &proto.S3ObjectRef{Bucket: bucket, Key: key},
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
		Ref: &proto.S3ObjectRef{Bucket: bucket, Key: key},
	})
	if err != nil {
		return nil, fmt.Errorf("head s3 object: %w", err)
	}

	readStream, err := client.ReadObject(ctx, &proto.ReadObjectRequest{
		Ref: &proto.S3ObjectRef{Bucket: bucket, Key: key},
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
		Bucket: bucket,
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

func fakeHostedWorkflowManagerRoundTrip(invocationToken string, env map[string]string) (map[string]any, error) {
	target := strings.TrimSpace(env[workflowservice.DefaultManagerSocketEnv])
	if target == "" {
		return nil, fmt.Errorf("missing workflow manager relay target in %s", workflowservice.DefaultManagerSocketEnv)
	}
	token := strings.TrimSpace(env[workflowservice.ManagerSocketTokenEnv()])
	if token == "" {
		return nil, fmt.Errorf("missing workflow manager relay token in %s", workflowservice.ManagerSocketTokenEnv())
	}
	address := strings.TrimSpace(strings.TrimPrefix(target, "tls://"))
	if address == "" || address == target {
		return nil, fmt.Errorf("unsupported workflow manager relay target %q", target)
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(runtimehost.HostServiceRelayTokenHeader, token))

	client := proto.NewWorkflowManagerHostClient(conn)
	created, err := client.CreateSchedule(ctx, &proto.WorkflowManagerCreateScheduleRequest{
		InvocationToken: invocationToken,
		ProviderName:    "managed",
		Cron:            "*/5 * * * *",
		Timezone:        "UTC",
		IdempotencyKey:  "workflow-manager-roundtrip",
		Target: &proto.BoundWorkflowTarget{
			Kind: &proto.BoundWorkflowTarget_Plugin{
				Plugin: &proto.BoundWorkflowPluginTarget{
					PluginName: "roadmap",
					Operation:  "sync",
				},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create workflow schedule: %w", err)
	}
	scheduleID := strings.TrimSpace(created.GetSchedule().GetId())
	if scheduleID == "" {
		return nil, fmt.Errorf("workflow manager create did not return a schedule id")
	}
	fetched, err := client.GetSchedule(ctx, &proto.WorkflowManagerGetScheduleRequest{
		InvocationToken: invocationToken,
		ScheduleId:      scheduleID,
	})
	if err != nil {
		return nil, fmt.Errorf("get workflow schedule: %w", err)
	}

	return map[string]any{
		"provider_name": created.GetProviderName(),
		"schedule_id":   scheduleID,
		"cron":          fetched.GetSchedule().GetCron(),
		"operation":     fetched.GetSchedule().GetTarget().GetPlugin().GetOperation(),
	}, nil
}

func fakeHostedAuthorizationRoundTrip(env map[string]string) (map[string]any, error) {
	target := strings.TrimSpace(env[authorizationservice.DefaultSocketEnv])
	if target == "" {
		return nil, fmt.Errorf("missing authorization relay target in %s", authorizationservice.DefaultSocketEnv)
	}
	token := strings.TrimSpace(env[authorizationservice.SocketTokenEnv()])
	if token == "" {
		return nil, fmt.Errorf("missing authorization relay token in %s", authorizationservice.SocketTokenEnv())
	}
	address := strings.TrimSpace(strings.TrimPrefix(target, "tls://"))
	if address == "" || address == target {
		return nil, fmt.Errorf("unsupported authorization relay target %q", target)
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
		return nil, fmt.Errorf("connect authorization relay: %w", err)
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(runtimehost.HostServiceRelayTokenHeader, token))

	client := proto.NewAuthorizationProviderClient(conn)
	meta, err := client.GetMetadata(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, fmt.Errorf("get authorization metadata: %w", err)
	}
	resp, err := client.SearchSubjects(ctx, &proto.SubjectSearchRequest{
		SubjectType: "user",
		Resource: &proto.Resource{
			Type: "slack_identity",
			Id:   "team:T123:user:U456",
		},
		Action:   &proto.Action{Name: "assume"},
		PageSize: 1,
	})
	if err != nil {
		return nil, fmt.Errorf("search authorization subjects: %w", err)
	}
	if len(resp.GetSubjects()) == 0 {
		return nil, fmt.Errorf("authorization search did not return any subjects")
	}

	return map[string]any{
		"model_id":     resp.GetModelId(),
		"subject_id":   resp.GetSubjects()[0].GetId(),
		"subject_type": resp.GetSubjects()[0].GetType(),
		"capabilities": meta.GetCapabilities(),
	}, nil
}

func fakeHostedAgentManagerRoundTrip(invocationToken string, env map[string]string) (map[string]any, error) {
	target := strings.TrimSpace(env[agentservice.DefaultManagerSocketEnv])
	if target == "" {
		return nil, fmt.Errorf("missing agent manager relay target in %s", agentservice.DefaultManagerSocketEnv)
	}
	token := strings.TrimSpace(env[agentservice.ManagerSocketTokenEnv()])
	if token == "" {
		return nil, fmt.Errorf("missing agent manager relay token in %s", agentservice.ManagerSocketTokenEnv())
	}
	address := strings.TrimSpace(strings.TrimPrefix(target, "tls://"))
	if address == "" || address == target {
		return nil, fmt.Errorf("unsupported agent manager relay target %q", target)
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(runtimehost.HostServiceRelayTokenHeader, token))

	client := proto.NewAgentManagerHostClient(conn)
	session, err := client.CreateSession(ctx, &proto.AgentManagerCreateSessionRequest{
		InvocationToken: invocationToken,
		ProviderName:    "managed",
		Model:           "gpt-test",
		ClientRef:       "plugin-session",
		IdempotencyKey:  "plugin-agent-session",
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

	turn, err := client.CreateTurn(ctx, &proto.AgentManagerCreateTurnRequest{
		InvocationToken: invocationToken,
		SessionId:       sessionID,
		Model:           "gpt-test",
		IdempotencyKey:  "plugin-agent-turn",
		ToolSource:      proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_MCP_CATALOG,
		ToolRefs: []*proto.AgentToolRef{{
			Plugin:    "roadmap",
			Operation: "sync",
		}},
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

	interactions, err := client.ListInteractions(ctx, &proto.AgentManagerListInteractionsRequest{
		InvocationToken: invocationToken,
		TurnId:          turnID,
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
	resolved, err := client.ResolveInteraction(ctx, &proto.AgentManagerResolveInteractionRequest{
		InvocationToken: invocationToken,
		TurnId:          turnID,
		InteractionId:   interactionID,
		Resolution:      resolution,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve agent interaction: %w", err)
	}

	fetched, err := client.GetTurn(ctx, &proto.AgentManagerGetTurnRequest{
		InvocationToken: invocationToken,
		TurnId:          turnID,
	})
	if err != nil {
		return nil, fmt.Errorf("get agent turn: %w", err)
	}

	events, err := client.ListTurnEvents(ctx, &proto.AgentManagerListTurnEventsRequest{
		InvocationToken: invocationToken,
		TurnId:          turnID,
		AfterSeq:        0,
		Limit:           10,
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

func fakeHostedInvokePlugin(targetPlugin, targetOperation, invocationToken string, env map[string]string) (invokePluginEnvelope, error) {
	envelope := invokePluginEnvelope{
		OK:              false,
		TargetPlugin:    targetPlugin,
		TargetOperation: targetOperation,
	}
	target := strings.TrimSpace(env[plugininvokerservice.DefaultSocketEnv])
	if target == "" {
		return envelope, fmt.Errorf("missing plugin invoker relay target in %s", plugininvokerservice.DefaultSocketEnv)
	}
	token := strings.TrimSpace(env[plugininvokerservice.SocketTokenEnv()])
	if token == "" {
		return envelope, fmt.Errorf("missing plugin invoker relay token in %s", plugininvokerservice.SocketTokenEnv())
	}
	address := strings.TrimSpace(strings.TrimPrefix(target, "tls://"))
	if address == "" || address == target {
		return envelope, fmt.Errorf("unsupported plugin invoker relay target %q", target)
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
		return envelope, fmt.Errorf("connect plugin invoker relay: %w", err)
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(runtimehost.HostServiceRelayTokenHeader, token))

	resp, err := proto.NewPluginInvokerClient(conn).Invoke(ctx, &proto.PluginInvokeRequest{
		InvocationToken: invocationToken,
		Plugin:          targetPlugin,
		Operation:       targetOperation,
	})
	if err != nil {
		return envelope, err
	}

	envelope.OK = true
	envelope.Status = int(resp.GetStatus())
	if err := json.Unmarshal([]byte(resp.GetBody()), &envelope.Body); err != nil {
		return envelope, fmt.Errorf("decode nested invoke body: %w", err)
	}
	return envelope, nil
}

func (r *capturingBundlePluginRuntime) startPluginRequestsCopy() []pluginruntime.StartPluginRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]pluginruntime.StartPluginRequest, len(r.startPluginRequests))
	for i, req := range r.startPluginRequests {
		out[i] = pluginruntime.StartPluginRequest{
			SessionID:  req.SessionID,
			PluginName: req.PluginName,
			Command:    req.Command,
			Args:       slices.Clone(req.Args),
			Env:        cloneRuntimeMetadata(req.Env),
			Egress:     cloneRuntimeEgressPolicy(req.Egress),
			HostBinary: req.HostBinary,
		}
	}
	return out
}

func (r *capturingBundlePluginRuntime) setSessionLifecycle(sessionID string, lifecycle *pluginruntime.SessionLifecycle) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sessionLifecycles == nil {
		r.sessionLifecycles = make(map[string]*pluginruntime.SessionLifecycle)
	}
	r.sessionLifecycles[sessionID] = clonePluginRuntimeSessionLifecycle(lifecycle)
}

func (r *capturingBundlePluginRuntime) attachSessionLifecycle(session *pluginruntime.Session) {
	if session == nil || session.ID == "" {
		return
	}
	r.mu.Lock()
	lifecycle := clonePluginRuntimeSessionLifecycle(r.sessionLifecycles[session.ID])
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

func cloneRuntimeEgressPolicy(policy pluginruntime.RuntimeEgressPolicy) pluginruntime.RuntimeEgressPolicy {
	return pluginruntime.RuntimeEgressPolicy{
		AllowedHosts:  slices.Clone(policy.AllowedHosts),
		DefaultAction: policy.DefaultAction,
	}
}

func hostedExecutionConfig(runtimeCfg *config.HostedRuntimeConfig) *config.ExecutionConfig {
	return &config.ExecutionConfig{
		Mode:    config.ExecutionModeHosted,
		Runtime: runtimeCfg,
	}
}

func assertStartPluginEgressPolicy(t *testing.T, req pluginruntime.StartPluginRequest, allowedHosts []string, action pluginruntime.PolicyAction) {
	t.Helper()
	if got := req.Egress.AllowedHosts; !slices.Equal(got, allowedHosts) {
		t.Fatalf("StartPlugin egress allowed hosts = %#v, want %#v", got, allowedHosts)
	}
	if got := req.Egress.DefaultAction; got != action {
		t.Fatalf("StartPlugin egress default action = %q, want %q", got, action)
	}
}

func assertStartPluginRelayEnv(t *testing.T, req pluginruntime.StartPluginRequest, wantEnvVar string) {
	t.Helper()
	if got := req.Env[wantEnvVar]; !strings.HasPrefix(got, "tls://") {
		t.Fatalf("StartPlugin env %s = %q, want tls:// public relay target", wantEnvVar, got)
	}
	if got := req.Env[wantEnvVar+"_TOKEN"]; strings.TrimSpace(got) == "" {
		t.Fatalf("StartPlugin env missing non-empty %s_TOKEN", wantEnvVar)
	}
}

type slowStopPluginRuntime struct {
	inner     pluginruntime.Provider
	stopCount atomic.Int32
}

func (r *slowStopPluginRuntime) Support(ctx context.Context) (pluginruntime.Support, error) {
	return r.inner.Support(ctx)
}

func (r *slowStopPluginRuntime) StartSession(ctx context.Context, req pluginruntime.StartSessionRequest) (*pluginruntime.Session, error) {
	return r.inner.StartSession(ctx, req)
}

func (r *slowStopPluginRuntime) ListSessions(ctx context.Context) ([]pluginruntime.Session, error) {
	return r.inner.ListSessions(ctx)
}

func (r *slowStopPluginRuntime) GetSession(ctx context.Context, req pluginruntime.GetSessionRequest) (*pluginruntime.Session, error) {
	return r.inner.GetSession(ctx, req)
}

func (r *slowStopPluginRuntime) StopSession(ctx context.Context, req pluginruntime.StopSessionRequest) error {
	r.stopCount.Add(1)
	<-ctx.Done()
	return ctx.Err()
}

func (r *slowStopPluginRuntime) StartPlugin(ctx context.Context, req pluginruntime.StartPluginRequest) (*pluginruntime.HostedPlugin, error) {
	return r.inner.StartPlugin(ctx, req)
}

func (r *slowStopPluginRuntime) Close() error {
	return r.inner.Close()
}

type failingStartPluginSlowStopPluginRuntime struct {
	slowStopPluginRuntime
	err error
}

func (r *failingStartPluginSlowStopPluginRuntime) StartPlugin(context.Context, pluginruntime.StartPluginRequest) (*pluginruntime.HostedPlugin, error) {
	return nil, r.err
}

type staticCapabilityPluginRuntime struct {
	inner   pluginruntime.Provider
	support pluginruntime.Support
}

func (r *staticCapabilityPluginRuntime) Support(context.Context) (pluginruntime.Support, error) {
	return r.support, nil
}

func (r *staticCapabilityPluginRuntime) StartSession(ctx context.Context, req pluginruntime.StartSessionRequest) (*pluginruntime.Session, error) {
	return r.inner.StartSession(ctx, req)
}

func (r *staticCapabilityPluginRuntime) ListSessions(ctx context.Context) ([]pluginruntime.Session, error) {
	return r.inner.ListSessions(ctx)
}

func (r *staticCapabilityPluginRuntime) GetSession(ctx context.Context, req pluginruntime.GetSessionRequest) (*pluginruntime.Session, error) {
	return r.inner.GetSession(ctx, req)
}

func (r *staticCapabilityPluginRuntime) StopSession(ctx context.Context, req pluginruntime.StopSessionRequest) error {
	return r.inner.StopSession(ctx, req)
}

func (r *staticCapabilityPluginRuntime) StartPlugin(ctx context.Context, req pluginruntime.StartPluginRequest) (*pluginruntime.HostedPlugin, error) {
	return r.inner.StartPlugin(ctx, req)
}

func (r *staticCapabilityPluginRuntime) Close() error {
	return r.inner.Close()
}

type stubWorkflowManager struct {
	mu              sync.Mutex
	subjects        []string
	nextScheduleID  int
	nextTriggerID   int
	schedules       map[string]*workflowmanager.ManagedSchedule
	triggers        map[string]*workflowmanager.ManagedEventTrigger
	publishedEvents []coreworkflow.Event
	scheduleKeys    []string
	triggerKeys     []string
}

func newStubWorkflowManager() *stubWorkflowManager {
	return &stubWorkflowManager{
		schedules: make(map[string]*workflowmanager.ManagedSchedule),
		triggers:  make(map[string]*workflowmanager.ManagedEventTrigger),
	}
}

func (m *stubWorkflowManager) ListSchedules(context.Context, *principal.Principal) ([]*workflowmanager.ManagedSchedule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*workflowmanager.ManagedSchedule, 0, len(m.schedules))
	for _, item := range m.schedules {
		out = append(out, cloneManagedSchedule(item))
	}
	return out, nil
}

func (m *stubWorkflowManager) CreateSchedule(_ context.Context, p *principal.Principal, req workflowmanager.ScheduleUpsert) (*workflowmanager.ManagedSchedule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextScheduleID++
	id := fmt.Sprintf("sched-%d", m.nextScheduleID)
	now := time.Now().UTC().Truncate(time.Second)
	m.scheduleKeys = append(m.scheduleKeys, strings.TrimSpace(req.IdempotencyKey))
	value := &workflowmanager.ManagedSchedule{
		ProviderName: defaultWorkflowProviderName(req.ProviderName),
		Schedule: &coreworkflow.Schedule{
			ID:        id,
			Cron:      req.Cron,
			Timezone:  req.Timezone,
			Target:    cloneWorkflowTarget(req.Target),
			Paused:    req.Paused,
			CreatedAt: &now,
			UpdatedAt: &now,
		},
	}
	m.schedules[id] = value
	m.subjects = append(m.subjects, subjectIDOf(p))
	return cloneManagedSchedule(value), nil
}

func (m *stubWorkflowManager) GetSchedule(_ context.Context, p *principal.Principal, scheduleID string) (*workflowmanager.ManagedSchedule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subjects = append(m.subjects, subjectIDOf(p))
	value, ok := m.schedules[scheduleID]
	if !ok {
		return nil, core.ErrNotFound
	}
	return cloneManagedSchedule(value), nil
}

func (m *stubWorkflowManager) UpdateSchedule(_ context.Context, p *principal.Principal, scheduleID string, req workflowmanager.ScheduleUpsert) (*workflowmanager.ManagedSchedule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subjects = append(m.subjects, subjectIDOf(p))
	value, ok := m.schedules[scheduleID]
	if !ok {
		return nil, core.ErrNotFound
	}
	now := time.Now().UTC().Truncate(time.Second)
	value.ProviderName = defaultWorkflowProviderName(req.ProviderName)
	value.Schedule.Cron = req.Cron
	value.Schedule.Timezone = req.Timezone
	value.Schedule.Target = cloneWorkflowTarget(req.Target)
	value.Schedule.Paused = req.Paused
	value.Schedule.UpdatedAt = &now
	return cloneManagedSchedule(value), nil
}

func (m *stubWorkflowManager) DeleteSchedule(_ context.Context, p *principal.Principal, scheduleID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subjects = append(m.subjects, subjectIDOf(p))
	if _, ok := m.schedules[scheduleID]; !ok {
		return core.ErrNotFound
	}
	delete(m.schedules, scheduleID)
	return nil
}

func (m *stubWorkflowManager) PauseSchedule(_ context.Context, p *principal.Principal, scheduleID string) (*workflowmanager.ManagedSchedule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subjects = append(m.subjects, subjectIDOf(p))
	value, ok := m.schedules[scheduleID]
	if !ok {
		return nil, core.ErrNotFound
	}
	now := time.Now().UTC().Truncate(time.Second)
	value.Schedule.Paused = true
	value.Schedule.UpdatedAt = &now
	return cloneManagedSchedule(value), nil
}

func (m *stubWorkflowManager) ResumeSchedule(_ context.Context, p *principal.Principal, scheduleID string) (*workflowmanager.ManagedSchedule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subjects = append(m.subjects, subjectIDOf(p))
	value, ok := m.schedules[scheduleID]
	if !ok {
		return nil, core.ErrNotFound
	}
	now := time.Now().UTC().Truncate(time.Second)
	value.Schedule.Paused = false
	value.Schedule.UpdatedAt = &now
	return cloneManagedSchedule(value), nil
}

func (m *stubWorkflowManager) ListEventTriggers(context.Context, *principal.Principal) ([]*workflowmanager.ManagedEventTrigger, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*workflowmanager.ManagedEventTrigger, 0, len(m.triggers))
	for _, item := range m.triggers {
		out = append(out, cloneManagedEventTrigger(item))
	}
	return out, nil
}

func (m *stubWorkflowManager) CreateEventTrigger(_ context.Context, p *principal.Principal, req workflowmanager.EventTriggerUpsert) (*workflowmanager.ManagedEventTrigger, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextTriggerID++
	id := fmt.Sprintf("trg-%d", m.nextTriggerID)
	now := time.Now().UTC().Truncate(time.Second)
	m.triggerKeys = append(m.triggerKeys, strings.TrimSpace(req.IdempotencyKey))
	value := &workflowmanager.ManagedEventTrigger{
		ProviderName: defaultWorkflowProviderName(req.ProviderName),
		Trigger: &coreworkflow.EventTrigger{
			ID:        id,
			Match:     cloneWorkflowEventMatch(req.Match),
			Target:    cloneWorkflowTarget(req.Target),
			Paused:    req.Paused,
			CreatedAt: &now,
			UpdatedAt: &now,
		},
	}
	m.triggers[id] = value
	m.subjects = append(m.subjects, subjectIDOf(p))
	return cloneManagedEventTrigger(value), nil
}

func (m *stubWorkflowManager) GetEventTrigger(_ context.Context, p *principal.Principal, triggerID string) (*workflowmanager.ManagedEventTrigger, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subjects = append(m.subjects, subjectIDOf(p))
	value, ok := m.triggers[triggerID]
	if !ok {
		return nil, core.ErrNotFound
	}
	return cloneManagedEventTrigger(value), nil
}

func (m *stubWorkflowManager) UpdateEventTrigger(_ context.Context, p *principal.Principal, triggerID string, req workflowmanager.EventTriggerUpsert) (*workflowmanager.ManagedEventTrigger, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subjects = append(m.subjects, subjectIDOf(p))
	value, ok := m.triggers[triggerID]
	if !ok {
		return nil, core.ErrNotFound
	}
	now := time.Now().UTC().Truncate(time.Second)
	value.ProviderName = defaultWorkflowProviderName(req.ProviderName)
	value.Trigger.Match = cloneWorkflowEventMatch(req.Match)
	value.Trigger.Target = cloneWorkflowTarget(req.Target)
	value.Trigger.Paused = req.Paused
	value.Trigger.UpdatedAt = &now
	return cloneManagedEventTrigger(value), nil
}

func (m *stubWorkflowManager) DeleteEventTrigger(_ context.Context, p *principal.Principal, triggerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subjects = append(m.subjects, subjectIDOf(p))
	if _, ok := m.triggers[triggerID]; !ok {
		return core.ErrNotFound
	}
	delete(m.triggers, triggerID)
	return nil
}

func (m *stubWorkflowManager) PauseEventTrigger(_ context.Context, p *principal.Principal, triggerID string) (*workflowmanager.ManagedEventTrigger, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subjects = append(m.subjects, subjectIDOf(p))
	value, ok := m.triggers[triggerID]
	if !ok {
		return nil, core.ErrNotFound
	}
	now := time.Now().UTC().Truncate(time.Second)
	value.Trigger.Paused = true
	value.Trigger.UpdatedAt = &now
	return cloneManagedEventTrigger(value), nil
}

func (m *stubWorkflowManager) ResumeEventTrigger(_ context.Context, p *principal.Principal, triggerID string) (*workflowmanager.ManagedEventTrigger, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subjects = append(m.subjects, subjectIDOf(p))
	value, ok := m.triggers[triggerID]
	if !ok {
		return nil, core.ErrNotFound
	}
	now := time.Now().UTC().Truncate(time.Second)
	value.Trigger.Paused = false
	value.Trigger.UpdatedAt = &now
	return cloneManagedEventTrigger(value), nil
}

func (m *stubWorkflowManager) ListRuns(context.Context, *principal.Principal) ([]*workflowmanager.ManagedRun, error) {
	return nil, nil
}

func (m *stubWorkflowManager) StartRun(context.Context, *principal.Principal, workflowmanager.RunStart) (*workflowmanager.ManagedRun, error) {
	return nil, core.ErrNotFound
}

func (m *stubWorkflowManager) GetRun(context.Context, *principal.Principal, string) (*workflowmanager.ManagedRun, error) {
	return nil, core.ErrNotFound
}

func (m *stubWorkflowManager) CancelRun(context.Context, *principal.Principal, string, string) (*workflowmanager.ManagedRun, error) {
	return nil, core.ErrNotFound
}

func (m *stubWorkflowManager) SignalRun(context.Context, *principal.Principal, workflowmanager.RunSignal) (*workflowmanager.ManagedRunSignal, error) {
	return nil, core.ErrNotFound
}

func (m *stubWorkflowManager) SignalOrStartRun(context.Context, *principal.Principal, workflowmanager.RunSignalOrStart) (*workflowmanager.ManagedRunSignal, error) {
	return nil, core.ErrNotFound
}

func (m *stubWorkflowManager) PublishEvent(_ context.Context, p *principal.Principal, event coreworkflow.Event) (coreworkflow.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subjects = append(m.subjects, subjectIDOf(p))
	if strings.TrimSpace(event.ID) == "" {
		event.ID = fmt.Sprintf("evt-%d", len(m.publishedEvents)+1)
	}
	m.publishedEvents = append(m.publishedEvents, cloneWorkflowEvent(event))
	return cloneWorkflowEvent(event), nil
}

func (m *stubWorkflowManager) Subjects() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.subjects...)
}

func (m *stubWorkflowManager) ScheduleIdempotencyKeys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.scheduleKeys)
}

type stubAgentTurnManagerProvider struct {
	coreagent.UnimplementedProvider
	mu                    sync.Mutex
	createSessionRequests []coreagent.CreateSessionRequest
	createTurnRequests    []coreagent.CreateTurnRequest
	sessions              map[string]*coreagent.Session
	turns                 map[string]*coreagent.Turn
	turnEvents            map[string][]*coreagent.TurnEvent
	interactions          map[string]*coreagent.Interaction
}

func newStubAgentTurnManagerProvider() *stubAgentTurnManagerProvider {
	return &stubAgentTurnManagerProvider{
		sessions:     map[string]*coreagent.Session{},
		turns:        map[string]*coreagent.Turn{},
		turnEvents:   map[string][]*coreagent.TurnEvent{},
		interactions: map[string]*coreagent.Interaction{},
	}
}

func (p *stubAgentTurnManagerProvider) CreateSession(_ context.Context, req coreagent.CreateSessionRequest) (*coreagent.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now().UTC().Truncate(time.Second)
	p.createSessionRequests = append(p.createSessionRequests, req)
	session := &coreagent.Session{
		ID:           req.SessionID,
		ProviderName: "managed",
		Model:        req.Model,
		ClientRef:    req.ClientRef,
		State:        coreagent.SessionStateActive,
		Metadata:     maps.Clone(req.Metadata),
		CreatedBy:    req.CreatedBy,
		CreatedAt:    &now,
		UpdatedAt:    &now,
	}
	p.sessions[session.ID] = session
	return cloneAgentSession(session), nil
}

func (p *stubAgentTurnManagerProvider) GetSession(_ context.Context, req coreagent.GetSessionRequest) (*coreagent.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	session, ok := p.sessions[req.SessionID]
	if !ok {
		return nil, core.ErrNotFound
	}
	return cloneAgentSession(session), nil
}

func (p *stubAgentTurnManagerProvider) ListSessions(context.Context, coreagent.ListSessionsRequest) ([]*coreagent.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*coreagent.Session, 0, len(p.sessions))
	for _, session := range p.sessions {
		out = append(out, cloneAgentSession(session))
	}
	return out, nil
}

func (p *stubAgentTurnManagerProvider) UpdateSession(_ context.Context, req coreagent.UpdateSessionRequest) (*coreagent.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	session, ok := p.sessions[req.SessionID]
	if !ok {
		return nil, core.ErrNotFound
	}
	now := time.Now().UTC().Truncate(time.Second)
	if req.ClientRef != "" {
		session.ClientRef = req.ClientRef
	}
	if req.State != "" {
		session.State = req.State
	}
	if req.Metadata != nil {
		session.Metadata = maps.Clone(req.Metadata)
	}
	session.UpdatedAt = &now
	return cloneAgentSession(session), nil
}

func (p *stubAgentTurnManagerProvider) CreateTurn(_ context.Context, req coreagent.CreateTurnRequest) (*coreagent.Turn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now().UTC().Truncate(time.Second)
	p.createTurnRequests = append(p.createTurnRequests, req)

	turn := &coreagent.Turn{
		ID:           req.TurnID,
		SessionID:    req.SessionID,
		ProviderName: "managed",
		Model:        req.Model,
		Status:       coreagent.ExecutionStatusSucceeded,
		Messages:     append([]coreagent.Message(nil), req.Messages...),
		OutputText:   "turn completed",
		CreatedBy:    req.CreatedBy,
		CreatedAt:    &now,
		StartedAt:    &now,
		CompletedAt:  &now,
		ExecutionRef: req.ExecutionRef,
	}
	p.turns[turn.ID] = turn
	p.appendTurnEventLocked(turn.ID, "turn.started", map[string]any{"session_id": req.SessionID})

	if requireInteraction, _ := req.Metadata["requireInteraction"].(bool); requireInteraction {
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

	if session := p.sessions[req.SessionID]; session != nil {
		session.LastTurnAt = &now
		session.UpdatedAt = &now
	}
	return cloneAgentTurn(turn), nil
}

func (p *stubAgentTurnManagerProvider) GetTurn(_ context.Context, req coreagent.GetTurnRequest) (*coreagent.Turn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	turn, ok := p.turns[req.TurnID]
	if !ok {
		return nil, core.ErrNotFound
	}
	return cloneAgentTurn(turn), nil
}

func (p *stubAgentTurnManagerProvider) ListTurns(_ context.Context, req coreagent.ListTurnsRequest) ([]*coreagent.Turn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*coreagent.Turn, 0, len(p.turns))
	for _, turn := range p.turns {
		if req.SessionID == "" || turn.SessionID == req.SessionID {
			out = append(out, cloneAgentTurn(turn))
		}
	}
	return out, nil
}

func (p *stubAgentTurnManagerProvider) CancelTurn(_ context.Context, req coreagent.CancelTurnRequest) (*coreagent.Turn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	turn, ok := p.turns[req.TurnID]
	if !ok {
		return nil, core.ErrNotFound
	}
	now := time.Now().UTC().Truncate(time.Second)
	turn.Status = coreagent.ExecutionStatusCanceled
	turn.StatusMessage = req.Reason
	turn.CompletedAt = &now
	p.appendTurnEventLocked(turn.ID, "turn.canceled", map[string]any{"reason": req.Reason})
	return cloneAgentTurn(turn), nil
}

func (p *stubAgentTurnManagerProvider) ListTurnEvents(_ context.Context, req coreagent.ListTurnEventsRequest) ([]*coreagent.TurnEvent, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	events := p.turnEvents[req.TurnID]
	out := make([]*coreagent.TurnEvent, 0, len(events))
	for _, event := range events {
		if event.Seq <= req.AfterSeq {
			continue
		}
		out = append(out, cloneAgentTurnEvent(event))
		if req.Limit > 0 && len(out) >= req.Limit {
			break
		}
	}
	return out, nil
}

func (p *stubAgentTurnManagerProvider) GetInteraction(_ context.Context, req coreagent.GetInteractionRequest) (*coreagent.Interaction, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	interaction, ok := p.interactions[req.InteractionID]
	if !ok {
		return nil, core.ErrNotFound
	}
	return cloneAgentInteraction(interaction), nil
}

func (p *stubAgentTurnManagerProvider) ListInteractions(_ context.Context, req coreagent.ListInteractionsRequest) ([]*coreagent.Interaction, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*coreagent.Interaction, 0, len(p.interactions))
	for _, interaction := range p.interactions {
		if req.TurnID == "" || interaction.TurnID == req.TurnID {
			out = append(out, cloneAgentInteraction(interaction))
		}
	}
	return out, nil
}

func (p *stubAgentTurnManagerProvider) ResolveInteraction(_ context.Context, req coreagent.ResolveInteractionRequest) (*coreagent.Interaction, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	interaction, ok := p.interactions[req.InteractionID]
	if !ok {
		return nil, core.ErrNotFound
	}
	now := time.Now().UTC().Truncate(time.Second)
	interaction.State = coreagent.InteractionStateResolved
	interaction.Resolution = maps.Clone(req.Resolution)
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

func (p *stubAgentTurnManagerProvider) GetCapabilities(context.Context, coreagent.GetCapabilitiesRequest) (*coreagent.ProviderCapabilities, error) {
	return &coreagent.ProviderCapabilities{
		StreamingText:        true,
		ToolCalls:            true,
		Interactions:         true,
		ResumableTurns:       true,
		StructuredOutput:     true,
		BoundedListHydration: true,
		SupportedToolSources: []coreagent.ToolSourceMode{coreagent.ToolSourceModeMCPCatalog},
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
	dst.StructuredOutput = maps.Clone(src.StructuredOutput)
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

func cloneManagedSchedule(value *workflowmanager.ManagedSchedule) *workflowmanager.ManagedSchedule {
	if value == nil {
		return nil
	}
	out := *value
	if value.Schedule != nil {
		schedule := *value.Schedule
		schedule.Target = cloneWorkflowTarget(value.Schedule.Target)
		out.Schedule = &schedule
	}
	if value.ExecutionRef != nil {
		executionRef := *value.ExecutionRef
		executionRef.Target = cloneWorkflowTarget(value.ExecutionRef.Target)
		out.ExecutionRef = &executionRef
	}
	return &out
}

func cloneManagedEventTrigger(value *workflowmanager.ManagedEventTrigger) *workflowmanager.ManagedEventTrigger {
	if value == nil {
		return nil
	}
	out := *value
	if value.Trigger != nil {
		trigger := *value.Trigger
		trigger.Match = cloneWorkflowEventMatch(value.Trigger.Match)
		trigger.Target = cloneWorkflowTarget(value.Trigger.Target)
		out.Trigger = &trigger
	}
	if value.ExecutionRef != nil {
		executionRef := *value.ExecutionRef
		executionRef.Target = cloneWorkflowTarget(value.ExecutionRef.Target)
		out.ExecutionRef = &executionRef
	}
	return &out
}

func cloneWorkflowTarget(value coreworkflow.Target) coreworkflow.Target {
	out := coreworkflow.Target{}
	if value.Plugin != nil {
		plugin := *value.Plugin
		plugin.Input = maps.Clone(plugin.Input)
		out.Plugin = &plugin
	}
	if value.Agent != nil {
		agent := *value.Agent
		agent.Messages = slices.Clone(agent.Messages)
		agent.ToolRefs = slices.Clone(agent.ToolRefs)
		agent.ResponseSchema = maps.Clone(agent.ResponseSchema)
		agent.ProviderOptions = maps.Clone(agent.ProviderOptions)
		agent.Metadata = maps.Clone(agent.Metadata)
		out.Agent = &agent
	}
	return out
}

func cloneWorkflowEventMatch(value coreworkflow.EventMatch) coreworkflow.EventMatch {
	return coreworkflow.EventMatch{
		Type:    value.Type,
		Source:  value.Source,
		Subject: value.Subject,
	}
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
		Plugins: map[string]*config.ProviderEntry{
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
	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{})
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
	if cat.Operations[0].Transport != catalog.TransportPlugin {
		t.Fatalf("unexpected catalog transport: %+v", cat.Operations[0])
	}

	result, err := prov.Execute(context.Background(), "greet", map[string]any{"name": "Gestalt"}, "")
	if err != nil {
		t.Fatalf("Execute(greet): %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("greet status = %d", result.Status)
	}
	if result.Body != `{"message":"Hello from config, Gestalt!"}` {
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
	if err := json.Unmarshal([]byte(result.Body), &got); err != nil {
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
		Kind:        providermanifestv1.KindPlugin,
		Source:      "github.com/testowner/plugins/python-source",
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
	}
	manifestData, err := providerpkg.EncodeSourceManifestFormat(manifest, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat: %v", err)
	}
	manifestPath := filepath.Join(root, "manifest.yaml")
	if err := os.WriteFile(manifestPath, manifestData, 0o644); err != nil {
		t.Fatalf("WriteFile(manifest.yaml): %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte(`[tool.gestalt]
provider = "provider"
`), 0o644); err != nil {
		t.Fatalf("WriteFile(pyproject.toml): %v", err)
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
		Plugins: map[string]*config.ProviderEntry{
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
	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{})
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
	if result.Body != `{"message":"Hi, Ada!"}` {
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
	if err := os.WriteFile(manifestPath, []byte("kind: plugin\n"), 0o644); err != nil {
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
		Plugins: map[string]*config.ProviderEntry{
			"example": {
				ResolvedManifestPath: manifestPath,
				ResolvedManifest: &providermanifestv1.Manifest{
					Kind:        providermanifestv1.KindPlugin,
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
	if got := result.Body; got != `{"source":"config"}` {
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
		Plugins: map[string]*config.ProviderEntry{
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
	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{})
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
		Plugins: map[string]*config.ProviderEntry{
			"hybrid": entry,
		},
	}

	factories := NewFactoryRegistry()
	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{})
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
		Plugins: map[string]*config.ProviderEntry{
			"hybrid": entry,
		},
	}

	factories := NewFactoryRegistry()
	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{})
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

	services := coretesting.NewStubServices(t)
	subjectID := principal.UserSubjectID("u-hybrid")
	if err := services.ExternalCredentials.PutCredential(context.Background(), &core.ExternalCredential{
		SubjectID:   subjectID,
		Integration: "hybrid",
		Connection:  "default",
		Instance:    "default",
		AccessToken: "tok-default",
	}); err != nil {
		t.Fatalf("PutCredential(default): %v", err)
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
	if result.Body != `{"message":"hello"}` {
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
	if err := os.WriteFile(manifestPath, []byte("kind: plugin\n"), 0o644); err != nil {
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
		Plugins: map[string]*config.ProviderEntry{
			"notion": {
				ResolvedManifestPath: manifestPath,
				ResolvedManifest: &providermanifestv1.Manifest{
					Kind:        providermanifestv1.KindPlugin,
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
	if apiResult.Body != `{"source":"api"}` {
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
		Plugins: map[string]*config.ProviderEntry{
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
	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{})
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
		t.Fatal("shared echo plugin binary not initialized")
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
		Kind:        providermanifestv1.KindPlugin,
		Source:      "github.com/acme/plugins/test",
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
	callerInvokes := []config.PluginInvocationDependency{
		{Plugin: "example", Operation: "request_context"},
	}
	exampleInvokes := []config.PluginInvocationDependency{
		{Plugin: "example", Operation: "request_context"},
	}
	cfg := &config.Config{
		Plugins: map[string]*config.ProviderEntry{
			"caller": {
				Command:              callerBin,
				Args:                 []string{"provider"},
				ResolvedManifest:     callerManifest,
				ResolvedManifestPath: filepath.Join(callerRoot, "manifest.yaml"),
				Invokes:              callerInvokes,
			},
			"example": {
				Command:              exampleBin,
				ResolvedManifest:     exampleManifest,
				ResolvedManifestPath: filepath.Join(exampleRoot, "manifest.yaml"),
				Invokes:              exampleInvokes,
				Config: mustNode(t, map[string]any{
					"greeting": "Hello from nested invoke",
				}),
			},
		},
	}

	providers, _, err := buildProvidersStrict(context.Background(), cfg, NewFactoryRegistry(), testRuntimePublicEndpointDeps(t, Deps{
		EncryptionKey: secret,
		PluginInvoker: bridge,
	}))
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })

	services, err := coredata.New(&coretesting.StubIndexedDB{})
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	coretesting.AttachStubExternalCredentials(services)
	t.Cleanup(func() { _ = services.Close() })

	broker := invocation.NewBroker(providers, services.Users, services.ExternalCredentials, brokerOpts...)
	bridge.SetTarget(invocation.NewGuarded(broker, nil, "plugin", nil, invocation.WithoutRateLimit()))

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

func newGraphQLSurfaceInvokeHarness(t *testing.T, graphQLURL string, allowSurface bool, authCfg config.AuthorizationConfig, brokerOpts ...invocation.BrokerOption) *nestedInvokeHarness {
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
	if err := os.WriteFile(linearManifestPath, []byte("kind: plugin\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(manifest.yaml): %v", err)
	}
	linearManifest := &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindPlugin,
		Source:      "github.com/acme/plugins/linear",
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

	callerInvokes := []config.PluginInvocationDependency{
		{Plugin: "linear", Operation: "viewer"},
	}
	if allowSurface {
		callerInvokes = append(callerInvokes, config.PluginInvocationDependency{
			Plugin:  "linear",
			Surface: "graphql",
		})
	}

	bridge := newLazyInvoker()
	cfg := &config.Config{
		Authorization: authCfg,
		Plugins: map[string]*config.ProviderEntry{
			"caller": {
				Command:              callerBin,
				Args:                 []string{"provider"},
				ResolvedManifest:     callerManifest,
				ResolvedManifestPath: filepath.Join(callerRoot, "manifest.yaml"),
				Invokes:              callerInvokes,
			},
			"linear": {
				ResolvedManifest:     linearManifest,
				ResolvedManifestPath: linearManifestPath,
			},
		},
	}

	secret := []byte("0123456789abcdef0123456789abcdef")
	providers, _, err := buildProvidersStrict(context.Background(), cfg, NewFactoryRegistry(), testRuntimePublicEndpointDeps(t, Deps{
		EncryptionKey: secret,
		PluginInvoker: bridge,
	}))
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })

	services, err := coredata.New(&coretesting.StubIndexedDB{})
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	coretesting.AttachStubExternalCredentials(services)
	t.Cleanup(func() { _ = services.Close() })

	if len(authCfg.Policies) > 0 {
		authz, err := authorizationservice.New(config.AuthorizationStaticConfig(authCfg, cfg.Plugins))
		if err != nil {
			t.Fatalf("authorization.New: %v", err)
		}
		brokerOpts = append(brokerOpts, invocation.WithAuthorizer(authz))
	}
	broker := invocation.NewBroker(providers, services.Users, services.ExternalCredentials, brokerOpts...)
	bridge.SetTarget(invocation.NewGuarded(broker, nil, "plugin", nil, invocation.WithoutRateLimit()))

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

	if err := harness.services.ExternalCredentials.PutCredential(ctx, &core.ExternalCredential{
		SubjectID:    subjectID,
		Integration:  plugin,
		Connection:   connection,
		Instance:     instance,
		AccessToken:  plugin + "-" + connection + "-token",
		RefreshToken: "refresh-token",
	}); err != nil {
		t.Fatalf("PutCredential(%s,%s,%s): %v", plugin, connection, instance, err)
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
	manifest.Spec.DefaultConnection = config.PluginConnectionAlias
	manifest.Spec.Connections = map[string]*providermanifestv1.ManifestConnectionDef{
		"api": {
			Mode: providermanifestv1.ConnectionModeUser,
			Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeBearer},
		},
		"openapi": {
			Mode: providermanifestv1.ConnectionModeUser,
			Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeBearer},
		},
		"mcp": {
			Mode: providermanifestv1.ConnectionModeUser,
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
	if got := operationRouting.connections["echo"]; got != config.PluginConnectionName {
		t.Fatalf("echo connection = %q, want %q", got, config.PluginConnectionName)
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
				"default": {Mode: providermanifestv1.ConnectionModeUser},
				"bot":     {Mode: providermanifestv1.ConnectionModeUser},
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
		ArtifactPath: "bin/gestalt-plugin-echo",
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
		Plugins: map[string]*config.ProviderEntry{
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
		Plugins: map[string]*config.ProviderEntry{
			"echonoauth": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
			},
		},
	}

	factories := NewFactoryRegistry()
	providers, connAuth, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{})
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	if _, ok := connAuth["echonoauth"]; ok {
		t.Fatal("expected no connection auth for plugin without oauth2 auth")
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
		Plugins: map[string]*config.ProviderEntry{
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
	if prov.ConnectionMode() != core.ConnectionModeUser {
		t.Fatalf("ConnectionMode = %q, want %q", prov.ConnectionMode(), core.ConnectionModeUser)
	}
}

func TestPreparedProviderStub_RejectsMixedConnectionModes(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Plugins: map[string]*config.ProviderEntry{
			"echoauth": {
				Source: config.NewMetadataSource("https://example.invalid/github-com-acme-plugins-test/v1.0.0/provider-release.yaml"),
				ResolvedManifest: &providermanifestv1.Manifest{
					DisplayName: "Echo Auth",
					Spec: &providermanifestv1.Spec{
						Connections: map[string]*providermanifestv1.ManifestConnectionDef{
							"default": {
								Mode: providermanifestv1.ConnectionModeUser,
								Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeNone},
							},
							"workspace": {
								Mode: providermanifestv1.ConnectionModeUser,
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

func TestPluginProcessEnvIsolation(t *testing.T) {
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
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
			},
		},
	}

	factories := NewFactoryRegistry()
	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{})
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
	if err := json.Unmarshal([]byte(result.Body), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Found {
		t.Fatalf("plugin process should not see USER, but got %q", env.Value)
	}

	result, err = prov.Execute(context.Background(), "read_env", map[string]any{"name": "PATH"}, "")
	if err != nil {
		t.Fatalf("Execute read_env PATH: %v", err)
	}
	if err := json.Unmarshal([]byte(result.Body), &env); err != nil {
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

	makeConfig := func(indexedDB *config.HostIndexedDBBindingConfig) *config.Config {
		return &config.Config{
			Plugins: map[string]*config.ProviderEntry{
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
			Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.1/provider-release.yaml"),
			Config: mustNode(t, map[string]any{"dsn": "postgres://main.example.test/gestalt"}),
		},
		"archive": {
			Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.1/provider-release.yaml"),
			Config: mustNode(t, map[string]any{"dsn": "sqlite://archive.db"}),
		},
	}

	checkEnv := func(t *testing.T, indexedDB *config.HostIndexedDBBindingConfig, envName string) bool {
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
		if err := json.Unmarshal([]byte(result.Body), &env); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return env.Found && env.Value != ""
	}

	if got := checkEnv(t, nil, indexeddbservice.DefaultSocketEnv); !got {
		t.Fatal("default IndexedDB env should be set when plugin omits indexeddb and inherits the host selection")
	}
	if got := checkEnv(t, &config.HostIndexedDBBindingConfig{}, indexeddbservice.DefaultSocketEnv); !got {
		t.Fatal("default IndexedDB env should be set when plugin indexeddb is explicitly empty")
	}
	if got := checkEnv(t, &config.HostIndexedDBBindingConfig{Provider: "archive"}, indexeddbservice.DefaultSocketEnv); !got {
		t.Fatal("default IndexedDB env should be set when plugin explicitly selects one indexeddb provider")
	}
	if got := checkEnv(t, nil, indexeddbservice.SocketEnv("main")); got {
		t.Fatal("named IndexedDB env should not be set for inherited plugin indexeddb access")
	}
	if got := checkEnv(t, &config.HostIndexedDBBindingConfig{Provider: "archive"}, indexeddbservice.SocketEnv("archive")); got {
		t.Fatal("named IndexedDB env should not be set when plugins expose a single indexeddb socket")
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
		Plugins: map[string]*config.ProviderEntry{
			"caller": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Invokes: []config.PluginInvocationDependency{
					{Plugin: "callee", Operation: "request_context"},
				},
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

	result, err := prov.Execute(context.Background(), "read_env", map[string]any{"name": plugininvokerservice.DefaultSocketEnv}, "")
	if err != nil {
		t.Fatalf("Execute read_env: %v", err)
	}

	var env struct {
		Value string `json:"value"`
		Found bool   `json:"found"`
	}
	if err := json.Unmarshal([]byte(result.Body), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !env.Found || env.Value == "" {
		t.Fatalf("plugin invoker env %q should be set when plugin declares invokes", plugininvokerservice.DefaultSocketEnv)
	}
}

func TestPluginWorkflowManagerExposeHostSocketEnv(t *testing.T) {
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
		Plugins: map[string]*config.ProviderEntry{
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

	result, err := prov.Execute(context.Background(), "read_env", map[string]any{"name": workflowservice.DefaultManagerSocketEnv}, "")
	if err != nil {
		t.Fatalf("Execute read_env: %v", err)
	}

	var env struct {
		Value string `json:"value"`
		Found bool   `json:"found"`
	}
	if err := json.Unmarshal([]byte(result.Body), &env); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if !env.Found || env.Value == "" {
		t.Fatalf("workflow manager env %q should be set for executable plugins", workflowservice.DefaultManagerSocketEnv)
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
		Plugins: map[string]*config.ProviderEntry{
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

	result, err := prov.Execute(context.Background(), "read_env", map[string]any{"name": agentservice.DefaultManagerSocketEnv}, "")
	if err != nil {
		t.Fatalf("Execute read_env: %v", err)
	}

	var env struct {
		Value string `json:"value"`
		Found bool   `json:"found"`
	}
	if err := json.Unmarshal([]byte(result.Body), &env); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if !env.Found || env.Value == "" {
		t.Fatalf("agent manager env %q should be set for executable plugins", agentservice.DefaultManagerSocketEnv)
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
	services := coretesting.NewStubServices(t)

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
			return &core.OperationResult{Status: http.StatusAccepted, Body: string(body)}, nil
		},
	}); err != nil {
		t.Fatalf("Register roadmap provider: %v", err)
	}

	agentProvider := newStubAgentTurnManagerProvider()
	agentRuntime := &agentRuntime{defaultProviderName: "managed", providers: map[string]coreagent.Provider{"managed": agentProvider}}
	broker := invocation.NewBroker(&pluginProviders.Providers, services.Users, services.ExternalCredentials)
	toolGrants := newTestAgentToolGrants(t)
	agentRuntime.SetToolGrants(toolGrants)
	agentRuntime.SetInvoker(broker)
	manager := agentmanager.New(agentmanager.Config{
		Providers:  &pluginProviders.Providers,
		Agent:      agentRuntime,
		ToolGrants: toolGrants,
		Invoker:    broker,
		PluginInvokes: map[string][]invocation.PluginInvocationDependency{
			"echoext": {{
				Plugin:    "roadmap",
				Operation: "sync",
			}},
		},
	})
	relaySrv := httptest.NewUnstartedServer(newRuntimeRelayTestHandler(t, secret, publicHostServices))
	relaySrv.EnableHTTP2 = true
	relaySrv.StartTLS()
	testutil.CloseOnCleanup(t, relaySrv)

	runtimeProvider := newCapturingBundlePluginRuntime()
	runtimeProvider.support.EgressMode = pluginruntime.EgressModeHostname
	runtimeProvider.fakeHosted = true
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}

	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultHostedProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Execution:            hostedExecutionConfig(&config.HostedRuntimeConfig{}),
				Invokes: []config.PluginInvocationDependency{{
					Plugin:    "roadmap",
					Operation: "sync",
				}},
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
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)

	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()
	assertPublicHostServicesVerified(t, publicHostServices, "agent_manager", agentservice.DefaultManagerSocketEnv)

	prov, err := providers.Get("echoext")
	if err != nil {
		t.Fatalf("providers.Get(echoext): %v", err)
	}

	perms := principal.CompilePermissions([]core.AccessPermission{{
		Plugin:     "roadmap",
		Operations: []string{"sync"},
	}, {
		Plugin: "managed",
	}})
	ctx := principal.WithPrincipal(context.Background(), &principal.Principal{
		SubjectID:        "user:user-123",
		UserID:           "user-123",
		Kind:             principal.KindUser,
		Source:           principal.SourceSession,
		TokenPermissions: perms,
		Scopes:           append([]string{"echoext"}, principal.PermissionPlugins(perms)...),
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
	if err := json.Unmarshal([]byte(result.Body), &roundTrip); err != nil {
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
	var sessionReq coreagent.CreateSessionRequest
	if createSessionCount > 0 {
		sessionReq = agentProvider.createSessionRequests[0]
	}
	var turnReq coreagent.CreateTurnRequest
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
	if sessionReq.CreatedBy.SubjectID != "user:user-123" {
		t.Fatalf("CreateSession created_by.subject_id = %q, want %q", sessionReq.CreatedBy.SubjectID, "user:user-123")
	}
	if turnReq.IdempotencyKey != "plugin-agent-turn" {
		t.Fatalf("CreateTurn idempotency_key = %q, want %q", turnReq.IdempotencyKey, "plugin-agent-turn")
	}
	if turnReq.SessionID != roundTrip.SessionID {
		t.Fatalf("CreateTurn session_id = %q, want %q", turnReq.SessionID, roundTrip.SessionID)
	}
	if turnReq.CreatedBy.SubjectID != "user:user-123" {
		t.Fatalf("CreateTurn created_by.subject_id = %q, want %q", turnReq.CreatedBy.SubjectID, "user:user-123")
	}
	if requireInteraction, _ := turnReq.Metadata["requireInteraction"].(bool); !requireInteraction {
		t.Fatalf("CreateTurn metadata = %#v, want requireInteraction=true", turnReq.Metadata)
	}
	if len(turnReq.Tools) != 0 {
		t.Fatalf("CreateTurn tools = %#v, want no preloaded tools", turnReq.Tools)
	}
	if turnReq.ToolSource != coreagent.ToolSourceModeMCPCatalog {
		t.Fatalf("CreateTurn tool source = %q, want mcp_catalog", turnReq.ToolSource)
	}
	if len(turnReq.ToolRefs) != 1 || turnReq.ToolRefs[0].Plugin != "roadmap" || turnReq.ToolRefs[0].Operation != "sync" {
		t.Fatalf("CreateTurn tool refs = %#v", turnReq.ToolRefs)
	}
}

func TestPluginHostedHTTPBindingsExposeAuthorizationSocketEnv(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echo",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Hosted HTTP subject resolution env")
	manifest.Spec.SecuritySchemes = map[string]*providermanifestv1.HTTPSecurityScheme{
		"public": {
			Type: providermanifestv1.HTTPSecuritySchemeTypeNone,
		},
	}
	manifest.Spec.HTTP = map[string]*providermanifestv1.HTTPBinding{
		"command": {
			Path:     "/command",
			Method:   http.MethodPost,
			Security: "public",
			Target:   "read_env",
			RequestBody: &providermanifestv1.HTTPRequestBody{
				Content: map[string]*providermanifestv1.HTTPMediaType{
					"application/x-www-form-urlencoded": {},
				},
			},
		},
	}

	cfg := &config.Config{
		Plugins: map[string]*config.ProviderEntry{
			"echo": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
			},
		},
	}

	providers, _, err := buildProvidersStrict(context.Background(), cfg, NewFactoryRegistry(), testRuntimePublicEndpointDeps(t, Deps{
		AuthorizationProvider: &hostedHTTPAuthorizationProvider{},
	}))
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	prov, err := providers.Get("echo")
	if err != nil {
		t.Fatalf("providers.Get(echo): %v", err)
	}

	result, err := prov.Execute(context.Background(), "read_env", map[string]any{"name": authorizationservice.DefaultSocketEnv}, "")
	if err != nil {
		t.Fatalf("Execute read_env: %v", err)
	}

	var env struct {
		Value string `json:"value"`
		Found bool   `json:"found"`
	}
	if err := json.Unmarshal([]byte(result.Body), &env); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if !env.Found || env.Value == "" {
		t.Fatalf("authorization env %q should be set for executable plugins with hosted HTTP bindings", authorizationservice.DefaultSocketEnv)
	}
}

func TestPluginWorkflowManagerCRUDUsesRequestContext(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echo",
		Operations: []catalog.CatalogOperation{
			{ID: "create_workflow_schedule", Method: http.MethodPost},
			{ID: "get_workflow_schedule", Method: http.MethodGet},
			{ID: "update_workflow_schedule", Method: http.MethodPost},
			{ID: "delete_workflow_schedule", Method: http.MethodPost},
			{ID: "pause_workflow_schedule", Method: http.MethodPost},
			{ID: "resume_workflow_schedule", Method: http.MethodPost},
			{ID: "create_workflow_trigger", Method: http.MethodPost},
			{ID: "get_workflow_trigger", Method: http.MethodGet},
			{ID: "update_workflow_trigger", Method: http.MethodPost},
			{ID: "delete_workflow_trigger", Method: http.MethodPost},
			{ID: "pause_workflow_trigger", Method: http.MethodPost},
			{ID: "resume_workflow_trigger", Method: http.MethodPost},
			{ID: "publish_workflow_event", Method: http.MethodPost},
		},
	})
	manifest := newExecutableManifest("Echo", "Workflow manager CRUD")
	manager := newStubWorkflowManager()

	cfg := &config.Config{
		Plugins: map[string]*config.ProviderEntry{
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
		Source:    principal.SourceSession,
		Scopes:    []string{"echo"},
	})

	createResult, err := prov.Execute(ctx, "create_workflow_schedule", map[string]any{
		"provider_name": "basic",
		"cron":          "*/5 * * * *",
		"timezone":      "America/New_York",
		"target": map[string]any{
			"plugin":     "roadmap",
			"operation":  "sync",
			"connection": "work",
			"instance":   "default",
			"input": map[string]any{
				"mode": "incremental",
			},
		},
	}, "")
	if err != nil {
		t.Fatalf("Execute(create_workflow_schedule): %v", err)
	}
	var created struct {
		ProviderName string `json:"provider_name"`
		Schedule     struct {
			ID     string `json:"id"`
			Cron   string `json:"cron"`
			Paused bool   `json:"paused"`
			Target struct {
				Plugin    string         `json:"plugin"`
				Operation string         `json:"operation"`
				Input     map[string]any `json:"input"`
			} `json:"target"`
		} `json:"schedule"`
	}
	if err := json.Unmarshal([]byte(createResult.Body), &created); err != nil {
		t.Fatalf("json.Unmarshal(create): %v", err)
	}
	if created.ProviderName != "basic" {
		t.Fatalf("provider_name = %q, want basic", created.ProviderName)
	}
	if created.Schedule.ID == "" {
		t.Fatal("created schedule id should be set")
	}
	if created.Schedule.Target.Plugin != "roadmap" || created.Schedule.Target.Operation != "sync" {
		t.Fatalf("unexpected target: %+v", created.Schedule.Target)
	}
	if got := created.Schedule.Target.Input["mode"]; got != "incremental" {
		t.Fatalf("target.input.mode = %v, want incremental", got)
	}

	getResult, err := prov.Execute(ctx, "get_workflow_schedule", map[string]any{
		"schedule_id": created.Schedule.ID,
	}, "")
	if err != nil {
		t.Fatalf("Execute(get_workflow_schedule): %v", err)
	}
	var fetched map[string]any
	if err := json.Unmarshal([]byte(getResult.Body), &fetched); err != nil {
		t.Fatalf("json.Unmarshal(get): %v", err)
	}
	if fetched["provider_name"] != "basic" {
		t.Fatalf("fetched provider_name = %v, want basic", fetched["provider_name"])
	}

	updateResult, err := prov.Execute(ctx, "update_workflow_schedule", map[string]any{
		"schedule_id":   created.Schedule.ID,
		"provider_name": "secondary",
		"cron":          "0 * * * *",
		"timezone":      "UTC",
		"paused":        true,
		"target": map[string]any{
			"plugin":    "roadmap",
			"operation": "status",
		},
	}, "")
	if err != nil {
		t.Fatalf("Execute(update_workflow_schedule): %v", err)
	}
	var updated struct {
		ProviderName string `json:"provider_name"`
		Schedule     struct {
			Cron   string `json:"cron"`
			Paused bool   `json:"paused"`
			Target struct {
				Operation string `json:"operation"`
			} `json:"target"`
		} `json:"schedule"`
	}
	if err := json.Unmarshal([]byte(updateResult.Body), &updated); err != nil {
		t.Fatalf("json.Unmarshal(update): %v", err)
	}
	if updated.ProviderName != "secondary" || updated.Schedule.Cron != "0 * * * *" || !updated.Schedule.Paused || updated.Schedule.Target.Operation != "status" {
		t.Fatalf("unexpected update result: %+v", updated)
	}

	pauseResult, err := prov.Execute(ctx, "pause_workflow_schedule", map[string]any{
		"schedule_id": created.Schedule.ID,
	}, "")
	if err != nil {
		t.Fatalf("Execute(pause_workflow_schedule): %v", err)
	}
	var paused struct {
		Schedule struct {
			Paused bool `json:"paused"`
		} `json:"schedule"`
	}
	if err := json.Unmarshal([]byte(pauseResult.Body), &paused); err != nil {
		t.Fatalf("json.Unmarshal(pause): %v", err)
	}
	if !paused.Schedule.Paused {
		t.Fatalf("pause result = %+v, want paused schedule", paused)
	}

	resumeResult, err := prov.Execute(ctx, "resume_workflow_schedule", map[string]any{
		"schedule_id": created.Schedule.ID,
	}, "")
	if err != nil {
		t.Fatalf("Execute(resume_workflow_schedule): %v", err)
	}
	var resumed struct {
		Schedule struct {
			Paused bool `json:"paused"`
		} `json:"schedule"`
	}
	if err := json.Unmarshal([]byte(resumeResult.Body), &resumed); err != nil {
		t.Fatalf("json.Unmarshal(resume): %v", err)
	}
	if resumed.Schedule.Paused {
		t.Fatalf("resume result = %+v, want resumed schedule", resumed)
	}

	deleteResult, err := prov.Execute(ctx, "delete_workflow_schedule", map[string]any{
		"schedule_id": created.Schedule.ID,
	}, "")
	if err != nil {
		t.Fatalf("Execute(delete_workflow_schedule): %v", err)
	}
	var deleted struct {
		Deleted bool `json:"deleted"`
	}
	if err := json.Unmarshal([]byte(deleteResult.Body), &deleted); err != nil {
		t.Fatalf("json.Unmarshal(delete): %v", err)
	}
	if !deleted.Deleted {
		t.Fatalf("delete result = %+v, want deleted", deleted)
	}

	createTriggerResult, err := prov.Execute(ctx, "create_workflow_trigger", map[string]any{
		"provider_name": "basic",
		"match": map[string]any{
			"type":    "roadmap.item.updated",
			"source":  "roadmap",
			"subject": "item-123",
		},
		"target": map[string]any{
			"plugin":    "slack",
			"operation": "chat.postMessage",
		},
	}, "")
	if err != nil {
		t.Fatalf("Execute(create_workflow_trigger): %v", err)
	}
	var createdTrigger struct {
		ProviderName string `json:"provider_name"`
		Trigger      struct {
			ID     string `json:"id"`
			Paused bool   `json:"paused"`
			Match  struct {
				Type    string `json:"type"`
				Source  string `json:"source"`
				Subject string `json:"subject"`
			} `json:"match"`
			Target struct {
				Plugin    string `json:"plugin"`
				Operation string `json:"operation"`
			} `json:"target"`
		} `json:"trigger"`
	}
	if err := json.Unmarshal([]byte(createTriggerResult.Body), &createdTrigger); err != nil {
		t.Fatalf("json.Unmarshal(create trigger): %v", err)
	}
	if createdTrigger.ProviderName != "basic" || createdTrigger.Trigger.ID == "" {
		t.Fatalf("unexpected create trigger result: %+v", createdTrigger)
	}
	if createdTrigger.Trigger.Match.Type != "roadmap.item.updated" || createdTrigger.Trigger.Target.Plugin != "slack" || createdTrigger.Trigger.Target.Operation != "chat.postMessage" {
		t.Fatalf("unexpected trigger payload: %+v", createdTrigger.Trigger)
	}

	getTriggerResult, err := prov.Execute(ctx, "get_workflow_trigger", map[string]any{
		"trigger_id": createdTrigger.Trigger.ID,
	}, "")
	if err != nil {
		t.Fatalf("Execute(get_workflow_trigger): %v", err)
	}
	var fetchedTrigger map[string]any
	if err := json.Unmarshal([]byte(getTriggerResult.Body), &fetchedTrigger); err != nil {
		t.Fatalf("json.Unmarshal(get trigger): %v", err)
	}
	if fetchedTrigger["provider_name"] != "basic" {
		t.Fatalf("fetched trigger provider_name = %v, want basic", fetchedTrigger["provider_name"])
	}

	updateTriggerResult, err := prov.Execute(ctx, "update_workflow_trigger", map[string]any{
		"trigger_id":    createdTrigger.Trigger.ID,
		"provider_name": "secondary",
		"paused":        true,
		"match": map[string]any{
			"type": "roadmap.item.synced",
		},
		"target": map[string]any{
			"plugin":    "roadmap",
			"operation": "status",
		},
	}, "")
	if err != nil {
		t.Fatalf("Execute(update_workflow_trigger): %v", err)
	}
	var updatedTrigger struct {
		ProviderName string `json:"provider_name"`
		Trigger      struct {
			Paused bool `json:"paused"`
			Match  struct {
				Type string `json:"type"`
			} `json:"match"`
			Target struct {
				Operation string `json:"operation"`
			} `json:"target"`
		} `json:"trigger"`
	}
	if err := json.Unmarshal([]byte(updateTriggerResult.Body), &updatedTrigger); err != nil {
		t.Fatalf("json.Unmarshal(update trigger): %v", err)
	}
	if updatedTrigger.ProviderName != "secondary" || !updatedTrigger.Trigger.Paused || updatedTrigger.Trigger.Match.Type != "roadmap.item.synced" || updatedTrigger.Trigger.Target.Operation != "status" {
		t.Fatalf("unexpected update trigger result: %+v", updatedTrigger)
	}

	pauseTriggerResult, err := prov.Execute(ctx, "pause_workflow_trigger", map[string]any{
		"trigger_id": createdTrigger.Trigger.ID,
	}, "")
	if err != nil {
		t.Fatalf("Execute(pause_workflow_trigger): %v", err)
	}
	var pausedTrigger struct {
		Trigger struct {
			Paused bool `json:"paused"`
		} `json:"trigger"`
	}
	if err := json.Unmarshal([]byte(pauseTriggerResult.Body), &pausedTrigger); err != nil {
		t.Fatalf("json.Unmarshal(pause trigger): %v", err)
	}
	if !pausedTrigger.Trigger.Paused {
		t.Fatalf("pause trigger result = %+v, want paused trigger", pausedTrigger)
	}

	resumeTriggerResult, err := prov.Execute(ctx, "resume_workflow_trigger", map[string]any{
		"trigger_id": createdTrigger.Trigger.ID,
	}, "")
	if err != nil {
		t.Fatalf("Execute(resume_workflow_trigger): %v", err)
	}
	var resumedTrigger struct {
		Trigger struct {
			Paused bool `json:"paused"`
		} `json:"trigger"`
	}
	if err := json.Unmarshal([]byte(resumeTriggerResult.Body), &resumedTrigger); err != nil {
		t.Fatalf("json.Unmarshal(resume trigger): %v", err)
	}
	if resumedTrigger.Trigger.Paused {
		t.Fatalf("resume trigger result = %+v, want resumed trigger", resumedTrigger)
	}

	deleteTriggerResult, err := prov.Execute(ctx, "delete_workflow_trigger", map[string]any{
		"trigger_id": createdTrigger.Trigger.ID,
	}, "")
	if err != nil {
		t.Fatalf("Execute(delete_workflow_trigger): %v", err)
	}
	var deletedTrigger struct {
		Deleted bool `json:"deleted"`
	}
	if err := json.Unmarshal([]byte(deleteTriggerResult.Body), &deletedTrigger); err != nil {
		t.Fatalf("json.Unmarshal(delete trigger): %v", err)
	}
	if !deletedTrigger.Deleted {
		t.Fatalf("delete trigger result = %+v, want deleted", deletedTrigger)
	}

	publishEventResult, err := prov.Execute(ctx, "publish_workflow_event", map[string]any{
		"type":    "roadmap.item.updated",
		"source":  "roadmap",
		"subject": "item-123",
		"data": map[string]any{
			"id":    "item-123",
			"title": "Ship parity",
		},
		"extensions": map[string]any{
			"tenant": "acme",
		},
	}, "")
	if err != nil {
		t.Fatalf("Execute(publish_workflow_event): %v", err)
	}
	var publishedEvent struct {
		ID         string         `json:"id"`
		Type       string         `json:"type"`
		Source     string         `json:"source"`
		Subject    string         `json:"subject"`
		Data       map[string]any `json:"data"`
		Extensions map[string]any `json:"extensions"`
	}
	if err := json.Unmarshal([]byte(publishEventResult.Body), &publishedEvent); err != nil {
		t.Fatalf("json.Unmarshal(publish event): %v", err)
	}
	if publishedEvent.ID == "" || publishedEvent.Type != "roadmap.item.updated" || publishedEvent.Source != "roadmap" || publishedEvent.Subject != "item-123" {
		t.Fatalf("unexpected published event result: %+v", publishedEvent)
	}
	if publishedEvent.Data["title"] != "Ship parity" || publishedEvent.Extensions["tenant"] != "acme" {
		t.Fatalf("unexpected published event data: %+v", publishedEvent)
	}

	if got := manager.Subjects(); len(got) != 13 || slices.Contains(got, "") || !slices.Equal(got, []string{
		"user:user-123",
		"user:user-123",
		"user:user-123",
		"user:user-123",
		"user:user-123",
		"user:user-123",
		"user:user-123",
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

func TestPluginWorkflowManagerRejectsInvalidInvocationToken(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echo",
		Operations: []catalog.CatalogOperation{
			{ID: "create_workflow_schedule", Method: http.MethodPost},
		},
	})
	manifest := newExecutableManifest("Echo", "Workflow manager invalid handle")

	cfg := &config.Config{
		Plugins: map[string]*config.ProviderEntry{
			"echo": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
			},
		},
	}

	manager := newStubWorkflowManager()
	secret := []byte("0123456789abcdef0123456789abcdef")
	providers, _, err := buildProvidersStrict(context.Background(), cfg, NewFactoryRegistry(), testRuntimePublicEndpointDeps(t, Deps{
		EncryptionKey:   secret,
		WorkflowManager: manager,
	}))
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	prov, err := providers.Get("echo")
	if err != nil {
		t.Fatalf("providers.Get(echo): %v", err)
	}

	result, err := prov.Execute(context.Background(), "create_workflow_schedule", map[string]any{
		"invocation_token": "forged-token",
		"cron":             "*/5 * * * *",
		"target": map[string]any{
			"plugin":    "roadmap",
			"operation": "sync",
		},
	}, "")
	if err != nil {
		t.Fatalf("Execute(create_workflow_schedule): %v", err)
	}

	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(result.Body), &body); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if !strings.Contains(body.Error, "invalid or expired") {
		t.Fatalf("invalid invocation token error = %q, want invalid or expired", body.Error)
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
				Source:      principal.SourceSession,
				DisplayName: "Nested Success",
				Scopes:      []string{"caller", "example"},
			}

			params := map[string]any{
				"plugin":    "example",
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
			if err := json.Unmarshal([]byte(result.Body), &got); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}
			if !got.OK {
				t.Fatalf("invoke_plugin returned error envelope: %+v", got)
			}
			if got.TargetPlugin != "example" || got.TargetOperation != "request_context" {
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
			if got.Body.Subject.Kind != string(principal.KindUser) {
				t.Fatalf("nested subject.kind = %q, want %q", got.Body.Subject.Kind, principal.KindUser)
			}
			if got.Body.Subject.AuthSource != principal.SourceSession.String() {
				t.Fatalf("nested subject.auth_source = %q, want %q", got.Body.Subject.AuthSource, principal.SourceSession.String())
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
			Source: principal.SourceSession,
			Scopes: []string{"caller", "example"},
		},
		"caller",
		"",
		"invoke_plugin",
		map[string]any{
			"plugin":    "example",
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
	if err := json.Unmarshal([]byte(result.Body), &got); err != nil {
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
			Source: principal.SourceAPIToken,
			Scopes: []string{"caller"},
		},
		"caller",
		"",
		"invoke_plugin",
		map[string]any{
			"plugin":    "example",
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
	if err := json.Unmarshal([]byte(result.Body), &got); err != nil {
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
			Source: principal.SourceSession,
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
	if err := json.Unmarshal([]byte(result.Body), &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got.InvocationToken == "" {
		t.Fatalf("nested invocation_token = %q, want non-empty", got.InvocationToken)
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
			Source: principal.SourceSession,
			Scopes: []string{"caller", "linear"},
		},
		"caller",
		"",
		"invoke_plugin_graphql",
		map[string]any{
			"plugin":   "linear",
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
		TargetPlugin           string         `json:"target_plugin"`
		TargetOperation        string         `json:"target_operation"`
		UsedConnectionOverride bool           `json:"used_connection_override"`
		Status                 int            `json:"status"`
		Body                   map[string]any `json:"body"`
		Error                  string         `json:"error"`
	}
	if err := json.Unmarshal([]byte(result.Body), &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if !got.OK {
		t.Fatalf("invoke_plugin_graphql returned error envelope: %+v", got)
	}
	if got.TargetPlugin != "linear" || got.TargetOperation != "graphql" {
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

func TestPluginInvokesRejectUndeclaredGraphQLSurface(t *testing.T) {
	t.Parallel()

	var nonIntrospectionCalls atomic.Int32
	schema := pluginInvokeGraphQLSchema()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(payload.Query, "__schema") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"__schema": schema,
				},
			})
			return
		}
		nonIntrospectionCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"ok": true},
		})
	}))
	t.Cleanup(srv.Close)

	harness := newGraphQLSurfaceInvokeHarness(t, srv.URL, false, config.AuthorizationConfig{})
	ctx := context.Background()
	user := newNestedInvokeUser(t, harness, ctx, "nested-graphql-surface-denied@test.com")
	storeNestedInvokeToken(t, harness, ctx, user.ID, "caller", "work", "default")
	storeNestedInvokeToken(t, harness, ctx, user.ID, "linear", "work", "default")

	result, err := harness.invoker.Invoke(
		invocation.WithConnection(context.Background(), "work"),
		&principal.Principal{
			UserID: user.ID,
			Kind:   principal.KindUser,
			Source: principal.SourceSession,
			Scopes: []string{"caller", "linear"},
		},
		"caller",
		"",
		"invoke_plugin_graphql",
		map[string]any{
			"plugin":   "linear",
			"document": "query Viewer($team: String!) { viewer(team: $team) { id } }",
			"variables": map[string]any{
				"team": "eng",
			},
		},
	)
	if err != nil {
		t.Fatalf("Invoke(caller.invoke_plugin_graphql): %v", err)
	}

	var got struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(result.Body), &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got.OK {
		t.Fatalf("expected undeclared graphql surface rejection, got success: %+v", got)
	}
	if !strings.Contains(got.Error, `may not invoke linear surface "graphql"`) {
		t.Fatalf("undeclared graphql surface error = %q, want target rejection", got.Error)
	}
	if got := nonIntrospectionCalls.Load(); got != 0 {
		t.Fatalf("non-introspection graphql calls = %d, want 0", got)
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
			Source: principal.SourceSession,
			Scopes: []string{"caller", "example"},
		},
		"caller",
		"",
		"invoke_plugin",
		map[string]any{
			"plugin":    "example",
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
	if err := json.Unmarshal([]byte(result.Body), &got); err != nil {
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
		name      string
		email     string
		tokens    []tokenSpec
		params    map[string]any
		wantError string
	}{
		{
			name:  "undeclared target",
			email: "nested-declared@test.com",
			tokens: []tokenSpec{
				{plugin: "caller", connection: "work", instance: "default"},
				{plugin: "example", connection: "work", instance: "default"},
			},
			params: map[string]any{
				"plugin":    "example",
				"operation": "status",
			},
			wantError: `may not invoke example.status`,
		},
		{
			name:  "invalid invocation token",
			email: "nested-invalid-handle@test.com",
			tokens: []tokenSpec{
				{plugin: "caller", connection: "work", instance: "default"},
				{plugin: "example", connection: "work", instance: "default"},
			},
			params: map[string]any{
				"plugin":           "example",
				"operation":        "request_context",
				"invocation_token": "forged-token",
			},
			wantError: "invalid or expired",
		},
		{
			name:  "missing target token",
			email: "nested-no-target-token@test.com",
			tokens: []tokenSpec{
				{plugin: "caller", connection: "work", instance: "default"},
			},
			params: map[string]any{
				"plugin":    "example",
				"operation": "request_context",
			},
			wantError: "code = FailedPrecondition",
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
				"plugin":     "example",
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
					Source: principal.SourceSession,
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

			var got invokePluginEnvelope
			if err := json.Unmarshal([]byte(result.Body), &got); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
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
			Plugins: map[string]*config.ProviderEntry{
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
		if err := json.Unmarshal([]byte(result.Body), &env); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return env.Found && env.Value != ""
	}

	if got := checkEnv(t, nil, cacheservice.DefaultSocketEnv); got {
		t.Fatal("default cache env should not be set without plugin cache bindings")
	}
	if got := checkEnv(t, []string{"session"}, cacheservice.DefaultSocketEnv); !got {
		t.Fatal("default cache env should be set with a single plugin cache binding")
	}
	if got := checkEnv(t, []string{"session"}, cacheservice.SocketEnv("session")); !got {
		t.Fatal("named cache env should be set with a single plugin cache binding")
	}
	if got := checkEnv(t, []string{"session", "rate_limit"}, cacheservice.DefaultSocketEnv); got {
		t.Fatal("default cache env should not be set with multiple plugin cache bindings")
	}
	if got := checkEnv(t, []string{"session", "rate_limit"}, cacheservice.SocketEnv("session")); !got {
		t.Fatal(`named cache env for "session" should be set with multiple plugin cache bindings`)
	}
	if got := checkEnv(t, []string{"session", "rate_limit"}, cacheservice.SocketEnv("rate_limit")); !got {
		t.Fatal(`named cache env for "rate_limit" should be set with multiple plugin cache bindings`)
	}
}

func TestInjectedPluginRuntimeStopsSessionOnProviderClose(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	runtimeProvider := newCapturingPluginRuntime()
	t.Cleanup(func() { _ = runtimeProvider.Close() })
	cfg := &config.Config{
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
			},
		},
	}

	providers, _, err := buildProvidersStrict(context.Background(), cfg, NewFactoryRegistry(), Deps{
		PluginRuntime: runtimeProvider,
	})
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
		t.Fatal("expected CloseProviders to stop the hosted plugin runtime session")
	}
}

func TestInjectedPluginRuntimeStopSessionTimeoutDoesNotHangProviderClose(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	runtimeProvider := &slowStopPluginRuntime{inner: pluginruntime.NewLocalProvider()}
	t.Cleanup(func() { _ = runtimeProvider.Close() })
	cfg := &config.Config{
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
			},
		},
	}

	providers, _, err := buildProvidersStrict(context.Background(), cfg, NewFactoryRegistry(), Deps{
		PluginRuntime: runtimeProvider,
	})
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

	done := make(chan error, 1)
	go func() {
		done <- CloseProviders(providers)
	}()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
			t.Fatalf("CloseProviders error = %v, want deadline exceeded", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CloseProviders hung waiting for hosted runtime shutdown")
	}

	if runtimeProvider.stopCount.Load() == 0 {
		t.Fatal("expected CloseProviders to attempt stopping the hosted plugin runtime session")
	}
}

func TestInjectedPluginRuntimeStopSessionTimeoutDoesNotHangBootstrapFailure(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	runtimeProvider := &failingStartPluginSlowStopPluginRuntime{
		slowStopPluginRuntime: slowStopPluginRuntime{inner: pluginruntime.NewLocalProvider()},
		err:                   fmt.Errorf("start failed"),
	}
	t.Cleanup(func() { _ = runtimeProvider.Close() })
	cfg := &config.Config{
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Invokes: []config.PluginInvocationDependency{
					{Plugin: "other", Operation: "read"},
				},
			},
		},
	}

	done := make(chan error, 1)
	go func() {
		_, _, err := buildProvidersStrict(context.Background(), cfg, NewFactoryRegistry(), testRuntimePublicEndpointDeps(t, Deps{
			PluginRuntime: runtimeProvider,
		}))
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "start failed") {
			t.Fatalf("buildProvidersStrict error = %v, want hosted plugin start failure", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("buildProvidersStrict hung waiting for hosted runtime shutdown")
	}

	if runtimeProvider.stopCount.Load() == 0 {
		t.Fatal("expected bootstrap failure to attempt stopping the hosted plugin runtime session")
	}
}

func TestPluginRuntimeConfigSelectedProviderStartsSessionWithRuntimeFields(t *testing.T) {
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
	imageEntrypoint := filepath.Join(imageEntrypointDir, "plugin")
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
	runtimeProvider := newCapturingPluginRuntime()
	ctxSentinel := &struct{}{}
	var factoryContextValue any
	factories := NewFactoryRegistry()
	factories.Runtime = func(ctx context.Context, _ string, _ *config.RuntimeProviderEntry, _ Deps) (pluginruntime.Provider, error) {
		factoryContextValue = ctx.Value(runtimeFactoryContextKey{})
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultHostedProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Execution: &config.ExecutionConfig{
					Mode: config.ExecutionModeHosted,
					Runtime: &config.HostedRuntimeConfig{
						Template: "python-dev",
						Image:    "ghcr.io/valon/gestalt-python-runtime:latest",
						Metadata: map[string]string{"tenant": "eng"},
					},
				},
			},
		},
	}

	deps := Deps{
		PluginRuntimeRegistry: newPluginRuntimeRegistry(cfg, factories.Runtime, Deps{}),
	}
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
	if req.PluginName != "echoext" {
		t.Fatalf("StartSession PluginName = %q, want echoext", req.PluginName)
	}
	if req.Template != "python-dev" {
		t.Fatalf("StartSession Template = %q, want python-dev", req.Template)
	}
	if req.Image != "ghcr.io/valon/gestalt-python-runtime:latest" {
		t.Fatalf("StartSession Image = %q", req.Image)
	}
	if req.Metadata["tenant"] != "eng" {
		t.Fatalf("StartSession Metadata[tenant] = %q, want eng", req.Metadata["tenant"])
	}
	if req.Metadata["provider_kind"] != "plugin" {
		t.Fatalf("StartSession Metadata[provider_kind] = %q, want plugin", req.Metadata["provider_kind"])
	}
	if req.Metadata["provider_name"] != "echoext" {
		t.Fatalf("StartSession Metadata[provider_name] = %q, want echoext", req.Metadata["provider_name"])
	}
	if factoryContextValue != ctxSentinel {
		t.Fatalf("runtime factory context value = %#v, want %#v", factoryContextValue, ctxSentinel)
	}
}

func TestPluginRuntimeStartsHostedCommandWithoutBundleStaging(t *testing.T) {
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

	runtimeProvider := newCapturingBundlePluginRuntime()
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultHostedProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: manifestPath,
				Execution:            hostedExecutionConfig(&config.HostedRuntimeConfig{}),
			},
		},
	}

	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{
		PluginRuntimeRegistry: newPluginRuntimeRegistry(cfg, factories.Runtime, Deps{}),
	})
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

	requests := runtimeProvider.startPluginRequestsCopy()
	if len(requests) != 1 {
		t.Fatalf("start plugin requests = %d, want 1", len(requests))
	}
	req := requests[0]
	if req.Command != bin {
		t.Fatalf("StartPlugin Command = %q, want configured command", req.Command)
	}
	if !slices.Equal(req.Args, []string{"provider"}) {
		t.Fatalf("StartPlugin Args = %#v, want configured args", req.Args)
	}

	if err := CloseProviders(providers); err != nil {
		t.Fatalf("CloseProviders: %v", err)
	}
}

func TestPluginRuntimeImageLaunchUsesManifestEntrypoint(t *testing.T) {
	t.Parallel()

	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	manifest.Entrypoint = &providermanifestv1.Entrypoint{
		ArtifactPath: "bin/gestalt-plugin-echo",
		Args:         []string{"--config", "/etc/gestalt/echo.yaml"},
	}

	runtimeProvider := newCapturingBundlePluginRuntime()
	runtimeProvider.fakeHosted = true
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultHostedProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Execution: hostedExecutionConfig(&config.HostedRuntimeConfig{
					Image: "ghcr.io/example/echo-plugin@sha256:abc123",
				}),
			},
		},
	}

	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{
		PluginRuntimeRegistry: newPluginRuntimeRegistry(cfg, factories.Runtime, Deps{}),
	})
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() {
		if err := CloseProviders(providers); err != nil {
			t.Fatalf("CloseProviders: %v", err)
		}
	}()

	requests := runtimeProvider.startPluginRequestsCopy()
	if len(requests) != 1 {
		t.Fatalf("start plugin requests = %d, want 1", len(requests))
	}
	req := requests[0]
	if req.Command != "./bin/gestalt-plugin-echo" {
		t.Fatalf("StartPlugin Command = %q, want manifest image entrypoint", req.Command)
	}
	if !slices.Equal(req.Args, []string{"--config", "/etc/gestalt/echo.yaml"}) {
		t.Fatalf("StartPlugin Args = %#v, want manifest image args", req.Args)
	}
}

func TestPluginRuntimeTemplateLaunchUsesManifestEntrypoint(t *testing.T) {
	t.Parallel()

	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	manifest.Entrypoint = &providermanifestv1.Entrypoint{
		ArtifactPath: "bin/gestalt-plugin-echo",
		Args:         []string{"--config", "/etc/gestalt/echo.yaml"},
	}

	runtimeProvider := newCapturingBundlePluginRuntime()
	runtimeProvider.fakeHosted = true
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultHostedProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              "/host/only/plugin",
				Args:                 []string{"host-arg"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Execution: hostedExecutionConfig(&config.HostedRuntimeConfig{
					Template: "python-runtime",
				}),
			},
		},
	}

	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{
		PluginRuntimeRegistry: newPluginRuntimeRegistry(cfg, factories.Runtime, Deps{}),
	})
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() {
		if err := CloseProviders(providers); err != nil {
			t.Fatalf("CloseProviders: %v", err)
		}
	}()

	requests := runtimeProvider.startPluginRequestsCopy()
	if len(requests) != 1 {
		t.Fatalf("start plugin requests = %d, want 1", len(requests))
	}
	req := requests[0]
	if req.Command != "./bin/gestalt-plugin-echo" {
		t.Fatalf("StartPlugin Command = %q, want manifest template entrypoint", req.Command)
	}
	if !slices.Equal(req.Args, []string{"--config", "/etc/gestalt/echo.yaml"}) {
		t.Fatalf("StartPlugin Args = %#v, want manifest template args", req.Args)
	}
}

func TestPluginRuntimeLocalFallbackImageLaunchUsesConfiguredCommand(t *testing.T) {
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
		ArtifactPath: "bin/gestalt-plugin-echo",
		Args:         []string{"--config", "/etc/gestalt/echo.yaml"},
	}

	cfg := &config.Config{
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Execution: hostedExecutionConfig(&config.HostedRuntimeConfig{
					Image: "ghcr.io/example/echo-plugin@sha256:abc123",
				}),
			},
		},
	}

	providers, _, err := buildProvidersStrict(context.Background(), cfg, NewFactoryRegistry(), Deps{})
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

func TestPluginRuntimeConfigUsesPublicS3RelayWithoutHostServiceTunnelCapability(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	runtimeProvider := newCapturingBundlePluginRuntime()
	runtimeProvider.support.EgressMode = pluginruntime.EgressModeHostname
	runtimeProvider.fakeHosted = true
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultHostedProvider: "hosted"},
			Egress:  config.EgressConfig{DefaultAction: string(egress.PolicyDeny)},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Execution:            hostedExecutionConfig(&config.HostedRuntimeConfig{}),
				S3:                   []string{"main"},
			},
		},
	}

	deps := Deps{
		BaseURL:       "https://gestalt.example.test",
		EncryptionKey: []byte("0123456789abcdef0123456789abcdef"),
		Egress:        newEgressDeps(cfg),
		S3: map[string]s3store.Client{
			"main":    &coretesting.StubS3{},
			"archive": &coretesting.StubS3{},
		},
	}
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)

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
		if err := json.Unmarshal([]byte(result.Body), &env); err != nil {
			t.Fatalf("unmarshal env result for %s: %v", envName, err)
		}
		return env.Value, env.Found
	}

	if got, found := checkEnv(s3service.DefaultSocketEnv); !found || got != "tls://gestalt.example.test:443" {
		t.Fatalf("plugin s3 env %s = (%q, %v), want (%q, true)", s3service.DefaultSocketEnv, got, found, "tls://gestalt.example.test:443")
	}
	for _, binding := range []string{"main"} {
		envName := s3service.SocketEnv(binding)
		if got, found := checkEnv(envName); !found || got != "tls://gestalt.example.test:443" {
			t.Fatalf("plugin s3 env %s = (%q, %v), want (%q, true)", envName, got, found, "tls://gestalt.example.test:443")
		}
		tokenEnvName := s3service.SocketTokenEnv(binding)
		if got, found := checkEnv(tokenEnvName); !found || got == "" {
			t.Fatalf("plugin s3 token env %s = (%q, %v), want non-empty token", tokenEnvName, got, found)
		}
	}
	if got, found := checkEnv(s3service.SocketTokenEnv("")); !found || got == "" {
		t.Fatalf("plugin s3 token env %s = (%q, %v), want non-empty token", s3service.SocketTokenEnv(""), got, found)
	}

	startRequests := runtimeProvider.startPluginRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("StartPlugin requests = %d, want 1", len(startRequests))
	}
	for _, binding := range []string{"", "main"} {
		assertStartPluginRelayEnv(t, startRequests[0], s3service.SocketEnv(binding))
	}
	if allowedHosts := slices.Clone(startRequests[0].Egress.AllowedHosts); !slices.Contains(allowedHosts, "gestalt.example.test") {
		t.Fatalf("StartPlugin allowed hosts = %#v, want relay host gestalt.example.test", allowedHosts)
	}
}

func TestProviderDevRuntimeEnvUsesPublicHostServiceRelay(t *testing.T) {
	t.Parallel()

	secret := []byte("0123456789abcdef0123456789abcdef")
	publicHostServices := runtimehost.NewPublicHostServiceRegistry()
	relaySrv := httptest.NewUnstartedServer(newRuntimeRelayTestHandler(t, secret, publicHostServices))
	relaySrv.EnableHTTP2 = true
	relaySrv.StartTLS()
	testutil.CloseOnCleanup(t, relaySrv)

	entry := &config.ProviderEntry{
		ResolvedManifest: &providermanifestv1.Manifest{
			Source: "local://echoext",
		},
		S3: []string{"main"},
	}
	deps := Deps{
		BaseURL:            relaySrv.URL,
		EncryptionKey:      secret,
		PublicHostServices: publicHostServices,
		S3: map[string]s3store.Client{
			"main": &coretesting.StubS3{},
		},
	}
	runtimeHostServiceDescriptors := map[string][]hostServiceBindingDescriptor{}
	manager, err := providerdev.NewManager([]providerdev.Target{{
		Name: "echoext",
		RuntimeEnv: func(sessionID string) (providerdev.RuntimeEnv, error) {
			return buildProviderDevRuntimeEnv("echoext", entry, deps, sessionID, runtimeHostServiceDescriptors["echoext"])
		},
	}})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() {
		if err := manager.Close(); err != nil {
			t.Fatalf("provider dev manager close: %v", err)
		}
	})
	targets := []providerdev.Target{{Name: "echoext"}}
	if err := registerProviderDevPublicHostServices(&config.Config{
		Plugins: map[string]*config.ProviderEntry{"echoext": entry},
	}, manager, deps, targets, runtimeHostServiceDescriptors); err != nil {
		t.Fatalf("registerProviderDevPublicHostServices: %v", err)
	}
	session, err := manager.CreateSession(context.Background(), &principal.Principal{
		SubjectID: "user:test-user",
		UserID:    "test-user",
		Kind:      principal.KindUser,
	}, providerdev.CreateSessionRequest{
		Providers: []providerdev.AttachProvider{{Name: "echoext"}},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if len(session.Providers) != 1 {
		t.Fatalf("session providers = %#v, want one", session.Providers)
	}
	env := session.Providers[0].Env

	for _, binding := range []string{"", "main"} {
		socketEnv := s3service.SocketEnv(binding)
		if got := env[socketEnv]; !strings.HasPrefix(got, "tls://") {
			t.Fatalf("runtime env %s = %q, want tls relay target", socketEnv, got)
		}
		tokenEnv := s3service.SocketTokenEnv(binding)
		if got := env[tokenEnv]; got == "" {
			t.Fatalf("runtime env %s is empty, want relay token", tokenEnv)
		}
	}
	record, err := fakeHostedS3RoundTrip("assets", "plans/q3.txt", "ship-it", "main", env)
	if err != nil {
		t.Fatalf("S3 round trip via provider-dev relay: %v", err)
	}
	if got := record["body"]; got != "ship-it" {
		t.Fatalf("S3 body = %#v, want ship-it", got)
	}
	if err := manager.CloseSession(&principal.Principal{
		SubjectID: "user:test-user",
		UserID:    "test-user",
		Kind:      principal.KindUser,
	}, session.AttachID); err != nil {
		t.Fatalf("CloseSession: %v", err)
	}
	if _, err := fakeHostedS3RoundTrip("assets", "plans/stale.txt", "stale", "main", env); err == nil {
		t.Fatalf("S3 round trip after provider-dev session close succeeded, want stale relay failure")
	}
}

func TestBuildProviderDevManagerRegistersMemoryModePublicHostServiceVerifiers(t *testing.T) {
	t.Parallel()

	publicHostServices := runtimehost.NewPublicHostServiceRegistry()
	providers := registry.New()
	if err := providers.Providers.Register("echoext", &connectedCapabilityProvider{}); err != nil {
		t.Fatalf("Register provider: %v", err)
	}
	entry := &config.ProviderEntry{
		ResolvedManifest: &providermanifestv1.Manifest{
			Source: "local://echoext",
		},
		Cache: []string{"main"},
		S3:    []string{"main"},
	}
	var cacheFactoryCalls atomic.Int64
	manager, err := buildProviderDevManager(&config.Config{
		Plugins: map[string]*config.ProviderEntry{"echoext": entry},
	}, &providers.Providers, Deps{
		BaseURL:            "https://gestalt.example.test",
		EncryptionKey:      []byte("0123456789abcdef0123456789abcdef"),
		PublicHostServices: publicHostServices,
		CacheDefs: map[string]*config.ProviderEntry{
			"main": {},
		},
		CacheFactory: func(yaml.Node) (corecache.Cache, error) {
			if cacheFactoryCalls.Add(1) > 1 {
				return nil, fmt.Errorf("cache factory opened more than once")
			}
			return coretesting.NewStubCache(), nil
		},
		S3: map[string]s3store.Client{
			"main": &coretesting.StubS3{},
		},
	})
	if err != nil {
		t.Fatalf("buildProviderDevManager: %v", err)
	}
	if manager == nil {
		t.Fatal("buildProviderDevManager returned nil manager")
	}
	t.Cleanup(func() {
		if err := manager.Close(); err != nil {
			t.Fatalf("provider dev manager close: %v", err)
		}
	})
	assertPublicHostServicesVerified(t, publicHostServices, "s3", s3service.SocketEnv("main"))
	assertPublicHostServicesVerified(t, publicHostServices, "cache", cacheservice.SocketEnv("main"))

	p := &principal.Principal{SubjectID: "user:test-user", UserID: "test-user", Kind: principal.KindUser}
	session, err := manager.CreateSession(context.Background(), p, providerdev.CreateSessionRequest{
		Providers: []providerdev.AttachProvider{{Name: "echoext"}},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if calls := cacheFactoryCalls.Load(); calls != 1 {
		t.Fatalf("cache factory calls after CreateSession = %d, want startup-only open", calls)
	}
	for _, service := range publicHostServices.Services() {
		if strings.TrimSpace(service.Service.Name) != "s3" || strings.TrimSpace(service.Service.EnvVar) != s3service.SocketEnv("main") {
			continue
		}
		if service.SessionVerifier == nil {
			t.Fatal("S3 public host service verifier is nil")
		}
		if err := service.SessionVerifier.VerifyHostServiceSession(context.Background(), session.AttachID); err != nil {
			t.Fatalf("VerifyHostServiceSession(active memory session): %v", err)
		}
		if err := manager.CloseSession(p, session.AttachID); err != nil {
			t.Fatalf("CloseSession: %v", err)
		}
		if err := service.SessionVerifier.VerifyHostServiceSession(context.Background(), session.AttachID); grpcstatus.Code(err) != codes.NotFound {
			t.Fatalf("VerifyHostServiceSession(closed memory session) code = %v, want %v (err=%v)", grpcstatus.Code(err), codes.NotFound, err)
		}
		return
	}
	t.Fatalf("public host services = %#v, want s3/%s", publicHostServices.Services(), s3service.SocketEnv("main"))
}

func TestPluginRuntimeConfigUsesPublicAuthorizationRelayWithoutHostServiceTunnelCapability(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Hosted HTTP auth lookup env")
	manifest.Spec.SecuritySchemes = map[string]*providermanifestv1.HTTPSecurityScheme{
		"public": {
			Type: providermanifestv1.HTTPSecuritySchemeTypeNone,
		},
	}
	manifest.Spec.HTTP = map[string]*providermanifestv1.HTTPBinding{
		"command": {
			Path:     "/command",
			Method:   http.MethodPost,
			Security: "public",
			Target:   "read_env",
			RequestBody: &providermanifestv1.HTTPRequestBody{
				Content: map[string]*providermanifestv1.HTTPMediaType{
					"application/x-www-form-urlencoded": {},
				},
			},
		},
	}
	artifactPath := filepath.Join(manifestRoot, filepath.Base(bin))
	artifactBytes, err := os.ReadFile(bin)
	if err != nil {
		t.Fatalf("os.ReadFile(bin): %v", err)
	}
	if err := os.WriteFile(artifactPath, artifactBytes, 0o755); err != nil {
		t.Fatalf("os.WriteFile(artifact): %v", err)
	}
	digest, err := providerpkg.FileSHA256(artifactPath)
	if err != nil {
		t.Fatalf("providerpkg.FileSHA256(artifact): %v", err)
	}
	manifest.Artifacts = []providermanifestv1.Artifact{{
		OS:     runtime.GOOS,
		Arch:   runtime.GOARCH,
		Path:   filepath.Base(artifactPath),
		SHA256: digest,
	}}
	manifest.Entrypoint = &providermanifestv1.Entrypoint{ArtifactPath: filepath.Base(artifactPath)}
	manifestData, err := providerpkg.EncodeManifestFormat(manifest, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("providerpkg.EncodeManifestFormat(manifest): %v", err)
	}
	manifestPath := filepath.Join(manifestRoot, "manifest.yaml")
	if err := os.WriteFile(manifestPath, manifestData, 0o644); err != nil {
		t.Fatalf("os.WriteFile(manifest): %v", err)
	}

	runtimeProvider := newCapturingBundlePluginRuntime()
	runtimeProvider.support.EgressMode = pluginruntime.EgressModeHostname
	runtimeProvider.fakeHosted = true
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultHostedProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: manifestPath,
				Execution:            hostedExecutionConfig(&config.HostedRuntimeConfig{}),
			},
		},
	}

	deps := Deps{
		BaseURL:               "https://gestalt.example.test",
		EncryptionKey:         []byte("0123456789abcdef0123456789abcdef"),
		AuthorizationProvider: &hostedHTTPAuthorizationProvider{},
	}
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)

	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })

	prov, err := providers.Get("echoext")
	if err != nil {
		t.Fatalf("providers.Get(echoext): %v", err)
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
		if err := json.Unmarshal([]byte(result.Body), &env); err != nil {
			t.Fatalf("json.Unmarshal(%s): %v", envName, err)
		}
		return env.Value, env.Found
	}

	if got, found := checkEnv(authorizationservice.DefaultSocketEnv); !found || got != "tls://gestalt.example.test:443" {
		t.Fatalf("plugin authorization env %s = (%q, %v), want (%q, true)", authorizationservice.DefaultSocketEnv, got, found, "tls://gestalt.example.test:443")
	}
	if got, found := checkEnv(authorizationservice.SocketTokenEnv()); !found || got == "" {
		t.Fatalf("plugin authorization token env %s = (%q, %v), want non-empty token", authorizationservice.SocketTokenEnv(), got, found)
	}

	startRequests := runtimeProvider.startPluginRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("StartPlugin requests = %d, want 1", len(startRequests))
	}
	assertStartPluginRelayEnv(t, startRequests[0], authorizationservice.DefaultSocketEnv)
	if allowedHosts := slices.Clone(startRequests[0].Egress.AllowedHosts); len(allowedHosts) != 0 {
		t.Fatalf("StartPlugin allowed hosts = %#v, want none when hostname egress enforcement is not required", allowedHosts)
	}
}

func TestPluginRuntimeConfigUsesPublicIndexedDBRelayWithoutHostServiceTunnelCapability(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	runtimeProvider := newCapturingBundlePluginRuntime()
	runtimeProvider.support.EgressMode = pluginruntime.EgressModeHostname
	runtimeProvider.fakeHosted = true
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultHostedProvider: "hosted"},
			Egress:  config.EgressConfig{DefaultAction: string(egress.PolicyDeny)},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Execution:            hostedExecutionConfig(&config.HostedRuntimeConfig{}),
				IndexedDB:            &config.HostIndexedDBBindingConfig{ObjectStores: []string{"tasks"}},
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
				Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.1/provider-release.yaml"),
				Config: mustNode(t, map[string]any{"bucket": "plugin-state"}),
			},
		},
		IndexedDBFactory: func(yaml.Node) (indexeddb.IndexedDB, error) {
			return &coretesting.StubIndexedDB{}, nil
		},
	}
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)

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
		if err := json.Unmarshal([]byte(result.Body), &env); err != nil {
			t.Fatalf("unmarshal env result for %s: %v", envName, err)
		}
		if !env.Found {
			t.Fatalf("env %s not found", envName)
		}
		return env.Value
	}

	if got := checkEnv(indexeddbservice.DefaultSocketEnv); got != "tls://gestalt.example.test:443" {
		t.Fatalf("plugin indexeddb socket env = %q, want %q", got, "tls://gestalt.example.test:443")
	}
	if got := checkEnv(indexeddbservice.SocketTokenEnv("")); got == "" {
		t.Fatal("plugin indexeddb socket token env should be set for the public relay")
	}

	startRequests := runtimeProvider.startPluginRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("StartPlugin requests = %d, want 1", len(startRequests))
	}
	assertStartPluginRelayEnv(t, startRequests[0], indexeddbservice.DefaultSocketEnv)
	if allowedHosts := slices.Clone(startRequests[0].Egress.AllowedHosts); !slices.Contains(allowedHosts, "gestalt.example.test") {
		t.Fatalf("StartPlugin allowed hosts = %#v, want relay host gestalt.example.test", allowedHosts)
	}
}

func TestPluginRuntimePublicIndexedDBRelayRoundTripsThroughHostedPlugin(t *testing.T) {
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
	runtimeProvider := newCapturingBundlePluginRuntime()
	runtimeProvider.support.EgressMode = pluginruntime.EgressModeHostname
	runtimeProvider.fakeHosted = true

	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}

	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultHostedProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {Driver: config.RuntimeProviderDriver("capture")},
			},
		},
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Execution:            hostedExecutionConfig(&config.HostedRuntimeConfig{}),
				IndexedDB:            &config.HostIndexedDBBindingConfig{ObjectStores: []string{"tasks"}},
			},
		},
	}

	boundDB := &trackedIndexedDB{StubIndexedDB: coretesting.StubIndexedDB{}}
	deps := Deps{
		BaseURL:               relaySrv.URL,
		EncryptionKey:         secret,
		SelectedIndexedDBName: "memory",
		IndexedDBDefs: map[string]*config.ProviderEntry{
			"memory": {
				Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.1/provider-release.yaml"),
				Config: mustNode(t, map[string]any{"bucket": "plugin-state"}),
			},
		},
		IndexedDBFactory: func(yaml.Node) (indexeddb.IndexedDB, error) {
			return boundDB, nil
		},
		PublicHostServices: publicHostServices,
	}
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)

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
	if err := json.Unmarshal([]byte(result.Body), &record); err != nil {
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

	startRequests := runtimeProvider.startPluginRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("StartPlugin requests = %d, want 1", len(startRequests))
	}
	if got := startRequests[0].Env[indexeddbservice.SocketTokenEnv("")]; got == "" {
		t.Fatal("StartPlugin env should include the IndexedDB relay token")
	}
	if got := startRequests[0].Env[indexeddbservice.DefaultSocketEnv]; !strings.HasPrefix(got, "tls://") {
		t.Fatalf("StartPlugin env %s = %q, want tls relay target", indexeddbservice.DefaultSocketEnv, got)
	}

	expiredAt := time.Now().Add(-time.Minute)
	runtimeProvider.setSessionLifecycle(startRequests[0].SessionID, &pluginruntime.SessionLifecycle{
		ExpiresAt: &expiredAt,
	})
	_, err = prov.Execute(context.Background(), "indexeddb_roundtrip", map[string]any{
		"store": "tasks",
		"id":    "task-2",
		"value": "expired-session",
	}, "")
	if err == nil || !strings.Contains(err.Error(), "invalid-host-service-relay-session") {
		t.Fatalf("Execute indexeddb_roundtrip after runtime expiry error = %v, want relay session rejection", err)
	}
}

func TestPluginRuntimeConfigUsesPublicCacheRelayWithoutHostServiceTunnelCapability(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	runtimeProvider := newCapturingBundlePluginRuntime()
	runtimeProvider.support.EgressMode = pluginruntime.EgressModeHostname
	runtimeProvider.fakeHosted = true
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultHostedProvider: "hosted"},
			Egress:  config.EgressConfig{DefaultAction: string(egress.PolicyDeny)},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Execution:            hostedExecutionConfig(&config.HostedRuntimeConfig{}),
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
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)

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
		if err := json.Unmarshal([]byte(result.Body), &env); err != nil {
			t.Fatalf("unmarshal env result for %s: %v", envName, err)
		}
		return env.Value, env.Found
	}

	if _, found := checkEnv(cacheservice.DefaultSocketEnv); found {
		t.Fatalf("env %s should not be set with multiple cache bindings", cacheservice.DefaultSocketEnv)
	}
	for _, binding := range []string{"session", "rate_limit"} {
		envName := cacheservice.SocketEnv(binding)
		if got, found := checkEnv(envName); !found || got != "tls://gestalt.example.test:443" {
			t.Fatalf("plugin cache env %s = (%q, %v), want (%q, true)", envName, got, found, "tls://gestalt.example.test:443")
		}
		tokenEnvName := cacheservice.SocketTokenEnv(binding)
		if got, found := checkEnv(tokenEnvName); !found || got == "" {
			t.Fatalf("plugin cache token env %s = (%q, %v), want non-empty token", tokenEnvName, got, found)
		}
	}

	startRequests := runtimeProvider.startPluginRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("StartPlugin requests = %d, want 1", len(startRequests))
	}
	for _, binding := range []string{"session", "rate_limit"} {
		assertStartPluginRelayEnv(t, startRequests[0], cacheservice.SocketEnv(binding))
	}
	if allowedHosts := slices.Clone(startRequests[0].Egress.AllowedHosts); !slices.Contains(allowedHosts, "gestalt.example.test") {
		t.Fatalf("StartPlugin allowed hosts = %#v, want relay host gestalt.example.test", allowedHosts)
	}
}

func TestPluginRuntimePublicCacheRelayRoundTripsThroughHostedPlugin(t *testing.T) {
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
	runtimeProvider := newCapturingBundlePluginRuntime()
	runtimeProvider.support.EgressMode = pluginruntime.EgressModeHostname
	runtimeProvider.fakeHosted = true

	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}

	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultHostedProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {Driver: config.RuntimeProviderDriver("capture")},
			},
		},
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Execution:            hostedExecutionConfig(&config.HostedRuntimeConfig{}),
				Cache:                []string{"session"},
			},
		},
	}

	boundCache := coretesting.NewStubCache()
	deps := Deps{
		BaseURL:       relaySrv.URL,
		EncryptionKey: secret,
		CacheDefs: map[string]*config.ProviderEntry{
			"session": {Config: mustNode(t, map[string]any{"namespace": "session"})},
		},
		CacheFactory: func(yaml.Node) (corecache.Cache, error) {
			return boundCache, nil
		},
		PublicHostServices: publicHostServices,
	}
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)

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
	if err := json.Unmarshal([]byte(result.Body), &record); err != nil {
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

	startRequests := runtimeProvider.startPluginRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("StartPlugin requests = %d, want 1", len(startRequests))
	}
	assertStartPluginRelayEnv(t, startRequests[0], cacheservice.DefaultSocketEnv)
}

func TestPluginRuntimePublicS3RelayRoundTripsThroughHostedPlugin(t *testing.T) {
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
					{Name: "bucket", Type: "string", Required: true},
					{Name: "key", Type: "string", Required: true},
					{Name: "value", Type: "string", Required: true},
				},
			},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	runtimeProvider := newCapturingBundlePluginRuntime()
	runtimeProvider.support.EgressMode = pluginruntime.EgressModeHostname
	runtimeProvider.fakeHosted = true

	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}

	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultHostedProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {Driver: config.RuntimeProviderDriver("capture")},
			},
		},
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Execution:            hostedExecutionConfig(&config.HostedRuntimeConfig{}),
				S3:                   []string{"main"},
			},
		},
	}

	boundS3 := &coretesting.StubS3{}
	deps := Deps{
		BaseURL:       relaySrv.URL,
		EncryptionKey: secret,
		S3: map[string]s3store.Client{
			"main": boundS3,
		},
		PublicHostServices: publicHostServices,
	}
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)

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
		"bucket": "assets",
		"key":    "plans/q3.txt",
		"value":  "ship-it",
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
	if err := json.Unmarshal([]byte(result.Body), &body); err != nil {
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

	if _, err := boundS3.HeadObject(context.Background(), s3store.ObjectRef{
		Bucket: "assets",
		Key:    testPluginS3NamespacePrefix("echoext") + "plans/q3.txt",
	}); err != nil {
		t.Fatalf("expected namespaced backing key: %v", err)
	}

	startRequests := runtimeProvider.startPluginRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("StartPlugin requests = %d, want 1", len(startRequests))
	}
	assertStartPluginRelayEnv(t, startRequests[0], s3service.DefaultSocketEnv)
	assertStartPluginRelayEnv(t, startRequests[0], s3service.SocketEnv("main"))
}

func TestPluginRuntimePublicPluginInvokerRelayRoundTripsThroughHostedPlugin(t *testing.T) {
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

	runtimeProvider := newCapturingBundlePluginRuntime()
	runtimeProvider.support.EgressMode = pluginruntime.EgressModeHostname
	runtimeProvider.fakeHosted = true

	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}

	bridge := newLazyInvoker()
	callerInvokes := []config.PluginInvocationDependency{
		{Plugin: "example", Operation: "request_context"},
	}
	relaySrv := httptest.NewUnstartedServer(newRuntimeRelayTestHandler(t, secret, publicHostServices))
	relaySrv.EnableHTTP2 = true
	relaySrv.StartTLS()
	testutil.CloseOnCleanup(t, relaySrv)

	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultHostedProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {Driver: config.RuntimeProviderDriver("capture")},
			},
		},
		Plugins: map[string]*config.ProviderEntry{
			"caller": {
				Command:              callerBin,
				Args:                 []string{"provider"},
				ResolvedManifest:     callerManifest,
				ResolvedManifestPath: filepath.Join(callerRoot, "manifest.yaml"),
				Execution:            hostedExecutionConfig(&config.HostedRuntimeConfig{}),
				Invokes:              callerInvokes,
			},
			"example": {
				Command:              exampleBin,
				ResolvedManifest:     exampleManifest,
				ResolvedManifestPath: filepath.Join(exampleRoot, "manifest.yaml"),
				Invokes: []config.PluginInvocationDependency{
					{Plugin: "example", Operation: "request_context"},
				},
				Config: mustNode(t, map[string]any{
					"greeting": "Hello from relay invoke",
				}),
			},
		},
	}

	deps := Deps{
		BaseURL:            relaySrv.URL,
		EncryptionKey:      secret,
		PluginInvoker:      bridge,
		PublicHostServices: publicHostServices,
	}
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)

	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })
	assertPublicHostServicesVerified(t, publicHostServices, "plugin_invoker", plugininvokerservice.DefaultSocketEnv)

	services, err := coredata.New(&coretesting.StubIndexedDB{})
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	coretesting.AttachStubExternalCredentials(services)
	t.Cleanup(func() { _ = services.Close() })

	broker := invocation.NewBroker(providers, services.Users, services.ExternalCredentials)
	guarded := invocation.NewGuarded(broker, nil, "plugin", nil, invocation.WithoutRateLimit())
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
			Source:      principal.SourceSession,
			DisplayName: "Runtime Relay",
			Scopes:      []string{"caller", "example"},
		},
		"caller",
		"default",
		"invoke_plugin",
		map[string]any{
			"plugin":    "example",
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
	if err := json.Unmarshal([]byte(result.Body), &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if !got.OK {
		t.Fatalf("invoke_plugin returned error envelope: %+v", got)
	}
	if got.TargetPlugin != "example" || got.TargetOperation != "request_context" {
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

	startRequests := runtimeProvider.startPluginRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("StartPlugin requests = %d, want 1", len(startRequests))
	}
	assertStartPluginRelayEnv(t, startRequests[0], plugininvokerservice.DefaultSocketEnv)
	if allowedHosts := slices.Clone(startRequests[0].Egress.AllowedHosts); len(allowedHosts) != 0 {
		t.Fatalf("StartPlugin allowed hosts = %#v, want none when hostname egress enforcement is not required", allowedHosts)
	}
}

func TestPluginRuntimeConfigUsesPublicWorkflowManagerRelayWithoutHostServiceTunnelCapability(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	runtimeProvider := newCapturingBundlePluginRuntime()
	runtimeProvider.support.EgressMode = pluginruntime.EgressModeHostname
	runtimeProvider.fakeHosted = true
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultHostedProvider: "hosted"},
			Egress:  config.EgressConfig{DefaultAction: string(egress.PolicyDeny)},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Execution:            hostedExecutionConfig(&config.HostedRuntimeConfig{}),
			},
		},
	}

	deps := Deps{
		BaseURL:         "https://gestalt.example.test",
		EncryptionKey:   []byte("0123456789abcdef0123456789abcdef"),
		Egress:          newEgressDeps(cfg),
		WorkflowManager: newStubWorkflowManager(),
	}
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)

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
		if err := json.Unmarshal([]byte(result.Body), &env); err != nil {
			t.Fatalf("unmarshal env result for %s: %v", envName, err)
		}
		return env.Value, env.Found
	}

	if got, found := checkEnv(workflowservice.DefaultManagerSocketEnv); !found || got != "tls://gestalt.example.test:443" {
		t.Fatalf("plugin workflow manager env %s = (%q, %v), want (%q, true)", workflowservice.DefaultManagerSocketEnv, got, found, "tls://gestalt.example.test:443")
	}
	if got, found := checkEnv(workflowservice.ManagerSocketTokenEnv()); !found || got == "" {
		t.Fatalf("plugin workflow manager token env %s = (%q, %v), want non-empty token", workflowservice.ManagerSocketTokenEnv(), got, found)
	}

	startRequests := runtimeProvider.startPluginRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("StartPlugin requests = %d, want 1", len(startRequests))
	}
	assertStartPluginRelayEnv(t, startRequests[0], workflowservice.DefaultManagerSocketEnv)
	if allowedHosts := slices.Clone(startRequests[0].Egress.AllowedHosts); !slices.Contains(allowedHosts, "gestalt.example.test") {
		t.Fatalf("StartPlugin allowed hosts = %#v, want relay host gestalt.example.test", allowedHosts)
	}
}

func TestPluginRuntimeConfigRejectsMissingHostnameEgressCapability(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	runtimeProvider := &staticCapabilityPluginRuntime{
		inner: pluginruntime.NewLocalProvider(),
		support: pluginruntime.Support{
			CanHostPlugins: true,
		},
	}
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultHostedProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Execution:            &config.ExecutionConfig{Mode: config.ExecutionModeHosted},
				Egress:               &config.ProviderEgressConfig{AllowedHosts: []string{"api.github.com"}},
			},
		},
	}

	_, _, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{
		PluginRuntimeRegistry: newPluginRuntimeRegistry(cfg, factories.Runtime, Deps{}),
	})
	if err == nil || !strings.Contains(err.Error(), "cannot preserve hostname-based egress required by this provider") {
		t.Fatalf("buildProvidersStrict error = %v, want hostname-based egress requirement failure", err)
	}
}

func TestPluginRuntimeConfigRejectsMissingHostServiceAccess(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	runtimeProvider := &staticCapabilityPluginRuntime{
		inner: pluginruntime.NewLocalProvider(),
		support: pluginruntime.Support{
			CanHostPlugins: true,
		},
	}
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultHostedProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Execution:            hostedExecutionConfig(&config.HostedRuntimeConfig{}),
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
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)

	_, _, err := buildProvidersStrict(context.Background(), cfg, factories, deps)
	if err == nil || !strings.Contains(err.Error(), "cannot provide host service access required by this provider") {
		t.Fatalf("buildProvidersStrict error = %v, want host service access failure", err)
	}
}

func TestPluginRuntimeConfigInjectsRuntimeLogSessionAndHostService(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	runtimeProvider := newCapturingBundlePluginRuntime()
	runtimeProvider.fakeHosted = true
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultHostedProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {Driver: config.RuntimeProviderDriver("capture")},
			},
		},
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Execution:            hostedExecutionConfig(&config.HostedRuntimeConfig{}),
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
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)

	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })

	startRequests := runtimeProvider.startPluginRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("StartPlugin requests = %d, want 1", len(startRequests))
	}
	if got := startRequests[0].Env[runtimehost.DefaultRuntimeSessionIDEnv]; got != startRequests[0].SessionID {
		t.Fatalf("StartPlugin %s = %q, want session id %q", runtimehost.DefaultRuntimeSessionIDEnv, got, startRequests[0].SessionID)
	}
	if got := startRequests[0].Env[runtimehost.DefaultRuntimeLogHostSocketEnv]; got != "tls://gestalt.example.test:443" {
		t.Fatalf("runtime log host relay target = %q, want public relay target", got)
	}
	if got := startRequests[0].Env[runtimehost.DefaultRuntimeLogHostSocketEnv+"_TOKEN"]; got == "" {
		t.Fatalf("StartPlugin env missing %s_TOKEN", runtimehost.DefaultRuntimeLogHostSocketEnv)
	}
}

func assertPublicHostServicesVerified(t *testing.T, registry *runtimehost.PublicHostServiceRegistry, serviceName, envVar string) {
	t.Helper()

	if registry == nil {
		t.Fatalf("public host services registry is nil, want %s/%s verifier entry", serviceName, envVar)
	}
	found := false
	for _, service := range registry.Services() {
		if strings.TrimSpace(service.Service.Name) != strings.TrimSpace(serviceName) {
			continue
		}
		if strings.TrimSpace(service.Service.EnvVar) != strings.TrimSpace(envVar) {
			continue
		}
		found = true
		if service.SessionVerifier == nil {
			t.Fatalf("public host services = %#v, want %s/%s verifier entry", registry.Services(), serviceName, envVar)
		}
	}
	if !found {
		t.Fatalf("public host services = %#v, want %s/%s verifier entry", registry.Services(), serviceName, envVar)
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
		handler, err := runtimeRelayPublicHostServiceHandler(r.Context(), publicHostServices, target)
		if err != nil {
			writeRuntimeRelayGRPCTrailersOnly(w, codes.Unauthenticated, "invalid-host-service-relay-session")
			return
		}
		if handler == nil {
			writeRuntimeRelayGRPCTrailersOnly(w, codes.Unavailable, "host-service-relay-unavailable")
			return
		}
		relayReq := r.Clone(r.Context())
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
	if strings.TrimSpace(deps.BaseURL) == "" {
		srv := newRuntimePublicEndpointTestServer(t, deps.EncryptionKey, deps.PublicHostServices)
		deps.BaseURL = srv.URL
		if cert := srv.Certificate(); cert != nil && strings.TrimSpace(deps.HostServiceTLSCAFile) == "" && strings.TrimSpace(deps.HostServiceTLSCAPEM) == "" {
			deps.HostServiceTLSCAPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}))
		}
	}
	return deps
}

func runtimeRelayPublicHostServiceHandler(ctx context.Context, registry *runtimehost.PublicHostServiceRegistry, target runtimehost.HostServiceRelayTarget) (http.Handler, error) {
	if registry == nil {
		return nil, nil
	}
	for _, entry := range registry.Services() {
		if strings.TrimSpace(entry.PluginName) != strings.TrimSpace(target.PluginName) {
			continue
		}
		if strings.TrimSpace(entry.Service.Name) != strings.TrimSpace(target.Service) {
			continue
		}
		if strings.TrimSpace(entry.Service.EnvVar) != strings.TrimSpace(target.EnvVar) {
			continue
		}
		if entry.Service.Register == nil {
			continue
		}
		if entry.SessionVerifier == nil {
			return nil, fmt.Errorf("public host service %s/%s/%s requires a session verifier", strings.TrimSpace(target.PluginName), strings.TrimSpace(target.Service), strings.TrimSpace(target.EnvVar))
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

func TestPluginRuntimePublicWorkflowManagerRelayRoundTripsThroughHostedPlugin(t *testing.T) {
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
	runtimeProvider := newCapturingBundlePluginRuntime()
	runtimeProvider.support.EgressMode = pluginruntime.EgressModeHostname
	runtimeProvider.fakeHosted = true
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultHostedProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Execution:            hostedExecutionConfig(&config.HostedRuntimeConfig{}),
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
	}
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)

	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })
	assertPublicHostServicesVerified(t, publicHostServices, "workflow_manager", workflowservice.DefaultManagerSocketEnv)

	prov, err := providers.Get("echoext")
	if err != nil {
		t.Fatalf("providers.Get: %v", err)
	}

	ctx := principal.WithPrincipal(context.Background(), &principal.Principal{
		SubjectID: "user:user-123",
		UserID:    "user-123",
		Kind:      principal.KindUser,
		Source:    principal.SourceSession,
		Scopes:    []string{"echoext"},
	})

	result, err := prov.Execute(ctx, "workflow_manager_roundtrip", nil, "")
	if err != nil {
		t.Fatalf("Execute workflow_manager_roundtrip: %v", err)
	}

	var body struct {
		ProviderName string `json:"provider_name"`
		ScheduleID   string `json:"schedule_id"`
		Cron         string `json:"cron"`
		Operation    string `json:"operation"`
	}
	if err := json.Unmarshal([]byte(result.Body), &body); err != nil {
		t.Fatalf("unmarshal workflow_manager_roundtrip: %v", err)
	}
	if body.ProviderName != "managed" {
		t.Fatalf("provider_name = %q, want %q", body.ProviderName, "managed")
	}
	if body.ScheduleID == "" {
		t.Fatal("workflow_manager_roundtrip should return a schedule id")
	}
	if body.Cron != "*/5 * * * *" {
		t.Fatalf("cron = %q, want %q", body.Cron, "*/5 * * * *")
	}
	if body.Operation != "sync" {
		t.Fatalf("operation = %q, want %q", body.Operation, "sync")
	}

	schedules, err := manager.ListSchedules(context.Background(), nil)
	if err != nil {
		t.Fatalf("manager.ListSchedules: %v", err)
	}
	if len(schedules) != 1 {
		t.Fatalf("manager schedules len = %d, want 1", len(schedules))
	}
	scheduleTarget := schedules[0].Schedule.Target.Plugin
	if scheduleTarget == nil {
		t.Fatalf("stored target plugin is nil: %#v", schedules[0].Schedule.Target)
		return
	}
	if got := scheduleTarget.Operation; got != "sync" {
		t.Fatalf("stored target operation = %q, want %q", got, "sync")
	}
	if got := manager.Subjects(); !slices.Equal(got, []string{"user:user-123", "user:user-123"}) {
		t.Fatalf("manager subjects = %v, want two user:user-123 entries", got)
	}
	if got := manager.ScheduleIdempotencyKeys(); !slices.Equal(got, []string{"workflow-manager-roundtrip"}) {
		t.Fatalf("manager schedule idempotency keys = %v, want [workflow-manager-roundtrip]", got)
	}

	startRequests := runtimeProvider.startPluginRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("StartPlugin requests = %d, want 1", len(startRequests))
	}
	assertStartPluginRelayEnv(t, startRequests[0], workflowservice.DefaultManagerSocketEnv)
}

func TestPluginRuntimePublicAuthorizationRelayRoundTripsThroughHostedPlugin(t *testing.T) {
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
			{ID: "authorization_roundtrip", Method: http.MethodPost},
		},
	})
	manifest := newExecutableManifest("Echo", "Authorization relay roundtrip")
	manifest.Spec.SecuritySchemes = map[string]*providermanifestv1.HTTPSecurityScheme{
		"public": {
			Type: providermanifestv1.HTTPSecuritySchemeTypeNone,
		},
	}
	manifest.Spec.HTTP = map[string]*providermanifestv1.HTTPBinding{
		"command": {
			Path:     "/command",
			Method:   http.MethodPost,
			Security: "public",
			Target:   "authorization_roundtrip",
			RequestBody: &providermanifestv1.HTTPRequestBody{
				Content: map[string]*providermanifestv1.HTTPMediaType{
					"application/x-www-form-urlencoded": {},
				},
			},
		},
	}
	runtimeProvider := newCapturingBundlePluginRuntime()
	runtimeProvider.support.EgressMode = pluginruntime.EgressModeHostname
	runtimeProvider.fakeHosted = true
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultHostedProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Execution:            hostedExecutionConfig(&config.HostedRuntimeConfig{}),
			},
		},
	}

	authz := &recordingHostedAuthorizationProvider{}
	deps := Deps{
		BaseURL:               relaySrv.URL,
		EncryptionKey:         secret,
		AuthorizationProvider: authz,
		PublicHostServices:    publicHostServices,
	}
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)

	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })

	prov, err := providers.Get("echoext")
	if err != nil {
		t.Fatalf("providers.Get: %v", err)
	}

	result, err := prov.Execute(context.Background(), "authorization_roundtrip", nil, "")
	if err != nil {
		t.Fatalf("Execute authorization_roundtrip: %v", err)
	}

	var body struct {
		ModelID      string   `json:"model_id"`
		SubjectID    string   `json:"subject_id"`
		SubjectType  string   `json:"subject_type"`
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal([]byte(result.Body), &body); err != nil {
		t.Fatalf("unmarshal authorization_roundtrip: %v", err)
	}
	if body.ModelID != "authz-model-1" {
		t.Fatalf("model_id = %q, want %q", body.ModelID, "authz-model-1")
	}
	if body.SubjectID != "user:user-123" {
		t.Fatalf("subject_id = %q, want %q", body.SubjectID, "user:user-123")
	}
	if body.SubjectType != "user" {
		t.Fatalf("subject_type = %q, want %q", body.SubjectType, "user")
	}
	if !slices.Equal(body.Capabilities, []string{"search_subjects"}) {
		t.Fatalf("capabilities = %#v, want [search_subjects]", body.Capabilities)
	}

	if got := authz.Calls(); len(got) != 1 {
		t.Fatalf("authorization search calls = %d, want 1", len(got))
	} else {
		if got[0].SubjectType != "user" {
			t.Fatalf("subject type = %q, want %q", got[0].SubjectType, "user")
		}
		if got[0].ResourceType != "slack_identity" || got[0].ResourceID != "team:T123:user:U456" {
			t.Fatalf("resource = (%q, %q), want (%q, %q)", got[0].ResourceType, got[0].ResourceID, "slack_identity", "team:T123:user:U456")
		}
		if got[0].ActionName != "assume" {
			t.Fatalf("action name = %q, want %q", got[0].ActionName, "assume")
		}
		if got[0].PageSize != 1 {
			t.Fatalf("page size = %d, want 1", got[0].PageSize)
		}
	}

	startRequests := runtimeProvider.startPluginRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("StartPlugin requests = %d, want 1", len(startRequests))
	}
	assertStartPluginRelayEnv(t, startRequests[0], authorizationservice.DefaultSocketEnv)
}

func TestPluginRuntimeConfigInjectsPublicEgressProxyWithoutHostServiceTunnelCapability(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	runtimeProvider := newCapturingBundlePluginRuntime()
	runtimeProvider.support.EgressMode = pluginruntime.EgressModeHostname
	runtimeProvider.fakeHosted = true
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultHostedProvider: "hosted"},
			Egress:  config.EgressConfig{DefaultAction: string(egress.PolicyDeny)},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Execution:            hostedExecutionConfig(&config.HostedRuntimeConfig{}),
				Egress:               &config.ProviderEgressConfig{AllowedHosts: []string{"api.github.com"}},
			},
		},
	}
	deps := Deps{
		BaseURL:       "https://gestalt.example.test",
		EncryptionKey: []byte("0123456789abcdef0123456789abcdef"),
		Egress:        EgressDeps{DefaultAction: egress.PolicyDeny},
	}
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)

	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })

	startRequests := runtimeProvider.startPluginRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("StartPlugin requests = %d, want 1", len(startRequests))
	}
	httpProxy := startRequests[0].Env["HTTP_PROXY"]
	httpsProxy := startRequests[0].Env["HTTPS_PROXY"]
	if httpProxy == "" {
		t.Fatal("StartPlugin env should include HTTP_PROXY")
	}
	if httpsProxy == "" {
		t.Fatal("StartPlugin env should include HTTPS_PROXY")
	}
	if httpProxy != httpsProxy {
		t.Fatalf("HTTP_PROXY = %q, HTTPS_PROXY = %q, want matching values", httpProxy, httpsProxy)
	}
	parsed, err := url.Parse(httpProxy)
	if err != nil {
		t.Fatalf("parse HTTP_PROXY: %v", err)
	}
	if parsed.Scheme != "https" {
		t.Fatalf("HTTP_PROXY scheme = %q, want https", parsed.Scheme)
	}
	if parsed.Host != "gestalt.example.test" {
		t.Fatalf("HTTP_PROXY host = %q, want gestalt.example.test", parsed.Host)
	}
	if parsed.User == nil {
		t.Fatal("HTTP_PROXY should include relay credentials")
	}
	assertStartPluginEgressPolicy(t, startRequests[0], []string{"api.github.com"}, pluginruntime.PolicyDeny)
}

func TestPluginRuntimeConfigSkipsPublicEgressProxyWhenHostnameEgressIsNotRequired(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	runtimeProvider := newCapturingBundlePluginRuntime()
	runtimeProvider.support.EgressMode = pluginruntime.EgressModeHostname
	runtimeProvider.fakeHosted = true
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultHostedProvider: "hosted"},
			Egress:  config.EgressConfig{DefaultAction: string(egress.PolicyAllow)},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Execution:            hostedExecutionConfig(&config.HostedRuntimeConfig{}),
			},
		},
	}
	deps := Deps{
		BaseURL:       "https://gestalt.example.test",
		EncryptionKey: []byte("0123456789abcdef0123456789abcdef"),
		Egress:        EgressDeps{DefaultAction: egress.PolicyAllow},
	}
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)

	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })

	startRequests := runtimeProvider.startPluginRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("StartPlugin requests = %d, want 1", len(startRequests))
	}
	if got := startRequests[0].Env["HTTP_PROXY"]; got != "" {
		t.Fatalf("StartPlugin HTTP_PROXY = %q, want empty when hostname egress is not required", got)
	}
	if got := startRequests[0].Env["HTTPS_PROXY"]; got != "" {
		t.Fatalf("StartPlugin HTTPS_PROXY = %q, want empty when hostname egress is not required", got)
	}
}

func TestPluginRuntimeConfigUsesPublicRelayAndEgressProxyWhenHostCanRelay(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	runtimeProvider := newCapturingBundlePluginRuntime()
	runtimeProvider.support.EgressMode = pluginruntime.EgressModeHostname
	runtimeProvider.fakeHosted = true
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultHostedProvider: "hosted"},
			Egress:  config.EgressConfig{DefaultAction: string(egress.PolicyDeny)},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Execution:            hostedExecutionConfig(&config.HostedRuntimeConfig{}),
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
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)

	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })

	startRequests := runtimeProvider.startPluginRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("StartPlugin requests = %d, want 1", len(startRequests))
	}
	if got := startRequests[0].Env["HTTP_PROXY"]; !strings.Contains(got, "@gestalt.example.test") {
		t.Fatalf("StartPlugin HTTP_PROXY = %q, want public egress proxy on gestalt.example.test", got)
	}
	if got := startRequests[0].Env["HTTPS_PROXY"]; !strings.Contains(got, "@gestalt.example.test") {
		t.Fatalf("StartPlugin HTTPS_PROXY = %q, want public egress proxy on gestalt.example.test", got)
	}
	assertStartPluginRelayEnv(t, startRequests[0], cacheservice.SocketEnv("session"))
	assertStartPluginEgressPolicy(t, startRequests[0], []string{"api.github.com", "gestalt.example.test"}, pluginruntime.PolicyDeny)
}

func TestPluginRuntimePublicEgressProxyRoundTripsThroughHostedPlugin(t *testing.T) {
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
	runtimeProvider := newCapturingBundlePluginRuntime()
	runtimeProvider.support.EgressMode = pluginruntime.EgressModeHostname
	runtimeProvider.fakeHosted = true
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultHostedProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Execution:            hostedExecutionConfig(&config.HostedRuntimeConfig{}),
				Egress:               &config.ProviderEgressConfig{AllowedHosts: []string{"127.0.0.1", "localhost"}},
			},
		},
	}
	deps := Deps{
		BaseURL:       proxySrv.URL,
		EncryptionKey: secret,
	}
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)

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
	if err := json.Unmarshal([]byte(result.Body), &body); err != nil {
		t.Fatalf("unmarshal make_http_request: %v", err)
	}
	if body.Status != http.StatusOK {
		t.Fatalf("result status = %d, want %d (body=%s)", body.Status, http.StatusOK, body.Body)
	}
	if body.Body != "egress-proxy-ok" {
		t.Fatalf("result body = %q, want %q", body.Body, "egress-proxy-ok")
	}

	startRequests := runtimeProvider.startPluginRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("StartPlugin requests = %d, want 1", len(startRequests))
	}
	if got := startRequests[0].Env["HTTP_PROXY"]; got == "" {
		t.Fatal("StartPlugin env should include HTTP_PROXY")
	}
	if got := startRequests[0].Env["HTTPS_PROXY"]; got == "" {
		t.Fatal("StartPlugin env should include HTTPS_PROXY")
	}
}

func TestPluginRuntimeConfigRejectsDefaultDenyWithoutHostnameEgressCapability(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	runtimeProvider := &staticCapabilityPluginRuntime{
		inner: pluginruntime.NewLocalProvider(),
		support: pluginruntime.Support{
			CanHostPlugins: true,
		},
	}
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultHostedProvider: "hosted"},
			Egress:  config.EgressConfig{DefaultAction: string(egress.PolicyDeny)},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Execution:            hostedExecutionConfig(&config.HostedRuntimeConfig{}),
			},
		},
	}

	_, _, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{
		PluginRuntimeRegistry: newPluginRuntimeRegistry(cfg, factories.Runtime, Deps{
			Egress: EgressDeps{DefaultAction: egress.PolicyDeny},
		}),
		Egress: EgressDeps{DefaultAction: egress.PolicyDeny},
	})
	if err == nil || !strings.Contains(err.Error(), "cannot preserve hostname-based egress required by this provider") {
		t.Fatalf("buildProvidersStrict error = %v, want hostname-based egress requirement failure", err)
	}
}

func TestPluginCacheBindingsRejectUnknownCaches(t *testing.T) {
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
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				Cache:                []string{"missing"},
			},
		},
	}

	_, _, err := buildProvidersStrict(context.Background(), cfg, NewFactoryRegistry(), testRuntimePublicEndpointDeps(t, Deps{
		CacheDefs: map[string]*config.ProviderEntry{
			"session": {
				Config: mustNode(t, map[string]any{"namespace": "session"}),
			},
		},
		CacheFactory: func(yaml.Node) (corecache.Cache, error) {
			return coretesting.NewStubCache(), nil
		},
	}))
	if err == nil {
		t.Fatal("buildProvidersStrict: expected error, got nil")
	}
	if !strings.Contains(err.Error(), `cache "missing" is not available`) {
		t.Fatalf("unexpected error: %v", err)
	}
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
		indexedDB *config.HostIndexedDBBindingConfig
	}{
		{name: "omitted indexeddb inherits host selection"},
		{name: "empty indexeddb inherits host selection", indexedDB: &config.HostIndexedDBBindingConfig{}},
		{name: "objectStores-only indexeddb inherits host selection", indexedDB: &config.HostIndexedDBBindingConfig{ObjectStores: []string{"tasks"}}},
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
				var runtimeProvider *capturingPluginRuntime
				deps := Deps{
					SelectedIndexedDBName: "memory",
					IndexedDBDefs: map[string]*config.ProviderEntry{
						"memory": {
							Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.1/provider-release.yaml"),
							Config: mustNode(t, map[string]any{"bucket": "plugin-state"}),
						},
					},
					IndexedDBFactory: func(yaml.Node) (indexeddb.IndexedDB, error) {
						return boundDB, nil
					},
				}
				if runtimeMode.hosted {
					runtimeProvider = newCapturingPluginRuntime()
					deps.PluginRuntime = runtimeProvider
					t.Cleanup(func() { _ = runtimeProvider.Close() })
				}

				providers, _, err := buildProvidersStrict(context.Background(), &config.Config{
					Plugins: map[string]*config.ProviderEntry{
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
				if err := json.Unmarshal([]byte(result.Body), &record); err != nil {
					t.Fatalf("unmarshal record: %v", err)
				}
				if got := record["value"]; got != "ship-it" {
					t.Fatalf("record value = %#v, want %q", got, "ship-it")
				}
				if _, err := boundDB.ObjectStore("tasks").Get(context.Background(), "task-1"); err != nil {
					t.Fatalf("inherited host indexeddb should expose logical store name directly: %v", err)
				}
				if runtimeProvider != nil {
					startRequests := runtimeProvider.startPluginRequestsCopy()
					if len(startRequests) != 1 {
						t.Fatalf("StartPlugin requests = %d, want 1", len(startRequests))
					}
					assertStartPluginRelayEnv(t, startRequests[0], indexeddbservice.DefaultSocketEnv)
				}
			})
		}
	}
}

func TestPluginIndexedDBBuildScopedConfig(t *testing.T) {
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

	makeConfig := func(indexedDB *config.HostIndexedDBBindingConfig) *config.Config {
		return &config.Config{
			Plugins: map[string]*config.ProviderEntry{
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
			Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.1/provider-release.yaml"),
			Config: mustNode(t, map[string]any{
				"dsn":    "postgres://db.example.test/gestalt",
				"schema": "host_schema",
			}),
		},
		"sqlite": {
			Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.1/provider-release.yaml"),
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
			Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.1/provider-release.yaml"),
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
		indexedDB  *config.HostIndexedDBBindingConfig
		wantDSN    string
		wantDB     string
		wantSQLite bool
		wantSecret bool
	}{
		{
			name:      "defaults db to plugin name for postgres",
			indexedDB: &config.HostIndexedDBBindingConfig{Provider: "postgres"},
			wantDSN:   "postgres://db.example.test/gestalt",
			wantDB:    "echoext",
		},
		{
			name:      "uses db override for postgres",
			indexedDB: &config.HostIndexedDBBindingConfig{Provider: "postgres", DB: "roadmap_state"},
			wantDSN:   "postgres://db.example.test/gestalt",
			wantDB:    "roadmap_state",
		},
		{
			name:       "uses db override for sqlite table prefixes",
			indexedDB:  &config.HostIndexedDBBindingConfig{Provider: "sqlite", DB: "roadmap_state"},
			wantDSN:    "sqlite://plugin-state.db",
			wantDB:     "roadmap_state",
			wantSQLite: true,
		},
		{
			name:       "uses schema scope for secret-backed relational DSNs",
			indexedDB:  &config.HostIndexedDBBindingConfig{Provider: "mysql-secret", DB: "secret_state"},
			wantDB:     "secret_state",
			wantSecret: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var closeCount atomic.Int32
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
						onClose:       closeCount.Add,
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
				wantPrefix := tc.wantDB + "_"
				if got := cfg.Config["table_prefix"]; got != wantPrefix {
					t.Fatalf("sqlite table_prefix = %#v, want %q", got, wantPrefix)
				}
				if got := cfg.Config["prefix"]; got != wantPrefix {
					t.Fatalf("sqlite prefix = %#v, want %q", got, wantPrefix)
				}
				if _, ok := cfg.Config["schema"]; ok {
					t.Fatalf("sqlite schema should be removed, got %#v", cfg.Config["schema"])
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
			if got := closeCount.Load(); got != 1 {
				t.Fatalf("closeCount after provider shutdown = %d, want 1", got)
			}
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
				closeCount      atomic.Int32
				boundDB         *trackedIndexedDB
				runtimeProvider *capturingPluginRuntime
			)
			deps := Deps{
				SelectedIndexedDBName: "memory",
				IndexedDBDefs: map[string]*config.ProviderEntry{
					"memory": {
						Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.1/provider-release.yaml"),
						Config: mustNode(t, map[string]any{"bucket": "plugin-state"}),
					},
				},
				IndexedDBFactory: func(yaml.Node) (indexeddb.IndexedDB, error) {
					boundDB = &trackedIndexedDB{
						StubIndexedDB: coretesting.StubIndexedDB{},
						onClose:       closeCount.Add,
					}
					return boundDB, nil
				},
			}
			if runtimeMode.hosted {
				runtimeProvider = newCapturingPluginRuntime()
				deps.PluginRuntime = runtimeProvider
				t.Cleanup(func() { _ = runtimeProvider.Close() })
			}

			providers, _, err := buildProvidersStrict(context.Background(), &config.Config{
				Plugins: map[string]*config.ProviderEntry{
					"echoext": {
						Command:              bin,
						Args:                 []string{"provider"},
						ResolvedManifest:     manifest,
						ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
						IndexedDB: &config.HostIndexedDBBindingConfig{
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
			if err := json.Unmarshal([]byte(result.Body), &record); err != nil {
				t.Fatalf("unmarshal record: %v", err)
			}
			if got := record["value"]; got != "ship-it" {
				t.Fatalf("record value = %#v, want %q", got, "ship-it")
			}
			if _, err := boundDB.ObjectStore("tasks").Get(context.Background(), "task-1"); err != nil {
				t.Fatalf("logical backing store should contain task: %v", err)
			}

			if _, err := prov.Execute(context.Background(), "indexeddb_roundtrip", map[string]any{
				"store": "events",
				"id":    "evt-1",
				"value": "blocked",
			}, ""); err == nil {
				t.Fatal("indexeddb_roundtrip on disallowed object store should fail")
			}
			if runtimeProvider != nil {
				startRequests := runtimeProvider.startPluginRequestsCopy()
				if len(startRequests) != 1 {
					t.Fatalf("StartPlugin requests = %d, want 1", len(startRequests))
				}
				assertStartPluginRelayEnv(t, startRequests[0], indexeddbservice.DefaultSocketEnv)
			}

			_ = CloseProviders(providers)
			providers = nil
			if got := closeCount.Load(); got != 1 {
				t.Fatalf("closeCount after provider shutdown = %d, want 1", got)
			}
		})
	}
}

func TestPluginIndexedDBProviderOverrideUsesExplicitHostIndexedDB(t *testing.T) {
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
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				IndexedDB: &config.HostIndexedDBBindingConfig{
					Provider: "archive",
					DB:       "roadmap",
				},
			},
		},
	}, NewFactoryRegistry(), testRuntimePublicEndpointDeps(t, Deps{
		SelectedIndexedDBName: "main",
		IndexedDBDefs: map[string]*config.ProviderEntry{
			"main": {
				Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.1/provider-release.yaml"),
				Config: mustNode(t, map[string]any{"bucket": "main"}),
			},
			"archive": {
				Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.1/provider-release.yaml"),
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
	if err := json.Unmarshal([]byte(result.Body), &record); err != nil {
		t.Fatalf("unmarshal record: %v", err)
	}
	if got := record["value"]; got != "stored" {
		t.Fatalf("record value = %#v, want %q", got, "stored")
	}
	if len(boundDBs) != 1 {
		t.Fatalf("boundDBs = %d, want 1 explicit provider build", len(boundDBs))
	}
	if _, ok := boundDBs["main"]; ok {
		t.Fatal("main indexeddb should not be rebuilt when plugin explicitly selects archive")
	}
	if _, err := boundDBs["archive"].ObjectStore("events").Get(context.Background(), "evt-1"); err != nil {
		t.Fatalf("archive backing store should contain event: %v", err)
	}
}

func TestPluginIndexedDBBindingsCleanupOnS3BindingFailure(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")

	var closeCount atomic.Int32
	_, _, err := buildProvidersStrict(context.Background(), &config.Config{
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				IndexedDB:            &config.HostIndexedDBBindingConfig{Provider: "main"},
				S3:                   []string{"missing"},
			},
		},
	}, NewFactoryRegistry(), testRuntimePublicEndpointDeps(t, Deps{
		SelectedIndexedDBName: "main",
		IndexedDBDefs: map[string]*config.ProviderEntry{
			"main": {
				Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.1/provider-release.yaml"),
				Config: mustNode(t, map[string]any{"bucket": "main"}),
			},
		},
		IndexedDBFactory: func(yaml.Node) (indexeddb.IndexedDB, error) {
			return &trackedIndexedDB{
				StubIndexedDB: coretesting.StubIndexedDB{},
				onClose:       closeCount.Add,
			}, nil
		},
		S3: map[string]s3store.Client{},
	}))
	if err == nil {
		t.Fatal("expected buildProvidersStrict to fail for missing S3 binding")
	}
	if got := closeCount.Load(); got != 1 {
		t.Fatalf("closeCount after S3 binding failure = %d, want 1", got)
	}
}

func TestPluginS3BindingsRoundtripAndNamespaceKeys(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{
				ID:     "s3_roundtrip",
				Method: http.MethodPost,
				Parameters: []catalog.CatalogParameter{
					{Name: "bucket", Type: "string", Required: true},
					{Name: "key", Type: "string", Required: true},
					{Name: "value", Type: "string", Required: true},
				},
			},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")

	stubS3 := &coretesting.StubS3{}
	providers, _, err := buildProvidersStrict(context.Background(), &config.Config{
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				S3:                   []string{"main"},
			},
		},
	}, NewFactoryRegistry(), testRuntimePublicEndpointDeps(t, Deps{
		Services: coretesting.NewStubServices(t),
		S3: map[string]s3store.Client{
			"main": stubS3,
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
		"bucket": "assets",
		"key":    "plans/q1.txt",
		"value":  "ship-it",
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
	if err := json.Unmarshal([]byte(result.Body), &body); err != nil {
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

	if _, err := stubS3.HeadObject(context.Background(), s3store.ObjectRef{
		Bucket: "assets",
		Key:    testPluginS3NamespacePrefix("echoext") + "plans/q1.txt",
	}); err != nil {
		t.Fatalf("expected namespaced backing key: %v", err)
	}
	if _, err := stubS3.HeadObject(context.Background(), s3store.ObjectRef{
		Bucket: "assets",
		Key:    "plans/q1.txt",
	}); err == nil {
		t.Fatal("unnamespaced backing key should remain empty")
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
					{Name: "bucket", Type: "string", Required: true},
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
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				S3:                   []string{"main", "archive"},
			},
		},
	}, NewFactoryRegistry(), testRuntimePublicEndpointDeps(t, Deps{
		Services: coretesting.NewStubServices(t),
		S3: map[string]s3store.Client{
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
		"bucket":  "assets",
		"key":     "plans/q2.txt",
		"value":   "ship-archive",
	}, ""); err != nil {
		t.Fatalf("Execute s3_roundtrip: %v", err)
	}

	if _, err := archiveS3.HeadObject(context.Background(), s3store.ObjectRef{
		Bucket: "assets",
		Key:    testPluginS3NamespacePrefix("echoext") + "plans/q2.txt",
	}); err != nil {
		t.Fatalf("archive binding should receive the write: %v", err)
	}
	if _, err := mainS3.HeadObject(context.Background(), s3store.ObjectRef{
		Bucket: "assets",
		Key:    testPluginS3NamespacePrefix("echoext") + "plans/q2.txt",
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
			Plugins: map[string]*config.ProviderEntry{
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

	services := coretesting.NewStubServices(t)
	s3Bindings := map[string]s3store.Client{
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
		if err := json.Unmarshal([]byte(result.Body), &env); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return env.Found && env.Value != ""
	}

	if got := checkEnv(t, nil, s3service.DefaultSocketEnv); got {
		t.Fatal("default S3 env should not be set without plugin s3 bindings")
	}
	if got := checkEnv(t, []string{"main"}, s3service.DefaultSocketEnv); !got {
		t.Fatal("default S3 env should be set with a single plugin s3 binding")
	}
	if got := checkEnv(t, []string{"main"}, s3service.SocketEnv("main")); !got {
		t.Fatal("named S3 env should be set with a single plugin s3 binding")
	}
	if got := checkEnv(t, []string{"main", "archive"}, s3service.DefaultSocketEnv); got {
		t.Fatal("default S3 env should not be set with multiple plugin s3 bindings")
	}
	if got := checkEnv(t, []string{"main", "archive"}, s3service.SocketEnv("main")); !got {
		t.Fatal(`named S3 env for "main" should be set with multiple plugin s3 bindings`)
	}
	if got := checkEnv(t, []string{"main", "archive"}, s3service.SocketEnv("archive")); !got {
		t.Fatal(`named S3 env for "archive" should be set with multiple plugin s3 bindings`)
	}
}

func testPluginS3NamespacePrefix(pluginName string) string {
	return "plugin_" + strconv.Itoa(len(pluginName)) + "_" + pluginName + "/"
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
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command: bin,
				Args:    []string{"provider"},
			},
		},
	}

	factories := NewFactoryRegistry()
	_, _, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{})
	if err == nil {
		t.Fatal("expected buildProvidersStrict to reject executable plugin without manifest")
	}
	if got := err.Error(); got != `bootstrap: provider validation failed: integration "echoext": integration "echoext" must resolve to a provider manifest` {
		t.Fatalf("unexpected error: %v", err)
	}
}
