package server

import (
	"bytes"
	"fmt"
	"html"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/ui"
)

func mountedAppStaticsFromEntries(apps map[string]*config.ProviderEntry, devHandlerResolver func(string) http.Handler) ([]MountedUI, error) {
	names := make([]string, 0, len(apps))
	for name, entry := range apps {
		if entry == nil || entry.Static == nil {
			continue
		}
		names = append(names, name)
	}
	slices.Sort(names)

	mounted := make([]MountedUI, 0, len(names))
	for _, name := range names {
		entry := apps[name]
		mount := strings.TrimSpace(entry.Static.Mount)
		if mount == "" {
			return nil, fmt.Errorf("app %q static mount is required", name)
		}
		mountedName := "app:" + name

		if entry.DevActive {
			handler := lazyDevHandler(devHandlerResolver, name)
			mounted = append(mounted, MountedUI{
				Name:                mountedName,
				Path:                mount,
				AppName:             name,
				AuthorizationPolicy: entry.AuthorizationPolicy,
				AppLevelAuth:        !entry.Static.Public,
				Handler:             handler,
				ThemeStylesheet:     entry.ResolvedThemeStylesheet,
				ThemeAssetsDir:      entry.ResolvedThemeAssetsDir,
				IsDev:               true,
			})
			continue
		}

		if strings.TrimSpace(entry.ResolvedStaticRoot) == "" {
			return nil, fmt.Errorf("app %q static configured but asset root not resolved", name)
		}

		handler, err := ui.StaticHandler(ui.StaticConfig{
			FS:           os.DirFS(entry.ResolvedStaticRoot),
			DynamicIndex: true,
			RenderIndex:  injectBaseHref(mount),
		})
		if err != nil {
			return nil, fmt.Errorf("app %q static: %w", name, err)
		}

		mounted = append(mounted, MountedUI{
			Name:                mountedName,
			Path:                mount,
			AppName:             name,
			AuthorizationPolicy: entry.AuthorizationPolicy,
			AppLevelAuth:        !public,
			Handler:             handler,
			ThemeStylesheet:     entry.ResolvedThemeStylesheet,
			ThemeAssetsDir:      entry.ResolvedThemeAssetsDir,
		})
	}

	return mounted, nil
}

func injectBaseHref(mount string) func([]byte) []byte {
	mount = strings.TrimSpace(mount)
	baseHref := "/"
	if mount != "" && mount != "/" {
		baseHref = mount + "/"
	}
	tag := []byte("<base href=\"" + html.EscapeString(baseHref) + "\">")
	return func(data []byte) []byte {
		if hasHTMLBaseTag(data) {
			return data
		}
		lower := bytes.ToLower(data)
		if idx := bytes.Index(lower, []byte("<head>")); idx >= 0 {
			insertAt := idx + len("<head>")
			out := make([]byte, 0, len(data)+len(tag))
			out = append(out, data[:insertAt]...)
			out = append(out, tag...)
			out = append(out, data[insertAt:]...)
			return out
		}
		if idx := bytes.Index(lower, []byte("<html>")); idx >= 0 {
			insertAt := idx + len("<html>")
			out := make([]byte, 0, len(data)+len(tag))
			out = append(out, data[:insertAt]...)
			out = append(out, tag...)
			out = append(out, data[insertAt:]...)
			return out
		}
		return append(append([]byte(nil), tag...), data...)
	}
}

func hasHTMLBaseTag(data []byte) bool {
	lower := bytes.ToLower(data)
	return bytes.Contains(lower, []byte("<base"))
}
