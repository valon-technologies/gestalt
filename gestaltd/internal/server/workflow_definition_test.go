package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type workflowDefinitionHTTPResponse struct {
	ID                 string `json:"id"`
	Provider           string `json:"provider"`
	Generation         int64  `json:"generation"`
	Paused             bool   `json:"paused"`
	CreatedBySubjectID string `json:"createdBySubjectId"`
	Target             struct {
		Steps []struct {
			ID  string `json:"id"`
			App *struct {
				Name      string `json:"name"`
				Operation string `json:"operation"`
			} `json:"app"`
		} `json:"steps"`
	} `json:"target"`
	Activations []struct {
		ID       string         `json:"id"`
		Input    map[string]any `json:"input"`
		Paused   bool           `json:"paused"`
		Schedule *struct {
			Cron     string `json:"cron"`
			Timezone string `json:"timezone"`
		} `json:"schedule"`
		Event *struct {
			Match struct {
				Type    string `json:"type"`
				Source  string `json:"source"`
				Subject string `json:"subject"`
			} `json:"match"`
		} `json:"event"`
	} `json:"activations"`
	RunAs *struct {
		SubjectID           string `json:"subjectId"`
		CredentialSubjectID string `json:"credentialSubjectId"`
	} `json:"runAs"`
}

type workflowDefinitionHTTPListResponse struct {
	Definitions []workflowDefinitionHTTPResponse `json:"definitions"`
}

