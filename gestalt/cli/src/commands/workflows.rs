use anyhow::{Context, Result};
use serde_json::{Map, Value};

use crate::api::ApiClient;
use crate::cli::{
    WorkflowCommands, WorkflowEventCommands, WorkflowEventDeliverArgs, WorkflowRunCommands,
};
use crate::output::{self, Format};
use crate::params::{self, ParamEntry};

use gestalt_sdk::public::generated::app_client::WorkflowClient;
use gestalt_sdk::public::generated::workflow::{
    CancelWorkflowProviderRunRequest, GetWorkflowProviderRunRequest,
    ListWorkflowProviderRunsRequest,
};
use gestalt_sdk::public::rest_transport::SyncRestTransport;
use gestalt_sdk::workflow::WorkflowRunStatus;

use super::workflow_target::{target_app, target_operation};

const EVENTS_PATH: &str = "/api/v1/workflow/events";

pub fn list_runs(
    client: &WorkflowClient<SyncRestTransport>,
    app: Option<&str>,
    status: Option<&str>,
    page_size: Option<u32>,
    page_token: Option<&str>,
    format: Format,
) -> Result<()> {
    let request = ListWorkflowProviderRunsRequest {
        target_app: app.unwrap_or_default().to_string(),
        status: parse_run_status(status),
        page_size: page_size.unwrap_or(0) as i32,
        page_token: page_token.unwrap_or_default().to_string(),
        ..Default::default()
    };
    let resp = client
        .list_runs_sync(request)
        .context("failed to list workflow runs")?;
    print_runs(&serde_json::to_value(&resp)?, format, app);
    Ok(())
}

pub fn get_run(client: &WorkflowClient<SyncRestTransport>, id: &str, format: Format) -> Result<()> {
    let request = GetWorkflowProviderRunRequest {
        run_id: id.to_string(),
        ..Default::default()
    };
    let resp = client
        .get_run_sync(request)
        .with_context(|| format!("failed to get workflow run {id}"))?;
    print_run(&serde_json::to_value(&resp)?, format);
    Ok(())
}

pub fn cancel_run(
    client: &WorkflowClient<SyncRestTransport>,
    id: &str,
    reason: Option<&str>,
    format: Format,
) -> Result<()> {
    let request = CancelWorkflowProviderRunRequest {
        run_id: id.to_string(),
        reason: reason.unwrap_or_default().to_string(),
        ..Default::default()
    };
    let resp = client
        .cancel_run_sync(request)
        .with_context(|| format!("failed to cancel workflow run {id}"))?;
    print_run(&serde_json::to_value(&resp)?, format);
    Ok(())
}

pub fn deliver_event(
    client: &ApiClient,
    args: &WorkflowEventDeliverArgs,
    format: Format,
) -> Result<()> {
    let data = build_optional_map(&args.data, args.data_file.as_deref())?;
    let extensions = if args.extensions.is_empty() {
        None
    } else {
        Some(params::assemble_params(&args.extensions, None, "")?)
    };
    let body = build_event_deliver_body(args, data.as_ref(), extensions.as_ref());
    let resp = client
        .post(EVENTS_PATH, &body)
        .context("failed to deliver workflow event")?;
    print_delivered_event(&resp, format);
    Ok(())
}

fn parse_run_status(value: Option<&str>) -> WorkflowRunStatus {
    use gestalt_sdk::workflow::workflow_run_status::*;
    match value.map(str::trim).filter(|v| !v.is_empty()) {
        Some("pending") => WORKFLOW_RUN_STATUS_PENDING,
        Some("running") => WORKFLOW_RUN_STATUS_RUNNING,
        Some("succeeded") => WORKFLOW_RUN_STATUS_SUCCEEDED,
        Some("failed") => WORKFLOW_RUN_STATUS_FAILED,
        Some("canceled") | Some("cancelled") => WORKFLOW_RUN_STATUS_CANCELED,
        _ => WORKFLOW_RUN_STATUS_UNSPECIFIED,
    }
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

fn build_event_deliver_body(
    args: &WorkflowEventDeliverArgs,
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

fn print_run(value: &Value, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let rows = vec![run_row(value, None)];
            output::print_table(&run_headers(), &rows);
        }
    }
}

fn print_runs(value: &Value, format: Format, preferred_app: Option<&str>) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let items = workflow_run_items(value);
            let rows: Vec<Vec<String>> = items
                .iter()
                .map(|item| run_row(item, preferred_app))
                .collect();
            output::print_table(&run_headers(), &rows);
            if let Some(token) = next_page_token(value) {
                eprintln!("Next page token: {token}");
            }
        }
    }
}

fn workflow_run_items(value: &Value) -> Vec<Value> {
    value
        .get("runs")
        .and_then(Value::as_array)
        .or_else(|| value.as_array())
        .cloned()
        .unwrap_or_default()
}

fn next_page_token(value: &Value) -> Option<&str> {
    value
        .get("nextPageToken")
        .and_then(Value::as_str)
        .map(str::trim)
        .filter(|token| !token.is_empty())
}

fn print_delivered_event(value: &Value, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let event = value.get("event").unwrap_or(value);
            let rows = vec![delivered_event_row(event)];
            output::print_table(&delivered_event_headers(), &rows);
        }
    }
}

fn run_headers() -> [&'static str; 7] {
    [
        "ID",
        "App",
        "Operation",
        "Status",
        "Trigger",
        "Started",
        "Created",
    ]
}

fn delivered_event_headers() -> [&'static str; 5] {
    ["ID", "Type", "Source", "Subject", "Time"]
}

fn delivered_event_row(value: &Value) -> Vec<String> {
    vec![
        value["id"].as_str().unwrap_or("-").to_string(),
        value["type"].as_str().unwrap_or("-").to_string(),
        value["source"].as_str().unwrap_or("-").to_string(),
        value["subject"].as_str().unwrap_or("-").to_string(),
        value["time"].as_str().unwrap_or("-").to_string(),
    ]
}

fn run_row(value: &Value, preferred_app: Option<&str>) -> Vec<String> {
    vec![
        value["id"].as_str().unwrap_or("-").to_string(),
        target_app(value, preferred_app).unwrap_or("-").to_string(),
        target_operation(value, preferred_app)
            .unwrap_or("-")
            .to_string(),
        value["status"].to_string(),
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
        Some("schedule") => trigger["activationId"]
            .as_str()
            .map(|id| format!("schedule:{id}"))
            .unwrap_or_else(|| "schedule".to_string()),
        Some("event") => trigger["activationId"]
            .as_str()
            .map(|id| format!("event:{id}"))
            .unwrap_or_else(|| "event".to_string()),
        Some("manual") => "manual".to_string(),
        Some(other) if !other.is_empty() => other.to_string(),
        _ => "-".to_string(),
    }
}

pub fn dispatch(
    api: &ApiClient,
    workflow: &WorkflowClient<SyncRestTransport>,
    command: WorkflowCommands,
    format: Format,
) -> Result<()> {
    match command {
        WorkflowCommands::Runs { command } => match command {
            WorkflowRunCommands::List {
                app,
                status,
                page_size,
                page_token,
            } => list_runs(
                workflow,
                app.as_deref(),
                status.as_deref(),
                page_size,
                page_token.as_deref(),
                format,
            ),
            WorkflowRunCommands::Get { id } => get_run(workflow, &id, format),
            WorkflowRunCommands::Cancel { id, reason } => {
                cancel_run(workflow, &id, reason.as_deref(), format)
            }
        },
        WorkflowCommands::Events { command } => match command {
            WorkflowEventCommands::Deliver(args) => deliver_event(api, &args, format),
        },
    }
}
