package server

import (
	"bytes"
	"fmt"
	"html"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/ui"
)

type AppVersionReporter interface {
	RunningVersion(app string) string
}

func mountedAppStaticsFromEntries(apps map[string]*config.ProviderEntry, devHandlerResolver func(string) http.Handler, artifactsDir string, versions AppVersionReporter) ([]MountedUI, error) {
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

		if entry.Source.IsRegistry() {
			mounted = append(mounted, MountedUI{
				Name:                mountedName,
				Path:                mount,
				AppName:             name,
				AuthorizationPolicy: entry.AuthorizationPolicy,
				AppLevelAuth:        !entry.Static.Public,
				Handler:             registryAppStaticHandler(name, entry, mount, artifactsDir, versions),
				ThemeStylesheet:     entry.ResolvedThemeStylesheet,
				ThemeAssetsDir:      entry.ResolvedThemeAssetsDir,
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
			AppLevelAuth:        !entry.Static.Public,
			Handler:             handler,
			ThemeStylesheet:     entry.ResolvedThemeStylesheet,
			ThemeAssetsDir:      entry.ResolvedThemeAssetsDir,
		})
	}

	return mounted, nil
}

func registryAppStaticHandler(appName string, entry *config.ProviderEntry, mount, artifactsDir string, versions AppVersionReporter) http.Handler {
	var mu sync.Mutex
	var cachedVersion string
	var cachedHandler http.Handler
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if versions == nil || strings.TrimSpace(artifactsDir) == "" {
			http.Error(w, "registry app runtime is unavailable", http.StatusServiceUnavailable)
			return
		}
		version := strings.TrimSpace(versions.RunningVersion(appName))
		if version == "" {
			http.Error(w, "registry app is not running", http.StatusServiceUnavailable)
			return
		}

		mu.Lock()
		handler := cachedHandler
		if handler == nil || cachedVersion != version {
			resolved, err := appregistry.ResolveInstalledApp(
				appName,
				entry,
				appregistry.MaterializedPath(artifactsDir, appName, version),
				version,
			)
			if err != nil {
				mu.Unlock()
				http.Error(w, "registry app artifacts are unavailable", http.StatusServiceUnavailable)
				return
			}
			handler, err = ui.StaticHandler(ui.StaticConfig{
				FS:           os.DirFS(resolved.ResolvedStaticRoot),
				DynamicIndex: true,
				RenderIndex:  injectBaseHref(mount),
			})
			if err != nil {
				mu.Unlock()
				http.Error(w, "registry app static handler is unavailable", http.StatusInternalServerError)
				return
			}
			cachedVersion = version
			cachedHandler = handler
		}
		mu.Unlock()

		if strings.TrimSpace(versions.RunningVersion(appName)) != version {
			http.Error(w, "registry app version changed during request", http.StatusServiceUnavailable)
			return
		}
		handler.ServeHTTP(w, r)
	})
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
