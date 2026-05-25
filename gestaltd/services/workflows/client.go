package workflows

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/observability"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/emptypb"
)

type ExecConfig struct {
	Command      string
	Args         []string
	Workdir      string
	Env          map[string]string
	Config       map[string]any
	Egress       egress.Policy
	HostBinary   string
	Cleanup      func()
	HostServices []runtimehost.HostService
	Name         string
	Telemetry    runtimehost.TelemetryProviders
}

type RemoteConfig struct {
	Client  proto.WorkflowProviderClient
	Runtime proto.ProviderLifecycleClient
	Closer  io.Closer
	Config  map[string]any
	Name    string
}

var startWorkflowProviderProcess = runtimehost.StartAppProcess

type remoteWorkflow struct {
	client  proto.WorkflowProviderClient
	runtime proto.ProviderLifecycleClient
	closer  io.Closer
	name    string
}

func NewExecutable(ctx context.Context, cfg ExecConfig) (coreworkflow.Provider, error) {
	proc, err := startWorkflowProviderProcess(ctx, runtimehost.ProcessConfig{
		Command:      cfg.Command,
		Args:         cfg.Args,
		Workdir:      cfg.Workdir,
		Env:          cfg.Env,
		Egress:       cfg.Egress,
		HostBinary:   cfg.HostBinary,
		Cleanup:      cfg.Cleanup,
		HostServices: cfg.HostServices,
		ProviderName: cfg.Name,
		Telemetry:    cfg.Telemetry,
	})
	if err != nil {
		return nil, err
	}

	runtimeClient := proc.Lifecycle()
	workflowClient := proto.NewWorkflowProviderClient(proc.Conn())
	if _, err := runtimehost.ConfigureRuntimeProvider(ctx, runtimeClient, proto.ProviderKind_PROVIDER_KIND_WORKFLOW, cfg.Name, cfg.Config); err != nil {
		_ = proc.Close()
		return nil, err
	}
	return &remoteWorkflow{client: workflowClient, runtime: runtimeClient, closer: proc, name: cfg.Name}, nil
}

func NewRemote(ctx context.Context, cfg RemoteConfig) (coreworkflow.Provider, error) {
	if cfg.Client == nil {
		if cfg.Closer != nil {
			_ = cfg.Closer.Close()
		}
		return nil, fmt.Errorf("workflow provider client is required")
	}
	if cfg.Runtime == nil {
		if cfg.Closer != nil {
			_ = cfg.Closer.Close()
		}
		return nil, fmt.Errorf("workflow provider lifecycle client is required")
	}
	if _, err := runtimehost.ConfigureRuntimeProvider(ctx, cfg.Runtime, proto.ProviderKind_PROVIDER_KIND_WORKFLOW, cfg.Name, cfg.Config); err != nil {
		if cfg.Closer != nil {
			_ = cfg.Closer.Close()
		}
		return nil, err
	}
	return &remoteWorkflow{client: cfg.Client, runtime: cfg.Runtime, closer: cfg.Closer, name: cfg.Name}, nil
}

func (r *remoteWorkflow) StartRun(ctx context.Context, req coreworkflow.StartRunRequest) (run *coreworkflow.Run, err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationStartRun, workflowDims{
		triggerKind: observability.WorkflowTriggerKindManual,
		targetKind:  workflowTargetKind(req.Target),
	})
	defer func() { end(err) }()
	target, err := workflowTargetToProto(req.Target)
	if err != nil {
		return nil, err
	}
	pbReq := &proto.StartWorkflowProviderRunRequest{
		Target:         target,
		IdempotencyKey: req.IdempotencyKey,
		CreatedBy:      workflowActorToProto(req.CreatedBy),
		ExecutionRef:   req.ExecutionRef,
		WorkflowKey:    req.WorkflowKey,
	}
	ctx, cancel := workflowProviderRequestContext(ctx, pbReq)
	defer cancel()
	resp, err := r.client.StartRun(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	run, err = workflowRunFromProto(resp)
	if err == nil && strings.TrimSpace(req.IdempotencyKey) == "" {
		observability.RecordWorkflowRunStarted(ctx, r.workflowMetricDims(observability.WorkflowOperationStartRun, workflowDims{
			triggerKind: observability.WorkflowTriggerKindManual,
			targetKind:  workflowTargetKind(req.Target),
			runStatus:   workflowRunStatusFromCore(run),
		}))
	}
	return run, err
}

