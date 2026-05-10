package gestalt

import (
	"context"
	"testing"

	"google.golang.org/grpc/metadata"
)

func TestTenantScopeContextAndOutgoingMetadata(t *testing.T) {
	ctx := ContextWithOutgoingTenantScope(context.Background(), TenantScope{
		TenantID:    " acme ",
		Host:        "Acme.Dev.Valon.Tools.",
		PrincipalID: " user:123 ",
	})

	scope, ok := TenantScopeFromContext(ctx)
	if !ok {
		t.Fatal("TenantScopeFromContext ok = false, want true")
	}
	if scope.TenantID != "acme" {
		t.Fatalf("TenantID = %q, want acme", scope.TenantID)
	}
	if scope.Host != "acme.dev.valon.tools" {
		t.Fatalf("Host = %q, want acme.dev.valon.tools", scope.Host)
	}
	if !scope.TenantBound {
		t.Fatal("TenantBound = false, want true")
	}
	if scope.PrincipalID != "user:123" {
		t.Fatalf("PrincipalID = %q, want user:123", scope.PrincipalID)
	}

	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		t.Fatal("outgoing metadata missing")
	}
	if got := md.Get(TenantIDMetadataKey); len(got) != 1 || got[0] != "acme" {
		t.Fatalf("%s metadata = %#v, want [acme]", TenantIDMetadataKey, got)
	}
	if got := md.Get(TenantBoundMetadataKey); len(got) != 1 || got[0] != "true" {
		t.Fatalf("%s metadata = %#v, want [true]", TenantBoundMetadataKey, got)
	}
}

func TestTenantScopeFromIncomingMetadata(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		TenantIDMetadataKey, "vt",
		TenantHostMetadataKey, "VT.Dev.Valon.Tools",
		TenantBoundMetadataKey, "true",
	))

	scope, ok := TenantScopeFromContext(ctx)
	if !ok {
		t.Fatal("TenantScopeFromContext ok = false, want true")
	}
	if scope.TenantID != "vt" {
		t.Fatalf("TenantID = %q, want vt", scope.TenantID)
	}
	if scope.Host != "vt.dev.valon.tools" {
		t.Fatalf("Host = %q, want vt.dev.valon.tools", scope.Host)
	}
	if !scope.TenantBound {
		t.Fatal("TenantBound = false, want true")
	}
}
