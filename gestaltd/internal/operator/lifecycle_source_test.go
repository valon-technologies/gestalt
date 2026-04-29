package operator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	secretsprovider "github.com/valon-technologies/gestalt/server/internal/drivers/secrets/provider"
	"github.com/valon-technologies/gestalt/server/internal/providerpkg"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"gopkg.in/yaml.v3"
)

const (
	testOwner   = "testowner"
	testRepo    = "testrepo"
	testPlugin  = "testplugin"
	testVersion = "1.0.0"
	testSource  = "github.com/" + testOwner + "/" + testRepo + "/plugins/" + testPlugin
	testBinary  = "fake-binary-content"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func sha256hex(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

func artifactRelPath(binary string) string {
	return filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, binary))
}

func buildV2Archive(t *testing.T, dir, source, version, binaryContent string) string {
	t.Helper()

	artPath := artifactRelPath("provider")
	return buildV2ArchiveForArtifact(t, dir, source, version, artPath, "", binaryContent)
}

func buildV2ArchiveForArtifact(t *testing.T, dir, source, version, artifactPath, libc, binaryContent string) string {
	t.Helper()

	safeName := strings.NewReplacer("/", "-", ".", "_").Replace(artifactPath + "-" + libc + "-" + binaryContent)
	srcDir := filepath.Join(dir, safeName+"-src")
	if err := os.MkdirAll(filepath.Join(srcDir, filepath.Dir(filepath.FromSlash(artifactPath))), 0755); err != nil {
		t.Fatalf("create provider src dir: %v", err)
	}
	manifest := &providermanifestv1.Manifest{
		Source:  source,
		Version: version,
		Kind:    providermanifestv1.KindPlugin, Spec: &providermanifestv1.Spec{},
		Artifacts: []providermanifestv1.Artifact{
			{
				OS:     runtime.GOOS,
				Arch:   runtime.GOARCH,
				LibC:   libc,
				Path:   artifactPath,
				SHA256: sha256hex(binaryContent),
			},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: artifactPath},
	}

	manifestBytes, err := providerpkg.EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "manifest.json"), manifestBytes, 0644); err != nil {
		t.Fatalf("write provider manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "catalog.yaml"), []byte("name: provider\noperations:\n  - id: echo\n    method: POST\n"), 0644); err != nil {
		t.Fatalf("write provider catalog: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, filepath.FromSlash(artifactPath)), []byte(binaryContent), 0755); err != nil {
		t.Fatalf("write provider artifact: %v", err)
	}

	archivePath := filepath.Join(dir, safeName+".tar.gz")
	if err := providerpkg.CreatePackageFromDir(srcDir, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir plugin: %v", err)
	}

	return archivePath
}

func writeProviderReleaseMetadataFile(t *testing.T, path string, metadata providerReleaseMetadata) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create metadata dir: %v", err)
	}
	data, err := yaml.Marshal(metadata)
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
}

func buildExecutableArchive(t *testing.T, dir, srcDirName, source, version, kind, binaryName, binaryContent string) string {
	t.Helper()

	return buildExecutableArchiveData(t, dir, srcDirName, source, version, kind, binaryName, []byte(binaryContent))
}

func buildExecutableArchiveFromBinaryPath(t *testing.T, dir, srcDirName, source, version, kind, binaryName, binaryPath string) string {
	t.Helper()

	data, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("read binary %s: %v", binaryPath, err)
	}
	return buildExecutableArchiveData(t, dir, srcDirName, source, version, kind, binaryName, data)
}

func buildExecutableArchiveData(t *testing.T, dir, srcDirName, source, version, kind, binaryName string, binaryData []byte) string {
	t.Helper()

	artPath := artifactRelPath(binaryName)
	srcDir := filepath.Join(dir, srcDirName)
	if err := os.MkdirAll(filepath.Join(srcDir, filepath.Dir(filepath.FromSlash(artPath))), 0755); err != nil {
		t.Fatalf("create plugin src dir: %v", err)
	}
	manifest := &providermanifestv1.Manifest{
		Source:  source,
		Version: version,
		Artifacts: []providermanifestv1.Artifact{
			{
				OS:   runtime.GOOS,
				Arch: runtime.GOARCH,
				Path: artPath,
				SHA256: func() string {
					sum := sha256.Sum256(binaryData)
					return hex.EncodeToString(sum[:])
				}(),
			},
		},
	}
	manifest.Kind = kind
	manifest.Spec = &providermanifestv1.Spec{}
	manifest.Entrypoint = &providermanifestv1.Entrypoint{ArtifactPath: artPath}

	manifestBytes, err := providerpkg.EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "manifest.json"), manifestBytes, 0644); err != nil {
		t.Fatalf("write provider manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "catalog.yaml"), []byte("name: provider\noperations:\n  - id: echo\n    method: POST\n"), 0644); err != nil {
		t.Fatalf("write provider catalog: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, filepath.FromSlash(artPath)), binaryData, 0755); err != nil {
		t.Fatalf("write provider artifact: %v", err)
	}

	archivePath := filepath.Join(dir, srcDirName+".tar.gz")
	if err := providerpkg.CreatePackageFromDir(srcDir, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir plugin: %v", err)
	}

	return archivePath
}

type localExecutableManifestArtifact struct {
	goos       string
	goarch     string
	libc       string
	binaryName string
	data       []byte
}

func writeExecutableSourceManifest(t *testing.T, dir, srcDirName, source, version, kind string, artifacts []localExecutableManifestArtifact) string {
	t.Helper()

	srcDir := filepath.Join(dir, srcDirName)
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
	manifest := &providermanifestv1.Manifest{
		Source:  source,
		Version: version,
		Kind:    kind,
		Spec:    &providermanifestv1.Spec{},
	}
	var entrypoint string
	for i, artifact := range artifacts {
		artifactPath := filepath.ToSlash(filepath.Join("artifacts", artifact.goos, artifact.goarch, artifact.binaryName))
		manifest.Artifacts = append(manifest.Artifacts, providermanifestv1.Artifact{
			OS:     artifact.goos,
			Arch:   artifact.goarch,
			LibC:   artifact.libc,
			Path:   artifactPath,
			SHA256: sha256hex(string(artifact.data)),
		})
		if i == 0 || (artifact.goos == runtime.GOOS && artifact.goarch == runtime.GOARCH && artifact.libc == "") {
			entrypoint = artifactPath
		}
		artifactFilePath := filepath.Join(srcDir, filepath.FromSlash(artifactPath))
		if err := os.MkdirAll(filepath.Dir(artifactFilePath), 0o755); err != nil {
			t.Fatalf("create artifact dir: %v", err)
		}
		if err := os.WriteFile(artifactFilePath, artifact.data, 0o755); err != nil {
			t.Fatalf("write artifact: %v", err)
		}
	}
	if entrypoint == "" {
		t.Fatal("writeExecutableSourceManifest: no entrypoint artifact selected")
	}
	manifest.Entrypoint = &providermanifestv1.Entrypoint{ArtifactPath: entrypoint}
	manifestBytes, err := providerpkg.EncodeSourceManifestFormat(manifest, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("encode source manifest: %v", err)
	}
	manifestPath := filepath.Join(srcDir, "manifest.yaml")
	if err := os.WriteFile(manifestPath, manifestBytes, 0o644); err != nil {
		t.Fatalf("write source manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "catalog.yaml"), []byte("name: provider\noperations:\n  - id: echo\n    method: POST\n"), 0o644); err != nil {
		t.Fatalf("write provider catalog: %v", err)
	}
	return manifestPath
}

func buildGoSourceSecretsBinary(t *testing.T) string {
	t.Helper()

	providerDir := filepath.Join(t.TempDir(), "go-secrets")
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(providerDir): %v", err)
	}
	if err := os.WriteFile(filepath.Join(providerDir, "go.mod"), []byte(testutil.GeneratedProviderModuleSource(t, "example.com/test-go-secrets")), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(providerDir, "go.sum"), testutil.GeneratedProviderModuleSum(t), 0o644); err != nil {
		t.Fatalf("write go.sum: %v", err)
	}
	if err := os.WriteFile(filepath.Join(providerDir, "secrets.go"), []byte(testutil.GeneratedSecretsPackageSource()), 0o644); err != nil {
		t.Fatalf("write secrets.go: %v", err)
	}
	outputPath := filepath.Join(t.TempDir(), "secrets-provider")
	if err := providerpkg.BuildGoComponentBinary(providerDir, outputPath, providermanifestv1.KindSecrets, runtime.GOOS, runtime.GOARCH); err != nil {
		t.Fatalf("BuildGoComponentBinary(secrets): %v", err)
	}
	return outputPath
}

func writeBootstrapSecretsManifest(t *testing.T, dir, source, version string) string {
	t.Helper()

	bootstrapArtifact := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "bootstrap-secrets"))
	manifestPath := filepath.Join(dir, "bootstrap-secrets-manifest.yaml")
	manifest, err := providerpkg.EncodeSourceManifestFormat(&providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindSecrets,
		Source:  source,
		Version: version,
		Spec:    &providermanifestv1.Spec{},
		Artifacts: []providermanifestv1.Artifact{
			{OS: runtime.GOOS, Arch: runtime.GOARCH, Path: bootstrapArtifact},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: bootstrapArtifact},
	}, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat bootstrap: %v", err)
	}
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("write bootstrap manifest: %v", err)
	}
	bootstrapBinaryData, err := os.ReadFile(buildGoSourceSecretsBinary(t))
	if err != nil {
		t.Fatalf("read bootstrap binary: %v", err)
	}
	bootstrapBinaryPath := filepath.Join(dir, filepath.FromSlash(bootstrapArtifact))
	if err := os.MkdirAll(filepath.Dir(bootstrapBinaryPath), 0o755); err != nil {
		t.Fatalf("MkdirAll bootstrap artifact: %v", err)
	}
	if err := os.WriteFile(bootstrapBinaryPath, bootstrapBinaryData, 0o755); err != nil {
		t.Fatalf("write bootstrap artifact: %v", err)
	}
	return manifestPath
}

