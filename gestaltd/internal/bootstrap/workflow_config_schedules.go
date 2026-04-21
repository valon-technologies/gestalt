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
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const workflowConfigScheduleStateStore = "workflow_config_schedules"

var workflowConfigScheduleStateSchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_plugin", KeyPath: []string{"plugin_name"}},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "plugin_name", Type: indexeddb.TypeString, NotNull: true},
		{Name: "schedule_key", Type: indexeddb.TypeString, NotNull: true},
		{Name: "provider_name", Type: indexeddb.TypeString, NotNull: true},
		{Name: "schedule_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "execution_ref", Type: indexeddb.TypeString},
		{Name: "updated_at", Type: indexeddb.TypeTime},
	},
}

type workflowConfigScheduleState struct {
	ID           string
	PluginName   string
	ScheduleKey  string
	ProviderName string
	ScheduleID   string
	ExecutionRef string
	UpdatedAt    time.Time
}

type desiredWorkflowConfigSchedule struct {
	state    workflowConfigScheduleState
	schedule config.WorkflowScheduleConfig
}

func reconcileWorkflowConfigSchedules(ctx context.Context, cfg *config.Config, runtime *workflowRuntime, db indexeddb.IndexedDB) error {
	if cfg == nil || runtime == nil || db == nil {
		return nil
	}
	executionRefs := coredata.NewWorkflowExecutionRefService(db)
	if err := db.CreateObjectStore(ctx, workflowConfigScheduleStateStore, workflowConfigScheduleStateSchema); err != nil {
		return fmt.Errorf("bootstrap: create workflow config schedule store: %w", err)
	}
	store := db.ObjectStore(workflowConfigScheduleStateStore)
	previous, err := loadWorkflowConfigScheduleState(ctx, store)
	if err != nil {
		return err
	}
	desired, err := desiredWorkflowConfigSchedules(cfg)
	if err != nil {
		return err
	}

	for _, rowID := range slices.Sorted(maps.Keys(previous)) {
		prev := previous[rowID]
		if _, ok := desired[rowID]; ok {
			continue
		}
		provider, err := runtime.ResolveProvider(prev.ProviderName)
		if err != nil {
			return fmt.Errorf("bootstrap: cleanup workflow schedule %q for plugin %q requires provider %q: %w", prev.ScheduleID, prev.PluginName, prev.ProviderName, err)
		}
		providerCtx := invocation.WithWorkflowContextString(ctx, "plugin", prev.PluginName)
		existing, err := provider.GetSchedule(providerCtx, coreworkflow.GetScheduleRequest{
			ScheduleID: prev.ScheduleID,
		})
		existingExecutionRef := ""
		switch {
		case err == nil:
			existingExecutionRef = workflowScheduleExecutionRef(existing, prev.ExecutionRef)
		case isWorkflowObjectNotFound(err):
			existingExecutionRef = strings.TrimSpace(prev.ExecutionRef)
		default:
			return fmt.Errorf("bootstrap: get workflow schedule %q for plugin %q: %w", prev.ScheduleID, prev.PluginName, err)
		}
		if err := provider.DeleteSchedule(providerCtx, coreworkflow.DeleteScheduleRequest{
			ScheduleID: prev.ScheduleID,
		}); err != nil {
			if isWorkflowObjectNotFound(err) {
				if err := workflowRevokeExecutionRefByID(ctx, executionRefs, existingExecutionRef); err != nil {
					return fmt.Errorf("bootstrap: revoke workflow execution ref %q for schedule %q on plugin %q: %w", existingExecutionRef, prev.ScheduleID, prev.PluginName, err)
				}
				if err := store.Delete(ctx, rowID); err != nil {
					return fmt.Errorf("bootstrap: delete workflow schedule state %q: %w", rowID, err)
				}
				continue
			}
			return fmt.Errorf("bootstrap: delete workflow schedule %q for plugin %q: %w", prev.ScheduleID, prev.PluginName, err)
		}
		if err := workflowRevokeExecutionRefByID(ctx, executionRefs, existingExecutionRef); err != nil {
			return fmt.Errorf("bootstrap: revoke workflow execution ref %q for schedule %q on plugin %q: %w", existingExecutionRef, prev.ScheduleID, prev.PluginName, err)
		}
		if err := store.Delete(ctx, rowID); err != nil {
			return fmt.Errorf("bootstrap: delete workflow schedule state %q: %w", rowID, err)
		}
	}

	for _, scheduleKey := range slices.Sorted(maps.Keys(cfg.Workflows.Schedules)) {
		schedule := cfg.Workflows.Schedules[scheduleKey]
		pluginName := schedule.Plugin
		provider, allowed, err := runtime.ResolvePlugin(pluginName)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow schedules for plugin %q: %w", pluginName, err)
		}
		target := workflowConfigScheduleTarget(schedule)
		if _, ok := allowed[schedule.Operation]; !ok {
			return fmt.Errorf("bootstrap: workflow schedule %q for plugin %q targets disabled operation %q", scheduleKey, pluginName, schedule.Operation)
		}
		rowID := workflowConfigScheduleStateID(scheduleKey)
		desiredEntry := desired[rowID]
		prev, owned := previous[rowID]
		existingExecutionRef := ""
		providerCtx := invocation.WithWorkflowContextString(ctx, "plugin", pluginName)
		existing, err := provider.GetSchedule(providerCtx, coreworkflow.GetScheduleRequest{
			ScheduleID: desiredEntry.state.ScheduleID,
		})
		switch {
		case err == nil:
			if !isWorkflowConfigOwnedSchedule(existing, pluginName, desiredEntry.state.ScheduleID) &&
				!isAdoptableWorkflowSchedule(existing, schedule, desiredEntry.state.ScheduleID) {
				return fmt.Errorf("bootstrap: workflow schedule %q for plugin %q conflicts with existing unmanaged schedule id %q", scheduleKey, pluginName, desiredEntry.state.ScheduleID)
			}
		case isWorkflowObjectNotFound(err):
			existing = nil
			if owned {
				existingExecutionRef = strings.TrimSpace(prev.ExecutionRef)
			}
		default:
			return fmt.Errorf("bootstrap: get workflow schedule %q for plugin %q: %w", desiredEntry.state.ScheduleID, pluginName, err)
		}
		if existing != nil {
			existingExecutionRef = workflowScheduleExecutionRef(existing, prev.ExecutionRef)
		}
		desiredExecutionRef := &coreworkflow.ExecutionReference{
			ProviderName: desiredEntry.state.ProviderName,
			Target:       target,
			SubjectID:    principal.WorkloadSubjectID(workflowWorkloadID(pluginName)),
			Permissions:  workflowExecutionRefPermissionsForTarget(target),
		}
		executionRefID, createdExecutionRef, err := workflowEnsureConfigScheduleExecutionRef(ctx, executionRefs, desiredEntry.state.ScheduleID, desiredExecutionRef, existingExecutionRef)
		if err != nil {
			return fmt.Errorf("bootstrap: store workflow execution ref for schedule %q on plugin %q: %w", scheduleKey, pluginName, err)
		}
		if _, err := provider.UpsertSchedule(providerCtx, coreworkflow.UpsertScheduleRequest{
			ScheduleID:   desiredEntry.state.ScheduleID,
			Cron:         schedule.Cron,
			Timezone:     schedule.Timezone,
			Target:       target,
			Paused:       schedule.Paused,
			RequestedBy:  workflowConfigActor(pluginName),
			ExecutionRef: executionRefID,
		}); err != nil {
			if createdExecutionRef {
				_ = workflowRevokeExecutionRefByID(ctx, executionRefs, executionRefID)
			}
			return fmt.Errorf("bootstrap: workflow schedule %q for plugin %q: %w", scheduleKey, pluginName, err)
		}
		if existingExecutionRef != executionRefID {
			if err := workflowRevokeExecutionRefByID(ctx, executionRefs, existingExecutionRef); err != nil {
				return fmt.Errorf("bootstrap: revoke workflow execution ref %q for schedule %q on plugin %q: %w", existingExecutionRef, desiredEntry.state.ScheduleID, pluginName, err)
			}
		}
		if owned && prev.ProviderName != desiredEntry.state.ProviderName {
			oldProvider, err := runtime.ResolveProvider(prev.ProviderName)
			if err != nil {
				return fmt.Errorf("bootstrap: cleanup workflow schedule %q for plugin %q requires provider %q: %w", prev.ScheduleID, prev.PluginName, prev.ProviderName, err)
			}
			oldProviderCtx := invocation.WithWorkflowContextString(ctx, "plugin", prev.PluginName)
			oldExisting, err := oldProvider.GetSchedule(oldProviderCtx, coreworkflow.GetScheduleRequest{
				ScheduleID: prev.ScheduleID,
			})
			oldExecutionRef := ""
			switch {
			case err == nil:
				oldExecutionRef = workflowScheduleExecutionRef(oldExisting, prev.ExecutionRef)
			case isWorkflowObjectNotFound(err):
				oldExecutionRef = strings.TrimSpace(prev.ExecutionRef)
			default:
				return fmt.Errorf("bootstrap: get workflow schedule %q for plugin %q: %w", prev.ScheduleID, prev.PluginName, err)
			}
			if err := oldProvider.DeleteSchedule(oldProviderCtx, coreworkflow.DeleteScheduleRequest{
				ScheduleID: prev.ScheduleID,
			}); err != nil && !isWorkflowObjectNotFound(err) {
				return fmt.Errorf("bootstrap: delete workflow schedule %q for plugin %q: %w", prev.ScheduleID, prev.PluginName, err)
			}
			if err := workflowRevokeExecutionRefByID(ctx, executionRefs, oldExecutionRef); err != nil {
				return fmt.Errorf("bootstrap: revoke workflow execution ref %q for schedule %q on plugin %q: %w", oldExecutionRef, prev.ScheduleID, prev.PluginName, err)
			}
		}
		desiredEntry.state.ExecutionRef = executionRefID
		if err := store.Put(ctx, desiredEntry.state.record()); err != nil {
			return fmt.Errorf("bootstrap: store workflow schedule state %q: %w", rowID, err)
		}
	}
	return nil
}

