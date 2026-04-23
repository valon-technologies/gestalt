package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/agentmanager"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

type stubAgentControl struct {
	defaultProviderName string
	provider            coreagent.Provider
	providers           map[string]coreagent.Provider
	selectionErr        error
	providerErr         error
}

func (s *stubAgentControl) ResolveProviderSelection(name string) (string, coreagent.Provider, error) {
	if s.selectionErr != nil {
		return "", nil, s.selectionErr
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = s.defaultProviderName
	}
	if name == "" {
		return "", nil, errors.New("provider not found")
	}
	provider, err := s.ResolveProvider(name)
	if err != nil {
		return "", nil, err
	}
	return name, provider, nil
}

func (s *stubAgentControl) ResolveProvider(name string) (coreagent.Provider, error) {
	if s.providerErr != nil {
		return nil, s.providerErr
	}
	if s.providers != nil {
		provider, ok := s.providers[name]
		if !ok {
			return nil, errors.New("provider not found")
		}
		return provider, nil
	}
	return s.provider, nil
}

func (s *stubAgentControl) ProviderNames() []string {
	if s.providers != nil {
		names := make([]string, 0, len(s.providers))
		for name := range s.providers {
			names = append(names, name)
		}
		slices.Sort(names)
		return names
	}
	if strings.TrimSpace(s.defaultProviderName) != "" {
		return []string{strings.TrimSpace(s.defaultProviderName)}
	}
	if s.provider != nil {
		return []string{"default"}
	}
	return nil
}

type memoryAgentProvider struct {
	startRunRequests []coreagent.StartRunRequest
	cancelRequests   []coreagent.CancelRunRequest
	runs             map[string]*coreagent.Run
	getRunErr        error
	listRunsErr      error
	cancelRunErr     error
}

func newMemoryAgentProvider() *memoryAgentProvider {
	return &memoryAgentProvider{runs: map[string]*coreagent.Run{}}
}

func (p *memoryAgentProvider) StartRun(_ context.Context, req coreagent.StartRunRequest) (*coreagent.Run, error) {
	p.startRunRequests = append(p.startRunRequests, req)
	now := time.Now().UTC().Truncate(time.Second)
	run := &coreagent.Run{
		ID:           req.RunID,
		ProviderName: req.ProviderName,
		Model:        req.Model,
		Status:       coreagent.RunStatusRunning,
		Messages:     append([]coreagent.Message(nil), req.Messages...),
		SessionRef:   req.SessionRef,
		CreatedBy:    req.CreatedBy,
		CreatedAt:    &now,
		StartedAt:    &now,
		ExecutionRef: req.ExecutionRef,
	}
	p.runs[req.RunID] = run
	return cloneAgentRun(run), nil
}

func (p *memoryAgentProvider) GetRun(_ context.Context, req coreagent.GetRunRequest) (*coreagent.Run, error) {
	if p.getRunErr != nil {
		return nil, p.getRunErr
	}
	run, ok := p.runs[req.RunID]
	if !ok || run == nil {
		return nil, core.ErrNotFound
	}
	return cloneAgentRun(run), nil
}

func (p *memoryAgentProvider) ListRuns(_ context.Context, _ coreagent.ListRunsRequest) ([]*coreagent.Run, error) {
	if p.listRunsErr != nil {
		return nil, p.listRunsErr
	}
	out := make([]*coreagent.Run, 0, len(p.runs))
	for _, run := range p.runs {
		if run != nil {
			out = append(out, cloneAgentRun(run))
		}
	}
	return out, nil
}

func (p *memoryAgentProvider) CancelRun(_ context.Context, req coreagent.CancelRunRequest) (*coreagent.Run, error) {
	if p.cancelRunErr != nil {
		return nil, p.cancelRunErr
	}
	run, ok := p.runs[req.RunID]
	if !ok || run == nil {
		return nil, core.ErrNotFound
	}
	now := time.Now().UTC().Truncate(time.Second)
	run.Status = coreagent.RunStatusCanceled
	run.CompletedAt = &now
	run.StatusMessage = strings.TrimSpace(req.Reason)
	p.cancelRequests = append(p.cancelRequests, req)
	return cloneAgentRun(run), nil
}

