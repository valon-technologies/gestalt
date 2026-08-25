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

func TestAuthCallContextFromIncomingPreservesExistingIdentityContext(t *testing.T) {
	t.Parallel()

	ctx := WithTrustedCallerSubject(context.Background(), "user:verified")
	ctx = WithIdentityCallContext(ctx, IdentityCallContext{
		CallerSubjectID:   "user:stale",
		CallerBearerToken: "session-token",
		Introspection: &IntrospectResponse{
			Active:  true,
			Subject: "user:alias@example.com",
		},
	})
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs(
		TrustedCallerSubjectMetadataKey, "user:metadata",
		CallerBearerTokenMetadataKey, "metadata-token",
	))

	call := IdentityCallContextFromContext(AuthCallContextFromIncoming(ctx))
	if call.CallerSubjectID != "user:verified" {
		t.Fatalf("CallerSubjectID = %q, want user:verified", call.CallerSubjectID)
	}
	if call.CallerBearerToken != "session-token" {
		t.Fatalf("CallerBearerToken = %q, want session-token", call.CallerBearerToken)
	}
	if call.Introspection == nil || call.Introspection.Subject != "user:alias@example.com" {
		t.Fatalf("Introspection = %#v, want preserved provider alias", call.Introspection)
	}
}

func TestAuthCallContextFromIncomingPreservesIncomingIdentityMetadataTogether(t *testing.T) {
	t.Parallel()

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		TrustedCallerSubjectMetadataKey, "user:caller-123",
		CallerBearerTokenMetadataKey, "session-token",
	))

	call := IdentityCallContextFromContext(AuthCallContextFromIncoming(ctx))
	if call.CallerSubjectID != "user:caller-123" {
		t.Fatalf("CallerSubjectID = %q, want user:caller-123", call.CallerSubjectID)
	}
	if call.CallerBearerToken != "session-token" {
		t.Fatalf("CallerBearerToken = %q, want session-token", call.CallerBearerToken)
	}
}

func TestAuthCallContextFromIncomingReadsAuthorizationBearer(t *testing.T) {
	t.Parallel()

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		"authorization", "Bearer session-token",
	))

	call := IdentityCallContextFromContext(AuthCallContextFromIncoming(ctx))
	if call.CallerBearerToken != "session-token" {
		t.Fatalf("CallerBearerToken = %q, want session-token", call.CallerBearerToken)
	}
}
