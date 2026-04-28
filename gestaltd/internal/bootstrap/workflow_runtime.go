package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/agentmanager"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
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
	if workflowTargetHasMixedKinds(req.Target) {
		return nil, fmt.Errorf("workflow target cannot include both agent and plugin fields")
	}
	principalValue := principal.Canonicalized(principal.FromContext(ctx))
	target := req.Target
	completion := req.Completion
	if strings.TrimSpace(req.ExecutionRef) != "" {
		resolvedRef, err := r.resolveWorkflowExecutionRef(ctx, req)
		if err != nil {
			return nil, err
		}
		principalValue = workflowExecutionReferencePrincipalForInvocation(resolvedRef, req)
		target = resolvedRef.Target
		completion = resolvedRef.Completion
	} else if principalValue == nil || strings.TrimSpace(principalValue.SubjectID) == "" {
		return nil, fmt.Errorf("%w: workflow execution principal is required when execution_ref is omitted", invocation.ErrInternal)
	}
	primaryReq := req
	primaryReq.Target = target
	primaryReq.Completion = completion
	primaryReq.Target = workflowRenderTargetTemplates(target, workflowPublicTemplateContext(primaryReq))
	resp, err := r.invokePrimary(ctx, primaryReq, principalValue, invoker, agentManager)
	return r.completeInvocation(ctx, primaryReq, principalValue, invoker, resp, err)
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
	if strings.TrimSpace(ref.TargetFingerprint) != "" {
		fingerprint, err := coreworkflow.InvocationFingerprint(req.Target, req.Completion)
		if err != nil {
			return nil, fmt.Errorf("%w: workflow execution ref %q invocation fingerprint failed: %v", invocation.ErrInternal, refID, err)
		}
		if fingerprint != strings.TrimSpace(ref.TargetFingerprint) {
			return nil, fmt.Errorf("%w: workflow execution ref %q target does not match the scheduled invocation", invocation.ErrAuthorizationDenied, refID)
		}
		return ref, nil
	}
	if ref.Target.Agent != nil || req.Target.Agent != nil {
		return nil, fmt.Errorf("%w: workflow execution ref %q target fingerprint is required for agent targets", invocation.ErrAuthorizationDenied, refID)
	}
	if !coreworkflow.CompletionEmpty(ref.Completion) || !coreworkflow.CompletionEmpty(req.Completion) {
		return nil, fmt.Errorf("%w: workflow execution ref %q target fingerprint is required for completion delivery", invocation.ErrAuthorizationDenied, refID)
	}
	if ref.Target.Plugin == nil || req.Target.Plugin == nil {
		return nil, fmt.Errorf("%w: workflow execution ref %q target does not match the scheduled invocation", invocation.ErrAuthorizationDenied, refID)
	}
	left := *ref.Target.Plugin
	right := *req.Target.Plugin
	if strings.TrimSpace(left.PluginName) != strings.TrimSpace(right.PluginName) ||
		strings.TrimSpace(left.Operation) != strings.TrimSpace(right.Operation) ||
		strings.TrimSpace(left.Connection) != strings.TrimSpace(right.Connection) ||
		strings.TrimSpace(left.Instance) != strings.TrimSpace(right.Instance) {
		return nil, fmt.Errorf("%w: workflow execution ref %q target does not match the scheduled invocation", invocation.ErrAuthorizationDenied, refID)
	}
	return ref, nil
}

func workflowExecutionReferencePrincipalForInvocation(ref *coreworkflow.ExecutionReference, req coreworkflow.InvokeOperationRequest) *principal.Principal {
	if ref == nil {
		return nil
	}
	subjectID := strings.TrimSpace(ref.SubjectID)
	credentialSubjectID := strings.TrimSpace(ref.CredentialSubjectID)
	if req.Trigger.Event != nil {
		if publishedSubjectID := strings.TrimSpace(req.CreatedBy.SubjectID); publishedSubjectID != "" {
			subjectID = publishedSubjectID
			credentialSubjectID = strings.TrimSpace(req.CreatedBy.CredentialSubjectID)
			if credentialSubjectID == "" {
				credentialSubjectID = subjectID
			}
		}
	}
	return executionReferencePrincipal(subjectID, credentialSubjectID, ref.Permissions)
}

