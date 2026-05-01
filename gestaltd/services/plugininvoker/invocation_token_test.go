package plugininvoker

import (
	"context"
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestInvocationTokenExchangePreservesAbsoluteDelegationExpiry(t *testing.T) {
	t.Parallel()

	manager, err := NewInvocationTokenManager([]byte("invocation-token-test-secret"))
	if err != nil {
		t.Fatalf("NewInvocationTokenManager: %v", err)
	}

	baseTime := time.Date(2026, time.April, 21, 12, 0, 0, 0, time.UTC)
	now := baseTime
	manager.now = func() time.Time { return now }
	manager.rootTTL = time.Minute
	manager.defaultChildTTL = 10 * time.Minute
	manager.maxChildTTL = 15 * time.Minute

	ctx := invocation.ContextWithMeta(
		principal.WithPrincipal(
			context.Background(),
			&principal.Principal{
				SubjectID: "user:test-user",
				UserID:    "test-user",
				Kind:      principal.KindUser,
				Source:    principal.SourceSession,
			},
		),
		&invocation.InvocationMeta{RequestID: "req-1"},
	)
	rootToken, err := manager.MintRootToken(ctx, "caller", InvocationGrants{
		"example": {Operations: map[string]core.ConnectionMode{"request_context": ""}},
	})
	if err != nil {
		t.Fatalf("MintRootToken: %v", err)
	}

	childToken, err := manager.ExchangeToken(rootToken, "caller", nil, 15*time.Minute)
	if err != nil {
		t.Fatalf("ExchangeToken(root): %v", err)
	}
	childClaims, err := manager.parseClaims(childToken)
	if err != nil {
		t.Fatalf("parseClaims(child): %v", err)
	}
	wantExpiry := baseTime.Add(15 * time.Minute)
	if got := childClaims.ExpiresAt.Time; !got.Equal(wantExpiry) {
		t.Fatalf("child expiry = %s, want %s", got, wantExpiry)
	}

	now = baseTime.Add(14 * time.Minute)
	refreshedToken, err := manager.ExchangeToken(childToken, "caller", nil, 15*time.Minute)
	if err != nil {
		t.Fatalf("ExchangeToken(child): %v", err)
	}
	refreshedClaims, err := manager.parseClaims(refreshedToken)
	if err != nil {
		t.Fatalf("parseClaims(refreshed): %v", err)
	}
	if got := refreshedClaims.ExpiresAt.Time; !got.Equal(wantExpiry) {
		t.Fatalf("refreshed expiry = %s, want %s", got, wantExpiry)
	}

	now = baseTime.Add(16 * time.Minute)
	if _, err := manager.ExchangeToken(childToken, "caller", nil, time.Minute); err == nil {
		t.Fatal("ExchangeToken should reject tokens after the delegation window expires")
	}
}

func TestInvocationTokenExchangeAllowsNarrowingWildcardGrants(t *testing.T) {
	t.Parallel()

	manager, err := NewInvocationTokenManager([]byte("invocation-token-test-secret"))
	if err != nil {
		t.Fatalf("NewInvocationTokenManager: %v", err)
	}

	now := time.Date(2026, time.April, 21, 12, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }

	ctx := principal.WithPrincipal(
		context.Background(),
		&principal.Principal{
			SubjectID: "user:test-user",
			UserID:    "test-user",
			Kind:      principal.KindUser,
			Source:    principal.SourceSession,
		},
	)
	rootToken, err := manager.MintRootToken(ctx, "caller", InvocationGrants{
		"example": {AllOperations: true},
	})
	if err != nil {
		t.Fatalf("MintRootToken: %v", err)
	}

	if _, err := manager.ExchangeToken(rootToken, "caller", InvocationGrants{
		"example": {Operations: map[string]core.ConnectionMode{"request_context": ""}},
	}, time.Minute); err != nil {
		t.Fatalf("ExchangeToken should allow narrowing wildcard grants: %v", err)
	}
}

func TestPluginInvokerExchangeRequiresExplicitGrantScope(t *testing.T) {
	t.Parallel()

	manager, err := NewInvocationTokenManager([]byte("invocation-token-test-secret"))
	if err != nil {
		t.Fatalf("NewInvocationTokenManager: %v", err)
	}

	now := time.Date(2026, time.April, 21, 12, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }

	ctx := principal.WithPrincipal(
		context.Background(),
		&principal.Principal{
			SubjectID: "user:test-user",
			UserID:    "test-user",
			Kind:      principal.KindUser,
			Source:    principal.SourceSession,
		},
	)
	rootToken, err := manager.MintRootToken(ctx, "caller", InvocationGrants{
		"example": {AllOperations: true},
	})
	if err != nil {
		t.Fatalf("MintRootToken: %v", err)
	}

	server := NewPluginInvokerServer("caller", []config.PluginInvocationDependency{{
		Plugin:    "example",
		Operation: "request_context",
	}}, nil, manager)
	_, err = server.ExchangeInvocationToken(context.Background(), &proto.ExchangeInvocationTokenRequest{
		ParentInvocationToken: rootToken,
		Grants: []*proto.PluginInvocationGrant{{
			Plugin: "example",
		}},
		TtlSeconds: 60,
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("ExchangeInvocationToken error = %v, want InvalidArgument", err)
	}
}

func TestInvocationTokenResolvePreservesEmailOnlyPrincipals(t *testing.T) {
	t.Parallel()

	manager, err := NewInvocationTokenManager([]byte("invocation-token-test-secret"))
	if err != nil {
		t.Fatalf("NewInvocationTokenManager: %v", err)
	}

	now := time.Date(2026, time.April, 21, 12, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }

	ctx := principal.WithPrincipal(
		context.Background(),
		&principal.Principal{
			Identity: &core.UserIdentity{
				Email:       "ada@example.com",
				DisplayName: "Ada",
			},
			Kind:   principal.KindUser,
			Source: principal.SourceEnv,
		},
	)
	token, err := manager.MintRootToken(ctx, "caller", InvocationGrants{
		"example": {Operations: map[string]core.ConnectionMode{"request_context": ""}},
	})
	if err != nil {
		t.Fatalf("MintRootToken: %v", err)
	}

	tokenCtx, err := manager.resolveToken(token, "caller")
	if err != nil {
		t.Fatalf("ResolveToken: %v", err)
	}
	if tokenCtx.principal == nil || tokenCtx.principal.Identity == nil {
		t.Fatal("ResolveToken should preserve email-only identity metadata")
	}
	if got := tokenCtx.principal.Identity.Email; got != "ada@example.com" {
		t.Fatalf("resolved email = %q, want ada@example.com", got)
	}
}

func TestDecodeInvocationGrantClaimsIgnoresModesForUndeclaredOperations(t *testing.T) {
	t.Parallel()

	grants := decodeInvocationGrantClaims(map[string]invocationGrantClaims{
		"slack": {
			Operations: []string{"chat.postMessage"},
			OperationModes: map[string]string{
				"chat.postMessage": "user",
				"events.reply":     "none",
			},
		},
	})

	slackGrant := grants["slack"]
	if _, ok := slackGrant.Operations["events.reply"]; ok {
		t.Fatal("decodeInvocationGrantClaims should not add operations that only appear in operation_modes")
	}
	if got := slackGrant.Operations["chat.postMessage"]; got != core.ConnectionModeUser {
		t.Fatalf("chat.postMessage mode = %q, want %q", got, core.ConnectionModeUser)
	}
}

func TestDecodeInvocationGrantClaimsRequiresGrantScope(t *testing.T) {
	t.Parallel()

	grants := decodeInvocationGrantClaims(map[string]invocationGrantClaims{
		"slack": {},
	})

	if allowsOperation(grants, "slack", "chat.postMessage") {
		t.Fatal("empty grant claims should not grant wildcard operation access")
	}
}
