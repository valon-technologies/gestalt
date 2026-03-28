package webui

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func newTestHandler(t *testing.T, files fstest.MapFS) http.Handler {
	t.Helper()

	fileServer := http.FileServer(http.FS(files))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		if _, err := fs.Stat(files, path); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}

		if !strings.Contains(path, ".") {
			if _, err := fs.Stat(files, path+".html"); err == nil {
				serve(fileServer, w, r, "/"+path+".html")
				return
			}
			if _, err := fs.Stat(files, path+"/index.html"); err == nil {
				serve(fileServer, w, r, "/"+path+"/index.html")
				return
			}
		}

		serve(fileServer, w, r, "/index.html")
	})
}

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

	handler := newTestHandler(t, fstest.MapFS{
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

	handler := newTestHandler(t, fstest.MapFS{
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

	handler := newTestHandler(t, fstest.MapFS{
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

func TestHandler_DirectoryIndexServedForTrailingSlash(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{
		"index.html":      {Data: []byte("<html>home</html>")},
		"docs/index.html": {Data: []byte("<html>docs</html>")},
	}
	fileServer := http.FileServer(http.FS(files))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/docs/", nil)
	fileServer.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if !strings.Contains(rr.Body.String(), "docs") {
		t.Fatalf("body = %q, want docs content", rr.Body.String())
	}
}

func TestHandler_DirectoryRedirectsToTrailingSlash(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, fstest.MapFS{
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

	handler := newTestHandler(t, fstest.MapFS{
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

	handler := newTestHandler(t, fstest.MapFS{
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

	handler := newTestHandler(t, fstest.MapFS{
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

	handler := Handler()
	if handler != nil {
		t.Fatal("expected nil handler when embedded frontend has not been built")
	}
}

func TestHandler_SPAFallbackForDeepPath(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, fstest.MapFS{
		"index.html":    {Data: []byte("<html>spa-root</html>")},
		"assets/app.js": {Data: []byte("console.log('app');")},
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/app/dashboard", nil)
	handler.ServeHTTP(rr, req)

	switch rr.Code {
	case http.StatusOK:
		if !strings.Contains(rr.Body.String(), "spa-root") {
			t.Fatalf("body = %q, want SPA fallback content", rr.Body.String())
		}
	case http.StatusMovedPermanently, http.StatusFound:
		loc := rr.Header().Get("Location")
		if loc != "/" && loc != "./" {
			t.Fatalf("expected redirect to root, got Location: %q", loc)
		}
	default:
		t.Fatalf("unexpected status = %d", rr.Code)
	}
}

func TestHandler_StaticAssetInSubdirectory(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, fstest.MapFS{
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
