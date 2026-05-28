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

func (r *remoteWorkflow) CreateDefinition(ctx context.Context, req *proto.CreateWorkflowProviderDefinitionRequest) (definition *proto.BoundWorkflowDefinition, err error) {
	req = cloneWorkflowRequest(req, &proto.CreateWorkflowProviderDefinitionRequest{}).(*proto.CreateWorkflowProviderDefinitionRequest)
	req.ProviderName = r.name
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationCreateDefinition, workflowDims{targetKind: workflowProtoTargetKind(req.GetTarget())})
	defer func() { end(err) }()
	ctx, cancel := workflowProviderRequestContext(ctx, req)
	defer cancel()
	return r.client.CreateDefinition(ctx, req)
}

func (r *remoteWorkflow) GetDefinition(ctx context.Context, req *proto.GetWorkflowProviderDefinitionRequest) (definition *proto.BoundWorkflowDefinition, err error) {
	req = cloneWorkflowRequest(req, &proto.GetWorkflowProviderDefinitionRequest{}).(*proto.GetWorkflowProviderDefinitionRequest)
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationGetDefinition, workflowDims{})
	defer func() { end(err) }()
	ctx, cancel := workflowProviderRequestContext(ctx, req)
	defer cancel()
	return r.client.GetDefinition(ctx, req)
}

func (r *remoteWorkflow) UpdateDefinition(ctx context.Context, req *proto.UpdateWorkflowProviderDefinitionRequest) (definition *proto.BoundWorkflowDefinition, err error) {
	req = cloneWorkflowRequest(req, &proto.UpdateWorkflowProviderDefinitionRequest{}).(*proto.UpdateWorkflowProviderDefinitionRequest)
	req.ProviderName = r.name
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationUpdateDefinition, workflowDims{targetKind: workflowProtoTargetKind(req.GetTarget())})
	defer func() { end(err) }()
	ctx, cancel := workflowProviderRequestContext(ctx, req)
	defer cancel()
	return r.client.UpdateDefinition(ctx, req)
}

func (r *remoteWorkflow) DeleteDefinition(ctx context.Context, req *proto.DeleteWorkflowProviderDefinitionRequest) (err error) {
	req = cloneWorkflowRequest(req, &proto.DeleteWorkflowProviderDefinitionRequest{}).(*proto.DeleteWorkflowProviderDefinitionRequest)
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationDeleteDefinition, workflowDims{})
	defer func() { end(err) }()
	ctx, cancel := workflowProviderRequestContext(ctx, req)
	defer cancel()
	_, err = r.client.DeleteDefinition(ctx, req)
	return err
}

func (r *remoteWorkflow) StartRun(ctx context.Context, req *proto.StartWorkflowProviderRunRequest) (run *proto.BoundWorkflowRun, err error) {
	req = cloneWorkflowRequest(req, &proto.StartWorkflowProviderRunRequest{}).(*proto.StartWorkflowProviderRunRequest)
	req.ProviderName = r.name
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationStartRun, workflowDims{
		triggerKind: observability.WorkflowTriggerKindManual,
		targetKind:  workflowProtoTargetKind(req.GetTarget()),
	})
	defer func() { end(err) }()
	ctx, cancel := workflowProviderRequestContext(ctx, req)
	defer cancel()
	run, err = r.client.StartRun(ctx, req)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.GetIdempotencyKey()) == "" {
		observability.RecordWorkflowRunStarted(ctx, r.workflowMetricDims(observability.WorkflowOperationStartRun, workflowDims{
			triggerKind: observability.WorkflowTriggerKindManual,
			targetKind:  workflowProtoTargetKind(req.GetTarget()),
			runStatus:   workflowRunStatusMetric(run),
		}))
	}
	return run, nil
}

func (r *remoteWorkflow) GetRun(ctx context.Context, req *proto.GetWorkflowProviderRunRequest) (run *proto.BoundWorkflowRun, err error) {
	req = cloneWorkflowRequest(req, &proto.GetWorkflowProviderRunRequest{}).(*proto.GetWorkflowProviderRunRequest)
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationGetRun, workflowDims{})
	defer func() { end(err) }()
	ctx, cancel := workflowProviderRequestContext(ctx, req)
	defer cancel()
	return r.client.GetRun(ctx, req)
}

