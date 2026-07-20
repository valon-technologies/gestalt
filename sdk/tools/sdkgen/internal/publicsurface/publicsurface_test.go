package publicsurface_test

import (
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/publicsurface"
)

func TestBuildAppOnlySurface(t *testing.T) {
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
						Input:      grpcOnlyInput,
					},
					{
						Name:       "BidiReject",
						FullMethod: "/gestalt.provider.v1.App/BidiReject",
						Public:     true,
						Stream:     model.Bidi,
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
		t.Fatal("Validate() = nil, want error for public bidi-streaming method")
	} else if !strings.Contains(err.Error(), "BidiReject") {
		t.Fatalf("Validate() = %v, want BidiReject error", err)
	}

	// Drop the unsupported bidi method before building the view for structure
	// checks. StreamEvents (server-streaming) stays and must be projected.
	schema.Services[0].Methods = schema.Services[0].Methods[:4]

	if err := publicsurface.Validate(schema); err != nil {
		t.Fatalf("Validate() after dropping bidi: %v", err)
	}

	view := publicsurface.Build(schema)
	if len(view.Services) != 2 {
		t.Fatalf("services = %d, want 2", len(view.Services))
	}
	if view.Services[0].Name != publicsurface.AppServiceName {
		t.Fatalf("first service = %q, want %q", view.Services[0].Name, publicsurface.AppServiceName)
	}

	methods, err := publicsurface.ParseMethods(schema, view)
	if err != nil {
		t.Fatalf("ParseMethods: %v", err)
	}
	if len(methods) != 5 {
		t.Fatalf("methods = %d, want 5", len(methods))
	}

	var invoke, invokeGraphQL, grpcOnly, streamEvents *publicsurface.PublicMethod
	for i := range methods {
		switch methods[i].Method {
		case "Invoke":
			invoke = &methods[i]
		case "InvokeGraphQL":
			invokeGraphQL = &methods[i]
		case "GrpcOnly":
			grpcOnly = &methods[i]
		case "StreamEvents":
			streamEvents = &methods[i]
		}
	}
	if invoke == nil || invokeGraphQL == nil || grpcOnly == nil || streamEvents == nil {
		t.Fatalf("methods = %+v, want Invoke, InvokeGraphQL, GrpcOnly, StreamEvents", methodNames(methods))
	}
	if streamEvents.Stream != model.ServerStream {
		t.Fatalf("StreamEvents.Stream = %v, want ServerStream", streamEvents.Stream)
	}

	if got := publicsurface.GRPCMethodCount(view); got != 5 {
		t.Fatalf("gRPC methods = %d, want 5", got)
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
	if _, err := publicsurface.ParseMethods(&complexSchema, publicsurface.Build(&complexSchema)); err == nil {
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
	if _, err := publicsurface.ParseMethods(&unknownSchema, publicsurface.Build(&unknownSchema)); err == nil {
		t.Fatal("ParseMethods: want unknown path field error")
	}

	projected, err := publicsurface.Project(schema, view)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	invokeMsg := projected.Services[0].Methods[0].Input
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

func TestFilterRESTIncludesServerStreamWithoutHTTPBinding(t *testing.T) {
	t.Parallel()

	view := &publicsurface.View{
		Services: []*publicsurface.Service{{
			Service: &model.Service{FullName: "gestalt.provider.v1.App"},
			PublicMethods: []*model.Method{
				{
					Name:       "Invoke",
					FullMethod: "/gestalt.provider.v1.App/Invoke",
					Stream:     model.Unary,
					HTTP:       &model.HTTPRule{Verb: "POST", Path: "/api/v2/app/{app}/operations/{operation}", Body: "*"},
					Public:     true,
					PublicPolicy: &model.PublicPolicy{Fill: []string{"context"}, Reject: []string{"run_as"}},
				},
				{
					Name:       "InvokeStream",
					FullMethod: "/gestalt.provider.v1.App/InvokeStream",
					Stream:     model.ServerStream,
					HTTP:       nil, // no HTTP binding — reachable via unified Invoke path
					Public:     true,
					PublicPolicy: &model.PublicPolicy{Fill: []string{"context"}, Reject: []string{"run_as"}},
				},
			},
		}},
	}

	filtered := publicsurface.FilterREST(view)
	if len(filtered.Services) != 1 {
		t.Fatalf("services = %d, want 1", len(filtered.Services))
	}
	methods := filtered.Services[0].PublicMethods
	if len(methods) != 2 {
		t.Fatalf("methods = %d, want 2 (Invoke + InvokeStream)", len(methods))
	}
	var sawInvoke, sawInvokeStream bool
	for _, m := range methods {
		if m.Name == "Invoke" {
			sawInvoke = true
		}
		if m.Name == "InvokeStream" {
			sawInvokeStream = true
			if m.Stream != model.ServerStream {
				t.Errorf("InvokeStream stream = %v, want ServerStream", m.Stream)
			}
		}
	}
	if !sawInvoke {
		t.Errorf("Invoke missing from filtered view")
	}
	if !sawInvokeStream {
		t.Errorf("InvokeStream missing from filtered view — should be included as a server-stream method on a service with HTTP bindings")
	}
}

func TestFilterRESTExcludesServerStreamWhenServiceHasNoHTTP(t *testing.T) {
	t.Parallel()

	view := &publicsurface.View{
		Services: []*publicsurface.Service{{
			Service: &model.Service{FullName: "gestalt.provider.v1.NoHTTP"},
			PublicMethods: []*model.Method{
				{
					Name:       "StreamOnly",
					FullMethod: "/gestalt.provider.v1.NoHTTP/StreamOnly",
					Stream:     model.ServerStream,
					HTTP:       nil,
					Public:     true,
				},
			},
		}},
	}

	filtered := publicsurface.FilterREST(view)
	if len(filtered.Services) != 0 {
		t.Fatalf("services = %d, want 0 (no HTTP-backed methods)", len(filtered.Services))
	}
}
