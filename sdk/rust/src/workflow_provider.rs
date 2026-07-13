use std::collections::BTreeMap;
use std::sync::Arc;

use serde::Serialize;
use serde_json::Value;
use tonic::codegen::async_trait;
use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};

use crate::api::RuntimeMetadata;
use crate::error::Result as ProviderResult;
use crate::generated::v1 as pb;
use crate::protocol;
use crate::rpc_status::rpc_status;
use crate::{Error, Result};

/// Native JSON object used by authored workflow providers.
pub type WorkflowJson = serde_json::Value;

pub use pb::{workflow_activation, workflow_run_trigger, workflow_step, workflow_value};

/// Alias for `pb::BoundWorkflowTarget`.
pub type BoundWorkflowTarget = pb::BoundWorkflowTarget;
/// Alias for `pb::WorkflowStep`.
pub type WorkflowStep = pb::WorkflowStep;
/// Alias for `pb::workflow_step::Action`.
pub type WorkflowStepAction = pb::workflow_step::Action;
/// Alias for `pb::WorkflowStepAppCall`.
pub type WorkflowStepAppCall = pb::WorkflowStepAppCall;
/// Alias for `pb::WorkflowStepAgentTurn`.
pub type WorkflowStepAgentTurn = pb::WorkflowStepAgentTurn;
/// Alias for `pb::WorkflowAgentMessage`.
pub type WorkflowAgentMessage = pb::WorkflowAgentMessage;
/// Alias for `pb::WorkflowText`.
pub type WorkflowText = pb::WorkflowText;
/// Alias for `pb::WorkflowStepWhen`.
pub type WorkflowStepWhen = pb::WorkflowStepWhen;
/// Alias for `pb::WorkflowValue`.
pub type WorkflowValue = pb::WorkflowValue;
/// Alias for `pb::WorkflowObject`.
pub type WorkflowObject = pb::WorkflowObject;
/// Alias for `pb::WorkflowArray`.
pub type WorkflowArray = pb::WorkflowArray;
/// Alias for `pb::WorkflowPathSource`.
pub type WorkflowPathSource = pb::WorkflowPathSource;
/// Alias for `pb::WorkflowStepOutputSource`.
pub type WorkflowStepOutputSource = pb::WorkflowStepOutputSource;
/// Alias for `pb::WorkflowStepInputSource`.
pub type WorkflowStepInputSource = pb::WorkflowStepInputSource;
/// Alias for `pb::WorkflowEvent`.
pub type WorkflowEvent = pb::WorkflowEvent;
/// Alias for `pb::WorkflowEventMatch`.
pub type WorkflowEventMatch = pb::WorkflowEventMatch;
/// Alias for `pb::WorkflowScheduleActivation`.
pub type WorkflowScheduleActivation = pb::WorkflowScheduleActivation;
/// Alias for `pb::WorkflowEventActivation`.
pub type WorkflowEventActivation = pb::WorkflowEventActivation;
/// Alias for `pb::WorkflowActivation`.
pub type WorkflowActivation = pb::WorkflowActivation;
/// Alias for `pb::WorkflowDefinitionSpec`.
pub type WorkflowDefinitionSpec = pb::WorkflowDefinitionSpec;
/// Alias for `pb::WorkflowDefinition`.
pub type WorkflowDefinition = pb::WorkflowDefinition;
/// Alias for `pb::WorkflowManualTrigger`.
pub type WorkflowManualTrigger = pb::WorkflowManualTrigger;
/// Alias for `pb::WorkflowScheduleTrigger`.
pub type WorkflowScheduleTrigger = pb::WorkflowScheduleTrigger;
/// Alias for `pb::WorkflowEventTriggerInvocation`.
pub type WorkflowEventTriggerInvocation = pb::WorkflowEventTriggerInvocation;
/// Alias for `pb::WorkflowRunTrigger`.
pub type WorkflowRunTrigger = pb::WorkflowRunTrigger;
/// Alias for `pb::WorkflowStepAttempt`.
pub type WorkflowStepAttempt = pb::WorkflowStepAttempt;
/// Alias for `pb::WorkflowStepExecution`.
pub type WorkflowStepExecution = pb::WorkflowStepExecution;
/// Alias for `pb::WorkflowRun`.
pub type WorkflowRun = pb::WorkflowRun;
/// Alias for `pb::WorkflowSignal`.
pub type WorkflowSignal = pb::WorkflowSignal;
/// Alias for `pb::SignalWorkflowRunResponse`.
pub type SignalWorkflowRunResponse = pb::SignalWorkflowRunResponse;
/// Alias for `pb::WorkflowRunEvent`.
pub type WorkflowRunEvent = pb::WorkflowRunEvent;
/// Alias for `pb::WorkflowRunStatus`.
pub type WorkflowRunStatus = pb::WorkflowRunStatus;
/// Alias for `pb::WorkflowStepStatus`.
pub type WorkflowStepStatus = pb::WorkflowStepStatus;

