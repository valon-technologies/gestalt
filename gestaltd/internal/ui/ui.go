// Package ui serves the client UI static assets from a prepared directory.
package ui

import (
	"fmt"
	"net/http"
	"os"

	"github.com/valon-technologies/gestalt/server/internal/staticui"
)

func DirHandler(path string) (http.Handler, error) {
	handler, err := staticui.Handler(staticui.Config{
		FS:           os.DirFS(path),
		DynamicIndex: true,
	})
	if err != nil {
		return nil, fmt.Errorf("ui: %s: %w", path, err)
	}
	return handler, nil
}
