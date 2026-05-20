package workflows

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	proto "github.com/valon-technologies/gestalt/server/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/observability"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

var startWorkflowProviderProcess = runtimehost.StartPluginProcess

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

func (r *remoteWorkflow) PlanWorkflow(ctx context.Context, req coreworkflow.PlanWorkflowRequest) (out *coreworkflow.CompileTargetResponse, err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationPlanWorkflow, workflowDims{targetKind: workflowTargetKind(req.Spec.Target)})
	defer func() { end(err) }()
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	pbReq, err := workflowPlanRequestToProto(req)
	if err != nil {
		return nil, err
	}
	resp, err := r.client.PlanWorkflow(ctx, pbReq)
	if err != nil {
		return nil, err
	}
	return workflowPlanResponseFromProto(resp), nil
}

func (r *remoteWorkflow) ApplyWorkflowDeployment(ctx context.Context, req coreworkflow.ApplyDeploymentRequest) (deployment *coreworkflow.Deployment, err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationApplyWorkflowDeployment, workflowDims{targetKind: workflowTargetKind(req.Spec.Target)})
	defer func() { end(err) }()
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	spec, err := workflowDeploymentSpecToProto(req.Spec)
	if err != nil {
		return nil, err
	}
	resp, err := r.client.ApplyWorkflowDeployment(ctx, &proto.ApplyWorkflowDeploymentRequest{
		Spec:         spec,
		Plan:         workflowPlanResponseToProto(req.Plan),
		Binding:      workflowDeploymentBindingToProto(req.Binding),
		RequestId:    req.RequestID,
		ValidateOnly: req.ValidateOnly,
	})
	if err != nil {
		return nil, err
	}
	return workflowDeploymentFromProto(resp)
}

func (r *remoteWorkflow) GetWorkflowDeployment(ctx context.Context, req coreworkflow.GetDeploymentRequest) (deployment *coreworkflow.Deployment, err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationGetWorkflowDeployment, workflowDims{})
	defer func() { end(err) }()
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := r.client.GetWorkflowDeployment(ctx, &proto.GetWorkflowDeploymentRequest{DeploymentId: req.DeploymentID})
	if err != nil {
		return nil, err
	}
	return workflowDeploymentFromProto(resp)
}

func (r *remoteWorkflow) ListWorkflowDeployments(ctx context.Context, req coreworkflow.ListDeploymentsRequest) (out *coreworkflow.ListDeploymentsResponse, err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationListWorkflowDeployments, workflowDims{})
	defer func() { end(err) }()
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := r.client.ListWorkflowDeployments(ctx, &proto.ListWorkflowDeploymentsRequest{
		PageSize:  int32(req.PageSize),
		PageToken: strings.TrimSpace(req.PageToken),
		Labels:    req.Labels,
	})
	if err != nil {
		return nil, err
	}
	deployments := make([]*coreworkflow.Deployment, 0, len(resp.GetDeployments()))
	for _, deployment := range resp.GetDeployments() {
		value, err := workflowDeploymentFromProto(deployment)
		if err != nil {
			return nil, err
		}
		deployments = append(deployments, value)
	}
	return &coreworkflow.ListDeploymentsResponse{
		Deployments:   deployments,
		NextPageToken: strings.TrimSpace(resp.GetNextPageToken()),
	}, nil
}

func (r *remoteWorkflow) DeleteWorkflowDeployment(ctx context.Context, req coreworkflow.DeleteDeploymentRequest) (err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationDeleteWorkflowDeployment, workflowDims{})
	defer func() { end(err) }()
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	_, err = r.client.DeleteWorkflowDeployment(ctx, &proto.DeleteWorkflowDeploymentRequest{
		DeploymentId: req.DeploymentID,
		Generation:   req.Generation,
		RequestId:    req.RequestID,
	})
	return err
}

