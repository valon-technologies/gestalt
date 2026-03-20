package webhook_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/valon-technologies/toolshed/core"
	"github.com/valon-technologies/toolshed/internal/bootstrap"
	"github.com/valon-technologies/toolshed/internal/config"
	"github.com/valon-technologies/toolshed/internal/invocation"
	"github.com/valon-technologies/toolshed/internal/principal"
	"github.com/valon-technologies/toolshed/internal/testutil"
	"github.com/valon-technologies/toolshed/plugins/bindings/webhook"
	"gopkg.in/yaml.v3"
)

func TestWebhookRoutes(t *testing.T) {
	t.Parallel()

	b := makeBinding(t, "/incoming", "", "", &testutil.StubInvoker{})

	routes := b.Routes()
	if len(routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(routes))
	}
	if routes[0].Method != http.MethodPost {
		t.Errorf("expected POST, got %s", routes[0].Method)
	}
	if routes[0].Pattern != "/incoming" {
		t.Errorf("expected /incoming, got %s", routes[0].Pattern)
	}
}

func TestWebhookEcho(t *testing.T) {
	t.Parallel()

	b := makeBinding(t, "/incoming", "", "", &testutil.StubInvoker{})
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	body := bytes.NewBufferString(`{"message":"hello"}`)
	req := httptest.NewRequest(http.MethodPost, "/incoming", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	b.Routes()[0].Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result map[string]any
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result["message"] != "hello" {
		t.Fatalf("expected hello, got %v", result["message"])
	}
}

func TestWebhookInvokesInvoker(t *testing.T) {
	t.Parallel()

	invoker := &testutil.StubInvoker{
		Result: &core.OperationResult{
			Status: http.StatusOK,
			Body:   `{"echoed":true}`,
		},
	}
	b := makeBinding(t, "/incoming", "echo", "echo", invoker)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	body := bytes.NewBufferString(`{"data":"test"}`)
	req := httptest.NewRequest(http.MethodPost, "/incoming", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	b.Routes()[0].Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !invoker.Invoked {
		t.Fatal("expected invoker.Invoke to be called")
	}
	if invoker.Provider != "echo" {
		t.Errorf("expected provider echo, got %q", invoker.Provider)
	}
	if invoker.Operation != "echo" {
		t.Errorf("expected operation echo, got %q", invoker.Operation)
	}
	if invoker.Params["data"] != "test" {
		t.Errorf("expected params data=test, got %v", invoker.Params["data"])
	}
}

func TestWebhookInvokesInvoker_Principal(t *testing.T) {
	t.Parallel()

	invoker := &testutil.StubInvoker{
		Result: &core.OperationResult{
			Status: http.StatusOK,
			Body:   `{"ok":true}`,
		},
	}
	b := makeBinding(t, "/incoming", "echo", "echo", invoker)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	body := bytes.NewBufferString(`{"data":"test"}`)
	req := httptest.NewRequest(http.MethodPost, "/incoming", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	b.Routes()[0].Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !invoker.Invoked {
		t.Fatal("expected invoker.Invoke to be called")
	}

	p := principal.FromContext(invoker.LastCtx)
	if p == nil {
		t.Fatal("expected principal in context")
	}
}

func TestWebhookInvokerError(t *testing.T) {
	t.Parallel()

	invoker := &testutil.StubInvoker{Err: fmt.Errorf("provider not available")}
	b := makeBinding(t, "/incoming", "echo", "echo", invoker)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	body := bytes.NewBufferString(`{"data":"test"}`)
	req := httptest.NewRequest(http.MethodPost, "/incoming", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	b.Routes()[0].Handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}
}

func TestWebhookKind(t *testing.T) {
	t.Parallel()

	b := makeBinding(t, "/incoming", "", "", &testutil.StubInvoker{})
	if b.Kind() != core.BindingTrigger {
		t.Fatalf("expected BindingTrigger, got %d", b.Kind())
	}
}

func TestWebhookFactory(t *testing.T) {
	t.Parallel()

	cfgYAML := `path: /hooks/test
provider: echo
operation: echo`
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(cfgYAML), &node); err != nil {
		t.Fatal(err)
	}

	def := config.BindingDef{
		Type:      "webhook",
		Config:    *node.Content[0],
		Providers: []string{"echo"},
	}

	binding, err := webhook.Factory(context.Background(), "test-hook", def, bootstrap.BindingDeps{Invoker: &testutil.StubInvoker{}})
	if err != nil {
		t.Fatalf("Factory: %v", err)
	}
	if binding.Name() != "test-hook" {
		t.Errorf("expected name test-hook, got %q", binding.Name())
	}
	routes := binding.Routes()
	if len(routes) != 1 || routes[0].Pattern != "/hooks/test" {
		t.Errorf("unexpected routes: %v", routes)
	}
}

func TestWebhookFactoryValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		path string
	}{
		{"empty path", ""},
		{"no leading slash", "incoming"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfgMap := map[string]string{}
			if tc.path != "" {
				cfgMap["path"] = tc.path
			}
			cfgYAML, err := yaml.Marshal(cfgMap)
			if err != nil {
				t.Fatal(err)
			}
			var node yaml.Node
			if err := yaml.Unmarshal(cfgYAML, &node); err != nil {
				t.Fatal(err)
			}
			def := config.BindingDef{Type: "webhook", Config: *node.Content[0]}
			_, err = webhook.Factory(context.Background(), "bad", def, bootstrap.BindingDeps{Invoker: &testutil.StubInvoker{}})
			if err == nil {
				t.Fatal("expected error for invalid path")
			}
		})
	}
}

