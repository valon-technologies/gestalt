package bootstrap

import (
	"context"
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
	"github.com/valon-technologies/gestalt/server/internal/workflowwire"
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
		Description:      "Create a reusable workflow definition from explicit workflow steps.",
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
		Description:      "Create a recurring workflow schedule from explicit workflow steps or a workflow definition.",
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
		Description:      "Start a one-off workflow run from explicit workflow steps or an existing workflow definition.",
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
		ProviderName:   workflowSystemToolStringArg(args, "provider"),
		Target:         target,
		IdempotencyKey: strings.TrimSpace(req.IdempotencyKey),
		CallerAppName:  workflowSystemToolCallerScope(req),
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
	permissions := workflowSystemToolPermissionsForTarget(target, req.ProviderName)
	scopedPrincipal, err := workflowSystemToolScopedPrincipal(req.Principal, permissions, workflowSystemToolTrustedAgentProvider(req, target))
	if err != nil {
		return nil, err
	}
	definition, err := t.manager.UpdateDefinition(ctx, scopedPrincipal, definitionID, workflowmanager.DefinitionUpsert{
		ProviderName:  workflowSystemToolStringArg(args, "provider"),
		Target:        target,
		CallerAppName: workflowSystemToolCallerScope(req),
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
		permissions := workflowSystemToolPermissionsForTarget(target, req.ProviderName)
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
		ProviderName:   workflowSystemToolStringArg(args, "provider"),
		Cron:           cron,
		Timezone:       workflowSystemToolStringArg(args, "timezone"),
		Target:         upsertTarget,
		DefinitionID:   definitionID,
		Paused:         workflowSystemToolBoolArg(args, "paused"),
		IdempotencyKey: strings.TrimSpace(req.IdempotencyKey),
		CallerAppName:  workflowSystemToolCallerScope(req),
	})
	if err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "agent workflow system tool created schedule", workflowSystemToolScheduleLogAttrs(req, schedule)...)
	return workflowSystemToolJSONResponse(http.StatusCreated, map[string]any{"schedule": workflowSystemToolScheduleInfo(schedule)})
}

func (t *workflowSystemTools) executeStartRun(ctx context.Context, req agentSystemToolExecutionRequest) (*coreagent.ExecuteToolResponse, error) {
	args := req.Arguments
	if err := workflowSystemToolRejectUnknownKeys(args, "workflow.runs.start", "provider", "workflowKey", "target", "definitionId"); err != nil {
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

	var target coreworkflow.Target
	startTarget := coreworkflow.Target{}
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
		permissions := workflowSystemToolPermissionsForTarget(target, req.ProviderName)
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
		startTarget = target
	}
	run, err := t.manager.StartRun(ctx, scopedPrincipal, workflowmanager.RunStart{
		ProviderName:   workflowSystemToolStringArg(args, "provider"),
		Target:         startTarget,
		DefinitionID:   definitionID,
		IdempotencyKey: strings.TrimSpace(req.IdempotencyKey),
		WorkflowKey:    workflowSystemToolStringArg(args, "workflowKey"),
		CallerAppName:  workflowSystemToolCallerScope(req),
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
	updatePrincipal := workflowSystemToolManagementPrincipal(req)
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
		permissions := workflowSystemToolPermissionsForTarget(target, req.ProviderName)
		scopedPrincipal, err := workflowSystemToolExactPermissionsPrincipal(req.Principal, permissions, workflowSystemToolTrustedAgentProvider(req, target))
		if err != nil {
			return nil, err
		}
		updatePrincipal = scopedPrincipal
	case hasTarget:
		target, err = workflowSystemToolTargetFromValue(targetValue)
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
		updatePrincipal = scopedPrincipal
		upsertTarget = target
	default:
		definitionID = strings.TrimSpace(existing.Schedule.DefinitionID)
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
		} else {
			target = workflowSystemToolExistingScheduleTarget(existing)
			upsertTarget = target
		}
		if err := workflowSystemToolValidateCreateScope(req, target); err != nil {
			return nil, err
		}
		permissions := workflowSystemToolPermissionsForTarget(target, req.ProviderName)
		scopedPrincipal, err := workflowSystemToolScopedPrincipal(req.Principal, permissions, workflowSystemToolTrustedAgentProvider(req, target))
		if err != nil {
			return nil, err
		}
		if definitionID == "" {
			updatePrincipal = scopedPrincipal
		}
	}
	if definitionID != "" {
		upsertTarget = coreworkflow.Target{}
	}
	schedule, err := t.manager.UpdateSchedule(ctx, updatePrincipal, scheduleID, workflowmanager.ScheduleUpsert{
		ProviderName:  providerName,
		Cron:          cron,
		Timezone:      timezone,
		Target:        upsertTarget,
		DefinitionID:  definitionID,
		Paused:        paused,
		CallerAppName: workflowSystemToolCallerScope(req),
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
		"agent_caller_app", strings.TrimSpace(req.CallerAppName),
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
			"credential_subject_id", strings.TrimSpace(p.CredentialSubjectID),
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
			"workflow_target", workflowSystemToolTargetInfo(definition.Definition.Target),
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
			"workflow_definition_id", strings.TrimSpace(schedule.Schedule.DefinitionID),
			"workflow_target", workflowSystemToolTargetInfo(schedule.Schedule.Target),
		)
		if schedule.Schedule.NextRunAt != nil {
			attrs = append(attrs, "workflow_schedule_next_run_at", schedule.Schedule.NextRunAt.UTC().Format(time.RFC3339Nano))
		}
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
			"workflow_definition_id", strings.TrimSpace(run.Run.DefinitionID),
			"workflow_target", workflowSystemToolTargetInfo(run.Run.Target),
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
		if definitionID := strings.TrimSpace(coreSchedule.DefinitionID); definitionID != "" {
			value["definitionId"] = definitionID
		}
		value["target"] = workflowSystemToolTargetInfo(coreSchedule.Target)
		workflowSystemToolPutTime(value, "createdAt", coreSchedule.CreatedAt)
		workflowSystemToolPutTime(value, "updatedAt", coreSchedule.UpdatedAt)
		workflowSystemToolPutTime(value, "nextRunAt", coreSchedule.NextRunAt)
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
		if definitionID := strings.TrimSpace(coreRun.DefinitionID); definitionID != "" {
			value["definitionId"] = definitionID
		}
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
	return workflowwire.EncodeTargetMap(target)
}

func workflowSystemToolValueInfo(value coreworkflow.Value) any {
	return workflowwire.EncodeValue(value)
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
		PageToken: workflowSystemToolStringArg(args, "pageToken"),
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

func workflowSystemToolMapDeepClone(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	return workflowwire.CloneJSON(value).(map[string]any)
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
