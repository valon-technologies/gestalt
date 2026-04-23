package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/google/uuid"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
)

type desiredWorkflowConfigEventTrigger struct {
	ID           string
	PluginName   string
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
		pluginName := trigger.Plugin
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
			if !isWorkflowConfigOwnedEventTrigger(existing, pluginName, desiredEntry.TriggerID) &&
				!isAdoptableWorkflowEventTrigger(existing, trigger, desiredEntry.TriggerID) {
				return fmt.Errorf("bootstrap: workflow event trigger %q for plugin %q conflicts with existing unmanaged trigger id %q", desiredEntry.TriggerKey, pluginName, desiredEntry.TriggerID)
			}
			existingExecutionRef = workflowEventTriggerExecutionRef(existing, "")
		case isWorkflowObjectNotFound(err):
		default:
			return fmt.Errorf("bootstrap: get workflow event trigger %q for plugin %q: %w", desiredEntry.TriggerID, pluginName, err)
		}
		desiredExecutionRef, err := workflowConfigExecutionReference(cfg, providerName, workflowConfigEventTriggerTarget(trigger))
		if err != nil {
			return fmt.Errorf("bootstrap: workflow event trigger %q for plugin %q: %w", desiredEntry.TriggerKey, pluginName, err)
		}
		executionRefs, err := workflowExecutionReferenceStore(providerName, provider)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow event trigger %q for plugin %q: %w", desiredEntry.TriggerKey, pluginName, err)
		}
		executionRefID, createdExecutionRef, err := workflowEnsureConfigExecutionRef(
			ctx,
			executionRefs,
			desiredExecutionRef,
			workflowConfigEventTriggerExecutionRefID(desiredEntry.TriggerID),
			existingExecutionRef,
		)
		if err != nil {
			return fmt.Errorf("bootstrap: store workflow execution ref for event trigger %q on plugin %q: %w", desiredEntry.TriggerKey, pluginName, err)
		}
		if _, err := provider.UpsertEventTrigger(providerCtx, coreworkflow.UpsertEventTriggerRequest{
			TriggerID:    desiredEntry.TriggerID,
			Match:        workflowConfigEventTriggerMatch(trigger),
			Target:       workflowConfigEventTriggerTarget(trigger),
			Paused:       trigger.Paused,
			RequestedBy:  workflowConfigActor(),
			ExecutionRef: executionRefID,
		}); err != nil {
			if createdExecutionRef {
				_ = workflowRevokeExecutionRefByID(ctx, executionRefs, executionRefID)
			}
			return fmt.Errorf("bootstrap: workflow event trigger %q for plugin %q: %w", desiredEntry.TriggerKey, pluginName, err)
		}
		if existingExecutionRef != executionRefID {
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
		rowID := workflowConfigEventTriggerStateID(triggerKey)
		desired[rowID] = desiredWorkflowConfigEventTrigger{
			ID:           rowID,
			PluginName:   trigger.Plugin,
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
			return fmt.Errorf("bootstrap: list workflow event triggers for provider %q: %w", providerName, err)
		}
		var executionRefs coreworkflow.ExecutionReferenceStore
		for _, trigger := range triggers {
			if trigger == nil || !isWorkflowConfigOwnedEventTrigger(trigger, trigger.Target.PluginName, trigger.ID) {
				continue
			}
			if _, ok := desiredByProviderTrigger[workflowConfigProviderObjectKey(providerName, trigger.ID)]; ok {
				continue
			}
			pluginName := strings.TrimSpace(trigger.Target.PluginName)
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

func isAdoptableWorkflowEventTrigger(existing *coreworkflow.EventTrigger, trigger config.WorkflowEventTriggerConfig, triggerID string) bool {
	if existing == nil {
		return false
	}
	return existing.ID == triggerID &&
		existing.Match == workflowConfigEventTriggerMatch(trigger) &&
		existing.Paused == trigger.Paused &&
		existing.Target.PluginName == trigger.Plugin &&
		existing.Target.Operation == trigger.Operation &&
		existing.Target.Connection == trigger.Connection &&
		existing.Target.Instance == trigger.Instance &&
		workflowTargetInputsEqual(existing.Target.Input, trigger.Input)
}

func isWorkflowConfigOwnedEventTrigger(existing *coreworkflow.EventTrigger, pluginName, triggerID string) bool {
	if existing == nil {
		return false
	}
	actor := workflowConfigActor()
	return existing.ID == triggerID &&
		existing.Target.PluginName == pluginName &&
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

func workflowConfigEventTriggerTarget(trigger config.WorkflowEventTriggerConfig) coreworkflow.Target {
	return coreworkflow.Target{
		PluginName: trigger.Plugin,
		Operation:  trigger.Operation,
		Connection: trigger.Connection,
		Instance:   trigger.Instance,
		Input:      maps.Clone(trigger.Input),
	}
}

func workflowConfigEventTriggerStateID(triggerKey string) string {
	return strings.TrimSpace(triggerKey)
}

func workflowConfigEventTriggerID(triggerKey string) string {
	sum := sha256.Sum256([]byte("event_trigger\x00" + strings.TrimSpace(triggerKey)))
	return coreworkflow.ConfigManagedSchedulePrefix + hex.EncodeToString(sum[:])
}

func workflowConfigEventTriggerExecutionRefID(triggerID string) string {
	return "workflow_event_trigger:" + strings.TrimSpace(triggerID) + ":" + uuid.NewString()
}

func workflowEventTriggerExecutionRef(existing *coreworkflow.EventTrigger, fallback string) string {
	if existing != nil {
		if refID := strings.TrimSpace(existing.ExecutionRef); refID != "" {
			return refID
		}
	}
	return strings.TrimSpace(fallback)
}
