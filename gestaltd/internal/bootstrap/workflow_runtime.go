package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/agentmanager"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/workflowprincipal"
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
	if req.Target.Agent != nil {
		return r.invokeAgent(ctx, req, agentManager)
	}
	if invoker == nil {
		return nil, fmt.Errorf("workflow runtime invoker is not configured")
	}
	if req.Target.Plugin == nil || strings.TrimSpace(req.Target.Plugin.PluginName) == "" {
		return nil, fmt.Errorf("workflow target plugin is required")
	}
	principalValue := principal.Canonicalized(principal.FromContext(ctx))
	target := req.Target
	invokeConnection := ""
	invokeInstance := ""
	if strings.TrimSpace(req.ExecutionRef) != "" {
		resolvedRef, err := r.resolveWorkflowExecutionRef(ctx, req)
		if err != nil {
			return nil, err
		}
		principalValue = workflowprincipal.FromExecutionReference(resolvedRef)
		target = resolvedRef.Target
		if target.Plugin != nil {
			invokeConnection = strings.TrimSpace(target.Plugin.Connection)
			invokeInstance = strings.TrimSpace(target.Plugin.Instance)
		}
	} else if principalValue == nil || strings.TrimSpace(principalValue.SubjectID) == "" {
		return nil, fmt.Errorf("%w: workflow execution principal is required when execution_ref is omitted", invocation.ErrInternal)
	}
	if contextValue := workflowInvocationContext(req); len(contextValue) > 0 {
		ctx = invocation.WithWorkflowContext(ctx, contextValue)
	}
	if invokeConnection != "" {
		ctx = invocation.WithConnection(ctx, invokeConnection)
	}
	params := workflowInvocationParams(req)
	if target.Plugin == nil {
		return nil, fmt.Errorf("workflow target plugin is required")
	}
	pluginTarget := target.Plugin
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

func workflowTargetHasMixedKinds(target coreworkflow.Target) bool {
	return target.Agent != nil && target.Plugin != nil && coreworkflow.PluginTargetSet(*target.Plugin)
}

func (r *workflowRuntime) invokeAgent(ctx context.Context, req coreworkflow.InvokeOperationRequest, agentManager agentmanager.Service) (*coreworkflow.InvokeOperationResponse, error) {
	if agentManager == nil {
		return nil, fmt.Errorf("workflow runtime agent manager is not configured")
	}
	if strings.TrimSpace(req.RunID) == "" {
		return nil, fmt.Errorf("workflow agent target requires run_id")
	}
	principalValue := principal.Canonicalized(principal.FromContext(ctx))
	target := req.Target
	callerPluginName := ""
	if strings.TrimSpace(req.ExecutionRef) != "" {
		resolvedRef, err := r.resolveWorkflowExecutionRef(ctx, req)
		if err != nil {
			return nil, err
		}
		principalValue = workflowprincipal.FromExecutionReference(resolvedRef)
		target = resolvedRef.Target
		callerPluginName = strings.TrimSpace(resolvedRef.CallerPluginName)
	} else if principalValue == nil || strings.TrimSpace(principalValue.SubjectID) == "" {
		return nil, fmt.Errorf("%w: workflow execution principal is required when execution_ref is omitted", invocation.ErrInternal)
	}
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
	if signalMessage := workflowSignalMessage(req.Signals); signalMessage != nil {
		messages = append(messages, *signalMessage)
	}
	turn, err := agentManager.CreateTurn(runCtx, principalValue, coreagent.ManagerCreateTurnRequest{
		CallerPluginName: callerPluginName,
		SessionID:        session.ID,
		Model:            agentTarget.Model,
		Messages:         messages,
		ToolRefs:         append([]coreagent.ToolRef(nil), agentTarget.ToolRefs...),
		ToolSource:       agentTarget.ToolSource,
		ResponseSchema:   maps.Clone(agentTarget.ResponseSchema),
		Metadata:         metadata,
		ProviderOptions:  maps.Clone(agentTarget.ProviderOptions),
		IdempotencyKey:   workflowAgentTurnIdempotencyKey(req),
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

func workflowAgentTurnIdempotencyKey(req coreworkflow.InvokeOperationRequest) string {
	batchID := workflowSignalBatchID(req.Signals)
	if batchID == "" {
		return workflowAgentIdempotencyKey(req, "turn")
	}
	return workflowAgentIdempotencyKey(req, "turn:"+batchID)
}

func workflowSignalBatchID(signals []coreworkflow.Signal) string {
	if len(signals) == 0 {
		return ""
	}
	parts := make([]string, 0, len(signals))
	for i := range signals {
		signal := &signals[i]
		key := strings.TrimSpace(signal.IdempotencyKey)
		if key == "" {
			key = strings.TrimSpace(signal.ID)
		}
		if key == "" && signal.Sequence != 0 {
			key = fmt.Sprintf("seq-%d", signal.Sequence)
		}
		if key == "" {
			key = strings.TrimSpace(signal.Name)
		}
		parts = append(parts, key)
	}
	body, err := json.Marshal(parts)
	if err != nil {
		return "signal-batch-" + fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%#v", parts))))
	}
	return "signal-batch-" + fmt.Sprintf("%x", sha256.Sum256(body))
}

const (
	workflowSignalContextMaxSignals     = 10
	workflowSignalContextMaxItems       = 20
	workflowSignalContextMaxDepth       = 4
	workflowSignalContextMaxStringBytes = 4096
	workflowSignalMessageMaxBytes       = 64 * 1024
	workflowSignalMessagePrefix         = "Workflow signal batch:\n"
)

func workflowSignalMessage(signals []coreworkflow.Signal) *coreagent.Message {
	if len(signals) == 0 {
		return nil
	}
	payload := map[string]any{
		"signals": workflowSignalsContext(signals),
	}
	if omitted := len(signals) - workflowSignalContextMaxSignals; omitted > 0 {
		payload["omittedSignals"] = omitted
	}
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		body = []byte(fmt.Sprintf("%#v", payload))
	}
	textMaxBytes := workflowSignalMessageMaxBytes - len(workflowSignalMessagePrefix)
	text := truncateWorkflowString(string(body), textMaxBytes)
	return &coreagent.Message{
		Role: "user",
		Text: workflowSignalMessagePrefix + text,
		Metadata: map[string]any{
			"gestalt.workflow.signal_batch": true,
		},
	}
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
	for _, key := range []string{"agent_request", "agentRequest", "installation", "repository", "sender", "pull_request", "pullRequest", "issue", "comment", "review", "ref"} {
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
			for i := range agentTarget.ToolRefs {
				ref := agentTarget.ToolRefs[i]
				tool := map[string]any{}
				if systemName := strings.TrimSpace(ref.System); systemName != "" {
					tool["system"] = systemName
				}
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