/// Alias for `pb::ApplyWorkflowProviderDefinitionRequest`.
pub type ApplyWorkflowProviderDefinitionRequest = pb::ApplyWorkflowProviderDefinitionRequest;
/// Alias for `pb::GetWorkflowProviderDefinitionRequest`.
pub type GetWorkflowProviderDefinitionRequest = pb::GetWorkflowProviderDefinitionRequest;
/// Alias for `pb::ListWorkflowProviderDefinitionsRequest`.
pub type ListWorkflowProviderDefinitionsRequest = pb::ListWorkflowProviderDefinitionsRequest;
/// Alias for `pb::ListWorkflowProviderDefinitionsResponse`.
pub type ListWorkflowProviderDefinitionsResponse = pb::ListWorkflowProviderDefinitionsResponse;
/// Alias for the definitionpaused request message.
pub type SetWorkflowProviderDefinitionPausedRequest =
    pb::SetWorkflowProviderDefinitionPausedRequest;
/// Alias for the activationpaused request message.
pub type SetWorkflowProviderActivationPausedRequest =
    pb::SetWorkflowProviderActivationPausedRequest;
/// Alias for `pb::DeleteWorkflowProviderDefinitionRequest`.
pub type DeleteWorkflowProviderDefinitionRequest = pb::DeleteWorkflowProviderDefinitionRequest;
/// Alias for `pb::StartWorkflowProviderRunRequest`.
pub type StartWorkflowProviderRunRequest = pb::StartWorkflowProviderRunRequest;
/// Alias for `pb::GetWorkflowProviderRunRequest`.
pub type GetWorkflowProviderRunRequest = pb::GetWorkflowProviderRunRequest;
/// Alias for `pb::ListWorkflowProviderRunsRequest`.
pub type ListWorkflowProviderRunsRequest = pb::ListWorkflowProviderRunsRequest;
/// Alias for `pb::ListWorkflowProviderRunsResponse`.
pub type ListWorkflowProviderRunsResponse = pb::ListWorkflowProviderRunsResponse;
/// Alias for `pb::GetWorkflowProviderRunEventsRequest`.
pub type GetWorkflowProviderRunEventsRequest = pb::GetWorkflowProviderRunEventsRequest;
/// Alias for `pb::GetWorkflowProviderRunEventsResponse`.
pub type GetWorkflowProviderRunEventsResponse = pb::GetWorkflowProviderRunEventsResponse;
/// Alias for `pb::GetWorkflowProviderRunOutputRequest`.
pub type GetWorkflowProviderRunOutputRequest = pb::GetWorkflowProviderRunOutputRequest;
/// Alias for `pb::GetWorkflowProviderRunOutputResponse`.
pub type GetWorkflowProviderRunOutputResponse = pb::GetWorkflowProviderRunOutputResponse;
/// Alias for `pb::CancelWorkflowProviderRunRequest`.
pub type CancelWorkflowProviderRunRequest = pb::CancelWorkflowProviderRunRequest;
/// Alias for `pb::SignalWorkflowProviderRunRequest`.
pub type SignalWorkflowProviderRunRequest = pb::SignalWorkflowProviderRunRequest;
/// Alias for `pb::SignalOrStartWorkflowProviderRunRequest`.
pub type SignalOrStartWorkflowProviderRunRequest = pb::SignalOrStartWorkflowProviderRunRequest;
/// Alias for `pb::DeliverWorkflowProviderEventRequest`.
pub type DeliverWorkflowProviderEventRequest = pb::DeliverWorkflowProviderEventRequest;

/// Validates and returns the bound workflow target value.
pub fn new_bound_workflow_target(
    input: BoundWorkflowTarget,
) -> ProviderResult<BoundWorkflowTarget> {
    Ok(input)
}
/// Builds the value from an existing target.
pub fn new_bound_workflow_target_from_target(
    input: &BoundWorkflowTarget,
) -> ProviderResult<BoundWorkflowTarget> {
    Ok(input.clone())
}

/// Validates and returns the workflow definition spec value.
pub fn new_workflow_definition_spec(
    input: WorkflowDefinitionSpec,
) -> ProviderResult<WorkflowDefinitionSpec> {
    Ok(input)
}

/// Validates and returns the workflow definition value.
pub fn new_workflow_definition(input: WorkflowDefinition) -> ProviderResult<WorkflowDefinition> {
    Ok(input)
}

/// Validates and returns the workflow run value.
pub fn new_workflow_run(input: WorkflowRun) -> ProviderResult<WorkflowRun> {
    Ok(input)
}

/// Builds the value from an existing run.
pub fn new_workflow_run_from_run(input: &WorkflowRun) -> ProviderResult<WorkflowRun> {
    Ok(input.clone())
}

/// Validates and returns the workflow event value.
pub fn new_workflow_event(input: WorkflowEvent) -> ProviderResult<WorkflowEvent> {
    Ok(input)
}

/// Builds the value from an existing event.
pub fn new_workflow_event_from_event(input: &WorkflowEvent) -> ProviderResult<WorkflowEvent> {
    Ok(input.clone())
}

/// Validates and returns the workflow event match value.
pub fn new_workflow_event_match(input: WorkflowEventMatch) -> WorkflowEventMatch {
    input
}