func (r *remoteWorkflow) GetRun(ctx context.Context, req coreworkflow.GetRunRequest) (run *coreworkflow.Run, err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationGetRun, workflowDims{})
	defer func() { end(err) }()
	pbReq := &proto.GetWorkflowProviderRunRequest{RunId: req.RunID}
	ctx, cancel := workflowProviderRequestContext(ctx, pbReq)
	defer cancel()
	resp, err := r.client.GetRun(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	return workflowRunFromProto(resp)
}

func (r *remoteWorkflow) ListRuns(ctx context.Context, req coreworkflow.ListRunsRequest) (out *coreworkflow.ListRunsResponse, err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationListRuns, workflowDims{})
	defer func() { end(err) }()
	pbReq := &proto.ListWorkflowProviderRunsRequest{
		PageSize:  int32(req.PageSize),
		PageToken: strings.TrimSpace(req.PageToken),
		Status:    workflowRunStatusToProto(req.Status),
		TargetApp: strings.TrimSpace(req.TargetApp),
	}
	ctx, cancel := workflowProviderRequestContext(ctx, pbReq)
	defer cancel()
	resp, err := r.client.ListRuns(ctx, pbReq)
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
	return &coreworkflow.ListRunsResponse{
		Runs:          runs,
		NextPageToken: strings.TrimSpace(resp.GetNextPageToken()),
	}, nil
}

func (r *remoteWorkflow) CancelRun(ctx context.Context, req coreworkflow.CancelRunRequest) (run *coreworkflow.Run, err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationCancelRun, workflowDims{})
	defer func() { end(err) }()
	pbReq := &proto.CancelWorkflowProviderRunRequest{
		RunId:  req.RunID,
		Reason: req.Reason,
	}
	ctx, cancel := workflowProviderRequestContext(ctx, pbReq)
	defer cancel()
	resp, err := r.client.CancelRun(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	return workflowRunFromProto(resp)
}

func (r *remoteWorkflow) SignalRun(ctx context.Context, req coreworkflow.SignalRunRequest) (out *coreworkflow.SignalRunResponse, err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationSignalRun, workflowDims{triggerKind: observability.WorkflowTriggerKindSignal})
	defer func() { end(err) }()
	signal, err := workflowSignalToProto(req.Signal)
	if err != nil {
		return nil, err
	}
	pbReq := &proto.SignalWorkflowProviderRunRequest{
		RunId:  req.RunID,
		Signal: signal,
	}
	ctx, cancel := workflowProviderRequestContext(ctx, pbReq)
	defer cancel()
	resp, err := r.client.SignalRun(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	return workflowSignalRunResponseFromProto(resp)
}

func (r *remoteWorkflow) SignalOrStartRun(ctx context.Context, req coreworkflow.SignalOrStartRunRequest) (out *coreworkflow.SignalRunResponse, err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationSignalOrStartRun, workflowDims{
		triggerKind: observability.WorkflowTriggerKindSignal,
		targetKind:  workflowTargetKind(req.Target),
	})
	defer func() { end(err) }()
	target, err := workflowTargetToProto(req.Target)
	if err != nil {
		return nil, err
	}
	signal, err := workflowSignalToProto(req.Signal)
	if err != nil {
		return nil, err
	}
	pbReq := &proto.SignalOrStartWorkflowProviderRunRequest{
		WorkflowKey:    req.WorkflowKey,
		Target:         target,
		IdempotencyKey: req.IdempotencyKey,
		CreatedBy:      workflowActorToProto(req.CreatedBy),
		ExecutionRef:   req.ExecutionRef,
		Signal:         signal,
	}
	ctx, cancel := workflowProviderRequestContext(ctx, pbReq)
	defer cancel()
	resp, err := r.client.SignalOrStartRun(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	out, err = workflowSignalRunResponseFromProto(resp)
	if err == nil && out != nil && out.StartedRun {
		dims := r.workflowMetricDims(observability.WorkflowOperationSignalOrStartRun, workflowDims{
			triggerKind: observability.WorkflowTriggerKindSignal,
			targetKind:  workflowTargetKind(req.Target),
			runStatus:   workflowRunStatusFromCore(out.Run),
		})
		observability.RecordWorkflowRunStarted(ctx, dims)
	}
	return out, err
}

