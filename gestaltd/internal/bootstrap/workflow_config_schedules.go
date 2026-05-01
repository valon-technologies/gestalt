package bootstrap

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type desiredWorkflowConfigSchedule struct {
	ScheduleKey  string
	ProviderName string
	ScheduleID   string
	schedule     config.WorkflowScheduleConfig
}

func reconcileWorkflowConfigSchedules(ctx context.Context, cfg *config.Config, runtime *workflowRuntime) error {
	if cfg == nil || runtime == nil {
		return nil
	}
	desired, err := desiredWorkflowConfigSchedules(cfg)
	if err != nil {
		return err
	}

	for _, rowID := range slices.Sorted(maps.Keys(desired)) {
		desiredEntry := desired[rowID]
		schedule := desiredEntry.schedule
		target := workflowConfigTarget(schedule.Target)
		pluginName := workflowConfigTargetLabel(target)
		providerName, provider, err := runtime.ResolveProviderSelection(schedule.Provider)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow schedule %q for plugin %q: %w", desiredEntry.ScheduleKey, pluginName, err)
		}
		existingExecutionRef := ""
		providerCtx := invocation.WithWorkflowContextString(ctx, "plugin", pluginName)
		existing, err := provider.GetSchedule(providerCtx, coreworkflow.GetScheduleRequest{
			ScheduleID: desiredEntry.ScheduleID,
		})
		switch {
		case err == nil:
			if !isWorkflowConfigOwnedSchedule(existing, pluginName, desiredEntry.ScheduleID) {
				return fmt.Errorf("bootstrap: workflow schedule %q for plugin %q conflicts with existing unmanaged schedule id %q", desiredEntry.ScheduleKey, pluginName, desiredEntry.ScheduleID)
			}
		case isWorkflowObjectNotFound(err):
			existing = nil
		default:
			return fmt.Errorf("bootstrap: get workflow schedule %q for plugin %q: %w", desiredEntry.ScheduleID, pluginName, err)
		}
		if existing != nil {
			existingExecutionRef = strings.TrimSpace(existing.ExecutionRef)
		}
		desiredExecutionRef, err := workflowConfigExecutionReference(cfg, providerName, target, schedule.Permissions)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow schedule %q for plugin %q: %w", desiredEntry.ScheduleKey, pluginName, err)
		}
		executionRefs, err := workflowExecutionReferenceStore(providerName, provider)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow schedule %q for plugin %q: %w", desiredEntry.ScheduleKey, pluginName, err)
		}
		executionRefID, createdExecutionRef, err := workflowEnsureConfigExecutionRef(
			ctx,
			executionRefs,
			desiredExecutionRef,
			workflowConfigScheduleExecutionRefID(desiredEntry.ScheduleID),
			existingExecutionRef,
		)
		if err != nil {
			return fmt.Errorf("bootstrap: store workflow execution ref for schedule %q on plugin %q: %w", desiredEntry.ScheduleKey, pluginName, err)
		}
		if _, err := provider.UpsertSchedule(providerCtx, coreworkflow.UpsertScheduleRequest{
			ScheduleID:   desiredEntry.ScheduleID,
			Cron:         schedule.Cron,
			Timezone:     schedule.Timezone,
			Target:       target,
			Paused:       schedule.Paused,
			RequestedBy:  workflowConfigActor(),
			ExecutionRef: executionRefID,
		}); err != nil {
			if createdExecutionRef {
				_ = workflowRevokeExecutionRefByID(ctx, executionRefs, executionRefID)
			}
			return fmt.Errorf("bootstrap: workflow schedule %q for plugin %q: %w", desiredEntry.ScheduleKey, pluginName, err)
		}
		if existingExecutionRef != executionRefID {
			if err := workflowRevokeExecutionRefByID(ctx, executionRefs, existingExecutionRef); err != nil {
				return fmt.Errorf("bootstrap: revoke workflow execution ref %q for schedule %q on plugin %q: %w", existingExecutionRef, desiredEntry.ScheduleID, pluginName, err)
			}
		}
	}

	if err := cleanupRemovedWorkflowConfigSchedules(ctx, runtime, desired); err != nil {
		return err
	}
	return nil
}

