package publicsurface

import (
	"fmt"
	"strings"

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
	if err := validateCollisions(view); err != nil {
		return err
	}
	if _, err := ProjectGRPC(schema, view); err != nil {
		return err
	}
	if _, err := ProjectREST(schema, view); err != nil {
		return err
	}
	if _, err := ParseMethods(schema, view, ProjectionGRPC); err != nil {
		return err
	}
	if _, err := ParseMethods(schema, view, ProjectionREST); err != nil {
		return err
	}
	return nil
}

func validateCollisions(view *View) error {
	if view == nil {
		return nil
	}
	serviceNames := map[string]string{}
	clientNames := map[string]string{}
	methodKeys := map[string]string{}
	messageNames := map[string]string{}
	enumNames := map[string]string{}
	moduleBases := map[string]string{}
	requestAliases := map[string]string{}

	for _, svc := range view.Services {
		local := svc.Name
		if existing, ok := serviceNames[local]; ok {
			return fmt.Errorf("publicsurface: duplicate service local name %q (%s and %s)", local, existing, svc.FullName)
		}
		serviceNames[local] = svc.FullName

		client := local + "Client"
		if existing, ok := clientNames[client]; ok {
			return fmt.Errorf("publicsurface: duplicate client name %q (%s and %s)", client, existing, svc.FullName)
		}
		clientNames[client] = svc.FullName

		if svc.ProtoFile != "" {
			base := protoModuleBase(svc.ProtoFile)
			if existing, ok := moduleBases[base]; ok && existing != svc.FullName {
				return fmt.Errorf("publicsurface: duplicate generated module basename %q", base)
			}
			moduleBases[base] = svc.FullName
		}

		for _, m := range svc.PublicMethods {
			key := svc.FullName + "." + m.Name
			if existing, ok := methodKeys[key]; ok {
				return fmt.Errorf("publicsurface: duplicate method key %q (%s)", key, existing)
			}
			methodKeys[key] = m.FullMethod

			if m.Input != nil && !m.InputIsEmpty {
				alias := requestAliasName(svc.Name, m.Name)
				if existing, ok := requestAliases[alias]; ok {
					return fmt.Errorf("publicsurface: duplicate public request alias %q (%s and %s)", alias, existing, key)
				}
				requestAliases[alias] = key
			}
		}
	}

	for _, m := range view.Messages {
		short := messageShortName(m.FullName)
		if existing, ok := messageNames[short]; ok {
			return fmt.Errorf("publicsurface: duplicate message name %q (%s and %s)", short, existing, m.FullName)
		}
		if _, ok := enumNames[short]; ok {
			return fmt.Errorf("publicsurface: message/enum name collision %q", short)
		}
		messageNames[short] = m.FullName
	}
	for _, e := range view.Enums {
		short := messageShortName(e.FullName)
		if existing, ok := enumNames[short]; ok {
			return fmt.Errorf("publicsurface: duplicate enum name %q (%s and %s)", short, existing, e.FullName)
		}
		if _, ok := messageNames[short]; ok {
			return fmt.Errorf("publicsurface: message/enum name collision %q", short)
		}
		enumNames[short] = e.FullName
	}
	return nil
}

func protoModuleBase(protoFile string) string {
	base := protoFile
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	if strings.HasSuffix(base, ".proto") {
		base = strings.TrimSuffix(base, ".proto")
	}
	return base
}

func messageShortName(fullName string) string {
	if i := strings.LastIndex(fullName, "."); i >= 0 {
		return fullName[i+1:]
	}
	return fullName
}

func requestAliasName(service, method string) string {
	return service + method + "Request"
}