func (r *remoteWorkflow) UpsertSchedule(ctx context.Context, req coreworkflow.UpsertScheduleRequest) (schedule *coreworkflow.Schedule, err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationUpsertSchedule, workflowDims{
		triggerKind: observability.WorkflowTriggerKindSchedule,
		targetKind:  workflowTargetKind(req.Target),
	})
	defer func() { end(err) }()
	target, err := workflowTargetToProto(req.Target)
	if err != nil {
		return nil, err
	}
	pbReq := &proto.UpsertWorkflowProviderScheduleRequest{
		ScheduleId:   req.ScheduleID,
		Cron:         req.Cron,
		Timezone:     req.Timezone,
		Target:       target,
		Paused:       req.Paused,
		RequestedBy:  workflowActorToProto(req.RequestedBy),
		ExecutionRef: req.ExecutionRef,
	}
	ctx, cancel := workflowProviderRequestContext(ctx, pbReq)
	defer cancel()
	resp, err := r.client.UpsertSchedule(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	return workflowScheduleFromProto(resp)
}

func (r *remoteWorkflow) GetSchedule(ctx context.Context, req coreworkflow.GetScheduleRequest) (schedule *coreworkflow.Schedule, err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationGetSchedule, workflowDims{triggerKind: observability.WorkflowTriggerKindSchedule})
	defer func() { end(err) }()
	pbReq := &proto.GetWorkflowProviderScheduleRequest{ScheduleId: req.ScheduleID}
	ctx, cancel := workflowProviderRequestContext(ctx, pbReq)
	defer cancel()
	resp, err := r.client.GetSchedule(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	return workflowScheduleFromProto(resp)
}

func (r *remoteWorkflow) ListSchedules(ctx context.Context, req coreworkflow.ListSchedulesRequest) (schedules []*coreworkflow.Schedule, err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationListSchedules, workflowDims{triggerKind: observability.WorkflowTriggerKindSchedule})
	defer func() { end(err) }()
	pbReq := &proto.ListWorkflowProviderSchedulesRequest{}
	ctx, cancel := workflowProviderRequestContext(ctx, pbReq)
	defer cancel()
	resp, err := r.client.ListSchedules(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	schedules = make([]*coreworkflow.Schedule, 0, len(resp.GetSchedules()))
	for _, schedule := range resp.GetSchedules() {
		value, err := workflowScheduleFromProto(schedule)
		if err != nil {
			return nil, err
		}
		schedules = append(schedules, value)
	}
	return schedules, nil
}

func (r *remoteWorkflow) DeleteSchedule(ctx context.Context, req coreworkflow.DeleteScheduleRequest) (err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationDeleteSchedule, workflowDims{triggerKind: observability.WorkflowTriggerKindSchedule})
	defer func() { end(err) }()
	pbReq := &proto.DeleteWorkflowProviderScheduleRequest{ScheduleId: req.ScheduleID}
	ctx, cancel := workflowProviderRequestContext(ctx, pbReq)
	defer cancel()
	_, err = r.client.DeleteSchedule(ctx, pbReq)
	return err
}

func (r *remoteWorkflow) PauseSchedule(ctx context.Context, req coreworkflow.PauseScheduleRequest) (schedule *coreworkflow.Schedule, err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationPauseSchedule, workflowDims{triggerKind: observability.WorkflowTriggerKindSchedule})
	defer func() { end(err) }()
	pbReq := &proto.PauseWorkflowProviderScheduleRequest{ScheduleId: req.ScheduleID}
	ctx, cancel := workflowProviderRequestContext(ctx, pbReq)
	defer cancel()
	resp, err := r.client.PauseSchedule(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	return workflowScheduleFromProto(resp)
}

