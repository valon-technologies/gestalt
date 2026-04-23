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
		pluginName := schedule.Plugin
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
			if schedule == nil || !isWorkflowConfigOwnedSchedule(schedule, schedule.Target.PluginName, schedule.ID) {
				continue
			}
			if _, ok := desiredByProviderSchedule[workflowConfigProviderObjectKey(providerName, schedule.ID)]; ok {
				continue
			}
			pluginName := strings.TrimSpace(schedule.Target.PluginName)
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
		existing.Target.PluginName == schedule.Plugin &&
		existing.Target.Operation == schedule.Operation &&
		existing.Target.Connection == schedule.Connection &&
		existing.Target.Instance == schedule.Instance &&
		workflowTargetInputsEqual(existing.Target.Input, schedule.Input)
}

func isWorkflowConfigOwnedSchedule(existing *coreworkflow.Schedule, pluginName, scheduleID string) bool {
	if existing == nil {
		return false
	}
	actor := workflowConfigActor()
	return existing.ID == scheduleID &&
		existing.Target.PluginName == pluginName &&
		existing.CreatedBy.SubjectID == actor.SubjectID &&
		existing.CreatedBy.SubjectKind == actor.SubjectKind &&
		existing.CreatedBy.AuthSource == actor.AuthSource
}

func workflowTargetInputsEqual(left, right map[string]any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

func workflowConfigScheduleTarget(schedule config.WorkflowScheduleConfig) coreworkflow.Target {
	return coreworkflow.Target{
		PluginName: schedule.Plugin,
		Operation:  schedule.Operation,
		Connection: schedule.Connection,
		Instance:   schedule.Instance,
		Input:      maps.Clone(schedule.Input),
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
	pluginName := target.PluginName
	operation := strings.TrimSpace(target.Operation)
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
	mode, err := workflowConfigTargetConnectionMode(cfg, target.PluginName)
	if err != nil {
		return nil, err
	}
	switch mode {
	case core.ConnectionModeNone:
		return ref, nil
	case core.ConnectionModeUser:
		return nil, fmt.Errorf("config-managed workflows do not support user-credentialed plugin %q", strings.TrimSpace(target.PluginName))
	default:
		return nil, fmt.Errorf("unsupported connection mode %q for config-managed workflow target %q", mode, strings.TrimSpace(target.PluginName))
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
	if strings.TrimSpace(existing.Target.PluginName) != strings.TrimSpace(desired.Target.PluginName) {
		return false
	}
	if strings.TrimSpace(existing.Target.Operation) != strings.TrimSpace(desired.Target.Operation) {
		return false
	}
	if strings.TrimSpace(existing.Target.Connection) != strings.TrimSpace(desired.Target.Connection) {
		return false
	}
	if strings.TrimSpace(existing.Target.Instance) != strings.TrimSpace(desired.Target.Instance) {
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
