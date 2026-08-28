package server_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestCreateAuthorizationSubjectCreatesManagedSubject(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Services = testutil.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"id":"ingress-verify-probe","displayName":"Ingress verify probe"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/authorization/subjects", body)
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

	var created struct {
		SubjectID   string `json:"subjectId"`
		DisplayName string `json:"displayName"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if created.SubjectID != "service_account:ingress-verify-probe" {
		t.Fatalf("subjectId = %q, want service_account:ingress-verify-probe", created.SubjectID)
	}
	if created.DisplayName != "Ingress verify probe" {
		t.Fatalf("displayName = %q, want Ingress verify probe", created.DisplayName)
	}
}

func TestCreateAuthorizationSubjectTokenForwardsGrantSubject(t *testing.T) {
	t.Parallel()

	authz := &serviceAccountCredentialAuthorizationProvider{allowed: true}
	svc := testutil.NewStubServices(t)
	seedUserRecord(t, svc, testCanonicalAdminUserID, "grant-test@example.test", time.Now())
	var stub *grantTrackingAuthStub
	ts := newTestServer(t, func(cfg *server.Config) {
		stub = configureGrantTestAuthForUser(cfg, testCanonicalAdminUserID)
		cfg.Authorization = authz
		cfg.Providers = grantTestProviders(t)
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	createReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/authorization/subjects", bytes.NewBufferString(`{"id":"probe-bot","displayName":"Probe bot"}`))
	createReq.Header.Set("Content-Type", "application/json")
	addGrantTestSessionCookie(createReq)
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatalf("create subject: %v", err)
	}
	_ = createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create subject status = %d, want 201", createResp.StatusCode)
	}

	body := bytes.NewBufferString(`{"name":"ingress-verify-sa","permissions":["testapp:list"]}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/authorization/subjects/probe-bot/tokens", body)
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
	if stub.lastTokenExchangeReq.GrantSubject != "service_account:probe-bot" {
		t.Fatalf("GrantSubject = %q, want service_account:probe-bot", stub.lastTokenExchangeReq.GrantSubject)
	}
	if stub.lastTokenExchangeReq.Name != "ingress-verify-sa" {
		t.Fatalf("Name = %q, want ingress-verify-sa", stub.lastTokenExchangeReq.Name)
	}
	if stub.lastTokenExchangeReq.Scope != "testapp:list" {
		t.Fatalf("Scope = %q, want testapp:list", stub.lastTokenExchangeReq.Scope)
	}

	var created struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if created.Token == "" {
		t.Fatal("expected token in create response")
	}
}
