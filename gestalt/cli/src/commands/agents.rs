use anyhow::{Context, Result};
use serde_json::{Map, Value, json};

use crate::api::ApiClient;
use crate::cli::{AgentRunCreateArgs, AgentToolArg};
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

    Ok(Value::Object(body))
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

fn run_headers() -> [&'static str; 6] {
    ["ID", "Provider", "Model", "Status", "Session", "Created"]
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
