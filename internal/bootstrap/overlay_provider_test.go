package bootstrap

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/provider"
	"gopkg.in/yaml.v3"
)

const (
	overlayTestDummyClientID     = "dummy-client-id"
	overlayTestDummyClientSecret = "dummy-client-secret"

	bqOpListDatasets = "list_datasets"
	bqOpGetDataset   = "get_dataset"
	bqOpListTables   = "list_tables"
	bqOpGetTable     = "get_table"
	bqOpListRoutines = "list_routines"
	bqOpQuery        = "query"

	bqOverlayOpCount = 6
)

var bqBaseOps = []string{
	bqOpListDatasets,
	bqOpGetDataset,
	bqOpListTables,
	bqOpGetTable,
	bqOpListRoutines,
}

func TestOverlayProviderComposition(t *testing.T) {
	t.Parallel()

	bin := buildBigQueryPluginBinary(t)

	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"bigquery": {
				ClientID:     overlayTestDummyClientID,
				ClientSecret: overlayTestDummyClientSecret,
				Plugin: &config.ExecutablePluginDef{
					Mode:    config.PluginModeOverlay,
					Command: bin,
				},
			},
		},
	}

	factories := NewFactoryRegistry()
	factories.Providers["bigquery"] = bigqueryFactoryFromDisk(t)

	providers, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{})
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	prov, err := providers.Get("bigquery")
	if err != nil {
		t.Fatalf("providers.Get: %v", err)
	}

	ops := prov.ListOperations()
	if len(ops) != bqOverlayOpCount {
		t.Fatalf("ListOperations returned %d ops, want %d", len(ops), bqOverlayOpCount)
	}

	opNames := make([]string, len(ops))
	for i, op := range ops {
		opNames[i] = op.Name
	}

	for _, expected := range bqBaseOps {
		if !slices.Contains(opNames, expected) {
			t.Errorf("missing base operation %q in %v", expected, opNames)
		}
	}
	if !slices.Contains(opNames, bqOpQuery) {
		t.Fatalf("missing overlay operation %q in %v", bqOpQuery, opNames)
	}

	catProv, ok := prov.(interface {
		Catalog() *catalog.Catalog
	})
	if !ok {
		t.Fatal("overlay provider does not implement CatalogProvider")
	}

	cat := catProv.Catalog()
	if cat == nil {
		t.Fatal("Catalog() returned nil")
	}

	catOpIDs := make(map[string]bool, len(cat.Operations))
	for _, catOp := range cat.Operations {
		catOpIDs[catOp.ID] = true
	}
	for _, expected := range bqBaseOps {
		if !catOpIDs[expected] {
			t.Errorf("missing base operation %q in catalog", expected)
		}
	}
}

func bigqueryFactoryFromDisk(t *testing.T) ProviderFactory {
	t.Helper()
	root := repoRoot(t)
	yamlPath := filepath.Join(root, "plugins", "providers", "bigquery", "provider.yaml")
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("reading bigquery provider.yaml: %v", err)
	}

	return func(_ context.Context, _ string, intg config.IntegrationDef, _ Deps) (core.Provider, error) {
		var def provider.Definition
		if err := yaml.Unmarshal(data, &def); err != nil {
			return nil, err
		}
		return provider.Build(&def, intg, nil)
	}
}

func buildBigQueryPluginBinary(t *testing.T) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "gestalt-plugin-bigquery")
	root := repoRoot(t)
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/gestalt-plugin-bigquery")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build bigquery plugin binary: %v\n%s", err, out)
	}
	return bin
}
