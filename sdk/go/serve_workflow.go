package gestalt

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/sdk/go/client"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
)

// ServeWorkflowProvider starts a gRPC server for a [WorkflowProvider].
func ServeWorkflowProvider(ctx context.Context, provider WorkflowProvider) error {
	return serveProvider(withProviderCloser(ctx, provider), func(srv *grpc.Server) {
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindWorkflow, provider))
		proto.RegisterWorkflowServer(srv, client.NewWorkflowProviderServer(workflowHandler{provider: provider}))
	})
}

// workflowHandler bridges the ergonomic [WorkflowProvider] facade onto the
// generated transport handler; wire conversion lives in the generated adapter.
// providerRPCError preserves root sentinel-error mapping.
type workflowHandler struct {
	client.UnimplementedWorkflowProvider
	provider WorkflowProvider
}

func (h workflowHandler) ApplyDefinition(ctx context.Context, req *client.ApplyWorkflowProviderDefinitionRequest) (*client.WorkflowDefinition, error) {
	var spec *WorkflowDefinitionSpec
	if req.Spec != nil {
		input, err := clientWorkflowDefinitionSpecToRoot(req.Spec)
		if err != nil {
			return nil, providerRPCError("workflow apply definition", err)
		}
		spec = &input
	}
	rootReq := &ApplyWorkflowProviderDefinitionRequest{
		Spec:                 spec,
		IdempotencyKey:       req.IdempotencyKey,
		RequestedBySubjectID: req.RequestedBySubjectID,
	}
	definition, err := h.provider.ApplyDefinition(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("workflow apply definition", err)
	}
	out, err := rootWorkflowDefinitionToClient(definition)
	if err != nil {
		return nil, providerRPCError("workflow apply definition", err)
	}
	return out, nil
}

func (h workflowHandler) GetDefinition(ctx context.Context, req *client.GetWorkflowProviderDefinitionRequest) (*client.WorkflowDefinition, error) {
	definition, err := h.provider.GetDefinition(ctx, &GetWorkflowProviderDefinitionRequest{DefinitionID: req.DefinitionID})
	if err != nil {
		return nil, providerRPCError("workflow get definition", err)
	}
	out, err := rootWorkflowDefinitionToClient(definition)
	if err != nil {
		return nil, providerRPCError("workflow get definition", err)
	}
	return out, nil
}

func (h workflowHandler) ListDefinitions(ctx context.Context, req *client.ListWorkflowProviderDefinitionsRequest) (*client.ListWorkflowProviderDefinitionsResponse, error) {
	_ = req
	resp, err := h.provider.ListDefinitions(ctx, &ListWorkflowProviderDefinitionsRequest{})
	if err != nil {
		return nil, providerRPCError("workflow list definitions", err)
	}
	definitions, err := rootWorkflowDefinitionsToClient(resp.GetDefinitions())
	if err != nil {
		return nil, providerRPCError("workflow list definitions", err)
	}
	return &client.ListWorkflowProviderDefinitionsResponse{Definitions: definitions}, nil
}

func (h workflowHandler) SetDefinitionPaused(ctx context.Context, req *client.SetWorkflowProviderDefinitionPausedRequest) (*client.WorkflowDefinition, error) {
	definition, err := h.provider.SetDefinitionPaused(ctx, &SetWorkflowProviderDefinitionPausedRequest{
		DefinitionID:         req.DefinitionID,
		Paused:               req.Paused,
		RequestedBySubjectID: req.RequestedBySubjectID,
	})
	if err != nil {
		return nil, providerRPCError("workflow set definition paused", err)
	}
	out, err := rootWorkflowDefinitionToClient(definition)
	if err != nil {
		return nil, providerRPCError("workflow set definition paused", err)
	}
	return out, nil
}

func (h workflowHandler) SetActivationPaused(ctx context.Context, req *client.SetWorkflowProviderActivationPausedRequest) (*client.WorkflowDefinition, error) {
	definition, err := h.provider.SetActivationPaused(ctx, &SetWorkflowProviderActivationPausedRequest{
		DefinitionID:         req.DefinitionID,
		ActivationID:         req.ActivationID,
		Paused:               req.Paused,
		RequestedBySubjectID: req.RequestedBySubjectID,
	})
	if err != nil {
		return nil, providerRPCError("workflow set activation paused", err)
	}
	out, err := rootWorkflowDefinitionToClient(definition)
	if err != nil {
		return nil, providerRPCError("workflow set activation paused", err)
	}
	return out, nil
}

func (h workflowHandler) DeleteDefinition(ctx context.Context, req *client.DeleteWorkflowProviderDefinitionRequest) error {
	return providerRPCError("workflow delete definition", h.provider.DeleteDefinition(ctx, &DeleteWorkflowProviderDefinitionRequest{DefinitionID: req.DefinitionID}))
}