func TestGlobalWorkflowDefinitionInspectionAndPause(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	user := seedUser(t, services, "ada@example.test")
	provider := newMemoryWorkflowProvider()

	now := time.Now().UTC().Truncate(time.Second)
	provider.definitions["cfg_roadmap_sync"] = &coreworkflow.Definition{
		ID:                 "cfg_roadmap_sync",
		Generation:         7,
		Target:             workflowAppStepTarget("roadmap", "sync"),
		Paused:             false,
		CreatedBySubjectID: principal.UserSubjectID(user.ID),
		CreatedAt:          &now,
		UpdatedAt:          &now,
		RunAs: &core.RunAsSubject{
			SubjectID:           "service_account:workflow:roadmap",
			CredentialSubjectID: "service_account:credential:roadmap",
		},
		Activations: []coreworkflow.Activation{{
			ID: "nightly",
			Input: coreworkflow.Value{Object: map[string]coreworkflow.Value{
				"item": {Literal: "roadmap", LiteralSet: true},
			}},
			Schedule: &coreworkflow.ScheduleActivation{Cron: "0 3 * * *", Timezone: "America/New_York"},
		}, {
			ID:     "task_updated",
			Paused: true,
			Event: &coreworkflow.EventActivation{Match: coreworkflow.EventMatch{
				Type:    "task.updated",
				Source:  "roadmap",
				Subject: "workspace:planning",
			}},
		}},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "ada-session" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: user.Email, DisplayName: "Ada"}, nil
			},
		}
		cfg.Services = services
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "roadmap",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name:       "roadmap",
				Operations: []catalog.CatalogOperation{{ID: "sync", Method: http.MethodPost}},
			},
		})
		cfg.Workflow = &stubWorkflowControl{
			defaultProviderName: "basic",
			provider:            provider,
		}
	})
	testutil.CloseOnCleanup(t, ts)

	listReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/workflow/definitions/", nil)
	listReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatalf("list request: %v", err)
	}
	defer func() { _ = listResp.Body.Close() }()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("expected list 200, got %d", listResp.StatusCode)
	}
	var listed workflowDefinitionHTTPListResponse
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed.Definitions) != 1 || listed.Definitions[0].ID != "cfg_roadmap_sync" || listed.Definitions[0].Provider != "basic" {
		t.Fatalf("listed definitions = %#v", listed.Definitions)
	}

	getReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/workflow/definitions/cfg_roadmap_sync", nil)
	getReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatalf("get request: %v", err)
	}
	defer func() { _ = getResp.Body.Close() }()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("expected get 200, got %d", getResp.StatusCode)
	}
	var got workflowDefinitionHTTPResponse
	if err := json.NewDecoder(getResp.Body).Decode(&got); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if got.Generation != 7 || got.CreatedBySubjectID != principal.UserSubjectID(user.ID) {
		t.Fatalf("definition identity fields = %#v", got)
	}
	if got.RunAs == nil || got.RunAs.SubjectID != "service_account:workflow:roadmap" || got.RunAs.CredentialSubjectID != "service_account:credential:roadmap" {
		t.Fatalf("runAs = %#v", got.RunAs)
	}
	if len(got.Target.Steps) != 1 || got.Target.Steps[0].ID != "app" || got.Target.Steps[0].App.Name != "roadmap" || got.Target.Steps[0].App.Operation != "sync" {
		t.Fatalf("target = %#v", got.Target)
	}
	if len(got.Activations) != 2 || got.Activations[0].Schedule == nil || got.Activations[1].Event == nil {
		t.Fatalf("activations = %#v", got.Activations)
	}
	if got.Activations[0].Schedule.Cron != "0 3 * * *" || got.Activations[1].Event.Match.Type != "task.updated" {
		t.Fatalf("activation details = %#v", got.Activations)
	}

	pauseReq, _ := http.NewRequest(http.MethodPatch, ts.URL+"/api/v1/workflow/definitions/cfg_roadmap_sync", bytes.NewBufferString(`{"paused":true}`))
	pauseReq.Header.Set("Content-Type", "application/json")
	pauseReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	pauseResp, err := http.DefaultClient.Do(pauseReq)
	if err != nil {
		t.Fatalf("pause request: %v", err)
	}
	defer func() { _ = pauseResp.Body.Close() }()
	if pauseResp.StatusCode != http.StatusOK {
		t.Fatalf("expected pause 200, got %d", pauseResp.StatusCode)
	}
	if len(provider.definitionPauseReqs) != 1 || !provider.definitionPauseReqs[0].GetPaused() || provider.definitionPauseReqs[0].GetRequestedBySubjectId() != principal.UserSubjectID(user.ID) {
		t.Fatalf("definition pause requests = %#v", provider.definitionPauseReqs)
	}

	activationReq, _ := http.NewRequest(http.MethodPatch, ts.URL+"/api/v1/workflow/definitions/cfg_roadmap_sync/activations/task_updated", bytes.NewBufferString(`{"paused":false}`))
	activationReq.Header.Set("Content-Type", "application/json")
	activationReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	activationResp, err := http.DefaultClient.Do(activationReq)
	if err != nil {
		t.Fatalf("activation pause request: %v", err)
	}
	defer func() { _ = activationResp.Body.Close() }()
	if activationResp.StatusCode != http.StatusOK {
		t.Fatalf("expected activation pause 200, got %d", activationResp.StatusCode)
	}
	if len(provider.activationPauseReqs) != 1 || provider.activationPauseReqs[0].GetActivationId() != "task_updated" || provider.activationPauseReqs[0].GetPaused() {
		t.Fatalf("activation pause requests = %#v", provider.activationPauseReqs)
	}

	deleteReq, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/workflow/definitions/cfg_roadmap_sync", nil)
	deleteReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	deleteResp, err := http.DefaultClient.Do(deleteReq)
	if err != nil {
		t.Fatalf("delete request: %v", err)
	}
	defer func() { _ = deleteResp.Body.Close() }()
	if deleteResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected delete 204, got %d", deleteResp.StatusCode)
	}
	if len(provider.deletedDefinitions) != 1 || provider.deletedDefinitions[0] != "cfg_roadmap_sync" {
		t.Fatalf("deleted definitions = %#v", provider.deletedDefinitions)
	}
}

