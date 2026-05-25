package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/services/agents/agentgrant"
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
)

func TestAgentRuntimeWorkflowSystemToolCreatesScopedSchedule(t *testing.T) {
	t.Parallel()

	runtime, workflowProvider := newWorkflowSystemToolRuntime(t)
	workflowTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolSchedulesCreate)
	runGrant := mustMintWorkflowSystemRunGrant(t, runtime, workflowSystemRunGrantScope{
		Permissions: []core.AccessPermission{{
			App:        "roadmap",
			Operations: []string{"sync"},
		}},
		ToolRefs: []coreagent.ToolRef{
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolSchedulesCreate},
			{App: "roadmap", Operation: "sync"},
		},
		Tools: []coreagent.Tool{workflowTool},
	})

	req := coreagent.ExecuteToolRequest{
		ProviderName: "managed",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolCallID:   "call-1",
		ToolID:       workflowTool.ID,
		RunGrant:     runGrant,
		Arguments: map[string]any{
			"cron":     "*/5 * * * *",
			"timezone": "UTC",
			"target": workflowSystemToolTestTarget(map[string]any{
				"id": "sync",
				"app": map[string]any{
					"name":      "roadmap",
					"operation": "sync",
					"input": map[string]any{
						"source": "agent",
					},
				},
			}),
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
				Steps []struct {
					App struct {
						Name      string `json:"name"`
						Operation string `json:"operation"`
					} `json:"app"`
				} `json:"steps"`
			} `json:"target"`
		} `json:"schedule"`
	}
	if err := json.Unmarshal([]byte(resp.Body), &body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	if body.Schedule.Cron != "*/5 * * * *" || body.Schedule.Timezone != "UTC" || len(body.Schedule.Target.Steps) != 1 || body.Schedule.Target.Steps[0].App.Name != "roadmap" || body.Schedule.Target.Steps[0].App.Operation != "sync" {
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
	if len(upsert.Target.Steps) != 1 || upsert.Target.Steps[0].App == nil || upsert.Target.Steps[0].App.Name != "roadmap" || upsert.Target.Steps[0].App.Operation != "sync" {
		t.Fatalf("upsert target = %#v", upsert.Target)
	}
}

func TestAgentRuntimeWorkflowSystemToolRejectsForwardStepOutputRefs(t *testing.T) {
	t.Parallel()

	agentRuntime, workflowProvider := newWorkflowSystemToolRuntime(t)
	workflowTool := mustWorkflowSystemTool(t, agentRuntime, workflowSystemToolRunsStart)
	runGrant := mustMintWorkflowSystemRunGrant(t, agentRuntime, workflowSystemRunGrantScope{
		CallerAppName: "slack",
		Permissions: []core.AccessPermission{
			{App: "github", Operations: []string{"createIssue"}},
			{App: "notification", Operations: []string{"reply"}},
		},
		ToolRefs: []coreagent.ToolRef{
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolRunsStart},
			{App: "github", Operation: "createIssue"},
			{App: "notification", Operation: "reply"},
		},
		Tools: []coreagent.Tool{workflowTool},
	})

	_, err := agentRuntime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
		ProviderName: "managed",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolCallID:   "call-run-forward-step-output",
		ToolID:       workflowTool.ID,
		RunGrant:     runGrant,
		Arguments: map[string]any{
			"workflowKey": "slack-child-run-forward-step-output",
			"target": workflowSystemToolTestTarget(
				map[string]any{
					"id": "create_issue",
					"app": map[string]any{
						"name":      "github",
						"operation": "createIssue",
						"input": map[string]any{
							"object": map[string]any{
								"title": map[string]any{
									"stepOutput": map[string]any{
										"stepId": "notify",
										"path":   "app.body.text",
									},
								},
							},
						},
					},
				},
				map[string]any{
					"id": "notify",
					"app": map[string]any{
						"name":      "notification",
						"operation": "reply",
						"input": map[string]any{
							"literal": map[string]any{"text": "created"},
						},
					},
				},
			),
		},
	})
	if err == nil {
		t.Fatal("ExecuteTool succeeded, want invalid invocation")
	}
	if !errors.Is(err, invocation.ErrInvalidInvocation) {
		t.Fatalf("ExecuteTool error = %v, want invalid invocation", err)
	}
	if !strings.Contains(err.Error(), `target.steps[0].app.input.title.step_output.step_id "notify" must reference an earlier step`) {
		t.Fatalf("ExecuteTool error = %v, want forward step output message", err)
	}
	if len(workflowProvider.startedRuns) != 0 {
		t.Fatalf("started runs = %d, want none", len(workflowProvider.startedRuns))
	}
}

func TestAgentRuntimeWorkflowSystemToolRejectsDuplicateStepIDs(t *testing.T) {
	t.Parallel()

	agentRuntime, workflowProvider := newWorkflowSystemToolRuntime(t)
	workflowTool := mustWorkflowSystemTool(t, agentRuntime, workflowSystemToolRunsStart)
	runGrant := mustMintWorkflowSystemRunGrant(t, agentRuntime, workflowSystemRunGrantScope{
		CallerAppName: "slack",
		Permissions: []core.AccessPermission{
			{App: "github", Operations: []string{"createIssue"}},
			{App: "notification", Operations: []string{"reply"}},
		},
		ToolRefs: []coreagent.ToolRef{
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolRunsStart},
			{App: "github", Operation: "createIssue"},
			{App: "notification", Operation: "reply"},
		},
		Tools: []coreagent.Tool{workflowTool},
	})

	_, err := agentRuntime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
		ProviderName: "managed",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolCallID:   "call-run-duplicate-step-ids",
		ToolID:       workflowTool.ID,
		RunGrant:     runGrant,
		Arguments: map[string]any{
			"workflowKey": "slack-child-run-duplicate-step-ids",
			"target": workflowSystemToolTestTarget(
				map[string]any{
					"id": "duplicate",
					"app": map[string]any{
						"name":      "github",
						"operation": "createIssue",
						"input": map[string]any{
							"literal": map[string]any{"title": "create"},
						},
					},
				},
				map[string]any{
					"id": "duplicate",
					"app": map[string]any{
						"name":      "notification",
						"operation": "reply",
						"input": map[string]any{
							"literal": map[string]any{"text": "created"},
						},
					},
				},
			),
		},
	})
	if err == nil {
		t.Fatal("ExecuteTool succeeded, want invalid invocation")
	}
	if !errors.Is(err, invocation.ErrInvalidInvocation) {
		t.Fatalf("ExecuteTool error = %v, want invalid invocation", err)
	}
	if !strings.Contains(err.Error(), `target.steps[1].id "duplicate" is duplicated`) {
		t.Fatalf("ExecuteTool error = %v, want duplicate step id message", err)
	}
	if len(workflowProvider.startedRuns) != 0 {
		t.Fatalf("started runs = %d, want none", len(workflowProvider.startedRuns))
	}
}

func TestWorkflowSystemToolValueInfoTemplateRoundTrips(t *testing.T) {
	t.Parallel()

	got := workflowSystemToolValueInfo(coreworkflow.Value{
		Template: &coreworkflow.Text{Template: "${steps.first.app.issue_number}"},
	})
	want := map[string]any{"template": "${steps.first.app.issue_number}"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("workflowSystemToolValueInfo = %#v, want %#v", got, want)
	}
}

