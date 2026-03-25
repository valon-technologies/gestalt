package pluginapi

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/text/message"
)

var defaultPrinter = message.NewPrinter(message.MatchLanguage("en"))

func validateConfigSchema(config map[string]any, schemaJSON string) error {
	var raw any
	if err := json.Unmarshal([]byte(schemaJSON), &raw); err != nil {
		return fmt.Errorf("invalid config schema JSON: %w", err)
	}

	c := jsonschema.NewCompiler()
	if err := c.AddResource("schema.json", raw); err != nil {
		return fmt.Errorf("loading config schema: %w", err)
	}
	sch, err := c.Compile("schema.json")
	if err != nil {
		return fmt.Errorf("compiling config schema: %w", err)
	}

	err = sch.Validate(config)
	if err == nil {
		return nil
	}

	ve, ok := err.(*jsonschema.ValidationError)
	if !ok {
		return fmt.Errorf("config validation: %w", err)
	}

	var msgs []string
	collectErrors(ve, &msgs)
	return fmt.Errorf("config validation failed:\n  %s", strings.Join(msgs, "\n  "))
}

func collectErrors(ve *jsonschema.ValidationError, out *[]string) {
	if len(ve.Causes) == 0 {
		path := "/" + strings.Join(ve.InstanceLocation, "/")
		if len(ve.InstanceLocation) == 0 {
			path = "(root)"
		}
		*out = append(*out, fmt.Sprintf("%s: %s", path, ve.ErrorKind.LocalizedString(defaultPrinter)))
		return
	}
	for _, cause := range ve.Causes {
		collectErrors(cause, out)
	}
}
