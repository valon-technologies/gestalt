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
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/operator"
	"github.com/valon-technologies/gestalt/server/internal/pluginpkg"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
	"gopkg.in/yaml.v3"
)

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

	port := allocateTestPort(t)
	cfgPath := writeE2EConfigWithPaths(t, deployDir, pluginDir, filepath.Join(dataDir, "gestalt.db"), artifactsDir, port)
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

	out, err := exec.Command(gestaltdBin, "init", "--config", cfgPath, "--artifacts-dir", artifactsDir).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd init: %v\n%s", err, out)
	}

	lockPath := filepath.Join(deployDir, operator.InitLockfileName)
	lockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lockfile: %v", err)
	}
	var rawLock map[string]any
	if err := json.Unmarshal(lockBytes, &rawLock); err != nil {
		t.Fatalf("decode lockfile json: %v", err)
	}
	if _, ok := rawLock["plugins"]; ok {
		t.Fatalf("expected lockfile to omit legacy plugins map: %s", lockBytes)
	}
	rawProviders, ok := rawLock["providers"].(map[string]any)
	if !ok {
		t.Fatalf("expected lockfile providers object: %s", lockBytes)
	}
	if len(rawProviders) != 0 {
		t.Fatalf("expected local source config to avoid prepared provider entries, got: %s", lockBytes)
	}

	lock, err := operator.ReadLockfile(lockPath)
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	if lock.Version != 1 {
		t.Fatalf("lock version = %d, want 1", lock.Version)
	}
	if len(lock.Providers) != 0 {
		t.Fatalf("expected no prepared provider entries for local source config, got %+v", lock.Providers)
	}
	if lock.UI != nil {
		t.Fatalf("expected no ui lock entry when config has no managed ui plugin")
	}

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
	cfgPath := writeE2EConfig(t, dir, pluginDir, port)
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
		if !bytes.Contains(adminBody, []byte("Requests and failures")) {
			t.Fatalf("expected activity graph section in embedded admin UI: %s", adminBody)
		}
		if !bytes.Contains(adminBody, []byte("Average and p95")) {
			t.Fatalf("expected latency graph section in embedded admin UI: %s", adminBody)
		}
		if !bytes.Contains(adminBody, []byte("Top providers")) {
			t.Fatalf("expected graph sections in embedded admin UI: %s", adminBody)
		}
		if !bytes.Contains(adminBody, []byte("Time window")) {
			t.Fatalf("expected chart controls in embedded admin UI: %s", adminBody)
		}
		if !bytes.Contains(adminBody, []byte("Refresh cadence")) {
			t.Fatalf("expected refresh controls in embedded admin UI: %s", adminBody)
		}
		if bytes.Contains(adminBody, []byte("Raw Prometheus output")) {
			t.Fatalf("did not expect raw prometheus section in embedded admin UI: %s", adminBody)
		}
		if !bytes.Contains(adminBody, []byte("echarts.simple.min.js")) {
			t.Fatalf("expected admin ui to include echarts asset: %s", adminBody)
		}
		if !bytes.Contains(adminBody, []byte("theme.css")) {
			t.Fatalf("expected admin ui to include shared theme asset: %s", adminBody)
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

	port := allocateTestPort(t)
	cfgPath := writeE2EConfig(t, dir, pluginDir, port)
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
		if !bytes.Contains(adminBody, []byte("Requests and failures")) {
			t.Fatalf("expected activity graph section in embedded admin UI: %s", adminBody)
		}
		if !bytes.Contains(adminBody, []byte("Top providers")) {
			t.Fatalf("expected provider chart section in embedded admin UI: %s", adminBody)
		}
		if !bytes.Contains(adminBody, []byte("Time window")) {
			t.Fatalf("expected chart controls in embedded admin UI: %s", adminBody)
		}
		if !bytes.Contains(adminBody, []byte("Refresh cadence")) {
			t.Fatalf("expected refresh controls in embedded admin UI: %s", adminBody)
		}
		if bytes.Contains(adminBody, []byte("Raw Prometheus output")) {
			t.Fatalf("did not expect raw prometheus section in embedded admin UI: %s", adminBody)
		}
		if !bytes.Contains(adminBody, []byte("echarts.simple.min.js")) {
			t.Fatalf("expected admin ui to include echarts asset: %s", adminBody)
		}
		if !bytes.Contains(adminBody, []byte("theme.css")) {
			t.Fatalf("expected admin ui to include shared theme asset: %s", adminBody)
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

	port := allocateTestPort(t)
	cfgPath := writeE2EConfig(t, dir, pluginDir, port)
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
		if !bytes.Contains(adminBody, []byte("echarts.simple.min.js")) {
			t.Fatalf("expected admin ui to include echarts asset: %s", adminBody)
		}
		if !bytes.Contains(adminBody, []byte("theme.css")) {
			t.Fatalf("expected admin ui to include shared theme asset: %s", adminBody)
		}
	})
}

