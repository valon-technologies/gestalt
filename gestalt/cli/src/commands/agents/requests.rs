use anyhow::{Context, Result, bail};
use serde_json::{Map, Value, json};
use std::io;
use time::{OffsetDateTime, format_description::well_known::Rfc3339};

use crate::api::ApiClient;
use crate::cli::{
    AgentSessionCreateArgs, AgentSessionUpdateArgs, AgentToolArg, AgentTurnCreateArgs,
    AgentTurnEventListArgs, AgentTurnEventStreamArgs,
};
use crate::output::{self, Format};
use crate::params;

use super::fields::decode_json;
use super::format::table::{
    print_session, print_sessions, print_turn, print_turn_events, print_turns,
};
use super::render::stdout::render_turn_transcript;
use super::stream::DEFAULT_EVENT_PAGE_SIZE;
use super::types::{
    AgentInteractionInfo, AgentProviderInfo, AgentProviderListInfo, AgentSessionInfo,
    AgentTurnEventInfo, AgentTurnInfo,
};

pub(crate) const SESSIONS_PATH: &str = "/api/v1/agent/sessions";
pub(crate) const PROVIDERS_PATH: &str = "/api/v1/agent/providers";
pub(crate) const TURNS_PATH: &str = "/api/v1/agent/turns";
pub(crate) const DEFAULT_SESSION_LIST_LIMIT: usize = 50;
pub(crate) const INTERRUPT_CANCEL_REASON: &str = "operator interrupted";

pub fn create_session(
    client: &ApiClient,
    args: &AgentSessionCreateArgs,
    format: Format,
) -> Result<()> {
    let body = build_session_create_body(args)?;
    let resp = client
        .post(SESSIONS_PATH, &body)
        .context("failed to create agent session")?;
    print_session(&resp, format);
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
    let resp = client
        .get(&sessions_path(provider, state, summary_limit))
        .context("failed to list agent sessions")?;
    print_sessions(&resp, format);
    Ok(())
}

pub fn get_session(client: &ApiClient, id: &str, format: Format) -> Result<()> {
    let resp = client
        .get(&format!("{SESSIONS_PATH}/{id}"))
        .with_context(|| format!("failed to get agent session {id}"))?;
    print_session(&resp, format);
    Ok(())
}

pub fn update_session(
    client: &ApiClient,
    args: &AgentSessionUpdateArgs,
    format: Format,
) -> Result<()> {
    let body = build_session_update_body(args)?;
    let resp = client
        .patch(&format!("{SESSIONS_PATH}/{}", args.id), &body)
        .with_context(|| format!("failed to update agent session {}", args.id))?;
    print_session(&resp, format);
    Ok(())
}

pub fn create_turn(client: &ApiClient, args: &AgentTurnCreateArgs, format: Format) -> Result<()> {
    let body = build_turn_create_body(args)?;
    let resp = client
        .post(&format!("{SESSIONS_PATH}/{}/turns", args.session_id), &body)
        .with_context(|| format!("failed to create agent turn in session {}", args.session_id))?;
    print_turn(&resp, format);
    Ok(())
}

pub fn list_turns(
    client: &ApiClient,
    session_id: &str,
    status: Option<&str>,
    format: Format,
) -> Result<()> {
    let resp = client
        .get(&session_turns_path(session_id, status))
        .with_context(|| format!("failed to list agent turns for session {session_id}"))?;
    print_turns(&resp, format);
    Ok(())
}

pub fn get_turn(client: &ApiClient, id: &str, format: Format) -> Result<()> {
    let resp = client
        .get(&format!("{TURNS_PATH}/{id}"))
        .with_context(|| format!("failed to get agent turn {id}"))?;
    print_turn(&resp, format);
    Ok(())
}

