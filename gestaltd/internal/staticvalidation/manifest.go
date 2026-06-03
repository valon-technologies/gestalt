package staticvalidation

import (
	"path/filepath"
	"strings"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/packageio"
)

const EntrypointPlaceholder = "static-validation-placeholder"

func ProjectManifest(manifest *providermanifestv1.Manifest, manifestPath string, platformNeutral bool) (*providermanifestv1.Manifest, error) {
	if manifest == nil {
		return nil, nil
	}
	if platformNeutral {
		cloned, err := packageio.CloneManifest(manifest)
		if err != nil {
			return nil, err
		}
		cloned.Artifacts = nil
		if cloned.Entrypoint != nil {
			cloned.Entrypoint = &providermanifestv1.Entrypoint{ArtifactPath: EntrypointPlaceholder}
		}
		return RelativizeManifest(cloned, manifestPath), nil
	}
	return RelativizeManifest(manifest, manifestPath), nil
}

func RelativizeManifest(manifest *providermanifestv1.Manifest, manifestPath string) *providermanifestv1.Manifest {
	if manifest == nil || manifest.Spec == nil || manifest.Spec.Surfaces == nil || strings.TrimSpace(manifestPath) == "" {
		return manifest
	}
	manifestDir := filepath.Dir(manifestPath)
	relativize := func(raw string) string {
		if raw == "" {
			return raw
		}
		if strings.HasPrefix(raw, "file://") {
			path := strings.TrimPrefix(raw, "file://")
			if rel, ok := relativePathWithin(manifestDir, path); ok {
				return "file://" + rel
			}
			return raw
		}
		if strings.Contains(raw, "://") {
			return raw
		}
		if rel, ok := relativePathWithin(manifestDir, raw); ok {
			return rel
		}
		return raw
	}

	surfaces := *manifest.Spec.Surfaces
	changed := false
	if surfaces.OpenAPI != nil {
		openapi := *surfaces.OpenAPI
		if next := relativize(openapi.Document); next != openapi.Document {
			openapi.Document = next
			surfaces.OpenAPI = &openapi
			changed = true
		}
	}
	if surfaces.GraphQL != nil {
		graphql := *surfaces.GraphQL
		if next := relativize(graphql.URL); next != graphql.URL {
			graphql.URL = next
			surfaces.GraphQL = &graphql
			changed = true
		}
	}
	if surfaces.MCP != nil {
		mcp := *surfaces.MCP
		if next := relativize(mcp.URL); next != mcp.URL {
			mcp.URL = next
			surfaces.MCP = &mcp
			changed = true
		}
	}
	if !changed {
		return manifest
	}

	spec := *manifest.Spec
	spec.Surfaces = &surfaces
	cloned := *manifest
	cloned.Spec = &spec
	return &cloned
}

func relativePathWithin(baseDir, path string) (string, bool) {
	if !filepath.IsAbs(path) {
		return filepath.ToSlash(filepath.Clean(path)), false
	}
	rel, err := filepath.Rel(baseDir, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}