func TestAgentRuntimeWorkflowSystemToolRunInfoIncludesSteps(t *testing.T) {
	t.Parallel()

	agentRuntime, workflowProvider := newWorkflowSystemToolRuntime(t)
	workflowTool := mustWorkflowSystemTool(t, agentRuntime, workflowSystemToolRunsStart)
	runGrant := mustMintWorkflowSystemRunGrant(t, agentRuntime, workflowSystemRunGrantScope{
		Permissions: []core.AccessPermission{
			{App: "managed"},
			{App: "datadog", Operations: []string{"queryLogs"}},
		},
		ToolRefs: []coreagent.ToolRef{
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolRunsStart},
			{App: "datadog", Operation: "queryLogs"},
		},
		Tools: []coreagent.Tool{workflowTool},
	})

	resp, err := agentRuntime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
		ProviderName: "managed",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolCallID:   "call-run-step-info",
		ToolID:       workflowTool.ID,
		RunGrant:     runGrant,
		Arguments: map[string]any{
			"workflowKey": "slack-child-run-step-info",
			"target": workflowSystemToolTestTarget(map[string]any{
				"id": "diagnosis",
				"agent": map[string]any{
					"provider": "managed",
					"prompt":   "Diagnose the Datadog alert.",
					"tools": []any{
						map[string]any{"app": "datadog", "operation": "queryLogs"},
					},
				},
			}),
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	if resp == nil || resp.Status != http.StatusCreated {
		t.Fatalf("response = %#v, want 201", resp)
	}
	var body struct {
		Run struct {
			Target struct {
				Steps []struct {
					ID    string `json:"id"`
					Agent struct {
						Prompt map[string]any `json:"prompt"`
						Tools  []struct {
							App       string `json:"app"`
							Operation string `json:"operation"`
						} `json:"tools"`
					} `json:"agent"`
				} `json:"steps"`
			} `json:"target"`
		} `json:"run"`
	}
	if err := json.Unmarshal([]byte(resp.Body), &body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	if len(body.Run.Target.Steps) != 1 || body.Run.Target.Steps[0].ID != "diagnosis" || body.Run.Target.Steps[0].Agent.Prompt == nil {
		t.Fatalf("run target steps = %#v", body.Run.Target.Steps)
	}
	if len(body.Run.Target.Steps[0].Agent.Tools) != 1 || body.Run.Target.Steps[0].Agent.Tools[0].App != "datadog" {
		t.Fatalf("step tools = %#v", body.Run.Target.Steps[0].Agent.Tools)
	}
	if len(workflowProvider.startedRuns) != 1 {
		t.Fatalf("started runs = %d, want 1", len(workflowProvider.startedRuns))
	}
}

func TestWorkflowSystemToolStartRunSchemaMatchesV1Contract(t *testing.T) {
	t.Parallel()

	schema := workflowSystemToolStartRunSchema()
	branches, ok := schema["oneOf"].([]any)
	if !ok || len(branches) != 2 {
		t.Fatalf("runs.start schema oneOf = %#v, want target and definition branches", schema["oneOf"])
	}
	targetBranch, ok := branches[0].(map[string]any)
	if !ok {
		t.Fatalf("target branch = %#v", branches[0])
	}
	targetProps, ok := targetBranch["properties"].(map[string]any)
	if !ok {
		t.Fatalf("target branch properties = %#v", targetBranch["properties"])
	}
	if _, ok := targetProps["definitionId"]; ok {
		t.Fatalf("target branch exposes definitionId: %#v", targetProps)
	}
	target, ok := targetProps["target"].(map[string]any)
	if !ok {
		t.Fatalf("target property = %#v", targetProps["target"])
	}
	targetTargetProps, ok := target["properties"].(map[string]any)
	if !ok {
		t.Fatalf("target properties = %#v", target["properties"])
	}
	if _, ok := targetTargetProps["app"]; ok {
		t.Fatalf("runs.start target schema exposes top-level app field: %#v", targetTargetProps)
	}
	if _, ok := targetTargetProps["agent"]; ok {
		t.Fatalf("runs.start target schema exposes top-level agent field: %#v", targetTargetProps)
	}
	if _, ok := targetTargetProps["steps"]; !ok {
		t.Fatalf("runs.start target schema missing steps: %#v", targetTargetProps)
	}
	definitionBranch, ok := branches[1].(map[string]any)
	if !ok {
		t.Fatalf("definition branch = %#v", branches[1])
	}
	definitionProps, ok := definitionBranch["properties"].(map[string]any)
	if !ok {
		t.Fatalf("definition branch properties = %#v", definitionBranch["properties"])
	}
	if _, ok := definitionProps["target"]; ok {
		t.Fatalf("definition branch exposes target: %#v", definitionProps)
	}
}

func TestAgentRuntimeWorkflowSystemToolCreatesDefinitionAndScheduleFromDefinition(t *testing.T) {
	t.Parallel()

	runtime, workflowProvider := newWorkflowSystemToolRuntime(t)
	definitionTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolDefinitionsCreate)
	scheduleTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolSchedulesCreate)
	runTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolRunsStart)
	runGrant := mustMintWorkflowSystemRunGrant(t, runtime, workflowSystemRunGrantScope{
		Permissions: []core.AccessPermission{
			{App: "roadmap", Operations: []string{"sync"}},
			{App: "linear", Operations: []string{"viewer"}},
		},
		ToolRefs: []coreagent.ToolRef{
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolDefinitionsCreate},
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolSchedulesCreate},
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolRunsStart},
			{App: "roadmap", Operation: "sync"},
		},
		Tools: []coreagent.Tool{definitionTool, scheduleTool, runTool},
	})

	definitionResp, err := runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
		ProviderName: "managed",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolCallID:   "definition-call",
		ToolID:       definitionTool.ID,
		RunGrant:     runGrant,
		Arguments: map[string]any{
			"target": workflowSystemToolTestTarget(map[string]any{
				"id": "agent",
				"agent": map[string]any{
					"provider": "managed",
					"prompt":   "Sync the roadmap and open the needed code changes.",
					"tools": []any{
						map[string]any{"app": "roadmap", "operation": "sync"},
						map[string]any{"system": coreagent.SystemToolWorkflow, "operation": workflowSystemToolSchedulesCreate},
					},
				},
			}),
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTool definition: %v", err)
	}
	if definitionResp == nil || definitionResp.Status != http.StatusCreated {
		t.Fatalf("definition response = %#v, want 201", definitionResp)
	}
	var definitionBody struct {
		Definition struct {
			ID       string `json:"id"`
			Provider string `json:"provider"`
			Target   struct {
				Steps []struct {
					Agent struct {
						Provider string         `json:"provider"`
						Prompt   map[string]any `json:"prompt"`
					} `json:"agent"`
				} `json:"steps"`
			} `json:"target"`
		} `json:"definition"`
	}
	if err := json.Unmarshal([]byte(definitionResp.Body), &definitionBody); err != nil {
		t.Fatalf("decode definition response body: %v", err)
	}
	definitionID := definitionBody.Definition.ID
	if definitionID == "" || definitionBody.Definition.Provider != "temporal" || len(definitionBody.Definition.Target.Steps) != 1 || definitionBody.Definition.Target.Steps[0].Agent.Provider != "managed" {
		t.Fatalf("definition response = %#v", definitionBody.Definition)
	}
	scheduleResp, err := runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
		ProviderName: "managed",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolCallID:   "schedule-call",
		ToolID:       scheduleTool.ID,
		RunGrant:     runGrant,
		Arguments: map[string]any{
			"cron":         "0 9 * * 1-5",
			"timezone":     "America/New_York",
			"definitionId": definitionID,
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTool schedule: %v", err)
	}
	if scheduleResp == nil || scheduleResp.Status != http.StatusCreated {
		t.Fatalf("schedule response = %#v, want 201", scheduleResp)
	}
	var scheduleBody struct {
		Schedule struct {
			ID           string `json:"id"`
			DefinitionID string `json:"definitionId"`
			Target       struct {
				Steps []struct {
					Agent struct {
						Provider string `json:"provider"`
					} `json:"agent"`
				} `json:"steps"`
			} `json:"target"`
		} `json:"schedule"`
	}
	if err := json.Unmarshal([]byte(scheduleResp.Body), &scheduleBody); err != nil {
		t.Fatalf("decode schedule response body: %v", err)
	}
	if scheduleBody.Schedule.ID == "" || scheduleBody.Schedule.DefinitionID != definitionID || len(scheduleBody.Schedule.Target.Steps) != 1 || scheduleBody.Schedule.Target.Steps[0].Agent.Provider != "managed" {
		t.Fatalf("schedule response = %#v", scheduleBody.Schedule)
	}
	if len(workflowProvider.upsertedSchedules) != 1 {
		t.Fatalf("upserted schedules = %d, want 1", len(workflowProvider.upsertedSchedules))
	}
	upsert := workflowProvider.upsertedSchedules[0]
	if upsert.DefinitionID != definitionID {
		t.Fatalf("schedule definition id = %q, want %q", upsert.DefinitionID, definitionID)
	}

	runResp, err := runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
		ProviderName: "managed",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolCallID:   "run-definition-call",
		ToolID:       runTool.ID,
		RunGrant:     runGrant,
		Arguments: map[string]any{
			"definitionId": definitionID,
			"workflowKey":  "definition-one-off",
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTool run from definition: %v", err)
	}
	if runResp == nil || runResp.Status != http.StatusCreated {
		t.Fatalf("run response = %#v, want 201", runResp)
	}
	if len(workflowProvider.startedRuns) != 1 {
		t.Fatalf("started runs = %d, want 1", len(workflowProvider.startedRuns))
	}
	if workflowProvider.startedRuns[0].DefinitionID != definitionID {
		t.Fatalf("run definition id = %q, want %q", workflowProvider.startedRuns[0].DefinitionID, definitionID)
	}
}

