#[allow(dead_code)]
mod helpers;

use std::sync::{Arc, Mutex};

use gestalt::proto::v1::provider_lifecycle_client::ProviderLifecycleClient;
use gestalt::proto::v1::workflow_host_server::{
    WorkflowHost as WorkflowHostRpc, WorkflowHostServer as WorkflowHostGrpcServer,
};
use gestalt::proto::v1::workflow_provider_client::WorkflowProviderClient;
use gestalt::proto::v1::{
    self as pb, ApplyWorkflowDefinitionRequest, BoundWorkflowTarget, ConfigureProviderRequest,
    DeliverWorkflowEventRequest, InvokeWorkflowActionRequest, ProviderKind,
    StartWorkflowRunRequest, WorkflowActionResult, WorkflowActivation, WorkflowActivationMode,
    WorkflowDefinition, WorkflowDefinitionSpec, WorkflowDefinitionStatus, WorkflowEvent,
    WorkflowEventDeliveryResult, WorkflowHostActionSelector, WorkflowPluginActionPayload,
    WorkflowRun, WorkflowRunStatus, WorkflowStep, WorkflowStepPluginCall,
    invoke_workflow_action_request, workflow_step,
};
use gestalt::{RuntimeMetadata, WorkflowHost, WorkflowProvider, workflow_json_from_struct};
use hyper_util::rt::tokio::TokioIo;
use tokio::net::{TcpListener, UnixListener, UnixStream};
use tokio_stream::wrappers::{TcpListenerStream, UnixListenerStream};
use tonic::transport::{Endpoint, Server};
use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};
use tower::service_fn;

const ENV_WORKFLOW_HOST_SOCKET_TOKEN: &str = "GESTALT_WORKFLOW_HOST_SOCKET_TOKEN";

#[derive(Default)]
struct TestWorkflowProvider {
    configured_name: Mutex<String>,
    delivered_events: Mutex<Vec<String>>,
}

#[derive(Default, Clone)]
struct TestWorkflowHostService {
    relay_tokens: Arc<Mutex<Vec<String>>>,
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

    async fn apply_workflow_definition(
        &self,
        request: ApplyWorkflowDefinitionRequest,
    ) -> gestalt::Result<WorkflowDefinition> {
        Ok(WorkflowDefinition {
            spec: request.spec,
            status: WorkflowDefinitionStatus::Active as i32,
            provider_plan_id: "apply-local-1".to_string(),
            ..Default::default()
        })
    }

    async fn start_workflow_run(
        &self,
        request: StartWorkflowRunRequest,
    ) -> gestalt::Result<WorkflowRun> {
        Ok(WorkflowRun {
            id: request.idempotency_key,
            definition_id: request.definition_id,
            definition_generation: request.definition_generation,
            workflow_key: request.workflow_key,
            input: request.input,
            status: WorkflowRunStatus::Pending as i32,
            ..Default::default()
        })
    }