func (p *memoryAgentProvider) Ping(context.Context) error { return nil }
func (p *memoryAgentProvider) Close() error               { return nil }

func cloneAgentRun(run *coreagent.Run) *coreagent.Run {
	if run == nil {
		return nil
	}
	cloned := *run
	cloned.Messages = append([]coreagent.Message(nil), run.Messages...)
	cloned.StructuredOutput = maps.Clone(run.StructuredOutput)
	return &cloned
}

type agentRunMessageResponse struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

type agentRunResponse struct {
	ID           string                    `json:"id"`
	Provider     string                    `json:"provider"`
	Model        string                    `json:"model"`
	Status       string                    `json:"status"`
	Messages     []agentRunMessageResponse `json:"messages"`
	ExecutionRef string                    `json:"executionRef"`
}

func TestGlobalAgentRunLifecycleSupportsCreateListGetAndCancel(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
	user := seedUser(t, services, "ada@example.test")
	provider := newMemoryAgentProvider()
	manager := agentmanager.New(agentmanager.Config{
		Providers:   testutil.NewProviderRegistry(t),
		Agent:       &stubAgentControl{defaultProviderName: "managed", provider: provider},
		RunMetadata: services.AgentRunMetadata,
	})

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
		cfg.AgentManager = manager
	})
	testutil.CloseOnCleanup(t, ts)

	createBody := []byte(`{
		"provider":"managed",
		"model":"gpt-5.4",
		"messages":[
			{"role":"system","text":"Be concise."},
			{"role":"user","text":"Summarize the roadmap risk."}
		]
	}`)
	createReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/runs/", bytes.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Idempotency-Key", "agent-http-create-1")
	createReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	defer func() { _ = createResp.Body.Close() }()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", createResp.StatusCode)
	}
	var created agentRunResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.Provider != "managed" || created.Model != "gpt-5.4" || created.Status != string(coreagent.RunStatusRunning) {
		t.Fatalf("created run = %#v", created)
	}
	if len(created.Messages) != 2 || created.Messages[1].Text != "Summarize the roadmap risk." {
		t.Fatalf("created messages = %#v", created.Messages)
	}
	if created.ExecutionRef != created.ID {
		t.Fatalf("execution ref = %q, want %q", created.ExecutionRef, created.ID)
	}
	if len(provider.startRunRequests) != 1 {
		t.Fatalf("StartRun count = %d, want 1", len(provider.startRunRequests))
	}
	if got := provider.startRunRequests[0].IdempotencyKey; got != "agent-http-create-1" {
		t.Fatalf("StartRun idempotency key = %q, want %q", got, "agent-http-create-1")
	}

	listReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/agent/runs/?provider=managed&status=running", nil)
	listReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatalf("list request: %v", err)
	}
	defer func() { _ = listResp.Body.Close() }()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", listResp.StatusCode)
	}
	var listed []agentRunResponse
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("listed runs = %#v, want %q", listed, created.ID)
	}

	getReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/agent/runs/"+created.ID, nil)
	getReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatalf("get request: %v", err)
	}
	defer func() { _ = getResp.Body.Close() }()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", getResp.StatusCode)
	}
	var fetched agentRunResponse
	if err := json.NewDecoder(getResp.Body).Decode(&fetched); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if fetched.ID != created.ID || fetched.Status != string(coreagent.RunStatusRunning) {
		t.Fatalf("fetched run = %#v", fetched)
	}

	cancelReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/runs/"+created.ID+"/cancel", bytes.NewBufferString(`{"reason":"stop now"}`))
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
	var canceled agentRunResponse
	if err := json.NewDecoder(cancelResp.Body).Decode(&canceled); err != nil {
		t.Fatalf("decode cancel response: %v", err)
	}
	if canceled.Status != string(coreagent.RunStatusCanceled) {
		t.Fatalf("canceled run = %#v", canceled)
	}
	if len(provider.cancelRequests) != 1 || provider.cancelRequests[0].Reason != "stop now" {
		t.Fatalf("cancel requests = %#v", provider.cancelRequests)
	}
}

