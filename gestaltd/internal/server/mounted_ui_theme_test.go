package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestMountedUIThemeHandlerFullPathRootMount(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stylesheet := filepath.Join(dir, "tenant.css")
	if err := os.WriteFile(stylesheet, []byte("body{}"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	mounted := MountedUI{
		Path:            "/",
		ThemeStylesheet: stylesheet,
		IsDev:           true,
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "next", http.StatusTeapot)
	})

	handler := mountedUIThemeHandlerFullPath(mounted, next)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/theme.css", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /theme.css status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != "body{}" {
		t.Fatalf("GET /theme.css body = %q, want body{}", got)
	}
}

func TestMountedUIThemePaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mount      string
		stylesheet string
		assets     string
	}{
		{mount: "/", stylesheet: "/theme.css", assets: "/theme/"},
		{mount: "/demo", stylesheet: "/demo/theme.css", assets: "/demo/theme/"},
		{mount: "/demo/", stylesheet: "/demo/theme.css", assets: "/demo/theme/"},
	}
	for _, tc := range tests {
		gotStylesheet, gotAssets := mountedUIThemePaths(tc.mount)
		if gotStylesheet != tc.stylesheet {
			t.Fatalf("mountedUIThemePaths(%q) stylesheet = %q, want %q", tc.mount, gotStylesheet, tc.stylesheet)
		}
		if gotAssets != tc.assets {
			t.Fatalf("mountedUIThemePaths(%q) assets = %q, want %q", tc.mount, gotAssets, tc.assets)
		}
	}
}
