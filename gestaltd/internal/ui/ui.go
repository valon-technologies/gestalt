// Package ui serves the client UI static assets from a prepared directory.
package ui

import (
	"fmt"
	"net/http"
	"os"
)

func DirHandler(path string) (http.Handler, error) {
	handler, err := StaticHandler(StaticConfig{
		FS:           os.DirFS(path),
		DynamicIndex: true,
	})
	if err != nil {
		return nil, fmt.Errorf("ui: %s: %w", path, err)
	}
	return handler, nil
}
