// Package publicsurface derives the public client view from the normalized
// sdkgen model.
package publicsurface

import (
	"sort"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

// View is the public client projection of a schema.
type View struct {
	Services []*Service
	Messages []*model.Message
	Enums    []*model.Enum
}

// Service is one public service with its public methods.
type Service struct {
	*model.Service
	PublicMethods []*model.Method
}

// Build constructs the public view, retaining only PUBLIC methods.
func Build(schema *model.Schema) *View {
	if schema == nil {
		return &View{}
	}
	seenMessages := map[string]bool{}
	seenEnums := map[string]bool{}
	var services []*Service

	for _, svc := range schema.Services {
		var publicMethods []*model.Method
		for _, m := range svc.Methods {
			if !m.Public || m.Stream != model.Unary {
				continue
			}
			publicMethods = append(publicMethods, m)
			collectReachable(m, seenMessages, seenEnums)
		}
		if len(publicMethods) == 0 {
			continue
		}
		services = append(services, &Service{
			Service:       svc,
			PublicMethods: publicMethods,
		})
	}

	var messages []*model.Message
	for fullName := range seenMessages {
		for _, m := range schema.Messages {
			if m.FullName == fullName {
				messages = append(messages, m)
				break
			}
		}
	}
	sort.Slice(messages, func(i, j int) bool { return messages[i].FullName < messages[j].FullName })

	var enums []*model.Enum
	for fullName := range seenEnums {
		for _, e := range schema.Enums {
			if e.FullName == fullName {
				enums = append(enums, e)
				break
			}
		}
	}
	sort.Slice(enums, func(i, j int) bool { return enums[i].FullName < enums[j].FullName })

	sort.Slice(services, func(i, j int) bool { return services[i].FullName < services[j].FullName })
	return &View{Services: services, Messages: messages, Enums: enums}
}

// RESTMethodCount returns public methods with HTTP annotations.
func RESTMethodCount(view *View) int {
	n := 0
	for _, svc := range view.Services {
		for _, m := range svc.PublicMethods {
			if m.HTTP != nil {
				n++
			}
		}
	}
	return n
}

// GRPCMethodCount returns all public unary methods.
func GRPCMethodCount(view *View) int {
	n := 0
	for _, svc := range view.Services {
		n += len(svc.PublicMethods)
	}
	return n
}

// OmittedFields returns fill and reject field names for a public method.
func OmittedFields(m *model.Method) map[string]bool {
	out := map[string]bool{}
	if m == nil || m.PublicPolicy == nil {
		return out
	}
	for _, name := range m.PublicPolicy.Fill {
		out[name] = true
	}
	for _, name := range m.PublicPolicy.Reject {
		out[name] = true
	}
	return out
}

func collectReachable(m *model.Method, messages, enums map[string]bool) {
	if m.Input != nil {
		visitMessage(m.Input, messages, enums)
	}
	if m.Output != nil {
		visitMessage(m.Output, messages, enums)
	}
}

func visitMessage(m *model.Message, messages, enums map[string]bool) {
	if m == nil || messages[m.FullName] {
		return
	}
	messages[m.FullName] = true
	for _, f := range m.Fields {
		visitField(f, messages, enums)
	}
}

func visitField(f *model.Field, messages, enums map[string]bool) {
	switch f.Kind {
	case model.KindMessage:
		// resolved later from schema index during emit
	case model.KindEnum:
		enums[f.Enum] = true
	case model.KindRepeated:
		visitRef(f.Elem, messages, enums)
	case model.KindMap:
		visitRef(f.MapValue, messages, enums)
	}
}

func visitRef(ref *model.TypeRef, messages, enums map[string]bool) {
	if ref == nil {
		return
	}
	switch ref.Kind {
	case model.KindMessage:
		// placeholder; emitters resolve from schema
	case model.KindEnum:
		enums[ref.Enum] = true
	}
}
