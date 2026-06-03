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
)

type workflowRunResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Target struct {
		Steps []struct {
			App *struct {
				Name      string `json:"name"`
				Operation string `json:"operation"`
			} `json:"app"`
		} `json:"steps"`
	} `json:"target"`
	Trigger struct {
		Kind       string `json:"kind"`
		ScheduleID string `json:"scheduleId"`
	} `json:"trigger"`
}

type workflowRunListResponse struct {
	Runs          []workflowRunResponse `json:"runs"`
	NextPageToken string                `json:"nextPageToken,omitempty"`
}

func workflowAppStepTarget(appName, operation string) coreworkflow.Target {
	return workflowAppStepTargetWithRouting(appName, operation, "", "")
}

func workflowAppStepTargetWithRouting(appName, operation, connection, instance string) coreworkflow.Target {
	return coreworkflow.Target{
		Steps: []coreworkflow.Step{{
			ID: "app",
			App: &coreworkflow.AppCall{
				Name:       appName,
				Operation:  operation,
				Connection: connection,
				Instance:   instance,
			},
		}},
	}
}

func TestGlobalWorkflowRunListPassesPaginationAndFilters(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	user := seedUser(t, services, "ada@example.test")
	provider := newMemoryWorkflowProvider()
	provider.listRunsNextPage = "next-run-page"

	now := time.Now().UTC().Truncate(time.Second)
	provider.runs["run-page"] = &coreworkflow.Run{
		ID:                 "run-page",
		Status:             coreworkflow.RunStatusRunning,
		Target:             workflowAppStepTarget("roadmap", "sync"),
		CreatedBySubjectID: principal.UserSubjectID(user.ID),
		CreatedAt:          &now,
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

	listReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/workflow/runs/?pageSize=17&app=roadmap&status=running", nil)
	listReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatalf("list request: %v", err)
	}
	defer func() { _ = listResp.Body.Close() }()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", listResp.StatusCode)
	}
	var listed workflowRunListResponse
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if listed.NextPageToken == "" || len(listed.Runs) != 1 || listed.Runs[0].ID != "run-page" {
		t.Fatalf("list response = %#v", listed)
	}
	if len(provider.listRunReqs) != 1 {
		t.Fatalf("provider list requests = %#v, want 1", provider.listRunReqs)
	}
	got := provider.listRunReqs[0]
	if got.PageSize != 17 || got.PageToken != "" || got.TargetApp != "roadmap" || got.Status != coreworkflow.RunStatusRunning {
		t.Fatalf("provider list request = %#v", got)
	}

	nextReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/workflow/runs/?pageSize=17&pageToken="+listed.NextPageToken+"&app=roadmap&status=running", nil)
	nextReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	nextResp, err := http.DefaultClient.Do(nextReq)
	if err != nil {
		t.Fatalf("next list request: %v", err)
	}
	defer func() { _ = nextResp.Body.Close() }()
	if nextResp.StatusCode != http.StatusOK {
		t.Fatalf("expected next 200, got %d", nextResp.StatusCode)
	}
	if len(provider.listRunReqs) != 2 {
		t.Fatalf("provider list requests = %#v, want 2", provider.listRunReqs)
	}
	got = provider.listRunReqs[1]
	if got.PageSize != 17 || got.PageToken != "next-run-page" || got.TargetApp != "roadmap" || got.Status != coreworkflow.RunStatusRunning {
		t.Fatalf("next provider list request = %#v", got)
	}
}

