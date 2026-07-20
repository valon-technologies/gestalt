use serde::{Deserialize, Deserializer, Serialize};
use serde_json::{Map, Value};

use crate::cli::AgentToolArg;

/// Deserializes a field that may be a string (v1 REST shape) or a number
/// (SDK proto enum shape). Numbers are mapped to the CLI-expected string
/// names via `map_fn`; strings pass through unchanged.
fn enum_string<'de, D>(
    deserializer: D,
    map_fn: fn(i64) -> &'static str,
) -> std::result::Result<String, D::Error>
where
    D: Deserializer<'de>,
{
    let value = Value::deserialize(deserializer)?;
    match value {
        Value::String(s) => Ok(s),
        Value::Number(n) => Ok(n
            .as_i64()
            .map(map_fn)
            .filter(|name| !name.is_empty())
            .unwrap_or_default()
            .to_string()),
        Value::Null => Ok(String::new()),
        _ => Ok(value.to_string()),
    }
}

fn deserialize_session_state<'de, D>(deserializer: D) -> std::result::Result<String, D::Error>
where
    D: Deserializer<'de>,
{
    enum_string(deserializer, |v| match v {
        1 => "active",
        2 => "closed",
        _ => "",
    })
}

fn deserialize_execution_status<'de, D>(deserializer: D) -> std::result::Result<String, D::Error>
where
    D: Deserializer<'de>,
{
    enum_string(deserializer, |v| match v {
        1 => "pending",
        2 => "running",
        3 => "succeeded",
        4 => "failed",
        5 => "canceled",
        6 => "waiting_for_input",
        _ => "",
    })
}

fn deserialize_interaction_state<'de, D>(deserializer: D) -> std::result::Result<String, D::Error>
where
    D: Deserializer<'de>,
{
    enum_string(deserializer, |v| match v {
        1 => "pending",
        2 => "resolved",
        3 => "canceled",
        _ => "",
    })
}

fn deserialize_interaction_type<'de, D>(deserializer: D) -> std::result::Result<String, D::Error>
where
    D: Deserializer<'de>,
{
    enum_string(deserializer, |v| match v {
        1 => "approval",
        2 => "clarification",
        3 => "input",
        _ => "",
    })
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct AgentHarnessResolveRequest<'a> {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub(crate) provider: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub(crate) harness: Option<&'a str>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct AgentHarnessPlan {
    pub(crate) provider: String,
    #[serde(default)]
    pub(crate) harness: String,
    pub(crate) command: String,
    #[serde(default)]
    pub(crate) args: Vec<String>,
    #[serde(default)]
    pub(crate) env: Map<String, Value>,
    #[serde(default)]
    pub(crate) working_directory: String,
    #[serde(default)]
    pub(crate) required_commands: Vec<String>,
    pub(crate) install: Option<AgentHarnessInstallPlan>,
}

#[derive(Deserialize, Clone)]
#[serde(rename_all = "camelCase")]
pub(crate) struct AgentHarnessInstallPlan {
    #[serde(default)]
    pub(crate) instructions: String,
    #[serde(default)]
    pub(crate) commands: Vec<AgentHarnessInstallCommand>,
}

#[derive(Deserialize, Clone)]
#[serde(rename_all = "camelCase")]
pub(crate) struct AgentHarnessInstallCommand {
    #[serde(default)]
    pub(crate) description: String,
    #[serde(default)]
    pub(crate) command: String,
    #[serde(default)]
    pub(crate) args: Vec<String>,
    #[serde(default)]
    pub(crate) shell: String,
    #[serde(default)]
    pub(crate) env: Map<String, Value>,
}

pub(crate) fn deserialize_turn_display<'de, D>(
    deserializer: D,
) -> std::result::Result<Option<AgentTurnDisplayInfo>, D::Error>
where
    D: Deserializer<'de>,
{
    let value = Option::<Value>::deserialize(deserializer)?;
    Ok(value.and_then(AgentTurnDisplayInfo::from_value))
}

