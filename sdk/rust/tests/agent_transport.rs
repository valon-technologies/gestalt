#[allow(dead_code)]
mod helpers;

use std::sync::{Arc, Mutex};

use gestalt::proto::v1::agent_host_server::{
    AgentHost as AgentHostRpc, AgentHostServer as AgentHostGrpcServer,
};
use gestalt::proto::v1::agent_provider_client::AgentProviderClient;
use gestalt::proto::v1::agent_provider_server::AgentProvider as AgentProviderGrpc;
use gestalt::proto::v1::provider_lifecycle_client::ProviderLifecycleClient;
use gestalt::proto::v1::{
    self as pb, AgentMessage, AgentRunStatus, BoundAgentRun, ConfigureProviderRequest,
    ProviderKind, StartAgentProviderRunRequest,
};
use gestalt::{AgentHost, AgentProvider, RuntimeMetadata};
use hyper_util::rt::tokio::TokioIo;
use tokio::net::UnixListener;
use tokio::net::UnixStream;
use tokio_stream::wrappers::UnixListenerStream;
use tonic::transport::{Endpoint, Server};
use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};
use tower::service_fn;

#[derive(Default)]
struct TestAgentProvider {
    configured_name: Mutex<String>,
}

#[derive(Default, Clone)]
struct TestAgentHostService {
    events: Arc<Mutex<Vec<String>>>,
}

#[gestalt::async_trait]
impl AgentProvider for TestAgentProvider {
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
            name: "agent-example".to_string(),
            display_name: "Agent Example".to_string(),
            description: "Test agent provider".to_string(),
            version: "0.1.0".to_string(),
        })
    }

    fn warnings(&self) -> Vec<String> {
        vec!["set OPENAI_API_KEY".to_string()]
    }
}

