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
	BaseURL string

	cmd    *exec.Cmd
	stdout bytes.Buffer
	stderr bytes.Buffer
	done   chan error
}

var gestaltdBinary struct {
	once sync.Once
	path string
	err  error
}

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

func StartGestaltd(t *testing.T, configPath string, port int) *GestaltdProcess {
	t.Helper()

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	cmd := exec.Command(gestaltdBinaryPath(t), "--config", configPath)
	cmd.Dir = RepoRoot(t)

	proc := &GestaltdProcess{
		BaseURL: baseURL,
		cmd:     cmd,
		done:    make(chan error, 1),
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

func WriteConfigFile(t *testing.T, dir, body string) string {
	t.Helper()

	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	return path
}

func gestaltdBinaryPath(t *testing.T) string {
	t.Helper()

	gestaltdBinary.once.Do(func() {
		dir, err := os.MkdirTemp("", "gestaltd-bin-*")
		if err != nil {
			gestaltdBinary.err = fmt.Errorf("create temp dir for gestaltd binary: %w", err)
			return
		}

		bin := filepath.Join(dir, "gestaltd")
		cmd := exec.Command("go", "build", "-o", bin, "./cmd/gestaltd")
		cmd.Dir = RepoRoot(t)

		out, err := cmd.CombinedOutput()
		if err != nil {
			gestaltdBinary.err = fmt.Errorf("build gestaltd: %w\n%s", err, out)
			return
		}
		gestaltdBinary.path = bin
	})

	if gestaltdBinary.err != nil {
		t.Fatal(gestaltdBinary.err)
	}
	return gestaltdBinary.path
}

func NewCookieClient(t *testing.T) *http.Client {
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
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, url, err)
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

	var out T
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal response: %v\nbody=%s", err, string(body))
	}
	return out
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
