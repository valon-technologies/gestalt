use anyhow::{Context, Result, bail};
use serde_json::{Map, Value, json};
use std::sync::Arc;
use time::{OffsetDateTime, format_description::well_known::Rfc3339};

use crate::api::ApiClient;
use crate::cli::{
    AgentSessionCreateArgs, AgentSessionUpdateArgs, AgentToolArg, AgentTurnCreateArgs,
    AgentTurnEventListArgs,
};
use crate::output::{self, Format};
use crate::params;

use gestalt_sdk::agent::agent_execution_status::*;
use gestalt_sdk::agent::agent_message_part_type::*;
use gestalt_sdk::agent::agent_session_state::*;
use gestalt_sdk::agent::{
    AgentCatalogToolConfig, AgentExecutionStatus, AgentMessage, AgentMessagePart, AgentOutput,
    AgentSessionState, AgentTextOutput, AgentToolConfig, AgentToolConfigSource,
};
use gestalt_sdk::app::AgentToolRef;
use gestalt_sdk::public::auth::BearerAuth;
use gestalt_sdk::public::generated::app_client::AgentClient;
use gestalt_sdk::public::rest_transport::SyncRestTransport;
use gestalt_sdk::rpc_support::GestaltError;

use super::fields::decode_json;
use super::format::table::{
    print_session, print_sessions, print_turn, print_turn_events, print_turns,
};
use super::render::stdout::render_turn_transcript;
use super::stream::DEFAULT_EVENT_PAGE_SIZE;
use super::types::{AgentInteractionInfo, AgentSessionInfo, AgentTurnEventInfo, AgentTurnInfo};

pub(crate) const DEFAULT_SESSION_LIST_LIMIT: usize = 50;
pub(crate) const INTERRUPT_CANCEL_REASON: &str = "operator interrupted";

fn agent_client(api: &ApiClient) -> Result<AgentClient<SyncRestTransport>> {
    let transport = SyncRestTransport::new(api.base_url(), Arc::new(BearerAuth::new(api.token())))
        .with_timeout(std::time::Duration::from_secs(30));
    Ok(AgentClient::new(transport))
}

fn map_sdk_error(err: GestaltError) -> anyhow::Error {
    err.into()
}

pub fn create_session(
    client: &ApiClient,
    args: &AgentSessionCreateArgs,
    format: Format,
) -> Result<()> {
    let agent = agent_client(client)?;
    let request = build_create_session_request(args)?;
    let resp = agent
        .create_session_sync(request)
        .map_err(map_sdk_error)
        .context("failed to create agent session")?;
    print_session(&serde_json::to_value(&resp)?, format);
    Ok(())
}

pub fn list_sessions(
    client: &ApiClient,
    provider: Option<&str>,
    state: Option<&str>,
    limit: Option<usize>,
    full: bool,
    format: Format,
) -> Result<()> {
    let summary_limit = if full {
        None
    } else {
        let limit = limit.unwrap_or(DEFAULT_SESSION_LIST_LIMIT);
        if limit == 0 {
            bail!("--limit must be greater than 0");
        }
        Some(limit)
    };
    let agent = agent_client(client)?;
    let request = gestalt_sdk::public::generated::agent::ListAgentProviderSessionsRequest {
        provider_name: provider.unwrap_or_default().to_string(),
        state: parse_session_state(state),
        limit: summary_limit.unwrap_or(0) as i32,
        summary_only: summary_limit.is_some(),
        ..Default::default()
    };
    let resp = agent
        .list_sessions_sync(request)
        .map_err(map_sdk_error)
        .context("failed to list agent sessions")?;
    print_sessions(&serde_json::to_value(&resp)?["sessions"], format);
    Ok(())
}

pub fn get_session(client: &ApiClient, id: &str, format: Format) -> Result<()> {
    let agent = agent_client(client)?;
    let request = gestalt_sdk::public::generated::agent::GetAgentProviderSessionRequest {
        session_id: id.to_string(),
        ..Default::default()
    };
    let resp = agent
        .get_session_sync(request)
        .map_err(map_sdk_error)
        .with_context(|| format!("failed to get agent session {id}"))?;
    print_session(&serde_json::to_value(&resp)?, format);
    Ok(())
}

pub fn update_session(
    client: &ApiClient,
    args: &AgentSessionUpdateArgs,
    format: Format,
) -> Result<()> {
    let agent = agent_client(client)?;
    let request = build_update_session_request(args)?;
    let resp = agent
        .update_session_sync(request)
        .map_err(map_sdk_error)
        .with_context(|| format!("failed to update agent session {}", args.id))?;
    print_session(&serde_json::to_value(&resp)?, format);
    Ok(())
}

