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
	"github.com/valon-technologies/gestalt/server/core"
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
		target := workflowConfigExecutionRefTarget(workflowConfigTarget(trigger.Target))
		pluginName := workflowConfigTargetLabel(target)
		providerName, provider, err := runtime.ResolveProviderSelection(trigger.Provider)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow event trigger %q for plugin %q: %w", desiredEntry.TriggerKey, pluginName, err)
		}
		deploymentProvider, ok := provider.(coreworkflow.DeploymentProvider)
		if !ok {
			return fmt.Errorf("bootstrap: workflow event trigger %q for plugin %q: workflow provider %q does not support deployments", desiredEntry.TriggerKey, pluginName, providerName)
		}
		spec := workflowConfigEventTriggerDeploymentSpec(desiredEntry, target, trigger)
		existingExecutionRef := ""
		providerCtx := invocation.WithWorkflowContextString(ctx, "plugin", pluginName)
		existing, err := deploymentProvider.GetWorkflowDeployment(providerCtx, coreworkflow.GetDeploymentRequest{
			DeploymentID: desiredEntry.TriggerID,
		})
		switch {
		case err == nil:
			if !isWorkflowConfigOwnedEventDeployment(existing, pluginName, desiredEntry) {
				return fmt.Errorf("bootstrap: workflow event trigger %q for plugin %q conflicts with existing unmanaged trigger id %q", desiredEntry.TriggerKey, pluginName, desiredEntry.TriggerID)
			}
		case isWorkflowObjectNotFound(err):
			existing = nil
		default:
			return fmt.Errorf("bootstrap: get workflow deployment %q for plugin %q: %w", desiredEntry.TriggerID, pluginName, err)
		}
		if existing != nil && existing.Binding != nil {
			existingExecutionRef = strings.TrimSpace(existing.Binding.ExecutionRef)
		}
		runAs, err := workflowConfigRunAsSubject("workflows.eventTriggers."+desiredEntry.TriggerKey+".runAs", trigger.RunAs)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow event trigger %q for plugin %q: %w", desiredEntry.TriggerKey, pluginName, err)
		}
		permissions, err := workflowConfigExecutionPermissions(cfg, "workflows.eventTriggers."+desiredEntry.TriggerKey, trigger.Invokes, trigger.Permissions)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow event trigger %q for plugin %q: %w", desiredEntry.TriggerKey, pluginName, err)
		}
		spec.RunAs = runAs
		spec.Permissions = append([]core.AccessPermission(nil), permissions...)
		desiredExecutionRef, err := workflowConfigExecutionReference(cfg, providerName, target, runAs, permissions)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow event trigger %q for plugin %q: %w", desiredEntry.TriggerKey, pluginName, err)
		}
		targetDigest, err := coreworkflow.TargetFingerprint(target)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow event trigger %q for plugin %q target digest: %w", desiredEntry.TriggerKey, pluginName, err)
		}
		actionTableDigest, err := coreworkflow.TargetActionTableDigest(target)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow event trigger %q for plugin %q action table digest: %w", desiredEntry.TriggerKey, pluginName, err)
		}
		specDigest, err := coreworkflow.DeploymentSpecDigest(spec)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow event trigger %q for plugin %q spec digest: %w", desiredEntry.TriggerKey, pluginName, err)
		}
		plan, err := deploymentProvider.PlanWorkflow(providerCtx, coreworkflow.PlanWorkflowRequest{
			Spec:                          spec,
			SpecDigest:                    specDigest,
			TargetDigest:                  targetDigest,
			ActionTableDigest:             actionTableDigest,
			TargetCanonicalizationVersion: workflowConfigTargetCanonicalizationV1,
			WorkflowSemanticsVersion:      workflowConfigSemanticsVersionSteps,
		})
		if err != nil {
			return fmt.Errorf("bootstrap: plan workflow event trigger %q for plugin %q: %w", desiredEntry.TriggerKey, pluginName, err)
		}
		if plan == nil || strings.TrimSpace(plan.ProviderPlanDigest) == "" {
			return fmt.Errorf("bootstrap: workflow event trigger %q for plugin %q provider returned empty plan digest", desiredEntry.TriggerKey, pluginName)
		}
		desiredExecutionRef.TargetDigest = targetDigest
		desiredExecutionRef.ProviderPlanDigest = strings.TrimSpace(plan.ProviderPlanDigest)
		desiredExecutionRef.SemanticsVersion = workflowConfigSemanticsVersionSteps
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
			return fmt.Errorf("bootstrap: store workflow execution ref for event trigger %q on plugin %q: %w", desiredEntry.TriggerKey, pluginName, err)
		}
		binding := &coreworkflow.DeploymentBinding{
			ID:                     workflowConfigSHA256(strings.Join([]string{"config-binding", providerName, desiredEntry.TriggerID, executionRefID, strings.TrimSpace(plan.ProviderPlanDigest)}, "\x00")),
			ExecutionRef:           executionRefID,
			ExecutionRefGeneration: desiredExecutionRef.Generation,
			ExecutionRefSeal:       strings.TrimSpace(desiredExecutionRef.Seal),
			DeploymentID:           desiredEntry.TriggerID,
			DeploymentGeneration:   spec.Generation,
			SpecDigest:             specDigest,
			TargetDigest:           targetDigest,
			ActionTableDigest:      actionTableDigest,
			ProviderPlanID:         strings.TrimSpace(plan.ProviderPlanID),
			ProviderPlanDigest:     strings.TrimSpace(plan.ProviderPlanDigest),
			SemanticsVersion:       workflowConfigSemanticsVersionSteps,
			IdempotencyKey:         desiredEntry.TriggerID,
		}
		if _, err := deploymentProvider.ApplyWorkflowDeployment(providerCtx, coreworkflow.ApplyDeploymentRequest{
			Spec:      spec,
			Plan:      plan,
			Binding:   binding,
			RequestID: desiredEntry.TriggerID,
		}); err != nil {
			if createdExecutionRef {
				_ = workflowRevokeExecutionRefByID(ctx, executionRefs, executionRefID)
			}
			return fmt.Errorf("bootstrap: workflow deployment %q for plugin %q: %w", desiredEntry.TriggerKey, pluginName, err)
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
		deploymentProvider, ok := provider.(coreworkflow.DeploymentProvider)
		if !ok {
			workflowLogSkippedConfigWorkflowCleanup(ctx, "deployments", providerName, fmt.Errorf("workflow provider does not support deployments"))
			continue
		}
		deployments, err := deploymentProvider.ListWorkflowDeployments(ctx, coreworkflow.ListDeploymentsRequest{
			Labels: map[string]string{workflowConfigDeploymentLabelKind: workflowConfigDeploymentKindEventTrigger},
		})
		if err != nil {
			workflowLogSkippedConfigWorkflowCleanup(ctx, "deployments", providerName, err)
			continue
		}
		var executionRefs coreworkflow.ExecutionReferenceStore
		for _, deployment := range deployments.Deployments {
			if deployment == nil || !isWorkflowConfigOwnedEventDeployment(deployment, workflowConfigTargetLabel(deployment.Spec.Target), desiredWorkflowConfigEventTrigger{TriggerKey: deployment.Spec.Labels[workflowConfigDeploymentLabelTriggerKey], TriggerID: deployment.Spec.ID}) {
				continue
			}
			if _, ok := desiredByProviderTrigger[workflowConfigProviderObjectKey(providerName, deployment.Spec.ID)]; ok {
				continue
			}
			pluginName := workflowConfigTargetLabel(deployment.Spec.Target)
			providerCtx := invocation.WithWorkflowContextString(ctx, "plugin", pluginName)
			if err := deploymentProvider.DeleteWorkflowDeployment(providerCtx, coreworkflow.DeleteDeploymentRequest{DeploymentID: deployment.Spec.ID, Generation: deployment.Spec.Generation}); err != nil && !isWorkflowObjectNotFound(err) {
				return fmt.Errorf("bootstrap: delete workflow deployment %q for plugin %q: %w", deployment.Spec.ID, pluginName, err)
			}
			if executionRefs == nil {
				executionRefs, err = workflowExecutionReferenceStore(providerName, provider)
				if err != nil {
					return fmt.Errorf("bootstrap: cleanup workflow event triggers for provider %q: %w", providerName, err)
				}
			}
			executionRefID := ""
			if deployment.Binding != nil {
				executionRefID = strings.TrimSpace(deployment.Binding.ExecutionRef)
			}
			if err := workflowRevokeExecutionRefByID(ctx, executionRefs, executionRefID); err != nil {
				return fmt.Errorf("bootstrap: revoke workflow execution ref %q for deployment %q on plugin %q: %w", executionRefID, deployment.Spec.ID, pluginName, err)
			}
		}
	}
	return nil
}

