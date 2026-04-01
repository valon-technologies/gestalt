package pluginpkg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/santhosh-tekuri/jsonschema/v6"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

func ValidateConfigForManifest(manifestPath string, manifest *pluginmanifestv1.Manifest, kind string, config map[string]any) error {
	schemaPath, schemaName, ok, err := configSchemaForManifest(manifestPath, manifest, kind)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		return fmt.Errorf("read config schema %q: %w", schemaName, err)
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

func configSchemaForManifest(manifestPath string, manifest *pluginmanifestv1.Manifest, kind string) (path string, name string, ok bool, err error) {
	if manifest == nil {
		return "", "", false, nil
	}

	switch kind {
	case pluginmanifestv1.KindProvider:
		if manifest.Provider == nil || manifest.Provider.ConfigSchemaPath == "" {
			return "", "", false, nil
		}
		return filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(manifest.Provider.ConfigSchemaPath)), manifest.Provider.ConfigSchemaPath, true, nil
	default:
		return "", "", false, fmt.Errorf("unsupported manifest config kind %q", kind)
	}
}
