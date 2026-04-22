package bootstrap

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
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
	corecache "github.com/valon-technologies/gestalt/server/core/cache"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	corecrypto "github.com/valon-technologies/gestalt/server/core/crypto"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	s3store "github.com/valon-technologies/gestalt/server/core/s3"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/egress"
	graphqlschema "github.com/valon-technologies/gestalt/server/internal/graphql"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/pluginruntime"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	"github.com/valon-technologies/gestalt/server/internal/providerpkg"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/internal/workflowmanager"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"golang.org/x/net/http2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
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

type capturingPluginRuntime struct {
	provider *pluginruntime.LocalProvider

	mu            sync.Mutex
	startRequests []pluginruntime.StartSessionRequest
	bindRequests  []pluginruntime.BindHostServiceRequest
	stopCount     atomic.Int32
}

func newCapturingPluginRuntime() *capturingPluginRuntime {
	return &capturingPluginRuntime{provider: pluginruntime.NewLocalProvider()}
}

func (r *capturingPluginRuntime) Capabilities(ctx context.Context) (pluginruntime.Capabilities, error) {
	return r.provider.Capabilities(ctx)
}

func (r *capturingPluginRuntime) StartSession(ctx context.Context, req pluginruntime.StartSessionRequest) (*pluginruntime.Session, error) {
	r.mu.Lock()
	r.startRequests = append(r.startRequests, pluginruntime.StartSessionRequest{
		PluginName: req.PluginName,
		Template:   req.Template,
		Image:      req.Image,
		Metadata:   cloneRuntimeMetadata(req.Metadata),
	})
	r.mu.Unlock()
	return r.provider.StartSession(ctx, req)
}

func (r *capturingPluginRuntime) ListSessions(ctx context.Context) ([]pluginruntime.Session, error) {
	return r.provider.ListSessions(ctx)
}

func (r *capturingPluginRuntime) GetSession(ctx context.Context, req pluginruntime.GetSessionRequest) (*pluginruntime.Session, error) {
	return r.provider.GetSession(ctx, req)
}

func (r *capturingPluginRuntime) StopSession(ctx context.Context, req pluginruntime.StopSessionRequest) error {
	r.stopCount.Add(1)
	return r.provider.StopSession(ctx, req)
}

func (r *capturingPluginRuntime) BindHostService(ctx context.Context, req pluginruntime.BindHostServiceRequest) (*pluginruntime.HostServiceBinding, error) {
	r.mu.Lock()
	r.bindRequests = append(r.bindRequests, cloneBindHostServiceRequest(req))
	r.mu.Unlock()
	return r.provider.BindHostService(ctx, req)
}

func (r *capturingPluginRuntime) StartPlugin(ctx context.Context, req pluginruntime.StartPluginRequest) (*pluginruntime.HostedPlugin, error) {
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
			PluginName: req.PluginName,
			Template:   req.Template,
			Image:      req.Image,
			Metadata:   cloneRuntimeMetadata(req.Metadata),
		}
	}
	return out
}

func (r *capturingPluginRuntime) bindHostServiceRequests() []pluginruntime.BindHostServiceRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]pluginruntime.BindHostServiceRequest, len(r.bindRequests))
	for i, req := range r.bindRequests {
		out[i] = cloneBindHostServiceRequest(req)
	}
	return out
}

type capturingBundlePluginRuntime struct {
	provider      *pluginruntime.LocalProvider
	capabilities  pluginruntime.Capabilities
	bindHostCalls atomic.Int32
	fakeHosted    bool

	mu                  sync.Mutex
	bindRequests        []pluginruntime.BindHostServiceRequest
	startPluginRequests []pluginruntime.StartPluginRequest
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
		capabilities: pluginruntime.Capabilities{
			HostedPluginRuntime: true,
			ProviderGRPCTunnel:  true,
			ExecutionGOOS:       runtime.GOOS,
			ExecutionGOARCH:     runtime.GOARCH,
		},
		fakePlugins: make(map[string]*fakeHostedPluginServer),
	}
}

func (r *capturingBundlePluginRuntime) Capabilities(context.Context) (pluginruntime.Capabilities, error) {
	return r.capabilities, nil
}

func (r *capturingBundlePluginRuntime) StartSession(ctx context.Context, req pluginruntime.StartSessionRequest) (*pluginruntime.Session, error) {
	return r.provider.StartSession(ctx, req)
}

func (r *capturingBundlePluginRuntime) ListSessions(ctx context.Context) ([]pluginruntime.Session, error) {
	return r.provider.ListSessions(ctx)
}

func (r *capturingBundlePluginRuntime) GetSession(ctx context.Context, req pluginruntime.GetSessionRequest) (*pluginruntime.Session, error) {
	return r.provider.GetSession(ctx, req)
}

func (r *capturingBundlePluginRuntime) StopSession(ctx context.Context, req pluginruntime.StopSessionRequest) error {
	r.cleanupFakeHostedPlugin(req.SessionID)
	return r.provider.StopSession(ctx, req)
}

func (r *capturingBundlePluginRuntime) BindHostService(ctx context.Context, req pluginruntime.BindHostServiceRequest) (*pluginruntime.HostServiceBinding, error) {
	r.bindHostCalls.Add(1)
	r.mu.Lock()
	r.bindRequests = append(r.bindRequests, cloneBindHostServiceRequest(req))
	r.mu.Unlock()
	return r.provider.BindHostService(ctx, req)
}

