package bigquery

import (
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/provider"
)

const (
	dummyClientID     = "dummy-client-id"
	dummyClientSecret = "dummy-client-secret"
	expectedOpCount   = 5
)

func TestDefinitionParses(t *testing.T) {
	t.Parallel()

	var def provider.Definition
	if err := yaml.Unmarshal(definitionYAML, &def); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if def.Provider != "bigquery" {
		t.Fatalf("provider = %q, want bigquery", def.Provider)
	}
	if def.ConnectionMode != "user" {
		t.Fatalf("connection_mode = %q, want user", def.ConnectionMode)
	}
	if len(def.Operations) != expectedOpCount {
		t.Fatalf("operations = %d, want %d", len(def.Operations), expectedOpCount)
	}

	wantOps := []string{"list_datasets", "get_dataset", "list_tables", "get_table", "list_routines"}
	for _, name := range wantOps {
		if _, ok := def.Operations[name]; !ok {
			t.Errorf("missing operation %q", name)
		}
	}
}

func TestBuildProvider(t *testing.T) {
	t.Parallel()

	var def provider.Definition
	if err := yaml.Unmarshal(definitionYAML, &def); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	intg := config.IntegrationDef{
		ClientID:     dummyClientID,
		ClientSecret: dummyClientSecret,
	}

	p, err := provider.Build(&def, intg, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if p.Name() != "bigquery" {
		t.Fatalf("Name() = %q, want bigquery", p.Name())
	}
	if p.ConnectionMode() != core.ConnectionModeUser {
		t.Fatalf("ConnectionMode() = %q, want user", p.ConnectionMode())
	}

	ops := p.ListOperations()
	if len(ops) != expectedOpCount {
		t.Fatalf("ListOperations() = %d, want %d", len(ops), expectedOpCount)
	}

	opNames := make(map[string]bool, len(ops))
	for _, op := range ops {
		opNames[op.Name] = true
	}
	for _, name := range []string{"list_datasets", "get_dataset", "list_tables", "get_table", "list_routines"} {
		if !opNames[name] {
			t.Errorf("missing operation %q in built provider", name)
		}
	}
}