/// Validates and returns the workflow signal value.
pub fn new_workflow_signal(input: WorkflowSignal) -> ProviderResult<WorkflowSignal> {
    Ok(input)
}

/// Builds the value from an existing signal.
pub fn new_workflow_signal_from_signal(input: &WorkflowSignal) -> ProviderResult<WorkflowSignal> {
    Ok(input.clone())
}

/// Validates and returns the workflow step value.
pub fn new_workflow_step(input: WorkflowStep) -> ProviderResult<WorkflowStep> {
    Ok(input)
}

/// Validates and returns the workflow step app call value.
pub fn new_workflow_step_app_call(
    input: WorkflowStepAppCall,
) -> ProviderResult<WorkflowStepAppCall> {
    Ok(input)
}

/// Validates and returns the workflow step agent turn value.
pub fn new_workflow_step_agent_turn(
    input: WorkflowStepAgentTurn,
) -> ProviderResult<WorkflowStepAgentTurn> {
    Ok(input)
}

/// Validates and returns the workflow agent message value.
pub fn new_workflow_agent_message(
    input: WorkflowAgentMessage,
) -> ProviderResult<WorkflowAgentMessage> {
    Ok(input)
}

/// Validates and returns the workflow step when value.
pub fn new_workflow_step_when(input: WorkflowStepWhen) -> ProviderResult<WorkflowStepWhen> {
    Ok(input)
}

/// Validates and returns the workflow text value.
pub fn new_workflow_text(input: WorkflowText) -> WorkflowText {
    input
}

/// Validates and returns the workflow value value.
pub fn new_workflow_value(input: WorkflowValue) -> ProviderResult<WorkflowValue> {
    Ok(input)
}

/// Builds a workflow value carrying a literal JSON value.
pub fn workflow_value_literal<T: Serialize>(value: T) -> ProviderResult<WorkflowValue> {
    Ok(WorkflowValue {
        kind: Some(workflow_value::Kind::Literal(protocol::value_from_json(
            protocol::json_from_serializable(value)?,
        ))),
    })
}

/// Builds a workflow value from named member values.
pub fn workflow_value_object(fields: BTreeMap<String, WorkflowValue>) -> WorkflowValue {
    WorkflowValue {
        kind: Some(workflow_value::Kind::Object(WorkflowObject { fields })),
    }
}

/// Builds a workflow value from element values.
pub fn workflow_value_array(values: Vec<WorkflowValue>) -> WorkflowValue {
    WorkflowValue {
        kind: Some(workflow_value::Kind::Array(WorkflowArray { values })),
    }
}

/// Builds a workflow value rendered from a text template.
pub fn workflow_value_template(template: impl Into<String>) -> WorkflowValue {
    WorkflowValue {
        kind: Some(workflow_value::Kind::Template(WorkflowText {
            template: template.into(),
        })),
    }
}

/// Builds a workflow value read from a run input path.
pub fn workflow_value_input(path: impl Into<String>) -> WorkflowValue {
    WorkflowValue {
        kind: Some(workflow_value::Kind::Input(WorkflowPathSource {
            path: path.into(),
        })),
    }
}

/// Builds a workflow value read from a delivered signal path.
pub fn workflow_value_signal(path: impl Into<String>) -> WorkflowValue {
    WorkflowValue {
        kind: Some(workflow_value::Kind::Signal(WorkflowPathSource {
            path: path.into(),
        })),
    }
}

/// Builds a workflow value read from a prior step's output.
pub fn workflow_value_step_output(
    step_id: impl Into<String>,
    path: impl Into<String>,
) -> WorkflowValue {
    WorkflowValue {
        kind: Some(workflow_value::Kind::StepOutput(WorkflowStepOutputSource {
            step_id: step_id.into(),
            path: path.into(),
        })),
    }
}

/// Builds a workflow value read from a step's input.
pub fn workflow_value_step_input(
    step_id: impl Into<String>,
    path: impl Into<String>,
) -> WorkflowValue {
    WorkflowValue {
        kind: Some(workflow_value::Kind::StepInput(WorkflowStepInputSource {
            step_id: step_id.into(),
            path: path.into(),
        })),
    }
}

/// Builds the input form of an existing event.
pub fn workflow_event_input_from_event(input: &WorkflowEvent) -> WorkflowEvent {
    input.clone()
}

/// Builds the input form of an existing match.
pub fn workflow_event_match_input_from_match(input: &WorkflowEventMatch) -> WorkflowEventMatch {
    input.clone()
}

/// Builds the input form of an existing signal.
pub fn workflow_signal_input_from_signal(input: &WorkflowSignal) -> WorkflowSignal {
    input.clone()
}

/// Builds the input form of an existing step.
pub fn workflow_step_input_from_step(input: &WorkflowStep) -> WorkflowStep {
    input.clone()
}

/// Builds the input form of an existing call.
pub fn workflow_step_app_call_input_from_call(input: &WorkflowStepAppCall) -> WorkflowStepAppCall {
    input.clone()
}

