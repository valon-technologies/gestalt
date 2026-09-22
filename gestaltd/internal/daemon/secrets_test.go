package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMainUsageIncludesSecretsCommand(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	printMainUsage(&output)
	if !strings.Contains(output.String(), "gestaltd secrets") {
		t.Fatalf("main usage does not contain secrets command:\n%s", output.String())
	}
}

func TestRunSecretsUsage(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	printSecretsUsage(&output)
	for _, want := range []string{"create", "rotate", "list", "describe", "audit", "preflight", "retire", "GESTALT_URL"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("secrets usage does not contain %q:\n%s", want, output.String())
		}
	}
}

func TestRunSecretsHelpDoesNotRequireConfiguration(t *testing.T) {
	t.Setenv("GESTALT_URL", "")
	t.Setenv("GESTALT_API_KEY", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	for _, args := range [][]string{
		{"--help"},
		{"create", "--help"},
		{"rotate", "--help"},
		{"list", "--help"},
		{"describe", "--help"},
		{"audit", "--help"},
		{"preflight", "--help"},
		{"retire", "--help"},
	} {
		if err := runSecrets(args); err != nil && !errors.Is(err, flag.ErrHelp) {
			t.Fatalf("runSecrets(%v) error = %v, want help", args, err)
		}
	}
}

func TestRunSecretsRejectsUnknownCommand(t *testing.T) {
	t.Parallel()

	err := runSecrets([]string{"reveal", "test-secret"})
	if err == nil || !strings.Contains(err.Error(), "unknown secrets command") {
		t.Fatalf("runSecrets() error = %v, want unknown command", err)
	}
}

func TestManagedSecretClientRequestsAndResponses(t *testing.T) {
	t.Parallel()

	var requests []string
	var auth []string
	client := &managedSecretClient{
		BaseURL: "https://gestalt.example",
		Token:   "test-token",
		HTTPClient: &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			requests = append(requests, r.Method+" "+r.URL.Path)
			auth = append(auth, r.Header.Get("Authorization"))
			var payload any
			switch r.URL.Path {
			case "/admin/api/v1/managed-secrets":
				payload = []managedSecretSummary{{
					Name: "demo-secret", OwnerApp: "demo", Scope: "app", CurrentVersion: 2,
				}}
			case "/admin/api/v1/managed-secrets/demo-secret":
				payload = managedSecretDetail{
					managedSecretSummary: managedSecretSummary{Name: "demo-secret", OwnerApp: "demo", Scope: "app", CurrentVersion: 2},
					Audit:                []managedSecretAuditRow{{Action: "create", Version: 1, Actor: "admin", CreatedAt: "2026-01-01T00:00:00Z"}},
				}
			case "/admin/api/v1/managed-secrets/demo-secret/retire":
				payload = managedSecretSummary{Name: "demo-secret"}
			default:
				return textResponse(http.StatusNotFound, `{"error":"not found"}`), nil
			}
			encoded, _ := json.Marshal(payload)
			return textResponse(http.StatusOK, string(encoded)), nil
		})},
	}

	var summaries []managedSecretSummary
	if err := client.get(context.Background(), managedSecretsAdminPath, &summaries); err != nil {
		t.Fatalf("get secrets: %v", err)
	}
	if len(summaries) != 1 || summaries[0].Name != "demo-secret" {
		t.Fatalf("get secrets = %#v", summaries)
	}

	var detail managedSecretDetail
	if err := client.get(context.Background(), managedSecretsAdminPath+"/demo-secret", &detail); err != nil {
		t.Fatalf("get secret detail: %v", err)
	}
	if len(detail.Audit) != 1 || detail.Audit[0].Action != "create" {
		t.Fatalf("get secret detail = %#v", detail)
	}

	var retired managedSecretSummary
	if err := client.post(context.Background(), managedSecretsAdminPath+"/demo-secret/retire", map[string]string{"reason": "unused"}, &retired); err != nil {
		t.Fatalf("retire secret: %v", err)
	}
	if want := []string{"GET /admin/api/v1/managed-secrets", "GET /admin/api/v1/managed-secrets/demo-secret", "POST /admin/api/v1/managed-secrets/demo-secret/retire"}; strings.Join(requests, ",") != strings.Join(want, ",") {
		t.Fatalf("requests = %v", requests)
	}
	for _, value := range auth {
		if value != "Bearer test-token" {
			t.Fatalf("authorization = %q", value)
		}
	}
}

func TestManagedSecretClientReturnsAPIError(t *testing.T) {
	t.Parallel()

	client := &managedSecretClient{
		BaseURL: "https://gestalt.example",
		Token:   "test-token",
		HTTPClient: &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return textResponse(http.StatusBadRequest, `{"error":"bad request"}`), nil
		})},
	}
	err := client.get(context.Background(), managedSecretsAdminPath, &struct{}{})
	if err == nil || !strings.Contains(err.Error(), "bad request") {
		t.Fatalf("get error = %v, want API error", err)
	}
}