func (h workflowHandler) StartRun(ctx context.Context, req *client.StartWorkflowProviderRunRequest) (*client.WorkflowRun, error) {
	rootReq := &StartWorkflowProviderRunRequest{
		DefinitionID:                 req.DefinitionID,
		ExpectedDefinitionGeneration: req.ExpectedDefinitionGeneration,
		Input:                        req.Input,
		IdempotencyKey:               req.IdempotencyKey,
		CreatedBySubjectID:           req.CreatedBySubjectID,
		RunAs:                        clientSubjectContextToRootSubject(req.RunAs),
		WorkflowKey:                  req.WorkflowKey,
	}
	run, err := h.provider.StartRun(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("workflow start run", err)
	}
	out, err := rootWorkflowRunToClient(run)
	if err != nil {
		return nil, providerRPCError("workflow start run", err)
	}
	return out, nil
}

func (h workflowHandler) GetRun(ctx context.Context, req *client.GetWorkflowProviderRunRequest) (*client.WorkflowRun, error) {
	run, err := h.provider.GetRun(ctx, &GetWorkflowProviderRunRequest{RunID: req.RunID})
	if err != nil {
		return nil, providerRPCError("workflow get run", err)
	}
	out, err := rootWorkflowRunToClient(run)
	if err != nil {
		return nil, providerRPCError("workflow get run", err)
	}
	return out, nil
}

func (h workflowHandler) ListRuns(ctx context.Context, req *client.ListWorkflowProviderRunsRequest) (*client.ListWorkflowProviderRunsResponse, error) {
	resp, err := h.provider.ListRuns(ctx, &ListWorkflowProviderRunsRequest{
		PageSize:  int(req.PageSize),
		PageToken: req.PageToken,
		Status:    WorkflowRunStatus(req.Status),
		TargetApp: req.TargetApp,
	})
	if err != nil {
		return nil, providerRPCError("workflow list runs", err)
	}
	runs, err := rootWorkflowRunsToClient(resp.GetRuns())
	if err != nil {
		return nil, providerRPCError("workflow list runs", err)
	}
	return &client.ListWorkflowProviderRunsResponse{
		Runs:          runs,
		NextPageToken: resp.GetNextPageToken(),
	}, nil
}

func (h workflowHandler) GetRunEvents(ctx context.Context, req *client.GetWorkflowProviderRunEventsRequest) (*client.GetWorkflowProviderRunEventsResponse, error) {
	resp, err := h.provider.GetRunEvents(ctx, &GetWorkflowProviderRunEventsRequest{RunID: req.RunID})
	if err != nil {
		return nil, providerRPCError("workflow get run events", err)
	}
	events, err := rootWorkflowRunEventsToClient(resp.GetEvents())
	if err != nil {
		return nil, providerRPCError("workflow get run events", err)
	}
	return &client.GetWorkflowProviderRunEventsResponse{Events: events}, nil
}

func (h workflowHandler) GetRunOutput(ctx context.Context, req *client.GetWorkflowProviderRunOutputRequest) (*client.GetWorkflowProviderRunOutputResponse, error) {
	resp, err := h.provider.GetRunOutput(ctx, &GetWorkflowProviderRunOutputRequest{RunID: req.RunID})
	if err != nil {
		return nil, providerRPCError("workflow get run output", err)
	}
	var outputValue any
	if resp != nil {
		outputValue = resp.Output
	}
	// Validate via valueFromAny — errors on unsupported Go types, same as old adapter.
	if _, err := valueFromAny(outputValue); err != nil {
		return nil, providerRPCError("workflow get run output", fmt.Errorf("output: %w", err))
	}
	return &client.GetWorkflowProviderRunOutputResponse{Output: outputValue}, nil
}

func (h workflowHandler) CancelRun(ctx context.Context, req *client.CancelWorkflowProviderRunRequest) (*client.WorkflowRun, error) {
	run, err := h.provider.CancelRun(ctx, &CancelWorkflowProviderRunRequest{RunID: req.RunID, Reason: req.Reason})
	if err != nil {
		return nil, providerRPCError("workflow cancel run", err)
	}
	out, err := rootWorkflowRunToClient(run)
	if err != nil {
		return nil, providerRPCError("workflow cancel run", err)
	}
	return out, nil
}

func (h workflowHandler) SignalRun(ctx context.Context, req *client.SignalWorkflowProviderRunRequest) (*client.SignalWorkflowRunResponse, error) {
	rootReq := &SignalWorkflowProviderRunRequest{
		RunID:  req.RunID,
		Signal: clientWorkflowSignalToRoot(req.Signal),
	}
	resp, err := h.provider.SignalRun(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("workflow signal run", err)
	}
	out, err := rootSignalWorkflowRunResponseToClient(resp)
	if err != nil {
		return nil, providerRPCError("workflow signal run", err)
	}
	return out, nil
}

func (h workflowHandler) SignalOrStartRun(ctx context.Context, req *client.SignalOrStartWorkflowProviderRunRequest) (*client.SignalWorkflowRunResponse, error) {
	rootReq := &SignalOrStartWorkflowProviderRunRequest{
		WorkflowKey:                  req.WorkflowKey,
		DefinitionID:                 req.DefinitionID,
		ExpectedDefinitionGeneration: req.ExpectedDefinitionGeneration,
		Input:                        req.Input,
		IdempotencyKey:               req.IdempotencyKey,
		CreatedBySubjectID:           req.CreatedBySubjectID,
		RunAs:                        clientSubjectContextToRootSubject(req.RunAs),
		Signal:                       clientWorkflowSignalToRoot(req.Signal),
	}
	resp, err := h.provider.SignalOrStartRun(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("workflow signal or start run", err)
	}
	out, err := rootSignalWorkflowRunResponseToClient(resp)
	if err != nil {
		return nil, providerRPCError("workflow signal or start run", err)
	}
	return out, nil
}

