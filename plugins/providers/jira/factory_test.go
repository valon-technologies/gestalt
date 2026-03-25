package jira

import (
	"testing"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/provider"
	"gopkg.in/yaml.v3"
)

const (
	testClientID     = "dummy-client-id"
	testClientSecret = "dummy-client-secret"
	expectedOps      = 7
	discoveryURL     = "https://api.atlassian.com/oauth/token/accessible-resources"
)

func TestDefinitionParses(t *testing.T) {
	t.Parallel()

	var def provider.Definition
	if err := yaml.Unmarshal(definitionYAML, &def); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if def.Provider != "jira" {
		t.Fatalf("expected provider jira, got %q", def.Provider)
	}
	if len(def.Operations) != expectedOps {
		t.Fatalf("expected %d operations, got %d", expectedOps, len(def.Operations))
	}

	cloudID, ok := def.Connection["cloud_id"]
	if !ok {
		t.Fatal("missing cloud_id connection param")
	}
	if cloudID.From != "discovery" {
		t.Fatalf("expected cloud_id from=discovery, got %q", cloudID.From)
	}
	if !cloudID.Required {
		t.Fatal("expected cloud_id to be required")
	}
}

func TestBuildProvider(t *testing.T) {
	t.Parallel()

	var def provider.Definition
	if err := yaml.Unmarshal(definitionYAML, &def); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	intg := config.IntegrationDef{
		ClientID:     testClientID,
		ClientSecret: testClientSecret,
	}

	prov, err := provider.Build(&def, intg, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if prov.Name() != "jira" {
		t.Fatalf("expected name jira, got %q", prov.Name())
	}
	if prov.ConnectionMode() != core.ConnectionModeUser {
		t.Fatalf("expected connection mode user, got %q", prov.ConnectionMode())
	}

	ops := prov.ListOperations()
	if len(ops) != expectedOps {
		t.Fatalf("expected %d operations, got %d", expectedOps, len(ops))
	}

	dcp, ok := prov.(core.DiscoveryConfigProvider)
	if !ok {
		t.Fatal("provider does not implement DiscoveryConfigProvider")
	}

	dc := dcp.DiscoveryConfig()
	if dc == nil {
		t.Fatal("DiscoveryConfig() returned nil")
	}
	if dc.URL != discoveryURL {
		t.Fatalf("expected discovery URL %q, got %q", discoveryURL, dc.URL)
	}
	if dc.MetadataMapping["cloud_id"] != "id" {
		t.Fatalf("expected metadata_mapping cloud_id=id, got %q", dc.MetadataMapping["cloud_id"])
	}
}
