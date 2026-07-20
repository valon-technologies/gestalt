package identity

import (
	"context"
	"strings"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"google.golang.org/grpc/metadata"
)

// providerHandlerContext derives identity call context from verified ingress
// state on context. Request metadata is only consulted for the private
// gestaltd-to-provider transport where no verified caller subject exists,
// except that the caller's bearer token is forwarded from the public gRPC
// path so grant-management RPCs can identify the caller's credentials.
func providerHandlerContext(ctx context.Context) context.Context {
	if subjectID := gestalt.TrustedCallerSubjectFromContext(ctx); subjectID != "" {
		call := gestalt.IdentityCallContext{
			CallerSubjectID: subjectID,
		}
		if token := bearerTokenFromIncomingMetadata(ctx); token != "" {
			call.CallerBearerToken = token
		}
		return gestalt.WithIdentityCallContext(ctx, call)
	}
	return gestalt.AuthCallContextFromIncoming(ctx)
}

func bearerTokenFromIncomingMetadata(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	for _, value := range md.Get("authorization") {
		value = strings.TrimSpace(value)
		if len(value) > 7 && strings.EqualFold(value[:7], "Bearer ") {
			if token := strings.TrimSpace(value[7:]); token != "" {
				return token
			}
		}
	}
	return ""
}
