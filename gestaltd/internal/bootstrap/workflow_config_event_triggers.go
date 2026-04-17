package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
)

const workflowConfigEventTriggerStateStore = "workflow_config_event_triggers"

var workflowConfigEventTriggerStateSchema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_plugin", KeyPath: []string{"plugin_name"}},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "plugin_name", Type: indexeddb.TypeString, NotNull: true},
		{Name: "trigger_key", Type: indexeddb.TypeString, NotNull: true},
		{Name: "provider_name", Type: indexeddb.TypeString, NotNull: true},
		{Name: "trigger_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "updated_at", Type: indexeddb.TypeTime},
	},
}

type workflowConfigEventTriggerState struct {
	ID           string
	PluginName   string
	TriggerKey   string
	ProviderName string
	TriggerID    string
	UpdatedAt    time.Time
}

type desiredWorkflowConfigEventTrigger struct {
	state   workflowConfigEventTriggerState
	trigger config.PluginWorkflowEventTrigger
}

func reconcileWorkflowConfigEventTriggers(ctx context.Context, cfg *config.Config, runtime *workflowRuntime, db indexeddb.IndexedDB) error {
	if cfg == nil || runtime == nil || db == nil {
		return nil
	}
	if err := db.CreateObjectStore(ctx, workflowConfigEventTriggerStateStore, workflowConfigEventTriggerStateSchema); err != nil {
		return fmt.Errorf("bootstrap: create workflow config event trigger store: %w", err)
	}
	store := db.ObjectStore(workflowConfigEventTriggerStateStore)
	previous, err := loadWorkflowConfigEventTriggerState(ctx, store)
	if err != nil {
		return err
	}
	desired, err := desiredWorkflowConfigEventTriggers(cfg)
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
			return fmt.Errorf("bootstrap: cleanup workflow event trigger %q for plugin %q requires provider %q: %w", prev.TriggerID, prev.PluginName, prev.ProviderName, err)
		}
		if err := provider.DeleteEventTrigger(ctx, coreworkflow.DeleteEventTriggerRequest{
			PluginName: prev.PluginName,
			TriggerID:  prev.TriggerID,
		}); err != nil {
			if isWorkflowObjectNotFound(err) {
				if err := store.Delete(ctx, rowID); err != nil {
					return fmt.Errorf("bootstrap: delete workflow event trigger state %q: %w", rowID, err)
				}
				continue
			}
			return fmt.Errorf("bootstrap: delete workflow event trigger %q for plugin %q: %w", prev.TriggerID, prev.PluginName, err)
		}
		if err := store.Delete(ctx, rowID); err != nil {
			return fmt.Errorf("bootstrap: delete workflow event trigger state %q: %w", rowID, err)
		}
	}

	for _, pluginName := range slices.Sorted(maps.Keys(cfg.Plugins)) {
		entry := cfg.Plugins[pluginName]
		effective, err := cfg.EffectivePluginWorkflow(pluginName, entry)
		if err != nil {
			return err
		}
		if !effective.Enabled || len(effective.EventTriggers) == 0 {
			continue
		}
		provider, allowed, err := runtime.ResolvePlugin(pluginName)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow event triggers for plugin %q: %w", pluginName, err)
		}
		for _, triggerKey := range slices.Sorted(maps.Keys(effective.EventTriggers)) {
			trigger := effective.EventTriggers[triggerKey]
			if _, ok := allowed[trigger.Operation]; !ok {
				return fmt.Errorf("bootstrap: workflow event trigger %q for plugin %q targets disabled operation %q", triggerKey, pluginName, trigger.Operation)
			}
			rowID := workflowConfigEventTriggerStateID(pluginName, triggerKey)
			desiredEntry := desired[rowID]
			prev, owned := previous[rowID]
			if !owned || prev.ProviderName != desiredEntry.state.ProviderName {
				existing, err := provider.GetEventTrigger(ctx, coreworkflow.GetEventTriggerRequest{
					PluginName: pluginName,
					TriggerID:  desiredEntry.state.TriggerID,
				})
				switch {
				case err == nil:
					if !isWorkflowConfigOwnedEventTrigger(existing, pluginName, desiredEntry.state.TriggerID) &&
						!isAdoptableWorkflowEventTrigger(existing, pluginName, trigger, desiredEntry.state.TriggerID) {
						return fmt.Errorf("bootstrap: workflow event trigger %q for plugin %q conflicts with existing unmanaged trigger id %q", triggerKey, pluginName, desiredEntry.state.TriggerID)
					}
				case isWorkflowObjectNotFound(err):
				default:
					return fmt.Errorf("bootstrap: get workflow event trigger %q for plugin %q: %w", desiredEntry.state.TriggerID, pluginName, err)
				}
			}
			if _, err := provider.UpsertEventTrigger(ctx, coreworkflow.UpsertEventTriggerRequest{
				TriggerID:   desiredEntry.state.TriggerID,
				Match:       workflowConfigEventTriggerMatch(trigger),
				Target:      workflowConfigEventTriggerTarget(pluginName, trigger),
				Paused:      trigger.Paused,
				RequestedBy: workflowConfigActor(pluginName),
			}); err != nil {
				return fmt.Errorf("bootstrap: workflow event trigger %q for plugin %q: %w", triggerKey, pluginName, err)
			}
			if owned && prev.ProviderName != desiredEntry.state.ProviderName {
				oldProvider, err := runtime.ResolveProvider(prev.ProviderName)
				if err != nil {
					return fmt.Errorf("bootstrap: cleanup workflow event trigger %q for plugin %q requires provider %q: %w", prev.TriggerID, prev.PluginName, prev.ProviderName, err)
				}
				if err := oldProvider.DeleteEventTrigger(ctx, coreworkflow.DeleteEventTriggerRequest{
					PluginName: prev.PluginName,
					TriggerID:  prev.TriggerID,
				}); err != nil && !isWorkflowObjectNotFound(err) {
					return fmt.Errorf("bootstrap: delete workflow event trigger %q for plugin %q: %w", prev.TriggerID, prev.PluginName, err)
				}
			}
			if err := store.Put(ctx, desiredEntry.state.record()); err != nil {
				return fmt.Errorf("bootstrap: store workflow event trigger state %q: %w", rowID, err)
			}
		}
	}
	return nil
}

