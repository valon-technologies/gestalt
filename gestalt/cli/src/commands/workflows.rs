use anyhow::{Context, Result};
use serde_json::{Map, Value, json};

use crate::api::ApiClient;
use crate::cli::WorkflowEventPublishArgs;
use crate::output::{self, Format};
use crate::params::{self, ParamEntry};

const EVENTS_PATH: &str = "/api/v1/workflow/events";
const RUNS_PATH: &str = "/api/v1/workflow/runs";

pub fn list_runs(
    client: &ApiClient,
    plugin: Option<&str>,
    status: Option<&str>,
    format: Format,
) -> Result<()> {
    let resp = client
        .get(RUNS_PATH)
        .context("failed to list workflow runs")?;
    let filtered = filter_runs(resp, plugin, status);
    print_runs(&filtered, format);
    Ok(())
}

pub fn get_run(client: &ApiClient, id: &str, format: Format) -> Result<()> {
    let resp = client
        .get(&format!("{RUNS_PATH}/{id}"))
        .with_context(|| format!("failed to get workflow run {id}"))?;
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
        .with_context(|| format!("failed to cancel workflow run {id}"))?;
    print_run(&resp, format);
    Ok(())
}

pub fn publish_event(
    client: &ApiClient,
    args: &WorkflowEventPublishArgs,
    format: Format,
) -> Result<()> {
    let data = build_optional_map(&args.data, args.data_file.as_deref())?;
    let extensions = if args.extensions.is_empty() {
        None
    } else {
        Some(params::assemble_params(&args.extensions, None, "")?)
    };
    let body = build_event_publish_body(args, data.as_ref(), extensions.as_ref());
    let resp = client
        .post(EVENTS_PATH, &body)
        .context("failed to publish workflow event")?;
    print_published_event(&resp, format);
    Ok(())
}

fn filter_runs(value: Value, plugin: Option<&str>, status: Option<&str>) -> Value {
    let Value::Array(items) = value else {
        return value;
    };
    Value::Array(
        items
            .into_iter()
            .filter(|item| {
                plugin
                    .map(|plugin| target_plugin(item) == Some(plugin))
                    .unwrap_or(true)
                    && status
                        .map(|status| item["status"].as_str() == Some(status))
                        .unwrap_or(true)
            })
            .collect(),
    )
}

fn build_optional_map(
    params: &[ParamEntry],
    input_file: Option<&str>,
) -> Result<Option<Map<String, Value>>> {
    let file_map = match input_file {
        Some(path) => Some(params::load_input_file(path)?),
        None => None,
    };
    let param_map = params::assemble_params(params, None, "")?;

    if file_map.is_none() && param_map.is_empty() {
        return Ok(None);
    }
    let merged = match file_map {
        Some(file) => params::merge_params(file, param_map),
        None => param_map,
    };
    if merged.is_empty() {
        Ok(None)
    } else {
        Ok(Some(merged))
    }
}

fn build_event_publish_body(
    args: &WorkflowEventPublishArgs,
    data: Option<&Map<String, Value>>,
    extensions: Option<&Map<String, Value>>,
) -> Value {
    let mut body = Map::new();
    body.insert("type".to_string(), Value::String(args.event_type.clone()));
    if let Some(value) = args.source.as_deref() {
        body.insert("source".to_string(), Value::String(value.to_string()));
    }
    if let Some(value) = args.subject.as_deref() {
        body.insert("subject".to_string(), Value::String(value.to_string()));
    }
    if let Some(value) = args.id.as_deref() {
        body.insert("id".to_string(), Value::String(value.to_string()));
    }
    if let Some(value) = args.spec_version.as_deref() {
        body.insert("specVersion".to_string(), Value::String(value.to_string()));
    }
    if let Some(value) = args.time.as_deref() {
        body.insert("time".to_string(), Value::String(value.to_string()));
    }
    if let Some(value) = args.data_content_type.as_deref() {
        body.insert(
            "dataContentType".to_string(),
            Value::String(value.to_string()),
        );
    }
    if let Some(data) = data {
        body.insert("data".to_string(), Value::Object(data.clone()));
    }
    if let Some(extensions) = extensions {
        body.insert("extensions".to_string(), Value::Object(extensions.clone()));
    }
    Value::Object(body)
}

fn target_plugin(value: &Value) -> Option<&str> {
    value
        .get("target")?
        .get("plugin")?
        .get("name")
        .and_then(Value::as_str)
}

fn target_operation(value: &Value) -> Option<&str> {
    value
        .get("target")?
        .get("plugin")?
        .get("operation")
        .and_then(Value::as_str)
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

fn print_published_event(value: &Value, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let event = value.get("event").unwrap_or(value);
            let rows = vec![published_event_row(event)];
            output::print_table(&published_event_headers(), &rows);
        }
    }
}

fn run_headers() -> [&'static str; 7] {
    [
        "ID",
        "Plugin",
        "Operation",
        "Status",
        "Trigger",
        "Started",
        "Created",
    ]
}

fn published_event_headers() -> [&'static str; 5] {
    ["ID", "Type", "Source", "Subject", "Time"]
}

fn published_event_row(value: &Value) -> Vec<String> {
    vec![
        value["id"].as_str().unwrap_or("-").to_string(),
        value["type"].as_str().unwrap_or("-").to_string(),
        value["source"].as_str().unwrap_or("-").to_string(),
        value["subject"].as_str().unwrap_or("-").to_string(),
        value["time"].as_str().unwrap_or("-").to_string(),
    ]
}

fn run_row(value: &Value) -> Vec<String> {
    vec![
        value["id"].as_str().unwrap_or("-").to_string(),
        target_plugin(value).unwrap_or("-").to_string(),
        target_operation(value).unwrap_or("-").to_string(),
        value["status"].as_str().unwrap_or("-").to_string(),
        run_trigger_label(value),
        value["startedAt"]
            .as_str()
            .or_else(|| value["completedAt"].as_str())
            .unwrap_or("-")
            .to_string(),
        value["createdAt"].as_str().unwrap_or("-").to_string(),
    ]
}

fn run_trigger_label(value: &Value) -> String {
    let trigger = &value["trigger"];
    match trigger["kind"].as_str() {
        Some("schedule") => trigger["scheduleId"]
            .as_str()
            .map(|id| format!("schedule:{id}"))
            .unwrap_or_else(|| "schedule".to_string()),
        Some("event") => trigger["triggerId"]
            .as_str()
            .map(|id| format!("event:{id}"))
            .unwrap_or_else(|| "event".to_string()),
        Some("manual") => "manual".to_string(),
        Some(other) if !other.is_empty() => other.to_string(),
        _ => "-".to_string(),
    }
}