func (h workflowHandler) DeliverEvent(ctx context.Context, req *client.DeliverWorkflowProviderEventRequest) (*client.WorkflowEvent, error) {
	rootReq := &DeliverWorkflowProviderEventRequest{
		AppName:              req.AppName,
		Event:                clientWorkflowEventToRoot(req.Event),
		DeliveredBySubjectID: req.DeliveredBySubjectID,
	}
	eventResult, err := h.provider.DeliverEvent(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("workflow deliver event", err)
	}
	// Preserve echo fallback: nil result → echo request event; nil request event → empty event.
	if eventResult == nil {
		eventResult = rootReq.Event
	}
	if eventResult == nil {
		eventResult = &WorkflowEvent{}
	}
	out, err := rootWorkflowEventToClient(eventResult)
	if err != nil {
		return nil, providerRPCError("workflow deliver event", err)
	}
	return out, nil
}

// --- root → client conversions ---

func rootWorkflowDefinitionToClient(in *WorkflowDefinition) (*client.WorkflowDefinition, error) {
	if in == nil {
		return nil, nil
	}
	activations, err := rootWorkflowActivationsToClient(in.Activations)
	if err != nil {
		return nil, err
	}
	out := &client.WorkflowDefinition{
		ID:                 in.ID,
		Generation:         in.Generation,
		Target:             rootBoundWorkflowTargetToClient(in.Target),
		Activations:        activations,
		Paused:             in.Paused,
		CreatedBySubjectID: in.CreatedBySubjectID,
		ProviderName:       in.ProviderName,
		RunAs:              rootSubjectToClientSubjectContext(in.RunAs),
	}
	if !in.CreatedAt.IsZero() {
		t := in.CreatedAt
		out.CreatedAt = &t
	}
	if !in.UpdatedAt.IsZero() {
		t := in.UpdatedAt
		out.UpdatedAt = &t
	}
	return out, nil
}

func rootWorkflowDefinitionsToClient(in []WorkflowDefinition) ([]*client.WorkflowDefinition, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]*client.WorkflowDefinition, 0, len(in))
	for i := range in {
		def, err := rootWorkflowDefinitionToClient(&in[i])
		if err != nil {
			return nil, err
		}
		out = append(out, def)
	}
	return out, nil
}

func rootBoundWorkflowTargetToClient(in *BoundWorkflowTarget) *client.BoundWorkflowTarget {
	if in == nil {
		return nil
	}
	out := &client.BoundWorkflowTarget{}
	for i := range in.Steps {
		out.Steps = append(out.Steps, rootWorkflowStepToClient(&in.Steps[i]))
	}
	return out
}

func rootWorkflowStepToClient(in *WorkflowStep) *client.WorkflowStep {
	if in == nil {
		return nil
	}
	out := &client.WorkflowStep{
		ID:             in.ID,
		TimeoutSeconds: in.TimeoutSeconds,
		When:           rootWorkflowStepWhenToClient(in.When),
	}
	if in.Metadata != nil {
		if m, ok := in.Metadata.(map[string]any); ok {
			out.Metadata = m
		}
	}
	if in.Inputs != nil {
		out.Inputs = make(map[string]*client.WorkflowValue, len(in.Inputs))
		for k, v := range in.Inputs {
			cv := rootWorkflowValueToClient(v)
			out.Inputs[k] = cv
		}
	}
	switch {
	case in.App != nil:
		out.Action = &client.WorkflowStepActionApp{Value: rootWorkflowStepAppCallToClient(in.App)}
	case in.Agent != nil:
		out.Action = &client.WorkflowStepActionAgent{Value: rootWorkflowStepAgentTurnToClient(in.Agent)}
	}
	return out
}

func rootWorkflowStepWhenToClient(in *WorkflowStepWhen) *client.WorkflowStepWhen {
	if in == nil {
		return nil
	}
	return &client.WorkflowStepWhen{
		Value:  rootWorkflowValueToClient(in.Value),
		Equals: in.Equals,
	}
}

func rootWorkflowStepAppCallToClient(in *WorkflowStepAppCall) *client.WorkflowStepAppCall {
	if in == nil {
		return nil
	}
	return &client.WorkflowStepAppCall{
		Name:           in.Name,
		Operation:      in.Operation,
		Input:          rootWorkflowValueToClient(in.Input),
		Connection:     in.Connection,
		Instance:       in.Instance,
		CredentialMode: in.CredentialMode,
	}
}