func TestAgentRuntimeWorkflowSystemToolCreatesDefinitionWithInheritedAgentToolRefs(t *testing.T) {
	t.Parallel()

	runtime, workflowProvider := newWorkflowSystemToolRuntime(t)
	definitionTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolDefinitionsCreate)
	scheduleTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolSchedulesCreate)
	runGrant := mustMintWorkflowSystemRunGrant(t, runtime, workflowSystemRunGrantScope{
		Permissions: []core.AccessPermission{
			{App: "roadmap", Operations: []string{"sync"}},
			{App: "linear", Operations: []string{"viewer"}},
			{App: "slack", Operations: []string{"chat.postMessage"}},
		},
		ToolRefs: []coreagent.ToolRef{
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolDefinitionsCreate},
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolSchedulesCreate},
			{App: "roadmap", Operation: "sync"},
			{App: "linear", Operation: "viewer"},
			{App: "*"},
		},
		Tools: []coreagent.Tool{
			definitionTool,
			scheduleTool,
			{Target: coreagent.ToolTarget{App: "slack", Operation: "chat.postMessage"}},
		},
	})

	resp, err := runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
		ProviderName: "managed",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolCallID:   "definition-call",
		ToolID:       definitionTool.ID,
		RunGrant:     runGrant,
		Arguments: map[string]any{
			"target": workflowSystemToolTestTarget(map[string]any{
				"id": "agent",
				"agent": map[string]any{
					"provider": "managed",
					"prompt":   "Sync the roadmap and post an update.",
				},
			}),
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTool definition: %v", err)
	}
	if resp == nil || resp.Status != http.StatusCreated {
		t.Fatalf("definition response = %#v, want 201", resp)
	}
	var body struct {
		Definition struct {
			ID string `json:"id"`
		} `json:"definition"`
	}
	if err := json.Unmarshal([]byte(resp.Body), &body); err != nil {
		t.Fatalf("decode definition response body: %v", err)
	}
	definition, err := workflowProvider.GetDefinition(context.Background(), coreworkflow.GetDefinitionRequest{DefinitionID: body.Definition.ID})
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	if len(definition.Target.Steps) != 1 || definition.Target.Steps[0].Agent == nil {
		t.Fatalf("definition target = %#v, want agent", definition.Target)
	}
	wantRefs := []coreagent.ToolRef{
		{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolDefinitionsCreate},
		{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolSchedulesCreate},
		{App: "roadmap", Operation: "sync"},
		{App: "linear", Operation: "viewer"},
		{App: "slack", Operation: "chat.postMessage"},
	}
	if !reflect.DeepEqual(definition.Target.Steps[0].Agent.ToolRefs, wantRefs) {
		t.Fatalf("inherited tool refs = %#v, want %#v", definition.Target.Steps[0].Agent.ToolRefs, wantRefs)
	}
}

func TestAgentRuntimeWorkflowSystemToolCreatesScheduleWithInheritedAgentToolRefs(t *testing.T) {
	t.Parallel()

	runtime, workflowProvider := newWorkflowSystemToolRuntime(t)
	workflowTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolSchedulesCreate)
	runGrant := mustMintWorkflowSystemRunGrant(t, runtime, workflowSystemRunGrantScope{
		Permissions: []core.AccessPermission{{
			App:        "roadmap",
			Operations: []string{"sync"},
		}},
		ToolRefs: []coreagent.ToolRef{
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolSchedulesCreate},
			{App: "roadmap", Operation: "sync"},
		},
		Tools: []coreagent.Tool{workflowTool},
	})

	resp, err := runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
		ProviderName: "managed",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolCallID:   "schedule-call",
		ToolID:       workflowTool.ID,
		RunGrant:     runGrant,
		Arguments: map[string]any{
			"cron":     "*/5 * * * *",
			"timezone": "UTC",
			"target": workflowSystemToolTestTarget(map[string]any{
				"id": "agent",
				"agent": map[string]any{
					"provider": "managed",
					"prompt":   "Sync the roadmap.",
				},
			}),
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTool schedule: %v", err)
	}
	if resp == nil || resp.Status != http.StatusCreated {
		t.Fatalf("schedule response = %#v, want 201", resp)
	}
	if len(workflowProvider.upsertedSchedules) != 1 {
		t.Fatalf("upserted schedules = %d, want 1", len(workflowProvider.upsertedSchedules))
	}
	upsert := workflowProvider.upsertedSchedules[0]
	if len(upsert.Target.Steps) != 1 || upsert.Target.Steps[0].Agent == nil {
		t.Fatalf("schedule target = %#v, want agent", upsert.Target)
	}
	wantRefs := []coreagent.ToolRef{
		{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolSchedulesCreate},
		{App: "roadmap", Operation: "sync"},
	}
	if !reflect.DeepEqual(upsert.Target.Steps[0].Agent.ToolRefs, wantRefs) {
		t.Fatalf("inherited tool refs = %#v, want %#v", upsert.Target.Steps[0].Agent.ToolRefs, wantRefs)
	}
}

func TestAgentRuntimeWorkflowSystemToolCreatesScheduleWithGrantedCallerToolRefs(t *testing.T) {
	t.Parallel()

	runtime, workflowProvider := newWorkflowSystemToolRuntime(t)
	workflowTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolSchedulesCreate)
	runAs := &core.RunAsSubject{
		SubjectID:   "service_account:github-toolshed",
		SubjectKind: "service_account",
	}
	externalIdentity := &core.ExternalIdentityRef{
		Type: "github_app_installation",
		ID:   "repo:valon-technologies/toolshed",
	}
	runGrant := mustMintWorkflowSystemRunGrant(t, runtime, workflowSystemRunGrantScope{
		CallerAppName: "slack",
		Permissions: []core.AccessPermission{{
			App:        "github",
			Operations: []string{"bot.createPullRequest"},
		}},
		ToolRefs: []coreagent.ToolRef{
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolSchedulesCreate},
			{
				App:                   "github",
				Operation:             "bot.createPullRequest",
				CredentialMode:        core.ConnectionModeNone,
				RunAs:                 runAs,
				RunAsExternalIdentity: externalIdentity,
			},
		},
		Tools: []coreagent.Tool{workflowTool},
	})

	resp, err := runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
		ProviderName: "managed",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolCallID:   "schedule-call",
		ToolID:       workflowTool.ID,
		RunGrant:     runGrant,
		Arguments: map[string]any{
			"cron":     "*/5 * * * *",
			"timezone": "UTC",
			"target": workflowSystemToolTestTarget(map[string]any{
				"id": "agent",
				"agent": map[string]any{
					"provider": "managed",
					"prompt":   "Open a GitHub pull request.",
				},
			}),
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTool schedule: %v", err)
	}
	if resp == nil || resp.Status != http.StatusCreated {
		t.Fatalf("schedule response = %#v, want 201", resp)
	}
	if len(workflowProvider.upsertedSchedules) != 1 {
		t.Fatalf("upserted schedules = %d, want 1", len(workflowProvider.upsertedSchedules))
	}
	upsert := workflowProvider.upsertedSchedules[0]
	if len(upsert.Target.Steps) != 1 || upsert.Target.Steps[0].Agent == nil {
		t.Fatalf("schedule target = %#v, want agent", upsert.Target)
	}
	wantRefs := []coreagent.ToolRef{
		{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolSchedulesCreate},
		{App: "github", Operation: "bot.createPullRequest"},
	}
	if !reflect.DeepEqual(upsert.Target.Steps[0].Agent.ToolRefs, wantRefs) {
		t.Fatalf("inherited tool refs = %#v, want %#v", upsert.Target.Steps[0].Agent.ToolRefs, wantRefs)
	}
}