func (r *capturingBundlePluginRuntime) StartPlugin(ctx context.Context, req pluginruntime.StartPluginRequest) (*pluginruntime.HostedPlugin, error) {
	r.mu.Lock()
	r.startPluginRequests = append(r.startPluginRequests, pluginruntime.StartPluginRequest{
		SessionID:     req.SessionID,
		PluginName:    req.PluginName,
		Command:       req.Command,
		Args:          slices.Clone(req.Args),
		Env:           cloneRuntimeMetadata(req.Env),
		BundleDir:     req.BundleDir,
		AllowedHosts:  slices.Clone(req.AllowedHosts),
		DefaultAction: req.DefaultAction,
		HostBinary:    req.HostBinary,
	})
	r.mu.Unlock()

	translated := req
	if req.BundleDir != "" && strings.HasPrefix(req.Command, pluginruntime.HostedPluginBundleRoot+"/") {
		rel := strings.TrimPrefix(req.Command, pluginruntime.HostedPluginBundleRoot+"/")
		translated.Command = filepath.Join(req.BundleDir, filepath.FromSlash(rel))
	}
	if req.BundleDir != "" {
		translated.Args = make([]string, len(req.Args))
		for i, arg := range req.Args {
			switch {
			case arg == pluginruntime.HostedPluginBundleRoot:
				translated.Args[i] = req.BundleDir
			case strings.HasPrefix(arg, pluginruntime.HostedPluginBundleRoot+"/"):
				rel := strings.TrimPrefix(arg, pluginruntime.HostedPluginBundleRoot+"/")
				translated.Args[i] = filepath.Join(req.BundleDir, filepath.FromSlash(rel))
			default:
				translated.Args[i] = arg
			}
		}
	}
	if r.fakeHosted {
		return r.startFakeHostedPlugin(req)
	}
	return r.provider.StartPlugin(ctx, translated)
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
	r.mu.Lock()
	for _, binding := range r.bindRequests {
		if binding.SessionID != req.SessionID {
			continue
		}
		if env == nil {
			env = map[string]string{}
		}
		env[binding.EnvVar] = binding.Relay.DialTarget
	}
	r.mu.Unlock()

	dir, err := providerhost.NewPluginTempDir("gstp-fake-")
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
	proto.RegisterIntegrationProviderServer(srv, providerhost.NewProviderServer(&coretesting.StubIntegration{
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
					ID:     "invoke_plugin",
					Method: http.MethodPost,
					Parameters: []catalog.CatalogParameter{
						{Name: "plugin", Type: "string", Required: true},
						{Name: "operation", Type: "string", Required: true},
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
			case "invoke_plugin":
				targetPlugin, _ := params["plugin"].(string)
				targetOperation, _ := params["operation"].(string)
				envelope, err := fakeHostedInvokePlugin(targetPlugin, targetOperation, providerhost.InvocationTokenFromContext(ctx), env)
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
	envName := providerhost.DefaultIndexedDBSocketEnv
	tokenEnvName := providerhost.IndexedDBSocketTokenEnv("")
	if strings.TrimSpace(binding) != "" {
		envName = providerhost.IndexedDBSocketEnv(binding)
		tokenEnvName = providerhost.IndexedDBSocketTokenEnv(binding)
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
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(providerhost.HostServiceRelayTokenHeader, token))

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

func fakeHostedInvokePlugin(targetPlugin, targetOperation, invocationToken string, env map[string]string) (invokePluginEnvelope, error) {
	envelope := invokePluginEnvelope{
		OK:              false,
		TargetPlugin:    targetPlugin,
		TargetOperation: targetOperation,
	}
	target := strings.TrimSpace(env[providerhost.DefaultPluginInvokerSocketEnv])
	if target == "" {
		return envelope, fmt.Errorf("missing plugin invoker relay target in %s", providerhost.DefaultPluginInvokerSocketEnv)
	}
	token := strings.TrimSpace(env[providerhost.PluginInvokerSocketTokenEnv()])
	if token == "" {
		return envelope, fmt.Errorf("missing plugin invoker relay token in %s", providerhost.PluginInvokerSocketTokenEnv())
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
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(providerhost.HostServiceRelayTokenHeader, token))

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
			SessionID:     req.SessionID,
			PluginName:    req.PluginName,
			Command:       req.Command,
			Args:          slices.Clone(req.Args),
			Env:           cloneRuntimeMetadata(req.Env),
			BundleDir:     req.BundleDir,
			AllowedHosts:  slices.Clone(req.AllowedHosts),
			DefaultAction: req.DefaultAction,
			HostBinary:    req.HostBinary,
		}
	}
	return out
}

func (r *capturingBundlePluginRuntime) bindHostServiceRequests() []pluginruntime.BindHostServiceRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]pluginruntime.BindHostServiceRequest, len(r.bindRequests))
	for i, req := range r.bindRequests {
		out[i] = cloneBindHostServiceRequest(req)
	}
	return out
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

func cloneBindHostServiceRequest(req pluginruntime.BindHostServiceRequest) pluginruntime.BindHostServiceRequest {
	return pluginruntime.BindHostServiceRequest{
		SessionID: req.SessionID,
		EnvVar:    req.EnvVar,
		Relay: pluginruntime.HostServiceRelay{
			DialTarget: req.Relay.DialTarget,
		},
	}
}

func assertHostServiceRelayBindings(t *testing.T, requests []pluginruntime.BindHostServiceRequest, wantEnvVar string) {
	t.Helper()
	if len(requests) == 0 {
		t.Fatal("BindHostService requests = 0, want at least one relay binding")
	}
	foundWantEnv := false
	for _, req := range requests {
		if !strings.HasPrefix(req.Relay.DialTarget, "unix://") {
			t.Fatalf("BindHostService(%s) Relay.DialTarget = %q, want unix:// host relay target", req.EnvVar, req.Relay.DialTarget)
		}
		if req.EnvVar == wantEnvVar {
			foundWantEnv = true
		}
	}
	if !foundWantEnv {
		t.Fatalf("BindHostService env vars = %+v, want %q", requests, wantEnvVar)
	}
}

type slowStopPluginRuntime struct {
	inner     pluginruntime.Provider
	stopCount atomic.Int32
}

func (r *slowStopPluginRuntime) Capabilities(ctx context.Context) (pluginruntime.Capabilities, error) {
	return r.inner.Capabilities(ctx)
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

func (r *slowStopPluginRuntime) BindHostService(ctx context.Context, req pluginruntime.BindHostServiceRequest) (*pluginruntime.HostServiceBinding, error) {
	return r.inner.BindHostService(ctx, req)
}

func (r *slowStopPluginRuntime) StartPlugin(ctx context.Context, req pluginruntime.StartPluginRequest) (*pluginruntime.HostedPlugin, error) {
	return r.inner.StartPlugin(ctx, req)
}

func (r *slowStopPluginRuntime) Close() error {
	return r.inner.Close()
}

type failingBindSlowStopPluginRuntime struct {
	slowStopPluginRuntime
	err error
}

func (r *failingBindSlowStopPluginRuntime) BindHostService(context.Context, pluginruntime.BindHostServiceRequest) (*pluginruntime.HostServiceBinding, error) {
	return nil, r.err
}

type staticCapabilityPluginRuntime struct {
	inner        pluginruntime.Provider
	capabilities pluginruntime.Capabilities
}

func (r *staticCapabilityPluginRuntime) Capabilities(context.Context) (pluginruntime.Capabilities, error) {
	return r.capabilities, nil
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

func (r *staticCapabilityPluginRuntime) BindHostService(ctx context.Context, req pluginruntime.BindHostServiceRequest) (*pluginruntime.HostServiceBinding, error) {
	return r.inner.BindHostService(ctx, req)
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

func (m *stubWorkflowManager) GetRun(context.Context, *principal.Principal, string) (*workflowmanager.ManagedRun, error) {
	return nil, core.ErrNotFound
}

func (m *stubWorkflowManager) CancelRun(context.Context, *principal.Principal, string, string) (*workflowmanager.ManagedRun, error) {
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
	return coreworkflow.Target{
		PluginName: value.PluginName,
		Operation:  value.Operation,
		Connection: value.Connection,
		Instance:   value.Instance,
		Input:      maps.Clone(value.Input),
	}
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
plugin = "provider"
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
	manifestPath := filepath.Join(manifestRoot, "manifest.yaml")

	cfg := &config.Config{
		Plugins: map[string]*config.ProviderEntry{
			"hybrid": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: manifestPath,
				AllowedOperations: map[string]*config.OperationOverride{
					"echo":   {Alias: "renamed_echo"},
					"status": {Alias: "renamed_status"},
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

	_, operationConnections, err := buildStartupProviderSpec("hybrid", entry)
	if err != nil {
		t.Fatalf("buildStartupProviderSpec: %v", err)
	}
	if got := operationConnections["echo"]; got != "default" {
		t.Fatalf("startup echo connection = %q, want %q", got, "default")
	}
	if _, ok := operationConnections["status"]; ok {
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
					Name:   "status",
					Method: http.MethodGet,
					Path:   "/status",
				},
			},
		},
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

	services := coretesting.NewStubServices(t)
	subjectID := principal.UserSubjectID("u-hybrid")
	if err := services.Tokens.StoreToken(context.Background(), &core.IntegrationToken{
		SubjectID:   subjectID,
		Integration: "hybrid",
		Connection:  "default",
		Instance:    "default",
		AccessToken: "tok-default",
	}); err != nil {
		t.Fatalf("StoreToken(default): %v", err)
	}

	result, err := invocation.NewBroker(providers, services.Users, services.Tokens).Invoke(
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
	cfg := &config.Config{
		Plugins: map[string]*config.ProviderEntry{
			"caller": {
				Command:              callerBin,
				Args:                 []string{"provider"},
				ResolvedManifest:     callerManifest,
				ResolvedManifestPath: filepath.Join(callerRoot, "manifest.yaml"),
				Invokes: []config.PluginInvocationDependency{
					{Plugin: "example", Operation: "request_context"},
				},
			},
			"example": {
				Command:              exampleBin,
				ResolvedManifest:     exampleManifest,
				ResolvedManifestPath: filepath.Join(exampleRoot, "manifest.yaml"),
				Invokes: []config.PluginInvocationDependency{
					{Plugin: "example", Operation: "request_context"},
				},
				Config: mustNode(t, map[string]any{
					"greeting": "Hello from nested invoke",
				}),
			},
		},
	}

	providers, _, err := buildProvidersStrict(context.Background(), cfg, NewFactoryRegistry(), Deps{
		PluginInvoker: bridge,
	})
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })

	enc, err := corecrypto.NewAESGCM(corecrypto.DeriveKey("plugin-invokes-test-key"))
	if err != nil {
		t.Fatalf("NewAESGCM: %v", err)
	}
	services, err := coredata.New(&coretesting.StubIndexedDB{}, enc)
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	t.Cleanup(func() { _ = services.Close() })

	broker := invocation.NewBroker(providers, services.Users, services.Tokens, brokerOpts...)
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

	providers, _, err := buildProvidersStrict(context.Background(), cfg, NewFactoryRegistry(), Deps{
		PluginInvoker: bridge,
	})
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })

	enc, err := corecrypto.NewAESGCM(corecrypto.DeriveKey("plugin-graphql-invokes-test-key"))
	if err != nil {
		t.Fatalf("NewAESGCM: %v", err)
	}
	services, err := coredata.New(&coretesting.StubIndexedDB{}, enc)
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	t.Cleanup(func() { _ = services.Close() })

	if len(authCfg.Workloads) > 0 || len(authCfg.Policies) > 0 {
		authz, err := authorization.New(authCfg, cfg.Plugins, providers, nil)
		if err != nil {
			t.Fatalf("authorization.New: %v", err)
		}
		brokerOpts = append(brokerOpts, invocation.WithAuthorizer(authz))
	}
	broker := invocation.NewBroker(providers, services.Users, services.Tokens, brokerOpts...)
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

	if err := harness.services.Tokens.StoreToken(ctx, &core.IntegrationToken{
		SubjectID:    subjectID,
		Integration:  plugin,
		Connection:   connection,
		Instance:     instance,
		AccessToken:  plugin + "-" + connection + "-token",
		RefreshToken: "refresh-token",
	}); err != nil {
		t.Fatalf("StoreToken(%s,%s,%s): %v", plugin, connection, instance, err)
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
		"mcp": {
			Mode: providermanifestv1.ConnectionModeUser,
			Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeBearer},
		},
	}
	manifest.Spec.Surfaces = &providermanifestv1.ProviderSurfaces{
		REST: &providermanifestv1.RESTSurface{Connection: "api"},
		MCP:  &providermanifestv1.MCPSurface{URL: "https://example.invalid/mcp", Connection: "mcp"},
	}

	spec, operationConnections, err := buildStartupProviderSpec("roadmap", &config.ProviderEntry{
		ResolvedManifest:     manifest,
		ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
	})
	if err != nil {
		t.Fatalf("buildStartupProviderSpec: %v", err)
	}
	if spec.Catalog == nil || len(spec.Catalog.Operations) != 3 {
		t.Fatalf("unexpected startup catalog: %+v", spec.Catalog)
	}
	if got := operationConnections["status"]; got != "api" {
		t.Fatalf("status connection = %q, want %q", got, "api")
	}
	if got := operationConnections["search"]; got != "mcp" {
		t.Fatalf("search connection = %q, want %q", got, "mcp")
	}
	if got := operationConnections["echo"]; got != config.PluginConnectionName {
		t.Fatalf("echo connection = %q, want %q", got, config.PluginConnectionName)
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

	makeConfig := func(indexedDB *config.PluginIndexedDBConfig) *config.Config {
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
			Config: mustNode(t, map[string]any{"dsn": "postgres://main.example.test/gestalt"}),
		},
		"archive": {
			Config: mustNode(t, map[string]any{"dsn": "sqlite://archive.db"}),
		},
	}

	checkEnv := func(t *testing.T, indexedDB *config.PluginIndexedDBConfig, envName string) bool {
		t.Helper()
		providers, _, err := buildProvidersStrict(context.Background(), makeConfig(indexedDB), NewFactoryRegistry(), Deps{
			SelectedIndexedDBName: "main",
			IndexedDBDefs:         indexedDBDefs,
			IndexedDBFactory: func(yaml.Node) (indexeddb.IndexedDB, error) {
				return &coretesting.StubIndexedDB{}, nil
			},
		})
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

	if got := checkEnv(t, nil, providerhost.DefaultIndexedDBSocketEnv); !got {
		t.Fatal("default IndexedDB env should be set when plugin omits indexeddb and inherits the host selection")
	}
	if got := checkEnv(t, &config.PluginIndexedDBConfig{}, providerhost.DefaultIndexedDBSocketEnv); !got {
		t.Fatal("default IndexedDB env should be set when plugin indexeddb is explicitly empty")
	}
	if got := checkEnv(t, &config.PluginIndexedDBConfig{Provider: "archive"}, providerhost.DefaultIndexedDBSocketEnv); !got {
		t.Fatal("default IndexedDB env should be set when plugin explicitly selects one indexeddb provider")
	}
	if got := checkEnv(t, nil, providerhost.IndexedDBSocketEnv("main")); got {
		t.Fatal("named IndexedDB env should not be set for inherited plugin indexeddb access")
	}
	if got := checkEnv(t, &config.PluginIndexedDBConfig{Provider: "archive"}, providerhost.IndexedDBSocketEnv("archive")); got {
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

	providers, _, err := buildProvidersStrict(context.Background(), cfg, NewFactoryRegistry(), Deps{})
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	prov, err := providers.Get("caller")
	if err != nil {
		t.Fatalf("providers.Get(caller): %v", err)
	}

	result, err := prov.Execute(context.Background(), "read_env", map[string]any{"name": providerhost.DefaultPluginInvokerSocketEnv}, "")
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
		t.Fatalf("plugin invoker env %q should be set when plugin declares invokes", providerhost.DefaultPluginInvokerSocketEnv)
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

	providers, _, err := buildProvidersStrict(context.Background(), cfg, NewFactoryRegistry(), Deps{
		WorkflowManager: newStubWorkflowManager(),
	})
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	prov, err := providers.Get("echo")
	if err != nil {
		t.Fatalf("providers.Get(echo): %v", err)
	}

	result, err := prov.Execute(context.Background(), "read_env", map[string]any{"name": providerhost.DefaultWorkflowManagerSocketEnv}, "")
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
		t.Fatalf("workflow manager env %q should be set for executable plugins", providerhost.DefaultWorkflowManagerSocketEnv)
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

	providers, _, err := buildProvidersStrict(context.Background(), cfg, NewFactoryRegistry(), Deps{
		WorkflowManager: manager,
	})
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

	providers, _, err := buildProvidersStrict(context.Background(), cfg, NewFactoryRegistry(), Deps{
		WorkflowManager: newStubWorkflowManager(),
	})
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

func TestPluginInvokesGraphQLSurfaceRejectsWorkloadWithoutGraphQLPermission(t *testing.T) {
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

	authCfg := config.AuthorizationConfig{
		Workloads: map[string]config.WorkloadDef{
			"triage-bot": {
				Token: "gst_wld_triage-bot",
				Providers: map[string]config.WorkloadProviderDef{
					"caller": {
						Connection: "default",
						Allow:      []string{"invoke_plugin_graphql"},
					},
					"linear": {
						Connection: "default",
						Allow:      []string{"viewer"},
					},
				},
			},
		},
	}
	harness := newGraphQLSurfaceInvokeHarness(t, srv.URL, true, authCfg)
	ctx := context.Background()
	workloadSubjectID := principal.WorkloadSubjectID("triage-bot")
	storeNestedInvokeTokenForSubject(t, harness, ctx, workloadSubjectID, "caller", "default", "default")
	storeNestedInvokeTokenForSubject(t, harness, ctx, workloadSubjectID, "linear", "default", "default")

	result, err := harness.invoker.Invoke(
		invocation.WithConnection(context.Background(), "default"),
		&principal.Principal{
			SubjectID: workloadSubjectID,
			Kind:      principal.KindWorkload,
			Source:    principal.SourceWorkloadToken,
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
		t.Fatalf("expected workload graphql authorization rejection, got success: %+v", got)
	}
	if !strings.Contains(got.Error, `authorization denied: linear.graphql`) {
		t.Fatalf("workload graphql error = %q, want graphql authorization rejection", got.Error)
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

func TestPluginInvokesRejectUndeclaredTargets(t *testing.T) {
	t.Parallel()

	harness := newNestedInvokeHarness(t)
	ctx := context.Background()
	user := newNestedInvokeUser(t, harness, ctx, "nested-declared@test.com")
	storeNestedInvokeToken(t, harness, ctx, user.ID, "caller", "work", "default")
	storeNestedInvokeToken(t, harness, ctx, user.ID, "example", "work", "default")

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
		map[string]any{
			"plugin":    "example",
			"operation": "status",
		},
	)
	if err != nil {
		t.Fatalf("Invoke(caller.invoke_plugin): %v", err)
	}

	var got invokePluginEnvelope
	if err := json.Unmarshal([]byte(result.Body), &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got.OK {
		t.Fatalf("expected undeclared target rejection, got success: %+v", got)
	}
	if !strings.Contains(got.Error, `may not invoke example.status`) {
		t.Fatalf("undeclared target error = %q, want target rejection", got.Error)
	}
}

func TestPluginInvokesRejectInvalidInvocationToken(t *testing.T) {
	t.Parallel()

	harness := newNestedInvokeHarness(t)
	ctx := context.Background()
	user := newNestedInvokeUser(t, harness, ctx, "nested-invalid-handle@test.com")
	storeNestedInvokeToken(t, harness, ctx, user.ID, "caller", "work", "default")
	storeNestedInvokeToken(t, harness, ctx, user.ID, "example", "work", "default")

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
		map[string]any{
			"plugin":           "example",
			"operation":        "request_context",
			"invocation_token": "forged-token",
		},
	)
	if err != nil {
		t.Fatalf("Invoke(caller.invoke_plugin): %v", err)
	}

	var got invokePluginEnvelope
	if err := json.Unmarshal([]byte(result.Body), &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got.OK {
		t.Fatalf("expected invalid invocation token rejection, got success: %+v", got)
	}
	if !strings.Contains(got.Error, "invalid or expired") {
		t.Fatalf("invalid invocation token error = %q, want invalid or expired", got.Error)
	}
}

func TestPluginInvokesMissingTargetTokenReturnsFailedPrecondition(t *testing.T) {
	t.Parallel()

	harness := newNestedInvokeHarness(t)
	ctx := context.Background()
	user := newNestedInvokeUser(t, harness, ctx, "nested-no-target-token@test.com")
	storeNestedInvokeToken(t, harness, ctx, user.ID, "caller", "work", "default")

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
		map[string]any{
			"plugin":    "example",
			"operation": "request_context",
		},
	)
	if err != nil {
		t.Fatalf("Invoke(caller.invoke_plugin): %v", err)
	}

	var got invokePluginEnvelope
	if err := json.Unmarshal([]byte(result.Body), &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got.OK {
		t.Fatalf("expected missing target token envelope, got success: %+v", got)
	}
	if !strings.Contains(got.Error, "code = FailedPrecondition") {
		t.Fatalf("missing target token error = %q, want FailedPrecondition", got.Error)
	}
}

func TestPluginInvokesAmbiguousTargetInstanceReturnsAborted(t *testing.T) {
	t.Parallel()

	harness := newNestedInvokeHarness(t)
	ctx := context.Background()
	user := newNestedInvokeUser(t, harness, ctx, "nested-ambiguous-target@test.com")
	storeNestedInvokeToken(t, harness, ctx, user.ID, "caller", "work", "default")
	storeNestedInvokeToken(t, harness, ctx, user.ID, "example", "work", "primary")
	storeNestedInvokeToken(t, harness, ctx, user.ID, "example", "work", "secondary")

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
		map[string]any{
			"plugin":     "example",
			"operation":  "request_context",
			"connection": "work",
		},
	)
	if err != nil {
		t.Fatalf("Invoke(caller.invoke_plugin): %v", err)
	}

	var got invokePluginEnvelope
	if err := json.Unmarshal([]byte(result.Body), &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got.OK {
		t.Fatalf("expected ambiguous target instance envelope, got success: %+v", got)
	}
	if !strings.Contains(got.Error, "code = Aborted") {
		t.Fatalf("ambiguous target instance error = %q, want Aborted", got.Error)
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
		providers, _, err := buildProvidersStrict(context.Background(), makeConfig(bindings), NewFactoryRegistry(), Deps{
			CacheDefs: cacheBindings,
			CacheFactory: func(yaml.Node) (corecache.Cache, error) {
				return coretesting.NewStubCache(), nil
			},
		})
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

	if got := checkEnv(t, nil, providerhost.DefaultCacheSocketEnv); got {
		t.Fatal("default cache env should not be set without plugin cache bindings")
	}
	if got := checkEnv(t, []string{"session"}, providerhost.DefaultCacheSocketEnv); !got {
		t.Fatal("default cache env should be set with a single plugin cache binding")
	}
	if got := checkEnv(t, []string{"session"}, providerhost.CacheSocketEnv("session")); !got {
		t.Fatal("named cache env should be set with a single plugin cache binding")
	}
	if got := checkEnv(t, []string{"session", "rate_limit"}, providerhost.DefaultCacheSocketEnv); got {
		t.Fatal("default cache env should not be set with multiple plugin cache bindings")
	}
	if got := checkEnv(t, []string{"session", "rate_limit"}, providerhost.CacheSocketEnv("session")); !got {
		t.Fatal(`named cache env for "session" should be set with multiple plugin cache bindings`)
	}
	if got := checkEnv(t, []string{"session", "rate_limit"}, providerhost.CacheSocketEnv("rate_limit")); !got {
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
	runtimeProvider := &failingBindSlowStopPluginRuntime{
		slowStopPluginRuntime: slowStopPluginRuntime{inner: pluginruntime.NewLocalProvider()},
		err:                   fmt.Errorf("bind failed"),
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
		_, _, err := buildProvidersStrict(context.Background(), cfg, NewFactoryRegistry(), Deps{
			PluginRuntime: runtimeProvider,
		})
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "bind host service") {
			t.Fatalf("buildProvidersStrict error = %v, want bind host service failure", err)
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
			Runtime: config.ServerRuntimeConfig{Provider: "hosted"},
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
				Runtime: &config.PluginRuntimeConfig{
					Template: "python-dev",
					Image:    "ghcr.io/valon/gestalt-python-runtime:latest",
					Metadata: map[string]string{"tenant": "eng"},
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
	if req.Metadata["plugin"] != "echoext" {
		t.Fatalf("StartSession Metadata[plugin] = %q, want echoext", req.Metadata["plugin"])
	}
	if factoryContextValue != ctxSentinel {
		t.Fatalf("runtime factory context value = %#v, want %#v", factoryContextValue, ctxSentinel)
	}
}

func TestPluginRuntimeStagesBundleForNonHostPathExecution(t *testing.T) {
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
			Runtime: config.ServerRuntimeConfig{Provider: "hosted"},
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
				Runtime:              &config.PluginRuntimeConfig{},
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
	if req.BundleDir == "" {
		t.Fatal("StartPlugin BundleDir = empty, want staged bundle")
	}
	if !strings.HasPrefix(req.Command, pluginruntime.HostedPluginBundleRoot+"/") {
		t.Fatalf("StartPlugin Command = %q, want command under %s", req.Command, pluginruntime.HostedPluginBundleRoot)
	}
	if got := runtimeProvider.bindHostCalls.Load(); got != 0 {
		t.Fatalf("BindHostService calls = %d, want 0", got)
	}

	if err := CloseProviders(providers); err != nil {
		t.Fatalf("CloseProviders: %v", err)
	}
	if _, err := os.Stat(req.BundleDir); !os.IsNotExist(err) {
		t.Fatalf("bundle dir stat after close = %v, want not-exist", err)
	}
}

func TestPluginRuntimeDropsSourceStyleArgsForNonHostPathExecution(t *testing.T) {
	t.Parallel()

	bin := buildExampleProviderBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "example",
		Operations: []catalog.CatalogOperation{
			{ID: "status", Method: http.MethodGet},
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
	manifest := newExecutableManifest("Example Provider", "Prepared install artifact")
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
			Runtime: config.ServerRuntimeConfig{Provider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Plugins: map[string]*config.ProviderEntry{
			"example": {
				Args:                 []string{"-m", "gestalt._runtime", "/host/source", "pkg.provider", "integration"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: manifestPath,
				Runtime:              &config.PluginRuntimeConfig{},
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
	if len(requests[0].Args) != 0 {
		t.Fatalf("StartPlugin Args = %#v, want source-style host args dropped", requests[0].Args)
	}
	if !strings.HasPrefix(requests[0].Command, pluginruntime.HostedPluginBundleRoot+"/") {
		t.Fatalf("StartPlugin Command = %q, want command under %s", requests[0].Command, pluginruntime.HostedPluginBundleRoot)
	}
}

func TestPluginRuntimeRewritesHostPathArgsForNonHostPathExecution(t *testing.T) {
	t.Parallel()

	bin := buildExampleProviderBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "example",
		Operations: []catalog.CatalogOperation{
			{ID: "status", Method: http.MethodGet},
		},
	})
	configPath := filepath.Join(manifestRoot, "fixtures", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(fixtures): %v", err)
	}
	if err := os.WriteFile(configPath, []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatalf("WriteFile(config.json): %v", err)
	}
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
	manifest := newExecutableManifest("Example Provider", "Prepared install artifact")
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
			Runtime: config.ServerRuntimeConfig{Provider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
		Plugins: map[string]*config.ProviderEntry{
			"example": {
				Command:              bin,
				Args:                 []string{"--config", configPath, "--name", "example"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: manifestPath,
				Runtime:              &config.PluginRuntimeConfig{},
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
	if requests[0].Args[1] != pluginruntime.HostedPluginBundleRoot+"/fixtures/config.json" {
		t.Fatalf("StartPlugin Args = %#v, want staged bundle config path", requests[0].Args)
	}
	if requests[0].Args[3] != "example" {
		t.Fatalf("StartPlugin Args = %#v, want non-path args preserved", requests[0].Args)
	}
}

func TestPluginRuntimeConfigRejectsMissingHostServiceTunnelCapability(t *testing.T) {
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
		capabilities: pluginruntime.Capabilities{
			HostedPluginRuntime: true,
			ProviderGRPCTunnel:  true,
			HostnameProxyEgress: true,
		},
	}
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{Provider: "hosted"},
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
				Runtime:              &config.PluginRuntimeConfig{},
				Cache:                []string{"session"},
			},
		},
	}

	_, _, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{
		CacheDefs: map[string]*config.ProviderEntry{
			"session": {Config: mustNode(t, map[string]any{"namespace": "session"})},
		},
		CacheFactory: func(yaml.Node) (corecache.Cache, error) {
			return coretesting.NewStubCache(), nil
		},
		PluginRuntimeRegistry: newPluginRuntimeRegistry(cfg, factories.Runtime, Deps{}),
	})
	if err == nil || !strings.Contains(err.Error(), "host service tunnels") {
		t.Fatalf("buildProvidersStrict error = %v, want missing host service tunnels", err)
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
	runtimeProvider.capabilities.HostnameProxyEgress = true
	runtimeProvider.capabilities.HostPathExecution = true
	runtimeProvider.fakeHosted = true
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{Provider: "hosted"},
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
				Runtime:              &config.PluginRuntimeConfig{},
				IndexedDB:            &config.PluginIndexedDBConfig{ObjectStores: []string{"tasks"}},
			},
		},
	}

	deps := Deps{
		BaseURL:               "https://gestalt.example.test",
		EncryptionKey:         []byte("0123456789abcdef0123456789abcdef"),
		SelectedIndexedDBName: "memory",
		IndexedDBDefs: map[string]*config.ProviderEntry{
			"memory": {
				Source: config.ProviderSource{Path: "./providers/datastore/memory"},
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

	if got := checkEnv(providerhost.DefaultIndexedDBSocketEnv); got != "tls://gestalt.example.test:443" {
		t.Fatalf("plugin indexeddb socket env = %q, want %q", got, "tls://gestalt.example.test:443")
	}
	if got := checkEnv(providerhost.IndexedDBSocketTokenEnv("")); got == "" {
		t.Fatal("plugin indexeddb socket token env should be set for the public relay")
	}

	bindRequests := runtimeProvider.bindHostServiceRequests()
	if len(bindRequests) != 1 {
		t.Fatalf("BindHostService requests = %d, want 1", len(bindRequests))
	}
	if got := bindRequests[0].Relay.DialTarget; got != "tls://gestalt.example.test:443" {
		t.Fatalf("BindHostService relay target = %q, want %q", got, "tls://gestalt.example.test:443")
	}

	startRequests := runtimeProvider.startPluginRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("StartPlugin requests = %d, want 1", len(startRequests))
	}
	if got := startRequests[0].Env[providerhost.IndexedDBSocketTokenEnv("")]; got == "" {
		t.Fatal("StartPlugin env should include the IndexedDB relay token")
	}
	if !slices.Contains(startRequests[0].AllowedHosts, "gestalt.example.test") {
		t.Fatalf("StartPlugin allowed hosts = %#v, want relay host gestalt.example.test", startRequests[0].AllowedHosts)
	}
}

func TestPluginRuntimePublicIndexedDBRelayRoundTripsThroughHostedPlugin(t *testing.T) {
	t.Parallel()

	secret := []byte("0123456789abcdef0123456789abcdef")
	relaySrv := httptest.NewUnstartedServer(newRuntimeRelayTestHandler(t, secret))
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
	runtimeProvider.capabilities.HostnameProxyEgress = true
	runtimeProvider.capabilities.HostPathExecution = true
	runtimeProvider.fakeHosted = true

	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}

	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{Provider: "hosted"},
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
				Runtime:              &config.PluginRuntimeConfig{},
				IndexedDB:            &config.PluginIndexedDBConfig{ObjectStores: []string{"tasks"}},
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
				Source: config.ProviderSource{Path: "./providers/datastore/memory"},
				Config: mustNode(t, map[string]any{"bucket": "plugin-state"}),
			},
		},
		IndexedDBFactory: func(yaml.Node) (indexeddb.IndexedDB, error) {
			return boundDB, nil
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

	gotRecord, err := boundDB.ObjectStore("echoext_tasks").Get(context.Background(), "task-1")
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
	if got := startRequests[0].Env[providerhost.IndexedDBSocketTokenEnv("")]; got == "" {
		t.Fatal("StartPlugin env should include the IndexedDB relay token")
	}
	bindRequests := runtimeProvider.bindHostServiceRequests()
	if len(bindRequests) != 1 {
		t.Fatalf("BindHostService requests = %d, want 1", len(bindRequests))
	}
	if got := bindRequests[0].Relay.DialTarget; !strings.HasPrefix(got, "tls://") {
		t.Fatalf("BindHostService relay target = %q, want tls relay target", got)
	}
}

func TestPluginRuntimePublicPluginInvokerRelayRoundTripsThroughHostedPlugin(t *testing.T) {
	t.Parallel()

	secret := []byte("0123456789abcdef0123456789abcdef")
	relaySrv := httptest.NewUnstartedServer(newRuntimeRelayTestHandler(t, secret))
	relaySrv.EnableHTTP2 = true
	relaySrv.StartTLS()
	testutil.CloseOnCleanup(t, relaySrv)

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
	runtimeProvider.capabilities.HostnameProxyEgress = true
	runtimeProvider.capabilities.HostPathExecution = true
	runtimeProvider.fakeHosted = true

	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}

	bridge := newLazyInvoker()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{Provider: "hosted"},
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
				Runtime:              &config.PluginRuntimeConfig{},
				Invokes: []config.PluginInvocationDependency{
					{Plugin: "example", Operation: "request_context"},
				},
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
		BaseURL:       relaySrv.URL,
		EncryptionKey: secret,
		PluginInvoker: bridge,
	}
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)

	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })

	enc, err := corecrypto.NewAESGCM(corecrypto.DeriveKey("plugin-invokes-test-key"))
	if err != nil {
		t.Fatalf("NewAESGCM: %v", err)
	}
	services, err := coredata.New(&coretesting.StubIndexedDB{}, enc)
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	t.Cleanup(func() { _ = services.Close() })

	broker := invocation.NewBroker(providers, services.Users, services.Tokens)
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

	relayURL, err := url.Parse(relaySrv.URL)
	if err != nil {
		t.Fatalf("url.Parse(relay URL): %v", err)
	}
	startRequests := runtimeProvider.startPluginRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("StartPlugin requests = %d, want 1", len(startRequests))
	}
	if got := startRequests[0].Env[providerhost.PluginInvokerSocketTokenEnv()]; got == "" {
		t.Fatal("StartPlugin env should include the plugin invoker relay token")
	}
	if !slices.Contains(startRequests[0].AllowedHosts, relayURL.Hostname()) {
		t.Fatalf("StartPlugin allowed hosts = %#v, want relay host %q", startRequests[0].AllowedHosts, relayURL.Hostname())
	}
	bindRequests := runtimeProvider.bindHostServiceRequests()
	if len(bindRequests) != 1 {
		t.Fatalf("BindHostService requests = %d, want 1", len(bindRequests))
	}
	if bindRequests[0].EnvVar != providerhost.DefaultPluginInvokerSocketEnv {
		t.Fatalf("BindHostService env = %q, want %q", bindRequests[0].EnvVar, providerhost.DefaultPluginInvokerSocketEnv)
	}
	if got := bindRequests[0].Relay.DialTarget; !strings.HasPrefix(got, "tls://") {
		t.Fatalf("BindHostService relay target = %q, want tls relay target", got)
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
		capabilities: pluginruntime.Capabilities{
			HostedPluginRuntime: true,
			ProviderGRPCTunnel:  true,
			HostServiceTunnels:  true,
		},
	}
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{Provider: "hosted"},
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
				Runtime:              &config.PluginRuntimeConfig{},
				AllowedHosts:         []string{"api.github.com"},
			},
		},
	}

	_, _, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{
		PluginRuntimeRegistry: newPluginRuntimeRegistry(cfg, factories.Runtime, Deps{}),
	})
	if err == nil || !strings.Contains(err.Error(), "hostname-based egress controls") {
		t.Fatalf("buildProvidersStrict error = %v, want missing hostname-based egress controls", err)
	}
}

func newRuntimeRelayTestHandler(t *testing.T, stateSecret []byte) http.Handler {
	t.Helper()

	tokenManager, err := providerhost.NewHostServiceRelayTokenManager(stateSecret)
	if err != nil {
		t.Fatalf("NewHostServiceRelayTokenManager: %v", err)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimSpace(r.Header.Get(providerhost.HostServiceRelayTokenHeader))
		target, err := tokenManager.ResolveToken(token)
		if err != nil {
			writeRuntimeRelayGRPCTrailersOnly(w, codes.Unauthenticated, "invalid-host-service-relay-token")
			return
		}
		if !runtimeRelayMethodAllowed(r.URL.Path, target.MethodPrefix) {
			writeRuntimeRelayGRPCTrailersOnly(w, codes.PermissionDenied, "host-service-relay-method-not-allowed")
			return
		}
		newRuntimeRelayProxy(target.SocketPath).ServeHTTP(w, r)
	})
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

func newRuntimeRelayProxy(socketPath string) *httputil.ReverseProxy {
	target := &url.URL{
		Scheme: "http",
		Host:   "gestalt-test-host-service-relay",
	}
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.Host = target.Host
			pr.Out.Header.Del(providerhost.HostServiceRelayTokenHeader)
		},
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, _, _ string, _ *tls.Config) (net.Conn, error) {
				var dialer net.Dialer
				return dialer.DialContext(ctx, "unix", socketPath)
			},
		},
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			writeRuntimeRelayGRPCTrailersOnly(w, codes.Unavailable, "host-service-relay-unavailable")
		},
	}
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