func workflowTargetHasMixedKinds(target coreworkflow.Target) bool {
	return target.Agent != nil && target.Plugin != nil && coreworkflow.PluginTargetSet(*target.Plugin)
}

func (r *workflowRuntime) invokePrimary(ctx context.Context, req coreworkflow.InvokeOperationRequest, principalValue *principal.Principal, invoker invocation.Invoker, agentManager agentmanager.Service) (*coreworkflow.InvokeOperationResponse, error) {
	if req.Target.Agent != nil {
		return r.invokeAgent(ctx, req, principalValue, agentManager)
	}
	return r.invokePlugin(ctx, req, principalValue, invoker)
}

func (r *workflowRuntime) invokePlugin(ctx context.Context, req coreworkflow.InvokeOperationRequest, principalValue *principal.Principal, invoker invocation.Invoker) (*coreworkflow.InvokeOperationResponse, error) {
	return r.invokePluginWithParams(ctx, req, principalValue, invoker, workflowInvocationParams(req))
}

func (r *workflowRuntime) invokeAgent(ctx context.Context, req coreworkflow.InvokeOperationRequest, principalValue *principal.Principal, agentManager agentmanager.Service) (*coreworkflow.InvokeOperationResponse, error) {
	if agentManager == nil {
		return nil, fmt.Errorf("workflow runtime agent manager is not configured")
	}
	if strings.TrimSpace(req.RunID) == "" {
		return nil, fmt.Errorf("workflow agent target requires run_id")
	}
	target := req.Target
	if target.Agent == nil {
		return nil, fmt.Errorf("workflow agent target is required")
	}
	agentTarget := *target.Agent
	timeout := workflowAgentTimeout(agentTarget.TimeoutSeconds)
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	metadata := maps.Clone(agentTarget.Metadata)
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["workflow"] = workflowInvocationContext(req)
	session, err := agentManager.CreateSession(runCtx, principalValue, coreagent.ManagerCreateSessionRequest{
		ProviderName:    agentTarget.ProviderName,
		Model:           agentTarget.Model,
		Metadata:        metadata,
		ProviderOptions: maps.Clone(agentTarget.ProviderOptions),
		IdempotencyKey:  workflowAgentIdempotencyKey(req, "session"),
	})
	if err != nil {
		return nil, err
	}
	messages := append([]coreagent.Message(nil), agentTarget.Messages...)
	if prompt := strings.TrimSpace(agentTarget.Prompt); prompt != "" {
		messages = append(messages, coreagent.Message{Role: "user", Text: prompt})
	}
	turn, err := agentManager.CreateTurn(runCtx, principalValue, coreagent.ManagerCreateTurnRequest{
		SessionID:       session.ID,
		Model:           agentTarget.Model,
		Messages:        messages,
		ToolRefs:        append([]coreagent.ToolRef(nil), agentTarget.ToolRefs...),
		ToolSource:      agentTarget.ToolSource,
		ResponseSchema:  maps.Clone(agentTarget.ResponseSchema),
		Metadata:        metadata,
		ProviderOptions: maps.Clone(agentTarget.ProviderOptions),
		IdempotencyKey:  workflowAgentIdempotencyKey(req, "turn"),
	})
	if err != nil {
		return nil, err
	}
	turn, err = waitForWorkflowAgentTurn(runCtx, agentManager, principalValue, turn)
	if err != nil {
		if turn != nil && strings.TrimSpace(turn.ID) != "" {
			_, _ = agentManager.CancelTurn(context.WithoutCancel(ctx), principalValue, turn.ID, err.Error())
		}
		return nil, err
	}
	switch turn.Status {
	case coreagent.ExecutionStatusSucceeded:
		return &coreworkflow.InvokeOperationResponse{Status: 200, Body: turn.OutputText}, nil
	case coreagent.ExecutionStatusCanceled:
		return nil, fmt.Errorf("workflow agent turn %q was canceled: %s", turn.ID, strings.TrimSpace(turn.StatusMessage))
	case coreagent.ExecutionStatusWaitingForInput:
		_, _ = agentManager.CancelTurn(context.WithoutCancel(ctx), principalValue, turn.ID, "workflow agent turn cannot wait for input")
		return nil, fmt.Errorf("workflow agent turn %q is waiting for input", turn.ID)
	default:
		return nil, fmt.Errorf("workflow agent turn %q finished with status %q: %s", turn.ID, turn.Status, strings.TrimSpace(turn.StatusMessage))
	}
}

