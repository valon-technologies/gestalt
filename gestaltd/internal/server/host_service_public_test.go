package server

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc"
)

func TestHostServiceHandlerDoesNotFallbackWhenExactSessionEntryFails(t *testing.T) {
	t.Parallel()

	registry := runtimehost.NewPublicHostServiceRegistry()
	registry.RegisterSession("support", "session-1", testHostService(), testHostService())
	registry.RegisterVerified("support", allowHostServiceSessionVerifier{}, testHostService())
	s := &Server{publicHostServices: registry}

	handler, err := s.hostServiceHandler(context.Background(), runtimehost.HostServiceRelayTarget{
		PluginName: "support",
		SessionID:  "session-1",
		Service:    "cache",
		EnvVar:     "GESTALT_TEST_CACHE_SOCKET",
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate public host service support/cache/GESTALT_TEST_CACHE_SOCKET/session=session-1") {
		t.Fatalf("hostServiceHandler err = %v, want exact session duplicate failure", err)
	}
	if handler != nil {
		t.Fatalf("handler = %v, want no provider-wide fallback", handler)
	}
}

func TestValidatePublicHostServicesRejectsDuplicateUnverifiedServices(t *testing.T) {
	t.Parallel()

	err := validatePublicHostServices([]runtimehost.PublicHostService{
		testPublicHostService("support", nil),
		testPublicHostService("support", nil),
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate public host service support/cache/GESTALT_TEST_CACHE_SOCKET") {
		t.Fatalf("validatePublicHostServices err = %v, want duplicate public host service", err)
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

func TestHostServiceHandlerEntryRejectsDynamicDuplicateUnverifiedServices(t *testing.T) {
	t.Parallel()

	registry := runtimehost.NewPublicHostServiceRegistry()
	registry.Register("support", testHostService(), testHostService())
	s := &Server{publicHostServices: registry}
	key := hostServiceHandlerKey{
		pluginName: "support",
		service:    "cache",
		envVar:     "GESTALT_TEST_CACHE_SOCKET",
	}
	_, _, _, err := s.hostServiceHandlerEntry(context.Background(), key, "")
	if err == nil || !strings.Contains(err.Error(), "duplicate public host service support/cache/GESTALT_TEST_CACHE_SOCKET") {
		t.Fatalf("hostServiceHandlerEntry err = %v, want duplicate public host service", err)
	}
}

func testPublicHostService(pluginName string, verifier runtimehost.PublicHostServiceSessionVerifier) runtimehost.PublicHostService {
	return runtimehost.PublicHostService{
		PluginName:      pluginName,
		SessionVerifier: verifier,
		Service:         testHostService(),
	}
}

func testHostService() runtimehost.HostService {
	return runtimehost.HostService{
		Name:     "cache",
		EnvVar:   "GESTALT_TEST_CACHE_SOCKET",
		Register: func(*grpc.Server) {},
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
