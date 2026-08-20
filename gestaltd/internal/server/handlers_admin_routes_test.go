package server_test

import (
	"net/http"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestAdminRegistryBookmarkRedirect(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t)
	testutil.CloseOnCleanup(t, ts)

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(ts.URL + "/admin/registry/g-issues")
	if err != nil {
		t.Fatalf("GET registry bookmark: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want 301", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "/admin/versions/g-issues" {
		t.Fatalf("Location = %q", got)
	}
}

func TestAdminRegistryBookmarkRedirectOnManagementProfile(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.RouteProfile = server.RouteProfileManagement
		cfg.PublicBaseURL = "https://gestalt.example.test"
	})
	testutil.CloseOnCleanup(t, ts)

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(ts.URL + "/admin/registry/g-issues")
	if err != nil {
		t.Fatalf("GET registry bookmark: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want 301", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "https://gestalt.example.test/admin/versions/g-issues" {
		t.Fatalf("Location = %q", got)
	}
}

func TestManagementAdminPagesDoNotRedirectWithoutPublicBaseURL(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.RouteProfile = server.RouteProfileManagement
	})
	testutil.CloseOnCleanup(t, ts)

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	for _, path := range []string{"/", "/admin", "/admin/", "/admin/metrics", "/admin/versions"} {
		resp, err := client.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusMovedPermanently || resp.StatusCode == http.StatusFound {
			t.Fatalf("%s status = %d Location = %q, want no redirect", path, resp.StatusCode, resp.Header.Get("Location"))
		}
	}
}
