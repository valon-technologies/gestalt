package providerrelease

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
	"github.com/valon-technologies/gestalt/server/services/apps/source"
	"gopkg.in/yaml.v3"
)

const (
	MetadataFile           = "provider-release.yaml"
	ValidationManifestFile = "validation-manifest.yaml"
	ValidationCatalogFile  = "validation-catalog.yaml"
	RuntimeExecutable      = "executable"
	RuntimeDeclarative     = "declarative"
	RuntimeUI              = "ui"
	GenericTarget          = "generic"
	MaxBytes               = 4 << 20
)

type Metadata struct {
	Package                  string    `yaml:"package"`
	Kind                     string    `yaml:"kind"`
	Version                  string    `yaml:"version"`
	Artifacts                Artifacts `yaml:"artifacts"`
	ValidationManifestSHA256 string    `yaml:"validationManifestSHA256"`
	ValidationCatalogSHA256  string    `yaml:"validationCatalogSHA256,omitempty"`
}

type Artifacts map[string]Artifact

type Artifact struct {
	Path   string `yaml:"path"`
	SHA256 string `yaml:"sha256"`
}

func SHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func Decode(data []byte) (*Metadata, error) {
	var metadata Metadata
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&metadata); err != nil {
		return nil, fmt.Errorf("decode provider release metadata: %w", err)
	}
	if err := validateDescriptor(&metadata); err != nil {
		return nil, err
	}
	return &metadata, nil
}

func DecodeManifest(data []byte) (*providermanifestv1.Manifest, error) {
	var manifest providermanifestv1.Manifest
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func DecodeCatalog(data []byte) (*catalog.Catalog, error) {
	var cat catalog.Catalog
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cat); err != nil {
		return nil, err
	}
	return &cat, nil
}

func ValidateLocalBundle(metadataPath string) error {
	metadata, manifestData, catalogData, err := readLocalValidationFiles(metadataPath)
	if err != nil {
		return err
	}
	manifest, err := DecodeManifest(manifestData)
	if err != nil {
		return fmt.Errorf("decode %s: %w", ValidationManifestFile, err)
	}
	var staticCatalog *catalog.Catalog
	if len(catalogData) != 0 {
		staticCatalog, err = DecodeCatalog(catalogData)
		if err != nil {
			return fmt.Errorf("decode %s: %w", ValidationCatalogFile, err)
		}
	}
	return ValidateBundle(metadata, manifest, staticCatalog)
}

func readLocalValidationFiles(metadataPath string) (*Metadata, []byte, []byte, error) {
	data, err := ReadLocalFile(metadataPath)
	if err != nil {
		return nil, nil, nil, err
	}
	metadata, err := Decode(data)
	if err != nil {
		return nil, nil, nil, err
	}
	dir := filepath.Dir(metadataPath)
	manifestData, err := readVerifiedLocalSidecar(filepath.Join(dir, ValidationManifestFile), metadata.ValidationManifestSHA256)
	if err != nil {
		return nil, nil, nil, err
	}
	var catalogData []byte
	if metadata.ValidationCatalogSHA256 != "" {
		catalogData, err = readVerifiedLocalSidecar(filepath.Join(dir, ValidationCatalogFile), metadata.ValidationCatalogSHA256)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	return metadata, manifestData, catalogData, nil
}

func readVerifiedLocalSidecar(path, wantSHA string) ([]byte, error) {
	data, err := ReadLocalFile(path)
	if err != nil {
		return nil, err
	}
	if got := SHA256Hex(data); got != strings.TrimSpace(wantSHA) {
		return nil, fmt.Errorf("provider release validation sidecar %s sha256 %q does not match %q", filepath.Base(path), got, strings.TrimSpace(wantSHA))
	}
	return data, nil
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

func validateDescriptor(metadata *Metadata) error {
	if metadata == nil {
		return fmt.Errorf("provider release metadata is required")
	}
	metadata.Kind = providermanifestv1.NormalizeKind(metadata.Kind)
	if _, err := source.Parse(strings.TrimSpace(metadata.Package)); err != nil {
		return fmt.Errorf("provider release package: %w", err)
	}
	if err := source.ValidateVersion(strings.TrimSpace(metadata.Version)); err != nil {
		return fmt.Errorf("provider release version: %w", err)
	}
	switch metadata.Kind {
	case providermanifestv1.KindApp, providermanifestv1.KindAuthentication, providermanifestv1.KindAuthorization, providermanifestv1.KindExternalCredentials, providermanifestv1.KindIndexedDB, providermanifestv1.KindCache, providermanifestv1.KindS3, providermanifestv1.KindWorkflow, providermanifestv1.KindAgent, providermanifestv1.KindSecrets, providermanifestv1.KindRuntime, providermanifestv1.KindUI:
	default:
		return fmt.Errorf("provider release kind %q is not supported", metadata.Kind)
	}
	if _, err := ArtifactsByTarget(metadata.Artifacts); err != nil {
		return err
	}
	if strings.TrimSpace(metadata.ValidationManifestSHA256) == "" {
		return fmt.Errorf("provider release validation manifest sha256 is required")
	}
	return nil
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

func ValidateBundle(metadata *Metadata, manifest *providermanifestv1.Manifest, staticCatalog *catalog.Catalog) error {
	if err := validateDescriptor(metadata); err != nil {
		return err
	}
	if manifest == nil {
		return fmt.Errorf("provider release validation manifest is required")
	}
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
	case strings.TrimSpace(metadata.ValidationCatalogSHA256) != "" && staticCatalog == nil:
		return fmt.Errorf("provider release validation catalog sidecar is required")
	case strings.TrimSpace(metadata.ValidationCatalogSHA256) == "" && staticCatalog != nil:
		return fmt.Errorf("provider release validation catalog sha256 is required")
	case CatalogRequired(metadataKind, manifest) && staticCatalog == nil && !CatalogSessionModeAllowed(metadataKind, manifest):
		return fmt.Errorf("provider release validation for package %q must include catalog metadata unless the validation manifest is MCP-only", metadata.Package)
	}
	if _, ok := metadata.Artifacts[GenericTarget]; ok && metadataKind == providermanifestv1.KindApp && RuntimeForManifest(metadataKind, manifest) != RuntimeDeclarative {
		return fmt.Errorf("provider release generic app artifact requires declarative-only validation manifest")
	}
	return nil
}

func RuntimeForManifest(kind string, manifest *providermanifestv1.Manifest) string {
	switch providermanifestv1.NormalizeKind(kind) {
	case providermanifestv1.KindUI:
		return RuntimeUI
	case providermanifestv1.KindApp:
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
	if packageFileReference(manifest.IconFile) {
		return true
	}
	spec := manifest.Spec
	if spec == nil {
		return false
	}
	if packageFileReference(spec.ConfigSchemaPath) {
		return true
	}
	if spec.UI != nil && packageFileReference(spec.UI.Path) {
		return true
	}
	if spec.Surfaces == nil {
		return false
	}
	return (spec.Surfaces.OpenAPI != nil && packageFileReference(spec.Surfaces.OpenAPI.Document)) ||
		(spec.Surfaces.GraphQL != nil && packageFileReference(spec.Surfaces.GraphQL.URL)) ||
		(spec.Surfaces.MCP != nil && packageFileReference(spec.Surfaces.MCP.URL))
}

func packageFileReference(raw string) bool {
	value := strings.TrimSpace(raw)
	return value != "" && (strings.HasPrefix(value, "file://") || !strings.Contains(value, "://"))
}
