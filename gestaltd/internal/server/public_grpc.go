package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	agentservice "github.com/valon-technologies/gestalt/server/services/agents"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
	workflowservice "github.com/valon-technologies/gestalt/server/services/workflows"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
)

type publicGRPCConfig struct {
	Transport       *providergateway.ProviderGatewayTransport
	Invoker         invocation.Invoker
	AgentManager    agentmanager.Service
	WorkflowManager workflowmanager.Service
	Authorization   core.AuthorizationProvider
}

func buildPublicGRPCHandler(cfg publicGRPCConfig) http.Handler {
	if cfg.Transport == nil || cfg.Invoker == nil {
		return nil
	}
	srv := grpc.NewServer()
	publicrpc.RegisterPublicAppServer(srv, newPublicAdaptedAppServer(cfg.Transport, appaccessservice.NewServer(
		cfg.Invoker,
		appaccessservice.WithAgentAppInvocationAuthorizer(cfg.AgentManager),
	)))
	if cfg.AgentManager != nil {
		publicrpc.RegisterPublicAgentServer(srv, newPublicAdaptedAgentServer(cfg.Transport, agentservice.NewProviderServer(
			"gestaltd",
			cfg.AgentManager,
		)))
	}
	if cfg.WorkflowManager != nil {
		publicrpc.RegisterPublicWorkflowServer(srv, newPublicAdaptedWorkflowServer(cfg.Transport, workflowservice.NewProviderServer(
			"gestaltd",
			cfg.WorkflowManager,
			cfg.Authorization,
			workflowservice.WithAgentWorkflowInvocationAuthorizer(cfg.AgentManager),
		)))
	}
	reflection.Register(srv)
	return http.HandlerFunc(srv.ServeHTTP)
}

func (s *Server) publicGRPCMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s == nil || r == nil {
			next.ServeHTTP(w, r)
			return
		}
		if !isGRPCRequest(r) || s.hostServiceRelayToken(r) != "" {
			next.ServeHTTP(w, r)
			return
		}
		if bearerTokenFromHTTPRequest(r) == "" {
			next.ServeHTTP(w, r)
			return
		}
		handler := s.cachedPublicGRPCHandler()
		if handler == nil {
			writeGRPCTrailersOnly(w, codes.Unauthenticated, "public-grpc-unavailable")
			return
		}
		handler.ServeHTTP(w, r)
	})
}

func (s *Server) cachedPublicGRPCHandler() http.Handler {
	if s == nil || s.publicGatewayTransport == nil || s.invoker == nil {
		return nil
	}
	s.publicGRPCMu.Lock()
	defer s.publicGRPCMu.Unlock()
	if s.publicGRPCHandler != nil {
		return s.publicGRPCHandler
	}
	s.publicGRPCHandler = buildPublicGRPCHandler(publicGRPCConfig{
		Transport:       s.publicGatewayTransport,
		Invoker:         s.invoker,
		AgentManager:    s.agentRuns,
		WorkflowManager: s.workflowSchedules,
		Authorization:   s.authorization,
	})
	return s.publicGRPCHandler
}

func bearerTokenFromHTTPRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(value) > 7 && strings.EqualFold(value[:7], "Bearer ") {
		return strings.TrimSpace(value[7:])
	}
	return ""
}

func preparePublicRequest[T gproto.Message](transport *providergateway.ProviderGatewayTransport, ctx context.Context, fullMethod string, req T) (T, error) {
	var zero T
	if transport == nil {
		return zero, status.Error(codes.Unavailable, "public gateway transport is not configured")
	}
	_, adapted, err := transport.PreparePublicRequest(ctx, fullMethod, req)
	if err != nil {
		return zero, err
	}
	out, ok := adapted.(T)
	if !ok {
		return zero, status.Error(codes.Internal, "adapted request type mismatch")
	}
	return out, nil
}

type publicAdaptedAppServer struct {
	proto.UnimplementedAppServer
	inner     proto.AppServer
	transport *providergateway.ProviderGatewayTransport
}

func newPublicAdaptedAppServer(transport *providergateway.ProviderGatewayTransport, inner proto.AppServer) proto.AppServer {
	return &publicAdaptedAppServer{inner: inner, transport: transport}
}

func (s *publicAdaptedAppServer) Invoke(ctx context.Context, req *proto.AppInvokeRequest) (*proto.OperationResult, error) {
	adapted, err := preparePublicRequest(s.transport, ctx, proto.App_Invoke_FullMethodName, req)
	if err != nil {
		return nil, err
	}
	return s.inner.Invoke(ctx, adapted)
}

func (s *publicAdaptedAppServer) InvokeGraphQL(ctx context.Context, req *proto.AppInvokeGraphQLRequest) (*proto.OperationResult, error) {
	adapted, err := preparePublicRequest(s.transport, ctx, proto.App_InvokeGraphQL_FullMethodName, req)
	if err != nil {
		return nil, err
	}
	return s.inner.InvokeGraphQL(ctx, adapted)
}

