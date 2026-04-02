package pluginhost

import (
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/valon-technologies/gestalt/server/internal/jsonyaml"
)

func validateConfigSchema(config map[string]any, schemaText string) error {
	schemaDoc, err := jsonyaml.Decode([]byte(schemaText))
	if err != nil {
		return fmt.Errorf("invalid config schema: %w", err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("schema.json", schemaDoc); err != nil {
		return fmt.Errorf("invalid config schema: %w", err)
	}
	schema, err := compiler.Compile("schema.json")
	if err != nil {
		return fmt.Errorf("compile config schema: %w", err)
	}
	if err := schema.Validate(config); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}
	return nil
}
