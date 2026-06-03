package workflowmanager

import (
	"context"
	"fmt"
	"maps"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/workflowwire"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

func (m *Manager) ListSchedules(ctx context.Context, p *principal.Principal) ([]*ManagedSchedule, error) {
	if strings.TrimSpace(principalSubjectID(principal.Canonicalized(p))) == "" {
		return nil, ErrWorkflowSubjectRequired
	}
	out := []*ManagedSchedule{}
	var firstErr error
	for _, providerName := range m.providerNames() {
		_, provider, err := m.resolveProvider(ctx, providerName)
		if err != nil {
			return nil, err
		}
		resp, err := provider.ListSchedules(ctx, &proto.ListWorkflowProviderSchedulesRequest{})
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, scheduleProto := range resp.GetSchedules() {
			if scheduleProto == nil {
				continue
			}
			schedule, err := workflowwire.ScheduleFromProto(scheduleProto)
			if err != nil {
				return nil, err
			}
			managed := &ManagedSchedule{ProviderName: providerName, Schedule: schedule, provider: provider}
			if !m.scheduleAccessible(ctx, p, managed) {
				continue
			}
			out = append(out, managed)
		}
	}
	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	sort.Slice(out, func(i, j int) bool {
		left := out[i]
		right := out[j]
		if left.Schedule != nil && right.Schedule != nil && left.Schedule.CreatedAt != nil && right.Schedule.CreatedAt != nil && !left.Schedule.CreatedAt.Equal(*right.Schedule.CreatedAt) {
			return left.Schedule.CreatedAt.Before(*right.Schedule.CreatedAt)
		}
		return workflowScheduleID(left.Schedule) < workflowScheduleID(right.Schedule)
	})
	return out, nil
}

func (m *Manager) CreateSchedule(ctx context.Context, p *principal.Principal, req ScheduleUpsert) (out *ManagedSchedule, err error) {
	p = principal.Canonicalized(p)
	ctx = WithCallerAppName(ctx, req.CallerAppName)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationScheduleCreate)
	audit.setCallerApp(req.CallerAppName)
	defer func() {
		if out != nil && out.Schedule != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetSchedule, out.Schedule.ID, "")
			audit.setWorkflowTarget(out.Schedule.Target)
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
	var providerName string
	var provider coreworkflow.Provider
	var target coreworkflow.Target
	var resolved bool
	resolve := func() error {
		if resolved {
			return nil
		}
		var err error
		providerName, provider, target, err = m.resolveRequestProviderTarget(ctx, p, req.ProviderName, req.Target, req.DefinitionID, req.CallerAppName)
		if err != nil {
			return err
		}
		resolved = true
		return nil
	}
	if idempotencyKey != "" {
		existing, err := m.findSchedule(ctx, scheduleID, req.ProviderName)
		if err == nil {
			matchProviderName := existing.ProviderName
			matchTarget := existing.Schedule.Target
			if strings.TrimSpace(existing.Schedule.DefinitionID) == "" && strings.TrimSpace(req.DefinitionID) == "" {
				if err := resolve(); err != nil {
					return nil, err
				}
				matchProviderName = providerName
				matchTarget = target
			}
			if !managedScheduleMatchesDefinitionUpsert(existing, req, matchProviderName, matchTarget) {
				return nil, fmt.Errorf("%w: workflow schedule idempotency key reused with different request", invocation.ErrInvalidInvocation)
			}
			return existing, nil
		}
		if !isWorkflowProviderNotFound(err) {
			return nil, err
		}
	}

	if err := resolve(); err != nil {
		return nil, err
	}
	audit.setProvider(providerName)
	audit.setWorkflowTarget(target)
	targetProto, err := workflowwire.TargetToProto(target)
	if err != nil {
		return nil, err
	}
	scheduleProto, err := provider.UpsertSchedule(ctx, &proto.UpsertWorkflowProviderScheduleRequest{
		ScheduleId:           scheduleID,
		Cron:                 strings.TrimSpace(req.Cron),
		Timezone:             strings.TrimSpace(req.Timezone),
		Target:               targetProto,
		Paused:               req.Paused,
		RequestedBySubjectId: workflowSubjectIDFromPrincipal(p),
		DefinitionId:         strings.TrimSpace(req.DefinitionID),
	})
	if err != nil {
		return nil, err
	}
	schedule, err := workflowwire.ScheduleFromProto(scheduleProto)
	if err != nil {
		return nil, err
	}
	return &ManagedSchedule{ProviderName: providerName, Schedule: schedule, provider: provider}, nil
}