func TestPluginRuntimeConfigRejectsMissingHostServiceTunnelCapabilityWithWorkflowProvider(t *testing.T) {
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
		capabilities: pluginruntime.Capabilities{
			HostedPluginRuntime: true,
			ProviderGRPCTunnel:  true,
		},
	}
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{Provider: "hosted"},
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
				Runtime:              &config.PluginRuntimeConfig{},
			},
		},
	}

	workflowRuntime := &workflowRuntime{
		providers: map[string]coreworkflow.Provider{
			"managed": nil,
		},
	}
	_, _, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{
		WorkflowRuntime:       workflowRuntime,
		PluginRuntimeRegistry: newPluginRuntimeRegistry(cfg, factories.Runtime, Deps{WorkflowRuntime: workflowRuntime}),
	})
	if err == nil || !strings.Contains(err.Error(), "host service tunnels") {
		t.Fatalf("buildProvidersStrict error = %v, want missing host service tunnels", err)
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
		capabilities: pluginruntime.Capabilities{
			HostedPluginRuntime: true,
			ProviderGRPCTunnel:  true,
			HostServiceTunnels:  true,
		},
	}
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{Provider: "hosted"},
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
				Runtime:              &config.PluginRuntimeConfig{},
			},
		},
	}

	_, _, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{
		PluginRuntimeRegistry: newPluginRuntimeRegistry(cfg, factories.Runtime, Deps{
			Egress: EgressDeps{DefaultAction: egress.PolicyDeny},
		}),
		Egress: EgressDeps{DefaultAction: egress.PolicyDeny},
	})
	if err == nil || !strings.Contains(err.Error(), "hostname-based egress controls") {
		t.Fatalf("buildProvidersStrict error = %v, want missing hostname-based egress controls", err)
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

	_, _, err := buildProvidersStrict(context.Background(), cfg, NewFactoryRegistry(), Deps{
		CacheDefs: map[string]*config.ProviderEntry{
			"session": {
				Config: mustNode(t, map[string]any{"namespace": "session"}),
			},
		},
		CacheFactory: func(yaml.Node) (corecache.Cache, error) {
			return coretesting.NewStubCache(), nil
		},
	})
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
		indexedDB *config.PluginIndexedDBConfig
	}{
		{name: "omitted indexeddb inherits host selection"},
		{name: "empty indexeddb inherits host selection", indexedDB: &config.PluginIndexedDBConfig{}},
		{name: "objectStores-only indexeddb inherits host selection", indexedDB: &config.PluginIndexedDBConfig{ObjectStores: []string{"tasks"}}},
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
							Source: config.ProviderSource{Path: "./providers/datastore/memory"},
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
				}, NewFactoryRegistry(), deps)
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
				if _, err := boundDB.ObjectStore("echoext_tasks").Get(context.Background(), "task-1"); err != nil {
					t.Fatalf("inherited host indexeddb should use plugin-name default db prefix: %v", err)
				}
				if runtimeProvider != nil {
					assertHostServiceRelayBindings(t, runtimeProvider.bindHostServiceRequests(), providerhost.DefaultIndexedDBSocketEnv)
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

	makeConfig := func(indexedDB *config.PluginIndexedDBConfig) *config.Config {
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
				"dsn":                 "postgres://db.example.test/gestalt",
				"schema":              "host_schema",
				"namespace":           "host_schema_alias_should_be_removed",
				"legacy_table_prefix": "host_legacy_should_be_replaced_",
				"legacy_prefix":       "host_legacy_alias_should_be_removed_",
			}),
		},
		"sqlite": {
			Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.1/provider-release.yaml"),
			Config: mustNode(t, map[string]any{
				"dsn":                 "sqlite://plugin-state.db",
				"table_prefix":        "host_",
				"prefix":              "host_",
				"schema":              "should_be_removed",
				"namespace":           "should_be_removed",
				"legacy_table_prefix": "host_legacy_should_be_replaced_",
				"legacy_prefix":       "host_legacy_alias_should_be_removed_",
			}),
		},
		"local-postgres": {
			Source: config.ProviderSource{Path: "./relationaldb/manifest.yaml"},
			Config: mustNode(t, map[string]any{
				"dsn":                 "postgres://local.example.test/gestalt",
				"schema":              "host_local",
				"namespace":           "host_local_alias_should_be_removed",
				"legacy_table_prefix": "host_local_legacy_should_be_replaced_",
				"legacy_prefix":       "host_local_legacy_alias_should_be_removed_",
			}),
		},
	}

	cases := []struct {
		name       string
		indexedDB  *config.PluginIndexedDBConfig
		wantDSN    string
		wantDB     string
		wantSQLite bool
	}{
		{
			name:      "defaults db to plugin name for postgres",
			indexedDB: &config.PluginIndexedDBConfig{Provider: "postgres"},
			wantDSN:   "postgres://db.example.test/gestalt",
			wantDB:    "echoext",
		},
		{
			name:      "uses db override for postgres",
			indexedDB: &config.PluginIndexedDBConfig{Provider: "postgres", DB: "roadmap_state"},
			wantDSN:   "postgres://db.example.test/gestalt",
			wantDB:    "roadmap_state",
		},
		{
			name:       "uses db override for sqlite table prefixes",
			indexedDB:  &config.PluginIndexedDBConfig{Provider: "sqlite", DB: "roadmap_state"},
			wantDSN:    "sqlite://plugin-state.db",
			wantDB:     "roadmap_state",
			wantSQLite: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var closeCount atomic.Int32
			captured := make(map[string]capturedIndexedDBConfig)
			providers, _, err := buildProvidersStrict(context.Background(), makeConfig(tc.indexedDB), NewFactoryRegistry(), Deps{
				SelectedIndexedDBName: "postgres",
				IndexedDBDefs:         indexedDBDefs,
				IndexedDBFactory: func(node yaml.Node) (indexeddb.IndexedDB, error) {
					var decoded capturedIndexedDBConfig
					if err := node.Decode(&decoded); err != nil {
						return nil, err
					}
					dsn, _ := decoded.Config["dsn"].(string)
					captured[dsn] = decoded
					return &trackedIndexedDB{
						StubIndexedDB: coretesting.StubIndexedDB{},
						onClose:       closeCount.Add,
					}, nil
				},
			})
			if err != nil {
				t.Fatalf("buildProvidersStrict: %v", err)
			}
			t.Cleanup(func() {
				if providers != nil {
					_ = CloseProviders(providers)
				}
			})

			cfg, ok := captured[tc.wantDSN]
			if !ok {
				t.Fatalf("missing captured indexeddb config for %q", tc.wantDSN)
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
			if _, ok := cfg.Config["namespace"]; ok {
				t.Fatalf("namespace should be removed, got %#v", cfg.Config["namespace"])
			}
			if got := cfg.Config["legacy_table_prefix"]; got != "plugin_echoext_" {
				t.Fatalf("legacy_table_prefix = %#v, want %q", got, "plugin_echoext_")
			}
			if _, ok := cfg.Config["legacy_prefix"]; ok {
				t.Fatalf("legacy_prefix should be removed, got %#v", cfg.Config["legacy_prefix"])
			}

			_ = CloseProviders(providers)
			providers = nil
			if got := closeCount.Load(); got != 1 {
				t.Fatalf("closeCount after provider shutdown = %d, want 1", got)
			}
		})
	}
}

