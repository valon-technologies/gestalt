package bootstrap

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
)

func TestWorkflowSystemToolsExposeApplyDefinition(t *testing.T) {
	t.Parallel()

	if _, err := workflowSystemToolFromRef(coreagent.ToolRef{System: coreagent.SystemToolWorkflow, Operation: workflowSystemToolDefinitionsApply}); err != nil {
		t.Fatalf("definitions.apply tool: %v", err)
	}
}

func TestWorkflowSystemToolApplyDefinitionCallsManager(t *testing.T) {
	t.Parallel()

	manager := &recordingWorkflowToolManager{}
	tools := newWorkflowSystemTools(manager, workflowSystemToolAlwaysAvailable{})
	tool := mustWorkflowTool(t, workflowSystemToolDefinitionsApply)
	resp, err := tools.ExecuteSystemTool(context.Background(), agentSystemToolExecutionRequest{
		Principal:      workflowToolPrincipal(),
		CallerKind:     invocation.ProviderKindApp,
		CallerName:     "slack",
		ProviderName:   "agent",
		Tool:           tool,
		ToolRefs:       []coreagent.ToolRef{{App: "github", Operation: "issues.triage"}},
		IdempotencyKey: "apply-1",
		Arguments: map[string]any{
			"definitionId": "triage_issue",
			"provider":     "temporal",
			"target": map[string]any{
				"steps": []any{
					map[string]any{
						"id":  "triage",
						"app": map[string]any{"name": "github", "operation": "issues.triage"},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("ExecuteSystemTool: %v", err)
	}
	if resp.Status != 201 {
		t.Fatalf("status = %d, want 201", resp.Status)
	}
	if manager.applied.Spec.ID != "triage_issue" {
		t.Fatalf("definition id = %q, want triage_issue", manager.applied.Spec.ID)
	}
	if manager.applied.ProviderName != "temporal" {
		t.Fatalf("provider = %q, want temporal", manager.applied.ProviderName)
	}
	if len(manager.applied.Spec.Target.Steps) != 1 || manager.applied.Spec.Target.Steps[0].App == nil {
		t.Fatalf("target = %#v, want one app step", manager.applied.Spec.Target)
	}
	if manager.applied.Caller.Kind != invocation.ProviderKindApp || manager.applied.Caller.Name != "slack" {
		t.Fatalf("caller = %#v, want app/slack", manager.applied.Caller)
	}
}

func TestWorkflowSystemToolStartRunRequiresDefinitionID(t *testing.T) {
	t.Parallel()

	manager := &recordingWorkflowToolManager{
		definition: &workflowmanager.ManagedDefinition{
			ProviderName: "temporal",
			Definition: &coreworkflow.Definition{
				ID: "triage_issue",
				Target: coreworkflow.Target{Steps: []coreworkflow.Step{{
					ID:  "triage",
					App: &coreworkflow.AppCall{Name: "github", Operation: "issues.triage"},
				}}},
				CreatedBySubjectID: "user:ada",
			},
		},
	}
	tools := newWorkflowSystemTools(manager, workflowSystemToolAlwaysAvailable{})
	tool := mustWorkflowTool(t, workflowSystemToolRunsStart)
	resp, err := tools.ExecuteSystemTool(context.Background(), agentSystemToolExecutionRequest{
		Principal:      workflowToolPrincipal(),
		CallerKind:     invocation.ProviderKindApp,
		CallerName:     "slack",
		ProviderName:   "agent",
		Tool:           tool,
		ToolRefs:       []coreagent.ToolRef{{App: "github", Operation: "issues.triage"}},
		IdempotencyKey: "run-1",
		Arguments: map[string]any{
			"definitionId": "triage_issue",
			"provider":     "temporal",
			"workflowKey":  "issue:123",
		},
	})
	if err != nil {
		t.Fatalf("ExecuteSystemTool: %v", err)
	}
	if resp.Status != 201 {
		t.Fatalf("status = %d, want 201", resp.Status)
	}
	if manager.started.DefinitionID != "triage_issue" {
		t.Fatalf("started definition id = %q, want triage_issue", manager.started.DefinitionID)
	}
	if len(manager.started.Input) != 0 {
		t.Fatalf("start input = %#v, want empty", manager.started.Input)
	}
	if manager.started.Caller.Kind != invocation.ProviderKindApp || manager.started.Caller.Name != "slack" {
		t.Fatalf("caller = %#v, want app/slack", manager.started.Caller)
	}
}

func mustWorkflowTool(t *testing.T, operation string) coreagent.Tool {
	t.Helper()
	tool, err := workflowSystemToolFromRef(coreagent.ToolRef{System: coreagent.SystemToolWorkflow, Operation: operation})
	if err != nil {
		t.Fatalf("workflowSystemToolFromRef: %v", err)
	}
	return tool
}

func workflowToolPrincipal() *principal.Principal {
	permissions := principal.CompilePermissions([]core.AccessPermission{{App: "github", Operations: []string{"issues.triage"}}})
	return principal.Canonicalize(&principal.Principal{
		SubjectID:        "user:ada",
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: permissions,
		Scopes:           principal.PermissionApps(permissions),
	})
}

type workflowSystemToolAlwaysAvailable struct{}

func (workflowSystemToolAlwaysAvailable) HasConfiguredProviders() bool { return true }

type recordingWorkflowToolManager struct {
	workflowmanager.Service
	applied    workflowmanager.DefinitionApply
	started    workflowmanager.RunStart
	definition *workflowmanager.ManagedDefinition
}

func (m *recordingWorkflowToolManager) ApplyDefinition(_ context.Context, _ *principal.Principal, req workflowmanager.DefinitionApply) (*workflowmanager.ManagedDefinition, error) {
	m.applied = req
	return &workflowmanager.ManagedDefinition{
		ProviderName: req.ProviderName,
		Definition: &coreworkflow.Definition{
			ID:                 req.Spec.ID,
			Target:             req.Spec.Target,
			Paused:             req.Spec.Paused,
			CreatedBySubjectID: "user:ada",
		},
	}, nil
}

func (m *recordingWorkflowToolManager) GetDefinition(_ context.Context, _ *principal.Principal, definitionID string) (*workflowmanager.ManagedDefinition, error) {
	if m.definition == nil || m.definition.Definition == nil || m.definition.Definition.ID != definitionID {
		return nil, core.ErrNotFound
	}
	return m.definition, nil
}

func (m *recordingWorkflowToolManager) StartRun(_ context.Context, _ *principal.Principal, req workflowmanager.RunStart) (*workflowmanager.ManagedRun, error) {
	m.started = req
	return &workflowmanager.ManagedRun{
		ProviderName: req.ProviderName,
		Run: &coreworkflow.Run{
			ID:                 "run-1",
			Status:             coreworkflow.RunStatusPending,
			DefinitionID:       req.DefinitionID,
			WorkflowKey:        req.WorkflowKey,
			Target:             m.definition.Definition.Target,
			CreatedBySubjectID: "user:ada",
		},
	}, nil
}
