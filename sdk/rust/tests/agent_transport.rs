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
    self as pb, AgentExecutionStatus, AgentInteractionState, AgentInteractionType, AgentMessage,
    AgentMessagePart, AgentMessagePartType, AgentSessionState, ConfigureProviderRequest,
    ProviderKind,
};
use gestalt::{AgentHost, AgentProvider, ENV_AGENT_HOST_SOCKET_TOKEN, RuntimeMetadata};
use hyper_util::rt::tokio::TokioIo;
use tokio::net::{TcpListener, UnixListener, UnixStream};
use tokio_stream::wrappers::{TcpListenerStream, UnixListenerStream};
use tonic::transport::{Endpoint, Server};
use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};
use tower::service_fn;

#[derive(Default)]
struct TestAgentProvider {
    configured_name: Mutex<String>,
}

#[derive(Default, Clone)]
struct TestAgentHostService {
    relay_tokens: Arc<Mutex<Vec<String>>>,
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
    async fn create_session(
        &self,
        request: GrpcRequest<pb::CreateAgentProviderSessionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentSession>, Status> {
        let request = request.into_inner();
        Ok(GrpcResponse::new(pb::AgentSession {
            id: request.session_id,
            provider_name: configured_name(self),
            model: request.model,
            client_ref: request.client_ref,
            state: AgentSessionState::Active as i32,
            metadata: request.metadata,
            created_by: request.created_by,
            created_at: Some(helpers::timestamp_now()),
            updated_at: Some(helpers::timestamp_now()),
            ..Default::default()
        }))
    }

    async fn get_session(
        &self,
        request: GrpcRequest<pb::GetAgentProviderSessionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentSession>, Status> {
        let request = request.into_inner();
        Ok(GrpcResponse::new(pb::AgentSession {
            id: request.session_id,
            provider_name: configured_name(self),
            model: "gpt-5.1".to_string(),
            client_ref: "cli-session-1".to_string(),
            state: AgentSessionState::Archived as i32,
            metadata: Some(helpers::struct_from_json(serde_json::json!({
                "source": "rust-test"
            }))),
            created_at: Some(helpers::timestamp_now()),
            updated_at: Some(helpers::timestamp_now()),
            last_turn_at: Some(helpers::timestamp_now()),
            ..Default::default()
        }))
    }

    async fn list_sessions(
        &self,
        _request: GrpcRequest<pb::ListAgentProviderSessionsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListAgentProviderSessionsResponse>, Status> {
        Ok(GrpcResponse::new(pb::ListAgentProviderSessionsResponse {
            sessions: vec![pb::AgentSession {
                id: "session-1".to_string(),
                provider_name: configured_name(self),
                model: "gpt-5.1".to_string(),
                client_ref: "cli-session-1".to_string(),
                state: AgentSessionState::Archived as i32,
                created_at: Some(helpers::timestamp_now()),
                updated_at: Some(helpers::timestamp_now()),
                ..Default::default()
            }],
        }))
    }

    async fn update_session(
        &self,
        request: GrpcRequest<pb::UpdateAgentProviderSessionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentSession>, Status> {
        let request = request.into_inner();
        Ok(GrpcResponse::new(pb::AgentSession {
            id: request.session_id,
            provider_name: configured_name(self),
            model: "gpt-5.1".to_string(),
            client_ref: request.client_ref,
            state: request.state,
            metadata: request.metadata,
            created_at: Some(helpers::timestamp_now()),
            updated_at: Some(helpers::timestamp_now()),
            ..Default::default()
        }))
    }

    async fn create_turn(
        &self,
        request: GrpcRequest<pb::CreateAgentProviderTurnRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentTurn>, Status> {
        let request = request.into_inner();
        Ok(GrpcResponse::new(pb::AgentTurn {
            id: request.turn_id,
            session_id: request.session_id,
            provider_name: configured_name(self),
            model: request.model,
            status: AgentExecutionStatus::WaitingForInput as i32,
            messages: request.messages,
            output_text: "echo:Plan it".to_string(),
            status_message: "waiting for input".to_string(),
            created_by: request.created_by,
            created_at: Some(helpers::timestamp_now()),
            started_at: Some(helpers::timestamp_now()),
            execution_ref: request.execution_ref,
            ..Default::default()
        }))
    }

    async fn get_turn(
        &self,
        request: GrpcRequest<pb::GetAgentProviderTurnRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentTurn>, Status> {
        let request = request.into_inner();
        Ok(GrpcResponse::new(pb::AgentTurn {
            id: request.turn_id,
            session_id: "session-1".to_string(),
            provider_name: configured_name(self),
            model: "gpt-5.1".to_string(),
            status: AgentExecutionStatus::WaitingForInput as i32,
            output_text: "echo:Plan it".to_string(),
            status_message: "waiting for input".to_string(),
            created_at: Some(helpers::timestamp_now()),
            started_at: Some(helpers::timestamp_now()),
            ..Default::default()
        }))
    }

    async fn list_turns(
        &self,
        request: GrpcRequest<pb::ListAgentProviderTurnsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListAgentProviderTurnsResponse>, Status> {
        let request = request.into_inner();
        Ok(GrpcResponse::new(pb::ListAgentProviderTurnsResponse {
            turns: vec![pb::AgentTurn {
                id: "turn-1".to_string(),
                session_id: request.session_id,
                provider_name: configured_name(self),
                model: "gpt-5.1".to_string(),
                status: AgentExecutionStatus::Succeeded as i32,
                status_message: "done".to_string(),
                created_at: Some(helpers::timestamp_now()),
                started_at: Some(helpers::timestamp_now()),
                completed_at: Some(helpers::timestamp_now()),
                ..Default::default()
            }],
        }))
    }

    async fn cancel_turn(
        &self,
        request: GrpcRequest<pb::CancelAgentProviderTurnRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentTurn>, Status> {
        let request = request.into_inner();
        Ok(GrpcResponse::new(pb::AgentTurn {
            id: request.turn_id,
            session_id: "session-1".to_string(),
            provider_name: configured_name(self),
            model: "gpt-5.1".to_string(),
            status: AgentExecutionStatus::Canceled as i32,
            status_message: request.reason,
            created_at: Some(helpers::timestamp_now()),
            started_at: Some(helpers::timestamp_now()),
            completed_at: Some(helpers::timestamp_now()),
            ..Default::default()
        }))
    }

    async fn list_turn_events(
        &self,
        request: GrpcRequest<pb::ListAgentProviderTurnEventsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListAgentProviderTurnEventsResponse>, Status> {
        let request = request.into_inner();
        let provider_name = configured_name(self);
        Ok(GrpcResponse::new(pb::ListAgentProviderTurnEventsResponse {
            events: vec![
                pb::AgentTurnEvent {
                    id: format!("{}-event-1", request.turn_id),
                    turn_id: request.turn_id.clone(),
                    seq: 1,
                    r#type: "turn.started".to_string(),
                    source: provider_name.clone(),
                    visibility: "private".to_string(),
                    created_at: Some(helpers::timestamp_now()),
                    ..Default::default()
                },
                pb::AgentTurnEvent {
                    id: format!("{}-event-2", request.turn_id),
                    turn_id: request.turn_id,
                    seq: 2,
                    r#type: "interaction.requested".to_string(),
                    source: provider_name,
                    visibility: "private".to_string(),
                    created_at: Some(helpers::timestamp_now()),
                    ..Default::default()
                },
            ],
        }))
    }

    async fn get_interaction(
        &self,
        request: GrpcRequest<pb::GetAgentProviderInteractionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentInteraction>, Status> {
        let request = request.into_inner();
        Ok(GrpcResponse::new(pb::AgentInteraction {
            id: request.interaction_id,
            turn_id: "turn-1".to_string(),
            session_id: "session-1".to_string(),
            r#type: AgentInteractionType::Approval as i32,
            state: AgentInteractionState::Pending as i32,
            title: "Approve command".to_string(),
            prompt: "Run git status?".to_string(),
            created_at: Some(helpers::timestamp_now()),
            ..Default::default()
        }))
    }

    async fn list_interactions(
        &self,
        request: GrpcRequest<pb::ListAgentProviderInteractionsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListAgentProviderInteractionsResponse>, Status> {
        let request = request.into_inner();
        Ok(GrpcResponse::new(
            pb::ListAgentProviderInteractionsResponse {
                interactions: vec![pb::AgentInteraction {
                    id: "interaction-1".to_string(),
                    turn_id: request.turn_id,
                    session_id: "session-1".to_string(),
                    r#type: AgentInteractionType::Approval as i32,
                    state: AgentInteractionState::Pending as i32,
                    title: "Approve command".to_string(),
                    prompt: "Run git status?".to_string(),
                    created_at: Some(helpers::timestamp_now()),
                    ..Default::default()
                }],
            },
        ))
    }

    async fn resolve_interaction(
        &self,
        request: GrpcRequest<pb::ResolveAgentProviderInteractionRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentInteraction>, Status> {
        let request = request.into_inner();
        Ok(GrpcResponse::new(pb::AgentInteraction {
            id: request.interaction_id,
            turn_id: "turn-1".to_string(),
            session_id: "session-1".to_string(),
            r#type: AgentInteractionType::Approval as i32,
            state: AgentInteractionState::Resolved as i32,
            title: "Approve command".to_string(),
            prompt: "Run git status?".to_string(),
            resolution: request.resolution,
            created_at: Some(helpers::timestamp_now()),
            resolved_at: Some(helpers::timestamp_now()),
            ..Default::default()
        }))
    }

    async fn get_capabilities(
        &self,
        _request: GrpcRequest<pb::GetAgentProviderCapabilitiesRequest>,
    ) -> std::result::Result<GrpcResponse<pb::AgentProviderCapabilities>, Status> {
        Ok(GrpcResponse::new(pb::AgentProviderCapabilities {
            streaming_text: true,
            tool_calls: true,
            parallel_tool_calls: false,
            structured_output: true,
            interactions: true,
            resumable_turns: true,
            reasoning_summaries: false,
            bounded_list_hydration: true,
            supported_tool_sources: vec![pb::AgentToolSourceMode::McpCatalog as i32],
        }))
    }
}

#[tonic::async_trait]
impl AgentHostRpc for TestAgentHostService {
    async fn list_tools(
        &self,
        request: GrpcRequest<pb::ListAgentToolsRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ListAgentToolsResponse>, Status> {
        let request = request.into_inner();
        Ok(GrpcResponse::new(pb::ListAgentToolsResponse {
            tools: vec![pb::ListedAgentTool {
                id: format!("{}:{}:lookup", request.session_id, request.turn_id),
                mcp_name: "search__lookup".to_string(),
                title: "lookup".to_string(),
                description: "Look up records".to_string(),
                input_schema: r#"{"type":"object"}"#.to_string(),
                r#ref: Some(pb::AgentToolRef {
                    plugin: "search".to_string(),
                    operation: "lookup".to_string(),
                    ..Default::default()
                }),
                ..Default::default()
            }],
            next_page_token: "next-1".to_string(),
        }))
    }

    async fn execute_tool(
        &self,
        request: GrpcRequest<pb::ExecuteAgentToolRequest>,
    ) -> std::result::Result<GrpcResponse<pb::ExecuteAgentToolResponse>, Status> {
        if let Some(token) = request.metadata().get("x-gestalt-host-service-relay-token") {
            self.relay_tokens
                .lock()
                .expect("lock relay tokens")
                .push(token.to_str().expect("relay token ascii").to_string());
        }
        let request = request.into_inner();
        Ok(GrpcResponse::new(pb::ExecuteAgentToolResponse {
            status: 207,
            body: format!(
                "{}:{}:{}:{}",
                request.session_id, request.turn_id, request.tool_call_id, request.tool_id
            ),
        }))
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
    let session = client
        .create_session(pb::CreateAgentProviderSessionRequest {
            session_id: "session-1".to_string(),
            idempotency_key: "session-req-1".to_string(),
            model: "gpt-5.1".to_string(),
            client_ref: "cli-session-1".to_string(),
            metadata: Some(helpers::struct_from_json(serde_json::json!({
                "source": "rust-test"
            }))),
            ..Default::default()
        })
        .await
        .expect("create session")
        .into_inner();
    assert_eq!(session.id, "session-1");
    assert_eq!(
        AgentSessionState::try_from(session.state)
            .expect("valid session state")
            .as_str_name(),
        "AGENT_SESSION_STATE_ACTIVE"
    );

    let listed_sessions = client
        .list_sessions(pb::ListAgentProviderSessionsRequest {
            ..Default::default()
        })
        .await
        .expect("list sessions")
        .into_inner();
    assert_eq!(listed_sessions.sessions.len(), 1);

    let fetched_session = client
        .get_session(pb::GetAgentProviderSessionRequest {
            session_id: "session-1".to_string(),
            ..Default::default()
        })
        .await
        .expect("get session")
        .into_inner();
    assert_eq!(
        AgentSessionState::try_from(fetched_session.state)
            .expect("valid fetched session state")
            .as_str_name(),
        "AGENT_SESSION_STATE_ARCHIVED"
    );

    let updated_session = client
        .update_session(pb::UpdateAgentProviderSessionRequest {
            session_id: "session-1".to_string(),
            client_ref: "cli-session-2".to_string(),
            state: AgentSessionState::Archived as i32,
            metadata: Some(helpers::struct_from_json(serde_json::json!({
                "source": "rust-test-updated"
            }))),
            ..Default::default()
        })
        .await
        .expect("update session")
        .into_inner();
    assert_eq!(updated_session.client_ref, "cli-session-2");

    let created_turn = client
        .create_turn(pb::CreateAgentProviderTurnRequest {
            turn_id: "turn-1".to_string(),
            session_id: "session-1".to_string(),
            model: "gpt-5.1".to_string(),
            messages: vec![AgentMessage {
                role: "user".to_string(),
                text: "Plan it".to_string(),
                parts: vec![AgentMessagePart {
                    r#type: AgentMessagePartType::Text as i32,
                    text: "Plan it".to_string(),
                    ..Default::default()
                }],
                metadata: Some(helpers::struct_from_json(serde_json::json!({
                    "priority": "high"
                }))),
            }],
            execution_ref: "exec-turn-1".to_string(),
            ..Default::default()
        })
        .await
        .expect("create turn")
        .into_inner();
    assert_eq!(created_turn.id, "turn-1");
    assert_eq!(
        AgentExecutionStatus::try_from(created_turn.status)
            .expect("valid turn status")
            .as_str_name(),
        "AGENT_EXECUTION_STATUS_WAITING_FOR_INPUT"
    );
    assert_eq!(created_turn.messages[0].parts.len(), 1);

    let listed_turns = client
        .list_turns(pb::ListAgentProviderTurnsRequest {
            session_id: "session-1".to_string(),
            ..Default::default()
        })
        .await
        .expect("list turns")
        .into_inner();
    assert_eq!(listed_turns.turns.len(), 1);

    let fetched_turn = client
        .get_turn(pb::GetAgentProviderTurnRequest {
            turn_id: "turn-1".to_string(),
            ..Default::default()
        })
        .await
        .expect("get turn")
        .into_inner();
    assert_eq!(fetched_turn.status_message, "waiting for input");

    let turn_events = client
        .list_turn_events(pb::ListAgentProviderTurnEventsRequest {
            turn_id: "turn-1".to_string(),
            after_seq: 0,
            limit: 10,
            ..Default::default()
        })
        .await
        .expect("list turn events")
        .into_inner();
    assert_eq!(
        turn_events
            .events
            .iter()
            .map(|event| event.r#type.clone())
            .collect::<Vec<_>>(),
        vec![
            "turn.started".to_string(),
            "interaction.requested".to_string()
        ]
    );

    let listed_interactions = client
        .list_interactions(pb::ListAgentProviderInteractionsRequest {
            turn_id: "turn-1".to_string(),
            ..Default::default()
        })
        .await
        .expect("list interactions")
        .into_inner();
    assert_eq!(listed_interactions.interactions.len(), 1);

    let fetched_interaction = client
        .get_interaction(pb::GetAgentProviderInteractionRequest {
            interaction_id: "interaction-1".to_string(),
            ..Default::default()
        })
        .await
        .expect("get interaction")
        .into_inner();
    assert_eq!(
        AgentInteractionState::try_from(fetched_interaction.state)
            .expect("valid interaction state")
            .as_str_name(),
        "AGENT_INTERACTION_STATE_PENDING"
    );

    let resolved_interaction = client
        .resolve_interaction(pb::ResolveAgentProviderInteractionRequest {
            interaction_id: "interaction-1".to_string(),
            resolution: Some(helpers::struct_from_json(serde_json::json!({
                "approved": true
            }))),
            ..Default::default()
        })
        .await
        .expect("resolve interaction")
        .into_inner();
    assert_eq!(
        AgentInteractionState::try_from(resolved_interaction.state)
            .expect("valid resolved interaction state")
            .as_str_name(),
        "AGENT_INTERACTION_STATE_RESOLVED"
    );

    let capabilities = client
        .get_capabilities(pb::GetAgentProviderCapabilitiesRequest {})
        .await
        .expect("get capabilities")
        .into_inner();
    assert!(capabilities.streaming_text);
    assert!(capabilities.tool_calls);
    assert!(capabilities.interactions);
    assert!(capabilities.resumable_turns);

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
    let listed = host
        .list_tools(pb::ListAgentToolsRequest {
            session_id: "session-1".to_string(),
            turn_id: "turn-1".to_string(),
            page_size: 10,
            page_token: "page-0".to_string(),
            tool_grant: "grant-token".to_string(),
        })
        .await
        .expect("list tools");
    assert_eq!(listed.tools.len(), 1);
    assert_eq!(listed.tools[0].mcp_name, "search__lookup");
    assert_eq!(listed.next_page_token, "next-1");

    let invoked = host
        .execute_tool(pb::ExecuteAgentToolRequest {
            session_id: "session-1".to_string(),
            turn_id: "turn-1".to_string(),
            tool_call_id: "call-7".to_string(),
            tool_id: "lookup".to_string(),
            idempotency_key: "agent/simple:agent-runtime:turn-1:call-7".to_string(),
            arguments: Some(helpers::struct_from_json(serde_json::json!({
                "query": "Ada Lovelace"
            }))),
            ..Default::default()
        })
        .await
        .expect("execute tool");
    assert_eq!(invoked.status, 207);
    assert_eq!(invoked.body, "session-1:turn-1:call-7:lookup");

    host_task.abort();
    let _ = host_task.await;
}

#[tokio::test]
async fn agent_host_client_round_trip_over_tcp_and_sends_relay_token() {
    let _env_lock = helpers::env_lock().lock().await;
    let listener = TcpListener::bind("127.0.0.1:0")
        .await
        .expect("bind tcp listener");
    let address = listener.local_addr().expect("local addr");
    let _agent_host_env =
        helpers::EnvGuard::set(gestalt::ENV_AGENT_HOST_SOCKET, format!("tcp://{address}"));
    let _token_guard = helpers::EnvGuard::set(ENV_AGENT_HOST_SOCKET_TOKEN, "relay-token-rust");

    let host_service = TestAgentHostService::default();
    let served_service = host_service.clone();
    let host_task = tokio::spawn(async move {
        Server::builder()
            .add_service(AgentHostGrpcServer::new(served_service))
            .serve_with_incoming(TcpListenerStream::new(listener))
            .await
            .expect("serve agent host");
    });

    let mut host = AgentHost::connect().await.expect("connect agent host");
    let invoked = host
        .execute_tool(pb::ExecuteAgentToolRequest {
            session_id: "session-1".to_string(),
            turn_id: "turn-1".to_string(),
            tool_call_id: "call-7".to_string(),
            tool_id: "lookup".to_string(),
            ..Default::default()
        })
        .await
        .expect("execute tool");

    assert_eq!(invoked.status, 207);
    assert_eq!(invoked.body, "session-1:turn-1:call-7:lookup");
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

fn configured_name(provider: &TestAgentProvider) -> String {
    provider
        .configured_name
        .lock()
        .expect("configured_name lock")
        .clone()
}
