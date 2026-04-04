package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/operator"
	"github.com/valon-technologies/gestalt/server/internal/pluginpkg"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
	"gopkg.in/yaml.v3"
)

func TestE2EInitArchiveAndValidate(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := setupPluginDir(t, dir)
	archivePath := filepath.Join(dir, "plugin.tar.gz")

	out, err := exec.Command(gestaltdBin, "plugin", "package", "--input", pluginDir, "--output", archivePath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd plugin package: %v\n%s", err, out)
	}

	cfgPath := writeE2EConfig(t, dir, "plugin.tar.gz", 0)
	out, err = exec.Command(gestaltdBin, "init", "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd init: %v\n%s", err, out)
	}

	lockPath := filepath.Join(dir, operator.InitLockfileName)
	lock, err := operator.ReadLockfile(lockPath)
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	entry, ok := lock.Plugins[operator.LockPluginKey("integration", "example")]
	if !ok {
		t.Fatalf("lockfile missing plugin entry: %+v", lock.Plugins)
	}
	if entry.SourceDigest == "" {
		t.Fatal("expected non-empty SourceDigest for archive package")
	}
	if entry.Package == "" {
		t.Fatal("expected non-empty Package in lockfile entry")
	}

	out, err = exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd validate: %v\n%s", err, out)
	}
}

func TestE2EValidateRejectsInvalidInlineConnectionConfigs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		pluginYAML string
		wantError  string
	}{
		{
			name: "openapi requires explicit surface connection",
			pluginYAML: `connections:
  workspace:
    auth:
      type: manual
surfaces:
  openapi:
    document: https://api.example.test/openapi.json`,
			wantError: "surfaces.openapi.connection is required when using named connections without connections.default",
		},
		{
			name: "graphql requires explicit surface connection",
			pluginYAML: `connections:
  workspace:
    auth:
      type: manual
surfaces:
  graphql:
    url: https://api.example.test/graphql`,
			wantError: "surfaces.graphql.connection is required when using named connections without connections.default",
		},
		{
			name: "default connection reference must exist",
			pluginYAML: `connections:
  workspace:
    auth:
      type: manual
surfaces:
  rest:
    connection: missing
    base_url: https://api.example.test
    operations:
      - name: list_items
        method: GET
        path: /items`,
			wantError: "surfaces.rest.connection references undeclared connection",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "config.yaml")
			cfg := fmt.Sprintf(`auth:
  provider: none
datastore:
  provider: sqlite
  config:
    path: %s
server:
  port: 18080
  encryption_key: test-e2e-key
providers:
  example:
    %s
`, filepath.Join(dir, "gestalt.db"), strings.ReplaceAll(tc.pluginYAML, "\n", "\n    "))

			if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
				t.Fatalf("write config: %v", err)
			}

			out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
			if err == nil {
				t.Fatalf("expected gestaltd validate to fail, got success\n%s", out)
			}
			if !strings.Contains(string(out), tc.wantError) {
				t.Fatalf("expected %q, got: %s", tc.wantError, out)
			}
		})
	}
}

func TestE2EInitDirectoryPackage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := setupPluginDir(t, dir)
	cfgPath := writeE2EConfigForDir(t, dir, pluginDir)

	out, err := exec.Command(gestaltdBin, "init", "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd init: %v\n%s", err, out)
	}

	lockPath := filepath.Join(dir, operator.InitLockfileName)
	lock, err := operator.ReadLockfile(lockPath)
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	entry, ok := lock.Plugins[operator.LockPluginKey("integration", "example")]
	if !ok {
		t.Fatalf("lockfile missing plugin entry: %+v", lock.Plugins)
	}
	if entry.SourceDigest == "" {
		t.Fatal("expected non-empty SourceDigest for directory package")
	}
}

//nolint:paralleltest // Uses a process-wide HTTP transport override so the TLS package fetch trusts the test server.
func TestE2EInitHTTPSPackage(t *testing.T) {
	dir := t.TempDir()
	pluginDir := setupPluginDir(t, dir)
	archivePath := filepath.Join(dir, "plugin.tar.gz")
	if err := pluginpkg.CreatePackageFromDir(pluginDir, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir: %v", err)
	}

	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, archivePath)
	}))
	defer ts.Close()

	savedTransport := http.DefaultTransport
	http.DefaultTransport = ts.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = savedTransport })

	cfgPath := writeE2EConfig(t, dir, ts.URL+"/plugin.tar.gz", 0)
	if err := run([]string{"init", "--config", cfgPath}); err != nil {
		t.Fatalf("run init: %v", err)
	}

	lockPath := filepath.Join(dir, operator.InitLockfileName)
	lock, err := operator.ReadLockfile(lockPath)
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	entry, ok := lock.Plugins[operator.LockPluginKey("integration", "example")]
	if !ok {
		t.Fatalf("lockfile missing plugin entry: %+v", lock.Plugins)
	}
	if entry.SourceDigest != "" {
		t.Fatalf("expected empty SourceDigest for HTTPS package, got %q", entry.SourceDigest)
	}
}

func TestE2EInitServeLockedGoldenPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	deployDir := filepath.Join(dir, "deploy")
	dataDir := filepath.Join(dir, "data")
	artifactsDir := filepath.Join(dir, "runtime-artifacts")
	if err := os.MkdirAll(deployDir, 0o755); err != nil {
		t.Fatalf("MkdirAll deploy dir: %v", err)
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("MkdirAll data dir: %v", err)
	}

	pluginDir := setupPluginDir(t, dir)
	archivePath := filepath.Join(deployDir, "plugin.tar.gz")

	out, err := exec.Command(gestaltdBin, "plugin", "package", "--input", pluginDir, "--output", archivePath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd plugin package: %v\n%s", err, out)
	}

	port := allocateTestPort(t)
	cfgPath := writeE2EConfigWithPaths(t, deployDir, "plugin.tar.gz", filepath.Join(dataDir, "gestalt.db"), artifactsDir, port)
	cfgBytes, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	cfgBytes = append(cfgBytes, []byte(`telemetry:
  provider: stdout
  config:
    level: warn
    format: json
`)...)
	if err := os.WriteFile(cfgPath, cfgBytes, 0o644); err != nil {
		t.Fatalf("write config telemetry: %v", err)
	}

	out, err = exec.Command(gestaltdBin, "init", "--config", cfgPath, "--artifacts-dir", artifactsDir).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd init: %v\n%s", err, out)
	}

	lockPath := filepath.Join(deployDir, operator.InitLockfileName)
	t.Cleanup(func() {
		_ = os.Chmod(deployDir, 0o755)
		_ = os.Chmod(cfgPath, 0o644)
		_ = os.Chmod(lockPath, 0o644)
	})
	if err := os.Chmod(cfgPath, 0o444); err != nil {
		t.Fatalf("Chmod config: %v", err)
	}
	if err := os.Chmod(lockPath, 0o444); err != nil {
		t.Fatalf("Chmod lockfile: %v", err)
	}
	if err := os.Chmod(deployDir, 0o555); err != nil {
		t.Fatalf("Chmod deploy dir: %v", err)
	}

	stdout, stderr := serveLockedAndInvokeExampleEcho(t, cfgPath, port, artifactsDir)

	var foundAudit bool
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			continue
		}
		if record["msg"] != "audit" {
			continue
		}

		foundAudit = true
		if record["level"] != "INFO" {
			t.Fatalf("expected audit log level=INFO, got %v\nstdout:\n%s\nstderr:\n%s", record["level"], stdout, stderr)
		}
		if record["log.type"] != "audit" {
			t.Fatalf("expected audit log.type=audit, got %v\nstdout:\n%s\nstderr:\n%s", record["log.type"], stdout, stderr)
		}
		if record["provider"] != "example" {
			t.Fatalf("expected audit provider=example, got %v\nstdout:\n%s\nstderr:\n%s", record["provider"], stdout, stderr)
		}
		if record["operation"] != "echo" {
			t.Fatalf("expected audit operation=echo, got %v\nstdout:\n%s\nstderr:\n%s", record["operation"], stdout, stderr)
		}
		if record["allowed"] != true {
			t.Fatalf("expected audit allowed=true, got %v\nstdout:\n%s\nstderr:\n%s", record["allowed"], stdout, stderr)
		}
		break
	}

	if !foundAudit {
		t.Fatalf("expected audit log in stdout\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestE2EInitServeLockedOTLPExportsTracesAndMetricsButKeepsLogsOnStdout(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := setupPluginDir(t, dir)
	archivePath := filepath.Join(dir, "plugin.tar.gz")
	if err := pluginpkg.CreatePackageFromDir(pluginDir, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir: %v", err)
	}

	var logRequests, traceRequests, metricRequests atomic.Int32
	var metricBodiesMu sync.Mutex
	var metricBodies [][]byte
	otlpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()

		switch r.URL.Path {
		case "/v1/logs":
			logRequests.Add(1)
		case "/v1/traces":
			traceRequests.Add(1)
		case "/v1/metrics":
			metricRequests.Add(1)
			metricBodiesMu.Lock()
			metricBodies = append(metricBodies, bytes.Clone(body))
			metricBodiesMu.Unlock()
		}

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(otlpServer.Close)

	port := allocateTestPort(t)
	cfgPath := writeE2EConfig(t, dir, "plugin.tar.gz", port)
	cfgBytes, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	cfgBytes = append(cfgBytes, []byte(`telemetry:
  provider: otlp
  config:
    endpoint: `+strings.TrimPrefix(otlpServer.URL, "http://")+`
    protocol: http
    insecure: true
    traces:
      sampling_ratio: 1.0
    metrics:
      interval: 50ms
    logs:
      exporter: stdout
      format: json
      level: info
`)...)
	if err := os.WriteFile(cfgPath, cfgBytes, 0o644); err != nil {
		t.Fatalf("write config telemetry: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "init", "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd init: %v\n%s", err, out)
	}

	stdout, stderr := serveLockedAndExerciseExample(t, cfgPath, port, "", func(t *testing.T, baseURL string) {
		body := invokeExampleOperation(t, baseURL, "echo", `{"message":"hello"}`, http.StatusOK)

		var result map[string]any
		if err := json.Unmarshal(body, &result); err != nil {
			t.Fatalf("unmarshal success response: %v\nbody: %s", err, body)
		}
		if result["echo"] != "hello" {
			t.Fatalf("expected echo=hello, got %v", result)
		}

		invokeExampleOperation(t, baseURL, "nope", `{}`, http.StatusNotFound)

		promBody := getEndpointBody(t, baseURL+"/metrics", http.StatusOK)
		if !bytes.Contains(promBody, []byte("gestaltd_operation_count_total")) {
			t.Fatalf("expected prometheus counter in /metrics body: %s", promBody)
		}
		if !bytes.Contains(promBody, []byte("gestaltd_operation_duration_seconds_bucket")) {
			t.Fatalf("expected prometheus histogram in /metrics body: %s", promBody)
		}

		adminBody := getEndpointBody(t, baseURL+"/admin", http.StatusOK)
		if !bytes.Contains(adminBody, []byte("Prometheus metrics")) {
			t.Fatalf("expected embedded admin UI at /admin: %s", adminBody)
		}
		if !bytes.Contains(adminBody, []byte(`fetch("/metrics"`)) {
			t.Fatalf("expected admin ui to read /metrics directly: %s", adminBody)
		}
	})

	if traceRequests.Load() == 0 {
		t.Fatalf("expected OTLP trace export\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if metricRequests.Load() == 0 {
		t.Fatalf("expected OTLP metric export\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if logRequests.Load() != 0 {
		t.Fatalf("expected logs to stay on stdout, saw %d OTLP log exports\nstdout:\n%s\nstderr:\n%s", logRequests.Load(), stdout, stderr)
	}
	metricBodiesMu.Lock()
	exports := append([][]byte(nil), metricBodies...)
	metricBodiesMu.Unlock()
	requireMetricPayload(t, exports, stdout, stderr, "gestaltd.operation.count")
	requireMetricPayload(t, exports, stdout, stderr, "gestaltd.operation.duration")
	requireMetricPayload(t, exports, stdout, stderr, "gestaltd.operation.error_count")
	requireMetricPayload(t, exports, stdout, stderr,
		"gestaltd.operation.count",
		"gestalt.connection_mode",
		"none",
		"gestalt.operation",
		"echo",
		"gestalt.provider",
		"example",
		"gestalt.transport",
		"plugin",
	)
	requireMetricPayload(t, exports, stdout, stderr,
		"gestaltd.operation.error_count",
		"gestalt.connection_mode",
		"none",
		"gestalt.operation",
		"unknown",
		"gestalt.provider",
		"example",
		"gestalt.transport",
		"unknown",
	)
	if !strings.Contains(stdout, `"msg":"audit"`) {
		t.Fatalf("expected audit log in stdout\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestE2EInitServeLockedStdoutExposesPrometheusAndEmbeddedAdminUIByDefault(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := setupPluginDir(t, dir)
	archivePath := filepath.Join(dir, "plugin.tar.gz")
	if err := pluginpkg.CreatePackageFromDir(pluginDir, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir: %v", err)
	}

	port := allocateTestPort(t)
	cfgPath := writeE2EConfig(t, dir, "plugin.tar.gz", port)
	cfgBytes, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	cfgBytes = append(cfgBytes, []byte(`telemetry:
  provider: stdout
  config:
    level: info
    format: json
`)...)
	if err := os.WriteFile(cfgPath, cfgBytes, 0o644); err != nil {
		t.Fatalf("write config telemetry: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "init", "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd init: %v\n%s", err, out)
	}

	stdout, stderr := serveLockedAndExerciseExample(t, cfgPath, port, "", func(t *testing.T, baseURL string) {
		invokeExampleOperation(t, baseURL, "echo", `{"message":"hello"}`, http.StatusOK)
		invokeExampleOperation(t, baseURL, "nope", `{}`, http.StatusNotFound)

		promBody := getEndpointBody(t, baseURL+"/metrics", http.StatusOK)
		if !bytes.Contains(promBody, []byte("gestaltd_operation_count_total")) {
			t.Fatalf("expected prometheus counter in /metrics body: %s", promBody)
		}
		if !bytes.Contains(promBody, []byte(`gestalt_provider="example"`)) {
			t.Fatalf("expected provider label in /metrics body: %s", promBody)
		}

		adminBody := getEndpointBody(t, baseURL+"/admin", http.StatusOK)
		if !bytes.Contains(adminBody, []byte("Prometheus metrics")) {
			t.Fatalf("expected embedded admin UI at /admin: %s", adminBody)
		}
	})

	if !strings.Contains(stdout, `"msg":"audit"`) {
		t.Fatalf("expected audit log in stdout\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestE2EInitServeLockedNoopKeepsAdminUIAndReturnsMetricsUnavailable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := setupPluginDir(t, dir)
	archivePath := filepath.Join(dir, "plugin.tar.gz")
	if err := pluginpkg.CreatePackageFromDir(pluginDir, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir: %v", err)
	}

	port := allocateTestPort(t)
	cfgPath := writeE2EConfig(t, dir, "plugin.tar.gz", port)
	cfgBytes, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	cfgBytes = append(cfgBytes, []byte(`telemetry:
  provider: noop
`)...)
	if err := os.WriteFile(cfgPath, cfgBytes, 0o644); err != nil {
		t.Fatalf("write config telemetry: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "init", "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd init: %v\n%s", err, out)
	}

	serveLockedAndExerciseExample(t, cfgPath, port, "", func(t *testing.T, baseURL string) {
		invokeExampleOperation(t, baseURL, "echo", `{"message":"hello"}`, http.StatusOK)

		promBody := getEndpointBody(t, baseURL+"/metrics", http.StatusServiceUnavailable)
		if !bytes.Contains(promBody, []byte("Prometheus metrics are unavailable")) {
			t.Fatalf("expected disabled metrics message in /metrics body: %s", promBody)
		}

		adminBody := getEndpointBody(t, baseURL+"/admin", http.StatusOK)
		if !bytes.Contains(adminBody, []byte("Prometheus metrics")) {
			t.Fatalf("expected embedded admin UI at /admin: %s", adminBody)
		}
	})
}

func TestE2EServeLockedUsesUIPluginForClientRootAndKeepsAdminBuiltIn(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	providerPluginDir := setupPluginDir(t, dir)
	providerArchivePath := filepath.Join(dir, "plugin.tar.gz")
	if err := pluginpkg.CreatePackageFromDir(providerPluginDir, providerArchivePath); err != nil {
		t.Fatalf("CreatePackageFromDir provider: %v", err)
	}

	uiPluginDir := newWebUIReleaseFixture(t, dir)
	if err := os.WriteFile(
		filepath.Join(uiPluginDir, webUITestAssetRoot, "index.html"),
		[]byte("<html><body>custom client ui</body></html>\n"),
		0o644,
	); err != nil {
		t.Fatalf("write ui plugin index: %v", err)
	}
	uiArchivePath := filepath.Join(dir, "client-ui.tar.gz")
	if err := pluginpkg.CreatePackageFromDir(uiPluginDir, uiArchivePath); err != nil {
		t.Fatalf("CreatePackageFromDir ui: %v", err)
	}

	port := allocateTestPort(t)
	cfgPath := writeE2EConfig(t, dir, "plugin.tar.gz", port)
	cfgBytes, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	cfgBytes = append(cfgBytes, []byte(`
ui:
  plugin:
    package: client-ui.tar.gz
`)...)
	if err := os.WriteFile(cfgPath, cfgBytes, 0o644); err != nil {
		t.Fatalf("write ui plugin config: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "init", "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd init: %v\n%s", err, out)
	}

	serveLockedAndExerciseExample(t, cfgPath, port, "", func(t *testing.T, baseURL string) {
		rootBody := getEndpointBody(t, baseURL+"/", http.StatusOK)
		if !bytes.Contains(rootBody, []byte("custom client ui")) {
			t.Fatalf("expected ui.plugin assets at root: %s", rootBody)
		}

		adminBody := getEndpointBody(t, baseURL+"/admin", http.StatusOK)
		if !bytes.Contains(adminBody, []byte("Prometheus metrics")) {
			t.Fatalf("expected built-in admin UI at /admin: %s", adminBody)
		}
	})
}

func TestE2EBareGestaltdAutoInit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := setupPluginDir(t, dir)
	archivePath := filepath.Join(dir, "plugin.tar.gz")
	if err := pluginpkg.CreatePackageFromDir(pluginDir, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir: %v", err)
	}

	port := allocateTestPort(t)
	cfgPath := writeE2EConfig(t, dir, "plugin.tar.gz", port)

	cmd := exec.Command(gestaltdBin, "--config", cfgPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start gestaltd: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		_ = cmd.Wait()
	})

	baseURL := fmt.Sprintf("http://localhost:%d", port)
	waitForHealth(t, baseURL, 20*time.Second)
}

func TestE2EBareGestaltdUsesDotGestaltdHomeConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	homeDir := filepath.Join(dir, "home")
	configDir := filepath.Join(homeDir, ".gestaltd")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll config dir: %v", err)
	}

	port := allocateTestPort(t)
	cfg := `auth:
  provider: none
datastore:
  provider: sqlite
  config:
    path: ` + filepath.Join(configDir, "gestalt.db") + `
server:
  port: ` + fmt.Sprintf("%d", port) + `
  encryption_key: test-key
`
	cfgPath := filepath.Join(configDir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	cmd := exec.Command(gestaltdBin)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = withoutEnvVar(os.Environ(), "GESTALT_CONFIG")
	cmd.Env = append(cmd.Env, "HOME="+homeDir)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start gestaltd: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		_ = cmd.Wait()
	})

	baseURL := fmt.Sprintf("http://localhost:%d", port)
	waitForHealth(t, baseURL, 20*time.Second)
}

func TestE2EValidateNonMutating(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := setupPluginDir(t, dir)
	archivePath := filepath.Join(dir, "plugin.tar.gz")
	if err := pluginpkg.CreatePackageFromDir(pluginDir, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir: %v", err)
	}

	cfgPath := writeE2EConfig(t, dir, "plugin.tar.gz", 0)
	lockPath := filepath.Join(dir, operator.InitLockfileName)

	out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
	if err == nil {
		t.Fatalf("expected validate to fail without init, output: %s", out)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatal("expected no lockfile after non-mutating validate")
	}

	out, err = exec.Command(gestaltdBin, "validate", "--init", "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd validate --init: %v\n%s", err, out)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("expected lockfile after validate --init: %v", err)
	}
}

func TestE2EHelmChart(t *testing.T) {
	t.Parallel()

	helmPath, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("helm not installed")
	}

	chartDir := filepath.Join("..", "..", "deploy", "helm", "gestalt")

	t.Run("default chart profile boots", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		port := allocateTestPort(t)
		dbPath := filepath.Join(dir, "gestalt.db")
		rendered := renderHelmChart(t, helmPath, chartDir,
			"--set", fmt.Sprintf("config.server.port=%d", port),
			"--set-string", "config.datastore.config.path="+dbPath,
		)

		cfgPath := filepath.Join(dir, "config.yaml")
		if err := os.WriteFile(cfgPath, []byte(extractRenderedConfig(t, rendered)), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
		if err != nil {
			t.Fatalf("gestaltd validate: %v\n%s", err, out)
		}

		cmd := exec.Command(gestaltdBin, "serve", "--locked", "--config", cfgPath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatalf("start serve: %v", err)
		}
		t.Cleanup(func() {
			_ = cmd.Process.Signal(os.Interrupt)
			_ = cmd.Wait()
		})

		baseURL := fmt.Sprintf("http://localhost:%d", port)
		waitForHealth(t, baseURL, 20*time.Second)
		waitForReady(t, baseURL, 20*time.Second)
	})

	t.Run("ingress paths render from values", func(t *testing.T) {
		t.Parallel()

		rendered := renderHelmChart(t, helmPath, chartDir,
			"--set", "ingress.enabled=true",
			"--set-string", "ingress.hosts[0].host=gestalt.example.com",
			"--set-string", "ingress.hosts[0].paths[0].path=/gestalt",
			"--set-string", "ingress.hosts[0].paths[0].pathType=Prefix",
		)

		for _, want := range []string{
			`host: "gestalt.example.com"`,
			`path: "/gestalt"`,
			`pathType: Prefix`,
		} {
			if !strings.Contains(rendered, want) {
				t.Fatalf("expected rendered manifest to contain %q\n%s", want, rendered)
			}
		}
	})
}

func withoutEnvVar(env []string, name string) []string {
	prefix := name + "="
	filtered := env[:0]
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func renderHelmChart(t *testing.T, helmPath, chartDir string, extraArgs ...string) string {
	t.Helper()
	args := append([]string{"template", "test-release", chartDir}, extraArgs...)
	out, err := exec.Command(helmPath, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, out)
	}
	return string(out)
}

func extractRenderedConfig(t *testing.T, rendered string) string {
	t.Helper()

	var doc struct {
		Kind string            `yaml:"kind"`
		Data map[string]string `yaml:"data"`
	}

	dec := yaml.NewDecoder(strings.NewReader(rendered))
	for {
		doc = struct {
			Kind string            `yaml:"kind"`
			Data map[string]string `yaml:"data"`
		}{}
		if err := dec.Decode(&doc); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("decode rendered manifest: %v", err)
		}
		if doc.Kind == "ConfigMap" && doc.Data["config.yaml"] != "" {
			return doc.Data["config.yaml"]
		}
	}

	t.Fatal("rendered chart missing config.yaml ConfigMap")
	return ""
}

//nolint:paralleltest // Spawns the CLI binary; keeping it serial avoids package-level e2e flake.
func TestE2EValidateRejectsUnknownYAMLField(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		pluginYAML string
		wantError  string
	}{
		{
			name: "bogus field",
			pluginYAML: `from:
  command: /tmp/provider
bogus: true`,
			wantError: "bogus",
		},
		{
			name: "removed plugin connection field",
			pluginYAML: `from:
  command: /tmp/provider
connection: default`,
			wantError: "connection",
		},
		{
			name: "removed provider params field",
			pluginYAML: `from:
  command: /tmp/provider
params:
  tenant:
    required: true`,
			wantError: "params",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "config.yaml")
			cfg := fmt.Sprintf(`auth:
  provider: local
datastore:
  provider: sqlite
  config:
    path: %s
server:
  encryption_key: test-key
providers:
  example:
    %s
`, filepath.Join(dir, "gestalt.db"), strings.ReplaceAll(tc.pluginYAML, "\n", "\n    "))
			if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
				t.Fatalf("WriteFile config: %v", err)
			}

			out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
			if err == nil {
				t.Fatalf("expected validate to fail for unknown field, output: %s", out)
			}
			if !strings.Contains(string(out), tc.wantError) || !strings.Contains(string(out), "parsing config YAML") {
				t.Fatalf("expected output to mention %q and YAML parsing, got: %s", tc.wantError, out)
			}
		})
	}
}

