package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/google/uuid"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

type desiredWorkflowConfigEventTrigger struct {
	TriggerKey   string
	ProviderName string
	TriggerID    string
	trigger      config.WorkflowEventTriggerConfig
}

func reconcileWorkflowConfigEventTriggers(ctx context.Context, cfg *config.Config, runtime *workflowRuntime) error {
	if cfg == nil || runtime == nil {
		return nil
	}
	desired, err := desiredWorkflowConfigEventTriggers(cfg)
	if err != nil {
		return err
	}

	for _, rowID := range slices.Sorted(maps.Keys(desired)) {
		desiredEntry := desired[rowID]
		trigger := desiredEntry.trigger
		target := workflowConfigTarget(trigger.Target)
		pluginName := workflowConfigTargetLabel(target)
		providerName, provider, err := runtime.ResolveProviderSelection(trigger.Provider)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow event trigger %q for plugin %q: %w", desiredEntry.TriggerKey, pluginName, err)
		}
		existingExecutionRef := ""
		providerCtx := invocation.WithWorkflowContextString(ctx, "plugin", pluginName)
		existing, err := provider.GetEventTrigger(providerCtx, coreworkflow.GetEventTriggerRequest{
			TriggerID: desiredEntry.TriggerID,
		})
		switch {
		case err == nil:
			if !isWorkflowConfigOwnedEventTrigger(existing, pluginName, desiredEntry.TriggerID) {
				return fmt.Errorf("bootstrap: workflow event trigger %q for plugin %q conflicts with existing unmanaged trigger id %q", desiredEntry.TriggerKey, pluginName, desiredEntry.TriggerID)
			}
			existingExecutionRef = strings.TrimSpace(existing.ExecutionRef)
		case isWorkflowObjectNotFound(err):
		default:
			return fmt.Errorf("bootstrap: get workflow event trigger %q for plugin %q: %w", desiredEntry.TriggerID, pluginName, err)
		}
		desiredExecutionRef, err := workflowConfigExecutionReference(cfg, providerName, target, trigger.Permissions)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow event trigger %q for plugin %q: %w", desiredEntry.TriggerKey, pluginName, err)
		}
		executionRefs, err := workflowExecutionReferenceStore(providerName, provider)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow event trigger %q for plugin %q: %w", desiredEntry.TriggerKey, pluginName, err)
		}
		executionRefID, createdExecutionRef, replacedUnreadableExecutionRef, replacedUnreadableExecutionRefErr, err := workflowEnsureConfigExecutionRef(
			ctx,
			executionRefs,
			desiredExecutionRef,
			workflowConfigEventTriggerExecutionRefID(desiredEntry.TriggerID),
			existingExecutionRef,
		)
		if err != nil {
			if existingExecutionRef != "" && workflowConfigEventTriggerDefinitionMatches(existing, target, trigger) {
				continue
			}
			return fmt.Errorf("bootstrap: store workflow execution ref for event trigger %q on plugin %q: %w", desiredEntry.TriggerKey, pluginName, err)
		}
		if _, err := provider.UpsertEventTrigger(providerCtx, coreworkflow.UpsertEventTriggerRequest{
			TriggerID:    desiredEntry.TriggerID,
			Match:        workflowConfigEventTriggerMatch(trigger),
			Target:       target,
			Paused:       trigger.Paused,
			RequestedBy:  workflowConfigActor(),
			ExecutionRef: executionRefID,
		}); err != nil {
			if createdExecutionRef {
				_ = workflowRevokeExecutionRefByID(ctx, executionRefs, executionRefID)
			}
			return fmt.Errorf("bootstrap: workflow event trigger %q for plugin %q: %w", desiredEntry.TriggerKey, pluginName, err)
		}
		if replacedUnreadableExecutionRef != "" {
			workflowLogReplacedUnreadableExecutionRef(ctx, "event_trigger", desiredEntry.TriggerKey, desiredEntry.TriggerID, providerName, pluginName, replacedUnreadableExecutionRef, executionRefID, replacedUnreadableExecutionRefErr)
		}
		if existingExecutionRef != executionRefID && replacedUnreadableExecutionRef == "" {
			if err := workflowRevokeExecutionRefByID(ctx, executionRefs, existingExecutionRef); err != nil {
				return fmt.Errorf("bootstrap: revoke workflow execution ref %q for event trigger %q on plugin %q: %w", existingExecutionRef, desiredEntry.TriggerID, pluginName, err)
			}
		}
	}

	if err := cleanupRemovedWorkflowConfigEventTriggers(ctx, runtime, desired); err != nil {
		return err
	}
	return nil
}