func (r *remoteWorkflow) ListRuns(ctx context.Context, req *proto.ListWorkflowProviderRunsRequest) (out *proto.ListWorkflowProviderRunsResponse, err error) {
	req = cloneWorkflowRequest(req, &proto.ListWorkflowProviderRunsRequest{}).(*proto.ListWorkflowProviderRunsRequest)
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationListRuns, workflowDims{})
	defer func() { end(err) }()
	ctx, cancel := workflowProviderRequestContext(ctx, req)
	defer cancel()
	return r.client.ListRuns(ctx, req)
}

func (r *remoteWorkflow) CancelRun(ctx context.Context, req *proto.CancelWorkflowProviderRunRequest) (run *proto.BoundWorkflowRun, err error) {
	req = cloneWorkflowRequest(req, &proto.CancelWorkflowProviderRunRequest{}).(*proto.CancelWorkflowProviderRunRequest)
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationCancelRun, workflowDims{})
	defer func() { end(err) }()
	ctx, cancel := workflowProviderRequestContext(ctx, req)
	defer cancel()
	return r.client.CancelRun(ctx, req)
}

func (r *remoteWorkflow) SignalRun(ctx context.Context, req *proto.SignalWorkflowProviderRunRequest) (out *proto.SignalWorkflowRunResponse, err error) {
	req = cloneWorkflowRequest(req, &proto.SignalWorkflowProviderRunRequest{}).(*proto.SignalWorkflowProviderRunRequest)
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationSignalRun, workflowDims{triggerKind: observability.WorkflowTriggerKindSignal})
	defer func() { end(err) }()
	ctx, cancel := workflowProviderRequestContext(ctx, req)
	defer cancel()
	return r.client.SignalRun(ctx, req)
}

func (r *remoteWorkflow) SignalOrStartRun(ctx context.Context, req *proto.SignalOrStartWorkflowProviderRunRequest) (out *proto.SignalWorkflowRunResponse, err error) {
	req = cloneWorkflowRequest(req, &proto.SignalOrStartWorkflowProviderRunRequest{}).(*proto.SignalOrStartWorkflowProviderRunRequest)
	req.ProviderName = r.name
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationSignalOrStartRun, workflowDims{
		triggerKind: observability.WorkflowTriggerKindSignal,
		targetKind:  workflowProtoTargetKind(req.GetTarget()),
	})
	defer func() { end(err) }()
	ctx, cancel := workflowProviderRequestContext(ctx, req)
	defer cancel()
	out, err = r.client.SignalOrStartRun(ctx, req)
	if err != nil {
		return nil, err
	}
	if out != nil && out.GetStartedRun() {
		dims := r.workflowMetricDims(observability.WorkflowOperationSignalOrStartRun, workflowDims{
			triggerKind: observability.WorkflowTriggerKindSignal,
			targetKind:  workflowProtoTargetKind(req.GetTarget()),
			runStatus:   workflowRunStatusMetric(out.GetRun()),
		})
		observability.RecordWorkflowRunStarted(ctx, dims)
	}
	return out, nil
}

func (r *remoteWorkflow) UpsertSchedule(ctx context.Context, req *proto.UpsertWorkflowProviderScheduleRequest) (schedule *proto.BoundWorkflowSchedule, err error) {
	req = cloneWorkflowRequest(req, &proto.UpsertWorkflowProviderScheduleRequest{}).(*proto.UpsertWorkflowProviderScheduleRequest)
	req.ProviderName = r.name
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationUpsertSchedule, workflowDims{
		triggerKind: observability.WorkflowTriggerKindSchedule,
		targetKind:  workflowProtoTargetKind(req.GetTarget()),
	})
	defer func() { end(err) }()
	ctx, cancel := workflowProviderRequestContext(ctx, req)
	defer cancel()
	return r.client.UpsertSchedule(ctx, req)
}

func (r *remoteWorkflow) GetSchedule(ctx context.Context, req *proto.GetWorkflowProviderScheduleRequest) (schedule *proto.BoundWorkflowSchedule, err error) {
	req = cloneWorkflowRequest(req, &proto.GetWorkflowProviderScheduleRequest{}).(*proto.GetWorkflowProviderScheduleRequest)
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationGetSchedule, workflowDims{triggerKind: observability.WorkflowTriggerKindSchedule})
	defer func() { end(err) }()
	ctx, cancel := workflowProviderRequestContext(ctx, req)
	defer cancel()
	return r.client.GetSchedule(ctx, req)
}

