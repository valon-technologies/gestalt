use anyhow::{Result, anyhow};
use serde_json::{Map, Value};

type JsonMap = Map<String, Value>;
type SingleAppStep<'a> = (&'a JsonMap, &'a JsonMap);

struct AppTargetBuild<'a> {
    existing_step: Option<&'a JsonMap>,
    app: &'a str,
    operation: &'a str,
    connection: Option<&'a str>,
    instance: Option<&'a str>,
    input: Option<&'a Value>,
    existing: Option<&'a Value>,
    step_id: Option<&'a str>,
}

pub(super) struct AppTargetUpdate<'a> {
    pub(super) resource: &'static str,
    pub(super) step_id: Option<&'a str>,
    pub(super) app: Option<&'a str>,
    pub(super) operation: Option<&'a str>,
    pub(super) connection: Option<&'a str>,
    pub(super) instance: Option<&'a str>,
    pub(super) clear_input: bool,
    pub(super) replace_input: bool,
    pub(super) input: Option<&'a Value>,
}

impl AppTargetUpdate<'_> {
    pub(super) fn has_overrides(&self) -> bool {
        self.app.is_some()
            || self.operation.is_some()
            || self.connection.is_some()
            || self.instance.is_some()
            || self.clear_input
            || self.replace_input
    }
}

pub(super) fn merge_app_target_flags(
    existing: &Value,
    update: AppTargetUpdate<'_>,
) -> Result<Value> {
    if !update.has_overrides() {
        return existing
            .get("target")
            .cloned()
            .ok_or_else(|| anyhow!("existing {} is missing target", update.resource));
    }

    let (step, app_step) = target_app_step_for_update(existing, update.step_id).ok_or_else(|| {
        if update.step_id.is_some() {
            return anyhow!(
                "existing {} has no app step with id {:?}",
                update.resource,
                update.step_id
            );
        }
        anyhow!(
            "cannot apply app target flags to an existing non-app or multi-step {}; pass --step-id to update a specific step or recreate it with a full target definition",
            update.resource
        )
    })?;
    let app = match update.app {
        Some(value) => value.to_string(),
        None => app_step
            .get("name")
            .and_then(Value::as_str)
            .ok_or_else(|| {
                anyhow!(
                    "existing {} is missing target.steps app name; pass --app",
                    update.resource
                )
            })?
            .to_string(),
    };
    let operation = match update.operation {
        Some(value) => value.to_string(),
        None => app_step
            .get("operation")
            .and_then(Value::as_str)
            .ok_or_else(|| {
                anyhow!(
                    "existing {} is missing target.steps app operation; pass --operation",
                    update.resource
                )
            })?
            .to_string(),
    };
    let connection = resolve_optional_string(
        update.connection,
        app_step.get("connection").and_then(Value::as_str),
    );
    let instance = resolve_optional_string(
        update.instance,
        app_step.get("instance").and_then(Value::as_str),
    );
    let input = if update.clear_input {
        None
    } else if update.replace_input {
        update.input.cloned()
    } else {
        app_step.get("input").cloned()
    };

    Ok(build_app_target_from_step(AppTargetBuild {
        existing_step: Some(step),
        app: &app,
        operation: &operation,
        connection: connection.as_deref(),
        instance: instance.as_deref(),
        input: input.as_ref(),
        existing: Some(existing),
        step_id: update.step_id,
    }))
}

pub(super) fn build_app_target(
    app: &str,
    operation: &str,
    connection: Option<&str>,
    instance: Option<&str>,
    input: Option<&Value>,
) -> Value {
    build_app_target_from_step(AppTargetBuild {
        existing_step: None,
        app,
        operation,
        connection,
        instance,
        input,
        existing: None,
        step_id: None,
    })
}

