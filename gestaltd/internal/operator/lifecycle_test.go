package operator

import (
	"archive/tar"
	"bytes"
	"cmp"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"unicode"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	appservice "github.com/valon-technologies/gestalt/server/services/apps"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
	"gopkg.in/yaml.v3"
)

func configDirPaths(dir string) (lockfilePath, artifactsDir string) {
	return filepath.Join(dir, LockfileName), filepath.Join(dir, "artifacts")
}

func lockAndArtifactsForConfig(configPath string) (lockfilePath, artifactsDir string) {
	configDir := filepath.Dir(configPath)
	return filepath.Join(configDir, LockfileName), filepath.Join(configDir, "artifacts")
}

func prepareAtPathInTest(t *testing.T, lc *Lifecycle, cfgPath string) (*Lockfile, error) {
	t.Helper()
	lockfilePath, artifactsDir := lockAndArtifactsForConfig(cfgPath)
	return lc.PrepareAtPaths([]string{cfgPath}, lockfilePath, artifactsDir)
}

func testDisplayName(name string) string {
	parts := strings.Fields(strings.ReplaceAll(name, "-", " "))
	for i, part := range parts {
		runes := []rune(part)
		if len(runes) == 0 {
			continue
		}
		runes[0] = unicode.ToUpper(runes[0])
		for j := 1; j < len(runes); j++ {
			runes[j] = unicode.ToLower(runes[j])
		}
		parts[i] = string(runes)
	}
	return strings.Join(parts, " ")
}

func decodeNodeMap(t *testing.T, node any) map[string]any {
	t.Helper()
	var out map[string]any
	switch n := node.(type) {
	case yaml.Node:
		if err := n.Decode(&out); err != nil {
			t.Fatalf("Decode: %v", err)
		}
	case *yaml.Node:
		if n == nil {
			return nil
		}
		if err := n.Decode(&out); err != nil {
			t.Fatalf("Decode: %v", err)
		}
	case interface{ Decode(any) error }:
		if err := n.Decode(&out); err != nil {
			t.Fatalf("Decode: %v", err)
		}
	default:
		t.Fatalf("unsupported node type %T", node)
	}
	return out
}

func withNoAuthDefaultConnection(spec *providermanifestv1.Spec) *providermanifestv1.Spec {
	if spec == nil {
		spec = &providermanifestv1.Spec{}
	}
	if spec.Connections == nil {
		spec.Connections = map[string]*providermanifestv1.ManifestConnectionDef{}
	}
	def := spec.Connections["default"]
	if def == nil {
		def = &providermanifestv1.ManifestConnectionDef{}
	}
	def.Auth = &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeNone}
	spec.Connections["default"] = def
	return spec
}

func encodeSourceManifestForTest(manifest *providermanifestv1.Manifest, format string) ([]byte, error) {
	if manifest == nil {
		return providerpkg.EncodeSourceManifestFormat(nil, format)
	}
	clone := *manifest
	clone.Entrypoint = nil
	return providerpkg.EncodeSourceManifestFormat(&clone, format)
}

func writeTestFile(t *testing.T, dir, rel string, data []byte, mode os.FileMode) {
	t.Helper()

	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func writeOperatorGoComponentBuildFixture(t *testing.T, providerDir, importPath, kind, artifactRel string) {
	t.Helper()

	serveCall := operatorGoComponentServeCallForTest(t, kind)
	mainSource := fmt.Sprintf(`package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	providerpkg %q
	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := %s; err != nil {
		fmt.Fprintf(os.Stderr, "error: %%v\n", err)
		os.Exit(1)
	}
}
`, importPath, serveCall)
	writeTestFile(t, providerDir, filepath.Join("cmd", "provider", "main.go"), []byte(mainSource), 0o644)
	buildScript := fmt.Sprintf("mkdir -p %q\ngo build -o %q ./cmd/provider\n", filepath.ToSlash(filepath.Dir(artifactRel)), artifactRel)
	writeTestFile(t, providerDir, "build.sh", []byte(buildScript), 0o755)
}

func writeOperatorGoPluginBuildFixture(t *testing.T, providerDir, importPath, pluginName, artifactRel string) {
	t.Helper()

	mainSource := fmt.Sprintf(`package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	providerpkg %q
	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := gestalt.ServeProvider(ctx, providerpkg.New(), providerpkg.Router.WithName(%q)); err != nil {
		fmt.Fprintf(os.Stderr, "error: %%v\n", err)
		os.Exit(1)
	}
}
`, importPath, pluginName)
	writeTestFile(t, providerDir, filepath.Join("cmd", "provider", "main.go"), []byte(mainSource), 0o644)
	buildScript := fmt.Sprintf("mkdir -p %q\ngo build -o %q ./cmd/provider\n", filepath.ToSlash(filepath.Dir(artifactRel)), artifactRel)
	writeTestFile(t, providerDir, "build.sh", []byte(buildScript), 0o755)
}

func operatorGoComponentServeCallForTest(t *testing.T, kind string) string {
	t.Helper()
	switch providermanifestv1.NormalizeKind(kind) {
	case providermanifestv1.KindIdentity:
		return "gestalt.ServeIdentityProvider(ctx, providerpkg.New())"
	case providermanifestv1.KindAuthorization:
		return "gestalt.ServeAuthorizationProvider(ctx, providerpkg.New())"
	case providermanifestv1.KindCache:
		return "gestalt.ServeCacheProvider(ctx, providerpkg.New())"
	case providermanifestv1.KindWorkflow:
		return "gestalt.ServeWorkflowProvider(ctx, providerpkg.New())"
	case providermanifestv1.KindExternalCredentials:
		return "gestalt.ServeExternalCredentialProvider(ctx, providerpkg.New())"
	case providermanifestv1.KindSecrets:
		return "gestalt.ServeSecretsProvider(ctx, providerpkg.New())"
	case providermanifestv1.KindIndexedDB:
		return "gestalt.ServeIndexedDBProvider(ctx, providerpkg.New())"
	case providermanifestv1.KindS3:
		return "gestalt.ServeS3Provider(ctx, providerpkg.New())"
	case providermanifestv1.KindAgent:
		return "gestalt.ServeAgentProvider(ctx, providerpkg.New())"
	case providermanifestv1.KindRuntime:
		return "gestalt.ServeRuntimeProvider(ctx, providerpkg.New())"
	default:
		t.Fatalf("unsupported Go component fixture kind %q", kind)
		return ""
	}
}

func writeStubIndexedDBManifest(t *testing.T, dir string) string {
	t.Helper()
	providerDir := filepath.Join(dir, "indexeddb-stub")
	manifestPath := filepath.Join(providerDir, "indexeddb-manifest.yaml")
	const source = "github.com/test/providers/indexeddb-stub"
	buildOutput := ".gestaltd/bin/indexeddb-stub"
	buildScript := fmt.Sprintf("mkdir -p .gestaltd/bin\nprintf 'stub-indexeddb' > %s\nchmod +x %s\n", buildOutput, buildOutput)
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		t.Fatalf("mkdir indexeddb provider dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(providerDir, "build.sh"), []byte(buildScript), 0o755); err != nil {
		t.Fatalf("write indexeddb build script: %v", err)
	}
	data, err := encodeSourceManifestForTest(&providermanifestv1.Manifest{
		Source:  source,
		Version: "0.0.1-alpha.1",
		Kind:    providermanifestv1.KindIndexedDB,
		Spec:    &providermanifestv1.Spec{},
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"build.sh"},
		},
	}, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("encode indexeddb manifest: %v", err)
	}
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		t.Fatalf("write indexeddb manifest: %v", err)
	}
	return manifestPath
}

func requiredComponentConfigYAML(t *testing.T, dir, dbPath string) string {
	manifestPath := writeStubIndexedDBManifest(t, dir)
	return fmt.Sprintf(`providers:
  indexeddb:
    sqlite:
      source:
        path: %s
      config:
        path: %q
`, manifestPath, dbPath)
}

func requiredComponentConfigWithAPIVersionYAML(t *testing.T, dir, dbPath string) string {
	t.Helper()
	return "apiVersion: " + config.ConfigAPIVersion + "\n" + requiredComponentConfigYAML(t, dir, dbPath)
}

func requiredServerIndexedDBYAML() string {
	return `  providers:
    indexeddb: sqlite
`
}

type managedMetadataRelease struct {
	metadataPath    string
	archiveURLPath  string
	archiveFilePath string
	packageSource   string
	version         string
	kind            string
	allowInvalid    bool
	token           string
}

func newManagedMetadataServer(t *testing.T, releases []managedMetadataRelease) *httptest.Server {
	t.Helper()

	type response struct {
		contentType string
		token       string
		body        []byte
	}

	routes := make(map[string]response, len(releases)*2)
	for _, release := range releases {
		archiveData, err := os.ReadFile(release.archiveFilePath)
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", release.archiveFilePath, err)
		}
		archiveSum := sha256.Sum256(archiveData)
		fixture := newProviderReleaseFixtureFiles(t, providerReleaseMetadataFixture{
			Package:      release.packageSource,
			Kind:         release.kind,
			Version:      release.version,
			ArchivePath:  release.archiveFilePath,
			AllowInvalid: release.allowInvalid,
			Artifacts: map[string]providerrelease.Artifact{
				providerpkg.CurrentPlatformString(): {
					Path:   filepath.Base(release.archiveURLPath),
					SHA256: hex.EncodeToString(archiveSum[:]),
				},
			},
		})
		routes[release.metadataPath] = response{
			contentType: "application/yaml",
			token:       release.token,
			body:        fixture.Metadata,
		}
		routes[release.archiveURLPath] = response{
			contentType: "application/octet-stream",
			token:       release.token,
			body:        archiveData,
		}
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if resp.token != "" {
			if got, want := r.Header.Get(httpAuthorizationHeader), httpBearerAuthorizationPrefix+resp.token; got != want {
				http.Error(w, fmt.Sprintf("authorization = %q, want %q", got, want), http.StatusBadRequest)
				return
			}
		}
		w.Header().Set("Content-Type", resp.contentType)
		_, _ = w.Write(resp.body)
	}))
}

func writeLocalUIManifest(t *testing.T, dir, name, source, version string, spec *providermanifestv1.Spec, files map[string]string) string {
	t.Helper()

	root := filepath.Join(dir, name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", root, err)
	}
	for rel, contents := range files {
		fullPath := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", filepath.Dir(fullPath), err)
		}
		if err := os.WriteFile(fullPath, []byte(contents), 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", fullPath, err)
		}
	}
	manifestPath := filepath.Join(root, "manifest.yaml")
	if spec == nil {
		spec = &providermanifestv1.Spec{}
	}
	if spec.AssetRoot == "" {
		spec.AssetRoot = "dist"
	}
	buildScript := fmt.Sprintf("mkdir -p %s\nprintf '<html>%s</html>\\n' > %s/index.html\n", spec.AssetRoot, name, spec.AssetRoot)
	if err := os.WriteFile(filepath.Join(root, "build.sh"), []byte(buildScript), 0o755); err != nil {
		t.Fatalf("WriteFile build.sh: %v", err)
	}
	build := &providermanifestv1.SourceBuild{Command: []string{"sh", "./build.sh"}, Inputs: []string{"build.sh"}}
	manifest, err := encodeSourceManifestForTest(&providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindUI,
		Source:      source,
		Version:     version,
		DisplayName: testDisplayName(name),
		Build:       build,
		Spec:        spec,
	}, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat(%s): %v", name, err)
	}
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", manifestPath, err)
	}
	return manifestPath
}

func localAppSourceRunCommand(buildOutput string) *providermanifestv1.SourceRun {
	return &providermanifestv1.SourceRun{
		Command: []string{"sh", "-c", "sh ./build.sh && ./" + buildOutput},
	}
}

func writeLocalExecutablePlugin(t *testing.T, dir, name string, operations ...string) string {
	t.Helper()

	root := filepath.Join(dir, name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", root, err)
	}
	buildOutput := ".gestaltd/bin/" + name
	buildScript := fmt.Sprintf("mkdir -p .gestaltd/bin\nprintf '%s' > %s\nchmod +x %s\n", name+"-binary", buildOutput, buildOutput)
	if err := os.WriteFile(filepath.Join(root, "build.sh"), []byte(buildScript), 0o755); err != nil {
		t.Fatalf("WriteFile(build.sh): %v", err)
	}
	manifestPath := filepath.Join(root, "manifest.yaml")
	manifest, err := encodeSourceManifestForTest(&providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      "github.com/test/apps/" + name,
		Version:     "0.0.1-alpha.1",
		DisplayName: testDisplayName(name),
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"build.sh"},
		},
		Run:  localAppSourceRunCommand(buildOutput),
		Spec: withNoAuthDefaultConnection(&providermanifestv1.Spec{}),
	}, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat(%s): %v", name, err)
	}
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", manifestPath, err)
	}

	var builder strings.Builder
	builder.WriteString("name: " + name + "\noperations:\n")
	for _, operation := range operations {
		builder.WriteString("  - id: " + operation + "\n")
		builder.WriteString("    method: GET\n")
	}
	if err := os.WriteFile(filepath.Join(root, "catalog.yaml"), []byte(builder.String()), 0o644); err != nil {
		t.Fatalf("WriteFile(catalog.yaml): %v", err)
	}
	return manifestPath
}

func requiredIndexedDBConfigYAML(t *testing.T, dir, dbPath string) string {
	return requiredComponentConfigYAML(t, dir, dbPath)
}

func mustSelectedHostProviderEntry(t *testing.T, cfg *config.Config, kind config.HostProviderKind) *config.ProviderEntry {
	t.Helper()
	_, entry, err := cfg.SelectedHostProvider(kind)
	if err != nil {
		t.Fatalf("SelectedHostProvider(%s): %v", kind, err)
	}
	return entry
}

func mustLockEntryByName(t *testing.T, entries map[string]LockEntry, name string) LockEntry {
	t.Helper()
	entry, ok := entries[name]
	if !ok {
		t.Fatalf("lock entry %q not found in %#v", name, entries)
	}
	return entry
}

