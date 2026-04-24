package bootstrap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	corecrypto "github.com/valon-technologies/gestalt/server/core/crypto"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/pluginruntime"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func buildAgentProviderBinary(t *testing.T) string {
	t.Helper()
	if sharedAgentProviderBin == "" {
		t.Fatal("shared agent provider binary not initialized")
	}
	return sharedAgentProviderBin
}

type agentRuntimeFactoryContextKey struct{}

type agentRuntimeInvokerCall struct {
	providerName string
	operation    string
	params       map[string]any
	subjectID    string
}

type recordingAgentRuntimeInvoker struct {
	mu    sync.Mutex
	calls []agentRuntimeInvokerCall
}

func (i *recordingAgentRuntimeInvoker) Invoke(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error) {
	i.mu.Lock()
	i.calls = append(i.calls, agentRuntimeInvokerCall{
		providerName: providerName,
		operation:    operation,
		params:       cloneAnyMap(params),
		subjectID:    p.SubjectID,
	})
	i.mu.Unlock()

	body, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	return &core.OperationResult{Status: http.StatusAccepted, Body: string(body)}, nil
}

func (i *recordingAgentRuntimeInvoker) Calls() []agentRuntimeInvokerCall {
	i.mu.Lock()
	defer i.mu.Unlock()
	out := make([]agentRuntimeInvokerCall, len(i.calls))
	copy(out, i.calls)
	return out
}

func cloneAnyMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	out := make(map[string]any, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}

func TestAgentRuntimeConfigSelectedProviderStartsSessionWithRuntimeFields(t *testing.T) {
	t.Parallel()

	bin := buildAgentProviderBinary(t)
	runtimeProvider := newCapturingPluginRuntime()
	ctxSentinel := &struct{}{}
	var factoryContextValue any

	factories := NewFactoryRegistry()
	factories.Runtime = func(ctx context.Context, _ string, _ *config.RuntimeProviderEntry, _ Deps) (pluginruntime.Provider, error) {
		factoryContextValue = ctx.Value(agentRuntimeFactoryContextKey{})
		return runtimeProvider, nil
	}

	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{Provider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {Driver: config.RuntimeProviderDriver("capture")},
			},
		},
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				"simple": {
					Command: bin,
					Runtime: &config.HostedRuntimeConfig{
						Template: "python-dev",
						Image:    "ghcr.io/valon/gestalt-python-runtime:latest",
						Metadata: map[string]string{"tenant": "eng"},
					},
				},
			},
		},
	}

	deps := Deps{
		AgentRuntime:          &agentRuntime{providers: map[string]coreagent.Provider{}},
		PluginRuntimeRegistry: newPluginRuntimeRegistry(cfg, factories.Runtime, Deps{}),
	}
	buildCtx := context.WithValue(context.Background(), agentRuntimeFactoryContextKey{}, ctxSentinel)
	agents, err := buildAgents(buildCtx, cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildAgents: %v", err)
	}
	defer func() {
		if err := closeAgents(agents...); err != nil {
			t.Fatalf("closeAgents: %v", err)
		}
	}()

	requests := runtimeProvider.startSessionRequests()
	if len(requests) != 1 {
		t.Fatalf("start session requests = %d, want 1", len(requests))
	}
	req := requests[0]
	if req.PluginName != "simple" {
		t.Fatalf("StartSession PluginName = %q, want simple", req.PluginName)
	}
	if req.Template != "python-dev" {
		t.Fatalf("StartSession Template = %q, want python-dev", req.Template)
	}
	if req.Image != "ghcr.io/valon/gestalt-python-runtime:latest" {
		t.Fatalf("StartSession Image = %q", req.Image)
	}
	if req.Metadata["tenant"] != "eng" {
		t.Fatalf("StartSession Metadata[tenant] = %q, want eng", req.Metadata["tenant"])
	}
	if req.Metadata["provider_kind"] != "agent" {
		t.Fatalf("StartSession Metadata[provider_kind] = %q, want agent", req.Metadata["provider_kind"])
	}
	if req.Metadata["provider_name"] != "simple" {
		t.Fatalf("StartSession Metadata[provider_name] = %q, want simple", req.Metadata["provider_name"])
	}
	if factoryContextValue != ctxSentinel {
		t.Fatalf("runtime factory context value = %#v, want %#v", factoryContextValue, ctxSentinel)
	}
}

