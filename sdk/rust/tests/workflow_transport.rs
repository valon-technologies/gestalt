#[path = "../src/generated.rs"]
mod generated;

#[allow(dead_code)]
mod helpers;

use std::sync::{Arc, Mutex};

use generated::v1::provider_lifecycle_client::ProviderLifecycleClient;
use generated::v1::workflow_host_server::{
    WorkflowHost as WorkflowHostRpc, WorkflowHostServer as WorkflowHostGrpcServer,
};
use generated::v1::workflow_provider_client::WorkflowProviderClient;
use generated::v1::{
    self as pb, BoundWorkflowPluginTarget as ProtoBoundWorkflowPluginTarget,
    BoundWorkflowTarget as ProtoBoundWorkflowTarget, ConfigureProviderRequest,
    ListWorkflowProviderRunsRequest as ProtoListWorkflowProviderRunsRequest, ProviderKind,
    PublishWorkflowProviderEventRequest as ProtoPublishWorkflowProviderEventRequest,
    StartWorkflowProviderRunRequest as ProtoStartWorkflowProviderRunRequest,
    UpsertWorkflowProviderEventTriggerRequest as ProtoUpsertWorkflowProviderEventTriggerRequest,
    UpsertWorkflowProviderScheduleRequest as ProtoUpsertWorkflowProviderScheduleRequest,
    WorkflowEvent as ProtoWorkflowEvent, WorkflowRunStatus as ProtoWorkflowRunStatus,
    bound_workflow_target,
};
use gestalt::{
    BoundWorkflowEventTrigger, BoundWorkflowPluginTarget, BoundWorkflowRun, BoundWorkflowSchedule,
    BoundWorkflowTarget, InvokeWorkflowOperationInput, ListWorkflowProviderRunsRequest,
    ListWorkflowProviderRunsResponse, PublishWorkflowProviderEventRequest, RuntimeMetadata,
    StartWorkflowProviderRunRequest, UpsertWorkflowProviderEventTriggerRequest,
    UpsertWorkflowProviderScheduleRequest, WorkflowHost, WorkflowProvider, WorkflowRunStatus,
};
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
    published_events: Mutex<Vec<(String, String)>>,
    list_run_target_plugins: Mutex<Vec<String>>,
    schedule_bindings: Mutex<Vec<(String, String)>>,
    trigger_bindings: Mutex<Vec<(String, String)>>,
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

    async fn start_run(
        &self,
        request: StartWorkflowProviderRunRequest,
    ) -> gestalt::Result<BoundWorkflowRun> {
        if request.definition_id != "definition-42" {
            return Err(gestalt::Error::bad_request("missing definition id"));
        }
        let target = request
            .target
            .ok_or_else(|| gestalt::Error::bad_request("missing target"))?;
        Ok(BoundWorkflowRun {
            id: request.idempotency_key,
            status: WorkflowRunStatus::Pending,
            target: Some(target),
            ..Default::default()
        })
    }

    async fn list_runs(
        &self,
        request: ListWorkflowProviderRunsRequest,
    ) -> gestalt::Result<ListWorkflowProviderRunsResponse> {
        self.list_run_target_plugins
            .lock()
            .expect("list_run_target_plugins lock")
            .push(request.target_plugin);
        Ok(ListWorkflowProviderRunsResponse::default())
    }

    async fn upsert_schedule(
        &self,
        request: UpsertWorkflowProviderScheduleRequest,
    ) -> gestalt::Result<BoundWorkflowSchedule> {
        self.schedule_bindings
            .lock()
            .expect("schedule_bindings lock")
            .push((
                request.idempotency_key.clone(),
                request.definition_id.clone(),
            ));
        Ok(BoundWorkflowSchedule {
            id: request.schedule_id,
            ..Default::default()
        })
    }

    async fn upsert_event_trigger(
        &self,
        request: UpsertWorkflowProviderEventTriggerRequest,
    ) -> gestalt::Result<BoundWorkflowEventTrigger> {
        self.trigger_bindings
            .lock()
            .expect("trigger_bindings lock")
            .push((
                request.idempotency_key.clone(),
                request.definition_id.clone(),
            ));
        Ok(BoundWorkflowEventTrigger {
            id: request.trigger_id,
            ..Default::default()
        })
    }

    async fn publish_event(
        &self,
        request: PublishWorkflowProviderEventRequest,
    ) -> gestalt::Result<gestalt::WorkflowEvent> {
        let event = request
            .event
            .ok_or_else(|| gestalt::Error::bad_request("missing event"))?;
        self.published_events
            .lock()
            .expect("published_events lock")
            .push((request.plugin_name, event.event_type.clone()));
        Ok(event)
    }
}

