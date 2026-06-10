package gestalt

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDecodeAppResultSharedFixtures(t *testing.T) {
	fixture := func(name string) []byte {
		body, err := os.ReadFile(filepath.Join("..", "testdata", "app_invoke", name))
		if err != nil {
			t.Fatalf("read fixture %s: %v", name, err)
		}
		return body
	}

	tests := []struct {
		name string
		want any
	}{
		{"success_envelope.json", map[string]any{"id": float64(1)}},
		{"plain_ok.json", map[string]any{"pull_request": map[string]any{"id": float64(123), "title": "Fix transport"}}},
		{"empty_body.json", map[string]any{}},
		{"success_missing_data.json", map[string]any{"status": "success", "ok": true}},
		{"success_null_data.json", nil},
		{"unknown_status.json", map[string]any{"status": "pending", "data": map[string]any{"id": float64(2)}}},
		{"non_string_status.json", map[string]any{"status": true, "data": map[string]any{"id": float64(3)}}},
		{"array_ok.json", []any{float64(1), float64(2), float64(3)}},
		{"primitive_ok.json", "ok"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeAppOperationResult("github", "get_issue", &OperationResult{Status: 200, Body: fixture(tt.name)})
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if !deepEqualJSON(got, tt.want) {
				t.Fatalf("decode = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestDecodeAppResultErrors(t *testing.T) {
	fixture := func(name string) []byte {
		body, err := os.ReadFile(filepath.Join("..", "testdata", "app_invoke", name))
		if err != nil {
			t.Fatalf("read fixture %s: %v", name, err)
		}
		return body
	}

	_, err := decodeAppOperationResult("github", "get_issue", &OperationResult{Status: 200, Body: fixture("error_envelope.json")})
	var invokeErr *InvokeError
	if !errors.As(err, &invokeErr) {
		t.Fatalf("error = %v, want InvokeError", err)
	}
	if invokeErr.Code != "missing_credential" || invokeErr.Message != "missing credential" {
		t.Fatalf("InvokeError = %+v", invokeErr)
	}

	_, err = decodeAppOperationResult("github", "get_issue", &OperationResult{Status: 401, Body: fixture("http_401.json")})
	if !errors.As(err, &invokeErr) || invokeErr.Status != 401 || len(invokeErr.RawBody) == 0 {
		t.Fatalf("HTTP InvokeError = %+v err=%v", invokeErr, err)
	}

	_, err = decodeAppOperationResult("github", "get_issue", &OperationResult{Status: 200, Body: fixture("invalid_json.txt")})
	if !errors.As(err, &invokeErr) || len(invokeErr.RawBody) == 0 {
		t.Fatalf("invalid JSON InvokeError = %+v err=%v", invokeErr, err)
	}
}

func deepEqualJSON(left, right any) bool {
	leftBytes, _ := json.Marshal(left)
	rightBytes, _ := json.Marshal(right)
	return string(leftBytes) == string(rightBytes)
}
