#[allow(dead_code)]
mod helpers;

use std::sync::{Arc, Mutex};

use gestalt::proto::v1::provider_lifecycle_client::ProviderLifecycleClient;
use gestalt::proto::v1::workflow_client::WorkflowClient;
use gestalt::proto::v1::{
    ApplyWorkflowProviderDefinitionRequest, BoundWorkflowTarget, ConfigureProviderRequest,
    DeliverWorkflowProviderEventRequest, GetWorkflowProviderRunEventsRequest,
    GetWorkflowProviderRunOutputRequest, ProviderKind, SetWorkflowProviderActivationPausedRequest,
    SetWorkflowProviderDefinitionPausedRequest, StartWorkflowProviderRunRequest,
    WorkflowActivation, WorkflowEvent, WorkflowRunEvent, WorkflowRunStatus,
    WorkflowScheduleActivation, WorkflowStep, WorkflowStepAppCall, workflow_activation,
    workflow_step,
};
use gestalt::{
    ApplyWorkflowProviderDefinitionRequest as NativeApplyDefinitionRequest,
    DeliverWorkflowProviderEventRequest as NativeDeliverEventRequest,
    GetWorkflowProviderRunEventsRequest as NativeGetRunEventsRequest,
    GetWorkflowProviderRunOutputRequest as NativeGetRunOutputRequest, RuntimeMetadata,
    SetWorkflowProviderActivationPausedRequest as NativeSetActivationPausedRequest,
    SetWorkflowProviderDefinitionPausedRequest as NativeSetDefinitionPausedRequest,
    StartWorkflowProviderRunRequest as NativeStartRunRequest, WorkflowProvider,
};
use hyper_util::rt::tokio::TokioIo;
use tokio::net::UnixStream;
use tonic::transport::Endpoint;
use tower::service_fn;

#[derive(Default)]
struct TestWorkflowProvider {
    configured_name: Mutex<String>,
    applied_definitions: Mutex<Vec<(String, String, usize)>>,
    definition_pauses: Mutex<Vec<(String, bool)>>,
    activation_pauses: Mutex<Vec<(String, String, bool)>>,
    delivered_events: Mutex<Vec<String>>,
    started_inputs: Mutex<Vec<serde_json::Value>>,
}

#[gestalt::async_trait]
impl WorkflowProvider for TestWorkflowProvider {
    async fn configure(
        &self,
        name: &str,
        _config: serde_json::Map<String, serde_json::Value>,
    ) -> gestalt::Result<()> {
        *self.configured_name.lock().expect("configured_name lock") = name.to_string();
        Ok(())
    }

    fn metadata(&self) -> Option<RuntimeMetadata> {
        Some(RuntimeMetadata {
            name: "workflow-example".to_string(),
            display_name: "Workflow Example".to_string(),
            description: "Test workflow provider".to_string(),
            version: "0.1.0".to_string(),
        })
    }

    fn warnings(&self) -> Vec<String> {
        vec!["set TEMPORAL_ADDRESS".to_string()]
    }

    async fn apply_definition(
        &self,
        request: NativeApplyDefinitionRequest,
    ) -> gestalt::Result<gestalt::WorkflowDefinition> {
        let spec = request
            .spec
            .ok_or_else(|| gestalt::Error::bad_request("missing definition spec"))?;
        self.applied_definitions
            .lock()
            .expect("applied_definitions lock")
            .push((
                "workflow-runtime".to_string(),
                spec.id.clone(),
                spec.activations.len(),
            ));
        Ok(gestalt::WorkflowDefinition {
            id: spec.id,
            generation: 7,
            target: spec.target,
            activations: spec.activations,
            paused: spec.paused,
            provider_name: "workflow-runtime".to_string(),
            ..Default::default()
        })
    }

    async fn set_definition_paused(
        &self,
        request: NativeSetDefinitionPausedRequest,
    ) -> gestalt::Result<gestalt::WorkflowDefinition> {
        self.definition_pauses
            .lock()
            .expect("definition_pauses lock")
            .push((request.definition_id.clone(), request.paused));
        Ok(gestalt::WorkflowDefinition {
            id: request.definition_id,
            paused: request.paused,
            provider_name: "workflow-runtime".to_string(),
            ..Default::default()
        })
    }

    async fn set_activation_paused(
        &self,
        request: NativeSetActivationPausedRequest,
    ) -> gestalt::Result<gestalt::WorkflowDefinition> {
        self.activation_pauses
            .lock()
            .expect("activation_pauses lock")
            .push((
                request.definition_id.clone(),
                request.activation_id.clone(),
                request.paused,
            ));
        Ok(gestalt::WorkflowDefinition {
            id: request.definition_id,
            activations: vec![WorkflowActivation {
                id: request.activation_id,
                paused: request.paused,
                ..Default::default()
            }],
            provider_name: "workflow-runtime".to_string(),
            ..Default::default()
        })
    }

