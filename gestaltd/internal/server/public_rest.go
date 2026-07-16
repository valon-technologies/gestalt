package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	gestaltproto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	gproto "google.golang.org/protobuf/proto"
)

var errRESTResponseWritten = errors.New("rest response written")

const (
	gestaltResponseKindHeader          = "X-Gestalt-Response-Kind"
	gestaltResponseKindOperationResult = "operation-result"
)

// preservedSecurityResponseHeaders are installed by securityHeadersMiddleware and must
// survive App OperationResult passthrough.
var preservedSecurityResponseHeaders = []string{
	"Content-Security-Policy",
	"X-Content-Type-Options",
	"X-Frame-Options",
	"Strict-Transport-Security",
}

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
	mux := runtime.NewServeMux(
		runtime.WithForwardResponseOption(forwardOperationResult),
		runtime.WithErrorHandler(publicRESTErrorHandler),
	)
	if err := publicrpc.RegisterRESTGateway(context.Background(), mux, conn.ClientConn(), servers); err != nil {
		conn.Close()
		return nil, nil, err
	}
	return conn, publicRESTSessionBridge(mux), nil
}

func publicRESTSessionBridge(next http.Handler) http.Handler {
	if next == nil {
		return nil
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r != nil && strings.TrimSpace(r.Header.Get("Authorization")) == "" {
			if c, err := r.Cookie(sessionCookieName); err == nil {
				if token := strings.TrimSpace(c.Value); token != "" {
					r.Header.Set("Authorization", "Bearer "+token)
				}
			}
		}
		next.ServeHTTP(w, r)
	})
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

func forwardOperationResult(_ context.Context, w http.ResponseWriter, resp gproto.Message) error {
	result, ok := resp.(*gestaltproto.OperationResult)
	if !ok {
		return nil
	}
	writeProtoOperationResult(w, result)
	return errRESTResponseWritten
}

func writeProtoOperationResult(w http.ResponseWriter, result *gestaltproto.OperationResult) {
	if w == nil || result == nil {
		return
	}
	headers := w.Header()
	preserved := make(map[string][]string, len(preservedSecurityResponseHeaders))
	for _, name := range preservedSecurityResponseHeaders {
		if values := headers.Values(name); len(values) > 0 {
			preserved[name] = append([]string(nil), values...)
		}
	}
	headers.Del("Content-Type") // grpc-gateway's default response type.
	for name, values := range result.GetHeaders() {
		if values == nil || strings.EqualFold(name, gestaltResponseKindHeader) {
			continue
		}
		for i, value := range values.GetValues() {
			if i == 0 {
				headers.Set(name, value)
			} else {
				headers.Add(name, value)
			}
		}
	}
	for name, values := range preserved {
		headers.Del(name)
		for _, value := range values {
			headers.Add(name, value)
		}
	}
	statusCode := int(result.GetStatus())
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	headers.Set(gestaltResponseKindHeader, gestaltResponseKindOperationResult)
	w.WriteHeader(statusCode)
	if body := result.GetBody(); len(body) > 0 {
		_, _ = w.Write(body)
	}
}

func publicRESTErrorHandler(ctx context.Context, mux *runtime.ServeMux, marshaler runtime.Marshaler, w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, errRESTResponseWritten) {
		return
	}
	runtime.DefaultHTTPErrorHandler(ctx, mux, marshaler, w, r, err)
}
