package adminui

import (
	"embed"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"os"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/staticui"
)

//go:embed all:out
var assets embed.FS

type Options struct {
	BrandHref string
}

func EmbeddedHandler(opts Options) http.Handler {
	sub, err := fs.Sub(assets, "out")
	if err != nil {
		return nil
	}
	handler, err := staticui.Handler(staticui.Config{
		FS:          sub,
		RenderIndex: renderFunc(opts),
	})
	if err != nil {
		return nil
	}
	return handler
}

func DirHandler(path string, opts Options) (http.Handler, error) {
	return staticui.Handler(staticui.Config{
		FS:          os.DirFS(path),
		RenderIndex: renderFunc(opts),
	})
}

func renderFunc(opts Options) func([]byte) []byte {
	return func(indexHTML []byte) []byte {
		return renderIndexHTML(indexHTML, opts)
	}
}

func renderIndexHTML(indexHTML []byte, opts Options) []byte {
	normalized := normalizeOptions(opts)
	replaced := strings.Replace(
		string(indexHTML),
		`<a class="brand" href="/">Gestalt</a>`,
		fmt.Sprintf(`<a class="brand" href=%q>Gestalt</a>`, html.EscapeString(normalized.BrandHref)),
		1,
	)

	replaced = strings.Replace(replaced, `<a href="/">Client UI</a>`, "", 1)
	return []byte(replaced)
}

func normalizeOptions(opts Options) Options {
	opts.BrandHref = strings.TrimSpace(opts.BrandHref)
	if opts.BrandHref == "" {
		opts.BrandHref = "/"
	}
	return opts
}
