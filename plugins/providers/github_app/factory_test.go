package github_app

import (
	"testing"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/plugins/providers/inventory"
	"github.com/valon-technologies/gestalt/plugins/providers/providertest"
)

const (
	dummyClientID     = "dummy-client-id"
	dummyClientSecret = "dummy-client-secret"
)

func TestDefinitionParses(t *testing.T) {
	t.Parallel()

	inv, err := inventory.Load()
	if err != nil {
		t.Fatalf("inventory.Load: %v", err)
	}
	spec, ok := inv.Providers["github_app"]
	if !ok {
		t.Fatal("provider github_app not found in inventory")
	}

	def := providertest.ParseDefinition(t, definitionYAML)
	providertest.CheckDefinition(t, def, providertest.DefinitionExpect{
		Name:           "github_app",
		OperationCount: len(spec.Operations),
		AuthType:       spec.AuthType,
		ConnectionMode: spec.ConnectionMode,
	})
}

func TestBuildProvider(t *testing.T) {
	t.Parallel()

	inv, err := inventory.Load()
	if err != nil {
		t.Fatalf("inventory.Load: %v", err)
	}
	spec, ok := inv.Providers["github_app"]
	if !ok {
		t.Fatal("provider github_app not found in inventory")
	}

	def := providertest.ParseDefinition(t, definitionYAML)
	prov := providertest.BuildProvider(t, def, config.IntegrationDef{
		ClientID:     dummyClientID,
		ClientSecret: dummyClientSecret,
	})

	providertest.CheckProvider(t, prov, providertest.ProviderExpect{
		Name:           "github_app",
		ConnectionMode: core.ConnectionMode(spec.ConnectionMode),
		OperationCount: len(spec.Operations),
		OperationNames: spec.Operations,
	})

	providertest.CheckManualAuth(t, prov)
	providertest.CheckCatalog(t, prov)
}
