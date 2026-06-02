package server_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

func TestHostedHTTPBinding_RejectsOneSegmentRoutesForSessionCatalogProviders(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	prov := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N:        "reports",
				ConnMode: core.ConnectionModeNone,
			},
		},
		catalog: serverTestCatalog("reports", []catalog.CatalogOperation{{
			ID:        "handle_status",
			Method:    http.MethodPost,
			Transport: catalog.TransportREST,
		}}),
	}
	providers := testutil.NewProviderRegistry(t, prov)
	cfg := server.Config{
		Auth:        &coretesting.StubAuthProvider{N: "none"},
		Services:    svc,
		Providers:   providers,
		Invoker:     invocation.NewBroker(providers, svc.Users, svc.ExternalCredentials),
		StateSecret: []byte("0123456789abcdef0123456789abcdef"),
		AppDefs: map[string]*config.ProviderEntry{
			"reports": {
				SecuritySchemes: map[string]*config.HTTPSecurityScheme{
					"none": {Type: providermanifestv1.HTTPSecuritySchemeTypeNone},
				},
				HTTP: map[string]*config.HTTPBinding{
					"status_binding": {
						Path:     "/status",
						Method:   http.MethodPost,
						Security: "none",
						Target:   "handle_status",
					},
				},
			},
		},
	}

	_, err := server.New(cfg)
	if err == nil {
		t.Fatal("expected session catalog route conflict")
	}
	if !strings.Contains(err.Error(), "session catalog operation route") {
		t.Fatalf("error = %v, want session catalog operation route", err)
	}
}

func TestHostedHTTPBinding_RejectsReservedMCPBindingTargets(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	providers := testutil.NewProviderRegistry(t, &stubIntegrationWithCatalog{
		StubIntegration: coretesting.StubIntegration{
			N:        "reports",
			ConnMode: core.ConnectionModeNone,
		},
		catalog: serverTestCatalog("reports", []catalog.CatalogOperation{{
			ID:          "reserved_status",
			Method:      http.MethodPost,
			InputSchema: json.RawMessage(`{"type":"object","properties":{"_connection":{"type":"string"}}}`),
			Transport:   catalog.TransportMCPPassthrough,
		}}),
	})
	cfg := server.Config{
		Auth:        &coretesting.StubAuthProvider{N: "none"},
		Services:    svc,
		Providers:   providers,
		Invoker:     invocation.NewBroker(providers, svc.Users, svc.ExternalCredentials),
		StateSecret: []byte("0123456789abcdef0123456789abcdef"),
		AppDefs: map[string]*config.ProviderEntry{
			"reports": {
				SecuritySchemes: map[string]*config.HTTPSecurityScheme{
					"none": {Type: providermanifestv1.HTTPSecuritySchemeTypeNone},
				},
				HTTP: map[string]*config.HTTPBinding{
					"status_binding": {
						Path:           "/status",
						Method:         http.MethodPost,
						Security:       "none",
						Target:         "reserved_status",
						CredentialMode: providermanifestv1.ConnectionModeNone,
					},
				},
			},
		},
	}

	_, err := server.New(cfg)
	if err == nil {
		t.Fatal("expected reserved MCP binding target rejection")
	}
	if !strings.Contains(err.Error(), "reserved REST control parameters") {
		t.Fatalf("error = %v, want reserved REST control parameters", err)
	}
}

func TestHostedHTTPBinding_AllowsReservedNamesForRESTTargets(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	providers := testutil.NewProviderRegistry(t, &stubIntegrationWithCatalog{
		StubIntegration: coretesting.StubIntegration{
			N:        "reports",
			ConnMode: core.ConnectionModeNone,
		},
		catalog: serverTestCatalog("reports", []catalog.CatalogOperation{{
			ID:          "rest_status",
			Method:      http.MethodPost,
			InputSchema: json.RawMessage(`{"type":"object","properties":{"_connection":{"type":"string"}}}`),
			Transport:   catalog.TransportREST,
		}}),
	})
	cfg := server.Config{
		Auth:        &coretesting.StubAuthProvider{N: "none"},
		Services:    svc,
		Providers:   providers,
		Invoker:     invocation.NewBroker(providers, svc.Users, svc.ExternalCredentials),
		StateSecret: []byte("0123456789abcdef0123456789abcdef"),
		AppDefs: map[string]*config.ProviderEntry{
			"reports": {
				SecuritySchemes: map[string]*config.HTTPSecurityScheme{
					"none": {Type: providermanifestv1.HTTPSecuritySchemeTypeNone},
				},
				HTTP: map[string]*config.HTTPBinding{
					"status_binding": {
						Path:           "/custom/status",
						Method:         http.MethodPost,
						Security:       "none",
						Target:         "rest_status",
						CredentialMode: providermanifestv1.ConnectionModeNone,
					},
				},
			},
		},
	}

	if _, err := server.New(cfg); err != nil {
		t.Fatalf("New: %v", err)
	}
}
