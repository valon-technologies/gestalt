use std::collections::VecDeque;
use std::io::{self, IsTerminal};
use std::sync::mpsc::{self, Receiver, Sender, TryRecvError};
use std::thread;
use std::time::Duration;

use anyhow::{Context, Result};
use ratatui::crossterm::{
    event::{
        self, DisableMouseCapture, EnableMouseCapture, Event, KeyCode, KeyEvent, KeyEventKind,
        KeyModifiers, MouseEventKind,
    },
    execute,
};
use ratatui::prelude::*;
use ratatui::widgets::{Block, Borders, Clear, Paragraph, Wrap};
use ratatui_textarea::{CursorMove, TextArea};
use serde_json::{Map, Value};
use unicode_segmentation::UnicodeSegmentation;
use unicode_width::UnicodeWidthStr;

use crate::api::ApiClient;
use crate::cli::{AgentArgs, AgentTurnCreateArgs};

mod state;
mod worker;

use state::{AgentUiState, TranscriptItem, turn_status_label};
use worker::{TurnWorker, WorkerCommand, WorkerEvent, spawn_turn_worker};

use super::{
    AgentInteractionInfo, AgentShell, INTERRUPT_CANCEL_REASON, cancel_turn_silent, compact_json,
};

const TICK_RATE: Duration = Duration::from_millis(50);
const HISTORY_LIMIT: usize = 100;
const SPINNER_FRAMES: [&str; 4] = ["|", "/", "-", "\\"];
const USER_PROMPT: &str = "› ";
const ASSISTANT_BULLET: &str = "● ";
const META_PREFIX: &str = "* ";

pub(super) fn can_run() -> bool {
    io::stdin().is_terminal() && io::stdout().is_terminal() && io::stderr().is_terminal()
}

pub(super) fn run(client: &ApiClient, args: &AgentArgs) -> Result<()> {
    let shell = AgentShell::connect(client, args)?;
    let mut app = TuiApp::new(client.clone(), shell);
    let mut terminal = TerminalGuard::start()?;

    if !args.messages.is_empty() {
        app.enqueue_or_start(args.messages.clone());
    }

    let result = app.run(terminal.inner_mut());
    terminal.restore()?;
    result
}

struct TerminalGuard {
    terminal: ratatui::DefaultTerminal,
    restored: bool,
}

impl TerminalGuard {
    fn start() -> Result<Self> {
        let terminal = ratatui::try_init().context("failed to initialize terminal UI")?;
        if let Err(error) = execute!(io::stdout(), EnableMouseCapture) {
            ratatui::restore();
            return Err(error).context("failed to enable mouse capture");
        }
        Ok(Self {
            terminal,
            restored: false,
        })
    }

    fn inner_mut(&mut self) -> &mut ratatui::DefaultTerminal {
        &mut self.terminal
    }

    fn restore(&mut self) -> Result<()> {
        if !self.restored {
            let mouse_result = execute!(io::stdout(), DisableMouseCapture);
            let restore_result = ratatui::try_restore();
            mouse_result.context("failed to disable mouse capture")?;
            restore_result.context("failed to restore terminal UI")?;
            self.restored = true;
        }
        Ok(())
    }
}

impl Drop for TerminalGuard {
    fn drop(&mut self) {
        if !self.restored {
            let _ = execute!(io::stdout(), DisableMouseCapture);
            ratatui::restore();
            self.restored = true;
        }
    }
}

struct TuiApp {
    client: ApiClient,
    shell: AgentShell,
    state: AgentUiState,
    composer: TextArea<'static>,
    interaction_input: InteractionInput,
    worker: Option<TurnWorker>,
    worker_rx: Receiver<WorkerEvent>,
    worker_tx: Sender<WorkerEvent>,
    queued_messages: VecDeque<Vec<String>>,
    should_quit: bool,
    quit_after_turn: bool,
    transcript_visible_height: usize,
    transcript_content_height: usize,
    tick: usize,
    input_history: Vec<String>,
    history_cursor: Option<usize>,
    history_draft: String,
}

impl TuiApp {
    fn new(client: ApiClient, shell: AgentShell) -> Self {
        let (worker_tx, worker_rx) = mpsc::channel();
        Self {
            state: AgentUiState::new(&shell.session),
            client,
            shell,
            composer: styled_textarea("Message"),
            interaction_input: InteractionInput::default(),
            worker: None,
            worker_rx,
            worker_tx,
            queued_messages: VecDeque::new(),
            should_quit: false,
            quit_after_turn: false,
            transcript_visible_height: 1,
            transcript_content_height: 0,
            tick: 0,
            input_history: Vec::new(),
            history_cursor: None,
            history_draft: String::new(),
        }
    }