func desiredWorkflowConfigSchedules(cfg *config.Config) (map[string]desiredWorkflowConfigSchedule, error) {
	desired := make(map[string]desiredWorkflowConfigSchedule)
	if cfg == nil {
		return desired, nil
	}
	for _, scheduleKey := range slices.Sorted(maps.Keys(cfg.Workflows.Schedules)) {
		schedule := cfg.Workflows.Schedules[scheduleKey]
		providerName, _, err := cfg.EffectiveWorkflowProvider(schedule.Provider)
		if err != nil {
			return nil, err
		}
		rowID := strings.TrimSpace(scheduleKey)
		desired[rowID] = desiredWorkflowConfigSchedule{
			ScheduleKey:  scheduleKey,
			ProviderName: providerName,
			ScheduleID:   workflowConfigScheduleID(scheduleKey),
			schedule:     schedule,
		}
	}
	return desired, nil
}

func isWorkflowObjectNotFound(err error) bool {
	return errors.Is(err, core.ErrNotFound) || status.Code(err) == codes.NotFound
}

func cleanupRemovedWorkflowConfigSchedules(ctx context.Context, runtime *workflowRuntime, desired map[string]desiredWorkflowConfigSchedule) error {
	desiredByProviderSchedule := make(map[string]struct{}, len(desired))
	for rowID := range desired {
		entry := desired[rowID]
		desiredByProviderSchedule[workflowConfigProviderObjectKey(entry.ProviderName, entry.ScheduleID)] = struct{}{}
	}
	for _, providerName := range runtime.ProviderNames() {
		provider, err := runtime.ResolveProvider(providerName)
		if err != nil {
			return fmt.Errorf("bootstrap: cleanup workflow schedules requires provider %q: %w", providerName, err)
		}
		schedules, err := provider.ListSchedules(ctx, coreworkflow.ListSchedulesRequest{})
		if err != nil {
			return fmt.Errorf("bootstrap: list workflow schedules for provider %q: %w", providerName, err)
		}
		var executionRefs coreworkflow.ExecutionReferenceStore
		for _, schedule := range schedules {
			if schedule == nil || !isWorkflowConfigOwnedSchedule(schedule, workflowConfigTargetLabel(schedule.Target), schedule.ID) {
				continue
			}
			if _, ok := desiredByProviderSchedule[workflowConfigProviderObjectKey(providerName, schedule.ID)]; ok {
				continue
			}
			pluginName := workflowConfigTargetLabel(schedule.Target)
			providerCtx := invocation.WithWorkflowContextString(ctx, "plugin", pluginName)
			if err := provider.DeleteSchedule(providerCtx, coreworkflow.DeleteScheduleRequest{ScheduleID: schedule.ID}); err != nil && !isWorkflowObjectNotFound(err) {
				return fmt.Errorf("bootstrap: delete workflow schedule %q for plugin %q: %w", schedule.ID, pluginName, err)
			}
			if executionRefs == nil {
				executionRefs, err = workflowExecutionReferenceStore(providerName, provider)
				if err != nil {
					return fmt.Errorf("bootstrap: cleanup workflow schedules for provider %q: %w", providerName, err)
				}
			}
			if err := workflowRevokeExecutionRefByID(ctx, executionRefs, schedule.ExecutionRef); err != nil {
				return fmt.Errorf("bootstrap: revoke workflow execution ref %q for schedule %q on plugin %q: %w", schedule.ExecutionRef, schedule.ID, pluginName, err)
			}
		}
	}
	return nil
}

func workflowConfigProviderObjectKey(providerName, objectID string) string {
	return strings.TrimSpace(providerName) + "\x00" + strings.TrimSpace(objectID)
}

func isWorkflowConfigOwnedSchedule(existing *coreworkflow.Schedule, pluginName, scheduleID string) bool {
	if existing == nil {
		return false
	}
	actor := workflowConfigActor()
	return existing.ID == scheduleID &&
		workflowConfigTargetLabel(existing.Target) == pluginName &&
		existing.CreatedBy.SubjectID == actor.SubjectID &&
		existing.CreatedBy.SubjectKind == actor.SubjectKind &&
		existing.CreatedBy.AuthSource == actor.AuthSource
}

func workflowConfigTargetLabel(target coreworkflow.Target) string {
	if target.Agent != nil {
		providerName := strings.TrimSpace(target.Agent.ProviderName)
		if providerName == "" {
			providerName = "default"
		}
		return "agent:" + providerName
	}
	if target.Plugin == nil {
		return ""
	}
	return strings.TrimSpace(target.Plugin.PluginName)
}

