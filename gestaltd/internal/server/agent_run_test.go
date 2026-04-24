package server_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/agentmanager"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
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
		return "", nil, agentmanager.ErrAgentProviderRequired
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
			return nil, agentmanager.NewAgentProviderNotAvailableError(name)
		}
		return provider, nil
	}
	if s.provider == nil {
		return nil, agentmanager.NewAgentProviderNotAvailableError(name)
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
	startRunErr      error
	getRunErr        error
	listRunsErr      error
	cancelRunErr     error
}

func newMemoryAgentProvider() *memoryAgentProvider {
	return &memoryAgentProvider{runs: map[string]*coreagent.Run{}}
}

func (p *memoryAgentProvider) StartRun(_ context.Context, req coreagent.StartRunRequest) (*coreagent.Run, error) {
	p.startRunRequests = append(p.startRunRequests, req)
	if p.startRunErr != nil {
		return nil, p.startRunErr
	}
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

type stubAgentToolInvoker struct {
	tokens map[string]string
}

func (s *stubAgentToolInvoker) Invoke(context.Context, *principal.Principal, string, string, string, map[string]any) (*core.OperationResult, error) {
	return nil, nil
}

func (s *stubAgentToolInvoker) ResolveToken(ctx context.Context, _ *principal.Principal, providerName, _, _ string) (context.Context, string, error) {
	if s != nil && s.tokens != nil {
		if token, ok := s.tokens[providerName]; ok {
			return ctx, token, nil
		}
	}
	return ctx, "", core.ErrNotFound
}

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

type agentRunEventResponse struct {
	ID         string         `json:"id"`
	RunID      string         `json:"runId"`
	Seq        int64          `json:"seq"`
	Type       string         `json:"type"`
	Source     string         `json:"source"`
	Visibility string         `json:"visibility"`
	Data       map[string]any `json:"data"`
}

func TestGlobalAgentRunLifecycleSupportsCreateListGetAndCancel(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
	user := seedUser(t, services, "ada@example.test")
	provider := newMemoryAgentProvider()
	roadmapProvider := &coretesting.StubIntegration{
		N:        "roadmap",
		DN:       "Roadmap",
		ConnMode: core.ConnectionModeUser,
		CatalogVal: &catalog.Catalog{
			Name: "roadmap",
			Operations: []catalog.CatalogOperation{
				{ID: "sync", Method: http.MethodGet, Path: "/sync"},
				{ID: "push", Method: http.MethodPost, Path: "/push"},
			},
		},
	}
	slackProvider := &coretesting.StubIntegration{
		N:        "slack",
		DN:       "Slack",
		ConnMode: core.ConnectionModeUser,
		CatalogVal: &catalog.Catalog{
			Name: "slack",
			Operations: []catalog.CatalogOperation{
				{ID: "users.profile.get", Method: http.MethodGet, Path: "/users.profile.get"},
			},
		},
	}
	manager := agentmanager.New(agentmanager.Config{
		Providers:   testutil.NewProviderRegistry(t, roadmapProvider, slackProvider),
		Agent:       &stubAgentControl{defaultProviderName: "managed", provider: provider},
		Invoker:     &stubAgentToolInvoker{tokens: map[string]string{"roadmap": "roadmap-token"}},
		RunMetadata: services.AgentRunMetadata,
		RunEvents:   services.AgentRunEvents,
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
	host := providerhost.NewAgentHostServer("managed", nil, func(ctx context.Context, req coreagent.EmitEventRequest) (*coreagent.RunEvent, error) {
		return services.AgentRunEvents.Append(ctx, coreagent.RunEvent{
			RunID:      req.RunID,
			Type:       req.Type,
			Source:     req.ProviderName,
			Visibility: req.Visibility,
			Data:       req.Data,
		})
	})

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
	if got := provider.startRunRequests[0].Tools; len(got) != 1 {
		t.Fatalf("StartRun tools = %#v, want one default safe tool", got)
	} else if got[0].Target.PluginName != "roadmap" || got[0].Target.Operation != "sync" {
		t.Fatalf("StartRun default tool = %#v, want roadmap.sync", got[0])
	}
	startedData, err := structpb.NewStruct(map[string]any{"status": "running"})
	if err != nil {
		t.Fatalf("started event data: %v", err)
	}
	if _, err := host.EmitEvent(context.Background(), &proto.EmitAgentEventRequest{
		RunId:      created.ID,
		Type:       "agent.run.started",
		Visibility: "public",
		Data:       startedData,
	}); err != nil {
		t.Fatalf("emit started event: %v", err)
	}
	if _, err := services.AgentRunEvents.Append(context.Background(), coreagent.RunEvent{
		RunID:      created.ID,
		Type:       "agent.message.delta",
		Source:     "managed",
		Visibility: "public",
		Data:       map[string]any{"text": "risk is dependency churn"},
	}); err != nil {
		t.Fatalf("append delta event: %v", err)
	}

	eventsReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/agent/runs/"+created.ID+"/events?after=0&limit=10", nil)
	eventsReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	eventsResp, err := http.DefaultClient.Do(eventsReq)
	if err != nil {
		t.Fatalf("events request: %v", err)
	}
	defer func() { _ = eventsResp.Body.Close() }()
	if eventsResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", eventsResp.StatusCode)
	}
	var events []agentRunEventResponse
	if err := json.NewDecoder(eventsResp.Body).Decode(&events); err != nil {
		t.Fatalf("decode events response: %v", err)
	}
	if len(events) != 2 || events[0].Seq != 1 || events[1].Seq != 2 {
		t.Fatalf("events = %#v", events)
	}
	if events[0].Source != "managed" || events[0].Data["status"] != "running" {
		t.Fatalf("started event = %#v", events[0])
	}
	if events[1].Type != "agent.message.delta" || events[1].Data["text"] != "risk is dependency churn" {
		t.Fatalf("delta event = %#v", events[1])
	}

	streamCtx, streamCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer streamCancel()
	streamReq, _ := http.NewRequestWithContext(streamCtx, http.MethodGet, ts.URL+"/api/v1/agent/runs/"+created.ID+"/events/stream?after=2&limit=1", nil)
	streamReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	streamResp, err := http.DefaultClient.Do(streamReq)
	if err != nil {
		t.Fatalf("stream request: %v", err)
	}
	defer func() { _ = streamResp.Body.Close() }()
	if streamResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", streamResp.StatusCode)
	}
	appendErr := make(chan error, 1)
	go func() {
		time.Sleep(100 * time.Millisecond)
		_, err := services.AgentRunEvents.Append(context.Background(), coreagent.RunEvent{
			RunID:      created.ID,
			Type:       "agent.run.completed",
			Source:     "managed",
			Visibility: "public",
			Data:       map[string]any{"status": "succeeded"},
		})
		appendErr <- err
	}()
	streamText := readSSEFrame(t, streamResp.Body)
	streamCancel()
	if err := <-appendErr; err != nil {
		t.Fatalf("append stream event: %v", err)
	}
	if !strings.Contains(streamText, "id: 3") || !strings.Contains(streamText, "event: agent.run.completed") || !strings.Contains(streamText, `"status":"succeeded"`) {
		t.Fatalf("stream body = %q", streamText)
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
	ref, err := services.AgentRunMetadata.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("metadata after cancel: %v", err)
	}
	if ref.RevokedAt == nil || ref.RevokedAt.IsZero() {
		t.Fatalf("metadata revoked_at = %#v, want set", ref.RevokedAt)
	}
	eventsAfterCancelReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/agent/runs/"+created.ID+"/events?after=0&limit=10", nil)
	eventsAfterCancelReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	eventsAfterCancelResp, err := http.DefaultClient.Do(eventsAfterCancelReq)
	if err != nil {
		t.Fatalf("events after cancel request: %v", err)
	}
	defer func() { _ = eventsAfterCancelResp.Body.Close() }()
	if eventsAfterCancelResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", eventsAfterCancelResp.StatusCode)
	}
	var eventsAfterCancel []agentRunEventResponse
	if err := json.NewDecoder(eventsAfterCancelResp.Body).Decode(&eventsAfterCancel); err != nil {
		t.Fatalf("decode events after cancel response: %v", err)
	}
	if len(eventsAfterCancel) != 3 || eventsAfterCancel[0].ID != events[0].ID {
		t.Fatalf("events after cancel = %#v", eventsAfterCancel)
	}
}