pub fn create_turn(client: &ApiClient, args: &AgentTurnCreateArgs, format: Format) -> Result<()> {
    let agent = agent_client(client)?;
    let request = build_create_turn_request(args)?;
    let resp = agent
        .create_turn_sync(request)
        .map_err(map_sdk_error)
        .with_context(|| format!("failed to create agent turn in session {}", args.session_id))?;
    print_turn(&serde_json::to_value(&resp)?, format);
    Ok(())
}

pub fn list_turns(
    client: &ApiClient,
    session_id: &str,
    status: Option<&str>,
    format: Format,
) -> Result<()> {
    let agent = agent_client(client)?;
    let request = gestalt_sdk::public::generated::agent::ListAgentProviderTurnsRequest {
        session_id: session_id.to_string(),
        status: parse_execution_status(status),
        ..Default::default()
    };
    let resp = agent
        .list_turns_sync(request)
        .map_err(map_sdk_error)
        .with_context(|| format!("failed to list agent turns for session {session_id}"))?;
    print_turns(&serde_json::to_value(&resp)?["turns"], format);
    Ok(())
}

pub fn get_turn(client: &ApiClient, id: &str, format: Format) -> Result<()> {
    let agent = agent_client(client)?;
    let request = gestalt_sdk::public::generated::agent::GetAgentProviderTurnRequest {
        turn_id: id.to_string(),
        ..Default::default()
    };
    let resp = agent
        .get_turn_sync(request)
        .map_err(map_sdk_error)
        .with_context(|| format!("failed to get agent turn {id}"))?;
    print_turn(&serde_json::to_value(&resp)?, format);
    Ok(())
}

pub fn transcript_turn(client: &ApiClient, id: &str, format: Format) -> Result<()> {
    let agent = agent_client(client)?;
    let request = gestalt_sdk::public::generated::agent::GetAgentProviderTurnRequest {
        turn_id: id.to_string(),
        ..Default::default()
    };
    let turn = agent
        .get_turn_sync(request)
        .map_err(map_sdk_error)
        .with_context(|| format!("failed to get agent turn {id}"))?;
    let turn_value = serde_json::to_value(&turn)?;
    let event_values = list_turn_event_values(client, id, 0)?;

    match format {
        Format::Json => output::print_json(&json!({
            "turn": turn_value,
            "events": event_values,
        })),
        Format::Table => {
            let turn = decode_json::<AgentTurnInfo>(turn_value)?;
            let events: Vec<AgentTurnEventInfo> = event_values
                .into_iter()
                .map(decode_json)
                .collect::<Result<_>>()?;
            render_turn_transcript(&turn, &events)?;
        }
    }
    Ok(())
}

pub fn cancel_turn(
    client: &ApiClient,
    id: &str,
    reason: Option<&str>,
    format: Format,
) -> Result<()> {
    let agent = agent_client(client)?;
    let request = gestalt_sdk::public::generated::agent::CancelAgentProviderTurnRequest {
        turn_id: id.to_string(),
        reason: reason.unwrap_or_default().to_string(),
        ..Default::default()
    };
    let resp = agent
        .cancel_turn_sync(request)
        .map_err(map_sdk_error)
        .with_context(|| format!("failed to cancel agent turn {id}"))?;
    print_turn(&serde_json::to_value(&resp)?, format);
    Ok(())
}

pub fn list_turn_events(
    client: &ApiClient,
    args: &AgentTurnEventListArgs,
    format: Format,
) -> Result<()> {
    let agent = agent_client(client)?;
    let request = gestalt_sdk::public::generated::agent::ListAgentProviderTurnEventsRequest {
        turn_id: args.id.clone(),
        after_seq: args.after.unwrap_or(0) as i64,
        limit: args.limit.unwrap_or(0) as i32,
        ..Default::default()
    };
    let resp = agent
        .list_turn_events_sync(request)
        .map_err(map_sdk_error)
        .with_context(|| format!("failed to list events for agent turn {}", args.id))?;
    print_turn_events(&serde_json::to_value(&resp)?["events"], format);
    Ok(())
}

