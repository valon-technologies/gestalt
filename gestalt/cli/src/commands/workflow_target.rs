use anyhow::{Result, anyhow};
use serde_json::{Map, Value};

type JsonMap = Map<String, Value>;
type SinglePluginStep<'a> = (&'a JsonMap, &'a JsonMap);

pub(super) struct PluginTargetUpdate<'a> {
    pub(super) resource: &'static str,
    pub(super) plugin: Option<&'a str>,
    pub(super) operation: Option<&'a str>,
    pub(super) connection: Option<&'a str>,
    pub(super) instance: Option<&'a str>,
    pub(super) clear_input: bool,
    pub(super) replace_input: bool,
    pub(super) input: Option<&'a Value>,
}

impl PluginTargetUpdate<'_> {
    pub(super) fn has_overrides(&self) -> bool {
        self.plugin.is_some()
            || self.operation.is_some()
            || self.connection.is_some()
            || self.instance.is_some()
            || self.clear_input
            || self.replace_input
    }
}

pub(super) fn merge_plugin_target_flags(
    existing: &Value,
    update: PluginTargetUpdate<'_>,
) -> Result<Value> {
    if !update.has_overrides() {
        return existing
            .get("target")
            .cloned()
            .ok_or_else(|| anyhow!("existing {} is missing target", update.resource));
    }

    let (step, plugin_step) = target_single_plugin_step(existing).ok_or_else(|| {
        anyhow!(
            "cannot apply plugin target flags to an existing non-plugin or multi-step {}; recreate it with a full target definition",
            update.resource
        )
    })?;
    let plugin = match update.plugin {
        Some(value) => value.to_string(),
        None => plugin_step
            .get("name")
            .and_then(Value::as_str)
            .ok_or_else(|| {
                anyhow!(
                    "existing {} is missing target.steps plugin name; pass --plugin",
                    update.resource
                )
            })?
            .to_string(),
    };
    let operation = match update.operation {
        Some(value) => value.to_string(),
        None => plugin_step
            .get("operation")
            .and_then(Value::as_str)
            .ok_or_else(|| {
                anyhow!(
                    "existing {} is missing target.steps plugin operation; pass --operation",
                    update.resource
                )
            })?
            .to_string(),
    };
    let connection = resolve_optional_string(
        update.connection,
        plugin_step.get("connection").and_then(Value::as_str),
    );
    let instance = resolve_optional_string(
        update.instance,
        plugin_step.get("instance").and_then(Value::as_str),
    );
    let input = if update.clear_input {
        None
    } else if update.replace_input {
        update.input.cloned()
    } else {
        plugin_step.get("input").cloned()
    };

    Ok(build_plugin_target_from_step(
        Some(step),
        &plugin,
        &operation,
        connection.as_deref(),
        instance.as_deref(),
        input.as_ref(),
    ))
}

pub(super) fn build_plugin_target(
    plugin: &str,
    operation: &str,
    connection: Option<&str>,
    instance: Option<&str>,
    input: Option<&Value>,
) -> Value {
    build_plugin_target_from_step(None, plugin, operation, connection, instance, input)
}

fn build_plugin_target_from_step(
    existing_step: Option<&Map<String, Value>>,
    plugin: &str,
    operation: &str,
    connection: Option<&str>,
    instance: Option<&str>,
    input: Option<&Value>,
) -> Value {
    let mut step = existing_step.cloned().unwrap_or_default();
    let mut plugin_target = existing_step
        .and_then(|step| step.get("plugin"))
        .and_then(Value::as_object)
        .cloned()
        .unwrap_or_default();
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
    } else {
        plugin_target.remove("connection");
    }
    if let Some(instance) = instance {
        plugin_target.insert("instance".to_string(), Value::String(instance.to_string()));
    } else {
        plugin_target.remove("instance");
    }
    if let Some(input) = input {
        plugin_target.insert("input".to_string(), input.clone());
    } else {
        plugin_target.remove("input");
    }
    if !matches!(step.get("id"), Some(Value::String(value)) if !value.is_empty()) {
        step.insert("id".to_string(), Value::String(operation.to_string()));
    }
    step.remove("agent");
    step.insert("plugin".to_string(), Value::Object(plugin_target));

    let mut target = Map::new();
    target.insert("steps".to_string(), Value::Array(vec![Value::Object(step)]));
    Value::Object(target)
}

