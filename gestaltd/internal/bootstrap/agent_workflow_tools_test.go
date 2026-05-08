package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/services/agents/agentgrant"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/plugins/registry"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
)

func TestAgentRuntimeWorkflowSystemToolCreatesScopedSchedule(t *testing.T) {
	t.Parallel()

	runtime, workflowProvider := newWorkflowSystemToolRuntime(t)
	workflowTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolSchedulesCreate)
	runGrant := mustMintWorkflowSystemRunGrant(t, runtime, workflowSystemRunGrantScope{
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
		RunGrant:     runGrant,
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

func TestAgentRuntimeWorkflowSystemToolStartsRunWithInheritedOutputDelivery(t *testing.T) {
	t.Parallel()

	agentRuntime, workflowProvider := newWorkflowSystemToolRuntime(t)
	workflowTool := mustWorkflowSystemTool(t, agentRuntime, workflowSystemToolRunsStart)
	runGrant := mustMintWorkflowSystemRunGrant(t, agentRuntime, workflowSystemRunGrantScope{
		CallerPluginName: "slack",
		Permissions: []core.AccessPermission{
			{Plugin: "managed"},
			{Plugin: "roadmap", Operations: []string{"sync"}},
			{Plugin: "notification", Operations: []string{"reply"}},
		},
		ToolRefs: []coreagent.ToolRef{
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolRunsStart},
			{Plugin: "roadmap", Operation: "sync"},
		},
		Tools: []coreagent.Tool{workflowTool},
		InheritedOutputDelivery: &coreworkflow.OutputDelivery{
			Target: coreworkflow.PluginTarget{
				PluginName: "notification",
				Operation:  "reply",
				Input:      map[string]any{"format": "plain"},
			},
			CredentialMode: core.ConnectionModeNone,
			InputBindings: []coreworkflow.OutputBinding{
				{InputField: "text", Value: coreworkflow.OutputValueSource{AgentOutput: "text"}},
				{InputField: "reply_ref", Value: coreworkflow.OutputValueSource{Literal: "signed-parent-reply-ref"}},
			},
		},
	})

	req := coreagent.ExecuteToolRequest{
		ProviderName: "managed",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolCallID:   "call-run-1",
		ToolID:       workflowTool.ID,
		RunGrant:     runGrant,
		Arguments: map[string]any{
			"workflowKey": "slack-child-run",
			"target": map[string]any{
				"agent": map[string]any{
					"provider": "managed",
					"prompt":   "Investigate the deployment and summarize the result.",
					"toolRefs": []any{map[string]any{"plugin": "roadmap", "operation": "sync"}},
				},
			},
			"deliverResultToCaller": true,
		},
	}
	resp, err := agentRuntime.ExecuteTool(context.Background(), req)
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	if resp == nil || resp.Status != http.StatusCreated {
		t.Fatalf("response = %#v, want 201", resp)
	}
	if strings.Contains(resp.Body, "outputDelivery") || strings.Contains(resp.Body, "signed-parent-reply-ref") || strings.Contains(resp.Body, "notification") {
		t.Fatalf("response leaked inherited delivery: %s", resp.Body)
	}
	var body struct {
		Run struct {
			ID          string `json:"id"`
			WorkflowKey string `json:"workflowKey"`
			Target      struct {
				Agent struct {
					Prompt string `json:"prompt"`
				} `json:"agent"`
			} `json:"target"`
		} `json:"run"`
	}
	if err := json.Unmarshal([]byte(resp.Body), &body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	if body.Run.ID == "" || body.Run.WorkflowKey != "slack-child-run" || body.Run.Target.Agent.Prompt == "" {
		t.Fatalf("run response = %#v", body.Run)
	}
	if len(workflowProvider.startedRuns) != 1 {
		t.Fatalf("started runs = %d, want 1", len(workflowProvider.startedRuns))
	}
	started := workflowProvider.startedRuns[0]
	if started.Target.Agent == nil || started.Target.Agent.OutputDelivery == nil {
		t.Fatalf("started target missing output delivery: %#v", started.Target)
	}
	if got := started.Target.Agent.OutputDelivery.InputBindings[1].Value.Literal; got != "signed-parent-reply-ref" {
		t.Fatalf("inherited reply ref = %#v, want signed-parent-reply-ref", got)
	}
	ref, err := workflowProvider.GetExecutionReference(context.Background(), started.ExecutionRef)
	if err != nil {
		t.Fatalf("GetExecutionReference: %v", err)
	}
	assertWorkflowSystemPermissions(t, ref.Permissions, []core.AccessPermission{
		{Plugin: "managed"},
		{Plugin: "notification", Operations: []string{"reply"}},
		{Plugin: "roadmap", Operations: []string{"sync"}},
	})
	replayResp, err := agentRuntime.ExecuteTool(context.Background(), req)
	if err != nil {
		t.Fatalf("ExecuteTool replay: %v", err)
	}
	var replayBody struct {
		Run struct {
			ID string `json:"id"`
		} `json:"run"`
	}
	if err := json.Unmarshal([]byte(replayResp.Body), &replayBody); err != nil {
		t.Fatalf("decode replay body: %v", err)
	}
	if replayBody.Run.ID != body.Run.ID || len(workflowProvider.startedRuns) != 1 {
		t.Fatalf("replay run id = %q startedRuns=%d, want %q and one provider start", replayBody.Run.ID, len(workflowProvider.startedRuns), body.Run.ID)
	}
	conflictingReq := req
	conflictingReq.Arguments = maps.Clone(req.Arguments)
	conflictingReq.Arguments["workflowKey"] = "different-slack-child-run"
	_, err = agentRuntime.ExecuteTool(context.Background(), conflictingReq)
	if err == nil {
		t.Fatal("ExecuteTool conflicting workflow key replay succeeded, want provider idempotency conflict")
	}

	childAgentManager := &workflowRuntimeAgentManagerStub{}
	childRuntime := &workflowRuntime{}
	childRuntime.SetAgentManager(childAgentManager)
	var gotDeliveryParams map[string]any
	childRuntime.SetInvoker(funcInvoker{
		invoke: func(_ context.Context, _ *principal.Principal, providerName, _ string, operation string, params map[string]any) (*core.OperationResult, error) {
			if providerName != "notification" || operation != "reply" {
				t.Fatalf("delivery target = %s.%s, want notification.reply", providerName, operation)
			}
			gotDeliveryParams = maps.Clone(params)
			return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
		},
	})
	p := principal.Canonicalize(&principal.Principal{
		SubjectID: principal.UserSubjectID("ada"),
		TokenPermissions: principal.CompilePermissions([]core.AccessPermission{
			{Plugin: "notification", Operations: []string{"reply"}},
		}),
	})
	childResp, err := childRuntime.Invoke(principal.WithPrincipal(context.Background(), p), coreworkflow.InvokeOperationRequest{
		ProviderName: "temporal",
		RunID:        "child-run",
		Target:       started.Target,
	})
	if err != nil {
		t.Fatalf("Invoke child: %v", err)
	}
	if childResp.Status != http.StatusOK {
		t.Fatalf("child response = %#v", childResp)
	}
	if gotDeliveryParams["text"] != "turn completed" || gotDeliveryParams["reply_ref"] != "signed-parent-reply-ref" || gotDeliveryParams["format"] != "plain" {
		t.Fatalf("child delivery params = %#v", gotDeliveryParams)
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
	if _, ok := targetTargetProps["plugin"]; ok {
		t.Fatalf("runs.start target schema exposes plugin target: %#v", targetTargetProps)
	}
	if _, ok := targetTargetProps["agent"]; !ok {
		t.Fatalf("runs.start target schema missing agent target: %#v", targetTargetProps)
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
	if _, ok := definitionProps["deliverResultToCaller"]; ok {
		t.Fatalf("definition branch exposes deliverResultToCaller: %#v", definitionProps)
	}
}

func TestAgentRuntimeWorkflowSystemToolStartRunRejectsInvalidCallerDelivery(t *testing.T) {
	t.Parallel()

	agentRuntime, _ := newWorkflowSystemToolRuntime(t)
	workflowTool := mustWorkflowSystemTool(t, agentRuntime, workflowSystemToolRunsStart)
	runGrant := mustMintWorkflowSystemRunGrant(t, agentRuntime, workflowSystemRunGrantScope{
		Permissions: []core.AccessPermission{{Plugin: "roadmap", Operations: []string{"sync"}}},
		ToolRefs: []coreagent.ToolRef{
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolRunsStart},
			{Plugin: "roadmap", Operation: "sync"},
		},
		Tools: []coreagent.Tool{workflowTool},
	})
	baseReq := coreagent.ExecuteToolRequest{
		ProviderName: "managed",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolCallID:   "call-run-invalid",
		ToolID:       workflowTool.ID,
		RunGrant:     runGrant,
	}
	tests := []struct {
		name string
		args map[string]any
	}{
		{
			name: "missing inherited output delivery",
			args: map[string]any{
				"deliverResultToCaller": true,
				"target":                map[string]any{"agent": map[string]any{"prompt": "run", "toolRefs": []any{map[string]any{"plugin": "roadmap", "operation": "sync"}}}},
			},
		},
		{
			name: "definition callback",
			args: map[string]any{
				"definitionId":          "workflow_definition_abc",
				"deliverResultToCaller": true,
			},
		},
		{
			name: "direct plugin target",
			args: map[string]any{
				"target": map[string]any{"plugin": map[string]any{"name": "roadmap", "operation": "sync"}},
			},
		},
		{
			name: "non boolean callback flag",
			args: map[string]any{
				"deliverResultToCaller": "true",
				"target":                map[string]any{"agent": map[string]any{"prompt": "run", "toolRefs": []any{map[string]any{"plugin": "roadmap", "operation": "sync"}}}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := baseReq
			req.Arguments = tt.args
			_, err := agentRuntime.ExecuteTool(context.Background(), req)
			if err == nil {
				t.Fatal("ExecuteTool succeeded, want invalid invocation")
			}
			if !errors.Is(err, invocation.ErrInvalidInvocation) {
				t.Fatalf("ExecuteTool error = %v, want invalid invocation", err)
			}
		})
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
			{Plugin: "roadmap", Operations: []string{"sync"}},
			{Plugin: "linear", Operations: []string{"viewer"}},
		},
		ToolRefs: []coreagent.ToolRef{
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolDefinitionsCreate},
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolSchedulesCreate},
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolRunsStart},
			{Plugin: "roadmap", Operation: "sync"},
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
			"target": map[string]any{
				"agent": map[string]any{
					"provider": "managed",
					"prompt":   "Sync the roadmap and open the needed code changes.",
					"toolRefs": []any{
						map[string]any{"plugin": "roadmap", "operation": "sync"},
						map[string]any{"system": coreagent.SystemToolWorkflow, "operation": workflowSystemToolSchedulesCreate},
					},
				},
			},
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
				Agent struct {
					Provider string `json:"provider"`
					Prompt   string `json:"prompt"`
				} `json:"agent"`
			} `json:"target"`
		} `json:"definition"`
	}
	if err := json.Unmarshal([]byte(definitionResp.Body), &definitionBody); err != nil {
		t.Fatalf("decode definition response body: %v", err)
	}
	definitionID := definitionBody.Definition.ID
	if definitionID == "" || definitionBody.Definition.Provider != "temporal" || definitionBody.Definition.Target.Agent.Provider != "managed" {
		t.Fatalf("definition response = %#v", definitionBody.Definition)
	}
	definitionRef, err := workflowProvider.GetExecutionReference(context.Background(), definitionID)
	if err != nil {
		t.Fatalf("GetExecutionReference(definition): %v", err)
	}
	assertWorkflowSystemPermissions(t, definitionRef.Permissions, []core.AccessPermission{
		{Plugin: "managed"},
		{Plugin: "roadmap", Operations: []string{"sync"}},
	})

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
			ID                 string `json:"id"`
			SourceDefinitionID string `json:"sourceDefinitionId"`
			Target             struct {
				Agent struct {
					Provider string `json:"provider"`
				} `json:"agent"`
			} `json:"target"`
		} `json:"schedule"`
	}
	if err := json.Unmarshal([]byte(scheduleResp.Body), &scheduleBody); err != nil {
		t.Fatalf("decode schedule response body: %v", err)
	}
	if scheduleBody.Schedule.ID == "" || scheduleBody.Schedule.SourceDefinitionID != definitionID || scheduleBody.Schedule.Target.Agent.Provider != "managed" {
		t.Fatalf("schedule response = %#v", scheduleBody.Schedule)
	}
	if len(workflowProvider.upsertedSchedules) != 1 {
		t.Fatalf("upserted schedules = %d, want 1", len(workflowProvider.upsertedSchedules))
	}
	upsert := workflowProvider.upsertedSchedules[0]
	scheduleRef, err := workflowProvider.GetExecutionReference(context.Background(), upsert.ExecutionRef)
	if err != nil {
		t.Fatalf("GetExecutionReference(schedule): %v", err)
	}
	if scheduleRef.SourceDefinitionID != definitionID {
		t.Fatalf("schedule ref source definition id = %q, want %q", scheduleRef.SourceDefinitionID, definitionID)
	}
	assertWorkflowSystemPermissions(t, scheduleRef.Permissions, []core.AccessPermission{
		{Plugin: "managed"},
		{Plugin: "roadmap", Operations: []string{"sync"}},
	})

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
	runRef, err := workflowProvider.GetExecutionReference(context.Background(), workflowProvider.startedRuns[0].ExecutionRef)
	if err != nil {
		t.Fatalf("GetExecutionReference(run): %v", err)
	}
	if runRef.SourceDefinitionID != definitionID {
		t.Fatalf("run ref source definition id = %q, want %q", runRef.SourceDefinitionID, definitionID)
	}
	assertWorkflowSystemPermissions(t, runRef.Permissions, []core.AccessPermission{
		{Plugin: "managed"},
		{Plugin: "roadmap", Operations: []string{"sync"}},
	})
}

