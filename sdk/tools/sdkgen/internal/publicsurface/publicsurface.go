// Package publicsurface derives the public client view from the normalized
// sdkgen model.
package publicsurface

import (
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

// AppServiceName is the only public service generated in SDK-2.
const AppServiceName = "App"

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

// Build constructs the public view, retaining only PUBLIC methods on App.
func Build(schema *model.Schema) *View {
	if schema == nil {
		return &View{}
	}
	msgIndex := map[string]*model.Message{}
	enumIndex := map[string]*model.Enum{}
	for _, m := range schema.Messages {
		msgIndex[m.FullName] = m
	}
	for _, e := range schema.Enums {
		enumIndex[e.FullName] = e
	}

	var services []*Service
	for _, svc := range schema.Services {
		if svc.Name != AppServiceName {
			continue
		}
		var publicMethods []*model.Method
		for _, m := range svc.Methods {
			if !m.Public || m.Stream != model.Unary {
				continue
			}
			publicMethods = append(publicMethods, m)
		}
		if len(publicMethods) == 0 {
			continue
		}
		services = append(services, &Service{
			Service:       svc,
			PublicMethods: publicMethods,
		})
	}

	projected := &model.Schema{Services: make([]*model.Service, 0, len(services))}
	for _, svc := range services {
		projected.Services = append(projected.Services, &model.Service{
			FullName: svc.FullName,
			Name:     svc.Name,
			Methods:  svc.PublicMethods,
		})
	}
	view := &View{Services: services}
	view.Messages, view.Enums = Reachable(msgIndex, enumIndex, projected.Services)
	return view
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

// PolicyFill returns server-filled field names for a public method.
func PolicyFill(m *model.Method) []string {
	if m == nil || m.PublicPolicy == nil {
		return nil
	}
	return append([]string(nil), m.PublicPolicy.Fill...)
}

// PolicyReject returns client-rejected field names for a public method.
func PolicyReject(m *model.Method) []string {
	if m == nil || m.PublicPolicy == nil {
		return nil
	}
	return append([]string(nil), m.PublicPolicy.Reject...)
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
