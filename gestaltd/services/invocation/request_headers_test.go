package invocation

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/services/egress"
)

func TestApplyInvokeHeaderOverridesAllowsDeclaredStaticHeaderThroughWrapper(t *testing.T) {
	t.Parallel()

	inner := &headerOverrideProvider{
		StubIntegration: coretesting.StubIntegration{
			N: "frontPorch",
			CatalogVal: &catalog.Catalog{
				Name: "frontPorch",
			},
		},
		staticHeaders: map[string]string{
			"X-Tenant-Sid": "TENDefault",
		},
	}
	ctx := WithInvokeRequestHeaders(context.Background(), map[string]string{
		"X-Tenant-Sid": "TENSelected",
	})
	ctx, err := ApplyInvokeHeaderOverrides(ctx, &staticHeaderForwardingWrapper{inner: inner})
	if err != nil {
		t.Fatalf("ApplyInvokeHeaderOverrides: %v", err)
	}
	overrides := egress.OutboundHeaderOverridesFromContext(ctx)
	if overrides["X-Tenant-Sid"] != "TENSelected" {
		t.Fatalf("overrides = %#v, want TENSelected tenant header", overrides)
	}
}

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

// staticHeaderForwardingWrapper mimics connection-bound provider wrappers that
// delegate StaticHeaders to the inner declarative integration.
type staticHeaderForwardingWrapper struct {
	inner *headerOverrideProvider
}

func (w *staticHeaderForwardingWrapper) Name() string        { return w.inner.Name() }
func (w *staticHeaderForwardingWrapper) DisplayName() string { return w.inner.DisplayName() }
func (w *staticHeaderForwardingWrapper) Description() string { return w.inner.Description() }
func (w *staticHeaderForwardingWrapper) ConnectionMode() core.ConnectionMode {
	return w.inner.ConnectionMode()
}
func (w *staticHeaderForwardingWrapper) AuthTypes() []string { return w.inner.AuthTypes() }
func (w *staticHeaderForwardingWrapper) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return w.inner.ConnectionParamDefs()
}
func (w *staticHeaderForwardingWrapper) CredentialFields() []core.CredentialFieldDef {
	return w.inner.CredentialFields()
}
func (w *staticHeaderForwardingWrapper) DiscoveryConfig() *core.DiscoveryConfig {
	return w.inner.DiscoveryConfig()
}
func (w *staticHeaderForwardingWrapper) ConnectionForOperation(operation string) string {
	return w.inner.ConnectionForOperation(operation)
}
func (w *staticHeaderForwardingWrapper) Catalog() *catalog.Catalog { return w.inner.Catalog() }
func (w *staticHeaderForwardingWrapper) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	return w.inner.Execute(ctx, operation, params, token)
}
func (w *staticHeaderForwardingWrapper) StaticHeaders() map[string]string {
	return w.inner.StaticHeaders()
}
