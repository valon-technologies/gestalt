package providerdev_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/services/providerdev"
)

func TestSupervisorProxiesHTTP(t *testing.T) {
	t.Parallel()
	fakeServerBin := buildFakeDevServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	sup, err := providerdev.Start(ctx, nil, []providerdev.Target{{
		Name:         "demo",
		Kind:         "ui",
		BasePath:     "/demo",
		Workdir:      t.TempDir(),
		Command:      []string{fakeServerBin},
		ReadyTimeout: 10 * time.Second,
	}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(sup.Stop)

	handler := sup.Handlers()["demo"]
	waitForHandlerReady(t, handler, "/demo/")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/demo/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("direct proxy status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != "dev-ok" {
		t.Fatalf("direct proxy body = %q, want dev-ok", got)
	}
}

func TestSupervisorNotReadyReturns503(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	sup, err := providerdev.Start(ctx, nil, []providerdev.Target{{
		Name:         "slow",
		Kind:         "ui",
		BasePath:     "/slow",
		Workdir:      t.TempDir(),
		Command:      []string{runtime.GOOS + "_never_exists_binary"},
		ReadyTimeout: time.Hour,
	}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(sup.Stop)

	rec := &responseRecorder{header: make(http.Header)}
	sup.Handlers()["slow"].ServeHTTP(rec, &http.Request{Method: http.MethodGet, URL: mustParseURL("/slow/")})
	if rec.status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.status)
	}
	if got := rec.header.Get("Retry-After"); got != "1" {
		t.Fatalf("Retry-After = %q, want 1", got)
	}
}

func TestSupervisorProbeStopsOnRestart(t *testing.T) {
	t.Parallel()
	fakeServerBin := buildFakeDevServer(t)
	workdir := t.TempDir()
	attemptsFile := filepath.Join(workdir, "attempts")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	sup, err := providerdev.Start(ctx, nil, []providerdev.Target{{
		Name:     "flaky",
		Kind:     "ui",
		BasePath: "/flaky",
		Workdir:  workdir,
		Command:  []string{fakeServerBin},
		Env: map[string]string{
			"GESTALT_FAKE_ATTEMPTS_FILE": attemptsFile,
			"GESTALT_FAKE_FAIL_UNTIL":    "2",
			"GESTALT_FAKE_PID_FILE":      filepath.Join(workdir, "pid"),
		},
		ReadyTimeout: 10 * time.Second,
	}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(sup.Stop)

	handler := sup.Handlers()["flaky"]
	waitForHandlerStatus(t, handler, "/flaky/", http.StatusOK)

	pid := readPIDFile(t, filepath.Join(workdir, "pid"))
	if proc, err := os.FindProcess(pid); err == nil {
		_ = proc.Signal(syscall.SIGKILL)
	}
	waitUntilProcessDead(t, pid)

	waitForHandlerStatus(t, handler, "/flaky/", http.StatusServiceUnavailable)
	waitForHandlerStatus(t, handler, "/flaky/", http.StatusOK)
}

func TestSupervisorStopTerminatesChild(t *testing.T) {
	t.Parallel()
	fakeServerBin := buildFakeDevServer(t)
	pidFile := filepath.Join(t.TempDir(), "pid")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	sup, err := providerdev.Start(ctx, nil, []providerdev.Target{{
		Name:         "demo",
		Kind:         "ui",
		BasePath:     "/demo",
		Workdir:      t.TempDir(),
		Command:      []string{fakeServerBin},
		Env:          map[string]string{"GESTALT_FAKE_PID_FILE": pidFile},
		ReadyTimeout: 10 * time.Second,
	}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	pid := readPIDFile(t, pidFile)
	sup.Stop()
	waitUntilProcessDead(t, pid)
}

func buildFakeDevServer(t *testing.T) string {
	t.Helper()
	srcDir := filepath.Join(moduleRoot(t), "services", "providerdev", "testdata", "fakedevserver")
	bin := filepath.Join(t.TempDir(), "fakedevserver")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = srcDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build fake dev server: %v\n%s", err, out)
	}
	return bin
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func waitForHandlerReady(t *testing.T, handler http.Handler, path string) {
	t.Helper()
	waitForHandlerStatus(t, handler, path, http.StatusOK)
}

func waitForHandlerStatus(t *testing.T, handler http.Handler, path string, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code == want {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for handler status %d on %s", want, path)
}

func readPIDFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("pid file %s not written", path)
	return 0
}

func waitUntilProcessDead(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		proc, err := os.FindProcess(pid)
		if err != nil {
			return
		}
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("pid %d still running after Stop", pid)
}

type responseRecorder struct {
	header http.Header
	status int
	body   strings.Builder
}

func (r *responseRecorder) Header() http.Header { return r.header }
func (r *responseRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.body.Write(b)
}
func (r *responseRecorder) WriteHeader(statusCode int) { r.status = statusCode }

func mustParseURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return u
}