#[tonic::async_trait]
impl WorkflowHostRpc for TestWorkflowHostService {
    async fn invoke_operation(
        &self,
        request: GrpcRequest<pb::InvokeWorkflowOperationRequest>,
    ) -> std::result::Result<GrpcResponse<pb::InvokeWorkflowOperationResponse>, Status> {
        if let Some(token) = request.metadata().get("x-gestalt-host-service-relay-token") {
            self.relay_tokens
                .lock()
                .expect("lock relay tokens")
                .push(token.to_str().expect("relay token ascii").to_string());
        }
        let request = request.into_inner();
        let target = request
            .target
            .ok_or_else(|| Status::invalid_argument("missing target"))?;
        let plugin = match target.kind {
            Some(bound_workflow_target::Kind::Plugin(plugin)) => plugin,
            _ => return Err(Status::invalid_argument("missing target.plugin")),
        };
        Ok(GrpcResponse::new(pb::InvokeWorkflowOperationResponse {
            status: 202,
            body: format!("{}:{}", request.run_id, plugin.operation),
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
    let started = client
        .start_run(ProtoStartWorkflowProviderRunRequest {
            target: Some(ProtoBoundWorkflowTarget {
                kind: Some(bound_workflow_target::Kind::Plugin(
                    ProtoBoundWorkflowPluginTarget {
                        plugin_name: "demo".to_string(),
                        operation: "refresh".to_string(),
                        input: Some(helpers::struct_from_json(serde_json::json!({
                            "customer_id": "cust_123"
                        }))),
                        ..Default::default()
                    },
                )),
            }),
            idempotency_key: "run-42".to_string(),
            definition_id: "definition-42".to_string(),
            ..Default::default()
        })
        .await
        .expect("start run")
        .into_inner();
    assert_eq!(started.id, "run-42");
    assert_eq!(
        ProtoWorkflowRunStatus::try_from(started.status)
            .expect("valid workflow run status")
            .as_str_name(),
        "WORKFLOW_RUN_STATUS_PENDING"
    );
    assert_eq!(
        started.target.expect("target"),
        ProtoBoundWorkflowTarget {
            kind: Some(bound_workflow_target::Kind::Plugin(
                ProtoBoundWorkflowPluginTarget {
                    plugin_name: "demo".to_string(),
                    operation: "refresh".to_string(),
                    input: Some(helpers::struct_from_json(serde_json::json!({
                        "customer_id": "cust_123"
                    }))),
                    ..Default::default()
                }
            )),
        }
    );

    client
        .list_runs(ProtoListWorkflowProviderRunsRequest {
            target_plugin: "demo".to_string(),
            ..Default::default()
        })
        .await
        .expect("list runs");

    client
        .upsert_schedule(ProtoUpsertWorkflowProviderScheduleRequest {
            schedule_id: "schedule-1".to_string(),
            idempotency_key: "schedule-key".to_string(),
            definition_id: "definition-42".to_string(),
            ..Default::default()
        })
        .await
        .expect("upsert schedule");

    client
        .upsert_event_trigger(ProtoUpsertWorkflowProviderEventTriggerRequest {
            trigger_id: "trigger-1".to_string(),
            idempotency_key: "trigger-key".to_string(),
            definition_id: "definition-42".to_string(),
            ..Default::default()
        })
        .await
        .expect("upsert event trigger");

    let published = client
        .publish_event(ProtoPublishWorkflowProviderEventRequest {
            plugin_name: "demo".to_string(),
            event: Some(ProtoWorkflowEvent {
                id: "evt_1".to_string(),
                source: "urn:test".to_string(),
                spec_version: "1.0".to_string(),
                r#type: "demo.refresh.requested".to_string(),
                ..Default::default()
            }),
            published_by: None,
            invocation_token: String::new(),
            provider_name: String::new(),
        })
        .await
        .expect("publish event")
        .into_inner();
    assert_eq!(published.id, "evt_1");

    assert_eq!(
        *provider
            .configured_name
            .lock()
            .expect("configured_name lock"),
        "workflow-runtime"
    );
    assert_eq!(
        provider
            .published_events
            .lock()
            .expect("published_events lock")
            .clone(),
        vec![("demo".to_string(), "demo.refresh.requested".to_string())]
    );
    assert_eq!(
        provider
            .list_run_target_plugins
            .lock()
            .expect("list_run_target_plugins lock")
            .clone(),
        vec!["demo".to_string()]
    );
    assert_eq!(
        provider
            .schedule_bindings
            .lock()
            .expect("schedule_bindings lock")
            .clone(),
        vec![("schedule-key".to_string(), "definition-42".to_string())]
    );
    assert_eq!(
        provider
            .trigger_bindings
            .lock()
            .expect("trigger_bindings lock")
            .clone(),
        vec![("trigger-key".to_string(), "definition-42".to_string())]
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
        .invoke_operation(InvokeWorkflowOperationInput {
            target: Some(BoundWorkflowTarget::Plugin(BoundWorkflowPluginTarget {
                plugin_name: "demo".to_string(),
                operation: "sync".to_string(),
                ..Default::default()
            })),
            run_id: "run-42".to_string(),
            ..Default::default()
        })
        .await
        .expect("invoke operation");
    assert_eq!(invoked.status, 202);
    assert_eq!(invoked.body, "run-42:sync");

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
        .invoke_operation(InvokeWorkflowOperationInput {
            target: Some(BoundWorkflowTarget::Plugin(BoundWorkflowPluginTarget {
                plugin_name: "demo".to_string(),
                operation: "sync".to_string(),
                ..Default::default()
            })),
            run_id: "run-42".to_string(),
            ..Default::default()
        })
        .await
        .expect("invoke operation");
    assert_eq!(invoked.status, 202);
    assert_eq!(invoked.body, "run-42:sync");
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
