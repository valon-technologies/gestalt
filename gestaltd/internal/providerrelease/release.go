package providerrelease

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/packageio"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
	"github.com/valon-technologies/gestalt/server/services/apps/source"
	"gopkg.in/yaml.v3"
)

const (
	MetadataFile       = "provider-release.yaml"
	SchemaName         = "gestaltd-provider-release"
	SchemaVersion      = 1
	RuntimeExecutable  = "executable"
	RuntimeDeclarative = "declarative"
	RuntimeUI          = "ui"
	GenericTarget      = "generic"
	MaxBytes           = 16 << 20 // One file carries descriptor, validation manifest, and optional catalog.
)

type Metadata struct {
	Schema           string            `yaml:"schema"`
	SchemaVersion    int               `yaml:"schemaVersion"`
	Package          string            `yaml:"package"`
	Kind             string            `yaml:"kind"`
	Version          string            `yaml:"version"`
	Runtime          string            `yaml:"runtime"`
	Artifacts        Artifacts         `yaml:"artifacts"`
	StaticValidation *StaticValidation `yaml:"staticValidation,omitempty"`
}

type Artifacts map[string]Artifact

type Artifact struct {
	Path   string `yaml:"path"`
	SHA256 string `yaml:"sha256"`
}

type StaticValidation struct {
	Manifest           *providermanifestv1.Manifest `yaml:"manifest,omitempty"`
	Catalog            *catalog.Catalog             `yaml:"catalog,omitempty"`
	CatalogSessionOnly bool                         `yaml:"catalogSessionOnly,omitempty"`
}

type staticValidationYAML struct {
	Manifest           *staticValidationManifestYAML `yaml:"manifest,omitempty"`
	Catalog            *catalog.Catalog              `yaml:"catalog,omitempty"`
	CatalogSessionOnly bool                          `yaml:"catalogSessionOnly,omitempty"`
}

type staticValidationManifestYAML struct {
	DisplayName string                          `yaml:"displayName,omitempty"`
	Description string                          `yaml:"description,omitempty"`
	IconFile    string                          `yaml:"iconFile,omitempty"`
	Build       *providermanifestv1.SourceBuild `yaml:"build,omitempty"`
	Run         *providermanifestv1.SourceRun   `yaml:"run,omitempty"`
	Artifacts   []providermanifestv1.Artifact   `yaml:"artifacts,omitempty"`
	Entrypoint  *providermanifestv1.Entrypoint  `yaml:"entrypoint,omitempty"`
	Spec        *providermanifestv1.Spec        `yaml:"spec,omitempty"`
}

func Decode(data []byte) (*Metadata, error) {
	var metadata Metadata
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&metadata); err != nil {
		return nil, fmt.Errorf("decode provider release metadata: %w", err)
	}
	if err := ValidateMetadata(&metadata); err != nil {
		return nil, err
	}
	return &metadata, nil
}

func (s StaticValidation) MarshalYAML() (any, error) {
	return staticValidationYAML{
		Manifest:           staticValidationManifestForYAML(s.Manifest),
		Catalog:            s.Catalog,
		CatalogSessionOnly: s.CatalogSessionOnly,
	}, nil
}

func staticValidationManifestForYAML(manifest *providermanifestv1.Manifest) *staticValidationManifestYAML {
	if manifest == nil {
		return nil
	}
	return &staticValidationManifestYAML{
		DisplayName: manifest.DisplayName,
		Description: manifest.Description,
		IconFile:    manifest.IconFile,
		Build:       manifest.Build,
		Run:         manifest.Run,
		Artifacts:   manifest.Artifacts,
		Entrypoint:  manifest.Entrypoint,
		Spec:        manifest.Spec,
	}
}

func ValidateLocalMetadata(metadataPath string) error {
	data, err := ReadLocalFile(metadataPath)
	if err != nil {
		return err
	}
	_, err = Decode(data)
	return err
}

func ReadLocalFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) > MaxBytes {
		return nil, fmt.Errorf("%s exceeds %d byte limit", path, MaxBytes)
	}
	return data, nil
}

