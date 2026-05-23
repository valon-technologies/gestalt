package workflowmanager

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowprincipal"
)

func (m *Manager) ListSchedules(ctx context.Context, p *principal.Principal) ([]*ManagedSchedule, error) {
	refs, err := m.listOwnedExecutionRefs(ctx, p, true)
	if err != nil {
		return nil, err
	}
	out := make([]*ManagedSchedule, 0, len(refs))
	for _, ref := range refs {
		if !m.allowTarget(ctx, p, ref.Target) {
			continue
		}
		scheduleID := scheduleIDFromExecutionRefID(ref.ID)
		if scheduleID == "" {
			continue
		}
		provider, err := m.resolveProviderByName(strings.TrimSpace(ref.ProviderName))
		if err != nil {
			return nil, err
		}
		schedule, err := provider.GetSchedule(ctx, coreworkflow.GetScheduleRequest{ScheduleID: scheduleID})
		if err != nil {
			if isWorkflowProviderNotFound(err) {
				continue
			}
			return nil, err
		}
		if !scheduleMatchesExecutionRef(ref.ProviderName, schedule, ref) {
			continue
		}
		out = append(out, &ManagedSchedule{
			ProviderName: strings.TrimSpace(ref.ProviderName),
			Schedule:     schedule,
			ExecutionRef: ref,
			provider:     provider,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		left := out[i]
		right := out[j]
		if left.Schedule != nil && right.Schedule != nil && left.Schedule.CreatedAt != nil && right.Schedule.CreatedAt != nil && !left.Schedule.CreatedAt.Equal(*right.Schedule.CreatedAt) {
			return left.Schedule.CreatedAt.Before(*right.Schedule.CreatedAt)
		}
		leftID := ""
		rightID := ""
		if left.Schedule != nil {
			leftID = left.Schedule.ID
		}
		if right.Schedule != nil {
			rightID = right.Schedule.ID
		}
		return leftID < rightID
	})
	return out, nil
}

func (m *Manager) CreateSchedule(ctx context.Context, p *principal.Principal, req ScheduleUpsert) (out *ManagedSchedule, err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationScheduleCreate)
	audit.setCallerApp(req.CallerAppName)
	defer func() {
		if out != nil && out.Schedule != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetSchedule, out.Schedule.ID, "")
			if out.ExecutionRef != nil {
				audit.setWorkflowTarget(out.ExecutionRef.Target)
			}
		}
		audit.finish(ctx, err)
	}()
	if strings.TrimSpace(principalSubjectID(p)) == "" {
		return nil, ErrWorkflowSubjectRequired
	}

	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	idempotencyScope := workflowCreateIdempotencyScope(p, req.CallerAppName, idempotencyKey)
	scheduleID := newScheduleID(idempotencyScope)
	audit.setObjectTarget(workflowAuditTargetSchedule, scheduleID, "")
	var existing *ManagedSchedule
	if idempotencyKey != "" {
		var err error
		existing, err = m.requireOwnedSchedule(ctx, scheduleID, p)
		if err == nil {
			audit.setProvider(existing.ProviderName)
			if strings.TrimSpace(req.DefinitionID) != "" {
				if workflowTargetIsSet(req.Target) {
					return nil, fmt.Errorf("%w: workflow request must set either target or definition_id, not both", invocation.ErrInvalidInvocation)
				}
				if err := m.validateExistingProviderSelection(req.ProviderName, existing.ProviderName); err != nil {
					return nil, err
				}
				if !managedScheduleMatchesDefinitionUpsert(existing, req) {
					return nil, fmt.Errorf("%w: workflow schedule idempotency key reused with different request", invocation.ErrInvalidInvocation)
				}
				return existing, nil
			}
		} else if !errors.Is(err, core.ErrNotFound) {
			return nil, err
		}
	}

	providerName, provider, target, err := m.resolveRequestProviderTarget(ctx, p, req.ProviderName, req.Target, req.DefinitionID, req.CallerAppName)
	if err != nil {
		return nil, err
	}
	audit.setProvider(providerName)
	audit.setWorkflowTarget(target)
	if existing != nil {
		if !managedScheduleMatchesUpsert(existing, providerName, target, req) {
			return nil, fmt.Errorf("%w: workflow schedule idempotency key reused with different request", invocation.ErrInvalidInvocation)
		}
		return existing, nil
	}
	executionRefID := newScheduleExecutionRefID(scheduleID, idempotencyScope)
	ref, err := m.putExecutionRefWithPermissions(ctx, executionRefID, providerName, provider, target, p, req.CallerAppName, scheduleUpsertSourceDefinitionID(req), req.Permissions)
	if err != nil {
		return nil, err
	}
	schedule, err := provider.UpsertSchedule(ctx, coreworkflow.UpsertScheduleRequest{
		ScheduleID:   scheduleID,
		Cron:         strings.TrimSpace(req.Cron),
		Timezone:     strings.TrimSpace(req.Timezone),
		Target:       target,
		Paused:       req.Paused,
		RequestedBy:  workflowActorFromPrincipal(p),
		ExecutionRef: executionRefID,
	})
	if err != nil {
		m.revokeExecutionRef(ctx, ref)
		return nil, err
	}
	return &ManagedSchedule{
		ProviderName: providerName,
		Schedule:     schedule,
		ExecutionRef: ref,
		provider:     provider,
	}, nil
}

