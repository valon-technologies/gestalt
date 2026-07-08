#[path = "../src/generated.rs"]
mod generated;

#[allow(dead_code)]
mod helpers;

use std::sync::{Arc, Mutex};
use std::time::SystemTime;

use generated::v1::agent_client::AgentClient;
use generated::v1::provider_lifecycle_client::ProviderLifecycleClient;
use generated::v1::{self as pb, ConfigureProviderRequest, ProviderKind};
use gestalt::proto::v1 as sdk_pb;
use gestalt::{
    AgentExecutionStatus, AgentInteraction, AgentInteractionState, AgentInteractionType,
    AgentMessagePartType, AgentProvider, AgentProviderCapabilities, AgentSession,
    AgentSessionState, AgentToolConfigSource, AgentToolSourceMode, AgentTurn, AgentTurnEvent,
    AgentTurnOutput, AgentTurnTextOutput, CancelAgentProviderTurnRequest,
    CreateAgentProviderSessionRequest, CreateAgentProviderTurnRequest,
    GetAgentProviderCapabilitiesRequest, GetAgentProviderInteractionRequest,
    GetAgentProviderSessionRequest, GetAgentProviderTurnRequest,
    ListAgentProviderInteractionsRequest, ListAgentProviderInteractionsResponse,
    ListAgentProviderSessionsRequest, ListAgentProviderSessionsResponse,
    ListAgentProviderTurnEventsRequest, ListAgentProviderTurnEventsResponse,
    ListAgentProviderTurnsRequest, ListAgentProviderTurnsResponse,
    ResolveAgentProviderInteractionRequest, RuntimeMetadata, UpdateAgentProviderSessionRequest,
};
use hyper_util::rt::tokio::TokioIo;
use tokio::net::UnixStream;
use tonic::transport::Endpoint;
use tower::service_fn;

