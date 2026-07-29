package remotepublish

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/grpcutil"
)

func TestIsGRPCRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		method      string
		contentType string
		want        bool
	}{
		{http.MethodPost, "application/grpc", true},
		{http.MethodPost, "application/grpc+proto", true},
		{http.MethodPost, "application/grpc-web", true}, // grpc-web is a grpc variant
		{http.MethodGet, "application/grpc", false},
		{http.MethodPost, "text/html", false},
		{http.MethodPost, "", false},
	}
	for _, tc := range tests {
		req := httptest.NewRequest(tc.method, "/", nil)
		if tc.contentType != "" {
			req.Header.Set("Content-Type", tc.contentType)
		}
		if got := grpcutil.IsGRPCRequest(req); got != tc.want {
			t.Errorf("grpcutil.IsGRPCRequest(method=%s, ct=%s) = %v, want %v", tc.method, tc.contentType, got, tc.want)
		}
	}
}

func TestTunnelUIAppFromPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want string
	}{
		{"/ui/ci-cd", "ci-cd"},
		{"/ui/ci-cd/", "ci-cd"},
		{"/ui/ci-cd/index.html", "ci-cd"},
		{"/ui/my-app/assets/main.js", "my-app"},
		{"", ""},
		{"/", ""},
		{"/ui/", ""},
		{"/api/foo", ""},
	}
	for _, tc := range tests {
		if got := tunnelUIAppFromPath(tc.path); got != tc.want {
			t.Errorf("tunnelUIAppFromPath(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}