    fn run(&mut self, terminal: &mut ratatui::DefaultTerminal) -> Result<()> {
        loop {
            self.drain_worker_events();
            self.start_next_queued_turn();
            self.tick = self.tick.wrapping_add(1);
            terminal.draw(|frame| self.draw(frame))?;

            if self.should_quit {
                return Ok(());
            }

            if event::poll(TICK_RATE).context("failed to poll terminal events")? {
                match event::read().context("failed to read terminal event")? {
                    Event::Key(key) if key.kind == KeyEventKind::Press => self.handle_key(key),
                    Event::Mouse(mouse) => match mouse.kind {
                        MouseEventKind::ScrollUp => self.state.scroll_up(
                            self.transcript_visible_height,
                            self.transcript_content_height,
                        ),
                        MouseEventKind::ScrollDown => self.state.scroll_down(),
                        _ => {}
                    },
                    Event::Resize(_, _) => {}
                    _ => {}
                }
            }
        }
    }

    fn draw(&mut self, frame: &mut Frame<'_>) {
        let area = frame.area();
        let interaction_height = if self.state.pending_interaction.is_some() {
            7
        } else {
            0
        };
        let chunks = Layout::vertical([
            Constraint::Min(3),
            Constraint::Length(interaction_height),
            Constraint::Length(self.composer_height()),
            Constraint::Length(1),
        ])
        .split(area);

        self.draw_transcript(frame, chunks[0]);
        if self.state.pending_interaction.is_some() {
            self.draw_interaction(frame, chunks[1]);
        }
        self.draw_composer(frame, chunks[2]);
        self.draw_footer(frame, chunks[3]);
    }

    fn draw_transcript(&mut self, frame: &mut Frame<'_>, area: Rect) {
        let title = format!(
            "Session {} [{} / {}]",
            self.state.session_id, self.state.provider, self.state.model
        );
        self.transcript_visible_height = area.height.saturating_sub(2).max(1) as usize;
        let rendered_lines = self.transcript_lines(area);
        self.transcript_content_height = rendered_lines.len();
        self.state.clamp_scroll_offset(
            self.transcript_visible_height,
            self.transcript_content_height,
        );
        let mut lines = visible_lines(
            rendered_lines,
            self.transcript_visible_height,
            self.state.scroll_offset(),
        );
        if lines.is_empty() {
            lines.push(Line::from("Start typing below."));
        }

        let paragraph =
            Paragraph::new(lines).block(Block::default().borders(Borders::ALL).title(title));
        frame.render_widget(paragraph, area);
    }

