package egress

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestApplyHeaderMutations(t *testing.T) {
	t.Parallel()

	got := ApplyHeaderMutations(map[string]string{
		"x-api-key": "old",
		"X-Trace":   "trace-1",
	}, []HeaderMutation{
		{Action: HeaderActionReplace, Name: "x-api-key", Value: "new"},
		{Action: HeaderActionSet, Name: "x-org", Value: "acme"},
		{Action: HeaderActionRemove, Name: "x-trace"},
	})

	if got["X-Api-Key"] != "new" {
		t.Fatalf("X-Api-Key = %q, want new", got["X-Api-Key"])
	}
	if got["X-Org"] != "acme" {
		t.Fatalf("X-Org = %q, want acme", got["X-Org"])
	}
	if _, ok := got["X-Trace"]; ok {
		t.Fatal("X-Trace still present after remove")
	}
}

func TestMaterializeCredentialWithParser(t *testing.T) {
	t.Parallel()

	mat, err := MaterializeCredential("tok", AuthStyleBearer, func(token string) (string, map[string]string, error) {
		return "Token " + token, map[string]string{"X-Org": "acme"}, nil
	})
	if err != nil {
		t.Fatalf("MaterializeCredential: %v", err)
	}
	if mat.Authorization != "Token tok" {
		t.Fatalf("Authorization = %q, want %q", mat.Authorization, "Token tok")
	}
	if len(mat.Headers) != 1 {
		t.Fatalf("headers = %d, want 1", len(mat.Headers))
	}
}

func TestMaterializeCredentialBasic(t *testing.T) {
	t.Parallel()

	mat, err := MaterializeCredential("user:pass", AuthStyleBasic, nil)
	if err != nil {
		t.Fatalf("MaterializeCredential: %v", err)
	}
	if mat.Authorization == "" || mat.Authorization[:6] != "Basic " {
		t.Fatalf("Authorization = %q, want basic auth header", mat.Authorization)
	}
}

func TestExecuteHTTP(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"path": r.URL.Path,
			"auth": r.Header.Get("Authorization"),
			"org":  r.Header.Get("X-Org"),
		})
	}))
	t.Cleanup(func() { srv.Close() })

	result, err := ExecuteHTTP(context.Background(), srv.Client(), HTTPRequestSpec{
		Target: Target{
			Method: http.MethodGet,
			Path:   "/v1/messages",
		},
		BaseURL: srv.URL,
		Credential: CredentialMaterialization{
			Authorization: "Bearer real-token",
			Headers: []HeaderMutation{{
				Action: HeaderActionSet,
				Name:   "X-Org",
				Value:  "acme",
			}},
		},
	})
	if err != nil {
		t.Fatalf("ExecuteHTTP: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(result.Body), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp["path"] != "/v1/messages" {
		t.Fatalf("path = %v, want /v1/messages", resp["path"])
	}
	if resp["auth"] != "Bearer real-token" {
		t.Fatalf("auth = %v, want Bearer real-token", resp["auth"])
	}
	if resp["org"] != "acme" {
		t.Fatalf("org = %v, want acme", resp["org"])
	}
}

type stubPolicy struct {
	err error
}

func (s stubPolicy) Evaluate(_ context.Context, _ PolicyInput) error {
	return s.err
}

func TestEvaluatePolicyNilSafe(t *testing.T) {
	t.Parallel()

	if err := EvaluatePolicy(context.Background(), nil, PolicyInput{}); err != nil {
		t.Fatalf("EvaluatePolicy(nil): %v", err)
	}
}

func TestEvaluatePolicyReturnsError(t *testing.T) {
	t.Parallel()

	want := errors.New("blocked")
	err := EvaluatePolicy(context.Background(), stubPolicy{err: want}, PolicyInput{})
	if !errors.Is(err, want) {
		t.Fatalf("EvaluatePolicy error = %v, want %v", err, want)
	}
}
