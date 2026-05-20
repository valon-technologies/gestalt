package bootstrap

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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

type workflowConfigProviderFilter func(providerName string) bool

const (
	workflowConfigDeploymentLabelKind        = "gestalt.config.kind"
	workflowConfigDeploymentLabelScheduleKey = "gestalt.config.schedule_key"
	workflowConfigDeploymentLabelTriggerKey  = "gestalt.config.trigger_key"
	workflowConfigDeploymentLabelPlugin      = "gestalt.config.plugin"
	workflowConfigDeploymentKindSchedule     = "schedule"
	workflowConfigDeploymentKindEventTrigger = "event_trigger"
	workflowConfigSemanticsVersionSteps      = "workflow_steps_v1"
	workflowConfigTargetCanonicalizationV1   = "target_fingerprint_v1"
)

func reconcileWorkflowConfigSchedules(ctx context.Context, cfg *config.Config, runtime *workflowRuntime, includeProvider workflowConfigProviderFilter) error {
	if cfg == nil || runtime == nil {
		return nil
	}
	desired, err := desiredWorkflowConfigSchedules(cfg)
	if err != nil {
		return err
	}

	for _, rowID := range slices.Sorted(maps.Keys(desired)) {
		desiredEntry := desired[rowID]
		if !workflowConfigProviderIncluded(includeProvider, desiredEntry.ProviderName) {
			continue
		}
		schedule := desiredEntry.schedule
		target := workflowConfigExecutionRefTarget(workflowConfigTarget(schedule.Target))
		pluginName := workflowConfigTargetLabel(target)
		providerName, provider, err := runtime.ResolveProviderSelection(schedule.Provider)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow schedule %q for plugin %q: %w", desiredEntry.ScheduleKey, pluginName, err)
		}
		deploymentProvider, ok := provider.(coreworkflow.DeploymentProvider)
		if !ok {
			return fmt.Errorf("bootstrap: workflow schedule %q for plugin %q: workflow provider %q does not support deployments", desiredEntry.ScheduleKey, pluginName, providerName)
		}
		spec := workflowConfigScheduleDeploymentSpec(desiredEntry, target, schedule)
		existingExecutionRef := ""
		providerCtx := invocation.WithWorkflowContextString(ctx, "plugin", pluginName)
		existing, err := deploymentProvider.GetWorkflowDeployment(providerCtx, coreworkflow.GetDeploymentRequest{
			DeploymentID: desiredEntry.ScheduleID,
		})
		switch {
		case err == nil:
			if !isWorkflowConfigOwnedDeployment(existing, pluginName, desiredEntry) {
				return fmt.Errorf("bootstrap: workflow schedule %q for plugin %q conflicts with existing unmanaged schedule id %q", desiredEntry.ScheduleKey, pluginName, desiredEntry.ScheduleID)
			}
		case isWorkflowObjectNotFound(err):
			existing = nil
		default:
			return fmt.Errorf("bootstrap: get workflow deployment %q for plugin %q: %w", desiredEntry.ScheduleID, pluginName, err)
		}
		if existing != nil && existing.Binding != nil {
			existingExecutionRef = strings.TrimSpace(existing.Binding.ExecutionRef)
		}
		runAs, err := workflowConfigRunAsSubject("workflows.schedules."+desiredEntry.ScheduleKey+".runAs", schedule.RunAs)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow schedule %q for plugin %q: %w", desiredEntry.ScheduleKey, pluginName, err)
		}
		permissions, err := workflowConfigExecutionPermissions(cfg, "workflows.schedules."+desiredEntry.ScheduleKey, schedule.Invokes, schedule.Permissions)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow schedule %q for plugin %q: %w", desiredEntry.ScheduleKey, pluginName, err)
		}
		spec.RunAs = runAs
		spec.Permissions = append([]core.AccessPermission(nil), permissions...)
		desiredExecutionRef, err := workflowConfigExecutionReference(cfg, providerName, target, runAs, permissions)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow schedule %q for plugin %q: %w", desiredEntry.ScheduleKey, pluginName, err)
		}
		targetDigest, err := coreworkflow.TargetFingerprint(target)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow schedule %q for plugin %q target digest: %w", desiredEntry.ScheduleKey, pluginName, err)
		}
		actionTableDigest, err := coreworkflow.TargetActionTableDigest(target)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow schedule %q for plugin %q action table digest: %w", desiredEntry.ScheduleKey, pluginName, err)
		}
		specDigest, err := coreworkflow.DeploymentSpecDigest(spec)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow schedule %q for plugin %q spec digest: %w", desiredEntry.ScheduleKey, pluginName, err)
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
			return fmt.Errorf("bootstrap: plan workflow schedule %q for plugin %q: %w", desiredEntry.ScheduleKey, pluginName, err)
		}
		if plan == nil || strings.TrimSpace(plan.ProviderPlanDigest) == "" {
			return fmt.Errorf("bootstrap: workflow schedule %q for plugin %q provider returned empty plan digest", desiredEntry.ScheduleKey, pluginName)
		}
		desiredExecutionRef.TargetDigest = targetDigest
		desiredExecutionRef.ProviderPlanDigest = strings.TrimSpace(plan.ProviderPlanDigest)
		desiredExecutionRef.SemanticsVersion = workflowConfigSemanticsVersionSteps
		executionRefs, err := workflowExecutionReferenceStore(providerName, provider)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow schedule %q for plugin %q: %w", desiredEntry.ScheduleKey, pluginName, err)
		}
		executionRefID, createdExecutionRef, replacedUnreadableExecutionRef, replacedUnreadableExecutionRefErr, err := workflowEnsureConfigExecutionRef(
			ctx,
			executionRefs,
			desiredExecutionRef,
			workflowConfigScheduleExecutionRefID(desiredEntry.ScheduleID),
			existingExecutionRef,
		)
		if err != nil {
			return fmt.Errorf("bootstrap: store workflow execution ref for schedule %q on plugin %q: %w", desiredEntry.ScheduleKey, pluginName, err)
		}
		binding := &coreworkflow.DeploymentBinding{
			ID:                     workflowConfigSHA256(strings.Join([]string{"config-binding", providerName, desiredEntry.ScheduleID, executionRefID, strings.TrimSpace(plan.ProviderPlanDigest)}, "\x00")),
			ExecutionRef:           executionRefID,
			ExecutionRefGeneration: desiredExecutionRef.Generation,
			ExecutionRefSeal:       strings.TrimSpace(desiredExecutionRef.Seal),
			DeploymentID:           desiredEntry.ScheduleID,
			DeploymentGeneration:   spec.Generation,
			SpecDigest:             specDigest,
			TargetDigest:           targetDigest,
			ActionTableDigest:      actionTableDigest,
			ProviderPlanID:         strings.TrimSpace(plan.ProviderPlanID),
			ProviderPlanDigest:     strings.TrimSpace(plan.ProviderPlanDigest),
			SemanticsVersion:       workflowConfigSemanticsVersionSteps,
			IdempotencyKey:         desiredEntry.ScheduleID,
		}
		if _, err := deploymentProvider.ApplyWorkflowDeployment(providerCtx, coreworkflow.ApplyDeploymentRequest{
			Spec:      spec,
			Plan:      plan,
			Binding:   binding,
			RequestID: desiredEntry.ScheduleID,
		}); err != nil {
			if createdExecutionRef {
				_ = workflowRevokeExecutionRefByID(ctx, executionRefs, executionRefID)
			}
			return fmt.Errorf("bootstrap: workflow deployment %q for plugin %q: %w", desiredEntry.ScheduleKey, pluginName, err)
		}
		if replacedUnreadableExecutionRef != "" {
			workflowLogReplacedUnreadableExecutionRef(ctx, "schedule", desiredEntry.ScheduleKey, desiredEntry.ScheduleID, providerName, pluginName, replacedUnreadableExecutionRef, executionRefID, replacedUnreadableExecutionRefErr)
		}
		if existingExecutionRef != executionRefID && replacedUnreadableExecutionRef == "" {
			if err := workflowRevokeExecutionRefByID(ctx, executionRefs, existingExecutionRef); err != nil {
				return fmt.Errorf("bootstrap: revoke workflow execution ref %q for schedule %q on plugin %q: %w", existingExecutionRef, desiredEntry.ScheduleID, pluginName, err)
			}
		}
	}

	if err := cleanupRemovedWorkflowConfigSchedules(ctx, runtime, desired, includeProvider); err != nil {
		return err
	}
	return nil
}

