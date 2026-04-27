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
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type desiredWorkflowConfigSchedule struct {
	ID           string
	PluginName   string
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
		pluginName := workflowConfigTargetLabel(workflowConfigScheduleTarget(schedule))
		providerName, provider, err := runtime.ResolveProviderSelection(schedule.Provider)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow schedule %q for plugin %q: %w", desiredEntry.ScheduleKey, pluginName, err)
		}
		target := workflowConfigScheduleTarget(schedule)
		existingExecutionRef := ""
		providerCtx := invocation.WithWorkflowContextString(ctx, "plugin", pluginName)
		existing, err := provider.GetSchedule(providerCtx, coreworkflow.GetScheduleRequest{
			ScheduleID: desiredEntry.ScheduleID,
		})
		switch {
		case err == nil:
			if !isWorkflowConfigOwnedSchedule(existing, pluginName, desiredEntry.ScheduleID) &&
				!isAdoptableWorkflowSchedule(existing, schedule, desiredEntry.ScheduleID) {
				return fmt.Errorf("bootstrap: workflow schedule %q for plugin %q conflicts with existing unmanaged schedule id %q", desiredEntry.ScheduleKey, pluginName, desiredEntry.ScheduleID)
			}
		case isWorkflowObjectNotFound(err):
			existing = nil
		default:
			return fmt.Errorf("bootstrap: get workflow schedule %q for plugin %q: %w", desiredEntry.ScheduleID, pluginName, err)
		}
		if existing != nil {
			existingExecutionRef = workflowScheduleExecutionRef(existing, "")
		}
		desiredExecutionRef, err := workflowConfigExecutionReference(cfg, providerName, target)
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
		rowID := workflowConfigScheduleStateID(scheduleKey)
		desired[rowID] = desiredWorkflowConfigSchedule{
			ID:           rowID,
			PluginName:   schedule.Plugin,
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

func isAdoptableWorkflowSchedule(existing *coreworkflow.Schedule, schedule config.WorkflowScheduleConfig, scheduleID string) bool {
	if existing == nil {
		return false
	}
	return existing.ID == scheduleID &&
		existing.Cron == schedule.Cron &&
		existing.Timezone == schedule.Timezone &&
		existing.Paused == schedule.Paused &&
		workflowTargetsEqual(existing.Target, workflowConfigScheduleTarget(schedule))
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

func workflowTargetsEqual(left, right coreworkflow.Target) bool {
	leftFingerprint, leftErr := coreworkflow.TargetFingerprint(left)
	rightFingerprint, rightErr := coreworkflow.TargetFingerprint(right)
	return leftErr == nil && rightErr == nil && leftFingerprint == rightFingerprint
}

func workflowConfigTargetLabel(target coreworkflow.Target) string {
	if target.Agent != nil {
		providerName := strings.TrimSpace(target.Agent.ProviderName)
		if providerName == "" {
			providerName = "default"
		}
		return "agent:" + providerName
	}
	return strings.TrimSpace(target.PluginTarget().PluginName)
}

func workflowConfigScheduleTarget(schedule config.WorkflowScheduleConfig) coreworkflow.Target {
	if schedule.Agent != nil {
		return coreworkflow.Target{Agent: workflowConfigAgentTarget(schedule.Agent)}
	}
	pluginTarget := coreworkflow.PluginTarget{
		PluginName: schedule.Plugin,
		Operation:  schedule.Operation,
		Connection: schedule.Connection,
		Instance:   schedule.Instance,
		Input:      maps.Clone(schedule.Input),
	}
	return coreworkflow.Target{
		PluginName: pluginTarget.PluginName,
		Operation:  pluginTarget.Operation,
		Connection: pluginTarget.Connection,
		Instance:   pluginTarget.Instance,
		Input:      pluginTarget.Input,
		Plugin:     &pluginTarget,
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
		pluginName := strings.TrimSpace(tool.PluginName)
		if pluginName == "" {
			pluginName = strings.TrimSpace(tool.Plugin)
		}
		tools = append(tools, coreagent.ToolRef{
			PluginName:  pluginName,
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
		ToolSource:      coreagent.ToolSourceModeExplicit,
		ResponseSchema:  maps.Clone(agent.ResponseSchema),
		Metadata:        maps.Clone(agent.Metadata),
		ProviderOptions: maps.Clone(agent.ProviderOptions),
		TimeoutSeconds:  timeoutSeconds,
	}
}

func workflowConfigActor() coreworkflow.Actor {
	return coreworkflow.Actor{
		SubjectID:   workflowConfigOwnerSubjectID(),
		SubjectKind: "system",
		DisplayName: "Workflow Config",
		AuthSource:  "config",
	}
}

func workflowConfigScheduleStateID(scheduleKey string) string {
	return strings.TrimSpace(scheduleKey)
}

func workflowConfigScheduleID(scheduleKey string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(scheduleKey)))
	return coreworkflow.ConfigManagedSchedulePrefix + hex.EncodeToString(sum[:])
}

func workflowConfigScheduleExecutionRefID(scheduleID string) string {
	return "workflow_schedule:" + scheduleID + ":" + uuid.NewString()
}

func workflowScheduleExecutionRef(existing *coreworkflow.Schedule, fallback string) string {
	if existing != nil {
		if refID := strings.TrimSpace(existing.ExecutionRef); refID != "" {
			return refID
		}
	}
	return strings.TrimSpace(fallback)
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

func workflowExecutionRefPermissionsForTarget(target coreworkflow.Target) []core.AccessPermission {
	if target.Agent != nil {
		out := make([]core.AccessPermission, 0, len(target.Agent.ToolRefs))
		for _, tool := range target.Agent.ToolRefs {
			pluginName := strings.TrimSpace(tool.PluginName)
			operation := strings.TrimSpace(tool.Operation)
			if pluginName == "" || operation == "" {
				continue
			}
			out = append(out, core.AccessPermission{
				Plugin:     pluginName,
				Operations: []string{operation},
			})
		}
		return out
	}
	pluginTarget := target.PluginTarget()
	pluginName := pluginTarget.PluginName
	operation := strings.TrimSpace(pluginTarget.Operation)
	if pluginName == "" || operation == "" {
		return nil
	}
	return []core.AccessPermission{{
		Plugin:     pluginName,
		Operations: []string{operation},
	}}
}

func workflowConfigExecutionReference(cfg *config.Config, providerName string, target coreworkflow.Target) (*coreworkflow.ExecutionReference, error) {
	ref := &coreworkflow.ExecutionReference{
		ProviderName:        providerName,
		Target:              target,
		SubjectID:           workflowConfigOwnerSubjectID(),
		CredentialSubjectID: workflowConfigOwnerSubjectID(),
		Permissions:         workflowExecutionRefPermissionsForTarget(target),
	}
	fingerprint, err := coreworkflow.TargetFingerprint(target)
	if err != nil {
		return nil, err
	}
	ref.TargetFingerprint = fingerprint
	if target.Agent != nil {
		for _, tool := range target.Agent.ToolRefs {
			if err := workflowConfigValidateNoUserCredentialTarget(cfg, tool.PluginName); err != nil {
				return nil, err
			}
		}
		return ref, nil
	}
	if err := workflowConfigValidateNoUserCredentialTarget(cfg, target.PluginTarget().PluginName); err != nil {
		return nil, err
	}
	return ref, nil
}

func workflowConfigValidateNoUserCredentialTarget(cfg *config.Config, pluginName string) error {
	mode, err := workflowConfigTargetConnectionMode(cfg, pluginName)
	if err != nil {
		return err
	}
	switch mode {
	case core.ConnectionModeNone:
		return nil
	case core.ConnectionModeUser:
		return fmt.Errorf("config-managed workflows do not support user-credentialed plugin %q", strings.TrimSpace(pluginName))
	default:
		return fmt.Errorf("unsupported connection mode %q for config-managed workflow target %q", mode, strings.TrimSpace(pluginName))
	}
}

func workflowConfigTargetConnectionMode(cfg *config.Config, pluginName string) (core.ConnectionMode, error) {
	if cfg == nil {
		return core.ConnectionModeNone, fmt.Errorf("workflow config is not available")
	}
	pluginName = strings.TrimSpace(pluginName)
	entry := cfg.Plugins[pluginName]
	if entry == nil {
		return core.ConnectionModeNone, fmt.Errorf("workflow target plugin %q is not configured", pluginName)
	}
	return core.NormalizeConnectionMode(core.ConnectionMode(entry.ConnectionMode)), nil
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
	if !workflowTargetsEqual(existing.Target, desired.Target) {
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
