package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
)

const (
	workflowSystemToolSchedulesCreate = "schedules.create"
	workflowSystemToolSchedulesList   = "schedules.list"
	workflowSystemToolSchedulesGet    = "schedules.get"
	workflowSystemToolSchedulesPause  = "schedules.pause"
	workflowSystemToolSchedulesResume = "schedules.resume"
	workflowSystemToolRunsList        = "runs.list"
	workflowSystemToolRunsGet         = "runs.get"
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
	workflowSystemToolSchedulesCreate: {
		Operation:        workflowSystemToolSchedulesCreate,
		Name:             "workflow_schedules_create",
		Description:      "Create a recurring workflow schedule for a delegated target.",
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
	workflowSystemToolRunsList: {
		Operation:        workflowSystemToolRunsList,
		Name:             "workflow_runs_list",
		Description:      "List workflow runs owned by the current caller.",
		ParametersSchema: workflowSystemToolObjectSchema(nil, nil),
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
	case workflowSystemToolSchedulesCreate:
		return t.executeCreateSchedule(ctx, req)
	case workflowSystemToolSchedulesList:
		if err := workflowSystemToolRejectUnknownKeys(req.Arguments, "workflow.schedules.list"); err != nil {
			return nil, err
		}
		schedules, err := t.manager.ListSchedules(ctx, req.Principal)
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
		schedule, err := t.manager.GetSchedule(ctx, req.Principal, scheduleID)
		if err != nil {
			return nil, err
		}
		return workflowSystemToolJSONResponse(http.StatusOK, map[string]any{"schedule": workflowSystemToolScheduleInfo(schedule)})
	case workflowSystemToolSchedulesPause:
		if err := workflowSystemToolRejectUnknownKeys(req.Arguments, "workflow.schedules.pause", "scheduleId"); err != nil {
			return nil, err
		}
		scheduleID := workflowSystemToolStringArg(req.Arguments, "scheduleId")
		if scheduleID == "" {
			return nil, fmt.Errorf("%w: scheduleId is required", invocation.ErrInvalidInvocation)
		}
		schedule, err := t.manager.PauseSchedule(ctx, req.Principal, scheduleID)
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
		schedule, err := t.manager.ResumeSchedule(ctx, req.Principal, scheduleID)
		if err != nil {
			return nil, err
		}
		return workflowSystemToolJSONResponse(http.StatusOK, map[string]any{"schedule": workflowSystemToolScheduleInfo(schedule)})
	case workflowSystemToolRunsList:
		if err := workflowSystemToolRejectUnknownKeys(req.Arguments, "workflow.runs.list"); err != nil {
			return nil, err
		}
		runs, err := t.manager.ListRuns(ctx, req.Principal)
		if err != nil {
			return nil, err
		}
		items := make([]map[string]any, 0, len(runs))
		for _, run := range runs {
			items = append(items, workflowSystemRunInfo(run))
		}
		return workflowSystemToolJSONResponse(http.StatusOK, map[string]any{"runs": items})
	case workflowSystemToolRunsGet:
		if err := workflowSystemToolRejectUnknownKeys(req.Arguments, "workflow.runs.get", "runId"); err != nil {
			return nil, err
		}
		runID := workflowSystemToolStringArg(req.Arguments, "runId")
		if runID == "" {
			return nil, fmt.Errorf("%w: runId is required", invocation.ErrInvalidInvocation)
		}
		run, err := t.manager.GetRun(ctx, req.Principal, runID)
		if err != nil {
			return nil, err
		}
		return workflowSystemToolJSONResponse(http.StatusOK, map[string]any{"run": workflowSystemRunInfo(run)})
	default:
		return nil, fmt.Errorf("%w: workflow system operation %q is not supported", invocation.ErrOperationNotFound, req.Tool.Target.Operation)
	}
}

func (t *workflowSystemTools) executeCreateSchedule(ctx context.Context, req agentSystemToolExecutionRequest) (*coreagent.ExecuteToolResponse, error) {
	args := req.Arguments
	if err := workflowSystemToolRejectUnknownKeys(args, "workflow.schedules.create", "provider", "cron", "timezone", "paused", "target"); err != nil {
		return nil, err
	}
	cron := workflowSystemToolStringArg(args, "cron")
	if cron == "" {
		return nil, fmt.Errorf("%w: cron is required", invocation.ErrInvalidInvocation)
	}
	targetValue, ok := args["target"]
	if !ok {
		return nil, fmt.Errorf("%w: target is required", invocation.ErrInvalidInvocation)
	}
	target, err := workflowSystemToolTargetFromValue(targetValue)
	if err != nil {
		return nil, err
	}
	if err := workflowSystemToolValidateCreateScope(req, target); err != nil {
		return nil, err
	}
	permissions := workflowSystemToolPermissionsForTarget(target)
	scopedPrincipal, err := workflowSystemToolScopedPrincipal(req.Principal, permissions)
	if err != nil {
		return nil, err
	}
	schedule, err := t.manager.CreateSchedule(ctx, scopedPrincipal, workflowmanager.ScheduleUpsert{
		ProviderName:     workflowSystemToolStringArg(args, "provider"),
		Cron:             cron,
		Timezone:         workflowSystemToolStringArg(args, "timezone"),
		Target:           target,
		Paused:           workflowSystemToolBoolArg(args, "paused"),
		IdempotencyKey:   strings.TrimSpace(req.IdempotencyKey),
		CallerPluginName: workflowSystemToolCallerScope(req.ProviderName),
	})
	if err != nil {
		return nil, err
	}
	return workflowSystemToolJSONResponse(http.StatusCreated, map[string]any{"schedule": workflowSystemToolScheduleInfo(schedule)})
}

func workflowSystemToolCallerScope(providerName string) string {
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		return "agent"
	}
	return "agent:" + providerName
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
	targetSchema := workflowSystemToolObjectSchema([]string{}, map[string]any{
		"plugin": workflowSystemToolObjectSchema([]string{"name", "operation"}, map[string]any{
			"name":       workflowSystemToolStringSchema("Plugin name."),
			"operation":  workflowSystemToolStringSchema("Plugin operation."),
			"connection": workflowSystemToolStringSchema("Connection name."),
			"instance":   workflowSystemToolStringSchema("Instance name."),
			"input":      map[string]any{"type": "object"},
		}),
		"agent": workflowSystemToolObjectSchema([]string{}, map[string]any{
			"provider":        workflowSystemToolStringSchema("Agent provider name."),
			"model":           workflowSystemToolStringSchema("Agent model."),
			"prompt":          workflowSystemToolStringSchema("Agent prompt."),
			"messages":        map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			"toolRefs":        map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			"responseSchema":  map[string]any{"type": "object"},
			"metadata":        map[string]any{"type": "object"},
			"providerOptions": map[string]any{"type": "object"},
			"timeoutSeconds":  map[string]any{"type": "integer", "minimum": 0},
		}),
	})
	return workflowSystemToolObjectSchema([]string{"cron", "target"}, map[string]any{
		"provider": workflowSystemToolStringSchema("Workflow provider name."),
		"cron":     workflowSystemToolStringSchema("Cron expression."),
		"timezone": workflowSystemToolStringSchema("IANA timezone."),
		"paused":   map[string]any{"type": "boolean"},
		"target":   targetSchema,
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
	if err := workflowSystemToolRejectUnknownKeys(target, "target", "plugin", "agent"); err != nil {
		return coreworkflow.Target{}, err
	}
	pluginValue, hasPlugin := target["plugin"]
	agentValue, hasAgent := target["agent"]
	if hasPlugin == hasAgent {
		return coreworkflow.Target{}, fmt.Errorf("%w: target must set exactly one of plugin or agent", invocation.ErrInvalidInvocation)
	}
	if hasPlugin {
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
			return coreworkflow.Target{}, fmt.Errorf("%w: target.plugin.name and target.plugin.operation are required", invocation.ErrInvalidInvocation)
		}
		input, err := workflowSystemToolObjectArg(pluginMap, "input", "target.plugin")
		if err != nil {
			return coreworkflow.Target{}, err
		}
		return coreworkflow.Target{Plugin: &coreworkflow.PluginTarget{
			PluginName: pluginName,
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
	if err := workflowSystemToolRejectUnknownKeys(agentMap, "target.agent", "provider", "model", "prompt", "messages", "toolRefs", "responseSchema", "metadata", "providerOptions", "timeoutSeconds"); err != nil {
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
	providerOptions, err := workflowSystemToolObjectArg(agentMap, "providerOptions", "target.agent")
	if err != nil {
		return coreworkflow.Target{}, err
	}
	return coreworkflow.Target{Agent: &coreworkflow.AgentTarget{
		ProviderName:    workflowSystemToolStringArg(agentMap, "provider"),
		Model:           workflowSystemToolStringArg(agentMap, "model"),
		Prompt:          workflowSystemToolStringArg(agentMap, "prompt"),
		Messages:        messages,
		ToolRefs:        toolRefs,
		ResponseSchema:  responseSchema,
		Metadata:        metadata,
		ProviderOptions: providerOptions,
		TimeoutSeconds:  workflowSystemToolIntArg(agentMap, "timeoutSeconds"),
	}}, nil
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
		if err := workflowSystemToolRejectUnknownKeys(refMap, fmt.Sprintf("agent toolRefs[%d]", i), "system", "plugin", "operation", "connection", "instance", "title", "description"); err != nil {
			return nil, err
		}
		out = append(out, coreagent.ToolRef{
			System:      workflowSystemToolStringArg(refMap, "system"),
			Plugin:      workflowSystemToolStringArg(refMap, "plugin"),
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
	if target.Plugin != nil {
		if workflowSystemToolPluginTargetAllowed(*target.Plugin, req.ToolRefs, req.Tools) {
			return nil
		}
		return fmt.Errorf("%w: workflow target %s.%s is outside the current agent tool scope", invocation.ErrScopeDenied, target.Plugin.PluginName, target.Plugin.Operation)
	}
	if target.Agent == nil {
		return fmt.Errorf("%w: workflow target is required", invocation.ErrInvalidInvocation)
	}
	for i := range target.Agent.ToolRefs {
		ref := target.Agent.ToolRefs[i]
		if strings.TrimSpace(ref.System) != "" {
			if err := workflowSystemToolValidateFutureSystemRef(i, ref, req); err != nil {
				return err
			}
			continue
		}
		if strings.TrimSpace(ref.Plugin) == "" || strings.TrimSpace(ref.Plugin) == "*" || strings.TrimSpace(ref.Operation) == "" {
			return fmt.Errorf("%w: target.agent.toolRefs[%d] must be an exact plugin operation", invocation.ErrInvalidInvocation, i)
		}
		if ref.CredentialMode != "" {
			return fmt.Errorf("%w: target.agent.toolRefs[%d] credentialMode is not supported for scheduled agent targets", invocation.ErrInvalidInvocation, i)
		}
		pluginTarget := coreworkflow.PluginTarget{
			PluginName: ref.Plugin,
			Operation:  ref.Operation,
			Connection: ref.Connection,
			Instance:   ref.Instance,
		}
		if !workflowSystemToolPluginTargetAllowed(pluginTarget, req.ToolRefs, req.Tools) {
			return fmt.Errorf("%w: target.agent.toolRefs[%d] %s.%s is outside the current agent tool scope", invocation.ErrScopeDenied, i, ref.Plugin, ref.Operation)
		}
	}
	return nil
}

func workflowSystemToolValidateFutureSystemRef(index int, ref coreagent.ToolRef, req agentSystemToolExecutionRequest) error {
	if strings.TrimSpace(ref.System) != coreagent.SystemToolWorkflow || strings.TrimSpace(ref.Operation) == "" {
		return fmt.Errorf("%w: target.agent.toolRefs[%d] workflow system refs require an exact operation", invocation.ErrInvalidInvocation, index)
	}
	if strings.TrimSpace(ref.Plugin) != "" || strings.TrimSpace(ref.Connection) != "" || strings.TrimSpace(ref.Instance) != "" || ref.CredentialMode != "" {
		return fmt.Errorf("%w: target.agent.toolRefs[%d] system refs cannot include plugin, connection, instance, or credentialMode", invocation.ErrInvalidInvocation, index)
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
	return fmt.Errorf("%w: target.agent.toolRefs[%d] workflow.%s is outside the current agent tool scope", invocation.ErrScopeDenied, index, ref.Operation)
}

func workflowSystemToolPluginTargetAllowed(target coreworkflow.PluginTarget, refs []coreagent.ToolRef, tools []coreagent.Tool) bool {
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

func workflowSystemToolPluginRefMatchesTarget(ref coreagent.ToolRef, target coreworkflow.PluginTarget) bool {
	if strings.TrimSpace(ref.System) != "" || strings.TrimSpace(ref.Plugin) == "" || strings.TrimSpace(ref.Plugin) == "*" || strings.TrimSpace(ref.Operation) == "" {
		return false
	}
	if ref.CredentialMode != "" {
		return false
	}
	if strings.TrimSpace(ref.Plugin) != strings.TrimSpace(target.PluginName) || strings.TrimSpace(ref.Operation) != strings.TrimSpace(target.Operation) {
		return false
	}
	return workflowSystemToolRefBindingMatchesTarget(ref.Connection, ref.Instance, target.Connection, target.Instance)
}

func workflowSystemToolResolvedToolMatchesTarget(tool coreagent.Tool, target coreworkflow.PluginTarget) bool {
	if strings.TrimSpace(tool.Target.System) != "" || strings.TrimSpace(tool.Target.Plugin) == "" || strings.TrimSpace(tool.Target.Operation) == "" {
		return false
	}
	if tool.Target.CredentialMode != "" {
		return false
	}
	if strings.TrimSpace(tool.Target.Plugin) != strings.TrimSpace(target.PluginName) || strings.TrimSpace(tool.Target.Operation) != strings.TrimSpace(target.Operation) {
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

func workflowSystemToolPermissionsForTarget(target coreworkflow.Target) []core.AccessPermission {
	operationsByPlugin := map[string]map[string]struct{}{}
	add := func(pluginName, operation string) {
		pluginName = strings.TrimSpace(pluginName)
		operation = strings.TrimSpace(operation)
		if pluginName == "" || operation == "" {
			return
		}
		if operationsByPlugin[pluginName] == nil {
			operationsByPlugin[pluginName] = map[string]struct{}{}
		}
		operationsByPlugin[pluginName][operation] = struct{}{}
	}
	if target.Plugin != nil {
		add(target.Plugin.PluginName, target.Plugin.Operation)
	}
	if target.Agent != nil {
		for i := range target.Agent.ToolRefs {
			ref := target.Agent.ToolRefs[i]
			if strings.TrimSpace(ref.System) == "" {
				add(ref.Plugin, ref.Operation)
			}
		}
	}
	if len(operationsByPlugin) == 0 {
		return nil
	}
	plugins := slices.Sorted(maps.Keys(operationsByPlugin))
	out := make([]core.AccessPermission, 0, len(plugins))
	for _, pluginName := range plugins {
		operations := slices.Sorted(maps.Keys(operationsByPlugin[pluginName]))
		out = append(out, core.AccessPermission{Plugin: pluginName, Operations: operations})
	}
	return out
}

func workflowSystemToolScopedPrincipal(p *principal.Principal, permissions []core.AccessPermission) (*principal.Principal, error) {
	p = principal.Canonicalized(p)
	if p == nil || strings.TrimSpace(p.SubjectID) == "" {
		return nil, fmt.Errorf("%w: agent execution principal is required", invocation.ErrAuthorizationDenied)
	}
	if len(permissions) == 0 {
		return p, nil
	}
	requested := principal.CompilePermissions(permissions)
	if requested == nil {
		return p, nil
	}
	if p.TokenPermissions != nil {
		requested = principal.IntersectPermissions(requested, p.TokenPermissions)
		if len(requested) == 0 {
			return nil, fmt.Errorf("%w: workflow target is outside the caller permission scope", invocation.ErrScopeDenied)
		}
	}
	next := *p
	next.TokenPermissions = requested
	next.Scopes = principal.PermissionPlugins(requested)
	return principal.Canonicalize(&next), nil
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
	if target.Plugin != nil {
		plugin := target.Plugin
		value["plugin"] = map[string]any{
			"name":       plugin.PluginName,
			"operation":  plugin.Operation,
			"connection": plugin.Connection,
			"instance":   plugin.Instance,
			"input":      maps.Clone(plugin.Input),
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
		if pluginName := strings.TrimSpace(ref.Plugin); pluginName != "" {
			value["plugin"] = pluginName
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

func workflowSystemToolBoolArg(args map[string]any, key string) bool {
	value, ok := args[key]
	if !ok {
		return false
	}
	result, _ := value.(bool)
	return result
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
