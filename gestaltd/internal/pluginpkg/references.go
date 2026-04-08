package pluginpkg

import (
	"path/filepath"
	"strings"

	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

type LocalPackageReference struct {
	Path        string
	Description string
}

func LocalPackageReferences(manifest *pluginmanifestv1.Manifest) []LocalPackageReference {
	if manifest == nil {
		return nil
	}

	refs := make([]LocalPackageReference, 0, 3)
	seen := make(map[string]struct{}, 3)
	add := func(path, description string) {
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		refs = append(refs, LocalPackageReference{
			Path:        path,
			Description: description,
		})
	}

	if manifest.Plugin != nil {
		add(manifest.Plugin.ConfigSchemaPath, "provider config schema")
		if manifest.Plugin.OpenAPI != "" && !strings.Contains(manifest.Plugin.OpenAPI, "://") {
			add(manifest.Plugin.OpenAPI, "provider openapi document")
		}
		if manifest.Plugin.GraphQLURL != "" && !strings.Contains(manifest.Plugin.GraphQLURL, "://") {
			add(manifest.Plugin.GraphQLURL, "provider graphql document")
		}
		if manifest.Plugin.MCPURL != "" && !strings.Contains(manifest.Plugin.MCPURL, "://") {
			add(manifest.Plugin.MCPURL, "provider mcp document")
		}
	}
	add(manifest.IconFile, "icon_file")
	return refs
}

func ResolveManifestLocalReferences(manifest *pluginmanifestv1.Manifest, manifestPath string) *pluginmanifestv1.Manifest {
	if manifest == nil || manifest.Plugin == nil || manifestPath == "" {
		return manifest
	}

	resolve := func(value string) string {
		if value == "" || filepath.IsAbs(value) || strings.Contains(value, "://") {
			return value
		}
		return filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(value))
	}

	provider := *manifest.Plugin
	changed := false

	if resolved := resolve(provider.OpenAPI); resolved != provider.OpenAPI {
		provider.OpenAPI = resolved
		changed = true
	}
	if resolved := resolve(provider.GraphQLURL); resolved != provider.GraphQLURL {
		provider.GraphQLURL = resolved
		changed = true
	}
	if resolved := resolve(provider.MCPURL); resolved != provider.MCPURL {
		provider.MCPURL = resolved
		changed = true
	}

	if !changed {
		return manifest
	}

	cloned := *manifest
	cloned.Plugin = &provider
	return &cloned
}