fn build_app_target_from_step(args: AppTargetBuild<'_>) -> Value {
    let AppTargetBuild {
        existing_step,
        app,
        operation,
        connection,
        instance,
        input,
        existing,
        step_id,
    } = args;

    let mut step = existing_step.cloned().unwrap_or_default();
    let mut app_target = existing_step
        .and_then(|step| step.get("app"))
        .and_then(Value::as_object)
        .cloned()
        .unwrap_or_default();
    app_target.insert("name".to_string(), Value::String(app.to_string()));
    app_target.insert(
        "operation".to_string(),
        Value::String(operation.to_string()),
    );
    if let Some(connection) = connection {
        app_target.insert(
            "connection".to_string(),
            Value::String(connection.to_string()),
        );
    } else {
        app_target.remove("connection");
    }
    if let Some(instance) = instance {
        app_target.insert("instance".to_string(), Value::String(instance.to_string()));
    } else {
        app_target.remove("instance");
    }
    if let Some(input) = input {
        app_target.insert("input".to_string(), input.clone());
    } else {
        app_target.remove("input");
    }
    if !matches!(step.get("id"), Some(Value::String(value)) if !value.is_empty()) {
        step.insert("id".to_string(), Value::String(operation.to_string()));
    }
    step.remove("agent");
    step.insert("app".to_string(), Value::Object(app_target));

    if let (Some(existing), Some(step_id)) = (existing, step_id) {
        let mut steps = existing
            .get("target")
            .and_then(|target| target.get("steps"))
            .and_then(Value::as_array)
            .cloned()
            .unwrap_or_default();
        let mut replaced = false;
        for item in steps.iter_mut() {
            let Some(step_map) = item.as_object_mut() else {
                continue;
            };
            if step_map.get("id").and_then(Value::as_str) == Some(step_id) {
                *step_map = step.clone();
                replaced = true;
                break;
            }
        }
        if replaced {
            let mut target = Map::new();
            target.insert("steps".to_string(), Value::Array(steps));
            return Value::Object(target);
        }
    }

    let mut target = Map::new();
    target.insert("steps".to_string(), Value::Array(vec![Value::Object(step)]));
    Value::Object(target)
}

fn target_app_step_for_update<'a>(
    value: &'a Value,
    step_id: Option<&str>,
) -> Option<SingleAppStep<'a>> {
    if let Some(step_id) = step_id {
        let steps = value.get("target")?.get("steps")?.as_array()?;
        for step in steps {
            let Some(step_map) = step.as_object() else {
                continue;
            };
            if step_map.get("id").and_then(Value::as_str) != Some(step_id) {
                continue;
            }
            if step_map.get("agent").is_some() {
                return None;
            }
            let app = step_map.get("app").and_then(Value::as_object)?;
            return Some((step_map, app));
        }
        return None;
    }
    target_single_app_step(value)
}

pub(super) fn literal_input(input: &Map<String, Value>) -> Value {
    let mut value = Map::new();
    value.insert("literal".to_string(), Value::Object(input.clone()));
    Value::Object(value)
}

pub(super) fn target_app<'a>(value: &'a Value, preferred_app: Option<&str>) -> Option<&'a str> {
    target_app_step(value, preferred_app)?
        .get("name")
        .and_then(Value::as_str)
}

pub(super) fn target_operation<'a>(
    value: &'a Value,
    preferred_app: Option<&str>,
) -> Option<&'a str> {
    target_app_field(value, preferred_app, "operation")
}

pub(super) fn target_has_app(value: &Value, app: &str) -> bool {
    value
        .get("target")
        .and_then(|target| target.get("steps"))
        .and_then(Value::as_array)
        .map(|steps| {
            steps.iter().any(|step| {
                step.get("app")
                    .and_then(Value::as_object)
                    .and_then(|app_step| app_step.get("name"))
                    .and_then(Value::as_str)
                    == Some(app)
            })
        })
        .unwrap_or(false)
}

fn target_single_app_step(value: &Value) -> Option<SingleAppStep<'_>> {
    let steps = value.get("target")?.get("steps")?.as_array()?;
    if steps.len() != 1 {
        return None;
    }
    let step = steps.first()?.as_object()?;
    if step.get("agent").is_some() {
        return None;
    }
    let app = step.get("app").and_then(Value::as_object)?;
    Some((step, app))
}

