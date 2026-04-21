use anyhow::{Context, Result, anyhow};
use serde_json::{Map, Value, json};

use crate::api::ApiClient;
use crate::cli::{WorkflowScheduleCreateArgs, WorkflowScheduleUpdateArgs};
use crate::output::{self, Format};
use crate::params::{self, ParamEntry};

const SCHEDULES_PATH: &str = "/api/v1/workflow/schedules";
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
    let input = build_target_input(&args.params, args.input_file.as_deref())?;
    let body = build_upsert_body(
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
            .filter(|item| item["target"]["plugin"].as_str() == Some(plugin))
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
                    .map(|plugin| item["target"]["plugin"].as_str() == Some(plugin))
                    .unwrap_or(true)
                    && status
                        .map(|status| item["status"].as_str() == Some(status))
                        .unwrap_or(true)
            })
            .collect(),
    )
}

fn build_target_input(
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

#[allow(clippy::too_many_arguments)]
fn build_upsert_body(
    cron: &str,
    timezone: Option<&str>,
    plugin: &str,
    operation: &str,
    connection: Option<&str>,
    instance: Option<&str>,
    input: Option<&Map<String, Value>>,
    paused: bool,
) -> Value {
    let mut target = Map::new();
    target.insert("plugin".to_string(), Value::String(plugin.to_string()));
    target.insert(
        "operation".to_string(),
        Value::String(operation.to_string()),
    );
    if let Some(connection) = connection {
        target.insert(
            "connection".to_string(),
            Value::String(connection.to_string()),
        );
    }
    if let Some(instance) = instance {
        target.insert("instance".to_string(), Value::String(instance.to_string()));
    }
    if let Some(input) = input {
        target.insert("input".to_string(), Value::Object(input.clone()));
    }

    let mut body = Map::new();
    body.insert("cron".to_string(), Value::String(cron.to_string()));
    if let Some(timezone) = timezone {
        body.insert("timezone".to_string(), Value::String(timezone.to_string()));
    }
    body.insert("target".to_string(), Value::Object(target));
    body.insert("paused".to_string(), Value::Bool(paused));
    Value::Object(body)
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

    let plugin = match args.plugin.as_deref() {
        Some(value) => value.to_string(),
        None => existing["target"]["plugin"]
            .as_str()
            .ok_or_else(|| anyhow!("existing schedule is missing target.plugin; pass --plugin"))?
            .to_string(),
    };
    let operation = match args.operation.as_deref() {
        Some(value) => value.to_string(),
        None => existing["target"]["operation"]
            .as_str()
            .ok_or_else(|| {
                anyhow!("existing schedule is missing target.operation; pass --operation")
            })?
            .to_string(),
    };
    let connection = resolve_optional_string(
        args.connection.as_deref(),
        existing["target"]["connection"].as_str(),
    );
    let instance = resolve_optional_string(
        args.instance.as_deref(),
        existing["target"]["instance"].as_str(),
    );

    let input = if args.clear_input {
        None
    } else if !args.params.is_empty() || args.input_file.is_some() {
        build_target_input(&args.params, args.input_file.as_deref())?
    } else {
        existing["target"]["input"].as_object().cloned()
    };

    let paused = if args.paused {
        true
    } else if args.unpaused {
        false
    } else {
        existing["paused"].as_bool().unwrap_or(false)
    };

    Ok(build_upsert_body(
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

fn resolve_optional_string(arg: Option<&str>, existing: Option<&str>) -> Option<String> {
    match arg {
        Some("") => None,
        Some(value) => Some(value.to_string()),
        None => existing.map(str::to_string),
    }
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
        value["target"]["plugin"]
            .as_str()
            .unwrap_or("-")
            .to_string(),
        value["target"]["operation"]
            .as_str()
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
        "Plugin",
        "Operation",
        "Status",
        "Trigger",
        "Started",
        "Created",
    ]
}

fn run_row(value: &Value) -> Vec<String> {
    vec![
        value["id"].as_str().unwrap_or("-").to_string(),
        value["target"]["plugin"]
            .as_str()
            .unwrap_or("-")
            .to_string(),
        value["target"]["operation"]
            .as_str()
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
