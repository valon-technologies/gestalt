use anyhow::{Context, Result, anyhow};
use serde_json::{Map, Value, json};

use crate::api::ApiClient;
use crate::cli::{
    WorkflowEventPublishArgs, WorkflowScheduleCreateArgs, WorkflowScheduleUpdateArgs,
    WorkflowTriggerCreateArgs, WorkflowTriggerUpdateArgs,
};
use crate::output::{self, Format};
use crate::params::{self, ParamEntry};

use super::workflow_target::{
    AppTargetUpdate, build_app_target, literal_input, merge_app_target_flags,
    resolve_optional_string, target_app, target_has_app, target_operation,
};

const EVENTS_PATH: &str = "/api/v1/workflow/events";
const SCHEDULES_PATH: &str = "/api/v1/workflow/schedules";
const TRIGGERS_PATH: &str = "/api/v1/workflow/event-triggers";
const RUNS_PATH: &str = "/api/v1/workflow/runs";

pub fn list(client: &ApiClient, app: Option<&str>, format: Format) -> Result<()> {
    let resp = client
        .get(SCHEDULES_PATH)
        .context("failed to list workflow schedules")?;
    let filtered = filter_by_app(resp, app);
    print_schedules(&filtered, format, app);
    Ok(())
}

pub fn get(client: &ApiClient, id: &str, format: Format) -> Result<()> {
    let resp = client
        .get(&format!("{SCHEDULES_PATH}/{id}"))
        .with_context(|| format!("failed to get workflow schedule {id}"))?;
    print_schedule(&resp, format);
    Ok(())
}

pub fn create(client: &ApiClient, args: &WorkflowScheduleCreateArgs, format: Format) -> Result<()> {
    let input = build_optional_map(&args.params, args.input_file.as_deref())?;
    let input_value = input.as_ref().map(literal_input);
    let body = build_upsert_body(
        &args.cron,
        args.timezone.as_deref(),
        &args.app,
        &args.operation,
        args.connection.as_deref(),
        args.instance.as_deref(),
        input_value.as_ref(),
        args.paused,
    );

    let resp = client
        .post(SCHEDULES_PATH, &body)
        .context("failed to create workflow schedule")?;
    print_schedule(&resp, format);
    Ok(())
}

pub fn update(client: &ApiClient, args: &WorkflowScheduleUpdateArgs, format: Format) -> Result<()> {
    let existing = client
        .get(&format!("{SCHEDULES_PATH}/{id}", id = args.id))
        .with_context(|| format!("failed to load workflow schedule {}", args.id))?;

    let body = merge_update(args, &existing)?;
    let resp = client
        .put(&format!("{SCHEDULES_PATH}/{id}", id = args.id), &body)
        .with_context(|| format!("failed to update workflow schedule {}", args.id))?;
    print_schedule(&resp, format);
    Ok(())
}

pub fn delete(client: &ApiClient, id: &str, format: Format) -> Result<()> {
    let resp = client
        .delete(&format!("{SCHEDULES_PATH}/{id}"))
        .with_context(|| format!("failed to delete workflow schedule {id}"))?;
    match format {
        Format::Json => output::print_json(&resp),
        Format::Table => output::print_success(&format!("Workflow schedule {id} deleted.")),
    }
    Ok(())
}

pub fn pause(client: &ApiClient, id: &str, format: Format) -> Result<()> {
    let resp = client
        .post(&format!("{SCHEDULES_PATH}/{id}/pause"), &json!({}))
        .with_context(|| format!("failed to pause workflow schedule {id}"))?;
    print_schedule(&resp, format);
    Ok(())
}

pub fn resume(client: &ApiClient, id: &str, format: Format) -> Result<()> {
    let resp = client
        .post(&format!("{SCHEDULES_PATH}/{id}/resume"), &json!({}))
        .with_context(|| format!("failed to resume workflow schedule {id}"))?;
    print_schedule(&resp, format);
    Ok(())
}

pub fn list_triggers(
    client: &ApiClient,
    app: Option<&str>,
    event_type: Option<&str>,
    format: Format,
) -> Result<()> {
    let resp = client
        .get(TRIGGERS_PATH)
        .context("failed to list workflow triggers")?;
    let filtered = filter_triggers(resp, app, event_type);
    print_triggers(&filtered, format, app);
    Ok(())
}

pub fn get_trigger(client: &ApiClient, id: &str, format: Format) -> Result<()> {
    let resp = client
        .get(&format!("{TRIGGERS_PATH}/{id}"))
        .with_context(|| format!("failed to get workflow trigger {id}"))?;
    print_trigger(&resp, format);
    Ok(())
}