func managedScheduleMatchesDefinitionUpsert(existing *ManagedSchedule, req ScheduleUpsert, providerName string, target coreworkflow.Target) bool {
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
	existingDefinitionID := strings.TrimSpace(existing.Schedule.DefinitionID)
	requestDefinitionID := strings.TrimSpace(req.DefinitionID)
	if existingDefinitionID != "" || requestDefinitionID != "" {
		return existingDefinitionID == requestDefinitionID
	}
	return coreworkflow.TargetsEqual(existing.Schedule.Target, target)
}

func (m *Manager) GetSchedule(ctx context.Context, p *principal.Principal, scheduleID string) (*ManagedSchedule, error) {
	return m.requireOwnedSchedule(ctx, p, scheduleID, "")
}

func (m *Manager) UpdateSchedule(ctx context.Context, p *principal.Principal, scheduleID string, req ScheduleUpsert) (out *ManagedSchedule, err error) {
	p = principal.Canonicalized(p)
	ctx = WithCallerAppName(ctx, req.CallerAppName)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationScheduleUpdate)
	audit.setCallerApp(req.CallerAppName)
	audit.setObjectTarget(workflowAuditTargetSchedule, scheduleID, "")
	defer func() {
		if out != nil && out.Schedule != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetSchedule, out.Schedule.ID, "")
			audit.setWorkflowTarget(out.Schedule.Target)
		}
		audit.finish(ctx, err)
	}()
	if strings.TrimSpace(principalSubjectID(p)) == "" {
		return nil, ErrWorkflowSubjectRequired
	}
	existing, err := m.requireOwnedSchedule(ctx, p, scheduleID, "")
	if err != nil {
		return nil, err
	}
	nextProviderName, nextProvider, target, err := m.resolveRequestProviderTarget(ctx, p, req.ProviderNameOrDefault(existing.ProviderName), req.Target, req.DefinitionID, req.CallerAppName)
	if err != nil {
		return nil, err
	}
	audit.setProvider(nextProviderName)
	audit.setWorkflowTarget(target)
	targetProto, err := workflowwire.TargetToProto(target)
	if err != nil {
		return nil, err
	}
	scheduleProto, err := nextProvider.UpsertSchedule(ctx, &proto.UpsertWorkflowProviderScheduleRequest{
		ScheduleId:           strings.TrimSpace(existing.Schedule.ID),
		Cron:                 strings.TrimSpace(req.Cron),
		Timezone:             strings.TrimSpace(req.Timezone),
		Target:               targetProto,
		Paused:               req.Paused,
		RequestedBySubjectId: workflowSubjectIDFromPrincipal(p),
		DefinitionId:         strings.TrimSpace(req.DefinitionID),
	})
	if err != nil {
		return nil, err
	}
	schedule, err := workflowwire.ScheduleFromProto(scheduleProto)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(existing.ProviderName) != nextProviderName {
		if err := existing.provider.DeleteSchedule(ctx, &proto.DeleteWorkflowProviderScheduleRequest{ScheduleId: strings.TrimSpace(existing.Schedule.ID)}); err != nil && !isWorkflowProviderNotFound(err) {
			_ = nextProvider.DeleteSchedule(ctx, &proto.DeleteWorkflowProviderScheduleRequest{ScheduleId: strings.TrimSpace(existing.Schedule.ID)})
			return nil, err
		}
	}
	return &ManagedSchedule{ProviderName: nextProviderName, Schedule: schedule, provider: nextProvider}, nil
}

func (req ScheduleUpsert) ProviderNameOrDefault(defaultName string) string {
	if providerName := strings.TrimSpace(req.ProviderName); providerName != "" {
		return providerName
	}
	return strings.TrimSpace(defaultName)
}