func managedScheduleMatchesUpsert(existing *ManagedSchedule, providerName string, target coreworkflow.Target, req ScheduleUpsert) bool {
	if existing == nil || existing.Schedule == nil {
		return false
	}
	if strings.TrimSpace(existing.ProviderName) != strings.TrimSpace(providerName) {
		return false
	}
	if strings.TrimSpace(existing.Schedule.Cron) != strings.TrimSpace(req.Cron) {
		return false
	}
	if strings.TrimSpace(existing.Schedule.Timezone) != strings.TrimSpace(req.Timezone) {
		return false
	}
	if existing.Schedule.Paused != req.Paused {
		return false
	}
	return coreworkflow.TargetsEqual(existing.Schedule.Target, target)
}

func managedScheduleMatchesDefinitionUpsert(existing *ManagedSchedule, req ScheduleUpsert) bool {
	if existing == nil || existing.Schedule == nil || existing.ExecutionRef == nil {
		return false
	}
	if strings.TrimSpace(existing.ExecutionRef.SourceDefinitionID) != strings.TrimSpace(req.DefinitionID) {
		return false
	}
	if strings.TrimSpace(existing.Schedule.Cron) != strings.TrimSpace(req.Cron) {
		return false
	}
	if strings.TrimSpace(existing.Schedule.Timezone) != strings.TrimSpace(req.Timezone) {
		return false
	}
	return existing.Schedule.Paused == req.Paused
}

func scheduleUpsertSourceDefinitionID(req ScheduleUpsert) string {
	if definitionID := strings.TrimSpace(req.DefinitionID); definitionID != "" {
		return definitionID
	}
	return strings.TrimSpace(req.SourceDefinitionID)
}

func (m *Manager) GetSchedule(ctx context.Context, p *principal.Principal, scheduleID string) (*ManagedSchedule, error) {
	return m.requireOwnedSchedule(ctx, scheduleID, p)
}

func (m *Manager) UpdateSchedule(ctx context.Context, p *principal.Principal, scheduleID string, req ScheduleUpsert) (out *ManagedSchedule, err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationScheduleUpdate)
	audit.setCallerApp(req.CallerAppName)
	audit.setObjectTarget(workflowAuditTargetSchedule, scheduleID, "")
	defer func() {
		if out != nil && out.Schedule != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetSchedule, out.Schedule.ID, "")
			if out.ExecutionRef != nil {
				audit.setWorkflowTarget(out.ExecutionRef.Target)
			}
		}
		audit.finish(ctx, err)
	}()
	if strings.TrimSpace(principalSubjectID(p)) == "" {
		return nil, ErrWorkflowSubjectRequired
	}
	existing, err := m.requireOwnedSchedule(ctx, scheduleID, p)
	if err != nil {
		return nil, err
	}
	audit.setProvider(existing.ProviderName)
	nextProviderName, nextProvider, target, err := m.resolveRequestProviderTarget(ctx, p, req.ProviderName, req.Target, req.DefinitionID, req.CallerAppName)
	if err != nil {
		return nil, err
	}
	audit.setProvider(nextProviderName)
	audit.setWorkflowTarget(target)

	executionRefID := scheduleExecutionRefID(strings.TrimSpace(existing.Schedule.ID))
	nextRef, err := m.putExecutionRefWithPermissions(ctx, executionRefID, nextProviderName, nextProvider, target, p, req.CallerAppName, scheduleUpsertSourceDefinitionID(req), req.Permissions)
	if err != nil {
		return nil, err
	}
	schedule, err := nextProvider.UpsertSchedule(ctx, coreworkflow.UpsertScheduleRequest{
		ScheduleID:   strings.TrimSpace(existing.Schedule.ID),
		Cron:         strings.TrimSpace(req.Cron),
		Timezone:     strings.TrimSpace(req.Timezone),
		Target:       target,
		Paused:       req.Paused,
		RequestedBy:  workflowActorFromPrincipal(p),
		ExecutionRef: executionRefID,
	})
	if err != nil {
		m.revokeExecutionRef(ctx, nextRef)
		return nil, err
	}
	if strings.TrimSpace(existing.ProviderName) != nextProviderName {
		if err := existingProvider(existing).DeleteSchedule(ctx, coreworkflow.DeleteScheduleRequest{
			ScheduleID: strings.TrimSpace(existing.Schedule.ID),
		}); err != nil {
			_ = nextProvider.DeleteSchedule(ctx, coreworkflow.DeleteScheduleRequest{
				ScheduleID: strings.TrimSpace(existing.Schedule.ID),
			})
			m.revokeExecutionRef(ctx, nextRef)
			return nil, err
		}
	}
	if existing.ExecutionRef != nil && existing.ExecutionRef.ID != "" && existing.ExecutionRef.ID != executionRefID {
		m.revokeExecutionRef(ctx, existing.ExecutionRef)
	}
	return &ManagedSchedule{
		ProviderName: nextProviderName,
		Schedule:     schedule,
		ExecutionRef: nextRef,
		provider:     nextProvider,
	}, nil
}

func (m *Manager) DeleteSchedule(ctx context.Context, p *principal.Principal, scheduleID string) (err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationScheduleDelete)
	audit.setObjectTarget(workflowAuditTargetSchedule, scheduleID, "")
	defer func() {
		audit.finish(ctx, err)
	}()
	value, err := m.requireOwnedSchedule(ctx, scheduleID, p)
	if err != nil {
		return err
	}
	audit.setProvider(value.ProviderName)
	if value.ExecutionRef != nil {
		audit.setWorkflowTarget(value.ExecutionRef.Target)
	}
	if err := existingProvider(value).DeleteSchedule(ctx, coreworkflow.DeleteScheduleRequest{
		ScheduleID: strings.TrimSpace(value.Schedule.ID),
	}); err != nil {
		return err
	}
	m.revokeExecutionRef(ctx, value.ExecutionRef)
	return nil
}