func TestPluginIndexedDBRouteObjectStoresAndTransportPrefix(t *testing.T) {
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
						Source: config.ProviderSource{Path: "./providers/datastore/memory"},
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
						IndexedDB: &config.PluginIndexedDBConfig{
							Provider:     "memory",
							DB:           "roadmap",
							ObjectStores: []string{"tasks"},
						},
					},
				},
			}, NewFactoryRegistry(), deps)
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
			if _, err := boundDB.ObjectStore("roadmap_tasks").Get(context.Background(), "task-1"); err != nil {
				t.Fatalf("prefixed backing store should contain task: %v", err)
			}
			if _, err := boundDB.ObjectStore("tasks").Get(context.Background(), "task-1"); err == nil {
				t.Fatal("unprefixed backing store should remain empty")
			}

			if _, err := prov.Execute(context.Background(), "indexeddb_roundtrip", map[string]any{
				"store": "events",
				"id":    "evt-1",
				"value": "blocked",
			}, ""); err == nil {
				t.Fatal("indexeddb_roundtrip on disallowed object store should fail")
			}
			if runtimeProvider != nil {
				assertHostServiceRelayBindings(t, runtimeProvider.bindHostServiceRequests(), providerhost.DefaultIndexedDBSocketEnv)
			}

			_ = CloseProviders(providers)
			providers = nil
			if got := closeCount.Load(); got != 1 {
				t.Fatalf("closeCount after provider shutdown = %d, want 1", got)
			}
		})
	}
}

