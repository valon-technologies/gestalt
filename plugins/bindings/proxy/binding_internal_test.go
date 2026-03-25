package proxy

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/valon-technologies/gestalt/internal/egress"
	"github.com/valon-technologies/gestalt/internal/egress/egresstest"
)

func TestBindingNormalizeRunsResolver(t *testing.T) {
	t.Parallel()

	var got egress.PolicyInput
	b := New("agent-proxy", proxyConfig{Path: "/proxy"}, egress.Resolver{
		Policy: egresstest.PolicyFunc(func(_ context.Context, input egress.PolicyInput) error {
			got = input
			return nil
		}),
	}, nil)

	req := httptest.NewRequest(http.MethodPost, "/proxy/messages?cursor=123", bytes.NewBufferString("hello"))
	req.Host = "api.example.com"
	req.Header.Set("X-Proxy-Token", "abc")

	w := httptest.NewRecorder()
	b.Routes()[0].Handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", w.Code)
	}
	if got.Subject != (egress.Subject{Kind: egress.SubjectSystem, ID: "agent-proxy"}) {
		t.Fatalf("subject = %+v, want system agent-proxy", got.Subject)
	}
	if got.Target.Path != "/messages?cursor=123" {
		t.Fatalf("target path = %q, want /messages?cursor=123", got.Target.Path)
	}
	if got.Headers["X-Proxy-Token"] != "abc" {
		t.Fatalf("header = %q, want abc", got.Headers["X-Proxy-Token"])
	}
}
