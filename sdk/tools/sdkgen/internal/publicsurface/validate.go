package publicsurface

import (
	"fmt"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

// Validate checks that the schema's public surface is supported by sdkgen.
func Validate(schema *model.Schema) error {
	if schema == nil {
		return nil
	}
	for _, svc := range schema.Services {
		for _, m := range svc.Methods {
			if !m.Public {
				continue
			}
			if m.Stream != model.Unary {
				return fmt.Errorf("%s: public streaming methods are not supported", m.FullMethod)
			}
		}
	}
	view := Build(schema)
	if _, err := Project(schema, view); err != nil {
		return err
	}
	if _, err := ParseMethods(schema, view); err != nil {
		return err
	}
	return nil
}
