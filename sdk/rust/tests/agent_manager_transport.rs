#[allow(dead_code)]
mod helpers;

use std::path::Path;
use std::sync::{Arc, Mutex};

use gestalt::proto::v1::agent_manager_host_server::{
    AgentManagerHost as ProtoAgentManagerHost, AgentManagerHostServer,
};
use gestalt::proto::v1::{
    AgentManagerCancelRunRequest, AgentManagerGetRunRequest, AgentManagerListRunsRequest,
    AgentManagerListRunsResponse, AgentManagerRunRequest, AgentMessage, AgentRunStatus,
    AgentToolSourceMode, BoundAgentRun, ManagedAgentRun,
};
use gestalt::{AgentManager, ENV_AGENT_MANAGER_SOCKET, ENV_AGENT_MANAGER_SOCKET_TOKEN, Request};
use tokio::net::{TcpListener, UnixListener};
use tokio_stream::wrappers::{TcpListenerStream, UnixListenerStream};
use tonic::codegen::async_trait;
use tonic::transport::Server;
use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};

#[derive(Clone, Debug, Default, PartialEq)]
struct SeenRequest {
    method: String,
    invocation_token: String,
    provider_name: String,
    run_id: String,
    reason: String,
}

#[derive(Clone, Default)]
struct TestAgentManagerServer {
    seen: Arc<Mutex<Vec<SeenRequest>>>,
    relay_tokens: Arc<Mutex<Vec<String>>>,
}

#[async_trait]
impl ProtoAgentManagerHost for TestAgentManagerServer {
    async fn run(
        &self,
        request: GrpcRequest<AgentManagerRunRequest>,
    ) -> std::result::Result<GrpcResponse<ManagedAgentRun>, Status> {
        if let Some(token) = request.metadata().get("x-gestalt-host-service-relay-token") {
            self.relay_tokens
                .lock()
                .expect("lock relay tokens")
                .push(token.to_str().expect("relay token ascii").to_string());
        }
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "run".to_string(),
            invocation_token: request.invocation_token.clone(),
            provider_name: request.provider_name.clone(),
            run_id: String::new(),
            reason: String::new(),
        });
        Ok(GrpcResponse::new(ManagedAgentRun {
            provider_name: request.provider_name.clone(),
            run: Some(BoundAgentRun {
                id: "run-managed-1".to_string(),
                provider_name: request.provider_name,
                model: request.model,
                status: AgentRunStatus::Running as i32,
                messages: request.messages,
                ..Default::default()
            }),
        }))
    }

    async fn get_run(
        &self,
        request: GrpcRequest<AgentManagerGetRunRequest>,
    ) -> std::result::Result<GrpcResponse<ManagedAgentRun>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "get".to_string(),
            invocation_token: request.invocation_token.clone(),
            provider_name: String::new(),
            run_id: request.run_id.clone(),
            reason: String::new(),
        });
        Ok(GrpcResponse::new(ManagedAgentRun {
            provider_name: "openai".to_string(),
            run: Some(BoundAgentRun {
                id: request.run_id,
                provider_name: "openai".to_string(),
                model: "gpt-5.1".to_string(),
                status: AgentRunStatus::Succeeded as i32,
                ..Default::default()
            }),
        }))
    }

    async fn list_runs(
        &self,
        request: GrpcRequest<AgentManagerListRunsRequest>,
    ) -> std::result::Result<GrpcResponse<AgentManagerListRunsResponse>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "list".to_string(),
            invocation_token: request.invocation_token,
            provider_name: String::new(),
            run_id: String::new(),
            reason: String::new(),
        });
        Ok(GrpcResponse::new(AgentManagerListRunsResponse {
            runs: vec![ManagedAgentRun {
                provider_name: "openai".to_string(),
                run: Some(BoundAgentRun {
                    id: "run-managed-1".to_string(),
                    provider_name: "openai".to_string(),
                    model: "gpt-5.1".to_string(),
                    status: AgentRunStatus::Running as i32,
                    ..Default::default()
                }),
            }],
        }))
    }

    async fn cancel_run(
        &self,
        request: GrpcRequest<AgentManagerCancelRunRequest>,
    ) -> std::result::Result<GrpcResponse<ManagedAgentRun>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "cancel".to_string(),
            invocation_token: request.invocation_token.clone(),
            provider_name: String::new(),
            run_id: request.run_id.clone(),
            reason: request.reason.clone(),
        });
        Ok(GrpcResponse::new(ManagedAgentRun {
            provider_name: "openai".to_string(),
            run: Some(BoundAgentRun {
                id: request.run_id,
                provider_name: "openai".to_string(),
                model: "gpt-5.1".to_string(),
                status: AgentRunStatus::Canceled as i32,
                status_message: request.reason,
                ..Default::default()
            }),
        }))
    }
}