func (m *Manager) DeleteSchedule(ctx context.Context, p *principal.Principal, scheduleID string) (err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationScheduleDelete)
	audit.setObjectTarget(workflowAuditTargetSchedule, scheduleID, "")
	defer func() { audit.finish(ctx, err) }()
	value, err := m.requireOwnedSchedule(ctx, p, scheduleID, "")
	if err != nil {
		return err
	}
	audit.setProvider(value.ProviderName)
	if value.Schedule != nil {
		audit.setWorkflowTarget(value.Schedule.Target)
	}
	return value.provider.DeleteSchedule(ctx, &proto.DeleteWorkflowProviderScheduleRequest{ScheduleId: strings.TrimSpace(value.Schedule.ID)})
}

func (m *Manager) PauseSchedule(ctx context.Context, p *principal.Principal, scheduleID string) (out *ManagedSchedule, err error) {
	return m.updateSchedulePaused(ctx, p, scheduleID, true, workflowAuditOperationSchedulePause)
}

func (m *Manager) ResumeSchedule(ctx context.Context, p *principal.Principal, scheduleID string) (out *ManagedSchedule, err error) {
	return m.updateSchedulePaused(ctx, p, scheduleID, false, workflowAuditOperationScheduleResume)
}

func (m *Manager) updateSchedulePaused(ctx context.Context, p *principal.Principal, scheduleID string, paused bool, operation string) (out *ManagedSchedule, err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, operation)
	audit.setObjectTarget(workflowAuditTargetSchedule, scheduleID, "")
	defer func() {
		if out != nil && out.Schedule != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetSchedule, out.Schedule.ID, "")
			audit.setWorkflowTarget(out.Schedule.Target)
		}
		audit.finish(ctx, err)
	}()
	value, err := m.requireOwnedSchedule(ctx, p, scheduleID, "")
	if err != nil {
		return nil, err
	}
	var scheduleProto *proto.BoundWorkflowSchedule
	if paused {
		scheduleProto, err = value.provider.PauseSchedule(ctx, &proto.PauseWorkflowProviderScheduleRequest{ScheduleId: strings.TrimSpace(value.Schedule.ID)})
	} else {
		scheduleProto, err = value.provider.ResumeSchedule(ctx, &proto.ResumeWorkflowProviderScheduleRequest{ScheduleId: strings.TrimSpace(value.Schedule.ID)})
	}
	if err != nil {
		return nil, err
	}
	schedule, err := workflowwire.ScheduleFromProto(scheduleProto)
	if err != nil {
		return nil, err
	}
	value.Schedule = schedule
	return value, nil
}

func (m *Manager) ListEventTriggers(ctx context.Context, p *principal.Principal) ([]*ManagedEventTrigger, error) {
	if strings.TrimSpace(principalSubjectID(principal.Canonicalized(p))) == "" {
		return nil, ErrWorkflowSubjectRequired
	}
	out := []*ManagedEventTrigger{}
	var firstErr error
	for _, providerName := range m.providerNames() {
		_, provider, err := m.resolveProvider(ctx, providerName)
		if err != nil {
			return nil, err
		}
		resp, err := provider.ListEventTriggers(ctx, &proto.ListWorkflowProviderEventTriggersRequest{})
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, triggerProto := range resp.GetTriggers() {
			if triggerProto == nil {
				continue
			}
			trigger, err := workflowwire.EventTriggerFromProto(triggerProto)
			if err != nil {
				return nil, err
			}
			managed := &ManagedEventTrigger{ProviderName: providerName, Trigger: trigger, provider: provider}
			if !m.eventTriggerAccessible(ctx, p, managed) {
				continue
			}
			out = append(out, managed)
		}
	}
	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	sort.Slice(out, func(i, j int) bool {
		left := out[i]
		right := out[j]
		if left.Trigger != nil && right.Trigger != nil && left.Trigger.CreatedAt != nil && right.Trigger.CreatedAt != nil && !left.Trigger.CreatedAt.Equal(*right.Trigger.CreatedAt) {
			return left.Trigger.CreatedAt.Before(*right.Trigger.CreatedAt)
		}
		return workflowEventTriggerID(left.Trigger) < workflowEventTriggerID(right.Trigger)
	})
	return out, nil
}

