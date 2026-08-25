package identity

import (
	"context"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"google.golang.org/grpc/metadata"
)

func TestProviderHandlerContextPreservesAliasesAndPrefersVerifiedSubject(t *testing.T) {
	t.Parallel()

	ctx := gestalt.WithTrustedCallerSubject(context.Background(), "user:alice")
	ctx = gestalt.WithIdentityCallContext(ctx, gestalt.IdentityCallContext{
		CallerSubjectID: "user:stale",
		Introspection: &gestalt.IntrospectResponse{
			Active:  true,
			Subject: "user:alice@example.com",
		},
	})
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs(
		gestalt.TrustedCallerSubjectMetadataKey, "user:bob",
		"authorization", "Bearer session-token",
	))

	call := gestalt.IdentityCallContextFromContext(providerHandlerContext(ctx))
	if call.CallerSubjectID != "user:alice" {
		t.Fatalf("CallerSubjectID = %q, want user:alice", call.CallerSubjectID)
	}
	if call.CallerBearerToken != "session-token" {
		t.Fatalf("CallerBearerToken = %q, want session-token", call.CallerBearerToken)
	}
	if call.Introspection == nil || call.Introspection.Subject != "user:alice@example.com" {
		t.Fatalf("Introspection = %#v, want preserved email alias", call.Introspection)
	}
}

func TestProviderHandlerContextFallsBackToPrivateMetadata(t *testing.T) {
	t.Parallel()

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		gestalt.TrustedCallerSubjectMetadataKey, "user:bob",
		gestalt.CallerBearerTokenMetadataKey, "session-token",
	))
	call := gestalt.IdentityCallContextFromContext(providerHandlerContext(ctx))
	if call.CallerSubjectID != "user:bob" {
		t.Fatalf("CallerSubjectID = %q, want user:bob", call.CallerSubjectID)
	}
	if call.CallerBearerToken != "session-token" {
		t.Fatalf("CallerBearerToken = %q, want session-token", call.CallerBearerToken)
	}
}
