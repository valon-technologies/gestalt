package pluginpkg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/santhosh-tekuri/jsonschema/v6"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/sdk/pluginmanifest/v1"
)

func ValidateConfigForManifest(manifestPath string, manifest *pluginmanifestv1.Manifest, config map[string]any) error {
	if manifest == nil || manifest.Provider == nil || manifest.Provider.ConfigSchemaPath == "" {
		return nil
	}

	schemaPath := filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(manifest.Provider.ConfigSchemaPath))
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		return fmt.Errorf("read config schema %q: %w", manifest.Provider.ConfigSchemaPath, err)
	}

	var schemaDoc any
	if err := json.Unmarshal(data, &schemaDoc); err != nil {
		return fmt.Errorf("invalid config schema: %w", err)
	}

	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("config.schema.json", schemaDoc); err != nil {
		return fmt.Errorf("invalid config schema: %w", err)
	}
	schema, err := compiler.Compile("config.schema.json")
	if err != nil {
		return fmt.Errorf("compile config schema: %w", err)
	}

	if config == nil {
		config = map[string]any{}
	}
	if err := schema.Validate(config); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}
	return nil
}
