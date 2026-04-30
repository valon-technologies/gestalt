use anyhow::{Context, Result, anyhow};
use serde_json::{Map, Value, json};

use crate::api::ApiClient;
use crate::cli::{
    WorkflowEventPublishArgs, WorkflowScheduleCreateArgs, WorkflowScheduleUpdateArgs,
    WorkflowTriggerCreateArgs, WorkflowTriggerUpdateArgs,
};
use crate::output::{self, Format};
use crate::params::{self, ParamEntry};

const EVENTS_PATH: &str = "/api/v1/workflow/events";
const SCHEDULES_PATH: &str = "/api/v1/workflow/schedules";
const TRIGGERS_PATH: &str = "/api/v1/workflow/event-triggers";
const RUNS_PATH: &str = "/api/v1/workflow/runs";

pub fn list(client: &ApiClient, plugin: Option<&str>, format: Format) -> Result<()> {
    let resp = client
        .get(SCHEDULES_PATH)
        .context("failed to list workflow schedules")?;
    let filtered = filter_by_plugin(resp, plugin);
    print_schedules(&filtered, format);
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
    let body = build_upsert_body(
        args.provider.as_deref(),
        &args.cron,
        args.timezone.as_deref(),
        &args.plugin,
        &args.operation,
        args.connection.as_deref(),
        args.instance.as_deref(),
        input.as_ref(),
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
    plugin: Option<&str>,
    event_type: Option<&str>,
    format: Format,
) -> Result<()> {
    let resp = client
        .get(TRIGGERS_PATH)
        .context("failed to list workflow triggers")?;
    let filtered = filter_triggers(resp, plugin, event_type);
    print_triggers(&filtered, format);
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
    let input = build_optional_map(&args.params, args.input_file.as_deref())?;
    let body = build_trigger_upsert_body(
        args.provider.as_deref(),
        &args.event_type,
        args.source.as_deref(),
        args.subject.as_deref(),
        &args.plugin,
        &args.operation,
        args.connection.as_deref(),
        args.instance.as_deref(),
        input.as_ref(),
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

fn filter_by_plugin(value: Value, plugin: Option<&str>) -> Value {
    let Some(plugin) = plugin else {
        return value;
    };
    let Value::Array(items) = value else {
        return value;
    };
    Value::Array(
        items
            .into_iter()
            .filter(|item| target_plugin(item) == Some(plugin))
            .collect(),
    )
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

fn filter_triggers(value: Value, plugin: Option<&str>, event_type: Option<&str>) -> Value {
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
    provider: Option<&str>,
    cron: &str,
    timezone: Option<&str>,
    plugin: &str,
    operation: &str,
    connection: Option<&str>,
    instance: Option<&str>,
    input: Option<&Map<String, Value>>,
    paused: bool,
) -> Value {
    let mut body = Map::new();
    if let Some(provider) = provider {
        let provider = provider.trim();
        if !provider.is_empty() {
            body.insert("provider".to_string(), Value::String(provider.to_string()));
        }
    }
    body.insert("cron".to_string(), Value::String(cron.to_string()));
    if let Some(timezone) = timezone {
        body.insert("timezone".to_string(), Value::String(timezone.to_string()));
    }
    body.insert(
        "target".to_string(),
        build_target_object(plugin, operation, connection, instance, input),
    );
    body.insert("paused".to_string(), Value::Bool(paused));
    Value::Object(body)
}

#[allow(clippy::too_many_arguments)]
fn build_trigger_upsert_body(
    provider: Option<&str>,
    event_type: &str,
    source: Option<&str>,
    subject: Option<&str>,
    plugin: &str,
    operation: &str,
    connection: Option<&str>,
    instance: Option<&str>,
    input: Option<&Map<String, Value>>,
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
    body.insert(
        "target".to_string(),
        build_target_object(plugin, operation, connection, instance, input),
    );
    body.insert("paused".to_string(), Value::Bool(paused));
    Value::Object(body)
}

fn merge_update(args: &WorkflowScheduleUpdateArgs, existing: &Value) -> Result<Value> {
    let provider = resolve_optional_string(args.provider.as_deref(), existing["provider"].as_str());
    let cron = match args.cron.as_deref() {
        Some(value) => value.to_string(),
        None => existing["cron"]
            .as_str()
            .ok_or_else(|| anyhow!("existing schedule is missing cron; pass --cron"))?
            .to_string(),
    };

    let timezone = resolve_optional_string(args.timezone.as_deref(), existing["timezone"].as_str());

    let plugin = match args.plugin.as_deref() {
        Some(value) => value.to_string(),
        None => target_plugin(existing)
            .ok_or_else(|| {
                anyhow!("existing schedule is missing target.plugin.name; pass --plugin")
            })?
            .to_string(),
    };
    let operation = match args.operation.as_deref() {
        Some(value) => value.to_string(),
        None => target_operation(existing)
            .ok_or_else(|| {
                anyhow!("existing schedule is missing target.plugin.operation; pass --operation")
            })?
            .to_string(),
    };
    let connection =
        resolve_optional_string(args.connection.as_deref(), target_connection(existing));
    let instance = resolve_optional_string(args.instance.as_deref(), target_instance(existing));

    let input = if args.clear_input {
        None
    } else if !args.params.is_empty() || args.input_file.is_some() {
        build_optional_map(&args.params, args.input_file.as_deref())?
    } else {
        target_input(existing).cloned()
    };

    let paused = if args.paused {
        true
    } else if args.unpaused {
        false
    } else {
        existing["paused"].as_bool().unwrap_or(false)
    };

    Ok(build_upsert_body(
        provider.as_deref(),
        &cron,
        timezone.as_deref(),
        &plugin,
        &operation,
        connection.as_deref(),
        instance.as_deref(),
        input.as_ref(),
        paused,
    ))
}

fn merge_trigger_update(args: &WorkflowTriggerUpdateArgs, existing: &Value) -> Result<Value> {
    let provider = resolve_optional_string(args.provider.as_deref(), existing["provider"].as_str());
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
    let plugin = match args.plugin.as_deref() {
        Some(value) => value.to_string(),
        None => target_plugin(existing)
            .ok_or_else(|| {
                anyhow!("existing trigger is missing target.plugin.name; pass --plugin")
            })?
            .to_string(),
    };
    let operation = match args.operation.as_deref() {
        Some(value) => value.to_string(),
        None => target_operation(existing)
            .ok_or_else(|| {
                anyhow!("existing trigger is missing target.plugin.operation; pass --operation")
            })?
            .to_string(),
    };
    let connection =
        resolve_optional_string(args.connection.as_deref(), target_connection(existing));
    let instance = resolve_optional_string(args.instance.as_deref(), target_instance(existing));

    let input = if args.clear_input {
        None
    } else if !args.params.is_empty() || args.input_file.is_some() {
        build_optional_map(&args.params, args.input_file.as_deref())?
    } else {
        target_input(existing).cloned()
    };

    let paused = if args.paused {
        true
    } else if args.unpaused {
        false
    } else {
        existing["paused"].as_bool().unwrap_or(false)
    };

    Ok(build_trigger_upsert_body(
        provider.as_deref(),
        &event_type,
        source.as_deref(),
        subject.as_deref(),
        &plugin,
        &operation,
        connection.as_deref(),
        instance.as_deref(),
        input.as_ref(),
        paused,
    ))
}

fn resolve_optional_string(arg: Option<&str>, existing: Option<&str>) -> Option<String> {
    match arg {
        Some("") => None,
        Some(value) => Some(value.to_string()),
        None => existing.map(str::to_string),
    }
}

fn build_target_object(
    plugin: &str,
    operation: &str,
    connection: Option<&str>,
    instance: Option<&str>,
    input: Option<&Map<String, Value>>,
) -> Value {
    let mut plugin_target = Map::new();
    plugin_target.insert("name".to_string(), Value::String(plugin.to_string()));
    plugin_target.insert(
        "operation".to_string(),
        Value::String(operation.to_string()),
    );
    if let Some(connection) = connection {
        plugin_target.insert(
            "connection".to_string(),
            Value::String(connection.to_string()),
        );
    }
    if let Some(instance) = instance {
        plugin_target.insert("instance".to_string(), Value::String(instance.to_string()));
    }
    if let Some(input) = input {
        plugin_target.insert("input".to_string(), Value::Object(input.clone()));
    }
    let mut target = Map::new();
    target.insert("plugin".to_string(), Value::Object(plugin_target));
    Value::Object(target)
}

fn target_plugin(value: &Value) -> Option<&str> {
    value
        .get("target")?
        .get("plugin")?
        .get("name")
        .and_then(Value::as_str)
}

fn target_operation(value: &Value) -> Option<&str> {
    target_plugin_field(value, "operation")
}

fn target_connection(value: &Value) -> Option<&str> {
    target_plugin_field(value, "connection")
}

fn target_instance(value: &Value) -> Option<&str> {
    target_plugin_field(value, "instance")
}

fn target_input(value: &Value) -> Option<&Map<String, Value>> {
    value
        .get("target")?
        .get("plugin")?
        .get("input")
        .and_then(Value::as_object)
}

fn target_plugin_field<'a>(value: &'a Value, field: &str) -> Option<&'a str> {
    value
        .get("target")?
        .get("plugin")?
        .get(field)
        .and_then(Value::as_str)
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
            let rows = vec![schedule_row(value)];
            output::print_table(&schedule_headers(), &rows);
        }
    }
}

