package providergateway

import (
	"context"
	"strings"
	"testing"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeAuthorizationServer is a minimal AuthorizationServer for registry
// round-trip tests. It embeds UnimplementedAuthorizationServer by value (the
// generated handler's forward-compatibility contract) and overrides CheckAccess.
type fakeAuthorizationServer struct {
	proto.UnimplementedAuthorizationServer
	allowed bool
}

func (s *fakeAuthorizationServer) CheckAccess(_ context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	return &proto.CheckAccessResponse{Allowed: s.allowed, ModelId: req.GetResource().GetId()}, nil
}

// TestRegisterKindResolvesThroughGatewayTypedClient is the acceptance test:
// registering a kind makes it resolvable through PG-1's gateway via the
// generated typed client over gateway.Conn, with no Marshal/Unmarshal round-trip.
func TestRegisterKindResolvesThroughGatewayTypedClient(t *testing.T) {
	t.Parallel()

	server := &fakeAuthorizationServer{allowed: true}
	kinds, err := DefaultKindRegistry(server)
	if err != nil {
		t.Fatalf("DefaultKindRegistry: %v", err)
	}

	local := NewLocalRegistry()
	target := ProviderTarget{Kind: ProviderKindAuthorization, Name: "authz-primary"}
	if err := local.RegisterDirect(target, DirectEndpoint{
		Desc:   &proto.Authorization_ServiceDesc,
		Server: server,
	}); err != nil {
		t.Fatalf("RegisterDirect: %v", err)
	}
	gateway := NewRoutingGateway(local, nil, WithKindRegistry(kinds))

	client := proto.NewAuthorizationClient(gateway.Conn(target))
	resp, err := client.CheckAccess(context.Background(), &proto.CheckAccessRequest{
		Subject:  &proto.Subject{Type: "subject", Id: "user:alice"},
		Action:   &proto.Action{Name: "CheckAccess"},
		Resource: &proto.Resource{Type: "provider", Id: "authz-primary"},
	})
	if err != nil {
		t.Fatalf("CheckAccess: %v", err)
	}
	if !resp.GetAllowed() {
		t.Fatal("Allowed = false, want true")
	}
	if resp.GetModelId() != "authz-primary" {
		t.Fatalf("ModelId = %q, want %q", resp.GetModelId(), "authz-primary")
	}
}

func TestLookupCoversExactlyRegisteredMethods(t *testing.T) {
	t.Parallel()

	kinds, err := DefaultKindRegistry(&fakeAuthorizationServer{})
	if err != nil {
		t.Fatalf("DefaultKindRegistry: %v", err)
	}

	// Every method of every registered kind's ServiceDesc must resolve.
	wantMethods := map[ProviderKind]grpc.ServiceDesc{
		ProviderKindAuthorization: proto.Authorization_ServiceDesc,
		ProviderKindApp:           proto.AppProvider_ServiceDesc,
		ProviderKindWorkflow:      proto.Workflow_ServiceDesc,
		ProviderKindAgent:         proto.Agent_ServiceDesc,
	}
	for kind, desc := range wantMethods {
		for _, method := range desc.Methods {
			fullMethod := "/" + desc.ServiceName + "/" + method.MethodName
			km, ok := kinds.Lookup(fullMethod)
			if !ok {
				t.Errorf("Lookup(%q) = false, want true", fullMethod)
				continue
			}
			if km.Kind != kind {
				t.Errorf("Lookup(%q) kind = %q, want %q", fullMethod, km.Kind, kind)
			}
			if km.Method.MethodName != method.MethodName {
				t.Errorf("Lookup(%q) method = %q, want %q", fullMethod, km.Method.MethodName, method.MethodName)
			}
		}
	}

	// Methods that belong to no registered kind must not resolve.
	for _, unknown := range []string{
		"/gestalt.provider.v1.Authorization/UnknownMethod",
		"/gestalt.provider.v1.Cache/Get",
		"/gestalt.provider.v1.IndexedDB/Get",
		"/gestalt.provider.v1.Secrets/Get",
		"/gestalt.core.v1.Foo/Bar",
		"/unknown.Service/Method",
		"not-a-full-method",
	} {
		if _, ok := kinds.Lookup(unknown); ok {
			t.Errorf("Lookup(%q) = true, want false", unknown)
		}
	}

	// Kinds() reports exactly the registered kinds, sorted.
	gotKinds := kinds.Kinds()
	wantKinds := []ProviderKind{ProviderKindAgent, ProviderKindApp, ProviderKindAuthorization, ProviderKindWorkflow}
	if len(gotKinds) != len(wantKinds) {
		t.Fatalf("Kinds() = %v, want %v", gotKinds, wantKinds)
	}
	for i, k := range wantKinds {
		if gotKinds[i] != k {
			t.Fatalf("Kinds()[%d] = %q, want %q", i, gotKinds[i], k)
		}
	}
}

func TestRegisterKindRefusesCoreAdministrationRemoteManagement(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		serviceName string
		wantSubstr  string
	}{
		{"core", "gestalt.core.v1.Health", "core service"},
		{"administration", "gestalt.admin.v1.Admin", "administration service"},
		{"remote_management", "gestalt.provider.v1.RemoteManagement", "remote management service"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reg := NewKindRegistry()
			err := reg.RegisterKind(ProviderKind("test"), grpc.ServiceDesc{ServiceName: tc.serviceName}, nil)
			if err == nil {
				t.Fatalf("RegisterKind(%q) err = nil, want error", tc.serviceName)
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Fatalf("RegisterKind(%q) err = %q, want substring %q", tc.serviceName, err.Error(), tc.wantSubstr)
			}
		})
	}
}