func workflowConfigTarget(target *config.WorkflowTargetConfig) coreworkflow.Target {
	if target == nil {
		return coreworkflow.Target{}
	}
	if target.Agent != nil {
		return coreworkflow.Target{Agent: workflowConfigAgentTarget(target.Agent)}
	}
	plugin := target.Plugin
	if plugin == nil {
		return coreworkflow.Target{}
	}
	pluginTarget := coreworkflow.PluginTarget{
		PluginName: plugin.Name,
		Operation:  plugin.Operation,
		Connection: plugin.Connection,
		Instance:   plugin.Instance,
		Input:      maps.Clone(plugin.Input),
	}
	return coreworkflow.Target{
		Plugin: &pluginTarget,
	}
}

func workflowConfigAgentTarget(agent *config.WorkflowAgentConfig) *coreworkflow.AgentTarget {
	if agent == nil {
		return nil
	}
	timeoutSeconds := 0
	if timeout := strings.TrimSpace(agent.Timeout); timeout != "" {
		if parsed, err := time.ParseDuration(timeout); err == nil {
			timeoutSeconds = int(parsed.Seconds())
		}
	}
	messages := make([]coreagent.Message, 0, len(agent.Messages))
	for _, message := range agent.Messages {
		messages = append(messages, coreagent.Message{
			Role:     strings.TrimSpace(message.Role),
			Text:     strings.TrimSpace(message.Text),
			Metadata: maps.Clone(message.Metadata),
		})
	}
	tools := make([]coreagent.ToolRef, 0, len(agent.Tools))
	for _, tool := range agent.Tools {
		tools = append(tools, coreagent.ToolRef{
			System:      strings.TrimSpace(tool.System),
			Plugin:      strings.TrimSpace(tool.Plugin),
			Operation:   strings.TrimSpace(tool.Operation),
			Connection:  strings.TrimSpace(tool.Connection),
			Instance:    strings.TrimSpace(tool.Instance),
			Title:       strings.TrimSpace(tool.Title),
			Description: strings.TrimSpace(tool.Description),
		})
	}
	return &coreworkflow.AgentTarget{
		ProviderName:    strings.TrimSpace(agent.Provider),
		Model:           strings.TrimSpace(agent.Model),
		Prompt:          strings.TrimSpace(agent.Prompt),
		Messages:        messages,
		ToolRefs:        tools,
		OutputDelivery:  workflowConfigOutputDelivery(agent.OutputDelivery),
		ResponseSchema:  maps.Clone(agent.ResponseSchema),
		Metadata:        maps.Clone(agent.Metadata),
		ProviderOptions: maps.Clone(agent.ProviderOptions),
		TimeoutSeconds:  timeoutSeconds,
	}
}

func workflowConfigOutputDelivery(delivery *config.WorkflowOutputDeliveryConfig) *coreworkflow.OutputDelivery {
	if delivery == nil {
		return nil
	}
	out := &coreworkflow.OutputDelivery{
		Target: coreworkflow.PluginTarget{
			PluginName: strings.TrimSpace(delivery.Target.Name),
			Operation:  strings.TrimSpace(delivery.Target.Operation),
			Connection: strings.TrimSpace(delivery.Target.Connection),
			Instance:   strings.TrimSpace(delivery.Target.Instance),
			Input:      maps.Clone(delivery.Target.Input),
		},
		CredentialMode: core.ConnectionMode(strings.ToLower(strings.TrimSpace(string(delivery.CredentialMode)))),
		InputBindings:  make([]coreworkflow.OutputBinding, 0, len(delivery.InputBindings)),
	}
	for _, binding := range delivery.InputBindings {
		out.InputBindings = append(out.InputBindings, coreworkflow.OutputBinding{
			InputField: strings.TrimSpace(binding.InputField),
			Value: coreworkflow.OutputValueSource{
				AgentOutput:    strings.TrimSpace(binding.Value.AgentOutput),
				SignalPayload:  strings.TrimSpace(binding.Value.SignalPayload),
				SignalMetadata: strings.TrimSpace(binding.Value.SignalMetadata),
				Literal:        binding.Value.Literal,
			},
		})
	}
	return out
}

func workflowConfigActor() coreworkflow.Actor {
	return coreworkflow.Actor{
		SubjectID:   workflowConfigOwnerSubjectID(),
		SubjectKind: "system",
		DisplayName: "Workflow Config",
		AuthSource:  "config",
	}
}

func workflowConfigScheduleID(scheduleKey string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(scheduleKey)))
	return coreworkflow.ConfigManagedSchedulePrefix + hex.EncodeToString(sum[:])
}

