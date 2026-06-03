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
	"github.com/valon-technologies/gestalt/server/internal/agentwire"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/workflowwire"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
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
		_, provider, err := runtime.ResolveProvider(ctx, trigger.Provider)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow event trigger %q for app %q: %w", desiredEntry.TriggerKey, appName, err)
		}
		providerCtx := invocation.WithWorkflowContextString(ctx, "app", appName)
		existingProto, err := provider.GetEventTrigger(providerCtx, &proto.GetWorkflowProviderEventTriggerRequest{TriggerId: desiredEntry.TriggerID})
		switch {
		case err == nil:
			existing, existingErr := workflowwire.EventTriggerFromProto(existingProto)
			if existingErr != nil {
				return fmt.Errorf("bootstrap: decode workflow event trigger %q for app %q: %w", desiredEntry.TriggerID, appName, existingErr)
			}
			if !isWorkflowConfigOwnedEventTrigger(existing, appName, desiredEntry.TriggerID) {
				return fmt.Errorf("bootstrap: workflow event trigger %q for app %q conflicts with existing unmanaged trigger id %q", desiredEntry.TriggerKey, appName, desiredEntry.TriggerID)
			}
		case isWorkflowObjectNotFound(err):
		default:
			return fmt.Errorf("bootstrap: get workflow event trigger %q for app %q: %w", desiredEntry.TriggerID, appName, err)
		}
		runAs := trigger.RunAs.SubjectRef()
		if err := workflowConfigValidateExecutionTarget(cfg, target, runAs); err != nil {
			return fmt.Errorf("bootstrap: workflow event trigger %q for app %q: %w", desiredEntry.TriggerKey, appName, err)
		}
		targetProto, err := workflowwire.TargetToProto(target)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow event trigger %q for app %q: %w", desiredEntry.TriggerKey, appName, err)
		}
		if _, err := provider.UpsertEventTrigger(providerCtx, &proto.UpsertWorkflowProviderEventTriggerRequest{
			TriggerId:   desiredEntry.TriggerID,
			Match:       workflowwire.EventMatchToProto(workflowConfigEventTriggerMatch(trigger)),
			Target:      targetProto,
			Paused:      trigger.Paused,
			RequestedBySubjectId: workflowConfigOwnerSubjectID(),
			RunAs:       agentwire.RunAsSubjectToProto(runAs),
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
		_, provider, err := runtime.ResolveProvider(ctx, providerName)
		if err != nil {
			return fmt.Errorf("bootstrap: cleanup workflow event triggers requires provider %q: %w", providerName, err)
		}
		resp, err := provider.ListEventTriggers(ctx, &proto.ListWorkflowProviderEventTriggersRequest{})
		if err != nil {
			workflowLogSkippedConfigWorkflowCleanup(ctx, "event_triggers", providerName, err)
			continue
		}
		for _, triggerProto := range resp.GetTriggers() {
			trigger, err := workflowwire.EventTriggerFromProto(triggerProto)
			if err != nil {
				return fmt.Errorf("bootstrap: decode workflow event trigger from provider %q: %w", providerName, err)
			}
			if trigger == nil || !isWorkflowConfigOwnedEventTrigger(trigger, workflowConfigTargetLabel(trigger.Target), trigger.ID) {
				continue
			}
			if _, ok := desiredByProviderTrigger[workflowConfigProviderObjectKey(providerName, trigger.ID)]; ok {
				continue
			}
			appName := workflowConfigTargetLabel(trigger.Target)
			providerCtx := invocation.WithWorkflowContextString(ctx, "app", appName)
			if err := provider.DeleteEventTrigger(providerCtx, &proto.DeleteWorkflowProviderEventTriggerRequest{TriggerId: trigger.ID}); err != nil && !isWorkflowObjectNotFound(err) {
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
	return existing.ID == triggerID &&
		workflowConfigTargetLabel(existing.Target) == appName &&
		existing.CreatedBySubjectID == workflowConfigOwnerSubjectID()
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
