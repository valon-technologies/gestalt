package providerpkg

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/santhosh-tekuri/jsonschema/v6"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"gopkg.in/yaml.v3"
)

func ValidateConfigForManifest(manifestPath string, manifest *providermanifestv1.Manifest, kind string, config map[string]any) error {
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

	if config == nil {
		config = map[string]any{}
	}
	return validateConfigSchema(config, string(data))
}

func configSchemaForManifest(manifestPath string, manifest *providermanifestv1.Manifest, _ string) (path string, name string, ok bool, err error) {
	if manifest == nil || manifest.Spec == nil || manifest.Spec.ConfigSchemaPath == "" {
		return "", "", false, nil
	}
	schemaPath := manifest.Spec.ConfigSchemaPath
	return filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(schemaPath)), schemaPath, true, nil
}

func validateConfigSchema(config map[string]any, schemaText string) error {
	var schemaDoc any
	if err := yaml.Unmarshal([]byte(schemaText), &schemaDoc); err != nil {
		return fmt.Errorf("invalid config schema: %w", err)
	}

	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("config.schema", schemaDoc); err != nil {
		return fmt.Errorf("invalid config schema: %w", err)
	}

	schema, err := compiler.Compile("config.schema")
	if err != nil {
		return fmt.Errorf("compile config schema: %w", err)
	}

	if err := schema.Validate(config); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}
	return nil
}
