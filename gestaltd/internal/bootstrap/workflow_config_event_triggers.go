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
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

type desiredWorkflowConfigEventTrigger struct {
	TriggerKey   string
	ProviderName string
	TriggerID    string
	trigger      config.WorkflowEventTriggerConfig
}

func reconcileWorkflowConfigEventTriggers(ctx context.Context, cfg *config.Config, runtime *workflowRuntime, includeProvider workflowConfigProviderFilter) error {
	if cfg == nil || runtime == nil {
		return nil
	}
	desired, err := desiredWorkflowConfigEventTriggers(cfg)
	if err != nil {
		return err
	}

	for _, rowID := range slices.Sorted(maps.Keys(desired)) {
		desiredEntry := desired[rowID]
		if !workflowConfigProviderIncluded(includeProvider, desiredEntry.ProviderName) {
			continue
		}
		trigger := desiredEntry.trigger
		target := workflowConfigTarget(trigger.Target)
		appName := workflowConfigTargetLabel(target)
		providerName, provider, err := runtime.ResolveProviderSelection(trigger.Provider)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow event trigger %q for app %q: %w", desiredEntry.TriggerKey, appName, err)
		}
		existingExecutionRef := ""
		providerCtx := invocation.WithWorkflowContextString(ctx, "app", appName)
		existing, err := provider.GetEventTrigger(providerCtx, coreworkflow.GetEventTriggerRequest{
			TriggerID: desiredEntry.TriggerID,
		})
		switch {
		case err == nil:
			if !isWorkflowConfigOwnedEventTrigger(existing, appName, desiredEntry.TriggerID) {
				return fmt.Errorf("bootstrap: workflow event trigger %q for app %q conflicts with existing unmanaged trigger id %q", desiredEntry.TriggerKey, appName, desiredEntry.TriggerID)
			}
			existingExecutionRef = strings.TrimSpace(existing.ExecutionRef)
		case isWorkflowObjectNotFound(err):
		default:
			return fmt.Errorf("bootstrap: get workflow event trigger %q for app %q: %w", desiredEntry.TriggerID, appName, err)
		}
		runAs, err := workflowConfigRunAsSubject("workflows.eventTriggers."+desiredEntry.TriggerKey+".runAs", trigger.RunAs)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow event trigger %q for app %q: %w", desiredEntry.TriggerKey, appName, err)
		}
		permissions, err := workflowConfigExecutionPermissions(cfg, "workflows.eventTriggers."+desiredEntry.TriggerKey, trigger.Invokes)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow event trigger %q for app %q: %w", desiredEntry.TriggerKey, appName, err)
		}
		desiredExecutionRef, err := workflowConfigExecutionReference(cfg, providerName, target, runAs, permissions)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow event trigger %q for app %q: %w", desiredEntry.TriggerKey, appName, err)
		}
		executionRefs, err := workflowExecutionReferenceStore(providerName, provider)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow event trigger %q for app %q: %w", desiredEntry.TriggerKey, appName, err)
		}
		executionRefID, createdExecutionRef, replacedUnreadableExecutionRef, replacedUnreadableExecutionRefErr, err := workflowEnsureConfigExecutionRef(
			ctx,
			executionRefs,
			desiredExecutionRef,
			workflowConfigEventTriggerExecutionRefID(desiredEntry.TriggerID),
			existingExecutionRef,
		)
		if err != nil {
			return fmt.Errorf("bootstrap: store workflow execution ref for event trigger %q on app %q: %w", desiredEntry.TriggerKey, appName, err)
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
			return fmt.Errorf("bootstrap: workflow event trigger %q for app %q: %w", desiredEntry.TriggerKey, appName, err)
		}
		if replacedUnreadableExecutionRef != "" {
			workflowLogReplacedUnreadableExecutionRef(ctx, "event_trigger", desiredEntry.TriggerKey, desiredEntry.TriggerID, providerName, appName, replacedUnreadableExecutionRef, executionRefID, replacedUnreadableExecutionRefErr)
		}
		if existingExecutionRef != executionRefID && replacedUnreadableExecutionRef == "" {
			if err := workflowRevokeExecutionRefByID(ctx, executionRefs, existingExecutionRef); err != nil {
				return fmt.Errorf("bootstrap: revoke workflow execution ref %q for event trigger %q on app %q: %w", existingExecutionRef, desiredEntry.TriggerID, appName, err)
			}
		}
	}

	if err := cleanupRemovedWorkflowConfigEventTriggers(ctx, runtime, desired, includeProvider); err != nil {
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

func cleanupRemovedWorkflowConfigEventTriggers(ctx context.Context, runtime *workflowRuntime, desired map[string]desiredWorkflowConfigEventTrigger, includeProvider workflowConfigProviderFilter) error {
	desiredByProviderTrigger := make(map[string]struct{}, len(desired))
	for rowID := range desired {
		entry := desired[rowID]
		if !workflowConfigProviderIncluded(includeProvider, entry.ProviderName) {
			continue
		}
		desiredByProviderTrigger[workflowConfigProviderObjectKey(entry.ProviderName, entry.TriggerID)] = struct{}{}
	}
	for _, providerName := range runtime.ProviderNames() {
		if !workflowConfigProviderIncluded(includeProvider, providerName) {
			continue
		}
		provider, err := runtime.ResolveProvider(providerName)
		if err != nil {
			return fmt.Errorf("bootstrap: cleanup workflow event triggers requires provider %q: %w", providerName, err)
		}
		triggers, err := provider.ListEventTriggers(ctx, coreworkflow.ListEventTriggersRequest{})
		if err != nil {
			workflowLogSkippedConfigWorkflowCleanup(ctx, "event_triggers", providerName, err)
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
			appName := workflowConfigTargetLabel(trigger.Target)
			providerCtx := invocation.WithWorkflowContextString(ctx, "app", appName)
			if err := provider.DeleteEventTrigger(providerCtx, coreworkflow.DeleteEventTriggerRequest{TriggerID: trigger.ID}); err != nil && !isWorkflowObjectNotFound(err) {
				return fmt.Errorf("bootstrap: delete workflow event trigger %q for app %q: %w", trigger.ID, appName, err)
			}
			if executionRefs == nil {
				executionRefs, err = workflowExecutionReferenceStore(providerName, provider)
				if err != nil {
					return fmt.Errorf("bootstrap: cleanup workflow event triggers for provider %q: %w", providerName, err)
				}
			}
			if err := workflowRevokeExecutionRefByID(ctx, executionRefs, trigger.ExecutionRef); err != nil {
				return fmt.Errorf("bootstrap: revoke workflow execution ref %q for event trigger %q on app %q: %w", trigger.ExecutionRef, trigger.ID, appName, err)
			}
		}
	}
	return nil
}

func isWorkflowConfigOwnedEventTrigger(existing *coreworkflow.EventTrigger, appName, triggerID string) bool {
	if existing == nil {
		return false
	}
	actor := workflowConfigActor()
	return existing.ID == triggerID &&
		workflowConfigTargetLabel(existing.Target) == appName &&
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
