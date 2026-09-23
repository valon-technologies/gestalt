package server_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/scim"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

func newSCIMAdminTestServer(t *testing.T) (*httptest.Server, *coredata.Services, *scim.Runtime) {
	t.Helper()
	svc := testutil.NewStubServices(t)
	user := seedUserRecord(t, svc, "scim-admin-user", "scim-admin-user@example.test", time.Now())
	runtime := scim.NewRuntime(nil, config.ServerSCIMConfig{})
	authz := &serverTestAuthorizationProvider{
		resourceTypes: []*proto.AuthorizationModelResourceType{{
			Name: "group",
			Relations: []*proto.ModelRelation{{
				Name: "member",
				AllowedTargets: []*proto.ModelAllowedTarget{
					{Kind: &proto.ModelAllowedTarget_SubjectType{SubjectType: "subject"}},
					{Kind: &proto.ModelAllowedTarget_SubjectSetType{SubjectSetType: &proto.SubjectSetType{ResourceType: "group", Relation: "member"}}},
				},
			}},
		}, {
			Name:    "gestalt",
			Actions: []*proto.ModelAction{{Name: "admin", Relations: []string{"admin"}}},
		}},
		relationships: subjectSetGrant(principal.UserSubjectID(user.ID), "admin", "gestalt", "gestalt"),
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = coretesting.NamedIntrospectIdentityStub("test", func(_ context.Context, token string) (*core.UserIdentity, error) {
			if token != "session-token" {
				return nil, core.ErrNotFound
			}
			return &core.UserIdentity{Email: user.Email}, nil
		})
		cfg.Services = svc
		cfg.Authorization = authz
		cfg.Admin = server.AdminRouteConfig{AuthorizationPolicy: "gestalt", AuthorizationAction: "admin"}
		cfg.SCIMRuntime = runtime
		cfg.SCIMConfigFallback = config.ServerSCIMConfig{Clients: map[string]config.SCIMClientConfig{}}
		cfg.StateSecret = []byte("0123456789abcdef0123456789abcdef")
		cfg.SCIMRuntimeWritesEnabled = true
	})
	return ts, svc, runtime
}

func doSCIMAdmin(t *testing.T, method, url, body string) (*http.Response, string) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "session-token"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(resp.Body)
	return resp, string(payload)
}

func TestAdminSCIMConfigSourceFallback(t *testing.T) {
	t.Parallel()
	ts, svc, _ := newSCIMAdminTestServer(t)
	_, _ = ts, svc
	resp, body := doSCIMAdmin(t, http.MethodGet, ts.URL+"/admin/api/v1/scim/clients", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.StatusCode, body)
	}
}

func TestAdminSCIMClientCRUDAndHotReload(t *testing.T) {
	t.Parallel()
	ts, svc, runtime := newSCIMAdminTestServer(t)
	create := `{"id":"rippling","credentials":[{"id":"current","token":"token-one"}],"authoritativeUserDomains":["valon.com"],"activeUserRelationships":[{"relation":"member","resourceType":"group","resourceId":"employees"}],"enabled":true}`
	resp, body := doSCIMAdmin(t, http.MethodPost, ts.URL+"/admin/api/v1/scim/clients", create)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d: %s", resp.StatusCode, body)
	}
	if strings.Contains(body, "token-one") {
		t.Fatalf("token leaked in response: %s", body)
	}
	saved, err := svc.SCIMConfig.Get(context.Background(), "rippling")
	if err != nil || len(saved.Credentials) != 0 {
		t.Fatalf("saved client unexpectedly retained plaintext credential metadata: saved = %#v, err = %v", saved, err)
	}
	secrets, secretErr := svc.SCIMConfig.Secrets(context.Background(), "rippling")
	if secretErr != nil || len(secrets) != 1 || secrets[0].CredentialID != "current" || len(secrets[0].Ciphertext) == 0 {
		t.Fatalf("secrets = %#v, err = %v", secrets, secretErr)
	}
	if strings.Contains(string(secrets[0].Ciphertext), "token-one") {
		t.Fatal("SCIM credential ciphertext contains plaintext")
	}
	if runtime.Service() == nil || !runtime.Service().Enabled() {
		t.Fatal("runtime service was not enabled after create")
	}
	if _, ok := runtime.Service().ClientForToken("token-one"); !ok {
		t.Fatal("new token is not active")
	}
	patch := `{"credentials":[{"id":"current","token":"token-two"}],"authoritativeUserDomains":["valon.com"],"activeUserRelationships":[{"relation":"member","resourceType":"group","resourceId":"employees"}],"enabled":true,"revision":1}`
	resp, body = doSCIMAdmin(t, http.MethodPatch, ts.URL+"/admin/api/v1/scim/clients/rippling", patch)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch status = %d: %s", resp.StatusCode, body)
	}
	if _, ok := runtime.Service().ClientForToken("token-one"); ok {
		t.Fatal("old token remains active")
	}
	if _, ok := runtime.Service().ClientForToken("token-two"); !ok {
		t.Fatal("new token is not active")
	}
	resp, body = doSCIMAdmin(t, http.MethodDelete, ts.URL+"/admin/api/v1/scim/clients/rippling", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d: %s", resp.StatusCode, body)
	}
	if _, ok := runtime.Service().ClientForToken("token-two"); ok {
		t.Fatal("disabled client token remains active")
	}
	if _, err := svc.SCIMConfig.Get(context.Background(), "rippling"); err != nil {
		t.Fatalf("disabled client was deleted: %v", err)
	}
}

