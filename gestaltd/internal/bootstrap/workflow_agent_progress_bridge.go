package bootstrap

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/agentmanager"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

const (
	workflowAgentProgressMetadataKey       = "gestalt.workflow.agent_progress"
	workflowAgentProgressUpdateMinInterval = 2 * time.Second
	workflowAgentProgressUpdateTimeout     = 2 * time.Second
	workflowAgentProgressClearTimeout      = 5 * time.Second
)

type workflowAgentProgressBridge struct {
	invoker           invocation.Invoker
	principal         *principal.Principal
	updateTarget      coreagent.ToolTarget
	clearTarget       coreagent.ToolTarget
	params            map[string]any
	updateParams      map[string]any
	clearParams       map[string]any
	statusParam       string
	rules             []workflowAgentProgressRule
	defaultToolStatus string
	lastStatus        string
	lastUpdatedAt     time.Time
	minInterval       time.Duration
	cleared           bool
}

type workflowAgentProgressConfig struct {
	updateRef         coreagent.ToolRef
	clearRef          coreagent.ToolRef
	params            map[string]any
	updateParams      map[string]any
	clearParams       map[string]any
	statusParam       string
	rules             []workflowAgentProgressRule
	defaultToolStatus string
	minInterval       time.Duration
}

type workflowAgentProgressRule struct {
	eventType        string
	identifier       string
	identifierPrefix string
	plugin           string
	pluginPrefix     string
	operation        string
	operationPrefix  string
	status           string
	ignore           bool
}

func newWorkflowAgentProgressBridge(ctx context.Context, target coreworkflow.AgentTarget, principalValue *principal.Principal, agentManager agentmanager.Service, invoker invocation.Invoker, callerPluginName string) (*workflowAgentProgressBridge, error) {
	if agentManager == nil || invoker == nil || principalValue == nil {
		return nil, nil
	}
	cfg, ok, err := workflowAgentProgressConfigFromMetadata(target.Metadata)
	if err != nil || !ok {
		return nil, err
	}
	cfg.updateRef = workflowAgentProgressRefWithSelectors(cfg.updateRef, target.ToolRefs)
	cfg.clearRef = workflowAgentProgressRefWithSelectors(cfg.clearRef, target.ToolRefs)
	tools, err := agentManager.ResolveTools(ctx, principalValue, coreagent.ResolveToolsRequest{
		ToolRefs:         []coreagent.ToolRef{cfg.updateRef, cfg.clearRef},
		ToolSource:       target.ToolSource,
		CallerPluginName: strings.TrimSpace(callerPluginName),
	})
	if err != nil {
		return nil, err
	}
	updateTarget, ok := workflowAgentProgressTargetForRef(tools, cfg.updateRef)
	if !ok {
		return nil, fmt.Errorf("resolved workflow agent progress update tool is missing")
	}
	clearTarget, ok := workflowAgentProgressTargetForRef(tools, cfg.clearRef)
	if !ok {
		return nil, fmt.Errorf("resolved workflow agent progress clear tool is missing")
	}
	return &workflowAgentProgressBridge{
		invoker:           invoker,
		principal:         principalValue,
		updateTarget:      updateTarget,
		clearTarget:       clearTarget,
		params:            cfg.params,
		updateParams:      cfg.updateParams,
		clearParams:       cfg.clearParams,
		statusParam:       cfg.statusParam,
		rules:             cfg.rules,
		defaultToolStatus: cfg.defaultToolStatus,
		minInterval:       cfg.minInterval,
	}, nil
}

func workflowAgentProgressConfigFromMetadata(metadata map[string]any) (workflowAgentProgressConfig, bool, error) {
	raw, ok := metadata[workflowAgentProgressMetadataKey]
	if !ok || raw == nil {
		return workflowAgentProgressConfig{}, false, nil
	}
	configMap, ok := raw.(map[string]any)
	if !ok {
		return workflowAgentProgressConfig{}, false, fmt.Errorf("workflow agent progress metadata must be an object")
	}
	updateRef, updateParams, ok := workflowAgentProgressToolRef(configMap, "update")
	if !ok {
		return workflowAgentProgressConfig{}, false, fmt.Errorf("workflow agent progress metadata update tool is required")
	}
	clearRef, clearParams, ok := workflowAgentProgressToolRef(configMap, "clear")
	if !ok {
		return workflowAgentProgressConfig{}, false, fmt.Errorf("workflow agent progress metadata clear tool is required")
	}
	statusParam := stringMapValue(configMap, "statusParam", "status_param")
	if statusParam == "" {
		statusParam = "status"
	}
	minInterval := workflowAgentProgressUpdateMinInterval
	if parsed, ok := workflowAgentProgressDuration(configMap["minInterval"]); ok {
		minInterval = parsed
	} else if parsed, ok := workflowAgentProgressDuration(configMap["min_interval"]); ok {
		minInterval = parsed
	}
	return workflowAgentProgressConfig{
		updateRef:         updateRef,
		clearRef:          clearRef,
		params:            mapValue(configMap, "params"),
		updateParams:      updateParams,
		clearParams:       clearParams,
		statusParam:       statusParam,
		rules:             workflowAgentProgressRules(configMap["rules"]),
		defaultToolStatus: stringMapValue(configMap, "defaultToolStatus", "default_tool_status"),
		minInterval:       minInterval,
	}, true, nil
}