func readSSEFrame(t *testing.T, body io.Reader) string {
	t.Helper()
	reader := bufio.NewReader(body)
	var frame strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read SSE frame: %v", err)
		}
		frame.WriteString(line)
		if strings.TrimSpace(line) == "" {
			return frame.String()
		}
	}
}

func TestGlobalAgentRunProviderAvailabilityFailuresArePreconditionFailures(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
	user := seedUser(t, services, "ada@example.test")
	subjectID := principal.UserSubjectID(user.ID)
	if _, err := services.AgentRunMetadata.Put(context.Background(), &coreagent.ExecutionReference{
		ID:           "run-managed",
		ProviderName: "managed",
		SubjectID:    subjectID,
	}); err != nil {
		t.Fatalf("Put agent run metadata: %v", err)
	}
	if _, err := services.AgentRunEvents.Append(context.Background(), coreagent.RunEvent{
		RunID:      "run-managed",
		Type:       "agent.run.started",
		Source:     "managed",
		Visibility: "public",
		Data:       map[string]any{"status": "running"},
	}); err != nil {
		t.Fatalf("Append agent run event: %v", err)
	}
	manager := agentmanager.New(agentmanager.Config{
		Providers:   testutil.NewProviderRegistry(t),
		Agent:       &stubAgentControl{providers: map[string]coreagent.Provider{}},
		RunMetadata: services.AgentRunMetadata,
		RunEvents:   services.AgentRunEvents,
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

	errorCases := []struct {
		name      string
		method    string
		path      string
		body      string
		wantError string
	}{
		{
			name:      "create without selected provider",
			method:    http.MethodPost,
			path:      "/api/v1/agent/runs/",
			body:      `{"messages":[{"role":"user","text":"Summarize the rollout."}]}`,
			wantError: "agent provider is required",
		},
		{
			name:      "create with unavailable provider",
			method:    http.MethodPost,
			path:      "/api/v1/agent/runs/",
			body:      `{"provider":"managed","messages":[{"role":"user","text":"Summarize the rollout."}]}`,
			wantError: `agent provider "managed" is not available`,
		},
		{
			name:      "get stale run with unavailable provider",
			method:    http.MethodGet,
			path:      "/api/v1/agent/runs/run-managed",
			wantError: `agent provider "managed" is not available`,
		},
	}
	for _, tc := range errorCases {
		var body io.Reader
		if tc.body != "" {
			body = strings.NewReader(tc.body)
		}
		req, _ := http.NewRequest(tc.method, ts.URL+tc.path, body)
		if tc.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		req.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s request: %v", tc.name, err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusPreconditionFailed {
			t.Fatalf("%s status = %d, want %d", tc.name, resp.StatusCode, http.StatusPreconditionFailed)
		}
		var payload map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatalf("%s decode error response: %v", tc.name, err)
		}
		if payload["error"] != tc.wantError {
			t.Fatalf("%s error = %q, want %q", tc.name, payload["error"], tc.wantError)
		}
	}

	eventsReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/agent/runs/run-managed/events?after=0&limit=10", nil)
	eventsReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	eventsResp, err := http.DefaultClient.Do(eventsReq)
	if err != nil {
		t.Fatalf("events request: %v", err)
	}
	defer func() { _ = eventsResp.Body.Close() }()
	if eventsResp.StatusCode != http.StatusOK {
		t.Fatalf("events status = %d, want %d", eventsResp.StatusCode, http.StatusOK)
	}
	var events []agentRunEventResponse
	if err := json.NewDecoder(eventsResp.Body).Decode(&events); err != nil {
		t.Fatalf("decode events response: %v", err)
	}
	if len(events) != 1 || events[0].RunID != "run-managed" || events[0].Source != "managed" {
		t.Fatalf("events = %#v, want stored managed event", events)
	}
}

