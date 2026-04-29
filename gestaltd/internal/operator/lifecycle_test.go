package operator

import (
	"archive/tar"
	"cmp"
	"compress/gzip"
	"context"
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
	"sync/atomic"
	"testing"
	"unicode"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/providerpkg"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"gopkg.in/yaml.v3"
)

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

func writeStubIndexedDBManifest(t *testing.T, dir string) string {
	t.Helper()
	providerDir := filepath.Join(dir, "indexeddb-stub")
	manifestPath := filepath.Join(providerDir, "indexeddb-manifest.yaml")
	artifactRel := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "indexeddb"))
	artifactPath := filepath.Join(providerDir, filepath.FromSlash(artifactRel))
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatalf("mkdir indexeddb artifact dir: %v", err)
	}
	if err := os.WriteFile(artifactPath, []byte("stub-indexeddb"), 0o755); err != nil {
		t.Fatalf("write indexeddb artifact: %v", err)
	}
	data, err := providerpkg.EncodeSourceManifestFormat(&providermanifestv1.Manifest{
		Source:  "github.com/test/providers/indexeddb-stub",
		Version: "0.0.1-alpha.1",
		Kind:    providermanifestv1.KindIndexedDB,
		Spec:    &providermanifestv1.Spec{},
		Artifacts: []providermanifestv1.Artifact{{
			OS:   runtime.GOOS,
			Arch: runtime.GOARCH,
			Path: artifactRel,
		}},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: artifactRel},
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

func requiredComponentConfigV3YAML(t *testing.T, dir, dbPath string) string {
	t.Helper()
	return "apiVersion: " + config.APIVersionV3 + "\n" + requiredComponentConfigYAML(t, dir, dbPath)
}

func requiredServerDatastoreYAML() string {
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
	runtime         string
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
		metadataBytes, err := yaml.Marshal(providerReleaseMetadata{
			Schema:        providerReleaseSchemaName,
			SchemaVersion: providerReleaseSchemaVersion,
			Package:       release.packageSource,
			Kind:          release.kind,
			Version:       release.version,
			Runtime:       release.runtime,
			Artifacts: map[string]providerReleaseArtifact{
				providerpkg.CurrentPlatformString(): {
					Path:   filepath.Base(release.archiveURLPath),
					SHA256: hex.EncodeToString(archiveSum[:]),
				},
			},
		})
		if err != nil {
			t.Fatalf("yaml.Marshal(metadata %s): %v", release.metadataPath, err)
		}
		routes[release.metadataPath] = response{
			contentType: "application/yaml",
			token:       release.token,
			body:        metadataBytes,
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
	manifest, err := providerpkg.EncodeSourceManifestFormat(&providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindUI,
		Source:      source,
		Version:     version,
		DisplayName: testDisplayName(name),
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

func writeLocalExecutablePlugin(t *testing.T, dir, name string, operations ...string) string {
	t.Helper()

	root := filepath.Join(dir, name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", root, err)
	}
	artifactPath := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "plugin"))
	artifactFullPath := filepath.Join(root, filepath.FromSlash(artifactPath))
	if err := os.MkdirAll(filepath.Dir(artifactFullPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(artifactFullPath), err)
	}
	artifactContent := []byte(name + "-binary")
	if err := os.WriteFile(artifactFullPath, artifactContent, 0o755); err != nil {
		t.Fatalf("WriteFile(%s): %v", artifactFullPath, err)
	}
	artifactSum := sha256.Sum256(artifactContent)
	manifestPath := filepath.Join(root, "manifest.yaml")
	manifest, err := providerpkg.EncodeSourceManifestFormat(&providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindPlugin,
		Source:      "github.com/test/plugins/" + name,
		Version:     "0.0.1-alpha.1",
		DisplayName: testDisplayName(name),
		Entrypoint:  &providermanifestv1.Entrypoint{ArtifactPath: artifactPath},
		Artifacts: []providermanifestv1.Artifact{{
			OS:     runtime.GOOS,
			Arch:   runtime.GOARCH,
			Path:   artifactPath,
			SHA256: hex.EncodeToString(artifactSum[:]),
		}},
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

func writeLocalMCPSpecPlugin(t *testing.T, dir, name string) string {
	t.Helper()

	root := filepath.Join(dir, name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", root, err)
	}
	manifestPath := filepath.Join(root, "manifest.yaml")
	manifest, err := providerpkg.EncodeSourceManifestFormat(&providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindPlugin,
		Source:      "github.com/test/plugins/" + name,
		Version:     "0.0.1-alpha.1",
		DisplayName: testDisplayName(name),
		Spec: &providermanifestv1.Spec{
			Surfaces: &providermanifestv1.ProviderSurfaces{
				MCP: &providermanifestv1.MCPSurface{URL: "https://mcp.example.test"},
			},
		},
	}, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat(%s): %v", name, err)
	}
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", manifestPath, err)
	}
	return manifestPath
}

func writeLocalOpenAPIAndMCPSpecPlugin(t *testing.T, dir, name string) string {
	t.Helper()

	root := filepath.Join(dir, name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", root, err)
	}

	openAPIPath := filepath.Join(root, "openapi.yaml")
	openAPIDoc := `openapi: "3.1.0"
info:
  title: Hybrid
  version: "1.0.0"
paths:
  /status:
    get:
      operationId: status
      responses:
        "200":
          description: OK
`
	if err := os.WriteFile(openAPIPath, []byte(openAPIDoc), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", openAPIPath, err)
	}

	manifestPath := filepath.Join(root, "manifest.yaml")
	manifest, err := providerpkg.EncodeSourceManifestFormat(&providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindPlugin,
		Source:      "github.com/test/plugins/" + name,
		Version:     "0.0.1-alpha.1",
		DisplayName: testDisplayName(name),
		Spec: &providermanifestv1.Spec{
			Surfaces: &providermanifestv1.ProviderSurfaces{
				OpenAPI: &providermanifestv1.OpenAPISurface{Document: "openapi.yaml"},
				MCP:     &providermanifestv1.MCPSurface{URL: "https://mcp.example.test"},
			},
		},
	}, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat(%s): %v", name, err)
	}
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", manifestPath, err)
	}
	return manifestPath
}

func writeLocalExecutableOpenAPIPlugin(t *testing.T, dir, name string, staticOperations, openAPIOperations []string) string {
	t.Helper()

	root := filepath.Join(dir, name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", root, err)
	}

	var openAPI strings.Builder
	openAPI.WriteString(`openapi: "3.1.0"
info:
  title: Hybrid
  version: "1.0.0"
paths:
`)
	for _, operation := range openAPIOperations {
		openAPI.WriteString("  /" + operation + ":\n")
		openAPI.WriteString("    get:\n")
		openAPI.WriteString("      operationId: " + operation + "\n")
		openAPI.WriteString("      responses:\n")
		openAPI.WriteString("        \"200\":\n")
		openAPI.WriteString("          description: OK\n")
	}
	openAPIPath := filepath.Join(root, "openapi.yaml")
	if err := os.WriteFile(openAPIPath, []byte(openAPI.String()), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", openAPIPath, err)
	}

	artifactPath := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "plugin"))
	artifactFullPath := filepath.Join(root, filepath.FromSlash(artifactPath))
	if err := os.MkdirAll(filepath.Dir(artifactFullPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(artifactFullPath), err)
	}
	artifactContent := []byte(name + "-binary")
	if err := os.WriteFile(artifactFullPath, artifactContent, 0o755); err != nil {
		t.Fatalf("WriteFile(%s): %v", artifactFullPath, err)
	}
	artifactSum := sha256.Sum256(artifactContent)

	manifestPath := filepath.Join(root, "manifest.yaml")
	manifest, err := providerpkg.EncodeSourceManifestFormat(&providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindPlugin,
		Source:      "github.com/test/plugins/" + name,
		Version:     "0.0.1-alpha.1",
		DisplayName: testDisplayName(name),
		Entrypoint:  &providermanifestv1.Entrypoint{ArtifactPath: artifactPath},
		Artifacts: []providermanifestv1.Artifact{{
			OS:     runtime.GOOS,
			Arch:   runtime.GOARCH,
			Path:   artifactPath,
			SHA256: hex.EncodeToString(artifactSum[:]),
		}},
		Spec: withNoAuthDefaultConnection(&providermanifestv1.Spec{
			Surfaces: &providermanifestv1.ProviderSurfaces{
				OpenAPI: &providermanifestv1.OpenAPISurface{Document: "openapi.yaml"},
			},
		}),
	}, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat(%s): %v", name, err)
	}
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", manifestPath, err)
	}

	var catalogBuilder strings.Builder
	catalogBuilder.WriteString("name: " + name + "\noperations:\n")
	for _, operation := range staticOperations {
		catalogBuilder.WriteString("  - id: " + operation + "\n")
		catalogBuilder.WriteString("    method: GET\n")
	}
	if err := os.WriteFile(filepath.Join(root, "catalog.yaml"), []byte(catalogBuilder.String()), 0o644); err != nil {
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
	manifestPath := filepath.Join(dir, "manifest.yaml")
	artifactRel := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "provider"))
	artifactPath := filepath.Join(dir, filepath.FromSlash(artifactRel))
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatalf("MkdirAll artifact dir: %v", err)
	}
	if err := os.WriteFile(artifactPath, []byte("local-provider"), 0o755); err != nil {
		t.Fatalf("WriteFile artifact: %v", err)
	}
	manifest, err := providerpkg.EncodeSourceManifestFormat(&providermanifestv1.Manifest{
		Source:      "github.com/testowner/plugins/local-provider",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Local Provider",
		Description: "Local executable provider",
		Kind:        providermanifestv1.KindPlugin, Spec: withNoAuthDefaultConnection(&providermanifestv1.Spec{}),
		Artifacts: []providermanifestv1.Artifact{{
			OS:   runtime.GOOS,
			Arch: runtime.GOARCH,
			Path: artifactRel,
		}},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: artifactRel},
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
	cfg := requiredComponentConfigV3YAML(t, dir, filepath.Join(dir, "gestalt.db")) + `plugins:
    example:
      source:
        path: ./manifest.yaml
` + `server:
` + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lc := NewLifecycle()
	loaded, _, err := lc.LoadForExecutionAtPath(cfgPath, false)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath: %v", err)
	}

	intg := loaded.Plugins["example"]
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
	if !strings.HasSuffix(filepath.ToSlash(intg.ResolvedManifestPath), filepath.ToSlash(filepath.Join(".gestaltd", "providers", "example", "manifest.yaml"))) {
		t.Fatalf("ResolvedManifestPath = %q", intg.ResolvedManifestPath)
	}
	if intg.Command == "" {
		t.Fatal("Command = empty, want prepared executable path")
	}
	if _, err := os.Stat(filepath.Join(dir, InitLockfileName)); err != nil {
		t.Fatalf("expected lockfile to be created: %v", err)
	}
}

