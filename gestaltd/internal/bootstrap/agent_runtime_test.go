package bootstrap

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	corecrypto "github.com/valon-technologies/gestalt/server/core/crypto"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/egress"
	"github.com/valon-technologies/gestalt/server/internal/pluginruntime"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func buildAgentProviderBinary(t *testing.T) string {
	t.Helper()
	if sharedAgentProviderBin == "" {
		t.Fatal("shared agent provider binary not initialized")
	}
	return sharedAgentProviderBin
}

type agentRuntimeFactoryContextKey struct{}

type agentRuntimeInvokerCall struct {
	providerName string
	operation    string
	params       map[string]any
	subjectID    string
}

type recordingAgentRuntimeInvoker struct {
	mu    sync.Mutex
	calls []agentRuntimeInvokerCall
}

func (i *recordingAgentRuntimeInvoker) Invoke(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error) {
	i.mu.Lock()
	i.calls = append(i.calls, agentRuntimeInvokerCall{
		providerName: providerName,
		operation:    operation,
		params:       cloneAnyMap(params),
		subjectID:    p.SubjectID,
	})
	i.mu.Unlock()

	body, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	return &core.OperationResult{Status: http.StatusAccepted, Body: string(body)}, nil
}

func (i *recordingAgentRuntimeInvoker) Calls() []agentRuntimeInvokerCall {
	i.mu.Lock()
	defer i.mu.Unlock()
	out := make([]agentRuntimeInvokerCall, len(i.calls))
	copy(out, i.calls)
	return out
}

func cloneAnyMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	out := make(map[string]any, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}

type capturingHostedAgentRuntime struct {
	provider *pluginruntime.LocalProvider
	support  pluginruntime.Support

	mu                  sync.Mutex
	bindRequests        []pluginruntime.BindHostServiceRequest
	startPluginRequests []pluginruntime.StartPluginRequest
	extraEnv            map[string]string
	fakeAgents          map[string]*fakeHostedAgentServer
}

type fakeHostedAgentServer struct {
	dir      string
	env      map[string]string
	listener net.Listener
	server   *grpc.Server
}

func newCapturingHostedAgentRuntime() *capturingHostedAgentRuntime {
	return &capturingHostedAgentRuntime{
		provider: pluginruntime.NewLocalProvider(),
		support: pluginruntime.Support{
			CanHostPlugins: true,
			LaunchMode:     pluginruntime.LaunchModeHostPath,
		},
		fakeAgents: make(map[string]*fakeHostedAgentServer),
	}
}

func (r *capturingHostedAgentRuntime) Support(context.Context) (pluginruntime.Support, error) {
	return r.support, nil
}

func (r *capturingHostedAgentRuntime) StartSession(ctx context.Context, req pluginruntime.StartSessionRequest) (*pluginruntime.Session, error) {
	return r.provider.StartSession(ctx, req)
}

func (r *capturingHostedAgentRuntime) ListSessions(ctx context.Context) ([]pluginruntime.Session, error) {
	return r.provider.ListSessions(ctx)
}

func (r *capturingHostedAgentRuntime) GetSession(ctx context.Context, req pluginruntime.GetSessionRequest) (*pluginruntime.Session, error) {
	return r.provider.GetSession(ctx, req)
}

func (r *capturingHostedAgentRuntime) StopSession(ctx context.Context, req pluginruntime.StopSessionRequest) error {
	r.cleanupFakeHostedAgent(req.SessionID)
	return r.provider.StopSession(ctx, req)
}

func (r *capturingHostedAgentRuntime) BindHostService(ctx context.Context, req pluginruntime.BindHostServiceRequest) (*pluginruntime.HostServiceBinding, error) {
	r.mu.Lock()
	r.bindRequests = append(r.bindRequests, cloneBindHostServiceRequest(req))
	r.mu.Unlock()
	return r.provider.BindHostService(ctx, req)
}

func (r *capturingHostedAgentRuntime) StartPlugin(ctx context.Context, req pluginruntime.StartPluginRequest) (*pluginruntime.HostedPlugin, error) {
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
	return r.startFakeHostedAgent(req)
}

func (r *capturingHostedAgentRuntime) Close() error {
	r.mu.Lock()
	sessionIDs := make([]string, 0, len(r.fakeAgents))
	for sessionID := range r.fakeAgents {
		sessionIDs = append(sessionIDs, sessionID)
	}
	r.mu.Unlock()
	for _, sessionID := range sessionIDs {
		r.cleanupFakeHostedAgent(sessionID)
	}
	return r.provider.Close()
}

