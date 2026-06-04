package workflow

import (
	"fmt"
	"maps"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

const (
	workflowSignalContextMaxSignals     = 10
	workflowSignalContextMaxItems       = 20
	workflowSignalContextMaxDepth       = 4
	workflowSignalContextMaxStringBytes = 4096
)

func WorkflowStepInvocationScope(req Request) string {
	if signal := LatestSignal(req.Signals); signal != nil {
		if signalID := strings.TrimSpace(signal.ID); signalID != "" {
			return "signal-id:" + signalID
		}
		if key := strings.TrimSpace(signal.IdempotencyKey); key != "" {
			return "signal-idempotency:" + key
		}
		if signal.Sequence != 0 {
			return fmt.Sprintf("signal-sequence:%d", signal.Sequence)
		}
		if !signal.CreatedAt.IsZero() {
			return "signal-created-at:" + signal.CreatedAt.UTC().Format(time.RFC3339Nano)
		}
		return "signal-non-idempotent:" + uuid.NewString()
	}
	if req.Trigger != nil && req.Trigger.Event != nil {
		event := req.Trigger.Event.Event
		if event != nil {
			if eventID := strings.TrimSpace(event.ID); eventID != "" {
				return "event-id:" + eventID
			}
			if !event.Time.IsZero() {
				return "event-time:" + event.Time.UTC().Format(time.RFC3339Nano)
			}
		}
		if activationID := strings.TrimSpace(req.Trigger.Event.ActivationID); activationID != "" {
			return "event-activation:" + activationID + ":" + uuid.NewString()
		}
		return "event-non-idempotent:" + uuid.NewString()
	}
	return ""
}

func WorkflowStepIdempotencyKey(req Request, invocationScope, stepID, suffix string) string {
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

func WorkflowRunContext(req Request) map[string]any {
	ctxValue := map[string]any{}
	if runID := strings.TrimSpace(req.RunID); runID != "" {
		ctxValue["runId"] = runID
	}
	if providerName := strings.TrimSpace(req.ProviderName); providerName != "" {
		ctxValue["provider"] = providerName
	}
	if target := WorkflowTargetContext(req.Target); len(target) > 0 {
		ctxValue["target"] = target
	}
	if trigger := WorkflowTriggerContext(req.Trigger); len(trigger) > 0 {
		ctxValue["trigger"] = trigger
	}
	if req.Input != nil {
		ctxValue["input"] = maps.Clone(req.Input)
	}
	if req.Metadata != nil {
		ctxValue["metadata"] = maps.Clone(req.Metadata)
	}
	if len(req.Signals) > 0 {
		ctxValue["signals"] = WorkflowSignalsContext(req.Signals)
	}
	if createdBySubjectID := strings.TrimSpace(req.CreatedBySubjectID); createdBySubjectID != "" {
		ctxValue["createdBySubjectId"] = createdBySubjectID
	}
	if runAs := WorkflowSubjectContext(req.RunAs); len(runAs) > 0 {
		ctxValue["runAs"] = runAs
	}
	return ctxValue
}

func WorkflowTargetContext(target *gestalt.BoundWorkflowTarget) map[string]any {
	value := map[string]any{}
	if target == nil || len(target.Steps) == 0 {
		return value
	}
	steps := make([]map[string]any, 0, len(target.Steps))
	for i := range target.Steps {
		step := target.Steps[i]
		item := map[string]any{"id": strings.TrimSpace(step.ID)}
		switch {
		case step.App != nil:
			item["kind"] = "app"
			item["app"] = strings.TrimSpace(step.App.Name)
			item["operation"] = strings.TrimSpace(step.App.Operation)
			if connection := strings.TrimSpace(step.App.Connection); connection != "" {
				item["connection"] = connection
			}
			if instance := strings.TrimSpace(step.App.Instance); instance != "" {
				item["instance"] = instance
			}
			if credentialMode := strings.TrimSpace(step.App.CredentialMode); credentialMode != "" {
				item["credentialMode"] = credentialMode
			}
		case step.Agent != nil:
			item["kind"] = "agent"
			item["agentProvider"] = strings.TrimSpace(step.Agent.Provider)
			item["model"] = strings.TrimSpace(step.Agent.Model)
		default:
			item["kind"] = "unknown"
		}
		steps = append(steps, item)
	}
	value["kind"] = "steps"
	value["steps"] = steps
	return value
}

func WorkflowTriggerContext(trigger *gestalt.WorkflowRunTrigger) map[string]any {
	if trigger == nil {
		return nil
	}
	switch {
	case trigger.Schedule != nil:
		value := map[string]any{"kind": "schedule", "activationId": trigger.Schedule.ActivationID}
		if trigger.Schedule.ScheduledFor != nil {
			value["scheduledFor"] = trigger.Schedule.ScheduledFor.UTC().Format(time.RFC3339Nano)
		}
		return value
	case trigger.Event != nil:
		value := map[string]any{"kind": "event", "activationId": trigger.Event.ActivationID}
		if event := WorkflowEventContext(trigger.Event.Event); len(event) > 0 {
			value["event"] = event
		}
		return value
	case trigger.Manual:
		return map[string]any{"kind": "manual"}
	default:
		return nil
	}
}

func WorkflowEventContext(event *gestalt.WorkflowEvent) map[string]any {
	value := map[string]any{}
	if event == nil {
		return value
	}
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
	if !event.Time.IsZero() {
		value["time"] = event.Time.UTC().Format(time.RFC3339Nano)
	}
	if event.DataContentType != "" {
		value["dataContentType"] = event.DataContentType
	}
	if event.Data != nil {
		value["data"] = cloneWorkflowJSONValue(event.Data)
	}
	if event.Extensions != nil {
		value["extensions"] = maps.Clone(event.Extensions)
	}
	return value
}

func WorkflowSubjectContext(subject *gestalt.Subject) map[string]any {
	value := map[string]any{}
	if subject == nil {
		return value
	}
	if id := strings.TrimSpace(subject.ID); id != "" {
		value["id"] = id
	}
	if credentialSubjectID := strings.TrimSpace(subject.CredentialSubjectID); credentialSubjectID != "" {
		value["credentialSubjectId"] = credentialSubjectID
	}
	if email := strings.TrimSpace(subject.Email); email != "" {
		value["email"] = email
	}
	return value
}

func WorkflowSignalsContext(signals []gestalt.WorkflowSignal) []map[string]any {
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
			if payload := compactWorkflowSignalPayload(workflowMapValue(signal.Payload)); len(payload) > 0 {
				value["payload"] = payload
			}
		}
		if signal.Metadata != nil {
			if metadata, ok := compactWorkflowJSONValue(signal.Metadata, workflowSignalContextMaxDepth).(map[string]any); ok && len(metadata) > 0 {
				value["metadata"] = metadata
			}
		}
		if createdBySubjectID := strings.TrimSpace(signal.CreatedBySubjectID); createdBySubjectID != "" {
			value["createdBySubjectId"] = createdBySubjectID
		}
		if !signal.CreatedAt.IsZero() {
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
	for _, key := range []string{
		"delivery_id", "deliveryId", "github_event", "githubEvent", "github_action", "githubAction",
		"event", "action", "summary", "user_prompt", "userPrompt", "payload_sha256", "payloadSha256",
		"payload_omitted", "payloadOmitted",
	} {
		copyCompactPayloadField(out, payload, key)
	}
	for _, key := range []string{
		"agent_request", "agentRequest", "installation", "repository", "sender", "webhook_policy",
		"webhookPolicy", "pull_request", "pullRequest", "issue", "comment", "review", "ref",
		"check_run", "checkRun", "check_suite", "checkSuite", "workflow_run", "workflowRun",
		"review_check_run", "reviewCheckRun",
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
		if compact, ok := compactWorkflowJSONScalar(payload[key]); ok {
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
	case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
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

func workflowMapValue(value any) map[string]any {
	if value == nil {
		return nil
	}
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return map[string]any{"value": value}
}

func cloneWorkflowJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = cloneWorkflowJSONValue(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i := range typed {
			out[i] = cloneWorkflowJSONValue(typed[i])
		}
		return out
	default:
		return typed
	}
}