pub fn create_trigger(
    client: &ApiClient,
    args: &WorkflowTriggerCreateArgs,
    format: Format,
) -> Result<()> {
    let target = build_trigger_target(args)?;
    let body = build_trigger_upsert_body(
        args.provider.as_deref(),
        &args.event_type,
        args.source.as_deref(),
        args.subject.as_deref(),
        target,
        args.paused,
    );

    let resp = client
        .post(TRIGGERS_PATH, &body)
        .context("failed to create workflow trigger")?;
    print_trigger(&resp, format);
    Ok(())
}

pub fn update_trigger(
    client: &ApiClient,
    args: &WorkflowTriggerUpdateArgs,
    format: Format,
) -> Result<()> {
    let existing = client
        .get(&format!("{TRIGGERS_PATH}/{id}", id = args.id))
        .with_context(|| format!("failed to load workflow trigger {}", args.id))?;

    let body = merge_trigger_update(args, &existing)?;
    let resp = client
        .put(&format!("{TRIGGERS_PATH}/{id}", id = args.id), &body)
        .with_context(|| format!("failed to update workflow trigger {}", args.id))?;
    print_trigger(&resp, format);
    Ok(())
}

pub fn delete_trigger(client: &ApiClient, id: &str, format: Format) -> Result<()> {
    let resp = client
        .delete(&format!("{TRIGGERS_PATH}/{id}"))
        .with_context(|| format!("failed to delete workflow trigger {id}"))?;
    match format {
        Format::Json => output::print_json(&resp),
        Format::Table => output::print_success(&format!("Workflow trigger {id} deleted.")),
    }
    Ok(())
}

pub fn pause_trigger(client: &ApiClient, id: &str, format: Format) -> Result<()> {
    let resp = client
        .post(&format!("{TRIGGERS_PATH}/{id}/pause"), &json!({}))
        .with_context(|| format!("failed to pause workflow trigger {id}"))?;
    print_trigger(&resp, format);
    Ok(())
}

pub fn resume_trigger(client: &ApiClient, id: &str, format: Format) -> Result<()> {
    let resp = client
        .post(&format!("{TRIGGERS_PATH}/{id}/resume"), &json!({}))
        .with_context(|| format!("failed to resume workflow trigger {id}"))?;
    print_trigger(&resp, format);
    Ok(())
}

pub fn list_runs(
    client: &ApiClient,
    app: Option<&str>,
    status: Option<&str>,
    page_size: Option<u32>,
    page_token: Option<&str>,
    format: Format,
) -> Result<()> {
    let path = runs_path(app, status, page_size, page_token)?;
    let resp = client.get(&path).context("failed to list workflow runs")?;
    print_runs(&resp, format, app);
    Ok(())
}

fn runs_path(
    app: Option<&str>,
    status: Option<&str>,
    page_size: Option<u32>,
    page_token: Option<&str>,
) -> Result<String> {
    let mut params = Vec::new();
    push_query_param(&mut params, "app", app);
    push_query_param(&mut params, "status", status);
    if let Some(page_size) = page_size {
        params.push(("pageSize".to_string(), page_size.to_string()));
    }
    push_query_param(&mut params, "pageToken", page_token);
    if params.is_empty() {
        return Ok(RUNS_PATH.to_string());
    }
    Ok(format!(
        "{RUNS_PATH}?{}",
        serde_urlencoded::to_string(params).context("failed to encode query")?
    ))
}