func TestGlobalAgentRunListProviderFilterAvoidsUnhealthyProviders(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
	user := seedUser(t, services, "ada@example.test")
	managedProvider := newMemoryAgentProvider()
	brokenProvider := newMemoryAgentProvider()
	brokenProvider.listRunsErr = errors.New("provider is unavailable")

	now := time.Now().UTC().Truncate(time.Second)
	managedProvider.runs["run-managed"] = &coreagent.Run{
		ID:           "run-managed",
		ProviderName: "managed",
		Model:        "gpt-5.4",
		Status:       coreagent.RunStatusRunning,
		CreatedAt:    &now,
		ExecutionRef: "run-managed",
	}
	brokenProvider.runs["run-broken"] = &coreagent.Run{
		ID:           "run-broken",
		ProviderName: "broken",
		Model:        "claude",
		Status:       coreagent.RunStatusFailed,
		CreatedAt:    &now,
		ExecutionRef: "run-broken",
	}

	subjectID := principal.UserSubjectID(user.ID)
	for _, ref := range []*coreagent.ExecutionReference{
		{
			ID:           "run-managed",
			ProviderName: "managed",
			SubjectID:    subjectID,
		},
		{
			ID:           "run-broken",
			ProviderName: "broken",
			SubjectID:    subjectID,
		},
	} {
		if _, err := services.AgentRunMetadata.Put(context.Background(), ref); err != nil {
			t.Fatalf("Put agent run metadata %q: %v", ref.ID, err)
		}
	}

	manager := agentmanager.New(agentmanager.Config{
		Providers: testutil.NewProviderRegistry(t),
		Agent: &stubAgentControl{
			defaultProviderName: "managed",
			providers: map[string]coreagent.Provider{
				"managed": managedProvider,
				"broken":  brokenProvider,
			},
		},
		RunMetadata: services.AgentRunMetadata,
	})

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
		cfg.AgentManager = manager
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/agent/runs/?provider=managed", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var listed []agentRunResponse
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != "run-managed" {
		t.Fatalf("listed runs = %#v, want only run-managed", listed)
	}
}

func TestGlobalAgentRunCreateRejectsMismatchedIdempotencyKeySources(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
	user := seedUser(t, services, "ada@example.test")
	provider := newMemoryAgentProvider()
	manager := agentmanager.New(agentmanager.Config{
		Providers:   testutil.NewProviderRegistry(t),
		Agent:       &stubAgentControl{defaultProviderName: "managed", provider: provider},
		RunMetadata: services.AgentRunMetadata,
	})

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
		cfg.AgentManager = manager
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/runs/", bytes.NewBufferString(`{"idempotencyKey":"body-key"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "header-key")
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	if len(provider.startRunRequests) != 0 {
		t.Fatalf("unexpected StartRun requests = %#v", provider.startRunRequests)
	}
}

func TestGlobalAgentRunCreateReturnsConflictWhileIdempotentRunIsInitializing(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
	user := seedUser(t, services, "ada@example.test")
	provider := newMemoryAgentProvider()
	manager := agentmanager.New(agentmanager.Config{
		Providers:   testutil.NewProviderRegistry(t),
		Agent:       &stubAgentControl{defaultProviderName: "managed", provider: provider},
		RunMetadata: services.AgentRunMetadata,
	})

	subjectID := principal.UserSubjectID(user.ID)
	claimedRunID, claimed, err := services.AgentRunMetadata.ClaimIdempotency(context.Background(), subjectID, "managed", "pending-key", "pending-run", time.Now())
	if err != nil {
		t.Fatalf("ClaimIdempotency: %v", err)
	}
	if !claimed || claimedRunID != "pending-run" {
		t.Fatalf("ClaimIdempotency = (%q, %t), want (%q, true)", claimedRunID, claimed, "pending-run")
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
		cfg.AgentManager = manager
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/runs/", bytes.NewBufferString(`{"messages":[{"role":"user","text":"Continue"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "pending-key")
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
	if len(provider.startRunRequests) != 0 {
		t.Fatalf("unexpected StartRun requests = %#v", provider.startRunRequests)
	}
}
