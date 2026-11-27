package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestMountedUIThemeFontCORS(t *testing.T) {
	const login = "https://login.tb.valon.tools"
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "fonts"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, asset := range []string{"fonts/brand.woff", "fonts/brand.woff2", "fonts/note.txt", "not-a-theme-font.woff"} {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(asset)), []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name, method, mount, path, origin, configured, allow, vary string
		status                                                     int
	}{
		{"allowed", "GET", "", "/theme/fonts/brand.woff", login, login, login, "Origin", 200},
		{"mounted head", "HEAD", "/portal", "/portal/theme/fonts/brand.woff2", login, login, login, "Origin", 200},
		{"other origin", "GET", "", "/theme/fonts/brand.woff", "https://other.example", login, "", "Origin", 200},
		{"no origin", "GET", "", "/theme/fonts/brand.woff", "", login, "", "Origin", 200},
		{"disabled", "GET", "", "/theme/fonts/brand.woff", login, "", "", "", 200},
		{"not a font", "GET", "", "/theme/fonts/note.txt", login, login, "", "", 200},
		{"outside fonts", "GET", "", "/theme/not-a-theme-font.woff", login, login, "", "", 200},
		{"missing", "GET", "", "/theme/fonts/missing.woff", login, login, "", "", 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mounted := MountedUI{Path: tc.mount, ThemeAssetsDir: dir, ThemeFontCORSOrigin: tc.configured}
			h := mountedUIThemeHandler(mounted, http.NotFoundHandler())
			if tc.mount != "" {
				h = mountedUIThemeHandlerFullPath(mounted, http.NotFoundHandler())
			}
			r := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status || w.Header().Get("Access-Control-Allow-Origin") != tc.allow || w.Header().Get("Vary") != tc.vary {
				t.Fatalf("got status %d, headers %v; want %d, allow %q, vary %q", w.Code, w.Header(), tc.status, tc.allow, tc.vary)
			}
		})
	}
}