type publicAdaptedAgentServer struct {
	proto.UnimplementedAgentServer
	inner     *agentservice.ProviderServer
	transport *providergateway.ProviderGatewayTransport
}

func newPublicAdaptedAgentServer(transport *providergateway.ProviderGatewayTransport, inner *agentservice.ProviderServer) proto.AgentServer {
	return &publicAdaptedAgentServer{inner: inner, transport: transport}
}

func (s *publicAdaptedAgentServer) CreateSession(ctx context.Context, req *proto.CreateAgentProviderSessionRequest) (*proto.AgentSession, error) {
	adapted, err := preparePublicRequest(s.transport, ctx, proto.Agent_CreateSession_FullMethodName, req)
	if err != nil {
		return nil, err
	}
	return s.inner.CreateSession(ctx, adapted)
}

func (s *publicAdaptedAgentServer) GetSession(ctx context.Context, req *proto.GetAgentProviderSessionRequest) (*proto.AgentSession, error) {
	adapted, err := preparePublicRequest(s.transport, ctx, proto.Agent_GetSession_FullMethodName, req)
	if err != nil {
		return nil, err
	}
	return s.inner.GetSession(ctx, adapted)
}

func (s *publicAdaptedAgentServer) ListSessions(ctx context.Context, req *proto.ListAgentProviderSessionsRequest) (*proto.ListAgentProviderSessionsResponse, error) {
	adapted, err := preparePublicRequest(s.transport, ctx, proto.Agent_ListSessions_FullMethodName, req)
	if err != nil {
		return nil, err
	}
	return s.inner.ListSessions(ctx, adapted)
}

func (s *publicAdaptedAgentServer) UpdateSession(ctx context.Context, req *proto.UpdateAgentProviderSessionRequest) (*proto.AgentSession, error) {
	adapted, err := preparePublicRequest(s.transport, ctx, proto.Agent_UpdateSession_FullMethodName, req)
	if err != nil {
		return nil, err
	}
	return s.inner.UpdateSession(ctx, adapted)
}

func (s *publicAdaptedAgentServer) CreateTurn(ctx context.Context, req *proto.CreateAgentProviderTurnRequest) (*proto.AgentTurn, error) {
	adapted, err := preparePublicRequest(s.transport, ctx, proto.Agent_CreateTurn_FullMethodName, req)
	if err != nil {
		return nil, err
	}
	return s.inner.CreateTurn(ctx, adapted)
}

func (s *publicAdaptedAgentServer) GetTurn(ctx context.Context, req *proto.GetAgentProviderTurnRequest) (*proto.AgentTurn, error) {
	adapted, err := preparePublicRequest(s.transport, ctx, proto.Agent_GetTurn_FullMethodName, req)
	if err != nil {
		return nil, err
	}
	return s.inner.GetTurn(ctx, adapted)
}

func (s *publicAdaptedAgentServer) ListTurns(ctx context.Context, req *proto.ListAgentProviderTurnsRequest) (*proto.ListAgentProviderTurnsResponse, error) {
	adapted, err := preparePublicRequest(s.transport, ctx, proto.Agent_ListTurns_FullMethodName, req)
	if err != nil {
		return nil, err
	}
	return s.inner.ListTurns(ctx, adapted)
}

func (s *publicAdaptedAgentServer) CancelTurn(ctx context.Context, req *proto.CancelAgentProviderTurnRequest) (*proto.AgentTurn, error) {
	adapted, err := preparePublicRequest(s.transport, ctx, proto.Agent_CancelTurn_FullMethodName, req)
	if err != nil {
		return nil, err
	}
	return s.inner.CancelTurn(ctx, adapted)
}

func (s *publicAdaptedAgentServer) ListTurnEvents(ctx context.Context, req *proto.ListAgentProviderTurnEventsRequest) (*proto.ListAgentProviderTurnEventsResponse, error) {
	adapted, err := preparePublicRequest(s.transport, ctx, proto.Agent_ListTurnEvents_FullMethodName, req)
	if err != nil {
		return nil, err
	}
	return s.inner.ListTurnEvents(ctx, adapted)
}

type publicAdaptedWorkflowServer struct {
	proto.UnimplementedWorkflowServer
	inner     *workflowservice.ProviderServer
	transport *providergateway.ProviderGatewayTransport
}

func newPublicAdaptedWorkflowServer(transport *providergateway.ProviderGatewayTransport, inner *workflowservice.ProviderServer) proto.WorkflowServer {
	return &publicAdaptedWorkflowServer{inner: inner, transport: transport}
}

func (s *publicAdaptedWorkflowServer) DeliverEvent(ctx context.Context, req *proto.DeliverWorkflowProviderEventRequest) (*proto.WorkflowEvent, error) {
	adapted, err := preparePublicRequest(s.transport, ctx, proto.Workflow_DeliverEvent_FullMethodName, req)
	if err != nil {
		return nil, err
	}
	return s.inner.DeliverEvent(ctx, adapted)
}
