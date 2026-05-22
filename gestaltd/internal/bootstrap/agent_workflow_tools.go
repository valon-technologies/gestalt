package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/jsonvalue"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
)

const (
	workflowSystemToolDefinitionsCreate = "definitions.create"
	workflowSystemToolDefinitionsGet    = "definitions.get"
	workflowSystemToolDefinitionsUpdate = "definitions.update"
	workflowSystemToolDefinitionsDelete = "definitions.delete"
	workflowSystemToolSchedulesCreate   = "schedules.create"
	workflowSystemToolSchedulesList     = "schedules.list"
	workflowSystemToolSchedulesGet      = "schedules.get"
	workflowSystemToolSchedulesUpdate   = "schedules.update"
	workflowSystemToolSchedulesDelete   = "schedules.delete"
	workflowSystemToolSchedulesPause    = "schedules.pause"
	workflowSystemToolSchedulesResume   = "schedules.resume"
	workflowSystemToolRunsStart         = "runs.start"
	workflowSystemToolRunsList          = "runs.list"
	workflowSystemToolRunsGet           = "runs.get"
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
	workflowSystemToolDefinitionsCreate: {
		Operation:        workflowSystemToolDefinitionsCreate,
		Name:             "workflow_definitions_create",
		Description:      "Create a reusable workflow definition for a delegated target.",
		ParametersSchema: workflowSystemToolCreateDefinitionSchema(),
	},
	workflowSystemToolDefinitionsGet: {
		Operation:        workflowSystemToolDefinitionsGet,
		Name:             "workflow_definitions_get",
		Description:      "Get a workflow definition owned by the current caller.",
		ParametersSchema: workflowSystemToolObjectSchema([]string{"definitionId"}, map[string]any{"definitionId": workflowSystemToolStringSchema("Workflow definition ID.")}),
	},
	workflowSystemToolDefinitionsUpdate: {
		Operation:        workflowSystemToolDefinitionsUpdate,
		Name:             "workflow_definitions_update",
		Description:      "Update a workflow definition owned by the current caller.",
		ParametersSchema: workflowSystemToolUpdateDefinitionSchema(),
	},
	workflowSystemToolDefinitionsDelete: {
		Operation:        workflowSystemToolDefinitionsDelete,
		Name:             "workflow_definitions_delete",
		Description:      "Delete a workflow definition owned by the current caller.",
		ParametersSchema: workflowSystemToolObjectSchema([]string{"definitionId"}, map[string]any{"definitionId": workflowSystemToolStringSchema("Workflow definition ID.")}),
	},
	workflowSystemToolSchedulesCreate: {
		Operation:        workflowSystemToolSchedulesCreate,
		Name:             "workflow_schedules_create",
		Description:      "Create a recurring workflow schedule for a delegated target or workflow definition.",
		ParametersSchema: workflowSystemToolCreateScheduleSchema(),
	},
	workflowSystemToolSchedulesList: {
		Operation:        workflowSystemToolSchedulesList,
		Name:             "workflow_schedules_list",
		Description:      "List workflow schedules owned by the current caller.",
		ParametersSchema: workflowSystemToolObjectSchema(nil, nil),
	},
	workflowSystemToolSchedulesGet: {
		Operation:        workflowSystemToolSchedulesGet,
		Name:             "workflow_schedules_get",
		Description:      "Get a workflow schedule owned by the current caller.",
		ParametersSchema: workflowSystemToolObjectSchema([]string{"scheduleId"}, map[string]any{"scheduleId": workflowSystemToolStringSchema("Schedule ID.")}),
	},
	workflowSystemToolSchedulesUpdate: {
		Operation:        workflowSystemToolSchedulesUpdate,
		Name:             "workflow_schedules_update",
		Description:      "Update a workflow schedule owned by the current caller. Omitted fields preserve the existing schedule values.",
		ParametersSchema: workflowSystemToolUpdateScheduleSchema(),
	},
	workflowSystemToolSchedulesDelete: {
		Operation:        workflowSystemToolSchedulesDelete,
		Name:             "workflow_schedules_delete",
		Description:      "Delete a workflow schedule owned by the current caller.",
		ParametersSchema: workflowSystemToolObjectSchema([]string{"scheduleId"}, map[string]any{"scheduleId": workflowSystemToolStringSchema("Schedule ID.")}),
	},
	workflowSystemToolSchedulesPause: {
		Operation:        workflowSystemToolSchedulesPause,
		Name:             "workflow_schedules_pause",
		Description:      "Pause a workflow schedule owned by the current caller.",
		ParametersSchema: workflowSystemToolObjectSchema([]string{"scheduleId"}, map[string]any{"scheduleId": workflowSystemToolStringSchema("Schedule ID.")}),
	},
	workflowSystemToolSchedulesResume: {
		Operation:        workflowSystemToolSchedulesResume,
		Name:             "workflow_schedules_resume",
		Description:      "Resume a workflow schedule owned by the current caller.",
		ParametersSchema: workflowSystemToolObjectSchema([]string{"scheduleId"}, map[string]any{"scheduleId": workflowSystemToolStringSchema("Schedule ID.")}),
	},
	workflowSystemToolRunsStart: {
		Operation:        workflowSystemToolRunsStart,
		Name:             "workflow_runs_start",
		Description:      "Start a one-off workflow run for a delegated agent target or an existing workflow definition.",
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

func (t *workflowSystemTools) ExecuteSystemTool(ctx context.Context, req agentSystemToolExecutionRequest) (*coreagent.ExecuteToolResponse, error) {
	if !t.Available() {
		return nil, agentmanager.ErrAgentWorkflowToolsNotConfigured
	}
	if strings.TrimSpace(req.Tool.Target.System) != coreagent.SystemToolWorkflow {
		return nil, fmt.Errorf("%w: unsupported agent system tool %q", invocation.ErrInvalidInvocation, req.Tool.Target.System)
	}
	switch strings.TrimSpace(req.Tool.Target.Operation) {
	case workflowSystemToolDefinitionsCreate:
		return t.executeCreateDefinition(ctx, req)
	case workflowSystemToolDefinitionsGet:
		return t.executeGetDefinition(ctx, req)
	case workflowSystemToolDefinitionsUpdate:
		return t.executeUpdateDefinition(ctx, req)
	case workflowSystemToolDefinitionsDelete:
		return t.executeDeleteDefinition(ctx, req)
	case workflowSystemToolSchedulesCreate:
		return t.executeCreateSchedule(ctx, req)
	case workflowSystemToolSchedulesList:
		if err := workflowSystemToolRejectUnknownKeys(req.Arguments, "workflow.schedules.list"); err != nil {
			return nil, err
		}
		schedules, err := t.manager.ListSchedules(ctx, workflowSystemToolManagementPrincipal(req))
		if err != nil {
			return nil, err
		}
		items := make([]map[string]any, 0, len(schedules))
		for _, schedule := range schedules {
			items = append(items, workflowSystemToolScheduleInfo(schedule))
		}
		return workflowSystemToolJSONResponse(http.StatusOK, map[string]any{"schedules": items})
	case workflowSystemToolSchedulesGet:
		if err := workflowSystemToolRejectUnknownKeys(req.Arguments, "workflow.schedules.get", "scheduleId"); err != nil {
			return nil, err
		}
		scheduleID := workflowSystemToolStringArg(req.Arguments, "scheduleId")
		if scheduleID == "" {
			return nil, fmt.Errorf("%w: scheduleId is required", invocation.ErrInvalidInvocation)
		}
		schedule, err := t.manager.GetSchedule(ctx, workflowSystemToolManagementPrincipal(req), scheduleID)
		if err != nil {
			return nil, err
		}
		return workflowSystemToolJSONResponse(http.StatusOK, map[string]any{"schedule": workflowSystemToolScheduleInfo(schedule)})
	case workflowSystemToolSchedulesUpdate:
		return t.executeUpdateSchedule(ctx, req)
	case workflowSystemToolSchedulesDelete:
		return t.executeDeleteSchedule(ctx, req)
	case workflowSystemToolSchedulesPause:
		if err := workflowSystemToolRejectUnknownKeys(req.Arguments, "workflow.schedules.pause", "scheduleId"); err != nil {
			return nil, err
		}
		scheduleID := workflowSystemToolStringArg(req.Arguments, "scheduleId")
		if scheduleID == "" {
			return nil, fmt.Errorf("%w: scheduleId is required", invocation.ErrInvalidInvocation)
		}
		schedule, err := t.manager.PauseSchedule(ctx, workflowSystemToolManagementPrincipal(req), scheduleID)
		if err != nil {
			return nil, err
		}
		return workflowSystemToolJSONResponse(http.StatusOK, map[string]any{"schedule": workflowSystemToolScheduleInfo(schedule)})
	case workflowSystemToolSchedulesResume:
		if err := workflowSystemToolRejectUnknownKeys(req.Arguments, "workflow.schedules.resume", "scheduleId"); err != nil {
			return nil, err
		}
		scheduleID := workflowSystemToolStringArg(req.Arguments, "scheduleId")
		if scheduleID == "" {
			return nil, fmt.Errorf("%w: scheduleId is required", invocation.ErrInvalidInvocation)
		}
		schedule, err := t.manager.ResumeSchedule(ctx, workflowSystemToolManagementPrincipal(req), scheduleID)
		if err != nil {
			return nil, err
		}
		return workflowSystemToolJSONResponse(http.StatusOK, map[string]any{"schedule": workflowSystemToolScheduleInfo(schedule)})
	case workflowSystemToolRunsStart:
		return t.executeStartRun(ctx, req)
	case workflowSystemToolRunsList:
		if err := workflowSystemToolRejectUnknownKeys(req.Arguments, "workflow.runs.list", "pageSize", "pageToken", "app", "status"); err != nil {
			return nil, err
		}
		listReq, err := workflowSystemToolListRunsRequest(req.Arguments)
		if err != nil {
			return nil, err
		}
		resp, err := t.manager.ListRuns(ctx, workflowSystemToolManagementPrincipal(req), listReq)
		if err != nil {
			return nil, err
		}
		if resp == nil {
			resp = &workflowmanager.ListRunsResponse{}
		}
		runs := resp.Runs
		items := make([]map[string]any, 0, len(runs))
		for _, run := range runs {
			items = append(items, workflowSystemRunInfo(run))
		}
		out := map[string]any{"runs": items}
		if token := strings.TrimSpace(resp.NextPageToken); token != "" {
			out["nextPageToken"] = token
		}
		return workflowSystemToolJSONResponse(http.StatusOK, out)
	case workflowSystemToolRunsGet:
		if err := workflowSystemToolRejectUnknownKeys(req.Arguments, "workflow.runs.get", "runId"); err != nil {
			return nil, err
		}
		runID := workflowSystemToolStringArg(req.Arguments, "runId")
		if runID == "" {
			return nil, fmt.Errorf("%w: runId is required", invocation.ErrInvalidInvocation)
		}
		run, err := t.manager.GetRun(ctx, workflowSystemToolManagementPrincipal(req), runID)
		if err != nil {
			return nil, err
		}
		return workflowSystemToolJSONResponse(http.StatusOK, map[string]any{"run": workflowSystemRunInfo(run)})
	default:
		return nil, fmt.Errorf("%w: workflow system operation %q is not supported", invocation.ErrOperationNotFound, req.Tool.Target.Operation)
	}
}

func (t *workflowSystemTools) executeCreateDefinition(ctx context.Context, req agentSystemToolExecutionRequest) (*coreagent.ExecuteToolResponse, error) {
	args := req.Arguments
	if err := workflowSystemToolRejectUnknownKeys(args, "workflow.definitions.create", "provider", "target"); err != nil {
		return nil, err
	}
	targetValue, ok := args["target"]
	if !ok {
		return nil, fmt.Errorf("%w: target is required", invocation.ErrInvalidInvocation)
	}
	target, err := workflowSystemToolTargetFromValue(targetValue)
	if err != nil {
		return nil, err
	}
	workflowSystemToolInheritAgentToolRefs(req, &target)
	if err := workflowSystemToolValidateCreateScope(req, target); err != nil {
		return nil, err
	}
	permissions := workflowSystemToolPermissionsForTarget(target, req.ProviderName)
	scopedPrincipal, err := workflowSystemToolScopedPrincipal(req.Principal, permissions, workflowSystemToolTrustedAgentProvider(req, target))
	if err != nil {
		return nil, err
	}
	definition, err := t.manager.CreateDefinition(ctx, scopedPrincipal, workflowmanager.DefinitionUpsert{
		ProviderName:     workflowSystemToolStringArg(args, "provider"),
		Target:           target,
		IdempotencyKey:   strings.TrimSpace(req.IdempotencyKey),
		CallerAppName: workflowSystemToolCallerScope(req),
	})
	if err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "agent workflow system tool created definition", workflowSystemToolDefinitionLogAttrs(req, definition)...)
	return workflowSystemToolJSONResponse(http.StatusCreated, map[string]any{"definition": workflowSystemToolDefinitionInfo(definition)})
}

func (t *workflowSystemTools) executeGetDefinition(ctx context.Context, req agentSystemToolExecutionRequest) (*coreagent.ExecuteToolResponse, error) {
	args := req.Arguments
	if err := workflowSystemToolRejectUnknownKeys(args, "workflow.definitions.get", "definitionId"); err != nil {
		return nil, err
	}
	definitionID := workflowSystemToolStringArg(args, "definitionId")
	if definitionID == "" {
		return nil, fmt.Errorf("%w: definitionId is required", invocation.ErrInvalidInvocation)
	}
	definition, err := t.manager.GetDefinition(ctx, workflowSystemToolManagementPrincipal(req), definitionID)
	if err != nil {
		return nil, err
	}
	return workflowSystemToolJSONResponse(http.StatusOK, map[string]any{"definition": workflowSystemToolDefinitionInfo(definition)})
}

func (t *workflowSystemTools) executeUpdateDefinition(ctx context.Context, req agentSystemToolExecutionRequest) (*coreagent.ExecuteToolResponse, error) {
	args := req.Arguments
	if err := workflowSystemToolRejectUnknownKeys(args, "workflow.definitions.update", "definitionId", "provider", "target"); err != nil {
		return nil, err
	}
	definitionID := workflowSystemToolStringArg(args, "definitionId")
	if definitionID == "" {
		return nil, fmt.Errorf("%w: definitionId is required", invocation.ErrInvalidInvocation)
	}
	targetValue, ok := args["target"]
	if !ok {
		return nil, fmt.Errorf("%w: target is required", invocation.ErrInvalidInvocation)
	}
	target, err := workflowSystemToolTargetFromValue(targetValue)
	if err != nil {
		return nil, err
	}
	workflowSystemToolInheritAgentToolRefs(req, &target)
	if err := workflowSystemToolValidateCreateScope(req, target); err != nil {
		return nil, err
	}
	permissions, err := workflowSystemToolScopedPermissions(req, target)
	if err != nil {
		return nil, err
	}
	definition, err := t.manager.UpdateDefinition(ctx, workflowSystemToolManagementPrincipal(req), definitionID, workflowmanager.DefinitionUpsert{
		ProviderName:     workflowSystemToolStringArg(args, "provider"),
		Target:           target,
		CallerAppName: workflowSystemToolCallerScope(req),
		Permissions:      permissions,
	})
	if err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "agent workflow system tool updated definition", workflowSystemToolDefinitionLogAttrs(req, definition)...)
	return workflowSystemToolJSONResponse(http.StatusOK, map[string]any{"definition": workflowSystemToolDefinitionInfo(definition)})
}

