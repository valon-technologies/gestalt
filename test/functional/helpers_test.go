package functional

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/core/crypto"
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/server"
	"github.com/valon-technologies/gestalt/plugins/auth/google"
	"github.com/valon-technologies/gestalt/plugins/datastore/sqlite"
	"github.com/valon-technologies/gestalt/plugins/providers/echo"
	secretsenv "github.com/valon-technologies/gestalt/plugins/secrets/env"
)

type functionalServer struct {
	BaseURL string
	Client  *http.Client
}

type gestaltdProcess struct {
	BaseURL string
	Client  *http.Client
}

var (
	gestaltdBinaryOnce sync.Once
	gestaltdBinaryPath string
	gestaltdBinaryErr  error
)

func repoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not determine repo root")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func buildGestaltdBinary(t *testing.T) string {
	t.Helper()

	gestaltdBinaryOnce.Do(func() {
		dir, err := os.MkdirTemp("", "gestaltd-bin-*")
		if err != nil {
			gestaltdBinaryErr = err
			return
		}

		gestaltdBinaryPath = filepath.Join(dir, "gestaltd")
		cmd := exec.Command("go", "build", "-o", gestaltdBinaryPath, "./cmd/gestaltd")
		cmd.Dir = repoRoot(t)
		if out, err := cmd.CombinedOutput(); err != nil {
			gestaltdBinaryErr = fmt.Errorf("go build gestaltd: %w\n%s", err, out)
		}
	})

	if gestaltdBinaryErr != nil {
		t.Fatal(gestaltdBinaryErr)
	}
	return gestaltdBinaryPath
}

func runGestaltdCommand(t *testing.T, args ...string) string {
	t.Helper()

	cmd := exec.Command(buildGestaltdBinary(t), args...)
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}

func startGestaltdProcess(t *testing.T, cfgPath string, port int) *gestaltdProcess {
	t.Helper()

	cmd := exec.Command(buildGestaltdBinary(t), "serve", "--config", cfgPath)
	cmd.Dir = repoRoot(t)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("starting gestaltd: %v", err)
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitForHealthyServer(t, cmd, baseURL, &stdout, &stderr)

	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)

		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()

		select {
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		case <-done:
		}
	})

	return &gestaltdProcess{
		BaseURL: baseURL,
		Client:  newCookieClient(t),
	}
}

func waitForHealthyServer(t *testing.T, cmd *exec.Cmd, baseURL string, stdout, stderr *bytes.Buffer) {
	t.Helper()

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}

	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	t.Fatalf("gestaltd did not become healthy\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
}

func functionalFactories() *bootstrap.FactoryRegistry {
	factories := bootstrap.NewFactoryRegistry()
	factories.Auth["google"] = google.Factory
	factories.Datastores["sqlite"] = sqlite.Factory
	factories.Secrets["env"] = secretsenv.Factory
	factories.Builtins = append(factories.Builtins, echo.New())
	return factories
}

func startFunctionalServer(t *testing.T, cfgPath string) *functionalServer {
	t.Helper()

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	result, err := bootstrap.Bootstrap(ctx, cfg, functionalFactories())
	if err != nil {
		cancel()
		t.Fatalf("bootstrap: %v", err)
	}

	if err := result.Datastore.Migrate(ctx); err != nil {
		cancel()
		t.Fatalf("migrate datastore: %v", err)
	}

	<-result.ProvidersReady

	srv, err := server.New(server.Config{
		Auth:        result.Auth,
		Datastore:   result.Datastore,
		Providers:   result.Providers,
		Runtimes:    result.Runtimes,
		Bindings:    result.Bindings,
		Invoker:     result.Invoker,
		DevMode:     result.DevMode,
		StateSecret: crypto.DeriveKey(cfg.Server.EncryptionKey),
	})
	if err != nil {
		cancel()
		t.Fatalf("create server: %v", err)
	}

	ts := httptest.NewServer(srv)
	client := newCookieClient(t)

	t.Cleanup(func() {
		ts.Close()
		cancel()

		if result.Bindings != nil {
			_ = bootstrap.CloseBindings(result.Bindings, result.Bindings.List())
		}
		if result.Runtimes != nil {
			_ = bootstrap.StopRuntimes(context.Background(), result.Runtimes, result.Runtimes.List())
		}
		_ = bootstrap.CloseProviders(result.Providers)
		_ = result.Datastore.Close()
		if closer, ok := result.SecretManager.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	})

	return &functionalServer{
		BaseURL: ts.URL,
		Client:  client,
	}
}

