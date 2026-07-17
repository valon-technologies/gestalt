package publicclient_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/valon-technologies/gestalt/sdk/go/publicclient"
	"github.com/valon-technologies/gestalt/sdk/go/publicclient/generated"
)

func TestRestClientSurfaceIsFiveServices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := publicclient.NewRESTClientForTest(server.URL, publicclient.Unauthenticated(), server.Client())
	defer func() { _ = client.Close() }()

	if client.App == nil || client.Agent == nil || client.Workflow == nil ||
		client.Identity == nil || client.Authorization == nil {
		t.Fatal("REST client missing a REST-capable service client")
	}
}

func TestAgentGetSessionUsesRESTPath(t *testing.T) {
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = w.Write([]byte(`{"session":{}}`))
	}))
	defer server.Close()

	client := publicclient.NewRESTClientForTest(server.URL, publicclient.Unauthenticated(), server.Client())
	defer func() { _ = client.Close() }()

	_, err := client.Agent.GetSession(context.Background(), &generated.GetAgentProviderSessionRequest{
		SessionId: "sess-1",
	})
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if path != "/api/v2/agent/sessions/sess-1" {
		t.Fatalf("path = %q", path)
	}
}

func TestAuthorizationGetActiveModelRefEmptyInput(t *testing.T) {
	var method string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		_, _ = w.Write([]byte(`{"modelRef":"authz/default"}`))
	}))
	defer server.Close()

	client := publicclient.NewRESTClientForTest(server.URL, publicclient.Unauthenticated(), server.Client())
	defer func() { _ = client.Close() }()

	_, err := client.Authorization.GetActiveModelRef(context.Background())
	if err != nil {
		t.Fatalf("GetActiveModelRef: %v", err)
	}
	if method != http.MethodGet {
		t.Fatalf("method = %q, want GET", method)
	}
}

func TestBoundClientIsAppOnly(t *testing.T) {
	var client publicclient.BoundClient
	_ = client.App
}
