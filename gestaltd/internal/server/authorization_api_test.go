package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/server"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

func TestAuthorizationAPICheckAccess(t *testing.T) {
	authz := &authorizationAPITestProvider{}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Authorization = authz
	})
	defer ts.Close()
	t.Parallel()

	resp := doAuthorizationJSONRequest(t, http.MethodPost, ts.URL+"/api/v1/authorization/check-access", `{
		"subject": {"type": "subject", "id": "user:alice"},
		"action": {"name": "view"},
		"resource": {"type": "group", "id": "engineering"}
	}`)
	defer closeResponseBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}
	if got := authz.checkAccessRequest.GetSubject().GetId(); got != "user:alice" {
		t.Fatalf("subject id = %q, want user:alice", got)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["allowed"] != true || body["modelId"] != "model-1" {
		t.Fatalf("response = %#v, want allowed true model-1", body)
	}
}

func doAuthorizationJSONRequest(t *testing.T, method, url, body string) *http.Response {
	t.Helper()
	var reader io.Reader
	if strings.TrimSpace(body) != "" {
		reader = bytes.NewBufferString(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if reader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	return resp
}

func closeResponseBody(t *testing.T, resp *http.Response) {
	t.Helper()
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("close response body: %v", err)
	}
}

type authorizationAPITestProvider struct {
	core.AuthorizationProvider

	checkAccessRequest *proto.CheckAccessRequest
}

func (p *authorizationAPITestProvider) CheckAccess(_ context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	p.checkAccessRequest = req
	return &proto.CheckAccessResponse{Allowed: true, ModelId: "model-1"}, nil
}