func workflowAgentTimeout(seconds int) time.Duration {
	if seconds <= 0 {
		return 5 * time.Minute
	}
	return time.Duration(seconds) * time.Second
}

func workflowAgentIdempotencyKey(req coreworkflow.InvokeOperationRequest, suffix string) string {
	return strings.Join([]string{
		"workflow",
		strings.TrimSpace(req.ProviderName),
		strings.TrimSpace(req.RunID),
		strings.TrimSpace(suffix),
	}, ":")
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

func (r *workflowRuntime) completeInvocation(ctx context.Context, req coreworkflow.InvokeOperationRequest, principalValue *principal.Principal, invoker invocation.Invoker, resp *coreworkflow.InvokeOperationResponse, primaryErr error) (*coreworkflow.InvokeOperationResponse, error) {
	failedByStatus := primaryErr == nil && resp != nil && resp.Status >= 400
	if primaryErr == nil && !failedByStatus {
		if err := r.invokeCompletionDelivery(ctx, req, principalValue, invoker, req.Completion.OnSuccess, workflowTemplateContext(req, resp, "", nil)); err != nil {
			return nil, err
		}
		return resp, nil
	}

	deliveryErr := r.invokeCompletionDelivery(
		ctx,
		req,
		principalValue,
		invoker,
		req.Completion.OnFailure,
		workflowTemplateContext(req, resp, workflowDeliveryErrorMessage(resp, primaryErr), nil),
	)
	if primaryErr != nil {
		if deliveryErr != nil {
			return nil, fmt.Errorf("%w; workflow failure delivery failed: %v", primaryErr, deliveryErr)
		}
		return nil, primaryErr
	}
	if deliveryErr != nil {
		return nil, deliveryErr
	}
	return resp, nil
}

func (r *workflowRuntime) invokeCompletionDelivery(ctx context.Context, req coreworkflow.InvokeOperationRequest, principalValue *principal.Principal, invoker invocation.Invoker, delivery *coreworkflow.CompletionDelivery, templateContext map[string]any) error {
	if coreworkflow.CompletionDeliveryEmpty(delivery) {
		return nil
	}
	if invoker == nil {
		if delivery.BestEffort {
			return nil
		}
		return fmt.Errorf("workflow runtime invoker is not configured")
	}
	pluginTarget := *delivery.Plugin
	renderedParams := workflowRenderMapTemplates(pluginTarget.Input, templateContext)
	pluginTarget.Input = nil
	deliveryReq := req
	deliveryReq.Target = coreworkflow.Target{Plugin: &pluginTarget}
	deliveryReq.Completion = coreworkflow.Completion{}
	deliveryReq.Input = nil
	resp, err := r.invokePluginWithParams(ctx, deliveryReq, principalValue, invoker, renderedParams)
	if err == nil && resp != nil && resp.Status >= 400 {
		err = fmt.Errorf("workflow completion delivery returned status %d", resp.Status)
	}
	if err != nil && delivery.BestEffort {
		return nil
	}
	return err
}

func workflowDeliveryErrorMessage(resp *coreworkflow.InvokeOperationResponse, err error) string {
	if err != nil {
		return "workflow operation failed"
	}
	if resp != nil && resp.Status >= 400 {
		return fmt.Sprintf("workflow operation returned status %d", resp.Status)
	}
	return ""
}

func workflowInvocationParams(req coreworkflow.InvokeOperationRequest) map[string]any {
	var params map[string]any
	if req.Target.Plugin != nil {
		params = maps.Clone(req.Target.Plugin.Input)
	}
	if req.Input != nil {
		if params == nil {
			params = map[string]any{}
		}
		maps.Copy(params, req.Input)
	}
	return params
}

func (r *workflowRuntime) invokePluginWithParams(ctx context.Context, req coreworkflow.InvokeOperationRequest, principalValue *principal.Principal, invoker invocation.Invoker, params map[string]any) (*coreworkflow.InvokeOperationResponse, error) {
	if invoker == nil {
		return nil, fmt.Errorf("workflow runtime invoker is not configured")
	}
	if req.Target.Plugin == nil || strings.TrimSpace(req.Target.Plugin.PluginName) == "" {
		return nil, fmt.Errorf("workflow target plugin is required")
	}
	if contextValue := workflowInvocationContext(req); len(contextValue) > 0 {
		ctx = invocation.WithWorkflowContext(ctx, contextValue)
	}
	pluginTarget := req.Target.Plugin
	invokeInstance := ""
	if strings.TrimSpace(req.ExecutionRef) != "" {
		invokeInstance = strings.TrimSpace(pluginTarget.Instance)
		if connection := strings.TrimSpace(pluginTarget.Connection); connection != "" {
			ctx = invocation.WithConnection(ctx, connection)
		}
	}
	result, err := invoker.Invoke(ctx, principalValue, pluginTarget.PluginName, invokeInstance, pluginTarget.Operation, params)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return &coreworkflow.InvokeOperationResponse{}, nil
	}
	return &coreworkflow.InvokeOperationResponse{
		Status: result.Status,
		Body:   result.Body,
	}, nil
}