func (m *Manager) PauseSchedule(ctx context.Context, p *principal.Principal, scheduleID string) (out *ManagedSchedule, err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationSchedulePause)
	audit.setObjectTarget(workflowAuditTargetSchedule, scheduleID, "")
	defer func() {
		if out != nil && out.Schedule != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetSchedule, out.Schedule.ID, "")
			if out.ExecutionRef != nil {
				audit.setWorkflowTarget(out.ExecutionRef.Target)
			}
		}
		audit.finish(ctx, err)
	}()
	value, err := m.requireOwnedSchedule(ctx, scheduleID, p)
	if err != nil {
		return nil, err
	}
	audit.setProvider(value.ProviderName)
	if value.ExecutionRef != nil {
		audit.setWorkflowTarget(value.ExecutionRef.Target)
	}
	schedule, err := existingProvider(value).PauseSchedule(ctx, coreworkflow.PauseScheduleRequest{
		ScheduleID: strings.TrimSpace(value.Schedule.ID),
	})
	if err != nil {
		return nil, err
	}
	value.Schedule = schedule
	return value, nil
}

func (m *Manager) ResumeSchedule(ctx context.Context, p *principal.Principal, scheduleID string) (out *ManagedSchedule, err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationScheduleResume)
	audit.setObjectTarget(workflowAuditTargetSchedule, scheduleID, "")
	defer func() {
		if out != nil && out.Schedule != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetSchedule, out.Schedule.ID, "")
			if out.ExecutionRef != nil {
				audit.setWorkflowTarget(out.ExecutionRef.Target)
			}
		}
		audit.finish(ctx, err)
	}()
	value, err := m.requireOwnedSchedule(ctx, scheduleID, p)
	if err != nil {
		return nil, err
	}
	audit.setProvider(value.ProviderName)
	if value.ExecutionRef != nil {
		audit.setWorkflowTarget(value.ExecutionRef.Target)
	}
	schedule, err := existingProvider(value).ResumeSchedule(ctx, coreworkflow.ResumeScheduleRequest{
		ScheduleID: strings.TrimSpace(value.Schedule.ID),
	})
	if err != nil {
		return nil, err
	}
	value.Schedule = schedule
	return value, nil
}

func (m *Manager) ListEventTriggers(ctx context.Context, p *principal.Principal) ([]*ManagedEventTrigger, error) {
	refs, err := m.listOwnedExecutionRefs(ctx, p, true)
	if err != nil {
		return nil, err
	}
	out := make([]*ManagedEventTrigger, 0, len(refs))
	for _, ref := range refs {
		if !m.allowTarget(ctx, p, ref.Target) {
			continue
		}
		triggerID := eventTriggerIDFromExecutionRefID(ref.ID)
		if triggerID == "" {
			continue
		}
		provider, err := m.resolveProviderByName(strings.TrimSpace(ref.ProviderName))
		if err != nil {
			return nil, err
		}
		trigger, err := provider.GetEventTrigger(ctx, coreworkflow.GetEventTriggerRequest{TriggerID: triggerID})
		if err != nil {
			if isWorkflowProviderNotFound(err) {
				continue
			}
			return nil, err
		}
		if !eventTriggerMatchesExecutionRef(ref.ProviderName, trigger, ref) {
			continue
		}
		out = append(out, &ManagedEventTrigger{
			ProviderName: strings.TrimSpace(ref.ProviderName),
			Trigger:      trigger,
			ExecutionRef: ref,
			provider:     provider,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		left := out[i]
		right := out[j]
		if left.Trigger != nil && right.Trigger != nil && left.Trigger.CreatedAt != nil && right.Trigger.CreatedAt != nil && !left.Trigger.CreatedAt.Equal(*right.Trigger.CreatedAt) {
			return left.Trigger.CreatedAt.Before(*right.Trigger.CreatedAt)
		}
		leftID := ""
		rightID := ""
		if left.Trigger != nil {
			leftID = left.Trigger.ID
		}
		if right.Trigger != nil {
			rightID = right.Trigger.ID
		}
		return leftID < rightID
	})
	return out, nil
}

func (m *Manager) CreateEventTrigger(ctx context.Context, p *principal.Principal, req EventTriggerUpsert) (out *ManagedEventTrigger, err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationEventTriggerCreate)
	audit.setCallerApp(req.CallerAppName)
	defer func() {
		if out != nil && out.Trigger != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetEventTrigger, out.Trigger.ID, "")
			if out.ExecutionRef != nil {
				audit.setWorkflowTarget(out.ExecutionRef.Target)
			}
		}
		audit.finish(ctx, err)
	}()
	if strings.TrimSpace(principalSubjectID(p)) == "" {
		return nil, ErrWorkflowSubjectRequired
	}
	match := normalizeEventMatch(req.Match)
	if strings.TrimSpace(match.Type) == "" {
		return nil, ErrWorkflowEventMatchRequired
	}

	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	idempotencyScope := workflowCreateIdempotencyScope(p, req.CallerAppName, idempotencyKey)
	triggerID := newEventTriggerID(idempotencyScope)
	audit.setObjectTarget(workflowAuditTargetEventTrigger, triggerID, "")
	var existing *ManagedEventTrigger
	if idempotencyKey != "" {
		var err error
		existing, err = m.requireOwnedEventTrigger(ctx, triggerID, p)
		if err == nil {
			audit.setProvider(existing.ProviderName)
			if strings.TrimSpace(req.DefinitionID) != "" {
				if workflowTargetIsSet(req.Target) {
					return nil, fmt.Errorf("%w: workflow request must set either target or definition_id, not both", invocation.ErrInvalidInvocation)
				}
				if err := m.validateExistingProviderSelection(req.ProviderName, existing.ProviderName); err != nil {
					return nil, err
				}
				if !managedEventTriggerMatchesDefinitionUpsert(existing, match, req) {
					return nil, fmt.Errorf("%w: workflow trigger idempotency key reused with different request", invocation.ErrInvalidInvocation)
				}
				return existing, nil
			}
		} else if !errors.Is(err, core.ErrNotFound) {
			return nil, err
		}
	}

	providerName, provider, target, err := m.resolveRequestProviderTarget(ctx, p, req.ProviderName, req.Target, req.DefinitionID, req.CallerAppName)
	if err != nil {
		return nil, err
	}
	audit.setProvider(providerName)
	audit.setWorkflowTarget(target)
	if existing != nil {
		if !managedEventTriggerMatchesUpsert(existing, providerName, target, match, req) {
			return nil, fmt.Errorf("%w: workflow trigger idempotency key reused with different request", invocation.ErrInvalidInvocation)
		}
		return existing, nil
	}
	executionRefID := newEventTriggerExecutionRefID(triggerID, idempotencyScope)
	ref, err := m.putExecutionRef(ctx, executionRefID, providerName, provider, target, p, req.CallerAppName, req.DefinitionID)
	if err != nil {
		return nil, err
	}
	trigger, err := provider.UpsertEventTrigger(ctx, coreworkflow.UpsertEventTriggerRequest{
		TriggerID:    triggerID,
		Match:        match,
		Target:       target,
		Paused:       req.Paused,
		RequestedBy:  workflowActorFromPrincipal(p),
		ExecutionRef: executionRefID,
	})
	if err != nil {
		m.revokeExecutionRef(ctx, ref)
		return nil, err
	}
	return &ManagedEventTrigger{
		ProviderName: providerName,
		Trigger:      trigger,
		ExecutionRef: ref,
		provider:     provider,
	}, nil
}

