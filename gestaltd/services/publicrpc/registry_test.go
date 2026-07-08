package publicrpc_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/valon-technologies/gestalt/server/services/publicrpc"
)

func testRegistry(t *testing.T) *publicrpc.Registry {
	t.Helper()
	files, err := publicrpc.ProviderFiles()
	require.NoError(t, err)
	registry, err := publicrpc.NewRegistry(files)
	require.NoError(t, err)
	return registry
}

func TestRegistryDiscoversPublicMethods(t *testing.T) {
	registry := testRegistry(t)

	want := []string{
		"/gestalt.provider.v1.App/Invoke",
		"/gestalt.provider.v1.App/InvokeGraphQL",
		"/gestalt.provider.v1.Agent/CreateSession",
		"/gestalt.provider.v1.Agent/GetSession",
		"/gestalt.provider.v1.Agent/ListSessions",
		"/gestalt.provider.v1.Agent/UpdateSession",
		"/gestalt.provider.v1.Agent/CreateTurn",
		"/gestalt.provider.v1.Agent/GetTurn",
		"/gestalt.provider.v1.Agent/ListTurns",
		"/gestalt.provider.v1.Agent/CancelTurn",
		"/gestalt.provider.v1.Agent/ListTurnEvents",
		"/gestalt.provider.v1.Workflow/DeliverEvent",
	}

	for _, fullMethod := range want {
		policy, ok := registry.Lookup(fullMethod)
		require.True(t, ok, "expected public method %s", fullMethod)
		require.Equal(t, fullMethod, policy.FullMethod)
		require.NotEmpty(t, policy.Service)
		require.NotEmpty(t, policy.Method)
	}
}

func TestRegistryDoesNotExposeInternalMethods(t *testing.T) {
	registry := testRegistry(t)

	internal := []string{
		"/gestalt.provider.v1.Workflow/ApplyDefinition",
		"/gestalt.provider.v1.Agent/GetInteraction",
		"/gestalt.provider.v1.Agent/ListInteractions",
		"/gestalt.provider.v1.Agent/ResolveInteraction",
		"/gestalt.provider.v1.Agent/GetCapabilities",
		"/gestalt.provider.v1.AppProvider/Execute",
	}

	for _, fullMethod := range internal {
		_, ok := registry.Lookup(fullMethod)
		require.False(t, ok, "expected internal method %s", fullMethod)
	}
}

func TestRegistryReadsFillRejectPolicy(t *testing.T) {
	registry := testRegistry(t)

	invoke, ok := registry.Lookup("/gestalt.provider.v1.App/Invoke")
	require.True(t, ok)
	require.Equal(t, []string{"context"}, invoke.Fill)
	require.Equal(t, []string{"run_as"}, invoke.Reject)

	invokeGraphQL, ok := registry.Lookup("/gestalt.provider.v1.App/InvokeGraphQL")
	require.True(t, ok)
	require.Equal(t, []string{"context"}, invokeGraphQL.Fill)
	require.Empty(t, invokeGraphQL.Reject)

	createSession, ok := registry.Lookup("/gestalt.provider.v1.Agent/CreateSession")
	require.True(t, ok)
	require.Equal(t, []string{"context", "subject", "created_by_subject_id"}, createSession.Fill)
	require.Empty(t, createSession.Reject)

	deliverEvent, ok := registry.Lookup("/gestalt.provider.v1.Workflow/DeliverEvent")
	require.True(t, ok)
	require.Equal(t, []string{"context", "delivered_by_subject_id"}, deliverEvent.Fill)
	require.Empty(t, deliverEvent.Reject)
}

func TestRegistryAcceptsCurrentProviderDescriptors(t *testing.T) {
	files, err := publicrpc.ProviderFiles()
	require.NoError(t, err)

	_, err = publicrpc.NewRegistry(files)
	require.NoError(t, err)
}

func TestRegistryDoesNotTreatPublicPolicyAsExposure(t *testing.T) {
	registry := testRegistry(t)

	_, ok := registry.Lookup("/gestalt.provider.v1.Agent/GetCapabilities")
	require.False(t, ok, "GetCapabilities must stay internal even if future edits add option (public)")
}

func TestRegistryLookupNilRegistry(t *testing.T) {
	var registry *publicrpc.Registry
	_, ok := registry.Lookup("/gestalt.provider.v1.App/Invoke")
	require.False(t, ok)
}

func TestNewRegistryRequiresFiles(t *testing.T) {
	_, err := publicrpc.NewRegistry(nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "files registry is required")
}

func TestRegistryPublicMethodCount(t *testing.T) {
	registry := testRegistry(t)

	files, err := publicrpc.ProviderFiles()
	require.NoError(t, err)

	publicCount := 0
	files.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if string(fd.Package()) != "gestalt.provider.v1" {
			return true
		}
		services := fd.Services()
		for i := 0; i < services.Len(); i++ {
			methods := services.Get(i).Methods()
			for j := 0; j < methods.Len(); j++ {
				fullMethod := "/" + string(methods.Get(j).Parent().FullName()) + "/" + string(methods.Get(j).Name())
				if _, ok := registry.Lookup(fullMethod); ok {
					publicCount++
				}
			}
		}
		return true
	})

	require.Equal(t, 12, publicCount)
}

func TestRegistryServiceAndMethodNames(t *testing.T) {
	registry := testRegistry(t)

	policy, ok := registry.Lookup("/gestalt.provider.v1.App/Invoke")
	require.True(t, ok)
	require.Equal(t, "gestalt.provider.v1.App", policy.Service)
	require.Equal(t, "Invoke", policy.Method)
	require.False(t, strings.Contains(policy.FullMethod, "gestalt.provider.v1.App/Invoke") && !strings.HasPrefix(policy.FullMethod, "/"))
}
