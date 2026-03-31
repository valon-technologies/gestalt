package pluginhost

import (
	"encoding/json"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func validateConfigSchema(config map[string]any, schemaJSON string) error {
	var schemaDoc any
	if err := json.Unmarshal([]byte(schemaJSON), &schemaDoc); err != nil {
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
