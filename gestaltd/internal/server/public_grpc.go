package server

import (
	"context"
	"net/http"

	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	agentservice "github.com/valon-technologies/gestalt/server/services/agents"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
	workflowservice "github.com/valon-technologies/gestalt/server/services/workflows"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	gproto "google.golang.org/protobuf/proto"
)

func (s *Server) publicGRPCMiddleware(next http.Handler) http.Handler {
	if s == nil || s.publicGateway == nil || s.routeProfile == RouteProfileManagement {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.hostServiceRelayToken(r) != "" || !isGRPCRequest(r) {
			next.ServeHTTP(w, r)
			return
		}
		s.cachedPublicGRPCHandler().ServeHTTP(w, r)
	})
}

func (s *Server) cachedPublicGRPCHandler() http.Handler {
	s.publicGRPCMu.Lock()
	defer s.publicGRPCMu.Unlock()
	if s.publicGRPCHandler != nil {
		return s.publicGRPCHandler
	}
	srv := s.buildPublicGRPCServer()
	reflection.Register(srv)
	s.publicGRPCHandler = http.HandlerFunc(srv.ServeHTTP)
	return s.publicGRPCHandler
}

func (s *Server) buildPublicGRPCServer() *grpc.Server {
	srv := grpc.NewServer(grpc.UnaryInterceptor(publicUnaryInterceptor(s.publicGateway)))
	publicrpc.RegisterPublicAppServer(srv, appaccessservice.NewServer(
		s.pluginInvoker,
		appaccessservice.WithAgentAppInvocationAuthorizer(s.agentRuns),
	))
	publicrpc.RegisterPublicAgentServer(srv, agentservice.NewProviderServer("", s.agentRuns))
	publicrpc.RegisterPublicWorkflowServer(srv, workflowservice.NewProviderServer(
		"",
		s.workflowSchedules,
		s.rawAuthorization,
		workflowservice.WithAgentWorkflowInvocationAuthorizer(s.agentRuns),
	))
	return srv
}

func publicUnaryInterceptor(transport *providergateway.ProviderGatewayTransport) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		msg, ok := req.(gproto.Message)
		if !ok {
			return handler(ctx, req)
		}
		p, adapted, err := transport.PreparePublicRequest(ctx, info.FullMethod, msg)
		if err != nil {
			return nil, err
		}
		ctx = principal.WithPrincipal(ctx, p)
		return handler(ctx, adapted)
	}
}