func rootWorkflowStepAgentTurnToClient(in *WorkflowStepAgentTurn) *client.WorkflowStepAgentTurn {
	if in == nil {
		return nil
	}
	out := &client.WorkflowStepAgentTurn{
		Provider:   in.Provider,
		Model:      in.Model,
		SessionKey: in.SessionKey,
		Prompt:     rootWorkflowTextToClient(in.Prompt),
	}
	if m, ok := in.ModelOptions.(map[string]any); ok {
		out.ModelOptions = m
	}
	for i := range in.Tools {
		ref := rootAgentToolRefToClientRef(&in.Tools[i])
		out.Tools = append(out.Tools, ref)
	}
	for i := range in.Messages {
		out.Messages = append(out.Messages, rootWorkflowAgentMessageToClient(&in.Messages[i]))
	}
	if in.Output != nil {
		out.Output = rootAgentOutputToClient(in.Output)
	}
	return out
}

func rootWorkflowTextToClient(in WorkflowText) *client.WorkflowText {
	if in.Template == "" {
		return nil
	}
	return &client.WorkflowText{Template: in.Template}
}

func rootWorkflowAgentMessageToClient(in *WorkflowAgentMessage) *client.WorkflowAgentMessage {
	if in == nil {
		return nil
	}
	out := &client.WorkflowAgentMessage{
		Role: in.Role,
		Text: rootWorkflowTextToClient(in.Text),
	}
	if m, ok := in.Metadata.(map[string]any); ok {
		out.Metadata = m
	}
	return out
}

func rootAgentToolRefToClientRef(in *AgentToolRef) *client.AgentToolRef {
	if in == nil {
		return nil
	}
	return &client.AgentToolRef{
		App:            in.App,
		Operation:      in.Operation,
		Connection:     in.Connection,
		Instance:       in.Instance,
		Title:          in.Title,
		Description:    in.Description,
		CredentialMode: in.CredentialMode,
		System:         in.System,
		RunAs:          rootSubjectToClientSubjectContext(in.RunAs),
	}
}

func rootAgentOutputToClient(in *AgentOutput) *client.AgentOutput {
	if in == nil {
		return nil
	}
	switch {
	case in.Text != nil:
		return &client.AgentOutput{Kind: &client.AgentOutputKindText{Value: &client.AgentTextOutput{}}}
	case in.Structured != nil:
		return &client.AgentOutput{Kind: &client.AgentOutputKindStructured{Value: &client.AgentStructuredOutput{Schema: in.Structured.Schema}}}
	default:
		return nil
	}
}

func rootWorkflowValueToClient(in WorkflowValue) *client.WorkflowValue {
	switch {
	case in.LiteralSet:
		return &client.WorkflowValue{Kind: &client.WorkflowValueKindLiteral{Value: in.Literal}}
	case in.Object != nil:
		obj := &client.WorkflowObject{Fields: make(map[string]*client.WorkflowValue, len(in.Object))}
		for k, v := range in.Object {
			cv := rootWorkflowValueToClient(v)
			obj.Fields[k] = cv
		}
		return &client.WorkflowValue{Kind: &client.WorkflowValueKindObject{Value: obj}}
	case in.Array != nil:
		arr := &client.WorkflowArray{}
		for i := range in.Array {
			cv := rootWorkflowValueToClient(in.Array[i])
			arr.Values = append(arr.Values, cv)
		}
		return &client.WorkflowValue{Kind: &client.WorkflowValueKindArray{Value: arr}}
	case in.Template != nil:
		return &client.WorkflowValue{Kind: &client.WorkflowValueKindTemplate{Value: rootWorkflowTextToClient(*in.Template)}}
	case in.Input != "":
		return &client.WorkflowValue{Kind: &client.WorkflowValueKindInput{Value: &client.WorkflowPathSource{Path: in.Input}}}
	case in.Signal != "":
		return &client.WorkflowValue{Kind: &client.WorkflowValueKindSignal{Value: &client.WorkflowPathSource{Path: in.Signal}}}
	case in.StepOutput != nil:
		return &client.WorkflowValue{Kind: &client.WorkflowValueKindStepOutput{Value: &client.WorkflowStepOutputSource{
			StepID: in.StepOutput.StepID,
			Path:   in.StepOutput.Path,
		}}}
	case in.StepInput != nil:
		return &client.WorkflowValue{Kind: &client.WorkflowValueKindStepInput{Value: &client.WorkflowStepInputSource{
			StepID: in.StepInput.StepID,
			Path:   in.StepInput.Path,
		}}}
	default:
		return nil
	}
}

func rootWorkflowActivationsToClient(in []WorkflowActivation) ([]*client.WorkflowActivation, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]*client.WorkflowActivation, 0, len(in))
	for i := range in {
		act, err := rootWorkflowActivationToClient(&in[i])
		if err != nil {
			return nil, err
		}
		out = append(out, act)
	}
	return out, nil
}

