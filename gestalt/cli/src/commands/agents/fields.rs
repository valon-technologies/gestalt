use anyhow::{Context, Result};
use serde::de::DeserializeOwned;
use serde_json::{Map, Value};

use super::display_markdown;
use super::types::{
    AgentMessageInfo, AgentMessagePartInfo, AgentTurnDisplayInfo, AgentTurnEventInfo,
};
use super::wire::is_private_event_visibility;

pub(crate) fn decode_json<T>(value: Value) -> Result<T>
where
    T: DeserializeOwned,
{
    serde_json::from_value(value).context("failed to decode agent response")
}

pub(crate) fn string_field(data: &Map<String, Value>, key: &str) -> Option<String> {
    data.get(key)
        .and_then(Value::as_str)
        .map(ToString::to_string)
}

pub(crate) fn string_any_field(data: &Map<String, Value>, keys: &[&str]) -> Option<String> {
    keys.iter().find_map(|key| string_field(data, key))
}

pub(crate) fn value_any_field<'a>(
    data: &'a Map<String, Value>,
    keys: &[&str],
) -> Option<&'a Value> {
    keys.iter().find_map(|key| data.get(*key))
}

pub(crate) fn number_any_field(data: &Map<String, Value>, keys: &[&str]) -> Option<i64> {
    keys.iter().find_map(|key| data.get(*key)?.as_i64())
}

pub(crate) fn turn_event_display(event: &AgentTurnEventInfo) -> Option<&AgentTurnDisplayInfo> {
    let display = event.display.as_ref()?;
    if display.kind.trim().is_empty() {
        return None;
    }
    if is_private_event_visibility(&event.visibility) && !known_turn_event_type(&event.event_type) {
        return None;
    }
    Some(display)
}

pub(crate) fn known_turn_event_type(event_type: &str) -> bool {
    matches!(
        event_type,
        "agent.message.delta"
            | "assistant.delta"
            | "assistant.completed"
            | "turn.started"
            | "turn.completed"
            | "turn.failed"
            | "turn.canceled"
            | "tool.started"
            | "tool.completed"
            | "tool.failed"
            | "interaction.requested"
            | "interaction.resolved"
    )
}

pub(crate) fn message_label(message: &AgentMessageInfo) -> Option<String> {
    let role = non_empty_str(&message.role)?;
    Some(match role {
        "user" => "you>".to_string(),
        "system" => "system>".to_string(),
        "assistant" => "assistant>".to_string(),
        "tool" => "tool>".to_string(),
        other => format!("{other}>"),
    })
}

pub(crate) fn message_text(message: &AgentMessageInfo) -> Option<String> {
    if !message.text.is_empty() {
        return Some(message.text.clone());
    }
    let text = message
        .parts
        .iter()
        .filter_map(message_part_text)
        .collect::<Vec<_>>()
        .join("");
    if text.is_empty() { None } else { Some(text) }
}

pub(crate) fn message_part_text(part: &AgentMessagePartInfo) -> Option<String> {
    if !part.text.trim().is_empty() {
        return Some(part.text.clone());
    }
    if let Some(json) = part.json.as_ref() {
        return compact_json(json).ok();
    }
    if let Some(tool_call) = part.tool_call.as_ref() {
        return compact_json(tool_call)
            .ok()
            .map(|value| format!("tool call {value}"));
    }
    if let Some(tool_result) = part.tool_result.as_ref() {
        return compact_json(tool_result)
            .ok()
            .map(|value| format!("tool result {value}"));
    }
    if let Some(image_ref) = part.image_ref.as_ref() {
        return compact_json(image_ref)
            .ok()
            .map(|value| format!("image {value}"));
    }
    None
}

pub(crate) fn display_text(display: &AgentTurnDisplayInfo) -> Option<&str> {
    if display.text.is_empty() {
        None
    } else {
        Some(&display.text)
    }
}

pub(crate) fn rendered_display_text(display: &AgentTurnDisplayInfo) -> Option<String> {
    let text = display_text(display)?;
    Some(display_markdown::plain_text_for_format(
        text,
        display_format(display),
        display_language(display),
    ))
}

