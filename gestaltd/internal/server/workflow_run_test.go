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
		Plugin *struct {
			Name      string `json:"name"`
			Operation string `json:"operation"`
		} `json:"plugin"`
	} `json:"target"`
	Trigger struct {
		Kind       string `json:"kind"`
		ScheduleID string `json:"scheduleId"`
	} `json:"trigger"`
}

func workflowPluginTarget(pluginName, operation string) coreworkflow.Target {
	return workflowPluginTargetWithRouting(pluginName, operation, "", "")
}

func workflowPluginTargetWithRouting(pluginName, operation, connection, instance string) coreworkflow.Target {
	return coreworkflow.Target{
		Plugin: &coreworkflow.PluginTarget{
			PluginName: pluginName,
			Operation:  operation,
			Connection: connection,
			Instance:   instance,
		},
	}
}

func TestGlobalWorkflowRunInspectionIncludesHistoricalRevokedRefs(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	user := seedUser(t, services, "ada@example.test")
	other := seedUser(t, services, "grace@example.test")
	provider := newMemoryWorkflowProvider()

	now := time.Now().UTC().Truncate(time.Second)
	older := now.Add(-2 * time.Hour)
	revokedAt := now.Add(-1 * time.Hour)
	provider.runs["run-new"] = &coreworkflow.Run{
		ID:           "run-new",
		Status:       coreworkflow.RunStatusRunning,
		Target:       workflowPluginTarget("roadmap", "sync"),
		Trigger:      coreworkflow.RunTrigger{Schedule: &coreworkflow.ScheduleTrigger{ScheduleID: "sched-new"}},
		ExecutionRef: "workflow_schedule:sched-new:ref-active",
		CreatedAt:    &now,
	}
	provider.runs["run-old"] = &coreworkflow.Run{
		ID:            "run-old",
		Status:        coreworkflow.RunStatusSucceeded,
		Target:        workflowPluginTarget("roadmap", "sync"),
		Trigger:       coreworkflow.RunTrigger{Schedule: &coreworkflow.ScheduleTrigger{ScheduleID: "sched-old"}},
		ExecutionRef:  "workflow_schedule:sched-old:ref-revoked",
		CreatedAt:     &older,
		CompletedAt:   &now,
		StatusMessage: "done",
		ResultBody:    `{"ok":true}`,
	}
	provider.runs["run-other"] = &coreworkflow.Run{
		ID:           "run-other",
		Status:       coreworkflow.RunStatusSucceeded,
		Target:       workflowPluginTarget("roadmap", "sync"),
		ExecutionRef: "workflow_schedule:sched-other:ref-other",
		CreatedAt:    &now,
	}

	for _, ref := range []*coreworkflow.ExecutionReference{
		{
			ID:           "workflow_schedule:sched-new:ref-active",
			ProviderName: "basic",
			Target:       provider.runs["run-new"].Target,
			SubjectID:    principal.UserSubjectID(user.ID),
		},
		{
			ID:           "workflow_schedule:sched-old:ref-revoked",
			ProviderName: "basic",
			Target:       provider.runs["run-old"].Target,
			SubjectID:    principal.UserSubjectID(user.ID),
			RevokedAt:    &revokedAt,
		},
		{
			ID:           "workflow_schedule:sched-other:ref-other",
			ProviderName: "basic",
			Target:       provider.runs["run-other"].Target,
			SubjectID:    principal.UserSubjectID(other.ID),
		},
	} {
		if _, err := provider.PutExecutionReference(context.Background(), ref); err != nil {
			t.Fatalf("Put execution ref %q: %v", ref.ID, err)
		}
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

	listReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/workflow/runs/", nil)
	listReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatalf("list request: %v", err)
	}
	defer func() { _ = listResp.Body.Close() }()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", listResp.StatusCode)
	}
	var listed []workflowRunResponse
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("listed runs = %#v, want 2 items", listed)
	}
	if listed[0].ID != "run-new" || listed[1].ID != "run-old" {
		t.Fatalf("listed run order = %#v", listed)
	}
	if listed[1].Trigger.Kind != "schedule" || listed[1].Trigger.ScheduleID != "sched-old" {
		t.Fatalf("historical trigger = %#v", listed[1].Trigger)
	}

	getReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/workflow/runs/run-old", nil)
	getReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatalf("get request: %v", err)
	}
	defer func() { _ = getResp.Body.Close() }()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", getResp.StatusCode)
	}
	var got workflowRunResponse
	if err := json.NewDecoder(getResp.Body).Decode(&got); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if got.ID != "run-old" || got.Status != string(coreworkflow.RunStatusSucceeded) {
		t.Fatalf("run = %#v", got)
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
		Permissions:         []core.AccessPermission{{Plugin: "roadmap", Operations: []string{"sync"}}},
	}); err != nil {
		t.Fatalf("StoreAPIToken: %v", err)
	}

	provider := newMemoryWorkflowProvider()
	now := time.Now().UTC().Truncate(time.Second)
	provider.runs["run-sync"] = &coreworkflow.Run{
		ID:           "run-sync",
		Status:       coreworkflow.RunStatusSucceeded,
		Target:       workflowPluginTarget("roadmap", "sync"),
		ExecutionRef: "workflow_schedule:sched-sync:ref-sync",
		CreatedAt:    &now,
	}
	provider.runs["run-export"] = &coreworkflow.Run{
		ID:           "run-export",
		Status:       coreworkflow.RunStatusFailed,
		Target:       workflowPluginTarget("roadmap", "export"),
		ExecutionRef: "workflow_schedule:sched-export:ref-export",
		CreatedAt:    &now,
	}
	for _, ref := range []*coreworkflow.ExecutionReference{
		{
			ID:           "workflow_schedule:sched-sync:ref-sync",
			ProviderName: "basic",
			Target:       provider.runs["run-sync"].Target,
			SubjectID:    principal.UserSubjectID(user.ID),
		},
		{
			ID:           "workflow_schedule:sched-export:ref-export",
			ProviderName: "basic",
			Target:       provider.runs["run-export"].Target,
			SubjectID:    principal.UserSubjectID(user.ID),
		},
	} {
		if _, err := provider.PutExecutionReference(context.Background(), ref); err != nil {
			t.Fatalf("Put execution ref %q: %v", ref.ID, err)
		}
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
			ConnMode: core.ConnectionModeUser,
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
	var listed []workflowRunResponse
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
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
		ID:           "run-cancel",
		Status:       coreworkflow.RunStatusRunning,
		Target:       workflowPluginTarget("roadmap", "sync"),
		ExecutionRef: "workflow_schedule:sched-cancel:ref-active",
		CreatedAt:    &now,
		StartedAt:    &now,
	}
	provider.runs[run.ID] = run
	if _, err := provider.PutExecutionReference(context.Background(), &coreworkflow.ExecutionReference{
		ID:           run.ExecutionRef,
		ProviderName: "basic",
		Target:       run.Target,
		SubjectID:    principal.UserSubjectID(user.ID),
	}); err != nil {
		t.Fatalf("Put execution ref: %v", err)
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