func ValidateMetadata(metadata *Metadata) error {
	if metadata == nil {
		return fmt.Errorf("provider release metadata is required")
	}
	metadata.Kind = providermanifestv1.NormalizeKind(metadata.Kind)
	if metadata.Schema != SchemaName {
		return fmt.Errorf("unsupported provider release schema %q", metadata.Schema)
	}
	if metadata.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported provider release schema version %d", metadata.SchemaVersion)
	}
	if _, err := source.Parse(strings.TrimSpace(metadata.Package)); err != nil {
		return fmt.Errorf("provider release package: %w", err)
	}
	if err := source.ValidateVersion(strings.TrimSpace(metadata.Version)); err != nil {
		return fmt.Errorf("provider release version: %w", err)
	}
	switch metadata.Kind {
	case providermanifestv1.KindApp, providermanifestv1.KindIdentity, providermanifestv1.KindAuthorization, providermanifestv1.KindExternalCredentials, providermanifestv1.KindIndexedDB, providermanifestv1.KindCache, providermanifestv1.KindS3, providermanifestv1.KindWorkflow, providermanifestv1.KindAgent, providermanifestv1.KindSecrets, providermanifestv1.KindRuntime:
	default:
		return fmt.Errorf("provider release kind %q is not supported", metadata.Kind)
	}
	if _, err := ArtifactsByTarget(metadata.Artifacts); err != nil {
		return err
	}
	switch metadata.Runtime {
	case RuntimeExecutable:
	case RuntimeDeclarative:
		if metadata.Kind != providermanifestv1.KindApp {
			return fmt.Errorf("provider release runtime %q is only valid for kind %q", metadata.Runtime, providermanifestv1.KindApp)
		}
	case RuntimeUI:
		if metadata.Kind != providermanifestv1.KindApp {
			return fmt.Errorf("provider release runtime %q is only valid for kind %q", metadata.Runtime, providermanifestv1.KindApp)
		}
	default:
		return fmt.Errorf("provider release runtime %q is not supported", metadata.Runtime)
	}
	if metadata.StaticValidation == nil || metadata.StaticValidation.Manifest == nil {
		return fmt.Errorf("provider release validation manifest is required")
	}
	manifest := metadata.StaticValidation.Manifest
	inheritValidationManifestIdentity(metadata, manifest)
	staticCatalog := metadata.StaticValidation.Catalog
	metadataKind := providermanifestv1.NormalizeKind(metadata.Kind)
	manifestKind := providermanifestv1.NormalizeKind(manifest.Kind)
	if manifestKind != metadataKind {
		return fmt.Errorf("provider release validation manifest kind %q does not match %q", manifestKind, metadataKind)
	}
	if source := strings.TrimSpace(manifest.Source); source != "" && source != strings.TrimSpace(metadata.Package) {
		return fmt.Errorf("provider release validation manifest package %q does not match %q", source, metadata.Package)
	}
	if version := strings.TrimSpace(manifest.Version); version != "" && version != strings.TrimSpace(metadata.Version) {
		return fmt.Errorf("provider release validation manifest version %q does not match %q", version, metadata.Version)
	}
	// The validation manifest is a platform-neutral projection of the packaged
	// manifest: it intentionally strips Entrypoint and Artifacts (see staticvalidation.ProjectManifest).
	// Because the entrypoint signal is gone, RuntimeForManifest on the projection cannot distinguish a
	// hybrid app provider from one with no binary: a hybrid with a manifest-backed spec surface
	// projects to RuntimeDeclarative, and a hybrid carrying a static bundle (spec.assetRoot) projects
	// to RuntimeUI, exactly like a frontend-only app. The real classification lives in
	// metadata.Runtime, which is derived from the full packaged manifest. Validate against the
	// projection accordingly: a declarative release must project to declarative, a ui release must
	// project to ui, and an executable release accepts any projection because the projection cannot
	// prove the binary's absence.
	validationRuntime := RuntimeForManifest(metadataKind, manifest)
	switch metadata.Runtime {
	case RuntimeDeclarative:
		if validationRuntime != RuntimeDeclarative {
			return fmt.Errorf("provider release runtime %q does not match validation manifest runtime %q", metadata.Runtime, validationRuntime)
		}
	case RuntimeUI:
		if validationRuntime != RuntimeUI {
			return fmt.Errorf("provider release runtime %q does not match validation manifest runtime %q", metadata.Runtime, validationRuntime)
		}
	}
	if len(manifest.Artifacts) != 0 {
		return fmt.Errorf("provider release validation manifest must not include platform artifacts")
	}
	if manifest.Entrypoint != nil {
		return fmt.Errorf("provider release validation manifest must not include entrypoint")
	}
	if ManifestReferencesPackageFiles(manifest) {
		return fmt.Errorf("provider release validation manifest must be self-contained")
	}
	if staticCatalog != nil {
		if err := staticCatalog.Validate(); err != nil {
			return fmt.Errorf("provider release validation catalog: %w", err)
		}
	}
	switch {
	case staticCatalog != nil && metadata.StaticValidation.CatalogSessionOnly:
		return fmt.Errorf("provider release staticValidation.catalogSessionOnly must not be set when catalog metadata is present")
	case staticCatalog == nil && metadata.StaticValidation.CatalogSessionOnly && !CatalogSessionModeAllowed(metadataKind, manifest):
		return fmt.Errorf("provider release staticValidation.catalogSessionOnly is only valid for MCP-only validation manifests")
	case CatalogRequired(metadataKind, manifest) && staticCatalog == nil && !metadata.StaticValidation.CatalogSessionOnly && !CatalogSessionModeAllowed(metadataKind, manifest):
		return fmt.Errorf("provider release validation for package %q must include catalog metadata unless the validation manifest is MCP-only", metadata.Package)
	}
	if _, ok := metadata.Artifacts[GenericTarget]; ok && metadataKind == providermanifestv1.KindApp && metadata.Runtime != RuntimeDeclarative {
		return fmt.Errorf("provider release generic app artifact requires a declarative-only provider")
	}
	return nil
}

