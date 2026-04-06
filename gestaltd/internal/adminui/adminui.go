package adminui

import (
	"bytes"
	"embed"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"time"
)

//go:embed all:out
var assets embed.FS

type Options struct {
	BrandHref    string
	ClientUIHref string
}

func EmbeddedHandler(opts Options) http.Handler {
	handler, err := subdirHandler(assets, "out", opts)
	if err != nil {
		return nil
	}
	return handler
}

func DirHandler(path string, opts Options) (http.Handler, error) {
	return newHandler(os.DirFS(path), opts)
}

func subdirHandler(root fs.FS, dir string, opts Options) (http.Handler, error) {
	sub, err := fs.Sub(root, dir)
	if err != nil {
		return nil, err
	}
	return newHandler(sub, opts)
}

func newHandler(root fs.FS, opts Options) (http.Handler, error) {
	if _, err := fs.Stat(root, "index.html"); err != nil {
		return nil, fmt.Errorf("admin ui asset root does not contain index.html")
	}

	indexHTML, err := fs.ReadFile(root, "index.html")
	if err != nil {
		return nil, fmt.Errorf("read admin ui index: %w", err)
	}
	renderedIndex := renderIndexHTML(indexHTML, opts)
	fileServer := http.FileServer(http.FS(root))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" && strings.Contains(path, ".") && path != "index.html" {
			if info, err := fs.Stat(root, path); err == nil && !info.IsDir() {
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(renderedIndex))
	}), nil
}

func renderIndexHTML(indexHTML []byte, opts Options) []byte {
	normalized := normalizeOptions(opts)
	replaced := strings.Replace(
		string(indexHTML),
		`<a class="brand" href="/">Gestalt</a>`,
		fmt.Sprintf(`<a class="brand" href="%s">Gestalt</a>`, html.EscapeString(normalized.BrandHref)),
		1,
	)

	clientUILink := ""
	if normalized.ClientUIHref != "" {
		clientUILink = fmt.Sprintf(`<a href="%s">Client UI</a>`, html.EscapeString(normalized.ClientUIHref))
	}
	replaced = strings.Replace(replaced, `<a href="/">Client UI</a>`, clientUILink, 1)
	return []byte(replaced)
}

func normalizeOptions(opts Options) Options {
	opts.BrandHref = strings.TrimSpace(opts.BrandHref)
	if opts.BrandHref == "" {
		opts.BrandHref = "/"
	}
	opts.ClientUIHref = strings.TrimSpace(opts.ClientUIHref)
	return opts
}