func TestAdminSCIMValidationAndConflict(t *testing.T) {
	t.Parallel()
	ts, _, _ := newSCIMAdminTestServer(t)
	create := `{"id":"rippling","credentials":[{"id":"current","token":"token-one"}],"authoritativeUserDomains":["valon.com"],"activeUserRelationships":[{"relation":"member","resourceType":"group","resourceId":"employees"}],"enabled":true}`
	resp, body := doSCIMAdmin(t, http.MethodPost, ts.URL+"/admin/api/v1/scim/clients", create)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d: %s", resp.StatusCode, body)
	}
	bad := `{"id":"rippling","credentials":[{"id":"current","token":"token-one"}],"authoritativeUserDomains":["valon.com"],"activeUserRelationships":[{"relation":"member","resourceType":"group","resourceId":"employees"}],"enabled":true,"revision":99}`
	resp, body = doSCIMAdmin(t, http.MethodPatch, ts.URL+"/admin/api/v1/scim/clients/rippling", bad)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("conflict status = %d: %s", resp.StatusCode, body)
	}
	badCreate := `{"id":"rippling","credentials":[{"id":"current","token":"token-two"}],"authoritativeUserDomains":["valon.com"],"activeUserRelationships":[{"relation":"member","resourceType":"group","resourceId":"employees"}],"enabled":true}`
	resp, body = doSCIMAdmin(t, http.MethodPost, ts.URL+"/admin/api/v1/scim/clients", badCreate)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate status = %d: %s", resp.StatusCode, body)
	}
}

func TestAdminSCIMListsFallbackSourceAndCredentialIDs(t *testing.T) {
	t.Parallel()
	ts, svc, _ := newSCIMAdminTestServer(t)
	_ = svc

	resp, body := doSCIMAdmin(t, http.MethodGet, ts.URL+"/admin/api/v1/scim/clients", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("fallback list status = %d: %s", resp.StatusCode, body)
	}
	var payload struct {
		Clients []struct {
			ID          string `json:"id"`
			Credentials []struct {
				ID string `json:"id"`
			} `json:"credentials"`
		} `json:"clients"`
		Source string `json:"source"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decode fallback list: %v", err)
	}
	if payload.Source != "config" {
		t.Fatalf("source = %q, want config", payload.Source)
	}
	if len(payload.Clients) != 0 {
		t.Fatalf("fallback clients = %#v, want none", payload.Clients)
	}

	create := `{"id":"rippling","credentials":[{"id":"current","token":"token-one"}],"activeUserRelationships":[{"relation":"member","resourceType":"group","resourceId":"employees"}],"enabled":true}`
	resp, body = doSCIMAdmin(t, http.MethodPost, ts.URL+"/admin/api/v1/scim/clients", create)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d: %s", resp.StatusCode, body)
	}
	resp, body = doSCIMAdmin(t, http.MethodGet, ts.URL+"/admin/api/v1/scim/clients", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("runtime list status = %d: %s", resp.StatusCode, body)
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decode runtime list: %v", err)
	}
	if payload.Source != "runtime" {
		t.Fatalf("source = %q, want runtime", payload.Source)
	}
	if len(payload.Clients) != 1 || payload.Clients[0].ID != "rippling" || len(payload.Clients[0].Credentials) != 1 || payload.Clients[0].Credentials[0].ID != "current" {
		t.Fatalf("runtime clients = %#v", payload.Clients)
	}
}

func TestAdminSCIMRemovedCredentialIsRevoked(t *testing.T) {
	t.Parallel()
	ts, svc, runtime := newSCIMAdminTestServer(t)
	create := `{"id":"rippling","credentials":[{"id":"current","token":"token-one"},{"id":"next","token":"token-two"}],"activeUserRelationships":[{"relation":"member","resourceType":"group","resourceId":"employees"}],"enabled":true}`
	resp, body := doSCIMAdmin(t, http.MethodPost, ts.URL+"/admin/api/v1/scim/clients", create)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d: %s", resp.StatusCode, body)
	}
	patch := `{"credentials":[{"id":"next"}],"activeUserRelationships":[{"relation":"member","resourceType":"group","resourceId":"employees"}],"enabled":true,"revision":1}`
	resp, body = doSCIMAdmin(t, http.MethodPatch, ts.URL+"/admin/api/v1/scim/clients/rippling", patch)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch status = %d: %s", resp.StatusCode, body)
	}
	if _, ok := runtime.Service().ClientForToken("token-one"); ok {
		t.Fatal("removed credential remains active")
	}
	secrets, err := svc.SCIMConfig.Secrets(context.Background(), "rippling")
	if err != nil || len(secrets) != 1 || secrets[0].CredentialID != "next" {
		t.Fatalf("secrets = %#v, err = %v", secrets, err)
	}
}