func inheritValidationManifestIdentity(metadata *Metadata, manifest *providermanifestv1.Manifest) {
	if metadata == nil || manifest == nil {
		return
	}
	if strings.TrimSpace(manifest.Kind) == "" {
		manifest.Kind = metadata.Kind
	}
	if strings.TrimSpace(manifest.Source) == "" {
		manifest.Source = strings.TrimSpace(metadata.Package)
	}
	if strings.TrimSpace(manifest.Version) == "" {
		manifest.Version = strings.TrimSpace(metadata.Version)
	}
}

func ArtifactsByTarget(artifacts Artifacts) (map[string]Artifact, error) {
	if len(artifacts) == 0 {
		return nil, fmt.Errorf("provider release artifacts are required")
	}
	byTarget := make(map[string]Artifact, len(artifacts))
	hasGeneric := false
	hasPlatform := false
	for target, artifact := range artifacts {
		target = strings.TrimSpace(target)
		switch target {
		case "":
			return nil, fmt.Errorf("provider release artifact target is required")
		case GenericTarget:
			hasGeneric = true
		default:
			hasPlatform = true
			if _, _, err := providerpkg.ParsePlatformString(target); err != nil {
				return nil, fmt.Errorf("provider release artifact target %q: %w", target, err)
			}
		}
		if _, ok := byTarget[target]; ok {
			return nil, fmt.Errorf("provider release artifact target %q is duplicated", target)
		}
		if strings.TrimSpace(artifact.Path) == "" {
			return nil, fmt.Errorf("provider release artifact path is required for target %q", target)
		}
		if strings.TrimSpace(artifact.SHA256) == "" {
			return nil, fmt.Errorf("provider release artifact sha256 is required for target %q", target)
		}
		byTarget[target] = artifact
	}
	if hasGeneric && hasPlatform {
		return nil, fmt.Errorf("provider release generic artifact must not be mixed with platform artifacts")
	}
	return byTarget, nil
}

func RuntimeForManifest(kind string, manifest *providermanifestv1.Manifest) string {
	if providermanifestv1.NormalizeKind(kind) == providermanifestv1.KindApp {
		if manifest != nil && manifest.Spec != nil && manifest.Spec.AssetRoot != "" && manifest.Entrypoint == nil && !manifest.IsDeclarativeOnlyProvider() {
			return RuntimeUI
		}
		if manifest != nil && manifest.IsDeclarativeOnlyProvider() {
			return RuntimeDeclarative
		}
	}
	return RuntimeExecutable
}

func CatalogRequired(kind string, manifest *providermanifestv1.Manifest) bool {
	return providermanifestv1.NormalizeKind(kind) == providermanifestv1.KindApp &&
		manifest != nil &&
		manifest.Spec != nil &&
		(!manifest.Spec.IsManifestBacked() || manifest.Spec.MCPURL() != "")
}

func CatalogSessionModeAllowed(kind string, manifest *providermanifestv1.Manifest) bool {
	return providermanifestv1.NormalizeKind(kind) == providermanifestv1.KindApp &&
		manifest != nil &&
		manifest.Spec != nil &&
		manifest.Spec.MCPURL() != "" &&
		manifest.Spec.OpenAPIDocument() == "" &&
		manifest.Spec.GraphQLURL() == "" &&
		len(manifest.Spec.RESTOperations()) == 0
}

func ManifestReferencesPackageFiles(manifest *providermanifestv1.Manifest) bool {
	if manifest == nil {
		return false
	}
	if packageio.IsLocalPackageReference(manifest.IconFile) {
		return true
	}
	spec := manifest.Spec
	if spec == nil {
		return false
	}
	if packageio.IsLocalPackageReference(spec.ConfigSchemaPath) {
		return true
	}
	if spec.Surfaces == nil {
		return false
	}
	return (spec.Surfaces.OpenAPI != nil && packageio.IsLocalPackageReference(spec.Surfaces.OpenAPI.Document)) ||
		(spec.Surfaces.GraphQL != nil && packageio.IsLocalPackageReference(spec.Surfaces.GraphQL.URL)) ||
		(spec.Surfaces.MCP != nil && packageio.IsLocalPackageReference(spec.Surfaces.MCP.URL))
}