    async fn start_run(
        &self,
        request: NativeStartRunRequest,
    ) -> gestalt::Result<gestalt::WorkflowRun> {
        if request.definition_id != "definition-42" {
            return Err(gestalt::Error::bad_request("missing definition id"));
        }
        if let Some(input) = request.input.as_ref() {
            self.started_inputs
                .lock()
                .expect("started_inputs lock")
                .push(support_protocol_json(input));
        }
        Ok(gestalt::WorkflowRun {
            id: request.idempotency_key,
            status: WorkflowRunStatus::Pending as i32,
            target: Some(app_target()),
            provider_name: "workflow-runtime".to_string(),
            definition_id: request.definition_id,
            workflow_key: request.workflow_key,
            input: request.input,
            definition_generation: request.expected_definition_generation,
            current_step_id: "refresh".to_string(),
            ..Default::default()
        })
    }

    async fn get_run_events(
        &self,
        request: NativeGetRunEventsRequest,
    ) -> gestalt::Result<gestalt::GetWorkflowProviderRunEventsResponse> {
        Ok(gestalt::GetWorkflowProviderRunEventsResponse {
            events: vec![WorkflowRunEvent {
                id: "evt-1".to_string(),
                run_id: request.run_id,
                step_id: "refresh".to_string(),
                r#type: "step.succeeded".to_string(),
                ..Default::default()
            }],
        })
    }

    async fn get_run_output(
        &self,
        _request: NativeGetRunOutputRequest,
    ) -> gestalt::Result<gestalt::GetWorkflowProviderRunOutputResponse> {
        Ok(gestalt::GetWorkflowProviderRunOutputResponse {
            output: Some(helpers::json_to_prost(&serde_json::json!({"ok": true}))),
        })
    }

    async fn deliver_event(
        &self,
        request: NativeDeliverEventRequest,
    ) -> gestalt::Result<gestalt::WorkflowEvent> {
        let event = request
            .event
            .ok_or_else(|| gestalt::Error::bad_request("missing event"))?;
        self.delivered_events
            .lock()
            .expect("delivered_events lock")
            .push(event.r#type.clone());
        Ok(event)
    }
}

#[tokio::test]
async fn workflow_runtime_and_server_round_trip_over_unix_socket() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("gestalt-rust-workflow.sock");
    let _provider_socket = helpers::EnvGuard::set(gestalt::ENV_PROVIDER_SOCKET, socket.as_os_str());

    let provider = Arc::new(TestWorkflowProvider::default());
    let serve_provider = Arc::clone(&provider);
    let serve_task = tokio::spawn(async move {
        gestalt::runtime_impl::serve_workflow_provider(serve_provider)
            .await
            .expect("serve workflow provider");
    });

    helpers::wait_for_socket(&socket).await;

    let channel = Endpoint::try_from("http://[::]:50051")
        .expect("endpoint")
        .connect_with_connector(service_fn({
            let socket = socket.clone();
            move |_| {
                let socket = socket.clone();
                async move { UnixStream::connect(socket).await.map(TokioIo::new) }
            }
        }))
        .await
        .expect("connect channel");

    let mut runtime = ProviderLifecycleClient::new(channel.clone());
    let metadata = runtime
        .get_provider_identity(())
        .await
        .expect("get provider identity")
        .into_inner();
    assert_eq!(
        ProviderKind::try_from(metadata.kind)
            .expect("valid provider kind")
            .as_str_name(),
        "PROVIDER_KIND_WORKFLOW"
    );
    assert_eq!(metadata.name, "workflow-example");
    assert_eq!(metadata.warnings, vec!["set TEMPORAL_ADDRESS"]);

    runtime
        .configure_provider(ConfigureProviderRequest {
            name: "workflow-runtime".to_string(),
            config: Some(helpers::struct_from_json(serde_json::json!({}))),
            protocol_version: gestalt::CURRENT_PROTOCOL_VERSION,
        })
        .await
        .expect("configure provider");

    let mut client = WorkflowClient::new(channel);
    let applied = client
        .apply_definition(ApplyWorkflowProviderDefinitionRequest {
            spec: Some(gestalt::WorkflowDefinitionSpec {
                id: "definition-42".to_string(),
                target: Some(app_target()),
                activations: vec![WorkflowActivation {
                    id: "hourly".to_string(),
                    trigger: Some(workflow_activation::Trigger::Schedule(
                        WorkflowScheduleActivation {
                            cron: "0 * * * *".to_string(),
                            timezone: "America/New_York".to_string(),
                        },
                    )),
                    ..Default::default()
                }],
                ..Default::default()
            }),
            idempotency_key: "definition-key".to_string(),
            ..Default::default()
        })
        .await
        .expect("apply definition")
        .into_inner();
    assert_eq!(applied.id, "definition-42");
    assert_eq!(applied.generation, 7);
    assert_eq!(applied.activations.len(), 1);

    let paused = client
        .set_definition_paused(SetWorkflowProviderDefinitionPausedRequest {
            definition_id: "definition-42".to_string(),
            paused: true,
            ..Default::default()
        })
        .await
        .expect("set definition paused")
        .into_inner();
    assert!(paused.paused);