func TestWebhookSignedMode_ValidSignature(t *testing.T) {
	t.Parallel()

	invoker := &testutil.StubInvoker{
		Result: &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`},
	}
	secret := "my-secret"
	b := makeBindingWithAuth(t, "/incoming", "echo", "echo", "signed", secret, "", "", invoker)

	payload := []byte(`{"data":"test"}`)
	sig := computeHMAC([]byte(secret), payload)

	req := httptest.NewRequest(http.MethodPost, "/incoming", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Signature", sig)
	w := httptest.NewRecorder()

	b.Routes()[0].Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !invoker.Invoked {
		t.Fatal("expected invoker to be invoked")
	}
}

func TestWebhookSignedMode_InvalidSignature(t *testing.T) {
	t.Parallel()

	invoker := &testutil.StubInvoker{
		Result: &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`},
	}
	b := makeBindingWithAuth(t, "/incoming", "echo", "echo", "signed", "my-secret", "", "", invoker)

	payload := []byte(`{"data":"test"}`)
	req := httptest.NewRequest(http.MethodPost, "/incoming", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Signature", "deadbeef")
	w := httptest.NewRecorder()

	b.Routes()[0].Handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if invoker.Invoked {
		t.Fatal("invoker should not be invoked for invalid signature")
	}
}

func TestWebhookSignedMode_MissingSignature(t *testing.T) {
	t.Parallel()

	invoker := &testutil.StubInvoker{
		Result: &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`},
	}
	b := makeBindingWithAuth(t, "/incoming", "echo", "echo", "signed", "my-secret", "", "", invoker)

	payload := []byte(`{"data":"test"}`)
	req := httptest.NewRequest(http.MethodPost, "/incoming", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	b.Routes()[0].Handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestWebhookTrustedUserHeader_Present(t *testing.T) {
	t.Parallel()

	invoker := &testutil.StubInvoker{
		Result: &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`},
	}
	b := makeBindingWithAuth(t, "/incoming", "echo", "echo", "trusted_user_header", "", "X-User-Email", "", invoker)

	payload := []byte(`{"data":"test"}`)
	req := httptest.NewRequest(http.MethodPost, "/incoming", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-Email", "user@example.com")
	w := httptest.NewRecorder()

	b.Routes()[0].Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !invoker.Invoked {
		t.Fatal("expected invoker to be invoked")
	}
	if invoker.LastP == nil {
		t.Fatal("expected principal to be passed to invoker")
	}
	if invoker.LastP.UserID != "user@example.com" {
		t.Errorf("expected UserID user@example.com, got %q", invoker.LastP.UserID)
	}
}