fn push_query_param(params: &mut Vec<(String, String)>, name: &str, value: Option<&str>) {
    if let Some(value) = value.map(str::trim).filter(|value| !value.is_empty()) {
        params.push((name.to_string(), value.to_string()));
    }
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

fn filter_by_app(value: Value, app: Option<&str>) -> Value {
    let Some(app) = app else {
        return value;
    };
    let Value::Array(items) = value else {
        return value;
    };
    Value::Array(
        items
            .into_iter()
            .filter(|item| target_has_app(item, app))
            .collect(),
    )
}

fn filter_triggers(value: Value, app: Option<&str>, event_type: Option<&str>) -> Value {
    let Value::Array(items) = value else {
        return value;
    };
    Value::Array(
        items
            .into_iter()
            .filter(|item| {
                app.map(|app| target_has_app(item, app)).unwrap_or(true)
                    && event_type
                        .map(|event_type| item["match"]["type"].as_str() == Some(event_type))
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

#[allow(clippy::too_many_arguments)]
fn build_upsert_body(
    cron: &str,
    timezone: Option<&str>,
    app: &str,
    operation: &str,
    connection: Option<&str>,
    instance: Option<&str>,
    input: Option<&Value>,
    paused: bool,
) -> Value {
    build_schedule_upsert_body(
        cron,
        timezone,
        build_app_target(app, operation, connection, instance, input),
        paused,
    )
}

fn build_schedule_upsert_body(
    cron: &str,
    timezone: Option<&str>,
    target: Value,
    paused: bool,
) -> Value {
    let mut body = Map::new();
    body.insert("cron".to_string(), Value::String(cron.to_string()));
    if let Some(timezone) = timezone {
        body.insert("timezone".to_string(), Value::String(timezone.to_string()));
    }
    body.insert("target".to_string(), target);
    body.insert("paused".to_string(), Value::Bool(paused));
    Value::Object(body)
}

#[allow(clippy::too_many_arguments)]
fn build_trigger_upsert_body(
    provider: Option<&str>,
    event_type: &str,
    source: Option<&str>,
    subject: Option<&str>,
    target: Value,
    paused: bool,
) -> Value {
    let mut body = Map::new();
    if let Some(provider) = provider {
        let provider = provider.trim();
        if !provider.is_empty() {
            body.insert("provider".to_string(), Value::String(provider.to_string()));
        }
    }
    body.insert(
        "match".to_string(),
        build_event_match(event_type, source, subject),
    );
    body.insert("target".to_string(), target);
    body.insert("paused".to_string(), Value::Bool(paused));
    Value::Object(body)
}

fn build_trigger_target(args: &WorkflowTriggerCreateArgs) -> Result<Value> {
    if let Some(path) = args.target_file.as_deref() {
        if args.app.is_some()
            || args.operation.is_some()
            || args.connection.is_some()
            || args.instance.is_some()
            || !args.params.is_empty()
            || args.input_file.is_some()
        {
            return Err(anyhow!(
                "--target-file cannot be combined with app target flags or input options"
            ));
        }
        let target = params::load_input_file(path)?;
        if target.is_empty() {
            return Err(anyhow!("workflow trigger target file must not be empty"));
        }
        return Ok(Value::Object(target));
    }

    let app = args
        .app
        .as_deref()
        .ok_or_else(|| anyhow!("workflow trigger target requires --app or --target-file"))?;
    let operation = args
        .operation
        .as_deref()
        .ok_or_else(|| anyhow!("workflow trigger app target requires --operation"))?;
    let input = build_optional_map(&args.params, args.input_file.as_deref())?;
    let input_value = input.as_ref().map(literal_input);
    Ok(build_app_target(
        app,
        operation,
        args.connection.as_deref(),
        args.instance.as_deref(),
        input_value.as_ref(),
    ))
}

fn merge_update(args: &WorkflowScheduleUpdateArgs, existing: &Value) -> Result<Value> {
    let cron = match args.cron.as_deref() {
        Some(value) => value.to_string(),
        None => existing["cron"]
            .as_str()
            .ok_or_else(|| anyhow!("existing schedule is missing cron; pass --cron"))?
            .to_string(),
    };

    let timezone = resolve_optional_string(args.timezone.as_deref(), existing["timezone"].as_str());

    let target = merge_schedule_target_update(args, existing)?;

    let paused = if args.paused {
        true
    } else if args.unpaused {
        false
    } else {
        existing["paused"].as_bool().unwrap_or(false)
    };

    Ok(build_schedule_upsert_body(
        &cron,
        timezone.as_deref(),
        target,
        paused,
    ))
}

fn merge_trigger_update(args: &WorkflowTriggerUpdateArgs, existing: &Value) -> Result<Value> {
    let event_type = match args.event_type.as_deref() {
        Some(value) => value.to_string(),
        None => existing["match"]["type"]
            .as_str()
            .ok_or_else(|| anyhow!("existing trigger is missing match.type; pass --type"))?
            .to_string(),
    };
    let source =
        resolve_optional_string(args.source.as_deref(), existing["match"]["source"].as_str());
    let subject = resolve_optional_string(
        args.subject.as_deref(),
        existing["match"]["subject"].as_str(),
    );
    let target = merge_trigger_target_update(args, existing)?;

    let paused = if args.paused {
        true
    } else if args.unpaused {
        false
    } else {
        existing["paused"].as_bool().unwrap_or(false)
    };

    Ok(build_trigger_upsert_body(
        existing["provider"].as_str(),
        &event_type,
        source.as_deref(),
        subject.as_deref(),
        target,
        paused,
    ))
}

fn merge_schedule_target_update(
    args: &WorkflowScheduleUpdateArgs,
    existing: &Value,
) -> Result<Value> {
    let (input, replace_input) = target_input_update(&args.params, args.input_file.as_deref())?;
    merge_app_target_flags(
        existing,
        AppTargetUpdate {
            resource: "schedule",
            app: args.app.as_deref(),
            operation: args.operation.as_deref(),
            connection: args.connection.as_deref(),
            instance: args.instance.as_deref(),
            clear_input: args.clear_input,
            replace_input,
            input: input.as_ref(),
        },
    )
}

fn merge_trigger_target_update(
    args: &WorkflowTriggerUpdateArgs,
    existing: &Value,
) -> Result<Value> {
    let (input, replace_input) = target_input_update(&args.params, args.input_file.as_deref())?;
    merge_app_target_flags(
        existing,
        AppTargetUpdate {
            resource: "trigger",
            app: args.app.as_deref(),
            operation: args.operation.as_deref(),
            connection: args.connection.as_deref(),
            instance: args.instance.as_deref(),
            clear_input: args.clear_input,
            replace_input,
            input: input.as_ref(),
        },
    )
}

fn target_input_update(
    params: &[ParamEntry],
    input_file: Option<&str>,
) -> Result<(Option<Value>, bool)> {
    let replace_input = !params.is_empty() || input_file.is_some();
    let input = if replace_input {
        build_optional_map(params, input_file)?.map(|input| literal_input(&input))
    } else {
        None
    };
    Ok((input, replace_input))
}

fn build_event_match(event_type: &str, source: Option<&str>, subject: Option<&str>) -> Value {
    let mut match_body = Map::new();
    match_body.insert("type".to_string(), Value::String(event_type.to_string()));
    if let Some(source) = source {
        match_body.insert("source".to_string(), Value::String(source.to_string()));
    }
    if let Some(subject) = subject {
        match_body.insert("subject".to_string(), Value::String(subject.to_string()));
    }
    Value::Object(match_body)
}

fn print_schedule(value: &Value, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let rows = vec![schedule_row(value, None)];
            output::print_table(&schedule_headers(), &rows);
        }
    }
}

fn print_schedules(value: &Value, format: Format, preferred_app: Option<&str>) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let items = value.as_array().cloned().unwrap_or_default();
            let rows: Vec<Vec<String>> = items
                .iter()
                .map(|item| schedule_row(item, preferred_app))
                .collect();
            output::print_table(&schedule_headers(), &rows);
        }
    }
}