pub(crate) fn display_label(display: &AgentTurnDisplayInfo) -> Option<&str> {
    non_empty_str(&display.label)
}

pub(crate) fn display_ref(display: &AgentTurnDisplayInfo) -> Option<&str> {
    non_empty_str(&display.display_ref)
}

pub(crate) fn display_action(display: &AgentTurnDisplayInfo) -> Option<&str> {
    non_empty_str(&display.action)
}

pub(crate) fn display_format(display: &AgentTurnDisplayInfo) -> Option<&str> {
    non_empty_str(&display.format)
}

pub(crate) fn display_language(display: &AgentTurnDisplayInfo) -> Option<&str> {
    non_empty_str(&display.language)
}

pub(crate) fn display_tool_label(display: &AgentTurnDisplayInfo) -> String {
    display_label(display)
        .or_else(|| display_ref(display))
        .map(ToString::to_string)
        .unwrap_or_else(|| "tool".to_string())
}

pub(crate) fn display_tool_ref(display: &AgentTurnDisplayInfo) -> Option<String> {
    display_ref(display).map(ToString::to_string)
}

pub(crate) fn display_tool_input(display: &AgentTurnDisplayInfo) -> Option<&Value> {
    display.input.as_ref()
}

pub(crate) fn display_tool_output(display: &AgentTurnDisplayInfo) -> Option<&Value> {
    display.output.as_ref()
}

pub(crate) fn display_tool_error(display: &AgentTurnDisplayInfo) -> Option<&Value> {
    display.error.as_ref()
}

pub(crate) fn display_status(display: &AgentTurnDisplayInfo) -> Option<String> {
    display_text(display).map(ToString::to_string)
}

pub(crate) fn display_value_text(value: &Value) -> Result<String> {
    match value {
        Value::String(text) => Ok(text.clone()),
        _ => compact_json(value),
    }
}

pub(crate) fn non_empty_str(value: &str) -> Option<&str> {
    let trimmed = value.trim();
    if trimmed.is_empty() {
        None
    } else {
        Some(trimmed)
    }
}

pub(crate) fn compact_json(value: &Value) -> Result<String> {
    serde_json::to_string(value).context("failed to encode compact JSON")
}

pub(crate) fn pretty_json(value: &Value) -> Result<String> {
    serde_json::to_string_pretty(value).context("failed to encode pretty JSON")
}

pub(crate) fn extract_assistant_delta(data: &Map<String, Value>) -> Option<String> {
    string_any_field(data, &["text", "delta", "content"])
}

pub(crate) fn extract_tool_name(data: &Map<String, Value>) -> String {
    string_any_field(
        data,
        &[
            "tool_name",
            "toolName",
            "name",
            "operation",
            "tool_id",
            "toolId",
        ],
    )
    .unwrap_or_else(|| "tool".to_string())
}

pub(crate) fn extract_interaction_id(data: &Map<String, Value>) -> String {
    string_any_field(data, &["interaction_id", "interactionId"])
        .unwrap_or_else(|| "interaction".to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn fixture(name: &str) -> Map<String, Value> {
        let raw = match name {
            "assistant_delta" => include_str!("fields/fixtures/assistant_delta.json"),
            "tool_started" => include_str!("fields/fixtures/tool_started.json"),
            "interaction_requested" => include_str!("fields/fixtures/interaction_requested.json"),
            other => panic!("unknown fixture: {other}"),
        };
        serde_json::from_str(raw).expect("fixture json")
    }

    #[test]
    fn extract_assistant_delta_reads_text_field() {
        let data = fixture("assistant_delta");
        assert_eq!(
            extract_assistant_delta(&data).as_deref(),
            Some("hello world")
        );
    }

    #[test]
    fn extract_tool_name_prefers_tool_name() {
        let data = fixture("tool_started");
        assert_eq!(extract_tool_name(&data), "grep");
    }

    #[test]
    fn extract_interaction_id_reads_camel_case() {
        let data = fixture("interaction_requested");
        assert_eq!(extract_interaction_id(&data), "abc123");
    }
}
