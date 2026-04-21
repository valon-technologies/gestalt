#[allow(dead_code)]
mod helpers;

use std::sync::{Arc, Mutex};

use gestalt::proto::v1::provider_lifecycle_client::ProviderLifecycleClient;
use gestalt::proto::v1::workflow_host_server::{
    WorkflowHost as WorkflowHostRpc, WorkflowHostServer as WorkflowHostGrpcServer,
};
use gestalt::proto::v1::workflow_provider_client::WorkflowProviderClient;
use gestalt::proto::v1::workflow_provider_server::WorkflowProvider as WorkflowProviderGrpc;
use gestalt::proto::v1::{
    self as pb, BoundWorkflowRun, BoundWorkflowTarget, ConfigureProviderRequest, ProviderKind,
    PublishWorkflowProviderEventRequest, StartWorkflowProviderRunRequest, WorkflowEvent,
    WorkflowRunStatus,
};
use gestalt::{RuntimeMetadata, WorkflowHost, WorkflowProvider};
use hyper_util::rt::tokio::TokioIo;
use tokio::net::UnixListener;
use tokio::net::UnixStream;
use tokio_stream::wrappers::UnixListenerStream;
use tonic::transport::{Endpoint, Server};
use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};
use tower::service_fn;

#[derive(Default)]
struct TestWorkflowProvider {
    configured_name: Mutex<String>,
    published_events: Mutex<Vec<(String, String)>>,
}

#[derive(Default, Clone)]
struct TestWorkflowHostService;

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
}

