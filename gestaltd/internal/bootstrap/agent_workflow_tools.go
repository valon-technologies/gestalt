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

func (t *workflowSystemTools) ExecuteSystemTool(ctx context.Context, req agentSystemToolExecutionRequest) (*coreagent.ExecuteToolResponse, error) {
	if !t.Available() {
		return nil, agentmanager.ErrAgentWorkflowToolsNotConfigured
	}
	if strings.TrimSpace(req.Tool.Target.System) != coreagent.SystemToolWorkflow {
		return nil, fmt.Errorf("%w: unsupported agent system tool %q", invocation.ErrInvalidInvocation, req.Tool.Target.System)
	}
	switch strings.TrimSpace(req.Tool.Target.Operation) {
	case workflowSystemToolDefinitionsApply:
		return t.executeApplyDefinition(ctx, req)
	case workflowSystemToolDefinitionsGet:
		return t.executeGetDefinition(ctx, req)
	case workflowSystemToolDefinitionsList:
		if err := workflowSystemToolRejectUnknownKeys(req.Arguments, "workflow.definitions.list"); err != nil {
			return nil, err
		}
		resp, err := t.manager.ListDefinitions(ctx, workflowSystemToolManagementPrincipal(req))
		if err != nil {
			return nil, err
		}
		if resp == nil {
			resp = &workflowmanager.ListDefinitionsResponse{}
		}
		items := make([]map[string]any, 0, len(resp.Definitions))
		for _, definition := range resp.Definitions {
			items = append(items, workflowSystemToolDefinitionInfo(definition))
		}
		return workflowSystemToolJSONResponse(http.StatusOK, map[string]any{"definitions": items})
	case workflowSystemToolDefinitionsSetPaused:
		return t.executeSetDefinitionPaused(ctx, req)
	case workflowSystemToolDefinitionsSetActivationPaused:
		return t.executeSetActivationPaused(ctx, req)
	case workflowSystemToolDefinitionsDelete:
		return t.executeDeleteDefinition(ctx, req)
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

func (t *workflowSystemTools) executeApplyDefinition(ctx context.Context, req agentSystemToolExecutionRequest) (*coreagent.ExecuteToolResponse, error) {
	args := req.Arguments
	if err := workflowSystemToolRejectUnknownKeys(args, "workflow.definitions.apply", "definitionId", "provider", "target", "paused"); err != nil {
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
	definition, err := t.manager.ApplyDefinition(ctx, scopedPrincipal, workflowmanager.DefinitionApply{
		ProviderName:   workflowSystemToolStringArg(args, "provider"),
		Spec:           coreworkflow.DefinitionSpec{ID: definitionID, Target: target, Paused: workflowSystemToolBoolArg(args, "paused")},
		IdempotencyKey: strings.TrimSpace(req.IdempotencyKey),
		Caller:         workflowSystemToolCaller(req),
	})
	if err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "agent workflow system tool applied definition", workflowSystemToolDefinitionLogAttrs(req, definition)...)
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

func (t *workflowSystemTools) executeSetDefinitionPaused(ctx context.Context, req agentSystemToolExecutionRequest) (*coreagent.ExecuteToolResponse, error) {
	args := req.Arguments
	if err := workflowSystemToolRejectUnknownKeys(args, "workflow.definitions.setPaused", "definitionId", "paused"); err != nil {
		return nil, err
	}
	definitionID := workflowSystemToolStringArg(args, "definitionId")
	if definitionID == "" {
		return nil, fmt.Errorf("%w: definitionId is required", invocation.ErrInvalidInvocation)
	}
	paused, _, err := workflowSystemToolBoolArgPresent(args, "paused", "workflow.definitions.setPaused")
	if err != nil {
		return nil, err
	}
	definition, err := t.manager.SetDefinitionPaused(ctx, workflowSystemToolManagementPrincipal(req), definitionID, paused)
	if err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "agent workflow system tool set definition paused", workflowSystemToolDefinitionLogAttrs(req, definition)...)
	return workflowSystemToolJSONResponse(http.StatusOK, map[string]any{"definition": workflowSystemToolDefinitionInfo(definition)})
}

func (t *workflowSystemTools) executeSetActivationPaused(ctx context.Context, req agentSystemToolExecutionRequest) (*coreagent.ExecuteToolResponse, error) {
	args := req.Arguments
	if err := workflowSystemToolRejectUnknownKeys(args, "workflow.definitions.setActivationPaused", "definitionId", "activationId", "paused"); err != nil {
		return nil, err
	}
	definitionID := workflowSystemToolStringArg(args, "definitionId")
	if definitionID == "" {
		return nil, fmt.Errorf("%w: definitionId is required", invocation.ErrInvalidInvocation)
	}
	activationID := workflowSystemToolStringArg(args, "activationId")
	if activationID == "" {
		return nil, fmt.Errorf("%w: activationId is required", invocation.ErrInvalidInvocation)
	}
	paused, _, err := workflowSystemToolBoolArgPresent(args, "paused", "workflow.definitions.setActivationPaused")
	if err != nil {
		return nil, err
	}
	definition, err := t.manager.SetActivationPaused(ctx, workflowSystemToolManagementPrincipal(req), definitionID, activationID, paused)
	if err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "agent workflow system tool set activation paused", workflowSystemToolDefinitionLogAttrs(req, definition)...)
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

func (t *workflowSystemTools) executeStartRun(ctx context.Context, req agentSystemToolExecutionRequest) (*coreagent.ExecuteToolResponse, error) {
	args := req.Arguments
	if err := workflowSystemToolRejectUnknownKeys(args, "workflow.runs.start", "provider", "workflowKey", "definitionId"); err != nil {
		return nil, err
	}
	definitionID := workflowSystemToolStringArg(args, "definitionId")
	if definitionID == "" {
		return nil, fmt.Errorf("%w: definitionId is required", invocation.ErrInvalidInvocation)
	}

	definitionPrincipal := workflowSystemToolPrincipalWithTrustedProvider(req.Principal, strings.TrimSpace(req.ProviderName))
	definition, err := t.manager.GetDefinition(ctx, definitionPrincipal, definitionID)
	if err != nil {
		return nil, err
	}
	if definition == nil || definition.Definition == nil {
		return nil, core.ErrNotFound
	}
	target := definition.Definition.Target
	if err := workflowSystemToolValidateCreateScope(req, target); err != nil {
		return nil, err
	}
	permissions := workflowSystemToolPermissionsForTarget(target, req.ProviderName)
	scopedPrincipal, err := workflowSystemToolExactPermissionsPrincipal(req.Principal, permissions, workflowSystemToolTrustedAgentProvider(req, target))
	if err != nil {
		return nil, err
	}
	run, err := t.manager.StartRun(ctx, scopedPrincipal, workflowmanager.RunStart{
		ProviderName:   workflowSystemToolStringArg(args, "provider"),
		DefinitionID:   definitionID,
		IdempotencyKey: strings.TrimSpace(req.IdempotencyKey),
		WorkflowKey:    workflowSystemToolStringArg(args, "workflowKey"),
		Caller:         workflowSystemToolCaller(req),
	})
	if err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "agent workflow system tool started run", workflowSystemToolRunLogAttrs(req, run)...)
	return workflowSystemToolJSONResponse(http.StatusCreated, map[string]any{"run": workflowSystemRunInfo(run)})
}

func workflowSystemToolBaseLogAttrs(req agentSystemToolExecutionRequest) []any {
	attrs := []any{
		"agent_provider", strings.TrimSpace(req.ProviderName),
		"agent_caller_kind", strings.TrimSpace(string(req.CallerKind)),
		"agent_caller_name", strings.TrimSpace(req.CallerName),
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

func workflowSystemToolCaller(req agentSystemToolExecutionRequest) invocation.CallerProvider {
	callerKind := invocation.ProviderKind(strings.TrimSpace(string(req.CallerKind)))
	callerName := strings.TrimSpace(req.CallerName)
	if callerKind != "" && callerName != "" {
		return invocation.CallerProvider{Kind: callerKind, Name: callerName}
	}
	providerName := strings.TrimSpace(req.ProviderName)
	if providerName == "" {
		providerName = "agent"
	}
	return invocation.CallerProvider{Kind: invocation.ProviderKindAgent, Name: providerName}
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
		if coreRun.Output != nil {
			value["output"] = coreRun.Output
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