func (t *workflowSystemTools) executeDeleteDefinition(ctx context.Context, req agentSystemToolExecutionRequest) (*coreagent.ExecuteToolResponse, error) {
	args := req.Arguments
	if err := workflowSystemToolRejectUnknownKeys(args, "workflow.definitions.delete", "definitionId"); err != nil {
		return nil, err
	}
	definitionID := workflowSystemToolStringArg(args, "definitionId")
	if definitionID == "" {
		return nil, fmt.Errorf("%w: definitionId is required", invocation.ErrInvalidInvocation)
	}
	if err := t.manager.DeleteDefinition(ctx, workflowSystemToolManagementPrincipal(req), definitionID); err != nil {
		return nil, err
	}
	attrs := workflowSystemToolBaseLogAttrs(req)
	attrs = append(attrs, "workflow_definition_id", definitionID)
	slog.InfoContext(ctx, "agent workflow system tool deleted definition", attrs...)
	return workflowSystemToolJSONResponse(http.StatusOK, map[string]any{"definitionId": definitionID, "deleted": true})
}

func (t *workflowSystemTools) executeCreateSchedule(ctx context.Context, req agentSystemToolExecutionRequest) (*coreagent.ExecuteToolResponse, error) {
	args := req.Arguments
	if err := workflowSystemToolRejectUnknownKeys(args, "workflow.schedules.create", "provider", "cron", "timezone", "paused", "target", "definitionId"); err != nil {
		return nil, err
	}
	cron := workflowSystemToolStringArg(args, "cron")
	if cron == "" {
		return nil, fmt.Errorf("%w: cron is required", invocation.ErrInvalidInvocation)
	}
	definitionID := workflowSystemToolStringArg(args, "definitionId")
	targetValue, hasTarget := args["target"]
	if definitionID == "" && !hasTarget {
		return nil, fmt.Errorf("%w: target or definitionId is required", invocation.ErrInvalidInvocation)
	}
	if definitionID != "" && hasTarget {
		return nil, fmt.Errorf("%w: target and definitionId cannot both be set", invocation.ErrInvalidInvocation)
	}

	var target coreworkflow.Target
	var scopedPrincipal *principal.Principal
	if definitionID != "" {
		definitionPrincipal := workflowSystemToolPrincipalWithTrustedProvider(req.Principal, strings.TrimSpace(req.ProviderName))
		definition, err := t.manager.GetDefinition(ctx, definitionPrincipal, definitionID)
		if err != nil {
			return nil, err
		}
		if definition == nil || definition.Definition == nil {
			return nil, core.ErrNotFound
		}
		target = definition.Definition.Target
		if err := workflowSystemToolValidateCreateScope(req, target); err != nil {
			return nil, err
		}
		permissions := definition.Definition.Permissions
		if permissions == nil {
			permissions = workflowSystemToolPermissionsForTarget(target, req.ProviderName)
		}
		scopedPrincipal, err = workflowSystemToolExactPermissionsPrincipal(req.Principal, permissions, workflowSystemToolTrustedAgentProvider(req, target))
		if err != nil {
			return nil, err
		}
	} else {
		var err error
		target, err = workflowSystemToolTargetFromValue(targetValue)
		if err != nil {
			return nil, err
		}
		workflowSystemToolInheritAgentToolRefs(req, &target)
		if err := workflowSystemToolValidateCreateScope(req, target); err != nil {
			return nil, err
		}
		permissions := workflowSystemToolPermissionsForTarget(target, req.ProviderName)
		scopedPrincipal, err = workflowSystemToolScopedPrincipal(req.Principal, permissions, workflowSystemToolTrustedAgentProvider(req, target))
		if err != nil {
			return nil, err
		}
	}
	upsertTarget := target
	if definitionID != "" {
		upsertTarget = coreworkflow.Target{}
	}
	schedule, err := t.manager.CreateSchedule(ctx, scopedPrincipal, workflowmanager.ScheduleUpsert{
		ProviderName:     workflowSystemToolStringArg(args, "provider"),
		Cron:             cron,
		Timezone:         workflowSystemToolStringArg(args, "timezone"),
		Target:           upsertTarget,
		DefinitionID:     definitionID,
		Paused:           workflowSystemToolBoolArg(args, "paused"),
		IdempotencyKey:   strings.TrimSpace(req.IdempotencyKey),
		CallerAppName: workflowSystemToolCallerScope(req),
	})
	if err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "agent workflow system tool created schedule", workflowSystemToolScheduleLogAttrs(req, schedule)...)
	return workflowSystemToolJSONResponse(http.StatusCreated, map[string]any{"schedule": workflowSystemToolScheduleInfo(schedule)})
}

func (t *workflowSystemTools) executeStartRun(ctx context.Context, req agentSystemToolExecutionRequest) (*coreagent.ExecuteToolResponse, error) {
	args := req.Arguments
	if err := workflowSystemToolRejectUnknownKeys(args, "workflow.runs.start", "provider", "workflowKey", "target", "definitionId", "deliverResultToCaller"); err != nil {
		return nil, err
	}
	deliverResultToCaller, _, err := workflowSystemToolBoolArgPresent(args, "deliverResultToCaller", "workflow.runs.start")
	if err != nil {
		return nil, err
	}
	definitionID := workflowSystemToolStringArg(args, "definitionId")
	targetValue, hasTarget := args["target"]
	if definitionID == "" && !hasTarget {
		return nil, fmt.Errorf("%w: target or definitionId is required", invocation.ErrInvalidInvocation)
	}
	if definitionID != "" && hasTarget {
		return nil, fmt.Errorf("%w: target and definitionId cannot both be set", invocation.ErrInvalidInvocation)
	}
	if definitionID != "" && deliverResultToCaller {
		return nil, fmt.Errorf("%w: deliverResultToCaller is only supported with direct agent targets", invocation.ErrInvalidInvocation)
	}

	var target coreworkflow.Target
	startTarget := coreworkflow.Target{}
	var scopedPrincipal *principal.Principal
	var permissions []core.AccessPermission
	if definitionID != "" {
		definitionPrincipal := workflowSystemToolPrincipalWithTrustedProvider(req.Principal, strings.TrimSpace(req.ProviderName))
		definition, err := t.manager.GetDefinition(ctx, definitionPrincipal, definitionID)
		if err != nil {
			return nil, err
		}
		if definition == nil || definition.Definition == nil {
			return nil, core.ErrNotFound
		}
		target = definition.Definition.Target
		if err := workflowSystemToolValidateCreateScope(req, target); err != nil {
			return nil, err
		}
		permissions = definition.Definition.Permissions
		if permissions == nil {
			permissions = workflowSystemToolPermissionsForTarget(target, req.ProviderName)
		}
		scopedPrincipal, err = workflowSystemToolExactPermissionsPrincipal(req.Principal, permissions, workflowSystemToolTrustedAgentProvider(req, target))
		if err != nil {
			return nil, err
		}
	} else {
		var err error
		target, err = workflowSystemToolTargetFromValue(targetValue)
		if err != nil {
			return nil, err
		}
		if target.App != nil {
			return nil, fmt.Errorf("%w: workflow.runs.start only supports direct agent targets", invocation.ErrInvalidInvocation)
		}
		workflowSystemToolInheritAgentToolRefs(req, &target)
		if deliverResultToCaller {
			if err := workflowSystemToolApplyInheritedOutputDelivery(req, &target); err != nil {
				return nil, err
			}
		}
		if err := workflowSystemToolValidateCreateScope(req, target); err != nil {
			return nil, err
		}
		permissions = workflowSystemToolPermissionsForTarget(target, req.ProviderName)
		scopedPrincipal, err = workflowSystemToolScopedPrincipal(req.Principal, permissions, workflowSystemToolTrustedAgentProvider(req, target))
		if err != nil {
			return nil, err
		}
		startTarget = target
	}
	run, err := t.manager.StartRun(ctx, scopedPrincipal, workflowmanager.RunStart{
		ProviderName:     workflowSystemToolStringArg(args, "provider"),
		Target:           startTarget,
		DefinitionID:     definitionID,
		IdempotencyKey:   strings.TrimSpace(req.IdempotencyKey),
		WorkflowKey:      workflowSystemToolStringArg(args, "workflowKey"),
		CallerAppName: workflowSystemToolCallerScope(req),
		Permissions:      permissions,
	})
	if err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "agent workflow system tool started run", workflowSystemToolRunLogAttrs(req, run)...)
	return workflowSystemToolJSONResponse(http.StatusCreated, map[string]any{"run": workflowSystemRunInfo(run)})
}