func desiredWorkflowConfigSchedules(cfg *config.Config) (map[string]desiredWorkflowConfigSchedule, error) {
	desired := make(map[string]desiredWorkflowConfigSchedule)
	if cfg == nil {
		return desired, nil
	}
	now := time.Now()
	for _, scheduleKey := range slices.Sorted(maps.Keys(cfg.Workflows.Schedules)) {
		schedule := cfg.Workflows.Schedules[scheduleKey]
		binding, err := cfg.EffectiveWorkflowBinding(schedule.Plugin)
		if err != nil {
			return nil, err
		}
		if !binding.Enabled {
			continue
		}
		rowID := workflowConfigScheduleStateID(scheduleKey)
		desired[rowID] = desiredWorkflowConfigSchedule{
			state: workflowConfigScheduleState{
				ID:           rowID,
				PluginName:   schedule.Plugin,
				ScheduleKey:  scheduleKey,
				ProviderName: binding.ProviderName,
				ScheduleID:   workflowConfigScheduleID(scheduleKey),
				UpdatedAt:    now,
			},
			schedule: schedule,
		}
	}
	return desired, nil
}

func loadWorkflowConfigScheduleState(ctx context.Context, store indexeddb.ObjectStore) (map[string]workflowConfigScheduleState, error) {
	recs, err := store.GetAll(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: load workflow schedule state: %w", err)
	}
	state := make(map[string]workflowConfigScheduleState, len(recs))
	for _, rec := range recs {
		entry := workflowConfigScheduleStateFromRecord(rec)
		state[entry.ID] = entry
	}
	return state, nil
}