func TestLoadForExecutionAtPath_ResolvesLocalManifestPluginWithoutLockfile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lockfilePath, artifactsDir := configDirPaths(dir)
	manifestPath := filepath.Join(dir, "manifest.yaml")
	buildOutput := ".gestaltd/bin/local-provider"
	buildScript := "mkdir -p .gestaltd/bin\nprintf 'local-provider' > " + buildOutput + "\nchmod +x " + buildOutput + "\n"
	if err := os.WriteFile(filepath.Join(dir, "build.sh"), []byte(buildScript), 0o755); err != nil {
		t.Fatalf("WriteFile build.sh: %v", err)
	}
	manifest, err := encodeSourceManifestForTest(&providermanifestv1.Manifest{
		Source:      "github.com/testowner/apps/local-provider",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Local Provider",
		Description: "Local executable provider",
		Kind:        providermanifestv1.KindApp, Spec: withNoAuthDefaultConnection(&providermanifestv1.Spec{}),
		Run: localAppSourceRunCommand(buildOutput),
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"build.sh"},
		},
	}, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "catalog.yaml"), []byte("name: provider\noperations:\n  - id: ping\n    method: GET\n"), 0o644); err != nil {
		t.Fatalf("WriteFile catalog: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := requiredComponentConfigWithAPIVersionYAML(t, dir, filepath.Join(dir, "gestalt.db")) + `apps:
    example:
      source:
        path: ./manifest.yaml
      static:
        mount: /example
` + `server:
` + requiredServerIndexedDBYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lc := NewLifecycle()
	loaded, _, err := lc.LoadForExecutionAtPaths([]string{cfgPath}, lockfilePath, artifactsDir, false, false)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath: %v", err)
	}

	intg := loaded.Apps["example"]
	if intg == nil || intg.ResolvedManifest == nil {
		t.Fatalf("ResolvedManifest = %+v", intg)
		return
	}
	if intg.DisplayName != "Local Provider" {
		t.Fatalf("DisplayName = %q", intg.DisplayName)
	}
	if intg.Description != "Local executable provider" {
		t.Fatalf("Description = %q", intg.Description)
	}
	if !strings.HasSuffix(filepath.ToSlash(intg.ResolvedManifestPath), filepath.ToSlash(filepath.Join(dir, "manifest.yaml"))) {
		t.Fatalf("ResolvedManifestPath = %q", intg.ResolvedManifestPath)
	}
	if !intg.DevActive {
		t.Fatal("expected DevActive for local source-run app")
	}
	if intg.Static == nil || intg.Static.Mount != "/example" {
		t.Fatalf("Static = %#v, want mount /example", intg.Static)
	}
	if intg.ResolvedStaticRoot != "" {
		t.Fatalf("ResolvedStaticRoot = %q, want empty for dev-active static app", intg.ResolvedStaticRoot)
	}
	if intg.Command != "" {
		t.Fatalf("Command = %q, want empty for source-run app", intg.Command)
	}
	if _, err := os.Stat(filepath.Join(dir, LockfileName)); err != nil {
		t.Fatalf("expected lockfile to be created: %v", err)
	}
}

func TestLoadForExecutionAtPath_ResolvesLocalMCPOAuthManifestPluginWithoutLockfile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lockfilePath, artifactsDir := configDirPaths(dir)
	manifestPath := filepath.Join(dir, "manifest.yaml")
	manifest := []byte(`
kind: app
source: github.com/testowner/apps/notion
version: 0.0.1-alpha.1
displayName: Notion
spec:
  surfaces:
    mcp:
      url: https://mcp.notion.com/mcp
      connection: mcp
  connections:
    mcp:
      mode: subject
      auth:
        type: mcp_oauth
`)
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := requiredComponentConfigWithAPIVersionYAML(t, dir, filepath.Join(dir, "gestalt.db")) + `apps:
    notion:
      source:
        path: ./manifest.yaml
` + `server:
` + requiredServerIndexedDBYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lc := NewLifecycle()
	loaded, _, err := lc.LoadForExecutionAtPaths([]string{cfgPath}, lockfilePath, artifactsDir, false, false)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath: %v", err)
	}

	intg := loaded.Apps["notion"]
	if intg == nil || intg.ResolvedManifest == nil || intg.ResolvedManifest.Spec == nil {
		t.Fatalf("ResolvedManifest = %+v", intg)
		return
	}
	if got := intg.ResolvedManifest.Spec.MCPURL(); got != "https://mcp.notion.com/mcp" {
		t.Fatalf("MCPURL = %q, want %q", got, "https://mcp.notion.com/mcp")
	}
	conn := intg.ResolvedManifest.Spec.Connections["mcp"]
	if conn == nil || conn.Auth == nil {
		t.Fatalf("MCP connection = %#v", conn)
		return
	}
	if got := conn.Auth.Type; got != providermanifestv1.AuthTypeMCPOAuth {
		t.Fatalf("MCP auth type = %q, want %q", got, providermanifestv1.AuthTypeMCPOAuth)
	}
	if _, err := os.Stat(filepath.Join(dir, LockfileName)); err != nil {
		t.Fatalf("expected lockfile to be created: %v", err)
	}
}

func TestLoadForExecutionAtPath_RejectsUndeclaredManifestSurfaceConnections(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		surfaceYAML string
		wantErr     string
	}{
		{
			name: "rest",
			surfaceYAML: `    rest:
      baseUrl: https://example.com
      connection: workspace
      operations:
        - name: ping
          method: GET
          path: /ping
`,
			wantErr: `rest connection references undeclared connection "workspace"`,
		},
		{
			name: "openapi",
			surfaceYAML: `    openapi:
      document: https://example.com/openapi.json
      connection: workspace
`,
			wantErr: `openapi_connection references undeclared connection "workspace"`,
		},
		{
			name: "graphql",
			surfaceYAML: `    graphql:
      url: https://example.com/graphql
      connection: workspace
`,
			wantErr: `graphql_connection references undeclared connection "workspace"`,
		},
		{
			name: "mcp",
			surfaceYAML: `    mcp:
      url: https://example.com/mcp
      connection: workspace
`,
			wantErr: `mcp_connection references undeclared connection "workspace"`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			manifestPath := filepath.Join(dir, "manifest.yaml")
			manifest := fmt.Sprintf(`kind: app
source: github.com/testowner/apps/example
version: 0.0.1-alpha.1
displayName: Example
spec:
  connections:
    default:
      auth:
        type: none
  surfaces:
%s`, tc.surfaceYAML)
			if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
				t.Fatalf("WriteFile manifest: %v", err)
			}

			cfgPath := filepath.Join(dir, "config.yaml")
			cfg := requiredComponentConfigWithAPIVersionYAML(t, dir, filepath.Join(dir, "gestalt.db")) + `apps:
    example:
      source:
        path: ./manifest.yaml
` + `server:
` + requiredServerIndexedDBYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
			if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
				t.Fatalf("WriteFile config: %v", err)
			}

			lockfilePath, artifactsDir := lockAndArtifactsForConfig(cfgPath)
			_, _, err := NewLifecycle().LoadForExecutionAtPaths([]string{cfgPath}, lockfilePath, artifactsDir, false, false)
			if err == nil {
				t.Fatalf("LoadForExecutionAtPath: expected error containing %q", tc.wantErr)
				return
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("LoadForExecutionAtPath error = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}

func TestPrepareAtPath_RejectsAppInvokesField(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	callerManifestPath := writeLocalExecutablePlugin(t, dir, "caller", "invoke")

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := requiredComponentConfigWithAPIVersionYAML(t, dir, filepath.Join(dir, "gestalt.db")) + fmt.Sprintf(`apps:
    caller:
      source:
        path: %q
      invokes:
        - app: target
          operation: ping
server:
`, callerManifestPath) + requiredServerIndexedDBYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	_, err := prepareAtPathInTest(t, NewLifecycle(), cfgPath)
	if err == nil || !strings.Contains(err.Error(), `field invokes not found`) {
		t.Fatalf("PrepareAtPath error = %v, want field invokes not found", err)
	}
}

func TestPrepareAtPath_RejectsInvalidAppWorkflowCapabilitiesShape(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "missing operations",
			body: `apps:
    caller:
      source:
        path: ./caller/manifest.yaml
      capabilities:
        workflow: {}
`,
			want: `apps.caller.capabilities.workflow.operations is required`,
		},
		{
			name: "unsupported operation",
			body: `apps:
    caller:
      source:
        path: ./caller/manifest.yaml
      capabilities:
        workflow:
          operations:
            - workflow.create
`,
			want: `apps.caller.capabilities.workflow.operations[0] "workflow.create" is not supported`,
		},
		{
			name: "duplicate operation",
			body: `apps:
    caller:
      source:
        path: ./caller/manifest.yaml
      capabilities:
        workflow:
          operations:
            - events.deliver
            - events.deliver
`,
			want: `apps.caller.capabilities.workflow.operations[1] duplicates operations[0]`,
		},
		{
			name: "non app provider",
			body: `  cache:
    shared:
      source:
        path: ./cache-manifest.yaml
      capabilities:
        workflow:
          operations:
            - events.deliver
`,
			want: `providers.cache.shared.capabilities is only supported on apps.*`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			caseDir := t.TempDir()
			cfgPath := filepath.Join(caseDir, "config.yaml")
			cfg := requiredComponentConfigWithAPIVersionYAML(t, caseDir, filepath.Join(caseDir, "gestalt.db")) + tc.body + `server:
` + requiredServerIndexedDBYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
			if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
				t.Fatalf("WriteFile config: %v", err)
			}

			_, err := prepareAtPathInTest(t, NewLifecycle(), cfgPath)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("PrepareAtPath error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestPrepareAtPath_AllowsEffectiveOperationAlias(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	targetManifestPath := writeLocalExecutablePlugin(t, dir, "target", "ping")

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := requiredComponentConfigWithAPIVersionYAML(t, dir, filepath.Join(dir, "gestalt.db")) + fmt.Sprintf(`apps:
    target:
      source:
        path: %q
      allowedOperations:
        ping:
          alias: renamed_ping
server:
`, targetManifestPath) + requiredServerIndexedDBYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	if _, err := prepareAtPathInTest(t, NewLifecycle(), cfgPath); err != nil {
		t.Fatalf("PrepareAtPath: %v", err)
	}
}

func TestPrepareAtPath_AllowsManagedPluginsOnFirstPrepare(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	const callerRef = "github.com/testowner/apps/caller"
	const targetRef = "github.com/testowner/apps/target"
	const version = "0.0.1-alpha.1"

	callerPkg := mustBuildManagedProviderPackage(t, dir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      callerRef,
		Version:     version,
		DisplayName: "Caller",
		Entrypoint: &providermanifestv1.Entrypoint{
			ArtifactPath: filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "app")),
		},
		Spec: withNoAuthDefaultConnection(&providermanifestv1.Spec{}),
	}, map[string]string{
		filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "app")): "caller-binary",
	}, true)

	targetPkg := mustBuildManagedProviderPackage(t, dir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      targetRef,
		Version:     version,
		DisplayName: "Target",
		Entrypoint: &providermanifestv1.Entrypoint{
			ArtifactPath: filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "app")),
		},
		Spec: withNoAuthDefaultConnection(&providermanifestv1.Spec{}),
	}, map[string]string{
		filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "app")): "target-binary",
	}, true)

	srv := newManagedMetadataServer(t, []managedMetadataRelease{
		{
			metadataPath:    "/providers/caller/v" + version + "/provider-release.yaml",
			archiveURLPath:  "/providers/caller/v" + version + "/caller.tar.gz",
			archiveFilePath: callerPkg,
			packageSource:   callerRef,
			version:         version,
			kind:            providermanifestv1.KindApp,
		},
		{
			metadataPath:    "/providers/target/v" + version + "/provider-release.yaml",
			archiveURLPath:  "/providers/target/v" + version + "/target.tar.gz",
			archiveFilePath: targetPkg,
			packageSource:   targetRef,
			version:         version,
			kind:            providermanifestv1.KindApp,
		},
	})
	defer srv.Close()

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := requiredComponentConfigWithAPIVersionYAML(t, dir, filepath.Join(dir, "gestalt.db")) + fmt.Sprintf(`apps:
    caller:
      source: %s/providers/caller/v%s/provider-release.yaml
    target:
      source: %s/providers/target/v%s/provider-release.yaml
server:
`, srv.URL, version, srv.URL, version) + requiredServerIndexedDBYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lc := NewLifecycle()
	lockfilePath, artifactsDir := lockAndArtifactsForConfig(cfgPath)
	lock, err := lc.PrepareAtPaths([]string{cfgPath}, lockfilePath, artifactsDir)
	if err != nil {
		t.Fatalf("PrepareAtPath: %v", err)
	}
	if lock.Providers.App["caller"].Executable == "" || lock.Providers.App["target"].Executable == "" {
		t.Fatalf("prepared app executables = %#v", lock.Providers)
	}
}

func TestLoadForExecutionAtPath_RejectsAppInvokesField(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lockfilePath, artifactsDir := configDirPaths(dir)
	callerManifestPath := writeLocalExecutablePlugin(t, dir, "caller", "invoke")

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := requiredComponentConfigWithAPIVersionYAML(t, dir, filepath.Join(dir, "gestalt.db")) + fmt.Sprintf(`apps:
    caller:
      source:
        path: %q
      invokes:
        - app: target
          operation: missing
server:
`, callerManifestPath) + requiredServerIndexedDBYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	_, _, err := NewLifecycle().LoadForExecutionAtPaths([]string{cfgPath}, lockfilePath, artifactsDir, false, false)
	if err == nil || !strings.Contains(err.Error(), `field invokes not found`) {
		t.Fatalf("LoadForExecutionAtPath error = %v, want field invokes not found", err)
	}
}

func TestLoadForExecutionAtPath_ResolvesLocalMountedUIWithoutLockfile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		uiConfigYAML string
		extraYAML    string
		uiKey        string
		wantPath     string
		wantPolicy   string
		ownedUIPath  string
		uiManifest   string
		sourceBuild  bool
		createApp    bool
		wantErr      string
	}{
		{
			name: "direct mounted ui",
			uiConfigYAML: `  ui:
    roadmap:
      source:
        path: ./ui/manifest.yaml
      path: /create-customer-roadmap-review
`,
			uiKey:    "roadmap",
			wantPath: "/create-customer-roadmap-review",
		},
		{
			name: "direct mounted source-built ui",
			uiConfigYAML: `  ui:
    roadmap:
      source:
        path: ./ui/manifest.yaml
      path: /create-customer-roadmap-review
`,
			uiKey:       "roadmap",
			wantPath:    "/create-customer-roadmap-review",
			sourceBuild: true,
		},
		{
			name: "plugin ui object binds explicit ui",
			uiConfigYAML: "  ui:\n" +
				"    roadmap:\n" +
				"      source:\n" +
				"        path: ./ui/manifest.yaml\n" +
				"apps:\n" +
				"    roadmap:\n" +
				"      source:\n" +
				"        path: ./app/manifest.yaml\n" +
				"      ui:\n" +
				"        bundle: roadmap\n" +
				"        path: /create-customer-roadmap-review\n" +
				"      authorizationPolicy: roadmap_policy\n",
			uiKey:      "roadmap",
			wantPath:   "/create-customer-roadmap-review",
			wantPolicy: "roadmap_policy",
			createApp:  true,
		},
		{
			name: "plugin owned ui via app ui path",
			uiConfigYAML: `apps:
    roadmap:
      source:
        path: ./app/manifest.yaml
      ui:
        path: /create-customer-roadmap-review
      authorizationPolicy: roadmap_policy
`,
			uiKey:       "roadmap",
			wantPath:    "/create-customer-roadmap-review",
			wantPolicy:  "roadmap_policy",
			ownedUIPath: "../ui/manifest.yaml",
		},
		{
			name: "plugin owned source-built ui via app ui path",
			uiConfigYAML: `apps:
    roadmap:
      source:
        path: ./app/manifest.yaml
      ui:
        path: /create-customer-roadmap-review
`,
			uiKey:       "roadmap",
			wantPath:    "/create-customer-roadmap-review",
			ownedUIPath: "../ui/manifest.yaml",
			sourceBuild: true,
		},
		{
			name: "plugin owned ui via app ui path with noncanonical manifest filename",
			uiConfigYAML: `apps:
    roadmap:
      source:
        path: ./app/manifest.yaml
      ui:
        path: /create-customer-roadmap-review
      authorizationPolicy: roadmap_policy
`,
			uiKey:       "roadmap",
			wantPath:    "/create-customer-roadmap-review",
			wantPolicy:  "roadmap_policy",
			ownedUIPath: "../ui/ui-manifest.yaml",
			uiManifest:  "ui-manifest.yaml",
		},
		{
			name: "plugin owned ui with same-name ui overlay",
			uiConfigYAML: `  ui:
    roadmap:
      source:
        path: ./ui/manifest.yaml
apps:
    roadmap:
      source:
        path: ./app/manifest.yaml
      ui:
        path: /create-customer-roadmap-review
      authorizationPolicy: roadmap_policy
`,
			uiKey:       "roadmap",
			wantPath:    "/create-customer-roadmap-review",
			wantPolicy:  "roadmap_policy",
			ownedUIPath: "../ui/manifest.yaml",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			lockfilePath := filepath.Join(dir, LockfileName)
			artifactsDir := filepath.Join(dir, "artifacts")
			uiDir := filepath.Join(dir, "ui")
			if err := os.MkdirAll(uiDir, 0o755); err != nil {
				t.Fatalf("MkdirAll ui dir: %v", err)
			}
			manifestName := cmp.Or(tc.uiManifest, "manifest.yaml")
			manifestPath := filepath.Join(uiDir, manifestName)
			var spec *providermanifestv1.Spec
			var build *providermanifestv1.SourceBuild
			if tc.sourceBuild {
				build = &providermanifestv1.SourceBuild{
					Command: []string{"sh", "./build.sh"},
					Inputs:  []string{"build.sh"},
				}
				if err := os.WriteFile(filepath.Join(uiDir, "build.sh"), []byte("mkdir -p dist\nprintf '<html>roadmap</html>\\n' > dist/index.html\n"), 0o755); err != nil {
					t.Fatalf("WriteFile build.sh: %v", err)
				}
			} else {
				if err := os.MkdirAll(filepath.Join(uiDir, "dist"), 0o755); err != nil {
					t.Fatalf("MkdirAll ui dist: %v", err)
				}
				if err := os.WriteFile(filepath.Join(uiDir, "dist", "index.html"), []byte("<html>roadmap</html>"), 0o644); err != nil {
					t.Fatalf("WriteFile index.html: %v", err)
				}
				build = &providermanifestv1.SourceBuild{
					Command: []string{"sh", "./build.sh"},
				}
				if err := os.WriteFile(filepath.Join(uiDir, "build.sh"), []byte("mkdir -p dist\nprintf '<html>roadmap</html>\\n' > dist/index.html\n"), 0o755); err != nil {
					t.Fatalf("WriteFile build.sh: %v", err)
				}
			}
			if spec == nil {
				spec = &providermanifestv1.Spec{}
			}
			spec.AssetRoot = "dist"
			if tc.wantPolicy != "" {
				spec.Routes = []providermanifestv1.UIRoute{
					{Path: "/", AllowedRoles: []string{"viewer"}},
				}
			}
			manifest, err := encodeSourceManifestForTest(&providermanifestv1.Manifest{
				Kind:        providermanifestv1.KindUI,
				Source:      "github.com/testowner/web/roadmap",
				Version:     "0.0.1-alpha.1",
				DisplayName: "Roadmap UI",
				Build:       build,
				Spec:        spec,
			}, providerpkg.ManifestFormatYAML)
			if err != nil {
				t.Fatalf("EncodeManifest: %v", err)
			}
			if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
				t.Fatalf("WriteFile manifest: %v", err)
			}
			if tc.createApp || tc.extraYAML != "" || tc.ownedUIPath != "" {
				pluginManifestPath := filepath.Join(dir, "app", "manifest.yaml")
				if err := os.MkdirAll(filepath.Dir(pluginManifestPath), 0o755); err != nil {
					t.Fatalf("MkdirAll app dir: %v", err)
				}
				pluginBuildOutput := ".gestaltd/bin/roadmap"
				if err := os.WriteFile(filepath.Join(dir, "app", "build.sh"), []byte(fmt.Sprintf("mkdir -p .gestaltd/bin\nprintf 'roadmap-plugin' > %s\nchmod +x %s\n", pluginBuildOutput, pluginBuildOutput)), 0o755); err != nil {
					t.Fatalf("WriteFile build.sh: %v", err)
				}
				pluginSpec := withNoAuthDefaultConnection(&providermanifestv1.Spec{})
				if tc.ownedUIPath != "" {
					pluginSpec.UI = &providermanifestv1.OwnedUI{Path: tc.ownedUIPath}
				}
				pluginManifest, err := encodeSourceManifestForTest(&providermanifestv1.Manifest{
					Source:      "github.com/testowner/apps/roadmap",
					Version:     "0.0.1-alpha.1",
					DisplayName: "Roadmap Plugin",
					Kind:        providermanifestv1.KindApp,
					Spec:        pluginSpec,
					Build: &providermanifestv1.SourceBuild{
						Command: []string{"sh", "./build.sh"},
						Inputs:  []string{"build.sh"},
					},
					Run:        localAppSourceRunCommand(pluginBuildOutput),
					Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: pluginBuildOutput},
				}, providerpkg.ManifestFormatYAML)
				if err != nil {
					t.Fatalf("EncodePluginManifest: %v", err)
				}
				if err := os.WriteFile(pluginManifestPath, pluginManifest, 0o644); err != nil {
					t.Fatalf("WriteFile app manifest: %v", err)
				}
				if err := os.WriteFile(filepath.Join(dir, "app", "catalog.yaml"), []byte("name: roadmap\noperations:\n  - id: ping\n    method: GET\n"), 0o644); err != nil {
					t.Fatalf("WriteFile app catalog: %v", err)
				}
			}

			cfgPath := filepath.Join(dir, "config.yaml")
			cfg := requiredComponentConfigWithAPIVersionYAML(t, dir, filepath.Join(dir, "gestalt.db")) + tc.uiConfigYAML + tc.extraYAML + `server:
` + requiredServerIndexedDBYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
			if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
				t.Fatalf("WriteFile config: %v", err)
			}

			lc := NewLifecycle()
			loaded, _, err := lc.LoadForExecutionAtPaths([]string{cfgPath}, lockfilePath, artifactsDir, false, false)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("LoadForExecutionAtPath: expected error containing %q", tc.wantErr)
					return
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("LoadForExecutionAtPath error = %q, want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadForExecutionAtPath: %v", err)
			}

			entry := loaded.Providers.UI[tc.uiKey]
			if entry == nil {
				t.Fatalf(`Providers.UI[%q] = nil`, tc.uiKey)
				return
			}
			if entry.ResolvedManifest == nil {
				t.Fatal("ResolvedManifest = nil")
				return
			}
			gotManifestPath := filepath.ToSlash(entry.ResolvedManifestPath)
			wantUIManifest := filepath.ToSlash(filepath.Join("ui", tc.uiKey, "manifest.yaml"))
			wantSourceUIManifest := filepath.ToSlash(filepath.Join("ui", "manifest.yaml"))
			wantOwnedManifest := filepath.ToSlash(filepath.Join("providers", "roadmap", "_owned_ui", "ui", "manifest.yaml"))
			if !strings.HasSuffix(gotManifestPath, wantUIManifest) && !strings.HasSuffix(gotManifestPath, wantSourceUIManifest) && !strings.HasSuffix(gotManifestPath, wantOwnedManifest) {
				t.Fatalf("ResolvedManifestPath = %q", gotManifestPath)
			}
			gotAssetRoot := filepath.ToSlash(entry.ResolvedAssetRoot)
			wantUIAssetRoot := filepath.ToSlash(filepath.Join("ui", tc.uiKey, "dist"))
			wantSourceUIAssetRoot := filepath.ToSlash(filepath.Join("ui", "dist"))
			wantOwnedAssetRoot := filepath.ToSlash(filepath.Join("providers", "roadmap", "_owned_ui", "ui", "dist"))
			if !strings.HasSuffix(gotAssetRoot, wantUIAssetRoot) && !strings.HasSuffix(gotAssetRoot, wantSourceUIAssetRoot) && !strings.HasSuffix(gotAssetRoot, wantOwnedAssetRoot) {
				t.Fatalf("ResolvedAssetRoot = %q", gotAssetRoot)
			}
			if got := entry.Path; got != tc.wantPath {
				t.Fatalf("Path = %q, want %q", got, tc.wantPath)
			}
			if got := entry.AuthorizationPolicy; got != tc.wantPolicy {
				t.Fatalf("AuthorizationPolicy = %q, want %q", got, tc.wantPolicy)
			}
			if tc.wantPolicy != "" {
				if got := entry.OwnerApp; got != "roadmap" {
					t.Fatalf("OwnerApp = %q, want %q", got, "roadmap")
				}
			}
			if tc.wantPolicy != "" {
				app := loaded.Apps["roadmap"]
				if app == nil {
					t.Fatal(`Apps["roadmap"] = nil`)
					return
				}
				if got := app.AuthorizationPolicy; got != tc.wantPolicy {
					t.Fatalf("App AuthorizationPolicy = %q, want %q", got, tc.wantPolicy)
				}
			}
			if _, err := os.Stat(filepath.Join(dir, LockfileName)); err != nil {
				t.Fatalf("expected lockfile to be created: %v", err)
			}
		})
	}
}

func TestLoadForExecutionAtPath_AllowsLockedExplicitLocalUIWithoutPreparedUILockEntry(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lockfilePath := filepath.Join(dir, LockfileName)
	artifactsDir := filepath.Join(dir, "artifacts")
	uiDir := filepath.Join(dir, "ui")
	if err := os.MkdirAll(filepath.Join(uiDir, "dist"), 0o755); err != nil {
		t.Fatalf("MkdirAll ui dist: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uiDir, "dist", "index.html"), []byte("<html>roadmap</html>"), 0o644); err != nil {
		t.Fatalf("WriteFile index.html: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uiDir, "build.sh"), []byte("mkdir -p dist\nprintf '<html>roadmap</html>\\n' > dist/index.html\n"), 0o755); err != nil {
		t.Fatalf("WriteFile build.sh: %v", err)
	}
	manifest, err := encodeSourceManifestForTest(&providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindUI,
		Source:      "github.com/testowner/web/roadmap",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Roadmap UI",
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"build.sh"},
		},
		Spec: &providermanifestv1.Spec{AssetRoot: "dist"},
	}, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uiDir, "manifest.yaml"), manifest, 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := requiredComponentConfigWithAPIVersionYAML(t, dir, filepath.Join(dir, "gestalt.db")) + `  ui:
    roadmap:
      source:
        path: ./ui/manifest.yaml
      path: /create-customer-roadmap-review
server:
` + requiredServerIndexedDBYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lc := NewLifecycle()
	if _, err := lc.PrepareAtPaths([]string{cfgPath}, lockfilePath, artifactsDir); err != nil {
		t.Fatalf("PrepareAtPath: %v", err)
	}
	lockPath := filepath.Join(dir, LockfileName)
	lock, err := ReadLockfile(lockPath)
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	delete(lock.Providers.UI, "roadmap")
	if err := WriteLockfile(lockPath, lock); err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}

	cfgLoaded, _, err := lc.LoadForExecutionAtPaths([]string{cfgPath}, lockfilePath, artifactsDir, true, false)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath locked: %v", err)
	}
	if cfgLoaded.Providers.UI["roadmap"].ResolvedManifest == nil {
		t.Fatal(`Providers.UI["roadmap"].ResolvedManifest = nil`)
	}
}

func TestLoadForExecutionAtPath_ResolvesMountedUIThemeConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		themeYAML      string
		wantStylesheet string
		wantAssetsDir  string
		wantErr        string
	}{
		{
			name: "stylesheet and assetsDir resolved against the config dir",
			themeYAML: `      config:
        theme:
          stylesheet: ./theme/tenant.css
          assetsDir: ./theme/assets
`,
			wantStylesheet: filepath.Join("theme", "tenant.css"),
			wantAssetsDir:  filepath.Join("theme", "assets"),
		},
		{
			name: "no theme block leaves theme paths empty",
		},
		{
			name: "missing stylesheet fails sync",
			themeYAML: `      config:
        theme:
          stylesheet: ./theme/missing.css
`,
			wantErr: "theme stylesheet not found",
		},
		{
			name: "assetsDir pointing at a file fails sync",
			themeYAML: `      config:
        theme:
          stylesheet: ./theme/tenant.css
          assetsDir: ./theme/tenant.css
`,
			wantErr: "is not a directory",
		},
		{
			name: "unknown theme key fails decode",
			themeYAML: `      config:
        theme:
          stylesheet: ./theme/tenant.css
          asetsDir: ./theme/assets
`,
			wantErr: "theme config",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			lockfilePath, artifactsDir := configDirPaths(dir)
			uiDir := filepath.Join(dir, "ui")
			if err := os.MkdirAll(filepath.Join(uiDir, "dist"), 0o755); err != nil {
				t.Fatalf("MkdirAll ui dist: %v", err)
			}
			if err := os.WriteFile(filepath.Join(uiDir, "dist", "index.html"), []byte("<html>roadmap</html>"), 0o644); err != nil {
				t.Fatalf("WriteFile index.html: %v", err)
			}
			if err := os.WriteFile(filepath.Join(uiDir, "build.sh"), []byte("mkdir -p dist\nprintf '<html>roadmap</html>\\n' > dist/index.html\n"), 0o755); err != nil {
				t.Fatalf("WriteFile build.sh: %v", err)
			}
			manifest, err := encodeSourceManifestForTest(&providermanifestv1.Manifest{
				Kind:        providermanifestv1.KindUI,
				Source:      "github.com/testowner/web/roadmap",
				Version:     "0.0.1-alpha.1",
				DisplayName: "Roadmap UI",
				Build: &providermanifestv1.SourceBuild{
					Command: []string{"sh", "./build.sh"},
				},
				Spec: &providermanifestv1.Spec{AssetRoot: "dist"},
			}, providerpkg.ManifestFormatYAML)
			if err != nil {
				t.Fatalf("EncodeManifest: %v", err)
			}
			if err := os.WriteFile(filepath.Join(uiDir, "manifest.yaml"), manifest, 0o644); err != nil {
				t.Fatalf("WriteFile manifest: %v", err)
			}
			if err := os.MkdirAll(filepath.Join(dir, "theme", "assets"), 0o755); err != nil {
				t.Fatalf("MkdirAll theme assets: %v", err)
			}
			if err := os.WriteFile(filepath.Join(dir, "theme", "tenant.css"), []byte(":root{--brand:#123456;}"), 0o644); err != nil {
				t.Fatalf("WriteFile tenant.css: %v", err)
			}

			cfgPath := filepath.Join(dir, "config.yaml")
			cfg := requiredComponentConfigWithAPIVersionYAML(t, dir, filepath.Join(dir, "gestalt.db")) + `  ui:
    roadmap:
      source:
        path: ./ui/manifest.yaml
      path: /roadmap
` + tc.themeYAML + `server:
` + requiredServerIndexedDBYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
			if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
				t.Fatalf("WriteFile config: %v", err)
			}

			loaded, _, err := NewLifecycle().LoadForExecutionAtPaths([]string{cfgPath}, lockfilePath, artifactsDir, false, false)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("LoadForExecutionAtPath: expected error containing %q", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("LoadForExecutionAtPath error = %q, want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadForExecutionAtPath: %v", err)
			}

			entry := loaded.Providers.UI["roadmap"]
			if entry == nil {
				t.Fatal(`Providers.UI["roadmap"] = nil`)
			}
			wantStylesheet := ""
			if tc.wantStylesheet != "" {
				wantStylesheet = filepath.Join(dir, tc.wantStylesheet)
			}
			if got := entry.ResolvedThemeStylesheet; got != wantStylesheet {
				t.Fatalf("ResolvedThemeStylesheet = %q, want %q", got, wantStylesheet)
			}
			wantAssetsDir := ""
			if tc.wantAssetsDir != "" {
				wantAssetsDir = filepath.Join(dir, tc.wantAssetsDir)
			}
			if got := entry.ResolvedThemeAssetsDir; got != wantAssetsDir {
				t.Fatalf("ResolvedThemeAssetsDir = %q, want %q", got, wantAssetsDir)
			}
		})
	}
}

func TestLoadForExecutionAtPath_ResolvesManagedPluginOwnedUIFromManagedPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	var lockfilePath, artifactsDir string
	const pluginRef = "github.com/testowner/apps/roadmap"
	const version = "0.0.1-alpha.1"

	pkgDir := filepath.Join(dir, "roadmap-plugin-pkg")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("MkdirAll package dir: %v", err)
	}

	artifactPath := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "app"))
	artifactContent := []byte("plugin-binary")
	artifactFullPath := filepath.Join(pkgDir, filepath.FromSlash(artifactPath))
	if err := os.MkdirAll(filepath.Dir(artifactFullPath), 0o755); err != nil {
		t.Fatalf("MkdirAll artifact dir: %v", err)
	}
	if err := os.WriteFile(artifactFullPath, artifactContent, 0o755); err != nil {
		t.Fatalf("WriteFile artifact: %v", err)
	}

	ownedUIManifestPath := filepath.ToSlash(filepath.Join("_owned_ui", "roadmap-ui", providerpkg.ManifestFile))
	ownedUIManifestBytes, err := providerpkg.EncodeManifest(&providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindUI,
		Source:      "github.com/testowner/web/roadmap-review",
		Version:     version,
		DisplayName: "Roadmap Review UI",
		Spec: &providermanifestv1.Spec{
			AssetRoot: "dist",
		},
	})
	if err != nil {
		t.Fatalf("Encode owned UI manifest: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(pkgDir, "_owned_ui", "roadmap-ui", "dist"), 0o755); err != nil {
		t.Fatalf("MkdirAll owned UI dist: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, filepath.FromSlash(ownedUIManifestPath)), ownedUIManifestBytes, 0o644); err != nil {
		t.Fatalf("WriteFile owned UI manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "_owned_ui", "roadmap-ui", "dist", "index.html"), []byte("<html>roadmap review</html>"), 0o644); err != nil {
		t.Fatalf("WriteFile owned UI index: %v", err)
	}

	sum := sha256.Sum256(artifactContent)
	pluginManifestBytes, err := providerpkg.EncodeManifest(&providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      pluginRef,
		Version:     version,
		DisplayName: "Roadmap Review",
		Entrypoint: &providermanifestv1.Entrypoint{
			ArtifactPath: artifactPath,
		},
		Artifacts: []providermanifestv1.Artifact{{
			OS:     runtime.GOOS,
			Arch:   runtime.GOARCH,
			Path:   artifactPath,
			SHA256: hex.EncodeToString(sum[:]),
		}},
		Spec: withNoAuthDefaultConnection(&providermanifestv1.Spec{
			UI: &providermanifestv1.OwnedUI{
				Path: ownedUIManifestPath,
			},
		}),
	})
	if err != nil {
		t.Fatalf("Encode app manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, providerpkg.ManifestFile), pluginManifestBytes, 0o644); err != nil {
		t.Fatalf("WriteFile app manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "catalog.yaml"), []byte("name: roadmap\noperations:\n  - id: ping\n    method: GET\n"), 0o644); err != nil {
		t.Fatalf("WriteFile catalog: %v", err)
	}
	outsideOwnedUIPath := filepath.Join(dir, "owned-ui", "manifest.json")
	if err := os.MkdirAll(filepath.Dir(outsideOwnedUIPath), 0o755); err != nil {
		t.Fatalf("MkdirAll owned ui dir: %v", err)
	}
	outsideOwnedUIManifest, err := providerpkg.EncodeManifest(&providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindUI,
		Source:      "github.com/testowner/web/outside-roadmap",
		Version:     version,
		DisplayName: "Outside Roadmap UI",
		Spec:        &providermanifestv1.Spec{AssetRoot: "dist"},
	})
	if err != nil {
		t.Fatalf("Encode outside owned UI manifest: %v", err)
	}
	if err := os.WriteFile(outsideOwnedUIPath, outsideOwnedUIManifest, 0o644); err != nil {
		t.Fatalf("WriteFile outside owned UI manifest: %v", err)
	}

	pkgPath := filepath.Join(dir, "roadmap-plugin-pkg.tar.gz")
	if err := providerpkg.CreatePackageFromDir(pkgDir, pkgPath); err != nil {
		t.Fatalf("CreatePackageFromDir: %v", err)
	}

	srv := newManagedMetadataServer(t, []managedMetadataRelease{{
		metadataPath:    "/providers/roadmap-plugin/v" + version + "/provider-release.yaml",
		archiveURLPath:  "/providers/roadmap-plugin/v" + version + "/roadmap-app.tar.gz",
		archiveFilePath: pkgPath,
		packageSource:   pluginRef,
		version:         version,
		kind:            providermanifestv1.KindApp,
		allowInvalid:    true,
	}})
	defer srv.Close()

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := requiredComponentConfigWithAPIVersionYAML(t, dir, filepath.Join(dir, "gestalt.db")) + `apps:
  roadmap:
    source: ` + srv.URL + `/providers/roadmap-plugin/v` + version + `/provider-release.yaml
    ui:
      path: /create-customer-roadmap-review
` + `server:
` + requiredServerIndexedDBYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lc := NewLifecycle()
	lockfilePath, artifactsDir = lockAndArtifactsForConfig(cfgPath)
	if _, err := lc.PrepareAtPaths([]string{cfgPath}, lockfilePath, artifactsDir); err != nil {
		t.Fatalf("PrepareAtPath: %v", err)
	}

	lock, err := ReadLockfile(filepath.Join(dir, LockfileName))
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	pluginLock := lock.Providers.App["roadmap"]
	pluginLock.ArtifactManifest = ""
	lock.Providers.App["roadmap"] = pluginLock
	if err := WriteLockfile(filepath.Join(dir, LockfileName), lock); err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}

	for _, locked := range []bool{false, true} {
		loaded, _, err := lc.LoadForExecutionAtPaths([]string{cfgPath}, lockfilePath, artifactsDir, locked, false)
		if err != nil {
			t.Fatalf("LoadForExecutionAtPath(locked=%t): %v", locked, err)
		}
		entry := loaded.Providers.UI["roadmap"]
		if entry == nil || entry.ResolvedManifest == nil {
			t.Fatalf("Resolved plugin-owned UI = %+v", entry)
			return
		}
		if entry.Path != "/create-customer-roadmap-review" {
			t.Fatalf("entry.Path = %q, want %q", entry.Path, "/create-customer-roadmap-review")
		}
		if got, want := filepath.ToSlash(entry.ResolvedManifestPath), filepath.ToSlash(filepath.Join("_owned_ui", "roadmap-ui", providerpkg.ManifestFile)); !strings.HasSuffix(got, want) {
			t.Fatalf("ResolvedManifestPath = %q, want suffix %q", got, want)
		}
		if got, want := filepath.ToSlash(entry.ResolvedAssetRoot), filepath.ToSlash(filepath.Join("_owned_ui", "roadmap-ui", "dist")); !strings.HasSuffix(got, want) {
			t.Fatalf("ResolvedAssetRoot = %q, want suffix %q", got, want)
		}
	}

	rewrittenLock, err := ReadLockfile(filepath.Join(dir, LockfileName))
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	if got := rewrittenLock.Providers.App["roadmap"].ArtifactManifest; got != "" {
		t.Fatalf("lock.Providers.App[roadmap].ArtifactManifest = %q, want stale value preserved", got)
	}
	if len(rewrittenLock.Providers.UI) != 0 {
		t.Fatalf("lock.Providers.UI = %#v, want no separate UI entries for in-package owned UI", rewrittenLock.Providers.UI)
	}
}

func TestLoadForExecutionAtPath_RefreshesManagedPluginWhenGenericArchiveLockIsStale(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	var lockfilePath, artifactsDir string
	const pluginRef = "github.com/testowner/apps/roadmap"
	const version = "0.0.1-alpha.1"

	pluginPkg := mustBuildManagedProviderPackage(t, dir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      pluginRef,
		Version:     version,
		DisplayName: "Roadmap Review",
		Entrypoint: &providermanifestv1.Entrypoint{
			ArtifactPath: filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "app")),
		},
		Spec: withNoAuthDefaultConnection(&providermanifestv1.Spec{}),
	}, map[string]string{
		filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "app")): "plugin-binary-" + version,
	}, true)

	srv := newManagedMetadataServer(t, []managedMetadataRelease{{
		metadataPath:    "/providers/roadmap-plugin/v" + version + "/provider-release.yaml",
		archiveURLPath:  "/providers/roadmap-plugin/v" + version + "/roadmap-app.tar.gz",
		archiveFilePath: pluginPkg,
		packageSource:   pluginRef,
		version:         version,
		kind:            providermanifestv1.KindApp,
		allowInvalid:    true,
	}})
	defer srv.Close()

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := requiredComponentConfigWithAPIVersionYAML(t, dir, filepath.Join(dir, "gestalt.db")) + `apps:
  roadmap:
    source: ` + srv.URL + `/providers/roadmap-plugin/v` + version + `/provider-release.yaml
server:
` + requiredServerIndexedDBYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lc := NewLifecycle()
	lockfilePath, artifactsDir = lockAndArtifactsForConfig(cfgPath)
	if _, err := lc.PrepareAtPaths([]string{cfgPath}, lockfilePath, artifactsDir); err != nil {
		t.Fatalf("PrepareAtPath: %v", err)
	}

	loadedCfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	paths := lifecyclePathsForConfig(cfgPath)
	staleLock := &Lockfile{
		Providers: providerLockBuckets{
			App: map[string]LockEntry{
				"roadmap": {
					InputDigest: mustFingerprint(t, "roadmap", loadedCfg.Apps["roadmap"], paths.configDir),
					Source:      srv.URL + "/providers/roadmap-plugin/v" + version + "/provider-release.yaml",
					Version:     version,
					Archives: map[string]LockArchive{
						"generic": {URL: "https://example.com/roadmap.tar.gz", SHA256: "abc123"},
					},
				},
			},
		},
	}
	if err := WriteLockfile(filepath.Join(dir, LockfileName), staleLock); err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}

	loaded, _, err := lc.LoadForExecutionAtPaths([]string{cfgPath}, lockfilePath, artifactsDir, false, false)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath: %v", err)
	}
	if loaded.Apps["roadmap"] == nil || loaded.Apps["roadmap"].ResolvedManifest == nil {
		t.Fatalf("ResolvedManifest = %+v", loaded.Apps["roadmap"])
		return
	}
	if loaded.Apps["roadmap"].Command == "" {
		t.Fatal("loaded app command is empty")
	}

	rewrittenLock, err := ReadLockfile(filepath.Join(dir, LockfileName))
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	if _, ok := rewrittenLock.Providers.App["roadmap"].Archives["generic"]; ok {
		t.Fatalf("rewritten lock still contains generic archive: %#v", rewrittenLock.Providers.App["roadmap"].Archives)
	}
	if _, ok := rewrittenLock.Providers.App["roadmap"].Archives[providerpkg.CurrentPlatformString()]; !ok {
		t.Fatalf("rewritten lock missing current platform archive: %#v", rewrittenLock.Providers.App["roadmap"].Archives)
	}
}

func TestLoadForExecutionAtPath_LockedManagedDeclarativePluginMaterializesBeforeGenericPolicyCheck(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	var lockfilePath, artifactsDir string
	const pluginRef = "github.com/testowner/apps/roadmap"
	const oldVersion = "0.0.1-alpha.1"
	const newVersion = "0.0.2-alpha.1"

	buildExecutableApp := func(version string) string {
		return mustBuildManagedProviderPackage(t, dir, &providermanifestv1.Manifest{
			Kind:        providermanifestv1.KindApp,
			Source:      pluginRef,
			Version:     version,
			DisplayName: "Roadmap Review",
			Entrypoint: &providermanifestv1.Entrypoint{
				ArtifactPath: filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "app")),
			},
			Spec: withNoAuthDefaultConnection(&providermanifestv1.Spec{}),
		}, map[string]string{
			filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "app")): "plugin-binary-" + version,
		}, true)
	}
	buildDeclarativeApp := func(version string) string {
		return mustBuildManagedProviderPackage(t, dir, &providermanifestv1.Manifest{
			Kind:        providermanifestv1.KindApp,
			Source:      pluginRef,
			Version:     version,
			DisplayName: "Roadmap Review",
			Spec: withNoAuthDefaultConnection(&providermanifestv1.Spec{
				Surfaces: &providermanifestv1.ProviderSurfaces{
					REST: &providermanifestv1.RESTSurface{
						BaseURL: "https://api.example.com",
						Operations: []providermanifestv1.ProviderOperation{
							{
								Name:   "ping",
								Method: "GET",
								Path:   "/ping",
							},
						},
					},
				},
			}),
		}, nil, false)
	}

	oldPluginPkg := buildExecutableApp(oldVersion)
	newPluginPkg := buildDeclarativeApp(newVersion)
	newPluginArchive, err := os.ReadFile(newPluginPkg)
	if err != nil {
		t.Fatalf("ReadFile new declarative package: %v", err)
	}
	newPluginArchiveSum := sha256.Sum256(newPluginArchive)
	archiveServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(newPluginArchive)
	}))
	defer archiveServer.Close()

	srv := newManagedMetadataServer(t, []managedMetadataRelease{
		{
			metadataPath:    "/providers/roadmap-plugin/v" + oldVersion + "/provider-release.yaml",
			archiveURLPath:  "/providers/roadmap-plugin/v" + oldVersion + "/roadmap-app.tar.gz",
			archiveFilePath: oldPluginPkg,
			packageSource:   pluginRef,
			version:         oldVersion,
			kind:            providermanifestv1.KindApp,
		},
		{
			metadataPath:    "/providers/roadmap-plugin/v" + newVersion + "/provider-release.yaml",
			archiveURLPath:  "/providers/roadmap-plugin/v" + newVersion + "/roadmap-app.tar.gz",
			archiveFilePath: newPluginPkg,
			packageSource:   pluginRef,
			version:         newVersion,
			kind:            providermanifestv1.KindApp,
		},
	})
	defer srv.Close()

	cfgPath := filepath.Join(dir, "config.yaml")
	writeConfig := func(version string) {
		cfg := requiredComponentConfigWithAPIVersionYAML(t, dir, filepath.Join(dir, "gestalt.db")) + `apps:
  roadmap:
    source: ` + srv.URL + `/providers/roadmap-plugin/v` + version + `/provider-release.yaml
server:
` + requiredServerIndexedDBYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
		if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
			t.Fatalf("WriteFile config: %v", err)
		}
	}
	writeConfig(oldVersion)

	lc := NewLifecycle()
	lockfilePath, artifactsDir = lockAndArtifactsForConfig(cfgPath)
	initialLock, err := lc.PrepareAtPaths([]string{cfgPath}, lockfilePath, artifactsDir)
	if err != nil {
		t.Fatalf("PrepareAtPath: %v", err)
	}

	writeConfig(newVersion)
	loadedCfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	paths := lifecyclePathsForConfig(cfgPath)
	lock := normalizeLockfile(initialLock)
	lock.Providers.App = map[string]LockEntry{
		"roadmap": {
			InputDigest: mustFingerprint(t, "roadmap", loadedCfg.Apps["roadmap"], paths.configDir),
			Source:      srv.URL + "/providers/roadmap-plugin/v" + newVersion + "/provider-release.yaml",
			Package:     pluginRef,
			Kind:        providermanifestv1.KindApp, Version: newVersion,
			Archives: map[string]LockArchive{
				"generic": {URL: archiveServer.URL, SHA256: hex.EncodeToString(newPluginArchiveSum[:])},
			},
		},
	}
	if err := WriteLockfile(filepath.Join(dir, LockfileName), lock); err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}

	if err := lc.SyncAtPathsOptions([]string{cfgPath}, lockfilePath, artifactsDir, SyncOptions{}); err != nil {
		t.Fatalf("SyncAtPaths: %v", err)
	}
	loaded, _, err := lc.LoadForExecutionAtPaths([]string{cfgPath}, lockfilePath, artifactsDir, true, false)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
	}
	if loaded.Apps["roadmap"] == nil || loaded.Apps["roadmap"].ResolvedManifest == nil {
		t.Fatalf("ResolvedManifest = %+v", loaded.Apps["roadmap"])
		return
	}
	if got := loaded.Apps["roadmap"].ResolvedManifest.Version; got != newVersion {
		t.Fatalf("ResolvedManifest.Version = %q, want %q", got, newVersion)
	}
	if !loaded.Apps["roadmap"].ResolvedManifest.IsDeclarativeOnlyProvider() {
		t.Fatalf("ResolvedManifest = %+v, want declarative-only provider", loaded.Apps["roadmap"].ResolvedManifest)
	}
	if loaded.Apps["roadmap"].Command != "" {
		t.Fatalf("loaded app command = %q, want declarative app without executable", loaded.Apps["roadmap"].Command)
	}
}

func TestLoadForExecutionAtPath_UsesDerivedPreparedPathsWhenLockPathsAreStale(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	var lockfilePath, artifactsDir string
	const version = "0.0.1-alpha.1"
	pluginManifestPath := writeLocalExecutablePlugin(t, dir, "example", "ping")
	indexedDBManifestPath := writeStubIndexedDBManifest(t, dir)
	uiManifestPath := writeLocalUIManifest(t, dir, "roadmap-ui", "github.com/testowner/web/roadmap", version, &providermanifestv1.Spec{
		AssetRoot: "dist",
	}, map[string]string{
		"dist/index.html": "<html>roadmap</html>",
	})

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := fmt.Sprintf(`apiVersion: %s
providers:
  indexeddb:
    main:
      source:
        path: %q
      config:
        path: %q
  ui:
    roadmap:
      source:
        path: %q
      path: /roadmap
apps:
  example:
    source:
      path: %q
server:
  providers:
    indexeddb: main
  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`, config.ConfigAPIVersion, indexedDBManifestPath, filepath.Join(dir, "gestalt.db"), uiManifestPath, pluginManifestPath)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lc := NewLifecycle()
	lockfilePath, artifactsDir = lockAndArtifactsForConfig(cfgPath)
	lock, err := lc.PrepareAtPaths([]string{cfgPath}, lockfilePath, artifactsDir)
	if err != nil {
		t.Fatalf("PrepareAtPath: %v", err)
	}

	lock.Providers.App["example"] = LockEntry{
		InputDigest:      lock.Providers.App["example"].InputDigest,
		Source:           lock.Providers.App["example"].Source,
		Version:          lock.Providers.App["example"].Version,
		Archives:         lock.Providers.App["example"].Archives,
		ArtifactManifest: "stale/provider/manifest.json",
		Executable:       "stale/provider/executable",
	}
	indexedDBEntry := lock.Providers.IndexedDB["main"]
	indexedDBEntry.ArtifactManifest = "stale/indexeddb/manifest.json"
	indexedDBEntry.Executable = "stale/indexeddb/executable"
	lock.Providers.IndexedDB["main"] = indexedDBEntry
	uiEntry := lock.Providers.UI["roadmap"]
	uiEntry.ArtifactManifest = "stale/ui/manifest.json"
	uiEntry.AssetRoot = "stale/ui/assets"
	lock.Providers.UI["roadmap"] = uiEntry
	lockPath := filepath.Join(dir, LockfileName)
	if err := WriteLockfile(lockPath, lock); err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}

	for _, locked := range []bool{false, true} {
		loaded, _, err := lc.LoadForExecutionAtPaths([]string{cfgPath}, lockfilePath, artifactsDir, locked, false)
		if err != nil {
			t.Fatalf("LoadForExecutionAtPath(locked=%t): %v", locked, err)
		}

		app := loaded.Apps["example"]
		if app == nil || app.ResolvedManifest == nil {
			t.Fatalf("Apps[example] = %+v", app)
			return
		}
		if got := app.Command; strings.Contains(got, "stale/provider/executable") {
			t.Fatalf("app.Command = %q, want derived prepared path", got)
		}

		indexedDB := mustSelectedHostProviderEntry(t, loaded, config.HostProviderKindIndexedDB)
		if indexedDB == nil || indexedDB.ResolvedManifest == nil {
			t.Fatalf("SelectedHostProvider(indexeddb) = %+v", indexedDB)
			return
		}
		if got := indexedDB.Command; strings.Contains(got, "stale/indexeddb/executable") {
			t.Fatalf("idb.Command = %q, want derived prepared path", got)
		}

		ui := loaded.Providers.UI["roadmap"]
		if ui == nil || ui.ResolvedManifest == nil {
			t.Fatalf("Providers.UI[roadmap] = %+v", ui)
			return
		}
		if got := ui.ResolvedAssetRoot; strings.Contains(got, "stale/ui/assets") {
			t.Fatalf("ResolvedAssetRoot = %q, want derived prepared path", got)
		}
	}

	rewrittenData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(rewrittenData), "stale/provider/manifest.json") || strings.Contains(string(rewrittenData), "stale/ui/assets") {
		t.Fatalf("portable lockfile should not persist stale prepared paths: %s", rewrittenData)
	}
}

func TestPrepareAtPath_RejectsManagedPluginOwnedUIPathOutsidePackage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	const pluginRef = "github.com/testowner/apps/roadmap"
	const version = "0.0.1-alpha.1"

	pkgDir := filepath.Join(dir, "roadmap-managed-pkg")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("MkdirAll package dir: %v", err)
	}
	artifactPath := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "app"))
	artifactContent := []byte("plugin-binary")
	artifactFullPath := filepath.Join(pkgDir, filepath.FromSlash(artifactPath))
	if err := os.MkdirAll(filepath.Dir(artifactFullPath), 0o755); err != nil {
		t.Fatalf("MkdirAll artifact dir: %v", err)
	}
	if err := os.WriteFile(artifactFullPath, artifactContent, 0o755); err != nil {
		t.Fatalf("WriteFile artifact: %v", err)
	}
	sum := sha256.Sum256(artifactContent)
	manifestBytes, err := json.Marshal(&providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      pluginRef,
		Version:     version,
		DisplayName: "Roadmap Review",
		Entrypoint: &providermanifestv1.Entrypoint{
			ArtifactPath: artifactPath,
		},
		Artifacts: []providermanifestv1.Artifact{{
			OS:     runtime.GOOS,
			Arch:   runtime.GOARCH,
			Path:   artifactPath,
			SHA256: hex.EncodeToString(sum[:]),
		}},
		Spec: withNoAuthDefaultConnection(&providermanifestv1.Spec{
			UI: &providermanifestv1.OwnedUI{
				Path: "../owned-ui/manifest.json",
			},
		}),
	})
	if err != nil {
		t.Fatalf("Marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, providerpkg.ManifestFile), manifestBytes, 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "catalog.yaml"), []byte("name: roadmap\noperations:\n  - id: ping\n    method: GET\n"), 0o644); err != nil {
		t.Fatalf("WriteFile catalog: %v", err)
	}
	outsideOwnedUIRoot := filepath.Join(dir, "owned-ui")
	if err := os.MkdirAll(filepath.Join(outsideOwnedUIRoot, "dist"), 0o755); err != nil {
		t.Fatalf("MkdirAll outside owned UI dist: %v", err)
	}
	outsideOwnedUIManifest, err := providerpkg.EncodeManifest(&providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindUI,
		Source:      "github.com/testowner/web/outside-roadmap",
		Version:     version,
		DisplayName: "Outside Roadmap UI",
		Spec:        &providermanifestv1.Spec{AssetRoot: "dist"},
	})
	if err != nil {
		t.Fatalf("Encode outside owned UI manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outsideOwnedUIRoot, providerpkg.ManifestFile), outsideOwnedUIManifest, 0o644); err != nil {
		t.Fatalf("WriteFile outside owned UI manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outsideOwnedUIRoot, "dist", "index.html"), []byte("<html>outside roadmap</html>"), 0o644); err != nil {
		t.Fatalf("WriteFile outside owned UI index: %v", err)
	}
	pkgPath := filepath.Join(dir, "roadmap-managed-pkg.tar.gz")
	mustCreateLifecycleArchive(t, pkgPath,
		lifecycleArchiveFile{name: providerpkg.ManifestFile, data: manifestBytes, mode: 0o644},
		lifecycleArchiveFile{name: "catalog.yaml", data: []byte("name: roadmap\noperations:\n  - id: ping\n    method: GET\n"), mode: 0o644},
		lifecycleArchiveFile{name: artifactPath, data: artifactContent, mode: 0o755},
	)
	srv := newManagedMetadataServer(t, []managedMetadataRelease{{
		metadataPath:    "/providers/roadmap-plugin/v" + version + "/provider-release.yaml",
		archiveURLPath:  "/providers/roadmap-plugin/v" + version + "/roadmap-app.tar.gz",
		archiveFilePath: pkgPath,
		packageSource:   pluginRef,
		version:         version,
		kind:            providermanifestv1.KindApp,
		allowInvalid:    true,
	}})
	defer srv.Close()

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := requiredComponentConfigWithAPIVersionYAML(t, dir, filepath.Join(dir, "gestalt.db")) + `apps:
  roadmap:
    source: ` + srv.URL + `/providers/roadmap-plugin/v` + version + `/provider-release.yaml
    ui:
      path: /create-customer-roadmap-review
server:
` + requiredServerIndexedDBYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lc := NewLifecycle()
	if _, err := prepareAtPathInTest(t, lc, cfgPath); err == nil || !strings.Contains(err.Error(), "spec.ui.path must stay within the package") {
		t.Fatalf("PrepareAtPath error = %v, want substring %q", err, "spec.ui.path must stay within the package")
	}
}

func TestPrepareAtPath_RejectsPolicyBoundManagedMountedUIWithoutExplicitRouteCoverage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		spec *providermanifestv1.Spec
		want string
	}{
		{
			name: "missing routes",
			spec: &providermanifestv1.Spec{AssetRoot: "dist"},
			want: "must declare at least one route",
		},
		{
			name: "missing root coverage",
			spec: &providermanifestv1.Spec{
				AssetRoot: "dist",
				Routes: []providermanifestv1.UIRoute{
					{Path: "/reports", AllowedRoles: []string{"admin"}},
				},
			},
			want: "must declare a route covering /",
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			pkgPath := mustBuildManagedProviderPackage(t, dir, &providermanifestv1.Manifest{
				Kind:        providermanifestv1.KindUI,
				Source:      "github.com/testowner/web/sample-portal",
				Version:     "0.0.1-alpha.1",
				DisplayName: "Sample Portal",
				Spec:        tc.spec,
			}, map[string]string{
				"dist/index.html": "<html>sample portal</html>",
			}, false)
			srv := newManagedMetadataServer(t, []managedMetadataRelease{{
				metadataPath:    "/providers/sample-portal/v0.0.1-alpha.1/provider-release.yaml",
				archiveURLPath:  "/providers/sample-portal/v0.0.1-alpha.1/sample-portal.tar.gz",
				archiveFilePath: pkgPath,
				packageSource:   "github.com/testowner/web/sample-portal",
				version:         "0.0.1-alpha.1",
				kind:            providermanifestv1.KindUI,
				allowInvalid:    true,
			}})
			defer srv.Close()

			cfgPath := filepath.Join(dir, "config.yaml")
			cfg := requiredComponentConfigWithAPIVersionYAML(t, dir, filepath.Join(dir, "gestalt.db")) + `  ui:
    sample_portal:
      source: ` + srv.URL + `/providers/sample-portal/v0.0.1-alpha.1/provider-release.yaml
      path: /sample-portal
      authorizationPolicy: sample_policy
` + `server:
` + requiredServerIndexedDBYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
			if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
				t.Fatalf("WriteFile config: %v", err)
			}

			lc := NewLifecycle()
			_, err := prepareAtPathInTest(t, lc, cfgPath)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("PrepareAtPath error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestPrepareAtPath_RejectsMetadataPackageManifestKindMismatch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	const source = "github.com/testowner/gestalt-providers/app/auth-only"
	const version = "0.0.1-alpha.1"
	pkgPath := mustBuildManagedProviderPackage(t, dir, &providermanifestv1.Manifest{
		Kind:       providermanifestv1.KindIdentity,
		Source:     source,
		Version:    version,
		Spec:       &providermanifestv1.Spec{},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "auth"))},
	}, map[string]string{
		filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "auth")): "auth-binary",
	}, false)

	srv := newManagedMetadataServer(t, []managedMetadataRelease{{
		metadataPath:    "/providers/auth-only/v" + version + "/provider-release.yaml",
		archiveURLPath:  "/providers/auth-only/v" + version + "/auth-only.tar.gz",
		archiveFilePath: pkgPath,
		packageSource:   source,
		version:         version,
		kind:            providermanifestv1.KindApp,
		allowInvalid:    true,
	}})
	defer srv.Close()
	lc := NewLifecycle().WithHTTPClient(srv.Client())

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := requiredComponentConfigWithAPIVersionYAML(t, dir, filepath.Join(dir, "gestalt.db")) + `apps:
    example:
      source: ` + srv.URL + `/providers/auth-only/v` + version + `/provider-release.yaml
server:
` + requiredServerIndexedDBYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	_, err := prepareAtPathInTest(t, lc, cfgPath)
	if err == nil {
		t.Fatal("expected provider kind validation error")
		return
	}
	if !strings.Contains(err.Error(), `app "example" manifest has kind "identity", want "app"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadForExecutionAtPath_ResolvesLocalTopLevelPluginsWithoutLockfile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lockfilePath, artifactsDir := configDirPaths(dir)
	authManifestPath := filepath.Join(dir, "auth-manifest.yaml")
	buildOutput := ".gestaltd/bin/local-auth"
	buildScript := fmt.Sprintf("mkdir -p .gestaltd/bin\nprintf 'auth-binary' > %s\nchmod +x %s\n", buildOutput, buildOutput)
	if err := os.WriteFile(filepath.Join(dir, "build.sh"), []byte(buildScript), 0o755); err != nil {
		t.Fatalf("WriteFile build.sh: %v", err)
	}
	authManifest, err := encodeSourceManifestForTest(&providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindIdentity,
		Source:  "github.com/testowner/apps/local-auth",
		Version: "0.0.1-alpha.1",
		Spec:    &providermanifestv1.Spec{},
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"build.sh"},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: buildOutput, Args: []string{"serve-auth"}},
	}, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat auth: %v", err)
	}
	if err := os.WriteFile(authManifestPath, authManifest, 0o644); err != nil {
		t.Fatalf("WriteFile auth manifest: %v", err)
	}

	dbPath := filepath.Join(dir, "gestalt.db")
	idbManifestPath := writeStubIndexedDBManifest(t, dir)
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := fmt.Sprintf(`apiVersion: %s
providers:
  identity:
    auth:
      source:
        path: ./auth-manifest.yaml
      config:
        clientId: local-auth-client
  indexeddb:
    sqlite:
      source:
        path: %s
      config:
        dsn: %q
server:
  providers:
    identity: auth
    indexeddb: sqlite
  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`, config.ConfigAPIVersion, idbManifestPath, "sqlite://"+dbPath)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lc := NewLifecycle()
	loaded, _, err := lc.LoadForExecutionAtPaths([]string{cfgPath}, lockfilePath, artifactsDir, false, false)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath: %v", err)
	}

	authEntry := mustSelectedHostProviderEntry(t, loaded, config.HostProviderKindIdentity)
	if authEntry == nil || authEntry.ResolvedManifest == nil {
		t.Fatalf("auth resolved manifest = %+v", authEntry)
		return
	}
	if !strings.HasSuffix(filepath.ToSlash(authEntry.Command), filepath.ToSlash(filepath.Join("auth", "auth", "bin", "auth"))) {
		t.Fatalf("auth command = %q", authEntry.Command)
	}
	if got := authEntry.Args; len(got) != 0 {
		t.Fatalf("auth args = %v, want []", got)
	}
	authCfg := decodeNodeMap(t, authEntry.Config)
	if authCfg["command"] != authEntry.Command {
		t.Fatalf("auth config command = %v, want %q", authCfg["command"], authEntry.Command)
	}
	authPluginCfg, ok := authCfg["config"].(map[string]any)
	if !ok || authPluginCfg["clientId"] != "local-auth-client" {
		t.Fatalf("auth nested config = %#v", authCfg["config"])
	}

	if _, err := os.Stat(filepath.Join(dir, LockfileName)); err != nil {
		t.Fatalf("expected lockfile to be created: %v", err)
	}
}