func TestLoadForExecutionAtPath_ResolvesLocalMCPOAuthManifestPluginWithoutLockfile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.yaml")
	manifest := []byte(`
kind: plugin
source: github.com/testowner/plugins/notion
version: 0.0.1-alpha.1
displayName: Notion
spec:
  surfaces:
    mcp:
      url: https://mcp.notion.com/mcp
      connection: mcp
  connections:
    mcp:
      mode: user
      auth:
        type: mcp_oauth
`)
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := requiredComponentConfigV3YAML(t, dir, filepath.Join(dir, "gestalt.db")) + `plugins:
    notion:
      source:
        path: ./manifest.yaml
` + `server:
` + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lc := NewLifecycle()
	loaded, _, err := lc.LoadForExecutionAtPath(cfgPath, false)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath: %v", err)
	}

	intg := loaded.Plugins["notion"]
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
	if _, err := os.Stat(filepath.Join(dir, InitLockfileName)); err != nil {
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
			manifest := fmt.Sprintf(`kind: plugin
source: github.com/testowner/plugins/example
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
			cfg := requiredComponentConfigV3YAML(t, dir, filepath.Join(dir, "gestalt.db")) + `plugins:
    example:
      source:
        path: ./manifest.yaml
` + `server:
` + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
			if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
				t.Fatalf("WriteFile config: %v", err)
			}

			_, _, err := NewLifecycle().LoadForExecutionAtPath(cfgPath, false)
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

func TestInitAtPath_RejectsInvalidPluginInvokesShape(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "missing plugin",
			body: `plugins:
    caller:
      source:
        path: ./caller/manifest.yaml
      invokes:
        - operation: ping
`,
			want: `plugins.caller.invokes[0].plugin is required`,
		},
		{
			name: "missing operation",
			body: `plugins:
    caller:
      source:
        path: ./caller/manifest.yaml
      invokes:
        - plugin: target
`,
			want: `plugins.caller.invokes[0].operation or .surface is required`,
		},
		{
			name: "duplicate dependency",
			body: `plugins:
    caller:
      source:
        path: ./caller/manifest.yaml
      invokes:
        - plugin: target
          operation: ping
        - plugin: target
          operation: ping
`,
			want: `plugins.caller.invokes[1] duplicates invokes[0]`,
		},
		{
			name: "non plugin provider",
			body: `  cache:
    shared:
      source:
        path: ./cache-manifest.yaml
      invokes:
        - plugin: target
          operation: ping
`,
			want: `providers.cache.shared.invokes is only supported on plugins.*`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			caseDir := t.TempDir()
			cfgPath := filepath.Join(caseDir, "config.yaml")
			cfg := requiredComponentConfigV3YAML(t, caseDir, filepath.Join(caseDir, "gestalt.db")) + tc.body + `server:
` + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
			if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
				t.Fatalf("WriteFile config: %v", err)
			}

			_, err := NewLifecycle().InitAtPath(cfgPath)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("InitAtPath error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestInitAtPath_AllowsInvokesAgainstEffectiveAlias(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	callerManifestPath := writeLocalExecutablePlugin(t, dir, "caller", "invoke")
	targetManifestPath := writeLocalExecutablePlugin(t, dir, "target", "ping")

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := requiredComponentConfigV3YAML(t, dir, filepath.Join(dir, "gestalt.db")) + fmt.Sprintf(`plugins:
    caller:
      source:
        path: %q
      invokes:
        - plugin: target
          operation: renamed_ping
    target:
      source:
        path: %q
      allowedOperations:
        ping:
          alias: renamed_ping
server:
`, callerManifestPath, targetManifestPath) + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	if _, err := NewLifecycle().InitAtPath(cfgPath); err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}
}

func TestInitAtPath_RejectsHybridExecutableStaticOperationByOriginalNameAfterAlias(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	callerManifestPath := writeLocalExecutablePlugin(t, dir, "caller", "invoke")
	targetManifestPath := writeLocalExecutableOpenAPIPlugin(t, dir, "target", []string{"ping"}, []string{"status"})

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := requiredComponentConfigV3YAML(t, dir, filepath.Join(dir, "gestalt.db")) + fmt.Sprintf(`plugins:
    caller:
      source:
        path: %q
      invokes:
        - plugin: target
          operation: ping
    target:
      source:
        path: %q
      allowedOperations:
        ping:
          alias: renamed_ping
        status:
          alias: renamed_status
server:
`, callerManifestPath, targetManifestPath) + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	_, err := NewLifecycle().InitAtPath(cfgPath)
	if err == nil || !strings.Contains(err.Error(), `unknown effective operation "ping" on plugin "target"`) {
		t.Fatalf("InitAtPath error = %v, want unknown operation error", err)
	}
}

func TestInitAtPath_RejectsHybridExecutableDuplicateEffectiveOperation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	targetManifestPath := writeLocalExecutableOpenAPIPlugin(t, dir, "target", []string{"ping"}, []string{"status"})

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := requiredComponentConfigV3YAML(t, dir, filepath.Join(dir, "gestalt.db")) + fmt.Sprintf(`plugins:
    target:
      source:
        path: %q
      allowedOperations:
        status:
          alias: ping
server:
`, targetManifestPath) + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	_, err := NewLifecycle().InitAtPath(cfgPath)
	if err == nil || !strings.Contains(err.Error(), `duplicate operation "ping" across merged catalogs`) {
		t.Fatalf("InitAtPath error = %v, want duplicate operation error", err)
	}
}

func TestInitAtPath_AllowsManagedPluginInvokesOnFirstInit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	const callerRef = "github.com/testowner/plugins/caller"
	const targetRef = "github.com/testowner/plugins/target"
	const version = "0.0.1-alpha.1"

	callerPkg := mustBuildManagedProviderPackage(t, dir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindPlugin,
		Source:      callerRef,
		Version:     version,
		DisplayName: "Caller",
		Entrypoint: &providermanifestv1.Entrypoint{
			ArtifactPath: filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "plugin")),
		},
		Spec: withNoAuthDefaultConnection(&providermanifestv1.Spec{}),
	}, map[string]string{
		filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "plugin")): "caller-binary",
	}, true)

	targetPkg := mustBuildManagedProviderPackage(t, dir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindPlugin,
		Source:      targetRef,
		Version:     version,
		DisplayName: "Target",
		Entrypoint: &providermanifestv1.Entrypoint{
			ArtifactPath: filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "plugin")),
		},
		Spec: withNoAuthDefaultConnection(&providermanifestv1.Spec{}),
	}, map[string]string{
		filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "plugin")): "target-binary",
	}, true)

	srv := newManagedMetadataServer(t, []managedMetadataRelease{
		{
			metadataPath:    "/providers/caller/v" + version + "/provider-release.yaml",
			archiveURLPath:  "/providers/caller/v" + version + "/caller.tar.gz",
			archiveFilePath: callerPkg,
			packageSource:   callerRef,
			version:         version,
			kind:            providermanifestv1.KindPlugin,
			runtime:         providerReleaseRuntimeExecutable,
		},
		{
			metadataPath:    "/providers/target/v" + version + "/provider-release.yaml",
			archiveURLPath:  "/providers/target/v" + version + "/target.tar.gz",
			archiveFilePath: targetPkg,
			packageSource:   targetRef,
			version:         version,
			kind:            providermanifestv1.KindPlugin,
			runtime:         providerReleaseRuntimeExecutable,
		},
	})
	defer srv.Close()

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := requiredComponentConfigV3YAML(t, dir, filepath.Join(dir, "gestalt.db")) + fmt.Sprintf(`plugins:
    caller:
      source: %s/providers/caller/v%s/provider-release.yaml
      invokes:
        - plugin: target
          operation: ping
    target:
      source: %s/providers/target/v%s/provider-release.yaml
server:
`, srv.URL, version, srv.URL, version) + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lc := NewLifecycle()
	lock, err := lc.InitAtPath(cfgPath)
	if err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}
	if lock.Providers["caller"].Executable == "" || lock.Providers["target"].Executable == "" {
		t.Fatalf("prepared plugin executables = %#v", lock.Providers)
	}
}

func TestInitAtPath_RejectsSessionCatalogOnlyInvokesTarget(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	callerManifestPath := writeLocalExecutablePlugin(t, dir, "caller", "invoke")
	targetManifestPath := writeLocalMCPSpecPlugin(t, dir, "target")

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := requiredComponentConfigV3YAML(t, dir, filepath.Join(dir, "gestalt.db")) + fmt.Sprintf(`plugins:
    caller:
      source:
        path: %q
      invokes:
        - plugin: target
          operation: private_search
    target:
      source:
        path: %q
server:
`, callerManifestPath, targetManifestPath) + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	_, err := NewLifecycle().InitAtPath(cfgPath)
	if err == nil || !strings.Contains(err.Error(), `session-catalog-only operation "private_search" on plugin "target"`) {
		t.Fatalf("InitAtPath error = %v, want session catalog invokes error", err)
	}
}

func TestInitAtPath_DoesNotWriteLockfileWhenInvokesValidationFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	callerManifestPath := writeLocalExecutablePlugin(t, dir, "caller", "invoke")
	targetManifestPath := writeLocalExecutablePlugin(t, dir, "target", "ping")

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := requiredComponentConfigV3YAML(t, dir, filepath.Join(dir, "gestalt.db")) + fmt.Sprintf(`plugins:
    caller:
      source:
        path: %q
      invokes:
        - plugin: target
          operation: missing
    target:
      source:
        path: %q
server:
`, callerManifestPath, targetManifestPath) + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	_, err := NewLifecycle().InitAtPath(cfgPath)
	if err == nil || !strings.Contains(err.Error(), `unknown effective operation "missing" on plugin "target"`) {
		t.Fatalf("InitAtPath error = %v, want unknown operation error", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, InitLockfileName)); !os.IsNotExist(statErr) {
		t.Fatalf("lockfile should not be written on invokes validation failure, got stat error %v", statErr)
	}
}

func TestInitAtPath_RejectsHybridMCPTypoAsUnknownOperation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	callerManifestPath := writeLocalExecutablePlugin(t, dir, "caller", "invoke")
	targetManifestPath := writeLocalOpenAPIAndMCPSpecPlugin(t, dir, "target")

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := requiredComponentConfigV3YAML(t, dir, filepath.Join(dir, "gestalt.db")) + fmt.Sprintf(`plugins:
    caller:
      source:
        path: %q
      invokes:
        - plugin: target
          operation: private_search
    target:
      source:
        path: %q
server:
`, callerManifestPath, targetManifestPath) + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	_, err := NewLifecycle().InitAtPath(cfgPath)
	if err == nil || !strings.Contains(err.Error(), `unknown effective operation "private_search" on plugin "target"`) {
		t.Fatalf("InitAtPath error = %v, want unknown operation error", err)
	}
	if strings.Contains(err.Error(), "session-catalog-only operation") {
		t.Fatalf("InitAtPath error = %v, want unknown operation classification", err)
	}
}

func TestLoadForExecutionAtPath_RejectsInvalidPluginInvokesDependency(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	callerManifestPath := writeLocalExecutablePlugin(t, dir, "caller", "invoke")
	targetManifestPath := writeLocalExecutablePlugin(t, dir, "target", "ping")

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := requiredComponentConfigV3YAML(t, dir, filepath.Join(dir, "gestalt.db")) + fmt.Sprintf(`plugins:
    caller:
      source:
        path: %q
      invokes:
        - plugin: target
          operation: missing
    target:
      source:
        path: %q
server:
`, callerManifestPath, targetManifestPath) + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	_, _, err := NewLifecycle().LoadForExecutionAtPath(cfgPath, false)
	if err == nil || !strings.Contains(err.Error(), `unknown effective operation "missing" on plugin "target"`) {
		t.Fatalf("LoadForExecutionAtPath error = %v, want unknown operation error", err)
	}
}

func TestLoadForExecutionAtPath_CachesInvokesTargetCatalogResolution(t *testing.T) {
	t.Parallel()

	var docHits atomic.Int32
	docSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		docHits.Add(1)
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write([]byte(`openapi: "3.1.0"
info:
  title: Remote Target
  version: "1.0.0"
paths:
  /status:
    get:
      operationId: status
      responses:
        "200":
          description: OK
  /ping:
    get:
      operationId: ping
      responses:
        "200":
          description: OK
`))
	}))
	t.Cleanup(docSrv.Close)

	dir := t.TempDir()
	callerManifestPath := writeLocalExecutablePlugin(t, dir, "caller", "invoke")

	targetRoot := filepath.Join(dir, "target")
	if err := os.MkdirAll(targetRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", targetRoot, err)
	}
	targetManifestPath := filepath.Join(targetRoot, "manifest.yaml")
	targetManifest, err := providerpkg.EncodeSourceManifestFormat(&providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindPlugin,
		Source:      "github.com/test/plugins/target",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Target",
		Spec: &providermanifestv1.Spec{
			Surfaces: &providermanifestv1.ProviderSurfaces{
				OpenAPI: &providermanifestv1.OpenAPISurface{Document: docSrv.URL},
			},
		},
	}, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat(target): %v", err)
	}
	if err := os.WriteFile(targetManifestPath, targetManifest, 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", targetManifestPath, err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := requiredComponentConfigV3YAML(t, dir, filepath.Join(dir, "gestalt.db")) + fmt.Sprintf(`plugins:
    caller:
      source:
        path: %q
      invokes:
        - plugin: target
          operation: status
        - plugin: target
          operation: ping
    target:
      source:
        path: %q
server:
`, callerManifestPath, targetManifestPath) + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	if _, _, err := NewLifecycle().LoadForExecutionAtPath(cfgPath, false); err != nil {
		t.Fatalf("LoadForExecutionAtPath: %v", err)
	}
	if got := docHits.Load(); got != 1 {
		t.Fatalf("OpenAPI document hits = %d, want 1", got)
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
			name: "plugin ui object binds explicit ui",
			uiConfigYAML: `  ui:
    roadmap:
      source:
        path: ./ui/manifest.yaml
plugins:
    roadmap:
      source:
        path: ./plugin/manifest.yaml
      ui:
        bundle: roadmap
        path: /create-customer-roadmap-review
      authorizationPolicy: roadmap_policy
`,
			extraYAML: `authorization:
  policies:
    roadmap_policy:
      default: deny
      members:
        - subjectID: user:viewer-user
          role: viewer
`,
			uiKey:      "roadmap",
			wantPath:   "/create-customer-roadmap-review",
			wantPolicy: "roadmap_policy",
		},
		{
			name: "plugin owned ui via plugin ui path",
			uiConfigYAML: `plugins:
    roadmap:
      source:
        path: ./plugin/manifest.yaml
      ui:
        path: /create-customer-roadmap-review
      authorizationPolicy: roadmap_policy
`,
			extraYAML: `authorization:
  policies:
    roadmap_policy:
      default: deny
      members:
        - subjectID: user:viewer-user
          role: viewer
`,
			uiKey:       "roadmap",
			wantPath:    "/create-customer-roadmap-review",
			wantPolicy:  "roadmap_policy",
			ownedUIPath: "../ui/manifest.yaml",
		},
		{
			name: "plugin owned ui via plugin ui path with noncanonical manifest filename",
			uiConfigYAML: `plugins:
    roadmap:
      source:
        path: ./plugin/manifest.yaml
      ui:
        path: /create-customer-roadmap-review
      authorizationPolicy: roadmap_policy
`,
			extraYAML: `authorization:
  policies:
    roadmap_policy:
      default: deny
      members:
        - subjectID: user:viewer-user
          role: viewer
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
plugins:
    roadmap:
      source:
        path: ./plugin/manifest.yaml
      ui:
        path: /create-customer-roadmap-review
      authorizationPolicy: roadmap_policy
`,
			extraYAML: `authorization:
  policies:
    roadmap_policy:
      default: deny
      members:
        - subjectID: user:viewer-user
          role: viewer
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
			uiDir := filepath.Join(dir, "ui")
			if err := os.MkdirAll(filepath.Join(uiDir, "dist"), 0o755); err != nil {
				t.Fatalf("MkdirAll ui dist: %v", err)
			}
			if err := os.WriteFile(filepath.Join(uiDir, "dist", "index.html"), []byte("<html>roadmap</html>"), 0o644); err != nil {
				t.Fatalf("WriteFile index.html: %v", err)
			}
			manifestName := cmp.Or(tc.uiManifest, "manifest.yaml")
			manifestPath := filepath.Join(uiDir, manifestName)
			spec := &providermanifestv1.Spec{AssetRoot: "dist"}
			if tc.wantPolicy != "" {
				spec.Routes = []providermanifestv1.UIRoute{
					{Path: "/", AllowedRoles: []string{"viewer"}},
				}
			}
			manifest, err := providerpkg.EncodeSourceManifestFormat(&providermanifestv1.Manifest{
				Kind:        providermanifestv1.KindUI,
				Source:      "github.com/testowner/web/roadmap",
				Version:     "0.0.1-alpha.1",
				DisplayName: "Roadmap UI",
				Spec:        spec,
			}, providerpkg.ManifestFormatYAML)
			if err != nil {
				t.Fatalf("EncodeManifest: %v", err)
			}
			if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
				t.Fatalf("WriteFile manifest: %v", err)
			}
			if tc.extraYAML != "" || tc.ownedUIPath != "" {
				pluginManifestPath := filepath.Join(dir, "plugin", "manifest.yaml")
				if err := os.MkdirAll(filepath.Dir(pluginManifestPath), 0o755); err != nil {
					t.Fatalf("MkdirAll plugin dir: %v", err)
				}
				pluginArtifactRel := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "plugin"))
				pluginArtifactPath := filepath.Join(dir, "plugin", filepath.FromSlash(pluginArtifactRel))
				if err := os.MkdirAll(filepath.Dir(pluginArtifactPath), 0o755); err != nil {
					t.Fatalf("MkdirAll plugin artifact dir: %v", err)
				}
				if err := os.WriteFile(pluginArtifactPath, []byte("roadmap-plugin"), 0o755); err != nil {
					t.Fatalf("WriteFile plugin artifact: %v", err)
				}
				pluginSpec := withNoAuthDefaultConnection(&providermanifestv1.Spec{})
				if tc.ownedUIPath != "" {
					pluginSpec.UI = &providermanifestv1.OwnedUI{Path: tc.ownedUIPath}
				}
				pluginManifest, err := providerpkg.EncodeSourceManifestFormat(&providermanifestv1.Manifest{
					Source:      "github.com/testowner/plugins/roadmap",
					Version:     "0.0.1-alpha.1",
					DisplayName: "Roadmap Plugin",
					Kind:        providermanifestv1.KindPlugin,
					Spec:        pluginSpec,
					Artifacts: []providermanifestv1.Artifact{{
						OS:   runtime.GOOS,
						Arch: runtime.GOARCH,
						Path: pluginArtifactRel,
					}},
					Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: pluginArtifactRel},
				}, providerpkg.ManifestFormatYAML)
				if err != nil {
					t.Fatalf("EncodePluginManifest: %v", err)
				}
				if err := os.WriteFile(pluginManifestPath, pluginManifest, 0o644); err != nil {
					t.Fatalf("WriteFile plugin manifest: %v", err)
				}
				if err := os.WriteFile(filepath.Join(dir, "plugin", "catalog.yaml"), []byte("name: roadmap\noperations:\n  - id: ping\n    method: GET\n"), 0o644); err != nil {
					t.Fatalf("WriteFile plugin catalog: %v", err)
				}
			}

			cfgPath := filepath.Join(dir, "config.yaml")
			cfg := requiredComponentConfigV3YAML(t, dir, filepath.Join(dir, "gestalt.db")) + tc.uiConfigYAML + tc.extraYAML + `server:
` + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
			if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
				t.Fatalf("WriteFile config: %v", err)
			}

			lc := NewLifecycle()
			loaded, _, err := lc.LoadForExecutionAtPath(cfgPath, false)
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
			wantUIManifest := filepath.ToSlash(filepath.Join(".gestaltd", "ui", tc.uiKey, "manifest.yaml"))
			wantOwnedManifest := filepath.ToSlash(filepath.Join(".gestaltd", "providers", "roadmap", "_owned_ui", "ui", "manifest.yaml"))
			if !strings.HasSuffix(gotManifestPath, wantUIManifest) && !strings.HasSuffix(gotManifestPath, wantOwnedManifest) {
				t.Fatalf("ResolvedManifestPath = %q", gotManifestPath)
			}
			gotAssetRoot := filepath.ToSlash(entry.ResolvedAssetRoot)
			wantUIAssetRoot := filepath.ToSlash(filepath.Join(".gestaltd", "ui", tc.uiKey, "dist"))
			wantOwnedAssetRoot := filepath.ToSlash(filepath.Join(".gestaltd", "providers", "roadmap", "_owned_ui", "ui", "dist"))
			if !strings.HasSuffix(gotAssetRoot, wantUIAssetRoot) && !strings.HasSuffix(gotAssetRoot, wantOwnedAssetRoot) {
				t.Fatalf("ResolvedAssetRoot = %q", gotAssetRoot)
			}
			if got := entry.Path; got != tc.wantPath {
				t.Fatalf("Path = %q, want %q", got, tc.wantPath)
			}
			if got := entry.AuthorizationPolicy; got != tc.wantPolicy {
				t.Fatalf("AuthorizationPolicy = %q, want %q", got, tc.wantPolicy)
			}
			if tc.wantPolicy != "" {
				if got := entry.OwnerPlugin; got != "roadmap" {
					t.Fatalf("OwnerPlugin = %q, want %q", got, "roadmap")
				}
			}
			if tc.wantPolicy != "" {
				plugin := loaded.Plugins["roadmap"]
				if plugin == nil {
					t.Fatal(`Plugins["roadmap"] = nil`)
					return
				}
				if got := plugin.AuthorizationPolicy; got != tc.wantPolicy {
					t.Fatalf("Plugin AuthorizationPolicy = %q, want %q", got, tc.wantPolicy)
				}
			}
			if _, err := os.Stat(filepath.Join(dir, InitLockfileName)); err != nil {
				t.Fatalf("expected lockfile to be created: %v", err)
			}
		})
	}
}

