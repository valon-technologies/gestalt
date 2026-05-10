package bootstrap

import (
	"context"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/server/internal/config"
)

func TestHostedRuntimeStartSessionRequestIncludesTenantScope(t *testing.T) {
	t.Parallel()

	ctx := gestalt.ContextWithOutgoingTenantScope(context.Background(), gestalt.TenantScope{
		TenantID:    "acme",
		Host:        "acme.dev.valon.tools",
		TenantBound: true,
		PrincipalID: "user:123",
	})
	req := buildHostedRuntimeStartSessionRequest(ctx, "plugin", "httpbin", config.EffectiveRuntimePlacement{
		Metadata: map[string]string{"existing": "value"},
	})

	if req.Metadata["existing"] != "value" {
		t.Fatalf("existing metadata = %q, want value", req.Metadata["existing"])
	}
	if req.Metadata[gestalt.TenantIDMetadataKey] != "acme" {
		t.Fatalf("tenant metadata = %q, want acme", req.Metadata[gestalt.TenantIDMetadataKey])
	}
	if req.Metadata[gestalt.TenantHostMetadataKey] != "acme.dev.valon.tools" {
		t.Fatalf("tenant host metadata = %q, want acme.dev.valon.tools", req.Metadata[gestalt.TenantHostMetadataKey])
	}
	if req.Metadata[gestalt.TenantBoundMetadataKey] != "true" {
		t.Fatalf("tenant bound metadata = %q, want true", req.Metadata[gestalt.TenantBoundMetadataKey])
	}
	if req.Metadata[gestalt.TenantPrincipalIDMetadataKey] != "user:123" {
		t.Fatalf("tenant principal metadata = %q, want user:123", req.Metadata[gestalt.TenantPrincipalIDMetadataKey])
	}
}
