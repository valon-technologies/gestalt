package operator

import (
	"archive/tar"
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
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/pluginsource"
	"github.com/valon-technologies/gestalt/server/internal/providerpkg"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"gopkg.in/yaml.v3"
)

type staticSourceResolver struct {
	localPath string
}

func (r staticSourceResolver) Resolve(context.Context, pluginsource.Source, string) (*pluginsource.ResolvedPackage, error) {
	return &pluginsource.ResolvedPackage{
		LocalPath: r.localPath,
		Cleanup:   func() {},
	}, nil
}

type mappedSourceResolver struct {
	paths map[string]string
}

func (r mappedSourceResolver) Resolve(_ context.Context, src pluginsource.Source, _ string) (*pluginsource.ResolvedPackage, error) {
	localPath, ok := r.paths[src.String()]
	if !ok {
		return nil, fmt.Errorf("no test package for %s", src.String())
	}
	return &pluginsource.ResolvedPackage{
		LocalPath: localPath,
		Cleanup:   func() {},
	}, nil
}

type authMappedSourceResolver struct {
	paths  map[string]string
	tokens map[string]string
}

func (r authMappedSourceResolver) Resolve(_ context.Context, src pluginsource.Source, _ string) (*pluginsource.ResolvedPackage, error) {
	if wantToken, ok := r.tokens[src.String()]; ok && src.Token != wantToken {
		return nil, fmt.Errorf("source %s token = %q, want %q", src.String(), src.Token, wantToken)
	}
	localPath, ok := r.paths[src.String()]
	if !ok {
		return nil, fmt.Errorf("no test package for %s", src.String())
	}
	return &pluginsource.ResolvedPackage{
		LocalPath: localPath,
		Cleanup:   func() {},
	}, nil
}

type versionedSourceResolver struct {
	paths  map[string]map[string]string
	tokens map[string]string
}