func (t *workflowSystemTools) executeUpdateSchedule(ctx context.Context, req agentSystemToolExecutionRequest) (*coreagent.ExecuteToolResponse, error) {
	args := req.Arguments
	if err := workflowSystemToolRejectUnknownKeys(args, "workflow.schedules.update", "scheduleId", "provider", "cron", "timezone", "paused", "target", "definitionId"); err != nil {
		return nil, err
	}
	scheduleID := workflowSystemToolStringArg(args, "scheduleId")
	if scheduleID == "" {
		return nil, fmt.Errorf("%w: scheduleId is required", invocation.ErrInvalidInvocation)
	}
	existing, err := t.manager.GetSchedule(ctx, workflowSystemToolManagementPrincipal(req), scheduleID)
	if err != nil {
		return nil, err
	}
	if existing == nil || existing.Schedule == nil {
		return nil, core.ErrNotFound
	}

	providerName := workflowSystemToolStringArg(args, "provider")
	cron := workflowSystemToolStringArg(args, "cron")
	if cron == "" {
		cron = strings.TrimSpace(existing.Schedule.Cron)
	}
	if cron == "" {
		return nil, fmt.Errorf("%w: cron is required", invocation.ErrInvalidInvocation)
	}
	timezone := workflowSystemToolStringArg(args, "timezone")
	if timezone == "" {
		timezone = strings.TrimSpace(existing.Schedule.Timezone)
	}
	paused := existing.Schedule.Paused
	if value, ok, err := workflowSystemToolBoolArgPresent(args, "paused", "workflow.schedules.update"); err != nil {
		return nil, err
	} else if ok {
		paused = value
	}

	definitionID := workflowSystemToolStringArg(args, "definitionId")
	targetValue, hasTarget := args["target"]
	if definitionID != "" && hasTarget {
		return nil, fmt.Errorf("%w: target and definitionId cannot both be set", invocation.ErrInvalidInvocation)
	}
	if providerName == "" && definitionID == "" {
		providerName = strings.TrimSpace(existing.ProviderName)
	}

	var target coreworkflow.Target
	var upsertTarget coreworkflow.Target
	var permissions []core.AccessPermission
	sourceDefinitionID := ""
	switch {
	case definitionID != "":
		definitionPrincipal := workflowSystemToolPrincipalWithTrustedProvider(req.Principal, strings.TrimSpace(req.ProviderName))
		definition, err := t.manager.GetDefinition(ctx, definitionPrincipal, definitionID)
		if err != nil {
			return nil, err
		}
		if definition == nil || definition.Definition == nil {
			return nil, core.ErrNotFound
		}
		target = definition.Definition.Target
		if err := workflowSystemToolValidateCreateScope(req, target); err != nil {
			return nil, err
		}
		permissions = definition.Definition.Permissions
		if permissions == nil {
			permissions, err = workflowSystemToolScopedPermissions(req, target)
			if err != nil {
				return nil, err
			}
		} else {
			scopedPrincipal, err := workflowSystemToolExactPermissionsPrincipal(req.Principal, permissions, workflowSystemToolTrustedAgentProvider(req, target))
			if err != nil {
				return nil, err
			}
			permissions = workflowSystemToolPermissionsFromPrincipal(scopedPrincipal)
		}
	case hasTarget:
		target, err = workflowSystemToolTargetFromValue(targetValue)
		if err != nil {
			return nil, err
		}
		workflowSystemToolInheritAgentToolRefs(req, &target)
		if err := workflowSystemToolValidateCreateScope(req, target); err != nil {
			return nil, err
		}
		permissions, err = workflowSystemToolScopedPermissions(req, target)
		if err != nil {
			return nil, err
		}
		upsertTarget = target
	default:
		target = workflowSystemToolExistingScheduleTarget(existing)
		if existing.ExecutionRef != nil {
			sourceDefinitionID = strings.TrimSpace(existing.ExecutionRef.SourceDefinitionID)
		}
		if err := workflowSystemToolValidateCreateScope(req, target); err != nil {
			return nil, err
		}
		if existing.ExecutionRef != nil && existing.ExecutionRef.Permissions != nil {
			permissions = workflowSystemToolClonePermissions(existing.ExecutionRef.Permissions)
		} else {
			permissions, err = workflowSystemToolScopedPermissions(req, target)
			if err != nil {
				return nil, err
			}
		}
		upsertTarget = target
	}
	if definitionID != "" {
		upsertTarget = coreworkflow.Target{}
	}
	schedule, err := t.manager.UpdateSchedule(ctx, workflowSystemToolManagementPrincipal(req), scheduleID, workflowmanager.ScheduleUpsert{
		ProviderName:       providerName,
		Cron:               cron,
		Timezone:           timezone,
		Target:             upsertTarget,
		DefinitionID:       definitionID,
		SourceDefinitionID: sourceDefinitionID,
		Paused:             paused,
		CallerAppName:   workflowSystemToolCallerScope(req),
		Permissions:        permissions,
	})
	if err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "agent workflow system tool updated schedule", workflowSystemToolScheduleLogAttrs(req, schedule)...)
	return workflowSystemToolJSONResponse(http.StatusOK, map[string]any{"schedule": workflowSystemToolScheduleInfo(schedule)})
}

func (t *workflowSystemTools) executeDeleteSchedule(ctx context.Context, req agentSystemToolExecutionRequest) (*coreagent.ExecuteToolResponse, error) {
	args := req.Arguments
	if err := workflowSystemToolRejectUnknownKeys(args, "workflow.schedules.delete", "scheduleId"); err != nil {
		return nil, err
	}
	scheduleID := workflowSystemToolStringArg(args, "scheduleId")
	if scheduleID == "" {
		return nil, fmt.Errorf("%w: scheduleId is required", invocation.ErrInvalidInvocation)
	}
	if err := t.manager.DeleteSchedule(ctx, workflowSystemToolManagementPrincipal(req), scheduleID); err != nil {
		return nil, err
	}
	attrs := workflowSystemToolBaseLogAttrs(req)
	attrs = append(attrs, "workflow_schedule_id", scheduleID)
	slog.InfoContext(ctx, "agent workflow system tool deleted schedule", attrs...)
	return workflowSystemToolJSONResponse(http.StatusOK, map[string]any{"scheduleId": scheduleID, "deleted": true})
}

func workflowSystemToolBaseLogAttrs(req agentSystemToolExecutionRequest) []any {
	attrs := []any{
		"agent_provider", strings.TrimSpace(req.ProviderName),
		"agent_caller_plugin", strings.TrimSpace(req.CallerAppName),
		"agent_session_id", strings.TrimSpace(req.SessionID),
		"agent_turn_id", strings.TrimSpace(req.TurnID),
		"agent_tool_call_id", strings.TrimSpace(req.ToolCallID),
		"agent_tool_id", strings.TrimSpace(req.ToolID),
		"agent_tool_operation", strings.TrimSpace(req.Tool.Target.Operation),
		"agent_tool_idempotency_key", strings.TrimSpace(req.IdempotencyKey),
	}
	if p := principal.Canonicalized(req.Principal); p != nil {
		attrs = append(attrs,
			"subject_id", strings.TrimSpace(p.SubjectID),
			"subject_kind", strings.TrimSpace(string(p.Kind)),
			"credential_subject_id", strings.TrimSpace(p.CredentialSubjectID),
			"auth_source", strings.TrimSpace(p.AuthSource()),
		)
	}
	return attrs
}

func workflowSystemToolDefinitionLogAttrs(req agentSystemToolExecutionRequest, definition *workflowmanager.ManagedDefinition) []any {
	attrs := workflowSystemToolBaseLogAttrs(req)
	if definition == nil {
		return attrs
	}
	attrs = append(attrs, "workflow_provider", strings.TrimSpace(definition.ProviderName))
	if definition.Definition != nil {
		attrs = append(attrs,
			"workflow_definition_id", strings.TrimSpace(definition.Definition.ID),
			"workflow_target", workflowTargetContext(definition.Definition.Target),
			"workflow_caller_plugin", strings.TrimSpace(definition.Definition.CallerAppName),
		)
	}
	return attrs
}

func workflowSystemToolScheduleLogAttrs(req agentSystemToolExecutionRequest, schedule *workflowmanager.ManagedSchedule) []any {
	attrs := workflowSystemToolBaseLogAttrs(req)
	if schedule == nil {
		return attrs
	}
	attrs = append(attrs, "workflow_provider", strings.TrimSpace(schedule.ProviderName))
	if schedule.Schedule != nil {
		attrs = append(attrs,
			"workflow_schedule_id", strings.TrimSpace(schedule.Schedule.ID),
			"workflow_schedule_cron", strings.TrimSpace(schedule.Schedule.Cron),
			"workflow_schedule_timezone", strings.TrimSpace(schedule.Schedule.Timezone),
			"workflow_schedule_paused", schedule.Schedule.Paused,
			"workflow_schedule_execution_ref", strings.TrimSpace(schedule.Schedule.ExecutionRef),
			"workflow_target", workflowTargetContext(schedule.Schedule.Target),
		)
		if schedule.Schedule.NextRunAt != nil {
			attrs = append(attrs, "workflow_schedule_next_run_at", schedule.Schedule.NextRunAt.UTC().Format(time.RFC3339Nano))
		}
	}
	if schedule.ExecutionRef != nil {
		attrs = append(attrs,
			"workflow_execution_ref", strings.TrimSpace(schedule.ExecutionRef.ID),
			"workflow_source_definition_id", strings.TrimSpace(schedule.ExecutionRef.SourceDefinitionID),
			"workflow_caller_plugin", strings.TrimSpace(schedule.ExecutionRef.CallerAppName),
		)
	}
	return attrs
}