func workflowConfigEventTriggerDeploymentSpec(entry desiredWorkflowConfigEventTrigger, target coreworkflow.Target, trigger config.WorkflowEventTriggerConfig) coreworkflow.DeploymentSpec {
	pluginName := workflowConfigTargetLabel(target)
	return coreworkflow.DeploymentSpec{
		ID:                       entry.TriggerID,
		Generation:               1,
		Target:                   target,
		Paused:                   trigger.Paused,
		WorkflowSemanticsVersion: workflowConfigSemanticsVersionSteps,
		Labels: map[string]string{
			workflowConfigDeploymentLabelKind:       workflowConfigDeploymentKindEventTrigger,
			workflowConfigDeploymentLabelTriggerKey: strings.TrimSpace(entry.TriggerKey),
			workflowConfigDeploymentLabelPlugin:     pluginName,
		},
		Activations: []coreworkflow.Activation{{
			ID:     "event",
			Paused: trigger.Paused,
			Mode:   coreworkflow.ActivationModeSignalOrStart,
			Event: &coreworkflow.EventActivation{
				Match: workflowConfigEventTriggerMatch(trigger),
			},
		}},
	}
}

func isWorkflowConfigOwnedEventDeployment(existing *coreworkflow.Deployment, pluginName string, desired desiredWorkflowConfigEventTrigger) bool {
	if existing == nil {
		return false
	}
	labels := existing.Spec.Labels
	if strings.TrimSpace(existing.Spec.ID) != strings.TrimSpace(desired.TriggerID) ||
		strings.TrimSpace(labels[workflowConfigDeploymentLabelKind]) != workflowConfigDeploymentKindEventTrigger ||
		strings.TrimSpace(labels[workflowConfigDeploymentLabelPlugin]) != strings.TrimSpace(pluginName) {
		return false
	}
	if triggerKey := strings.TrimSpace(labels[workflowConfigDeploymentLabelTriggerKey]); triggerKey != "" && triggerKey != strings.TrimSpace(desired.TriggerKey) {
		return false
	}
	return true
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
