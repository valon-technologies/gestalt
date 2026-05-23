use serde_json::Value;

use crate::commands::agents::events::{
    ClassifiedTurnEvent, classify_data_event, classify_turn_event, effect_data_summary,
};
use crate::commands::agents::fields::{
    display_action, display_label, display_ref, display_status, display_text, display_tool_error,
    display_tool_input, display_tool_label, display_tool_output, display_value_text,
    turn_event_display,
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
    match classify_turn_event(event) {
        ClassifiedTurnEvent::Display { event, display } => {
            turn_event_display_summary(event, display).or_else(|| {
                classify_data_event(event)
                    .and_then(|effect| effect_data_summary(&effect))
                    .or_else(|| Some(generic_event_data_summary(event)))
            })
        }
        ClassifiedTurnEvent::Data(effect) => effect_data_summary(&effect),
        ClassifiedTurnEvent::Private => Some(String::new()),
        ClassifiedTurnEvent::Unknown(event) => Some(generic_event_data_summary(event)),
    }
}

fn generic_event_data_summary(event: &AgentTurnEventInfo) -> String {
    let mut fields = Vec::new();
    if !event.id.is_empty() {
        fields.push(format!("id={}", event.id));
    }
    if !event.source.is_empty() {
        fields.push(format!("source={}", event.source));
    }
    if !event.turn_id.is_empty() {
        fields.push(format!("turn={}", event.turn_id));
    }
    let suffix = if fields.is_empty() {
        String::new()
    } else {
        format!(" ({})", fields.join(" "))
    };
    if event.data.is_empty() {
        format!("{}{}", event.event_type, suffix)
    } else {
        super::super::fields::compact_json(&Value::Object(event.data.clone()))
            .map(|data| format!("{}{} {data}", event.event_type, suffix))
            .unwrap_or_else(|_| format!("{}{}", event.event_type, suffix))
    }
}

pub(crate) fn turn_event_display_summary(
    event: &AgentTurnEventInfo,
    display: &AgentTurnDisplayInfo,
) -> Option<String> {
    let _ = event;
    match display.kind.trim() {
        "text" => display_text(display).map(|text| match display.phase.trim() {
            "delta" => format!("assistant delta: {text}"),
            "completed" => format!("assistant completed: {text}"),
            phase if !phase.is_empty() => format!("assistant {phase}: {text}"),
            _ => format!("assistant: {text}"),
        }),
        "reasoning" => display_text(display).map(|text| format!("reasoning: {text}")),
        "tool" => Some(tool_display_summary(display)?),
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

pub(crate) fn turn_event_display_summary_from_value(value: &Value) -> Option<String> {
    let event: AgentTurnEventInfo = serde_json::from_value(value.clone()).ok()?;
    let display = turn_event_display(&event)?;
    turn_event_display_summary(&event, display)
}

pub(crate) fn tool_display_summary(display: &AgentTurnDisplayInfo) -> Option<String> {
    let tool = display_tool_label(display);
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
        && let Some(status) = display_status(display)
    {
        summary.push_str(&format!(" ({status})"));
    }
    if let Some(error) = display_tool_error(display) {
        summary.push_str(&format!(": {}", display_value_text(error).ok()?));
    } else if let Some(output) = display_tool_output(display) {
        summary.push(' ');
        summary.push_str(&super::super::fields::compact_json(output).ok()?);
    } else if let Some(input) = display_tool_input(display) {
        summary.push(' ');
        summary.push_str(&super::super::fields::compact_json(input).ok()?);
    }
    Some(summary)
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn turn_event_data_summary_falls_back_to_data_when_display_unrecognized() {
        let event = AgentTurnEventInfo {
            id: String::new(),
            turn_id: String::new(),
            seq: 0,
            event_type: "tool.completed".to_string(),
            source: String::new(),
            visibility: "public".to_string(),
            data: json!({"tool_name": "grep", "arguments": {"pattern": "foo"}})
                .as_object()
                .cloned()
                .unwrap(),
            display: Some(AgentTurnDisplayInfo {
                kind: "unknown-kind".to_string(),
                phase: "completed".to_string(),
                ..AgentTurnDisplayInfo::from_value(json!({})).unwrap()
            }),
        };

        let summary = turn_event_data_summary(&event).expect("summary");
        assert!(summary.contains("grep"));
        assert!(summary.contains("completed"));
    }

    #[test]
    fn turn_event_data_summary_falls_back_to_generic_for_unknown_display_and_type() {
        let event = AgentTurnEventInfo {
            id: String::new(),
            turn_id: String::new(),
            seq: 0,
            event_type: "custom.new".to_string(),
            source: String::new(),
            visibility: "public".to_string(),
            data: json!({"payload": "value"}).as_object().cloned().unwrap(),
            display: Some(AgentTurnDisplayInfo {
                kind: "widget".to_string(),
                phase: "ready".to_string(),
                ..AgentTurnDisplayInfo::from_value(json!({})).unwrap()
            }),
        };

        let summary = turn_event_data_summary(&event).expect("summary");
        assert!(summary.contains("custom.new"));
        assert!(summary.contains("payload"));
    }
}