func (r *remoteWorkflow) SetWorkflowDeploymentPaused(ctx context.Context, req coreworkflow.SetDeploymentPausedRequest) (deployment *coreworkflow.Deployment, err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationSetWorkflowDeploymentPaused, workflowDims{})
	defer func() { end(err) }()
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := r.client.SetWorkflowDeploymentPaused(ctx, &proto.SetWorkflowDeploymentPausedRequest{
		DeploymentId: req.DeploymentID,
		Paused:       req.Paused,
		RequestId:    req.RequestID,
	})
	if err != nil {
		return nil, err
	}
	return workflowDeploymentFromProto(resp)
}

func (r *remoteWorkflow) SetWorkflowActivationPaused(ctx context.Context, req coreworkflow.SetActivationPausedRequest) (deployment *coreworkflow.Deployment, err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationSetWorkflowActivationPaused, workflowDims{})
	defer func() { end(err) }()
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := r.client.SetWorkflowActivationPaused(ctx, &proto.SetWorkflowActivationPausedRequest{
		DeploymentId: req.DeploymentID,
		ActivationId: req.ActivationID,
		Paused:       req.Paused,
		RequestId:    req.RequestID,
	})
	if err != nil {
		return nil, err
	}
	return workflowDeploymentFromProto(resp)
}

func (r *remoteWorkflow) StartRun(ctx context.Context, req coreworkflow.StartRunRequest) (run *coreworkflow.Run, err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationStartRun, workflowDims{
		triggerKind: observability.WorkflowTriggerKindManual,
		targetKind:  workflowTargetKind(req.Target),
	})
	defer func() { end(err) }()
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	input, err := structFromMap(req.Input)
	if err != nil {
		return nil, err
	}
	resp, err := r.client.StartWorkflowRun(ctx, &proto.StartWorkflowRunRequest{
		DeploymentId:         req.DeploymentID,
		DeploymentGeneration: req.DeploymentGeneration,
		ActivationId:         req.ActivationID,
		WorkflowKey:          req.WorkflowKey,
		Input:                input,
		IdempotencyKey:       req.IdempotencyKey,
		CreatedBy:            workflowActorToProto(req.CreatedBy),
	})
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
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := r.client.GetWorkflowRun(ctx, &proto.GetWorkflowRunRequest{RunId: req.RunID})
	if err != nil {
		return nil, err
	}
	return workflowRunFromProto(resp)
}

func (r *remoteWorkflow) ListRuns(ctx context.Context, req coreworkflow.ListRunsRequest) (out *coreworkflow.ListRunsResponse, err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationListRuns, workflowDims{})
	defer func() { end(err) }()
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := r.client.ListWorkflowRuns(ctx, &proto.ListWorkflowRunsRequest{
		DeploymentId: strings.TrimSpace(req.DeploymentID),
		PageSize:     int32(req.PageSize),
		PageToken:    strings.TrimSpace(req.PageToken),
		Status:       workflowRunStatusToProto(req.Status),
	})
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
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := r.client.CancelWorkflowRun(ctx, &proto.CancelWorkflowRunRequest{
		RunId:  req.RunID,
		Reason: req.Reason,
	})
	if err != nil {
		return nil, err
	}
	return workflowRunFromProto(resp)
}

func (r *remoteWorkflow) SignalRun(ctx context.Context, req coreworkflow.SignalRunRequest) (out *coreworkflow.SignalRunResponse, err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationSignalRun, workflowDims{triggerKind: observability.WorkflowTriggerKindSignal})
	defer func() { end(err) }()
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	signal, err := workflowSignalToProto(req.Signal)
	if err != nil {
		return nil, err
	}
	resp, err := r.client.SignalWorkflowRun(ctx, &proto.SignalWorkflowRunRequest{
		RunId:  req.RunID,
		Signal: signal,
	})
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
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	input, err := structFromMap(req.Input)
	if err != nil {
		return nil, err
	}
	signal, err := workflowSignalToProto(req.Signal)
	if err != nil {
		return nil, err
	}
	resp, err := r.client.SignalOrStartWorkflowRun(ctx, &proto.SignalOrStartWorkflowRunRequest{
		DeploymentId:         req.DeploymentID,
		DeploymentGeneration: req.DeploymentGeneration,
		ActivationId:         req.ActivationID,
		WorkflowKey:          req.WorkflowKey,
		Input:                input,
		IdempotencyKey:       req.IdempotencyKey,
		Signal:               signal,
		CreatedBy:            workflowActorToProto(req.CreatedBy),
	})
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

