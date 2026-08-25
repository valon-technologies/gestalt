package identity

import (
	"context"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestProviderServerPreservesCallerContextAcrossGrantHandlers(t *testing.T) {
	t.Parallel()

	type observedCall struct {
		name string
		call gestalt.IdentityCallContext
	}
	var observed []observedCall
	record := func(name string, ctx context.Context) {
		observed = append(observed, observedCall{
			name: name,
			call: gestalt.IdentityCallContextFromContext(ctx),
		})
	}
	provider := &coretesting.StubIdentityProvider{
		TokenFn: func(ctx context.Context, _ *core.TokenRequest) (*core.TokenResponse, error) {
			record("Token", ctx)
			return &core.TokenResponse{}, nil
		},
		UserInfoFn: func(ctx context.Context, _ *core.UserInfoRequest) (*core.UserInfoResponse, error) {
			record("UserInfo", ctx)
			return &core.UserInfoResponse{}, nil
		},
		ListGrantsFn: func(ctx context.Context, _ *core.ListGrantsRequest) (*core.ListGrantsResponse, error) {
			record("ListGrants", ctx)
			return &core.ListGrantsResponse{}, nil
		},
		GetGrantFn: func(ctx context.Context, _ *core.GetGrantRequest) (*core.GetGrantResponse, error) {
			record("GetGrant", ctx)
			return &core.GetGrantResponse{}, nil
		},
		RevokeGrantFn: func(ctx context.Context, _ *core.RevokeGrantRequest) (*core.RevokeGrantResponse, error) {
			record("RevokeGrant", ctx)
			return &core.RevokeGrantResponse{}, nil
		},
	}
	server := NewProviderServer(provider)

	ctx := gestalt.WithTrustedCallerSubject(context.Background(), "user:alice")
	ctx = gestalt.WithIdentityCallContext(ctx, gestalt.IdentityCallContext{
		CallerSubjectID: "user:stale",
		Introspection: &gestalt.IntrospectResponse{
			Active:  true,
			Subject: "user:alice@example.com",
		},
	})
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("authorization", "Bearer session-token"))

	if _, err := server.Token(ctx, &proto.TokenRequest{}); err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	if _, err := server.UserInfo(ctx, &proto.UserInfoRequest{}); err != nil {
		t.Fatalf("UserInfo() error = %v", err)
	}
	if _, err := server.ListGrants(ctx, &proto.ListGrantsRequest{}); err != nil {
		t.Fatalf("ListGrants() error = %v", err)
	}
	if _, err := server.GetGrant(ctx, &proto.GetGrantRequest{GrantId: "grant"}); err != nil {
		t.Fatalf("GetGrant() error = %v", err)
	}
	if _, err := server.RevokeGrant(ctx, &proto.RevokeGrantRequest{GrantId: "grant"}); err != nil {
		t.Fatalf("RevokeGrant() error = %v", err)
	}

	wantNames := []string{"Token", "UserInfo", "ListGrants", "GetGrant", "RevokeGrant"}
	if len(observed) != len(wantNames) {
		t.Fatalf("observed %d provider calls, want %d", len(observed), len(wantNames))
	}
	for i, wantName := range wantNames {
		got := observed[i]
		if got.name != wantName {
			t.Fatalf("provider call %d = %q, want %q", i, got.name, wantName)
		}
		if got.call.CallerSubjectID != "user:alice" {
			t.Errorf("%s CallerSubjectID = %q, want user:alice", got.name, got.call.CallerSubjectID)
		}
		if got.call.CallerBearerToken != "session-token" {
			t.Errorf("%s CallerBearerToken = %q, want session-token", got.name, got.call.CallerBearerToken)
		}
		if got.call.Introspection == nil || got.call.Introspection.Subject != "user:alice@example.com" {
			t.Errorf("%s Introspection = %#v, want preserved provider alias", got.name, got.call.Introspection)
		}
	}
}

func TestIdentityToGRPCErrorPreservesProviderStatus(t *testing.T) {
	t.Parallel()

	err := identityToGRPCError("token", gestalt.Unauthenticated("subject token is inactive"))
	if got := status.Code(err); got != codes.Unauthenticated {
		t.Fatalf("status.Code() = %v, want %v", got, codes.Unauthenticated)
	}
	if got := status.Convert(err).Message(); got != "subject token is inactive" {
		t.Fatalf("status message = %q, want subject token is inactive", got)
	}
}
