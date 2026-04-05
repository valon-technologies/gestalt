package pluginpkg

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"gopkg.in/yaml.v3"
)

func ReadStaticCatalog(catalogPath, name string) (*catalog.Catalog, error) {
	if catalogPath == "" {
		return nil, nil
	}

	data, err := os.ReadFile(catalogPath)
	if err != nil {
		return nil, fmt.Errorf("read static catalog %q: %w", catalogPath, err)
	}

	var cat catalog.Catalog
	if isYAMLFile(catalogPath) {
		if err := yaml.Unmarshal(data, &cat); err != nil {
			return nil, fmt.Errorf("parse static catalog YAML %q: %w", catalogPath, err)
		}
	} else {
		if err := json.Unmarshal(data, &cat); err != nil {
			return nil, fmt.Errorf("parse static catalog JSON %q: %w", catalogPath, err)
		}
	}

	if cat.Name == "" && name != "" {
		cat.Name = name
	}
	if err := cat.Validate(); err != nil {
		return nil, fmt.Errorf("validate static catalog %q: %w", catalogPath, err)
	}
	return &cat, nil
}