func workflowTemplateContext(req coreworkflow.InvokeOperationRequest, resp *coreworkflow.InvokeOperationResponse, errorMessage string, extra map[string]any) map[string]any {
	ctxValue := workflowPublicTemplateContext(req)
	if ctxValue == nil {
		ctxValue = map[string]any{}
	}
	if resp != nil {
		ctxValue["result"] = map[string]any{
			"status": resp.Status,
			"body":   resp.Body,
		}
	}
	if errorMessage != "" {
		ctxValue["error"] = map[string]any{
			"message": errorMessage,
		}
	}
	if req.PrivateInput != nil {
		ctxValue["private"] = maps.Clone(req.PrivateInput)
	}
	if extra != nil {
		maps.Copy(ctxValue, extra)
	}
	return ctxValue
}

func workflowPublicTemplateContext(req coreworkflow.InvokeOperationRequest) map[string]any {
	return workflowInvocationContext(req)
}

func workflowRenderTargetTemplates(target coreworkflow.Target, context map[string]any) coreworkflow.Target {
	if target.Agent != nil {
		agentTarget := *target.Agent
		agentTarget.Prompt = workflowRenderStringTemplate(agentTarget.Prompt, context)
		agentTarget.Messages = workflowRenderAgentMessages(agentTarget.Messages, context)
		return coreworkflow.Target{Agent: &agentTarget}
	}
	if target.Plugin != nil {
		pluginTarget := *target.Plugin
		pluginTarget.Input = workflowRenderMapTemplates(pluginTarget.Input, context)
		return coreworkflow.Target{Plugin: &pluginTarget}
	}
	return target
}

func workflowRenderAgentMessages(messages []coreagent.Message, context map[string]any) []coreagent.Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]coreagent.Message, 0, len(messages))
	for _, message := range messages {
		cloned := message
		cloned.Text = workflowRenderStringTemplate(message.Text, context)
		if len(message.Parts) > 0 {
			cloned.Parts = make([]coreagent.MessagePart, 0, len(message.Parts))
			for _, part := range message.Parts {
				clonedPart := part
				clonedPart.Text = workflowRenderStringTemplate(part.Text, context)
				clonedPart.JSON = workflowRenderMapTemplates(part.JSON, context)
				cloned.Parts = append(cloned.Parts, clonedPart)
			}
		}
		out = append(out, cloned)
	}
	return out
}

func workflowRenderMapTemplates(values map[string]any, context map[string]any) map[string]any {
	rendered := workflowRenderTemplateValue(values, context)
	if out, ok := rendered.(map[string]any); ok {
		return out
	}
	return nil
}

func workflowRenderTemplateValue(value any, context map[string]any) any {
	switch typed := value.(type) {
	case string:
		return workflowRenderStringTemplateValue(typed, context)
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = workflowRenderTemplateValue(item, context)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, workflowRenderTemplateValue(item, context))
		}
		return out
	default:
		return value
	}
}