func (m *Manager) CreateEventTrigger(ctx context.Context, p *principal.Principal, req EventTriggerUpsert) (out *ManagedEventTrigger, err error) {
	p = principal.Canonicalized(p)
	ctx = WithCallerAppName(ctx, req.CallerAppName)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationEventTriggerCreate)
	audit.setCallerApp(req.CallerAppName)
	defer func() {
		if out != nil && out.Trigger != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetEventTrigger, out.Trigger.ID, "")
			audit.setWorkflowTarget(out.Trigger.Target)
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
	var providerName string
	var provider coreworkflow.Provider
	var target coreworkflow.Target
	var resolved bool
	resolve := func() error {
		if resolved {
			return nil
		}
		var err error
		providerName, provider, target, err = m.resolveRequestProviderTarget(ctx, p, req.ProviderName, req.Target, req.DefinitionID, req.CallerAppName)
		if err != nil {
			return err
		}
		resolved = true
		return nil
	}
	if idempotencyKey != "" {
		existing, err := m.findEventTrigger(ctx, triggerID, req.ProviderName)
		if err == nil {
			matchProviderName := existing.ProviderName
			matchTarget := existing.Trigger.Target
			if strings.TrimSpace(existing.Trigger.DefinitionID) == "" && strings.TrimSpace(req.DefinitionID) == "" {
				if err := resolve(); err != nil {
					return nil, err
				}
				matchProviderName = providerName
				matchTarget = target
			}
			if !managedEventTriggerMatchesDefinitionUpsert(existing, match, req, matchProviderName, matchTarget) {
				return nil, fmt.Errorf("%w: workflow trigger idempotency key reused with different request", invocation.ErrInvalidInvocation)
			}
			return existing, nil
		}
		if !isWorkflowProviderNotFound(err) {
			return nil, err
		}
	}

	if err := resolve(); err != nil {
		return nil, err
	}
	audit.setProvider(providerName)
	audit.setWorkflowTarget(target)
	targetProto, err := workflowwire.TargetToProto(target)
	if err != nil {
		return nil, err
	}
	triggerProto, err := provider.UpsertEventTrigger(ctx, &proto.UpsertWorkflowProviderEventTriggerRequest{
		TriggerId:            triggerID,
		Match:                workflowwire.EventMatchToProto(match),
		Target:               targetProto,
		Paused:               req.Paused,
		RequestedBySubjectId: workflowSubjectIDFromPrincipal(p),
		DefinitionId:         strings.TrimSpace(req.DefinitionID),
	})
	if err != nil {
		return nil, err
	}
	trigger, err := workflowwire.EventTriggerFromProto(triggerProto)
	if err != nil {
		return nil, err
	}
	return &ManagedEventTrigger{ProviderName: providerName, Trigger: trigger, provider: provider}, nil
}

func managedEventTriggerMatchesDefinitionUpsert(existing *ManagedEventTrigger, match coreworkflow.EventMatch, req EventTriggerUpsert, providerName string, target coreworkflow.Target) bool {
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
	existingDefinitionID := strings.TrimSpace(existing.Trigger.DefinitionID)
	requestDefinitionID := strings.TrimSpace(req.DefinitionID)
	if existingDefinitionID != "" || requestDefinitionID != "" {
		return existingDefinitionID == requestDefinitionID
	}
	return coreworkflow.TargetsEqual(existing.Trigger.Target, target)
}

func (m *Manager) GetEventTrigger(ctx context.Context, p *principal.Principal, triggerID string) (*ManagedEventTrigger, error) {
	return m.requireOwnedEventTrigger(ctx, p, triggerID, "")
}

