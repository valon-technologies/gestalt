use std::time::{Duration, Instant};

use ratatui::style::{Color, Style};
use serde_json::Value;
use unicode_segmentation::UnicodeSegmentation;
use unicode_width::UnicodeWidthStr;

use super::super::{
    AgentInteractionInfo, AgentSessionInfo, AgentTurnDisplayInfo, AgentTurnEventInfo,
    AgentTurnInfo, compact_json, display_status, display_text, display_tool_error,
    display_tool_input, display_tool_label, display_tool_output, display_tool_ref,
    display_value_text, number_any_field, pretty_json, string_any_field, string_field,
    turn_event_display, value_any_field,
};

const MAX_TRANSCRIPT_ITEMS: usize = 500;
const TOOL_PREVIEW_MAX_WIDTH: usize = 120;

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
    turn_started_at: Option<Instant>,
    turn_elapsed_reported: bool,
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
            turn_started_at: None,
            turn_elapsed_reported: false,
        }
    }

    pub(super) fn start_turn(&mut self) {
        self.finish_assistant_stream();
        self.finish_running_tool_activities("turn replaced");
        self.busy = true;
        self.current_turn_id = None;
        self.pending_interaction = None;
        self.status = "starting turn".to_string();
        self.saw_assistant_output = false;
        self.saw_structured_output = false;
        self.turn_started_at = Some(Instant::now());
        self.turn_elapsed_reported = false;
    }

    pub(super) fn finish_worker(&mut self) {
        self.finish_running_tool_activities("turn ended");
        self.busy = false;
        self.current_turn_id = None;
        self.pending_interaction = None;
        if self.status != "queued" {
            self.status = "ready".to_string();
        }
    }

    pub(super) fn apply_turn_event(&mut self, event: AgentTurnEventInfo) {
        if self.apply_display_turn_event(&event) {
            return;
        }
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
                let activity = ToolActivity::started(&event);
                let tool = activity.name.clone();
                self.push_tool_activity(activity);
                self.status = format!("{tool} running");
            }
            "tool.completed" => {
                let info = ToolTerminalEvent::from_completed(&event);
                self.finish_tool_activity(info);
            }
            "tool.failed" => {
                let info = ToolTerminalEvent::from_failed(&event);
                self.finish_tool_activity(info);
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

    fn apply_display_turn_event(&mut self, event: &AgentTurnEventInfo) -> bool {
        let Some(display) = turn_event_display(event) else {
            return false;
        };
        match display.kind.trim() {
            "text" => {
                match display.phase.trim() {
                    "delta" => {
                        if let Some(text) = display_text(display) {
                            self.push_assistant_delta(text);
                        } else {
                            return false;
                        }
                    }
                    "completed" => {
                        if let Some(text) = display_text(display) {
                            self.complete_assistant(text);
                        } else {
                            return false;
                        }
                    }
                    _ => {
                        if let Some(text) = display_text(display) {
                            self.push_assistant(text.to_string());
                        } else {
                            return false;
                        }
                    }
                }
                true
            }
            "reasoning" => {
                if let Some(text) = display_text(display) {
                    self.push_system(format!("reasoning: {text}"));
                } else {
                    return false;
                }
                true
            }
            "tool" => {
                match display.phase.trim() {
                    "started" => {
                        let activity = ToolActivity::started_display(event, display);
                        let tool = activity.name.clone();
                        self.push_tool_activity(activity);
                        self.status = format!("{tool} running");
                    }
                    "progress" => {
                        self.apply_tool_progress(event, display);
                    }
                    "completed" | "failed" => {
                        let info = ToolTerminalEvent::from_display(event, display);
                        self.finish_tool_activity(info);
                    }
                    _ => return false,
                }
                true
            }
            "interaction" => {
                let id = if display.display_ref.trim().is_empty() {
                    "interaction".to_string()
                } else {
                    display.display_ref.trim().to_string()
                };
                match display.phase.trim() {
                    "requested" => {
                        self.push_interaction(format!("requested {id}"));
                        self.status = "waiting for input".to_string();
                    }
                    "resolved" => {
                        self.push_interaction(format!("resolved {id}"));
                        self.status = "interaction resolved".to_string();
                    }
                    _ => return false,
                }
                true
            }
            "status" => {
                match display.phase.trim() {
                    "started" => {
                        self.status = display_text(display)
                            .map(|status| format!("turn {status}"))
                            .unwrap_or_else(|| "turn started".to_string());
                    }
                    "canceled" => {
                        if let Some(reason) = display_text(display) {
                            self.push_system(format!("turn canceled: {reason}"));
                        } else {
                            self.push_system("turn canceled");
                        }
                    }
                    "progress" => {
                        if let Some(text) = display_text(display) {
                            self.status = text.to_string();
                        }
                    }
                    "completed" => {
                        self.status = display_text(display)
                            .map(ToString::to_string)
                            .unwrap_or_else(|| "completed".to_string());
                    }
                    _ => return false,
                }
                true
            }
            "error" => {
                let text = display_text(display).map(ToString::to_string).or_else(|| {
                    display
                        .error
                        .as_ref()
                        .and_then(|value| display_value_text(value).ok())
                });
                let Some(text) = text else {
                    return false;
                };
                if display.label.trim() == "turn" || display.phase.trim() == "failed" {
                    self.push_error(format!("turn failed: {text}"));
                } else {
                    self.push_error(text.to_string());
                }
                true
            }
            _ => false,
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
            self.finish_running_tool_activities(&format!("turn {}", turn.status));
        }
        if !self.saw_structured_output
            && let Some(structured_output) = turn.structured_output.as_ref()
            && let Ok(text) = pretty_json(structured_output)
            && terminal
        {
            self.push_system(format!("structured output\n{text}"));
            self.saw_structured_output = true;
        }
        if terminal {
            self.push_turn_elapsed();
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

    fn push_tool_activity(&mut self, activity: ToolActivity) {
        self.transcript
            .push(TranscriptItem::tool_activity(activity));
        self.trim_transcript();
    }

    fn finish_tool_activity(&mut self, terminal: ToolTerminalEvent) {
        let tool = terminal.name.clone();
        let terminal_status = terminal.status_label();
        if let Some(item) = self.find_running_tool_activity_mut(&terminal) {
            if let Some(activity) = item.tool_activity.as_mut() {
                activity.finish(terminal);
                item.text = activity.render_text();
            }
        } else {
            self.push_tool_activity(ToolActivity::terminal(terminal));
        }
        self.status = terminal_status.unwrap_or_else(|| format!("{tool} completed"));
    }

    fn apply_tool_progress(&mut self, event: &AgentTurnEventInfo, display: &AgentTurnDisplayInfo) {
        let progress = ToolTerminalEvent::from_display_progress(event, display);
        let tool = progress.name.clone();
        if let Some(item) = self.find_running_tool_activity_mut(&progress) {
            if let Some(activity) = item.tool_activity.as_mut() {
                activity.update_progress(progress);
                item.text = activity.render_text();
            }
        } else {
            self.push_tool_activity(ToolActivity::progress(progress));
        }
        self.status = format!("{tool} running");
    }

    fn find_running_tool_activity_mut(
        &mut self,
        terminal: &ToolTerminalEvent,
    ) -> Option<&mut TranscriptItem> {
        let index = self.find_running_tool_activity_index(terminal)?;
        self.transcript.get_mut(index)
    }

    fn find_running_tool_activity_index(&self, terminal: &ToolTerminalEvent) -> Option<usize> {
        if let Some(key) = terminal.key.as_deref()
            && let Some(index) = self.transcript.iter().position(|item| {
                item.tool_activity
                    .as_ref()
                    .is_some_and(|activity| activity.is_running_key_match(key))
            })
        {
            return Some(index);
        }

        let mut candidates = self
            .transcript
            .iter()
            .enumerate()
            .filter(|(_, item)| {
                item.tool_activity
                    .as_ref()
                    .is_some_and(|activity| activity.is_running_name_match(&terminal.name))
            })
            .map(|(index, _)| index);
        let candidate = candidates.next()?;
        if candidates.next().is_some() {
            return None;
        }
        Some(candidate)
    }

    fn finish_running_tool_activities(&mut self, reason: &str) {
        for item in &mut self.transcript {
            if let Some(activity) = item.tool_activity.as_mut()
                && matches!(activity.status, ToolActivityStatus::Running)
            {
                activity.end(reason);
                item.text = activity.render_text();
            }
        }
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

    fn push_meta(&mut self, text: impl Into<String>) {
        self.push(TranscriptKind::Meta, text.into());
    }

    fn push_turn_elapsed(&mut self) {
        if self.turn_elapsed_reported {
            return;
        }
        self.turn_elapsed_reported = true;
        let Some(started_at) = self.turn_started_at.take() else {
            return;
        };
        self.push_meta(format!(
            "Brewed for {}",
            format_brewed_duration(started_at.elapsed())
        ));
    }

    fn push(&mut self, kind: TranscriptKind, text: String) {
        self.transcript.push(TranscriptItem {
            kind,
            text,
            streaming: false,
            tool_activity: None,
        });
        self.trim_transcript();
    }

    fn push_streaming(&mut self, kind: TranscriptKind, text: String) {
        self.transcript.push(TranscriptItem {
            kind,
            text,
            streaming: true,
            tool_activity: None,
        });
        self.trim_transcript();
    }

    fn trim_transcript(&mut self) {
        while self.transcript.len() > MAX_TRANSCRIPT_ITEMS {
            if let Some(remove) = self
                .transcript
                .iter()
                .position(|item| !item.has_running_tool_activity())
            {
                self.transcript.remove(remove);
                continue;
            }

            if let Some(item) = self.transcript.first_mut()
                && let Some(activity) = item.tool_activity.as_mut()
            {
                activity.end("trimmed");
                item.text = activity.render_text();
            }
        }
    }

    pub(super) fn transcript(&self) -> &[TranscriptItem] {
        &self.transcript
    }

    pub(super) fn scroll_offset(&self) -> usize {
        self.scroll_offset
    }

    pub(super) fn scroll_up(&mut self, height: usize, content_height: usize) {
        self.scroll_offset = self
            .scroll_offset
            .saturating_add(5)
            .min(max_scroll_offset(height, content_height));
    }

    pub(super) fn scroll_down(&mut self) {
        self.scroll_offset = self.scroll_offset.saturating_sub(5);
    }

    pub(super) fn scroll_to_top(&mut self, height: usize, content_height: usize) {
        self.scroll_offset = max_scroll_offset(height, content_height);
    }

    pub(super) fn scroll_to_bottom(&mut self) {
        self.scroll_offset = 0;
    }

    pub(super) fn clamp_scroll_offset(&mut self, height: usize, content_height: usize) {
        self.scroll_offset = self
            .scroll_offset
            .min(max_scroll_offset(height, content_height));
    }
}

fn max_scroll_offset(height: usize, content_height: usize) -> usize {
    content_height.saturating_sub(height.max(1))
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(super) enum TranscriptKind {
    User,
    Assistant,
    Tool,
    Interaction,
    System,
    Error,
    Meta,
}

impl TranscriptKind {
    pub(super) fn header(self) -> &'static str {
        match self {
            Self::User => "You",
            Self::Assistant => "Assistant",
            Self::Tool => "Tool",
            Self::Interaction => "Interaction",
            Self::System => "System",
            Self::Error => "Error",
            Self::Meta => "",
        }
    }

    pub(super) fn header_style(self) -> Style {
        Style::default().fg(match self {
            Self::User => Color::Cyan,
            Self::Assistant => Color::Green,
            Self::Tool => Color::Yellow,
            Self::Interaction => Color::Magenta,
            Self::System => Color::Blue,
            Self::Error => Color::Red,
            Self::Meta => Color::DarkGray,
        })
    }

    pub(super) fn body_style(self) -> Style {
        Style::default().fg(match self {
            Self::User => Color::White,
            Self::Assistant => Color::White,
            Self::Tool => Color::Gray,
            Self::Interaction => Color::Gray,
            Self::System => Color::Gray,
            Self::Error => Color::LightRed,
            Self::Meta => Color::DarkGray,
        })
    }
}

pub(super) struct TranscriptItem {
    pub(super) kind: TranscriptKind,
    pub(super) text: String,
    streaming: bool,
    tool_activity: Option<ToolActivity>,
}

impl TranscriptItem {
    fn tool_activity(activity: ToolActivity) -> Self {
        let text = activity.render_text();
        Self {
            kind: TranscriptKind::Tool,
            text,
            streaming: false,
            tool_activity: Some(activity),
        }
    }

    fn has_running_tool_activity(&self) -> bool {
        self.tool_activity
            .as_ref()
            .is_some_and(|activity| matches!(activity.status, ToolActivityStatus::Running))
    }
}

struct ToolActivity {
    key: Option<String>,
    name: String,
    status: ToolActivityStatus,
    started_at: Option<Instant>,
    elapsed: Option<Duration>,
    args: Option<String>,
    output: Option<String>,
    error: Option<String>,
}

impl ToolActivity {
    fn started(event: &AgentTurnEventInfo) -> Self {
        Self {
            key: event_tool_key(event),
            name: event_tool_name(event),
            status: ToolActivityStatus::Running,
            started_at: Some(Instant::now()),
            elapsed: None,
            args: preview_value(&event.data, &["arguments", "input", "request"]),
            output: None,
            error: None,
        }
    }

    fn started_display(event: &AgentTurnEventInfo, display: &AgentTurnDisplayInfo) -> Self {
        Self {
            key: display_tool_ref(event, display),
            name: display_tool_label(event, display),
            status: ToolActivityStatus::Running,
            started_at: Some(Instant::now()),
            elapsed: None,
            args: preview_display_value(display_tool_input(event, display)),
            output: None,
            error: None,
        }
    }

    fn progress(progress: ToolTerminalEvent) -> Self {
        Self {
            key: progress.key.clone(),
            name: progress.name.clone(),
            status: ToolActivityStatus::Running,
            started_at: Some(Instant::now()),
            elapsed: None,
            args: progress.args.clone(),
            output: progress.output.clone(),
            error: progress.error.clone(),
        }
    }

    fn terminal(terminal: ToolTerminalEvent) -> Self {
        let mut activity = Self {
            key: terminal.key.clone(),
            name: terminal.name.clone(),
            status: ToolActivityStatus::Running,
            started_at: None,
            elapsed: None,
            args: terminal.args.clone(),
            output: None,
            error: None,
        };
        activity.finish(terminal);
        activity
    }

    fn finish(&mut self, terminal: ToolTerminalEvent) {
        self.status = terminal.status;
        if self.args.is_none() {
            self.args = terminal.args;
        }
        self.output = terminal.output;
        self.error = terminal.error;
        self.elapsed = self.started_at.map(|started_at| started_at.elapsed());
    }

    fn update_progress(&mut self, progress: ToolTerminalEvent) {
        if self.args.is_none() {
            self.args = progress.args;
        }
        if progress.output.is_some() {
            self.output = progress.output;
        }
        if progress.error.is_some() {
            self.error = progress.error;
        }
    }

    fn end(&mut self, reason: &str) {
        self.status = ToolActivityStatus::Ended(reason.to_string());
        self.elapsed = self.started_at.map(|started_at| started_at.elapsed());
    }

    fn is_running_key_match(&self, key: &str) -> bool {
        matches!(self.status, ToolActivityStatus::Running) && self.key.as_deref() == Some(key)
    }

    fn is_running_name_match(&self, name: &str) -> bool {
        matches!(self.status, ToolActivityStatus::Running) && self.name == name
    }

    fn render_text(&self) -> String {
        let mut text = format!("{} {}", self.name, self.status.summary());
        let mut meta = Vec::new();
        if let Some(status) = self.status.detail() {
            meta.push(status);
        }
        if let Some(elapsed) = self.elapsed {
            meta.push(format_duration(elapsed));
        }
        if !meta.is_empty() {
            text.push_str(&format!(" ({})", meta.join(", ")));
        }
        if let Some(args) = self.args.as_deref() {
            text.push_str(&format!("\n  args {args}"));
        }
        if let Some(output) = self.output.as_deref() {
            text.push_str(&format!("\n  output {output}"));
        }
        if let Some(error) = self.error.as_deref() {
            text.push_str(&format!("\n  error {error}"));
        }
        text
    }
}

struct ToolTerminalEvent {
    key: Option<String>,
    name: String,
    status: ToolActivityStatus,
    args: Option<String>,
    output: Option<String>,
    error: Option<String>,
}

impl ToolTerminalEvent {
    fn from_completed(event: &AgentTurnEventInfo) -> Self {
        let error = string_field(&event.data, "error").map(|value| truncate_preview(&value));
        let status = if error.is_some() {
            ToolActivityStatus::Failed(tool_status(&event.data))
        } else {
            ToolActivityStatus::Completed(tool_status(&event.data))
        };
        Self {
            key: event_tool_key(event),
            name: event_tool_name(event),
            status,
            args: preview_value(&event.data, &["arguments", "input", "request"]),
            output: preview_value(&event.data, &["output", "result", "body"]),
            error,
        }
    }

    fn from_failed(event: &AgentTurnEventInfo) -> Self {
        Self {
            key: event_tool_key(event),
            name: event_tool_name(event),
            status: ToolActivityStatus::Failed(tool_status(&event.data)),
            args: preview_value(&event.data, &["arguments", "input", "request"]),
            output: preview_value(&event.data, &["output", "result", "body"]),
            error: string_any_field(&event.data, &["error", "message"])
                .map(|value| truncate_preview(&value)),
        }
    }

    fn from_display(event: &AgentTurnEventInfo, display: &AgentTurnDisplayInfo) -> Self {
        let error = preview_display_error(display_tool_error(event, display));
        let status = if display.phase.trim() == "failed" || error.is_some() {
            ToolActivityStatus::Failed(display_status(event, display))
        } else {
            ToolActivityStatus::Completed(display_status(event, display))
        };
        Self {
            key: display_tool_ref(event, display),
            name: display_tool_label(event, display),
            status,
            args: preview_display_value(display_tool_input(event, display)),
            output: preview_display_value(display_tool_output(event, display)),
            error,
        }
    }

    fn from_display_progress(event: &AgentTurnEventInfo, display: &AgentTurnDisplayInfo) -> Self {
        Self {
            key: display_tool_ref(event, display),
            name: display_tool_label(event, display),
            status: ToolActivityStatus::Running,
            args: preview_display_value(display_tool_input(event, display)),
            output: preview_display_value(display_tool_output(event, display))
                .or_else(|| display_text(display).map(truncate_preview)),
            error: preview_display_error(display_tool_error(event, display)),
        }
    }

    fn status_label(&self) -> Option<String> {
        match self.status {
            ToolActivityStatus::Running => None,
            ToolActivityStatus::Completed(_) => Some(format!("{} completed", self.name)),
            ToolActivityStatus::Failed(_) => Some(format!("{} failed", self.name)),
            ToolActivityStatus::Ended(_) => Some(format!("{} ended", self.name)),
        }
    }
}

enum ToolActivityStatus {
    Running,
    Completed(Option<String>),
    Failed(Option<String>),
    Ended(String),
}

impl ToolActivityStatus {
    fn summary(&self) -> &'static str {
        match self {
            Self::Running => "running",
            Self::Completed(_) => "completed",
            Self::Failed(_) => "failed",
            Self::Ended(_) => "ended",
        }
    }

    fn detail(&self) -> Option<String> {
        match self {
            Self::Running => None,
            Self::Completed(status) | Self::Failed(status) => status.clone(),
            Self::Ended(reason) => Some(reason.clone()),
        }
    }
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

fn event_tool_key(event: &AgentTurnEventInfo) -> Option<String> {
    string_any_field(
        &event.data,
        &[
            "tool_call_id",
            "toolCallId",
            "call_id",
            "callId",
            "invocation_id",
            "invocationId",
            "tool_use_id",
            "toolUseId",
            "id",
        ],
    )
}

fn tool_status(data: &serde_json::Map<String, Value>) -> Option<String> {
    string_any_field(data, &["status", "state"]).or_else(|| {
        number_any_field(data, &["status", "statusCode"]).map(|status| status.to_string())
    })
}

fn preview_value(data: &serde_json::Map<String, Value>, keys: &[&str]) -> Option<String> {
    value_any_field(data, keys)
        .and_then(|value| compact_json(value).ok())
        .map(|value| truncate_preview(&value))
}

fn preview_display_value(value: Option<&Value>) -> Option<String> {
    value
        .and_then(|value| compact_json(value).ok())
        .map(|value| truncate_preview(&value))
}

fn preview_display_error(value: Option<&Value>) -> Option<String> {
    value
        .and_then(|value| display_value_text(value).ok())
        .map(|value| truncate_preview(&value))
}

fn truncate_preview(value: &str) -> String {
    let mut preview = String::new();
    let mut width = 0usize;
    for grapheme in UnicodeSegmentation::graphemes(value, true) {
        let grapheme_width = UnicodeWidthStr::width(grapheme);
        if width > 0 && width.saturating_add(grapheme_width) > TOOL_PREVIEW_MAX_WIDTH {
            preview.push_str("...");
            return preview;
        }
        preview.push_str(grapheme);
        width += grapheme_width;
    }
    preview
}

fn format_duration(duration: Duration) -> String {
    if duration.as_secs() > 0 {
        format!("{:.1}s", duration.as_secs_f64())
    } else {
        format!("{}ms", duration.as_millis())
    }
}

fn format_brewed_duration(duration: Duration) -> String {
    let seconds = duration.as_secs();
    if seconds >= 60 {
        let minutes = seconds / 60;
        let remaining_seconds = seconds % 60;
        if remaining_seconds == 0 {
            format!("{minutes}m")
        } else {
            format!("{minutes}m {remaining_seconds}s")
        }
    } else if seconds > 0 {
        format!("{seconds}s")
    } else {
        format!("{}ms", duration.as_millis())
    }
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
