package gestalt

import proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"

type WorkflowApplyDefinition struct {
	ProviderName   string
	Spec           *WorkflowDefinitionSpec
	IdempotencyKey string
}

type WorkflowGetDefinition struct {
	DefinitionID string
}

type WorkflowListDefinitions struct{}

type WorkflowSetDefinitionPaused struct {
	DefinitionID string
	Paused       bool
}

type WorkflowSetActivationPaused struct {
	DefinitionID string
	ActivationID string
	Paused       bool
}

type WorkflowDeleteDefinition struct {
	DefinitionID string
}

type WorkflowStartRun struct {
	ProviderName                 string
	DefinitionID                 string
	ExpectedDefinitionGeneration int64
	Input                        map[string]any
	IdempotencyKey               string
	WorkflowKey                  string
	RunAs                        *Subject
}

type WorkflowSignalRun struct {
	RunID  string
	Signal *WorkflowSignal
}

type WorkflowSignalOrStartRun struct {
	ProviderName                 string
	WorkflowKey                  string
	DefinitionID                 string
	ExpectedDefinitionGeneration int64
	Input                        map[string]any
	IdempotencyKey               string
	Signal                       *WorkflowSignal
	RunAs                        *Subject
}

type WorkflowGetRun struct {
	RunID string
}

type WorkflowGetRunEvents struct {
	RunID string
}

type WorkflowGetRunOutput struct {
	RunID string
}

type WorkflowDeliverEvent struct {
	ProviderName string
	Event        *WorkflowEvent
}

type WorkflowRunSignal struct {
	Run         *WorkflowRun
	Signal      *WorkflowSignal
	StartedRun  bool
	WorkflowKey string
}

func newWorkflowApplyDefinitionRequest(input WorkflowApplyDefinition) (*proto.ApplyWorkflowProviderDefinitionRequest, error) {
	var spec *proto.WorkflowDefinitionSpec
	if input.Spec != nil {
		converted, err := workflowDefinitionSpecToProto(*input.Spec)
		if err != nil {
			return nil, err
		}
		spec = converted
	}
	return &proto.ApplyWorkflowProviderDefinitionRequest{
		ProviderName:   input.ProviderName,
		Spec:           spec,
		IdempotencyKey: input.IdempotencyKey,
	}, nil
}

func newWorkflowGetDefinitionRequest(input WorkflowGetDefinition) *proto.GetWorkflowProviderDefinitionRequest {
	return &proto.GetWorkflowProviderDefinitionRequest{DefinitionId: input.DefinitionID}
}

func newWorkflowListDefinitionsRequest(WorkflowListDefinitions) *proto.ListWorkflowProviderDefinitionsRequest {
	return &proto.ListWorkflowProviderDefinitionsRequest{}
}

func newWorkflowSetDefinitionPausedRequest(input WorkflowSetDefinitionPaused) *proto.SetWorkflowProviderDefinitionPausedRequest {
	return &proto.SetWorkflowProviderDefinitionPausedRequest{DefinitionId: input.DefinitionID, Paused: input.Paused}
}

func newWorkflowSetActivationPausedRequest(input WorkflowSetActivationPaused) *proto.SetWorkflowProviderActivationPausedRequest {
	return &proto.SetWorkflowProviderActivationPausedRequest{DefinitionId: input.DefinitionID, ActivationId: input.ActivationID, Paused: input.Paused}
}

func newWorkflowDeleteDefinitionRequest(input WorkflowDeleteDefinition) *proto.DeleteWorkflowProviderDefinitionRequest {
	return &proto.DeleteWorkflowProviderDefinitionRequest{DefinitionId: input.DefinitionID}
}

func newWorkflowStartRunRequest(input WorkflowStartRun) (*proto.StartWorkflowProviderRunRequest, error) {
	inputStruct, err := structFromAny(input.Input)
	if err != nil {
		return nil, err
	}
	return &proto.StartWorkflowProviderRunRequest{
		ProviderName:                 input.ProviderName,
		DefinitionId:                 input.DefinitionID,
		ExpectedDefinitionGeneration: input.ExpectedDefinitionGeneration,
		Input:                        inputStruct,
		IdempotencyKey:               input.IdempotencyKey,
		WorkflowKey:                  input.WorkflowKey,
		RunAs:                        subjectToProto(input.RunAs),
	}, nil
}