func workflowRenderStringTemplate(text string, context map[string]any) string {
	value := workflowRenderStringTemplateValue(text, context)
	if rendered, ok := value.(string); ok {
		return rendered
	}
	return fmt.Sprint(value)
}

func workflowRenderStringTemplateValue(text string, context map[string]any) any {
	start := strings.Index(text, "{{")
	if start < 0 {
		return text
	}
	end := strings.Index(text[start+2:], "}}")
	if end < 0 {
		return text
	}
	end += start + 2
	before := text[:start]
	after := text[end+2:]
	expr := strings.TrimSpace(text[start+2 : end])
	value, ok := workflowTemplateLookup(context, expr)
	if !ok {
		value = ""
	}
	if before == "" && after == "" {
		return value
	}
	var rendered strings.Builder
	rendered.Grow(len(text))
	cursor := 0
	for {
		start = strings.Index(text[cursor:], "{{")
		if start < 0 {
			rendered.WriteString(text[cursor:])
			return rendered.String()
		}
		start += cursor
		end = strings.Index(text[start+2:], "}}")
		if end < 0 {
			rendered.WriteString(text[cursor:])
			return rendered.String()
		}
		end += start + 2
		rendered.WriteString(text[cursor:start])
		expr = strings.TrimSpace(text[start+2 : end])
		value, ok = workflowTemplateLookup(context, expr)
		if !ok {
			value = ""
		}
		rendered.WriteString(workflowTemplateString(value))
		cursor = end + 2
	}
}

func workflowTemplateLookup(context map[string]any, expr string) (any, bool) {
	if expr == "" {
		return nil, false
	}
	var current any = context
	for _, part := range strings.Split(expr, ".") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, false
		}
		values, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = values[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func workflowTemplateString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
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
	if createdBy := workflowActorContext(req.CreatedBy); len(createdBy) > 0 {
		ctxValue["createdBy"] = createdBy
	}
	if executionRef := strings.TrimSpace(req.ExecutionRef); executionRef != "" {
		ctxValue["executionRef"] = executionRef
	}
	return ctxValue
}

func workflowTargetContext(target coreworkflow.Target) map[string]any {
	value := map[string]any{}
	if target.Agent != nil {
		agentTarget := *target.Agent
		value["kind"] = "agent"
		if providerName := strings.TrimSpace(agentTarget.ProviderName); providerName != "" {
			value["agentProvider"] = providerName
		}
		if model := strings.TrimSpace(agentTarget.Model); model != "" {
			value["model"] = model
		}
		if len(agentTarget.ToolRefs) > 0 {
			tools := make([]map[string]any, 0, len(agentTarget.ToolRefs))
			for _, ref := range agentTarget.ToolRefs {
				tool := map[string]any{}
				if pluginName := strings.TrimSpace(ref.Plugin); pluginName != "" {
					tool["plugin"] = pluginName
				}
				if operation := strings.TrimSpace(ref.Operation); operation != "" {
					tool["operation"] = operation
				}
				if len(tool) > 0 {
					tools = append(tools, tool)
				}
			}
			value["tools"] = tools
		}
		return value
	}
	if target.Plugin == nil {
		return value
	}
	pluginTarget := *target.Plugin
	plugin := map[string]any{}
	if pluginName := strings.TrimSpace(pluginTarget.PluginName); pluginName != "" {
		value["kind"] = "plugin"
		plugin["pluginName"] = pluginName
	}
	if operation := strings.TrimSpace(pluginTarget.Operation); operation != "" {
		plugin["operation"] = operation
	}
	if connection := strings.TrimSpace(pluginTarget.Connection); connection != "" {
		plugin["connection"] = connection
	}
	if instance := strings.TrimSpace(pluginTarget.Instance); instance != "" {
		plugin["instance"] = instance
	}
	if pluginTarget.Input != nil {
		plugin["input"] = maps.Clone(pluginTarget.Input)
	}
	if len(plugin) > 0 {
		value["plugin"] = plugin
	}
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
	if actor.CredentialSubjectID != "" {
		value["credentialSubjectId"] = actor.CredentialSubjectID
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