func TestAgentRuntimeWorkflowSystemToolCreatesDefinitionWithInheritedAgentToolRefs(t *testing.T) {
	t.Parallel()

	runtime, workflowProvider := newWorkflowSystemToolRuntime(t)
	definitionTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolDefinitionsCreate)
	scheduleTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolSchedulesCreate)
	runGrant := mustMintWorkflowSystemRunGrant(t, runtime, workflowSystemRunGrantScope{
		Permissions: []core.AccessPermission{
			{Plugin: "roadmap", Operations: []string{"sync"}},
			{Plugin: "linear", Operations: []string{"viewer"}},
			{Plugin: "slack", Operations: []string{"chat.postMessage"}},
		},
		ToolRefs: []coreagent.ToolRef{
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolDefinitionsCreate},
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolSchedulesCreate},
			{Plugin: "roadmap", Operation: "sync"},
			{Plugin: "linear", Operation: "viewer"},
			{Plugin: "*"},
		},
		Tools: []coreagent.Tool{
			definitionTool,
			scheduleTool,
			{Target: coreagent.ToolTarget{Plugin: "slack", Operation: "chat.postMessage"}},
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
			"target": map[string]any{
				"agent": map[string]any{
					"provider": "managed",
					"prompt":   "Sync the roadmap and post an update.",
				},
			},
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
	ref, err := workflowProvider.GetExecutionReference(context.Background(), body.Definition.ID)
	if err != nil {
		t.Fatalf("GetExecutionReference(definition): %v", err)
	}
	if ref.Target.Agent == nil {
		t.Fatalf("definition target = %#v, want agent", ref.Target)
	}
	wantRefs := []coreagent.ToolRef{
		{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolDefinitionsCreate},
		{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolSchedulesCreate},
		{Plugin: "roadmap", Operation: "sync"},
		{Plugin: "linear", Operation: "viewer"},
		{Plugin: "slack", Operation: "chat.postMessage"},
	}
	if !reflect.DeepEqual(ref.Target.Agent.ToolRefs, wantRefs) {
		t.Fatalf("inherited tool refs = %#v, want %#v", ref.Target.Agent.ToolRefs, wantRefs)
	}
	assertWorkflowSystemPermissions(t, ref.Permissions, []core.AccessPermission{
		{Plugin: "linear", Operations: []string{"viewer"}},
		{Plugin: "managed"},
		{Plugin: "roadmap", Operations: []string{"sync"}},
		{Plugin: "slack", Operations: []string{"chat.postMessage"}},
	})
}

