package bootstrap

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/pluginruntime"
	workflowservice "github.com/valon-technologies/gestalt/server/services/workflows"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestHostedWorkflowProviderPoolStartsWorkersFromWorkflowProviderStartup(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	runtimeProvider := newRecordingHostedWorkflowRuntime(t)
	t.Cleanup(func() { _ = runtimeProvider.Close() })

	deps := Deps{
		BaseURL:            "http://127.0.0.1:8080",
		EncryptionKey:      []byte("0123456789abcdef0123456789abcdef"),
		PluginRuntime:      runtimeProvider,
		PublicHostServices: runtimehost.NewPublicHostServiceRegistry(),
	}
	entry := &config.ProviderEntry{
		Execution: &config.ExecutionConfig{
			Mode: config.ExecutionModeHosted,
			Runtime: &config.HostedRuntimeConfig{
				Provider: "gke",
				Metadata: map[string]string{
					"workload": "temporal-workers",
				},
				Pool: &config.HostedRuntimePoolConfig{
					MinReadyInstances:   2,
					MaxReadyInstances:   2,
					StartupTimeout:      "5s",
					HealthCheckInterval: "1m",
					RestartPolicy:       config.HostedRuntimeRestartPolicyNever,
					DrainTimeout:        "50ms",
				},
			},
		},
	}

	bootstrapProvider := newRecordingBootstrapWorkflowProvider()
	provider, err := buildHostedWorkflowProvider(ctx, "temporal", entry, mustNode(t, map[string]any{
		"command": "/bin/temporal-provider",
		"config":  map[string]any{"namespace": "default"},
	}), []runtimehost.HostService{{
		Name:   "workflow_host",
		EnvVar: workflowservice.DefaultHostSocketEnv,
	}}, deps, bootstrapProvider)
	if err != nil {
		t.Fatalf("buildHostedWorkflowProvider: %v", err)
	}
	result := &Result{ExtraWorkflows: []workflow.Provider{provider}}
	t.Cleanup(func() { _ = provider.Close() })
	assertPublicHostServicesVerified(t, deps.PublicHostServices, "workflow_host", workflowservice.DefaultHostSocketEnv)
	executionRefs, ok := provider.(workflow.ExecutionReferenceStore)
	if !ok {
		t.Fatalf("hosted workflow pool does not expose ExecutionReferenceStore")
	}
	ref, err := executionRefs.PutExecutionReference(ctx, &workflow.ExecutionReference{
		ID:           "workflow_schedule:sched-test:ref-test",
		ProviderName: "temporal",
		SubjectID:    "subject-test",
		SubjectKind:  "user",
		Target: workflow.Target{
			Plugin: &workflow.PluginTarget{
				PluginName: "roadmap",
				Operation:  "sync_items",
			},
		},
	})
	if err != nil {
		t.Fatalf("PutExecutionReference: %v", err)
	}
	if ref.ID != "workflow_schedule:sched-test:ref-test" {
		t.Fatalf("PutExecutionReference id = %q, want workflow_schedule:sched-test:ref-test", ref.ID)
	}
	if _, err := executionRefs.GetExecutionReference(ctx, "workflow_schedule:sched-test:ref-test"); err != nil {
		t.Fatalf("GetExecutionReference: %v", err)
	}

	if got := runtimeProvider.startProviderCalls(); got != 0 {
		t.Fatalf("StartProvider calls before StartWorkflowProviders = %d, want 0", got)
	}
	if got := len(runtimeProvider.startPluginRequestsCopy()); got != 0 {
		t.Fatalf("StartPlugin requests before StartWorkflowProviders = %d, want 0", got)
	}

	if err := result.Start(ctx); err != nil {
		t.Fatalf("Result.Start: %v", err)
	}
	if got := runtimeProvider.startProviderCalls(); got != 0 {
		t.Fatalf("StartProvider calls after Result.Start = %d, want 0", got)
	}
	if err := result.StartWorkflowProviders(ctx); err != nil {
		t.Fatalf("StartWorkflowProviders: %v", err)
	}
	waitForHostedWorkflowRuntimeStartProviderCalls(t, runtimeProvider, 2)
	if got := bootstrapProvider.closeCalls.Load(); got != 1 {
		t.Fatalf("bootstrap provider Close calls = %d, want 1", got)
	}

	startRequests := runtimeProvider.startPluginRequestsCopy()
	if len(startRequests) != 2 {
		t.Fatalf("StartPlugin requests after StartWorkflowProviders = %d, want 2 workers", len(startRequests))
	}
	workerReq := startRequests[0]
	if got := workerReq.Env[workflowservice.DefaultHostSocketEnv]; got != "tcp://127.0.0.1:8080" {
		t.Fatalf("worker env %s = %q, want public relay target", workflowservice.DefaultHostSocketEnv, got)
	}
	if got := workerReq.Env[workflowservice.HostSocketTokenEnv()]; got == "" {
		t.Fatalf("worker env missing %s", workflowservice.HostSocketTokenEnv())
	}
	sessions := runtimeProvider.startSessionRequestsCopy()
	if len(sessions) != 2 {
		t.Fatalf("StartSession requests = %d, want 2 workers", len(sessions))
	}
	if got := sessions[0].Metadata["provider_kind"]; got != providermanifestKindWorkflow {
		t.Fatalf("worker session provider_kind = %q, want %q", got, providermanifestKindWorkflow)
	}
	if got := sessions[0].Metadata["provider_name"]; got != "temporal" {
		t.Fatalf("worker session provider_name = %q, want temporal", got)
	}
	if got := sessions[0].Metadata["workload"]; got != "temporal-workers" {
		t.Fatalf("worker session workload = %q, want temporal-workers", got)
	}
}

