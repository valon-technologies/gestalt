package identity

import (
	"context"
	"strings"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"google.golang.org/grpc/metadata"
)

// providerHandlerContext composes caller identity already verified by
// gestaltd with the provider-native bearer token forwarded by the public
// gateway. The existing call context matters because it can carry a verified
// provider alias; replacing it with AuthCallContextFromIncoming would silently
// discard that compatibility data.
func providerHandlerContext(ctx context.Context) context.Context {
	call := gestalt.IdentityCallContextFromContext(ctx)

	if subjectID := gestalt.TrustedCallerSubjectFromContext(ctx); subjectID != "" {
		call.CallerSubjectID = subjectID
	} else if subjectID := trustedCallerSubjectFromIncomingMetadata(ctx); subjectID != "" {
		call.CallerSubjectID = subjectID
	}

	if strings.TrimSpace(call.CallerBearerToken) == "" {
		if token := gestalt.CallerBearerTokenFromIncomingContext(ctx); token != "" {
			call.CallerBearerToken = token
		}
	}
	if strings.TrimSpace(call.CallerBearerToken) == "" {
		if token := bearerTokenFromIncomingMetadata(ctx); token != "" {
			call.CallerBearerToken = token
		}
	}

	if strings.TrimSpace(call.CallerBearerToken) == "" &&
		strings.TrimSpace(call.CallerSubjectID) == "" &&
		call.Introspection == nil {
		return gestalt.AuthCallContextFromIncoming(ctx)
	}
	return gestalt.WithIdentityCallContext(ctx, call)
}

func trustedCallerSubjectFromIncomingMetadata(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	for _, value := range md.Get(gestalt.TrustedCallerSubjectMetadataKey) {
		if subjectID := strings.TrimSpace(value); subjectID != "" {
			return subjectID
		}
	}
	return ""
}

func bearerTokenFromIncomingMetadata(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	for _, value := range md.Get("authorization") {
		value = strings.TrimSpace(value)
		if len(value) <= len("Bearer ") || !strings.EqualFold(value[:len("Bearer ")], "Bearer ") {
			continue
		}
		if token := strings.TrimSpace(value[len("Bearer "):]); token != "" {
			return token
		}
	}
	return ""
}
