package gestalt

import (
	"fmt"

	proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"
)

type WorkflowManagerApplyDefinition struct {
	ProviderName   string
	Spec           *WorkflowDefinitionSpec
	IdempotencyKey string
}

type WorkflowManagerGetDefinition struct {
	DefinitionID string
}

type WorkflowManagerListDefinitions struct {
	ProviderName string
}

type WorkflowManagerDeleteDefinition struct {
	DefinitionID string
	Generation   int64
}

type WorkflowManagerSetDefinitionPaused struct {
	DefinitionID string
	Paused       bool
}

type WorkflowManagerSetActivationPaused struct {
	DefinitionID string
	ActivationID string
	Paused       bool
}

type WorkflowManagerStartRun struct {
	ProviderName         string
	DefinitionID         string
	DefinitionGeneration int64
	ActivationID         string
	WorkflowKey          string
	Input                any
	IdempotencyKey       string
}

type WorkflowManagerSignalRun struct {
	RunID  string
	Signal *WorkflowSignal
}

type WorkflowManagerSignalOrStartRun struct {
	ProviderName         string
	DefinitionID         string
	DefinitionGeneration int64
	ActivationID         string
	WorkflowKey          string
	Input                any
	IdempotencyKey       string
	Signal               *WorkflowSignal
}

type WorkflowManagerCancelRun struct {
	RunID  string
	Reason string
}

type WorkflowManagerDeliverEvent struct {
	ProviderName   string
	Event          *WorkflowEvent
	IdempotencyKey string
}

type WorkflowManagerDefinition struct {
	ProviderName string
	Definition   *WorkflowDefinition
}

type WorkflowManagerListDefinitionsResponse struct {
	Definitions []WorkflowManagerDefinition
}

func (r *WorkflowManagerListDefinitionsResponse) GetDefinitions() []WorkflowManagerDefinition {
	if r == nil {
		return nil
	}
	return r.Definitions
}

type WorkflowManagerRun struct {
	ProviderName string
	Run          *WorkflowRun
}

type WorkflowManagerRunSignal struct {
	ProviderName string
	Run          *WorkflowRun
	Signal       *WorkflowSignal
	StartedRun   bool
	WorkflowKey  string
}

type WorkflowManagerDeliverEventResponse struct {
	Results []WorkflowEventDeliveryResult
}

func (r *WorkflowManagerDeliverEventResponse) GetResults() []WorkflowEventDeliveryResult {
	if r == nil {
		return nil
	}
	return r.Results
}

func newWorkflowManagerApplyDefinitionRequest(input WorkflowManagerApplyDefinition) (*proto.WorkflowManagerApplyDefinitionRequest, error) {
	spec, err := newOptionalWorkflowDefinitionSpec(input.Spec)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowManagerApplyDefinitionRequest{
		ProviderName:   input.ProviderName,
		Spec:           spec,
		IdempotencyKey: input.IdempotencyKey,
	}, nil
}

func newWorkflowManagerGetDefinitionRequest(input WorkflowManagerGetDefinition) *proto.WorkflowManagerGetDefinitionRequest {
	return &proto.WorkflowManagerGetDefinitionRequest{DefinitionId: input.DefinitionID}
}

func newWorkflowManagerListDefinitionsRequest(input WorkflowManagerListDefinitions) *proto.WorkflowManagerListDefinitionsRequest {
	return &proto.WorkflowManagerListDefinitionsRequest{ProviderName: input.ProviderName}
}

func newWorkflowManagerDeleteDefinitionRequest(input WorkflowManagerDeleteDefinition) *proto.WorkflowManagerDeleteDefinitionRequest {
	return &proto.WorkflowManagerDeleteDefinitionRequest{
		DefinitionId: input.DefinitionID,
		Generation:   input.Generation,
	}
}

func newWorkflowManagerSetDefinitionPausedRequest(input WorkflowManagerSetDefinitionPaused) *proto.WorkflowManagerSetDefinitionPausedRequest {
	return &proto.WorkflowManagerSetDefinitionPausedRequest{
		DefinitionId: input.DefinitionID,
		Paused:       input.Paused,
	}
}

func newWorkflowManagerSetActivationPausedRequest(input WorkflowManagerSetActivationPaused) *proto.WorkflowManagerSetActivationPausedRequest {
	return &proto.WorkflowManagerSetActivationPausedRequest{
		DefinitionId: input.DefinitionID,
		ActivationId: input.ActivationID,
		Paused:       input.Paused,
	}
}

