package server

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/plugins/providerpkg"
	"github.com/valon-technologies/gestalt/server/services/ui"
	"github.com/valon-technologies/gestalt/server/services/ui/adminui"
)

func normalizeAdminRouteConfig(admin AdminRouteConfig) (AdminRouteConfig, error) {
	admin.AuthorizationPolicy = strings.TrimSpace(admin.AuthorizationPolicy)
	if admin.AuthorizationPolicy == "" {
		if len(admin.AllowedRoles) > 0 {
			return AdminRouteConfig{}, fmt.Errorf("admin allowedRoles requires AuthorizationPolicy")
		}
		admin.AllowedRoles = nil
		return admin, nil
	}
	if len(admin.AllowedRoles) == 0 {
		admin.AllowedRoles = []string{"admin"}
		return admin, nil
	}

	roles, err := providerpkg.NormalizeUIAllowedRoles("admin allowedRoles", admin.AllowedRoles)
	if err != nil {
		return AdminRouteConfig{}, err
	}
	admin.AllowedRoles = roles
	return admin, nil
}

func validateAdminRouteRuntime(admin AdminRouteConfig, noAuth bool, publicBaseURL, managementBaseURL string, routeProfile RouteProfile) error {
	if admin.AuthorizationPolicy == "" {
		return nil
	}
	if noAuth {
		return fmt.Errorf("admin authorization requires auth to be enabled")
	}
	if routeProfile == RouteProfileAll {
		if strings.TrimSpace(managementBaseURL) != "" {
			return fmt.Errorf("ManagementBaseURL requires RouteProfilePublic or RouteProfileManagement for admin authorization")
		}
		return nil
	}

	publicURL, err := parseAbsoluteBaseURL("PublicBaseURL", publicBaseURL)
	if err != nil {
		return err
	}
	managementURL, err := parseAbsoluteBaseURL("ManagementBaseURL", managementBaseURL)
	if err != nil {
		return err
	}
	if publicURL.Hostname() != managementURL.Hostname() {
		return fmt.Errorf("PublicBaseURL and ManagementBaseURL must use the same hostname for admin authorization")
	}
	if strings.EqualFold(publicURL.Scheme, "https") && !strings.EqualFold(managementURL.Scheme, "https") {
		return fmt.Errorf("ManagementBaseURL must use https when PublicBaseURL uses https for admin authorization")
	}
	return nil
}

func parseAbsoluteBaseURL(label, raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("%s is required", label)
	}
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" {
		return nil, fmt.Errorf("%s must be an absolute URL", label)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("%s may not include query or fragment", label)
	}
	return parsed, nil
}

func mountedUIsFromEntries(entries map[string]*config.UIEntry) ([]MountedUI, error) {
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	slices.Sort(names)

	mounted := make([]MountedUI, 0, len(names))
	for _, name := range names {
		entry := entries[name]
		if entry == nil {
			continue
		}
		if entry.ResolvedAssetRoot == "" {
			return nil, fmt.Errorf("ui %q configured but asset root not resolved", name)
		}

		handler, err := ui.DirHandler(entry.ResolvedAssetRoot)
		if err != nil {
			return nil, fmt.Errorf("ui %q: %w", name, err)
		}

		routes := []MountedUIRoute(nil)
		if spec := entry.ManifestSpec(); spec != nil && len(spec.Routes) > 0 {
			routes = make([]MountedUIRoute, 0, len(spec.Routes))
			for _, route := range spec.Routes {
				routes = append(routes, MountedUIRoute{
					Path:         route.Path,
					AllowedRoles: append([]string(nil), route.AllowedRoles...),
				})
			}
		}

		mounted = append(mounted, MountedUI{
			Name:                name,
			Path:                entry.Path,
			PluginName:          entry.OwnerPlugin,
			AuthorizationPolicy: entry.AuthorizationPolicy,
			Routes:              routes,
			Handler:             handler,
		})
	}

	return mounted, nil
}

func resolveBuiltinAdminUI(opts BuiltinAdminUIOptions) (http.Handler, error) {
	adminOpts := adminui.Options{
		BrandHref: opts.BrandHref,
		LoginBase: opts.LoginBase,
	}
	if dir := strings.TrimSpace(os.Getenv(adminUIDirEnv)); dir != "" {
		return adminui.DirHandler(dir, adminOpts)
	}
	handler := adminui.EmbeddedHandler(adminOpts)
	if handler == nil {
		return nil, fmt.Errorf("embedded admin ui assets not found")
	}
	return handler, nil
}

