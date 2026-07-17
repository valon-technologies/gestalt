package runtimehost

import (
	"context"
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestHostServiceRelayMethodAllowed(t *testing.T) {
	t.Parallel()

	identityPrefix := "/" + proto.Identity_ServiceDesc.ServiceName + "/"
	if !HostServiceRelayMethodAllowed("/gestalt.provider.v1.Identity/UserInfo", identityPrefix) {
		t.Fatal("expected identity userinfo to be allowed")
	}
	if HostServiceRelayMethodAllowed("/gestalt.provider.v1.App/Invoke", identityPrefix) {
		t.Fatal("expected app invoke to be rejected for identity-scoped capability")
	}
}

func TestAuthenticateGRPCRejectsMissingCallerCapability(t *testing.T) {
	t.Parallel()

	manager := testRelayTokenManager(t)
	method := "/" + proto.Identity_ServiceDesc.ServiceName + "/UserInfo"

	_, err := manager.AuthenticateGRPC(context.Background(), method)
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("AuthenticateGRPC() error = %v, want Unauthenticated", err)
	}
}

func TestAuthenticateGRPCRejectsCallerlessCapability(t *testing.T) {
	t.Parallel()

	manager := testRelayTokenManager(t)
	method := "/" + proto.Identity_ServiceDesc.ServiceName + "/UserInfo"
	token, err := manager.MintToken(HostServiceRelayTokenRequest{
		Service:      "identity",
		MethodPrefix: "/" + proto.Identity_ServiceDesc.ServiceName + "/",
	})
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		HostServiceRelayTokenHeader, token,
	))

	_, err = manager.AuthenticateGRPC(ctx, method)
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("AuthenticateGRPC() error = %v, want Unauthenticated", err)
	}
}

func TestAuthenticateGRPCRejectsWrongMethodPrefix(t *testing.T) {
	t.Parallel()

	manager := testRelayTokenManager(t)
	method := "/" + proto.App_ServiceDesc.ServiceName + "/Invoke"
	token, err := manager.MintToken(HostServiceRelayTokenRequest{
		Service:      "identity",
		MethodPrefix: "/" + proto.Identity_ServiceDesc.ServiceName + "/",
		Caller:       &PrincipalClaims{SubjectID: "user:alice"},
	})
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		HostServiceRelayTokenHeader, token,
	))

	_, err = manager.AuthenticateGRPC(ctx, method)
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("AuthenticateGRPC() error = %v, want PermissionDenied", err)
	}
}

func TestAuthenticateGRPCRejectsExpiredCapability(t *testing.T) {
	t.Parallel()

	manager := testRelayTokenManager(t)
	manager.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	method := "/" + proto.Identity_ServiceDesc.ServiceName + "/UserInfo"
	token, err := manager.MintToken(HostServiceRelayTokenRequest{
		Service:      "identity",
		MethodPrefix: "/" + proto.Identity_ServiceDesc.ServiceName + "/",
		Caller:       &PrincipalClaims{SubjectID: "user:alice"},
		TTL:          time.Second,
	})
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	manager.now = func() time.Time { return time.Unix(1_700_000_010, 0) }
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		HostServiceRelayTokenHeader, token,
	))

	_, err = manager.AuthenticateGRPC(ctx, method)
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("AuthenticateGRPC() error = %v, want Unauthenticated", err)
	}
}

func testRelayTokenManager(t *testing.T) *HostServiceRelayTokenManager {
	t.Helper()
	manager, err := NewHostServiceRelayTokenManager([]byte("capability-ingress-test-secret-01"))
	if err != nil {
		t.Fatalf("NewHostServiceRelayTokenManager: %v", err)
	}
	return manager
}
