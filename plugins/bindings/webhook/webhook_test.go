package webhook_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/valon-technologies/toolshed/core"
	"github.com/valon-technologies/toolshed/internal/config"
	"github.com/valon-technologies/toolshed/plugins/bindings/webhook"
	"gopkg.in/yaml.v3"
)

type stubBroker struct {
	invoked   bool
	provider  string
	operation string
	params    map[string]any
	result    *core.OperationResult
	err       error
}

func (b *stubBroker) Invoke(_ context.Context, req core.InvocationRequest) (*core.OperationResult, error) {
	b.invoked = true
	b.provider = req.Provider
	b.operation = req.Operation
	b.params = req.Params
	if b.err != nil {
		return nil, b.err
	}
	return b.result, nil
}

func (b *stubBroker) ListCapabilities() []core.Capability { return nil }

func TestWebhookRoutes(t *testing.T) {
	t.Parallel()

	b := makeBinding(t, "/incoming", "", "", &stubBroker{})

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

	b := makeBinding(t, "/incoming", "", "", &stubBroker{})
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

func TestWebhookInvokesBroker(t *testing.T) {
	t.Parallel()

	brk := &stubBroker{
		result: &core.OperationResult{
			Status: http.StatusOK,
			Body:   `{"echoed":true}`,
		},
	}
	b := makeBinding(t, "/incoming", "echo", "echo", brk)
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
	if !brk.invoked {
		t.Fatal("expected broker.Invoke to be called")
	}
	if brk.provider != "echo" {
		t.Errorf("expected provider echo, got %q", brk.provider)
	}
	if brk.operation != "echo" {
		t.Errorf("expected operation echo, got %q", brk.operation)
	}
	if brk.params["data"] != "test" {
		t.Errorf("expected params data=test, got %v", brk.params["data"])
	}
}

func TestWebhookBrokerError(t *testing.T) {
	t.Parallel()

	brk := &stubBroker{err: fmt.Errorf("provider not available")}
	b := makeBinding(t, "/incoming", "echo", "echo", brk)
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

	b := makeBinding(t, "/incoming", "", "", &stubBroker{})
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
		Type:   "webhook",
		Config: *node.Content[0],
	}

	binding, err := webhook.Factory(context.Background(), "test-hook", def, &stubBroker{})
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
			_, err = webhook.Factory(context.Background(), "bad", def, &stubBroker{})
			if err == nil {
				t.Fatal("expected error for invalid path")
			}
		})
	}
}

func makeBinding(t *testing.T, path, provider, operation string, brk core.Broker) core.Binding {
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
	b, err := webhook.Factory(context.Background(), "test-webhook", def, brk)
	if err != nil {
		t.Fatalf("Factory: %v", err)
	}
	return b
}