func (r *capturingHostedAgentRuntime) startPluginRequestsCopy() []pluginruntime.StartPluginRequest {
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

func (r *capturingHostedAgentRuntime) startFakeHostedAgent(req pluginruntime.StartPluginRequest) (*pluginruntime.HostedPlugin, error) {
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
	for key, value := range r.extraEnv {
		if env == nil {
			env = map[string]string{}
		}
		env[key] = value
	}
	r.mu.Unlock()

	dir, err := providerhost.NewPluginTempDir("gstp-fake-agent-")
	if err != nil {
		return nil, fmt.Errorf("create fake hosted agent dir: %w", err)
	}
	socketPath := filepath.Join(dir, "agent.sock")
	lis, err := net.Listen("unix", socketPath)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("listen for fake hosted agent: %w", err)
	}

	srv := grpc.NewServer()
	proto.RegisterAgentProviderServer(srv, &fakeHostedAgentProviderServer{env: env})
	proto.RegisterProviderLifecycleServer(srv, &fakeHostedAgentLifecycleServer{name: req.PluginName})
	go func() {
		_ = srv.Serve(lis)
	}()

	r.mu.Lock()
	r.fakeAgents[req.SessionID] = &fakeHostedAgentServer{
		dir:      dir,
		env:      env,
		listener: lis,
		server:   srv,
	}
	r.mu.Unlock()

	return &pluginruntime.HostedPlugin{
		ID:         req.SessionID,
		SessionID:  req.SessionID,
		PluginName: req.PluginName,
		DialTarget: "unix://" + socketPath,
	}, nil
}

func (r *capturingHostedAgentRuntime) cleanupFakeHostedAgent(sessionID string) {
	r.mu.Lock()
	server := r.fakeAgents[sessionID]
	delete(r.fakeAgents, sessionID)
	r.mu.Unlock()
	if server == nil {
		return
	}
	server.server.Stop()
	_ = server.listener.Close()
	_ = os.RemoveAll(server.dir)
}

type fakeHostedAgentLifecycleServer struct {
	proto.UnimplementedProviderLifecycleServer
	name string
}

func (s *fakeHostedAgentLifecycleServer) GetProviderIdentity(context.Context, *emptypb.Empty) (*proto.ProviderIdentity, error) {
	return &proto.ProviderIdentity{
		Kind:               proto.ProviderKind_PROVIDER_KIND_AGENT,
		Name:               s.name,
		DisplayName:        "Fake Hosted Agent",
		Description:        "test-only fake hosted agent",
		Version:            "test",
		MinProtocolVersion: proto.CurrentProtocolVersion,
		MaxProtocolVersion: proto.CurrentProtocolVersion,
	}, nil
}

func (s *fakeHostedAgentLifecycleServer) ConfigureProvider(context.Context, *proto.ConfigureProviderRequest) (*proto.ConfigureProviderResponse, error) {
	return &proto.ConfigureProviderResponse{ProtocolVersion: proto.CurrentProtocolVersion}, nil
}

func (s *fakeHostedAgentLifecycleServer) HealthCheck(context.Context, *emptypb.Empty) (*proto.HealthCheckResponse, error) {
	return &proto.HealthCheckResponse{Ready: true}, nil
}

type fakeHostedAgentProviderServer struct {
	proto.UnimplementedAgentProviderServer
	env map[string]string
}