func TestAgentRuntimeWorkflowSystemToolCreatesScheduleWithExplicitEmptyAgentToolRefs(t *testing.T) {
	t.Parallel()

	runtime, workflowProvider := newWorkflowSystemToolRuntime(t)
	workflowTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolSchedulesCreate)
	runGrant := mustMintWorkflowSystemRunGrant(t, runtime, workflowSystemRunGrantScope{
		Permissions: []core.AccessPermission{{
			App:        "roadmap",
			Operations: []string{"sync"},
		}},
		ToolRefs: []coreagent.ToolRef{
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolSchedulesCreate},
			{App: "roadmap", Operation: "sync"},
		},
		Tools: []coreagent.Tool{workflowTool},
	})

	resp, err := runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
		ProviderName: "managed",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolCallID:   "schedule-call",
		ToolID:       workflowTool.ID,
		RunGrant:     runGrant,
		Arguments: map[string]any{
			"cron":     "*/5 * * * *",
			"timezone": "UTC",
			"target": workflowSystemToolTestTarget(map[string]any{
				"id": "agent",
				"agent": map[string]any{
					"provider": "managed",
					"prompt":   "Run without tools.",
					"tools":    []any{},
				},
			}),
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTool schedule: %v", err)
	}
	if resp == nil || resp.Status != http.StatusCreated {
		t.Fatalf("schedule response = %#v, want 201", resp)
	}
	if len(workflowProvider.upsertedSchedules) != 1 {
		t.Fatalf("upserted schedules = %d, want 1", len(workflowProvider.upsertedSchedules))
	}
	upsert := workflowProvider.upsertedSchedules[0]
	if len(upsert.Target.Steps) != 1 || upsert.Target.Steps[0].Agent == nil {
		t.Fatalf("schedule target = %#v, want agent", upsert.Target)
	}
	if len(upsert.Target.Steps[0].Agent.ToolRefs) != 0 {
		t.Fatalf("explicit empty tool refs = %#v, want empty slice", upsert.Target.Steps[0].Agent.ToolRefs)
	}
}

func TestAgentRuntimeWorkflowSystemToolUpdatesAndDeletesDefinition(t *testing.T) {
	t.Parallel()

	runtime, workflowProvider := newWorkflowSystemToolRuntime(t)
	createTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolDefinitionsCreate)
	getTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolDefinitionsGet)
	updateTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolDefinitionsUpdate)
	deleteTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolDefinitionsDelete)
	runGrant := mustMintWorkflowSystemRunGrant(t, runtime, workflowSystemRunGrantScope{
		Permissions: []core.AccessPermission{{App: "roadmap", Operations: []string{"sync"}}},
		ToolRefs: []coreagent.ToolRef{
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolDefinitionsCreate},
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolDefinitionsGet},
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolDefinitionsUpdate},
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolDefinitionsDelete},
			{App: "roadmap", Operation: "sync"},
		},
		Tools: []coreagent.Tool{createTool, getTool, updateTool, deleteTool},
	})

	createResp, err := runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
		ProviderName: "managed",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolCallID:   "definition-create",
		ToolID:       createTool.ID,
		RunGrant:     runGrant,
		Arguments: map[string]any{
			"target": workflowSystemToolTestTarget(map[string]any{
				"id": "agent",
				"agent": map[string]any{
					"provider": "managed",
					"prompt":   "Sync the roadmap.",
				},
			}),
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTool create definition: %v", err)
	}
	var createBody struct {
		Definition struct {
			ID string `json:"id"`
		} `json:"definition"`
	}
	if err := json.Unmarshal([]byte(createResp.Body), &createBody); err != nil {
		t.Fatalf("decode create definition response body: %v", err)
	}
	definitionID := createBody.Definition.ID
	if definitionID == "" {
		t.Fatalf("definition id is empty")
	}

	updateResp, err := runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
		ProviderName: "managed",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolCallID:   "definition-update",
		ToolID:       updateTool.ID,
		RunGrant:     runGrant,
		Arguments: map[string]any{
			"definitionId": definitionID,
			"target": workflowSystemToolTestTarget(map[string]any{
				"id": "agent",
				"agent": map[string]any{
					"provider": "managed",
					"prompt":   "Sync the roadmap and summarize changes.",
				},
			}),
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTool update definition: %v", err)
	}
	if updateResp == nil || updateResp.Status != http.StatusOK {
		t.Fatalf("update definition response = %#v, want 200", updateResp)
	}
	definition, err := workflowProvider.GetDefinition(context.Background(), coreworkflow.GetDefinitionRequest{DefinitionID: definitionID})
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	if len(definition.Target.Steps) != 1 || definition.Target.Steps[0].Agent == nil || definition.Target.Steps[0].Agent.Prompt.Template != "Sync the roadmap and summarize changes." {
		t.Fatalf("definition target = %#v", definition.Target)
	}
	wantRefs := []coreagent.ToolRef{
		{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolDefinitionsCreate},
		{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolDefinitionsGet},
		{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolDefinitionsUpdate},
		{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolDefinitionsDelete},
		{App: "roadmap", Operation: "sync"},
	}
	if !reflect.DeepEqual(definition.Target.Steps[0].Agent.ToolRefs, wantRefs) {
		t.Fatalf("updated definition inherited tool refs = %#v, want %#v", definition.Target.Steps[0].Agent.ToolRefs, wantRefs)
	}

	getResp, err := runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
		ProviderName: "managed",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolCallID:   "definition-get",
		ToolID:       getTool.ID,
		RunGrant:     runGrant,
		Arguments:    map[string]any{"definitionId": definitionID},
	})
	if err != nil {
		t.Fatalf("ExecuteTool get definition: %v", err)
	}
	if getResp == nil || getResp.Status != http.StatusOK {
		t.Fatalf("get definition response = %#v, want 200", getResp)
	}

	deleteResp, err := runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
		ProviderName: "managed",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolCallID:   "definition-delete",
		ToolID:       deleteTool.ID,
		RunGrant:     runGrant,
		Arguments:    map[string]any{"definitionId": definitionID},
	})
	if err != nil {
		t.Fatalf("ExecuteTool delete definition: %v", err)
	}
	if deleteResp == nil || deleteResp.Status != http.StatusOK {
		t.Fatalf("delete definition response = %#v, want 200", deleteResp)
	}
	_, err = runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
		ProviderName: "managed",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolCallID:   "definition-get-deleted",
		ToolID:       getTool.ID,
		RunGrant:     runGrant,
		Arguments:    map[string]any{"definitionId": definitionID},
	})
	if !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("get deleted definition error = %v, want not found", err)
	}
}

