#[path = "../src/generated.rs"]
mod generated;

mod support_protocol;

#[allow(dead_code)]
mod helpers;

use std::path::Path;
use std::sync::{Arc, Mutex};

use generated::v1::agent_server::{Agent as ProtoAgentProvider, AgentServer};
use generated::v1::{
    AgentExecutionStatus, AgentInteraction, AgentInteractionState as ProtoAgentInteractionState,
    AgentInteractionType, AgentMessagePartType, AgentProviderCapabilities, AgentSession,
    AgentSessionState as ProtoAgentSessionState, AgentTurn, AgentTurnEvent, AgentTurnTextOutput,
    CancelAgentProviderTurnRequest, CreateAgentProviderSessionRequest,
    CreateAgentProviderTurnRequest, GetAgentProviderCapabilitiesRequest,
    GetAgentProviderInteractionRequest, GetAgentProviderSessionRequest,
    GetAgentProviderTurnRequest, ListAgentProviderInteractionsRequest,
    ListAgentProviderInteractionsResponse, ListAgentProviderSessionsRequest,
    ListAgentProviderSessionsResponse, ListAgentProviderTurnEventsRequest,
    ListAgentProviderTurnEventsResponse, ListAgentProviderTurnsRequest,
    ListAgentProviderTurnsResponse, RequestContext as ProviderRequestContext,
    ResolveAgentProviderInteractionRequest, UpdateAgentProviderSessionRequest,
};
use gestalt::Agent;
use gestalt::agent::{
    AgentCancelTurnOptions, AgentCatalogToolConfig, AgentCreateSessionOptions,
    AgentCreateTurnOptions, AgentGetSessionOptions, AgentGetTurnOptions,
    AgentListInteractionsOptions, AgentListSessionsOptions, AgentListTurnEventsOptions,
    AgentListTurnsOptions, AgentMessage, AgentMessagePart, AgentOutput, AgentOutputKind,
    AgentResolveInteractionOptions, AgentStructuredOutput, AgentTextOutput,
    AgentToolConfig as NativeAgentToolConfig, AgentToolConfigSource as NativeAgentToolConfigSource,
    AgentUpdateSessionOptions, GetAgentProviderSessionRequest as NativeGetSessionRequest,
    ListedAgentTool as NativeListedAgentTool, agent_interaction_state, agent_message_part_type,
    agent_session_state,
};
use gestalt::app::{AgentToolRef as NativeAgentToolRef, RequestContext, SubjectContext};
use tokio::net::{TcpListener, UnixListener};
use tokio_stream::wrappers::{TcpListenerStream, UnixListenerStream};
use tonic::codegen::async_trait;
use tonic::transport::Server;
use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};

#[derive(Clone, Debug, Default, PartialEq)]
struct SeenRequest {
    method: String,
    context_subject_id: String,
    provider_name: String,
    session_id: String,
    turn_id: String,
    interaction_id: String,
    reason: String,
    tool_ref_operation: String,
    listed_tool_mcp_name: String,
}

#[derive(Clone, Default)]
struct TestAgentServer {
    seen: Arc<Mutex<Vec<SeenRequest>>>,
    relay_tokens: Arc<Mutex<Vec<String>>>,
    create_turn_requests: Arc<Mutex<Vec<CreateAgentProviderTurnRequest>>>,
}

