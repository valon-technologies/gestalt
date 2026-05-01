package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/registry"
	"github.com/valon-technologies/gestalt/server/services/agents/agentgrant"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
)

func TestAgentRuntimeWorkflowSystemToolCreatesScopedSchedule(t *testing.T) {
	t.Parallel()

	runtime, workflowProvider := newWorkflowSystemToolRuntime(t)
	workflowTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolSchedulesCreate)
	toolGrant := mustMintWorkflowSystemToolGrant(t, runtime, workflowSystemToolGrantScope{
		Permissions: []core.AccessPermission{{
			Plugin:     "roadmap",
			Operations: []string{"sync"},
		}},
		ToolRefs: []coreagent.ToolRef{
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolSchedulesCreate},
			{Plugin: "roadmap", Operation: "sync"},
		},
		Tools: []coreagent.Tool{workflowTool},
	})

	req := coreagent.ExecuteToolRequest{
		ProviderName: "managed",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolCallID:   "call-1",
		ToolID:       workflowTool.ID,
		ToolGrant:    toolGrant,
		Arguments: map[string]any{
			"cron":     "*/5 * * * *",
			"timezone": "UTC",
			"target": map[string]any{
				"plugin": map[string]any{
					"name":      "roadmap",
					"operation": "sync",
					"input": map[string]any{
						"source": "agent",
					},
				},
			},
		},
	}
	resp, err := runtime.ExecuteTool(context.Background(), req)
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	if resp == nil || resp.Status != http.StatusCreated {
		t.Fatalf("response = %#v, want 201", resp)
	}
	var body struct {
		Schedule struct {
			ID       string `json:"id"`
			Cron     string `json:"cron"`
			Timezone string `json:"timezone"`
			Target   struct {
				Plugin struct {
					Name      string         `json:"name"`
					Operation string         `json:"operation"`
					Input     map[string]any `json:"input"`
				} `json:"plugin"`
			} `json:"target"`
		} `json:"schedule"`
	}
	if err := json.Unmarshal([]byte(resp.Body), &body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	if body.Schedule.Cron != "*/5 * * * *" || body.Schedule.Timezone != "UTC" || body.Schedule.Target.Plugin.Name != "roadmap" || body.Schedule.Target.Plugin.Operation != "sync" {
		t.Fatalf("schedule response = %#v", body.Schedule)
	}
	secondResp, err := runtime.ExecuteTool(context.Background(), req)
	if err != nil {
		t.Fatalf("ExecuteTool replay: %v", err)
	}
	if secondResp == nil || secondResp.Status != http.StatusCreated {
		t.Fatalf("replay response = %#v, want 201", secondResp)
	}
	var secondBody struct {
		Schedule struct {
			ID string `json:"id"`
		} `json:"schedule"`
	}
	if err := json.Unmarshal([]byte(secondResp.Body), &secondBody); err != nil {
		t.Fatalf("decode replay response body: %v", err)
	}
	if secondBody.Schedule.ID != body.Schedule.ID {
		t.Fatalf("replayed schedule id = %q, want %q", secondBody.Schedule.ID, body.Schedule.ID)
	}
	conflictingReq := req
	conflictingReq.Arguments = maps.Clone(req.Arguments)
	conflictingReq.Arguments["cron"] = "*/10 * * * *"
	_, err = runtime.ExecuteTool(context.Background(), conflictingReq)
	if err == nil {
		t.Fatal("ExecuteTool conflicting replay succeeded, want invalid invocation")
	}
	if !errors.Is(err, invocation.ErrInvalidInvocation) {
		t.Fatalf("conflicting replay error = %v, want invalid invocation", err)
	}
	if len(workflowProvider.upsertedSchedules) != 1 {
		t.Fatalf("upserted schedules = %d, want 1", len(workflowProvider.upsertedSchedules))
	}
	upsert := workflowProvider.upsertedSchedules[0]
	if upsert.Target.Plugin == nil || upsert.Target.Plugin.PluginName != "roadmap" || upsert.Target.Plugin.Operation != "sync" {
		t.Fatalf("upsert target = %#v", upsert.Target)
	}
	ref, err := workflowProvider.GetExecutionReference(context.Background(), upsert.ExecutionRef)
	if err != nil {
		t.Fatalf("GetExecutionReference: %v", err)
	}
	if len(ref.Permissions) != 1 || ref.Permissions[0].Plugin != "roadmap" || len(ref.Permissions[0].Operations) != 1 || ref.Permissions[0].Operations[0] != "sync" {
		t.Fatalf("execution ref permissions = %#v", ref.Permissions)
	}
	if ref.CallerPluginName != "agent:managed" {
		t.Fatalf("execution ref caller = %q, want agent:managed", ref.CallerPluginName)
	}
}