    fn transcript_lines(&self, area: Rect) -> Vec<Line<'static>> {
        let content_width = area.width.saturating_sub(2).max(1) as usize;
        let mut lines = Vec::new();
        for (index, item) in self.state.transcript().iter().enumerate() {
            if index > 0 && item.kind != state::TranscriptKind::Meta {
                lines.push(Line::from(""));
            }
            push_transcript_item_lines(&mut lines, item, content_width);
        }
        lines
    }

    fn draw_interaction(&mut self, frame: &mut Frame<'_>, area: Rect) {
        let Some(interaction) = self.state.pending_interaction.as_ref() else {
            return;
        };
        frame.render_widget(Clear, area);

        let title = if interaction.title.is_empty() {
            format!("Interaction {}", interaction.id)
        } else {
            interaction.title.clone()
        };
        let body = if interaction.interaction_type == "approval" {
            interaction_summary(interaction, None, "Press y to approve, n to deny.")
        } else {
            let help = if interaction.request.get("secret").and_then(Value::as_bool) == Some(true) {
                "Enter submits. Typed secret input is masked."
            } else {
                "Enter submits. Alt-Enter inserts a newline."
            };
            interaction_summary(interaction, self.interaction_input.validation(), help)
        };
        let block = Block::default()
            .borders(Borders::ALL)
            .border_style(Style::default().fg(Color::Magenta))
            .title(title);
        frame.render_widget(
            Paragraph::new(body).block(block).wrap(Wrap { trim: false }),
            area,
        );

        if interaction.interaction_type != "approval" && area.height > 4 {
            let input_area = Rect {
                x: area.x + 1,
                y: area.y + area.height.saturating_sub(3),
                width: area.width.saturating_sub(2),
                height: 2,
            };
            self.interaction_input.render(frame, input_area);
        }
    }

    fn draw_composer(&mut self, frame: &mut Frame<'_>, area: Rect) {
        let queued = self.queued_messages.len();
        let title = if queued > 0 {
            format!("Message - {queued} queued")
        } else if self.state.busy {
            "Message - working".to_string()
        } else {
            "Message".to_string()
        };
        self.composer.set_block(
            Block::default()
                .borders(Borders::ALL)
                .border_style(Style::default().fg(Color::Cyan))
                .title(title),
        );
        frame.render_widget(&self.composer, area);
    }

    fn draw_footer(&self, frame: &mut Frame<'_>, area: Rect) {
        let status = if self.state.busy {
            let queued = if self.queued_messages.is_empty() {
                String::new()
            } else {
                format!(" | queued {}", self.queued_messages.len())
            };
            format!(
                "{} {}{} | Enter queue | Alt-Enter newline | Up/Down history | PgUp/PgDn/wheel scroll | Ctrl-C cancel",
                self.activity_indicator(),
                self.state.status,
                queued
            )
        } else {
            format!(
                "{} | Enter send | Alt-Enter newline | Up/Down history | PgUp/PgDn/wheel scroll | Ctrl-C clear/exit",
                self.state.status
            )
        };
        frame.render_widget(
            Paragraph::new(status).style(Style::default().fg(Color::DarkGray)),
            area,
        );
    }

    fn activity_indicator(&self) -> &'static str {
        SPINNER_FRAMES[(self.tick / 2) % SPINNER_FRAMES.len()]
    }

    fn composer_height(&self) -> u16 {
        let lines = self.composer.lines().len().max(1) as u16;
        lines.saturating_add(2).clamp(3, 8)
    }

    fn handle_key(&mut self, key: KeyEvent) {
        if self.state.pending_interaction.is_some() {
            self.handle_interaction_key(key);
            return;
        }

        match (key.code, key.modifiers) {
            (KeyCode::Char('c'), modifiers) if modifiers.contains(KeyModifiers::CONTROL) => {
                self.handle_interrupt();
            }
            (KeyCode::PageUp, _) => self.state.scroll_up(
                self.transcript_visible_height,
                self.transcript_content_height,
            ),
            (KeyCode::PageDown, _) => self.state.scroll_down(),
            (KeyCode::Home, modifiers) if modifiers.contains(KeyModifiers::CONTROL) => {
                self.state.scroll_to_top(
                    self.transcript_visible_height,
                    self.transcript_content_height,
                );
            }
            (KeyCode::End, modifiers) if modifiers.contains(KeyModifiers::CONTROL) => {
                self.state.scroll_to_bottom();
            }
            (KeyCode::Up, modifiers) if modifiers.is_empty() && self.can_step_history_back() => {
                self.history_previous();
            }
            (KeyCode::Down, modifiers) if modifiers.is_empty() && self.history_cursor.is_some() => {
                self.history_next();
            }
            (KeyCode::Enter, modifiers) if modifiers.contains(KeyModifiers::ALT) => {
                self.reset_history_navigation();
                self.composer.insert_newline();
            }
            (KeyCode::Enter, _) => self.submit_composer(),
            _ => {
                self.reset_history_navigation();
                self.composer.input(key);
            }
        }
    }

    fn handle_interaction_key(&mut self, key: KeyEvent) {
        let Some(interaction) = self.state.pending_interaction.clone() else {
            return;
        };
        if interaction.interaction_type == "approval" {
            match (key.code, key.modifiers) {
                (KeyCode::Char('c'), modifiers) if modifiers.contains(KeyModifiers::CONTROL) => {
                    self.handle_interrupt();
                }
                (KeyCode::Char('y') | KeyCode::Char('Y'), _) => {
                    self.resolve_pending_interaction(Map::from_iter([(
                        "approved".to_string(),
                        Value::Bool(true),
                    )]));
                }
                (KeyCode::Char('n') | KeyCode::Char('N'), _) => {
                    self.resolve_pending_interaction(Map::from_iter([(
                        "approved".to_string(),
                        Value::Bool(false),
                    )]));
                }
                _ => {}
            }
            return;
        }

        match (key.code, key.modifiers) {
            (KeyCode::Char('c'), modifiers) if modifiers.contains(KeyModifiers::CONTROL) => {
                self.handle_interrupt();
            }
            (KeyCode::Enter, modifiers) if modifiers.contains(KeyModifiers::ALT) => {
                self.interaction_input.insert_newline();
            }
            (KeyCode::Enter, _) => {
                let Some(response) = self.interaction_input.response(&interaction) else {
                    self.state.status = "A value is required.".to_string();
                    return;
                };
                self.resolve_pending_interaction(Map::from_iter([(
                    "response".to_string(),
                    Value::String(response),
                )]));
                self.interaction_input = InteractionInput::default();
            }
            _ => {
                self.interaction_input.input(key);
            }
        }
    }

    fn submit_composer(&mut self) {
        let message = textarea_text(&self.composer);
        let trimmed = message.trim();
        if trimmed.is_empty() {
            return;
        }

        match trimmed {
            "/quit" | "/exit" => {
                if self.state.busy {
                    self.quit_after_turn = true;
                    self.state
                        .push_system("Will exit after the current turn finishes.");
                } else {
                    self.should_quit = true;
                }
            }
            "/help" => {
                self.push_help();
            }
            "/session" => {
                self.state
                    .push_system(format!("session {}", self.shell.session.id));
            }
            _ => {
                self.record_history(&message);
                self.enqueue_or_start(vec![message]);
            }
        }

        self.clear_composer();
    }

    fn push_help(&mut self) {
        self.state.push_system(
            "Commands\n  /help     Show commands and keys.\n  /session  Show the active session id.\n  /quit     Exit now, or after the active turn.\nKeys\n  Enter sends; busy turns queue the prompt.\n  Alt-Enter inserts a newline.\n  Up/Down recalls prompt history.\n  PgUp/PgDn or mouse wheel scrolls the transcript.\n  Ctrl-C cancels, clears input, or exits.",
        );
    }

    fn enqueue_or_start(&mut self, messages: Vec<String>) {
        if self.state.busy {
            self.queued_messages.push_back(messages);
            self.state.status = "queued".to_string();
            return;
        }
        self.state.push_user(messages.join("\n"));
        self.start_turn(messages);
    }

    fn start_next_queued_turn(&mut self) {
        if self.state.busy {
            return;
        }
        if self.quit_after_turn {
            self.should_quit = true;
            return;
        }
        if let Some(messages) = self.queued_messages.pop_front() {
            self.state.push_user(messages.join("\n"));
            self.start_turn(messages);
        }
    }

    fn start_turn(&mut self, messages: Vec<String>) {
        let system_messages = if self.shell.applied_system_messages {
            Vec::new()
        } else {
            self.shell.system_messages.clone()
        };
        self.shell.applied_system_messages = true;
        let turn_args = AgentTurnCreateArgs {
            session_id: self.shell.session.id.clone(),
            model: self.shell.model_override.clone(),
            system: system_messages,
            messages,
            tools: self.shell.tools.clone(),
            idempotency_key: None,
            input: None,
        };
        let (command_tx, command_rx) = mpsc::channel();
        let worker = spawn_turn_worker(
            self.client.clone(),
            turn_args,
            self.worker_tx.clone(),
            command_rx,
        );
        self.worker = Some(TurnWorker {
            command_tx,
            handle: Some(worker),
        });
        self.state.start_turn();
    }

    fn drain_worker_events(&mut self) {
        loop {
            match self.worker_rx.try_recv() {
                Ok(event) => self.handle_worker_event(event),
                Err(TryRecvError::Empty) => break,
                Err(TryRecvError::Disconnected) => break,
            }
        }
    }

    fn handle_worker_event(&mut self, event: WorkerEvent) {
        match event {
            WorkerEvent::TurnCreated(turn) => {
                self.state.current_turn_id = Some(turn.id.clone());
                self.state.status = turn_status_label(&turn);
            }
            WorkerEvent::TurnEvent(event) => self.state.apply_turn_event(*event),
            WorkerEvent::TurnSnapshot(turn) => self.state.finish_turn(&turn),
            WorkerEvent::WaitingForInput(interaction) => {
                self.interaction_input = InteractionInput::for_interaction(&interaction);
                self.state.wait_for_interaction(interaction);
            }
            WorkerEvent::InteractionResolved(interaction) => {
                let _ = interaction;
                self.state.status = "interaction resolved".to_string();
                self.state.pending_interaction = None;
            }
            WorkerEvent::Error(message) => {
                self.state.push_error(message);
                self.state.finish_worker();
                self.worker = None;
            }
            WorkerEvent::Done => {
                self.join_worker();
                self.state.finish_worker();
            }
        }
    }

    fn resolve_pending_interaction(&mut self, resolution: Map<String, Value>) {
        let Some(interaction) = self.state.pending_interaction.as_ref() else {
            return;
        };
        if let Some(worker) = self.worker.as_ref() {
            let sent = worker.command_tx.send(WorkerCommand::Resolve {
                interaction_id: interaction.id.clone(),
                resolution,
            });
            if sent.is_err() {
                self.state
                    .push_error("failed to send interaction resolution".to_string());
            } else {
                self.state.status = "resolving interaction".to_string();
            }
        }
    }

    fn handle_interrupt(&mut self) {
        if self.state.busy {
            if let Some(worker) = self.worker.as_ref() {
                let _ = worker.command_tx.send(WorkerCommand::Cancel);
            }
            if let Some(turn_id) = self.state.current_turn_id.clone() {
                let client = self.client.clone();
                thread::spawn(move || {
                    let _ = cancel_turn_silent(&client, &turn_id, INTERRUPT_CANCEL_REASON);
                });
            }
            self.state.status = "cancel requested".to_string();
            self.state.push_system("Cancel requested.");
        } else if !textarea_text(&self.composer).is_empty() {
            self.clear_composer();
        } else {
            self.should_quit = true;
        }
    }

    fn join_worker(&mut self) {
        if let Some(mut worker) = self.worker.take()
            && let Some(handle) = worker.handle.take()
        {
            let _ = handle.join();
        }
    }

    fn can_step_history_back(&self) -> bool {
        !self.input_history.is_empty()
            && (self.history_cursor.is_some() || self.composer.lines().len() == 1)
    }

    fn history_previous(&mut self) {
        if self.input_history.is_empty() {
            return;
        }
        let index = match self.history_cursor {
            Some(0) => 0,
            Some(index) => index - 1,
            None => {
                self.history_draft = textarea_text(&self.composer);
                self.input_history.len() - 1
            }
        };
        self.history_cursor = Some(index);
        let message = self.input_history[index].clone();
        self.set_composer_text(&message);
    }

    fn history_next(&mut self) {
        let Some(index) = self.history_cursor else {
            return;
        };
        if index + 1 < self.input_history.len() {
            let next = index + 1;
            self.history_cursor = Some(next);
            let message = self.input_history[next].clone();
            self.set_composer_text(&message);
        } else {
            self.history_cursor = None;
            let draft = std::mem::take(&mut self.history_draft);
            self.set_composer_text(&draft);
        }
    }

    fn set_composer_text(&mut self, text: &str) {
        self.composer = textarea_with_text("Message", text);
    }

    fn clear_composer(&mut self) {
        self.composer = styled_textarea("Message");
        self.reset_history_navigation();
    }

    fn reset_history_navigation(&mut self) {
        self.history_cursor = None;
        self.history_draft.clear();
    }

    fn record_history(&mut self, message: &str) {
        if message.trim().is_empty()
            || self
                .input_history
                .last()
                .is_some_and(|last| last == message)
        {
            return;
        }
        self.input_history.push(message.to_string());
        if self.input_history.len() > HISTORY_LIMIT {
            let remove = self.input_history.len() - HISTORY_LIMIT;
            self.input_history.drain(0..remove);
        }
    }
}