func workflowConfigScheduleExecutionRefID(scheduleID string) string {
	return "workflow_schedule:" + scheduleID + ":" + uuid.NewString()
}

func workflowEnsureConfigExecutionRef(
	ctx context.Context,
	store coreworkflow.ExecutionReferenceStore,
	desired *coreworkflow.ExecutionReference,
	refID string,
	candidateIDs ...string,
) (string, bool, error) {
	if store == nil {
		return "", false, fmt.Errorf("workflow execution refs are not configured")
	}
	refID = strings.TrimSpace(refID)
	if refID == "" {
		return "", false, fmt.Errorf("workflow execution ref id is required")
	}
	for _, candidateID := range candidateIDs {
		candidateID = strings.TrimSpace(candidateID)
		if candidateID == "" {
			continue
		}
		existing, err := store.GetExecutionReference(ctx, candidateID)
		if err != nil {
			if isWorkflowObjectNotFound(err) {
				continue
			}
			return "", false, err
		}
		if workflowConfigExecutionRefMatches(existing, desired) {
			return candidateID, false, nil
		}
	}
	desired.ID = refID
	if _, err := store.PutExecutionReference(ctx, desired); err != nil {
		return "", false, err
	}
	return refID, true, nil
}

func workflowExecutionRefPermissionsForTarget(target coreworkflow.Target, explicit ...[]core.AccessPermission) []core.AccessPermission {
	var base []core.AccessPermission
	if target.Agent != nil {
		base = make([]core.AccessPermission, 0, len(target.Agent.ToolRefs)+1)
		for i := range target.Agent.ToolRefs {
			tool := target.Agent.ToolRefs[i]
			pluginName := strings.TrimSpace(tool.Plugin)
			operation := strings.TrimSpace(tool.Operation)
			if pluginName == "" || operation == "" {
				continue
			}
			base = append(base, core.AccessPermission{
				Plugin:     pluginName,
				Operations: []string{operation},
			})
		}
		if delivery := target.Agent.OutputDelivery; delivery != nil {
			pluginName := strings.TrimSpace(delivery.Target.PluginName)
			operation := strings.TrimSpace(delivery.Target.Operation)
			if pluginName != "" && operation != "" {
				base = append(base, core.AccessPermission{
					Plugin:     pluginName,
					Operations: []string{operation},
				})
			}
		}
		return workflowMergeExecutionRefPermissions(append([][]core.AccessPermission{base}, explicit...)...)
	}
	if target.Plugin == nil {
		return workflowMergeExecutionRefPermissions(explicit...)
	}
	pluginTarget := *target.Plugin
	pluginName := pluginTarget.PluginName
	operation := strings.TrimSpace(pluginTarget.Operation)
	if pluginName != "" && operation != "" {
		base = []core.AccessPermission{{
			Plugin:     pluginName,
			Operations: []string{operation},
		}}
	}
	return workflowMergeExecutionRefPermissions(append([][]core.AccessPermission{base}, explicit...)...)
}