func TestRunManagedSecretWriteValidation(t *testing.T) {
	t.Parallel()

	// No client is constructed, so local argument validation cannot depend
	// on credentials, configuration, or network reachability.
	clientOnce := func() (*managedSecretClient, error) {
		t.Fatal("client should not be constructed during local validation")
		return nil, nil
	}

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "requires name", args: []string{"--reason", "test"}, want: "secret name is required"},
		{name: "requires reason", args: []string{"demo-secret"}, want: "--reason is required"},
		{name: "rejects generate with file", args: []string{"demo-secret", "--reason", "test", "--generate", "--file", "/tmp/value"}, want: "--generate and --file are mutually exclusive"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := runManagedSecretWrite(tt.args, "create", clientOnce)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("runManagedSecretWrite() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestRunManagedSecretWriteReadsPipedStdin(t *testing.T) {
	t.Parallel()

	restoreStdin(t, "piped-value")
	client := managedSecretWriteRecorder()
	err := runManagedSecretWrite([]string{"demo-secret", "--reason", "test"}, "create", func() (*managedSecretClient, error) {
		return client.managedSecretClient, nil
	})
	if err != nil {
		t.Fatalf("runManagedSecretWrite() error = %v", err)
	}
	if client.body["value"] != "piped-value" {
		t.Fatalf("request value = %#v, want piped stdin bytes", client.body["value"])
	}
}

func TestValidateManagedSecretValue(t *testing.T) {
	t.Parallel()

	if _, err := validateManagedSecretValue([]byte("value")); err != nil {
		t.Fatalf("valid value error = %v", err)
	}
	if _, err := validateManagedSecretValue([]byte("")); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("empty value error = %v", err)
	}
	if _, err := validateManagedSecretValue(bytes.Repeat([]byte("a"), managedSecretMaxBytes+1)); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized value error = %v", err)
	}
}

func TestRunManagedSecretWriteUsesFile(t *testing.T) {
	t.Parallel()

	valuePath := filepath.Join(t.TempDir(), "value.txt")
	if err := os.WriteFile(valuePath, []byte("file-value"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := managedSecretWriteRecorder()
	err := runManagedSecretWrite(
		[]string{"demo-secret", "--reason", "test", "--owner-app", "demo", "--file", valuePath},
		"create",
		func() (*managedSecretClient, error) { return client.managedSecretClient, nil },
	)
	if err != nil {
		t.Fatalf("runManagedSecretWrite() error = %v", err)
	}
	if client.body["value"] != "file-value" || client.body["source"] != "gestaltd-cli" || client.body["operation"] != "create" {
		t.Fatalf("request body = %#v", client.body)
	}
}

type managedSecretWriteRecorderClient struct {
	*managedSecretClient
	body map[string]any
}

func managedSecretWriteRecorder() *managedSecretWriteRecorderClient {
	recorder := &managedSecretWriteRecorderClient{body: map[string]any{}}
	recorder.managedSecretClient = &managedSecretClient{
		BaseURL: "https://gestalt.example",
		Token:   "test-token",
		HTTPClient: &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != managedSecretsAdminPath {
				return textResponse(http.StatusNotFound, "{}"), nil
			}
			if err := json.NewDecoder(r.Body).Decode(&recorder.body); err != nil {
				return nil, err
			}
			encoded, _ := json.Marshal(managedSecretWriteResponse{
				Secret: managedSecretSummary{Name: "demo-secret", OwnerApp: "demo", Scope: "app"},
				Version: managedSecretVersionSummary{
					Version: 1, CreatedAt: "2026-01-01T00:00:00Z",
				},
			})
			return textResponse(http.StatusOK, string(encoded)), nil
		})},
	}
	return recorder
}

func TestRunManagedSecretPreflightFailsNonZero(t *testing.T) {
	t.Parallel()

	// The public wrapper reads references from stdin. Replace stdin with a
	// minimal valid references document so the test reaches the API client.
	restoreStdin(t, `[]`)
	var err error

	client := &managedSecretClient{
		BaseURL: "https://gestalt.example",
		Token:   "test-token",
		HTTPClient: &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			encoded, _ := json.Marshal(managedSecretPreflightResponse{Missing: []managedSecretReference{{Name: "missing-secret"}}})
			return textResponse(http.StatusOK, string(encoded)), nil
		})},
	}
	err = runManagedSecretPreflight([]string{}, func() (*managedSecretClient, error) { return client, nil })
	var exitErr exitCodeError
	if !errors.As(err, &exitErr) || exitErr.code != 1 {
		t.Fatalf("preflight error = %#v, want exit code 1", err)
	}
}

func TestNewManagedSecretClientRequiresURLAndToken(t *testing.T) {
	t.Setenv("GESTALT_URL", "")
	t.Setenv("GESTALT_API_KEY", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if _, err := newManagedSecretClient(); err == nil || !strings.Contains(err.Error(), "URL is required") {
		t.Fatalf("newManagedSecretClient() error = %v, want URL error", err)
	}
	t.Setenv("GESTALT_URL", "https://gestalt.example")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"loginSupported":true}`))
	}))
	defer server.Close()
	t.Setenv("GESTALT_URL", server.URL)
	if _, err := newManagedSecretClient(); err == nil || !strings.Contains(err.Error(), "credentials are required") {
		t.Fatalf("newManagedSecretClient() error = %v, want credentials error", err)
	}
}

func TestNewManagedSecretClientAllowsAuthDisabledServerWithoutToken(t *testing.T) {
	t.Setenv("GESTALT_URL", "")
	t.Setenv("GESTALT_API_KEY", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"loginSupported":false}`))
	}))
	defer server.Close()
	t.Setenv("GESTALT_URL", server.URL)

	client, err := newManagedSecretClient()
	if err != nil {
		t.Fatalf("newManagedSecretClient() error = %v", err)
	}
	if client.Token != "" {
		t.Fatalf("client token = %q, want empty", client.Token)
	}
}

// restoreStdin replaces os.Stdin for a test and restores it afterward.
func restoreStdin(t *testing.T, input string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	replacement, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdin
	os.Stdin = replacement
	t.Cleanup(func() {
		_ = replacement.Close()
		os.Stdin = original
	})
}

func textResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