fn target_app_step<'a>(
    value: &'a Value,
    preferred_app: Option<&str>,
) -> Option<&'a Map<String, Value>> {
    let steps = value.get("target")?.get("steps")?.as_array()?;
    let mut first_app_step = None;
    for step in steps {
        let Some(app_step) = step.get("app").and_then(Value::as_object) else {
            continue;
        };
        if first_app_step.is_none() {
            first_app_step = Some(app_step);
        }
        if preferred_app.is_some_and(|preferred_app| {
            app_step.get("name").and_then(Value::as_str) == Some(preferred_app)
        }) {
            return Some(app_step);
        }
    }
    first_app_step
}

fn target_app_field<'a>(
    value: &'a Value,
    preferred_app: Option<&str>,
    field: &str,
) -> Option<&'a str> {
    target_app_step(value, preferred_app)?
        .get(field)
        .and_then(Value::as_str)
}

pub(super) fn resolve_optional_string(arg: Option<&str>, existing: Option<&str>) -> Option<String> {
    match arg {
        Some("") => None,
        Some(value) => Some(value.to_string()),
        None => existing.map(str::to_string),
    }
}

#[cfg(test)]
mod tests {
    use serde_json::json;

    use super::*;

    #[test]
    fn target_app_fields_prefer_matching_app_step() {
        let value = json!({
            "target": {
                "steps": [
                    {
                        "id": "first",
                        "app": {
                            "name": "github",
                            "operation": "createIssue"
                        }
                    },
                    {
                        "id": "second",
                        "app": {
                            "name": "slack",
                            "operation": "reply"
                        }
                    }
                ]
            }
        });

        assert_eq!(target_app(&value, None), Some("github"));
        assert_eq!(target_operation(&value, None), Some("createIssue"));
        assert_eq!(target_app(&value, Some("slack")), Some("slack"));
        assert_eq!(target_operation(&value, Some("slack")), Some("reply"));
        assert_eq!(target_app(&value, Some("jira")), Some("github"));
        assert_eq!(target_operation(&value, Some("jira")), Some("createIssue"));
    }

    #[test]
    fn merge_app_target_flags_updates_matching_step_in_multi_step_target() {
        let existing = json!({
            "target": {
                "steps": [
                    {
                        "id": "first",
                        "app": {
                            "name": "github",
                            "operation": "createIssue",
                            "input": {"literal": {"title": "old"}}
                        }
                    },
                    {
                        "id": "notify",
                        "app": {
                            "name": "slack",
                            "operation": "reply",
                            "input": {"literal": {"text": "hello"}}
                        }
                    }
                ]
            }
        });
        let update = AppTargetUpdate {
            resource: "schedule",
            step_id: Some("notify"),
            app: Some("slack"),
            operation: Some("postMessage"),
            connection: None,
            instance: None,
            clear_input: false,
            replace_input: false,
            input: None,
        };

        let merged = merge_app_target_flags(&existing, update).expect("merge target");
        let steps = merged
            .get("steps")
            .and_then(Value::as_array)
            .expect("steps");
        assert_eq!(steps.len(), 2);
        assert_eq!(
            steps[0]
                .get("app")
                .and_then(|app| app.get("operation"))
                .and_then(Value::as_str),
            Some("createIssue")
        );
        assert_eq!(
            steps[1]
                .get("app")
                .and_then(|app| app.get("operation"))
                .and_then(Value::as_str),
            Some("postMessage")
        );
    }

    #[test]
    fn merge_app_target_flags_skips_non_object_steps_when_matching_by_step_id() {
        let existing = json!({
            "target": {
                "steps": [
                    "not-a-step",
                    {
                        "id": "notify",
                        "app": {
                            "name": "slack",
                            "operation": "reply"
                        }
                    }
                ]
            }
        });
        let update = AppTargetUpdate {
            resource: "trigger",
            step_id: Some("notify"),
            app: Some("slack"),
            operation: Some("postMessage"),
            connection: None,
            instance: None,
            clear_input: false,
            replace_input: false,
            input: None,
        };

        let merged = merge_app_target_flags(&existing, update).expect("merge target");
        let steps = merged
            .get("steps")
            .and_then(Value::as_array)
            .expect("steps");
        assert_eq!(steps[0], json!("not-a-step"));
        assert_eq!(
            steps[1]
                .get("app")
                .and_then(|app| app.get("operation"))
                .and_then(Value::as_str),
            Some("postMessage")
        );
    }
}