func TestSourcePluginMetadataURLInitAndLockedLoad(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name               string
		apiVersion         string
		localSource        bool
		remoteArchives     bool
		tamperLocalArchive bool
	}{
		{name: "remote metadata url", apiVersion: config.ConfigAPIVersion},
		{name: "local metadata file", apiVersion: config.ConfigAPIVersion, localSource: true},
		{name: "local metadata file with remote archives", apiVersion: config.ConfigAPIVersion, localSource: true, remoteArchives: true},
		{name: "local metadata file rejects tampered archive", apiVersion: config.ConfigAPIVersion, localSource: true, tamperLocalArchive: true},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			packageSource := "github.com/acme/tools/alpha"
			version := "1.2.3"
			currentArchivePath := buildV2Archive(t, dir, packageSource, version, "metadata-url-plugin-binary")
			currentArchiveData, err := os.ReadFile(currentArchivePath)
			if err != nil {
				t.Fatalf("read current archive: %v", err)
			}
			currentArchiveSHA := sha256.Sum256(currentArchiveData)

			extraPlatform := struct {
				goos   string
				goarch string
			}{
				goos:   "linux",
				goarch: "amd64",
			}
			for _, candidate := range []struct {
				goos   string
				goarch string
			}{
				{goos: "linux", goarch: "amd64"},
				{goos: "linux", goarch: "arm64"},
				{goos: "darwin", goarch: "amd64"},
				{goos: "darwin", goarch: "arm64"},
			} {
				if candidate.goos != runtime.GOOS || candidate.goarch != runtime.GOARCH {
					extraPlatform = candidate
					break
				}
			}
			extraPlatformKey := providerpkg.PlatformString(extraPlatform.goos, extraPlatform.goarch)
			extraArchiveData := []byte("metadata-extra-platform-archive")
			extraArchiveSHA := sha256.Sum256(extraArchiveData)

			var metadataCount atomic.Int64
			var currentArchiveCount atomic.Int64
			var extraArchiveCount atomic.Int64
			handlerErrs := make(chan error, 4)
			nextHandlerErr := func() error {
				t.Helper()
				select {
				case err := <-handlerErrs:
					return err
				default:
					return nil
				}
			}

			metadataPath := "/providers/alpha/provider-release.yaml"
			currentArchivePathURL := "/providers/alpha/alpha-current.tar.gz"
			extraArchivePathURL := "/providers/alpha/alpha-extra.tar.gz"
			sourceValue := ""
			wantSource := ""
			wantCurrentArchiveURL := ""
			wantExtraArchiveURL := ""
			localCurrentArchivePath := ""
			var srv *httptest.Server

			if tc.localSource {
				metadataRelPath := filepath.ToSlash(filepath.Join("providers", "alpha", "provider-release.yaml"))
				metadataAbsPath := filepath.Join(dir, filepath.FromSlash(metadataRelPath))
				metadataDir := filepath.Dir(metadataAbsPath)
				if err := os.MkdirAll(metadataDir, 0o755); err != nil {
					t.Fatalf("create metadata dir: %v", err)
				}
				currentArchiveName := "alpha-current.tar.gz"
				extraArchiveName := "alpha-extra.tar.gz"
				currentArtifactPath := currentArchiveName
				extraArtifactPath := extraArchiveName
				localCurrentArchivePath = filepath.Join(metadataDir, currentArchiveName)
				if tc.remoteArchives {
					srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						switch r.URL.Path {
						case currentArchivePathURL:
							currentArchiveCount.Add(1)
							if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
								handlerErrs <- fmt.Errorf("current archive authorization = %q, want %q", got, "Bearer test-token")
								http.Error(w, "bad archive authorization", http.StatusBadRequest)
								return
							}
							w.Header().Set("Content-Type", "application/octet-stream")
							_, _ = w.Write(currentArchiveData)
						case extraArchivePathURL:
							extraArchiveCount.Add(1)
							if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
								handlerErrs <- fmt.Errorf("extra archive authorization = %q, want %q", got, "Bearer test-token")
								http.Error(w, "bad archive authorization", http.StatusBadRequest)
								return
							}
							w.Header().Set("Content-Type", "application/octet-stream")
							_, _ = w.Write(extraArchiveData)
						default:
							http.NotFound(w, r)
						}
					}))
					defer srv.Close()
					currentArtifactPath = srv.URL + currentArchivePathURL
					extraArtifactPath = srv.URL + extraArchivePathURL
				} else {
					if err := os.WriteFile(filepath.Join(metadataDir, currentArchiveName), currentArchiveData, 0o644); err != nil {
						t.Fatalf("write current archive: %v", err)
					}
					if err := os.WriteFile(filepath.Join(metadataDir, extraArchiveName), extraArchiveData, 0o644); err != nil {
						t.Fatalf("write extra archive: %v", err)
					}
				}
				writeProviderReleaseMetadataFile(t, metadataAbsPath, providerReleaseMetadata{
					Schema:        providerReleaseSchemaName,
					SchemaVersion: providerReleaseSchemaVersion,
					Package:       packageSource,
					Kind:          providermanifestv1.KindPlugin,
					Version:       version,
					Runtime:       providerReleaseRuntimeExecutable,
					Artifacts: map[string]providerReleaseArtifact{
						providerpkg.CurrentPlatformString(): {
							Path:   currentArtifactPath,
							SHA256: hex.EncodeToString(currentArchiveSHA[:]),
						},
						extraPlatformKey: {
							Path:   extraArtifactPath,
							SHA256: hex.EncodeToString(extraArchiveSHA[:]),
						},
					},
				})
				sourceValue = "./" + metadataRelPath
				wantSource = metadataRelPath
				if tc.remoteArchives {
					wantCurrentArchiveURL = srv.URL + currentArchivePathURL
					wantExtraArchiveURL = srv.URL + extraArchivePathURL
				} else {
					wantCurrentArchiveURL = currentArchiveName
					wantExtraArchiveURL = extraArchiveName
				}
			} else {
				srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case metadataPath:
						metadataCount.Add(1)
						if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
							handlerErrs <- fmt.Errorf("metadata authorization = %q, want %q", got, "Bearer test-token")
							http.Error(w, "bad metadata authorization", http.StatusBadRequest)
							return
						}
						metadata := providerReleaseMetadata{
							Schema:        providerReleaseSchemaName,
							SchemaVersion: providerReleaseSchemaVersion,
							Package:       packageSource,
							Kind:          providermanifestv1.KindPlugin,
							Version:       version,
							Runtime:       providerReleaseRuntimeExecutable,
							Artifacts: map[string]providerReleaseArtifact{
								providerpkg.CurrentPlatformString(): {
									Path:   filepath.Base(currentArchivePathURL),
									SHA256: hex.EncodeToString(currentArchiveSHA[:]),
								},
								extraPlatformKey: {
									Path:   filepath.Base(extraArchivePathURL),
									SHA256: hex.EncodeToString(extraArchiveSHA[:]),
								},
							},
						}
						data, err := yaml.Marshal(metadata)
						if err != nil {
							handlerErrs <- fmt.Errorf("marshal metadata: %v", err)
							http.Error(w, "metadata marshal failed", http.StatusInternalServerError)
							return
						}
						w.Header().Set("Content-Type", "application/yaml")
						_, _ = w.Write(data)
					case currentArchivePathURL:
						currentArchiveCount.Add(1)
						if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
							handlerErrs <- fmt.Errorf("current archive authorization = %q, want %q", got, "Bearer test-token")
							http.Error(w, "bad archive authorization", http.StatusBadRequest)
							return
						}
						w.Header().Set("Content-Type", "application/octet-stream")
						_, _ = w.Write(currentArchiveData)
					case extraArchivePathURL:
						extraArchiveCount.Add(1)
						if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
							handlerErrs <- fmt.Errorf("extra archive authorization = %q, want %q", got, "Bearer test-token")
							http.Error(w, "bad archive authorization", http.StatusBadRequest)
							return
						}
						w.Header().Set("Content-Type", "application/octet-stream")
						_, _ = w.Write(extraArchiveData)
					default:
						http.NotFound(w, r)
					}
				}))
				defer srv.Close()
				sourceValue = srv.URL + metadataPath + "?download=1"
				wantSource = sourceValue
				wantCurrentArchiveURL = srv.URL + currentArchivePathURL
				wantExtraArchiveURL = srv.URL + extraArchivePathURL
			}

			artifactsDir := filepath.Join(dir, "prepared-artifacts")
			configPath := filepath.Join(dir, "gestalt.yaml")
			configLines := []string{
				"apiVersion: " + tc.apiVersion,
			}
			if tc.localSource {
				indexedDBSource := "github.com/acme/tools/indexeddb-sqlite"
				indexedDBVersion := "0.0.1"
				indexedDBArchivePath := buildExecutableArchive(t, dir, "indexeddb-src", indexedDBSource, indexedDBVersion, providermanifestv1.KindIndexedDB, "indexeddb", "indexeddb-release-binary")
				indexedDBArchiveData, err := os.ReadFile(indexedDBArchivePath)
				if err != nil {
					t.Fatalf("read indexeddb archive: %v", err)
				}
				indexedDBArchiveSum := sha256.Sum256(indexedDBArchiveData)
				indexedDBRelPath := filepath.ToSlash(filepath.Join("providers", "indexeddb", "provider-release.yaml"))
				indexedDBAbsPath := filepath.Join(dir, filepath.FromSlash(indexedDBRelPath))
				indexedDBDir := filepath.Dir(indexedDBAbsPath)
				if err := os.MkdirAll(indexedDBDir, 0o755); err != nil {
					t.Fatalf("create indexeddb metadata dir: %v", err)
				}
				indexedDBArchiveName := "indexeddb-current.tar.gz"
				if err := os.WriteFile(filepath.Join(indexedDBDir, indexedDBArchiveName), indexedDBArchiveData, 0o644); err != nil {
					t.Fatalf("write indexeddb archive: %v", err)
				}
				writeProviderReleaseMetadataFile(t, indexedDBAbsPath, providerReleaseMetadata{
					Schema:        providerReleaseSchemaName,
					SchemaVersion: providerReleaseSchemaVersion,
					Package:       indexedDBSource,
					Kind:          providermanifestv1.KindIndexedDB,
					Version:       indexedDBVersion,
					Runtime:       providerReleaseRuntimeExecutable,
					Artifacts: map[string]providerReleaseArtifact{
						providerpkg.CurrentPlatformString(): {
							Path:   indexedDBArchiveName,
							SHA256: hex.EncodeToString(indexedDBArchiveSum[:]),
						},
					},
				})
				configLines = append(configLines,
					"providers:",
					"  indexeddb:",
					"    sqlite:",
					"      source: ./"+indexedDBRelPath,
					"      config:",
					"        path: "+filepath.Join(dir, "data.db"),
				)
			} else {
				configLines = append(configLines, strings.Split(strings.TrimSuffix(requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")), "\n"), "\n")...)
			}
			configLines = append(configLines,
				"plugins:",
				"  alpha:",
			)
			if !tc.localSource || tc.remoteArchives {
				configLines = append(configLines,
					"    source:",
				)
				if tc.localSource {
					configLines = append(configLines, "      path: "+sourceValue)
				} else {
					configLines = append(configLines, "      url: "+sourceValue)
				}
				configLines = append(configLines,
					"      auth:",
					"        token: test-token",
				)
			} else {
				configLines = append(configLines, "    source: "+sourceValue)
			}
			configLines = append(configLines,
				"server:",
				"  providers:",
				"    indexeddb: sqlite",
				"  artifactsDir: "+artifactsDir,
				"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			)
			configYAML := strings.Join(configLines, "\n") + "\n"
			if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}

			lc := NewLifecycle()
			lock, err := lc.InitAtPathWithPlatforms(configPath, "", []struct{ GOOS, GOARCH string }{
				{GOOS: extraPlatform.goos, GOARCH: extraPlatform.goarch},
			})
			if err == nil {
				if handlerErr := nextHandlerErr(); handlerErr != nil {
					t.Fatal(handlerErr)
				}
			}
			if err != nil {
				t.Fatalf("InitAtPathWithPlatforms: %v", err)
			}
			if handlerErr := nextHandlerErr(); handlerErr != nil {
				t.Fatal(handlerErr)
			}

			entry, ok := lock.Providers["alpha"]
			if !ok {
				t.Fatal(`lock.Providers["alpha"] not found`)
			}
			if entry.Source != wantSource {
				t.Fatalf("entry.Source = %q, want %q", entry.Source, wantSource)
			}
			if entry.Package != packageSource {
				t.Fatalf("entry.Package = %q, want %q", entry.Package, packageSource)
			}
			if entry.Kind != providermanifestv1.KindPlugin {
				t.Fatalf("entry.Kind = %q, want %q", entry.Kind, providermanifestv1.KindPlugin)
			}
			if entry.Runtime != providerReleaseRuntimeExecutable {
				t.Fatalf("entry.Runtime = %q, want %q", entry.Runtime, providerReleaseRuntimeExecutable)
			}
			if entry.Version != version {
				t.Fatalf("entry.Version = %q, want %q", entry.Version, version)
			}
			if got := entry.Archives[providerpkg.CurrentPlatformString()].URL; got != wantCurrentArchiveURL {
				t.Fatalf("current archive URL = %q, want %q", got, wantCurrentArchiveURL)
			}
			wantCurrentSHA := hex.EncodeToString(currentArchiveSHA[:])
			wantExtraSHA := hex.EncodeToString(extraArchiveSHA[:])
			if got := entry.Archives[providerpkg.CurrentPlatformString()].SHA256; got != wantCurrentSHA {
				t.Fatalf("current platform SHA256 = %q, want %q", got, wantCurrentSHA)
			}
			if got := entry.Archives[extraPlatformKey].SHA256; got != wantExtraSHA {
				t.Fatalf("extra platform SHA256 = %q, want %q", got, wantExtraSHA)
			}
			if !tc.localSource {
				if got := metadataCount.Load(); got != 1 {
					t.Fatalf("metadata request count = %d, want 1", got)
				}
				if got := currentArchiveCount.Load(); got != 1 {
					t.Fatalf("current archive request count = %d, want 1", got)
				}
				if got := extraArchiveCount.Load(); got != 0 {
					t.Fatalf("extra archive request count = %d, want 0", got)
				}
			}

			lockData, err := os.ReadFile(filepath.Join(dir, InitLockfileName))
			if err != nil {
				t.Fatalf("ReadFile lockfile: %v", err)
			}
			var diskLock providerLockfile
			if err := json.Unmarshal(lockData, &diskLock); err != nil {
				t.Fatalf("Unmarshal lockfile: %v", err)
			}
			diskEntry, ok := diskLock.Providers.Plugin["alpha"]
			if !ok {
				t.Fatal(`disk lock providers.plugin["alpha"] not found`)
			}
			if diskEntry.Package != packageSource {
				t.Fatalf("disk lock package = %q, want %q", diskEntry.Package, packageSource)
			}
			if diskEntry.Source != wantSource {
				t.Fatalf("disk lock source = %q, want %q", diskEntry.Source, wantSource)
			}
			if diskEntry.Runtime != providerReleaseRuntimeExecutable {
				t.Fatalf("disk lock runtime = %q, want %q", diskEntry.Runtime, providerReleaseRuntimeExecutable)
			}
			if diskEntry.Kind != providermanifestv1.KindPlugin {
				t.Fatalf("disk lock kind = %q, want %q", diskEntry.Kind, providermanifestv1.KindPlugin)
			}
			if got := diskEntry.Archives[providerpkg.CurrentPlatformString()].SHA256; got != wantCurrentSHA {
				t.Fatalf("disk lock current SHA256 = %q, want %q", got, wantCurrentSHA)
			}
			if got := diskEntry.Archives[extraPlatformKey].SHA256; got != wantExtraSHA {
				t.Fatalf("disk lock extra SHA256 = %q, want %q", got, wantExtraSHA)
			}
			if got := diskEntry.Archives[extraPlatformKey].URL; got != wantExtraArchiveURL {
				t.Fatalf("disk lock extra archive URL = %q, want %q", got, wantExtraArchiveURL)
			}

			pluginRoot := filepath.Join(artifactsDir, ".gestaltd", "providers", "alpha")
			if err := os.RemoveAll(pluginRoot); err != nil {
				t.Fatalf("RemoveAll plugin root: %v", err)
			}
			if tc.tamperLocalArchive {
				if err := os.WriteFile(localCurrentArchivePath, []byte("tampered-local-archive"), 0o644); err != nil {
					t.Fatalf("write tampered archive: %v", err)
				}
			}

			metadataBefore := metadataCount.Load()
			currentBefore := currentArchiveCount.Load()
			cfg, _, err := lc.LoadForExecutionAtPath(configPath, true)
			if tc.tamperLocalArchive {
				if handlerErr := nextHandlerErr(); handlerErr != nil {
					t.Fatal(handlerErr)
				}
				if err == nil || !strings.Contains(err.Error(), "digest mismatch") {
					t.Fatalf("LoadForExecutionAtPath(locked=true) error = %v, want digest mismatch", err)
				}
				return
			}
			if err != nil {
				if handlerErr := nextHandlerErr(); handlerErr != nil {
					t.Fatal(handlerErr)
				}
				t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
			}
			if handlerErr := nextHandlerErr(); handlerErr != nil {
				t.Fatal(handlerErr)
			}
			if !tc.localSource || tc.remoteArchives {
				if got := metadataCount.Load(); got != metadataBefore {
					t.Fatalf("metadata request count during locked load = %d, want %d", got, metadataBefore)
				}
				if got := currentArchiveCount.Load() - currentBefore; got != 1 {
					t.Fatalf("current archive request count during locked load = %d, want 1", got)
				}
				if got := extraArchiveCount.Load(); got != 0 {
					t.Fatalf("extra archive request count after locked load = %d, want 0", got)
				}
			}
			if cfg.Plugins["alpha"] == nil {
				t.Fatal(`cfg.Plugins["alpha"] = nil`)
				return
			}
			if cfg.Plugins["alpha"].ResolvedManifest == nil {
				t.Fatal(`cfg.Plugins["alpha"].ResolvedManifest = nil`)
				return
			}
			if cfg.Plugins["alpha"].ResolvedManifest.Source != packageSource {
				t.Fatalf("ResolvedManifest.Source = %q, want %q", cfg.Plugins["alpha"].ResolvedManifest.Source, packageSource)
			}
			executablePath := resolveLockPath(artifactsDir, entry.Executable)
			if cfg.Plugins["alpha"].Command != executablePath {
				t.Fatalf("plugin command = %q, want %q", cfg.Plugins["alpha"].Command, executablePath)
			}
		})
	}
}