func TestAgentRuntimeWorkflowSystemToolUpdatesAndDeletesSchedule(t *testing.T) {
	t.Parallel()

	runtime, workflowProvider := newWorkflowSystemToolRuntime(t)
	definitionTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolDefinitionsCreate)
	createTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolSchedulesCreate)
	getTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolSchedulesGet)
	updateTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolSchedulesUpdate)
	deleteTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolSchedulesDelete)
	runGrant := mustMintWorkflowSystemRunGrant(t, runtime, workflowSystemRunGrantScope{
		Permissions: []core.AccessPermission{{App: "roadmap", Operations: []string{"sync"}}},
		ToolRefs: []coreagent.ToolRef{
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolDefinitionsCreate},
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolSchedulesCreate},
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolSchedulesGet},
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolSchedulesUpdate},
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolSchedulesDelete},
			{App: "roadmap", Operation: "sync"},
		},
		Tools: []coreagent.Tool{definitionTool, createTool, getTool, updateTool, deleteTool},
	})

	definitionResp, err := runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
		ProviderName: "managed",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolCallID:   "definition-create",
		ToolID:       definitionTool.ID,
		RunGrant:     runGrant,
		Arguments: map[string]any{
			"target": workflowSystemToolTestTarget(map[string]any{
				"id": "agent",
				"agent": map[string]any{
					"provider": "managed",
					"prompt":   "Sync the roadmap.",
				},
			}),
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTool create definition: %v", err)
	}
	var definitionBody struct {
		Definition struct {
			ID string `json:"id"`
		} `json:"definition"`
	}
	if err := json.Unmarshal([]byte(definitionResp.Body), &definitionBody); err != nil {
		t.Fatalf("decode definition response body: %v", err)
	}

	createResp, err := runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
		ProviderName: "managed",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolCallID:   "schedule-create",
		ToolID:       createTool.ID,
		RunGrant:     runGrant,
		Arguments: map[string]any{
			"cron":         "*/5 * * * *",
			"timezone":     "UTC",
			"definitionId": definitionBody.Definition.ID,
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTool create schedule: %v", err)
	}
	var createBody struct {
		Schedule struct {
			ID           string `json:"id"`
			DefinitionID string `json:"definitionId"`
		} `json:"schedule"`
	}
	if err := json.Unmarshal([]byte(createResp.Body), &createBody); err != nil {
		t.Fatalf("decode create schedule response body: %v", err)
	}
	scheduleID := createBody.Schedule.ID
	if scheduleID == "" || createBody.Schedule.DefinitionID != definitionBody.Definition.ID {
		t.Fatalf("created schedule = %#v", createBody.Schedule)
	}

	updateResp, err := runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
		ProviderName: "managed",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolCallID:   "schedule-update",
		ToolID:       updateTool.ID,
		RunGrant:     runGrant,
		Arguments: map[string]any{
			"scheduleId": scheduleID,
			"cron":       "*/15 * * * *",
			"paused":     true,
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTool update schedule: %v", err)
	}
	if updateResp == nil || updateResp.Status != http.StatusOK {
		t.Fatalf("update schedule response = %#v, want 200", updateResp)
	}
	var updateBody struct {
		Schedule struct {
			ID           string `json:"id"`
			Cron         string `json:"cron"`
			Paused       bool   `json:"paused"`
			DefinitionID string `json:"definitionId"`
		} `json:"schedule"`
	}
	if err := json.Unmarshal([]byte(updateResp.Body), &updateBody); err != nil {
		t.Fatalf("decode update schedule response body: %v", err)
	}
	if updateBody.Schedule.ID != scheduleID || updateBody.Schedule.Cron != "*/15 * * * *" || !updateBody.Schedule.Paused || updateBody.Schedule.DefinitionID != definitionBody.Definition.ID {
		t.Fatalf("updated schedule = %#v", updateBody.Schedule)
	}
	if len(workflowProvider.upsertedSchedules) != 2 {
		t.Fatalf("upserted schedules = %d, want 2", len(workflowProvider.upsertedSchedules))
	}
	updateUpsert := workflowProvider.upsertedSchedules[1]
	if len(updateUpsert.Target.Steps) != 1 || updateUpsert.Target.Steps[0].Agent == nil || updateUpsert.Target.Steps[0].Agent.Prompt.Template != "Sync the roadmap." {
		t.Fatalf("updated upsert target = %#v", updateUpsert.Target)
	}
	if updateUpsert.DefinitionID != definitionBody.Definition.ID {
		t.Fatalf("schedule definition id = %q, want %q", updateUpsert.DefinitionID, definitionBody.Definition.ID)
	}

	deleteResp, err := runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
		ProviderName: "managed",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolCallID:   "schedule-delete",
		ToolID:       deleteTool.ID,
		RunGrant:     runGrant,
		Arguments:    map[string]any{"scheduleId": scheduleID},
	})
	if err != nil {
		t.Fatalf("ExecuteTool delete schedule: %v", err)
	}
	if deleteResp == nil || deleteResp.Status != http.StatusOK {
		t.Fatalf("delete schedule response = %#v, want 200", deleteResp)
	}
	_, err = runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
		ProviderName: "managed",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolCallID:   "schedule-get-deleted",
		ToolID:       getTool.ID,
		RunGrant:     runGrant,
		Arguments:    map[string]any{"scheduleId": scheduleID},
	})
	if !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("get deleted schedule error = %v, want not found", err)
	}
}

func TestAgentRuntimeWorkflowSystemToolUpdateDefinitionScheduleUsesManagementPrincipal(t *testing.T) {
	t.Parallel()

	permissions := principal.CompilePermissions([]core.AccessPermission{
		{App: "roadmap", Operations: []string{"sync"}},
		{App: "notification", Operations: []string{"reply"}},
	})
	owner := coreworkflow.Actor{SubjectID: principal.UserSubjectID("ada")}
	target := workflowSystemToolTestAppStepTarget("roadmap", "sync")
	manager := &workflowSystemToolPrincipalRecordingManager{
		schedule: &workflowmanager.ManagedSchedule{
			ProviderName: "temporal",
			Schedule: &coreworkflow.Schedule{
				ID:           "schedule-1",
				Cron:         "*/5 * * * *",
				Timezone:     "UTC",
				Target:       target,
				DefinitionID: "definition-1",
				CreatedBy:    owner,
			},
		},
		definition: &workflowmanager.ManagedDefinition{
			ProviderName: "temporal",
			Definition: &coreworkflow.Definition{
				ID:        "definition-1",
				Target:    target,
				CreatedBy: owner,
			},
		},
	}
	tools := newWorkflowSystemTools(manager, nil)

	resp, err := tools.executeUpdateSchedule(context.Background(), agentSystemToolExecutionRequest{
		Principal: &principal.Principal{
			SubjectID:        principal.UserSubjectID("ada"),
			Kind:             principal.KindUser,
			TokenPermissions: permissions,
			Scopes:           principal.PermissionApps(permissions),
		},
		ProviderName: "managed",
		Arguments: map[string]any{
			"scheduleId": "schedule-1",
			"cron":       "*/15 * * * *",
		},
		ToolRefs: []coreagent.ToolRef{{App: "roadmap", Operation: "sync"}},
	})
	if err != nil {
		t.Fatalf("executeUpdateSchedule: %v", err)
	}
	if resp == nil || resp.Status != http.StatusOK {
		t.Fatalf("update schedule response = %#v, want 200", resp)
	}
	if manager.getSchedulePrincipal == nil || manager.updateSchedulePrincipal == nil {
		t.Fatalf("schedule principals were not recorded")
	}
	if !reflect.DeepEqual(manager.updateSchedulePrincipal.TokenPermissions, manager.getSchedulePrincipal.TokenPermissions) {
		t.Fatalf("update token permissions = %#v, want management permissions %#v", manager.updateSchedulePrincipal.TokenPermissions, manager.getSchedulePrincipal.TokenPermissions)
	}
	if !principal.AllowsOperationPermission(manager.updateSchedulePrincipal, "notification", "reply") {
		t.Fatalf("definition schedule metadata update used narrowed target permissions: %#v", manager.updateSchedulePrincipal.TokenPermissions)
	}
	if !principal.AllowsProviderPermission(manager.updateSchedulePrincipal, "managed") {
		t.Fatalf("definition schedule metadata update lost trusted agent provider: %#v", manager.updateSchedulePrincipal.TokenPermissions)
	}
	if manager.updateScheduleID != "schedule-1" || manager.updateReq.DefinitionID != "definition-1" || manager.updateReq.Cron != "*/15 * * * *" {
		t.Fatalf("update request = id %q req %#v", manager.updateScheduleID, manager.updateReq)
	}
	if len(manager.updateReq.Target.Steps) != 0 {
		t.Fatalf("definition-linked schedule update target = %#v, want empty upsert target", manager.updateReq.Target)
	}
}

