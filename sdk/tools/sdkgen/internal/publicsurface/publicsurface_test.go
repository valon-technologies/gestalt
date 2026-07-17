package publicsurface_test

import (
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/publicsurface"
)

func TestBuildPublicSurface(t *testing.T) {
	t.Parallel()

	invokeInput := &model.Message{
		FullName: "gestalt.provider.v1.AppInvokeRequest",
		Name:     "AppInvokeRequest",
		Fields: []*model.Field{
			{Name: "app", JSONName: "app", Kind: model.KindScalar, Scalar: model.ScalarString},
			{Name: "operation", JSONName: "operation", Kind: model.KindScalar, Scalar: model.ScalarString},
			{Name: "params", JSONName: "params", Kind: model.KindJSONStruct},
			{Name: "context", JSONName: "context", Kind: model.KindJSONStruct},
			{Name: "run_as", JSONName: "runAs", Kind: model.KindScalar, Scalar: model.ScalarString},
		},
	}
	graphqlInput := &model.Message{
		FullName: "gestalt.provider.v1.AppInvokeGraphQLRequest",
		Name:     "AppInvokeGraphQLRequest",
		Fields: []*model.Field{
			{Name: "app", JSONName: "app", Kind: model.KindScalar, Scalar: model.ScalarString},
			{Name: "document", JSONName: "document", Kind: model.KindScalar, Scalar: model.ScalarString},
			{Name: "context", JSONName: "context", Kind: model.KindJSONStruct},
			{Name: "run_as", JSONName: "runAs", Kind: model.KindScalar, Scalar: model.ScalarString},
		},
	}
	grpcOnlyInput := &model.Message{
		FullName: "gestalt.provider.v1.GrpcOnlyRequest",
		Name:     "GrpcOnlyRequest",
	}

	schema := &model.Schema{
		Messages: []*model.Message{invokeInput, graphqlInput, grpcOnlyInput},
		Services: []*model.Service{
			{
				Name:     "App",
				FullName: "gestalt.provider.v1.App",
				Methods: []*model.Method{
					{
						Name:       "Invoke",
						FullMethod: "/gestalt.provider.v1.App/Invoke",
						Public:     true,
						Stream:     model.Unary,
						Input:      invokeInput,
						HTTP:       &model.HTTPRule{Verb: "POST", Path: "/api/v2/app/{app}/operations/{operation}", Body: "*"},
						PublicPolicy: &model.PublicPolicy{
							Fill:   []string{"context", "run_as"},
							Reject: []string{"credential_mode"},
						},
					},
					{
						Name:       "InvokeGraphQL",
						FullMethod: "/gestalt.provider.v1.App/InvokeGraphQL",
						Public:     true,
						Stream:     model.Unary,
						Input:      graphqlInput,
						HTTP:       &model.HTTPRule{Verb: "POST", Path: "/api/v2/app/{app}/graphql", Body: "*"},
						PublicPolicy: &model.PublicPolicy{
							Fill: []string{"context", "run_as"},
						},
					},
					{
						Name:       "GrpcOnly",
						FullMethod: "/gestalt.provider.v1.App/GrpcOnly",
						Public:     true,
						Stream:     model.Unary,
						Input:      grpcOnlyInput,
					},
					{
						Name:       "StreamEvents",
						FullMethod: "/gestalt.provider.v1.App/StreamEvents",
						Public:     true,
						Stream:     model.ServerStream,
					},
				},
			},
			{
				Name: "Identity",
				Methods: []*model.Method{
					{Name: "UserInfo", Public: true, Stream: model.Unary, HTTP: &model.HTTPRule{Verb: "GET", Path: "/api/v2/identity/userinfo"}},
				},
			},
		},
	}

	if err := publicsurface.Validate(schema); err == nil {
		t.Fatal("Validate() = nil, want error for public streaming method")
	} else if !strings.Contains(err.Error(), "StreamEvents") {
		t.Fatalf("Validate() = %v, want StreamEvents error", err)
	}

	// Drop the unsupported stream before building the view for structure checks.
	schema.Services[0].Methods = schema.Services[0].Methods[:3]

	view := publicsurface.Build(schema)
	if len(view.Services) != 2 {
		t.Fatalf("services = %d, want 2 (App and Identity)", len(view.Services))
	}
	if view.Services[0].Name != "App" && view.Services[1].Name != "App" {
		t.Fatalf("services = %+v, want App included", serviceNames(view))
	}
	if view.Services[0].Name != "Identity" && view.Services[1].Name != "Identity" {
		t.Fatalf("services = %+v, want Identity included", serviceNames(view))
	}

	methods, err := publicsurface.ParseMethods(schema, view, publicsurface.ProjectionGRPC)
	if err != nil {
		t.Fatalf("ParseMethods: %v", err)
	}
	if len(methods) != 4 {
		t.Fatalf("methods = %d, want 4 (3 App + 1 Identity)", len(methods))
	}

	var invoke, invokeGraphQL, grpcOnly *publicsurface.PublicMethod
	for i := range methods {
		switch methods[i].Method {
		case "Invoke":
			invoke = &methods[i]
		case "InvokeGraphQL":
			invokeGraphQL = &methods[i]
		case "GrpcOnly":
			grpcOnly = &methods[i]
		}
	}
	if invoke == nil || invokeGraphQL == nil || grpcOnly == nil {
		t.Fatalf("methods = %+v, want Invoke, InvokeGraphQL, GrpcOnly", methodNames(methods))
	}

	if got := publicsurface.GRPCMethodCount(view); got != 4 {
		t.Fatalf("gRPC methods = %d, want 4", got)
	}
	if got := publicsurface.RESTMethodCount(view); got != 3 {
		t.Fatalf("REST methods = %d, want 3", got)
	}

	if invoke.REST == nil || invokeGraphQL.REST == nil {
		t.Fatal("Invoke and InvokeGraphQL must have REST rules")
	}
	if grpcOnly.REST != nil {
		t.Fatal("GrpcOnly must be gRPC-only")
	}

	if len(invoke.REST.PathFields) != 2 || invoke.REST.PathFields[0].Name != "app" || invoke.REST.PathFields[1].Name != "operation" {
		t.Fatalf("Invoke path fields = %+v", invoke.REST.PathFields)
	}
	if invoke.REST.Body != publicsurface.BodyStar {
		t.Fatalf("Invoke body = %v, want BodyStar", invoke.REST.Body)
	}
	if len(invokeGraphQL.REST.PathFields) != 1 || invokeGraphQL.REST.PathFields[0].Name != "app" {
		t.Fatalf("InvokeGraphQL path fields = %+v", invokeGraphQL.REST.PathFields)
	}

	complexSchema := *schema
	complexSchema.Services = []*model.Service{{
		Name: "App",
		Methods: []*model.Method{{
			Name:       "BadPath",
			FullMethod: "/gestalt.provider.v1.App/BadPath",
			Public:     true,
			Stream:     model.Unary,
			Input:      invokeInput,
			HTTP:       &model.HTTPRule{Verb: "GET", Path: "/api/v2/projects/{name=projects/*}"},
		}},
	}}
	if _, err := publicsurface.ParseMethods(&complexSchema, publicsurface.Build(&complexSchema), publicsurface.ProjectionGRPC); err == nil {
		t.Fatal("ParseMethods: want complex path binding error")
	}

	unknownSchema := *schema
	unknownSchema.Services = []*model.Service{{
		Name: "App",
		Methods: []*model.Method{{
			Name:       "MissingField",
			FullMethod: "/gestalt.provider.v1.App/MissingField",
			Public:     true,
			Stream:     model.Unary,
			Input:      invokeInput,
			HTTP:       &model.HTTPRule{Verb: "GET", Path: "/api/v2/app/{missing}"},
		}},
	}}
	if _, err := publicsurface.ParseMethods(&unknownSchema, publicsurface.Build(&unknownSchema), publicsurface.ProjectionGRPC); err == nil {
		t.Fatal("ParseMethods: want unknown path field error")
	}

	projected, err := publicsurface.Project(schema, view)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	var invokeMsg *model.Message
	for _, svc := range projected.Services {
		if svc.Name != "App" {
			continue
		}
		for _, m := range svc.Methods {
			if m.Name == "Invoke" {
				invokeMsg = m.Input
				break
			}
		}
	}
	if invokeMsg == nil {
		t.Fatal("projected Invoke input not found")
	}
	for _, omitted := range []string{"context", "run_as"} {
		if fieldByName(invokeMsg, omitted) != nil {
			t.Fatalf("projected Invoke still has field %q", omitted)
		}
	}
}

