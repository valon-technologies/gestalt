package operator

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/pluginpkg"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

func TestLoadForExecutionAtPath_ResolvesLocalManifestPluginWithoutLockfile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "plugin.yaml")
	manifest, err := pluginpkg.EncodeManifest(&pluginmanifestv1.Manifest{
		Source:      "github.com/testowner/plugins/local-provider",
		Version:     "0.1.0",
		DisplayName: "Local Provider",
		Description: "Local manifest-backed provider",
		Kinds:       []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			Auth:    &pluginmanifestv1.ProviderAuth{Type: pluginmanifestv1.AuthTypeNone},
			BaseURL: "https://example.com",
			Operations: []pluginmanifestv1.ProviderOperation{
				{
					Name:   "ping",
					Method: "GET",
					Path:   "/ping",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `auth:
  provider: none
datastore:
  provider: sqlite
  config:
    path: ./gestalt.db
providers:
  example:
    from:
      source:
        path: ./plugin.yaml
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lc := NewLifecycle(nil)
	loaded, _, err := lc.LoadForExecutionAtPath(cfgPath, false)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath: %v", err)
	}

	intg := loaded.Integrations["example"]
	if intg.DisplayName != "Local Provider" {
		t.Fatalf("DisplayName = %q", intg.DisplayName)
	}
	if intg.Description != "Local manifest-backed provider" {
		t.Fatalf("Description = %q", intg.Description)
	}
	if intg.Plugin == nil || intg.Plugin.ResolvedManifest == nil {
		t.Fatalf("ResolvedManifest = %+v", intg.Plugin)
	}
	if intg.Plugin.ResolvedManifestPath != manifestPath {
		t.Fatalf("ResolvedManifestPath = %q, want %q", intg.Plugin.ResolvedManifestPath, manifestPath)
	}
	if !intg.Plugin.IsDeclarative {
		t.Fatal("expected manifest-backed source plugin to resolve as declarative")
	}
	if _, err := os.Stat(filepath.Join(dir, InitLockfileName)); !os.IsNotExist(err) {
		t.Fatalf("lockfile should not be created, got err=%v", err)
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
	manifest, err := pluginpkg.EncodeSourceManifestFormat(&pluginmanifestv1.Manifest{
		Source:      "github.com/testowner/plugins/local-generated-provider",
		Version:     "0.1.0",
		DisplayName: "Generated Local Provider",
		Kinds:       []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			Auth: &pluginmanifestv1.ProviderAuth{Type: pluginmanifestv1.AuthTypeNone},
		},
	}, pluginpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeManifestFormat: %v", err)
	}
	writeTestFile("plugin.yaml", manifest, 0o644)

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `auth:
  provider: none
datastore:
  provider: sqlite
  config:
    path: ./gestalt.db
providers:
  example:
    from:
      source:
        path: ./plugin.yaml
`
	writeTestFile("config.yaml", []byte(cfg), 0o644)

	lc := NewLifecycle(nil)
	loaded, _, err := lc.LoadForExecutionAtPath(cfgPath, false)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath: %v", err)
	}

	intg := loaded.Integrations["example"]
	if intg.Plugin == nil || intg.Plugin.ResolvedManifest == nil {
		t.Fatalf("ResolvedManifest = %+v", intg.Plugin)
	}
	if intg.Plugin.IsDeclarative {
		t.Fatal("expected executable manifest-backed plugin to remain non-declarative")
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
dependencies = ["gestalt"]

[tool.gestalt]
plugin = "provider:plugin"
`, "\n")), 0o644)
	writeTestFile("provider.py", []byte(`from __future__ import annotations

from typing import Optional

import gestalt

plugin = gestalt.Plugin.from_manifest("plugin.yaml")


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


@plugin.operation(id="echo", method="POST")
def echo(input: EchoInput, _req: gestalt.Request) -> dict[str, object]:
    return {
        "names": input.names or [],
        "metadata": input.metadata or {},
        "filters_type": type(input.filters).__name__ if input.filters else "",
        "owner": input.filters.owner if input.filters else "",
        "limit_type": type(input.limit).__name__,
        "limit": input.limit,
    }


@plugin.operation(id="double", method="POST")
def double(value: int, _req: gestalt.Request) -> dict[str, object]:
    return {
        "value_type": type(value).__name__,
        "value": value * 2,
    }


@plugin.operation(id="maybe_filters", method="POST")
def maybe_filters(input: Optional[Filters], _req: gestalt.Request) -> dict[str, object]:
    return {
        "filters_type": type(input).__name__ if input else "",
        "owner": input.owner if input else "",
    }


@plugin.operation(id="list_items", method="GET", read_only=True)
def list_items(_req: gestalt.Request) -> dict[str, object]:
    return {
        "items": [Item(name="Ada"), Item(name="Grace")],
        "groups": {"staff": [Item(name="Linus")]},
    }


@plugin.operation(id="status_zero", method="POST")
def status_zero(_req: gestalt.Request) -> gestalt.Response[dict[str, bool]]:
    return gestalt.Response(status=0, body={"ok": True})
`), 0o644)
	if err := installLocalPythonSDK(t, python3Path, filepath.Join(dir, ".venv")); err != nil {
		t.Fatalf("installLocalPythonSDK: %v", err)
	}

	manifest, err := pluginpkg.EncodeSourceManifestFormat(&pluginmanifestv1.Manifest{
		Source:      "github.com/testowner/plugins/local-python-provider",
		Version:     "0.1.0",
		DisplayName: "Generated Local Python Provider",
		Kinds:       []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			Auth:    &pluginmanifestv1.ProviderAuth{Type: pluginmanifestv1.AuthTypeNone},
			BaseURL: "https://example.com",
			Operations: []pluginmanifestv1.ProviderOperation{
				{Name: "ping", Method: "GET", Path: "/ping"},
			},
		},
	}, pluginpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeManifestFormat: %v", err)
	}
	writeTestFile("plugin.yaml", manifest, 0o644)

	cfg := `auth:
  provider: none
datastore:
  provider: sqlite
  config:
    path: ./gestalt.db
providers:
  example:
    from:
      source:
        path: ./plugin.yaml
`
	writeTestFile("config.yaml", []byte(cfg), 0o644)
	writeTestFile("exercise.py", []byte(`from __future__ import annotations

import json

import gestalt
import provider

status, body = provider.plugin.execute("echo", {
    "names": ["Ada", "Grace"],
    "metadata": {"role": "admin"},
    "filters": {"owner": "Ada"},
    "limit": 3,
}, gestalt.Request())
double_status, double_body = provider.plugin.execute("double", {
    "value": 3,
}, gestalt.Request())
zero_status, zero_body = provider.plugin.execute("status_zero", {}, gestalt.Request())
maybe_status, maybe_body = provider.plugin.execute("maybe_filters", {
    "owner": "Grace",
}, gestalt.Request())
list_status, list_body = provider.plugin.execute("list_items", {}, gestalt.Request())
print(json.dumps({
    "status": status,
    "body": json.loads(body),
    "double_status": double_status,
    "double_body": json.loads(double_body),
    "list_status": list_status,
    "list_body": json.loads(list_body),
    "maybe_status": maybe_status,
    "maybe_body": json.loads(maybe_body),
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

	intg := loaded.Integrations["example"]
	if intg.Plugin == nil || intg.Plugin.ResolvedManifest == nil {
		t.Fatalf("ResolvedManifest = %+v", intg.Plugin)
	}
	if intg.Plugin.IsDeclarative {
		t.Fatal("expected executable Python source plugin to remain non-declarative")
	}
	catalogData, err := os.ReadFile(filepath.Join(dir, "catalog.yaml"))
	if err != nil {
		t.Fatalf("ReadFile(catalog.yaml): %v", err)
	}
	catalogText := string(catalogData)
	if !strings.Contains(catalogText, `id: "echo"`) {
		t.Fatalf("unexpected catalog contents: %s", catalogData)
	}
	if strings.Contains(catalogText, "\n\n") {
		t.Fatalf("catalog contains unexpected blank lines: %q", catalogText)
	}
	arrayParam := regexp.MustCompile(`(?m)- name: "names"\n\s+type: "array"$`)
	if !arrayParam.MatchString(catalogText) {
		t.Fatalf("catalog missing array parameter type: %s", catalogText)
	}
	objectParam := regexp.MustCompile(`(?m)- name: "metadata"\n\s+type: "object"$`)
	if !objectParam.MatchString(catalogText) {
		t.Fatalf("catalog missing object parameter type: %s", catalogText)
	}
	namesDefault := regexp.MustCompile(`(?m)- name: "names"\n\s+type: "array"\n\s+default: null$`)
	if !namesDefault.MatchString(catalogText) {
		t.Fatalf("catalog missing null default for optional array: %s", catalogText)
	}
	filtersParam := regexp.MustCompile(`(?m)- name: "filters"\n\s+type: "object"$`)
	if !filtersParam.MatchString(catalogText) {
		t.Fatalf("catalog missing nested object parameter type: %s", catalogText)
	}
	optionalModelParams := regexp.MustCompile(`(?s)- id: "maybe_filters".*?- name: "owner"\n\s+type: "string"\n\s+default: ""`)
	if !optionalModelParams.MatchString(catalogText) {
		t.Fatalf("catalog missing parameters for Optional model input: %s", catalogText)
	}
	limitParam := regexp.MustCompile(`(?m)- name: "limit"\n\s+type: "integer"$`)
	if !limitParam.MatchString(catalogText) {
		t.Fatalf("catalog missing integer parameter type: %s", catalogText)
	}
	emptyStringDefault := regexp.MustCompile(`(?m)- name: "prefix"\n\s+type: "string"\n\s+default: ""$`)
	if !emptyStringDefault.MatchString(catalogText) {
		t.Fatalf("catalog missing empty string default: %s", catalogText)
	}

	command := filepath.Join(dir, ".venv", "bin", "python")
	cmd := exec.Command(command, "exercise.py")
	cmd.Dir = dir
	result, err := cmd.CombinedOutput()
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
	if body["zero_status"] != float64(0) {
		t.Fatalf("zero_status = %v, want 0", body["zero_status"])
	}

	if _, err := os.Stat(filepath.Join(dir, InitLockfileName)); !os.IsNotExist(err) {
		t.Fatalf("lockfile should not be created, got err=%v", err)
	}
}

func localPythonSDKSourceDir(t *testing.T) string {
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

func installLocalPythonSDK(t *testing.T, python3Path, venvDir string) error {
	t.Helper()

	create := exec.Command(python3Path, "-m", "venv", venvDir)
	if output, err := create.CombinedOutput(); err != nil {
		return fmt.Errorf("create venv: %w\n%s", err, output)
	}

	venvPython := filepath.Join(venvDir, "bin", "python")
	pipPath := filepath.Join(venvDir, "bin", "pip")
	if _, err := os.Stat(pipPath); err != nil {
		ensurePip := exec.Command(venvPython, "-m", "ensurepip", "--upgrade")
		if output, ensureErr := ensurePip.CombinedOutput(); ensureErr != nil {
			return fmt.Errorf("ensure pip: %w\n%s", ensureErr, output)
		}
	}

	install := exec.Command(pipPath, "install", "--no-deps", localPythonSDKSourceDir(t))
	install.Env = append(os.Environ(), "PIP_DISABLE_PIP_VERSION_CHECK=1")
	if output, err := install.CombinedOutput(); err != nil {
		return fmt.Errorf("install local gestalt package: %w\n%s", err, output)
	}
	return nil
}

func TestApplyLockedPlugins_SkipsNilIntegrationPlugins(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "plugin.yaml")
	manifest, err := pluginpkg.EncodeManifest(&pluginmanifestv1.Manifest{
		Source:      "github.com/testowner/plugins/local-provider",
		Version:     "0.1.0",
		DisplayName: "Local Provider",
		Kinds:       []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			Auth:    &pluginmanifestv1.ProviderAuth{Type: pluginmanifestv1.AuthTypeNone},
			BaseURL: "https://example.com",
			Operations: []pluginmanifestv1.ProviderOperation{
				{Name: "ping", Method: "GET", Path: "/ping"},
			},
		},
	})
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `auth:
  provider: none
datastore:
  provider: sqlite
  config:
    path: ./gestalt.db
providers:
  example:
    from:
      source:
        path: ./plugin.yaml
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load config: %v", err)
	}
	loaded.Integrations["missing"] = config.IntegrationDef{}

	lc := NewLifecycle(nil)
	if err := lc.applyLockedPlugins(cfgPath, "", loaded, false); err != nil {
		t.Fatalf("applyLockedPlugins: %v", err)
	}
	if loaded.Integrations["example"].Plugin == nil || loaded.Integrations["example"].Plugin.ResolvedManifest == nil {
		t.Fatalf("ResolvedManifest = %+v", loaded.Integrations["example"].Plugin)
	}
}

