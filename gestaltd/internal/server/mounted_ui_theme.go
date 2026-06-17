package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"mime"
	"net/http"
	"os"
	stdpath "path"
	"strings"
	"time"
)

const (
	mountedUIThemeStylesheetPath = "/theme.css"
	mountedUIThemeAssetsPrefix   = "/theme/"
)

// mountedUIThemeHandler intercepts the deployment theme endpoints ahead of
// the static handler, so its SPA fallback can never answer /theme.css with
// index.html as text/html. An unconfigured mount serves an empty stylesheet
// (200, text/css) instead of falling through; /theme/* falls through unless
// an assets directory is configured.
func mountedUIThemeHandlerFullPath(mounted MountedUI, next http.Handler) http.Handler {
	stylesheetPath, assetsPrefix := mountedUIThemePaths(mounted.Path)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		switch {
		case r.URL.Path == stylesheetPath:
			serveMountedUIThemeStylesheet(w, r, mounted.ThemeStylesheet)
		case mounted.ThemeAssetsDir != "" && strings.HasPrefix(r.URL.Path, assetsPrefix):
			serveMountedUIThemeAsset(w, r, mounted.ThemeAssetsDir)
		default:
			next.ServeHTTP(w, r)
		}
	})
}

func mountedUIThemePaths(mountPath string) (stylesheetPath, assetsPrefix string) {
	mountPath = strings.TrimRight(mountPath, "/")
	if mountPath == "" {
		return mountedUIThemeStylesheetPath, mountedUIThemeAssetsPrefix
	}
	return mountPath + mountedUIThemeStylesheetPath, mountPath + mountedUIThemeAssetsPrefix
}

func mountedUIThemeHandler(mounted MountedUI, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		switch {
		case r.URL.Path == mountedUIThemeStylesheetPath:
			serveMountedUIThemeStylesheet(w, r, mounted.ThemeStylesheet)
		case mounted.ThemeAssetsDir != "" && strings.HasPrefix(r.URL.Path, mountedUIThemeAssetsPrefix):
			serveMountedUIThemeAsset(w, r, mounted.ThemeAssetsDir)
		default:
			next.ServeHTTP(w, r)
		}
	})
}

func serveMountedUIThemeStylesheet(w http.ResponseWriter, r *http.Request, stylesheet string) {
	body := []byte(nil)
	if stylesheet != "" {
		data, err := os.ReadFile(stylesheet)
		if err != nil {
			http.Error(w, "failed to read theme stylesheet", http.StatusInternalServerError)
			return
		}
		body = data
	}
	serveMountedUIThemeContent(w, r, "text/css; charset=utf-8", body)
}

func serveMountedUIThemeAsset(w http.ResponseWriter, r *http.Request, assetsDir string) {
	assetPath, ok := cleanMountedUIThemeAssetPath(strings.TrimPrefix(r.URL.Path, mountedUIThemeAssetsPrefix))
	if !ok {
		http.NotFound(w, r)
		return
	}
	fsys := os.DirFS(assetsDir)
	info, err := fs.Stat(fsys, assetPath)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	body, err := fs.ReadFile(fsys, assetPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ext := strings.ToLower(stdpath.Ext(assetPath))
	contentType := mountedUIThemeContentTypes[ext]
	if contentType == "" {
		contentType = mime.TypeByExtension(ext)
	}
	if contentType == "" {
		contentType = http.DetectContentType(body)
	}
	serveMountedUIThemeContent(w, r, contentType, body)
}

// mountedUIThemeContentTypes covers font types explicitly: Go's builtin mime
// table lacks .woff2, and OS mime databases are absent in minimal containers
// — while delivering licensed fonts is the main reason assetsDir exists.
var mountedUIThemeContentTypes = map[string]string{
	".woff2": "font/woff2",
	".woff":  "font/woff",
}

// cleanMountedUIThemeAssetPath guards /theme/ requests against path
// traversal, mirroring the static handler's cleanStaticPath: backslashes,
// parent path elements, and anything fs.ValidPath rejects are refused.
func cleanMountedUIThemeAssetPath(requestPath string) (string, bool) {
	if strings.Contains(requestPath, "\\") {
		return "", false
	}
	for _, part := range strings.Split(requestPath, "/") {
		if part == ".." {
			return "", false
		}
	}
	cleaned := stdpath.Clean(strings.TrimPrefix(requestPath, "/"))
	if cleaned == "." || cleaned == "" {
		return "", false
	}
	return cleaned, fs.ValidPath(cleaned)
}

// serveMountedUIThemeContent serves theme bytes with a content-hash ETag and
// Cache-Control: no-cache — the artifact's /theme.css href is frozen, so
// revalidation (If-None-Match → 304) is the caching strategy.
func serveMountedUIThemeContent(w http.ResponseWriter, r *http.Request, contentType string, body []byte) {
	sum := sha256.Sum256(body)
	header := w.Header()
	header.Set("Content-Type", contentType)
	header.Set("Cache-Control", "no-cache")
	header.Set("ETag", `"`+hex.EncodeToString(sum[:])+`"`)
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
}