func rootWorkflowActivationToClient(in *WorkflowActivation) (*client.WorkflowActivation, error) {
	if in == nil {
		return nil, nil
	}
	out := &client.WorkflowActivation{
		ID:     in.ID,
		Paused: in.Paused,
		Input:  rootWorkflowValueToClient(in.Input),
	}
	switch {
	case in.Schedule != nil && in.Event != nil:
		return nil, fmt.Errorf("activation must set exactly one of schedule or event")
	case in.Schedule != nil:
		out.Trigger = &client.WorkflowActivationTriggerSchedule{Value: &client.WorkflowScheduleActivation{
			Cron:     in.Schedule.Cron,
			Timezone: in.Schedule.Timezone,
		}}
	case in.Event != nil:
		var match *client.WorkflowEventMatch
		if in.Event.Match != nil {
			match = &client.WorkflowEventMatch{
				Type:    in.Event.Match.Type,
				Source:  in.Event.Match.Source,
				Subject: in.Event.Match.Subject,
			}
		}
		out.Trigger = &client.WorkflowActivationTriggerEvent{Value: &client.WorkflowEventActivation{Match: match}}
	}
	return out, nil
}

func rootWorkflowRunToClient(in *WorkflowRun) (*client.WorkflowRun, error) {
	if in == nil {
		return nil, nil
	}
	// Validate output via valueFromAny (same validator as old adapter's workflowRunToProto).
	if _, err := valueFromAny(in.Output); err != nil {
		return nil, fmt.Errorf("output: %w", err)
	}
	steps, err := rootWorkflowStepExecutionsToClient(in.Steps)
	if err != nil {
		return nil, err
	}
	out := &client.WorkflowRun{
		ID:                   in.ID,
		Status:               client.WorkflowRunStatus(in.Status),
		Target:               rootBoundWorkflowTargetToClient(in.Target),
		Trigger:              rootWorkflowRunTriggerToClient(in.Trigger),
		StatusMessage:        in.StatusMessage,
		Output:               in.Output,
		CreatedBySubjectID:   in.CreatedBySubjectID,
		RunAs:                rootSubjectToClientSubjectContext(in.RunAs),
		WorkflowKey:          in.WorkflowKey,
		ProviderName:         in.ProviderName,
		DefinitionID:         in.DefinitionID,
		Input:                in.Input,
		DefinitionGeneration: in.DefinitionGeneration,
		CurrentStepID:        in.CurrentStepID,
		Steps:                steps,
	}
	if !in.CreatedAt.IsZero() {
		t := in.CreatedAt
		out.CreatedAt = &t
	}
	if in.StartedAt != nil {
		out.StartedAt = in.StartedAt
	}
	if in.CompletedAt != nil {
		out.CompletedAt = in.CompletedAt
	}
	return out, nil
}

func rootWorkflowRunsToClient(in []WorkflowRun) ([]*client.WorkflowRun, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]*client.WorkflowRun, 0, len(in))
	for i := range in {
		r, err := rootWorkflowRunToClient(&in[i])
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func rootWorkflowRunTriggerToClient(in *WorkflowRunTrigger) *client.WorkflowRunTrigger {
	if in == nil {
		return nil
	}
	out := &client.WorkflowRunTrigger{}
	switch {
	case in.Manual:
		out.Kind = &client.WorkflowRunTriggerKindManual{Value: &client.WorkflowManualTrigger{}}
	case in.Schedule != nil:
		out.Kind = &client.WorkflowRunTriggerKindSchedule{Value: &client.WorkflowScheduleTrigger{
			ActivationID: in.Schedule.ActivationID,
			ScheduledFor: in.Schedule.ScheduledFor,
		}}
	case in.Event != nil:
		inv := &client.WorkflowEventTriggerInvocation{ActivationID: in.Event.ActivationID}
		if in.Event.Event != nil {
			inv.Event = rootWorkflowEventToClientDirect(in.Event.Event)
		}
		out.Kind = &client.WorkflowRunTriggerKindEvent{Value: inv}
	}
	return out
}

func rootWorkflowStepExecutionsToClient(in []WorkflowStepExecution) ([]*client.WorkflowStepExecution, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]*client.WorkflowStepExecution, 0, len(in))
	for i := range in {
		se, err := rootWorkflowStepExecutionToClient(&in[i])
		if err != nil {
			return nil, err
		}
		out = append(out, se)
	}
	return out, nil
}

func rootWorkflowStepExecutionToClient(in *WorkflowStepExecution) (*client.WorkflowStepExecution, error) {
	if in == nil {
		return nil, nil
	}
	if _, err := valueFromAny(in.Input); err != nil {
		return nil, fmt.Errorf("input: %w", err)
	}
	if _, err := valueFromAny(in.Output); err != nil {
		return nil, fmt.Errorf("output: %w", err)
	}
	attempts, err := rootWorkflowStepAttemptsToClient(in.Attempts)
	if err != nil {
		return nil, err
	}
	out := &client.WorkflowStepExecution{
		StepID:        in.StepID,
		Status:        client.WorkflowStepStatus(in.Status),
		Attempts:      attempts,
		Input:         in.Input,
		Output:        in.Output,
		StatusMessage: in.StatusMessage,
		SkipReason:    in.SkipReason,
		StartedAt:     in.StartedAt,
		CompletedAt:   in.CompletedAt,
	}
	return out, nil
}