func TestLoadForExecutionAtPath_ResolvesLocalSourceTopLevelPluginsWithoutArtifacts(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTestSourceFile := func(rel string, data []byte, mode os.FileMode) {
		t.Helper()
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", rel, err)
		}
		if err := os.WriteFile(path, data, mode); err != nil {
			t.Fatalf("WriteFile(%s): %v", rel, err)
		}
	}

	writeTestSourceFile("go.mod", []byte(testutil.GeneratedProviderModuleSource(t, "example.com/local-components")), 0o644)
	writeTestSourceFile("go.sum", testutil.GeneratedProviderModuleSum(t), 0o644)

	authManifestPath := filepath.Join(dir, "auth-manifest.yaml")
	writeTestSourceFile("auth.go", []byte(testutil.GeneratedAuthPackageSource()), 0o644)
	authArtifactRel := ".gestaltd/bin/local-source-auth"
	writeOperatorGoComponentBuildFixture(t, dir, "example.com/local-components", providermanifestv1.KindIdentity, authArtifactRel)
	authManifest, err := encodeSourceManifestForTest(&providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindIdentity,
		Source:  "github.com/testowner/apps/local-source-auth",
		Version: "0.0.1-alpha.1",
		Spec:    &providermanifestv1.Spec{},
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"go.mod", "go.sum", "auth.go", "cmd", "build.sh"},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: authArtifactRel},
	}, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat auth: %v", err)
	}
	if err := os.WriteFile(authManifestPath, authManifest, 0o644); err != nil {
		t.Fatalf("WriteFile auth manifest: %v", err)
	}

	dbPath := filepath.Join(dir, "gestalt.db")
	idbManifestPath := writeStubIndexedDBManifest(t, dir)
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := fmt.Sprintf(`apiVersion: %s
providers:
  identity:
    auth:
      source:
        path: ./auth-manifest.yaml
  indexeddb:
    sqlite:
      source:
        path: %s
      config:
        dsn: %q
server:
  providers:
    identity: auth
    indexeddb: sqlite
  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`, config.ConfigAPIVersion, idbManifestPath, "sqlite://"+dbPath)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lockfilePath, artifactsDir := lockAndArtifactsForConfig(cfgPath)
	lc := NewLifecycle()
	loaded, _, err := lc.LoadForExecutionAtPaths([]string{cfgPath}, lockfilePath, artifactsDir, false, false)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath: %v", err)
	}

	authEntry := mustSelectedHostProviderEntry(t, loaded, config.HostProviderKindIdentity)
	if authEntry == nil || authEntry.ResolvedManifest == nil {
		t.Fatalf("auth resolved manifest = %+v", authEntry)
		return
	}
	if authEntry.Command == "" {
		t.Fatal("auth command = empty, want prepared executable path")
	}
	authCfg := decodeNodeMap(t, authEntry.Config)
	manifestPathValue, _ := authCfg["manifestPath"].(string)
	if !strings.HasSuffix(filepath.ToSlash(manifestPathValue), filepath.ToSlash(filepath.Join("auth", "auth", "manifest.yaml"))) {
		t.Fatalf("auth manifest_path = %v", authCfg["manifestPath"])
	}
	if authCfg["command"] == "" {
		t.Fatalf("auth config command = %v, want prepared executable path", authCfg["command"])
	}
	if _, err := os.Stat(filepath.Join(dir, LockfileName)); err != nil {
		t.Fatalf("expected lockfile to be created: %v", err)
	}
}