#[async_trait]
impl ProtoAgentProvider for TestAgentServer {
    async fn create_session(
        &self,
        request: GrpcRequest<CreateAgentProviderSessionRequest>,
    ) -> std::result::Result<GrpcResponse<AgentSession>, Status> {
        maybe_record_relay_token(&self.relay_tokens, &request);
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "create_session".to_string(),
            context_subject_id: context_subject_id(&request.context),
            provider_name: request.provider_name.clone(),
            session_id: String::new(),
            turn_id: String::new(),
            interaction_id: String::new(),
            reason: String::new(),
            tool_ref_operation: request
                .tools
                .as_ref()
                .and_then(|tools| tools.source.as_ref())
                .and_then(|source| match source {
                    generated::v1::agent_tool_config::Source::Catalog(catalog) => catalog
                        .refs
                        .first()
                        .map(|tool_ref| tool_ref.operation.clone()),
                    generated::v1::agent_tool_config::Source::None(_) => None,
                })
                .unwrap_or_default(),
            listed_tool_mcp_name: request
                .tools
                .as_ref()
                .and_then(|tools| tools.source.as_ref())
                .and_then(|source| match source {
                    generated::v1::agent_tool_config::Source::Catalog(catalog) => {
                        catalog.tools.first().map(|tool| tool.mcp_name.clone())
                    }
                    generated::v1::agent_tool_config::Source::None(_) => None,
                })
                .unwrap_or_default(),
        });
        Ok(GrpcResponse::new(AgentSession {
            id: "session-managed-1".to_string(),
            provider_name: request.provider_name,
            model: request.model,
            client_ref: request.client_ref,
            state: ProtoAgentSessionState::Active as i32,
            metadata: request.metadata,
            created_at: Some(helpers::timestamp_now()),
            updated_at: Some(helpers::timestamp_now()),
            ..Default::default()
        }))
    }

    async fn get_session(
        &self,
        request: GrpcRequest<GetAgentProviderSessionRequest>,
    ) -> std::result::Result<GrpcResponse<AgentSession>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "get_session".to_string(),
            context_subject_id: context_subject_id(&request.context),
            provider_name: String::new(),
            session_id: request.session_id.clone(),
            turn_id: String::new(),
            interaction_id: String::new(),
            reason: String::new(),
            ..Default::default()
        });
        Ok(GrpcResponse::new(AgentSession {
            id: request.session_id,
            provider_name: "openai".to_string(),
            model: "gpt-5.1".to_string(),
            client_ref: "cli-session-1".to_string(),
            state: ProtoAgentSessionState::Archived as i32,
            created_at: Some(helpers::timestamp_now()),
            updated_at: Some(helpers::timestamp_now()),
            ..Default::default()
        }))
    }

    async fn list_sessions(
        &self,
        request: GrpcRequest<ListAgentProviderSessionsRequest>,
    ) -> std::result::Result<GrpcResponse<ListAgentProviderSessionsResponse>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "list_sessions".to_string(),
            context_subject_id: context_subject_id(&request.context),
            provider_name: request.provider_name,
            session_id: String::new(),
            turn_id: String::new(),
            interaction_id: String::new(),
            reason: String::new(),
            ..Default::default()
        });
        Ok(GrpcResponse::new(ListAgentProviderSessionsResponse {
            sessions: vec![AgentSession {
                id: "session-managed-1".to_string(),
                provider_name: "openai".to_string(),
                model: "gpt-5.1".to_string(),
                client_ref: "cli-session-1".to_string(),
                state: ProtoAgentSessionState::Active as i32,
                created_at: Some(helpers::timestamp_now()),
                updated_at: Some(helpers::timestamp_now()),
                ..Default::default()
            }],
        }))
    }

    async fn update_session(
        &self,
        request: GrpcRequest<UpdateAgentProviderSessionRequest>,
    ) -> std::result::Result<GrpcResponse<AgentSession>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "update_session".to_string(),
            context_subject_id: context_subject_id(&request.context),
            provider_name: String::new(),
            session_id: request.session_id.clone(),
            turn_id: String::new(),
            interaction_id: String::new(),
            reason: String::new(),
            ..Default::default()
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
        request: GrpcRequest<CreateAgentProviderTurnRequest>,
    ) -> std::result::Result<GrpcResponse<AgentTurn>, Status> {
        let request = request.into_inner();
        self.create_turn_requests
            .lock()
            .expect("lock create turn requests")
            .push(request.clone());
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "create_turn".to_string(),
            context_subject_id: context_subject_id(&request.context),
            provider_name: String::new(),
            session_id: request.session_id.clone(),
            turn_id: String::new(),
            interaction_id: String::new(),
            reason: String::new(),
            ..Default::default()
        });
        Ok(GrpcResponse::new(AgentTurn {
            id: "turn-managed-1".to_string(),
            session_id: request.session_id,
            provider_name: "openai".to_string(),
            model: request.model,
            status: AgentExecutionStatus::WaitingForInput as i32,
            messages: request.messages,
            output: Some(generated::v1::agent_turn::Output::Text(
                AgentTurnTextOutput {
                    text: "echo:Summarize this".to_string(),
                },
            )),
            status_message: "waiting for input".to_string(),
            created_at: Some(helpers::timestamp_now()),
            started_at: Some(helpers::timestamp_now()),
            ..Default::default()
        }))
    }

    async fn get_turn(
        &self,
        request: GrpcRequest<GetAgentProviderTurnRequest>,
    ) -> std::result::Result<GrpcResponse<AgentTurn>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "get_turn".to_string(),
            context_subject_id: context_subject_id(&request.context),
            provider_name: String::new(),
            session_id: String::new(),
            turn_id: request.turn_id.clone(),
            interaction_id: String::new(),
            reason: String::new(),
            ..Default::default()
        });
        Ok(GrpcResponse::new(AgentTurn {
            id: request.turn_id,
            session_id: "session-managed-1".to_string(),
            provider_name: "openai".to_string(),
            model: "gpt-5.1".to_string(),
            status: AgentExecutionStatus::Succeeded as i32,
            output: Some(generated::v1::agent_turn::Output::Text(
                AgentTurnTextOutput {
                    text: "done".to_string(),
                },
            )),
            status_message: "completed".to_string(),
            created_at: Some(helpers::timestamp_now()),
            started_at: Some(helpers::timestamp_now()),
            completed_at: Some(helpers::timestamp_now()),
            ..Default::default()
        }))
    }

    async fn list_turns(
        &self,
        request: GrpcRequest<ListAgentProviderTurnsRequest>,
    ) -> std::result::Result<GrpcResponse<ListAgentProviderTurnsResponse>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "list_turns".to_string(),
            context_subject_id: context_subject_id(&request.context),
            provider_name: String::new(),
            session_id: request.session_id.clone(),
            turn_id: String::new(),
            interaction_id: String::new(),
            reason: String::new(),
            ..Default::default()
        });
        Ok(GrpcResponse::new(ListAgentProviderTurnsResponse {
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
        request: GrpcRequest<CancelAgentProviderTurnRequest>,
    ) -> std::result::Result<GrpcResponse<AgentTurn>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "cancel_turn".to_string(),
            context_subject_id: context_subject_id(&request.context),
            provider_name: String::new(),
            session_id: String::new(),
            turn_id: request.turn_id.clone(),
            interaction_id: String::new(),
            reason: request.reason.clone(),
            ..Default::default()
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
        request: GrpcRequest<ListAgentProviderTurnEventsRequest>,
    ) -> std::result::Result<GrpcResponse<ListAgentProviderTurnEventsResponse>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "list_turn_events".to_string(),
            context_subject_id: context_subject_id(&request.context),
            provider_name: String::new(),
            session_id: String::new(),
            turn_id: request.turn_id.clone(),
            interaction_id: String::new(),
            reason: String::new(),
            ..Default::default()
        });
        Ok(GrpcResponse::new(ListAgentProviderTurnEventsResponse {
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
        request: GrpcRequest<ListAgentProviderInteractionsRequest>,
    ) -> std::result::Result<GrpcResponse<ListAgentProviderInteractionsResponse>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "list_interactions".to_string(),
            context_subject_id: context_subject_id(&request.context),
            provider_name: String::new(),
            session_id: String::new(),
            turn_id: request.turn_id.clone(),
            interaction_id: String::new(),
            reason: String::new(),
            ..Default::default()
        });
        Ok(GrpcResponse::new(ListAgentProviderInteractionsResponse {
            interactions: vec![AgentInteraction {
                id: "interaction-1".to_string(),
                turn_id: request.turn_id,
                session_id: "session-managed-1".to_string(),
                r#type: AgentInteractionType::Approval as i32,
                state: ProtoAgentInteractionState::Pending as i32,
                title: "Approve command".to_string(),
                prompt: "Run git status?".to_string(),
                created_at: Some(helpers::timestamp_now()),
                ..Default::default()
            }],
        }))
    }

    async fn get_interaction(
        &self,
        request: GrpcRequest<GetAgentProviderInteractionRequest>,
    ) -> std::result::Result<GrpcResponse<AgentInteraction>, Status> {
        let request = request.into_inner();
        Ok(GrpcResponse::new(AgentInteraction {
            id: request.interaction_id,
            turn_id: "turn-managed-1".to_string(),
            session_id: "session-managed-1".to_string(),
            r#type: AgentInteractionType::Approval as i32,
            state: ProtoAgentInteractionState::Pending as i32,
            title: "Approve command".to_string(),
            prompt: "Run git status?".to_string(),
            created_at: Some(helpers::timestamp_now()),
            ..Default::default()
        }))
    }

    async fn resolve_interaction(
        &self,
        request: GrpcRequest<ResolveAgentProviderInteractionRequest>,
    ) -> std::result::Result<GrpcResponse<AgentInteraction>, Status> {
        let request = request.into_inner();
        self.seen.lock().expect("lock seen").push(SeenRequest {
            method: "resolve_interaction".to_string(),
            context_subject_id: context_subject_id(&request.context),
            provider_name: String::new(),
            session_id: String::new(),
            turn_id: request.turn_id.clone(),
            interaction_id: request.interaction_id.clone(),
            reason: String::new(),
            ..Default::default()
        });
        Ok(GrpcResponse::new(AgentInteraction {
            id: request.interaction_id,
            turn_id: request.turn_id,
            session_id: "session-managed-1".to_string(),
            r#type: AgentInteractionType::Approval as i32,
            state: ProtoAgentInteractionState::Resolved as i32,
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
        _request: GrpcRequest<GetAgentProviderCapabilitiesRequest>,
    ) -> std::result::Result<GrpcResponse<AgentProviderCapabilities>, Status> {
        Ok(GrpcResponse::new(AgentProviderCapabilities::default()))
    }
}

fn catalog_tool_config() -> NativeAgentToolConfig {
    NativeAgentToolConfig {
        source: Some(NativeAgentToolConfigSource::Catalog(
            AgentCatalogToolConfig {
                refs: vec![NativeAgentToolRef {
                    app: "slack".to_string(),
                    operation: "chat.postMessage".to_string(),
                    ..Default::default()
                }],
                tools: vec![NativeListedAgentTool {
                    id: "tool-slack".to_string(),
                    mcp_name: "slack__chat_post_message".to_string(),
                    title: "Send Slack message".to_string(),
                    description: "Post a Slack message".to_string(),
                    input_schema: r#"{"type":"object"}"#.to_string(),
                    r#ref: Some(NativeAgentToolRef {
                        app: "slack".to_string(),
                        operation: "chat.postMessage".to_string(),
                        ..Default::default()
                    }),
                    ..Default::default()
                }],
            },
        )),
    }
}