func TestSourceWorkflowMetadataURLInitAndLockedLoad(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	packageSource := "github.com/acme/tools/workflow-runner"
	version := "2.3.4"
	archivePath := buildExecutableArchive(t, dir, "workflow-metadata-src", packageSource, version, providermanifestv1.KindWorkflow, "workflow-runner", "metadata-workflow-binary")
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read workflow archive: %v", err)
	}
	archiveSHA := sha256.Sum256(archiveData)

	var metadataCount atomic.Int64
	var archiveCount atomic.Int64
	handlerErrs := make(chan error, 4)
	nextHandlerErr := func() error {
		t.Helper()
		select {
		case err := <-handlerErrs:
			return err
		default:
			return nil
		}
	}

	metadataPath := "/providers/workflow/provider-release.yaml"
	archivePathURL := "/providers/workflow/workflow-runner.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case metadataPath:
			metadataCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("metadata authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad metadata authorization", http.StatusBadRequest)
				return
			}
			metadata := providerReleaseMetadata{
				Schema:        providerReleaseSchemaName,
				SchemaVersion: providerReleaseSchemaVersion,
				Package:       packageSource,
				Kind:          providermanifestv1.KindWorkflow,
				Version:       version,
				Runtime:       providerReleaseRuntimeExecutable,
				Artifacts: map[string]providerReleaseArtifact{
					providerpkg.CurrentPlatformString(): {
						Path:   filepath.Base(archivePathURL),
						SHA256: hex.EncodeToString(archiveSHA[:]),
					},
				},
			}
			data, err := yaml.Marshal(metadata)
			if err != nil {
				handlerErrs <- fmt.Errorf("marshal metadata: %v", err)
				http.Error(w, "metadata marshal failed", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write(data)
		case archivePathURL:
			archiveCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("archive authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad archive authorization", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(archiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configPath := filepath.Join(dir, "gestalt.yaml")
	configYAML := "apiVersion: " + config.ConfigAPIVersion + "\n" + requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")) + strings.Join([]string{
		"  workflow:",
		"    runner:",
		"      source:",
		"        url: " + srv.URL + metadataPath,
		"        auth:",
		"          token: test-token",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle()
	lock, err := lc.InitAtPath(configPath)
	if err == nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
	}
	if err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}

	entry, ok := lock.Workflows["runner"]
	if !ok {
		t.Fatal(`lock.Workflows["runner"] not found`)
	}
	if entry.Package != packageSource {
		t.Fatalf("entry.Package = %q, want %q", entry.Package, packageSource)
	}
	if entry.Kind != providermanifestv1.KindWorkflow {
		t.Fatalf("entry.Kind = %q, want %q", entry.Kind, providermanifestv1.KindWorkflow)
	}
	if entry.Runtime != providerReleaseRuntimeExecutable {
		t.Fatalf("entry.Runtime = %q, want %q", entry.Runtime, providerReleaseRuntimeExecutable)
	}
	if entry.Version != version {
		t.Fatalf("entry.Version = %q, want %q", entry.Version, version)
	}
	if got := metadataCount.Load(); got != 1 {
		t.Fatalf("metadata request count = %d, want 1", got)
	}
	if got := archiveCount.Load(); got != 1 {
		t.Fatalf("archive request count = %d, want 1", got)
	}

	workflowRoot := filepath.Join(artifactsDir, filepath.FromSlash(PreparedWorkflowDir), "runner")
	if err := os.RemoveAll(workflowRoot); err != nil {
		t.Fatalf("RemoveAll workflow root: %v", err)
	}

	metadataBefore := metadataCount.Load()
	archiveBefore := archiveCount.Load()
	cfg, _, err := lc.LoadForExecutionAtPath(configPath, true)
	if err == nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
	}
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if got := metadataCount.Load(); got != metadataBefore {
		t.Fatalf("metadata request count during locked load = %d, want %d", got, metadataBefore)
	}
	if got := archiveCount.Load() - archiveBefore; got != 1 {
		t.Fatalf("archive request count during locked load = %d, want 1", got)
	}
	workflow := cfg.Providers.Workflow["runner"]
	if workflow == nil || workflow.ResolvedManifest == nil {
		t.Fatalf("workflow resolved manifest = %+v", workflow)
		return
	}
	if got := workflow.ResolvedManifest.Kind; got != providermanifestv1.KindWorkflow {
		t.Fatalf("workflow manifest kind = %q, want %q", got, providermanifestv1.KindWorkflow)
	}
	if got := workflow.Command; got != resolveLockPath(artifactsDir, entry.Executable) {
		t.Fatalf("workflow command = %q, want %q", got, resolveLockPath(artifactsDir, entry.Executable))
	}
}

func TestSourceExternalCredentialsMetadataURLInitAndLockedLoad(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	packageSource := "github.com/acme/tools/external-credentials-runner"
	version := "1.4.2"
	archivePath := buildExecutableArchive(t, dir, "external-credentials-src", packageSource, version, providermanifestv1.KindExternalCredentials, "external-credentials-runner", "metadata-external-credentials-binary")
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read external credentials archive: %v", err)
	}
	archiveSHA := sha256.Sum256(archiveData)

	var metadataCount atomic.Int64
	var archiveCount atomic.Int64
	handlerErrs := make(chan error, 4)
	nextHandlerErr := func() error {
		t.Helper()
		select {
		case err := <-handlerErrs:
			return err
		default:
			return nil
		}
	}

	metadataPath := "/providers/external-credentials/provider-release.yaml"
	archivePathURL := "/providers/external-credentials/external-credentials-runner.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case metadataPath:
			metadataCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("metadata authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad metadata authorization", http.StatusBadRequest)
				return
			}
			metadata := providerReleaseMetadata{
				Schema:        providerReleaseSchemaName,
				SchemaVersion: providerReleaseSchemaVersion,
				Package:       packageSource,
				Kind:          providermanifestv1.KindExternalCredentials,
				Version:       version,
				Runtime:       providerReleaseRuntimeExecutable,
				Artifacts: map[string]providerReleaseArtifact{
					providerpkg.CurrentPlatformString(): {
						Path:   filepath.Base(archivePathURL),
						SHA256: hex.EncodeToString(archiveSHA[:]),
					},
				},
			}
			data, err := yaml.Marshal(metadata)
			if err != nil {
				handlerErrs <- fmt.Errorf("marshal metadata: %v", err)
				http.Error(w, "metadata marshal failed", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write(data)
		case archivePathURL:
			archiveCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("archive authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad archive authorization", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(archiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configPath := filepath.Join(dir, "gestalt.yaml")
	configYAML := "apiVersion: " + config.ConfigAPIVersion + "\n" + requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")) + strings.Join([]string{
		"  externalCredentials:",
		"    runner:",
		"      source:",
		"        url: " + srv.URL + metadataPath,
		"        auth:",
		"          token: test-token",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"    externalCredentials: runner",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle()
	lock, err := lc.InitAtPath(configPath)
	if err == nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
	}
	if err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}

	entry, ok := lock.ExternalCredentials["runner"]
	if !ok {
		t.Fatal(`lock.ExternalCredentials["runner"] not found`)
	}
	if entry.Package != packageSource {
		t.Fatalf("entry.Package = %q, want %q", entry.Package, packageSource)
	}
	if entry.Kind != providermanifestv1.KindExternalCredentials {
		t.Fatalf("entry.Kind = %q, want %q", entry.Kind, providermanifestv1.KindExternalCredentials)
	}
	if entry.Runtime != providerReleaseRuntimeExecutable {
		t.Fatalf("entry.Runtime = %q, want %q", entry.Runtime, providerReleaseRuntimeExecutable)
	}
	if entry.Version != version {
		t.Fatalf("entry.Version = %q, want %q", entry.Version, version)
	}
	if got := metadataCount.Load(); got != 1 {
		t.Fatalf("metadata request count = %d, want 1", got)
	}
	if got := archiveCount.Load(); got != 1 {
		t.Fatalf("archive request count = %d, want 1", got)
	}

	externalCredentialsRoot := filepath.Join(artifactsDir, filepath.FromSlash(PreparedExternalCredentialsDir), "runner")
	if err := os.RemoveAll(externalCredentialsRoot); err != nil {
		t.Fatalf("RemoveAll external credentials root: %v", err)
	}

	metadataBefore := metadataCount.Load()
	archiveBefore := archiveCount.Load()
	cfg, _, err := lc.LoadForExecutionAtPath(configPath, true)
	if err == nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
	}
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if got := metadataCount.Load(); got != metadataBefore {
		t.Fatalf("metadata request count during locked load = %d, want %d", got, metadataBefore)
	}
	if got := archiveCount.Load() - archiveBefore; got != 1 {
		t.Fatalf("archive request count during locked load = %d, want 1", got)
	}
	externalCredentials := cfg.Providers.ExternalCredentials["runner"]
	if externalCredentials == nil || externalCredentials.ResolvedManifest == nil {
		t.Fatalf("external credentials resolved manifest = %+v", externalCredentials)
		return
	}
	if got := externalCredentials.ResolvedManifest.Kind; got != providermanifestv1.KindExternalCredentials {
		t.Fatalf("external credentials manifest kind = %q, want %q", got, providermanifestv1.KindExternalCredentials)
	}
	if got := externalCredentials.Command; got != resolveLockPath(artifactsDir, entry.Executable) {
		t.Fatalf("external credentials command = %q, want %q", got, resolveLockPath(artifactsDir, entry.Executable))
	}
}

func TestSourceUIMetadataURLInitAndLockedLoad(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	packageSource := "github.com/acme/tools/roadmap-ui"
	version := "0.9.1"
	archivePath := mustBuildManagedProviderPackage(t, dir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindUI,
		Source:      packageSource,
		Version:     version,
		DisplayName: "Roadmap UI",
		Spec: &providermanifestv1.Spec{
			AssetRoot: "dist",
		},
	}, map[string]string{
		"dist/index.html": "<html>roadmap</html>",
	}, false)
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read ui archive: %v", err)
	}
	archiveSHA := sha256.Sum256(archiveData)

	var metadataCount atomic.Int64
	var archiveCount atomic.Int64
	handlerErrs := make(chan error, 4)
	nextHandlerErr := func() error {
		t.Helper()
		select {
		case err := <-handlerErrs:
			return err
		default:
			return nil
		}
	}

	metadataPath := "/providers/roadmap/provider-release.yaml"
	archivePathURL := "/providers/roadmap/roadmap-ui.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case metadataPath:
			metadataCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("metadata authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad metadata authorization", http.StatusBadRequest)
				return
			}
			metadata := providerReleaseMetadata{
				Schema:        providerReleaseSchemaName,
				SchemaVersion: providerReleaseSchemaVersion,
				Package:       packageSource,
				Kind:          providermanifestv1.KindUI,
				Version:       version,
				Runtime:       providerReleaseRuntimeUI,
				Artifacts: map[string]providerReleaseArtifact{
					providerpkg.CurrentPlatformString(): {
						Path:   filepath.Base(archivePathURL),
						SHA256: hex.EncodeToString(archiveSHA[:]),
					},
				},
			}
			data, err := yaml.Marshal(metadata)
			if err != nil {
				handlerErrs <- fmt.Errorf("marshal metadata: %v", err)
				http.Error(w, "metadata marshal failed", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write(data)
		case archivePathURL:
			archiveCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("archive authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad archive authorization", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(archiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configPath := filepath.Join(dir, "gestalt.yaml")
	configYAML := "apiVersion: " + config.ConfigAPIVersion + "\n" + requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")) + strings.Join([]string{
		"  ui:",
		"    roadmap:",
		"      source:",
		"        url: " + srv.URL + metadataPath + "?download=1",
		"        auth:",
		"          token: test-token",
		"      path: /roadmap",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle()
	lock, err := lc.InitAtPath(configPath)
	if err == nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
	}
	if err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}

	entry, ok := lock.UIs["roadmap"]
	if !ok {
		t.Fatal(`lock.UIs["roadmap"] not found`)
	}
	if entry.Source != srv.URL+metadataPath+"?download=1" {
		t.Fatalf("entry.Source = %q, want %q", entry.Source, srv.URL+metadataPath+"?download=1")
	}
	if entry.Package != packageSource {
		t.Fatalf("entry.Package = %q, want %q", entry.Package, packageSource)
	}
	if entry.Kind != providermanifestv1.KindUI {
		t.Fatalf("entry.Kind = %q, want %q", entry.Kind, providermanifestv1.KindUI)
	}
	if entry.Runtime != providerReleaseRuntimeUI {
		t.Fatalf("entry.Runtime = %q, want %q", entry.Runtime, providerReleaseRuntimeUI)
	}
	if entry.Version != version {
		t.Fatalf("entry.Version = %q, want %q", entry.Version, version)
	}
	if got := metadataCount.Load(); got != 1 {
		t.Fatalf("metadata request count = %d, want 1", got)
	}
	if got := archiveCount.Load(); got != 1 {
		t.Fatalf("archive request count = %d, want 1", got)
	}

	uiRoot := filepath.Join(artifactsDir, filepath.FromSlash(PreparedUIDir), "roadmap")
	if err := os.RemoveAll(uiRoot); err != nil {
		t.Fatalf("RemoveAll ui root: %v", err)
	}

	metadataBefore := metadataCount.Load()
	archiveBefore := archiveCount.Load()
	cfg, _, err := lc.LoadForExecutionAtPath(configPath, true)
	if err == nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
	}
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if got := metadataCount.Load(); got != metadataBefore {
		t.Fatalf("metadata request count during locked load = %d, want %d", got, metadataBefore)
	}
	if got := archiveCount.Load() - archiveBefore; got != 1 {
		t.Fatalf("archive request count during locked load = %d, want 1", got)
	}
	ui := cfg.Providers.UI["roadmap"]
	if ui == nil || ui.ResolvedManifest == nil {
		t.Fatalf("ui resolved manifest = %+v", ui)
		return
	}
	if got := ui.ResolvedManifest.Kind; got != providermanifestv1.KindUI {
		t.Fatalf("ui manifest kind = %q, want %q", got, providermanifestv1.KindUI)
	}
	if got := ui.ResolvedManifest.Source; got != packageSource {
		t.Fatalf("ui manifest source = %q, want %q", got, packageSource)
	}
	if got := ui.ResolvedAssetRoot; got != resolveLockPath(artifactsDir, entry.AssetRoot) {
		t.Fatalf("ui asset root = %q, want %q", got, resolveLockPath(artifactsDir, entry.AssetRoot))
	}
}

