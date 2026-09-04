package remotemanagement_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	coredata "github.com/valon-technologies/gestalt/server/internal/coredata"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/remotemanagement"
)

const testLease = 30 * time.Second

func TestNewRejectsNonPositiveLeaseDuration(t *testing.T) {
	t.Parallel()
	services, err := coredata.New(&coretesting.StubIndexedDB{})
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	for _, d := range []time.Duration{0, -time.Second} {
		if _, err := remotemanagement.New(
			services.RemoteRegistrations, nil, nil, &fakeValidator{}, remotemanagement.Config{LeaseDuration: d},
		); err == nil {
			t.Fatalf("New(LeaseDuration=%v) succeeded, want error", d)
		}
	}
}

func newRemoteService(t *testing.T, authz core.AuthorizationProvider) *remotemanagement.Service {
	t.Helper()
	services, err := coredata.New(&coretesting.StubIndexedDB{})
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	start := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	services.RemoteRegistrations.SetClock(func() time.Time { return start })
	validate := &fakeValidator{}
	svc, err := remotemanagement.New(
		services.RemoteRegistrations, authz, services.Users, validate, remotemanagement.Config{LeaseDuration: testLease},
	)
	if err != nil {
		t.Fatalf("remotemanagement.New: %v", err)
	}
	return svc
}

func validTunnel() *proto.TunnelEndpoint {
	return &proto.TunnelEndpoint{
		Host:             "tunnel.example.test",
		Certificate:      []byte("cert-bytes"),
		ServerSpkiSha256: "spki-sha256",
	}
}

func appProvider(kind, name string) *proto.RemoteProviderDefinition {
	def, _ := structpb.NewStruct(map[string]any{"displayName": kind + "/" + name})
	return &proto.RemoteProviderDefinition{Kind: kind, Name: name, Definition: def}
}

func withPrincipal(ctx context.Context, subjectID string) context.Context {
	return principal.WithPrincipal(ctx, &principal.Principal{SubjectID: subjectID})
}

func codeOf(t *testing.T, err error) codes.Code {
	t.Helper()
	if err == nil {
		return codes.OK
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("err is not a grpc status: %v", err)
	}
	return st.Code()
}

type fakeValidator struct{ fail bool }

func (f *fakeValidator) Validate(ctx context.Context, tunnel *proto.TunnelEndpoint, providers []*proto.RemoteProviderDefinition) error {
	if f.fail {
		return errors.New("tunnel unreachable")
	}
	return nil
}

func TestCreateRemoteGenerationMatrix(t *testing.T) {
	t.Parallel()
	ctx := withPrincipal(context.Background(), "user:alice-uuid")

	t.Run("create_at_generation_zero", func(t *testing.T) {
		t.Parallel()
		svc := newRemoteService(t, nil)
		remote, err := svc.CreateRemote(ctx, &proto.CreateRemoteRequest{
			Tunnel:             validTunnel(),
			Providers:          []*proto.RemoteProviderDefinition{appProvider("app", "test-app")},
			ExpectedGeneration: 0,
		})
		if err != nil {
			t.Fatalf("CreateRemote: %v", err)
		}
		if remote.Generation != 1 {
			t.Fatalf("generation = %d, want 1", remote.Generation)
		}
		if remote.OwnerSubjectId != "user:alice-uuid" {
			t.Fatalf("owner = %q, want user:alice-uuid", remote.OwnerSubjectId)
		}
	})

	t.Run("replace_at_current_generation_increments", func(t *testing.T) {
		t.Parallel()
		svc := newRemoteService(t, nil)
		_, err := svc.CreateRemote(ctx, &proto.CreateRemoteRequest{
			Tunnel:             validTunnel(),
			Providers:          []*proto.RemoteProviderDefinition{appProvider("app", "test-app")},
			ExpectedGeneration: 0,
		})
		if err != nil {
			t.Fatalf("first CreateRemote: %v", err)
		}
		remote, err := svc.CreateRemote(ctx, &proto.CreateRemoteRequest{
			Tunnel:             validTunnel(),
			Providers:          []*proto.RemoteProviderDefinition{appProvider("app", "test-app"), appProvider("workflow", "billing")},
			ExpectedGeneration: 1,
		})
		if err != nil {
			t.Fatalf("replace CreateRemote: %v", err)
		}
		if remote.Generation != 2 {
			t.Fatalf("generation = %d, want 2", remote.Generation)
		}
		if len(remote.Providers) != 2 {
			t.Fatalf("providers = %d, want 2", len(remote.Providers))
		}
	})

	t.Run("stale_generation_aborts", func(t *testing.T) {
		t.Parallel()
		svc := newRemoteService(t, nil)
		_, err := svc.CreateRemote(ctx, &proto.CreateRemoteRequest{
			Tunnel:             validTunnel(),
			Providers:          []*proto.RemoteProviderDefinition{appProvider("app", "test-app")},
			ExpectedGeneration: 0,
		})
		if err != nil {
			t.Fatalf("first CreateRemote: %v", err)
		}
		_, err = svc.CreateRemote(ctx, &proto.CreateRemoteRequest{
			Tunnel:             validTunnel(),
			Providers:          []*proto.RemoteProviderDefinition{appProvider("app", "test-app")},
			ExpectedGeneration: 0,
		})
		if got := codeOf(t, err); got != codes.Aborted {
			t.Fatalf("stale generation code = %v, want %v", got, codes.Aborted)
		}
	})
}

