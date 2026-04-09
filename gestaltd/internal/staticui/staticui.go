package staticui

import (
	"bytes"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"
)

type Config struct {
	FS           fs.FS
	RenderIndex  func([]byte) []byte
	DynamicIndex bool
}

func Handler(cfg Config) (http.Handler, error) {
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

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		if info, err := fs.Stat(cfg.FS, path); err == nil && !info.IsDir() {
			if path == "index.html" {
				serveIndex(w, r, readIndex)
				return
			}
			fileServer.ServeHTTP(w, r)
			return
		}

		if !strings.Contains(path, ".") {
			if _, err := fs.Stat(cfg.FS, path+".html"); err == nil {
				serve(fileServer, w, r, "/"+path+".html")
				return
			}
			if _, err := fs.Stat(cfg.FS, path+"/index.html"); err == nil {
				serve(fileServer, w, r, "/"+path+"/index.html")
				return
			}
		}

		serveIndex(w, r, readIndex)
	}), nil
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

func serve(h http.Handler, w http.ResponseWriter, r *http.Request, path string) {
	r2 := *r
	u := *r.URL
	u.Path = path
	r2.URL = &u
	h.ServeHTTP(w, &r2)
}