func (r versionedSourceResolver) Resolve(_ context.Context, src pluginsource.Source, version string) (*pluginsource.ResolvedPackage, error) {
	if wantToken, ok := r.tokens[src.String()]; ok && src.Token != wantToken {
		return nil, fmt.Errorf("source %s token = %q, want %q", src.String(), src.Token, wantToken)
	}
	versions, ok := r.paths[src.String()]
	if !ok {
		return nil, fmt.Errorf("no test package for %s", src.String())
	}
	localPath, ok := versions[version]
	if !ok {
		return nil, fmt.Errorf("no test package for %s version %s", src.String(), version)
	}
	return &pluginsource.ResolvedPackage{
		LocalPath: localPath,
		Cleanup:   func() {},
	}, nil
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

func writeStubIndexedDBManifest(t *testing.T, dir string) string {
	t.Helper()
	manifestPath := filepath.Join(dir, "indexeddb-manifest.yaml")
	data, err := providerpkg.EncodeSourceManifestFormat(&providermanifestv1.Manifest{
		Source:  "github.com/test/providers/indexeddb-stub",
		Version: "0.0.1-alpha.1",
		Kind:    providermanifestv1.KindIndexedDB,
		Spec:    &providermanifestv1.Spec{},
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

func requiredServerDatastoreYAML() string {
	return `  providers:
    indexeddb: sqlite
`
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
	manifest, err := providerpkg.EncodeSourceManifestFormat(&providermanifestv1.Manifest{
		Source:      "github.com/testowner/plugins/local-provider",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Local Provider",
		Description: "Local executable provider",
		Kind:        providermanifestv1.KindPlugin, Spec: &providermanifestv1.Spec{
			Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeNone},
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
	cfg := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "gestalt.db")) + `plugins:
    example:
      source:
        path: ./manifest.yaml
` + `server:
` + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lc := NewLifecycle(nil)
	loaded, _, err := lc.LoadForExecutionAtPath(cfgPath, false)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath: %v", err)
	}

	intg := loaded.Plugins["example"]
	if intg.DisplayName != "Local Provider" {
		t.Fatalf("DisplayName = %q", intg.DisplayName)
	}
	if intg.Description != "Local executable provider" {
		t.Fatalf("Description = %q", intg.Description)
	}
	if intg == nil || intg.ResolvedManifest == nil {
		t.Fatalf("ResolvedManifest = %+v", intg)
	}
	if intg.ResolvedManifestPath != manifestPath {
		t.Fatalf("ResolvedManifestPath = %q, want %q", intg.ResolvedManifestPath, manifestPath)
	}
	if _, err := os.Stat(filepath.Join(dir, InitLockfileName)); !os.IsNotExist(err) {
		t.Fatalf("lockfile should not be created, got err=%v", err)
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
	cfg := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "gestalt.db")) + `plugins:
    notion:
      source:
        path: ./manifest.yaml
` + `server:
` + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lc := NewLifecycle(nil)
	loaded, _, err := lc.LoadForExecutionAtPath(cfgPath, false)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath: %v", err)
	}

	intg := loaded.Plugins["notion"]
	if intg == nil || intg.ResolvedManifest == nil || intg.ResolvedManifest.Spec == nil {
		t.Fatalf("ResolvedManifest = %+v", intg)
	}
	if got := intg.ResolvedManifest.Spec.MCPURL(); got != "https://mcp.notion.com/mcp" {
		t.Fatalf("MCPURL = %q, want %q", got, "https://mcp.notion.com/mcp")
	}
	conn := intg.ResolvedManifest.Spec.Connections["mcp"]
	if conn == nil || conn.Auth == nil {
		t.Fatalf("MCP connection = %#v", conn)
	}
	if got := conn.Auth.Type; got != providermanifestv1.AuthTypeMCPOAuth {
		t.Fatalf("MCP auth type = %q, want %q", got, providermanifestv1.AuthTypeMCPOAuth)
	}
	if _, err := os.Stat(filepath.Join(dir, InitLockfileName)); !os.IsNotExist(err) {
		t.Fatalf("lockfile should not be created, got err=%v", err)
	}
}

func TestLoadForExecutionAtPath_ResolvesLocalMountedWebUIWithoutLockfile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		uiConfigYAML string
		extraYAML    string
		uiKey        string
		wantPath     string
		wantPolicy   string
		ownedUIPath  string
		wantErr      string
	}{
		{
			name: "direct mounted ui",
			uiConfigYAML: `  ui:
    roadmap:
      source:
        path: ./webui/manifest.yaml
      path: /create-customer-roadmap-review
`,
			uiKey:    "roadmap",
			wantPath: "/create-customer-roadmap-review",
		},
		{
			name: "plugin mount binds explicit ui",
			uiConfigYAML: `  ui:
    roadmap:
      source:
        path: ./webui/manifest.yaml
plugins:
    roadmap:
      source:
        path: ./plugin/manifest.yaml
      ui: roadmap
      mountPath: /create-customer-roadmap-review
      authorizationPolicy: roadmap_policy
`,
			extraYAML: `authorization:
  policies:
    roadmap_policy:
      default: deny
      members:
        - email: viewer@example.test
          role: viewer
`,
			uiKey:      "roadmap",
			wantPath:   "/create-customer-roadmap-review",
			wantPolicy: "roadmap_policy",
		},
		{
			name: "disabled explicit plugin mount is rejected",
			uiConfigYAML: `  ui:
    roadmap:
      source:
        path: ./webui/manifest.yaml
    console:
      source:
        path: ./webui/manifest.yaml
      path: /console
plugins:
    roadmap:
      disabled: true
      source:
        path: ./plugin/manifest.yaml
      ui: roadmap
      mountPath: /create-customer-roadmap-review
      authorizationPolicy: roadmap_policy
`,
			uiKey:   "roadmap",
			wantErr: "field disabled not found",
		},
		{
			name: "disabled plugin is rejected",
			uiConfigYAML: `  ui:
    roadmap:
      source:
        path: ./webui/manifest.yaml
plugins:
    roadmap:
      disabled: true
      source:
        path: ./plugin/manifest.yaml
      mountPath: /create-customer-roadmap-review
`,
			wantErr: "field disabled not found",
		},
		{
			name: "plugin owned ui via plugin mount",
			uiConfigYAML: `plugins:
    roadmap:
      source:
        path: ./plugin/manifest.yaml
      mountPath: /create-customer-roadmap-review
      authorizationPolicy: roadmap_policy
`,
			extraYAML: `authorization:
  policies:
    roadmap_policy:
      default: deny
      members:
        - email: viewer@example.test
          role: viewer
`,
			uiKey:       "roadmap",
			wantPath:    "/create-customer-roadmap-review",
			wantPolicy:  "roadmap_policy",
			ownedUIPath: "../webui/manifest.yaml",
		},
		{
			name: "plugin owned ui with same-name ui overlay",
			uiConfigYAML: `  ui:
    roadmap:
      source:
        path: ./webui/manifest.yaml
plugins:
    roadmap:
      source:
        path: ./plugin/manifest.yaml
      mountPath: /create-customer-roadmap-review
      authorizationPolicy: roadmap_policy
`,
			extraYAML: `authorization:
  policies:
    roadmap_policy:
      default: deny
      members:
        - email: viewer@example.test
          role: viewer
`,
			uiKey:       "roadmap",
			wantPath:    "/create-customer-roadmap-review",
			wantPolicy:  "roadmap_policy",
			ownedUIPath: "../webui/manifest.yaml",
		},
		{
			name: "disabled same-name ui overlay is rejected at parse time",
			uiConfigYAML: `  ui:
    roadmap:
      disabled: true
      source:
        path: ./webui/manifest.yaml
plugins:
    roadmap:
      source:
        path: ./plugin/manifest.yaml
      mountPath: /create-customer-roadmap-review
`,
			wantErr:     "field disabled not found",
			ownedUIPath: "../webui/manifest.yaml",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			webUIDir := filepath.Join(dir, "webui")
			if err := os.MkdirAll(filepath.Join(webUIDir, "dist"), 0o755); err != nil {
				t.Fatalf("MkdirAll webui dist: %v", err)
			}
			if err := os.WriteFile(filepath.Join(webUIDir, "dist", "index.html"), []byte("<html>roadmap</html>"), 0o644); err != nil {
				t.Fatalf("WriteFile index.html: %v", err)
			}
			manifestPath := filepath.Join(webUIDir, "manifest.yaml")
			spec := &providermanifestv1.Spec{AssetRoot: "dist"}
			if tc.wantPolicy != "" {
				spec.Routes = []providermanifestv1.WebUIRoute{
					{Path: "/", AllowedRoles: []string{"viewer"}},
				}
			}
			manifest, err := providerpkg.EncodeSourceManifestFormat(&providermanifestv1.Manifest{
				Kind:        providermanifestv1.KindWebUI,
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
				pluginSpec := &providermanifestv1.Spec{
					Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeNone},
				}
				if tc.ownedUIPath != "" {
					pluginSpec.UI = &providermanifestv1.OwnedUIRef{Path: tc.ownedUIPath}
				}
				pluginManifest, err := providerpkg.EncodeSourceManifestFormat(&providermanifestv1.Manifest{
					Source:      "github.com/testowner/plugins/roadmap",
					Version:     "0.0.1-alpha.1",
					DisplayName: "Roadmap Plugin",
					Kind:        providermanifestv1.KindPlugin,
					Spec:        pluginSpec,
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
			cfg := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "gestalt.db")) + tc.uiConfigYAML + tc.extraYAML + `server:
` + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
			if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
				t.Fatalf("WriteFile config: %v", err)
			}

			lc := NewLifecycle(nil)
			loaded, _, err := lc.LoadForExecutionAtPath(cfgPath, false)
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

			entry := loaded.Providers.UI[tc.uiKey]
			if entry == nil {
				t.Fatalf(`Providers.UI[%q] = nil`, tc.uiKey)
			}
			if entry.ResolvedManifest == nil {
				t.Fatal("ResolvedManifest = nil")
			}
			if got := entry.ResolvedManifestPath; got != manifestPath {
				t.Fatalf("ResolvedManifestPath = %q, want %q", got, manifestPath)
			}
			wantAssetRoot := filepath.Join(webUIDir, "dist")
			if got := entry.ResolvedAssetRoot; got != wantAssetRoot {
				t.Fatalf("ResolvedAssetRoot = %q, want %q", got, wantAssetRoot)
			}
			if got := entry.Path; got != tc.wantPath {
				t.Fatalf("Path = %q, want %q", got, tc.wantPath)
			}
			if got := entry.AuthorizationPolicy; got != tc.wantPolicy {
				t.Fatalf("AuthorizationPolicy = %q, want %q", got, tc.wantPolicy)
			}
			if tc.wantPolicy != "" {
				plugin := loaded.Plugins["roadmap"]
				if plugin == nil {
					t.Fatal(`Plugins["roadmap"] = nil`)
				}
				if got := plugin.AuthorizationPolicy; got != tc.wantPolicy {
					t.Fatalf("Plugin AuthorizationPolicy = %q, want %q", got, tc.wantPolicy)
				}
			}
			if _, err := os.Stat(filepath.Join(dir, InitLockfileName)); !os.IsNotExist(err) {
				t.Fatalf("lockfile should not be created, got err=%v", err)
			}
		})
	}
}