#[tokio::test]
async fn agent_connects_over_tcp_and_sends_relay_token() {
    let _env_lock = helpers::env_lock().lock().await;

    let listener = TcpListener::bind("127.0.0.1:0")
        .await
        .expect("bind tcp listener");
    let address = listener.local_addr().expect("local addr");
    let _socket_guard =
        helpers::EnvGuard::set(gestalt::ENV_HOST_SERVICE_SOCKET, format!("tcp://{address}"));
    let _token_guard = helpers::EnvGuard::set(gestalt::ENV_HOST_SERVICE_TOKEN, "relay-token-rust");

    let server = TestAgentServer::default();
    let serve_server = server.clone();
    let serve_task = tokio::spawn(async move {
        serve_agent_tcp(serve_server, listener)
            .await
            .expect("serve agent");
    });

    let mut agent = Agent::connect()
        .await
        .expect("connect agent")
        .with_context(request_context("user:agent-access"));
    let created = agent
        .create_session(
            String::new(),
            "gpt-5.1".to_string(),
            AgentCreateSessionOptions {
                provider_name: "openai".to_string(),
                client_ref: "cli-session-1".to_string(),
                tools: Some(catalog_tool_config()),
                ..Default::default()
            },
        )
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
async fn agent_connects_over_unix_socket_and_sends_context_subject_id() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("g-rust-agent.sock");
    let _socket_guard =
        helpers::EnvGuard::set(gestalt::ENV_HOST_SERVICE_SOCKET, socket.as_os_str());

    let server = TestAgentServer::default();
    let serve_server = server.clone();
    let serve_socket = socket.clone();
    let serve_task = tokio::spawn(async move {
        serve_agent(serve_server, &serve_socket)
            .await
            .expect("serve agent");
    });

    helpers::wait_for_socket(&socket).await;

    let mut agent = Agent::connect()
        .await
        .expect("connect agent")
        .with_context(request_context("user:agent-access"));
    let created_session = agent
        .create_session(
            String::new(),
            "gpt-5.1".to_string(),
            AgentCreateSessionOptions {
                provider_name: "openai".to_string(),
                client_ref: "cli-session-1".to_string(),
                tools: Some(catalog_tool_config()),
                ..Default::default()
            },
        )
        .await
        .expect("create session");
    let fetched_session = agent
        .get_session(
            "session-managed-1".to_string(),
            AgentGetSessionOptions {
                provider_name: "openai".to_string(),
            },
        )
        .await
        .expect("get session");
    let listed_sessions = agent
        .list_sessions(AgentListSessionsOptions {
            provider_name: "openai".to_string(),
            ..Default::default()
        })
        .await
        .expect("list sessions");
    let updated_session = agent
        .update_session(
            "session-managed-1".to_string(),
            AgentUpdateSessionOptions {
                client_ref: "cli-session-2".to_string(),
                state: agent_session_state::AGENT_SESSION_STATE_ARCHIVED,
                provider_name: "openai".to_string(),
                metadata: None,
            },
        )
        .await
        .expect("update session");
    let created_turn = agent
        .create_turn(
            "session-managed-1".to_string(),
            String::new(),
            "gpt-5.1".to_string(),
            vec![AgentMessage {
                role: "user".to_string(),
                text: "Summarize this".to_string(),
                parts: vec![AgentMessagePart {
                    r#type: agent_message_part_type::AGENT_MESSAGE_PART_TYPE_TEXT,
                    text: "Summarize this".to_string(),
                    ..Default::default()
                }],
                ..Default::default()
            }],
            AgentCreateTurnOptions {
                timeout_seconds: 120,
                provider_name: "openai".to_string(),
                output: Some(AgentOutput {
                    kind: Some(AgentOutputKind::Text(AgentTextOutput {})),
                }),
                ..Default::default()
            },
        )
        .await
        .expect("create turn");
    let fetched_turn = agent
        .get_turn(
            "turn-managed-1".to_string(),
            AgentGetTurnOptions {
                provider_name: "openai".to_string(),
            },
        )
        .await
        .expect("get turn");
    let listed_turns = agent
        .list_turns(
            "session-managed-1".to_string(),
            AgentListTurnsOptions {
                provider_name: "openai".to_string(),
                ..Default::default()
            },
        )
        .await
        .expect("list turns");
    let canceled_turn = agent
        .cancel_turn(
            "turn-managed-1".to_string(),
            AgentCancelTurnOptions {
                reason: "user canceled".to_string(),
                provider_name: "openai".to_string(),
            },
        )
        .await
        .expect("cancel turn");
    let turn_events = agent
        .list_turn_events(
            "turn-managed-1".to_string(),
            AgentListTurnEventsOptions {
                after_seq: 0,
                limit: 10,
                provider_name: "openai".to_string(),
            },
        )
        .await
        .expect("list turn events");
    let interactions = agent
        .list_interactions(
            "turn-managed-1".to_string(),
            AgentListInteractionsOptions {
                provider_name: "openai".to_string(),
            },
        )
        .await
        .expect("list interactions");
    let resolution = serde_json::json!({ "approved": true });
    let resolved = agent
        .resolve_interaction(
            "interaction-1".to_string(),
            Some(resolution.as_object().expect("resolution object").clone()),
            AgentResolveInteractionOptions {
                turn_id: "turn-managed-1".to_string(),
                provider_name: "openai".to_string(),
            },
        )
        .await
        .expect("resolve interaction");

    assert_eq!(created_session.id, "session-managed-1");
    assert_eq!(fetched_session.id, "session-managed-1");
    assert_eq!(listed_sessions.len(), 1);
    assert_eq!(updated_session.client_ref, "cli-session-2");
    assert_eq!(created_turn.id, "turn-managed-1");
    assert_eq!(created_turn.messages[0].parts.len(), 1);
    assert_eq!(fetched_turn.id, "turn-managed-1");
    assert_eq!(listed_turns.len(), 1);
    assert_eq!(canceled_turn.status_message, "user canceled");
    assert_eq!(turn_events.len(), 1);
    assert_eq!(interactions.len(), 1);
    assert_eq!(resolved.id, "interaction-1");
    assert_eq!(
        resolved.state,
        agent_interaction_state::AGENT_INTERACTION_STATE_RESOLVED
    );

    let seen = server.seen.lock().expect("lock seen").clone();
    assert_eq!(
        seen,
        vec![
            SeenRequest {
                method: "create_session".to_string(),
                context_subject_id: "user:agent-access".to_string(),
                provider_name: "openai".to_string(),
                session_id: String::new(),
                turn_id: String::new(),
                interaction_id: String::new(),
                reason: String::new(),
                tool_ref_operation: "chat.postMessage".to_string(),
                listed_tool_mcp_name: "slack__chat_post_message".to_string(),
            },
            SeenRequest {
                method: "get_session".to_string(),
                context_subject_id: "user:agent-access".to_string(),
                provider_name: String::new(),
                session_id: "session-managed-1".to_string(),
                turn_id: String::new(),
                interaction_id: String::new(),
                reason: String::new(),
                ..Default::default()
            },
            SeenRequest {
                method: "list_sessions".to_string(),
                context_subject_id: "user:agent-access".to_string(),
                provider_name: "openai".to_string(),
                session_id: String::new(),
                turn_id: String::new(),
                interaction_id: String::new(),
                reason: String::new(),
                ..Default::default()
            },
            SeenRequest {
                method: "update_session".to_string(),
                context_subject_id: "user:agent-access".to_string(),
                provider_name: String::new(),
                session_id: "session-managed-1".to_string(),
                turn_id: String::new(),
                interaction_id: String::new(),
                reason: String::new(),
                ..Default::default()
            },
            SeenRequest {
                method: "create_turn".to_string(),
                context_subject_id: "user:agent-access".to_string(),
                provider_name: String::new(),
                session_id: "session-managed-1".to_string(),
                turn_id: String::new(),
                interaction_id: String::new(),
                reason: String::new(),
                ..Default::default()
            },
            SeenRequest {
                method: "get_turn".to_string(),
                context_subject_id: "user:agent-access".to_string(),
                provider_name: String::new(),
                session_id: String::new(),
                turn_id: "turn-managed-1".to_string(),
                interaction_id: String::new(),
                reason: String::new(),
                ..Default::default()
            },
            SeenRequest {
                method: "list_turns".to_string(),
                context_subject_id: "user:agent-access".to_string(),
                provider_name: String::new(),
                session_id: "session-managed-1".to_string(),
                turn_id: String::new(),
                interaction_id: String::new(),
                reason: String::new(),
                ..Default::default()
            },
            SeenRequest {
                method: "cancel_turn".to_string(),
                context_subject_id: "user:agent-access".to_string(),
                provider_name: String::new(),
                session_id: String::new(),
                turn_id: "turn-managed-1".to_string(),
                interaction_id: String::new(),
                reason: "user canceled".to_string(),
                ..Default::default()
            },
            SeenRequest {
                method: "list_turn_events".to_string(),
                context_subject_id: "user:agent-access".to_string(),
                provider_name: String::new(),
                session_id: String::new(),
                turn_id: "turn-managed-1".to_string(),
                interaction_id: String::new(),
                reason: String::new(),
                ..Default::default()
            },
            SeenRequest {
                method: "list_interactions".to_string(),
                context_subject_id: "user:agent-access".to_string(),
                provider_name: String::new(),
                session_id: String::new(),
                turn_id: "turn-managed-1".to_string(),
                interaction_id: String::new(),
                reason: String::new(),
                ..Default::default()
            },
            SeenRequest {
                method: "resolve_interaction".to_string(),
                context_subject_id: "user:agent-access".to_string(),
                provider_name: String::new(),
                session_id: String::new(),
                turn_id: "turn-managed-1".to_string(),
                interaction_id: "interaction-1".to_string(),
                reason: String::new(),
                ..Default::default()
            },
        ]
    );

    serve_task.abort();
    let _ = serve_task.await;
}

