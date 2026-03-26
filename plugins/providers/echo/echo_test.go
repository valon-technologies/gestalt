package echo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/plugins/providers/echo"
)

func TestProviderExposesOperationAndExecutes(t *testing.T) {
	t.Parallel()

	p := echo.New()

	if p.Name() != "echo" {
		t.Fatalf("expected name echo, got %q", p.Name())
	}
	if p.ConnectionMode() != core.ConnectionModeNone {
		t.Fatalf("expected connection mode none, got %q", p.ConnectionMode())
	}

	ops := p.ListOperations()
	if len(ops) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(ops))
	}
	if ops[0].Name != "echo" {
		t.Fatalf("expected operation echo, got %q", ops[0].Name)
	}
	if ops[0].Method != http.MethodPost {
		t.Fatalf("expected method POST, got %q", ops[0].Method)
	}

	params := map[string]any{"message": "hello", "count": float64(42)}
	result, err := p.Execute(context.Background(), "echo", params, "")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("expected status 200, got %d", result.Status)
	}

	var echoed map[string]any
	if err := json.Unmarshal([]byte(result.Body), &echoed); err != nil {
		t.Fatalf("unmarshaling body: %v", err)
	}
	if echoed["message"] != "hello" {
		t.Fatalf("expected message hello, got %v", echoed["message"])
	}
	if echoed["count"] != float64(42) {
		t.Fatalf("expected count 42, got %v", echoed["count"])
	}

	if _, err := p.Execute(context.Background(), "nonexistent", nil, ""); err == nil {
		t.Fatal("expected error for unknown operation")
	}
}