    async fn deliver_workflow_event(
        &self,
        request: DeliverWorkflowEventRequest,
    ) -> gestalt::Result<pb::DeliverWorkflowEventResponse> {
        let event_type = request
            .event
            .as_ref()
            .map(|event| event.r#type.clone())
            .unwrap_or_default();
        self.delivered_events
            .lock()
            .expect("delivered_events lock")
            .push(event_type);
        Ok(pb::DeliverWorkflowEventResponse {
            results: vec![WorkflowEventDeliveryResult {
                definition_id: "deployment-1".to_string(),
                activation_id: "evt".to_string(),
                started_run: true,
                ..Default::default()
            }],
        })
    }
}

#[tonic::async_trait]
impl WorkflowHostRpc for TestWorkflowHostService {
    async fn invoke_workflow_action(
        &self,
        request: GrpcRequest<InvokeWorkflowActionRequest>,
    ) -> std::result::Result<GrpcResponse<WorkflowActionResult>, Status> {
        if let Some(token) = request.metadata().get("x-gestalt-host-service-relay-token") {
            self.relay_tokens
                .lock()
                .expect("lock relay tokens")
                .push(token.to_str().expect("relay token ascii").to_string());
        }
        let request = request.into_inner();
        let selector = request
            .selector
            .ok_or_else(|| Status::invalid_argument("missing selector"))?;
        let plugin = match request.action {
            Some(invoke_workflow_action_request::Action::Plugin(plugin)) => plugin,
            _ => return Err(Status::invalid_argument("missing plugin action")),
        };
        let customer_id = plugin
            .input
            .as_ref()
            .and_then(|input| workflow_json_from_struct(input).get("customer_id").cloned())
            .unwrap_or_default();
        Ok(GrpcResponse::new(WorkflowActionResult {
            status: 202,
            body: format!("{}:{customer_id}", selector.run_id),
            ..Default::default()
        }))
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
        gestalt::runtime::serve_workflow_provider(serve_provider)
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

    let mut client = WorkflowProviderClient::new(channel);
    let target = BoundWorkflowTarget::from_steps([WorkflowStep {
        id: "refresh".to_string(),
        action: Some(workflow_step::Action::Plugin(WorkflowStepPluginCall {
            name: "demo".to_string(),
            operation: "refresh".to_string(),
            ..Default::default()
        })),
        ..Default::default()
    }]);
    let spec = WorkflowDefinitionSpec {
        id: "deployment-1".to_string(),
        generation: 7,
        target: Some(target.clone()),
        activations: vec![WorkflowActivation {
            id: "manual".to_string(),
            mode: WorkflowActivationMode::Start as i32,
            ..Default::default()
        }],
        workflow_semantics_version: "workflow.steps.v1".to_string(),
        ..Default::default()
    };
    let applied = client
        .apply_workflow_definition(ApplyWorkflowDefinitionRequest {
            spec: Some(spec),
            request_id: "apply-1".to_string(),
            ..Default::default()
        })
        .await
        .expect("apply deployment")
        .into_inner();
    assert_eq!(
        WorkflowDefinitionStatus::try_from(applied.status)
            .expect("deployment status")
            .as_str_name(),
        "WORKFLOW_DEFINITION_STATUS_ACTIVE"
    );

    let started = client
        .start_workflow_run(
            StartWorkflowRunRequest {
                definition_id: "deployment-1".to_string(),
                definition_generation: 7,
                activation_id: "manual".to_string(),
                workflow_key: "workflow-key".to_string(),
                idempotency_key: "run-1".to_string(),
                ..Default::default()
            }
            .with_input(serde_json::json!({ "customer_id": "cust_123" }))
            .expect("input"),
        )
        .await
        .expect("start run")
        .into_inner();
    assert_eq!(started.id, "run-1");
    assert_eq!(started.workflow_key, "workflow-key");

    let delivered = client
        .deliver_workflow_event(DeliverWorkflowEventRequest {
            delivery_id: "delivery-1".to_string(),
            event: Some(WorkflowEvent {
                id: "evt_1".to_string(),
                source: "urn:test".to_string(),
                spec_version: "1.0".to_string(),
                r#type: "demo.refresh.requested".to_string(),
                ..Default::default()
            }),
            ..Default::default()
        })
        .await
        .expect("deliver event")
        .into_inner();
    assert_eq!(delivered.results[0].definition_id, "deployment-1");

    assert_eq!(
        *provider
            .configured_name
            .lock()
            .expect("configured_name lock"),
        "workflow-runtime"
    );
    assert_eq!(
        provider
            .delivered_events
            .lock()
            .expect("delivered_events lock")
            .clone(),
        vec!["demo.refresh.requested".to_string()]
    );

    serve_task.abort();
    let _ = serve_task.await;
}

#[tokio::test]
async fn workflow_host_client_round_trip_over_unix_socket() {
    let _env_lock = helpers::env_lock().lock().await;
    let host_socket = helpers::temp_socket("gestalt-rust-workflow-host.sock");
    let _workflow_host_env =
        helpers::EnvGuard::set(gestalt::ENV_WORKFLOW_HOST_SOCKET, host_socket.as_os_str());
    let host_service = TestWorkflowHostService::default();

    let host_socket_for_task = host_socket.clone();
    let host_task = tokio::spawn(async move {
        let listener =
            UnixListener::bind(&host_socket_for_task).expect("bind workflow host socket");
        Server::builder()
            .add_service(WorkflowHostGrpcServer::new(host_service))
            .serve_with_incoming(UnixListenerStream::new(listener))
            .await
            .expect("serve workflow host");
    });

    helpers::wait_for_socket(&host_socket).await;

    let mut host = WorkflowHost::connect()
        .await
        .expect("connect workflow host");
    let invoked = host
        .invoke_action(InvokeWorkflowActionRequest {
            selector: Some(WorkflowHostActionSelector {
                run_id: "run-42".to_string(),
                ..Default::default()
            }),
            action: Some(invoke_workflow_action_request::Action::Plugin(
                WorkflowPluginActionPayload::default()
                    .with_input(serde_json::json!({ "customer_id": "cust_123" }))
                    .expect("plugin input"),
            )),
            ..Default::default()
        })
        .await
        .expect("invoke action");
    assert_eq!(invoked.status, 202);
    assert_eq!(invoked.body, "run-42:\"cust_123\"");

    host_task.abort();
    let _ = host_task.await;
}

#[tokio::test]
async fn workflow_host_client_round_trip_over_tcp_and_sends_relay_token() {
    let _env_lock = helpers::env_lock().lock().await;
    let listener = TcpListener::bind("127.0.0.1:0")
        .await
        .expect("bind tcp listener");
    let address = listener.local_addr().expect("local addr");
    let _workflow_host_env = helpers::EnvGuard::set(
        gestalt::ENV_WORKFLOW_HOST_SOCKET,
        format!("tcp://{address}"),
    );
    let _token_guard = helpers::EnvGuard::set(ENV_WORKFLOW_HOST_SOCKET_TOKEN, "relay-token-rust");

    let host_service = TestWorkflowHostService::default();
    let served_service = host_service.clone();
    let host_task = tokio::spawn(async move {
        Server::builder()
            .add_service(WorkflowHostGrpcServer::new(served_service))
            .serve_with_incoming(TcpListenerStream::new(listener))
            .await
            .expect("serve workflow host");
    });

    let mut host = WorkflowHost::connect()
        .await
        .expect("connect workflow host");
    let invoked = host
        .invoke_action(InvokeWorkflowActionRequest {
            selector: Some(WorkflowHostActionSelector {
                run_id: "run-42".to_string(),
                ..Default::default()
            }),
            action: Some(invoke_workflow_action_request::Action::Plugin(
                WorkflowPluginActionPayload::default()
                    .with_input(serde_json::json!({ "customer_id": "cust_123" }))
                    .expect("plugin input"),
            )),
            ..Default::default()
        })
        .await
        .expect("invoke action");
    assert_eq!(invoked.status, 202);
    assert_eq!(invoked.body, "run-42:\"cust_123\"");
    assert_eq!(
        host_service
            .relay_tokens
            .lock()
            .expect("lock relay tokens")
            .clone(),
        vec!["relay-token-rust".to_string()]
    );

    host_task.abort();
    let _ = host_task.await;
}
