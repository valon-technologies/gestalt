package jira

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
	discoveryURL      = "https://api.atlassian.com/oauth/token/accessible-resources"
)

func TestDefinitionParses(t *testing.T) {
	t.Parallel()

	inv, err := inventory.Load()
	if err != nil {
		t.Fatalf("inventory.Load: %v", err)
	}
	spec := inv.Providers["jira"]

	def := providertest.ParseDefinition(t, definitionYAML)
	providertest.CheckDefinition(t, def, providertest.DefinitionExpect{
		Name:           "jira",
		OperationCount: len(spec.Operations),
		AuthType:       spec.AuthType,
		ConnectionMode: spec.ConnectionMode,
		Connection: map[string]providertest.ConnParam{
			"cloud_id": {Required: true, From: "discovery"},
		},
	})
}

func TestBuildProvider(t *testing.T) {
	t.Parallel()

	inv, err := inventory.Load()
	if err != nil {
		t.Fatalf("inventory.Load: %v", err)
	}
	spec := inv.Providers["jira"]

	def := providertest.ParseDefinition(t, definitionYAML)
	prov := providertest.BuildProvider(t, def, config.IntegrationDef{
		ClientID:     dummyClientID,
		ClientSecret: dummyClientSecret,
	})

	providertest.CheckProvider(t, prov, providertest.ProviderExpect{
		Name:           "jira",
		ConnectionMode: core.ConnectionMode(spec.ConnectionMode),
		OperationCount: len(spec.Operations),
		OperationNames: spec.Operations,
	})

	providertest.CheckDiscovery(t, prov, providertest.DiscoveryExpect{
		URL:             discoveryURL,
		MetadataMapping: map[string]string{"cloud_id": "id"},
	})
}
