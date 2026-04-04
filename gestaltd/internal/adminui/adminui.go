package adminui

import (
	"embed"
	"net/http"

	"github.com/valon-technologies/gestalt/server/internal/webui"
)

//go:embed all:out
var assets embed.FS

func EmbeddedHandler() http.Handler {
	return webui.SubdirHandler(assets, "out")
}
