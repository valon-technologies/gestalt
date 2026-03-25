package proxy_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/egress"
	"github.com/valon-technologies/gestalt/plugins/bindings/proxy"
	"gopkg.in/yaml.v3"
)

func TestProxyNormalizeRequest(t *testing.T) {
	t.Parallel()

	b := makeBinding(t, "/proxy")
	req := httptest.NewRequest(http.MethodPost, "/proxy/messages?cursor=123", bytes.NewBufferString("hello"))
	req.Host = "api.example.com"
	req.Header.Set("X-Proxy-Token", "abc")

	w := httptest.NewRecorder()
	b.Routes()[0].Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", w.Code)
	}

	var resp struct {
		Note   string             `json:"note"`
		Policy egress.PolicyInput `json:"policy_input"`
		Target egress.Target      `json:"target"`
		Body   string             `json:"body"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Target.Host != "api.example.com" {
		t.Fatalf("target host = %q, want api.example.com", resp.Target.Host)
	}
	if resp.Target.Method != http.MethodPost {
		t.Fatalf("target method = %q, want POST", resp.Target.Method)
	}
	if resp.Target.Path != "/messages?cursor=123" {
		t.Fatalf("target path = %q, want /messages?cursor=123", resp.Target.Path)
	}
	if resp.Policy.Subject.Kind != egress.SubjectSystem {
		t.Fatalf("subject kind = %q, want system", resp.Policy.Subject.Kind)
	}
	if resp.Body != "hello" {
		t.Fatalf("body = %q, want hello", resp.Body)
	}
}

func makeBinding(t *testing.T, path string) *proxy.Binding {
	t.Helper()

	cfgYAML := "path: " + path
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(cfgYAML), &node); err != nil {
		t.Fatal(err)
	}

	binding, err := proxy.Factory(context.Background(), "proxy-surface", config.BindingDef{
		Type:   "proxy",
		Config: *node.Content[0],
	}, bootstrap.BindingDeps{})
	if err != nil {
		t.Fatalf("Factory: %v", err)
	}

	return binding.(*proxy.Binding)
}