fn print_schedules(value: &Value, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let items = value.as_array().cloned().unwrap_or_default();
            let rows: Vec<Vec<String>> = items.iter().map(schedule_row).collect();
            output::print_table(&schedule_headers(), &rows);
        }
    }
}

fn print_trigger(value: &Value, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let rows = vec![trigger_row(value)];
            output::print_table(&trigger_headers(), &rows);
        }
    }
}

fn print_triggers(value: &Value, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let items = value.as_array().cloned().unwrap_or_default();
            let rows: Vec<Vec<String>> = items.iter().map(trigger_row).collect();
            output::print_table(&trigger_headers(), &rows);
        }
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
        "Plugin",
        "Operation",
        "Paused",
        "Created",
    ]
}

fn trigger_row(value: &Value) -> Vec<String> {
    vec![
        value["id"].as_str().unwrap_or("-").to_string(),
        value["match"]["type"].as_str().unwrap_or("-").to_string(),
        value["match"]["source"].as_str().unwrap_or("-").to_string(),
        value["match"]["subject"]
            .as_str()
            .unwrap_or("-")
            .to_string(),
        target_plugin(value).unwrap_or("-").to_string(),
        target_operation(value).unwrap_or("-").to_string(),
        format_bool(value["paused"].as_bool()),
        value["createdAt"].as_str().unwrap_or("-").to_string(),
    ]
}

fn schedule_headers() -> [&'static str; 8] {
    [
        "ID",
        "Plugin",
        "Operation",
        "Cron",
        "TZ",
        "Paused",
        "Next Run",
        "Created",
    ]
}

fn schedule_row(value: &Value) -> Vec<String> {
    vec![
        value["id"].as_str().unwrap_or("-").to_string(),
        target_plugin(value).unwrap_or("-").to_string(),
        target_operation(value).unwrap_or("-").to_string(),
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

fn format_bool(value: Option<bool>) -> String {
    match value {
        Some(true) => "yes".to_string(),
        Some(false) => "no".to_string(),
        None => "-".to_string(),
    }
}