func workflowSystemToolRunLogAttrs(req agentSystemToolExecutionRequest, run *workflowmanager.ManagedRun) []any {
	attrs := workflowSystemToolBaseLogAttrs(req)
	if run == nil {
		return attrs
	}
	attrs = append(attrs, "workflow_provider", strings.TrimSpace(run.ProviderName))
	if run.Run != nil {
		attrs = append(attrs,
			"workflow_run_id", strings.TrimSpace(run.Run.ID),
			"workflow_run_status", strings.TrimSpace(string(run.Run.Status)),
			"workflow_run_execution_ref", strings.TrimSpace(run.Run.ExecutionRef),
			"workflow_target", workflowTargetContext(run.Run.Target),
		)
	}
	if run.ExecutionRef != nil {
		attrs = append(attrs,
			"workflow_execution_ref", strings.TrimSpace(run.ExecutionRef.ID),
			"workflow_source_definition_id", strings.TrimSpace(run.ExecutionRef.SourceDefinitionID),
			"workflow_caller_plugin", strings.TrimSpace(run.ExecutionRef.CallerAppName),
		)
	}
	return attrs
}

func workflowSystemToolCallerScope(req agentSystemToolExecutionRequest) string {
	if callerAppName := strings.TrimSpace(req.CallerAppName); callerAppName != "" {
		return callerAppName
	}
	providerName := strings.TrimSpace(req.ProviderName)
	if providerName == "" {
		return "agent"
	}
	return "agent:" + providerName
}

func workflowSystemToolManagementPrincipal(req agentSystemToolExecutionRequest) *principal.Principal {
	return workflowSystemToolPrincipalWithTrustedProvider(req.Principal, strings.TrimSpace(req.ProviderName))
}

func workflowSystemToolFromRef(ref coreagent.ToolRef) (coreagent.Tool, error) {
	systemName := strings.TrimSpace(ref.System)
	if systemName != coreagent.SystemToolWorkflow {
		return coreagent.Tool{}, fmt.Errorf("%w: unsupported agent system tool %q", invocation.ErrInvalidInvocation, systemName)
	}
	operation := strings.TrimSpace(ref.Operation)
	desc, ok := workflowSystemToolDescriptors[operation]
	if !ok {
		return coreagent.Tool{}, fmt.Errorf("%w: workflow system operation %q is not supported", invocation.ErrOperationNotFound, operation)
	}
	name := strings.TrimSpace(ref.Title)
	if name == "" {
		name = desc.Name
	}
	description := strings.TrimSpace(ref.Description)
	if description == "" {
		description = desc.Description
	}
	return coreagent.Tool{
		ID:               "system.workflow." + operation,
		Name:             name,
		Description:      description,
		ParametersSchema: workflowSystemToolMapDeepClone(desc.ParametersSchema),
		Target: coreagent.ToolTarget{
			System:    coreagent.SystemToolWorkflow,
			Operation: operation,
		},
	}, nil
}

func workflowSystemToolCreateScheduleSchema() map[string]any {
	return workflowSystemToolObjectSchema([]string{"cron"}, map[string]any{
		"provider":     workflowSystemToolStringSchema("Workflow provider name."),
		"cron":         workflowSystemToolStringSchema("Cron expression."),
		"timezone":     workflowSystemToolStringSchema("IANA timezone."),
		"paused":       map[string]any{"type": "boolean"},
		"target":       workflowSystemToolTargetSchema(),
		"definitionId": workflowSystemToolStringSchema("Workflow definition ID to schedule."),
	})
}

func workflowSystemToolStartRunSchema() map[string]any {
	common := map[string]any{
		"provider":    workflowSystemToolStringSchema("Workflow provider name."),
		"workflowKey": workflowSystemToolStringSchema("Workflow key."),
	}
	targetProperties := maps.Clone(common)
	targetProperties["target"] = workflowSystemToolAgentTargetSchema()
	targetProperties["deliverResultToCaller"] = map[string]any{"type": "boolean", "description": "When true, deliver the child agent's final result back to the current caller."}
	definitionProperties := maps.Clone(common)
	definitionProperties["definitionId"] = workflowSystemToolStringSchema("Workflow definition ID to run.")
	return map[string]any{
		"type": "object",
		"oneOf": []any{
			workflowSystemToolObjectSchema([]string{"target"}, targetProperties),
			workflowSystemToolObjectSchema([]string{"definitionId"}, definitionProperties),
		},
	}
}

func workflowSystemToolUpdateScheduleSchema() map[string]any {
	return workflowSystemToolObjectSchema([]string{"scheduleId"}, map[string]any{
		"scheduleId":   workflowSystemToolStringSchema("Schedule ID."),
		"provider":     workflowSystemToolStringSchema("Workflow provider name."),
		"cron":         workflowSystemToolStringSchema("Cron expression. If omitted, the existing cron is preserved."),
		"timezone":     workflowSystemToolStringSchema("IANA timezone. If omitted, the existing timezone is preserved."),
		"paused":       map[string]any{"type": "boolean", "description": "Paused state. If omitted, the existing paused state is preserved."},
		"target":       workflowSystemToolTargetSchema(),
		"definitionId": workflowSystemToolStringSchema("Workflow definition ID to schedule. If omitted with no target, the existing resolved target is preserved."),
	})
}

func workflowSystemToolListRunsSchema() map[string]any {
	return workflowSystemToolObjectSchema(nil, map[string]any{
		"pageSize":  map[string]any{"type": "integer", "minimum": 0, "description": "Maximum runs to return."},
		"pageToken": workflowSystemToolStringSchema("Pagination token from a previous workflow_runs_list response."),
		"app":    workflowSystemToolStringSchema("Target app name to filter by."),
		"status": map[string]any{
			"type":        "string",
			"description": "Workflow run status to filter by.",
			"enum": []any{
				string(coreworkflow.RunStatusPending),
				string(coreworkflow.RunStatusRunning),
				string(coreworkflow.RunStatusSucceeded),
				string(coreworkflow.RunStatusFailed),
				string(coreworkflow.RunStatusCanceled),
			},
		},
	})
}

func workflowSystemToolCreateDefinitionSchema() map[string]any {
	return workflowSystemToolObjectSchema([]string{"target"}, map[string]any{
		"provider": workflowSystemToolStringSchema("Workflow provider name."),
		"target":   workflowSystemToolTargetSchema(),
	})
}

func workflowSystemToolUpdateDefinitionSchema() map[string]any {
	return workflowSystemToolObjectSchema([]string{"definitionId", "target"}, map[string]any{
		"definitionId": workflowSystemToolStringSchema("Workflow definition ID."),
		"provider":     workflowSystemToolStringSchema("Workflow provider name."),
		"target":       workflowSystemToolTargetSchema(),
	})
}

func workflowSystemToolTargetSchema() map[string]any {
	return workflowSystemToolObjectSchema([]string{}, map[string]any{
		"app": workflowSystemToolObjectSchema([]string{"name", "operation"}, map[string]any{
			"name":       workflowSystemToolStringSchema("App name."),
			"operation":  workflowSystemToolStringSchema("App operation."),
			"connection": workflowSystemToolStringSchema("Connection name."),
			"instance":   workflowSystemToolStringSchema("Instance name."),
			"input":      map[string]any{"type": "object"},
		}),
		"agent": workflowSystemToolAgentSchema(),
	})
}

func workflowSystemToolAgentTargetSchema() map[string]any {
	return workflowSystemToolObjectSchema([]string{"agent"}, map[string]any{
		"agent": workflowSystemToolAgentSchema(),
	})
}

func workflowSystemToolAgentSchema() map[string]any {
	return workflowSystemToolObjectSchema([]string{}, map[string]any{
		"provider":       workflowSystemToolStringSchema("Agent provider name."),
		"model":          workflowSystemToolStringSchema("Agent model."),
		"prompt":         workflowSystemToolStringSchema("Agent prompt."),
		"messages":       map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
		"toolRefs":       map[string]any{"type": "array", "items": map[string]any{"type": "object"}, "description": "Agent tool references. If omitted, the created workflow agent inherits the current agent turn's tool references."},
		"responseSchema": map[string]any{"type": "object"},
		"metadata":       map[string]any{"type": "object"},
		"modelOptions":   map[string]any{"type": "object"},
		"timeoutSeconds": map[string]any{"type": "integer", "minimum": 0},
		"steps":          map[string]any{"type": "array", "items": workflowSystemToolAgentStepSchema()},
	})
}

func workflowSystemToolAgentStepSchema() map[string]any {
	return workflowSystemToolObjectSchema([]string{"id"}, map[string]any{
		"id":             workflowSystemToolStringSchema("Stable step ID."),
		"prompt":         workflowSystemToolStringSchema("Agent step prompt."),
		"messages":       map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
		"toolRefs":       map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
		"responseSchema": map[string]any{"type": "object"},
		"metadata":       map[string]any{"type": "object"},
		"modelOptions":   map[string]any{"type": "object"},
		"timeoutSeconds": map[string]any{"type": "integer", "minimum": 0},
		"outputDelivery": workflowSystemToolOutputDeliverySchema(),
		"when": workflowSystemToolObjectSchema([]string{"stepId", "outputPath", "equals"}, map[string]any{
			"stepId":     workflowSystemToolStringSchema("Earlier step ID."),
			"outputPath": workflowSystemToolStringSchema("Dot path in the earlier step output."),
			"equals":     map[string]any{},
		}),
	})
}

func workflowSystemToolOutputDeliverySchema() map[string]any {
	return workflowSystemToolObjectSchema([]string{"target"}, map[string]any{
		"target": workflowSystemToolObjectSchema([]string{"name", "operation"}, map[string]any{
			"name":       workflowSystemToolStringSchema("Delivery app name."),
			"operation":  workflowSystemToolStringSchema("Delivery app operation."),
			"connection": workflowSystemToolStringSchema("Connection name."),
			"instance":   workflowSystemToolStringSchema("Instance name."),
			"input":      map[string]any{"type": "object"},
		}),
		"inputBindings": map[string]any{"type": "array", "items": workflowSystemToolObjectSchema([]string{"inputField", "value"}, map[string]any{
			"inputField": workflowSystemToolStringSchema("Delivery input field."),
			"value": workflowSystemToolObjectSchema([]string{}, map[string]any{
				"agentOutput":    workflowSystemToolStringSchema("Path in the agent step output."),
				"signalPayload":  workflowSystemToolStringSchema("Path in the workflow signal payload."),
				"signalMetadata": workflowSystemToolStringSchema("Path in the workflow signal metadata."),
				"agentSession":   workflowSystemToolStringSchema("Path in the agent session."),
				"literal":        map[string]any{},
			}),
		})},
		"credentialMode": workflowSystemToolStringSchema("Optional delivery credential mode."),
	})
}

func workflowSystemToolObjectSchema(required []string, properties map[string]any) map[string]any {
	if properties == nil {
		properties = map[string]any{}
	}
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = append([]string(nil), required...)
	}
	return schema
}

func workflowSystemToolStringSchema(description string) map[string]any {
	schema := map[string]any{"type": "string"}
	if strings.TrimSpace(description) != "" {
		schema["description"] = description
	}
	return schema
}

func workflowSystemToolJSONResponse(status int, value any) (*coreagent.ExecuteToolResponse, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal workflow tool response: %v", invocation.ErrInternal, err)
	}
	return &coreagent.ExecuteToolResponse{Status: status, Body: string(body)}, nil
}