fn print_trigger(value: &Value, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let rows = vec![trigger_row(value, None)];
            output::print_table(&trigger_headers(), &rows);
        }
    }
}

fn print_triggers(value: &Value, format: Format, preferred_app: Option<&str>) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let items = value.as_array().cloned().unwrap_or_default();
            let rows: Vec<Vec<String>> = items
                .iter()
                .map(|item| trigger_row(item, preferred_app))
                .collect();
            output::print_table(&trigger_headers(), &rows);
        }
    }
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

fn trigger_headers() -> [&'static str; 8] {
    [
        "ID",
        "Type",
        "Source",
        "Subject",
        "App",
        "Operation",
        "Paused",
        "Created",
    ]
}

fn trigger_row(value: &Value, preferred_app: Option<&str>) -> Vec<String> {
    vec![
        value["id"].as_str().unwrap_or("-").to_string(),
        value["match"]["type"].as_str().unwrap_or("-").to_string(),
        value["match"]["source"].as_str().unwrap_or("-").to_string(),
        value["match"]["subject"]
            .as_str()
            .unwrap_or("-")
            .to_string(),
        target_app(value, preferred_app).unwrap_or("-").to_string(),
        target_operation(value, preferred_app)
            .unwrap_or("-")
            .to_string(),
        format_bool(value["paused"].as_bool()),
        value["createdAt"].as_str().unwrap_or("-").to_string(),
    ]
}

fn schedule_headers() -> [&'static str; 8] {
    [
        "ID",
        "App",
        "Operation",
        "Cron",
        "TZ",
        "Paused",
        "Next Run",
        "Created",
    ]
}

fn schedule_row(value: &Value, preferred_app: Option<&str>) -> Vec<String> {
    vec![
        value["id"].as_str().unwrap_or("-").to_string(),
        target_app(value, preferred_app).unwrap_or("-").to_string(),
        target_operation(value, preferred_app)
            .unwrap_or("-")
            .to_string(),
        value["cron"].as_str().unwrap_or("-").to_string(),
        value["timezone"].as_str().unwrap_or("-").to_string(),
        format_bool(value["paused"].as_bool()),
        value["nextRunAt"].as_str().unwrap_or("-").to_string(),
        value["createdAt"].as_str().unwrap_or("-").to_string(),
    ]
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

fn run_row(value: &Value, preferred_app: Option<&str>) -> Vec<String> {
    vec![
        value["id"].as_str().unwrap_or("-").to_string(),
        target_app(value, preferred_app).unwrap_or("-").to_string(),
        target_operation(value, preferred_app)
            .unwrap_or("-")
            .to_string(),
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

fn format_bool(value: Option<bool>) -> String {
    match value {
        Some(true) => "yes".to_string(),
        Some(false) => "no".to_string(),
        None => "-".to_string(),
    }
}
