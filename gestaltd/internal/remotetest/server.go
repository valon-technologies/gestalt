package remotetest

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/remote"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const DefaultToken = "gst_api_plan6_test"

// Server is a narrow fake remote gestaltd exposing generated public gRPC APIs.
type Server struct {
	Token    string
	Recorder *Recorder

	listener net.Listener
	grpc    *grpc.Server
}

// Recorder captures inbound public RPC metadata and payloads.
type Recorder struct {
	mu sync.Mutex

	AppInvokes      []AppInvokeRecord
	AgentCreates    []AgentCreateRecord
	WorkflowStarts  []WorkflowStartRecord
	IndexedDBGets   []IndexedDBGetRecord
	AuthFailures    int
}

type AppInvokeRecord struct {
	Authorization string
	App           string
	Operation     string
}

type AgentCreateRecord struct {
	Authorization string
	ProviderName  string
}

type WorkflowStartRecord struct {
	Authorization string
	ProviderName  string
}

type IndexedDBGetRecord struct {
	Authorization string
	HostBinding   string
	Store         string
	Key           string
}

// New starts a fake remote gestaltd on a local TCP port.
func New(t *testing.T, token string) *Server {
	t.Helper()

	token = strings.TrimSpace(token)
	if token == "" {
		token = DefaultToken
	}

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}

	srv := &Server{
		Token:    token,
		Recorder: &Recorder{},
		listener: lis,
		grpc:     grpc.NewServer(),
	}
	proto.RegisterAppServer(srv.grpc, &fakeAppServer{parent: srv})
	proto.RegisterAgentServer(srv.grpc, &fakeAgentServer{parent: srv})
	proto.RegisterWorkflowServer(srv.grpc, &fakeWorkflowServer{parent: srv})
	proto.RegisterIndexedDBServer(srv.grpc, &fakeIndexedDBServer{parent: srv})

	go func() {
		_ = srv.grpc.Serve(lis)
	}()
	t.Cleanup(srv.Close)
	return srv
}

// URL returns an http:// URL suitable for server.remote.
func (s *Server) URL() string {
	return "http://" + s.listener.Addr().String()
}

// NewClientSet dials this fake remote through the production remote client set.
func (s *Server) NewClientSet(ctx context.Context) (*remote.ClientSet, error) {
	return remote.NewClientSet(ctx, remote.Config{
		URL:   s.URL(),
		Token: s.Token,
	})
}

// Close stops the fake remote gestaltd.
func (s *Server) Close() {
	if s == nil {
		return
	}
	if s.grpc != nil {
		s.grpc.Stop()
	}
	if s.listener != nil {
		_ = s.listener.Close()
	}
}

// AppInvokesSnapshot returns a copy of recorded app invokes.
func (r *Recorder) AppInvokesSnapshot() []AppInvokeRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]AppInvokeRecord, len(r.AppInvokes))
	copy(out, r.AppInvokes)
	return out
}

// AgentCreatesSnapshot returns a copy of recorded agent session creates.
func (r *Recorder) AgentCreatesSnapshot() []AgentCreateRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]AgentCreateRecord, len(r.AgentCreates))
	copy(out, r.AgentCreates)
	return out
}

// WorkflowStartsSnapshot returns a copy of recorded workflow starts.
func (r *Recorder) WorkflowStartsSnapshot() []WorkflowStartRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]WorkflowStartRecord, len(r.WorkflowStarts))
	copy(out, r.WorkflowStarts)
	return out
}

// IndexedDBGetsSnapshot returns a copy of recorded indexeddb gets.
func (r *Recorder) IndexedDBGetsSnapshot() []IndexedDBGetRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]IndexedDBGetRecord, len(r.IndexedDBGets))
	copy(out, r.IndexedDBGets)
	return out
}

// AuthFailureCount reports rejected requests missing bearer auth.
func (r *Recorder) AuthFailureCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.AuthFailures
}

func (r *Recorder) recordAuth(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		r.AuthFailures++
		return "", status.Error(codes.Unauthenticated, "missing authorization metadata")
	}
	values := md.Get("authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		r.AuthFailures++
		return "", status.Error(codes.Unauthenticated, "invalid authorization metadata")
	}
	return values[0], nil
}

func (r *Recorder) recordAuthForToken(ctx context.Context, wantToken string) (string, error) {
	auth, err := r.recordAuth(ctx)
	if err != nil {
		return "", err
	}
	want := "Bearer " + strings.TrimSpace(wantToken)
	if auth != want {
		r.mu.Lock()
		r.AuthFailures++
		r.mu.Unlock()
		return "", status.Error(codes.Unauthenticated, "remote token invalid")
	}
	return auth, nil
}