func (m *Manager) UpdateEventTrigger(ctx context.Context, p *principal.Principal, triggerID string, req EventTriggerUpsert) (out *ManagedEventTrigger, err error) {
	p = principal.Canonicalized(p)
	ctx = WithCallerAppName(ctx, req.CallerAppName)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationEventTriggerUpdate)
	audit.setCallerApp(req.CallerAppName)
	audit.setObjectTarget(workflowAuditTargetEventTrigger, triggerID, "")
	defer func() {
		if out != nil && out.Trigger != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetEventTrigger, out.Trigger.ID, "")
			audit.setWorkflowTarget(out.Trigger.Target)
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
	existing, err := m.requireOwnedEventTrigger(ctx, p, triggerID, "")
	if err != nil {
		return nil, err
	}
	nextProviderName, nextProvider, target, err := m.resolveRequestProviderTarget(ctx, p, req.ProviderNameOrDefault(existing.ProviderName), req.Target, req.DefinitionID, req.CallerAppName)
	if err != nil {
		return nil, err
	}
	audit.setProvider(nextProviderName)
	audit.setWorkflowTarget(target)
	targetProto, err := workflowwire.TargetToProto(target)
	if err != nil {
		return nil, err
	}
	triggerProto, err := nextProvider.UpsertEventTrigger(ctx, &proto.UpsertWorkflowProviderEventTriggerRequest{
		TriggerId:            strings.TrimSpace(existing.Trigger.ID),
		Match:                workflowwire.EventMatchToProto(match),
		Target:               targetProto,
		Paused:               req.Paused,
		RequestedBySubjectId: workflowSubjectIDFromPrincipal(p),
		DefinitionId:         strings.TrimSpace(req.DefinitionID),
	})
	if err != nil {
		return nil, err
	}
	trigger, err := workflowwire.EventTriggerFromProto(triggerProto)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(existing.ProviderName) != nextProviderName {
		if err := existing.provider.DeleteEventTrigger(ctx, &proto.DeleteWorkflowProviderEventTriggerRequest{TriggerId: strings.TrimSpace(existing.Trigger.ID)}); err != nil && !isWorkflowProviderNotFound(err) {
			_ = nextProvider.DeleteEventTrigger(ctx, &proto.DeleteWorkflowProviderEventTriggerRequest{TriggerId: strings.TrimSpace(existing.Trigger.ID)})
			return nil, err
		}
	}
	return &ManagedEventTrigger{ProviderName: nextProviderName, Trigger: trigger, provider: nextProvider}, nil
}

func (req EventTriggerUpsert) ProviderNameOrDefault(defaultName string) string {
	if providerName := strings.TrimSpace(req.ProviderName); providerName != "" {
		return providerName
	}
	return strings.TrimSpace(defaultName)
}

func (m *Manager) DeleteEventTrigger(ctx context.Context, p *principal.Principal, triggerID string) (err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationEventTriggerDelete)
	audit.setObjectTarget(workflowAuditTargetEventTrigger, triggerID, "")
	defer func() { audit.finish(ctx, err) }()
	value, err := m.requireOwnedEventTrigger(ctx, p, triggerID, "")
	if err != nil {
		return err
	}
	audit.setProvider(value.ProviderName)
	if value.Trigger != nil {
		audit.setWorkflowTarget(value.Trigger.Target)
	}
	return value.provider.DeleteEventTrigger(ctx, &proto.DeleteWorkflowProviderEventTriggerRequest{TriggerId: strings.TrimSpace(value.Trigger.ID)})
}

func (m *Manager) PauseEventTrigger(ctx context.Context, p *principal.Principal, triggerID string) (out *ManagedEventTrigger, err error) {
	return m.updateEventTriggerPaused(ctx, p, triggerID, true, workflowAuditOperationEventTriggerPause)
}

func (m *Manager) ResumeEventTrigger(ctx context.Context, p *principal.Principal, triggerID string) (out *ManagedEventTrigger, err error) {
	return m.updateEventTriggerPaused(ctx, p, triggerID, false, workflowAuditOperationEventTriggerResume)
}