#[derive(Default)]
struct TestAgentProvider {
    configured_name: Mutex<String>,
    session_context_subjects: Mutex<Vec<String>>,
    turn_context_subjects: Mutex<Vec<String>>,
    turn_request_context_subjects: Mutex<Vec<String>>,
    session_tools: Mutex<Vec<SessionTools>>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct SessionTools {
    tool_ref_operation: String,
    listed_tool_mcp_name: String,
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

    async fn create_session(
        &self,
        request: CreateAgentProviderSessionRequest,
    ) -> gestalt::Result<AgentSession> {
        self.session_context_subjects
            .lock()
            .expect("session_context_subjects lock")
            .push(request_context_subject_id(
                gestalt::current_request_context().as_ref(),
            ));
        self.session_tools.lock().expect("session_tools lock").push(
            match request.tools.and_then(|tools| tools.source) {
                Some(AgentToolConfigSource::Catalog(catalog)) => SessionTools {
                    tool_ref_operation: catalog
                        .refs
                        .first()
                        .map(|tool_ref| tool_ref.operation.clone())
                        .unwrap_or_default(),
                    listed_tool_mcp_name: catalog
                        .tools
                        .first()
                        .map(|tool| tool.mcp_name.clone())
                        .unwrap_or_default(),
                },
                Some(AgentToolConfigSource::None(_)) | None => SessionTools {
                    tool_ref_operation: String::new(),
                    listed_tool_mcp_name: String::new(),
                },
            },
        );
        Ok(AgentSession {
            id: "session-1".to_string(),
            provider_name: configured_name(self),
            model: request.model,
            client_ref: request.client_ref,
            state: AgentSessionState::Active,
            metadata: request.metadata,
            created_by_subject_id: {
                let id = request_context_subject_id(gestalt::current_request_context().as_ref());
                if id.trim().is_empty() { None } else { Some(id) }
            },
            created_at: Some(SystemTime::now()),
            updated_at: Some(SystemTime::now()),
            ..Default::default()
        })
    }

    async fn get_session(
        &self,
        request: GetAgentProviderSessionRequest,
    ) -> gestalt::Result<AgentSession> {
        Ok(AgentSession {
            id: request.session_id,
            provider_name: configured_name(self),
            model: "gpt-5.1".to_string(),
            client_ref: "cli-session-1".to_string(),
            state: AgentSessionState::Archived,
            metadata: Some(serde_json::json!({
                "source": "rust-test"
            })),
            created_at: Some(SystemTime::now()),
            updated_at: Some(SystemTime::now()),
            last_turn_at: Some(SystemTime::now()),
            ..Default::default()
        })
    }

    async fn list_sessions(
        &self,
        _request: ListAgentProviderSessionsRequest,
    ) -> gestalt::Result<ListAgentProviderSessionsResponse> {
        Ok(ListAgentProviderSessionsResponse {
            sessions: vec![AgentSession {
                id: "session-1".to_string(),
                provider_name: configured_name(self),
                model: "gpt-5.1".to_string(),
                client_ref: "cli-session-1".to_string(),
                state: AgentSessionState::Archived,
                created_at: Some(SystemTime::now()),
                updated_at: Some(SystemTime::now()),
                ..Default::default()
            }],
        })
    }

    async fn update_session(
        &self,
        request: UpdateAgentProviderSessionRequest,
    ) -> gestalt::Result<AgentSession> {
        Ok(AgentSession {
            id: request.session_id,
            provider_name: configured_name(self),
            model: "gpt-5.1".to_string(),
            client_ref: request.client_ref,
            state: request.state,
            metadata: request.metadata,
            created_at: Some(SystemTime::now()),
            updated_at: Some(SystemTime::now()),
            ..Default::default()
        })
    }

    async fn create_turn(
        &self,
        request: CreateAgentProviderTurnRequest,
    ) -> gestalt::Result<AgentTurn> {
        self.turn_context_subjects
            .lock()
            .expect("turn_context_subjects lock")
            .push(request_context_subject_id(
                gestalt::current_request_context().as_ref(),
            ));
        self.turn_request_context_subjects
            .lock()
            .expect("turn_request_context_subjects lock")
            .push(request_context_subject_id(request.context.as_ref()));
        Ok(AgentTurn {
            id: request.turn_id,
            session_id: request.session_id,
            provider_name: configured_name(self),
            model: request.model,
            status: AgentExecutionStatus::WaitingForInput,
            messages: request.messages,
            output: Some(AgentTurnOutput::Text(AgentTurnTextOutput {
                text: "echo:Plan it".to_string(),
            })),
            status_message: "waiting for input".to_string(),
            created_by_subject_id: {
                let id = request_context_subject_id(request.context.as_ref());
                if id.trim().is_empty() { None } else { Some(id) }
            },
            created_at: Some(SystemTime::now()),
            started_at: Some(SystemTime::now()),
            execution_ref: request.execution_ref,
            ..Default::default()
        })
    }

    async fn get_turn(&self, request: GetAgentProviderTurnRequest) -> gestalt::Result<AgentTurn> {
        Ok(AgentTurn {
            id: request.turn_id,
            session_id: "session-1".to_string(),
            provider_name: configured_name(self),
            model: "gpt-5.1".to_string(),
            status: AgentExecutionStatus::WaitingForInput,
            output: Some(AgentTurnOutput::Text(AgentTurnTextOutput {
                text: "echo:Plan it".to_string(),
            })),
            status_message: "waiting for input".to_string(),
            created_at: Some(SystemTime::now()),
            started_at: Some(SystemTime::now()),
            ..Default::default()
        })
    }

    async fn list_turns(
        &self,
        request: ListAgentProviderTurnsRequest,
    ) -> gestalt::Result<ListAgentProviderTurnsResponse> {
        Ok(ListAgentProviderTurnsResponse {
            turns: vec![AgentTurn {
                id: "turn-1".to_string(),
                session_id: request.session_id,
                provider_name: configured_name(self),
                model: "gpt-5.1".to_string(),
                status: AgentExecutionStatus::Succeeded,
                status_message: "done".to_string(),
                created_at: Some(SystemTime::now()),
                started_at: Some(SystemTime::now()),
                completed_at: Some(SystemTime::now()),
                ..Default::default()
            }],
        })
    }

    async fn cancel_turn(
        &self,
        request: CancelAgentProviderTurnRequest,
    ) -> gestalt::Result<AgentTurn> {
        Ok(AgentTurn {
            id: request.turn_id,
            session_id: "session-1".to_string(),
            provider_name: configured_name(self),
            model: "gpt-5.1".to_string(),
            status: AgentExecutionStatus::Canceled,
            status_message: request.reason,
            created_at: Some(SystemTime::now()),
            started_at: Some(SystemTime::now()),
            completed_at: Some(SystemTime::now()),
            ..Default::default()
        })
    }

    async fn list_turn_events(
        &self,
        request: ListAgentProviderTurnEventsRequest,
    ) -> gestalt::Result<ListAgentProviderTurnEventsResponse> {
        let provider_name = configured_name(self);
        Ok(ListAgentProviderTurnEventsResponse {
            events: vec![
                AgentTurnEvent {
                    id: format!("{}-event-1", request.turn_id),
                    turn_id: request.turn_id.clone(),
                    seq: 1,
                    r#type: "turn.started".to_string(),
                    source: provider_name.clone(),
                    visibility: "private".to_string(),
                    created_at: Some(SystemTime::now()),
                    ..Default::default()
                },
                AgentTurnEvent {
                    id: format!("{}-event-2", request.turn_id),
                    turn_id: request.turn_id,
                    seq: 2,
                    r#type: "interaction.requested".to_string(),
                    source: provider_name,
                    visibility: "private".to_string(),
                    created_at: Some(SystemTime::now()),
                    ..Default::default()
                },
            ],
        })
    }

    async fn get_interaction(
        &self,
        request: GetAgentProviderInteractionRequest,
    ) -> gestalt::Result<AgentInteraction> {
        Ok(AgentInteraction {
            id: request.interaction_id,
            turn_id: "turn-1".to_string(),
            session_id: "session-1".to_string(),
            r#type: AgentInteractionType::Approval,
            state: AgentInteractionState::Pending,
            title: "Approve command".to_string(),
            prompt: "Run git status?".to_string(),
            created_at: Some(SystemTime::now()),
            ..Default::default()
        })
    }

    async fn list_interactions(
        &self,
        request: ListAgentProviderInteractionsRequest,
    ) -> gestalt::Result<ListAgentProviderInteractionsResponse> {
        Ok(ListAgentProviderInteractionsResponse {
            interactions: vec![AgentInteraction {
                id: "interaction-1".to_string(),
                turn_id: request.turn_id,
                session_id: "session-1".to_string(),
                r#type: AgentInteractionType::Approval,
                state: AgentInteractionState::Pending,
                title: "Approve command".to_string(),
                prompt: "Run git status?".to_string(),
                created_at: Some(SystemTime::now()),
                ..Default::default()
            }],
        })
    }

    async fn resolve_interaction(
        &self,
        request: ResolveAgentProviderInteractionRequest,
    ) -> gestalt::Result<AgentInteraction> {
        Ok(AgentInteraction {
            id: request.interaction_id,
            turn_id: "turn-1".to_string(),
            session_id: "session-1".to_string(),
            r#type: AgentInteractionType::Approval,
            state: AgentInteractionState::Resolved,
            title: "Approve command".to_string(),
            prompt: "Run git status?".to_string(),
            resolution: request.resolution,
            created_at: Some(SystemTime::now()),
            resolved_at: Some(SystemTime::now()),
            ..Default::default()
        })
    }

    async fn get_capabilities(
        &self,
        _request: GetAgentProviderCapabilitiesRequest,
    ) -> gestalt::Result<AgentProviderCapabilities> {
        Ok(AgentProviderCapabilities {
            streaming_text: true,
            tool_calls: true,
            parallel_tool_calls: false,
            interactions: true,
            resumable_turns: true,
            reasoning_summaries: false,
            supports_session_start: false,
            supports_prepared_workspace: false,
            bounded_list_hydration: true,
            supported_tool_sources: vec![AgentToolSourceMode::Catalog, AgentToolSourceMode::None],
        })
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
        gestalt::runtime_impl::serve_agent_provider(serve_provider)
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

    let mut client = AgentClient::new(channel);
    let session = client
        .create_session(pb::CreateAgentProviderSessionRequest {
            idempotency_key: "session-req-1".to_string(),
            model: "gpt-5.1".to_string(),
            client_ref: "cli-session-1".to_string(),
            metadata: Some(helpers::struct_from_json(serde_json::json!({
                "source": "rust-test"
            }))),
            context: Some(pb::RequestContext {
                subject: Some(pb::SubjectContext {
                    id: "user:session".to_string(),
                    ..Default::default()
                }),
                ..Default::default()
            }),
            tools: Some(pb::AgentToolConfig {
                source: Some(pb::agent_tool_config::Source::Catalog(
                    pb::AgentCatalogToolConfig {
                        refs: vec![pb::AgentToolRef {
                            app: "slack".to_string(),
                            operation: "chat.postMessage".to_string(),
                            ..Default::default()
                        }],
                        tools: vec![pb::ListedAgentTool {
                            id: "tool-slack".to_string(),
                            mcp_name: "slack__chat_post_message".to_string(),
                            title: "Send Slack message".to_string(),
                            description: "Post a Slack message".to_string(),
                            input_schema: r#"{"type":"object"}"#.to_string(),
                            r#ref: Some(pb::AgentToolRef {
                                app: "slack".to_string(),
                                operation: "chat.postMessage".to_string(),
                                ..Default::default()
                            }),
                            ..Default::default()
                        }],
                    },
                )),
            }),
            ..Default::default()
        })
        .await
        .expect("create session")
        .into_inner();
    assert_eq!(session.id, "session-1");
    assert_eq!(
        AgentSessionState::try_from(session.state).expect("valid session state"),
        AgentSessionState::Active
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
        AgentSessionState::try_from(fetched_session.state).expect("valid fetched session state"),
        AgentSessionState::Archived
    );

