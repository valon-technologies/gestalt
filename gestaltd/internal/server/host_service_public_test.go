package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc"
)

func TestHostServiceHandlerFallsBackWhenExactVerifierRejects(t *testing.T) {
	t.Parallel()

	exactKey := hostServiceHandlerKey{
		pluginName: "support",
		sessionID:  "session-1",
		service:    "cache",
		envVar:     "GESTALT_TEST_CACHE_SOCKET",
	}
	providerKey := exactKey
	providerKey.sessionID = ""
	exactHandler := markerHostServiceHandler("exact")
	providerHandler := markerHostServiceHandler("provider")

	s := &Server{
		hostServiceHandlers: map[hostServiceHandlerKey][]hostServiceHandlerEntry{
			exactKey: {{
				handler:  exactHandler,
				verifier: rejectHostServiceSessionVerifier{},
			}},
			providerKey: {{
				handler:  providerHandler,
				verifier: allowHostServiceSessionVerifier{},
			}},
		},
	}

	handler, err := s.hostServiceHandler(context.Background(), runtimehost.HostServiceRelayTarget{
		PluginName: "support",
		SessionID:  "session-1",
		Service:    "cache",
		EnvVar:     "GESTALT_TEST_CACHE_SOCKET",
	})
	if err != nil {
		t.Fatalf("hostServiceHandler: %v", err)
	}
	if handler != providerHandler {
		t.Fatalf("handler = %v, want provider-wide fallback", handler)
	}
}

func TestNewPublicHostServiceHandlersRejectsDuplicateUnverifiedServices(t *testing.T) {
	t.Parallel()

	_, err := newPublicHostServiceHandlers([]runtimehost.PublicHostService{
		testPublicHostService("support", nil),
		testPublicHostService("support", nil),
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate public host service support/cache/GESTALT_TEST_CACHE_SOCKET") {
		t.Fatalf("newPublicHostServiceHandlers err = %v, want duplicate public host service", err)
	}
}

func TestNewPublicHostServiceHandlersAllowsDuplicateVerifiedServices(t *testing.T) {
	t.Parallel()

	handlers, err := newPublicHostServiceHandlers([]runtimehost.PublicHostService{
		testPublicHostService("support", allowHostServiceSessionVerifier{}),
		testPublicHostService("support", rejectHostServiceSessionVerifier{}),
	})
	if err != nil {
		t.Fatalf("newPublicHostServiceHandlers: %v", err)
	}
	key := hostServiceHandlerKey{
		pluginName: "support",
		service:    "cache",
		envVar:     "GESTALT_TEST_CACHE_SOCKET",
	}
	if got := len(handlers[key]); got != 2 {
		t.Fatalf("handlers[%s] = %d, want 2", key.String(), got)
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
	_, _, err := s.hostServiceHandlerEntry(context.Background(), key, "")
	if err == nil || !strings.Contains(err.Error(), "duplicate public host service support/cache/GESTALT_TEST_CACHE_SOCKET") {
		t.Fatalf("hostServiceHandlerEntry err = %v, want duplicate public host service", err)
	}
}

type markerHostServiceHandler string

func (h markerHostServiceHandler) ServeHTTP(http.ResponseWriter, *http.Request) {}

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