func workflowSystemToolTargetFromValue(value any) (coreworkflow.Target, error) {
	target, ok := workflowSystemToolMap(value)
	if !ok {
		return coreworkflow.Target{}, fmt.Errorf("%w: target must be an object", invocation.ErrInvalidInvocation)
	}
	if err := workflowSystemToolRejectUnknownKeys(target, "target", "app", "agent"); err != nil {
		return coreworkflow.Target{}, err
	}
	pluginValue, hasApp := target["app"]
	agentValue, hasAgent := target["agent"]
	if hasApp == hasAgent {
		return coreworkflow.Target{}, fmt.Errorf("%w: target must set exactly one of app or agent", invocation.ErrInvalidInvocation)
	}
	if hasApp {
		pluginMap, ok := workflowSystemToolMap(pluginValue)
		if !ok {
			return coreworkflow.Target{}, fmt.Errorf("%w: target.plugin must be an object", invocation.ErrInvalidInvocation)
		}
		if err := workflowSystemToolRejectUnknownKeys(pluginMap, "target.plugin", "name", "operation", "connection", "instance", "input"); err != nil {
			return coreworkflow.Target{}, err
		}
		pluginName := workflowSystemToolStringArg(pluginMap, "name")
		operation := workflowSystemToolStringArg(pluginMap, "operation")
		if pluginName == "" || operation == "" {
			return coreworkflow.Target{}, fmt.Errorf("%w: target.app.name and target.app.operation are required", invocation.ErrInvalidInvocation)
		}
		input, err := workflowSystemToolObjectArg(pluginMap, "input", "target.plugin")
		if err != nil {
			return coreworkflow.Target{}, err
		}
		return coreworkflow.Target{App: &coreworkflow.AppTarget{
			AppName: pluginName,
			Operation:  operation,
			Connection: workflowSystemToolStringArg(pluginMap, "connection"),
			Instance:   workflowSystemToolStringArg(pluginMap, "instance"),
			Input:      input,
		}}, nil
	}
	agentMap, ok := workflowSystemToolMap(agentValue)
	if !ok {
		return coreworkflow.Target{}, fmt.Errorf("%w: target.agent must be an object", invocation.ErrInvalidInvocation)
	}
	if err := workflowSystemToolRejectUnknownKeys(agentMap, "target.agent", "provider", "model", "prompt", "messages", "toolRefs", "responseSchema", "metadata", "modelOptions", "timeoutSeconds", "steps"); err != nil {
		return coreworkflow.Target{}, err
	}
	steps, err := workflowSystemToolAgentStepsFromValue(agentMap["steps"])
	if err != nil {
		return coreworkflow.Target{}, err
	}
	toolRefs, err := workflowSystemToolRefsFromValue(agentMap["toolRefs"])
	if err != nil {
		return coreworkflow.Target{}, err
	}
	messages, err := workflowSystemToolMessagesFromValue(agentMap["messages"])
	if err != nil {
		return coreworkflow.Target{}, err
	}
	responseSchema, err := workflowSystemToolObjectArg(agentMap, "responseSchema", "target.agent")
	if err != nil {
		return coreworkflow.Target{}, err
	}
	metadata, err := workflowSystemToolObjectArg(agentMap, "metadata", "target.agent")
	if err != nil {
		return coreworkflow.Target{}, err
	}
	modelOptions, err := workflowSystemToolObjectArg(agentMap, "modelOptions", "target.agent")
	if err != nil {
		return coreworkflow.Target{}, err
	}
	return coreworkflow.Target{Agent: &coreworkflow.AgentTarget{
		ProviderName:   workflowSystemToolStringArg(agentMap, "provider"),
		Model:          workflowSystemToolStringArg(agentMap, "model"),
		Prompt:         workflowSystemToolStringArg(agentMap, "prompt"),
		Messages:       messages,
		ToolRefs:       toolRefs,
		ResponseSchema: responseSchema,
		Metadata:       metadata,
		ModelOptions:   modelOptions,
		TimeoutSeconds: workflowSystemToolIntArg(agentMap, "timeoutSeconds"),
		Steps:          steps,
	}}, nil
}

func workflowSystemToolAgentStepsFromValue(value any) ([]coreworkflow.AgentStep, error) {
	if value == nil {
		return nil, nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%w: target.agent.steps must be an array", invocation.ErrInvalidInvocation)
	}
	out := make([]coreworkflow.AgentStep, 0, len(items))
	for i, item := range items {
		stepMap, ok := workflowSystemToolMap(item)
		if !ok {
			return nil, fmt.Errorf("%w: target.agent.steps[%d] must be an object", invocation.ErrInvalidInvocation, i)
		}
		path := fmt.Sprintf("target.agent.steps[%d]", i)
		if err := workflowSystemToolRejectUnknownKeys(stepMap, path, "id", "prompt", "messages", "toolRefs", "responseSchema", "metadata", "modelOptions", "timeoutSeconds", "outputDelivery", "when"); err != nil {
			return nil, err
		}
		toolRefs, err := workflowSystemToolRefsFromValue(stepMap["toolRefs"])
		if err != nil {
			return nil, err
		}
		messages, err := workflowSystemToolMessagesFromValue(stepMap["messages"])
		if err != nil {
			return nil, err
		}
		responseSchema, err := workflowSystemToolObjectArg(stepMap, "responseSchema", path)
		if err != nil {
			return nil, err
		}
		metadata, err := workflowSystemToolObjectArg(stepMap, "metadata", path)
		if err != nil {
			return nil, err
		}
		modelOptions, err := workflowSystemToolObjectArg(stepMap, "modelOptions", path)
		if err != nil {
			return nil, err
		}
		outputDelivery, err := workflowSystemToolOutputDeliveryFromValue(stepMap["outputDelivery"], path+".outputDelivery")
		if err != nil {
			return nil, err
		}
		when, err := workflowSystemToolAgentStepWhenFromValue(stepMap["when"], path+".when")
		if err != nil {
			return nil, err
		}
		out = append(out, coreworkflow.AgentStep{
			ID:             workflowSystemToolStringArg(stepMap, "id"),
			Prompt:         workflowSystemToolStringArg(stepMap, "prompt"),
			Messages:       messages,
			ToolRefs:       toolRefs,
			ResponseSchema: responseSchema,
			Metadata:       metadata,
			ModelOptions:   modelOptions,
			TimeoutSeconds: workflowSystemToolIntArg(stepMap, "timeoutSeconds"),
			OutputDelivery: outputDelivery,
			When:           when,
		})
	}
	return out, nil
}

func workflowSystemToolOutputDeliveryFromValue(value any, path string) (*coreworkflow.OutputDelivery, error) {
	if value == nil {
		return nil, nil
	}
	deliveryMap, ok := workflowSystemToolMap(value)
	if !ok {
		return nil, fmt.Errorf("%w: %s must be an object", invocation.ErrInvalidInvocation, path)
	}
	if err := workflowSystemToolRejectUnknownKeys(deliveryMap, path, "target", "inputBindings", "credentialMode"); err != nil {
		return nil, err
	}
	targetValue, ok := deliveryMap["target"]
	if !ok {
		return nil, fmt.Errorf("%w: %s.target is required", invocation.ErrInvalidInvocation, path)
	}
	targetMap, ok := workflowSystemToolMap(targetValue)
	if !ok {
		return nil, fmt.Errorf("%w: %s.target must be an object", invocation.ErrInvalidInvocation, path)
	}
	if err := workflowSystemToolRejectUnknownKeys(targetMap, path+".target", "name", "operation", "connection", "instance", "input"); err != nil {
		return nil, err
	}
	input, err := workflowSystemToolObjectArg(targetMap, "input", path+".target")
	if err != nil {
		return nil, err
	}
	bindings, err := workflowSystemToolOutputBindingsFromValue(deliveryMap["inputBindings"], path+".inputBindings")
	if err != nil {
		return nil, err
	}
	return &coreworkflow.OutputDelivery{
		Target: coreworkflow.AppTarget{
			AppName: workflowSystemToolStringArg(targetMap, "name"),
			Operation:  workflowSystemToolStringArg(targetMap, "operation"),
			Connection: workflowSystemToolStringArg(targetMap, "connection"),
			Instance:   workflowSystemToolStringArg(targetMap, "instance"),
			Input:      input,
		},
		InputBindings:  bindings,
		CredentialMode: core.NormalizeOptionalConnectionMode(core.ConnectionMode(workflowSystemToolStringArg(deliveryMap, "credentialMode"))),
	}, nil
}

func workflowSystemToolOutputBindingsFromValue(value any, path string) ([]coreworkflow.OutputBinding, error) {
	if value == nil {
		return nil, nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%w: %s must be an array", invocation.ErrInvalidInvocation, path)
	}
	out := make([]coreworkflow.OutputBinding, 0, len(items))
	for i, item := range items {
		bindingMap, ok := workflowSystemToolMap(item)
		if !ok {
			return nil, fmt.Errorf("%w: %s[%d] must be an object", invocation.ErrInvalidInvocation, path, i)
		}
		bindingPath := fmt.Sprintf("%s[%d]", path, i)
		if err := workflowSystemToolRejectUnknownKeys(bindingMap, bindingPath, "inputField", "value"); err != nil {
			return nil, err
		}
		valueSource, err := workflowSystemToolOutputValueSourceFromValue(bindingMap["value"], bindingPath+".value")
		if err != nil {
			return nil, err
		}
		out = append(out, coreworkflow.OutputBinding{
			InputField: workflowSystemToolStringArg(bindingMap, "inputField"),
			Value:      valueSource,
		})
	}
	return out, nil
}

func workflowSystemToolOutputValueSourceFromValue(value any, path string) (coreworkflow.OutputValueSource, error) {
	valueMap, ok := workflowSystemToolMap(value)
	if !ok {
		return coreworkflow.OutputValueSource{}, fmt.Errorf("%w: %s must be an object", invocation.ErrInvalidInvocation, path)
	}
	if err := workflowSystemToolRejectUnknownKeys(valueMap, path, "agentOutput", "signalPayload", "signalMetadata", "agentSession", "literal"); err != nil {
		return coreworkflow.OutputValueSource{}, err
	}
	source := coreworkflow.OutputValueSource{
		AgentOutput:    workflowSystemToolStringArg(valueMap, "agentOutput"),
		SignalPayload:  workflowSystemToolStringArg(valueMap, "signalPayload"),
		SignalMetadata: workflowSystemToolStringArg(valueMap, "signalMetadata"),
		AgentSession:   workflowSystemToolStringArg(valueMap, "agentSession"),
	}
	if literal, ok := valueMap["literal"]; ok {
		source.Literal = workflowSystemToolValueDeepClone(literal)
	}
	return source, nil
}

func workflowSystemToolAgentStepWhenFromValue(value any, path string) (*coreworkflow.AgentStepWhen, error) {
	if value == nil {
		return nil, nil
	}
	whenMap, ok := workflowSystemToolMap(value)
	if !ok {
		return nil, fmt.Errorf("%w: %s must be an object", invocation.ErrInvalidInvocation, path)
	}
	if err := workflowSystemToolRejectUnknownKeys(whenMap, path, "stepId", "outputPath", "equals"); err != nil {
		return nil, err
	}
	equals, ok := whenMap["equals"]
	if !ok {
		return nil, fmt.Errorf("%w: %s.equals is required", invocation.ErrInvalidInvocation, path)
	}
	if !jsonvalue.IsScalar(equals) {
		return nil, fmt.Errorf("%w: %s.equals must be a scalar JSON value", invocation.ErrInvalidInvocation, path)
	}
	return &coreworkflow.AgentStepWhen{
		StepID:     workflowSystemToolStringArg(whenMap, "stepId"),
		OutputPath: workflowSystemToolStringArg(whenMap, "outputPath"),
		Equals:     equals,
		EqualsSet:  true,
	}, nil
}