func TestCreateRemoteCrossOwnerConflict(t *testing.T) {
	t.Parallel()
	svc := newRemoteService(t, nil)
	alice := withPrincipal(context.Background(), "user:alice-uuid")
	bob := withPrincipal(context.Background(), "user:bob-uuid")

	if _, err := svc.CreateRemote(alice, &proto.CreateRemoteRequest{
		Tunnel: validTunnel(), Providers: []*proto.RemoteProviderDefinition{appProvider("app", "test-app")}, ExpectedGeneration: 0,
	}); err != nil {
		t.Fatalf("alice CreateRemote: %v", err)
	}
	_, err := svc.CreateRemote(bob, &proto.CreateRemoteRequest{
		Tunnel: validTunnel(), Providers: []*proto.RemoteProviderDefinition{appProvider("app", "test-app")}, ExpectedGeneration: 0,
	})
	if got := codeOf(t, err); got != codes.AlreadyExists {
		t.Fatalf("cross-owner conflict code = %v, want %v", got, codes.AlreadyExists)
	}
}

func TestCreateRemoteRejectsForbiddenKinds(t *testing.T) {
	t.Parallel()
	ctx := withPrincipal(context.Background(), "user:alice-uuid")
	svc := newRemoteService(t, nil)

	for _, kind := range []string{"identity", "authorization"} {
		_, err := svc.CreateRemote(ctx, &proto.CreateRemoteRequest{
			Tunnel: validTunnel(), Providers: []*proto.RemoteProviderDefinition{appProvider(kind, "local")}, ExpectedGeneration: 0,
		})
		if got := codeOf(t, err); got != codes.InvalidArgument {
			t.Fatalf("forbidden kind %q code = %v, want %v", kind, got, codes.InvalidArgument)
		}
	}
}

func TestDeleteRemoteGenerationRequiredAndNotFound(t *testing.T) {
	t.Parallel()
	ctx := withPrincipal(context.Background(), "user:alice-uuid")
	svc := newRemoteService(t, nil)

	if _, err := svc.DeleteRemote(ctx, &proto.DeleteRemoteRequest{Id: "reg-1", ExpectedGeneration: 0}); codeOf(t, err) != codes.InvalidArgument {
		t.Fatalf("delete generation 0 code = %v, want InvalidArgument", codeOf(t, err))
	}
	if _, err := svc.DeleteRemote(ctx, &proto.DeleteRemoteRequest{Id: "reg-1", ExpectedGeneration: 1}); codeOf(t, err) != codes.NotFound {
		t.Fatalf("delete unknown code = %v, want NotFound", codeOf(t, err))
	}
}

func TestDeleteRemoteRejectsCrossOwnerDeletion(t *testing.T) {
	t.Parallel()
	svc := newRemoteService(t, &stubAuthz{
		allowedResourceTypes: map[string]bool{"reversePublication": true},
	})
	alice := withPrincipal(context.Background(), "user:alice-uuid")
	bob := withPrincipal(context.Background(), "user:bob-uuid")

	remote, err := svc.CreateRemote(alice, &proto.CreateRemoteRequest{
		Tunnel: validTunnel(), Providers: []*proto.RemoteProviderDefinition{appProvider("app", "test-app")}, ExpectedGeneration: 0,
	})
	if err != nil {
		t.Fatalf("alice CreateRemote: %v", err)
	}
	if _, err := svc.DeleteRemote(bob, &proto.DeleteRemoteRequest{
		Id: remote.GetId(), ExpectedGeneration: remote.GetGeneration(),
	}); codeOf(t, err) != codes.NotFound {
		t.Fatalf("bob cross-owner delete code = %v, want NotFound", codeOf(t, err))
	}
	resp, err := svc.ListRemotes(alice, &proto.ListRemotesRequest{})
	if err != nil {
		t.Fatalf("alice ListRemotes after bob delete: %v", err)
	}
	if len(resp.GetRemotes()) != 1 {
		t.Fatalf("alice remotes after bob delete = %d, want 1", len(resp.GetRemotes()))
	}
}

