package workflows

import (
	"github.com/valon-technologies/gestalt/server/internal/workflowwire"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
)

func managedWorkflowDefinitionToProto(managed *workflowmanager.ManagedDefinition) (*proto.WorkflowDefinition, error) {
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

func managedWorkflowRunToProto(managed *workflowmanager.ManagedRun) (*proto.WorkflowRun, error) {
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
