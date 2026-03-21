package apiexec

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/internal/testutil"
)

func TestDoGETWithQueryAndPathParams(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"path":  r.URL.Path,
			"query": r.URL.RawQuery,
			"auth":  r.Header.Get("Authorization"),
		})
	}))
	testutil.CloseOnCleanup(t, srv)

	result, err := Do(context.Background(), srv.Client(), Request{
		Method:  http.MethodGet,
		BaseURL: srv.URL,
		Path:    "/items/{item_id}",
		Params: map[string]any{
			"item_id": "abc123",
			"limit":   10,
		},
		Token: "test-token",
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(result.Body), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp["path"] != "/items/abc123" {
		t.Fatalf("path = %v, want /items/abc123", resp["path"])
	}
	if resp["auth"] != "Bearer test-token" {
		t.Fatalf("auth = %v, want Bearer test-token", resp["auth"])
	}
}

func TestDoPOSTWithJSONBody(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(body)
	}))
	testutil.CloseOnCleanup(t, srv)

	result, err := Do(context.Background(), srv.Client(), Request{
		Method:  http.MethodPost,
		BaseURL: srv.URL,
		Path:    "/search",
		Params: map[string]any{
			"query": "hello",
			"count": 5,
		},
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(result.Body), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp["query"] != "hello" {
		t.Fatalf("query = %v, want hello", resp["query"])
	}
}

func TestDoUsesCustomAuthHeaderAndBodyOverride(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"auth": r.Header.Get("Authorization"),
			"body": body,
		})
	}))
	testutil.CloseOnCleanup(t, srv)

	overrideBody, err := json.Marshal(map[string]any{
		"query": "{ viewer { id } }",
	})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	result, err := Do(context.Background(), srv.Client(), Request{
		Method:     http.MethodPost,
		BaseURL:    srv.URL,
		Path:       "/graphql",
		AuthHeader: "Token abc123",
		Body:       overrideBody,
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(result.Body), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp["auth"] != "Token abc123" {
		t.Fatalf("auth = %v, want Token abc123", resp["auth"])
	}
}

func TestDoGraphQLBasicQuery(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body[graphqlBodyKeyQuery] != "{ viewer { login } }" {
			t.Fatalf("query = %v, want { viewer { login } }", body[graphqlBodyKeyQuery])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"viewer":{"login":"test"}}}`))
	}))
	testutil.CloseOnCleanup(t, srv)

	result, err := DoGraphQL(context.Background(), srv.Client(), GraphQLRequest{
		URL:   srv.URL,
		Query: "{ viewer { login } }",
	})
	if err != nil {
		t.Fatalf("DoGraphQL: %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("status = %d, want 200", result.Status)
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(result.Body), &data); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	viewer := data["viewer"].(map[string]any)
	if viewer["login"] != "test" {
		t.Fatalf("login = %v, want test", viewer["login"])
	}
}

func TestDoGraphQLWithVariables(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		vars, ok := body[graphqlBodyKeyVariables].(map[string]any)
		if !ok {
			t.Fatal("variables not present in request body")
		}
		if vars["first"] != float64(10) {
			t.Fatalf("first = %v, want 10", vars["first"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"repos":[{"name":"toolshed"}]}}`))
	}))
	testutil.CloseOnCleanup(t, srv)

	result, err := DoGraphQL(context.Background(), srv.Client(), GraphQLRequest{
		URL:       srv.URL,
		Query:     "query($first: Int) { repos(first: $first) { name } }",
		Variables: map[string]any{"first": 10},
	})
	if err != nil {
		t.Fatalf("DoGraphQL: %v", err)
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(result.Body), &data); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	repos := data["repos"].([]any)
	if len(repos) != 1 {
		t.Fatalf("repos len = %d, want 1", len(repos))
	}
}

func TestDoGraphQLErrors(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errors":[{"message":"Not found"}]}`))
	}))
	testutil.CloseOnCleanup(t, srv)

	_, err := DoGraphQL(context.Background(), srv.Client(), GraphQLRequest{
		URL:   srv.URL,
		Query: "{ viewer { login } }",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "Not found") {
		t.Fatalf("error = %v, want to contain 'Not found'", err)
	}
}

func TestDoGraphQLPartialErrors(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"viewer":null},"errors":[{"message":"Forbidden field"}]}`))
	}))
	testutil.CloseOnCleanup(t, srv)

	_, err := DoGraphQL(context.Background(), srv.Client(), GraphQLRequest{
		URL:   srv.URL,
		Query: "{ viewer { email } }",
	})
	if err == nil {
		t.Fatal("expected error for partial errors response, got nil")
	}
	if !strings.Contains(err.Error(), "Forbidden field") {
		t.Fatalf("error = %v, want to contain 'Forbidden field'", err)
	}
}

func TestDoGraphQLAuthHeaders(t *testing.T) {
	t.Parallel()

	t.Run("bearer token", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"auth":"` + auth + `"}}`))
		}))
		testutil.CloseOnCleanup(t, srv)

		result, err := DoGraphQL(context.Background(), srv.Client(), GraphQLRequest{
			URL:   srv.URL,
			Query: "{ viewer { login } }",
			Token: "gh-token",
		})
		if err != nil {
			t.Fatalf("DoGraphQL: %v", err)
		}

		var data map[string]any
		if err := json.Unmarshal([]byte(result.Body), &data); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}
		if data["auth"] != "Bearer gh-token" {
			t.Fatalf("auth = %v, want Bearer gh-token", data["auth"])
		}
	})

	t.Run("custom auth header", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			custom := r.Header.Get("X-Api-Key")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"auth":"` + auth + `","key":"` + custom + `"}}`))
		}))
		testutil.CloseOnCleanup(t, srv)

		result, err := DoGraphQL(context.Background(), srv.Client(), GraphQLRequest{
			URL:        srv.URL,
			Query:      "{ viewer { login } }",
			AuthHeader: "Token custom-val",
			CustomHeaders: map[string]string{
				"X-Api-Key": "key-123",
			},
		})
		if err != nil {
			t.Fatalf("DoGraphQL: %v", err)
		}

		var data map[string]any
		if err := json.Unmarshal([]byte(result.Body), &data); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}
		if data["auth"] != "Token custom-val" {
			t.Fatalf("auth = %v, want Token custom-val", data["auth"])
		}
		if data["key"] != "key-123" {
			t.Fatalf("key = %v, want key-123", data["key"])
		}
	})
}