func TestGlobalWorkflowRunInspectionAPITokenScopeFiltersOperations(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	user := seedUser(t, services, "ada@example.test")
	plaintext, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	expiresAt := time.Now().Add(24 * time.Hour)
	if err := services.APITokens.StoreAPIToken(context.Background(), &core.APIToken{
		ID:                  "workflow-runs-token",
		OwnerKind:           core.APITokenOwnerKindUser,
		OwnerID:             user.ID,
		CredentialSubjectID: principal.UserSubjectID(user.ID),
		Name:                "workflow-runs-token",
		HashedToken:         hashed,
		ExpiresAt:           &expiresAt,
		Permissions:         []core.AccessPermission{{App: "roadmap", Operations: []string{"sync"}}},
	}); err != nil {
		t.Fatalf("StoreAPIToken: %v", err)
	}

	provider := newMemoryWorkflowProvider()
	now := time.Now().UTC().Truncate(time.Second)
	provider.runs["run-sync"] = &coreworkflow.Run{
		ID:                 "run-sync",
		Status:             coreworkflow.RunStatusSucceeded,
		Target:             workflowAppStepTarget("roadmap", "sync"),
		CreatedBySubjectID: principal.UserSubjectID(user.ID),
		CreatedAt:          &now,
	}
	provider.runs["run-export"] = &coreworkflow.Run{
		ID:                 "run-export",
		Status:             coreworkflow.RunStatusFailed,
		Target:             workflowAppStepTarget("roadmap", "export"),
		CreatedBySubjectID: principal.UserSubjectID(user.ID),
		CreatedAt:          &now,
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, _ string) (*core.UserIdentity, error) {
				return nil, core.ErrNotFound
			},
		}
		cfg.Services = services
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "roadmap",
			ConnMode: core.ConnectionModeSubject,
			CatalogVal: &catalog.Catalog{
				Name: "roadmap",
				Operations: []catalog.CatalogOperation{
					{ID: "sync", Method: http.MethodPost},
					{ID: "export", Method: http.MethodPost},
				},
			},
		})
		cfg.Workflow = &stubWorkflowControl{
			defaultProviderName: "basic",
			provider:            provider,
		}
	})
	testutil.CloseOnCleanup(t, ts)

	listReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/workflow/runs/", nil)
	listReq.Header.Set("Authorization", "Bearer "+plaintext)
	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatalf("list request: %v", err)
	}
	defer func() { _ = listResp.Body.Close() }()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", listResp.StatusCode)
	}
	var listedResp workflowRunListResponse
	if err := json.NewDecoder(listResp.Body).Decode(&listedResp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	listed := listedResp.Runs
	if len(listed) != 1 || listed[0].ID != "run-sync" {
		t.Fatalf("listed runs = %#v", listed)
	}

	getReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/workflow/runs/run-export", nil)
	getReq.Header.Set("Authorization", "Bearer "+plaintext)
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatalf("get request: %v", err)
	}
	defer func() { _ = getResp.Body.Close() }()
	if getResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", getResp.StatusCode)
	}
}

func TestGlobalWorkflowRunCancelUpdatesOwnedRun(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	user := seedUser(t, services, "ada@example.test")
	provider := newMemoryWorkflowProvider()

	now := time.Now().UTC().Truncate(time.Second)
	run := &coreworkflow.Run{
		ID:                 "run-cancel",
		Status:             coreworkflow.RunStatusRunning,
		Target:             workflowAppStepTarget("roadmap", "sync"),
		CreatedBySubjectID: principal.UserSubjectID(user.ID),
		CreatedAt:          &now,
		StartedAt:          &now,
	}
	provider.runs[run.ID] = run

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
				Name: "roadmap",
				Operations: []catalog.CatalogOperation{
					{ID: "sync", Method: http.MethodPost},
				},
			},
		})
		cfg.Workflow = &stubWorkflowControl{
			defaultProviderName: "basic",
			provider:            provider,
		}
	})
	testutil.CloseOnCleanup(t, ts)

	cancelBody := bytes.NewBufferString(`{"reason":"operator requested"}`)
	cancelReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/workflow/runs/run-cancel/cancel", cancelBody)
	cancelReq.Header.Set("Content-Type", "application/json")
	cancelReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	cancelResp, err := http.DefaultClient.Do(cancelReq)
	if err != nil {
		t.Fatalf("cancel request: %v", err)
	}
	defer func() { _ = cancelResp.Body.Close() }()
	if cancelResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", cancelResp.StatusCode)
	}
	var got workflowRunResponse
	if err := json.NewDecoder(cancelResp.Body).Decode(&got); err != nil {
		t.Fatalf("decode cancel response: %v", err)
	}
	if got.ID != "run-cancel" || got.Status != string(coreworkflow.RunStatusCanceled) {
		t.Fatalf("canceled run = %#v", got)
	}
	if len(provider.cancelReqs) != 1 || provider.cancelReqs[0].Reason != "operator requested" {
		t.Fatalf("cancel requests = %#v", provider.cancelReqs)
	}
}