func (s *fakeHostedAgentProviderServer) StartRun(ctx context.Context, req *proto.StartAgentProviderRunRequest) (*proto.BoundAgentRun, error) {
	runID := strings.TrimSpace(req.GetRunId())
	if runID == "" {
		runID = "agent-run-1"
	}
	output := map[string]any{
		"provider_name": req.GetProviderName(),
	}
	if proxyTargetURL := strings.TrimSpace(s.env["GESTALT_FAKE_PROXY_TARGET_URL"]); proxyTargetURL != "" {
		resp, err := fakeHostedMakeHTTPRequest(proxyTargetURL, s.env)
		if err != nil {
			output["proxy_error"] = err.Error()
		} else {
			output["proxy_status"] = resp["status"]
			output["proxy_body"] = resp["body"]
		}
	}
	if len(req.GetTools()) > 0 {
		client, conn, err := dialFakeAgentHost(ctx, s.env[providerhost.DefaultAgentHostSocketEnv], s.env[providerhost.DefaultAgentHostSocketEnv+"_TOKEN"])
		if err != nil {
			output["host_error"] = err.Error()
		} else {
			defer func() { _ = conn.Close() }()
			arguments, err := structpb.NewStruct(map[string]any{"taskId": "task-123"})
			if err != nil {
				return nil, fmt.Errorf("build tool arguments: %w", err)
			}
			resp, err := client.ExecuteTool(ctx, &proto.ExecuteAgentToolRequest{
				RunId:      runID,
				ToolCallId: "call-1",
				ToolId:     req.GetTools()[0].GetId(),
				Arguments:  arguments,
			})
			if err != nil {
				output["tool_error"] = err.Error()
			} else {
				output["tool_status"] = resp.GetStatus()
				output["tool_body"] = resp.GetBody()
			}
		}
	}

	body, err := json.Marshal(output)
	if err != nil {
		return nil, fmt.Errorf("marshal output: %w", err)
	}
	now := timestamppb.Now()
	return &proto.BoundAgentRun{
		Id:           runID,
		ProviderName: req.GetProviderName(),
		Model:        req.GetModel(),
		Status:       proto.AgentRunStatus_AGENT_RUN_STATUS_SUCCEEDED,
		OutputText:   string(body),
		SessionRef:   req.GetSessionRef(),
		CreatedBy:    req.GetCreatedBy(),
		CreatedAt:    now,
		StartedAt:    now,
		CompletedAt:  now,
		ExecutionRef: req.GetExecutionRef(),
	}, nil
}

func dialFakeAgentHost(ctx context.Context, target, token string) (proto.AgentHostClient, *grpc.ClientConn, error) {
	network, address, err := parseFakeManagerTarget("agent host", target)
	if err != nil {
		return nil, nil, err
	}
	opts := make([]grpc.DialOption, 0, 4)
	switch network {
	case "unix":
		opts = append(opts,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", address)
			}),
			grpc.WithAuthority("localhost"),
		)
		conn, err := grpc.NewClient("passthrough:///localhost", opts...)
		if err != nil {
			return nil, nil, err
		}
		conn.Connect()
		return proto.NewAgentHostClient(conn), conn, nil
	case "tls":
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return nil, nil, fmt.Errorf("agent host: parse tls target %q: %w", address, err)
		}
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			MinVersion:         tls.VersionTLS12,
			ServerName:         host,
			InsecureSkipVerify: true, // Test-only relay harness certificate.
			NextProtos:         []string{"h2"},
		})))
		if strings.TrimSpace(token) != "" {
			opts = append(opts, grpc.WithPerRPCCredentials(fakeRelayPerRPCCredentials{token: token}))
		}
		conn, err := grpc.NewClient(address, opts...)
		if err != nil {
			return nil, nil, err
		}
		conn.Connect()
		return proto.NewAgentHostClient(conn), conn, nil
	default:
		return nil, nil, fmt.Errorf("agent host: unsupported transport network %q", network)
	}
}

func parseFakeManagerTarget(serviceName, raw string) (network string, address string, err error) {
	target := strings.TrimSpace(raw)
	if target == "" {
		return "", "", fmt.Errorf("%s: transport target is required", serviceName)
	}
	switch {
	case strings.HasPrefix(target, "tls://"):
		address = strings.TrimSpace(strings.TrimPrefix(target, "tls://"))
		if address == "" {
			return "", "", fmt.Errorf("%s: tls target %q is missing host:port", serviceName, raw)
		}
		return "tls", address, nil
	case strings.HasPrefix(target, "unix://"):
		address = strings.TrimSpace(strings.TrimPrefix(target, "unix://"))
		if address == "" {
			return "", "", fmt.Errorf("%s: unix target %q is missing a socket path", serviceName, raw)
		}
		return "unix", address, nil
	default:
		return "", "", fmt.Errorf("%s: unsupported target %q", serviceName, raw)
	}
}

type fakeRelayPerRPCCredentials struct {
	token string
}

func (c fakeRelayPerRPCCredentials) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{
		"x-gestalt-host-service-relay-token": c.token,
	}, nil
}

func (fakeRelayPerRPCCredentials) RequireTransportSecurity() bool { return false }