func (m *Manager) updateEventTriggerPaused(ctx context.Context, p *principal.Principal, triggerID string, paused bool, operation string) (out *ManagedEventTrigger, err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, operation)
	audit.setObjectTarget(workflowAuditTargetEventTrigger, triggerID, "")
	defer func() {
		if out != nil && out.Trigger != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetEventTrigger, out.Trigger.ID, "")
			audit.setWorkflowTarget(out.Trigger.Target)
		}
		audit.finish(ctx, err)
	}()
	value, err := m.requireOwnedEventTrigger(ctx, p, triggerID, "")
	if err != nil {
		return nil, err
	}
	var triggerProto *proto.BoundWorkflowEventTrigger
	if paused {
		triggerProto, err = value.provider.PauseEventTrigger(ctx, &proto.PauseWorkflowProviderEventTriggerRequest{TriggerId: strings.TrimSpace(value.Trigger.ID)})
	} else {
		triggerProto, err = value.provider.ResumeEventTrigger(ctx, &proto.ResumeWorkflowProviderEventTriggerRequest{TriggerId: strings.TrimSpace(value.Trigger.ID)})
	}
	if err != nil {
		return nil, err
	}
	trigger, err := workflowwire.EventTriggerFromProto(triggerProto)
	if err != nil {
		return nil, err
	}
	value.Trigger = trigger
	return value, nil
}