func TestLoadForExecutionAtPath_RejectsLockedExplicitLocalUIWithoutPreparedUILockEntry(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	uiDir := filepath.Join(dir, "ui")
	if err := os.MkdirAll(filepath.Join(uiDir, "dist"), 0o755); err != nil {
		t.Fatalf("MkdirAll ui dist: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uiDir, "dist", "index.html"), []byte("<html>roadmap</html>"), 0o644); err != nil {
		t.Fatalf("WriteFile index.html: %v", err)
	}
	manifest, err := providerpkg.EncodeSourceManifestFormat(&providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindUI,
		Source:      "github.com/testowner/web/roadmap",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Roadmap UI",
		Spec:        &providermanifestv1.Spec{AssetRoot: "dist"},
	}, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uiDir, "manifest.yaml"), manifest, 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := requiredComponentConfigV3YAML(t, dir, filepath.Join(dir, "gestalt.db")) + `  ui:
    roadmap:
      source:
        path: ./ui/manifest.yaml
      path: /create-customer-roadmap-review
server:
` + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lc := NewLifecycle()
	if _, err := lc.InitAtPath(cfgPath); err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}
	lockPath := filepath.Join(dir, InitLockfileName)
	lock, err := ReadLockfile(lockPath)
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	delete(lock.UIs, "roadmap")
	if err := WriteLockfile(lockPath, lock); err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}

	if _, _, err := lc.LoadForExecutionAtPath(cfgPath, true); err == nil || !strings.Contains(err.Error(), `prepared artifact for ui "roadmap" is missing or stale`) {
		t.Fatalf("LoadForExecutionAtPath locked error = %v, want missing prepared artifact", err)
	}
}