func workflowAgentProgressToolRef(configMap map[string]any, key string) (coreagent.ToolRef, map[string]any, bool) {
	raw := configMap[key]
	if raw == nil {
		return coreagent.ToolRef{}, nil, false
	}
	toolMap, ok := raw.(map[string]any)
	if !ok {
		return coreagent.ToolRef{}, nil, false
	}
	ref := coreagent.ToolRef{
		Plugin:         stringMapValue(toolMap, "plugin"),
		Operation:      stringMapValue(toolMap, "operation"),
		Connection:     stringMapValue(toolMap, "connection"),
		Instance:       stringMapValue(toolMap, "instance"),
		CredentialMode: core.ConnectionMode(stringMapValue(toolMap, "credentialMode", "credential_mode")),
	}
	return ref, mapValue(toolMap, "params"), strings.TrimSpace(ref.Plugin) != "" && strings.TrimSpace(ref.Operation) != ""
}

func workflowAgentProgressRefWithSelectors(ref coreagent.ToolRef, refs []coreagent.ToolRef) coreagent.ToolRef {
	var match *coreagent.ToolRef
	for i := range refs {
		candidate := &refs[i]
		if !workflowAgentProgressToolRefMatches(ref, *candidate) {
			continue
		}
		if match != nil {
			return ref
		}
		match = candidate
	}
	if match == nil {
		return ref
	}
	if strings.TrimSpace(ref.Connection) == "" {
		ref.Connection = strings.TrimSpace(match.Connection)
	}
	if strings.TrimSpace(ref.Instance) == "" {
		ref.Instance = strings.TrimSpace(match.Instance)
	}
	if ref.CredentialMode == "" {
		ref.CredentialMode = match.CredentialMode
	}
	return ref
}

func workflowAgentProgressToolRefMatches(ref coreagent.ToolRef, candidate coreagent.ToolRef) bool {
	if strings.TrimSpace(candidate.Plugin) != strings.TrimSpace(ref.Plugin) || strings.TrimSpace(candidate.Operation) != strings.TrimSpace(ref.Operation) {
		return false
	}
	return workflowAgentProgressSelectorMatches(ref.Connection, candidate.Connection, workflowAgentProgressNormalizeConnection) &&
		workflowAgentProgressSelectorMatches(ref.Instance, candidate.Instance, workflowAgentProgressNormalizeSelector) &&
		(ref.CredentialMode == "" || candidate.CredentialMode == ref.CredentialMode)
}

func workflowAgentProgressDuration(raw any) (time.Duration, bool) {
	switch value := raw.(type) {
	case string:
		parsed, err := time.ParseDuration(strings.TrimSpace(value))
		return parsed, err == nil
	case int:
		return time.Duration(value) * time.Second, true
	case int64:
		return time.Duration(value) * time.Second, true
	case float64:
		return time.Duration(value * float64(time.Second)), true
	default:
		return 0, false
	}
}

func workflowAgentProgressRules(raw any) []workflowAgentProgressRule {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	rules := make([]workflowAgentProgressRule, 0, len(items))
	for _, item := range items {
		ruleMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		status := stringMapValue(ruleMap, "status")
		ignore := boolMapValue(ruleMap, "ignore")
		if status == "" && !ignore {
			continue
		}
		rules = append(rules, workflowAgentProgressRule{
			eventType:        stringMapValue(ruleMap, "event", "type"),
			identifier:       stringMapValue(ruleMap, "identifier", "tool"),
			identifierPrefix: stringMapValue(ruleMap, "identifierPrefix", "identifier_prefix", "toolPrefix", "tool_prefix"),
			plugin:           stringMapValue(ruleMap, "plugin"),
			pluginPrefix:     stringMapValue(ruleMap, "pluginPrefix", "plugin_prefix"),
			operation:        stringMapValue(ruleMap, "operation"),
			operationPrefix:  stringMapValue(ruleMap, "operationPrefix", "operation_prefix"),
			status:           status,
			ignore:           ignore,
		})
	}
	return rules
}

