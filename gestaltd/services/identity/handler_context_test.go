package identity

import (
	"context"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"google.golang.org/grpc/metadata"
)

func TestProviderHandlerContextPrefersVerifiedCallerSubject(t *testing.T) {
	t.Parallel()

	ctx := gestalt.WithTrustedCallerSubject(context.Background(), "user:alice")
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs(
		gestalt.TrustedCallerSubjectMetadataKey, "user:bob",
	))

	enriched := providerHandlerContext(ctx)
	call := gestalt.IdentityCallContextFromContext(enriched)
	if call.CallerSubjectID != "user:alice" {
		t.Fatalf("CallerSubjectID = %q, want user:alice", call.CallerSubjectID)
	}
}

func TestProviderHandlerContextFallsBackToPrivateMetadata(t *testing.T) {
	t.Parallel()

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		gestalt.TrustedCallerSubjectMetadataKey, "user:bob",
	))

	enriched := providerHandlerContext(ctx)
	call := gestalt.IdentityCallContextFromContext(enriched)
	if call.CallerSubjectID != "user:bob" {
		t.Fatalf("CallerSubjectID = %q, want user:bob", call.CallerSubjectID)
	}
}