/// Builds the input form of an existing turn.
pub fn workflow_step_agent_turn_input_from_turn(
    input: &WorkflowStepAgentTurn,
) -> WorkflowStepAgentTurn {
    input.clone()
}

/// Builds the input form of an existing value.
pub fn workflow_value_input_from_value(input: &WorkflowValue) -> WorkflowValue {
    input.clone()
}

/// Builds the input form of an existing trigger.
pub fn workflow_run_trigger_input_from_trigger(input: &WorkflowRunTrigger) -> WorkflowRunTrigger {
    input.clone()
}

#[async_trait]
/// Provider trait for serving the Gestalt workflow-provider protocol.
pub trait WorkflowProvider: Send + Sync + 'static {
    /// Configures the provider before it starts serving requests.
    async fn configure(
        &self,
        _name: &str,
        _config: serde_json::Map<String, serde_json::Value>,
    ) -> ProviderResult<()> {
        Ok(())
    }

    /// Returns runtime metadata that should augment the static manifest.
    fn metadata(&self) -> Option<RuntimeMetadata> {
        None
    }

    /// Returns non-fatal warnings the host should surface to users.
    fn warnings(&self) -> Vec<String> {
        Vec::new()
    }

    /// Performs an optional health check.
    async fn health_check(&self) -> ProviderResult<()> {
        Ok(())
    }

    /// Starts provider-owned background work after configuration.
    async fn start(&self) -> ProviderResult<()> {
        Ok(())
    }

    /// Shuts the provider down before the runtime exits.
    async fn close(&self) -> ProviderResult<()> {
        Ok(())
    }

    /// Applies a workflow definition, creating or updating it.
    async fn apply_definition(
        &self,
        _request: ApplyWorkflowProviderDefinitionRequest,
    ) -> ProviderResult<WorkflowDefinition> {
        Err(crate::Error::unimplemented(
            "workflow apply definition is not implemented",
        ))
    }

    /// Fetches one workflow definition.
    async fn get_definition(
        &self,
        _request: GetWorkflowProviderDefinitionRequest,
    ) -> ProviderResult<WorkflowDefinition> {
        Err(crate::Error::unimplemented(
            "workflow get definition is not implemented",
        ))
    }

    /// Lists workflow definitions.
    async fn list_definitions(
        &self,
        _request: ListWorkflowProviderDefinitionsRequest,
    ) -> ProviderResult<ListWorkflowProviderDefinitionsResponse> {
        Err(crate::Error::unimplemented(
            "workflow list definitions is not implemented",
        ))
    }

    /// Pauses or resumes a workflow definition.
    async fn set_definition_paused(
        &self,
        _request: SetWorkflowProviderDefinitionPausedRequest,
    ) -> ProviderResult<WorkflowDefinition> {
        Err(crate::Error::unimplemented(
            "workflow set definition paused is not implemented",
        ))
    }

    /// Pauses or resumes one activation.
    async fn set_activation_paused(
        &self,
        _request: SetWorkflowProviderActivationPausedRequest,
    ) -> ProviderResult<WorkflowDefinition> {
        Err(crate::Error::unimplemented(
            "workflow set activation paused is not implemented",
        ))
    }

    /// Deletes a workflow definition.
    async fn delete_definition(
        &self,
        _request: DeleteWorkflowProviderDefinitionRequest,
    ) -> ProviderResult<()> {
        Err(crate::Error::unimplemented(
            "workflow delete definition is not implemented",
        ))
    }

    /// Starts a workflow run.
    async fn start_run(
        &self,
        _request: StartWorkflowProviderRunRequest,
    ) -> ProviderResult<WorkflowRun> {
        Err(crate::Error::unimplemented(
            "workflow start run is not implemented",
        ))
    }

    /// Lists workflow runs.
    async fn list_runs(
        &self,
        _request: ListWorkflowProviderRunsRequest,
    ) -> ProviderResult<ListWorkflowProviderRunsResponse> {
        Err(crate::Error::unimplemented(
            "workflow list runs is not implemented",
        ))
    }

    /// Fetches one workflow run.
    async fn get_run(
        &self,
        _request: GetWorkflowProviderRunRequest,
    ) -> ProviderResult<WorkflowRun> {
        Err(crate::Error::unimplemented(
            "workflow get run is not implemented",
        ))
    }

    /// Fetches the events of a workflow run.
    async fn get_run_events(
        &self,
        _request: GetWorkflowProviderRunEventsRequest,
    ) -> ProviderResult<GetWorkflowProviderRunEventsResponse> {
        Err(crate::Error::unimplemented(
            "workflow get run events is not implemented",
        ))
    }

    /// Fetches the output value of a workflow run.
    async fn get_run_output(
        &self,
        _request: GetWorkflowProviderRunOutputRequest,
    ) -> ProviderResult<GetWorkflowProviderRunOutputResponse> {
        Err(crate::Error::unimplemented(
            "workflow get run output is not implemented",
        ))
    }

    /// Cancels a workflow run.
    async fn cancel_run(
        &self,
        _request: CancelWorkflowProviderRunRequest,
    ) -> ProviderResult<WorkflowRun> {
        Err(crate::Error::unimplemented(
            "workflow cancel run is not implemented",
        ))
    }

    /// Signals an existing workflow run.
    async fn signal_run(
        &self,
        _request: SignalWorkflowProviderRunRequest,
    ) -> ProviderResult<SignalWorkflowRunResponse> {
        Err(crate::Error::unimplemented(
            "workflow signal run is not implemented",
        ))
    }

    /// Signals a run, starting it first when absent.
    async fn signal_or_start_run(
        &self,
        _request: SignalOrStartWorkflowProviderRunRequest,
    ) -> ProviderResult<SignalWorkflowRunResponse> {
        Err(crate::Error::unimplemented(
            "workflow signal or start run is not implemented",
        ))
    }

    /// Delivers an external event to the workflow engine.
    async fn deliver_event(
        &self,
        _request: DeliverWorkflowProviderEventRequest,
    ) -> ProviderResult<WorkflowEvent> {
        Err(crate::Error::unimplemented(
            "workflow deliver event is not implemented",
        ))
    }
}