    let updated_session = client
        .update_session(pb::UpdateAgentProviderSessionRequest {
            session_id: "session-1".to_string(),
            client_ref: "cli-session-2".to_string(),
            state: AgentSessionState::Archived.as_i32(),
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
            timeout_seconds: 120,
            messages: vec![pb::AgentMessage {
                role: "user".to_string(),
                text: "Plan it".to_string(),
                parts: vec![pb::AgentMessagePart {
                    r#type: AgentMessagePartType::Text as i32,
                    text: "Plan it".to_string(),
                    ..Default::default()
                }],
                metadata: Some(helpers::struct_from_json(serde_json::json!({
                    "priority": "high"
                }))),
            }],
            execution_ref: "exec-turn-1".to_string(),
            context: Some(pb::RequestContext {
                subject: Some(pb::SubjectContext {
                    id: "user:turn".to_string(),
                    ..Default::default()
                }),
                ..Default::default()
            }),
            output: Some(pb::AgentOutput {
                kind: Some(pb::agent_output::Kind::Text(pb::AgentTextOutput {})),
            }),
            ..Default::default()
        })
        .await
        .expect("create turn")
        .into_inner();
    assert_eq!(created_turn.id, "turn-1");
    assert_eq!(
        AgentExecutionStatus::try_from(created_turn.status).expect("valid turn status"),
        AgentExecutionStatus::WaitingForInput
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
            .expect("valid interaction state"),
        AgentInteractionState::Pending
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
            .expect("valid resolved interaction state"),
        AgentInteractionState::Resolved
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
        capabilities.supported_tool_sources,
        vec![
            pb::AgentToolSourceMode::Catalog as i32,
            pb::AgentToolSourceMode::None as i32
        ]
    );