func TestAgentRuntimeWorkflowSystemToolRejectsUndelegatedScheduleTarget(t *testing.T) {
	t.Parallel()

	runtime, _ := newWorkflowSystemToolRuntime(t)
	workflowTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolSchedulesCreate)
	toolGrant := mustMintWorkflowSystemToolGrant(t, runtime, workflowSystemToolGrantScope{
		Permissions: []core.AccessPermission{{
			Plugin:     "roadmap",
			Operations: []string{"sync"},
		}},
		ToolRefs: []coreagent.ToolRef{
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolSchedulesCreate},
		},
		Tools: []coreagent.Tool{workflowTool},
	})

	_, err := runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
		ProviderName: "managed",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolID:       workflowTool.ID,
		ToolGrant:    toolGrant,
		Arguments: map[string]any{
			"cron": "*/5 * * * *",
			"target": map[string]any{
				"plugin": map[string]any{
					"name":      "roadmap",
					"operation": "sync",
				},
			},
		},
	})
	if err == nil {
		t.Fatal("ExecuteTool succeeded, want scope denial")
	}
	if !errors.Is(err, invocation.ErrScopeDenied) {
		t.Fatalf("ExecuteTool error = %v, want scope denied", err)
	}
}

func TestAgentRuntimeWorkflowSystemToolRejectsUnsupportedScheduleTargetFields(t *testing.T) {
	t.Parallel()

	runtime, _ := newWorkflowSystemToolRuntime(t)
	workflowTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolSchedulesCreate)
	toolGrant := mustMintWorkflowSystemToolGrant(t, runtime, workflowSystemToolGrantScope{
		Permissions: []core.AccessPermission{{
			Plugin:     "roadmap",
			Operations: []string{"sync"},
		}},
		ToolRefs: []coreagent.ToolRef{
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolSchedulesCreate},
			{Plugin: "roadmap", Operation: "sync"},
		},
		Tools: []coreagent.Tool{workflowTool},
	})

	cases := []struct {
		name      string
		arguments map[string]any
	}{
		{
			name: "credential mode",
			arguments: map[string]any{
				"cron": "*/5 * * * *",
				"target": map[string]any{
					"agent": map[string]any{
						"provider": "managed",
						"prompt":   "Sync roadmap",
						"toolRefs": []any{
							map[string]any{
								"plugin":         "roadmap",
								"operation":      "sync",
								"credentialMode": "user",
							},
						},
					},
				},
			},
		},
		{
			name: "agent tools alias",
			arguments: map[string]any{
				"cron": "*/5 * * * *",
				"target": map[string]any{
					"agent": map[string]any{
						"provider": "managed",
						"prompt":   "Sync roadmap",
						"tools": []any{
							map[string]any{"plugin": "roadmap", "operation": "sync"},
						},
					},
				},
			},
		},
	}
	for _, tc := range cases {
		_, err := runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
			ProviderName: "managed",
			SessionID:    "session-1",
			TurnID:       "turn-1",
			ToolID:       workflowTool.ID,
			ToolGrant:    toolGrant,
			Arguments:    tc.arguments,
		})
		if err == nil {
			t.Fatalf("%s: ExecuteTool succeeded, want invalid invocation", tc.name)
		}
		if !errors.Is(err, invocation.ErrInvalidInvocation) {
			t.Fatalf("%s: ExecuteTool error = %v, want invalid invocation", tc.name, err)
		}
	}
}