func (s workflowConfigScheduleState) record() indexeddb.Record {
	return indexeddb.Record{
		"id":            s.ID,
		"plugin_name":   s.PluginName,
		"schedule_key":  s.ScheduleKey,
		"provider_name": s.ProviderName,
		"schedule_id":   s.ScheduleID,
		"execution_ref": s.ExecutionRef,
		"updated_at":    s.UpdatedAt,
	}
}

func workflowConfigScheduleStateFromRecord(rec indexeddb.Record) workflowConfigScheduleState {
	return workflowConfigScheduleState{
		ID:           workflowConfigScheduleRecordString(rec, "id"),
		PluginName:   workflowConfigScheduleRecordString(rec, "plugin_name"),
		ScheduleKey:  workflowConfigScheduleRecordString(rec, "schedule_key"),
		ProviderName: workflowConfigScheduleRecordString(rec, "provider_name"),
		ScheduleID:   workflowConfigScheduleRecordString(rec, "schedule_id"),
		ExecutionRef: workflowConfigScheduleRecordString(rec, "execution_ref"),
		UpdatedAt:    workflowConfigScheduleRecordTime(rec, "updated_at"),
	}
}

func workflowConfigScheduleRecordString(rec indexeddb.Record, key string) string {
	if rec == nil {
		return ""
	}
	value, _ := rec[key].(string)
	return value
}