func (r *remoteWorkflow) ResumeSchedule(ctx context.Context, req coreworkflow.ResumeScheduleRequest) (schedule *coreworkflow.Schedule, err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationResumeSchedule, workflowDims{triggerKind: observability.WorkflowTriggerKindSchedule})
	defer func() { end(err) }()
	pbReq := &proto.ResumeWorkflowProviderScheduleRequest{ScheduleId: req.ScheduleID}
	ctx, cancel := workflowProviderRequestContext(ctx, pbReq)
	defer cancel()
	resp, err := r.client.ResumeSchedule(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	return workflowScheduleFromProto(resp)
}

func (r *remoteWorkflow) UpsertEventTrigger(ctx context.Context, req coreworkflow.UpsertEventTriggerRequest) (trigger *coreworkflow.EventTrigger, err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationUpsertEventTrigger, workflowDims{
		triggerKind: observability.WorkflowTriggerKindEvent,
		targetKind:  workflowTargetKind(req.Target),
	})
	defer func() { end(err) }()
	target, err := workflowTargetToProto(req.Target)
	if err != nil {
		return nil, err
	}
	pbReq := &proto.UpsertWorkflowProviderEventTriggerRequest{
		TriggerId:    req.TriggerID,
		Match:        workflowEventMatchToProto(req.Match),
		Target:       target,
		Paused:       req.Paused,
		RequestedBy:  workflowActorToProto(req.RequestedBy),
		ExecutionRef: req.ExecutionRef,
	}
	ctx, cancel := workflowProviderRequestContext(ctx, pbReq)
	defer cancel()
	resp, err := r.client.UpsertEventTrigger(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	return workflowEventTriggerFromProto(resp)
}

func (r *remoteWorkflow) GetEventTrigger(ctx context.Context, req coreworkflow.GetEventTriggerRequest) (trigger *coreworkflow.EventTrigger, err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationGetEventTrigger, workflowDims{triggerKind: observability.WorkflowTriggerKindEvent})
	defer func() { end(err) }()
	pbReq := &proto.GetWorkflowProviderEventTriggerRequest{TriggerId: req.TriggerID}
	ctx, cancel := workflowProviderRequestContext(ctx, pbReq)
	defer cancel()
	resp, err := r.client.GetEventTrigger(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	return workflowEventTriggerFromProto(resp)
}

func (r *remoteWorkflow) ListEventTriggers(ctx context.Context, req coreworkflow.ListEventTriggersRequest) (triggers []*coreworkflow.EventTrigger, err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationListEventTriggers, workflowDims{triggerKind: observability.WorkflowTriggerKindEvent})
	defer func() { end(err) }()
	pbReq := &proto.ListWorkflowProviderEventTriggersRequest{}
	ctx, cancel := workflowProviderRequestContext(ctx, pbReq)
	defer cancel()
	resp, err := r.client.ListEventTriggers(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	triggers = make([]*coreworkflow.EventTrigger, 0, len(resp.GetTriggers()))
	for _, trigger := range resp.GetTriggers() {
		value, err := workflowEventTriggerFromProto(trigger)
		if err != nil {
			return nil, err
		}
		triggers = append(triggers, value)
	}
	return triggers, nil
}

func (r *remoteWorkflow) DeleteEventTrigger(ctx context.Context, req coreworkflow.DeleteEventTriggerRequest) (err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationDeleteEventTrigger, workflowDims{triggerKind: observability.WorkflowTriggerKindEvent})
	defer func() { end(err) }()
	pbReq := &proto.DeleteWorkflowProviderEventTriggerRequest{TriggerId: req.TriggerID}
	ctx, cancel := workflowProviderRequestContext(ctx, pbReq)
	defer cancel()
	_, err = r.client.DeleteEventTrigger(ctx, pbReq)
	return err
}

