use anyhow::{Context, Result, bail};
use serde_json::{Map, Value, json};
use std::io;

use crate::api::ApiClient;
use crate::cli::{
    AgentRunCreateArgs, AgentRunEventListArgs, AgentRunEventStreamArgs, AgentToolArg,
};
use crate::output::{self, Format};
use crate::params;

const RUNS_PATH: &str = "/api/v1/agent/runs";

pub fn create_run(client: &ApiClient, args: &AgentRunCreateArgs, format: Format) -> Result<()> {
    let body = build_create_body(args)?;
    let resp = client
        .post(RUNS_PATH, &body)
        .context("failed to create agent run")?;
    print_run(&resp, format);
    Ok(())
}

pub fn list_runs(
    client: &ApiClient,
    provider: Option<&str>,
    status: Option<&str>,
    format: Format,
) -> Result<()> {
    let resp = client
        .get(&runs_path(provider, status))
        .context("failed to list agent runs")?;
    print_runs(&resp, format);
    Ok(())
}

pub fn get_run(client: &ApiClient, id: &str, format: Format) -> Result<()> {
    let resp = client
        .get(&format!("{RUNS_PATH}/{id}"))
        .with_context(|| format!("failed to get agent run {id}"))?;
    print_run(&resp, format);
    Ok(())
}

pub fn cancel_run(
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
        .post(&format!("{RUNS_PATH}/{id}/cancel"), &body)
        .with_context(|| format!("failed to cancel agent run {id}"))?;
    print_run(&resp, format);
    Ok(())
}

pub fn list_run_events(
    client: &ApiClient,
    args: &AgentRunEventListArgs,
    format: Format,
) -> Result<()> {
    let resp = client
        .get(&run_events_path(&args.id, false, args.after, args.limit))
        .with_context(|| format!("failed to list events for agent run {}", args.id))?;
    print_run_events(&resp, format);
    Ok(())
}

pub fn stream_run_events(client: &ApiClient, args: &AgentRunEventStreamArgs) -> Result<()> {
    let mut resp = client
        .get_stream(&run_events_path(&args.id, true, args.after, args.limit))
        .with_context(|| format!("failed to stream events for agent run {}", args.id))?;
    let mut stdout = io::stdout().lock();
    io::copy(&mut resp, &mut stdout).context("failed to read agent run event stream")?;
    Ok(())
}

fn build_create_body(args: &AgentRunCreateArgs) -> Result<Value> {
    let mut body = match args.request_file.as_deref() {
        Some(path) => params::load_input_file(path)?,
        None => Map::new(),
    };

    if let Some(provider) = args.provider.as_deref() {
        body.insert("provider".to_string(), Value::String(provider.to_string()));
    }
    if let Some(model) = args.model.as_deref() {
        body.insert("model".to_string(), Value::String(model.to_string()));
    }
    if let Some(session_ref) = args.session_ref.as_deref() {
        body.insert(
            "sessionRef".to_string(),
            Value::String(session_ref.to_string()),
        );
    }
    if let Some(idempotency_key) = args.idempotency_key.as_deref() {
        body.insert(
            "idempotencyKey".to_string(),
            Value::String(idempotency_key.to_string()),
        );
    }

    let messages = build_messages(args);
    if !messages.is_empty() {
        body.insert("messages".to_string(), Value::Array(messages));
    }
    if !args.tools.is_empty() {
        body.insert(
            "toolRefs".to_string(),
            Value::Array(args.tools.iter().map(agent_tool_ref_value).collect()),
        );
    }
    validate_create_body(&body)?;

    Ok(Value::Object(body))
}

fn validate_create_body(body: &Map<String, Value>) -> Result<()> {
    let has_messages = body
        .get("messages")
        .and_then(Value::as_array)
        .is_some_and(|messages| !messages.is_empty());
    if !has_messages {
        bail!(
            "agent runs create requires at least one message; pass --message, --system, or --request-file with a non-empty messages array"
        );
    }
    Ok(())
}

fn build_messages(args: &AgentRunCreateArgs) -> Vec<Value> {
    let mut messages = Vec::with_capacity(args.system.len() + args.messages.len());
    for text in &args.system {
        messages.push(json!({ "role": "system", "text": text }));
    }
    for text in &args.messages {
        messages.push(json!({ "role": "user", "text": text }));
    }
    messages
}

fn agent_tool_ref_value(tool: &AgentToolArg) -> Value {
    json!({
        "pluginName": tool.plugin,
        "operation": tool.operation,
    })
}

fn runs_path(provider: Option<&str>, status: Option<&str>) -> String {
    let mut serializer = url::form_urlencoded::Serializer::new(String::new());
    if let Some(provider) = provider {
        serializer.append_pair("provider", provider);
    }
    if let Some(status) = status {
        serializer.append_pair("status", status);
    }
    let query = serializer.finish();
    if query.is_empty() {
        RUNS_PATH.to_string()
    } else {
        format!("{RUNS_PATH}?{query}")
    }
}

fn run_events_path(id: &str, stream: bool, after: Option<u64>, limit: Option<u32>) -> String {
    let mut serializer = url::form_urlencoded::Serializer::new(String::new());
    if let Some(after) = after {
        serializer.append_pair("after", &after.to_string());
    }
    if let Some(limit) = limit {
        serializer.append_pair("limit", &limit.to_string());
    }
    let suffix = if stream { "/events/stream" } else { "/events" };
    let query = serializer.finish();
    let path = format!("{RUNS_PATH}/{id}{suffix}");
    if query.is_empty() {
        path
    } else {
        format!("{path}?{query}")
    }
}

fn print_run(value: &Value, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let rows = vec![run_row(value)];
            output::print_table(&run_headers(), &rows);
        }
    }
}

fn print_runs(value: &Value, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let items = value.as_array().cloned().unwrap_or_default();
            let rows: Vec<Vec<String>> = items.iter().map(run_row).collect();
            output::print_table(&run_headers(), &rows);
        }
    }
}

fn print_run_events(value: &Value, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let items = value.as_array().cloned().unwrap_or_default();
            let rows: Vec<Vec<String>> = items.iter().map(run_event_row).collect();
            output::print_table(&event_headers(), &rows);
        }
    }
}

fn run_headers() -> [&'static str; 6] {
    ["ID", "Provider", "Model", "Status", "Session", "Created"]
}

fn event_headers() -> [&'static str; 7] {
    [
        "Seq",
        "Type",
        "Source",
        "Visibility",
        "Run",
        "Created",
        "Data",
    ]
}

fn run_row(value: &Value) -> Vec<String> {
    vec![
        value["id"].as_str().unwrap_or("-").to_string(),
        value["provider"].as_str().unwrap_or("-").to_string(),
        value["model"].as_str().unwrap_or("-").to_string(),
        value["status"].as_str().unwrap_or("-").to_string(),
        value["sessionRef"].as_str().unwrap_or("-").to_string(),
        value["createdAt"].as_str().unwrap_or("-").to_string(),
    ]
}

fn run_event_row(value: &Value) -> Vec<String> {
    vec![
        value["seq"]
            .as_i64()
            .map(|seq| seq.to_string())
            .unwrap_or_else(|| "-".to_string()),
        value["type"].as_str().unwrap_or("-").to_string(),
        value["source"].as_str().unwrap_or("-").to_string(),
        value["visibility"].as_str().unwrap_or("-").to_string(),
        value["runId"].as_str().unwrap_or("-").to_string(),
        value["createdAt"].as_str().unwrap_or("-").to_string(),
        serde_json::to_string(&value["data"]).unwrap_or_else(|_| "-".to_string()),
    ]
}
