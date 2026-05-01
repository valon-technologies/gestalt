package adminui

import (
	"embed"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/valon-technologies/gestalt/server/services/ui"
)

//go:embed all:out
var assets embed.FS

type Options struct {
	BrandHref string
	LoginBase string
}

func EmbeddedHandler(opts Options) http.Handler {
	sub, err := fs.Sub(assets, "out")
	if err != nil {
		return nil
	}
	handler, err := ui.StaticHandler(ui.StaticConfig{
		FS:          sub,
		RenderIndex: renderFunc(opts),
	})
	if err != nil {
		return nil
	}
	return handler
}

func DirHandler(path string, opts Options) (http.Handler, error) {
	return ui.StaticHandler(ui.StaticConfig{
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
	replaced = strings.Replace(replaced, `__GESTALT_ADMIN_LOGIN_BASE__`, strconv.Quote(normalized.LoginBase), 1)
	return []byte(replaced)
}

func normalizeOptions(opts Options) Options {
	opts.BrandHref = strings.TrimSpace(opts.BrandHref)
	if opts.BrandHref == "" {
		opts.BrandHref = "/"
	}
	opts.LoginBase = strings.TrimSpace(opts.LoginBase)
	if opts.LoginBase == "" {
		opts.LoginBase = "/api/v1/auth/login"
	}
	return opts
}
