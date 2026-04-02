package pluginhost

import (
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

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
