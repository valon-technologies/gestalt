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

func TestDirHandler_ServesIndexHTML(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustWriteFile(t, dir, "index.html", "<html>home</html>")

	handler, err := DirHandler(dir)
	if err != nil {
		t.Fatal(err)
	}

	code, body := mustServe(t, handler, "/")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if !strings.Contains(body, "home") {
		t.Fatalf("body = %q, want home content", body)
	}
}

func TestDirHandler_ServesExactFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustWriteFile(t, dir, "index.html", "<html>home</html>")
	mustWriteFile(t, dir, "style.css", "body { color: red; }")

	handler, err := DirHandler(dir)
	if err != nil {
		t.Fatal(err)
	}

	code, body := mustServe(t, handler, "/style.css")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if !strings.Contains(body, "color: red") {
		t.Fatalf("body = %q, want CSS content", body)
	}
}

func TestDirHandler_HTMLSuffixFallback(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustWriteFile(t, dir, "index.html", "<html>home</html>")
	mustWriteFile(t, dir, "about.html", "<html>about page</html>")

	handler, err := DirHandler(dir)
	if err != nil {
		t.Fatal(err)
	}

	code, body := mustServe(t, handler, "/about")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if !strings.Contains(body, "about page") {
		t.Fatalf("body = %q, want about page content", body)
	}
}

func TestDirHandler_SPAFallbackForUnknownPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustWriteFile(t, dir, "index.html", "<html>spa-root</html>")

	handler, err := DirHandler(dir)
	if err != nil {
		t.Fatal(err)
	}

	code, body := mustServe(t, handler, "/nonexistent")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if !strings.Contains(body, "spa-root") {
		t.Fatalf("body = %q, want SPA fallback", body)
	}
}

func TestDirHandler_StaticAssetInSubdirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustWriteFile(t, dir, "index.html", "<html>home</html>")
	mustWriteFile(t, dir, "assets/app.js", "console.log('app');")

	handler, err := DirHandler(dir)
	if err != nil {
		t.Fatal(err)
	}

	code, body := mustServe(t, handler, "/assets/app.js")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if !strings.Contains(body, "console.log") {
		t.Fatalf("body = %q, want JS content", body)
	}
}

func TestDirHandler_HTMLFallbackPreferredOverSPA(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustWriteFile(t, dir, "index.html", "<html>home</html>")
	mustWriteFile(t, dir, "settings.html", "<html>settings</html>")

	handler, err := DirHandler(dir)
	if err != nil {
		t.Fatal(err)
	}

	code, body := mustServe(t, handler, "/settings")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if !strings.Contains(body, "settings") {
		t.Fatalf("body = %q, want settings content", body)
	}
}

func TestDirHandler_HTMLFallbackPreferredOverDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustWriteFile(t, dir, "index.html", "<html>home</html>")
	mustWriteFile(t, dir, "integrations.html", "<html>integrations</html>")
	mustWriteFile(t, dir, "integrations/__next._full.txt", "metadata")

	handler, err := DirHandler(dir)
	if err != nil {
		t.Fatal(err)
	}

	code, body := mustServe(t, handler, "/integrations")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if !strings.Contains(body, "integrations") {
		t.Fatalf("body = %q, want integrations content", body)
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
