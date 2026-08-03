package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

func preferredInstancePluginDefs() map[string]*config.ProviderEntry {
	return map[string]*config.ProviderEntry{
		"manual-multi": {
			Connections: map[string]*config.ConnectionDef{
				testDefaultConnection: {
					ConnectionID: "manual-multi:" + testDefaultConnection,
					Mode:         providermanifestv1.ConnectionModeSubject,
					Auth: config.ConnectionAuthDef{
						Type: providermanifestv1.AuthTypeManual,
					},
				},
			},
		},
	}
}

func preferredInstanceTestSetup(t *testing.T) (*coredata.Services, string, *httptest.Server) {
	t.Helper()
	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	subjectID := principal.UserSubjectID(u.ID)
	seedSubjectToken(t, svc, subjectID, "manual-multi", testDefaultConnection, "team-a", "team-a-token")
	seedSubjectToken(t, svc, subjectID, "manual-multi", testDefaultConnection, "team-b", "team-b-token")

	providers := testutil.NewProviderRegistry(t,
		&stubManualProvider{StubIntegration: coretesting.StubIntegration{N: "manual-multi", DN: "Manual Multi"}},
	)
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = providers
		cfg.AppDefs = preferredInstancePluginDefs()
		cfg.Services = svc
		cfg.DefaultConnection = map[string]string{"manual-multi": testDefaultConnection}
	})
	testutil.CloseOnCleanup(t, ts)
	return svc, subjectID, ts
}

func TestSelectPreferredInstance_Succeeds(t *testing.T) {
	t.Parallel()

	svc, subjectID, ts := preferredInstanceTestSetup(t)
	body, _ := json.Marshal(map[string]string{"instance": "team-a"})
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/v1/apps/manual-multi/preferred-instance", bytes.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, respBody)
	}
	var got map[string]string
	if err := json.Unmarshal(respBody, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["status"] != "selected" || got["integration"] != "manual-multi" || got["instance"] != "team-a" {
		t.Fatalf("response = %#v", got)
	}
	pref, err := svc.ConnectionInstancePreferences.Get(context.Background(), subjectID, "manual-multi:"+testDefaultConnection)
	if err != nil {
		t.Fatalf("Get preference: %v", err)
	}
	if pref.Instance != "team-a" {
		t.Fatalf("stored instance = %q, want team-a", pref.Instance)
	}
}

func TestListIntegrations_PreferredInstanceShowsReady(t *testing.T) {
	t.Parallel()

	svc, subjectID, ts := preferredInstanceTestSetup(t)
	if _, err := svc.ConnectionInstancePreferences.Set(context.Background(), subjectID, "manual-multi:"+testDefaultConnection, "team-a"); err != nil {
		t.Fatalf("Set preference: %v", err)
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	type statusConnection struct {
		Name              string           `json:"name"`
		Status            string           `json:"status"`
		CredentialState   string           `json:"credentialState"`
		HealthState       string           `json:"healthState"`
		Actions           []string         `json:"actions"`
		PreferredInstance string           `json:"preferredInstance"`
		Instances         []map[string]any `json:"instances"`
	}
	type statusIntegration struct {
		Name            string             `json:"name"`
		Connections     []statusConnection `json:"connections"`
		Status          string             `json:"status"`
		CredentialState string             `json:"credentialState"`
		HealthState     string             `json:"healthState"`
		Actions         []string           `json:"actions"`
	}
	var integrations []statusIntegration
	if err := json.Unmarshal(body, &integrations); err != nil {
		t.Fatalf("decode integrations: %v (body: %s)", err, body)
	}
	var integration statusIntegration
	for _, item := range integrations {
		if item.Name == "manual-multi" {
			integration = item
			break
		}
	}
	if integration.Name == "" {
		t.Fatalf("manual-multi missing from response: %s", body)
	}
	if integration.Status != "ready" || integration.CredentialState != "connected" || integration.HealthState != "not_checked" {
		t.Fatalf("integration status = {status:%q credential:%q health:%q}, want ready connected not_checked", integration.Status, integration.CredentialState, integration.HealthState)
	}
	if !reflect.DeepEqual(integration.Actions, []string{"disconnect", "add_instance"}) {
		t.Fatalf("integration actions = %v, want [disconnect add_instance]", integration.Actions)
	}
	if len(integration.Connections) != 1 {
		t.Fatalf("connections = %+v", integration.Connections)
	}
	conn := integration.Connections[0]
	if conn.PreferredInstance != "team-a" {
		t.Fatalf("preferredInstance = %q, want team-a", conn.PreferredInstance)
	}
}

func TestExecuteOperation_UsesPreferredInstanceWithoutParam(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	subjectID := principal.UserSubjectID(u.ID)
	seedSubjectToken(t, svc, subjectID, "manual-multi", testDefaultConnection, "team-a", "team-a-token")
	seedSubjectToken(t, svc, subjectID, "manual-multi", testDefaultConnection, "team-b", "team-b-token")
	if _, err := svc.ConnectionInstancePreferences.Set(context.Background(), subjectID, "manual-multi:"+testDefaultConnection, "team-b"); err != nil {
		t.Fatalf("Set preference: %v", err)
	}

	fullStub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N: "manual-multi",
			ExecuteFn: func(_ context.Context, op string, _ map[string]any, token string) (*core.OperationResult, error) {
				return &core.OperationResult{
					Status: http.StatusOK,
					Body:   []byte(`{"operation":"` + op + `","token":"` + token + `"}`),
				}, nil
			},
		},
		ops: []core.Operation{{Name: "do_thing", Method: http.MethodGet}},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, fullStub)
		cfg.AppDefs = preferredInstancePluginDefs()
		cfg.Services = svc
		cfg.DefaultConnection = map[string]string{"manual-multi": testDefaultConnection}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/manual-multi/do_thing", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}
	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result["token"] != "team-b-token" {
		t.Fatalf("token = %q, want team-b-token", result["token"])
	}
}