func TestLoadForExecutionAtPath_GeneratesStaticCatalogForLocalSourceHybridPlugin(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lockfilePath, artifactsDir := configDirPaths(dir)
	writeTestFile := func(rel string, data []byte, mode os.FileMode) {
		t.Helper()
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", rel, err)
		}
		if err := os.WriteFile(path, data, mode); err != nil {
			t.Fatalf("WriteFile(%s): %v", rel, err)
		}
	}

	writeTestFile("go.mod", []byte(testutil.GeneratedProviderModuleSource(t, "example.com/local-generated-provider")), 0o644)
	writeTestFile("go.sum", testutil.GeneratedProviderModuleSum(t), 0o644)
	writeTestFile("provider.go", []byte(testutil.GeneratedProviderPackageSource()), 0o644)
	artifactRel := ".gestaltd/bin/local-generated-provider"
	writeOperatorGoPluginBuildFixture(t, dir, "example.com/local-generated-provider", "example", artifactRel)
	manifest, err := encodeSourceManifestForTest(&providermanifestv1.Manifest{
		Source:      "github.com/testowner/apps/local-generated-provider",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Generated Local Provider",
		Kind:        providermanifestv1.KindApp, Spec: withNoAuthDefaultConnection(&providermanifestv1.Spec{}),
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"go.mod", "go.sum", "provider.go", "cmd", "build.sh"},
		},
		Run: &providermanifestv1.SourceRun{
			Command: []string{"sh", "-c", "sh ./build.sh && ./" + artifactRel},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: artifactRel},
	}, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeManifestFormat: %v", err)
	}
	writeTestFile("manifest.yaml", manifest, 0o644)

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := requiredComponentConfigWithAPIVersionYAML(t, dir, filepath.Join(dir, "gestalt.db")) + `apps:
    example:
      source:
        path: ./manifest.yaml
` + `server:
` + requiredServerIndexedDBYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	writeTestFile("config.yaml", []byte(cfg), 0o644)

	lc := NewLifecycle()
	loaded, _, err := lc.LoadForExecutionAtPaths([]string{cfgPath}, lockfilePath, artifactsDir, false, false)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath: %v", err)
	}

	intg := loaded.Apps["example"]
	if intg == nil || intg.ResolvedManifest == nil {
		t.Fatalf("ResolvedManifest = %+v", intg)
	}
	if !intg.DevActive {
		t.Fatal("expected DevActive for local source-run hybrid app")
	}
	if intg.Command != "" {
		t.Fatalf("Command = %q, want empty for source-run app", intg.Command)
	}
	catalogData, err := os.ReadFile(filepath.Join(dir, "catalog.yaml"))
	if err != nil {
		t.Fatalf("ReadFile(catalog.yaml): %v", err)
	}
	if !strings.Contains(string(catalogData), "generated_op") {
		t.Fatalf("unexpected catalog contents: %s", catalogData)
	}
	if _, err := os.Stat(filepath.Join(dir, LockfileName)); err != nil {
		t.Fatalf("expected lockfile to be created: %v", err)
	}
}