func TestAgentRuntimeConfigSelectedProviderStartsSessionWithRuntimeFields(t *testing.T) {
	t.Parallel()

	bin := buildAgentProviderBinary(t)
	runtimeProvider := newCapturingPluginRuntime()
	ctxSentinel := &struct{}{}
	var factoryContextValue any

	factories := NewFactoryRegistry()
	factories.Runtime = func(ctx context.Context, _ string, _ *config.RuntimeProviderEntry, _ Deps) (pluginruntime.Provider, error) {
		factoryContextValue = ctx.Value(agentRuntimeFactoryContextKey{})
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
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				"simple": {
					Command: bin,
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
		AgentRuntime:          &agentRuntime{providers: map[string]coreagent.Provider{}},
		PluginRuntimeRegistry: newPluginRuntimeRegistry(cfg, factories.Runtime, Deps{}),
	}
	buildCtx := context.WithValue(context.Background(), agentRuntimeFactoryContextKey{}, ctxSentinel)
	agents, err := buildAgents(buildCtx, cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildAgents: %v", err)
	}
	defer func() {
		if err := closeAgents(agents...); err != nil {
			t.Fatalf("closeAgents: %v", err)
		}
	}()

	requests := runtimeProvider.startSessionRequests()
	if len(requests) != 1 {
		t.Fatalf("start session requests = %d, want 1", len(requests))
	}
	req := requests[0]
	if req.PluginName != "simple" {
		t.Fatalf("StartSession PluginName = %q, want simple", req.PluginName)
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
	if req.Metadata["provider_kind"] != "agent" {
		t.Fatalf("StartSession Metadata[provider_kind] = %q, want agent", req.Metadata["provider_kind"])
	}
	if req.Metadata["provider_name"] != "simple" {
		t.Fatalf("StartSession Metadata[provider_name] = %q, want simple", req.Metadata["provider_name"])
	}
	if factoryContextValue != ctxSentinel {
		t.Fatalf("runtime factory context value = %#v, want %#v", factoryContextValue, ctxSentinel)
	}
}

func TestAgentRuntimeConfigUsesDirectAgentHostBinding(t *testing.T) {
	t.Parallel()

	bin := buildAgentProviderBinary(t)
	secret := []byte("0123456789abcdef0123456789abcdef")

	encryptor, err := corecrypto.NewAESGCM(secret)
	if err != nil {
		t.Fatalf("corecrypto.NewAESGCM: %v", err)
	}
	services, err := coredata.New(&coretesting.StubIndexedDB{}, encryptor)
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	invoker := &recordingAgentRuntimeInvoker{}
	agentRuntime := &agentRuntime{providers: map[string]coreagent.Provider{}}
	agentRuntime.SetInvoker(invoker)
	agentRuntime.SetRunMetadata(services.AgentRunMetadata)
	agentRuntime.SetRunEvents(services.AgentRunEvents)
	agentRuntime.SetRunInteractions(services.AgentRunInteractions)
	capturingRuntime := newCapturingPluginRuntime()

	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return capturingRuntime, nil
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
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				"simple": {
					Command: bin,
					Runtime: &config.HostedRuntimeConfig{},
				},
			},
		},
	}

	deps := Deps{
		Services:     services,
		AgentRuntime: agentRuntime,
	}
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)

	agents, err := buildAgents(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildAgents: %v", err)
	}
	defer func() {
		if err := closeAgents(agents...); err != nil {
			t.Fatalf("closeAgents: %v", err)
		}
	}()
	if len(agents) != 1 {
		t.Fatalf("agents len = %d, want 1", len(agents))
	}
	capabilities, err := agents[0].GetCapabilities(context.Background(), coreagent.GetCapabilitiesRequest{})
	if err != nil {
		t.Fatalf("GetCapabilities: %v", err)
	}
	if capabilities == nil || !capabilities.Approvals || !capabilities.ResumableRuns {
		t.Fatalf("capabilities = %#v, want approvals+resumable_runs", capabilities)
	}

	run, err := agents[0].StartRun(context.Background(), coreagent.StartRunRequest{
		RunID:        "run-1",
		ProviderName: "simple",
		CreatedBy:    coreagent.Actor{SubjectID: "user:user-123"},
		Tools: []coreagent.Tool{{
			ID:   "lookup",
			Name: "Lookup roadmap task",
			Target: coreagent.ToolTarget{
				PluginName: "roadmap",
				Operation:  "sync",
			},
		}},
	})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if run == nil {
		t.Fatal("StartRun returned nil run")
	}

	var output struct {
		ProviderName string `json:"provider_name"`
		ToolStatus   int    `json:"tool_status"`
		ToolBody     string `json:"tool_body"`
		EventEmitted bool   `json:"event_emitted"`
		HostError    string `json:"host_error"`
		ToolError    string `json:"tool_error"`
		EventError   string `json:"event_error"`
	}
	if err := json.Unmarshal([]byte(run.OutputText), &output); err != nil {
		t.Fatalf("json.Unmarshal(run.OutputText): %v", err)
	}
	if output.ProviderName != "simple" {
		t.Fatalf("provider_name = %q, want simple", output.ProviderName)
	}
	if output.ToolStatus != http.StatusAccepted {
		t.Fatalf("tool_status = %d, want %d (output=%s)", output.ToolStatus, http.StatusAccepted, run.OutputText)
	}
	if output.ToolBody != `{"taskId":"task-123"}` {
		t.Fatalf("tool_body = %q, want %q", output.ToolBody, `{"taskId":"task-123"}`)
	}
	if !output.EventEmitted {
		t.Fatal("event_emitted = false, want true")
	}
	if output.HostError != "" || output.ToolError != "" || output.EventError != "" {
		t.Fatalf("runtime callback errors = %+v", output)
	}

	calls := invoker.Calls()
	if len(calls) != 1 {
		t.Fatalf("invoker calls = %d, want 1", len(calls))
	}
	if calls[0].providerName != "roadmap" || calls[0].operation != "sync" {
		t.Fatalf("invoker call = %+v", calls[0])
	}
	if calls[0].params["taskId"] != "task-123" {
		t.Fatalf("invoker params = %#v, want taskId=task-123", calls[0].params)
	}
	if calls[0].subjectID != "user:user-123" {
		t.Fatalf("invoker subject_id = %q, want user:user-123", calls[0].subjectID)
	}

	events, err := services.AgentRunEvents.ListByRun(context.Background(), "run-1", 0, 10)
	if err != nil {
		t.Fatalf("AgentRunEvents.ListByRun: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("agent run events = %d, want 1", len(events))
	}
	if events[0].Type != "agent.test" {
		t.Fatalf("event type = %q, want agent.test", events[0].Type)
	}
	if events[0].Data["provider_name"] != "simple" {
		t.Fatalf("event data = %#v, want provider_name=simple", events[0].Data)
	}

	bindRequests := capturingRuntime.bindHostServiceRequests()
	if len(bindRequests) != 1 {
		t.Fatalf("bind host service requests = %d, want 1", len(bindRequests))
	}
	if bindRequests[0].EnvVar != providerhost.DefaultAgentHostSocketEnv {
		t.Fatalf("BindHostService EnvVar = %q, want %q", bindRequests[0].EnvVar, providerhost.DefaultAgentHostSocketEnv)
	}
	if got := bindRequests[0].Relay.DialTarget; !strings.HasPrefix(got, "unix://") {
		t.Fatalf("BindHostService relay target = %q, want unix relay target", got)
	}

	pausedRun, err := agents[0].StartRun(context.Background(), coreagent.StartRunRequest{
		RunID:        "run-2",
		ProviderName: "simple",
		CreatedBy:    coreagent.Actor{SubjectID: "user:user-123"},
		Metadata: map[string]any{
			"requireInteraction": true,
		},
	})
	if err != nil {
		t.Fatalf("StartRun(waiting): %v", err)
	}
	if pausedRun == nil {
		t.Fatal("StartRun(waiting) returned nil run")
	}
	if pausedRun.Status != coreagent.RunStatusWaitingForInput {
		t.Fatalf("paused run status = %q, want %q", pausedRun.Status, coreagent.RunStatusWaitingForInput)
	}
	var pausedOutput struct {
		InteractionRequested bool   `json:"interaction_requested"`
		InteractionID        string `json:"interaction_id"`
		InteractionError     string `json:"interaction_error"`
	}
	if err := json.Unmarshal([]byte(pausedRun.OutputText), &pausedOutput); err != nil {
		t.Fatalf("json.Unmarshal(pausedRun.OutputText): %v", err)
	}
	if !pausedOutput.InteractionRequested || strings.TrimSpace(pausedOutput.InteractionID) == "" || pausedOutput.InteractionError != "" {
		t.Fatalf("paused run output = %+v", pausedOutput)
	}
	interactions, err := services.AgentRunInteractions.ListByRun(context.Background(), "run-2")
	if err != nil {
		t.Fatalf("AgentRunInteractions.ListByRun: %v", err)
	}
	if len(interactions) != 1 {
		t.Fatalf("agent run interactions = %d, want 1", len(interactions))
	}
	if interactions[0].Type != coreagent.InteractionTypeApproval || interactions[0].State != coreagent.InteractionStatePending {
		t.Fatalf("interaction = %#v, want pending approval", interactions[0])
	}
	resumedRun, err := agents[0].ResumeRun(context.Background(), coreagent.ResumeRunRequest{
		RunID:         "run-2",
		InteractionID: interactions[0].ID,
		Resolution: map[string]any{
			"approved": true,
		},
	})
	if err != nil {
		t.Fatalf("ResumeRun: %v", err)
	}
	if resumedRun == nil || resumedRun.Status != coreagent.RunStatusSucceeded || resumedRun.StatusMessage != interactions[0].ID {
		t.Fatalf("resumed run = %#v, want succeeded status_message=%q", resumedRun, interactions[0].ID)
	}
	resolvedInteractions, err := services.AgentRunInteractions.ListByRun(context.Background(), "run-2")
	if err != nil {
		t.Fatalf("AgentRunInteractions.ListByRun(resolved): %v", err)
	}
	if len(resolvedInteractions) != 1 || resolvedInteractions[0].State != coreagent.InteractionStateResolved || resolvedInteractions[0].Resolution["approved"] != true {
		t.Fatalf("resolved interactions = %#v, want resolved approved interaction", resolvedInteractions)
	}
}