func workflowAgentProgressTargetForRef(tools []coreagent.Tool, ref coreagent.ToolRef) (coreagent.ToolTarget, bool) {
	for i := range tools {
		target := tools[i].Target
		if workflowAgentProgressTargetMatchesRef(target, ref) {
			return target, true
		}
	}
	return coreagent.ToolTarget{}, false
}

func workflowAgentProgressTargetMatchesRef(target coreagent.ToolTarget, ref coreagent.ToolRef) bool {
	if strings.TrimSpace(target.Plugin) != strings.TrimSpace(ref.Plugin) || strings.TrimSpace(target.Operation) != strings.TrimSpace(ref.Operation) {
		return false
	}
	return workflowAgentProgressSelectorMatches(ref.Connection, target.Connection, workflowAgentProgressNormalizeConnection) &&
		workflowAgentProgressSelectorMatches(ref.Instance, target.Instance, workflowAgentProgressNormalizeSelector) &&
		(ref.CredentialMode == "" || target.CredentialMode == ref.CredentialMode)
}

func (b *workflowAgentProgressBridge) ObserveTurnEvents(ctx context.Context, events []*coreagent.TurnEvent) {
	if b == nil || len(events) == 0 {
		return
	}
	status := ""
	for _, event := range events {
		if next := b.statusForTurnEvent(event); next != "" {
			status = next
		}
	}
	if status == "" {
		return
	}
	b.update(ctx, status)
}

func (b *workflowAgentProgressBridge) CompleteTurn(ctx context.Context, _ *coreagent.Turn) {
	b.clear(ctx)
}

func (b *workflowAgentProgressBridge) ClearWithDetachedTimeout(ctx context.Context) {
	if b == nil {
		return
	}
	clearCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), workflowAgentProgressClearTimeout)
	defer cancel()
	b.clear(clearCtx)
}

func (b *workflowAgentProgressBridge) statusForTurnEvent(event *coreagent.TurnEvent) string {
	if b == nil || event == nil {
		return ""
	}
	for i := range b.rules {
		if b.rules[i].matches(event) {
			if b.rules[i].ignore {
				return ""
			}
			return b.rules[i].status
		}
	}
	if strings.TrimSpace(event.Type) == "tool.started" {
		return strings.TrimSpace(b.defaultToolStatus)
	}
	return ""
}

func (r workflowAgentProgressRule) matches(event *coreagent.TurnEvent) bool {
	if event == nil {
		return false
	}
	if r.eventType != "" && strings.TrimSpace(event.Type) != r.eventType {
		return false
	}
	if r.identifier == "" && r.identifierPrefix == "" && r.plugin == "" && r.pluginPrefix == "" && r.operation == "" && r.operationPrefix == "" {
		return true
	}
	for _, identifier := range workflowAgentProgressToolIdentifiers(event) {
		if workflowAgentProgressRuleMatchesIdentifier(r, identifier) {
			return true
		}
	}
	return false
}

func workflowAgentProgressRuleMatchesIdentifier(rule workflowAgentProgressRule, identifier string) bool {
	plugin, operation, normalized := normalizedToolIdentifier(identifier)
	if normalized == "" {
		return false
	}
	if rule.identifier != "" {
		_, _, wanted := normalizedToolIdentifier(rule.identifier)
		if normalized != wanted {
			return false
		}
	}
	if rule.identifierPrefix != "" && !strings.HasPrefix(normalized, normalizedToolRuleValue(rule.identifierPrefix)) {
		return false
	}
	if rule.plugin != "" && plugin != normalizedToolRuleValue(rule.plugin) {
		return false
	}
	if rule.pluginPrefix != "" && !strings.HasPrefix(plugin, normalizedToolRuleValue(rule.pluginPrefix)) {
		return false
	}
	if rule.operation != "" {
		_, _, wanted := normalizedToolIdentifier(rule.operation)
		if operation != wanted && normalized != wanted {
			return false
		}
	}
	if rule.operationPrefix != "" && !workflowAgentProgressOperationHasPrefix(operation, normalized, rule.operationPrefix) {
		return false
	}
	return true
}