func TestLoadForExecutionAtPath_LockedLocalSourcePluginUsesPreparedArtifactWithoutSource(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lockfilePath, artifactsDir := configDirPaths(dir)
	writeTestFile := func(rel string, data []byte, mode os.FileMode) {
		t.Helper()
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", rel, err)
		}
		if err := os.WriteFile(path, data, mode); err != nil {
			t.Fatalf("WriteFile(%s): %v", rel, err)
		}
	}

	writeTestFile("go.mod", []byte(testutil.GeneratedProviderModuleSource(t, "example.com/local-locked-provider")), 0o644)
	writeTestFile("go.sum", testutil.GeneratedProviderModuleSum(t), 0o644)
	writeTestFile("provider.go", []byte(testutil.GeneratedProviderPackageSource()), 0o644)
	artifactRel := ".gestaltd/bin/local-locked-provider"
	writeOperatorGoPluginBuildFixture(t, dir, "example.com/local-locked-provider", "example", artifactRel)
	manifest, err := encodeSourceManifestForTest(&providermanifestv1.Manifest{
		Source:      "github.com/testowner/apps/local-locked-provider",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Locked Local Provider",
		Kind:        providermanifestv1.KindApp,
		Spec:        withNoAuthDefaultConnection(&providermanifestv1.Spec{}),
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"go.mod", "go.sum", "provider.go", "cmd", "build.sh"},
		},
		Run: &providermanifestv1.SourceRun{
			Command: []string{"sh", "-c", "sh ./build.sh && ./" + artifactRel},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: artifactRel},
	}, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeManifestFormat: %v", err)
	}
	writeTestFile("manifest.yaml", manifest, 0o644)

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := requiredComponentConfigWithAPIVersionYAML(t, dir, filepath.Join(dir, "gestalt.db")) + `apps:
    example:
      source:
        path: ./manifest.yaml
` + `server:
` + requiredServerIndexedDBYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	writeTestFile("config.yaml", []byte(cfg), 0o644)

	lc := NewLifecycle()
	loaded, _, err := lc.LoadForExecutionAtPaths([]string{cfgPath}, lockfilePath, artifactsDir, false, false)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=false): %v", err)
	}
	if !loaded.Apps["example"].DevActive {
		t.Fatal("expected DevActive for local source-run app")
	}

	for _, rel := range []string{"manifest.yaml", "provider.go", "go.mod", "go.sum"} {
		if err := os.Remove(filepath.Join(dir, rel)); err != nil {
			t.Fatalf("Remove(%s): %v", rel, err)
		}
	}

	_, _, err = lc.LoadForExecutionAtPaths([]string{cfgPath}, lockfilePath, artifactsDir, true, true)
	if err == nil || !strings.Contains(err.Error(), `app "example"`) {
		t.Fatalf("LoadForExecutionAtPath(locked=true, noSync=true) without source tree error = %v, want missing app source", err)
	}
}

