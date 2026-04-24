use anyhow::{Context, Result, bail};
use serde_json::{Map, Value, json};
use std::io;

use crate::api::ApiClient;
use crate::cli::{
    AgentSessionCreateArgs, AgentSessionUpdateArgs, AgentToolArg, AgentTurnCreateArgs,
    AgentTurnEventListArgs, AgentTurnEventStreamArgs,
};
use crate::output::{self, Format};
use crate::params;

const SESSIONS_PATH: &str = "/api/v1/agent/sessions";
const TURNS_PATH: &str = "/api/v1/agent/turns";

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
    format: Format,
) -> Result<()> {
    let resp = client
        .get(&sessions_path(provider, state))
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
        .get(&turn_events_path(&args.id, false, args.after, args.limit))
        .with_context(|| format!("failed to list events for agent turn {}", args.id))?;
    print_turn_events(&resp, format);
    Ok(())
}

pub fn stream_turn_events(client: &ApiClient, args: &AgentTurnEventStreamArgs) -> Result<()> {
    let mut resp = client
        .get_stream(&turn_events_path(&args.id, true, args.after, args.limit))
        .with_context(|| format!("failed to stream events for agent turn {}", args.id))?;
    let mut stdout = io::stdout().lock();
    io::copy(&mut resp, &mut stdout).context("failed to read agent turn event stream")?;
    Ok(())
}

fn build_session_create_body(args: &AgentSessionCreateArgs) -> Result<Value> {
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

fn build_session_update_body(args: &AgentSessionUpdateArgs) -> Result<Value> {
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

fn build_turn_create_body(args: &AgentTurnCreateArgs) -> Result<Value> {
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

    validate_turn_create_body(&body)?;
    Ok(Value::Object(body))
}

fn validate_turn_create_body(body: &Map<String, Value>) -> Result<()> {
    let has_messages = body
        .get("messages")
        .and_then(Value::as_array)
        .is_some_and(|messages| !messages.is_empty());
    if !has_messages {
        bail!(
            "agent turns create requires at least one message; pass --message, --system, or --input with a non-empty messages array"
        );
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
        "pluginName": tool.plugin,
        "operation": tool.operation,
    })
}

fn sessions_path(provider: Option<&str>, state: Option<&str>) -> String {
    let mut serializer = url::form_urlencoded::Serializer::new(String::new());
    if let Some(provider) = provider {
        serializer.append_pair("provider", provider);
    }
    if let Some(state) = state {
        serializer.append_pair("state", state);
    }
    let query = serializer.finish();
    if query.is_empty() {
        SESSIONS_PATH.to_string()
    } else {
        format!("{SESSIONS_PATH}?{query}")
    }
}

fn session_turns_path(session_id: &str, status: Option<&str>) -> String {
    let mut serializer = url::form_urlencoded::Serializer::new(String::new());
    if let Some(status) = status {
        serializer.append_pair("status", status);
    }
    let query = serializer.finish();
    let path = format!("{SESSIONS_PATH}/{session_id}/turns");
    if query.is_empty() {
        path
    } else {
        format!("{path}?{query}")
    }
}

fn turn_events_path(id: &str, stream: bool, after: Option<u64>, limit: Option<u32>) -> String {
    let mut serializer = url::form_urlencoded::Serializer::new(String::new());
    if let Some(after) = after {
        serializer.append_pair("after", &after.to_string());
    }
    if let Some(limit) = limit {
        serializer.append_pair("limit", &limit.to_string());
    }
    let suffix = if stream { "/events/stream" } else { "/events" };
    let query = serializer.finish();
    let path = format!("{TURNS_PATH}/{id}{suffix}");
    if query.is_empty() {
        path
    } else {
        format!("{path}?{query}")
    }
}

fn print_session(value: &Value, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let rows = vec![session_row(value)];
            output::print_table(&session_headers(), &rows);
        }
    }
}

fn print_sessions(value: &Value, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let items = value.as_array().cloned().unwrap_or_default();
            let rows: Vec<Vec<String>> = items.iter().map(session_row).collect();
            output::print_table(&session_headers(), &rows);
        }
    }
}

fn print_turn(value: &Value, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let rows = vec![turn_row(value)];
            output::print_table(&turn_headers(), &rows);
        }
    }
}

fn print_turns(value: &Value, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let items = value.as_array().cloned().unwrap_or_default();
            let rows: Vec<Vec<String>> = items.iter().map(turn_row).collect();
            output::print_table(&turn_headers(), &rows);
        }
    }
}

fn print_turn_events(value: &Value, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let items = value.as_array().cloned().unwrap_or_default();
            let rows: Vec<Vec<String>> = items.iter().map(turn_event_row).collect();
            output::print_table(&event_headers(), &rows);
        }
    }
}

fn session_headers() -> [&'static str; 6] {
    ["ID", "Provider", "Model", "State", "Client Ref", "Updated"]
}

fn turn_headers() -> [&'static str; 6] {
    ["ID", "Session", "Provider", "Model", "Status", "Created"]
}

fn event_headers() -> [&'static str; 7] {
    [
        "Seq",
        "Type",
        "Source",
        "Visibility",
        "Turn",
        "Created",
        "Data",
    ]
}

fn session_row(value: &Value) -> Vec<String> {
    vec![
        value["id"].as_str().unwrap_or("-").to_string(),
        value["provider"].as_str().unwrap_or("-").to_string(),
        value["model"].as_str().unwrap_or("-").to_string(),
        value["state"].as_str().unwrap_or("-").to_string(),
        value["clientRef"].as_str().unwrap_or("-").to_string(),
        value["updatedAt"].as_str().unwrap_or("-").to_string(),
    ]
}

fn turn_row(value: &Value) -> Vec<String> {
    vec![
        value["id"].as_str().unwrap_or("-").to_string(),
        value["sessionId"].as_str().unwrap_or("-").to_string(),
        value["provider"].as_str().unwrap_or("-").to_string(),
        value["model"].as_str().unwrap_or("-").to_string(),
        value["status"].as_str().unwrap_or("-").to_string(),
        value["createdAt"].as_str().unwrap_or("-").to_string(),
    ]
}

fn turn_event_row(value: &Value) -> Vec<String> {
    vec![
        value["seq"]
            .as_i64()
            .map(|seq| seq.to_string())
            .unwrap_or_else(|| "-".to_string()),
        value["type"].as_str().unwrap_or("-").to_string(),
        value["source"].as_str().unwrap_or("-").to_string(),
        value["visibility"].as_str().unwrap_or("-").to_string(),
        value["turnId"].as_str().unwrap_or("-").to_string(),
        value["createdAt"].as_str().unwrap_or("-").to_string(),
        serde_json::to_string(&value["data"]).unwrap_or_else(|_| "-".to_string()),
    ]
}
