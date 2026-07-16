package publicsurface

import (
	"fmt"
	"sort"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

// Project returns the public App schema: only services in view, public methods
// only, with server-filled and rejected request fields omitted from messages.
func Project(full *model.Schema, view *View) (*model.Schema, error) {
	if full == nil || view == nil {
		return &model.Schema{}, nil
	}
	omitByInput, err := inputFieldPolicies(view)
	if err != nil {
		return nil, err
	}

	var services []*model.Service
	for _, svc := range view.Services {
		methods := make([]*model.Method, len(svc.PublicMethods))
		for i, m := range svc.PublicMethods {
			cloned := *m
			if m.Input != nil {
				if omitted := OmittedFields(m); len(omitted) > 0 {
					cloned.Input = CloneMessageOmitting(m.Input, omitted)
				}
			}
			methods[i] = &cloned
		}
		services = append(services, &model.Service{
			Doc:         svc.Doc,
			FullName:    svc.FullName,
			Name:        svc.Name,
			ProtoFile:   svc.ProtoFile,
			Methods:     methods,
			HostBinding: svc.HostBinding,
		})
	}

	messages := make([]*model.Message, 0, len(view.Messages))
	for _, m := range view.Messages {
		if omitted := omitByInput[m.FullName]; len(omitted) > 0 {
			messages = append(messages, CloneMessageOmitting(m, omitted))
			continue
		}
		messages = append(messages, m)
	}

	return &model.Schema{
		Services: services,
		Messages: messages,
		Enums:    append([]*model.Enum(nil), view.Enums...),
	}, nil
}

// MessageIndex builds a full-name message lookup from schema, applying public
// field omissions from view to cloned request messages.
func MessageIndex(full *model.Schema, view *View) (map[string]*model.Message, error) {
	out := map[string]*model.Message{}
	if full == nil {
		return out, nil
	}
	for _, m := range full.Messages {
		out[m.FullName] = m
	}
	if view == nil {
		return out, nil
	}
	omitByInput, err := inputFieldPolicies(view)
	if err != nil {
		return nil, err
	}
	for fullName, omitted := range omitByInput {
		if m := out[fullName]; m != nil && len(omitted) > 0 {
			out[fullName] = CloneMessageOmitting(m, omitted)
		}
	}
	return out, nil
}

// Reachable returns messages and enums transitively referenced by service
// method I/O, using messages from idx.
func Reachable(
	messages map[string]*model.Message,
	enums map[string]*model.Enum,
	services []*model.Service,
) ([]*model.Message, []*model.Enum) {
	seenMessages := map[string]bool{}
	seenEnums := map[string]bool{}

	var visit func(fullName string)
	visitRef := func(ref *model.TypeRef) {
		if ref == nil {
			return
		}
		switch ref.Kind {
		case model.KindMessage:
			visit(ref.Message)
		case model.KindEnum:
			seenEnums[ref.Enum] = true
		}
	}
	visit = func(fullName string) {
		if fullName == "" || seenMessages[fullName] {
			return
		}
		seenMessages[fullName] = true
		m := messages[fullName]
		if m == nil {
			return
		}
		for _, f := range m.Fields {
			switch f.Kind {
			case model.KindMessage:
				visit(f.Message)
			case model.KindEnum:
				seenEnums[f.Enum] = true
			case model.KindRepeated:
				visitRef(f.Elem)
			case model.KindMap:
				visitRef(f.MapValue)
			default:
				visitRef(fieldRef(f))
			}
		}
		for _, o := range m.Oneofs {
			for _, f := range oneofFields(m, o) {
				visitRef(fieldRef(f))
			}
		}
	}

	for _, svc := range services {
		for _, method := range svc.Methods {
			if method.Input != nil {
				visit(method.Input.FullName)
			}
			if method.Output != nil {
				visit(method.Output.FullName)
			}
		}
	}

	var outMessages []*model.Message
	for name := range seenMessages {
		if m := messages[name]; m != nil {
			outMessages = append(outMessages, m)
		}
	}
	sort.Slice(outMessages, func(i, j int) bool { return outMessages[i].FullName < outMessages[j].FullName })

	var outEnums []*model.Enum
	for name := range seenEnums {
		if e := enums[name]; e != nil {
			outEnums = append(outEnums, e)
		}
	}
	sort.Slice(outEnums, func(i, j int) bool { return outEnums[i].FullName < outEnums[j].FullName })
	return outMessages, outEnums
}

func inputFieldPolicies(view *View) (map[string]map[string]bool, error) {
	out := map[string]map[string]bool{}
	for _, svc := range view.Services {
		for _, m := range svc.PublicMethods {
			if m.Input == nil {
				continue
			}
			omitted := OmittedFields(m)
			key := m.Input.FullName
			if existing, ok := out[key]; ok {
				if !sameOmittedFields(existing, omitted) {
					return nil, fmt.Errorf(
						"publicsurface: conflicting fill/reject policy for request %s",
						key,
					)
				}
				continue
			}
			out[key] = omitted
		}
	}
	return out, nil
}

func sameOmittedFields(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for name := range a {
		if a[name] != b[name] {
			return false
		}
	}
	return true
}

// CloneMessageOmitting returns a copy of m without the named fields.
func CloneMessageOmitting(m *model.Message, omitted map[string]bool) *model.Message {
	if m == nil {
		return nil
	}
	clone := *m
	clone.Fields = nil
	clone.Oneofs = nil
	oneofDropped := map[int]bool{}
	for _, f := range m.Fields {
		if omitted[f.Name] {
			if f.OneofIndex >= 0 {
				oneofDropped[f.OneofIndex] = true
			}
			continue
		}
		clone.Fields = append(clone.Fields, f)
	}
	for i, o := range m.Oneofs {
		if oneofDropped[i] {
			continue
		}
		clone.Oneofs = append(clone.Oneofs, o)
	}
	return &clone
}

func fieldRef(f *model.Field) *model.TypeRef {
	if f == nil {
		return nil
	}
	return &model.TypeRef{
		Kind:    f.Kind,
		Scalar:  f.Scalar,
		Message: f.Message,
		Enum:    f.Enum,
	}
}

func oneofFields(m *model.Message, o *model.Oneof) []*model.Field {
	var out []*model.Field
	for _, number := range o.FieldNumbers {
		for _, f := range m.Fields {
			if f.Number == number {
				out = append(out, f)
			}
		}
	}
	return out
}