    assert_eq!(
        *provider
            .configured_name
            .lock()
            .expect("configured_name lock"),
        "agent-runtime"
    );
    assert_eq!(
        *provider
            .session_context_subjects
            .lock()
            .expect("session_context_subjects lock"),
        vec!["user:session".to_string()]
    );
    assert_eq!(
        *provider.session_tools.lock().expect("session_tools lock"),
        vec![SessionTools {
            tool_ref_operation: "chat.postMessage".to_string(),
            listed_tool_mcp_name: "slack__chat_post_message".to_string(),
        }]
    );
    assert_eq!(
        *provider
            .turn_context_subjects
            .lock()
            .expect("turn_context_subjects lock"),
        vec!["user:turn".to_string()]
    );
    assert_eq!(
        *provider
            .turn_request_context_subjects
            .lock()
            .expect("turn_request_context_subjects lock"),
        vec!["user:turn".to_string()]
    );

    serve_task.abort();
    let _ = serve_task.await;
}

fn configured_name(provider: &TestAgentProvider) -> String {
    provider
        .configured_name
        .lock()
        .expect("configured_name lock")
        .clone()
}

fn request_context_subject_id(context: Option<&sdk_pb::RequestContext>) -> String {
    context
        .and_then(|context| context.subject.as_ref())
        .map(|subject| subject.id.clone())
        .unwrap_or_default()
}