func TestE2EValidateRejectsUnsupportedPluginFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		pluginYAML string
		wantError  string
	}{
		{
			name: "env unsupported for inline plugin",
			pluginYAML: `from:
  env:
    API_KEY: secret
surfaces:
  openapi:
    document: https://api.example.test/openapi.json`,
			wantError: "plugin.env is only valid when the plugin runs as an executable process; remove plugin.env or switch this integration to plugin.command, plugin.package, or plugin.source",
		},
		{
			name: "allowed hosts unsupported for inline plugin",
			pluginYAML: `from:
  allowed_hosts:
    - api.example.test
surfaces:
  openapi:
    document: https://api.example.test/openapi.json`,
			wantError: "plugin.allowed_hosts is only valid when the plugin runs as an executable process; remove plugin.allowed_hosts or switch this integration to plugin.command, plugin.package, or plugin.source",
		},
		{
			name: "headers unsupported without declarative ops or spec surface",
			pluginYAML: `from:
  command: /tmp/provider
headers:
  x-test: value`,
			wantError: "plugin.headers are only valid when the plugin exposes declarative operations or a spec surface; remove plugin.headers or configure declarative operations, OpenAPI, GraphQL, or MCP",
		},
		{
			name: "managed parameters unsupported without api surface",
			pluginYAML: `from:
  command: /tmp/provider
managed_parameters:
  - in: header
    name: x-version
    value: "1"`,
			wantError: "plugin.managed_parameters are only valid with openapi/graphql surfaces; remove plugin.managed_parameters or configure OpenAPI or GraphQL",
		},
		{
			name: "response mapping unsupported for inline operations only",
			pluginYAML: `response_mapping:
  data_path: items
surfaces:
  rest:
    base_url: https://api.example.test
    operations:
      - name: list_items
        method: GET
        path: /items`,
			wantError: "plugin.response_mapping is only valid for openapi/graphql integrations; remove plugin.response_mapping or configure an OpenAPI or GraphQL surface",
		},
		{
			name: "multiple api surfaces are rejected",
			pluginYAML: `surfaces:
  rest:
    base_url: https://api.example.test
    operations:
      - name: list_items
        method: GET
        path: /items
  openapi:
    document: https://api.example.test/openapi.json`,
			wantError: "provider config can define only one of surfaces.rest, surfaces.openapi, or surfaces.graphql",
		},
		{
			name: "rest surface requires operations",
			pluginYAML: `surfaces:
  rest:
    base_url: https://api.example.test`,
			wantError: "surfaces.rest.operations is required when surfaces.rest is configured",
		},
		{
			name: "mcp connection requires mcp url",
			pluginYAML: `connections:
  workspace:
    auth:
      type: manual
surfaces:
  openapi:
    document: https://api.example.test/openapi.json
    connection: workspace
  mcp:
    connection: workspace`,
			wantError: "surfaces.mcp.url is required when surfaces.mcp is configured",
		},
		{
			name: "mcp tool prefix requires enabled",
			pluginYAML: `mcp:
  tool_prefix: github_`,
			wantError: "mcp.tool_prefix is only valid when mcp.enabled is true",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "config.yaml")
			cfg := fmt.Sprintf(`auth:
  provider: local
datastore:
  provider: sqlite
  config:
    path: %s
server:
  encryption_key: test-key
providers:
  example:
    %s
`, filepath.Join(dir, "gestalt.db"), strings.ReplaceAll(tc.pluginYAML, "\n", "\n    "))

			if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
				t.Fatalf("WriteFile config: %v", err)
			}

			out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
			if err == nil {
				t.Fatalf("expected validate to fail for unsupported plugin field, output: %s", out)
			}
			if !strings.Contains(string(out), tc.wantError) {
				t.Fatalf("expected output to mention %q, got: %s", tc.wantError, out)
			}
		})
	}
}

