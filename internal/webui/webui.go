// Package webui serves the embedded frontend static assets.
//
// Build the frontend before go build:
//
//	cd web && npm run build
//	cp -r web/out internal/webui/out
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:out
var assets embed.FS

// Handler returns an http.Handler that serves the embedded frontend,
// or nil if the frontend has not been built.
func Handler() http.Handler {
	sub, err := fs.Sub(assets, "out")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}

	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		if _, err := fs.Stat(sub, path); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}

		// Try path.html and path/index.html for Next.js static export routes.
		if !strings.Contains(path, ".") {
			if _, err := fs.Stat(sub, path+".html"); err == nil {
				serve(fileServer, w, r, "/"+path+".html")
				return
			}
			if _, err := fs.Stat(sub, path+"/index.html"); err == nil {
				serve(fileServer, w, r, "/"+path+"/index.html")
				return
			}
		}

		serve(fileServer, w, r, "/index.html")
	})
}

func serve(h http.Handler, w http.ResponseWriter, r *http.Request, path string) {
	r2 := *r
	u := *r.URL
	u.Path = path
	r2.URL = &u
	h.ServeHTTP(w, &r2)
}
