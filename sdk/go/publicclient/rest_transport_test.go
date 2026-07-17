package publicclient_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gestaltclient "github.com/valon-technologies/gestalt/sdk/go/client"
	"github.com/valon-technologies/gestalt/sdk/go/publicclient"
	"github.com/valon-technologies/gestalt/sdk/go/publicclient/generated"
)

type restCall struct {
	method        string
	path          string
	authorization string
	body          string
}

func TestRESTTransportMappingAndGatewayErrors(t *testing.T) {
	var calls []restCall
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, restCall{
			method:        r.Method,
			path:          r.URL.Path,
			authorization: auth,
			body:          string(body),
		})
		if auth == "Bearer bad" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized","code":"Unauthenticated"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":200,"body":"eyJzdGF0dXMiOiJzdWNjZXNzIiwiZGF0YSI6eyJvayI6dHJ1ZX19","headers":{"X-Example":{"values":["rest-v2"]}}}`))
	}))
	defer server.Close()

	client := publicclient.NewRESTClientForTest(server.URL, publicclient.Bearer(func(context.Context) (string, error) {
		return "token-123", nil
	}), server.Client())
	defer func() { _ = client.Close() }()

	got, err := client.App.Invoke(context.Background(), &generated.AppInvokeRequest{
		App:            "example",
		Operation:      "sync",
		Params:         map[string]any{"ok": true},
		IdempotencyKey: "key-1",
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	if calls[0].method != http.MethodPost {
		t.Fatalf("method = %q", calls[0].method)
	}
	if calls[0].path != "/api/v2/app/example/operations/sync" {
		t.Fatalf("path = %q", calls[0].path)
	}
	if calls[0].authorization != "Bearer token-123" {
		t.Fatalf("authorization = %q", calls[0].authorization)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(calls[0].body), &body); err != nil {
		t.Fatalf("body json: %v", err)
	}
	if body["params"].(map[string]any)["ok"] != true || body["idempotencyKey"] != "key-1" {
		t.Fatalf("body = %#v", body)
	}
	if got.(map[string]any)["ok"] != true {
		t.Fatalf("result = %#v", got)
	}

	clientBad := publicclient.NewRESTClientForTest(server.URL, publicclient.Bearer(func(context.Context) (string, error) {
		return "bad", nil
	}), server.Client())
	defer func() { _ = clientBad.Close() }()
	_, err = clientBad.App.Invoke(context.Background(), &generated.AppInvokeRequest{
		App:       "example",
		Operation: "sync",
	})
	var gerr *generated.GestaltError
	if !errors.As(err, &gerr) {
		t.Fatalf("error = %v, want *generated.GestaltError", err)
	}
	if gerr.Code != gestaltclient.GestaltErrorCodeUnauthenticated {
		t.Fatalf("code = %v", gerr.Code)
	}
}

func TestBearerRotationEvaluatedPerCall(t *testing.T) {
	token := "first"
	var authorizations []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorizations = append(authorizations, r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"status":200,"body":"","headers":{}}`))
	}))
	defer server.Close()

	client := publicclient.NewRESTClientForTest(server.URL, publicclient.Bearer(func(context.Context) (string, error) {
		return token, nil
	}), server.Client())
	defer func() { _ = client.Close() }()

	request := &generated.AppInvokeRequest{App: "example", Operation: "sync"}
	if _, err := client.App.Invoke(context.Background(), request); err != nil {
		t.Fatalf("first invoke: %v", err)
	}
	token = "second"
	if _, err := client.App.Invoke(context.Background(), request); err != nil {
		t.Fatalf("second invoke: %v", err)
	}
	if len(authorizations) != 2 || authorizations[0] != "Bearer first" || authorizations[1] != "Bearer second" {
		t.Fatalf("authorizations = %#v", authorizations)
	}
}