func workflowMergeExecutionRefPermissions(groups ...[]core.AccessPermission) []core.AccessPermission {
	out := make([]core.AccessPermission, 0)
	pluginIndexes := map[string]int{}
	seenOperations := map[string]map[string]struct{}{}
	for _, group := range groups {
		for _, value := range group {
			plugin := strings.TrimSpace(value.Plugin)
			if plugin == "" {
				continue
			}
			operations := make([]string, 0, len(value.Operations))
			for _, operation := range value.Operations {
				operation = strings.TrimSpace(operation)
				if operation != "" {
					operations = append(operations, operation)
				}
			}
			if len(operations) == 0 {
				continue
			}
			idx, ok := pluginIndexes[plugin]
			if !ok {
				idx = len(out)
				pluginIndexes[plugin] = idx
				seenOperations[plugin] = map[string]struct{}{}
				out = append(out, core.AccessPermission{Plugin: plugin})
			}
			for _, operation := range operations {
				if _, exists := seenOperations[plugin][operation]; exists {
					continue
				}
				seenOperations[plugin][operation] = struct{}{}
				out[idx].Operations = append(out[idx].Operations, operation)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func workflowConfigExecutionReference(cfg *config.Config, providerName string, target coreworkflow.Target, permissions []core.AccessPermission) (*coreworkflow.ExecutionReference, error) {
	ref := &coreworkflow.ExecutionReference{
		ProviderName:        providerName,
		Target:              target,
		SubjectID:           workflowConfigOwnerSubjectID(),
		SubjectKind:         "system",
		DisplayName:         "Gestalt config",
		AuthSource:          "config",
		CredentialSubjectID: workflowConfigOwnerSubjectID(),
		Permissions:         workflowExecutionRefPermissionsForTarget(target, permissions),
	}
	if target.Agent != nil {
		for i := range target.Agent.ToolRefs {
			tool := target.Agent.ToolRefs[i]
			if strings.TrimSpace(tool.System) != "" {
				continue
			}
			if err := workflowConfigValidateNoUserCredentialTarget(cfg, coreworkflow.PluginTarget{
				PluginName: strings.TrimSpace(tool.Plugin),
				Operation:  strings.TrimSpace(tool.Operation),
				Connection: strings.TrimSpace(tool.Connection),
				Instance:   strings.TrimSpace(tool.Instance),
			}); err != nil {
				return nil, err
			}
		}
		if delivery := target.Agent.OutputDelivery; delivery != nil {
			if err := workflowConfigValidateNoUserCredentialTarget(cfg, delivery.Target); err != nil {
				return nil, err
			}
		}
		return ref, nil
	}
	if target.Plugin == nil {
		return nil, fmt.Errorf("workflow target plugin is required")
	}
	if err := workflowConfigValidateNoUserCredentialTarget(cfg, *target.Plugin); err != nil {
		return nil, err
	}
	return ref, nil
}

func workflowConfigValidateNoUserCredentialTarget(cfg *config.Config, target coreworkflow.PluginTarget) error {
	mode, err := workflowConfigTargetConnectionMode(cfg, target)
	if err != nil {
		return err
	}
	pluginName := strings.TrimSpace(target.PluginName)
	switch mode {
	case core.ConnectionModeNone, core.ConnectionModePlatform:
		return nil
	case core.ConnectionModeUser:
		return fmt.Errorf("config-managed workflows do not support user-credentialed plugin %q", pluginName)
	default:
		return fmt.Errorf("unsupported connection mode %q for config-managed workflow target %q", mode, pluginName)
	}
}

func workflowConfigTargetConnectionMode(cfg *config.Config, target coreworkflow.PluginTarget) (core.ConnectionMode, error) {
	if cfg == nil {
		return core.ConnectionModeNone, fmt.Errorf("workflow config is not available")
	}
	pluginName := strings.TrimSpace(target.PluginName)
	entry := cfg.Plugins[pluginName]
	if entry == nil {
		return core.ConnectionModeNone, fmt.Errorf("workflow target plugin %q is not configured", pluginName)
	}
	plan, err := config.BuildStaticConnectionPlan(entry, entry.ManifestSpec())
	if err != nil {
		return core.ConnectionModeNone, fmt.Errorf("workflow target plugin %q connection plan: %w", pluginName, err)
	}

	if connection := strings.TrimSpace(target.Connection); connection != "" {
		return workflowConfigConnectionModeForName(plan, pluginName, connection)
	}
	if operation := strings.TrimSpace(target.Operation); operation != "" {
		mode, ok, err := workflowConfigOperationConnectionMode(plan, entry.ManifestSpec(), target)
		if err != nil {
			return core.ConnectionModeNone, fmt.Errorf("workflow target plugin %q operation %q connection plan: %w", pluginName, operation, err)
		}
		if ok {
			return mode, nil
		}
	}
	return core.NormalizeConnectionMode(core.ConnectionMode(entry.ConnectionMode)), nil
}

func workflowConfigOperationConnectionMode(plan config.StaticConnectionPlan, manifestPlugin *providermanifestv1.Spec, target coreworkflow.PluginTarget) (core.ConnectionMode, bool, error) {
	connections, selectors, _, err := plan.RESTOperationConnectionBindings(manifestPlugin)
	if err != nil {
		return core.ConnectionModeNone, false, err
	}
	operation := strings.TrimSpace(target.Operation)
	if selector, ok := selectors[operation]; ok {
		connectionName, resolved := workflowConfigConnectionSelectorTargetConnection(selector, target.Input)
		if resolved {
			mode, err := workflowConfigConnectionModeForName(plan, target.PluginName, connectionName)
			return mode, true, err
		}
		if connectionName := strings.TrimSpace(connections[operation]); connectionName != "" {
			mode, err := workflowConfigConnectionModeForName(plan, target.PluginName, connectionName)
			return mode, true, err
		}
		mode, err := workflowConfigConnectionSelectorMode(plan, target.PluginName, selector)
		return mode, true, err
	}
	if connectionName := strings.TrimSpace(connections[operation]); connectionName != "" {
		mode, err := workflowConfigConnectionModeForName(plan, target.PluginName, connectionName)
		return mode, true, err
	}
	return core.ConnectionModeNone, false, nil
}

func workflowConfigConnectionSelectorTargetConnection(selector core.OperationConnectionSelector, input map[string]any) (string, bool) {
	parameter := strings.TrimSpace(selector.Parameter)
	if parameter == "" || len(input) == 0 {
		return "", false
	}
	value, ok := input[parameter]
	if !ok {
		return "", false
	}
	selectorValue, ok := value.(string)
	if !ok {
		return "", false
	}
	connectionName := selector.Values[strings.TrimSpace(selectorValue)]
	return strings.TrimSpace(connectionName), strings.TrimSpace(connectionName) != ""
}

func workflowConfigConnectionSelectorMode(plan config.StaticConnectionPlan, pluginName string, selector core.OperationConnectionSelector) (core.ConnectionMode, error) {
	hasPlatform := false
	for _, connectionName := range selector.Values {
		mode, err := workflowConfigConnectionModeForName(plan, pluginName, connectionName)
		if err != nil {
			return core.ConnectionModeNone, err
		}
		switch mode {
		case core.ConnectionModeUser:
			return core.ConnectionModeUser, nil
		case core.ConnectionModePlatform:
			hasPlatform = true
		}
	}
	if hasPlatform {
		return core.ConnectionModePlatform, nil
	}
	return core.ConnectionModeNone, nil
}

func workflowConfigConnectionModeForName(plan config.StaticConnectionPlan, pluginName, connectionName string) (core.ConnectionMode, error) {
	conn, ok := plan.LookupConnection(connectionName)
	if !ok {
		return core.ConnectionModeNone, fmt.Errorf("workflow target plugin %q connection %q is not configured", strings.TrimSpace(pluginName), strings.TrimSpace(connectionName))
	}
	return config.ConnectionModeForConnection(conn), nil
}

func workflowConfigOwnerSubjectID() string {
	return "system:config"
}

func workflowConfigExecutionRefMatches(existing, desired *coreworkflow.ExecutionReference) bool {
	if existing == nil || desired == nil {
		return false
	}
	if existing.RevokedAt != nil && !existing.RevokedAt.IsZero() {
		return false
	}
	if strings.TrimSpace(existing.ProviderName) != strings.TrimSpace(desired.ProviderName) {
		return false
	}
	if strings.TrimSpace(existing.SubjectID) != strings.TrimSpace(desired.SubjectID) {
		return false
	}
	if strings.TrimSpace(existing.SubjectKind) != strings.TrimSpace(desired.SubjectKind) {
		return false
	}
	if strings.TrimSpace(existing.DisplayName) != strings.TrimSpace(desired.DisplayName) {
		return false
	}
	if strings.TrimSpace(existing.AuthSource) != strings.TrimSpace(desired.AuthSource) {
		return false
	}
	if !coreworkflow.TargetsEqual(existing.Target, desired.Target) {
		return false
	}
	existingJSON, existingErr := json.Marshal(existing.Permissions)
	desiredJSON, desiredErr := json.Marshal(desired.Permissions)
	return existingErr == nil && desiredErr == nil && bytes.Equal(existingJSON, desiredJSON)
}

func workflowExecutionReferenceStore(providerName string, provider coreworkflow.Provider) (coreworkflow.ExecutionReferenceStore, error) {
	store, ok := provider.(coreworkflow.ExecutionReferenceStore)
	if !ok {
		return nil, fmt.Errorf("workflow provider %q does not support execution refs", strings.TrimSpace(providerName))
	}
	return store, nil
}

func workflowRevokeExecutionRefByID(ctx context.Context, store coreworkflow.ExecutionReferenceStore, refID string) error {
	if store == nil || strings.TrimSpace(refID) == "" {
		return nil
	}
	ref, err := store.GetExecutionReference(ctx, refID)
	if err != nil {
		if isWorkflowObjectNotFound(err) {
			return nil
		}
		return err
	}
	if ref == nil || ref.RevokedAt != nil {
		return nil
	}
	now := time.Now()
	ref.RevokedAt = &now
	_, err = store.PutExecutionReference(ctx, ref)
	return err
}
