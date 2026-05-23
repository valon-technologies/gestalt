use serde_json::Value;

use crate::commands::agents::fields::{
    compact_json, display_action, display_label, display_ref, display_status, display_text,
    display_tool_error, display_tool_input, display_tool_label, display_tool_output,
    display_value_text, extract_assistant_delta, extract_interaction_id, extract_tool_name,
    number_any_field, string_any_field, string_field, turn_event_display, value_any_field,
};
use crate::commands::agents::types::{AgentTurnDisplayInfo, AgentTurnEventInfo};

pub(crate) fn fallback_turn_event_data_summary(value: &Value) -> String {
    if let Ok(event) = serde_json::from_value::<AgentTurnEventInfo>(value.clone()) {
        if let Some(summary) = turn_event_data_summary(&event) {
            return summary;
        }
        if event.visibility == "private" {
            return String::new();
        }
    } else if value["visibility"].as_str() == Some("private") {
        return String::new();
    }
    serde_json::to_string(&value["data"]).unwrap_or_else(|_| "-".to_string())
}

pub(crate) fn turn_event_data_summary(event: &AgentTurnEventInfo) -> Option<String> {
    match event.event_type.as_str() {
        "agent.message.delta" | "assistant.delta" => {
            extract_assistant_delta(&event.data).map(|text| format!("assistant delta: {text}"))
        }
        "assistant.completed" => {
            string_field(&event.data, "text").map(|text| format!("assistant completed: {text}"))
        }
        "turn.started" => string_any_field(&event.data, &["status", "state"])
            .map(|status| format!("turn started: {status}"))
            .or_else(|| Some("turn started".to_string())),
        "turn.completed" => string_any_field(&event.data, &["status", "state"])
            .map(|status| format!("turn completed: {status}"))
            .or_else(|| Some("turn completed".to_string())),
        "turn.failed" => string_field(&event.data, "error")
            .map(|error| format!("turn failed: {error}"))
            .or_else(|| Some("turn failed".to_string())),
        "turn.canceled" => string_field(&event.data, "reason")
            .map(|reason| format!("turn canceled: {reason}"))
            .or_else(|| Some("turn canceled".to_string())),
        "tool.started" => Some(tool_data_summary(event, "started")?),
        "tool.completed" => Some(tool_data_summary(event, "completed")?),
        "tool.failed" => Some(tool_data_summary(event, "failed")?),
        "interaction.requested" => {
            let id = extract_interaction_id(&event.data);
            Some(format!("interaction requested ({id})"))
        }
        "interaction.resolved" => {
            let id = extract_interaction_id(&event.data);
            Some(format!("interaction resolved ({id})"))
        }
        _ => None,
    }
}

pub(crate) fn tool_data_summary(event: &AgentTurnEventInfo, phase: &str) -> Option<String> {
    let tool = extract_tool_name(&event.data);
    let mut summary = format!("{tool} {phase}");
    if let Some(status) = string_any_field(&event.data, &["status", "state"]).or_else(|| {
        number_any_field(&event.data, &["status", "statusCode"]).map(|status| status.to_string())
    }) {
        summary.push_str(&format!(" ({status})"));
    }
    if let Some(error) = string_field(&event.data, "error") {
        summary.push_str(&format!(": {error}"));
    } else if let Some(output) = value_any_field(&event.data, &["output", "result", "body"]) {
        summary.push(' ');
        summary.push_str(&compact_json(output).ok()?);
    } else if let Some(input) = value_any_field(&event.data, &["arguments", "input", "request"]) {
        summary.push(' ');
        summary.push_str(&compact_json(input).ok()?);
    }
    Some(summary)
}