#[derive(Clone)]
pub(crate) struct WorkflowServer<P> {
    provider: Arc<P>,
}

impl<P> WorkflowServer<P> {
    pub(crate) fn new(provider: Arc<P>) -> Self {
        Self { provider }
    }
}

#[async_trait]
impl<P> pb::workflow_server::Workflow for WorkflowServer<P>
where
    P: WorkflowProvider,
{
    async fn apply_definition(
        &self,
        request: GrpcRequest<pb::ApplyWorkflowProviderDefinitionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::WorkflowDefinition>, Status> {
        let definition = self
            .provider
            .apply_definition(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow apply definition", error))?;
        Ok(GrpcResponse::new(definition))
    }

    async fn get_definition(
        &self,
        request: GrpcRequest<pb::GetWorkflowProviderDefinitionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::WorkflowDefinition>, Status> {
        let definition = self
            .provider
            .get_definition(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow get definition", error))?;
        Ok(GrpcResponse::new(definition))
    }

    async fn list_definitions(
        &self,
        request: GrpcRequest<pb::ListWorkflowProviderDefinitionsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListWorkflowProviderDefinitionsResponse>, Status>
    {
        let response = self
            .provider
            .list_definitions(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow list definitions", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn set_definition_paused(
        &self,
        request: GrpcRequest<pb::SetWorkflowProviderDefinitionPausedRequest>,
    ) -> std::result::Result<GrpcResponse<pb::WorkflowDefinition>, Status> {
        let definition = self
            .provider
            .set_definition_paused(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow set definition paused", error))?;
        Ok(GrpcResponse::new(definition))
    }

    async fn set_activation_paused(
        &self,
        request: GrpcRequest<pb::SetWorkflowProviderActivationPausedRequest>,
    ) -> std::result::Result<GrpcResponse<pb::WorkflowDefinition>, Status> {
        let definition = self
            .provider
            .set_activation_paused(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow set activation paused", error))?;
        Ok(GrpcResponse::new(definition))
    }

    async fn delete_definition(
        &self,
        request: GrpcRequest<pb::DeleteWorkflowProviderDefinitionRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        self.provider
            .delete_definition(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow delete definition", error))?;
        Ok(GrpcResponse::new(()))
    }

    async fn start_run(
        &self,
        request: GrpcRequest<pb::StartWorkflowProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<pb::WorkflowRun>, Status> {
        let run = self
            .provider
            .start_run(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow start run", error))?;
        Ok(GrpcResponse::new(run))
    }

    async fn list_runs(
        &self,
        request: GrpcRequest<pb::ListWorkflowProviderRunsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListWorkflowProviderRunsResponse>, Status> {
        let response = self
            .provider
            .list_runs(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow list runs", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn get_run(
        &self,
        request: GrpcRequest<pb::GetWorkflowProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<pb::WorkflowRun>, Status> {
        let run = self
            .provider
            .get_run(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow get run", error))?;
        Ok(GrpcResponse::new(run))
    }

    async fn get_run_events(
        &self,
        request: GrpcRequest<pb::GetWorkflowProviderRunEventsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::GetWorkflowProviderRunEventsResponse>, Status> {
        let response = self
            .provider
            .get_run_events(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow get run events", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn get_run_output(
        &self,
        request: GrpcRequest<pb::GetWorkflowProviderRunOutputRequest>,
    ) -> std::result::Result<GrpcResponse<pb::GetWorkflowProviderRunOutputResponse>, Status> {
        let response = self
            .provider
            .get_run_output(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow get run output", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn cancel_run(
        &self,
        request: GrpcRequest<pb::CancelWorkflowProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<pb::WorkflowRun>, Status> {
        let run = self
            .provider
            .cancel_run(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow cancel run", error))?;
        Ok(GrpcResponse::new(run))
    }

    async fn signal_run(
        &self,
        request: GrpcRequest<pb::SignalWorkflowProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<pb::SignalWorkflowRunResponse>, Status> {
        let response = self
            .provider
            .signal_run(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow signal run", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn signal_or_start_run(
        &self,
        request: GrpcRequest<pb::SignalOrStartWorkflowProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<pb::SignalWorkflowRunResponse>, Status> {
        let response = self
            .provider
            .signal_or_start_run(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow signal or start run", error))?;
        Ok(GrpcResponse::new(response))
    }

    async fn deliver_event(
        &self,
        request: GrpcRequest<pb::DeliverWorkflowProviderEventRequest>,
    ) -> std::result::Result<GrpcResponse<pb::WorkflowEvent>, Status> {
        let event = self
            .provider
            .deliver_event(request.into_inner())
            .await
            .map_err(|error| rpc_status("workflow deliver event", error))?;
        Ok(GrpcResponse::new(event))
    }
}

#[derive(Clone, Debug, Default, PartialEq)]
/// One workflow callback execution request.
pub struct WorkflowExecutionRequest {
    /// The `provider_name` field.
    pub provider_name: String,
    /// The `run_id` field.
    pub run_id: String,
    /// The `target` field.
    pub target: Option<BoundWorkflowTarget>,
    /// The `trigger` field.
    pub trigger: Option<WorkflowRunTrigger>,
    /// The `input` field.
    pub input: Option<Value>,
    /// The `metadata` field.
    pub metadata: Option<Value>,
    /// The `signals` field.
    pub signals: Vec<WorkflowSignal>,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// The run context a workflow value evaluates against.
pub struct WorkflowEvalContext {
    /// The `request` field.
    pub request: WorkflowExecutionRequest,
    /// The `outputs` field.
    pub outputs: BTreeMap<String, Value>,
    /// The `inputs` field.
    pub inputs: BTreeMap<String, Value>,
    /// The `allow_inputs` field.
    pub allow_inputs: bool,
}

#[derive(Clone, Debug, PartialEq)]
/// The resolved value of one workflow value expression.
pub struct WorkflowEvalResult {
    /// The `value` field.
    pub value: Option<Value>,
    /// The `resolved` field.
    pub resolved: bool,
}

/// Resolves one workflow value expression against a run context.
pub fn evaluate_workflow_value(
    ctx: &WorkflowEvalContext,
    value: &WorkflowValue,
) -> Result<WorkflowEvalResult> {
    match value.kind.as_ref() {
        None => Ok(WorkflowEvalResult {
            value: None,
            resolved: true,
        }),
        Some(workflow_value::Kind::Literal(value)) => Ok(WorkflowEvalResult {
            value: Some(protocol::json_from_value(value)),
            resolved: true,
        }),
        Some(workflow_value::Kind::Object(object)) => {
            let mut out = serde_json::Map::new();
            for (key, nested) in &object.fields {
                let resolved = evaluate_workflow_value(ctx, nested)?;
                if !resolved.resolved {
                    return Ok(WorkflowEvalResult {
                        value: None,
                        resolved: false,
                    });
                }
                out.insert(key.clone(), resolved.value.unwrap_or(Value::Null));
            }
            Ok(WorkflowEvalResult {
                value: Some(Value::Object(out)),
                resolved: true,
            })
        }
        Some(workflow_value::Kind::Array(array)) => {
            let mut out = Vec::with_capacity(array.values.len());
            for nested in &array.values {
                let resolved = evaluate_workflow_value(ctx, nested)?;
                if !resolved.resolved {
                    return Ok(WorkflowEvalResult {
                        value: None,
                        resolved: false,
                    });
                }
                out.push(resolved.value.unwrap_or(Value::Null));
            }
            Ok(WorkflowEvalResult {
                value: Some(Value::Array(out)),
                resolved: true,
            })
        }
        Some(workflow_value::Kind::Template(text)) => Ok(WorkflowEvalResult {
            value: Some(Value::String(render_workflow_template(
                ctx,
                &text.template,
            )?)),
            resolved: true,
        }),
        Some(workflow_value::Kind::Input(source)) => {
            path_value_option(ctx.request.input.as_ref(), &source.path)
        }
        Some(workflow_value::Kind::Signal(source)) => latest_signal_payload(ctx)
            .map(|payload| path_value(&payload, &source.path))
            .unwrap_or_else(|| {
                Ok(WorkflowEvalResult {
                    value: None,
                    resolved: false,
                })
            }),
        Some(workflow_value::Kind::StepOutput(source)) => {
            match ctx.outputs.get(source.step_id.trim()) {
                Some(output) => path_value(output, &source.path),
                None => Err(Error::bad_request(format!(
                    "workflow step output references missing step {:?}",
                    source.step_id.trim()
                ))),
            }
        }
        Some(workflow_value::Kind::StepInput(source)) => {
            if !ctx.allow_inputs {
                return Err(Error::bad_request(
                    "step input references are not allowed here",
                ));
            }
            match ctx.inputs.get(source.step_id.trim()) {
                Some(input) => path_value(input, &source.path),
                None => Ok(WorkflowEvalResult {
                    value: None,
                    resolved: false,
                }),
            }
        }
    }
}

/// Renders a workflow text template against provided values.
pub fn render_workflow_template(ctx: &WorkflowEvalContext, template: &str) -> Result<String> {
    let mut out = String::new();
    let mut i = 0;
    while i < template.len() {
        let remaining = &template[i..];
        if remaining.starts_with("$${{") {
            out.push_str("${{");
            i += 4;
            continue;
        }
        if !remaining.starts_with("${{") {
            let ch = remaining.chars().next().expect("non-empty string");
            out.push(ch);
            i += ch.len_utf8();
            continue;
        }
        let end = remaining[3..]
            .find("}}")
            .ok_or_else(|| Error::bad_request("unterminated template expression"))?;
        let expr = remaining[3..3 + end].trim();
        let resolved = template_expression_value(ctx, expr)?;
        if !resolved.resolved {
            return Err(Error::bad_request(format!(
                "template expression {expr:?} did not resolve"
            )));
        }
        out.push_str(&render_template_value(
            resolved.value.unwrap_or(Value::Null),
        )?);
        i += 3 + end + 2;
    }
    Ok(out)
}

/// Returns the most recent delivery of a named signal.
pub fn latest_workflow_signal(signals: &[WorkflowSignal]) -> Option<&WorkflowSignal> {
    signals.last()
}

/// Reads a dotted path from a JSON-like value.
pub fn path_value(root: &Value, path: &str) -> Result<WorkflowEvalResult> {
    if path.trim().is_empty() {
        return Ok(WorkflowEvalResult {
            value: Some(root.clone()),
            resolved: true,
        });
    }
    let mut current = root;
    for segment in path_segments(path)? {
        match (current, segment) {
            (Value::Object(map), PathSegment::Key(key)) => match map.get(&key) {
                Some(next) => current = next,
                None => {
                    return Ok(WorkflowEvalResult {
                        value: None,
                        resolved: false,
                    });
                }
            },
            (Value::Array(values), PathSegment::Index(index)) if index < values.len() => {
                current = &values[index];
            }
            _ => {
                return Ok(WorkflowEvalResult {
                    value: None,
                    resolved: false,
                });
            }
        }
    }
    Ok(WorkflowEvalResult {
        value: Some(current.clone()),
        resolved: true,
    })
}

fn path_value_option(root: Option<&Value>, path: &str) -> Result<WorkflowEvalResult> {
    match root {
        Some(root) => path_value(root, path),
        None => Ok(WorkflowEvalResult {
            value: None,
            resolved: false,
        }),
    }
}

fn template_expression_value(ctx: &WorkflowEvalContext, expr: &str) -> Result<WorkflowEvalResult> {
    if expr == "input" {
        return path_value_option(ctx.request.input.as_ref(), "");
    }
    if let Some(path) = expr.strip_prefix("input.") {
        return path_value_option(ctx.request.input.as_ref(), path);
    }
    if expr == "signal" {
        return latest_signal_payload(ctx)
            .map(|payload| path_value(&payload, ""))
            .unwrap_or_else(|| {
                Ok(WorkflowEvalResult {
                    value: None,
                    resolved: false,
                })
            });
    }
    if let Some(path) = expr.strip_prefix("signal.") {
        return latest_signal_payload(ctx)
            .map(|payload| path_value(&payload, path))
            .unwrap_or_else(|| {
                Ok(WorkflowEvalResult {
                    value: None,
                    resolved: false,
                })
            });
    }
    if let Some(path) = expr.strip_prefix("steps.") {
        return step_expression_value(ctx, path);
    }
    Err(Error::bad_request(format!(
        "unsupported template expression {expr:?}"
    )))
}

fn step_expression_value(ctx: &WorkflowEvalContext, path: &str) -> Result<WorkflowEvalResult> {
    let Some((step_id, rest)) = path.split_once('.') else {
        return Err(Error::bad_request(format!(
            "unsupported step template expression {path:?}"
        )));
    };
    if let Some(path) = rest.strip_prefix("outputs") {
        let path = path.strip_prefix('.').unwrap_or("");
        return match ctx.outputs.get(step_id.trim()) {
            Some(output) => path_value(output, path),
            None => Err(Error::bad_request(format!(
                "workflow step output references missing step {:?}",
                step_id.trim()
            ))),
        };
    }
    if let Some(path) = rest.strip_prefix("inputs") {
        if !ctx.allow_inputs {
            return Err(Error::bad_request(
                "step input references are not allowed here",
            ));
        }
        let path = path.strip_prefix('.').unwrap_or("");
        return match ctx.inputs.get(step_id.trim()) {
            Some(input) => path_value(input, path),
            None => Ok(WorkflowEvalResult {
                value: None,
                resolved: false,
            }),
        };
    }
    Err(Error::bad_request(format!(
        "unsupported step template expression {path:?}"
    )))
}

fn latest_signal_payload(ctx: &WorkflowEvalContext) -> Option<Value> {
    latest_workflow_signal(&ctx.request.signals)
        .and_then(|signal| signal.payload.as_ref())
        .map(protocol::json_from_struct)
}

fn render_template_value(value: Value) -> Result<String> {
    match value {
        Value::String(value) => Ok(value),
        other => Ok(serde_json::to_string(&other)?),
    }
}

#[derive(Debug)]
enum PathSegment {
    Key(String),
    Index(usize),
}

fn path_segments(path: &str) -> Result<Vec<PathSegment>> {
    let chars: Vec<char> = path.trim().chars().collect();
    let mut out = Vec::new();
    let mut i = 0;
    while i < chars.len() {
        match chars[i] {
            '.' => i += 1,
            '[' => {
                let start = i + 1;
                let mut end = start;
                while end < chars.len() && chars[end] != ']' {
                    end += 1;
                }
                if end >= chars.len() {
                    return Err(Error::bad_request(format!(
                        "invalid workflow path {path:?}"
                    )));
                }
                let token: String = chars[start..end].iter().collect();
                let token = token.trim();
                if token.starts_with('"') || token.starts_with('\'') {
                    out.push(PathSegment::Key(unquote_path_key(token, path)?));
                } else {
                    out.push(PathSegment::Index(token.parse().map_err(|_| {
                        Error::bad_request(format!("invalid workflow path {path:?}"))
                    })?));
                }
                i = end + 1;
            }
            _ => {
                let start = i;
                while i < chars.len() && chars[i] != '.' && chars[i] != '[' {
                    i += 1;
                }
                let key: String = chars[start..i].iter().collect::<String>().trim().to_owned();
                if key.is_empty() {
                    return Err(Error::bad_request(format!(
                        "invalid workflow path {path:?}"
                    )));
                }
                out.push(PathSegment::Key(key));
            }
        }
    }
    Ok(out)
}

fn unquote_path_key(token: &str, path: &str) -> Result<String> {
    if token.starts_with('"') {
        return serde_json::from_str(token)
            .map_err(|_| Error::bad_request(format!("invalid workflow path {path:?}")));
    }
    if token.len() < 2 || !token.ends_with('\'') {
        return Err(Error::bad_request(format!(
            "invalid workflow path {path:?}"
        )));
    }
    let mut out = String::new();
    let mut chars = token[1..token.len() - 1].chars();
    while let Some(ch) = chars.next() {
        if ch != '\\' {
            out.push(ch);
            continue;
        }
        let escaped = chars
            .next()
            .ok_or_else(|| Error::bad_request(format!("invalid workflow path {path:?}")))?;
        match escaped {
            '\'' | '"' | '\\' => out.push(escaped),
            'n' => out.push('\n'),
            'r' => out.push('\r'),
            't' => out.push('\t'),
            'u' => {
                let mut hex = String::new();
                for _ in 0..4 {
                    hex.push(chars.next().ok_or_else(|| {
                        Error::bad_request(format!("invalid workflow path {path:?}"))
                    })?);
                }
                let code = u32::from_str_radix(&hex, 16)
                    .map_err(|_| Error::bad_request(format!("invalid workflow path {path:?}")))?;
                let value = char::from_u32(code)
                    .ok_or_else(|| Error::bad_request(format!("invalid workflow path {path:?}")))?;
                out.push(value);
            }
            other => out.push(other),
        }
    }
    Ok(out)
}

#[cfg(test)]
mod tests {
    use serde_json::json;

    use crate::protocol;
    use crate::workflow_provider::{
        WorkflowEvalContext, WorkflowExecutionRequest, WorkflowSignal, evaluate_workflow_value,
        path_value, render_workflow_template, workflow_value_input,
    };

    #[test]
    fn evaluates_current_templates_and_paths() {
        let ctx = WorkflowEvalContext {
            request: WorkflowExecutionRequest {
                provider_name: "indexeddb".to_owned(),
                run_id: "run-1".to_owned(),
                input: Some(json!({"customer": {"id": "cust_1"}})),
                signals: vec![WorkflowSignal {
                    id: "sig-1".to_owned(),
                    payload: Some(
                        protocol::struct_from_json(json!({"thread": {"ts": "123.456"}})).unwrap(),
                    ),
                    ..Default::default()
                }],
                ..Default::default()
            },
            inputs: [("draft".to_owned(), json!({"title": "PR"}))].into(),
            outputs: [("diagnosis".to_owned(), json!({"action": "open_pr"}))].into(),
            allow_inputs: true,
        };

        assert_eq!(
            render_workflow_template(
                &ctx,
                "customer=${{ input.customer.id }}; thread=${{ signal.thread.ts }}; action=${{ steps.diagnosis.outputs.action }}; title=${{ steps.draft.inputs.title }}; literal=$${{x}}",
            )
            .unwrap(),
            "customer=cust_1; thread=123.456; action=open_pr; title=PR; literal=${{x}}"
        );
        assert_eq!(
            evaluate_workflow_value(&ctx, &workflow_value_input("customer.id"))
                .unwrap()
                .value,
            Some(json!("cust_1"))
        );
        assert_eq!(
            path_value(
                &json!({"quote'key": {"value": 42}}),
                "['quote\\'key'].value"
            )
            .unwrap()
            .value,
            Some(json!(42))
        );
    }
}