pub(super) fn literal_input(input: &Map<String, Value>) -> Value {
    let mut value = Map::new();
    value.insert("literal".to_string(), Value::Object(input.clone()));
    Value::Object(value)
}

pub(super) fn target_plugin<'a>(
    value: &'a Value,
    preferred_plugin: Option<&str>,
) -> Option<&'a str> {
    target_plugin_step(value, preferred_plugin)?
        .get("name")
        .and_then(Value::as_str)
}

pub(super) fn target_operation<'a>(
    value: &'a Value,
    preferred_plugin: Option<&str>,
) -> Option<&'a str> {
    target_plugin_field(value, preferred_plugin, "operation")
}

pub(super) fn target_has_plugin(value: &Value, plugin: &str) -> bool {
    value
        .get("target")
        .and_then(|target| target.get("steps"))
        .and_then(Value::as_array)
        .map(|steps| {
            steps.iter().any(|step| {
                step.get("plugin")
                    .and_then(Value::as_object)
                    .and_then(|plugin_step| plugin_step.get("name"))
                    .and_then(Value::as_str)
                    == Some(plugin)
            })
        })
        .unwrap_or(false)
}

fn target_single_plugin_step(value: &Value) -> Option<SinglePluginStep<'_>> {
    let steps = value.get("target")?.get("steps")?.as_array()?;
    if steps.len() != 1 {
        return None;
    }
    let step = steps.first()?.as_object()?;
    if step.get("agent").is_some() {
        return None;
    }
    let plugin = step.get("plugin").and_then(Value::as_object)?;
    Some((step, plugin))
}

fn target_plugin_step<'a>(
    value: &'a Value,
    preferred_plugin: Option<&str>,
) -> Option<&'a Map<String, Value>> {
    let steps = value.get("target")?.get("steps")?.as_array()?;
    let mut first_plugin_step = None;
    for step in steps {
        let Some(plugin_step) = step.get("plugin").and_then(Value::as_object) else {
            continue;
        };
        if first_plugin_step.is_none() {
            first_plugin_step = Some(plugin_step);
        }
        if preferred_plugin.is_some_and(|preferred_plugin| {
            plugin_step.get("name").and_then(Value::as_str) == Some(preferred_plugin)
        }) {
            return Some(plugin_step);
        }
    }
    first_plugin_step
}

fn target_plugin_field<'a>(
    value: &'a Value,
    preferred_plugin: Option<&str>,
    field: &str,
) -> Option<&'a str> {
    target_plugin_step(value, preferred_plugin)?
        .get(field)
        .and_then(Value::as_str)
}

fn resolve_optional_string(arg: Option<&str>, existing: Option<&str>) -> Option<String> {
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
    fn target_plugin_fields_prefer_matching_plugin_step() {
        let value = json!({
            "target": {
                "steps": [
                    {
                        "id": "first",
                        "plugin": {
                            "name": "github",
                            "operation": "createIssue"
                        }
                    },
                    {
                        "id": "second",
                        "plugin": {
                            "name": "slack",
                            "operation": "reply"
                        }
                    }
                ]
            }
        });

        assert_eq!(target_plugin(&value, None), Some("github"));
        assert_eq!(target_operation(&value, None), Some("createIssue"));
        assert_eq!(target_plugin(&value, Some("slack")), Some("slack"));
        assert_eq!(target_operation(&value, Some("slack")), Some("reply"));
        assert_eq!(target_plugin(&value, Some("jira")), Some("github"));
        assert_eq!(target_operation(&value, Some("jira")), Some("createIssue"));
    }
}
