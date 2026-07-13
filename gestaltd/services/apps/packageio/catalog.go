package packageio

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"gopkg.in/yaml.v3"
)

const StaticCatalogFile = "catalog.yaml"

func StaticCatalogPath(rootDir string) string {
	if rootDir == "" {
		return StaticCatalogFile
	}
	return filepath.Join(rootDir, StaticCatalogFile)
}

func StaticCatalogRequired(manifest *providermanifestv1.Manifest) bool {
	return manifest != nil && manifest.Kind == providermanifestv1.KindApp && manifest.Spec != nil && !manifest.Spec.IsManifestBacked()
}

func ReadStaticCatalog(rootDir, name string) (*catalog.Catalog, error) {
	catalogPath := StaticCatalogPath(rootDir)
	data, err := os.ReadFile(catalogPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read static catalog %q: %w", catalogPath, err)
	}
	if err := rejectProviderAllowedRoles(data, catalogPath); err != nil {
		return nil, err
	}

	var cat catalog.Catalog
	if err := decodeStrict(data, ManifestFormatFromPath(catalogPath), "static catalog", &cat); err != nil {
		return nil, err
	}

	if cat.Name == "" && name != "" {
		cat.Name = name
	}
	if err := cat.Validate(); err != nil {
		return nil, fmt.Errorf("validate static catalog %q: %w", catalogPath, err)
	}
	return &cat, nil
}

func rejectProviderAllowedRoles(data []byte, source string) error {
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("parse static catalog YAML: %w", err)
	}
	if len(document.Content) == 0 {
		return nil
	}
	root := document.Content[0]
	operations := yamlMappingValue(root, "operations")
	if operations == nil || operations.Kind != yaml.SequenceNode {
		return nil
	}
	for index, operation := range operations.Content {
		if yamlMappingValue(operation, "allowedRoles") == nil {
			continue
		}
		id := ""
		if value := yamlMappingValue(operation, "id"); value != nil {
			id = value.Value
		}
		if id == "" {
			id = fmt.Sprintf("at index %d", index)
		}
		return fmt.Errorf("static catalog %q operation %q: provider-owned allowedRoles are not supported; move roles to apps.<name>.allowedOperations in config.yaml", source, id)
	}
	return nil
}

func yamlMappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}
