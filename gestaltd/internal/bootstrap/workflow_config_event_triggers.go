package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
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
		{Name: "execution_ref", Type: indexeddb.TypeString},
		{Name: "updated_at", Type: indexeddb.TypeTime},
	},
}

type workflowConfigEventTriggerState struct {
	ID           string
	PluginName   string
	TriggerKey   string
	ProviderName string
	TriggerID    string
	ExecutionRef string
	UpdatedAt    time.Time
}

type desiredWorkflowConfigEventTrigger struct {
	state   workflowConfigEventTriggerState
	trigger config.WorkflowEventTriggerConfig
}

func reconcileWorkflowConfigEventTriggers(ctx context.Context, cfg *config.Config, runtime *workflowRuntime, db indexeddb.IndexedDB) error {
	if cfg == nil || runtime == nil || db == nil {
		return nil
	}
	executionRefs := coredata.NewWorkflowExecutionRefService(db)
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
		providerCtx := invocation.WithWorkflowContextString(ctx, "plugin", prev.PluginName)
		if err := provider.DeleteEventTrigger(providerCtx, coreworkflow.DeleteEventTriggerRequest{
			TriggerID: prev.TriggerID,
		}); err != nil {
			if isWorkflowObjectNotFound(err) {
				if err := workflowRevokeExecutionRefByID(ctx, executionRefs, prev.ExecutionRef); err != nil {
					return fmt.Errorf("bootstrap: revoke workflow execution ref %q for event trigger %q on plugin %q: %w", prev.ExecutionRef, prev.TriggerID, prev.PluginName, err)
				}
				if err := store.Delete(ctx, rowID); err != nil {
					return fmt.Errorf("bootstrap: delete workflow event trigger state %q: %w", rowID, err)
				}
				continue
			}
			return fmt.Errorf("bootstrap: delete workflow event trigger %q for plugin %q: %w", prev.TriggerID, prev.PluginName, err)
		}
		if err := workflowRevokeExecutionRefByID(ctx, executionRefs, prev.ExecutionRef); err != nil {
			return fmt.Errorf("bootstrap: revoke workflow execution ref %q for event trigger %q on plugin %q: %w", prev.ExecutionRef, prev.TriggerID, prev.PluginName, err)
		}
		if err := store.Delete(ctx, rowID); err != nil {
			return fmt.Errorf("bootstrap: delete workflow event trigger state %q: %w", rowID, err)
		}
	}

	for _, triggerKey := range slices.Sorted(maps.Keys(cfg.Workflows.EventTriggers)) {
		trigger := cfg.Workflows.EventTriggers[triggerKey]
		pluginName := trigger.Plugin
		providerName, provider, err := runtime.ResolveProviderSelection(trigger.Provider)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow event trigger %q for plugin %q: %w", triggerKey, pluginName, err)
		}
		rowID := workflowConfigEventTriggerStateID(triggerKey)
		desiredEntry := desired[rowID]
		prev, owned := previous[rowID]
		existingExecutionRef := ""
		providerCtx := invocation.WithWorkflowContextString(ctx, "plugin", pluginName)
		existing, err := provider.GetEventTrigger(providerCtx, coreworkflow.GetEventTriggerRequest{
			TriggerID: desiredEntry.state.TriggerID,
		})
		switch {
		case err == nil:
			if !isWorkflowConfigOwnedEventTrigger(existing, pluginName, desiredEntry.state.TriggerID) &&
				!isAdoptableWorkflowEventTrigger(existing, trigger, desiredEntry.state.TriggerID) {
				return fmt.Errorf("bootstrap: workflow event trigger %q for plugin %q conflicts with existing unmanaged trigger id %q", triggerKey, pluginName, desiredEntry.state.TriggerID)
			}
			existingExecutionRef = workflowEventTriggerExecutionRef(existing, prev.ExecutionRef)
		case isWorkflowObjectNotFound(err):
			if owned {
				existingExecutionRef = strings.TrimSpace(prev.ExecutionRef)
			}
		default:
			return fmt.Errorf("bootstrap: get workflow event trigger %q for plugin %q: %w", desiredEntry.state.TriggerID, pluginName, err)
		}
		desiredExecutionRef, err := workflowConfigExecutionReference(cfg, providerName, workflowConfigEventTriggerTarget(trigger))
		if err != nil {
			return fmt.Errorf("bootstrap: workflow event trigger %q for plugin %q: %w", triggerKey, pluginName, err)
		}
		executionRefID, createdExecutionRef, err := workflowEnsureConfigExecutionRef(
			ctx,
			executionRefs,
			desiredExecutionRef,
			workflowConfigEventTriggerExecutionRefID(desiredEntry.state.TriggerID),
			existingExecutionRef,
		)
		if err != nil {
			return fmt.Errorf("bootstrap: store workflow execution ref for event trigger %q on plugin %q: %w", triggerKey, pluginName, err)
		}
		if _, err := provider.UpsertEventTrigger(providerCtx, coreworkflow.UpsertEventTriggerRequest{
			TriggerID:    desiredEntry.state.TriggerID,
			Match:        workflowConfigEventTriggerMatch(trigger),
			Target:       workflowConfigEventTriggerTarget(trigger),
			Paused:       trigger.Paused,
			RequestedBy:  workflowConfigActor(),
			ExecutionRef: executionRefID,
		}); err != nil {
			if createdExecutionRef {
				_ = workflowRevokeExecutionRefByID(ctx, executionRefs, executionRefID)
			}
			return fmt.Errorf("bootstrap: workflow event trigger %q for plugin %q: %w", triggerKey, pluginName, err)
		}
		if existingExecutionRef != executionRefID {
			if err := workflowRevokeExecutionRefByID(ctx, executionRefs, existingExecutionRef); err != nil {
				return fmt.Errorf("bootstrap: revoke workflow execution ref %q for event trigger %q on plugin %q: %w", existingExecutionRef, desiredEntry.state.TriggerID, pluginName, err)
			}
		}
		if owned && prev.ProviderName != desiredEntry.state.ProviderName {
			oldProvider, err := runtime.ResolveProvider(prev.ProviderName)
			if err != nil {
				return fmt.Errorf("bootstrap: cleanup workflow event trigger %q for plugin %q requires provider %q: %w", prev.TriggerID, prev.PluginName, prev.ProviderName, err)
			}
			oldProviderCtx := invocation.WithWorkflowContextString(ctx, "plugin", prev.PluginName)
			oldExisting, err := oldProvider.GetEventTrigger(oldProviderCtx, coreworkflow.GetEventTriggerRequest{
				TriggerID: prev.TriggerID,
			})
			oldExecutionRef := ""
			switch {
			case err == nil:
				oldExecutionRef = workflowEventTriggerExecutionRef(oldExisting, prev.ExecutionRef)
			case isWorkflowObjectNotFound(err):
				oldExecutionRef = strings.TrimSpace(prev.ExecutionRef)
			default:
				return fmt.Errorf("bootstrap: get workflow event trigger %q for plugin %q: %w", prev.TriggerID, prev.PluginName, err)
			}
			if err := oldProvider.DeleteEventTrigger(oldProviderCtx, coreworkflow.DeleteEventTriggerRequest{
				TriggerID: prev.TriggerID,
			}); err != nil && !isWorkflowObjectNotFound(err) {
				return fmt.Errorf("bootstrap: delete workflow event trigger %q for plugin %q: %w", prev.TriggerID, prev.PluginName, err)
			}
			if err := workflowRevokeExecutionRefByID(ctx, executionRefs, oldExecutionRef); err != nil {
				return fmt.Errorf("bootstrap: revoke workflow execution ref %q for event trigger %q on plugin %q: %w", oldExecutionRef, prev.TriggerID, prev.PluginName, err)
			}
		}
		desiredEntry.state.ExecutionRef = executionRefID
		desiredEntry.state.ProviderName = providerName
		if err := store.Put(ctx, desiredEntry.state.record()); err != nil {
			return fmt.Errorf("bootstrap: store workflow event trigger state %q: %w", rowID, err)
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
	for _, triggerKey := range slices.Sorted(maps.Keys(cfg.Workflows.EventTriggers)) {
		trigger := cfg.Workflows.EventTriggers[triggerKey]
		providerName, _, err := cfg.EffectiveWorkflowProvider(trigger.Provider)
		if err != nil {
			return nil, err
		}
		rowID := workflowConfigEventTriggerStateID(triggerKey)
		desired[rowID] = desiredWorkflowConfigEventTrigger{
			state: workflowConfigEventTriggerState{
				ID:           rowID,
				PluginName:   trigger.Plugin,
				TriggerKey:   triggerKey,
				ProviderName: providerName,
				TriggerID:    workflowConfigEventTriggerID(triggerKey),
				UpdatedAt:    now,
			},
			trigger: trigger,
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
		"execution_ref": s.ExecutionRef,
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
		ExecutionRef: workflowConfigScheduleRecordString(rec, "execution_ref"),
		UpdatedAt:    workflowConfigScheduleRecordTime(rec, "updated_at"),
	}
}

func isAdoptableWorkflowEventTrigger(existing *coreworkflow.EventTrigger, trigger config.WorkflowEventTriggerConfig, triggerID string) bool {
	if existing == nil {
		return false
	}
	return existing.ID == triggerID &&
		existing.Match == workflowConfigEventTriggerMatch(trigger) &&
		existing.Paused == trigger.Paused &&
		existing.Target.PluginName == trigger.Plugin &&
		existing.Target.Operation == trigger.Operation &&
		existing.Target.Connection == trigger.Connection &&
		existing.Target.Instance == trigger.Instance &&
		workflowTargetInputsEqual(existing.Target.Input, trigger.Input)
}

func isWorkflowConfigOwnedEventTrigger(existing *coreworkflow.EventTrigger, pluginName, triggerID string) bool {
	if existing == nil {
		return false
	}
	actor := workflowConfigActor()
	return existing.ID == triggerID &&
		existing.Target.PluginName == pluginName &&
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

func workflowConfigEventTriggerTarget(trigger config.WorkflowEventTriggerConfig) coreworkflow.Target {
	return coreworkflow.Target{
		PluginName: trigger.Plugin,
		Operation:  trigger.Operation,
		Connection: trigger.Connection,
		Instance:   trigger.Instance,
		Input:      maps.Clone(trigger.Input),
	}
}

func workflowConfigEventTriggerStateID(triggerKey string) string {
	return strings.TrimSpace(triggerKey)
}

func workflowConfigEventTriggerID(triggerKey string) string {
	sum := sha256.Sum256([]byte("event_trigger\x00" + strings.TrimSpace(triggerKey)))
	return coreworkflow.ConfigManagedSchedulePrefix + hex.EncodeToString(sum[:])
}

func workflowConfigEventTriggerExecutionRefID(triggerID string) string {
	return "workflow_event_trigger:" + strings.TrimSpace(triggerID) + ":" + uuid.NewString()
}

func workflowEventTriggerExecutionRef(existing *coreworkflow.EventTrigger, fallback string) string {
	if existing != nil {
		if refID := strings.TrimSpace(existing.ExecutionRef); refID != "" {
			return refID
		}
	}
	return strings.TrimSpace(fallback)
}