func newCookieClient(t *testing.T) *http.Client {
	t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	return &http.Client{
		Jar:     jar,
		Timeout: 10 * time.Second,
	}
}

func devLogin(t *testing.T, client *http.Client, baseURL, email string) {
	t.Helper()

	status, body := doJSON(t, client, http.MethodPost, baseURL+"/api/dev-login", map[string]string{
		"email": email,
	})
	if status != http.StatusOK {
		t.Fatalf("dev login status=%d body=%s", status, string(body))
	}
}

func doJSON(t *testing.T, client *http.Client, method, url string, payload any) (int, []byte) {
	t.Helper()

	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	return resp.StatusCode, data
}

func decodeJSON[T any](t *testing.T, body []byte) T {
	t.Helper()

	var out T
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode json %s: %v", string(body), err)
	}
	return out
}

func freePort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	defer func() { _ = listener.Close() }()

	return listener.Addr().(*net.TCPAddr).Port
}

func writeConfig(t *testing.T, dir string, port int, integrations string) string {
	t.Helper()

	cfgPath := filepath.Join(dir, "config.yaml")
	redirectURL := fmt.Sprintf("http://127.0.0.1:%d%s", max(port, 8080), config.AuthCallbackPath)
	cfg := fmt.Sprintf(`auth:
  provider: google
  config:
    client_id: "test-client"
    client_secret: "test-secret"
    redirect_url: %q
datastore:
  provider: sqlite
  config:
    path: %q
server:
  dev_mode: true
  encryption_key: "test-encryption-key"
`, redirectURL, filepath.Join(dir, "gestalt.db"))

	if port > 0 {
		cfg += fmt.Sprintf("  port: %d\n", port)
	}
	if integrations != "" {
		cfg += "integrations:\n" + integrations
	}

	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath
}

func buildPluginFixture(t *testing.T, dir string) string {
	t.Helper()

	source := filepath.Join(dir, "plugin-src")
	artifactPath := filepath.Join(source, "artifacts", runtime.GOOS, runtime.GOARCH)
	if err := os.MkdirAll(artifactPath, 0o755); err != nil {
		t.Fatalf("mkdir artifact dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(artifactPath, "provider"), []byte("provider"), 0o755); err != nil {
		t.Fatalf("write provider artifact: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(source, "schemas"), 0o755); err != nil {
		t.Fatalf("mkdir schema dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, "schemas", "config.schema.json"), []byte(`{"type":"object"}`), 0o644); err != nil {
		t.Fatalf("write schema: %v", err)
	}

	artifactRel := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "provider"))
	manifest := fmt.Sprintf(`{
  "schema_version": 1,
  "id": "acme/provider",
  "version": "0.1.0",
  "kinds": ["provider"],
  "provider": {
    "protocol": { "min": 1, "max": 1 },
    "config_schema_path": "schemas/config.schema.json"
  },
  "artifacts": [
    {
      "os": %q,
      "arch": %q,
      "path": %q,
      "sha256": "5c4c1964340aca5b65393bbe9d3249cdd71be26665b3320ad694f034f2743283"
    }
  ],
  "entrypoints": {
    "provider": {
      "artifact_path": %q
    }
  }
}`, runtime.GOOS, runtime.GOARCH, artifactRel, artifactRel)
	if err := os.WriteFile(filepath.Join(source, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	return source
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