func TestWebhookTrustedUserHeader_Missing(t *testing.T) {
	t.Parallel()

	invoker := &testutil.StubInvoker{
		Result: &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`},
	}
	b := makeBindingWithAuth(t, "/incoming", "echo", "echo", "trusted_user_header", "", "X-User-Email", "", invoker)

	payload := []byte(`{"data":"test"}`)
	req := httptest.NewRequest(http.MethodPost, "/incoming", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	b.Routes()[0].Handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestWebhookFactoryValidation_AuthModes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		cfg     string
		wantErr string
	}{
		{
			name:    "signed without secret",
			cfg:     "path: /hook\nprovider: echo\noperation: echo\nauth_mode: signed",
			wantErr: "signing_secret is required",
		},
		{
			name:    "trusted_user_header without user_header",
			cfg:     "path: /hook\nprovider: echo\noperation: echo\nauth_mode: trusted_user_header",
			wantErr: "user_header is required",
		},
		{
			name:    "unknown auth_mode",
			cfg:     "path: /hook\nprovider: echo\noperation: echo\nauth_mode: magic",
			wantErr: "unknown auth_mode",
		},
		{
			name: "signed with secret is valid",
			cfg:  "path: /hook\nprovider: echo\noperation: echo\nauth_mode: signed\nsigning_secret: s3cret",
		},
		{
			name: "trusted_user_header with header is valid",
			cfg:  "path: /hook\nprovider: echo\noperation: echo\nauth_mode: trusted_user_header\nuser_header: X-User",
		},
		{
			name: "public is valid",
			cfg:  "path: /hook\nprovider: echo\noperation: echo\nauth_mode: public",
		},
		{
			name: "empty auth_mode defaults to public",
			cfg:  "path: /hook\nprovider: echo\noperation: echo",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var node yaml.Node
			if err := yaml.Unmarshal([]byte(tc.cfg), &node); err != nil {
				t.Fatal(err)
			}
			def := config.BindingDef{Type: "webhook", Config: *node.Content[0], Providers: []string{"echo"}}
			_, err := webhook.Factory(context.Background(), "test", def, bootstrap.BindingDeps{Invoker: &testutil.StubInvoker{}})
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tc.wantErr, err.Error())
				}
			}
		})
	}
}

func TestWebhookFactoryRejectsUnlistedProvider(t *testing.T) {
	t.Parallel()

	cfgYAML := "path: /hook\nprovider: echo\noperation: echo"
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(cfgYAML), &node); err != nil {
		t.Fatal(err)
	}

	def := config.BindingDef{
		Type:      "webhook",
		Config:    *node.Content[0],
		Providers: []string{"slack", "github"},
	}
	_, err := webhook.Factory(context.Background(), "test", def, bootstrap.BindingDeps{Invoker: &testutil.StubInvoker{}})
	if err == nil {
		t.Fatal("expected error for unlisted provider")
	}
	if !strings.Contains(err.Error(), "not in the binding's allowed providers") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWebhookFactoryRequiresAllowlistForForwarding(t *testing.T) {
	t.Parallel()

	cfgYAML := "path: /hook\nprovider: echo\noperation: echo"
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(cfgYAML), &node); err != nil {
		t.Fatal(err)
	}

	def := config.BindingDef{
		Type:   "webhook",
		Config: *node.Content[0],
	}
	_, err := webhook.Factory(context.Background(), "test", def, bootstrap.BindingDeps{Invoker: &testutil.StubInvoker{}})
	if err == nil {
		t.Fatal("expected error for missing allowlist")
	}
	if !strings.Contains(err.Error(), "requires a non-empty binding providers allowlist") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func makeBinding(t *testing.T, path, provider, operation string, invoker invocation.Invoker) core.Binding {
	t.Helper()

	cfgMap := map[string]string{"path": path}
	if provider != "" {
		cfgMap["provider"] = provider
	}
	if operation != "" {
		cfgMap["operation"] = operation
	}

	cfgYAML, err := yaml.Marshal(cfgMap)
	if err != nil {
		t.Fatal(err)
	}
	var node yaml.Node
	if err := yaml.Unmarshal(cfgYAML, &node); err != nil {
		t.Fatal(err)
	}

	def := config.BindingDef{Type: "webhook", Config: *node.Content[0]}
	if provider != "" {
		def.Providers = []string{provider}
	}
	b, err := webhook.Factory(context.Background(), "test-webhook", def, bootstrap.BindingDeps{Invoker: invoker})
	if err != nil {
		t.Fatalf("Factory: %v", err)
	}
	return b
}

func makeBindingWithAuth(t *testing.T, path, provider, operation, authMode, signingSecret, userHeader, sigHeader string, invoker invocation.Invoker) core.Binding {
	t.Helper()

	cfgMap := map[string]string{"path": path}
	if provider != "" {
		cfgMap["provider"] = provider
	}
	if operation != "" {
		cfgMap["operation"] = operation
	}
	if authMode != "" {
		cfgMap["auth_mode"] = authMode
	}
	if signingSecret != "" {
		cfgMap["signing_secret"] = signingSecret
	}
	if userHeader != "" {
		cfgMap["user_header"] = userHeader
	}
	if sigHeader != "" {
		cfgMap["signature_header"] = sigHeader
	}

	cfgYAML, err := yaml.Marshal(cfgMap)
	if err != nil {
		t.Fatal(err)
	}
	var node yaml.Node
	if err := yaml.Unmarshal(cfgYAML, &node); err != nil {
		t.Fatal(err)
	}

	def := config.BindingDef{Type: "webhook", Config: *node.Content[0]}
	if provider != "" {
		def.Providers = []string{provider}
	}
	b, err := webhook.Factory(context.Background(), "test-webhook", def, bootstrap.BindingDeps{Invoker: invoker})
	if err != nil {
		t.Fatalf("Factory: %v", err)
	}
	return b
}

func computeHMAC(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