func TestLoadForExecutionAtPath_ResolvesManagedPluginOwnedUIFromManagedPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	const pluginRef = "github.com/testowner/plugins/roadmap"
	const version = "0.0.1-alpha.1"

	pkgDir := filepath.Join(dir, "roadmap-plugin-pkg")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("MkdirAll package dir: %v", err)
	}

	artifactPath := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "plugin"))
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
		Kind:        providermanifestv1.KindPlugin,
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
		t.Fatalf("Encode plugin manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, providerpkg.ManifestFile), pluginManifestBytes, 0o644); err != nil {
		t.Fatalf("WriteFile plugin manifest: %v", err)
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
		archiveURLPath:  "/providers/roadmap-plugin/v" + version + "/roadmap-plugin.tar.gz",
		archiveFilePath: pkgPath,
		packageSource:   pluginRef,
		version:         version,
		kind:            providermanifestv1.KindPlugin,
		runtime:         providerReleaseRuntimeExecutable,
	}})
	defer srv.Close()

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := requiredComponentConfigV3YAML(t, dir, filepath.Join(dir, "gestalt.db")) + `plugins:
  roadmap:
    source: ` + srv.URL + `/providers/roadmap-plugin/v` + version + `/provider-release.yaml
    ui:
      path: /create-customer-roadmap-review
` + `server:
` + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lc := NewLifecycle()
	if _, err := lc.InitAtPath(cfgPath); err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}

	lock, err := ReadLockfile(filepath.Join(dir, InitLockfileName))
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	pluginLock := lock.Providers["roadmap"]
	pluginLock.Manifest = ""
	lock.Providers["roadmap"] = pluginLock
	if err := WriteLockfile(filepath.Join(dir, InitLockfileName), lock); err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}

	for _, locked := range []bool{false, true} {
		loaded, _, err := lc.LoadForExecutionAtPath(cfgPath, locked)
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

	rewrittenLock, err := ReadLockfile(filepath.Join(dir, InitLockfileName))
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	if got := rewrittenLock.Providers["roadmap"].Manifest; got != "" {
		t.Fatalf("lock.Providers[roadmap].Manifest = %q, want stale value preserved", got)
	}
	if len(rewrittenLock.UIs) != 0 {
		t.Fatalf("lock.UIs = %#v, want no separate UI entries for in-package owned UI", rewrittenLock.UIs)
	}
}

func TestLoadForExecutionAtPath_ReinitializesManagedPluginWhenGenericArchiveLockIsStale(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	const pluginRef = "github.com/testowner/plugins/roadmap"
	const version = "0.0.1-alpha.1"

	pluginPkg := mustBuildManagedProviderPackage(t, dir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindPlugin,
		Source:      pluginRef,
		Version:     version,
		DisplayName: "Roadmap Review",
		Entrypoint: &providermanifestv1.Entrypoint{
			ArtifactPath: filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "plugin")),
		},
		Spec: withNoAuthDefaultConnection(&providermanifestv1.Spec{}),
	}, map[string]string{
		filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "plugin")): "plugin-binary-" + version,
	}, true)

	srv := newManagedMetadataServer(t, []managedMetadataRelease{{
		metadataPath:    "/providers/roadmap-plugin/v" + version + "/provider-release.yaml",
		archiveURLPath:  "/providers/roadmap-plugin/v" + version + "/roadmap-plugin.tar.gz",
		archiveFilePath: pluginPkg,
		packageSource:   pluginRef,
		version:         version,
		kind:            providermanifestv1.KindPlugin,
		runtime:         providerReleaseRuntimeExecutable,
	}})
	defer srv.Close()

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := requiredComponentConfigV3YAML(t, dir, filepath.Join(dir, "gestalt.db")) + `plugins:
  roadmap:
    source: ` + srv.URL + `/providers/roadmap-plugin/v` + version + `/provider-release.yaml
server:
` + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lc := NewLifecycle()
	if _, err := lc.InitAtPath(cfgPath); err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}

	loadedCfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	paths := initPathsForConfig(cfgPath)
	staleLock := &Lockfile{
		Version: LockVersion,
		Providers: map[string]LockEntry{
			"roadmap": {
				Fingerprint: mustFingerprint(t, "roadmap", loadedCfg.Plugins["roadmap"], paths.configDir),
				Source:      srv.URL + "/providers/roadmap-plugin/v" + version + "/provider-release.yaml",
				Version:     version,
				Archives: map[string]LockArchive{
					"generic": {URL: "https://example.com/roadmap.tar.gz", SHA256: "abc123"},
				},
			},
		},
	}
	if err := WriteLockfile(filepath.Join(dir, InitLockfileName), staleLock); err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}

	loaded, _, err := lc.LoadForExecutionAtPath(cfgPath, false)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath: %v", err)
	}
	if loaded.Plugins["roadmap"] == nil || loaded.Plugins["roadmap"].ResolvedManifest == nil {
		t.Fatalf("ResolvedManifest = %+v", loaded.Plugins["roadmap"])
		return
	}
	if loaded.Plugins["roadmap"].Command == "" {
		t.Fatal("loaded plugin command is empty")
	}

	rewrittenLock, err := ReadLockfile(filepath.Join(dir, InitLockfileName))
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	if _, ok := rewrittenLock.Providers["roadmap"].Archives["generic"]; ok {
		t.Fatalf("rewritten lock still contains generic archive: %#v", rewrittenLock.Providers["roadmap"].Archives)
	}
	if _, ok := rewrittenLock.Providers["roadmap"].Archives[providerpkg.CurrentPlatformString()]; !ok {
		t.Fatalf("rewritten lock missing current platform archive: %#v", rewrittenLock.Providers["roadmap"].Archives)
	}
}

