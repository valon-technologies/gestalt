use serde_json::{Map, Value};

use super::fields::{
    extract_assistant_delta, extract_interaction_id, extract_tool_name, number_any_field,
    string_any_field, string_field, turn_event_display, value_any_field,
};
use super::types::{AgentTurnDisplayInfo, AgentTurnEventInfo};

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) enum ToolPhase {
    Started,
    Completed,
    Failed,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub(crate) struct ToolEventInfo {
    pub name: String,
    pub phase: ToolPhase,
    pub status: Option<String>,
    pub error: Option<String>,
    pub input: Option<Value>,
    pub output: Option<Value>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub(crate) enum TurnEventEffect {
    AssistantDelta(String),
    AssistantCompleted { text: Option<String> },
    TurnStarted { status: Option<String> },
    TurnCompleted { status: Option<String> },
    TurnFailed { error: Option<String> },
    TurnCanceled { reason: Option<String> },
    Tool(ToolEventInfo),
    InteractionRequested { id: String },
    InteractionResolved { id: String },
}

#[derive(Debug, Clone)]
pub(crate) enum ClassifiedTurnEvent<'a> {
    Display {
        event: &'a AgentTurnEventInfo,
        display: &'a AgentTurnDisplayInfo,
    },
    Data(TurnEventEffect),
    Private,
    Unknown(&'a AgentTurnEventInfo),
}

pub(crate) fn classify_turn_event(event: &AgentTurnEventInfo) -> ClassifiedTurnEvent<'_> {
    if let Some(display) = turn_event_display(event) {
        return ClassifiedTurnEvent::Display { event, display };
    }
    if let Some(effect) = classify_data_event(event) {
        return ClassifiedTurnEvent::Data(effect);
    }
    if event.visibility == "private" {
        return ClassifiedTurnEvent::Private;
    }
    ClassifiedTurnEvent::Unknown(event)
}

pub(crate) fn classify_data_event(event: &AgentTurnEventInfo) -> Option<TurnEventEffect> {
    match event.event_type.as_str() {
        "agent.message.delta" | "assistant.delta" => {
            extract_assistant_delta(&event.data).map(TurnEventEffect::AssistantDelta)
        }
        "assistant.completed" => Some(TurnEventEffect::AssistantCompleted {
            text: string_field(&event.data, "text"),
        }),
        "turn.started" => Some(TurnEventEffect::TurnStarted {
            status: string_any_field(&event.data, &["status", "state"]),
        }),
        "turn.completed" => Some(TurnEventEffect::TurnCompleted {
            status: string_any_field(&event.data, &["status", "state"]),
        }),
        "turn.failed" => Some(TurnEventEffect::TurnFailed {
            error: string_field(&event.data, "error"),
        }),
        "turn.canceled" => Some(TurnEventEffect::TurnCanceled {
            reason: string_field(&event.data, "reason"),
        }),
        "tool.started" => Some(TurnEventEffect::Tool(tool_event_info(
            &event.data,
            ToolPhase::Started,
        ))),
        "tool.completed" => Some(TurnEventEffect::Tool(tool_event_info(
            &event.data,
            ToolPhase::Completed,
        ))),
        "tool.failed" => Some(TurnEventEffect::Tool(tool_event_info(
            &event.data,
            ToolPhase::Failed,
        ))),
        "interaction.requested" => Some(TurnEventEffect::InteractionRequested {
            id: extract_interaction_id(&event.data),
        }),
        "interaction.resolved" => Some(TurnEventEffect::InteractionResolved {
            id: extract_interaction_id(&event.data),
        }),
        _ => None,
    }
}

fn tool_event_info(data: &Map<String, Value>, phase: ToolPhase) -> ToolEventInfo {
    let error = string_field(data, "error").or_else(|| {
        if matches!(phase, ToolPhase::Failed) {
            string_any_field(data, &["error", "message"])
        } else {
            None
        }
    });
    ToolEventInfo {
        name: extract_tool_name(data),
        phase,
        status: tool_status(data),
        error,
        input: value_any_field(data, &["arguments", "input", "request"]).cloned(),
        output: value_any_field(data, &["output", "result", "body"]).cloned(),
    }
}

fn tool_status(data: &Map<String, Value>) -> Option<String> {
    string_any_field(data, &["status", "state"]).or_else(|| {
        number_any_field(data, &["status", "statusCode"]).map(|status| status.to_string())
    })
}

pub(crate) fn effect_data_summary(effect: &TurnEventEffect) -> Option<String> {
    match effect {
        TurnEventEffect::AssistantDelta(text) => Some(format!("assistant delta: {text}")),
        TurnEventEffect::AssistantCompleted { text: Some(text) } => {
            Some(format!("assistant completed: {text}"))
        }
        TurnEventEffect::AssistantCompleted { text: None } => None,
        TurnEventEffect::TurnStarted {
            status: Some(status),
        } => Some(format!("turn started: {status}")),
        TurnEventEffect::TurnStarted { status: None } => Some("turn started".to_string()),
        TurnEventEffect::TurnCompleted {
            status: Some(status),
        } => Some(format!("turn completed: {status}")),
        TurnEventEffect::TurnCompleted { status: None } => Some("turn completed".to_string()),
        TurnEventEffect::TurnFailed { error: Some(error) } => Some(format!("turn failed: {error}")),
        TurnEventEffect::TurnFailed { error: None } => Some("turn failed".to_string()),
        TurnEventEffect::TurnCanceled {
            reason: Some(reason),
        } => Some(format!("turn canceled: {reason}")),
        TurnEventEffect::TurnCanceled { reason: None } => Some("turn canceled".to_string()),
        TurnEventEffect::Tool(info) => Some(tool_info_summary(info)),
        TurnEventEffect::InteractionRequested { id } => {
            Some(format!("interaction requested ({id})"))
        }
        TurnEventEffect::InteractionResolved { id } => Some(format!("interaction resolved ({id})")),
    }
}

pub(crate) fn tool_info_summary(info: &ToolEventInfo) -> String {
    let phase = match info.phase {
        ToolPhase::Started => "started",
        ToolPhase::Completed => "completed",
        ToolPhase::Failed => "failed",
    };
    let mut summary = format!("{} {phase}", info.name);
    if let Some(status) = info.status.as_deref() {
        summary.push_str(&format!(" ({status})"));
    }
    if let Some(error) = info.error.as_deref() {
        summary.push_str(&format!(": {error}"));
    } else if let Some(output) = info.output.as_ref() {
        if let Ok(encoded) = super::fields::compact_json(output) {
            summary.push(' ');
            summary.push_str(&encoded);
        }
    } else if let Some(input) = info.input.as_ref()
        && let Ok(encoded) = super::fields::compact_json(input)
    {
        summary.push(' ');
        summary.push_str(&encoded);
    }
    summary
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn data_event(event_type: &str, data: Value) -> AgentTurnEventInfo {
        AgentTurnEventInfo {
            id: String::new(),
            turn_id: String::new(),
            seq: 0,
            event_type: event_type.to_string(),
            source: String::new(),
            visibility: "public".to_string(),
            data: data.as_object().cloned().unwrap_or_default(),
            display: None,
        }
    }

    #[test]
    fn classify_assistant_delta() {
        let event = data_event("assistant.delta", json!({"text": "hello"}));
        match classify_turn_event(&event) {
            ClassifiedTurnEvent::Data(TurnEventEffect::AssistantDelta(text)) => {
                assert_eq!(text, "hello");
            }
            other => panic!("unexpected classification: {other:?}"),
        }
    }

    #[test]
    fn classify_tool_failed() {
        let event = data_event(
            "tool.failed",
            json!({"tool_name": "grep", "error": "timeout"}),
        );
        match classify_turn_event(&event) {
            ClassifiedTurnEvent::Data(TurnEventEffect::Tool(info)) => {
                assert_eq!(info.name, "grep");
                assert_eq!(info.phase, ToolPhase::Failed);
                assert_eq!(info.error.as_deref(), Some("timeout"));
            }
            other => panic!("unexpected classification: {other:?}"),
        }
    }

    #[test]
    fn classify_private_event() {
        let mut event = data_event("custom.internal", json!({}));
        event.visibility = "private".to_string();
        assert!(matches!(
            classify_turn_event(&event),
            ClassifiedTurnEvent::Private
        ));
    }

    #[test]
    fn tool_info_summary_includes_input_when_no_output_or_error() {
        let info = ToolEventInfo {
            name: "grep".to_string(),
            phase: ToolPhase::Completed,
            status: None,
            error: None,
            input: Some(json!({"pattern": "foo"})),
            output: None,
        };
        let summary = tool_info_summary(&info);
        assert!(summary.contains("grep completed"));
        assert!(summary.contains("pattern"));
    }
}