func TestListRemotesEmptyAndAfterCreate(t *testing.T) {
	t.Parallel()
	ctx := withPrincipal(context.Background(), "user:alice-uuid")
	svc := newRemoteService(t, nil)

	resp, err := svc.ListRemotes(ctx, &proto.ListRemotesRequest{})
	if err != nil {
		t.Fatalf("ListRemotes empty: %v", err)
	}
	if len(resp.Remotes) != 0 {
		t.Fatalf("empty list = %d, want 0", len(resp.Remotes))
	}

	if _, err := svc.CreateRemote(ctx, &proto.CreateRemoteRequest{
		Tunnel: validTunnel(), Providers: []*proto.RemoteProviderDefinition{appProvider("app", "test-app")}, ExpectedGeneration: 0,
	}); err != nil {
		t.Fatalf("CreateRemote: %v", err)
	}
	resp, err = svc.ListRemotes(ctx, &proto.ListRemotesRequest{})
	if err != nil {
		t.Fatalf("ListRemotes after create: %v", err)
	}
	if len(resp.Remotes) != 1 || resp.Remotes[0].Generation != 1 {
		t.Fatalf("list = %d remotes, gen %d, want 1 remote gen 1", len(resp.Remotes), func() uint64 {
			if len(resp.Remotes) > 0 {
				return resp.Remotes[0].Generation
			}
			return 0
		}())
	}
}

func TestPublicationAuthorizationStates(t *testing.T) {
	t.Parallel()
	ctx := withPrincipal(context.Background(), "user:alice-uuid")

	t.Run("omitted_provider_allows", func(t *testing.T) {
		t.Parallel()
		svc := newRemoteService(t, nil)
		if _, err := svc.CreateRemote(ctx, &proto.CreateRemoteRequest{
			Tunnel: validTunnel(), Providers: []*proto.RemoteProviderDefinition{appProvider("app", "test-app")}, ExpectedGeneration: 0,
		}); err != nil {
			t.Fatalf("omitted authz CreateRemote: %v", err)
		}
	})

	t.Run("denied_returns_permission_denied", func(t *testing.T) {
		t.Parallel()
		svc := newRemoteService(t, &stubAuthz{allowed: false})
		_, err := svc.CreateRemote(ctx, &proto.CreateRemoteRequest{
			Tunnel: validTunnel(), Providers: []*proto.RemoteProviderDefinition{appProvider("app", "test-app")}, ExpectedGeneration: 0,
		})
		if got := codeOf(t, err); got != codes.PermissionDenied {
			t.Fatalf("denied code = %v, want %v", got, codes.PermissionDenied)
		}
	})

	t.Run("dedicated_permission_allows", func(t *testing.T) {
		t.Parallel()
		svc := newRemoteService(t, &stubAuthz{
			allowedResourceTypes: map[string]bool{"reversePublication": true},
		})
		if _, err := svc.CreateRemote(ctx, &proto.CreateRemoteRequest{
			Tunnel: validTunnel(), Providers: []*proto.RemoteProviderDefinition{appProvider("app", "test-app")}, ExpectedGeneration: 0,
		}); err != nil {
			t.Fatalf("dedicated permission CreateRemote: %v", err)
		}
	})

	t.Run("provider_failure_returns_unavailable", func(t *testing.T) {
		t.Parallel()
		svc := newRemoteService(t, &stubAuthz{err: errors.New("authz down")})
		_, err := svc.CreateRemote(ctx, &proto.CreateRemoteRequest{
			Tunnel: validTunnel(), Providers: []*proto.RemoteProviderDefinition{appProvider("app", "test-app")}, ExpectedGeneration: 0,
		})
		if got := codeOf(t, err); got != codes.Unavailable {
			t.Fatalf("provider failure code = %v, want %v", got, codes.Unavailable)
		}
	})
}

func TestCreateRemoteUnauthenticatedWithoutPrincipal(t *testing.T) {
	t.Parallel()
	svc := newRemoteService(t, nil)
	_, err := svc.CreateRemote(context.Background(), &proto.CreateRemoteRequest{
		Tunnel: validTunnel(), Providers: []*proto.RemoteProviderDefinition{appProvider("app", "test-app")}, ExpectedGeneration: 0,
	})
	if got := codeOf(t, err); got != codes.Unauthenticated {
		t.Fatalf("no principal code = %v, want %v", got, codes.Unauthenticated)
	}
}

