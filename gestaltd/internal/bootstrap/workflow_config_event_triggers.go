package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"strings"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

type desiredWorkflowConfigEventTrigger struct {
	TriggerKey   string
	ProviderName string
	TriggerID    string
	trigger      config.WorkflowEventTriggerConfig
}

func reconcileWorkflowConfigEventTriggers(ctx context.Context, cfg *config.Config, runtime *workflowRuntime, tokens *appaccessservice.InvocationTokenManager, includeProvider workflowConfigProviderFilter) error {
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
		_, provider, err := runtime.ResolveProviderSelection(trigger.Provider)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow event trigger %q for app %q: %w", desiredEntry.TriggerKey, appName, err)
		}
		providerCtx := invocation.WithWorkflowContextString(ctx, "app", appName)
		existing, err := provider.GetEventTrigger(providerCtx, coreworkflow.GetEventTriggerRequest{
			TriggerID: desiredEntry.TriggerID,
		})
		switch {
		case err == nil:
			if !isWorkflowConfigOwnedEventTrigger(existing, appName, desiredEntry.TriggerID) {
				return fmt.Errorf("bootstrap: workflow event trigger %q for app %q conflicts with existing unmanaged trigger id %q", desiredEntry.TriggerKey, appName, desiredEntry.TriggerID)
			}
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
		if err := workflowConfigValidateExecutionTarget(cfg, target, runAs, permissions); err != nil {
			return fmt.Errorf("bootstrap: workflow event trigger %q for app %q: %w", desiredEntry.TriggerKey, appName, err)
		}
		executionPermissions := workflowConfigExecutionPermissionsForTarget(target, permissions)
		providerCtx, err = workflowConfigInvocationContext(providerCtx, tokens, runAs, executionPermissions)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow event trigger %q for app %q: %w", desiredEntry.TriggerKey, appName, err)
		}
		if _, err := provider.UpsertEventTrigger(providerCtx, coreworkflow.UpsertEventTriggerRequest{
			TriggerID:   desiredEntry.TriggerID,
			Match:       workflowConfigEventTriggerMatch(trigger),
			Target:      target,
			Paused:      trigger.Paused,
			RequestedBy: workflowConfigActor(),
		}); err != nil {
			return fmt.Errorf("bootstrap: workflow event trigger %q for app %q: %w", desiredEntry.TriggerKey, appName, err)
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
