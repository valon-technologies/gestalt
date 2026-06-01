package appaccess

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestInvocationTokenUsesCallerAppClaim(t *testing.T) {
	t.Parallel()

	manager, err := NewInvocationTokenManager([]byte("invocation-token-test-secret"))
	if err != nil {
		t.Fatalf("NewInvocationTokenManager: %v", err)
	}

	ctx := invocation.WithCallerProvider(context.Background(), invocation.ProviderKindApp, "caller")
	token, err := manager.MintRootToken(ctx, "caller", nil)
	if err != nil {
		t.Fatalf("MintRootToken: %v", err)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d segments, want 3", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("unmarshal claims: %v", err)
	}
	if got := claims["caller_app"]; got != "caller" {
		t.Fatalf("caller_app claim = %v, want caller", got)
	}
	if got := claims["caller_provider_kind"]; got != string(invocation.ProviderKindApp) {
		t.Fatalf("caller_provider_kind claim = %v, want app", got)
	}
	for key := range claims {
		if strings.HasPrefix(key, "caller_") && key != "caller_app" && key != "caller_provider_kind" {
			t.Fatalf("unexpected caller claim %q", key)
		}
	}
}

func TestInvocationTokenDoesNotInferCallerProviderKind(t *testing.T) {
	t.Parallel()

	manager, err := NewInvocationTokenManager([]byte("invocation-token-test-secret"))
	if err != nil {
		t.Fatalf("NewInvocationTokenManager: %v", err)
	}

	token, err := manager.MintRootToken(context.Background(), "caller", nil)
	if err != nil {
		t.Fatalf("MintRootToken: %v", err)
	}
	claims, err := manager.parseClaims(token)
	if err != nil {
		t.Fatalf("parseClaims: %v", err)
	}
	if got := claims.CallerApp; got != "caller" {
		t.Fatalf("caller app = %q, want caller", got)
	}
	if got := claims.CallerProviderKind; got != "" {
		t.Fatalf("caller provider kind = %q, want empty", got)
	}
}

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

	childToken, err := manager.ExchangeToken(rootToken, nil, 15*time.Minute)
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
	refreshedToken, err := manager.ExchangeToken(childToken, nil, 15*time.Minute)
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
	if _, err := manager.ExchangeToken(childToken, nil, time.Minute); err == nil {
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

	if _, err := manager.ExchangeToken(rootToken, InvocationGrants{
		"example": {Operations: map[string]core.ConnectionMode{"request_context": ""}},
	}, time.Minute); err != nil {
		t.Fatalf("ExchangeToken should allow narrowing wildcard grants: %v", err)
	}
}

