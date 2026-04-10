package pluginpkg

import (
	"fmt"
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
		if doc := manifest.Plugin.OpenAPIDocument(); doc != "" && !strings.Contains(doc, "://") {
			add(doc, "provider openapi document")
		}
		if url := manifest.Plugin.GraphQLURL(); url != "" && !strings.Contains(url, "://") {
			add(url, "provider graphql document")
		}
		if url := manifest.Plugin.MCPURL(); url != "" && !strings.Contains(url, "://") {
			add(url, "provider mcp document")
		}
	}
	if manifest.WebUI != nil {
		add(manifest.WebUI.ConfigSchemaPath, "webui config schema")
	}
	add(manifest.IconFile, "icon_file")
	return refs
}

func ResolveManifestLocalReferences(manifest *pluginmanifestv1.Manifest, manifestPath string) (*pluginmanifestv1.Manifest, error) {
	if manifest == nil || manifest.Plugin == nil || manifestPath == "" {
		return manifest, nil
	}
	if manifest.Plugin.Surfaces == nil {
		return manifest, nil
	}

	manifestDir := filepath.Dir(manifestPath)
	var resolveErr error
	resolve := func(value string) string {
		if resolveErr != nil || value == "" || strings.Contains(value, "://") {
			return value
		}
		if filepath.IsAbs(value) {
			if !isPathWithinDir(manifestDir, filepath.Clean(value)) {
				resolveErr = fmt.Errorf("local reference %q is outside the manifest directory", value)
			}
			return value
		}
		resolved := filepath.Join(manifestDir, filepath.FromSlash(value))
		if !isPathWithinDir(manifestDir, resolved) {
			resolveErr = fmt.Errorf("local reference %q escapes the manifest directory", value)
			return value
		}
		return resolved
	}

	provider := *manifest.Plugin
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

	if resolveErr != nil {
		return nil, resolveErr
	}

	if !changed {
		return manifest, nil
	}

	provider.Surfaces = &surfaces
	cloned := *manifest
	cloned.Plugin = &provider
	return &cloned, nil
}