func TestHostedWorkflowAllowedHostsFiltersLoopbackRelayTargets(t *testing.T) {
	t.Parallel()

	allowed := hostedWorkflowAllowedHosts([]string{"localhost", "127.0.0.1", "api.example.com"}, HostedRuntimePlan{
		Resolved: RuntimeBehavior{
			HostServiceAccess: RuntimeHostServiceAccessRelay,
			EgressMode:        RuntimeEgressModeNone,
		},
	})
	if !slices.Equal(allowed, []string{"api.example.com"}) {
		t.Fatalf("hostedWorkflowAllowedHosts = %#v, want api.example.com only", allowed)
	}
}

func TestHostedWorkflowProviderKeepsSharedRuntimeOpen(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	runtimeProvider := newRecordingHostedWorkflowRuntime(t)
	t.Cleanup(func() { _ = runtimeProvider.Close() })
	deps := Deps{
		BaseURL:            "http://127.0.0.1:8080",
		EncryptionKey:      []byte("0123456789abcdef0123456789abcdef"),
		PluginRuntime:      runtimeProvider,
		PublicHostServices: runtimehost.NewPublicHostServiceRegistry(),
	}
	entry := &config.ProviderEntry{
		Execution: &config.ExecutionConfig{
			Mode: config.ExecutionModeHosted,
			Runtime: &config.HostedRuntimeConfig{
				Provider: "gke",
			},
		},
	}

	provider, err := buildHostedWorkflowProvider(ctx, "temporal", entry, mustNode(t, map[string]any{
		"command": "/bin/temporal-provider",
	}), []runtimehost.HostService{{
		Name:   "workflow_host",
		EnvVar: workflowservice.DefaultHostSocketEnv,
	}}, deps, nil)
	if err != nil {
		t.Fatalf("buildHostedWorkflowProvider: %v", err)
	}
	if err := provider.Close(); err != nil {
		t.Fatalf("provider.Close: %v", err)
	}
	if got := runtimeProvider.closeCalls.Load(); got != 0 {
		t.Fatalf("runtime Close calls after provider.Close = %d, want 0 for shared runtime", got)
	}
}