type fakeAppServer struct {
	proto.UnimplementedAppServer
	parent *Server
}

func (s *fakeAppServer) Invoke(ctx context.Context, req *proto.AppInvokeRequest) (*proto.OperationResult, error) {
	auth, err := s.parent.Recorder.recordAuthForToken(ctx, s.parent.Token)
	if err != nil {
		return nil, err
	}
	s.parent.Recorder.mu.Lock()
	s.parent.Recorder.AppInvokes = append(s.parent.Recorder.AppInvokes, AppInvokeRecord{
		Authorization: auth,
		App:           req.GetApp(),
		Operation:     req.GetOperation(),
	})
	s.parent.Recorder.mu.Unlock()
	return &proto.OperationResult{Status: 200, Body: []byte(`{"remote":true}`)}, nil
}

func (s *fakeAppServer) InvokeGraphQL(ctx context.Context, req *proto.AppInvokeGraphQLRequest) (*proto.OperationResult, error) {
	auth, err := s.parent.Recorder.recordAuthForToken(ctx, s.parent.Token)
	if err != nil {
		return nil, err
	}
	s.parent.Recorder.mu.Lock()
	s.parent.Recorder.AppInvokes = append(s.parent.Recorder.AppInvokes, AppInvokeRecord{
		Authorization: auth,
		App:           req.GetApp(),
		Operation:     "graphql",
	})
	s.parent.Recorder.mu.Unlock()
	return &proto.OperationResult{Status: 200}, nil
}

type fakeAgentServer struct {
	proto.UnimplementedAgentServer
	parent *Server
}

func (s *fakeAgentServer) GetCapabilities(ctx context.Context, req *proto.GetAgentProviderCapabilitiesRequest) (*proto.AgentProviderCapabilities, error) {
	if _, err := s.parent.Recorder.recordAuthForToken(ctx, s.parent.Token); err != nil {
		return nil, err
	}
	return &proto.AgentProviderCapabilities{}, nil
}

func (s *fakeAgentServer) CreateSession(ctx context.Context, req *proto.CreateAgentProviderSessionRequest) (*proto.AgentSession, error) {
	auth, err := s.parent.Recorder.recordAuthForToken(ctx, s.parent.Token)
	if err != nil {
		return nil, err
	}
	s.parent.Recorder.mu.Lock()
	s.parent.Recorder.AgentCreates = append(s.parent.Recorder.AgentCreates, AgentCreateRecord{
		Authorization: auth,
		ProviderName:  req.GetProviderName(),
	})
	s.parent.Recorder.mu.Unlock()
	return &proto.AgentSession{Id: "remote-session", ProviderName: req.GetProviderName()}, nil
}

type fakeWorkflowServer struct {
	proto.UnimplementedWorkflowServer
	parent *Server
}

func (s *fakeWorkflowServer) StartRun(ctx context.Context, req *proto.StartWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	auth, err := s.parent.Recorder.recordAuthForToken(ctx, s.parent.Token)
	if err != nil {
		return nil, err
	}
	s.parent.Recorder.mu.Lock()
	s.parent.Recorder.WorkflowStarts = append(s.parent.Recorder.WorkflowStarts, WorkflowStartRecord{
		Authorization: auth,
		ProviderName:  req.GetProviderName(),
	})
	s.parent.Recorder.mu.Unlock()
	return &proto.WorkflowRun{Id: "remote-run", ProviderName: req.GetProviderName()}, nil
}

type fakeIndexedDBServer struct {
	proto.UnimplementedIndexedDBServer
	parent *Server
}

func (s *fakeIndexedDBServer) Get(ctx context.Context, req *proto.ObjectStoreRequest) (*proto.RecordResponse, error) {
	auth, err := s.parent.Recorder.recordAuthForToken(ctx, s.parent.Token)
	if err != nil {
		return nil, err
	}
	binding := ""
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		values := md.Get("x-gestalt-host-binding")
		if len(values) == 1 {
			binding = values[0]
		}
	}
	s.parent.Recorder.mu.Lock()
	s.parent.Recorder.IndexedDBGets = append(s.parent.Recorder.IndexedDBGets, IndexedDBGetRecord{
		Authorization: auth,
		HostBinding:   binding,
		Store:         req.GetStore(),
		Key:           req.GetId(),
	})
	s.parent.Recorder.mu.Unlock()
	return &proto.RecordResponse{Record: &proto.Record{}}, nil
}