func managedEventTriggerMatchesUpsert(existing *ManagedEventTrigger, providerName string, target coreworkflow.Target, match coreworkflow.EventMatch, req EventTriggerUpsert) bool {
	if existing == nil || existing.Trigger == nil {
		return false
	}
	if strings.TrimSpace(existing.ProviderName) != strings.TrimSpace(providerName) {
		return false
	}
	if existing.Trigger.Paused != req.Paused {
		return false
	}
	if normalizeEventMatch(existing.Trigger.Match) != match {
		return false
	}
	return coreworkflow.TargetsEqual(existing.Trigger.Target, target)
}

func managedEventTriggerMatchesDefinitionUpsert(existing *ManagedEventTrigger, match coreworkflow.EventMatch, req EventTriggerUpsert) bool {
	if existing == nil || existing.Trigger == nil || existing.ExecutionRef == nil {
		return false
	}
	if strings.TrimSpace(existing.ExecutionRef.SourceDefinitionID) != strings.TrimSpace(req.DefinitionID) {
		return false
	}
	if existing.Trigger.Paused != req.Paused {
		return false
	}
	return normalizeEventMatch(existing.Trigger.Match) == match
}

func (m *Manager) GetEventTrigger(ctx context.Context, p *principal.Principal, triggerID string) (*ManagedEventTrigger, error) {
	return m.requireOwnedEventTrigger(ctx, triggerID, p)
}

func (m *Manager) UpdateEventTrigger(ctx context.Context, p *principal.Principal, triggerID string, req EventTriggerUpsert) (out *ManagedEventTrigger, err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationEventTriggerUpdate)
	audit.setCallerApp(req.CallerAppName)
	audit.setObjectTarget(workflowAuditTargetEventTrigger, triggerID, "")
	defer func() {
		if out != nil && out.Trigger != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetEventTrigger, out.Trigger.ID, "")
			if out.ExecutionRef != nil {
				audit.setWorkflowTarget(out.ExecutionRef.Target)
			}
		}
		audit.finish(ctx, err)
	}()
	if strings.TrimSpace(principalSubjectID(p)) == "" {
		return nil, ErrWorkflowSubjectRequired
	}
	existing, err := m.requireOwnedEventTrigger(ctx, triggerID, p)
	if err != nil {
		return nil, err
	}
	audit.setProvider(existing.ProviderName)
	nextProviderName, nextProvider, target, err := m.resolveRequestProviderTarget(ctx, p, req.ProviderName, req.Target, req.DefinitionID, req.CallerAppName)
	if err != nil {
		return nil, err
	}
	audit.setProvider(nextProviderName)
	audit.setWorkflowTarget(target)
	match := normalizeEventMatch(req.Match)
	if strings.TrimSpace(match.Type) == "" {
		return nil, ErrWorkflowEventMatchRequired
	}

	executionRefID := eventTriggerExecutionRefID(strings.TrimSpace(existing.Trigger.ID))
	nextRef, err := m.putExecutionRef(ctx, executionRefID, nextProviderName, nextProvider, target, p, req.CallerAppName, req.DefinitionID)
	if err != nil {
		return nil, err
	}
	trigger, err := nextProvider.UpsertEventTrigger(ctx, coreworkflow.UpsertEventTriggerRequest{
		TriggerID:    strings.TrimSpace(existing.Trigger.ID),
		Match:        match,
		Target:       target,
		Paused:       req.Paused,
		RequestedBy:  workflowActorFromPrincipal(p),
		ExecutionRef: executionRefID,
	})
	if err != nil {
		m.revokeExecutionRef(ctx, nextRef)
		return nil, err
	}
	if strings.TrimSpace(existing.ProviderName) != nextProviderName {
		if err := existingEventTriggerProvider(existing).DeleteEventTrigger(ctx, coreworkflow.DeleteEventTriggerRequest{
			TriggerID: strings.TrimSpace(existing.Trigger.ID),
		}); err != nil {
			_ = nextProvider.DeleteEventTrigger(ctx, coreworkflow.DeleteEventTriggerRequest{
				TriggerID: strings.TrimSpace(existing.Trigger.ID),
			})
			m.revokeExecutionRef(ctx, nextRef)
			return nil, err
		}
	}
	if existing.ExecutionRef != nil && existing.ExecutionRef.ID != "" && existing.ExecutionRef.ID != executionRefID {
		m.revokeExecutionRef(ctx, existing.ExecutionRef)
	}
	return &ManagedEventTrigger{
		ProviderName: nextProviderName,
		Trigger:      trigger,
		ExecutionRef: nextRef,
		provider:     nextProvider,
	}, nil
}

