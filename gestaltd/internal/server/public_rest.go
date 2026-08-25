package server

import (
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/protoutil"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/appaccess"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

func buildPublicGateway(cfg publicGRPCConfig) (*publicrpc.InProcessConn, http.Handler, error) {
	if cfg.Transport == nil || cfg.Invoker == nil {
		return nil, nil, nil
	}
	servers := buildPublicServers(cfg)
	srv := grpc.NewServer(grpc.UnaryInterceptor(publicPrepareUnaryInterceptor(cfg.Transport)), grpc.StreamInterceptor(publicPrepareStreamInterceptor(cfg.Transport)))
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
	appServer := servers.App
	if err := mux.HandlePath(http.MethodPost, "/api/v2/app/{app}/operations/{operation}", func(w http.ResponseWriter, r *http.Request, pathParams map[string]string) {
		handleRESTInvoke(w, r, cfg.Transport, appServer, pathParams)
	}); err != nil {
		conn.Close()
		return nil, nil, err
	}
	return conn, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, err := requestBearerTokenPreferringHeader(r)
		if err == nil && token != "" {
			headerToken, _ := requestBearerToken(r)
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
	if cfg.RemoteManagement != nil {
		servers.RemoteManagement = cfg.RemoteManagement
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

const invokeFullMethod = "/gestalt.provider.v1.App/Invoke"

func handleRESTInvoke(
	w http.ResponseWriter,
	r *http.Request,
	transport *providergateway.ProviderGatewayTransport,
	appServer proto.AppServer,
	pathParams map[string]string,
) {
	var protoReq proto.AppInvokeRequest
	body, err := io.ReadAll(r.Body)
	if err != nil {
		publicRESTErrorHandler(r.Context(), nil, nil, w, r, status.Errorf(codes.InvalidArgument, "%v", err))
		return
	}
	if len(body) > 0 {
		if err := protojson.Unmarshal(body, &protoReq); err != nil {
			publicRESTErrorHandler(r.Context(), nil, nil, w, r, status.Errorf(codes.InvalidArgument, "%v", err))
			return
		}
	}
	if v, ok := pathParams["app"]; ok {
		protoReq.App = v
	}
	if v, ok := pathParams["operation"]; ok {
		protoReq.Operation = v
	}

	ctx := publicrpc.WithPublicOrigin(r.Context(), invokeFullMethod)
	existingMD, _ := metadata.FromIncomingContext(r.Context())
	ctx = metadata.NewIncomingContext(ctx, metadata.Join(
		existingMD,
		httpHeadersToGRPCMetadata(r.Header),
	))
	ctx = stripInternalIdentityMetadata(ctx)

	p, adapted, err := transport.PreparePublicRequest(ctx, invokeFullMethod, &protoReq)
	if err != nil {
		publicRESTErrorHandler(ctx, nil, nil, w, r, err)
		return
	}
	req, ok := adapted.(*proto.AppInvokeRequest)
	if !ok || req == nil {
		req = &protoReq
	}

	if p != nil {
		canonical := principal.Canonicalized(p)
		ctx = principal.WithPrincipal(ctx, canonical)
		if subjectID := strings.TrimSpace(canonical.SubjectID); subjectID != "" {
			ctx = gestalt.WithTrustedCallerSubject(ctx, subjectID)
		}
	}

	maybeServer, ok := appServer.(*appaccess.AppServer)
	if !ok {
		publicRESTErrorHandler(ctx, nil, nil, w, r, status.Error(codes.Internal, "streaming invocation is not available"))
		return
	}
	outcome, err := maybeServer.InvokeMaybeStream(ctx, req)
	if err != nil {
		st := status.Convert(err)
		publicRESTErrorHandler(ctx, nil, nil, w, r, st.Err())
		return
	}
	if outcome == nil {
		publicRESTErrorHandler(ctx, nil, nil, w, r, status.Error(codes.Internal, "internal error"))
		return
	}
	if outcome.IsStream() {
		writeStreamingOperationResult(w, r, outcome.Stream)
		return
	}
	writeUnaryOperationResultJSON(w, outcome.Unary)
}

func writeUnaryOperationResultJSON(w http.ResponseWriter, result *core.OperationResult) {
	if result == nil {
		writeJSON(w, http.StatusInternalServerError, apiErrorResponse{Error: "internal error"})
		return
	}
	pb := &proto.OperationResult{
		Status:  int32(result.Status),
		Headers: protoutil.StringSlicesToProto(result.Headers),
		Body:    result.Body,
	}
	data, err := protojson.Marshal(pb)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, apiErrorResponse{Error: "internal error"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func httpHeadersToGRPCMetadata(h http.Header) metadata.MD {
	md := metadata.MD{}
	for key, values := range h {
		lk := strings.ToLower(key)
		for _, v := range values {
			md.Append(lk, v)
		}
	}
	return md
}