func TestPluginIndexedDBRouteObjectStoresWithoutTransportPrefix(t *testing.T) {
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

	var (
		closeCount atomic.Int32
		boundDB    *trackedIndexedDB
	)
	providers, _, err := buildProvidersStrict(context.Background(), &config.Config{
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				IndexedDB: &config.PluginIndexedDBConfig{
					Provider:     "postgres",
					DB:           "roadmap",
					ObjectStores: []string{"tasks"},
				},
			},
		},
	}, NewFactoryRegistry(), Deps{
		SelectedIndexedDBName: "postgres",
		IndexedDBDefs: map[string]*config.ProviderEntry{
			"postgres": {
				Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.1/provider-release.yaml"),
				Config: mustNode(t, map[string]any{
					"dsn":                 "postgres://db.example.test/gestalt",
					"schema":              "host_schema",
					"legacy_table_prefix": "host_legacy_",
				}),
			},
		},
		IndexedDBFactory: func(yaml.Node) (indexeddb.IndexedDB, error) {
			boundDB = &trackedIndexedDB{
				StubIndexedDB: coretesting.StubIndexedDB{},
				onClose:       closeCount.Add,
			}
			return boundDB, nil
		},
	})
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
		t.Fatalf("scoped-provider indexeddb should use the requested store name directly: %v", err)
	}
	if _, err := boundDB.ObjectStore("roadmap_tasks").Get(context.Background(), "task-1"); err == nil {
		t.Fatal("transport-prefixed backing store should remain empty when scoped provider config is used")
	}
	if _, err := boundDB.ObjectStore("plugin_echoext_tasks").Get(context.Background(), "task-1"); err == nil {
		t.Fatal("legacy transport-prefixed backing store should remain empty when scoped provider config is used")
	}

	if _, err := prov.Execute(context.Background(), "indexeddb_roundtrip", map[string]any{
		"store": "events",
		"id":    "evt-1",
		"value": "blocked",
	}, ""); err == nil {
		t.Fatal("indexeddb_roundtrip on disallowed object store should fail when only allowed stores are configured")
	}

	_ = CloseProviders(providers)
	providers = nil
	if got := closeCount.Load(); got != 1 {
		t.Fatalf("closeCount after provider shutdown = %d, want 1", got)
	}
}