//nolint:paralleltest // Exercises validate --init so the prepared manifest can affect validation.
func TestE2EValidateInitRejectsUnsupportedManagedPluginFields(t *testing.T) {
	cases := []struct {
		name       string
		setup      func(t *testing.T, dir string) string
		pluginYAML string
		wantError  string
	}{
		{
			name:  "config headers unsupported for executable-only package plugin",
			setup: setupPluginDir,
			pluginYAML: `from:
  package: %s
headers:
  x-test: value`,
			wantError: "plugin.headers are only valid when the plugin exposes declarative operations or a spec surface; remove plugin.headers or configure declarative operations, OpenAPI, GraphQL, or MCP",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			pluginDir := tc.setup(t, dir)
			cfgPath := filepath.Join(dir, "config.yaml")
			pluginYAML := fmt.Sprintf(tc.pluginYAML, pluginDir)
			cfg := fmt.Sprintf(`auth:
  provider: local
datastore:
  provider: sqlite
  config:
    path: %s
server:
  encryption_key: test-key
providers:
  example:
    %s
`, filepath.Join(dir, "gestalt.db"), strings.ReplaceAll(pluginYAML, "\n", "\n    "))

			if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
				t.Fatalf("WriteFile config: %v", err)
			}

			out, err := exec.Command(gestaltdBin, "validate", "--init", "--config", cfgPath).CombinedOutput()
			if err == nil {
				t.Fatalf("expected validate --init to fail for unsupported managed plugin field, output: %s", out)
			}
			if !strings.Contains(string(out), tc.wantError) {
				t.Fatalf("expected output to mention %q, got: %s", tc.wantError, out)
			}
		})
	}
}