func (r *remoteWorkflow) PauseEventTrigger(ctx context.Context, req coreworkflow.PauseEventTriggerRequest) (trigger *coreworkflow.EventTrigger, err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationPauseEventTrigger, workflowDims{triggerKind: observability.WorkflowTriggerKindEvent})
	defer func() { end(err) }()
	pbReq := &proto.PauseWorkflowProviderEventTriggerRequest{TriggerId: req.TriggerID}
	ctx, cancel := workflowProviderRequestContext(ctx, pbReq)
	defer cancel()
	resp, err := r.client.PauseEventTrigger(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	return workflowEventTriggerFromProto(resp)
}

func (r *remoteWorkflow) ResumeEventTrigger(ctx context.Context, req coreworkflow.ResumeEventTriggerRequest) (trigger *coreworkflow.EventTrigger, err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationResumeEventTrigger, workflowDims{triggerKind: observability.WorkflowTriggerKindEvent})
	defer func() { end(err) }()
	pbReq := &proto.ResumeWorkflowProviderEventTriggerRequest{TriggerId: req.TriggerID}
	ctx, cancel := workflowProviderRequestContext(ctx, pbReq)
	defer cancel()
	resp, err := r.client.ResumeEventTrigger(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	return workflowEventTriggerFromProto(resp)
}

func (r *remoteWorkflow) PublishEvent(ctx context.Context, req coreworkflow.PublishEventRequest) (out *coreworkflow.Event, err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationPublishEvent, workflowDims{triggerKind: observability.WorkflowTriggerKindEvent})
	metricCtx := ctx
	defer func() {
		end(err)
		observability.RecordWorkflowEventPublished(metricCtx, err, r.workflowMetricDims(observability.WorkflowOperationPublishEvent, workflowDims{triggerKind: observability.WorkflowTriggerKindEvent}))
	}()
	pbEvent, err := workflowEventToProto(req.Event)
	if err != nil {
		return nil, err
	}
	pbReq := &proto.PublishWorkflowProviderEventRequest{
		AppName:     req.AppName,
		Event:       pbEvent,
		PublishedBy: workflowActorToProto(req.PublishedBy),
	}
	ctx, cancel := workflowProviderRequestContext(ctx, pbReq)
	defer cancel()
	resp, err := r.client.PublishEvent(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	event, err := workflowEventFromProto(resp)
	if err != nil {
		return nil, err
	}
	return &event, nil
}

func (r *remoteWorkflow) PutExecutionReference(ctx context.Context, ref *coreworkflow.ExecutionReference) (out *coreworkflow.ExecutionReference, err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationPutExecutionReference, workflowDims{targetKind: workflowTargetKind(workflowExecutionReferenceTarget(ref))})
	defer func() { end(err) }()
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	pbRef, err := workflowExecutionReferenceToProto(ref)
	if err != nil {
		return nil, err
	}
	resp, err := r.client.PutExecutionReference(ctx, &proto.PutWorkflowExecutionReferenceRequest{Reference: pbRef})
	if err != nil {
		return nil, err
	}
	return workflowExecutionReferenceFromProto(resp)
}

func (r *remoteWorkflow) GetExecutionReference(ctx context.Context, id string) (ref *coreworkflow.ExecutionReference, err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationGetExecutionReference, workflowDims{})
	defer func() { end(err) }()
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := r.client.GetExecutionReference(ctx, &proto.GetWorkflowExecutionReferenceRequest{Id: id})
	if err != nil {
		return nil, err
	}
	return workflowExecutionReferenceFromProto(resp)
}

func (r *remoteWorkflow) ListExecutionReferences(ctx context.Context, subjectID string) (refs []*coreworkflow.ExecutionReference, err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationListExecutionReferences, workflowDims{})
	defer func() { end(err) }()
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := r.client.ListExecutionReferences(ctx, &proto.ListWorkflowExecutionReferencesRequest{SubjectId: subjectID})
	if err != nil {
		return nil, err
	}
	refs = make([]*coreworkflow.ExecutionReference, 0, len(resp.GetReferences()))
	for _, ref := range resp.GetReferences() {
		value, err := workflowExecutionReferenceFromProto(ref)
		if err != nil {
			return nil, err
		}
		refs = append(refs, value)
	}
	return refs, nil
}

