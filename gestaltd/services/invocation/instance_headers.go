package invocation

import (
	"context"
	"net/http"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/egress"
)

const tenantSidHeader = "X-Tenant-Sid"

type staticHeaderProvider interface {
	StaticHeaders() map[string]string
}

// PrepareCredentialInstance splits a provider invoke instance selector from
// outbound static header overrides. Integrations that declare X-Tenant-Sid in
// their static headers treat instance as a tenant SID rather than a stored
// credential qualifier.
func PrepareCredentialInstance(ctx context.Context, prov core.Provider, instance string) (context.Context, string) {
	credentialInstance, headerOverrides := credentialInstanceAndHeaderOverrides(providerStaticHeaders(prov), instance)
	if len(headerOverrides) > 0 {
		ctx = egress.WithOutboundHeaderOverrides(ctx, headerOverrides)
	}
	return ctx, credentialInstance
}

func credentialInstanceAndHeaderOverrides(staticHeaders map[string]string, instance string) (string, map[string]string) {
	instance = strings.TrimSpace(instance)
	if instance == "" {
		return "", nil
	}
	if len(staticHeaders) == 0 {
		return instance, nil
	}
	tenantHeaderKey := ""
	for name := range staticHeaders {
		if strings.EqualFold(name, tenantSidHeader) {
			tenantHeaderKey = http.CanonicalHeaderKey(name)
			break
		}
	}
	if tenantHeaderKey == "" {
		return instance, nil
	}
	return "", map[string]string{tenantHeaderKey: instance}
}

func providerStaticHeaders(prov core.Provider) map[string]string {
	if prov == nil {
		return nil
	}
	if hp, ok := prov.(staticHeaderProvider); ok {
		return hp.StaticHeaders()
	}
	return nil
}
