package server

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc"
)

func TestUnifiedHostServiceHandlerReturnsVerifierError(t *testing.T) {
	t.Parallel()

	registry := runtimehost.NewPublicHostServiceRegistry()
	registry.RegisterVerified("support", rejectHostServiceSessionVerifier{}, testHostService())
	s := &Server{publicHostServices: registry}

	handler, err := s.unifiedHostServiceHandler(context.Background(), runtimehost.HostServiceRelayTarget{
		AppName:   "support",
		SessionID: "session-1",
		Service:   "cache",
	}, "/gestalt.provider.v1.Cache/Get")
	if err == nil || !strings.Contains(err.Error(), `runtime session "session-1" is not active`) {
		t.Fatalf("unifiedHostServiceHandler err = %v, want verifier failure", err)
	}
	if handler != nil {
		t.Fatalf("handler = %v, want no handler", handler)
	}
}

func TestUnifiedHostServiceHandlerRoutesByGRPCMethod(t *testing.T) {
	t.Parallel()

	registry := runtimehost.NewPublicHostServiceRegistry()
	registry.RegisterVerified("support", allowHostServiceSessionVerifier{}, testHostService())
	s := &Server{publicHostServices: registry}

	handler, err := s.unifiedHostServiceHandler(context.Background(), runtimehost.HostServiceRelayTarget{
		AppName:   "support",
		SessionID: "session-1",
		Service:   "host_service",
	}, "/gestalt.provider.v1.Cache/Get")
	if err != nil {
		t.Fatalf("unifiedHostServiceHandler: %v", err)
	}
	if handler == nil {
		t.Fatal("handler = nil, want cache handler")
	}

	handler, err = s.unifiedHostServiceHandler(context.Background(), runtimehost.HostServiceRelayTarget{
		AppName:   "support",
		SessionID: "session-1",
		Service:   "host_service",
	}, "/gestalt.provider.v1.S3/ListObjects")
	if err != nil {
		t.Fatalf("unifiedHostServiceHandler unregistered method: %v", err)
	}
	if handler != nil {
		t.Fatal("handler for unregistered method is non-nil")
	}
}

func TestUnifiedHostServiceHandlerRoutesAppServiceToGlobalRegistration(t *testing.T) {
	t.Parallel()

	registry := runtimehost.NewPublicHostServiceRegistry()
	registry.RegisterVerified(appInvocationPublicProviderKey, allowHostServiceSessionVerifier{}, runtimehost.HostService{
		Name:           "app",
		MethodPrefixes: []string{"/gestalt.provider.v1.App/"},
		Register:       func(*grpc.Server) {},
	})
	s := &Server{publicHostServices: registry}

	// The token's AppName is the caller; App/* RPCs resolve to the global registration.
	handler, err := s.unifiedHostServiceHandler(context.Background(), runtimehost.HostServiceRelayTarget{
		AppName:   "agent-trace-viewer",
		SessionID: "session-1",
		Service:   "app",
	}, "/gestalt.provider.v1.App/Invoke")
	if err != nil {
		t.Fatalf("unifiedHostServiceHandler: %v", err)
	}
	if handler == nil {
		t.Fatal("handler = nil, want global app handler")
	}

	// Non-App services stay keyed by the caller provider name.
	handler, err = s.unifiedHostServiceHandler(context.Background(), runtimehost.HostServiceRelayTarget{
		AppName:   "agent-trace-viewer",
		SessionID: "session-1",
		Service:   "cache",
	}, "/gestalt.provider.v1.Cache/Get")
	if err != nil {
		t.Fatalf("unifiedHostServiceHandler cache: %v", err)
	}
	if handler != nil {
		t.Fatal("handler for caller-keyed service is non-nil, want no leak into the global app key")
	}
}

func TestValidatePublicHostServicesRejectsProviderWideServiceWithoutVerifier(t *testing.T) {
	t.Parallel()

	err := validatePublicHostServices([]runtimehost.PublicHostService{
		testPublicHostService("support", nil),
	})
	if err == nil || !strings.Contains(err.Error(), "public host service support requires a session verifier") {
		t.Fatalf("validatePublicHostServices err = %v, want missing verifier failure", err)
	}
}

func TestValidatePublicHostServicesAllowsDuplicateVerifiedServices(t *testing.T) {
	t.Parallel()

	err := validatePublicHostServices([]runtimehost.PublicHostService{
		testPublicHostService("support", allowHostServiceSessionVerifier{}),
		testPublicHostService("support", rejectHostServiceSessionVerifier{}),
	})
	if err != nil {
		t.Fatalf("validatePublicHostServices: %v", err)
	}
}

func testPublicHostService(pluginName string, verifier runtimehost.PublicHostServiceSessionVerifier) runtimehost.PublicHostService {
	return runtimehost.PublicHostService{
		AppName:         pluginName,
		SessionVerifier: verifier,
		Service:         testHostService(),
	}
}

func testHostService() runtimehost.HostService {
	return runtimehost.HostService{
		Name:           "cache",
		MethodPrefixes: []string{"/gestalt.provider.v1.Cache/"},
		Register:       func(*grpc.Server) {},
	}
}

type rejectHostServiceSessionVerifier struct{}

func (rejectHostServiceSessionVerifier) VerifyHostServiceSession(_ context.Context, sessionID string) error {
	return fmt.Errorf("runtime session %q is not active", strings.TrimSpace(sessionID))
}

type allowHostServiceSessionVerifier struct{}

func (allowHostServiceSessionVerifier) VerifyHostServiceSession(context.Context, string) error {
	return nil
}