func TestLoadForExecutionAtPath_GeneratesStaticCatalogForLocalPythonSourcePlugin(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("local Python source app fixture is POSIX-only")
	}

	dir := t.TempDir()
	lockfilePath, artifactsDir := configDirPaths(dir)
	python3Path, err := exec.LookPath("python3")
	if err != nil {
		t.Skipf("python3 not found: %v", err)
	}
	if runtime.GOOS == "darwin" {
		for _, tool := range []string{"arch", "lipo", "install_name_tool"} {
			if _, err := exec.LookPath(tool); err != nil {
				t.Skipf("%s not found: %v", tool, err)
			}
		}
	}
	writeTestFile := func(rel string, data []byte, mode os.FileMode) {
		t.Helper()
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", rel, err)
		}
		if err := os.WriteFile(path, data, mode); err != nil {
			t.Fatalf("WriteFile(%s): %v", rel, err)
		}
	}

	writeTestFile("pyproject.toml", []byte(strings.TrimLeft(`
[build-system]
requires = ["setuptools==82.0.1"]
build-backend = "setuptools.build_meta"

[project]
name = "local-python-provider"
	`, "\n")), 0o644)
	writeTestFile("provider.py", []byte(`from typing import Optional

import gestalt

PREFIX = ""


class BaseInput(gestalt.Model):
    prefix: str = gestalt.field(default="")


class Filters(gestalt.Model):
    owner: str = ""


class Item(gestalt.Model):
    name: str


class EchoInput(BaseInput):
    names: Optional[list[str]] = None
    metadata: Optional[dict[str, str]] = None
    filters: Optional[Filters] = None
    limit: int = 0


def configure(_name: str, config: dict[str, object]) -> None:
    global PREFIX
    PREFIX = str(config.get("prefix", ""))


@gestalt.operation(method="POST")
def echo(input: EchoInput, _req: gestalt.Request) -> dict[str, object]:
    return {
        "configured_prefix": PREFIX,
        "names": input.names or [],
        "metadata": input.metadata or {},
        "filters_type": type(input.filters).__name__ if input.filters else "",
        "owner": input.filters.owner if input.filters else "",
        "limit_type": type(input.limit).__name__,
        "limit": input.limit,
    }


@gestalt.operation(id="times_two", method="POST")
def double(value: int, _req: gestalt.Request) -> dict[str, object]:
    return {
        "value_type": type(value).__name__,
        "value": value * 2,
    }


@gestalt.operation(method="POST")
def explode(_req: gestalt.Request) -> dict[str, object]:
    raise RuntimeError("boom")


@gestalt.operation(method="POST")
def maybe_filters(input: Optional[Filters], _req: gestalt.Request) -> dict[str, object]:
    return {
        "filters_type": type(input).__name__ if input else "",
        "owner": input.owner if input else "",
    }


@gestalt.operation(method="GET", read_only=True)
def list_items(_req: gestalt.Request) -> dict[str, object]:
    return {
        "items": [Item(name="Ada"), Item(name="Grace")],
        "groups": {"staff": [Item(name="Linus")]},
    }


@gestalt.operation(method="POST")
def status_zero(_req: gestalt.Request) -> gestalt.Response[dict[str, bool]]:
    return gestalt.Response(status=0, body={"ok": True})


@gestalt.session_catalog
def session_catalog(request: gestalt.Request) -> gestalt.Catalog:
    return gestalt.Catalog(
        name="session-source",
        display_name=request.token,
        operations=[
            gestalt.CatalogOperation(
                id="private_search",
                method="POST",
                read_only=True,
            )
        ],
    )
`), 0o644)
	createLocalPythonSDKVenv(t, python3Path, filepath.Join(dir, ".venv"), localPythonSDKPath(t))
	buildOutput := ".gestaltd/bin/local-python-provider"
	buildScript := fmt.Sprintf("mkdir -p .gestaltd/bin\nprintf 'python-provider' > %s\nchmod +x %s\n", buildOutput, buildOutput)
	writeTestFile("build.sh", []byte(buildScript), 0o755)
	writeTestFile(providerpkg.StaticCatalogFile, []byte(`name: local-python-provider
displayName: Local Python Provider
operations:
  - id: echo
    method: POST
    params:
      - name: prefix
        type: string
        default: ''
      - name: names
        type: array
        default: null
      - name: metadata
        type: object
      - name: filters
        type: object
      - name: limit
        type: integer
  - id: times_two
    method: POST
  - id: explode
    method: POST
  - id: maybe_filters
    method: POST
    params:
      - name: owner
        type: string
        default: ''
  - id: list_items
    method: GET
    readOnly: true
  - id: status_zero
    method: POST
`), 0o644)

	manifest, err := encodeSourceManifestForTest(&providermanifestv1.Manifest{
		Source:      "github.com/testowner/apps/local-python-provider",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Generated Local Python Provider",
		Kind:        providermanifestv1.KindApp, Spec: withNoAuthDefaultConnection(&providermanifestv1.Spec{}),
		Run: localAppSourceRunCommand(buildOutput),
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"build.sh"},
		},
	}, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeManifestFormat: %v", err)
	}
	writeTestFile("manifest.yaml", manifest, 0o644)

	cfg := requiredComponentConfigWithAPIVersionYAML(t, dir, filepath.Join(dir, "gestalt.db")) + `apps:
    example:
      source:
        path: ./manifest.yaml
` + `server:
` + requiredServerIndexedDBYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	writeTestFile("config.yaml", []byte(cfg), 0o644)
	writeTestFile("exercise.py", []byte(`import json

import gestalt
import provider

provider.app.configure_provider("example", {"prefix": "Hello"})
result = provider.app.execute("echo", {
    "names": ["Ada", "Grace"],
    "metadata": {"role": "admin"},
    "filters": {"owner": "Ada"},
    "limit": 3,
}, gestalt.Request())
double_result = provider.app.execute("times_two", {
    "value": 3,
}, gestalt.Request())
decode_result = provider.app.execute("times_two", {
    "value": "oops",
}, gestalt.Request())
explode_result = provider.app.execute("explode", {}, gestalt.Request())
zero_result = provider.app.execute("status_zero", {}, gestalt.Request())
maybe_result = provider.app.execute("maybe_filters", {
    "owner": "Grace",
}, gestalt.Request())
list_result = provider.app.execute("list_items", {}, gestalt.Request())
session_catalog = provider.app.catalog_for_request(gestalt.Request(token="secret-token"))
print(json.dumps({
    "status": result.status,
    "body": json.loads(result.body),
    "double_status": double_result.status,
    "double_body": json.loads(double_result.body),
    "decode_status": decode_result.status,
    "decode_body": json.loads(decode_result.body),
    "explode_status": explode_result.status,
    "explode_body": json.loads(explode_result.body),
    "list_status": list_result.status,
    "list_body": json.loads(list_result.body),
    "maybe_status": maybe_result.status,
    "maybe_body": json.loads(maybe_result.body),
    "supports_session_catalog": provider.app.supports_session_catalog(),
    "session_catalog": {
        "name": session_catalog.name if session_catalog else "",
        "display_name": session_catalog.display_name if session_catalog else "",
        "operations": [
            {
                "id": operation.id,
                "read_only": operation.read_only,
            }
            for operation in (session_catalog.operations if session_catalog else [])
        ],
    },
    "zero_status": zero_result.status,
    "zero_body": json.loads(zero_result.body),
}, sort_keys=True))
`), 0o644)

	lc := NewLifecycle()
	loaded, _, err := lc.LoadForExecutionAtPaths([]string{filepath.Join(dir, "config.yaml")}, lockfilePath, artifactsDir, false, false)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath: %v", err)
	}

	intg := loaded.Apps["example"]
	if intg == nil || intg.ResolvedManifest == nil {
		t.Fatalf("ResolvedManifest = %+v", intg)
	}
	catalogData, err := os.ReadFile(filepath.Join(dir, "catalog.yaml"))
	if err != nil {
		t.Fatalf("ReadFile(catalog.yaml): %v", err)
	}
	catalogText := string(catalogData)
	if !strings.Contains(catalogText, "id: echo") {
		t.Fatalf("unexpected catalog contents: %s", catalogData)
	}
	if !strings.Contains(catalogText, "id: times_two") || strings.Contains(catalogText, "id: double") {
		t.Fatalf("catalog did not apply explicit operation id override: %s", catalogData)
	}
	if strings.Contains(catalogText, "\n\n") {
		t.Fatalf("catalog contains unexpected blank lines: %q", catalogText)
	}
	arrayParam := regexp.MustCompile(`(?m)- name: names\n\s+type: array$`)
	if !arrayParam.MatchString(catalogText) {
		t.Fatalf("catalog missing array parameter type: %s", catalogText)
	}
	objectParam := regexp.MustCompile(`(?m)- name: metadata\n\s+type: object$`)
	if !objectParam.MatchString(catalogText) {
		t.Fatalf("catalog missing object parameter type: %s", catalogText)
	}
	namesDefault := regexp.MustCompile(`(?m)- name: names\n\s+type: array\n\s+default: null$`)
	if !namesDefault.MatchString(catalogText) {
		t.Fatalf("catalog missing null default for optional array: %s", catalogText)
	}
	filtersParam := regexp.MustCompile(`(?m)- name: filters\n\s+type: object$`)
	if !filtersParam.MatchString(catalogText) {
		t.Fatalf("catalog missing nested object parameter type: %s", catalogText)
	}
	optionalModelParams := regexp.MustCompile(`(?s)- id: maybe_filters.*?- name: owner\n\s+type: string\n\s+default: ''`)
	if !optionalModelParams.MatchString(catalogText) {
		t.Fatalf("catalog missing parameters for Optional model input: %s", catalogText)
	}
	limitParam := regexp.MustCompile(`(?m)- name: limit\n\s+type: integer$`)
	if !limitParam.MatchString(catalogText) {
		t.Fatalf("catalog missing integer parameter type: %s", catalogText)
	}
	emptyStringDefault := regexp.MustCompile(`(?m)- name: prefix\n\s+type: string\n\s+default: ''$`)
	if !emptyStringDefault.MatchString(catalogText) {
		t.Fatalf("catalog missing empty string default: %s", catalogText)
	}

	command := filepath.Join(dir, ".venv", "bin", "python")
	cmd := exec.Command(command, "exercise.py")
	cmd.Dir = dir
	result, err := cmd.Output()
	if err != nil {
		t.Fatalf("exercise.py: %v\n%s", err, result)
	}

	var body map[string]any
	if err := json.Unmarshal(result, &body); err != nil {
		t.Fatalf("json.Unmarshal(result): %v\nbody: %s", err, result)
	}
	if body["status"] != float64(200) {
		t.Fatalf("status = %v, want 200", body["status"])
	}

	payload, ok := body["body"].(map[string]any)
	if !ok {
		t.Fatalf("body payload = %#v, want object", body["body"])
	}
	if payload["filters_type"] != "Filters" {
		t.Fatalf("filters_type = %v, want Filters", payload["filters_type"])
	}
	if payload["configured_prefix"] != "Hello" {
		t.Fatalf("configured_prefix = %v, want Hello", payload["configured_prefix"])
	}
	if payload["owner"] != "Ada" {
		t.Fatalf("owner = %v, want Ada", payload["owner"])
	}
	if payload["limit_type"] != "int" {
		t.Fatalf("limit_type = %v, want int", payload["limit_type"])
	}
	if payload["limit"] != float64(3) {
		t.Fatalf("limit = %v, want 3", payload["limit"])
	}

	doublePayload, ok := body["double_body"].(map[string]any)
	if !ok {
		t.Fatalf("double payload = %#v, want object", body["double_body"])
	}
	if body["double_status"] != float64(200) {
		t.Fatalf("double_status = %v, want 200", body["double_status"])
	}
	if doublePayload["value_type"] != "int" {
		t.Fatalf("double value_type = %v, want int", doublePayload["value_type"])
	}
	if doublePayload["value"] != float64(6) {
		t.Fatalf("double value = %v, want 6", doublePayload["value"])
	}
	decodePayload, ok := body["decode_body"].(map[string]any)
	if !ok {
		t.Fatalf("decode payload = %#v, want object", body["decode_body"])
	}
	if body["decode_status"] != float64(http.StatusBadRequest) {
		t.Fatalf("decode_status = %v, want %d", body["decode_status"], http.StatusBadRequest)
	}
	decodeError, ok := decodePayload["error"].(string)
	if !ok || !strings.Contains(decodeError, "invalid literal for int()") {
		t.Fatalf("decode error = %#v, want conversion error", decodePayload["error"])
	}
	explodePayload, ok := body["explode_body"].(map[string]any)
	if !ok {
		t.Fatalf("explode payload = %#v, want object", body["explode_body"])
	}
	if body["explode_status"] != float64(http.StatusInternalServerError) {
		t.Fatalf("explode_status = %v, want %d", body["explode_status"], http.StatusInternalServerError)
	}
	if explodePayload["error"] != "internal error" {
		t.Fatalf("explode error = %v, want internal error", explodePayload["error"])
	}
	listPayload, ok := body["list_body"].(map[string]any)
	if !ok {
		t.Fatalf("list payload = %#v, want object", body["list_body"])
	}
	if body["list_status"] != float64(200) {
		t.Fatalf("list_status = %v, want 200", body["list_status"])
	}
	items, ok := listPayload["items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("items = %#v, want 2 items", listPayload["items"])
	}
	firstItem, ok := items[0].(map[string]any)
	if !ok || firstItem["name"] != "Ada" {
		t.Fatalf("first item = %#v, want Ada", items[0])
	}
	groups, ok := listPayload["groups"].(map[string]any)
	if !ok {
		t.Fatalf("groups = %#v, want object", listPayload["groups"])
	}
	staff, ok := groups["staff"].([]any)
	if !ok || len(staff) != 1 {
		t.Fatalf("staff = %#v, want one item", groups["staff"])
	}
	staffItem, ok := staff[0].(map[string]any)
	if !ok || staffItem["name"] != "Linus" {
		t.Fatalf("staff item = %#v, want Linus", staff[0])
	}
	maybePayload, ok := body["maybe_body"].(map[string]any)
	if !ok {
		t.Fatalf("maybe payload = %#v, want object", body["maybe_body"])
	}
	if body["maybe_status"] != float64(200) {
		t.Fatalf("maybe_status = %v, want 200", body["maybe_status"])
	}
	if maybePayload["filters_type"] != "Filters" {
		t.Fatalf("maybe filters_type = %v, want Filters", maybePayload["filters_type"])
	}
	if maybePayload["owner"] != "Grace" {
		t.Fatalf("maybe owner = %v, want Grace", maybePayload["owner"])
	}
	if body["supports_session_catalog"] != true {
		t.Fatalf("supports_session_catalog = %v, want true", body["supports_session_catalog"])
	}
	sessionCatalog, ok := body["session_catalog"].(map[string]any)
	if !ok {
		t.Fatalf("session_catalog = %#v, want object", body["session_catalog"])
	}
	if sessionCatalog["name"] != "session-source" {
		t.Fatalf("session catalog name = %v, want session-source", sessionCatalog["name"])
	}
	if sessionCatalog["display_name"] != "secret-token" {
		t.Fatalf("session catalog display_name = %v, want secret-token", sessionCatalog["display_name"])
	}
	sessionOps, ok := sessionCatalog["operations"].([]any)
	if !ok || len(sessionOps) != 1 {
		t.Fatalf("session catalog operations = %#v, want one item", sessionCatalog["operations"])
	}
	sessionOp, ok := sessionOps[0].(map[string]any)
	if !ok {
		t.Fatalf("session catalog operation = %#v, want object", sessionOps[0])
	}
	if sessionOp["id"] != "private_search" {
		t.Fatalf("session catalog operation id = %v, want private_search", sessionOp["id"])
	}
	if sessionOp["read_only"] != true {
		t.Fatalf("session catalog operation read_only = %v, want true", sessionOp["read_only"])
	}
	if body["zero_status"] != float64(0) {
		t.Fatalf("zero_status = %v, want 0", body["zero_status"])
	}

	if _, err := os.Stat(filepath.Join(dir, LockfileName)); err != nil {
		t.Fatalf("expected lockfile to be created: %v", err)
	}
}

func localPythonSDKPath(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "sdk", "python"))
	if _, err := os.Stat(filepath.Join(path, "pyproject.toml")); err != nil {
		t.Fatalf("Stat(%s): %v", path, err)
	}
	return path
}

func createLocalPythonSDKVenv(t *testing.T, pythonPath, venvPath, sdkPath string) {
	t.Helper()

	createVenv := exec.Command(
		pythonPath,
		"-m",
		"venv",
		venvPath,
	)
	result, err := createVenv.CombinedOutput()
	if err != nil {
		t.Fatalf("create Python test venv: %v\n%s", err, result)
	}

	venvPython := filepath.Join(venvPath, "bin", "python")
	installSDK := exec.Command(
		venvPython,
		"-m",
		"pip",
		"install",
		"--disable-pip-version-check",
		"--quiet",
		sdkPath,
	)
	result, err = installSDK.CombinedOutput()
	if err != nil {
		t.Fatalf("install local Python SDK into test venv: %v\n%s", err, result)
	}
}

func TestPortableStaticValidationManifestRelativizesLocalSurfaces(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.yaml")
	outsidePath := filepath.Join(t.TempDir(), "mcp.json")
	manifest := &providermanifestv1.Manifest{
		Spec: &providermanifestv1.Spec{
			Surfaces: &providermanifestv1.ProviderSurfaces{
				OpenAPI: &providermanifestv1.OpenAPISurface{
					Document: filepath.Join(dir, "openapi.yaml"),
				},
				GraphQL: &providermanifestv1.GraphQLSurface{
					URL: "file://" + filepath.Join(dir, "schema.graphql"),
				},
				MCP: &providermanifestv1.MCPSurface{
					URL: outsidePath,
				},
			},
		},
	}

	const preserveRuntimeFields = false // This test only exercises path relativization for local metadata.
	got, err := portableStaticValidationManifest(manifest, manifestPath, preserveRuntimeFields)
	if err != nil {
		t.Fatalf("portableStaticValidationManifest: %v", err)
	}
	if got == manifest {
		t.Fatal("portableStaticValidationManifest returned original manifest after changing local references")
	}
	if got.Spec.Surfaces.OpenAPI.Document != "openapi.yaml" {
		t.Fatalf("OpenAPI document = %q, want openapi.yaml", got.Spec.Surfaces.OpenAPI.Document)
	}
	if got.Spec.Surfaces.GraphQL.URL != "file://schema.graphql" {
		t.Fatalf("GraphQL URL = %q, want file://schema.graphql", got.Spec.Surfaces.GraphQL.URL)
	}
	if got.Spec.Surfaces.MCP.URL != outsidePath {
		t.Fatalf("MCP URL = %q, want outside path unchanged", got.Spec.Surfaces.MCP.URL)
	}
	if manifest.Spec.Surfaces.OpenAPI.Document == "openapi.yaml" {
		t.Fatal("portableStaticValidationManifest mutated original manifest")
	}
}

func TestPortableStaticValidationManifestProjectsPlatformNeutralRuntimeFields(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.yaml")
	baseManifest := func(artifactPath string, args []string, artifacts []providermanifestv1.Artifact) *providermanifestv1.Manifest {
		return &providermanifestv1.Manifest{
			Kind:      providermanifestv1.KindApp,
			Source:    "github.com/acme/provider",
			Version:   "1.2.3",
			Artifacts: artifacts,
			Entrypoint: &providermanifestv1.Entrypoint{
				ArtifactPath: artifactPath,
				Args:         args,
			},
			Spec: &providermanifestv1.Spec{
				Surfaces: &providermanifestv1.ProviderSurfaces{
					OpenAPI: &providermanifestv1.OpenAPISurface{
						Document: filepath.Join(dir, "openapi.yaml"),
					},
				},
			},
		}
	}

	darwinManifest := baseManifest(
		"artifacts/darwin/arm64/provider",
		[]string{"serve-darwin"},
		[]providermanifestv1.Artifact{{
			OS:     "darwin",
			Arch:   "arm64",
			Path:   "artifacts/darwin/arm64/provider",
			SHA256: "darwin-sha",
		}},
	)
	linuxManifest := baseManifest(
		"artifacts/linux/amd64/provider",
		[]string{"serve-linux"},
		[]providermanifestv1.Artifact{{
			OS:     "linux",
			Arch:   "amd64",
			Path:   "artifacts/linux/amd64/provider",
			SHA256: "linux-sha",
		}},
	)

	const platformNeutral = true // Archive-backed locks compare static metadata across host platforms.
	darwinProjected, err := portableStaticValidationManifest(darwinManifest, manifestPath, platformNeutral)
	if err != nil {
		t.Fatalf("project darwin manifest: %v", err)
	}
	linuxProjected, err := portableStaticValidationManifest(linuxManifest, manifestPath, platformNeutral)
	if err != nil {
		t.Fatalf("project linux manifest: %v", err)
	}
	if len(darwinProjected.Artifacts) != 0 {
		t.Fatalf("projected artifacts = %+v, want nil", darwinProjected.Artifacts)
	}
	if darwinProjected.Entrypoint != nil {
		t.Fatalf("projected entrypoint = %+v, want nil", darwinProjected.Entrypoint)
	}
	if darwinProjected.Spec.Surfaces.OpenAPI.Document != "" {
		t.Fatalf("projected OpenAPI document = %q, want package-local reference stripped", darwinProjected.Spec.Surfaces.OpenAPI.Document)
	}
	darwinJSON, err := json.Marshal(darwinProjected)
	if err != nil {
		t.Fatalf("marshal darwin projection: %v", err)
	}
	linuxJSON, err := json.Marshal(linuxProjected)
	if err != nil {
		t.Fatalf("marshal linux projection: %v", err)
	}
	if !bytes.Equal(darwinJSON, linuxJSON) {
		t.Fatalf("projected manifests differ:\ndarwin: %s\nlinux: %s", darwinJSON, linuxJSON)
	}
	if len(darwinManifest.Artifacts) == 0 {
		t.Fatal("portableStaticValidationManifest mutated source manifest")
	}

	const preserveRuntimeFields = false // Local/source manifests remain runnable and keep platform artifacts.
	localProjected, err := portableStaticValidationManifest(darwinManifest, manifestPath, preserveRuntimeFields)
	if err != nil {
		t.Fatalf("project local manifest: %v", err)
	}
	if len(localProjected.Artifacts) != 1 {
		t.Fatalf("local artifacts = %+v, want preserved runtime artifacts", localProjected.Artifacts)
	}
	if localProjected.Entrypoint == nil || localProjected.Entrypoint.ArtifactPath != "artifacts/darwin/arm64/provider" || len(localProjected.Entrypoint.Args) != 1 {
		t.Fatalf("local entrypoint = %+v, want original entrypoint", localProjected.Entrypoint)
	}

	declarative := &providermanifestv1.Manifest{
		Kind:      providermanifestv1.KindApp,
		Version:   "1.2.3",
		Artifacts: []providermanifestv1.Artifact{{Path: "generic.tar.gz", SHA256: "generic-sha"}},
		Spec:      &providermanifestv1.Spec{},
	}
	declarativeProjected, err := portableStaticValidationManifest(declarative, "", platformNeutral)
	if err != nil {
		t.Fatalf("project declarative manifest: %v", err)
	}
	if declarativeProjected.Entrypoint != nil {
		t.Fatalf("declarative entrypoint = %+v, want nil", declarativeProjected.Entrypoint)
	}
	if len(declarativeProjected.Artifacts) != 0 {
		t.Fatalf("declarative artifacts = %+v, want nil", declarativeProjected.Artifacts)
	}
}

