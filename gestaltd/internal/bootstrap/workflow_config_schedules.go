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
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
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
		{Name: "updated_at", Type: indexeddb.TypeTime},
	},
}

type workflowConfigScheduleState struct {
	ID           string
	PluginName   string
	ScheduleKey  string
	ProviderName string
	ScheduleID   string
	UpdatedAt    time.Time
}

type desiredWorkflowConfigSchedule struct {
	state    workflowConfigScheduleState
	schedule config.PluginWorkflowSchedule
}

func reconcileWorkflowConfigSchedules(ctx context.Context, cfg *config.Config, runtime *workflowRuntime, db indexeddb.IndexedDB) error {
	if cfg == nil || runtime == nil || db == nil {
		return nil
	}
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
		if err := provider.DeleteSchedule(ctx, coreworkflow.DeleteScheduleRequest{
			PluginName: prev.PluginName,
			ScheduleID: prev.ScheduleID,
		}); err != nil {
			if isWorkflowScheduleNotFound(err) {
				if err := store.Delete(ctx, rowID); err != nil {
					return fmt.Errorf("bootstrap: delete workflow schedule state %q: %w", rowID, err)
				}
				continue
			}
			return fmt.Errorf("bootstrap: delete workflow schedule %q for plugin %q: %w", prev.ScheduleID, prev.PluginName, err)
		}
		if err := store.Delete(ctx, rowID); err != nil {
			return fmt.Errorf("bootstrap: delete workflow schedule state %q: %w", rowID, err)
		}
	}

	for _, pluginName := range slices.Sorted(maps.Keys(cfg.Plugins)) {
		entry := cfg.Plugins[pluginName]
		effective, err := cfg.EffectivePluginWorkflow(pluginName, entry)
		if err != nil {
			return err
		}
		if !effective.Enabled || len(effective.Schedules) == 0 {
			continue
		}
		provider, allowed, err := runtime.ResolvePlugin(pluginName)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow schedules for plugin %q: %w", pluginName, err)
		}
		for _, scheduleKey := range slices.Sorted(maps.Keys(effective.Schedules)) {
			schedule := effective.Schedules[scheduleKey]
			if _, ok := allowed[schedule.Operation]; !ok {
				return fmt.Errorf("bootstrap: workflow schedule %q for plugin %q targets disabled operation %q", scheduleKey, pluginName, schedule.Operation)
			}
			rowID := workflowConfigScheduleStateID(pluginName, scheduleKey)
			desiredEntry := desired[rowID]
			prev, owned := previous[rowID]
			if !owned {
				existing, err := provider.GetSchedule(ctx, coreworkflow.GetScheduleRequest{
					PluginName: pluginName,
					ScheduleID: desiredEntry.state.ScheduleID,
				})
				switch {
				case err == nil:
					if !isWorkflowConfigOwnedSchedule(existing, pluginName, desiredEntry.state.ScheduleID) &&
						!isAdoptableWorkflowSchedule(existing, pluginName, schedule, desiredEntry.state.ScheduleID) {
						return fmt.Errorf("bootstrap: workflow schedule %q for plugin %q conflicts with existing unmanaged schedule id %q", scheduleKey, pluginName, desiredEntry.state.ScheduleID)
					}
				case isWorkflowScheduleNotFound(err):
				default:
					return fmt.Errorf("bootstrap: get workflow schedule %q for plugin %q: %w", desiredEntry.state.ScheduleID, pluginName, err)
				}
			}
			if _, err := provider.UpsertSchedule(ctx, coreworkflow.UpsertScheduleRequest{
				ScheduleID:  desiredEntry.state.ScheduleID,
				Cron:        schedule.Cron,
				Timezone:    schedule.Timezone,
				Target:      workflowConfigScheduleTarget(pluginName, schedule),
				Paused:      schedule.Paused,
				RequestedBy: workflowConfigActor(pluginName),
			}); err != nil {
				return fmt.Errorf("bootstrap: workflow schedule %q for plugin %q: %w", scheduleKey, pluginName, err)
			}
			if owned && prev.ProviderName != desiredEntry.state.ProviderName {
				oldProvider, err := runtime.ResolveProvider(prev.ProviderName)
				if err != nil {
					return fmt.Errorf("bootstrap: cleanup workflow schedule %q for plugin %q requires provider %q: %w", prev.ScheduleID, prev.PluginName, prev.ProviderName, err)
				}
				if err := oldProvider.DeleteSchedule(ctx, coreworkflow.DeleteScheduleRequest{
					PluginName: prev.PluginName,
					ScheduleID: prev.ScheduleID,
				}); err != nil && !isWorkflowScheduleNotFound(err) {
					return fmt.Errorf("bootstrap: delete workflow schedule %q for plugin %q: %w", prev.ScheduleID, prev.PluginName, err)
				}
			}
			if err := store.Put(ctx, desiredEntry.state.record()); err != nil {
				return fmt.Errorf("bootstrap: store workflow schedule state %q: %w", rowID, err)
			}
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
	for _, pluginName := range slices.Sorted(maps.Keys(cfg.Plugins)) {
		entry := cfg.Plugins[pluginName]
		effective, err := cfg.EffectivePluginWorkflow(pluginName, entry)
		if err != nil {
			return nil, err
		}
		if !effective.Enabled {
			continue
		}
		for _, scheduleKey := range slices.Sorted(maps.Keys(effective.Schedules)) {
			rowID := workflowConfigScheduleStateID(pluginName, scheduleKey)
			desired[rowID] = desiredWorkflowConfigSchedule{
				state: workflowConfigScheduleState{
					ID:           rowID,
					PluginName:   pluginName,
					ScheduleKey:  scheduleKey,
					ProviderName: effective.ProviderName,
					ScheduleID:   workflowConfigScheduleID(pluginName, scheduleKey),
					UpdatedAt:    now,
				},
				schedule: effective.Schedules[scheduleKey],
			}
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

func isWorkflowScheduleNotFound(err error) bool {
	return errors.Is(err, core.ErrNotFound) || status.Code(err) == codes.NotFound
}

func isAdoptableWorkflowSchedule(existing *coreworkflow.Schedule, pluginName string, schedule config.PluginWorkflowSchedule, scheduleID string) bool {
	if existing == nil {
		return false
	}
	return existing.ID == scheduleID &&
		existing.Cron == schedule.Cron &&
		existing.Timezone == schedule.Timezone &&
		existing.Paused == schedule.Paused &&
		existing.Target.PluginName == pluginName &&
		existing.Target.Operation == schedule.Operation &&
		workflowScheduleInputsEqual(existing.Target.Input, schedule.Input)
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

func workflowScheduleInputsEqual(left, right map[string]any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

func workflowConfigScheduleTarget(pluginName string, schedule config.PluginWorkflowSchedule) coreworkflow.Target {
	return coreworkflow.Target{
		PluginName: pluginName,
		Operation:  schedule.Operation,
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

func workflowConfigScheduleStateID(pluginName, scheduleKey string) string {
	return pluginName + "\x00" + scheduleKey
}

func workflowConfigScheduleID(pluginName, scheduleKey string) string {
	sum := sha256.Sum256([]byte(pluginName + "\x00" + scheduleKey))
	return coreworkflow.ConfigManagedSchedulePrefix + hex.EncodeToString(sum[:])
}