func resolveConfiguredAdminUI(opts BuiltinAdminUIOptions, providerName string, entries map[string]*config.UIEntry) (http.Handler, error) {
	entry, resolvedName, err := selectAdminUIProviderEntry(strings.TrimSpace(providerName), entries)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return nil, nil
	}

	adminDir := filepath.Join(entry.ResolvedAssetRoot, "admin")
	if _, err := os.Stat(filepath.Join(adminDir, "index.html")); err != nil {
		return nil, fmt.Errorf("ui.%s admin assets not found at %s: %w", resolvedName, adminDir, err)
	}

	handler, err := adminui.DirHandler(adminDir, adminui.Options{
		BrandHref: opts.BrandHref,
		LoginBase: opts.LoginBase,
	})
	if err != nil {
		return nil, fmt.Errorf("ui.%s admin assets: %w", resolvedName, err)
	}
	return handler, nil
}

func selectAdminUIProviderEntry(providerName string, entries map[string]*config.UIEntry) (*config.UIEntry, string, error) {
	if len(entries) == 0 {
		if providerName != "" {
			return nil, "", fmt.Errorf("server.admin.ui %q not found", providerName)
		}
		return nil, "", nil
	}

	if providerName != "" {
		entry := entries[providerName]
		if entry == nil {
			return nil, "", fmt.Errorf("server.admin.ui %q not found", providerName)
		}
		return entry, providerName, nil
	}

	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	slices.Sort(names)

	for _, name := range names {
		entry := entries[name]
		if entry == nil || strings.TrimSpace(entry.Path) != "/" {
			continue
		}
		if uiEntryHasAdminShell(entry) {
			return entry, name, nil
		}
	}

	return nil, "", nil
}

func uiEntryHasAdminShell(entry *config.UIEntry) bool {
	if entry == nil || strings.TrimSpace(entry.ResolvedAssetRoot) == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(entry.ResolvedAssetRoot, "admin", "index.html"))
	return err == nil && !info.IsDir()
}

func normalizeMountedUIs(mounted []MountedUI) ([]MountedUI, error) {
	if len(mounted) == 0 {
		return nil, nil
	}

	normalized := append([]MountedUI(nil), mounted...)
	for i := range normalized {
		routes, err := normalizeMountedUIRoutes(normalized[i].Routes)
		if err != nil {
			name := normalized[i].Name
			if name == "" {
				name = normalized[i].Path
			}
			return nil, fmt.Errorf("normalize mounted ui %q routes: %w", name, err)
		}
		normalized[i].Routes = routes
		if err := validatePolicyBoundMountedUIRoutes(normalized[i]); err != nil {
			name := normalized[i].Name
			if name == "" {
				name = normalized[i].Path
			}
			return nil, fmt.Errorf("normalize mounted ui %q routes: %w", name, err)
		}
	}
	return normalized, nil
}

func normalizeMountedUIRoutes(routes []MountedUIRoute) ([]MountedUIRoute, error) {
	if len(routes) == 0 {
		return nil, nil
	}

	normalized := append([]MountedUIRoute(nil), routes...)
	seenPaths := make(map[string]struct{}, len(normalized))
	for i := range normalized {
		routePath, err := providerpkg.NormalizeUIRoutePath(fmt.Sprintf("route %d path", i), normalized[i].Path)
		if err != nil {
			return nil, err
		}
		normalized[i].Path = routePath
		if _, exists := seenPaths[routePath]; exists {
			return nil, fmt.Errorf("route %d path %q duplicates another route", i, routePath)
		}
		seenPaths[routePath] = struct{}{}

		roles, err := providerpkg.NormalizeUIAllowedRoles(fmt.Sprintf("route %d allowedRoles", i), normalized[i].AllowedRoles)
		if err != nil {
			return nil, err
		}
		normalized[i].AllowedRoles = roles
	}

	slices.SortFunc(normalized, func(a, b MountedUIRoute) int {
		aLen, aWildcard := mountedUIRouteSpecificity(a.Path)
		bLen, bWildcard := mountedUIRouteSpecificity(b.Path)
		if aLen != bLen {
			return bLen - aLen
		}
		if aWildcard != bWildcard {
			if aWildcard {
				return 1
			}
			return -1
		}
		return strings.Compare(a.Path, b.Path)
	})
	return normalized, nil
}

func validatePolicyBoundMountedUIRoutes(mounted MountedUI) error {
	if mounted.AuthorizationPolicy == "" {
		return nil
	}
	if len(mounted.Routes) == 0 {
		return fmt.Errorf("policy-bound UIs must declare at least one route")
	}
	coversRoot := false
	for i := range mounted.Routes {
		if len(mounted.Routes[i].AllowedRoles) == 0 {
			return fmt.Errorf("route %q allowedRoles must not be empty", mounted.Routes[i].Path)
		}
		if providerpkg.UIRouteMatches(mounted.Routes[i].Path, "/") {
			coversRoot = true
		}
	}
	if !coversRoot {
		return fmt.Errorf("policy-bound UIs must declare a route covering /")
	}
	return nil
}