func TestLoadForExecutionAtPath_LockedManagedDeclarativePluginMaterializesBeforeGenericPolicyCheck(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	const pluginRef = "github.com/testowner/plugins/roadmap"
	const oldVersion = "0.0.1-alpha.1"
	const newVersion = "0.0.2-alpha.1"

	buildExecutablePlugin := func(version string) string {
		return mustBuildManagedProviderPackage(t, dir, &providermanifestv1.Manifest{
			Kind:        providermanifestv1.KindPlugin,
			Source:      pluginRef,
			Version:     version,
			DisplayName: "Roadmap Review",
			Entrypoint: &providermanifestv1.Entrypoint{
				ArtifactPath: filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "plugin")),
			},
			Spec: withNoAuthDefaultConnection(&providermanifestv1.Spec{}),
		}, map[string]string{
			filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "plugin")): "plugin-binary-" + version,
		}, true)
	}
	buildDeclarativePlugin := func(version string) string {
		return mustBuildManagedProviderPackage(t, dir, &providermanifestv1.Manifest{
			Kind:        providermanifestv1.KindPlugin,
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

	oldPluginPkg := buildExecutablePlugin(oldVersion)
	newPluginPkg := buildDeclarativePlugin(newVersion)
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
			archiveURLPath:  "/providers/roadmap-plugin/v" + oldVersion + "/roadmap-plugin.tar.gz",
			archiveFilePath: oldPluginPkg,
			packageSource:   pluginRef,
			version:         oldVersion,
			kind:            providermanifestv1.KindPlugin,
			runtime:         providerReleaseRuntimeExecutable,
		},
		{
			metadataPath:    "/providers/roadmap-plugin/v" + newVersion + "/provider-release.yaml",
			archiveURLPath:  "/providers/roadmap-plugin/v" + newVersion + "/roadmap-plugin.tar.gz",
			archiveFilePath: newPluginPkg,
			packageSource:   pluginRef,
			version:         newVersion,
			kind:            providermanifestv1.KindPlugin,
			runtime:         providerReleaseRuntimeDeclarative,
		},
	})
	defer srv.Close()

	cfgPath := filepath.Join(dir, "config.yaml")
	writeConfig := func(version string) {
		cfg := requiredComponentConfigV3YAML(t, dir, filepath.Join(dir, "gestalt.db")) + `plugins:
  roadmap:
    source: ` + srv.URL + `/providers/roadmap-plugin/v` + version + `/provider-release.yaml
server:
` + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
		if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
			t.Fatalf("WriteFile config: %v", err)
		}
	}
	writeConfig(oldVersion)

	lc := NewLifecycle()
	initialLock, err := lc.InitAtPath(cfgPath)
	if err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}

	writeConfig(newVersion)
	loadedCfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	paths := initPathsForConfig(cfgPath)
	lock := normalizeLockfile(initialLock)
	lock.Providers = map[string]LockEntry{
		"roadmap": {
			Fingerprint: mustFingerprint(t, "roadmap", loadedCfg.Plugins["roadmap"], paths.configDir),
			Source:      srv.URL + "/providers/roadmap-plugin/v" + newVersion + "/provider-release.yaml",
			Package:     pluginRef,
			Kind:        providermanifestv1.KindPlugin,
			Runtime:     providerReleaseRuntimeDeclarative,
			Version:     newVersion,
			Archives: map[string]LockArchive{
				"generic": {URL: archiveServer.URL, SHA256: hex.EncodeToString(newPluginArchiveSum[:])},
			},
		},
	}
	if err := WriteLockfile(filepath.Join(dir, InitLockfileName), lock); err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}

	loaded, _, err := lc.LoadForExecutionAtPath(cfgPath, true)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
	}
	if loaded.Plugins["roadmap"] == nil || loaded.Plugins["roadmap"].ResolvedManifest == nil {
		t.Fatalf("ResolvedManifest = %+v", loaded.Plugins["roadmap"])
		return
	}
	if got := loaded.Plugins["roadmap"].ResolvedManifest.Version; got != newVersion {
		t.Fatalf("ResolvedManifest.Version = %q, want %q", got, newVersion)
	}
	if !loaded.Plugins["roadmap"].ResolvedManifest.IsDeclarativeOnlyProvider() {
		t.Fatalf("ResolvedManifest = %+v, want declarative-only provider", loaded.Plugins["roadmap"].ResolvedManifest)
	}
	if loaded.Plugins["roadmap"].Command != "" {
		t.Fatalf("loaded plugin command = %q, want declarative plugin without executable", loaded.Plugins["roadmap"].Command)
	}
}

func TestLoadForExecutionAtPath_UsesDerivedPreparedPathsWhenLockPathsAreStale(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
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
plugins:
  example:
    source:
      path: %q
server:
  providers:
    indexeddb: main
  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`, config.APIVersionV3, indexedDBManifestPath, filepath.Join(dir, "gestalt.db"), uiManifestPath, pluginManifestPath)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lc := NewLifecycle()
	lock, err := lc.InitAtPath(cfgPath)
	if err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}

	lock.Providers["example"] = LockEntry{
		Fingerprint: lock.Providers["example"].Fingerprint,
		Source:      lock.Providers["example"].Source,
		Version:     lock.Providers["example"].Version,
		Archives:    lock.Providers["example"].Archives,
		Manifest:    "stale/provider/manifest.json",
		Executable:  "stale/provider/executable",
	}
	indexedDBEntry := lock.IndexedDBs["main"]
	indexedDBEntry.Manifest = "stale/indexeddb/manifest.json"
	indexedDBEntry.Executable = "stale/indexeddb/executable"
	lock.IndexedDBs["main"] = indexedDBEntry
	uiEntry := lock.UIs["roadmap"]
	uiEntry.Manifest = "stale/ui/manifest.json"
	uiEntry.AssetRoot = "stale/ui/assets"
	lock.UIs["roadmap"] = uiEntry
	lockPath := filepath.Join(dir, InitLockfileName)
	if err := WriteLockfile(lockPath, lock); err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}

	for _, locked := range []bool{false, true} {
		loaded, _, err := lc.LoadForExecutionAtPath(cfgPath, locked)
		if err != nil {
			t.Fatalf("LoadForExecutionAtPath(locked=%t): %v", locked, err)
		}

		plugin := loaded.Plugins["example"]
		if plugin == nil || plugin.ResolvedManifest == nil {
			t.Fatalf("Plugins[example] = %+v", plugin)
			return
		}
		if got := plugin.Command; strings.Contains(got, "stale/provider/executable") {
			t.Fatalf("plugin.Command = %q, want derived prepared path", got)
		}

		indexedDB := mustSelectedHostProviderEntry(t, loaded, config.HostProviderKindIndexedDB)
		if indexedDB == nil || indexedDB.ResolvedManifest == nil {
			t.Fatalf("SelectedHostProvider(indexeddb) = %+v", indexedDB)
			return
		}
		if got := indexedDB.Command; strings.Contains(got, "stale/indexeddb/executable") {
			t.Fatalf("indexeddb.Command = %q, want derived prepared path", got)
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

func TestInitAtPath_RejectsManagedPluginOwnedUIPathOutsidePackage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	const pluginRef = "github.com/testowner/plugins/roadmap"
	const version = "0.0.1-alpha.1"

	pkgDir := filepath.Join(dir, "roadmap-managed-pkg")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("MkdirAll package dir: %v", err)
	}
	artifactPath := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "plugin"))
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
		Kind:        providermanifestv1.KindPlugin,
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
		archiveURLPath:  "/providers/roadmap-plugin/v" + version + "/roadmap-plugin.tar.gz",
		archiveFilePath: pkgPath,
		packageSource:   pluginRef,
		version:         version,
		kind:            providermanifestv1.KindPlugin,
		runtime:         providerReleaseRuntimeExecutable,
	}})
	defer srv.Close()

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := requiredComponentConfigV3YAML(t, dir, filepath.Join(dir, "gestalt.db")) + `plugins:
  roadmap:
    source: ` + srv.URL + `/providers/roadmap-plugin/v` + version + `/provider-release.yaml
    ui:
      path: /create-customer-roadmap-review
server:
` + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lc := NewLifecycle()
	if _, err := lc.InitAtPath(cfgPath); err == nil || !strings.Contains(err.Error(), "spec.ui.path must stay within the package") {
		t.Fatalf("InitAtPath error = %v, want substring %q", err, "spec.ui.path must stay within the package")
	}
}

func TestInitAtPath_RejectsPolicyBoundManagedMountedUIWithoutExplicitRouteCoverage(t *testing.T) {
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
				runtime:         providerReleaseRuntimeUI,
			}})
			defer srv.Close()

			cfgPath := filepath.Join(dir, "config.yaml")
			cfg := requiredComponentConfigV3YAML(t, dir, filepath.Join(dir, "gestalt.db")) + `  ui:
    sample_portal:
      source: ` + srv.URL + `/providers/sample-portal/v0.0.1-alpha.1/provider-release.yaml
      path: /sample-portal
      authorizationPolicy: sample_policy
authorization:
  policies:
    sample_policy:
      default: deny
      members:
        - subjectID: user:viewer-user
          role: viewer
` + `server:
` + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
			if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
				t.Fatalf("WriteFile config: %v", err)
			}

			lc := NewLifecycle()
			_, err := lc.InitAtPath(cfgPath)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("InitAtPath error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestLockEntryForSource_RejectsManifestWithoutProviderKind(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	const source = "github.com/testowner/gestalt-providers/plugins/auth-only"
	const version = "0.0.1-alpha.1"
	pkgPath := mustBuildManagedProviderPackage(t, dir, &providermanifestv1.Manifest{
		Kind:       providermanifestv1.KindAuthentication,
		Source:     source,
		Version:    version,
		Spec:       &providermanifestv1.Spec{},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "auth"))},
	}, map[string]string{
		filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "auth")): "auth-binary",
	}, false)

	cfgPath := filepath.Join(dir, "config.yaml")
	paths := initPathsForConfig(cfgPath)
	srv := newManagedMetadataServer(t, []managedMetadataRelease{{
		metadataPath:    "/providers/auth-only/v" + version + "/provider-release.yaml",
		archiveURLPath:  "/providers/auth-only/v" + version + "/auth-only.tar.gz",
		archiveFilePath: pkgPath,
		packageSource:   source,
		version:         version,
		kind:            providermanifestv1.KindPlugin,
		runtime:         providerReleaseRuntimeExecutable,
	}})
	defer srv.Close()
	lc := NewLifecycle().WithHTTPClient(srv.Client())
	plugin := &config.ProviderEntry{
		Source: config.NewMetadataSource(srv.URL + "/providers/auth-only/v" + version + "/provider-release.yaml"),
	}

	_, err := lc.lockProviderEntryForSource(context.Background(), paths, "example", plugin, map[string]any{})
	if err == nil {
		t.Fatal("expected provider kind validation error")
		return
	}
	if !strings.Contains(err.Error(), `manifest has kind "authentication", want "plugin"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHashPlatformInEntries_HashesMountedUIAndProviderArchives(t *testing.T) {
	t.Parallel()

	archiveBytes := []byte("mounted-ui-archive")
	sum := sha256.Sum256(archiveBytes)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archiveBytes)
	}))
	defer srv.Close()

	lock := &Lockfile{
		Caches: map[string]LockEntry{
			"session": {
				Source: "github.com/testowner/cache/session",
				Archives: map[string]LockArchive{
					providerpkg.CurrentPlatformString(): {URL: srv.URL},
				},
			},
		},
		S3: map[string]LockEntry{
			"assets": {
				Source: "github.com/testowner/providers/s3",
				Archives: map[string]LockArchive{
					providerpkg.CurrentPlatformString(): {URL: srv.URL},
				},
			},
		},
		UIs: map[string]LockEntry{
			"roadmap": {
				Source: "github.com/testowner/web/roadmap",
				Archives: map[string]LockArchive{
					platformKeyGeneric: {URL: srv.URL},
				},
			},
		},
	}

	if err := NewLifecycle().hashPlatformInEntries(context.Background(), lock, initPaths{}, providerpkg.CurrentPlatformString(), map[string]string{}); err != nil {
		t.Fatalf("hashPlatformInEntries: %v", err)
	}

	got := lock.UIs["roadmap"].Archives[platformKeyGeneric].SHA256
	want := hex.EncodeToString(sum[:])
	if got != want {
		t.Fatalf("ui SHA256 = %q, want %q", got, want)
	}
	if got := lock.Caches["session"].Archives[providerpkg.CurrentPlatformString()].SHA256; got != want {
		t.Fatalf("cache SHA256 = %q, want %q", got, want)
	}
	if got := lock.S3["assets"].Archives[providerpkg.CurrentPlatformString()].SHA256; got != want {
		t.Fatalf("S3 SHA256 = %q, want %q", got, want)
	}
}