func TestAgentRuntimeWorkflowSystemToolListsRunsWithPaginationAndFilters(t *testing.T) {
	t.Parallel()

	runtime, workflowProvider := newWorkflowSystemToolRuntime(t)
	listTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolRunsList)
	runGrant := mustMintWorkflowSystemRunGrant(t, runtime, workflowSystemRunGrantScope{
		Permissions: []core.AccessPermission{{App: "roadmap", Operations: []string{"sync"}}},
		ToolRefs: []coreagent.ToolRef{
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolRunsList},
			{App: "roadmap", Operation: "sync"},
		},
		Tools: []coreagent.Tool{listTool},
	})
	roadmapTarget := workflowSystemToolTestAppStepTarget("roadmap", "sync")
	notificationTarget := workflowSystemToolTestAppStepTarget("notification", "reply")
	owner := coreworkflow.Actor{SubjectID: principal.UserSubjectID("ada")}
	workflowProvider.runs = map[string]*coreworkflow.Run{
		"run-a": {ID: "run-a", Status: coreworkflow.RunStatusSucceeded, Target: roadmapTarget, CreatedBy: owner},
		"run-b": {ID: "run-b", Status: coreworkflow.RunStatusSucceeded, Target: notificationTarget, CreatedBy: owner},
		"run-c": {ID: "run-c", Status: coreworkflow.RunStatusSucceeded, Target: roadmapTarget, CreatedBy: owner},
		"run-d": {ID: "run-d", Status: coreworkflow.RunStatusFailed, Target: roadmapTarget, CreatedBy: owner},
	}

	firstResp, err := runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
		ProviderName: "managed",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolCallID:   "runs-list-first",
		ToolID:       listTool.ID,
		RunGrant:     runGrant,
		Arguments: map[string]any{
			"pageSize": 1,
			"app":      "roadmap",
			"status":   string(coreworkflow.RunStatusSucceeded),
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTool first list: %v", err)
	}
	var firstBody struct {
		Runs []struct {
			ID string `json:"id"`
		} `json:"runs"`
		NextPageToken string `json:"nextPageToken"`
	}
	if err := json.Unmarshal([]byte(firstResp.Body), &firstBody); err != nil {
		t.Fatalf("decode first response body: %v", err)
	}
	if len(firstBody.Runs) != 1 || firstBody.Runs[0].ID != "run-a" || firstBody.NextPageToken == "" {
		t.Fatalf("first page = %#v next=%q, want run-a and next token", firstBody.Runs, firstBody.NextPageToken)
	}

	secondResp, err := runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
		ProviderName: "managed",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolCallID:   "runs-list-second",
		ToolID:       listTool.ID,
		RunGrant:     runGrant,
		Arguments: map[string]any{
			"pageSize":  1,
			"pageToken": firstBody.NextPageToken,
			"app":       "roadmap",
			"status":    string(coreworkflow.RunStatusSucceeded),
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTool second list: %v", err)
	}
	var secondBody struct {
		Runs []struct {
			ID string `json:"id"`
		} `json:"runs"`
		NextPageToken string `json:"nextPageToken"`
	}
	if err := json.Unmarshal([]byte(secondResp.Body), &secondBody); err != nil {
		t.Fatalf("decode second response body: %v", err)
	}
	if len(secondBody.Runs) != 1 || secondBody.Runs[0].ID != "run-c" || secondBody.NextPageToken != "" {
		t.Fatalf("second page = %#v next=%q, want final run-c page", secondBody.Runs, secondBody.NextPageToken)
	}
}

func TestAgentRuntimeWorkflowSystemToolRejectsUngrantedDefinitionTarget(t *testing.T) {
	t.Parallel()

	runtime, _ := newWorkflowSystemToolRuntime(t)
	definitionTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolDefinitionsCreate)
	runGrant := mustMintWorkflowSystemRunGrant(t, runtime, workflowSystemRunGrantScope{
		Permissions: []core.AccessPermission{
			{App: "roadmap", Operations: []string{"sync"}},
		},
		ToolRefs: []coreagent.ToolRef{
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolDefinitionsCreate},
		},
		Tools: []coreagent.Tool{definitionTool},
	})

	cases := []struct {
		name      string
		arguments map[string]any
	}{
		{
			name: "app step",
			arguments: map[string]any{
				"target": workflowSystemToolTestTarget(map[string]any{
					"id": "sync",
					"app": map[string]any{
						"name":      "roadmap",
						"operation": "sync",
					},
				}),
			},
		},
		{
			name: "future system ref",
			arguments: map[string]any{
				"target": workflowSystemToolTestTarget(map[string]any{
					"id": "agent",
					"agent": map[string]any{
						"provider": "managed",
						"prompt":   "Create another cron.",
						"tools": []any{
							map[string]any{"system": coreagent.SystemToolWorkflow, "operation": workflowSystemToolSchedulesCreate},
						},
					},
				}),
			},
		},
	}
	for _, tc := range cases {
		_, err := runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
			ProviderName: "managed",
			SessionID:    "session-1",
			TurnID:       "turn-1",
			ToolID:       definitionTool.ID,
			RunGrant:     runGrant,
			Arguments:    tc.arguments,
		})
		if err == nil {
			t.Fatalf("%s: ExecuteTool succeeded, want scope denial", tc.name)
		}
		if !errors.Is(err, invocation.ErrScopeDenied) {
			t.Fatalf("%s: ExecuteTool error = %v, want scope denied", tc.name, err)
		}
	}
}

