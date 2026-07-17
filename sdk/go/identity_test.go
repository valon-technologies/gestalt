package gestalt

import (
	"context"
	"testing"

	"google.golang.org/grpc/metadata"
)

func TestAppendIdentityCallMetadataForwardsTrustedCallerSubject(t *testing.T) {
	t.Parallel()

	ctx := WithTrustedCallerSubject(context.Background(), "user:caller-123")
	ctx = AppendIdentityCallMetadata(ctx)

	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		t.Fatal("outgoing metadata missing")
	}
	if got := md.Get(TrustedCallerSubjectMetadataKey); len(got) != 1 || got[0] != "user:caller-123" {
		t.Fatalf("caller subject metadata = %#v, want user:caller-123", got)
	}
}

func TestAppendIdentityCallMetadataForwardsBearerAndSubject(t *testing.T) {
	t.Parallel()

	ctx := WithIdentityCallContext(context.Background(), IdentityCallContext{
		CallerBearerToken: "bearer-token",
		CallerSubjectID:   "user:caller-123",
	})
	ctx = AppendIdentityCallMetadata(ctx)

	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		t.Fatal("outgoing metadata missing")
	}
	if got := md.Get(CallerBearerTokenMetadataKey); len(got) != 1 || got[0] != "bearer-token" {
		t.Fatalf("bearer metadata = %#v, want bearer-token", got)
	}
	if got := md.Get(TrustedCallerSubjectMetadataKey); len(got) != 1 || got[0] != "user:caller-123" {
		t.Fatalf("caller subject metadata = %#v, want user:caller-123", got)
	}
}

func TestAuthCallContextFromIncomingReadsTrustedSubjectMetadata(t *testing.T) {
	t.Parallel()

	incoming := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		TrustedCallerSubjectMetadataKey, "user:caller-123",
	))
	call := IdentityCallContextFromContext(AuthCallContextFromIncoming(incoming))
	if call.CallerSubjectID != "user:caller-123" {
		t.Fatalf("CallerSubjectID = %q, want user:caller-123", call.CallerSubjectID)
	}
}
