package server

import (
	"io"
	"net/http"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/authorizationstate"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

// applyAuthorizationStateFullMethod reuses SetAuthorizationState policy until generated bindings include ApplyAuthorizationState.
const applyAuthorizationStateFullMethod = proto.Authorization_SetAuthorizationState_FullMethodName

func handleRESTApplyAuthorizationState(
	w http.ResponseWriter,
	r *http.Request,
	transport *providergateway.ProviderGatewayTransport,
	provider core.AuthorizationProvider,
) {
	var protoReq proto.SetAuthorizationStateRequest
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

	ctx := publicrpc.WithPublicOrigin(r.Context(), applyAuthorizationStateFullMethod)
	existingMD, _ := metadata.FromIncomingContext(r.Context())
	ctx = metadata.NewIncomingContext(ctx, metadata.Join(
		existingMD,
		httpHeadersToGRPCMetadata(r.Header),
	))
	ctx = stripInternalIdentityMetadata(ctx)
	if _, _, _, err := transport.PreparePublicRequest(ctx, applyAuthorizationStateFullMethod, &protoReq); err != nil {
		publicRESTErrorHandler(ctx, nil, nil, w, r, err)
		return
	}
	if provider == nil {
		publicRESTErrorHandler(ctx, nil, nil, w, r, status.Error(codes.FailedPrecondition, "authorization provider is not configured"))
		return
	}
	resp, err := authorizationstate.Apply(ctx, provider, &protoReq)
	if err != nil {
		publicRESTErrorHandler(ctx, nil, nil, w, r, status.Errorf(codes.Internal, "%v", err))
		return
	}
	data, err := protojson.Marshal(resp)
	if err != nil {
		publicRESTErrorHandler(ctx, nil, nil, w, r, status.Error(codes.Internal, "internal error"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