func workflowSystemToolInheritAgentToolRefs(req agentSystemToolExecutionRequest, target *coreworkflow.Target) {
	if target == nil || target.Agent == nil {
		return
	}
	if len(target.Agent.Steps) == 0 && target.Agent.ToolRefs == nil {
		target.Agent.ToolRefs = workflowSystemToolInheritedAgentToolRefs(req)
	}
	for i := range target.Agent.Steps {
		if target.Agent.Steps[i].ToolRefs == nil {
			target.Agent.Steps[i].ToolRefs = workflowSystemToolInheritedAgentToolRefs(req)
		}
	}
}

func workflowSystemToolApplyInheritedOutputDelivery(req agentSystemToolExecutionRequest, target *coreworkflow.Target) error {
	if target == nil || target.Agent == nil {
		return fmt.Errorf("%w: deliverResultToCaller requires a direct agent target", invocation.ErrInvalidInvocation)
	}
	if req.InheritedOutputDelivery == nil {
		return fmt.Errorf("%w: no caller output delivery is available for this turn", invocation.ErrInvalidInvocation)
	}
	if len(target.Agent.Steps) > 0 {
		if target.Agent.OutputDelivery != nil {
			return fmt.Errorf("%w: deliverResultToCaller cannot override an agent delivery target", invocation.ErrInvalidInvocation)
		}
		lastStep := &target.Agent.Steps[len(target.Agent.Steps)-1]
		if lastStep.OutputDelivery != nil {
			return fmt.Errorf("%w: deliverResultToCaller cannot override final agent step delivery target", invocation.ErrInvalidInvocation)
		}
		if lastStep.When != nil {
			return fmt.Errorf("%w: deliverResultToCaller requires an unconditional final agent step; configure explicit outputDelivery for conditional final steps", invocation.ErrInvalidInvocation)
		}
		lastStep.OutputDelivery = coreworkflow.CloneOutputDelivery(req.InheritedOutputDelivery)
		return nil
	}
	if target.Agent.OutputDelivery != nil || target.Agent.SessionReadyDelivery != nil {
		return fmt.Errorf("%w: deliverResultToCaller cannot override an agent delivery target", invocation.ErrInvalidInvocation)
	}
	target.Agent.OutputDelivery = coreworkflow.CloneOutputDelivery(req.InheritedOutputDelivery)
	return nil
}

func workflowSystemToolInheritedAgentToolRefs(req agentSystemToolExecutionRequest) []coreagent.ToolRef {
	out := []coreagent.ToolRef{}
	seen := map[string]struct{}{}
	add := func(ref coreagent.ToolRef) {
		ref.System = strings.TrimSpace(ref.System)
		ref.App = strings.TrimSpace(ref.App)
		ref.Operation = strings.TrimSpace(ref.Operation)
		ref.Connection = strings.TrimSpace(ref.Connection)
		ref.Instance = strings.TrimSpace(ref.Instance)
		if ref.System != "" {
			if ref.System != coreagent.SystemToolWorkflow || ref.Operation == "" {
				return
			}
			if ref.App != "" || ref.Connection != "" || ref.Instance != "" || ref.CredentialMode != "" || ref.RunAs != nil || ref.RunAsExternalIdentity != nil {
				return
			}
		} else if ref.App == "" || ref.App == "*" || ref.Operation == "" {
			return
		}
		inherited := coreagent.ToolRef{
			System:     ref.System,
			App:     ref.App,
			Operation:  ref.Operation,
			Connection: ref.Connection,
			Instance:   ref.Instance,
		}
		key := strings.Join([]string{inherited.System, inherited.App, inherited.Operation, inherited.Connection, inherited.Instance}, "\x00")
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, inherited)
	}
	for i := range req.ToolRefs {
		add(req.ToolRefs[i])
	}
	for i := range req.Tools {
		target := req.Tools[i].Target
		if strings.TrimSpace(target.System) != "" {
			add(coreagent.ToolRef{
				System:    target.System,
				Operation: target.Operation,
			})
			continue
		}
		add(coreagent.ToolRef{
			App:     target.App,
			Operation:  target.Operation,
			Connection: target.Connection,
			Instance:   target.Instance,
		})
	}
	return out
}

func workflowSystemToolRefsFromValue(value any) ([]coreagent.ToolRef, error) {
	if value == nil {
		return nil, nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%w: agent toolRefs must be an array", invocation.ErrInvalidInvocation)
	}
	out := make([]coreagent.ToolRef, 0, len(items))
	for i, item := range items {
		refMap, ok := workflowSystemToolMap(item)
		if !ok {
			return nil, fmt.Errorf("%w: agent toolRefs[%d] must be an object", invocation.ErrInvalidInvocation, i)
		}
		if err := workflowSystemToolRejectUnknownKeys(refMap, fmt.Sprintf("agent toolRefs[%d]", i), "system", "app", "operation", "connection", "instance", "title", "description"); err != nil {
			return nil, err
		}
		out = append(out, coreagent.ToolRef{
			System:      workflowSystemToolStringArg(refMap, "system"),
			App:      workflowSystemToolStringArg(refMap, "app"),
			Operation:   workflowSystemToolStringArg(refMap, "operation"),
			Connection:  workflowSystemToolStringArg(refMap, "connection"),
			Instance:    workflowSystemToolStringArg(refMap, "instance"),
			Title:       workflowSystemToolStringArg(refMap, "title"),
			Description: workflowSystemToolStringArg(refMap, "description"),
		})
	}
	return out, nil
}

func workflowSystemToolMessagesFromValue(value any) ([]coreagent.Message, error) {
	if value == nil {
		return nil, nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%w: agent messages must be an array", invocation.ErrInvalidInvocation)
	}
	out := make([]coreagent.Message, 0, len(items))
	for i, item := range items {
		messageMap, ok := workflowSystemToolMap(item)
		if !ok {
			return nil, fmt.Errorf("%w: agent messages[%d] must be an object", invocation.ErrInvalidInvocation, i)
		}
		if err := workflowSystemToolRejectUnknownKeys(messageMap, fmt.Sprintf("agent messages[%d]", i), "role", "text", "metadata"); err != nil {
			return nil, err
		}
		metadata, err := workflowSystemToolObjectArg(messageMap, "metadata", fmt.Sprintf("agent messages[%d]", i))
		if err != nil {
			return nil, err
		}
		out = append(out, coreagent.Message{
			Role:     workflowSystemToolStringArg(messageMap, "role"),
			Text:     workflowSystemToolStringArg(messageMap, "text"),
			Metadata: metadata,
		})
	}
	return out, nil
}

func workflowSystemToolValidateCreateScope(req agentSystemToolExecutionRequest, target coreworkflow.Target) error {
	if target.App != nil {
		if workflowSystemToolAppTargetAllowed(*target.App, req.ToolRefs, req.Tools) {
			return nil
		}
		return fmt.Errorf("%w: workflow target %s.%s is outside the current agent tool scope", invocation.ErrScopeDenied, target.App.AppName, target.App.Operation)
	}
	if target.Agent == nil {
		return fmt.Errorf("%w: workflow target is required", invocation.ErrInvalidInvocation)
	}
	for i := range target.Agent.ToolRefs {
		ref := target.Agent.ToolRefs[i]
		if strings.TrimSpace(ref.System) != "" {
			path := fmt.Sprintf("target.agent.toolRefs[%d]", i)
			if err := workflowSystemToolValidateFutureSystemRef(path, ref, req); err != nil {
				return err
			}
			continue
		}
		if strings.TrimSpace(ref.App) == "" || strings.TrimSpace(ref.App) == "*" || strings.TrimSpace(ref.Operation) == "" {
			return fmt.Errorf("%w: target.agent.toolRefs[%d] must be an exact app operation", invocation.ErrInvalidInvocation, i)
		}
		if ref.CredentialMode != "" {
			return fmt.Errorf("%w: target.agent.toolRefs[%d] credentialMode is not supported for scheduled agent targets", invocation.ErrInvalidInvocation, i)
		}
		if ref.RunAs != nil || ref.RunAsExternalIdentity != nil {
			return fmt.Errorf("%w: target.agent.toolRefs[%d] runAs is not supported for scheduled agent targets", invocation.ErrInvalidInvocation, i)
		}
		if !workflowSystemToolAgentPluginRefAllowed(ref, req.ToolRefs, req.Tools) {
			return fmt.Errorf("%w: target.agent.toolRefs[%d] %s.%s is outside the current agent tool scope", invocation.ErrScopeDenied, i, ref.App, ref.Operation)
		}
	}
	for stepIndex := range target.Agent.Steps {
		for i := range target.Agent.Steps[stepIndex].ToolRefs {
			ref := target.Agent.Steps[stepIndex].ToolRefs[i]
			if strings.TrimSpace(ref.System) != "" {
				path := fmt.Sprintf("target.agent.steps[%d].toolRefs[%d]", stepIndex, i)
				if err := workflowSystemToolValidateFutureSystemRef(path, ref, req); err != nil {
					return err
				}
				continue
			}
			if strings.TrimSpace(ref.App) == "" || strings.TrimSpace(ref.App) == "*" || strings.TrimSpace(ref.Operation) == "" {
				return fmt.Errorf("%w: target.agent.steps[%d].toolRefs[%d] must be an exact app operation", invocation.ErrInvalidInvocation, stepIndex, i)
			}
			if ref.CredentialMode != "" {
				return fmt.Errorf("%w: target.agent.steps[%d].toolRefs[%d] credentialMode is not supported for scheduled agent targets", invocation.ErrInvalidInvocation, stepIndex, i)
			}
			if ref.RunAs != nil || ref.RunAsExternalIdentity != nil {
				return fmt.Errorf("%w: target.agent.steps[%d].toolRefs[%d] runAs is not supported for scheduled agent targets", invocation.ErrInvalidInvocation, stepIndex, i)
			}
			if !workflowSystemToolAgentPluginRefAllowed(ref, req.ToolRefs, req.Tools) {
				return fmt.Errorf("%w: target.agent.steps[%d].toolRefs[%d] %s.%s is outside the current agent tool scope", invocation.ErrScopeDenied, stepIndex, i, ref.App, ref.Operation)
			}
		}
	}
	return nil
}

func workflowSystemToolValidateFutureSystemRef(path string, ref coreagent.ToolRef, req agentSystemToolExecutionRequest) error {
	if strings.TrimSpace(ref.System) != coreagent.SystemToolWorkflow || strings.TrimSpace(ref.Operation) == "" {
		return fmt.Errorf("%w: %s workflow system refs require an exact operation", invocation.ErrInvalidInvocation, path)
	}
	if strings.TrimSpace(ref.App) != "" || strings.TrimSpace(ref.Connection) != "" || strings.TrimSpace(ref.Instance) != "" || ref.CredentialMode != "" || ref.RunAs != nil || ref.RunAsExternalIdentity != nil {
		return fmt.Errorf("%w: %s system refs cannot include plugin, connection, instance, credentialMode, or runAs", invocation.ErrInvalidInvocation, path)
	}
	for i := range req.ToolRefs {
		if strings.TrimSpace(req.ToolRefs[i].System) == coreagent.SystemToolWorkflow && strings.TrimSpace(req.ToolRefs[i].Operation) == strings.TrimSpace(ref.Operation) {
			return nil
		}
	}
	for i := range req.Tools {
		if strings.TrimSpace(req.Tools[i].Target.System) == coreagent.SystemToolWorkflow && strings.TrimSpace(req.Tools[i].Target.Operation) == strings.TrimSpace(ref.Operation) {
			return nil
		}
	}
	return fmt.Errorf("%w: %s workflow.%s is outside the current agent tool scope", invocation.ErrScopeDenied, path, ref.Operation)
}

