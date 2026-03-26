package gmail

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

	passthroughOpCount = 10
)

func TestDefinitionParses(t *testing.T) {
	t.Parallel()

	inv, err := inventory.Load()
	if err != nil {
		t.Fatalf("inventory.Load: %v", err)
	}
	spec := inv.Providers["gmail"]

	def := providertest.ParseDefinition(t, definitionYAML)
	providertest.CheckDefinition(t, def, providertest.DefinitionExpect{
		Name:           "gmail",
		OperationCount: passthroughOpCount,
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
	spec := inv.Providers["gmail"]

	def := providertest.ParseDefinition(t, definitionYAML)
	prov := providertest.BuildProvider(t, def, config.IntegrationDef{
		ClientID:     dummyClientID,
		ClientSecret: dummyClientSecret,
	})

	providertest.CheckProvider(t, prov, providertest.ProviderExpect{
		Name:           "gmail",
		ConnectionMode: core.ConnectionMode(spec.ConnectionMode),
		OperationCount: passthroughOpCount,
	})

	providertest.CheckOAuth(t, prov)
	providertest.CheckCatalog(t, prov)
}