func (m *Manager) DeleteEventTrigger(ctx context.Context, p *principal.Principal, triggerID string) (err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationEventTriggerDelete)
	audit.setObjectTarget(workflowAuditTargetEventTrigger, triggerID, "")
	defer func() {
		audit.finish(ctx, err)
	}()
	value, err := m.requireOwnedEventTrigger(ctx, triggerID, p)
	if err != nil {
		return err
	}
	audit.setProvider(value.ProviderName)
	if value.ExecutionRef != nil {
		audit.setWorkflowTarget(value.ExecutionRef.Target)
	}
	if err := existingEventTriggerProvider(value).DeleteEventTrigger(ctx, coreworkflow.DeleteEventTriggerRequest{
		TriggerID: strings.TrimSpace(value.Trigger.ID),
	}); err != nil {
		return err
	}
	m.revokeExecutionRef(ctx, value.ExecutionRef)
	return nil
}

func (m *Manager) PauseEventTrigger(ctx context.Context, p *principal.Principal, triggerID string) (out *ManagedEventTrigger, err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationEventTriggerPause)
	audit.setObjectTarget(workflowAuditTargetEventTrigger, triggerID, "")
	defer func() {
		if out != nil && out.Trigger != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetEventTrigger, out.Trigger.ID, "")
			if out.ExecutionRef != nil {
				audit.setWorkflowTarget(out.ExecutionRef.Target)
			}
		}
		audit.finish(ctx, err)
	}()
	value, err := m.requireOwnedEventTrigger(ctx, triggerID, p)
	if err != nil {
		return nil, err
	}
	audit.setProvider(value.ProviderName)
	if value.ExecutionRef != nil {
		audit.setWorkflowTarget(value.ExecutionRef.Target)
	}
	trigger, err := existingEventTriggerProvider(value).PauseEventTrigger(ctx, coreworkflow.PauseEventTriggerRequest{
		TriggerID: strings.TrimSpace(value.Trigger.ID),
	})
	if err != nil {
		return nil, err
	}
	value.Trigger = trigger
	return value, nil
}

func (m *Manager) ResumeEventTrigger(ctx context.Context, p *principal.Principal, triggerID string) (out *ManagedEventTrigger, err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationEventTriggerResume)
	audit.setObjectTarget(workflowAuditTargetEventTrigger, triggerID, "")
	defer func() {
		if out != nil && out.Trigger != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetEventTrigger, out.Trigger.ID, "")
			if out.ExecutionRef != nil {
				audit.setWorkflowTarget(out.ExecutionRef.Target)
			}
		}
		audit.finish(ctx, err)
	}()
	value, err := m.requireOwnedEventTrigger(ctx, triggerID, p)
	if err != nil {
		return nil, err
	}
	audit.setProvider(value.ProviderName)
	if value.ExecutionRef != nil {
		audit.setWorkflowTarget(value.ExecutionRef.Target)
	}
	trigger, err := existingEventTriggerProvider(value).ResumeEventTrigger(ctx, coreworkflow.ResumeEventTriggerRequest{
		TriggerID: strings.TrimSpace(value.Trigger.ID),
	})
	if err != nil {
		return nil, err
	}
	value.Trigger = trigger
	return value, nil
}

func (m *Manager) catalogSelectorConfig() invocation.CatalogSelectorConfig {
	return invocation.CatalogSelectorConfig{
		Invoker:           m.invoker,
		CatalogConnection: m.catalogConnection,
		DefaultConnection: m.defaultConnection,
	}
}

func executionRefOwnedBy(ref *coreworkflow.ExecutionReference, p *principal.Principal) bool {
	if ref == nil || p == nil {
		return false
	}
	subjectID := strings.TrimSpace(principalSubjectID(principal.Canonicalized(p)))
	return subjectID != "" && strings.TrimSpace(ref.SubjectID) == subjectID
}