fn styled_textarea(title: &'static str) -> TextArea<'static> {
    textarea_with_text(title, "")
}

fn visible_lines(
    lines: Vec<Line<'static>>,
    visible_height: usize,
    scroll_offset: usize,
) -> Vec<Line<'static>> {
    let end = lines.len().saturating_sub(scroll_offset);
    let start = end.saturating_sub(visible_height.max(1));
    lines.into_iter().skip(start).take(end - start).collect()
}

fn push_transcript_item_lines(
    lines: &mut Vec<Line<'static>>,
    item: &TranscriptItem,
    content_width: usize,
) {
    match item.kind {
        state::TranscriptKind::User => {
            push_user_item_lines(lines, item, content_width);
            return;
        }
        state::TranscriptKind::Assistant => {
            push_assistant_item_lines(lines, item, content_width);
            return;
        }
        state::TranscriptKind::Meta => {
            push_meta_item_lines(lines, item, content_width);
            return;
        }
        _ => {}
    }

    let header_style = item.kind.header_style().add_modifier(Modifier::BOLD);
    lines.push(Line::from(Span::styled(
        item.kind.header().to_string(),
        header_style,
    )));

    let body_style = item.kind.body_style();
    let indent = "  ";
    let indent_width = UnicodeWidthStr::width(indent);
    let text_width = content_width.saturating_sub(indent_width).max(1);
    for text_line in item.text.split('\n') {
        for chunk in text_chunks(text_line, text_width) {
            lines.push(Line::from(Span::styled(
                format!("{indent}{chunk}"),
                body_style,
            )));
        }
    }
}

