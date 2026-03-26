package testutil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

type GestaltdProcess struct {
	BaseURL    string
	ConfigPath string
	Client     *http.Client

	cmd    *exec.Cmd
	stdout bytes.Buffer
	stderr bytes.Buffer
	done   chan error
}

type GestaltdCommandResult struct {
	Output string
	Err    error
}

var (
	gestaltdBinaryOnce sync.Once
	gestaltdBinaryPath string
	gestaltdBinaryErr  error
)

func FreePort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for free port: %v", err)
	}
	defer func() { _ = listener.Close() }()

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("unexpected listener address type %T", listener.Addr())
	}
	return addr.Port
}

func FindFreePort(t *testing.T) int {
	t.Helper()
	return FreePort(t)
}

func AllocatePort(t *testing.T) int {
	t.Helper()
	return FreePort(t)
}

func RunGestaltd(t *testing.T, args ...string) GestaltdCommandResult {
	t.Helper()

	cmd := exec.Command(gestaltdBinary(t), args...)
	cmd.Dir = RepoRoot(t)

	out, err := cmd.CombinedOutput()
	return GestaltdCommandResult{
		Output: string(out),
		Err:    err,
	}
}

func StartGestaltd(t *testing.T, configPath string, port int) *GestaltdProcess {
	t.Helper()

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	cmd := exec.Command(gestaltdBinary(t), "--config", configPath)
	cmd.Dir = RepoRoot(t)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}

	proc := &GestaltdProcess{
		BaseURL:    baseURL,
		ConfigPath: configPath,
		Client:     &http.Client{Jar: jar},
		cmd:        cmd,
		done:       make(chan error, 1),
	}
	cmd.Stdout = &proc.stdout
	cmd.Stderr = &proc.stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start gestaltd: %v", err)
	}

	go func() {
		proc.done <- cmd.Wait()
	}()

	if err := proc.waitForReady(45 * time.Second); err != nil {
		_ = proc.stop(2 * time.Second)
		t.Fatalf("wait for gestaltd readiness: %v\nstdout:\n%s\nstderr:\n%s", err, proc.stdout.String(), proc.stderr.String())
	}

	t.Cleanup(func() {
		if err := proc.stop(5 * time.Second); err != nil {
			t.Fatalf("stop gestaltd: %v\nstdout:\n%s\nstderr:\n%s", err, proc.stdout.String(), proc.stderr.String())
		}
	})

	return proc
}

func RepoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve repo root: runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func gestaltdBinary(t *testing.T) string {
	t.Helper()

	gestaltdBinaryOnce.Do(func() {
		dir, err := os.MkdirTemp("", "gestaltd-bin-*")
		if err != nil {
			gestaltdBinaryErr = fmt.Errorf("create temp binary dir: %w", err)
			return
		}

		path := filepath.Join(dir, "gestaltd")
		cmd := exec.Command("go", "build", "-o", path, "./cmd/gestaltd")
		cmd.Dir = RepoRoot(t)
		out, err := cmd.CombinedOutput()
		if err != nil {
			gestaltdBinaryErr = fmt.Errorf("build gestaltd: %w\n%s", err, out)
			return
		}
		gestaltdBinaryPath = path
	})

	if gestaltdBinaryErr != nil {
		t.Fatal(gestaltdBinaryErr)
	}
	return gestaltdBinaryPath
}

func WriteConfigFile(t *testing.T, dir, body string) string {
	t.Helper()

	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	return path
}

func (p *GestaltdProcess) Logs() string {
	return p.stdout.String() + p.stderr.String()
}

func (p *GestaltdProcess) waitForReady(timeout time.Duration) error {
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		select {
		case err := <-p.done:
			return fmt.Errorf("process exited before ready: %w", err)
		default:
		}

		resp, err := client.Get(p.BaseURL + "/ready")
		if err == nil {
			func() {
				defer func() { _ = resp.Body.Close() }()
				if resp.StatusCode == http.StatusOK {
					err = nil
				} else {
					err = fmt.Errorf("unexpected /ready status %d", resp.StatusCode)
				}
			}()
			if err == nil {
				return nil
			}
		}

		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("timed out waiting for %s/ready", p.BaseURL)
}

func (p *GestaltdProcess) stop(timeout time.Duration) error {
	if p.cmd.Process == nil {
		return nil
	}

	if err := p.cmd.Process.Signal(os.Interrupt); err != nil && !isDone(p.done) {
		return fmt.Errorf("interrupt gestaltd: %w", err)
	}

	select {
	case err := <-p.done:
		if err != nil {
			return err
		}
		return nil
	case <-time.After(timeout):
	}

	if err := p.cmd.Process.Kill(); err != nil {
		return fmt.Errorf("kill gestaltd: %w", err)
	}

	select {
	case err := <-p.done:
		if err != nil {
			return err
		}
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("timed out waiting for process exit")
	}
}

func isDone(ch <-chan error) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func DevLogin(t *testing.T, client *http.Client, baseURL, email string) {
	t.Helper()

	status, body := DoJSON(t, client, http.MethodPost, baseURL+"/api/dev-login", map[string]string{
		"email": email,
	})
	if status != http.StatusOK {
		t.Fatalf("dev login status=%d body=%s", status, string(body))
	}
}

func DoJSON(t *testing.T, client *http.Client, method, url string, payload any) (int, []byte) {
	t.Helper()

	var body io.Reader
	if payload != nil {
		buf := new(bytes.Buffer)
		if err := json.NewEncoder(buf).Encode(payload); err != nil {
			t.Fatalf("encode JSON payload: %v", err)
		}
		body = buf
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	return resp.StatusCode, respBody
}

func DecodeJSON[T any](t *testing.T, body []byte) T {
	t.Helper()

	var value T
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatalf("decode JSON: %v\nbody=%s", err, body)
	}
	return value
}
