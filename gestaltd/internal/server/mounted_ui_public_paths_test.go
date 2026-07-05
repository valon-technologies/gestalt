package server_test

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestPublicPathMatches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pattern string
		path    string
		want    bool
	}{
		{pattern: "/login", path: "/login", want: true},
		{pattern: "/login", path: "/login/", want: false},
		{pattern: "/login/**", path: "/login", want: true},
		{pattern: "/login/**", path: "/login/extra", want: true},
		{pattern: "/docs/**", path: "/docs/guide", want: true},
		{pattern: "/docs/**", path: "/docs/guide/nested", want: true},
		{pattern: "/docs/*", path: "/docs/guide", want: true},
		{pattern: "/docs/*", path: "/docs/guide/nested", want: false},
		{pattern: "/apps", path: "/apps", want: true},
		{pattern: "/apps", path: "/login", want: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.pattern+"_"+tc.path, func(t *testing.T) {
			t.Parallel()
			if got := server.PublicPathMatches(tc.pattern, tc.path); got != tc.want {
				t.Fatalf("PublicPathMatches(%q, %q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
			}
		})
	}
}

func TestMountedAppStaticPublicPaths(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	writeTestUIAsset(t, filepath.Join(rootDir, "index.html"), "<html>home</html>")
	writeTestUIAsset(t, filepath.Join(rootDir, "login", "index.html"), "<html>login page</html>")
	writeTestUIAsset(t, filepath.Join(rootDir, "docs", "guide", "index.html"), "<html>docs guide</html>")
	writeTestUIAsset(t, filepath.Join(rootDir, "apps", "index.html"), "<html>apps page</html>")

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"home": {
				Static: &config.AppStaticConfig{
					Mount: "/",
					PublicPaths: []string{
						"/login",
						"/login/**",
						"/docs/**",
					},
				},
				ResolvedStaticRoot: rootDir,
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	t.Run("login without session", func(t *testing.T) {
		t.Parallel()
		resp, err := http.Get(ts.URL + "/login")
		if err != nil {
			t.Fatalf("GET /login: %v", err)
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
		}
		if !strings.Contains(string(body), "login page") {
			t.Fatalf("body = %q, want login page", body)
		}
	})

	t.Run("docs without session", func(t *testing.T) {
		t.Parallel()
		resp, err := http.Get(ts.URL + "/docs/guide")
		if err != nil {
			t.Fatalf("GET /docs/guide: %v", err)
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
		}
		if !strings.Contains(string(body), "docs guide") {
			t.Fatalf("body = %q, want docs guide", body)
		}
	})

	t.Run("protected path redirects to login", func(t *testing.T) {
		t.Parallel()
		noRedirect := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		}}
		resp, err := noRedirect.Get(ts.URL + "/apps")
		if err != nil {
			t.Fatalf("GET /apps: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusFound {
			t.Fatalf("status = %d, want 302", resp.StatusCode)
		}
		if got := resp.Header.Get("Location"); got != "/login?next=%2Fapps" {
			t.Fatalf("Location = %q, want /login?next=%%2Fapps", got)
		}
	})
}
