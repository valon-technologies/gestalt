package bootstrap

import (
	"context"
	"strings"

	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/internal/workflowwire"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
)

const (
	workflowSystemToolDefinitionsApply               = "definitions.apply"
	workflowSystemToolDefinitionsGet                 = "definitions.get"
	workflowSystemToolDefinitionsList                = "definitions.list"
	workflowSystemToolDefinitionsSetPaused           = "definitions.setPaused"
	workflowSystemToolDefinitionsSetActivationPaused = "definitions.setActivationPaused"
	workflowSystemToolDefinitionsDelete              = "definitions.delete"
	workflowSystemToolRunsStart                      = "runs.start"
	workflowSystemToolRunsList                       = "runs.list"
	workflowSystemToolRunsGet                        = "runs.get"
)

type workflowSystemToolAvailability interface {
	HasConfiguredProviders() bool
}

type workflowSystemTools struct {
	manager      workflowmanager.Service
	availability workflowSystemToolAvailability
}

type workflowSystemToolDescriptor struct {
	Operation        string
	Name             string
	Description      string
	ParametersSchema map[string]any
}

func newWorkflowSystemTools(manager workflowmanager.Service, availability workflowSystemToolAvailability) *workflowSystemTools {
	return &workflowSystemTools{manager: manager, availability: availability}
}

var workflowSystemToolDescriptors = map[string]workflowSystemToolDescriptor{
	workflowSystemToolDefinitionsApply: {
		Operation:        workflowSystemToolDefinitionsApply,
		Name:             "workflow_definitions_apply",
		Description:      "Create or replace a reusable workflow definition from explicit workflow steps and activations.",
		ParametersSchema: workflowSystemToolApplyDefinitionSchema(),
	},
	workflowSystemToolDefinitionsGet: {
		Operation:        workflowSystemToolDefinitionsGet,
		Name:             "workflow_definitions_get",
		Description:      "Get a workflow definition owned by the current caller.",
		ParametersSchema: workflowSystemToolObjectSchema([]string{"definitionId"}, map[string]any{"definitionId": workflowSystemToolStringSchema("Workflow definition ID.")}),
	},
	workflowSystemToolDefinitionsList: {
		Operation:        workflowSystemToolDefinitionsList,
		Name:             "workflow_definitions_list",
		Description:      "List workflow definitions owned by the current caller.",
		ParametersSchema: workflowSystemToolObjectSchema(nil, nil),
	},
	workflowSystemToolDefinitionsSetPaused: {
		Operation:        workflowSystemToolDefinitionsSetPaused,
		Name:             "workflow_definitions_set_paused",
		Description:      "Set the paused state for a workflow definition owned by the current caller.",
		ParametersSchema: workflowSystemToolObjectSchema([]string{"definitionId"}, map[string]any{"definitionId": workflowSystemToolStringSchema("Workflow definition ID."), "paused": map[string]any{"type": "boolean"}}),
	},
	workflowSystemToolDefinitionsSetActivationPaused: {
		Operation:        workflowSystemToolDefinitionsSetActivationPaused,
		Name:             "workflow_definitions_set_activation_paused",
		Description:      "Set the paused state for one activation on a workflow definition owned by the current caller.",
		ParametersSchema: workflowSystemToolObjectSchema([]string{"definitionId", "activationId"}, map[string]any{"definitionId": workflowSystemToolStringSchema("Workflow definition ID."), "activationId": workflowSystemToolStringSchema("Workflow activation ID."), "paused": map[string]any{"type": "boolean"}}),
	},
	workflowSystemToolDefinitionsDelete: {
		Operation:        workflowSystemToolDefinitionsDelete,
		Name:             "workflow_definitions_delete",
		Description:      "Delete a workflow definition owned by the current caller.",
		ParametersSchema: workflowSystemToolObjectSchema([]string{"definitionId"}, map[string]any{"definitionId": workflowSystemToolStringSchema("Workflow definition ID.")}),
	},
	workflowSystemToolRunsStart: {
		Operation:        workflowSystemToolRunsStart,
		Name:             "workflow_runs_start",
		Description:      "Start a one-off workflow run from an existing workflow definition.",
		ParametersSchema: workflowSystemToolStartRunSchema(),
	},
	workflowSystemToolRunsList: {
		Operation:        workflowSystemToolRunsList,
		Name:             "workflow_runs_list",
		Description:      "List workflow runs owned by the current caller.",
		ParametersSchema: workflowSystemToolListRunsSchema(),
	},
	workflowSystemToolRunsGet: {
		Operation:        workflowSystemToolRunsGet,
		Name:             "workflow_runs_get",
		Description:      "Get a workflow run owned by the current caller.",
		ParametersSchema: workflowSystemToolObjectSchema([]string{"runId"}, map[string]any{"runId": workflowSystemToolStringSchema("Run ID.")}),
	},
}

func (t *workflowSystemTools) Available() bool {
	return t != nil && t.manager != nil && t.availability != nil && t.availability.HasConfiguredProviders()
}

func (t *workflowSystemTools) ResolveTool(ctx context.Context, _ *principal.Principal, ref coreagent.ToolRef) (coreagent.Tool, error) {
	if !t.Available() {
		return coreagent.Tool{}, agentmanager.ErrAgentWorkflowToolsNotConfigured
	}
	return workflowSystemToolFromRef(ref)
}

func (t *workflowSystemTools) ResolveTools(ctx context.Context, p *principal.Principal, refs []coreagent.ToolRef) ([]coreagent.Tool, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	if !t.Available() {
		return nil, agentmanager.ErrAgentWorkflowToolsNotConfigured
	}
	out := make([]coreagent.Tool, 0, len(refs))
	seen := map[string]struct{}{}
	for i := range refs {
		tool, err := workflowSystemToolFromRef(refs[i])
		if err != nil {
			return nil, err
		}
		if _, ok := seen[tool.ID]; ok {
			continue
		}
		seen[tool.ID] = struct{}{}
		out = append(out, tool)
	}
	return out, nil
}

func (t *workflowSystemTools) AllowTool(ctx context.Context, p *principal.Principal, tool coreagent.Tool) bool {
	if !t.Available() {
		return false
	}
	if strings.TrimSpace(tool.Target.System) != coreagent.SystemToolWorkflow {
		return false
	}
	_, ok := workflowSystemToolDescriptors[strings.TrimSpace(tool.Target.Operation)]
	return ok
}

func workflowSystemToolMapDeepClone(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	return workflowwire.CloneJSON(value).(map[string]any)
}

var _ agentmanager.WorkflowSystemTools = (*workflowSystemTools)(nil)
