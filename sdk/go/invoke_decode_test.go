package gestalt_test

// Conformance tests for the generated JSON operation-envelope decode,
// driven by the shared fixtures in sdk/testdata/app_invoke. The fixture
// suite is the normative spec of the envelope semantics across all four
// SDK languages.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/valon-technologies/gestalt/sdk/go/client"
)

func invokeDecodeFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "testdata", "app_invoke", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return body
}

func invokeDecodeJSONEqual(left, right any) bool {
	leftBytes, _ := json.Marshal(left)
	rightBytes, _ := json.Marshal(right)
	return string(leftBytes) == string(rightBytes)
}

func TestDecodeAppResultFixtureSuite(t *testing.T) {
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
			got, err := client.DecodeAppResult("github", "get_issue", 200, invokeDecodeFixture(t, tt.name))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if !invokeDecodeJSONEqual(got, tt.want) {
				t.Fatalf("decode = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestDecodeAppResultFixtureSuiteErrors(t *testing.T) {
	_, err := client.DecodeAppResult("github", "get_issue", 200, invokeDecodeFixture(t, "error_envelope.json"))
	var invokeErr *client.InvokeError
	if !errors.As(err, &invokeErr) {
		t.Fatalf("error = %v, want *client.InvokeError", err)
	}
	if invokeErr.App != "github" || invokeErr.Operation != "get_issue" || invokeErr.Status != 0 {
		t.Fatalf("error envelope InvokeError = %+v, want app=github operation=get_issue status=0", invokeErr)
	}
	if invokeErr.Code != "missing_credential" || invokeErr.Message != "missing credential" {
		t.Fatalf("error envelope InvokeError = %+v, want missing_credential", invokeErr)
	}

	_, err = client.DecodeAppResult("github", "get_issue", 401, invokeDecodeFixture(t, "http_401.json"))
	if !errors.As(err, &invokeErr) || invokeErr.Status != 401 || len(invokeErr.RawBody) == 0 {
		t.Fatalf("HTTP InvokeError = %+v err=%v, want status=401 with raw body", invokeErr, err)
	}

	_, err = client.DecodeAppResult("github", "get_issue", 200, invokeDecodeFixture(t, "invalid_json.txt"))
	if !errors.As(err, &invokeErr) || invokeErr.Message != "app invoke response is not valid JSON" || len(invokeErr.RawBody) == 0 {
		t.Fatalf("invalid JSON InvokeError = %+v err=%v", invokeErr, err)
	}
}

func TestIsOK(t *testing.T) {
	for _, tt := range []struct {
		status int32
		want   bool
	}{
		{0, false}, {199, false}, {200, true}, {204, true}, {299, true},
		{300, false}, {302, false}, {401, false}, {500, false},
	} {
		if got := client.IsOK(tt.status); got != tt.want {
			t.Errorf("IsOK(%d) = %v, want %v", tt.status, got, tt.want)
		}
	}
}

func TestRequireOK(t *testing.T) {
	if err := client.RequireOK("github", "get_issue", 200, invokeDecodeFixture(t, "success_envelope.json")); err != nil {
		t.Fatalf("RequireOK(200) = %v, want nil", err)
	}
	if err := client.RequireOK("github", "get_issue", 204, nil); err != nil {
		t.Fatalf("RequireOK(204) = %v, want nil", err)
	}

	err := client.RequireOK("github", "get_issue", 401, invokeDecodeFixture(t, "http_401.json"))
	var invokeErr *client.InvokeError
	if !errors.As(err, &invokeErr) {
		t.Fatalf("RequireOK(401) = %v, want *client.InvokeError", err)
	}
	if invokeErr.App != "github" || invokeErr.Operation != "get_issue" || invokeErr.Status != 401 {
		t.Fatalf("RequireOK(401) InvokeError = %+v, want app=github operation=get_issue status=401", invokeErr)
	}
	if invokeErr.Code != "unauthorized" || invokeErr.Message != "unauthorized" || len(invokeErr.RawBody) == 0 {
		t.Fatalf("RequireOK(401) InvokeError = %+v, want unauthorized envelope fields with raw body", invokeErr)
	}

	// RequireOK shares the HTTP-error extraction path with DecodeAppResult:
	// the errors it builds are identical.
	_, decodeErr := client.DecodeAppResult("github", "get_issue", 401, invokeDecodeFixture(t, "http_401.json"))
	var decodeInvokeErr *client.InvokeError
	if !errors.As(decodeErr, &decodeInvokeErr) {
		t.Fatalf("DecodeAppResult(401) = %v, want *client.InvokeError", decodeErr)
	}
	if !reflect.DeepEqual(invokeErr, decodeInvokeErr) {
		t.Fatalf("RequireOK error = %+v, want DecodeAppResult error %+v", invokeErr, decodeInvokeErr)
	}
}

func TestDecodeGraphQLResultFixtureSuite(t *testing.T) {
	got, err := client.DecodeGraphQLResult("linear", 200, invokeDecodeFixture(t, "graphql_ok.json"))
	if err != nil {
		t.Fatalf("graphql ok: %v", err)
	}
	want := map[string]any{"data": map[string]any{"viewer": map[string]any{"id": "user-1"}}, "errors": []any{}}
	if !invokeDecodeJSONEqual(got, want) {
		t.Fatalf("graphql ok = %#v, want %#v", got, want)
	}

	got, err = client.DecodeGraphQLResult("linear", 200, invokeDecodeFixture(t, "graphql_malformed_errors.json"))
	if err != nil {
		t.Fatalf("graphql malformed errors: %v", err)
	}
	want = map[string]any{"data": map[string]any{"viewer": nil}, "errors": map[string]any{"message": "not an array"}}
	if !invokeDecodeJSONEqual(got, want) {
		t.Fatalf("graphql malformed errors = %#v, want pass-through %#v", got, want)
	}

	_, err = client.DecodeGraphQLResult("linear", 200, invokeDecodeFixture(t, "graphql_errors.json"))
	var invokeErr *client.InvokeError
	if !errors.As(err, &invokeErr) {
		t.Fatalf("graphql errors = %v, want *client.InvokeError", err)
	}
	if invokeErr.Code != "graphql_errors" || invokeErr.Operation != "graphql" || invokeErr.Message != "permission denied" {
		t.Fatalf("graphql InvokeError = %+v, want code=graphql_errors operation=graphql", invokeErr)
	}

	if _, err = client.DecodeGraphQLResult("linear", 200, invokeDecodeFixture(t, "graphql_success_envelope_errors.json")); !errors.As(err, &invokeErr) {
		t.Fatalf("graphql errors behind success envelope = %v, want *client.InvokeError", err)
	}
}

func TestJSONResultAs(t *testing.T) {
	type issue struct {
		ID    int    `json:"id"`
		Title string `json:"title"`
	}

	got, err := client.JSONResultAs[issue](client.DecodeAppResult("github", "get_issue", 200, []byte(`{"status":"success","data":{"id":7,"title":"Fix transport"}}`)))
	if err != nil {
		t.Fatalf("JSONResultAs: %v", err)
	}
	if got.ID != 7 || got.Title != "Fix transport" {
		t.Fatalf("JSONResultAs = %+v, want id=7 title=Fix transport", got)
	}

	_, err = client.JSONResultAs[issue](client.DecodeAppResult("github", "get_issue", 200, invokeDecodeFixture(t, "error_envelope.json")))
	var invokeErr *client.InvokeError
	if !errors.As(err, &invokeErr) || invokeErr.Code != "missing_credential" {
		t.Fatalf("JSONResultAs error passthrough = %v, want missing_credential *client.InvokeError", err)
	}
}
