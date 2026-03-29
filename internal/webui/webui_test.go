package webui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)



func mustGet(t *testing.T, handler http.Handler, path string) (int, string) {
	t.Helper()
	ts := httptest.NewServer(handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return resp.StatusCode, string(body)
}

func TestHandler_RootServesIndexHTML(t *testing.T) {
	t.Parallel()

	handler := NewHandler(fstest.MapFS{
		"index.html": {Data: []byte("<html>home</html>")},
	})

	code, body := mustGet(t, handler, "/")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if !strings.Contains(body, "home") {
		t.Fatalf("body = %q, want to contain 'home'", body)
	}
}

func TestHandler_ExactFileMatch(t *testing.T) {
	t.Parallel()

	handler := NewHandler(fstest.MapFS{
		"index.html": {Data: []byte("<html>home</html>")},
		"style.css":  {Data: []byte("body { color: red; }")},
	})

	code, body := mustGet(t, handler, "/style.css")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if !strings.Contains(body, "color: red") {
		t.Fatalf("body = %q, want CSS content", body)
	}
}

func TestHandler_HTMLSuffixFallback(t *testing.T) {
	t.Parallel()

	handler := NewHandler(fstest.MapFS{
		"index.html": {Data: []byte("<html>home</html>")},
		"about.html": {Data: []byte("<html>about page</html>")},
	})

	code, body := mustGet(t, handler, "/about")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if !strings.Contains(body, "about page") {
		t.Fatalf("body = %q, want about page content", body)
	}
}

func TestHandler_DirectoryRedirectsToTrailingSlash(t *testing.T) {
	t.Parallel()

	handler := NewHandler(fstest.MapFS{
		"index.html":      {Data: []byte("<html>home</html>")},
		"docs/index.html": {Data: []byte("<html>docs</html>")},
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/docs", nil)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want %d (redirect to /docs/)", rr.Code, http.StatusMovedPermanently)
	}
}

func TestHandler_SPAFallbackForNonexistentPath(t *testing.T) {
	t.Parallel()

	handler := NewHandler(fstest.MapFS{
		"index.html": {Data: []byte("<html>spa-root</html>")},
	})

	code, body := mustGet(t, handler, "/nonexistent")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if !strings.Contains(body, "spa-root") {
		t.Fatalf("body = %q, want SPA fallback to index.html", body)
	}
}

func TestHandler_SPAFallbackForMissingExtensionFile(t *testing.T) {
	t.Parallel()

	handler := NewHandler(fstest.MapFS{
		"index.html": {Data: []byte("<html>spa-root</html>")},
	})

	code, body := mustGet(t, handler, "/missing.js")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if !strings.Contains(body, "spa-root") {
		t.Fatalf("body = %q, want SPA fallback for missing .js file", body)
	}
}

func TestHandler_HTMLFallbackPreferredOverSPA(t *testing.T) {
	t.Parallel()

	handler := NewHandler(fstest.MapFS{
		"index.html":    {Data: []byte("<html>home</html>")},
		"settings.html": {Data: []byte("<html>settings</html>")},
	})

	code, body := mustGet(t, handler, "/settings")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if !strings.Contains(body, "settings") {
		t.Fatalf("body = %q, want settings page (not SPA fallback)", body)
	}
}

func TestHandler_NilWhenNoIndexHTML(t *testing.T) {
	t.Parallel()

	handler := EmbeddedHandler()
	if handler != nil {
		t.Fatal("expected nil handler when embedded frontend has not been built")
	}
}


func TestHandler_StaticAssetInSubdirectory(t *testing.T) {
	t.Parallel()

	handler := NewHandler(fstest.MapFS{
		"index.html":    {Data: []byte("<html>home</html>")},
		"assets/app.js": {Data: []byte("console.log('app');")},
	})

	code, body := mustGet(t, handler, "/assets/app.js")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if !strings.Contains(body, "console.log") {
		t.Fatalf("body = %q, want JS content", body)
	}
}
