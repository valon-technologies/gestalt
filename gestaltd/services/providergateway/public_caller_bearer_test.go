package providergateway

import (
	"context"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc/metadata"
)

func TestWithValidatedPublicCallerBearer_UserInfo(t *testing.T) {
	t.Parallel()

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs())
	ctx = WithValidatedPublicCallerBearer(ctx, proto.Identity_UserInfo_FullMethodName, "caller-token")

	call := gestalt.IdentityCallContextFromContext(ctx)
	if call.CallerBearerToken != "caller-token" {
		t.Fatalf("IdentityCallContext token = %q, want %q", call.CallerBearerToken, "caller-token")
	}
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		t.Fatal("incoming metadata missing")
	}
	values := md.Get(gestalt.CallerBearerTokenMetadataKey)
	if len(values) != 1 || values[0] != "caller-token" {
		t.Fatalf("metadata caller bearer = %v, want [caller-token]", values)
	}
}

func TestWithValidatedPublicCallerBearer_NonGrantIdentityMethod(t *testing.T) {
	t.Parallel()

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs())
	ctx = WithValidatedPublicCallerBearer(ctx, proto.Identity_Introspect_FullMethodName, "caller-token")

	call := gestalt.IdentityCallContextFromContext(ctx)
	if call.CallerBearerToken != "" {
		t.Fatalf("IdentityCallContext token = %q, want empty for Introspect", call.CallerBearerToken)
	}
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		t.Fatal("incoming metadata missing")
	}
	if len(md.Get(gestalt.CallerBearerTokenMetadataKey)) != 0 {
		t.Fatalf("metadata caller bearer = %v, want none", md.Get(gestalt.CallerBearerTokenMetadataKey))
	}
}

func TestWithValidatedPublicCallerBearer_DoesNotStoreInvocationBearer(t *testing.T) {
	t.Parallel()

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs())
	ctx = WithValidatedPublicCallerBearer(ctx, proto.App_Invoke_FullMethodName, "session-token")

	if call := gestalt.IdentityCallContextFromContext(ctx); call.CallerBearerToken != "" {
		t.Fatalf("IdentityCallContext token = %q, want empty for app invoke", call.CallerBearerToken)
	}
}

func TestStripClientCallerBearerMetadata(t *testing.T) {
	t.Parallel()

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		gestalt.CallerBearerTokenMetadataKey, "client-supplied",
		"authorization", "Bearer valid-token",
	))
	ctx = StripClientCallerBearerMetadata(ctx)

	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		t.Fatal("incoming metadata missing")
	}
	if len(md.Get(gestalt.CallerBearerTokenMetadataKey)) != 0 {
		t.Fatalf("caller bearer metadata = %v, want stripped", md.Get(gestalt.CallerBearerTokenMetadataKey))
	}
	if got := md.Get("authorization"); len(got) != 1 || got[0] != "Bearer valid-token" {
		t.Fatalf("authorization metadata = %v, want preserved bearer", got)
	}
}