func rootWorkflowStepAttemptsToClient(in []WorkflowStepAttempt) ([]*client.WorkflowStepAttempt, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]*client.WorkflowStepAttempt, 0, len(in))
	for i := range in {
		a, err := rootWorkflowStepAttemptToClient(&in[i])
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

func rootWorkflowStepAttemptToClient(in *WorkflowStepAttempt) (*client.WorkflowStepAttempt, error) {
	if in == nil {
		return nil, nil
	}
	if _, err := valueFromAny(in.Input); err != nil {
		return nil, fmt.Errorf("input: %w", err)
	}
	if _, err := valueFromAny(in.Output); err != nil {
		return nil, fmt.Errorf("output: %w", err)
	}
	return &client.WorkflowStepAttempt{
		ID:             in.ID,
		Status:         client.WorkflowStepStatus(in.Status),
		IdempotencyKey: in.IdempotencyKey,
		Input:          in.Input,
		Output:         in.Output,
		StatusMessage:  in.StatusMessage,
		StartedAt:      in.StartedAt,
		CompletedAt:    in.CompletedAt,
	}, nil
}

func rootWorkflowRunEventsToClient(in []WorkflowRunEvent) ([]*client.WorkflowRunEvent, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]*client.WorkflowRunEvent, 0, len(in))
	for i := range in {
		e, err := rootWorkflowRunEventToClient(&in[i])
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func rootWorkflowRunEventToClient(in *WorkflowRunEvent) (*client.WorkflowRunEvent, error) {
	if in == nil {
		return nil, nil
	}
	// structFromAny validates the Data field (same as workflowRunEventToProto).
	if _, err := structFromAny(in.Data); err != nil {
		return nil, err
	}
	data, _ := in.Data.(map[string]any)
	out := &client.WorkflowRunEvent{
		ID:     in.ID,
		RunID:  in.RunID,
		StepID: in.StepID,
		Type:   in.Type,
		Data:   data,
	}
	if !in.CreatedAt.IsZero() {
		t := in.CreatedAt
		out.CreatedAt = &t
	}
	return out, nil
}

func rootWorkflowEventToClient(in *WorkflowEvent) (*client.WorkflowEvent, error) {
	if in == nil {
		return nil, nil
	}
	// Validate via structFromAny (same as workflowEventToProto).
	if _, err := structFromAny(in.Data); err != nil {
		return nil, err
	}
	return rootWorkflowEventToClientDirect(in), nil
}

func rootWorkflowEventToClientDirect(in *WorkflowEvent) *client.WorkflowEvent {
	if in == nil {
		return nil
	}
	data, _ := in.Data.(map[string]any)
	out := &client.WorkflowEvent{
		ID:              in.ID,
		Source:          in.Source,
		SpecVersion:     in.SpecVersion,
		Type:            in.Type,
		Subject:         in.Subject,
		Datacontenttype: in.DataContentType,
		Data:            data,
	}
	if !in.Time.IsZero() {
		t := in.Time
		out.Time = &t
	}
	if len(in.Extensions) > 0 {
		out.Extensions = in.Extensions
	}
	return out
}

func rootSignalWorkflowRunResponseToClient(in *SignalWorkflowRunResponse) (*client.SignalWorkflowRunResponse, error) {
	if in == nil {
		return nil, nil
	}
	run, err := rootWorkflowRunToClient(in.Run)
	if err != nil {
		return nil, err
	}
	signal, err := rootWorkflowSignalToClient(in.Signal)
	if err != nil {
		return nil, err
	}
	return &client.SignalWorkflowRunResponse{
		Run:         run,
		Signal:      signal,
		StartedRun:  in.StartedRun,
		WorkflowKey: in.WorkflowKey,
	}, nil
}

func rootWorkflowSignalToClient(in *WorkflowSignal) (*client.WorkflowSignal, error) {
	if in == nil {
		return nil, nil
	}
	// Validate payload/metadata (same as workflowSignalToProto).
	if _, err := structFromAny(in.Payload); err != nil {
		return nil, err
	}
	if _, err := structFromAny(in.Metadata); err != nil {
		return nil, err
	}
	payload, _ := in.Payload.(map[string]any)
	metadata, _ := in.Metadata.(map[string]any)
	out := &client.WorkflowSignal{
		ID:                 in.ID,
		Name:               in.Name,
		Payload:            payload,
		Metadata:           metadata,
		CreatedBySubjectID: in.CreatedBySubjectID,
		IdempotencyKey:     in.IdempotencyKey,
		Sequence:           in.Sequence,
	}
	if !in.CreatedAt.IsZero() {
		t := in.CreatedAt
		out.CreatedAt = &t
	}
	return out, nil
}

// --- client → root conversions ---

func clientWorkflowDefinitionSpecToRoot(in *client.WorkflowDefinitionSpec) (WorkflowDefinitionSpec, error) {
	if in == nil {
		return WorkflowDefinitionSpec{}, nil
	}
	activations, err := clientWorkflowActivationsToRoot(in.Activations)
	if err != nil {
		return WorkflowDefinitionSpec{}, err
	}
	return WorkflowDefinitionSpec{
		ID:          in.ID,
		Target:      clientBoundWorkflowTargetToRoot(in.Target),
		Activations: activations,
		Paused:      in.Paused,
		RunAs:       clientSubjectContextToRootSubject(in.RunAs),
	}, nil
}

func clientBoundWorkflowTargetToRoot(in *client.BoundWorkflowTarget) *BoundWorkflowTarget {
	if in == nil {
		return nil
	}
	out := &BoundWorkflowTarget{}
	for _, s := range in.Steps {
		out.Steps = append(out.Steps, clientWorkflowStepToRoot(s))
	}
	return out
}

func clientWorkflowStepToRoot(in *client.WorkflowStep) WorkflowStep {
	if in == nil {
		return WorkflowStep{}
	}
	out := WorkflowStep{
		ID:             in.ID,
		TimeoutSeconds: in.TimeoutSeconds,
		When:           clientWorkflowStepWhenToRoot(in.When),
		Metadata:       in.Metadata,
	}
	if in.Inputs != nil {
		out.Inputs = make(map[string]WorkflowValue, len(in.Inputs))
		for k, v := range in.Inputs {
			out.Inputs[k] = clientWorkflowValueToRoot(v)
		}
	}
	switch a := in.Action.(type) {
	case *client.WorkflowStepActionApp:
		out.App = clientWorkflowStepAppCallToRoot(a.Value)
	case *client.WorkflowStepActionAgent:
		out.Agent = clientWorkflowStepAgentTurnToRoot(a.Value)
	}
	return out
}

func clientWorkflowStepWhenToRoot(in *client.WorkflowStepWhen) *WorkflowStepWhen {
	if in == nil {
		return nil
	}
	return &WorkflowStepWhen{
		Value:  clientWorkflowValueToRoot(in.Value),
		Equals: in.Equals,
	}
}

func clientWorkflowStepAppCallToRoot(in *client.WorkflowStepAppCall) *WorkflowStepAppCall {
	if in == nil {
		return nil
	}
	return &WorkflowStepAppCall{
		Name:           in.Name,
		Operation:      in.Operation,
		Input:          clientWorkflowValueToRoot(in.Input),
		Connection:     in.Connection,
		Instance:       in.Instance,
		CredentialMode: in.CredentialMode,
	}
}

func clientWorkflowStepAgentTurnToRoot(in *client.WorkflowStepAgentTurn) *WorkflowStepAgentTurn {
	if in == nil {
		return nil
	}
	out := &WorkflowStepAgentTurn{
		Provider:     in.Provider,
		Model:        in.Model,
		SessionKey:   in.SessionKey,
		ModelOptions: in.ModelOptions,
	}
	if in.Prompt != nil {
		out.Prompt = WorkflowText{Template: in.Prompt.Template}
	}
	for _, t := range in.Tools {
		out.Tools = append(out.Tools, clientAgentToolRefToRoot(t))
	}
	for _, m := range in.Messages {
		out.Messages = append(out.Messages, clientWorkflowAgentMessageToRoot(m))
	}
	if in.Output != nil {
		out.Output = clientAgentOutputToRoot(in.Output)
	}
	return out
}

func clientWorkflowAgentMessageToRoot(in *client.WorkflowAgentMessage) WorkflowAgentMessage {
	if in == nil {
		return WorkflowAgentMessage{}
	}
	out := WorkflowAgentMessage{
		Role:     in.Role,
		Metadata: in.Metadata,
	}
	if in.Text != nil {
		out.Text = WorkflowText{Template: in.Text.Template}
	}
	return out
}

func clientAgentToolRefToRoot(in *client.AgentToolRef) AgentToolRef {
	if in == nil {
		return AgentToolRef{}
	}
	return AgentToolRef{
		App:            in.App,
		Operation:      in.Operation,
		Connection:     in.Connection,
		Instance:       in.Instance,
		Title:          in.Title,
		Description:    in.Description,
		CredentialMode: in.CredentialMode,
		System:         in.System,
		RunAs:          clientSubjectContextToRootSubject(in.RunAs),
	}
}

func clientAgentOutputToRoot(in *client.AgentOutput) *AgentOutput {
	if in == nil {
		return nil
	}
	switch k := in.Kind.(type) {
	case *client.AgentOutputKindText:
		_ = k
		return &AgentOutput{Text: &AgentTextOutput{}}
	case *client.AgentOutputKindStructured:
		var schema map[string]any
		if k.Value != nil {
			schema = k.Value.Schema
		}
		return &AgentOutput{Structured: &AgentStructuredOutput{Schema: schema}}
	default:
		return nil
	}
}

func clientWorkflowValueToRoot(in *client.WorkflowValue) WorkflowValue {
	if in == nil {
		return WorkflowValue{}
	}
	switch k := in.Kind.(type) {
	case *client.WorkflowValueKindLiteral:
		return WorkflowValue{Literal: k.Value, LiteralSet: true}
	case *client.WorkflowValueKindObject:
		if k.Value == nil {
			return WorkflowValue{Object: map[string]WorkflowValue{}}
		}
		obj := make(map[string]WorkflowValue, len(k.Value.Fields))
		for key, v := range k.Value.Fields {
			obj[key] = clientWorkflowValueToRoot(v)
		}
		return WorkflowValue{Object: obj}
	case *client.WorkflowValueKindArray:
		if k.Value == nil {
			return WorkflowValue{Array: []WorkflowValue{}}
		}
		arr := make([]WorkflowValue, 0, len(k.Value.Values))
		for _, v := range k.Value.Values {
			arr = append(arr, clientWorkflowValueToRoot(v))
		}
		return WorkflowValue{Array: arr}
	case *client.WorkflowValueKindTemplate:
		if k.Value == nil {
			t := WorkflowText{}
			return WorkflowValue{Template: &t}
		}
		t := WorkflowText{Template: k.Value.Template}
		return WorkflowValue{Template: &t}
	case *client.WorkflowValueKindInput:
		if k.Value == nil {
			return WorkflowValue{Input: ""}
		}
		return WorkflowValue{Input: k.Value.Path}
	case *client.WorkflowValueKindSignal:
		if k.Value == nil {
			return WorkflowValue{Signal: ""}
		}
		return WorkflowValue{Signal: k.Value.Path}
	case *client.WorkflowValueKindStepOutput:
		if k.Value == nil {
			return WorkflowValue{StepOutput: &WorkflowStepOutputSource{}}
		}
		return WorkflowValue{StepOutput: &WorkflowStepOutputSource{StepID: k.Value.StepID, Path: k.Value.Path}}
	case *client.WorkflowValueKindStepInput:
		if k.Value == nil {
			return WorkflowValue{StepInput: &WorkflowStepInputSource{}}
		}
		return WorkflowValue{StepInput: &WorkflowStepInputSource{StepID: k.Value.StepID, Path: k.Value.Path}}
	default:
		return WorkflowValue{}
	}
}

func clientWorkflowActivationsToRoot(in []*client.WorkflowActivation) ([]WorkflowActivation, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]WorkflowActivation, 0, len(in))
	for _, act := range in {
		rootAct, err := clientWorkflowActivationToRoot(act)
		if err != nil {
			return nil, err
		}
		out = append(out, rootAct)
	}
	return out, nil
}