func TestRegisterKindRejectsDuplicateKind(t *testing.T) {
	t.Parallel()

	reg := NewKindRegistry()
	if err := reg.RegisterKind(ProviderKindAuthorization, proto.Authorization_ServiceDesc, nil); err != nil {
		t.Fatalf("first RegisterKind: %v", err)
	}
	err := reg.RegisterKind(ProviderKindAuthorization, proto.Authorization_ServiceDesc, nil)
	if err == nil {
		t.Fatal("duplicate RegisterKind err = nil, want error")
	}
	if !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("duplicate RegisterKind err = %q, want substring %q", err.Error(), "already registered")
	}
}

func TestRegisterKindRejectsOverlappingFullMethod(t *testing.T) {
	t.Parallel()

	reg := NewKindRegistry()
	if err := reg.RegisterKind(ProviderKindAuthorization, proto.Authorization_ServiceDesc, nil); err != nil {
		t.Fatalf("first RegisterKind: %v", err)
	}
	// Re-registering the same ServiceDesc under a different kind collides on
	// every Authorization full method name.
	err := reg.RegisterKind(ProviderKind("dupe"), proto.Authorization_ServiceDesc, nil)
	if err == nil {
		t.Fatal("overlapping RegisterKind err = nil, want error")
	}
	if !strings.Contains(err.Error(), "already registered by kind") {
		t.Fatalf("overlapping RegisterKind err = %q, want substring %q", err.Error(), "already registered by kind")
	}
}

func TestRegisterKindRejectsEmptyKindAndServiceName(t *testing.T) {
	t.Parallel()

	reg := NewKindRegistry()
	if err := reg.RegisterKind("", proto.Authorization_ServiceDesc, nil); err == nil {
		t.Fatal("empty kind RegisterKind err = nil, want error")
	}
	if err := reg.RegisterKind(ProviderKind("test"), grpc.ServiceDesc{}, nil); err == nil {
		t.Fatal("empty service name RegisterKind err = nil, want error")
	}
}

func TestAssertExhaustiveFailsOnUnregisteredResolvableKind(t *testing.T) {
	t.Parallel()

	reg := NewKindRegistry()
	if err := reg.RegisterKind(ProviderKindAuthorization, proto.Authorization_ServiceDesc, nil); err != nil {
		t.Fatalf("RegisterKind: %v", err)
	}
	// The resolver can return authorization and a dummy kind that was never
	// registered; the assertion must name the missing one and fail.
	err := reg.AssertExhaustive([]ProviderKind{ProviderKindAuthorization, ProviderKind("dummy")})
	if err == nil {
		t.Fatal("AssertExhaustive err = nil, want error")
	}
	if !strings.Contains(err.Error(), "dummy") {
		t.Fatalf("AssertExhaustive err = %q, want substring %q", err.Error(), "dummy")
	}
	if !strings.Contains(err.Error(), "unregistered routable kinds") {
		t.Fatalf("AssertExhaustive err = %q, want substring %q", err.Error(), "unregistered routable kinds")
	}
}

