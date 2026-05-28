package workflows

import (
	"github.com/valon-technologies/gestalt/server/internal/workflowwire"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
)

func managedWorkflowScheduleToProto(managed *workflowmanager.ManagedSchedule) (*proto.BoundWorkflowSchedule, error) {
	if managed == nil {
		return nil, nil
	}
	schedule, err := workflowwire.ScheduleToProto(managed.Schedule)
	if err != nil {
		return nil, err
	}
	schedule.ProviderName = managed.ProviderName
	return schedule, nil
}

func managedWorkflowEventTriggerToProto(managed *workflowmanager.ManagedEventTrigger) (*proto.BoundWorkflowEventTrigger, error) {
	if managed == nil {
		return nil, nil
	}
	trigger, err := workflowwire.EventTriggerToProto(managed.Trigger)
	if err != nil {
		return nil, err
	}
	trigger.ProviderName = managed.ProviderName
	return trigger, nil
}

func managedWorkflowDefinitionToProto(managed *workflowmanager.ManagedDefinition) (*proto.BoundWorkflowDefinition, error) {
	if managed == nil {
		return nil, nil
	}
	definition, err := workflowwire.DefinitionToProto(managed.Definition)
	if err != nil {
		return nil, err
	}
	definition.ProviderName = managed.ProviderName
	return definition, nil
}

func managedWorkflowRunToProto(managed *workflowmanager.ManagedRun) (*proto.BoundWorkflowRun, error) {
	if managed == nil {
		return nil, nil
	}
	run, err := workflowwire.RunToProto(managed.Run)
	if err != nil {
		return nil, err
	}
	run.ProviderName = managed.ProviderName
	return run, nil
}

func managedWorkflowRunSignalToProto(managed *workflowmanager.ManagedRunSignal) (*proto.SignalWorkflowRunResponse, error) {
	if managed == nil {
		return nil, nil
	}
	run, err := workflowwire.RunToProto(managed.Run)
	if err != nil {
		return nil, err
	}
	run.ProviderName = managed.ProviderName
	signal, err := workflowwire.SignalToProto(managed.Signal)
	if err != nil {
		return nil, err
	}
	return &proto.SignalWorkflowRunResponse{
		Run:         run,
		Signal:      signal,
		StartedRun:  managed.StartedRun,
		WorkflowKey: managed.WorkflowKey,
	}, nil
}