func (r *remoteWorkflow) ListSchedules(ctx context.Context, req *proto.ListWorkflowProviderSchedulesRequest) (schedules *proto.ListWorkflowProviderSchedulesResponse, err error) {
	req = cloneWorkflowRequest(req, &proto.ListWorkflowProviderSchedulesRequest{}).(*proto.ListWorkflowProviderSchedulesRequest)
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationListSchedules, workflowDims{triggerKind: observability.WorkflowTriggerKindSchedule})
	defer func() { end(err) }()
	ctx, cancel := workflowProviderRequestContext(ctx, req)
	defer cancel()
	return r.client.ListSchedules(ctx, req)
}

func (r *remoteWorkflow) DeleteSchedule(ctx context.Context, req *proto.DeleteWorkflowProviderScheduleRequest) (err error) {
	req = cloneWorkflowRequest(req, &proto.DeleteWorkflowProviderScheduleRequest{}).(*proto.DeleteWorkflowProviderScheduleRequest)
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationDeleteSchedule, workflowDims{triggerKind: observability.WorkflowTriggerKindSchedule})
	defer func() { end(err) }()
	ctx, cancel := workflowProviderRequestContext(ctx, req)
	defer cancel()
	_, err = r.client.DeleteSchedule(ctx, req)
	return err
}

func (r *remoteWorkflow) PauseSchedule(ctx context.Context, req *proto.PauseWorkflowProviderScheduleRequest) (schedule *proto.BoundWorkflowSchedule, err error) {
	req = cloneWorkflowRequest(req, &proto.PauseWorkflowProviderScheduleRequest{}).(*proto.PauseWorkflowProviderScheduleRequest)
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationPauseSchedule, workflowDims{triggerKind: observability.WorkflowTriggerKindSchedule})
	defer func() { end(err) }()
	ctx, cancel := workflowProviderRequestContext(ctx, req)
	defer cancel()
	return r.client.PauseSchedule(ctx, req)
}

func (r *remoteWorkflow) ResumeSchedule(ctx context.Context, req *proto.ResumeWorkflowProviderScheduleRequest) (schedule *proto.BoundWorkflowSchedule, err error) {
	req = cloneWorkflowRequest(req, &proto.ResumeWorkflowProviderScheduleRequest{}).(*proto.ResumeWorkflowProviderScheduleRequest)
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationResumeSchedule, workflowDims{triggerKind: observability.WorkflowTriggerKindSchedule})
	defer func() { end(err) }()
	ctx, cancel := workflowProviderRequestContext(ctx, req)
	defer cancel()
	return r.client.ResumeSchedule(ctx, req)
}

func (r *remoteWorkflow) UpsertEventTrigger(ctx context.Context, req *proto.UpsertWorkflowProviderEventTriggerRequest) (trigger *proto.BoundWorkflowEventTrigger, err error) {
	req = cloneWorkflowRequest(req, &proto.UpsertWorkflowProviderEventTriggerRequest{}).(*proto.UpsertWorkflowProviderEventTriggerRequest)
	req.ProviderName = r.name
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationUpsertEventTrigger, workflowDims{
		triggerKind: observability.WorkflowTriggerKindEvent,
		targetKind:  workflowProtoTargetKind(req.GetTarget()),
	})
	defer func() { end(err) }()
	ctx, cancel := workflowProviderRequestContext(ctx, req)
	defer cancel()
	return r.client.UpsertEventTrigger(ctx, req)
}

func (r *remoteWorkflow) GetEventTrigger(ctx context.Context, req *proto.GetWorkflowProviderEventTriggerRequest) (trigger *proto.BoundWorkflowEventTrigger, err error) {
	req = cloneWorkflowRequest(req, &proto.GetWorkflowProviderEventTriggerRequest{}).(*proto.GetWorkflowProviderEventTriggerRequest)
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationGetEventTrigger, workflowDims{triggerKind: observability.WorkflowTriggerKindEvent})
	defer func() { end(err) }()
	ctx, cancel := workflowProviderRequestContext(ctx, req)
	defer cancel()
	return r.client.GetEventTrigger(ctx, req)
}