func TestInvocationTokenExchangePreservesInvocationDelegationMetadata(t *testing.T) {
	t.Parallel()

	manager, err := NewInvocationTokenManager([]byte("invocation-token-delegation-secret"))
	if err != nil {
		t.Fatalf("NewInvocationTokenManager: %v", err)
	}

	now := time.Date(2026, time.April, 21, 12, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }

	delegation := InvocationDelegation{
		RunAs: &core.RunAsSubject{
			SubjectID:           "service_account:data-schema-explorer",
			SubjectKind:         "service_account",
			CredentialSubjectID: "service_account:data-schema-explorer",
			DisplayName:         "Data Schema Explorer",
		},
	}
	rootToken, err := manager.MintRootToken(context.Background(), "caller", InvocationGrants{
		InvocationGrantAllApps: {AllOperations: true},
		"frontPorchRestApi": {
			Operations: map[string]core.ConnectionMode{
				"vds.schemaVersions": core.ConnectionModeSubject,
			},
			OperationDelegations: map[string]InvocationDelegation{
				"vds.schemaVersions": delegation,
			},
		},
	})
	if err != nil {
		t.Fatalf("MintRootToken: %v", err)
	}

	for _, tc := range []struct {
		name   string
		grants InvocationGrants
	}{
		{
			name: "exact operation",
			grants: InvocationGrants{
				"frontPorchRestApi": {Operations: map[string]core.ConnectionMode{"vds.schemaVersions": ""}},
			},
		},
		{
			name: "app all operations",
			grants: InvocationGrants{
				"frontPorchRestApi": {AllOperations: true},
			},
		},
		{
			name: "wildcard all operations",
			grants: InvocationGrants{
				InvocationGrantAllApps: {AllOperations: true},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			childToken, err := manager.ExchangeToken(rootToken, tc.grants, time.Minute)
			if err != nil {
				t.Fatalf("ExchangeToken: %v", err)
			}
			tokenCtx, err := manager.resolveToken(childToken, "caller")
			if err != nil {
				t.Fatalf("resolveToken: %v", err)
			}
			if !allowsOperation(tokenCtx.grants, "frontPorchRestApi", "vds.schemaVersions") {
				t.Fatal("exchanged token does not allow frontPorchRestApi.vds.schemaVersions")
			}
			profile, ok := EffectiveOperationProfile(tokenCtx.grants, "frontPorchRestApi", "vds.schemaVersions")
			if !ok {
				t.Fatal("exchanged token has no profile for frontPorchRestApi.vds.schemaVersions")
			}
			if got := profile.CredentialMode; got != core.ConnectionModeSubject {
				t.Fatalf("operation credential mode = %q, want subject", got)
			}
			if got := profile.Delegation; !invocationDelegationsEqual(got, delegation) {
				t.Fatalf("operation delegation = %#v, want %#v", got, delegation)
			}
		})
	}

	_, err = manager.ExchangeToken(rootToken, InvocationGrants{
		"frontPorchRestApi": {
			Operations: map[string]core.ConnectionMode{"vds.schemaVersions": ""},
			OperationDelegations: map[string]InvocationDelegation{
				"vds.schemaVersions": {RunAs: &core.RunAsSubject{SubjectID: "service_account:other"}},
			},
		},
	}, time.Minute)
	if err == nil {
		t.Fatal("ExchangeToken should reject changed delegation metadata")
	}

	_, err = manager.ExchangeToken(rootToken, InvocationGrants{
		"frontPorchRestApi": {
			Operations: map[string]core.ConnectionMode{"vds.schemaVersions": core.ConnectionModeNone},
		},
	}, time.Minute)
	if err == nil {
		t.Fatal("ExchangeToken should reject changed credential-mode metadata")
	}
}

