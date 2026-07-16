package providergateway

import (
	"context"
	"strings"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"google.golang.org/grpc/metadata"
)

func StripClientCallerBearerMetadata(ctx context.Context) context.Context {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx
	}
	if len(md.Get(gestalt.CallerBearerTokenMetadataKey)) == 0 {
		return ctx
	}
	md = md.Copy()
	delete(md, gestalt.CallerBearerTokenMetadataKey)
	return metadata.NewIncomingContext(ctx, md)
}

func publicIdentityCallerBearerMethod(fullMethod string) bool {
	if !publicIdentityServiceMethod(fullMethod) {
		return false
	}
	_, method := splitFullMethod(fullMethod)
	switch method {
	case "UserInfo", "ListGrants", "GetGrant", "RevokeGrant":
		return true
	default:
		return false
	}
}

func WithValidatedPublicCallerBearer(ctx context.Context, fullMethod, token string) context.Context {
	token = strings.TrimSpace(token)
	if token == "" || !publicIdentityCallerBearerMethod(fullMethod) {
		return ctx
	}
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		md = metadata.MD{}
	} else {
		md = md.Copy()
	}
	delete(md, gestalt.CallerBearerTokenMetadataKey)
	md.Set(gestalt.CallerBearerTokenMetadataKey, token)
	ctx = metadata.NewIncomingContext(ctx, md)
	return gestalt.WithIdentityCallContext(ctx, gestalt.IdentityCallContext{CallerBearerToken: token})
}
