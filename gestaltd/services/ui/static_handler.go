package ui

import (
	"bytes"
	"fmt"
	"io/fs"
	"net/http"
	stdpath "path"
	"strings"
	"time"
)

type StaticConfig struct {
	FS           fs.FS
	RenderIndex  func([]byte) []byte
	DynamicIndex bool
}

func StaticHandler(cfg StaticConfig) (http.Handler, error) {
	if _, err := fs.Stat(cfg.FS, "index.html"); err != nil {
		return nil, fmt.Errorf("asset root does not contain index.html: %w", err)
	}

	var cachedIndex []byte
	if !cfg.DynamicIndex {
		indexHTML, err := fs.ReadFile(cfg.FS, "index.html")
		if err != nil {
			return nil, fmt.Errorf("read index.html: %w", err)
		}
		cachedIndex = indexHTML
		if cfg.RenderIndex != nil {
			cachedIndex = cfg.RenderIndex(indexHTML)
		}
	}

	readIndex := func() ([]byte, error) {
		if cachedIndex != nil {
			return cachedIndex, nil
		}
		data, err := fs.ReadFile(cfg.FS, "index.html")
		if err != nil {
			return nil, err
		}
		if cfg.RenderIndex != nil {
			data = cfg.RenderIndex(data)
		}
		return data, nil
	}

	fileServer := http.FileServer(http.FS(cfg.FS))

	return &handler{
		fs:         cfg.FS,
		fileServer: fileServer,
		readIndex:  readIndex,
	}, nil
}

type handler struct {
	fs         fs.FS
	fileServer http.Handler
	readIndex  func() ([]byte, error)
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	resolution := h.resolve(strings.TrimPrefix(r.URL.Path, "/"))
	switch {
	case resolution.serveIndex:
		serveIndex(w, r, h.readIndex)
	case resolution.servePath != "" && isNavigationHTML(resolution.servePath):
		serveFile(w, r, h.fs, resolution.servePath)
	case resolution.servePath != "":
		serve(h.fileServer, w, r, resolution.servePath)
	default:
		serveIndex(w, r, h.readIndex)
	}
}

func (h *handler) NavigationPathForRequest(requestPath string) (string, bool) {
	resolution := h.resolve(strings.TrimPrefix(requestPath, "/"))
	return resolution.routePath, resolution.navigation
}

type requestResolution struct {
	navigation bool
	routePath  string
	serveIndex bool
	servePath  string
}

func (h *handler) resolve(path string) requestResolution {
	path, ok := cleanStaticPath(path)
	if !ok {
		return requestResolution{
			navigation: true,
			routePath:  "/",
			serveIndex: true,
		}
	}

	if info, err := fs.Stat(h.fs, path); err == nil && !info.IsDir() {
		if routePath, ok := navigationRoutePath(path); ok {
			if path == "index.html" {
				return requestResolution{navigation: true, routePath: routePath, serveIndex: true}
			}
			return requestResolution{navigation: true, routePath: routePath, servePath: "/" + path}
		}
		return requestResolution{servePath: "/" + path}
	}

	routePath := strings.TrimRight(path, "/")
	if routePath == "" {
		routePath = "index.html"
	}
	if !strings.Contains(routePath, ".") {
		if _, err := fs.Stat(h.fs, routePath+".html"); err == nil {
			return requestResolution{
				navigation: true,
				routePath:  cleanRoutePath("/" + routePath),
				servePath:  "/" + routePath + ".html",
			}
		}
		if _, err := fs.Stat(h.fs, routePath+"/index.html"); err == nil {
			return requestResolution{
				navigation: true,
				routePath:  cleanRoutePath("/" + routePath),
				servePath:  "/" + routePath + "/index.html",
			}
		}
	}

	return requestResolution{
		navigation: true,
		routePath:  cleanRoutePath("/" + path),
		serveIndex: true,
	}
}

func navigationRoutePath(path string) (string, bool) {
	switch {
	case path == "index.html":
		return "/", true
	case strings.HasSuffix(path, "/index.html"):
		return cleanRoutePath("/" + strings.TrimSuffix(path, "/index.html")), true
	case strings.HasSuffix(path, ".html"):
		return cleanRoutePath("/" + strings.TrimSuffix(path, ".html")), true
	default:
		return "", false
	}
}

func isNavigationHTML(path string) bool {
	_, ok := navigationRoutePath(strings.TrimPrefix(path, "/"))
	return ok
}

func cleanStaticPath(path string) (string, bool) {
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		return "index.html", true
	}
	if strings.Contains(path, "\\") {
		return "", false
	}
	if hasParentPathElement(path) {
		return "", false
	}
	path = stdpath.Clean(path)
	if path == "." {
		return "index.html", true
	}
	path = strings.TrimPrefix(path, "/")
	return path, fs.ValidPath(path)
}

func hasParentPathElement(path string) bool {
	for _, part := range strings.Split(path, "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

func cleanRoutePath(path string) string {
	path = stdpath.Clean(path)
	if path == "." {
		return "/"
	}
	return path
}

func serveIndex(w http.ResponseWriter, r *http.Request, readIndex func() ([]byte, error)) {
	data, err := readIndex()
	if err != nil {
		http.Error(w, "failed to read index.html", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(data))
}

func serveFile(w http.ResponseWriter, r *http.Request, fsys fs.FS, path string) {
	path, ok := cleanStaticPath(path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, stdpath.Base(path), time.Time{}, bytes.NewReader(data))
}

func serve(h http.Handler, w http.ResponseWriter, r *http.Request, path string) {
	r2 := *r
	u := *r.URL
	u.Path = path
	r2.URL = &u
	h.ServeHTTP(w, &r2)
}
