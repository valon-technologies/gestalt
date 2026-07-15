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
			RenderIndex:  injectAppContext(name, mount),
		})
		if err != nil {
			return nil, fmt.Errorf("app %q static: %w", name, err)
		}

		mounted = append(mounted, MountedUI{
			Name:                mountedName,
			Path:                mount,
			AppName:             name,
			AuthorizationPolicy: entry.AuthorizationPolicy,
			AppLevelAuth:        !entry.Static.Public,
			Handler:             handler,
			ThemeStylesheet:     entry.ResolvedThemeStylesheet,
			ThemeAssetsDir:      entry.ResolvedThemeAssetsDir,
		})
	}

	return mounted, nil
}

func injectAppContext(appName, mount string) func([]byte) []byte {
	appName = strings.TrimSpace(appName)
	mount = strings.TrimSpace(mount)
	baseHref := "/"
	if mount != "" && mount != "/" {
		baseHref = mount + "/"
	}
	appMeta := []byte(
		"<meta name=\"gestalt-app-name\" content=\"" + html.EscapeString(appName) + "\">",
	)
	baseTag := []byte("<base href=\"" + html.EscapeString(baseHref) + "\">")

	return func(data []byte) []byte {
		data = injectIntoHTMLHead(data, appMeta)
		if !hasHTMLBaseTag(data) {
			data = injectIntoHTMLHead(data, baseTag)
		}
		return data
	}
}

func injectIntoHTMLHead(data, tag []byte) []byte {
	lower := bytes.ToLower(data)
	for searchFrom := 0; searchFrom < len(lower); {
		headIdx := bytes.Index(lower[searchFrom:], []byte("<head"))
		if headIdx < 0 {
			break
		}
		headIdx += searchFrom
		if insertAt, ok := htmlTagInsertAt(data, lower, headIdx); ok {
			out := make([]byte, 0, len(data)+len(tag))
			out = append(out, data[:insertAt]...)
			out = append(out, tag...)
			out = append(out, data[insertAt:]...)
			return out
		}
		searchFrom = headIdx + 5
	}
	if htmlIdx := bytes.Index(lower, []byte("<html")); htmlIdx >= 0 {
		if insertAt, ok := htmlTagInsertAt(data, lower, htmlIdx); ok {
			out := make([]byte, 0, len(data)+len(tag))
			out = append(out, data[:insertAt]...)
			out = append(out, tag...)
			out = append(out, data[insertAt:]...)
			return out
		}
	}
	return append(append([]byte(nil), tag...), data...)
}

func htmlTagInsertAt(data, lower []byte, tagIdx int) (int, bool) {
	if tagIdx+5 >= len(lower) {
		return 0, false
	}
	switch lower[tagIdx+5] {
	case '>', ' ', '\t', '\n', '\r':
		closeIdx := bytes.IndexByte(data[tagIdx:], '>')
		if closeIdx < 0 {
			return 0, false
		}
		return tagIdx + closeIdx + 1, true
	default:
		return 0, false
	}
}

func hasHTMLBaseTag(data []byte) bool {
	lower := bytes.ToLower(data)
	return bytes.Contains(lower, []byte("<base"))
}