func TestAgentRuntimeWorkflowSystemToolAllowsSystemOnlyTurn(t *testing.T) {
	t.Parallel()

	runtime, _ := newWorkflowSystemToolRuntime(t)
	workflowTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolSchedulesList)
	toolGrant := mustMintWorkflowSystemToolGrant(t, runtime, workflowSystemToolGrantScope{
		ToolRefs: []coreagent.ToolRef{{
			System:    coreagent.SystemToolWorkflow,
			Operation: workflowSystemToolSchedulesList,
		}},
		Tools: []coreagent.Tool{workflowTool},
	})

	resp, err := runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
		ProviderName: "managed",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolID:       workflowTool.ID,
		ToolGrant:    toolGrant,
	})
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	if resp == nil || resp.Status != http.StatusOK {
		t.Fatalf("response = %#v, want 200", resp)
	}
	var body struct {
		Schedules []any `json:"schedules"`
	}
	if err := json.Unmarshal([]byte(resp.Body), &body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	if len(body.Schedules) != 0 {
		t.Fatalf("schedules = %#v, want empty", body.Schedules)
	}
}

func newWorkflowSystemToolRuntime(t *testing.T) (*agentRuntime, *workflowSystemToolRecordingProvider) {
	t.Helper()

	reg := registry.New()
	if err := reg.Providers.Register("roadmap", &coretesting.StubIntegration{
		N:        "roadmap",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{
			Name: "roadmap",
			Operations: []catalog.CatalogOperation{{
				ID:     "sync",
				Method: http.MethodPost,
			}},
		},
	}); err != nil {
		t.Fatalf("Register roadmap: %v", err)
	}
	workflowProvider := &workflowSystemToolRecordingProvider{}
	workflowRuntime := &workflowRuntime{
		defaultProviderName: "temporal",
		providers: map[string]coreworkflow.Provider{
			"temporal": workflowProvider,
		},
	}
	runtime := &agentRuntime{
		defaultProviderName: "managed",
		providers: map[string]coreagent.Provider{
			"managed": &routingAgentProvider{
				getTurn: func(_ context.Context, req coreagent.GetTurnRequest) (*coreagent.Turn, error) {
					return &coreagent.Turn{
						ID:        req.TurnID,
						SessionID: "session-1",
						Status:    coreagent.ExecutionStatusRunning,
						CreatedBy: coreagent.Actor{
							SubjectID: req.Subject.SubjectID,
						},
					}, nil
				},
			},
		},
	}
	agentManager := workflowSystemToolAgentManagerStub{}
	workflowRuntime.SetAgentManager(agentManager)
	workflowManager := workflowmanager.New(workflowmanager.Config{
		Providers:    &reg.Providers,
		Workflow:     workflowRuntime,
		Agent:        runtime,
		AgentManager: agentManager,
	})
	runtime.SetToolGrants(newTestAgentToolGrants(t))
	runtime.SetToolSearcher(workflowSystemToolResolver{})
	runtime.SetSystemToolExecutor(newWorkflowSystemTools(workflowManager, workflowRuntime))
	return runtime, workflowProvider
}

type workflowSystemToolResolver struct{}

func (workflowSystemToolResolver) SearchTools(context.Context, *principal.Principal, coreagent.SearchToolsRequest) (*coreagent.SearchToolsResponse, error) {
	return &coreagent.SearchToolsResponse{}, nil
}

func (workflowSystemToolResolver) ListTools(context.Context, *principal.Principal, coreagent.ListToolsRequest) (*coreagent.ListToolsResponse, error) {
	return &coreagent.ListToolsResponse{}, nil
}

func (workflowSystemToolResolver) ResolveTool(_ context.Context, _ *principal.Principal, ref coreagent.ToolRef) (coreagent.Tool, error) {
	if ref.System != coreagent.SystemToolWorkflow {
		return coreagent.Tool{}, core.ErrNotFound
	}
	return workflowSystemToolFromRef(ref)
}

type workflowSystemToolAgentManagerStub struct {
	unavailableAgentManager
}

func (workflowSystemToolAgentManagerStub) Available() bool {
	return true
}