#[tokio::test]
async fn agent_create_turn_accepts_native_values() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("g-rust-agent-native.sock");
    let _socket_guard =
        helpers::EnvGuard::set(gestalt::ENV_HOST_SERVICE_SOCKET, socket.as_os_str());

    let server = TestAgentServer::default();
    let serve_server = server.clone();
    let serve_socket = socket.clone();
    let serve_task = tokio::spawn(async move {
        serve_agent(serve_server, &serve_socket)
            .await
            .expect("serve agent");
    });

    helpers::wait_for_socket(&socket).await;

    let mut agent = Agent::connect()
        .await
        .expect("connect agent")
        .with_context(request_context("user:agent-access"));
    let message_metadata = serde_json::json!({ "source": "native" });
    let turn_metadata = serde_json::json!({ "request": "native" });
    let model_options = serde_json::json!({ "temperature": 0 });
    let schema = serde_json::json!({ "type": "object" });
    let created_turn = agent
        .create_turn(
            "session-managed-1".to_string(),
            String::new(),
            "gpt-5.1".to_string(),
            vec![AgentMessage {
                role: "user".to_string(),
                text: "Summarize this".to_string(),
                parts: vec![AgentMessagePart {
                    r#type: agent_message_part_type::AGENT_MESSAGE_PART_TYPE_TEXT,
                    text: "Summarize this".to_string(),
                    ..Default::default()
                }],
                metadata: Some(
                    message_metadata
                        .as_object()
                        .expect("message metadata object")
                        .clone(),
                ),
            }],
            AgentCreateTurnOptions {
                timeout_seconds: 120,
                provider_name: "openai".to_string(),
                metadata: Some(
                    turn_metadata
                        .as_object()
                        .expect("turn metadata object")
                        .clone(),
                ),
                model_options: Some(
                    model_options
                        .as_object()
                        .expect("model options object")
                        .clone(),
                ),
                output: Some(AgentOutput {
                    kind: Some(AgentOutputKind::Structured(AgentStructuredOutput {
                        schema: Some(schema.as_object().expect("schema object").clone()),
                    })),
                }),
                ..Default::default()
            },
        )
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
    assert_eq!(
        context_subject_id(&request.context),
        "user:agent-access".to_string()
    );
    assert_eq!(request.session_id, "session-managed-1");
    assert_eq!(request.model, "gpt-5.1");
    assert_eq!(request.messages.len(), 1);
    assert_eq!(request.messages[0].role, "user");
    assert_eq!(request.messages[0].text, "Summarize this");
    assert_eq!(
        support_protocol::json_from_struct(request.messages[0].metadata.as_ref().unwrap()),
        serde_json::json!({ "source": "native" })
    );
    assert_eq!(request.messages[0].parts.len(), 1);
    assert_eq!(
        request.messages[0].parts[0].r#type,
        AgentMessagePartType::Text as i32
    );
    assert_eq!(request.messages[0].parts[0].text, "Summarize this");
    let output = request.output.as_ref().expect("output");
    let generated::v1::agent_output::Kind::Structured(output) =
        output.kind.as_ref().expect("output kind")
    else {
        panic!("output = {output:?}, want structured");
    };
    assert_eq!(
        support_protocol::json_from_struct(output.schema.as_ref().unwrap()),
        serde_json::json!({ "type": "object" })
    );
    assert_eq!(
        support_protocol::json_from_struct(request.metadata.as_ref().unwrap()),
        serde_json::json!({ "request": "native" })
    );
    assert_eq!(
        support_protocol::json_from_struct(request.model_options.as_ref().unwrap()),
        serde_json::json!({ "temperature": 0.0 })
    );

    serve_task.abort();
    let _ = serve_task.await;
}