func TestSourcePluginInitRejectsMetadataSourceManifestMismatch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	packageSource := "github.com/acme/tools/gadget"
	version := "2.0.0"

	archivePath := buildV2Archive(t, dir, "github.com/acme/tools/other-gadget", version, "fake-gadget-binary")
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	archiveSHA := sha256.Sum256(archiveData)

	metadataPath := "/providers/gadget/provider-release.yaml"
	archivePathURL := "/providers/gadget/gadget-current.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case metadataPath:
			metadata := providerReleaseMetadata{
				Schema:        providerReleaseSchemaName,
				SchemaVersion: providerReleaseSchemaVersion,
				Package:       packageSource,
				Kind:          providermanifestv1.KindPlugin,
				Version:       version,
				Runtime:       providerReleaseRuntimeExecutable,
				Artifacts: map[string]providerReleaseArtifact{
					providerpkg.CurrentPlatformString(): {
						Path:   filepath.Base(archivePathURL),
						SHA256: hex.EncodeToString(archiveSHA[:]),
					},
				},
			}
			data, err := yaml.Marshal(metadata)
			if err != nil {
				http.Error(w, "metadata marshal failed", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write(data)
		case archivePathURL:
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(archiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configYAML := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")) + strings.Join([]string{
		"apiVersion: " + config.ConfigAPIVersion,
		"plugins:",
		"  gadget:",
		"    source: " + srv.URL + metadataPath,
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"

	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle()
	_, err = lc.InitAtPath(configPath)
	if err == nil {
		t.Fatal("InitAtPath unexpectedly succeeded")
		return
	}
	if !strings.Contains(err.Error(), `manifest source "github.com/acme/tools/other-gadget" does not match metadata package "github.com/acme/tools/gadget"`) {
		t.Fatalf("InitAtPath error = %v, want manifest source mismatch", err)
	}
}

func TestSourcePluginMetadataURLUsesGenericAuthenticatedFetch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	const packageSource = testSource
	const version = testVersion

	currentArchivePath := buildV2Archive(t, dir, packageSource, version, "metadata-github-asset-plugin-binary")
	currentArchiveData, err := os.ReadFile(currentArchivePath)
	if err != nil {
		t.Fatalf("read current archive: %v", err)
	}
	currentArchiveSHA := sha256.Sum256(currentArchiveData)

	var metadataCount atomic.Int64
	var archiveCount atomic.Int64
	handlerErrs := make(chan error, 4)
	nextHandlerErr := func() error {
		t.Helper()
		select {
		case err := <-handlerErrs:
			return err
		default:
			return nil
		}
	}

	metadataPath := "/releases/assets/999"
	archivePath := "/releases/assets/123"
	metadataURL := ""
	archiveURL := ""
	var currentMu sync.RWMutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case metadataPath:
			metadataCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("metadata authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad metadata authorization", http.StatusBadRequest)
				return
			}
			if got := r.Header.Get("Accept"); got != "application/octet-stream" {
				handlerErrs <- fmt.Errorf("metadata accept = %q, want %q", got, "application/octet-stream")
				http.Error(w, "bad metadata accept", http.StatusBadRequest)
				return
			}
			currentMu.RLock()
			currentArchiveURL := archiveURL
			currentMu.RUnlock()
			metadata := providerReleaseMetadata{
				Schema:        providerReleaseSchemaName,
				SchemaVersion: providerReleaseSchemaVersion,
				Package:       packageSource,
				Kind:          providermanifestv1.KindPlugin,
				Version:       version,
				Runtime:       providerReleaseRuntimeExecutable,
				Artifacts: map[string]providerReleaseArtifact{
					providerpkg.CurrentPlatformString(): {
						Path:   currentArchiveURL,
						SHA256: hex.EncodeToString(currentArchiveSHA[:]),
					},
				},
			}
			data, err := yaml.Marshal(metadata)
			if err != nil {
				handlerErrs <- fmt.Errorf("marshal metadata: %v", err)
				http.Error(w, "metadata marshal failed", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write(data)
		case archivePath:
			archiveCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("archive authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad archive authorization", http.StatusBadRequest)
				return
			}
			if got := r.Header.Get("Accept"); got != "application/octet-stream" {
				handlerErrs <- fmt.Errorf("archive accept = %q, want %q", got, "application/octet-stream")
				http.Error(w, "bad archive accept", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(currentArchiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	currentMu.Lock()
	metadataURL = srv.URL + metadataPath
	archiveURL = srv.URL + archivePath
	currentMu.Unlock()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configPath := filepath.Join(dir, "gestalt.yaml")
	configYAML := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")) + strings.Join([]string{
		"apiVersion: " + config.ConfigAPIVersion,
		"plugins:",
		"  alpha:",
		"    source:",
		"      url: " + metadataURL,
		"      auth:",
		"        token: test-token",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle()
	lock, err := lc.InitAtPath(configPath)
	if err == nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
	}
	if err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}

	entry, ok := lock.Providers["alpha"]
	if !ok {
		t.Fatal(`lock.Providers["alpha"] not found`)
	}
	if got := entry.Archives[providerpkg.CurrentPlatformString()].URL; got != archiveURL {
		t.Fatalf("current archive URL = %q, want %q", got, archiveURL)
	}
	if got := metadataCount.Load(); got != 1 {
		t.Fatalf("metadata request count = %d, want 1", got)
	}
	if got := archiveCount.Load(); got != 1 {
		t.Fatalf("archive request count = %d, want 1", got)
	}

	pluginRoot := filepath.Join(artifactsDir, ".gestaltd", "providers", "alpha")
	if err := os.RemoveAll(pluginRoot); err != nil {
		t.Fatalf("RemoveAll plugin root: %v", err)
	}

	metadataBefore := metadataCount.Load()
	archiveBefore := archiveCount.Load()
	cfg, _, err := lc.LoadForExecutionAtPath(configPath, true)
	if err == nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
	}
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if got := metadataCount.Load(); got != metadataBefore {
		t.Fatalf("metadata request count during locked load = %d, want %d", got, metadataBefore)
	}
	if got := archiveCount.Load() - archiveBefore; got != 1 {
		t.Fatalf("archive request count during locked load = %d, want 1", got)
	}
	if cfg.Plugins["alpha"] == nil || cfg.Plugins["alpha"].ResolvedManifest == nil {
		t.Fatal("resolved metadata plugin manifest missing after locked load")
	}
}

func TestSourcePluginGitHubReleaseSourceUsesResolvedAssetURL(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	archivePath := buildV2Archive(t, dir, testSource, testVersion, testBinary)
	currentArchiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	currentArchiveSHA := sha256.Sum256(currentArchiveData)

	const (
		repo         = "valon-technologies/toolshed"
		tag          = "plugins/workplace-hub/v0.0.1-alpha.1"
		metadataID   = int64(101)
		archiveID    = int64(202)
		metadataName = "provider-release.yaml"
		archiveName  = "alpha-current.tar.gz"
	)

	archiveAssetURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/assets/%d", repo, archiveID)
	logicalSource := "github-release://github.com/valon-technologies/toolshed?asset=provider-release.yaml&tag=plugins%2Fworkplace-hub%2Fv0.0.1-alpha.1"

	var releaseCount atomic.Int64
	var metadataCount atomic.Int64
	var archiveCount atomic.Int64
	handlerErrs := make(chan error, 8)
	nextHandlerErr := func() error {
		t.Helper()
		select {
		case err := <-handlerErrs:
			return err
		default:
			return nil
		}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		escapedPath := r.URL.EscapedPath()
		switch {
		case escapedPath == "/repos/valon-technologies/toolshed/releases/tags/"+url.PathEscape(tag) || r.URL.Path == "/repos/valon-technologies/toolshed/releases/tags/"+tag:
			releaseCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("release authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad release authorization", http.StatusBadRequest)
				return
			}
			if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
				handlerErrs <- fmt.Errorf("release accept = %q, want %q", got, "application/vnd.github+json")
				http.Error(w, "bad release accept", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"assets":[{"id":%d,"name":"%s"},{"id":%d,"name":"%s"}]}`, metadataID, metadataName, archiveID, archiveName)
		case escapedPath == fmt.Sprintf("/repos/%s/releases/assets/%d", repo, metadataID):
			metadataCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("metadata authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad metadata authorization", http.StatusBadRequest)
				return
			}
			if got := r.Header.Get("Accept"); got != "application/octet-stream" {
				handlerErrs <- fmt.Errorf("metadata accept = %q, want %q", got, "application/octet-stream")
				http.Error(w, "bad metadata accept", http.StatusBadRequest)
				return
			}
			metadata := providerReleaseMetadata{
				Schema:        providerReleaseSchemaName,
				SchemaVersion: providerReleaseSchemaVersion,
				Package:       testSource,
				Kind:          providermanifestv1.KindPlugin,
				Version:       testVersion,
				Runtime:       providerReleaseRuntimeExecutable,
				Artifacts: map[string]providerReleaseArtifact{
					providerpkg.CurrentPlatformString(): {
						Path:   "./" + archiveName,
						SHA256: hex.EncodeToString(currentArchiveSHA[:]),
					},
				},
			}
			data, err := yaml.Marshal(metadata)
			if err != nil {
				handlerErrs <- fmt.Errorf("marshal metadata: %v", err)
				http.Error(w, "metadata marshal failed", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write(data)
		case escapedPath == fmt.Sprintf("/repos/%s/releases/assets/%d", repo, archiveID):
			archiveCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("archive authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad archive authorization", http.StatusBadRequest)
				return
			}
			if got := r.Header.Get("Accept"); got != "application/octet-stream" {
				handlerErrs <- fmt.Errorf("archive accept = %q, want %q", got, "application/octet-stream")
				http.Error(w, "bad archive accept", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(currentArchiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	baseURL, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	serverClient := srv.Client()
	client := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			clone := req.Clone(req.Context())
			clone.URL.Scheme = baseURL.Scheme
			clone.URL.Host = baseURL.Host
			clone.Host = baseURL.Host
			return serverClient.Transport.RoundTrip(clone)
		}),
	}

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configPath := filepath.Join(dir, "gestalt.yaml")
	configYAML := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")) + strings.Join([]string{
		"apiVersion: " + config.ConfigAPIVersion,
		"plugins:",
		"  alpha:",
		"    source:",
		"      githubRelease:",
		"        repo: " + repo,
		"        tag: " + tag,
		"        asset: " + metadataName,
		"      auth:",
		"        token: test-token",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle().WithHTTPClient(client)
	lock, err := lc.InitAtPath(configPath)
	if err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}
	entry, ok := lock.Providers["alpha"]
	if !ok {
		t.Fatal(`lock.Providers["alpha"] not found`)
	}
	if entry.Source != logicalSource {
		t.Fatalf("lock source = %q, want %q", entry.Source, logicalSource)
	}
	if got := entry.Archives[providerpkg.CurrentPlatformString()].URL; got != archiveAssetURL {
		t.Fatalf("current archive URL = %q, want %q", got, archiveAssetURL)
	}
	if got := releaseCount.Load(); got != 1 {
		t.Fatalf("release request count = %d, want 1", got)
	}
	if got := metadataCount.Load(); got != 1 {
		t.Fatalf("metadata request count = %d, want 1", got)
	}
	if got := archiveCount.Load(); got != 1 {
		t.Fatalf("archive request count = %d, want 1", got)
	}
	if err := nextHandlerErr(); err != nil {
		t.Fatal(err)
	}
}

func TestSourcePluginMetadataURLRetriesTransientRemoteMetadataFailure(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	archivePath := buildV2Archive(t, dir, testSource, testVersion, testBinary)
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	archiveSHA := sha256.Sum256(archiveData)

	var metadataCount atomic.Int64
	var archiveCount atomic.Int64
	handlerErrs := make(chan error, 4)
	nextHandlerErr := func() error {
		t.Helper()
		select {
		case err := <-handlerErrs:
			return err
		default:
			return nil
		}
	}

	const metadataPath = "/providers/alpha/provider-release.yaml"
	const archivePathURL = "/providers/alpha/alpha-current.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case metadataPath:
			count := metadataCount.Add(1)
			if count <= 2 {
				http.Error(w, "bad gateway", http.StatusBadGateway)
				return
			}
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("metadata authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad metadata authorization", http.StatusBadRequest)
				return
			}
			if got := r.Header.Get("Accept"); got != "application/octet-stream" {
				handlerErrs <- fmt.Errorf("metadata accept = %q, want %q", got, "application/octet-stream")
				http.Error(w, "bad metadata accept", http.StatusBadRequest)
				return
			}
			metadata := providerReleaseMetadata{
				Schema:        providerReleaseSchemaName,
				SchemaVersion: providerReleaseSchemaVersion,
				Package:       testSource,
				Kind:          providermanifestv1.KindPlugin,
				Version:       testVersion,
				Runtime:       providerReleaseRuntimeExecutable,
				Artifacts: map[string]providerReleaseArtifact{
					providerpkg.CurrentPlatformString(): {
						Path:   "./alpha-current.tar.gz",
						SHA256: hex.EncodeToString(archiveSHA[:]),
					},
				},
			}
			data, err := yaml.Marshal(metadata)
			if err != nil {
				handlerErrs <- fmt.Errorf("marshal metadata: %v", err)
				http.Error(w, "metadata marshal failed", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write(data)
		case archivePathURL:
			archiveCount.Add(1)
			_, _ = w.Write(archiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configPath := filepath.Join(dir, "gestalt.yaml")
	configYAML := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")) + strings.Join([]string{
		"apiVersion: " + config.ConfigAPIVersion,
		"plugins:",
		"  alpha:",
		"    source:",
		"      url: " + srv.URL + metadataPath,
		"      auth:",
		"        token: test-token",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle()
	if _, err := lc.InitAtPath(configPath); err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if got := metadataCount.Load(); got != 3 {
		t.Fatalf("metadata request count = %d, want 3", got)
	}
	if got := archiveCount.Load(); got != 1 {
		t.Fatalf("archive request count = %d, want 1", got)
	}
}

func TestSourcePluginMetadataURLRejectsOversizedRemoteMetadata(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	var metadataCount atomic.Int64
	handlerErrs := make(chan error, 2)
	nextHandlerErr := func() error {
		t.Helper()
		select {
		case err := <-handlerErrs:
			return err
		default:
			return nil
		}
	}

	metadataPath := "/releases/assets/999"
	oversizedBody := bytes.Repeat([]byte("x"), providerReleaseMetadataMaxBytes+1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != metadataPath {
			http.NotFound(w, r)
			return
		}
		metadataCount.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			handlerErrs <- fmt.Errorf("metadata authorization = %q, want %q", got, "Bearer test-token")
			http.Error(w, "bad metadata authorization", http.StatusBadRequest)
			return
		}
		if got := r.Header.Get("Accept"); got != "application/octet-stream" {
			handlerErrs <- fmt.Errorf("metadata accept = %q, want %q", got, "application/octet-stream")
			http.Error(w, "bad metadata accept", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(oversizedBody)
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configPath := filepath.Join(dir, "gestalt.yaml")
	configYAML := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")) + strings.Join([]string{
		"apiVersion: " + config.ConfigAPIVersion,
		"plugins:",
		"  alpha:",
		"    source:",
		"      url: " + srv.URL + metadataPath,
		"      auth:",
		"        token: test-token",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle()
	_, err := lc.InitAtPath(configPath)
	if err == nil {
		t.Fatal("InitAtPath unexpectedly succeeded")
		return
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("provider release metadata exceeds %d byte limit", providerReleaseMetadataMaxBytes)) {
		t.Fatalf("InitAtPath error = %v, want metadata size limit", err)
	}
	if got := metadataCount.Load(); got != 1 {
		t.Fatalf("metadata request count = %d, want 1", got)
	}
}

func TestSourcePluginMetadataURLUnlockedLoadRefreshesMutableMetadata(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	const packageSource = testSource
	const initialVersion = "1.0.0"
	const updatedVersion = "1.0.1"

	initialArchivePath := buildV2Archive(t, dir, packageSource, initialVersion, "metadata-mutable-plugin-v1")
	initialArchiveData, err := os.ReadFile(initialArchivePath)
	if err != nil {
		t.Fatalf("read initial archive: %v", err)
	}
	initialArchiveSHA := sha256.Sum256(initialArchiveData)

	updatedArchivePath := buildV2Archive(t, dir, packageSource, updatedVersion, "metadata-mutable-plugin-v2")
	updatedArchiveData, err := os.ReadFile(updatedArchivePath)
	if err != nil {
		t.Fatalf("read updated archive: %v", err)
	}
	updatedArchiveSHA := sha256.Sum256(updatedArchiveData)

	var metadataCount atomic.Int64
	var archiveCount atomic.Int64
	var currentMu sync.RWMutex
	currentVersion := initialVersion
	currentArchiveData := initialArchiveData
	currentArchiveSHA := initialArchiveSHA

	handlerErrs := make(chan error, 4)
	nextHandlerErr := func() error {
		t.Helper()
		select {
		case err := <-handlerErrs:
			return err
		default:
			return nil
		}
	}

	metadataPath := "/providers/alpha/provider-release.yaml"
	currentArchivePathURL := "/providers/alpha/alpha-current.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case metadataPath:
			metadataCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("metadata authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad metadata authorization", http.StatusBadRequest)
				return
			}
			currentMu.RLock()
			version := currentVersion
			archiveSHA := currentArchiveSHA
			currentMu.RUnlock()
			metadata := providerReleaseMetadata{
				Schema:        providerReleaseSchemaName,
				SchemaVersion: providerReleaseSchemaVersion,
				Package:       packageSource,
				Kind:          providermanifestv1.KindPlugin,
				Version:       version,
				Runtime:       providerReleaseRuntimeExecutable,
				Artifacts: map[string]providerReleaseArtifact{
					providerpkg.CurrentPlatformString(): {
						Path:   filepath.Base(currentArchivePathURL),
						SHA256: hex.EncodeToString(archiveSHA[:]),
					},
				},
			}
			data, err := yaml.Marshal(metadata)
			if err != nil {
				handlerErrs <- fmt.Errorf("marshal metadata: %v", err)
				http.Error(w, "metadata marshal failed", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write(data)
		case currentArchivePathURL:
			archiveCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("archive authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad archive authorization", http.StatusBadRequest)
				return
			}
			currentMu.RLock()
			archiveData := currentArchiveData
			currentMu.RUnlock()
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(archiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configPath := filepath.Join(dir, "gestalt.yaml")
	configYAML := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")) + strings.Join([]string{
		"apiVersion: " + config.ConfigAPIVersion,
		"plugins:",
		"  alpha:",
		"    source:",
		"      url: " + srv.URL + metadataPath,
		"      auth:",
		"        token: test-token",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle()
	lock, err := lc.InitAtPath(configPath)
	if err == nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
	}
	if err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if got := lock.Providers["alpha"].Version; got != initialVersion {
		t.Fatalf("initial lock version = %q, want %q", got, initialVersion)
	}

	currentMu.Lock()
	currentVersion = updatedVersion
	currentArchiveData = updatedArchiveData
	currentArchiveSHA = updatedArchiveSHA
	currentMu.Unlock()

	metadataBefore := metadataCount.Load()
	archiveBefore := archiveCount.Load()
	cfg, _, err := lc.LoadForExecutionAtPath(configPath, false)
	if err == nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
	}
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=false): %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if got := metadataCount.Load(); got <= metadataBefore {
		t.Fatalf("metadata request count after unlocked load = %d, want > %d", got, metadataBefore)
	}
	if got := archiveCount.Load(); got <= archiveBefore {
		t.Fatalf("archive request count after unlocked load = %d, want > %d", got, archiveBefore)
	}
	if cfg.Plugins["alpha"] == nil || cfg.Plugins["alpha"].ResolvedManifest == nil {
		t.Fatal("resolved metadata plugin manifest missing after unlocked refresh")
		return
	}
	if got := cfg.Plugins["alpha"].ResolvedManifest.Version; got != updatedVersion {
		t.Fatalf("resolved manifest version after unlocked refresh = %q, want %q", got, updatedVersion)
	}

	updatedLock, err := ReadLockfile(filepath.Join(dir, InitLockfileName))
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	if got := updatedLock.Providers["alpha"].Version; got != updatedVersion {
		t.Fatalf("updated lock version = %q, want %q", got, updatedVersion)
	}
}

func TestMaterializeLockedComponent_AllowsGenericDeclarativeTelemetryAndAuditPackages(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	const source = "github.com/acme/providers/declarative"
	const version = "1.0.0"

	pkgPath := mustBuildManagedProviderPackage(t, dir, &providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindPlugin,
		Source:  source,
		Version: version,
		Spec: &providermanifestv1.Spec{
			Surfaces: &providermanifestv1.ProviderSurfaces{
				REST: &providermanifestv1.RESTSurface{
					BaseURL: "https://api.example.com",
					Operations: []providermanifestv1.ProviderOperation{
						{Name: "ping", Method: "GET", Path: "/ping"},
					},
				},
			},
		},
	}, nil, false)
	pkgData, err := os.ReadFile(pkgPath)
	if err != nil {
		t.Fatalf("read package: %v", err)
	}
	pkgSum := sha256.Sum256(pkgData)

	lc := NewLifecycle()

	for _, kind := range []string{providerLockKindTelemetry, providerLockKindAudit} {
		kind := kind
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/octet-stream")
				_, _ = w.Write(pkgData)
			}))
			defer srv.Close()

			entry := LockEntry{
				Source:  source,
				Version: version,
				Archives: map[string]LockArchive{
					platformKeyGeneric: {
						URL:    srv.URL,
						SHA256: hex.EncodeToString(pkgSum[:]),
					},
				},
			}
			providerEntry := &config.ProviderEntry{
				Source: config.NewMetadataSource("https://example.invalid/github-com-acme-providers-declarative/v1.0.0/provider-release.yaml"),
			}
			destDir := filepath.Join(dir, kind)
			if err := lc.materializeLockedComponent(context.Background(), initPaths{}, kind, "default", providerEntry, entry, destDir, true); err != nil {
				t.Fatalf("materializeLockedComponent: %v", err)
			}
			install, err := inspectPreparedInstall(destDir)
			if err != nil {
				t.Fatalf("inspectPreparedInstall: %v", err)
			}
			if install.manifest == nil || !install.manifest.IsDeclarativeOnlyProvider() {
				t.Fatalf("prepared manifest = %#v, want declarative manifest", install.manifest)
			}
		})
	}
}

func TestSourcePluginLoadForExecution_RehydratesWhenCachedManifestVersionMismatchesLock(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := "github.com/acme/tools/gadget"
	version := "2.0.0"
	binaryContent := "fake-gadget-binary"

	archivePath := buildV2Archive(t, dir, source, version, binaryContent)
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	archiveSHA := sha256.Sum256(archiveData)

	var downloadCount atomic.Int64
	metadataPath := "/providers/gadget/provider-release.yaml"
	archivePathURL := "/providers/gadget/gadget-current.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case metadataPath:
			metadata := providerReleaseMetadata{
				Schema:        providerReleaseSchemaName,
				SchemaVersion: providerReleaseSchemaVersion,
				Package:       source,
				Kind:          providermanifestv1.KindPlugin,
				Version:       version,
				Runtime:       providerReleaseRuntimeExecutable,
				Artifacts: map[string]providerReleaseArtifact{
					providerpkg.CurrentPlatformString(): {
						Path:   filepath.Base(archivePathURL),
						SHA256: hex.EncodeToString(archiveSHA[:]),
					},
				},
			}
			data, err := yaml.Marshal(metadata)
			if err != nil {
				http.Error(w, "metadata marshal failed", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write(data)
		case archivePathURL:
			downloadCount.Add(1)
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(archiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configYAML := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")) + strings.Join([]string{
		"apiVersion: " + config.ConfigAPIVersion,
		"plugins:",
		"  gadget:",
		"    source: " + srv.URL + metadataPath,
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"

	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle()
	if _, err := lc.InitAtPath(configPath); err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}

	lock, err := ReadLockfile(filepath.Join(filepath.Dir(configPath), InitLockfileName))
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	if _, ok := lock.Providers["gadget"]; !ok {
		t.Fatal(`lock.Providers["gadget"] not found`)
	}
	install, err := inspectPreparedInstall(filepath.Join(artifactsDir, ".gestaltd", "providers", "gadget"))
	if err != nil {
		t.Fatalf("inspectPreparedInstall: %v", err)
	}
	manifestPath := install.manifestPath

	_, staleManifest, err := providerpkg.ReadManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadManifestFile(%s): %v", manifestPath, err)
	}
	staleManifest.Version = "1.9.9"
	staleBytes, err := providerpkg.EncodeManifest(staleManifest)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	if err := os.WriteFile(manifestPath, staleBytes, 0644); err != nil {
		t.Fatalf("WriteFile(%s): %v", manifestPath, err)
	}

	downloadsBefore := downloadCount.Load()
	cfg, _, err := lc.LoadForExecutionAtPath(configPath, true)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
	}
	if got := downloadCount.Load() - downloadsBefore; got != 1 {
		t.Fatalf("download count during locked rehydration = %d, want 1", got)
	}

	gotManifest := cfg.Plugins["gadget"].ResolvedManifest
	if gotManifest == nil {
		t.Fatal("ResolvedManifest is nil")
		return
	}
	if gotManifest.Version != version {
		t.Fatalf("ResolvedManifest.Version = %q, want %q", gotManifest.Version, version)
	}

	_, readBack, err := providerpkg.ReadManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadManifestFile(%s) after rehydrate: %v", manifestPath, err)
	}
	if readBack.Version != version {
		t.Fatalf("cached manifest version = %q, want %q", readBack.Version, version)
	}
}

func TestSourceAuthPluginLoadForExecution(t *testing.T) {
	dir := t.TempDir()
	source := "github.com/acme/tools/auth-widget"
	version := "2.0.0"
	binaryContent := "fake-auth-binary"
	bootstrapManifestPath := writeBootstrapSecretsManifest(t, dir, "github.com/acme/tools/bootstrap-secrets", "0.1.0")

	archivePath := buildExecutableArchive(t, dir, "auth-src", source, version, providermanifestv1.KindAuthentication, "auth-plugin", binaryContent)
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	archiveSum := sha256.Sum256(archiveData)
	archiveSHA := hex.EncodeToString(archiveSum[:])

	var metadataCount atomic.Int64
	var downloadCount atomic.Int64
	metadataPath := "/providers/auth/provider-release.yaml"
	archivePathURL := "/providers/auth/auth-current.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case metadataPath:
			metadataCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer ghp_inline_auth_source_token" {
				http.Error(w, "bad metadata authorization", http.StatusBadRequest)
				return
			}
			metadata := providerReleaseMetadata{
				Schema:        providerReleaseSchemaName,
				SchemaVersion: providerReleaseSchemaVersion,
				Package:       source,
				Kind:          providermanifestv1.KindAuthentication,
				Version:       version,
				Runtime:       providerReleaseRuntimeExecutable,
				Artifacts: map[string]providerReleaseArtifact{
					providerpkg.CurrentPlatformString(): {
						Path:   filepath.Base(archivePathURL),
						SHA256: archiveSHA,
					},
				},
			}
			data, err := yaml.Marshal(metadata)
			if err != nil {
				http.Error(w, "metadata marshal failed", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write(data)
		case archivePathURL:
			downloadCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer ghp_inline_auth_source_token" {
				http.Error(w, "bad archive authorization", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(archiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configYAML := strings.Join([]string{
		"apiVersion: " + config.ConfigAPIVersion,
		requiredIndexedDBConfigYAML(t, dir, filepath.Join(dir, "data.db")),
		"  secrets:",
		"    secrets:",
		"      source:",
		"        path: " + bootstrapManifestPath,
		"  authentication:",
		"    auth:",
		"      source:",
		"        url: " + srv.URL + metadataPath,
		"        auth:",
		"          token:",
		"            secret:",
		"              provider: secrets",
		"              name: source-token",
		"      config:",
		"        clientId: managed-auth-client",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"    secrets: secrets",
		"    authentication: auth",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"

	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("source-token", "ghp_inline_auth_source_token")

	factories := bootstrap.NewFactoryRegistry()
	factories.Secrets["provider"] = secretsprovider.Factory
	lc := NewLifecycle().WithConfigSecretResolver(func(ctx context.Context, cfg *config.Config) error {
		return bootstrap.ResolveConfigSecrets(ctx, cfg, factories)
	})
	lc = lc.WithHTTPClient(srv.Client())
	lock, err := lc.InitAtPath(configPath)
	if err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}
	authLockEntry := mustLockEntryByName(t, lock.Authentication, "auth")
	if authLockEntry.Source != srv.URL+metadataPath {
		t.Fatalf("lock.Authentication[auth].Source = %q, want %q", authLockEntry.Source, srv.URL+metadataPath)
	}
	if authLockEntry.Executable == "" {
		t.Fatal("lock.Authentication.Executable is empty")
	}
	if got := metadataCount.Load(); got != 1 {
		t.Fatalf("metadata request count = %d, want 1", got)
	}

	metadataBefore := metadataCount.Load()
	_, _, err = lc.LoadForExecutionAtPath(configPath, true)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
	}
	if got := metadataCount.Load(); got != metadataBefore {
		t.Fatalf("metadata request count during locked load = %d, want %d", got, metadataBefore)
	}

	authRoot := filepath.Join(artifactsDir, ".gestaltd", "auth")
	if err := os.RemoveAll(authRoot); err != nil {
		t.Fatalf("RemoveAll auth root: %v", err)
	}

	downloadsBefore := downloadCount.Load()
	cfg, _, err := lc.LoadForExecutionAtPath(configPath, true)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath after cache removal: %v", err)
	}
	if got := metadataCount.Load(); got != metadataBefore {
		t.Fatalf("metadata request count during locked rehydration = %d, want %d", got, metadataBefore)
	}
	if got := downloadCount.Load() - downloadsBefore; got != 1 {
		t.Fatalf("download count during locked rehydration = %d, want 1", got)
	}

	authProvider := mustSelectedHostProviderEntry(t, cfg, config.HostProviderKindAuthentication)
	if authProvider == nil {
		t.Fatal("auth provider is nil after load")
		return
	}
	executablePath := resolveLockPath(artifactsDir, authLockEntry.Executable)
	if authProvider.Command != executablePath {
		t.Fatalf("auth provider command = %q, want %q", authProvider.Command, executablePath)
	}
	authCfg, err := config.NodeToMap(authProvider.Config)
	if err != nil {
		t.Fatalf("NodeToMap(auth config): %v", err)
	}
	if authCfg["command"] != executablePath {
		t.Fatalf("auth config command = %v, want %q", authCfg["command"], executablePath)
	}
	sourceCfg, ok := authCfg["source"].(map[string]any)
	if !ok {
		t.Fatalf("auth source config = %#v", authCfg["source"])
	}
	if want := srv.URL + metadataPath; sourceCfg["url"] != want {
		t.Fatalf("auth source url = %v, want %q", sourceCfg["url"], want)
	}
	nested, ok := authCfg["config"].(map[string]any)
	if !ok || nested["clientId"] != "managed-auth-client" {
		t.Fatalf("auth nested config = %#v", authCfg["config"])
	}
}

func TestSourceAuthPluginInitAllowsMissingEnvPlaceholderInNonStringField(t *testing.T) {
	dir := t.TempDir()
	source := "github.com/acme/tools/auth-widget"
	version := "2.0.0"
	portEnv := "GESTALT_TEST_PORT_" + strings.ToUpper(strings.ReplaceAll(t.Name(), "/", "_"))
	bootstrapManifestPath := writeBootstrapSecretsManifest(t, dir, "github.com/acme/tools/bootstrap-secrets", "0.1.0")

	archivePath := buildExecutableArchive(t, dir, "auth-src", source, version, providermanifestv1.KindAuthentication, "auth-plugin", "fake-auth-binary")
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	archiveSum := sha256.Sum256(archiveData)
	archiveSHA := hex.EncodeToString(archiveSum[:])

	var metadataCount atomic.Int64
	metadataPath := "/providers/auth/provider-release.yaml"
	archivePathURL := "/providers/auth/auth-current.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case metadataPath:
			metadataCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer ghp_inline_auth_source_token" {
				http.Error(w, "bad metadata authorization", http.StatusBadRequest)
				return
			}
			metadata := providerReleaseMetadata{
				Schema:        providerReleaseSchemaName,
				SchemaVersion: providerReleaseSchemaVersion,
				Package:       source,
				Kind:          providermanifestv1.KindAuthentication,
				Version:       version,
				Runtime:       providerReleaseRuntimeExecutable,
				Artifacts: map[string]providerReleaseArtifact{
					providerpkg.CurrentPlatformString(): {
						Path:   filepath.Base(archivePathURL),
						SHA256: archiveSHA,
					},
				},
			}
			data, err := yaml.Marshal(metadata)
			if err != nil {
				http.Error(w, "metadata marshal failed", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write(data)
		case archivePathURL:
			if got := r.Header.Get("Authorization"); got != "Bearer ghp_inline_auth_source_token" {
				http.Error(w, "bad archive authorization", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(archiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configYAML := strings.Join([]string{
		"apiVersion: " + config.ConfigAPIVersion,
		requiredIndexedDBConfigYAML(t, dir, filepath.Join(dir, "data.db")),
		"  secrets:",
		"    secrets:",
		"      source:",
		"        path: " + bootstrapManifestPath,
		"  authentication:",
		"    auth:",
		"      source:",
		"        url: " + srv.URL + metadataPath,
		"        auth:",
		"          token:",
		"            secret:",
		"              provider: secrets",
		"              name: source-token",
		"      config:",
		"        clientId: managed-auth-client",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"    secrets: secrets",
		"    authentication: auth",
		"  public:",
		"    port: ${" + portEnv + "}",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"

	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("source-token", "ghp_inline_auth_source_token")

	factories := bootstrap.NewFactoryRegistry()
	factories.Secrets["provider"] = secretsprovider.Factory
	lc := NewLifecycle().WithConfigSecretResolver(func(ctx context.Context, cfg *config.Config) error {
		return bootstrap.ResolveConfigSecrets(ctx, cfg, factories)
	})
	lc = lc.WithHTTPClient(srv.Client())
	lock, err := lc.InitAtPath(configPath)
	if err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}

	authLockEntry := mustLockEntryByName(t, lock.Authentication, "auth")
	if authLockEntry.Source != srv.URL+metadataPath {
		t.Fatalf("lock.Authentication[auth].Source = %q, want %q", authLockEntry.Source, srv.URL+metadataPath)
	}
	if authLockEntry.Executable == "" {
		t.Fatal("lock.Authentication.Executable is empty")
	}
	if got := metadataCount.Load(); got != 1 {
		t.Fatalf("metadata request count = %d, want 1", got)
	}
}

func TestManagedIndexedDBSourcesLoadForExecutionWithMultipleBindings(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mainSource := "github.com/acme/providers/indexeddb-main"
	archiveSource := "github.com/acme/providers/indexeddb-archive"
	version := "1.0.0"

	mainManifestPath := writeExecutableSourceManifest(t, dir, "indexeddb-main-src", mainSource, version, providermanifestv1.KindIndexedDB, []localExecutableManifestArtifact{{
		goos: runtime.GOOS, goarch: runtime.GOARCH, binaryName: "indexeddb-main", data: []byte("main-indexeddb-binary"),
	}})
	archiveManifestPath := writeExecutableSourceManifest(t, dir, "indexeddb-archive-src", archiveSource, version, providermanifestv1.KindIndexedDB, []localExecutableManifestArtifact{{
		goos: runtime.GOOS, goarch: runtime.GOARCH, binaryName: "indexeddb-archive", data: []byte("archive-indexeddb-binary"),
	}})

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configYAML := strings.Join([]string{
		"apiVersion: " + config.ConfigAPIVersion,
		"providers:",
		"  indexeddb:",
		"    main:",
		"      source:",
		"        path: " + mainManifestPath,
		"      config:",
		`        dsn: "sqlite://main.db"`,
		"    archive:",
		"      source:",
		"        path: " + archiveManifestPath,
		"      config:",
		`        dsn: "sqlite://archive.db"`,
		"server:",
		"  providers:",
		"    indexeddb: main",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"

	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle()
	lock, err := lc.InitAtPath(configPath)
	if err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}
	if len(lock.IndexedDBs) != 2 {
		t.Fatalf("lock.IndexedDBs = %#v, want 2 entries", lock.IndexedDBs)
	}
	if _, ok := lock.IndexedDBs["main"]; !ok {
		t.Fatal(`lock.IndexedDBs["main"] not found`)
	}
	if _, ok := lock.IndexedDBs["archive"]; !ok {
		t.Fatal(`lock.IndexedDBs["archive"] not found`)
	}

	cfg, _, err := lc.LoadForExecutionAtPath(configPath, true)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
	}

	for _, name := range []string{"main", "archive"} {
		entry := cfg.Providers.IndexedDB[name]
		if entry == nil {
			t.Fatalf("cfg.Providers.IndexedDB[%q] = nil", name)
			return
		}
		if entry.ResolvedManifest == nil {
			t.Fatalf("cfg.Providers.IndexedDB[%q].ResolvedManifest = nil", name)
			return
		}
		wantCommand := resolveLockPath(artifactsDir, lock.IndexedDBs[name].Executable)
		if entry.Command != wantCommand {
			t.Fatalf("cfg.Providers.IndexedDB[%q].Command = %q, want %q", name, entry.Command, wantCommand)
		}
	}
}

func TestManagedCacheSourcesLoadForExecutionWithMultipleBindings(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sessionSource := "github.com/acme/providers/cache-session"
	rateLimitSource := "github.com/acme/providers/cache-rate-limit"
	version := "1.0.0"
	bootstrapManifestPath := writeBootstrapSecretsManifest(t, dir, "github.com/acme/tools/bootstrap-secrets", "0.1.0")

	sessionManifestPath := writeExecutableSourceManifest(t, dir, "cache-session-src", sessionSource, version, providermanifestv1.KindCache, []localExecutableManifestArtifact{{
		goos: runtime.GOOS, goarch: runtime.GOARCH, binaryName: "cache-session", data: []byte("session-cache-binary"),
	}})
	rateLimitManifestPath := writeExecutableSourceManifest(t, dir, "cache-rate-limit-src", rateLimitSource, version, providermanifestv1.KindCache, []localExecutableManifestArtifact{{
		goos: runtime.GOOS, goarch: runtime.GOARCH, binaryName: "cache-rate-limit", data: []byte("rate-limit-cache-binary"),
	}})

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	indexedDBManifestPath := writeStubIndexedDBManifest(t, dir)
	configYAML := strings.Join([]string{
		"apiVersion: " + config.ConfigAPIVersion,
		"providers:",
		"  secrets:",
		"    session:",
		"      source:",
		"        path: " + bootstrapManifestPath,
		"    rate_limit:",
		"      source:",
		"        path: " + bootstrapManifestPath,
		"  indexeddb:",
		"    main:",
		"      source:",
		"        path: " + indexedDBManifestPath,
		"      config:",
		`        path: "` + filepath.Join(dir, "gestalt.db") + `"`,
		"  cache:",
		"    session:",
		"      source:",
		"        path: " + sessionManifestPath,
		"      config:",
		"        password:",
		"          secret:",
		"            provider: session",
		"            name: generated-secret",
		"    rate_limit:",
		"      source:",
		"        path: " + rateLimitManifestPath,
		"      config:",
		"        password:",
		"          secret:",
		"            provider: rate_limit",
		"            name: source-token",
		"server:",
		"  providers:",
		"    indexeddb: main",
		"    secrets: session",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"

	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	factories := bootstrap.NewFactoryRegistry()
	factories.Secrets["provider"] = secretsprovider.Factory
	lc := NewLifecycle().WithConfigSecretResolver(func(ctx context.Context, cfg *config.Config) error {
		return bootstrap.ResolveConfigSecrets(ctx, cfg, factories)
	})
	lock, err := lc.InitAtPath(configPath)
	if err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}
	if len(lock.Caches) != 2 {
		t.Fatalf("lock.Caches = %#v, want 2 entries", lock.Caches)
	}
	if _, ok := lock.Caches["session"]; !ok {
		t.Fatal(`lock.Caches["session"] not found`)
	}
	if _, ok := lock.Caches["rate_limit"]; !ok {
		t.Fatal(`lock.Caches["rate_limit"] not found`)
	}
	lockPath := filepath.Join(dir, InitLockfileName)
	lockData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("ReadFile lockfile: %v", err)
	}
	var diskLock providerLockfile
	if err := json.Unmarshal(lockData, &diskLock); err != nil {
		t.Fatalf("Unmarshal lockfile: %v", err)
	}
	if _, ok := diskLock.Providers.Cache["session"]; !ok {
		t.Fatal(`disk lock cache["session"] not found`)
	}
	if _, ok := diskLock.Providers.Cache["rate_limit"]; !ok {
		t.Fatal(`disk lock cache["rate_limit"] not found`)
	}

	cfg, _, err := lc.LoadForExecutionAtPath(configPath, true)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
	}

	wantPasswords := map[string]string{
		"session":    "generated-secret-value",
		"rate_limit": "ghp_inline_auth_source_token",
	}
	for _, name := range []string{"session", "rate_limit"} {
		entry := cfg.Providers.Cache[name]
		if entry == nil {
			t.Fatalf("cfg.Providers.Cache[%q] = nil", name)
			return
		}
		if entry.ResolvedManifest == nil {
			t.Fatalf("cfg.Providers.Cache[%q].ResolvedManifest = nil", name)
			return
		}
		wantCommand := resolveLockPath(artifactsDir, lock.Caches[name].Executable)
		if entry.Command != wantCommand {
			t.Fatalf("cfg.Providers.Cache[%q].Command = %q, want %q", name, entry.Command, wantCommand)
		}
		runtimeCfg, err := config.NodeToMap(entry.Config)
		if err != nil {
			t.Fatalf("NodeToMap(cache %q config): %v", name, err)
		}
		configMap, ok := runtimeCfg["config"].(map[string]any)
		if !ok {
			t.Fatalf("cache %q runtime config = %#v", name, runtimeCfg["config"])
		}
		if got := configMap["password"]; got != wantPasswords[name] {
			t.Fatalf("cache %q password = %#v, want %q", name, got, wantPasswords[name])
		}
	}
}

func TestManagedCacheSourcesInitAtPathWithPlatformsHashesExtraPlatformArchives(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		apiVersion  string
		localSource bool
	}{
		{name: "remote metadata url", apiVersion: config.ConfigAPIVersion},
		{name: "local metadata file", apiVersion: config.ConfigAPIVersion, localSource: true},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			cacheSource := "github.com/acme/tools/cache-session"
			version := "1.0.0"

			extraPlatform := struct{ GOOS, GOARCH string }{GOOS: "linux", GOARCH: "amd64"}
			if runtime.GOOS == extraPlatform.GOOS && runtime.GOARCH == extraPlatform.GOARCH {
				extraPlatform = struct{ GOOS, GOARCH string }{GOOS: "darwin", GOARCH: "arm64"}
			}
			extraPlatformKey := providerpkg.PlatformString(extraPlatform.GOOS, extraPlatform.GOARCH)
			currentArchivePath := buildExecutableArchive(t, dir, "cache-src", cacheSource, version, providermanifestv1.KindCache, "cache-plugin", "fake-cache-binary")
			currentArchiveData, err := os.ReadFile(currentArchivePath)
			if err != nil {
				t.Fatalf("read current archive: %v", err)
			}
			currentArchiveSum := sha256.Sum256(currentArchiveData)
			extraArchivePathURL := "/providers/cache/cache-extra.tar.gz"
			currentArchivePathURL := "/providers/cache/cache-current.tar.gz"
			metadataPath := "/providers/cache/provider-release.yaml"
			extraArchiveData := []byte("fake-cache-extra-platform-archive")
			extraArchiveSum := sha256.Sum256(extraArchiveData)

			sourceValue := ""
			wantSource := ""
			wantExtraArchiveURL := ""
			var client *http.Client
			var srv *httptest.Server

			if tc.localSource {
				metadataRelPath := filepath.ToSlash(filepath.Join("providers", "cache", "provider-release.yaml"))
				metadataAbsPath := filepath.Join(dir, filepath.FromSlash(metadataRelPath))
				metadataDir := filepath.Dir(metadataAbsPath)
				if err := os.MkdirAll(metadataDir, 0o755); err != nil {
					t.Fatalf("create metadata dir: %v", err)
				}
				currentArchiveName := "cache-current.tar.gz"
				extraArchiveName := "cache-extra.tar.gz"
				if err := os.WriteFile(filepath.Join(metadataDir, currentArchiveName), currentArchiveData, 0o644); err != nil {
					t.Fatalf("write current archive: %v", err)
				}
				if err := os.WriteFile(filepath.Join(metadataDir, extraArchiveName), extraArchiveData, 0o644); err != nil {
					t.Fatalf("write extra archive: %v", err)
				}
				writeProviderReleaseMetadataFile(t, metadataAbsPath, providerReleaseMetadata{
					Schema:        providerReleaseSchemaName,
					SchemaVersion: providerReleaseSchemaVersion,
					Package:       cacheSource,
					Kind:          providermanifestv1.KindCache,
					Version:       version,
					Runtime:       providerReleaseRuntimeExecutable,
					Artifacts: map[string]providerReleaseArtifact{
						providerpkg.CurrentPlatformString(): {
							Path:   currentArchiveName,
							SHA256: hex.EncodeToString(currentArchiveSum[:]),
						},
						extraPlatformKey: {
							Path:   extraArchiveName,
							SHA256: hex.EncodeToString(extraArchiveSum[:]),
						},
					},
				})
				sourceValue = "./" + metadataRelPath
				wantSource = metadataRelPath
				wantExtraArchiveURL = extraArchiveName
			} else {
				srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case metadataPath:
						metadata := providerReleaseMetadata{
							Schema:        providerReleaseSchemaName,
							SchemaVersion: providerReleaseSchemaVersion,
							Package:       cacheSource,
							Kind:          providermanifestv1.KindCache,
							Version:       version,
							Runtime:       providerReleaseRuntimeExecutable,
							Artifacts: map[string]providerReleaseArtifact{
								providerpkg.CurrentPlatformString(): {
									Path:   filepath.Base(currentArchivePathURL),
									SHA256: hex.EncodeToString(currentArchiveSum[:]),
								},
								extraPlatformKey: {
									Path:   filepath.Base(extraArchivePathURL),
									SHA256: hex.EncodeToString(extraArchiveSum[:]),
								},
							},
						}
						data, err := yaml.Marshal(metadata)
						if err != nil {
							http.Error(w, "metadata marshal failed", http.StatusInternalServerError)
							return
						}
						w.Header().Set("Content-Type", "application/yaml")
						_, _ = w.Write(data)
					case currentArchivePathURL:
						w.Header().Set("Content-Type", "application/octet-stream")
						_, _ = w.Write(currentArchiveData)
					case extraArchivePathURL:
						w.Header().Set("Content-Type", "application/octet-stream")
						_, _ = w.Write(extraArchiveData)
					default:
						http.NotFound(w, r)
					}
				}))
				defer srv.Close()
				client = srv.Client()
				sourceValue = srv.URL + metadataPath
				wantSource = sourceValue
				wantExtraArchiveURL = srv.URL + extraArchivePathURL
			}

			artifactsDir := filepath.Join(dir, "prepared-artifacts")
			configLines := []string{
				"apiVersion: " + tc.apiVersion,
				"providers:",
				"  cache:",
				"    session:",
				"      source: " + sourceValue,
				"server:",
				"  providers:",
				"    indexeddb: sqlite",
				"  artifactsDir: " + artifactsDir,
				"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			}
			configYAML := strings.Join(configLines, "\n") + "\n"

			configPath := filepath.Join(dir, "gestalt.yaml")
			if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}

			lc := NewLifecycle()
			if client != nil {
				lc = lc.WithHTTPClient(client)
			}
			lock, err := lc.InitAtPathWithPlatforms(configPath, "", []struct{ GOOS, GOARCH string }{extraPlatform})
			if err != nil {
				t.Fatalf("InitAtPathWithPlatforms: %v", err)
			}

			entry, ok := lock.Caches["session"]
			if !ok {
				t.Fatal(`lock.Caches["session"] not found`)
			}
			if entry.Source != wantSource {
				t.Fatalf("lock source = %q, want %q", entry.Source, wantSource)
			}
			wantCurrentSHA := hex.EncodeToString(currentArchiveSum[:])
			wantExtraSHA := hex.EncodeToString(extraArchiveSum[:])
			if got := entry.Archives[providerpkg.CurrentPlatformString()].SHA256; got != wantCurrentSHA {
				t.Fatalf("lock current-platform SHA256 = %q, want %q", got, wantCurrentSHA)
			}
			if got := entry.Archives[extraPlatformKey].SHA256; got != wantExtraSHA {
				t.Fatalf("lock extra-platform SHA256 = %q, want %q", got, wantExtraSHA)
			}
			if got := entry.Archives[extraPlatformKey].URL; got != wantExtraArchiveURL {
				t.Fatalf("lock extra-platform URL = %q, want %q", got, wantExtraArchiveURL)
			}

			readBack, err := ReadLockfile(filepath.Join(dir, InitLockfileName))
			if err != nil {
				t.Fatalf("ReadLockfile: %v", err)
			}
			if got := readBack.Caches["session"].Source; got != wantSource {
				t.Fatalf("readBack source = %q, want %q", got, wantSource)
			}
			if got := readBack.Caches["session"].Archives[providerpkg.CurrentPlatformString()].SHA256; got != wantCurrentSHA {
				t.Fatalf("readBack current-platform SHA256 = %q, want %q", got, wantCurrentSHA)
			}
			if got := readBack.Caches["session"].Archives[extraPlatformKey].SHA256; got != wantExtraSHA {
				t.Fatalf("readBack extra-platform SHA256 = %q, want %q", got, wantExtraSHA)
			}
			if got := readBack.Caches["session"].Archives[extraPlatformKey].URL; got != wantExtraArchiveURL {
				t.Fatalf("readBack extra-platform URL = %q, want %q", got, wantExtraArchiveURL)
			}
		})
	}
}

func TestSourceSecretsPluginBootstrapsManagedAuthSourceToken(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	secretsSourceToken := "ghp_inline_auth_source_token"
	bootstrapSource := "github.com/acme/tools/bootstrap-secrets"
	bootstrapVersion := "0.1.0"
	secretsSource := "github.com/acme/tools/secrets-widget"
	secretsVersion := "1.0.0"
	authSource := "github.com/acme/tools/auth-widget"
	authVersion := "2.0.0"
	bootstrapArtifact := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "bootstrap-secrets"))
	bootstrapManifestPath := filepath.Join(dir, "bootstrap-secrets-manifest.yaml")
	bootstrapManifest, err := providerpkg.EncodeSourceManifestFormat(&providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindSecrets,
		Source:  bootstrapSource,
		Version: bootstrapVersion,
		Spec:    &providermanifestv1.Spec{},
		Artifacts: []providermanifestv1.Artifact{
			{OS: runtime.GOOS, Arch: runtime.GOARCH, Path: bootstrapArtifact},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: bootstrapArtifact},
	}, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat bootstrap: %v", err)
	}
	if err := os.WriteFile(bootstrapManifestPath, bootstrapManifest, 0o644); err != nil {
		t.Fatalf("write bootstrap manifest: %v", err)
	}
	bootstrapBinaryData, err := os.ReadFile(buildGoSourceSecretsBinary(t))
	if err != nil {
		t.Fatalf("read bootstrap binary: %v", err)
	}
	bootstrapBinaryPath := filepath.Join(dir, filepath.FromSlash(bootstrapArtifact))
	if err := os.MkdirAll(filepath.Dir(bootstrapBinaryPath), 0o755); err != nil {
		t.Fatalf("MkdirAll bootstrap artifact: %v", err)
	}
	if err := os.WriteFile(bootstrapBinaryPath, bootstrapBinaryData, 0o755); err != nil {
		t.Fatalf("write bootstrap artifact: %v", err)
	}

	secretsArchivePath := buildExecutableArchiveFromBinaryPath(
		t,
		dir,
		"secrets-src",
		secretsSource,
		secretsVersion,
		providermanifestv1.KindSecrets,
		"secrets-plugin",
		buildGoSourceSecretsBinary(t),
	)
	authArchivePath := buildExecutableArchive(
		t,
		dir,
		"auth-src",
		authSource,
		authVersion,
		providermanifestv1.KindAuthentication,
		"auth-plugin",
		"fake-auth-binary",
	)

	secretsArchiveData, err := os.ReadFile(secretsArchivePath)
	if err != nil {
		t.Fatalf("read secrets archive: %v", err)
	}
	secretsArchiveSum := sha256.Sum256(secretsArchiveData)
	authArchiveData, err := os.ReadFile(authArchivePath)
	if err != nil {
		t.Fatalf("read auth archive: %v", err)
	}
	authArchiveSum := sha256.Sum256(authArchiveData)

	var secretsMetadataCount atomic.Int64
	var authMetadataCount atomic.Int64
	var secretsDownloads atomic.Int64
	var authDownloads atomic.Int64
	secretsMetadataPath := "/providers/secrets/provider-release.yaml"
	secretsArchivePathURL := "/providers/secrets/secrets.tar.gz"
	authMetadataPath := "/providers/auth/provider-release.yaml"
	authArchivePathURL := "/providers/auth/auth.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case secretsMetadataPath:
			secretsMetadataCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer "+secretsSourceToken {
				http.Error(w, "bad auth header for secrets metadata", http.StatusUnauthorized)
				return
			}
			metadata := providerReleaseMetadata{
				Schema:        providerReleaseSchemaName,
				SchemaVersion: providerReleaseSchemaVersion,
				Package:       secretsSource,
				Kind:          providermanifestv1.KindSecrets,
				Version:       secretsVersion,
				Runtime:       providerReleaseRuntimeExecutable,
				Artifacts: map[string]providerReleaseArtifact{
					providerpkg.CurrentPlatformString(): {
						Path:   filepath.Base(secretsArchivePathURL),
						SHA256: hex.EncodeToString(secretsArchiveSum[:]),
					},
				},
			}
			data, err := yaml.Marshal(metadata)
			if err != nil {
				http.Error(w, "metadata marshal failed", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write(data)
		case secretsArchivePathURL:
			secretsDownloads.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer "+secretsSourceToken {
				http.Error(w, "bad auth header for secrets download", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(secretsArchiveData)
		case authMetadataPath:
			authMetadataCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer ghp_inline_auth_source_token" {
				http.Error(w, "bad auth header for auth metadata", http.StatusUnauthorized)
				return
			}
			metadata := providerReleaseMetadata{
				Schema:        providerReleaseSchemaName,
				SchemaVersion: providerReleaseSchemaVersion,
				Package:       authSource,
				Kind:          providermanifestv1.KindAuthentication,
				Version:       authVersion,
				Runtime:       providerReleaseRuntimeExecutable,
				Artifacts: map[string]providerReleaseArtifact{
					providerpkg.CurrentPlatformString(): {
						Path:   filepath.Base(authArchivePathURL),
						SHA256: hex.EncodeToString(authArchiveSum[:]),
					},
				},
			}
			data, err := yaml.Marshal(metadata)
			if err != nil {
				http.Error(w, "metadata marshal failed", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write(data)
		case authArchivePathURL:
			authDownloads.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer ghp_inline_auth_source_token" {
				http.Error(w, "bad auth header for auth download", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(authArchiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configYAML := strings.Join([]string{
		"apiVersion: " + config.ConfigAPIVersion,
		requiredIndexedDBConfigYAML(t, dir, filepath.Join(dir, "data.db")),
		"  secrets:",
		"    bootstrap:",
		"      source:",
		"        path: ./bootstrap-secrets-manifest.yaml",
		"    secrets:",
		"      source:",
		"        url: " + srv.URL + secretsMetadataPath,
		"        auth:",
		"          token:",
		"            secret:",
		"              provider: bootstrap",
		"              name: source-token",
		"  authentication:",
		"    auth:",
		"      source:",
		"        url: " + srv.URL + authMetadataPath,
		"        auth:",
		"          token:",
		"            secret:",
		"              provider: secrets",
		"              name: source-token",
		"      config:",
		"        clientId: managed-auth-client",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"    secrets: secrets",
		"    authentication: auth",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"

	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	factories := bootstrap.NewFactoryRegistry()
	factories.Secrets["provider"] = secretsprovider.Factory

	lc := NewLifecycle().WithConfigSecretResolver(func(ctx context.Context, cfg *config.Config) error {
		return bootstrap.ResolveConfigSecrets(ctx, cfg, factories)
	})
	lc = lc.WithHTTPClient(srv.Client())

	lock, err := lc.InitAtPath(configPath)
	if err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}
	secretsLockEntry := mustLockEntryByName(t, lock.Secrets, "secrets")
	authLockEntry := mustLockEntryByName(t, lock.Authentication, "auth")
	if got := secretsMetadataCount.Load(); got != 1 {
		t.Fatalf("secrets metadata request count = %d, want 1", got)
	}
	if got := authMetadataCount.Load(); got != 1 {
		t.Fatalf("auth metadata request count = %d, want 1", got)
	}

	secretsRoot := filepath.Join(artifactsDir, ".gestaltd", "secrets", "secrets")
	if err := os.RemoveAll(secretsRoot); err != nil {
		t.Fatalf("RemoveAll secrets provider root: %v", err)
	}
	authRoot := filepath.Join(artifactsDir, ".gestaltd", "auth")
	if err := os.RemoveAll(authRoot); err != nil {
		t.Fatalf("RemoveAll auth root: %v", err)
	}

	secretsMetadataBefore := secretsMetadataCount.Load()
	authMetadataBefore := authMetadataCount.Load()
	secretsDownloadsBefore := secretsDownloads.Load()
	authDownloadsBefore := authDownloads.Load()
	cfg, _, err := lc.LoadForExecutionAtPath(configPath, true)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
	}
	if got := secretsMetadataCount.Load(); got != secretsMetadataBefore {
		t.Fatalf("secrets metadata requests during locked load = %d, want %d", got, secretsMetadataBefore)
	}
	if got := authMetadataCount.Load(); got != authMetadataBefore {
		t.Fatalf("auth metadata requests during locked load = %d, want %d", got, authMetadataBefore)
	}
	if got := secretsDownloads.Load() - secretsDownloadsBefore; got != 1 {
		t.Fatalf("secrets download count during locked load = %d, want 1", got)
	}
	if got := authDownloads.Load() - authDownloadsBefore; got != 1 {
		t.Fatalf("auth download count during locked load = %d, want 1", got)
	}
	authProvider := mustSelectedHostProviderEntry(t, cfg, config.HostProviderKindAuthentication)
	if authProvider == nil || authProvider.Source.Auth == nil {
		t.Fatalf("auth provider source auth = %#v", authProvider)
		return
	}
	if authProvider.Source.Auth.Token != "ghp_inline_auth_source_token" {
		t.Fatalf("resolved auth source token = %q, want %q", authProvider.Source.Auth.Token, "ghp_inline_auth_source_token")
	}

	secretsExecutablePath := resolveLockPath(artifactsDir, secretsLockEntry.Executable)
	secretsProvider := mustSelectedHostProviderEntry(t, cfg, config.HostProviderKindSecrets)
	if secretsProvider == nil {
		t.Fatal("secrets provider is nil after load")
		return
	}
	if secretsProvider.Source.Auth == nil {
		t.Fatalf("secrets provider source auth = %#v", secretsProvider)
		return
	}
	if secretsProvider.Source.Auth.Token != secretsSourceToken {
		t.Fatalf("resolved secrets source token = %q, want %q", secretsProvider.Source.Auth.Token, secretsSourceToken)
	}
	if secretsProvider.Command != secretsExecutablePath {
		t.Fatalf("secrets provider command = %q, want %q", secretsProvider.Command, secretsExecutablePath)
	}
	authExecutablePath := resolveLockPath(artifactsDir, authLockEntry.Executable)
	if authProvider.Command != authExecutablePath {
		t.Fatalf("auth provider command = %q, want %q", authProvider.Command, authExecutablePath)
	}
}

func TestLoadForExecutionAtPath_UnlockedBootstrapMetadataInitPreparesOnce(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	bootstrapSource := "github.com/acme/tools/bootstrap-secrets"
	bootstrapVersion := "0.1.0"
	authSource := "github.com/acme/tools/auth-widget"
	authVersion := "2.0.0"
	bootstrapArtifact := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "bootstrap-secrets"))
	bootstrapManifestPath := filepath.Join(dir, "bootstrap-secrets-manifest.yaml")
	bootstrapManifest, err := providerpkg.EncodeSourceManifestFormat(&providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindSecrets,
		Source:  bootstrapSource,
		Version: bootstrapVersion,
		Spec:    &providermanifestv1.Spec{},
		Artifacts: []providermanifestv1.Artifact{
			{OS: runtime.GOOS, Arch: runtime.GOARCH, Path: bootstrapArtifact},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: bootstrapArtifact},
	}, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat bootstrap: %v", err)
	}
	if err := os.WriteFile(bootstrapManifestPath, bootstrapManifest, 0o644); err != nil {
		t.Fatalf("write bootstrap manifest: %v", err)
	}
	bootstrapBinaryData, err := os.ReadFile(buildGoSourceSecretsBinary(t))
	if err != nil {
		t.Fatalf("read bootstrap binary: %v", err)
	}
	bootstrapBinaryPath := filepath.Join(dir, filepath.FromSlash(bootstrapArtifact))
	if err := os.MkdirAll(filepath.Dir(bootstrapBinaryPath), 0o755); err != nil {
		t.Fatalf("MkdirAll bootstrap artifact: %v", err)
	}
	if err := os.WriteFile(bootstrapBinaryPath, bootstrapBinaryData, 0o755); err != nil {
		t.Fatalf("write bootstrap artifact: %v", err)
	}

	authArchivePath := buildExecutableArchive(
		t,
		dir,
		"auth-metadata-src",
		authSource,
		authVersion,
		providermanifestv1.KindAuthentication,
		"auth-plugin",
		"fake-auth-binary",
	)
	authArchiveData, err := os.ReadFile(authArchivePath)
	if err != nil {
		t.Fatalf("read auth archive: %v", err)
	}
	authArchiveSum := sha256.Sum256(authArchiveData)

	var metadataCount atomic.Int64
	var archiveCount atomic.Int64
	handlerErrs := make(chan error, 4)
	nextHandlerErr := func() error {
		t.Helper()
		select {
		case err := <-handlerErrs:
			return err
		default:
			return nil
		}
	}

	metadataPath := "/providers/auth/provider-release.yaml"
	archivePathURL := "/providers/auth/auth-current.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case metadataPath:
			metadataCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer ghp_inline_auth_source_token" {
				handlerErrs <- fmt.Errorf("metadata authorization = %q, want %q", got, "Bearer ghp_inline_auth_source_token")
				http.Error(w, "bad metadata authorization", http.StatusBadRequest)
				return
			}
			metadata := providerReleaseMetadata{
				Schema:        providerReleaseSchemaName,
				SchemaVersion: providerReleaseSchemaVersion,
				Package:       authSource,
				Kind:          providermanifestv1.KindAuthentication,
				Version:       authVersion,
				Runtime:       providerReleaseRuntimeExecutable,
				Artifacts: map[string]providerReleaseArtifact{
					providerpkg.CurrentPlatformString(): {
						Path:   filepath.Base(archivePathURL),
						SHA256: hex.EncodeToString(authArchiveSum[:]),
					},
				},
			}
			data, err := yaml.Marshal(metadata)
			if err != nil {
				handlerErrs <- fmt.Errorf("marshal metadata: %v", err)
				http.Error(w, "metadata marshal failed", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write(data)
		case archivePathURL:
			archiveCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer ghp_inline_auth_source_token" {
				handlerErrs <- fmt.Errorf("archive authorization = %q, want %q", got, "Bearer ghp_inline_auth_source_token")
				http.Error(w, "bad archive authorization", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(authArchiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configYAML := strings.Join([]string{
		"apiVersion: " + config.ConfigAPIVersion,
		requiredIndexedDBConfigYAML(t, dir, filepath.Join(dir, "data.db")),
		"  secrets:",
		"    bootstrap:",
		"      source:",
		"        path: ./bootstrap-secrets-manifest.yaml",
		"  authentication:",
		"    auth:",
		"      source:",
		"        url: " + srv.URL + metadataPath + "?download=1",
		"        auth:",
		"          token:",
		"            secret:",
		"              provider: bootstrap",
		"              name: source-token",
		"      config:",
		"        clientId: managed-auth-client",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"    secrets: bootstrap",
		"    authentication: auth",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"

	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	factories := bootstrap.NewFactoryRegistry()
	factories.Secrets["provider"] = secretsprovider.Factory

	lc := NewLifecycle().WithConfigSecretResolver(func(ctx context.Context, cfg *config.Config) error {
		return bootstrap.ResolveConfigSecrets(ctx, cfg, factories)
	})

	cfg, _, err := lc.LoadForExecutionAtPath(configPath, false)
	if err != nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
		t.Fatalf("LoadForExecutionAtPath(locked=false): %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if got := metadataCount.Load(); got != 1 {
		t.Fatalf("metadata request count = %d, want 1", got)
	}
	if got := archiveCount.Load(); got != 1 {
		t.Fatalf("archive request count = %d, want 1", got)
	}

	authProvider := mustSelectedHostProviderEntry(t, cfg, config.HostProviderKindAuthentication)
	if authProvider == nil || authProvider.Source.Auth == nil {
		t.Fatalf("auth provider source auth = %#v", authProvider)
		return
	}
	if authProvider.Source.Auth.Token != "ghp_inline_auth_source_token" {
		t.Fatalf("resolved auth source token = %q, want %q", authProvider.Source.Auth.Token, "ghp_inline_auth_source_token")
	}
}

func TestLoadForExecutionAtPath_UnlockedMetadataSecretsProviderResolvesConfigSecrets(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	secretsSource := "github.com/acme/tools/secrets-widget"
	secretsVersion := "1.0.0"

	secretsArchivePath := buildExecutableArchiveFromBinaryPath(
		t,
		dir,
		"secrets-metadata-src",
		secretsSource,
		secretsVersion,
		providermanifestv1.KindSecrets,
		"secrets-plugin",
		buildGoSourceSecretsBinary(t),
	)
	secretsArchiveData, err := os.ReadFile(secretsArchivePath)
	if err != nil {
		t.Fatalf("read secrets archive: %v", err)
	}
	secretsArchiveSum := sha256.Sum256(secretsArchiveData)

	var metadataCount atomic.Int64
	var archiveCount atomic.Int64
	metadataPath := "/providers/secrets/provider-release.yaml"
	archivePathURL := "/providers/secrets/secrets-current.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case metadataPath:
			metadataCount.Add(1)
			metadata := providerReleaseMetadata{
				Schema:        providerReleaseSchemaName,
				SchemaVersion: providerReleaseSchemaVersion,
				Package:       secretsSource,
				Kind:          providermanifestv1.KindSecrets,
				Version:       secretsVersion,
				Runtime:       providerReleaseRuntimeExecutable,
				Artifacts: map[string]providerReleaseArtifact{
					providerpkg.CurrentPlatformString(): {
						Path:   filepath.Base(archivePathURL),
						SHA256: hex.EncodeToString(secretsArchiveSum[:]),
					},
				},
			}
			data, err := yaml.Marshal(metadata)
			if err != nil {
				http.Error(w, "metadata marshal failed", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write(data)
		case archivePathURL:
			archiveCount.Add(1)
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(secretsArchiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configYAML := strings.Join([]string{
		"apiVersion: " + config.ConfigAPIVersion,
		requiredIndexedDBConfigYAML(t, dir, filepath.Join(dir, "data.db")),
		"  secrets:",
		"    secrets:",
		"      source: " + srv.URL + metadataPath + "?download=1",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"    secrets: secrets",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey:",
		"    secret:",
		"      provider: secrets",
		"      name: source-token",
	}, "\n") + "\n"

	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	factories := bootstrap.NewFactoryRegistry()
	factories.Secrets["provider"] = secretsprovider.Factory

	lc := NewLifecycle().WithConfigSecretResolver(func(ctx context.Context, cfg *config.Config) error {
		return bootstrap.ResolveConfigSecrets(ctx, cfg, factories)
	})

	cfg, _, err := lc.LoadForExecutionAtPath(configPath, false)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=false): %v", err)
	}
	if got := metadataCount.Load(); got != 1 {
		t.Fatalf("metadata request count = %d, want 1", got)
	}
	if got := archiveCount.Load(); got != 1 {
		t.Fatalf("archive request count = %d, want 1", got)
	}
	if got := cfg.Server.EncryptionKey; got != "ghp_inline_auth_source_token" {
		t.Fatalf("resolved encryption key = %q, want %q", got, "ghp_inline_auth_source_token")
	}

	secretsProvider := mustSelectedHostProviderEntry(t, cfg, config.HostProviderKindSecrets)
	if secretsProvider == nil {
		t.Fatal("secrets provider is nil after load")
		return
	}
	if secretsProvider.Command == "" {
		t.Fatal("secrets provider command is empty after load")
	}
}
