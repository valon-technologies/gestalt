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

	if manifest.Spec != nil {
		add(manifest.Spec.ConfigSchemaPath, "provider config schema")
		if doc := manifest.Spec.OpenAPIDocument(); doc != "" && !strings.Contains(doc, "://") {
			add(doc, "provider openapi document")
		}
		if url := manifest.Spec.GraphQLURL(); url != "" && !strings.Contains(url, "://") {
			add(url, "provider graphql document")
		}
		if url := manifest.Spec.MCPURL(); url != "" && !strings.Contains(url, "://") {
			add(url, "provider mcp document")
		}
	}
	add(manifest.IconFile, "icon_file")
	return refs
}

func ResolveManifestLocalReferences(manifest *pluginmanifestv1.Manifest, manifestPath string) *pluginmanifestv1.Manifest {
	if manifest == nil || manifest.Spec == nil || manifestPath == "" {
		return manifest
	}
	if manifest.Spec.Surfaces == nil {
		return manifest
	}

	resolve := func(value string) string {
		if value == "" || filepath.IsAbs(value) || strings.Contains(value, "://") {
			return value
		}
		return filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(value))
	}

	provider := *manifest.Spec
	surfaces := *provider.Surfaces
	changed := false

	if surfaces.OpenAPI != nil {
		s := *surfaces.OpenAPI
		if resolved := resolve(s.Document); resolved != s.Document {
			s.Document = resolved
			surfaces.OpenAPI = &s
			changed = true
		}
	}
	if surfaces.GraphQL != nil {
		s := *surfaces.GraphQL
		if resolved := resolve(s.URL); resolved != s.URL {
			s.URL = resolved
			surfaces.GraphQL = &s
			changed = true
		}
	}
	if surfaces.MCP != nil {
		s := *surfaces.MCP
		if resolved := resolve(s.URL); resolved != s.URL {
			s.URL = resolved
			surfaces.MCP = &s
			changed = true
		}
	}

	if !changed {
		return manifest
	}

	provider.Surfaces = &surfaces
	cloned := *manifest
	cloned.Spec = &provider
	return &cloned
}