    let activation_paused = client
        .set_activation_paused(SetWorkflowProviderActivationPausedRequest {
            definition_id: "definition-42".to_string(),
            activation_id: "hourly".to_string(),
            paused: true,
            ..Default::default()
        })
        .await
        .expect("set activation paused")
        .into_inner();
    assert_eq!(activation_paused.activations[0].id, "hourly");
    assert!(activation_paused.activations[0].paused);

    let started = client
        .start_run(StartWorkflowProviderRunRequest {
            idempotency_key: "run-42".to_string(),
            definition_id: "definition-42".to_string(),
            workflow_key: "wf-key".to_string(),
            input: Some(helpers::struct_from_json(
                serde_json::json!({"priority": "high"}),
            )),
            expected_definition_generation: 7,
            ..Default::default()
        })
        .await
        .expect("start run")
        .into_inner();
    assert_eq!(started.id, "run-42");
    assert_eq!(
        WorkflowRunStatus::try_from(started.status)
            .expect("valid workflow run status")
            .as_str_name(),
        "WORKFLOW_RUN_STATUS_PENDING"
    );
    assert_eq!(started.definition_generation, 7);
    assert_eq!(started.current_step_id, "refresh");

    let events = client
        .get_run_events(GetWorkflowProviderRunEventsRequest {
            run_id: "run-42".to_string(),
            ..Default::default()
        })
        .await
        .expect("get run events")
        .into_inner();
    assert_eq!(events.events[0].step_id, "refresh");

    let output = client
        .get_run_output(GetWorkflowProviderRunOutputRequest {
            run_id: "run-42".to_string(),
            ..Default::default()
        })
        .await
        .expect("get run output")
        .into_inner();
    assert_eq!(
        output.output,
        Some(helpers::json_to_prost(&serde_json::json!({"ok": true})))
    );

    let delivered = client
        .deliver_event(DeliverWorkflowProviderEventRequest {
            event: Some(WorkflowEvent {
                id: "evt_1".to_string(),
                source: "github".to_string(),
                spec_version: "1.0".to_string(),
                r#type: "github.pull_request".to_string(),
                ..Default::default()
            }),
            ..Default::default()
        })
        .await
        .expect("deliver event")
        .into_inner();
    assert_eq!(delivered.id, "evt_1");

    assert_eq!(
        *provider
            .configured_name
            .lock()
            .expect("configured_name lock"),
        "workflow-runtime"
    );
    assert_eq!(
        provider
            .applied_definitions
            .lock()
            .expect("applied_definitions lock")
            .clone(),
        vec![(
            "workflow-runtime".to_string(),
            "definition-42".to_string(),
            1
        )]
    );
    assert_eq!(
        provider
            .definition_pauses
            .lock()
            .expect("definition_pauses lock")
            .clone(),
        vec![("definition-42".to_string(), true)]
    );
    assert_eq!(
        provider
            .activation_pauses
            .lock()
            .expect("activation_pauses lock")
            .clone(),
        vec![("definition-42".to_string(), "hourly".to_string(), true)]
    );
    assert_eq!(
        provider
            .started_inputs
            .lock()
            .expect("started_inputs lock")
            .clone(),
        vec![serde_json::json!({"priority": "high"})]
    );
    assert_eq!(
        provider
            .delivered_events
            .lock()
            .expect("delivered_events lock")
            .clone(),
        vec!["github.pull_request".to_string()]
    );

    serve_task.abort();
    let _ = serve_task.await;
}

fn app_target() -> BoundWorkflowTarget {
    BoundWorkflowTarget {
        steps: vec![WorkflowStep {
            id: "refresh".to_string(),
            action: Some(workflow_step::Action::App(WorkflowStepAppCall {
                name: "demo".to_string(),
                operation: "refresh".to_string(),
                ..Default::default()
            })),
            ..Default::default()
        }],
    }
}

fn support_protocol_json(input: &prost_types::Struct) -> serde_json::Value {
    serde_json::Value::Object(
        input
            .fields
            .iter()
            .map(|(key, value)| (key.clone(), support_protocol_value(value)))
            .collect(),
    )
}

fn support_protocol_value(input: &prost_types::Value) -> serde_json::Value {
    match input.kind.as_ref() {
        Some(prost_types::value::Kind::NullValue(_)) | None => serde_json::Value::Null,
        Some(prost_types::value::Kind::BoolValue(value)) => serde_json::Value::Bool(*value),
        Some(prost_types::value::Kind::NumberValue(value)) => serde_json::json!(*value),
        Some(prost_types::value::Kind::StringValue(value)) => {
            serde_json::Value::String(value.clone())
        }
        Some(prost_types::value::Kind::ListValue(value)) => {
            serde_json::Value::Array(value.values.iter().map(support_protocol_value).collect())
        }
        Some(prost_types::value::Kind::StructValue(value)) => support_protocol_json(value),
    }
}