func TestCreateRemoteValidationFailureIsFailedPrecondition(t *testing.T) {
	t.Parallel()
	ctx := withPrincipal(context.Background(), "user:alice-uuid")
	svc := newRemoteService(t, nil)
	svc.SetValidator(&fakeValidator{fail: true})
	_, err := svc.CreateRemote(ctx, &proto.CreateRemoteRequest{
		Tunnel: validTunnel(), Providers: []*proto.RemoteProviderDefinition{appProvider("app", "test-app")}, ExpectedGeneration: 0,
	})
	if got := codeOf(t, err); got != codes.FailedPrecondition {
		t.Fatalf("validation failure code = %v, want %v", got, codes.FailedPrecondition)
	}
}

func TestDeleteRemoteRequiresPrincipalEvenWithoutAuthz(t *testing.T) {
	t.Parallel()
	// With no AuthorizationProvider, admin checks are skipped, but an
	// unauthenticated caller still must not delete a registration by id.
	svc := newRemoteService(t, nil)
	_, err := svc.DeleteRemote(context.Background(), &proto.DeleteRemoteRequest{
		Id: "reg-1", ExpectedGeneration: 1,
	})
	if got := codeOf(t, err); got != codes.Unauthenticated {
		t.Fatalf("delete without principal code = %v, want %v", got, codes.Unauthenticated)
	}
}

func TestListRemotesOmitsExpiredRegistration(t *testing.T) {
	t.Parallel()
	ctx := withPrincipal(context.Background(), "user:alice-uuid")
	svc := newRemoteService(t, nil)
	if _, err := svc.CreateRemote(ctx, &proto.CreateRemoteRequest{
		Tunnel: validTunnel(), Providers: []*proto.RemoteProviderDefinition{appProvider("app", "test-app")}, ExpectedGeneration: 0,
	}); err != nil {
		t.Fatalf("CreateRemote: %v", err)
	}
	// Advance past the lease deadline; reads resolve unregistered, so List must not surface it.
	svc.AdvanceClock(testLease + time.Second)
	resp, err := svc.ListRemotes(ctx, &proto.ListRemotesRequest{})
	if err != nil {
		t.Fatalf("ListRemotes after expiry: %v", err)
	}
	if len(resp.Remotes) != 0 {
		t.Fatalf("remotes after expiry = %d, want 0", len(resp.Remotes))
	}
}

func TestCreateRemoteRejectsDuplicateProviders(t *testing.T) {
	t.Parallel()
	ctx := withPrincipal(context.Background(), "user:alice-uuid")
	svc := newRemoteService(t, nil)
	dup := appProvider("app", "test-app")
	_, err := svc.CreateRemote(ctx, &proto.CreateRemoteRequest{
		Tunnel:             validTunnel(),
		Providers:          []*proto.RemoteProviderDefinition{dup, dup},
		ExpectedGeneration: 0,
	})
	if got := codeOf(t, err); got != codes.InvalidArgument {
		t.Fatalf("duplicate providers code = %v, want %v", got, codes.InvalidArgument)
	}
}

type stubAuthz struct {
	allowed              bool
	allowedResourceTypes map[string]bool
	err                  error
}

func (s *stubAuthz) CheckAccess(ctx context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.allowedResourceTypes != nil {
		return &proto.CheckAccessResponse{Allowed: s.allowedResourceTypes[req.GetResource().GetType()]}, nil
	}
	return &proto.CheckAccessResponse{Allowed: s.allowed}, nil
}
func (s *stubAuthz) CheckAccessMany(context.Context, *proto.CheckAccessManyRequest) (*proto.CheckAccessManyResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}
func (s *stubAuthz) ListRelationships(context.Context, *proto.ListRelationshipsRequest) (*proto.ListRelationshipsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}
func (s *stubAuthz) WriteRelationships(context.Context, *proto.WriteRelationshipsRequest) (*proto.WriteRelationshipsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}
func (s *stubAuthz) AddRelationship(context.Context, *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}
func (s *stubAuthz) DeleteRelationship(context.Context, *proto.DeleteRelationshipRequest) (*proto.DeleteRelationshipResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}
func (s *stubAuthz) SetAuthorizationState(context.Context, *proto.SetAuthorizationStateRequest) (*proto.SetAuthorizationStateResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}
func (s *stubAuthz) GetActiveModelRef(context.Context) (*proto.GetActiveModelRefResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}
func (s *stubAuthz) SetActiveModel(context.Context, *proto.SetActiveModelRequest) (*proto.SetActiveModelResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}
func (s *stubAuthz) ListActiveModelResourceTypes(context.Context, *proto.ListActiveModelResourceTypesRequest) (*proto.ListActiveModelResourceTypesResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}
func (s *stubAuthz) Ping(context.Context) error { return nil }
func (s *stubAuthz) Close() error               { return nil }