func TestAgentRuntimeWorkflowSystemToolRejectsInvalidScheduleDefinitionArguments(t *testing.T) {
	t.Parallel()

	runtime, _ := newWorkflowSystemToolRuntime(t)
	scheduleTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolSchedulesCreate)
	runGrant := mustMintWorkflowSystemRunGrant(t, runtime, workflowSystemRunGrantScope{
		ToolRefs: []coreagent.ToolRef{
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolSchedulesCreate},
		},
		Tools: []coreagent.Tool{scheduleTool},
	})

	cases := []struct {
		name      string
		arguments map[string]any
	}{
		{
			name: "missing target and definition",
			arguments: map[string]any{
				"cron": "*/5 * * * *",
			},
		},
		{
			name: "target and definition",
			arguments: map[string]any{
				"cron":         "*/5 * * * *",
				"definitionId": "workflow_definition:def-1",
				"target": workflowSystemToolTestTarget(map[string]any{
					"id": "sync",
					"app": map[string]any{
						"name":      "roadmap",
						"operation": "sync",
					},
				}),
			},
		},
	}
	for _, tc := range cases {
		_, err := runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
			ProviderName: "managed",
			SessionID:    "session-1",
			TurnID:       "turn-1",
			ToolID:       scheduleTool.ID,
			RunGrant:     runGrant,
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

func TestAgentRuntimeWorkflowSystemToolRejectsUngrantedScheduleTarget(t *testing.T) {
	t.Parallel()

	runtime, _ := newWorkflowSystemToolRuntime(t)
	workflowTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolSchedulesCreate)
	runGrant := mustMintWorkflowSystemRunGrant(t, runtime, workflowSystemRunGrantScope{
		Permissions: []core.AccessPermission{{
			App:        "roadmap",
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
		RunGrant:     runGrant,
		Arguments: map[string]any{
			"cron": "*/5 * * * *",
			"target": workflowSystemToolTestTarget(map[string]any{
				"id": "sync",
				"app": map[string]any{
					"name":      "roadmap",
					"operation": "sync",
				},
			}),
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
	runGrant := mustMintWorkflowSystemRunGrant(t, runtime, workflowSystemRunGrantScope{
		Permissions: []core.AccessPermission{{
			App:        "roadmap",
			Operations: []string{"sync"},
		}},
		ToolRefs: []coreagent.ToolRef{
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolSchedulesCreate},
			{App: "roadmap", Operation: "sync"},
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
				"target": workflowSystemToolTestTarget(map[string]any{
					"id": "agent",
					"agent": map[string]any{
						"provider": "managed",
						"prompt":   "Sync roadmap",
						"tools": []any{
							map[string]any{
								"app":            "roadmap",
								"operation":      "sync",
								"credentialMode": "subject",
							},
						},
					},
				}),
			},
		},
		{
			name: "agent toolRefs rejected",
			arguments: map[string]any{
				"cron": "*/5 * * * *",
				"target": workflowSystemToolTestTarget(map[string]any{
					"id": "agent",
					"agent": map[string]any{
						"provider": "managed",
						"prompt":   "Sync roadmap",
						"toolRefs": []any{
							map[string]any{"app": "roadmap", "operation": "sync"},
						},
					},
				}),
			},
		},
	}
	for _, tc := range cases {
		_, err := runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
			ProviderName: "managed",
			SessionID:    "session-1",
			TurnID:       "turn-1",
			ToolID:       workflowTool.ID,
			RunGrant:     runGrant,
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
	runGrant := mustMintWorkflowSystemRunGrant(t, runtime, workflowSystemRunGrantScope{
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
		RunGrant:     runGrant,
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

func TestWorkflowSystemToolTrustedAgentProviderChecksAllAgentSteps(t *testing.T) {
	t.Parallel()

	req := agentSystemToolExecutionRequest{ProviderName: "managed"}
	target := coreworkflow.Target{Steps: []coreworkflow.Step{
		{
			ID: "first",
			Agent: &coreworkflow.AgentTurn{
				ProviderName: "managed",
				Prompt:       coreworkflow.Text{Template: "first"},
			},
		},
		{
			ID: "second",
			Agent: &coreworkflow.AgentTurn{
				ProviderName: "other",
				Prompt:       coreworkflow.Text{Template: "second"},
			},
		},
	}}

	if got := workflowSystemToolTrustedAgentProvider(req, target); got != "" {
		t.Fatalf("trusted provider = %q, want empty for mixed agent providers", got)
	}

	target.Steps[1].Agent.ProviderName = ""
	if got := workflowSystemToolTrustedAgentProvider(req, target); got != "managed" {
		t.Fatalf("trusted provider = %q, want managed when all agent steps use current/default provider", got)
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
	if err := reg.Providers.Register("notification", &coretesting.StubIntegration{
		N:        "notification",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{
			Name: "notification",
			Operations: []catalog.CatalogOperation{{
				ID:     "reply",
				Method: http.MethodPost,
			}},
		},
	}); err != nil {
		t.Fatalf("Register notification: %v", err)
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
	workflowManager := workflowmanager.New(workflowmanager.Config{
		Providers:    &reg.Providers,
		Workflow:     workflowRuntime,
		Agent:        runtime,
		AgentManager: agentManager,
		AppInvokes: map[string][]invocation.AppInvocationDependency{
			"slack": {
				{App: "notification", Operation: "reply", CredentialMode: core.ConnectionModeNone},
			},
		},
	})
	runtime.SetRunGrants(newTestAgentRunGrants(t))
	runtime.SetToolSearcher(workflowSystemToolResolver{})
	runtime.SetSystemToolExecutor(newWorkflowSystemTools(workflowManager, workflowRuntime))
	return runtime, workflowProvider
}

type workflowSystemToolResolver struct{}

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

type workflowSystemToolPrincipalRecordingManager struct {
	workflowmanager.Service

	getSchedulePrincipal    *principal.Principal
	getDefinitionPrincipal  *principal.Principal
	updateSchedulePrincipal *principal.Principal
	updateScheduleID        string
	updateReq               workflowmanager.ScheduleUpsert
	schedule                *workflowmanager.ManagedSchedule
	definition              *workflowmanager.ManagedDefinition
}

func (m *workflowSystemToolPrincipalRecordingManager) GetSchedule(_ context.Context, p *principal.Principal, scheduleID string) (*workflowmanager.ManagedSchedule, error) {
	m.getSchedulePrincipal = principal.Canonicalized(p)
	if m.schedule == nil || m.schedule.Schedule == nil || m.schedule.Schedule.ID != scheduleID {
		return nil, core.ErrNotFound
	}
	schedule := *m.schedule.Schedule
	return &workflowmanager.ManagedSchedule{ProviderName: m.schedule.ProviderName, Schedule: &schedule}, nil
}

func (m *workflowSystemToolPrincipalRecordingManager) GetDefinition(_ context.Context, p *principal.Principal, definitionID string) (*workflowmanager.ManagedDefinition, error) {
	m.getDefinitionPrincipal = principal.Canonicalized(p)
	if m.definition == nil || m.definition.Definition == nil || m.definition.Definition.ID != definitionID {
		return nil, core.ErrNotFound
	}
	definition := *m.definition.Definition
	return &workflowmanager.ManagedDefinition{ProviderName: m.definition.ProviderName, Definition: &definition}, nil
}

func (m *workflowSystemToolPrincipalRecordingManager) UpdateSchedule(_ context.Context, p *principal.Principal, scheduleID string, req workflowmanager.ScheduleUpsert) (*workflowmanager.ManagedSchedule, error) {
	m.updateSchedulePrincipal = principal.Canonicalized(p)
	m.updateScheduleID = scheduleID
	m.updateReq = req
	if m.schedule == nil || m.schedule.Schedule == nil || m.schedule.Schedule.ID != scheduleID {
		return nil, core.ErrNotFound
	}

	schedule := *m.schedule.Schedule
	schedule.Cron = strings.TrimSpace(req.Cron)
	schedule.Timezone = strings.TrimSpace(req.Timezone)
	schedule.Paused = req.Paused
	schedule.DefinitionID = strings.TrimSpace(req.DefinitionID)
	schedule.Target = req.Target
	if schedule.DefinitionID != "" && len(schedule.Target.Steps) == 0 && m.definition != nil && m.definition.Definition != nil {
		schedule.Target = m.definition.Definition.Target
	}
	providerName := strings.TrimSpace(req.ProviderName)
	if providerName == "" {
		providerName = m.schedule.ProviderName
	}
	return &workflowmanager.ManagedSchedule{ProviderName: providerName, Schedule: &schedule}, nil
}

type workflowSystemToolRecordingProvider struct {
	startedRuns       []coreworkflow.StartRunRequest
	runs              map[string]*coreworkflow.Run
	runIdempotency    map[string]string
	definitions       map[string]*coreworkflow.Definition
	definitionCounter int
	upsertedSchedules []coreworkflow.UpsertScheduleRequest
	schedules         map[string]*coreworkflow.Schedule
}

func (p *workflowSystemToolRecordingProvider) CreateDefinition(_ context.Context, req coreworkflow.CreateDefinitionRequest) (*coreworkflow.Definition, error) {
	if p.definitions == nil {
		p.definitions = map[string]*coreworkflow.Definition{}
	}
	id := strings.TrimSpace(req.IdempotencyKey)
	if id == "" {
		p.definitionCounter++
		id = fmt.Sprintf("definition-%d", p.definitionCounter)
	} else {
		id = "definition-" + id
	}
	if existing := p.definitions[id]; existing != nil {
		value := *existing
		return &value, nil
	}
	definition := &coreworkflow.Definition{
		ID:        id,
		Target:    req.Target,
		CreatedBy: req.CreatedBy,
	}
	p.definitions[id] = definition
	value := *definition
	return &value, nil
}

func (p *workflowSystemToolRecordingProvider) GetDefinition(_ context.Context, req coreworkflow.GetDefinitionRequest) (*coreworkflow.Definition, error) {
	if definition := p.definitions[strings.TrimSpace(req.DefinitionID)]; definition != nil {
		value := *definition
		return &value, nil
	}
	return nil, core.ErrNotFound
}

func (p *workflowSystemToolRecordingProvider) UpdateDefinition(_ context.Context, req coreworkflow.UpdateDefinitionRequest) (*coreworkflow.Definition, error) {
	if p.definitions == nil {
		p.definitions = map[string]*coreworkflow.Definition{}
	}
	id := strings.TrimSpace(req.DefinitionID)
	existing, ok := p.definitions[id]
	if !ok {
		return nil, core.ErrNotFound
	}
	definition := &coreworkflow.Definition{
		ID:        id,
		Target:    req.Target,
		CreatedBy: existing.CreatedBy,
	}
	p.definitions[id] = definition
	value := *definition
	return &value, nil
}

func (p *workflowSystemToolRecordingProvider) DeleteDefinition(_ context.Context, req coreworkflow.DeleteDefinitionRequest) error {
	if p.definitions == nil {
		return core.ErrNotFound
	}
	id := strings.TrimSpace(req.DefinitionID)
	if p.definitions[id] == nil {
		return core.ErrNotFound
	}
	delete(p.definitions, id)
	return nil
}

func (p *workflowSystemToolRecordingProvider) StartRun(_ context.Context, req coreworkflow.StartRunRequest) (*coreworkflow.Run, error) {
	if p.runs == nil {
		p.runs = map[string]*coreworkflow.Run{}
	}
	if p.runIdempotency == nil {
		p.runIdempotency = map[string]string{}
	}
	if req.IdempotencyKey != "" {
		for runID, idempotencyKey := range p.runIdempotency {
			if idempotencyKey == req.IdempotencyKey {
				run := p.runs[runID]
				if run.DefinitionID != req.DefinitionID {
					return nil, errors.New("idempotent run replay used a different definition id")
				}
				value := *run
				return &value, nil
			}
		}
	}
	p.startedRuns = append(p.startedRuns, req)
	runID := "run-" + req.WorkflowKey
	if runID == "run-" {
		runID = fmt.Sprintf("run-%d", len(p.runs)+1)
	}
	run := &coreworkflow.Run{
		ID:           runID,
		Status:       coreworkflow.RunStatusPending,
		WorkflowKey:  req.WorkflowKey,
		Target:       req.Target,
		DefinitionID: req.DefinitionID,
		CreatedBy:    req.CreatedBy,
	}
	p.runs[run.ID] = run
	if req.IdempotencyKey != "" {
		p.runIdempotency[run.ID] = req.IdempotencyKey
	}
	value := *run
	return &value, nil
}
func (p *workflowSystemToolRecordingProvider) GetRun(_ context.Context, req coreworkflow.GetRunRequest) (*coreworkflow.Run, error) {
	if run := p.runs[req.RunID]; run != nil {
		value := *run
		return &value, nil
	}
	return nil, core.ErrNotFound
}
func (p *workflowSystemToolRecordingProvider) ListRuns(_ context.Context, req coreworkflow.ListRunsRequest) (*coreworkflow.ListRunsResponse, error) {
	out := make([]*coreworkflow.Run, 0, len(p.runs))
	ids := make([]string, 0, len(p.runs))
	for id := range p.runs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		run := p.runs[id]
		if req.TargetApp != "" && workflowSystemToolTestTargetFirstApp(run.Target) != req.TargetApp {
			continue
		}
		if req.Status != "" && run.Status != req.Status {
			continue
		}
		value := *run
		out = append(out, &value)
	}
	start := 0
	if req.PageToken != "" {
		parsed, err := strconv.Atoi(req.PageToken)
		if err != nil || parsed < 0 || parsed > len(out) {
			return nil, errors.New("invalid page token")
		}
		start = parsed
	}
	pageSize := req.PageSize
	if pageSize <= 0 || pageSize > len(out) {
		pageSize = len(out)
	}
	end := start + pageSize
	if end > len(out) {
		end = len(out)
	}
	nextPageToken := ""
	if end < len(out) {
		nextPageToken = strconv.Itoa(end)
	}
	return &coreworkflow.ListRunsResponse{Runs: out[start:end], NextPageToken: nextPageToken}, nil
}

func workflowSystemToolTestAppStepTarget(appName, operation string) coreworkflow.Target {
	return coreworkflow.Target{Steps: []coreworkflow.Step{{
		ID: "run",
		App: &coreworkflow.AppCall{
			Name:      appName,
			Operation: operation,
		},
	}}}
}

func workflowSystemToolTestTargetFirstApp(target coreworkflow.Target) string {
	for i := range target.Steps {
		if target.Steps[i].App != nil {
			return strings.TrimSpace(target.Steps[i].App.Name)
		}
	}
	return ""
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
		DefinitionID: req.DefinitionID,
		Paused:       req.Paused,
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
func (p *workflowSystemToolRecordingProvider) PublishEvent(_ context.Context, req coreworkflow.PublishEventRequest) (*coreworkflow.Event, error) {
	return &req.Event, nil
}
func (p *workflowSystemToolRecordingProvider) Ping(context.Context) error { return nil }
func (p *workflowSystemToolRecordingProvider) Close() error               { return nil }

type workflowSystemRunGrantScope struct {
	CallerAppName string
	Permissions   []core.AccessPermission
	ToolRefs      []coreagent.ToolRef
	Tools         []coreagent.Tool
}

func mustMintWorkflowSystemRunGrant(t *testing.T, runtime *agentRuntime, scope workflowSystemRunGrantScope) string {
	t.Helper()

	grants := workflowSystemRunGrants(t, runtime)
	grant, err := grants.Mint(agentgrant.Grant{
		ProviderName:        "managed",
		SessionID:           "session-1",
		TurnID:              "turn-1",
		CallerAppName:       strings.TrimSpace(scope.CallerAppName),
		SubjectID:           principal.UserSubjectID("ada"),
		SubjectKind:         string(principal.KindUser),
		CredentialSubjectID: principal.UserSubjectID("ada"),
		Permissions:         append([]core.AccessPermission(nil), scope.Permissions...),
		ToolRefs:            append([]coreagent.ToolRef(nil), scope.ToolRefs...),
		Tools:               append([]coreagent.Tool(nil), scope.Tools...),
	})
	if err != nil {
		t.Fatalf("Mint workflow system run grant: %v", err)
	}
	return grant
}

func workflowSystemRunGrants(t *testing.T, runtime *agentRuntime) *agentgrant.Manager {
	t.Helper()

	runtime.mu.RLock()
	grants := runtime.runGrants
	runtime.mu.RUnlock()
	if grants == nil {
		t.Fatal("runtime run grants are not configured")
	}
	return grants
}

func workflowSystemToolTestTarget(steps ...map[string]any) map[string]any {
	items := make([]any, 0, len(steps))
	for _, step := range steps {
		items = append(items, step)
	}
	return map[string]any{"steps": items}
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
	tool.ID = mustMintAgentToolID(t, workflowSystemRunGrants(t, runtime), tool.Target)
	return tool
}
