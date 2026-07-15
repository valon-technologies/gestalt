package providerpkg

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPhaseCommandWritersCaptureOnFailure(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.yaml")
	if err := os.WriteFile(manifestPath, []byte("source: test\nkind: app\nversion: 1.0.0\n"), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	buildScript := filepath.Join(dir, "build.sh")
	if err := os.WriteFile(buildScript, []byte("#!/bin/sh\nprintf 'BUILD_DIAG_STDOUT\\n' >&1\nprintf 'BUILD_DIAG_STDERR\\n' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write build script: %v", err)
	}

	var captured bytes.Buffer
	opts := SourceBuildOptions{
		Output: CommandOutputCaptureOnFailure(&captured),
	}
	err := runSourcePhase(manifestPath, "build", "", []string{"sh", "./build.sh"}, nil, opts)
	if err == nil {
		t.Fatal("runSourcePhase() error = nil, want failure")
	}
	if !strings.Contains(err.Error(), "run build.command") {
		t.Fatalf("runSourcePhase() error = %v, want wrapped build failure", err)
	}
	got := captured.String()
	for _, want := range []string{"BUILD_DIAG_STDOUT", "BUILD_DIAG_STDERR"} {
		if !strings.Contains(got, want) {
			t.Fatalf("captured output = %q, want %q", got, want)
		}
	}
}

func TestPhaseCommandWritersCaptureOnSuccessDiscards(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.yaml")
	if err := os.WriteFile(manifestPath, []byte("source: test\nkind: app\nversion: 1.0.0\n"), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	buildScript := filepath.Join(dir, "build.sh")
	if err := os.WriteFile(buildScript, []byte("#!/bin/sh\nprintf 'BUILD_CHATTER\\n'\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write build script: %v", err)
	}

	var captured bytes.Buffer
	opts := SourceBuildOptions{
		Output: CommandOutputCaptureOnFailure(&captured),
	}
	if err := runSourcePhase(manifestPath, "build", "", []string{"sh", "./build.sh"}, nil, opts); err != nil {
		t.Fatalf("runSourcePhase() error = %v", err)
	}
	if got := captured.String(); got != "" {
		t.Fatalf("captured output = %q, want empty on success", got)
	}
}

func TestBoundedCaptureTruncates(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	capture := newCommandCaptureSession(&buf, 8)
	if _, err := capture.stdout.Write([]byte("1234567890")); err != nil {
		t.Fatalf("write stdout: %v", err)
	}
	capture.flush()
	if got := buf.String(); got != "12345678"+captureTruncatedNotice {
		t.Fatalf("truncated capture = %q, want %q", got, "12345678"+captureTruncatedNotice)
	}
}
