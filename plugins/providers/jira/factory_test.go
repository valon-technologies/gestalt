package jira

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/plugins/providers/inventory"
)

const (
	dummyClientID     = "dummy-client-id"
	dummyClientSecret = "dummy-client-secret"
	discoveryURL      = "https://api.atlassian.com/oauth/token/accessible-resources"
)

func TestFactoryBuildsProviderWithCatalogAndDiscovery(t *testing.T) {
	t.Parallel()

	inv, err := inventory.Load()
	if err != nil {
		t.Fatalf("inventory.Load: %v", err)
	}
	spec := inv.Providers["jira"]

	prov, err := Factory(context.Background(), "jira", config.IntegrationDef{
		ClientID:     dummyClientID,
		ClientSecret: dummyClientSecret,
	}, bootstrap.Deps{})
	if err != nil {
		t.Fatalf("Factory: %v", err)
	}

	if prov.Name() != "jira" {
		t.Fatalf("Name() = %q, want jira", prov.Name())
	}
	if prov.ConnectionMode() != core.ConnectionMode(spec.ConnectionMode) {
		t.Fatalf("ConnectionMode() = %q, want %q", prov.ConnectionMode(), spec.ConnectionMode)
	}

	ops := prov.ListOperations()
	if len(ops) != len(spec.Operations) {
		t.Fatalf("ListOperations() returned %d operations, want %d", len(ops), len(spec.Operations))
	}
	for i, opName := range spec.Operations {
		if ops[i].Name != opName {
			t.Fatalf("operation %d = %q, want %q", i, ops[i].Name, opName)
		}
	}

	discoveryProvider, ok := prov.(core.DiscoveryConfigProvider)
	if !ok {
		t.Fatal("provider does not expose discovery config")
	}
	discovery := discoveryProvider.DiscoveryConfig()
	if discovery == nil {
		t.Fatal("DiscoveryConfig() returned nil")
	}
	if discovery.URL != discoveryURL {
		t.Fatalf("DiscoveryConfig().URL = %q, want %q", discovery.URL, discoveryURL)
	}
	if discovery.MetadataMapping["cloud_id"] != "id" {
		t.Fatalf("DiscoveryConfig().MetadataMapping = %#v", discovery.MetadataMapping)
	}

	connProvider, ok := prov.(core.ConnectionParamProvider)
	if !ok {
		t.Fatal("provider does not expose connection params")
	}
	connDefs := connProvider.ConnectionParamDefs()
	cloudID, ok := connDefs["cloud_id"]
	if !ok {
		t.Fatalf("ConnectionParamDefs() missing cloud_id: %#v", connDefs)
	}
	if !cloudID.Required || cloudID.From != "discovery" {
		t.Fatalf("cloud_id connection param = %#v", cloudID)
	}

	catalogProvider, ok := prov.(core.CatalogProvider)
	if !ok {
		t.Fatal("provider does not expose a catalog")
	}
	cat := catalogProvider.Catalog()
	if cat == nil {
		t.Fatal("Catalog() returned nil")
	}
	if cat.Name != "jira" {
		t.Fatalf("Catalog().Name = %q, want jira", cat.Name)
	}
	if len(cat.Operations) != len(spec.Operations) {
		t.Fatalf("Catalog() returned %d operations, want %d", len(cat.Operations), len(spec.Operations))
	}
}