pub(crate) fn cancel_turn_silent(
    client: &ApiClient,
    id: &str,
    reason: &str,
) -> Result<AgentTurnInfo> {
    let agent = agent_client(client)?;
    let request = gestalt_sdk::public::generated::agent::CancelAgentProviderTurnRequest {
        turn_id: id.to_string(),
        reason: reason.to_string(),
        ..Default::default()
    };
    let resp = agent
        .cancel_turn_sync(request)
        .map_err(map_sdk_error)
        .with_context(|| format!("failed to cancel agent turn {id}"))?;
    decode_json(serde_json::to_value(&resp)?)
}

pub(crate) fn create_session_info(
    client: &ApiClient,
    args: &AgentSessionCreateArgs,
) -> Result<AgentSessionInfo> {
    let agent = agent_client(client)?;
    let request = build_create_session_request(args)?;
    let resp = agent
        .create_session_sync(request)
        .map_err(map_sdk_error)
        .context("failed to create agent session")?;
    decode_json(serde_json::to_value(&resp)?)
}

pub(crate) fn get_session_info(client: &ApiClient, id: &str) -> Result<AgentSessionInfo> {
    let agent = agent_client(client)?;
    let request = gestalt_sdk::public::generated::agent::GetAgentProviderSessionRequest {
        session_id: id.to_string(),
        ..Default::default()
    };
    let resp = agent
        .get_session_sync(request)
        .map_err(map_sdk_error)
        .with_context(|| format!("failed to get agent session {id}"))?;
    decode_json(serde_json::to_value(&resp)?)
}

pub(crate) fn resume_latest_session_info(
    client: &ApiClient,
    provider: Option<&str>,
) -> Result<AgentSessionInfo> {
    let agent = agent_client(client)?;
    let request = gestalt_sdk::public::generated::agent::ListAgentProviderSessionsRequest {
        provider_name: provider.unwrap_or_default().to_string(),
        state: AGENT_SESSION_STATE_ACTIVE,
        limit: DEFAULT_SESSION_LIST_LIMIT as i32,
        summary_only: true,
        ..Default::default()
    };
    let resp = agent
        .list_sessions_sync(request)
        .map_err(map_sdk_error)
        .context("failed to list active agent sessions")?;
    let sessions: Vec<AgentSessionInfo> =
        decode_json(serde_json::to_value(&resp)?["sessions"].clone())?;
    sessions
        .into_iter()
        .filter(|session| session.state.is_empty() || session.state == "active")
        .max_by(compare_sessions_for_resume)
        .ok_or_else(|| match provider {
            Some(provider) => anyhow::anyhow!(
                "no active agent sessions found for provider {provider}; use `gestalt agent` to create one"
            ),
            None => anyhow::anyhow!(
                "no active agent sessions found; use `gestalt agent` to create one"
            ),
        })
}

fn compare_sessions_for_resume(a: &AgentSessionInfo, b: &AgentSessionInfo) -> std::cmp::Ordering {
    compare_session_time_field(&a.last_turn_at, &b.last_turn_at)
        .then_with(|| compare_session_time_field(&a.updated_at, &b.updated_at))
        .then_with(|| compare_session_time_field(&a.created_at, &b.created_at))
        .then_with(|| a.id.cmp(&b.id))
}

fn compare_session_time_field(a: &str, b: &str) -> std::cmp::Ordering {
    match (parse_session_time(a), parse_session_time(b)) {
        (Some(a), Some(b)) => a.cmp(&b),
        (Some(_), None) => std::cmp::Ordering::Greater,
        (None, Some(_)) => std::cmp::Ordering::Less,
        (None, None) => a.cmp(b),
    }
}

fn parse_session_time(value: &str) -> Option<OffsetDateTime> {
    OffsetDateTime::parse(value, &Rfc3339).ok()
}

pub(crate) fn create_turn_info(
    client: &ApiClient,
    args: &AgentTurnCreateArgs,
) -> Result<AgentTurnInfo> {
    let agent = agent_client(client)?;
    let request = build_create_turn_request(args)?;
    let resp = agent
        .create_turn_sync(request)
        .map_err(map_sdk_error)
        .with_context(|| format!("failed to create agent turn in session {}", args.session_id))?;
    decode_json(serde_json::to_value(&resp)?)
}

pub(crate) fn get_turn_info(client: &ApiClient, id: &str) -> Result<AgentTurnInfo> {
    let agent = agent_client(client)?;
    let request = gestalt_sdk::public::generated::agent::GetAgentProviderTurnRequest {
        turn_id: id.to_string(),
        ..Default::default()
    };
    let resp = agent
        .get_turn_sync(request)
        .map_err(map_sdk_error)
        .with_context(|| format!("failed to get agent turn {id}"))?;
    decode_json(serde_json::to_value(&resp)?)
}