func TestGlobalAgentRunDefaultToolsUseAccessScopedSessionCatalog(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
	user := seedUser(t, services, "viewer-user@test.local")
	seedToken(t, services, &core.IntegrationToken{
		ID:          "tok-cat-human",
		SubjectID:   principal.UserSubjectID(user.ID),
		Integration: "test-int",
		Connection:  testCatalogConnection,
		Instance:    "default",
		AccessToken: testCatalogToken,
	})

	var seenAccess invocation.AccessContext
	provider := newMemoryAgentProvider()
	sessionProvider := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "test-int", ConnMode: core.ConnectionModeUser},
		},
		catalog: &catalog.Catalog{Name: "test-int"},
		catalogForRequestFn: func(ctx context.Context, token string) (*catalog.Catalog, error) {
			if token != testCatalogToken {
				return nil, errors.New("unexpected token")
			}
			seenAccess = invocation.AccessContextFromContext(ctx)
			switch seenAccess.Role {
			case "viewer":
				return &catalog.Catalog{
					Name: "test-int",
					Operations: []catalog.CatalogOperation{
						{ID: "viewer_session", Method: http.MethodGet, Path: "/viewer", AllowedRoles: []string{"viewer"}},
					},
				}, nil
			default:
				return &catalog.Catalog{
					Name: "test-int",
					Operations: []catalog.CatalogOperation{
						{ID: "public_session", Method: http.MethodGet, Path: "/public"},
					},
				}, nil
			}
		},
	}

	providers := testutil.NewProviderRegistry(t, sessionProvider)
	pluginDefs := map[string]*config.ProviderEntry{
		"test-int": {AuthorizationPolicy: "sample_policy"},
	}
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(user.ID), Role: "viewer"},
				},
			},
		},
	}, providers, pluginDefs, map[string]string{"test-int": testCatalogConnection})
	broker := invocation.NewBroker(providers, services.Users, services.Tokens, invocation.WithAuthorizer(authz))
	manager := agentmanager.New(agentmanager.Config{
		Providers:         providers,
		Agent:             &stubAgentControl{defaultProviderName: "managed", provider: provider},
		Invoker:           broker,
		Authorizer:        authz,
		CatalogConnection: map[string]string{"test-int": testCatalogConnection},
		RunMetadata:       services.AgentRunMetadata,
	})

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "viewer-session" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: user.Email, DisplayName: "Viewer"}, nil
			},
		}
		cfg.Services = services
		cfg.AgentManager = manager
		cfg.Authorizer = authz
		cfg.PluginDefs = pluginDefs
		cfg.CatalogConnection = map[string]string{"test-int": testCatalogConnection}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/runs/", bytes.NewBufferString(`{
		"messages":[{"role":"user","text":"Summarize the roadmap risk."}]
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "viewer-session"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	if len(provider.startRunRequests) != 1 {
		t.Fatalf("StartRun count = %d, want 1", len(provider.startRunRequests))
	}
	if got := provider.startRunRequests[0].Tools; len(got) != 1 {
		t.Fatalf("StartRun tools = %#v, want one access-scoped default tool", got)
	} else if got[0].Target.PluginName != "test-int" || got[0].Target.Operation != "viewer_session" {
		t.Fatalf("StartRun default tool = %#v, want test-int.viewer_session", got[0])
	}
	if seenAccess.Policy != "sample_policy" || seenAccess.Role != "viewer" {
		t.Fatalf("session catalog access context = %+v, want sample_policy/viewer", seenAccess)
	}
}

