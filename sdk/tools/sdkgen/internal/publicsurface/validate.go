package publicsurface

import (
	"fmt"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

// Validate checks that the schema's public App surface is supported by SDK-2.
func Validate(schema *model.Schema) error {
	if schema == nil {
		return nil
	}
	for _, svc := range schema.Services {
		if svc.Name != AppServiceName {
			continue
		}
		for _, m := range svc.Methods {
			if !m.Public {
				continue
			}
			if m.Stream != model.Unary {
				return fmt.Errorf("%s: public streaming methods are not supported", m.FullMethod)
			}
			if m.InputIsEmpty {
				return fmt.Errorf("%s: public methods with google.protobuf.Empty request are not supported", m.FullMethod)
			}
			if m.OutputIsEmpty {
				return fmt.Errorf("%s: public methods with google.protobuf.Empty response are not supported", m.FullMethod)
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