func TestAttachStaticValidationMetadataProjectsPortableArchiveBackedSources(t *testing.T) {
	t.Parallel()

	manifest := func(kind, binary, goos, goarch string) *providermanifestv1.Manifest {
		return &providermanifestv1.Manifest{
			Kind:    kind,
			Version: "1.2.3",
			Artifacts: []providermanifestv1.Artifact{{
				OS:     goos,
				Arch:   goarch,
				Path:   binary,
				SHA256: "sha",
			}},
			Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: binary},
			Spec:       &providermanifestv1.Spec{},
		}
	}
	appManifest := func(binary string) *providermanifestv1.Manifest {
		return manifest(providermanifestv1.KindApp, binary, runtime.GOOS, runtime.GOARCH)
	}
	sessionManifest := &providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindApp,
		Source:  "github.com/acme/provider/session",
		Version: "1.2.3",
		Spec: &providermanifestv1.Spec{
			Surfaces: &providermanifestv1.ProviderSurfaces{
				MCP: &providermanifestv1.MCPSurface{URL: "https://mcp.example.test"},
			},
		},
	}
	gitSnapshotSource := config.NewGitSource(config.GitSourceDef{
		Repo:               "https://github.com/acme/provider.git",
		Ref:                "main",
		Path:               "manifest.yaml",
		ArtifactRepository: "default",
		Materialization:    gitMaterializationSnapshot,
	})
	gitSource := config.NewGitSource(config.GitSourceDef{
		Repo:               "https://github.com/acme/provider.git",
		Ref:                "main",
		Path:               "manifest.yaml",
		ArtifactRepository: "default",
		Materialization:    gitMaterializationSource,
	})
	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"release": {
				Source:           config.NewMetadataSource("https://example.invalid/provider-release.yaml"),
				ResolvedManifest: appManifest("release-provider"),
			},
			"local": {
				Source:           config.ProviderSource{Path: "./local"},
				ResolvedManifest: appManifest("local-provider"),
			},
			"gitSource": {
				Source:           gitSource,
				ResolvedManifest: appManifest("git-source-provider"),
			},
			"gitSnapshot": {
				Source:           gitSnapshotSource,
				ResolvedManifest: appManifest("git-snapshot-provider"),
			},
			"gitSnapshotMissingSourceRef": {
				Source:           gitSnapshotSource,
				ResolvedManifest: appManifest("git-snapshot-missing-source-ref-provider"),
			},
			"gitSnapshotSourceMaterialization": {
				Source:           gitSnapshotSource,
				ResolvedManifest: appManifest("git-snapshot-source-materialization-provider"),
			},
			"gitSnapshotNoArchives": {
				Source:           gitSnapshotSource,
				ResolvedManifest: appManifest("git-snapshot-no-archives-provider"),
			},
			"gitSnapshotWrongSourceRefType": {
				Source:           gitSnapshotSource,
				ResolvedManifest: appManifest("git-snapshot-wrong-source-ref-type-provider"),
			},
			"session": {
				ResolvedManifest: sessionManifest,
			},
		},
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				"agentSnapshot": {
					Source:           gitSnapshotSource,
					ResolvedManifest: manifest(providermanifestv1.KindAgent, "agent-snapshot-provider", runtime.GOOS, runtime.GOARCH),
				},
			},
		},
	}
	gitSnapshotLockRef := gitSourceLockRef(cfg.Apps["gitSnapshot"], "gestalt-ref")
	gitSnapshotSourceMaterializationRef := *gitSnapshotLockRef
	gitSnapshotSourceMaterializationRef.Materialization = gitMaterializationSource
	gitSnapshotWrongTypeRef := *gitSnapshotLockRef
	gitSnapshotWrongTypeRef.Type = "metadata"
	archives := map[string]LockArchive{
		"darwin/arm64": {URL: "https://example.invalid/darwin-arm64.tar.gz", SHA256: "darwin-sha"},
		"linux/amd64":  {URL: "https://example.invalid/linux-amd64.tar.gz", SHA256: "linux-sha"},
	}
	lock := &Lockfile{
		Providers: providerLockBuckets{
			App: map[string]LockEntry{
				"release":                          {},
				"local":                            {ArtifactManifest: "local/manifest.yaml"},
				"gitSource":                        {},
				"gitSnapshot":                      {SourceRef: gitSnapshotLockRef, Archives: archives},
				"gitSnapshotMissingSourceRef":      {Archives: archives},
				"gitSnapshotSourceMaterialization": {SourceRef: &gitSnapshotSourceMaterializationRef, Archives: archives},
				"gitSnapshotNoArchives":            {SourceRef: gitSourceLockRef(cfg.Apps["gitSnapshotNoArchives"], "gestalt-ref")},
				"gitSnapshotWrongSourceRefType":    {SourceRef: &gitSnapshotWrongTypeRef, Archives: archives},
				"session":                          {CatalogSessionOnly: true},
			},
			Agent: map[string]LockEntry{
				"agentSnapshot": {SourceRef: gitSourceLockRef(cfg.Providers.Agent["agentSnapshot"], "gestalt-ref"), Archives: archives},
			},
		},
	}

	catalogs := map[string]appservice.EffectiveCatalogResult{
		"local": {
			Catalog: &catalog.Catalog{
				Operations: []catalog.CatalogOperation{{ID: "local_echo"}},
			},
			Available: true,
		},
		"session": {
			Catalog: &catalog.Catalog{
				Operations: []catalog.CatalogOperation{{ID: "should_not_attach"}},
			},
			Available: true,
		},
	}
	if err := attachStaticValidationMetadata(lock, cfg, catalogs); err != nil {
		t.Fatalf("attachStaticValidationMetadata: %v", err)
	}

	for _, name := range []string{"release", "local", "gitSource", "gitSnapshot", "gitSnapshotMissingSourceRef", "gitSnapshotSourceMaterialization", "gitSnapshotNoArchives", "gitSnapshotWrongSourceRefType", "session"} {
		staticManifest := lock.Providers.App[name].ValidationManifest
		if len(staticManifest.Artifacts) != 0 {
			t.Fatalf("%s artifacts = %+v, want nil", name, staticManifest.Artifacts)
		}
		if staticManifest.Entrypoint != nil {
			t.Fatalf("%s entrypoint = %+v, want nil", name, staticManifest.Entrypoint)
		}
	}
	agentManifest := lock.Providers.Agent["agentSnapshot"].ValidationManifest
	if len(agentManifest.Artifacts) != 0 {
		t.Fatalf("agentSnapshot artifacts = %+v, want nil", agentManifest.Artifacts)
	}
	if agentManifest.Entrypoint != nil {
		t.Fatalf("agentSnapshot entrypoint = %+v, want nil", agentManifest.Entrypoint)
	}
	localEntry := lock.Providers.App["local"]
	if !localEntry.CatalogAvailable {
		t.Fatal("local catalogAvailable = false, want true")
	}
	if localEntry.ValidationManifest.Spec == nil || localEntry.ValidationManifest.Spec.AllowedOperations["local_echo"] == nil {
		t.Fatalf("local static manifest spec = %+v, want local_echo operation", localEntry.ValidationManifest.Spec)
	}
	if !cfg.Apps["local"].ResolvedCatalogAvailable || cfg.Apps["local"].ResolvedCatalog == nil {
		t.Fatalf("local resolved catalog available = %v catalog = %+v, want available catalog", cfg.Apps["local"].ResolvedCatalogAvailable, cfg.Apps["local"].ResolvedCatalog)
	}
	sessionEntry := lock.Providers.App["session"]
	if sessionEntry.CatalogAvailable || !sessionEntry.CatalogSessionOnly {
		t.Fatalf("session catalog flags = available:%v sessionOnly:%v, want exclusive session-only", sessionEntry.CatalogAvailable, sessionEntry.CatalogSessionOnly)
	}
	if sessionEntry.ValidationManifest.Spec != nil && sessionEntry.ValidationManifest.Spec.AllowedOperations["should_not_attach"] != nil {
		t.Fatalf("session-only validation manifest unexpectedly included static catalog operation: %+v", sessionEntry.ValidationManifest.Spec.AllowedOperations)
	}
	if cfg.Apps["session"].ResolvedCatalogAvailable || !cfg.Apps["session"].ResolvedCatalogSessionOnly || cfg.Apps["session"].ResolvedCatalog != nil {
		t.Fatalf("session resolved catalog = available:%v sessionOnly:%v catalog:%+v, want exclusive session-only", cfg.Apps["session"].ResolvedCatalogAvailable, cfg.Apps["session"].ResolvedCatalogSessionOnly, cfg.Apps["session"].ResolvedCatalog)
	}
}

func TestArchiveBackedGitSnapshotStaticProjectionAvoidsAgentPlatformDrift(t *testing.T) {
	t.Parallel()

	gitSnapshotSource := config.NewGitSource(config.GitSourceDef{
		Repo:               "https://github.com/acme/provider.git",
		Ref:                "main",
		Path:               "agent/manifest.yaml",
		ArtifactRepository: "default",
		Materialization:    gitMaterializationSnapshot,
	})
	archives := map[string]LockArchive{
		"darwin/arm64": {URL: "https://example.invalid/agent-darwin-arm64.tar.gz", SHA256: "darwin-sha"},
		"linux/amd64":  {URL: "https://example.invalid/agent-linux-amd64.tar.gz", SHA256: "linux-sha"},
	}
	buildLock := func(goos, goarch string) *Lockfile {
		t.Helper()
		cfg := &config.Config{
			Providers: config.ProvidersConfig{
				Agent: map[string]*config.ProviderEntry{
					"deep": {
						Source: gitSnapshotSource,
						ResolvedManifest: &providermanifestv1.Manifest{
							Kind:    providermanifestv1.KindAgent,
							Source:  "github.com/acme/provider/agent",
							Version: "1.2.3",
							Artifacts: []providermanifestv1.Artifact{{
								OS:     goos,
								Arch:   goarch,
								Path:   filepath.ToSlash(filepath.Join("artifacts", goos, goarch, "agent")),
								SHA256: goos + "-" + goarch + "-sha",
							}},
							Entrypoint: &providermanifestv1.Entrypoint{
								ArtifactPath: filepath.ToSlash(filepath.Join("artifacts", goos, goarch, "agent")),
							},
							Spec: &providermanifestv1.Spec{},
						},
					},
				},
			},
		}
		lock := &Lockfile{
			Providers: providerLockBuckets{
				Agent: map[string]LockEntry{
					"deep": {
						InputDigest: "same-input-digest",
						Package:     "github.com/acme/provider/agent",
						Kind:        providermanifestv1.KindAgent, SourceRef: gitSourceLockRef(cfg.Providers.Agent["deep"], "gestalt-ref"),
						Version:  "1.2.3",
						Archives: archives,
					},
				},
			},
		}
		if err := attachStaticValidationMetadata(lock, cfg, nil); err != nil {
			t.Fatalf("attachStaticValidationMetadata: %v", err)
		}
		return lock
	}

	darwinLock := buildLock("darwin", "arm64")
	linuxLock := buildLock("linux", "amd64")
	if drifts := diagnoseLockfileDrift(darwinLock, linuxLock); len(drifts) != 0 {
		t.Fatalf("platform-specific agent manifests produced lock drift: %+v", drifts)
	}

	entry := darwinLock.Providers.Agent["deep"]
	if got := entry.Archives["darwin/arm64"].SHA256; got != "darwin-sha" {
		t.Fatalf("darwin archive SHA256 = %q, want darwin-sha", got)
	}
	if got := entry.Archives["linux/amd64"].SHA256; got != "linux-sha" {
		t.Fatalf("linux archive SHA256 = %q, want linux-sha", got)
	}
	if entry.SourceRef == nil || entry.SourceRef.Type != gitSourceRefType || entry.SourceRef.Materialization != gitMaterializationSnapshot {
		t.Fatalf("sourceRef = %+v, want git snapshot sourceRef", entry.SourceRef)
	}
	if entry.ValidationManifest == nil || len(entry.ValidationManifest.Artifacts) != 0 {
		t.Fatalf("static manifest artifacts = %+v, want nil", entry.ValidationManifest)
	}
	if entry.ValidationManifest.Entrypoint != nil {
		t.Fatalf("static manifest entrypoint = %+v, want nil", entry.ValidationManifest.Entrypoint)
	}

	lockPath := filepath.Join(t.TempDir(), LockfileName)
	if err := WriteLockfile(lockPath, darwinLock); err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}
	readBack, err := ReadLockfile(lockPath)
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	readBackManifest := readBack.Providers.Agent["deep"].ValidationManifest
	if readBackManifest == nil || len(readBackManifest.Artifacts) != 0 {
		t.Fatalf("read-back static manifest artifacts = %+v, want nil", readBackManifest)
	}
	if readBackManifest.Entrypoint != nil {
		t.Fatalf("read-back static manifest entrypoint = %+v, want nil", readBackManifest.Entrypoint)
	}
}

func TestLockFreshForConfig_RemoteS3UsesResourceNameFingerprint(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := &config.Config{
		Providers: config.ProvidersConfig{
			S3: map[string]*config.ProviderEntry{
				"assets": {
					Source: config.NewMetadataSource("https://example.invalid/github-com-testowner-providers-s3/v0.0.1-alpha.1/provider-release.yaml"),
				},
			},
		},
	}
	paths := lifecyclePathsForConfig(cfgPath)
	if err := os.MkdirAll(paths.artifactsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll artifacts: %v", err)
	}
	lockEntry := LockEntry{
		Source:           cfg.Providers.S3["assets"].SourceRemoteLocation(),
		Version:          "0.0.1-alpha.1",
		InputDigest:      mustFingerprint(t, "assets", cfg.Providers.S3["assets"], paths.configDir),
		ArtifactManifest: filepath.ToSlash(filepath.Join("s3", "assets", "manifest.yaml")),
	}
	manifestPath := resolveLockPath(paths.artifactsDir, lockEntry.ArtifactManifest)
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatalf("MkdirAll manifest dir: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte("kind: s3\n"), 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	lock := &Lockfile{
		Providers: providerLockBuckets{
			S3: map[string]LockEntry{
				"assets": lockEntry,
			},
		},
	}
	if !lockFreshForConfig(cfg, paths, lock, lockFreshnessOptions{RequireArtifacts: true}) {
		t.Fatal("lockFreshForConfig returned false for matching remote S3 lock entry")
	}
}

func mustFingerprint(t *testing.T, name string, entry *config.ProviderEntry, configDir string) string {
	t.Helper()
	fingerprint, err := ProviderFingerprint(name, entry, configDir)
	if err != nil {
		t.Fatalf("ProviderFingerprint(%q): %v", name, err)
	}
	return fingerprint
}

func TestProviderFingerprint_Stable(t *testing.T) {
	t.Parallel()

	writeSourceManifest := func(t *testing.T, rootDir, manifestRel, kind string) string {
		t.Helper()

		manifestPath := filepath.Join(rootDir, filepath.FromSlash(manifestRel))
		if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q): %v", filepath.Dir(manifestPath), err)
		}
		manifest := fmt.Sprintf("source: github.com/test-org/fingerprint-test/component\nversion: 0.0.1\nkind: %s\n", kind)
		if kind == providermanifestv1.KindUI {
			assetRoot := filepath.Join(filepath.Dir(manifestPath), "assets")
			if err := os.MkdirAll(assetRoot, 0o755); err != nil {
				t.Fatalf("MkdirAll(%q): %v", assetRoot, err)
			}
			if err := os.WriteFile(filepath.Join(assetRoot, "index.html"), []byte("<html></html>"), 0o644); err != nil {
				t.Fatalf("WriteFile(asset): %v", err)
			}
			manifest += "build:\n  command: [go, version]\nspec:\n  assetRoot: assets\n"
		} else {
			buildScript := filepath.Join(filepath.Dir(manifestPath), "build.sh")
			buildOutput := ".gestaltd/bin/fingerprint-provider"
			if err := os.WriteFile(buildScript, []byte("#!/bin/sh\nmkdir -p .gestaltd/bin\nprintf 'fingerprint-provider' > "+buildOutput+"\nchmod +x "+buildOutput+"\n"), 0o755); err != nil {
				t.Fatalf("WriteFile(build.sh): %v", err)
			}
			manifest += "build:\n  command: [sh, ./build.sh]\n  inputs: [build.sh]\n"
			if kind == providermanifestv1.KindApp {
				manifest += "spec: {}\n"
			}
		}
		if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
			t.Fatalf("WriteFile(%q): %v", manifestPath, err)
		}
		return manifestPath
	}

	t.Run("metadata source", func(t *testing.T) {
		t.Parallel()

		plugin := &config.ProviderEntry{
			Source: config.NewMetadataSource("https://example.invalid/github-com-test-org-test-repo-test-plugin/v1.0.0/provider-release.yaml"),
		}
		first, err := ProviderFingerprint("example", plugin, ".")
		if err != nil {
			t.Fatalf("ProviderFingerprint: %v", err)
		}
		second, err := ProviderFingerprint("example", plugin, ".")
		if err != nil {
			t.Fatalf("ProviderFingerprint: %v", err)
		}
		if first != second {
			t.Fatalf("fingerprint not stable: %q != %q", first, second)
		}
	})

	t.Run("local source path is stable across copied config trees", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		firstConfigDir := filepath.Join(root, "one", "deploy")
		secondConfigDir := filepath.Join(root, "two", "deploy")
		firstManifestPath := writeSourceManifest(t, filepath.Join(root, "one"), "apps/sample/manifest.yaml", providermanifestv1.KindIdentity)
		secondManifestPath := writeSourceManifest(t, filepath.Join(root, "two"), "apps/sample/manifest.yaml", providermanifestv1.KindIdentity)

		firstProvider := &config.ProviderEntry{
			Source: config.ProviderSource{Path: firstManifestPath},
		}
		secondProvider := &config.ProviderEntry{
			Source: config.ProviderSource{Path: secondManifestPath},
		}

		first, err := ProviderFingerprint("example", firstProvider, firstConfigDir)
		if err != nil {
			t.Fatalf("ProviderFingerprint(first): %v", err)
		}
		second, err := ProviderFingerprint("example", secondProvider, secondConfigDir)
		if err != nil {
			t.Fatalf("ProviderFingerprint(second): %v", err)
		}
		if first != second {
			t.Fatalf("local source fingerprint drifted across copied config trees: %q != %q", first, second)
		}
	})

	t.Run("named ui local source path is stable across copied config trees", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		firstConfigDir := filepath.Join(root, "one", "deploy")
		secondConfigDir := filepath.Join(root, "two", "deploy")
		firstManifestPath := writeSourceManifest(t, filepath.Join(root, "one"), "web/dashboard/manifest.yaml", providermanifestv1.KindUI)
		secondManifestPath := writeSourceManifest(t, filepath.Join(root, "two"), "web/dashboard/manifest.yaml", providermanifestv1.KindUI)

		firstProvider := &config.ProviderEntry{
			Source: config.ProviderSource{Path: firstManifestPath},
		}
		secondProvider := &config.ProviderEntry{
			Source: config.ProviderSource{Path: secondManifestPath},
		}

		first, err := NamedUIProviderFingerprint("dashboard", firstProvider, firstConfigDir)
		if err != nil {
			t.Fatalf("NamedUIProviderFingerprint(first): %v", err)
		}
		second, err := NamedUIProviderFingerprint("dashboard", secondProvider, secondConfigDir)
		if err != nil {
			t.Fatalf("NamedUIProviderFingerprint(second): %v", err)
		}
		if first != second {
			t.Fatalf("named ui fingerprint drifted across copied config trees: %q != %q", first, second)
		}
	})

	t.Run("local source content changes still change the fingerprint", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		firstConfigDir := filepath.Join(root, "one", "deploy")
		secondConfigDir := filepath.Join(root, "two", "deploy")
		firstManifestPath := writeSourceManifest(t, filepath.Join(root, "one"), "apps/sample/manifest.yaml", providermanifestv1.KindIdentity)
		secondManifestPath := writeSourceManifest(t, filepath.Join(root, "two"), "apps/sample/manifest.yaml", providermanifestv1.KindIdentity)
		if err := os.WriteFile(secondManifestPath, []byte("source: github.com/test-org/two/component\nversion: 0.0.2\nkind: identity\nbuild:\n  command: [sh, ./build.sh]\n  inputs: [build.sh]\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(%q): %v", secondManifestPath, err)
		}

		firstProvider := &config.ProviderEntry{
			Source: config.ProviderSource{Path: firstManifestPath},
		}
		secondProvider := &config.ProviderEntry{
			Source: config.ProviderSource{Path: secondManifestPath},
		}

		first, err := ProviderFingerprint("example", firstProvider, firstConfigDir)
		if err != nil {
			t.Fatalf("ProviderFingerprint(first): %v", err)
		}
		second, err := ProviderFingerprint("example", secondProvider, secondConfigDir)
		if err != nil {
			t.Fatalf("ProviderFingerprint(second): %v", err)
		}
		if first == second {
			t.Fatalf("local source fingerprint should change when manifest content changes: %q", first)
		}
	})

	t.Run("built source support files change the fingerprint", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		manifestPath := filepath.Join(root, "manifest.yaml")
		if err := os.MkdirAll(filepath.Join(root, "schemas"), 0o755); err != nil {
			t.Fatalf("MkdirAll(schemas): %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "build.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatalf("WriteFile(build.sh): %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "schemas", "config.schema.yaml"), []byte("type: object\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(config.schema.yaml): %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "catalog.yaml"), []byte("name: sample\noperations:\n  - id: echo\n    method: POST\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(catalog.yaml): %v", err)
		}
		manifest := []byte(`source: github.com/test-org/fingerprint-test/component
version: 0.0.1
kind: app
build:
  command: [sh, ./build.sh]
  inputs: [build.sh]
spec:
  configSchemaPath: schemas/config.schema.yaml
`)
		if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
			t.Fatalf("WriteFile(manifest.yaml): %v", err)
		}
		provider := &config.ProviderEntry{Source: config.ProviderSource{Path: manifestPath}}

		first := mustFingerprint(t, "example", provider, root)
		if err := os.WriteFile(filepath.Join(root, "schemas", "config.schema.yaml"), []byte("type: object\nadditionalProperties: false\n"), 0o644); err != nil {
			t.Fatalf("mutate config schema: %v", err)
		}
		second := mustFingerprint(t, "example", provider, root)
		if first == second {
			t.Fatalf("local built-source fingerprint should change when config schema changes: %q", first)
		}
		if err := os.WriteFile(filepath.Join(root, "catalog.yaml"), []byte("name: sample\noperations:\n  - id: status\n    method: GET\n"), 0o644); err != nil {
			t.Fatalf("mutate catalog: %v", err)
		}
		third := mustFingerprint(t, "example", provider, root)
		if second == third {
			t.Fatalf("local built-source fingerprint should change when static catalog changes: %q", second)
		}
	})
}

