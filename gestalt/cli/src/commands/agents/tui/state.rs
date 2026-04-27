use serde_json::Value;

use super::super::{
    AgentInteractionInfo, AgentSessionInfo, AgentTurnEventInfo, AgentTurnInfo, compact_json,
    pretty_json, string_any_field, string_field, value_any_field,
};

const MAX_TRANSCRIPT_ITEMS: usize = 500;

pub(super) struct AgentUiState {
    pub(super) session_id: String,
    pub(super) provider: String,
    pub(super) model: String,
    pub(super) status: String,
    pub(super) busy: bool,
    pub(super) current_turn_id: Option<String>,
    pub(super) pending_interaction: Option<AgentInteractionInfo>,
    transcript: Vec<TranscriptItem>,
    assistant_buffer: String,
    saw_assistant_output: bool,
    saw_structured_output: bool,
    scroll_offset: usize,
}

impl AgentUiState {
    pub(super) fn new(session: &AgentSessionInfo) -> Self {
        let model = if session.model.is_empty() {
            "<unspecified>".to_string()
        } else {
            session.model.clone()
        };
        Self {
            session_id: session.id.clone(),
            provider: session.provider.clone(),
            model,
            status: "ready".to_string(),
            busy: false,
            current_turn_id: None,
            pending_interaction: None,
            transcript: Vec::new(),
            assistant_buffer: String::new(),
            saw_assistant_output: false,
            saw_structured_output: false,
            scroll_offset: 0,
        }
    }

    pub(super) fn start_turn(&mut self) {
        self.finish_assistant_stream();
        self.busy = true;
        self.current_turn_id = None;
        self.pending_interaction = None;
        self.status = "starting turn".to_string();
        self.saw_assistant_output = false;
        self.saw_structured_output = false;
    }

    pub(super) fn finish_worker(&mut self) {
        self.busy = false;
        self.current_turn_id = None;
        self.pending_interaction = None;
        if self.status != "queued" {
            self.status = "ready".to_string();
        }
    }

    pub(super) fn apply_turn_event(&mut self, event: AgentTurnEventInfo) {
        match event.event_type.as_str() {
            "agent.message.delta" | "assistant.delta" => {
                if let Some(text) = string_any_field(&event.data, &["text", "delta", "content"]) {
                    self.push_assistant_delta(&text);
                }
            }
            "assistant.completed" => {
                if let Some(text) = string_field(&event.data, "text") {
                    self.complete_assistant(&text);
                } else {
                    self.assistant_buffer.clear();
                }
            }
            "turn.started" => {
                self.status = string_any_field(&event.data, &["status", "state"])
                    .map(|status| format!("turn {status}"))
                    .unwrap_or_else(|| "turn started".to_string());
            }
            "tool.started" => {
                let tool = event_tool_name(&event);
                let suffix = value_any_field(&event.data, &["arguments", "input", "request"])
                    .and_then(|value| compact_json(value).ok())
                    .map(|value| format!(" {value}"))
                    .unwrap_or_default();
                self.push_tool(format!("{tool} started{suffix}"));
                self.status = format!("{tool} running");
            }
            "tool.completed" => {
                let tool = event_tool_name(&event);
                let status = string_any_field(&event.data, &["status", "state"]).or_else(|| {
                    event
                        .data
                        .get("statusCode")
                        .and_then(Value::as_i64)
                        .map(|v| v.to_string())
                });
                let mut text = match status {
                    Some(status) => format!("{tool} completed ({status})"),
                    None => format!("{tool} completed"),
                };
                if let Some(error) = string_field(&event.data, "error") {
                    text.push_str(&format!(": {error}"));
                } else if let Some(output) =
                    value_any_field(&event.data, &["output", "result", "body"])
                    && let Ok(encoded) = compact_json(output)
                {
                    text.push(' ');
                    text.push_str(&encoded);
                }
                self.push_tool(text);
                self.status = "tool completed".to_string();
            }
            "interaction.requested" => {
                let id = string_any_field(&event.data, &["interaction_id", "interactionId"])
                    .unwrap_or_else(|| "interaction".to_string());
                self.push_interaction(format!("requested {id}"));
                self.status = "waiting for input".to_string();
            }
            "interaction.resolved" => {
                let id = string_any_field(&event.data, &["interaction_id", "interactionId"])
                    .unwrap_or_else(|| "interaction".to_string());
                self.push_interaction(format!("resolved {id}"));
                self.status = "interaction resolved".to_string();
            }
            "turn.failed" => {
                if let Some(message) = string_field(&event.data, "error") {
                    self.push_error(format!("turn failed: {message}"));
                }
            }
            "turn.canceled" => {
                if let Some(reason) = string_field(&event.data, "reason") {
                    self.push_system(format!("turn canceled: {reason}"));
                } else {
                    self.push_system("turn canceled");
                }
            }
            "turn.completed" => {}
            _ if event.visibility == "private" => {}
            _ => self.push_system(generic_event_text(&event)),
        }
    }

    pub(super) fn finish_turn(&mut self, turn: &AgentTurnInfo) {
        let terminal = matches!(turn.status.as_str(), "succeeded" | "failed" | "canceled");
        match turn.status.as_str() {
            "succeeded" if !turn.output_text.is_empty() => {
                if !self.assistant_buffer.is_empty() {
                    let output_text = turn.output_text.clone();
                    self.complete_assistant(&output_text);
                } else if !self.saw_assistant_output {
                    self.push_assistant(turn.output_text.clone());
                }
            }
            "failed" if !turn.status_message.is_empty() => {
                self.push_error(format!("turn failed: {}", turn.status_message));
            }
            "canceled" if !turn.status_message.is_empty() => {
                self.push_system(format!("turn canceled: {}", turn.status_message));
            }
            _ => {}
        }
        if terminal {
            self.finish_assistant_stream();
        }
        if !self.saw_structured_output
            && let Some(structured_output) = turn.structured_output.as_ref()
            && let Ok(text) = pretty_json(structured_output)
            && terminal
        {
            self.push_system(format!("structured output\n{text}"));
            self.saw_structured_output = true;
        }
        self.status = turn_status_label(turn);
    }