func TestAppInvocationExchangeRequiresExplicitGrantScope(t *testing.T) {
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

	server := NewAppServer(nil, manager)
	_, err = server.ExchangeInvocationToken(context.Background(), &proto.ExchangeInvocationTokenRequest{
		ParentInvocationToken: rootToken,
		Grants: []*proto.AppInvocationGrant{{
			App: "example",
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

func TestInvocationTokenRestoresWorkflowContext(t *testing.T) {
	t.Parallel()

	manager, err := NewInvocationTokenManager([]byte("invocation-token-workflow-context-secret"))
	if err != nil {
		t.Fatalf("NewInvocationTokenManager: %v", err)
	}

	ctx := principal.WithPrincipal(
		context.Background(),
		&principal.Principal{
			SubjectID: "user:test-user",
			UserID:    "test-user",
			Kind:      principal.KindUser,
			Source:    principal.SourceSession,
		},
	)
	ctx = invocation.WithCallerProvider(ctx, invocation.ProviderKindApp, "caller")
	ctx = invocation.WithWorkflowContext(ctx, map[string]any{
		"providerName":       "temporal",
		"runId":              "run-123",
		"definitionId":       "def-123",
		"definitionRevision": "7",
		"activationId":       "webhook",
		"step": map[string]any{
			"id": "notify",
		},
	})
	token, err := manager.MintRootToken(ctx, "caller", InvocationGrants{
		"slack": {Operations: map[string]core.ConnectionMode{"chat.postMessage": ""}},
	})
	if err != nil {
		t.Fatalf("MintRootToken: %v", err)
	}

	tokenCtx, err := manager.ResolveToken(token, "caller")
	if err != nil {
		t.Fatalf("ResolveToken: %v", err)
	}
	restored := RestoreTokenContext(context.Background(), tokenCtx, "")
	if got := InvocationTokenFromContext(restored); got != token {
		t.Fatalf("restored invocation token = %q, want original token", got)
	}
	caller := invocation.CallerProviderFromContext(restored)
	if caller.Kind != invocation.ProviderKindApp || caller.Name != "caller" {
		t.Fatalf("restored caller provider = %#v, want app/caller", caller)
	}
	workflow := invocation.WorkflowContextFromContext(restored)
	if got := invocation.WorkflowContextString(workflow, "providerName"); got != "temporal" {
		t.Fatalf("providerName = %q, want temporal", got)
	}
	if got := invocation.WorkflowContextString(workflow, "runId"); got != "run-123" {
		t.Fatalf("runId = %q, want run-123", got)
	}
	step := invocation.WorkflowContextMap(workflow, "step")
	if got := invocation.WorkflowContextString(step, "id"); got != "notify" {
		t.Fatalf("step.id = %q, want notify", got)
	}
}

func TestInvocationTokenExchangePreservesWorkflowContext(t *testing.T) {
	t.Parallel()

	manager, err := NewInvocationTokenManager([]byte("invocation-token-workflow-context-exchange-secret"))
	if err != nil {
		t.Fatalf("NewInvocationTokenManager: %v", err)
	}

	ctx := principal.WithPrincipal(
		context.Background(),
		&principal.Principal{
			SubjectID: "user:test-user",
			UserID:    "test-user",
			Kind:      principal.KindUser,
			Source:    principal.SourceSession,
		},
	)
	ctx = invocation.WithWorkflowContext(ctx, map[string]any{
		"providerName": "temporal",
		"runId":        "run-123",
		"step": map[string]any{
			"id": "notify",
		},
	})
	rootToken, err := manager.MintRootToken(ctx, "caller", InvocationGrants{
		"source": {Operations: map[string]core.ConnectionMode{"messages.send": ""}},
		"target": {Operations: map[string]core.ConnectionMode{"items.create": ""}},
	})
	if err != nil {
		t.Fatalf("MintRootToken: %v", err)
	}
	childToken, err := manager.ExchangeToken(rootToken, InvocationGrants{
		"source": {Operations: map[string]core.ConnectionMode{"messages.send": ""}},
	}, time.Minute)
	if err != nil {
		t.Fatalf("ExchangeToken: %v", err)
	}

	tokenCtx, err := manager.ResolveToken(childToken, "caller")
	if err != nil {
		t.Fatalf("ResolveToken(child): %v", err)
	}
	restored := RestoreTokenContext(context.Background(), tokenCtx, "")
	workflow := invocation.WorkflowContextFromContext(restored)
	if got := invocation.WorkflowContextString(workflow, "providerName"); got != "temporal" {
		t.Fatalf("providerName = %q, want temporal", got)
	}
	if got := invocation.WorkflowContextString(workflow, "runId"); got != "run-123" {
		t.Fatalf("runId = %q, want run-123", got)
	}
	step := invocation.WorkflowContextMap(workflow, "step")
	if got := invocation.WorkflowContextString(step, "id"); got != "notify" {
		t.Fatalf("step.id = %q, want notify", got)
	}
}

func TestDecodeInvocationGrantClaimsIgnoresModesForUndeclaredOperations(t *testing.T) {
	t.Parallel()

	grants := decodeInvocationGrantClaims(map[string]invocationGrantClaims{
		"source": {
			Operations: []string{"messages.send"},
			OperationModes: map[string]string{
				"messages.send":  "subject",
				"messages.reply": "none",
			},
		},
	})

	sourceGrant := grants["source"]
	if _, ok := sourceGrant.Operations["messages.reply"]; ok {
		t.Fatal("decodeInvocationGrantClaims should not add operations that only appear in operation_modes")
	}
	if got := sourceGrant.Operations["messages.send"]; got != core.ConnectionModeSubject {
		t.Fatalf("messages.send mode = %q, want %q", got, core.ConnectionModeSubject)
	}
}

func TestDecodeInvocationGrantClaimsRequiresGrantScope(t *testing.T) {
	t.Parallel()

	grants := decodeInvocationGrantClaims(map[string]invocationGrantClaims{
		"source": {},
	})

	if allowsOperation(grants, "source", "messages.send") {
		t.Fatal("empty grant claims should not grant wildcard operation access")
	}
}