func (r *remoteWorkflow) ListEventTriggers(ctx context.Context, req *proto.ListWorkflowProviderEventTriggersRequest) (triggers *proto.ListWorkflowProviderEventTriggersResponse, err error) {
	req = cloneWorkflowRequest(req, &proto.ListWorkflowProviderEventTriggersRequest{}).(*proto.ListWorkflowProviderEventTriggersRequest)
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationListEventTriggers, workflowDims{triggerKind: observability.WorkflowTriggerKindEvent})
	defer func() { end(err) }()
	ctx, cancel := workflowProviderRequestContext(ctx, req)
	defer cancel()
	return r.client.ListEventTriggers(ctx, req)
}

func (r *remoteWorkflow) DeleteEventTrigger(ctx context.Context, req *proto.DeleteWorkflowProviderEventTriggerRequest) (err error) {
	req = cloneWorkflowRequest(req, &proto.DeleteWorkflowProviderEventTriggerRequest{}).(*proto.DeleteWorkflowProviderEventTriggerRequest)
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationDeleteEventTrigger, workflowDims{triggerKind: observability.WorkflowTriggerKindEvent})
	defer func() { end(err) }()
	ctx, cancel := workflowProviderRequestContext(ctx, req)
	defer cancel()
	_, err = r.client.DeleteEventTrigger(ctx, req)
	return err
}

func (r *remoteWorkflow) PauseEventTrigger(ctx context.Context, req *proto.PauseWorkflowProviderEventTriggerRequest) (trigger *proto.BoundWorkflowEventTrigger, err error) {
	req = cloneWorkflowRequest(req, &proto.PauseWorkflowProviderEventTriggerRequest{}).(*proto.PauseWorkflowProviderEventTriggerRequest)
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationPauseEventTrigger, workflowDims{triggerKind: observability.WorkflowTriggerKindEvent})
	defer func() { end(err) }()
	ctx, cancel := workflowProviderRequestContext(ctx, req)
	defer cancel()
	return r.client.PauseEventTrigger(ctx, req)
}

func (r *remoteWorkflow) ResumeEventTrigger(ctx context.Context, req *proto.ResumeWorkflowProviderEventTriggerRequest) (trigger *proto.BoundWorkflowEventTrigger, err error) {
	req = cloneWorkflowRequest(req, &proto.ResumeWorkflowProviderEventTriggerRequest{}).(*proto.ResumeWorkflowProviderEventTriggerRequest)
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationResumeEventTrigger, workflowDims{triggerKind: observability.WorkflowTriggerKindEvent})
	defer func() { end(err) }()
	ctx, cancel := workflowProviderRequestContext(ctx, req)
	defer cancel()
	return r.client.ResumeEventTrigger(ctx, req)
}

func (r *remoteWorkflow) PublishEvent(ctx context.Context, req *proto.PublishWorkflowProviderEventRequest) (out *proto.WorkflowEvent, err error) {
	req = cloneWorkflowRequest(req, &proto.PublishWorkflowProviderEventRequest{}).(*proto.PublishWorkflowProviderEventRequest)
	req.ProviderName = r.name
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationPublishEvent, workflowDims{triggerKind: observability.WorkflowTriggerKindEvent})
	metricCtx := ctx
	defer func() {
		end(err)
		observability.RecordWorkflowEventPublished(metricCtx, err, r.workflowMetricDims(observability.WorkflowOperationPublishEvent, workflowDims{triggerKind: observability.WorkflowTriggerKindEvent}))
	}()
	ctx, cancel := workflowProviderRequestContext(ctx, req)
	defer cancel()
	return r.client.PublishEvent(ctx, req)
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

func cloneWorkflowRequest(req gproto.Message, empty gproto.Message) gproto.Message {
	if req == nil {
		return empty
	}
	return gproto.Clone(req)
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

func workflowRunStatusMetric(run *proto.BoundWorkflowRun) string {
	if run == nil {
		return observability.WorkflowRunStatusUnknown
	}
	switch run.GetStatus() {
	case proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_PENDING:
		return observability.WorkflowRunStatusPending
	case proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_RUNNING:
		return observability.WorkflowRunStatusRunning
	case proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_SUCCEEDED:
		return observability.WorkflowRunStatusSucceeded
	case proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_FAILED:
		return observability.WorkflowRunStatusFailed
	case proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_CANCELED:
		return observability.WorkflowRunStatusCanceled
	default:
		return observability.WorkflowRunStatusUnknown
	}
}

var _ coreworkflow.Provider = (*remoteWorkflow)(nil)