func clientWorkflowActivationToRoot(in *client.WorkflowActivation) (WorkflowActivation, error) {
	if in == nil {
		return WorkflowActivation{}, nil
	}
	out := WorkflowActivation{
		ID:     in.ID,
		Paused: in.Paused,
		Input:  clientWorkflowValueToRoot(in.Input),
	}
	switch t := in.Trigger.(type) {
	case *client.WorkflowActivationTriggerSchedule:
		if t.Value != nil {
			out.Schedule = &WorkflowScheduleActivation{Cron: t.Value.Cron, Timezone: t.Value.Timezone}
		}
	case *client.WorkflowActivationTriggerEvent:
		activation := &WorkflowEventActivation{}
		if t.Value != nil && t.Value.Match != nil {
			activation.Match = &WorkflowEventMatch{
				Type:    t.Value.Match.Type,
				Source:  t.Value.Match.Source,
				Subject: t.Value.Match.Subject,
			}
		}
		out.Event = activation
	}
	return out, nil
}

func clientWorkflowSignalToRoot(in *client.WorkflowSignal) *WorkflowSignal {
	if in == nil {
		return nil
	}
	out := &WorkflowSignal{
		ID:                 in.ID,
		Name:               in.Name,
		Payload:            in.Payload,
		Metadata:           in.Metadata,
		CreatedBySubjectID: in.CreatedBySubjectID,
		IdempotencyKey:     in.IdempotencyKey,
		Sequence:           in.Sequence,
	}
	if in.CreatedAt != nil {
		out.CreatedAt = *in.CreatedAt
	}
	return out
}