func TestAgentRuntimeConfigUsesDirectAgentHostBinding(t *testing.T) {
	t.Parallel()

	bin := buildAgentProviderBinary(t)
	secret := []byte("0123456789abcdef0123456789abcdef")

	encryptor, err := corecrypto.NewAESGCM(secret)
	if err != nil {
		t.Fatalf("corecrypto.NewAESGCM: %v", err)
	}
	services, err := coredata.New(&coretesting.StubIndexedDB{}, encryptor)
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	invoker := &recordingAgentRuntimeInvoker{}
	agentRuntime := &agentRuntime{providers: map[string]coreagent.Provider{}}
	agentRuntime.SetInvoker(invoker)
	agentRuntime.SetRunMetadata(services.AgentRunMetadata)
	capturingRuntime := newCapturingPluginRuntime()

	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return capturingRuntime, nil
	}

	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{Provider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {Driver: config.RuntimeProviderDriver("capture")},
			},
		},
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				"simple": {
					Command: bin,
					Runtime: &config.HostedRuntimeConfig{},
				},
			},
		},
	}

	deps := Deps{
		Services:     services,
		AgentRuntime: agentRuntime,
	}
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)

	agents, err := buildAgents(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildAgents: %v", err)
	}
	defer func() {
		if err := closeAgents(agents...); err != nil {
			t.Fatalf("closeAgents: %v", err)
		}
	}()
	if len(agents) != 1 {
		t.Fatalf("agents len = %d, want 1", len(agents))
	}
	capabilities, err := agents[0].GetCapabilities(context.Background(), coreagent.GetCapabilitiesRequest{})
	if err != nil {
		t.Fatalf("GetCapabilities: %v", err)
	}
	if capabilities == nil || !capabilities.Interactions || !capabilities.ResumableTurns {
		t.Fatalf("capabilities = %#v, want interactions+resumable_turns", capabilities)
	}

	session, err := agents[0].CreateSession(context.Background(), coreagent.CreateSessionRequest{
		SessionID:      "session-1",
		IdempotencyKey: "session-req-1",
		Model:          "gpt-test",
		ClientRef:      "cli-session-1",
		Metadata: map[string]any{
			"source": "agent-runtime-test",
		},
		CreatedBy: coreagent.Actor{SubjectID: "user:user-123"},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session == nil || session.ID != "session-1" || session.ProviderName != "simple" || session.State != coreagent.SessionStateActive {
		t.Fatalf("CreateSession = %#v, want active simple session-1", session)
	}

	updatedSession, err := agents[0].UpdateSession(context.Background(), coreagent.UpdateSessionRequest{
		SessionID: "session-1",
		ClientRef: "cli-session-2",
		State:     coreagent.SessionStateArchived,
		Metadata: map[string]any{
			"source": "agent-runtime-test-updated",
		},
	})
	if err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}
	if updatedSession == nil || updatedSession.ClientRef != "cli-session-2" || updatedSession.State != coreagent.SessionStateArchived {
		t.Fatalf("UpdateSession = %#v, want archived cli-session-2", updatedSession)
	}

	sessions, err := agents[0].ListSessions(context.Background(), coreagent.ListSessionsRequest{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != "session-1" {
		t.Fatalf("ListSessions = %#v, want session-1", sessions)
	}

	fetchedSession, err := agents[0].GetSession(context.Background(), coreagent.GetSessionRequest{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if fetchedSession == nil || fetchedSession.Metadata["source"] != "agent-runtime-test-updated" {
		t.Fatalf("GetSession = %#v, want updated source metadata", fetchedSession)
	}

	turn, err := agents[0].CreateTurn(context.Background(), coreagent.CreateTurnRequest{
		TurnID:       "turn-1",
		SessionID:    "session-1",
		Model:        "gpt-test",
		Messages:     []coreagent.Message{{Role: "user", Text: "Plan it"}},
		ExecutionRef: "exec-turn-1",
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if turn == nil || turn.ID != "turn-1" || turn.SessionID != "session-1" || turn.ProviderName != "simple" {
		t.Fatalf("CreateTurn = %#v, want simple turn-1 on session-1", turn)
	}

	turns, err := agents[0].ListTurns(context.Background(), coreagent.ListTurnsRequest{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("ListTurns: %v", err)
	}
	if len(turns) != 1 || turns[0].ID != "turn-1" {
		t.Fatalf("ListTurns = %#v, want turn-1", turns)
	}

	fetchedTurn, err := agents[0].GetTurn(context.Background(), coreagent.GetTurnRequest{TurnID: "turn-1"})
	if err != nil {
		t.Fatalf("GetTurn: %v", err)
	}
	if fetchedTurn == nil || fetchedTurn.Status != coreagent.ExecutionStatusSucceeded || fetchedTurn.OutputText == "" {
		t.Fatalf("GetTurn = %#v, want succeeded turn with output", fetchedTurn)
	}

	turnEvents, err := agents[0].ListTurnEvents(context.Background(), coreagent.ListTurnEventsRequest{
		TurnID:   "turn-1",
		AfterSeq: 0,
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("ListTurnEvents: %v", err)
	}
	if len(turnEvents) != 3 || turnEvents[0].Type != "turn.started" || turnEvents[2].Type != "turn.completed" {
		t.Fatalf("ListTurnEvents = %#v, want started/completed event sequence", turnEvents)
	}

	postTurnSession, err := agents[0].GetSession(context.Background(), coreagent.GetSessionRequest{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("GetSession(after CreateTurn): %v", err)
	}
	if postTurnSession == nil || postTurnSession.ClientRef != "cli-session-2" {
		t.Fatalf("GetSession(after CreateTurn) = %#v, want preserved client_ref cli-session-2", postTurnSession)
	}

	toolTurn, err := agents[0].CreateTurn(context.Background(), coreagent.CreateTurnRequest{
		TurnID:       "turn-1",
		SessionID:    "session-1",
		Model:        "gpt-test",
		CreatedBy:    coreagent.Actor{SubjectID: "user:user-123"},
		ExecutionRef: "exec-turn-1",
		Messages:     []coreagent.Message{{Role: "user", Text: "Plan it"}},
		Tools: []coreagent.Tool{{
			ID:   "lookup",
			Name: "Lookup roadmap task",
			Target: coreagent.ToolTarget{
				PluginName: "roadmap",
				Operation:  "sync",
			},
		}},
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if toolTurn == nil {
		t.Fatal("CreateTurn returned nil turn")
	}

	var output struct {
		ProviderName string `json:"provider_name"`
		ToolStatus   int    `json:"tool_status"`
		ToolBody     string `json:"tool_body"`
		EventEmitted bool   `json:"event_emitted"`
		HostError    string `json:"host_error"`
		ToolError    string `json:"tool_error"`
		EventError   string `json:"event_error"`
	}
	if err := json.Unmarshal([]byte(toolTurn.OutputText), &output); err != nil {
		t.Fatalf("json.Unmarshal(toolTurn.OutputText): %v", err)
	}
	if output.ProviderName != "simple" {
		t.Fatalf("provider_name = %q, want simple", output.ProviderName)
	}
	if output.ToolStatus != http.StatusAccepted {
		t.Fatalf("tool_status = %d, want %d (output=%s)", output.ToolStatus, http.StatusAccepted, toolTurn.OutputText)
	}
	if output.ToolBody != `{"taskId":"task-123"}` {
		t.Fatalf("tool_body = %q, want %q", output.ToolBody, `{"taskId":"task-123"}`)
	}
	if !output.EventEmitted {
		t.Fatal("event_emitted = false, want true")
	}
	if output.HostError != "" || output.ToolError != "" || output.EventError != "" {
		t.Fatalf("runtime callback errors = %+v", output)
	}

	calls := invoker.Calls()
	if len(calls) != 1 {
		t.Fatalf("invoker calls = %d, want 1", len(calls))
	}
	if calls[0].providerName != "roadmap" || calls[0].operation != "sync" {
		t.Fatalf("invoker call = %+v", calls[0])
	}
	if calls[0].params["taskId"] != "task-123" {
		t.Fatalf("invoker params = %#v, want taskId=task-123", calls[0].params)
	}
	if calls[0].subjectID != "user:user-123" {
		t.Fatalf("invoker subject_id = %q, want user:user-123", calls[0].subjectID)
	}

	events, err := agents[0].ListTurnEvents(context.Background(), coreagent.ListTurnEventsRequest{
		TurnID:   "turn-1",
		AfterSeq: 0,
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("ListTurnEvents: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("ListTurnEvents returned no events")
	}
	foundAgentTest := false
	for _, event := range events {
		if event.Type != "agent.test" {
			continue
		}
		foundAgentTest = true
		if event.Data["provider_name"] != "simple" {
			t.Fatalf("agent.test event data = %#v, want provider_name=simple", event.Data)
		}
	}
	if !foundAgentTest {
		t.Fatalf("events = %#v, want agent.test event", events)
	}

	bindRequests := capturingRuntime.bindHostServiceRequests()
	if len(bindRequests) != 1 {
		t.Fatalf("bind host service requests = %d, want 1", len(bindRequests))
	}
	if bindRequests[0].EnvVar != providerhost.DefaultAgentHostSocketEnv {
		t.Fatalf("BindHostService EnvVar = %q, want %q", bindRequests[0].EnvVar, providerhost.DefaultAgentHostSocketEnv)
	}
	if got := bindRequests[0].Relay.DialTarget; !strings.HasPrefix(got, "unix://") {
		t.Fatalf("BindHostService relay target = %q, want unix relay target", got)
	}

	pausedTurn, err := agents[0].CreateTurn(context.Background(), coreagent.CreateTurnRequest{
		TurnID:    "turn-2",
		SessionID: "session-1",
		Model:     "gpt-test",
		CreatedBy: coreagent.Actor{SubjectID: "user:user-123"},
		Metadata: map[string]any{
			"requireInteraction": true,
		},
	})
	if err != nil {
		t.Fatalf("CreateTurn(waiting): %v", err)
	}
	if pausedTurn == nil {
		t.Fatal("CreateTurn(waiting) returned nil turn")
	}
	if pausedTurn.Status != coreagent.ExecutionStatusWaitingForInput {
		t.Fatalf("paused turn status = %q, want %q", pausedTurn.Status, coreagent.ExecutionStatusWaitingForInput)
	}
	var pausedOutput struct {
		InteractionRequested bool   `json:"interaction_requested"`
		InteractionID        string `json:"interaction_id"`
		InteractionError     string `json:"interaction_error"`
	}
	if err := json.Unmarshal([]byte(pausedTurn.OutputText), &pausedOutput); err != nil {
		t.Fatalf("json.Unmarshal(pausedTurn.OutputText): %v", err)
	}
	if !pausedOutput.InteractionRequested || strings.TrimSpace(pausedOutput.InteractionID) == "" || pausedOutput.InteractionError != "" {
		t.Fatalf("paused turn output = %+v", pausedOutput)
	}
	interactions, err := agents[0].ListInteractions(context.Background(), coreagent.ListInteractionsRequest{TurnID: "turn-2"})
	if err != nil {
		t.Fatalf("ListInteractions: %v", err)
	}
	if len(interactions) != 1 {
		t.Fatalf("interactions = %d, want 1", len(interactions))
	}
	if interactions[0].Type != coreagent.InteractionTypeApproval || interactions[0].State != coreagent.InteractionStatePending {
		t.Fatalf("interaction = %#v, want pending approval", interactions[0])
	}
	resolvedInteraction, err := agents[0].ResolveInteraction(context.Background(), coreagent.ResolveInteractionRequest{
		InteractionID: interactions[0].ID,
		Resolution: map[string]any{
			"approved": true,
		},
	})
	if err != nil {
		t.Fatalf("ResolveInteraction: %v", err)
	}
	if resolvedInteraction == nil || resolvedInteraction.State != coreagent.InteractionStateResolved || resolvedInteraction.Resolution["approved"] != true {
		t.Fatalf("resolved interaction = %#v, want resolved approved interaction", resolvedInteraction)
	}
	resolvedTurn, err := agents[0].GetTurn(context.Background(), coreagent.GetTurnRequest{TurnID: "turn-2"})
	if err != nil {
		t.Fatalf("GetTurn(after ResolveInteraction): %v", err)
	}
	if resolvedTurn == nil || resolvedTurn.Status != coreagent.ExecutionStatusSucceeded || resolvedTurn.StatusMessage != interactions[0].ID {
		t.Fatalf("GetTurn(after ResolveInteraction) = %#v, want succeeded turn status_message=%q", resolvedTurn, interactions[0].ID)
	}
}

//nolint:paralleltest // Hosted public-relay startup is serialized to avoid Linux CI contention.
func TestAgentRuntimeConfigUsesPublicAgentHostRelayBinding(t *testing.T) {
	// This exercises the hosted agent startup path over the public relay and is
	// sensitive to Linux CI contention when it runs alongside the other hosted
	// runtime bootstrap tests.

	bin := buildAgentProviderBinary(t)
	secret := []byte("0123456789abcdef0123456789abcdef")
	relaySrv := httptest.NewUnstartedServer(newRuntimeRelayTestHandler(t, secret))
	relaySrv.EnableHTTP2 = true
	relaySrv.StartTLS()
	testutil.CloseOnCleanup(t, relaySrv)

	runtimeProvider := newCapturingBundlePluginRuntime()
	runtimeProvider.support.HostServiceAccess = pluginruntime.HostServiceAccessNone
	runtimeProvider.support.LaunchMode = pluginruntime.LaunchModeHostPath

	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}

	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{Provider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {Driver: config.RuntimeProviderDriver("capture")},
			},
		},
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				"simple": {
					Command: bin,
					Runtime: &config.HostedRuntimeConfig{},
				},
			},
		},
	}

	services := coretesting.NewStubServices(t)
	runtimeState := &agentRuntime{providers: map[string]coreagent.Provider{}}
	runtimeState.SetRunMetadata(services.AgentRunMetadata)
	deps := Deps{
		BaseURL:       relaySrv.URL,
		EncryptionKey: secret,
		AgentRuntime:  runtimeState,
	}
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)

	agents, err := buildAgents(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildAgents: %v", err)
	}
	defer func() {
		if err := closeAgents(agents...); err != nil {
			t.Fatalf("closeAgents: %v", err)
		}
	}()

	if _, err := agents[0].CreateSession(context.Background(), coreagent.CreateSessionRequest{
		SessionID: "session-1",
		Model:     "gpt-test",
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	turn, err := agents[0].CreateTurn(context.Background(), coreagent.CreateTurnRequest{
		TurnID:    "turn-1",
		SessionID: "session-1",
		Model:     "gpt-test",
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if turn == nil || turn.OutputText != `{"provider_name":"simple"}` {
		t.Fatalf("turn = %#v, want provider-only output", turn)
	}

	bindRequests := runtimeProvider.bindHostServiceRequests()
	if len(bindRequests) != 1 {
		t.Fatalf("bind host service requests = %d, want 1", len(bindRequests))
	}
	if bindRequests[0].EnvVar != providerhost.DefaultAgentHostSocketEnv {
		t.Fatalf("BindHostService EnvVar = %q, want %q", bindRequests[0].EnvVar, providerhost.DefaultAgentHostSocketEnv)
	}
	if got := bindRequests[0].Relay.DialTarget; got != "tls://"+relaySrv.Listener.Addr().String() {
		t.Fatalf("BindHostService relay target = %q, want tls relay target", got)
	}

	startRequests := runtimeProvider.startPluginRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("start plugin requests = %d, want 1", len(startRequests))
	}
	if got := startRequests[0].Env[providerhost.DefaultAgentHostSocketEnv+"_TOKEN"]; strings.TrimSpace(got) == "" {
		t.Fatalf("StartPlugin env missing %s_TOKEN: %#v", providerhost.DefaultAgentHostSocketEnv, startRequests[0].Env)
	}
}

func TestAgentRuntimeConfigRejectsMissingHostServiceAccess(t *testing.T) {
	t.Parallel()

	bin := buildAgentProviderBinary(t)
	runtimeProvider := &staticCapabilityPluginRuntime{
		inner: newCapturingPluginRuntime(),
		support: pluginruntime.Support{
			CanHostPlugins: true,
			LaunchMode:     pluginruntime.LaunchModeHostPath,
		},
	}

	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}

	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{Provider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {Driver: config.RuntimeProviderDriver("capture")},
			},
		},
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				"simple": {
					Command: bin,
					Runtime: &config.HostedRuntimeConfig{},
				},
			},
		},
	}

	deps := Deps{
		AgentRuntime: &agentRuntime{providers: map[string]coreagent.Provider{}},
	}
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)

	_, err := buildAgents(context.Background(), cfg, factories, deps)
	if err == nil {
		t.Fatal("buildAgents error = nil, want host service access failure")
	}
	if got := err.Error(); got != `bootstrap: agent from resource "simple": agent provider: runtime provider "hosted" cannot provide host service access required by this provider` {
		t.Fatalf("buildAgents error = %q", got)
	}
}
