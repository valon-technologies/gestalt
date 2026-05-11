#[allow(dead_code)]
mod helpers;

use std::path::Path;
use std::sync::{Arc, Mutex};

use gestalt::proto::v1::agent_manager_host_server::{
    AgentManagerHost as ProtoAgentManagerHost, AgentManagerHostServer,
};
use gestalt::proto::v1::{
    AgentExecutionStatus, AgentInteraction, AgentInteractionState, AgentInteractionType,
    AgentManagerCancelTurnRequest, AgentManagerCreateSessionRequest, AgentManagerCreateTurnRequest,
    AgentManagerGetSessionRequest, AgentManagerGetTurnRequest, AgentManagerListInteractionsRequest,
    AgentManagerListInteractionsResponse, AgentManagerListSessionsRequest,
    AgentManagerListSessionsResponse, AgentManagerListTurnEventsRequest,
    AgentManagerListTurnEventsResponse, AgentManagerListTurnsRequest,
    AgentManagerListTurnsResponse, AgentManagerResolveInteractionRequest,
    AgentManagerUpdateSessionRequest, AgentMessagePartType, AgentSession, AgentSessionState,
    AgentToolSourceMode, AgentTurn, AgentTurnEvent,
};
use gestalt::{
    AgentManager, AgentManagerCancelTurnInput, AgentManagerCreateSessionInput,
    AgentManagerCreateTurnInput, AgentManagerGetSessionInput, AgentManagerGetTurnInput,
    AgentManagerListInteractionsInput, AgentManagerListSessionsInput,
    AgentManagerListTurnEventsInput, AgentManagerListTurnsInput,
    AgentManagerResolveInteractionInput, AgentManagerUpdateSessionInput, AgentMessageInput,
    AgentMessagePartInput, AgentMessagePartType as NativeAgentMessagePartType, AgentToolRefInput,
    ENV_AGENT_MANAGER_SOCKET, ENV_AGENT_MANAGER_SOCKET_TOKEN, Request,
};
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
    session_id: String,
    turn_id: String,
    interaction_id: String,
    reason: String,
}

#[derive(Clone, Default)]
struct TestAgentManagerServer {
    seen: Arc<Mutex<Vec<SeenRequest>>>,
    relay_tokens: Arc<Mutex<Vec<String>>>,
    create_turn_requests: Arc<Mutex<Vec<AgentManagerCreateTurnRequest>>>,
}

