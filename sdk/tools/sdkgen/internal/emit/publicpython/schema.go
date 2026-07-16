package publicpython

import (
	"sort"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/publicsurface"
)

// publicSchema projects the normalized model to the public client surface.
func publicSchema(schema *model.Schema, view *publicsurface.View) *model.Schema {
	if schema == nil || view == nil {
		return &model.Schema{}
	}
	msgByName := map[string]*model.Message{}
	for _, m := range schema.Messages {
		msgByName[m.FullName] = m
	}
	omitByInput := omittedFieldsByInput(view)

	var services []*model.Service
	for _, svc := range view.Services {
		methods := append([]*model.Method(nil), svc.PublicMethods...)
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
			messages = append(messages, cloneMessageOmitting(m, omitted))
			continue
		}
		messages = append(messages, m)
	}

	return &model.Schema{
		Services: services,
		Messages: messages,
		Enums:    append([]*model.Enum(nil), view.Enums...),
	}
}

func omittedFieldsByInput(view *publicsurface.View) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, svc := range view.Services {
		for _, m := range svc.PublicMethods {
			if m.Input == nil {
				continue
			}
			omitted := publicsurface.OmittedFields(m)
			if len(omitted) == 0 {
				continue
			}
			key := m.Input.FullName
			if out[key] == nil {
				out[key] = map[string]bool{}
			}
			for name, drop := range omitted {
				if drop {
					out[key][name] = true
				}
			}
		}
	}
	return out
}

func cloneMessageOmitting(m *model.Message, omitted map[string]bool) *model.Message {
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

func restMethods(view *publicsurface.View) []*restMethod {
	var out []*restMethod
	for _, svc := range view.Services {
		for _, m := range svc.PublicMethods {
			if m.HTTP == nil {
				continue
			}
			out = append(out, &restMethod{svc: svc.Service, method: m})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		left := out[i].method.FullMethod
		right := out[j].method.FullMethod
		return left < right
	})
	return out
}

type restMethod struct {
	svc    *model.Service
	method *model.Method
}
