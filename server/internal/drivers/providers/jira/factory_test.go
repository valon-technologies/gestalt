package jira

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/drivers/providers/providertest"
)

const (
	dummyClientID     = "dummy-client-id"
	dummyClientSecret = "dummy-client-secret"
	discoveryURL      = "https://api.atlassian.com/oauth/token/accessible-resources"
	expectedAuthType  = "oauth2"
	expectedConnMode  = "user"
)

var expectedOperationNames = []string{
	"add_comment",
	"create_issue",
	"get_issue",
	"get_transitions",
	"list_projects",
	"search_issues",
	"transition_issue",
}

func TestDefinitionParses(t *testing.T) {
	t.Parallel()

	def := providertest.ParseDefinition(t, definitionYAML)
	providertest.CheckDefinition(t, def, providertest.DefinitionExpect{
		Name:           "jira",
		OperationCount: len(expectedOperationNames),
		AuthType:       expectedAuthType,
		ConnectionMode: expectedConnMode,
		Connection: map[string]providertest.ConnParam{
			"cloud_id": {Required: true, From: "discovery"},
		},
	})
}

func TestBuildProvider(t *testing.T) {
	t.Parallel()

	def := providertest.ParseDefinition(t, definitionYAML)
	prov := providertest.BuildProvider(t, def, config.ConnectionDef{Auth: config.ConnectionAuthDef{
		ClientID:     dummyClientID,
		ClientSecret: dummyClientSecret,
	}})

	providertest.CheckProvider(t, prov, providertest.ProviderExpect{
		Name:           "jira",
		ConnectionMode: core.ConnectionMode(expectedConnMode),
		OperationCount: len(expectedOperationNames),
		OperationNames: expectedOperationNames,
	})

	providertest.CheckDiscovery(t, prov, providertest.DiscoveryExpect{
		URL:             discoveryURL,
		MetadataMapping: map[string]string{"cloud_id": "id"},
	})
}
