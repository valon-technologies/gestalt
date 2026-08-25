package server

import (
	"context"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"google.golang.org/grpc/metadata"
)

func TestStripInternalIdentityMetadataRemovesForgedCallerIdentity(t *testing.T) {
	t.Parallel()

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		gestalt.TrustedCallerSubjectMetadataKey, "user:bob",
		gestalt.CallerBearerTokenMetadataKey, "token-for-bob",
	))
	ctx = principal.WithPrincipal(ctx, &principal.Principal{SubjectID: "user:alice"})

	stripped := stripInternalIdentityMetadata(ctx)
	md, ok := metadata.FromIncomingContext(stripped)
	if !ok {
		t.Fatal("expected incoming metadata")
	}
	if len(md.Get(gestalt.TrustedCallerSubjectMetadataKey)) != 0 {
		t.Fatalf("forged caller metadata = %v, want stripped", md.Get(gestalt.TrustedCallerSubjectMetadataKey))
	}
	if len(md.Get(gestalt.CallerBearerTokenMetadataKey)) != 0 {
		t.Fatalf("forged caller bearer metadata = %v, want stripped", md.Get(gestalt.CallerBearerTokenMetadataKey))
	}
}