pub fn transcript_turn(client: &ApiClient, id: &str, format: Format) -> Result<()> {
    let turn = client
        .get(&format!("{TURNS_PATH}/{id}"))
        .with_context(|| format!("failed to get agent turn {id}"))?;
    let event_values = list_turn_event_values(client, id)?;
    match format {
        Format::Json => output::print_json(&json!({
            "turn": turn,
            "events": event_values,
        })),
        Format::Table => {
            let turn = decode_json::<AgentTurnInfo>(turn)?;
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
    let body = match reason {
        Some(reason) => json!({ "reason": reason }),
        None => json!({}),
    };
    let resp = client
        .post(&format!("{TURNS_PATH}/{id}/cancel"), &body)
        .with_context(|| format!("failed to cancel agent turn {id}"))?;
    print_turn(&resp, format);
    Ok(())
}

pub fn list_turn_events(
    client: &ApiClient,
    args: &AgentTurnEventListArgs,
    format: Format,
) -> Result<()> {
    let resp = client
        .get(&turn_events_path(
            &args.id, false, args.after, args.limit, None,
        ))
        .with_context(|| format!("failed to list events for agent turn {}", args.id))?;
    print_turn_events(&resp, format);
    Ok(())
}

pub fn stream_turn_events(client: &ApiClient, args: &AgentTurnEventStreamArgs) -> Result<()> {
    let mut resp = client
        .get_stream(&turn_events_path(
            &args.id, true, args.after, args.limit, None,
        ))
        .with_context(|| format!("failed to stream events for agent turn {}", args.id))?;
    let mut stdout = io::stdout().lock();
    io::copy(&mut resp, &mut stdout).context("failed to read agent turn event stream")?;
    Ok(())
}

pub(crate) fn cancel_turn_silent(
    client: &ApiClient,
    id: &str,
    reason: &str,
) -> Result<AgentTurnInfo> {
    decode_json(
        client
            .post(
                &format!("{TURNS_PATH}/{id}/cancel"),
                &json!({ "reason": reason }),
            )
            .with_context(|| format!("failed to cancel agent turn {id}"))?,
    )
}

pub(crate) fn create_session_info(
    client: &ApiClient,
    args: &AgentSessionCreateArgs,
) -> Result<AgentSessionInfo> {
    let body = build_session_create_body(args)?;
    decode_json(
        client
            .post(SESSIONS_PATH, &body)
            .context("failed to create agent session")?,
    )
}

pub(crate) fn get_session_info(client: &ApiClient, id: &str) -> Result<AgentSessionInfo> {
    decode_json(
        client
            .get(&format!("{SESSIONS_PATH}/{id}"))
            .with_context(|| format!("failed to get agent session {id}"))?,
    )
}

pub(crate) fn list_agent_providers(client: &ApiClient) -> Result<Vec<AgentProviderInfo>> {
    let resp: AgentProviderListInfo = decode_json(
        client
            .get(PROVIDERS_PATH)
            .context("failed to list agent providers")?,
    )?;
    Ok(resp.providers)
}

pub(crate) fn resume_latest_session_info(
    client: &ApiClient,
    provider: Option<&str>,
) -> Result<AgentSessionInfo> {
    let sessions: Vec<AgentSessionInfo> = decode_json(
        client
            .get(&sessions_path(
                provider,
                Some("active"),
                Some(DEFAULT_SESSION_LIST_LIMIT),
            ))
            .context("failed to list active agent sessions")?,
    )?;
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
    let body = build_turn_create_body(args)?;
    decode_json(
        client
            .post(&format!("{SESSIONS_PATH}/{}/turns", args.session_id), &body)
            .with_context(|| {
                format!("failed to create agent turn in session {}", args.session_id)
            })?,
    )
}

pub(crate) fn get_turn_info(client: &ApiClient, id: &str) -> Result<AgentTurnInfo> {
    decode_json(
        client
            .get(&format!("{TURNS_PATH}/{id}"))
            .with_context(|| format!("failed to get agent turn {id}"))?,
    )
}

pub(crate) fn list_interactions_info(
    client: &ApiClient,
    turn_id: &str,
) -> Result<Vec<AgentInteractionInfo>> {
    decode_json(
        client
            .get(&format!("{TURNS_PATH}/{turn_id}/interactions"))
            .with_context(|| format!("failed to list interactions for agent turn {turn_id}"))?,
    )
}

pub(crate) fn resolve_interaction_info(
    client: &ApiClient,
    turn_id: &str,
    interaction_id: &str,
    resolution: Map<String, Value>,
) -> Result<AgentInteractionInfo> {
    decode_json(
        client
            .post(
                &format!("{TURNS_PATH}/{turn_id}/interactions/{interaction_id}/resolve"),
                &json!({ "resolution": resolution }),
            )
            .with_context(|| format!("failed to resolve interaction {interaction_id}"))?,
    )
}

pub(crate) fn list_turn_event_values(client: &ApiClient, turn_id: &str) -> Result<Vec<Value>> {
    let mut events = Vec::new();
    let mut after_seq = 0u64;
    loop {
        let page: Vec<Value> = decode_json(
            client
                .get(&turn_events_path(
                    turn_id,
                    false,
                    Some(after_seq),
                    Some(DEFAULT_EVENT_PAGE_SIZE),
                    None,
                ))
                .with_context(|| format!("failed to list events for agent turn {turn_id}"))?,
        )?;
        if page.is_empty() {
            return Ok(events);
        }

        let next_after_seq = page
            .iter()
            .filter_map(|event| event.get("seq").and_then(Value::as_u64))
            .max()
            .unwrap_or(after_seq);
        events.extend(page);
        if next_after_seq <= after_seq {
            return Ok(events);
        }
        after_seq = next_after_seq;
    }
}

pub(crate) fn build_session_create_body(args: &AgentSessionCreateArgs) -> Result<Value> {
    let mut body = match args.input.as_deref() {
        Some(path) => params::load_input_file(path)?,
        None => Map::new(),
    };

    if let Some(provider) = args.provider.as_deref() {
        body.insert("provider".to_string(), Value::String(provider.to_string()));
    }
    if let Some(model) = args.model.as_deref() {
        body.insert("model".to_string(), Value::String(model.to_string()));
    }
    if let Some(client_ref) = args.client_ref.as_deref() {
        body.insert(
            "clientRef".to_string(),
            Value::String(client_ref.to_string()),
        );
    }
    if let Some(idempotency_key) = args.idempotency_key.as_deref() {
        body.insert(
            "idempotencyKey".to_string(),
            Value::String(idempotency_key.to_string()),
        );
    }

    Ok(Value::Object(body))
}

pub(crate) fn build_session_update_body(args: &AgentSessionUpdateArgs) -> Result<Value> {
    let mut body = match args.input.as_deref() {
        Some(path) => params::load_input_file(path)?,
        None => Map::new(),
    };

    body.remove("provider");
    body.remove("model");

    if let Some(client_ref) = args.client_ref.as_deref() {
        body.insert(
            "clientRef".to_string(),
            Value::String(client_ref.to_string()),
        );
    }
    if let Some(state) = args.state.as_deref() {
        body.insert("state".to_string(), Value::String(state.to_string()));
    }

    Ok(Value::Object(body))
}

pub(crate) fn build_turn_create_body(args: &AgentTurnCreateArgs) -> Result<Value> {
    let mut body = match args.input.as_deref() {
        Some(path) => params::load_input_file(path)?,
        None => Map::new(),
    };

    body.remove("provider");
    body.remove("clientRef");
    body.remove("state");

    if let Some(model) = args.model.as_deref() {
        body.insert("model".to_string(), Value::String(model.to_string()));
    }
    if let Some(idempotency_key) = args.idempotency_key.as_deref() {
        body.insert(
            "idempotencyKey".to_string(),
            Value::String(idempotency_key.to_string()),
        );
    }
    if let Some(timeout_seconds) = args.timeout_seconds {
        body.insert(
            "timeoutSeconds".to_string(),
            Value::Number(timeout_seconds.into()),
        );
    }

    let messages = build_messages(&args.system, &args.messages);
    if !messages.is_empty() {
        body.insert("messages".to_string(), Value::Array(messages));
    }
    if !args.tools.is_empty() {
        body.insert(
            "toolRefs".to_string(),
            Value::Array(args.tools.iter().map(agent_tool_ref_value).collect()),
        );
    }
    if !body.contains_key("output") {
        body.insert("output".to_string(), json!({ "text": {} }));
    }

    validate_turn_create_body(&body)?;
    Ok(Value::Object(body))
}

fn validate_optional_timeout_seconds(body: &Map<String, Value>) -> Result<()> {
    if let Some(value) = body.get("timeoutSeconds")
        && !value.as_i64().is_some_and(|seconds| seconds >= 0)
    {
        bail!("timeoutSeconds must be a non-negative integer");
    }
    Ok(())
}

fn validate_turn_create_body(body: &Map<String, Value>) -> Result<()> {
    validate_optional_timeout_seconds(body)?;
    let has_messages = body
        .get("messages")
        .and_then(Value::as_array)
        .is_some_and(|messages| !messages.is_empty());
    if !has_messages {
        bail!(
            "agent turns create requires at least one message; pass --message, --system, or --input with a non-empty messages array"
        );
    }
    validate_turn_output_body(body)?;
    Ok(())
}

fn validate_turn_output_body(body: &Map<String, Value>) -> Result<()> {
    let Some(output) = body.get("output") else {
        bail!("agent turns create requires output.text or output.structured");
    };
    let Some(output) = output.as_object() else {
        bail!("agent turns create output must be an object");
    };
    let has_text = output.contains_key("text");
    let has_structured = output.contains_key("structured");
    if has_text == has_structured {
        bail!("agent turns create requires exactly one of output.text or output.structured");
    }
    Ok(())
}

fn build_messages(system: &[String], messages: &[String]) -> Vec<Value> {
    let mut out = Vec::with_capacity(system.len() + messages.len());
    for text in system {
        out.push(json!({ "role": "system", "text": text }));
    }
    for text in messages {
        out.push(json!({ "role": "user", "text": text }));
    }
    out
}

fn agent_tool_ref_value(tool: &AgentToolArg) -> Value {
    json!({
        "app": tool.app,
        "operation": tool.operation,
    })
}

pub(crate) fn sessions_path(
    provider: Option<&str>,
    state: Option<&str>,
    summary_limit: Option<usize>,
) -> String {
    let mut params = Vec::new();
    crate::query::push_opt_param(&mut params, "provider", provider);
    crate::query::push_opt_param(&mut params, "state", state);
    if let Some(limit) = summary_limit {
        params.push(("view".to_string(), "summary".to_string()));
        params.push(("limit".to_string(), limit.to_string()));
    }
    crate::query::with_query(SESSIONS_PATH, &params)
}

pub(crate) fn session_turns_path(session_id: &str, status: Option<&str>) -> String {
    let mut params = Vec::new();
    crate::query::push_opt_param(&mut params, "status", status);
    let path = format!("{SESSIONS_PATH}/{session_id}/turns");
    crate::query::with_query(&path, &params)
}

pub(crate) fn turn_events_path(
    id: &str,
    stream: bool,
    after: Option<u64>,
    limit: Option<u32>,
    until: Option<&str>,
) -> String {
    let mut params = Vec::new();
    crate::query::push_opt_u64(&mut params, "after", after);
    crate::query::push_opt_u32(&mut params, "limit", limit);
    crate::query::push_opt_param(&mut params, "until", until);
    let suffix = if stream { "/events/stream" } else { "/events" };
    let path = format!("{TURNS_PATH}/{id}{suffix}");
    crate::query::with_query(&path, &params)
}
