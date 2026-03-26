package slackbot

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
	spec := inv.Providers["slack_bot"]

	def := providertest.ParseDefinition(t, definitionYAML)
	providertest.CheckDefinition(t, def, providertest.DefinitionExpect{
		Name:           "slack_bot",
		OperationCount: 13,
		AuthType:       "manual",
		ConnectionMode: "identity",
	})

	prov := providertest.BuildProvider(t, def, config.IntegrationDef{})

	providertest.CheckProvider(t, prov, providertest.ProviderExpect{
		Name:           "slack_bot",
		ConnectionMode: core.ConnectionMode(spec.ConnectionMode),
		OperationCount: 13,
		OperationNames: baseOperationNames(),
	})

	providertest.CheckManualAuth(t, prov)
	providertest.CheckCatalog(t, prov)
}