func clientWorkflowEventToRoot(in *client.WorkflowEvent) *WorkflowEvent {
	if in == nil {
		return nil
	}
	out := &WorkflowEvent{
		ID:              in.ID,
		Source:          in.Source,
		SpecVersion:     in.SpecVersion,
		Type:            in.Type,
		Subject:         in.Subject,
		DataContentType: in.Datacontenttype,
		Data:            in.Data,
		Extensions:      in.Extensions,
	}
	if in.Time != nil {
		out.Time = *in.Time
	}
	return out
}

// rootSubjectToClientSubjectContext converts root *Subject → *client.SubjectContext.
func rootSubjectToClientSubjectContext(in *Subject) *client.SubjectContext {
	if in == nil {
		return nil
	}
	return &client.SubjectContext{
		ID:                  in.ID,
		CredentialSubjectID: in.CredentialSubjectID,
		Email:               in.Email,
		DisplayName:         in.DisplayName,
		Scopes:              append([]string(nil), in.Scopes...),
	}
}

// clientSubjectContextToRootSubject converts *client.SubjectContext → root *Subject.
func clientSubjectContextToRootSubject(in *client.SubjectContext) *Subject {
	if in == nil {
		return nil
	}
	return &Subject{
		ID:                  in.ID,
		CredentialSubjectID: in.CredentialSubjectID,
		Email:               in.Email,
		DisplayName:         in.DisplayName,
		Scopes:              append([]string(nil), in.Scopes...),
	}
}