func newWorkflowSignalRunRequest(input WorkflowSignalRun) (*proto.SignalWorkflowProviderRunRequest, error) {
	signal, err := newOptionalWorkflowSignal(input.Signal)
	if err != nil {
		return nil, err
	}
	return &proto.SignalWorkflowProviderRunRequest{
		RunId:  input.RunID,
		Signal: signal,
	}, nil
}

func newWorkflowSignalOrStartRunRequest(input WorkflowSignalOrStartRun) (*proto.SignalOrStartWorkflowProviderRunRequest, error) {
	inputStruct, err := structFromAny(input.Input)
	if err != nil {
		return nil, err
	}
	signal, err := newOptionalWorkflowSignal(input.Signal)
	if err != nil {
		return nil, err
	}
	return &proto.SignalOrStartWorkflowProviderRunRequest{
		ProviderName:                 input.ProviderName,
		WorkflowKey:                  input.WorkflowKey,
		DefinitionId:                 input.DefinitionID,
		ExpectedDefinitionGeneration: input.ExpectedDefinitionGeneration,
		Input:                        inputStruct,
		IdempotencyKey:               input.IdempotencyKey,
		Signal:                       signal,
		RunAs:                        subjectToProto(input.RunAs),
	}, nil
}

func newWorkflowGetRunRequest(input WorkflowGetRun) *proto.GetWorkflowProviderRunRequest {
	return &proto.GetWorkflowProviderRunRequest{RunId: input.RunID}
}

func newWorkflowGetRunEventsRequest(input WorkflowGetRunEvents) *proto.GetWorkflowProviderRunEventsRequest {
	return &proto.GetWorkflowProviderRunEventsRequest{RunId: input.RunID}
}

func newWorkflowGetRunOutputRequest(input WorkflowGetRunOutput) *proto.GetWorkflowProviderRunOutputRequest {
	return &proto.GetWorkflowProviderRunOutputRequest{RunId: input.RunID}
}

func newWorkflowDeliverEventRequest(input WorkflowDeliverEvent) (*proto.DeliverWorkflowProviderEventRequest, error) {
	event, err := newOptionalWorkflowEvent(input.Event)
	if err != nil {
		return nil, err
	}
	return &proto.DeliverWorkflowProviderEventRequest{
		Event:        event,
		ProviderName: input.ProviderName,
	}, nil
}

func newOptionalWorkflowSignal(input *WorkflowSignal) (*proto.WorkflowSignal, error) {
	if input == nil {
		return nil, nil
	}
	return workflowSignalToProto(*input)
}

func newOptionalWorkflowEvent(input *WorkflowEvent) (*proto.WorkflowEvent, error) {
	if input == nil {
		return nil, nil
	}
	return workflowEventToProto(*input)
}

func workflowRunPtrFromProto(value *proto.WorkflowRun) (*WorkflowRun, error) {
	if value == nil {
		return nil, nil
	}
	run, err := workflowRunFromProto(value)
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func workflowRunSignalFromProto(value *proto.SignalWorkflowRunResponse) (*WorkflowRunSignal, error) {
	if value == nil {
		return nil, nil
	}
	run, err := workflowRunPtrFromProto(value.GetRun())
	if err != nil {
		return nil, err
	}
	var signal *WorkflowSignal
	if value.GetSignal() != nil {
		input := workflowSignalFromProto(value.GetSignal())
		signal = &input
	}
	return &WorkflowRunSignal{
		Run:         run,
		Signal:      signal,
		StartedRun:  value.GetStartedRun(),
		WorkflowKey: value.GetWorkflowKey(),
	}, nil
}

func workflowDefinitionPtrFromProto(value *proto.WorkflowDefinition) (*WorkflowDefinition, error) {
	if value == nil {
		return nil, nil
	}
	definition, err := workflowDefinitionFromProto(value)
	if err != nil {
		return nil, err
	}
	return &definition, nil
}

func workflowDefinitionsFromProto(values []*proto.WorkflowDefinition) ([]WorkflowDefinition, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]WorkflowDefinition, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		definition, err := workflowDefinitionFromProto(value)
		if err != nil {
			return nil, err
		}
		out = append(out, definition)
	}
	return out, nil
}

func workflowRunEventsFromProto(values []*proto.WorkflowRunEvent) []WorkflowRunEvent {
	if len(values) == 0 {
		return nil
	}
	out := make([]WorkflowRunEvent, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		out = append(out, workflowRunEventFromProto(value))
	}
	return out
}