func TestAgentRuntimeWorkflowSystemToolCreatesScheduleWithInheritedAgentToolRefs(t *testing.T) {
	t.Parallel()

	runtime, workflowProvider := newWorkflowSystemToolRuntime(t)
	workflowTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolSchedulesCreate)
	runGrant := mustMintWorkflowSystemRunGrant(t, runtime, workflowSystemRunGrantScope{
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
			"target": map[string]any{
				"agent": map[string]any{
					"provider": "managed",
					"prompt":   "Sync the roadmap.",
				},
			},
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
	if upsert.Target.Agent == nil {
		t.Fatalf("schedule target = %#v, want agent", upsert.Target)
	}
	wantRefs := []coreagent.ToolRef{
		{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolSchedulesCreate},
		{Plugin: "roadmap", Operation: "sync"},
	}
	if !reflect.DeepEqual(upsert.Target.Agent.ToolRefs, wantRefs) {
		t.Fatalf("inherited tool refs = %#v, want %#v", upsert.Target.Agent.ToolRefs, wantRefs)
	}
}

func TestAgentRuntimeWorkflowSystemToolCreatesScheduleWithDelegatedCallerToolRefs(t *testing.T) {
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
		CallerPluginName: "slack",
		Permissions: []core.AccessPermission{{
			Plugin:     "github",
			Operations: []string{"bot.createPullRequest"},
		}},
		ToolRefs: []coreagent.ToolRef{
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolSchedulesCreate},
			{
				Plugin:                "github",
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
			"target": map[string]any{
				"agent": map[string]any{
					"provider": "managed",
					"prompt":   "Open a GitHub pull request.",
				},
			},
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
	if upsert.Target.Agent == nil {
		t.Fatalf("schedule target = %#v, want agent", upsert.Target)
	}
	wantRefs := []coreagent.ToolRef{
		{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolSchedulesCreate},
		{Plugin: "github", Operation: "bot.createPullRequest"},
	}
	if !reflect.DeepEqual(upsert.Target.Agent.ToolRefs, wantRefs) {
		t.Fatalf("inherited tool refs = %#v, want %#v", upsert.Target.Agent.ToolRefs, wantRefs)
	}
	ref, err := workflowProvider.GetExecutionReference(context.Background(), upsert.ExecutionRef)
	if err != nil {
		t.Fatalf("GetExecutionReference(schedule): %v", err)
	}
	if ref.CallerPluginName != "slack" {
		t.Fatalf("schedule caller plugin = %q, want slack", ref.CallerPluginName)
	}
}

func TestAgentRuntimeWorkflowSystemToolCreatesScheduleWithExplicitEmptyAgentToolRefs(t *testing.T) {
	t.Parallel()

	runtime, workflowProvider := newWorkflowSystemToolRuntime(t)
	workflowTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolSchedulesCreate)
	runGrant := mustMintWorkflowSystemRunGrant(t, runtime, workflowSystemRunGrantScope{
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
			"target": map[string]any{
				"agent": map[string]any{
					"provider": "managed",
					"prompt":   "Run without tools.",
					"toolRefs": []any{},
				},
			},
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
	if upsert.Target.Agent == nil {
		t.Fatalf("schedule target = %#v, want agent", upsert.Target)
	}
	if len(upsert.Target.Agent.ToolRefs) != 0 {
		t.Fatalf("explicit empty tool refs = %#v, want empty slice", upsert.Target.Agent.ToolRefs)
	}
	ref, err := workflowProvider.GetExecutionReference(context.Background(), upsert.ExecutionRef)
	if err != nil {
		t.Fatalf("GetExecutionReference: %v", err)
	}
	assertWorkflowSystemPermissions(t, ref.Permissions, []core.AccessPermission{{Plugin: "managed"}})
}

func TestAgentRuntimeWorkflowSystemToolUpdatesAndDeletesDefinition(t *testing.T) {
	t.Parallel()

	runtime, workflowProvider := newWorkflowSystemToolRuntime(t)
	createTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolDefinitionsCreate)
	getTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolDefinitionsGet)
	updateTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolDefinitionsUpdate)
	deleteTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolDefinitionsDelete)
	runGrant := mustMintWorkflowSystemRunGrant(t, runtime, workflowSystemRunGrantScope{
		Permissions: []core.AccessPermission{{Plugin: "roadmap", Operations: []string{"sync"}}},
		ToolRefs: []coreagent.ToolRef{
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolDefinitionsCreate},
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolDefinitionsGet},
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolDefinitionsUpdate},
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolDefinitionsDelete},
			{Plugin: "roadmap", Operation: "sync"},
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
			"target": map[string]any{
				"agent": map[string]any{
					"provider": "managed",
					"prompt":   "Sync the roadmap.",
				},
			},
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
			"target": map[string]any{
				"agent": map[string]any{
					"provider": "managed",
					"prompt":   "Sync the roadmap and summarize changes.",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTool update definition: %v", err)
	}
	if updateResp == nil || updateResp.Status != http.StatusOK {
		t.Fatalf("update definition response = %#v, want 200", updateResp)
	}
	ref, err := workflowProvider.GetExecutionReference(context.Background(), definitionID)
	if err != nil {
		t.Fatalf("GetExecutionReference(definition): %v", err)
	}
	if ref.Target.Agent == nil || ref.Target.Agent.Prompt != "Sync the roadmap and summarize changes." {
		t.Fatalf("definition target = %#v", ref.Target)
	}
	wantRefs := []coreagent.ToolRef{
		{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolDefinitionsCreate},
		{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolDefinitionsGet},
		{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolDefinitionsUpdate},
		{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolDefinitionsDelete},
		{Plugin: "roadmap", Operation: "sync"},
	}
	if !reflect.DeepEqual(ref.Target.Agent.ToolRefs, wantRefs) {
		t.Fatalf("updated definition inherited tool refs = %#v, want %#v", ref.Target.Agent.ToolRefs, wantRefs)
	}
	assertWorkflowSystemPermissions(t, ref.Permissions, []core.AccessPermission{
		{Plugin: "managed"},
		{Plugin: "roadmap", Operations: []string{"sync"}},
	})

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
		Permissions: []core.AccessPermission{{Plugin: "roadmap", Operations: []string{"sync"}}},
		ToolRefs: []coreagent.ToolRef{
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolDefinitionsCreate},
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolSchedulesCreate},
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolSchedulesGet},
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolSchedulesUpdate},
			{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolSchedulesDelete},
			{Plugin: "roadmap", Operation: "sync"},
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
			"target": map[string]any{
				"agent": map[string]any{
					"provider": "managed",
					"prompt":   "Sync the roadmap.",
				},
			},
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
			ID                 string `json:"id"`
			SourceDefinitionID string `json:"sourceDefinitionId"`
		} `json:"schedule"`
	}
	if err := json.Unmarshal([]byte(createResp.Body), &createBody); err != nil {
		t.Fatalf("decode create schedule response body: %v", err)
	}
	scheduleID := createBody.Schedule.ID
	if scheduleID == "" || createBody.Schedule.SourceDefinitionID != definitionBody.Definition.ID {
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
			ID                 string `json:"id"`
			Cron               string `json:"cron"`
			Paused             bool   `json:"paused"`
			SourceDefinitionID string `json:"sourceDefinitionId"`
		} `json:"schedule"`
	}
	if err := json.Unmarshal([]byte(updateResp.Body), &updateBody); err != nil {
		t.Fatalf("decode update schedule response body: %v", err)
	}
	if updateBody.Schedule.ID != scheduleID || updateBody.Schedule.Cron != "*/15 * * * *" || !updateBody.Schedule.Paused || updateBody.Schedule.SourceDefinitionID != definitionBody.Definition.ID {
		t.Fatalf("updated schedule = %#v", updateBody.Schedule)
	}
	if len(workflowProvider.upsertedSchedules) != 2 {
		t.Fatalf("upserted schedules = %d, want 2", len(workflowProvider.upsertedSchedules))
	}
	updateUpsert := workflowProvider.upsertedSchedules[1]
	if updateUpsert.Target.Agent == nil || updateUpsert.Target.Agent.Prompt != "Sync the roadmap." {
		t.Fatalf("updated upsert target = %#v", updateUpsert.Target)
	}
	scheduleRef, err := workflowProvider.GetExecutionReference(context.Background(), updateUpsert.ExecutionRef)
	if err != nil {
		t.Fatalf("GetExecutionReference(schedule): %v", err)
	}
	if scheduleRef.SourceDefinitionID != definitionBody.Definition.ID {
		t.Fatalf("schedule source definition id = %q, want %q", scheduleRef.SourceDefinitionID, definitionBody.Definition.ID)
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

func TestAgentRuntimeWorkflowSystemToolRejectsUndelegatedDefinitionTarget(t *testing.T) {
	t.Parallel()

	runtime, _ := newWorkflowSystemToolRuntime(t)
	definitionTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolDefinitionsCreate)
	runGrant := mustMintWorkflowSystemRunGrant(t, runtime, workflowSystemRunGrantScope{
		Permissions: []core.AccessPermission{
			{Plugin: "roadmap", Operations: []string{"sync"}},
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
			name: "plugin target",
			arguments: map[string]any{
				"target": map[string]any{
					"plugin": map[string]any{
						"name":      "roadmap",
						"operation": "sync",
					},
				},
			},
		},
		{
			name: "future system ref",
			arguments: map[string]any{
				"target": map[string]any{
					"agent": map[string]any{
						"provider": "managed",
						"prompt":   "Create another cron.",
						"toolRefs": []any{
							map[string]any{"system": coreagent.SystemToolWorkflow, "operation": workflowSystemToolSchedulesCreate},
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
				"target": map[string]any{
					"plugin": map[string]any{
						"name":      "roadmap",
						"operation": "sync",
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

func TestAgentRuntimeWorkflowSystemToolRejectsUndelegatedScheduleTarget(t *testing.T) {
	t.Parallel()

	runtime, _ := newWorkflowSystemToolRuntime(t)
	workflowTool := mustWorkflowSystemTool(t, runtime, workflowSystemToolSchedulesCreate)
	runGrant := mustMintWorkflowSystemRunGrant(t, runtime, workflowSystemRunGrantScope{
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
		RunGrant:     runGrant,
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
	runGrant := mustMintWorkflowSystemRunGrant(t, runtime, workflowSystemRunGrantScope{
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
	workflowRuntime.SetAgentManager(agentManager)
	workflowManager := workflowmanager.New(workflowmanager.Config{
		Providers:    &reg.Providers,
		Workflow:     workflowRuntime,
		Agent:        runtime,
		AgentManager: agentManager,
		PluginInvokes: map[string][]invocation.PluginInvocationDependency{
			"slack": {
				{Plugin: "notification", Operation: "reply", CredentialMode: core.ConnectionModeNone},
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

type workflowSystemToolRecordingProvider struct {
	startedRuns       []coreworkflow.StartRunRequest
	runs              map[string]*coreworkflow.Run
	runIdempotency    map[string]string
	upsertedSchedules []coreworkflow.UpsertScheduleRequest
	schedules         map[string]*coreworkflow.Schedule
	executionRefs     map[string]*coreworkflow.ExecutionReference
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
				if run.ExecutionRef != req.ExecutionRef {
					return nil, errors.New("idempotent run replay used a different execution ref")
				}
				value := *run
				return &value, nil
			}
		}
	}
	p.startedRuns = append(p.startedRuns, req)
	run := &coreworkflow.Run{
		ID:           "run-" + req.ExecutionRef,
		Status:       coreworkflow.RunStatusPending,
		WorkflowKey:  req.WorkflowKey,
		Target:       req.Target,
		ExecutionRef: req.ExecutionRef,
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
func (p *workflowSystemToolRecordingProvider) ListRuns(context.Context, coreworkflow.ListRunsRequest) ([]*coreworkflow.Run, error) {
	out := make([]*coreworkflow.Run, 0, len(p.runs))
	for _, run := range p.runs {
		value := *run
		out = append(out, &value)
	}
	return out, nil
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

type workflowSystemRunGrantScope struct {
	CallerPluginName        string
	Permissions             []core.AccessPermission
	ToolRefs                []coreagent.ToolRef
	Tools                   []coreagent.Tool
	InheritedOutputDelivery *coreworkflow.OutputDelivery
}

func mustMintWorkflowSystemRunGrant(t *testing.T, runtime *agentRuntime, scope workflowSystemRunGrantScope) string {
	t.Helper()

	grants := workflowSystemRunGrants(t, runtime)
	grant, err := grants.Mint(agentgrant.Grant{
		ProviderName:            "managed",
		SessionID:               "session-1",
		TurnID:                  "turn-1",
		CallerPluginName:        strings.TrimSpace(scope.CallerPluginName),
		SubjectID:               principal.UserSubjectID("ada"),
		SubjectKind:             string(principal.KindUser),
		CredentialSubjectID:     principal.UserSubjectID("ada"),
		Permissions:             append([]core.AccessPermission(nil), scope.Permissions...),
		ToolRefs:                append([]coreagent.ToolRef(nil), scope.ToolRefs...),
		Tools:                   append([]coreagent.Tool(nil), scope.Tools...),
		InheritedOutputDelivery: coreworkflow.CloneOutputDelivery(scope.InheritedOutputDelivery),
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

func assertWorkflowSystemPermissions(t *testing.T, got, want []core.AccessPermission) {
	t.Helper()

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("permissions = %#v, want %#v", got, want)
	}
}