func newWorkflowManagerStartRunRequest(input WorkflowManagerStartRun) (*proto.WorkflowManagerStartRunRequest, error) {
	body, err := structFromAny(input.Input)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowManagerStartRunRequest{
		ProviderName:         input.ProviderName,
		DefinitionId:         input.DefinitionID,
		DefinitionGeneration: input.DefinitionGeneration,
		ActivationId:         input.ActivationID,
		WorkflowKey:          input.WorkflowKey,
		Input:                body,
		IdempotencyKey:       input.IdempotencyKey,
	}, nil
}

func newWorkflowManagerSignalRunRequest(input WorkflowManagerSignalRun) (*proto.WorkflowManagerSignalRunRequest, error) {
	signal, err := newOptionalWorkflowSignal(input.Signal)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowManagerSignalRunRequest{
		RunId:  input.RunID,
		Signal: signal,
	}, nil
}

func newWorkflowManagerSignalOrStartRunRequest(input WorkflowManagerSignalOrStartRun) (*proto.WorkflowManagerSignalOrStartRunRequest, error) {
	body, err := structFromAny(input.Input)
	if err != nil {
		return nil, err
	}
	signal, err := newOptionalWorkflowSignal(input.Signal)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowManagerSignalOrStartRunRequest{
		ProviderName:         input.ProviderName,
		DefinitionId:         input.DefinitionID,
		DefinitionGeneration: input.DefinitionGeneration,
		ActivationId:         input.ActivationID,
		WorkflowKey:          input.WorkflowKey,
		Input:                body,
		IdempotencyKey:       input.IdempotencyKey,
		Signal:               signal,
	}, nil
}

func newWorkflowManagerCancelRunRequest(input WorkflowManagerCancelRun) *proto.WorkflowManagerCancelRunRequest {
	return &proto.WorkflowManagerCancelRunRequest{
		RunId:  input.RunID,
		Reason: input.Reason,
	}
}

func newWorkflowManagerDeliverEventRequest(input WorkflowManagerDeliverEvent) (*proto.WorkflowManagerDeliverEventRequest, error) {
	event, err := newOptionalWorkflowEvent(input.Event)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowManagerDeliverEventRequest{
		ProviderName:   input.ProviderName,
		Event:          event,
		IdempotencyKey: input.IdempotencyKey,
	}, nil
}

func workflowManagerDefinitionFromProto(value *proto.ManagedWorkflowDefinition) (*WorkflowManagerDefinition, error) {
	if value == nil {
		return nil, nil
	}
	definition, err := workflowDefinitionFromProto(value.GetDefinition())
	if err != nil {
		return nil, err
	}
	return &WorkflowManagerDefinition{ProviderName: value.GetProviderName(), Definition: definition}, nil
}

func workflowManagerDefinitionsFromProto(values []*proto.ManagedWorkflowDefinition) ([]WorkflowManagerDefinition, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]WorkflowManagerDefinition, 0, len(values))
	for i, value := range values {
		deployment, err := workflowManagerDefinitionFromProto(value)
		if err != nil {
			return nil, fmt.Errorf("definitions[%d]: %w", i, err)
		}
		if deployment != nil {
			out = append(out, *deployment)
		}
	}
	return out, nil
}

func workflowManagerRunFromProto(value *proto.ManagedWorkflowRun) (*WorkflowManagerRun, error) {
	if value == nil {
		return nil, nil
	}
	run, err := workflowRunFromProto(value.GetRun())
	if err != nil {
		return nil, err
	}
	return &WorkflowManagerRun{ProviderName: value.GetProviderName(), Run: run}, nil
}

func workflowManagerRunSignalFromProto(value *proto.ManagedWorkflowRunSignal) (*WorkflowManagerRunSignal, error) {
	if value == nil {
		return nil, nil
	}
	run, err := workflowRunFromProto(value.GetRun())
	if err != nil {
		return nil, err
	}
	var signal *WorkflowSignal
	if value.GetSignal() != nil {
		input := workflowSignalFromProto(value.GetSignal())
		signal = &input
	}
	return &WorkflowManagerRunSignal{
		ProviderName: value.GetProviderName(),
		Run:          run,
		Signal:       signal,
		StartedRun:   value.GetStartedRun(),
		WorkflowKey:  value.GetWorkflowKey(),
	}, nil
}

func workflowManagerDeliverEventResponseFromProto(value *proto.WorkflowManagerDeliverEventResponse) (*WorkflowManagerDeliverEventResponse, error) {
	if value == nil {
		return nil, nil
	}
	results, err := workflowEventDeliveryResultsFromProto(value.GetResults())
	if err != nil {
		return nil, err
	}
	return &WorkflowManagerDeliverEventResponse{Results: results}, nil
}
