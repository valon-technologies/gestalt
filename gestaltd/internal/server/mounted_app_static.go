package server

import (
	"bytes"
	"fmt"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
	"github.com/valon-technologies/gestalt/server/services/ui"
)

func mountedAppStaticsFromEntries(apps map[string]*config.ProviderEntry, providers *registry.ProviderMap[core.Provider], artifactsDir string, devHandlerResolver func(string) http.Handler) ([]MountedUI, error) {
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
				Handler:             registryAppStaticHandler(name, mount, entry, providers, artifactsDir),
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

func registryAppStaticHandler(app, mount string, entry *config.ProviderEntry, providers *registry.ProviderMap[core.Provider], artifactsDir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if providers == nil || strings.TrimSpace(artifactsDir) == "" {
			http.Error(w, "app unavailable", http.StatusServiceUnavailable)
			return
		}
		if _, err := providers.Get(app); err != nil {
			http.Error(w, "app unavailable", http.StatusServiceUnavailable)
			return
		}
		marker := filepath.Join(artifactsDir, appregistry.RegistryInstallSubdir, app, "active-version")
		raw, err := os.ReadFile(marker)
		version := strings.TrimSpace(string(raw))
		if err != nil || version == "" {
			http.Error(w, "app unavailable", http.StatusServiceUnavailable)
			return
		}
		destDir := appregistry.MaterializedPath(artifactsDir, app, version)
		resolved, err := appregistry.ResolveInstalledApp(app, entry, destDir, version)
		if err != nil || strings.TrimSpace(resolved.ResolvedStaticRoot) == "" {
			http.Error(w, "app unavailable", http.StatusServiceUnavailable)
			return
		}
		handler, err := ui.StaticHandler(ui.StaticConfig{
			FS:           os.DirFS(resolved.ResolvedStaticRoot),
			DynamicIndex: true,
			RenderIndex:  injectBaseHref(mount),
		})
		if err != nil {
			http.Error(w, "app unavailable", http.StatusServiceUnavailable)
			return
		}
		again, err := os.ReadFile(marker)
		if err != nil || strings.TrimSpace(string(again)) != version {
			http.Error(w, "app unavailable", http.StatusServiceUnavailable)
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