func TestAssertExhaustivePassesWhenAllResolvableKindsRegistered(t *testing.T) {
	t.Parallel()

	reg, err := DefaultKindRegistry(&fakeAuthorizationServer{})
	if err != nil {
		t.Fatalf("DefaultKindRegistry: %v", err)
	}
	if err := reg.AssertExhaustive([]ProviderKind{
		ProviderKindAuthorization, ProviderKindApp, ProviderKindWorkflow, ProviderKindAgent,
	}); err != nil {
		t.Fatalf("AssertExhaustive: %v", err)
	}
}

// TestRegistryAwareDispatchRejectsUnregisteredMethod asserts that with a
// KindRegistry attached, a full method the registry does not map to the
// target's kind returns Unimplemented rather than dispatching through the
// per-target ServiceDesc. This is the safe-by-construction property the PG-7
// allowlist relies on.
func TestRegistryAwareDispatchRejectsUnregisteredMethod(t *testing.T) {
	t.Parallel()

	kinds, err := DefaultKindRegistry(&fakeAuthorizationServer{})
	if err != nil {
		t.Fatalf("DefaultKindRegistry: %v", err)
	}
	local := NewLocalRegistry()
	target := ProviderTarget{Kind: ProviderKindAuthorization, Name: "authz-primary"}
	if err := local.RegisterDirect(target, DirectEndpoint{
		Desc:   &proto.Authorization_ServiceDesc,
		Server: &fakeAuthorizationServer{allowed: true},
	}); err != nil {
		t.Fatalf("RegisterDirect: %v", err)
	}
	gateway := NewRoutingGateway(local, nil, WithKindRegistry(kinds))

	// An App method is registered in the registry but belongs to a different
	// kind than the authorization target; dispatch must refuse it.
	appMethod := "/" + proto.AppProvider_ServiceDesc.ServiceName + "/Invoke"
	err = gateway.Conn(target).Invoke(context.Background(), appMethod, &proto.CheckAccessRequest{}, &proto.CheckAccessResponse{})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("status.Code(err) = %v, want Unimplemented (%v)", status.Code(err), err)
	}
}

// TestRegistryAwareDispatchSkipsAuthorizationForAuthorizationTarget mirrors
// PG-1's authorization-target exemption: the gateway must not call its own
// authorization provider for the authorization kind, even with a registry
// attached.
func TestRegistryAwareDispatchSkipsAuthorizationForAuthorizationTarget(t *testing.T) {
	t.Parallel()

	server := &fakeAuthorizationServer{allowed: true}
	kinds, err := DefaultKindRegistry(server)
	if err != nil {
		t.Fatalf("DefaultKindRegistry: %v", err)
	}
	local := NewLocalRegistry()
	target := ProviderTarget{Kind: ProviderKindAuthorization, Name: "authz-primary"}
	if err := local.RegisterDirect(target, DirectEndpoint{
		Desc:   &proto.Authorization_ServiceDesc,
		Server: server,
	}); err != nil {
		t.Fatalf("RegisterDirect: %v", err)
	}
	// A denying authorization provider: if the gateway consulted it for the
	// authorization target, the call would fail with PermissionDenied.
	authz := &stubAuthorizationProvider{allowedResult: boolPtr(false)}
	gateway := NewRoutingGateway(local, authz, WithKindRegistry(kinds))
	ctx := principal.WithPrincipal(context.Background(), &principal.Principal{SubjectID: "user:alice"})

	client := proto.NewAuthorizationClient(gateway.Conn(target))
	resp, err := client.CheckAccess(ctx, &proto.CheckAccessRequest{
		Resource: &proto.Resource{Type: "provider", Id: "authz-primary"},
	})
	if err != nil {
		t.Fatalf("CheckAccess: %v", err)
	}
	if !resp.GetAllowed() {
		t.Fatal("Allowed = false, want true (authorization target should skip gateway authz)")
	}
	if authz.called {
		t.Fatal("authorization provider called for authorization target")
	}
}

// TestRegistryNilSafe asserts Lookup and Kinds on a nil registry return zero
// values rather than panicking, so the fallback path in dispatchUnary is safe.
func TestRegistryNilSafe(t *testing.T) {
	t.Parallel()

	var reg *KindRegistry
	if _, ok := reg.Lookup("/anything/Method"); ok {
		t.Fatal("nil Lookup = true, want false")
	}
	if kinds := reg.Kinds(); kinds != nil {
		t.Fatalf("nil Kinds() = %v, want nil", kinds)
	}
	if err := reg.AssertExhaustive(nil); err == nil {
		t.Fatal("nil AssertExhaustive err = nil, want error")
	}
}
