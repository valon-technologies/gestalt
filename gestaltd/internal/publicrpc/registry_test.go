package publicrpc_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/bufbuild/protocompile"
	"google.golang.org/genproto/googleapis/api/annotations"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	gestaltproto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

func TestRegistryDiscoversPublicMethods(t *testing.T) {
	t.Parallel()

	files, err := publicrpc.GeneratedFiles()
	if err != nil {
		t.Fatalf("GeneratedFiles: %v", err)
	}
	public, err := publicrpc.NewRegistry(files)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	var got []string
	files.RangeFiles(func(file protoreflect.FileDescriptor) bool {
		if file.Package() != "gestalt.provider.v1" {
			return true
		}
		services := file.Services()
		for i := 0; i < services.Len(); i++ {
			methods := services.Get(i).Methods()
			for j := 0; j < methods.Len(); j++ {
				method := methods.Get(j)
				options := method.Options()
				if !gproto.HasExtension(options, annotations.E_Http) {
					continue
				}
				fullMethod := "/" + string(method.Parent().FullName()) + "/" + string(method.Name())
				if _, ok := public.Lookup(fullMethod); !ok {
					t.Fatalf("REST-bound method %s is not public", fullMethod)
				}
				rule := gproto.GetExtension(options, annotations.E_Http).(*annotations.HttpRule)
				if len(rule.GetAdditionalBindings()) != 0 {
					t.Fatalf("REST-bound method %s has additional bindings", fullMethod)
				}
				verb, path := httpRule(t, rule)
				got = append(got, verb+" "+path+" "+fullMethod)
			}
		}
		return true
	})

	want := strings.Split(strings.TrimSpace(`
POST /api/v2/app/{app}/operations/{operation} /gestalt.provider.v1.App/Invoke
POST /api/v2/app/{app}/graphql /gestalt.provider.v1.App/InvokeGraphQL
POST /api/v2/agent/sessions /gestalt.provider.v1.Agent/CreateSession
GET /api/v2/agent/sessions /gestalt.provider.v1.Agent/ListSessions
GET /api/v2/agent/sessions/{session_id} /gestalt.provider.v1.Agent/GetSession
PATCH /api/v2/agent/sessions/{session_id} /gestalt.provider.v1.Agent/UpdateSession
POST /api/v2/agent/sessions/{session_id}/turns /gestalt.provider.v1.Agent/CreateTurn
GET /api/v2/agent/sessions/{session_id}/turns /gestalt.provider.v1.Agent/ListTurns
GET /api/v2/agent/sessions/{session_id}/turns/{turn_id} /gestalt.provider.v1.Agent/GetTurn
POST /api/v2/agent/sessions/{session_id}/turns/{turn_id}:cancel /gestalt.provider.v1.Agent/CancelTurn
POST /api/v2/agent/turns/{turn_id}/interactions/{interaction_id}/resolve /gestalt.provider.v1.Agent/ResolveInteraction
GET /api/v2/agent/sessions/{session_id}/turns/{turn_id}/events /gestalt.provider.v1.Agent/ListTurnEvents
GET /api/v2/agent/turns/{turn_id}/interactions /gestalt.provider.v1.Agent/ListInteractions
POST /api/v2/workflow/definitions:apply /gestalt.provider.v1.Workflow/ApplyDefinition
GET /api/v2/workflow/definitions /gestalt.provider.v1.Workflow/ListDefinitions
GET /api/v2/workflow/definitions/{definition_id} /gestalt.provider.v1.Workflow/GetDefinition
DELETE /api/v2/workflow/definitions/{definition_id} /gestalt.provider.v1.Workflow/DeleteDefinition
POST /api/v2/workflow/definitions/{definition_id}:setPaused /gestalt.provider.v1.Workflow/SetDefinitionPaused
POST /api/v2/workflow/definitions/{definition_id}/activations/{activation_id}:setPaused /gestalt.provider.v1.Workflow/SetActivationPaused
POST /api/v2/workflow/definitions/{definition_id}/runs /gestalt.provider.v1.Workflow/StartRun
POST /api/v2/workflow/definitions/{definition_id}:signalOrStart /gestalt.provider.v1.Workflow/SignalOrStartRun
GET /api/v2/workflow/runs /gestalt.provider.v1.Workflow/ListRuns
GET /api/v2/workflow/runs/{run_id} /gestalt.provider.v1.Workflow/GetRun
GET /api/v2/workflow/runs/{run_id}/events /gestalt.provider.v1.Workflow/GetRunEvents
GET /api/v2/workflow/runs/{run_id}/output /gestalt.provider.v1.Workflow/GetRunOutput
POST /api/v2/workflow/runs/{run_id}:cancel /gestalt.provider.v1.Workflow/CancelRun
POST /api/v2/workflow/runs/{run_id}:signal /gestalt.provider.v1.Workflow/SignalRun
POST /api/v2/authorization/access:check /gestalt.provider.v1.Authorization/CheckAccess
POST /api/v2/authorization/access:checkMany /gestalt.provider.v1.Authorization/CheckAccessMany
GET /api/v2/authorization/relationships /gestalt.provider.v1.Authorization/ListRelationships
POST /api/v2/authorization/relationships /gestalt.provider.v1.Authorization/AddRelationship
POST /api/v2/authorization/relationships:delete /gestalt.provider.v1.Authorization/DeleteRelationship
PUT /api/v2/authorization/state /gestalt.provider.v1.Authorization/SetAuthorizationState
GET /api/v2/authorization/models/active /gestalt.provider.v1.Authorization/GetActiveModelRef
PUT /api/v2/authorization/models/active /gestalt.provider.v1.Authorization/SetActiveModel
GET /api/v2/authorization/models/active/resource-types /gestalt.provider.v1.Authorization/ListActiveModelResourceTypes
POST /api/v2/identity/authorize /gestalt.provider.v1.Identity/Authorize
POST /api/v2/identity/token /gestalt.provider.v1.Identity/Token
POST /api/v2/identity/introspect /gestalt.provider.v1.Identity/Introspect
GET /api/v2/identity/userinfo /gestalt.provider.v1.Identity/UserInfo
GET /api/v2/identity/grants /gestalt.provider.v1.Identity/ListGrants
GET /api/v2/identity/grants/{grant_id} /gestalt.provider.v1.Identity/GetGrant
DELETE /api/v2/identity/grants/{grant_id} /gestalt.provider.v1.Identity/RevokeGrant
POST /api/v2/remotes /gestalt.provider.v1.RemoteManagement/CreateRemote
GET /api/v2/remotes /gestalt.provider.v1.RemoteManagement/ListRemotes
DELETE /api/v2/remotes/{id} /gestalt.provider.v1.RemoteManagement/DeleteRemote
POST /api/v2/remotes:sessions /gestalt.provider.v1.RemoteManagement/PrepareRemoteSession
POST /api/v2/remotes/{registration_id}:activate /gestalt.provider.v1.RemoteManagement/ActivateRemoteSession
POST /api/v2/remotes/{registration_id}:heartbeat /gestalt.provider.v1.RemoteManagement/HeartbeatRemoteSession
`), "\n")
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("REST contract:\n got: %s\nwant: %s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func httpRule(t *testing.T, rule *annotations.HttpRule) (string, string) {
	t.Helper()
	for _, candidate := range []struct {
		verb string
		path string
	}{
		{"GET", rule.GetGet()},
		{"PUT", rule.GetPut()},
		{"POST", rule.GetPost()},
		{"DELETE", rule.GetDelete()},
		{"PATCH", rule.GetPatch()},
	} {
		if candidate.path != "" {
			return candidate.verb, candidate.path
		}
	}
	t.Fatalf("HTTP rule has no supported pattern: %v", rule)
	return "", ""
}

func TestRegistryDoesNotExposeInternalMethods(t *testing.T) {
	t.Parallel()

	registry, err := publicrpc.NewGeneratedRegistry()
	if err != nil {
		t.Fatalf("NewGeneratedRegistry: %v", err)
	}

	internal := []string{
		gestaltproto.Agent_GetInteraction_FullMethodName,
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