pub(crate) fn take_string_field(data: &mut Map<String, Value>, key: &str) -> String {
    match data.remove(key) {
        Some(Value::String(value)) => value,
        _ => String::new(),
    }
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct AgentSessionInfo {
    pub(crate) id: String,
    #[serde(alias = "providerName")]
    pub(crate) provider: String,
    #[serde(default)]
    pub(crate) model: String,
    #[serde(default, deserialize_with = "deserialize_session_state")]
    pub(crate) state: String,
    #[serde(default)]
    pub(crate) last_turn_at: String,
    #[serde(default)]
    pub(crate) created_at: String,
    #[serde(default)]
    pub(crate) updated_at: String,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct AgentProviderListInfo {
    #[serde(default)]
    pub(crate) providers: Vec<AgentProviderInfo>,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct AgentProviderInfo {
    pub(crate) name: String,
    #[serde(default)]
    pub(crate) default: bool,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct AgentTurnInfo {
    pub(crate) id: String,
    #[serde(default)]
    pub(crate) messages: Vec<AgentMessageInfo>,
    #[serde(default, deserialize_with = "deserialize_execution_status")]
    pub(crate) status: String,
    #[serde(default)]
    pub(crate) output: Option<AgentTurnOutputInfo>,
    #[serde(default)]
    pub(crate) status_message: String,
}

impl AgentTurnInfo {
    pub(crate) fn output_text(&self) -> &str {
        if let Some(text) = self
            .output
            .as_ref()
            .and_then(|output| output.text.as_ref())
            .map(|output| output.text.as_str())
        {
            return text;
        }
        self.output
            .as_ref()
            .and_then(|output| output.structured.as_ref())
            .map(|output| output.text.as_str())
            .unwrap_or("")
    }

    pub(crate) fn structured_output(&self) -> Option<&Value> {
        self.output
            .as_ref()
            .and_then(|output| output.structured.as_ref())
            .and_then(|output| output.value.as_ref())
    }
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct AgentTurnOutputInfo {
    #[serde(default)]
    pub(crate) text: Option<AgentTurnTextOutputInfo>,
    #[serde(default)]
    pub(crate) structured: Option<AgentTurnStructuredOutputInfo>,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct AgentTurnTextOutputInfo {
    #[serde(default)]
    pub(crate) text: String,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct AgentTurnStructuredOutputInfo {
    #[serde(default)]
    pub(crate) text: String,
    #[serde(default)]
    pub(crate) value: Option<Value>,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct AgentMessageInfo {
    #[serde(default)]
    pub(crate) role: String,
    #[serde(default)]
    pub(crate) text: String,
    #[serde(default)]
    pub(crate) parts: Vec<AgentMessagePartInfo>,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct AgentMessagePartInfo {
    #[serde(default)]
    pub(crate) text: String,
    #[serde(default)]
    pub(crate) json: Option<Value>,
    #[serde(default)]
    pub(crate) tool_call: Option<Value>,
    #[serde(default)]
    pub(crate) tool_result: Option<Value>,
    #[serde(default)]
    pub(crate) image_ref: Option<Value>,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct AgentTurnEventInfo {
    #[serde(default)]
    pub(crate) id: String,
    #[serde(default)]
    pub(crate) turn_id: String,
    #[serde(default)]
    pub(crate) seq: i64,
    #[serde(rename = "type")]
    pub(crate) event_type: String,
    #[serde(default)]
    pub(crate) source: String,
    #[serde(default)]
    pub(crate) visibility: String,
    #[serde(default)]
    pub(crate) data: Map<String, Value>,
    #[serde(default, deserialize_with = "deserialize_turn_display")]
    pub(crate) display: Option<AgentTurnDisplayInfo>,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct AgentTurnDisplayInfo {
    #[serde(default)]
    pub(crate) kind: String,
    #[serde(default)]
    pub(crate) phase: String,
    #[serde(default)]
    pub(crate) text: String,
    #[serde(default)]
    pub(crate) label: String,
    #[serde(default, rename = "ref")]
    pub(crate) display_ref: String,
    #[allow(dead_code)]
    #[serde(default)]
    pub(crate) parent_ref: String,
    #[serde(default)]
    pub(crate) input: Option<Value>,
    #[serde(default)]
    pub(crate) output: Option<Value>,
    #[serde(default)]
    pub(crate) error: Option<Value>,
    #[serde(default)]
    pub(crate) action: String,
    #[serde(default)]
    pub(crate) format: String,
    #[serde(default)]
    pub(crate) language: String,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct AgentInteractionInfo {
    pub(crate) id: String,
    #[serde(rename = "type", deserialize_with = "deserialize_interaction_type")]
    pub(crate) interaction_type: String,
    #[serde(default, deserialize_with = "deserialize_interaction_state")]
    pub(crate) state: String,
    #[serde(default)]
    pub(crate) title: String,
    #[serde(default)]
    pub(crate) prompt: String,
    #[serde(default)]
    pub(crate) request: Map<String, Value>,
}

pub(crate) struct AgentShell {
    pub(crate) session: AgentSessionInfo,
    pub(crate) model_override: Option<String>,
    pub(crate) system_messages: Vec<String>,
    pub(crate) tools: Vec<AgentToolArg>,
    pub(crate) timeout_seconds: Option<i32>,
    pub(crate) applied_system_messages: bool,
}

impl AgentTurnDisplayInfo {
    pub(crate) fn from_value(value: Value) -> Option<Self> {
        let Value::Object(mut data) = value else {
            return None;
        };
        Some(Self {
            kind: take_string_field(&mut data, "kind"),
            phase: take_string_field(&mut data, "phase"),
            text: take_string_field(&mut data, "text"),
            label: take_string_field(&mut data, "label"),
            display_ref: take_string_field(&mut data, "ref"),
            parent_ref: take_string_field(&mut data, "parentRef"),
            input: data.remove("input"),
            output: data.remove("output"),
            error: data.remove("error"),
            action: take_string_field(&mut data, "action"),
            format: take_string_field(&mut data, "format"),
            language: take_string_field(&mut data, "language"),
        })
    }
}