fn push_user_item_lines(
    lines: &mut Vec<Line<'static>>,
    item: &TranscriptItem,
    content_width: usize,
) {
    let style = Style::default().fg(Color::Black).bg(Color::Gray);
    let body_width = content_width
        .saturating_sub(UnicodeWidthStr::width(USER_PROMPT))
        .max(1);
    for (line_index, text_line) in item.text.split('\n').enumerate() {
        let chunks = text_chunks(text_line, body_width);
        for (chunk_index, chunk) in chunks.into_iter().enumerate() {
            let prefix = if line_index == 0 && chunk_index == 0 {
                USER_PROMPT
            } else {
                "  "
            };
            let line = pad_to_width(format!("{prefix}{chunk}"), content_width);
            lines.push(Line::from(Span::styled(line, style)));
        }
    }
}

fn push_assistant_item_lines(
    lines: &mut Vec<Line<'static>>,
    item: &TranscriptItem,
    content_width: usize,
) {
    let body_style = item.kind.body_style();
    let bullet_style = Style::default()
        .fg(Color::Green)
        .add_modifier(Modifier::BOLD);
    for (line_index, text_line) in item.text.split('\n').enumerate() {
        let prefix = if line_index == 0 {
            ASSISTANT_BULLET
        } else {
            "  "
        };
        let prefix_style = if line_index == 0 {
            bullet_style
        } else {
            body_style
        };
        push_wrapped_segments(
            lines,
            markdown_segments(text_line, body_style),
            prefix,
            "  ",
            prefix_style,
            body_style,
            content_width,
        );
    }
}