func workflowConfigScheduleRecordTime(rec indexeddb.Record, key string) time.Time {
	if rec == nil {
		return time.Time{}
	}
	value, _ := rec[key].(time.Time)
	return value
}

func isWorkflowObjectNotFound(err error) bool {
	return errors.Is(err, core.ErrNotFound) || status.Code(err) == codes.NotFound
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
	actor := workflowConfigActor(pluginName)
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

func workflowConfigActor(pluginName string) coreworkflow.Actor {
	return coreworkflow.Actor{
		SubjectID:   "config:workflow:" + pluginName,
		SubjectKind: "system",
		DisplayName: "Workflow Config (" + pluginName + ")",
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

func workflowEnsureConfigScheduleExecutionRef(
	ctx context.Context,
	service *coredata.WorkflowExecutionRefService,
	scheduleID string,
	desired *coreworkflow.ExecutionReference,
	candidateIDs ...string,
) (string, bool, error) {
	if service == nil {
		return "", false, fmt.Errorf("workflow execution refs are not configured")
	}
	for _, candidateID := range candidateIDs {
		candidateID = strings.TrimSpace(candidateID)
		if candidateID == "" {
			continue
		}
		existing, err := service.Get(ctx, candidateID)
		if err != nil {
			if errors.Is(err, indexeddb.ErrNotFound) {
				continue
			}
			return "", false, err
		}
		if workflowConfigExecutionRefMatches(existing, desired) {
			return candidateID, false, nil
		}
	}
	refID := workflowConfigScheduleExecutionRefID(scheduleID)
	desired.ID = refID
	if _, err := service.Put(ctx, desired); err != nil {
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

func workflowRevokeExecutionRefByID(ctx context.Context, service *coredata.WorkflowExecutionRefService, refID string) error {
	if service == nil || strings.TrimSpace(refID) == "" {
		return nil
	}
	ref, err := service.Get(ctx, refID)
	if err != nil {
		if errors.Is(err, indexeddb.ErrNotFound) {
			return nil
		}
		return err
	}
	if ref == nil || ref.RevokedAt != nil {
		return nil
	}
	now := time.Now()
	ref.RevokedAt = &now
	_, err = service.Put(ctx, ref)
	return err
}
