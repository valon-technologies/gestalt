package server_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestCreateTokenRequestDecodesExpiresIn(t *testing.T) {
	t.Parallel()

	type createTokenRequest struct {
		Name      string `json:"name"`
		Scopes    string `json:"scopes"`
		ExpiresIn *int64 `json:"expiresIn,omitempty"`
	}

	var req createTokenRequest
	if err := json.Unmarshal([]byte(`{"name":"ci","scopes":"my-app","expiresIn":2592000}`), &req); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if req.Name != "ci" || req.Scopes != "my-app" {
		t.Fatalf("name=%q scopes=%q, want ci/my-app", req.Name, req.Scopes)
	}
	if req.ExpiresIn == nil || *req.ExpiresIn != 2592000 {
		t.Fatalf("ExpiresIn = %v, want 2592000", req.ExpiresIn)
	}

	var req2 createTokenRequest
	if err := json.Unmarshal([]byte(`{"name":"ci","scopes":"my-app"}`), &req2); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if req2.ExpiresIn != nil {
		t.Fatalf("ExpiresIn = %v, want nil when omitted", req2.ExpiresIn)
	}
}

func TestCreateTokenRequestRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	type createTokenRequest struct {
		Name      string `json:"name"`
		Scopes    string `json:"scopes"`
		ExpiresIn *int64 `json:"expiresIn,omitempty"`
	}

	var req createTokenRequest
	dec := json.NewDecoder(strings.NewReader(`{"name":"ci","scopes":"my-app","unknown":123}`))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err == nil {
		t.Fatal("Decode() error = nil, want unknown field error")
	}
}

func TestCreateAPITokenForwardsExpiresIn(t *testing.T) {
	t.Parallel()

	stub := newGrantTrackingAuthStub()
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = stub
		cfg.Providers = grantTestProviders(t)
		cfg.Services = testutil.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"name":"ci-pipeline","scopes":"testapp","expiresIn":7776000}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/tokens", body)
	req.Header.Set("Content-Type", "application/json")
	addGrantTestSessionCookie(req)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 201: %s", resp.StatusCode, respBody)
	}
	if stub.lastTokenExchangeReq == nil {
		t.Fatal("token exchange request was not captured")
	}
	if stub.lastTokenExchangeCaller != "user:test-user" {
		t.Fatalf("token exchange CallerSubjectID = %q, want user:test-user", stub.lastTokenExchangeCaller)
	}
	if stub.lastTokenExchangeReq.ExpiresIn != 7776000 {
		t.Fatalf("ExpiresIn = %d, want 7776000", stub.lastTokenExchangeReq.ExpiresIn)
	}

	var created struct {
		ExpiresAt *time.Time `json:"expiresAt"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if created.ExpiresAt == nil {
		t.Fatal("expected expiresAt in create response")
	}
	wantExpiry := time.Now().UTC().Add(90 * 24 * time.Hour)
	if created.ExpiresAt.Before(wantExpiry.Add(-time.Minute)) || created.ExpiresAt.After(wantExpiry.Add(time.Minute)) {
		t.Fatalf("expiresAt = %s, want about %s", created.ExpiresAt, wantExpiry)
	}
}

func TestCreateAPITokenOmitsExpiresInWhenUnset(t *testing.T) {
	t.Parallel()

	stub := newGrantTrackingAuthStub()
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = stub
		cfg.Providers = grantTestProviders(t)
		cfg.Services = testutil.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"name":"default-ttl","scopes":"testapp"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/tokens", body)
	req.Header.Set("Content-Type", "application/json")
	addGrantTestSessionCookie(req)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 201: %s", resp.StatusCode, respBody)
	}
	if stub.lastTokenExchangeReq == nil {
		t.Fatal("token exchange request was not captured")
	}
	if stub.lastTokenExchangeReq.ExpiresIn != 0 {
		t.Fatalf("ExpiresIn = %d, want 0 when omitted", stub.lastTokenExchangeReq.ExpiresIn)
	}
}

func TestCreateAPITokenRejectsInvalidExpiresIn(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		configureGrantTestAuth(cfg)
		cfg.Providers = grantTestProviders(t)
		cfg.Services = testutil.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	for _, payload := range []string{
		`{"name":"bad","scopes":"testapp","expiresIn":-1}`,
		`{"name":"bad","scopes":"testapp","expiresIn":31536001}`,
	} {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/tokens", bytes.NewBufferString(payload))
		req.Header.Set("Content-Type", "application/json")
		addGrantTestSessionCookie(req)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("payload %s status = %d, want 400: %s", payload, resp.StatusCode, respBody)
		}
	}
}
