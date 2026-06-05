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
	"github.com/valon-technologies/gestalt/server/services/invocation"
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

func (r *remoteWorkflow) ApplyDefinition(ctx context.Context, req *proto.ApplyWorkflowProviderDefinitionRequest) (definition *proto.WorkflowDefinition, err error) {
	req = cloneWorkflowRequest(req, &proto.ApplyWorkflowProviderDefinitionRequest{}).(*proto.ApplyWorkflowProviderDefinitionRequest)
	req.ProviderName = r.name
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationApplyDefinition, workflowDims{targetKind: workflowProtoTargetKind(req.GetSpec().GetTarget())})
	defer func() { end(err) }()
	ctx, cancel, err := workflowProviderRequestContext(ctx, req)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return r.client.ApplyDefinition(ctx, req)
}

func (r *remoteWorkflow) GetDefinition(ctx context.Context, req *proto.GetWorkflowProviderDefinitionRequest) (definition *proto.WorkflowDefinition, err error) {
	req = cloneWorkflowRequest(req, &proto.GetWorkflowProviderDefinitionRequest{}).(*proto.GetWorkflowProviderDefinitionRequest)
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationGetDefinition, workflowDims{})
	defer func() { end(err) }()
	ctx, cancel, err := workflowProviderRequestContext(ctx, req)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return r.client.GetDefinition(ctx, req)
}

func (r *remoteWorkflow) ListDefinitions(ctx context.Context, req *proto.ListWorkflowProviderDefinitionsRequest) (definitions *proto.ListWorkflowProviderDefinitionsResponse, err error) {
	req = cloneWorkflowRequest(req, &proto.ListWorkflowProviderDefinitionsRequest{}).(*proto.ListWorkflowProviderDefinitionsRequest)
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationListDefinitions, workflowDims{})
	defer func() { end(err) }()
	ctx, cancel, err := workflowProviderRequestContext(ctx, req)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return r.client.ListDefinitions(ctx, req)
}

func (r *remoteWorkflow) SetDefinitionPaused(ctx context.Context, req *proto.SetWorkflowProviderDefinitionPausedRequest) (definition *proto.WorkflowDefinition, err error) {
	req = cloneWorkflowRequest(req, &proto.SetWorkflowProviderDefinitionPausedRequest{}).(*proto.SetWorkflowProviderDefinitionPausedRequest)
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationSetDefinitionPaused, workflowDims{})
	defer func() { end(err) }()
	ctx, cancel, err := workflowProviderRequestContext(ctx, req)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return r.client.SetDefinitionPaused(ctx, req)
}

func (r *remoteWorkflow) SetActivationPaused(ctx context.Context, req *proto.SetWorkflowProviderActivationPausedRequest) (definition *proto.WorkflowDefinition, err error) {
	req = cloneWorkflowRequest(req, &proto.SetWorkflowProviderActivationPausedRequest{}).(*proto.SetWorkflowProviderActivationPausedRequest)
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationSetActivationPaused, workflowDims{})
	defer func() { end(err) }()
	ctx, cancel, err := workflowProviderRequestContext(ctx, req)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return r.client.SetActivationPaused(ctx, req)
}

func (r *remoteWorkflow) DeleteDefinition(ctx context.Context, req *proto.DeleteWorkflowProviderDefinitionRequest) (err error) {
	req = cloneWorkflowRequest(req, &proto.DeleteWorkflowProviderDefinitionRequest{}).(*proto.DeleteWorkflowProviderDefinitionRequest)
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationDeleteDefinition, workflowDims{})
	defer func() { end(err) }()
	ctx, cancel, err := workflowProviderRequestContext(ctx, req)
	if err != nil {
		return err
	}
	defer cancel()
	_, err = r.client.DeleteDefinition(ctx, req)
	return err
}

func (r *remoteWorkflow) StartRun(ctx context.Context, req *proto.StartWorkflowProviderRunRequest) (run *proto.WorkflowRun, err error) {
	req = cloneWorkflowRequest(req, &proto.StartWorkflowProviderRunRequest{}).(*proto.StartWorkflowProviderRunRequest)
	req.ProviderName = r.name
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationStartRun, workflowDims{triggerKind: observability.WorkflowTriggerKindManual})
	defer func() { end(err) }()
	ctx, cancel, err := workflowProviderRequestContext(ctx, req)
	if err != nil {
		return nil, err
	}
	defer cancel()
	run, err = r.client.StartRun(ctx, req)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.GetIdempotencyKey()) == "" {
		observability.RecordWorkflowRunStarted(ctx, r.workflowMetricDims(observability.WorkflowOperationStartRun, workflowDims{
			triggerKind: observability.WorkflowTriggerKindManual,
			targetKind:  workflowProtoTargetKind(run.GetTarget()),
			runStatus:   workflowRunStatusMetric(run),
		}))
	}
	return run, nil
}