func desiredWorkflowConfigEventTriggers(cfg *config.Config) (map[string]desiredWorkflowConfigEventTrigger, error) {
	desired := make(map[string]desiredWorkflowConfigEventTrigger)
	if cfg == nil {
		return desired, nil
	}
	for _, triggerKey := range slices.Sorted(maps.Keys(cfg.Workflows.EventTriggers)) {
		trigger := cfg.Workflows.EventTriggers[triggerKey]
		providerName, _, err := cfg.EffectiveWorkflowProvider(trigger.Provider)
		if err != nil {
			return nil, err
		}
		rowID := strings.TrimSpace(triggerKey)
		desired[rowID] = desiredWorkflowConfigEventTrigger{
			TriggerKey:   triggerKey,
			ProviderName: providerName,
			TriggerID:    workflowConfigEventTriggerID(triggerKey),
			trigger:      trigger,
		}
	}
	return desired, nil
}

func cleanupRemovedWorkflowConfigEventTriggers(ctx context.Context, runtime *workflowRuntime, desired map[string]desiredWorkflowConfigEventTrigger) error {
	desiredByProviderTrigger := make(map[string]struct{}, len(desired))
	for rowID := range desired {
		entry := desired[rowID]
		desiredByProviderTrigger[workflowConfigProviderObjectKey(entry.ProviderName, entry.TriggerID)] = struct{}{}
	}
	for _, providerName := range runtime.ProviderNames() {
		provider, err := runtime.ResolveProvider(providerName)
		if err != nil {
			return fmt.Errorf("bootstrap: cleanup workflow event triggers requires provider %q: %w", providerName, err)
		}
		triggers, err := provider.ListEventTriggers(ctx, coreworkflow.ListEventTriggersRequest{})
		if err != nil {
			workflowLogSkippedConfigCleanup(ctx, "event_triggers", providerName, err)
			continue
		}
		var executionRefs coreworkflow.ExecutionReferenceStore
		for _, trigger := range triggers {
			if trigger == nil || !isWorkflowConfigOwnedEventTrigger(trigger, workflowConfigTargetLabel(trigger.Target), trigger.ID) {
				continue
			}
			if _, ok := desiredByProviderTrigger[workflowConfigProviderObjectKey(providerName, trigger.ID)]; ok {
				continue
			}
			pluginName := workflowConfigTargetLabel(trigger.Target)
			providerCtx := invocation.WithWorkflowContextString(ctx, "plugin", pluginName)
			if err := provider.DeleteEventTrigger(providerCtx, coreworkflow.DeleteEventTriggerRequest{TriggerID: trigger.ID}); err != nil && !isWorkflowObjectNotFound(err) {
				return fmt.Errorf("bootstrap: delete workflow event trigger %q for plugin %q: %w", trigger.ID, pluginName, err)
			}
			if executionRefs == nil {
				executionRefs, err = workflowExecutionReferenceStore(providerName, provider)
				if err != nil {
					return fmt.Errorf("bootstrap: cleanup workflow event triggers for provider %q: %w", providerName, err)
				}
			}
			if err := workflowRevokeExecutionRefByID(ctx, executionRefs, trigger.ExecutionRef); err != nil {
				return fmt.Errorf("bootstrap: revoke workflow execution ref %q for event trigger %q on plugin %q: %w", trigger.ExecutionRef, trigger.ID, pluginName, err)
			}
		}
	}
	return nil
}

func workflowConfigEventTriggerDefinitionMatches(existing *coreworkflow.EventTrigger, target coreworkflow.Target, trigger config.WorkflowEventTriggerConfig) bool {
	if existing == nil {
		return false
	}
	return existing.Paused == trigger.Paused &&
		reflect.DeepEqual(existing.Match, workflowConfigEventTriggerMatch(trigger)) &&
		reflect.DeepEqual(existing.Target, target)
}

func isWorkflowConfigOwnedEventTrigger(existing *coreworkflow.EventTrigger, pluginName, triggerID string) bool {
	if existing == nil {
		return false
	}
	actor := workflowConfigActor()
	return existing.ID == triggerID &&
		workflowConfigTargetLabel(existing.Target) == pluginName &&
		existing.CreatedBy.SubjectID == actor.SubjectID &&
		existing.CreatedBy.SubjectKind == actor.SubjectKind &&
		existing.CreatedBy.AuthSource == actor.AuthSource
}

func workflowConfigEventTriggerMatch(trigger config.WorkflowEventTriggerConfig) coreworkflow.EventMatch {
	return coreworkflow.EventMatch{
		Type:    trigger.Match.Type,
		Source:  trigger.Match.Source,
		Subject: trigger.Match.Subject,
	}
}

func workflowConfigEventTriggerID(triggerKey string) string {
	sum := sha256.Sum256([]byte("event_trigger\x00" + strings.TrimSpace(triggerKey)))
	return coreworkflow.ConfigManagedSchedulePrefix + hex.EncodeToString(sum[:])
}

func workflowConfigEventTriggerExecutionRefID(triggerID string) string {
	return "workflow_event_trigger:" + strings.TrimSpace(triggerID) + ":" + uuid.NewString()
}