func TestHostedWorkflowProviderPoolDrainWaitsBeforeClosingWorker(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	runtimeProvider := newRecordingHostedWorkflowRuntime(t)
	t.Cleanup(func() { _ = runtimeProvider.Close() })
	deps := Deps{
		BaseURL:            "http://127.0.0.1:8080",
		EncryptionKey:      []byte("0123456789abcdef0123456789abcdef"),
		PluginRuntime:      runtimeProvider,
		PublicHostServices: runtimehost.NewPublicHostServiceRegistry(),
	}
	entry := &config.ProviderEntry{
		Execution: &config.ExecutionConfig{
			Mode: config.ExecutionModeHosted,
			Runtime: &config.HostedRuntimeConfig{
				Provider: "gke",
				Pool: &config.HostedRuntimePoolConfig{
					MinReadyInstances:   1,
					MaxReadyInstances:   1,
					StartupTimeout:      "5s",
					HealthCheckInterval: "1m",
					RestartPolicy:       config.HostedRuntimeRestartPolicyNever,
					DrainTimeout:        "150ms",
				},
			},
		},
	}

	provider, err := buildHostedWorkflowProvider(ctx, "temporal", entry, mustNode(t, map[string]any{
		"command": "/bin/temporal-provider",
	}), []runtimehost.HostService{{
		Name:   "workflow_host",
		EnvVar: workflowservice.DefaultHostSocketEnv,
	}}, deps, nil)
	if err != nil {
		t.Fatalf("buildHostedWorkflowProvider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	pool, ok := provider.(*hostedWorkflowProviderPool)
	if !ok {
		t.Fatalf("provider = %T, want *hostedWorkflowProviderPool", provider)
	}
	if err := pool.Start(ctx); err != nil {
		t.Fatalf("pool.Start: %v", err)
	}
	workers := waitForHostedWorkflowReadyWorkers(t, pool, 1)
	pool.mu.Lock()
	workers[0].active = 1
	pool.mu.Unlock()

	done := make(chan error, 1)
	go func() {
		done <- pool.drainAndCloseWorker(workers[0])
	}()
	select {
	case err := <-done:
		t.Fatalf("drainAndCloseWorker finished before drain timeout with error %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	pool.mu.Lock()
	workers[0].active = 0
	pool.mu.Unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("drainAndCloseWorker: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("drainAndCloseWorker did not finish after drain timeout")
	}
}

func waitForHostedWorkflowRuntimeStartProviderCalls(t *testing.T, runtimeProvider *recordingHostedWorkflowRuntime, want int32) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := runtimeProvider.startProviderCalls(); got == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("StartProvider calls = %d, want %d", runtimeProvider.startProviderCalls(), want)
}

func waitForHostedWorkflowReadyWorkers(t *testing.T, pool *hostedWorkflowProviderPool, want int) []*hostedWorkflowWorker {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		workers := pool.readyWorkers()
		if len(workers) == want {
			return workers
		}
		time.Sleep(10 * time.Millisecond)
	}
	workers := pool.readyWorkers()
	t.Fatalf("ready workers = %d, want %d", len(workers), want)
	return nil
}

type recordingBootstrapWorkflowProvider struct {
	mu            sync.Mutex
	executionRefs map[string]*workflow.ExecutionReference
	closeCalls    atomic.Int32
}

func newRecordingBootstrapWorkflowProvider() *recordingBootstrapWorkflowProvider {
	return &recordingBootstrapWorkflowProvider{
		executionRefs: map[string]*workflow.ExecutionReference{},
	}
}

func (p *recordingBootstrapWorkflowProvider) StartRun(context.Context, workflow.StartRunRequest) (*workflow.Run, error) {
	return nil, status.Error(codes.Unimplemented, "start run is not implemented")
}

func (p *recordingBootstrapWorkflowProvider) GetRun(context.Context, workflow.GetRunRequest) (*workflow.Run, error) {
	return nil, status.Error(codes.Unimplemented, "get run is not implemented")
}

func (p *recordingBootstrapWorkflowProvider) ListRuns(context.Context, workflow.ListRunsRequest) ([]*workflow.Run, error) {
	return nil, status.Error(codes.Unimplemented, "list runs is not implemented")
}

func (p *recordingBootstrapWorkflowProvider) CancelRun(context.Context, workflow.CancelRunRequest) (*workflow.Run, error) {
	return nil, status.Error(codes.Unimplemented, "cancel run is not implemented")
}

func (p *recordingBootstrapWorkflowProvider) SignalRun(context.Context, workflow.SignalRunRequest) (*workflow.SignalRunResponse, error) {
	return nil, status.Error(codes.Unimplemented, "signal run is not implemented")
}

func (p *recordingBootstrapWorkflowProvider) SignalOrStartRun(context.Context, workflow.SignalOrStartRunRequest) (*workflow.SignalRunResponse, error) {
	return nil, status.Error(codes.Unimplemented, "signal or start run is not implemented")
}

func (p *recordingBootstrapWorkflowProvider) UpsertSchedule(context.Context, workflow.UpsertScheduleRequest) (*workflow.Schedule, error) {
	return nil, status.Error(codes.Unimplemented, "upsert schedule is not implemented")
}

func (p *recordingBootstrapWorkflowProvider) GetSchedule(context.Context, workflow.GetScheduleRequest) (*workflow.Schedule, error) {
	return nil, status.Error(codes.NotFound, "schedule not found")
}

func (p *recordingBootstrapWorkflowProvider) ListSchedules(context.Context, workflow.ListSchedulesRequest) ([]*workflow.Schedule, error) {
	return nil, nil
}

func (p *recordingBootstrapWorkflowProvider) DeleteSchedule(context.Context, workflow.DeleteScheduleRequest) error {
	return status.Error(codes.NotFound, "schedule not found")
}

func (p *recordingBootstrapWorkflowProvider) PauseSchedule(context.Context, workflow.PauseScheduleRequest) (*workflow.Schedule, error) {
	return nil, status.Error(codes.Unimplemented, "pause schedule is not implemented")
}

func (p *recordingBootstrapWorkflowProvider) ResumeSchedule(context.Context, workflow.ResumeScheduleRequest) (*workflow.Schedule, error) {
	return nil, status.Error(codes.Unimplemented, "resume schedule is not implemented")
}

func (p *recordingBootstrapWorkflowProvider) UpsertEventTrigger(context.Context, workflow.UpsertEventTriggerRequest) (*workflow.EventTrigger, error) {
	return nil, status.Error(codes.Unimplemented, "upsert event trigger is not implemented")
}

func (p *recordingBootstrapWorkflowProvider) GetEventTrigger(context.Context, workflow.GetEventTriggerRequest) (*workflow.EventTrigger, error) {
	return nil, status.Error(codes.NotFound, "event trigger not found")
}

func (p *recordingBootstrapWorkflowProvider) ListEventTriggers(context.Context, workflow.ListEventTriggersRequest) ([]*workflow.EventTrigger, error) {
	return nil, nil
}

func (p *recordingBootstrapWorkflowProvider) DeleteEventTrigger(context.Context, workflow.DeleteEventTriggerRequest) error {
	return status.Error(codes.NotFound, "event trigger not found")
}

func (p *recordingBootstrapWorkflowProvider) PauseEventTrigger(context.Context, workflow.PauseEventTriggerRequest) (*workflow.EventTrigger, error) {
	return nil, status.Error(codes.Unimplemented, "pause event trigger is not implemented")
}

func (p *recordingBootstrapWorkflowProvider) ResumeEventTrigger(context.Context, workflow.ResumeEventTriggerRequest) (*workflow.EventTrigger, error) {
	return nil, status.Error(codes.Unimplemented, "resume event trigger is not implemented")
}

func (p *recordingBootstrapWorkflowProvider) PublishEvent(context.Context, workflow.PublishEventRequest) error {
	return status.Error(codes.Unimplemented, "publish event is not implemented")
}

func (p *recordingBootstrapWorkflowProvider) Ping(context.Context) error {
	return nil
}

func (p *recordingBootstrapWorkflowProvider) Close() error {
	p.closeCalls.Add(1)
	return nil
}

func (p *recordingBootstrapWorkflowProvider) PutExecutionReference(_ context.Context, ref *workflow.ExecutionReference) (*workflow.ExecutionReference, error) {
	if ref == nil || ref.ID == "" {
		return nil, status.Error(codes.InvalidArgument, "missing execution reference id")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	cloned := *ref
	p.executionRefs[ref.ID] = &cloned
	out := cloned
	return &out, nil
}

func (p *recordingBootstrapWorkflowProvider) GetExecutionReference(_ context.Context, id string) (*workflow.ExecutionReference, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ref := p.executionRefs[id]
	if ref == nil {
		return nil, status.Error(codes.NotFound, "execution reference not found")
	}
	out := *ref
	return &out, nil
}

func (p *recordingBootstrapWorkflowProvider) ListExecutionReferences(_ context.Context, subjectID string) ([]*workflow.ExecutionReference, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	refs := make([]*workflow.ExecutionReference, 0, len(p.executionRefs))
	for _, ref := range p.executionRefs {
		if subjectID != "" && ref.SubjectID != subjectID {
			continue
		}
		out := *ref
		refs = append(refs, &out)
	}
	return refs, nil
}

const providermanifestKindWorkflow = "workflow"

type recordingHostedWorkflowRuntime struct {
	provider *pluginruntime.LocalProvider
	t        *testing.T

	mu                  sync.Mutex
	startRequests       []pluginruntime.StartSessionRequest
	startPluginRequests []pluginruntime.StartPluginRequest
	servers             map[string]*recordingHostedWorkflowServer
	closeCalls          atomic.Int32
}

func newRecordingHostedWorkflowRuntime(t *testing.T) *recordingHostedWorkflowRuntime {
	t.Helper()
	return &recordingHostedWorkflowRuntime{
		provider: pluginruntime.NewLocalProvider(),
		t:        t,
		servers:  map[string]*recordingHostedWorkflowServer{},
	}
}

func (r *recordingHostedWorkflowRuntime) Support(context.Context) (pluginruntime.Support, error) {
	return pluginruntime.Support{
		CanHostPlugins: true,
		EgressMode:     pluginruntime.EgressModeHostname,
	}, nil
}

func (r *recordingHostedWorkflowRuntime) StartSession(ctx context.Context, req pluginruntime.StartSessionRequest) (*pluginruntime.Session, error) {
	r.mu.Lock()
	r.startRequests = append(r.startRequests, pluginruntime.StartSessionRequest{
		PluginName:    req.PluginName,
		Template:      req.Template,
		Image:         req.Image,
		ImagePullAuth: cloneImagePullAuth(req.ImagePullAuth),
		Metadata:      cloneRuntimeMetadata(req.Metadata),
	})
	r.mu.Unlock()
	return r.provider.StartSession(ctx, req)
}

func (r *recordingHostedWorkflowRuntime) ListSessions(ctx context.Context) ([]pluginruntime.Session, error) {
	return r.provider.ListSessions(ctx)
}

func (r *recordingHostedWorkflowRuntime) GetSession(ctx context.Context, req pluginruntime.GetSessionRequest) (*pluginruntime.Session, error) {
	return r.provider.GetSession(ctx, req)
}

func (r *recordingHostedWorkflowRuntime) StopSession(ctx context.Context, req pluginruntime.StopSessionRequest) error {
	r.cleanupServer(req.SessionID)
	return r.provider.StopSession(ctx, req)
}

func (r *recordingHostedWorkflowRuntime) StartPlugin(_ context.Context, req pluginruntime.StartPluginRequest) (*pluginruntime.HostedPlugin, error) {
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

	dir, err := runtimehost.NewPluginTempDir("gst-workflow-runtime-*")
	if err != nil {
		return nil, fmt.Errorf("create fake hosted workflow dir: %w", err)
	}
	socketPath := filepath.Join(dir, "workflow.sock")
	lis, err := net.Listen("unix", socketPath)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("listen for fake hosted workflow: %w", err)
	}
	workflowServer := newRecordingHostedWorkflowServer()
	grpcServer := grpc.NewServer()
	proto.RegisterProviderLifecycleServer(grpcServer, workflowServer)
	proto.RegisterWorkflowProviderServer(grpcServer, workflowServer)
	go func() {
		_ = grpcServer.Serve(lis)
	}()

	r.mu.Lock()
	r.servers[req.SessionID] = workflowServer
	r.mu.Unlock()
	r.t.Cleanup(func() {
		grpcServer.Stop()
		_ = lis.Close()
		_ = os.RemoveAll(dir)
	})
	return &pluginruntime.HostedPlugin{
		ID:         "fake-" + req.SessionID,
		SessionID:  req.SessionID,
		PluginName: req.PluginName,
		DialTarget: "unix://" + socketPath,
	}, nil
}

func (r *recordingHostedWorkflowRuntime) Close() error {
	r.closeCalls.Add(1)
	r.mu.Lock()
	sessionIDs := make([]string, 0, len(r.servers))
	for sessionID := range r.servers {
		sessionIDs = append(sessionIDs, sessionID)
	}
	r.mu.Unlock()
	for _, sessionID := range sessionIDs {
		r.cleanupServer(sessionID)
	}
	return r.provider.Close()
}

func (r *recordingHostedWorkflowRuntime) startSessionRequestsCopy() []pluginruntime.StartSessionRequest {
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

func (r *recordingHostedWorkflowRuntime) startPluginRequestsCopy() []pluginruntime.StartPluginRequest {
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

func (r *recordingHostedWorkflowRuntime) startProviderCalls() int32 {
	r.mu.Lock()
	servers := make([]*recordingHostedWorkflowServer, 0, len(r.servers))
	for _, server := range r.servers {
		servers = append(servers, server)
	}
	r.mu.Unlock()
	var total int32
	for _, server := range servers {
		total += server.startProviderCalls.Load()
	}
	return total
}

func (r *recordingHostedWorkflowRuntime) cleanupServer(sessionID string) {
	r.mu.Lock()
	delete(r.servers, sessionID)
	r.mu.Unlock()
}

type recordingHostedWorkflowServer struct {
	proto.UnimplementedProviderLifecycleServer
	proto.UnimplementedWorkflowProviderServer

	startProviderCalls atomic.Int32
	mu                 sync.Mutex
	executionRefs      map[string]*proto.WorkflowExecutionReference
}

func newRecordingHostedWorkflowServer() *recordingHostedWorkflowServer {
	return &recordingHostedWorkflowServer{
		executionRefs: map[string]*proto.WorkflowExecutionReference{},
	}
}

func (s *recordingHostedWorkflowServer) GetProviderIdentity(context.Context, *emptypb.Empty) (*proto.ProviderIdentity, error) {
	return &proto.ProviderIdentity{
		Kind:               proto.ProviderKind_PROVIDER_KIND_WORKFLOW,
		Name:               "temporal",
		MinProtocolVersion: proto.CurrentProtocolVersion,
		MaxProtocolVersion: proto.CurrentProtocolVersion,
	}, nil
}

func (s *recordingHostedWorkflowServer) ConfigureProvider(context.Context, *proto.ConfigureProviderRequest) (*proto.ConfigureProviderResponse, error) {
	return &proto.ConfigureProviderResponse{ProtocolVersion: proto.CurrentProtocolVersion}, nil
}

func (s *recordingHostedWorkflowServer) HealthCheck(context.Context, *emptypb.Empty) (*proto.HealthCheckResponse, error) {
	return &proto.HealthCheckResponse{Ready: true}, nil
}

func (s *recordingHostedWorkflowServer) StartProvider(context.Context, *emptypb.Empty) (*proto.StartRuntimeProviderResponse, error) {
	s.startProviderCalls.Add(1)
	return &proto.StartRuntimeProviderResponse{ProtocolVersion: proto.CurrentProtocolVersion}, nil
}

func (s *recordingHostedWorkflowServer) PutExecutionReference(_ context.Context, req *proto.PutWorkflowExecutionReferenceRequest) (*proto.WorkflowExecutionReference, error) {
	ref := req.GetReference()
	if ref.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "missing execution reference id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.executionRefs[ref.GetId()] = gproto.Clone(ref).(*proto.WorkflowExecutionReference)
	return gproto.Clone(ref).(*proto.WorkflowExecutionReference), nil
}

func (s *recordingHostedWorkflowServer) GetExecutionReference(_ context.Context, req *proto.GetWorkflowExecutionReferenceRequest) (*proto.WorkflowExecutionReference, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ref := s.executionRefs[req.GetId()]
	if ref == nil {
		return nil, status.Error(codes.NotFound, "execution reference not found")
	}
	return gproto.Clone(ref).(*proto.WorkflowExecutionReference), nil
}

func (s *recordingHostedWorkflowServer) ListExecutionReferences(_ context.Context, req *proto.ListWorkflowExecutionReferencesRequest) (*proto.ListWorkflowExecutionReferencesResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	refs := make([]*proto.WorkflowExecutionReference, 0, len(s.executionRefs))
	for _, ref := range s.executionRefs {
		if req.GetSubjectId() == "" || ref.GetSubjectId() == req.GetSubjectId() {
			refs = append(refs, gproto.Clone(ref).(*proto.WorkflowExecutionReference))
		}
	}
	return &proto.ListWorkflowExecutionReferencesResponse{References: refs}, nil
}
