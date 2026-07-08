package server

import (
	"context"
	"net/http"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	agentservice "github.com/valon-technologies/gestalt/server/services/agents"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	indexeddbservice "github.com/valon-technologies/gestalt/server/services/indexeddb"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
	workflowservice "github.com/valon-technologies/gestalt/server/services/workflows"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
)

type publicGRPCConfig struct {
	Transport             *providergateway.ProviderGatewayTransport
	Invoker               invocation.Invoker
	AgentManager          agentmanager.Service
	WorkflowManager       workflowmanager.Service
	Authorization         core.AuthorizationProvider
	IndexedDBs            map[string]indexeddb.IndexedDB
	SelectedIndexedDBName string
}

func buildPublicGRPCHandler(cfg publicGRPCConfig) http.Handler {
	if cfg.Transport == nil || cfg.Invoker == nil {
		return nil
	}
	srv := grpc.NewServer(grpc.UnaryInterceptor(publicPrepareUnaryInterceptor(cfg.Transport)))
	publicrpc.RegisterPublicAppServer(srv, appaccessservice.NewServer(
		cfg.Invoker,
		appaccessservice.WithAgentAppInvocationAuthorizer(cfg.AgentManager),
	))
	if cfg.AgentManager != nil {
		publicrpc.RegisterPublicAgentServer(srv, agentservice.NewProviderServer(
			"gestaltd",
			cfg.AgentManager,
		))
	}
	if cfg.WorkflowManager != nil {
		publicrpc.RegisterPublicWorkflowServer(srv, workflowservice.NewProviderServer(
			"gestaltd",
			cfg.WorkflowManager,
			cfg.Authorization,
			workflowservice.WithAgentWorkflowInvocationAuthorizer(cfg.AgentManager),
		))
	}
	if len(cfg.IndexedDBs) > 0 {
		publicrpc.RegisterPublicIndexedDBServer(srv, indexeddbservice.NewRoutingServer(
			cfg.IndexedDBs,
			cfg.SelectedIndexedDBName,
			"gestaltd",
			indexeddbservice.ServerOptions{},
		))
	}
	return http.HandlerFunc(srv.ServeHTTP)
}

func publicPrepareUnaryInterceptor(transport *providergateway.ProviderGatewayTransport) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		origin, ok := publicrpc.PublicOriginFromContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "bearer token is required")
		}
		msg, ok := req.(gproto.Message)
		if !ok {
			return nil, status.Error(codes.Internal, "request type mismatch")
		}
		_, adapted, err := transport.PreparePublicRequest(ctx, origin.FullMethod, msg)
		if err != nil {
			return nil, err
		}
		return handler(ctx, adapted)
	}
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
		if s.publicGRPCHandler == nil {
			writeGRPCTrailersOnly(w, codes.Unauthenticated, "public-grpc-unavailable")
			return
		}
		s.publicGRPCHandler.ServeHTTP(w, r)
	})
}