fn push_meta_item_lines(
    lines: &mut Vec<Line<'static>>,
    item: &TranscriptItem,
    content_width: usize,
) {
    let style = item.kind.body_style();
    for (line_index, text_line) in item.text.split('\n').enumerate() {
        let prefix = if line_index == 0 { META_PREFIX } else { "  " };
        push_wrapped_segments(
            lines,
            vec![StyledSegment::new(text_line.to_string(), style)],
            prefix,
            "  ",
            style,
            style,
            content_width,
        );
    }
}

fn push_wrapped_segments(
    lines: &mut Vec<Line<'static>>,
    segments: Vec<StyledSegment>,
    first_prefix: &str,
    continuation_prefix: &str,
    first_prefix_style: Style,
    continuation_prefix_style: Style,
    content_width: usize,
) {
    let mut spans = vec![Span::styled(first_prefix.to_string(), first_prefix_style)];
    let mut line_width = UnicodeWidthStr::width(first_prefix);
    let mut prefix_width = line_width;
    let mut wrote_text = false;

    for segment in segments {
        for grapheme in UnicodeSegmentation::graphemes(segment.text.as_str(), true) {
            let grapheme_width = UnicodeWidthStr::width(grapheme);
            if line_width > prefix_width
                && line_width.saturating_add(grapheme_width) > content_width
            {
                lines.push(Line::from(spans));
                spans = vec![Span::styled(
                    continuation_prefix.to_string(),
                    continuation_prefix_style,
                )];
                line_width = UnicodeWidthStr::width(continuation_prefix);
                prefix_width = line_width;
            }
            push_span(&mut spans, grapheme, segment.style);
            line_width = line_width.saturating_add(grapheme_width);
            wrote_text = true;
        }
    }

    if wrote_text || !first_prefix.is_empty() {
        lines.push(Line::from(spans));
    }
}

fn push_span(spans: &mut Vec<Span<'static>>, text: &str, style: Style) {
    if text.is_empty() {
        return;
    }
    if let Some(last) = spans.last_mut()
        && last.style == style
    {
        last.content.to_mut().push_str(text);
        return;
    }
    spans.push(Span::styled(text.to_string(), style));
}

fn text_chunks(text: &str, width: usize) -> Vec<String> {
    if text.is_empty() {
        return vec![String::new()];
    }

    let mut chunks = Vec::new();
    let mut chunk = String::new();
    let mut chunk_width = 0usize;
    for grapheme in UnicodeSegmentation::graphemes(text, true) {
        let grapheme_width = UnicodeWidthStr::width(grapheme);
        if chunk_width > 0 && chunk_width.saturating_add(grapheme_width) > width {
            chunks.push(std::mem::take(&mut chunk));
            chunk_width = 0;
        }
        chunk.push_str(grapheme);
        chunk_width += grapheme_width;
    }
    if !chunk.is_empty() {
        chunks.push(chunk);
    }
    chunks
}

fn pad_to_width(mut text: String, width: usize) -> String {
    let text_width = UnicodeWidthStr::width(text.as_str());
    if text_width < width {
        text.push_str(&" ".repeat(width - text_width));
    }
    text
}

#[derive(Clone)]
struct StyledSegment {
    text: String,
    style: Style,
}

impl StyledSegment {
    fn new(text: String, style: Style) -> Self {
        Self { text, style }
    }
}

fn markdown_segments(text: &str, base_style: Style) -> Vec<StyledSegment> {
    markdown_segments_with_depth(text, base_style, 0)
}