func (r *remoteWorkflow) GetRun(ctx context.Context, req *proto.GetWorkflowProviderRunRequest) (run *proto.WorkflowRun, err error) {
	req = cloneWorkflowRequest(req, &proto.GetWorkflowProviderRunRequest{}).(*proto.GetWorkflowProviderRunRequest)
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationGetRun, workflowDims{})
	defer func() { end(err) }()
	ctx, cancel, err := workflowProviderRequestContext(ctx, req)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return r.client.GetRun(ctx, req)
}

func (r *remoteWorkflow) ListRuns(ctx context.Context, req *proto.ListWorkflowProviderRunsRequest) (out *proto.ListWorkflowProviderRunsResponse, err error) {
	req = cloneWorkflowRequest(req, &proto.ListWorkflowProviderRunsRequest{}).(*proto.ListWorkflowProviderRunsRequest)
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationListRuns, workflowDims{})
	defer func() { end(err) }()
	ctx, cancel, err := workflowProviderRequestContext(ctx, req)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return r.client.ListRuns(ctx, req)
}

func (r *remoteWorkflow) GetRunEvents(ctx context.Context, req *proto.GetWorkflowProviderRunEventsRequest) (out *proto.GetWorkflowProviderRunEventsResponse, err error) {
	req = cloneWorkflowRequest(req, &proto.GetWorkflowProviderRunEventsRequest{}).(*proto.GetWorkflowProviderRunEventsRequest)
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationGetRunEvents, workflowDims{})
	defer func() { end(err) }()
	ctx, cancel, err := workflowProviderRequestContext(ctx, req)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return r.client.GetRunEvents(ctx, req)
}

func (r *remoteWorkflow) GetRunOutput(ctx context.Context, req *proto.GetWorkflowProviderRunOutputRequest) (out *proto.GetWorkflowProviderRunOutputResponse, err error) {
	req = cloneWorkflowRequest(req, &proto.GetWorkflowProviderRunOutputRequest{}).(*proto.GetWorkflowProviderRunOutputRequest)
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationGetRunOutput, workflowDims{})
	defer func() { end(err) }()
	ctx, cancel, err := workflowProviderRequestContext(ctx, req)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return r.client.GetRunOutput(ctx, req)
}

func (r *remoteWorkflow) CancelRun(ctx context.Context, req *proto.CancelWorkflowProviderRunRequest) (run *proto.WorkflowRun, err error) {
	req = cloneWorkflowRequest(req, &proto.CancelWorkflowProviderRunRequest{}).(*proto.CancelWorkflowProviderRunRequest)
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationCancelRun, workflowDims{})
	defer func() { end(err) }()
	ctx, cancel, err := workflowProviderRequestContext(ctx, req)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return r.client.CancelRun(ctx, req)
}

func (r *remoteWorkflow) SignalRun(ctx context.Context, req *proto.SignalWorkflowProviderRunRequest) (out *proto.SignalWorkflowRunResponse, err error) {
	req = cloneWorkflowRequest(req, &proto.SignalWorkflowProviderRunRequest{}).(*proto.SignalWorkflowProviderRunRequest)
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationSignalRun, workflowDims{triggerKind: observability.WorkflowTriggerKindSignal})
	defer func() { end(err) }()
	ctx, cancel, err := workflowProviderRequestContext(ctx, req)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return r.client.SignalRun(ctx, req)
}

