package gestalt

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func TestOperationResultFromErrorLogsSanitizedInternalErrors(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer func() { _ = reader.Close() }()

	oldStderr := os.Stderr
	os.Stderr = writer
	defer func() {
		os.Stderr = oldStderr
	}()

	result := operationResultFromError(errors.New("boom"))

	_ = writer.Close()
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("io.ReadAll: %v", err)
	}

	if result.Status != 500 {
		t.Fatalf("status = %d, want 500", result.Status)
	}
	if result.Body != `{"error":"internal error"}` {
		t.Fatalf("body = %q, want %q", result.Body, `{"error":"internal error"}`)
	}
	if !strings.Contains(string(output), "internal error in Gestalt operation: boom") {
		t.Fatalf("stderr = %q, want log containing %q", string(output), "internal error in Gestalt operation: boom")
	}
}