func TestProjectRejectsConflictingInputPolicies(t *testing.T) {
	t.Parallel()

	input := &model.Message{FullName: "gestalt.v1.InvokeRequest", Name: "InvokeRequest"}
	schema := &model.Schema{
		Messages: []*model.Message{input},
		Services: []*model.Service{
			{
				Name: "App",
				Methods: []*model.Method{
					{
						Name:   "A",
						Public: true,
						Stream: model.Unary,
						Input:  input,
						PublicPolicy: &model.PublicPolicy{
							Fill: []string{"context"},
						},
					},
					{
						Name:   "B",
						Public: true,
						Stream: model.Unary,
						Input:  input,
						PublicPolicy: &model.PublicPolicy{
							Reject: []string{"tenant"},
						},
					},
				},
			},
		},
	}

	view := publicsurface.Build(schema)
	if _, err := publicsurface.Project(schema, view); err == nil {
		t.Fatal("Project() error = nil, want conflict")
	}
}

func TestProjectRejectsEmptyVsNonemptyInputPolicies(t *testing.T) {
	t.Parallel()

	input := &model.Message{FullName: "gestalt.v1.InvokeRequest", Name: "InvokeRequest"}
	schema := &model.Schema{
		Messages: []*model.Message{input},
		Services: []*model.Service{
			{
				Name: "App",
				Methods: []*model.Method{
					{
						Name:   "A",
						Public: true,
						Stream: model.Unary,
						Input:  input,
					},
					{
						Name:   "B",
						Public: true,
						Stream: model.Unary,
						Input:  input,
						PublicPolicy: &model.PublicPolicy{
							Reject: []string{"tenant"},
						},
					},
				},
			},
		},
	}

	view := publicsurface.Build(schema)
	if _, err := publicsurface.Project(schema, view); err == nil {
		t.Fatal("Project() error = nil, want conflict")
	}
}

func serviceNames(view *publicsurface.View) []string {
	out := make([]string, len(view.Services))
	for i, svc := range view.Services {
		out[i] = svc.Name
	}
	return out
}

func methodNames(methods []publicsurface.PublicMethod) []string {
	out := make([]string, len(methods))
	for i, m := range methods {
		out[i] = m.Method
	}
	return out
}

func fieldByName(m *model.Message, name string) *model.Field {
	for _, f := range m.Fields {
		if f.Name == name {
			return f
		}
	}
	return nil
}