func (r *remoteWorkflow) SignalOrStartRun(ctx context.Context, req *proto.SignalOrStartWorkflowProviderRunRequest) (out *proto.SignalWorkflowRunResponse, err error) {
	req = cloneWorkflowRequest(req, &proto.SignalOrStartWorkflowProviderRunRequest{}).(*proto.SignalOrStartWorkflowProviderRunRequest)
	req.ProviderName = r.name
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationSignalOrStartRun, workflowDims{triggerKind: observability.WorkflowTriggerKindSignal})
	defer func() { end(err) }()
	ctx, cancel, err := workflowProviderRequestContext(ctx, req)
	if err != nil {
		return nil, err
	}
	defer cancel()
	out, err = r.client.SignalOrStartRun(ctx, req)
	if err != nil {
		return nil, err
	}
	if out != nil && out.GetStartedRun() {
		dims := r.workflowMetricDims(observability.WorkflowOperationSignalOrStartRun, workflowDims{
			triggerKind: observability.WorkflowTriggerKindSignal,
			targetKind:  workflowProtoTargetKind(out.GetRun().GetTarget()),
			runStatus:   workflowRunStatusMetric(out.GetRun()),
		})
		observability.RecordWorkflowRunStarted(ctx, dims)
	}
	return out, nil
}

func (r *remoteWorkflow) DeliverEvent(ctx context.Context, req *proto.DeliverWorkflowProviderEventRequest) (out *proto.WorkflowEvent, err error) {
	req = cloneWorkflowRequest(req, &proto.DeliverWorkflowProviderEventRequest{}).(*proto.DeliverWorkflowProviderEventRequest)
	req.ProviderName = r.name
	ctx, end := r.startProviderOperation(ctx, observability.WorkflowOperationDeliverEvent, workflowDims{triggerKind: observability.WorkflowTriggerKindEvent})
	metricCtx := ctx
	defer func() {
		end(err)
		observability.RecordWorkflowEventDelivered(metricCtx, err, r.workflowMetricDims(observability.WorkflowOperationDeliverEvent, workflowDims{triggerKind: observability.WorkflowTriggerKindEvent}))
	}()
	ctx, cancel, err := workflowProviderRequestContext(ctx, req)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return r.client.DeliverEvent(ctx, req)
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

func workflowProtoTargetKind(target *proto.BoundWorkflowTarget) string {
	if target != nil && len(target.GetSteps()) > 0 {
		return observability.WorkflowTargetKindSteps
	}
	return observability.WorkflowTargetKindUnknown
}

func workflowProviderRequestContext(ctx context.Context, req gproto.Message) (context.Context, context.CancelFunc, error) {
	if err := attachWorkflowProviderRequestContext(ctx, req); err != nil {
		cancelCtx, cancel := context.WithCancel(ctx)
		return cancelCtx, cancel, err
	}
	callCtx, cancel := runtimehost.ProviderCallContext(ctx)
	return callCtx, cancel, nil
}

func cloneWorkflowRequest(req gproto.Message, empty gproto.Message) gproto.Message {
	if req == nil {
		return empty
	}
	return gproto.Clone(req)
}

func attachWorkflowProviderRequestContext(ctx context.Context, req gproto.Message) error {
	if req == nil {
		return nil
	}
	attachWorkflowProviderInvocationToken(ctx, req)
	reqCtx, err := appaccessservice.RequestContextProto(ctx, "", invocation.CallerProvider{})
	if err != nil {
		return err
	}
	if reqCtx == nil {
		return nil
	}
	msg := req.ProtoReflect()
	field := msg.Descriptor().Fields().ByName(protoreflect.Name("context"))
	if field == nil || field.Kind() != protoreflect.MessageKind {
		return nil
	}
	if msg.Has(field) {
		existing, ok := msg.Get(field).Message().Interface().(*proto.RequestContext)
		if !ok {
			return nil
		}
		merged := appaccessservice.MergeRequestContext(existing, reqCtx)
		msg.Set(field, protoreflect.ValueOfMessage(merged.ProtoReflect()))
		return nil
	}
	msg.Set(field, protoreflect.ValueOfMessage(reqCtx.ProtoReflect()))
	return nil
}

func attachWorkflowProviderInvocationToken(ctx context.Context, req gproto.Message) {
	token := strings.TrimSpace(appaccessservice.InvocationTokenFromContext(ctx))
	if token == "" || req == nil {
		return
	}
	msg := req.ProtoReflect()
	field := msg.Descriptor().Fields().ByName(protoreflect.Name("invocation_token"))
	if field == nil || field.Kind() != protoreflect.StringKind || msg.Get(field).String() != "" {
		return
	}
	msg.Set(field, protoreflect.ValueOfString(token))
}

func workflowRunStatusMetric(run *proto.WorkflowRun) string {
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