func (r *remoteWorkflow) Ping(ctx context.Context) (err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationPing, workflowDims{})
	defer func() { end(err) }()
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	_, err = r.runtime.HealthCheck(ctx, &emptypb.Empty{})
	return err
}

func (r *remoteWorkflow) Start(ctx context.Context) error {
	return runtimehost.StartRuntimeProvider(ctx, r.runtime)
}

func (r *remoteWorkflow) Close() error {
	if r == nil || r.closer == nil {
		return nil
	}
	return r.closer.Close()
}

type workflowDims struct {
	triggerKind string
	targetKind  string
	runStatus   string
}

func (r *remoteWorkflow) startProviderOperation(ctx context.Context, operation string, dims workflowDims) (context.Context, func(error)) {
	metricDims := r.workflowMetricDims(operation, dims)
	startedAt := time.Now()
	ctx, span := observability.StartSpan(ctx, "workflow.provider.operation", observability.WorkflowMetricAttributes(metricDims)...)
	return ctx, func(err error) {
		observability.EndSpan(span, err)
		observability.RecordWorkflowProviderOperation(ctx, startedAt, err, metricDims)
	}
}

func (r *remoteWorkflow) workflowMetricDims(operation string, dims workflowDims) observability.WorkflowMetricDims {
	providerName := ""
	if r != nil {
		providerName = r.name
	}
	return observability.WorkflowMetricDims{
		ProviderName:    providerName,
		OperationName:   operation,
		TriggerKind:     dims.triggerKind,
		TargetKind:      dims.targetKind,
		RunStatus:       dims.runStatus,
		TelemetrySource: observability.WorkflowTelemetrySourceCore,
	}
}

func workflowTargetKind(target coreworkflow.Target) string {
	if len(target.Steps) > 0 {
		return observability.WorkflowTargetKindSteps
	}
	return observability.WorkflowTargetKindUnknown
}

func workflowProviderRequestContext(ctx context.Context, req gproto.Message) (context.Context, context.CancelFunc) {
	attachWorkflowProviderInvocationToken(ctx, req)
	return runtimehost.ProviderCallContext(ctx)
}

func attachWorkflowProviderInvocationToken(ctx context.Context, req gproto.Message) {
	if req == nil {
		return
	}
	token := strings.TrimSpace(appaccessservice.InvocationTokenFromContext(ctx))
	if token == "" {
		return
	}
	msg := req.ProtoReflect()
	field := msg.Descriptor().Fields().ByName(protoreflect.Name("invocation_token"))
	if field == nil || field.Kind() != protoreflect.StringKind {
		return
	}
	msg.Set(field, protoreflect.ValueOfString(token))
}

func workflowExecutionReferenceTarget(ref *coreworkflow.ExecutionReference) coreworkflow.Target {
	if ref == nil {
		return coreworkflow.Target{}
	}
	return ref.Target
}

func workflowRunStatusFromCore(run *coreworkflow.Run) string {
	if run == nil {
		return observability.WorkflowRunStatusUnknown
	}
	switch run.Status {
	case coreworkflow.RunStatusPending:
		return observability.WorkflowRunStatusPending
	case coreworkflow.RunStatusRunning:
		return observability.WorkflowRunStatusRunning
	case coreworkflow.RunStatusSucceeded:
		return observability.WorkflowRunStatusSucceeded
	case coreworkflow.RunStatusFailed:
		return observability.WorkflowRunStatusFailed
	case coreworkflow.RunStatusCanceled:
		return observability.WorkflowRunStatusCanceled
	default:
		return observability.WorkflowRunStatusUnknown
	}
}

var _ coreworkflow.Provider = (*remoteWorkflow)(nil)
var _ coreworkflow.ExecutionReferenceStore = (*remoteWorkflow)(nil)