#[tokio::test]
async fn get_session_raw_injects_default_context_when_unset() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("g-rust-req-agent.sock");
    let _socket_guard =
        helpers::EnvGuard::set(gestalt::ENV_HOST_SERVICE_SOCKET, socket.as_os_str());

    let server = TestAgentServer::default();
    let serve_server = server.clone();
    let serve_socket = socket.clone();
    let serve_task = tokio::spawn(async move {
        serve_agent(serve_server, &serve_socket)
            .await
            .expect("serve agent");
    });

    helpers::wait_for_socket(&socket).await;

    let mut agent = Agent::connect()
        .await
        .expect("connect agent")
        .with_context(request_context("user:request-agent"));
    let response = agent
        .get_session_raw(NativeGetSessionRequest {
            session_id: "session-managed-1".to_string(),
            provider_name: "openai".to_string(),
            ..Default::default()
        })
        .await
        .expect("get session");

    assert_eq!(response.id, "session-managed-1");

    let seen = server.seen.lock().expect("lock seen").clone();
    assert_eq!(seen.len(), 1);
    assert_eq!(seen[0].context_subject_id, "user:request-agent");
    assert_eq!(seen[0].method, "get_session");

    serve_task.abort();
    let _ = serve_task.await;
}

async fn serve_agent(
    server: TestAgentServer,
    socket: &Path,
) -> std::result::Result<(), tonic::transport::Error> {
    let _ = std::fs::remove_file(socket);
    let listener = UnixListener::bind(socket).expect("bind unix listener");

    Server::builder()
        .add_service(AgentServer::new(server))
        .serve_with_incoming(UnixListenerStream::new(listener))
        .await
}

async fn serve_agent_tcp(
    server: TestAgentServer,
    listener: TcpListener,
) -> std::result::Result<(), tonic::transport::Error> {
    Server::builder()
        .add_service(AgentServer::new(server))
        .serve_with_incoming(TcpListenerStream::new(listener))
        .await
}

fn maybe_record_relay_token(
    relay_tokens: &Arc<Mutex<Vec<String>>>,
    request: &GrpcRequest<CreateAgentProviderSessionRequest>,
) {
    if let Some(token) = request.metadata().get("x-gestalt-host-service-relay-token") {
        relay_tokens
            .lock()
            .expect("lock relay tokens")
            .push(token.to_str().expect("relay token ascii").to_string());
    }
}

fn request_context(subject_id: &str) -> RequestContext {
    RequestContext {
        subject: Some(SubjectContext {
            id: subject_id.to_string(),
            ..Default::default()
        }),
        ..Default::default()
    }
}

fn context_subject_id(context: &Option<ProviderRequestContext>) -> String {
    context
        .as_ref()
        .and_then(|context| context.subject.as_ref())
        .map(|subject| subject.id.clone())
        .unwrap_or_default()
}