#[async_trait]
impl ProtoAgentManagerHost for TestAgentManagerServer {
    async fn create_session(
        &self,
        request: GrpcRequest<AgentManagerCreateSessionRequest>,
    ) -> std::result::Result<GrpcResponse<AgentSession>, Status> {
        maybe_record_relay_token(&self.relay_tokens, &request);
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "create_session".to_string(),
            invocation_token: request.invocation_token.clone(),
            provider_name: request.provider_name.clone(),
            session_id: String::new(),
            turn_id: String::new(),
            interaction_id: String::new(),
            reason: String::new(),
        });
        Ok(GrpcResponse::new(AgentSession {
            id: "session-managed-1".to_string(),
            provider_name: request.provider_name,
            model: request.model,
            client_ref: request.client_ref,
            state: AgentSessionState::Active as i32,
            metadata: request.metadata,
            created_at: Some(helpers::timestamp_now()),
            updated_at: Some(helpers::timestamp_now()),
            ..Default::default()
        }))
    }

    async fn get_session(
        &self,
        request: GrpcRequest<AgentManagerGetSessionRequest>,
    ) -> std::result::Result<GrpcResponse<AgentSession>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "get_session".to_string(),
            invocation_token: request.invocation_token.clone(),
            provider_name: String::new(),
            session_id: request.session_id.clone(),
            turn_id: String::new(),
            interaction_id: String::new(),
            reason: String::new(),
        });
        Ok(GrpcResponse::new(AgentSession {
            id: request.session_id,
            provider_name: "openai".to_string(),
            model: "gpt-5.1".to_string(),
            client_ref: "cli-session-1".to_string(),
            state: AgentSessionState::Archived as i32,
            created_at: Some(helpers::timestamp_now()),
            updated_at: Some(helpers::timestamp_now()),
            ..Default::default()
        }))
    }

    async fn list_sessions(
        &self,
        request: GrpcRequest<AgentManagerListSessionsRequest>,
    ) -> std::result::Result<GrpcResponse<AgentManagerListSessionsResponse>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "list_sessions".to_string(),
            invocation_token: request.invocation_token,
            provider_name: request.provider_name,
            session_id: String::new(),
            turn_id: String::new(),
            interaction_id: String::new(),
            reason: String::new(),
        });
        Ok(GrpcResponse::new(AgentManagerListSessionsResponse {
            sessions: vec![AgentSession {
                id: "session-managed-1".to_string(),
                provider_name: "openai".to_string(),
                model: "gpt-5.1".to_string(),
                client_ref: "cli-session-1".to_string(),
                state: AgentSessionState::Active as i32,
                created_at: Some(helpers::timestamp_now()),
                updated_at: Some(helpers::timestamp_now()),
                ..Default::default()
            }],
        }))
    }

    async fn update_session(
        &self,
        request: GrpcRequest<AgentManagerUpdateSessionRequest>,
    ) -> std::result::Result<GrpcResponse<AgentSession>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "update_session".to_string(),
            invocation_token: request.invocation_token.clone(),
            provider_name: String::new(),
            session_id: request.session_id.clone(),
            turn_id: String::new(),
            interaction_id: String::new(),
            reason: String::new(),
        });
        Ok(GrpcResponse::new(AgentSession {
            id: request.session_id,
            provider_name: "openai".to_string(),
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
        request: GrpcRequest<AgentManagerCreateTurnRequest>,
    ) -> std::result::Result<GrpcResponse<AgentTurn>, Status> {
        let request = request.into_inner();
        self.create_turn_requests
            .lock()
            .expect("lock create turn requests")
            .push(request.clone());
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "create_turn".to_string(),
            invocation_token: request.invocation_token.clone(),
            provider_name: String::new(),
            session_id: request.session_id.clone(),
            turn_id: String::new(),
            interaction_id: String::new(),
            reason: String::new(),
        });
        Ok(GrpcResponse::new(AgentTurn {
            id: "turn-managed-1".to_string(),
            session_id: request.session_id,
            provider_name: "openai".to_string(),
            model: request.model,
            status: AgentExecutionStatus::WaitingForInput as i32,
            messages: request.messages,
            output_text: "echo:Summarize this".to_string(),
            status_message: "waiting for input".to_string(),
            created_at: Some(helpers::timestamp_now()),
            started_at: Some(helpers::timestamp_now()),
            ..Default::default()
        }))
    }

    async fn get_turn(
        &self,
        request: GrpcRequest<AgentManagerGetTurnRequest>,
    ) -> std::result::Result<GrpcResponse<AgentTurn>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "get_turn".to_string(),
            invocation_token: request.invocation_token.clone(),
            provider_name: String::new(),
            session_id: String::new(),
            turn_id: request.turn_id.clone(),
            interaction_id: String::new(),
            reason: String::new(),
        });
        Ok(GrpcResponse::new(AgentTurn {
            id: request.turn_id,
            session_id: "session-managed-1".to_string(),
            provider_name: "openai".to_string(),
            model: "gpt-5.1".to_string(),
            status: AgentExecutionStatus::Succeeded as i32,
            output_text: "done".to_string(),
            status_message: "completed".to_string(),
            created_at: Some(helpers::timestamp_now()),
            started_at: Some(helpers::timestamp_now()),
            completed_at: Some(helpers::timestamp_now()),
            ..Default::default()
        }))
    }

    async fn list_turns(
        &self,
        request: GrpcRequest<AgentManagerListTurnsRequest>,
    ) -> std::result::Result<GrpcResponse<AgentManagerListTurnsResponse>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "list_turns".to_string(),
            invocation_token: request.invocation_token,
            provider_name: String::new(),
            session_id: request.session_id.clone(),
            turn_id: String::new(),
            interaction_id: String::new(),
            reason: String::new(),
        });
        Ok(GrpcResponse::new(AgentManagerListTurnsResponse {
            turns: vec![AgentTurn {
                id: "turn-managed-1".to_string(),
                session_id: request.session_id,
                provider_name: "openai".to_string(),
                model: "gpt-5.1".to_string(),
                status: AgentExecutionStatus::Running as i32,
                status_message: "running".to_string(),
                created_at: Some(helpers::timestamp_now()),
                started_at: Some(helpers::timestamp_now()),
                ..Default::default()
            }],
        }))
    }

    async fn cancel_turn(
        &self,
        request: GrpcRequest<AgentManagerCancelTurnRequest>,
    ) -> std::result::Result<GrpcResponse<AgentTurn>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "cancel_turn".to_string(),
            invocation_token: request.invocation_token.clone(),
            provider_name: String::new(),
            session_id: String::new(),
            turn_id: request.turn_id.clone(),
            interaction_id: String::new(),
            reason: request.reason.clone(),
        });
        Ok(GrpcResponse::new(AgentTurn {
            id: request.turn_id,
            session_id: "session-managed-1".to_string(),
            provider_name: "openai".to_string(),
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
        request: GrpcRequest<AgentManagerListTurnEventsRequest>,
    ) -> std::result::Result<GrpcResponse<AgentManagerListTurnEventsResponse>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "list_turn_events".to_string(),
            invocation_token: request.invocation_token,
            provider_name: String::new(),
            session_id: String::new(),
            turn_id: request.turn_id.clone(),
            interaction_id: String::new(),
            reason: String::new(),
        });
        Ok(GrpcResponse::new(AgentManagerListTurnEventsResponse {
            events: vec![AgentTurnEvent {
                id: format!("{}-event-1", request.turn_id.clone()),
                turn_id: request.turn_id,
                seq: 1,
                r#type: "turn.started".to_string(),
                source: "openai".to_string(),
                visibility: "private".to_string(),
                created_at: Some(helpers::timestamp_now()),
                ..Default::default()
            }],
        }))
    }

    async fn list_interactions(
        &self,
        request: GrpcRequest<AgentManagerListInteractionsRequest>,
    ) -> std::result::Result<GrpcResponse<AgentManagerListInteractionsResponse>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "list_interactions".to_string(),
            invocation_token: request.invocation_token,
            provider_name: String::new(),
            session_id: String::new(),
            turn_id: request.turn_id.clone(),
            interaction_id: String::new(),
            reason: String::new(),
        });
        Ok(GrpcResponse::new(AgentManagerListInteractionsResponse {
            interactions: vec![AgentInteraction {
                id: "interaction-1".to_string(),
                turn_id: request.turn_id,
                session_id: "session-managed-1".to_string(),
                r#type: AgentInteractionType::Approval as i32,
                state: AgentInteractionState::Pending as i32,
                title: "Approve command".to_string(),
                prompt: "Run git status?".to_string(),
                created_at: Some(helpers::timestamp_now()),
                ..Default::default()
            }],
        }))
    }

    async fn resolve_interaction(
        &self,
        request: GrpcRequest<AgentManagerResolveInteractionRequest>,
    ) -> std::result::Result<GrpcResponse<AgentInteraction>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "resolve_interaction".to_string(),
            invocation_token: request.invocation_token.clone(),
            provider_name: String::new(),
            session_id: String::new(),
            turn_id: request.turn_id.clone(),
            interaction_id: request.interaction_id.clone(),
            reason: String::new(),
        });
        Ok(GrpcResponse::new(AgentInteraction {
            id: request.interaction_id,
            turn_id: request.turn_id,
            session_id: "session-managed-1".to_string(),
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
    let created = manager
        .create_session(AgentManagerCreateSessionInput {
            provider_name: "openai".to_string(),
            model: "gpt-5.1".to_string(),
            client_ref: "cli-session-1".to_string(),
            ..Default::default()
        })
        .await
        .expect("create session");

    assert_eq!(created.id, "session-managed-1");
    assert_eq!(created.provider_name, "openai");

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
    let created_session = manager
        .create_session(AgentManagerCreateSessionInput {
            provider_name: "openai".to_string(),
            model: "gpt-5.1".to_string(),
            client_ref: "cli-session-1".to_string(),
            ..Default::default()
        })
        .await
        .expect("create session");
    let fetched_session = manager
        .get_session(AgentManagerGetSessionInput {
            session_id: "session-managed-1".to_string(),
        })
        .await
        .expect("get session");
    let listed_sessions = manager
        .list_sessions(AgentManagerListSessionsInput {
            provider_name: "openai".to_string(),
            ..Default::default()
        })
        .await
        .expect("list sessions");
    let updated_session = manager
        .update_session(AgentManagerUpdateSessionInput {
            session_id: "session-managed-1".to_string(),
            client_ref: "cli-session-2".to_string(),
            state: AgentSessionState::Archived,
            ..Default::default()
        })
        .await
        .expect("update session");
    let created_turn = manager
        .create_turn(AgentManagerCreateTurnInput {
            session_id: "session-managed-1".to_string(),
            model: "gpt-5.1".to_string(),
            messages: vec![AgentMessageInput {
                role: "user".to_string(),
                text: "Summarize this".to_string(),
                parts: vec![AgentMessagePartInput {
                    part_type: NativeAgentMessagePartType::Text,
                    text: "Summarize this".to_string(),
                    ..Default::default()
                }],
                ..Default::default()
            }],
            tool_source: AgentToolSourceMode::McpCatalog,
            ..Default::default()
        })
        .await
        .expect("create turn");
    let fetched_turn = manager
        .get_turn(AgentManagerGetTurnInput {
            turn_id: "turn-managed-1".to_string(),
        })
        .await
        .expect("get turn");
    let listed_turns = manager
        .list_turns(AgentManagerListTurnsInput {
            session_id: "session-managed-1".to_string(),
            ..Default::default()
        })
        .await
        .expect("list turns");
    let canceled_turn = manager
        .cancel_turn(AgentManagerCancelTurnInput {
            turn_id: "turn-managed-1".to_string(),
            reason: "user canceled".to_string(),
        })
        .await
        .expect("cancel turn");
    let turn_events = manager
        .list_turn_events(AgentManagerListTurnEventsInput {
            turn_id: "turn-managed-1".to_string(),
            after_seq: 0,
            limit: 10,
        })
        .await
        .expect("list turn events");
    let interactions = manager
        .list_interactions(AgentManagerListInteractionsInput {
            turn_id: "turn-managed-1".to_string(),
        })
        .await
        .expect("list interactions");
    let resolved = manager
        .resolve_interaction(AgentManagerResolveInteractionInput {
            turn_id: "turn-managed-1".to_string(),
            interaction_id: "interaction-1".to_string(),
            resolution: Some(serde_json::json!({
                "approved": true
            })),
        })
        .await
        .expect("resolve interaction");

    assert_eq!(created_session.id, "session-managed-1");
    assert_eq!(fetched_session.id, "session-managed-1");
    assert_eq!(listed_sessions.sessions.len(), 1);
    assert_eq!(updated_session.client_ref, "cli-session-2");
    assert_eq!(created_turn.id, "turn-managed-1");
    assert_eq!(created_turn.messages[0].parts.len(), 1);
    assert_eq!(fetched_turn.id, "turn-managed-1");
    assert_eq!(listed_turns.turns.len(), 1);
    assert_eq!(canceled_turn.status_message, "user canceled");
    assert_eq!(turn_events.events.len(), 1);
    assert_eq!(interactions.interactions.len(), 1);
    assert_eq!(resolved.id, "interaction-1");
    assert_eq!(
        AgentInteractionState::try_from(resolved.state).expect("valid interaction state"),
        AgentInteractionState::Resolved
    );

    let seen = server.seen.lock().expect("lock seen").clone();
    assert_eq!(
        seen,
        vec![
            SeenRequest {
                method: "create_session".to_string(),
                invocation_token: "token-123".to_string(),
                provider_name: "openai".to_string(),
                session_id: String::new(),
                turn_id: String::new(),
                interaction_id: String::new(),
                reason: String::new(),
            },
            SeenRequest {
                method: "get_session".to_string(),
                invocation_token: "token-123".to_string(),
                provider_name: String::new(),
                session_id: "session-managed-1".to_string(),
                turn_id: String::new(),
                interaction_id: String::new(),
                reason: String::new(),
            },
            SeenRequest {
                method: "list_sessions".to_string(),
                invocation_token: "token-123".to_string(),
                provider_name: "openai".to_string(),
                session_id: String::new(),
                turn_id: String::new(),
                interaction_id: String::new(),
                reason: String::new(),
            },
            SeenRequest {
                method: "update_session".to_string(),
                invocation_token: "token-123".to_string(),
                provider_name: String::new(),
                session_id: "session-managed-1".to_string(),
                turn_id: String::new(),
                interaction_id: String::new(),
                reason: String::new(),
            },
            SeenRequest {
                method: "create_turn".to_string(),
                invocation_token: "token-123".to_string(),
                provider_name: String::new(),
                session_id: "session-managed-1".to_string(),
                turn_id: String::new(),
                interaction_id: String::new(),
                reason: String::new(),
            },
            SeenRequest {
                method: "get_turn".to_string(),
                invocation_token: "token-123".to_string(),
                provider_name: String::new(),
                session_id: String::new(),
                turn_id: "turn-managed-1".to_string(),
                interaction_id: String::new(),
                reason: String::new(),
            },
            SeenRequest {
                method: "list_turns".to_string(),
                invocation_token: "token-123".to_string(),
                provider_name: String::new(),
                session_id: "session-managed-1".to_string(),
                turn_id: String::new(),
                interaction_id: String::new(),
                reason: String::new(),
            },
            SeenRequest {
                method: "cancel_turn".to_string(),
                invocation_token: "token-123".to_string(),
                provider_name: String::new(),
                session_id: String::new(),
                turn_id: "turn-managed-1".to_string(),
                interaction_id: String::new(),
                reason: "user canceled".to_string(),
            },
            SeenRequest {
                method: "list_turn_events".to_string(),
                invocation_token: "token-123".to_string(),
                provider_name: String::new(),
                session_id: String::new(),
                turn_id: "turn-managed-1".to_string(),
                interaction_id: String::new(),
                reason: String::new(),
            },
            SeenRequest {
                method: "list_interactions".to_string(),
                invocation_token: "token-123".to_string(),
                provider_name: String::new(),
                session_id: String::new(),
                turn_id: "turn-managed-1".to_string(),
                interaction_id: String::new(),
                reason: String::new(),
            },
            SeenRequest {
                method: "resolve_interaction".to_string(),
                invocation_token: "token-123".to_string(),
                provider_name: String::new(),
                session_id: String::new(),
                turn_id: "turn-managed-1".to_string(),
                interaction_id: "interaction-1".to_string(),
                reason: String::new(),
            },
        ]
    );

    serve_task.abort();
    let _ = serve_task.await;
}

#[tokio::test]
async fn agent_manager_create_turn_accepts_native_values() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("g-rust-agent-manager-native.sock");
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
    let created_turn = manager
        .create_turn(AgentManagerCreateTurnInput {
            session_id: "session-managed-1".to_string(),
            model: "gpt-5.1".to_string(),
            messages: vec![AgentMessageInput {
                role: "user".to_string(),
                text: "Summarize this".to_string(),
                parts: vec![AgentMessagePartInput {
                    text: "Summarize this".to_string(),
                    ..Default::default()
                }],
                metadata: Some(serde_json::json!({ "source": "native" })),
            }],
            tool_refs: vec![AgentToolRefInput {
                plugin: "github".to_string(),
                operation: "issues.get".to_string(),
                connection: "default".to_string(),
                ..Default::default()
            }],
            tool_source: AgentToolSourceMode::McpCatalog,
            response_schema: Some(serde_json::json!({ "type": "object" })),
            metadata: Some(serde_json::json!({ "request": "native" })),
            model_options: Some(serde_json::json!({ "temperature": 0 })),
            ..Default::default()
        })
        .await
        .expect("create turn");

    assert_eq!(created_turn.id, "turn-managed-1");

    let requests = server
        .create_turn_requests
        .lock()
        .expect("lock create turn requests")
        .clone();
    assert_eq!(requests.len(), 1);
    let request = &requests[0];
    assert_eq!(request.invocation_token, "token-123");
    assert_eq!(request.session_id, "session-managed-1");
    assert_eq!(request.model, "gpt-5.1");
    assert_eq!(request.tool_source, AgentToolSourceMode::McpCatalog as i32);
    assert_eq!(request.messages.len(), 1);
    assert_eq!(request.messages[0].role, "user");
    assert_eq!(request.messages[0].text, "Summarize this");
    assert_eq!(
        gestalt::protocol::json_from_struct(request.messages[0].metadata.as_ref().unwrap()),
        serde_json::json!({ "source": "native" })
    );
    assert_eq!(request.messages[0].parts.len(), 1);
    assert_eq!(
        request.messages[0].parts[0].r#type,
        AgentMessagePartType::Text as i32
    );
    assert_eq!(request.messages[0].parts[0].text, "Summarize this");
    assert_eq!(request.tool_refs.len(), 1);
    assert_eq!(request.tool_refs[0].plugin, "github");
    assert_eq!(request.tool_refs[0].operation, "issues.get");
    assert_eq!(request.tool_refs[0].connection, "default");
    assert_eq!(
        gestalt::protocol::json_from_struct(request.response_schema.as_ref().unwrap()),
        serde_json::json!({ "type": "object" })
    );
    assert_eq!(
        gestalt::protocol::json_from_struct(request.metadata.as_ref().unwrap()),
        serde_json::json!({ "request": "native" })
    );
    assert_eq!(
        gestalt::protocol::json_from_struct(request.model_options.as_ref().unwrap()),
        serde_json::json!({ "temperature": 0.0 })
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
        .get_session(AgentManagerGetSessionInput {
            session_id: "session-managed-1".to_string(),
        })
        .await
        .expect("get session");

    assert_eq!(response.id, "session-managed-1");

    let seen = server.seen.lock().expect("lock seen").clone();
    assert_eq!(seen.len(), 1);
    assert_eq!(seen[0].invocation_token, "token-embedded");
    assert_eq!(seen[0].method, "get_session");

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

fn maybe_record_relay_token(
    relay_tokens: &Arc<Mutex<Vec<String>>>,
    request: &GrpcRequest<AgentManagerCreateSessionRequest>,
) {
    if let Some(token) = request.metadata().get("x-gestalt-host-service-relay-token") {
        relay_tokens
            .lock()
            .expect("lock relay tokens")
            .push(token.to_str().expect("relay token ascii").to_string());
    }
}
