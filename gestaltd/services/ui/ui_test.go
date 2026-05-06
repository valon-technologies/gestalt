package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustWriteFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func mustServe(t *testing.T, handler http.Handler, path string) (int, string) {
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

func mustServeRequest(t *testing.T, handler http.Handler, path string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestDirHandler_ServesFilesAndFallbacks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		files map[string]string
		path  string
		want  string
	}{
		{
			name:  "serves index html",
			files: map[string]string{"index.html": "<html>home</html>"},
			path:  "/",
			want:  "home",
		},
		{
			name: "serves exact file",
			files: map[string]string{
				"index.html": "<html>home</html>",
				"style.css":  "body { color: red; }",
			},
			path: "/style.css",
			want: "color: red",
		},
		{
			name: "html suffix fallback",
			files: map[string]string{
				"index.html": "<html>home</html>",
				"about.html": "<html>about page</html>",
			},
			path: "/about",
			want: "about page",
		},
		{
			name:  "spa fallback for unknown path",
			files: map[string]string{"index.html": "<html>spa-root</html>"},
			path:  "/nonexistent",
			want:  "spa-root",
		},
		{
			name: "static asset in subdirectory",
			files: map[string]string{
				"index.html":    "<html>home</html>",
				"assets/app.js": "console.log('app');",
			},
			path: "/assets/app.js",
			want: "console.log",
		},
		{
			name: "html fallback preferred over spa",
			files: map[string]string{
				"index.html":    "<html>home</html>",
				"settings.html": "<html>settings</html>",
			},
			path: "/settings",
			want: "settings",
		},
		{
			name: "html fallback preferred over directory",
			files: map[string]string{
				"index.html":                    "<html>home</html>",
				"integrations.html":             "<html>integrations</html>",
				"integrations/__next._full.txt": "metadata",
			},
			path: "/integrations",
			want: "integrations",
		},
		{
			name: "directory index fallback",
			files: map[string]string{
				"index.html":         "<html>home</html>",
				"reports/index.html": "<html>reports</html>",
			},
			path: "/reports",
			want: "reports",
		},
		{
			name: "directory index fallback with trailing slash",
			files: map[string]string{
				"index.html":         "<html>home</html>",
				"reports/index.html": "<html>reports</html>",
			},
			path: "/reports/",
			want: "reports",
		},
		{
			name: "directory index file served without root fallback",
			files: map[string]string{
				"index.html":         "<html>home</html>",
				"reports/index.html": "<html>reports</html>",
			},
			path: "/reports/index.html",
			want: "reports",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			for rel, content := range tc.files {
				mustWriteFile(t, dir, rel, content)
			}

			handler, err := DirHandler(dir)
			if err != nil {
				t.Fatal(err)
			}

			code, body := mustServe(t, handler, tc.path)
			if code != http.StatusOK {
				t.Fatalf("status = %d, want %d", code, http.StatusOK)
			}
			if !strings.Contains(body, tc.want) {
				t.Fatalf("body = %q, want content containing %q", body, tc.want)
			}
		})
	}
}

func TestDirHandler_DoesNotServeParentPathElements(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	assetDir := filepath.Join(parent, "assets")
	mustWriteFile(t, assetDir, "index.html", "<html>home</html>")
	mustWriteFile(t, parent, "secret.html", "<html>secret</html>")

	handler, err := DirHandler(assetDir)
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/../secret.html", "/../secret"} {
		code, body := mustServeRequest(t, handler, path)
		if code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d", path, code, http.StatusOK)
		}
		if strings.Contains(body, "secret") {
			t.Fatalf("%s body = %q, should not include parent file content", path, body)
		}
	}
}

func TestDirHandler_RejectsDirectoryWithoutIndex(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustWriteFile(t, dir, "style.css", "body {}")

	_, err := DirHandler(dir)
	if err == nil {
		t.Fatal("expected error for directory without index.html")
	}
}