func TestLoadForExecutionAtPath_ReinitializesManagedPluginOwnedUIWhenUILockEntryIsMissing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	const pluginRef = "github.com/testowner/plugins/roadmap"
	const webUIRef = "github.com/testowner/web/roadmap-review"
	const version = "0.0.1-alpha.1"

	webUIPkg := mustBuildManagedProviderPackage(t, dir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindWebUI,
		Source:      webUIRef,
		Version:     version,
		DisplayName: "Roadmap Review UI",
		Spec: &providermanifestv1.Spec{
			AssetRoot: "dist",
		},
	}, map[string]string{
		"dist/index.html": "<html>roadmap review</html>",
	}, false)

	pluginPkg := mustBuildManagedProviderPackage(t, dir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindPlugin,
		Source:      pluginRef,
		Version:     version,
		DisplayName: "Roadmap Review",
		Entrypoint: &providermanifestv1.Entrypoint{
			ArtifactPath: filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "plugin")),
		},
		Spec: &providermanifestv1.Spec{
			Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeNone},
			UI: &providermanifestv1.OwnedUIRef{
				Ref:     webUIRef,
				Version: version,
				Auth:    &providermanifestv1.SourceAuth{Token: "owned-ui-token"},
			},
		},
	}, map[string]string{
		filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "plugin")): "plugin-binary",
	}, true)

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "gestalt.db")) + `  ui:
    roadmap:
      source:
        ref: ` + webUIRef + `
        version: ` + version + `
      path: /create-customer-roadmap-review
plugins:
    roadmap:
      source:
        ref: ` + pluginRef + `
        version: ` + version + `
      mountPath: /create-customer-roadmap-review
` + `server:
` + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lc := NewLifecycle(authMappedSourceResolver{
		paths: map[string]string{
			pluginRef: pluginPkg,
			webUIRef:  webUIPkg,
		},
		tokens: map[string]string{
			webUIRef: "owned-ui-token",
		},
	})
	lock, err := lc.InitAtPath(cfgPath)
	if err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}
	delete(lock.UIs, "roadmap")
	pluginLock := lock.Providers["roadmap"]
	pluginLock.Manifest = ""
	lock.Providers["roadmap"] = pluginLock
	if err := WriteLockfile(filepath.Join(dir, InitLockfileName), lock); err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}

	loaded, _, err := lc.LoadForExecutionAtPath(cfgPath, false)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath: %v", err)
	}
	entry := loaded.Providers.UI["roadmap"]
	if entry == nil || entry.ResolvedManifest == nil {
		t.Fatalf("Resolved plugin-owned UI = %+v", entry)
	}
	if entry.Path != "/create-customer-roadmap-review" {
		t.Fatalf("entry.Path = %q", entry.Path)
	}

	rewrittenLock, err := ReadLockfile(filepath.Join(dir, InitLockfileName))
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	if _, ok := rewrittenLock.UIs["roadmap"]; !ok {
		t.Fatalf("lock.UIs = %#v, want roadmap entry restored", rewrittenLock.UIs)
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
		Kind:        providermanifestv1.KindWebUI,
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
		Spec: &providermanifestv1.Spec{
			Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeNone},
			UI: &providermanifestv1.OwnedUIRef{
				Path: ownedUIManifestPath,
			},
		},
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

	pkgPath := filepath.Join(dir, "roadmap-plugin-pkg.tar.gz")
	if err := providerpkg.CreatePackageFromDir(pkgDir, pkgPath); err != nil {
		t.Fatalf("CreatePackageFromDir: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "gestalt.db")) + `plugins:
    roadmap:
      source:
        ref: ` + pluginRef + `
        version: ` + version + `
      mountPath: /create-customer-roadmap-review
` + `server:
` + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lc := NewLifecycle(mappedSourceResolver{paths: map[string]string{pluginRef: pkgPath}})
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

func TestLoadForExecutionAtPath_ReinitializesManagedPluginOwnedUIWhenPluginLockIsStale(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	const pluginRef = "github.com/testowner/plugins/roadmap"
	const webUIRef = "github.com/testowner/web/roadmap-review"
	const oldVersion = "0.0.1-alpha.1"
	const newVersion = "0.0.2-alpha.1"

	buildWebUIPackage := func(version string) string {
		return mustBuildManagedProviderPackage(t, dir, &providermanifestv1.Manifest{
			Kind:        providermanifestv1.KindWebUI,
			Source:      webUIRef,
			Version:     version,
			DisplayName: "Roadmap Review UI",
			Spec: &providermanifestv1.Spec{
				AssetRoot: "dist",
			},
		}, map[string]string{
			"dist/index.html": "<html>roadmap review " + version + "</html>",
		}, false)
	}
	buildPluginPackage := func(version string) string {
		return mustBuildManagedProviderPackage(t, dir, &providermanifestv1.Manifest{
			Kind:        providermanifestv1.KindPlugin,
			Source:      pluginRef,
			Version:     version,
			DisplayName: "Roadmap Review",
			Entrypoint: &providermanifestv1.Entrypoint{
				ArtifactPath: filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "plugin")),
			},
			Spec: &providermanifestv1.Spec{
				Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeNone},
				UI: &providermanifestv1.OwnedUIRef{
					Ref:     webUIRef,
					Version: version,
					Auth:    &providermanifestv1.SourceAuth{Token: "owned-ui-token"},
				},
			},
		}, map[string]string{
			filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "plugin")): "plugin-binary-" + version,
		}, true)
	}

	oldWebUIPkg := buildWebUIPackage(oldVersion)
	oldPluginPkg := buildPluginPackage(oldVersion)
	newWebUIPkg := buildWebUIPackage(newVersion)
	newPluginPkg := buildPluginPackage(newVersion)

	cfgPath := filepath.Join(dir, "config.yaml")
	writeConfig := func(version string) {
		cfg := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "gestalt.db")) + `plugins:
    roadmap:
      source:
        ref: ` + pluginRef + `
        version: ` + version + `
      mountPath: /create-customer-roadmap-review
` + `server:
` + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
		if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
			t.Fatalf("WriteFile config: %v", err)
		}
	}
	writeConfig(oldVersion)

	lc := NewLifecycle(versionedSourceResolver{
		paths: map[string]map[string]string{
			pluginRef: {
				oldVersion: oldPluginPkg,
				newVersion: newPluginPkg,
			},
			webUIRef: {
				oldVersion: oldWebUIPkg,
				newVersion: newWebUIPkg,
			},
		},
		tokens: map[string]string{
			webUIRef: "owned-ui-token",
		},
	})
	if _, err := lc.InitAtPath(cfgPath); err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}

	writeConfig(newVersion)

	loaded, _, err := lc.LoadForExecutionAtPath(cfgPath, false)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath: %v", err)
	}
	entry := loaded.Providers.UI["roadmap"]
	if entry == nil || entry.ResolvedManifest == nil {
		t.Fatalf("Resolved plugin-owned UI = %+v", entry)
	}
	if got := entry.ResolvedManifest.Version; got != newVersion {
		t.Fatalf("ResolvedManifest.Version = %q, want %q", got, newVersion)
	}
	if entry.Path != "/create-customer-roadmap-review" {
		t.Fatalf("entry.Path = %q", entry.Path)
	}

	rewrittenLock, err := ReadLockfile(filepath.Join(dir, InitLockfileName))
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	lockEntry, ok := rewrittenLock.UIs["roadmap"]
	if !ok {
		t.Fatalf("lock.UIs = %#v, want roadmap entry restored", rewrittenLock.UIs)
	}
	if got := lockEntry.Version; got != newVersion {
		t.Fatalf("lock.UIs[roadmap].Version = %q, want %q", got, newVersion)
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
		Spec: &providermanifestv1.Spec{
			Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeNone},
		},
	}, map[string]string{
		filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "plugin")): "plugin-binary-" + version,
	}, true)

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "gestalt.db")) + `plugins:
  roadmap:
    source:
      ref: ` + pluginRef + `
      version: ` + version + `
server:
` + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lc := NewLifecycle(versionedSourceResolver{
		paths: map[string]map[string]string{
			pluginRef: {
				version: pluginPkg,
			},
		},
	})
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
		Providers: map[string]LockProviderEntry{
			"roadmap": {
				Fingerprint: mustFingerprint(t, "roadmap", loadedCfg.Plugins["roadmap"], paths.configDir),
				Source:      pluginRef,
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
			Spec: &providermanifestv1.Spec{
				Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeNone},
			},
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
			Spec: &providermanifestv1.Spec{
				Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeNone},
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
			},
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

	cfgPath := filepath.Join(dir, "config.yaml")
	writeConfig := func(version string) {
		cfg := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "gestalt.db")) + `plugins:
  roadmap:
    source:
      ref: ` + pluginRef + `
      version: ` + version + `
server:
` + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
		if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
			t.Fatalf("WriteFile config: %v", err)
		}
	}
	writeConfig(oldVersion)

	lc := NewLifecycle(versionedSourceResolver{
		paths: map[string]map[string]string{
			pluginRef: {
				oldVersion: oldPluginPkg,
				newVersion: newPluginPkg,
			},
		},
	})
	if _, err := lc.InitAtPath(cfgPath); err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}

	writeConfig(newVersion)
	loadedCfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	paths := initPathsForConfig(cfgPath)
	lock := &Lockfile{
		Version: LockVersion,
		Providers: map[string]LockProviderEntry{
			"roadmap": {
				Fingerprint: mustFingerprint(t, "roadmap", loadedCfg.Plugins["roadmap"], paths.configDir),
				Source:      pluginRef,
				Version:     newVersion,
				Archives: map[string]LockArchive{
					"generic": {URL: archiveServer.URL, SHA256: hex.EncodeToString(newPluginArchiveSum[:])},
				},
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
	const pluginRef = "github.com/testowner/plugins/example"
	const indexedDBRef = "github.com/testowner/indexeddb/main"
	const webUIRef = "github.com/testowner/web/roadmap"
	const version = "0.0.1-alpha.1"

	pluginPkg := mustBuildManagedProviderPackage(t, dir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindPlugin,
		Source:      pluginRef,
		Version:     version,
		DisplayName: "Example Plugin",
		Entrypoint: &providermanifestv1.Entrypoint{
			ArtifactPath: filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "plugin")),
			Args:         []string{"serve-plugin"},
		},
		Spec: &providermanifestv1.Spec{
			Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeNone},
		},
	}, map[string]string{
		filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "plugin")): "plugin-binary",
	}, true)

	indexedDBPkg := mustBuildManagedProviderPackage(t, dir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindIndexedDB,
		Source:      indexedDBRef,
		Version:     version,
		DisplayName: "Main IndexedDB",
		Entrypoint: &providermanifestv1.Entrypoint{
			ArtifactPath: filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "indexeddb")),
			Args:         []string{"serve-indexeddb"},
		},
		Spec: &providermanifestv1.Spec{},
	}, map[string]string{
		filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "indexeddb")): "indexeddb-binary",
	}, false)

	webUIPkg := mustBuildManagedProviderPackage(t, dir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindWebUI,
		Source:      webUIRef,
		Version:     version,
		DisplayName: "Roadmap UI",
		Spec: &providermanifestv1.Spec{
			AssetRoot: "dist",
		},
	}, map[string]string{
		"dist/index.html": "<html>roadmap</html>",
	}, false)

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := fmt.Sprintf(`providers:
  indexeddb:
    main:
      source:
        ref: %s
        version: %s
      config:
        path: %q
  ui:
    roadmap:
      source:
        ref: %s
        version: %s
      path: /roadmap
plugins:
  example:
    source:
      ref: %s
      version: %s
server:
  providers:
    indexeddb: main
  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`, indexedDBRef, version, filepath.Join(dir, "gestalt.db"), webUIRef, version, pluginRef, version)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lc := NewLifecycle(mappedSourceResolver{paths: map[string]string{
		pluginRef:    pluginPkg,
		indexedDBRef: indexedDBPkg,
		webUIRef:     webUIPkg,
	}})
	lock, err := lc.InitAtPath(cfgPath)
	if err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}

	lock.Providers["example"] = LockProviderEntry{
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
	if err := writeJSONFile(lockPath, lock); err != nil {
		t.Fatalf("writeJSONFile: %v", err)
	}

	for _, locked := range []bool{false, true} {
		loaded, _, err := lc.LoadForExecutionAtPath(cfgPath, locked)
		if err != nil {
			t.Fatalf("LoadForExecutionAtPath(locked=%t): %v", locked, err)
		}

		plugin := loaded.Plugins["example"]
		if plugin == nil || plugin.ResolvedManifest == nil {
			t.Fatalf("Plugins[example] = %+v", plugin)
		}
		if got := plugin.Command; strings.Contains(got, "stale/provider/executable") {
			t.Fatalf("plugin.Command = %q, want derived prepared path", got)
		}

		indexedDB := mustSelectedHostProviderEntry(t, loaded, config.HostProviderKindIndexedDB)
		if indexedDB == nil || indexedDB.ResolvedManifest == nil {
			t.Fatalf("SelectedHostProvider(indexeddb) = %+v", indexedDB)
		}
		if got := indexedDB.Command; strings.Contains(got, "stale/indexeddb/executable") {
			t.Fatalf("indexeddb.Command = %q, want derived prepared path", got)
		}

		ui := loaded.Providers.UI["roadmap"]
		if ui == nil || ui.ResolvedManifest == nil {
			t.Fatalf("Providers.UI[roadmap] = %+v", ui)
		}
		if got := ui.ResolvedAssetRoot; strings.Contains(got, "stale/ui/assets") {
			t.Fatalf("ResolvedAssetRoot = %q, want derived prepared path", got)
		}
	}

	rewrittenLock, err := ReadLockfile(lockPath)
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	if got := rewrittenLock.Providers["example"].Manifest; got != "stale/provider/manifest.json" {
		t.Fatalf("lock.Providers[example].Manifest = %q, want stale path preserved", got)
	}
	if got := rewrittenLock.IndexedDBs["main"].Executable; got != "stale/indexeddb/executable" {
		t.Fatalf("lock.IndexedDBs[main].Executable = %q, want stale path preserved", got)
	}
	if got := rewrittenLock.UIs["roadmap"].AssetRoot; got != "stale/ui/assets" {
		t.Fatalf("lock.UIs[roadmap].AssetRoot = %q, want stale path preserved", got)
	}
	rewrittenData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(rewrittenData), "stale/provider/manifest.json") || !strings.Contains(string(rewrittenData), "stale/ui/assets") {
		t.Fatalf("lockfile was unexpectedly rewritten: %s", rewrittenData)
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
		Spec: &providermanifestv1.Spec{
			Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeNone},
			UI: &providermanifestv1.OwnedUIRef{
				Path: "../owned-ui/manifest.json",
			},
		},
	})
	if err != nil {
		t.Fatalf("Marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, providerpkg.ManifestFile), manifestBytes, 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
	pkgPath := filepath.Join(dir, "roadmap-managed-pkg.tar.gz")
	mustCreateLifecycleArchive(t, pkgPath,
		lifecycleArchiveFile{name: providerpkg.ManifestFile, data: manifestBytes, mode: 0o644},
		lifecycleArchiveFile{name: artifactPath, data: artifactContent, mode: 0o755},
	)

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "gestalt.db")) + `plugins:
    roadmap:
      source:
        ref: ` + pluginRef + `
        version: ` + version + `
      mountPath: /create-customer-roadmap-review
server:
` + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lc := NewLifecycle(mappedSourceResolver{paths: map[string]string{pluginRef: pkgPath}})
	if _, err := lc.InitAtPath(cfgPath); err == nil || !strings.Contains(err.Error(), "spec.ui.path must stay within the package") {
		t.Fatalf("InitAtPath error = %v, want substring %q", err, "spec.ui.path must stay within the package")
	}
}

func TestLoadForExecutionAtPath_RejectsDisabledLocalMountedWebUIWithoutLockfile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "gestalt.db")) + `  ui:
    roadmap:
      disabled: true
      source:
        path: ./missing-webui/manifest.yaml
      path: /create-customer-roadmap-review
` + `server:
` + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lc := NewLifecycle(nil)
	_, _, err := lc.LoadForExecutionAtPath(cfgPath, false)
	if err == nil {
		t.Fatal("LoadForExecutionAtPath: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "field disabled not found") {
		t.Fatalf("LoadForExecutionAtPath error = %v, want substring %q", err, "field disabled not found")
	}
}

func TestInitAtPath_RejectsDisabledManagedMountedWebUI(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "gestalt.db")) + `  ui:
    roadmap:
      disabled: true
      source:
        ref: github.com/testowner/web/roadmap
        version: 0.0.1-alpha.1
        auth:
          token:
            secret:
              provider: missing
              name: disabled-webui-token
      path: /create-customer-roadmap-review
` + `server:
` + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lc := NewLifecycle(nil)
	_, err := lc.InitAtPath(cfgPath)
	if err == nil {
		t.Fatal("InitAtPath: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "field disabled not found") {
		t.Fatalf("InitAtPath error = %v, want substring %q", err, "field disabled not found")
	}
}

func TestInitAtPath_RejectsDisabledManagedHostProvidersWithStructuredSecretRefs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	manifestPath := writeStubIndexedDBManifest(t, dir)
	cfg := `providers:
  indexeddb:
    sqlite:
      source:
        path: ` + manifestPath + `
      config:
        path: "` + filepath.Join(dir, "gestalt.db") + `"
    disabled:
      disabled: true
      config:
        path:
          secret:
            provider: missing
            name: disabled-indexeddb-path
  auth:
    disabled:
      disabled: true
      config:
        clientSecret:
          secret:
            provider: missing
            name: disabled-auth-token
  telemetry:
    disabled:
      disabled: true
      config:
        endpoint:
          secret:
            provider: missing
            name: disabled-telemetry-endpoint
  audit:
    disabled:
      disabled: true
      config:
        endpoint:
          secret:
            provider: missing
            name: disabled-audit-endpoint
  secrets:
    disabled:
      disabled: true
      displayName:
        secret:
          provider: missing
          name: disabled-secrets-token
  cache:
    disabled:
      disabled: true
      config:
        password:
          secret:
            provider: missing
            name: disabled-cache-password
` + `server:
` + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lc := NewLifecycle(nil)
	_, err := lc.InitAtPath(cfgPath)
	if err == nil {
		t.Fatal("InitAtPath: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "field disabled not found") {
		t.Fatalf("InitAtPath error = %v, want substring %q", err, "field disabled not found")
	}
}

func TestLoadForExecutionAtPath_RejectsDisabledManagedHostProvidersWithoutLockfile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	manifestPath := writeStubIndexedDBManifest(t, dir)
	cfg := `providers:
  indexeddb:
    sqlite:
      source:
        path: ` + manifestPath + `
      config:
        path: "` + filepath.Join(dir, "gestalt.db") + `"
    disabled:
      disabled: true
      source:
        ref: github.com/testowner/indexeddb/disabled
        version: 0.0.1-alpha.1
      config:
        path:
          secret:
            provider: missing
            name: disabled-indexeddb-path
  auth:
    disabled:
      disabled: true
      source:
        ref: github.com/testowner/auth/disabled
        version: 0.0.1-alpha.1
      config:
        clientSecret:
          secret:
            provider: missing
            name: disabled-auth-token
  telemetry:
    disabled:
      disabled: true
      source:
        ref: github.com/testowner/plugins/telemetry-disabled
        version: 0.0.1-alpha.1
      config:
        endpoint:
          secret:
            provider: missing
            name: disabled-telemetry-endpoint
  audit:
    disabled:
      disabled: true
      source:
        ref: github.com/testowner/plugins/audit-disabled
        version: 0.0.1-alpha.1
      config:
        endpoint:
          secret:
            provider: missing
            name: disabled-audit-endpoint
  secrets:
    disabled:
      disabled: true
      source:
        ref: github.com/testowner/secrets/disabled
        version: 0.0.1-alpha.1
      displayName:
        secret:
          provider: missing
          name: disabled-secrets-token
  cache:
    disabled:
      disabled: true
      source:
        ref: github.com/testowner/cache/disabled
        version: 0.0.1-alpha.1
      config:
        password:
          secret:
            provider: missing
            name: disabled-cache-password
` + `server:
` + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lc := NewLifecycle(nil)
	_, _, err := lc.LoadForExecutionAtPath(cfgPath, false)
	if err == nil {
		t.Fatal("LoadForExecutionAtPath: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "field disabled not found") {
		t.Fatalf("LoadForExecutionAtPath error = %v, want substring %q", err, "field disabled not found")
	}
}

func TestInitAtPath_RejectsPolicyBoundManagedMountedWebUIWithoutExplicitRouteCoverage(t *testing.T) {
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
				Routes: []providermanifestv1.WebUIRoute{
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
				Kind:        providermanifestv1.KindWebUI,
				Source:      "github.com/testowner/web/sample-portal",
				Version:     "0.0.1-alpha.1",
				DisplayName: "Sample Portal",
				Spec:        tc.spec,
			}, map[string]string{
				"dist/index.html": "<html>sample portal</html>",
			}, false)

			cfgPath := filepath.Join(dir, "config.yaml")
			cfg := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "gestalt.db")) + `  ui:
    sample_portal:
      source:
        ref: github.com/testowner/web/sample-portal
        version: 0.0.1-alpha.1
      path: /sample-portal
      authorizationPolicy: sample_policy
authorization:
  policies:
    sample_policy:
      default: deny
      members:
        - email: viewer@example.test
          role: viewer
` + `server:
` + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
			if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
				t.Fatalf("WriteFile config: %v", err)
			}

			lc := NewLifecycle(staticSourceResolver{localPath: pkgPath})
			_, err := lc.InitAtPath(cfgPath)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("InitAtPath error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestLockProviderEntryForSource_RejectsManifestWithoutProviderKind(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pkgPath := mustBuildManagedProviderPackage(t, dir, &providermanifestv1.Manifest{
		Kind:       providermanifestv1.KindAuth,
		Source:     "github.com/testowner/gestalt-providers/plugins/auth-only",
		Version:    "0.0.1-alpha.1",
		Spec:       &providermanifestv1.Spec{},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "auth"))},
	}, map[string]string{
		filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "auth")): "auth-binary",
	}, false)

	cfgPath := filepath.Join(dir, "config.yaml")
	paths := initPathsForConfig(cfgPath)
	lc := NewLifecycle(staticSourceResolver{localPath: pkgPath})
	plugin := &config.ProviderEntry{
		Source: config.ProviderSource{
			Ref:     "github.com/testowner/gestalt-providers/plugins/auth-only",
			Version: "0.0.1-alpha.1",
		},
	}

	_, err := lc.lockProviderEntryForSource(context.Background(), paths, "example", plugin, map[string]any{})
	if err == nil {
		t.Fatal("expected provider kind validation error")
	}
	if !strings.Contains(err.Error(), `manifest has kind "auth", want "plugin"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHashPlatformInEntries_HashesMountedWebUIAndProviderArchives(t *testing.T) {
	t.Parallel()

	archiveBytes := []byte("mounted-web-ui-archive")
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
		UIs: map[string]LockUIEntry{
			"roadmap": {
				Source: "github.com/testowner/web/roadmap",
				Archives: map[string]LockArchive{
					platformKeyGeneric: {URL: srv.URL},
				},
			},
		},
	}

	if err := hashPlatformInEntries(context.Background(), lock, initPaths{}, providerpkg.CurrentPlatformString(), map[string]string{}); err != nil {
		t.Fatalf("hashPlatformInEntries: %v", err)
	}

	got := lock.UIs["roadmap"].Archives[platformKeyGeneric].SHA256
	want := hex.EncodeToString(sum[:])
	if got != want {
		t.Fatalf("webui SHA256 = %q, want %q", got, want)
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
		Kind:    providermanifestv1.KindAuth,
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
	cfg := fmt.Sprintf(`providers:
  auth:
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
    auth: auth
    indexeddb: sqlite
  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`, idbManifestPath, "sqlite://"+dbPath)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lc := NewLifecycle(nil)
	loaded, _, err := lc.LoadForExecutionAtPath(cfgPath, false)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath: %v", err)
	}

	authEntry := mustSelectedHostProviderEntry(t, loaded, config.HostProviderKindAuth)
	if authEntry == nil || authEntry.ResolvedManifest == nil {
		t.Fatalf("auth resolved manifest = %+v", authEntry)
	}
	if authEntry.Command != authExecutablePath {
		t.Fatalf("auth command = %q, want %q", authEntry.Command, authExecutablePath)
	}
	if got := authEntry.Args; len(got) != 1 || got[0] != "serve-auth" {
		t.Fatalf("auth args = %v, want [serve-auth]", got)
	}
	authCfg := decodeNodeMap(t, authEntry.Config)
	if authCfg["command"] != authExecutablePath {
		t.Fatalf("auth config command = %v, want %q", authCfg["command"], authExecutablePath)
	}
	authPluginCfg, ok := authCfg["config"].(map[string]any)
	if !ok || authPluginCfg["clientId"] != "local-auth-client" {
		t.Fatalf("auth nested config = %#v", authCfg["config"])
	}

	if _, err := os.Stat(filepath.Join(dir, InitLockfileName)); !os.IsNotExist(err) {
		t.Fatalf("lockfile should not be created, got err=%v", err)
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
		Kind:    providermanifestv1.KindAuth,
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
	cfg := fmt.Sprintf(`providers:
  auth:
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
    auth: auth
    indexeddb: sqlite
  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`, idbManifestPath, "sqlite://"+dbPath)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lc := NewLifecycle(nil)
	loaded, _, err := lc.LoadForExecutionAtPath(cfgPath, false)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath: %v", err)
	}

	authEntry := mustSelectedHostProviderEntry(t, loaded, config.HostProviderKindAuth)
	if authEntry == nil || authEntry.ResolvedManifest == nil {
		t.Fatalf("auth resolved manifest = %+v", authEntry)
	}
	if authEntry.Command != "" {
		t.Fatalf("auth command = %q, want empty", authEntry.Command)
	}
	authCfg := decodeNodeMap(t, authEntry.Config)
	if authCfg["manifestPath"] != authManifestPath {
		t.Fatalf("auth manifest_path = %v, want %q", authCfg["manifestPath"], authManifestPath)
	}
	if authCfg["command"] != "" {
		t.Fatalf("auth config command = %v, want empty", authCfg["command"])
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
		Kind:        providermanifestv1.KindPlugin, Spec: &providermanifestv1.Spec{
			Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeNone},
		},
	}, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeManifestFormat: %v", err)
	}
	writeTestFile("manifest.yaml", manifest, 0o644)

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "gestalt.db")) + `plugins:
    example:
      source:
        path: ./manifest.yaml
` + `server:
` + requiredServerDatastoreYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	writeTestFile("config.yaml", []byte(cfg), 0o644)

	lc := NewLifecycle(nil)
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
	if _, err := os.Stat(filepath.Join(dir, InitLockfileName)); !os.IsNotExist(err) {
		t.Fatalf("lockfile should not be created, got err=%v", err)
	}
}

