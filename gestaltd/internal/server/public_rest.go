package server

import (
	"context"
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

func buildPublicGateway(cfg publicGRPCConfig) (*publicrpc.InProcessConn, http.Handler, error) {
	if cfg.Transport == nil || cfg.Invoker == nil {
		return nil, nil, nil
	}
	servers := buildPublicServers(cfg)
	srv := grpc.NewServer(grpc.UnaryInterceptor(publicPrepareUnaryInterceptor(cfg.Transport)))
	publicrpc.RegisterPublicServers(srv, servers)
	conn, err := publicrpc.NewInProcessConn(srv)
	if err != nil {
		return nil, nil, err
	}
	mux := runtime.NewServeMux(runtime.WithErrorHandler(publicRESTErrorHandler))
	if err := publicrpc.RegisterRESTGateway(context.Background(), mux, conn.ClientConn(), servers); err != nil {
		conn.Close()
		return nil, nil, err
	}
	return conn, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, err := requestBearerTokenPreferringHeader(r)
		if err == nil && token != "" {
			headerToken, _ := requestBearerToken(r)
			// Preserve an explicit bearer; otherwise lift session_token when the header is absent or malformed.
			if headerToken == "" {
				r.Header.Set("Authorization", "Bearer "+token)
			}
		}
		mux.ServeHTTP(w, r)
	}), nil
}

func buildPublicServers(cfg publicGRPCConfig) publicrpc.Servers {
	servers := publicrpc.Servers{
		App: appAccessServer(cfg),
	}
	if cfg.AgentManager != nil {
		servers.Agent = agentProviderServer(cfg)
	}
	if cfg.WorkflowManager != nil {
		servers.Workflow = workflowProviderServer(cfg)
	}
	if cfg.IndexedDB != nil {
		servers.IndexedDB = indexedDBServer(cfg)
	}
	if cfg.Authentication != nil {
		servers.Identity = identityProviderServer(cfg)
	}
	if cfg.Authorization != nil {
		servers.Authorization = authorizationProviderServer(cfg)
	}
	if cfg.ExternalCredentials != nil {
		servers.ExternalCredentials = externalCredentialsProviderServer(cfg)
	}
	return servers
}

func publicRESTErrorHandler(_ context.Context, _ *runtime.ServeMux, _ runtime.Marshaler, w http.ResponseWriter, _ *http.Request, err error) {
	st := status.Convert(err)
	httpStatus := runtime.HTTPStatusFromCode(st.Code())
	if httpStatus == 0 {
		httpStatus = http.StatusInternalServerError
	}
	writeJSON(w, httpStatus, apiErrorResponse{
		Error: st.Message(),
		Code:  st.Code().String(),
	})
}