//nolint:paralleltest // Hosted public-relay startup is serialized to avoid Linux CI contention.
func TestAgentRuntimeConfigUsesPublicAgentHostRelayBinding(t *testing.T) {
	// This exercises the hosted agent startup path over the public relay and is
	// sensitive to Linux CI contention when it runs alongside the other hosted
	// runtime bootstrap tests.

	bin := buildAgentProviderBinary(t)
	secret := []byte("0123456789abcdef0123456789abcdef")
	relaySrv := httptest.NewUnstartedServer(newRuntimeRelayTestHandler(t, secret))
	relaySrv.EnableHTTP2 = true
	relaySrv.StartTLS()
	testutil.CloseOnCleanup(t, relaySrv)

	runtimeProvider := newCapturingBundlePluginRuntime()
	runtimeProvider.support.HostServiceAccess = pluginruntime.HostServiceAccessNone
	runtimeProvider.support.EgressMode = pluginruntime.EgressModeHostname
	runtimeProvider.support.LaunchMode = pluginruntime.LaunchModeHostPath

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
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				"simple": {
					Command: bin,
					Runtime: &config.HostedRuntimeConfig{},
				},
			},
		},
	}

	services := coretesting.NewStubServices(t)
	runtimeState := &agentRuntime{providers: map[string]coreagent.Provider{}}
	runtimeState.SetRunMetadata(services.AgentRunMetadata)
	runtimeState.SetRunEvents(services.AgentRunEvents)
	runtimeState.SetRunInteractions(services.AgentRunInteractions)
	deps := Deps{
		BaseURL:       relaySrv.URL,
		EncryptionKey: secret,
		AgentRuntime:  runtimeState,
	}
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)

	agents, err := buildAgents(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildAgents: %v", err)
	}
	defer func() {
		if err := closeAgents(agents...); err != nil {
			t.Fatalf("closeAgents: %v", err)
		}
	}()

	run, err := agents[0].StartRun(context.Background(), coreagent.StartRunRequest{
		RunID:        "run-1",
		ProviderName: "simple",
	})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if run == nil || run.OutputText != `{"provider_name":"simple"}` {
		t.Fatalf("run = %#v, want provider-only output", run)
	}

	bindRequests := runtimeProvider.bindHostServiceRequests()
	if len(bindRequests) != 1 {
		t.Fatalf("bind host service requests = %d, want 1", len(bindRequests))
	}
	if bindRequests[0].EnvVar != providerhost.DefaultAgentHostSocketEnv {
		t.Fatalf("BindHostService EnvVar = %q, want %q", bindRequests[0].EnvVar, providerhost.DefaultAgentHostSocketEnv)
	}
	if got := bindRequests[0].Relay.DialTarget; got != "tls://"+relaySrv.Listener.Addr().String() {
		t.Fatalf("BindHostService relay target = %q, want tls relay target", got)
	}

	startRequests := runtimeProvider.startPluginRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("start plugin requests = %d, want 1", len(startRequests))
	}
	if got := startRequests[0].Env[providerhost.DefaultAgentHostSocketEnv+"_TOKEN"]; strings.TrimSpace(got) == "" {
		t.Fatalf("StartPlugin env missing %s_TOKEN: %#v", providerhost.DefaultAgentHostSocketEnv, startRequests[0].Env)
	}
	if got := startRequests[0].Env["HTTP_PROXY"]; got != "" {
		t.Fatalf("StartPlugin HTTP_PROXY = %q, want empty when relay access does not require hostname egress", got)
	}
	if got := startRequests[0].Env["HTTPS_PROXY"]; got != "" {
		t.Fatalf("StartPlugin HTTPS_PROXY = %q, want empty when relay access does not require hostname egress", got)
	}
	if got := startRequests[0].AllowedHosts; len(got) != 0 {
		t.Fatalf("StartPlugin allowed hosts = %#v, want empty when relay access is not enforcing hostname egress", got)
	}
}