pub(crate) fn list_interactions_info(
    client: &ApiClient,
    turn_id: &str,
) -> Result<Vec<AgentInteractionInfo>> {
    let agent = agent_client(client)?;
    let request = gestalt_sdk::public::generated::agent::ListAgentProviderInteractionsRequest {
        turn_id: turn_id.to_string(),
        ..Default::default()
    };
    let resp = agent
        .list_interactions_sync(request)
        .map_err(map_sdk_error)
        .with_context(|| format!("failed to list interactions for agent turn {turn_id}"))?;
    decode_json(serde_json::to_value(&resp)?["interactions"].clone())
}

pub(crate) fn resolve_interaction_info(
    client: &ApiClient,
    turn_id: &str,
    interaction_id: &str,
    resolution: Map<String, Value>,
) -> Result<AgentInteractionInfo> {
    let agent = agent_client(client)?;
    let request = gestalt_sdk::public::generated::agent::ResolveAgentProviderInteractionRequest {
        interaction_id: interaction_id.to_string(),
        turn_id: turn_id.to_string(),
        resolution: Some(resolution),
        ..Default::default()
    };
    let resp = agent
        .resolve_interaction_sync(request)
        .map_err(map_sdk_error)
        .with_context(|| format!("failed to resolve interaction {interaction_id}"))?;
    decode_json(serde_json::to_value(&resp)?)
}

pub(crate) fn list_turn_event_values(
    client: &ApiClient,
    turn_id: &str,
    after_seq: u64,
) -> Result<Vec<Value>> {
    let agent = agent_client(client)?;
    let mut events = Vec::new();
    let mut after_seq = after_seq as i64;
    loop {
        let request = gestalt_sdk::public::generated::agent::ListAgentProviderTurnEventsRequest {
            turn_id: turn_id.to_string(),
            after_seq,
            limit: DEFAULT_EVENT_PAGE_SIZE as i32,
            ..Default::default()
        };
        let resp = agent
            .list_turn_events_sync(request)
            .map_err(map_sdk_error)
            .with_context(|| format!("failed to list events for agent turn {turn_id}"))?;
        let page: Vec<Value> = decode_json(serde_json::to_value(&resp)?["events"].clone())?;
        if page.is_empty() {
            return Ok(events);
        }

        let next_after_seq = page
            .iter()
            .filter_map(|event| event.get("seq").and_then(Value::as_i64))
            .max()
            .unwrap_or(after_seq);
        events.extend(page);
        if next_after_seq <= after_seq {
            return Ok(events);
        }
        after_seq = next_after_seq;
    }
}

fn build_tool_config(tools: &[AgentToolArg]) -> Option<AgentToolConfig> {
    if tools.is_empty() {
        return None;
    }
    let refs: Vec<AgentToolRef> = tools
        .iter()
        .map(|tool| AgentToolRef {
            app: tool.app.clone(),
            operation: tool.operation.clone(),
            ..Default::default()
        })
        .collect();
    Some(AgentToolConfig {
        source: Some(AgentToolConfigSource::Catalog(AgentCatalogToolConfig {
            refs,
            ..Default::default()
        })),
    })
}

fn build_create_session_request(
    args: &AgentSessionCreateArgs,
) -> Result<gestalt_sdk::public::generated::agent::CreateAgentProviderSessionRequest> {
    let mut request = gestalt_sdk::public::generated::agent::CreateAgentProviderSessionRequest {
        provider_name: args.provider.as_deref().unwrap_or_default().to_string(),
        model: args.model.as_deref().unwrap_or_default().to_string(),
        client_ref: args.client_ref.as_deref().unwrap_or_default().to_string(),
        idempotency_key: args
            .idempotency_key
            .as_deref()
            .unwrap_or_default()
            .to_string(),
        tools: build_tool_config(&args.tools),
        ..Default::default()
    };
    if let Some(path) = args.input.as_deref() {
        let map = params::load_input_file(path)?;
        let json = serde_json::Value::Object(map);
        if let Some(meta) = json
            .as_object()
            .and_then(|obj| obj.get("metadata"))
            .and_then(|v| v.as_object())
        {
            request.metadata = Some(meta.clone());
        }
    }
    Ok(request)
}