func TestGlobalAgentRunDefaultToolsSkipCredentialedProvidersWithoutStoredTokens(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
	user := seedUser(t, services, "viewer-user@test.local")
	seedToken(t, services, &core.IntegrationToken{
		ID:          "tok-cat-connected",
		SubjectID:   principal.UserSubjectID(user.ID),
		Integration: "connected",
		Connection:  testCatalogConnection,
		Instance:    "default",
		AccessToken: testCatalogToken,
	})
	now := time.Now().UTC()
	if err := services.DB.ObjectStore(coredata.StoreExternalCredentials).Put(context.Background(), indexeddb.Record{
		"id":                      "tok-corrupt-other-connection",
		"subject_id":              principal.UserSubjectID(user.ID),
		"integration":             "connected",
		"connection":              "other",
		"instance":                "default",
		"access_token_encrypted":  "not-valid-ciphertext",
		"refresh_token_encrypted": "not-valid-ciphertext",
		"created_at":              now,
		"updated_at":              now,
	}); err != nil {
		t.Fatalf("seed corrupt token: %v", err)
	}

	provider := newMemoryAgentProvider()
	connectedCatalogCalls := 0
	missingCatalogCalls := 0
	connectedProvider := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "connected", ConnMode: core.ConnectionModeUser},
		},
		catalog: &catalog.Catalog{Name: "connected"},
		catalogForRequestFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			connectedCatalogCalls++
			if token != testCatalogToken {
				return nil, errors.New("unexpected connected token")
			}
			return &catalog.Catalog{
				Name: "connected",
				Operations: []catalog.CatalogOperation{
					{ID: "read", Method: http.MethodGet, Path: "/read", ReadOnly: true},
				},
			}, nil
		},
	}
	missingProvider := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "missing", ConnMode: core.ConnectionModeUser},
		},
		catalog: &catalog.Catalog{Name: "missing"},
		catalogForRequestFn: func(_ context.Context, _ string) (*catalog.Catalog, error) {
			missingCatalogCalls++
			return &catalog.Catalog{
				Name: "missing",
				Operations: []catalog.CatalogOperation{
					{ID: "should_not_appear", Method: http.MethodGet, Path: "/missing", ReadOnly: true},
				},
			}, nil
		},
	}
	weatherProvider := &coretesting.StubIntegration{
		N:        "weather",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{
			Name: "weather",
			Operations: []catalog.CatalogOperation{
				{ID: "forecast", Method: http.MethodGet, Path: "/forecast", ReadOnly: true},
			},
		},
	}

	providers := testutil.NewProviderRegistry(t, connectedProvider, missingProvider, weatherProvider)
	broker := invocation.NewBroker(providers, services.Users, services.Tokens)
	manager := agentmanager.New(agentmanager.Config{
		Providers:         providers,
		Agent:             &stubAgentControl{defaultProviderName: "managed", provider: provider},
		Invoker:           broker,
		Tokens:            services.Tokens,
		CatalogConnection: map[string]string{"connected": testCatalogConnection, "missing": testCatalogConnection},
		RunMetadata:       services.AgentRunMetadata,
	})

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "viewer-session" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: user.Email, DisplayName: "Viewer"}, nil
			},
		}
		cfg.Services = services
		cfg.AgentManager = manager
		cfg.CatalogConnection = map[string]string{"connected": testCatalogConnection, "missing": testCatalogConnection}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/runs/", bytes.NewBufferString(`{
		"messages":[{"role":"user","text":"Summarize the roadmap risk."}]
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "viewer-session"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	if len(provider.startRunRequests) != 1 {
		t.Fatalf("StartRun count = %d, want 1", len(provider.startRunRequests))
	}

	gotTools := provider.startRunRequests[0].Tools
	gotTargets := make([]string, 0, len(gotTools))
	for _, tool := range gotTools {
		gotTargets = append(gotTargets, tool.Target.PluginName+"."+tool.Target.Operation)
	}
	slices.Sort(gotTargets)
	if !slices.Equal(gotTargets, []string{"connected.read", "weather.forecast"}) {
		t.Fatalf("StartRun tools = %#v, want connected.read and weather.forecast", gotTargets)
	}
	if connectedCatalogCalls == 0 {
		t.Fatal("connected catalog calls = 0, want at least one session catalog resolution")
	}
	if missingCatalogCalls != 0 {
		t.Fatalf("missing catalog calls = %d, want 0", missingCatalogCalls)
	}
}