//nolint:paralleltest // Hosted public-relay startup is serialized to avoid Linux CI contention.
func TestAgentRuntimeConfigUsesPublicEgressProxyWhenHostnameEgressIsRequired(t *testing.T) {
	bin := buildAgentProviderBinary(t)
	secret := []byte("0123456789abcdef0123456789abcdef")
	relayHandler := newRuntimeRelayTestHandler(t, secret)
	proxyHandler := newRuntimeEgressProxyTestHandler(t, secret)
	relaySrv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(r.Header.Get(providerhost.HostServiceRelayTokenHeader)) != "" {
			relayHandler.ServeHTTP(w, r)
			return
		}
		proxyHandler.ServeHTTP(w, r)
	}))
	relaySrv.EnableHTTP2 = true
	relaySrv.StartTLS()
	testutil.CloseOnCleanup(t, relaySrv)

	targetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/proxy-test" {
			t.Fatalf("target path = %q, want /proxy-test", got)
		}
		_, _ = w.Write([]byte("agent-egress-proxy-ok"))
	}))
	testutil.CloseOnCleanup(t, targetSrv)

	runtimeProvider := newCapturingHostedAgentRuntime()
	runtimeProvider.support.HostServiceAccess = pluginruntime.HostServiceAccessNone
	runtimeProvider.support.EgressMode = pluginruntime.EgressModeHostname
	runtimeProvider.extraEnv = map[string]string{
		"GESTALT_FAKE_PROXY_TARGET_URL": targetSrv.URL + "/proxy-test",
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
				"hosted": {Driver: config.RuntimeProviderDriver("capture")},
			},
		},
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				"simple": {
					Command: bin,
					Runtime: &config.HostedRuntimeConfig{},
					AllowedHosts: []string{
						"127.0.0.1",
						"localhost",
					},
				},
			},
		},
	}

	services := coretesting.NewStubServices(t)
	invoker := &recordingAgentRuntimeInvoker{}
	runtimeState := &agentRuntime{providers: map[string]coreagent.Provider{}}
	runtimeState.SetInvoker(invoker)
	runtimeState.SetRunMetadata(services.AgentRunMetadata)
	runtimeState.SetRunEvents(services.AgentRunEvents)
	deps := Deps{
		BaseURL:       relaySrv.URL,
		EncryptionKey: secret,
		Egress:        EgressDeps{DefaultAction: egress.PolicyDeny},
		AgentRuntime:  runtimeState,
	}
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)

	agents, err := buildAgents(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildAgents: %v", err)
	}
	defer func() {
		if err := closeAgents(agents...); err != nil {
			t.Fatalf("closeAgents: %v", err)
		}
	}()

	run, err := agents[0].StartRun(context.Background(), coreagent.StartRunRequest{
		RunID:        "run-1",
		ProviderName: "simple",
		CreatedBy:    coreagent.Actor{SubjectID: "user:user-123"},
		Tools: []coreagent.Tool{{
			ID:   "lookup",
			Name: "Lookup roadmap task",
			Target: coreagent.ToolTarget{
				PluginName: "roadmap",
				Operation:  "sync",
			},
		}},
	})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	var output struct {
		ProviderName string `json:"provider_name"`
		ProxyStatus  int    `json:"proxy_status"`
		ProxyBody    string `json:"proxy_body"`
		ToolStatus   int    `json:"tool_status"`
		ToolBody     string `json:"tool_body"`
		ProxyError   string `json:"proxy_error"`
		HostError    string `json:"host_error"`
		ToolError    string `json:"tool_error"`
	}
	if err := json.Unmarshal([]byte(run.OutputText), &output); err != nil {
		t.Fatalf("json.Unmarshal(run.OutputText): %v", err)
	}
	if output.ProviderName != "simple" {
		t.Fatalf("provider_name = %q, want simple", output.ProviderName)
	}
	if output.ProxyStatus != http.StatusOK {
		t.Fatalf("proxy_status = %d, want %d (output=%s)", output.ProxyStatus, http.StatusOK, run.OutputText)
	}
	if output.ProxyBody != "agent-egress-proxy-ok" {
		t.Fatalf("proxy_body = %q, want %q", output.ProxyBody, "agent-egress-proxy-ok")
	}
	if output.ToolStatus != http.StatusAccepted {
		t.Fatalf("tool_status = %d, want %d (output=%s)", output.ToolStatus, http.StatusAccepted, run.OutputText)
	}
	if output.ToolBody != `{"taskId":"task-123"}` {
		t.Fatalf("tool_body = %q, want %q", output.ToolBody, `{"taskId":"task-123"}`)
	}
	if output.ProxyError != "" || output.HostError != "" || output.ToolError != "" {
		t.Fatalf("runtime callback errors = %+v", output)
	}
	calls := invoker.Calls()
	if len(calls) != 1 {
		t.Fatalf("invoker calls = %d, want 1", len(calls))
	}
	if calls[0].providerName != "roadmap" || calls[0].operation != "sync" {
		t.Fatalf("invoker call = %#v, want roadmap sync", calls[0])
	}

	startRequests := runtimeProvider.startPluginRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("start plugin requests = %d, want 1", len(startRequests))
	}
	if got := startRequests[0].Env[providerhost.DefaultAgentHostSocketEnv+"_TOKEN"]; strings.TrimSpace(got) == "" {
		t.Fatalf("StartPlugin env missing %s_TOKEN: %#v", providerhost.DefaultAgentHostSocketEnv, startRequests[0].Env)
	}
	httpProxy := startRequests[0].Env["HTTP_PROXY"]
	httpsProxy := startRequests[0].Env["HTTPS_PROXY"]
	if httpProxy == "" {
		t.Fatal("StartPlugin env should include HTTP_PROXY when hostname egress is required")
	}
	if httpsProxy == "" {
		t.Fatal("StartPlugin env should include HTTPS_PROXY when hostname egress is required")
	}
	if httpProxy != httpsProxy {
		t.Fatalf("HTTP_PROXY = %q, HTTPS_PROXY = %q, want matching values", httpProxy, httpsProxy)
	}
	parsed, err := url.Parse(httpProxy)
	if err != nil {
		t.Fatalf("parse HTTP_PROXY: %v", err)
	}
	if parsed.Host != relaySrv.Listener.Addr().String() {
		t.Fatalf("HTTP_PROXY host = %q, want %q", parsed.Host, relaySrv.Listener.Addr().String())
	}
	if parsed.User == nil {
		t.Fatal("HTTP_PROXY should include relay credentials")
	}
	if got := startRequests[0].AllowedHosts; !slices.Contains(got, parsed.Hostname()) {
		t.Fatalf("StartPlugin allowed hosts = %#v, want relay host %q", got, parsed.Hostname())
	}
	if got := startRequests[0].AllowedHosts; !slices.Contains(got, "127.0.0.1") {
		t.Fatalf("StartPlugin allowed hosts = %#v, want explicit proxy target host 127.0.0.1", got)
	}
}

func TestAgentRuntimeConfigRejectsMissingHostServiceAccess(t *testing.T) {
	t.Parallel()

	bin := buildAgentProviderBinary(t)
	runtimeProvider := &staticCapabilityPluginRuntime{
		inner: newCapturingPluginRuntime(),
		support: pluginruntime.Support{
			CanHostPlugins: true,
			LaunchMode:     pluginruntime.LaunchModeHostPath,
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
				"hosted": {Driver: config.RuntimeProviderDriver("capture")},
			},
		},
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				"simple": {
					Command: bin,
					Runtime: &config.HostedRuntimeConfig{},
				},
			},
		},
	}

	deps := Deps{
		AgentRuntime: &agentRuntime{providers: map[string]coreagent.Provider{}},
	}
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)

	_, err := buildAgents(context.Background(), cfg, factories, deps)
	if err == nil {
		t.Fatal("buildAgents error = nil, want host service access failure")
	}
	if got := err.Error(); got != `bootstrap: agent from resource "simple": agent provider: runtime provider "hosted" cannot provide host service access required by this provider` {
		t.Fatalf("buildAgents error = %q", got)
	}
}