#[tonic::async_trait]
impl WorkflowProviderGrpc for TestWorkflowProvider {
    async fn start_run(
        &self,
        request: GrpcRequest<StartWorkflowProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<BoundWorkflowRun>, Status> {
        let request = request.into_inner();
        let target = request
            .target
            .ok_or_else(|| Status::invalid_argument("missing target"))?;
        Ok(GrpcResponse::new(BoundWorkflowRun {
            id: request.idempotency_key,
            status: WorkflowRunStatus::Pending as i32,
            target: Some(target),
            ..Default::default()
        }))
    }

    async fn get_run(
        &self,
        _request: GrpcRequest<gestalt::proto::v1::GetWorkflowProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<BoundWorkflowRun>, Status> {
        Err(Status::unimplemented("not used"))
    }

    async fn list_runs(
        &self,
        _request: GrpcRequest<gestalt::proto::v1::ListWorkflowProviderRunsRequest>,
    ) -> std::result::Result<
        GrpcResponse<gestalt::proto::v1::ListWorkflowProviderRunsResponse>,
        Status,
    > {
        Err(Status::unimplemented("not used"))
    }

    async fn cancel_run(
        &self,
        _request: GrpcRequest<gestalt::proto::v1::CancelWorkflowProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<BoundWorkflowRun>, Status> {
        Err(Status::unimplemented("not used"))
    }

    async fn upsert_schedule(
        &self,
        _request: GrpcRequest<gestalt::proto::v1::UpsertWorkflowProviderScheduleRequest>,
    ) -> std::result::Result<GrpcResponse<gestalt::proto::v1::BoundWorkflowSchedule>, Status> {
        Err(Status::unimplemented("not used"))
    }

    async fn get_schedule(
        &self,
        _request: GrpcRequest<gestalt::proto::v1::GetWorkflowProviderScheduleRequest>,
    ) -> std::result::Result<GrpcResponse<gestalt::proto::v1::BoundWorkflowSchedule>, Status> {
        Err(Status::unimplemented("not used"))
    }

    async fn list_schedules(
        &self,
        _request: GrpcRequest<gestalt::proto::v1::ListWorkflowProviderSchedulesRequest>,
    ) -> std::result::Result<
        GrpcResponse<gestalt::proto::v1::ListWorkflowProviderSchedulesResponse>,
        Status,
    > {
        Err(Status::unimplemented("not used"))
    }

    async fn delete_schedule(
        &self,
        _request: GrpcRequest<gestalt::proto::v1::DeleteWorkflowProviderScheduleRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        Err(Status::unimplemented("not used"))
    }

    async fn pause_schedule(
        &self,
        _request: GrpcRequest<gestalt::proto::v1::PauseWorkflowProviderScheduleRequest>,
    ) -> std::result::Result<GrpcResponse<gestalt::proto::v1::BoundWorkflowSchedule>, Status> {
        Err(Status::unimplemented("not used"))
    }

    async fn resume_schedule(
        &self,
        _request: GrpcRequest<gestalt::proto::v1::ResumeWorkflowProviderScheduleRequest>,
    ) -> std::result::Result<GrpcResponse<gestalt::proto::v1::BoundWorkflowSchedule>, Status> {
        Err(Status::unimplemented("not used"))
    }

    async fn upsert_event_trigger(
        &self,
        _request: GrpcRequest<gestalt::proto::v1::UpsertWorkflowProviderEventTriggerRequest>,
    ) -> std::result::Result<GrpcResponse<gestalt::proto::v1::BoundWorkflowEventTrigger>, Status>
    {
        Err(Status::unimplemented("not used"))
    }

    async fn get_event_trigger(
        &self,
        _request: GrpcRequest<gestalt::proto::v1::GetWorkflowProviderEventTriggerRequest>,
    ) -> std::result::Result<GrpcResponse<gestalt::proto::v1::BoundWorkflowEventTrigger>, Status>
    {
        Err(Status::unimplemented("not used"))
    }

    async fn list_event_triggers(
        &self,
        _request: GrpcRequest<gestalt::proto::v1::ListWorkflowProviderEventTriggersRequest>,
    ) -> std::result::Result<
        GrpcResponse<gestalt::proto::v1::ListWorkflowProviderEventTriggersResponse>,
        Status,
    > {
        Err(Status::unimplemented("not used"))
    }

    async fn delete_event_trigger(
        &self,
        _request: GrpcRequest<gestalt::proto::v1::DeleteWorkflowProviderEventTriggerRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        Err(Status::unimplemented("not used"))
    }

    async fn pause_event_trigger(
        &self,
        _request: GrpcRequest<gestalt::proto::v1::PauseWorkflowProviderEventTriggerRequest>,
    ) -> std::result::Result<GrpcResponse<gestalt::proto::v1::BoundWorkflowEventTrigger>, Status>
    {
        Err(Status::unimplemented("not used"))
    }

    async fn resume_event_trigger(
        &self,
        _request: GrpcRequest<gestalt::proto::v1::ResumeWorkflowProviderEventTriggerRequest>,
    ) -> std::result::Result<GrpcResponse<gestalt::proto::v1::BoundWorkflowEventTrigger>, Status>
    {
        Err(Status::unimplemented("not used"))
    }

    async fn publish_event(
        &self,
        request: GrpcRequest<PublishWorkflowProviderEventRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        let request = request.into_inner();
        let event = request
            .event
            .ok_or_else(|| Status::invalid_argument("missing event"))?;
        self.published_events
            .lock()
            .expect("published_events lock")
            .push((request.plugin_name, event.r#type));
        Ok(GrpcResponse::new(()))
    }
}

#[tonic::async_trait]
impl WorkflowHostRpc for TestWorkflowHostService {
    async fn invoke_operation(
        &self,
        request: GrpcRequest<pb::InvokeWorkflowOperationRequest>,
    ) -> std::result::Result<GrpcResponse<pb::InvokeWorkflowOperationResponse>, Status> {
        let request = request.into_inner();
        let target = request
            .target
            .ok_or_else(|| Status::invalid_argument("missing target"))?;
        Ok(GrpcResponse::new(pb::InvokeWorkflowOperationResponse {
            status: 202,
            body: format!("{}:{}", request.run_id, target.operation),
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
        .start_run(StartWorkflowProviderRunRequest {
            target: Some(BoundWorkflowTarget {
                plugin_name: "demo".to_string(),
                operation: "refresh".to_string(),
                input: Some(helpers::struct_from_json(serde_json::json!({
                    "customer_id": "cust_123"
                }))),
                connection: String::new(),
                instance: String::new(),
            }),
            idempotency_key: "run-42".to_string(),
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
    assert_eq!(
        started.target.expect("target"),
        BoundWorkflowTarget {
            plugin_name: "demo".to_string(),
            operation: "refresh".to_string(),
            input: Some(helpers::struct_from_json(serde_json::json!({
                "customer_id": "cust_123"
            }))),
            connection: String::new(),
            instance: String::new(),
        }
    );

    client
        .publish_event(PublishWorkflowProviderEventRequest {
            plugin_name: "demo".to_string(),
            event: Some(WorkflowEvent {
                id: "evt_1".to_string(),
                source: "urn:test".to_string(),
                spec_version: "1.0".to_string(),
                r#type: "demo.refresh.requested".to_string(),
                ..Default::default()
            }),
        })
        .await
        .expect("publish event");

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

    serve_task.abort();
    let _ = serve_task.await;
}

#[tokio::test]
async fn workflow_host_client_round_trip_over_unix_socket() {
    let _env_lock = helpers::env_lock().lock().await;
    let host_socket = helpers::temp_socket("gestalt-rust-workflow-host.sock");
    let _workflow_host_env =
        helpers::EnvGuard::set(gestalt::ENV_WORKFLOW_HOST_SOCKET, host_socket.as_os_str());

    let host_socket_for_task = host_socket.clone();
    let host_task = tokio::spawn(async move {
        let listener =
            UnixListener::bind(&host_socket_for_task).expect("bind workflow host socket");
        Server::builder()
            .add_service(WorkflowHostGrpcServer::new(TestWorkflowHostService))
            .serve_with_incoming(UnixListenerStream::new(listener))
            .await
            .expect("serve workflow host");
    });

    helpers::wait_for_socket(&host_socket).await;

    let mut host = WorkflowHost::connect()
        .await
        .expect("connect workflow host");
    let invoked = host
        .invoke_operation(pb::InvokeWorkflowOperationRequest {
            target: Some(pb::BoundWorkflowTarget {
                plugin_name: "demo".to_string(),
                operation: "sync".to_string(),
                ..Default::default()
            }),
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
