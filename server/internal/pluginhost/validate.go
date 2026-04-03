package pluginhost

import (
	"github.com/valon-technologies/gestalt/server/internal/configschema"
)

func validateConfigSchema(config map[string]any, schemaText string) error {
	return configschema.Validate(config, schemaText)
}