fn build_update_session_request(
    args: &AgentSessionUpdateArgs,
) -> Result<gestalt_sdk::public::generated::agent::UpdateAgentProviderSessionRequest> {
    let mut request = gestalt_sdk::public::generated::agent::UpdateAgentProviderSessionRequest {
        session_id: args.id.clone(),
        client_ref: args.client_ref.as_deref().unwrap_or_default().to_string(),
        state: parse_session_state(args.state.as_deref()),
        ..Default::default()
    };
    if let Some(path) = args.input.as_deref() {
        let map = params::load_input_file(path)?;
        if let Some(meta) = map.get("metadata").and_then(|v| v.as_object()) {
            request.metadata = Some(meta.clone());
        }
    }
    Ok(request)
}

fn build_create_turn_request(
    args: &AgentTurnCreateArgs,
) -> Result<gestalt_sdk::public::generated::agent::CreateAgentProviderTurnRequest> {
    let mut request = gestalt_sdk::public::generated::agent::CreateAgentProviderTurnRequest {
        session_id: args.session_id.clone(),
        model: args.model.as_deref().unwrap_or_default().to_string(),
        idempotency_key: args
            .idempotency_key
            .as_deref()
            .unwrap_or_default()
            .to_string(),
        timeout_seconds: args.timeout_seconds.unwrap_or(0),
        messages: build_messages(&args.system, &args.messages),
        output: Some(AgentOutput {
            kind: Some(gestalt_sdk::agent::AgentOutputKind::Text(
                AgentTextOutput {},
            )),
        }),
        ..Default::default()
    };
    if let Some(path) = args.input.as_deref() {
        let map = params::load_input_file(path)?;
        if let Some(meta) = map.get("metadata").and_then(|v| v.as_object()) {
            request.metadata = Some(meta.clone());
        }
        if request.messages.is_empty()
            && let Some(messages) = map.get("messages")
        {
            let parsed: Vec<AgentMessage> = serde_json::from_value(messages.clone())
                .context("failed to decode messages from --input file")?;
            if !parsed.is_empty() {
                request.messages = parsed;
            }
        }
        if let Some(model_options) = map.get("modelOptions").and_then(|v| v.as_object())
            && !model_options.is_empty()
        {
            request.model_options = Some(model_options.clone());
        }
        if let Some(output) = map.get("output")
            && let Ok(parsed) = serde_json::from_value::<AgentOutput>(output.clone())
        {
            request.output = Some(parsed);
        }
    }
    validate_turn_request(&request)?;
    Ok(request)
}

fn validate_turn_request(
    request: &gestalt_sdk::public::generated::agent::CreateAgentProviderTurnRequest,
) -> Result<()> {
    if request.messages.is_empty() {
        bail!(
            "agent turns create requires at least one message; pass --message, --system, or --input with a non-empty messages array"
        );
    }
    Ok(())
}

fn build_messages(system: &[String], messages: &[String]) -> Vec<AgentMessage> {
    let mut out = Vec::with_capacity(system.len() + messages.len());
    for text in system {
        out.push(AgentMessage {
            role: "system".to_string(),
            parts: vec![AgentMessagePart {
                r#type: AGENT_MESSAGE_PART_TYPE_TEXT,
                text: text.clone(),
                ..Default::default()
            }],
            ..Default::default()
        });
    }
    for text in messages {
        out.push(AgentMessage {
            role: "user".to_string(),
            parts: vec![AgentMessagePart {
                r#type: AGENT_MESSAGE_PART_TYPE_TEXT,
                text: text.clone(),
                ..Default::default()
            }],
            ..Default::default()
        });
    }
    out
}

fn parse_session_state(value: Option<&str>) -> AgentSessionState {
    match value.map(str::trim).filter(|v| !v.is_empty()) {
        Some("active") => AGENT_SESSION_STATE_ACTIVE,
        Some("closed") => AGENT_SESSION_STATE_ARCHIVED,
        _ => AGENT_SESSION_STATE_UNSPECIFIED,
    }
}

fn parse_execution_status(value: Option<&str>) -> AgentExecutionStatus {
    match value.map(str::trim).filter(|v| !v.is_empty()) {
        Some("pending") => AGENT_EXECUTION_STATUS_PENDING,
        Some("running") => AGENT_EXECUTION_STATUS_RUNNING,
        Some("succeeded") => AGENT_EXECUTION_STATUS_SUCCEEDED,
        Some("failed") => AGENT_EXECUTION_STATUS_FAILED,
        Some("canceled") | Some("cancelled") => AGENT_EXECUTION_STATUS_CANCELED,
        Some("waiting_for_input") => AGENT_EXECUTION_STATUS_WAITING_FOR_INPUT,
        _ => AGENT_EXECUTION_STATUS_UNSPECIFIED,
    }
}
