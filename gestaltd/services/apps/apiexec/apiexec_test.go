package apiexec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newRetryTestClient(t *testing.T, handle func(*http.Request) (int, http.Header, string)) *http.Client {
	t.Helper()
	return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		status, headers, body := handle(req)
		if headers == nil {
			headers = http.Header{}
		}
		return &http.Response{
			StatusCode: status,
			Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
			Header:     headers,
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})}
}

func advanceRetryDelay(t *testing.T, delay time.Duration, beforeComplete func()) {
	t.Helper()
	if delay > time.Nanosecond {
		time.Sleep(delay - time.Nanosecond)
		synctest.Wait()
	}
	if beforeComplete != nil {
		beforeComplete()
	}
	time.Sleep(time.Nanosecond)
	synctest.Wait()
}

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

func TestSubstitutePath_EncodesSlashesInPathParams(t *testing.T) {
	t.Parallel()
	result, err := substitutePath(
		"/storage/v1/b/{bucket}/o/{object}",
		map[string]any{
			"bucket": "my-bucket",
			"object": "folder/sub/file.json",
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "/storage/v1/b/my-bucket/o/folder%2Fsub%2Ffile.json"
	if result != want {
		t.Errorf("got %q, want %q", result, want)
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

func TestDoReturnsUpstreamHTTPError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid parameter: limit"}}`))
	}))
	testutil.CloseOnCleanup(t, srv)

	_, err := Do(context.Background(), srv.Client(), Request{
		Method:  http.MethodGet,
		BaseURL: srv.URL,
		Path:    "/items",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var upstreamErr *UpstreamHTTPError
	if !errors.As(err, &upstreamErr) {
		t.Fatalf("expected UpstreamHTTPError, got %T", err)
	}
	if upstreamErr.Status != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", upstreamErr.Status, http.StatusBadRequest)
	}
	if upstreamErr.Body != `{"error":{"message":"invalid parameter: limit"}}` {
		t.Fatalf("body = %q", upstreamErr.Body)
	}
	if got := upstreamErr.Headers.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
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
		if body[graphqlBodyKeyOperationName] != "Repos" {
			t.Fatalf("operationName = %v, want Repos", body[graphqlBodyKeyOperationName])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"repos":[{"name":"toolshed"}]}}`))
	}))
	testutil.CloseOnCleanup(t, srv)

	result, err := DoGraphQL(context.Background(), srv.Client(), GraphQLRequest{
		URL:           srv.URL,
		Query:         "query($first: Int) { repos(first: $first) { name } }",
		OperationName: "Repos",
		Variables:     map[string]any{"first": 10},
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

	synctest.Test(t, func(t *testing.T) {
		var attempts atomic.Int32
		client := newRetryTestClient(t, func(*http.Request) (int, http.Header, string) {
			if attempts.Add(1) == 1 {
				return http.StatusTooManyRequests, nil, "rate limited"
			}
			return http.StatusOK, nil, `{"status":"ok"}`
		})

		done := make(chan error, 1)
		var resultStatus int
		go func() {
			result, err := Do(context.Background(), client, Request{
				Method:     http.MethodGet,
				BaseURL:    "https://api.example.test",
				Path:       "/test",
				MaxRetries: 2,
			})
			if result != nil {
				resultStatus = result.Status
			}
			done <- err
		}()

		synctest.Wait()
		if got := attempts.Load(); got != 1 {
			t.Fatalf("attempts before retry delay = %d, want 1", got)
		}
		advanceRetryDelay(t, time.Second, func() {
			if got := attempts.Load(); got != 1 {
				t.Fatalf("attempts before retry delay completes = %d, want 1", got)
			}
		})
		if got := attempts.Load(); got != 2 {
			t.Fatalf("attempts after retry delay = %d, want 2", got)
		}

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("expected success after retry, got: %v", err)
			}
		default:
			t.Fatal("Do did not return after successful retry")
		}
		if resultStatus != http.StatusOK {
			t.Fatalf("status = %d, want %d", resultStatus, http.StatusOK)
		}
	})
}

func TestRetryOn503WithBackoff(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var attempts atomic.Int32
		client := newRetryTestClient(t, func(*http.Request) (int, http.Header, string) {
			if attempts.Add(1) <= 2 {
				return http.StatusServiceUnavailable, nil, "unavailable"
			}
			return http.StatusOK, nil, `{"status":"recovered"}`
		})

		done := make(chan error, 1)
		var resultStatus int
		go func() {
			result, err := Do(context.Background(), client, Request{
				Method:     http.MethodGet,
				BaseURL:    "https://api.example.test",
				Path:       "/test",
				MaxRetries: 3,
			})
			if result != nil {
				resultStatus = result.Status
			}
			done <- err
		}()

		synctest.Wait()
		if got := attempts.Load(); got != 1 {
			t.Fatalf("attempts before first backoff = %d, want 1", got)
		}
		select {
		case err := <-done:
			t.Fatalf("Do returned before first backoff elapsed: %v", err)
		default:
		}

		advanceRetryDelay(t, time.Second, func() {
			if got := attempts.Load(); got != 1 {
				t.Fatalf("attempts before first backoff completes = %d, want 1", got)
			}
		})
		if got := attempts.Load(); got != 2 {
			t.Fatalf("attempts after first backoff = %d, want 2", got)
		}

		advanceRetryDelay(t, 2*time.Second, func() {
			if got := attempts.Load(); got != 2 {
				t.Fatalf("attempts before second backoff completes = %d, want 2", got)
			}
		})
		if got := attempts.Load(); got != 3 {
			t.Fatalf("attempts after second backoff = %d, want 3", got)
		}

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("expected success after retries, got: %v", err)
			}
		default:
			t.Fatal("Do did not return after successful retry")
		}
		if resultStatus != http.StatusOK {
			t.Fatalf("status = %d, want %d", resultStatus, http.StatusOK)
		}
	})
}

func TestRetryAfterHeaderRespected(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var attempts atomic.Int32
		client := newRetryTestClient(t, func(*http.Request) (int, http.Header, string) {
			if attempts.Add(1) == 1 {
				headers := http.Header{}
				headers.Set("Retry-After", "2")
				return http.StatusTooManyRequests, headers, "rate limited"
			}
			return http.StatusOK, nil, `{"status":"ok"}`
		})

		done := make(chan error, 1)
		var resultStatus int
		go func() {
			result, err := Do(context.Background(), client, Request{
				Method:     http.MethodGet,
				BaseURL:    "https://api.example.test",
				Path:       "/test",
				MaxRetries: 2,
			})
			if result != nil {
				resultStatus = result.Status
			}
			done <- err
		}()

		synctest.Wait()
		if got := attempts.Load(); got != 1 {
			t.Fatalf("attempts before Retry-After delay = %d, want 1", got)
		}
		advanceRetryDelay(t, 2*time.Second, func() {
			if got := attempts.Load(); got != 1 {
				t.Fatalf("attempts before Retry-After delay completes = %d, want 1", got)
			}
		})
		if got := attempts.Load(); got != 2 {
			t.Fatalf("attempts after Retry-After delay = %d, want 2", got)
		}

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("expected success, got: %v", err)
			}
		default:
			t.Fatal("Do did not return after Retry-After retry")
		}
		if resultStatus != http.StatusOK {
			t.Fatalf("status = %d, want %d", resultStatus, http.StatusOK)
		}
	})
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

	synctest.Test(t, func(t *testing.T) {
		var attempts atomic.Int32
		client := newRetryTestClient(t, func(*http.Request) (int, http.Header, string) {
			attempts.Add(1)
			return http.StatusBadGateway, nil, "bad gateway"
		})

		done := make(chan error, 1)
		go func() {
			_, err := Do(context.Background(), client, Request{
				Method:     http.MethodGet,
				BaseURL:    "https://api.example.test",
				Path:       "/test",
				MaxRetries: 2,
			})
			done <- err
		}()

		synctest.Wait()
		if got := attempts.Load(); got != 1 {
			t.Fatalf("attempts before first retry delay = %d, want 1", got)
		}
		advanceRetryDelay(t, time.Second, func() {
			if got := attempts.Load(); got != 1 {
				t.Fatalf("attempts before first retry delay completes = %d, want 1", got)
			}
		})
		if got := attempts.Load(); got != 2 {
			t.Fatalf("attempts after first retry delay = %d, want 2", got)
		}
		advanceRetryDelay(t, 2*time.Second, func() {
			if got := attempts.Load(); got != 2 {
				t.Fatalf("attempts before second retry delay completes = %d, want 2", got)
			}
		})
		if got := attempts.Load(); got != 3 {
			t.Fatalf("attempts after second retry delay = %d, want 3", got)
		}

		select {
		case err := <-done:
			if err == nil {
				t.Fatal("expected error after exhausting retries")
			}
		default:
			t.Fatal("Do did not return after exhausting retries")
		}
	})
}

func TestContextCancellationStopsRetries(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var attempts atomic.Int32
		client := newRetryTestClient(t, func(*http.Request) (int, http.Header, string) {
			attempts.Add(1)
			return http.StatusServiceUnavailable, nil, "unavailable"
		})

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() {
			_, err := Do(ctx, client, Request{
				Method:     http.MethodGet,
				BaseURL:    "https://api.example.test",
				Path:       "/test",
				MaxRetries: 5,
			})
			done <- err
		}()

		synctest.Wait()
		if got := attempts.Load(); got != 1 {
			t.Fatalf("attempts before cancellation = %d, want 1", got)
		}
		time.Sleep(500 * time.Millisecond)
		cancel()
		synctest.Wait()

		select {
		case err := <-done:
			if err == nil {
				t.Fatal("expected error from context cancellation")
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("err = %v, want context.Canceled", err)
			}
		default:
			t.Fatal("Do did not return after context cancellation")
		}
		if got := attempts.Load(); got != 1 {
			t.Fatalf("attempts = %d, want 1", got)
		}
	})
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
