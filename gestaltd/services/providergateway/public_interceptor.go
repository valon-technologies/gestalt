package providergateway

import (
	"context"

	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
)

// PublicUnaryServerInterceptor authenticates, authorizes, and adapts public
// requests through ProviderGateway before dispatching to shared services.
func PublicUnaryServerInterceptor(gateway *ProviderGatewayTransport) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if gateway == nil {
			return nil, status.Error(codes.Internal, "provider gateway: transport is nil")
		}
		msg, ok := req.(gproto.Message)
		if !ok {
			return nil, status.Error(codes.Internal, "provider gateway: request must be a proto message")
		}
		ctx = publicrpc.WithPublicOrigin(ctx, info.FullMethod)
		if token := bearerTokenFromContext(ctx); token != "" {
			ctx = WithPublicBearerToken(ctx, token)
		}
		p, adapted, err := gateway.PreparePublicRequest(ctx, info.FullMethod, msg)
		if err != nil {
			return nil, err
		}
		ctx = principal.WithPrincipal(ctx, p)
		return handler(ctx, adapted)
	}
}