func workflowSystemToolAgentPluginRefAllowed(target coreagent.ToolRef, refs []coreagent.ToolRef, tools []coreagent.Tool) bool {
	for i := range refs {
		if workflowSystemToolPluginRefMatchesAgentRef(refs[i], target) {
			return true
		}
	}
	for i := range tools {
		if workflowSystemToolResolvedToolMatchesAgentRef(tools[i], target) {
			return true
		}
	}
	return false
}

func workflowSystemToolAppTargetAllowed(target coreworkflow.AppTarget, refs []coreagent.ToolRef, tools []coreagent.Tool) bool {
	for i := range refs {
		if workflowSystemToolPluginRefMatchesTarget(refs[i], target) {
			return true
		}
	}
	for i := range tools {
		if workflowSystemToolResolvedToolMatchesTarget(tools[i], target) {
			return true
		}
	}
	return false
}

func workflowSystemToolPluginRefMatchesTarget(ref coreagent.ToolRef, target coreworkflow.AppTarget) bool {
	if strings.TrimSpace(ref.System) != "" || strings.TrimSpace(ref.App) == "" || strings.TrimSpace(ref.App) == "*" || strings.TrimSpace(ref.Operation) == "" {
		return false
	}
	if ref.CredentialMode != "" {
		return false
	}
	if strings.TrimSpace(ref.App) != strings.TrimSpace(target.AppName) || strings.TrimSpace(ref.Operation) != strings.TrimSpace(target.Operation) {
		return false
	}
	return workflowSystemToolRefBindingMatchesTarget(ref.Connection, ref.Instance, target.Connection, target.Instance)
}

func workflowSystemToolPluginRefMatchesAgentRef(ref coreagent.ToolRef, target coreagent.ToolRef) bool {
	if strings.TrimSpace(ref.System) != "" || strings.TrimSpace(ref.App) == "" || strings.TrimSpace(ref.App) == "*" || strings.TrimSpace(ref.Operation) == "" {
		return false
	}
	if strings.TrimSpace(ref.App) != strings.TrimSpace(target.App) || strings.TrimSpace(ref.Operation) != strings.TrimSpace(target.Operation) {
		return false
	}
	return workflowSystemToolRefBindingMatchesTarget(ref.Connection, ref.Instance, target.Connection, target.Instance)
}

func workflowSystemToolResolvedToolMatchesTarget(tool coreagent.Tool, target coreworkflow.AppTarget) bool {
	if strings.TrimSpace(tool.Target.System) != "" || strings.TrimSpace(tool.Target.App) == "" || strings.TrimSpace(tool.Target.Operation) == "" {
		return false
	}
	if tool.Target.CredentialMode != "" {
		return false
	}
	if strings.TrimSpace(tool.Target.App) != strings.TrimSpace(target.AppName) || strings.TrimSpace(tool.Target.Operation) != strings.TrimSpace(target.Operation) {
		return false
	}
	return workflowSystemToolResolvedBindingMatchesTarget(tool.Target.Connection, tool.Target.Instance, target.Connection, target.Instance)
}

func workflowSystemToolResolvedToolMatchesAgentRef(tool coreagent.Tool, target coreagent.ToolRef) bool {
	if strings.TrimSpace(tool.Target.System) != "" || strings.TrimSpace(tool.Target.App) == "" || strings.TrimSpace(tool.Target.Operation) == "" {
		return false
	}
	if strings.TrimSpace(tool.Target.App) != strings.TrimSpace(target.App) || strings.TrimSpace(tool.Target.Operation) != strings.TrimSpace(target.Operation) {
		return false
	}
	return workflowSystemToolResolvedBindingMatchesTarget(tool.Target.Connection, tool.Target.Instance, target.Connection, target.Instance)
}

func workflowSystemToolRefBindingMatchesTarget(scopeConnection, scopeInstance, targetConnection, targetInstance string) bool {
	scopeConnection = config.ResolveConnectionAlias(strings.TrimSpace(scopeConnection))
	targetConnection = config.ResolveConnectionAlias(strings.TrimSpace(targetConnection))
	if scopeConnection != "" && scopeConnection != targetConnection {
		return false
	}
	if scopeInstance = strings.TrimSpace(scopeInstance); scopeInstance != "" && scopeInstance != strings.TrimSpace(targetInstance) {
		return false
	}
	return true
}

func workflowSystemToolResolvedBindingMatchesTarget(scopeConnection, scopeInstance, targetConnection, targetInstance string) bool {
	scopeConnection = config.ResolveConnectionAlias(strings.TrimSpace(scopeConnection))
	targetConnection = config.ResolveConnectionAlias(strings.TrimSpace(targetConnection))
	if scopeConnection != targetConnection {
		return false
	}
	if strings.TrimSpace(scopeInstance) != strings.TrimSpace(targetInstance) {
		return false
	}
	return true
}

func workflowSystemToolPermissionsForTarget(target coreworkflow.Target, defaultAgentProvider string) []core.AccessPermission {
	operationsByApp := map[string]map[string]struct{}{}
	addOperation := func(pluginName, operation string) {
		pluginName = strings.TrimSpace(pluginName)
		operation = strings.TrimSpace(operation)
		if pluginName == "" || operation == "" {
			return
		}
		if ops, ok := operationsByApp[pluginName]; ok && ops == nil {
			return
		}
		if operationsByApp[pluginName] == nil {
			operationsByApp[pluginName] = map[string]struct{}{}
		}
		operationsByApp[pluginName][operation] = struct{}{}
	}
	addProvider := func(providerName string) {
		providerName = strings.TrimSpace(providerName)
		if providerName == "" {
			return
		}
		if _, ok := operationsByApp[providerName]; !ok {
			operationsByApp[providerName] = nil
		}
	}
	if target.App != nil {
		addOperation(target.App.AppName, target.App.Operation)
	}
	if target.Agent != nil {
		agentProvider := strings.TrimSpace(target.Agent.ProviderName)
		if agentProvider == "" {
			agentProvider = strings.TrimSpace(defaultAgentProvider)
		}
		addProvider(agentProvider)
		for i := range target.Agent.ToolRefs {
			ref := target.Agent.ToolRefs[i]
			if strings.TrimSpace(ref.System) == "" {
				addOperation(ref.App, ref.Operation)
			}
		}
		if delivery := target.Agent.OutputDelivery; delivery != nil {
			addOperation(delivery.Target.AppName, delivery.Target.Operation)
		}
		if delivery := target.Agent.SessionReadyDelivery; delivery != nil {
			addOperation(delivery.Target.AppName, delivery.Target.Operation)
		}
		for i := range target.Agent.Steps {
			step := target.Agent.Steps[i]
			for j := range step.ToolRefs {
				ref := step.ToolRefs[j]
				if strings.TrimSpace(ref.System) == "" {
					addOperation(ref.App, ref.Operation)
				}
			}
			if delivery := step.OutputDelivery; delivery != nil {
				addOperation(delivery.Target.AppName, delivery.Target.Operation)
			}
		}
	}
	if len(operationsByApp) == 0 {
		return []core.AccessPermission{}
	}
	plugins := slices.Sorted(maps.Keys(operationsByApp))
	out := make([]core.AccessPermission, 0, len(plugins))
	for _, pluginName := range plugins {
		operations := slices.Sorted(maps.Keys(operationsByApp[pluginName]))
		out = append(out, core.AccessPermission{App: pluginName, Operations: operations})
	}
	return out
}

func workflowSystemToolScopedPermissions(req agentSystemToolExecutionRequest, target coreworkflow.Target) ([]core.AccessPermission, error) {
	permissions := workflowSystemToolPermissionsForTarget(target, req.ProviderName)
	scopedPrincipal, err := workflowSystemToolScopedPrincipal(req.Principal, permissions, workflowSystemToolTrustedAgentProvider(req, target))
	if err != nil {
		return nil, err
	}
	return workflowSystemToolPermissionsFromPrincipal(scopedPrincipal), nil
}

func workflowSystemToolPermissionsFromPrincipal(p *principal.Principal) []core.AccessPermission {
	p = principal.Canonicalized(p)
	if p == nil || p.TokenPermissions == nil {
		return nil
	}
	return principal.PermissionsToAccessPermissions(p.TokenPermissions)
}

func workflowSystemToolExistingScheduleTarget(schedule *workflowmanager.ManagedSchedule) coreworkflow.Target {
	if schedule == nil {
		return coreworkflow.Target{}
	}
	if schedule.ExecutionRef != nil && (schedule.ExecutionRef.Target.App != nil || schedule.ExecutionRef.Target.Agent != nil) {
		return schedule.ExecutionRef.Target
	}
	if schedule.Schedule != nil {
		return schedule.Schedule.Target
	}
	return coreworkflow.Target{}
}

func workflowSystemToolClonePermissions(src []core.AccessPermission) []core.AccessPermission {
	if src == nil {
		return nil
	}
	out := append([]core.AccessPermission(nil), src...)
	for i := range out {
		out[i].Operations = append([]string(nil), out[i].Operations...)
		out[i].Actions = append([]string(nil), out[i].Actions...)
	}
	return out
}

func workflowSystemToolTrustedAgentProvider(req agentSystemToolExecutionRequest, target coreworkflow.Target) string {
	if target.Agent == nil {
		return ""
	}
	currentProvider := strings.TrimSpace(req.ProviderName)
	if currentProvider == "" {
		return ""
	}
	targetProvider := strings.TrimSpace(target.Agent.ProviderName)
	if targetProvider == "" || targetProvider == currentProvider {
		return currentProvider
	}
	return ""
}

func workflowSystemToolPrincipalWithTrustedProvider(p *principal.Principal, trustedProvider string) *principal.Principal {
	p = principal.Canonicalized(p)
	trustedProvider = strings.TrimSpace(trustedProvider)
	if p == nil || trustedProvider == "" || p.TokenPermissions == nil {
		return p
	}
	next := *p
	next.TokenPermissions = principal.ClonePermissionSet(p.TokenPermissions)
	next.TokenPermissions[trustedProvider] = nil
	next.Scopes = principal.PermissionApps(next.TokenPermissions)
	return principal.Canonicalize(&next)
}

func workflowSystemToolScopedPrincipal(p *principal.Principal, permissions []core.AccessPermission, trustedProvider string) (*principal.Principal, error) {
	return workflowSystemToolScopedPrincipalWithPermissions(p, permissions, trustedProvider, true)
}

func workflowSystemToolExactPermissionsPrincipal(p *principal.Principal, permissions []core.AccessPermission, trustedProvider string) (*principal.Principal, error) {
	return workflowSystemToolScopedPrincipalWithPermissions(p, permissions, trustedProvider, false)
}