pub(crate) fn turn_event_display_summary(value: &Value) -> Option<String> {
    let event: AgentTurnEventInfo = serde_json::from_value(value.clone()).ok()?;
    let display = turn_event_display(&event)?;
    match display.kind.trim() {
        "text" => display_text(display).map(|text| match display.phase.trim() {
            "delta" => format!("assistant delta: {text}"),
            "completed" => format!("assistant completed: {text}"),
            phase if !phase.is_empty() => format!("assistant {phase}: {text}"),
            _ => format!("assistant: {text}"),
        }),
        "reasoning" => display_text(display).map(|text| format!("reasoning: {text}")),
        "tool" => Some(tool_display_summary(&event, display)?),
        "interaction" => {
            let label = display_label(display).unwrap_or("interaction");
            let reference = display_ref(display).unwrap_or(label);
            match display.phase.trim() {
                "requested" => Some(format!("{label} requested ({reference})")),
                "resolved" => Some(format!("{label} resolved ({reference})")),
                phase if !phase.is_empty() => Some(format!("{label} {phase} ({reference})")),
                _ => Some(label.to_string()),
            }
        }
        "status" => {
            let label = display_label(display).unwrap_or("turn");
            match (display.phase.trim(), display_text(display)) {
                ("started", Some(text)) => Some(format!("{label} started: {text}")),
                ("started", None) => Some(format!("{label} started")),
                ("completed", Some(text)) => Some(format!("{label} completed: {text}")),
                ("completed", None) => Some(format!("{label} completed")),
                ("canceled", Some(text)) => Some(format!("{label} canceled: {text}")),
                ("canceled", None) => Some(format!("{label} canceled")),
                ("progress", Some(text)) => Some(format!("{label}: {text}")),
                (phase, Some(text)) if !phase.is_empty() => {
                    Some(format!("{label} {phase}: {text}"))
                }
                _ => None,
            }
        }
        "error" => {
            let text = display_text(display).map(ToString::to_string).or_else(|| {
                display
                    .error
                    .as_ref()
                    .and_then(|value| display_value_text(value).ok())
            })?;
            let label = display_label(display).unwrap_or("error");
            match display.phase.trim() {
                "failed" if label == "turn" => Some(format!("turn failed: {text}")),
                "failed" => Some(format!("{label} failed: {text}")),
                _ => Some(format!("{label}: {text}")),
            }
        }
        _ => None,
    }
}

pub(crate) fn tool_display_summary(
    event: &AgentTurnEventInfo,
    display: &AgentTurnDisplayInfo,
) -> Option<String> {
    let tool = display_tool_label(event, display);
    let mut summary = match display.phase.trim() {
        "started" => display_action(display)
            .map(|action| format!("{action} {tool}"))
            .unwrap_or_else(|| format!("{tool} started")),
        "completed" => display_action(display)
            .map(|action| format!("{action} {tool}"))
            .unwrap_or_else(|| format!("{tool} completed")),
        "failed" => display_action(display)
            .map(|action| format!("{action} {tool}"))
            .unwrap_or_else(|| format!("{tool} failed")),
        "progress" => match (display_action(display), display_text(display)) {
            (Some(action), Some(text)) => format!("{action} {tool}: {text}"),
            (Some(action), None) => format!("{action} {tool}"),
            (None, Some(text)) => format!("{tool} {text}"),
            (None, None) => format!("{tool} progress"),
        },
        phase if !phase.is_empty() => format!("{tool} {phase}"),
        _ => tool,
    };
    if matches!(display.phase.trim(), "completed" | "failed")
        && let Some(status) = display_status(event, display)
    {
        summary.push_str(&format!(" ({status})"));
    }
    if let Some(error) = display_tool_error(event, display) {
        summary.push_str(&format!(": {}", display_value_text(error).ok()?));
    } else if let Some(output) = display_tool_output(event, display) {
        summary.push(' ');
        summary.push_str(&compact_json(output).ok()?);
    } else if let Some(input) = display_tool_input(event, display) {
        summary.push(' ');
        summary.push_str(&compact_json(input).ok()?);
    }
    Some(summary)
}
