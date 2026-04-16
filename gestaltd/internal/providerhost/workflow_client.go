package providerhost

import (
	"context"
	"io"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/egress"
	"google.golang.org/protobuf/types/known/emptypb"
)

type WorkflowExecConfig struct {
	Command       string
	Args          []string
	Env           map[string]string
	Config        map[string]any
	AllowedHosts  []string
	DefaultAction egress.PolicyAction
	HostBinary    string
	Cleanup       func()
	HostServices  []HostService
	Name          string
}

var startWorkflowProviderProcess = startProviderProcess

type remoteWorkflow struct {
	client  proto.WorkflowProviderClient
	runtime proto.ProviderLifecycleClient
	closer  io.Closer
}

func NewExecutableWorkflow(ctx context.Context, cfg WorkflowExecConfig) (coreworkflow.Provider, error) {
	proc, err := startWorkflowProviderProcess(ctx, ExecConfig{
		Command:       cfg.Command,
		Args:          cfg.Args,
		Env:           cfg.Env,
		Config:        cfg.Config,
		AllowedHosts:  cfg.AllowedHosts,
		DefaultAction: cfg.DefaultAction,
		HostBinary:    cfg.HostBinary,
		Cleanup:       cfg.Cleanup,
		HostServices:  cfg.HostServices,
	})
	if err != nil {
		return nil, err
	}

	runtimeClient := proto.NewProviderLifecycleClient(proc.conn)
	workflowClient := proto.NewWorkflowProviderClient(proc.conn)
	if _, err := configureRuntimeProvider(ctx, runtimeClient, proto.ProviderKind_PROVIDER_KIND_WORKFLOW, cfg.Name, cfg.Config); err != nil {
		_ = proc.Close()
		return nil, err
	}
	return &remoteWorkflow{client: workflowClient, runtime: runtimeClient, closer: proc}, nil
}

func (r *remoteWorkflow) StartRun(ctx context.Context, req coreworkflow.StartRunRequest) (*coreworkflow.Run, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	target, err := workflowTargetToProto(req.Target)
	if err != nil {
		return nil, err
	}
	resp, err := r.client.StartRun(ctx, &proto.StartWorkflowProviderRunRequest{
		Target:         target,
		IdempotencyKey: req.IdempotencyKey,
		PluginName:     req.Target.PluginName,
		CreatedBy:      workflowActorToProto(req.CreatedBy),
	})
	if err != nil {
		return nil, err
	}
	return workflowRunFromProto(resp)
}

func (r *remoteWorkflow) GetRun(ctx context.Context, req coreworkflow.GetRunRequest) (*coreworkflow.Run, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := r.client.GetRun(ctx, &proto.GetWorkflowProviderRunRequest{
		PluginName: req.PluginName,
		RunId:      req.RunID,
	})
	if err != nil {
		return nil, err
	}
	return workflowRunFromProto(resp)
}

func (r *remoteWorkflow) ListRuns(ctx context.Context, req coreworkflow.ListRunsRequest) ([]*coreworkflow.Run, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := r.client.ListRuns(ctx, &proto.ListWorkflowProviderRunsRequest{PluginName: req.PluginName})
	if err != nil {
		return nil, err
	}
	runs := make([]*coreworkflow.Run, 0, len(resp.GetRuns()))
	for _, run := range resp.GetRuns() {
		value, err := workflowRunFromProto(run)
		if err != nil {
			return nil, err
		}
		runs = append(runs, value)
	}
	return runs, nil
}

func (r *remoteWorkflow) CancelRun(ctx context.Context, req coreworkflow.CancelRunRequest) (*coreworkflow.Run, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := r.client.CancelRun(ctx, &proto.CancelWorkflowProviderRunRequest{
		PluginName: req.PluginName,
		RunId:      req.RunID,
		Reason:     req.Reason,
	})
	if err != nil {
		return nil, err
	}
	return workflowRunFromProto(resp)
}

func (r *remoteWorkflow) UpsertSchedule(ctx context.Context, req coreworkflow.UpsertScheduleRequest) (*coreworkflow.Schedule, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	target, err := workflowTargetToProto(req.Target)
	if err != nil {
		return nil, err
	}
	resp, err := r.client.UpsertSchedule(ctx, &proto.UpsertWorkflowProviderScheduleRequest{
		ScheduleId:  req.ScheduleID,
		Cron:        req.Cron,
		Timezone:    req.Timezone,
		Target:      target,
		Paused:      req.Paused,
		PluginName:  req.Target.PluginName,
		RequestedBy: workflowActorToProto(req.RequestedBy),
	})
	if err != nil {
		return nil, err
	}
	return workflowScheduleFromProto(resp)
}

func (r *remoteWorkflow) GetSchedule(ctx context.Context, req coreworkflow.GetScheduleRequest) (*coreworkflow.Schedule, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := r.client.GetSchedule(ctx, &proto.GetWorkflowProviderScheduleRequest{
		PluginName: req.PluginName,
		ScheduleId: req.ScheduleID,
	})
	if err != nil {
		return nil, err
	}
	return workflowScheduleFromProto(resp)
}