func TestGlobalAgentRunDefaultToolsUseAPITokenCredentialSubject(t *testing.T) {
	t.Parallel()

	plaintext, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	services := coretesting.NewStubServices(t)
	owner := seedUser(t, services, "api-owner@test.local")
	credentialUser := seedUser(t, services, "credential-user@test.local")
	expiresAt := time.Now().UTC().Add(time.Hour)
	if err := services.APITokens.StoreAPIToken(context.Background(), &core.APIToken{
		ID:                  "api-token-bound-credential-subject",
		OwnerKind:           core.APITokenOwnerKindUser,
		OwnerID:             owner.ID,
		CredentialSubjectID: principal.UserSubjectID(credentialUser.ID),
		Name:                "bound-credential-subject",
		HashedToken:         hashed,
		ExpiresAt:           &expiresAt,
	}); err != nil {
		t.Fatalf("StoreAPIToken: %v", err)
	}

	seedToken(t, services, &core.IntegrationToken{
		ID:          "tok-cat-credential-subject",
		SubjectID:   principal.UserSubjectID(credentialUser.ID),
		Integration: "connected",
		Connection:  testCatalogConnection,
		Instance:    "default",
		AccessToken: testCatalogToken,
	})

	provider := newMemoryAgentProvider()
	connectedProvider := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "connected", ConnMode: core.ConnectionModeUser},
		},
		catalog: &catalog.Catalog{Name: "connected"},
		catalogForRequestFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			if token != testCatalogToken {
				return nil, errors.New("unexpected connected token")
			}
			return &catalog.Catalog{
				Name: "connected",
				Operations: []catalog.CatalogOperation{
					{ID: "read", Method: http.MethodGet, Path: "/read", ReadOnly: true},
				},
			}, nil
		},
	}

	providers := testutil.NewProviderRegistry(t, connectedProvider)
	broker := invocation.NewBroker(providers, services.Users, services.Tokens)
	manager := agentmanager.New(agentmanager.Config{
		Providers:         providers,
		Agent:             &stubAgentControl{defaultProviderName: "managed", provider: provider},
		Invoker:           broker,
		Tokens:            services.Tokens,
		CatalogConnection: map[string]string{"connected": testCatalogConnection},
		RunMetadata:       services.AgentRunMetadata,
	})

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, _ string) (*core.UserIdentity, error) {
				return nil, core.ErrNotFound
			},
		}
		cfg.Services = services
		cfg.AgentManager = manager
		cfg.CatalogConnection = map[string]string{"connected": testCatalogConnection}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/runs/", bytes.NewBufferString(`{
		"messages":[{"role":"user","text":"Summarize the roadmap risk."}]
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+plaintext)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	if len(provider.startRunRequests) != 1 {
		t.Fatalf("StartRun count = %d, want 1", len(provider.startRunRequests))
	}
	if got := provider.startRunRequests[0].Tools; len(got) != 1 {
		t.Fatalf("StartRun tools = %#v, want one default tool from credential subject", got)
	} else if got[0].Target.PluginName != "connected" || got[0].Target.Operation != "read" {
		t.Fatalf("StartRun default tool = %#v, want connected.read", got[0])
	}
}