func TestGlobalWorkflowDefinitionPauseRequiresPaused(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	user := seedUser(t, services, "ada@example.test")
	provider := newMemoryWorkflowProvider()
	provider.definitions["cfg_roadmap_sync"] = &coreworkflow.Definition{
		ID:                 "cfg_roadmap_sync",
		Target:             workflowAppStepTarget("roadmap", "sync"),
		CreatedBySubjectID: principal.UserSubjectID(user.ID),
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "ada-session" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: user.Email, DisplayName: "Ada"}, nil
			},
		}
		cfg.Services = services
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "roadmap",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name:       "roadmap",
				Operations: []catalog.CatalogOperation{{ID: "sync", Method: http.MethodPost}},
			},
		})
		cfg.Workflow = &stubWorkflowControl{
			defaultProviderName: "basic",
			provider:            provider,
		}
	})
	testutil.CloseOnCleanup(t, ts)

	getReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/workflow/definitions/cfg_roadmap_sync", nil)
	getReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatalf("get request: %v", err)
	}
	defer func() { _ = getResp.Body.Close() }()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("expected get 200, got %d", getResp.StatusCode)
	}
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(getResp.Body).Decode(&raw); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if string(raw["activations"]) != "[]" {
		t.Fatalf("activations JSON = %s, want []", raw["activations"])
	}

	req, _ := http.NewRequest(http.MethodPatch, ts.URL+"/api/v1/workflow/definitions/cfg_roadmap_sync", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("pause request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestGlobalWorkflowDefinitionProviderNotFoundMapsToHTTPNotFound(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	user := seedUser(t, services, "ada@example.test")
	provider := newMemoryWorkflowProvider()
	provider.definitions["cfg_roadmap_sync"] = &coreworkflow.Definition{
		ID: "cfg_roadmap_sync",
		Target: coreworkflow.Target{Steps: []coreworkflow.Step{{
			ID: "sync",
			App: &coreworkflow.AppCall{
				Name:      "roadmap",
				Operation: "sync",
			},
		}}},
		CreatedBySubjectID: principal.UserSubjectID(user.ID),
		Activations: []coreworkflow.Activation{{
			ID:       "nightly",
			Schedule: &coreworkflow.ScheduleActivation{Cron: "0 3 * * *"},
		}},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "ada-session" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: user.Email, DisplayName: "Ada"}, nil
			},
		}
		cfg.Services = services
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "roadmap",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name:       "roadmap",
				Operations: []catalog.CatalogOperation{{ID: "sync", Method: http.MethodPost}},
			},
		})
		cfg.Workflow = &stubWorkflowControl{
			defaultProviderName: "basic",
			provider:            provider,
		}
	})
	testutil.CloseOnCleanup(t, ts)

	provider.activationPauseErr = status.Error(codes.NotFound, "activation not found")
	activationReq, _ := http.NewRequest(http.MethodPatch, ts.URL+"/api/v1/workflow/definitions/cfg_roadmap_sync/activations/nightly", bytes.NewBufferString(`{"paused":true}`))
	activationReq.Header.Set("Content-Type", "application/json")
	activationReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	activationResp, err := http.DefaultClient.Do(activationReq)
	if err != nil {
		t.Fatalf("activation pause request: %v", err)
	}
	defer func() { _ = activationResp.Body.Close() }()
	if activationResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected activation pause 404, got %d", activationResp.StatusCode)
	}

	provider.activationPauseErr = nil
	provider.deleteDefinitionErr = status.Error(codes.NotFound, "definition not found")
	deleteReq, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/workflow/definitions/cfg_roadmap_sync", nil)
	deleteReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	deleteResp, err := http.DefaultClient.Do(deleteReq)
	if err != nil {
		t.Fatalf("delete request: %v", err)
	}
	defer func() { _ = deleteResp.Body.Close() }()
	if deleteResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected delete 404, got %d", deleteResp.StatusCode)
	}
}