func executionRefActive(ref *coreworkflow.ExecutionReference) bool {
	return ref != nil && (ref.RevokedAt == nil || ref.RevokedAt.IsZero())
}

func executionRefsByProvider(refs []*coreworkflow.ExecutionReference) map[string][]*coreworkflow.ExecutionReference {
	if len(refs) == 0 {
		return nil
	}
	out := make(map[string][]*coreworkflow.ExecutionReference)
	for _, ref := range refs {
		if ref == nil {
			continue
		}
		providerName := strings.TrimSpace(ref.ProviderName)
		if providerName == "" {
			continue
		}
		out[providerName] = append(out[providerName], ref)
	}
	return out
}

func executionRefsByID(refs []*coreworkflow.ExecutionReference) map[string]*coreworkflow.ExecutionReference {
	if len(refs) == 0 {
		return nil
	}
	out := make(map[string]*coreworkflow.ExecutionReference, len(refs))
	for _, ref := range refs {
		if ref == nil || strings.TrimSpace(ref.ID) == "" {
			continue
		}
		out[strings.TrimSpace(ref.ID)] = ref
	}
	return out
}

func scheduleMatchesExecutionRef(providerName string, schedule *coreworkflow.Schedule, ref *coreworkflow.ExecutionReference) bool {
	if schedule == nil || ref == nil {
		return false
	}
	if providerName = strings.TrimSpace(providerName); providerName != "" && strings.TrimSpace(ref.ProviderName) != providerName {
		return false
	}
	return targetMatchesExecutionRef(schedule.Target, ref)
}

func eventTriggerMatchesExecutionRef(providerName string, trigger *coreworkflow.EventTrigger, ref *coreworkflow.ExecutionReference) bool {
	if trigger == nil || ref == nil {
		return false
	}
	if providerName = strings.TrimSpace(providerName); providerName != "" && strings.TrimSpace(ref.ProviderName) != providerName {
		return false
	}
	return targetMatchesExecutionRef(trigger.Target, ref)
}

func runMatchesExecutionRef(providerName string, run *coreworkflow.Run, ref *coreworkflow.ExecutionReference) bool {
	if run == nil || ref == nil {
		return false
	}
	if providerName = strings.TrimSpace(providerName); providerName != "" && strings.TrimSpace(ref.ProviderName) != providerName {
		return false
	}
	return targetMatchesExecutionRef(run.Target, ref)
}

func targetMatchesExecutionRef(target coreworkflow.Target, ref *coreworkflow.ExecutionReference) bool {
	if ref == nil {
		return false
	}
	return coreworkflow.TargetsEqual(target, ref.Target)
}

func signalOrStartExecutionRefMatches(ref *coreworkflow.ExecutionReference, executionRefID, providerName string, target coreworkflow.Target, p *principal.Principal, callerAppName string, permissions []core.AccessPermission) bool {
	if ref == nil {
		return false
	}
	if strings.TrimSpace(ref.ID) != strings.TrimSpace(executionRefID) {
		return false
	}
	if providerName = strings.TrimSpace(providerName); providerName != "" && strings.TrimSpace(ref.ProviderName) != providerName {
		return false
	}
	if strings.TrimSpace(ref.CallerAppName) != strings.TrimSpace(callerAppName) {
		return false
	}
	if strings.TrimSpace(ref.CredentialSubjectID) != strings.TrimSpace(principal.EffectiveCredentialSubjectID(principal.Canonicalized(p))) {
		return false
	}
	if executionRefPermissionsScope(ref.Permissions) != executionRefPermissionsScope(permissions) {
		return false
	}
	return executionRefOwnedBy(ref, p) && targetMatchesExecutionRef(target, ref)
}

func runExecutionRefMatches(ref *coreworkflow.ExecutionReference, executionRefID, providerName string, target coreworkflow.Target, p *principal.Principal, callerAppName, sourceDefinitionID string, permissions []core.AccessPermission) bool {
	if !signalOrStartExecutionRefMatches(ref, executionRefID, providerName, target, p, callerAppName, permissions) {
		return false
	}
	return strings.TrimSpace(ref.SourceDefinitionID) == strings.TrimSpace(sourceDefinitionID)
}

func normalizeEventMatch(match coreworkflow.EventMatch) coreworkflow.EventMatch {
	return coreworkflow.EventMatch{
		Type:    strings.TrimSpace(match.Type),
		Source:  strings.TrimSpace(match.Source),
		Subject: strings.TrimSpace(match.Subject),
	}
}

func workflowActorFromPrincipal(p *principal.Principal) coreworkflow.Actor {
	p = principal.Canonicalized(p)
	if p == nil {
		return coreworkflow.Actor{}
	}
	return coreworkflow.Actor{
		SubjectID:   strings.TrimSpace(p.SubjectID),
		SubjectKind: string(p.Kind),
		DisplayName: workflowActorDisplayName(p),
		AuthSource:  p.AuthSource(),
	}
}

func workflowActorDisplayName(p *principal.Principal) string {
	if p == nil {
		return ""
	}
	if value := strings.TrimSpace(p.DisplayName); value != "" {
		return value
	}
	if p.Identity != nil {
		return strings.TrimSpace(p.Identity.DisplayName)
	}
	return ""
}

func principalSubjectID(p *principal.Principal) string {
	if p == nil {
		return ""
	}
	return p.SubjectID
}

