package invocation

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/services/egress"
)

func TestCredentialInstanceAndHeaderOverridesUsesTenantHeader(t *testing.T) {
	t.Parallel()

	credentialInstance, overrides := credentialInstanceAndHeaderOverrides(map[string]string{
		"X-Tenant-Sid": "TENDefault",
	}, "TENSelected")
	if credentialInstance != "" {
		t.Fatalf("credential instance = %q, want empty", credentialInstance)
	}
	if overrides["X-Tenant-Sid"] != "TENSelected" {
		t.Fatalf("overrides = %#v, want TENSelected tenant header", overrides)
	}
}

func TestCredentialInstanceAndHeaderOverridesPreservesCredentialInstance(t *testing.T) {
	t.Parallel()

	credentialInstance, overrides := credentialInstanceAndHeaderOverrides(map[string]string{
		"X-Api-Version": "1",
	}, "team-a")
	if credentialInstance != "team-a" {
		t.Fatalf("credential instance = %q, want team-a", credentialInstance)
	}
	if len(overrides) != 0 {
		t.Fatalf("overrides = %#v, want none", overrides)
	}
}

func TestApplyOutboundHeaderOverridesOverridesStaticHeader(t *testing.T) {
	t.Parallel()

	ctx := egress.WithOutboundHeaderOverrides(context.Background(), map[string]string{
		"X-Tenant-Sid": "TENSelected",
	})
	headers := egress.ApplyOutboundHeaderOverrides(ctx, map[string]string{
		"X-Tenant-Sid": "TENDefault",
		"X-Other":      "value",
	})
	if headers["X-Tenant-Sid"] != "TENSelected" {
		t.Fatalf("X-Tenant-Sid = %q, want TENSelected", headers["X-Tenant-Sid"])
	}
	if headers["X-Other"] != "value" {
		t.Fatalf("X-Other = %q, want value", headers["X-Other"])
	}
}