func desiredWorkflowConfigEventTriggers(cfg *config.Config) (map[string]desiredWorkflowConfigEventTrigger, error) {
	desired := make(map[string]desiredWorkflowConfigEventTrigger)
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
		for _, triggerKey := range slices.Sorted(maps.Keys(effective.EventTriggers)) {
			rowID := workflowConfigEventTriggerStateID(pluginName, triggerKey)
			desired[rowID] = desiredWorkflowConfigEventTrigger{
				state: workflowConfigEventTriggerState{
					ID:           rowID,
					PluginName:   pluginName,
					TriggerKey:   triggerKey,
					ProviderName: effective.ProviderName,
					TriggerID:    workflowConfigEventTriggerID(pluginName, triggerKey),
					UpdatedAt:    now,
				},
				trigger: effective.EventTriggers[triggerKey],
			}
		}
	}
	return desired, nil
}

func loadWorkflowConfigEventTriggerState(ctx context.Context, store indexeddb.ObjectStore) (map[string]workflowConfigEventTriggerState, error) {
	recs, err := store.GetAll(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: load workflow event trigger state: %w", err)
	}
	state := make(map[string]workflowConfigEventTriggerState, len(recs))
	for _, rec := range recs {
		entry := workflowConfigEventTriggerStateFromRecord(rec)
		state[entry.ID] = entry
	}
	return state, nil
}

func (s workflowConfigEventTriggerState) record() indexeddb.Record {
	return indexeddb.Record{
		"id":            s.ID,
		"plugin_name":   s.PluginName,
		"trigger_key":   s.TriggerKey,
		"provider_name": s.ProviderName,
		"trigger_id":    s.TriggerID,
		"updated_at":    s.UpdatedAt,
	}
}

func workflowConfigEventTriggerStateFromRecord(rec indexeddb.Record) workflowConfigEventTriggerState {
	return workflowConfigEventTriggerState{
		ID:           workflowConfigScheduleRecordString(rec, "id"),
		PluginName:   workflowConfigScheduleRecordString(rec, "plugin_name"),
		TriggerKey:   workflowConfigScheduleRecordString(rec, "trigger_key"),
		ProviderName: workflowConfigScheduleRecordString(rec, "provider_name"),
		TriggerID:    workflowConfigScheduleRecordString(rec, "trigger_id"),
		UpdatedAt:    workflowConfigScheduleRecordTime(rec, "updated_at"),
	}
}

func isAdoptableWorkflowEventTrigger(existing *coreworkflow.EventTrigger, pluginName string, trigger config.PluginWorkflowEventTrigger, triggerID string) bool {
	if existing == nil {
		return false
	}
	return existing.ID == triggerID &&
		existing.Match == workflowConfigEventTriggerMatch(trigger) &&
		existing.Paused == trigger.Paused &&
		existing.Target.PluginName == pluginName &&
		existing.Target.Operation == trigger.Operation &&
		workflowTargetInputsEqual(existing.Target.Input, trigger.Input)
}

func isWorkflowConfigOwnedEventTrigger(existing *coreworkflow.EventTrigger, pluginName, triggerID string) bool {
	if existing == nil {
		return false
	}
	actor := workflowConfigActor(pluginName)
	return existing.ID == triggerID &&
		existing.Target.PluginName == pluginName &&
		existing.CreatedBy.SubjectID == actor.SubjectID &&
		existing.CreatedBy.SubjectKind == actor.SubjectKind &&
		existing.CreatedBy.AuthSource == actor.AuthSource
}

func workflowConfigEventTriggerMatch(trigger config.PluginWorkflowEventTrigger) coreworkflow.EventMatch {
	return coreworkflow.EventMatch{
		Type:    trigger.Match.Type,
		Source:  trigger.Match.Source,
		Subject: trigger.Match.Subject,
	}
}

func workflowConfigEventTriggerTarget(pluginName string, trigger config.PluginWorkflowEventTrigger) coreworkflow.Target {
	return coreworkflow.Target{
		PluginName: pluginName,
		Operation:  trigger.Operation,
		Input:      maps.Clone(trigger.Input),
	}
}

func workflowConfigEventTriggerStateID(pluginName, triggerKey string) string {
	return pluginName + "\x00event_trigger\x00" + triggerKey
}

func workflowConfigEventTriggerID(pluginName, triggerKey string) string {
	sum := sha256.Sum256([]byte(pluginName + "\x00event_trigger\x00" + triggerKey))
	return coreworkflow.ConfigManagedSchedulePrefix + hex.EncodeToString(sum[:])
}