func TestProviderFingerprint_ChangesWithName(t *testing.T) {
	t.Parallel()

	plugin := &config.ProviderEntry{
		Source: config.NewMetadataSource("https://example.invalid/github-com-test-org-test-repo-test-plugin/v1.0.0/provider-release.yaml"),
	}
	first, err := ProviderFingerprint("alpha", plugin, ".")
	if err != nil {
		t.Fatalf("ProviderFingerprint: %v", err)
	}
	second, err := ProviderFingerprint("beta", plugin, ".")
	if err != nil {
		t.Fatalf("ProviderFingerprint: %v", err)
	}
	if first == second {
		t.Fatal("fingerprint should differ with different name")
	}
}

func mustBuildManagedProviderPackage(t *testing.T, dir string, manifest *providermanifestv1.Manifest, artifacts map[string]string, includeCatalog bool) string {
	t.Helper()

	srcDir := filepath.Join(dir, strings.NewReplacer("/", "-", "@", "-", ".", "_").Replace(manifest.Source+"-"+manifest.Version)+"-pkg")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("MkdirAll source dir: %v", err)
	}

	manifestCopy := *manifest
	manifestCopy.Artifacts = nil
	for artifactPath, content := range artifacts {
		fullPath := filepath.Join(srcDir, filepath.FromSlash(artifactPath))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("MkdirAll artifact dir: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o755); err != nil {
			t.Fatalf("WriteFile artifact: %v", err)
		}
		sum := sha256.Sum256([]byte(content))
		manifestCopy.Artifacts = append(manifestCopy.Artifacts, providermanifestv1.Artifact{
			OS:     runtime.GOOS,
			Arch:   runtime.GOARCH,
			Path:   artifactPath,
			SHA256: hex.EncodeToString(sum[:]),
		})
	}

	manifestBytes, err := providerpkg.EncodeManifest(&manifestCopy)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, providerpkg.ManifestFile), manifestBytes, 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
	if includeCatalog {
		if err := os.WriteFile(filepath.Join(srcDir, "catalog.yaml"), []byte("name: example\noperations:\n  - id: ping\n    method: GET\n"), 0o644); err != nil {
			t.Fatalf("WriteFile catalog: %v", err)
		}
	}

	pkgPath := filepath.Join(dir, filepath.Base(srcDir)+".tar.gz")
	if err := providerpkg.CreatePackageFromDir(srcDir, pkgPath); err != nil {
		t.Fatalf("CreatePackageFromDir: %v", err)
	}
	return pkgPath
}

type lifecycleArchiveFile struct {
	name string
	data []byte
	mode int64
}

func mustCreateLifecycleArchive(t *testing.T, archivePath string, files ...lifecycleArchiveFile) {
	t.Helper()

	out, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("Create(%q): %v", archivePath, err)
	}
	defer func() {
		if err := out.Close(); err != nil {
			t.Fatalf("close archive: %v", err)
		}
	}()

	gzw := gzip.NewWriter(out)
	defer func() {
		if err := gzw.Close(); err != nil {
			t.Fatalf("close gzip: %v", err)
		}
	}()

	tw := tar.NewWriter(gzw)
	defer func() {
		if err := tw.Close(); err != nil {
			t.Fatalf("close tar: %v", err)
		}
	}()

	for _, file := range files {
		hdr := &tar.Header{Name: file.name, Mode: file.mode, Size: int64(len(file.data))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader(%q): %v", file.name, err)
		}
		if _, err := tw.Write(file.data); err != nil {
			t.Fatalf("Write(%q): %v", file.name, err)
		}
	}
}

func TestReadWriteLockfile_RoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lockPath := filepath.Join(dir, LockfileName)
	want := &Lockfile{
		Providers: providerLockBuckets{
			App: map[string]LockEntry{
				"example": {
					InputDigest: "provider-fp",
					Source:      "github.com/test-org/test-repo/test-plugin",
					Version:     "1.0.0",
					Archives: map[string]LockArchive{
						"darwin/arm64": {URL: "https://example.com/example.tar.gz", SHA256: "abc123"},
					},
					ArtifactManifest: "providers/example/manifest.json",
					Executable:       "providers/example/artifacts/darwin/arm64/provider",
				},
			},
			Identity: map[string]LockEntry{
				"oauth": {
					InputDigest: "auth-fp",
					Source:      "github.com/test-org/test-repo/auth-oauth",
					Version:     "1.0.1",
					Archives: map[string]LockArchive{
						"darwin/arm64": {URL: "https://example.com/auth-oauth.tar.gz", SHA256: "auth123"},
					},
					ArtifactManifest: "providers/auth/oauth/manifest.json",
					Executable:       "providers/auth/oauth/artifacts/darwin/arm64/auth-oauth",
				},
			},
			IndexedDB: map[string]LockEntry{
				"main": {
					InputDigest: "indexeddb-main-fp",
					Source:      "github.com/test-org/test-repo/indexeddb-main",
					Version:     "1.1.0",
					Archives: map[string]LockArchive{
						"darwin/arm64": {URL: "https://example.com/indexeddb-main.tar.gz", SHA256: "abc999"},
					},
					ArtifactManifest: "indexeddb/main/manifest.json",
					Executable:       "indexeddb/main/artifacts/darwin/arm64/indexeddb-main",
				},
				"archive": {
					InputDigest: "indexeddb-archive-fp",
					Source:      "github.com/test-org/test-repo/indexeddb-archive",
					Version:     "1.2.0",
					Archives: map[string]LockArchive{
						"darwin/arm64": {URL: "https://example.com/indexeddb-archive.tar.gz", SHA256: "def999"},
					},
					ArtifactManifest: "indexeddb/archive/manifest.json",
					Executable:       "indexeddb/archive/artifacts/darwin/arm64/indexeddb-archive",
				},
			},
			Workflow: map[string]LockEntry{
				"temporal": {
					InputDigest: "workflow-temporal-fp",
					Source:      "github.com/test-org/test-repo/workflow-temporal",
					Version:     "1.3.0",
					Archives: map[string]LockArchive{
						"darwin/arm64": {URL: "https://example.com/workflow-temporal.tar.gz", SHA256: "workflow123"},
					},
					ArtifactManifest: "workflow/temporal/manifest.json",
					Executable:       "workflow/temporal/artifacts/darwin/arm64/workflow-temporal",
				},
			},
			Telemetry: map[string]LockEntry{
				"default": {
					InputDigest: "telemetry-fp",
					Source:      "github.com/test-org/test-repo/telemetry-declarative",
					Kind:        providermanifestv1.KindApp, Version: "1.4.0",
					Archives: map[string]LockArchive{
						"generic": {URL: "https://example.com/telemetry.tar.gz", SHA256: "telemetry123"},
					},
				},
			},
			Audit: map[string]LockEntry{
				"default": {
					InputDigest: "audit-fp",
					Source:      "github.com/test-org/test-repo/audit-declarative",
					Kind:        providermanifestv1.KindApp, Version: "1.5.0",
					Archives: map[string]LockArchive{
						"generic": {URL: "https://example.com/audit.tar.gz", SHA256: "audit123"},
					},
				},
			},
			UI: map[string]LockEntry{
				"roadmap": {
					InputDigest: "ui-fp",
					Source:      "github.com/test-org/test-repo/test-ui",
					Version:     "2.0.0",
					Archives: map[string]LockArchive{
						"generic": {URL: "https://example.com/ui.tar.gz", SHA256: "def456"},
					},
					ArtifactManifest: "ui/roadmap/manifest.json",
					AssetRoot:        "ui/roadmap/assets",
				},
			},
		},
	}
	if err := WriteLockfile(lockPath, want); err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}

	lockData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(lockData), `"schema": "gestaltd-provider-lock"`) {
		t.Fatalf("lockfile = %s, want provider lock schema", lockData)
	}
	if strings.Contains(string(lockData), `"manifest":`) || strings.Contains(string(lockData), `"executable":`) || strings.Contains(string(lockData), `"assetRoot":`) {
		t.Fatalf("lockfile = %s, want portable entries only", lockData)
	}
	var diskLock Lockfile
	if err := json.Unmarshal(lockData, &diskLock); err != nil {
		t.Fatalf("Unmarshal lockfile: %v", err)
	}
	if diskLock.SchemaVersion != providerLockSchemaVersion {
		t.Fatalf("lock schemaVersion = %d, want explicit v%d schema", diskLock.SchemaVersion, providerLockSchemaVersion)
	}
	providerEntry, ok := diskLock.Providers.App["example"]
	if !ok {
		t.Fatal(`disk lock providers.plugin["example"] not found`)
	}
	if providerEntry.InputDigest != want.Providers.App["example"].InputDigest {
		t.Fatalf("provider inputDigest = %q, want %q", providerEntry.InputDigest, want.Providers.App["example"].InputDigest)
	}
	if providerEntry.Package != want.Providers.App["example"].Source {
		t.Fatalf("provider package = %q, want %q", providerEntry.Package, want.Providers.App["example"].Source)
	}
	if providerEntry.Source != "" {
		t.Fatalf("provider source = %q, want omitted portable source", providerEntry.Source)
	}
	if providerEntry.Kind != providermanifestv1.KindApp {
		t.Fatalf("provider kind = %q, want %q", providerEntry.Kind, providermanifestv1.KindApp)
	}
	if providerEntry.Runtime != providerLockRuntimeExecutable {
		t.Fatalf("provider runtime = %q, want %q", providerEntry.Runtime, providerLockRuntimeExecutable)
	}
	authEntry, ok := diskLock.Providers.Identity["oauth"]
	if !ok {
		t.Fatal(`disk lock providers.identity["oauth"] not found`)
	}
	if authEntry.InputDigest != want.Providers.Identity["oauth"].InputDigest {
		t.Fatalf("authentication inputDigest = %q, want %q", authEntry.InputDigest, want.Providers.Identity["oauth"].InputDigest)
	}
	if authEntry.Package != want.Providers.Identity["oauth"].Source {
		t.Fatalf("authentication package = %q, want %q", authEntry.Package, want.Providers.Identity["oauth"].Source)
	}
	if authEntry.Source != "" {
		t.Fatalf("authentication source = %q, want omitted portable source", authEntry.Source)
	}
	if authEntry.Kind != providermanifestv1.KindIdentity {
		t.Fatalf("authentication kind = %q, want %q", authEntry.Kind, providermanifestv1.KindIdentity)
	}
	if authEntry.Runtime != providerLockRuntimeExecutable {
		t.Fatalf("authentication runtime = %q, want %q", authEntry.Runtime, providerLockRuntimeExecutable)
	}
	telemetryEntry, ok := diskLock.Providers.Telemetry["default"]
	if !ok {
		t.Fatal(`disk lock providers.telemetry["default"] not found`)
	}
	if telemetryEntry.Package != want.Providers.Telemetry["default"].Source {
		t.Fatalf("telemetry package = %q, want %q", telemetryEntry.Package, want.Providers.Telemetry["default"].Source)
	}
	if telemetryEntry.Kind != providermanifestv1.KindApp {
		t.Fatalf("telemetry kind = %q, want %q", telemetryEntry.Kind, providermanifestv1.KindApp)
	}
	if telemetryEntry.Runtime != providerLockRuntimeExecutable {
		t.Fatalf("telemetry runtime = %q, want %q", telemetryEntry.Runtime, providerLockRuntimeExecutable)
	}
	auditEntry, ok := diskLock.Providers.Audit["default"]
	if !ok {
		t.Fatal(`disk lock providers.audit["default"] not found`)
	}
	if auditEntry.Package != want.Providers.Audit["default"].Source {
		t.Fatalf("audit package = %q, want %q", auditEntry.Package, want.Providers.Audit["default"].Source)
	}
	if auditEntry.Kind != providermanifestv1.KindApp {
		t.Fatalf("audit kind = %q, want %q", auditEntry.Kind, providermanifestv1.KindApp)
	}
	if auditEntry.Runtime != providerLockRuntimeExecutable {
		t.Fatalf("audit runtime = %q, want %q", auditEntry.Runtime, providerLockRuntimeExecutable)
	}
	uiEntry, ok := diskLock.Providers.UI["roadmap"]
	if !ok {
		t.Fatal(`disk lock providers.ui["roadmap"] not found`)
	}
	if uiEntry.InputDigest != want.Providers.UI["roadmap"].InputDigest {
		t.Fatalf("ui inputDigest = %q, want %q", uiEntry.InputDigest, want.Providers.UI["roadmap"].InputDigest)
	}
	if uiEntry.Source != "" {
		t.Fatalf("ui source = %q, want omitted portable source", uiEntry.Source)
	}
	if uiEntry.Kind != providermanifestv1.KindUI {
		t.Fatalf("ui kind = %q, want %q", uiEntry.Kind, providermanifestv1.KindUI)
	}
	if uiEntry.Runtime != providerLockRuntimeAssets {
		t.Fatalf("ui runtime = %q, want %q", uiEntry.Runtime, providerLockRuntimeAssets)
	}

	got, err := ReadLockfile(lockPath)
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	if got.Providers.App["example"].InputDigest != want.Providers.App["example"].InputDigest {
		t.Fatal("provider fingerprint mismatch")
	}
	if got.Providers.App["example"].Source != want.Providers.App["example"].Source || got.Providers.App["example"].Version != want.Providers.App["example"].Version {
		t.Fatal("provider source mismatch")
	}
	if got.Providers.Identity["oauth"].InputDigest != want.Providers.Identity["oauth"].InputDigest {
		t.Fatal("authentication fingerprint mismatch")
	}
	if got.Providers.IndexedDB["main"].InputDigest != want.Providers.IndexedDB["main"].InputDigest {
		t.Fatal("indexeddb fingerprint mismatch")
	}
	if got.Providers.IndexedDB["archive"].Executable != "" {
		t.Fatal("indexeddb executable should not round-trip from portable lock schema")
	}
	if got.Providers.Workflow["temporal"].Source != want.Providers.Workflow["temporal"].Source || got.Providers.Workflow["temporal"].Version != want.Providers.Workflow["temporal"].Version {
		t.Fatal("workflow lock entry mismatch")
	}
	if got.Providers.Workflow["temporal"].Executable != "" {
		t.Fatal("workflow executable should not round-trip from portable lock schema")
	}
	if got.Providers.Telemetry["default"].Runtime != providerLockRuntimeExecutable {
		t.Fatalf("telemetry runtime = %q, want %q", got.Providers.Telemetry["default"].Runtime, providerLockRuntimeExecutable)
	}
	if got.Providers.Audit["default"].Runtime != providerLockRuntimeExecutable {
		t.Fatalf("audit runtime = %q, want %q", got.Providers.Audit["default"].Runtime, providerLockRuntimeExecutable)
	}
	if got.Providers.UI["roadmap"].Source != want.Providers.UI["roadmap"].Source || got.Providers.UI["roadmap"].Version != want.Providers.UI["roadmap"].Version {
		t.Fatal("ui lock entry mismatch")
	}
	if got.Providers.App["example"].ArtifactManifest != "" || got.Providers.UI["roadmap"].AssetRoot != "" {
		t.Fatal("portable lock schema should not populate local path fields on read")
	}
}
