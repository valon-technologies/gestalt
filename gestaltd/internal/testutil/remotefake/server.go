package remotefake

import (
	"context"
	"errors"
	"net"
	"sync"

	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Server exposes generated public gRPC APIs for plan-6 remote routing tests.
type Server struct {
	App      *RecordingApp
	Agent    *RecordingAgent
	Workflow *RecordingWorkflow

	baseURL string
	grpc    *grpc.Server
	done    chan error
}

// Start listens on 127.0.0.1:0 and serves public App, Agent, and Workflow APIs.
func Start() (*Server, error) {
	srv := &Server{
		App:      &RecordingApp{},
		Agent:    &RecordingAgent{},
		Workflow: &RecordingWorkflow{},
	}
	srv.grpc = grpc.NewServer()
	publicrpc.RegisterPublicAppServer(srv.grpc, srv.App)
	publicrpc.RegisterPublicAgentServer(srv.grpc, srv.Agent)
	publicrpc.RegisterPublicWorkflowServer(srv.grpc, srv.Workflow)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	srv.baseURL = "http://" + listener.Addr().String()
	srv.done = make(chan error, 1)
	go func() {
		srv.done <- srv.grpc.Serve(listener)
	}()
	return srv, nil
}

// BaseURL returns the HTTP-style remote gestaltd URL accepted by remote.NewClientSet.
func (s *Server) BaseURL() string {
	if s == nil {
		return ""
	}
	return s.baseURL
}

// Close stops the fake remote gestaltd.
func (s *Server) Close() error {
	if s == nil || s.grpc == nil {
		return nil
	}
	s.grpc.GracefulStop()
	err := <-s.done
	if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		return err
	}
	return nil
}

// RecordingApp records public App invocations.
type RecordingApp struct {
	proto.UnimplementedAppServer
	mu       sync.Mutex
	lastAuth string
	calls    []AppCall
}

// AppCall captures one Invoke request observed by the fake remote.
type AppCall struct {
	Auth      string
	App       string
	Operation string
}

func (s *RecordingApp) Invoke(ctx context.Context, req *proto.AppInvokeRequest) (*proto.OperationResult, error) {
	auth := authorizationFromContext(ctx)
	s.mu.Lock()
	s.lastAuth = auth
	s.calls = append(s.calls, AppCall{
		Auth:      auth,
		App:       req.GetApp(),
		Operation: req.GetOperation(),
	})
	s.mu.Unlock()
	if req.GetContext() != nil {
		return nil, status.Error(codes.InvalidArgument, "public invoke must not include request context")
	}
	return &proto.OperationResult{Status: 200, Body: []byte(`{"ok":true}`)}, nil
}

func (s *RecordingApp) Snapshot() (lastAuth string, calls []AppCall) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]AppCall(nil), s.calls...)
	return s.lastAuth, out
}

// RecordingAgent records public Agent invocations.
type RecordingAgent struct {
	proto.UnimplementedAgentServer
	mu       sync.Mutex
	lastAuth string
	calls    []AgentCall
}

// AgentCall captures one Agent RPC observed by the fake remote.
type AgentCall struct {
	Auth   string
	Method string
}

func (s *RecordingAgent) CreateSession(ctx context.Context, req *proto.CreateAgentProviderSessionRequest) (*proto.AgentSession, error) {
	s.record(ctx, "CreateSession")
	return &proto.AgentSession{
		Id:           "remote-session",
		ProviderName: req.GetProviderName(),
		Model:        req.GetModel(),
	}, nil
}

func (s *RecordingAgent) record(ctx context.Context, method string) {
	auth := authorizationFromContext(ctx)
	s.mu.Lock()
	s.lastAuth = auth
	s.calls = append(s.calls, AgentCall{Auth: auth, Method: method})
	s.mu.Unlock()
}

func (s *RecordingAgent) Snapshot() (lastAuth string, calls []AgentCall) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]AgentCall(nil), s.calls...)
	return s.lastAuth, out
}

// RecordingWorkflow records public Workflow invocations.
type RecordingWorkflow struct {
	proto.UnimplementedWorkflowServer
	mu       sync.Mutex
	lastAuth string
	calls    []WorkflowCall
}

// WorkflowCall captures one Workflow RPC observed by the fake remote.
type WorkflowCall struct {
	Auth   string
	Method string
}

func (s *RecordingWorkflow) DeliverEvent(ctx context.Context, _ *proto.DeliverWorkflowProviderEventRequest) (*proto.WorkflowEvent, error) {
	s.record(ctx, "DeliverEvent")
	return &proto.WorkflowEvent{}, nil
}

func (s *RecordingWorkflow) record(ctx context.Context, method string) {
	auth := authorizationFromContext(ctx)
	s.mu.Lock()
	s.lastAuth = auth
	s.calls = append(s.calls, WorkflowCall{Auth: auth, Method: method})
	s.mu.Unlock()
}

func (s *RecordingWorkflow) Snapshot() (lastAuth string, calls []WorkflowCall) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]WorkflowCall(nil), s.calls...)
	return s.lastAuth, out
}

func authorizationFromContext(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	values := md.Get("authorization")
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