#[tokio::test]
async fn agent_manager_connects_over_tcp_and_sends_relay_token() {
    let _env_lock = helpers::env_lock().lock().await;

    let listener = TcpListener::bind("127.0.0.1:0")
        .await
        .expect("bind tcp listener");
    let address = listener.local_addr().expect("local addr");
    let _socket_guard =
        helpers::EnvGuard::set(ENV_AGENT_MANAGER_SOCKET, format!("tcp://{address}"));
    let _token_guard = helpers::EnvGuard::set(ENV_AGENT_MANAGER_SOCKET_TOKEN, "relay-token-rust");

    let server = TestAgentManagerServer::default();
    let serve_server = server.clone();
    let serve_task = tokio::spawn(async move {
        serve_agent_manager_tcp(serve_server, listener)
            .await
            .expect("serve agent manager");
    });

    let mut manager = AgentManager::connect("token-123")
        .await
        .expect("connect agent manager");
    let started = manager
        .run(AgentManagerRunRequest {
            provider_name: "openai".to_string(),
            model: "gpt-5.1".to_string(),
            messages: vec![AgentMessage {
                role: "user".to_string(),
                text: "hello".to_string(),
            }],
            tool_source: AgentToolSourceMode::Explicit as i32,
            ..Default::default()
        })
        .await
        .expect("run agent");

    assert_eq!(started.provider_name, "openai");
    assert_eq!(started.run.expect("managed run").id, "run-managed-1");

    let relay_tokens = server
        .relay_tokens
        .lock()
        .expect("lock relay tokens")
        .clone();
    assert_eq!(relay_tokens, vec!["relay-token-rust".to_string()]);

    serve_task.abort();
    let _ = serve_task.await;
}

