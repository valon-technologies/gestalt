package datadog

import (
	"testing"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/plugins/providers/inventory"
	"github.com/valon-technologies/gestalt/plugins/providers/providertest"
)

func TestBuildProvider(t *testing.T) {
	t.Parallel()

	inv, err := inventory.Load()
	if err != nil {
		t.Fatalf("inventory.Load: %v", err)
	}
	spec, ok := inv.Providers["datadog"]
	if !ok {
		t.Fatal("provider datadog not found in inventory")
	}

	def := providertest.ParseDefinition(t, definitionYAML)
	prov := providertest.BuildProvider(t, def, config.IntegrationDef{})

	providertest.CheckProvider(t, prov, providertest.ProviderExpect{
		Name:           "datadog",
		ConnectionMode: core.ConnectionMode(spec.ConnectionMode),
		OperationCount: len(spec.Operations),
		OperationNames: spec.Operations,
	})
	providertest.CheckManualAuth(t, prov)
	providertest.CheckCatalog(t, prov)
}
