package mcpoauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/egress"
)

func TestDiscoverRejectsHTTPWithoutLocalOptIn(t *testing.T) {
	t.Parallel()

	_, err := Discover(context.Background(), "http://example.com/mcp")
	if err == nil {
		t.Fatal("expected HTTP discovery to be rejected")
	}
	if !strings.Contains(err.Error(), "must use https") {
		t.Fatalf("Discover error = %v, want HTTPS requirement", err)
	}
}

func TestDiscoverRejectsUnsafeAdvertisedMetadataURL(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="https://169.254.169.254/.well-known/oauth-protected-resource/mcp"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	testutil.CloseOnCleanup(t, srv)

	_, err := Discover(context.Background(), srv.URL+"/mcp", withInsecureLocalDiscovery())
	if !errors.Is(err, egress.ErrUnsafeDestination) {
		t.Fatalf("Discover error = %v, want ErrUnsafeDestination", err)
	}
}

func TestDiscoverRejectsUnsafeAuthorizationServerURL(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /mcp", func(w http.ResponseWriter, r *http.Request) {
		baseURL := "http://" + r.Host
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(
			`Bearer resource_metadata="%s/.well-known/oauth-protected-resource/mcp"`, baseURL))
		w.WriteHeader(http.StatusUnauthorized)
	})
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"authorization_servers": []string{"https://169.254.169.254"},
		})
	})
	srv := httptest.NewServer(mux)
	testutil.CloseOnCleanup(t, srv)

	_, err := Discover(context.Background(), srv.URL+"/mcp", withInsecureLocalDiscovery())
	if !errors.Is(err, egress.ErrUnsafeDestination) {
		t.Fatalf("Discover error = %v, want ErrUnsafeDestination", err)
	}
}
