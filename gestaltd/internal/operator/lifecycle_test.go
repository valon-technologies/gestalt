package operator

import (
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
  indexeddbs:
    sqlite:
      source:
        path: %s
      config:
        path: %q
  ui:
    disabled: true
`, manifestPath, dbPath)
}

func requiredServerDatastoreYAML() string {
	return `  indexeddb: sqlite
`
}

func requiredIndexedDBConfigYAML(t *testing.T, dir, dbPath string) string {
	return requiredComponentConfigYAML(t, dir, dbPath)
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
	cfg := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "gestalt.db")) + `  plugins:
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

	intg := loaded.Providers.Plugins["example"]
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
	cfg := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "gestalt.db")) + `  plugins:
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

	intg := loaded.Providers.Plugins["notion"]
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
    source:
      path: ./auth-manifest.yaml
    config:
      clientId: local-auth-client
  indexeddbs:
    sqlite:
      source:
        path: %s
      config:
        dsn: %q
  ui:
    disabled: true
server:
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

	if loaded.Providers.Auth == nil || loaded.Providers.Auth.ResolvedManifest == nil {
		t.Fatalf("auth resolved manifest = %+v", loaded.Providers.Auth)
	}
	if loaded.Providers.Auth.Command != authExecutablePath {
		t.Fatalf("auth command = %q, want %q", loaded.Providers.Auth.Command, authExecutablePath)
	}
	if got := loaded.Providers.Auth.Args; len(got) != 1 || got[0] != "serve-auth" {
		t.Fatalf("auth args = %v, want [serve-auth]", got)
	}
	authCfg := decodeNodeMap(t, loaded.Providers.Auth.Config)
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
    source:
      path: ./auth-manifest.yaml
  indexeddbs:
    sqlite:
      source:
        path: %s
      config:
        dsn: %q
  ui:
    disabled: true
server:
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

	if loaded.Providers.Auth == nil || loaded.Providers.Auth.ResolvedManifest == nil {
		t.Fatalf("auth resolved manifest = %+v", loaded.Providers.Auth)
	}
	if loaded.Providers.Auth.Command != "" {
		t.Fatalf("auth command = %q, want empty", loaded.Providers.Auth.Command)
	}
	authCfg := decodeNodeMap(t, loaded.Providers.Auth.Config)
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
	cfg := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "gestalt.db")) + `  plugins:
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

	intg := loaded.Providers.Plugins["example"]
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

	cfg := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "gestalt.db")) + `  plugins:
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

	intg := loaded.Providers.Plugins["example"]
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
	if explodePayload["error"] != "boom" {
		t.Fatalf("explode error = %v, want boom", explodePayload["error"])
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
	cfg := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "gestalt.db")) + `  plugins:
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
	loaded.Providers.Plugins["missing"] = &config.ProviderEntry{}

	lc := NewLifecycle(nil)
	if err := lc.applyLockedProviders(cfgPath, "", loaded, false); err != nil {
		t.Fatalf("applyLockedProviders: %v", err)
	}
	if loaded.Providers.Plugins["example"] == nil || loaded.Providers.Plugins["example"].ResolvedManifest == nil {
		t.Fatalf("ResolvedManifest = %+v", loaded.Providers.Plugins["example"])
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
	lock := &Lockfile{
		Version:   999,
		Providers: make(map[string]LockProviderEntry),
	}
	if err := WriteLockfile(lockPath, lock); err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}

	_, err := ReadLockfile(lockPath)
	if err == nil {
		t.Fatal("expected error for unsupported lockfile version")
	}
	if !strings.Contains(err.Error(), "unsupported lockfile version") {
		t.Fatalf("unexpected error: %v", err)
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
		UI: &LockUIEntry{
			Fingerprint: "ui-fp",
			Source:      "github.com/test-org/test-repo/test-ui",
			Version:     "2.0.0",
			Archives: map[string]LockArchive{
				"generic": {URL: "https://example.com/ui.tar.gz", SHA256: "def456"},
			},
			Manifest:  ".gestaltd/ui/manifest.json",
			AssetRoot: ".gestaltd/ui/assets",
		},
	}
	if err := WriteLockfile(lockPath, want); err != nil {
		t.Fatalf("WriteLockfile: %v", err)
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
	if got.UI == nil || got.UI.Source != want.UI.Source || got.UI.Version != want.UI.Version {
		t.Fatal("ui lock entry mismatch")
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

	if err := hashArchiveEntry(context.Background(), &entry, "linux/amd64", nil); err != nil {
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