func TestLoadForExecutionAtPath_ResolvesLocalTopLevelPluginsWithoutLockfile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	authArtifact := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "auth-plugin"))
	authManifestPath := filepath.Join(dir, "auth-manifest.yaml")
	authManifest, err := providerpkg.EncodeSourceManifestFormat(&providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindAuthentication,
		Source:  "github.com/testowner/plugins/local-auth",
		Version: "0.0.1-alpha.1",
		Spec:    &providermanifestv1.Spec{},
		Artifacts: []providermanifestv1.Artifact{
			{OS: runtime.GOOS, Arch: runtime.GOARCH, Path: authArtifact},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: authArtifact, Args: []string{"serve-auth"}},
	}, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat auth: %v", err)
	}
	if err := os.WriteFile(authManifestPath, authManifest, 0o644); err != nil {
		t.Fatalf("WriteFile auth manifest: %v", err)
	}
	authExecutablePath := filepath.Join(dir, filepath.FromSlash(authArtifact))
	if err := os.MkdirAll(filepath.Dir(authExecutablePath), 0o755); err != nil {
		t.Fatalf("MkdirAll auth artifact: %v", err)
	}
	if err := os.WriteFile(authExecutablePath, []byte("auth-binary"), 0o755); err != nil {
		t.Fatalf("WriteFile auth artifact: %v", err)
	}

	dbPath := filepath.Join(dir, "gestalt.db")
	idbManifestPath := writeStubIndexedDBManifest(t, dir)
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := fmt.Sprintf(`apiVersion: %s
providers:
  authentication:
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
    authentication: auth
    indexeddb: sqlite
  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`, config.APIVersionV3, idbManifestPath, "sqlite://"+dbPath)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lc := NewLifecycle()
	loaded, _, err := lc.LoadForExecutionAtPath(cfgPath, false)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath: %v", err)
	}

	authEntry := mustSelectedHostProviderEntry(t, loaded, config.HostProviderKindAuthentication)
	if authEntry == nil || authEntry.ResolvedManifest == nil {
		t.Fatalf("auth resolved manifest = %+v", authEntry)
		return
	}
	if !strings.HasSuffix(filepath.ToSlash(authEntry.Command), filepath.ToSlash(filepath.Join(".gestaltd", "auth", "auth", filepath.FromSlash(authArtifact)))) {
		t.Fatalf("auth command = %q", authEntry.Command)
	}
	if got := authEntry.Args; len(got) != 1 || got[0] != "serve-auth" {
		t.Fatalf("auth args = %v, want [serve-auth]", got)
	}
	authCfg := decodeNodeMap(t, authEntry.Config)
	if authCfg["command"] != authEntry.Command {
		t.Fatalf("auth config command = %v, want %q", authCfg["command"], authEntry.Command)
	}
	authPluginCfg, ok := authCfg["config"].(map[string]any)
	if !ok || authPluginCfg["clientId"] != "local-auth-client" {
		t.Fatalf("auth nested config = %#v", authCfg["config"])
	}

	if _, err := os.Stat(filepath.Join(dir, InitLockfileName)); err != nil {
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
	authManifest, err := providerpkg.EncodeSourceManifestFormat(&providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindAuthentication,
		Source:  "github.com/testowner/plugins/local-source-auth",
		Version: "0.0.1-alpha.1",
		Spec:    &providermanifestv1.Spec{},
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
  authentication:
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
    authentication: auth
    indexeddb: sqlite
  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`, config.APIVersionV3, idbManifestPath, "sqlite://"+dbPath)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lc := NewLifecycle()
	loaded, _, err := lc.LoadForExecutionAtPath(cfgPath, false)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath: %v", err)
	}

	authEntry := mustSelectedHostProviderEntry(t, loaded, config.HostProviderKindAuthentication)
	if authEntry == nil || authEntry.ResolvedManifest == nil {
		t.Fatalf("auth resolved manifest = %+v", authEntry)
		return
	}
	if authEntry.Command == "" {
		t.Fatal("auth command = empty, want prepared executable path")
	}
	authCfg := decodeNodeMap(t, authEntry.Config)
	manifestPathValue, _ := authCfg["manifestPath"].(string)
	if !strings.HasSuffix(filepath.ToSlash(manifestPathValue), filepath.ToSlash(filepath.Join(".gestaltd", "auth", "auth", "manifest.yaml"))) {
		t.Fatalf("auth manifest_path = %v", authCfg["manifestPath"])
	}
	if authCfg["command"] == "" {
		t.Fatalf("auth config command = %v, want prepared executable path", authCfg["command"])
	}
	if _, err := os.Stat(filepath.Join(dir, InitLockfileName)); err != nil {
		t.Fatalf("expected lockfile to be created: %v", err)
	}
}

func TestLoadForExecutionAtPath_GeneratesStaticCatalogForLocalSourceHybridPlugin(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
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
	manifest, err := providerpkg.EncodeSourceManifestFormat(&providermanifestv1.Manifest{
		Source:      "github.com/testowner/plugins/local-generated-provider",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Generated Local Provider",
		Kind:        providermanifestv1.KindPlugin, Spec: withNoAuthDefaultConnection(&providermanifestv1.Spec{}),
	}, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeManifestFormat: %v", err)
	}
	writeTestFile("manifest.yaml", manifest, 0o644)

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := requiredComponentConfigV3YAML(t, dir, filepath.Join(dir, "gestalt.db")) + `plugins:
    example:
      source:
        path: ./manifest.yaml
` + `server:
` + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	writeTestFile("config.yaml", []byte(cfg), 0o644)

	lc := NewLifecycle()
	loaded, _, err := lc.LoadForExecutionAtPath(cfgPath, false)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath: %v", err)
	}

	intg := loaded.Plugins["example"]
	if intg == nil || intg.ResolvedManifest == nil {
		t.Fatalf("ResolvedManifest = %+v", intg)
	}
	catalogData, err := os.ReadFile(filepath.Join(dir, "catalog.yaml"))
	if err != nil {
		t.Fatalf("ReadFile(catalog.yaml): %v", err)
	}
	if !strings.Contains(string(catalogData), "generated_op") {
		t.Fatalf("unexpected catalog contents: %s", catalogData)
	}
	if _, err := os.Stat(filepath.Join(dir, InitLockfileName)); err != nil {
		t.Fatalf("expected lockfile to be created: %v", err)
	}
}

func TestLoadForExecutionAtPath_GeneratesStaticCatalogForLocalPythonSourcePlugin(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("local Python source plugin fixture is POSIX-only")
	}

	dir := t.TempDir()
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

[tool.gestalt]
provider = "provider"
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
	artifactRel := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "provider"))
	writeTestFile(artifactRel, []byte("python-provider"), 0o755)

	manifest, err := providerpkg.EncodeSourceManifestFormat(&providermanifestv1.Manifest{
		Source:      "github.com/testowner/plugins/local-python-provider",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Generated Local Python Provider",
		Kind:        providermanifestv1.KindPlugin, Spec: withNoAuthDefaultConnection(&providermanifestv1.Spec{}),
		Artifacts: []providermanifestv1.Artifact{{
			OS:   runtime.GOOS,
			Arch: runtime.GOARCH,
			Path: artifactRel,
		}},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: artifactRel},
	}, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeManifestFormat: %v", err)
	}
	writeTestFile("manifest.yaml", manifest, 0o644)

	cfg := requiredComponentConfigV3YAML(t, dir, filepath.Join(dir, "gestalt.db")) + `plugins:
    example:
      source:
        path: ./manifest.yaml
` + `server:
` + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	writeTestFile("config.yaml", []byte(cfg), 0o644)
	writeTestFile("exercise.py", []byte(`import json

import gestalt
import provider

provider.plugin.configure_provider("example", {"prefix": "Hello"})
status, body = provider.plugin.execute("echo", {
    "names": ["Ada", "Grace"],
    "metadata": {"role": "admin"},
    "filters": {"owner": "Ada"},
    "limit": 3,
}, gestalt.Request())
double_status, double_body = provider.plugin.execute("times_two", {
    "value": 3,
}, gestalt.Request())
decode_status, decode_body = provider.plugin.execute("times_two", {
    "value": "oops",
}, gestalt.Request())
explode_status, explode_body = provider.plugin.execute("explode", {}, gestalt.Request())
zero_status, zero_body = provider.plugin.execute("status_zero", {}, gestalt.Request())
maybe_status, maybe_body = provider.plugin.execute("maybe_filters", {
    "owner": "Grace",
}, gestalt.Request())
list_status, list_body = provider.plugin.execute("list_items", {}, gestalt.Request())
session_catalog = provider.plugin.catalog_for_request(gestalt.Request(token="secret-token"))
print(json.dumps({
    "status": status,
    "body": json.loads(body),
    "double_status": double_status,
    "double_body": json.loads(double_body),
    "decode_status": decode_status,
    "decode_body": json.loads(decode_body),
    "explode_status": explode_status,
    "explode_body": json.loads(explode_body),
    "list_status": list_status,
    "list_body": json.loads(list_body),
    "maybe_status": maybe_status,
    "maybe_body": json.loads(maybe_body),
    "supports_session_catalog": provider.plugin.supports_session_catalog(),
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
    "zero_status": zero_status,
    "zero_body": json.loads(zero_body),
}, sort_keys=True))
`), 0o644)

	lc := NewLifecycle()
	loaded, _, err := lc.LoadForExecutionAtPath(filepath.Join(dir, "config.yaml"), false)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath: %v", err)
	}

	intg := loaded.Plugins["example"]
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

	if _, err := os.Stat(filepath.Join(dir, InitLockfileName)); err != nil {
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

func TestApplyLockedPlugins_SkipsNilIntegrationPlugins(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.yaml")
	artifactRel := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "provider"))
	artifactPath := filepath.Join(dir, filepath.FromSlash(artifactRel))
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatalf("MkdirAll artifact dir: %v", err)
	}
	if err := os.WriteFile(artifactPath, []byte("local-provider"), 0o755); err != nil {
		t.Fatalf("WriteFile artifact: %v", err)
	}
	manifest, err := providerpkg.EncodeSourceManifestFormat(&providermanifestv1.Manifest{
		Source:      "github.com/testowner/plugins/local-provider",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Local Provider",
		Kind:        providermanifestv1.KindPlugin, Spec: withNoAuthDefaultConnection(&providermanifestv1.Spec{}),
		Artifacts: []providermanifestv1.Artifact{{
			OS:   runtime.GOOS,
			Arch: runtime.GOARCH,
			Path: artifactRel,
		}},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: artifactRel},
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
	cfg := requiredComponentConfigV3YAML(t, dir, filepath.Join(dir, "gestalt.db")) + `plugins:
    example:
      source:
        path: ./manifest.yaml
` + `server:
` + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load config: %v", err)
	}
	loaded.Plugins["missing"] = &config.ProviderEntry{}

	lc := NewLifecycle()
	if _, err := lc.applyLockedProviders([]string{cfgPath}, StatePaths{}, loaded, false, nil); err != nil {
		t.Fatalf("applyLockedProviders: %v", err)
	}
	if loaded.Plugins["example"] == nil || loaded.Plugins["example"].ResolvedManifest == nil {
		t.Fatalf("ResolvedManifest = %+v", loaded.Plugins["example"])
	}
}

func TestLockMatchesConfig_FalseWithNilLock(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("apiVersion: "+config.APIVersionV3+"\nserver:\n  public:\n    port: 8080\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	paths := initPathsForConfig(cfgPath)

	if lockMatchesConfig(cfg, paths, nil) {
		t.Fatal("lockMatchesConfig returned true for nil lock")
	}
}

func TestLockMatchesConfig_RemoteS3UsesResourceNameFingerprint(t *testing.T) {
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
	paths := initPathsForConfig(cfgPath)
	if err := os.MkdirAll(paths.artifactsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll artifacts: %v", err)
	}
	lockEntry := LockEntry{
		Source:      cfg.Providers.S3["assets"].SourceRemoteLocation(),
		Version:     "0.0.1-alpha.1",
		Fingerprint: mustFingerprint(t, "assets", cfg.Providers.S3["assets"], paths.configDir),
		Manifest:    filepath.ToSlash(filepath.Join("s3", "assets", "manifest.yaml")),
	}
	manifestPath := resolveLockPath(paths.artifactsDir, lockEntry.Manifest)
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatalf("MkdirAll manifest dir: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte("kind: s3\n"), 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	lock := &Lockfile{
		Version: LockVersion,
		S3: map[string]LockEntry{
			"assets": lockEntry,
		},
	}
	if !lockMatchesConfig(cfg, paths, lock) {
		t.Fatal("lockMatchesConfig returned false for matching remote S3 lock entry")
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
			manifest += "spec:\n  assetRoot: assets\n"
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
		firstManifestPath := writeSourceManifest(t, filepath.Join(root, "one"), "plugins/sample/manifest.yaml", providermanifestv1.KindAuthorization)
		secondManifestPath := writeSourceManifest(t, filepath.Join(root, "two"), "plugins/sample/manifest.yaml", providermanifestv1.KindAuthorization)

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
		firstManifestPath := writeSourceManifest(t, filepath.Join(root, "one"), "plugins/sample/manifest.yaml", providermanifestv1.KindAuthorization)
		secondManifestPath := writeSourceManifest(t, filepath.Join(root, "two"), "plugins/sample/manifest.yaml", providermanifestv1.KindAuthorization)
		if err := os.WriteFile(secondManifestPath, []byte("source: github.com/test-org/two/component\nversion: 0.0.2\nkind: authorization\n"), 0o644); err != nil {
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

func TestReadLockfile_RejectsUnsupportedVersion(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lockPath := filepath.Join(dir, InitLockfileName)
	if err := os.WriteFile(lockPath, []byte(`{"version":999,"providers":{}}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := ReadLockfile(lockPath)
	if err == nil {
		t.Fatal("expected error for unsupported lockfile version")
		return
	}
	if !strings.Contains(err.Error(), "unsupported lockfile version") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadLockfile_AllowsRuntimeProviderNamedAuth(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lockPath := filepath.Join(dir, InitLockfileName)
	if err := os.WriteFile(lockPath, []byte(`{
  "version": 10,
  "providers": {
    "auth": {
      "fingerprint": "plugin-fp",
      "source": "github.com/example/auth-plugin",
      "manifest": ".gestaltd/providers/auth/manifest.yaml"
    }
  }
}
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	lock, err := ReadLockfile(lockPath)
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	if got := lock.Providers["auth"].Source; got != "github.com/example/auth-plugin" {
		t.Fatalf("provider auth source = %q, want %q", got, "github.com/example/auth-plugin")
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
	lockPath := filepath.Join(dir, InitLockfileName)
	want := &Lockfile{
		Version: LockVersion,
		Providers: map[string]LockEntry{
			"example": {
				Fingerprint: "provider-fp",
				Source:      "github.com/test-org/test-repo/test-plugin",
				Version:     "1.0.0",
				Archives: map[string]LockArchive{
					"darwin/arm64": {URL: "https://example.com/example.tar.gz", SHA256: "abc123"},
				},
				Manifest:   ".gestaltd/providers/example/manifest.json",
				Executable: ".gestaltd/providers/example/artifacts/darwin/arm64/provider",
			},
		},
		Authentication: map[string]LockEntry{
			"oauth": {
				Fingerprint: "auth-fp",
				Source:      "github.com/test-org/test-repo/auth-oauth",
				Version:     "1.0.1",
				Archives: map[string]LockArchive{
					"darwin/arm64": {URL: "https://example.com/auth-oauth.tar.gz", SHA256: "auth123"},
				},
				Manifest:   ".gestaltd/providers/auth/oauth/manifest.json",
				Executable: ".gestaltd/providers/auth/oauth/artifacts/darwin/arm64/auth-oauth",
			},
		},
		IndexedDBs: map[string]LockEntry{
			"main": {
				Fingerprint: "indexeddb-main-fp",
				Source:      "github.com/test-org/test-repo/indexeddb-main",
				Version:     "1.1.0",
				Archives: map[string]LockArchive{
					"darwin/arm64": {URL: "https://example.com/indexeddb-main.tar.gz", SHA256: "abc999"},
				},
				Manifest:   "indexeddb/main/manifest.json",
				Executable: "indexeddb/main/artifacts/darwin/arm64/indexeddb-main",
			},
			"archive": {
				Fingerprint: "indexeddb-archive-fp",
				Source:      "github.com/test-org/test-repo/indexeddb-archive",
				Version:     "1.2.0",
				Archives: map[string]LockArchive{
					"darwin/arm64": {URL: "https://example.com/indexeddb-archive.tar.gz", SHA256: "def999"},
				},
				Manifest:   "indexeddb/archive/manifest.json",
				Executable: "indexeddb/archive/artifacts/darwin/arm64/indexeddb-archive",
			},
		},
		Workflows: map[string]LockEntry{
			"temporal": {
				Fingerprint: "workflow-temporal-fp",
				Source:      "github.com/test-org/test-repo/workflow-temporal",
				Version:     "1.3.0",
				Archives: map[string]LockArchive{
					"darwin/arm64": {URL: "https://example.com/workflow-temporal.tar.gz", SHA256: "workflow123"},
				},
				Manifest:   "workflow/temporal/manifest.json",
				Executable: "workflow/temporal/artifacts/darwin/arm64/workflow-temporal",
			},
		},
		Telemetry: map[string]LockEntry{
			"default": {
				Fingerprint: "telemetry-fp",
				Source:      "github.com/test-org/test-repo/telemetry-declarative",
				Kind:        providermanifestv1.KindPlugin,
				Runtime:     providerReleaseRuntimeDeclarative,
				Version:     "1.4.0",
				Archives: map[string]LockArchive{
					"generic": {URL: "https://example.com/telemetry.tar.gz", SHA256: "telemetry123"},
				},
			},
		},
		Audit: map[string]LockEntry{
			"default": {
				Fingerprint: "audit-fp",
				Source:      "github.com/test-org/test-repo/audit-declarative",
				Kind:        providermanifestv1.KindPlugin,
				Runtime:     providerReleaseRuntimeDeclarative,
				Version:     "1.5.0",
				Archives: map[string]LockArchive{
					"generic": {URL: "https://example.com/audit.tar.gz", SHA256: "audit123"},
				},
			},
		},
		UIs: map[string]LockEntry{
			"roadmap": {
				Fingerprint: "ui-fp",
				Source:      "github.com/test-org/test-repo/test-ui",
				Version:     "2.0.0",
				Archives: map[string]LockArchive{
					"generic": {URL: "https://example.com/ui.tar.gz", SHA256: "def456"},
				},
				Manifest:  ".gestaltd/ui/roadmap/manifest.json",
				AssetRoot: ".gestaltd/ui/roadmap/assets",
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
	if strings.Contains(string(lockData), `"version": 7`) {
		t.Fatalf("lockfile = %s, want schema-based versioning", lockData)
	}
	if strings.Contains(string(lockData), `"manifest":`) || strings.Contains(string(lockData), `"executable":`) || strings.Contains(string(lockData), `"assetRoot":`) {
		t.Fatalf("lockfile = %s, want portable entries only", lockData)
	}
	var diskLock providerLockfile
	if err := json.Unmarshal(lockData, &diskLock); err != nil {
		t.Fatalf("Unmarshal lockfile: %v", err)
	}
	providerEntry, ok := diskLock.Providers.Plugin["example"]
	if !ok {
		t.Fatal(`disk lock providers.plugin["example"] not found`)
	}
	if providerEntry.InputDigest != want.Providers["example"].Fingerprint {
		t.Fatalf("provider inputDigest = %q, want %q", providerEntry.InputDigest, want.Providers["example"].Fingerprint)
	}
	if providerEntry.Package != want.Providers["example"].Source {
		t.Fatalf("provider package = %q, want %q", providerEntry.Package, want.Providers["example"].Source)
	}
	if providerEntry.Source != "" {
		t.Fatalf("provider source = %q, want omitted portable source", providerEntry.Source)
	}
	if providerEntry.Kind != providermanifestv1.KindPlugin {
		t.Fatalf("provider kind = %q, want %q", providerEntry.Kind, providermanifestv1.KindPlugin)
	}
	if providerEntry.Runtime != providerLockRuntimeExecutable {
		t.Fatalf("provider runtime = %q, want %q", providerEntry.Runtime, providerLockRuntimeExecutable)
	}
	authEntry, ok := diskLock.Providers.Authentication["oauth"]
	if !ok {
		t.Fatal(`disk lock providers.authentication["oauth"] not found`)
	}
	if authEntry.InputDigest != want.Authentication["oauth"].Fingerprint {
		t.Fatalf("authentication inputDigest = %q, want %q", authEntry.InputDigest, want.Authentication["oauth"].Fingerprint)
	}
	if authEntry.Package != want.Authentication["oauth"].Source {
		t.Fatalf("authentication package = %q, want %q", authEntry.Package, want.Authentication["oauth"].Source)
	}
	if authEntry.Source != "" {
		t.Fatalf("authentication source = %q, want omitted portable source", authEntry.Source)
	}
	if authEntry.Kind != providermanifestv1.KindAuthentication {
		t.Fatalf("authentication kind = %q, want %q", authEntry.Kind, providermanifestv1.KindAuthentication)
	}
	if authEntry.Runtime != providerLockRuntimeExecutable {
		t.Fatalf("authentication runtime = %q, want %q", authEntry.Runtime, providerLockRuntimeExecutable)
	}
	telemetryEntry, ok := diskLock.Providers.Telemetry["default"]
	if !ok {
		t.Fatal(`disk lock providers.telemetry["default"] not found`)
	}
	if telemetryEntry.Package != want.Telemetry["default"].Source {
		t.Fatalf("telemetry package = %q, want %q", telemetryEntry.Package, want.Telemetry["default"].Source)
	}
	if telemetryEntry.Kind != providermanifestv1.KindPlugin {
		t.Fatalf("telemetry kind = %q, want %q", telemetryEntry.Kind, providermanifestv1.KindPlugin)
	}
	if telemetryEntry.Runtime != providerReleaseRuntimeDeclarative {
		t.Fatalf("telemetry runtime = %q, want %q", telemetryEntry.Runtime, providerReleaseRuntimeDeclarative)
	}
	auditEntry, ok := diskLock.Providers.Audit["default"]
	if !ok {
		t.Fatal(`disk lock providers.audit["default"] not found`)
	}
	if auditEntry.Package != want.Audit["default"].Source {
		t.Fatalf("audit package = %q, want %q", auditEntry.Package, want.Audit["default"].Source)
	}
	if auditEntry.Kind != providermanifestv1.KindPlugin {
		t.Fatalf("audit kind = %q, want %q", auditEntry.Kind, providermanifestv1.KindPlugin)
	}
	if auditEntry.Runtime != providerReleaseRuntimeDeclarative {
		t.Fatalf("audit runtime = %q, want %q", auditEntry.Runtime, providerReleaseRuntimeDeclarative)
	}
	uiEntry, ok := diskLock.Providers.UI["roadmap"]
	if !ok {
		t.Fatal(`disk lock providers.ui["roadmap"] not found`)
	}
	if uiEntry.InputDigest != want.UIs["roadmap"].Fingerprint {
		t.Fatalf("ui inputDigest = %q, want %q", uiEntry.InputDigest, want.UIs["roadmap"].Fingerprint)
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
	if got.Version != want.Version {
		t.Fatalf("Version = %d, want %d", got.Version, want.Version)
	}
	if got.Providers["example"].Fingerprint != want.Providers["example"].Fingerprint {
		t.Fatal("provider fingerprint mismatch")
	}
	if got.Providers["example"].Source != want.Providers["example"].Source || got.Providers["example"].Version != want.Providers["example"].Version {
		t.Fatal("provider source mismatch")
	}
	if got.Authentication["oauth"].Fingerprint != want.Authentication["oauth"].Fingerprint {
		t.Fatal("authentication fingerprint mismatch")
	}
	if got.IndexedDBs["main"].Fingerprint != want.IndexedDBs["main"].Fingerprint {
		t.Fatal("indexeddb fingerprint mismatch")
	}
	if got.IndexedDBs["archive"].Executable != "" {
		t.Fatal("indexeddb executable should not round-trip from portable lock schema")
	}
	if got.Workflows["temporal"].Source != want.Workflows["temporal"].Source || got.Workflows["temporal"].Version != want.Workflows["temporal"].Version {
		t.Fatal("workflow lock entry mismatch")
	}
	if got.Workflows["temporal"].Executable != "" {
		t.Fatal("workflow executable should not round-trip from portable lock schema")
	}
	if got.Telemetry["default"].Runtime != providerReleaseRuntimeDeclarative {
		t.Fatalf("telemetry runtime = %q, want %q", got.Telemetry["default"].Runtime, providerReleaseRuntimeDeclarative)
	}
	if got.Audit["default"].Runtime != providerReleaseRuntimeDeclarative {
		t.Fatalf("audit runtime = %q, want %q", got.Audit["default"].Runtime, providerReleaseRuntimeDeclarative)
	}
	if got.UIs["roadmap"].Source != want.UIs["roadmap"].Source || got.UIs["roadmap"].Version != want.UIs["roadmap"].Version {
		t.Fatal("ui lock entry mismatch")
	}
	if got.Providers["example"].Manifest != "" || got.UIs["roadmap"].AssetRoot != "" {
		t.Fatal("portable lock schema should not populate local path fields on read")
	}
}

func TestResolveArchiveForPlatform(t *testing.T) {
	t.Parallel()

	entry := LockEntry{
		Archives: map[string]LockArchive{
			"darwin/arm64": {URL: "https://example.com/darwin-arm64", SHA256: "abc"},
			"linux/amd64":  {URL: "https://example.com/linux-amd64", SHA256: "def"},
			"generic":      {URL: "https://example.com/generic", SHA256: "xyz"},
		},
	}

	tests := []struct {
		name     string
		platform string
		wantURL  string
		wantOK   bool
	}{
		{"exact match", "darwin/arm64", "https://example.com/darwin-arm64", true},
		{"fallback without libc", "linux/amd64", "https://example.com/linux-amd64", true},
		{"no match falls to generic", "windows/amd64", "https://example.com/generic", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			archive, _, ok := resolveArchiveForPlatform(entry, tt.platform)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && archive.URL != tt.wantURL {
				t.Errorf("URL = %q, want %q", archive.URL, tt.wantURL)
			}
		})
	}

	// No match at all
	sparse := LockEntry{Archives: map[string]LockArchive{"windows/amd64": {URL: "x"}}}
	if _, _, ok := resolveArchiveForPlatform(sparse, "darwin/arm64"); ok {
		t.Error("expected no match for darwin/arm64 when only windows is available")
	}
}

func TestHashArchiveEntry_HashesFallbackArchive(t *testing.T) {
	t.Parallel()

	const payload = "generic plugin archive"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(payload))
	}))
	defer server.Close()

	entry := LockEntry{
		Source: server.URL,
		Archives: map[string]LockArchive{
			platformKeyGeneric: {URL: server.URL},
		},
	}

	if err := NewLifecycle().hashArchiveEntry(context.Background(), providermanifestv1.KindUI, "roadmap", &entry, initPaths{}, "linux/amd64", nil); err != nil {
		t.Fatalf("hashArchiveEntry: %v", err)
	}

	got := entry.Archives[platformKeyGeneric]
	if got.URL != server.URL {
		t.Fatalf("generic URL = %q, want %q", got.URL, server.URL)
	}
	want := sha256.Sum256([]byte(payload))
	if got.SHA256 != hex.EncodeToString(want[:]) {
		t.Fatalf("generic SHA256 = %q, want %q", got.SHA256, hex.EncodeToString(want[:]))
	}
}