func TestLockMatchesConfig_FalseWithNilLock(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("server:\n  port: 8080\n"), 0644); err != nil {
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

func TestPluginFingerprint_Stable(t *testing.T) {
	t.Parallel()

	plugin := &config.PluginDef{
		Source: &config.PluginSourceDef{Ref: "github.com/test-org/test-repo/test-plugin", Version: "1.0.0"},
	}
	first, err := PluginFingerprint("example", plugin, ".")
	if err != nil {
		t.Fatalf("PluginFingerprint: %v", err)
	}
	second, err := PluginFingerprint("example", plugin, ".")
	if err != nil {
		t.Fatalf("PluginFingerprint: %v", err)
	}
	if first != second {
		t.Fatalf("fingerprint not stable: %q != %q", first, second)
	}
}

func TestPluginFingerprint_ChangesWithName(t *testing.T) {
	t.Parallel()

	plugin := &config.PluginDef{
		Source: &config.PluginSourceDef{Ref: "github.com/test-org/test-repo/test-plugin", Version: "1.0.0"},
	}
	first, err := PluginFingerprint("alpha", plugin, ".")
	if err != nil {
		t.Fatalf("PluginFingerprint: %v", err)
	}
	second, err := PluginFingerprint("beta", plugin, ".")
	if err != nil {
		t.Fatalf("PluginFingerprint: %v", err)
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

func TestReadWriteLockfile_RoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lockPath := filepath.Join(dir, InitLockfileName)
	want := &Lockfile{
		Version: LockVersion,
		Providers: map[string]LockProviderEntry{
			"example": {
				Fingerprint:   "provider-fp",
				Source:        "github.com/test-org/test-repo/test-plugin",
				Version:       "1.0.0",
				ResolvedURL:   "https://example.com/example.tar.gz",
				ArchiveSHA256: "abc123",
				Manifest:      ".gestaltd/providers/example/plugin.json",
				Executable:    ".gestaltd/providers/example/artifacts/darwin/arm64/provider",
			},
		},
		UI: &LockUIEntry{
			Fingerprint:   "ui-fp",
			Source:        "github.com/test-org/test-repo/test-ui",
			Version:       "2.0.0",
			ResolvedURL:   "https://example.com/ui.tar.gz",
			ArchiveSHA256: "def456",
			Manifest:      ".gestaltd/ui/plugin.json",
			AssetRoot:     ".gestaltd/ui/assets",
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