func newDefinitionID(idempotencyScope string) string {
	idempotencyScope = strings.TrimSpace(idempotencyScope)
	if idempotencyScope == "" {
		return workflowDefinitionExecutionRefBasePrefix + uuid.NewString()
	}
	return workflowDefinitionExecutionRefBasePrefix + uuid.NewSHA1(uuid.NameSpaceURL, []byte("gestalt.workflow.definition:"+idempotencyScope)).String()
}

func scheduleExecutionRefID(scheduleID string) string {
	return scheduleExecutionRefPrefix(scheduleID) + uuid.NewString()
}

func newScheduleID(idempotencyScope string) string {
	idempotencyScope = strings.TrimSpace(idempotencyScope)
	if idempotencyScope == "" {
		return uuid.NewString()
	}
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("gestalt.workflow.schedule:"+idempotencyScope)).String()
}

func workflowCreateIdempotencyScope(p *principal.Principal, callerAppName, idempotencyKey string) string {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return ""
	}
	return strings.Join([]string{strings.TrimSpace(principalSubjectID(p)), strings.TrimSpace(callerAppName), idempotencyKey}, "\x00")
}

func newScheduleExecutionRefID(scheduleID, idempotencyScope string) string {
	idempotencyScope = strings.TrimSpace(idempotencyScope)
	if idempotencyScope == "" {
		return scheduleExecutionRefID(scheduleID)
	}
	return scheduleExecutionRefPrefix(scheduleID) + uuid.NewSHA1(uuid.NameSpaceURL, []byte("gestalt.workflow.schedule.ref:"+strings.TrimSpace(scheduleID)+":"+idempotencyScope)).String()
}

func scheduleExecutionRefPrefix(scheduleID string) string {
	return workflowScheduleExecutionRefBasePrefix + strings.TrimSpace(scheduleID) + ":"
}

func scheduleIDFromExecutionRefID(executionRefID string) string {
	trimmed := strings.TrimSpace(executionRefID)
	if !strings.HasPrefix(trimmed, workflowScheduleExecutionRefBasePrefix) {
		return ""
	}
	rest := strings.TrimPrefix(trimmed, workflowScheduleExecutionRefBasePrefix)
	lastColon := strings.LastIndex(rest, ":")
	if lastColon <= 0 {
		return ""
	}
	return rest[:lastColon]
}

func eventTriggerExecutionRefID(triggerID string) string {
	return eventTriggerExecutionRefPrefix(triggerID) + uuid.NewString()
}

func newEventTriggerID(idempotencyScope string) string {
	idempotencyScope = strings.TrimSpace(idempotencyScope)
	if idempotencyScope == "" {
		return uuid.NewString()
	}
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("gestalt.workflow.event-trigger:"+idempotencyScope)).String()
}

func newEventTriggerExecutionRefID(triggerID, idempotencyScope string) string {
	idempotencyScope = strings.TrimSpace(idempotencyScope)
	if idempotencyScope == "" {
		return eventTriggerExecutionRefID(triggerID)
	}
	return eventTriggerExecutionRefPrefix(triggerID) + uuid.NewSHA1(uuid.NameSpaceURL, []byte("gestalt.workflow.event-trigger.ref:"+strings.TrimSpace(triggerID)+":"+idempotencyScope)).String()
}

func eventTriggerExecutionRefPrefix(triggerID string) string {
	return workflowEventTriggerExecutionRefBasePrefix + strings.TrimSpace(triggerID) + ":"
}

func eventTriggerIDFromExecutionRefID(executionRefID string) string {
	trimmed := strings.TrimSpace(executionRefID)
	if !strings.HasPrefix(trimmed, workflowEventTriggerExecutionRefBasePrefix) {
		return ""
	}
	rest := strings.TrimPrefix(trimmed, workflowEventTriggerExecutionRefBasePrefix)
	lastColon := strings.LastIndex(rest, ":")
	if lastColon <= 0 {
		return ""
	}
	return rest[:lastColon]
}

func runExecutionRefID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = uuid.NewString()
	}
	return workflowRunExecutionRefBasePrefix + value
}

func newRunExecutionRefID(idempotencyScope, workflowKey string) string {
	idempotencyScope = strings.TrimSpace(idempotencyScope)
	if idempotencyScope == "" {
		return runExecutionRefID("")
	}
	scope := strings.Join([]string{"gestalt.workflow.run", idempotencyScope, strings.TrimSpace(workflowKey)}, "\x00")
	return runExecutionRefID(uuid.NewSHA1(uuid.NameSpaceURL, []byte(scope)).String())
}

func signalOrStartExecutionRefID(providerName, workflowKey string, target coreworkflow.Target, p *principal.Principal, callerAppName string, permissions []core.AccessPermission) (string, error) {
	targetFingerprint, err := coreworkflow.TargetFingerprint(target)
	if err != nil {
		return "", fmt.Errorf("workflow target fingerprint: %w", err)
	}
	scope := strings.Join([]string{
		"gestalt.workflow.run.signal_or_start.ref.v2",
		strings.TrimSpace(providerName),
		strings.TrimSpace(workflowKey),
		strings.TrimSpace(principalSubjectID(p)),
		strings.TrimSpace(principal.EffectiveCredentialSubjectID(p)),
		strings.TrimSpace(callerAppName),
		targetFingerprint,
		executionRefPermissionsScope(permissions),
	}, "\x00")
	return runExecutionRefID(uuid.NewSHA1(uuid.NameSpaceURL, []byte(scope)).String()), nil
}

