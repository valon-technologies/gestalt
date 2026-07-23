package invocation

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/services/egress"
)

func TestApplyInvokeHeaderOverridesAllowsDeclaredStaticHeader(t *testing.T) {
	t.Parallel()

	ctx := WithInvokeRequestHeaders(context.Background(), map[string]string{
		"X-Tenant-Sid": "TENSelected",
	})
	ctx, err := ApplyInvokeHeaderOverrides(ctx, &headerOverrideProvider{
		StubIntegration: coretesting.StubIntegration{
			N: "frontPorch",
			CatalogVal: &catalog.Catalog{
				Name: "frontPorch",
			},
		},
		staticHeaders: map[string]string{
			"X-Tenant-Sid": "TENDefault",
		},
	})
	if err != nil {
		t.Fatalf("ApplyInvokeHeaderOverrides: %v", err)
	}
	overrides := egress.OutboundHeaderOverridesFromContext(ctx)
	if overrides["X-Tenant-Sid"] != "TENSelected" {
		t.Fatalf("overrides = %#v, want TENSelected tenant header", overrides)
	}
}

func TestApplyInvokeHeaderOverridesRejectsUndeclaredHeader(t *testing.T) {
	t.Parallel()

	ctx := WithInvokeRequestHeaders(context.Background(), map[string]string{
		"X-Other": "value",
	})
	_, err := ApplyInvokeHeaderOverrides(ctx, &headerOverrideProvider{
		StubIntegration: coretesting.StubIntegration{
			N: "frontPorch",
			CatalogVal: &catalog.Catalog{
				Name: "frontPorch",
			},
		},
		staticHeaders: map[string]string{
			"X-Tenant-Sid": "TENDefault",
		},
	})
	if err == nil {
		t.Fatal("expected error for undeclared header override")
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

type headerOverrideProvider struct {
	coretesting.StubIntegration
	staticHeaders map[string]string
}

func (p *headerOverrideProvider) StaticHeaders() map[string]string {
	return p.staticHeaders
}