func (r *remoteWorkflow) ListSchedules(ctx context.Context, req coreworkflow.ListSchedulesRequest) ([]*coreworkflow.Schedule, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := r.client.ListSchedules(ctx, &proto.ListWorkflowProviderSchedulesRequest{PluginName: req.PluginName})
	if err != nil {
		return nil, err
	}
	schedules := make([]*coreworkflow.Schedule, 0, len(resp.GetSchedules()))
	for _, schedule := range resp.GetSchedules() {
		value, err := workflowScheduleFromProto(schedule)
		if err != nil {
			return nil, err
		}
		schedules = append(schedules, value)
	}
	return schedules, nil
}

func (r *remoteWorkflow) DeleteSchedule(ctx context.Context, req coreworkflow.DeleteScheduleRequest) error {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	_, err := r.client.DeleteSchedule(ctx, &proto.DeleteWorkflowProviderScheduleRequest{
		PluginName: req.PluginName,
		ScheduleId: req.ScheduleID,
	})
	return err
}

func (r *remoteWorkflow) PauseSchedule(ctx context.Context, req coreworkflow.PauseScheduleRequest) (*coreworkflow.Schedule, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := r.client.PauseSchedule(ctx, &proto.PauseWorkflowProviderScheduleRequest{
		PluginName: req.PluginName,
		ScheduleId: req.ScheduleID,
	})
	if err != nil {
		return nil, err
	}
	return workflowScheduleFromProto(resp)
}

func (r *remoteWorkflow) ResumeSchedule(ctx context.Context, req coreworkflow.ResumeScheduleRequest) (*coreworkflow.Schedule, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := r.client.ResumeSchedule(ctx, &proto.ResumeWorkflowProviderScheduleRequest{
		PluginName: req.PluginName,
		ScheduleId: req.ScheduleID,
	})
	if err != nil {
		return nil, err
	}
	return workflowScheduleFromProto(resp)
}

func (r *remoteWorkflow) UpsertEventTrigger(ctx context.Context, req coreworkflow.UpsertEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	target, err := workflowTargetToProto(req.Target)
	if err != nil {
		return nil, err
	}
	resp, err := r.client.UpsertEventTrigger(ctx, &proto.UpsertWorkflowProviderEventTriggerRequest{
		TriggerId:   req.TriggerID,
		Match:       workflowEventMatchToProto(req.Match),
		Target:      target,
		Paused:      req.Paused,
		PluginName:  req.Target.PluginName,
		RequestedBy: workflowActorToProto(req.RequestedBy),
	})
	if err != nil {
		return nil, err
	}
	return workflowEventTriggerFromProto(resp)
}

func (r *remoteWorkflow) GetEventTrigger(ctx context.Context, req coreworkflow.GetEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := r.client.GetEventTrigger(ctx, &proto.GetWorkflowProviderEventTriggerRequest{
		PluginName: req.PluginName,
		TriggerId:  req.TriggerID,
	})
	if err != nil {
		return nil, err
	}
	return workflowEventTriggerFromProto(resp)
}

func (r *remoteWorkflow) ListEventTriggers(ctx context.Context, req coreworkflow.ListEventTriggersRequest) ([]*coreworkflow.EventTrigger, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := r.client.ListEventTriggers(ctx, &proto.ListWorkflowProviderEventTriggersRequest{PluginName: req.PluginName})
	if err != nil {
		return nil, err
	}
	triggers := make([]*coreworkflow.EventTrigger, 0, len(resp.GetTriggers()))
	for _, trigger := range resp.GetTriggers() {
		value, err := workflowEventTriggerFromProto(trigger)
		if err != nil {
			return nil, err
		}
		triggers = append(triggers, value)
	}
	return triggers, nil
}

func (r *remoteWorkflow) DeleteEventTrigger(ctx context.Context, req coreworkflow.DeleteEventTriggerRequest) error {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	_, err := r.client.DeleteEventTrigger(ctx, &proto.DeleteWorkflowProviderEventTriggerRequest{
		PluginName: req.PluginName,
		TriggerId:  req.TriggerID,
	})
	return err
}

func (r *remoteWorkflow) PauseEventTrigger(ctx context.Context, req coreworkflow.PauseEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := r.client.PauseEventTrigger(ctx, &proto.PauseWorkflowProviderEventTriggerRequest{
		PluginName: req.PluginName,
		TriggerId:  req.TriggerID,
	})
	if err != nil {
		return nil, err
	}
	return workflowEventTriggerFromProto(resp)
}

func (r *remoteWorkflow) ResumeEventTrigger(ctx context.Context, req coreworkflow.ResumeEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := r.client.ResumeEventTrigger(ctx, &proto.ResumeWorkflowProviderEventTriggerRequest{
		PluginName: req.PluginName,
		TriggerId:  req.TriggerID,
	})
	if err != nil {
		return nil, err
	}
	return workflowEventTriggerFromProto(resp)
}

func (r *remoteWorkflow) PublishEvent(ctx context.Context, req coreworkflow.PublishEventRequest) error {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	pbEvent, err := workflowEventToProto(req.Event)
	if err != nil {
		return err
	}
	_, err = r.client.PublishEvent(ctx, &proto.PublishWorkflowProviderEventRequest{
		PluginName: req.PluginName,
		Event:      pbEvent,
	})
	return err
}

func (r *remoteWorkflow) Ping(ctx context.Context) error {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	_, err := r.runtime.HealthCheck(ctx, &emptypb.Empty{})
	return err
}

func (r *remoteWorkflow) Close() error {
	if r == nil || r.closer == nil {
		return nil
	}
	return r.closer.Close()
}

var _ coreworkflow.Provider = (*remoteWorkflow)(nil)
