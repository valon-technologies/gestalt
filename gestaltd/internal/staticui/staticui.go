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
	FS          fs.FS
	RenderIndex func([]byte) []byte
}

func Handler(cfg Config) (http.Handler, error) {
	if _, err := fs.Stat(cfg.FS, "index.html"); err != nil {
		return nil, fmt.Errorf("asset root does not contain index.html")
	}

	indexHTML, err := fs.ReadFile(cfg.FS, "index.html")
	if err != nil {
		return nil, fmt.Errorf("read index.html: %w", err)
	}

	renderedIndex := indexHTML
	if cfg.RenderIndex != nil {
		renderedIndex = cfg.RenderIndex(indexHTML)
	}

	fileServer := http.FileServer(http.FS(cfg.FS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		if info, err := fs.Stat(cfg.FS, path); err == nil && !info.IsDir() {
			if path == "index.html" {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(renderedIndex))
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

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(renderedIndex))
	}), nil
}

func serve(h http.Handler, w http.ResponseWriter, r *http.Request, path string) {
	r2 := *r
	u := *r.URL
	u.Path = path
	r2.URL = &u
	h.ServeHTTP(w, &r2)
}
