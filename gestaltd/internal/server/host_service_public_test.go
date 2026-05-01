package server

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc"
)

func TestHostServiceHandlerReturnsVerifierError(t *testing.T) {
	t.Parallel()

	registry := runtimehost.NewPublicHostServiceRegistry()
	registry.RegisterVerified("support", rejectHostServiceSessionVerifier{}, testHostService())
	s := &Server{publicHostServices: registry}

	handler, err := s.hostServiceHandler(context.Background(), runtimehost.HostServiceRelayTarget{
		PluginName: "support",
		SessionID:  "session-1",
		Service:    "cache",
		EnvVar:     "GESTALT_TEST_CACHE_SOCKET",
	})
	if err == nil || !strings.Contains(err.Error(), `runtime session "session-1" is not active`) {
		t.Fatalf("hostServiceHandler err = %v, want verifier failure", err)
	}
	if handler != nil {
		t.Fatalf("handler = %v, want no handler", handler)
	}
}

func TestValidatePublicHostServicesRejectsProviderWideServiceWithoutVerifier(t *testing.T) {
	t.Parallel()

	err := validatePublicHostServices([]runtimehost.PublicHostService{
		testPublicHostService("support", nil),
	})
	if err == nil || !strings.Contains(err.Error(), "public host service support/cache/GESTALT_TEST_CACHE_SOCKET requires a session verifier") {
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