    pub(super) fn wait_for_interaction(&mut self, interaction: AgentInteractionInfo) {
        self.status = "waiting for input".to_string();
        self.pending_interaction = Some(interaction);
    }

    pub(super) fn push_user(&mut self, text: String) {
        self.push(TranscriptKind::User, text);
    }

    fn push_assistant(&mut self, text: String) {
        self.saw_assistant_output = true;
        self.push(TranscriptKind::Assistant, text);
    }

    fn push_assistant_delta(&mut self, text: &str) {
        self.saw_assistant_output = true;
        self.assistant_buffer.push_str(text);
        if let Some(last) = self.transcript.last_mut()
            && last.kind == TranscriptKind::Assistant
            && last.streaming
        {
            last.text.push_str(text);
            return;
        }
        self.push_streaming(TranscriptKind::Assistant, text.to_string());
    }

    fn complete_assistant(&mut self, text: &str) {
        self.saw_assistant_output = true;
        if let Some(last) = self.transcript.last_mut()
            && last.kind == TranscriptKind::Assistant
            && last.streaming
        {
            if self.assistant_buffer.is_empty() {
                last.text.push_str(text);
            } else if let Some(suffix) = text.strip_prefix(&self.assistant_buffer) {
                last.text.push_str(suffix);
            }
            last.streaming = false;
            self.assistant_buffer.clear();
            return;
        }
        self.push_assistant(text.to_string());
        self.assistant_buffer.clear();
    }

    fn finish_assistant_stream(&mut self) {
        for item in &mut self.transcript {
            if item.kind == TranscriptKind::Assistant {
                item.streaming = false;
            }
        }
        self.assistant_buffer.clear();
    }

    fn push_tool(&mut self, text: String) {
        self.push(TranscriptKind::Tool, text);
    }

    pub(super) fn push_interaction(&mut self, text: String) {
        self.push(TranscriptKind::Interaction, text);
    }

    pub(super) fn push_system(&mut self, text: impl Into<String>) {
        self.push(TranscriptKind::System, text.into());
    }

    pub(super) fn push_error(&mut self, text: String) {
        self.push(TranscriptKind::Error, text);
    }

    fn push(&mut self, kind: TranscriptKind, text: String) {
        self.transcript.push(TranscriptItem {
            kind,
            text,
            streaming: false,
        });
        self.trim_transcript();
    }

    fn push_streaming(&mut self, kind: TranscriptKind, text: String) {
        self.transcript.push(TranscriptItem {
            kind,
            text,
            streaming: true,
        });
        self.trim_transcript();
    }

    fn trim_transcript(&mut self) {
        if self.transcript.len() > MAX_TRANSCRIPT_ITEMS {
            let remove = self.transcript.len() - MAX_TRANSCRIPT_ITEMS;
            self.transcript.drain(0..remove);
        }
    }

    pub(super) fn visible_transcript(&self, height: usize) -> &[TranscriptItem] {
        let visible = height.max(1);
        let offset = self.scroll_offset.min(self.max_scroll_offset(visible));
        let end = self.transcript.len().saturating_sub(offset);
        let start = end.saturating_sub(visible);
        &self.transcript[start..end]
    }

    pub(super) fn scroll_up(&mut self, height: usize) {
        self.scroll_offset = self
            .scroll_offset
            .saturating_add(5)
            .min(self.max_scroll_offset(height));
    }

    pub(super) fn scroll_down(&mut self) {
        self.scroll_offset = self.scroll_offset.saturating_sub(5);
    }

    pub(super) fn scroll_to_top(&mut self, height: usize) {
        self.scroll_offset = self.max_scroll_offset(height);
    }

    pub(super) fn scroll_to_bottom(&mut self) {
        self.scroll_offset = 0;
    }

    fn max_scroll_offset(&self, height: usize) -> usize {
        self.transcript.len().saturating_sub(height.max(1))
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(super) enum TranscriptKind {
    User,
    Assistant,
    Tool,
    Interaction,
    System,
    Error,
}

impl TranscriptKind {
    pub(super) fn label(self) -> &'static str {
        match self {
            Self::User => "you>",
            Self::Assistant => "assistant>",
            Self::Tool => "tool>",
            Self::Interaction => "interaction>",
            Self::System => "system>",
            Self::Error => "error>",
        }
    }
}

pub(super) struct TranscriptItem {
    pub(super) kind: TranscriptKind,
    pub(super) text: String,
    streaming: bool,
}

fn event_tool_name(event: &AgentTurnEventInfo) -> String {
    string_any_field(
        &event.data,
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

fn generic_event_text(event: &AgentTurnEventInfo) -> String {
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
        compact_json(&Value::Object(event.data.clone()))
            .map(|data| format!("{}{} {}", event.event_type, suffix, data))
            .unwrap_or_else(|_| format!("{}{}", event.event_type, suffix))
    }
}

pub(super) fn turn_status_label(turn: &AgentTurnInfo) -> String {
    if turn.status.is_empty() {
        "running".to_string()
    } else {
        turn.status.clone()
    }
}
