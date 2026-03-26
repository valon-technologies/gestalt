package providerinfo_test

import (
	"context"
	"slices"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/providerinfo"
)

type baseProvider struct {
	name string
}

func (p *baseProvider) Name() string                        { return p.name }
func (p *baseProvider) DisplayName() string                 { return p.name }
func (p *baseProvider) Description() string                 { return "" }
func (p *baseProvider) ConnectionMode() core.ConnectionMode { return core.ConnectionModeUser }
func (p *baseProvider) ListOperations() []core.Operation    { return nil }
func (p *baseProvider) Execute(context.Context, string, map[string]any, string) (*core.OperationResult, error) {
	return nil, nil
}

type specProvider struct {
	baseProvider
	spec core.ConnectionSpec
}

func (p *specProvider) ConnectionSpec() core.ConnectionSpec {
	return p.spec
}

type authTypeProvider struct {
	baseProvider
	authTypes []string
}

func (p *authTypeProvider) AuthTypes() []string {
	return slices.Clone(p.authTypes)
}

type manualProvider struct {
	baseProvider
	manual bool
}

func (p *manualProvider) SupportsManualAuth() bool {
	return p.manual
}

type oauthManualProvider struct {
	manualProvider
}

func (p *oauthManualProvider) AuthorizationURL(string, []string) string { return "" }

func (p *oauthManualProvider) ExchangeCode(context.Context, string) (*core.TokenResponse, error) {
	return nil, nil
}

func (p *oauthManualProvider) RefreshToken(context.Context, string) (*core.TokenResponse, error) {
	return nil, nil
}

type metadataProvider struct {
	authTypeProvider
	connectionParams map[string]core.ConnectionParamDef
	discovery        *core.DiscoveryConfig
}

func (p *metadataProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	out := make(map[string]core.ConnectionParamDef, len(p.connectionParams))
	for name, def := range p.connectionParams {
		out[name] = def
	}
	return out
}

func (p *metadataProvider) DiscoveryConfig() *core.DiscoveryConfig {
	if p.discovery == nil {
		return nil
	}
	clone := *p.discovery
	if p.discovery.MetadataMapping != nil {
		clone.MetadataMapping = make(map[string]string, len(p.discovery.MetadataMapping))
		for key, value := range p.discovery.MetadataMapping {
			clone.MetadataMapping[key] = value
		}
	}
	return &clone
}

func TestResolveConnectionSpec_PrefersConnectionSpecProvider(t *testing.T) {
	t.Parallel()

	prov := &specProvider{
		baseProvider: baseProvider{name: "test"},
		spec: core.ConnectionSpec{
			AuthTypes: []string{"manual"},
			ConnectionParams: map[string]core.ConnectionParamDef{
				"tenant": {Required: true},
			},
			Discovery: &core.DiscoveryConfig{
				URL:             "https://example.com/discover",
				MetadataMapping: map[string]string{"region": "region"},
			},
		},
	}

	got := providerinfo.ResolveConnectionSpec(prov)
	got.AuthTypes[0] = "oauth"
	got.ConnectionParams["tenant"] = core.ConnectionParamDef{Description: "mutated"}
	got.Discovery.URL = "https://mutated.invalid"
	got.Discovery.MetadataMapping["region"] = "mutated"

	if prov.spec.AuthTypes[0] != "manual" {
		t.Fatalf("spec auth types were not cloned: %+v", prov.spec.AuthTypes)
	}
	if prov.spec.ConnectionParams["tenant"].Description != "" {
		t.Fatalf("spec connection params were not cloned: %+v", prov.spec.ConnectionParams["tenant"])
	}
	if prov.spec.Discovery.URL != "https://example.com/discover" {
		t.Fatalf("spec discovery URL was not cloned: %q", prov.spec.Discovery.URL)
	}
	if prov.spec.Discovery.MetadataMapping["region"] != "region" {
		t.Fatalf("spec discovery metadata was not cloned: %+v", prov.spec.Discovery.MetadataMapping)
	}
}

func TestResolveConnectionSpec_FallsBackToLegacyMetadataInterfaces(t *testing.T) {
	t.Parallel()

	prov := &metadataProvider{
		authTypeProvider: authTypeProvider{
			baseProvider: baseProvider{name: "test"},
			authTypes:    []string{"oauth", "manual"},
		},
		connectionParams: map[string]core.ConnectionParamDef{
			"tenant":  {Required: true},
			"team_id": {From: "token_response", Field: "team_id"},
		},
		discovery: &core.DiscoveryConfig{
			URL:             "https://example.com/discover",
			MetadataMapping: map[string]string{"project": "id"},
		},
	}

	got := providerinfo.ResolveConnectionSpec(prov)

	if !slices.Equal(got.AuthTypes, []string{"oauth", "manual"}) {
		t.Fatalf("auth types = %+v", got.AuthTypes)
	}
	if got.ConnectionParams["tenant"].Required != true {
		t.Fatalf("tenant param missing from resolved spec: %+v", got.ConnectionParams)
	}
	if got.Discovery == nil || got.Discovery.URL != "https://example.com/discover" {
		t.Fatalf("discovery = %+v", got.Discovery)
	}
}

func TestResolveConnectionSpec_DerivesLegacyAuthDefaults(t *testing.T) {
	t.Parallel()

	manualOnly := providerinfo.ResolveConnectionSpec(&manualProvider{
		baseProvider: baseProvider{name: "manual"},
		manual:       true,
	})
	if !slices.Equal(manualOnly.AuthTypes, []string{"manual"}) {
		t.Fatalf("manual-only auth types = %+v", manualOnly.AuthTypes)
	}

	oauthAndManual := providerinfo.ResolveConnectionSpec(&oauthManualProvider{
		manualProvider: manualProvider{
			baseProvider: baseProvider{name: "hybrid"},
			manual:       true,
		},
	})
	if !slices.Equal(oauthAndManual.AuthTypes, []string{"oauth", "manual"}) {
		t.Fatalf("oauth+manual auth types = %+v", oauthAndManual.AuthTypes)
	}

	defaultOAuth := providerinfo.ResolveConnectionSpec(&baseProvider{name: "default"})
	if !slices.Equal(defaultOAuth.AuthTypes, []string{"oauth"}) {
		t.Fatalf("default auth types = %+v", defaultOAuth.AuthTypes)
	}
}

func TestUserConnectionParams_FiltersNonUserProvidedEntries(t *testing.T) {
	t.Parallel()

	spec := core.ConnectionSpec{
		ConnectionParams: map[string]core.ConnectionParamDef{
			"tenant":  {Required: true},
			"team_id": {From: "token_response", Field: "team_id"},
			"region":  {From: "discovery", Field: "region"},
		},
	}

	got := providerinfo.UserConnectionParams(spec)
	if len(got) != 1 {
		t.Fatalf("user params len = %d, want 1", len(got))
	}
	if _, ok := got["tenant"]; !ok {
		t.Fatalf("tenant missing from user params: %+v", got)
	}
	if _, ok := got["team_id"]; ok {
		t.Fatalf("team_id should have been filtered out: %+v", got)
	}
}
