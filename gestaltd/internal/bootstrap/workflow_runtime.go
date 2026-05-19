package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/jsonvalue"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowprincipal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type workflowRuntime struct {
	mu                  sync.RWMutex
	defaultProviderName string
	providers           map[string]coreworkflow.Provider
	startupWaits        *startupWaitTracker
	invoker             invocation.Invoker
	agentManager        agentmanager.Service
}

const (
	workflowSignalContextMaxSignals     = 10
	workflowSignalContextMaxItems       = 20
	workflowSignalContextMaxDepth       = 4
	workflowSignalContextMaxStringBytes = 4096
)

func newWorkflowRuntime(cfg *config.Config) (*workflowRuntime, error) {
	runtime := &workflowRuntime{
		providers:    map[string]coreworkflow.Provider{},
		startupWaits: newStartupWaitTracker(),
	}
	if cfg != nil {
		selectedProviderName, _, err := cfg.SelectedWorkflowProvider()
		if err == nil {
			runtime.defaultProviderName = strings.TrimSpace(selectedProviderName)
		}
	}
	return runtime, nil
}

func (r *workflowRuntime) InitProviderPlaceholders(defs map[string]*config.ProviderEntry) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.providers == nil {
		r.providers = map[string]coreworkflow.Provider{}
	}
	for name, entry := range defs {
		if entry == nil {
			continue
		}
		if _, exists := r.providers[name]; exists {
			continue
		}
		r.providers[name] = newStartupWorkflowProviderProxy(name, r.startupWaits)
	}
}

func (r *workflowRuntime) PublishProvider(name string, provider coreworkflow.Provider) {
	if r == nil || strings.TrimSpace(name) == "" || provider == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if proxy, ok := r.providers[name].(*startupWorkflowProviderProxy); ok {
		proxy.publish(provider)
	}
	r.providers[name] = provider
}

func (r *workflowRuntime) FailProvider(name string, err error) {
	if r == nil || strings.TrimSpace(name) == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if proxy, ok := r.providers[name].(*startupWorkflowProviderProxy); ok {
		proxy.fail(err)
	}
	delete(r.providers, name)
}

func (r *workflowRuntime) FailPendingProviders(err error) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for name, provider := range r.providers {
		proxy, ok := provider.(*startupWorkflowProviderProxy)
		if !ok {
			continue
		}
		proxy.fail(err)
		delete(r.providers, name)
	}
}

func (r *workflowRuntime) StartupWaitTracker() *startupWaitTracker {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.startupWaits
}

func (r *workflowRuntime) SetInvoker(invoker invocation.Invoker) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.invoker = invoker
}

func (r *workflowRuntime) SetAgentManager(agentManager agentmanager.Service) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agentManager = agentManager
}

func (r *workflowRuntime) HasConfiguredProviders() bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.providers) > 0
}

