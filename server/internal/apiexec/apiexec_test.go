package apiexec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestSubstitutePath_MissingParam(t *testing.T) {
	t.Parallel()
	_, err := substitutePath("/users/{userId}/messages", map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing path parameter")
	}
	if !errors.Is(err, ErrMissingPathParam) {
		t.Errorf("expected ErrMissingPathParam, got: %v", err)
	}
	if !strings.Contains(err.Error(), "userId") {
		t.Errorf("error should mention the missing param name, got: %v", err)
	}
}

func TestSubstitutePath_Success(t *testing.T) {
	t.Parallel()
	result, err := substitutePath("/users/{userId}/messages", map[string]any{"userId": "me"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "/users/me/messages" {
		t.Errorf("expected /users/me/messages, got: %s", result)
	}
}

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

func TestRetryOn429ThenSuccess(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	var delays []time.Duration
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("rate limited"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	testutil.CloseOnCleanup(t, srv)

	result, err := Do(context.Background(), srv.Client(), Request{
		Method:     http.MethodGet,
		BaseURL:    srv.URL,
		Path:       "/test",
		MaxRetries: 2,
		retryWait: func(_ context.Context, delay time.Duration) error {
			delays = append(delays, delay)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", result.Status, http.StatusOK)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("attempts = %d, want 2", got)
	}
	if len(delays) != 1 || delays[0] != time.Second {
		t.Fatalf("delays = %v, want [1s]", delays)
	}
}

func TestRetryOn503WithBackoff(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	var delays []time.Duration
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("unavailable"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "recovered"})
	}))
	testutil.CloseOnCleanup(t, srv)

	result, err := Do(context.Background(), srv.Client(), Request{
		Method:     http.MethodGet,
		BaseURL:    srv.URL,
		Path:       "/test",
		MaxRetries: 3,
		retryWait: func(_ context.Context, delay time.Duration) error {
			delays = append(delays, delay)
			return nil
		},
	})

	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", result.Status, http.StatusOK)
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
	if !slices.Equal(delays, []time.Duration{time.Second, 2 * time.Second}) {
		t.Fatalf("delays = %v, want [1s 2s]", delays)
	}
}

func TestRetryAfterHeaderRespected(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	var delays []time.Duration
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("rate limited"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	testutil.CloseOnCleanup(t, srv)

	result, err := Do(context.Background(), srv.Client(), Request{
		Method:     http.MethodGet,
		BaseURL:    srv.URL,
		Path:       "/test",
		MaxRetries: 2,
		retryWait: func(_ context.Context, delay time.Duration) error {
			delays = append(delays, delay)
			return nil
		},
	})

	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", result.Status, http.StatusOK)
	}
	if !slices.Equal(delays, []time.Duration{2 * time.Second}) {
		t.Fatalf("delays = %v, want [2s]", delays)
	}
}

func TestNoRetryDisablesRetry(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("rate limited"))
	}))
	testutil.CloseOnCleanup(t, srv)

	_, err := Do(context.Background(), srv.Client(), Request{
		Method:  http.MethodGet,
		BaseURL: srv.URL,
		Path:    "/test",
		NoRetry: true,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts = %d, want 1 (no retry)", got)
	}
}

func TestRetriesStopAfterMaxRetries(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	var delays []time.Duration
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("bad gateway"))
	}))
	testutil.CloseOnCleanup(t, srv)

	_, err := Do(context.Background(), srv.Client(), Request{
		Method:     http.MethodGet,
		BaseURL:    srv.URL,
		Path:       "/test",
		MaxRetries: 2,
		retryWait: func(_ context.Context, delay time.Duration) error {
			delays = append(delays, delay)
			return nil
		},
	})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	// 1 initial + 2 retries = 3 total
	if got := attempts.Load(); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
	if !slices.Equal(delays, []time.Duration{time.Second, 2 * time.Second}) {
		t.Fatalf("delays = %v, want [1s 2s]", delays)
	}
}

func TestContextCancellationStopsRetries(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("unavailable"))
	}))
	testutil.CloseOnCleanup(t, srv)

	ctx, cancel := context.WithCancel(context.Background())

	_, err := Do(ctx, srv.Client(), Request{
		Method:     http.MethodGet,
		BaseURL:    srv.URL,
		Path:       "/test",
		MaxRetries: 5,
		retryWait: func(ctx context.Context, _ time.Duration) error {
			cancel()
			return ctx.Err()
		},
	})
	if err == nil {
		t.Fatal("expected error from context cancellation")
	}
	if err != context.Canceled {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if got := attempts.Load(); got > 2 {
		t.Fatalf("attempts = %d, want at most 2", got)
	}
}

func TestNonRetryableErrorsNotRetried(t *testing.T) {
	t.Parallel()

	for _, code := range []int{
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusInternalServerError,
	} {
		t.Run(fmt.Sprintf("HTTP_%d", code), func(t *testing.T) {
			t.Parallel()

			var attempts atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				attempts.Add(1)
				w.WriteHeader(code)
				_, _ = fmt.Fprintf(w, "error %d", code)
			}))
			testutil.CloseOnCleanup(t, srv)

			_, err := Do(context.Background(), srv.Client(), Request{
				Method:  http.MethodGet,
				BaseURL: srv.URL,
				Path:    "/test",
			})
			if err == nil {
				t.Fatalf("expected error for HTTP %d", code)
			}
			if got := attempts.Load(); got != 1 {
				t.Fatalf("attempts = %d, want 1 (no retry for HTTP %d)", got, code)
			}
		})
	}
}

func TestResponseBodySizeLimit(t *testing.T) {
	t.Parallel()

	oversized := int64(maxResponseBodySize + 1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		buf := make([]byte, 32*1024)
		var written int64
		for written < oversized {
			n := int64(len(buf))
			if remaining := oversized - written; remaining < n {
				n = remaining
			}
			nn, err := w.Write(buf[:n])
			if err != nil {
				return
			}
			written += int64(nn)
		}
	}))
	testutil.CloseOnCleanup(t, srv)

	result, err := Do(context.Background(), srv.Client(), Request{
		Method:  http.MethodGet,
		BaseURL: srv.URL,
		Path:    "/large",
		NoRetry: true,
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	if got := int64(len(result.Body)); got != maxResponseBodySize {
		t.Fatalf("response body size = %d, want %d (truncated at limit)", got, maxResponseBodySize)
	}
}