func (r *remoteWorkflow) DeliverWorkflowEvent(ctx context.Context, req coreworkflow.PublishEventRequest) (out *coreworkflow.DeliverEventResponse, err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationDeliverWorkflowEvent, workflowDims{triggerKind: observability.WorkflowTriggerKindEvent})
	defer func() { end(err) }()
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	pbEvent, err := workflowEventToProto(req.Event)
	if err != nil {
		return nil, err
	}
	resp, err := r.client.DeliverWorkflowEvent(ctx, &proto.DeliverWorkflowEventRequest{
		DeliveryId:     req.DeliveryID,
		Event:          pbEvent,
		PublishedBy:    workflowActorToProto(req.PublishedBy),
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		return nil, err
	}
	results, err := workflowEventDeliveryResultsFromProto(resp.GetResults())
	if err != nil {
		return nil, err
	}
	return &coreworkflow.DeliverEventResponse{Results: results}, nil
}

func (r *remoteWorkflow) PublishEvent(ctx context.Context, req coreworkflow.PublishEventRequest) (err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationPublishEvent, workflowDims{triggerKind: observability.WorkflowTriggerKindEvent})
	metricCtx := ctx
	defer func() {
		end(err)
		observability.RecordWorkflowEventPublished(metricCtx, err, r.workflowMetricDims(observability.WorkflowOperationPublishEvent, workflowDims{triggerKind: observability.WorkflowTriggerKindEvent}))
	}()
	_, err = r.DeliverWorkflowEvent(ctx, req)
	return err
}

func (r *remoteWorkflow) GetWorkflowRunEvents(ctx context.Context, req coreworkflow.GetRunEventsRequest) (out *coreworkflow.ListRunEventsResponse, err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationGetWorkflowRunEvents, workflowDims{})
	defer func() { end(err) }()
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := r.client.GetWorkflowRunEvents(ctx, &proto.GetWorkflowRunEventsRequest{
		RunId:     req.RunID,
		PageSize:  int32(req.PageSize),
		PageToken: strings.TrimSpace(req.PageToken),
	})
	if err != nil {
		return nil, err
	}
	return &coreworkflow.ListRunEventsResponse{
		Events:        workflowRunEventsFromProto(resp.GetEvents()),
		NextPageToken: strings.TrimSpace(resp.GetNextPageToken()),
	}, nil
}

func (r *remoteWorkflow) GetWorkflowRunOutput(ctx context.Context, req coreworkflow.GetRunOutputRequest) (out *coreworkflow.RunOutput, err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationGetWorkflowRunOutput, workflowDims{})
	defer func() { end(err) }()
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := r.client.GetWorkflowRunOutput(ctx, &proto.GetWorkflowRunOutputRequest{
		RunId:     req.RunID,
		OutputRef: req.OutputRef,
		StepId:    req.StepID,
	})
	if err != nil {
		return nil, err
	}
	value := workflowRunOutputFromProto(resp)
	return &value, nil
}

func (r *remoteWorkflow) UpsertSchedule(context.Context, coreworkflow.UpsertScheduleRequest) (*coreworkflow.Schedule, error) {
	return nil, removedWorkflowProviderRPC("upsert schedule")
}

func (r *remoteWorkflow) GetSchedule(context.Context, coreworkflow.GetScheduleRequest) (*coreworkflow.Schedule, error) {
	return nil, removedWorkflowProviderRPC("get schedule")
}

func (r *remoteWorkflow) ListSchedules(context.Context, coreworkflow.ListSchedulesRequest) ([]*coreworkflow.Schedule, error) {
	return nil, removedWorkflowProviderRPC("list schedules")
}

