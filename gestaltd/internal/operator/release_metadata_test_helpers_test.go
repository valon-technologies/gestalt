package operator

import (
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	"github.com/valon-technologies/gestalt/server/internal/staticvalidation"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/packageio"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
	"gopkg.in/yaml.v3"
)

type providerReleaseMetadataFixture struct {
	Package      string
	Kind         string
	Version      string
	Artifacts    map[string]providerrelease.Artifact
	ArchivePath  string
	Manifest     *providermanifestv1.Manifest
	Catalog      *catalog.Catalog
	NoCatalog    bool
	AllowInvalid bool
}

type providerReleaseFixtureFiles struct {
	Metadata []byte
}

func newProviderReleaseFixtureFiles(t *testing.T, fixture providerReleaseMetadataFixture) providerReleaseFixtureFiles {
	t.Helper()

	if strings.TrimSpace(fixture.ArchivePath) != "" {
		deriveProviderReleaseFixtureFromArchive(t, "", &fixture)
	}
	manifest := fixture.Manifest
	if manifest == nil {
		manifest = &providermanifestv1.Manifest{
			Kind:    fixture.Kind,
			Source:  fixture.Package,
			Version: fixture.Version,
			Spec:    &providermanifestv1.Spec{},
		}
	}
	staticCatalog := fixture.Catalog
	if staticCatalog == nil && !fixture.NoCatalog && providerrelease.CatalogRequired(fixture.Kind, manifest) && !providerrelease.CatalogSessionModeAllowed(fixture.Kind, manifest) {
		staticCatalog = &catalog.Catalog{
			Name: "test",
			Operations: []catalog.CatalogOperation{
				{ID: "echo", Method: "POST"},
			},
		}
	}

	metadata := &providerrelease.Metadata{
		Schema:        providerrelease.SchemaName,
		SchemaVersion: providerrelease.SchemaVersion,
		Package:       fixture.Package,
		Kind:          fixture.Kind,
		Version:       fixture.Version,
		Runtime:       providerrelease.RuntimeForManifest(fixture.Kind, manifest),
		Artifacts:     providerrelease.Artifacts{},
		StaticValidation: &providerrelease.StaticValidation{
			Manifest: manifest,
			Catalog:  staticCatalog,
		},
	}
	for target, artifact := range fixture.Artifacts {
		metadata.Artifacts[strings.TrimSpace(target)] = artifact
	}
	if staticCatalog == nil && providerrelease.CatalogSessionModeAllowed(fixture.Kind, manifest) {
		metadata.StaticValidation.CatalogSessionOnly = true
	}
	if err := providerrelease.ValidateMetadata(metadata); err != nil && !fixture.AllowInvalid {
		t.Fatalf("validate provider release fixture: %v", err)
	}
	metadataData, err := yaml.Marshal(metadata)
	if err != nil {
		t.Fatalf("encode provider release metadata: %v", err)
	}
	return providerReleaseFixtureFiles{Metadata: metadataData}
}

func writeProviderReleaseMetadataFileWithStaticValidation(t *testing.T, metadataPath string, fixture providerReleaseMetadataFixture) {
	t.Helper()

	deriveProviderReleaseFixtureFromArchive(t, metadataPath, &fixture)
	files := newProviderReleaseFixtureFiles(t, fixture)
	if err := os.MkdirAll(filepath.Dir(metadataPath), 0o755); err != nil {
		t.Fatalf("create metadata dir: %v", err)
	}
	writeProviderReleaseFile(t, metadataPath, files.Metadata)
}

func deriveProviderReleaseFixtureFromArchive(t *testing.T, metadataPath string, fixture *providerReleaseMetadataFixture) {
	t.Helper()

	if fixture == nil || len(fixture.Artifacts) == 0 || (fixture.Manifest != nil && (fixture.Catalog != nil || fixture.NoCatalog)) {
		return
	}
	targets := make([]string, 0, len(fixture.Artifacts))
	for target := range fixture.Artifacts {
		targets = append(targets, target)
	}
	slices.Sort(targets)
	archivePath := strings.TrimSpace(fixture.ArchivePath)
	if archivePath == "" {
		archivePath = filepath.Join(filepath.Dir(metadataPath), filepath.FromSlash(fixture.Artifacts[targets[0]].Path))
	}
	if _, err := os.Stat(archivePath); err != nil {
		return
	}
	if fixture.Manifest == nil {
		_, manifest, err := providerpkg.ReadPackageManifest(archivePath)
		if err != nil {
			if !fixture.AllowInvalid {
				t.Fatalf("read fixture archive manifest: %v", err)
			}
			manifest = readInvalidFixtureArchiveManifest(t, archivePath)
		}
		staticManifest, err := staticvalidation.ProjectManifest(manifest, "", true)
		if err != nil {
			t.Fatalf("project fixture archive manifest: %v", err)
		}
		fixture.Manifest = staticManifest
	}
	if fixture.Catalog == nil && !fixture.NoCatalog {
		data, err := packageio.ReadArchiveEntry(archivePath, packageio.StaticCatalogFile)
		if err != nil {
			return
		}
		var staticCatalog catalog.Catalog
		if err := yaml.Unmarshal(data, &staticCatalog); err != nil {
			t.Fatalf("decode fixture archive catalog: %v", err)
		}
		if err := staticCatalog.Validate(); err != nil {
			t.Fatalf("validate fixture archive catalog: %v", err)
		}
		fixture.Catalog = &staticCatalog
	}
}

func readInvalidFixtureArchiveManifest(t *testing.T, archivePath string) *providermanifestv1.Manifest {
	t.Helper()

	for _, name := range providerpkg.ManifestFiles {
		data, err := packageio.ReadArchiveEntry(archivePath, name)
		if err != nil {
			continue
		}
		var manifest providermanifestv1.Manifest
		if err := yaml.Unmarshal(data, &manifest); err != nil {
			t.Fatalf("decode invalid fixture archive manifest %s: %v", name, err)
		}
		return &manifest
	}
	t.Fatalf("read invalid fixture archive manifest: no root manifest found")
	return nil
}

func writeProviderReleaseFile(t *testing.T, path string, data []byte) {
	t.Helper()

	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func serveProviderReleaseFixture(t *testing.T, w httpResponseWriter, requestPath, metadataPath string, fixture providerReleaseMetadataFixture) bool {
	t.Helper()

	deriveProviderReleaseFixtureFromArchive(t, metadataPath, &fixture)
	files := newProviderReleaseFixtureFiles(t, fixture)
	switch requestPath {
	case metadataPath:
		setYAMLContentType(w)
		_, _ = w.Write(files.Metadata)
		return true
	default:
		return false
	}
}

func serveProviderReleaseFixtureForRequest(t *testing.T, w httpResponseWriter, requestPath string, fixture providerReleaseMetadataFixture) bool {
	t.Helper()

	return serveProviderReleaseFixture(t, w, requestPath, requestPath, fixture)
}

type httpResponseWriter interface {
	Header() http.Header
	Write([]byte) (int, error)
}

func setYAMLContentType(w httpResponseWriter) {
	w.Header()["Content-Type"] = []string{"application/yaml"}
}
