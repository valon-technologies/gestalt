package registrytest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/config"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/packageio"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

const Bucket = "gitlab-peach-street-gestalt-app-registry"

// InstallFixture is a published g-issues registry version with a downloadable archive.
type InstallFixture struct {
	Registry     config.AppRegistryConfig
	Reader       *appregistry.RegistryReader
	Version      string
	SHA256       string
	ArchiveBytes []byte
}

// NewInstallFixture builds a mock GCS registry with one installable app version.
func NewInstallFixture(t *testing.T) InstallFixture {
	t.Helper()

	version := "0.0.0-snapshot.gabc123"
	sourceRef := "abc123def456abc123def456abc123def456abcd"
	platform := providerpkg.CurrentPlatformString()

	root := t.TempDir()
	packageRoot := filepath.Join(root, "package")
	artifactRel := packageio.PackageExecutablePath("g-issues", runtime.GOOS)
	artifactPath := filepath.Join(packageRoot, filepath.FromSlash(artifactRel))
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(artifactPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	digest, err := packageio.FileSHA256(artifactPath)
	if err != nil {
		t.Fatalf("FileSHA256: %v", err)
	}

	manifest := &providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindApp,
		Source:  "github.com/valon-technologies/valon-tools/apps/g-issues",
		Version: version,
		Spec:    &providermanifestv1.Spec{},
		Entrypoint: &providermanifestv1.Entrypoint{
			ArtifactPath: artifactRel,
		},
		Artifacts: []providermanifestv1.Artifact{
			{
				OS:     runtime.GOOS,
				Arch:   runtime.GOARCH,
				Path:   artifactRel,
				SHA256: digest,
			},
		},
	}
	manifestData, err := packageio.EncodeManifestFormat(manifest, packageio.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeManifestFormat: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packageRoot, "manifest.yaml"), manifestData, 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packageRoot, providerpkg.StaticCatalogFile), []byte("name: g-issues\noperations:\n  - id: issues.list\n    method: POST\n"), 0o644); err != nil {
		t.Fatalf("WriteFile catalog: %v", err)
	}

	packagePath := filepath.Join(root, "artifact.tar.gz")
	if err := packageio.CreatePackageFromDir(packageRoot, packagePath); err != nil {
		t.Fatalf("CreatePackageFromDir: %v", err)
	}
	archiveBytes, err := os.ReadFile(packagePath)
	if err != nil {
		t.Fatalf("ReadFile package: %v", err)
	}
	archiveSHA, err := packageio.FileSHA256(packagePath)
	if err != nil {
		t.Fatalf("FileSHA256 package: %v", err)
	}

	entry := appregistry.Entry{
		SchemaVersion: appregistry.EntrySchemaVersion,
		App:           "g-issues",
		Version:       version,
		SourceRef:     sourceRef,
		ManifestPath:  "valon-tools/apps/g-issues/manifest.yaml",
		Repository:    "github.com/valon-technologies/valon-tools",
		Publication: &appregistry.Publication{
			WorkflowRunURL: "https://github.com/valon-technologies/valon-tools/actions/runs/123456789",
			TriggerPullRequest: &appregistry.PublicationPullRequest{
				Number: 3251,
				URL:    "https://github.com/valon-technologies/valon-tools/pull/3251",
			},
		},
		Artifacts: map[string]appregistry.Artifact{
			platform: {
				URL:       "gs://" + Bucket + "/apps/g-issues/artifacts/" + version + "/artifact.tar.gz",
				PublicURL: "https://storage.googleapis.com/" + Bucket + "/apps/g-issues/artifacts/" + version + "/artifact.tar.gz",
				SHA256:    archiveSHA,
			},
		},
		PublishedAt: time.Date(2026, 7, 10, 2, 21, 54, 0, time.UTC),
	}
	entryJSON, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal entry: %v", err)
	}

	index := appregistry.Index{
		SchemaVersion: appregistry.IndexSchemaVersion,
		Apps: map[string]appregistry.AppVersions{
			"g-issues": {
				Versions: map[string]appregistry.IndexVersion{
					version: {
						Metadata:    appregistry.AppVersionEntryPath("g-issues", version),
						Platforms:   []string{platform},
						PublishedAt: entry.PublishedAt,
						SourceRef:   entry.SourceRef,
						Repository:  entry.Repository,
						Publication: entry.Publication,
					},
				},
			},
		},
	}
	indexJSON, err := json.Marshal(index)
	if err != nil {
		t.Fatalf("Marshal index: %v", err)
	}

	registrySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + Bucket + "/apps/g-issues/index.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(indexJSON)
		case "/" + Bucket + "/apps/g-issues/versions/" + version + ".json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(entryJSON)
		case "/" + Bucket + "/apps/g-issues/artifacts/" + version + "/artifact.tar.gz":
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(archiveBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(registrySrv.Close)

	registry, err := config.NewGCSAppRegistry(Bucket)
	if err != nil {
		t.Fatalf("NewGCSAppRegistry: %v", err)
	}

	return InstallFixture{
		Registry:     registry,
		Reader:       NewReaderForServer(t, registrySrv.URL),
		Version:      version,
		SHA256:       archiveSHA,
		ArchiveBytes: archiveBytes,
	}
}

// NewReaderForServer returns a reader that rewrites GCS public URLs to a test server.
func NewReaderForServer(t *testing.T, serverURL string) *appregistry.RegistryReader {
	t.Helper()
	registryHost := strings.TrimPrefix(serverURL, "http://")
	return &appregistry.RegistryReader{
		HTTPClient: &http.Client{
			Transport: &RewriteHostTransport{Host: registryHost, Scheme: "http"},
		},
	}
}

// NewDigestMismatchReader returns a reader whose entry advertises a bad checksum.
func (f InstallFixture) NewDigestMismatchReader(t *testing.T, version string) *appregistry.RegistryReader {
	t.Helper()

	publicURL, err := f.Registry.PublicURL()
	if err != nil {
		t.Fatalf("PublicURL: %v", err)
	}
	entry, err := f.Reader.FetchEntry(t.Context(), publicURL, "g-issues", f.Version)
	if err != nil {
		t.Fatalf("FetchEntry: %v", err)
	}
	entry.Version = version
	platform := providerpkg.CurrentPlatformString()
	artifact := entry.Artifacts[platform]
	artifact.SHA256 = "deadbeef"
	entry.Artifacts[platform] = artifact
	entryJSON, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	registrySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + Bucket + "/apps/g-issues/versions/" + version + ".json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(entryJSON)
		case "/" + Bucket + "/apps/g-issues/artifacts/" + f.Version + "/artifact.tar.gz":
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(f.ArchiveBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(registrySrv.Close)
	return NewReaderForServer(t, registrySrv.URL)
}

// RewriteHostTransport redirects requests to a local test HTTP server.
type RewriteHostTransport struct {
	Host   string
	Scheme string
	Inner  http.RoundTripper
}

func (t *RewriteHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = t.Scheme
	clone.URL.Host = t.Host
	inner := t.Inner
	if inner == nil {
		inner = http.DefaultTransport
	}
	return inner.RoundTrip(clone)
}
