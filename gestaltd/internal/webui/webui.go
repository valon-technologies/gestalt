// Package webui serves the client UI static assets.
//
// Build the client UI before go build:
//
//	cd gestaltd/ui && npm run build
//	cp -r gestaltd/ui/out gestaltd/internal/webui/out
package webui

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strings"
)

//go:embed all:out
var assets embed.FS

func NewHandler(root fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(root))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		if info, err := fs.Stat(root, path); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}

		if !strings.Contains(path, ".") {
			if _, err := fs.Stat(root, path+".html"); err == nil {
				serve(fileServer, w, r, "/"+path+".html")
				return
			}
			if _, err := fs.Stat(root, path+"/index.html"); err == nil {
				serve(fileServer, w, r, "/"+path+"/index.html")
				return
			}
		}

		serve(fileServer, w, r, "/index.html")
	})
}

func SubdirHandler(root fs.FS, dir string) http.Handler {
	sub, err := fs.Sub(root, dir)
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return NewHandler(sub)
}

func EmbeddedHandler() http.Handler {
	return SubdirHandler(assets, "out")
}

func DirHandler(path string) (http.Handler, error) {
	root := os.DirFS(path)
	if _, err := fs.Stat(root, "index.html"); err != nil {
		return nil, fmt.Errorf("webui asset root %s does not contain index.html", path)
	}
	return NewHandler(root), nil
}

func serve(h http.Handler, w http.ResponseWriter, r *http.Request, path string) {
	r2 := *r
	u := *r.URL
	u.Path = path
	r2.URL = &u
	h.ServeHTTP(w, &r2)
}
