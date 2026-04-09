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

	"github.com/valon-technologies/gestalt/server/internal/staticui"
)

//go:embed all:out
var assets embed.FS

func EmbeddedHandler() http.Handler {
	sub, err := fs.Sub(assets, "out")
	if err != nil {
		return nil
	}
	handler, err := staticui.Handler(staticui.Config{FS: sub})
	if err != nil {
		return nil
	}
	return handler
}

func DirHandler(path string) (http.Handler, error) {
	root := os.DirFS(path)
	handler, err := staticui.Handler(staticui.Config{FS: root})
	if err != nil {
		return nil, fmt.Errorf("webui asset root %s does not contain index.html", path)
	}
	return handler, nil
}
