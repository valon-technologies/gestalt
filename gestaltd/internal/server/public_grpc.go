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
	authorizationservice "github.com/valon-technologies/gestalt/server/services/authorization"
	externalcredentialsservice "github.com/valon-technologies/gestalt/server/services/externalcredentials"
	identityservice "github.com/valon-technologies/gestalt/server/services/identity"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	indexeddbservice "github.com/valon-technologies/gestalt/server/services/indexeddb"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
	workflowservice "github.com/valon-technologies/gestalt/server/services/workflows"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

type publicGRPCConfig struct {
	Transport           *providergateway.ProviderGatewayTransport
	Invoker             invocation.Invoker
	AgentManager        agentmanager.Service
	WorkflowManager     workflowmanager.Service
	Authentication      core.IdentityProvider
	Authorization       core.AuthorizationProvider
	IndexedDB           indexeddb.IndexedDB
	ExternalCredentials core.ExternalCredentialProvider
}

func appAccessServer(cfg publicGRPCConfig) proto.AppServer {
	return appaccessservice.NewServer(
		cfg.Invoker,
		appaccessservice.WithAgentAppInvocationAuthorizer(cfg.AgentManager),
	)
}

func agentProviderServer(cfg publicGRPCConfig) proto.AgentServer {
	return agentservice.NewProviderServer(
		"gestaltd",
		cfg.AgentManager,
	)
}

func workflowProviderServer(cfg publicGRPCConfig) proto.WorkflowServer {
	return workflowservice.NewProviderServer(
		"gestaltd",
		cfg.WorkflowManager,
		cfg.Authorization,
		workflowservice.WithAgentWorkflowInvocationAuthorizer(cfg.AgentManager),
	)
}

func indexedDBServer(cfg publicGRPCConfig) proto.IndexedDBServer {
	return indexeddbservice.NewServer(
		cfg.IndexedDB,
		"gestaltd",
		indexeddbservice.ServerOptions{},
	)
}

func identityProviderServer(cfg publicGRPCConfig) proto.IdentityServer {
	return identityservice.NewProviderServer(cfg.Authentication)
}

func authorizationProviderServer(cfg publicGRPCConfig) proto.AuthorizationServer {
	return authorizationservice.NewProviderServer(cfg.Authorization)
}

func externalCredentialsProviderServer(cfg publicGRPCConfig) proto.ExternalCredentialsServer {
	return externalcredentialsservice.NewProviderServer(cfg.ExternalCredentials)
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
		p, adapted, err := transport.PreparePublicRequest(ctx, origin.FullMethod, msg)
		if err != nil {
			return nil, err
		}
		if p != nil {
			ctx = principal.WithPrincipal(ctx, principal.Canonicalized(p))
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
