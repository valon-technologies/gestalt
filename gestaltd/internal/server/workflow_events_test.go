package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestWorkflowEventPublishFansOutAcrossWorkflowProviders(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
	user := seedUser(t, services, "ada@example.test")
	basicProvider := newMemoryWorkflowProvider()
	advancedProvider := newMemoryWorkflowProvider()
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
			ConnMode: core.ConnectionModeUser,
			CatalogVal: &catalog.Catalog{
				Name:       "roadmap",
				Operations: []catalog.CatalogOperation{{ID: "sync", Method: http.MethodPost}},
			},
		})
		cfg.Workflow = &stubWorkflowControl{
			defaultProviderName: "basic",
			providers: map[string]coreworkflow.Provider{
				"basic":    basicProvider,
				"advanced": advancedProvider,
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(
		http.MethodPost,
		ts.URL+"/api/v1/workflow/events",
		bytes.NewBufferString(`{"type":"roadmap.item.updated","source":"roadmap","subject":"item","data":{"id":"item-1"},"extensions":{"traceId":"trace-1"}}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("publish request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 202, got %d: %s", resp.StatusCode, body)
	}

	var body struct {
		Status string `json:"status"`
		Event  struct {
			ID          string         `json:"id"`
			Type        string         `json:"type"`
			Source      string         `json:"source"`
			Subject     string         `json:"subject"`
			SpecVersion string         `json:"specVersion"`
			Time        *time.Time     `json:"time"`
			Data        map[string]any `json:"data"`
			Extensions  map[string]any `json:"extensions"`
		} `json:"event"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode publish response: %v", err)
	}
	if body.Status != "published" {
		t.Fatalf("publish response status = %q, want published", body.Status)
	}
	if body.Event.ID == "" || body.Event.SpecVersion != "1.0" || body.Event.Time == nil {
		t.Fatalf("published event = %#v", body.Event)
	}
	if body.Event.Type != "roadmap.item.updated" || body.Event.Source != "roadmap" || body.Event.Subject != "item" {
		t.Fatalf("published event = %#v", body.Event)
	}
	if got := body.Event.Data["id"]; got != "item-1" {
		t.Fatalf("published event data = %#v", body.Event.Data)
	}
	if got := body.Event.Extensions["traceId"]; got != "trace-1" {
		t.Fatalf("published event extensions = %#v", body.Event.Extensions)
	}

	for name, provider := range map[string]*memoryWorkflowProvider{
		"basic":    basicProvider,
		"advanced": advancedProvider,
	} {
		if len(provider.publishEventReqs) != 1 {
			t.Fatalf("%s publish requests = %d, want 1", name, len(provider.publishEventReqs))
		}
		if got := provider.publishEventReqs[0].PluginName; got != "" {
			t.Fatalf("%s publish plugin = %q, want empty global publish", name, got)
		}
		publishReq := provider.publishEventReqs[0]
		if publishReq.Event.ID != body.Event.ID || publishReq.Event.SpecVersion != "1.0" || publishReq.Event.Time == nil || !publishReq.Event.Time.Equal(*body.Event.Time) {
			t.Fatalf("%s publish event = %#v, response = %#v", name, publishReq.Event, body.Event)
		}
		if publishReq.PublishedBy.SubjectID != principal.UserSubjectID(user.ID) ||
			publishReq.PublishedBy.SubjectKind != "user" ||
			publishReq.PublishedBy.DisplayName != "Ada" ||
			publishReq.PublishedBy.AuthSource != "session" {
			t.Fatalf("%s published_by = %#v, want publishing user actor", name, publishReq.PublishedBy)
		}
	}
}

func TestWorkflowEventPublishRequiresType(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
	user := seedUser(t, services, "ada@example.test")
	provider := newMemoryWorkflowProvider()
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
			ConnMode: core.ConnectionModeUser,
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

	req, _ := http.NewRequest(
		http.MethodPost,
		ts.URL+"/api/v1/workflow/events",
		bytes.NewBufferString(`{"source":"roadmap","data":{"id":"item-1"}}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("publish request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, body)
	}
	if len(provider.publishEventReqs) != 0 {
		t.Fatalf("publish event requests = %d, want 0", len(provider.publishEventReqs))
	}
}

func TestRemovedWorkflowObjectRoutesDoNotFallThroughToWorkflowPlugin(t *testing.T) {
	t.Parallel()

	var executed atomic.Bool
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "workflow",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name: "workflow",
				Operations: []catalog.CatalogOperation{
					{ID: "schedules", Method: http.MethodGet},
					{ID: "schedules", Method: http.MethodPost},
					{ID: "event-triggers", Method: http.MethodGet},
					{ID: "event-triggers", Method: http.MethodPost},
				},
			},
			ExecuteFn: func(context.Context, string, map[string]any, string) (*core.OperationResult, error) {
				executed.Store(true)
				return &core.OperationResult{Status: http.StatusOK, Body: `{"fellThrough":true}`}, nil
			},
		})
	})
	testutil.CloseOnCleanup(t, ts)

	cases := []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/v1/workflow/schedules"},
		{method: http.MethodPost, path: "/api/v1/workflow/schedules"},
		{method: http.MethodGet, path: "/api/v1/workflow/schedules/schedule-1"},
		{method: http.MethodPost, path: "/api/v1/workflow/schedules/schedule-1/pause"},
		{method: http.MethodGet, path: "/api/v1/workflow/event-triggers"},
		{method: http.MethodPost, path: "/api/v1/workflow/event-triggers"},
		{method: http.MethodGet, path: "/api/v1/workflow/event-triggers/trigger-1"},
		{method: http.MethodPost, path: "/api/v1/workflow/event-triggers/trigger-1/resume"},
	}
	for _, tc := range cases {
		req, _ := http.NewRequest(tc.method, ts.URL+tc.path, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", tc.method, tc.path, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusGone {
			t.Fatalf("%s %s status = %d, want %d", tc.method, tc.path, resp.StatusCode, http.StatusGone)
		}
	}
	if executed.Load() {
		t.Fatal("removed workflow object route fell through to workflow plugin operation")
	}
}
