package otlp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestNew_StdoutLogsUsesStdoutLogger(t *testing.T) { //nolint:paralleltest // mutates os.Stdout
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	endpoint := strings.TrimPrefix(server.URL, "http://")

	originalStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating stdout pipe: %v", err)
	}
	os.Stdout = writer
	t.Cleanup(func() {
		os.Stdout = originalStdout
		_ = reader.Close()
	})

	provider, err := New(context.Background(), yamlConfig{
		Endpoint: endpoint,
		Protocol: "http",
		Insecure: true,
		Logs: logsConfig{
			Exporter: "stdout",
			Format:   "json",
			Level:    "info",
		},
	})
	if err != nil {
		t.Fatalf("creating provider: %v", err)
	}
	t.Cleanup(func() {
		if shutdownErr := provider.Shutdown(context.Background()); shutdownErr != nil {
			t.Fatalf("shutting down provider: %v", shutdownErr)
		}
	})

	if provider.lp != nil {
		t.Fatal("expected OTLP logger provider to be disabled when logs.exporter=stdout")
	}

	provider.Logger().Info("hello", "foo", "bar")

	_ = writer.Close()
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("reading stdout output: %v", err)
	}

	got := string(output)
	if !strings.Contains(got, `"msg":"hello"`) {
		t.Fatalf("expected JSON log message in stdout output, got %q", got)
	}
	if !strings.Contains(got, `"foo":"bar"`) {
		t.Fatalf("expected structured attribute in stdout output, got %q", got)
	}
}

func TestNew_InvalidLogsExporterReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	endpoint := strings.TrimPrefix(server.URL, "http://")

	_, err := New(context.Background(), yamlConfig{
		Endpoint: endpoint,
		Protocol: "http",
		Insecure: true,
		Logs: logsConfig{
			Exporter: "bogus",
		},
	})
	if err == nil {
		t.Fatal("expected an error for an unknown logs exporter")
	}
	if !strings.Contains(err.Error(), `unknown logs exporter "bogus"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}