#[tonic::async_trait]
impl AgentProviderGrpc for TestAgentProvider {
    async fn start_run(
        &self,
        request: GrpcRequest<StartAgentProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<BoundAgentRun>, Status> {
        let request = request.into_inner();
        Ok(GrpcResponse::new(BoundAgentRun {
            id: if request.run_id.is_empty() {
                request.idempotency_key
            } else {
                request.run_id
            },
            provider_name: request.provider_name,
            model: request.model,
            status: AgentRunStatus::Pending as i32,
            messages: request.messages,
            session_ref: request.session_ref,
            execution_ref: request.execution_ref,
            ..Default::default()
        }))
    }

    async fn get_run(
        &self,
        _request: GrpcRequest<pb::GetAgentProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<BoundAgentRun>, Status> {
        Err(Status::unimplemented("not used"))
    }

    async fn list_runs(
        &self,
        _request: GrpcRequest<pb::ListAgentProviderRunsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListAgentProviderRunsResponse>, Status> {
        Err(Status::unimplemented("not used"))
    }

    async fn cancel_run(
        &self,
        _request: GrpcRequest<pb::CancelAgentProviderRunRequest>,
    ) -> std::result::Result<GrpcResponse<BoundAgentRun>, Status> {
        Err(Status::unimplemented("not used"))
    }
}

#[tonic::async_trait]
impl AgentHostRpc for TestAgentHostService {
    async fn execute_tool(
        &self,
        request: GrpcRequest<pb::ExecuteAgentToolRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ExecuteAgentToolResponse>, Status> {
        let request = request.into_inner();
        Ok(GrpcResponse::new(pb::ExecuteAgentToolResponse {
            status: 207,
            body: format!(
                "{}:{}:{}",
                request.run_id, request.tool_call_id, request.tool_id
            ),
        }))
    }

    async fn emit_event(
        &self,
        request: GrpcRequest<pb::EmitAgentEventRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        let request = request.into_inner();
        self.events.lock().expect("events lock").push(format!(
            "{}:{}:{}",
            request.run_id, request.r#type, request.visibility
        ));
        Ok(GrpcResponse::new(()))
    }
}

#[tokio::test]
async fn agent_runtime_and_server_round_trip_over_unix_socket() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("gestalt-rust-agent.sock");
    let _provider_socket = helpers::EnvGuard::set(gestalt::ENV_PROVIDER_SOCKET, socket.as_os_str());

    let provider = Arc::new(TestAgentProvider::default());
    let serve_provider = Arc::clone(&provider);
    let serve_task = tokio::spawn(async move {
        gestalt::runtime::serve_agent_provider(serve_provider)
            .await
            .expect("serve agent provider");
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
        "PROVIDER_KIND_AGENT"
    );
    assert_eq!(metadata.name, "agent-example");
    assert_eq!(metadata.warnings, vec!["set OPENAI_API_KEY"]);

    runtime
        .configure_provider(ConfigureProviderRequest {
            name: "agent-runtime".to_string(),
            config: Some(helpers::struct_from_json(serde_json::json!({}))),
            protocol_version: gestalt::CURRENT_PROTOCOL_VERSION,
        })
        .await
        .expect("configure provider");

    let mut client = AgentProviderClient::new(channel);
    let started = client
        .start_run(StartAgentProviderRunRequest {
            run_id: "run-42".to_string(),
            idempotency_key: "idem-42".to_string(),
            provider_name: "openai".to_string(),
            model: "gpt-5.1".to_string(),
            messages: vec![AgentMessage {
                role: "user".to_string(),
                text: "Plan it".to_string(),
            }],
            session_ref: "sess-1".to_string(),
            execution_ref: "exec-1".to_string(),
            ..Default::default()
        })
        .await
        .expect("start run")
        .into_inner();

    assert_eq!(started.id, "run-42");
    assert_eq!(started.provider_name, "openai");
    assert_eq!(started.model, "gpt-5.1");
    assert_eq!(
        AgentRunStatus::try_from(started.status)
            .expect("valid agent run status")
            .as_str_name(),
        "AGENT_RUN_STATUS_PENDING"
    );
    assert_eq!(
        started.messages,
        vec![AgentMessage {
            role: "user".to_string(),
            text: "Plan it".to_string(),
        }]
    );
    assert_eq!(started.session_ref, "sess-1");
    assert_eq!(started.execution_ref, "exec-1");

    assert_eq!(
        *provider
            .configured_name
            .lock()
            .expect("configured_name lock"),
        "agent-runtime"
    );

    serve_task.abort();
    let _ = serve_task.await;
}

#[tokio::test]
async fn agent_host_client_round_trip_over_unix_socket() {
    let _env_lock = helpers::env_lock().lock().await;
    let host_socket = helpers::temp_socket("gestalt-rust-agent-host.sock");
    let _agent_host_env =
        helpers::EnvGuard::set(gestalt::ENV_AGENT_HOST_SOCKET, host_socket.as_os_str());
    let host_service = TestAgentHostService::default();
    let events = Arc::clone(&host_service.events);

    let host_socket_for_task = host_socket.clone();
    let host_task = tokio::spawn(async move {
        let listener = UnixListener::bind(&host_socket_for_task).expect("bind agent host socket");
        Server::builder()
            .add_service(AgentHostGrpcServer::new(host_service))
            .serve_with_incoming(UnixListenerStream::new(listener))
            .await
            .expect("serve agent host");
    });

    helpers::wait_for_socket(&host_socket).await;

    let mut host = AgentHost::connect().await.expect("connect agent host");
    let invoked = host
        .execute_tool(pb::ExecuteAgentToolRequest {
            run_id: "run-42".to_string(),
            tool_call_id: "call-7".to_string(),
            tool_id: "lookup".to_string(),
            arguments: Some(helpers::struct_from_json(serde_json::json!({
                "query": "Ada Lovelace"
            }))),
        })
        .await
        .expect("execute tool");
    assert_eq!(invoked.status, 207);
    assert_eq!(invoked.body, "run-42:call-7:lookup");

    host.emit_event(pb::EmitAgentEventRequest {
        run_id: "run-42".to_string(),
        r#type: "agent.tool_call.started".to_string(),
        visibility: "public".to_string(),
        data: Some(helpers::struct_from_json(serde_json::json!({
            "phase": "tool_call",
            "attempt": 1
        }))),
    })
    .await
    .expect("emit event");
    assert_eq!(
        *events.lock().expect("events lock"),
        vec!["run-42:agent.tool_call.started:public".to_string()]
    );

    host_task.abort();
    let _ = host_task.await;
}
