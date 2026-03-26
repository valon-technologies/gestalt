package dbt_cloud

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
	spec, ok := inv.Providers["dbt_cloud"]
	if !ok {
		t.Fatal("dbt_cloud not found in inventory")
	}

	def := providertest.ParseDefinition(t, definitionYAML)
	providertest.CheckDefinition(t, def, providertest.DefinitionExpect{
		Name:           "dbt_cloud",
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
	spec, ok := inv.Providers["dbt_cloud"]
	if !ok {
		t.Fatal("dbt_cloud not found in inventory")
	}

	def := providertest.ParseDefinition(t, definitionYAML)
	prov := providertest.BuildProvider(t, def, config.IntegrationDef{
		ClientID:     dummyClientID,
		ClientSecret: dummyClientSecret,
	})

	providertest.CheckProvider(t, prov, providertest.ProviderExpect{
		Name:           "dbt_cloud",
		ConnectionMode: core.ConnectionMode(spec.ConnectionMode),
		OperationCount: len(spec.Operations),
		OperationNames: spec.Operations,
	})

	providertest.CheckManualAuth(t, prov)
	providertest.CheckCatalog(t, prov)
}