func workflowSystemToolScopedPrincipalWithPermissions(p *principal.Principal, permissions []core.AccessPermission, trustedProvider string, requireWithinCaller bool) (*principal.Principal, error) {
	p = principal.Canonicalized(p)
	if p == nil || strings.TrimSpace(p.SubjectID) == "" {
		return nil, fmt.Errorf("%w: agent execution principal is required", invocation.ErrAuthorizationDenied)
	}
	if permissions == nil {
		return p, nil
	}
	requested := principal.CompilePermissions(permissions)
	if requested == nil {
		requested = principal.PermissionSet{}
	}
	if p.TokenPermissions != nil {
		requested = principal.IntersectPermissions(requested, p.TokenPermissions)
		if trustedProvider != "" {
			if requested == nil {
				requested = principal.PermissionSet{}
			}
			requested[trustedProvider] = nil
		}
		if requireWithinCaller && len(permissions) > 0 && len(requested) == 0 {
			return nil, fmt.Errorf("%w: workflow target is outside the caller permission scope", invocation.ErrScopeDenied)
		}
	}
	next := *p
	next.TokenPermissions = requested
	next.ActionPermissions = nil
	next.Scopes = principal.PermissionApps(requested)
	return principal.Canonicalize(&next), nil
}

func workflowSystemToolDefinitionInfo(definition *workflowmanager.ManagedDefinition) map[string]any {
	value := map[string]any{}
	if definition == nil {
		return value
	}
	if providerName := strings.TrimSpace(definition.ProviderName); providerName != "" {
		value["provider"] = providerName
	}
	if definition.Definition != nil {
		coreDefinition := definition.Definition
		value["id"] = coreDefinition.ID
		value["target"] = workflowSystemToolTargetInfo(coreDefinition.Target)
		workflowSystemToolPutTime(value, "createdAt", coreDefinition.CreatedAt)
	}
	return value
}

func workflowSystemToolScheduleInfo(schedule *workflowmanager.ManagedSchedule) map[string]any {
	value := map[string]any{}
	if schedule == nil {
		return value
	}
	if providerName := strings.TrimSpace(schedule.ProviderName); providerName != "" {
		value["provider"] = providerName
	}
	if schedule.Schedule != nil {
		coreSchedule := schedule.Schedule
		value["id"] = coreSchedule.ID
		value["cron"] = coreSchedule.Cron
		value["timezone"] = coreSchedule.Timezone
		value["paused"] = coreSchedule.Paused
		value["target"] = workflowSystemToolTargetInfo(coreSchedule.Target)
		workflowSystemToolPutTime(value, "createdAt", coreSchedule.CreatedAt)
		workflowSystemToolPutTime(value, "updatedAt", coreSchedule.UpdatedAt)
		workflowSystemToolPutTime(value, "nextRunAt", coreSchedule.NextRunAt)
	}
	if schedule.ExecutionRef != nil {
		if sourceDefinitionID := strings.TrimSpace(schedule.ExecutionRef.SourceDefinitionID); sourceDefinitionID != "" {
			value["sourceDefinitionId"] = sourceDefinitionID
		}
	}
	return value
}

func workflowSystemRunInfo(run *workflowmanager.ManagedRun) map[string]any {
	value := map[string]any{}
	if run == nil {
		return value
	}
	if providerName := strings.TrimSpace(run.ProviderName); providerName != "" {
		value["provider"] = providerName
	}
	if run.Run != nil {
		coreRun := run.Run
		value["id"] = coreRun.ID
		value["status"] = string(coreRun.Status)
		value["workflowKey"] = coreRun.WorkflowKey
		value["target"] = workflowSystemToolTargetInfo(coreRun.Target)
		if coreRun.StatusMessage != "" {
			value["statusMessage"] = coreRun.StatusMessage
		}
		if coreRun.ResultBody != "" {
			value["resultBody"] = coreRun.ResultBody
		}
		workflowSystemToolPutTime(value, "createdAt", coreRun.CreatedAt)
		workflowSystemToolPutTime(value, "startedAt", coreRun.StartedAt)
		workflowSystemToolPutTime(value, "completedAt", coreRun.CompletedAt)
	}
	return value
}

func workflowSystemToolTargetInfo(target coreworkflow.Target) map[string]any {
	value := map[string]any{}
	if target.App != nil {
		appTarget := target.App
		value["app"] = map[string]any{
			"name":       appTarget.AppName,
			"operation":  appTarget.Operation,
			"connection": appTarget.Connection,
			"instance":   appTarget.Instance,
			"input":      maps.Clone(appTarget.Input),
		}
		return value
	}
	if target.Agent == nil {
		return value
	}
	agentTarget := target.Agent
	agent := map[string]any{
		"provider":       agentTarget.ProviderName,
		"model":          agentTarget.Model,
		"prompt":         agentTarget.Prompt,
		"toolRefs":       workflowSystemToolRefsInfo(agentTarget.ToolRefs),
		"timeoutSeconds": agentTarget.TimeoutSeconds,
	}
	if len(agentTarget.Messages) > 0 {
		messages := make([]map[string]any, 0, len(agentTarget.Messages))
		for _, message := range agentTarget.Messages {
			messages = append(messages, map[string]any{
				"role":     message.Role,
				"text":     message.Text,
				"metadata": maps.Clone(message.Metadata),
			})
		}
		agent["messages"] = messages
	}
	if len(agentTarget.Steps) > 0 {
		steps := make([]map[string]any, 0, len(agentTarget.Steps))
		for i := range agentTarget.Steps {
			step := agentTarget.Steps[i]
			stepInfo := map[string]any{
				"id":             step.ID,
				"prompt":         step.Prompt,
				"toolRefs":       workflowSystemToolRefsInfo(step.ToolRefs),
				"timeoutSeconds": step.TimeoutSeconds,
			}
			if len(step.Messages) > 0 {
				messages := make([]map[string]any, 0, len(step.Messages))
				for _, message := range step.Messages {
					messages = append(messages, map[string]any{
						"role":     message.Role,
						"text":     message.Text,
						"metadata": maps.Clone(message.Metadata),
					})
				}
				stepInfo["messages"] = messages
			}
			if step.When != nil {
				stepInfo["when"] = map[string]any{
					"stepId":     step.When.StepID,
					"outputPath": step.When.OutputPath,
					"equals":     step.When.Equals,
				}
			}
			steps = append(steps, stepInfo)
		}
		agent["steps"] = steps
	}
	value["agent"] = agent
	return value
}

func workflowSystemToolRefsInfo(refs []coreagent.ToolRef) []map[string]any {
	out := make([]map[string]any, 0, len(refs))
	for i := range refs {
		ref := refs[i]
		value := map[string]any{}
		if systemName := strings.TrimSpace(ref.System); systemName != "" {
			value["system"] = systemName
		}
		if pluginName := strings.TrimSpace(ref.App); pluginName != "" {
			value["app"] = pluginName
		}
		if operation := strings.TrimSpace(ref.Operation); operation != "" {
			value["operation"] = operation
		}
		if connection := strings.TrimSpace(ref.Connection); connection != "" {
			value["connection"] = connection
		}
		if instance := strings.TrimSpace(ref.Instance); instance != "" {
			value["instance"] = instance
		}
		if len(value) > 0 {
			out = append(out, value)
		}
	}
	return out
}

func workflowSystemToolPutTime(value map[string]any, key string, t *time.Time) {
	if t != nil {
		value[key] = t.UTC().Format(time.RFC3339Nano)
	}
}

func workflowSystemToolStringArg(args map[string]any, key string) string {
	if value, ok := args[key]; ok {
		if s, ok := value.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func workflowSystemToolListRunsRequest(args map[string]any) (coreworkflow.ListRunsRequest, error) {
	req := coreworkflow.ListRunsRequest{
		PageToken:    workflowSystemToolStringArg(args, "pageToken"),
		TargetApp: workflowSystemToolStringArg(args, "app"),
	}
	if value, ok := args["pageSize"]; ok && value != nil {
		switch v := value.(type) {
		case int:
			req.PageSize = v
		case int64:
			req.PageSize = int(v)
		case float64:
			req.PageSize = int(v)
			if float64(req.PageSize) != v {
				return coreworkflow.ListRunsRequest{}, fmt.Errorf("%w: workflow.runs.list.pageSize must be an integer", invocation.ErrInvalidInvocation)
			}
		default:
			return coreworkflow.ListRunsRequest{}, fmt.Errorf("%w: workflow.runs.list.pageSize must be an integer", invocation.ErrInvalidInvocation)
		}
	}
	if status := workflowSystemToolStringArg(args, "status"); status != "" {
		req.Status = coreworkflow.RunStatus(status)
		switch req.Status {
		case coreworkflow.RunStatusPending, coreworkflow.RunStatusRunning, coreworkflow.RunStatusSucceeded, coreworkflow.RunStatusFailed, coreworkflow.RunStatusCanceled:
		default:
			return coreworkflow.ListRunsRequest{}, fmt.Errorf("%w: workflow.runs.list.status is not supported", invocation.ErrInvalidInvocation)
		}
	}
	return req, nil
}

func workflowSystemToolBoolArg(args map[string]any, key string) bool {
	value, ok := args[key]
	if !ok {
		return false
	}
	result, _ := value.(bool)
	return result
}

func workflowSystemToolBoolArgPresent(args map[string]any, key, path string) (bool, bool, error) {
	value, ok := args[key]
	if !ok || value == nil {
		return false, false, nil
	}
	result, ok := value.(bool)
	if !ok {
		return false, true, fmt.Errorf("%w: %s.%s must be a boolean", invocation.ErrInvalidInvocation, path, key)
	}
	return result, true, nil
}

func workflowSystemToolIntArg(args map[string]any, key string) int {
	value, ok := args[key]
	if !ok {
		return 0
	}
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

func workflowSystemToolMap(value any) (map[string]any, bool) {
	if value == nil {
		return nil, false
	}
	out, ok := value.(map[string]any)
	return out, ok
}

func workflowSystemToolObjectArg(args map[string]any, key, path string) (map[string]any, error) {
	value, ok := args[key]
	if !ok || value == nil {
		return nil, nil
	}
	typed, ok := workflowSystemToolMap(value)
	if !ok {
		return nil, fmt.Errorf("%w: %s.%s must be an object", invocation.ErrInvalidInvocation, path, key)
	}
	return maps.Clone(typed), nil
}

func workflowSystemToolMapDeepClone(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	return workflowSystemToolValueDeepClone(value).(map[string]any)
}

func workflowSystemToolValueDeepClone(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			out[key] = workflowSystemToolValueDeepClone(child)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, child := range typed {
			out[i] = workflowSystemToolValueDeepClone(child)
		}
		return out
	default:
		return typed
	}
}

func workflowSystemToolRejectUnknownKeys(args map[string]any, path string, allowed ...string) error {
	if len(args) == 0 {
		return nil
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allowedSet[key] = struct{}{}
	}
	for _, key := range slices.Sorted(maps.Keys(args)) {
		if _, ok := allowedSet[key]; ok {
			continue
		}
		return fmt.Errorf("%w: %s.%s is not supported", invocation.ErrInvalidInvocation, path, key)
	}
	return nil
}

var _ agentmanager.WorkflowSystemTools = (*workflowSystemTools)(nil)