type workflowSystemToolRecordingProvider struct {
	upsertedSchedules []coreworkflow.UpsertScheduleRequest
	schedules         map[string]*coreworkflow.Schedule
	executionRefs     map[string]*coreworkflow.ExecutionReference
}

func (p *workflowSystemToolRecordingProvider) StartRun(context.Context, coreworkflow.StartRunRequest) (*coreworkflow.Run, error) {
	return &coreworkflow.Run{}, nil
}
func (p *workflowSystemToolRecordingProvider) GetRun(context.Context, coreworkflow.GetRunRequest) (*coreworkflow.Run, error) {
	return &coreworkflow.Run{}, nil
}
func (p *workflowSystemToolRecordingProvider) ListRuns(context.Context, coreworkflow.ListRunsRequest) ([]*coreworkflow.Run, error) {
	return nil, nil
}
func (p *workflowSystemToolRecordingProvider) CancelRun(context.Context, coreworkflow.CancelRunRequest) (*coreworkflow.Run, error) {
	return &coreworkflow.Run{}, nil
}
func (p *workflowSystemToolRecordingProvider) SignalRun(context.Context, coreworkflow.SignalRunRequest) (*coreworkflow.SignalRunResponse, error) {
	return &coreworkflow.SignalRunResponse{Run: &coreworkflow.Run{}}, nil
}
func (p *workflowSystemToolRecordingProvider) SignalOrStartRun(context.Context, coreworkflow.SignalOrStartRunRequest) (*coreworkflow.SignalRunResponse, error) {
	return &coreworkflow.SignalRunResponse{Run: &coreworkflow.Run{}}, nil
}
func (p *workflowSystemToolRecordingProvider) UpsertSchedule(_ context.Context, req coreworkflow.UpsertScheduleRequest) (*coreworkflow.Schedule, error) {
	p.upsertedSchedules = append(p.upsertedSchedules, req)
	schedule := &coreworkflow.Schedule{
		ID:           req.ScheduleID,
		Cron:         req.Cron,
		Timezone:     req.Timezone,
		Target:       req.Target,
		Paused:       req.Paused,
		ExecutionRef: req.ExecutionRef,
		CreatedBy:    req.RequestedBy,
	}
	if p.schedules == nil {
		p.schedules = map[string]*coreworkflow.Schedule{}
	}
	p.schedules[req.ScheduleID] = schedule
	return schedule, nil
}
func (p *workflowSystemToolRecordingProvider) GetSchedule(_ context.Context, req coreworkflow.GetScheduleRequest) (*coreworkflow.Schedule, error) {
	if schedule := p.schedules[req.ScheduleID]; schedule != nil {
		value := *schedule
		return &value, nil
	}
	return nil, core.ErrNotFound
}
func (p *workflowSystemToolRecordingProvider) ListSchedules(context.Context, coreworkflow.ListSchedulesRequest) ([]*coreworkflow.Schedule, error) {
	out := make([]*coreworkflow.Schedule, 0, len(p.schedules))
	for _, schedule := range p.schedules {
		value := *schedule
		out = append(out, &value)
	}
	return out, nil
}
func (p *workflowSystemToolRecordingProvider) DeleteSchedule(_ context.Context, req coreworkflow.DeleteScheduleRequest) error {
	delete(p.schedules, req.ScheduleID)
	return nil
}
func (p *workflowSystemToolRecordingProvider) PauseSchedule(context.Context, coreworkflow.PauseScheduleRequest) (*coreworkflow.Schedule, error) {
	return &coreworkflow.Schedule{}, nil
}
func (p *workflowSystemToolRecordingProvider) ResumeSchedule(context.Context, coreworkflow.ResumeScheduleRequest) (*coreworkflow.Schedule, error) {
	return &coreworkflow.Schedule{}, nil
}
func (p *workflowSystemToolRecordingProvider) UpsertEventTrigger(context.Context, coreworkflow.UpsertEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return &coreworkflow.EventTrigger{}, nil
}
func (p *workflowSystemToolRecordingProvider) GetEventTrigger(context.Context, coreworkflow.GetEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return nil, core.ErrNotFound
}
func (p *workflowSystemToolRecordingProvider) ListEventTriggers(context.Context, coreworkflow.ListEventTriggersRequest) ([]*coreworkflow.EventTrigger, error) {
	return nil, nil
}
func (p *workflowSystemToolRecordingProvider) DeleteEventTrigger(context.Context, coreworkflow.DeleteEventTriggerRequest) error {
	return nil
}
func (p *workflowSystemToolRecordingProvider) PauseEventTrigger(context.Context, coreworkflow.PauseEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return &coreworkflow.EventTrigger{}, nil
}
func (p *workflowSystemToolRecordingProvider) ResumeEventTrigger(context.Context, coreworkflow.ResumeEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return &coreworkflow.EventTrigger{}, nil
}
func (p *workflowSystemToolRecordingProvider) PublishEvent(context.Context, coreworkflow.PublishEventRequest) error {
	return nil
}
func (p *workflowSystemToolRecordingProvider) PutExecutionReference(_ context.Context, ref *coreworkflow.ExecutionReference) (*coreworkflow.ExecutionReference, error) {
	if p.executionRefs == nil {
		p.executionRefs = map[string]*coreworkflow.ExecutionReference{}
	}
	value := *ref
	p.executionRefs[value.ID] = &value
	return &value, nil
}
func (p *workflowSystemToolRecordingProvider) GetExecutionReference(_ context.Context, id string) (*coreworkflow.ExecutionReference, error) {
	if ref := p.executionRefs[id]; ref != nil {
		value := *ref
		return &value, nil
	}
	return nil, core.ErrNotFound
}
func (p *workflowSystemToolRecordingProvider) ListExecutionReferences(_ context.Context, subjectID string) ([]*coreworkflow.ExecutionReference, error) {
	out := make([]*coreworkflow.ExecutionReference, 0, len(p.executionRefs))
	for _, ref := range p.executionRefs {
		if ref.SubjectID != subjectID {
			continue
		}
		value := *ref
		out = append(out, &value)
	}
	return out, nil
}
func (p *workflowSystemToolRecordingProvider) Ping(context.Context) error { return nil }
func (p *workflowSystemToolRecordingProvider) Close() error               { return nil }