func workflowAgentProgressToolIdentifiers(event *coreagent.TurnEvent) []string {
	if event == nil {
		return nil
	}
	identifiers := make([]string, 0, 4)
	if event.Display != nil {
		identifiers = append(identifiers, event.Display.Label)
	}
	if event.Data != nil {
		identifiers = append(identifiers,
			stringMapValue(event.Data, "tool_id", "toolId"),
			stringMapValue(event.Data, "operation"),
			stringMapValue(event.Data, "tool_name", "toolName", "name"),
		)
	}
	return identifiers
}

func (b *workflowAgentProgressBridge) update(ctx context.Context, status string) {
	status = strings.TrimSpace(status)
	if b == nil || status == "" || status == b.lastStatus {
		return
	}
	now := time.Now()
	minInterval := b.minInterval
	if minInterval <= 0 {
		minInterval = workflowAgentProgressUpdateMinInterval
	}
	if !b.lastUpdatedAt.IsZero() && now.Sub(b.lastUpdatedAt) < minInterval {
		return
	}
	b.lastStatus = status
	b.lastUpdatedAt = now
	b.cleared = false
	params := maps.Clone(b.params)
	if params == nil {
		params = map[string]any{}
	}
	maps.Copy(params, b.updateParams)
	params[b.statusParam] = status
	updateCtx, cancel := context.WithTimeout(ctx, workflowAgentProgressUpdateTimeout)
	defer cancel()
	_, _ = invokeResolvedToolTarget(updateCtx, b.invoker, b.principal, b.updateTarget, params)
}

func (b *workflowAgentProgressBridge) clear(ctx context.Context) {
	if b == nil || b.cleared {
		return
	}
	params := maps.Clone(b.params)
	if params == nil {
		params = map[string]any{}
	}
	maps.Copy(params, b.clearParams)
	if _, err := invokeResolvedToolTarget(ctx, b.invoker, b.principal, b.clearTarget, params); err == nil {
		b.cleared = true
	}
}

func invokeResolvedToolTarget(ctx context.Context, invoker invocation.Invoker, principalValue *principal.Principal, target coreagent.ToolTarget, params map[string]any) (*core.OperationResult, error) {
	if invoker == nil {
		return nil, fmt.Errorf("plugin invoker is not available")
	}
	if connection := strings.TrimSpace(target.Connection); connection != "" {
		ctx = invocation.WithConnection(ctx, connection)
	}
	if mode := target.CredentialMode; mode != "" {
		ctx = invocation.WithCredentialModeOverride(ctx, mode)
	}
	return invoker.Invoke(ctx, principalValue, target.Plugin, strings.TrimSpace(target.Instance), target.Operation, maps.Clone(params))
}

func normalizedToolIdentifier(identifier string) (plugin string, operation string, normalized string) {
	normalized = strings.ToLower(strings.TrimSpace(identifier))
	if idx := strings.Index(normalized, "?"); idx >= 0 {
		normalized = normalized[:idx]
	}
	if normalized == "" {
		return "", "", ""
	}
	if before, after, ok := strings.Cut(normalized, "/"); ok {
		return strings.TrimSpace(before), strings.TrimSpace(after), normalized
	}
	if before, after, ok := strings.Cut(normalized, "."); ok {
		return strings.TrimSpace(before), strings.TrimSpace(after), normalized
	}
	return "", "", normalized
}

func normalizedToolRuleValue(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func workflowAgentProgressOperationHasPrefix(operation string, normalized string, prefix string) bool {
	prefix = normalizedToolRuleValue(prefix)
	if prefix == "" {
		return false
	}
	return strings.HasPrefix(operation, prefix) || strings.HasPrefix(normalized, prefix)
}

func workflowAgentProgressSelectorMatches(want string, got string, normalize func(string) string) bool {
	want = normalize(want)
	if want == "" {
		return true
	}
	return normalize(got) == want
}

func workflowAgentProgressNormalizeConnection(value string) string {
	return config.ResolveConnectionAlias(strings.TrimSpace(value))
}

func workflowAgentProgressNormalizeSelector(value string) string {
	return strings.TrimSpace(value)
}

func mapValue(values map[string]any, key string) map[string]any {
	if values == nil {
		return nil
	}
	value, ok := values[key]
	if !ok || value == nil {
		return nil
	}
	if typed, ok := value.(map[string]any); ok {
		return maps.Clone(typed)
	}
	return nil
}

func stringMapValue(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key]; ok && value != nil {
			text := strings.TrimSpace(fmt.Sprint(value))
			if text != "" && text != "<nil>" {
				return text
			}
		}
	}
	return ""
}

func boolMapValue(values map[string]any, key string) bool {
	if values == nil {
		return false
	}
	value, ok := values[key]
	if !ok {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}
