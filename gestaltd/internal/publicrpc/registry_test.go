package publicrpc_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/bufbuild/protocompile"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	gestaltproto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

func TestRegistryDiscoversPublicMethods(t *testing.T) {
	t.Parallel()

	registry, err := publicrpc.NewGeneratedRegistry()
	if err != nil {
		t.Fatalf("NewGeneratedRegistry: %v", err)
	}

	want := []string{
		gestaltproto.App_Invoke_FullMethodName,
		gestaltproto.App_InvokeGraphQL_FullMethodName,
		gestaltproto.Workflow_GetDefinition_FullMethodName,
		gestaltproto.Agent_CreateSession_FullMethodName,
		gestaltproto.Agent_GetSession_FullMethodName,
		gestaltproto.Agent_ListSessions_FullMethodName,
		gestaltproto.Agent_UpdateSession_FullMethodName,
		gestaltproto.Agent_CreateTurn_FullMethodName,
		gestaltproto.Agent_GetTurn_FullMethodName,
		gestaltproto.Agent_ListTurns_FullMethodName,
		gestaltproto.Agent_CancelTurn_FullMethodName,
		gestaltproto.Agent_ListTurnEvents_FullMethodName,
	}
	for _, fullMethod := range want {
		if _, ok := registry.Lookup(fullMethod); !ok {
			t.Fatalf("Lookup(%q) = false, want true", fullMethod)
		}
	}
}

func TestRegistryDoesNotExposeInternalMethods(t *testing.T) {
	t.Parallel()

	registry, err := publicrpc.NewGeneratedRegistry()
	if err != nil {
		t.Fatalf("NewGeneratedRegistry: %v", err)
	}

	internal := []string{
		gestaltproto.Workflow_DeliverEvent_FullMethodName,
		gestaltproto.Agent_GetInteraction_FullMethodName,
		gestaltproto.Agent_ListInteractions_FullMethodName,
		gestaltproto.Agent_ResolveInteraction_FullMethodName,
		gestaltproto.Agent_GetCapabilities_FullMethodName,
		gestaltproto.AppProvider_GetMetadata_FullMethodName,
	}
	for _, fullMethod := range internal {
		if _, ok := registry.Lookup(fullMethod); ok {
			t.Fatalf("Lookup(%q) = true, want false", fullMethod)
		}
	}
}

func TestRegistryReadsFillRejectPolicy(t *testing.T) {
	t.Parallel()

	registry, err := publicrpc.NewGeneratedRegistry()
	if err != nil {
		t.Fatalf("NewGeneratedRegistry: %v", err)
	}

	policy, ok := registry.Lookup(gestaltproto.App_Invoke_FullMethodName)
	if !ok {
		t.Fatalf("Lookup(%q) = false, want true", gestaltproto.App_Invoke_FullMethodName)
	}
	if got, want := policy.Fill, []string{"context"}; !slices.Equal(got, want) {
		t.Fatalf("Fill = %#v, want %#v", got, want)
	}
	if got, want := policy.Reject, []string{"run_as"}; !slices.Equal(got, want) {
		t.Fatalf("Reject = %#v, want %#v", got, want)
	}

	graphql, ok := registry.Lookup(gestaltproto.App_InvokeGraphQL_FullMethodName)
	if !ok {
		t.Fatalf("Lookup(%q) = false, want true", gestaltproto.App_InvokeGraphQL_FullMethodName)
	}
	if got, want := graphql.Fill, []string{"context"}; !slices.Equal(got, want) {
		t.Fatalf("InvokeGraphQL Fill = %#v, want %#v", got, want)
	}
	if len(graphql.Reject) != 0 {
		t.Fatalf("InvokeGraphQL Reject = %#v, want empty", graphql.Reject)
	}
}

func TestRegistryRejectsUnknownPolicyFields(t *testing.T) {
	t.Parallel()

	files, err := compileFixture(t, "invalid_policy_field.proto")
	if err != nil {
		t.Fatalf("compileFixture: %v", err)
	}
	registry, err := publicrpc.RegisterFiles(files)
	if err != nil {
		t.Fatalf("RegisterFiles: %v", err)
	}
	if _, err := publicrpc.NewRegistry(registry); err == nil {
		t.Fatal("NewRegistry = nil, want validation error for unknown field")
	} else if !strings.Contains(err.Error(), "unknown request field") {
		t.Fatalf("NewRegistry error = %v, want unknown request field", err)
	}
}

func TestRegistryDoesNotTreatPublicPolicyAsExposure(t *testing.T) {
	t.Parallel()

	files, err := compileFixture(t, "public_policy_without_visibility.proto")
	if err != nil {
		t.Fatalf("compileFixture: %v", err)
	}
	registry, err := publicrpc.RegisterFiles(files)
	if err != nil {
		t.Fatalf("RegisterFiles: %v", err)
	}
	if _, err := publicrpc.NewRegistry(registry); err == nil {
		t.Fatal("NewRegistry = nil, want validation error")
	} else if !strings.Contains(err.Error(), "not marked PUBLIC") {
		t.Fatalf("NewRegistry error = %v, want not marked PUBLIC", err)
	}
}

func TestRegistryPublicMethodWithoutPolicyIsNoOp(t *testing.T) {
	t.Parallel()

	files, err := compileFixture(t, "public_without_policy.proto")
	if err != nil {
		t.Fatalf("compileFixture: %v", err)
	}
	registry, err := publicrpc.RegisterFiles(files)
	if err != nil {
		t.Fatalf("RegisterFiles: %v", err)
	}
	publicRegistry, err := publicrpc.NewRegistry(registry)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	policy, ok := publicRegistry.Lookup("/example.v1.Example/PublicNoPolicy")
	if !ok {
		t.Fatal("Lookup = false, want true")
	}
	if len(policy.Fill) != 0 || len(policy.Reject) != 0 {
		t.Fatalf("policy = %#v, want empty fill/reject", policy)
	}
}

func compileFixture(t *testing.T, filename string) (protoreflect.FileDescriptor, error) {
	t.Helper()
	compiler := protocompile.Compiler{
		Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{
			ImportPaths: []string{"testdata"},
		}),
		SourceInfoMode: protocompile.SourceInfoStandard,
	}
	files, err := compiler.Compile(context.Background(), filename)
	if err != nil {
		return nil, err
	}
	return files[0], nil
}