func (r *remoteWorkflow) DeleteSchedule(context.Context, coreworkflow.DeleteScheduleRequest) error {
	return removedWorkflowProviderRPC("delete schedule")
}

func (r *remoteWorkflow) PauseSchedule(context.Context, coreworkflow.PauseScheduleRequest) (*coreworkflow.Schedule, error) {
	return nil, removedWorkflowProviderRPC("pause schedule")
}

func (r *remoteWorkflow) ResumeSchedule(context.Context, coreworkflow.ResumeScheduleRequest) (*coreworkflow.Schedule, error) {
	return nil, removedWorkflowProviderRPC("resume schedule")
}

func (r *remoteWorkflow) UpsertEventTrigger(context.Context, coreworkflow.UpsertEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return nil, removedWorkflowProviderRPC("upsert event trigger")
}

func (r *remoteWorkflow) GetEventTrigger(context.Context, coreworkflow.GetEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return nil, removedWorkflowProviderRPC("get event trigger")
}

func (r *remoteWorkflow) ListEventTriggers(context.Context, coreworkflow.ListEventTriggersRequest) ([]*coreworkflow.EventTrigger, error) {
	return nil, removedWorkflowProviderRPC("list event triggers")
}

func (r *remoteWorkflow) DeleteEventTrigger(context.Context, coreworkflow.DeleteEventTriggerRequest) error {
	return removedWorkflowProviderRPC("delete event trigger")
}

func (r *remoteWorkflow) PauseEventTrigger(context.Context, coreworkflow.PauseEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return nil, removedWorkflowProviderRPC("pause event trigger")
}

func (r *remoteWorkflow) ResumeEventTrigger(context.Context, coreworkflow.ResumeEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return nil, removedWorkflowProviderRPC("resume event trigger")
}

func removedWorkflowProviderRPC(operation string) error {
	return status.Errorf(codes.Unimplemented, "workflow provider %s RPC is no longer part of the provider contract", operation)
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
	switch {
	case len(target.Steps) > 0:
		return observability.WorkflowTargetKindSteps
	default:
		return observability.WorkflowTargetKindUnknown
	}
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
var _ coreworkflow.DeploymentProvider = (*remoteWorkflow)(nil)
var _ coreworkflow.ExecutionReferenceStore = (*remoteWorkflow)(nil)

func (r *remoteWorkflow) PutExecutionReference(ctx context.Context, ref *coreworkflow.ExecutionReference) (out *coreworkflow.ExecutionReference, err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationPutExecutionReference, workflowDims{})
	defer func() { end(err) }()
	reqRef, err := workflowExecutionReferenceToProto(ref)
	if err != nil {
		return nil, err
	}
	resp, err := r.client.PutExecutionReference(ctx, &proto.PutWorkflowExecutionReferenceRequest{
		ExecutionRef: reqRef,
	})
	if err != nil {
		return nil, err
	}
	return workflowExecutionReferenceFromProto(resp), nil
}

func (r *remoteWorkflow) GetExecutionReference(ctx context.Context, id string) (out *coreworkflow.ExecutionReference, err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationGetExecutionReference, workflowDims{})
	defer func() { end(err) }()
	resp, err := r.client.GetExecutionReference(ctx, &proto.GetWorkflowExecutionReferenceRequest{Id: id})
	if err != nil {
		return nil, err
	}
	return workflowExecutionReferenceFromProto(resp), nil
}

func (r *remoteWorkflow) ListExecutionReferences(ctx context.Context, subjectID string) (out []*coreworkflow.ExecutionReference, err error) {
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationListExecutionReferences, workflowDims{})
	defer func() { end(err) }()
	resp, err := r.client.ListExecutionReferences(ctx, &proto.ListWorkflowExecutionReferencesRequest{SubjectId: subjectID})
	if err != nil {
		return nil, err
	}
	refs := resp.GetExecutionRefs()
	out = make([]*coreworkflow.ExecutionReference, 0, len(refs))
	for _, ref := range refs {
		out = append(out, workflowExecutionReferenceFromProto(ref))
	}
	return out, nil
}