func workflowConfigProviderIncluded(includeProvider workflowConfigProviderFilter, providerName string) bool {
	if includeProvider == nil {
		return true
	}
	return includeProvider(strings.TrimSpace(providerName))
}

func workflowConfigScheduleDeploymentSpec(entry desiredWorkflowConfigSchedule, target coreworkflow.Target, schedule config.WorkflowScheduleConfig) coreworkflow.DeploymentSpec {
	pluginName := workflowConfigTargetLabel(target)
	return coreworkflow.DeploymentSpec{
		ID:                       entry.ScheduleID,
		Generation:               1,
		Target:                   target,
		Paused:                   schedule.Paused,
		WorkflowSemanticsVersion: workflowConfigSemanticsVersionSteps,
		Labels: map[string]string{
			workflowConfigDeploymentLabelKind:        workflowConfigDeploymentKindSchedule,
			workflowConfigDeploymentLabelScheduleKey: strings.TrimSpace(entry.ScheduleKey),
			workflowConfigDeploymentLabelPlugin:      pluginName,
		},
		Activations: []coreworkflow.Activation{{
			ID:     "schedule",
			Paused: schedule.Paused,
			Mode:   coreworkflow.ActivationModeStart,
			Schedule: &coreworkflow.ScheduleActivation{
				Cron:     strings.TrimSpace(schedule.Cron),
				Timezone: strings.TrimSpace(schedule.Timezone),
			},
		}},
	}
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

func cleanupRemovedWorkflowConfigSchedules(ctx context.Context, runtime *workflowRuntime, desired map[string]desiredWorkflowConfigSchedule, includeProvider workflowConfigProviderFilter) error {
	desiredByProviderSchedule := make(map[string]struct{}, len(desired))
	for rowID := range desired {
		entry := desired[rowID]
		if !workflowConfigProviderIncluded(includeProvider, entry.ProviderName) {
			continue
		}
		desiredByProviderSchedule[workflowConfigProviderObjectKey(entry.ProviderName, entry.ScheduleID)] = struct{}{}
	}
	for _, providerName := range runtime.ProviderNames() {
		if !workflowConfigProviderIncluded(includeProvider, providerName) {
			continue
		}
		provider, err := runtime.ResolveProvider(providerName)
		if err != nil {
			return fmt.Errorf("bootstrap: cleanup workflow schedules requires provider %q: %w", providerName, err)
		}
		deploymentProvider, ok := provider.(coreworkflow.DeploymentProvider)
		if !ok {
			workflowLogSkippedConfigWorkflowCleanup(ctx, "deployments", providerName, fmt.Errorf("workflow provider does not support deployments"))
			continue
		}
		deployments, err := deploymentProvider.ListWorkflowDeployments(ctx, coreworkflow.ListDeploymentsRequest{
			Labels: map[string]string{workflowConfigDeploymentLabelKind: workflowConfigDeploymentKindSchedule},
		})
		if err != nil {
			workflowLogSkippedConfigWorkflowCleanup(ctx, "deployments", providerName, err)
			continue
		}
		var executionRefs coreworkflow.ExecutionReferenceStore
		for _, deployment := range deployments.Deployments {
			if deployment == nil || !isWorkflowConfigOwnedDeployment(deployment, workflowConfigTargetLabel(deployment.Spec.Target), desiredWorkflowConfigSchedule{ScheduleKey: deployment.Spec.Labels[workflowConfigDeploymentLabelScheduleKey], ScheduleID: deployment.Spec.ID}) {
				continue
			}
			if _, ok := desiredByProviderSchedule[workflowConfigProviderObjectKey(providerName, deployment.Spec.ID)]; ok {
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
					return fmt.Errorf("bootstrap: cleanup workflow schedules for provider %q: %w", providerName, err)
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

func workflowConfigProviderObjectKey(providerName, objectID string) string {
	return strings.TrimSpace(providerName) + "\x00" + strings.TrimSpace(objectID)
}

func workflowLogSkippedConfigWorkflowCleanup(ctx context.Context, objectType, providerName string, err error) {
	slog.WarnContext(ctx, "skipping config-managed workflow cleanup after provider list failed",
		"object_type", objectType,
		"provider", providerName,
		"error", err,
	)
}

func isWorkflowConfigOwnedDeployment(existing *coreworkflow.Deployment, pluginName string, desired desiredWorkflowConfigSchedule) bool {
	if existing == nil {
		return false
	}
	labels := existing.Spec.Labels
	if strings.TrimSpace(existing.Spec.ID) != strings.TrimSpace(desired.ScheduleID) ||
		strings.TrimSpace(labels[workflowConfigDeploymentLabelKind]) != workflowConfigDeploymentKindSchedule ||
		strings.TrimSpace(labels[workflowConfigDeploymentLabelPlugin]) != strings.TrimSpace(pluginName) {
		return false
	}
	if scheduleKey := strings.TrimSpace(labels[workflowConfigDeploymentLabelScheduleKey]); scheduleKey != "" && scheduleKey != strings.TrimSpace(desired.ScheduleKey) {
		return false
	}
	return true
}

func workflowConfigTargetLabel(target coreworkflow.Target) string {
	for i := range target.Steps {
		step := target.Steps[i]
		if step.Plugin != nil {
			return strings.TrimSpace(step.Plugin.Name)
		}
		if step.Agent != nil {
			providerName := strings.TrimSpace(step.Agent.ProviderName)
			if providerName == "" {
				providerName = "default"
			}
			return "agent:" + providerName
		}
	}
	for i := range target.Steps {
		step := target.Steps[i]
		if step.Plugin != nil && strings.TrimSpace(step.Plugin.Name) != "" {
			return strings.TrimSpace(step.Plugin.Name)
		}
		if step.Agent != nil {
			providerName := strings.TrimSpace(step.Agent.ProviderName)
			if providerName == "" {
				providerName = "default"
			}
			return "agent:" + providerName
		}
	}
	return ""
}

func workflowConfigTarget(target *config.WorkflowTargetConfig) coreworkflow.Target {
	if target == nil {
		return coreworkflow.Target{}
	}
	return coreworkflow.Target{Steps: workflowConfigSteps(target.Steps)}
}

func workflowConfigSteps(steps []config.WorkflowStepConfig) []coreworkflow.Step {
	if len(steps) == 0 {
		return nil
	}
	out := make([]coreworkflow.Step, 0, len(steps))
	for i := range steps {
		step := &steps[i]
		timeoutSeconds := 0
		if timeout := strings.TrimSpace(step.Timeout); timeout != "" {
			if parsed, err := time.ParseDuration(timeout); err == nil {
				timeoutSeconds = int(parsed.Seconds())
			}
		}
		outStep := coreworkflow.Step{
			ID:             strings.TrimSpace(step.ID),
			Inputs:         workflowConfigValueMap(step.Inputs),
			Plugin:         workflowConfigPluginCall(step.Plugin),
			Agent:          workflowConfigAgentTurn(step.Agent),
			OutputDelivery: workflowConfigStepDelivery(step.OutputDelivery),
			Metadata:       maps.Clone(step.Metadata),
			TimeoutSeconds: timeoutSeconds,
			When:           workflowConfigStepWhen(step.When),
		}
		out = append(out, outStep)
	}
	return out
}

func workflowConfigPluginCall(plugin *config.WorkflowStepPluginCallConfig) *coreworkflow.PluginCall {
	if plugin == nil {
		return nil
	}
	return &coreworkflow.PluginCall{
		Name:           strings.TrimSpace(plugin.Name),
		Operation:      strings.TrimSpace(plugin.Operation),
		Connection:     strings.TrimSpace(plugin.Connection),
		Instance:       strings.TrimSpace(plugin.Instance),
		CredentialMode: core.NormalizeOptionalConnectionMode(core.ConnectionMode(plugin.CredentialMode)),
		Input:          workflowConfigValue(plugin.Input),
	}
}

func workflowConfigAgentTurn(agent *config.WorkflowStepAgentConfig) *coreworkflow.AgentTurn {
	if agent == nil {
		return nil
	}
	messages := make([]coreworkflow.AgentMessage, 0, len(agent.Messages))
	for _, message := range agent.Messages {
		messages = append(messages, coreworkflow.AgentMessage{
			Role:     strings.TrimSpace(message.Role),
			Text:     coreworkflow.Text{Template: strings.TrimSpace(message.Text.Template)},
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
	return &coreworkflow.AgentTurn{
		ProviderName:   strings.TrimSpace(agent.Provider),
		Model:          strings.TrimSpace(agent.Model),
		SessionKey:     strings.TrimSpace(agent.SessionKey),
		Prompt:         coreworkflow.Text{Template: strings.TrimSpace(agent.Prompt.Template)},
		Messages:       messages,
		ToolRefs:       tools,
		ResponseSchema: maps.Clone(agent.ResponseSchema),
		ModelOptions:   maps.Clone(agent.ModelOptions),
	}
}

func workflowConfigStepWhen(when *config.WorkflowStepWhenConfig) *coreworkflow.StepWhen {
	if when == nil {
		return nil
	}
	return &coreworkflow.StepWhen{
		Value:     workflowConfigValue(when.Value),
		Equals:    when.Equals,
		EqualsSet: true,
	}
}

func workflowConfigStepDelivery(delivery *config.WorkflowStepDeliveryConfig) *coreworkflow.StepDelivery {
	if delivery == nil {
		return nil
	}
	return &coreworkflow.StepDelivery{Plugin: workflowConfigPluginCall(delivery.Plugin)}
}

func workflowConfigValueMap(values map[string]config.WorkflowValueConfig) map[string]coreworkflow.Value {
	if values == nil {
		return nil
	}
	out := make(map[string]coreworkflow.Value, len(values))
	for key := range values {
		out[key] = workflowConfigValue(values[key])
	}
	return out
}

func workflowConfigValue(value config.WorkflowValueConfig) coreworkflow.Value {
	out := coreworkflow.Value{
		Literal:       value.Literal,
		LiteralSet:    value.LiteralSet,
		Object:        workflowConfigValueMap(value.Object),
		Array:         workflowConfigValueArray(value.Array),
		RunInput:      strings.TrimSpace(value.RunInput),
		SignalPayload: strings.TrimSpace(value.SignalPayload),
	}
	if value.Template != nil {
		out.Template = &coreworkflow.Text{Template: strings.TrimSpace(value.Template.Template)}
	}
	if value.StepOutput != nil {
		out.StepOutput = &coreworkflow.StepOutputSource{
			StepID: strings.TrimSpace(value.StepOutput.StepID),
			Path:   strings.TrimSpace(value.StepOutput.Path),
		}
	}
	return out
}

func workflowConfigValueArray(values []config.WorkflowValueConfig) []coreworkflow.Value {
	if values == nil {
		return nil
	}
	out := make([]coreworkflow.Value, 0, len(values))
	for i := range values {
		out = append(out, workflowConfigValue(values[i]))
	}
	return out
}

func workflowConfigRunAsSubject(path string, runAs *config.WorkflowRunAsConfig) (*core.RunAsSubject, error) {
	if runAs == nil {
		return nil, nil
	}
	subject := runAs.SubjectRef()
	if subject == nil {
		return nil, fmt.Errorf("config validation: %s.subject is required", path)
	}
	kind, _, ok := core.ParseSubjectID(subject.SubjectID)
	if !ok {
		return nil, fmt.Errorf("config validation: %s.subject.id %q must be a fully-qualified service_account subject", path, subject.SubjectID)
	}
	if kind != "service_account" {
		return nil, fmt.Errorf("config validation: %s.subject.id %q must identify a service_account subject", path, subject.SubjectID)
	}
	if subject.SubjectKind != kind {
		return nil, fmt.Errorf("config validation: %s.subject.kind %q must match subject.id kind %q", path, subject.SubjectKind, kind)
	}
	if subject.AuthSource == "" {
		subject.AuthSource = "config"
	}
	return subject, nil
}

func workflowConfigExecutionPermissions(cfg *config.Config, path string, invokes []config.WorkflowInvokeConfig, permissions []core.AccessPermission) ([]core.AccessPermission, error) {
	if len(invokes) > 0 && len(permissions) > 0 {
		return nil, fmt.Errorf("config validation: %s must not set both invokes and permissions", path)
	}
	if len(invokes) == 0 {
		return permissions, nil
	}
	out := make([]core.AccessPermission, 0, len(invokes))
	pluginIndexes := make(map[string]int, len(invokes))
	seenOperations := make(map[string]map[string]struct{}, len(invokes))
	for i, invoke := range invokes {
		plugin := strings.TrimSpace(invoke.Plugin)
		if plugin == "" {
			return nil, fmt.Errorf("config validation: %s.invokes[%d].plugin is required", path, i)
		}
		if cfg == nil || cfg.Plugins[plugin] == nil {
			return nil, fmt.Errorf("config validation: %s.invokes[%d].plugin references unknown plugin %q", path, i, plugin)
		}
		operation := strings.TrimSpace(invoke.Operation)
		if operation == "" {
			return nil, fmt.Errorf("config validation: %s.invokes[%d].operation is required", path, i)
		}
		if seenOperations[plugin] == nil {
			seenOperations[plugin] = map[string]struct{}{}
		}
		if _, exists := seenOperations[plugin][operation]; exists {
			continue
		}
		seenOperations[plugin][operation] = struct{}{}
		idx, ok := pluginIndexes[plugin]
		if !ok {
			idx = len(out)
			pluginIndexes[plugin] = idx
			out = append(out, core.AccessPermission{Plugin: plugin})
		}
		out[idx].Operations = append(out[idx].Operations, operation)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
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
) (string, bool, string, error, error) {
	if store == nil {
		return "", false, "", nil, fmt.Errorf("workflow execution refs are not configured")
	}
	refID = strings.TrimSpace(refID)
	if refID == "" {
		return "", false, "", nil, fmt.Errorf("workflow execution ref id is required")
	}
	replacedUnreadableCandidateID := ""
	var replacedUnreadableCandidateErr error
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
			if replacedUnreadableCandidateID == "" {
				replacedUnreadableCandidateID = candidateID
				replacedUnreadableCandidateErr = err
			}
			continue
		}
		desiredForCandidate := *desired
		desiredForCandidate.ID = candidateID
		if err := workflowConfigFinalizeExecutionRef(&desiredForCandidate); err != nil {
			return "", false, "", nil, err
		}
		if workflowConfigExecutionRefMatches(existing, &desiredForCandidate) {
			return candidateID, false, "", nil, nil
		}
	}
	desired.ID = refID
	if err := workflowConfigFinalizeExecutionRef(desired); err != nil {
		return "", false, "", nil, err
	}
	stored, err := store.PutExecutionReference(ctx, desired)
	if err != nil {
		return "", false, "", nil, err
	}
	if desired.RunAs != nil && !workflowConfigExecutionRefMatches(stored, desired) {
		verified, err := store.GetExecutionReference(ctx, refID)
		if err != nil {
			return "", false, "", nil, fmt.Errorf("workflow execution ref %q was written but could not be verified: %w", refID, err)
		}
		if !workflowConfigExecutionRefMatches(verified, desired) {
			return "", false, "", nil, workflowConfigExecutionRefPersistenceError(refID, verified, desired)
		}
	}
	return refID, true, replacedUnreadableCandidateID, replacedUnreadableCandidateErr, nil
}

func workflowConfigFinalizeExecutionRef(ref *coreworkflow.ExecutionReference) error {
	if ref == nil {
		return nil
	}
	if strings.TrimSpace(ref.TargetDigest) == "" {
		digest, err := coreworkflow.TargetFingerprint(ref.Target)
		if err != nil {
			return err
		}
		ref.TargetDigest = digest
	}
	if strings.TrimSpace(ref.ProviderPlanDigest) == "" {
		ref.ProviderPlanDigest = workflowConfigSHA256(strings.Join([]string{
			"config-managed",
			strings.TrimSpace(ref.ProviderName),
			strings.TrimSpace(ref.TargetDigest),
		}, "\x00"))
	}
	if ref.Generation == 0 {
		ref.Generation = 1
	}
	ref.PermissionsDigest = workflowConfigPermissionsDigest(ref.Permissions)
	if strings.TrimSpace(ref.Seal) == "" {
		ref.Seal = workflowConfigExecutionRefSeal(ref)
	}
	return nil
}

func workflowConfigPermissionsDigest(permissions []core.AccessPermission) string {
	data, err := json.Marshal(permissions)
	if err != nil {
		return ""
	}
	return workflowConfigSHA256(string(data))
}

func workflowConfigExecutionRefSeal(ref *coreworkflow.ExecutionReference) string {
	if ref == nil {
		return ""
	}
	return workflowConfigSHA256(strings.Join([]string{
		strings.TrimSpace(ref.ID),
		strings.TrimSpace(ref.ProviderName),
		strings.TrimSpace(ref.SubjectID),
		strings.TrimSpace(ref.CredentialSubjectID),
		strings.TrimSpace(ref.CallerPluginName),
		strings.TrimSpace(ref.TargetDigest),
		strings.TrimSpace(ref.ProviderPlanDigest),
		strings.TrimSpace(ref.PermissionsDigest),
		strings.TrimSpace(ref.SemanticsVersion),
		fmt.Sprintf("%d", ref.Generation),
	}, "\x00"))
}

func workflowConfigSHA256(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func workflowExecutionRefPermissionsForTarget(target coreworkflow.Target, explicit ...[]core.AccessPermission) []core.AccessPermission {
	var base []core.AccessPermission
	if len(target.Steps) > 0 {
		for i := range target.Steps {
			step := target.Steps[i]
			if step.Plugin != nil {
				pluginName := strings.TrimSpace(step.Plugin.Name)
				operation := strings.TrimSpace(step.Plugin.Operation)
				if pluginName != "" && operation != "" {
					base = append(base, core.AccessPermission{
						Plugin:     pluginName,
						Operations: []string{operation},
					})
				}
				if actionID, ok := coreworkflow.StepPluginActionID(step.ID); ok {
					base = append(base, core.AccessPermission{
						Plugin:  coreworkflow.StepActionPermissionPlugin,
						Actions: []string{actionID},
					})
				}
			}
			if step.Agent != nil {
				agentProvider := strings.TrimSpace(step.Agent.ProviderName)
				if agentProvider != "" {
					base = append(base, core.AccessPermission{
						Plugin: agentProvider,
					})
				}
				if actionID, ok := coreworkflow.StepAgentActionID(step.ID); ok {
					base = append(base, core.AccessPermission{
						Plugin:  coreworkflow.StepActionPermissionPlugin,
						Actions: []string{actionID},
					})
				}
				for j := range step.Agent.ToolRefs {
					tool := step.Agent.ToolRefs[j]
					if strings.TrimSpace(tool.System) != "" {
						continue
					}
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
			}
			if delivery := step.OutputDelivery; delivery != nil && delivery.Plugin != nil {
				pluginName := strings.TrimSpace(delivery.Plugin.Name)
				operation := strings.TrimSpace(delivery.Plugin.Operation)
				if pluginName != "" && operation != "" {
					base = append(base, core.AccessPermission{
						Plugin:     pluginName,
						Operations: []string{operation},
					})
				}
				if actionID, ok := coreworkflow.StepDeliveryActionID(step.ID); ok {
					base = append(base, core.AccessPermission{
						Plugin:  coreworkflow.StepActionPermissionPlugin,
						Actions: []string{actionID},
					})
				}
			}
		}
		return workflowMergeExecutionRefPermissions(append([][]core.AccessPermission{base}, explicit...)...)
	}
	return workflowMergeExecutionRefPermissions(append([][]core.AccessPermission{base}, explicit...)...)
}

func workflowMergeExecutionRefPermissions(groups ...[]core.AccessPermission) []core.AccessPermission {
	type permissionParts struct {
		provider   bool
		operations map[string]struct{}
		actions    map[string]struct{}
	}
	byPlugin := map[string]*permissionParts{}
	for _, group := range groups {
		for _, value := range group {
			plugin := strings.TrimSpace(value.Plugin)
			if plugin == "" {
				continue
			}
			parts := byPlugin[plugin]
			if parts == nil {
				parts = &permissionParts{}
				byPlugin[plugin] = parts
			}
			if len(value.Operations) == 0 && len(value.Actions) == 0 {
				parts.provider = true
				continue
			}
			for _, operation := range value.Operations {
				operation = strings.TrimSpace(operation)
				if operation == "" {
					continue
				}
				if parts.operations == nil {
					parts.operations = map[string]struct{}{}
				}
				parts.operations[operation] = struct{}{}
			}
			for _, action := range value.Actions {
				action = strings.TrimSpace(action)
				if action == "" {
					continue
				}
				if parts.actions == nil {
					parts.actions = map[string]struct{}{}
				}
				parts.actions[action] = struct{}{}
			}
		}
	}
	plugins := make([]string, 0, len(byPlugin))
	for plugin := range byPlugin {
		plugins = append(plugins, plugin)
	}
	slices.Sort(plugins)
	out := make([]core.AccessPermission, 0, len(plugins))
	for _, plugin := range plugins {
		parts := byPlugin[plugin]
		if parts == nil {
			continue
		}
		perm := core.AccessPermission{Plugin: plugin}
		if !parts.provider {
			if len(parts.operations) > 0 {
				operations := make([]string, 0, len(parts.operations))
				for operation := range parts.operations {
					operations = append(operations, operation)
				}
				slices.Sort(operations)
				perm.Operations = operations
			}
			if len(parts.actions) > 0 {
				actions := make([]string, 0, len(parts.actions))
				for action := range parts.actions {
					actions = append(actions, action)
				}
				slices.Sort(actions)
				perm.Actions = actions
			}
		}
		out = append(out, perm)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func workflowConfigExecutionReference(cfg *config.Config, providerName string, target coreworkflow.Target, runAs *core.RunAsSubject, permissions []core.AccessPermission) (*coreworkflow.ExecutionReference, error) {
	runAs = core.NormalizeRunAsSubject(runAs)
	executionTarget := workflowConfigExecutionRefTarget(target)
	ref := &coreworkflow.ExecutionReference{
		ProviderName:        providerName,
		Target:              executionTarget,
		SubjectID:           workflowConfigOwnerSubjectID(),
		SubjectKind:         "system",
		DisplayName:         "Gestalt config",
		AuthSource:          "config",
		CredentialSubjectID: workflowConfigOwnerSubjectID(),
		RunAs:               runAs,
		Permissions:         workflowExecutionRefPermissionsForTarget(executionTarget, permissions),
	}
	hasRunAs := runAs != nil
	for i := range target.Steps {
		step := target.Steps[i]
		if step.Plugin != nil {
			if err := workflowConfigValidateNoUserCredentialTarget(cfg, *step.Plugin, hasRunAs); err != nil {
				return nil, err
			}
		}
		if step.Agent != nil {
			for j := range step.Agent.ToolRefs {
				tool := step.Agent.ToolRefs[j]
				if strings.TrimSpace(tool.System) != "" {
					continue
				}
				if err := workflowConfigValidateNoUserCredentialTarget(cfg, coreworkflow.PluginCall{
					Name:       strings.TrimSpace(tool.Plugin),
					Operation:  strings.TrimSpace(tool.Operation),
					Connection: strings.TrimSpace(tool.Connection),
					Instance:   strings.TrimSpace(tool.Instance),
				}, hasRunAs); err != nil {
					return nil, err
				}
			}
		}
		if delivery := step.OutputDelivery; delivery != nil && delivery.Plugin != nil {
			if err := workflowConfigValidateNoUserCredentialTarget(cfg, *delivery.Plugin, hasRunAs); err != nil {
				return nil, err
			}
		}
	}
	return ref, nil
}

func workflowConfigExecutionRefTarget(target coreworkflow.Target) coreworkflow.Target {
	return target
}

func workflowConfigValidateNoUserCredentialTarget(cfg *config.Config, target coreworkflow.PluginCall, hasRunAs bool) error {
	modeOverride := core.NormalizeOptionalConnectionMode(target.CredentialMode)
	switch modeOverride {
	case "":
	case core.ConnectionModeNone:
		return nil
	case core.ConnectionModeUser:
		if hasRunAs {
			return nil
		}
		return fmt.Errorf("config-managed workflows do not support user-credentialed plugin %q", strings.TrimSpace(target.Name))
	default:
		return fmt.Errorf("unsupported credential mode %q for config-managed workflow target %q", modeOverride, strings.TrimSpace(target.Name))
	}
	mode, err := workflowConfigTargetConnectionMode(cfg, target)
	if err != nil {
		return err
	}
	pluginName := strings.TrimSpace(target.Name)
	switch mode {
	case core.ConnectionModeNone:
		return nil
	case core.ConnectionModeUser:
		if hasRunAs {
			return nil
		}
		return fmt.Errorf("config-managed workflows do not support user-credentialed plugin %q", pluginName)
	default:
		return fmt.Errorf("unsupported connection mode %q for config-managed workflow target %q", mode, pluginName)
	}
}

func workflowConfigTargetConnectionMode(cfg *config.Config, target coreworkflow.PluginCall) (core.ConnectionMode, error) {
	if cfg == nil {
		return core.ConnectionModeNone, fmt.Errorf("workflow config is not available")
	}
	pluginName := strings.TrimSpace(target.Name)
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

func workflowConfigOperationConnectionMode(plan config.StaticConnectionPlan, manifestPlugin *providermanifestv1.Spec, target coreworkflow.PluginCall) (core.ConnectionMode, bool, error) {
	connections, selectors, _, err := plan.RESTOperationConnectionBindings(manifestPlugin)
	if err != nil {
		return core.ConnectionModeNone, false, err
	}
	operation := strings.TrimSpace(target.Operation)
	if selector, ok := selectors[operation]; ok {
		connectionName, resolved := workflowConfigConnectionSelectorTargetConnection(selector, workflowConfigValueObjectMap(target.Input))
		if resolved {
			mode, err := workflowConfigConnectionModeForName(plan, target.Name, connectionName)
			return mode, true, err
		}
		if connectionName := strings.TrimSpace(connections[operation]); connectionName != "" {
			mode, err := workflowConfigConnectionModeForName(plan, target.Name, connectionName)
			return mode, true, err
		}
		mode, err := workflowConfigConnectionSelectorMode(plan, target.Name, selector)
		return mode, true, err
	}
	if connectionName := strings.TrimSpace(connections[operation]); connectionName != "" {
		mode, err := workflowConfigConnectionModeForName(plan, target.Name, connectionName)
		return mode, true, err
	}
	return core.ConnectionModeNone, false, nil
}

func workflowConfigValueObjectMap(value coreworkflow.Value) map[string]any {
	if len(value.Object) == 0 {
		return nil
	}
	out := make(map[string]any, len(value.Object))
	for key := range value.Object {
		nested := value.Object[key]
		if nested.LiteralSet {
			out[key] = nested.Literal
		}
	}
	return out
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
	connection, ok := value.(string)
	if !ok {
		return "", false
	}
	connectionName := selector.Values[strings.TrimSpace(connection)]
	return strings.TrimSpace(connectionName), strings.TrimSpace(connectionName) != ""
}

func workflowConfigConnectionSelectorMode(plan config.StaticConnectionPlan, pluginName string, selector core.OperationConnectionSelector) (core.ConnectionMode, error) {
	for _, connectionName := range selector.Values {
		mode, err := workflowConfigConnectionModeForName(plan, pluginName, connectionName)
		if err != nil {
			return core.ConnectionModeNone, err
		}
		if mode == core.ConnectionModeUser {
			return core.ConnectionModeUser, nil
		}
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
	if strings.TrimSpace(existing.CredentialSubjectID) != strings.TrimSpace(desired.CredentialSubjectID) {
		return false
	}
	if !core.RunAsSubjectsEqual(existing.RunAs, desired.RunAs) {
		return false
	}
	if !coreworkflow.TargetsEqual(existing.Target, desired.Target) {
		return false
	}
	if strings.TrimSpace(existing.TargetDigest) != strings.TrimSpace(desired.TargetDigest) {
		return false
	}
	if strings.TrimSpace(existing.ProviderPlanDigest) != strings.TrimSpace(desired.ProviderPlanDigest) {
		return false
	}
	if strings.TrimSpace(existing.PermissionsDigest) != strings.TrimSpace(desired.PermissionsDigest) {
		return false
	}
	if existing.Generation != desired.Generation {
		return false
	}
	if strings.TrimSpace(existing.Seal) != strings.TrimSpace(desired.Seal) {
		return false
	}
	existingJSON, existingErr := json.Marshal(existing.Permissions)
	desiredJSON, desiredErr := json.Marshal(desired.Permissions)
	return existingErr == nil && desiredErr == nil && bytes.Equal(existingJSON, desiredJSON)
}

func workflowConfigExecutionRefPersistenceError(refID string, stored, desired *coreworkflow.ExecutionReference) error {
	if desired != nil && desired.RunAs != nil && (stored == nil || !core.RunAsSubjectsEqual(stored.RunAs, desired.RunAs)) {
		return fmt.Errorf("workflow execution ref %q was written but provider did not preserve runAs subject %q", refID, strings.TrimSpace(desired.RunAs.SubjectID))
	}
	return fmt.Errorf("workflow execution ref %q was written but provider did not preserve config-managed execution reference metadata", refID)
}

func workflowExecutionReferenceStore(providerName string, provider coreworkflow.Provider) (coreworkflow.ExecutionReferenceStore, error) {
	store, ok := provider.(coreworkflow.ExecutionReferenceStore)
	if !ok {
		return nil, fmt.Errorf("workflow provider %q does not support execution refs", strings.TrimSpace(providerName))
	}
	return store, nil
}

func workflowLogReplacedUnreadableExecutionRef(ctx context.Context, objectType, objectKey, objectID, providerName, pluginName, oldExecutionRef, newExecutionRef string, lookupErr error) {
	slog.WarnContext(ctx, "replaced unreadable workflow execution ref during config reconciliation",
		"workflow_object_type", strings.TrimSpace(objectType),
		"workflow_object_key", strings.TrimSpace(objectKey),
		"workflow_object_id", strings.TrimSpace(objectID),
		"workflow_provider", strings.TrimSpace(providerName),
		"plugin", strings.TrimSpace(pluginName),
		"old_execution_ref", strings.TrimSpace(oldExecutionRef),
		"new_execution_ref", strings.TrimSpace(newExecutionRef),
		"error", lookupErr,
	)
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