func TestGlobalAgentRunCreateExplicitToolSourceSkipsDefaultTools(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
	user := seedUser(t, services, "ada@example.test")
	provider := newMemoryAgentProvider()
	roadmapProvider := &coretesting.StubIntegration{
		N:        "roadmap",
		DN:       "Roadmap",
		ConnMode: core.ConnectionModeUser,
		CatalogVal: &catalog.Catalog{
			Name: "roadmap",
			Operations: []catalog.CatalogOperation{
				{ID: "sync", Method: http.MethodGet, Path: "/sync"},
			},
		},
	}
	manager := agentmanager.New(agentmanager.Config{
		Providers:   testutil.NewProviderRegistry(t, roadmapProvider),
		Agent:       &stubAgentControl{defaultProviderName: "managed", provider: provider},
		Invoker:     &stubAgentToolInvoker{tokens: map[string]string{"roadmap": "roadmap-token"}},
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

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/runs/", bytes.NewBufferString(`{
		"messages":[{"role":"user","text":"Summarize the roadmap risk."}],
		"toolSource":"explicit"
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	if len(provider.startRunRequests) != 1 {
		t.Fatalf("StartRun count = %d, want 1", len(provider.startRunRequests))
	}
	if got := provider.startRunRequests[0].Tools; len(got) != 0 {
		t.Fatalf("StartRun tools = %#v, want none in explicit mode", got)
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

func TestGlobalAgentRunCreateMapsProviderInvalidArgumentToBadRequest(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
	user := seedUser(t, services, "ada@example.test")
	provider := newMemoryAgentProvider()
	provider.startRunErr = status.Error(codes.InvalidArgument, "messages must contain at least one entry")
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

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/runs/", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	var payload map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if payload["error"] != "messages must contain at least one entry" {
		t.Fatalf("error = %q, want %q", payload["error"], "messages must contain at least one entry")
	}
	if len(provider.startRunRequests) != 1 {
		t.Fatalf("StartRun count = %d, want 1", len(provider.startRunRequests))
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
