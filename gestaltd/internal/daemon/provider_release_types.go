package daemon

import (
	"github.com/valon-technologies/gestalt/server/core/catalog"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

const defaultPlatforms = "darwin/amd64,darwin/arm64,linux/amd64,linux/arm64"
const allPlatformsValue = "all"
const defaultReleaseOutputDir = "dist/"
const providerReleaseMetadataFile = "provider-release.yaml"
const providerReleaseSchemaName = "gestaltd-provider-release"
const providerReleaseSchemaVersion = 1
const providerReleaseRuntimeKindExecutable = "executable"
const providerReleaseRuntimeKindDeclarative = "declarative"
const providerReleaseRuntimeKindUI = "ui"
const providerReleaseGenericTarget = "generic"

type releasePlatform struct {
	GOOS   string
	GOARCH string
}

type releaseBuildTarget struct {
	Kind          string
	DeclaredBuild bool
	Prebuilt      bool
}

type releaseArchive struct {
	Path   string
	SHA256 string
	Target string
}

type providerReleaseMetadata struct {
	Schema           string                               `yaml:"schema"`
	SchemaVersion    int                                  `yaml:"schemaVersion"`
	Package          string                               `yaml:"package"`
	Kind             string                               `yaml:"kind"`
	Version          string                               `yaml:"version"`
	Runtime          string                               `yaml:"runtime"`
	Artifacts        map[string]providerReleaseArtifact   `yaml:"artifacts,omitempty"`
	StaticValidation *providerReleaseStaticValidationData `yaml:"staticValidation,omitempty"`
}

type providerReleaseArtifact struct {
	Path   string `yaml:"path"`
	SHA256 string `yaml:"sha256"`
}

type providerReleaseStaticValidationData struct {
	Manifest           *providermanifestv1.Manifest `yaml:"manifest,omitempty"`
	Catalog            *catalog.Catalog             `yaml:"catalog,omitempty"`
	CatalogSessionOnly bool                         `yaml:"catalogSessionOnly,omitempty"`
}
