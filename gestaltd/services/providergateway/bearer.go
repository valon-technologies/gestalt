package providergateway

import (
	"context"
	"strings"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/server/services/identity"
	"google.golang.org/grpc/metadata"
)

func bearerTokenFromContext(ctx context.Context) string {
	if token := strings.TrimSpace(gestalt.CallerBearerTokenFromIncomingContext(ctx)); token != "" {
		return token
	}
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

// WithPublicBearerToken attaches the caller bearer token for identity UserInfo.
func WithPublicBearerToken(ctx context.Context, token string) context.Context {
	return identity.WithCallerBearerToken(ctx, token)
}
