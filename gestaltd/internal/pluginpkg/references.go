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

	if manifest.Provider != nil {
		add(manifest.Provider.ConfigSchemaPath, "provider config schema")
		add(manifest.Provider.StaticCatalogPath, "provider static catalog")
	}
	add(manifest.IconFile, "icon_file")
	return refs
}

func ResolveManifestLocalReferences(manifest *pluginmanifestv1.Manifest, manifestPath string) *pluginmanifestv1.Manifest {
	if manifest == nil || manifest.Provider == nil || manifestPath == "" {
		return manifest
	}

	resolve := func(value string) string {
		if value == "" || filepath.IsAbs(value) || strings.Contains(value, "://") {
			return value
		}
		return filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(value))
	}

	provider := *manifest.Provider
	changed := false

	if resolved := resolve(provider.OpenAPI); resolved != provider.OpenAPI {
		provider.OpenAPI = resolved
		changed = true
	}
	if resolved := resolve(provider.StaticCatalogPath); resolved != provider.StaticCatalogPath {
		provider.StaticCatalogPath = resolved
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
	cloned.Provider = &provider
	return &cloned
}