func TestUnauthenticatedOmitsAuthorizationHeader(t *testing.T) {
	var authorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"status":200,"body":"","headers":{}}`))
	}))
	defer server.Close()

	client := publicclient.NewRESTClientForTest(server.URL, publicclient.Unauthenticated(), server.Client())
	defer func() { _ = client.Close() }()
	if _, err := client.App.Invoke(context.Background(), &generated.AppInvokeRequest{
		App: "example", Operation: "sync",
	}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if authorization != "" {
		t.Fatalf("authorization = %q, want empty", authorization)
	}
}

func TestNewRequiresValidAddress(t *testing.T) {
	_, err := publicclient.NewREST(publicclient.AddressOptions{
		Address: "",
		Auth:    publicclient.Unauthenticated(),
	})
	if err == nil || !strings.Contains(err.Error(), "address is required") {
		t.Fatalf("error = %v", err)
	}

	_, err = publicclient.NewREST(publicclient.AddressOptions{
		Address: "not-a-url",
		Auth:    publicclient.Unauthenticated(),
	})
	if err == nil {
		t.Fatal("expected invalid address error")
	}

	_, err = publicclient.NewGRPC(publicclient.AddressOptions{
		Auth: publicclient.Unauthenticated(),
	})
	if err == nil || !strings.Contains(err.Error(), "address is required") {
		t.Fatalf("gRPC error = %v", err)
	}

	_, err = publicclient.NewGRPC(publicclient.AddressOptions{
		Address: "ftp://example.com",
		Auth:    publicclient.Unauthenticated(),
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported address scheme") {
		t.Fatalf("unsupported scheme error = %v", err)
	}
}

func TestBearerProviderReceivesContext(t *testing.T) {
	var seen context.Context
	auth := publicclient.Bearer(func(ctx context.Context) (string, error) {
		seen = ctx
		return "token", nil
	})
	req := &publicclient.Request{Headers: map[string]string{}}
	ctx := context.WithValue(context.Background(), struct{}{}, "marker")
	if err := auth.Apply(ctx, req); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if seen != ctx {
		t.Fatal("token provider did not receive call context")
	}
}

func TestRESTTransportSharedConformanceCases(t *testing.T) {
	cases := loadClientCases(t)
	for _, tc := range cases {
		t.Run(tc.ID, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch tc.ID {
				case "invoke_success":
					body, err := base64.StdEncoding.DecodeString(tc.Response.OperationResult.BodyBase64)
					if err != nil {
						t.Fatalf("decode body: %v", err)
					}
					_, _ = w.Write([]byte(`{"status":200,"body":"` + base64.StdEncoding.EncodeToString(body) + `","headers":{}}`))
				case "platform_error":
					w.WriteHeader(http.StatusUnauthorized)
					_, _ = w.Write([]byte(`{"error":"Not authenticated","code":"Unauthenticated"}`))
				}
			}))
			defer server.Close()

			client := publicclient.NewRESTClientForTest(server.URL, publicclient.Bearer(func(context.Context) (string, error) {
				return "token", nil
			}), server.Client())
			defer func() { _ = client.Close() }()

			request := &generated.AppInvokeRequest{
				App:       tc.PublicRequest["app"].(string),
				Operation: tc.PublicRequest["operation"].(string),
			}
			if params, ok := tc.PublicRequest["params"].(map[string]any); ok {
				request.Params = params
			}

			switch tc.ID {
			case "invoke_success":
				got, err := client.App.Invoke(context.Background(), request)
				if err != nil {
					t.Fatalf("Invoke: %v", err)
				}
				if !jsonEqual(got, tc.Expect.Result) {
					t.Fatalf("result = %#v, want %#v", got, tc.Expect.Result)
				}
			case "platform_error":
				_, err := client.App.Invoke(context.Background(), request)
				var gerr *generated.GestaltError
				if !errors.As(err, &gerr) {
					t.Fatalf("error = %v, want *generated.GestaltError", err)
				}
				if int32(gerr.Code) != tc.Expect.GestaltError.Code || gerr.Message != tc.Expect.GestaltError.Message {
					t.Fatalf("GestaltError = %+v", gerr)
				}
			}
		})
	}
}

func TestContextCancellation(t *testing.T) {
	block := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-block
	}))
	defer server.Close()
	defer close(block)

	client := publicclient.NewRESTClientForTest(server.URL, publicclient.Unauthenticated(), server.Client())
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		cancel()
	}()

	_, err := client.App.Invoke(ctx, &generated.AppInvokeRequest{App: "example", Operation: "sync"})
	var gerr *generated.GestaltError
	if !errors.As(err, &gerr) {
		t.Fatalf("error = %v, want *generated.GestaltError", err)
	}
	if gerr.Code != gestaltclient.GestaltErrorCodeCanceled {
		t.Fatalf("code = %v", gerr.Code)
	}
}

func TestGestaltFromContextRequiresHostEnvironment(t *testing.T) {
	_, err := publicclient.GestaltFromContext(context.Background())
	if err == nil {
		t.Fatal("expected GestaltFromContext to fail outside provider host")
	}
}

func TestPublicBearerAuthDoesNotCarryHostServiceCapability(t *testing.T) {
	publicAuth := publicclient.Bearer(func(context.Context) (string, error) { return "public", nil })
	req := &publicclient.Request{Headers: map[string]string{}}
	if err := publicAuth.Apply(context.Background(), req); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if req.Headers["Authorization"] != "Bearer public" {
		t.Fatalf("headers = %#v", req.Headers)
	}
	if strings.Contains(req.Headers["Authorization"], "caller") {
		t.Fatal("public bearer must not carry caller proof semantics")
	}
}

type clientCase struct {
	ID            string         `json:"id"`
	PublicRequest map[string]any `json:"publicRequest"`
	Response      struct {
		OperationResult *struct {
			BodyBase64 string `json:"bodyBase64"`
		} `json:"operationResult"`
	} `json:"response"`
	Expect struct {
		Result       any `json:"result"`
		GestaltError *struct {
			Code    int32  `json:"code"`
			Message string `json:"message"`
		} `json:"gestaltError"`
	} `json:"expect"`
}

func loadClientCases(t *testing.T) []clientCase {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "public_conformance", "client_cases.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var cases []clientCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	return cases
}

func jsonEqual(left, right any) bool {
	leftBytes, _ := json.Marshal(left)
	rightBytes, _ := json.Marshal(right)
	return string(leftBytes) == string(rightBytes)
}