#[tokio::test]
async fn agent_manager_connects_over_unix_socket_and_sends_invocation_token() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("g-rust-agent-manager.sock");
    let _socket_guard = helpers::EnvGuard::set(ENV_AGENT_MANAGER_SOCKET, socket.as_os_str());

    let server = TestAgentManagerServer::default();
    let serve_server = server.clone();
    let serve_socket = socket.clone();
    let serve_task = tokio::spawn(async move {
        serve_agent_manager(serve_server, &serve_socket)
            .await
            .expect("serve agent manager");
    });

    helpers::wait_for_socket(&socket).await;

    let mut manager = AgentManager::connect("token-123")
        .await
        .expect("connect agent manager");
    let started = manager
        .run(AgentManagerRunRequest {
            provider_name: "openai".to_string(),
            model: "gpt-5.1".to_string(),
            messages: vec![AgentMessage {
                role: "user".to_string(),
                text: "Summarize this".to_string(),
            }],
            tool_source: AgentToolSourceMode::Explicit as i32,
            ..Default::default()
        })
        .await
        .expect("run agent");
    let fetched = manager
        .get_run(AgentManagerGetRunRequest {
            run_id: "run-managed-1".to_string(),
            ..Default::default()
        })
        .await
        .expect("get run");
    let listed = manager
        .list_runs(AgentManagerListRunsRequest::default())
        .await
        .expect("list runs");
    let canceled = manager
        .cancel_run(AgentManagerCancelRunRequest {
            run_id: "run-managed-1".to_string(),
            reason: "user canceled".to_string(),
            ..Default::default()
        })
        .await
        .expect("cancel run");

    assert_eq!(started.provider_name, "openai");
    assert_eq!(started.run.expect("started run").id, "run-managed-1");
    assert_eq!(fetched.run.expect("fetched run").id, "run-managed-1");
    assert_eq!(listed.runs.len(), 1);
    assert_eq!(
        listed.runs[0].run.as_ref().expect("listed run").id,
        "run-managed-1"
    );
    assert_eq!(
        canceled.run.expect("canceled run").status_message,
        "user canceled"
    );

    let seen = server.seen.lock().expect("lock seen").clone();
    assert_eq!(
        seen,
        vec![
            SeenRequest {
                method: "run".to_string(),
                invocation_token: "token-123".to_string(),
                provider_name: "openai".to_string(),
                run_id: String::new(),
                reason: String::new(),
            },
            SeenRequest {
                method: "get".to_string(),
                invocation_token: "token-123".to_string(),
                provider_name: String::new(),
                run_id: "run-managed-1".to_string(),
                reason: String::new(),
            },
            SeenRequest {
                method: "list".to_string(),
                invocation_token: "token-123".to_string(),
                provider_name: String::new(),
                run_id: String::new(),
                reason: String::new(),
            },
            SeenRequest {
                method: "cancel".to_string(),
                invocation_token: "token-123".to_string(),
                provider_name: String::new(),
                run_id: "run-managed-1".to_string(),
                reason: "user canceled".to_string(),
            },
        ]
    );

    serve_task.abort();
    let _ = serve_task.await;
}

#[tokio::test]
async fn request_agent_manager_uses_embedded_invocation_token() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("g-rust-req-agent-manager.sock");
    let _socket_guard = helpers::EnvGuard::set(ENV_AGENT_MANAGER_SOCKET, socket.as_os_str());

    let server = TestAgentManagerServer::default();
    let serve_server = server.clone();
    let serve_socket = socket.clone();
    let serve_task = tokio::spawn(async move {
        serve_agent_manager(serve_server, &serve_socket)
            .await
            .expect("serve agent manager");
    });

    helpers::wait_for_socket(&socket).await;

    let request = Request {
        invocation_token: "token-embedded".to_string(),
        ..Request::default()
    };
    let mut manager = request
        .agent_manager()
        .await
        .expect("request agent manager");
    let response = manager
        .get_run(AgentManagerGetRunRequest {
            run_id: "run-managed-1".to_string(),
            ..Default::default()
        })
        .await
        .expect("get run");

    assert_eq!(response.run.expect("run").id, "run-managed-1");

    let seen = server.seen.lock().expect("lock seen").clone();
    assert_eq!(seen.len(), 1);
    assert_eq!(seen[0].invocation_token, "token-embedded");
    assert_eq!(seen[0].method, "get");

    serve_task.abort();
    let _ = serve_task.await;
}

async fn serve_agent_manager(
    server: TestAgentManagerServer,
    socket: &Path,
) -> std::result::Result<(), tonic::transport::Error> {
    let _ = std::fs::remove_file(socket);
    let listener = UnixListener::bind(socket).expect("bind unix listener");

    Server::builder()
        .add_service(AgentManagerHostServer::new(server))
        .serve_with_incoming(UnixListenerStream::new(listener))
        .await
}

async fn serve_agent_manager_tcp(
    server: TestAgentManagerServer,
    listener: TcpListener,
) -> std::result::Result<(), tonic::transport::Error> {
    Server::builder()
        .add_service(AgentManagerHostServer::new(server))
        .serve_with_incoming(TcpListenerStream::new(listener))
        .await
}