func executionRefPermissionsScope(permissions []core.AccessPermission) string {
	if permissions == nil {
		return "nil"
	}
	if len(permissions) == 0 {
		return "empty"
	}
	var b strings.Builder
	b.WriteString("set\x1f")
	wrote := false
	for _, permission := range permissions {
		app := strings.TrimSpace(permission.App)
		if app == "" {
			continue
		}
		wrote = true
		b.WriteString(app)
		b.WriteByte('\x1e')
		operations := append([]string(nil), permission.Operations...)
		sort.Strings(operations)
		for _, operation := range operations {
			operation = strings.TrimSpace(operation)
			if operation == "" {
				continue
			}
			b.WriteString(operation)
			b.WriteByte('\x1d')
		}
		b.WriteByte('\x1e')
		actions := append([]string(nil), permission.Actions...)
		sort.Strings(actions)
		for _, action := range actions {
			action = strings.TrimSpace(action)
			if action == "" {
				continue
			}
			b.WriteString(action)
			b.WriteByte('\x1d')
		}
		b.WriteByte('\x1f')
	}
	if !wrote {
		return "empty"
	}
	return b.String()
}

func (m *Manager) managedSignalResponse(ctx context.Context, p *principal.Principal, providerName string, provider coreworkflow.Provider, resp *coreworkflow.SignalRunResponse, candidateRef *coreworkflow.ExecutionReference, targetPrincipalSource signalTargetPrincipalSource) (*ManagedRunSignal, error) {
	if resp == nil || resp.Run == nil {
		return nil, core.ErrNotFound
	}
	providerName = strings.TrimSpace(providerName)
	ref := candidateRef
	if !runMatchesExecutionRef(providerName, resp.Run, ref) || strings.TrimSpace(ref.ID) != strings.TrimSpace(resp.Run.ExecutionRef) {
		ref = nil
	}
	if ref == nil {
		store, err := workflowExecutionReferenceStore(providerName, provider)
		if err != nil {
			return nil, err
		}
		ref, err = store.GetExecutionReference(ctx, strings.TrimSpace(resp.Run.ExecutionRef))
		if err != nil {
			return nil, err
		}
		ref = workflowExecutionRefForProvider(ref, providerName)
	}
	targetPrincipal := p
	if targetPrincipalSource == signalTargetPrincipalExecutionRef {
		targetPrincipal = workflowprincipal.RuntimePrincipalFromExecutionReference(ref)
	}
	if !executionRefOwnedBy(ref, p) || !executionRefActive(ref) || !m.allowTarget(ctx, targetPrincipal, ref.Target) || !runMatchesExecutionRef(providerName, resp.Run, ref) {
		return nil, core.ErrNotFound
	}
	workflowKey := strings.TrimSpace(resp.WorkflowKey)
	if workflowKey == "" {
		workflowKey = strings.TrimSpace(resp.Run.WorkflowKey)
	}
	return &ManagedRunSignal{
		ProviderName: providerName,
		Run:          resp.Run,
		Signal:       resp.Signal,
		StartedRun:   resp.StartedRun,
		WorkflowKey:  workflowKey,
		ExecutionRef: ref,
		provider:     provider,
	}, nil
}

func existingProvider(value *ManagedSchedule) coreworkflow.Provider {
	if value == nil {
		return nil
	}
	return value.provider
}

func existingRunProvider(value *ManagedRun) coreworkflow.Provider {
	if value == nil {
		return nil
	}
	return value.provider
}

func existingEventTriggerProvider(value *ManagedEventTrigger) coreworkflow.Provider {
	if value == nil {
		return nil
	}
	return value.provider
}

func (m *Manager) normalizeSignal(signal coreworkflow.Signal, p *principal.Principal) (coreworkflow.Signal, error) {
	signal.ID = strings.TrimSpace(signal.ID)
	signal.Name = strings.TrimSpace(signal.Name)
	signal.IdempotencyKey = strings.TrimSpace(signal.IdempotencyKey)
	signal.Payload = maps.Clone(signal.Payload)
	signal.Metadata = maps.Clone(signal.Metadata)
	if signal.Name == "" {
		return coreworkflow.Signal{}, ErrWorkflowSignalNameRequired
	}
	if signal.CreatedBy == (coreworkflow.Actor{}) {
		signal.CreatedBy = workflowActorFromPrincipal(p)
	}
	if signal.CreatedAt == nil || signal.CreatedAt.IsZero() {
		value := m.now().UTC()
		signal.CreatedAt = &value
	} else {
		value := signal.CreatedAt.UTC()
		signal.CreatedAt = &value
	}
	return signal, nil
}

func normalizePublishedEvent(event coreworkflow.Event, now time.Time) coreworkflow.Event {
	event.ID = strings.TrimSpace(event.ID)
	event.Source = strings.TrimSpace(event.Source)
	event.SpecVersion = strings.TrimSpace(event.SpecVersion)
	event.Type = strings.TrimSpace(event.Type)
	event.Subject = strings.TrimSpace(event.Subject)
	event.DataContentType = strings.TrimSpace(event.DataContentType)
	event.Data = maps.Clone(event.Data)
	event.Extensions = maps.Clone(event.Extensions)
	if event.ID == "" {
		event.ID = uuid.NewString()
	}
	if event.SpecVersion == "" {
		event.SpecVersion = defaultWorkflowEventSpecVersion
	}
	if event.Time == nil || event.Time.IsZero() {
		value := now.UTC()
		event.Time = &value
	} else {
		value := event.Time.UTC()
		event.Time = &value
	}
	return event
}