func TestE2EInitServeLockedSplitManagementListener(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := setupPluginDir(t, dir)

	publicPort := allocateTestPort(t)
	managementPort := allocateTestPort(t)
	cfgPath := writeSplitListenerE2EConfig(t, dir, pluginDir, publicPort, managementPort)

	out, err := exec.Command(gestaltdBin, "init", "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd init: %v\n%s", err, out)
	}

	stdout, stderr := serveLockedAndExerciseWithManagement(t, cfgPath, publicPort, managementPort, "", func(t *testing.T, publicBaseURL, managementBaseURL string) {
		invokeExampleOperation(t, publicBaseURL, "echo", `{"message":"hello"}`, http.StatusOK)

		publicMetrics := getEndpointBody(t, publicBaseURL+"/metrics", http.StatusNotFound)
		if bytes.Contains(publicMetrics, []byte("gestaltd_operation_count_total")) {
			t.Fatalf("did not expect public listener to expose /metrics: %s", publicMetrics)
		}

		publicAdmin := getEndpointBody(t, publicBaseURL+"/admin", http.StatusNotFound)
		if bytes.Contains(publicAdmin, []byte("Prometheus metrics")) {
			t.Fatalf("did not expect public listener to expose /admin: %s", publicAdmin)
		}

		managementMetrics := getEndpointBody(t, managementBaseURL+"/metrics", http.StatusOK)
		if !bytes.Contains(managementMetrics, []byte("gestaltd_operation_count_total")) {
			t.Fatalf("expected management listener to expose /metrics: %s", managementMetrics)
		}

		managementRoot := getEndpointBody(t, managementBaseURL+"/", http.StatusOK)
		if !bytes.Contains(managementRoot, []byte("Prometheus metrics")) {
			t.Fatalf("expected management listener root to land on admin ui: %s", managementRoot)
		}

		managementAdmin := getEndpointBody(t, managementBaseURL+"/admin", http.StatusOK)
		if !bytes.Contains(managementAdmin, []byte("Prometheus metrics")) {
			t.Fatalf("expected management listener to expose /admin: %s", managementAdmin)
		}
		if !bytes.Contains(managementAdmin, []byte(`class="brand" href="/admin/"`)) {
			t.Fatalf("expected management admin brand link to stay on /admin: %s", managementAdmin)
		}
		if bytes.Contains(managementAdmin, []byte(`<a href="/">Client UI</a>`)) {
			t.Fatalf("did not expect management admin to link to same-origin /: %s", managementAdmin)
		}
		if !bytes.Contains(managementAdmin, []byte(`href="https://gestalt.example.test"`)) {
			t.Fatalf("expected management admin to link to configured public base url: %s", managementAdmin)
		}
	})
	if !strings.Contains(stdout, "management listener serves /admin and /metrics without Gestalt auth") {
		t.Fatalf("expected management-listener warning in stdout\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestE2EBareGestaltdAutoInit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := setupPluginDir(t, dir)

	port := allocateTestPort(t)
	cfgPath := writeE2EConfig(t, dir, pluginDir, port)

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

	cfgPath := writeE2EConfig(t, dir, pluginDir, 0)
	lockPath := filepath.Join(dir, operator.InitLockfileName)

	out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("expected validate to succeed without init for local source plugins: %v\n%s", err, out)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatal("expected no lockfile after non-mutating validate")
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

	t.Run("management service renders from values", func(t *testing.T) {
		t.Parallel()

		rendered := renderHelmChart(t, helmPath, chartDir,
			"--set", "managementService.enabled=true",
			"--set", "managementService.port=9090",
			"--set", "config.server.management.port=9090",
			"--set-string", "config.server.management.host=0.0.0.0",
		)

		for _, want := range []string{
			`kind: Service`,
			`name: test-release-gestalt-management`,
			`port: 9090`,
			`targetPort: management`,
			`containerPort: 9090`,
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
  source:
    path: /tmp/plugin.yaml
bogus: true`,
			wantError: "bogus",
		},
		{
			name: "removed plugin connection field",
			pluginYAML: `from:
  source:
    path: /tmp/plugin.yaml
connection: default`,
			wantError: "connection",
		},
		{
			name: "removed provider params field",
			pluginYAML: `from:
  source:
    path: /tmp/plugin.yaml
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
			wantError: "plugin.env is only valid when the plugin runs as an executable process; remove plugin.env or switch this integration to plugin.source",
		},
		{
			name: "allowed hosts unsupported for inline plugin",
			pluginYAML: `from:
  allowed_hosts:
    - api.example.test
surfaces:
  openapi:
    document: https://api.example.test/openapi.json`,
			wantError: "plugin.allowed_hosts is only valid when the plugin runs as an executable process; remove plugin.allowed_hosts or switch this integration to plugin.source",
		},
		{
			name: "headers unsupported without declarative ops or spec surface",
			pluginYAML: `from:
  source:
    path: /tmp/plugin.yaml
headers:
  x-test: value`,
			wantError: "plugin.headers are only valid when the plugin exposes declarative operations or a spec surface; remove plugin.headers or configure declarative operations, OpenAPI, GraphQL, or MCP",
		},
		{
			name: "managed parameters unsupported without api surface",
			pluginYAML: `from:
  source:
    path: /tmp/plugin.yaml
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

func TestE2EValidateRejectsUnsupportedSourcePluginFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		setup      func(t *testing.T, dir string) string
		pluginYAML string
		wantError  string
	}{
		{
			name:  "config headers unsupported for executable-only source plugin",
			setup: setupPluginDir,
			pluginYAML: `from:
  source:
    path: %s
headers:
  x-test: value`,
			wantError: "plugin.headers are only valid when the plugin exposes declarative operations or a spec surface; remove plugin.headers or configure declarative operations, OpenAPI, GraphQL, or MCP",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			pluginDir := tc.setup(t, dir)
			manifestPath, err := pluginpkg.FindManifestFile(pluginDir)
			if err != nil {
				t.Fatalf("FindManifestFile(%s): %v", pluginDir, err)
			}
			cfgPath := filepath.Join(dir, "config.yaml")
			pluginYAML := fmt.Sprintf(tc.pluginYAML, manifestPath)
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

			out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
			if err == nil {
				t.Fatalf("expected validate to fail for unsupported source plugin field, output: %s", out)
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
      source:
        path: /tmp/plugin.yaml
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
	testutil.CopyExampleProviderPlugin(t, pluginDir)
	manifest := &pluginmanifestv1.Manifest{
		Source:      "github.com/test/plugins/provider",
		Version:     version,
		DisplayName: "Example Provider",
		Description: "A minimal example provider built with the public SDK",
		Kinds:       []string{pluginmanifestv1.KindProvider},
		Provider:    &pluginmanifestv1.Provider{},
	}
	writeManifestFile(t, pluginDir, manifest)
	return pluginDir
}

func writeManifestFile(t *testing.T, pluginDir string, manifest *pluginmanifestv1.Manifest) {
	t.Helper()
	data, err := pluginpkg.EncodeSourceManifestFormat(manifest, pluginpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.yaml"), data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func writeE2EConfig(t *testing.T, dir, pluginDir string, port int) string {
	t.Helper()
	return writeE2EConfigWithPaths(t, dir, pluginDir, filepath.Join(dir, "gestalt.db"), "", port)
}

func writeSplitListenerE2EConfig(t *testing.T, dir, pluginDir string, publicPort, managementPort int) string {
	t.Helper()

	if publicPort == 0 {
		publicPort = 18080
	}
	if managementPort == 0 {
		managementPort = 19090
	}
	manifestPath, err := pluginpkg.FindManifestFile(pluginDir)
	if err != nil {
		t.Fatalf("FindManifestFile(%s): %v", pluginDir, err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := fmt.Sprintf(`auth:
  provider: none
datastore:
  provider: sqlite
  config:
    path: %s
server:
  encryption_key: test-e2e-key
  base_url: https://gestalt.example.test
  public:
    port: %d
  management:
    host: 127.0.0.1
    port: %d
providers:
  example:
    from:
      source:
        path: %s
`, filepath.Join(dir, "gestalt.db"), publicPort, managementPort, manifestPath)

	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath
}

func writeE2EConfigWithPaths(t *testing.T, dir, pluginDir, dbPath, artifactsDir string, port int) string {
	t.Helper()

	if port == 0 {
		port = 18080
	}
	manifestPath, err := pluginpkg.FindManifestFile(pluginDir)
	if err != nil {
		t.Fatalf("FindManifestFile(%s): %v", pluginDir, err)
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
      source:
        path: %s
`, dbPath, serverBlock, manifestPath)

	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath
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

func serveLockedAndExerciseWithManagement(t *testing.T, cfgPath string, publicPort, managementPort int, artifactsDir string, exercise func(t *testing.T, publicBaseURL, managementBaseURL string)) (string, string) {
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

	publicBaseURL := fmt.Sprintf("http://localhost:%d", publicPort)
	managementBaseURL := fmt.Sprintf("http://localhost:%d", managementPort)
	waitForReady(t, publicBaseURL, 30*time.Second)
	waitForReady(t, managementBaseURL, 30*time.Second)

	exercise(t, publicBaseURL, managementBaseURL)

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

		body = invokeExampleOperation(t, baseURL, "status", `{}`, http.StatusOK)
		result = map[string]any{}
		if err := json.Unmarshal(body, &result); err != nil {
			t.Fatalf("unmarshal status: %v\nbody: %s", err, body)
		}
		if result["name"] != "example" {
			t.Fatalf("expected configured name=example, got %v", result)
		}
		if result["greeting"] != "" {
			t.Fatalf("expected empty greeting without provider config, got %v", result)
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

	port := allocateTestPort(t)
	cfgPath := writeHybridAPIPluginConfig(t, dir, pluginDir, port)

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

func TestE2EHybridSpecLoadedSourceKeepsExecutableAndAllowedOperations(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := setupHybridSpecLoadedPluginDir(t, dir)

	port := allocateTestPort(t)
	cfgPath := writeE2EConfig(t, dir, pluginDir, port)

	out, err := exec.Command(gestaltdBin, "init", "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd init: %v\n%s", err, out)
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
		t.Fatalf("operation ids = %v, want echo from the executable provider", ids)
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

func TestE2EGraphQLOperationsExposeDisplayReadyParameters(t *testing.T) {
	t.Parallel()

	schemaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"__schema":{"queryType":{"name":"Query"},"mutationType":{"name":"Mutation"},"types":[{"kind":"OBJECT","name":"Query","description":"","fields":[],"inputFields":null,"enumValues":null},{"kind":"OBJECT","name":"Mutation","description":"","fields":[{"name":"createIssue","description":"Create an issue","args":[{"name":"input","description":"","type":{"kind":"NON_NULL","name":null,"ofType":{"kind":"INPUT_OBJECT","name":"CreateIssueInput","ofType":null}},"defaultValue":null}],"type":{"kind":"OBJECT","name":"IssuePayload","ofType":null}}],"inputFields":null,"enumValues":null},{"kind":"INPUT_OBJECT","name":"CreateIssueInput","description":"","fields":null,"inputFields":[{"name":"title","description":"","type":{"kind":"NON_NULL","name":null,"ofType":{"kind":"SCALAR","name":"String","ofType":null}},"defaultValue":null},{"name":"teamId","description":"","type":{"kind":"NON_NULL","name":null,"ofType":{"kind":"SCALAR","name":"String","ofType":null}},"defaultValue":null},{"name":"priority","description":"","type":{"kind":"ENUM","name":"IssuePriority","ofType":null},"defaultValue":null}],"enumValues":null},{"kind":"ENUM","name":"IssuePriority","description":"","fields":null,"inputFields":null,"enumValues":[{"name":"low","description":""},{"name":"high","description":""}]},{"kind":"OBJECT","name":"IssuePayload","description":"","fields":[{"name":"success","description":"","args":[],"type":{"kind":"SCALAR","name":"Boolean","ofType":null}}],"inputFields":null,"enumValues":null}]}}}`)
	}))
	defer schemaSrv.Close()

	dir := t.TempDir()
	port := allocateTestPort(t)
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := fmt.Sprintf(`auth:
  provider: none
datastore:
  provider: sqlite
  config:
    path: %s
server:
  port: %d
  encryption_key: test-graphql-key
providers:
  example:
    surfaces:
      graphql:
        url: %s
`, filepath.Join(dir, "gestalt.db"), port, schemaSrv.URL)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := exec.Command(gestaltdBin, "serve", "--config", cfgPath)
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

	var ops []struct {
		ID         string `json:"id"`
		Parameters []struct {
			Name     string `json:"name"`
			Type     string `json:"type"`
			Required bool   `json:"required"`
		} `json:"parameters"`
	}
	if err := json.Unmarshal(getEndpointBody(t, baseURL+"/api/v1/integrations/example/operations", http.StatusOK), &ops); err != nil {
		t.Fatalf("decode operations: %v", err)
	}

	var createIssueParams []struct {
		Name     string `json:"name"`
		Type     string `json:"type"`
		Required bool   `json:"required"`
	}
	for _, op := range ops {
		if op.ID != "createIssue" {
			continue
		}
		createIssueParams = op.Parameters
		break
	}
	if len(createIssueParams) != 1 {
		t.Fatalf("createIssue parameters = %+v, want 1", createIssueParams)
	}
	if createIssueParams[0].Name != "input" || createIssueParams[0].Type != "object{title!, teamId!, priority}" || !createIssueParams[0].Required {
		t.Fatalf("createIssue parameter = %+v", createIssueParams[0])
	}
}

func writeHybridAPIPluginConfig(t *testing.T, dir, pluginDir string, port int) string {
	t.Helper()
	manifestPath, err := pluginpkg.FindManifestFile(pluginDir)
	if err != nil {
		t.Fatalf("FindManifestFile(%s): %v", pluginDir, err)
	}

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
      source:
        path: %s
`, filepath.Join(dir, "gestalt.db"), port, manifestPath)

	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath
}

func setupHybridSpecLoadedPluginDir(t *testing.T, baseDir string) string {
	t.Helper()

	pluginDir := setupPluginDir(t, baseDir)
	specRel := filepath.ToSlash(filepath.Join("specs", "openapi.yaml"))
	if err := os.MkdirAll(filepath.Join(pluginDir, "specs"), 0o755); err != nil {
		t.Fatalf("MkdirAll specs dir: %v", err)
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

	manifest := &pluginmanifestv1.Manifest{
		Source:      "github.com/test/plugins/hybrid-spec-loaded",
		Version:     "0.1.0",
		DisplayName: "Example Provider",
		Description: "A minimal example provider built with the public SDK",
		Kinds:       []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			OpenAPI: specRel,
			AllowedOperations: map[string]*pluginmanifestv1.ManifestOperationOverride{
				"gmail.users.messages.list": {Alias: "messages.list"},
				"gmail.users.getProfile":    {Alias: "getProfile"},
			},
		},
	}
	writeManifestFile(t, pluginDir, manifest)

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