func (m *Manager) findSchedule(ctx context.Context, scheduleID, providerSelection string) (*ManagedSchedule, error) {
	scheduleID = strings.TrimSpace(scheduleID)
	if scheduleID == "" {
		return nil, coreworkflowProviderNotFound()
	}
	if providerSelection = strings.TrimSpace(providerSelection); providerSelection != "" {
		providerName, provider, err := m.resolveProvider(ctx, providerSelection)
		if err != nil {
			return nil, err
		}
		scheduleProto, err := provider.GetSchedule(ctx, &proto.GetWorkflowProviderScheduleRequest{ScheduleId: scheduleID})
		if err != nil {
			return nil, err
		}
		schedule, err := workflowwire.ScheduleFromProto(scheduleProto)
		if err != nil {
			return nil, err
		}
		return &ManagedSchedule{ProviderName: providerName, Schedule: schedule, provider: provider}, nil
	}
	var match *ManagedSchedule
	var firstErr error
	for _, providerName := range m.providerNames() {
		_, provider, err := m.resolveProvider(ctx, providerName)
		if err != nil {
			return nil, err
		}
		scheduleProto, err := provider.GetSchedule(ctx, &proto.GetWorkflowProviderScheduleRequest{ScheduleId: scheduleID})
		if err != nil {
			if isWorkflowProviderNotFound(err) {
				continue
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		schedule, err := workflowwire.ScheduleFromProto(scheduleProto)
		if err != nil {
			return nil, err
		}
		if match != nil {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateWorkflowObjects, scheduleID)
		}
		match = &ManagedSchedule{ProviderName: strings.TrimSpace(providerName), Schedule: schedule, provider: provider}
	}
	if match != nil {
		return match, nil
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return nil, coreworkflowProviderNotFound()
}

func (m *Manager) findEventTrigger(ctx context.Context, triggerID, providerSelection string) (*ManagedEventTrigger, error) {
	triggerID = strings.TrimSpace(triggerID)
	if triggerID == "" {
		return nil, coreworkflowProviderNotFound()
	}
	if providerSelection = strings.TrimSpace(providerSelection); providerSelection != "" {
		providerName, provider, err := m.resolveProvider(ctx, providerSelection)
		if err != nil {
			return nil, err
		}
		triggerProto, err := provider.GetEventTrigger(ctx, &proto.GetWorkflowProviderEventTriggerRequest{TriggerId: triggerID})
		if err != nil {
			return nil, err
		}
		trigger, err := workflowwire.EventTriggerFromProto(triggerProto)
		if err != nil {
			return nil, err
		}
		return &ManagedEventTrigger{ProviderName: providerName, Trigger: trigger, provider: provider}, nil
	}
	var match *ManagedEventTrigger
	var firstErr error
	for _, providerName := range m.providerNames() {
		_, provider, err := m.resolveProvider(ctx, providerName)
		if err != nil {
			return nil, err
		}
		triggerProto, err := provider.GetEventTrigger(ctx, &proto.GetWorkflowProviderEventTriggerRequest{TriggerId: triggerID})
		if err != nil {
			if isWorkflowProviderNotFound(err) {
				continue
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		trigger, err := workflowwire.EventTriggerFromProto(triggerProto)
		if err != nil {
			return nil, err
		}
		if match != nil {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateWorkflowObjects, triggerID)
		}
		match = &ManagedEventTrigger{ProviderName: strings.TrimSpace(providerName), Trigger: trigger, provider: provider}
	}
	if match != nil {
		return match, nil
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return nil, coreworkflowProviderNotFound()
}

func (m *Manager) requireOwnedSchedule(ctx context.Context, p *principal.Principal, scheduleID, providerSelection string) (*ManagedSchedule, error) {
	schedule, err := m.findSchedule(ctx, scheduleID, providerSelection)
	if err != nil {
		return nil, err
	}
	if !m.scheduleAccessible(ctx, p, schedule) {
		return nil, core.ErrNotFound
	}
	return schedule, nil
}

func (m *Manager) scheduleAccessible(ctx context.Context, p *principal.Principal, schedule *ManagedSchedule) bool {
	if schedule == nil || schedule.Schedule == nil {
		return false
	}
	return workflowSubjectOwnedBy(schedule.Schedule.CreatedBySubjectID, p) && m.allowStoredTarget(ctx, p, schedule.Schedule.Target)
}

func (m *Manager) requireOwnedEventTrigger(ctx context.Context, p *principal.Principal, triggerID, providerSelection string) (*ManagedEventTrigger, error) {
	trigger, err := m.findEventTrigger(ctx, triggerID, providerSelection)
	if err != nil {
		return nil, err
	}
	if !m.eventTriggerAccessible(ctx, p, trigger) {
		return nil, core.ErrNotFound
	}
	return trigger, nil
}

func (m *Manager) eventTriggerAccessible(ctx context.Context, p *principal.Principal, trigger *ManagedEventTrigger) bool {
	if trigger == nil || trigger.Trigger == nil {
		return false
	}
	return workflowSubjectOwnedBy(trigger.Trigger.CreatedBySubjectID, p) && m.allowStoredTarget(ctx, p, trigger.Trigger.Target)
}

func workflowSubjectOwnedBy(createdBySubjectID string, p *principal.Principal) bool {
	subjectID := strings.TrimSpace(principalSubjectID(principal.Canonicalized(p)))
	return subjectID != "" && strings.TrimSpace(createdBySubjectID) == subjectID
}

func coreworkflowProviderNotFound() error {
	return core.ErrNotFound
}

func workflowScheduleID(schedule *coreworkflow.Schedule) string {
	if schedule == nil {
		return ""
	}
	return strings.TrimSpace(schedule.ID)
}

func workflowEventTriggerID(trigger *coreworkflow.EventTrigger) string {
	if trigger == nil {
		return ""
	}
	return strings.TrimSpace(trigger.ID)
}

func (m *Manager) catalogSelectorConfig() invocation.CatalogSelectorConfig {
	return invocation.CatalogSelectorConfig{
		Invoker:           m.invoker,
		CatalogConnection: m.catalogConnection,
		DefaultConnection: m.defaultConnection,
	}
}

func normalizeEventMatch(match coreworkflow.EventMatch) coreworkflow.EventMatch {
	return coreworkflow.EventMatch{
		Type:    strings.TrimSpace(match.Type),
		Source:  strings.TrimSpace(match.Source),
		Subject: strings.TrimSpace(match.Subject),
	}
}

func workflowSubjectIDFromPrincipal(p *principal.Principal) string {
	p = principal.Canonicalized(p)
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.SubjectID)
}

func principalSubjectID(p *principal.Principal) string {
	if p == nil {
		return ""
	}
	return p.SubjectID
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

func newEventTriggerID(idempotencyScope string) string {
	idempotencyScope = strings.TrimSpace(idempotencyScope)
	if idempotencyScope == "" {
		return uuid.NewString()
	}
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("gestalt.workflow.event-trigger:"+idempotencyScope)).String()
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
	if strings.TrimSpace(signal.CreatedBySubjectID) == "" {
		signal.CreatedBySubjectID = workflowSubjectIDFromPrincipal(p)
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