func TestPluginIndexedDBPreserveLegacyTransportPrefixedData(t *testing.T) {
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

	var boundDB *trackedIndexedDB
	providers, _, err := buildProvidersStrict(context.Background(), &config.Config{
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				IndexedDB: &config.PluginIndexedDBConfig{
					Provider:     "memory",
					DB:           "roadmap",
					ObjectStores: []string{"tasks"},
				},
			},
		},
	}, NewFactoryRegistry(), Deps{
		SelectedIndexedDBName: "memory",
		IndexedDBDefs: map[string]*config.ProviderEntry{
			"memory": {
				Source: config.ProviderSource{Path: "./providers/datastore/memory"},
				Config: mustNode(t, map[string]any{"bucket": "plugin-state"}),
			},
		},
		IndexedDBFactory: func(yaml.Node) (indexeddb.IndexedDB, error) {
			boundDB = &trackedIndexedDB{StubIndexedDB: coretesting.StubIndexedDB{}}
			return boundDB, nil
		},
	})
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })

	if err := boundDB.CreateObjectStore(context.Background(), "plugin_echoext_tasks", indexeddb.ObjectStoreSchema{}); err != nil {
		t.Fatalf("CreateObjectStore legacy tasks: %v", err)
	}
	if err := boundDB.ObjectStore("plugin_echoext_tasks").Put(context.Background(), indexeddb.Record{
		"id":    "legacy-task",
		"value": "already-there",
	}); err != nil {
		t.Fatalf("Put legacy task: %v", err)
	}

	prov, err := providers.Get("echoext")
	if err != nil {
		t.Fatalf("providers.Get: %v", err)
	}
	if _, err := prov.Execute(context.Background(), "indexeddb_roundtrip", map[string]any{
		"store": "tasks",
		"id":    "task-1",
		"value": "ship-it",
	}, ""); err != nil {
		t.Fatalf("Execute indexeddb_roundtrip: %v", err)
	}

	if _, err := boundDB.ObjectStore("plugin_echoext_tasks").Get(context.Background(), "task-1"); err != nil {
		t.Fatalf("legacy backing store should receive new writes: %v", err)
	}
	if _, err := boundDB.ObjectStore("plugin_echoext_tasks").Get(context.Background(), "legacy-task"); err != nil {
		t.Fatalf("legacy backing store should keep old rows: %v", err)
	}
	if _, err := boundDB.ObjectStore("roadmap_tasks").Get(context.Background(), "task-1"); err == nil {
		t.Fatal("new transport-prefixed store should remain unused while only legacy data exists")
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
				IndexedDB: &config.PluginIndexedDBConfig{
					Provider: "archive",
					DB:       "roadmap",
				},
			},
		},
	}, NewFactoryRegistry(), Deps{
		SelectedIndexedDBName: "main",
		IndexedDBDefs: map[string]*config.ProviderEntry{
			"main": {
				Source: config.ProviderSource{Path: "./providers/datastore/main"},
				Config: mustNode(t, map[string]any{"bucket": "main"}),
			},
			"archive": {
				Source: config.ProviderSource{Path: "./providers/datastore/archive"},
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
	})
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
	if _, err := boundDBs["archive"].ObjectStore("roadmap_events").Get(context.Background(), "evt-1"); err != nil {
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
				IndexedDB:            &config.PluginIndexedDBConfig{Provider: "main"},
				S3:                   []string{"missing"},
			},
		},
	}, NewFactoryRegistry(), Deps{
		SelectedIndexedDBName: "main",
		IndexedDBDefs: map[string]*config.ProviderEntry{
			"main": {
				Source: config.ProviderSource{Path: "./providers/datastore/main"},
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
	})
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
	}, NewFactoryRegistry(), Deps{
		Services: coretesting.NewStubServices(t),
		S3: map[string]s3store.Client{
			"main": stubS3,
		},
	})
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
	}, NewFactoryRegistry(), Deps{
		Services: coretesting.NewStubServices(t),
		S3: map[string]s3store.Client{
			"main":    mainS3,
			"archive": archiveS3,
		},
	})
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
		providers, _, err := buildProvidersStrict(context.Background(), makeConfig(bindings), NewFactoryRegistry(), Deps{
			Services: services,
			S3:       s3Bindings,
		})
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

	if got := checkEnv(t, nil, providerhost.DefaultS3SocketEnv); got {
		t.Fatal("default S3 env should not be set without plugin s3 bindings")
	}
	if got := checkEnv(t, []string{"main"}, providerhost.DefaultS3SocketEnv); !got {
		t.Fatal("default S3 env should be set with a single plugin s3 binding")
	}
	if got := checkEnv(t, []string{"main"}, providerhost.S3SocketEnv("main")); !got {
		t.Fatal("named S3 env should be set with a single plugin s3 binding")
	}
	if got := checkEnv(t, []string{"main", "archive"}, providerhost.DefaultS3SocketEnv); got {
		t.Fatal("default S3 env should not be set with multiple plugin s3 bindings")
	}
	if got := checkEnv(t, []string{"main", "archive"}, providerhost.S3SocketEnv("main")); !got {
		t.Fatal(`named S3 env for "main" should be set with multiple plugin s3 bindings`)
	}
	if got := checkEnv(t, []string{"main", "archive"}, providerhost.S3SocketEnv("archive")); !got {
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