fn markdown_segments_with_depth(text: &str, base_style: Style, depth: usize) -> Vec<StyledSegment> {
    if depth > 6 {
        return vec![StyledSegment::new(text.to_string(), base_style)];
    }

    let mut segments = Vec::new();
    let mut index = 0usize;
    while index < text.len() {
        let rest = &text[index..];

        if let Some((label, url, consumed)) = markdown_link(rest) {
            let link_style = base_style
                .fg(Color::Cyan)
                .add_modifier(Modifier::UNDERLINED);
            append_segments(
                &mut segments,
                markdown_segments_with_depth(label, link_style, depth + 1),
            );
            push_segment(&mut segments, " (", base_style);
            push_segment(&mut segments, url, link_style);
            push_segment(&mut segments, ")", base_style);
            index += consumed;
            continue;
        }

        if let Some((url, consumed)) = raw_url(rest) {
            push_segment(
                &mut segments,
                url,
                base_style
                    .fg(Color::Cyan)
                    .add_modifier(Modifier::UNDERLINED),
            );
            index += consumed;
            continue;
        }

        if let Some((inner, consumed)) = delimited(text, index, "`") {
            push_segment(
                &mut segments,
                inner,
                base_style.fg(Color::Cyan).add_modifier(Modifier::BOLD),
            );
            index += consumed;
            continue;
        }

        if let Some((inner, consumed)) = delimited(text, index, "**") {
            append_segments(
                &mut segments,
                markdown_segments_with_depth(
                    inner,
                    base_style.add_modifier(Modifier::BOLD),
                    depth + 1,
                ),
            );
            index += consumed;
            continue;
        }

        if let Some((inner, consumed)) =
            delimited(text, index, "*").or_else(|| delimited(text, index, "_"))
        {
            append_segments(
                &mut segments,
                markdown_segments_with_depth(
                    inner,
                    base_style.add_modifier(Modifier::ITALIC),
                    depth + 1,
                ),
            );
            index += consumed;
            continue;
        }

        let Some(ch) = rest.chars().next() else {
            break;
        };
        push_segment(&mut segments, &ch.to_string(), base_style);
        index += ch.len_utf8();
    }
    segments
}

fn append_segments(target: &mut Vec<StyledSegment>, segments: Vec<StyledSegment>) {
    for segment in segments {
        push_segment(target, &segment.text, segment.style);
    }
}

fn push_segment(segments: &mut Vec<StyledSegment>, text: &str, style: Style) {
    if text.is_empty() {
        return;
    }
    if let Some(last) = segments.last_mut()
        && last.style == style
    {
        last.text.push_str(text);
        return;
    }
    segments.push(StyledSegment::new(text.to_string(), style));
}

fn markdown_link(rest: &str) -> Option<(&str, &str, usize)> {
    if !rest.starts_with('[') {
        return None;
    }
    let label_end = 1 + rest[1..].find("](")?;
    let url_start = label_end + 2;
    let url_end = url_start + rest[url_start..].find(')')?;
    let label = &rest[1..label_end];
    if label.is_empty() {
        return None;
    }
    let url = &rest[url_start..url_end];
    if url.is_empty() {
        return None;
    }
    Some((label, url, url_end + 1))
}

fn raw_url(rest: &str) -> Option<(&str, usize)> {
    if !(rest.starts_with("https://") || rest.starts_with("http://")) {
        return None;
    }
    let end = rest
        .char_indices()
        .find_map(|(index, ch)| ch.is_whitespace().then_some(index))
        .unwrap_or(rest.len());
    Some((&rest[..end], end))
}

fn delimited<'a>(text: &'a str, index: usize, delimiter: &str) -> Option<(&'a str, usize)> {
    let rest = &text[index..];
    if !rest.starts_with(delimiter) {
        return None;
    }
    if delimiter != "`" && !delimiter_boundary_before(text, index) {
        return None;
    }
    let after_open = &rest[delimiter.len()..];
    if after_open.chars().next().is_none_or(char::is_whitespace) {
        return None;
    }
    let close = after_open.find(delimiter)?;
    if close == 0 {
        return None;
    }
    let inner = &after_open[..close];
    if inner.chars().last().is_none_or(char::is_whitespace) {
        return None;
    }
    let consumed = delimiter.len() + close + delimiter.len();
    if delimiter != "`" && !delimiter_boundary_after(text, index + consumed) {
        return None;
    }
    Some((inner, consumed))
}

fn delimiter_boundary_before(text: &str, index: usize) -> bool {
    text[..index]
        .chars()
        .next_back()
        .is_none_or(|ch| !is_identifier_char(ch))
}

