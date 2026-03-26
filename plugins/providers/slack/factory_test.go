package slack

import (
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/provider"
	"github.com/valon-technologies/gestalt/plugins/providers/inventory"
	"github.com/valon-technologies/gestalt/plugins/providers/providertest"
)

func TestBuildProvider(t *testing.T) {
	t.Parallel()

	inv, err := inventory.Load()
	if err != nil {
		t.Fatalf("inventory.Load: %v", err)
	}
	spec := inv.Providers["slack"]

	def := providertest.ParseDefinition(t, definitionYAML)
	providertest.CheckDefinition(t, def, providertest.DefinitionExpect{
		Name:           "slack",
		OperationCount: 13,
		AuthType:       "oauth2",
		ConnectionMode: "user",
	})

	auth := newSlackAuthHandler("dummy-id", "dummy-secret", "https://dummy.example.com/callback", def.Auth.Scopes)
	prov := providertest.BuildProvider(t, def, config.IntegrationDef{
		ClientID:     "dummy-id",
		ClientSecret: "dummy-secret",
	}, provider.WithAuthHandler(auth))

	providertest.CheckProvider(t, prov, providertest.ProviderExpect{
		Name:           "slack",
		ConnectionMode: core.ConnectionMode(spec.ConnectionMode),
		OperationCount: 13,
		OperationNames: baseOperationNames(),
	})

	providertest.CheckOAuth(t, prov)
	providertest.CheckCatalog(t, prov)
}

func TestSlackAuthHandler_AuthorizationURL(t *testing.T) {
	t.Parallel()

	h := newSlackAuthHandler("test-client-id", "test-secret", "https://example.com/cb", []string{"channels:read", "chat:write"})
	authURL := h.AuthorizationURL("test-state", nil)

	if authURL == "" {
		t.Fatal("AuthorizationURL returned empty string")
	}

	for _, want := range []string{"user_scope=", "test-client-id", "test-state"} {
		if !strings.Contains(authURL, want) {
			t.Errorf("AuthorizationURL missing %q in %q", want, authURL)
		}
	}
}
