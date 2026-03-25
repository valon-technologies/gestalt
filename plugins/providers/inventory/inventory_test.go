package inventory

import (
	"sort"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestInventoryLoads(t *testing.T) {
	t.Parallel()

	inv, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(inv.Providers) == 0 {
		t.Fatal("inventory has no providers")
	}
}

func TestProviderKeysUnique(t *testing.T) {
	t.Parallel()

	var root yaml.Node
	if err := yaml.Unmarshal(rawYAML, &root); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		t.Fatal("unexpected YAML structure")
	}
	topMap := root.Content[0]
	if topMap.Kind != yaml.MappingNode {
		t.Fatal("expected top-level mapping")
	}

	var providersNode *yaml.Node
	for i := 0; i < len(topMap.Content)-1; i += 2 {
		if topMap.Content[i].Value == "providers" {
			providersNode = topMap.Content[i+1]
			break
		}
	}
	if providersNode == nil || providersNode.Kind != yaml.MappingNode {
		t.Fatal("missing providers mapping")
	}

	seen := make(map[string]int)
	for i := 0; i < len(providersNode.Content)-1; i += 2 {
		key := providersNode.Content[i].Value
		if prev, ok := seen[key]; ok {
			t.Errorf("duplicate provider key %q at lines %d and %d", key, prev, providersNode.Content[i].Line)
		}
		seen[key] = providersNode.Content[i].Line
	}
}

func TestOperationsUniquePerProvider(t *testing.T) {
	t.Parallel()

	inv, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	for name, spec := range inv.Providers {
		seen := make(map[string]struct{}, len(spec.Operations))
		for _, op := range spec.Operations {
			if _, ok := seen[op]; ok {
				t.Errorf("provider %q: duplicate operation %q", name, op)
			}
			seen[op] = struct{}{}
		}
	}
}

func TestOperationsSorted(t *testing.T) {
	t.Parallel()

	inv, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	for name, spec := range inv.Providers {
		if !sort.StringsAreSorted(spec.Operations) {
			t.Errorf("provider %q: operations are not sorted alphabetically", name)
		}
	}
}

func TestAllProvidersHaveOperations(t *testing.T) {
	t.Parallel()

	inv, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	for name, spec := range inv.Providers {
		if len(spec.Operations) == 0 {
			t.Errorf("provider %q: has no operations", name)
		}
		if spec.AuthType == "" {
			t.Errorf("provider %q: missing auth_type", name)
		}
		if spec.ConnectionMode == "" {
			t.Errorf("provider %q: missing connection_mode", name)
		}
	}
}