func (r *workflowRuntime) ResolveProvider(name string) (coreworkflow.Provider, error) {
	if r == nil {
		return nil, fmt.Errorf("workflow runtime is not configured")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	provider, ok := r.providers[strings.TrimSpace(name)]
	if !ok || provider == nil {
		return nil, fmt.Errorf("workflow provider %q is not available", name)
	}
	return provider, nil
}

func (r *workflowRuntime) ResolveProviderSelection(name string) (string, coreworkflow.Provider, error) {
	if r == nil {
		return "", nil, fmt.Errorf("workflow runtime is not configured")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	selectedName := strings.TrimSpace(name)
	if selectedName == "" {
		selectedName = strings.TrimSpace(r.defaultProviderName)
	}
	if selectedName == "" {
		return "", nil, fmt.Errorf("workflow provider is required")
	}
	provider, ok := r.providers[selectedName]
	if !ok || provider == nil {
		return "", nil, fmt.Errorf("workflow provider %q is not available", selectedName)
	}
	return selectedName, provider, nil
}

func (r *workflowRuntime) ProviderNames() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		if strings.TrimSpace(name) == "" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (r *workflowRuntime) Invoke(ctx context.Context, req coreworkflow.InvokeOperationRequest) (*coreworkflow.InvokeOperationResponse, error) {
	if r == nil {
		return nil, fmt.Errorf("workflow runtime is not configured")
	}
	r.mu.RLock()
	invoker := r.invoker
	agentManager := r.agentManager
	r.mu.RUnlock()
	principalValue := principal.Canonicalized(principal.FromContext(ctx))
	target := req.Target
	callerPluginName := ""
	if strings.TrimSpace(req.ExecutionRef) != "" {
		resolvedRef, err := r.resolveWorkflowExecutionRef(ctx, req)
		if err != nil {
			return nil, err
		}
		principalValue = workflowprincipal.RuntimePrincipalFromExecutionReference(resolvedRef)
		target = resolvedRef.Target
		callerPluginName = strings.TrimSpace(resolvedRef.CallerPluginName)
		ctx = workflowExecutionRefWithRunAsAudit(ctx, resolvedRef)
		if workflowExecutionRefAllowsInternalConnectionAccess(resolvedRef) {
			ctx = invocation.WithInternalConnectionAccess(ctx)
		}
	} else if principalValue == nil || strings.TrimSpace(principalValue.SubjectID) == "" {
		return nil, fmt.Errorf("%w: workflow execution principal is required when execution_ref is omitted", invocation.ErrInternal)
	}
	if len(target.Steps) == 0 {
		return nil, fmt.Errorf("workflow target steps are required")
	}
	return r.invokeWorkflowSteps(ctx, req, target, agentManager, invoker, principalValue, callerPluginName)
}

const workflowStepOutputBodyMaxBytes = 64 * 1024

type workflowStepResult struct {
	ID            string             `json:"id"`
	Status        string             `json:"status"`
	TurnID        string             `json:"turnId,omitempty"`
	SkippedReason string             `json:"skippedReason,omitempty"`
	Error         *workflowStepError `json:"error,omitempty"`
}

type workflowStepError struct {
	StepID  string `json:"stepId,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type workflowStepsResult struct {
	Version     int                  `json:"version"`
	Status      string               `json:"status"`
	Steps       []workflowStepResult `json:"steps"`
	Outputs     map[string]any       `json:"outputs"`
	FinalStepID string               `json:"finalStepId,omitempty"`
	FinalOutput any                  `json:"finalOutput,omitempty"`
	Error       *workflowStepError   `json:"error,omitempty"`
}

type workflowAgentSessionState struct {
	session      *coreagent.Session
	providerName string
	model        string
	options      string
}

func (r *workflowRuntime) invokeWorkflowSteps(ctx context.Context, req coreworkflow.InvokeOperationRequest, target coreworkflow.Target, agentManager agentmanager.Service, invoker invocation.Invoker, p *principal.Principal, callerPluginName string) (*coreworkflow.InvokeOperationResponse, error) {
	result := workflowStepsResult{
		Version: 1,
		Status:  "succeeded",
		Steps:   make([]workflowStepResult, 0, len(target.Steps)),
		Outputs: map[string]any{},
	}
	skipped := map[string]struct{}{}
	sessions := map[string]workflowAgentSessionState{}
	invocationScope := workflowStepInvocationScope(req)
	for i := range target.Steps {
		step := target.Steps[i]
		stepID := strings.TrimSpace(step.ID)
		if stepID == "" {
			return workflowFailedStepResponse(result, stepID, "invalid_step", "workflow step id is required")
		}
		runStep, skipReason, err := workflowStepWhenMatches(step.When, req, result.Outputs, skipped)
		if err != nil {
			return workflowFailedStepResponse(result, stepID, "invalid_when", err.Error())
		}
		if !runStep {
			result.Steps = append(result.Steps, workflowStepResult{ID: stepID, Status: "skipped", SkippedReason: skipReason})
			skipped[stepID] = struct{}{}
			continue
		}
		inputs, err := workflowEvaluateStepInputs(step.Inputs, req, result.Outputs)
		if err != nil {
			return workflowFailedStepResponse(result, stepID, "invalid_input", err.Error())
		}
		stepCtx := ctx
		cancelStep := func() {}
		if step.TimeoutSeconds > 0 {
			stepCtx, cancelStep = context.WithTimeout(ctx, time.Duration(step.TimeoutSeconds)*time.Second)
		}
		var output any
		var turnID string
		switch {
		case step.Plugin != nil && step.Agent != nil:
			cancelStep()
			return workflowFailedStepResponse(result, stepID, "invalid_step", "workflow step cannot set both plugin and agent")
		case step.Plugin != nil:
			output, err = r.invokeWorkflowPluginStep(stepCtx, req, invoker, p, step.Plugin, inputs, result.Outputs, invocationScope, stepID, "step")
		case step.Agent != nil:
			output, turnID, err = r.invokeWorkflowAgentStep(stepCtx, req, agentManager, p, callerPluginName, step.Agent, inputs, sessions, invocationScope, stepID, step.TimeoutSeconds, step.Metadata)
		default:
			err = fmt.Errorf("workflow step must set plugin or agent")
		}
		cancelStep()
		if err != nil {
			return workflowFailedStepResponse(result, stepID, "step_failed", err.Error())
		}
		result.Outputs[stepID] = output
		if step.OutputDelivery != nil {
			if step.OutputDelivery.Plugin == nil {
				delete(result.Outputs, stepID)
				return workflowFailedStepResponse(result, stepID, "delivery_failed", "output_delivery.plugin is required")
			}
			if _, err := r.invokeWorkflowPluginStep(ctx, req, invoker, p, step.OutputDelivery.Plugin, inputs, result.Outputs, invocationScope, stepID, "output_delivery"); err != nil {
				delete(result.Outputs, stepID)
				return workflowFailedStepResponse(result, stepID, "delivery_failed", err.Error())
			}
		}
		result.FinalStepID = stepID
		result.FinalOutput = output
		result.Steps = append(result.Steps, workflowStepResult{ID: stepID, Status: "succeeded", TurnID: turnID})
	}
	body, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return &coreworkflow.InvokeOperationResponse{Status: http.StatusOK, Body: string(body)}, nil
}

func workflowFailedStepResponse(result workflowStepsResult, stepID, code, message string) (*coreworkflow.InvokeOperationResponse, error) {
	errValue := &workflowStepError{StepID: stepID, Code: code, Message: message}
	result.Status = "failed"
	result.Error = errValue
	if stepID != "" {
		result.Steps = append(result.Steps, workflowStepResult{ID: stepID, Status: "failed", Error: errValue})
	}
	body, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return &coreworkflow.InvokeOperationResponse{Status: http.StatusInternalServerError, Body: string(body)}, nil
}

func (r *workflowRuntime) invokeWorkflowPluginStep(ctx context.Context, req coreworkflow.InvokeOperationRequest, invoker invocation.Invoker, p *principal.Principal, plugin *coreworkflow.PluginCall, inputs map[string]any, outputs map[string]any, invocationScope, stepID, fieldName string) (any, error) {
	if invoker == nil {
		return nil, fmt.Errorf("%w: workflow %s requires an invoker", invocation.ErrInternal, fieldName)
	}
	pluginName := strings.TrimSpace(plugin.Name)
	operation := strings.TrimSpace(plugin.Operation)
	if pluginName == "" || operation == "" {
		return nil, fmt.Errorf("%w: workflow %s plugin name and operation are required", invocation.ErrInvalidInvocation, fieldName)
	}
	paramsValue, ok, err := workflowEvaluateValue(plugin.Input, req, outputs, inputs, true)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%w: workflow %s plugin input did not resolve", invocation.ErrInvalidInvocation, fieldName)
	}
	params := map[string]any{}
	if paramsValue != nil {
		object, ok := paramsValue.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: workflow %s plugin input must resolve to an object", invocation.ErrInvalidInvocation, fieldName)
		}
		params = object
	}
	if fieldName == "step" && req.Input != nil {
		params = maps.Clone(params)
		for key, value := range req.Input {
			params[key] = value
		}
	}
	if contextValue := workflowInvocationContext(req); len(contextValue) > 0 {
		ctx = invocation.WithWorkflowContext(ctx, contextValue)
	}
	if connection := strings.TrimSpace(plugin.Connection); connection != "" {
		ctx = invocation.WithConnection(ctx, connection)
	}
	if mode := core.NormalizeOptionalConnectionMode(plugin.CredentialMode); mode != "" {
		ctx = invocation.WithCredentialModeOverride(ctx, mode)
	}
	ctx = invocation.WithIdempotencyKey(ctx, workflowStepIdempotencyKey(req, invocationScope, stepID, fieldName))
	result, err := invoker.Invoke(ctx, p, pluginName, strings.TrimSpace(plugin.Instance), operation, params)
	if err != nil {
		return nil, err
	}
	output := workflowPluginOutputEnvelope(result)
	if result != nil && result.Status >= http.StatusBadRequest {
		return nil, fmt.Errorf("workflow %s plugin %s.%s returned status %d", fieldName, pluginName, operation, result.Status)
	}
	return output, nil
}

func (r *workflowRuntime) invokeWorkflowAgentStep(ctx context.Context, req coreworkflow.InvokeOperationRequest, agentManager agentmanager.Service, p *principal.Principal, callerPluginName string, agent *coreworkflow.AgentTurn, inputs map[string]any, sessions map[string]workflowAgentSessionState, invocationScope, stepID string, timeoutSeconds int, stepMetadata map[string]any) (any, string, error) {
	if agentManager == nil {
		return nil, "", fmt.Errorf("workflow runtime agent manager is not configured")
	}
	ctx = runtimehost.WithWorkflowAgentProviderDeadline(ctx)
	providerName := strings.TrimSpace(agent.ProviderName)
	model := strings.TrimSpace(agent.Model)
	if providerName == "" {
		return nil, "", fmt.Errorf("%w: workflow agent provider is required", invocation.ErrInvalidInvocation)
	}
	sessionKey := strings.TrimSpace(agent.SessionKey)
	if sessionKey == "" {
		sessionKey = stepID
	}
	optionsKey := workflowStableJSON(agent.ModelOptions)
	state, ok := sessions[sessionKey]
	if ok {
		if state.providerName != providerName || state.model != model || state.options != optionsKey {
			return nil, "", fmt.Errorf("%w: workflow agent session_key %q uses incompatible provider, model, or model_options", invocation.ErrInvalidInvocation, sessionKey)
		}
	} else {
		metadata := map[string]any{"workflow": workflowInvocationContext(req), "workflowStepId": stepID}
		session, err := agentManager.CreateSession(ctx, p, coreagent.ManagerCreateSessionRequest{
			ProviderName:   providerName,
			Model:          model,
			Metadata:       metadata,
			IdempotencyKey: workflowStepIdempotencyKey(req, invocationScope, stepID, "agent-session:"+sessionKey),
		})
		if err != nil {
			return nil, "", err
		}
		state = workflowAgentSessionState{session: session, providerName: providerName, model: model, options: optionsKey}
		sessions[sessionKey] = state
	}
	messages, err := workflowAgentTurnMessages(agent, inputs, req, nil)
	if err != nil {
		return nil, "", err
	}
	turn, err := agentManager.CreateTurn(ctx, p, coreagent.ManagerCreateTurnRequest{
		CallerPluginName:  callerPluginName,
		SessionID:         state.session.ID,
		Model:             model,
		Messages:          messages,
		ToolRefs:          append([]coreagent.ToolRef(nil), agent.ToolRefs...),
		ToolRefsSet:       true,
		ResponseSchema:    maps.Clone(agent.ResponseSchema),
		ResponseSchemaSet: len(agent.ResponseSchema) > 0,
		Metadata:          maps.Clone(stepMetadata),
		ModelOptions:      maps.Clone(agent.ModelOptions),
		TimeoutSeconds:    timeoutSeconds,
		IdempotencyKey:    workflowStepIdempotencyKey(req, invocationScope, stepID, "agent-turn"),
	})
	if err != nil {
		return nil, "", err
	}
	turn, err = waitForWorkflowAgentTurn(ctx, agentManager, p, turn)
	if err != nil {
		if turn != nil && strings.TrimSpace(turn.ID) != "" {
			_, _ = agentManager.CancelTurn(context.WithoutCancel(ctx), p, turn.ID, err.Error())
		}
		return nil, workflowAgentTurnID(turn), err
	}
	switch turn.Status {
	case coreagent.ExecutionStatusSucceeded:
		return workflowAgentOutputEnvelope(state.session, turn), turn.ID, nil
	case coreagent.ExecutionStatusCanceled:
		return nil, turn.ID, fmt.Errorf("workflow agent turn %q was canceled: %s", turn.ID, strings.TrimSpace(turn.StatusMessage))
	case coreagent.ExecutionStatusWaitingForInput:
		_, _ = agentManager.CancelTurn(context.WithoutCancel(ctx), p, turn.ID, "workflow agent step turn cannot wait for input")
		return nil, turn.ID, fmt.Errorf("workflow agent turn %q is waiting for input", turn.ID)
	default:
		return nil, turn.ID, fmt.Errorf("workflow agent turn %q finished with status %q: %s", turn.ID, turn.Status, strings.TrimSpace(turn.StatusMessage))
	}
}

func workflowPluginOutputEnvelope(result *core.OperationResult) map[string]any {
	status := 0
	headers := map[string][]string{}
	body := ""
	if result != nil {
		status = result.Status
		body = result.Body
		for key, values := range result.Headers {
			headers[key] = append([]string(nil), values...)
		}
	}
	bodyText := truncateWorkflowString(body, workflowStepOutputBodyMaxBytes)
	bodyEnvelope := map[string]any{
		"text":      bodyText,
		"truncated": len(body) != len(bodyText),
		"jsonValid": false,
	}
	if len(body) == len(bodyText) {
		var parsed any
		if err := json.Unmarshal([]byte(bodyText), &parsed); err == nil {
			bodyEnvelope["jsonValid"] = true
			bodyEnvelope["json"] = parsed
		}
	}
	return map[string]any{
		"version": 1,
		"kind":    "plugin",
		"plugin": map[string]any{
			"status":  status,
			"headers": headers,
			"body":    bodyEnvelope,
		},
	}
}

func workflowAgentOutputEnvelope(session *coreagent.Session, turn *coreagent.Turn) map[string]any {
	agent := map[string]any{}
	if session != nil {
		agent["sessionId"] = session.ID
	}
	if turn != nil {
		agent["turnId"] = turn.ID
		agent["text"] = turn.OutputText
		agent["structuredOutput"] = maps.Clone(turn.StructuredOutput)
	}
	return map[string]any{"version": 1, "kind": "agent", "agent": agent}
}

func workflowStepWhenMatches(when *coreworkflow.StepWhen, req coreworkflow.InvokeOperationRequest, outputs map[string]any, skipped map[string]struct{}) (bool, string, error) {
	if when == nil {
		return true, "", nil
	}
	if source := when.Value.StepOutput; source != nil {
		if _, ok := skipped[strings.TrimSpace(source.StepID)]; ok {
			return false, "missing_dependency", nil
		}
	}
	value, ok, err := workflowEvaluateValue(when.Value, req, outputs, nil, false)
	if err != nil {
		return false, "", err
	}
	if !ok {
		return false, "", fmt.Errorf("%w: workflow step when value did not resolve", invocation.ErrInvalidInvocation)
	}
	if !jsonvalue.IsScalar(value) || !jsonvalue.IsScalar(when.Equals) {
		return false, "", fmt.Errorf("%w: workflow step when values must be scalar JSON values", invocation.ErrInvalidInvocation)
	}
	if workflowAgentScalarEqual(value, when.Equals) {
		return true, "", nil
	}
	return false, "when_false", nil
}

func workflowEvaluateStepInputs(values map[string]coreworkflow.Value, req coreworkflow.InvokeOperationRequest, outputs map[string]any) (map[string]any, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make(map[string]any, len(values))
	for key := range values {
		resolved, ok, err := workflowEvaluateValue(values[key], req, outputs, nil, false)
		if err != nil {
			return nil, fmt.Errorf("inputs.%s: %w", key, err)
		}
		if !ok {
			return nil, fmt.Errorf("%w: inputs.%s did not resolve", invocation.ErrInvalidInvocation, key)
		}
		out[key] = resolved
	}
	return out, nil
}

func workflowEvaluateValue(value coreworkflow.Value, req coreworkflow.InvokeOperationRequest, outputs map[string]any, inputs map[string]any, allowInputs bool) (any, bool, error) {
	switch {
	case value.LiteralSet:
		return value.Literal, true, nil
	case value.Object != nil:
		out := make(map[string]any, len(value.Object))
		for key := range value.Object {
			resolved, ok, err := workflowEvaluateValue(value.Object[key], req, outputs, inputs, allowInputs)
			if err != nil {
				return nil, false, fmt.Errorf("%s: %w", key, err)
			}
			if !ok {
				return nil, false, nil
			}
			out[key] = resolved
		}
		return out, true, nil
	case value.Array != nil:
		out := make([]any, 0, len(value.Array))
		for i := range value.Array {
			resolved, ok, err := workflowEvaluateValue(value.Array[i], req, outputs, inputs, allowInputs)
			if err != nil {
				return nil, false, fmt.Errorf("[%d]: %w", i, err)
			}
			if !ok {
				return nil, false, nil
			}
			out = append(out, resolved)
		}
		return out, true, nil
	case value.Template != nil:
		rendered, err := workflowRenderTemplate(value.Template.Template, req, outputs, inputs, allowInputs)
		return rendered, err == nil, err
	case strings.TrimSpace(value.RunInput) != "":
		return workflowMapPathValue(req.Input, value.RunInput)
	case strings.TrimSpace(value.SignalPayload) != "":
		signal := workflowOutputDeliverySignal(req.Signals)
		if signal == nil {
			return nil, false, nil
		}
		return workflowMapPathValue(signal.Payload, value.SignalPayload)
	case value.StepOutput != nil:
		stepID := strings.TrimSpace(value.StepOutput.StepID)
		output, ok := outputs[stepID]
		if !ok {
			return nil, false, fmt.Errorf("%w: workflow step output references missing step %q", invocation.ErrInvalidInvocation, stepID)
		}
		return workflowPathValue(output, value.StepOutput.Path)
	default:
		return nil, true, nil
	}
}

func workflowAgentTurnMessages(agent *coreworkflow.AgentTurn, inputs map[string]any, req coreworkflow.InvokeOperationRequest, outputs map[string]any) ([]coreagent.Message, error) {
	messages := make([]coreagent.Message, 0, len(agent.Messages)+1)
	for i := range agent.Messages {
		message := agent.Messages[i]
		text, err := workflowRenderTemplate(message.Text.Template, req, outputs, inputs, true)
		if err != nil {
			return nil, fmt.Errorf("messages[%d].text: %w", i, err)
		}
		messages = append(messages, coreagent.Message{
			Role:     message.Role,
			Text:     text,
			Metadata: maps.Clone(message.Metadata),
		})
	}
	if agent.Prompt.Template != "" {
		text, err := workflowRenderTemplate(agent.Prompt.Template, req, outputs, inputs, true)
		if err != nil {
			return nil, fmt.Errorf("prompt: %w", err)
		}
		messages = append(messages, coreagent.Message{Role: "user", Text: text})
	}
	return messages, nil
}

func workflowRenderTemplate(template string, req coreworkflow.InvokeOperationRequest, outputs map[string]any, inputs map[string]any, allowInputs bool) (string, error) {
	var b strings.Builder
	for i := 0; i < len(template); {
		if strings.HasPrefix(template[i:], "$${") {
			b.WriteString("${")
			i += 3
			continue
		}
		if !strings.HasPrefix(template[i:], "${") {
			b.WriteByte(template[i])
			i++
			continue
		}
		end := strings.IndexByte(template[i+2:], '}')
		if end < 0 {
			return "", fmt.Errorf("%w: unterminated template expression", invocation.ErrInvalidInvocation)
		}
		expr := strings.TrimSpace(template[i+2 : i+2+end])
		value, ok, err := workflowTemplateExpressionValue(expr, req, outputs, inputs, allowInputs)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", fmt.Errorf("%w: template expression %q did not resolve", invocation.ErrInvalidInvocation, expr)
		}
		rendered, err := workflowTemplateRenderValue(value)
		if err != nil {
			return "", err
		}
		b.WriteString(rendered)
		i += 2 + end + 1
	}
	return b.String(), nil
}

func workflowTemplateExpressionValue(expr string, req coreworkflow.InvokeOperationRequest, outputs map[string]any, inputs map[string]any, allowInputs bool) (any, bool, error) {
	switch {
	case strings.HasPrefix(expr, "inputs."):
		if !allowInputs {
			return nil, false, fmt.Errorf("%w: inputs references are not allowed here", invocation.ErrInvalidInvocation)
		}
		return workflowMapPathValue(inputs, strings.TrimPrefix(expr, "inputs."))
	case strings.HasPrefix(expr, "runInput."):
		return workflowMapPathValue(req.Input, strings.TrimPrefix(expr, "runInput."))
	case strings.HasPrefix(expr, "signalPayload."):
		signal := workflowOutputDeliverySignal(req.Signals)
		if signal == nil {
			return nil, false, nil
		}
		return workflowMapPathValue(signal.Payload, strings.TrimPrefix(expr, "signalPayload."))
	default:
		return nil, false, fmt.Errorf("%w: unsupported template expression %q", invocation.ErrInvalidInvocation, expr)
	}
}

func workflowTemplateRenderValue(value any) (string, error) {
	if text, ok := value.(string); ok {
		return text, nil
	}
	body, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func workflowStableJSON(value any) string {
	if value == nil {
		return ""
	}
	body, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%#v", value)
	}
	return string(body)
}

func workflowStepInvocationScope(req coreworkflow.InvokeOperationRequest) string {
	if signal := workflowOutputDeliverySignal(req.Signals); signal != nil {
		if signalID := strings.TrimSpace(signal.ID); signalID != "" {
			return "signal-id:" + signalID
		}
		if key := strings.TrimSpace(signal.IdempotencyKey); key != "" {
			return "signal-idempotency:" + key
		}
		if signal.Sequence != 0 {
			return fmt.Sprintf("signal-sequence:%d", signal.Sequence)
		}
		if signal.CreatedAt != nil && !signal.CreatedAt.IsZero() {
			return "signal-created-at:" + signal.CreatedAt.UTC().Format(time.RFC3339Nano)
		}
		return "signal-non-idempotent:" + uuid.NewString()
	}
	if req.Trigger.Event != nil {
		event := req.Trigger.Event.Event
		if eventID := strings.TrimSpace(event.ID); eventID != "" {
			return "event-id:" + eventID
		}
		if event.Time != nil && !event.Time.IsZero() {
			return "event-time:" + event.Time.UTC().Format(time.RFC3339Nano)
		}
		if triggerID := strings.TrimSpace(req.Trigger.Event.TriggerID); triggerID != "" {
			return "event-trigger:" + triggerID + ":" + uuid.NewString()
		}
		return "event-non-idempotent:" + uuid.NewString()
	}
	return ""
}

func workflowStepIdempotencyKey(req coreworkflow.InvokeOperationRequest, invocationScope, stepID, suffix string) string {
	parts := []string{
		"workflow",
		strings.TrimSpace(req.ProviderName),
		strings.TrimSpace(req.RunID),
	}
	if scope := strings.TrimSpace(invocationScope); scope != "" {
		parts = append(parts, "invocation", scope)
	}
	parts = append(parts, "step", strings.TrimSpace(stepID), strings.TrimSpace(suffix))
	return strings.Join(parts, ":")
}

func (r *workflowRuntime) resolveWorkflowExecutionRef(ctx context.Context, req coreworkflow.InvokeOperationRequest) (*coreworkflow.ExecutionReference, error) {
	refID := strings.TrimSpace(req.ExecutionRef)
	providerName := strings.TrimSpace(req.ProviderName)
	if providerName == "" {
		return nil, fmt.Errorf("%w: workflow execution provider is required", invocation.ErrInternal)
	}
	provider, err := r.ResolveProvider(providerName)
	if err != nil {
		return nil, fmt.Errorf("%w: workflow provider %q is not available: %v", invocation.ErrInternal, providerName, err)
	}
	store, ok := provider.(coreworkflow.ExecutionReferenceStore)
	if !ok {
		return nil, fmt.Errorf("%w: workflow provider %q does not support execution refs", invocation.ErrInternal, providerName)
	}
	ref, err := store.GetExecutionReference(ctx, refID)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) || status.Code(err) == codes.NotFound {
			return nil, fmt.Errorf("%w: workflow execution ref %q was not found", invocation.ErrAuthorizationDenied, refID)
		}
		return nil, fmt.Errorf("%w: workflow execution ref %q lookup failed: %v", invocation.ErrInternal, refID, err)
	}
	if ref == nil {
		return nil, fmt.Errorf("%w: workflow execution ref %q was not found", invocation.ErrAuthorizationDenied, refID)
	}
	if ref.RevokedAt != nil && !ref.RevokedAt.IsZero() {
		return nil, fmt.Errorf("%w: workflow execution ref %q is revoked", invocation.ErrAuthorizationDenied, refID)
	}
	if strings.TrimSpace(ref.ProviderName) != strings.TrimSpace(req.ProviderName) {
		return nil, fmt.Errorf("%w: workflow execution ref %q is not valid for provider %q", invocation.ErrAuthorizationDenied, refID, req.ProviderName)
	}
	if !coreworkflow.TargetsEqual(req.Target, ref.Target) {
		return nil, fmt.Errorf("%w: workflow execution ref %q target does not match the scheduled invocation", invocation.ErrAuthorizationDenied, refID)
	}
	return ref, nil
}

func workflowExecutionRefAllowsInternalConnectionAccess(ref *coreworkflow.ExecutionReference) bool {
	if ref == nil {
		return false
	}
	return strings.TrimSpace(ref.AuthSource) == "config" &&
		strings.TrimSpace(ref.SubjectKind) == "system" &&
		strings.TrimSpace(ref.SubjectID) == workflowConfigOwnerSubjectID() &&
		strings.TrimSpace(ref.CredentialSubjectID) == workflowConfigOwnerSubjectID()
}

func workflowExecutionRefWithRunAsAudit(ctx context.Context, ref *coreworkflow.ExecutionReference) context.Context {
	if ref == nil {
		return ctx
	}
	runAs := core.NormalizeRunAsSubject(ref.RunAs)
	if runAs == nil {
		return ctx
	}
	owner := core.NormalizeRunAsSubject(&core.RunAsSubject{
		SubjectID:           ref.SubjectID,
		SubjectKind:         ref.SubjectKind,
		CredentialSubjectID: ref.CredentialSubjectID,
		DisplayName:         ref.DisplayName,
		AuthSource:          ref.AuthSource,
	})
	return invocation.WithRunAsAudit(ctx, owner, runAs)
}

func workflowAgentTurnID(turn *coreagent.Turn) string {
	if turn == nil {
		return ""
	}
	return strings.TrimSpace(turn.ID)
}

func workflowAgentScalarEqual(left, right any) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	if leftString, ok := left.(string); ok {
		rightString, ok := right.(string)
		return ok && leftString == rightString
	}
	if leftBool, ok := left.(bool); ok {
		rightBool, ok := right.(bool)
		return ok && leftBool == rightBool
	}
	leftNumber, leftOK := workflowAgentNumber(left)
	rightNumber, rightOK := workflowAgentNumber(right)
	return leftOK && rightOK && leftNumber == rightNumber
}

func workflowAgentNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case float32:
		return float64(v), true
	case float64:
		return v, true
	case json.Number:
		parsed, err := v.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func workflowOutputDeliverySignal(signals []coreworkflow.Signal) *coreworkflow.Signal {
	if len(signals) == 0 {
		return nil
	}
	return &signals[len(signals)-1]
}

func workflowMapPathValue(values map[string]any, path string) (any, bool, error) {
	if len(values) == 0 {
		return nil, false, nil
	}
	return workflowPathValue(values, path)
}

func workflowPathValue(root any, path string) (any, bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return root, true, nil
	}
	segments, err := workflowPathSegments(path)
	if err != nil {
		return nil, false, err
	}
	current := root
	for _, segment := range segments {
		switch typed := current.(type) {
		case map[string]any:
			next, ok := typed[segment.key]
			if !ok {
				return nil, false, nil
			}
			current = next
		case []any:
			if !segment.indexSet || segment.index < 0 || segment.index >= len(typed) {
				return nil, false, nil
			}
			current = typed[segment.index]
		default:
			return nil, false, nil
		}
	}
	return current, true, nil
}

type workflowPathSegment struct {
	key      string
	index    int
	indexSet bool
}

func workflowPathSegments(path string) ([]workflowPathSegment, error) {
	var out []workflowPathSegment
	for i := 0; i < len(path); {
		switch path[i] {
		case '.':
			i++
			continue
		case '[':
			end := strings.IndexByte(path[i:], ']')
			if end < 0 {
				return nil, fmt.Errorf("%w: invalid workflow path %q", invocation.ErrInvalidInvocation, path)
			}
			token := strings.TrimSpace(path[i+1 : i+end])
			if strings.HasPrefix(token, "'") || strings.HasPrefix(token, "\"") {
				unquoted, err := strconv.Unquote(token)
				if err != nil {
					return nil, fmt.Errorf("%w: invalid workflow path %q", invocation.ErrInvalidInvocation, path)
				}
				out = append(out, workflowPathSegment{key: unquoted})
			} else {
				index, err := strconv.Atoi(token)
				if err != nil {
					return nil, fmt.Errorf("%w: invalid workflow path %q", invocation.ErrInvalidInvocation, path)
				}
				out = append(out, workflowPathSegment{index: index, indexSet: true})
			}
			i += end + 1
		default:
			start := i
			for i < len(path) && path[i] != '.' && path[i] != '[' {
				i++
			}
			key := strings.TrimSpace(path[start:i])
			if key == "" {
				return nil, fmt.Errorf("%w: invalid workflow path %q", invocation.ErrInvalidInvocation, path)
			}
			out = append(out, workflowPathSegment{key: key})
		}
	}
	return out, nil
}

func waitForWorkflowAgentTurn(ctx context.Context, agentManager agentmanager.Service, p *principal.Principal, turn *coreagent.Turn) (*coreagent.Turn, error) {
	if turn == nil || strings.TrimSpace(turn.ID) == "" {
		return nil, fmt.Errorf("workflow agent turn is missing")
	}
	current := turn
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		switch current.Status {
		case coreagent.ExecutionStatusSucceeded, coreagent.ExecutionStatusFailed, coreagent.ExecutionStatusCanceled, coreagent.ExecutionStatusWaitingForInput:
			return current, nil
		}
		select {
		case <-ctx.Done():
			return current, ctx.Err()
		case <-ticker.C:
			next, err := agentManager.GetTurn(ctx, p, current.ID)
			if err != nil {
				return current, err
			}
			if next == nil || strings.TrimSpace(next.ID) == "" {
				return current, fmt.Errorf("workflow agent turn is missing")
			}
			current = next
		}
	}
}

func workflowInvocationContext(req coreworkflow.InvokeOperationRequest) map[string]any {
	ctxValue := map[string]any{}
	if runID := strings.TrimSpace(req.RunID); runID != "" {
		ctxValue["runId"] = runID
	}
	if providerName := strings.TrimSpace(req.ProviderName); providerName != "" {
		ctxValue["provider"] = providerName
	}
	target := workflowTargetContext(req.Target)
	if len(target) > 0 {
		ctxValue["target"] = target
	}
	trigger := workflowTriggerContext(req.Trigger)
	if len(trigger) > 0 {
		ctxValue["trigger"] = trigger
	}
	if req.Input != nil {
		ctxValue["input"] = maps.Clone(req.Input)
	}
	if req.Metadata != nil {
		ctxValue["metadata"] = maps.Clone(req.Metadata)
	}
	if len(req.Signals) > 0 {
		ctxValue["signals"] = workflowSignalsContext(req.Signals)
	}
	if createdBy := workflowActorContext(req.CreatedBy); len(createdBy) > 0 {
		ctxValue["createdBy"] = createdBy
	}
	if executionRef := strings.TrimSpace(req.ExecutionRef); executionRef != "" {
		ctxValue["executionRef"] = executionRef
	}
	return ctxValue
}

func workflowSignalsContext(signals []coreworkflow.Signal) []map[string]any {
	if len(signals) == 0 {
		return nil
	}
	limit := len(signals)
	if limit > workflowSignalContextMaxSignals {
		limit = workflowSignalContextMaxSignals
	}
	out := make([]map[string]any, 0, limit)
	for i := 0; i < limit; i++ {
		signal := &signals[i]
		value := map[string]any{}
		if id := strings.TrimSpace(signal.ID); id != "" {
			value["id"] = id
		}
		if name := strings.TrimSpace(signal.Name); name != "" {
			value["name"] = name
		}
		if signal.Payload != nil {
			if payload := compactWorkflowSignalPayload(signal.Payload); len(payload) > 0 {
				value["payload"] = payload
			}
		}
		if signal.Metadata != nil {
			if metadata, ok := compactWorkflowJSONValue(signal.Metadata, workflowSignalContextMaxDepth).(map[string]any); ok && len(metadata) > 0 {
				value["metadata"] = metadata
			}
		}
		if createdBy := workflowActorContext(signal.CreatedBy); len(createdBy) > 0 {
			value["createdBy"] = createdBy
		}
		if signal.CreatedAt != nil {
			value["createdAt"] = signal.CreatedAt.UTC().Format(time.RFC3339Nano)
		}
		if key := strings.TrimSpace(signal.IdempotencyKey); key != "" {
			value["idempotencyKey"] = key
		}
		if signal.Sequence != 0 {
			value["sequence"] = signal.Sequence
		}
		out = append(out, value)
	}
	return out
}

func compactWorkflowSignalPayload(payload map[string]any) map[string]any {
	if len(payload) == 0 {
		return nil
	}
	out := map[string]any{}
	copyCompactPayloadField(out, payload, "delivery_id")
	copyCompactPayloadField(out, payload, "deliveryId")
	copyCompactPayloadField(out, payload, "github_event")
	copyCompactPayloadField(out, payload, "githubEvent")
	copyCompactPayloadField(out, payload, "github_action")
	copyCompactPayloadField(out, payload, "githubAction")
	copyCompactPayloadField(out, payload, "event")
	copyCompactPayloadField(out, payload, "action")
	copyCompactPayloadField(out, payload, "summary")
	copyCompactPayloadField(out, payload, "user_prompt")
	copyCompactPayloadField(out, payload, "userPrompt")
	copyCompactPayloadField(out, payload, "payload_sha256")
	copyCompactPayloadField(out, payload, "payloadSha256")
	copyCompactPayloadField(out, payload, "payload_omitted")
	copyCompactPayloadField(out, payload, "payloadOmitted")
	for _, key := range []string{
		"agent_request",
		"agentRequest",
		"installation",
		"repository",
		"sender",
		"webhook_policy",
		"webhookPolicy",
		"pull_request",
		"pullRequest",
		"issue",
		"comment",
		"review",
		"ref",
		"check_run",
		"checkRun",
		"check_suite",
		"checkSuite",
		"workflow_run",
		"workflowRun",
		"review_check_run",
		"reviewCheckRun",
	} {
		if value, ok := payload[key]; ok {
			out[key] = compactWorkflowJSONValue(value, workflowSignalContextMaxDepth)
		}
	}
	scalars := map[string]any{}
	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if len(scalars) >= workflowSignalContextMaxItems {
			break
		}
		if _, exists := out[key]; exists || workflowSignalPayloadKeyExcluded(key) {
			continue
		}
		value := payload[key]
		if compact, ok := compactWorkflowJSONScalar(value); ok {
			scalars[key] = compact
		}
	}
	if len(scalars) > 0 {
		out["fields"] = scalars
	}
	out["payloadOmitted"] = true
	return out
}

func copyCompactPayloadField(out map[string]any, payload map[string]any, key string) {
	value, ok := payload[key]
	if !ok || workflowSignalPayloadKeyExcluded(key) {
		return
	}
	if compact, ok := compactWorkflowJSONScalar(value); ok {
		out[key] = compact
		return
	}
	out[key] = compactWorkflowJSONValue(value, workflowSignalContextMaxDepth)
}

func workflowSignalPayloadKeyExcluded(key string) bool {
	switch strings.TrimSpace(key) {
	case "", "payload", "_gestalt_payload_preview_json":
		return true
	default:
		return false
	}
}

func compactWorkflowJSONScalar(value any) (any, bool) {
	switch typed := value.(type) {
	case nil:
		return nil, true
	case string:
		return truncateWorkflowString(typed, workflowSignalContextMaxStringBytes), true
	case bool:
		return typed, true
	case int:
		return typed, true
	case int8:
		return typed, true
	case int16:
		return typed, true
	case int32:
		return typed, true
	case int64:
		return typed, true
	case uint:
		return typed, true
	case uint8:
		return typed, true
	case uint16:
		return typed, true
	case uint32:
		return typed, true
	case uint64:
		return typed, true
	case float32:
		return typed, true
	case float64:
		return typed, true
	default:
		return nil, false
	}
}

func compactWorkflowJSONValue(value any, depth int) any {
	if scalar, ok := compactWorkflowJSONScalar(value); ok {
		return scalar
	}
	if depth <= 0 {
		return map[string]any{"omitted": true}
	}
	switch typed := value.(type) {
	case map[string]any:
		out := map[string]any{}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			if workflowSignalPayloadKeyExcluded(key) {
				continue
			}
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if len(out) >= workflowSignalContextMaxItems {
				out["omittedFields"] = len(keys) - len(out)
				break
			}
			out[key] = compactWorkflowJSONValue(typed[key], depth-1)
		}
		return out
	case []any:
		limit := len(typed)
		if limit > workflowSignalContextMaxItems {
			limit = workflowSignalContextMaxItems
		}
		out := make([]any, 0, limit)
		for i := 0; i < limit; i++ {
			out = append(out, compactWorkflowJSONValue(typed[i], depth-1))
		}
		return out
	default:
		return truncateWorkflowString(fmt.Sprintf("%v", typed), workflowSignalContextMaxStringBytes)
	}
}

func truncateWorkflowString(value string, maxBytes int) string {
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value
	}
	if maxBytes <= len("...") {
		cut := maxBytes
		for cut > 0 && !utf8.RuneStart(value[cut]) {
			cut--
		}
		return value[:cut]
	}
	cut := maxBytes - len("...")
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut] + "..."
}

func workflowTargetContext(target coreworkflow.Target) map[string]any {
	value := map[string]any{}
	if len(target.Steps) == 0 {
		return value
	}
	steps := make([]map[string]any, 0, len(target.Steps))
	for i := range target.Steps {
		step := target.Steps[i]
		item := map[string]any{"id": strings.TrimSpace(step.ID)}
		switch {
		case step.Plugin != nil:
			item["kind"] = "plugin"
			item["plugin"] = strings.TrimSpace(step.Plugin.Name)
			item["operation"] = strings.TrimSpace(step.Plugin.Operation)
		case step.Agent != nil:
			item["kind"] = "agent"
			item["agentProvider"] = strings.TrimSpace(step.Agent.ProviderName)
			item["model"] = strings.TrimSpace(step.Agent.Model)
		default:
			item["kind"] = "unknown"
		}
		if step.OutputDelivery != nil && step.OutputDelivery.Plugin != nil {
			item["outputDelivery"] = map[string]any{
				"plugin":    strings.TrimSpace(step.OutputDelivery.Plugin.Name),
				"operation": strings.TrimSpace(step.OutputDelivery.Plugin.Operation),
			}
		}
		steps = append(steps, item)
	}
	value["kind"] = "steps"
	value["steps"] = steps
	return value
}

func workflowTriggerContext(trigger coreworkflow.RunTrigger) map[string]any {
	switch {
	case trigger.Schedule != nil:
		value := map[string]any{
			"kind":       "schedule",
			"scheduleId": trigger.Schedule.ScheduleID,
		}
		if trigger.Schedule.ScheduledFor != nil {
			value["scheduledFor"] = trigger.Schedule.ScheduledFor.UTC().Format(time.RFC3339Nano)
		}
		return value
	case trigger.Event != nil:
		value := map[string]any{
			"kind":      "event",
			"triggerId": trigger.Event.TriggerID,
		}
		if event := workflowEventContext(trigger.Event.Event); len(event) > 0 {
			value["event"] = event
		}
		return value
	case trigger.Manual:
		return map[string]any{"kind": "manual"}
	default:
		return nil
	}
}

func workflowEventContext(event coreworkflow.Event) map[string]any {
	value := map[string]any{}
	if event.ID != "" {
		value["id"] = event.ID
	}
	if event.Source != "" {
		value["source"] = event.Source
	}
	if event.SpecVersion != "" {
		value["specVersion"] = event.SpecVersion
	}
	if event.Type != "" {
		value["type"] = event.Type
	}
	if event.Subject != "" {
		value["subject"] = event.Subject
	}
	if event.Time != nil {
		value["time"] = event.Time.UTC().Format(time.RFC3339Nano)
	}
	if event.DataContentType != "" {
		value["dataContentType"] = event.DataContentType
	}
	if event.Data != nil {
		value["data"] = maps.Clone(event.Data)
	}
	if event.Extensions != nil {
		value["extensions"] = maps.Clone(event.Extensions)
	}
	return value
}

func workflowActorContext(actor coreworkflow.Actor) map[string]any {
	value := map[string]any{}
	if actor.SubjectID != "" {
		value["subjectId"] = actor.SubjectID
	}
	if actor.SubjectKind != "" {
		value["subjectKind"] = actor.SubjectKind
	}
	if actor.DisplayName != "" {
		value["displayName"] = actor.DisplayName
	}
	if actor.AuthSource != "" {
		value["authSource"] = actor.AuthSource
	}
	return value
}