func TestLoadForExecutionAtPath_GeneratesStaticCatalogForLocalPythonSourcePlugin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local Python source plugin fixture is POSIX-only")
	}

	dir := t.TempDir()
	python3Path, err := exec.LookPath("python3")
	if err != nil {
		t.Skipf("python3 not found: %v", err)
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
plugin = "provider"
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

	manifest, err := providerpkg.EncodeSourceManifestFormat(&providermanifestv1.Manifest{
		Source:      "github.com/testowner/plugins/local-python-provider",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Generated Local Python Provider",
		Kind:        providermanifestv1.KindPlugin, Spec: &providermanifestv1.Spec{
			Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeNone},
		},
	}, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeManifestFormat: %v", err)
	}
	writeTestFile("manifest.yaml", manifest, 0o644)

	cfg := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "gestalt.db")) + `plugins:
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
	t.Setenv("PATH", t.TempDir())

	lc := NewLifecycle(nil)
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

	if _, err := os.Stat(filepath.Join(dir, InitLockfileName)); !os.IsNotExist(err) {
		t.Fatalf("lockfile should not be created, got err=%v", err)
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
	manifest, err := providerpkg.EncodeSourceManifestFormat(&providermanifestv1.Manifest{
		Source:      "github.com/testowner/plugins/local-provider",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Local Provider",
		Kind:        providermanifestv1.KindPlugin, Spec: &providermanifestv1.Spec{
			Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeNone},
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
	cfg := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "gestalt.db")) + `plugins:
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

	lc := NewLifecycle(nil)
	if err := lc.applyLockedProviders([]string{cfgPath}, "", loaded, false); err != nil {
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
	if err := os.WriteFile(cfgPath, []byte("server:\n  public:\n    port: 8080\n"), 0644); err != nil {
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

func TestLockMatchesConfig_ManagedS3UsesResourceNameFingerprint(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := &config.Config{
		Providers: config.ProvidersConfig{
			S3: map[string]*config.ProviderEntry{
				"assets": {
					Source: config.ProviderSource{
						Ref:     "github.com/testowner/providers/s3",
						Version: "0.0.1-alpha.1",
					},
				},
			},
		},
	}
	paths := initPathsForConfig(cfgPath)
	if err := os.MkdirAll(paths.artifactsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll artifacts: %v", err)
	}
	lockEntry := LockEntry{
		Source:      cfg.Providers.S3["assets"].SourceRef(),
		Version:     cfg.Providers.S3["assets"].SourceVersion(),
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
		t.Fatal("lockMatchesConfig returned false for matching managed S3 lock entry")
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

	plugin := &config.ProviderEntry{
		Source: config.ProviderSource{Ref: "github.com/test-org/test-repo/test-plugin", Version: "1.0.0"},
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
}

func TestProviderFingerprint_ChangesWithName(t *testing.T) {
	t.Parallel()

	plugin := &config.ProviderEntry{
		Source: config.ProviderSource{Ref: "github.com/test-org/test-repo/test-plugin", Version: "1.0.0"},
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
	}
	if !strings.Contains(err.Error(), "unsupported lockfile version") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadLockfile_RejectsLegacyUILockShapeBeforeUnmarshal(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lockPath := filepath.Join(dir, InitLockfileName)
	legacy := `{
  "version": 3,
  "providers": {},
  "ui": {
    "fingerprint": "ui-fp",
    "source": "github.com/test-org/test-repo/test-ui",
    "version": "2.0.0",
    "manifest": ".gestaltd/ui/manifest.json",
    "assetRoot": ".gestaltd/ui/assets"
  }
}
`
	if err := os.WriteFile(lockPath, []byte(legacy), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := ReadLockfile(lockPath)
	if err == nil {
		t.Fatal("expected error for legacy ui lock shape")
	}
	if !strings.Contains(err.Error(), "unsupported lockfile version 3") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadLockfile_AcceptsSchemaV1PortableEntries(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lockPath := filepath.Join(dir, InitLockfileName)
	legacy := `{
  "schema": "gestaltd-provider-lock",
  "schemaVersion": 1,
  "revision": 0,
  "providers": {
    "plugin": {
      "example": {
        "fingerprint": "provider-fp",
        "source": "github.com/test-org/test-repo/test-plugin",
        "version": "1.0.0"
      }
    }
  }
}
`
	if err := os.WriteFile(lockPath, []byte(legacy), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	lock, err := ReadLockfile(lockPath)
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	entry, ok := lock.Providers["example"]
	if !ok {
		t.Fatal(`lock.Providers["example"] not found`)
	}
	if entry.Fingerprint != "provider-fp" {
		t.Fatalf("Fingerprint = %q, want %q", entry.Fingerprint, "provider-fp")
	}
	if entry.Source != "github.com/test-org/test-repo/test-plugin" || entry.Version != "1.0.0" {
		t.Fatalf("entry = %#v", entry)
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
		Providers: map[string]LockProviderEntry{
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
		Auth: map[string]LockEntry{
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
		UIs: map[string]LockUIEntry{
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
	if providerEntry.Kind != providermanifestv1.KindPlugin {
		t.Fatalf("provider kind = %q, want %q", providerEntry.Kind, providermanifestv1.KindPlugin)
	}
	if providerEntry.Runtime != providerLockRuntimeExecutable {
		t.Fatalf("provider runtime = %q, want %q", providerEntry.Runtime, providerLockRuntimeExecutable)
	}
	authEntry, ok := diskLock.Providers.Auth["oauth"]
	if !ok {
		t.Fatal(`disk lock providers.auth["oauth"] not found`)
	}
	if authEntry.InputDigest != want.Auth["oauth"].Fingerprint {
		t.Fatalf("auth inputDigest = %q, want %q", authEntry.InputDigest, want.Auth["oauth"].Fingerprint)
	}
	if authEntry.Package != want.Auth["oauth"].Source {
		t.Fatalf("auth package = %q, want %q", authEntry.Package, want.Auth["oauth"].Source)
	}
	if authEntry.Kind != providermanifestv1.KindAuth {
		t.Fatalf("auth kind = %q, want %q", authEntry.Kind, providermanifestv1.KindAuth)
	}
	if authEntry.Runtime != providerLockRuntimeExecutable {
		t.Fatalf("auth runtime = %q, want %q", authEntry.Runtime, providerLockRuntimeExecutable)
	}
	uiEntry, ok := diskLock.Providers.WebUI["roadmap"]
	if !ok {
		t.Fatal(`disk lock providers.webui["roadmap"] not found`)
	}
	if uiEntry.InputDigest != want.UIs["roadmap"].Fingerprint {
		t.Fatalf("ui inputDigest = %q, want %q", uiEntry.InputDigest, want.UIs["roadmap"].Fingerprint)
	}
	if uiEntry.Kind != providermanifestv1.KindWebUI {
		t.Fatalf("ui kind = %q, want %q", uiEntry.Kind, providermanifestv1.KindWebUI)
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
	if got.Auth["oauth"].Fingerprint != want.Auth["oauth"].Fingerprint {
		t.Fatal("auth fingerprint mismatch")
	}
	if got.IndexedDBs["main"].Fingerprint != want.IndexedDBs["main"].Fingerprint {
		t.Fatal("indexeddb fingerprint mismatch")
	}
	if got.IndexedDBs["archive"].Executable != "" {
		t.Fatal("indexeddb executable should not round-trip from portable lock schema")
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

	if err := hashArchiveEntry(context.Background(), providermanifestv1.KindWebUI, "roadmap", &entry, initPaths{}, "linux/amd64", nil); err != nil {
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
