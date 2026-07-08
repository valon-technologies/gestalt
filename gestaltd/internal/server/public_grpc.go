package server

import (
	"fmt"
	"net/http"

	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	agentservice "github.com/valon-technologies/gestalt/server/services/agents"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
	workflowservice "github.com/valon-technologies/gestalt/server/services/workflows"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

const gestaltdPublicCallerApp = "gestaltd"

type publicGRPCSurface struct {
	handler http.Handler
}

func buildPublicGRPCSurface(cfg Config, workflowManager workflowservice.ManagerService) (*publicGRPCSurface, error) {
	if cfg.Auth == nil {
		return nil, nil
	}
	registry, err := publicrpc.NewGeneratedRegistry()
	if err != nil {
		return nil, fmt.Errorf("init public rpc registry: %w", err)
	}
	gateway := providergateway.NewProviderGatewayTransport()
	gateway.SetPublicMethods(registry)
	gateway.SetIdentityProvider(cfg.Auth)
	gateway.SetAuthorizationProvider(cfg.Authorization)
	gateway.SetPublicBaseURL(cfg.PublicBaseURL)
	gateway.SetPublicCallerApp(gestaltdPublicCallerApp)

	invoker := cfg.AppInvocation
	if invoker == nil {
		invoker = cfg.Invoker
	}
	if invoker == nil {
		return nil, fmt.Errorf("public gRPC app invocation is required")
	}

	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(providergateway.PublicUnaryServerInterceptor(gateway)))
	publicrpc.RegisterPublicAppServer(srv, appaccessservice.NewServer(
		invoker,
		appaccessservice.WithAgentAppInvocationAuthorizer(cfg.AgentManager),
		appaccessservice.WithCallerApp(gestaltdPublicCallerApp),
	))
	publicrpc.RegisterPublicAgentServer(srv, agentservice.NewProviderServer(
		gestaltdPublicCallerApp,
		cfg.AgentManager,
	))
	publicrpc.RegisterPublicWorkflowServer(srv, workflowservice.NewProviderServer(
		gestaltdPublicCallerApp,
		workflowManager,
		cfg.Authorization,
		workflowservice.WithAgentWorkflowInvocationAuthorizer(cfg.AgentManager),
	))
	reflection.Register(srv)

	return &publicGRPCSurface{
		handler: http.HandlerFunc(srv.ServeHTTP),
	}, nil
}

func (s *Server) publicGRPCMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s == nil || s.publicGRPCHandler == nil || !isGRPCRequest(r) || s.hostServiceRelayToken(r) != "" {
			next.ServeHTTP(w, r)
			return
		}
		s.publicGRPCHandler.ServeHTTP(w, r)
	})
}
