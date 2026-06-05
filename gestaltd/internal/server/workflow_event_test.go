package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/access"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

type workflowEventDeliveryResponse struct {
	Status string `json:"status"`
	Event  struct {
		Source string `json:"source"`
		Type   string `json:"type"`
	} `json:"event"`
}

func TestWorkflowEventDeliveryUsesAuthorizedSourceAsCallerApp(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	plaintext, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	seedAPITokenWithPermissions(t, services, plaintext, hashed, "event-user", []core.AccessPermission{{App: "roadmap"}})
	provider := newMemoryWorkflowProvider()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(context.Context, string) (*core.UserIdentity, error) {
				return nil, core.ErrNotFound
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

	body := bytes.NewBufferString(`{"source":"roadmap","type":"roadmap.item.updated","subject":"item-1","data":{"id":"item-1"}}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/workflow/events", body)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("deliver request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}
	var got workflowEventDeliveryResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Event.Source != "roadmap" || got.Event.Type != "roadmap.item.updated" {
		t.Fatalf("delivered event response = %#v", got)
	}
	if len(provider.deliveredEvents) != 1 {
		t.Fatalf("delivered events = %#v, want 1", provider.deliveredEvents)
	}
	delivered := provider.deliveredEvents[0]
	if delivered.GetAppName() != "roadmap" || delivered.GetEvent().GetSource() != "roadmap" {
		t.Fatalf("delivered provider event = %#v", delivered)
	}
}

func TestWorkflowEventDeliveryRejectsUnauthorizedSource(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	plaintext, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	seedAPITokenWithPermissions(t, services, plaintext, hashed, "event-user", []core.AccessPermission{{App: "roadmap"}})
	provider := newMemoryWorkflowProvider()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(context.Context, string) (*core.UserIdentity, error) {
				return nil, core.ErrNotFound
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

	body := bytes.NewBufferString(`{"source":"github","type":"github.pull_request","subject":"repo:toolshed"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/workflow/events", body)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("deliver request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
	if len(provider.deliveredEvents) != 0 {
		t.Fatalf("delivered events = %#v, want none", provider.deliveredEvents)
	}
}

func TestWorkflowEventDeliveryPropagatesAuthorizationPolicyUnavailable(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	plaintext, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	seedAPITokenWithPermissions(t, services, plaintext, hashed, "event-user", []core.AccessPermission{{App: "github"}})
	provider := newMemoryWorkflowProvider()
	authzErr := errors.New("authorization backend unavailable")

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(context.Context, string) (*core.UserIdentity, error) {
				return nil, core.ErrNotFound
			},
		}
		cfg.Services = services
		cfg.Access = access.NewEnforcer(workflowEventAuthorizationProvider{err: authzErr})
		cfg.Workflow = &stubWorkflowControl{
			defaultProviderName: "basic",
			provider:            provider,
		}
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"source":"github","type":"github.pull_request","subject":"repo:toolshed"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/workflow/events", body)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("deliver request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}
	if len(provider.deliveredEvents) != 0 {
		t.Fatalf("delivered events = %#v, want none", provider.deliveredEvents)
	}
}

type workflowEventAuthorizationProvider struct {
	err error
	core.AuthorizationProvider
}

func (p workflowEventAuthorizationProvider) CheckAccess(context.Context, *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	if p.err != nil {
		return nil, p.err
	}
	return &proto.CheckAccessResponse{Allowed: true}, nil
}