fn delimiter_boundary_after(text: &str, index: usize) -> bool {
    text[index..]
        .chars()
        .next()
        .is_none_or(|ch| !is_identifier_char(ch))
}

fn is_identifier_char(ch: char) -> bool {
    ch.is_alphanumeric() || ch == '_'
}

fn textarea_with_text(title: &'static str, text: &str) -> TextArea<'static> {
    let lines = if text.is_empty() {
        vec![String::new()]
    } else {
        text.split('\n').map(|line| line.to_string()).collect()
    };
    let mut textarea = TextArea::new(lines);
    configure_textarea(&mut textarea, title);
    textarea.move_cursor(CursorMove::Bottom);
    textarea.move_cursor(CursorMove::End);
    textarea
}

fn configure_textarea(textarea: &mut TextArea<'static>, title: &'static str) {
    textarea.set_cursor_line_style(Style::default());
    textarea.set_block(Block::default().borders(Borders::ALL).title(title));
}

struct InteractionInput {
    textarea: TextArea<'static>,
    secret: String,
    is_secret: bool,
    validation: Option<String>,
}

impl Default for InteractionInput {
    fn default() -> Self {
        Self {
            textarea: styled_textarea("Response"),
            secret: String::new(),
            is_secret: false,
            validation: None,
        }
    }
}

impl InteractionInput {
    fn for_interaction(interaction: &AgentInteractionInfo) -> Self {
        let is_secret = interaction
            .request
            .get("secret")
            .and_then(Value::as_bool)
            .unwrap_or(false);
        let mut input = Self {
            is_secret,
            ..Self::default()
        };
        if !is_secret
            && let Some(default) = interaction.request.get("default").and_then(Value::as_str)
        {
            input.textarea.insert_str(default);
        }
        input
    }

    fn render(&mut self, frame: &mut Frame<'_>, area: Rect) {
        if self.is_secret {
            let masked = if self.secret.is_empty() {
                "Value hidden".to_string()
            } else {
                "*".repeat(self.secret.chars().count())
            };
            let paragraph = Paragraph::new(masked)
                .block(Block::default().borders(Borders::TOP).title("Secret"));
            frame.render_widget(paragraph, area);
        } else {
            self.textarea
                .set_block(Block::default().borders(Borders::TOP).title("Response"));
            frame.render_widget(&self.textarea, area);
        }
    }

    fn input(&mut self, key: KeyEvent) {
        self.validation = None;
        if self.is_secret {
            match key.code {
                KeyCode::Char(ch)
                    if !key.modifiers.contains(KeyModifiers::CONTROL)
                        && !key.modifiers.contains(KeyModifiers::ALT) =>
                {
                    self.secret.push(ch);
                }
                KeyCode::Backspace => {
                    self.secret.pop();
                }
                _ => {}
            }
        } else {
            self.textarea.input(key);
        }
    }

    fn insert_newline(&mut self) {
        self.validation = None;
        if !self.is_secret {
            self.textarea.insert_newline();
        }
    }

    fn response(&mut self, interaction: &AgentInteractionInfo) -> Option<String> {
        let value = if self.is_secret {
            self.secret.clone()
        } else {
            textarea_text(&self.textarea)
        };
        let trimmed = value.trim().to_string();
        if trimmed.is_empty() {
            if let Some(default) = interaction.request.get("default").and_then(Value::as_str) {
                self.validation = None;
                return Some(default.to_string());
            }
            let required = interaction
                .request
                .get("required")
                .and_then(Value::as_bool)
                .unwrap_or(true);
            if required {
                self.validation = Some("A value is required.".to_string());
                return None;
            }
            self.validation = None;
            return Some(String::new());
        }
        self.validation = None;
        Some(trimmed)
    }

    fn validation(&self) -> Option<&str> {
        self.validation.as_deref()
    }
}

fn textarea_text(textarea: &TextArea<'_>) -> String {
    textarea.lines().join("\n")
}

fn interaction_summary(
    interaction: &AgentInteractionInfo,
    validation: Option<&str>,
    help: &str,
) -> Vec<Line<'static>> {
    let mut lines = Vec::new();
    if !interaction.prompt.is_empty() {
        lines.push(Line::from(interaction.prompt.clone()));
    }
    if !interaction.request.is_empty()
        && let Ok(request) = compact_json(&Value::Object(interaction.request.clone()))
    {
        lines.push(Line::from(format!("Request: {request}")));
    }
    if let Some(validation) = validation {
        lines.push(Line::from(Span::styled(
            validation.to_string(),
            Style::default().fg(Color::Red),
        )));
    }
    lines.push(Line::from(""));
    lines.push(Line::from(help.to_string()));
    lines
}
