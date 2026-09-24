package server_test

import (
	"io"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestMountedUIThemeAssetFullPathDev(t *testing.T) {
	t.Parallel()

	uiDir := t.TempDir()
	writeTestUIAsset(t, filepath.Join(uiDir, "index.html"), "<html>portal-shell</html>")
	themeDir := t.TempDir()
	writeTestUIAsset(t, filepath.Join(themeDir, "assets", "mark.svg"), "<svg></svg>")

	handler, err := testutilUIHandler(uiDir)
	if err != nil {
		t.Fatalf("ui handler: %v", err)
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.MountedUIs = []server.MountedUI{{
			Name:           "portal",
			Path:           "/portal",
			Handler:        handler,
			ThemeAssetsDir: filepath.Join(themeDir, "assets"),
			BrandName:      "Acme",
			BrandMarkSrc:   "theme/mark.svg",
			IsDev:          true,
		}}
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/portal/theme/mark.svg")
	if err != nil {
		t.Fatalf("GET mark: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%q", resp.StatusCode, body)
	}
	if got := string(body); got != "<svg></svg>" {
		t.Fatalf("body = %q", got)
	}
	if got := resp.Header.Get("Content-Type"); got != "image/svg+xml" {
		t.Fatalf("Content-Type = %q", got)
	}

	resp, err = http.Get(ts.URL + "/portal/brand.json")
	if err != nil {
		t.Fatalf("GET brand.json: %v", err)
	}
	body, err = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll brand: %v", err)
	}
	want := `{"name":"Acme","markSrc":"/portal/theme/mark.svg"}`
	if got := string(body); got != want {
		t.Fatalf("brand.json = %q, want %q", got, want)
	}
}