//nolint:paralleltest // Spawns the CLI binary; keeping it serial avoids package-level e2e flake.
func TestE2EDefaultStartRejectsUnknownYAMLField(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `auth:
  provider: local
datastore:
  provider: sqlite
  config:
    path: ` + filepath.Join(dir, "gestalt.db") + `
server:
  encryption_key: test-key
  typo: true
providers:
  example:
    from:
      command: /tmp/provider
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "--config", cfgPath).CombinedOutput()
	if err == nil {
		t.Fatalf("expected default start command to fail for unknown field, output: %s", out)
	}
	if !strings.Contains(string(out), "typo") || !strings.Contains(string(out), "parsing config YAML") {
		t.Fatalf("expected output to mention unknown field and YAML parsing, got: %s", out)
	}
}

//nolint:paralleltest // Spawns the CLI binary; keeping it serial avoids package-level e2e flake.
func TestE2EValidateRejectsMalformedYAML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(`{{{invalid yaml`), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
	if err == nil {
		t.Fatalf("expected validate to fail for malformed YAML, output: %s", out)
	}
	if !strings.Contains(string(out), "parsing config YAML") {
		t.Fatalf("expected output to mention YAML parsing failure, got: %s", out)
	}
}

func setupPluginDir(t *testing.T, baseDir string) string {
	t.Helper()
	return setupPluginDirWithVersion(t, baseDir, "0.1.0")
}

func setupPluginDirWithVersion(t *testing.T, baseDir, version string) string {
	t.Helper()

	pluginDir := filepath.Join(baseDir, "plugin-src")
	artifactRel := pluginArtifactRel()
	artifactAbs := filepath.Join(pluginDir, filepath.FromSlash(artifactRel))

	if err := os.MkdirAll(filepath.Dir(artifactAbs), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	if err := copyFile(pluginBin, artifactAbs); err != nil {
		t.Fatalf("copy plugin binary: %v", err)
	}

	writeManifest(t, pluginDir, version)
	return pluginDir
}

func writeManifest(t *testing.T, pluginDir, version string) {
	t.Helper()

	artifactRel := pluginArtifactRel()
	artifactAbs := filepath.Join(pluginDir, filepath.FromSlash(artifactRel))

	digest, err := fileSHA256(artifactAbs)
	if err != nil {
		t.Fatalf("compute artifact digest: %v", err)
	}

	manifest := &pluginmanifestv1.Manifest{
		Source:   "github.com/test/plugins/provider",
		Version:  version,
		Kinds:    []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:     runtime.GOOS,
				Arch:   runtime.GOARCH,
				Path:   artifactRel,
				SHA256: digest,
			},
		},
		Entrypoints: pluginmanifestv1.Entrypoints{
			Provider: &pluginmanifestv1.Entrypoint{
				ArtifactPath: artifactRel,
			},
		},
	}
	writeManifestFile(t, pluginDir, manifest)
}

func writeManifestFile(t *testing.T, pluginDir string, manifest *pluginmanifestv1.Manifest) {
	t.Helper()

	data, err := pluginpkg.EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, pluginpkg.ManifestFile), data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func pluginArtifactRel() string {
	return filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "provider"))
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func fileSHA256(path string) (string, error) {
	return pluginpkg.FileSHA256(path)
}

func writeE2EConfig(t *testing.T, dir, packageRef string, port int) string {
	t.Helper()
	return writeE2EConfigWithPaths(t, dir, packageRef, filepath.Join(dir, "gestalt.db"), "", port)
}

func writeE2EConfigWithPaths(t *testing.T, dir, packageRef, dbPath, artifactsDir string, port int) string {
	t.Helper()

	if port == 0 {
		port = 18080
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	serverBlock := fmt.Sprintf(`server:
  port: %d
  encryption_key: test-e2e-key
`, port)
	if artifactsDir != "" {
		serverBlock += fmt.Sprintf("  artifacts_dir: %s\n", artifactsDir)
	}
	cfg := fmt.Sprintf(`auth:
  provider: none
datastore:
  provider: sqlite
  config:
    path: %s
%sproviders:
  example:
    from:
      package: %s
`, dbPath, serverBlock, packageRef)

	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath
}

func writeE2EConfigForDir(t *testing.T, dir, pluginDir string) string {
	t.Helper()
	return writeE2EConfig(t, dir, pluginDir, 0)
}

func serveLockedAndExerciseExample(t *testing.T, cfgPath string, port int, artifactsDir string, exercise func(t *testing.T, baseURL string)) (string, string) {
	t.Helper()

	args := []string{"serve", "--locked", "--config", cfgPath}
	if artifactsDir != "" {
		args = append(args, "--artifacts-dir", artifactsDir)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := exec.Command(gestaltdBin, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start serve: %v", err)
	}
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			_ = cmd.Process.Signal(os.Interrupt)
			_ = cmd.Wait()
		}
	})

	baseURL := fmt.Sprintf("http://localhost:%d", port)
	waitForReady(t, baseURL, 30*time.Second)

	exercise(t, baseURL)

	stopped = true
	_ = cmd.Process.Signal(os.Interrupt)
	_ = cmd.Wait()
	return stdout.String(), stderr.String()
}

func serveLockedAndInvokeExampleEcho(t *testing.T, cfgPath string, port int, artifactsDir string) (string, string) {
	t.Helper()

	return serveLockedAndExerciseExample(t, cfgPath, port, artifactsDir, func(t *testing.T, baseURL string) {
		body := invokeExampleOperation(t, baseURL, "echo", `{"message":"hello"}`, http.StatusOK)

		var result map[string]any
		if err := json.Unmarshal(body, &result); err != nil {
			t.Fatalf("unmarshal: %v\nbody: %s", err, body)
		}
		if result["echo"] != "hello" {
			t.Fatalf("expected echo=hello, got %v", result)
		}
	})
}

func invokeExampleOperation(t *testing.T, baseURL, operation, requestBody string, wantStatus int) []byte {
	t.Helper()

	req, _ := http.NewRequest(http.MethodPost, baseURL+"/api/v1/example/"+operation, strings.NewReader(requestBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != wantStatus {
		t.Fatalf("invoke %q returned %d, want %d: %s", operation, resp.StatusCode, wantStatus, respBody)
	}
	return respBody
}

func getEndpointBody(t *testing.T, url string, wantStatus int) []byte {
	t.Helper()

	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("GET %s returned %d, want %d: %s", url, resp.StatusCode, wantStatus, body)
	}
	return body
}

func requireMetricPayload(t *testing.T, exports [][]byte, stdout, stderr string, parts ...string) {
	t.Helper()

	for _, body := range exports {
		if payloadContainsAll(body, parts...) {
			return
		}
	}

	t.Fatalf("expected OTLP metric payload to contain %q\nstdout:\n%s\nstderr:\n%s", parts, stdout, stderr)
}

func payloadContainsAll(body []byte, parts ...string) bool {
	for _, part := range parts {
		if !bytes.Contains(body, []byte(part)) {
			return false
		}
	}
	return true
}

var nextTestPort atomic.Int32 // zero value; first allocation returns 19100

func allocateTestPort(t *testing.T) int {
	t.Helper()
	return int(nextTestPort.Add(1)) + 19099
}

func waitForHealth(t *testing.T, baseURL string, timeout time.Duration) {
	t.Helper()
	waitForEndpoint(t, baseURL+"/health", timeout)
}

func waitForReady(t *testing.T, baseURL string, timeout time.Duration) {
	t.Helper()
	waitForEndpoint(t, baseURL+"/ready", timeout)
}

func waitForEndpoint(t *testing.T, url string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("%s did not return 200 within %s", url, timeout)
}

func TestE2EHybridAPIPluginIntegration(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := setupPluginDir(t, dir)
	archivePath := filepath.Join(dir, "plugin.tar.gz")
	if err := pluginpkg.CreatePackageFromDir(pluginDir, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir: %v", err)
	}

	port := allocateTestPort(t)
	cfgPath := writeHybridAPIPluginConfig(t, dir, archivePath, port)

	out, err := exec.Command(gestaltdBin, "init", "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd init: %v\n%s", err, out)
	}

	out, err = exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd validate failed for hybrid api+plugin config: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "config ok") {
		t.Fatalf("expected 'config ok' in validate output, got: %s", out)
	}

	cmd := exec.Command(gestaltdBin, "serve", "--locked", "--config", cfgPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start serve: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		_ = cmd.Wait()
	})

	baseURL := fmt.Sprintf("http://localhost:%d", port)
	waitForReady(t, baseURL, 30*time.Second)
}

func TestE2EHybridSpecLoadedPackageKeepsExecutableAndAllowedOperations(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := setupHybridSpecLoadedPluginDir(t, dir)
	archivePath := filepath.Join(dir, "plugin.tar.gz")

	out, err := exec.Command(gestaltdBin, "plugin", "package", "--input", pluginDir, "--output", archivePath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd plugin package: %v\n%s", err, out)
	}

	port := allocateTestPort(t)
	cfgPath := writeE2EConfig(t, dir, "plugin.tar.gz", port)

	out, err = exec.Command(gestaltdBin, "init", "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd init: %v\n%s", err, out)
	}

	lockPath := filepath.Join(dir, operator.InitLockfileName)
	lock, err := operator.ReadLockfile(lockPath)
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	entry, ok := lock.Plugins[operator.LockPluginKey("integration", "example")]
	if !ok {
		t.Fatalf("lockfile missing plugin entry: %+v", lock.Plugins)
	}
	if entry.Executable == "" {
		t.Fatal("expected packaged hybrid plugin executable to be preserved in lockfile")
	}

	cmd := exec.Command(gestaltdBin, "serve", "--locked", "--config", cfgPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start serve: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		_ = cmd.Wait()
	})

	baseURL := fmt.Sprintf("http://localhost:%d", port)
	waitForReady(t, baseURL, 30*time.Second)

	resp, err := http.Get(baseURL + "/api/v1/integrations/example/operations")
	if err != nil {
		t.Fatalf("list operations: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("list operations returned %d: %s", resp.StatusCode, body)
	}

	var ops []struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ops); err != nil {
		t.Fatalf("decode operations: %v", err)
	}

	ids := make([]string, 0, len(ops))
	for _, op := range ops {
		ids = append(ids, op.ID)
	}
	sort.Strings(ids)
	if !containsString(ids, "echo") {
		t.Fatalf("operation ids = %v, want echo from the packaged executable", ids)
	}
	if !containsString(ids, "messages.list") || !containsString(ids, "getProfile") {
		t.Fatalf("operation ids = %v, want aliased spec operations", ids)
	}
	if containsString(ids, "gmail.users.labels.list") {
		t.Fatalf("operation ids = %v, did not expect disallowed raw spec operation", ids)
	}

	toolNames := listMCPTools(t, baseURL)
	for _, want := range []string{
		"example_echo",
		"example_messages.list",
		"example_getProfile",
	} {
		if !containsString(toolNames, want) {
			t.Fatalf("mcp tool names = %v, want %s", toolNames, want)
		}
	}
	if containsString(toolNames, "example_gmail.users.labels.list") {
		t.Fatalf("mcp tool names = %v, did not expect disallowed raw spec tool", toolNames)
	}
}

func writeHybridAPIPluginConfig(t *testing.T, dir, packageRef string, port int) string {
	t.Helper()

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := fmt.Sprintf(`auth:
  provider: none
datastore:
  provider: sqlite
  config:
    path: %s
server:
  port: %d
  encryption_key: test-hybrid-key
providers:
  hybrid:
    display_name: Hybrid Test
    from:
      package: %s
`, filepath.Join(dir, "gestalt.db"), port, packageRef)

	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath
}

func setupHybridSpecLoadedPluginDir(t *testing.T, baseDir string) string {
	t.Helper()

	pluginDir := filepath.Join(baseDir, "hybrid-plugin-src")
	artifactRel := pluginArtifactRel()
	artifactAbs := filepath.Join(pluginDir, filepath.FromSlash(artifactRel))
	specRel := filepath.ToSlash(filepath.Join("specs", "openapi.yaml"))

	if err := os.MkdirAll(filepath.Dir(artifactAbs), 0o755); err != nil {
		t.Fatalf("MkdirAll artifact dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(pluginDir, "specs"), 0o755); err != nil {
		t.Fatalf("MkdirAll specs dir: %v", err)
	}
	if err := copyFile(pluginBin, artifactAbs); err != nil {
		t.Fatalf("copy plugin binary: %v", err)
	}

	spec := `openapi: 3.0.0
info:
  title: Hybrid Allowed Ops API
  version: 1.0.0
servers:
  - url: https://api.hybrid.example/v1
paths:
  /messages:
    get:
      operationId: gmail.users.messages.list
      responses:
        "200":
          description: ok
  /profile:
    get:
      operationId: gmail.users.getProfile
      responses:
        "200":
          description: ok
  /labels:
    get:
      operationId: gmail.users.labels.list
      responses:
        "200":
          description: ok
`
	if err := os.WriteFile(filepath.Join(pluginDir, filepath.FromSlash(specRel)), []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	digest, err := fileSHA256(artifactAbs)
	if err != nil {
		t.Fatalf("compute artifact digest: %v", err)
	}

	manifest := &pluginmanifestv1.Manifest{
		Source:  "github.com/test/plugins/hybrid-spec-loaded",
		Version: "0.1.0",
		Kinds:   []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			OpenAPI: specRel,
			AllowedOperations: map[string]*pluginmanifestv1.ManifestOperationOverride{
				"gmail.users.messages.list": {Alias: "messages.list"},
				"gmail.users.getProfile":    {Alias: "getProfile"},
			},
		},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:     runtime.GOOS,
				Arch:   runtime.GOARCH,
				Path:   artifactRel,
				SHA256: digest,
			},
		},
		Entrypoints: pluginmanifestv1.Entrypoints{
			Provider: &pluginmanifestv1.Entrypoint{
				ArtifactPath: artifactRel,
			},
		},
	}
	data, err := pluginpkg.EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, pluginpkg.ManifestFile), data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	return pluginDir
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func listMCPTools(t *testing.T, baseURL string) []string {
	t.Helper()

	status, resp := mcpJSONRPC(t, baseURL, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test", "version": "1.0"},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("initialize: expected 200, got %d", status)
	}
	if _, ok := resp["result"].(map[string]any); !ok {
		t.Fatalf("initialize: expected result object, got %v", resp)
	}

	status, resp = mcpJSONRPC(t, baseURL, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
	})
	if status != http.StatusOK {
		t.Fatalf("tools/list: expected 200, got %d", status)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("tools/list: expected result object, got %v", resp)
	}
	rawTools, ok := result["tools"].([]any)
	if !ok {
		t.Fatalf("tools/list: expected tools array, got %v", result)
	}

	toolNames := make([]string, 0, len(rawTools))
	for _, rawTool := range rawTools {
		tool, ok := rawTool.(map[string]any)
		if !ok {
			t.Fatalf("tools/list: expected tool object, got %T", rawTool)
		}
		name, ok := tool["name"].(string)
		if !ok {
			t.Fatalf("tools/list: expected string tool name, got %v", tool)
		}
		toolNames = append(toolNames, name)
	}
	sort.Strings(toolNames)
	return toolNames
}

func mcpJSONRPC(t *testing.T, baseURL string, body map[string]any) (int, map[string]any) {
	t.Helper()

	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal mcp body: %v", err)
	}
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/mcp", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read /mcp response: %v", err)
	}

	var result map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatalf("decode /mcp response: %v\nbody: %s", err, raw)
		}
	}
	return resp.StatusCode, result
}