type workflowSystemToolGrantScope struct {
	Permissions []core.AccessPermission
	ToolRefs    []coreagent.ToolRef
	Tools       []coreagent.Tool
}

func mustMintWorkflowSystemToolGrant(t *testing.T, runtime *agentRuntime, scope workflowSystemToolGrantScope) string {
	t.Helper()

	grants := workflowSystemToolGrants(t, runtime)
	grant, err := grants.Mint(agentgrant.Grant{
		ProviderName:        "managed",
		SessionID:           "session-1",
		TurnID:              "turn-1",
		SubjectID:           principal.UserSubjectID("ada"),
		SubjectKind:         string(principal.KindUser),
		CredentialSubjectID: principal.UserSubjectID("ada"),
		Permissions:         append([]core.AccessPermission(nil), scope.Permissions...),
		ToolRefs:            append([]coreagent.ToolRef(nil), scope.ToolRefs...),
		Tools:               append([]coreagent.Tool(nil), scope.Tools...),
	})
	if err != nil {
		t.Fatalf("Mint workflow system tool grant: %v", err)
	}
	return grant
}

func workflowSystemToolGrants(t *testing.T, runtime *agentRuntime) *agentgrant.Manager {
	t.Helper()

	runtime.mu.RLock()
	grants := runtime.toolGrants
	runtime.mu.RUnlock()
	if grants == nil {
		t.Fatal("runtime tool grants are not configured")
	}
	return grants
}

func mustWorkflowSystemTool(t *testing.T, runtime *agentRuntime, operation string) coreagent.Tool {
	t.Helper()

	tool, err := workflowSystemToolFromRef(coreagent.ToolRef{
		System:    coreagent.SystemToolWorkflow,
		Operation: operation,
	})
	if err != nil {
		t.Fatalf("workflowSystemToolFromRef: %v", err)
	}
	tool.ID = mustMintAgentToolID(t, workflowSystemToolGrants(t, runtime), tool.Target)
	return tool
}
